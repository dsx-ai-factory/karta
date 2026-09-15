<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 NVIDIA Corporation -->

# Changelog - v0.2

Every release on the `v0.2` line: the `v0.2.0` minor and the patch releases tagged from this branch. Each entry carries the content of the published [GitHub Release](https://github.com/dsx-ai-factory/workload-map/releases), normalized to the repository Markdown rules in [AGENTS.md](AGENTS.md).

`main`'s changelog covers minor and major releases across all lines.

## v0.2.3 - 2026-08-17

Adds a digest-pinned air-gap image lock to the release. Each release now carries
per-platform `ImageLock` YAML assets (linux/amd64, linux/arm64) that list the exact
image digests needed to install Karta in an air-gapped cluster.

### What's Changed
* feat(release): generate air-gap image locks per release (v0.2 backport) by @AviadHayumi in #237

Full Changelog: https://github.com/dsx-ai-factory/workload-map/compare/v0.2.2...v0.2.3

## v0.2.2 - 2026-07-23

### What's Changed
* Fix/v0.2 crd cache transform by @shaked-bouktus in https://github.com/dsx-ai-factory/workload-map/pull/177


Full Changelog: https://github.com/dsx-ai-factory/workload-map/compare/v0.2.1...v0.2.2

## v0.2.1 - 2026-07-20

### What's Changed
* fix(chart): bump operator memory limit to 256Mi by @shaked-bouktus in https://github.com/dsx-ai-factory/workload-map/pull/164


Full Changelog: https://github.com/dsx-ai-factory/workload-map/compare/v0.2.0...v0.2.1

## v0.2.0 - 2026-07-15

This release extends Karta from a CRD and Go library into an optional runnable system. It lands the controller / operator (reconcile core, workload-tree model, Helm chart, container images), and a new pod-grouping API for better schedulers integration, alongside a much larger catalog of built-in workload samples. There are no breaking API changes; the previous grouping format is deprecated but still works. See [Deprecations](#deprecations) for the recommended migration.

---

### Highlights

| | |
|---|---|
| Controller / operator | The reconcile core, an operator Helm chart, and container images all land, so Karta can now run as a controller rather than only being a CRD plus library. ([#77](https://github.com/dsx-ai-factory/workload-map/pull/77), [#97](https://github.com/dsx-ai-factory/workload-map/pull/97), [#91](https://github.com/dsx-ai-factory/workload-map/pull/91)) |
| WorkloadTree | A `WorkloadTree` is the raw component hierarchy of a workload (its desired structure, scale, specs, and status) produced by the Karta library, giving clients a single shared tree to traverse and render. ([#87](https://github.com/dsx-ai-factory/workload-map/pull/87))|
| New pod-grouping API | `gangScheduling.podGroup` adds subgroups and topology constraints for better schedulers integration; the old `gangScheduling.podGroups` is deprecated but still honored. ([#145](https://github.com/dsx-ai-factory/workload-map/pull/145)) |

---

### Install

Helm chart (from GHCR):

```bash
helm install karta oci://ghcr.io/dsx-ai-factory/workload-map/karta --version 0.2.0
```

Go consumers:

```bash
go get github.com/dsx-ai-factory/workload-map@v0.2.0
```

```go
import karta "github.com/dsx-ai-factory/workload-map/pkg/api/runai/v1alpha1"
```

---

### Compatibility

| | |
|---|---|
| Kubernetes | tested against v1.31 - v1.35 (`k8s.io/api v0.36.2`) |
| Go | 1.26.3 or later |
| Helm | 3.14 or later |

---

### Deprecations

#### `gangScheduling.podGroups` -> `gangScheduling.podGroup`

The alpha grouping format `podGroups` (a list) is deprecated in favor of the new `podGroup` mapping, which adds subgroups and topology constraints. Existing Kartas using `podGroups` continue to validate and run.

Correction added after release: this is not a drop-in migration at v0.2.0, and the original release notes did not say so.

- Karta's own instruction helpers read only `podGroups`. `pkg/instructions/summary.go` builds gang-scheduling candidates from `GangScheduling.PodGroups` and never reads `GangScheduling.PodGroup`. That is true at v0.2.0 and still true today. A consumer that relies on those helpers for grouping loses its grouping if it switches formats.
- `podGroup` is honored by the scheduler integration instead. The KAI Karta podgrouper plugin prefers `podGroup` and falls back to `podGroups`. It shipped in KAI v0.17.0 on 2026-08-03, after this release, so at v0.2.0 nothing consumed the new field yet.
- The two formats are not equivalent. A `podGroups` member carries `groupByKeyPaths` and `filters`; a `subGroups` entry carries only `componentName` and `topology`, so per-component grouping keys and filters have no direct counterpart.

Target shape of the new format:

```diff
  optimizationInstructions:
    gangScheduling:
-     podGroups:
-       - name: job
-         members:
-           - componentName: launcher
-             groupByKeyPaths:
-               - .metadata.labels["training.kubeflow.org/job-name"]
-           - componentName: worker
-             groupByKeyPaths:
-               - .metadata.labels["training.kubeflow.org/job-name"]
+     podGroup:
+       name: job
+       subGroups:
+         - componentName: launcher
+         - componentName: worker
```

---

### New features

- Controller reconcile core - the operator's central reconcile logic. ([#77](https://github.com/dsx-ai-factory/workload-map/pull/77) by @shaked-bouktus)
- `WorkloadTree` data model and builder - a typed tree that assembles a workload's root and child components. ([#87](https://github.com/dsx-ai-factory/workload-map/pull/87) by @rogirun)
- Operator Helm chart - deploy the karta operator via Helm. ([#97](https://github.com/dsx-ai-factory/workload-map/pull/97) by @shaked-bouktus)
- Operator container images - Dockerfiles for the karta operator. ([#91](https://github.com/dsx-ai-factory/workload-map/pull/91) by @shaked-bouktus)
- Operator Makefile - build / run targets for the operator. ([#96](https://github.com/dsx-ai-factory/workload-map/pull/96) by @shaked-bouktus)
- `karta` CLI - CLI entrypoint and Cobra root command. ([#127](https://github.com/dsx-ai-factory/workload-map/pull/127) by @rogirun)
- New pod-grouping API - `gangScheduling.podGroup` with subgroups and topology. ([#145](https://github.com/dsx-ai-factory/workload-map/pull/145) by @davidLif)
- Kind-based e2e provisioner - `hack/e2e` spins up a kind cluster for end-to-end tests. ([#144](https://github.com/dsx-ai-factory/workload-map/pull/144) by @AviadHayumi)
- Expanded workload sample catalog - many additional built-in Karta samples across core, batch, training, serving, and Ray workloads. ([#155](https://github.com/dsx-ai-factory/workload-map/pull/155) by @AviadHayumi)
- Grove `PodCliqueSet` sample - `grove.io/v1alpha1` Karta example. ([#66](https://github.com/dsx-ai-factory/workload-map/pull/66) by @shmuel-runai)
- Milvus sample - `milvus.io/v1beta1` Karta example. ([#103](https://github.com/dsx-ai-factory/workload-map/pull/103) by @ronlv10)
- Controller example with LWS - a worked controller example over LeaderWorkerSet. ([#92](https://github.com/dsx-ai-factory/workload-map/pull/92) by @yuval-gr)
- Quickstart controller example - minimal controller to get started. ([#84](https://github.com/dsx-ai-factory/workload-map/pull/84) by @yuval-gr)

### Bug fixes

- Fix native resource discovery. ([#132](https://github.com/dsx-ai-factory/workload-map/pull/132) by @shaked-bouktus)
- Avoid append aliasing of caller-owned component slices. ([#129](https://github.com/dsx-ai-factory/workload-map/pull/129) by @ronlv10)
- Fix rayjob worker `instanceIdPath` nesting in the sample. ([#133](https://github.com/dsx-ai-factory/workload-map/pull/133) by @lavianalon)
- Fix dependabot missing code generation failing CI. ([#150](https://github.com/dsx-ai-factory/workload-map/pull/150) by @Isan-Rivkin)

### Maintenance

- Bump Go to 1.26.3 and update dependencies. ([#75](https://github.com/dsx-ai-factory/workload-map/pull/75) by @yuval-gr)
- Update `golang.org/x` dependencies to latest. ([#93](https://github.com/dsx-ai-factory/workload-map/pull/93) by @yuval-gr)
- Bump the go-minor-patch dependency group. ([#136](https://github.com/dsx-ai-factory/workload-map/pull/136), [#147](https://github.com/dsx-ai-factory/workload-map/pull/147) by @dependabot)
- Add `dependabot.yml` for version updates. ([#120](https://github.com/dsx-ai-factory/workload-map/pull/120) by @yuval-gr)
- Create verified dependabot codegen commits via the GitHub API. ([#153](https://github.com/dsx-ai-factory/workload-map/pull/153) by @Isan-Rivkin)
- CI action bumps: checkout 4->7 ([#123](https://github.com/dsx-ai-factory/workload-map/pull/123)), cache 4->6 ([#122](https://github.com/dsx-ai-factory/workload-map/pull/122)), setup-go 5->6 ([#121](https://github.com/dsx-ai-factory/workload-map/pull/121)), setup-buildx 3->4 ([#146](https://github.com/dsx-ai-factory/workload-map/pull/146)). (by @dependabot)

### Documentation

- Add an authoring tutorial and troubleshooting guide. ([#130](https://github.com/dsx-ai-factory/workload-map/pull/130) by @lavianalon)
- Add ROADMAP and GOVERNANCE. ([#89](https://github.com/dsx-ai-factory/workload-map/pull/89) by @lavianalon)
- Add a maintainer ladder / emeritus policy and RELEASE.md. ([#128](https://github.com/dsx-ai-factory/workload-map/pull/128) by @lavianalon)
- Harden OSS health docs (security policy, code of conduct, contributing). ([#111](https://github.com/dsx-ai-factory/workload-map/pull/111) by @lavianalon)
- Add README badges, community section, and CoC link. ([#125](https://github.com/dsx-ai-factory/workload-map/pull/125) by @lavianalon)
- Add AGENTS.md guide for AI coding agents. ([#73](https://github.com/dsx-ai-factory/workload-map/pull/73) by @lavianalon)
- Add a CLI high-level design document. ([#31](https://github.com/dsx-ai-factory/workload-map/pull/31) by @ronlv10)
- Add KAI Scheduler to the "who uses Karta" section. ([#156](https://github.com/dsx-ai-factory/workload-map/pull/156) by @lavianalon)
- Drop the API group rename from the roadmap and governance. ([#119](https://github.com/dsx-ai-factory/workload-map/pull/119) by @lavianalon)

---

### Dependencies

#### Changed

- Go `1.25.9` -> `1.26.3`
- `k8s.io/api` `v0.35.1` -> `v0.36.2`

---

### Contributors

thanks to everyone who shipped this release:

[@AviadHayumi](https://github.com/AviadHayumi), [@Isan-Rivkin](https://github.com/Isan-Rivkin), [@davidLif](https://github.com/davidLif), [@lavianalon](https://github.com/lavianalon), [@rogirun](https://github.com/rogirun), [@ronlv10](https://github.com/ronlv10), [@shaked-bouktus](https://github.com/shaked-bouktus), [@shmuel-runai](https://github.com/shmuel-runai), [@yuval-gr](https://github.com/yuval-gr)

plus automated dependency updates from [@dependabot](https://github.com/dependabot).

---

Full changelog: https://github.com/dsx-ai-factory/workload-map/compare/v0.1.0...v0.2.0
Helm chart: `oci://ghcr.io/dsx-ai-factory/workload-map:0.2.0`
Container images: see [packages page](https://github.com/dsx-ai-factory/workload-map/pkgs/container/karta)
