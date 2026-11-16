// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package resolver_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

type routingFixture struct {
	clk   *clock.FakeClock
	st    *sqlite.Store
	mgr   *snapshot.Manager
	orgID string
	prjID string
	envID string
}

func setupRoutingFixture(t testing.TB) *routingFixture {
	t.Helper()
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000100000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "routing.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	pubPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubFP := trust.PublicKeyFingerprint(pubPub)

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "rte-org", DisplayName: "Route Org"})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "rte-proj", DisplayName: "Route Proj"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}

	now := nowFn()
	db := st.RawDB()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed error: %v", err)
		}
	}
	exec(`INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms) VALUES ('exp_1', '@acme/openai', 'PROVIDER_ADAPTER', 'acme', 'verified', ?)`, now)
	exec(`INSERT INTO extension_versions (id, package_id, semver, contract_version, core_version_range, execution_tier, bundle_sha256, signature_verified, signing_key_fingerprint, source_uri, manifest_json, created_at_ms) VALUES ('exv_1', 'exp_1', '1.0.0', '0.1', '>=0.1.0', 'WASM_SANDBOX_T2', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 1, ?, 'file://acme.tpkg', '{"kind":"ExtensionManifest"}', ?)`, pubFP, now)
	exec(`INSERT INTO extension_installations (id, package_id, active_version_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms) VALUES ('ins_1', 'exp_1', 'exv_1', 'ENABLED_ACTIVE', '{"allowed_hosts":["api.eu.example","api.us.example"]}', 1, ?, ?)`, now, now)
	exec(`INSERT INTO provider_adapters (id, installation_id, adapter_slug, native_prefix, supported_dialects_json, allowed_hosts_json, version, created_at_ms) VALUES ('adp_1', 'ins_1', 'acme-openai', '/native/openai', '["openai"]', '["api.eu.example","api.us.example"]', 1, ?)`, now)

	exec(`INSERT INTO provider_connections (id, org_id, owner_project_id, adapter_id, name, endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region, billing_mode, auth_mode, state, shared_project_ids_json, config_json, version, created_at_ms, updated_at_ms) VALUES
		('con_eu', ?, ?, 'adp_1', 'eu-conn', 'https://api.eu.example', 0, '[]', 'EU', 'COMMERCIAL_API', 'API_KEY', 'ACTIVE', '[]', '{}', 1, ?, ?),
		('con_us', ?, ?, 'adp_1', 'us-conn', 'https://api.us.example', 0, '[]', 'US', 'COMMERCIAL_API', 'API_KEY', 'ACTIVE', '[]', '{}', 1, ?, ?)`,
		org.ID, proj.ID, now, now, org.ID, proj.ID, now, now)

	exec(`INSERT INTO logical_models (id, org_id, canonical_slug, modality_kind, context_window, max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms) VALUES
		('lmod_1', NULL, 'acme/pro', 'CHAT', 128000, 4096, '{"streaming":"VERIFIED","tool_calling":"VERIFIED"}', 1, ?, ?)`, now, now)

	// 4 deployments:
	// dep_eu_expensive: EU, supports tool_calling & strict_json_schema, input=5000, output=10000
	// dep_eu_cheap: EU, supports tool_calling, strict_json_schema=NONE (missing), input=500, output=1000
	// dep_eu_fallback: EU, supports tool_calling & strict_json_schema, input=3000, output=6000
	// dep_us_backup: US, cheap, supports all caps
	exec(`INSERT INTO model_deployments (id, org_id, connection_id, logical_model_id, upstream_model_name, native_dialect, capability_overrides_json, assurance_level, pricing_nano_usd_json, state, connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms, version, created_at_ms, updated_at_ms) VALUES
		('dep_eu_expensive', ?, 'con_eu', 'lmod_1', 'pro-eu-1', 'openai', '{"strict_json_schema":"VERIFIED"}', 'TESTED', '{"input_per_1m_nano_usd":5000,"output_per_1m_nano_usd":10000}', 'ACTIVE_HEALTHY', 5000, 30000, 30000, 1, ?, ?),
		('dep_eu_cheap', ?, 'con_eu', 'lmod_1', 'pro-eu-cheap', 'openai', '{"strict_json_schema":"NONE","tool_calling":"NONE"}', 'TESTED', '{"input_per_1m_nano_usd":500,"output_per_1m_nano_usd":1000}', 'ACTIVE_HEALTHY', 5000, 30000, 30000, 1, ?, ?),
		('dep_eu_fallback', ?, 'con_eu', 'lmod_1', 'pro-eu-2', 'openai', '{"strict_json_schema":"VERIFIED"}', 'TESTED', '{"input_per_1m_nano_usd":3000,"output_per_1m_nano_usd":6000}', 'ACTIVE_HEALTHY', 5000, 30000, 30000, 1, ?, ?),
		('dep_us_backup', ?, 'con_us', 'lmod_1', 'pro-us-1', 'openai', '{"strict_json_schema":"VERIFIED"}', 'TESTED', '{"input_per_1m_nano_usd":100,"output_per_1m_nano_usd":200}', 'ACTIVE_HEALTHY', 5000, 30000, 30000, 1, ?, ?)`,
		org.ID, now, now, org.ID, now, now, org.ID, now, now, org.ID, now, now)

	// Priority failover route with 4 targets including US misconfigured target.
	exec(`INSERT INTO route_policies (id, org_id, project_id, env_id, route_slug, strategy, max_failover_hops, required_region, version, created_at_ms, updated_at_ms) VALUES
		('rte_prod', ?, ?, ?, 'prod-assistant', 'PRIORITY_FAILOVER', 3, NULL, 1, ?, ?),
		('rte_cost', ?, ?, ?, 'cost-assistant', 'LOWEST_COST', 2, NULL, 1, ?, ?),
		('rte_wrr', ?, ?, ?, 'wrr-assistant', 'WEIGHTED_ROUND_ROBIN', 1, NULL, 1, ?, ?),
		('rte_lat', ?, ?, ?, 'lat-assistant', 'LOWEST_LATENCY_EWMA', 2, NULL, 1, ?, ?)`,
		org.ID, proj.ID, env.ID, now, now,
		org.ID, proj.ID, env.ID, now, now,
		org.ID, proj.ID, env.ID, now, now,
		org.ID, proj.ID, env.ID, now, now)

	exec(`INSERT INTO route_targets (org_id, route_id, deployment_id, priority_order, weight) VALUES
		(?, 'rte_prod', 'dep_eu_expensive', 1, 100),
		(?, 'rte_prod', 'dep_eu_cheap', 2, 100),
		(?, 'rte_prod', 'dep_eu_fallback', 3, 100),
		(?, 'rte_prod', 'dep_us_backup', 4, 100),
		(?, 'rte_cost', 'dep_eu_expensive', 1, 100),
		(?, 'rte_cost', 'dep_eu_fallback', 2, 100),
		(?, 'rte_cost', 'dep_eu_cheap', 3, 100),
		(?, 'rte_wrr', 'dep_eu_expensive', 1, 75),
		(?, 'rte_wrr', 'dep_eu_fallback', 1, 25),
		(?, 'rte_lat', 'dep_eu_expensive', 1, 100),
		(?, 'rte_lat', 'dep_eu_fallback', 2, 100)`,
		org.ID, org.ID, org.ID, org.ID, org.ID, org.ID, org.ID, org.ID, org.ID, org.ID, org.ID)

	exec(`INSERT INTO policies (id, org_id, scope_type, scope_id, policy_type, failure_mode, rules_json, version, created_at_ms, updated_at_ms) VALUES
		('pol_eu', ?, 'PROJECT', ?, 'DATA_RESIDENCY', 'FAIL_CLOSED', '{"allowed_data_regions":["EU"]}', 1, ?, ?)`,
		org.ID, proj.ID, now, now)

	// Also seed a second organization + second unshared project inside org1 to prove:
	// (a) SQLite composite FK rejects inserting cross-org route_targets;
	// (b) Resolver rejects unshared cross-project connections inside the same org;
	// (c) Direct 1:1 LogicalModel resolution never sees another organization's deployments.
	org2, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "other-org", DisplayName: "Other Org"})
	if err != nil {
		t.Fatal(err)
	}
	proj2, err := st.Tenancy().CreateProject(ctx, org2.ID, port.CreateProjectParams{Slug: "other-proj", DisplayName: "Other Proj"})
	if err != nil {
		t.Fatal(err)
	}
	projUnshared, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "unshared-proj", DisplayName: "Unshared Proj"})
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO provider_connections (id, org_id, owner_project_id, adapter_id, name, endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region, billing_mode, auth_mode, state, shared_project_ids_json, config_json, version, created_at_ms, updated_at_ms) VALUES
		('con_other_org', ?, ?, 'adp_1', 'other-conn', 'https://api.eu.example', 0, '[]', 'EU', 'COMMERCIAL_API', 'API_KEY', 'ACTIVE', '[]', '{}', 1, ?, ?),
		('con_unshared', ?, ?, 'adp_1', 'unshared-conn', 'https://api.eu.example', 0, '[]', 'EU', 'COMMERCIAL_API', 'API_KEY', 'ACTIVE', '[]', '{}', 1, ?, ?)`,
		org2.ID, proj2.ID, now, now, org.ID, projUnshared.ID, now, now)
	exec(`INSERT INTO model_deployments (id, org_id, connection_id, logical_model_id, upstream_model_name, native_dialect, capability_overrides_json, assurance_level, pricing_nano_usd_json, state, connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms, version, created_at_ms, updated_at_ms) VALUES
		('dep_other_org', ?, 'con_other_org', 'lmod_1', 'pro-other', 'openai', '{"strict_json_schema":"VERIFIED"}', 'TESTED', '{"input_per_1m_nano_usd":10,"output_per_1m_nano_usd":20}', 'ACTIVE_HEALTHY', 5000, 30000, 30000, 1, ?, ?),
		('dep_unshared', ?, 'con_unshared', 'lmod_1', 'pro-unshared', 'openai', '{"strict_json_schema":"VERIFIED"}', 'TESTED', '{"input_per_1m_nano_usd":10,"output_per_1m_nano_usd":20}', 'ACTIVE_HEALTHY', 5000, 30000, 30000, 1, ?, ?)`,
		org2.ID, now, now, org.ID, now, now)
	exec(`INSERT INTO route_policies (id, org_id, project_id, env_id, route_slug, strategy, max_failover_hops, required_region, version, created_at_ms, updated_at_ms) VALUES
		('rte_cross', ?, ?, ?, 'cross-org-assistant', 'PRIORITY_FAILOVER', 2, NULL, 1, ?, ?),
		('rte_weighted', ?, ?, ?, 'weighted-assistant', 'WEIGHTED', 1, NULL, 1, ?, ?)`,
		org.ID, proj.ID, env.ID, now, now, org.ID, proj.ID, env.ID, now, now)
	if _, fkErr := db.ExecContext(ctx, `INSERT INTO route_targets (org_id, route_id, deployment_id, priority_order, weight) VALUES (?, 'rte_cross', 'dep_other_org', 1, 100)`, org.ID); fkErr == nil {
		t.Fatalf("expected composite FK (org_id, deployment_id) to reject cross-org route_target")
	}
	exec(`INSERT INTO route_targets (org_id, route_id, deployment_id, priority_order, weight) VALUES
		(?, 'rte_cross', 'dep_unshared', 1, 100),
		(?, 'rte_cross', 'dep_eu_fallback', 2, 100),
		(?, 'rte_weighted', 'dep_eu_expensive', 1, 80),
		(?, 'rte_weighted', 'dep_eu_fallback', 1, 20)`,
		org.ID, org.ID, org.ID, org.ID)

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{
		DB:    db,
		NowMs: nowFn,
		Keyring: trust.TrustedKeyring{
			PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": pubPub},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &routingFixture{clk: clk, st: st, mgr: mgr, orgID: org.ID, prjID: proj.ID, envID: env.ID}
}

func resolveEUPolicy(t testing.TB, fix *routingFixture) policyengine.EffectivePolicy {
	t.Helper()
	eff, err := policyengine.NewEvaluator().ResolveEffective(fix.mgr.Current(), &snapshot.EffectiveConfig{
		OrgID:     fix.orgID,
		ProjectID: fix.prjID,
		EnvID:     fix.envID,
	}, nil)
	if err != nil {
		t.Fatalf("ResolveEffective failed: %v", err)
	}
	return eff
}

func TestSixStepResolverCT08ResidencyCapabilityAndExplain(t *testing.T) {
	t.Parallel()
	fix := setupRoutingFixture(t)
	cb := breaker.New(breaker.Config{Now: func() time.Time { return fix.clk.Now() }})
	res := resolver.New()
	effEU := resolveEUPolicy(t, fix)

	// Explain with EU residency + strict_json_schema:
	// - dep_us_backup pruned in Step 2 (DATA_RESIDENCY_MISMATCH)
	// - dep_eu_cheap pruned in Step 3 (CAPABILITY_NOT_SUPPORTED: strict_json_schema)
	// - dep_eu_expensive (#1) and dep_eu_fallback (#2) survive!
	explain := res.Explain(resolver.ResolveRequest{
		RequestID:            "req_explain_01",
		Snapshot:             fix.mgr.Current(),
		OrgID:                fix.orgID,
		ProjectID:            fix.prjID,
		EnvID:                fix.envID,
		TargetModel:          "prod-assistant",
		EffectivePolicy:      effEU,
		RequiredCapabilities: []string{"streaming", "tool_calling", "strict_json_schema"},
		EstimatedInputTokens: 1000,
		MaxOutputTokens:      500,
		Breaker:              cb,
	})
	if explain.Rejection != nil {
		t.Fatalf("unexpected Explain rejection: %v (rejected=%+v strategy=%s)", explain.Rejection, explain.RejectedDeployments, explain.Strategy)
	}
	if len(explain.SelectedCandidatesOrdered) != 2 {
		t.Fatalf("expected 2 selected EU candidates, got %+v", explain.SelectedCandidatesOrdered)
	}
	if explain.SelectedCandidatesOrdered[0].DeploymentID != "dep_eu_expensive" ||
		explain.SelectedCandidatesOrdered[1].DeploymentID != "dep_eu_fallback" {
		t.Fatalf("unexpected candidate order: %+v", explain.SelectedCandidatesOrdered)
	}
	for _, sel := range explain.SelectedCandidatesOrdered {
		if sel.DataRegion != "EU" {
			t.Fatalf("CT-08 violation: non-EU candidate leaked into selected list: %+v", sel)
		}
	}
	if len(explain.RejectedDeployments) != 2 {
		t.Fatalf("expected 2 rejected deployments in explain tree, got %+v", explain.RejectedDeployments)
	}

	// If ALL EU capable deployments have open circuits while dep_us_backup is healthy,
	// Step 2 MUST still exclude US -> 503 no_compliant_deployment_available!
	for i := 0; i < 5; i++ {
		cb.RecordOutcome("dep_eu_expensive", breaker.AttemptOutcome{Kind: breaker.OutcomeSystemFault})
		cb.RecordOutcome("dep_eu_fallback", breaker.AttemptOutcome{Kind: breaker.OutcomeSystemFault})
	}
	cands, rej := res.Resolve(resolver.ResolveRequest{
		RequestID:            "req_ct08_exhausted",
		Snapshot:             fix.mgr.Current(),
		OrgID:                fix.orgID,
		ProjectID:            fix.prjID,
		EnvID:                fix.envID,
		TargetModel:          "prod-assistant",
		EffectivePolicy:      effEU,
		RequiredCapabilities: []string{"tool_calling"},
		Breaker:              cb,
	})
	if rej == nil || len(cands) != 0 || rej.HTTPStatus != 503 || rej.Code != "no_compliant_deployment_available" {
		t.Fatalf("CT-08 fail-closed violation: cands=%d rej=%+v", len(cands), rej)
	}

	// NEG-1: Require reasoning capability (not supported by any EU deployment) -> 400 capability_not_supported.
	_, capRej := res.Resolve(resolver.ResolveRequest{
		RequestID:            "req_neg1_cap",
		Snapshot:             fix.mgr.Current(),
		OrgID:                fix.orgID,
		ProjectID:            fix.prjID,
		EnvID:                fix.envID,
		TargetModel:          "prod-assistant",
		EffectivePolicy:      effEU,
		RequiredCapabilities: []string{"reasoning"},
		Breaker:              cb,
	})
	if capRej == nil || capRej.HTTPStatus != 400 || capRej.Code != "capability_not_supported" {
		t.Fatalf("expected 400 capability_not_supported, got %+v", capRej)
	}
}

func TestSixStepResolverStrategiesAndCrossOrgIsolation(t *testing.T) {
	t.Parallel()
	fix := setupRoutingFixture(t)
	cb := breaker.New(breaker.Config{Now: func() time.Time { return fix.clk.Now() }})
	res := resolver.New()
	effEU := resolveEUPolicy(t, fix)

	// Security Invariant: unshared cross-project deployment & foreign-org deployment MUST NEVER be selectable.
	crossExplain := res.Explain(resolver.ResolveRequest{
		Snapshot:        fix.mgr.Current(),
		OrgID:           fix.orgID,
		ProjectID:       fix.prjID,
		EnvID:           fix.envID,
		TargetModel:     "cross-org-assistant",
		EffectivePolicy: effEU,
		Breaker:         cb,
	})
	if crossExplain.Rejection != nil || len(crossExplain.SelectedCandidatesOrdered) != 1 ||
		crossExplain.SelectedCandidatesOrdered[0].DeploymentID != "dep_eu_fallback" {
		t.Fatalf("cross-project isolation failed: selected=%+v rejected=%+v rej=%v",
			crossExplain.SelectedCandidatesOrdered, crossExplain.RejectedDeployments, crossExplain.Rejection)
	}
	if len(crossExplain.RejectedDeployments) != 1 || crossExplain.RejectedDeployments[0].RuleCode != "SCOPE_OR_CONNECTION_UNAVAILABLE" {
		t.Fatalf("expected scope isolation rejection diagnostics, got %+v", crossExplain.RejectedDeployments)
	}

	// Also confirm direct LogicalModel routing under org1 never returns dep_other_org or dep_unshared.
	directCands, directRej := res.Resolve(resolver.ResolveRequest{
		Snapshot:        fix.mgr.Current(),
		OrgID:           fix.orgID,
		ProjectID:       fix.prjID,
		EnvID:           fix.envID,
		TargetModel:     "acme/pro",
		EffectivePolicy: effEU,
		Breaker:         cb,
	})
	if directRej != nil {
		t.Fatalf("direct LogicalModel resolution failed: %v", directRej)
	}
	for _, dc := range directCands {
		if dc.Target.Deployment.ID == "dep_other_org" || dc.Target.Deployment.ID == "dep_unshared" {
			t.Fatalf("tenant isolation breach: leaked deployment %s", dc.Target.Deployment.ID)
		}
	}

	// WEIGHTED strategy with deterministic seed produces reproducible permutation bounded by 1+max_failover_hops.
	seed := int64(42)
	w1, w1Rej := res.Resolve(resolver.ResolveRequest{
		Snapshot:        fix.mgr.Current(),
		OrgID:           fix.orgID,
		ProjectID:       fix.prjID,
		EnvID:           fix.envID,
		TargetModel:     "weighted-assistant",
		EffectivePolicy: effEU,
		Breaker:         cb,
		WeightedSeed:    &seed,
	})
	w2, w2Rej := res.Resolve(resolver.ResolveRequest{
		Snapshot:        fix.mgr.Current(),
		OrgID:           fix.orgID,
		ProjectID:       fix.prjID,
		EnvID:           fix.envID,
		TargetModel:     "weighted-assistant",
		EffectivePolicy: effEU,
		Breaker:         cb,
		WeightedSeed:    &seed,
	})
	if w1Rej != nil || w2Rej != nil || len(w1) != 2 || len(w2) != 2 ||
		w1[0].Target.Deployment.ID != w2[0].Target.Deployment.ID {
		t.Fatalf("seeded WEIGHTED strategy should be deterministic: w1=%+v w2=%+v", w1, w2)
	}

	// LOWEST_COST ranks dep_eu_cheap first, then dep_eu_fallback, then dep_eu_expensive.
	costCands, rej := res.Resolve(resolver.ResolveRequest{
		Snapshot:             fix.mgr.Current(),
		OrgID:                fix.orgID,
		ProjectID:            fix.prjID,
		EnvID:                fix.envID,
		TargetModel:          "cost-assistant",
		EffectivePolicy:      effEU,
		EstimatedInputTokens: 10000,
		MaxOutputTokens:      2000,
		Breaker:              cb,
	})
	if rej != nil || len(costCands) != 3 {
		t.Fatalf("LOWEST_COST failed: len=%d rej=%+v", len(costCands), rej)
	}
	if costCands[0].Target.Deployment.ID != "dep_eu_cheap" ||
		costCands[1].Target.Deployment.ID != "dep_eu_fallback" ||
		costCands[2].Target.Deployment.ID != "dep_eu_expensive" {
		t.Fatalf("unexpected LOWEST_COST order: %s, %s, %s",
			costCands[0].Target.Deployment.ID, costCands[1].Target.Deployment.ID, costCands[2].Target.Deployment.ID)
	}

	// WEIGHTED_ROUND_ROBIN (75 / 25) over 4 requests -> 3 primary hits to dep_eu_expensive, 1 to dep_eu_fallback, capped at max_failover_hops=1 (max 2).
	counts := map[string]int{}
	for i := 0; i < 4; i++ {
		wCands, wRej := res.Resolve(resolver.ResolveRequest{
			Snapshot:        fix.mgr.Current(),
			OrgID:           fix.orgID,
			ProjectID:       fix.prjID,
			EnvID:           fix.envID,
			TargetModel:     "wrr-assistant",
			EffectivePolicy: effEU,
			Breaker:         cb,
		})
		if wRej != nil || len(wCands) != 2 {
			t.Fatalf("WRR failed: len=%d rej=%+v", len(wCands), wRej)
		}
		counts[wCands[0].Target.Deployment.ID]++
	}
	if counts["dep_eu_expensive"] != 3 || counts["dep_eu_fallback"] != 1 {
		t.Fatalf("expected 75/25 smooth WRR distribution (3/1), got %+v", counts)
	}

	// Explain MUST be strictly side-effect-free on SWRR counters!
	for i := 0; i < 3; i++ {
		_ = res.Explain(resolver.ResolveRequest{
			Snapshot:        fix.mgr.Current(),
			OrgID:           fix.orgID,
			ProjectID:       fix.prjID,
			EnvID:           fix.envID,
			TargetModel:     "wrr-assistant",
			EffectivePolicy: effEU,
			Breaker:         cb,
		})
	}
	// Next 4 live calls should still produce exact 3/1 split.
	countsAfter := map[string]int{}
	for i := 0; i < 4; i++ {
		wCands, _ := res.Resolve(resolver.ResolveRequest{
			Snapshot:        fix.mgr.Current(),
			OrgID:           fix.orgID,
			ProjectID:       fix.prjID,
			EnvID:           fix.envID,
			TargetModel:     "wrr-assistant",
			EffectivePolicy: effEU,
			Breaker:         cb,
		})
		countsAfter[wCands[0].Target.Deployment.ID]++
	}
	if countsAfter["dep_eu_expensive"] != 3 || countsAfter["dep_eu_fallback"] != 1 {
		t.Fatalf("Explain mutated SWRR state! got %+v", countsAfter)
	}

	// LOWEST_LATENCY_EWMA sorts fastest deployment first.
	cb.RecordOutcome("dep_eu_expensive", breaker.AttemptOutcome{Kind: breaker.OutcomeSuccess, Latency: 250 * time.Millisecond})
	cb.RecordOutcome("dep_eu_fallback", breaker.AttemptOutcome{Kind: breaker.OutcomeSuccess, Latency: 35 * time.Millisecond})
	latCands, latRej := res.Resolve(resolver.ResolveRequest{
		Snapshot:        fix.mgr.Current(),
		OrgID:           fix.orgID,
		ProjectID:       fix.prjID,
		EnvID:           fix.envID,
		TargetModel:     "lat-assistant",
		EffectivePolicy: effEU,
		Breaker:         cb,
	})
	if latRej != nil || len(latCands) != 2 || latCands[0].Target.Deployment.ID != "dep_eu_fallback" {
		t.Fatalf("expected dep_eu_fallback fastest EWMA first, got cands=%+v rej=%+v", latCands, latRej)
	}
}

func BenchmarkResolveRoute(b *testing.B) {
	fix := setupRoutingFixture(b)
	cb := breaker.New(breaker.Config{Now: func() time.Time { return fix.clk.Now() }})
	res := resolver.New()
	snap := fix.mgr.Current()
	effEU := resolveEUPolicy(b, fix)
	req := resolver.ResolveRequest{
		RequestID:            "req_bench",
		Snapshot:             snap,
		OrgID:                fix.orgID,
		ProjectID:            fix.prjID,
		EnvID:                fix.envID,
		TargetModel:          "prod-assistant",
		EffectivePolicy:      effEU,
		RequiredCapabilities: []string{"streaming", "tool_calling", "strict_json_schema"},
		EstimatedInputTokens: 512,
		MaxOutputTokens:      512,
		Breaker:              cb,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cands, rej := res.Resolve(req)
		if rej != nil || len(cands) == 0 {
			b.Fatalf("unexpected resolution failure: %v", rej)
		}
	}
}

func TestRouteAliasAllowlistAndExplicitDenylist(t *testing.T) {
	t.Parallel()
	fix := setupRoutingFixture(t)
	cb := breaker.New(breaker.Config{Now: func() time.Time { return fix.clk.Now() }})
	res := resolver.New()

	// 1. Policy allows ONLY route alias "prod-assistant" (neither "*" nor "acme/pro").
	effAliasOnly := policyengine.MergeLayers(
		policyengine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeProject,
			ScopeID:   fix.prjID,
			Rules: snapshot.PolicyRulesView{
				HasAllowedDataRegions: true,
				AllowedDataRegions:    []string{"EU"},
			},
		},
		policyengine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeVirtualKey,
			ScopeID:   "vk_alias_only",
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels: true,
				AllowedModels:    []string{"prod-assistant"},
			},
		},
	)
	cands, rej := res.Resolve(resolver.ResolveRequest{
		RequestID:       "req_alias_ok",
		Snapshot:        fix.mgr.Current(),
		OrgID:           fix.orgID,
		ProjectID:       fix.prjID,
		EnvID:           fix.envID,
		TargetModel:     "prod-assistant",
		EffectivePolicy: effAliasOnly,
		Breaker:         cb,
	})
	if rej != nil || len(cands) == 0 {
		t.Fatalf("expected non-wildcard route-alias allowlist ['prod-assistant'] to resolve targets, got rej=%+v", rej)
	}

	// 2. Same route alias allowlist PLUS explicit denied_models=["acme/pro"] must still fail closed with 403!
	effAliasWithDeniedTarget := policyengine.MergeLayers(
		policyengine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeOrg,
			ScopeID:   fix.orgID,
			Rules: snapshot.PolicyRulesView{
				DeniedModels: []string{"acme/pro"},
			},
		},
		policyengine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeProject,
			ScopeID:   fix.prjID,
			Rules: snapshot.PolicyRulesView{
				HasAllowedDataRegions: true,
				AllowedDataRegions:    []string{"EU"},
			},
		},
		policyengine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeVirtualKey,
			ScopeID:   "vk_alias_only",
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels: true,
				AllowedModels:    []string{"prod-assistant"},
			},
		},
	)
	candsDenied, rejDenied := res.Resolve(resolver.ResolveRequest{
		RequestID:       "req_alias_denied",
		Snapshot:        fix.mgr.Current(),
		OrgID:           fix.orgID,
		ProjectID:       fix.prjID,
		EnvID:           fix.envID,
		TargetModel:     "prod-assistant",
		EffectivePolicy: effAliasWithDeniedTarget,
		Breaker:         cb,
	})
	if rejDenied == nil || rejDenied.HTTPStatus != 403 || len(candsDenied) != 0 {
		t.Fatalf("expected 403 policy denial when underlying canonical model acme/pro is explicitly denied, got cands=%d rej=%+v", len(candsDenied), rejDenied)
	}
}

func TestRouteAliasCannotBypassAncestorAllowlistOrShadowLogicalModel(t *testing.T) {
	t.Parallel()
	fix := setupRoutingFixture(t)
	cb := breaker.New(breaker.Config{Now: func() time.Time { return fix.clk.Now() }})
	res := resolver.New()

	// Seed a second LogicalModel ("openai/gpt-4o-mini") plus a route policy whose route_slug shadows it
	// while pointing at dep_eu_expensive (canonical model "acme/pro"), and an alias route ("alias-bypass").
	now := fix.clk.Now().UnixMilli()
	if _, err := fix.st.RawDB().ExecContext(context.Background(), `
		INSERT INTO logical_models (id, org_id, canonical_slug, modality_kind, context_window, max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms)
		VALUES ('lmod_mini', NULL, 'openai/gpt-4o-mini', 'CHAT', 128000, 4096, '{"streaming":"VERIFIED"}', 1, ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := fix.st.RawDB().ExecContext(context.Background(), `
		INSERT INTO route_policies (id, org_id, project_id, env_id, route_slug, strategy, max_failover_hops, required_region, version, created_at_ms, updated_at_ms) VALUES
		('rte_shadow', ?, ?, ?, 'openai/gpt-4o-mini', 'PRIORITY_FAILOVER', 1, NULL, 1, ?, ?),
		('rte_alias_bypass', ?, ?, ?, 'alias-bypass', 'PRIORITY_FAILOVER', 1, NULL, 1, ?, ?)
	`, fix.orgID, fix.prjID, fix.envID, now, now, fix.orgID, fix.prjID, fix.envID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := fix.st.RawDB().ExecContext(context.Background(), `
		INSERT INTO route_targets (org_id, route_id, deployment_id, priority_order, weight) VALUES
		(?, 'rte_shadow', 'dep_eu_expensive', 1, 100),
		(?, 'rte_alias_bypass', 'dep_eu_expensive', 1, 100)
	`, fix.orgID, fix.orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := fix.st.RawDB().ExecContext(context.Background(), `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES
		(?, 'logical_model', 'lmod_mini', 'UPSERT', 0, ?),
		(?, 'route_policy', 'rte_shadow', 'UPSERT', 0, ?),
		(?, 'route_policy', 'rte_alias_bypass', 'UPSERT', 0, ?)
	`, fix.orgID, now, fix.orgID, now, fix.orgID, now); err != nil {
		t.Fatal(err)
	}
	if err := fix.mgr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Case 1: ORG_OWNER sets MODEL_ALLOWLIST=["openai/gpt-4o-mini"] (with no denied_models).
	// Neither shadowed route "openai/gpt-4o-mini" nor non-colliding alias "alias-bypass" pointing to "acme/pro" may bypass ORG allowlist.
	effOrgRestricted := policyengine.MergeLayers(
		policyengine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeOrg,
			ScopeID:   fix.orgID,
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels: true,
				AllowedModels:    []string{"openai/gpt-4o-mini", "alias-bypass"},
			},
		},
		policyengine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeProject,
			ScopeID:   fix.prjID,
			Rules: snapshot.PolicyRulesView{
				HasAllowedDataRegions: true,
				AllowedDataRegions:    []string{"EU"},
			},
		},
		policyengine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeVirtualKey,
			ScopeID:   "vk_bypass",
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels: true,
				AllowedModels:    []string{"*"},
			},
		},
	)
	for _, slug := range []string{"openai/gpt-4o-mini", "alias-bypass"} {
		cands, rej := res.Resolve(resolver.ResolveRequest{
			RequestID:       "req_bypass_" + slug,
			Snapshot:        fix.mgr.Current(),
			OrgID:           fix.orgID,
			ProjectID:       fix.prjID,
			EnvID:           fix.envID,
			TargetModel:     slug,
			EffectivePolicy: effOrgRestricted,
			Breaker:         cb,
		})
		if rej == nil || rej.HTTPStatus != 403 || rej.Code != policyengine.DenialCodeModelDenied || len(cands) != 0 {
			t.Fatalf("slug=%q bypassed ORG MODEL_ALLOWLIST: cands=%d rej=%+v", slug, len(cands), rej)
		}
	}

	// Case 2: No ORG MODEL_ALLOWLIST, only VirtualKey allowlist=["openai/gpt-4o-mini"].
	// Shadowing route_slug="openai/gpt-4o-mini" -> canonical "acme/pro" must still be denied 403!
	effVKRestricted := policyengine.MergeLayers(
		policyengine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeProject,
			ScopeID:   fix.prjID,
			Rules: snapshot.PolicyRulesView{
				HasAllowedDataRegions: true,
				AllowedDataRegions:    []string{"EU"},
			},
		},
		policyengine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeVirtualKey,
			ScopeID:   "vk_shadow_only",
			Rules: snapshot.PolicyRulesView{
				HasAllowedModels: true,
				AllowedModels:    []string{"openai/gpt-4o-mini"},
			},
		},
	)
	candsShadow, rejShadow := res.Resolve(resolver.ResolveRequest{
		RequestID:       "req_shadow_vk",
		Snapshot:        fix.mgr.Current(),
		OrgID:           fix.orgID,
		ProjectID:       fix.prjID,
		EnvID:           fix.envID,
		TargetModel:     "openai/gpt-4o-mini",
		EffectivePolicy: effVKRestricted,
		Breaker:         cb,
	})
	if rejShadow == nil || rejShadow.HTTPStatus != 403 || len(candsShadow) != 0 {
		t.Fatalf("expected 403 when route_slug shadows LogicalModel slug to denied model, got cands=%d rej=%+v", len(candsShadow), rejShadow)
	}
}

func TestRouteRequiredRegionHeaderConflictAndDegradedPriorityOrder(t *testing.T) {
	t.Parallel()
	fix := setupRoutingFixture(t)
	cb := breaker.New(breaker.Config{Now: func() time.Time { return fix.clk.Now() }})
	res := resolver.New()

	now := fix.clk.Now().UnixMilli()
	// Route with explicit required_region="EU" covering EU & US targets (evaluated WITHOUT separate DATA_RESIDENCY policy).
	// Also mark con_eu's peer DEGRADED in a mixed-state route at identical priority_order=1 to test ACTIVE > DEGRADED sorting.
	if _, err := fix.st.RawDB().ExecContext(context.Background(), `
		INSERT INTO provider_connections (id, org_id, owner_project_id, adapter_id, name, endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region, billing_mode, auth_mode, state, shared_project_ids_json, config_json, version, created_at_ms, updated_at_ms)
		VALUES ('con_eu_deg', ?, ?, 'adp_1', 'eu-deg', 'https://api.eu.example', 0, '[]', 'EU', 'COMMERCIAL_API', 'API_KEY', 'DEGRADED', '[]', '{}', 1, ?, ?)
	`, fix.orgID, fix.prjID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := fix.st.RawDB().ExecContext(context.Background(), `
		INSERT INTO model_deployments (id, org_id, connection_id, logical_model_id, upstream_model_name, native_dialect, capability_overrides_json, assurance_level, pricing_nano_usd_json, state, connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms, version, created_at_ms, updated_at_ms)
		VALUES ('dep_eu_deg', ?, 'con_eu_deg', 'lmod_1', 'pro-eu-deg', 'openai', '{"strict_json_schema":"VERIFIED"}', 'TESTED', '{"input_per_1m_nano_usd":1000,"output_per_1m_nano_usd":2000}', 'ACTIVE_HEALTHY', 5000, 30000, 30000, 1, ?, ?)
	`, fix.orgID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := fix.st.RawDB().ExecContext(context.Background(), `
		INSERT INTO route_policies (id, org_id, project_id, env_id, route_slug, strategy, max_failover_hops, required_region, version, created_at_ms, updated_at_ms) VALUES
		('rte_req_eu', ?, ?, ?, 'route-req-eu', 'PRIORITY_FAILOVER', 3, 'EU', 1, ?, ?),
		('rte_deg_pri', ?, ?, ?, 'route-deg-pri', 'PRIORITY_FAILOVER', 2, NULL, 1, ?, ?)
	`, fix.orgID, fix.prjID, fix.envID, now, now, fix.orgID, fix.prjID, fix.envID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := fix.st.RawDB().ExecContext(context.Background(), `
		INSERT INTO route_targets (org_id, route_id, deployment_id, priority_order, weight) VALUES
		(?, 'rte_req_eu', 'dep_us_backup', 1, 100),
		(?, 'rte_req_eu', 'dep_eu_expensive', 2, 100),
		(?, 'rte_deg_pri', 'dep_eu_deg', 1, 90),
		(?, 'rte_deg_pri', 'dep_eu_fallback', 1, 10)
	`, fix.orgID, fix.orgID, fix.orgID, fix.orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := fix.st.RawDB().ExecContext(context.Background(), `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES
		(?, 'provider_connection', 'con_eu_deg', 'UPSERT', 0, ?),
		(?, 'model_deployment', 'dep_eu_deg', 'UPSERT', 0, ?),
		(?, 'route_policy', 'rte_req_eu', 'UPSERT', 0, ?),
		(?, 'route_policy', 'rte_deg_pri', 'UPSERT', 0, ?)
	`, fix.orgID, now, fix.orgID, now, fix.orgID, now, fix.orgID, now); err != nil {
		t.Fatal(err)
	}
	if err := fix.mgr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	effOpen := policyengine.MergeLayers(policyengine.ScopedLayer{
		ScopeType: snapshot.PolicyScopeVirtualKey,
		ScopeID:   "vk_open",
		Rules: snapshot.PolicyRulesView{
			HasAllowedModels: true,
			AllowedModels:    []string{"*"},
		},
	})

	// 1. Route required_region="EU" rejects dep_us_backup even with open EffectivePolicy -> only dep_eu_expensive survives.
	candsEU, rejEU := res.Resolve(resolver.ResolveRequest{
		RequestID:       "req_route_eu",
		Snapshot:        fix.mgr.Current(),
		OrgID:           fix.orgID,
		ProjectID:       fix.prjID,
		EnvID:           fix.envID,
		TargetModel:     "route-req-eu",
		EffectivePolicy: effOpen,
		Breaker:         cb,
	})
	if rejEU != nil || len(candsEU) != 1 || candsEU[0].Target.Deployment.ID != "dep_eu_expensive" {
		t.Fatalf("expected only EU target surviving route.required_region=EU, got cands=%+v rej=%+v", candsEU, rejEU)
	}

	// 2. Conflicting X-Tanpopo-Data-Region: US yields empty intersection -> 503 no_compliant_deployment_available!
	candsConflict, rejConflict := res.Resolve(resolver.ResolveRequest{
		RequestID:           "req_route_conflict",
		Snapshot:            fix.mgr.Current(),
		OrgID:               fix.orgID,
		ProjectID:           fix.prjID,
		EnvID:               fix.envID,
		TargetModel:         "route-req-eu",
		RequestedDataRegion: "US",
		EffectivePolicy:     effOpen,
		Breaker:             cb,
	})
	if rejConflict == nil || rejConflict.HTTPStatus != 503 || rejConflict.Code != "no_compliant_deployment_available" || len(candsConflict) != 0 {
		t.Fatalf("expected 503 no_compliant_deployment_available on conflicting route.required_region + X-Tanpopo-Data-Region, got cands=%d rej=%+v", len(candsConflict), rejConflict)
	}

	// 3. Equal priority_order=1 with mixed ACTIVE/DEGRADED connections keeps ACTIVE ahead of DEGRADED across multiple calls.
	for i := 0; i < 4; i++ {
		candsDeg, rejDeg := res.Resolve(resolver.ResolveRequest{
			RequestID:       "req_deg",
			Snapshot:        fix.mgr.Current(),
			OrgID:           fix.orgID,
			ProjectID:       fix.prjID,
			EnvID:           fix.envID,
			TargetModel:     "route-deg-pri",
			EffectivePolicy: effOpen,
			Breaker:         cb,
		})
		if rejDeg != nil || len(candsDeg) != 2 || candsDeg[0].Target.Deployment.ID != "dep_eu_fallback" {
			t.Fatalf("iter %d: expected ACTIVE dep_eu_fallback strictly ahead of DEGRADED dep_eu_deg, got cands=%+v rej=%+v", i, candsDeg, rejDeg)
		}
	}

	// 4. Caller project mismatch against matching env route fails closed (never falls back to ListCandidateTargets).
	candsPrjMismatch, rejPrjMismatch := res.Resolve(resolver.ResolveRequest{
		RequestID:       "req_prj_mismatch",
		Snapshot:        fix.mgr.Current(),
		OrgID:           fix.orgID,
		ProjectID:       "prj_other",
		EnvID:           fix.envID,
		TargetModel:     "route-req-eu",
		EffectivePolicy: effOpen,
		Breaker:         cb,
	})
	if rejPrjMismatch == nil || rejPrjMismatch.HTTPStatus != 503 || rejPrjMismatch.Code != "no_compliant_deployment_available" || len(candsPrjMismatch) != 0 {
		t.Fatalf("expected 503 no_compliant_deployment_available on route project mismatch, got cands=%d rej=%+v", len(candsPrjMismatch), rejPrjMismatch)
	}
	// 5. Empty req.ProjectID against a project-scoped route fails closed (ROUTE_PROJECT_MISMATCH).
	candsPrjEmpty, rejPrjEmpty := res.Resolve(resolver.ResolveRequest{
		RequestID:       "req_prj_empty",
		Snapshot:        fix.mgr.Current(),
		OrgID:           fix.orgID,
		ProjectID:       "",
		EnvID:           fix.envID,
		TargetModel:     "route-req-eu",
		EffectivePolicy: effOpen,
		Breaker:         cb,
	})
	if rejPrjEmpty == nil || rejPrjEmpty.HTTPStatus != 503 || rejPrjEmpty.Code != "no_compliant_deployment_available" || len(candsPrjEmpty) != 0 {
		t.Fatalf("expected 503 no_compliant_deployment_available when req.ProjectID is empty on project route, got cands=%d rej=%+v", len(candsPrjEmpty), rejPrjEmpty)
	}
}

func TestSWRRThreeTargets50_30_20Distribution(t *testing.T) {
	fix := setupRoutingFixture(t)
	ctx := context.Background()
	now := fix.clk.Now().UnixMilli()
	db := fix.st.RawDB()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO provider_connections (id, org_id, owner_project_id, adapter_id, name, endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region, billing_mode, auth_mode, state, shared_project_ids_json, config_json, version, created_at_ms, updated_at_ms)
		VALUES ('con_eu_3', ?, ?, 'adp_1', 'eu-conn-3', 'https://api.eu.example', 0, '[]', 'EU', 'COMMERCIAL_API', 'API_KEY', 'ACTIVE', '[]', '{}', 1, ?, ?)
	`, fix.orgID, fix.prjID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO model_deployments (id, org_id, connection_id, logical_model_id, upstream_model_name, native_dialect, capability_overrides_json, assurance_level, pricing_nano_usd_json, state, connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms, version, created_at_ms, updated_at_ms)
		VALUES ('dep_eu_third', ?, 'con_eu_3', 'lmod_1', 'pro-eu-3', 'openai', '{"strict_json_schema":"VERIFIED"}', 'TESTED', '{"input_per_1m_nano_usd":2000,"output_per_1m_nano_usd":4000}', 'ACTIVE_HEALTHY', 5000, 30000, 30000, 1, ?, ?)
	`, fix.orgID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO route_policies (id, org_id, project_id, env_id, route_slug, strategy, max_failover_hops, required_region, version, created_at_ms, updated_at_ms)
		VALUES ('rte_wrr3', ?, ?, ?, 'wrr3-assistant', 'WEIGHTED_ROUND_ROBIN', 2, NULL, 1, ?, ?)
	`, fix.orgID, fix.prjID, fix.envID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO route_targets (org_id, route_id, deployment_id, priority_order, weight) VALUES
		(?, 'rte_wrr3', 'dep_eu_expensive', 1, 50),
		(?, 'rte_wrr3', 'dep_eu_fallback', 1, 30),
		(?, 'rte_wrr3', 'dep_eu_third', 1, 20)
	`, fix.orgID, fix.orgID, fix.orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES
		(?, 'provider_connection', 'con_eu_3', 'UPSERT', 0, ?),
		(?, 'model_deployment', 'dep_eu_third', 'UPSERT', 0, ?),
		(?, 'route_policy', 'rte_wrr3', 'UPSERT', 0, ?)
	`, fix.orgID, now, fix.orgID, now, fix.orgID, now); err != nil {
		t.Fatal(err)
	}
	if err := fix.mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	res := resolver.New()
	cb := breaker.New(breaker.Config{})
	effOpen := policyengine.MergeLayers(policyengine.ScopedLayer{
		ScopeType: snapshot.PolicyScopeVirtualKey,
		ScopeID:   "vk_open",
		Rules: snapshot.PolicyRulesView{
			HasAllowedModels: true,
			AllowedModels:    []string{"*"},
		},
	})

	counts := map[string]int{}
	for i := 0; i < 100; i++ {
		cands, rej := res.Resolve(resolver.ResolveRequest{
			RequestID:       "req_wrr3",
			Snapshot:        fix.mgr.Current(),
			OrgID:           fix.orgID,
			ProjectID:       fix.prjID,
			EnvID:           fix.envID,
			TargetModel:     "wrr3-assistant",
			EffectivePolicy: effOpen,
			Breaker:         cb,
		})
		if rej != nil || len(cands) != 3 {
			t.Fatalf("iter %d: expected 3 candidates, got len=%d rej=%+v", i, len(cands), rej)
		}
		counts[cands[0].Target.Deployment.ID]++
	}
	if counts["dep_eu_expensive"] != 50 || counts["dep_eu_fallback"] != 30 || counts["dep_eu_third"] != 20 {
		t.Fatalf("expected exact 50/30/20 SWRR distribution across 3 targets, got %v", counts)
	}
}


