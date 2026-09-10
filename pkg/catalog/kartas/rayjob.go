// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package kartas

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
)

// Rayjob returns the built-in Karta for the RayJob workload
// (ray.io/v1).
func Rayjob() *v1alpha1.Karta {
	return &v1alpha1.Karta{
		TypeMeta:   metav1.TypeMeta{APIVersion: "run.ai/v1alpha1", Kind: "Karta"},
		ObjectMeta: metav1.ObjectMeta{Name: "ray-io-rayjob-v1"},
		Spec: v1alpha1.KartaSpec{
			StructureDefinition: v1alpha1.StructureDefinition{
				RootComponent: v1alpha1.ComponentDefinition{
					Name: "rayjob",
					Kind: &v1alpha1.GroupVersionKind{Group: "ray.io", Version: "v1", Kind: "RayJob"},
					SuspendDefinition: &v1alpha1.SuspendDefinition{
						SuspendActions: []v1alpha1.SuspendAction{{Path: ".spec.suspend", Value: "true"}},
						ResumeActions:  []v1alpha1.SuspendAction{{Path: ".spec.suspend", Value: "false"}},
					},
					StatusDefinition: &v1alpha1.StatusDefinition{
						PhaseDefinition: &v1alpha1.PhaseDefinition{
							Path: ".status.jobStatus",
						},
						ConditionsDefinition: &v1alpha1.ConditionsDefinition{
							Path:             ".status.conditions",
							TypeFieldName:    "type",
							StatusFieldName:  "status",
							MessageFieldName: ptr.To("message"),
						},
						StatusMappings: v1alpha1.StatusMappings{
							// PENDING once the job is queued, plus the provisioning window before
							// that: jobStatus empty while the RayJob brings up its cluster. Both
							// exclude the suspension states, which read Suspended, never Initializing.
							Initializing: []v1alpha1.StatusMatcher{
								{ByExpression: &v1alpha1.ExpressionMatcher{
									Expression:     `(.status.jobStatus // "") == "PENDING" and (.status.jobDeploymentStatus // "") != "Suspended" and (.status.jobDeploymentStatus // "") != "Suspending"`,
									ExpectedResult: "true",
								}},
								{ByExpression: &v1alpha1.ExpressionMatcher{
									Expression:     `(.status.jobStatus // "") == "" and (.status.jobDeploymentStatus // "") != "Suspended" and (.status.jobDeploymentStatus // "") != "Suspending"`,
									ExpectedResult: "true",
								}},
							},
							Running:   []v1alpha1.StatusMatcher{{ByPhase: "RUNNING"}},
							Completed: []v1alpha1.StatusMatcher{{ByPhase: "SUCCEEDED"}},
							Failed:    []v1alpha1.StatusMatcher{{ByPhase: "FAILED"}},
							// Suspended or on the way there: the operator reports Suspending while it
							// tears the cluster down before settling on Suspended.
							Suspended: []v1alpha1.StatusMatcher{{ByExpression: &v1alpha1.ExpressionMatcher{
								Expression:     `.status.jobDeploymentStatus == "Suspended" or .status.jobDeploymentStatus == "Suspending"`,
								ExpectedResult: "true",
							}}},
						},
					},
				},
				ChildComponents: []v1alpha1.ComponentDefinition{
					{
						Name:     "head",
						Kind:     &v1alpha1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
						OwnerRef: ptr.To("rayjob"),
						SpecDefinition: &v1alpha1.SpecDefinition{
							PodTemplateSpecPath: ptr.To(".spec.rayClusterSpec.headGroupSpec.template"),
						},
						ScaleDefinition: &v1alpha1.ScaleDefinition{
							ReplicasPath: ptr.To("1"),
						},
						PodSelector: &v1alpha1.PodSelector{
							ComponentTypeSelector: &v1alpha1.ComponentTypeSelector{
								KeyPath: `.metadata.labels["ray.io/node-type"]`,
								Value:   ptr.To("head"),
							},
						},
					},
					{
						Name:     "worker",
						Kind:     &v1alpha1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
						OwnerRef: ptr.To("rayjob"),
						SpecDefinition: &v1alpha1.SpecDefinition{
							PodTemplateSpecPath: ptr.To(".spec.rayClusterSpec.workerGroupSpecs[].template"),
						},
						ScaleDefinition: &v1alpha1.ScaleDefinition{
							ReplicasPath:    ptr.To(".spec.rayClusterSpec.workerGroupSpecs[].replicas // 1"),
							MinReplicasPath: ptr.To(".spec.rayClusterSpec.workerGroupSpecs[].minReplicas"),
							MaxReplicasPath: ptr.To(".spec.rayClusterSpec.workerGroupSpecs[].maxReplicas"),
						},
						InstanceIdPath: ptr.To(".spec.rayClusterSpec.workerGroupSpecs[].groupName"),
						PodSelector: &v1alpha1.PodSelector{
							ComponentTypeSelector: &v1alpha1.ComponentTypeSelector{
								KeyPath: `.metadata.labels["ray.io/node-type"]`,
								Value:   ptr.To("worker"),
							},
							ComponentInstanceSelector: &v1alpha1.ComponentInstanceSelector{
								IdPath: `.metadata.labels["ray.io/group"]`,
							},
						},
					},
				},
			},
			Instructions: v1alpha1.OptimizationInstructions{
				GangScheduling: &v1alpha1.GangSchedulingInstruction{
					PodGroups: []v1alpha1.PodGroupDefinition{{
						Name: "job",
						Members: []v1alpha1.PodGroupMemberDefinition{
							{
								ComponentName:   "head",
								GroupByKeyPaths: []string{`.metadata.labels["ray.io/cluster"]`},
							},
							{
								ComponentName:   "worker",
								GroupByKeyPaths: []string{`.metadata.labels["ray.io/cluster"]`},
							},
						},
					}},
				},
			},
		},
	}
}
