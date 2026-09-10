// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// exitCodeOf runs the root command with args and reports the code the binary
// would exit with.
func exitCodeOf(t *testing.T, args ...string) (int, error) {
	t.Helper()
	isolateConfig(t)
	return runRoot(t, args...)
}

// isolateConfig points config discovery at an empty environment, so a test does
// not depend on the developer's own ~/.karta/config.yaml.
func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(configEnvVar, "")
}

// runRoot executes the command tree and reports the code the binary would exit
// with, leaving the environment as the caller set it.
func runRoot(t *testing.T, args ...string) (int, error) {
	t.Helper()

	root := NewRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs(args)

	err := root.Execute()
	if err == nil {
		return 0, nil
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode(), err
	}
	return ExitError, err
}

// An unrecognised command is a usage error, not a cluster failure.
func TestUnknownCommandIsAUsageError(t *testing.T) {
	code, err := exitCodeOf(t, "wrkload")
	if err == nil {
		t.Fatal("expected an error for an unknown command")
	}
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d (%v)", ExitUsage, code, err)
	}
}

// An invalid flag value anywhere in the tree is a usage error, without the
// command having to wrap it.
func TestInvalidFlagValueIsAUsageError(t *testing.T) {
	root := NewRootCommand()
	root.AddCommand(&cobra.Command{Use: "noop", RunE: func(*cobra.Command, []string) error { return nil }})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"-o", "bogus", "noop"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an invalid output format")
	}
	var coded interface{ ExitCode() int }
	if !errors.As(err, &coded) || coded.ExitCode() != ExitUsage {
		t.Errorf("expected exit %d, got %v", ExitUsage, err)
	}
}

// Every command reports a rejected argument the same way, so the same user
// mistake does not produce different exit codes.
func TestArgumentErrorIsAUsageError(t *testing.T) {
	for _, command := range []string{"definition", "workload"} {
		code, err := exitCodeOf(t, command, "bogus")
		if err == nil {
			t.Errorf("%s: expected an error for an unexpected argument", command)
			continue
		}
		if code != ExitUsage {
			t.Errorf("%s: expected exit %d, got %d (%v)", command, ExitUsage, code, err)
		}
	}
}

// An invalid output value is a usage error wherever it came from; as a flag it
// is rejected by pflag, from the environment by the config loader.
func TestInvalidOutputFromEnvironmentIsAUsageError(t *testing.T) {
	t.Setenv("KARTA_OUTPUT", "bogus")

	code, err := exitCodeOf(t, "definition")
	if err == nil {
		t.Fatal("expected an error for an invalid output format")
	}
	if code != ExitUsage {
		t.Errorf("expected exit %d, got %d (%v)", ExitUsage, code, err)
	}
}

// A bad config file must not stop the reader reaching the help. The config path
// is set after isolation so it survives into the run.
func TestBareRootIgnoresConfig(t *testing.T) {
	isolateConfig(t)

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("output: bogus\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv(configEnvVar, path)

	if code, err := runRoot(t); err != nil || code != 0 {
		t.Errorf("expected help and a clean exit, got %d (%v)", code, err)
	}
}

// Typo recovery has to survive the root taking over argument handling.
func TestUnknownCommandSuggestsANearMatch(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"definitons"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an unknown command")
	}
	if !strings.Contains(err.Error(), "Did you mean this?") ||
		!strings.Contains(err.Error(), "definitions") {
		t.Errorf("expected a near-match suggestion, got %q", err.Error())
	}
}

// The root prints help rather than failing when invoked bare.
func TestRootWithoutArgsSucceeds(t *testing.T) {
	if code, err := exitCodeOf(t); err != nil || code != 0 {
		t.Errorf("expected a clean exit, got %d (%v)", code, err)
	}
}
