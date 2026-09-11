// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

// Package main demonstrates how to use Karta to uniformly read and mutate
// distributed training workloads without writing per-CRD integration code.
//
// The same operations run over two completely different CRD types — JobSet and
// LeaderWorkerSet — without any per-type branching.
//
// Usage (from the docs/examples/quickstart directory):
//
//	go run . [flags]
//
// Flags:
//
//	--scheduler name to inject (default: kai-scheduler)
//	--print-mutated  Print the full mutated CRD YAML after injection
//
// Examples:
//
//	go run .
//	go run . --scheduler volcano
//	go run . --scheduler kai-scheduler --print-mutated
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/karta"
	"github.com/run-ai/karta/pkg/tree"
)

// Sample workload objects embedded at compile time.
// In a real controller these come from the Kubernetes API (Reconcile request).

//go:embed jobset.yaml
var jobsetWorkloadYAML []byte

//go:embed lws.yaml
var lwsWorkloadYAML []byte

// workloadExample pairs a sample workload with its Karta definition path.
// Karta definitions live in docs/catalog/ and are read at runtime so they
// always reflect the latest version in the repository.
type workloadExample struct {
	name         string
	kartaPath    string
	workloadYAML []byte
}

// opts carries the parsed CLI flags shared across all workload samples.
type opts struct {
	scheduler    string
	printMutated bool
}

func formatQuantity(q *apiresource.Quantity) string {
	if q == nil || q.IsZero() {
		return "<none>"
	}
	return q.String()
}

// eachComponent visits every (ComponentNode, InstanceNode) pair depth-first,
// including nested children (e.g. LWS leader/worker inside each group instance).
func eachComponent(nodes []tree.ComponentNode, fn func(tree.ComponentNode, tree.InstanceNode)) {
	for _, comp := range nodes {
		for _, inst := range comp.Instances {
			fn(comp, inst)
			eachComponent(inst.Children, fn)
		}
	}
}

func main() {
	o := opts{}
	flag.StringVar(&o.scheduler, "scheduler", "kai-scheduler",
		"scheduler name to inject into all pod-bearing components")
	flag.BoolVar(&o.printMutated, "print-mutated", false,
		"print the full mutated CRD YAML after injection")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: go run ./docs/examples/quickstart [flags]\n\nFlags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  go run ./docs/examples/quickstart\n")
		fmt.Fprintf(os.Stderr, "  go run ./docs/examples/quickstart --scheduler volcano\n")
		fmt.Fprintf(os.Stderr, "  go run ./docs/examples/quickstart --scheduler kai-scheduler --print-mutated\n")
	}
	flag.Parse()

	ctx := context.Background()

	examples := []workloadExample{
		{
			name:         "JobSet",
			kartaPath:    "../../catalog/jobset-x-k8s-io-jobset-v1alpha2.yaml",
			workloadYAML: jobsetWorkloadYAML,
		},
		{
			name:         "LeaderWorkerSet",
			kartaPath:    "../../catalog/leaderworkerset-x-k8s-io-leaderworkerset-v1.yaml",
			workloadYAML: lwsWorkloadYAML,
		},
	}

	for _, ex := range examples {
		fmt.Printf("══════════════════════════════════════════\n")
		fmt.Printf("  %s  (scheduler: %s)\n", ex.name, o.scheduler)
		fmt.Printf("══════════════════════════════════════════\n\n")
		if err := run(ctx, ex, o); err != nil {
			log.Fatalf("%s: %v", ex.name, err)
		}
		fmt.Println()
	}
}

// run executes the Karta operations for a single workload type.
// Notice there is no switch on CRD kind anywhere in this function — the Karta
// definition absorbs all structural differences between workload types.
func run(ctx context.Context, ex workloadExample, o opts) error {
	// Load the Karta definition from docs/catalog/.
	kartaYAML, err := os.ReadFile(ex.kartaPath)
	if err != nil {
		return fmt.Errorf("read Karta definition %s: %w", ex.kartaPath, err)
	}
	definition := &v1alpha1.Karta{}
	if err := yaml.Unmarshal(kartaYAML, definition); err != nil {
		return fmt.Errorf("parse Karta: %w", err)
	}

	// Parse the workload into an unstructured object.
	var rawObj map[string]any
	if err := yaml.Unmarshal(ex.workloadYAML, &rawObj); err != nil {
		return fmt.Errorf("parse workload: %w", err)
	}
	obj := &unstructured.Unstructured{Object: rawObj}

	// Open Karta's front door: one Workload per object, validated eagerly.
	w, err := karta.New(definition, obj)
	if err != nil {
		return fmt.Errorf("new karta workload: %w", err)
	}

	// Build a WorkloadTree for uniform inspection of status, scale, and spec resources.
	wt, err := w.Tree(ctx)
	if err != nil {
		return fmt.Errorf("build workload tree: %w", err)
	}

	// ── Step 1: Read unified status ──────────────────────────────────────────
	fmt.Println("=== Workload status ===")
	status := "<none>"
	if wt.Status != nil {
		status = strings.Join(wt.Status.Phases, ", ")
	}
	fmt.Printf("  Karta workload status: %s\n\n", status)

	// ── Step 2: Inspect replica counts ───────────────────────────────────────
	// Scale values are read from wherever the CRD stores them — a JQ formula
	// for LWS, per replicatedJob entry for JobSet — with no per-type branching.
	fmt.Println("=== Component replica counts ===")
	eachComponent(wt.Children, func(comp tree.ComponentNode, inst tree.InstanceNode) {
		label := comp.Name
		if inst.InstanceKey != nil {
			label = fmt.Sprintf("%s[%s]", comp.Name, *inst.InstanceKey)
		}
		if !comp.HasPodDefinition {
			label += " (virtual)"
		}
		if inst.Scale != nil && inst.Scale.Replicas != nil {
			fmt.Printf("  %-28s replicas=%d\n", label, *inst.Scale.Replicas)
		}
	})
	fmt.Println()

	// ── Step 3: Resource requests per component ─────────────────────────────
	// PodTemplateSpec is extracted from the workload spec via Karta, so container
	// resources are directly accessible — no per-CRD path knowledge required.
	fmt.Println("=== Resource requests per component ===")
	eachComponent(wt.Children, func(comp tree.ComponentNode, inst tree.InstanceNode) {
		if !comp.HasPodDefinition || inst.ExtractedInstance == nil || inst.ExtractedInstance.PodTemplateSpec == nil {
			return
		}
		compLabel := comp.Name
		if inst.InstanceKey != nil {
			compLabel = fmt.Sprintf("%s[%s]", comp.Name, *inst.InstanceKey)
		}
		for _, c := range inst.ExtractedInstance.PodTemplateSpec.Spec.Containers {
			req := c.Resources.Requests
			lim := c.Resources.Limits
			cpu := req.Cpu()
			mem := req.Memory()
			// Extended resources (e.g. GPUs) are often set only in limits;
			// the API server normalises requests=limits, but offline YAML won't.
			gpu := req[corev1.ResourceName("nvidia.com/gpu")]
			if gpu.IsZero() {
				gpu = lim[corev1.ResourceName("nvidia.com/gpu")]
			}
			fmt.Printf("  %-28s container=%-12s cpu=%-8s memory=%-10s gpu=%s\n",
				compLabel, c.Name,
				formatQuantity(cpu),
				formatQuantity(mem),
				formatQuantity(&gpu),
			)
		}
	})
	fmt.Println()

	// ── Step 4: Inject scheduler + label into all pod-bearing components ─────
	// One UpdatePodTemplate call per component states the INTENT (scheduler + label);
	// Karta routes each field to wherever the CRD stores it - full template,
	// bare pod spec, or fragmented paths - without any per-type branching.
	// Components() lists what each component can accept.
	fmt.Printf("=== Injecting scheduler %q + label ===\n", o.scheduler)
	patch := karta.PodPatch{
		SchedulerName: &o.scheduler,
		Labels:        map[string]string{"app.kubernetes.io/managed-by": "karta"},
	}
	for _, info := range w.Components() {
		if info.Root || len(info.PodFields) == 0 {
			continue
		}
		if err := w.UpdatePodTemplate(ctx, info.Name, patch); err != nil {
			return fmt.Errorf("update pods for %s: %w", info.Name, err)
		}
		fmt.Printf("  Injected into %q\n", info.Name)
	}
	fmt.Println()

	// ── Step 5: Verify via Karta read-back ───────────────────────────────────
	// Reading back through a fresh tree confirms both mutations landed at the
	// right paths regardless of where in the CRD structure the template lives.
	fmt.Println("=== Verification ===")
	verified, err := w.Tree(ctx)
	if err != nil {
		return fmt.Errorf("rebuild workload tree: %w", err)
	}
	eachComponent(verified.Children, func(comp tree.ComponentNode, inst tree.InstanceNode) {
		if !comp.HasPodDefinition || inst.ExtractedInstance == nil || inst.ExtractedInstance.PodTemplateSpec == nil {
			return
		}
		compLabel := comp.Name
		if inst.InstanceKey != nil {
			compLabel = fmt.Sprintf("%s[%s]", comp.Name, *inst.InstanceKey)
		}
		pts := inst.ExtractedInstance.PodTemplateSpec
		fmt.Printf("  %-28s schedulerName=%-20q managed-by=%q\n",
			compLabel, pts.Spec.SchedulerName, pts.Labels["app.kubernetes.io/managed-by"])
	})

	// ── Step 5: Retrieve the fully mutated object ─────────────────────────────
	// GetResource returns the modified unstructured object ready for
	//   k8sClient.Update(ctx, updated)
	updated, err := w.Object()
	if err != nil {
		return fmt.Errorf("get updated resource: %w", err)
	}
	fmt.Printf("\n  → In a real controller: k8sClient.Update(ctx, updated)\n")

	// ── Optional: print full mutated CRD YAML ────────────────────────────────
	if o.printMutated {
		mutatedYAML, err := yaml.Marshal(updated.(*unstructured.Unstructured).Object)
		if err != nil {
			return fmt.Errorf("marshal mutated %s: %w", ex.name, err)
		}
		fmt.Printf("\n=== Mutated %s YAML ===\n%s", ex.name, string(mutatedYAML))
	}

	return nil
}
