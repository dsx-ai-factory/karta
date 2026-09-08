# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 NVIDIA Corporation

# Location to install dependencies to, and to output built binaries.
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# Version is derived automatically from git. Falls back to "dev" when there
# are no tags or git is unavailable (e.g. a bare source tarball).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Container image settings (defaults for ghcr.io/run-ai/karta OSS publishing).
# Override any component from the command line, e.g.:
#   make docker-build IMAGE_TAG=v1.2.3
#   make docker-build IMAGE_REGISTRY=ghcr.io/myorg/karta
IMAGE_REGISTRY ?= ghcr.io/run-ai/karta
IMAGE_NAME      ?= karta-operator
IMAGE_TAG       ?= $(VERSION)
IMAGE           ?= $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

CONTAINER_TOOL ?= docker
BUILD_ARGS     ?=

# VERSION_PKG is the Go package path that holds the Version variable. The build
# targets inject VERSION into the binary via -ldflags so it is available at runtime.
VERSION_PKG = github.com/run-ai/karta/operator/pkg/version
LDFLAGS     = -ldflags "-X $(VERSION_PKG).Version=$(VERSION)"

GOLANGCI_LINT         ?= $(LOCALBIN)/golangci-lint
# GOLANGCI_LINT_VERSION is defined in ../hack/tools.mk (shared with the root
# Makefile) so the linter version has a single source of truth. This Makefile is
# always run from the operator/ directory (see the ../.golangci.yml reference).
include ../hack/tools.mk

ENVTEST          ?= $(LOCALBIN)/setup-envtest
ENVTEST_VERSION  ?= release-0.23
# Kubernetes control-plane (apiserver/etcd/kubectl) version envtest downloads.
ENVTEST_K8S_VERSION ?= 1.31.0

# Architectures to build for `make build-all` and `make docker-buildx-push`
PLATFORMS ?= linux/amd64 linux/arm64
empty :=
space := $(empty) $(empty)
comma := ,
PLATFORMS_CSV = $(subst $(space),$(comma),$(PLATFORMS))

##@ Development

.PHONY: fmt
fmt: ## Run go fmt (fixes files in place)
	go fmt ./...

.PHONY: fmt-check
fmt-check: ## Check formatting without modifying files (intended for CI)
	@set -e; \
	dirs="$$(go list -f '{{.Dir}}' ./...)"; \
	unformatted="$$(gofmt -l $$dirs)"; \
	test -z "$$unformatted" || (echo "go fmt required:"; echo "$$unformatted"; exit 1)

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint (set VERBOSE=1 for -v)
	$(GOLANGCI_LINT) run $(if $(VERBOSE),-v) -c ../.golangci.yml

.PHONY: test-unit
test-unit: ## Run unit tests (no envtest binaries required)
	go test -coverprofile=cover-unit.out ./pkg/... ./cmd/...

.PHONY: test-integration
test-integration: envtest ## Run envtest integration tests (downloads control-plane binaries)
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
		go test -coverprofile=cover-integration.out ./test/integration/...

.PHONY: test
test: test-unit test-integration ## Run unit + integration tests

.PHONY: check
check: fmt-check vet lint test ## Run all checks (fmt-check, vet, lint, test)

##@ Build

.PHONY: build
build: ## Build the karta-operator binary for the host OS/arch
	mkdir -p $(LOCALBIN)
	go build $(LDFLAGS) -o $(LOCALBIN)/karta-operator ./cmd

.PHONY: build-linux-amd64
build-linux-amd64: ## Cross-compile for linux/amd64
	mkdir -p $(LOCALBIN)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(LOCALBIN)/karta-operator-amd64 ./cmd

.PHONY: build-linux-arm64
build-linux-arm64: ## Cross-compile for linux/arm64
	mkdir -p $(LOCALBIN)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(LOCALBIN)/karta-operator-arm64 ./cmd

.PHONY: build-all
build-all: build-linux-amd64 build-linux-arm64 ## Cross-compile for all supported platforms

##@ Docker

.PHONY: docker-build
docker-build: ## Build Docker image for the host arch
	$(CONTAINER_TOOL) build $(BUILD_ARGS) \
		--build-arg TARGETARCH=$(shell go env GOARCH) \
		--build-arg VERSION=$(VERSION) \
		--tag $(IMAGE) \
		-f Dockerfile \
		..

.PHONY: docker-buildx-push
docker-buildx-push: ## Build and push a multi-arch Docker image with an SBOM attestation via BuildKit (requires Docker)
	@[ "$(CONTAINER_TOOL)" = "docker" ] || (echo "Error: docker-buildx-push requires CONTAINER_TOOL=docker (got '$(CONTAINER_TOOL)')" >&2; exit 1)
	$(CONTAINER_TOOL) buildx build $(BUILD_ARGS) \
		--platform $(PLATFORMS_CSV) \
		--build-arg VERSION=$(VERSION) \
		--tag $(IMAGE) \
		--attest type=sbom \
		-f Dockerfile \
		--push \
		..

.PHONY: docker-push
docker-push: ## Push the Docker image
	$(CONTAINER_TOOL) push $(IMAGE)

##@ Tools

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	@[ -f "$(ENVTEST)" ] || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	@[ -f "$(GOLANGCI_LINT)" ] || { \
	set -e; \
	echo "Downloading golangci-lint@$(GOLANGCI_LINT_VERSION)"; \
	curl -sSfL https://golangci-lint.run/install.sh \
		| sh -s -- -b $(LOCALBIN) $(GOLANGCI_LINT_VERSION); \
	}

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
	/^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } \
	/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help
