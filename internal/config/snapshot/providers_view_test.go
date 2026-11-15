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

func TestProviderCatalogSnapshotO1AndLocalKeyFencePreservation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000000000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "tanpopo.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "snap-prov", DisplayName: "Snap Prov"})
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "snap-other", DisplayName: "Snap Other"})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "p1", DisplayName: "P1"})
	if err != nil {
		t.Fatal(err)
	}

	now := nowFn()
	db := st.RawDB()
	_, err = db.ExecContext(ctx, `INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms) VALUES ('ext_1', '@tanpopo/adp-snap', 'PROVIDER_ADAPTER', 'tanpopo', 'official', ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO extension_versions (id, package_id, semver, contract_version, core_version_range, execution_tier, bundle_sha256, signature_verified, source_uri, manifest_json, created_at_ms) VALUES ('exv_1', 'ext_1', '1.0.0', '0.1', '>=0.1.0', 'DECLARATIVE_T1', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 1, 'oci://local', '{}', ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO extension_installations (id, package_id, active_version_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms) VALUES ('ins_1', 'ext_1', 'exv_1', 'ENABLED_ACTIVE', '{}', 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO provider_adapters (id, installation_id, adapter_slug, supported_dialects_json, allowed_hosts_json, version, created_at_ms) VALUES ('adp_1', 'ins_1', 'ollama', '["openai_chat"]', '["127.0.0.1"]', 1, ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO provider_connections (id, org_id, owner_project_id, adapter_id, name, endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region, billing_mode, auth_mode, state, shared_project_ids_json, config_json, version, created_at_ms, updated_at_ms) VALUES ('con_1', ?, ?, 'adp_1', 'Local', 'http://127.0.0.1:11434', 1, '["127.0.0.1/32"]', 'on-prem', 'SELF_HOSTED_INFRA', 'NONE', 'ACTIVE', '[]', '{}', 1, ?, ?)`, org.ID, proj.ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO logical_models (id, org_id, canonical_slug, modality_kind, context_window, max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms) VALUES ('lmd_global', NULL, 'qwen-2.5', 'CHAT', 32768, 4096, '{"streaming":"VERIFIED"}', 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO logical_models (id, org_id, canonical_slug, modality_kind, context_window, max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms) VALUES ('lmd_org', ?, 'qwen-2.5', 'CHAT', 65536, 8192, '{"streaming":"VERIFIED"}', 1, ?, ?)`, org.ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO model_deployments (id, org_id, connection_id, logical_model_id, upstream_model_name, assurance_level, pricing_nano_usd_json, state, version, created_at_ms, updated_at_ms) VALUES ('dpl_1', ?, 'con_1', 'lmd_org', 'qwen2.5:7b', 'TESTED', '{"input_per_1m_nano_usd":0,"output_per_1m_nano_usd":0}', 'ACTIVE_HEALTHY', 1, ?, ?)`, org.ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'model_deployment', 'dpl_1', 'UPSERT', 0, ?)`, org.ID, now)
	if err != nil {
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
	if _, ok := snap.LookupProviderAdapter("adp_1"); !ok {
		t.Fatal("expected adp_1 in snapshot")
	}
	if _, ok := snap.LookupProviderConnection(orgB.ID, "con_1"); ok {
		t.Fatal("cross-org LookupProviderConnection must fail closed")
	}
	// Org-specific logical model overrides global logical model by slug!
	lmOrg, ok := snap.LookupLogicalModelBySlug(org.ID, "qwen-2.5")
	if !ok || lmOrg.ID != "lmd_org" {
		t.Fatalf("expected org-scoped logical model override, got ok=%v lm=%+v", ok, lmOrg)
	}
	lmGlobal, ok := snap.LookupLogicalModelBySlug(orgB.ID, "qwen-2.5")
	if !ok || lmGlobal.ID != "lmd_global" {
		t.Fatalf("expected global fallback for orgB, got ok=%v lm=%+v", ok, lmGlobal)
	}

	// Applying a local key revocation fence MUST preserve the loaded ProviderCatalogView.
	mgr.FenceKeyRevocation("vky_nonexistent", "HARD", now, snap.Epoch+1)
	fencedSnap := mgr.Current()
	if depls := fencedSnap.LookupDeploymentsForSlug(org.ID, "qwen-2.5"); len(depls) != 1 || depls[0].ID != "dpl_1" {
		t.Fatalf("local key fence dropped ProviderCatalogView: %+v", depls)
	}
}

func TestProviderCatalogSnapshotSecurityAndFailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000000000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "tanpopo.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "snap-a", DisplayName: "Snap A"})
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "snap-b", DisplayName: "Snap B"})
	if err != nil {
		t.Fatal(err)
	}
	projA1, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "pa1", DisplayName: "PA1"})
	if err != nil {
		t.Fatal(err)
	}
	projA2, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "pa2", DisplayName: "PA2"})
	if err != nil {
		t.Fatal(err)
	}

	now := nowFn()
	db := st.RawDB()
	_, err = db.ExecContext(ctx, `INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms) VALUES ('ext_s', '@tanpopo/adp-sec', 'PROVIDER_ADAPTER', 'tanpopo', 'official', ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO extension_versions (id, package_id, semver, contract_version, core_version_range, execution_tier, bundle_sha256, signature_verified, source_uri, manifest_json, created_at_ms) VALUES ('exv_s', 'ext_s', '1.0.0', '0.1', '>=0.1.0', 'DECLARATIVE_T1', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 1, 'oci://local', '{}', ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO extension_installations (id, package_id, active_version_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms) VALUES ('ins_s', 'ext_s', 'exv_s', 'ENABLED_ACTIVE', '{}', 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO provider_adapters (id, installation_id, adapter_slug, supported_dialects_json, allowed_hosts_json, version, created_at_ms) VALUES ('adp_s', 'ins_s', 'openai', '["openai_chat"]', '["api.example.com"]', 1, ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO provider_connections (id, org_id, owner_project_id, adapter_id, name, endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region, billing_mode, auth_mode, state, shared_project_ids_json, config_json, version, created_at_ms, updated_at_ms) VALUES ('con_s', ?, NULL, 'adp_s', 'SharedOnly', 'https://api.example.com', 0, '["10.0.0.0/8"]', 'us', 'COMMERCIAL_API', 'NONE', 'ACTIVE', ?, '{}', 1, ?, ?)`, orgA.ID, `["`+projA2.ID+`"]`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO logical_models (id, org_id, canonical_slug, modality_kind, context_window, max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms) VALUES ('lmd_glob', NULL, 'global-m', 'CHAT', 8192, 2048, '{"streaming":"VERIFIED"}', 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO logical_models (id, org_id, canonical_slug, modality_kind, context_window, max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms) VALUES ('lmd_privA', ?, 'priv-a', 'CHAT', 8192, 2048, '{"streaming":"VERIFIED"}', 1, ?, ?)`, orgA.ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'logical_model', 'lmd_privA', 'UPSERT', 0, ?)`, orgA.ID, now)
	if err != nil {
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

	// 1. LookupLogicalModelByID enforces strict org scoping + allows global fallback.
	if _, ok := snap.LookupLogicalModelByID(orgB.ID, "lmd_privA"); ok {
		t.Fatal("cross-org LookupLogicalModelByID must return false")
	}
	if lm, ok := snap.LookupLogicalModelByID(orgA.ID, "lmd_privA"); !ok || lm.ID != "lmd_privA" {
		t.Fatalf("owner LookupLogicalModelByID failed: ok=%v lm=%+v", ok, lm)
	}
	if lm, ok := snap.LookupLogicalModelByID(orgB.ID, "lmd_glob"); !ok || lm.ID != "lmd_glob" {
		t.Fatalf("global fallback LookupLogicalModelByID failed: ok=%v lm=%+v", ok, lm)
	}

	// 2. Empty projectID denied when connection has SharedProjectIDs; allowed only for projA2.
	connView, ok := snap.LookupProviderConnection(orgA.ID, "con_s")
	if !ok {
		t.Fatal("missing con_s")
	}
	if connView.VisibleToProject(orgA.ID, "") {
		t.Fatal("blank projectID must be denied when SharedProjectIDs is restricted")
	}
	if connView.VisibleToProject(orgA.ID, projA1.ID) {
		t.Fatal("unshared projA1 must be denied")
	}
	if !connView.VisibleToProject(orgA.ID, projA2.ID) {
		t.Fatal("shared projA2 must be allowed")
	}

	// 3. Returned Lookup views and their pointers are cloned (caller mutations do not corrupt cached snapshot).
	connView.AllowedCIDRs[0] = "0.0.0.0/0"
	freshConn, _ := snap.LookupProviderConnection(orgA.ID, "con_s")
	if freshConn.AllowedCIDRs[0] != "10.0.0.0/8" {
		t.Fatalf("snapshot AllowedCIDRs mutated by caller: %+v", freshConn.AllowedCIDRs)
	}
	lmPriv, ok := snap.LookupLogicalModelByID(orgA.ID, "lmd_privA")
	if !ok || lmPriv.OrgID == nil {
		t.Fatal("expected lmPriv with OrgID")
	}
	*lmPriv.OrgID = "mutated_org"
	lmPrivFresh, _ := snap.LookupLogicalModelByID(orgA.ID, "lmd_privA")
	if *lmPrivFresh.OrgID != orgA.ID {
		t.Fatalf("snapshot LogicalModel OrgID mutated by caller: %q", *lmPrivFresh.OrgID)
	}

	// 4. Corrupt DB JSON in shared_project_ids_json = 'null' fails closed during Refresh.
	if _, err := db.ExecContext(ctx, `UPDATE provider_connections SET shared_project_ids_json = 'null' WHERE id = 'con_s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'provider_connection', 'con_s', 'UPSERT', 0, ?)`, orgA.ID, now+1); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err == nil {
		t.Fatal("expected Refresh to fail closed on shared_project_ids_json = 'null'")
	}
	if _, err := db.ExecContext(ctx, `UPDATE provider_connections SET shared_project_ids_json = '[]' WHERE id = 'con_s'`); err != nil {
		t.Fatal(err)
	}

	// 5. Corrupt DB JSON in model_deployments.pricing_nano_usd_json = 'null' fails closed during Refresh.
	if _, err := db.ExecContext(ctx, `INSERT INTO model_deployments (id, org_id, connection_id, logical_model_id, upstream_model_name, assurance_level, pricing_nano_usd_json, state, version, created_at_ms, updated_at_ms) VALUES ('dpl_badp', ?, 'con_s', 'lmd_privA', 'm', 'DECLARED', 'null', 'ACTIVE_HEALTHY', 1, ?, ?)`, orgA.ID, now+2, now+2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'model_deployment', 'dpl_badp', 'UPSERT', 0, ?)`, orgA.ID, now+2); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err == nil {
		t.Fatal("expected Refresh to fail closed on pricing_nano_usd_json = 'null'")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM model_deployments WHERE id = 'dpl_badp'`); err != nil {
		t.Fatal(err)
	}

	// 6. Corrupt DB JSON in provider_adapters.allowed_hosts_json fails closed during Refresh.
	if _, err := db.ExecContext(ctx, `UPDATE provider_adapters SET allowed_hosts_json = '{bad-json' WHERE id = 'adp_s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'provider_adapter', 'adp_s', 'UPSERT', 0, ?)`, orgA.ID, now+3); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err == nil {
		t.Fatal("expected Refresh to fail closed on corrupt allowed_hosts_json")
	}
}
