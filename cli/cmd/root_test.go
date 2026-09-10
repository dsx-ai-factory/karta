// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// execute runs the root command with the given args, capturing its output. A
// noop runnable subcommand is attached so a valid invocation reaches a RunE and
// returns nil; a bare parent command would only print help.
func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	// Config is read from the environment, so an ambient ~/.karta/config.yaml
	// would otherwise decide the outcome.
	t.Setenv("HOME", t.TempDir())
	t.Setenv(configEnvVar, "")

	cmd := NewRootCommand()
	cmd.AddCommand(&cobra.Command{
		Use:  "noop",
		RunE: func(*cobra.Command, []string) error { return nil },
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestRootHelp(t *testing.T) {
	out, err := execute(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	for _, want := range []string{"--kubeconfig", "--output", "get", "definition"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q\n%s", want, out)
		}
	}
}

func TestOutputValidation(t *testing.T) {
	for _, format := range []string{"table", "wide", "json", "yaml"} {
		if _, err := execute(t, "-o", format, "noop"); err != nil {
			t.Errorf("output %q rejected: %v", format, err)
		}
	}

	_, err := execute(t, "-o", "bogus", "noop")
	if err == nil {
		t.Fatal("expected error for invalid output format, got nil")
	}
	if !strings.Contains(err.Error(), "must be one of table, wide, json, yaml") {
		t.Errorf("unexpected error message: %v", err)
	}
}
