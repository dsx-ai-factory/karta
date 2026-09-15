// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dsx-ai-factory/workload-map/cli/pkg/generator"
)

// ErrInvalidValue is wrapped by Enum.Set when a value is not one of the allowed
// set, so callers can match it with errors.Is.
var ErrInvalidValue = errors.New("invalid value")

// Enum backs a --flag whose value must be one of a fixed set. It implements
// pflag.Value and works with any ~string type.
type Enum[T ~string] struct {
	val      T
	allowed  []T
	typeName string
}

// NewEnum returns an Enum seeded with def and constrained to allowed. typeName
// is shown for the flag's value in help/usage.
func NewEnum[T ~string](typeName string, def T, allowed ...T) *Enum[T] {
	return &Enum[T]{val: def, allowed: allowed, typeName: typeName}
}

// Get returns the current typed value.
func (e *Enum[T]) Get() T { return e.val }

func (e *Enum[T]) String() string { return string(e.val) }

func (e *Enum[T]) Type() string { return e.typeName }

func (e *Enum[T]) Set(v string) error {
	for _, a := range e.allowed {
		if string(a) == v {
			e.val = a
			return nil
		}
	}
	return fmt.Errorf("%w: must be one of %s", ErrInvalidValue, strings.Join(e.Allowed(), ", "))
}

// Allowed returns the permitted values as strings, for usage text and shell
// completion.
func (e *Enum[T]) Allowed() []string {
	out := make([]string, len(e.allowed))
	for i, a := range e.allowed {
		out[i] = string(a)
	}
	return out
}

// NewOutputFlag returns an Enum backing the -o/--output flag, defaulting to
// table.
func NewOutputFlag() *Enum[generator.Output] {
	return NewEnum("output", generator.OutputTable,
		generator.OutputTable, generator.OutputWide, generator.OutputJSON, generator.OutputYAML)
}
