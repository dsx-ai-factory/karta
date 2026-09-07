// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package generator

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/cli-runtime/pkg/printers"

	"github.com/run-ai/karta/cli/pkg/workload"
)

// ShowAllPods is the --pod-limit default: a hidden pod is the one a reader most
// needs to see.
const ShowAllPods = -1

// fileModeNote marks output built from a manifest that never reached a cluster,
// so an empty status section reads as "not applicable" and not as "healthy".
const fileModeNote = "(file mode: no live status)"

// DescribeOptions controls how one workload is rendered.
type DescribeOptions struct {
	// Output selects the format. The zero value renders the default table, so
	// DescribeOptions{} is usable as-is.
	Output Output
	// PodLimit caps the pod rows per component. Negative shows every pod.
	PodLimit int
}

// RenderWorkload writes one workload to out. The machine formats emit the view
// itself, so what a human reads and what an agent parses cannot drift.
func RenderWorkload(out io.Writer, view *workload.DescribeView, opts DescribeOptions) error {
	format := opts.Output
	if format == "" {
		format = OutputTable
	}

	limit := opts.PodLimit
	if opts.PodLimit == 0 {
		// Zero pod rows hides the whole point of the command; treat the unset
		// int as the default rather than as a request for nothing.
		limit = ShowAllPods
	}

	return RenderOne(out, format, view, func(w io.Writer) error {
		return workloadText(w, view, limit)
	})
}

func workloadText(out io.Writer, view *workload.DescribeView, limit int) error {
	if err := writeHeader(out, view); err != nil {
		return err
	}
	if err := writeTree(out, view, limit); err != nil {
		return err
	}
	if err := writeStatus(out, view); err != nil {
		return err
	}
	return writeResources(out, view)
}

func writeHeader(out io.Writer, view *workload.DescribeView) error {
	fields := []string{
		fmt.Sprintf("%s/%s", view.Kind, view.Name),
		"namespace: " + orNone(view.Namespace),
		fmt.Sprintf("definition: %s (%s)", view.Definition, view.Origin),
	}
	if !view.FileMode {
		fields = append(fields, "age: "+age(time.Now(), view.CreatedAt))
	}

	if _, err := fmt.Fprintln(out, strings.Join(fields, "   ")); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if view.FileMode {
		if _, err := fmt.Fprintln(out, fileModeNote); err != nil {
			return fmt.Errorf("write header: %w", err)
		}
	}
	return nil
}

// writeTree renders the component hierarchy, one row per component and one per
// pod, through a tab writer so every column lines up across both row kinds.
func writeTree(out io.Writer, view *workload.DescribeView, limit int) error {
	if len(view.Components) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return fmt.Errorf("write tree: %w", err)
	}

	writer := printers.GetNewTabWriter(out)
	writeComponents(writer, view.Components, "", limit, view.FileMode)
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("write tree: %w", err)
	}
	return nil
}

func writeComponents(out io.Writer, components []workload.ComponentView, prefix string, limit int, fileMode bool) {
	for i, component := range components {
		last := i == len(components)-1
		fmt.Fprintln(out, row(
			prefix+branch(last)+component.Name,
			scale(component.Replicas, fileMode),
			resourceCell(component.Resources),
			strings.Join(component.Nodes, ","),
		))

		childPrefix := prefix + indent(last)
		writePods(out, component.Pods, childPrefix, limit)
		writeComponents(out, component.Children, childPrefix, limit, fileMode)
	}
}

// row joins cells for the tab writer, dropping the empty ones at the end: a
// trailing tab pads the line with spaces no reader sees but every diff does.
func row(cells ...string) string {
	for len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return strings.Join(cells, "\t")
}

func writePods(out io.Writer, pods []workload.PodView, prefix string, limit int) {
	shown, hidden, unhealthy := limitPods(pods, limit)

	for i, pod := range shown {
		// The truncation line is the last row under this component, so a shown
		// pod is only "last" when nothing follows it.
		last := i == len(shown)-1 && hidden == 0
		fmt.Fprintln(out, row(
			prefix+branch(last)+pod.Name,
			podStatus(pod),
			resourceCell(pod.Resources),
			orNone(deref(pod.Node)),
		))
	}

	if hidden > 0 {
		// No tabs: the note is prose, and a cell of it would widen the name
		// column for every row in the tree.
		fmt.Fprintf(out, "%s%s... and %d more (%d unhealthy shown)\n",
			prefix, branch(true), hidden, unhealthy)
	}
}

// limitPods applies --pod-limit. Unhealthy pods sort first, so truncation can
// never hide the failing pod the reader is looking for.
func limitPods(pods []workload.PodView, limit int) (shown []workload.PodView, hidden, unhealthy int) {
	if limit < 0 || len(pods) <= limit {
		return pods, 0, 0
	}

	ordered := slices.Clone(pods)
	slices.SortStableFunc(ordered, func(a, b workload.PodView) int {
		switch {
		case a.Ready == b.Ready:
			return 0
		case a.Ready:
			return 1
		default:
			return -1
		}
	})

	shown = ordered[:limit]
	for _, pod := range shown {
		if !pod.Ready {
			unhealthy++
		}
	}
	return shown, len(ordered) - limit, unhealthy
}

func writeStatus(out io.Writer, view *workload.DescribeView) error {
	if view.FileMode {
		return nil
	}
	// Several status mappings can match at once, and hiding one would misreport
	// the workload, so every matched phase is named.
	if _, err := fmt.Fprintf(out, "\nPhase: %s\n", strings.Join(view.Phases, ",")); err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	return nil
}

// writeResources breaks the request down per component, with the workload total
// last, so a reader can see which component accounts for the bill.
func writeResources(out io.Writer, view *workload.DescribeView) error {
	if _, err := fmt.Fprintln(out, "\nResources:"); err != nil {
		return fmt.Errorf("write resources: %w", err)
	}

	writer := printers.GetNewTabWriter(out)
	fmt.Fprintln(writer, "COMPONENT\tREPLICAS\tGPU\tCPU\tMEMORY")

	var replicas int32
	for _, component := range leaves(view.Components) {
		replicas += component.Replicas.Desired
		writeResourceRow(writer, component.Name, component.Replicas.Desired, component.Resources)
	}
	writeResourceRow(writer, "TOTAL", replicas, view.Resources)

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("write resources: %w", err)
	}
	return nil
}

func writeResourceRow(out io.Writer, name string, replicas int32, request workload.Resources) {
	fmt.Fprintf(out, "%s\t%d\t%d\t%s\t%s\n",
		name, replicas, request.GPUs, cpu(request.CPUMillis), memory(request.MemoryBytes))
}

// leaves flattens the tree to the components that carry pods. A grouping
// component only repeats the sum of the rows below it.
func leaves(components []workload.ComponentView) []workload.ComponentView {
	var out []workload.ComponentView
	for _, component := range components {
		if len(component.Children) == 0 {
			out = append(out, component)
			continue
		}
		out = append(out, leaves(component.Children)...)
	}
	return out
}

func branch(last bool) string {
	if last {
		return "`-- "
	}
	return "|-- "
}

func indent(last bool) string {
	if last {
		return "    "
	}
	return "|   "
}

// scale reports readiness against the desired count. File mode has no pods, so
// it reports the count alone; "0/9 ready" would read as nine that failed.
func scale(replicas workload.Replicas, fileMode bool) string {
	if fileMode {
		return fmt.Sprintf("replicas: %d", replicas.Desired)
	}
	return fmt.Sprintf("%d/%d ready", replicas.Ready, replicas.Desired)
}

func podStatus(pod workload.PodView) string {
	if pod.Reason == "" {
		return pod.Phase
	}
	return fmt.Sprintf("%s (%s)", pod.Phase, pod.Reason)
}

func resourceCell(request workload.Resources) string {
	if request.GPUs == 0 {
		return ""
	}
	return fmt.Sprintf("gpu: %d", request.GPUs)
}

// cpu renders millicores the way a request is written, so 20000m reads as 20.
func cpu(millis int64) string {
	return resource.NewMilliQuantity(millis, resource.DecimalSI).String()
}

// memory prefers binary units and falls back to decimal, so a request written
// as 70M renders as 70M rather than as a raw byte count.
func memory(bytes int64) string {
	if binary := resource.NewQuantity(bytes, resource.BinarySI).String(); strings.HasSuffix(binary, "i") {
		return binary
	}
	return resource.NewQuantity(bytes, resource.DecimalSI).String()
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func orNone(value string) string {
	if value == "" {
		return "<none>"
	}
	return value
}
