// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package cmd

import "github.com/spf13/cobra"

// Exit codes. The numbers are not a contract; only that conditions differ.
const (
	ExitError            = 1 // cluster unreachable, auth failure
	ExitUsage            = 2 // invalid flag value or argument
	ExitWorkloadNotFound = 3 // no such workload of the requested type
	ExitNotFound         = 4 // no Karta definition covers the requested type
)

// exitError carries the exit code a failure should produce. A plain error still
// means ExitError.
type exitError struct {
	code int
	err  error
	// path names the command to point at in the usage hint, so a subcommand
	// failure does not send the reader to the root help.
	path string
}

func (e exitError) Error() string { return e.err.Error() }

func (e exitError) Unwrap() error { return e.err }

// ExitCode lets main match on behaviour rather than the concrete type.
func (e exitError) ExitCode() int { return e.code }

func (e exitError) UsagePath() string { return e.path }

// usageError marks a failure the caller can fix by reinvoking the command.
func usageError(cmd *cobra.Command, err error) error {
	return exitError{code: ExitUsage, err: err, path: cmd.CommandPath()}
}

// usageArgs gives every command the same reporting for a rejected argument.
func usageArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return usageError(cmd, err)
		}
		return nil
	}
}
