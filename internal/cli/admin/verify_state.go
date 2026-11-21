// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	vkcluster "github.com/nguywnben/tanpopo/internal/cluster/valkey"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

// VerifyStateOptions configures `tanpopo admin verify-state` execution (DR-02).
type VerifyStateOptions struct {
	DBPath                 string
	PGDSN                  string
	ValkeyAddrs            []string
	ValkeyUsername         string
	ValkeyPassword         string
	ValkeyRequireTLS       bool
	CheckAuditChain        bool
	ReconcileOrphanedHolds bool
	RebuildValkeyState     bool
}

// VerifyStateReport summarizes audit integrity, orphan reclamation, and Valkey reconstruction.
type VerifyStateReport struct {
	Status              string                `json:"status" yaml:"status"`
	StorageEngine       string                `json:"storage_engine" yaml:"storage_engine"`
	AuditChainChecked   bool                  `json:"audit_chain_checked" yaml:"audit_chain_checked"`
	AuditChainValid     bool                  `json:"audit_chain_valid" yaml:"audit_chain_valid"`
	AuditEventsVerified int                   `json:"audit_events_verified" yaml:"audit_events_verified"`
	AuditFindings       []audit.TamperFinding `json:"audit_findings,omitempty" yaml:"audit_findings,omitempty"`
	OrphansReconciled   int                   `json:"orphans_reconciled" yaml:"orphans_reconciled"`
	ValkeyRebuilt       bool                  `json:"valkey_rebuilt" yaml:"valkey_rebuilt"`
	ValkeyHoldsRestored int                   `json:"valkey_holds_restored" yaml:"valkey_holds_restored"`
}

func newVerifyStateCommand(stdout io.Writer, loc i18n.Locale, outputFormat ...*string) *cobra.Command {
	var (
		dbPath           string
		pgDSN            string
		valkeyAddrsRaw   string
		valkeyUsername   string
		valkeyPassword   string
		valkeyRequireTLS bool
		checkAudit       bool
		reconcileOrphans bool
		rebuildValkey    bool
	)
	cmd := &cobra.Command{
		Use:   "verify-state",
		Short: i18n.T(loc, "cli.admin.verify_state.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format := "table"
			if len(outputFormat) > 0 && outputFormat[0] != nil {
				format = strings.ToLower(strings.TrimSpace(*outputFormat[0]))
			}
			if format != "" && format != "json" && format != "yaml" && format != "table" {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.err.output_format"), nil)
			}
			if strings.TrimSpace(pgDSN) == "" {
				pgDSN = strings.TrimSpace(env("TANPOPO_PG_DSN"))
			}
			if strings.TrimSpace(dbPath) == "" && pgDSN == "" {
				dbPath = strings.TrimSpace(env("TANPOPO_DB_PATH"))
			}
			if strings.TrimSpace(dbPath) == "" && strings.TrimSpace(pgDSN) == "" {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.admin.verify_state.err.store_required"), nil)
			}
			if strings.TrimSpace(valkeyAddrsRaw) == "" {
				valkeyAddrsRaw = strings.TrimSpace(env("TANPOPO_VALKEY_ADDRS"))
			}
			var valkeyAddrs []string
			for _, part := range strings.Split(valkeyAddrsRaw, ",") {
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					valkeyAddrs = append(valkeyAddrs, trimmed)
				}
			}
			effectiveRebuild := rebuildValkey
			if !cmd.Flags().Changed("rebuild-valkey-state") && len(valkeyAddrs) > 0 {
				effectiveRebuild = true
			}
			if effectiveRebuild && len(valkeyAddrs) == 0 {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.admin.verify_state.err.valkey_required"), nil)
			}

			report, err := VerifyState(cmd.Context(), VerifyStateOptions{
				DBPath:                 strings.TrimSpace(dbPath),
				PGDSN:                  strings.TrimSpace(pgDSN),
				ValkeyAddrs:            valkeyAddrs,
				ValkeyUsername:         strings.TrimSpace(valkeyUsername),
				ValkeyPassword:         valkeyPassword,
				ValkeyRequireTLS:       valkeyRequireTLS,
				CheckAuditChain:        checkAudit,
				ReconcileOrphanedHolds: reconcileOrphans,
				RebuildValkeyState:     effectiveRebuild,
			})
			if report != nil {
				switch format {
				case "json":
					_ = json.NewEncoder(stdout).Encode(report)
				case "yaml":
					_ = yaml.NewEncoder(stdout).Encode(report)
				default:
					_, _ = fmt.Fprintln(stdout, i18n.T(
						loc,
						"cli.admin.verify_state.output",
						"status", report.Status,
						"engine", report.StorageEngine,
						"events", fmt.Sprintf("%d", report.AuditEventsVerified),
						"orphans", fmt.Sprintf("%d", report.OrphansReconciled),
						"restored", fmt.Sprintf("%d", report.ValkeyHoldsRestored),
					))
				}
			}
			if err != nil {
				if errors.Is(err, audit.ErrAuditTampered) {
					return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.admin.verify_state.err.tampered"), err)
				}
				return cliError(testharness.ExitInternal, i18n.T(loc, "cli.admin.verify_state.err.failed"), err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "", i18n.T(loc, "cli.admin.verify_state.flag.db"))
	cmd.Flags().StringVar(&pgDSN, "pg-dsn", "", i18n.T(loc, "cli.admin.verify_state.flag.pg_dsn"))
	cmd.Flags().StringVar(&valkeyAddrsRaw, "valkey-addrs", "", i18n.T(loc, "cli.admin.verify_state.flag.valkey_addrs"))
	cmd.Flags().StringVar(&valkeyUsername, "valkey-username", env("TANPOPO_VALKEY_USERNAME"), i18n.T(loc, "cli.admin.verify_state.flag.valkey_username"))
	cmd.Flags().StringVar(&valkeyPassword, "valkey-password", env("TANPOPO_VALKEY_PASSWORD"), i18n.T(loc, "cli.admin.verify_state.flag.valkey_password"))
	cmd.Flags().BoolVar(&valkeyRequireTLS, "valkey-require-tls", false, i18n.T(loc, "cli.admin.verify_state.flag.valkey_require_tls"))
	cmd.Flags().BoolVar(&checkAudit, "check-audit-chain", true, i18n.T(loc, "cli.admin.verify_state.flag.check_audit"))
	cmd.Flags().BoolVar(&reconcileOrphans, "reconcile-orphaned-holds", true, i18n.T(loc, "cli.admin.verify_state.flag.reconcile_orphans"))
	cmd.Flags().BoolVar(&rebuildValkey, "rebuild-valkey-state", false, i18n.T(loc, "cli.admin.verify_state.flag.rebuild_valkey"))
	return cmd
}

// VerifyState performs DR-02 operational state verification and recovery across SQLite/PostgreSQL and Valkey Cluster.
func VerifyState(ctx context.Context, opts VerifyStateOptions) (*VerifyStateReport, error) {
	var (
		db     *sql.DB
		closeFn func() error
		engine string
	)
	if strings.TrimSpace(opts.PGDSN) != "" {
		pgStore, err := postgres.Open(ctx, postgres.Options{
			DSN:                      strings.TrimSpace(opts.PGDSN),
			SkipMigrate:              true,
			PartitionLookaheadMonths: -1,
		})
		if err != nil {
			return nil, fmt.Errorf("open PostgreSQL store: %w", err)
		}
		db = pgStore.RawDB()
		closeFn = pgStore.Close
		engine = "postgres"
	} else {
		dbPath := strings.TrimSpace(opts.DBPath)
		if dbPath == "" {
			return nil, errors.New("db path or pg-dsn is required")
		}
		if _, err := os.Stat(dbPath); err != nil {
			return nil, fmt.Errorf("inspect sqlite db path %s: %w", dbPath, err)
		}
		sqliteStore, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
		if err != nil {
			return nil, fmt.Errorf("open sqlite store: %w", err)
		}
		db = sqliteStore.RawDB()
		closeFn = sqliteStore.Close
		engine = "sqlite"
	}
	defer func() {
		if closeFn != nil {
			_ = closeFn()
		}
	}()

	report := &VerifyStateReport{
		Status:          "ok",
		StorageEngine:   engine,
		AuditChainValid: true,
	}

	if opts.CheckAuditChain {
		report.AuditChainChecked = true
		vr, err := audit.VerifyAuditChain(ctx, db, audit.VerifyOptions{})
		if vr != nil {
			report.AuditEventsVerified = vr.TotalVerified
			report.AuditChainValid = vr.Valid
			report.AuditFindings = vr.Findings
		}
		if err != nil {
			if errors.Is(err, audit.ErrAuditTampered) {
				report.Status = "tampered"
				report.AuditChainValid = false
			}
			return report, err
		}
	}

	if opts.ReconcileOrphanedHolds {
		auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db})
		if err != nil {
			return report, fmt.Errorf("initialize audit logger: %w", err)
		}
		ledgerSvc, err := ledger.NewService(ledger.Config{DB: db, Audit: auditLog})
		if err != nil {
			return report, fmt.Errorf("initialize ledger service: %w", err)
		}
		reaper, err := ledger.NewOrphanReservationReaper(ledgerSvc, 15*time.Second, 500)
		if err != nil {
			return report, fmt.Errorf("initialize orphan reaper: %w", err)
		}
		for {
			count, err := reaper.ReapOnce(ctx)
			if err != nil {
				return report, fmt.Errorf("reconcile orphaned budget holds: %w", err)
			}
			report.OrphansReconciled += count
			if count < 500 {
				break
			}
		}
	}

	if opts.RebuildValkeyState {
		vkClient, err := vkcluster.Dial(vkcluster.Config{
			Addrs:      opts.ValkeyAddrs,
			Username:   opts.ValkeyUsername,
			Password:   opts.ValkeyPassword,
			RequireTLS: opts.ValkeyRequireTLS,
		})
		if err != nil {
			return report, fmt.Errorf("dial Valkey Cluster: %w", err)
		}
		defer vkClient.Close()
		if err := vkClient.Ping(ctx); err != nil {
			return report, fmt.Errorf("ping Valkey Cluster: %w", err)
		}
		restored, err := vkcluster.RebuildLedgerHoldsFromPostgres(ctx, db, vkClient, time.Now().UTC().UnixMilli())
		if err != nil {
			return report, fmt.Errorf("rebuild Valkey ledger holds: %w", err)
		}
		report.ValkeyRebuilt = true
		report.ValkeyHoldsRestored = restored
	}

	return report, nil
}
