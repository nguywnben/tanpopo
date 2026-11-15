// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package plugin implements the `tanpopo plugin` Extension SDK subcommand tree
// (init, validate, test, pack) over internal/extension/testharness (TP-IMP-0014).
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/nguywnben/tanpopo/internal/extension/testharness"
)

// Options configures CLI output formatting shared with the root `tanpopo` command.
type Options struct {
	OutputFormat *string
	Stdout       io.Writer
	Stderr       io.Writer
}

// NewCommand constructs the `tanpopo plugin` Cobra command tree.
func NewCommand(opts Options) *cobra.Command {
	pluginCmd := &cobra.Command{
		Use:   "plugin",
		Short: "Extension SDK scaffolder, validator, offline cassette runner & .tpkg packer",
		Long:  "Scaffold, schema-validate, contract-test offline (CT-01..18), and pack reproducible .tpkg extension packages.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	pluginCmd.AddCommand(
		newInitCmd(opts),
		newValidateCmd(opts),
		newTestCmd(opts),
		newPackCmd(opts),
	)
	return pluginCmd
}

func outputFormat(opts Options) string {
	if opts.OutputFormat == nil || strings.TrimSpace(*opts.OutputFormat) == "" {
		return "table"
	}
	return strings.ToLower(strings.TrimSpace(*opts.OutputFormat))
}

func validateOutputFormat(fmtName string) error {
	switch strings.ToLower(strings.TrimSpace(fmtName)) {
	case "", "table", "json", "yaml":
		return nil
	default:
		return &testharness.ExitError{
			Code:    testharness.ExitValidation,
			Message: fmt.Sprintf("unsupported --output format %q (expected table|json|yaml)", fmtName),
		}
	}
}

func newInitCmd(opts Options) *cobra.Command {
	var (
		tier  string
		dir   string
		force bool
	)
	cmd := &cobra.Command{
		Use:   "init <name>",
		Short: "Scaffold a starter Tier-1 Declarative or Tier-2 Wasm extension project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOutputFormat(outputFormat(opts)); err != nil {
				return err
			}
			res, err := testharness.ScaffoldExtension(testharness.ScaffoldOptions{
				Name:  args[0],
				Tier:  tier,
				Dir:   dir,
				Force: force,
			})
			if err != nil {
				return err
			}
			return renderScaffold(cmd, opts, res)
		},
	}
	cmd.Flags().StringVar(&tier, "tier", "1", "Extension execution tier (1|declarative or 2|wasm)")
	cmd.Flags().StringVarP(&dir, "dir", "o", "", "Target directory (defaults to ./<name>)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite non-empty target directory")
	return cmd
}

func newValidateCmd(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate [dir]",
		Short: "Validate manifest.yaml schema, namespaces, egress bounds & runtime artifact",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOutputFormat(outputFormat(opts)); err != nil {
				return err
			}
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			report, _, _, err := testharness.ValidateExtensionDir(cmd.Context(), target)
			if err != nil {
				return err
			}
			return renderValidate(cmd, opts, report)
		},
	}
	return cmd
}

func newTestCmd(opts Options) *cobra.Command {
	var cassettePath string
	cmd := &cobra.Command{
		Use:   "test [dir]",
		Short: "Execute 100% offline cassette contract tests inside loopback isolation",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOutputFormat(outputFormat(opts)); err != nil {
				return err
			}
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			report, err := testharness.TestExtension(ctx, testharness.TestOptions{
				Dir:          target,
				CassettePath: cassettePath,
			})
			if report != nil {
				_ = renderTestReport(cmd, opts, report)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&cassettePath, "cassette", "", "Run a single cassette file instead of scanning cassettes/")
	return cmd
}

func newPackCmd(opts Options) *cobra.Command {
	var outFile string
	cmd := &cobra.Command{
		Use:   "pack [dir]",
		Short: "Validate and build a bit-for-bit reproducible .tpkg archive",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOutputFormat(outputFormat(opts)); err != nil {
				return err
			}
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			res, err := testharness.PackExtension(ctx, target, outFile)
			if err != nil {
				return err
			}
			return renderPackResult(cmd, opts, res)
		},
	}
	cmd.Flags().StringVarP(&outFile, "out", "o", "", "Output .tpkg file path")
	return cmd
}

func outWriter(cmd *cobra.Command, opts Options) io.Writer {
	if opts.Stdout != nil {
		return opts.Stdout
	}
	if cmd != nil {
		return cmd.OutOrStdout()
	}
	return io.Discard
}

func renderStructured(w io.Writer, opts Options, v any) error {
	switch outputFormat(opts) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case "yaml":
		raw, err := yaml.Marshal(v)
		if err != nil {
			return err
		}
		_, err = w.Write(raw)
		return err
	default:
		return nil
	}
}

func renderScaffold(cmd *cobra.Command, opts Options, r *testharness.ScaffoldResult) error {
	w := outWriter(cmd, opts)
	if fmtName := outputFormat(opts); fmtName == "json" || fmtName == "yaml" {
		return renderStructured(w, opts, r)
	}
	_, _ = fmt.Fprintf(w, "Initialized %s (%s) in %s (%d files)\n",
		testharness.SanitizeTerminalText(r.ExtensionID),
		testharness.SanitizeTerminalText(r.Tier),
		testharness.SanitizeTerminalText(r.OutputDir),
		len(r.Files),
	)
	return nil
}

func renderValidate(cmd *cobra.Command, opts Options, r *testharness.ValidationReport) error {
	w := outWriter(cmd, opts)
	if fmtName := outputFormat(opts); fmtName == "json" || fmtName == "yaml" {
		return renderStructured(w, opts, r)
	}
	_, _ = fmt.Fprintf(w, "EXTENSION\t%s\nVERSION\t%s\nTIER\t%s\nSTATUS\t%s\n",
		testharness.SanitizeTerminalText(r.ExtensionID),
		testharness.SanitizeTerminalText(r.Version),
		r.ExecutionTier,
		r.Status,
	)
	if r.Entrypoint != "" {
		_, _ = fmt.Fprintf(w, "ENTRYPOINT\t%s (sha256:%s)\n",
			testharness.SanitizeTerminalText(r.Entrypoint), r.EntrypointSHA256)
	}
	return nil
}

func renderTestReport(cmd *cobra.Command, opts Options, r *testharness.TestReport) error {
	w := outWriter(cmd, opts)
	if fmtName := outputFormat(opts); fmtName == "json" || fmtName == "yaml" {
		return renderStructured(w, opts, r)
	}
	_, _ = fmt.Fprintf(w, "CASE\tCAPABILITY\tSTATUS\tDURATION_MS\n")
	for _, c := range r.Cases {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\n",
			testharness.SanitizeTerminalText(c.CaseID),
			c.Capability,
			c.Status,
			c.DurationMs,
		)
	}
	keys := make([]string, 0, len(r.CapabilityBadges))
	for k := range r.CapabilityBadges {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		var parts []string
		for _, k := range keys {
			parts = append(parts, k+"="+r.CapabilityBadges[k])
		}
		_, _ = fmt.Fprintf(w, "BADGES\t%s\n", strings.Join(parts, " "))
	}
	_, _ = fmt.Fprintf(w, "SUMMARY\t%s (%d passed, %d failed, %d ms)\n",
		r.Status, r.CasesPassed, r.CasesFailed, r.DurationMs)
	return nil
}

func renderPackResult(cmd *cobra.Command, opts Options, r *testharness.PackResult) error {
	w := outWriter(cmd, opts)
	if fmtName := outputFormat(opts); fmtName == "json" || fmtName == "yaml" {
		return renderStructured(w, opts, r)
	}
	_, _ = fmt.Fprintf(w, "PACKED\t%s\nARCHIVE\t%s\nSHA256\t%s\nBYTES\t%d\nENTRIES\t%d\n",
		testharness.SanitizeTerminalText(r.ExtensionID),
		testharness.SanitizeTerminalText(r.OutputPath),
		r.SHA256,
		r.SizeBytes,
		r.EntryCount,
	)
	return nil
}
