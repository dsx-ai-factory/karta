// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package replay

import (
	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
)

// verdict of one replayed frame against what Karta read.
type verdict int

const (
	verdictPass verdict = iota
	verdictTolerated
	verdictFail
)

// judgeFrame applies the replay tolerance rule. The recorded state among Karta's reads passes. Karta
// reading exactly Undefined on a non-last frame is a tolerated transition dip: operators report some
// windows only through fields being absent, and the reviewers chose not to express absence in the
// catalog. A wrong state anywhere and Undefined on the last frame stay failures - those are the two
// shapes real catalog bugs take, and both were caught by exactly these checks.
func judgeFrame(matched []kartav1alpha1.ResourceStatus, recorded kartav1alpha1.ResourceStatus, last bool) verdict {
	// Undefined is the accessor's no-match fallback and only ever legal alone; a mixed set is a
	// library contract break, never a pass, even when the recorded state is also present.
	if len(matched) > 1 {
		for _, m := range matched {
			if m == kartav1alpha1.UndefinedStatus {
				return verdictFail
			}
		}
	}
	for _, m := range matched {
		if m == recorded {
			return verdictPass
		}
	}
	if !last && len(matched) == 1 && matched[0] == kartav1alpha1.UndefinedStatus {
		return verdictTolerated
	}
	return verdictFail
}
