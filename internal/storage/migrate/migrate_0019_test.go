// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"

	sqlitemigrations "github.com/nguywnben/tanpopo/db/migrations/sqlite"
)

func TestSQLiteMigration0019ApprovalsAndWebhooksFreshAndUpgrade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	all, err := LoadMigrations(sqlitemigrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	preFS := fstest.MapFS{}
	for _, migration := range all {
		if migration.Version > 18 {
			continue
		}
		name := fmt.Sprintf("%04d_%s.sql", migration.Version, migration.Name)
		data, err := fs.ReadFile(sqlitemigrations.FS, name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		preFS[name] = &fstest.MapFile{Data: data}
	}
	if len(preFS) != 18 {
		t.Fatalf("expected migrations 0001..0018, got %d", len(preFS))
	}

	for _, tc := range []struct {
		name string
		fs   fs.FS
	}{
		{name: "fresh", fs: sqlitemigrations.FS},
		{name: "upgrade", fs: preFS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestSQLite(t)
			if tc.name == "upgrade" {
				before, err := Apply(ctx, db, tc.fs, func() int64 { return 1795083504000 })
				if err != nil || before.LatestVersion != 18 {
					t.Fatalf("apply through migration 0018: summary=%+v err=%v", before, err)
				}
			}
			after, err := Apply(ctx, db, sqlitemigrations.FS, func() int64 { return 1795083505000 })
			if err != nil || after.LatestVersion < 19 {
				t.Fatalf("apply migration 0019: summary=%+v err=%v", after, err)
			}
			for _, object := range []string{
				"approval_requests", "webhook_subscriptions", "webhook_deliveries", "idx_webhook_deliveries_queue",
			} {
				var count int
				if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, object).Scan(&count); err != nil || count != 1 {
					t.Errorf("SQLite object %q count=%d err=%v", object, count, err)
				}
			}
		})
	}
}
