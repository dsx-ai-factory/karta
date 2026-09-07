// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package workload

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// maxOwnerDepth bounds the walk, which is what ends a cycle in malformed data.
// Real chains are two or three hops.
const maxOwnerDepth = 6

// podsGVR is the fixed core/v1 Pod resource, which discovery does not need to map.
var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

// PodAttributor narrows a namespace of pods to the one workload that owns them.
// A Karta PodSelector names a component type, never a workload.
type PodAttributor struct {
	dyn    dynamic.Interface
	mapper meta.RESTMapper
	cache  map[ownerKey]*unstructured.Unstructured
}

type ownerKey struct {
	gvk       schema.GroupVersionKind
	namespace string
	name      string
}

func NewPodAttributor(dyn dynamic.Interface, mapper meta.RESTMapper) *PodAttributor {
	return &PodAttributor{dyn: dyn, mapper: mapper, cache: map[ownerKey]*unstructured.Unstructured{}}
}

// Filter returns the pods whose owner-reference chain reaches rootUID, decoding
// only the matches. pods may span namespaces, as in a cluster-wide search.
func (a *PodAttributor) Filter(
	ctx context.Context, pods []unstructured.Unstructured, rootUID types.UID,
) ([]corev1.Pod, error) {
	var matched []corev1.Pod
	for i := range pods {
		if !a.belongsTo(ctx, pods[i].GetOwnerReferences(), pods[i].GetNamespace(), rootUID, 0) {
			continue
		}

		var pod corev1.Pod
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(pods[i].Object, &pod); err != nil {
			return nil, fmt.Errorf("decode pod %s: %w", pods[i].GetName(), err)
		}
		matched = append(matched, pod)
	}
	return matched, nil
}

// belongsTo reports whether any owner in refs is, or transitively leads to,
// rootUID.
func (a *PodAttributor) belongsTo(
	ctx context.Context, refs []metav1.OwnerReference, namespace string, rootUID types.UID, depth int,
) bool {
	if depth >= maxOwnerDepth {
		return false
	}
	for _, ref := range refs {
		if ref.UID == rootUID {
			return true
		}
	}
	controller := controllerRef(refs)
	if controller == nil {
		return false
	}
	owner, err := a.get(ctx, *controller, namespace)
	if err != nil || owner == nil {
		return false
	}
	return a.belongsTo(ctx, owner.GetOwnerReferences(), namespace, rootUID, depth+1)
}

// controllerRef returns the single owner reference with Controller set, the
// only one an owner chain can be walked through unambiguously.
func controllerRef(refs []metav1.OwnerReference) *metav1.OwnerReference {
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller {
			return &refs[i]
		}
	}
	return nil
}

func (a *PodAttributor) get(
	ctx context.Context, ref metav1.OwnerReference, namespace string,
) (*unstructured.Unstructured, error) {
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return nil, fmt.Errorf("parse owner apiVersion %q: %w", ref.APIVersion, err)
	}
	gvk := gv.WithKind(ref.Kind)
	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("discover %s: %w", gvk.Kind, err)
	}

	// A cluster-scoped owner is not addressed by namespace, and a namespaced
	// request for one is a 404 that would drop the pod from its workload.
	client := dynamic.ResourceInterface(a.dyn.Resource(mapping.Resource))
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		client = a.dyn.Resource(mapping.Resource).Namespace(namespace)
	} else {
		namespace = ""
	}

	key := ownerKey{gvk: gvk, namespace: namespace, name: ref.Name}
	if cached, ok := a.cache[key]; ok {
		return cached, nil
	}

	obj, err := client.Get(ctx, ref.Name, metav1.GetOptions{})
	// Cache the miss too, so a broken chain is not re-fetched once per pod.
	a.cache[key] = obj
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// ListPods reads every pod in namespace once, left undecoded for Filter to
// narrow. An empty namespace lists cluster-wide.
func ListPods(ctx context.Context, dyn dynamic.Interface, namespace string) ([]unstructured.Unstructured, error) {
	list, err := dyn.Resource(podsGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}
