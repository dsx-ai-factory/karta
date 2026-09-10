// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package replay

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/resource"
	"github.com/run-ai/karta/test/e2e/recorder"
)

// statePair is one (karta, recorded state) the corpus claims a definition can read.
type statePair struct {
	Karta string
	State kartav1alpha1.ResourceStatus
}

// knownGaps is the reviewed list of pairs the corpus records but no definition matches yet, each with the
// written reason it is acceptable. An entry whose pair starts matching is stale and fails the suite until
// it is removed, so coverage can only be traded away or won back through a visible diff.
var knownGaps = map[statePair]string{
	{Karta: "serving-knative-dev-service-v1", State: kartav1alpha1.InitializingStatus}:             "Ready=Unknown rule deferred by reviewers",
	{Karta: "ray-io-raycluster-v1", State: kartav1alpha1.InitializingStatus}:                       "provisioning is reported only through an absent state field; rule deferred by reviewers",
	{Karta: "serving-kserve-io-inferenceservice-v1beta1", State: kartav1alpha1.InitializingStatus}: "the deploy phase is Ready-undecided, an absence-only window; rule deferred by reviewers",
}

// The tolerance in judgeFrame forgives per-frame dips, so on its own a deleted catalog matcher could turn
// every mid-flow frame of a state into tolerated dips without failing replay. This spec closes that hole:
// every (karta, recorded state) pair appearing in the corpus must be positively matched on at least one
// frame somewhere, unless a knownGaps entry names the reason.
var _ = Describe("Karta covers every recorded state at least once", func() {
	It("matches each (karta, state) pair somewhere in the corpus", func(ctx SpecContext) {
		recordings, _ := filepath.Glob(recordedGlob)
		if len(recordings) == 0 {
			Skip("no recordings under test/e2e/recorded_data")
		}

		seen := map[statePair][]string{}
		witnessed := map[statePair]bool{}
		for _, path := range recordings {
			r, err := recorder.OpenRecording(path)
			Expect(err).NotTo(HaveOccurred())
			karta, err := loadKarta(r.Recording())
			Expect(err).NotTo(HaveOccurred())

			short := strings.TrimPrefix(path, "../recorded_data/")
			for r.Next() {
				state := kartav1alpha1.ResourceStatus(r.State())
				if state == kartav1alpha1.UndefinedStatus {
					continue
				}
				pair := statePair{Karta: r.Recording().KartaName, State: state}
				if len(seen[pair]) == 0 || seen[pair][len(seen[pair])-1] != short {
					seen[pair] = append(seen[pair], short)
				}
				if witnessed[pair] {
					continue
				}
				root, err := resource.NewComponentFactoryFromObject(karta, r.Object()).GetRootComponent()
				Expect(err).NotTo(HaveOccurred())
				status, err := root.GetStatus(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).NotTo(BeNil())
				for _, m := range status.MatchedStatuses {
					if m == pair.State {
						witnessed[pair] = true
						break
					}
				}
			}
		}

		var missing, stale []string
		for pair, fixtures := range seen {
			if witnessed[pair] {
				if reason, listed := knownGaps[pair]; listed {
					stale = append(stale, fmt.Sprintf("%s/%s (%q) now matches; delete the entry", pair.Karta, pair.State, reason))
				}
				continue
			}
			if _, listed := knownGaps[pair]; listed {
				continue
			}
			missing = append(missing, fmt.Sprintf("%s/%s is never matched (recorded in %s)",
				pair.Karta, pair.State, strings.Join(fixtures, ", ")))
		}
		sort.Strings(missing)
		sort.Strings(stale)
		Expect(missing).To(BeEmpty(), "states the corpus records but no definition reads:\n%s", strings.Join(missing, "\n"))
		Expect(stale).To(BeEmpty(), "stale knownGaps entries:\n%s", strings.Join(stale, "\n"))
	})
})
