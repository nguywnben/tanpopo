// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

var testSchemaSeq atomic.Int64

func livePostgresDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if dsn == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_DSN is required when TANPOPO_REQUIRE_POSTGRES_TESTS=1")
		}
		return ""
	}
	return dsn
}

func isolatedPGSchemaDSN(t *testing.T, baseDSN string) string {
	t.Helper()
	ctx := context.Background()
	schemaName := fmt.Sprintf("tp_pgtest_%d_%d", time.Now().UnixNano(), testSchemaSeq.Add(1))

	db, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open base pg dsn: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, schemaName)); err != nil {
		t.Fatalf("create isolated schema %s: %v", schemaName, err)
	}

	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse pg url: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()
	isoDSN := u.String()

	t.Cleanup(func() {
		cleanDB, cerr := sql.Open("pgx", baseDSN)
		if cerr == nil {
			_, _ = cleanDB.ExecContext(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, schemaName))
			_ = cleanDB.Close()
		}
	})
	return isoDSN
}

func TestRedactDSN(t *testing.T) {
	got := postgres.RedactDSN("postgres://tanpopo_app:supersecret123@127.0.0.1:5432/tanpopo?sslmode=disable")
	if strings.Contains(got, "supersecret123") {
		t.Fatalf("password leaked in redacted URL DSN: %s", got)
	}
	if !strings.Contains(got, "tanpopo_app") {
		t.Fatalf("username unexpectedly stripped: %s", got)
	}

	gotKV := postgres.RedactDSN("host=127.0.0.1 user=tanpopo_app password=anothersecret dbname=tanpopo")
	if strings.Contains(gotKV, "anothersecret") {
		t.Fatalf("password leaked in keyword DSN: %s", gotKV)
	}
}

func TestMonthBoundsUTC(t *testing.T) {
	suffix, startMs, endMs := postgres.MonthBoundsUTC(time.Date(2026, 12, 19, 15, 30, 0, 0, time.UTC))
	if suffix != "2026_12" {
		t.Fatalf("suffix = %q, want 2026_12", suffix)
	}
	wantStart := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	wantEnd := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	if startMs != wantStart || endMs != wantEnd {
		t.Fatalf("bounds = [%d, %d), want [%d, %d)", startMs, endMs, wantStart, wantEnd)
	}
}

func TestRebindAndDetectNil(t *testing.T) {
	if got := port.DetectDialect(context.Background(), nil); got != port.DialectSQLite {
		t.Fatalf("DetectDialect(nil) = %q, want %q", got, port.DialectSQLite)
	}
	in := `SELECT * FROM t WHERE a = '?' AND b = ? -- ignore ? comment` + "\nAND c = ? /* ? */"
	want := `SELECT * FROM t WHERE a = '?' AND b = $1 -- ignore ? comment` + "\nAND c = $2 /* ? */"
	if got := port.Rebind(port.DialectPostgres, in); got != want {
		t.Fatalf("Rebind(postgres) =\n%s\nwant:\n%s", got, want)
	}
}

func TestPostgresSchemaMigrationsAndPartitions(t *testing.T) {
	baseDSN := livePostgresDSN(t)
	if baseDSN == "" {
		return
	}
	ctx := context.Background()
	dsn := isolatedPGSchemaDSN(t, baseDSN)

	fixedNow := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	st, err := postgres.Open(ctx, postgres.Options{
		DSN:   dsn,
		NowMs: func() int64 { return fixedNow.UnixMilli() },
	})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	if d := port.DetectDialect(ctx, st.DB()); d != port.DialectPostgres {
		t.Fatalf("DetectDialect(pg) = %q, want postgres", d)
	}

	// Verify 3-month lookahead partitions exist across all three partitioned tables.
	for _, parent := range []string{"usage_settlements", "request_traces_meta", "audit_events"} {
		parts, err := st.Partitions().ListPartitions(ctx, parent)
		if err != nil {
			t.Fatalf("ListPartitions(%s): %v", parent, err)
		}
		joined := strings.Join(parts, ",")
		for _, expect := range []string{"2026_11", "2026_12", "2027_01", "2027_02"} {
			if !strings.Contains(joined, parent+"_"+expect) {
				t.Fatalf("%s missing lookahead partition %s; got %v", parent, expect, parts)
			}
		}
	}

	// Verify immutability trigger rejects UPDATE/DELETE on audit_events and usage_settlements.
	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "imm-org", DisplayName: "Imm Org",
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	var audID string
	err = st.WithOrgTx(ctx, org.ID, func(tx *sql.Tx) error {
		audID = idgen.MustNew(idgen.PrefixAudit)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_events (
				id, created_at_ms, org_id, chain_key, seq_no,
				actor_kind, actor_id, entry_point, event_type, target_type, target_id,
				metadata_json, prev_hash, entry_hash
			) VALUES ($1, $2, $3, $3, 1, 'admin', 'usr_1', 'admin_api', 'org.create', 'organization', $3, '{}'::jsonb, $4, $4)`,
			audID, fixedNow.UnixMilli(), org.ID, strings.Repeat("a", 64),
		); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("audit insert check: %v", err)
	}
	err = st.WithOrgTx(ctx, org.ID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM audit_events WHERE id = $1`, audID)
		return err
	})
	if err == nil {
		t.Fatalf("expected DELETE on immutable audit_events to fail")
	}

	// Create historical expired partition (2025_01) and verify DetachAndDropExpired cleans it up.
	hist := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	if err := st.Partitions().EnsureMonth(ctx, hist); err != nil {
		t.Fatalf("EnsureMonth(2025-01): %v", err)
	}
	cutoff := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	dropped, err := st.Partitions().DetachAndDropExpired(ctx, "audit_events", cutoff)
	if err != nil {
		t.Fatalf("DetachAndDropExpired: %v", err)
	}
	if len(dropped) != 1 || dropped[0] != "audit_events_2025_01" {
		t.Fatalf("dropped = %v, want [audit_events_2025_01]", dropped)
	}
}

func TestPostgresDualDBStoragePortParity(t *testing.T) {
	baseDSN := livePostgresDSN(t)
	if baseDSN == "" {
		return
	}
	ctx := context.Background()
	var clockMs int64 = 1795176000000

	sqStore, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "parity.db"),
		NowMs:  func() int64 { return clockMs },
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = sqStore.Close() }()

	pgStore, err := postgres.Open(ctx, postgres.Options{
		DSN:   isolatedPGSchemaDSN(t, baseDSN),
		NowMs: func() int64 { return clockMs },
	})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	defer func() { _ = pgStore.Close() }()

	exercise := func(name string, store port.Store) {
		t.Helper()
		org, err := store.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
			ID: "org_01J00000000000000000000001", Slug: "acme", DisplayName: "Acme",
		})
		if err != nil {
			t.Fatalf("%s CreateOrganization: %v", name, err)
		}
		if _, err := store.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
			ID: "org_01J00000000000000000000002", Slug: "acme", DisplayName: "Dup",
		}); !errors.Is(err, port.ErrAlreadyExists) {
			t.Fatalf("%s duplicate slug err = %v, want ErrAlreadyExists", name, err)
		}

		updatedOrg, err := store.Tenancy().UpdateOrganization(ctx, org.ID, 1, port.UpdateOrgParams{
			DisplayName: strPtr("Acme Updated"),
		})
		if err != nil || updatedOrg.Version != 2 {
			t.Fatalf("%s UpdateOrganization: err=%v ver=%v", name, err, updatedOrg)
		}
		if _, err := store.Tenancy().UpdateOrganization(ctx, org.ID, 1, port.UpdateOrgParams{DisplayName: strPtr("Stale")}); !errors.Is(err, port.ErrVersionConflict) {
			t.Fatalf("%s stale UpdateOrganization = %v, want ErrVersionConflict", name, err)
		}

		prj, err := store.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{
			ID: "prj_01J00000000000000000000001", Slug: "core", DisplayName: "Core",
		})
		if err != nil {
			t.Fatalf("%s CreateProject: %v", name, err)
		}
		env, err := store.Tenancy().CreateEnvironment(ctx, org.ID, prj.ID, port.CreateEnvironmentParams{
			ID: "env_01J00000000000000000000001", Slug: "prod", RequireTestedCaps: true,
		})
		if err != nil || !env.RequireTestedCaps {
			t.Fatalf("%s CreateEnvironment: env=%+v err=%v", name, env, err)
		}
		if gotOrg, err := store.Tenancy().GetOrganization(ctx, org.ID); err != nil || gotOrg.DisplayName != "Acme Updated" {
			t.Fatalf("%s GetOrganization: %+v err=%v", name, gotOrg, err)
		}
		if gotSlug, err := store.Tenancy().GetOrganizationBySlug(ctx, org.Slug); err != nil || gotSlug.ID != org.ID {
			t.Fatalf("%s GetOrganizationBySlug: %+v err=%v", name, gotSlug, err)
		}
		if _, err := store.Tenancy().GetOrganization(ctx, "org_00000000000000000000000000"); !errors.Is(err, port.ErrNotFound) {
			t.Fatalf("%s missing GetOrganization = %v, want ErrNotFound", name, err)
		}
		if gotPrj, err := store.Tenancy().GetProject(ctx, org.ID, prj.ID); err != nil || gotPrj.ID != prj.ID {
			t.Fatalf("%s GetProject: %+v err=%v", name, gotPrj, err)
		}
		if gotEnv, err := store.Tenancy().GetEnvironment(ctx, org.ID, env.ID); err != nil || gotEnv.ID != env.ID {
			t.Fatalf("%s GetEnvironment: %+v err=%v", name, gotEnv, err)
		}
		if err := store.Tenancy().DeleteProject(ctx, org.ID, prj.ID, prj.Version+99); !errors.Is(err, port.ErrVersionConflict) {
			t.Fatalf("%s DeleteProject stale version = %v, want ErrVersionConflict", name, err)
		}
		orgID := org.ID
		if err := store.Audit().Append(ctx, port.AuditEvent{
			ID: "aud_01J00000000000000000000001", OrgID: &orgID,
			ActorKind: "admin", ActorID: "usr_1", EntryPoint: "admin_api",
			EventType: "project.create", TargetType: "project", TargetID: prj.ID,
			MetadataJSON: `{"ok":true}`,
		}); err != nil {
			t.Fatalf("%s Audit.Append: %v", name, err)
		}

		epoch, err := store.Changelog().Append(ctx, port.ChangelogEntry{
			OrgID: &org.ID, EntityKind: "project", EntityID: prj.ID, MutationType: port.MutationUpsert,
		})
		if err != nil || epoch < 1 {
			t.Fatalf("%s Changelog.Append: epoch=%d err=%v", name, epoch, err)
		}
		latest, err := store.Changelog().LatestEpoch(ctx)
		if err != nil || latest != epoch {
			t.Fatalf("%s LatestEpoch = %d, want %d (err=%v)", name, latest, epoch, err)
		}

		lease1, ok, err := store.Leases().AcquireOrRenew(ctx, "leader:reaper", "node-a", 1000, 5000)
		if err != nil || !ok || lease1.FencingToken != 1 {
			t.Fatalf("%s AcquireOrRenew initial: lease=%+v ok=%v err=%v", name, lease1, ok, err)
		}
		_, ok2, err := store.Leases().AcquireOrRenew(ctx, "leader:reaper", "node-b", 2000, 5000)
		if err != nil || ok2 {
			t.Fatalf("%s AcquireOrRenew hijack while active should fail: ok=%v err=%v", name, ok2, err)
		}
		lease3, ok3, err := store.Leases().AcquireOrRenew(ctx, "leader:reaper", "node-b", 7000, 5000)
		if err != nil || !ok3 || lease3.FencingToken != 2 {
			t.Fatalf("%s AcquireOrRenew after expiry: lease=%+v ok=%v err=%v", name, lease3, ok3, err)
		}
		if err := store.Leases().Release(ctx, "leader:reaper", "node-a", 1); !errors.Is(err, port.ErrVersionConflict) {
			t.Fatalf("%s Release stale token = %v, want ErrVersionConflict", name, err)
		}
		if err := store.Leases().Release(ctx, "leader:reaper", "node-b", lease3.FencingToken); err != nil {
			t.Fatalf("%s Release active holder: %v", name, err)
		}
		if _, err := store.Leases().Get(ctx, "leader:reaper"); !errors.Is(err, port.ErrNotFound) {
			t.Fatalf("%s Get released = %v, want ErrNotFound", name, err)
		}

		// List, update, delete, changelog ListSince, and InTx parity.
		orgs, err := store.Tenancy().ListOrganizations(ctx, 10)
		if err != nil || len(orgs) != 1 {
			t.Fatalf("%s ListOrganizations = %+v err=%v", name, orgs, err)
		}
		prjs, err := store.Tenancy().ListProjects(ctx, org.ID, 10)
		if err != nil || len(prjs) != 1 {
			t.Fatalf("%s ListProjects = %+v err=%v", name, prjs, err)
		}
		envs, err := store.Tenancy().ListEnvironments(ctx, org.ID, prj.ID)
		if err != nil || len(envs) != 1 {
			t.Fatalf("%s ListEnvironments = %+v err=%v", name, envs, err)
		}
		if _, err := store.Tenancy().UpdateProject(ctx, org.ID, prj.ID, prj.Version, port.UpdateProjectParams{
			DisplayName: strPtr("Core Two"),
		}); err != nil {
			t.Fatalf("%s UpdateProject: %v", name, err)
		}
		updEnv, err := store.Tenancy().UpdateEnvironment(ctx, org.ID, env.ID, env.Version, port.UpdateEnvironmentParams{
			Slug: strPtr("staging"),
		})
		if err != nil || updEnv.Slug != "staging" {
			t.Fatalf("%s UpdateEnvironment: %+v err=%v", name, updEnv, err)
		}
		entries, err := store.Changelog().ListSince(ctx, 0, 10)
		if err != nil || len(entries) != 7 {
			t.Fatalf("%s ListSince: %+v err=%v", name, entries, err)
		}
		if err := store.Ping(ctx); err != nil {
			t.Fatalf("%s Ping: %v", name, err)
		}
		if err := store.InTx(ctx, func(tx port.Tx) error {
			return tx.Tenancy().DeleteEnvironment(ctx, org.ID, env.ID, updEnv.Version)
		}); err != nil {
			t.Fatalf("%s InTx DeleteEnvironment: %v", name, err)
		}
		if err := store.Tenancy().DeleteProject(ctx, org.ID, prj.ID, 2); err != nil {
			t.Fatalf("%s DeleteProject after env deleted: %v", name, err)
		}
	}

	exercise("sqlite", sqStore)
	exercise("postgres", pgStore)
}

func TestPostgresForceRLSZeroRowFailClosedAndCrossOrgIsolation(t *testing.T) {
	baseDSN := livePostgresDSN(t)
	if baseDSN == "" {
		return
	}
	ctx := context.Background()
	st, err := postgres.Open(ctx, postgres.Options{DSN: isolatedPGSchemaDSN(t, baseDSN)})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "rls-org-a", DisplayName: "Org A",
	})
	if err != nil {
		t.Fatalf("CreateOrganization A: %v", err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "rls-org-b", DisplayName: "Org B",
	})
	if err != nil {
		t.Fatalf("CreateOrganization B: %v", err)
	}
	prjA, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{
		ID: idgen.MustNew(idgen.PrefixProject), Slug: "proj-a", DisplayName: "Proj A",
	})
	if err != nil {
		t.Fatalf("CreateProject A: %v", err)
	}

	// NEG-1a: Querying projects without app.current_org_id MUST fail closed (0 rows).
	var zeroCount int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&zeroCount); err != nil {
		t.Fatalf("query without org setting: %v", err)
	}
	if zeroCount != 0 {
		t.Fatalf("expected 0 rows without app.current_org_id under FORCE RLS, got %d", zeroCount)
	}

	// NEG-1b: Inside Org B transaction, Org A's project is invisible.
	err = st.WithOrgTx(ctx, orgB.ID, func(tx *sql.Tx) error {
		var cnt int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id = $1`, prjA.ID).Scan(&cnt); err != nil {
			return err
		}
		if cnt != 0 {
			return fmt.Errorf("cross-org read leaked %d rows", cnt)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cross-org RLS read check: %v", err)
	}

	// NEG-1c: Cross-org INSERT inside Org B transaction MUST fail FORCE RLS WITH CHECK.
	err = st.WithOrgTx(ctx, orgB.ID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO projects (id, org_id, slug, display_name, status, version, created_at_ms, updated_at_ms)
			VALUES ($1, $2, 'hijack', 'Hijack', 'ACTIVE', 1, 100, 100)`,
			idgen.MustNew(idgen.PrefixProject), orgA.ID,
		)
		return err
	})
	if err == nil {
		t.Fatalf("expected FORCE RLS WITH CHECK to block cross-org insert into orgA")
	}
}

func TestPostgresClusterConcurrentMigrationsFMEA06(t *testing.T) {
	baseDSN := livePostgresDSN(t)
	if baseDSN == "" {
		return
	}
	ctx := context.Background()
	dsn := isolatedPGSchemaDSN(t, baseDSN)

	const nodes = 8
	var wg sync.WaitGroup
	errs := make(chan error, nodes)
	for i := 0; i < nodes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := postgres.Open(ctx, postgres.Options{DSN: dsn})
			if err != nil {
				errs <- err
				return
			}
			_ = st.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent cluster Open failed (FMEA-06): %v", err)
	}

	st, err := postgres.Open(ctx, postgres.Options{DSN: dsn, SkipMigrate: true, PartitionLookaheadMonths: -1})
	if err != nil {
		t.Fatalf("re-open after concurrent migrations: %v", err)
	}
	defer func() { _ = st.Close() }()
	var migCount int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE dirty = FALSE`).Scan(&migCount); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if migCount != 2 {
		t.Fatalf("schema_migrations count = %d, want 2", migCount)
	}
}

func TestPostgresRefusesPrivilegedRoles(t *testing.T) {
	adminDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_ADMIN_DSN"))
	if adminDSN == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_ADMIN_DSN is required when TANPOPO_REQUIRE_POSTGRES_TESTS=1")
		}
		return
	}
	ctx := context.Background()

	// 1. SUPERUSER DSN must be refused immediately with ErrPrivilegedRole.
	_, err := postgres.Open(ctx, postgres.Options{DSN: adminDSN, SkipMigrate: true, PartitionLookaheadMonths: -1})
	if !errors.Is(err, port.ErrPrivilegedRole) {
		t.Fatalf("Open(SUPERUSER) = %v, want ErrPrivilegedRole", err)
	}

	// 2. Non-SUPERUSER role with BYPASSRLS must also be refused with ErrPrivilegedRole.
	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	defer func() { _ = adminDB.Close() }()

	roleName := fmt.Sprintf("tp_bypass_role_%d", time.Now().UnixNano())
	pass := "tp_test_bypass_pw"
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE ROLE %s WITH LOGIN NOSUPERUSER BYPASSRLS PASSWORD '%s'`, roleName, pass)); err != nil {
		t.Fatalf("create bypassrls role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(context.Background(), fmt.Sprintf(`DROP ROLE IF EXISTS %s`, roleName))
	})

	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	u.User = url.UserPassword(roleName, pass)
	_, err = postgres.Open(ctx, postgres.Options{DSN: u.String(), SkipMigrate: true, PartitionLookaheadMonths: -1})
	if !errors.Is(err, port.ErrPrivilegedRole) {
		t.Fatalf("Open(BYPASSRLS) = %v, want ErrPrivilegedRole", err)
	}
}

type stubTxAuditSink struct {
	events  []string
	badEp   string
	txCnt   int
}

func (s *stubTxAuditSink) Append(_ context.Context, ev port.AuditEvent) error {
	s.events = append(s.events, ev.ID)
	return nil
}

func (s *stubTxAuditSink) AppendTx(_ context.Context, _ *sql.Tx, ev port.AuditEvent) error {
	s.txCnt++
	if ev.EventType != "" && ev.EntryPoint != port.AuditEntryPointAdminAPI {
		s.badEp = ev.EntryPoint
	}
	s.events = append(s.events, ev.ID)
	return nil
}

func TestPostgresEdgePathsAndCoverage(t *testing.T) {
	ctx := context.Background()
	if _, err := postgres.Open(ctx, postgres.Options{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("Open(empty) = %v, want ErrInvalidInput", err)
	}
	if got := postgres.RedactDSN("://bad dsn"); got == "" {
		t.Fatalf("expected non-empty redacted fallback")
	}
	if got := postgres.RedactDSN("postgres://user@127.0.0.1:5432/db"); !strings.Contains(got, "user@") {
		t.Fatalf("expected user without password preserved, got %q", got)
	}

	baseDSN := livePostgresDSN(t)
	if baseDSN == "" {
		return
	}
	sink := &stubTxAuditSink{}
	st, err := postgres.Open(ctx, postgres.Options{
		DSN:       isolatedPGSchemaDSN(t, baseDSN),
		AuditSink: sink,
	})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	if st.Pool() == nil {
		t.Fatalf("expected non-nil Pool()")
	}
	if err := st.WithOrgTx(ctx, "", func(*sql.Tx) error { return nil }); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("WithOrgTx empty orgID = %v, want ErrInvalidInput", err)
	}
	if err := st.Partitions().EnsureTimestampCovered(ctx, time.Date(2028, 3, 15, 0, 0, 0, 0, time.UTC).UnixMilli()); err != nil {
		t.Fatalf("EnsureTimestampCovered: %v", err)
	}
	if _, err := st.Partitions().ListPartitions(ctx, "not_partitioned"); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("ListPartitions(invalid) = %v, want ErrInvalidInput", err)
	}
	if _, err := st.Partitions().DetachAndDropExpired(ctx, "not_partitioned", 1000); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("DetachAndDropExpired(invalid) = %v, want ErrInvalidInput", err)
	}
	if dropped, err := st.Partitions().DetachAndDropExpired(ctx, "audit_events", 0); err != nil || len(dropped) != 0 {
		t.Fatalf("DetachAndDropExpired(zero) = %v err=%v", dropped, err)
	}

	// Verify BeginTx wrappers & explicit Rollback.
	tx, err := st.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := tx.Audit().Append(ctx, port.AuditEvent{ID: "aud_tx_1"}); err != nil {
		t.Fatalf("tx.Audit().Append: %v", err)
	}
	if err := st.Audit().Append(ctx, port.AuditEvent{ID: "aud_st_1"}); err != nil {
		t.Fatalf("st.Audit().Append: %v", err)
	}
	if len(sink.events) != 2 || sink.txCnt != 1 {
		t.Fatalf("sink state mismatch: events=%v txCnt=%d", sink.events, sink.txCnt)
	}
	if _, err := tx.Changelog().LatestEpoch(ctx); err != nil {
		t.Fatalf("tx.Changelog.LatestEpoch: %v", err)
	}
	if _, err := tx.Leases().Get(ctx, "missing"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("tx.Leases.Get = %v, want ErrNotFound", err)
	}
	_ = tx.Rollback()

	if err := st.InTx(ctx, func(port.Tx) error { return errors.New("boom") }); err == nil {
		t.Fatalf("expected InTx callback error")
	}

	// Tenancy validation & optional field update parity.
	if _, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("CreateOrg empty = %v", err)
	}
	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "cov-org", DisplayName: "Cov Org",
		DefaultDataRegion: strPtr("us-east-1"), MinPluginTrust: port.MinTrustVerifiedOnly,
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if _, err := st.Tenancy().UpdateOrganization(ctx, org.ID, org.Version, port.UpdateOrgParams{DisplayName: strPtr(" ")}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("UpdateOrganization blank display = %v", err)
	}
	updOrg, err := st.Tenancy().UpdateOrganization(ctx, org.ID, org.Version, port.UpdateOrgParams{
		Status: strPtr(port.OrgStatusSuspended), ClearDefaultDataRegion: true,
		MinPluginTrust: strPtr(port.MinTrustAnyApproved), RequireFourEyesApproval: boolPtr(true),
	})
	if err != nil || updOrg.DefaultDataRegion != nil || !updOrg.RequireFourEyesApproval {
		t.Fatalf("UpdateOrganization flags: %+v err=%v", updOrg, err)
	}

	prj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{
		ID: idgen.MustNew(idgen.PrefixProject), Slug: "cov-prj", DisplayName: "Cov Prj",
		EnforcedRegion: strPtr("eu-west-1"),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if got, err := st.Tenancy().GetProjectBySlug(ctx, org.ID, "cov-prj"); err != nil || got.ID != prj.ID {
		t.Fatalf("GetProjectBySlug: %+v err=%v", got, err)
	}
	if _, err := st.Tenancy().GetProjectBySlug(ctx, org.ID, ""); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("GetProjectBySlug empty = %v", err)
	}
	if _, err := st.Tenancy().UpdateProject(ctx, org.ID, prj.ID, prj.Version, port.UpdateProjectParams{DisplayName: strPtr(" ")}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("UpdateProject blank display = %v", err)
	}
	updPrj, err := st.Tenancy().UpdateProject(ctx, org.ID, prj.ID, prj.Version, port.UpdateProjectParams{
		Status: strPtr(port.ProjectStatusEmergencyHalted), ClearEnforcedRegion: true,
	})
	if err != nil || updPrj.EnforcedRegion != nil || updPrj.Status != port.ProjectStatusEmergencyHalted {
		t.Fatalf("UpdateProject flags: %+v err=%v", updPrj, err)
	}
	if _, err := st.Tenancy().UpdateProject(ctx, org.ID, prj.ID, 1, port.UpdateProjectParams{DisplayName: strPtr("Stale")}); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("UpdateProject stale = %v, want ErrVersionConflict", err)
	}

	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, prj.ID, port.CreateEnvironmentParams{
		ID: idgen.MustNew(idgen.PrefixEnv), Slug: "cov-env",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if _, err := st.Tenancy().UpdateEnvironment(ctx, org.ID, env.ID, env.Version, port.UpdateEnvironmentParams{Slug: strPtr(" ")}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("UpdateEnvironment blank slug = %v", err)
	}
	updEnv, err := st.Tenancy().UpdateEnvironment(ctx, org.ID, env.ID, env.Version, port.UpdateEnvironmentParams{
		RequireTestedCaps: boolPtr(true),
	})
	if err != nil || !updEnv.RequireTestedCaps {
		t.Fatalf("UpdateEnvironment flags: %+v err=%v", updEnv, err)
	}
	if _, err := st.Tenancy().UpdateEnvironment(ctx, org.ID, env.ID, 1, port.UpdateEnvironmentParams{Slug: strPtr("stale")}); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("UpdateEnvironment stale = %v, want ErrVersionConflict", err)
	}
	if err := st.Tenancy().DeleteEnvironment(ctx, org.ID, env.ID, 1); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("DeleteEnvironment stale = %v, want ErrVersionConflict", err)
	}
	if err := st.Tenancy().DeleteEnvironment(ctx, org.ID, "env_missing", 1); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("DeleteEnvironment missing = %v, want ErrNotFound", err)
	}

	// Lease same-holder renew + active Get + validation.
	l1, ok, err := st.Leases().AcquireOrRenew(ctx, "l-cov", "node-1", 100, 1000)
	if err != nil || !ok {
		t.Fatalf("AcquireOrRenew 1: %v ok=%v", err, ok)
	}
	l2, ok2, err := st.Leases().AcquireOrRenew(ctx, "l-cov", "node-1", 200, 1000)
	if err != nil || !ok2 || l2.FencingToken < l1.FencingToken {
		t.Fatalf("AcquireOrRenew renew same holder: l2=%+v ok=%v err=%v", l2, ok2, err)
	}
	gotLease, err := st.Leases().Get(ctx, "l-cov")
	if err != nil || gotLease.HolderNodeID != "node-1" {
		t.Fatalf("Leases().Get: %+v err=%v", gotLease, err)
	}
	if err := st.Leases().Release(ctx, "l-cov", "node-1", l2.FencingToken); err != nil {
		t.Fatalf("Leases().Release: %v", err)
	}
	if err := st.Leases().Release(ctx, "l-cov", "node-1", l2.FencingToken); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("Leases().Release twice = %v, want ErrNotFound", err)
	}
	if _, _, err := st.Leases().AcquireOrRenew(ctx, "", "node-1", 100, 1000); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("AcquireOrRenew invalid = %v", err)
	}
	if err := st.Leases().Release(ctx, "", "node-1", 1); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("Leases().Release invalid = %v", err)
	}
	if _, err := st.Leases().Get(ctx, ""); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("Leases().Get empty = %v", err)
	}
	if _, err := st.Changelog().Append(ctx, port.ChangelogEntry{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("Changelog.Append invalid = %v", err)
	}
	if _, err := st.Tenancy().GetOrganization(ctx, ""); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("GetOrganization empty = %v", err)
	}
	if _, err := st.Tenancy().GetOrganizationBySlug(ctx, ""); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("GetOrganizationBySlug empty = %v", err)
	}
	if _, err := st.Tenancy().CreateProject(ctx, "", port.CreateProjectParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("CreateProject empty = %v", err)
	}
	if _, err := st.Tenancy().GetProject(ctx, "", ""); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("GetProject empty = %v", err)
	}
	if _, err := st.Tenancy().ListProjects(ctx, "", 0); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("ListProjects empty = %v", err)
	}
	if err := st.Tenancy().DeleteProject(ctx, "", "", 0); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("DeleteProject invalid = %v", err)
	}
	if _, err := st.Tenancy().CreateEnvironment(ctx, "", "", port.CreateEnvironmentParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("CreateEnvironment invalid = %v", err)
	}
	if _, err := st.Tenancy().GetEnvironment(ctx, "", ""); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("GetEnvironment invalid = %v", err)
	}
	if _, err := st.Tenancy().ListEnvironments(ctx, "", ""); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("ListEnvironments invalid = %v", err)
	}
	if _, err := st.Tenancy().UpdateEnvironment(ctx, "", "", 0, port.UpdateEnvironmentParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("UpdateEnvironment invalid = %v", err)
	}
	if err := st.Tenancy().DeleteEnvironment(ctx, "", "", 0); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("DeleteEnvironment invalid = %v", err)
	}
	if _, err := st.Tenancy().UpdateOrganization(ctx, "", 0, port.UpdateOrgParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("UpdateOrganization invalid = %v", err)
	}
	if _, err := st.Tenancy().UpdateProject(ctx, "", "", 0, port.UpdateProjectParams{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("UpdateProject invalid = %v", err)
	}
	if err := st.WithOperatorTx(ctx, func(*sql.Tx) error { return errors.New("op-fail") }); err == nil {
		t.Fatalf("expected WithOperatorTx error")
	}
	if orgsAll, err := st.Tenancy().ListOrganizations(ctx, 0); err != nil || len(orgsAll) == 0 {
		t.Fatalf("ListOrganizations(0): %v len=%d", err, len(orgsAll))
	}
	if prjsAll, err := st.Tenancy().ListProjects(ctx, org.ID, 0); err != nil || len(prjsAll) == 0 {
		t.Fatalf("ListProjects(0): %v len=%d", err, len(prjsAll))
	}
	if chgAll, err := st.Changelog().ListSince(ctx, 0, 0); err != nil || len(chgAll) == 0 {
		t.Fatalf("ListSince(0): %v len=%d", err, len(chgAll))
	}

	// Cover in-transaction Tenancy/Leases/Changelog + Commit paths (r.tx != nil branches).
	tx2, err := st.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 2: %v", err)
	}
	txOrg, err := tx2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "tx-org", DisplayName: "Tx Org",
	})
	if err != nil {
		t.Fatalf("tx CreateOrganization: %v", err)
	}
	if _, err := tx2.Tenancy().CreateProject(ctx, txOrg.ID, port.CreateProjectParams{
		ID: idgen.MustNew(idgen.PrefixProject), Slug: "tx-prj", DisplayName: "Tx Prj",
	}); err != nil {
		t.Fatalf("tx CreateProject: %v", err)
	}
	if _, err := tx2.Changelog().Append(ctx, port.ChangelogEntry{
		OrgID: &txOrg.ID, EntityKind: "organization", EntityID: txOrg.ID, MutationType: port.MutationUpsert,
	}); err != nil {
		t.Fatalf("tx Changelog.Append: %v", err)
	}
	txLease, ok, err := tx2.Leases().AcquireOrRenew(ctx, "l-tx", "node-tx", 10, 1000)
	if err != nil || !ok {
		t.Fatalf("tx AcquireOrRenew: %v ok=%v", err, ok)
	}
	if err := tx2.Leases().Release(ctx, "l-tx", "node-tx", txLease.FencingToken); err != nil {
		t.Fatalf("tx Release: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("tx2 Commit: %v", err)
	}

	// Verify non-TxAuditSink is rejected fail-closed by Open (matching SQLite parity).
	if _, err := postgres.Open(ctx, postgres.Options{
		DSN:       baseDSN,
		AuditSink: &plainAuditSink{},
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("Open(non-TxAuditSink) = %v, want ErrInvalidInput", err)
	}

	if err := st.SetAuditSink(nil); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("SetAuditSink(nil) = %v, want ErrInvalidInput", err)
	}
	if err := st.SetAuditSink(&plainAuditSink{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("SetAuditSink(non-TxAuditSink) = %v, want ErrInvalidInput", err)
	}
	if err := st.SetAuditSink(sink); err != nil {
		t.Fatalf("SetAuditSink(sink): %v", err)
	}

	// Verify auto-generated IDs + invalid caller ID rejection + tenancy audit emission.
	prevEvents := len(sink.events)
	actorCtx := port.WithAuditActor(ctx, port.AuditActor{
		ActorKind:  "PRINCIPAL",
		ActorID:    "usr_pg_cov",
		EntryPoint: port.AuditEntryPointAdminAPI,
		RequestID:  "req_pg_cov",
	})
	autoOrg, err := st.Tenancy().CreateOrganization(actorCtx, port.CreateOrgParams{Slug: "auto-id-org", DisplayName: "Auto Org"})
	if err != nil || !strings.HasPrefix(autoOrg.ID, "org_") {
		t.Fatalf("CreateOrganization auto-ID: %+v err=%v", autoOrg, err)
	}
	autoPrj, err := st.Tenancy().CreateProject(ctx, autoOrg.ID, port.CreateProjectParams{Slug: "auto-id-prj", DisplayName: "Auto Prj"})
	if err != nil || !strings.HasPrefix(autoPrj.ID, "prj_") {
		t.Fatalf("CreateProject auto-ID: %+v err=%v", autoPrj, err)
	}
	autoEnv, err := st.Tenancy().CreateEnvironment(ctx, autoOrg.ID, autoPrj.ID, port.CreateEnvironmentParams{Slug: "auto-id-env"})
	if err != nil || !strings.HasPrefix(autoEnv.ID, "env_") {
		t.Fatalf("CreateEnvironment auto-ID: %+v err=%v", autoEnv, err)
	}
	if sink.badEp != "" || len(sink.events) < prevEvents+3 {
		t.Fatalf("expected canonical tenancy audit events, before=%d after=%d badEp=%q", prevEvents, len(sink.events), sink.badEp)
	}
	if _, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{ID: "org_bad:colon", Slug: "bad-id", DisplayName: "Bad"}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("CreateOrganization colon ID = %v, want ErrInvalidInput", err)
	}
	if _, err := st.Tenancy().CreateProject(ctx, autoOrg.ID, port.CreateProjectParams{ID: "prj_bad:colon", Slug: "bad-id", DisplayName: "Bad"}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("CreateProject colon ID = %v, want ErrInvalidInput", err)
	}
	if _, err := st.Tenancy().CreateEnvironment(ctx, autoOrg.ID, autoPrj.ID, port.CreateEnvironmentParams{ID: "env_bad:colon", Slug: "bad-id"}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("CreateEnvironment colon ID = %v, want ErrInvalidInput", err)
	}

	// Verify RedactDSN strips malformed URL passwords (even containing @), query secrets, and sslpassword.
	for _, tc := range []struct {
		dsn    string
		secret string
	}{
		{"postgres://u:p@ss%bad@127.0.0.1:5432/db", "ss%bad"},
		{"postgres://u:pw1@127.0.0.1:5432/db?sslmode=disable&password=qp1&sslpassword=qp2", "qp1"},
		{"postgres://u:pw1@127.0.0.1:5432/db?sslpassword=qp2", "qp2"},
		{`host=127.0.0.1 sslpassword='a\'sec' user=u`, "sec"},
	} {
		red := postgres.RedactDSN(tc.dsn)
		if strings.Contains(red, tc.secret) {
			t.Fatalf("RedactDSN leaked secret %q in %q", tc.secret, red)
		}
	}

	// Verify background PartitionRefreshInterval loop ticks cleanly and shuts down on Close().
	stMaint, err := postgres.Open(ctx, postgres.Options{
		DSN:                      isolatedPGSchemaDSN(t, baseDSN),
		PartitionRefreshInterval: 15 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open with PartitionRefreshInterval: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := stMaint.Close(); err != nil {
		t.Fatalf("Close stMaint: %v", err)
	}
	if _, err := st.Tenancy().GetOrganizationBySlug(ctx, "missing-slug"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("GetOrganizationBySlug missing = %v", err)
	}
	if _, err := st.Tenancy().GetProject(ctx, org.ID, "prj_missing"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("GetProject missing = %v", err)
	}
	if _, err := st.Tenancy().GetProjectBySlug(ctx, org.ID, "missing-slug"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("GetProjectBySlug missing = %v", err)
	}
	if _, err := st.Tenancy().GetEnvironment(ctx, org.ID, "env_missing"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("GetEnvironment missing = %v", err)
	}
	if err := st.Tenancy().DeleteProject(ctx, org.ID, "prj_missing", 1); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("DeleteProject missing = %v", err)
	}
	if err := st.Partitions().EnsureMonth(ctx, time.Time{}); err != nil {
		t.Fatalf("EnsureMonth(zero time): %v", err)
	}

	// Verify RunMigrations refuses checksum mismatch and dirty flag with ErrDirtyMigration.
	chkDSN := isolatedPGSchemaDSN(t, baseDSN)
	stChk, err := postgres.Open(ctx, postgres.Options{DSN: chkDSN})
	if err != nil {
		t.Fatalf("Open chkDSN: %v", err)
	}
	if _, err := stChk.DB().ExecContext(ctx, `UPDATE schema_migrations SET checksum_sha256 = 'deadbeef' WHERE version = 1`); err != nil {
		t.Fatalf("corrupt checksum: %v", err)
	}
	_ = stChk.Close()
	if _, err := postgres.Open(ctx, postgres.Options{DSN: chkDSN}); !errors.Is(err, port.ErrDirtyMigration) {
		t.Fatalf("Open checksum mismatch = %v, want ErrDirtyMigration", err)
	}
	stDirty, err := postgres.Open(ctx, postgres.Options{DSN: chkDSN, SkipMigrate: true, PartitionLookaheadMonths: -1})
	if err != nil {
		t.Fatalf("Open SkipMigrate: %v", err)
	}
	if _, err := stDirty.DB().ExecContext(ctx, `UPDATE schema_migrations SET dirty = TRUE WHERE version = 1`); err != nil {
		t.Fatalf("set dirty: %v", err)
	}
	canceledCtx, cancelPing := context.WithCancel(ctx)
	cancelPing()
	if err := stDirty.Ping(canceledCtx); err == nil {
		t.Fatalf("expected Ping error with canceled context")
	}
	_ = stDirty.Close()
	if _, err := postgres.Open(ctx, postgres.Options{DSN: chkDSN}); !errors.Is(err, port.ErrDirtyMigration) {
		t.Fatalf("Open dirty migration = %v, want ErrDirtyMigration", err)
	}

	if _, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{
		ID: idgen.MustNew(idgen.PrefixProject), Slug: "cov-prj", DisplayName: "Dup Prj",
	}); !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("duplicate CreateProject slug = %v, want ErrAlreadyExists", err)
	}
	if _, err := st.Tenancy().CreateEnvironment(ctx, org.ID, prj.ID, port.CreateEnvironmentParams{
		ID: idgen.MustNew(idgen.PrefixEnv), Slug: "cov-env",
	}); !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("duplicate CreateEnvironment slug = %v, want ErrAlreadyExists", err)
	}
	if _, _, err := st.Leases().AcquireOrRenew(ctx, "l-ttl", "n", 100, 0); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("AcquireOrRenew zero ttl = %v, want ErrInvalidInput", err)
	}
	if err := st.Partitions().EnsureLookahead(ctx, time.Now().UTC(), 0); err != nil {
		t.Fatalf("EnsureLookahead(0): %v", err)
	}
	if err := postgres.CheckNonPrivilegedRole(ctx, nil); err == nil {
		t.Fatalf("expected CheckNonPrivilegedRole(nil) error")
	}

	// Verify closed-store paths fail cleanly without panic.
	_ = st.Close()
	if err := st.Ping(ctx); err == nil {
		t.Fatalf("expected Ping error on closed store")
	}
	if _, err := st.BeginTx(ctx); err == nil {
		t.Fatalf("expected BeginTx error on closed store")
	}
	if err := st.WithOrgTx(ctx, org.ID, func(*sql.Tx) error { return nil }); err == nil {
		t.Fatalf("expected WithOrgTx error on closed store")
	}
	if err := st.WithOperatorTx(ctx, func(*sql.Tx) error { return nil }); err == nil {
		t.Fatalf("expected WithOperatorTx error on closed store")
	}
	if _, err := st.Changelog().LatestEpoch(ctx); err == nil {
		t.Fatalf("expected LatestEpoch error on closed store")
	}
	if _, _, err := st.Leases().AcquireOrRenew(ctx, "l-closed", "n", 100, 1000); err == nil {
		t.Fatalf("expected AcquireOrRenew error on closed store")
	}
	if _, err := st.Partitions().ListPartitions(ctx, "audit_events"); err == nil {
		t.Fatalf("expected ListPartitions error on closed store")
	}
	if _, err := st.Partitions().DetachAndDropExpired(ctx, "audit_events", 1000); err == nil {
		t.Fatalf("expected DetachAndDropExpired error on closed store")
	}

	badCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err := postgres.Open(badCtx, postgres.Options{DSN: "postgres://u:p@127.0.0.1:1/db?sslmode=disable"}); err == nil {
		t.Fatalf("expected unreachable DSN Open error")
	}
}

type plainAuditSink struct {
	events []string
}

func (p *plainAuditSink) Append(_ context.Context, ev port.AuditEvent) error {
	p.events = append(p.events, ev.ID)
	return nil
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

