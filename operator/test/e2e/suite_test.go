//go:build e2e

// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

// Package e2e holds the cluster-backed tests for the Karta operator. Unlike the
// envtest suite next door it starts no manager and installs nothing: it talks to
// whatever cluster the ambient kubeconfig points at, where Karta is already running
// from the chart. hack/e2e/up.sh puts that cluster there.
//
// The build tag keeps it out of go test ./... so `make check` can never reach for a
// cluster; `make test-e2e` is the only way in.
package e2e

import (
	"context"
	"testing"
	"time"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Timeouts are named rather than inline so a slow cluster is one change, not twenty.
// Each is overridable from the environment for the same reason CI and a laptop
// disagree about how long a reconcile takes.
var (
	// reconcileTimeout covers one controller round trip: the operator is already
	// running, so this is watch latency plus a status patch, not a deployment.
	reconcileTimeout = envDuration("KARTA_E2E_RECONCILE_TIMEOUT", 60*time.Second)
	// settleWindow is how long a condition must hold to count as settled, for the
	// negative assertions where Eventually alone would accept a flicker.
	settleWindow = envDuration("KARTA_E2E_SETTLE_WINDOW", 10*time.Second)
	// pollInterval is the gap between polls for both of the above.
	pollInterval = envDuration("KARTA_E2E_POLL_INTERVAL", 500*time.Millisecond)
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

	// GetConfigOrDie and nothing else: the suite never creates a cluster, so the same
	// binary runs against kind from e2e-up, a reused cluster, or a remote one.
	cfg, err := ctrl.GetConfig()
	Expect(err).NotTo(HaveOccurred(), "no kubeconfig; run make e2e-up first")

	k8sClient, err = client.New(cfg, client.Options{Scheme: buildScheme()})
	Expect(err).NotTo(HaveOccurred())

	// Fail here rather than in the first spec if the cluster has no Karta CRD, since
	// that means the operator was never installed and every spec would fail alike.
	Expect(k8sClient.List(testCtx, &kartav1alpha1.KartaList{})).To(Succeed(),
		"cannot list Kartas; is the operator installed? run make e2e-up WORKLOADS=none")
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
	return s
}
