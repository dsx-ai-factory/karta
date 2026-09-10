# Karta CLI - High-Level Design

## Background

Karta is a CRD-based Go library that provides a universal abstraction for any Kubernetes workload type. Using JQ-based Karta definitions, it can extract structure (components, hierarchy), status (phase, conditions), scaling (replicas, min/max), and pod specs from any CRD - PyTorchJob, RayCluster, JobSet, KServe, and others.

We believe Karta's abstraction layer can serve as the foundation for a tool that brings visibility to the k8s workload space - a CLI/web/MCP that understands any workload type out of the box for a live cluster.

### What exists today for workload visibility

- [**kubectl**](https://kubernetes.io/docs/reference/kubectl/) - works per resource type. `kubectl get pytorchjob` shows the CRD, `kubectl get pods` shows a flat list. No connection between them. Each CRD type requires different knowledge to inspect.
- [**k9s**](https://k9scli.io/) / [**Lens**](https://k8slens.dev/) - general-purpose cluster browsers. They show resources by kind but have no concept of a "workload" as a composed unit of multiple resource types.
- [**kubectl-tree**](https://github.com/ahmetb/kubectl-tree) - shows the ownership chain (Pod → ReplicaSet → Deployment) but has no semantic understanding. It can't tell you which pod is a master and which is a worker. No phase or status parsing - it doesn't understand the workload's lifecycle state. No scaling awareness - it shows individual objects but doesn't understand replicas, min/max, or autoscaling.

### What's missing

No existing tool provides **workload-aware** visibility across different CRD types. The questions that keep coming up and we think that Karta can answer:

- What workloads are running in my cluster, across all types, and what's their status?
- What does the structure of a workload look like - which pods play which role (master, worker, head)?
- How many GPUs does a workload consume across all its components?
- How are the components of my inference pipeline performing - which are scaling, which are bottlenecked?

Every workload type (PyTorchJob, RayCluster, JobSet, KServe) structures this information differently. Today, answering these questions requires per-CRD knowledge and manual inspection.

Karta already has the abstraction layer to read any workload type uniformly. A CLI (and later web/MCP) can build on this to provide the missing visibility.

### Side-by-side: kubectl-tree vs karta

Consider a `DynamoGraphDeployment` with Frontend, PrefillWorker, and DecodeWorker roles.

**kubectl-tree** - ownership walk:

```shell
$ kubectl tree dynamographdeployment my-pipeline
NAMESPACE  NAME                                               READY
ml-team    DynamoGraphDeployment/my-pipeline                  -
ml-team    ├─DynamoComponentDeployment/my-pipeline-frontend   -
ml-team    │ └─LeaderWorkerSet/my-pipeline-frontend           -
ml-team    │   ├─Pod/my-pipeline-frontend-0-0                 True
ml-team    │   └─Pod/my-pipeline-frontend-0-1                 True
ml-team    ├─DynamoComponentDeployment/my-pipeline-prefill    -
ml-team    │ └─LeaderWorkerSet/my-pipeline-prefill            -
ml-team    │   ├─Pod/my-pipeline-prefill-0-0                  True
...
```

**karta workload tree** - semantic walk:

```shell
$ karta workload tree my-pipeline
DynamoGraphDeployment/my-pipeline [Running]
└── service
    ├── Frontend       (2/2 replicas)   2/2 ready
    ├── PrefillWorker  (3/4 replicas)   3/3 ready
    └── DecodeWorker   (4/4 replicas)   4/4 ready
```

What Karta brings on top, all derived from the Karta definition:

- **Semantic role names** - Frontend / PrefillWorker / DecodeWorker come from the label mapping declared in the Karta YAML.
- **Pods grouped by their role, not by their owner** - pods playing different roles are shown as separate groups even when they share the same owning resource.
- **Normalized phase** - `Running` has the same meaning across PyTorchJob, RayCluster, Dynamo, etc.
- **Desired vs current replicas** - `3/4 replicas` shows the scale target vs. the actual pod count, per role.
- **Collapsed plumbing** - intermediate objects (DynamoComponentDeployment, LeaderWorkerSet) are optionally hidden to reduce noise while keeping the semantic hierarchy visible.

**kubectl-tree walks ownership. Karta walks semantics.**

---

## Karta definition resolution

We want the CLI to be useful immediately - without requiring users to install Karta CRDs into their cluster first. If someone has PyTorchJobs running, `karta get pytorchjob` should find them out of the box.

To make this work, the CLI would ship with **community definitions** - the Karta definitions from `docs/examples/` embedded in the binary. These are maintained by the open-source community, cover standard frameworks (PyTorchJob, RayCluster, JobSet, KServe, etc.), and are tested against each framework's stable API. They update automatically when you upgrade the CLI.

For users who need more - custom CRDs, org-specific status mappings, different pod selectors - they can apply Karta definitions to the cluster as CRDs. These **cluster definitions** take precedence over community ones when both target the same workload type.

The `ORIGIN` column in `karta definition list` shows where each definition came from (`community` or `cluster`).

---

## High-level design

### Command structure

Two top-level nouns:

```shell
karta workload ...        # operational: what's running in my cluster
karta definition ...      # meta: what workload types does Karta understand
```

### Namespace behavior

- Karta definitions are **cluster-scoped**. Community definitions (embedded in the binary) and cluster definitions (CRDs) both apply across all namespaces.
- Workloads are **namespaced**. All `karta workload *` commands follow standard kubectl conventions:
  - `-n <namespace>` targets a specific namespace
  - `-A, --all-namespaces` scans all namespaces
  - Default is the namespace from the current kubeconfig context
- `karta workload tree|status|resources <name>` operates on a single workload. The namespace is taken from `-n` or the current context. If the same name exists in multiple namespaces, the command errors and asks the user to specify `-n`.
- Pod discovery for tree building happens in the workload's namespace - pods outside it are not considered.

### `karta get`

List workloads of a type, resolved through the Karta definition that covers it. The type is a positional token, following the verb-first kubectl grammar (`karta VERB TYPE[/NAME]`).

```shell
$ karta get pytorchjob -n ml-team
NAME              NAMESPACE   PHASE       COMPONENTS                               GPU   AGE
llama-finetune    ml-team     Running     master(1), worker(4)                     33    2h
sweep-7           ml-team     Degraded    master(1), worker(8)                     64    30m
```

`karta get jobset/preprocess` fetches one row. Type matching is lenient: case-insensitive, singular or plural, and kubectl short names all resolve.

Key flags:
- `--phase <Running|Failed|...>` - filter by normalized phase, repeatable
- `-l, --selector <labels>` - filter by labels (same syntax as `kubectl`)
- `--chunk-size <n>` - API list page size
- `-o <table|wide|json|yaml>` - output format. `-o json` emits the typed `WorkloadView` for scripting and MCP consumers, always under an `items` key alongside a `count`, so consumers never branch on shape.

Rows are ordered newest first. Counts and GPU are read from the workload spec, so listing costs one API call per type and no pod reads; `wide` adds the ORIGIN of the resolving definition. A NODES column needs live pod data and follows with `karta describe`, which has to build pod matching anyway.

The cross-type view - every workload Karta covers, in one table, which is the view no native tool provides - is the next step for this command. It needs a rule for which objects are workload roots: the catalog covers Deployment and Pod but not ReplicaSet, so a single-level owner check either floods the table with a Deployment's pods or hides workloads whose controller Karta does not cover.

Clusters can be huge - thousands of workloads across many types is a realistic scenario for our users. `karta get` pages through large result sets so per-request API pressure stays bounded. Note that `--chunk-size` bounds neither total memory nor time-to-first-row: every page is retained, and the default ordering is global, so the whole result is sorted before the first row is rendered.

### `karta workload tree <name>`

Hierarchical tree view: workload -> components -> instances -> pods. Each component shows its readiness and resource usage inline, so operators can see the state of the workload at a glance without running additional commands.

Simple workload (PyTorchJob):

```shell
$ karta workload tree llama-finetune
PyTorchJob/llama-finetune [Running]
├── master   (1/1 replicas)   1/1 ready   gpu: 1    nodes: node-01
│   └── Pod/llama-finetune-master-0    Running   gpu: 1   node-01
└── worker   (4/4 replicas)   3/4 ready   gpu: 32   nodes: node-02,03,04
    ├── Pod/llama-finetune-worker-0    Running   gpu: 8   node-02
    ├── Pod/llama-finetune-worker-1    Running   gpu: 8   node-03
    ├── Pod/llama-finetune-worker-2    Running   gpu: 8   node-04
    └── Pod/llama-finetune-worker-3    Pending   gpu: 8   <none>
```

Complex workload (Dynamo - multi-instance with nested children):

```shell
$ karta workload tree my-dynamo-graph
DynamoGraphDeployment/my-dynamo-graph [Running]
└── service
    ├── Frontend        (2/2 replicas)   2/2 ready   gpu: 2    nodes: node-01,02
    │   ├── Pod/frontend-0    Running   gpu: 1   node-01
    │   └── Pod/frontend-1    Running   gpu: 1   node-02
    ├── PrefillWorker   (3/4 replicas)   3/3 ready   gpu: 24   nodes: node-03..05
    │   ├── Pod/prefill-0     Running   gpu: 8   node-03
    │   ├── Pod/prefill-1     Running   gpu: 8   node-04
    │   └── Pod/prefill-2     Running   gpu: 8   node-05
    └── DecodeWorker    (4/4 replicas)   4/4 ready   gpu: 16   nodes: node-07..10
        ├── Pod/decode-0      Running   gpu: 4   node-07
        ├── Pod/decode-1      Running   gpu: 4   node-08
        ├── Pod/decode-2      Running   gpu: 4   node-09
        └── Pod/decode-3      Running   gpu: 4   node-10
```

### `karta workload status <name>`

Workload status as Karta sees it - normalized phases and per-component readiness.

```shell
$ karta workload status llama-finetune
Workload:  PyTorchJob/llama-finetune
Phase:     Running
Age:       2h15m

Components:
  master:  1/1 pods running
  worker:  3/4 pods running
```

### `karta workload resources <name>`

Resource breakdown for a single workload by component.

```shell
$ karta workload resources llama-finetune
COMPONENT   REPLICAS   CPU(req)   MEM(req)   GPU
master      1          4          16Gi       1
worker      4          16         64Gi       32
────────────────────────────────────────────────
TOTAL       5          20         80Gi       33
```

### `karta definition list` / `karta definition describe <name>` / `karta definition validate <file>`

Inspect the Karta definitions the CLI knows about - both community and cluster-provided.

```shell
$ karta definition list
NAME                              KIND               ORIGIN     COMPONENTS
kubeflow-org-pytorchjob-v1        PyTorchJob         community    pytorchjob, master, worker
ray-io-raycluster-v1              RayCluster         community    raycluster, head, worker
my-custom-workload                CustomJob          cluster    customjob, runner

$ karta definition describe kubeflow-org-pytorchjob-v1
(shows structure tree, status mappings, gang scheduling config)

$ karta definition validate ./my-custom-karta.yaml
✓ Valid Karta definition
```

---

## Distribution

We will ship the CLI first as a **standalone binary**, with a clear path to becoming a kubectl plugin later.

For v0.1, we plan to use [GoReleaser](https://goreleaser.com/) with GitHub Actions to build binaries for `darwin/linux × amd64/arm64` on every tag, distributed via GitHub Releases, a Homebrew tap (`brew install run-ai/tap/karta`), and `go install`. The CLI itself will be built on [Cobra](https://cobra.dev/), the standard Go framework for Kubernetes CLIs.

In a later phase, we plan to add [krew](https://krew.sigs.k8s.io/) distribution so users can run `kubectl karta tree ...`. From day one the binary will support being invoked as either `karta` or `kubectl-karta` - Cobra does this out of the box, as long as help text uses `cmd.Name()` and we avoid hardcoding "karta" in error messages and usage strings. Enabling krew later will be a flag flip in GoReleaser plus a PR to [`kubernetes-sigs/krew-index`](https://github.com/kubernetes-sigs/krew-index).

---

## Data Model

The CLI, web dashboard, and MCP all need the same underlying data - a workload's component hierarchy with pods, scale, status, and resources. Rather than each consumer assembling this independently, the Karta library should produce a single `WorkloadTree` that any consumer can traverse and render in its own way.

Beyond the visibility tool, this tree can serve as a shared language between components. The Run:ai External Workload Integrator (EWI) already builds a similar tree for its complex workload structure feature (tracking hierarchies like Dynamo with multi-instance components and nested children) - it could adopt `WorkloadTree` as its foundation. A user submitting a workload could use the same tree structure to set desired topology or other instructions pre-submission.

The model is split into two layers: `WorkloadTree` is the raw data produced by the Karta library (desired structure, scale, specs, status - no live pods, no computation). `WorkloadView` is the display layer built on top of it by the CLI (resource aggregation, ready counts, pod details).

### `WorkloadTree` - shared utility (`pkg/tree/`)

A generic tree built from a Karta definition + workload object. Contains the raw Karta-extracted data - the desired structure, scale, extracted specs, and status, derived entirely from the workload spec. This lives in `pkg/` so any consumer can use it.

The structure was designed with the Run:ai External Workload Integrator (EWI) as a reference consumer - recently a feature was added to the EWI that builds complex hierarchical trees for workloads like Dynamo (multi-instance components with nested children per instance). The `WorkloadTree` is shaped so the EWI could adopt it as its tree-building foundation and add its own persistence layer on top.

```go
// WorkloadTree is the raw tree produced by the shared builder.
type WorkloadTree struct {
    Status   WorkloadStatus                // from root component GetStatus()
    Children []ComponentNode               // root-level components
}

type WorkloadStatus struct {
    Phases []string                        // matched phases, can be multiple (e.g. ["Running", "Degraded"])
}

type ComponentNode struct {
    Name      string
    Kind      *GroupVersionKind             // nil for logical grouping components
    Instances []InstanceNode               // always at least one (see below)
}

// InstanceNode represents one instance of a component.
// Every component has at least one instance. When InstanceKey is nil,
// it means the component is not multi-instance - there's just one.
// For multi-instance components (e.g., Dynamo services with "Frontend",
// "PrefillWorker"), each instance has its own InstanceKey and potentially
// its own child components underneath.
type InstanceNode struct {
    InstanceKey       *string              // nil = single instance (not multi-instance)
    ReplicaKey        *string              // nil = not replicated
    Scale             *Scale               // from Component.GetScale()
    ExtractedInstance *ExtractedInstance    // from Component.GetExtractedInstances() - pod specs, metadata
    Children          []ComponentNode       // child components under this instance
}

type Scale struct {
    Replicas    *int32
    MinReplicas *int32
    MaxReplicas *int32
}
```

Each pod-bearing component carries its desired scale over the scale envelope via `Scale` (`MinReplicas`, `MaxReplicas`, and `Replicas`), read directly from the workload spec; a consumer that needs a desired pod count computes it from the scale. The tree carries no live pods; pod-level data (resource usage, phase, node, readiness) is gathered separately by the consumer, not from the tree.

### Tree builder (`pkg/tree/builder.go`)

```go
func Build(ctx context.Context, karta *v1alpha1.Karta, factory *resource.ComponentFactory) (*WorkloadTree, error)
```

The builder works top-down, component by component, reading the desired structure entirely from the workload spec. Each component is expanded into instances from its `InstanceIdPath` (for example the JobSet `replicatedJobs[].name` or Dynamo service keys); a component with no instance id path has a single unnamed instance. Each instance then recurses into its child components.


### Pod matching (live consumers)

Mapping live pods onto the tree is a separate step from building it. Building stays spec-only so it has no cluster dependency; a consumer that needs per-pod status (phase, node, readiness) or the per-replica breakdown fetches the pods itself and matches them onto the tree. The matching strategy is decoupled behind a `PodMatcher` so each consumer can plug in its own logic via the `pkg/resource` pod selectors (`ReplicaSelector.KeyPath`, `ComponentInstanceSelector`):

```go
type PodMatcher interface {
    Matches(ctx context.Context, pod *corev1.Pod, node *ComponentNode) (bool, error)
}
```

Matching walks top-down, component by component. At each component the matcher claims the pods that belong to it or its descendants, then distributes them across instances; each instance passes only its pods down to child components, narrowing the set at each level until pods reach their leaf component.

Example for a PyTorchJob with 5 pods (master-0, worker-0..3):

```shell
[5 pods] → master component: matcher claims master-0 → [4 remaining]
         → worker component: matcher claims worker-0..3
```

Example for Dynamo with 6 pods (2 frontend, 4 prefill):

```shell
[6 pods] → service component: matcher claims all 6
           → "Frontend" instance: 2 pods
             → lws child: matcher claims them → placed
           → "PrefillWorker" instance: 4 pods
             → lws child: matcher claims them → placed
```

### `WorkloadView` - CLI/web layer

The CLI walks the `WorkloadTree` and computes display data as it traverses: resource aggregation (CPU, memory, GPU summed up the tree), ready counts ("3/4"), and pod details (phase, node). This `WorkloadView` is what gets rendered as table, tree, JSON, or served via web/MCP.

```go
type WorkloadView struct {
    Name       string
    Namespace  string
    Kind       string              // "PyTorchJob", "RayCluster", etc.
    APIVersion string
    Age        time.Duration
    CreatedAt  time.Time

    Phases     []string            // from WorkloadTree.Status.Phases
    Resources  ResourceSummary     // aggregated across all components (computed)
    Children   []ComponentView
}

type ComponentView struct {
    Name       string
    Kind       string
    Instances  []InstanceView
    Scale      ScaleView
    Resources  ResourceSummary     // aggregated across instances (computed)
    ReadyCount string              // "3/4" (computed from live pods)
    Nodes      []string            // distinct nodes hosting this component's pods (computed)
}

type ScaleView struct {
    Replicas    int32
    MinReplicas *int32
    MaxReplicas *int32
}

type InstanceView struct {
    InstanceKey *string             // nil = single instance
    ReplicaKey  *string
    Resources   ResourceSummary    // from extracted pod spec (computed)
    Pods        []PodView
    Children    []ComponentView    // child components under this instance
}

type PodView struct {
    Name       string
    Phase      string              // Running, Pending, Succeeded, Failed
    Ready      bool
    Node       string
    Resources  ResourceSummary     // from live pod spec (computed)
}

type ResourceSummary struct {
    CPUMillis    int64
    MemoryBytes  int64
    GPUs         int64
    Extended     map[string]int64
}
```

