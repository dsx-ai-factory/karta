// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package replay

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/resource"
	"github.com/run-ai/karta/test/e2e/recorder"
)

const (
	repoRoot     = "../../.."
	recordedGlob = "../recorded_data/*/*/*/*.yaml"
)

// loadKarta reads the catalog definition a recording was captured against.
func loadKarta(rec recorder.Recording) (*kartav1alpha1.Karta, error) {
	name := filepath.Base(rec.KartaFile)
	kartaYAML, err := os.ReadFile(filepath.Join(repoRoot, "docs", "catalog", name))
	if err != nil {
		return nil, err
	}
	karta := &kartav1alpha1.Karta{}
	if err := yaml.Unmarshal(kartaYAML, karta); err != nil {
		return nil, err
	}
	return karta, nil
}

// Each recording is a real flow the recorder captured. Walk it step by step through the recording reader:
// Karta must parse every CR, and the statuses it reads must include the state the recorder observed from
// the CR's own fields. Karta reading exactly Undefined on a non-last frame is a tolerated transition dip,
// reported per fixture; the last frame is always strict, and a wrong state fails anywhere (judgeFrame).
var _ = Describe("Karta reads the recorded state", func() {
	recordings, _ := filepath.Glob(recordedGlob)
	if len(recordings) == 0 {
		It("has recordings to replay", func() {
			Fail("no recordings under test/e2e/recorded_data; run make record-e2e")
		})
		return
	}

	for _, path := range recordings {
		path := path
		It(strings.TrimPrefix(path, "../recorded_data/"), func(ctx SpecContext) {
			r, err := recorder.OpenRecording(path)
			Expect(err).NotTo(HaveOccurred())

			name := filepath.Base(r.Recording().KartaFile)
			Expect(name).To(MatchRegexp(`^[a-zA-Z0-9._-]+\.yaml$`),
				"recording %q names a suspicious KartaFile %q", path, r.Recording().KartaFile)
			Expect(r.Recording().Result.Succeeded).To(BeTrue(),
				"recording %q is not a successful run; re-record it before committing", path)
			Expect(r.Len()).To(BeNumerically(">", 0),
				"recording %q holds no state frames; it proves nothing", path)

			karta, err := loadKarta(r.Recording())
			Expect(err).NotTo(HaveOccurred())

			var tolerated []string
			var lastRecorded kartav1alpha1.ResourceStatus
			for r.Next() {
				state, cr := r.State(), r.Object()
				lastRecorded = kartav1alpha1.ResourceStatus(state)
				root, err := resource.NewComponentFactoryFromObject(karta, cr).GetRootComponent()
				Expect(err).NotTo(HaveOccurred(), "Karta could not parse the %q CR", state)
				status, err := root.GetStatus(ctx)
				Expect(err).NotTo(HaveOccurred(), "Karta could not read the %q CR", state)
				Expect(status).NotTo(BeNil(), "Karta read no status from the %q CR", state)

				last := r.Pos() == r.Len()-1
				switch judgeFrame(status.MatchedStatuses, kartav1alpha1.ResourceStatus(state), last) {
				case verdictTolerated:
					tolerated = append(tolerated, fmt.Sprintf("frame %d/%d: recorded %q, Karta read Undefined",
						r.Pos()+1, r.Len(), state))
				case verdictPass:
				default:
					Fail(fmt.Sprintf("Karta read %v, recorded state was %q (frame %d/%d of %s)",
						status.MatchedStatuses, state, r.Pos()+1, r.Len(), path))
				}
			}
			Expect(lastRecorded).NotTo(Equal(kartav1alpha1.UndefinedStatus),
				"recording %q ends on an Undefined frame; a walk must end on a real state", path)
			if want := r.Recording().Want; want != "" {
				Expect(string(lastRecorded)).To(Equal(want),
					"recording %q ends on %q but its flow declared the terminal %q; the fixture is truncated or edited",
					path, lastRecorded, want)
			}
			if len(tolerated) > 0 {
				AddReportEntry("tolerated undefined dips in "+strings.TrimPrefix(path, "../recorded_data/"),
					strings.Join(tolerated, "\n"), ReportEntryVisibilityAlways)
			}
		})
	}
})
