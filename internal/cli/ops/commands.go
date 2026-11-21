// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package ops implements local lifecycle commands for backup, erasure, and
// upgrade preflight. Commands operate on the explicitly configured local store.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/ops/backup"
	"github.com/nguywnben/tanpopo/internal/ops/gitops"
	opsmigrate "github.com/nguywnben/tanpopo/internal/ops/migrate"
	"github.com/nguywnben/tanpopo/internal/ops/upgrade"
	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	storagemigrate "github.com/nguywnben/tanpopo/internal/storage/migrate"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

type Options struct {
	DBPath       string
	DataRoot     string
	LockfilePath string
	OutputFormat *string
	Stdout       io.Writer
	Locale       i18n.Locale
}

func resolveLocale(opts Options) i18n.Locale {
	if opts.Locale == "" {
		return i18n.LocaleEn
	}
	return opts.Locale
}

// NewCommands builds the supported local lifecycle command groups.
func NewCommands(opts Options) []*cobra.Command {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if strings.TrimSpace(opts.DBPath) == "" {
		opts.DBPath = "data/tanpopo.db"
	}
	if strings.TrimSpace(opts.DataRoot) == "" {
		opts.DataRoot = "data"
	}
	if strings.TrimSpace(opts.LockfilePath) == "" {
		opts.LockfilePath = filepath.Join(opts.DataRoot, "tanpopo.lock")
	}
	return []*cobra.Command{newBackupCommand(opts), newConfigCommand(opts), newDBCommand(opts), newUpgradeCommand(opts), newTenantCommand(opts), newVaultCommand(opts)}
}

func newConfigCommand(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	var desiredPath, appliedPath, lockPath string
	var artifacts []string
	group := &cobra.Command{Use: "config", Short: i18n.T(loc, "cli.ops.config.short"), RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	reconcile := &cobra.Command{
		Use: "reconcile", Short: i18n.T(loc, "cli.ops.config.reconcile.short"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(desiredPath) == "" || strings.TrimSpace(appliedPath) == "" || strings.TrimSpace(lockPath) == "" {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.config.reconcile.err.required"), nil)
			}
			format := outputFormat(opts.OutputFormat)
			if !validOutputFormat(format) {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.err.output_format"), nil)
			}
			paths := make(map[string]string, len(artifacts))
			for _, item := range artifacts {
				id, path, ok := strings.Cut(item, "=")
				id, path = strings.TrimSpace(id), strings.TrimSpace(path)
				if !ok || id == "" || path == "" || strings.Contains(path, "=") {
					return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.config.reconcile.err.artifact_format"), nil)
				}
				if _, duplicate := paths[id]; duplicate {
					return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.config.reconcile.err.artifact_dup"), nil)
				}
				paths[id] = path
			}
			if err := gitops.Reconcile(cmd.Context(), gitops.Options{
				DesiredConfigPath: desiredPath, AppliedConfigPath: appliedPath,
				LockfilePath: lockPath, ArtifactPaths: paths,
			}); err != nil {
				return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.ops.config.reconcile.err.failed"), err)
			}
			return writeResult(opts.Stdout, format, map[string]string{"status": "APPLIED", "config": appliedPath, "lockfile": lockPath})
		},
	}
	reconcile.Flags().StringVar(&desiredPath, "desired", filepath.Join(opts.DataRoot, "tanpopo.yaml"), i18n.T(loc, "cli.ops.config.reconcile.flag.desired"))
	reconcile.Flags().StringVar(&appliedPath, "applied", filepath.Join(opts.DataRoot, "tanpopo.applied.yaml"), i18n.T(loc, "cli.ops.config.reconcile.flag.applied"))
	reconcile.Flags().StringVar(&lockPath, "lockfile", opts.LockfilePath, i18n.T(loc, "cli.ops.config.reconcile.flag.lockfile"))
	reconcile.Flags().StringArrayVar(&artifacts, "artifact", nil, i18n.T(loc, "cli.ops.config.reconcile.flag.artifact"))
	group.AddCommand(reconcile)
	return group
}

func newVaultCommand(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	var newKeyID, newKeyEnv string
	group := &cobra.Command{Use: "vault", Short: i18n.T(loc, "cli.ops.vault.short"), RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	rotate := &cobra.Command{
		Use: "rotate-kek", Short: i18n.T(loc, "cli.ops.vault.rotate.short"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(newKeyID) == "" || strings.TrimSpace(newKeyEnv) == "" {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.vault.rotate.err.required"), nil)
			}
			format := outputFormat(opts.OutputFormat)
			if !validOutputFormat(format) {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.err.output_format"), nil)
			}
			summary, err := runKEKRotation(cmd.Context(), opts.DBPath, strings.TrimSpace(newKeyID), strings.TrimSpace(newKeyEnv), loc)
			if err != nil {
				return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.ops.vault.rotate.err.failed"), err)
			}
			return writeResult(opts.Stdout, format, summary)
		},
	}
	rotate.Flags().StringVar(&newKeyID, "new-kek-id", "", i18n.T(loc, "cli.ops.vault.rotate.flag.new_kek_id"))
	rotate.Flags().StringVar(&newKeyEnv, "new-key-env", "TANPOPO_MASTER_KEY_NEXT", i18n.T(loc, "cli.ops.vault.rotate.flag.new_key_env"))
	group.AddCommand(rotate)
	return group
}

type rotationReceipt struct {
	Status           string `json:"status" yaml:"status"`
	OldKEKID         string `json:"old_kek_id" yaml:"old_kek_id"`
	NewKEKID         string `json:"new_kek_id" yaml:"new_kek_id"`
	OrgDEKsRewrapped int    `json:"org_deks_rewrapped" yaml:"org_deks_rewrapped"`
	ConfigEpoch      int64  `json:"config_epoch" yaml:"config_epoch"`
	NextStep         string `json:"next_step" yaml:"next_step"`
}

func runKEKRotation(ctx context.Context, dbPath, newKeyID, newKeyEnv string, loc i18n.Locale) (rotationReceipt, error) {
	oldKeyID := strings.TrimSpace(env("TANPOPO_MASTER_KEY_ID"))
	if oldKeyID == "" {
		oldKeyID = "kek_v1"
	}
	kek, err := vault.LoadStaticKEKFromEnv(oldKeyID, os.LookupEnv)
	if err != nil {
		return rotationReceipt{}, err
	}
	defer kek.Close()
	newKeyText, ok := os.LookupEnv(newKeyEnv)
	if !ok || strings.TrimSpace(newKeyText) == "" {
		return rotationReceipt{}, fmt.Errorf("new Master KEK environment variable %q is unset", newKeyEnv)
	}
	newKey, err := vault.ParseMasterKeyString(newKeyText)
	if err != nil {
		return rotationReceipt{}, errors.New("new Master KEK is malformed")
	}
	defer clear(newKey)
	if newKeyID == oldKeyID {
		return rotationReceipt{}, errors.New("new Master KEK identifier must differ from the active identifier")
	}
	if err := kek.RegisterKey(newKeyID, newKey, true); err != nil {
		return rotationReceipt{}, err
	}
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath, SkipMigrate: true})
	if err != nil {
		return rotationReceipt{}, err
	}
	defer func() { _ = store.Close() }()
	keyVault, err := vault.NewStore(vault.Config{DB: store.RawDB(), KEK: kek, PostShredHook: store.CheckpointWAL})
	if err != nil {
		return rotationReceipt{}, err
	}
	defer keyVault.Close()
	summary, err := keyVault.RewrapAllOrgDEKs(ctx, oldKeyID, newKeyID)
	if err != nil {
		return rotationReceipt{}, err
	}
	return rotationReceipt{
		Status: "REWRAPPED_PENDING_OPERATOR_CUTOVER", OldKEKID: oldKeyID, NewKEKID: newKeyID,
		OrgDEKsRewrapped: summary.Rewrapped, ConfigEpoch: summary.ConfigEpoch,
		NextStep: i18n.T(loc, "cli.ops.vault.rotate.next_step"),
	}, nil
}

func newBackupCommand(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	var outputDir, lockfile, configPath, artifactDir, archivePath, destination string
	var verifySecretsDecrypt bool
	group := &cobra.Command{Use: "backup", Short: i18n.T(loc, "cli.ops.backup.short"), RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	create := &cobra.Command{
		Use: "create", Short: i18n.T(loc, "cli.ops.backup.create.short"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(outputDir) == "" || !validOutputFormat(outputFormat(opts.OutputFormat)) {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.backup.create.err.required"), nil)
			}
			store, keyVault, closeFn, err := openBackupRuntime(cmd.Context(), opts.DBPath)
			if err != nil {
				return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.ops.backup.create.err.init"), err)
			}
			defer closeFn()
			receipt, err := backup.CreateSQLite(cmd.Context(), store, keyVault, outputDir, lockfile, configPath, artifactDir, time.Now().UTC().UnixMilli())
			if err != nil {
				return cliError(testharness.ExitInternal, i18n.T(loc, "cli.ops.backup.create.err.failed"), err)
			}
			return writeResult(opts.Stdout, outputFormat(opts.OutputFormat), receipt)
		},
	}
	create.Flags().StringVar(&outputDir, "output-dir", filepath.Join(opts.DataRoot, "backups"), i18n.T(loc, "cli.ops.backup.create.flag.output_dir"))
	create.Flags().StringVar(&lockfile, "lockfile", filepath.Join(opts.DataRoot, "tanpopo.lock"), i18n.T(loc, "cli.ops.backup.create.flag.lockfile"))
	create.Flags().StringVar(&configPath, "config", filepath.Join(opts.DataRoot, "tanpopo.yaml"), i18n.T(loc, "cli.ops.backup.create.flag.config"))
	create.Flags().StringVar(&artifactDir, "artifact-dir", filepath.Join(opts.DataRoot, "plugins"), i18n.T(loc, "cli.ops.backup.create.flag.artifact_dir"))

	verify := &cobra.Command{
		Use: "verify", Short: i18n.T(loc, "cli.ops.backup.verify.short"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(archivePath) == "" || !validOutputFormat(outputFormat(opts.OutputFormat)) {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.backup.verify.err.required"), nil)
			}
			kek, err := openKEK()
			if err != nil {
				return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.ops.backup.verify.err.init"), err)
			}
			defer kek.Close()
			result, err := backup.Verify(cmd.Context(), archivePath, kek)
			if err != nil {
				return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.ops.backup.verify.err.failed"), err)
			}
			return writeResult(opts.Stdout, outputFormat(opts.OutputFormat), result)
		},
	}
	verify.Flags().StringVar(&archivePath, "archive", "", i18n.T(loc, "cli.ops.backup.verify.flag.archive"))

	restore := &cobra.Command{
		Use: "restore", Short: i18n.T(loc, "cli.ops.backup.restore.short"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(archivePath) == "" || strings.TrimSpace(destination) == "" || !validOutputFormat(outputFormat(opts.OutputFormat)) {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.backup.restore.err.required"), nil)
			}
			kek, err := openKEK()
			if err != nil {
				return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.ops.backup.restore.err.init"), err)
			}
			defer kek.Close()
			receipt, err := backup.RestoreWithOptions(cmd.Context(), archivePath, destination, kek, backup.RestoreOptions{
				VerifySecretsDecrypt: verifySecretsDecrypt,
			})
			if err != nil {
				return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.ops.backup.restore.err.failed"), err)
			}
			return writeResult(opts.Stdout, outputFormat(opts.OutputFormat), receipt)
		},
	}
	restore.Flags().StringVar(&archivePath, "archive", "", i18n.T(loc, "cli.ops.backup.restore.flag.archive"))
	restore.Flags().StringVar(&destination, "destination", "", i18n.T(loc, "cli.ops.backup.restore.flag.destination"))
	restore.Flags().BoolVar(&verifySecretsDecrypt, "verify-secrets-decrypt", false, i18n.T(loc, "cli.ops.backup.restore.flag.verify_secrets"))

	group.AddCommand(create, verify, restore)
	return group
}

func openBackupRuntime(ctx context.Context, dbPath string) (*sqlite.Store, *vault.Store, func(), error) {
	dbInfo, err := os.Lstat(dbPath)
	if err != nil || !dbInfo.Mode().IsRegular() || dbInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, nil, errors.New("backup requires an existing regular database")
	}
	kek, err := openKEK()
	if err != nil {
		return nil, nil, nil, err
	}
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath, SkipMigrate: true})
	if err != nil {
		kek.Close()
		return nil, nil, nil, err
	}
	keyVault, err := vault.NewStore(vault.Config{DB: store.RawDB(), KEK: kek, PostShredHook: store.CheckpointWAL})
	if err != nil {
		_ = store.Close()
		kek.Close()
		return nil, nil, nil, err
	}
	closeFn := func() {
		keyVault.Close()
		kek.Close()
		_ = store.Close()
	}
	return store, keyVault, closeFn, nil
}

func openKEK() (*vault.StaticKEKProvider, error) {
	keyID := strings.TrimSpace(env("TANPOPO_MASTER_KEY_ID"))
	if keyID == "" {
		keyID = "kek_v1"
	}
	kek, err := vault.LoadStaticKEKFromEnv(keyID, os.LookupEnv)
	if err != nil {
		return nil, err
	}
	return kek, nil
}

func newUpgradeCommand(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	var targetVersion string
	group := &cobra.Command{Use: "upgrade", Short: i18n.T(loc, "cli.ops.upgrade.short"), RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	preflight := &cobra.Command{
		Use:   "preflight",
		Short: i18n.T(loc, "cli.ops.upgrade.preflight.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(targetVersion) == "" {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.upgrade.preflight.err.required"), nil)
			}
			if _, err := installer.SemverMatchesRange(targetVersion, ">=0.0.0"); err != nil {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.upgrade.preflight.err.semver"), nil)
			}
			format := outputFormat(opts.OutputFormat)
			if !validOutputFormat(format) {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.err.output_format"), nil)
			}
			result, err := runPreflight(cmd.Context(), opts.DBPath, opts.LockfilePath, targetVersion)
			if err != nil {
				return cliError(testharness.ExitInternal, i18n.T(loc, "cli.ops.upgrade.preflight.err.failed"), err)
			}
			if err := writeResult(opts.Stdout, format, result); err != nil {
				return cliError(testharness.ExitInternal, i18n.T(loc, "cli.ops.upgrade.preflight.err.write"), err)
			}
			if !result.Compatible {
				return cliError(testharness.ExitContract, i18n.T(loc, "cli.ops.upgrade.preflight.err.blocked"), nil)
			}
			return nil
		},
	}
	preflight.Flags().StringVar(&targetVersion, "target-version", "", i18n.T(loc, "cli.ops.upgrade.preflight.flag.target"))
	group.AddCommand(preflight)
	return group
}

func runPreflight(ctx context.Context, dbPath, lockfilePath, targetVersion string) (upgrade.PreflightResult, error) {
	locked, err := lockfile.Read(lockfilePath)
	if err != nil {
		return upgrade.PreflightResult{}, fmt.Errorf("read immutable tanpopo.lock: %w", err)
	}
	dbInfo, err := os.Lstat(dbPath)
	if err != nil || !dbInfo.Mode().IsRegular() || dbInfo.Mode()&os.ModeSymlink != 0 {
		return upgrade.PreflightResult{}, errors.New("preflight requires an existing regular database and will not create one")
	}
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath, SkipMigrate: true})
	if err != nil {
		return upgrade.PreflightResult{}, err
	}
	defer func() { _ = store.Close() }()
	rows, err := store.RawDB().QueryContext(ctx, `
		SELECT ei.id, ep.canonical_name, ev.semver, ev.core_version_range
		FROM extension_installations ei
		JOIN extension_packages ep ON ep.id = ei.package_id
		JOIN extension_versions ev ON ev.id = ei.active_version_id
		WHERE ei.active_version_id IS NOT NULL
		ORDER BY ei.id`)
	if err != nil {
		return upgrade.PreflightResult{}, err
	}
	defer rows.Close()
	installed := make([]upgrade.InstalledExtension, 0)
	seen := make(map[string]bool, len(locked.Extensions))
	for rows.Next() {
		var ext upgrade.InstalledExtension
		if err := rows.Scan(&ext.InstallationID, &ext.PackageID, &ext.Version, &ext.CoreRange); err != nil {
			return upgrade.PreflightResult{}, err
		}
		entry, ok := locked.Extensions[ext.PackageID]
		if !ok {
			return upgrade.PreflightResult{}, fmt.Errorf("active extension %q is missing from tanpopo.lock", ext.PackageID)
		}
		if entry.Version != ext.Version {
			return upgrade.PreflightResult{}, fmt.Errorf("active extension %q does not match its locked version", ext.PackageID)
		}
		seen[ext.PackageID] = true
		installed = append(installed, ext)
	}
	if err := rows.Err(); err != nil {
		return upgrade.PreflightResult{}, err
	}
	for packageID, entry := range locked.Extensions {
		if !seen[packageID] {
			installed = append(installed, upgrade.InstalledExtension{
				InstallationID: "lockfile:" + packageID,
				PackageID:      packageID,
				Version:        entry.Version,
				CoreRange:      "invalid: locked extension is not active in the local database",
			})
		}
	}
	return upgrade.Evaluate(targetVersion, installed)
}

func newTenantCommand(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	var orgID, projectID, requestID string
	var generation int64
	group := &cobra.Command{Use: "tenant", Short: i18n.T(loc, "cli.ops.tenant.short"), RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	shred := &cobra.Command{
		Use:   "shred",
		Short: i18n.T(loc, "cli.ops.tenant.shred.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			orgID = strings.TrimSpace(orgID)
			projectID = strings.TrimSpace(projectID)
			format := outputFormat(opts.OutputFormat)
			if !validOutputFormat(format) {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.err.output_format"), nil)
			}
			if orgID == "" || generation < 1 {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.tenant.shred.err.required"), nil)
			}
			if requestID == "" {
				requestID = fmt.Sprintf("cli-shred-%d", time.Now().UTC().UnixNano())
			}
			if err := runShred(cmd.Context(), opts.DBPath, orgID, projectID, vault.ShredRequest{RequestID: requestID, ExpectedGeneration: generation}); err != nil {
				return cliError(testharness.ExitInternal, i18n.T(loc, "cli.ops.tenant.shred.err.failed"), err)
			}
			return writeResult(opts.Stdout, format, map[string]any{
				"status": "SHREDDED", "org_id": orgID, "project_id": projectID, "request_id": requestID,
				"completed_at": time.Now().UTC().Format(time.RFC3339Nano),
			})
		},
	}
	shred.Flags().StringVar(&orgID, "org", "", i18n.T(loc, "cli.ops.tenant.shred.flag.org"))
	shred.Flags().StringVar(&projectID, "project", "", i18n.T(loc, "cli.ops.tenant.shred.flag.project"))
	shred.Flags().Int64Var(&generation, "generation", 0, i18n.T(loc, "cli.ops.tenant.shred.flag.generation"))
	shred.Flags().StringVar(&requestID, "request-id", "", i18n.T(loc, "cli.ops.tenant.shred.flag.request_id"))
	group.AddCommand(shred)
	return group
}

func runShred(ctx context.Context, dbPath, orgID, projectID string, request vault.ShredRequest) error {
	keyID := strings.TrimSpace(env("TANPOPO_MASTER_KEY_ID"))
	if keyID == "" {
		keyID = "kek_v1"
	}
	kek, err := vault.LoadStaticKEKFromEnv(keyID, os.LookupEnv)
	if err != nil {
		return fmt.Errorf("load local Master KEK: %w", err)
	}
	defer kek.Close()
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	keyVault, err := vault.NewStore(vault.Config{DB: store.RawDB(), KEK: kek, PostShredHook: store.CheckpointWAL})
	if err != nil {
		return err
	}
	defer keyVault.Close()
	started := time.Now()
	shredCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if projectID != "" {
		err = keyVault.ShredProjectDEK(shredCtx, orgID, projectID, request)
	} else {
		err = keyVault.ShredOrgDEK(shredCtx, orgID, request)
	}
	if err != nil {
		return err
	}
	if time.Since(started) >= 60*time.Second {
		return errors.New("cryptographic erasure exceeded the 60-second acceptance bound")
	}
	return nil
}

func writeResult(w io.Writer, format string, value any) error {
	switch format {
	case "json":
		return json.NewEncoder(w).Encode(value)
	case "yaml":
		return yaml.NewEncoder(w).Encode(value)
	case "table":
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	default:
		return fmt.Errorf("--output must be table, json, or yaml")
	}
}

func outputFormat(format *string) string {
	if format == nil || strings.TrimSpace(*format) == "" {
		return "table"
	}
	return strings.ToLower(strings.TrimSpace(*format))
}

func validOutputFormat(format string) bool {
	return format == "table" || format == "json" || format == "yaml"
}

func cliError(code int, message string, cause error) error {
	return &testharness.ExitError{Code: code, Message: message, Cause: cause}
}

func env(name string) string {
	value, _ := os.LookupEnv(name)
	return value
}

func newDBCommand(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	cmd := &cobra.Command{Use: "db", Short: i18n.T(loc, "cli.ops.db.short")}
	var (
		fromDSN string
		toDSN   string
		verify  bool
	)
	migrateCmd := &cobra.Command{
		Use:   "migrate-engine",
		Short: i18n.T(loc, "cli.ops.db.migrate.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(fromDSN) == "" || strings.TrimSpace(toDSN) == "" {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.db.migrate.err.required"), nil)
			}
			format := outputFormat(opts.OutputFormat)
			if !validOutputFormat(format) {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.err.output_format"), nil)
			}
			rep, err := opsmigrate.MigrateSQLiteToPostgres(cmd.Context(), opsmigrate.MigrateOptions{
				FromSQLite: strings.TrimSpace(fromDSN),
				ToPostgres: strings.TrimSpace(toDSN),
				Verify:     verify,
			})
			if err != nil {
				switch {
				case errors.Is(err, port.ErrDirtyMigration):
					return cliError(storagemigrate.ExitCodeDirtyMigration, i18n.T(loc, "cli.ops.db.migrate.err.dirty"), err)
				case errors.Is(err, port.ErrPrivilegedRole):
					return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.ops.db.migrate.err.security"), err)
				case errors.Is(err, opsmigrate.ErrParityMismatch):
					return cliError(testharness.ExitContract, i18n.T(loc, "cli.ops.db.migrate.err.parity"), err)
				case errors.Is(err, port.ErrInvalidInput):
					return cliError(testharness.ExitValidation, i18n.T(loc, "cli.ops.db.migrate.err.failed"), err)
				default:
					return cliError(testharness.ExitInternal, i18n.T(loc, "cli.ops.db.migrate.err.failed"), err)
				}
			}
			return writeResult(opts.Stdout, format, rep)
		},
	}
	migrateCmd.Flags().StringVar(&fromDSN, "from", "", i18n.T(loc, "cli.ops.db.migrate.flag.from"))
	migrateCmd.Flags().StringVar(&toDSN, "to", "", i18n.T(loc, "cli.ops.db.migrate.flag.to"))
	migrateCmd.Flags().BoolVar(&verify, "verify", true, i18n.T(loc, "cli.ops.db.migrate.flag.verify"))
	cmd.AddCommand(migrateCmd)
	return cmd
}
