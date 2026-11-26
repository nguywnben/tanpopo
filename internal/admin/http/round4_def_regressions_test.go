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
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
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

