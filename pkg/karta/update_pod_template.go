// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package karta

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/run-ai/karta/internal/jq/execution"
	"github.com/run-ai/karta/pkg/resource"
)

func (w *workload) UpdatePodTemplate(ctx context.Context, component string, update PodTemplateUpdate, opts ...UpdateOption) error {
	if update == nil {
		return ErrEmptyPatch
	}
	return w.patchPodTemplate(ctx, component, update.AsPodMergePatch(), opts...)
}

// instanceTargets carries the WithInstances resolution: selected is nil when
// the update applies to every instance.
type instanceTargets struct {
	selected map[string]bool
	all      []string // every instance id, in path evaluation order
}

// resolveTargets validates WithInstances: known ids, no duplicates, and the v1
// one-to-one iteration rule between instanceIdPath and every written path.
// Checking each physical route directly (instead of one shape-level base)
// makes per-instance targeting work on any shape whose written paths iterate
// alongside the instance ids - including fragmented ones.
func resolveTargets(ctx context.Context, component *resource.Component, options UpdateOptions, writes []pathWrite) (instanceTargets, error) {
	if len(options.Instances) == 0 {
		return instanceTargets{}, nil
	}
	ids, err := component.GetInstanceIds(ctx)
	if err != nil {
		return instanceTargets{}, fmt.Errorf("karta: get instance ids: %w", err)
	}
	known := make(map[string]bool, len(ids))
	for _, id := range ids {
		if known[id] {
			return instanceTargets{}, fmt.Errorf("karta: duplicate instance id %q on component %s", id, component.Name())
		}
		known[id] = true
	}
	set := make(map[string]bool, len(options.Instances))
	for _, id := range options.Instances {
		if !known[id] {
			return instanceTargets{}, fmt.Errorf("karta: unknown instance id %q for component %s", id, component.Name())
		}
		if set[id] {
			return instanceTargets{}, fmt.Errorf("karta: instance id %q requested twice", id)
		}
		set[id] = true
	}
	definition := component.Definition()
	if definition.InstanceIdPath == nil {
		return instanceTargets{}, fmt.Errorf("karta: WithInstances requires an instanceIdPath on component %s: %w", component.Name(), ErrNotSupported)
	}
	idSegments, ok := parseWritablePath(*definition.InstanceIdPath)
	if !ok || iterCount(idSegments) != 1 {
		return instanceTargets{}, fmt.Errorf("karta: WithInstances requires a writable single-iteration instanceIdPath on component %s: %w", component.Name(), ErrNotSupported)
	}
	for _, write := range writes {
		segments, ok := parseWritablePath(write.path)
		if !ok || !sharesIterationBase(idSegments, segments) {
			return instanceTargets{}, fmt.Errorf("karta: WithInstances requires %s and instanceIdPath to share one iteration base on component %s: %w",
				write.path, component.Name(), ErrNotSupported)
		}
	}
	return instanceTargets{selected: set, all: ids}, nil
}

// pathWrite is one jq assignment at one physical path: read the current
// values, transform each (or keep, when the instance is untargeted), write
// back.
type pathWrite struct {
	path      string
	transform func(current any) (any, error)
}

// applyWrite reads the current values at the write's path, transforms the
// targeted ones and assigns them back in the same evaluation order.
func applyWrite(ctx context.Context, runner execution.Runner, write pathWrite, instances instanceTargets) error {
	current, err := runner.Evaluate(ctx, write.path)
	if err != nil {
		return fmt.Errorf("karta: read %s: %w", write.path, err)
	}
	if len(current) == 0 {
		return fmt.Errorf("karta: path %s matched nothing on the object", write.path)
	}
	if instances.selected != nil && len(current) != len(instances.all) {
		return fmt.Errorf("karta: %s matched %d locations but the component has %d instances: %w",
			write.path, len(current), len(instances.all), ErrNotSupported)
	}
	values := make([]any, len(current))
	for i, value := range current {
		if instances.selected != nil && !instances.selected[instances.all[i]] {
			values[i] = value
			continue
		}
		transformed, err := write.transform(value)
		if err != nil {
			return err
		}
		values[i] = transformed
	}
	if err := runner.AssignZip(ctx, write.path, values); err != nil {
		return fmt.Errorf("karta: write %s: %w", write.path, err)
	}
	return nil
}

// mergeMap merges entries into the current raw map, creating it when
// absent. Untouched keys survive verbatim. Entry values are JSON-primitive
// strings - the strict pod-template decode guarantees it.
func mergeMap(entries map[string]any) func(any) (any, error) {
	return func(current any) (any, error) {
		merged := map[string]any{}
		if existing, ok := current.(map[string]any); ok {
			maps.Copy(merged, existing)
		} else if current != nil {
			return nil, fmt.Errorf("karta: existing value is %T, not an object", current)
		}
		maps.Copy(merged, entries)
		return merged, nil
	}
}

// toUnstructured converts a typed intent value into raw JSON shape. The value is
// consumer-provided intent, so the round-trip loses nothing of the object.
func toUnstructured(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("karta: encode patch value: %w", err)
	}
	var raw any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nil, fmt.Errorf("karta: decode patch value: %w", err)
	}
	return raw, nil
}
