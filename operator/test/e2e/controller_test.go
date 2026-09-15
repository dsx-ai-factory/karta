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

// replicaSetGVK is a built-in, so CRDExists is satisfied with nothing installed, and no
// catalog Karta roots there, so the cluster-wide root-GVK uniqueness rule cannot reject
// these. hack/e2e/smoke.yaml picks it for the same two reasons.
var replicaSetGVK = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}

// Serial throughout: Kartas are cluster-scoped and the CRD specs mutate cluster-wide
// state, so nothing here is safe to run beside a sibling.
var _ = Describe("Karta controller on a live cluster", Serial, func() {
	It("drives a valid Karta to Ready", func() {
		k := createKarta(newKarta("e2e-ready", replicaSetGVK))

		expectCondition(k.Name, kartav1alpha1.ConditionValidated, metav1.ConditionTrue)
		expectCondition(k.Name, kartav1alpha1.ConditionCRDExists, metav1.ConditionTrue)
		expectConditionSettled(k.Name, kartav1alpha1.ConditionReady, metav1.ConditionTrue)
	})

	It("reports CRDExists=False and Ready=False when the referenced CRD is absent", func() {
		// A group nothing serves, so the reconciler finds neither a native GVK nor a CRD.
		absent := schema.GroupVersionKind{Group: "absent.e2e.run.ai", Version: "v1", Kind: "Missing"}
		k := createKarta(newKarta("e2e-crd-absent", absent))

		expectCondition(k.Name, kartav1alpha1.ConditionCRDExists, metav1.ConditionFalse)
		expectConditionSettled(k.Name, kartav1alpha1.ConditionReady, metav1.ConditionFalse)
	})

	It("follows a CRD appearing and then being removed", func() {
		gvk := schema.GroupVersionKind{Group: "flow.e2e.run.ai", Version: "v1", Kind: "Widget"}
		k := createKarta(newKarta("e2e-crd-flow", gvk))

		// Assert the starting state before acting. Installing a CRD does not bump the
		// Karta's generation, so the generation guard cannot tell a stale False from a
		// fresh one; observing False first is what makes the flip to True a transition
		// rather than a coincidence.
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

		// There is no finalizer today, so this asserts the object actually goes away
		// rather than sitting in Terminating. It is the regression that would catch a
		// finalizer added without the cleanup to release it.
		expectGone(k.Name)
	})
})

// createCRD installs a minimal CRD for the GVK and registers its removal.
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
		Eventually(func() bool {
			err := k8sClient.Get(testCtx, types.NamespacedName{Name: crd.Name}, &apiextensionsv1.CustomResourceDefinition{})
			return apierrors.IsNotFound(err)
		}, reconcileTimeout, pollInterval).Should(BeTrue(), "CRD %s outlived its spec", crd.Name)
	})
	return crd
}
