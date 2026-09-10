// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package replay

import (
	"testing"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
)

func TestJudgeFrame(t *testing.T) {
	undef := kartav1alpha1.UndefinedStatus
	initializing := kartav1alpha1.InitializingStatus
	running := kartav1alpha1.RunningStatus

	cases := []struct {
		name     string
		matched  []kartav1alpha1.ResourceStatus
		recorded kartav1alpha1.ResourceStatus
		last     bool
		want     verdict
	}{
		{"recorded state among reads", []kartav1alpha1.ResourceStatus{running}, running, false, verdictPass},
		{"recorded state among overlapping reads", []kartav1alpha1.ResourceStatus{running, initializing}, initializing, true, verdictPass},
		{"recorder-labeled undefined frame still matches", []kartav1alpha1.ResourceStatus{undef}, undef, true, verdictPass},
		{"undefined dip mid-flow is tolerated", []kartav1alpha1.ResourceStatus{undef}, initializing, false, verdictTolerated},
		{"terminal shrug fails", []kartav1alpha1.ResourceStatus{undef}, kartav1alpha1.CompletedStatus, true, verdictFail},
		{"one-frame fixture is entirely strict", []kartav1alpha1.ResourceStatus{undef}, initializing, true, verdictFail},
		{"wrong state fails mid-flow", []kartav1alpha1.ResourceStatus{initializing}, running, false, verdictFail},
		{"wrong state fails on the last frame", []kartav1alpha1.ResourceStatus{initializing}, running, true, verdictFail},
		{"mixed undefined set is not the tolerance shape", []kartav1alpha1.ResourceStatus{undef, running}, initializing, false, verdictFail},
		{"mixed undefined set fails even when the recorded state is present", []kartav1alpha1.ResourceStatus{undef, running}, running, false, verdictFail},
		{"mixed undefined set fails even when recorded is undefined", []kartav1alpha1.ResourceStatus{undef, running}, undef, true, verdictFail},
		{"empty reads fail", nil, initializing, false, verdictFail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := judgeFrame(c.matched, c.recorded, c.last); got != c.want {
				t.Fatalf("judgeFrame(%v, %q, last=%v) = %v, want %v", c.matched, c.recorded, c.last, got, c.want)
			}
		})
	}
}
