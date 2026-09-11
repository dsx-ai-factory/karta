// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package karta_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	"github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/karta"
)

// the catalog conformance gate: for EVERY shipped definition, every statically
// writable field must apply cleanly to a synthetic object, changing only what
// the patch names (canaries prove it), and every unsupported field must return
// the typed error without touching the object.
var _ = Describe("catalog conformance gate", func() {
	ctx := context.Background()

	catalog := func() map[string]*v1alpha1.Karta {
		matches, err := filepath.Glob("../../docs/catalog/*.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).NotTo(BeEmpty())
		definitions := map[string]*v1alpha1.Karta{}
		for _, path := range matches {
			raw, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			definition := &v1alpha1.Karta{}
			Expect(yaml.Unmarshal(raw, definition)).To(Succeed(), path)
			definitions[filepath.Base(path)] = definition
		}
		return definitions
	}

	It("every catalog definition passes New's eager validation on a matching object", func() {
		for name, definition := range catalog() {
			obj := karta.SyntheticWorkload(definition)
			_, err := karta.New(definition, obj)
			Expect(err).NotTo(HaveOccurred(), name)
		}
	})

	It("every statically writable field applies cleanly and touches only its leaves", func() {
		checked := 0
		for name, definition := range catalog() {
			for _, info := range componentInfos(definition) {
				for _, field := range info.PodFields {
					patch, want := minimalPatch(field)
					obj := karta.SyntheticWorkload(definition)
					workload, err := karta.New(definition, obj)
					Expect(err).NotTo(HaveOccurred(), name)

					err = workload.UpdatePodTemplate(ctx, info.Name, patch)
					Expect(err).NotTo(HaveOccurred(),
						fmt.Sprintf("%s component %s field %s", name, info.Name, field))

					mutated, err := workload.Object()
					Expect(err).NotTo(HaveOccurred())
					object := mutated.(*unstructured.Unstructured).Object
					Expect(fmt.Sprintf("%v", object)).To(ContainSubstring(want),
						fmt.Sprintf("%s component %s field %s: patched value missing", name, info.Name, field))
					Expect(fmt.Sprintf("%v", object)).To(ContainSubstring(karta.GateCanary),
						fmt.Sprintf("%s component %s field %s: canary was destroyed", name, info.Name, field))
					checked++
				}
			}
		}
		Expect(checked).To(BeNumerically(">", 40), "the gate must actually exercise the catalog")
	})

	It("the raw door: an unenumerated field works on template shapes, classifies on fragmented", func() {
		rawPatch := karta.Patch{
			"spec": karta.Patch{"tolerations": []any{
				map[string]any{"key": "karta-gate", "operator": "Exists"},
			}},
		}
		exercised := 0
		for name, definition := range catalog() {
			for _, info := range componentInfos(definition) {
				if len(info.PodFields) == 0 {
					continue
				}
				obj := karta.SyntheticWorkload(definition)
				workload, err := karta.New(definition, obj)
				Expect(err).NotTo(HaveOccurred(), name)
				err = workload.UpdatePodTemplate(ctx, info.Name, rawPatch)
				if err != nil {
					// only ever the typed capability error, never a jq failure
					var unsupportedFields *karta.UnsupportedFieldsError
					Expect(errors.As(err, &unsupportedFields)).To(BeTrue(),
						fmt.Sprintf("%s/%s: %v", name, info.Name, err))
					continue
				}
				mutated, err := workload.Object()
				Expect(err).NotTo(HaveOccurred())
				flat := fmt.Sprintf("%v", mutated.(*unstructured.Unstructured).Object)
				Expect(flat).To(ContainSubstring("karta-gate"), name+"/"+info.Name)
				Expect(flat).To(ContainSubstring(karta.GateCanary), name+"/"+info.Name)
				exercised++
			}
		}
		Expect(exercised).To(BeNumerically(">", 10), "the raw door must exercise the catalog")
	})

	It("named impure paths classify as unsupported, typed, before any write", func() {
		definitions := catalog()

		By("nimcache resources is a computed projection - not writable")
		nimcache := definitions["apps-nvidia-com-nimcache-v1alpha1.yaml"]
		Expect(nimcache).NotTo(BeNil())
		root := nimcache.Spec.StructureDefinition.RootComponent
		Expect(karta.WritablePodFields(root)).NotTo(ContainElement(karta.PodFieldResources))
		workload, err := karta.New(nimcache, karta.SyntheticWorkload(nimcache))
		Expect(err).NotTo(HaveOccurred())
		err = workload.UpdatePodTemplate(ctx, root.Name, karta.PodPatch{Resources: ptr.To(resourcesFixture())})
		Expect(errors.Is(err, karta.ErrNotSupported)).To(BeTrue())
		var unsupportedFields *karta.UnsupportedFieldsError
		Expect(errors.As(err, &unsupportedFields)).To(BeTrue())

		By("kserve predictor image/resources/containers only had containerPath - read-only")
		kserve := definitions["serving-kserve-io-inferenceservice-v1beta1.yaml"]
		Expect(kserve).NotTo(BeNil())
		for _, child := range kserve.Spec.StructureDefinition.ChildComponents {
			if child.Name != "predictor" {
				continue
			}
			fields := karta.WritablePodFields(child)
			Expect(fields).NotTo(ContainElement(karta.PodFieldImage))
			Expect(fields).NotTo(ContainElement(karta.PodFieldResources))
		}

		By("kserve transformer shares podSpecPath and metadataPath - labels stay writable and safe")
		for _, child := range kserve.Spec.StructureDefinition.ChildComponents {
			if child.Name != "transformer" {
				continue
			}
			Expect(karta.WritablePodFields(child)).To(ContainElement(karta.PodFieldLabels))
		}
	})
})

func componentInfos(definition *v1alpha1.Karta) []karta.ComponentInfo {
	structure := definition.Spec.StructureDefinition
	infos := []karta.ComponentInfo{{
		Name:      structure.RootComponent.Name,
		Root:      true,
		PodFields: karta.WritablePodFields(structure.RootComponent),
	}}
	for _, child := range structure.ChildComponents {
		infos = append(infos, karta.ComponentInfo{
			Name:      child.Name,
			PodFields: karta.WritablePodFields(child),
		})
	}
	return infos
}

func minimalPatch(field karta.PodField) (karta.PodPatch, string) {
	switch field {
	case karta.PodFieldSchedulerName:
		return karta.PodPatch{SchedulerName: ptr.To("karta-gate-sched")}, "karta-gate-sched"
	case karta.PodFieldPriorityClassName:
		return karta.PodPatch{PriorityClassName: ptr.To("karta-gate-prio")}, "karta-gate-prio"
	case karta.PodFieldLabels:
		return karta.PodPatch{Labels: map[string]string{"karta-gate": "label-hit"}}, "label-hit"
	case karta.PodFieldAnnotations:
		return karta.PodPatch{Annotations: map[string]string{"karta-gate": "annotation-hit"}}, "annotation-hit"
	case karta.PodFieldNodeAffinity:
		affinity := nodeAffinityFixture()
		return karta.PodPatch{NodeAffinity: &affinity}, "gpu"
	case karta.PodFieldPodAffinity:
		return karta.PodPatch{PodAffinity: podAffinityFixture()}, "karta-gate-topology"
	case karta.PodFieldResourceClaims:
		return karta.PodPatch{ResourceClaims: []corev1.PodResourceClaim{{Name: "karta-gate-claim"}}}, "karta-gate-claim"
	case karta.PodFieldImage:
		return karta.PodPatch{Image: ptr.To("karta-gate-image:1")}, "karta-gate-image:1"
	case karta.PodFieldResources:
		resources := gateResourcesFixture()
		return karta.PodPatch{Resources: &resources}, "777Mi"
	case karta.PodFieldContainers:
		return karta.PodPatch{Containers: []karta.ContainerPatch{{Name: "main", Image: ptr.To("karta-gate-image:2")}}}, "karta-gate-image:2"
	default:
		panic("unknown field " + string(field))
	}
}
