// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	moderncsqlite "modernc.org/sqlite"

	sqlitemigrations "github.com/nguywnben/tanpopo/db/migrations/sqlite"
	"github.com/nguywnben/tanpopo/internal/storage/migrate"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// Config configures an embedded pure-Go SQLite WAL Store.
type Config struct {
	DBPath       string
	MigrationsFS fs.FS
	NowMs        func() int64
	AuditSink    port.AuditSink
	FSChecker    FSChecker
	SkipMigrate  bool
}

// Store implements port.Store over pure-Go SQLite WAL (modernc.org/sqlite).
type Store struct {
	db        *sql.DB
	dbPath    string
	nowMs     func() int64
	auditMu   sync.RWMutex
	auditSink port.AuditSink
	fsChecker FSChecker
	closeOnce sync.Once
	closeErr  error
}

// Open initializes a local SQLite WAL database with strict pragmas
// (journal_mode=WAL, busy_timeout=5000, synchronous=NORMAL, foreign_keys=ON),
// rejects network filesystems (NFS/SMB/CIFS/UNC), enforces 0700 directory permissions,
// and executes pending Expand-Only migrations unless SkipMigrate is set.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	if strings.TrimSpace(cfg.DBPath) == "" || strings.Contains(cfg.DBPath, ":memory:") {
		return nil, fmt.Errorf("%w: on-disk WAL DBPath is required", port.ErrInvalidInput)
	}
	if err := checkLocalPath(cfg.DBPath, cfg.FSChecker); err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("sqlite: resolve db path: %w", err)
	}
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("sqlite: create data directory %q: %w", dir, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, fmt.Errorf("sqlite: enforce 0700 permissions on %q: %w", dir, err)
		}
	}
	if err := checkLocalPath(absPath, cfg.FSChecker); err != nil {
		return nil, err
	}
	if err := checkLeafNotSymlink(absPath + "-wal"); err != nil {
		return nil, err
	}
	if err := checkLeafNotSymlink(absPath + "-shm"); err != nil {
		return nil, err
	}

	if cfg.NowMs == nil {
		cfg.NowMs = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	if cfg.MigrationsFS == nil {
		cfg.MigrationsFS = sqlitemigrations.FS
	}
	if cfg.AuditSink == nil {
		cfg.AuditSink = noopAuditSink{}
	} else if _, ok := cfg.AuditSink.(port.TxAuditSink); !ok {
		return nil, fmt.Errorf("%w: AuditSink must implement port.TxAuditSink", port.ErrInvalidInput)
	}

	dsn := buildWALDSN(absPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", absPath, err)
	}
	// Single writer/pool connection eliminates SQLITE_BUSY write contention while preserving WAL + VACUUM INTO.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: ping %q: %w", absPath, err)
	}

	if err := verifyWALPragmas(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(absPath, 0o600); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("sqlite: enforce 0600 permissions on %q: %w", absPath, err)
		}
		for _, companion := range []string{absPath + "-wal", absPath + "-shm"} {
			if cerr := os.Chmod(companion, 0o600); cerr != nil && !errors.Is(cerr, fs.ErrNotExist) {
				_ = db.Close()
				return nil, fmt.Errorf("sqlite: enforce 0600 permissions on %q: %w", companion, cerr)
			}
		}
	}

	if !cfg.SkipMigrate {
		if _, merr := migrate.Apply(ctx, db, cfg.MigrationsFS, cfg.NowMs); merr != nil {
			_ = db.Close()
			return nil, merr
		}
	}

	return &Store{
		db:        db,
		dbPath:    absPath,
		nowMs:     cfg.NowMs,
		auditSink: cfg.AuditSink,
		fsChecker: cfg.FSChecker,
	}, nil
}

func buildWALDSN(absPath string) string {
	slashPath := filepath.ToSlash(absPath)
	if !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	u := url.URL{
		Scheme: "file",
		Path:   slashPath,
	}
	q := u.Query()
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "secure_delete(1)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	return u.String()
}

func verifyWALPragmas(ctx context.Context, db *sql.DB) error {
	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return fmt.Errorf("sqlite: read PRAGMA journal_mode: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		return fmt.Errorf("sqlite: expected PRAGMA journal_mode=wal, got %q", journalMode)
	}

	var fk int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		return fmt.Errorf("sqlite: read PRAGMA foreign_keys: %w", err)
	}
	if fk != 1 {
		return fmt.Errorf("sqlite: expected PRAGMA foreign_keys=1, got %d", fk)
	}

	var busyMs int
	if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyMs); err != nil {
		return fmt.Errorf("sqlite: read PRAGMA busy_timeout: %w", err)
	}
	if busyMs < 5000 {
		return fmt.Errorf("sqlite: expected PRAGMA busy_timeout>=5000, got %d", busyMs)
	}

	var secDel int
	if err := db.QueryRowContext(ctx, "PRAGMA secure_delete").Scan(&secDel); err != nil {
		return fmt.Errorf("sqlite: read PRAGMA secure_delete: %w", err)
	}
	if secDel != 1 {
		return fmt.Errorf("sqlite: expected PRAGMA secure_delete=1, got %d", secDel)
	}
	return nil
}

// RawDB exposes the underlying *sql.DB pool for co-located internal/storage packages
// and integration tests verifying database-level triggers and constraints.
func (s *Store) RawDB() *sql.DB {
	return s.db
}

// DBPath returns the canonical on-disk database path used by local operational services.
func (s *Store) DBPath() string { return s.dbPath }

// WALBytes reports the current SQLite WAL file size, returning zero when no WAL file exists.
func (s *Store) WALBytes() (int64, error) {
	info, err := os.Stat(s.dbPath + "-wal")
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("sqlite: inspect WAL file: %w", err)
	}
	return info.Size(), nil
}

// ErrWALCheckpointBusy indicates PRAGMA wal_checkpoint(TRUNCATE) could not flush
// all WAL frames because an active reader/snapshot held a WAL read mark.
var ErrWALCheckpointBusy = errors.New("sqlite: WAL checkpoint blocked by busy reader or incomplete frames")

// CheckpointWAL checkpoints and truncates the SQLite WAL file so deleted rows/keys
// overwritten by PRAGMA secure_delete=ON do not linger in pre-shred WAL frames.
// Fails closed if SQLite reports busy != 0 or incomplete frame checkpointing.
func (s *Store) CheckpointWAL(ctx context.Context) error {
	var busy, logFrames, checkpointed int
	if err := s.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
		return fmt.Errorf("sqlite: PRAGMA wal_checkpoint(TRUNCATE): %w", err)
	}
	if busy != 0 || checkpointed != logFrames {
		return fmt.Errorf("%w (busy=%d, log=%d, checkpointed=%d)", ErrWALCheckpointBusy, busy, logFrames, checkpointed)
	}
	return nil
}

// Ping verifies database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close closes the SQLite connection pool idempotently and safely across concurrent callers.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		inUse := s.db.Stats().InUse
		s.closeErr = s.db.Close()
		if inUse != 0 {
			s.closeErr = errors.Join(s.closeErr, fmt.Errorf("sqlite: close requested with %d active connection(s)", inUse))
		}
	})
	return s.closeErr
}

// BackupTo creates a transactionally consistent hot online backup via SQLite VACUUM INTO.
func (s *Store) BackupTo(ctx context.Context, destPath string) error {
	if err := checkLocalPath(destPath, s.fsChecker); err != nil {
		return err
	}
	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return fmt.Errorf("sqlite: resolve backup path: %w", err)
	}
	if _, statErr := os.Lstat(absDest); statErr == nil {
		return fmt.Errorf("%w: backup destination %q already exists", port.ErrAlreadyExists, absDest)
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("sqlite: inspect backup destination %q: %w", absDest, statErr)
	}
	destDir := filepath.Dir(absDest)
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return fmt.Errorf("sqlite: create backup directory %q: %w", destDir, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(destDir, 0o700); err != nil {
			return fmt.Errorf("sqlite: enforce 0700 permissions on backup directory %q: %w", destDir, err)
		}
	}

	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", absDest); err != nil {
		return fmt.Errorf("sqlite: VACUUM INTO %q: %w", absDest, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(absDest, 0o600); err != nil {
			return fmt.Errorf("sqlite: enforce 0600 permissions on backup %q: %w", absDest, err)
		}
	}
	return nil
}

// BackupBytes creates a transactionally consistent SQLite hot snapshot in a
// shared in-memory database and serializes it without writing plaintext to a
// temporary filesystem. The returned bytes contain plaintext and callers must
// clear them as soon as the encrypted archive has been written.
func (s *Store) BackupBytes(ctx context.Context) ([]byte, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, fmt.Errorf("sqlite: generate in-memory backup id: %w", err)
	}
	uri := "file:tanpopo-backup-" + hex.EncodeToString(id[:]) + "?mode=memory&cache=shared"
	dstDB, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open in-memory backup database: %w", err)
	}
	defer dstDB.Close()
	dstConn, err := dstDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite: connect in-memory backup database: %w", err)
	}
	defer dstConn.Close()
	srcConn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite: connect source for in-memory backup: %w", err)
	}
	defer srcConn.Close()
	var snapshot []byte
	err = srcConn.Raw(func(srcDriverConn any) error {
		creator, ok := srcDriverConn.(interface {
			NewBackup(string) (*moderncsqlite.Backup, error)
		})
		if !ok {
			return errors.New("sqlite: driver does not support online in-memory backup")
		}
		backup, err := creator.NewBackup(uri)
		if err != nil {
			return fmt.Errorf("sqlite: initialize in-memory online backup: %w", err)
		}
		finished := false
		defer func() {
			if !finished {
				_ = backup.Finish()
			}
		}()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := backup.Step(128)
			if err != nil {
				return fmt.Errorf("sqlite: copy online backup pages: %w", err)
			}
			if !more {
				break
			}
		}
		if err := backup.Finish(); err != nil {
			finished = true
			return fmt.Errorf("sqlite: finish in-memory online backup: %w", err)
		}
		finished = true
		return dstConn.Raw(func(dstDriverConn any) error {
			serializer, ok := dstDriverConn.(interface{ Serialize() ([]byte, error) })
			if !ok {
				return errors.New("sqlite: driver does not support in-memory serialization")
			}
			snapshot, err = serializer.Serialize()
			return err
		})
	})
	if err != nil {
		clear(snapshot)
		return nil, err
	}
	if len(snapshot) == 0 {
		return nil, errors.New("sqlite: in-memory online backup produced an empty snapshot")
	}
	return snapshot, nil
}

// BeginTx starts an immediate write-safe SQLite transaction.
func (s *Store) BeginTx(ctx context.Context) (port.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sqlite: begin tx: %w", err)
	}
	return &sqliteTx{
		tx:        tx,
		store:     s,
		nowMs:     s.nowMs,
		auditSink: s.getAuditSink(),
	}, nil
}

// InTx executes fn inside an atomic transaction, rolling back on panic or error.
func (s *Store) InTx(ctx context.Context, fn func(tx port.Tx) error) (err error) {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit tx: %w", err)
	}
	committed = true
	return nil
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

func (s *Store) Tenancy() port.TenancyRepository {
	return &tenancyRepo{store: s, exec: s.db, nowMs: s.nowMs, auditSink: s.getAuditSink()}
}

func (s *Store) Changelog() port.ChangelogRepository {
	return &changelogRepo{exec: s.db, nowMs: s.nowMs}
}

func (s *Store) Leases() port.LeaseRepository {
	return &leaseRepo{store: s, exec: s.db}
}

func (s *Store) Audit() port.AuditSink {
	return s.getAuditSink()
}

type sqliteTx struct {
	tx        *sql.Tx
	store     *Store
	nowMs     func() int64
	auditSink port.AuditSink
}

func (t *sqliteTx) Commit() error   { return t.tx.Commit() }
func (t *sqliteTx) Rollback() error { return t.tx.Rollback() }
func (t *sqliteTx) Tenancy() port.TenancyRepository {
	return &tenancyRepo{exec: t.tx, nowMs: t.nowMs, auditSink: t.auditSink}
}
func (t *sqliteTx) Changelog() port.ChangelogRepository {
	return &changelogRepo{exec: t.tx, nowMs: t.nowMs}
}
func (t *sqliteTx) Leases() port.LeaseRepository {
	return &leaseRepo{exec: t.tx}
}
func (t *sqliteTx) Audit() port.AuditSink {
	return t.auditSink
}

type noopAuditSink struct{}

func (noopAuditSink) Append(context.Context, port.AuditEvent) error { return nil }

type sqlExecQuery interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func mapSQLiteError(err error) error {
	return mapSQLiteErrorWithOp(err, false)
}

func mapSQLiteErrorForDelete(err error) error {
	return mapSQLiteErrorWithOp(err, true)
}

func mapSQLiteErrorWithOp(err error, isDelete bool) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return port.ErrNotFound
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "UNIQUE constraint failed"):
		return fmt.Errorf("%w: %v", port.ErrAlreadyExists, err)
	case strings.Contains(msg, "FOREIGN KEY constraint failed"):
		if isDelete {
			return fmt.Errorf("%w: %v", port.ErrResourceInUse, err)
		}
		return fmt.Errorf("%w: %v", port.ErrNotFound, err)
	case strings.Contains(msg, "CHECK constraint failed"):
		return fmt.Errorf("%w: %v", port.ErrInvalidInput, err)
	default:
		return err
	}
}
