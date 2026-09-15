// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

// Package version exposes the operator version that is stamped into the binary
// at build time via -ldflags "-X github.com/dsx-ai-factory/workload-map/operator/pkg/version.Version=<ver>".
//
// When the binary is built without ldflags (e.g. `go run ./cmd` during
// development) Version is "dev".
package version

// Version is set by the Makefile at link time. Default is "dev".
var Version = "dev"
