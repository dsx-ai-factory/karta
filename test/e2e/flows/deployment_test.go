// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package flows

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/test/e2e/recorder"
)

var _ = Describe("Deployment (built-in)", Ordered, Label("deployment", "builtin"), func() {
	var rec *recorder.Recorder
	var fx recorder.Fixture

	BeforeAll(func(ctx SpecContext) {
		installKarta(ctx, "../../docs/catalog/apps-deployment-v1.yaml", "apps-deployment-v1")
		fx = recorder.Fixture{Operator: "deployment", Version: operatorVersion("deployment"), KartaName: "apps-deployment-v1", KartaFile: "docs/catalog/apps-deployment-v1.yaml"}
		rec = recorder.New(cfg).
			AddState(kartav1alpha1.ProgressingStatus, AllOf(CondTrue("Progressing"), CondFalse("Available"))).
			AddState(kartav1alpha1.RunningStatus, CondReason("Progressing", "True", "NewReplicaSetAvailable")).
			AddState(kartav1alpha1.FailedStatus, CondReason("Progressing", "False", "ProgressDeadlineExceeded"))
	})

	It("scaled", func(ctx SpecContext) {
		out, err := recorder.NewFlow(rec, "scaled", "testdata/deployment/running.yaml").Through(
			recorder.Reaches(kartav1alpha1.ProgressingStatus).Optional(), // startup, before the first Running (Deployment stays Running while scaling)
			recorder.Reaches(kartav1alpha1.RunningStatus).With(ReplicasReady(1)).Do(ScaleReplicas(3)),
			recorder.Reaches(kartav1alpha1.RunningStatus).With(ReplicasReady(3)).Do(ScaleReplicas(1)),
			recorder.Reaches(kartav1alpha1.RunningStatus).With(ReplicasReady(1)),
		).Run(ctx)
		Expect(rec.Save(fx, out)).Error().NotTo(HaveOccurred())
		Expect(err).To(Succeed())
	})

	// Bad image, no progress deadline: Progressing stays True with Available False, read as Progressing.
	It("progressing", func(ctx SpecContext) {
		out, err := recorder.NewFlow(rec, "progressing", "testdata/deployment/progressing.yaml").
			Through(recorder.Reaches(kartav1alpha1.ProgressingStatus)).Run(ctx)
		Expect(rec.Save(fx, out)).Error().NotTo(HaveOccurred())
		Expect(err).To(Succeed())
	})

	// Pinned to a nonexistent node with a 10s progress deadline: Progressing=False/ProgressDeadlineExceeded,
	// read as Failed. It passes through Progressing first.
	It("failed", func(ctx SpecContext) {
		out, err := recorder.NewFlow(rec, "failed", "testdata/deployment/failed.yaml").Through(
			recorder.Reaches(kartav1alpha1.ProgressingStatus),
			recorder.Reaches(kartav1alpha1.FailedStatus),
		).Run(ctx)
		Expect(rec.Save(fx, out)).Error().NotTo(HaveOccurred())
		Expect(err).To(Succeed())
	})
})
