# Karta Roadmap

This document describes where Karta is headed. It is a **living document** — the
direction is set by the community and reviewed regularly, so items move, change,
or drop as the project learns. Dates are intentionally avoided in favor of **Now / Next /
Later** horizons; concrete delivery is tracked in
[GitHub issues](https://github.com/dsx-ai-factory/workload-map/issues) and milestones.

For the day-to-day picture of what is actively being built, the issue tracker is
the source of truth. This page is the *why* and the *order*.

## Vision

Karta aims to be **the standard, vendor-neutral way to describe the structure of
any Kubernetes workload** — so that schedulers, controllers, dashboards, and
platforms can inspect, modify, and manage any workload type without writing
bespoke per-CRD code.

This removes the burden on workload-agnostic tools of diving into the internals
of each workload type's operator. A tested, verified Karta encodes that structure
once, so every consumer relies on the same vetted description.

Success looks like: a workload type (built-in, Kubeflow, Ray, KServe, a custom
CRD, or one that does not exist yet) can be described once with a Karta, and any
Karta-aware tool understands it for free.

## How this roadmap works

- **Now** — actively in progress; tracked by open issues with current activity.
- **Next** — agreed direction, scheduled after the *Now* set lands.
- **Later** — directional intent; shape and timing still open to community input.

Anyone can propose a change to this roadmap by opening a GitHub issue or raising
it in a community discussion. Roadmap changes are reviewed by the maintainers
(see [MAINTAINERS.md](MAINTAINERS.md)) and merged via pull request, so the history
of the roadmap is itself public and reviewable.

---

## Now

**Theme: make Karta a standalone, shippable project.**

- **Karta controller / operator.** A standalone controller that reconciles Karta
  resources and the workloads they describe — today the core processing lives in
  the Go library only. This is the foundational piece that turns Karta from a
  library into a deployable project.
  Tracked by [#34](https://github.com/dsx-ai-factory/workload-map/issues/34) (epic) and
  [#35](https://github.com/dsx-ai-factory/workload-map/issues/35) (design), with task issues
  #67–#72 (core logic, conditions, packaging, end-to-end tests).
- **Validated, non-rotting examples.** Automated tests that check the bundled
  Karta definitions against the upstream CRDs they target, so the
  [`docs/samples/`](docs/samples/) set stays correct as upstream APIs evolve.
  Tracked by [#79](https://github.com/dsx-ai-factory/workload-map/issues/79).
- **Workload visibility.** Tooling to render and explore the structure of any
  Karta-described workload — the most approachable on-ramp to the project. This
  rests on a first-class workload tree: traversal of a workload's full resource
  tree, from the root component down through child components to the pods, so
  consumers can reason about and render the whole structure rather than individual
  fields. Tracked by [#32](https://github.com/dsx-ai-factory/workload-map/issues/32).
- **Karta as a registry.** A discoverable catalog of Karta definitions, so the
  community can publish, find, and reuse descriptions for common workload types
  instead of every consumer re-bundling its own copies. Turns the bundled
  `docs/samples/` set into a shared, versioned source the ecosystem can pull from.
  Tracked by [#86](https://github.com/dsx-ai-factory/workload-map/issues/86).

## Next

**Theme: deepen the contract — breadth and richer expressions — on the path to a
vendor-neutral v1beta1.**

- **Broader, tested workload coverage.** Expand the pre-built, tested example set
  and keep pace with upstream APIs of the workloads Karta describes — including
  **Kubeflow Trainer v2** and Dynamo `v1beta1`
  ([#78](https://github.com/dsx-ai-factory/workload-map/issues/78)).
- **API versioning toward v1beta1.** A documented versioning and deprecation
  policy, and graduation of the CRD from `v1alpha1` toward `v1beta1` as the
  schema settles.
- **Cross-resource expressions.** Let a Karta reference values from related
  resources in its path expressions
  ([#80](https://github.com/dsx-ai-factory/workload-map/issues/80)).
- **Packaging & install.** Helm chart hardening, including a CRD upgrade hook
  ([#43](https://github.com/dsx-ai-factory/workload-map/issues/43)).

## Later

**Theme: ecosystem.**

*Later items are directional, not commitments — order and shape will change with community input.*

- **Karta-aware tooling ecosystem.** Karta is most valuable when many tools read
  and act on the same description. Karta itself stays a thin, neutral contract;
  richer behavior lives in separate projects across the Karta ecosystem. Examples
  of such ecosystem projects that read and act on the contract:
  - **CLI** — read-only visibility: list and drill into any Karta-described
    workload from the command line.
  - **Headlamp plugin** — the GUI face of visibility, built on the CNCF
    [Headlamp](https://github.com/kubernetes-sigs/headlamp) project rather than a
    bespoke dashboard.
  - **Mutation runtime** — a controller that submits and mutates workloads and
    provisions secondary resources from a Karta description, without per-CRD code.
  - **Governance / policy engine** — admission- and runtime-level rules
    (filter / condition / action) applied to Karta-described workloads, including
    descheduling and rebalancing.

  Clear integration patterns will be documented so anyone can build their own
  Karta-aware tool.
- **Community engagement with related projects.** Engage the relevant upstream
  communities (workload and scheduling SIGs/TAGs, queueing and gang-scheduling
  projects) to position Karta as complementary infrastructure — *how to read and
  modify the structure of a workload* — rather than as a competing scheduler or
  queue.

---

## Non-goals

Karta deliberately stays narrow. It describes workload **structure**; it does not
schedule, queue, autoscale, or own a workload's lifecycle. Those belong to the
tools that consume a Karta description. Keeping Karta a thin contract is what lets
it stay vendor-neutral and broadly adoptable.

## Get involved

- Browse [open issues](https://github.com/dsx-ai-factory/workload-map/issues) and look for
  `good first issue` and `help wanted` labels.
- Have a workload type Karta should describe? Open an issue or contribute an
  example to [`docs/samples/`](docs/samples/).
- Read [CONTRIBUTING.md](CONTRIBUTING.md) to get started (DCO sign-off required).

*This roadmap is reviewed regularly and updated as priorities shift with
community input.*
