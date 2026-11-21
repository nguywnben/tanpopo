// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	extport "github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/finops/rollup"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/oauth"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

type pg16StubProviderRuntime struct{}

func (pg16StubProviderRuntime) BuildValidationProbe(context.Context, extport.ValidationProbeInput) (extport.UpstreamHttpSpec, error) {
	return extport.UpstreamHttpSpec{}, nil
}
func (pg16StubProviderRuntime) VerifyValidationResult(context.Context, extport.ValidationResultInput) (extport.ValidationVerdict, error) {
	return extport.ValidationVerdict{}, nil
}
func (pg16StubProviderRuntime) TransformRequest(context.Context, extport.TransformRequestInput) (extport.UpstreamHttpSpec, error) {
	return extport.UpstreamHttpSpec{}, nil
}
func (pg16StubProviderRuntime) TransformResponse(context.Context, extport.TransformResponseInput) (extport.NormalizedInferenceResponse, error) {
	return extport.NormalizedInferenceResponse{}, nil
}
func (pg16StubProviderRuntime) TransformStreamChunk(context.Context, extport.StreamChunkInput) (extport.StreamChunkOutput, error) {
	return extport.StreamChunkOutput{}, nil
}
func (pg16StubProviderRuntime) NormalizeError(context.Context, extport.NormalizeErrorInput) (extport.CanonicalProviderError, error) {
	return extport.CanonicalProviderError{}, nil
}
func (pg16StubProviderRuntime) ExtractUsage(context.Context, extport.ExtractUsageInput) (extport.NormalizedUsage, error) {
	return extport.NormalizedUsage{}, nil
}
func (pg16StubProviderRuntime) InspectNativeRequest(context.Context, extport.NativeRequestInput) (extport.NativeRoutingHints, error) {
	return extport.NativeRoutingHints{}, nil
}

func TestTPIMP0040PG16ForceRLSEndToEnd(t *testing.T) {
	pgDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if pgDSN == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_DSN is required when TANPOPO_REQUIRE_POSTGRES_TESTS=1")
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	effectiveDSN, cleanupDB := prepareIsolatedPGDatabase(t, pgDSN)
	defer cleanupDB()

	tmpRoot := t.TempDir()
	kekHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	bootstrapFile := filepath.Join(tmpRoot, "bootstrap.token")

	t.Setenv("TANPOPO_STORAGE_ENGINE", "postgres")
	t.Setenv("TANPOPO_PG_DSN", effectiveDSN)
	t.Setenv("TANPOPO_VALKEY_ADDRS", "")
	t.Setenv("TANPOPO_MASTER_KEY", kekHex)
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	t.Setenv("TANPOPO_DISABLE_VALKEY_PUBSUB", "1")
	t.Setenv("TANPOPO_TEST_HERMETIC", "1")
	t.Setenv("TANPOPO_DRAIN_TIMEOUT", "5s")

	cpURL, cpClose := startNodeServer(t, "control-plane", filepath.Join(tmpRoot, "cp"), bootstrapFile, true)
	defer cpClose()

	dp := startDataPlaneNode(t, "dp-pg16-e2e", filepath.Join(tmpRoot, "dp"), true)
	defer func() {
		if dp != nil && dp.closeFn != nil {
			dp.closeFn()
		}
	}()

	client := netguard.NewHTTPClient()
	client.Timeout = 15 * time.Second
	assertReady(t, client, cpURL)
	assertReady(t, client, dp.url)

	bootTokenRaw, err := os.ReadFile(bootstrapFile)
	if err != nil {
		t.Fatalf("read bootstrap token: %v", err)
	}
	bootResp := bootstrapViaHTTP(t, client, cpURL, strings.TrimSpace(string(bootTokenRaw)))
	csrf, cookies := loginViaHTTP(t, client, cpURL, bootResp.OrgID)

	doReq := func(baseURL, method, path, etag string, payload any) (int, []byte) {
		t.Helper()
		var bodyReader io.Reader
		if payload != nil {
			raw, merr := json.Marshal(payload)
			if merr != nil {
				t.Fatalf("marshal payload for %s %s: %v", method, path, merr)
			}
			bodyReader = bytes.NewReader(raw)
		}
		req, rerr := http.NewRequestWithContext(ctx, method, baseURL+path, bodyReader)
		if rerr != nil {
			t.Fatalf("new request %s %s: %v", method, path, rerr)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" && (method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete) {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		req.Header.Set("X-Tanpopo-Org-Id", bootResp.OrgID)
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		for _, c := range cookies {
			req.AddCookie(c)
		}
		resp, derr := client.Do(req)
		if derr != nil {
			t.Fatalf("%s %s: %v", method, path, derr)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, raw
	}

	assertStatus := func(method, path, etag string, payload any, want ...int) []byte {
		t.Helper()
		code, raw := doReq(cpURL, method, path, etag, payload)
		for _, w := range want {
			if code == w {
				return raw
			}
		}
		t.Fatalf("%s %s status=%d want=%v body=%s", method, path, code, want, string(raw))
		return nil
	}

	// Policies (GET effective, PUT create + update under PG16 FORCE RLS).
	assertStatus(http.MethodGet, "/admin/v1/policies/effective", "", nil, http.StatusOK)
	polID := idgen.MustNew(idgen.PrefixPolicy)
	assertStatus(http.MethodPut, "/admin/v1/policies/"+polID, `"v1"`, map[string]any{
		"scope_tier":               "ORGANIZATION",
		"scope_id":                 bootResp.OrgID,
		"allowed_data_regions":     []string{"US"},
		"min_plugin_trust":         "OFFICIAL_ONLY",
		"fail_mode":                "FAIL_CLOSED",
		"allowed_mcp_tools":        []string{},
		"blocked_model_families":   []string{},
	}, http.StatusOK)

	// Logical models & 23505 unique conflict.
	modelPayload := map[string]any{
		"slug":              "pg16-gpt-4o",
		"display_name":      "PG16 GPT-4o",
		"context_window":    128000,
		"max_output_tokens": 16384,
	}
	assertStatus(http.MethodPost, "/admin/v1/models", "", modelPayload, http.StatusCreated)
	assertStatus(http.MethodPost, "/admin/v1/models", "", modelPayload, http.StatusConflict)
	assertStatus(http.MethodGet, "/admin/v1/models", "", nil, http.StatusOK)
	assertStatus(http.MethodGet, "/admin/v1/deployments", "", nil, http.StatusOK)
	assertStatus(http.MethodGet, "/admin/v1/routes", "", nil, http.StatusOK)

	// Secrets create, list, delete + connections list.
	secRaw := assertStatus(http.MethodPost, "/admin/v1/secrets", "", map[string]any{
		"name":         "pg16-hmac-secret",
		"project_id":   bootResp.ProjectID,
		"secret_kind":  "WEBHOOK_HMAC_SECRET",
		"secret_value": "whsec_pg16_e2e_secret_12345678",
	}, http.StatusCreated)
	var secResp struct {
		CredentialID string `json:"credential_id"`
		ID           string `json:"id"`
	}
	_ = json.Unmarshal(secRaw, &secResp)
	credID := secResp.CredentialID
	if credID == "" {
		credID = secResp.ID
	}
	if credID == "" {
		t.Fatalf("expected secret credential_id in %s", string(secRaw))
	}
	assertStatus(http.MethodGet, "/admin/v1/secrets", "", nil, http.StatusOK)
	assertStatus(http.MethodGet, "/admin/v1/connections", "", nil, http.StatusOK)

	// Extensions & org extension policies under PG16 FORCE RLS + FlexBool.
	assertStatus(http.MethodGet, "/admin/v1/extensions/installations", "", nil, http.StatusOK)
	assertStatus(http.MethodPut, "/admin/v1/organizations/"+bootResp.OrgID+"/extension-policies", `"v1"`, map[string]any{
		"min_plugin_trust": "OFFICIAL_ONLY",
		"policies":         []any{},
	}, http.StatusOK)

	// Members create, conflict, list, role update under PG16 FORCE RLS.
	memRaw := assertStatus(http.MethodPost, "/admin/v1/members", "", map[string]any{
		"email":       "pg16-dev@example.test",
		"kind":        "HUMAN",
		"roles":       []string{"DEVELOPER"},
		"project_ids": []string{bootResp.ProjectID},
	}, http.StatusCreated)
	assertStatus(http.MethodPost, "/admin/v1/members", "", map[string]any{
		"email":       "pg16-dev@example.test",
		"kind":        "HUMAN",
		"roles":       []string{"DEVELOPER"},
		"project_ids": []string{bootResp.ProjectID},
	}, http.StatusConflict)
	var memResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(memRaw, &memResp)
	assertStatus(http.MethodGet, "/admin/v1/members", "", nil, http.StatusOK)
	assertStatus(http.MethodPatch, "/admin/v1/members/"+memResp.ID+"/roles", `"v1"`, map[string]any{
		"roles":       []string{"TEAM_ADMIN"},
		"project_ids": []string{bootResp.ProjectID},
	}, http.StatusOK)

	// Budgets create, duplicate conflict, update, list, usage summary & export.
	bdgID := idgen.MustNew(idgen.PrefixBudget)
	assertStatus(http.MethodPut, "/admin/v1/budgets/"+bdgID, `"v1"`, map[string]any{
		"scope_tier":            "PROJECT",
		"scope_id":              bootResp.ProjectID,
		"period":                "MONTHLY",
		"hard_limit_micros":     50000000,
		"soft_warning_percents": []int{80, 90},
	}, http.StatusOK)
	dupBdgID := idgen.MustNew(idgen.PrefixBudget)
	assertStatus(http.MethodPut, "/admin/v1/budgets/"+dupBdgID, `"v1"`, map[string]any{
		"scope_tier":        "PROJECT",
		"scope_id":          bootResp.ProjectID,
		"period":            "MONTHLY",
		"hard_limit_micros": 60000000,
	}, http.StatusConflict)
	assertStatus(http.MethodGet, "/admin/v1/budgets", "", nil, http.StatusOK)
	assertStatus(http.MethodGet, "/admin/v1/usage/summary?group_by=project", "", nil, http.StatusOK)
	assertStatus(http.MethodGet, "/admin/v1/usage/export?format=csv", "", nil, http.StatusOK)

	// Overview, alerts, traces, webhooks, playground, MCP servers under PG16 FORCE RLS.
	assertStatus(http.MethodGet, "/admin/v1/overview/summary", "", nil, http.StatusOK)
	assertStatus(http.MethodGet, "/admin/v1/alerts/active", "", nil, http.StatusOK)
	assertStatus(http.MethodGet, "/admin/v1/traces", "", nil, http.StatusOK)

	whPayload := map[string]any{
		"target_url":            "https://93.184.216.34/pg16-receiver",
		"project_id":            bootResp.ProjectID,
		"events":                []string{"budget.threshold"},
		"signing_credential_id": credID,
	}
	assertStatus(http.MethodPost, "/admin/v1/webhooks", "", whPayload, http.StatusCreated)
	assertStatus(http.MethodGet, "/admin/v1/webhooks", "", nil, http.StatusOK)

	// Deleting secret referenced by webhook triggers PG16 SQLSTATE 23503 conflict rather than 500.
	assertStatus(http.MethodDelete, "/admin/v1/secrets/"+credID, `"v1"`, nil, http.StatusConflict)

	// Unreferenced secret can be created and deleted cleanly.
	unrefRaw := assertStatus(http.MethodPost, "/admin/v1/secrets", "", map[string]any{
		"name":         "pg16-ephemeral-key",
		"project_id":   bootResp.ProjectID,
		"secret_kind":  "API_KEY",
		"secret_value": "sk-pg16-delete-me-123456",
	}, http.StatusCreated)
	var unrefResp struct {
		CredentialID string `json:"credential_id"`
		ID           string `json:"id"`
	}
	_ = json.Unmarshal(unrefRaw, &unrefResp)
	delID := unrefResp.CredentialID
	if delID == "" {
		delID = unrefResp.ID
	}
	assertStatus(http.MethodDelete, "/admin/v1/secrets/"+delID, `"v1"`, nil, http.StatusNoContent)

	mcpPayload := map[string]any{
		"project_id":   bootResp.ProjectID,
		"slug":         "pg16-mcp-tools",
		"name":         "PG16 MCP Tools",
		"transport":    "streamable_http",
		"endpoint_url": "https://93.184.216.34/mcp",
	}
	assertStatus(http.MethodPost, "/admin/v1/mcp/servers", "", mcpPayload, http.StatusCreated)
	assertStatus(http.MethodPost, "/admin/v1/mcp/servers", "", mcpPayload, http.StatusConflict)
	assertStatus(http.MethodGet, "/admin/v1/mcp/servers", "", nil, http.StatusOK)

	assertStatus(http.MethodPost, "/admin/v1/playground/sessions", "", map[string]any{
		"project_id":        bootResp.ProjectID,
		"env_id":            bootResp.EnvID,
		"ttl_seconds":       300,
		"max_budget_micros": 125000,
	}, http.StatusCreated)

	// Audit list & cryptographic hash chain verification under PG16 FORCE RLS (VerifyAuditChainTx).
	assertStatus(http.MethodGet, "/admin/v1/audit-events", "", nil, http.StatusOK)
	verifyRaw := assertStatus(http.MethodPost, "/admin/v1/audit-events/verify", "", map[string]any{}, http.StatusOK)
	var verifyOut struct {
		Intact bool `json:"intact"`
	}
	if err := json.Unmarshal(verifyRaw, &verifyOut); err != nil || !verifyOut.Intact {
		t.Fatalf("expected intact audit hash chain on PG16, got err=%v body=%s", err, string(verifyRaw))
	}

	// Kill-switch trigger, list, config_epoch check, and clear.
	vkey := createVKeyViaHTTP(t, client, cpURL, csrf, cookies, bootResp.OrgID, bootResp.ProjectID, bootResp.EnvID, "pg16-mcp-vk")
	ksRaw := assertStatus(http.MethodPost, "/admin/v1/kill-switches", "", map[string]any{
		"target_level":         "VIRTUAL_KEY",
		"target_id":            vkey.ID,
		"reason":               "pg16 e2e kill switch verification",
		"abort_active_streams": true,
	}, http.StatusOK)
	var ksResp struct {
		ConfigEpoch int64 `json:"config_epoch"`
	}
	if err := json.Unmarshal(ksRaw, &ksResp); err != nil || ksResp.ConfigEpoch < 1 {
		t.Fatalf("expected valid config_epoch >= 1 in kill switch response, got err=%v body=%s", err, string(ksRaw))
	}
	ksListRaw := assertStatus(http.MethodGet, "/admin/v1/kill-switches", "", nil, http.StatusOK)
	var ksList struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(ksListRaw, &ksList); err != nil || len(ksList.Data) == 0 {
		t.Fatalf("expected active kill switch in list, got err=%v body=%s", err, string(ksListRaw))
	}
	assertStatus(http.MethodDelete, "/admin/v1/kill-switches/"+ksList.Data[0].ID, "", map[string]any{"expected_version": 1, "clear_reason": "pg16 e2e clear"}, http.StatusNoContent, http.StatusOK)

	// Data Plane /mcp ingress exercise with Virtual Key against live PG16 snapshot/ledger.
	mcpReqBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	mcpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, dp.url+"/mcp", bytes.NewReader(mcpReqBody))
	mcpReq.Header.Set("Content-Type", "application/json")
	mcpReq.Header.Set("Authorization", "Bearer "+vkey.SecretKey)
	mcpResp, merr := client.Do(mcpReq)
	if merr != nil {
		t.Fatalf("DP POST /mcp: %v", merr)
	}
	mcpRespBody, _ := io.ReadAll(mcpResp.Body)
	_ = mcpResp.Body.Close()
	if mcpResp.StatusCode >= 500 {
		t.Fatalf("DP POST /mcp returned 5xx (%d): %s", mcpResp.StatusCode, string(mcpRespBody))
	}

	// Direct package exercises against live PG16 FORCE RLS schema.
	pgStore, err := postgres.Open(ctx, postgres.Options{DSN: effectiveDSN, SkipMigrate: true, PartitionLookaheadMonths: -1})
	if err != nil {
		t.Fatalf("postgres.Open direct: %v", err)
	}
	defer pgStore.Close()
	db := pgStore.RawDB()

	agg, err := rollup.NewAggregator(db, nil)
	if err != nil {
		t.Fatalf("rollup.NewAggregator: %v", err)
	}
	nowMs := time.Now().UTC().UnixMilli()
	if _, err := agg.AggregateHour(ctx, nowMs); err != nil {
		t.Fatalf("rollup.AggregateHour on PG16 FORCE RLS: %v", err)
	}
	if _, err := agg.QueryOrgRollups(ctx, bootResp.OrgID, rollup.FloorHourMs(nowMs), rollup.FloorHourMs(nowMs)+rollup.HourMs); err != nil {
		t.Fatalf("rollup.QueryOrgRollups on PG16 FORCE RLS: %v", err)
	}

	txAudit, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}

	ksReg, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{
		DB:    db,
		Audit: txAudit,
	})
	if err != nil {
		t.Fatalf("killswitch.NewRegistry: %v", err)
	}
	abortTrue := true
	sw, err := ksReg.Trigger(ctx, killswitch.TriggerParams{
		OrgID:                  &bootResp.OrgID,
		ProjectID:              bootResp.ProjectID,
		Level:                  killswitch.LevelVirtualKey,
		TargetID:               vkey.ID,
		Reason:                 "direct PG16 barrier test",
		TriggeredByPrincipalID: memResp.ID,
		AbortActiveStreams:     &abortTrue,
	})
	if err != nil {
		t.Fatalf("killswitch.Trigger on PG16: %v", err)
	}
	if _, err := ksReg.EnsureActiveAbortBarrier(ctx, killswitch.LevelVirtualKey, vkey.ID, &bootResp.OrgID); err != nil {
		t.Fatalf("killswitch.EnsureActiveAbortBarrier on PG16: %v (switch=%s)", err, sw.ID)
	}

	catRepo, err := catalog.NewRepository(catalog.Config{
		DB:                        db,
		AllowDetachedControlPlane: true,
		Audit:                     txAudit,
	})
	if err != nil {
		t.Fatalf("catalog.NewRepository: %v", err)
	}
	kekRaw := make([]byte, 32)
	for i := range kekRaw {
		kekRaw[i] = byte(i + 1)
	}
	kekProv, err := vault.NewStaticKEKProvider("kek_pg16_e2e", kekRaw)
	if err != nil {
		t.Fatalf("vault.NewStaticKEKProvider: %v", err)
	}
	defer kekProv.Close()
	vStore, err := vault.NewStore(vault.Config{
		DB:            db,
		KEK:           kekProv,
		PostShredHook: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	defer vStore.Close()
	brk, err := broker.NewBlindBroker(broker.Config{
		Resolver:       catRepo,
		PolicyResolver: catRepo,
		Vault:          vStore,
		Audit:          txAudit,
	})
	if err != nil {
		t.Fatalf("broker.NewBlindBroker: %v", err)
	}
	if _, _, verr := catRepo.ValidateConnection(ctx, bootResp.OrgID, "con_missing_pg16", pg16StubProviderRuntime{}, brk); !errors.Is(verr, port.ErrNotFound) {
		t.Fatalf("catalog.ValidateConnection expected ErrNotFound under PG16 FORCE RLS, got %v", verr)
	}

	lifeSvc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:           db,
		Audit:        txAudit,
		PluginsDir:   filepath.Join(tmpRoot, "plugins"),
		LockfilePath: filepath.Join(tmpRoot, "extensions.lock.json"),
	})
	if err != nil {
		t.Fatalf("installer.NewLifecycleService: %v", err)
	}
	if _, terr := lifeSvc.EvaluatePackagePolicyTrust(ctx, "pkg_missing_pg16", "OFFICIAL_ONLY", nil); !errors.Is(terr, port.ErrNotFound) {
		t.Fatalf("installer.EvaluatePackagePolicyTrust expected ErrNotFound on PG16, got %v", terr)
	}

	oauthSvc, err := oauth.NewService(oauth.ServiceConfig{
		DB:          db,
		Vault:       vStore,
		Audit:       txAudit,
		RedirectURI: "https://console.tanpopo.test/admin/v1/oauth/callback",
	})
	if err != nil {
		t.Fatalf("oauth.NewService: %v", err)
	}
	if _, aerr := oauthSvc.AccessToken(ctx, bootResp.OrgID, "crd_missing_pg16", ""); !errors.Is(aerr, port.ErrNotFound) && aerr == nil {
		t.Fatalf("oauth.AccessToken expected non-nil not-found/state error on PG16, got %v", aerr)
	}
}
