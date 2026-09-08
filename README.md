# Karta

**A standard way to describe the structure of any Kubernetes workload type.**

[![CI](https://github.com/run-ai/karta/actions/workflows/ci.yaml/badge.svg)](https://github.com/run-ai/karta/actions/workflows/ci.yaml)
[![Go Reference](https://pkg.go.dev/badge/github.com/run-ai/karta.svg)](https://pkg.go.dev/github.com/run-ai/karta)
[![Go Report Card](https://goreportcard.com/badge/github.com/run-ai/karta)](https://goreportcard.com/report/github.com/run-ai/karta)
[![Latest release](https://img.shields.io/github/v/release/run-ai/karta)](https://github.com/run-ai/karta/releases)
[![License](https://img.shields.io/github/license/run-ai/karta)](LICENSE)

Karta lets you define a portable, declarative blueprint for any Kubernetes workload - whether it's a simple Deployment, a distributed PyTorchJob, or a custom CRD. Controllers and platforms can then use that blueprint to inspect, modify, and manage workloads without hard-coding knowledge of each type.

## The Problem

In Kubernetes, and especially in AI systems, a workload is not a standalone execution unit such as a single Pod. Instead, it is composed of multiple components organized in a complex hierarchy of resources, often exposed via custom resource definitions (CRDs) - for example: PyTorchJob, RayCluster, and MPIJob. Each of these CRDs structures the workload configuration differently, but they all share the same conceptual building blocks: pod specifications, scaling parameters, and status definitions.

If you're building a controller, scheduler, or platform that needs to work with multiple workload types, you end up writing bespoke logic for each one:

- Where is the pod template?
- How do I find the replica count?
- Which status conditions mean "running" vs "failed"?
- How do I modify the pod spec without breaking the workload?

This doesn't scale. Every new workload type means new integration code - schedulers, controllers, and platforms all end up maintaining per-CRD adapters that implement the same patterns over and over.

## The Solution

Karta (*a map to navigate resources*) introduces a CRD that maps the structure of any workload type into a standard schema. Using JQ-based path expressions, a Karta declaratively defines how to locate pod specifications, scaling parameters, and status fields within any workload hierarchy. Define it once, and any controller can use it to:

- **Extract** pod templates, replica counts, status, and metadata
- **Update** pod specs, labels, and annotations across all instances
- **Understand** workload hierarchy (e.g., a JobSet with master + worker groups)

In addition to the CRD, Karta provides a **Go package** that performs the core processing logic: query evaluation to dynamically interpret custom resource schemas, resource extraction that traverses workload hierarchies to identify and group pods, and optimization instruction processors that apply strategies such as gang scheduling to ensure coordinated placement of pods for distributed workloads.

```
┌──────────────────────────────────────────────────┐
│                   Your Platform                  │
│  (scheduler, controller, dashboard, CLI, etc.)   │
├──────────────────────────────────────────────────┤
│              Karta Component API                 │
│    Extract pods · Update specs · Read status     │
├────────────┬────────────┬────────────┬───────────┤
│ Karta:     │ Karta:     │ Karta:     │ Karta:    │
│ JobSet     │ RayCluster │ PyTorchJob │ YourCRD   │
└────────────┴────────────┴────────────┴───────────┘
```

## Quick Start

### Install the CRD

```bash
kubectl apply -f https://raw.githubusercontent.com/run-ai/karta/main/charts/karta/crds/run.ai_kartas.yaml
```

### Use the Go library

```bash
go get github.com/run-ai/karta@latest
```

### Define a Karta

Here's a Karta for a JobSet - a distributed training workload with master and worker groups:

```yaml
apiVersion: run.ai/v1alpha1
kind: Karta
spec:
  structureDefinition:
    rootComponent:
      name: jobset
      kind:
        group: jobset.x-k8s.io
        version: v1alpha2
        kind: JobSet
      statusDefinition:
        conditionsDefinition:
          path: .status.conditions
          typeFieldName: type
          statusFieldName: status
        statusMappings:
          running:
          - byConditions:
            - type: StartupPolicyCompleted
              status: "True"
          completed:
          - byConditions:
            - type: Completed
              status: "True"
          failed:
          - byConditions:
            - type: Failed
              status: "True"

    childComponents:
    - name: replicatedjob
      kind:
        group: batch
        version: v1
        kind: Job
      ownerRef: jobset
      specDefinition:
        podTemplateSpecPath: .spec.replicatedJobs[].template.spec.template
      scaleDefinition:
        replicasPath: .spec.replicatedJobs[].replicas
      instanceIdPath: .spec.replicatedJobs[].name  # Instances: "master", "worker"
```

### Extract workload information

```go
import "github.com/run-ai/karta/pkg/resource"

// Create a factory from your Karta and workload object
factory := resource.NewComponentFactoryFromObject(karta, jobSetObject)

// Get the child component which has the per-instance data
component, _ := factory.GetComponent("replicatedjob")
summaries, _ := component.GetExtractedInstances(ctx)

// Access pod template specs, metadata, and scale info for each instance
for instanceID, summary := range summaries {
    // instanceID will be "master" or "worker"
    if summary.PodTemplateSpec != nil {
        fmt.Printf("Instance %s image: %s\n", instanceID, summary.PodTemplateSpec.Spec.Containers[0].Image)
    }
}

// Get status from the root component
rootComponent, _ := factory.GetRootComponent()
status, _ := rootComponent.GetStatus(ctx)
// status.MatchedStatuses: matched statuses based on conditions (e.g., ["running"])
// status.Phase: raw phase string from the workload
// status.Conditions: []Condition with Type, Status, Message fields
```

### Update workload specs

The same paths defined in `specDefinition` are used for both extraction and updates:

```go
// Prepare updates per instance
updates := map[string]resource.FragmentedPodSpec{
    "master": {
        SchedulerName: "my-custom-scheduler",
        Labels: map[string]string{"my-label": "true"},
    },
    "worker": {
        SchedulerName: "my-custom-scheduler",
    },
}

// Apply updates - modifies the underlying unstructured object
err := component.UpdateFragmentedPodSpec(ctx, updates)

// Get the updated object to apply back to the cluster
updatedObject, _ := factory.GetObject()
```

## Pre-built Karta Definitions

Karta supports any workload type. The following are pre-built and tested Karta definitions that ship with the project:

| Workload Type | Framework |
|---|---|
| JobSet | Kubernetes |
| PyTorchJob | Kubeflow |
| RayCluster | Ray |
| RayJob | Ray |
| InferenceService | KServe |
| Knative Service | Knative |
| MPIJob | Kubeflow |
| NIM Service | NVIDIA |
| LeaderWorkerSet | Kubernetes |
| Milvus | Milvus |
| DynamoGraphDeployment | NVIDIA Dynamo |

See [`docs/samples/`](docs/samples/) for the full Karta definitions.

### Complex example: NVIDIA Dynamo

The [Dynamo Karta](docs/samples/dynamo.yaml) shows Karta handling a real-world multi-service inference graph - fragmented pod specs across services, autoscaling with min/max replicas, replica selectors for multi-node workers, gang scheduling, and 6 additional child resource types (DynamoComponentDeployment, LeaderWorkerSet, PodGang, PodClique, PodCliqueSet, PodCliqueScalingGroup). A single Karta definition replaces what would otherwise require hundreds of lines of per-type controller logic.

## Runnable examples

Two runnable examples live under [`docs/examples/`](docs/examples/):

- [quickstart](docs/examples/quickstart/) - reads and mutates a JobSet and a LeaderWorkerSet offline, no cluster required. The fastest way to see the uniform API in action.
- [controller-runtime](docs/examples/controller-runtime/) - a controller-runtime manager you install into a Kind cluster. It watches live LeaderWorkerSet workloads, then inspects and mutates them through Karta with no per-CRD code. Adding more workload types is a flag change, not a rebuild.

## Who Uses Karta?

Karta was created at [Run:ai](https://run.ai) (NVIDIA) to power workload management across diverse Kubernetes workload types. It is used internally by multiple services including the workload controllers, scheduler integrations, and platform components.

[KAI Scheduler](https://github.com/kai-scheduler/KAI-Scheduler), a CNCF sandbox open-source Kubernetes scheduler for AI and GPU workloads, uses Karta as the generic fallback plugin in its pod grouper ([kai-scheduler/KAI-Scheduler#1527](https://github.com/kai-scheduler/KAI-Scheduler/issues/1527)). A Karta definition gives any CRD correct pod grouping and gang scheduling in KAI, with no scheduler plugin to write and no KAI release to wait for. Native in-tree plugins keep precedence; Karta handles the long tail.

## Documentation

- [Technical Guide](docs/Technical%20Guide.md) - Full Karta spec, path syntax (jq), validation rules
- [FIPS 140-3](docs/FIPS.md) - Running the operator with Go's FIPS 140-3 crypto module
- [Karta definitions](docs/samples/) - Real-world Karta definitions for common workload types
- [Runnable examples](docs/examples/) - Offline quickstart and an installable controller-runtime example
- [API Reference](https://pkg.go.dev/github.com/run-ai/karta) - Go package documentation
- [CONTRIBUTING.md](CONTRIBUTING.md) - How to contribute (DCO required)
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) - Community standards and expectations

## Community

Have a question, an idea, or want to show what you built with Karta? Join the conversation in [GitHub Discussions](https://github.com/run-ai/karta/discussions). For bugs and feature requests, [open an issue](https://github.com/run-ai/karta/issues).

## Status

Karta is in active development (pre-1.0). The API may change between minor versions. We welcome feedback and contributions - please [open an issue](https://github.com/run-ai/karta/issues) or [start a discussion](https://github.com/run-ai/karta/discussions).

## Third-Party Software

This project includes third-party software components. See the [NOTICE](NOTICE) file for attributions and the [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES) file for detailed license information.

## License

Apache License 2.0 - see [LICENSE](LICENSE) for the full text.

Copyright (c) 2026 NVIDIA Corporation.
