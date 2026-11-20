// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPlaygroundSessionCreatesExpiringBudgetCappedKey(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	h.fk.Set(time.Date(2026, time.November, 30, 23, 58, 0, 0, time.UTC))
	boot, cookie := h.bootstrapOwner(t, "Playground Org", "playground-project")
	createdAt := h.fk.Now()
	path := "/admin/v1/playground/sessions"
	body := map[string]any{"project_id": boot.ProjectID, "env_id": boot.EnvID, "ttl_seconds": 300, "max_budget_micros": 125000}
	if got := h.doAuthedCookie(t, http.MethodPost, path, cookie, "", "", body); got.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d body=%s", got.Code, got.Body.String())
	}
	created := h.doAuthedCookie(t, http.MethodPost, path, cookie, boot.CSRFToken, "", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var result playgroundSessionResponse
	mustDecodeJSON(t, created.Body.Bytes(), &result)
	if result.SessionID == "" || !strings.HasPrefix(result.EphemeralVirtualKey, "vk_tp_") || result.EnvID != boot.EnvID || result.MaxBudgetMicros != 125000 {
		t.Fatalf("unexpected playground session response: %+v", result)
	}
	expiresAt, err := time.Parse(time.RFC3339, result.ExpiresAt)
	if err != nil || expiresAt.UnixMilli() != h.fk.Now().Add(5*time.Minute).UnixMilli() {
		t.Fatalf("unexpected expiration %q err=%v", result.ExpiresAt, err)
	}
	var ephemeral int
	var keyExpiry int64
	if err := h.st.RawDB().QueryRow(`SELECT is_ephemeral_playground,expires_at_ms FROM virtual_keys WHERE org_id=? AND id=?`, boot.OrgID, result.SessionID).Scan(&ephemeral, &keyExpiry); err != nil {
		t.Fatal(err)
	}
	if ephemeral != 1 || keyExpiry != expiresAt.UnixMilli() {
		t.Fatalf("persisted ephemeral=%d expires=%d want=%d", ephemeral, keyExpiry, expiresAt.UnixMilli())
	}
	var kind string
	var startMs, endMs, ceiling int64
	if err := h.st.RawDB().QueryRow(`SELECT period_kind,period_start_ms,period_end_ms,hard_limit_micros FROM budget_ledgers WHERE org_id=? AND scope_type='VIRTUAL_KEY' AND scope_id=?`, boot.OrgID, result.SessionID).Scan(&kind, &startMs, &endMs, &ceiling); err != nil {
		t.Fatal(err)
	}
	if kind != "TOTAL_LIFETIME" || startMs != createdAt.UnixMilli() || endMs != expiresAt.UnixMilli() || !time.UnixMilli(endMs).After(time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)) || ceiling != 125000 {
		t.Fatalf("playground key ledger kind=%s period=[%d,%d) ceiling=%d; want TOTAL_LIFETIME [%d,%d) with rollover coverage and ceiling=125000", kind, startMs, endMs, ceiling, createdAt.UnixMilli(), expiresAt.UnixMilli())
	}
}

func TestPlaygroundSessionRejectsInvalidBoundsAndCrossProjectEnvironment(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, cookie := h.bootstrapOwner(t, "Playground Bounds Org", "playground-project")
	path := "/admin/v1/playground/sessions"
	for _, body := range []map[string]any{
		{"project_id": boot.ProjectID, "env_id": boot.EnvID, "ttl_seconds": 59},
		{"project_id": boot.ProjectID, "env_id": boot.EnvID, "max_budget_micros": 999},
		{"project_id": boot.ProjectID, "env_id": "env_missing"},
	} {
		got := h.doAuthedCookie(t, http.MethodPost, path, cookie, boot.CSRFToken, "", body)
		if got.Code != http.StatusBadRequest && got.Code != http.StatusNotFound {
			t.Fatalf("invalid request status=%d body=%s payload=%v", got.Code, got.Body.String(), body)
		}
	}
}
