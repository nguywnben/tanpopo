// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package snapshot_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

func TestParsePolicyRulesJSONStrict(t *testing.T) {
	t.Parallel()

	rules, err := snapshot.ParsePolicyRulesJSONStrict(`{
		"allowed_models": ["gpt-4o", "claude-sonnet-4"],
		"denied_models": ["gpt-4o-mini"],
		"allowed_adapters": ["openai-chat"],
		"denied_adapters": ["legacy-adapter"],
		"allowed_data_regions": ["eu-central-1", "eu-west-1"],
		"min_plugin_trust": "verified_only",
		"min_capability_assurance": "tested",
		"rpm_limit": 120,
		"tpm_ceiling": 50000,
		"max_concurrent_streams": 4,
		"max_context_tokens": 16384,
		"max_request_body_bytes": 1048576,
		"guardrail_hooks": [{"extension_id": "ext_pii", "failure_posture": "FAIL_OPEN"}, {"extension_id": "ext_dlp"}]
	}`)
	if err != nil {
		t.Fatalf("ParsePolicyRulesJSONStrict unexpected error: %v", err)
	}
	if !rules.HasAllowedModels || len(rules.AllowedModels) != 2 {
		t.Fatalf("unexpected allowed_models: has=%v %+v", rules.HasAllowedModels, rules.AllowedModels)
	}
	if !rules.HasAllowedDataRegions || len(rules.AllowedDataRegions) != 2 {
		t.Fatalf("unexpected allowed_data_regions: has=%v %+v", rules.HasAllowedDataRegions, rules.AllowedDataRegions)
	}
	if rules.MinPluginTrust != "VERIFIED_ONLY" || rules.MinCapabilityAssurance != "TESTED" {
		t.Fatalf("unexpected trust/assurance: %q / %q", rules.MinPluginTrust, rules.MinCapabilityAssurance)
	}
	if rules.RPMLimit == nil || *rules.RPMLimit != 120 || rules.TPMLimit == nil || *rules.TPMLimit != 50000 {
		t.Fatalf("unexpected rpm/tpm limits: %+v / %+v", rules.RPMLimit, rules.TPMLimit)
	}
	if len(rules.GuardrailHooks) != 2 || rules.GuardrailHooks[1].FailurePosture != snapshot.PolicyFailureClosed {
		t.Fatalf("unexpected guardrail hooks: %+v", rules.GuardrailHooks)
	}

	cp := rules.Clone()
	cp.AllowedModels[0] = "mutated"
	*cp.RPMLimit = 999
	if rules.AllowedModels[0] != "gpt-4o" || *rules.RPMLimit != 120 {
		t.Fatal("Clone did not deep copy slices and numeric pointers")
	}

	// Alias tests and strict rejections
	aliasRules, err := snapshot.ParsePolicyRulesJSONStrict(`{"allowed_regions": ["us-east-1"], "rpm_ceiling": 10, "tpm_limit": 100}`)
	if err != nil || !aliasRules.HasAllowedDataRegions || len(aliasRules.AllowedDataRegions) != 1 || *aliasRules.RPMLimit != 10 {
		t.Fatalf("alias parse failed: rules=%+v err=%v", aliasRules, err)
	}

	invalidCases := []string{
		"",
		"[]",
		`{}{"extra":1}`,
		`{"unknown_field": true}`,
		`{"allowed_models": null}`,
		`{"denied_models": null}`,
		`{"allowed_adapters": null}`,
		`{"denied_adapters": null}`,
		`{"allowed_models": "gpt-4o"}`,
		`{"allowed_data_regions": ["EU"], "allowed_regions": ["US"]}`,
		`{"rpm_limit": 10, "rpm_ceiling": 20}`,
		`{"rpm_limit": -1}`,
		`{"max_context_tokens": 0}`,
		`{"max_request_body_bytes": -5}`,
		`{"min_plugin_trust": "BOGUS"}`,
		`{"min_capability_assurance": "UNKNOWN"}`,
		`{"guardrail_hooks": [{"extension_id": ""}]}`,
		`{"guardrail_hooks": [{"extension_id": "ext_1", "failure_posture": "MAYBE"}]}`,
	}
	for _, tc := range invalidCases {
		if _, err := snapshot.ParsePolicyRulesJSONStrict(tc); err == nil {
			t.Fatalf("expected error for invalid rules_json: %s", tc)
		}
	}
}

func TestConfigSnapshotPoliciesLoadAndFailClosedCorruption(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000100000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "policies.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "pol-snap", DisplayName: "Policy Snap"})
	if err != nil {
		t.Fatal(err)
	}
	db := st.RawDB()
	now := nowFn()

	// Insert disabled invalid row (should be ignored) and enabled valid row.
	if _, err := db.ExecContext(ctx, `INSERT INTO policies (id, org_id, scope_type, scope_id, policy_type, failure_mode, rules_json, enabled, version, created_at_ms, updated_at_ms) VALUES ('pol_disabled', ?, 'ORG', ?, 'RATE_LIMIT', 'FAIL_CLOSED', '{bad json', 0, 1, ?, ?)`, org.ID, org.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO policies (id, org_id, scope_type, scope_id, policy_type, failure_mode, rules_json, enabled, version, created_at_ms, updated_at_ms) VALUES ('pol_valid', ?, 'ORG', ?, 'DATA_RESIDENCY', 'FAIL_CLOSED', '{"allowed_data_regions":["EU-CENTRAL-1"]}', 1, 1, ?, ?)`, org.ID, org.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'policy', 'pol_valid', 'UPSERT', 0, ?)`, org.ID, now); err != nil {
		t.Fatal(err)
	}

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: db, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	snap := mgr.Current()
	if err := snap.PoliciesHealthErr(); err != nil {
		t.Fatalf("expected clean snapshot policies health, got: %v", err)
	}
	pols, err := snap.LookupPoliciesForScope(org.ID, snapshot.PolicyScopeOrg, org.ID)
	if err != nil || len(pols) != 1 || pols[0].ID != "pol_valid" {
		t.Fatalf("unexpected policies lookup: pols=%+v err=%v", pols, err)
	}
	// Mutate clone and verify snapshot immutability.
	pols[0].ParsedRules.AllowedDataRegions[0] = "US-EAST-1"
	pols2, _ := snap.LookupPoliciesForScope(org.ID, snapshot.PolicyScopeOrg, org.ID)
	if pols2[0].ParsedRules.AllowedDataRegions[0] != "EU-CENTRAL-1" {
		t.Fatal("snapshot LookupPoliciesForScope leaked mutable slice")
	}

	// Corrupt enabled row and refresh -> must mark snapshot fail-closed!
	clk.Advance(time.Second)
	now2 := nowFn()
	if _, err := db.ExecContext(ctx, `INSERT INTO policies (id, org_id, scope_type, scope_id, policy_type, failure_mode, rules_json, enabled, version, created_at_ms, updated_at_ms) VALUES ('pol_corrupt', ?, 'ORG', ?, 'RATE_LIMIT', 'FAIL_CLOSED', '{"unknown_corrupt":123}', 1, 1, ?, ?)`, org.ID, org.ID, now2, now2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'policy', 'pol_corrupt', 'UPSERT', 0, ?)`, org.ID, now2); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	snap2 := mgr.Current()
	if err := snap2.PoliciesHealthErr(); err == nil {
		t.Fatal("expected ErrPolicyConfigCorrupted after loading corrupt enabled policy")
	}
	if _, err := snap2.LookupPoliciesForScope(org.ID, snapshot.PolicyScopeOrg, org.ID); err == nil {
		t.Fatal("expected LookupPoliciesForScope to fail closed when policy catalog is corrupted")
	}
}
