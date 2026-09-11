// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

// Package karta is the consumer-facing API of the Karta library: one small
// interface over a workload object paired with its Karta definition.
//
// Compatibility: PodPatch and the PodField constants grow additively; a field
// is never renamed or repurposed. Capability widening (a patch that errored
// in one release succeeding in a later one) is non-breaking; consumers must
// not depend on a mutation failing. ErrNotSupported is a permanent sentinel.
package karta

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/resource"
	"github.com/run-ai/karta/pkg/tree"
)

// WorkloadTree is the inspection snapshot returned by Workload.Tree.
type WorkloadTree = tree.WorkloadTree

// ComponentNode is one component in the tree.
type ComponentNode = tree.ComponentNode

// InstanceNode is one instance of a component in the tree.
type InstanceNode = tree.InstanceNode

// Workload is one Kubernetes workload object paired with its Karta
// definition - the single front door for consumers. The mutation and
// capability APIs are shape-blind: which of the definition shapes (template,
// split, fragmented) stores the pods is never visible through them. Tree
// retains the legacy shape-specific read model in v1. All mutating methods
// are atomic: on any non-nil error the underlying object is unchanged.
type Workload interface {
	// Tree returns the inspection view: components, instances, scale,
	// status, and extracted pod data.
	Tree(ctx context.Context) (*WorkloadTree, error)

	// UpdatePodTemplate applies a partial pod template update to the named
	// component. It accepts either the typed PodPatch (compile-safe sugar)
	// or a raw Patch (the {metadata, spec} view - ANY field the definition
	// can route, no fixed vocabulary). Both compile to the same merge patch
	// and go through one router. Capability failures list every unroutable
	// path in one typed error, before anything is written.
	UpdatePodTemplate(ctx context.Context, component string, update PodTemplateUpdate, opts ...UpdateOption) error

	// Suspend applies the suspend actions of every component that declares
	// a SuspendDefinition, children before the root. It returns
	// *UnsupportedOperationError when no component declares one.
	Suspend(ctx context.Context) error

	// Resume is the inverse of Suspend, root before the children.
	Resume(ctx context.Context) error

	// Components lists every component of the definition with its
	// statically writable pod fields and whether it is suspendable, the
	// root included. Pure function of the definition.
	Components() []ComponentInfo

	// Object returns a deep copy of the underlying object with all
	// mutations applied, ready to be sent to the cluster. The object is
	// JSON-canonical: every number is a float64, so integers above 2^53
	// lose precision. Typical pod values (ports, replicas, quantities)
	// are unaffected; a CRD carrying larger integers is not supported.
	Object() (resource.KubernetesObject, error)
}

// PodTemplateUpdate is a partial pod template update: the typed PodPatch or a
// raw Patch. Both compile to the same merge patch; one router serves both.
type PodTemplateUpdate interface {
	AsPodMergePatch() Patch
}

// ComponentInfo describes one component's mutation capabilities.
type ComponentInfo struct {
	Name        string
	Root        bool
	PodFields   []PodField
	Suspendable bool
}

// New validates the Karta definition eagerly and wraps the object. A bad
// definition fails here, not at first use.
func New(definition *v1alpha1.Karta, obj resource.KubernetesObject) (Workload, error) {
	if definition == nil {
		return nil, fmt.Errorf("karta: nil definition")
	}
	if obj == nil {
		return nil, fmt.Errorf("karta: nil object")
	}
	if err := v1alpha1.NewKartaValidator(definition).Validate(); err != nil {
		return nil, fmt.Errorf("karta: invalid definition: %w", err)
	}
	definition = definition.DeepCopy()
	copied, ok := obj.DeepCopyObject().(resource.KubernetesObject)
	if !ok {
		return nil, fmt.Errorf("karta: deep copy did not return a kubernetes object")
	}
	root := definition.Spec.StructureDefinition.RootComponent
	if root.Kind != nil {
		gvk := copied.GetObjectKind().GroupVersionKind()
		if gvk.Group != root.Kind.Group || gvk.Version != root.Kind.Version || gvk.Kind != root.Kind.Kind {
			return nil, fmt.Errorf("karta: object is %s/%s %s, definition expects %s/%s %s",
				gvk.Group, gvk.Version, gvk.Kind, root.Kind.Group, root.Kind.Version, root.Kind.Kind)
		}
	}
	if err := validateSuspendActions(definition); err != nil {
		return nil, err
	}
	factory := resource.NewComponentFactoryFromObject(definition, copied)
	if _, err := factory.GetResource(); err != nil {
		return nil, fmt.Errorf("karta: invalid object: %w", err)
	}
	return &workload{karta: definition, factory: factory}, nil
}

// validateSuspendActions makes the eager-validation promise true for suspend:
// every action path must be a writable path and every value valid JSON.
func validateSuspendActions(definition *v1alpha1.Karta) error {
	validate := func(component v1alpha1.ComponentDefinition) error {
		if component.SuspendDefinition == nil {
			return nil
		}
		actions := slices.Concat(
			component.SuspendDefinition.SuspendActions,
			component.SuspendDefinition.ResumeActions)
		for _, action := range actions {
			if !isWritablePath(action.Path) {
				return fmt.Errorf("karta: invalid definition: suspend action path %q on component %s is not a writable path", action.Path, component.Name)
			}
			if !json.Valid([]byte(action.Value)) {
				return fmt.Errorf("karta: invalid definition: suspend action value %q on component %s is not valid JSON", action.Value, component.Name)
			}
		}
		return nil
	}
	if err := validate(definition.Spec.StructureDefinition.RootComponent); err != nil {
		return err
	}
	for _, child := range definition.Spec.StructureDefinition.ChildComponents {
		if err := validate(child); err != nil {
			return err
		}
	}
	return nil
}

type workload struct {
	karta   *v1alpha1.Karta
	factory *resource.ComponentFactory
}

func (w *workload) Tree(ctx context.Context) (*WorkloadTree, error) {
	return tree.Build(ctx, w.factory)
}

func (w *workload) Components() []ComponentInfo {
	return ComponentInfos(w.karta)
}

// ComponentInfos describes every component of a Karta definition: name, root,
// statically writable pod fields and suspendability. Pure function of the
// definition; the real implementation and the kartatest fake share it as the
// one source of discovery truth.
func ComponentInfos(definition *v1alpha1.Karta) []ComponentInfo {
	structure := definition.Spec.StructureDefinition
	infos := make([]ComponentInfo, 0, 1+len(structure.ChildComponents))
	describe := func(component v1alpha1.ComponentDefinition, root bool) ComponentInfo {
		return ComponentInfo{
			Name:        component.Name,
			Root:        root,
			PodFields:   WritablePodFields(component),
			Suspendable: component.SuspendDefinition != nil,
		}
	}
	infos = append(infos, describe(structure.RootComponent, true))
	for _, child := range structure.ChildComponents {
		infos = append(infos, describe(child, false))
	}
	return infos
}

func (w *workload) Object() (resource.KubernetesObject, error) {
	current, err := w.factory.GetResource()
	if err != nil {
		return nil, err
	}
	copied, ok := current.DeepCopyObject().(resource.KubernetesObject)
	if !ok {
		return nil, fmt.Errorf("karta: deep copy did not return a kubernetes object")
	}
	return copied, nil
}

// mutateAtomically runs mutate against a deep copy of the object and keeps
// the result only when mutate returns nil - on any error the workload is
// untouched. This is what makes every mutating method atomic.
func (w *workload) mutateAtomically(mutate func(*resource.ComponentFactory) error) error {
	current, err := w.factory.GetResource()
	if err != nil {
		return fmt.Errorf("karta: get object: %w", err)
	}
	copied, ok := current.DeepCopyObject().(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("karta: unsupported object type %T", current)
	}
	// copied is JSON-primitive: GetResource returns the accessor's converted
	// data and DeepCopyObject preserves the primitive types.
	updated := resource.NewComponentFactoryFromPrimitiveObject(w.karta, copied)
	if err := mutate(updated); err != nil {
		return err
	}
	w.factory = updated
	return nil
}
