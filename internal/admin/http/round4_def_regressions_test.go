// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func TestDEF052_053_077_078_079_Round4FinOpsAndPricingRegressions(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "Round4 FinOps Org", "r4-finops-proj")
	ctx := context.Background()
	nowMs := h.fk.Now().UnixMilli()

	// 1. DEF-053: Canonical pricing schema version 2 in NANO_USD_PER_TOKEN + legacy v1 compatibility.
	recMod := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/models", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":              "r4-priced-model",
		"display_name":      "Round 4 Priced Model",
		"context_window":    32768,
		"max_output_tokens": 4096,
		"assurance_tier":    "TESTED",
	})
	if recMod.Code != http.StatusCreated {
		t.Fatalf("DEF-053 POST /models code=%d body=%s", recMod.Code, recMod.Body.String())
	}
	var modResp struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, recMod.Body.Bytes(), &modResp)

	adpID := seedLocalOpenAIAdapter(t, h, "127.0.0.1")
	recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "r4-conn",
		"adapter_id":       adpID,
		"billing_mode":     "COMMERCIAL_API",
		"endpoint_url":     "http://127.0.0.1:18080/v1",
		"data_region":      "US",
		"allow_private_ip": true,
		"allowed_cidrs":    []string{"127.0.0.1/32"},
	})
	if recConn.Code != http.StatusCreated {
		t.Fatalf("POST /connections code=%d body=%s", recConn.Code, recConn.Body.String())
	}
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)

	// Reject ambiguous legacy-unit writes while permitting canonical NANO_USD_PER_TOKEN writes.
	recDepBad := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/deployments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":               boot.EnvID,
		"logical_model_id":     modResp.ID,
		"connection_id":        conn.ID,
		"upstream_model_name":  "qwen2.5",
		"data_region":          "US",
		"activate_immediately": false,
		"rate_card": map[string]any{
			"token_rate_unit":       "LEGACY_NANO_USD_PER_1M",
			"input_token_nano_usd":  1000,
			"output_token_nano_usd": 2000,
		},
	})
	if recDepBad.Code != http.StatusBadRequest {
		t.Fatalf("DEF-053: expected 400 rejecting non-canonical deployment token_rate_unit, got %d body=%s", recDepBad.Code, recDepBad.Body.String())
	}

	recDep := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/deployments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":               boot.EnvID,
		"logical_model_id":     modResp.ID,
		"connection_id":        conn.ID,
		"upstream_model_name":  "qwen2.5",
		"data_region":          "US",
		"activate_immediately": false,
		"rate_card": map[string]any{
			"token_rate_unit":       "NANO_USD_PER_TOKEN",
			"input_token_nano_usd":  1000, // $1.00 / 1M tokens = 1000 nano-USD / token
			"output_token_nano_usd": 2000,
		},
	})
	if recDep.Code != http.StatusCreated {
		t.Fatalf("DEF-053 POST /deployments code=%d body=%s", recDep.Code, recDep.Body.String())
	}
	var depResp struct {
		ID       string `json:"id"`
		RateCard struct {
			TokenRateUnit string `json:"token_rate_unit"`
		} `json:"rate_card"`
	}
	mustDecodeJSON(t, recDep.Body.Bytes(), &depResp)
	if depResp.RateCard.TokenRateUnit != "NANO_USD_PER_TOKEN" {
		t.Fatalf("DEF-053: expected rate_card.token_rate_unit NANO_USD_PER_TOKEN on deployment, got %+v", depResp)
	}
	var rawPricingJSON string
	if err := h.st.RawDB().QueryRow(`SELECT pricing_nano_usd_json FROM model_deployments WHERE id = ?`, depResp.ID).Scan(&rawPricingJSON); err != nil {
		t.Fatalf("query stored deployment pricing json: %v", err)
	}
	if !strings.Contains(rawPricingJSON, `"pricing_schema_version":2`) || !strings.Contains(rawPricingJSON, `"token_rate_unit":"nano_usd_per_token"`) {
		t.Fatalf("DEF-053: expected stored pricing_schema_version:2 and nano_usd_per_token, got %s", rawPricingJSON)
	}

	// Verify pricing calculator scales canonical v2 (per token) vs legacy v1 (per 1M tokens).
	costV2, err := pricing.Calculate(pricing.RateCard{
		PricingSchemaVersion: 2,
		TokenRateUnit:        "NANO_USD_PER_TOKEN",
		InputTokenNanoUSD:    1000,
		OutputTokenNanoUSD:   2000,
		BillingMode:          pricing.BillingModeCommercialAPI,
	}, pricing.UsageInput{
		InputTokens:          1,
		OutputTokens:         1,
		UpstreamUsagePresent: true,
		UpstreamAccuracy:     pricing.AccuracyExact,
	})
	if err != nil || costV2.TotalMicroUSD != 3 {
		t.Fatalf("DEF-053: expected 3 micro-USD for 1 in + 1 out @ 1000/2000 nano-USD/tok, got %+v err=%v", costV2, err)
	}
	costV1Legacy, err := pricing.Calculate(pricing.RateCard{
		PricingSchemaVersion: 1,
		TokenRateUnit:        "LEGACY_NANO_USD_PER_1M",
		InputPer1MNanoUSD:    1_000_000_000, // legacy $1.00 / 1M stored in per_1m
		OutputPer1MNanoUSD:   2_000_000_000,
		BillingMode:          pricing.BillingModeCommercialAPI,
	}, pricing.UsageInput{
		InputTokens:          1000,
		OutputTokens:         1000,
		UpstreamUsagePresent: true,
		UpstreamAccuracy:     pricing.AccuracyExact,
	})
	if err != nil || costV1Legacy.TotalMicroUSD != 3000 {
		t.Fatalf("DEF-053: expected legacy per_1m conversion == 3000 micro-USD for 1k tokens @ $1/$2 per 1M, got %+v err=%v", costV1Legacy, err)
	}

	// 2. DEF-077: Two distinct Virtual Keys with identical display name must stay separate with key_prefix & group_id in JSON and CSV.
	var vkIDs []string
	var vkPrefixes []string
	for i := 0; i < 2; i++ {
		recVK := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", ownerCookie, boot.CSRFToken, "", map[string]any{
			"project_id": boot.ProjectID,
			"env_id":     boot.EnvID,
			"name":       "Duplicate Ops Key",
		})
		if recVK.Code != http.StatusCreated {
			t.Fatalf("POST /virtual-keys[%d] code=%d body=%s", i, recVK.Code, recVK.Body.String())
		}
		var vkRes struct {
			ID     string `json:"id"`
			Prefix string `json:"key_prefix"`
		}
		mustDecodeJSON(t, recVK.Body.Bytes(), &vkRes)
		vkIDs = append(vkIDs, vkRes.ID)
		vkPrefixes = append(vkPrefixes, vkRes.Prefix)
	}

	for idx, cost := range []int64{2500, 4700} {
		setID := idgen.MustNew(idgen.PrefixSettlement)
		if _, err := h.st.RawDB().ExecContext(ctx, `
			INSERT INTO usage_settlements (
				id, settled_at_ms, request_id, org_id, project_id, env_id, virtual_key_id,
				plane, logical_model_slug, billing_mode, usage_accuracy, total_cost_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, 'PROVIDER', 'r4-priced-model', 'COMMERCIAL_API', 'EXACT', ?)`,
			setID, nowMs, fmt.Sprintf("trc_r4_dupvk_%d", idx), boot.OrgID, boot.ProjectID, boot.EnvID, vkIDs[idx], cost,
		); err != nil {
			t.Fatalf("insert usage_settlements[%d]: %v", idx, err)
		}
	}
	// Also record 1 unmetered row for vkIDs[0] to exercise ALT-UNMETERED-SPEND later.
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO usage_settlements (
			id, settled_at_ms, request_id, org_id, project_id, env_id, virtual_key_id,
			plane, logical_model_slug, billing_mode, usage_accuracy, total_cost_micros
		) VALUES (?, ?, 'trc_r4_unmetered', ?, ?, ?, ?, 'PROVIDER', 'r4-priced-model', 'COMMERCIAL_API', 'UNMETERED_FLAGGED', 1000)`,
		idgen.MustNew(idgen.PrefixSettlement), nowMs, boot.OrgID, boot.ProjectID, boot.EnvID, vkIDs[0],
	); err != nil {
		t.Fatalf("insert unmetered settlement: %v", err)
	}

	recChg := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/usage/summary?group_by=key", ownerCookie, "", "", nil)
	if recChg.Code != http.StatusOK {
		t.Fatalf("DEF-077 GET /usage/summary?group_by=key code=%d body=%s", recChg.Code, recChg.Body.String())
	}
	var chgResp struct {
		Data []struct {
			GroupID     string `json:"group_id"`
			GroupKey    string `json:"group_key"`
			KeyPrefix   string `json:"key_prefix"`
			ExactMicros int64  `json:"exact_micro_usd"`
		} `json:"data"`
	}
	mustDecodeJSON(t, recChg.Body.Bytes(), &chgResp)
	seenDup := map[string]int64{}
	for _, row := range chgResp.Data {
		if row.GroupKey == "Duplicate Ops Key" {
			if row.GroupID == "" || !strings.Contains(row.KeyPrefix, "...") {
				t.Fatalf("DEF-077: expected group_id and masked key_prefix (...), got %+v", row)
			}
			seenDup[row.GroupID] = row.ExactMicros
		}
	}
	if seenDup[vkIDs[0]] != 2500 || seenDup[vkIDs[1]] != 4700 {
		t.Fatalf("DEF-077: expected distinct rows for duplicate Virtual Keys (2500, 4700), got %+v (prefixes=%v)", seenDup, vkPrefixes)
	}

	recCSV := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/usage/export?group_by=key&format=csv", ownerCookie, "", "", nil)
	if recCSV.Code != http.StatusOK {
		t.Fatalf("DEF-077 CSV export code=%d body=%s", recCSV.Code, recCSV.Body.String())
	}
	csvRows, err := csv.NewReader(strings.NewReader(recCSV.Body.String())).ReadAll()
	if err != nil || len(csvRows) < 3 {
		t.Fatalf("DEF-077 invalid CSV rows=%v err=%v", csvRows, err)
	}
	expectedHdr := []string{"group_key", "exact_micro_usd", "estimated_micro_usd", "exact_request_count", "estimated_request_count", "unmetered_request_count", "group_id", "key_prefix"}
	if strings.Join(csvRows[0], ",") != strings.Join(expectedHdr, ",") {
		t.Fatalf("DEF-077 unexpected CSV header %v, want %v", csvRows[0], expectedHdr)
	}
	csvDupIDs := map[string]bool{}
	for _, r := range csvRows[1:] {
		if r[0] == "Duplicate Ops Key" && strings.Contains(r[7], "...") {
			csvDupIDs[r[6]] = true
		}
	}
	if !csvDupIDs[vkIDs[0]] || !csvDupIDs[vkIDs[1]] {
		t.Fatalf("DEF-077 expected both duplicate key group_id values in CSV trailing columns, got %v", csvDupIDs)
	}

	// 3. DEF-078: Mid-month budget creation must backfill prior usage_settlements in current window (total exact = 7200).
	bdgID := idgen.MustNew(idgen.PrefixBudget)
	recBdg := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/budgets/"+bdgID, ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"scope_tier":            "ORG",
		"scope_id":              boot.OrgID,
		"period":                "MONTHLY",
		"hard_limit_micros":     8000, // 7200/8000 = 90% -> triggers ALT-BUDGET-80 too!
		"soft_warning_percents": []int{80},
	})
	if recBdg.Code != http.StatusOK {
		t.Fatalf("DEF-078 PUT /budgets/{id} code=%d body=%s", recBdg.Code, recBdg.Body.String())
	}
	var bdgResp struct {
		ID                 string `json:"id"`
		SettledExactMicros int64  `json:"settled_exact_micros"`
		PeriodStart        string `json:"period_start"`
		PeriodEnd          string `json:"period_end"`
	}
	mustDecodeJSON(t, recBdg.Body.Bytes(), &bdgResp)
	if bdgResp.SettledExactMicros != 7200 {
		t.Fatalf("DEF-078: expected mid-month budget backfill settled_exact_micros=7200, got %d", bdgResp.SettledExactMicros)
	}
	if bdgResp.PeriodStart == "" || bdgResp.PeriodEnd == "" {
		t.Fatalf("DEF-078: expected period_start and period_end timestamps on budget resource, got %+v", bdgResp)
	}

	// 4. DEF-052: Trace with no matching usage_settlements row must return "UNSETTLED" instead of false "EXACT".
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO request_traces_meta (
			request_id, created_at_ms, started_at_ms, org_id, project_id, env_id, virtual_key_id, connection_id,
			extension_installation_id, entry_point, route_slug, http_status, proxy_overhead_us, total_duration_ms, waterfall_stages_json
		) VALUES ('trc_r4_unsettled', ?, ?, ?, ?, ?, ?, ?, '', 'native_http', 'r4-priced-model', 200, 50, 12, '[]')`,
		nowMs, nowMs-12, boot.OrgID, boot.ProjectID, boot.EnvID, vkIDs[0], conn.ID,
	); err != nil {
		t.Fatalf("insert unsettled trace: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO request_traces_meta (
			request_id, created_at_ms, started_at_ms, org_id, project_id, env_id, virtual_key_id, connection_id,
			extension_installation_id, entry_point, route_slug, http_status, proxy_overhead_us, total_duration_ms, waterfall_stages_json
		) VALUES ('trc_r4_dupvk_0', ?, ?, ?, ?, ?, ?, ?, '', 'native_http', 'r4-priced-model', 200, 50, 12, '[]')`,
		nowMs, nowMs-12, boot.OrgID, boot.ProjectID, boot.EnvID, vkIDs[0], conn.ID,
	); err != nil {
		t.Fatalf("insert settled trace: %v", err)
	}
	recTraces := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/traces?limit=20", ownerCookie, "", "", nil)
	if recTraces.Code != http.StatusOK {
		t.Fatalf("GET /traces code=%d body=%s", recTraces.Code, recTraces.Body.String())
	}
	var trList struct {
		Data []struct {
			RequestID     string `json:"request_id"`
			UsageAccuracy string `json:"usage_accuracy"`
			CostMicros    int64  `json:"total_cost_micros"`
		} `json:"data"`
	}
	mustDecodeJSON(t, recTraces.Body.Bytes(), &trList)
	gotTraceStatus := map[string]string{}
	for _, tr := range trList.Data {
		gotTraceStatus[tr.RequestID] = tr.UsageAccuracy
	}
	if gotTraceStatus["trc_r4_unsettled"] != "UNSETTLED" {
		t.Fatalf("DEF-052: expected unsettled trace to report UNSETTLED (not EXACT), got %q", gotTraceStatus["trc_r4_unsettled"])
	}
	if gotTraceStatus["trc_r4_dupvk_0"] != "EXACT" {
		t.Fatalf("DEF-052: expected reconciled trace to report EXACT, got %q", gotTraceStatus["trc_r4_dupvk_0"])
	}

	// 5. DEF-079: Active alerts (ALT-BUDGET-80, ALT-UNMETERED-SPEND, ALT-PLUGIN-TRAP) must carry valid Console routes.
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE extension_installations SET state = 'QUARANTINED'`); err != nil {
		t.Fatalf("quarantine extension installation for alert check: %v", err)
	}
	recAlerts := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/alerts/active", ownerCookie, "", "", nil)
	if recAlerts.Code != http.StatusOK {
		t.Fatalf("GET /alerts/active code=%d body=%s", recAlerts.Code, recAlerts.Body.String())
	}
	var alResp struct {
		Data []struct {
			AlertCode        string `json:"alert_code"`
			RemediationRoute string `json:"remediation_route"`
		} `json:"data"`
	}
	mustDecodeJSON(t, recAlerts.Body.Bytes(), &alResp)
	routesByCode := map[string]string{}
	for _, a := range alResp.Data {
		routesByCode[a.AlertCode] = a.RemediationRoute
	}
	if routesByCode["ALT-BUDGET-80"] != "/finops/budgets" ||
		routesByCode["ALT-UNMETERED-SPEND"] != "/finops/chargeback" ||
		routesByCode["ALT-PLUGIN-TRAP"] != "/extensions/installed" {
		t.Fatalf("DEF-079: expected valid remediation routes for alerts, got %+v", routesByCode)
	}
}

func TestDEF058_061_062_071_073_ExtensionLifecycleAndSecurityAPI(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "Round4 Extensions Org", "r4-ext-proj")

	// 1. DEF-071: Multi-hyphen spoofed @tanpopo-qa004-spoof/canary archive without root sig -> 403 reserved_namespace_spoofing.
	spoofURI := "https://marketplace.tanpopo.dev/spoof-qa004.tpkg"
	spoofSHA := h.registerProviderTPKGWithSig(t, spoofURI, "@tanpopo-qa004-spoof/canary", "1.0.0", []string{"api.openai.com"}, false, true, false)
	recSpoof := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      spoofURI,
		"expected_sha256": spoofSHA,
	})
	if recSpoof.Code != http.StatusForbidden || !strings.Contains(recSpoof.Body.String(), "reserved_namespace_spoofing") {
		t.Fatalf("DEF-071: expected 403 reserved_namespace_spoofing, got %d body=%s", recSpoof.Code, recSpoof.Body.String())
	}

	// 2. DEF-073: Digest mismatch returns hash_mismatch.
	v1URI := "https://marketplace.tanpopo.dev/acme-r4-v1.tpkg"
	v1SHA := h.registerSignedProviderTPKG(t, v1URI, "@acme/r4-lifecycle", "1.0.0", []string{"api.openai.com"}, false, true)
	wrongSHA := strings.Repeat("0", 64)
	recMismatch := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      v1URI,
		"expected_sha256": wrongSHA,
	})
	if recMismatch.Code != http.StatusBadRequest || !strings.Contains(recMismatch.Body.String(), "hash_mismatch") {
		t.Fatalf("DEF-073: expected 400 hash_mismatch, got %d body=%s", recMismatch.Code, recMismatch.Body.String())
	}

	// Stage and approve v1 -> ENABLED_ACTIVE.
	recStage := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      v1URI,
		"expected_sha256": v1SHA,
	})
	if recStage.Code != http.StatusAccepted {
		t.Fatalf("stage v1 code=%d body=%s", recStage.Code, recStage.Body.String())
	}
	var stageResp struct {
		InstallationID string `json:"installation_id"`
	}
	mustDecodeJSON(t, recStage.Body.Bytes(), &stageResp)
	recApprove := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+stageResp.InstallationID+"/approve-permissions", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"enable_immediately": true,
		"ack_unverified":     false,
	})
	if recApprove.Code != http.StatusOK {
		t.Fatalf("approve v1 code=%d body=%s", recApprove.Code, recApprove.Body.String())
	}
	var inst extensionInstallationDTO
	mustDecodeJSON(t, recApprove.Body.Bytes(), &inst)

	// 3. DEF-061 + DEF-062: Escalated upgrade enters UPDATING_PENDING_RECONSENT; discarding candidate returns ENABLED_ACTIVE; re-submitting same candidate returns 409 already_exists.
	v2URI := "https://marketplace.tanpopo.dev/acme-r4-v2.tpkg"
	v2SHA := h.registerSignedProviderTPKG(t, v2URI, "@acme/r4-lifecycle", "2.0.0", []string{"api.openai.com", "api.extra.example"}, false, true)
	recUpg := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+inst.ID+"/upgrade", ownerCookie, boot.CSRFToken, FormatETag(inst.Version), map[string]any{
		"source_uri":      v2URI,
		"expected_sha256": v2SHA,
	})
	if recUpg.Code != http.StatusAccepted {
		t.Fatalf("DEF-061 upgrade code=%d body=%s", recUpg.Code, recUpg.Body.String())
	}
	recList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/extensions/installations", ownerCookie, "", "", nil)
	var extList struct {
		Data []extensionInstallationDTO `json:"data"`
	}
	mustDecodeJSON(t, recList.Body.Bytes(), &extList)
	var upgInst extensionInstallationDTO
	for _, it := range extList.Data {
		if it.ID == inst.ID {
			upgInst = it
		}
	}
	if upgInst.State != "UPDATING_PENDING_RECONSENT" {
		t.Fatalf("expected UPDATING_PENDING_RECONSENT, got %q", upgInst.State)
	}
	recDiscard := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/extensions/"+inst.ID, ownerCookie, boot.CSRFToken, FormatETag(upgInst.Version), nil)
	if recDiscard.Code != http.StatusNoContent {
		t.Fatalf("DEF-061 discard candidate DELETE code=%d body=%s", recDiscard.Code, recDiscard.Body.String())
	}
	recListAfterDiscard := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/extensions/installations", ownerCookie, "", "", nil)
	mustDecodeJSON(t, recListAfterDiscard.Body.Bytes(), &extList)
	for _, it := range extList.Data {
		if it.ID == inst.ID {
			inst = it
		}
	}
	if inst.State != "ENABLED_ACTIVE" {
		t.Fatalf("DEF-061 expected ENABLED_ACTIVE after discarding candidate, got %q", inst.State)
	}
	recDupUpg := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+inst.ID+"/upgrade", ownerCookie, boot.CSRFToken, FormatETag(inst.Version), map[string]any{
		"source_uri":      v2URI,
		"expected_sha256": v2SHA,
	})
	if recDupUpg.Code != http.StatusConflict || !strings.Contains(recDupUpg.Body.String(), "already_exists") {
		t.Fatalf("DEF-062 expected 409 already_exists on duplicate candidate version, got %d body=%s", recDupUpg.Code, recDupUpg.Body.String())
	}

	// 4. DEF-058: Quarantine -> Unquarantine (INSTALLED_DISABLED) -> Approve-permissions re-enable (ENABLED_ACTIVE).
	recQuar := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+inst.ID+"/quarantine", ownerCookie, boot.CSRFToken, FormatETag(inst.Version), map[string]any{
		"reason": "anomaly isolated",
	})
	if recQuar.Code != http.StatusOK {
		t.Fatalf("quarantine code=%d body=%s", recQuar.Code, recQuar.Body.String())
	}
	mustDecodeJSON(t, recQuar.Body.Bytes(), &inst)
	if inst.State != "QUARANTINED" {
		t.Fatalf("expected QUARANTINED, got %q", inst.State)
	}
	// Stale ETag on unquarantine must fail with 409 optimistic_lock_conflict.
	recStaleUnq := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+inst.ID+"/unquarantine", ownerCookie, boot.CSRFToken, `"v999"`, map[string]any{
		"reason": "verified safe",
	})
	if recStaleUnq.Code != http.StatusConflict || !strings.Contains(recStaleUnq.Body.String(), "optimistic_lock_conflict") {
		t.Fatalf("DEF-058 expected 409 optimistic_lock_conflict on stale If-Match unquarantine, got %d body=%s", recStaleUnq.Code, recStaleUnq.Body.String())
	}
	recUnq := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+inst.ID+"/unquarantine", ownerCookie, boot.CSRFToken, FormatETag(inst.Version), map[string]any{
		"reason": "verified safe",
	})
	if recUnq.Code != http.StatusOK {
		t.Fatalf("DEF-058 unquarantine code=%d body=%s", recUnq.Code, recUnq.Body.String())
	}
	mustDecodeJSON(t, recUnq.Body.Bytes(), &inst)
	if inst.State != "INSTALLED_DISABLED" {
		t.Fatalf("DEF-058 expected INSTALLED_DISABLED after unquarantine, got %q", inst.State)
	}
	recReenable := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+inst.ID+"/approve-permissions", ownerCookie, boot.CSRFToken, FormatETag(inst.Version), map[string]any{
		"enable_immediately": true,
		"ack_unverified":     false,
	})
	if recReenable.Code != http.StatusOK {
		t.Fatalf("DEF-058 re-enable code=%d body=%s", recReenable.Code, recReenable.Body.String())
	}
	mustDecodeJSON(t, recReenable.Body.Bytes(), &inst)
	if inst.State != "ENABLED_ACTIVE" {
		t.Fatalf("DEF-058 expected ENABLED_ACTIVE after reactivation, got %q", inst.State)
	}
}

func TestDEF072_ClusterNodesAndDR03KeyRetirementGate(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "Round4 DR03 Org", "r4-dr03-proj")
	ctx := context.Background()
	nowMs := h.fk.Now().UTC().UnixMilli()

	// Stage kek_v2, promote active, and rewrap org DEKs from kek_v1 -> kek_v2.
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = byte(i + 1)
	}
	if err := h.kek.RegisterKey("kek_v2", key2, true); err != nil {
		t.Fatalf("RegisterKey kek_v2: %v", err)
	}
	if _, err := h.vault.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2"); err != nil {
		t.Fatalf("RewrapAllOrgDEKs: %v", err)
	}

	var currentEpoch int64
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COALESCE(MAX(config_epoch), 0) FROM config_changelog`).Scan(&currentEpoch); err != nil {
		t.Fatalf("query currentEpoch: %v", err)
	}
	if currentEpoch <= 0 {
		currentEpoch = 1
	}

	insertNode := func(id, role string, epoch, hbMs int64, health string) {
		t.Helper()
		_, err := h.st.RawDB().ExecContext(ctx, `
			INSERT INTO cluster_nodes (
				node_id, role, listen_addr, config_epoch, snapshot_age_ms,
				health, storage_engine, wal_bytes, loaded_kek_ids_json,
				last_heartbeat_ms, started_at_ms
			) VALUES (?, ?, ?, ?, 15, ?, 'sqlite_wal', 4096, '["kek_v1","kek_v2"]', ?, ?)
			ON CONFLICT(node_id) DO UPDATE SET
				config_epoch = excluded.config_epoch,
				health = excluded.health,
				last_heartbeat_ms = excluded.last_heartbeat_ms
		`, id, role, "127.0.0.1:8080", epoch, health, hbMs, hbMs-5000)
		if err != nil {
			t.Fatalf("upsert cluster_nodes(%s): %v", id, err)
		}
	}

	// Synchronized control-plane node + lagging data-plane node must block KEK retirement (409).
	insertNode("cp-node-1", "control-plane", currentEpoch, nowMs, "ok")
	insertNode("dp-node-1", "data-plane", currentEpoch-1, nowMs, "ok")

	recLaggingStatus := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/ops/cluster-status", ownerCookie, "", "", nil)
	if recLaggingStatus.Code != http.StatusOK || !strings.Contains(recLaggingStatus.Body.String(), `"lagging"`) {
		t.Fatalf("DEF-072 expected cluster-status to report lagging node, got %d body=%s", recLaggingStatus.Code, recLaggingStatus.Body.String())
	}

	recRetireLagging := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/ops/keys/retire", ownerCookie, boot.CSRFToken, "", map[string]any{
		"key_id": "kek_v1",
	})
	if recRetireLagging.Code != http.StatusConflict || !strings.Contains(recRetireLagging.Body.String(), "cluster_node_sync_incomplete") {
		t.Fatalf("DEF-072 expected 409 cluster_node_sync_incomplete while dp-node-1 lags, got %d body=%s", recRetireLagging.Code, recRetireLagging.Body.String())
	}

	// Stale node heartbeat (> 15s old) must also block KEK retirement and surface stale=true.
	insertNode("dp-node-1", "data-plane", currentEpoch, nowMs-60000, "ok")
	recStaleStatus := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/ops/cluster-status", ownerCookie, "", "", nil)
	if recStaleStatus.Code != http.StatusOK || !strings.Contains(recStaleStatus.Body.String(), `"stale":true`) {
		t.Fatalf("DEF-072 expected cluster-status to surface stale=true, got %d body=%s", recStaleStatus.Code, recStaleStatus.Body.String())
	}
	recRetireStale := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/ops/keys/retire", ownerCookie, boot.CSRFToken, "", map[string]any{
		"key_id": "kek_v1",
	})
	if recRetireStale.Code != http.StatusConflict || !strings.Contains(recRetireStale.Body.String(), "cluster_node_sync_incomplete") {
		t.Fatalf("DEF-072 expected 409 cluster_node_sync_incomplete while dp-node-1 stale, got %d body=%s", recRetireStale.Code, recRetireStale.Body.String())
	}

	// Once all nodes are synchronized and healthy, retiring kek_v1 succeeds while kek_v2 remains active.
	insertNode("dp-node-1", "data-plane", currentEpoch, nowMs, "ok")
	recRetireOK := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/ops/keys/retire", ownerCookie, boot.CSRFToken, "", map[string]any{
		"key_id": "kek_v1",
	})
	if recRetireOK.Code != http.StatusOK {
		t.Fatalf("DEF-072 expected 200 retiring kek_v1 after 100%% node sync, got %d body=%s", recRetireOK.Code, recRetireOK.Body.String())
	}
	var retireResp struct {
		RetiredKeyID  string   `json:"retired_key_id"`
		VerifiedNodes []string `json:"verified_nodes"`
	}
	mustDecodeJSON(t, recRetireOK.Body.Bytes(), &retireResp)
	if retireResp.RetiredKeyID != "kek_v1" || len(retireResp.VerifiedNodes) != 2 {
		t.Fatalf("unexpected retire response: %+v", retireResp)
	}
	loaded := strings.Join(h.vault.LoadedKeyIDs(ctx), ",")
	if strings.Contains(loaded, "kek_v1") || !strings.Contains(loaded, "kek_v2") {
		t.Fatalf("expected kek_v1 retired and kek_v2 active, loaded=%q", loaded)
	}
}

func TestDEF054_055_056_057_Round4RBACModelsAndDisplayNames(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "Round4 Slice E Org", "r4-slice-e-proj")
	ctx := context.Background()

	// Create sibling project + second org for strict scoping & anti-oracle checks.
	sibPrj, err := h.st.Tenancy().CreateProject(ctx, boot.OrgID, port.CreateProjectParams{
		Slug:        "r4-sibling-proj",
		DisplayName: "Round4 Sibling Project",
	})
	if err != nil {
		t.Fatalf("CreateProject sibling: %v", err)
	}
	otherOrg, err := h.st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		Slug:        "r4-other-org",
		DisplayName: "Round4 Other Org",
	})
	if err != nil {
		t.Fatalf("CreateOrganization other: %v", err)
	}

	// 1. DEF-057: Environment display_name must survive POST + GET list (plus legacy blank fallback to slug).
	recEnvCreate := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects/"+boot.ProjectID+"/environments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "prod-eu",
		"name": "EU Production Edge",
	})
	if recEnvCreate.Code != http.StatusCreated {
		t.Fatalf("DEF-057 POST environment code=%d body=%s", recEnvCreate.Code, recEnvCreate.Body.String())
	}
	var createdEnv environmentDTO
	mustDecodeJSON(t, recEnvCreate.Body.Bytes(), &createdEnv)
	if createdEnv.Name != "EU Production Edge" || createdEnv.Slug != "prod-eu" {
		t.Fatalf("DEF-057 POST expected name='EU Production Edge', got %+v", createdEnv)
	}
	recEnvList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/projects/"+boot.ProjectID+"/environments", ownerCookie, "", "", nil)
	if recEnvList.Code != http.StatusOK {
		t.Fatalf("DEF-057 GET environments code=%d body=%s", recEnvList.Code, recEnvList.Body.String())
	}
	var envList struct {
		Data []environmentDTO `json:"data"`
	}
	mustDecodeJSON(t, recEnvList.Body.Bytes(), &envList)
	foundCustomEnv := false
	for _, e := range envList.Data {
		if e.ID == createdEnv.ID {
			foundCustomEnv = true
			if e.Name != "EU Production Edge" {
				t.Fatalf("DEF-057 GET lost persisted environment display name: got %q", e.Name)
			}
		}
	}
	if !foundCustomEnv {
		t.Fatalf("DEF-057 created environment %s missing from list", createdEnv.ID)
	}

	// Legacy environment row with blank display_name must fall back cleanly to slug.
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE environments SET display_name = '' WHERE id = ?`, boot.EnvID); err != nil {
		t.Fatalf("seed legacy blank env display_name: %v", err)
	}
	recEnvLegacy := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/projects/"+boot.ProjectID+"/environments", ownerCookie, "", "", nil)
	mustDecodeJSON(t, recEnvLegacy.Body.Bytes(), &envList)
	for _, e := range envList.Data {
		if e.ID == boot.EnvID && e.Name != e.Slug {
			t.Fatalf("DEF-057 legacy blank display_name expected slug fallback %q, got %q", e.Slug, e.Name)
		}
	}

	// 2. DEF-055: Logical model display_name must survive POST + GET list (with legacy fallback).
	recModCreate := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/models", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":              "r4-custom-model",
		"display_name":      "Round4 Enterprise Reasoning Model",
		"context_window":    65536,
		"max_output_tokens": 8192,
		"assurance_tier":    "DECLARED",
	})
	if recModCreate.Code != http.StatusCreated {
		t.Fatalf("DEF-055 POST /models code=%d body=%s", recModCreate.Code, recModCreate.Body.String())
	}
	var createdModel logicalModelDTO
	mustDecodeJSON(t, recModCreate.Body.Bytes(), &createdModel)
	if createdModel.DisplayName != "Round4 Enterprise Reasoning Model" {
		t.Fatalf("DEF-055 POST expected display_name preserved, got %+v", createdModel)
	}
	recModList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/models", ownerCookie, "", "", nil)
	if recModList.Code != http.StatusOK {
		t.Fatalf("DEF-055 GET /models code=%d body=%s", recModList.Code, recModList.Body.String())
	}
	var modList struct {
		Data []logicalModelDTO `json:"data"`
	}
	mustDecodeJSON(t, recModList.Body.Bytes(), &modList)
	foundCustomModel := false
	for _, m := range modList.Data {
		if m.ID == createdModel.ID {
			foundCustomModel = true
			if m.DisplayName != "Round4 Enterprise Reasoning Model" {
				t.Fatalf("DEF-055 GET lost persisted model display_name: got %q", m.DisplayName)
			}
		}
	}
	if !foundCustomModel {
		t.Fatalf("DEF-055 created model %s missing from list", createdModel.ID)
	}

	// 3. DEF-056 + DEF-054: RBAC enforcement across TEAM_ADMIN, DEVELOPER, FINOPS_VIEWER, SECURITY_REVIEWER, cross-org anti-oracle.
	h.registerBearerToken("tok_ta_r4", &rbac.Actor{
		PrincipalID:     "prc_ta_r4",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleTeamAdmin}},
	})
	h.registerBearerToken("tok_dev_r4", &rbac.Actor{
		PrincipalID:     "prc_dev_r4",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleDeveloper}},
	})
	h.registerBearerToken("tok_finops_r4", &rbac.Actor{
		PrincipalID:     "prc_finops_r4",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleFinOpsViewer}},
	})
	h.registerBearerToken("tok_sec_r4", &rbac.Actor{
		PrincipalID:     "prc_sec_r4",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleSecurityReviewer}},
	})
	h.registerBearerToken("tok_other_org_r4", &rbac.Actor{
		PrincipalID:     "prc_other_r4",
		SessionOrgID:    &otherOrg.ID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &otherOrg.ID, ScopeType: rbac.ScopeOrg, ScopeID: otherOrg.ID, RoleName: rbac.RoleOrgOwner}},
	})

	// TEAM_ADMIN succeeds on bound project_id AND omitted project_id, fails 403 on sibling project.
	recTAOwn := h.doBearer(t, http.MethodPost, "/admin/v1/models", "tok_ta_r4", "", map[string]any{
		"slug":              "ta-bound-model",
		"display_name":      "Team Admin Bound Model",
		"project_id":        boot.ProjectID,
		"context_window":    16384,
		"max_output_tokens": 4096,
	})
	if recTAOwn.Code != http.StatusCreated {
		t.Fatalf("DEF-056 TEAM_ADMIN POST /models with bound project_id expected 201, got %d body=%s", recTAOwn.Code, recTAOwn.Body.String())
	}
	recTAOmit := h.doBearer(t, http.MethodPost, "/admin/v1/models", "tok_ta_r4", "", map[string]any{
		"slug":              "ta-omitted-model",
		"display_name":      "Team Admin Default Project Model",
		"context_window":    16384,
		"max_output_tokens": 4096,
	})
	if recTAOmit.Code != http.StatusCreated {
		t.Fatalf("DEF-056 TEAM_ADMIN POST /models omitting project_id expected 201, got %d body=%s", recTAOmit.Code, recTAOmit.Body.String())
	}
	recTASib := h.doBearer(t, http.MethodPost, "/admin/v1/models", "tok_ta_r4", "", map[string]any{
		"slug":              "ta-sibling-denied",
		"display_name":      "Sibling Denied Model",
		"project_id":        sibPrj.ID,
		"context_window":    16384,
		"max_output_tokens": 4096,
	})
	if recTASib.Code != http.StatusForbidden {
		t.Fatalf("DEF-056 TEAM_ADMIN POST /models sibling project expected 403, got %d body=%s", recTASib.Code, recTASib.Body.String())
	}

	// Cross-org actor targeting Org A's project gets uniform 404 anti-oracle (note tok_other_org has SessionOrgID=otherOrg; probe via ?org_id=boot.OrgID).
	recCrossOrg := h.doBearer(t, http.MethodPost, "/admin/v1/models?org_id="+boot.OrgID, "tok_other_org_r4", "", map[string]any{
		"org_id":            boot.OrgID,
		"slug":              "cross-org-probe",
		"display_name":      "Cross Org",
		"project_id":        boot.ProjectID,
		"context_window":    16384,
		"max_output_tokens": 4096,
	})
	if recCrossOrg.Code != http.StatusForbidden && recCrossOrg.Code != http.StatusNotFound {
		t.Fatalf("DEF-056 cross-org probe expected 403/404, got %d body=%s", recCrossOrg.Code, recCrossOrg.Body.String())
	}

	// TEAM_ADMIN secrets scoped to project_id returns 200 (vs unscoped 403).
	if rec := h.doBearer(t, http.MethodGet, "/admin/v1/secrets?project_id="+boot.ProjectID, "tok_ta_r4", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("DEF-054 TEAM_ADMIN GET /secrets?project_id expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := h.doBearer(t, http.MethodGet, "/admin/v1/secrets", "tok_ta_r4", "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("DEF-054 TEAM_ADMIN unscoped GET /secrets expected 403, got %d", rec.Code)
	}

	// DEVELOPER can read models/routes/project-policy, cannot write models.
	if rec := h.doBearer(t, http.MethodGet, "/admin/v1/models", "tok_dev_r4", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("DEVELOPER GET /models expected 200, got %d", rec.Code)
	}
	if rec := h.doBearer(t, http.MethodGet, "/admin/v1/policies/effective?project_id="+boot.ProjectID, "tok_dev_r4", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("DEVELOPER GET /policies/effective?project_id expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := h.doBearer(t, http.MethodPost, "/admin/v1/models", "tok_dev_r4", "", map[string]any{
		"slug": "dev-denied", "display_name": "Dev Denied", "project_id": boot.ProjectID, "context_window": 4096, "max_output_tokens": 1024,
	}); rec.Code != http.StatusForbidden {
		t.Fatalf("DEVELOPER POST /models expected 403, got %d", rec.Code)
	}

	// FINOPS_VIEWER and SECURITY_REVIEWER cannot mutate models.
	for _, tok := range []string{"tok_finops_r4", "tok_sec_r4"} {
		if rec := h.doBearer(t, http.MethodPost, "/admin/v1/models", tok, "", map[string]any{
			"slug": "ro-denied-" + tok, "display_name": "RO Denied", "project_id": boot.ProjectID, "context_window": 4096, "max_output_tokens": 1024,
		}); rec.Code != http.StatusForbidden {
			t.Fatalf("read-only role %s POST /models expected 403, got %d", tok, rec.Code)
		}
	}
}

func TestAuditRound4_UnquarantineOrgSwitchAndConnectionSecretKinds(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "Audit Round4 Org", "audit-r4-proj")

	// 1. Seed extension, approve + enable, trigger org-scoped EXTENSION kill switch, quarantine, then unquarantine.
	v1URI := "https://marketplace.tanpopo.dev/acme-audit-r4.tpkg"
	v1SHA := h.registerSignedProviderTPKG(t, v1URI, "@acme/audit-r4", "1.0.0", []string{"api.openai.com"}, false, true)
	recStage := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri": v1URI, "expected_sha256": v1SHA,
	})
	if recStage.Code != http.StatusAccepted {
		t.Fatalf("stage code=%d body=%s", recStage.Code, recStage.Body.String())
	}
	var stResp struct {
		InstallationID string `json:"installation_id"`
	}
	mustDecodeJSON(t, recStage.Body.Bytes(), &stResp)
	if rec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+stResp.InstallationID+"/approve-permissions", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"enable_immediately": true, "ack_unverified": false,
	}); rec.Code != http.StatusOK {
		t.Fatalf("approve code=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst extensionInstallationDTO
	_ = h.doAuthedCookie(t, http.MethodGet, "/admin/v1/extensions/installations", ownerCookie, "", "", nil)
	recQuar := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+stResp.InstallationID+"/quarantine", ownerCookie, boot.CSRFToken, `"v2"`, map[string]any{
		"reason": "quarantine test",
	})
	if recQuar.Code != http.StatusOK {
		t.Fatalf("quarantine code=%d body=%s", recQuar.Code, recQuar.Body.String())
	}
	mustDecodeJSON(t, recQuar.Body.Bytes(), &inst)

	// Trigger an org-scoped EXTENSION kill-switch (as happens for non-operator or org-targeted switches).
	orgScopedID := boot.OrgID
	if _, err := h.killSwitch.Trigger(context.Background(), killswitch.TriggerParams{
		OrgID:                  &orgScopedID,
		Level:                  killswitch.LevelExtensionInstallation,
		TargetID:               stResp.InstallationID,
		Reason:                 "org extension freeze",
		TriggeredByPrincipalID: boot.OwnerPrincipalID,
	}); err != nil {
		t.Fatalf("trigger org-scoped EXTENSION kill switch: %v", err)
	}

	var resetCalledFor string
	h.srv.onExtensionUnquarantined = func(id string) { resetCalledFor = id }

	recUnq := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+stResp.InstallationID+"/unquarantine", ownerCookie, boot.CSRFToken, FormatETag(inst.Version), map[string]any{
		"reason": "verified clean recovery",
	})
	if recUnq.Code != http.StatusOK {
		t.Fatalf("unquarantine code=%d body=%s", recUnq.Code, recUnq.Body.String())
	}
	if resetCalledFor != stResp.InstallationID {
		t.Fatalf("expected onExtensionUnquarantined hook for %q, got %q", stResp.InstallationID, resetCalledFor)
	}
	activeSwitches, err := h.killSwitch.ListWithFilter(context.Background(), killswitch.ListFilter{AllowInstanceScope: true, ActiveOnly: true})
	if err != nil {
		t.Fatalf("list switches: %v", err)
	}
	for _, sw := range activeSwitches {
		if sw.TargetID == stResp.InstallationID {
			t.Fatalf("expected org-scoped EXTENSION switch to be cleared on unquarantine, found active switch %+v", sw)
		}
	}

	// 2. Reject non-provider secret kinds (WEBHOOK_HMAC_SECRET, OCI_REGISTRY_AUTH) on provider connection create/update.
	adpID := seedLocalOpenAIAdapter(t, h, "api.openai.com")
	for _, kind := range []string{"WEBHOOK_HMAC_SECRET", "OCI_REGISTRY_AUTH"} {
		recSec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
			"name": "non-prov-" + kind, "secret_kind": kind, "secret_value": "secret-val-123456",
		})
		if recSec.Code != http.StatusCreated {
			t.Fatalf("create secret %s code=%d body=%s", kind, recSec.Code, recSec.Body.String())
		}
		var sec secretMetadataDTO
		mustDecodeJSON(t, recSec.Body.Bytes(), &sec)

		recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
			"name": "bad-kind-" + kind, "adapter_id": adpID, "billing_mode": "COMMERCIAL_API",
			"endpoint_url": "https://api.openai.com/v1", "data_region": "US",
			"secret_bindings": map[string]string{"api_key": sec.CredentialID},
		})
		if recConn.Code != http.StatusBadRequest || !strings.Contains(recConn.Body.String(), "credential_kind_mismatch") {
			t.Fatalf("expected 400 credential_kind_mismatch binding %s to connection, got %d body=%s", kind, recConn.Code, recConn.Body.String())
		}
	}
}


