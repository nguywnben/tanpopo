// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package audit_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
)

var auditSchemaSeq atomic.Int64

func livePGForAuditDSN(t *testing.T) (*postgres.Store, string) {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if baseDSN == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_DSN is required when TANPOPO_REQUIRE_POSTGRES_TESTS=1")
		}
		return nil, ""
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("tp_audit_pg_%d_%d", time.Now().UnixNano(), auditSchemaSeq.Add(1))
	baseDB, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open base pg dsn: %v", err)
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
		t.Fatalf("parse pg url: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()

	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC).UnixMilli()
	pgStore, err := postgres.Open(ctx, postgres.Options{
		DSN:   u.String(),
		NowMs: func() int64 { return now },
	})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = pgStore.Close() })
	return pgStore, u.String()
}

func TestPostgresAuditConcurrentEightWritersCT30(t *testing.T) {
	pgStore, isoDSN := livePGForAuditDSN(t)
	if pgStore == nil {
		return
	}
	ctx := context.Background()
	nowMs := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC).UnixMilli()

	orgA, err := pgStore.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "aud-pg-a", DisplayName: "Aud A",
	})
	if err != nil {
		t.Fatalf("CreateOrganization A: %v", err)
	}
	orgB, err := pgStore.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "aud-pg-b", DisplayName: "Aud B",
	})
	if err != nil {
		t.Fatalf("CreateOrganization B: %v", err)
	}

	logger, err := audit.NewLogger(audit.LoggerConfig{
		DB:    pgStore.DB(),
		NowMs: func() int64 { return nowMs },
	})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}

	// Simulate 8 concurrent HA writer nodes writing simultaneously to orgA, orgB, and SYSTEM chains.
	const writerNodes = 8
	const eventsPerNode = 15
	var wg sync.WaitGroup
	errs := make(chan error, writerNodes*eventsPerNode)
	for node := 0; node < writerNodes; node++ {
		wg.Add(1)
		go func(nodeID int) {
			defer wg.Done()
			for i := 0; i < eventsPerNode; i++ {
				var orgPtr *string
				if i%3 == 0 {
					v := orgA.ID
					orgPtr = &v
				} else if i%3 == 1 {
					v := orgB.ID
					orgPtr = &v
				}
				err := logger.Append(ctx, port.AuditEvent{
					OrgID:        orgPtr,
					ActorKind:    "admin",
					ActorID:      fmt.Sprintf("node-%d", nodeID),
					EntryPoint:   port.AuditEntryPointAdminAPI,
					EventType:    "kill_switch.activate",
					TargetType:   "emergency_kill_switch",
					TargetID:     "ksw_01",
					MetadataJSON: fmt.Sprintf(`{"target_id":"ext_shared","node":%d,"seq":%d}`, nodeID, i),
				})
				if err != nil {
					errs <- err
				}
			}
		}(node)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Append error (CT-30): %v", err)
	}

	report, err := audit.VerifyAuditChain(ctx, pgStore.DB(), audit.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyAuditChain failed on PostgreSQL: %v (report=%+v)", err, report)
	}
	wantTotal := writerNodes * eventsPerNode
	if !report.Valid || report.TotalVerified != wantTotal {
		t.Fatalf("VerifyAuditChain total=%d valid=%v, want total=%d valid=true", report.TotalVerified, report.Valid, wantTotal)
	}

	// Verify ListEvents & QueryBlastRadius with PG JSONB metadata_json->>'target_id' filter.
	listed, err := audit.ListEvents(ctx, pgStore.DB(), audit.ListFilter{ChainKey: orgA.ID, Limit: 200})
	if err != nil || len(listed) != writerNodes*(eventsPerNode/3) {
		t.Fatalf("ListEvents(%s) count=%d err=%v", orgA.ID, len(listed), err)
	}
	br, err := audit.QueryBlastRadius(ctx, pgStore.DB(), audit.BlastRadiusQuery{
		OrgID:                   orgA.ID,
		ExtensionInstallationID: "ext_shared",
		AllowInstanceScope:      true,
	})
	if err != nil {
		t.Fatalf("QueryBlastRadius: %v", err)
	}
	if len(br.AuditEvents) == 0 {
		t.Fatalf("expected BlastRadius AuditEvents matching JSONB target_id='ext_shared'")
	}

	// Seed request_traces_meta rows under orgA and orgB and verify AllowInstanceScope=true, OrgID="".
	for i, org := range []string{orgA.ID, orgB.ID} {
		err := pgStore.WithOrgTx(ctx, org, func(tx *sql.Tx) error {
			_, exErr := tx.ExecContext(ctx, `
				INSERT INTO projects (id, org_id, slug, display_name, status, version, created_at_ms, updated_at_ms)
				VALUES ($1, $2, 'brp', 'BR Prj', 'ACTIVE', 1, $3, $3)`,
				fmt.Sprintf("prj_br_%d", i), org, nowMs,
			)
			if exErr != nil {
				return exErr
			}
			_, exErr = tx.ExecContext(ctx, `
				INSERT INTO environments (id, org_id, project_id, slug, require_tested_caps, version, created_at_ms, updated_at_ms)
				VALUES ($1, $2, $3, 'prod', FALSE, 1, $4, $4)`,
				fmt.Sprintf("env_br_%d", i), org, fmt.Sprintf("prj_br_%d", i), nowMs,
			)
			if exErr != nil {
				return exErr
			}
			_, exErr = tx.ExecContext(ctx, `
				INSERT INTO request_traces_meta (
					request_id, created_at_ms, started_at_ms, org_id, project_id, env_id,
					entry_point, extension_installation_id, http_status, proxy_overhead_us, total_duration_ms
				) VALUES ($1, $2, $2, $3, $4, $5, 'unified_http', 'ext_shared', 200, 10, 25)`,
				fmt.Sprintf("trc_01J0000000000000000000000%d", i), nowMs+int64(i), org, fmt.Sprintf("prj_br_%d", i), fmt.Sprintf("env_br_%d", i),
			)
			return exErr
		})
		if err != nil {
			t.Fatalf("seed request_traces_meta(%s): %v", org, err)
		}
	}

	instBR, err := audit.QueryBlastRadius(ctx, pgStore.DB(), audit.BlastRadiusQuery{
		AllowInstanceScope:      true,
		ExtensionInstallationID: "ext_shared",
	})
	if err != nil {
		t.Fatalf("QueryBlastRadius(instance): %v", err)
	}
	if instBR.TotalTraceCount != 2 || len(instBR.AffectedOrgs) != 2 || len(instBR.Traces) != 2 {
		t.Fatalf("instance blast radius traces = (total=%d orgs=%v traces=%d), want (2, 2 orgs, 2 traces)", instBR.TotalTraceCount, instBR.AffectedOrgs, len(instBR.Traces))
	}
	if len(instBR.AuditEvents) < writerNodes*eventsPerNode {
		t.Fatalf("instance blast radius audit events = %d, want >= %d (both orgA, orgB, and SYSTEM)", len(instBR.AuditEvents), writerNodes*eventsPerNode)
	}

	// Verify real *audit.Logger wired as postgres.Store AuditSink persists valid JCS audit rows on tenancy mutations.
	auditedStore, err := postgres.Open(ctx, postgres.Options{
		DSN:                      isoDSN,
		AuditSink:                logger,
		SkipMigrate:              true,
		PartitionLookaheadMonths: -1,
	})
	if err != nil {
		t.Fatalf("postgres.Open(AuditSink: logger): %v", err)
	}
	defer func() { _ = auditedStore.Close() }()
	audOrg, err := auditedStore.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "audited-pg-org", DisplayName: "Audited PG Org"})
	if err != nil {
		t.Fatalf("audited CreateOrganization: %v", err)
	}
	audPrj, err := auditedStore.Tenancy().CreateProject(ctx, audOrg.ID, port.CreateProjectParams{Slug: "audited-pg-prj", DisplayName: "Audited PG Prj"})
	if err != nil {
		t.Fatalf("audited CreateProject: %v", err)
	}
	if _, err := auditedStore.Tenancy().CreateEnvironment(ctx, audOrg.ID, audPrj.ID, port.CreateEnvironmentParams{Slug: "prod"}); err != nil {
		t.Fatalf("audited CreateEnvironment: %v", err)
	}
	audRep2, err := audit.VerifyAuditChain(ctx, pgStore.DB(), audit.VerifyOptions{})
	if err != nil || !audRep2.Valid || audRep2.TotalVerified != wantTotal+3 {
		t.Fatalf("VerifyAuditChain after audited tenancy mutations: total=%d valid=%v err=%v", audRep2.TotalVerified, audRep2.Valid, err)
	}
}
