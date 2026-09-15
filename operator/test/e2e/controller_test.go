//go:build e2e

// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package e2e

import (
	"strings"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Built-in, so CRDExists needs nothing installed, and unclaimed by any catalog root,
// so the cluster-wide uniqueness rule cannot reject these.
var (
	replicaSetGVK = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	daemonSetGVK  = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"}
)

// Rendered by the chart from karta.fullname; hack/e2e/global.env holds the same base.
const validatingWebhookName = "karta-operator-validating"

// Serial: everything here is cluster-scoped, so no spec is safe beside a sibling.
var _ = Describe("Karta controller on a live cluster", Serial, func() {
	It("drives a valid Karta to Ready", func() {
		k := createKarta(newKarta("e2e-ready", replicaSetGVK))

		expectCondition(k.Name, kartav1alpha1.ConditionValidated, metav1.ConditionTrue)
		expectCondition(k.Name, kartav1alpha1.ConditionCRDExists, metav1.ConditionTrue)
		expectConditionSettled(k.Name, kartav1alpha1.ConditionReady, metav1.ConditionTrue)
	})

	It("reports CRDExists=False and Ready=False when the referenced CRD is absent", func() {
		absent := schema.GroupVersionKind{Group: "absent.e2e.run.ai", Version: "v1", Kind: "Missing"}
		k := createKarta(newKarta("e2e-crd-absent", absent))

		expectCondition(k.Name, kartav1alpha1.ConditionCRDExists, metav1.ConditionFalse)
		expectConditionSettled(k.Name, kartav1alpha1.ConditionReady, metav1.ConditionFalse)
	})

	It("follows a CRD appearing and then being removed", func() {
		gvk := schema.GroupVersionKind{Group: "flow.e2e.run.ai", Version: "v1", Kind: "Widget"}
		k := createKarta(newKarta("e2e-crd-flow", gvk))

		// Installing a CRD does not bump generation, so observing False first is what
		// makes the flip a transition rather than a coincidence.
		expectCondition(k.Name, kartav1alpha1.ConditionCRDExists, metav1.ConditionFalse)

		crd := createCRD(gvk)
		expectCondition(k.Name, kartav1alpha1.ConditionCRDExists, metav1.ConditionTrue)
		expectCondition(k.Name, kartav1alpha1.ConditionReady, metav1.ConditionTrue)

		Expect(k8sClient.Delete(testCtx, crd)).To(Succeed())
		expectCondition(k.Name, kartav1alpha1.ConditionCRDExists, metav1.ConditionFalse)
		expectConditionSettled(k.Name, kartav1alpha1.ConditionReady, metav1.ConditionFalse)
	})

	It("removes a Karta cleanly on delete", func() {
		k := createKarta(newKarta("e2e-delete", replicaSetGVK))
		expectCondition(k.Name, kartav1alpha1.ConditionReady, metav1.ConditionTrue)

		Expect(k8sClient.Delete(testCtx, k)).To(Succeed())

		// No finalizer today; this is what would catch one added without its release.
		expectGone(k.Name)
	})
})

func createCRD(gvk schema.GroupVersionKind) *apiextensionsv1.CustomResourceDefinition {
	GinkgoHelper()
	plural := strings.ToLower(gvk.Kind) + "s"
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: plural + "." + gvk.Group},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: gvk.Group,
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: plural,
				Kind:   gvk.Kind,
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    gvk.Version,
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
				},
			}},
		},
	}
	Expect(k8sClient.Create(testCtx, crd)).To(Succeed())
	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(testCtx, crd))).To(Succeed())
		// Wait it out: a surviving CRD would flip the next spec's CRDExists.
		Eventually(func() bool {
			err := k8sClient.Get(testCtx, types.NamespacedName{Name: crd.Name}, &apiextensionsv1.CustomResourceDefinition{})
			return apierrors.IsNotFound(err)
		}, reconcileTimeout, pollInterval).Should(BeTrue(), "CRD %s outlived its spec", crd.Name)
	})
	return crd
}

// An invalid Karta behaves differently per route, so each half gets its own block
// rather than a branch inside one spec. ValidateCreate runs the same validator the
// controller runs, so with the webhook on the Karta never exists and no condition is
// ever written.
var _ = Describe("an invalid Karta, webhook installed", Serial, Label("webhook"), func() {
	BeforeEach(func() {
		if !webhookEnabled() {
			Skip("no validating webhook on this cluster (KARTA_WEBHOOK_MODE=disabled)")
		}
	})

	It("is refused at admission and does not persist", func() {
		k := newInvalidKarta("e2e-invalid-admission", daemonSetGVK)

		Expect(k8sClient.Create(testCtx, k)).To(MatchError(ContainSubstring("status definition")))

		err := k8sClient.Get(testCtx, types.NamespacedName{Name: k.Name}, &kartav1alpha1.Karta{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "a refused create must leave nothing behind")
	})
})

var _ = Describe("an invalid Karta, webhook disabled", Serial, Label("no-webhook"), func() {
	BeforeEach(func() {
		if webhookEnabled() {
			Skip("validating webhook is installed, so an invalid Karta never reaches the controller")
		}
	})

	It("is admitted and reported as Validated=False then Ready=False", func() {
		k := createKarta(newInvalidKarta("e2e-invalid-reported", daemonSetGVK))

		expectCondition(k.Name, kartav1alpha1.ConditionValidated, metav1.ConditionFalse)
		expectConditionSettled(k.Name, kartav1alpha1.ConditionReady, metav1.ConditionFalse)
	})

	It("carries the validator error in the condition message", func() {
		k := createKarta(newInvalidKarta("e2e-invalid-message", daemonSetGVK))

		expectCondition(k.Name, kartav1alpha1.ConditionValidated, metav1.ConditionFalse)
		Eventually(func(g Gomega) {
			for _, c := range getKarta(g, k.Name).Status.Conditions {
				if c.Type == string(kartav1alpha1.ConditionValidated) {
					g.Expect(c.Message).To(ContainSubstring("status definition"))
					return
				}
			}
			g.Expect(false).To(BeTrue(), "Validated is not set")
		}, reconcileTimeout, pollInterval).Should(Succeed())
	})
})
