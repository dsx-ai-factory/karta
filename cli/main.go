// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

// Command karta is the Karta CLI: workload-aware visibility for any Kubernetes
// workload type. It builds on the Karta abstraction layer to list and describe
// workloads and the definitions Karta understands.
package main

import (
	"os"

	"github.com/dsx-ai-factory/workload-map/cli/cmd"
)

func main() {
	// Cobra prints the error to stderr; main only sets the exit code.
	if err := cmd.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
