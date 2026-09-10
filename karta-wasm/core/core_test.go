// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package core

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/test/types"
)

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("failed to marshal %T: %v", value, err)
	}
	return string(jsonBytes)
}

func TestDecodeDefinition(t *testing.T) {
	definition, err := DecodeDefinition(mustMarshalJSON(t, types.ReactorKarta()))
	if err != nil {
		t.Fatalf("DecodeDefinition() error = %v", err)
	}
	if definition.Name != "reactor" {
		t.Errorf("expected definition name = %q, got %q", "reactor", definition.Name)
	}
}

func TestDecodeDefinitionRejectsInvalidJSON(t *testing.T) {
	if _, err := DecodeDefinition("not json"); err == nil {
		t.Fatal("expected an error for malformed definition JSON")
	}
}

func TestDecodeWorkload(t *testing.T) {
	workload, err := DecodeWorkload(mustMarshalJSON(t, types.NewReactorObject()))
	if err != nil {
		t.Fatalf("DecodeWorkload() error = %v", err)
	}
	if workload.GetKind() == "" {
		t.Error("expected the decoded workload to carry a kind")
	}
}

func TestDecodeWorkloadRejectsInvalidJSON(t *testing.T) {
	if _, err := DecodeWorkload("not json"); err == nil {
		t.Fatal("expected an error for malformed workload JSON")
	}
}

func TestBuildTree(t *testing.T) {
	workloadTree, err := BuildTree(context.Background(),
		mustMarshalJSON(t, types.ReactorKarta()), mustMarshalJSON(t, types.NewReactorObject()))
	if err != nil {
		t.Fatalf("BuildTree() error = %v", err)
	}
	if workloadTree == nil {
		t.Fatal("expected a tree")
	}
}

func TestEvaluatePhases(t *testing.T) {
	phases, err := EvaluatePhases(context.Background(),
		mustMarshalJSON(t, types.ReactorKarta()), mustMarshalJSON(t, types.NewReactorObject()))
	if err != nil {
		t.Fatalf("EvaluatePhases() error = %v", err)
	}
	if len(phases) != 1 || phases[0] != "Running" {
		t.Fatalf("expected phases = [Running], got %#v", phases)
	}
}

func TestEvaluatePhasesRejectsInvalidJSON(t *testing.T) {
	if _, err := EvaluatePhases(context.Background(), "not json",
		mustMarshalJSON(t, types.NewReactorObject())); err == nil {
		t.Fatal("expected an error for malformed definition JSON")
	}
}

// EvaluatePhases reaches the root status without building the tree, so it
// duplicates what BuildTree does with that status. This pins the two together.
func TestEvaluatePhasesMatchesBuildTree(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		definition *v1alpha1.Karta
		workload   any
	}{
		{"reactor", types.ReactorKarta(), types.NewReactorObject()},
		{"pyflow", types.PyFlowKarta(), types.NewPyFlowObject()},
		{"milvus", types.MilvusKarta(), types.NewMilvusObject()},
		{"jobgroup", types.JobGroupKarta(), types.NewJobGroupObject()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			definitionJSON := mustMarshalJSON(t, testCase.definition)
			workloadJSON := mustMarshalJSON(t, testCase.workload)

			phases, err := EvaluatePhases(context.Background(), definitionJSON, workloadJSON)
			if err != nil {
				t.Fatalf("EvaluatePhases() error = %v", err)
			}

			workloadTree, err := BuildTree(context.Background(), definitionJSON, workloadJSON)
			if err != nil {
				t.Fatalf("BuildTree() error = %v", err)
			}
			var expectedPhases []string
			if workloadTree.Status != nil {
				expectedPhases = workloadTree.Status.Phases
			}

			if !slices.Equal(phases, expectedPhases) {
				t.Errorf("phases = %#v, BuildTree's Status.Phases = %#v", phases, expectedPhases)
			}
		})
	}
}

func TestListCatalog(t *testing.T) {
	if len(ListCatalog()) == 0 {
		t.Error("expected the built-in catalog to be non-empty")
	}
}
