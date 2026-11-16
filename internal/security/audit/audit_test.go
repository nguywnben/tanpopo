// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func openAuditTestDB(t *testing.T, fk *clock.FakeClock) (*sqlite.Store, string) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, dbPath
}

func TestJCSRFC8785Canonicalization(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "whitespace_and_key_order",
			in:   `{ "b" : 1 , "a" : 2 , "c" : { "y" : true , "x" : null } }`,
			want: `{"a":2,"b":1,"c":{"x":null,"y":true}}`,
		},
		{
			name: "numbers_rfc8785_es6",
			in:   `{"neg_zero":-0,"pos_zero":0.0,"int":42.0,"exp20":1e20,"exp21":1e21,"small":0.000001,"tiny":0.0000001,"frac":333333333.3333333}`,
			want: `{"exp20":100000000000000000000,"exp21":1e+21,"frac":333333333.3333333,"int":42,"neg_zero":0,"pos_zero":0,"small":0.000001,"tiny":1e-7}`,
		},
		{
			name: "unicode_and_no_html_escape",
			in:   `{"euro":"\u20ac","ctrl":"\u0008\t\n\u000b\f\r\u001f","html":"<script>&\"/\\</script>"}`,
			want: "{\"ctrl\":\"\\b\\t\\n\\u000b\\f\\r\\u001f\",\"euro\":\"€\",\"html\":\"<script>&\\\"/\\\\</script>\"}",
		},
		{
			// RFC 8785 Section 3.2.3: U+10000 encodes in UTF-16 as surrogate pair (0xD800, 0xDC00) < U+FFFD (0xFFFD).
			// Naive UTF-8 sort puts "\ufffd" before "\U00010000", whereas UTF-16 puts "\U00010000" first!
			name: "utf16_code_unit_sort_order",
			in:   `{"\ufffd":1,"\ud800\udc00":2,"\u00e9":3}`,
			want: "{\"é\":3,\"\U00010000\":2,\"\ufffd\":1}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalizeJSON([]byte(tc.in))
			if err != nil {
				t.Fatalf("CanonicalizeJSON(%s): %v", tc.in, err)
			}
			if string(got) != tc.want {
				t.Fatalf("CanonicalizeJSON mismatch:\n got: %s\nwant: %s", string(got), tc.want)
			}
		})
	}

	// Negative cases: invalid UTF-8, trailing tokens, non-finite float.
	if _, err := CanonicalizeJSON([]byte{'"', 0xff, '"'}); !errors.Is(err, ErrInvalidCanonicalJSON) {
		t.Fatalf("expected ErrInvalidCanonicalJSON on invalid UTF-8, got %v", err)
	}
	if _, err := CanonicalizeJSON([]byte(`{"a":1}{"b":2}`)); !errors.Is(err, ErrInvalidCanonicalJSON) {
		t.Fatalf("expected ErrInvalidCanonicalJSON on trailing token, got %v", err)
	}
	if _, err := CanonicalizeValue(math.NaN()); !errors.Is(err, ErrInvalidCanonicalJSON) {
		t.Fatalf("expected ErrInvalidCanonicalJSON on NaN, got %v", err)
	}
}

func TestAC1VerifyAuditChain500InterleavedAndTamperDetection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st, dbPath := openAuditTestDB(t, fk)

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "audit-org-a", DisplayName: "Audit Org A"})
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "audit-org-b", DisplayName: "Audit Org B"})
	if err != nil {
		t.Fatal(err)
	}

	logger, err := NewLogger(LoggerConfig{
		DB:    st.RawDB(),
		NowMs: func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	// Write 500 interleaved audit rows across Org A, Org B, and SYSTEM (ensure Org A & Org B each reach > 250?
	// Wait: insert 500 interleaved across the 3 chains, and append extra up to >=250 on Org A & Org B or distribute cleanly!
	// Specifically: 260 in Org A, 260 in Org B, 80 in SYSTEM = 600 >= 500 total interleaved so seq_no=250 exists cleanly in Org A & Org B!)
	// Also test exact 500 interleaved check first, then append to 260 for seq_no=250 tamper pinpoint.
	chains := []struct {
		orgPtr *string
		name   string
	}{
		{orgPtr: &orgA.ID, name: orgA.ID},
		{orgPtr: &orgB.ID, name: orgB.ID},
		{orgPtr: nil, name: SystemChainKey},
	}

	for i := 0; i < 500; i++ {
		c := chains[i%3]
		fk.Advance(10)
		reqID := fmt.Sprintf("req_01TEST%018d", i)
		err := logger.Append(ctx, port.AuditEvent{
			ID:           idgen.MustNew(idgen.PrefixAudit),
			CreatedAtMs:  fk.Now().UTC().UnixMilli(),
			OrgID:        c.orgPtr,
			ActorKind:    "PRINCIPAL",
			ActorID:      fmt.Sprintf("prn_%03d", i%7),
			EntryPoint:   port.AuditEntryPointAdminAPI,
			EventType:    "config.mutated",
			TargetType:   "virtual_key",
			TargetID:     fmt.Sprintf("vk_%04d", i),
			RequestID:    &reqID,
			MetadataJSON: fmt.Sprintf(`{"step":%d,"ok":true}`, i),
		})
		if err != nil {
			t.Fatalf("Append i=%d chain=%s: %v", i, c.name, err)
		}
	}

	// Verify all 500 interleaved rows across the 3 chains validate 100% cleanly.
	report, err := VerifyAuditChain(ctx, st.RawDB(), VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyAuditChain(500): %v", err)
	}
	if !report.Valid || report.TotalVerified != 500 || len(report.Chains) != 3 {
		t.Fatalf("unexpected 500-row VerifyReport: valid=%v total=%d chains=%d", report.Valid, report.TotalVerified, len(report.Chains))
	}

	// Append further rows on Org A and Org B so both exceed seq_no = 250.
	for report.Chains[orgA.ID].LastSeqNo < 260 || report.Chains[orgB.ID].LastSeqNo < 260 {
		for _, op := range []*string{&orgA.ID, &orgB.ID} {
			fk.Advance(10)
			if err := logger.Append(ctx, port.AuditEvent{
				CreatedAtMs:  fk.Now().UTC().UnixMilli(),
				OrgID:        op,
				ActorKind:    "PRINCIPAL",
				ActorID:      "prn_extra",
				EntryPoint:   port.AuditEntryPointAdminAPI,
				EventType:    "config.mutated",
				TargetType:   "policy",
				TargetID:     "pol_extra",
				MetadataJSON: `{"extra":true}`,
			}); err != nil {
				t.Fatal(err)
			}
		}
		report, err = VerifyAuditChain(ctx, st.RawDB(), VerifyOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}

	// NEG-1: Over normal application DB connection, UPDATE and DELETE must abort immediately.
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE audit_events SET actor_id = 'evil' WHERE chain_key = ? AND seq_no = 1`, orgA.ID); err == nil || !strings.Contains(err.Error(), "audit_events_immutable") {
		t.Fatalf("expected NEG-1 UPDATE abort, got %v", err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `DELETE FROM audit_events WHERE chain_key = ? AND seq_no = 1`, orgA.ID); err == nil || !strings.Contains(err.Error(), "audit_events_immutable") {
		t.Fatalf("expected NEG-1 DELETE abort, got %v", err)
	}

	// Copy raw SQLite file to simulate offline disk tampering while preserving original DB clean.
	// Case A: Mutate 1 JSON character at seq_no = 250 in Org A.
	tamperPathA := filepath.Join(t.TempDir(), "tamper_flip.db")
	copySQLiteDB(t, st.RawDB(), dbPath, tamperPathA)

	dbTamperA, err := sql.Open("sqlite", tamperPathA)
	if err != nil {
		t.Fatal(err)
	}
	defer dbTamperA.Close()

	if _, err := dbTamperA.ExecContext(ctx, `DROP TRIGGER trg_audit_events_no_update`); err != nil {
		t.Fatalf("drop trigger in tamper harness A: %v", err)
	}
	resFlip, err := dbTamperA.ExecContext(ctx, `
		UPDATE audit_events
		SET metadata_json = replace(metadata_json, 'true', 'false')
		WHERE chain_key = ? AND seq_no = 250`, orgA.ID,
	)
	if err != nil {
		t.Fatalf("tamper 1 char at seq_no=250: %v", err)
	}
	if n, _ := resFlip.RowsAffected(); n != 1 {
		t.Fatalf("expected 1 row mutated at seq_no=250, got %d", n)
	}

	var alertsA []TamperFinding
	repA, errA := VerifyAuditChain(ctx, dbTamperA, VerifyOptions{
		OnTamper: func(f TamperFinding) { alertsA = append(alertsA, f) },
	})
	if !errors.Is(errA, ErrAuditTampered) {
		t.Fatalf("expected ErrAuditTampered on flipped JSON byte, got %v", errA)
	}
	if repA.Valid || len(alertsA) == 0 {
		t.Fatalf("expected ALT-AUDIT-TAMPER callback and invalid report, got valid=%v alerts=%d", repA.Valid, len(alertsA))
	}
	if alertsA[0].AlertCode != AlertAuditTamper || alertsA[0].ChainKey != orgA.ID || alertsA[0].SeqNo != 250 {
		t.Fatalf("expected ALT-AUDIT-TAMPER at (%s, seq=250), got %+v", orgA.ID, alertsA[0])
	}

	// Case B: Delete seq_no = 250 in Org B.
	tamperPathB := filepath.Join(t.TempDir(), "tamper_delete.db")
	copySQLiteDB(t, st.RawDB(), dbPath, tamperPathB)

	dbTamperB, err := sql.Open("sqlite", tamperPathB)
	if err != nil {
		t.Fatal(err)
	}
	defer dbTamperB.Close()

	if _, err := dbTamperB.ExecContext(ctx, `DROP TRIGGER trg_audit_events_no_delete`); err != nil {
		t.Fatalf("drop delete trigger in tamper harness B: %v", err)
	}
	if _, err := dbTamperB.ExecContext(ctx, `DELETE FROM audit_events WHERE chain_key = ? AND seq_no = 250`, orgB.ID); err != nil {
		t.Fatalf("delete seq_no=250 in tamper harness B: %v", err)
	}

	var alertsB []TamperFinding
	repB, errB := VerifyAuditChain(ctx, dbTamperB, VerifyOptions{
		OnTamper: func(f TamperFinding) { alertsB = append(alertsB, f) },
	})
	if !errors.Is(errB, ErrAuditTampered) {
		t.Fatalf("expected ErrAuditTampered on deleted seq_no=250, got %v", errB)
	}
	if repB.Valid || len(alertsB) == 0 {
		t.Fatalf("expected ALT-AUDIT-TAMPER on deletion, got valid=%v alerts=%d", repB.Valid, len(alertsB))
	}
	if alertsB[0].AlertCode != AlertAuditTamper || alertsB[0].ChainKey != orgB.ID || alertsB[0].SeqNo != 250 || alertsB[0].Kind != TamperMissingSequence {
		t.Fatalf("expected MISSING_SEQUENCE ALT-AUDIT-TAMPER at (%s, seq=250), got %+v", orgB.ID, alertsB[0])
	}
}

func TestLoggerSecretScrubbingAndConcurrentAppends(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st, _ := openAuditTestDB(t, fk)

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "scrub-org", DisplayName: "Scrub Org"})
	if err != nil {
		t.Fatal(err)
	}

	logger, err := NewLogger(LoggerConfig{
		DB:    st.RawDB(),
		NowMs: func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := logger.Append(ctx, port.AuditEvent{
		OrgID:        &orgA.ID,
		ActorKind:    "PRINCIPAL",
		ActorID:      "prn_sec",
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "debug.note",
		TargetType:   "virtual_key",
		TargetID:     "vk_1",
		MetadataJSON: `{"leaked":"Bearer supersecret_token_value_123456","vk":"vk_tp_prod_AbCdEfGhIjKlMnOpQrStUvWxYz012345","upstream":"sk-ant-api03-secretsecret1234"}`,
	}); err != nil {
		t.Fatal(err)
	}

	events, err := logger.List(ctx, ListFilter{ChainKey: orgA.ID})
	if err != nil || len(events) != 1 {
		t.Fatalf("List: err=%v len=%d", err, len(events))
	}
	if strings.Contains(events[0].MetadataJSON, "supersecret_token") ||
		strings.Contains(events[0].MetadataJSON, "vk_tp_prod_") ||
		strings.Contains(events[0].MetadataJSON, "sk-ant-") {
		t.Fatalf("expected secret leakage to be scrubbed from audit metadata_json, got %s", events[0].MetadataJSON)
	}

	// Concurrent appends serialize cleanly without sequence collisions.
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs <- logger.Append(ctx, port.AuditEvent{
				OrgID:        &orgA.ID,
				ActorKind:    "SYSTEM",
				ActorID:      "worker",
				EntryPoint:   port.AuditEntryPointSystemReaper,
				EventType:    "worker.tick",
				TargetType:   "system",
				TargetID:     fmt.Sprintf("job_%d", idx),
				MetadataJSON: fmt.Sprintf(`{"idx":%d}`, idx),
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatalf("concurrent Append failed: %v", e)
		}
	}

	rep, err := VerifyAuditChain(ctx, st.RawDB(), VerifyOptions{ChainKeys: []string{orgA.ID}})
	if err != nil {
		t.Fatalf("VerifyAuditChain after concurrent appends: %v", err)
	}
	if rep.Chains[orgA.ID].LastSeqNo != 13 {
		t.Fatalf("expected LastSeqNo=13, got %+v", rep.Chains[orgA.ID])
	}
}

func TestBlastRadiusQueryCorrelationAndTenantIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st, _ := openAuditTestDB(t, fk)

	orgA, _ := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "br-org-a", DisplayName: "BR Org A"})
	orgB, _ := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "br-org-b", DisplayName: "BR Org B"})
	t0 := fk.Now().UTC().UnixMilli()

	// Seed request_traces_meta rows spanning 2 orgs, 2 projects in Org A, 2 plugins, plus out-of-window trace.
	insertTrace := func(reqID string, ts int64, orgID, projID, envID, vkID, depID, connID, extID string) {
		_, err := st.RawDB().ExecContext(ctx, `
			INSERT INTO request_traces_meta (
				request_id, created_at_ms, started_at_ms, org_id, project_id, env_id, virtual_key_id,
				entry_point, selected_deployment_id, connection_id, extension_installation_id,
				failover_attempts, http_status, proxy_overhead_us, total_duration_ms, waterfall_stages_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, 'unified_http', ?, ?, ?, 0, 200, 120, 45, '[]')`,
			reqID, ts, ts-45, orgID, projID, envID, vkID, depID, connID, extID,
		)
		if err != nil {
			t.Fatalf("insert trace %s: %v", reqID, err)
		}
	}

	insertTrace("req_01A1", t0+1000, orgA.ID, "prj_a1", "env_a1", "vk_a1", "dep_1", "conn_a1", "ins_bad_plugin")
	insertTrace("req_01A2", t0+2000, orgA.ID, "prj_a2", "env_a2", "vk_a2", "dep_2", "conn_a1", "ins_bad_plugin")
	insertTrace("req_01A3", t0+90000, orgA.ID, "prj_a3", "env_a3", "vk_a3", "dep_3", "conn_a1", "ins_bad_plugin") // out of window
	insertTrace("req_01A4", t0+1500, orgA.ID, "prj_a1", "env_a1", "vk_other", "dep_4", "conn_a2", "ins_good_plugin")
	insertTrace("req_01B1", t0+1500, orgB.ID, "prj_b1", "env_b1", "vk_b1", "dep_b", "conn_b1", "ins_bad_plugin") // sibling org

	logger, err := NewLogger(LoggerConfig{DB: st.RawDB(), NowMs: func() int64 { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Append(ctx, port.AuditEvent{
		CreatedAtMs:  t0 + 1200,
		OrgID:        nil,
		ActorKind:    "PRINCIPAL",
		ActorID:      "prn_op",
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "extension.quarantined",
		TargetType:   "extension_installation",
		TargetID:     "ins_bad_plugin",
		MetadataJSON: `{"reason":"cve_2026"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := logger.Append(ctx, port.AuditEvent{
		CreatedAtMs:  t0 + 1300,
		OrgID:        &orgB.ID,
		ActorKind:    "PRINCIPAL",
		ActorID:      "prn_b",
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "provider_connection.disabled",
		TargetType:   "extension_installation",
		TargetID:     "ins_bad_plugin",
		MetadataJSON: `{"org":"b"}`,
	}); err != nil {
		t.Fatal(err)
	}

	// Tenant-scoped query for Org A within [t0, t0+5000] MUST NOT include Org B traces/audit or out-of-window req_01A3.
	repA, err := QueryBlastRadius(ctx, st.RawDB(), BlastRadiusQuery{
		OrgID:                   orgA.ID,
		ExtensionInstallationID: "ins_bad_plugin",
		FromMs:                  t0,
		ToMs:                    t0 + 5000,
	})
	if err != nil {
		t.Fatalf("QueryBlastRadius Org A: %v", err)
	}
	if repA.TotalTraceCount != 2 || len(repA.Traces) != 2 {
		t.Fatalf("expected 2 impacted Org A traces, got total=%d traces=%+v", repA.TotalTraceCount, repA.Traces)
	}
	if len(repA.AffectedProjects) != 2 || len(repA.AffectedVirtualKeys) != 2 {
		t.Fatalf("expected 2 affected projects and 2 virtual keys, got projs=%v vks=%v", repA.AffectedProjects, repA.AffectedVirtualKeys)
	}
	for _, ev := range repA.AuditEvents {
		if ev.OrgID != nil && *ev.OrgID == orgB.ID {
			t.Fatalf("cross-tenant audit leak from Org B into Org A blast radius: %+v", ev)
		}
		if ev.ChainKey == SystemChainKey {
			t.Fatalf("tenant query without AllowInstanceScope leaked SYSTEM audit event: %+v", ev)
		}
	}

	// Instance-scoped operator query sees both Org A and Org B affected resources AND the SYSTEM audit event.
	repInst, err := QueryBlastRadius(ctx, st.RawDB(), BlastRadiusQuery{
		AllowInstanceScope:      true,
		ExtensionInstallationID: "ins_bad_plugin",
		FromMs:                  t0,
		ToMs:                    t0 + 5000,
	})
	if err != nil {
		t.Fatalf("QueryBlastRadius Instance scope: %v", err)
	}
	if repInst.TotalTraceCount != 3 || len(repInst.AffectedOrgs) != 2 {
		t.Fatalf("expected 3 impacted traces across 2 orgs, got total=%d orgs=%v", repInst.TotalTraceCount, repInst.AffectedOrgs)
	}
	sawSystemEvent := false
	for _, ev := range repInst.AuditEvents {
		if ev.ChainKey == SystemChainKey && ev.EventType == "extension.quarantined" {
			sawSystemEvent = true
		}
	}
	if !sawSystemEvent {
		t.Fatalf("expected instance-scoped blast radius query to include SYSTEM extension.quarantined audit event, got %+v", repInst.AuditEvents)
	}

	// Verify Limit: 1 still returns FULL unpaginated AffectedProjects / AffectedVirtualKeys AND
	// correlates emergency_kill_switch audit events where TargetID is ksw_... and metadata_json.target_id is ins_bad_plugin.
	if err := logger.Append(ctx, port.AuditEvent{
		CreatedAtMs:  t0 + 1400,
		OrgID:        nil,
		ActorKind:    "PRINCIPAL",
		ActorID:      "prn_op",
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "kill_switch.triggered",
		TargetType:   "emergency_kill_switch",
		TargetID:     "ksw_01BADPLUGIN",
		MetadataJSON: `{"level":"EXTENSION_INSTALLATION","target_id":"ins_bad_plugin"}`,
	}); err != nil {
		t.Fatal(err)
	}
	repPaginated, err := QueryBlastRadius(ctx, st.RawDB(), BlastRadiusQuery{
		AllowInstanceScope:      true,
		ExtensionInstallationID: "ins_bad_plugin",
		FromMs:                  t0,
		ToMs:                    t0 + 5000,
		Limit:                   1,
	})
	if err != nil {
		t.Fatalf("QueryBlastRadius paginated: %v", err)
	}
	if repPaginated.TotalTraceCount != 3 || len(repPaginated.Traces) != 1 {
		t.Fatalf("expected TotalTraceCount=3 with 1 paginated trace, got total=%d len=%d", repPaginated.TotalTraceCount, len(repPaginated.Traces))
	}
	if len(repPaginated.AffectedOrgs) != 2 || len(repPaginated.AffectedProjects) != 3 || len(repPaginated.AffectedVirtualKeys) != 3 {
		t.Fatalf("expected unpaginated affected sets under Limit=1, got orgs=%v projs=%v vks=%v",
			repPaginated.AffectedOrgs, repPaginated.AffectedProjects, repPaginated.AffectedVirtualKeys)
	}
	if len(repPaginated.AuditEvents) != 1 || repPaginated.AuditEvents[0].EventType != "kill_switch.triggered" {
		t.Fatalf("expected paginated Limit=1 audit event kill_switch.triggered via metadata_json.target_id, got %+v", repPaginated.AuditEvents)
	}
}

func TestLoggerKeyAwareScrubbingAndRequestIDSanitization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st, _ := openAuditTestDB(t, fk)

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "scrub-adv-org", DisplayName: "Scrub Adv Org"})
	if err != nil {
		t.Fatal(err)
	}
	logger, err := NewLogger(LoggerConfig{
		DB:    st.RawDB(),
		NowMs: func() int64 { return fk.Now().UTC().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}

	reqWithSecret := "req_01TEST?token=sk-ant-api03-verysecretvalue123"
	if err := logger.Append(ctx, port.AuditEvent{
		OrgID:        &orgA.ID,
		ActorKind:    "PRINCIPAL",
		ActorID:      "prn_sec",
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "debug.privacy",
		TargetType:   "connection",
		TargetID:     "conn_1",
		RequestID:    &reqWithSecret,
		MetadataJSON: `{"api_key":"raw_secret_no_prefix","password":"hunter2","credentials":{"access_token":"tok123"},"endpoint":"https://user:secretpass@api.example.com/v1","basic_hdr":"Basic dXNlcjpwYXNz","gh":"ghp_1234567890abcdefghijklmnop","input_tokens":128,"max_tokens":4096,"token_count":256}`,
	}); err != nil {
		t.Fatal(err)
	}

	// Top-level null metadata_json normalizes cleanly to {} without panicking or failing JCS.
	if err := logger.Append(ctx, port.AuditEvent{
		OrgID:        &orgA.ID,
		ActorKind:    "SYSTEM",
		ActorID:      "worker",
		EntryPoint:   port.AuditEntryPointSystemReaper,
		EventType:    "debug.null_meta",
		TargetType:   "system",
		TargetID:     "sys_1",
		MetadataJSON: "null",
	}); err != nil {
		t.Fatalf("Append null metadata_json: %v", err)
	}

	events, err := logger.List(ctx, ListFilter{ChainKey: orgA.ID})
	if err != nil || len(events) != 2 {
		t.Fatalf("List: err=%v len=%d", err, len(events))
	}
	ev0 := events[1] // chronological descending; oldest is debug.privacy (seq 1)
	if ev0.RequestID == nil || strings.Contains(*ev0.RequestID, "sk-ant-") {
		t.Fatalf("expected RequestID scrubbed of secrets, got %v", ev0.RequestID)
	}
	for _, leak := range []string{"raw_secret_no_prefix", "hunter2", "tok123", "secretpass", "dXNlcjpwYXNz", "ghp_1234567890"} {
		if strings.Contains(ev0.MetadataJSON, leak) {
			t.Fatalf("expected secret %q redacted from audit metadata_json, got %s", leak, ev0.MetadataJSON)
		}
	}
	for _, safeTokenField := range []string{`"input_tokens":128`, `"max_tokens":4096`, `"token_count":256`} {
		if !strings.Contains(ev0.MetadataJSON, safeTokenField) {
			t.Fatalf("expected telemetry count %s preserved unredacted, got %s", safeTokenField, ev0.MetadataJSON)
		}
	}
	if events[0].MetadataJSON != "{}" {
		t.Fatalf("expected null metadata_json normalized to {}, got %q", events[0].MetadataJSON)
	}
}

func copySQLiteDB(t *testing.T, srcDB *sql.DB, srcPath, dstPath string) {
	t.Helper()
	if _, err := srcDB.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("wal_checkpoint: %v", err)
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read src sqlite file: %v", err)
	}
	if err := os.WriteFile(dstPath, raw, 0o600); err != nil {
		t.Fatalf("write dst sqlite file: %v", err)
	}
}
