//go:build e2e

// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	kartav1alpha1 "github.com/dsx-ai-factory/workload-map/pkg/api/runai/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	reconcileTimeout = envDuration("KARTA_E2E_RECONCILE_TIMEOUT", 60*time.Second)
	settleWindow     = envDuration("KARTA_E2E_SETTLE_WINDOW", 10*time.Second)
	pollInterval     = envDuration("KARTA_E2E_POLL_INTERVAL", 500*time.Millisecond)
)

var (
	k8sClient  client.Client
	testCtx    context.Context
	testCancel context.CancelFunc
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Karta Operator E2E Suite")
}

var _ = BeforeSuite(func() {
	testCtx, testCancel = context.WithCancel(context.Background())

	cfg, err := ctrl.GetConfig()
	Expect(err).NotTo(HaveOccurred(), "no kubeconfig; run make e2e-up first")

	k8sClient, err = client.New(cfg, client.Options{Scheme: buildScheme()})
	Expect(err).NotTo(HaveOccurred())

	Expect(k8sClient.List(testCtx, &kartav1alpha1.KartaList{})).To(Succeed(),
		"cannot list Kartas; run make e2e-up WORKLOADS=none")
})

var _ = AfterSuite(func() {
	if testCancel != nil {
		testCancel()
	}
})

func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	Expect(kartav1alpha1.AddToScheme(s)).To(Succeed())
	Expect(apiextensionsv1.AddToScheme(s)).To(Succeed())
	Expect(admissionv1.AddToScheme(s)).To(Succeed())
	return s
}

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

func newInvalidKarta(name string, gvk schema.GroupVersionKind) *kartav1alpha1.Karta {
	k := newKarta(name, gvk)
	k.Spec.StructureDefinition.RootComponent.StatusDefinition = nil
	return k
}

func webhookEnabled() bool {
	GinkgoHelper()
	cfg := &admissionv1.ValidatingWebhookConfiguration{}
	err := k8sClient.Get(testCtx, types.NamespacedName{Name: validatingWebhookName}, cfg)
	if apierrors.IsNotFound(err) {
		return false
	}
	Expect(err).NotTo(HaveOccurred())
	return true
}

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

// The generation guard stops an assertion passing on the status a previous reconcile
// left behind. It is inert where the spec itself does not bump generation.
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

func expectCondition(name string, ct kartav1alpha1.ConditionType, status metav1.ConditionStatus) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		ok, why := conditionIs(getKarta(g, name), ct, status)
		g.Expect(ok).To(BeTrue(), why)
	}, reconcileTimeout, pollInterval).Should(Succeed())
}

// Eventually alone accepts a value that flickers past.
func expectConditionSettled(name string, ct kartav1alpha1.ConditionType, status metav1.ConditionStatus) {
	GinkgoHelper()
	expectCondition(name, ct, status)
	Consistently(func(g Gomega) {
		ok, why := conditionIs(getKarta(g, name), ct, status)
		g.Expect(ok).To(BeTrue(), why)
	}, settleWindow, pollInterval).Should(Succeed())
}

func expectGone(name string) {
	GinkgoHelper()
	Eventually(func() bool {
		err := k8sClient.Get(testCtx, types.NamespacedName{Name: name}, &kartav1alpha1.Karta{})
		return apierrors.IsNotFound(err)
	}, reconcileTimeout, pollInterval).Should(BeTrue(), "Karta %s was not removed", name)
}
