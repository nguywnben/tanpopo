// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package admin implements local-only administrative CLI operations.
package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

// NewCommand creates local administrative commands using the default English locale.
func NewCommand(stdout io.Writer, outputFormat ...*string) *cobra.Command {
	return NewCommandWithLocale(stdout, i18n.LocaleEn, outputFormat...)
}

// NewCommandWithLocale creates local administrative commands localized to loc.
// These operations never register an HTTP handler and only open the explicitly supplied local DB file.
func NewCommandWithLocale(stdout io.Writer, loc i18n.Locale, outputFormat ...*string) *cobra.Command {
	if loc == "" {
		loc = i18n.LocaleEn
	}
	adminCommand := &cobra.Command{Use: "admin", Short: i18n.T(loc, "cli.admin.short"), RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	var dbPath, ownerEmail string
	reset := &cobra.Command{
		Use:   "reset-local-owner",
		Short: i18n.T(loc, "cli.admin.reset.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(dbPath) == "" {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.admin.reset.err.db_required"), nil)
			}
			keyPath := strings.TrimSpace(env("TANPOPO_MASTER_KEY_FILE"))
			if keyPath == "" || strings.TrimSpace(env("TANPOPO_MASTER_KEY")) != "" {
				return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.admin.reset.err.kek_env"), nil)
			}
			keyID := strings.TrimSpace(env("TANPOPO_MASTER_KEY_ID"))
			if keyID == "" {
				keyID = "kek_v1"
			}
			proof, err := vault.LoadStaticKEKFromFile(keyID, keyPath)
			if err != nil {
				return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.admin.reset.err.kek_access"), err)
			}
			defer proof.Close()
			format := "table"
			if len(outputFormat) > 0 && outputFormat[0] != nil {
				format = strings.ToLower(strings.TrimSpace(*outputFormat[0]))
			}
			if format != "json" && format != "yaml" && format != "table" {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.err.output_format"), nil)
			}
			password, err := resetOwner(cmd.Context(), dbPath, strings.TrimSpace(ownerEmail), proof)
			if err != nil {
				return cliError(testharness.ExitInternal, i18n.T(loc, "cli.admin.reset.err.failed"), err)
			}
			var writeErr error
			switch format {
			case "json":
				writeErr = json.NewEncoder(stdout).Encode(map[string]string{"recovery_password": password})
			case "yaml":
				writeErr = yaml.NewEncoder(stdout).Encode(map[string]string{"recovery_password": password})
			case "table", "":
				_, writeErr = fmt.Fprintln(stdout, i18n.T(loc, "cli.admin.reset.output", "password", password))
			default:
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.err.output_format"), nil)
			}
			if writeErr != nil {
				return cliError(testharness.ExitInternal, i18n.T(loc, "cli.admin.reset.err.write"), writeErr)
			}
			return nil
		},
	}
	reset.Flags().StringVar(&dbPath, "db", "", i18n.T(loc, "cli.admin.reset.flag.db"))
	reset.Flags().StringVar(&ownerEmail, "email", "", i18n.T(loc, "cli.admin.reset.flag.email"))
	adminCommand.AddCommand(reset)
	return adminCommand
}

func resetOwner(ctx context.Context, dbPath, ownerEmail string, proof *vault.StaticKEKProvider) (string, error) {
	var rawPassword [32]byte
	if _, err := rand.Read(rawPassword[:]); err != nil {
		return "", fmt.Errorf("generate one-time password: %w", err)
	}
	password := base64.RawURLEncoding.EncodeToString(rawPassword[:])
	clear(rawPassword[:])
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", fmt.Errorf("hash one-time password: %w", err)
	}

	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		return "", err
	}
	defer func() { _ = store.Close() }()
	db := store.RawDB()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		return "", err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin recovery transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	query := `SELECT DISTINCT p.id, p.org_id FROM principals p
		JOIN role_bindings rb ON rb.principal_id = p.id
		WHERE p.kind = ? AND p.status = ? AND rb.role_name = ? AND rb.scope_type = ?
		AND rb.org_id = p.org_id AND rb.scope_id = p.org_id`
	args := []any{auth.PrincipalKindHumanLocal, auth.PrincipalStatusActive, "ORG_OWNER", "ORG"}
	if ownerEmail != "" {
		query += ` AND lower(p.email_or_sub) = lower(?)`
		args = append(args, ownerEmail)
	}
	query += ` ORDER BY p.id LIMIT 2`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return "", fmt.Errorf("find active local organization owner: %w", err)
	}
	type owner struct{ id, org string }
	var owners []owner
	for rows.Next() {
		var found owner
		if err := rows.Scan(&found.id, &found.org); err != nil {
			_ = rows.Close()
			return "", err
		}
		owners = append(owners, found)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return "", err
	}
	_ = rows.Close()
	if len(owners) == 0 {
		return "", errors.New("no matching active local ORG_OWNER exists")
	}
	if len(owners) > 1 {
		return "", errors.New("multiple active local ORG_OWNER accounts exist; specify --email")
	}
	selected := owners[0]
	if err := verifyOwnerKEK(ctx, tx, selected.org, proof); err != nil {
		return "", fmt.Errorf("verify Master KEK against the local database: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE principals SET
		password_argon2id = CASE WHEN id = ? THEN ? ELSE password_argon2id END,
		version = version + 1, updated_at_ms = ?
		WHERE kind IN (?, ?)`,
		selected.id, hash, time.Now().UTC().UnixMilli(), auth.PrincipalKindHumanLocal, auth.PrincipalKindHumanOIDC)
	if err != nil {
		return "", fmt.Errorf("replace owner password: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed < 1 {
		return "", errors.New("local principals changed during recovery")
	}
	auditID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return "", err
	}
	metadata, err := json.Marshal(map[string]string{"recovery_method": "local_cli"})
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().UnixMilli()
	if err := auditLog.AppendTx(ctx, tx, port.AuditEvent{
		ID:           auditID,
		CreatedAtMs:  now,
		OrgID:        nil,
		ActorKind:    "SYSTEM",
		ActorID:      "tanpopo_local_cli",
		EntryPoint:   port.AuditEntryPointCLI,
		EventType:    "LOCAL_BREAK_GLASS_OWNER_RESET",
		TargetType:   "principal",
		TargetID:     selected.id,
		MetadataJSON: string(metadata),
	}); err != nil {
		return "", fmt.Errorf("append recovery audit event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit recovery transaction: %w", err)
	}
	return password, nil
}

func verifyOwnerKEK(ctx context.Context, tx *sql.Tx, orgID string, proof *vault.StaticKEKProvider) error {
	if proof == nil {
		return errors.New("Master KEK proof is unavailable")
	}
	var keyID string
	var version int64
	var nonce, wrapped []byte
	if err := tx.QueryRowContext(ctx, `SELECT kek_key_id, dek_version, nonce, wrapped_org_dek FROM org_encryption_keys WHERE org_id = ?`, orgID).Scan(&keyID, &version, &nonce, &wrapped); err != nil {
		return fmt.Errorf("instance organization key is unavailable: %w", err)
	}
	activeID, err := proof.ActiveKeyID(ctx)
	if err != nil {
		return err
	}
	if keyID != activeID {
		return errors.New("configured Master KEK ID does not match the organization key envelope")
	}
	plain, err := proof.UnwrapDEK(ctx, keyID, orgID, version, nonce, wrapped)
	if err != nil {
		return errors.New("configured Master KEK does not unwrap the organization's protected key")
	}
	clear(plain)
	return nil
}

func cliError(code int, message string, cause error) error {
	return &testharness.ExitError{Code: code, Message: message, Cause: cause}
}

func env(name string) string {
	value, _ := os.LookupEnv(name)
	return value
}
