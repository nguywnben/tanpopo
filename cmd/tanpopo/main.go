// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package main provides the single canonical Tanpopo CLI and server entrypoint (`tanpopo`).
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/nguywnben/tanpopo/internal/cli/plugin"
	"github.com/nguywnben/tanpopo/internal/cli/remote"
	"github.com/nguywnben/tanpopo/internal/cli/support"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
)

// Build metadata populated via linker -ldflags during release builds.
var (
	version = "0.1.0-dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	var (
		outputFmt   string
		showVersion bool
	)

	rootCmd := &cobra.Command{
		Use:           "tanpopo",
		Short:         "Open-source, self-hosted-first, extension-first AI Gateway",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				_, _ = fmt.Fprintf(stdout, "tanpopo version %s (commit=%s built=%s)\n", version, commit, date)
				return nil
			}
			return cmd.Help()
		},
	}
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)

	rootCmd.PersistentFlags().StringVar(&outputFmt, "output", "table", "Output format (table|json|yaml)")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Print version information")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			_, _ = fmt.Fprintf(stdout, "tanpopo version %s (commit=%s built=%s)\n", version, commit, date)
		},
	}

	rootCmd.AddCommand(
		versionCmd,
		plugin.NewCommand(plugin.Options{
			OutputFormat: &outputFmt,
			Stdout:       stdout,
			Stderr:       stderr,
		}),
		support.NewCommand(support.Options{RootDir: ".", Stdout: stdout}),
	)
	for _, command := range remote.NewCommands(remote.Options{
		OutputFormat: &outputFmt,
		Stdout:       stdout,
		Stderr:       stderr,
	}) {
		rootCmd.AddCommand(command)
	}

	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintf(stderr, "tanpopo: %s\n", testharness.SanitizeTerminalText(err.Error()))
		return testharness.CodeOf(err)
	}
	return testharness.ExitOK
}
