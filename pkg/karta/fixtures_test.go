// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package karta_test

import (
	"encoding/json"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"

	"github.com/run-ai/karta/pkg/api/runai/v1alpha1"
)

func definitionWith(spec *v1alpha1.SpecDefinition, suspend *v1alpha1.SuspendDefinition) *v1alpha1.Karta {
	return &v1alpha1.Karta{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: v1alpha1.KartaSpec{
			StructureDefinition: v1alpha1.StructureDefinition{
				RootComponent: v1alpha1.ComponentDefinition{
					Name: "root",
					Kind: &v1alpha1.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Job"},
					StatusDefinition: &v1alpha1.StatusDefinition{
						PhaseDefinition: &v1alpha1.PhaseDefinition{Path: ".status.phase"},
						StatusMappings: v1alpha1.StatusMappings{
							Running: []v1alpha1.StatusMatcher{{ByPhase: "Running"}},
						},
					},
					SpecDefinition:    spec,
					SuspendDefinition: suspend,
				},
			},
		},
	}
}

func templateDefinition() *v1alpha1.Karta {
	return definitionWith(&v1alpha1.SpecDefinition{
		PodTemplateSpecPath: ptr.To(".spec.template"),
	}, nil)
}

func podSpecDefinition() *v1alpha1.Karta {
	return definitionWith(&v1alpha1.SpecDefinition{
		PodSpecPath: ptr.To(".spec.podSpec"),
	}, nil)
}

func splitDefinition() *v1alpha1.Karta {
	return definitionWith(&v1alpha1.SpecDefinition{
		PodSpecPath:  ptr.To(".spec.podSpec"),
		MetadataPath: ptr.To(".spec.podMetadata"),
	}, nil)
}

func fragmentedDefinition() *v1alpha1.Karta {
	return definitionWith(&v1alpha1.SpecDefinition{
		FragmentedPodSpecDefinition: &v1alpha1.FragmentedPodSpecDefinition{
			SchedulerNamePath: ptr.To(".spec.schedulerName"),
			LabelsPath:        ptr.To(".spec.podLabels"),
			ImagePath:         ptr.To(".spec.image"),
		},
	}, nil)
}

func fragmentedWorkersDefinition() *v1alpha1.Karta {
	definition := definitionWith(&v1alpha1.SpecDefinition{
		FragmentedPodSpecDefinition: &v1alpha1.FragmentedPodSpecDefinition{
			SchedulerNamePath: ptr.To(".spec.workers[].schedulerName"),
			ImagePath:         ptr.To(".spec.workers[].image"),
		},
	}, nil)
	definition.Spec.StructureDefinition.RootComponent.InstanceIdPath = ptr.To(".spec.workers[].name")
	definition.Spec.StructureDefinition.RootComponent.PodSelector = &v1alpha1.PodSelector{
		ComponentInstanceSelector: &v1alpha1.ComponentInstanceSelector{
			IdPath: ".metadata.labels[\"worker\"]",
		},
	}
	return definition
}

func fragmentedWorkersWorkload() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Job",
		"metadata": map[string]any{"name": "job"},
		"spec": map[string]any{
			"workers": []any{
				map[string]any{"name": "a", "image": "app:v1", "schedulerName": "default-scheduler"},
				map[string]any{"name": "b", "image": "app:v1", "schedulerName": "default-scheduler"},
			},
		},
	}}
}

// routelessDefinition has a spec group with no writable paths at all - the
// shape a non-path definition language produces before its routes plug in.
func routelessDefinition() *v1alpha1.Karta {
	return definitionWith(&v1alpha1.SpecDefinition{
		FragmentedPodSpecDefinition: &v1alpha1.FragmentedPodSpecDefinition{},
	}, nil)
}

func templateWorkload() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Job",
		"metadata": map[string]any{"name": "job"},
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"existing": "yes"}},
				"spec": map[string]any{
					"schedulerName": "default-scheduler",
					"containers": []any{
						map[string]any{"name": "main", "image": "app:v1"},
					},
				},
			},
		},
	}}
}

func podSpecWorkload() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Job",
		"metadata": map[string]any{"name": "job"},
		"spec": map[string]any{
			"podSpec": map[string]any{
				"schedulerName": "default-scheduler",
				"containers": []any{
					map[string]any{"name": "main", "image": "app:v1"},
					map[string]any{"name": "sidecar", "image": "sidecar:v1"},
				},
			},
			"podMetadata": map[string]any{"labels": map[string]any{"existing": "yes"}},
		},
	}}
}

func fragmentedWorkload() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Job",
		"metadata": map[string]any{"name": "job"},
		"spec": map[string]any{
			"schedulerName": "default-scheduler",
			"podLabels":     map[string]any{"existing": "yes"},
			"image":         "app:v1",
		},
	}}
}

var corev1NodeAffinity = nodeAffinityFixture()
var corev1Resources = resourcesFixture()

func nodeAffinityFixture() corev1.NodeAffinity {
	return corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{{
				MatchExpressions: []corev1.NodeSelectorRequirement{{
					Key: "gpu", Operator: corev1.NodeSelectorOpExists,
				}},
			}},
		},
	}
}

func resourcesFixture() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
	}
}

func podAffinityFixture() *corev1.PodAffinity {
	return &corev1.PodAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
			TopologyKey: "karta-gate-topology",
		}},
	}
}

func gateResourcesFixture() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("777Mi")},
	}
}

// asJSON normalizes for comparison: Go number types may differ, values must not.
func asJSON(value any) string {
	raw, err := json.Marshal(value)
	Expect(err).NotTo(HaveOccurred())
	return string(raw)
}
