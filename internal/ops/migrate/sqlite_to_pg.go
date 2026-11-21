// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package migrate implements database engine migration tooling including
// lossless, FORCE-RLS-compliant SQLite WAL to PostgreSQL 16+ migration with
// cryptographic DEK/secret trial decryption and audit JCS parity verification (CT-32).
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Register pure-Go SQLite driver for read-only source inspection.

	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
)

var (
	// ErrParityMismatch indicates post-migration row-count, currency sum, audit hash-chain,
	// or AEAD trial decryption verification failed; migration changes are rolled back atomically.
	ErrParityMismatch = errors.New("migrate: post-migration parity or cryptographic verification failed")

	// ErrTargetNotEmpty indicates the target PostgreSQL database already contains tenant rows.
	ErrTargetNotEmpty = errors.New("migrate: target PostgreSQL database is not empty")

	safeIdentRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
)

// MigrateOptions configures a lossless SQLite WAL -> PostgreSQL 16+ migration run.
type MigrateOptions struct {
	FromSQLite string
	ToPostgres string
	Verify     bool
	KEK        vault.KEKProvider
	NowMs      func() int64
}

// MigrateReport summarizes a completed (and verified, if Verify=true) migration.
type MigrateReport struct {
	Status                  string           `json:"status"`
	SourcePath              string           `json:"source_path"`
	TargetDSN               string           `json:"target_dsn"`
	TablesCopied            int              `json:"tables_copied"`
	RowsCopied              int64            `json:"rows_copied"`
	TableCounts             map[string]int64 `json:"table_counts"`
	AuditEventsVerified     int              `json:"audit_events_verified"`
	SecretEnvelopesVerified int              `json:"secret_envelopes_verified"`
	HardLimitSumMicros      int64            `json:"hard_limit_sum_micros"`
	SettledCostSumMicros    int64            `json:"settled_cost_sum_micros"`
	Verified                bool             `json:"verified"`
}

// topologicalTables lists all application tables in FK-safe copy order.
// schema_migrations is excluded because PostgreSQL manages its own schema_migrations ledger.
var topologicalTables = []string{
	"organizations",
	"principals",
	"admin_change_requests",
	"projects",
	"environments",
	"org_encryption_keys",
	"project_encryption_keys",
	"vault_pending_shred_cleanups",
	"vault_key_generations",
	"vault_shred_requests",
	"secret_credentials",
	"virtual_keys",
	"role_bindings",
	"oauth_authorization_sessions",
	"oauth_token_states",
	"oauth_token_leases",
	"extension_packages",
	"extension_versions",
	"extension_installations",
	"extension_revocations",
	"marketplace_feed_cursors",
	"marketplace_index_entries",
	"extension_revocation_actions",
	"provider_adapters",
	"provider_connections",
	"connection_secret_bindings",
	"agent_endpoints",
	"logical_models",
	"model_deployments",
	"response_cache_entries",
	"route_policies",
	"route_targets",
	"policies",
	"idempotency_records",
	"org_extension_policies",
	"mcp_servers",
	"mcp_tools",
	"mcp_tool_grants",
	"webhook_subscriptions",
	"webhook_deliveries",
	"webhook_event_outbox",
	"approval_requests",
	"budget_ledgers",
	"budget_reservations",
	"budget_reservation_ledgers",
	"valkey_hold_checkpoints",
	"a2a_tasks",
	"usage_settlements",
	"usage_rollups_hourly",
	"request_traces_meta",
	"debug_payload_blobs",
	"audit_events",
	"emergency_kill_switches",
	"config_changelog",
	"distributed_leases",
}

// MigrateSQLiteToPostgres executes a strictly read-only extract from SQLite WAL into
// PostgreSQL 16+ under a single atomic transaction while honoring all FORCE RLS policies.
// When opts.Verify is true, row counts, integer micro-USD sums, JCS audit hash chains,
// and Vault DEK/secret AEAD tags are verified before Commit(); any mismatch rolls back.
func MigrateSQLiteToPostgres(ctx context.Context, opts MigrateOptions) (*MigrateReport, error) {
	srcPath, err := parseSQLiteSourcePath(opts.FromSQLite)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.ToPostgres) == "" {
		return nil, fmt.Errorf("%w: target PostgreSQL DSN is required", port.ErrInvalidInput)
	}
	redactedDSN := postgres.RedactDSN(opts.ToPostgres)

	nowFn := opts.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}

	uriPath := filepath.ToSlash(srcPath)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{
		Scheme:   "file",
		Path:     uriPath,
		RawQuery: "mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(ON)",
	}
	srcDB, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("migrate: open read-only sqlite %q: %w", srcPath, err)
	}
	defer func() { _ = srcDB.Close() }()
	srcDB.SetMaxOpenConns(1)

	if err := verifyCleanSQLiteSource(ctx, srcDB); err != nil {
		return nil, err
	}

	pgStore, err := postgres.Open(ctx, postgres.Options{
		DSN:   opts.ToPostgres,
		NowMs: nowFn,
	})
	if err != nil {
		return nil, fmt.Errorf("migrate: open target postgres (%s): %w", redactedDSN, err)
	}
	defer func() { _ = pgStore.Close() }()

	var existingOrgs int64
	if err := pgStore.WithOperatorTx(ctx, func(checkTx *sql.Tx) error {
		return checkTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM organizations`).Scan(&existingOrgs)
	}); err != nil {
		return nil, fmt.Errorf("migrate: inspect target organizations (%s): %w", redactedDSN, err)
	}
	if existingOrgs > 0 {
		return nil, fmt.Errorf("%w: target PostgreSQL already contains %d organization(s)", ErrTargetNotEmpty, existingOrgs)
	}

	if err := ensureHistoricalPartitions(ctx, srcDB, pgStore.Partitions()); err != nil {
		return nil, err
	}

	tx, err := pgStore.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("migrate: begin target transaction (%s): %w", redactedDSN, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'true', true)`); err != nil {
		return nil, fmt.Errorf("migrate: set operator context: %w", err)
	}

	report := &MigrateReport{
		Status:      "MIGRATED",
		SourcePath:  srcPath,
		TargetDSN:   redactedDSN,
		TableCounts: make(map[string]int64, len(topologicalTables)),
		Verified:    opts.Verify,
	}

	var copiedTables []string
	var configChangelogCopied bool
	for _, table := range topologicalTables {
		if !safeIdentRe.MatchString(table) {
			return nil, fmt.Errorf("%w: invalid table name %q", port.ErrInvalidInput, table)
		}
		srcCols, err := sqliteTableColumns(ctx, srcDB, table)
		if err != nil {
			return nil, err
		}
		if len(srcCols) == 0 {
			continue // Table not created in older SQLite snapshot; PG table stays empty cleanly.
		}
		copiedTables = append(copiedTables, table)
		n, hasOrgCol, err := copyTableSQLiteToPGTx(ctx, srcDB, tx, table, srcCols)
		if err != nil {
			return nil, fmt.Errorf("migrate: copy table %q: %w", table, err)
		}
		_ = hasOrgCol
		report.TableCounts[table] = n
		report.RowsCopied += n
		report.TablesCopied++
		if table == "config_changelog" && n > 0 {
			configChangelogCopied = true
		}
	}

	if configChangelogCopied {
		if _, err := tx.ExecContext(ctx, `
			SELECT setval(
				pg_get_serial_sequence('config_changelog', 'config_epoch'),
				COALESCE((SELECT MAX(config_epoch) FROM config_changelog), 1),
				TRUE
			)`); err != nil {
			return nil, fmt.Errorf("migrate: sync config_changelog identity sequence: %w", err)
		}
	}

	if opts.Verify {
		if err := verifyMigrationParityTx(ctx, srcDB, pgStore.DB(), tx, copiedTables, opts.KEK, report); err != nil {
			return nil, err
		}
		report.Status = "VERIFIED"
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("migrate: commit transaction (%s): %w", redactedDSN, err)
	}
	committed = true
	return report, nil
}

func parseSQLiteSourcePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("%w: source SQLite path is required", port.ErrInvalidInput)
	}
	if strings.HasPrefix(trimmed, "sqlite://") {
		trimmed = strings.TrimPrefix(trimmed, "sqlite://")
	}
	if strings.TrimSpace(trimmed) == "" {
		return "", fmt.Errorf("%w: empty sqlite:// source path", port.ErrInvalidInput)
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: resolve sqlite path %q: %v", port.ErrInvalidInput, raw, err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("%w: inspect sqlite file %q: %v", port.ErrInvalidInput, abs, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: source sqlite path %q must be a regular non-symlink file", port.ErrInvalidInput, abs)
	}
	return abs, nil
}

func verifyCleanSQLiteSource(ctx context.Context, srcDB *sql.DB) error {
	var hasMig int
	if err := srcDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&hasMig); err != nil {
		return fmt.Errorf("migrate: inspect sqlite schema_migrations: %w", err)
	}
	if hasMig == 0 {
		return fmt.Errorf("%w: source sqlite database has no schema_migrations table", port.ErrDirtyMigration)
	}
	var dirty int
	if err := srcDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE dirty <> 0`).Scan(&dirty); err != nil {
		return fmt.Errorf("migrate: check dirty migrations in sqlite: %w", err)
	}
	if dirty > 0 {
		return fmt.Errorf("%w: source sqlite database has %d dirty migration(s)", port.ErrDirtyMigration, dirty)
	}
	return nil
}

func ensureHistoricalPartitions(ctx context.Context, srcDB *sql.DB, pm *postgres.PartitionManager) error {
	queries := []struct {
		table string
		col   string
	}{
		{table: "usage_settlements", col: "settled_at_ms"},
		{table: "request_traces_meta", col: "created_at_ms"},
		{table: "audit_events", col: "created_at_ms"},
	}
	seenMonths := make(map[string]struct{})
	for _, spec := range queries {
		cols, err := sqliteTableColumns(ctx, srcDB, spec.table)
		if err != nil || len(cols) == 0 {
			continue
		}
		q := fmt.Sprintf(`SELECT DISTINCT (%s / 86400000) FROM %s ORDER BY 1 ASC`, spec.col, spec.table)
		rows, err := srcDB.QueryContext(ctx, q)
		if err != nil {
			return fmt.Errorf("migrate: inspect %s timestamps: %w", spec.table, err)
		}
		var days []int64
		for rows.Next() {
			var day int64
			if serr := rows.Scan(&day); serr != nil {
				_ = rows.Close()
				return serr
			}
			days = append(days, day)
		}
		if rerr := rows.Err(); rerr != nil {
			_ = rows.Close()
			return rerr
		}
		_ = rows.Close()

		for _, day := range days {
			ts := time.UnixMilli(day * 86400000).UTC()
			suffix, _, _ := postgres.MonthBoundsUTC(ts)
			if _, ok := seenMonths[suffix]; ok {
				continue
			}
			seenMonths[suffix] = struct{}{}
			if err := pm.EnsureMonth(ctx, ts); err != nil {
				return fmt.Errorf("migrate: ensure historical partition %s: %w", suffix, err)
			}
		}
	}
	return nil
}

type pgColumnMeta struct {
	name       string
	dataType   string
	isIdentity bool
}

func sqliteTableColumns(ctx context.Context, srcDB *sql.DB, table string) (map[string]bool, error) {
	rows, err := srcDB.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return nil, fmt.Errorf("migrate: pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var (
			cid        int
			name, ctype string
			notnull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func pgTableColumnsTx(ctx context.Context, tx *sql.Tx, table string) ([]pgColumnMeta, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT column_name, data_type, is_identity
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1
		ORDER BY ordinal_position ASC`, table)
	if err != nil {
		return nil, fmt.Errorf("migrate: inspect pg columns for %s: %w", table, err)
	}
	defer rows.Close()

	var cols []pgColumnMeta
	for rows.Next() {
		var c pgColumnMeta
		var isID string
		if err := rows.Scan(&c.name, &c.dataType, &isID); err != nil {
			return nil, err
		}
		c.isIdentity = strings.EqualFold(isID, "YES")
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func copyTableSQLiteToPGTx(ctx context.Context, srcDB *sql.DB, tx *sql.Tx, table string, srcCols map[string]bool) (int64, bool, error) {
	pgCols, err := pgTableColumnsTx(ctx, tx, table)
	if err != nil {
		return 0, false, err
	}
	var common []pgColumnMeta
	orgColIdx := -1
	hasIdentity := false
	for _, c := range pgCols {
		if !srcCols[c.name] || !safeIdentRe.MatchString(c.name) {
			continue
		}
		if c.name == "org_id" {
			orgColIdx = len(common)
		}
		if c.isIdentity {
			hasIdentity = true
		}
		common = append(common, c)
	}
	if len(common) == 0 {
		return 0, false, nil
	}

	colNames := make([]string, len(common))
	placeholders := make([]string, len(common))
	for i, c := range common {
		colNames[i] = c.name
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	selectSQL := fmt.Sprintf(`SELECT %s FROM %s`, strings.Join(colNames, ", "), table)
	switch {
	case table == "a2a_tasks":
		selectSQL += ` ORDER BY COALESCE(org_id, '') ASC, hop_count ASC, created_at_ms ASC, id ASC`
	case orgColIdx >= 0:
		selectSQL += ` ORDER BY COALESCE(org_id, '') ASC`
	}

	rows, err := srcDB.QueryContext(ctx, selectSQL)
	if err != nil {
		return 0, orgColIdx >= 0, fmt.Errorf("select from sqlite %s: %w", table, err)
	}
	defer rows.Close()

	overriding := ""
	if hasIdentity {
		overriding = " OVERRIDING SYSTEM VALUE"
	}
	insertSQL := fmt.Sprintf(
		`INSERT INTO %s (%s)%s VALUES (%s)`,
		table, strings.Join(colNames, ", "), overriding, strings.Join(placeholders, ", "),
	)

	var (
		copied     int64
		lastOrgSet string
		stmt       *sql.Stmt
	)
	stmt, err = tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return 0, orgColIdx >= 0, fmt.Errorf("prepare pg insert %s: %w", table, err)
	}
	defer func() { _ = stmt.Close() }()

	rawVals := make([]any, len(common))
	scanPtrs := make([]any, len(common))
	for i := range rawVals {
		scanPtrs[i] = &rawVals[i]
	}

	for rows.Next() {
		if err := rows.Scan(scanPtrs...); err != nil {
			return copied, orgColIdx >= 0, fmt.Errorf("scan sqlite row %s: %w", table, err)
		}
		if orgColIdx >= 0 {
			if rawVals[orgColIdx] != nil {
				orgStr := fmt.Sprint(rawVals[orgColIdx])
				if orgStr != "" && orgStr != lastOrgSet {
					if _, serr := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, orgStr); serr != nil {
						return copied, true, fmt.Errorf("set app.current_org_id for %s: %w", table, serr)
					}
					lastOrgSet = orgStr
				}
			}
		}
		for i, c := range common {
			if c.dataType == "boolean" && rawVals[i] != nil {
				rawVals[i] = toPGPredicateBool(rawVals[i])
			}
		}
		if _, err := stmt.ExecContext(ctx, rawVals...); err != nil {
			return copied, orgColIdx >= 0, fmt.Errorf("insert pg row %s: %w", table, err)
		}
		copied++
	}
	if err := rows.Err(); err != nil {
		return copied, orgColIdx >= 0, err
	}
	return copied, orgColIdx >= 0, nil
}

func toPGPredicateBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	case int:
		return x != 0
	case []byte:
		s := strings.TrimSpace(string(x))
		return s == "1" || strings.EqualFold(s, "true")
	case string:
		s := strings.TrimSpace(x)
		return s == "1" || strings.EqualFold(s, "true")
	default:
		return false
	}
}

func verifyMigrationParityTx(
	ctx context.Context,
	srcDB *sql.DB,
	pgDB *sql.DB,
	tx *sql.Tx,
	tables []string,
	kek vault.KEKProvider,
	report *MigrateReport,
) error {
	orgRows, err := tx.QueryContext(ctx, `SELECT id FROM organizations ORDER BY id ASC`)
	if err != nil {
		return fmt.Errorf("migrate verify: list target organizations: %w", err)
	}
	var orgIDs []string
	for orgRows.Next() {
		var oid string
		if serr := orgRows.Scan(&oid); serr != nil {
			_ = orgRows.Close()
			return serr
		}
		orgIDs = append(orgIDs, oid)
	}
	if rerr := orgRows.Err(); rerr != nil {
		_ = orgRows.Close()
		return rerr
	}
	_ = orgRows.Close()

	// 1. Row-count parity across every copied table (honoring FORCE RLS per org).
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'true', true)`); err != nil {
		return err
	}
	for _, table := range tables {
		srcCols, err := sqliteTableColumns(ctx, srcDB, table)
		if err != nil {
			return err
		}
		var srcCount int64
		if err := srcDB.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table)).Scan(&srcCount); err != nil {
			return fmt.Errorf("migrate verify: count sqlite %s: %w", table, err)
		}

		var pgCount int64
		if srcCols["org_id"] && table != "config_changelog" {
			var nullOrgCount int64
			if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE org_id IS NULL`, table)).Scan(&nullOrgCount); err != nil {
				return fmt.Errorf("migrate verify: count pg null-org %s: %w", table, err)
			}
			pgCount += nullOrgCount
			for _, oid := range orgIDs {
				if _, serr := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, oid); serr != nil {
					return serr
				}
				var c int64
				if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE org_id = $1`, table), oid).Scan(&c); err != nil {
					return fmt.Errorf("migrate verify: count pg org %s (%s): %w", table, oid, err)
				}
				pgCount += c
			}
			if _, serr := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', '', true)`); serr != nil {
				return serr
			}
		} else {
			if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table)).Scan(&pgCount); err != nil {
				return fmt.Errorf("migrate verify: count pg %s: %w", table, err)
			}
		}

		if srcCount != pgCount {
			return fmt.Errorf("%w: table %q row count mismatch (sqlite=%d, postgres=%d)", ErrParityMismatch, table, srcCount, pgCount)
		}
	}

	// 2. Integer micro-USD & token accounting parity across all financial tables.
	if err := verifyFinancialParityTx(ctx, srcDB, tx, orgIDs, report); err != nil {
		return err
	}

	// 3. RFC 8785 JCS audit hash-chain continuity & count parity.
	var srcAuditCount int
	if err := srcDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events`).Scan(&srcAuditCount); err != nil {
		return fmt.Errorf("migrate verify: count sqlite audit_events: %w", err)
	}
	auditRep, err := audit.VerifyAuditChainTx(ctx, tx, port.DialectPostgres, audit.VerifyOptions{})
	if err != nil {
		return fmt.Errorf("%w: audit hash-chain verification failed: %v", ErrParityMismatch, err)
	}
	if auditRep.TotalVerified != srcAuditCount {
		return fmt.Errorf("%w: audit event verification count mismatch (sqlite=%d, verified=%d)", ErrParityMismatch, srcAuditCount, auditRep.TotalVerified)
	}
	report.AuditEventsVerified = auditRep.TotalVerified

	// 4. Vault DEK & Secret AEAD trial decryption (fail closed on missing KEK or tampered tag).
	var srcDEKCount, srcSecretCount int
	if err := srcDB.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM org_encryption_keys) + (SELECT COUNT(*) FROM project_encryption_keys)`).Scan(&srcDEKCount); err != nil {
		return fmt.Errorf("migrate verify: count sqlite DEKs: %w", err)
	}
	if err := srcDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM secret_credentials`).Scan(&srcSecretCount); err != nil {
		return fmt.Errorf("migrate verify: count sqlite secrets: %w", err)
	}
	if srcDEKCount > 0 || srcSecretCount > 0 {
		if kek == nil {
			keyID := strings.TrimSpace(os.Getenv("TANPOPO_MASTER_KEY_ID"))
			if keyID == "" {
				keyID = "kek_v1"
			}
			loadedKEK, kerr := vault.LoadStaticKEKFromEnv(keyID, os.LookupEnv)
			if kerr != nil {
				return fmt.Errorf("%w: Master KEK unavailable for cryptographic migration verification: %v", ErrParityMismatch, kerr)
			}
			defer loadedKEK.Close()
			kek = loadedKEK
		}
		vStore, verr := vault.NewStore(vault.Config{
			DB:            pgDB,
			KEK:           kek,
			PostShredHook: func(context.Context) error { return nil },
		})
		if verr != nil {
			return fmt.Errorf("%w: initialize vault verifier: %v", ErrParityMismatch, verr)
		}
		defer vStore.Close()

		if err := vStore.VerifyAllDEKEnvelopesTx(ctx, tx, port.DialectPostgres); err != nil {
			return fmt.Errorf("%w: DEK envelope verification failed: %v", ErrParityMismatch, err)
		}
		nSec, err := vStore.VerifyAllSecretEnvelopesTx(ctx, tx, port.DialectPostgres)
		if err != nil {
			return fmt.Errorf("%w: secret AEAD trial decryption failed: %v", ErrParityMismatch, err)
		}
		if nSec != srcSecretCount {
			return fmt.Errorf("%w: verified secret envelope count mismatch (sqlite=%d, verified=%d)", ErrParityMismatch, srcSecretCount, nSec)
		}
		report.SecretEnvelopesVerified = nSec
	}

	return nil
}

func verifyFinancialParityTx(
	ctx context.Context,
	srcDB *sql.DB,
	tx *sql.Tx,
	orgIDs []string,
	report *MigrateReport,
) error {
	var (
		srcLimit, srcHold, srcExact, srcEst, srcCarry int64
		pgLimit, pgHold, pgExact, pgEst, pgCarry      int64
	)
	if err := srcDB.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(hard_limit_micros),0),
		       COALESCE(SUM(reserved_hold_micros),0),
		       COALESCE(SUM(settled_exact_micros),0),
		       COALESCE(SUM(settled_estimated_micros),0),
		       COALESCE(SUM(negative_carryover_micros),0)
		FROM budget_ledgers`).Scan(&srcLimit, &srcHold, &srcExact, &srcEst, &srcCarry); err != nil {
		return fmt.Errorf("migrate verify: sum sqlite budget_ledgers: %w", err)
	}

	var srcResHold int64
	if err := srcDB.QueryRowContext(ctx, `SELECT COALESCE(SUM(hold_micros),0) FROM budget_reservations`).Scan(&srcResHold); err != nil {
		return fmt.Errorf("migrate verify: sum sqlite budget_reservations: %w", err)
	}
	var srcCost, srcInTok, srcOutTok int64
	if err := srcDB.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(total_cost_micros),0), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0)
		FROM usage_settlements`).Scan(&srcCost, &srcInTok, &srcOutTok); err != nil {
		return fmt.Errorf("migrate verify: sum sqlite usage_settlements: %w", err)
	}

	var pgResHold, pgCost, pgInTok, pgOutTok int64
	for _, oid := range orgIDs {
		if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, oid); err != nil {
			return err
		}
		var l, h, ex, es, ca int64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(hard_limit_micros),0),
			       COALESCE(SUM(reserved_hold_micros),0),
			       COALESCE(SUM(settled_exact_micros),0),
			       COALESCE(SUM(settled_estimated_micros),0),
			       COALESCE(SUM(negative_carryover_micros),0)
			FROM budget_ledgers WHERE org_id = $1`, oid).Scan(&l, &h, &ex, &es, &ca); err != nil {
			return fmt.Errorf("migrate verify: sum pg budget_ledgers (%s): %w", oid, err)
		}
		pgLimit += l
		pgHold += h
		pgExact += ex
		pgEst += es
		pgCarry += ca

		var rh int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(hold_micros),0) FROM budget_reservations WHERE org_id = $1`, oid).Scan(&rh); err != nil {
			return fmt.Errorf("migrate verify: sum pg budget_reservations (%s): %w", oid, err)
		}
		pgResHold += rh

		var sc, si, so int64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(total_cost_micros),0), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0)
			FROM usage_settlements WHERE org_id = $1`, oid).Scan(&sc, &si, &so); err != nil {
			return fmt.Errorf("migrate verify: sum pg usage_settlements (%s): %w", oid, err)
		}
		pgCost += sc
		pgInTok += si
		pgOutTok += so
	}

	if srcLimit != pgLimit || srcHold != pgHold || srcExact != pgExact || srcEst != pgEst || srcCarry != pgCarry {
		return fmt.Errorf("%w: budget_ledgers micro-USD sum mismatch (sqlite=[%d,%d,%d,%d,%d], pg=[%d,%d,%d,%d,%d])",
			ErrParityMismatch, srcLimit, srcHold, srcExact, srcEst, srcCarry, pgLimit, pgHold, pgExact, pgEst, pgCarry)
	}
	if srcResHold != pgResHold {
		return fmt.Errorf("%w: budget_reservations hold_micros sum mismatch (sqlite=%d, pg=%d)", ErrParityMismatch, srcResHold, pgResHold)
	}
	if srcCost != pgCost || srcInTok != pgInTok || srcOutTok != pgOutTok {
		return fmt.Errorf("%w: usage_settlements sum mismatch (sqlite=[%d,%d,%d], pg=[%d,%d,%d])",
			ErrParityMismatch, srcCost, srcInTok, srcOutTok, pgCost, pgInTok, pgOutTok)
	}
	report.HardLimitSumMicros = pgLimit
	report.SettledCostSumMicros = pgCost
	return nil
}
