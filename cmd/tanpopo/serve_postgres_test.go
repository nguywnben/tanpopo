// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/webhook"
)

var servePGSeq atomic.Int64

func TestDEF081_PostgresForcedRLSExtensionWebhookOutbox(t *testing.T) {
	baseDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if baseDSN == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_DSN required when TANPOPO_REQUIRE_POSTGRES_TESTS=1")
		}
		t.Log("TANPOPO_TEST_PG_DSN not set; skipping live Postgres webhook outbox RLS test")
		return
	}

	ctx := context.Background()
	schemaName := fmt.Sprintf("tp_serve_pg_%d_%d", time.Now().UnixNano(), servePGSeq.Add(1))
	baseDB, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open base pg: %v", err)
	}
	if _, err := baseDB.ExecContext(ctx, `CREATE SCHEMA `+schemaName); err != nil {
		_ = baseDB.Close()
		t.Fatalf("create schema %s: %v", schemaName, err)
	}
	_ = baseDB.Close()
	t.Cleanup(func() {
		cleanDB, cerr := sql.Open("pgx", baseDSN)
		if cerr == nil {
			_, _ = cleanDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`)
			_ = cleanDB.Close()
		}
	})

	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()

	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	nowMs := func() int64 { return fk.Now().UTC().UnixMilli() }
	st, err := postgres.Open(ctx, postgres.Options{DSN: u.String(), NowMs: nowMs})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "srv-pg-a", DisplayName: "Srv PG A"})
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "srv-pg-b", DisplayName: "Srv PG B"})
	if err != nil {
		t.Fatal(err)
	}
	orgC, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "srv-pg-c", DisplayName: "Srv PG C"})
	if err != nil {
		t.Fatal(err)
	}
	now := nowMs()
	pkgID, insID, adpID, connID := "pkg_srv_pg_1", "ins_srv_pg_1", "adp_srv_pg_1", "conn_srv_pg_1"

	seedTx, err := st.RawDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = seedTx.Rollback() }()
	if err := port.SetOperatorRLSTx(ctx, seedTx, port.DialectPostgres); err != nil {
		t.Fatal(err)
	}
	if _, err := seedTx.ExecContext(ctx, `
		INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms)
		VALUES ($1, '@tanpopo/ollama', 'PROVIDER_ADAPTER', 'pub_tanpopo', 'official', $2)`, pkgID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := seedTx.ExecContext(ctx, `
		INSERT INTO extension_installations (id, package_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms)
		VALUES ($1, $2, 'ENABLED_ACTIVE', '{}', 1, $3, $3)`, insID, pkgID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := seedTx.ExecContext(ctx, `
		INSERT INTO provider_adapters (id, installation_id, adapter_slug, supported_dialects_json, allowed_hosts_json, version, created_at_ms)
		VALUES ($1, $2, 'ollama', '["ollama"]', '["127.0.0.1"]', 1, $3)`, adpID, insID, now); err != nil {
		t.Fatal(err)
	}
	if err := port.SetOrgRLSTx(ctx, seedTx, port.DialectPostgres, orgA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := seedTx.ExecContext(ctx, `
		INSERT INTO provider_connections (id, org_id, adapter_id, name, endpoint_base_url, data_region, billing_mode, auth_mode, state, version, created_at_ms, updated_at_ms)
		VALUES ($1, $2, $3, 'Ollama Conn', 'http://127.0.0.1:11434', 'ON_PREM', 'SELF_HOSTED_INFRA', 'NONE', 'ACTIVE', 1, $4, $4)`,
		connID, orgA.ID, adpID, now); err != nil {
		t.Fatal(err)
	}
	// Saturate orgA's webhook_event_outbox backlog (5000 pending rows) so normal quota would reject;
	// global security containment fanout MUST still succeed via RecordPriorityEventTx.
	if _, err := seedTx.ExecContext(ctx, `
		INSERT INTO webhook_event_outbox (id, org_id, event_type, payload_json, created_at_ms)
		SELECT 'wbk_spam_' || g::text, $1, 'trace.completed', '{}', $2
		FROM generate_series(1, 5000) AS g`, orgA.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := port.SetOrgRLSTx(ctx, seedTx, port.DialectPostgres, orgB.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := seedTx.ExecContext(ctx, `
		INSERT INTO org_extension_policies (org_id, package_id, enabled, org_owner_opt_in, version, created_at_ms, updated_at_ms)
		VALUES ($1, $2, TRUE, FALSE, 1, $3, $3)`, orgB.ID, pkgID, now); err != nil {
		t.Fatal(err)
	}
	if err := port.SetOrgRLSTx(ctx, seedTx, port.DialectPostgres, orgC.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := seedTx.ExecContext(ctx, `
		INSERT INTO org_extension_policies (org_id, package_id, enabled, org_owner_opt_in, version, created_at_ms, updated_at_ms)
		VALUES ($1, $2, FALSE, FALSE, 1, $3, $3)`, orgC.ID, pkgID, now); err != nil {
		t.Fatal(err)
	}
	if err := seedTx.Commit(); err != nil {
		t.Fatal(err)
	}

	disp, err := webhook.New(webhook.Config{
		DB:       st.RawDB(),
		Postgres: true,
		Now:      fk.Now,
		Resolver: egress.NewStdHostResolver(),
		Unseal: func(context.Context, string, string) ([]byte, func(), error) {
			return []byte("test-hmac-secret"), func() {}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = disp.Close() })

	tx, err := st.RawDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOperatorRLSTx(ctx, tx, port.DialectPostgres); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"target_id":"ins_srv_pg_1"}`)
	if err := recordExtensionWebhookOutbox(ctx, tx, port.DialectPostgres, disp, insID, "kill_switch:ksw_pg_1", "kill_switch.triggered", payload); err != nil {
		t.Fatalf("recordExtensionWebhookOutbox under PG forced RLS + saturated orgA outbox: %v", err)
	}
	// Verify operator RLS is restored inside tx so global emergency_kill_switches writes would still succeed.
	var opSetting string
	if err := tx.QueryRowContext(ctx, `SELECT current_setting('app.is_instance_operator', true)`).Scan(&opSetting); err != nil || opSetting != "true" {
		t.Fatalf("expected operator RLS restored after outbox write, got %q (err=%v)", opSetting, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertOutboxCount := func(orgID string, want int) {
		t.Helper()
		vTx, err := st.RawDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = vTx.Rollback() }()
		if err := port.SetOrgRLSTx(ctx, vTx, port.DialectPostgres, orgID); err != nil {
			t.Fatal(err)
		}
		var outboxCount int
		if err := vTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_event_outbox WHERE org_id = $1 AND event_type = 'kill_switch.triggered'`, orgID).Scan(&outboxCount); err != nil {
			t.Fatal(err)
		}
		if outboxCount != want {
			t.Fatalf("org %s webhook_event_outbox count = %d, want %d", orgID, outboxCount, want)
		}
	}
	assertOutboxCount(orgA.ID, 1)
	assertOutboxCount(orgB.ID, 1)
	assertOutboxCount(orgC.ID, 0)
}
