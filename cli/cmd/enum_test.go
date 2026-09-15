// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package cmd

import (
	"errors"
	"testing"

	"github.com/dsx-ai-factory/workload-map/cli/pkg/generator"
)

func TestOutputSetValid(t *testing.T) {
	o := NewOutputFlag()
	for _, v := range o.Allowed() {
		if err := o.Set(v); err != nil {
			t.Errorf("Set(%q) returned error: %v", v, err)
		}
		if o.String() != v {
			t.Errorf("after Set(%q), String() = %q", v, o.String())
		}
		if string(o.Get()) != v {
			t.Errorf("after Set(%q), Get() = %q", v, o.Get())
		}
	}
}

func TestOutputSetInvalid(t *testing.T) {
	o := NewOutputFlag()
	err := o.Set("bogus")
	if err == nil {
		t.Fatal("expected error for invalid output value, got nil")
	}
	if !errors.Is(err, ErrInvalidValue) {
		t.Errorf("error is not ErrInvalidValue: %v", err)
	}
	// An invalid Set must not mutate the value away from its default.
	if o.Get() != generator.OutputTable {
		t.Errorf("value changed after failed Set: %q", o.String())
	}
}

func TestOutputType(t *testing.T) {
	if got := NewOutputFlag().Type(); got != "output" {
		t.Errorf("Type() = %q, want %q", got, "output")
	}
}
