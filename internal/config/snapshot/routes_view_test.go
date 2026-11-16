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

func TestConfigSnapshotRoutesLoadAndTenantIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000100000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "routes.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "r1", DisplayName: "Org R1"})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "proj", DisplayName: "Proj R1"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}

	now := nowFn()
	db := st.RawDB()
	execStep := func(name, query string, args ...any) {
		t.Helper()
		if _, e := db.ExecContext(ctx, query, args...); e != nil {
			t.Fatalf("%s: %v", name, e)
		}
	}
	execStep("pkg", `INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms) VALUES ('exp_r1', '@acme/r-adapter', 'PROVIDER_ADAPTER', 'acme', 'verified', ?)`, now)
	execStep("ver", `INSERT INTO extension_versions (id, package_id, semver, contract_version, core_version_range, execution_tier, bundle_sha256, signature_verified, signing_key_fingerprint, source_uri, manifest_json, created_at_ms) VALUES ('exv_r1', 'exp_r1', '1.0.0', '0.1', '>=0.1.0', 'WASM_SANDBOX_T2', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 1, 'fp_1', 'file://acme.tpkg', '{"kind":"ExtensionManifest"}', ?)`, now)
	execStep("ins", `INSERT INTO extension_installations (id, package_id, active_version_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms) VALUES ('ins_r1', 'exp_r1', 'exv_r1', 'ENABLED_ACTIVE', '{"allowed_hosts":["127.0.0.1:8080"]}', 1, ?, ?)`, now, now)
	execStep("adp", `INSERT INTO provider_adapters (id, installation_id, adapter_slug, native_prefix, supported_dialects_json, allowed_hosts_json, version, created_at_ms) VALUES ('adp_r1', 'ins_r1', 'acme', '/native/acme', '["openai"]', '["127.0.0.1:8080"]', 1, ?)`, now)
	execStep("con", `INSERT INTO provider_connections (id, org_id, owner_project_id, adapter_id, name, endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region, billing_mode, auth_mode, state, shared_project_ids_json, config_json, version, created_at_ms, updated_at_ms) VALUES ('con_r1', ?, ?, 'adp_r1', 'eu-conn', 'http://127.0.0.1:8080', 1, '["127.0.0.1/32"]', 'EU', 'COMMERCIAL_API', 'API_KEY', 'ACTIVE', '[]', '{}', 1, ?, ?)`, org.ID, proj.ID, now, now)
	execStep("lmod", `INSERT INTO logical_models (id, org_id, canonical_slug, modality_kind, context_window, max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms) VALUES ('lmod_r1', NULL, 'acme/chat-1', 'CHAT', 128000, 4096, '{"streaming":"VERIFIED"}', 1, ?, ?)`, now, now)
	execStep("depls", `INSERT INTO model_deployments (id, org_id, connection_id, logical_model_id, upstream_model_name, native_dialect, capability_overrides_json, assurance_level, pricing_nano_usd_json, state, connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms, version, created_at_ms, updated_at_ms) VALUES ('dep_r1_a', ?, 'con_r1', 'lmod_r1', 'chat-a', 'openai', '{}', 'TESTED', '{"input_per_1m_nano_usd":100,"output_per_1m_nano_usd":200}', 'ACTIVE_HEALTHY', 5000, 30000, 30000, 1, ?, ?), ('dep_r1_b', ?, 'con_r1', 'lmod_r1', 'chat-b', 'openai', '{}', 'TESTED', '{"input_per_1m_nano_usd":50,"output_per_1m_nano_usd":100}', 'ACTIVE_HEALTHY', 5000, 30000, 30000, 1, ?, ?)`, org.ID, now, now, org.ID, now, now)
	execStep("rp", `INSERT INTO route_policies (id, org_id, project_id, env_id, route_slug, strategy, max_failover_hops, required_region, version, created_at_ms, updated_at_ms) VALUES ('rte_r1', ?, ?, ?, 'prod-assistant', 'PRIORITY_FAILOVER', 2, 'eu', 1, ?, ?)`, org.ID, proj.ID, env.ID, now, now)
	execStep("rt", `INSERT INTO route_targets (org_id, route_id, deployment_id, priority_order, weight) VALUES (?, 'rte_r1', 'dep_r1_b', 2, 20), (?, 'rte_r1', 'dep_r1_a', 1, 80)`, org.ID, org.ID)

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: db, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	snap := mgr.Current()
	if err := snap.RoutesHealthy(); err != nil {
		t.Fatalf("RoutesHealthy error: %v", err)
	}
	rp, ok, err := snap.LookupRoutePolicy(org.ID, env.ID, "prod-assistant")
	if err != nil || !ok || rp == nil {
		t.Fatalf("LookupRoutePolicy failed: ok=%v err=%v", ok, err)
	}
	if rp.Strategy != snapshot.RouteStrategyPriorityFailover || rp.MaxFailoverHops != 2 || rp.RequiredRegion == nil || *rp.RequiredRegion != "EU" {
		t.Fatalf("unexpected route policy: %+v", rp)
	}
	if len(rp.Targets) != 2 || rp.Targets[0].DeploymentID != "dep_r1_a" || rp.Targets[1].DeploymentID != "dep_r1_b" {
		t.Fatalf("expected targets sorted by priority_order ASC: %+v", rp.Targets)
	}
	// Mutating clone must not leak into snapshot.
	rp.Targets[0].Weight = 999
	rp2, _, _ := snap.LookupRoutePolicy(org.ID, env.ID, "prod-assistant")
	if rp2.Targets[0].Weight != 80 {
		t.Fatalf("expected defensive clone on route targets, got weight=%d", rp2.Targets[0].Weight)
	}
	// Cross-org isolation: LookupRoutePolicy and LookupResolvedTarget must deny wrong org.
	if _, ok, _ := snap.LookupRoutePolicy("org_other", env.ID, "prod-assistant"); ok {
		t.Fatal("expected cross-org route lookup miss")
	}
	if _, ok := snap.LookupResolvedTarget("org_other", "dep_r1_a"); ok {
		t.Fatal("expected cross-org resolved target miss")
	}
	tgt, ok := snap.LookupResolvedTarget(org.ID, "dep_r1_a")
	if !ok || tgt == nil || tgt.Deployment.ID != "dep_r1_a" {
		t.Fatalf("expected LookupResolvedTarget success, got ok=%v tgt=%+v", ok, tgt)
	}
}
