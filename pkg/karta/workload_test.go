// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package karta_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"

	"github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/karta"
	"github.com/run-ai/karta/pkg/karta/kartatest"
)

func mustWorkload(definition *v1alpha1.Karta, obj *unstructured.Unstructured) karta.Workload {
	GinkgoHelper()
	workload, err := karta.New(definition, obj)
	Expect(err).NotTo(HaveOccurred())
	return workload
}

func field(obj karta.Workload, path ...string) any {
	GinkgoHelper()
	raw, err := obj.Object()
	Expect(err).NotTo(HaveOccurred())
	value, found, err := unstructured.NestedFieldNoCopy(raw.(*unstructured.Unstructured).Object, path...)
	Expect(err).NotTo(HaveOccurred())
	Expect(found).To(BeTrue(), "path %v", path)
	return value
}

var _ = Describe("Workload", func() {
	ctx := context.Background()

	It("rejects a nil or invalid definition eagerly", func() {
		_, err := karta.New(nil, templateWorkload())
		Expect(err).To(HaveOccurred())

		bad := templateDefinition()
		bad.Spec.StructureDefinition.RootComponent.SpecDefinition.PodTemplateSpecPath = ptr.To("this is not jq {{{")
		_, err = karta.New(bad, templateWorkload())
		Expect(err).To(HaveOccurred())
	})

	Describe("UpdatePodTemplate routing", func() {
		It("routes through a full pod template", func() {
			w := mustWorkload(templateDefinition(), templateWorkload())
			Expect(w.UpdatePodTemplate(ctx, "root", karta.PodPatch{
				SchedulerName: ptr.To("kai-scheduler"),
				Labels:        map[string]string{"team": "ml"},
				Image:         ptr.To("app:v2"),
			})).To(Succeed())

			Expect(field(w, "spec", "template", "spec", "schedulerName")).To(Equal("kai-scheduler"))
			Expect(field(w, "spec", "template", "metadata", "labels")).To(
				Equal(map[string]any{"existing": "yes", "team": "ml"}))
			containers := field(w, "spec", "template", "spec", "containers").([]any)
			Expect(containers[0].(map[string]any)["image"]).To(Equal("app:v2"))
		})

		It("routes through a bare pod spec and reports labels as unsupported", func() {
			w := mustWorkload(podSpecDefinition(), podSpecWorkload())
			Expect(w.UpdatePodTemplate(ctx, "root", karta.PodPatch{
				SchedulerName: ptr.To("kai-scheduler"),
			})).To(Succeed())
			Expect(field(w, "spec", "podSpec", "schedulerName")).To(Equal("kai-scheduler"))

			err := w.UpdatePodTemplate(ctx, "root", karta.PodPatch{Labels: map[string]string{"team": "ml"}})
			Expect(errors.Is(err, karta.ErrNotSupported)).To(BeTrue())
			var unsupported *karta.UnsupportedFieldsError
			Expect(errors.As(err, &unsupported)).To(BeTrue())
			Expect(unsupported.Fields).To(Equal([]karta.PodField{karta.PodFieldLabels}))
		})

		It("routes metadata through the split shape", func() {
			w := mustWorkload(splitDefinition(), podSpecWorkload())
			Expect(w.UpdatePodTemplate(ctx, "root", karta.PodPatch{
				SchedulerName: ptr.To("kai-scheduler"),
				Labels:        map[string]string{"team": "ml"},
			})).To(Succeed())
			Expect(field(w, "spec", "podSpec", "schedulerName")).To(Equal("kai-scheduler"))
			Expect(field(w, "spec", "podMetadata", "labels")).To(
				Equal(map[string]any{"existing": "yes", "team": "ml"}))
		})

		It("routes per-field paths on the fragmented shape and writes only set fields", func() {
			w := mustWorkload(fragmentedDefinition(), fragmentedWorkload())
			Expect(w.UpdatePodTemplate(ctx, "root", karta.PodPatch{
				Labels: map[string]string{"team": "ml"},
			})).To(Succeed())
			Expect(field(w, "spec", "podLabels")).To(
				Equal(map[string]any{"existing": "yes", "team": "ml"}))
			// untouched fragmented paths keep their exact values
			Expect(field(w, "spec", "schedulerName")).To(Equal("default-scheduler"))
			Expect(field(w, "spec", "image")).To(Equal("app:v1"))
		})

		It("reports every unsupported field at once, before any write", func() {
			w := mustWorkload(fragmentedDefinition(), fragmentedWorkload())
			err := w.UpdatePodTemplate(ctx, "root", karta.PodPatch{
				SchedulerName: ptr.To("x"), // supported
				NodeAffinity:  &corev1NodeAffinity,
				Resources:     &corev1Resources,
			})
			var unsupported *karta.UnsupportedFieldsError
			Expect(errors.As(err, &unsupported)).To(BeTrue())
			Expect(unsupported.Fields).To(Equal([]karta.PodField{
				karta.PodFieldNodeAffinity, karta.PodFieldResources,
			}))
			// nothing changed
			Expect(field(w, "spec", "schedulerName")).To(Equal("default-scheduler"))
		})

		It("rejects an empty patch", func() {
			w := mustWorkload(templateDefinition(), templateWorkload())
			Expect(w.UpdatePodTemplate(ctx, "root", karta.PodPatch{})).To(MatchError(karta.ErrEmptyPatch))
		})

		It("fails image on a multi-container pod, naming the containers", func() {
			w := mustWorkload(podSpecDefinition(), podSpecWorkload())
			err := w.UpdatePodTemplate(ctx, "root", karta.PodPatch{Image: ptr.To("app:v2")})
			Expect(err).To(MatchError(ContainSubstring("sole container")))
			Expect(errors.Is(err, karta.ErrNotSupported)).To(BeFalse())
			// atomic: nothing changed
			Expect(field(w, "spec", "podSpec", "schedulerName")).To(Equal("default-scheduler"))
		})

		It("merges containers by name and rejects unknown names", func() {
			w := mustWorkload(podSpecDefinition(), podSpecWorkload())
			Expect(w.UpdatePodTemplate(ctx, "root", karta.PodPatch{
				Containers: []karta.ContainerPatch{{Name: "sidecar", Image: ptr.To("sidecar:v2")}},
			})).To(Succeed())
			containers := field(w, "spec", "podSpec", "containers").([]any)
			Expect(containers[1].(map[string]any)["image"]).To(Equal("sidecar:v2"))

			err := w.UpdatePodTemplate(ctx, "root", karta.PodPatch{
				Containers: []karta.ContainerPatch{{Name: "nope", Image: ptr.To("x")}},
			})
			Expect(err).To(MatchError(ContainSubstring(`container "nope" not found`)))
		})

		It("stays atomic when a route fails mid-way on the split shape", func() {
			definition := splitDefinition()
			// metadata path reads a scalar - the second write arm fails after
			// the first already wrote to the scratch
			definition.Spec.StructureDefinition.RootComponent.SpecDefinition.MetadataPath = ptr.To(".spec.image")
			workload := podSpecWorkload()
			workload.Object["spec"].(map[string]any)["image"] = "not-metadata"
			w := mustWorkload(definition, workload)

			err := w.UpdatePodTemplate(ctx, "root", karta.PodPatch{
				SchedulerName: ptr.To("kai-scheduler"),
				Labels:        map[string]string{"team": "ml"},
			})
			Expect(err).To(HaveOccurred())
			// the pod spec write that succeeded on the scratch never landed
			Expect(field(w, "spec", "podSpec", "schedulerName")).To(Equal("default-scheduler"))
		})

		It("rejects unknown instance ids before any write", func() {
			w := mustWorkload(templateDefinition(), templateWorkload())
			err := w.UpdatePodTemplate(ctx, "root", karta.PodPatch{SchedulerName: ptr.To("x")},
				karta.WithInstances("missing"))
			Expect(err).To(MatchError(ContainSubstring(`unknown instance id "missing"`)))
			Expect(field(w, "spec", "template", "spec", "schedulerName")).To(Equal("default-scheduler"))
		})
	})

	Describe("Suspend and Resume", func() {
		suspendable := func() *v1alpha1.Karta {
			definition := templateDefinition()
			definition.Spec.StructureDefinition.RootComponent.SuspendDefinition = &v1alpha1.SuspendDefinition{
				SuspendActions: []v1alpha1.SuspendAction{{Path: ".spec.suspend", Value: "true"}},
				ResumeActions:  []v1alpha1.SuspendAction{{Path: ".spec.suspend", Value: "false"}},
			}
			return definition
		}

		It("applies suspend actions and resumes back", func() {
			w := mustWorkload(suspendable(), templateWorkload())
			Expect(w.Suspend(ctx)).To(Succeed())
			Expect(field(w, "spec", "suspend")).To(Equal(true))
			Expect(w.Resume(ctx)).To(Succeed())
			Expect(field(w, "spec", "suspend")).To(Equal(false))
		})

		It("returns the typed error when nothing is suspendable", func() {
			w := mustWorkload(templateDefinition(), templateWorkload())
			err := w.Suspend(ctx)
			Expect(errors.Is(err, karta.ErrNotSupported)).To(BeTrue())
			var unsupportedOp *karta.UnsupportedOperationError
			Expect(errors.As(err, &unsupportedOp)).To(BeTrue())
		})
	})

	Describe("Tree and Object", func() {
		It("builds the tree and iterates instances", func() {
			// the root itself is not a tree node; give it a child that
			// carries the pod definition
			definition := templateDefinition()
			definition.Spec.StructureDefinition.ChildComponents = []v1alpha1.ComponentDefinition{{
				Name:     "child",
				Kind:     &v1alpha1.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Job"},
				OwnerRef: ptr.To("root"),
				SpecDefinition: &v1alpha1.SpecDefinition{
					PodTemplateSpecPath: ptr.To(".spec.template"),
				},
			}}
			w := mustWorkload(definition, templateWorkload())
			workloadTree, err := w.Tree(ctx)
			Expect(err).NotTo(HaveOccurred())
			count := 0
			for component, instance := range workloadTree.Instances() {
				Expect(component.Name).To(Equal("child"))
				Expect(instance.ExtractedInstance).NotTo(BeNil())
				count++
			}
			Expect(count).To(Equal(1))
		})
	})

	Describe("the fake", func() {
		It("rejects exactly what production rejects for the same definition", func() {
			definition := fragmentedDefinition()
			real := mustWorkload(definition, fragmentedWorkload())
			fake := kartatest.NewFromKarta(definition)

			patch := karta.PodPatch{NodeAffinity: &corev1NodeAffinity}
			realErr := real.UpdatePodTemplate(ctx, "root", patch)
			fakeErr := fake.UpdatePodTemplate(ctx, "root", patch)

			var fromReal, fromFake *karta.UnsupportedFieldsError
			Expect(errors.As(realErr, &fromReal)).To(BeTrue())
			Expect(errors.As(fakeErr, &fromFake)).To(BeTrue())
			Expect(fromFake.Fields).To(Equal(fromReal.Fields))
		})

		It("records accepted updates", func() {
			fake := kartatest.NewFromKarta(templateDefinition())
			Expect(fake.UpdatePodTemplate(ctx, "root", karta.PodPatch{SchedulerName: ptr.To("x")},
				karta.WithInstances("a"))).To(Succeed())
			Expect(fake.PodUpdates).To(HaveLen(1))
			Expect(fake.PodUpdates[0].Instances).To(Equal([]string{"a"}))
		})
	})
})

var _ = Describe("a definition with no write routes", func() {
	ctx := context.Background()

	It("reads fine and rejects every write with one typed error", func() {
		w := mustWorkload(routelessDefinition(), fragmentedWorkload())
		Expect(w.Components()[0].PodFields).To(BeEmpty())
		Expect(karta.WritablePodFields(routelessDefinition().Spec.StructureDefinition.RootComponent)).To(BeEmpty())

		err := w.UpdatePodTemplate(ctx, "root", karta.PodPatch{SchedulerName: ptr.To("kai")})
		Expect(errors.Is(err, karta.ErrNotSupported)).To(BeTrue())
		var unsupported *karta.UnsupportedFieldsError
		Expect(errors.As(err, &unsupported)).To(BeTrue())
		Expect(unsupported.Fields).To(Equal([]karta.PodField{karta.PodFieldSchedulerName}))

		err = w.UpdatePodTemplate(ctx, "root", karta.Patch{
			"spec": karta.Patch{"schedulerName": "kai"},
		})
		Expect(errors.Is(err, karta.ErrNotSupported)).To(BeTrue())
	})
})

var _ = Describe("WithInstances on a fragmented shape", func() {
	ctx := context.Background()

	It("targets one instance when the fragment paths iterate with the ids", func() {
		w := mustWorkload(fragmentedWorkersDefinition(), fragmentedWorkersWorkload())
		Expect(w.UpdatePodTemplate(ctx, "root", karta.PodPatch{Image: ptr.To("app:v2")},
			karta.WithInstances("b"))).To(Succeed())
		workers := field(w, "spec", "workers").([]any)
		Expect(workers[0].(map[string]any)["image"]).To(Equal("app:v1"))
		Expect(workers[1].(map[string]any)["image"]).To(Equal("app:v2"))
	})
})

var _ = Describe("PatchPodTemplate - the raw merge-patch door", func() {
	ctx := context.Background()

	It("writes fields the typed door never enumerated", func() {
		w := mustWorkload(templateDefinition(), templateWorkload())
		Expect(w.UpdatePodTemplate(ctx, "root", karta.Patch{
			"spec": karta.Patch{
				"tolerations": []any{
					map[string]any{"key": "gpu", "operator": "Exists", "effect": "NoSchedule"},
				},
				"schedulerName": "kai-scheduler",
			},
			"metadata": karta.Patch{"labels": karta.Patch{"team": "ml"}},
		})).To(Succeed())

		tolerations := field(w, "spec", "template", "spec", "tolerations").([]any)
		Expect(tolerations[0].(map[string]any)["key"]).To(Equal("gpu"))
		Expect(field(w, "spec", "template", "spec", "schedulerName")).To(Equal("kai-scheduler"))
		Expect(field(w, "spec", "template", "metadata", "labels")).To(
			Equal(map[string]any{"existing": "yes", "team": "ml"}))
	})

	It("merges containers by name, touching only provided keys", func() {
		w := mustWorkload(podSpecDefinition(), podSpecWorkload())
		Expect(w.UpdatePodTemplate(ctx, "root", karta.Patch{
			"spec": karta.Patch{"containers": []any{
				map[string]any{"name": "sidecar", "image": "sidecar:v9"},
			}},
		})).To(Succeed())
		containers := field(w, "spec", "podSpec", "containers").([]any)
		Expect(containers[1].(map[string]any)["image"]).To(Equal("sidecar:v9"))
		Expect(containers[0].(map[string]any)["image"]).To(Equal("app:v1")) // untouched
	})

	It("reports metadata as unsupported on the bare pod spec shape", func() {
		w := mustWorkload(podSpecDefinition(), podSpecWorkload())
		err := w.UpdatePodTemplate(ctx, "root", karta.Patch{
			"metadata": karta.Patch{"labels": karta.Patch{"a": "b"}},
		})
		Expect(errors.Is(err, karta.ErrNotSupported)).To(BeTrue())
		var unsupported *karta.UnsupportedFieldsError
		Expect(errors.As(err, &unsupported)).To(BeTrue())
		Expect(unsupported.Fields).To(Equal([]karta.PodField{"metadata.labels"}))
	})

	It("routes fragmented leaves through the definition and lists the rest", func() {
		w := mustWorkload(fragmentedDefinition(), fragmentedWorkload())
		Expect(w.UpdatePodTemplate(ctx, "root", karta.Patch{
			"metadata": karta.Patch{"labels": karta.Patch{"team": "ml"}},
		})).To(Succeed())
		Expect(field(w, "spec", "podLabels")).To(
			Equal(map[string]any{"existing": "yes", "team": "ml"}))

		err := w.UpdatePodTemplate(ctx, "root", karta.Patch{
			"spec": karta.Patch{"tolerations": []any{map[string]any{"key": "gpu"}}},
		})
		var unsupported *karta.UnsupportedFieldsError
		Expect(errors.As(err, &unsupported)).To(BeTrue())
		Expect(unsupported.Fields).To(Equal([]karta.PodField{"spec.tolerations"}))
	})

	It("rejects nulls and non-template top-level keys, replaces zero scalars", func() {
		w := mustWorkload(templateDefinition(), templateWorkload())
		Expect(w.UpdatePodTemplate(ctx, "root", karta.Patch{
			"spec": karta.Patch{"schedulerName": nil},
		})).To(MatchError(ContainSubstring("deleting values is not supported")))
		Expect(w.UpdatePodTemplate(ctx, "root", karta.Patch{
			"status": karta.Patch{"phase": "x"},
		})).To(MatchError(ContainSubstring("top-level keys are metadata and spec")))
		Expect(w.UpdatePodTemplate(ctx, "root", karta.Patch{
			"spec": karta.Patch{"labels": karta.Patch{"x": "y"}},
		})).To(MatchError(ContainSubstring("not a valid partial pod template")))
		// atomic: nothing landed
		Expect(field(w, "spec", "template", "spec", "schedulerName")).To(Equal("default-scheduler"))
		// scalars replace, including the zero value - "" is deliberate intent
		Expect(w.UpdatePodTemplate(ctx, "root", karta.Patch{
			"spec": karta.Patch{"schedulerName": ""},
		})).To(Succeed())
		Expect(field(w, "spec", "template", "spec", "schedulerName")).To(Equal(""))
	})
})
