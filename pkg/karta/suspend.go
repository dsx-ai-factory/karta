// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package karta

import (
	"context"
	"fmt"

	"github.com/run-ai/karta/pkg/resource"
)

func (w *workload) Suspend(ctx context.Context) error {
	return w.mutateAtomically(func(factory *resource.ComponentFactory) error {
		components, err := suspendableComponents(factory)
		if err != nil {
			return err
		}
		if len(components) == 0 {
			return &UnsupportedOperationError{Op: "suspend"}
		}
		// Children first, the root last: a suspended root must not orphan
		// still-running children.
		for i := len(components) - 1; i >= 0; i-- {
			if err := components[i].Suspend(ctx); err != nil {
				return fmt.Errorf("karta: suspend %s: %w", components[i].Name(), err)
			}
		}
		return nil
	})
}

func (w *workload) Resume(ctx context.Context) error {
	return w.mutateAtomically(func(factory *resource.ComponentFactory) error {
		components, err := suspendableComponents(factory)
		if err != nil {
			return err
		}
		if len(components) == 0 {
			return &UnsupportedOperationError{Op: "resume"}
		}
		// The root first: children must not start against a suspended root.
		for _, component := range components {
			if err := component.Resume(ctx); err != nil {
				return fmt.Errorf("karta: resume %s: %w", component.Name(), err)
			}
		}
		return nil
	})
}

// suspendableComponents returns the root (when suspendable) followed by the
// suspendable children.
func suspendableComponents(factory *resource.ComponentFactory) ([]*resource.Component, error) {
	var components []*resource.Component
	root, err := factory.GetRootComponent()
	if err != nil {
		return nil, fmt.Errorf("karta: get root component: %w", err)
	}
	if root.HasSuspendDefinition() {
		components = append(components, root)
	}
	children, err := factory.GetChildComponents()
	if err != nil {
		return nil, fmt.Errorf("karta: get child components: %w", err)
	}
	for _, child := range children {
		if child.HasSuspendDefinition() {
			components = append(components, child)
		}
	}
	return components, nil
}
