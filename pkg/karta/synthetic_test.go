// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package karta

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/run-ai/karta/pkg/api/runai/v1alpha1"
)

// GateCanary marks values the catalog gate plants next to every seeded leaf;
// a surviving canary proves a patch touched only what it named.
const GateCanary = "karta-gate-canary"

// SyntheticWorkload builds a minimal raw object every pure path of the
// definition matches, with canaries planted beside each seeded leaf.
// Test-only export for the catalog conformance gate.
func SyntheticWorkload(definition *v1alpha1.Karta) *unstructured.Unstructured {
	object := map[string]any{
		"metadata":  map[string]any{"name": "gate"},
		"kartaGate": GateCanary,
	}
	root := definition.Spec.StructureDefinition.RootComponent
	if root.Kind != nil {
		apiVersion := root.Kind.Version
		if root.Kind.Group != "" {
			apiVersion = root.Kind.Group + "/" + root.Kind.Version
		}
		object["apiVersion"] = apiVersion
		object["kind"] = root.Kind.Kind
	}
	seedComponent(object, root)
	for _, child := range definition.Spec.StructureDefinition.ChildComponents {
		seedComponent(object, child)
	}
	return &unstructured.Unstructured{Object: object}
}

func seedComponent(object map[string]any, component v1alpha1.ComponentDefinition) {
	if component.InstanceIdPath != nil {
		seedPath(object, *component.InstanceIdPath, "instance-a")
	}
	spec := component.SpecDefinition
	if spec == nil {
		return
	}
	pod := map[string]any{
		"containers": []any{map[string]any{
			"name": "main", "image": "app:v1", "kartaGate": GateCanary,
		}},
		"kartaGate": GateCanary,
	}
	switch specShape(component) {
	case shapeTemplate:
		seedPath(object, *spec.PodTemplateSpecPath, map[string]any{
			"metadata":  map[string]any{"kartaGate": GateCanary},
			"spec":      pod,
			"kartaGate": GateCanary,
		})
	case shapePodSpec, shapeSplit:
		seedPath(object, *spec.PodSpecPath, pod)
		if spec.MetadataPath != nil {
			seedPath(object, *spec.MetadataPath, map[string]any{"kartaGate": GateCanary})
		}
	case shapeFragmented:
		fragmented := spec.FragmentedPodSpecDefinition
		seedPath(object, deref(fragmented.SchedulerNamePath), "old-scheduler")
		seedPath(object, deref(fragmented.PriorityClassNamePath), "old-priority")
		seedPath(object, deref(fragmented.LabelsPath), map[string]any{"existing": GateCanary})
		seedPath(object, deref(fragmented.AnnotationsPath), map[string]any{"existing": GateCanary})
		seedPath(object, deref(fragmented.NodeAffinityPath), map[string]any{})
		seedPath(object, deref(fragmented.PodAffinityPath), map[string]any{})
		seedPath(object, deref(fragmented.ResourcesPath), map[string]any{})
		seedPath(object, deref(fragmented.ResourceClaimsPath), []any{})
		seedPath(object, deref(fragmented.ContainersPath), []any{map[string]any{"name": "main", "image": "app:v1"}})
		seedPath(object, deref(fragmented.ImagePath), "app:v1")
	}
}

func deref(path *string) string {
	if path == nil {
		return ""
	}
	return *path
}

// seedPath merges a minimal structure matching the pure path into object.
// Impure paths are skipped - they are read-only by contract.
func seedPath(object map[string]any, path string, leaf any) {
	if path == "" {
		return
	}
	segments, ok := parseWritablePath(path)
	if !ok {
		return
	}
	synthetic := syntheticObject(segments, leaf)
	syntheticMap, ok := synthetic.(map[string]any)
	if !ok {
		return
	}
	mergeSynthetic(object, syntheticMap)
}

// mergeSynthetic deep-merges b into a; lists merge index-wise.
func mergeSynthetic(a, b map[string]any) {
	for key, value := range b {
		existing, present := a[key]
		if !present {
			a[key] = value
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			if existingMap, ok := existing.(map[string]any); ok {
				mergeSynthetic(existingMap, typed)
				continue
			}
		case []any:
			if existingList, ok := existing.([]any); ok {
				for i := range typed {
					if i < len(existingList) {
						if em, ok1 := existingList[i].(map[string]any); ok1 {
							if tm, ok2 := typed[i].(map[string]any); ok2 {
								mergeSynthetic(em, tm)
								continue
							}
						}
						continue
					}
					existingList = append(existingList, typed[i])
				}
				a[key] = existingList
				continue
			}
		}
		// keep the existing seed on conflicts
	}
}

// syntheticObject builds a minimal raw object that the pure path matches,
// with leaf as the value at the path (one element per iteration).
func syntheticObject(segments []pathSegment, leaf any) any {
	if len(segments) == 0 {
		return leaf
	}
	head, rest := segments[0], segments[1:]
	child := syntheticObject(rest, leaf)
	switch {
	case head.iter:
		return []any{child}
	case head.key != "":
		return map[string]any{head.key: child}
	default:
		return map[string]any{head.field: child}
	}
}
