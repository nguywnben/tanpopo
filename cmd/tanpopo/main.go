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
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	admincli "github.com/nguywnben/tanpopo/internal/cli/admin"
	opscli "github.com/nguywnben/tanpopo/internal/cli/ops"
	"github.com/nguywnben/tanpopo/internal/cli/plugin"
	"github.com/nguywnben/tanpopo/internal/cli/remote"
	"github.com/nguywnben/tanpopo/internal/cli/support"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
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

// extractLangFlag pre-scans raw CLI arguments for --lang <val> or --lang=<val>
// so Cobra help output, command descriptions, and flag descriptions render in
// the requested language before command execution begins.
func extractLangFlag(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--lang" && i+1 < len(args) {
			return args[i+1]
		}
		if val, ok := strings.CutPrefix(arg, "--lang="); ok {
			return val
		}
	}
	return ""
}

func localizedUsageTemplate(loc i18n.Locale) string {
	usageLbl := i18n.T(loc, "cli.help.usage")
	aliasesLbl := i18n.T(loc, "cli.help.aliases")
	examplesLbl := i18n.T(loc, "cli.help.examples")
	availLbl := i18n.T(loc, "cli.help.available_commands")
	flagsLbl := i18n.T(loc, "cli.help.flags")
	globalLbl := i18n.T(loc, "cli.help.global_flags")
	moreHelp := strings.ReplaceAll(i18n.T(loc, "cli.help.additional_help", "command", "{{.CommandPath}}"), "%", "%%")

	return fmt.Sprintf(`%s:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

%s:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

%s:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

%s:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

%s:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

%s:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

%s:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

`+moreHelp+`{{end}}
`, usageLbl, aliasesLbl, examplesLbl, availLbl, availLbl, flagsLbl, globalLbl)
}

func runWithContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	langArg := extractLangFlag(args)
	loc, langErr := i18n.ResolveCLI(langArg, os.LookupEnv)
	if langErr != nil {
		_, _ = fmt.Fprintf(stderr, "tanpopo: %s\n", testharness.SanitizeTerminalText(langErr.Error()))
		return testharness.ExitValidation
	}

	var (
		outputFmt   string
		langFlag    string
		showVersion bool
	)

	rootCmd := &cobra.Command{
		Use:           "tanpopo",
		Short:         i18n.T(loc, "cli.root.short"),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				return writeVersion(stdout, outputFmt, loc)
			}
			return cmd.Help()
		},
	}
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)
	rootCmd.SetContext(ctx)
	rootCmd.SetUsageTemplate(localizedUsageTemplate(loc))

	rootCmd.PersistentFlags().StringVar(&outputFmt, "output", "table", i18n.T(loc, "cli.flag.output"))
	rootCmd.PersistentFlags().StringVar(&langFlag, "lang", "auto", i18n.T(loc, "cli.flag.lang"))
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, i18n.T(loc, "cli.flag.version"))

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: i18n.T(loc, "cli.version.short"),
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return writeVersion(stdout, outputFmt, loc)
		},
	}

	rootCmd.AddCommand(
		versionCmd,
		newServeCommand(loc),
		admincli.NewCommandWithLocale(stdout, loc, &outputFmt),
		plugin.NewCommand(plugin.Options{
			OutputFormat: &outputFmt,
			Stdout:       stdout,
			Stderr:       stderr,
			Locale:       loc,
		}),
		support.NewCommand(support.Options{RootDir: envOr("TANPOPO_DATA_DIR", "data"), OutputFormat: &outputFmt, Stdout: stdout, Locale: loc}),
		support.NewDoctorCommand(support.Options{RootDir: envOr("TANPOPO_DATA_DIR", "data"), OutputFormat: &outputFmt, Stdout: stdout, Locale: loc}),
	)
	rootCmd.AddCommand(opscli.NewCommands(opscli.Options{
		DBPath:       envOr("TANPOPO_DB_PATH", filepath.Join(envOr("TANPOPO_DATA_DIR", "data"), "tanpopo.db")),
		DataRoot:     envOr("TANPOPO_DATA_DIR", "data"),
		LockfilePath: envOr("TANPOPO_LOCK_FILE", filepath.Join(envOr("TANPOPO_DATA_DIR", "data"), "tanpopo.lock")),
		OutputFormat: &outputFmt,
		Stdout:       stdout,
		Locale:       loc,
	})...)
	for _, command := range remote.NewCommands(remote.Options{
		OutputFormat: &outputFmt,
		Stdout:       stdout,
		Stderr:       stderr,
		Locale:       loc,
	}) {
		rootCmd.AddCommand(command)
	}

	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintf(stderr, "tanpopo: %s\n", testharness.SanitizeTerminalText(err.Error()))
		return testharness.CodeOf(err)
	}
	return testharness.ExitOK
}

func writeVersion(stdout io.Writer, format string, loc i18n.Locale) error {
	switch format {
	case "json":
		return json.NewEncoder(stdout).Encode(map[string]string{"version": version, "commit": commit, "built": date})
	case "yaml":
		return yaml.NewEncoder(stdout).Encode(map[string]string{"version": version, "commit": commit, "built": date})
	case "table", "":
		_, err := fmt.Fprintln(stdout, i18n.T(loc, "cli.version.table", "version", version, "commit", commit, "built", date))
		return err
	default:
		return &testharness.ExitError{Code: testharness.ExitValidation, Message: i18n.T(loc, "cli.err.output_format")}
	}
}
