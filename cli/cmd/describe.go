// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	"github.com/run-ai/karta/cli/pkg/definitions"
	"github.com/run-ai/karta/cli/pkg/generator"
	"github.com/run-ai/karta/cli/pkg/workload"
	"github.com/run-ai/karta/pkg/catalog"
)

const (
	flagPodLimit = "pod-limit"
	flagFile     = "file"

	usagePodLimit = "Maximum pod rows per component; the default shows every pod. " +
		"When set, unhealthy pods are shown first"
	usageFile = "Describe a manifest that has not been submitted; \"-\" reads stdin. " +
		"No cluster is needed, and no TYPE/NAME is accepted"

	describeUse   = "describe TYPE[/NAME] [NAME]"
	describeShort = "Show one workload in full"

	describeLong = `Show one workload as Karta reads it through the definition covering its type:
the component tree with live pods attributed to the role they play, the
normalized phase, and the requested resources per component.

Type matching is lenient: case-insensitive, singular or plural, and kubectl short
names all resolve.

Pods are attributed by ownership, so only the pods of this workload are shown, and
by the definition's own pod selectors, so a pod lands under the component whose
role it plays rather than under the object that happens to own it.

With -f, the same view is built from a manifest alone, with no cluster and no
pods: the structure and the desired scale are real, everything live is absent.
It answers what a workload would look like before it is submitted.`

	describeExample = `  # Describe a workload, kubectl-style
  kli describe pytorchjob/llama-finetune

  # Two-token form, also kubectl-style
  kli describe pytorchjob llama-finetune

  # Large workloads: cap the pod rows, unhealthy pods first
  kli describe pytorchjob/llama-finetune --pod-limit 10

  # Validate a manifest before submitting it, no cluster needed
  kli describe -f jobset.yaml

  # Machine output for scripting or agents
  kli describe pytorchjob/llama-finetune -o json`
)

// describeOptions holds one run's inputs. Embedding getOptions is what makes
// describe accept the same TYPE/NAME forms as get.
type describeOptions struct {
	getOptions
	podLimit int
	file     string
}

// newDescribeCommand builds the "kli describe" command: one workload in full.
func newDescribeCommand() *cobra.Command {
	opts := &describeOptions{podLimit: generator.ShowAllPods}
	var output *Enum[generator.Output]

	cmd := &cobra.Command{
		Use:     describeUse,
		Short:   describeShort,
		Long:    describeLong,
		Example: describeExample,
		// The range applies to the positional form only: -f reads the kind from
		// the manifest, so it takes no arguments at all.
		Args: usageArgs(func(cmd *cobra.Command, args []string) error {
			if opts.file != "" {
				if len(args) > 0 {
					return fmt.Errorf(
						"--%s reads the kind from the manifest, so it cannot be combined with %q",
						flagFile, strings.Join(args, " "))
				}
				return nil
			}
			if err := cobra.RangeArgs(1, 2)(cmd, args); err != nil {
				return err
			}
			if err := parseArgs(&opts.getOptions, args); err != nil {
				return err
			}
			if opts.name == "" {
				return fmt.Errorf(
					"a NAME is required: give it as %s or as %s", "TYPE/NAME", "TYPE NAME")
			}
			return nil
		}),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDescribe(cmd, opts, output.Get())
		},
	}

	// A single workload renders no extra columns, so wide is rejected at parse
	// time rather than silently treated as the table.
	output = withOutput(cmd, cmd.Flags(), false)
	cmd.Flags().IntVar(&opts.podLimit, flagPodLimit, generator.ShowAllPods, usagePodLimit)
	cmd.Flags().StringVarP(&opts.file, flagFile, "f", "", usageFile)

	return cmd
}

func runDescribe(cmd *cobra.Command, opts *describeOptions, format generator.Output) error {
	ctx := cmd.Context()
	access := clusterAccess()

	// Read best-effort: an unreachable cluster degrades to the embedded catalog,
	// which is what lets file mode work with no cluster at all.
	resolver, warnings := loadDefinitions(ctx, access)
	if err := printWarnings(cmd.ErrOrStderr(), warningMessages(warnings)); err != nil {
		return err
	}

	view, err := resolveView(ctx, cmd, opts, resolver, access)
	if err != nil {
		return reportNoDefinition(cmd, format, err)
	}

	return generator.RenderWorkload(cmd.OutOrStdout(), view, generator.DescribeOptions{
		Output:   format,
		PodLimit: opts.podLimit,
	})
}

func resolveView(
	ctx context.Context,
	cmd *cobra.Command,
	opts *describeOptions,
	resolver *definitions.Resolver,
	access genericclioptions.RESTClientGetter,
) (*workload.DescribeView, error) {
	if opts.file != "" {
		return describeManifest(ctx, cmd, opts, resolver)
	}
	return describeLive(ctx, opts, resolver, access)
}

// describeLive reads the named workload and its pods from the cluster.
func describeLive(
	ctx context.Context,
	opts *describeOptions,
	resolver *definitions.Resolver,
	access genericclioptions.RESTClientGetter,
) (*workload.DescribeView, error) {
	namespace, _, err := ResolvedNamespace(access)
	if err != nil {
		return nil, fmt.Errorf("resolve namespace: %w", err)
	}

	mapper, err := access.ToRESTMapper()
	if err != nil {
		return nil, fmt.Errorf("kubernetes discovery: %w", err)
	}
	dyn, err := newDynamicClient(access)
	if err != nil {
		return nil, err
	}

	target, err := resolveTarget(&opts.getOptions, resolver, mapper)
	if err != nil {
		return nil, err
	}

	obj, err := getOne(ctx, dyn, mapper, target, namespace, opts.name)
	if err != nil {
		return nil, err
	}

	// Pods are read from the object's own namespace: a workload's pods are
	// created beside it, and the request must not widen when -n was omitted.
	pods, err := workload.ListPods(ctx, dyn, obj.GetNamespace())
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	mine, err := workload.NewPodAttributor(dyn, mapper).Filter(ctx, pods, obj.GetUID())
	if err != nil {
		return nil, fmt.Errorf("attribute pods: %w", err)
	}

	view, err := workload.ResolveDescribe(ctx, obj, target, mine)
	if err != nil {
		return nil, fmt.Errorf("describe %s %q: %w", obj.GetKind(), obj.GetName(), err)
	}
	return view, nil
}

// describeManifest builds the view from a manifest alone. The kind comes from
// the manifest, so no discovery is involved and no cluster is required.
func describeManifest(
	ctx context.Context, cmd *cobra.Command, opts *describeOptions, resolver *definitions.Resolver,
) (*workload.DescribeView, error) {
	obj, err := readManifest(cmd, opts.file)
	if err != nil {
		return nil, err
	}

	gvk := obj.GroupVersionKind()
	if gvk.Kind == "" || gvk.Version == "" {
		return nil, usageError(cmd, fmt.Errorf(
			"%s declares no apiVersion and kind, so there is nothing to resolve it by", opts.file))
	}

	target, err := resolver.Resolve(gvk)
	switch {
	case err == nil:
	case errors.Is(err, definitions.ErrAmbiguous):
		return nil, exitError{code: ExitUsage, err: err}
	default:
		return nil, noDefinitionFor(gvk)
	}

	// No pods: a manifest that never reached the cluster has none, and inventing
	// zeroes would read as a workload whose pods have all gone.
	view, err := workload.ResolveDescribe(ctx, obj, target, nil)
	if err != nil {
		return nil, fmt.Errorf("describe %s: %w", opts.file, err)
	}
	view.FileMode = true
	return view, nil
}

// readManifest decodes one workload manifest, from stdin when path is "-".
func readManifest(cmd *cobra.Command, path string) (*unstructured.Unstructured, error) {
	var (
		raw []byte
		err error
	)
	if path == "-" {
		raw, err = io.ReadAll(cmd.InOrStdin())
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	// A plain map, not Unstructured: its unmarshal rejects a missing kind with a
	// message about JSON internals rather than the field that is absent.
	var fields map[string]any
	if err := yaml.Unmarshal(raw, &fields); err != nil {
		return nil, usageError(cmd, fmt.Errorf("parse %s: %w", path, err))
	}
	return &unstructured.Unstructured{Object: fields}, nil
}

// machineError is the shape a failure takes in the machine formats. Only the
// no-definition case uses it, being the one an agent handles differently.
type machineError struct {
	Error   string `json:"error"`
	GVK     string `json:"gvk,omitempty"`
	Type    string `json:"type,omitempty"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

const (
	noDefinitionReason  = "no_definition_for_type"
	noDefinitionMessage = "no Karta definition covers this workload type"
	noDefinitionHint    = `"kli definitions" lists the types Karta covers; ` +
		"apply a Karta definition to the cluster to cover this one"
)

// noDefinitionNotFound marks a failure reportNoDefinition should also emit as a
// machine error, carrying the identity the payload names.
type noDefinitionNotFound struct {
	exitError
	subject machineError
}

func noDefinitionFor(gvk schema.GroupVersionKind) error {
	return noDefinitionNotFound{
		exitError: exitError{code: ExitNotFound,
			err: fmt.Errorf("%s: %s", noDefinitionMessage, gvk)},
		subject: machineError{
			Error: noDefinitionReason, GVK: gvk.String(),
			Message: noDefinitionMessage, Hint: noDefinitionHint,
		},
	}
}

// reportNoDefinition puts the machine error on stdout, where an agent reads,
// and leaves the human message on stderr, where a reader does.
func reportNoDefinition(cmd *cobra.Command, format generator.Output, err error) error {
	if format != generator.OutputJSON && format != generator.OutputYAML {
		return err
	}

	var structured noDefinitionNotFound
	if !errors.As(err, &structured) {
		// A type token that matched no definition carries no GVK to name, so
		// the payload names the token the caller used instead.
		var coded exitError
		if !errors.As(err, &coded) || coded.code != ExitNotFound {
			return err
		}
		structured.subject = machineError{
			Error: noDefinitionReason, Type: coded.Error(),
			Message: noDefinitionMessage, Hint: noDefinitionHint,
		}
	}

	if writeErr := generator.RenderOne(cmd.OutOrStdout(), format, structured.subject,
		func(io.Writer) error { return nil }); writeErr != nil {
		return writeErr
	}
	return err
}

// getOne fetches the single named object of target's type, reporting a miss the
// way get reports one so a script sees the same code either way.
func getOne(
	ctx context.Context,
	dyn dynamic.Interface,
	mapper meta.RESTMapper,
	target definitions.Definition,
	namespace, name string,
) (*unstructured.Unstructured, error) {
	gvk := catalog.RootKey(target.Karta)

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	switch {
	case err == nil:
	case meta.IsNoMatchError(err):
		// The type is absent, so the named workload cannot exist.
		return nil, exitError{code: ExitWorkloadNotFound,
			err: fmt.Errorf("%s is not installed in this cluster", gvk.Kind)}
	default:
		return nil, fmt.Errorf("discover %s: %w", gvk.Kind, err)
	}

	// A cluster-scoped root is not addressed by namespace.
	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		namespace = ""
	}

	obj, err := dyn.Resource(mapping.Resource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	switch {
	case err == nil:
		return obj, nil
	case apierrors.IsNotFound(err):
		return nil, exitError{code: ExitWorkloadNotFound,
			err: fmt.Errorf("%s %q not found%s", gvk.Kind, name, inNamespace(namespace))}
	case apierrors.IsForbidden(err):
		return nil, exitError{code: ExitError,
			err: fmt.Errorf("not allowed to read %s: %w", gvk.Kind, err)}
	default:
		return nil, exitError{code: ExitError,
			err: fmt.Errorf("get %s %q: %w", gvk.Kind, name, err)}
	}
}
