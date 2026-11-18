// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package main provides the single canonical Tanpopo CLI and server entrypoint (`tanpopo`).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	admincli "github.com/nguywnben/tanpopo/internal/cli/admin"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := runWithContext(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithContext(context.Background(), args, stdout, stderr)
}

func runWithContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
				return writeVersion(stdout, outputFmt)
			}
			return cmd.Help()
		},
	}
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)
	rootCmd.SetContext(ctx)

	rootCmd.PersistentFlags().StringVar(&outputFmt, "output", "table", "Output format (table|json|yaml)")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Print version information")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return writeVersion(stdout, outputFmt)
		},
	}

	rootCmd.AddCommand(
		versionCmd,
		newServeCommand(),
		admincli.NewCommand(stdout, &outputFmt),
		plugin.NewCommand(plugin.Options{
			OutputFormat: &outputFmt,
			Stdout:       stdout,
			Stderr:       stderr,
		}),
		support.NewCommand(support.Options{RootDir: envOr("TANPOPO_DATA_DIR", "data"), OutputFormat: &outputFmt, Stdout: stdout}),
		support.NewDoctorCommand(support.Options{RootDir: envOr("TANPOPO_DATA_DIR", "data"), OutputFormat: &outputFmt, Stdout: stdout}),
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

func writeVersion(stdout io.Writer, format string) error {
	switch format {
	case "json":
		return json.NewEncoder(stdout).Encode(map[string]string{"version": version, "commit": commit, "built": date})
	case "yaml":
		return yaml.NewEncoder(stdout).Encode(map[string]string{"version": version, "commit": commit, "built": date})
	case "table", "":
		_, err := fmt.Fprintf(stdout, "tanpopo version %s (commit=%s built=%s)\n", version, commit, date)
		return err
	default:
		return &testharness.ExitError{Code: testharness.ExitValidation, Message: "--output must be table, json, or yaml"}
	}
}
