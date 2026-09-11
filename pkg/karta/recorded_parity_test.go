// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package karta_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	"github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/karta"
	"github.com/run-ai/karta/pkg/resource"
	"github.com/run-ai/karta/pkg/tree"
)

// recordedState is the slice of an e2e recording this spec consumes: the karta
// definition path and every recorded real-cluster object state.
type recordedState struct {
	source     string
	definition *v1alpha1.Karta
	object     map[string]any
}

func loadRecordedStates() []recordedState {
	GinkgoHelper()
	matches, err := filepath.Glob("../../test/e2e/recorded_data/*/*/*/*.yaml")
	Expect(err).NotTo(HaveOccurred())
	var states []recordedState
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		var recording struct {
			KartaFile string `json:"kartaFile"`
			Events    []struct {
				Kind   string         `json:"kind"`
				Object map[string]any `json:"object"`
			} `json:"events"`
		}
		Expect(yaml.Unmarshal(raw, &recording)).To(Succeed(), path)
		definitionRaw, err := os.ReadFile(filepath.Join("../..", recording.KartaFile))
		Expect(err).NotTo(HaveOccurred(), recording.KartaFile)
		definition := &v1alpha1.Karta{}
		Expect(yaml.Unmarshal(definitionRaw, definition)).To(Succeed())
		for i, event := range recording.Events {
			if event.Kind != "STATE" || event.Object == nil {
				continue
			}
			states = append(states, recordedState{
				source:     fmt.Sprintf("%s#%d", filepath.Base(filepath.Dir(path))+"/"+filepath.Base(path), i),
				definition: definition,
				object:     event.Object,
			})
		}
	}
	return states
}

// diffPaths returns every leaf path where a and b differ.
func diffPaths(a, b any, prefix string) []string {
	switch typedA := a.(type) {
	case map[string]any:
		typedB, ok := b.(map[string]any)
		if !ok {
			return []string{prefix}
		}
		keys := map[string]bool{}
		for key := range typedA {
			keys[key] = true
		}
		for key := range typedB {
			keys[key] = true
		}
		var paths []string
		for key := range keys {
			paths = append(paths, diffPaths(typedA[key], typedB[key], prefix+"."+key)...)
		}
		return paths
	case []any:
		typedB, ok := b.([]any)
		if !ok || len(typedA) != len(typedB) {
			return []string{prefix}
		}
		var paths []string
		for i := range typedA {
			paths = append(paths, diffPaths(typedA[i], typedB[i], fmt.Sprintf("%s[%d]", prefix, i))...)
		}
		return paths
	default:
		if fmt.Sprintf("%v", a) != fmt.Sprintf("%v", b) {
			return []string{prefix}
		}
		return nil
	}
}

var _ = Describe("recorded real-cluster data: before/after parity", func() {
	ctx := context.Background()
	states := loadRecordedStates()

	It("has recordings to work with", func() {
		Expect(states).NotTo(BeEmpty())
	})

	It("extraction is identical: tree.Build then vs Workload.Tree now", func() {
		for _, state := range states {
			obj := &unstructured.Unstructured{Object: state.object}

			factory := resource.NewComponentFactoryFromObject(state.definition, obj.DeepCopy())
			before, err := tree.Build(ctx, factory)
			Expect(err).NotTo(HaveOccurred(), state.source)

			w, err := karta.New(state.definition, obj.DeepCopy())
			Expect(err).NotTo(HaveOccurred(), state.source)
			after, err := w.Tree(ctx)
			Expect(err).NotTo(HaveOccurred(), state.source)

			Expect(asJSON(after)).To(Equal(asJSON(before)), state.source)
		}
	})

	It("mutation: the new leaf writes change only the intended leaves; the typed path did not", func() {
		type rowT struct {
			source      string
			newExtra    int
			oldExtra    int
			oldExamples []string
		}
		var rows []rowT
		for _, state := range states {
			original := &unstructured.Unstructured{Object: state.object}
			rootName := state.definition.Spec.StructureDefinition.RootComponent.Name

			// the intent, both ways
			scheduler := "karta-parity-scheduler"
			labelKey, labelValue := "karta-parity", "yes"

			// NEW way: the front door
			w, err := karta.New(state.definition, original.DeepCopy())
			Expect(err).NotTo(HaveOccurred(), state.source)
			Expect(w.UpdatePodTemplate(ctx, rootName, karta.PodPatch{
				SchedulerName: ptr.To(scheduler),
				Labels:        map[string]string{labelKey: labelValue},
			})).To(Succeed(), state.source)
			mutated, err := w.Object()
			Expect(err).NotTo(HaveOccurred())
			newObject := mutated.(*unstructured.Unstructured).Object

			// OLD way: the typed component API, as the quickstart did before
			factory := resource.NewComponentFactoryFromObject(state.definition, original.DeepCopy())
			component, err := factory.GetComponent(rootName)
			Expect(err).NotTo(HaveOccurred())
			templates, err := component.GetPodTemplateSpec(ctx)
			Expect(err).NotTo(HaveOccurred(), state.source)
			updates := map[string]corev1.PodTemplateSpec{}
			for id, template := range templates {
				template.Spec.SchedulerName = scheduler
				if template.Labels == nil {
					template.Labels = map[string]string{}
				}
				template.Labels[labelKey] = labelValue
				updates[id] = template
			}
			var oldObject map[string]any
			oldDestroyed := ""
			if err := component.UpdatePodTemplateSpec(ctx, updates); err != nil {
				oldDestroyed = "typed update failed: " + err.Error()
			} else if oldResult, err := factory.GetResource(); err != nil {
				// the typed round-trip replaced the object so thoroughly that
				// karta itself no longer accepts it
				oldDestroyed = "object destroyed: " + err.Error()
			} else {
				oldObject = oldResult.(*unstructured.Unstructured).Object
			}

			// the new way landed the intent
			flat := fmt.Sprintf("%v", newObject)
			Expect(flat).To(ContainSubstring(scheduler), state.source)
			Expect(flat).To(ContainSubstring(labelKey), state.source)

			expected := func(path string) bool {
				return strings.HasSuffix(path, ".schedulerName") ||
					strings.HasSuffix(path, ".labels") || // the labels map created
					strings.Contains(path, ".labels.")
			}
			classify := func(object map[string]any) (int, []string) {
				var extra []string
				for _, path := range diffPaths(state.object, object, "") {
					if !expected(path) {
						extra = append(extra, path)
					}
				}
				sort.Strings(extra)
				return len(extra), extra
			}
			newExtra, newPaths := classify(newObject)

			// THE claim under test: the new way touches nothing else.
			Expect(newPaths).To(BeEmpty(),
				fmt.Sprintf("%s: leaf writes changed unexpected paths", state.source))

			oldExtra := -1
			var examples []string
			if oldDestroyed != "" {
				examples = []string{oldDestroyed}
			} else {
				var oldPaths []string
				oldExtra, oldPaths = classify(oldObject)
				if len(oldPaths) > 6 {
					oldPaths = oldPaths[:6]
				}
				examples = oldPaths
			}
			rows = append(rows, rowT{state.source, newExtra, oldExtra, examples})
		}

		AddReportEntry("before/after mutation parity", func() string {
			var builder strings.Builder
			for _, row := range rows {
				fmt.Fprintf(&builder, "%-40s new-way extra changes: %d | typed-path extra changes: %d %v\n",
					row.source, row.newExtra, row.oldExtra, row.oldExamples)
			}
			return builder.String()
		}())
	})
})
