// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package snapshot_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/security/trust"
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
	rootPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootFP := trust.PublicKeyFingerprint(rootPub)
	_, err = db.ExecContext(ctx, `INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms) VALUES ('ext_s', '@tanpopo/adp-sec', 'PROVIDER_ADAPTER', 'tanpopo', 'official', ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO extension_versions (id, package_id, semver, contract_version, core_version_range, execution_tier, bundle_sha256, signature_verified, signing_key_fingerprint, source_uri, manifest_json, created_at_ms) VALUES ('exv_s', 'ext_s', '1.0.0', '0.1', '>=0.1.0', 'DECLARATIVE_T1', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 1, ?, 'oci://local', '{}', ?)`, rootFP, now)
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

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{
		DB:      db,
		NowMs:   nowFn,
		Keyring: trust.TrustedKeyring{RootPublicKeys: map[string]ed25519.PublicKey{rootFP: rootPub}},
	})
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

func TestProviderConnectionVisibleToProjectEnforcesTPBR0202OrgOptIn(t *testing.T) {
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

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		Slug:           "snap-tpbr0202",
		DisplayName:    "Snap TP-BR-0202",
		MinPluginTrust: port.MinTrustAnyApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "child-1", DisplayName: "Child 1"})
	if err != nil {
		t.Fatal(err)
	}

	now := nowFn()
	db := st.RawDB()
	if _, err := db.ExecContext(ctx, `INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms) VALUES ('ext_u', '@third/unsigned', 'PROVIDER_ADAPTER', 'third', 'unverified_external', ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO extension_versions (id, package_id, semver, contract_version, core_version_range, execution_tier, bundle_sha256, signature_verified, enterprise_signed, source_uri, manifest_json, created_at_ms) VALUES ('exv_u', 'ext_u', '1.0.0', '0.1', '>=0.1.0', 'DECLARATIVE_T1', 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc', 0, 0, 'oci://local', '{}', ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO extension_installations (id, package_id, active_version_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms) VALUES ('ins_u', 'ext_u', 'exv_u', 'ENABLED_ACTIVE', '{"allowed_hosts":["api.example.com"],"execution_tier":"DECLARATIVE_T1"}', 1, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_adapters (id, installation_id, adapter_slug, supported_dialects_json, allowed_hosts_json, version, created_at_ms) VALUES ('adp_u', 'ins_u', 'unsigned', '["openai_chat"]', '["api.example.com"]', 1, ?)`, now); err != nil {
		t.Fatal(err)
	}
	// Org-level connection with owner_project_id = NULL and empty shared_project_ids_json = '[]'.
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_connections (id, org_id, owner_project_id, adapter_id, name, endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region, billing_mode, auth_mode, state, shared_project_ids_json, config_json, version, created_at_ms, updated_at_ms) VALUES ('con_org_u', ?, NULL, 'adp_u', 'OrgSharedUnsigned', 'https://api.example.com', 0, '[]', 'us', 'COMMERCIAL_API', 'NONE', 'ACTIVE', '[]', '{}', 1, ?, ?)`, org.ID, now, now); err != nil {
		t.Fatal(err)
	}

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: db, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	refresh := func(ts int64) *snapshot.ProviderConnectionView {
		if _, err := db.ExecContext(ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'provider_connection', 'con_org_u', 'UPSERT', 0, ?)`, org.ID, ts); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		v, ok := mgr.Current().LookupProviderConnection(org.ID, "con_org_u")
		if !ok {
			t.Fatal("missing con_org_u in snapshot")
		}
		return v
	}

	// 1. No ORG_OWNER opt-in: org scope (projectID=="") allowed, child project (projectID!="") BLOCKED despite empty shared_project_ids.
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	c1, ok := mgr.Current().LookupProviderConnection(org.ID, "con_org_u")
	if !ok {
		t.Fatal("missing con_org_u")
	}
	if !c1.VisibleToProject(org.ID, "") {
		t.Fatal("expected org-level access (projectID=='') allowed for org-owned connection with empty shared_project_ids")
	}
	if c1.VisibleToProject(org.ID, proj.ID) {
		t.Fatal("security bypass (TP-BR-0202): child project saw unsigned unverified_external org connection without ORG_OWNER opt-in")
	}

	// 2. Policy enabled=1 WITHOUT org_owner_opt_in=1 (e.g., non-ORG_OWNER SECURITY_REVIEWER enablement)
	//    MUST NOT unlock child project visibility, while org-level scope stays operational.
	if _, err := db.ExecContext(ctx, `INSERT INTO org_extension_policies (org_id, package_id, enabled, org_owner_opt_in, version, created_at_ms, updated_at_ms) VALUES (?, 'ext_u', 1, 0, 1, ?, ?)`, org.ID, now+1, now+1); err != nil {
		t.Fatal(err)
	}
	cNonOwner := refresh(now + 1)
	if !cNonOwner.VisibleToProject(org.ID, "") {
		t.Fatal("expected org-level access allowed when org_extension_policies.enabled=1")
	}
	if cNonOwner.VisibleToProject(org.ID, proj.ID) {
		t.Fatal("security bypass (TP-BR-0202): enabled=1 with org_owner_opt_in=0 unlocked child project visibility")
	}

	// 3. Explicit ORG_OWNER opt-in (enabled=1, org_owner_opt_in=1) grants visibility to child projects.
	if _, err := db.ExecContext(ctx, `UPDATE org_extension_policies SET org_owner_opt_in = 1, version = version + 1, updated_at_ms = ? WHERE org_id = ? AND package_id = 'ext_u'`, now+2, org.ID); err != nil {
		t.Fatal(err)
	}
	c2 := refresh(now + 2)
	if !c2.VisibleToProject(org.ID, "") || !c2.VisibleToProject(org.ID, proj.ID) {
		t.Fatal("expected ORG_OWNER opt-in to unlock child project visibility for org-level connection")
	}

	// 4. Explicit org policy disable (enabled = 0, org_owner_opt_in = 0) blocks both org and child project scopes.
	if _, err := db.ExecContext(ctx, `UPDATE org_extension_policies SET enabled = 0, org_owner_opt_in = 0, version = version + 1, updated_at_ms = ? WHERE org_id = ? AND package_id = 'ext_u'`, now+3, org.ID); err != nil {
		t.Fatal(err)
	}
	c3 := refresh(now + 3)
	if c3.VisibleToProject(org.ID, "") || c3.VisibleToProject(org.ID, proj.ID) {
		t.Fatal("expected disabled org policy (enabled=0) to block both org and child project scopes")
	}
}

func TestProviderConnectionVisibleToProjectKeyringDriftWithStaleDBTier(t *testing.T) {
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

	orgAny, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		Slug:           "snap-kr-any",
		DisplayName:    "Snap Keyring ANY_APPROVED",
		MinPluginTrust: port.MinTrustAnyApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	projAny, err := st.Tenancy().CreateProject(ctx, orgAny.ID, port.CreateProjectParams{Slug: "p-any", DisplayName: "P Any"})
	if err != nil {
		t.Fatal(err)
	}

	orgVer, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		Slug:           "snap-kr-ver",
		DisplayName:    "Snap Keyring VERIFIED_ONLY",
		MinPluginTrust: port.MinTrustVerifiedOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	projVer, err := st.Tenancy().CreateProject(ctx, orgVer.ID, port.CreateProjectParams{Slug: "p-ver", DisplayName: "P Ver"})
	if err != nil {
		t.Fatal(err)
	}

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubFP := trust.PublicKeyFingerprint(pubKey)
	now := nowFn()
	db := st.RawDB()

	// Seed package whose DB row still says origin_trust_tier='verified' and version carries publisher signature + fingerprint.
	if _, err := db.ExecContext(ctx, `INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms) VALUES ('ext_kr', '@third/kr-drift', 'PROVIDER_ADAPTER', 'third', 'verified', ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO extension_versions (id, package_id, semver, contract_version, core_version_range, execution_tier, bundle_sha256, signature_verified, signing_key_fingerprint, enterprise_signed, source_uri, manifest_json, created_at_ms) VALUES ('exv_kr', 'ext_kr', '1.0.0', '0.1', '>=0.1.0', 'DECLARATIVE_T1', 'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd', 1, ?, 0, 'oci://local', '{}', ?)`, pubFP, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO extension_installations (id, package_id, active_version_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms) VALUES ('ins_kr', 'ext_kr', 'exv_kr', 'ENABLED_ACTIVE', '{"allowed_hosts":["api.example.com"],"execution_tier":"DECLARATIVE_T1"}', 1, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_adapters (id, installation_id, adapter_slug, supported_dialects_json, allowed_hosts_json, version, created_at_ms) VALUES ('adp_kr', 'ins_kr', 'kr-drift', '["openai_chat"]', '["api.example.com"]', 1, ?)`, now); err != nil {
		t.Fatal(err)
	}
	// Org-level connections with empty shared_project_ids_json = '[]' in both orgs.
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_connections (id, org_id, owner_project_id, adapter_id, name, endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region, billing_mode, auth_mode, state, shared_project_ids_json, config_json, version, created_at_ms, updated_at_ms) VALUES ('con_any', ?, NULL, 'adp_kr', 'OrgAny', 'https://api.example.com', 0, '[]', 'us', 'COMMERCIAL_API', 'NONE', 'ACTIVE', '[]', '{}', 1, ?, ?), ('con_ver', ?, NULL, 'adp_kr', 'OrgVer', 'https://api.example.com', 0, '[]', 'us', 'COMMERCIAL_API', 'NONE', 'ACTIVE', '[]', '{}', 1, ?, ?)`, orgAny.ID, now, now, orgVer.ID, now, now); err != nil {
		t.Fatal(err)
	}
	// Prior non-ORG_OWNER (e.g. SECURITY_REVIEWER) enabled=1 policy created while package was verified (org_owner_opt_in=0).
	if _, err := db.ExecContext(ctx, `INSERT INTO org_extension_policies (org_id, package_id, enabled, org_owner_opt_in, version, created_at_ms, updated_at_ms) VALUES (?, 'ext_kr', 1, 0, 1, ?, ?)`, orgAny.ID, now, now); err != nil {
		t.Fatal(err)
	}

	activeKR := trust.TrustedKeyring{PublisherPublicKeys: map[string]ed25519.PublicKey{pubFP: pubKey}}
	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: db, NowMs: nowFn, Keyring: activeKR})
	if err != nil {
		t.Fatal(err)
	}
	cAny1, _ := mgr.Current().LookupProviderConnection(orgAny.ID, "con_any")
	cVer1, _ := mgr.Current().LookupProviderConnection(orgVer.ID, "con_ver")
	if !cAny1.VisibleToProject(orgAny.ID, "") || !cAny1.VisibleToProject(orgAny.ID, projAny.ID) {
		t.Fatalf("expected verified keyring to allow org and child project in ANY_APPROVED, got conn=%+v", cAny1)
	}
	if !cVer1.VisibleToProject(orgVer.ID, "") || !cVer1.VisibleToProject(orgVer.ID, projVer.ID) {
		t.Fatalf("expected verified keyring to allow org and child project in VERIFIED_ONLY, got conn=%+v", cVer1)
	}

	// Withdraw publisher key from keyring WITHOUT modifying DB extension_packages.origin_trust_tier or org_extension_policies!
	withdrawnKR := trust.TrustedKeyring{}
	mgr.SetKeyring(withdrawnKR)
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	freshMgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: db, NowMs: nowFn, Keyring: withdrawnKR})
	if err != nil {
		t.Fatal(err)
	}

	for name, snap := range map[string]*snapshot.ConfigSnapshot{
		"rotated_live_mgr": mgr.Current(),
		"fresh_withdrawn":  freshMgr.Current(),
	} {
		cAny, _ := snap.LookupProviderConnection(orgAny.ID, "con_any")
		if cAny.DerivedPublicTrust != trust.TierUnverifiedExternal {
			t.Fatalf("[%s] expected derived trust unverified_external after key withdrawal, got %q", name, cAny.DerivedPublicTrust)
		}
		if !cAny.VisibleToProject(orgAny.ID, "") {
			t.Fatalf("[%s] expected org-level flow still allowed under ANY_APPROVED", name)
		}
		if cAny.VisibleToProject(orgAny.ID, projAny.ID) {
			t.Fatalf("[%s] security bypass: child project allowed via stale DB 'verified' or SECURITY_REVIEWER enabled=1", name)
		}
		cVer, _ := snap.LookupProviderConnection(orgVer.ID, "con_ver")
		if cVer.VisibleToProject(orgVer.ID, "") || cVer.VisibleToProject(orgVer.ID, projVer.ID) {
			t.Fatalf("[%s] expected VERIFIED_ONLY org to block both org and child project after key withdrawal", name)
		}
	}

	// Explicit ORG_OWNER opt-in (enabled=1, org_owner_opt_in=1) restores child project visibility under ANY_APPROVED.
	if _, err := db.ExecContext(ctx, `UPDATE org_extension_policies SET org_owner_opt_in = 1, version = version + 1, updated_at_ms = ? WHERE org_id = ? AND package_id = 'ext_kr'`, now+10, orgAny.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'org_extension_policy', ?, 'UPSERT', 0, ?)`, orgAny.ID, orgAny.ID+":ext_kr", now+10); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	cAnyAfter, _ := mgr.Current().LookupProviderConnection(orgAny.ID, "con_any")
	if !cAnyAfter.VisibleToProject(orgAny.ID, "") || !cAnyAfter.VisibleToProject(orgAny.ID, projAny.ID) {
		t.Fatal("expected ORG_OWNER opt-in to restore child project visibility under ANY_APPROVED")
	}

	// QUARANTINED and INSTALLED_DISABLED installation states block VisibleToProject after Refresh.
	for _, badState := range []string{"QUARANTINED", "INSTALLED_DISABLED"} {
		if _, err := db.ExecContext(ctx, `UPDATE extension_installations SET state = ?, version = version + 1, updated_at_ms = ? WHERE id = 'ins_kr'`, badState, now+11); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'extension_installation', 'ins_kr', 'QUARANTINE', 1, ?)`, orgAny.ID, now+11); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		cBad, _ := mgr.Current().LookupProviderConnection(orgAny.ID, "con_any")
		if cBad.VisibleToProject(orgAny.ID, "") || cBad.VisibleToProject(orgAny.ID, projAny.ID) {
			t.Fatalf("expected extension state %s to block VisibleToProject at both org and child scopes", badState)
		}
	}

	// Enterprise key withdrawal revokes VERIFIED_OR_ENTERPRISE_SIGNED and child access even while DB enterprise_signed=1.
	entPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	entFP := trust.PublicKeyFingerprint(entPub)
	if _, err := db.ExecContext(ctx, `UPDATE extension_installations SET state = 'ENABLED_ACTIVE', version = version + 1, updated_at_ms = ? WHERE id = 'ins_kr'`, now+12); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE extension_versions SET signature_verified = 0, enterprise_signed = 1, signing_key_fingerprint = ?, enterprise_key_fingerprint = ? WHERE id = 'exv_kr'`, entFP, entFP); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM org_extension_policies WHERE org_id = ? AND package_id = 'ext_kr'`, orgAny.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE organizations SET min_plugin_trust = 'VERIFIED_OR_ENTERPRISE_SIGNED', version = version + 1, updated_at_ms = ? WHERE id = ?`, now+12, orgAny.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'organization', ?, 'UPSERT', 0, ?)`, orgAny.ID, orgAny.ID, now+12); err != nil {
		t.Fatal(err)
	}
	mgr.SetKeyring(trust.TrustedKeyring{EnterprisePublicKeys: map[string]ed25519.PublicKey{"ent": entPub}})
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	cEntOK, _ := mgr.Current().LookupProviderConnection(orgAny.ID, "con_any")
	if !cEntOK.VisibleToProject(orgAny.ID, "") || !cEntOK.VisibleToProject(orgAny.ID, projAny.ID) {
		t.Fatal("expected active enterprise key to permit VERIFIED_OR_ENTERPRISE_SIGNED across org and child scopes")
	}
	mgr.SetKeyring(trust.TrustedKeyring{})
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	cEntRevoked, _ := mgr.Current().LookupProviderConnection(orgAny.ID, "con_any")
	if cEntRevoked.VisibleToProject(orgAny.ID, "") || cEntRevoked.VisibleToProject(orgAny.ID, projAny.ID) {
		t.Fatal("expected withdrawn enterprise key to fail closed even while DB enterprise_signed=1")
	}
}
