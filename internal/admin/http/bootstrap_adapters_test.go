// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/foureyes"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
)

func registerCustomSignedProviderTPKG(
	t *testing.T,
	h *part2Harness,
	sourceURI, pkgID, ver, adapterSlug string,
	allowedHosts []string,
	allowPrivIP, requireTLS bool,
) string {
	t.Helper()
	adapterYAML := []byte("id: " + pkgID + "\nslug: " + adapterSlug + "\n")
	adapterSum := sha256.Sum256(adapterYAML)
	adapterSHA := hex.EncodeToString(adapterSum[:])

	var hostsYAML strings.Builder
	for _, host := range allowedHosts {
		fmt.Fprintf(&hostsYAML, "      - %q\n", host)
	}
	manifest := []byte(fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: %q
  name: %q
  version: %q
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"
runtime:
  tier: "declarative"
  entrypoint: "adapter.yaml"
  sha256: %q
capabilities:
  - type: "provider_adapter"
    adapterSlug: %q
    supportedDialects: ["openai_compatible"]
    authMethods:
      - id: "api_key_header"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
permissions:
  networkEgress:
    allowedHosts:
%s    allowPrivateIp: %t
    requireTls: %t
  hooks:
    - "provider:transform_request"
`, pkgID, "Adapter "+adapterSlug, ver, adapterSHA, adapterSlug, hostsYAML.String(), allowPrivIP, requireTLS))

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, ent := range []struct {
		name string
		body []byte
	}{
		{"manifest.yaml", manifest},
		{"adapter.yaml", adapterYAML},
	} {
		hdr := &tar.Header{
			Name:     ent.name,
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(ent.body)),
			ModTime:  time.Unix(0, 0).UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader %s: %v", ent.name, err)
		}
		if _, err := tw.Write(ent.body); err != nil {
			t.Fatalf("Write %s: %v", ent.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}

	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	shaHex := hex.EncodeToString(sum[:])
	sig := ed25519.Sign(h.rootPriv, installer.SignatureSigningPayload(pkgID, ver, shaHex))

	h.archivesMu.Lock()
	h.archives[sourceURI] = signedTestArchive{raw: raw, sig: sig}
	h.archivesMu.Unlock()
	return shaHex
}

func seedAdapterInstallationState(
	t *testing.T,
	h *part2Harness,
	pkgSuffix, slug, instState string,
	allowedHosts []string,
) (adapterID, instID string) {
	t.Helper()
	ctx := context.Background()
	now := h.fk.Now().UTC().UnixMilli()
	pkgID := idgen.MustNew(idgen.PrefixExtension)
	exvID := idgen.MustNew(idgen.PrefixExtensionVersion)
	instID = idgen.MustNew(idgen.PrefixInstallation)
	adapterID = idgen.MustNew(idgen.PrefixAdapter)

	_, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_packages (
			id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms
		) VALUES (?, ?, 'PROVIDER_ADAPTER', 'tanpopo', 'official', ?)`,
		pkgID, "tanpopo."+pkgSuffix, now,
	)
	if err != nil {
		t.Fatalf("insert extension_packages: %v", err)
	}
	_, err = h.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_versions (
			id, package_id, semver, contract_version, core_version_range, execution_tier,
			bundle_sha256, signature_verified, enterprise_signed, source_uri, manifest_json, created_at_ms
		) VALUES (?, ?, '1.0.0', '0.1', '>=0.1.0 <0.3.0', 'DECLARATIVE_T1', ?, 1, 0, ?, '{}', ?)`,
		exvID, pkgID, strings.Repeat("c", 64), "builtin://tanpopo."+pkgSuffix, now,
	)
	if err != nil {
		t.Fatalf("insert extension_versions: %v", err)
	}
	permsJSON, _ := json.Marshal(map[string]any{
		"allowed_hosts":    allowedHosts,
		"allow_private_ip": false,
		"require_tls":      true,
	})
	_, err = h.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_installations (
			id, package_id, active_version_id, state, approved_permissions_json,
			approved_by_principal_id, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, NULL, 1, ?, ?)`,
		instID, pkgID, exvID, instState, string(permsJSON), now, now,
	)
	if err != nil {
		t.Fatalf("insert extension_installations: %v", err)
	}
	hostsJSON, _ := json.Marshal(allowedHosts)
	_, err = h.st.RawDB().ExecContext(ctx, `
		INSERT INTO provider_adapters (
			id, installation_id, adapter_slug, supported_dialects_json, allowed_hosts_json, version, created_at_ms
		) VALUES (?, ?, ?, '["openai_compatible"]', ?, 1, ?)`,
		adapterID, instID, slug, string(hostsJSON), now,
	)
	if err != nil {
		t.Fatalf("insert provider_adapters: %v", err)
	}
	return adapterID, instID
}

func TestBootstrapStatusAndCompleteAdapterValidation(t *testing.T) {
	t.Parallel()
	h := newPart2TestHarness(t, 20)

	// 1. Clean database: initialized=false, marketplace count populated, available_adapters=[]
	recEmpty := h.doPublic(t, http.MethodGet, "/admin/v1/bootstrap/status", nil)
	if recEmpty.Code != http.StatusOK {
		t.Fatalf("GET /bootstrap/status code=%d body=%s", recEmpty.Code, recEmpty.Body.String())
	}
	var stEmpty bootstrapStatusResponse
	mustDecodeJSON(t, recEmpty.Body.Bytes(), &stEmpty)
	if stEmpty.Initialized {
		t.Fatal("expected initialized=false on clean DB")
	}
	if stEmpty.MarketplaceCatalogCount != 1 {
		t.Fatalf("expected marketplace_catalog_count=1, got %d", stEmpty.MarketplaceCatalogCount)
	}
	if len(stEmpty.AvailableAdapters) != 0 {
		t.Fatalf("expected empty available_adapters on clean DB, got %+v", stEmpty.AvailableAdapters)
	}

	// 2. Seed ACTIVE, INSTALLED_DISABLED, QUARANTINED, UNINSTALLED adapters
	activeAdpID, _ := seedAdapterInstallationState(t, h, "active_openai", "active-openai", "ENABLED_ACTIVE", []string{"api.openai.com"})
	disabledAdpID, _ := seedAdapterInstallationState(t, h, "disabled_openai", "disabled-openai", "INSTALLED_DISABLED", []string{"api.openai.com"})
	quarantinedAdpID, _ := seedAdapterInstallationState(t, h, "quarantined_openai", "quarantined-openai", "QUARANTINED", []string{"api.openai.com"})
	uninstalledAdpID, _ := seedAdapterInstallationState(t, h, "uninstalled_openai", "uninstalled-openai", "UNINSTALLED", []string{"api.openai.com"})

	recPop := h.doPublic(t, http.MethodGet, "/admin/v1/bootstrap/status", nil)
	if recPop.Code != http.StatusOK {
		t.Fatalf("GET /bootstrap/status (seeded) code=%d body=%s", recPop.Code, recPop.Body.String())
	}
	var stPop bootstrapStatusResponse
	mustDecodeJSON(t, recPop.Body.Bytes(), &stPop)
	if len(stPop.AvailableAdapters) != 1 || stPop.AvailableAdapters[0].ID != activeAdpID {
		t.Fatalf("expected only active adapter %s in available_adapters, got %+v", activeAdpID, stPop.AvailableAdapters)
	}

	tok := h.readBootstrapToken(t)

	// 3. Inactive / nonexistent adapter IDs reject without burning bootstrap token
	for _, badAdp := range []string{disabledAdpID, quarantinedAdpID, uninstalledAdpID, "adp_nonexistent"} {
		recBad := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
			"bootstrap_token":                tok,
			"owner_email":                    "owner@example.test",
			"owner_password":                 "StrongPassBootstrap#2026!",
			"org_name":                       "Primary Org",
			"project_name":                   "Default Project",
			"master_kek_backup_acknowledged": true,
			"initial_connection": map[string]any{
				"adapter_id":       badAdp,
				"name":             "Invalid Conn",
				"billing_mode":     "COMMERCIAL_API",
				"endpoint_url":     "https://api.openai.com/v1",
				"allow_private_ip": false,
				"secret_value":     canary.DefaultCanarySecret(),
				"model_id":         "gpt-4o",
				"data_region":      "US",
			},
		})
		if recBad.Code != http.StatusUnprocessableEntity || !strings.Contains(recBad.Body.String(), "unknown_provider_adapter") {
			t.Fatalf("adapter %s expected 422 unknown_provider_adapter, got %d body=%s", badAdp, recBad.Code, recBad.Body.String())
		}
		if initNow, _ := h.bootMgr.IsBootstrapped(context.Background()); initNow {
			t.Fatalf("bootstrap token was consumed despite bad adapter %s", badAdp)
		}
	}

	// 4. Disallowed endpoint host under active adapter manifest rejects with egress_policy_denied without burning token
	recBadHost := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                tok,
		"owner_email":                    "owner@example.test",
		"owner_password":                 "StrongPassBootstrap#2026!",
		"org_name":                       "Primary Org",
		"project_name":                   "Default Project",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":       activeAdpID,
			"name":             "Disallowed Host Conn",
			"billing_mode":     "COMMERCIAL_API",
			"endpoint_url":     "https://evil.example.test/v1",
			"allow_private_ip": false,
			"secret_value":     canary.DefaultCanarySecret(),
			"model_id":         "gpt-4o",
			"data_region":      "US",
		},
	})
	if recBadHost.Code != http.StatusUnprocessableEntity || !strings.Contains(recBadHost.Body.String(), "egress_policy_denied") {
		t.Fatalf("expected 422 egress_policy_denied, got %d body=%s", recBadHost.Code, recBadHost.Body.String())
	}
	if initNow, _ := h.bootMgr.IsBootstrapped(context.Background()); initNow {
		t.Fatal("bootstrap token was consumed despite disallowed endpoint host")
	}

	// 5. Valid active adapter + allowed host succeeds and clears pre-login available_adapters exposure
	recOK := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                tok,
		"owner_email":                    "owner@example.test",
		"owner_password":                 "StrongPassBootstrap#2026!",
		"org_name":                       "Primary Org",
		"project_name":                   "Default Project",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":       activeAdpID,
			"name":             "Primary OpenAI",
			"billing_mode":     "COMMERCIAL_API",
			"endpoint_url":     "https://api.openai.com/v1",
			"allow_private_ip": false,
			"secret_value":     canary.DefaultCanarySecret(),
			"model_id":         "gpt-4o",
			"data_region":      "US",
		},
	})
	if recOK.Code != http.StatusCreated {
		t.Fatalf("expected 201 on valid bootstrap/complete, got %d body=%s", recOK.Code, recOK.Body.String())
	}
	var bootResp bootstrapCompleteResponse
	mustDecodeJSON(t, recOK.Body.Bytes(), &bootResp)
	if bootResp.ConnectionID == "" || bootResp.DeploymentID == "" || bootResp.DefaultRouteID == "" {
		t.Fatalf("expected connection/deployment/route provisioned, got %+v", bootResp)
	}

	recAfter := h.doPublic(t, http.MethodGet, "/admin/v1/bootstrap/status", nil)
	if recAfter.Code != http.StatusOK {
		t.Fatalf("GET /bootstrap/status post-init code=%d body=%s", recAfter.Code, recAfter.Body.String())
	}
	var stAfter bootstrapStatusResponse
	mustDecodeJSON(t, recAfter.Body.Bytes(), &stAfter)
	if !stAfter.Initialized {
		t.Fatal("expected initialized=true after bootstrap")
	}
	if len(stAfter.AvailableAdapters) != 0 {
		t.Fatalf("expected available_adapters suppressed after initialization, got %+v", stAfter.AvailableAdapters)
	}
}

func TestSyncExtensionProviderAdaptersLifecycleAndFourEyes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "Sync Org", "sync-proj")

	reviewerToken := "tok_reviewer_sync"
	reviewerID := idgen.MustNew(idgen.PrefixPrincipal)
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'reviewer-sync@example.test', 'ACTIVE', 1000, 1000)`,
		reviewerID, boot.OrgID,
	); err != nil {
		t.Fatalf("insert reviewer: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_sync_rev', ?, ?, 'ORG', ?, 'SECURITY_REVIEWER', 1, 1000)`,
		boot.OrgID, reviewerID, boot.OrgID,
	); err != nil {
		t.Fatalf("insert reviewer role_binding: %v", err)
	}
	h.registerBearerToken(reviewerToken, &rbac.Actor{
		PrincipalID:     reviewerID,
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleSecurityReviewer}},
	})

	listAdapters := func() []providerAdapterSummaryDTO {
		rec := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/provider-adapters", ownerCookie, "", "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /provider-adapters code=%d body=%s", rec.Code, rec.Body.String())
		}
		var env paginatedListEnvelope[providerAdapterSummaryDTO]
		mustDecodeJSON(t, rec.Body.Bytes(), &env)
		return env.Data
	}

	// 1. approve-permissions with enable_immediately=false keeps adapter out of active registry until enabled, or syncs with INSTALLED_DISABLED
	disURI := "https://marketplace.tanpopo.dev/sync-disabled-1.0.0.tpkg"
	disSHA := registerCustomSignedProviderTPKG(t, h, disURI, "@tanpopo/sync-disabled", "1.0.0", "sync-disabled", []string{"api.disabled.test"}, false, true)
	recStageDis := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      disURI,
		"expected_sha256": disSHA,
	})
	if recStageDis.Code != http.StatusAccepted {
		t.Fatalf("stage disabled code=%d body=%s", recStageDis.Code, recStageDis.Body.String())
	}
	var stageDisDiff extensionPermissionDiffDTO
	mustDecodeJSON(t, recStageDis.Body.Bytes(), &stageDisDiff)
	recAppDis := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+stageDisDiff.InstallationID+"/approve-permissions", reviewerToken, recStageDis.Result().Header.Get("ETag"), map[string]any{
		"enable_immediately": false,
	})
	if recAppDis.Code != http.StatusOK {
		t.Fatalf("approve enable_immediately=false code=%d body=%s", recAppDis.Code, recAppDis.Body.String())
	}
	var disIns extensionInstallationDTO
	mustDecodeJSON(t, recAppDis.Body.Bytes(), &disIns)
	for _, adp := range listAdapters() {
		if adp.Slug == "sync-disabled" && adp.InstallationState != "INSTALLED_DISABLED" {
			t.Fatalf("expected disabled adapter state INSTALLED_DISABLED, got %+v", adp)
		}
	}

	// 2. approve-permissions with enable_immediately=true registers provider_adapters row cleanly
	v1URI := "https://marketplace.tanpopo.dev/sync-live-1.0.0.tpkg"
	v1SHA := registerCustomSignedProviderTPKG(t, h, v1URI, "@tanpopo/sync-live", "1.0.0", "sync-live", []string{"api.openai.com"}, false, true)
	recStageV1 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      v1URI,
		"expected_sha256": v1SHA,
	})
	if recStageV1.Code != http.StatusAccepted {
		t.Fatalf("stage v1 code=%d body=%s", recStageV1.Code, recStageV1.Body.String())
	}
	var stageV1Diff extensionPermissionDiffDTO
	mustDecodeJSON(t, recStageV1.Body.Bytes(), &stageV1Diff)
	recAppV1 := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+stageV1Diff.InstallationID+"/approve-permissions", reviewerToken, recStageV1.Result().Header.Get("ETag"), map[string]any{
		"enable_immediately": true,
	})
	if recAppV1.Code != http.StatusOK {
		t.Fatalf("approve v1 code=%d body=%s", recAppV1.Code, recAppV1.Body.String())
	}
	var insV1 extensionInstallationDTO
	mustDecodeJSON(t, recAppV1.Body.Bytes(), &insV1)

	var liveAdpID string
	for _, adp := range listAdapters() {
		if adp.Slug == "sync-live" {
			liveAdpID = adp.ID
			if len(adp.AllowedHosts) != 1 || adp.AllowedHosts[0] != "api.openai.com" {
				t.Fatalf("expected allowed_hosts=[api.openai.com], got %+v", adp.AllowedHosts)
			}
			if len(adp.SupportedAuthMethods) != 1 || adp.SupportedAuthMethods[0] != "api_key_header" {
				t.Fatalf("expected supported_auth_methods=[api_key_header], got %+v", adp.SupportedAuthMethods)
			}
		}
	}
	if liveAdpID == "" {
		t.Fatal("sync-live adapter missing from GET /provider-adapters after approve-permissions")
	}

	// 3. Upgrade adds eu.api.openai.com -> provider_adapters updates; Rollback reverts -> provider_adapters reverts
	v2URI := "https://marketplace.tanpopo.dev/sync-live-1.1.0.tpkg"
	v2SHA := registerCustomSignedProviderTPKG(t, h, v2URI, "@tanpopo/sync-live", "1.1.0", "sync-live", []string{"api.openai.com", "eu.api.openai.com"}, false, true)
	recUp := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+insV1.ID+"/upgrade", ownerCookie, boot.CSRFToken, FormatETag(insV1.Version), map[string]any{
		"source_uri":      v2URI,
		"expected_sha256": v2SHA,
	})
	if recUp.Code != http.StatusAccepted {
		t.Fatalf("upgrade v2 code=%d body=%s", recUp.Code, recUp.Body.String())
	}
	recAppUp := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+insV1.ID+"/approve-permissions", reviewerToken, recUp.Result().Header.Get("ETag"), map[string]any{
		"enable_immediately": true,
	})
	if recAppUp.Code != http.StatusOK {
		t.Fatalf("approve upgrade v2 code=%d body=%s", recAppUp.Code, recAppUp.Body.String())
	}
	mustDecodeJSON(t, recAppUp.Body.Bytes(), &insV1)

	recUpAdp := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/provider-adapters/"+liveAdpID, ownerCookie, "", "", nil)
	if recUpAdp.Code != http.StatusOK {
		t.Fatalf("GET /provider-adapters/{id} after upgrade code=%d body=%s", recUpAdp.Code, recUpAdp.Body.String())
	}
	var upAdp providerAdapterSummaryDTO
	mustDecodeJSON(t, recUpAdp.Body.Bytes(), &upAdp)
	if len(upAdp.AllowedHosts) != 2 {
		t.Fatalf("expected 2 allowed_hosts after upgrade, got %+v", upAdp.AllowedHosts)
	}

	recRB := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+insV1.ID+"/rollback", ownerCookie, boot.CSRFToken, FormatETag(insV1.Version), map[string]any{
		"reason": "verify adapter host revert",
	})
	if recRB.Code != http.StatusOK {
		t.Fatalf("rollback code=%d body=%s", recRB.Code, recRB.Body.String())
	}
	mustDecodeJSON(t, recRB.Body.Bytes(), &insV1)

	recRBAdp := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/provider-adapters/"+liveAdpID, ownerCookie, "", "", nil)
	if recRBAdp.Code != http.StatusOK {
		t.Fatalf("GET /provider-adapters/{id} after rollback code=%d body=%s", recRBAdp.Code, recRBAdp.Body.String())
	}
	var rbAdp providerAdapterSummaryDTO
	mustDecodeJSON(t, recRBAdp.Body.Bytes(), &rbAdp)
	if len(rbAdp.AllowedHosts) != 1 || rbAdp.AllowedHosts[0] != "api.openai.com" {
		t.Fatalf("expected reverted allowed_hosts=[api.openai.com] after rollback, got %+v", rbAdp.AllowedHosts)
	}

	// 4. Quarantine then uninstall excludes adapter from both list and get-by-id (404)
	recQuar := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+insV1.ID+"/quarantine", ownerCookie, boot.CSRFToken, FormatETag(insV1.Version), map[string]any{
		"reason": "prepare uninstall verification",
	})
	if recQuar.Code != http.StatusOK {
		t.Fatalf("POST /quarantine code=%d body=%s", recQuar.Code, recQuar.Body.String())
	}
	mustDecodeJSON(t, recQuar.Body.Bytes(), &insV1)
	for _, adp := range listAdapters() {
		if adp.Slug == "sync-live" && adp.InstallationState != "QUARANTINED" {
			t.Fatalf("expected QUARANTINED installation_state, got %+v", adp)
		}
	}

	recDel := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/extensions/"+insV1.ID, ownerCookie, boot.CSRFToken, FormatETag(insV1.Version), nil)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("DELETE /extensions/{id} code=%d body=%s", recDel.Code, recDel.Body.String())
	}
	for _, adp := range listAdapters() {
		if adp.ID == liveAdpID {
			t.Fatalf("uninstalled adapter %s should not appear in GET /provider-adapters", liveAdpID)
		}
	}
	recGetDel := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/provider-adapters/"+liveAdpID, ownerCookie, "", "", nil)
	if recGetDel.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for uninstalled adapter %s, got %d body=%s", liveAdpID, recGetDel.Code, recGetDel.Body.String())
	}

	// 5. Four-eyes change request (EXTENSION_ACTIVATE_EXTERNAL) syncs provider_adapters when second approver decides APPROVE
	feURI := "https://marketplace.tanpopo.dev/sync-foureyes-1.0.0.tpkg"
	feSHA := registerCustomSignedProviderTPKG(t, h, feURI, "@tanpopo/sync-foureyes", "1.0.0", "sync-foureyes", []string{"api.foureyes.test"}, false, true)
	recStageFE := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      feURI,
		"expected_sha256": feSHA,
	})
	if recStageFE.Code != http.StatusAccepted {
		t.Fatalf("stage four-eyes code=%d body=%s", recStageFE.Code, recStageFE.Body.String())
	}
	var stageFEDiff extensionPermissionDiffDTO
	mustDecodeJSON(t, recStageFE.Body.Bytes(), &stageFEDiff)

	recCR := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/change-requests", ownerCookie, boot.CSRFToken, "", map[string]any{
		"action_type": foureyes.ActionExtensionActivateExternal,
		"target_type": "extension_installation",
		"target_id":   stageFEDiff.InstallationID,
		"payload": map[string]any{
			"expected_version":   1,
			"enable_immediately": true,
		},
	})
	if recCR.Code != http.StatusCreated && recCR.Code != http.StatusAccepted {
		t.Fatalf("initiate change-request code=%d body=%s", recCR.Code, recCR.Body.String())
	}
	var cr foureyes.ChangeRequest
	mustDecodeJSON(t, recCR.Body.Bytes(), &cr)

	recDecide := h.doBearer(t, http.MethodPost, "/admin/v1/change-requests/"+cr.ID+"/decide", reviewerToken, FormatETag(cr.Version), map[string]any{
		"decision": "APPROVE",
		"reason":   "second approver activates provider adapter",
	})
	if recDecide.Code != http.StatusOK {
		t.Fatalf("decide change-request code=%d body=%s", recDecide.Code, recDecide.Body.String())
	}
	foundFE := false
	for _, adp := range listAdapters() {
		if adp.Slug == "sync-foureyes" {
			foundFE = true
		}
	}
	if !foundFE {
		t.Fatal("expected sync-foureyes adapter registered after four-eyes change-request approval")
	}
}
