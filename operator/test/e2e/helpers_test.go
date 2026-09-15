//go:build e2e

// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package e2e

import (
	"fmt"
	"os"
	"time"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func envDuration(key string, fallback time.Duration) time.Duration {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		panic(fmt.Sprintf("%s=%q is not a duration: %v", key, raw, err))
	}
	return d
}

// newKarta returns a Karta rooted at the given GVK, valid unless opted out. Specs pick
// a built-in GVK when they want CRDExists satisfied without installing anything, and a
// made-up one when they want it unsatisfied.
//
// StatusDefinition is what makes it valid: the validator requires one on the root, and
// with the webhook on, a Karta without it is refused at admission rather than reported
// as Validated=False.
func newKarta(name string, gvk schema.GroupVersionKind) *kartav1alpha1.Karta {
	return &kartav1alpha1.Karta{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kartav1alpha1.KartaSpec{
			StructureDefinition: kartav1alpha1.StructureDefinition{
				RootComponent: kartav1alpha1.ComponentDefinition{
					Name: name + "-root",
					Kind: &kartav1alpha1.GroupVersionKind{
						Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind,
					},
					StatusDefinition: &kartav1alpha1.StatusDefinition{
						StatusMappings: kartav1alpha1.StatusMappings{},
					},
				},
			},
		},
	}
}

// newInvalidKarta drops the StatusDefinition, which is the cheapest way to fail the
// validator without depending on any other rule.
func newInvalidKarta(name string, gvk schema.GroupVersionKind) *kartav1alpha1.Karta {
	k := newKarta(name, gvk)
	k.Spec.StructureDefinition.RootComponent.StatusDefinition = nil
	return k
}

// createKarta applies the Karta and registers its deletion, so a failing spec cannot
// leak a cluster-scoped object into the next one.
func createKarta(k *kartav1alpha1.Karta) *kartav1alpha1.Karta {
	GinkgoHelper()
	Expect(k8sClient.Create(testCtx, k)).To(Succeed())
	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(testCtx, k))).To(Succeed())
	})
	return k
}

func getKarta(g Gomega, name string) *kartav1alpha1.Karta {
	k := &kartav1alpha1.Karta{}
	g.Expect(k8sClient.Get(testCtx, types.NamespacedName{Name: name}, k)).To(Succeed())
	return k
}

// conditionIs reports whether the named condition holds status at the object's current
// generation. The generation guard is what stops an assertion from passing on the
// status a previous reconcile left behind; it is inert for transitions the spec does
// not cause (deleting a referenced CRD does not bump generation), which is why those
// specs assert the starting state before acting.
func conditionIs(k *kartav1alpha1.Karta, ct kartav1alpha1.ConditionType, status metav1.ConditionStatus) (bool, string) {
	for _, c := range k.Status.Conditions {
		if c.Type != string(ct) {
			continue
		}
		switch {
		case c.ObservedGeneration != k.Generation:
			return false, fmt.Sprintf("%s is stale (observed %d, want %d)", ct, c.ObservedGeneration, k.Generation)
		case c.Status != status:
			return false, fmt.Sprintf("%s is %s (reason %q), want %s", ct, c.Status, c.Reason, status)
		}
		return true, ""
	}
	return false, fmt.Sprintf("%s is not set", ct)
}

// expectCondition waits for the condition to reach status.
func expectCondition(name string, ct kartav1alpha1.ConditionType, status metav1.ConditionStatus) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		ok, why := conditionIs(getKarta(g, name), ct, status)
		g.Expect(ok).To(BeTrue(), why)
	}, reconcileTimeout, pollInterval).Should(Succeed())
}

// expectConditionSettled waits for the condition, then holds it for settleWindow.
// Eventually alone accepts a value that flickers past; the negative assertions are
// only meaningful if the condition stays put.
func expectConditionSettled(name string, ct kartav1alpha1.ConditionType, status metav1.ConditionStatus) {
	GinkgoHelper()
	expectCondition(name, ct, status)
	Consistently(func(g Gomega) {
		ok, why := conditionIs(getKarta(g, name), ct, status)
		g.Expect(ok).To(BeTrue(), why)
	}, settleWindow, pollInterval).Should(Succeed())
}

// expectGone waits for the object to disappear, which is what proves cleanup finished
// rather than that a delete was merely accepted.
func expectGone(name string) {
	GinkgoHelper()
	Eventually(func() bool {
		err := k8sClient.Get(testCtx, types.NamespacedName{Name: name}, &kartav1alpha1.Karta{})
		return errors.IsNotFound(err)
	}, reconcileTimeout, pollInterval).Should(BeTrue(), "Karta %s was not removed", name)
}
