// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package flows

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/run-ai/karta/test/e2e/recorder"
)

// State predicates: each reads a workload's own fields to recognise one state, never Karta.

// AllOf matches when every check matches, for a state read from more than one condition (a Deployment
// is progressing while Progressing is True and Available is False).
func AllOf(checks ...recorder.StateCheck) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		for _, c := range checks {
			if !c(u) {
				return false
			}
		}
		return len(checks) > 0
	}
}

// CondTrue matches when any of the given condition types is present with status True.
func CondTrue(condTypes ...string) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
		for _, c := range conds {
			m, ok := c.(map[string]any)
			if !ok || m["status"] != "True" {
				continue
			}
			for _, t := range condTypes {
				if m["type"] == t {
					return true
				}
			}
		}
		return false
	}
}

// CondFalse matches when a condition of the given type is present and False.
func CondFalse(condType string) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
		for _, c := range conds {
			m, ok := c.(map[string]any)
			if ok && m["type"] == condType && m["status"] == "False" {
				return true
			}
		}
		return false
	}
}

// CondStatus matches when a condition of condType is present with the given status. Useful for the
// "Unknown" status a workload reports while it is still reconciling (Knative Ready=Unknown while deploying).
func CondStatus(condType, status string) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
		for _, c := range conds {
			if m, ok := c.(map[string]any); ok && m["type"] == condType && m["status"] == status {
				return true
			}
		}
		return false
	}
}

// CondReason matches when the condition of the given type has the given status and reason. A Deployment is
// Running only when Progressing is True with reason NewReplicaSetAvailable, and Failed only when it is False
// with reason ProgressDeadlineExceeded, so type and status alone are not enough.
func CondReason(condType, status, reason string) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
		for _, c := range conds {
			if m, ok := c.(map[string]any); ok && m["type"] == condType && m["status"] == status && m["reason"] == reason {
				return true
			}
		}
		return false
	}
}

// CondsFalse matches when every listed status condition is present and False (the KServe failed mapping:
// PredictorReady, PredictorConfigurationReady, and RoutesReady all False).
func CondsFalse(types ...string) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
		for _, want := range types {
			ok := false
			for _, c := range conds {
				if m, is := c.(map[string]any); is && m["type"] == want && m["status"] == "False" {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
		}
		return len(types) > 0
	}
}

func PhaseEq(want string, path ...string) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		got, _, _ := unstructured.NestedString(u.Object, path...)
		return got == want
	}
}

// PhaseAny matches when the string at the path equals any of wants (some operators map one state from
// several phase strings).
func PhaseAny(wants []string, path ...string) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		got, _, _ := unstructured.NestedString(u.Object, path...)
		for _, w := range wants {
			if got == w {
				return true
			}
		}
		return false
	}
}

func IntAtLeast(n int64, path ...string) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		got, found, err := unstructured.NestedInt64(u.Object, path...)
		return err == nil && found && got >= n
	}
}

// IntEq matches when the integer field at the given path is present and exactly n. It gates a scale flow's
// step for a workload whose readiness count is a single field (Grove reports status.availableReplicas).
func IntEq(n int64, path ...string) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		got, found, err := unstructured.NestedInt64(u.Object, path...)
		return err == nil && found && got == n
	}
}

func BoolTrue(path ...string) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		got, found, err := unstructured.NestedBool(u.Object, path...)
		return err == nil && found && got
	}
}

// Absent matches when the field at the given path is not present (a CronJob that has not scheduled yet
// has no status.lastScheduleTime).
func Absent(path ...string) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		_, found, _ := unstructured.NestedFieldNoCopy(u.Object, path...)
		return !found
	}
}

// ReplicasReady matches a workload settled at exactly n replicas: status.replicas and status.readyReplicas
// both equal n. It gates a scale flow's step so each replica count is captured only once the controller
// has finished scaling to it, not mid-rollout.
func ReplicasReady(n int64) recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		replicas, ok, _ := unstructured.NestedInt64(u.Object, "status", "replicas")
		ready, _, _ := unstructured.NestedInt64(u.Object, "status", "readyReplicas")
		return ok && replicas == n && ready == n
	}
}

// FullyAvailable matches when every desired replica is created and ready (readyReplicas == updatedReplicas
// == spec.replicas), Karta's Running for a StatefulSet. Compares to spec.replicas, not the lagging
// status.replicas, so a gradually-scaled StatefulSet never reads Running mid-ramp.
func FullyAvailable() recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		desired, ok, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
		if !ok {
			desired = 1 // Karta defaults `.spec.replicas // 1`
		}
		ready, _, _ := unstructured.NestedInt64(u.Object, "status", "readyReplicas")
		updated, _, _ := unstructured.NestedInt64(u.Object, "status", "updatedReplicas")
		return desired > 0 && ready == desired && updated == desired
	}
}

// ReplicasDegraded matches a settled-degraded workload: every desired replica created (updatedReplicas ==
// spec.replicas) but some not ready (0 < readyReplicas < spec.replicas).
func ReplicasDegraded() recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		desired, ok, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
		if !ok {
			desired = 1
		}
		ready, _, _ := unstructured.NestedInt64(u.Object, "status", "readyReplicas")
		updated, _, _ := unstructured.NestedInt64(u.Object, "status", "updatedReplicas")
		return desired > 0 && updated == desired && ready > 0 && ready < desired
	}
}

// StatefulSetProgressing matches a StatefulSet rolling toward its spec: nothing ready yet, the controller
// has not observed the spec, or the revisions have not converged.
func StatefulSetProgressing() recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		desired, ok, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
		if !ok {
			desired = 1
		}
		ready, _, _ := unstructured.NestedInt64(u.Object, "status", "readyReplicas")
		observed, _, _ := unstructured.NestedInt64(u.Object, "status", "observedGeneration")
		generation, _, _ := unstructured.NestedInt64(u.Object, "metadata", "generation")
		current, _, _ := unstructured.NestedString(u.Object, "status", "currentRevision")
		update, _, _ := unstructured.NestedString(u.Object, "status", "updateRevision")
		return desired > 0 && (ready == 0 || observed != generation || current != update)
	}
}

// AllReplicasAvailable matches when every desired replica is available (status.availableReplicas >=
// spec.replicas), Karta's Running for a Grove PodCliqueSet. Includes the vacuous spec.replicas == 0 case.
func AllReplicasAvailable() recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		desired, _, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
		avail, _, _ := unstructured.NestedInt64(u.Object, "status", "availableReplicas")
		return avail >= desired
	}
}

// ReplicasComingUp is the progressing counterpart of AllReplicasAvailable: spec.replicas > 0 and not every
// desired replica is available yet (status.availableReplicas < spec.replicas).
func ReplicasComingUp() recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		desired, _, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
		avail, _, _ := unstructured.NestedInt64(u.Object, "status", "availableReplicas")
		return desired > 0 && avail < desired
	}
}

// CronjobRunning matches a fired, enabled CronJob (lastScheduleTime set and spec.suspend not true),
// mirroring the catalog's running matcher.
func CronjobRunning() recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		_, scheduled, _ := unstructured.NestedFieldNoCopy(u.Object, "status", "lastScheduleTime")
		suspended, _, _ := unstructured.NestedBool(u.Object, "spec", "suspend")
		return scheduled && !suspended
	}
}

// JobDegraded matches a parallel Job settled degraded: parallelism > 1, some but not all pods ready, and
// at least one pod already succeeded or failed - the Job analog of ReplicasDegraded.
func JobDegraded() recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		par, ok, _ := unstructured.NestedInt64(u.Object, "spec", "parallelism")
		ready, _, _ := unstructured.NestedInt64(u.Object, "status", "ready")
		succeeded, _, _ := unstructured.NestedInt64(u.Object, "status", "succeeded")
		failedN, _, _ := unstructured.NestedInt64(u.Object, "status", "failed")
		return ok && par > 1 && ready > 0 && ready < par && (succeeded > 0 || failedN > 0)
	}
}

// JobsetRunning matches a JobSet with working pods: at least one replicatedJob has active or ready pods and
// none have failed. Reading either count (not both) keeps the state stable while the controller briefly
// flaps ready to 0 mid-run.
func JobsetRunning() recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		rjs, _, _ := unstructured.NestedSlice(u.Object, "status", "replicatedJobsStatus")
		anyWorking := false
		for _, r := range rjs {
			m, ok := r.(map[string]any)
			if !ok {
				continue
			}
			if failed, _, _ := unstructured.NestedInt64(m, "failed"); failed > 0 {
				return false
			}
			ready, _, _ := unstructured.NestedInt64(m, "ready")
			active, _, _ := unstructured.NestedInt64(m, "active")
			if ready > 0 || active > 0 {
				anyWorking = true
			}
		}
		return anyWorking
	}
}

// JobsetProgressing matches a JobSet whose jobs have active pods but none ready yet: the window between
// the jobs starting and the first pod coming up.
func JobsetProgressing() recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		rjs, _, _ := unstructured.NestedSlice(u.Object, "status", "replicatedJobsStatus")
		anyActive := false
		for _, r := range rjs {
			m, ok := r.(map[string]any)
			if !ok {
				continue
			}
			if ready, _, _ := unstructured.NestedInt64(m, "ready"); ready > 0 {
				return false
			}
			if active, _, _ := unstructured.NestedInt64(m, "active"); active > 0 {
				anyActive = true
			}
		}
		return anyActive
	}
}

// RaySuspended matches a RayCluster suspended at creation: spec.suspend is true and status.state is either
// "suspended" or not yet set (the operator may not have written a state yet).
func RaySuspended() recorder.StateCheck {
	return func(u *unstructured.Unstructured) bool {
		if suspend, _, _ := unstructured.NestedBool(u.Object, "spec", "suspend"); !suspend {
			return false
		}
		state, found, _ := unstructured.NestedString(u.Object, "status", "state")
		return !found || state == "" || state == "suspended"
	}
}
