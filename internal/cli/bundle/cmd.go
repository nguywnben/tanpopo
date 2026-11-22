// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package bundle

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
)

// Options configures CLI output formatting and dependencies for `tanpopo bundle`.
type Options struct {
	OutputFormat   *string
	Stdout         io.Writer
	Stderr         io.Writer
	DataDir        string
	DBPath         string
	TanpopoVersion string
	Locale         i18n.Locale
}

func resolveLocale(opts Options) i18n.Locale {
	if opts.Locale == "" {
		return i18n.LocaleEn
	}
	return opts.Locale
}

func outputFormat(opts Options) string {
	if opts.OutputFormat == nil || strings.TrimSpace(*opts.OutputFormat) == "" {
		return "table"
	}
	return strings.ToLower(strings.TrimSpace(*opts.OutputFormat))
}

func validateOutputFormat(loc i18n.Locale, fmtName string) error {
	switch strings.ToLower(strings.TrimSpace(fmtName)) {
	case "", "table", "json", "yaml":
		return nil
	default:
		return &testharness.ExitError{
			Code:    testharness.ExitValidation,
			Message: i18n.T(loc, "cli.err.unsupported_output", "format", fmtName),
		}
	}
}

func writeStructured(out io.Writer, format string, v any, tableLine string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return json.NewEncoder(out).Encode(v)
	case "yaml":
		return yaml.NewEncoder(out).Encode(v)
	default:
		_, err := fmt.Fprintln(out, tableLine)
		return err
	}
}

// NewCommand constructs the `tanpopo bundle` Cobra subcommand tree.
func NewCommand(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	root := &cobra.Command{
		Use:   "bundle",
		Short: i18n.T(loc, "cli.bundle.short"),
		Long:  i18n.T(loc, "cli.bundle.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.AddCommand(
		newExportCmd(opts),
		newVerifyCmd(opts),
		newImportCmd(opts),
	)
	return root
}

func newExportCmd(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	var (
		lockPath     string
		packagesDir  string
		signingKey   string
		publicKey    string
		indexFeed    string
		revocFeed    string
		includeRevoc bool
		sboms        []string
		artifacts    []string
		outputPath   string
		formatFlag   string
	)
	cmd := &cobra.Command{
		Use:   "export",
		Short: i18n.T(loc, "cli.bundle.export.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmtName := strings.TrimSpace(formatFlag)
			if fmtName == "" {
				fmtName = outputFormat(opts)
			}
			if err := validateOutputFormat(loc, fmtName); err != nil {
				return err
			}
			res, err := ExportBundle(cmd.Context(), ExportOptions{
				LockfilePath:          lockPath,
				PackagesDir:           packagesDir,
				SigningKeyPath:        signingKey,
				PublicKeyPath:         publicKey,
				IndexFeedPath:         indexFeed,
				RevocationFeedPath:    revocFeed,
				IncludeRevocationFeed: includeRevoc,
				SBOMPaths:             sboms,
				ArtifactPaths:         artifacts,
				OutputPath:            outputPath,
				TanpopoVersion:        opts.TanpopoVersion,
			})
			if err != nil {
				return err
			}
			line := i18n.T(loc, "cli.bundle.export.summary",
				"path", res.OutputPath,
				"count", fmt.Sprintf("%d", res.ExtensionsCount),
				"sha256", res.BundleSHA256,
			)
			return writeStructured(opts.Stdout, fmtName, res, line)
		},
	}
	cmd.Flags().StringVar(&lockPath, "lockfile", "", i18n.T(loc, "cli.bundle.flag.lockfile"))
	cmd.Flags().StringVar(&packagesDir, "packages-dir", "", i18n.T(loc, "cli.bundle.flag.packages_dir"))
	cmd.Flags().StringVar(&signingKey, "signing-key", "", i18n.T(loc, "cli.bundle.flag.signing_key"))
	cmd.Flags().StringVar(&publicKey, "public-key", "", i18n.T(loc, "cli.bundle.flag.public_key"))
	cmd.Flags().StringVar(&indexFeed, "index-feed", "", i18n.T(loc, "cli.bundle.flag.index_feed"))
	cmd.Flags().StringVar(&revocFeed, "revocation-feed", "", i18n.T(loc, "cli.bundle.flag.revocation_feed"))
	cmd.Flags().BoolVar(&includeRevoc, "include-revocation-feed", false, i18n.T(loc, "cli.bundle.flag.include_revocation_feed"))
	cmd.Flags().StringSliceVar(&sboms, "sbom", nil, i18n.T(loc, "cli.bundle.flag.sbom"))
	cmd.Flags().StringSliceVar(&artifacts, "artifact", nil, i18n.T(loc, "cli.bundle.flag.artifact"))
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", i18n.T(loc, "cli.bundle.flag.output"))
	cmd.Flags().StringVar(&formatFlag, "format", "", i18n.T(loc, "cli.flag.output"))
	return cmd
}

func newVerifyCmd(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	var publicKey string
	var inputFlag string
	cmd := &cobra.Command{
		Use:   "verify [bundle.tpbundle]",
		Short: i18n.T(loc, "cli.bundle.verify.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmtName := outputFormat(opts)
			if err := validateOutputFormat(loc, fmtName); err != nil {
				return err
			}
			bundlePath := inputFlag
			if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
				bundlePath = args[0]
			}
			res, err := VerifyBundle(cmd.Context(), VerifyOptions{
				BundlePath:    bundlePath,
				PublicKeyPath: publicKey,
				DataDir:       opts.DataDir,
				LookupEnv:     os.LookupEnv,
			})
			if err != nil {
				return err
			}
			line := i18n.T(loc, "cli.bundle.verify.summary",
				"path", res.BundlePath,
				"entries", fmt.Sprintf("%d", res.VerifiedEntries),
				"key", res.KeyID,
			)
			return writeStructured(opts.Stdout, fmtName, res, line)
		},
	}
	cmd.Flags().StringVar(&publicKey, "public-key", "", i18n.T(loc, "cli.bundle.flag.public_key"))
	cmd.Flags().StringVar(&inputFlag, "input", "", i18n.T(loc, "cli.bundle.flag.input"))
	return cmd
}

func newImportCmd(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	var (
		publicKey string
		destDir   string
		dbPath    string
		mirrorTo  string
		inputFlag string
	)
	cmd := &cobra.Command{
		Use:   "import [bundle.tpbundle]",
		Short: i18n.T(loc, "cli.bundle.import.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmtName := outputFormat(opts)
			if err := validateOutputFormat(loc, fmtName); err != nil {
				return err
			}
			bundlePath := inputFlag
			if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
				bundlePath = args[0]
			}
			targetDB := dbPath
			if strings.TrimSpace(targetDB) == "" {
				targetDB = opts.DBPath
			}
			res, err := ImportBundle(cmd.Context(), ImportOptions{
				BundlePath:    bundlePath,
				PublicKeyPath: publicKey,
				DataDir:       opts.DataDir,
				DestDir:       destDir,
				DBPath:        targetDB,
				MirrorTo:      mirrorTo,
				LookupEnv:     os.LookupEnv,
			})
			if err != nil {
				return err
			}
			line := i18n.T(loc, "cli.bundle.import.summary",
				"count", fmt.Sprintf("%d", res.ImportedCount),
				"dest", res.DestDir,
			)
			return writeStructured(opts.Stdout, fmtName, res, line)
		},
	}
	cmd.Flags().StringVar(&publicKey, "public-key", "", i18n.T(loc, "cli.bundle.flag.public_key"))
	cmd.Flags().StringVar(&destDir, "dest-dir", "", i18n.T(loc, "cli.bundle.flag.dest_dir"))
	cmd.Flags().StringVar(&dbPath, "db", "", i18n.T(loc, "cli.bundle.flag.db"))
	cmd.Flags().StringVar(&mirrorTo, "mirror-to", "", i18n.T(loc, "cli.bundle.flag.mirror_to"))
	cmd.Flags().StringVar(&inputFlag, "input", "", i18n.T(loc, "cli.bundle.flag.input"))
	return cmd
}
