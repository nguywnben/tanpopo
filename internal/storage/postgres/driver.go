// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package postgres implements the PostgreSQL 16+ dual-mode persistence backend
// using pgx/v5 with pgxpool connection pooling, cluster-safe advisory-locked
// migrations, non-privileged role enforcement, per-transaction FORCE RLS org
// scoping, and monthly RANGE partition lifecycle management (TP-IMP-0039).
package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	postgresmigrations "github.com/nguywnben/tanpopo/db/migrations/postgres"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const pgMigrationLockKey int64 = 6075498448399230769 // "TPPGMIG1"

var (
	migrationFileRe    = regexp.MustCompile(`^(\d+)_([A-Za-z0-9_]+)\.sql$`)
	kwPasswordRedacRe  = regexp.MustCompile(`(?i)(\b(?:ssl)?password\s*=\s*)('(?:\\'|[^'])*'|[^\s']+)`)
	urlUserinfoRedacRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/@\s]+:)[^\s]+(@[^/@\s]+)`)
	urlQueryPassRe     = regexp.MustCompile(`(?i)([?&](?:ssl)?password=)[^&\s#]*`)
)

// Options configures a PostgreSQL 16+ Store instance.
type Options struct {
	DSN                      string
	NowMs                    func() int64
	AuditSink                port.AuditSink
	MaxConns                 int32
	MinConns                 int32
	SkipMigrate              bool
	PartitionLookaheadMonths int           // 0 defaults to 3 months; < 0 disables lookahead during Open
	PartitionRefreshInterval time.Duration // Optional background EnsureLookahead interval (defaults to 6h when lookahead > 0 && !SkipMigrate; < 0 disables)
}

// Store implements port.Store over PostgreSQL 16+ via pgxpool + stdlib *sql.DB.
type Store struct {
	pool        *pgxpool.Pool
	db          *sql.DB
	nowMs       func() int64
	auditMu     sync.RWMutex
	auditSink   port.AuditSink
	partitions  *PartitionManager
	maintCancel context.CancelFunc
	maintWG     sync.WaitGroup

	closeOnce sync.Once
}

// Open connects to PostgreSQL using pgxpool, verifies the authenticated role has
// neither SUPERUSER nor BYPASSRLS, runs embedded migrations under a PostgreSQL
// advisory lock, and ensures current + 3-month lookahead RANGE partitions exist.
func Open(ctx context.Context, opts Options) (*Store, error) {
	if strings.TrimSpace(opts.DSN) == "" {
		return nil, fmt.Errorf("%w: empty PostgreSQL DSN", port.ErrInvalidInput)
	}
	if opts.AuditSink != nil {
		if _, ok := opts.AuditSink.(port.TxAuditSink); !ok {
			return nil, fmt.Errorf("%w: AuditSink must implement port.TxAuditSink", port.ErrInvalidInput)
		}
	}
	nowFn := opts.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}

	cfg, err := pgxpool.ParseConfig(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse DSN (%s): %s", RedactDSN(opts.DSN), RedactDSN(err.Error()))
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	} else if cfg.MaxConns <= 0 {
		cfg.MaxConns = 16
	}
	if opts.MinConns >= 0 {
		cfg.MinConns = opts.MinConns
	}
	if cfg.MinConns > cfg.MaxConns {
		cfg.MinConns = cfg.MaxConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect (%s): %w", RedactDSN(opts.DSN), err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping (%s): %w", RedactDSN(opts.DSN), err)
	}

	db := stdlib.OpenDBFromPool(pool)

	if err := CheckNonPrivilegedRole(ctx, db); err != nil {
		_ = db.Close()
		pool.Close()
		return nil, err
	}

	if !opts.SkipMigrate {
		if err := RunMigrations(ctx, pool, nowFn); err != nil {
			_ = db.Close()
			pool.Close()
			return nil, err
		}
	}

	pm := NewPartitionManager(db)
	lookahead := opts.PartitionLookaheadMonths
	if lookahead == 0 {
		lookahead = 3
	}
	if lookahead > 0 {
		ref := time.UnixMilli(nowFn()).UTC()
		if err := pm.EnsureLookahead(ctx, ref, lookahead); err != nil {
			_ = db.Close()
			pool.Close()
			return nil, fmt.Errorf("postgres: ensure lookahead partitions: %w", err)
		}
	}

	st := &Store{
		pool:       pool,
		db:         db,
		nowMs:      nowFn,
		auditSink:  opts.AuditSink,
		partitions: pm,
	}
	refreshInterval := opts.PartitionRefreshInterval
	if refreshInterval == 0 && !opts.SkipMigrate && lookahead > 0 {
		refreshInterval = 6 * time.Hour
	}
	if refreshInterval > 0 && lookahead > 0 {
		mctx, cancel := context.WithCancel(context.Background())
		st.maintCancel = cancel
		st.maintWG.Add(1)
		go func() {
			defer st.maintWG.Done()
			ticker := time.NewTicker(refreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-mctx.Done():
					return
				case <-ticker.C:
					runCtx, runCancel := context.WithTimeout(mctx, 15*time.Second)
					_ = pm.EnsureLookahead(runCtx, time.UnixMilli(nowFn()).UTC(), lookahead)
					runCancel()
				}
			}
		}()
	}
	return st, nil
}

// SetAuditSink attaches a fail-closed transactional AuditSink after store creation.
func (s *Store) SetAuditSink(sink port.AuditSink) error {
	if sink == nil {
		return fmt.Errorf("%w: non-nil port.TxAuditSink is required", port.ErrInvalidInput)
	}
	if _, ok := sink.(port.TxAuditSink); !ok {
		return fmt.Errorf("%w: AuditSink must implement port.TxAuditSink", port.ErrInvalidInput)
	}
	s.auditMu.Lock()
	s.auditSink = sink
	s.auditMu.Unlock()
	return nil
}

func (s *Store) getAuditSink() port.AuditSink {
	s.auditMu.RLock()
	defer s.auditMu.RUnlock()
	return s.auditSink
}

// RedactDSN strips credential material from URL or keyword/value PostgreSQL DSNs
// (including malformed URLs, query password/sslpassword, and escaped quotes)
// before emitting diagnostics or wrapped errors.
func RedactDSN(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
		if u, err := url.Parse(trimmed); err == nil {
			if u.User != nil {
				username := u.User.Username()
				if _, hasPass := u.User.Password(); hasPass {
					u.User = url.UserPassword(username, "***")
				}
			}
			if u.RawQuery != "" {
				u.RawQuery = urlQueryPassRe.ReplaceAllString(trimmed[strings.Index(trimmed, "?"):], "${1}***")
				u.RawQuery = strings.TrimPrefix(u.RawQuery, "?")
				if frag := strings.Index(u.RawQuery, "#"); frag >= 0 {
					u.RawQuery = u.RawQuery[:frag]
				}
			}
			return u.String()
		}
		redacted := urlUserinfoRedacRe.ReplaceAllString(trimmed, "${1}***${2}")
		redacted = urlQueryPassRe.ReplaceAllString(redacted, "${1}***")
		return kwPasswordRedacRe.ReplaceAllString(redacted, `${1}***`)
	}
	return kwPasswordRedacRe.ReplaceAllString(trimmed, `${1}***`)
}

// RowQuerier abstracts *sql.DB, *sql.Tx, or *sql.Conn for CheckNonPrivilegedRole.
type RowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// CheckNonPrivilegedRole verifies that the active PostgreSQL role is neither
// SUPERUSER nor BYPASSRLS so ENGINE-level FORCE ROW LEVEL SECURITY cannot be bypassed.
func CheckNonPrivilegedRole(ctx context.Context, q RowQuerier) error {
	if q == nil {
		return fmt.Errorf("%w: nil PostgreSQL query handle", port.ErrInvalidInput)
	}
	var (
		roleName       string
		rolSuper       bool
		rolBypassRLS   bool
		isSuperSetting string
	)
	const query = `
		SELECT
			current_user,
			r.rolsuper,
			r.rolbypassrls,
			COALESCE(current_setting('is_superuser', true), 'off')
		FROM pg_roles r
		WHERE r.rolname = current_user
	`
	if err := q.QueryRowContext(ctx, query).Scan(&roleName, &rolSuper, &rolBypassRLS, &isSuperSetting); err != nil {
		return fmt.Errorf("postgres: inspect current_user privileges: %w", err)
	}
	if rolSuper || rolBypassRLS || strings.EqualFold(isSuperSetting, "on") {
		return fmt.Errorf(
			"%w: authenticated role %q has rolsuper=%t rolbypassrls=%t is_superuser=%s",
			port.ErrPrivilegedRole, roleName, rolSuper, rolBypassRLS, isSuperSetting,
		)
	}
	return nil
}

type pgMigration struct {
	Version  int64
	Name     string
	SQL      string
	Checksum string
}

// RunMigrations applies all embedded PostgreSQL DDL migrations atomically under
// pg_advisory_lock so concurrent cluster startups serialize cleanly (FMEA-06).
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, nowMs func() int64) error {
	if pool == nil {
		return fmt.Errorf("%w: nil pgxpool", port.ErrInvalidInput)
	}
	if nowMs == nil {
		nowMs = func() int64 { return time.Now().UTC().UnixMilli() }
	}

	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		return err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("postgres: acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", pgMigrationLockKey); err != nil {
		return fmt.Errorf("postgres: acquire migration advisory lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", pgMigrationLockKey)
	}()

	const createLedgerSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version         BIGINT PRIMARY KEY,
    name            VARCHAR(128) NOT NULL,
    checksum_sha256 CHAR(64) NOT NULL,
    dirty           BOOLEAN NOT NULL DEFAULT FALSE,
    applied_at_ms   BIGINT NOT NULL
)`
	if _, err := conn.Exec(ctx, createLedgerSQL); err != nil {
		return fmt.Errorf("postgres: ensure schema_migrations ledger: %w", err)
	}

	rows, err := conn.Query(ctx, `SELECT version, name, checksum_sha256, dirty FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return fmt.Errorf("postgres: read schema_migrations: %w", err)
	}
	applied := make(map[int64]struct {
		name     string
		checksum string
		dirty    bool
	})
	for rows.Next() {
		var (
			ver      int64
			name     string
			checksum string
			dirty    bool
		)
		if err := rows.Scan(&ver, &name, &checksum, &dirty); err != nil {
			rows.Close()
			return fmt.Errorf("postgres: scan schema_migrations: %w", err)
		}
		if dirty {
			rows.Close()
			return fmt.Errorf("%w: migration %d (%s) is marked dirty", port.ErrDirtyMigration, ver, name)
		}
		applied[ver] = struct {
			name     string
			checksum string
			dirty    bool
		}{name: name, checksum: checksum, dirty: dirty}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("postgres: iterate schema_migrations: %w", err)
	}
	rows.Close()

	for _, mig := range migrations {
		if existing, ok := applied[mig.Version]; ok {
			if existing.checksum != mig.Checksum {
				return fmt.Errorf(
					"%w: checksum mismatch for version %d (%s): recorded=%s embedded=%s",
					port.ErrDirtyMigration, mig.Version, mig.Name, existing.checksum, mig.Checksum,
				)
			}
			continue
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("postgres: begin migration tx %d: %w", mig.Version, err)
		}
		_, execErr := tx.Conn().PgConn().Exec(ctx, mig.SQL).ReadAll()
		if execErr != nil {
			_ = tx.Rollback(context.Background())
			return fmt.Errorf("postgres: apply migration %04d_%s: %w", mig.Version, mig.Name, execErr)
		}
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO schema_migrations (version, name, checksum_sha256, dirty, applied_at_ms)
			 VALUES ($1, $2, $3, FALSE, $4)
			 ON CONFLICT (version) DO NOTHING`,
			mig.Version, mig.Name, mig.Checksum, nowMs(),
		); err != nil {
			_ = tx.Rollback(context.Background())
			return fmt.Errorf("postgres: record migration %04d_%s: %w", mig.Version, mig.Name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("postgres: commit migration %04d_%s: %w", mig.Version, mig.Name, err)
		}
	}

	return nil
}

func loadEmbeddedMigrations() ([]pgMigration, error) {
	entries, err := fs.ReadDir(postgresmigrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("postgres: read embedded migrations: %w", err)
	}
	var out []pgMigration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m := migrationFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("%w: invalid migration filename %q", port.ErrDirtyMigration, e.Name())
		}
		ver, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || ver <= 0 {
			return nil, fmt.Errorf("%w: invalid migration version in %q", port.ErrDirtyMigration, e.Name())
		}
		raw, err := fs.ReadFile(postgresmigrations.FS, e.Name())
		if err != nil {
			return nil, fmt.Errorf("postgres: read migration %q: %w", e.Name(), err)
		}
		normalized := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
		sum := sha256.Sum256(normalized)
		out = append(out, pgMigration{
			Version:  ver,
			Name:     m[2],
			SQL:      string(normalized),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// Pool returns the underlying pgxpool.Pool.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// DB returns a database/sql *sql.DB backed by the Store's pgxpool.
func (s *Store) DB() *sql.DB { return s.db }

// RawDB returns the database/sql *sql.DB bridge for Store consumers expecting RawDB().
func (s *Store) RawDB() *sql.DB { return s.db }

// Partitions returns the monthly RANGE partition lifecycle manager.
func (s *Store) Partitions() *PartitionManager { return s.partitions }

// Ping verifies PostgreSQL connectivity.
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres: store is closed")
	}
	return s.pool.Ping(ctx)
}

// Close stops background partition maintenance, releases the stdlib bridge, and closes pgxpool.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var closeErr error
	s.closeOnce.Do(func() {
		if s.maintCancel != nil {
			s.maintCancel()
			s.maintWG.Wait()
		}
		if s.db != nil {
			closeErr = s.db.Close()
		}
		if s.pool != nil {
			s.pool.Close()
		}
	})
	return closeErr
}

// WithOrgTx executes fn inside a *sql.Tx pinned via SET LOCAL app.current_org_id.
func (s *Store) WithOrgTx(ctx context.Context, orgID string, fn func(tx *sql.Tx) error) error {
	if strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("%w: org_id is required for RLS-scoped transaction", port.ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'false', true), set_config('app.current_org_id', $1, true)`, orgID); err != nil {
		return fmt.Errorf("postgres: set app.current_org_id: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// WithOperatorTx executes fn inside a *sql.Tx pinned with app.is_instance_operator='true'.
func (s *Store) WithOperatorTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', '', true), set_config('app.is_instance_operator', 'true', true)`); err != nil {
		return fmt.Errorf("postgres: set app.is_instance_operator: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
