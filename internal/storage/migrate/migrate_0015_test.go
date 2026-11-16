// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package migrate

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	sqlitemigrations "github.com/nguywnben/tanpopo/db/migrations/sqlite"
)

func TestSQLiteMigration0015RouteRequestTimeoutFreshAndUpgrade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Fresh DB applying all embedded migrations 0001..0015.
	dbFresh := openTestSQLite(t)
	sumFresh, err := Apply(ctx, dbFresh, sqlitemigrations.FS, func() int64 { return 1760000000000 })
	if err != nil {
		t.Fatalf("fresh Apply(0001..0015): %v", err)
	}
	if sumFresh.LatestVersion < 15 || sumFresh.NewlyApplied < 15 {
		t.Fatalf("expected >=15 migrations applied fresh, got %+v", sumFresh)
	}

	// 2. Existing DB upgrade path: apply 0001..0014 first, insert a route_policies row, then apply 0015.
	preFS := fstest.MapFS{}
	entries, err := fs.ReadDir(sqlitemigrations.FS, ".")
	if err != nil {
		t.Fatalf("ReadDir sqlitemigrations.FS: %v", err)
	}
	for _, ent := range entries {
		if !strings.HasSuffix(ent.Name(), ".sql") || ent.Name() >= "0015_" {
			continue
		}
		data, rerr := fs.ReadFile(sqlitemigrations.FS, ent.Name())
		if rerr != nil {
			t.Fatalf("ReadFile(%s): %v", ent.Name(), rerr)
		}
		preFS[ent.Name()] = &fstest.MapFile{Data: data}
	}

	dbUpgrade := openTestSQLite(t)
	sumPre, err := Apply(ctx, dbUpgrade, preFS, func() int64 { return 1760000000000 })
	if err != nil {
		t.Fatalf("Apply(0001..0014): %v", err)
	}
	if sumPre.LatestVersion != 14 {
		t.Fatalf("expected pre-upgrade LatestVersion=14, got %+v", sumPre)
	}

	nowMs := int64(1760000000000)
	for _, stmt := range []string{
		`INSERT INTO organizations (id, slug, display_name, created_at_ms, updated_at_ms) VALUES ('org_m15', 'org-m15', 'Org M15', ?, ?)`,
		`INSERT INTO projects (id, org_id, slug, display_name, created_at_ms, updated_at_ms) VALUES ('prj_m15', 'org_m15', 'prj-m15', 'Prj M15', ?, ?)`,
		`INSERT INTO environments (id, org_id, project_id, slug, created_at_ms, updated_at_ms) VALUES ('env_m15', 'org_m15', 'prj_m15', 'prod', ?, ?)`,
		`INSERT INTO route_policies (id, org_id, project_id, env_id, route_slug, strategy, max_failover_hops, version, created_at_ms, updated_at_ms) VALUES ('rte_m15_pre', 'org_m15', 'prj_m15', 'env_m15', 'default-route', 'PRIORITY_FAILOVER', 3, 1, ?, ?)`,
	} {
		if _, err := dbUpgrade.ExecContext(ctx, stmt, nowMs, nowMs); err != nil {
			t.Fatalf("seed pre-0015 row (%s): %v", stmt, err)
		}
	}

	sumUp, err := Apply(ctx, dbUpgrade, sqlitemigrations.FS, func() int64 { return 1760000001000 })
	if err != nil {
		t.Fatalf("upgrade Apply(->0015): %v", err)
	}
	if sumUp.NewlyApplied < 1 || sumUp.AlreadyApplied != 14 || sumUp.LatestVersion < 15 {
		t.Fatalf("unexpected upgrade summary: %+v", sumUp)
	}

	var preDefaultTimeout int
	if err := dbUpgrade.QueryRowContext(ctx, `SELECT request_timeout_ms FROM route_policies WHERE id = 'rte_m15_pre'`).Scan(&preDefaultTimeout); err != nil {
		t.Fatalf("query upgraded route_policies.request_timeout_ms: %v", err)
	}
	if preDefaultTimeout != 60000 {
		t.Fatalf("expected upgraded default request_timeout_ms=60000, got %d", preDefaultTimeout)
	}

	// Verify CHECK constraint rejects out-of-bounds values (<500 and >300000) and accepts boundary values.
	for _, bad := range []int{0, 499, 300001} {
		if _, err := dbUpgrade.ExecContext(ctx, `
			INSERT INTO route_policies (id, org_id, project_id, env_id, route_slug, strategy, max_failover_hops, request_timeout_ms, version, created_at_ms, updated_at_ms)
			VALUES ('rte_bad', 'org_m15', 'prj_m15', 'env_m15', 'bad-timeout', 'PRIORITY_FAILOVER', 3, ?, 1, ?, ?)`,
			bad, nowMs, nowMs,
		); err == nil {
			t.Fatalf("expected CHECK failure for request_timeout_ms=%d", bad)
		}
	}
	for idx, okVal := range []int{500, 300000} {
		slug := "ok-min"
		id := "rte_m15_min"
		if idx == 1 {
			slug = "ok-max"
			id = "rte_m15_max"
		}
		if _, err := dbUpgrade.ExecContext(ctx, `
			INSERT INTO route_policies (id, org_id, project_id, env_id, route_slug, strategy, max_failover_hops, request_timeout_ms, version, created_at_ms, updated_at_ms)
			VALUES (?, 'org_m15', 'prj_m15', 'env_m15', ?, 'PRIORITY_FAILOVER', 3, ?, 1, ?, ?)`,
			id, slug, okVal, nowMs, nowMs,
		); err != nil {
			t.Fatalf("expected boundary request_timeout_ms=%d to succeed: %v", okVal, err)
		}
	}
}
