// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/builtin"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func TestDEF081_DEF082_PostgresQuarantineStreamBarrierRecoveryAndDenials(t *testing.T) {
	h := newPGAdminHarness(t)
	if h == nil {
		return
	}
	h.srv.installer.SetSyncAdaptersTx(h.srv.catalog.SyncExtensionProviderAdaptersTx)
	boot, ownerCookie := h.bootstrap(t)
	ctx := context.Background()

	// Helper to install & activate a built-in extension package by canonical package ID.
	installAndActivateBuiltin := func(packageID, expectedAdapterSlug string) extensionInstallationDTO {
		t.Helper()
		var pkg *builtin.Package
		for _, p := range h.builtins.Packages() {
			if p.PackageID == packageID {
				cp := p
				pkg = &cp
				break
			}
		}
		if pkg == nil {
			t.Fatalf("missing builtin %s", packageID)
		}
		recStage := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
			"source_uri":      pkg.SourceURI,
			"expected_sha256": pkg.ArchiveSHA256,
		})
		if recStage.Code != http.StatusAccepted {
			t.Fatalf("stage %s code=%d body=%s", packageID, recStage.Code, recStage.Body.String())
		}
		var stageResp struct {
			InstallationID string `json:"installation_id"`
		}
		mustDecodeJSON(t, recStage.Body.Bytes(), &stageResp)

		recApp := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+stageResp.InstallationID+"/approve-permissions", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
			"enable_immediately": true,
			"acknowledge_unverified_external":     false,
		})
		if recApp.Code != http.StatusOK {
			t.Fatalf("approve %s code=%d body=%s", packageID, recApp.Code, recApp.Body.String())
		}
		var out extensionInstallationDTO
		mustDecodeJSON(t, recApp.Body.Bytes(), &out)
		if out.State != "ENABLED_ACTIVE" {
			t.Fatalf("expected %s state=ENABLED_ACTIVE, got %s", packageID, out.State)
		}
		recAdapters := h.doCookie(t, http.MethodGet, "/admin/v1/provider-adapters", ownerCookie, "", "", nil)
		if recAdapters.Code != http.StatusOK || !strings.Contains(recAdapters.Body.String(), `"`+expectedAdapterSlug+`"`) {
			t.Fatalf("expected adapter %q registered in Postgres, code=%d body=%s", expectedAdapterSlug, recAdapters.Code, recAdapters.Body.String())
		}
		return out
	}

	// 1. Fresh extension installation + activation + Postgres adapter registration for both OpenAI-compatible & Ollama.
	openAIIns := installAndActivateBuiltin("@tanpopo/openai-compatible", "openai-compatible")
	ollamaIns := installAndActivateBuiltin("@tanpopo/ollama", "ollama")

	// 2. Register in-flight SSE streams on KillSwitch StreamRegistry for both extensions.
	openAIStreamCtx, openAIStreamCancel := context.WithCancel(ctx)
	defer openAIStreamCancel()
	ollamaStreamCtx, ollamaStreamCancel := context.WithCancel(ctx)
	defer ollamaStreamCancel()
	hOpenAIStream := h.srv.killSwitch.RegisterStream(killswitch.StreamRegistration{
		OrgID:           boot.OrgID,
		ProjectID:       boot.ProjectID,
		PluginInstallID: openAIIns.ID,
		Cancel:          openAIStreamCancel,
	})
	defer h.srv.killSwitch.UnregisterStream(hOpenAIStream)
	hOllamaStream := h.srv.killSwitch.RegisterStream(killswitch.StreamRegistration{
		OrgID:           boot.OrgID,
		ProjectID:       boot.ProjectID,
		PluginInstallID: ollamaIns.ID,
		Cancel:          ollamaStreamCancel,
	})
	defer h.srv.killSwitch.UnregisterStream(hOllamaStream)

	// 3. Negative RBAC check BEFORE quarantine: enroll a FINOPS_VIEWER member and confirm HTTP 403 fail-closed.
	recMember := h.doCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email": "viewer-r6@pg-test.local",
		"roles": []string{"FINOPS_VIEWER"},
	})
	if recMember.Code != http.StatusCreated {
		t.Fatalf("create viewer member code=%d body=%s", recMember.Code, recMember.Body.String())
	}
	var memberResp struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, recMember.Body.Bytes(), &memberResp)
	recTok := h.doCookie(t, http.MethodPost, "/admin/v1/members/"+memberResp.ID+"/enrollment-token", ownerCookie, boot.CSRFToken, "", map[string]any{})
	if recTok.Code != http.StatusCreated {
		t.Fatalf("enrollment token code=%d body=%s", recTok.Code, recTok.Body.String())
	}
	var tokResp struct {
		EnrollmentToken string `json:"enrollment_token"`
	}
	mustDecodeJSON(t, recTok.Body.Bytes(), &tokResp)
	recEnroll := h.doCookie(t, http.MethodPost, "/admin/v1/auth/enroll", nil, "", "", map[string]any{
		"token":    tokResp.EnrollmentToken,
		"password": "ViewerStrong#2026!",
	})
	if recEnroll.Code != http.StatusOK {
		t.Fatalf("enroll viewer code=%d body=%s", recEnroll.Code, recEnroll.Body.String())
	}
	recLoginViewer := h.doCookie(t, http.MethodPost, "/admin/v1/auth/login", nil, "", "", map[string]any{
		"email":    "viewer-r6@pg-test.local",
		"password": "ViewerStrong#2026!",
	})
	if recLoginViewer.Code != http.StatusOK {
		t.Fatalf("viewer login code=%d body=%s", recLoginViewer.Code, recLoginViewer.Body.String())
	}
	var viewerLoginResp struct {
		CSRFToken string `json:"csrf_token"`
	}
	mustDecodeJSON(t, recLoginViewer.Body.Bytes(), &viewerLoginResp)
	viewerCookie := findCookie(t, recLoginViewer.Result().Cookies(), "__Host-tanpopo_admin_session")

	for _, ins := range []extensionInstallationDTO{openAIIns, ollamaIns} {
		recDenied := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+ins.ID+"/quarantine", viewerCookie, viewerLoginResp.CSRFToken, FormatETag(ins.Version), map[string]any{
			"reason": "unauthorized viewer quarantine attempt",
		})
		if recDenied.Code != http.StatusForbidden {
			t.Fatalf("expected viewer quarantine to fail 403, got %d body=%s", recDenied.Code, recDenied.Body.String())
		}
	}

	// 4. Legitimate bootstrap personal operator quarantines BOTH OpenAI-compatible AND Ollama -> HTTP 200,
	//    immediate emergency stream termination, CheckAdmission block, and Kill-Switch inventory consistency.
	quarantineAndVerify := func(ins extensionInstallationDTO, streamCtx context.Context, label string) extensionInstallationDTO {
		t.Helper()
		recQ := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+ins.ID+"/quarantine", ownerCookie, boot.CSRFToken, FormatETag(ins.Version), map[string]any{
			"reason": "CP125 verified quarantine " + label,
		})
		if recQ.Code != http.StatusOK {
			t.Fatalf("quarantine %s failed code=%d body=%s", label, recQ.Code, recQ.Body.String())
		}
		var out extensionInstallationDTO
		mustDecodeJSON(t, recQ.Body.Bytes(), &out)
		if out.State != "QUARANTINED" {
			t.Fatalf("expected %s state=QUARANTINED, got %s", label, out.State)
		}
		select {
		case <-streamCtx.Done():
		case <-time.After(time.Second):
			t.Fatalf("expected live stream for %s to be terminated on quarantine", label)
		}
		if _, blocked := h.srv.killSwitch.CheckAdmission(killswitch.AdmissionQuery{
			OrgID:           boot.OrgID,
			PluginInstallID: ins.ID,
		}); !blocked {
			t.Fatalf("expected Data Plane admission blocked for quarantined %s", label)
		}
		return out
	}

	qOpenAI := quarantineAndVerify(openAIIns, openAIStreamCtx, "openai-compatible")
	qOllama := quarantineAndVerify(ollamaIns, ollamaStreamCtx, "ollama")

	// Kill-switch inventory shows exactly 2 active EXTENSION_INSTALLATION switches.
	recKSList := h.doCookie(t, http.MethodGet, "/admin/v1/kill-switches?active_only=true", ownerCookie, "", "", nil)
	if recKSList.Code != http.StatusOK {
		t.Fatalf("list kill switches code=%d body=%s", recKSList.Code, recKSList.Body.String())
	}
	var ksList struct {
		Data []killswitch.KillSwitch `json:"data"`
	}
	mustDecodeJSON(t, recKSList.Body.Bytes(), &ksList)
	if len(ksList.Data) != 2 {
		t.Fatalf("expected 2 active kill switches after dual quarantine, got %d (%+v)", len(ksList.Data), ksList.Data)
	}

	// Non-operator ORG_OWNER denied in multi-tenant orgCount > 1 on quarantine/approve:
	// Create org2 so cluster has 2 orgs and enroll an org1-only ORG_OWNER without INSTANCE OPERATOR.
	org2, err := h.st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "r6-org2", DisplayName: "R6 Org 2"})
	if err != nil {
		t.Fatal(err)
	}
	_ = org2
	recOwner2 := h.doCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email": "orgonly-owner@pg-test.local",
		"roles": []string{"ORG_OWNER"},
	})
	if recOwner2.Code != http.StatusCreated {
		t.Fatalf("create orgonly owner code=%d body=%s", recOwner2.Code, recOwner2.Body.String())
	}
	var o2Resp struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, recOwner2.Body.Bytes(), &o2Resp)
	recO2Tok := h.doCookie(t, http.MethodPost, "/admin/v1/members/"+o2Resp.ID+"/enrollment-token", ownerCookie, boot.CSRFToken, "", map[string]any{})
	if recO2Tok.Code != http.StatusCreated {
		t.Fatalf("orgonly owner enrollment token code=%d body=%s", recO2Tok.Code, recO2Tok.Body.String())
	}
	var o2Tok struct {
		EnrollmentToken string `json:"enrollment_token"`
	}
	mustDecodeJSON(t, recO2Tok.Body.Bytes(), &o2Tok)
	recO2Enroll := h.doCookie(t, http.MethodPost, "/admin/v1/auth/enroll", nil, "", "", map[string]any{
		"token":    o2Tok.EnrollmentToken,
		"password": "OrgOnlyStrong#2026!",
	})
	if recO2Enroll.Code != http.StatusOK {
		t.Fatalf("enroll orgonly owner code=%d body=%s", recO2Enroll.Code, recO2Enroll.Body.String())
	}
	recLoginOrgOnly := h.doCookie(t, http.MethodPost, "/admin/v1/auth/login", nil, "", "", map[string]any{
		"email":    "orgonly-owner@pg-test.local",
		"password": "OrgOnlyStrong#2026!",
	})
	if recLoginOrgOnly.Code != http.StatusOK {
		t.Fatalf("login orgonly owner code=%d body=%s", recLoginOrgOnly.Code, recLoginOrgOnly.Body.String())
	}
	var o2Login struct {
		CSRFToken string `json:"csrf_token"`
	}
	mustDecodeJSON(t, recLoginOrgOnly.Body.Bytes(), &o2Login)
	o2Cookie := findCookie(t, recLoginOrgOnly.Result().Cookies(), "__Host-tanpopo_admin_session")

	// Multi-tenant non-operator ORG_OWNER cannot unquarantine -> HTTP 403; cannot quarantine -> HTTP 403.
	recDeniedUnq := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+qOpenAI.ID+"/unquarantine", o2Cookie, o2Login.CSRFToken, FormatETag(qOpenAI.Version), map[string]any{
		"reason": "unauthorized org-only unquarantine attempt",
	})
	if recDeniedUnq.Code != http.StatusForbidden {
		t.Fatalf("expected non-operator multi-org unquarantine = 403, got %d body=%s", recDeniedUnq.Code, recDeniedUnq.Body.String())
	}

	// 5. Bootstrapped personal operator unquarantines BOTH -> state=INSTALLED_DISABLED, kill switches cleared!
	unquarantineAndVerify := func(ins extensionInstallationDTO, label string) extensionInstallationDTO {
		t.Helper()
		recU := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+ins.ID+"/unquarantine", ownerCookie, boot.CSRFToken, FormatETag(ins.Version), map[string]any{
			"reason": "verified clean recovery " + label,
		})
		if recU.Code != http.StatusOK {
			t.Fatalf("unquarantine %s code=%d body=%s", label, recU.Code, recU.Body.String())
		}
		var out extensionInstallationDTO
		mustDecodeJSON(t, recU.Body.Bytes(), &out)
		if out.State != "INSTALLED_DISABLED" {
			t.Fatalf("expected %s state=INSTALLED_DISABLED, got %s", label, out.State)
		}
		if _, blocked := h.srv.killSwitch.CheckAdmission(killswitch.AdmissionQuery{
			OrgID:           boot.OrgID,
			PluginInstallID: ins.ID,
		}); blocked {
			t.Fatalf("expected Data Plane admission unblocked after unquarantine of %s", label)
		}
		return out
	}
	uOpenAI := unquarantineAndVerify(qOpenAI, "openai-compatible")
	uOllama := unquarantineAndVerify(qOllama, "ollama")

	recKSListAfter := h.doCookie(t, http.MethodGet, "/admin/v1/kill-switches?active_only=true", ownerCookie, "", "", nil)
	if recKSListAfter.Code != http.StatusOK {
		t.Fatalf("list kill switches after unquarantine code=%d body=%s", recKSListAfter.Code, recKSListAfter.Body.String())
	}
	var ksListAfter struct {
		Data []killswitch.KillSwitch `json:"data"`
	}
	mustDecodeJSON(t, recKSListAfter.Body.Bytes(), &ksListAfter)
	if len(ksListAfter.Data) != 0 {
		t.Fatalf("expected 0 active kill switches after unquarantine, got %d", len(ksListAfter.Data))
	}

	// 6. Transaction failure rollback on re-enable, then clean re-enable under multi-org!
	h.srv.installer.SetSyncAdaptersTx(func(context.Context, *sql.Tx, string, int64) error {
		return errors.New("injected pg adapter sync failure during re-enable")
	})
	recReFail := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+uOllama.ID+"/approve-permissions", ownerCookie, boot.CSRFToken, FormatETag(uOllama.Version), map[string]any{
		"enable_immediately": true,
		"acknowledge_unverified_external":     false,
	})
	if recReFail.Code == http.StatusOK {
		t.Fatalf("expected injected sync failure on re-enable to fail")
	}
	stillDisabled, _, err := h.srv.loadExtensionInstallationDTO(ctx, uOllama.ID)
	if err != nil || stillDisabled.State != "INSTALLED_DISABLED" {
		t.Fatalf("expected atomic rollback to keep INSTALLED_DISABLED, got (%+v, %v)", stillDisabled, err)
	}

	// Restore sync hook and re-enable both under multi-org!
	h.srv.installer.SetSyncAdaptersTx(h.srv.catalog.SyncExtensionProviderAdaptersTx)
	for _, ins := range []extensionInstallationDTO{uOpenAI, stillDisabled} {
		recReOK := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+ins.ID+"/approve-permissions", ownerCookie, boot.CSRFToken, FormatETag(ins.Version), map[string]any{
			"enable_immediately": true,
			"acknowledge_unverified_external":     false,
		})
		if recReOK.Code != http.StatusOK {
			t.Fatalf("multi-org re-enable %s code=%d body=%s", ins.ID, recReOK.Code, recReOK.Body.String())
		}
		var out extensionInstallationDTO
		mustDecodeJSON(t, recReOK.Body.Bytes(), &out)
		if out.State != "ENABLED_ACTIVE" {
			t.Fatalf("expected %s state=ENABLED_ACTIVE after re-enable, got %s", ins.ID, out.State)
		}
	}

	// 7. Verify audit trail consistency across operator (org_id IS NULL) and org-scoped audit partitions!
	eventCounts := map[string]int{}
	tx, err := h.st.RawDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	collectAudit := func() {
		rows, qerr := tx.QueryContext(ctx, `SELECT event_type FROM audit_events`)
		if qerr != nil {
			t.Fatal(qerr)
		}
		defer rows.Close()
		for rows.Next() {
			var et string
			if serr := rows.Scan(&et); serr != nil {
				t.Fatal(serr)
			}
			eventCounts[et]++
		}
		if rerr := rows.Err(); rerr != nil {
			t.Fatal(rerr)
		}
	}
	if err := port.SetOperatorRLSTx(ctx, tx, port.DialectPostgres); err != nil {
		t.Fatal(err)
	}
	collectAudit()
	if err := port.SetOrgRLSTx(ctx, tx, port.DialectPostgres, boot.OrgID); err != nil {
		t.Fatal(err)
	}
	collectAudit()
	for _, et := range []string{
		"kill_switch.triggered",
		"kill_switch.cleared",
		"extension.quarantined",
		"extension.unquarantined",
		"extension.enabled",
	} {
		if eventCounts[et] < 2 {
			t.Fatalf("expected >= 2 audit events for %q after dual quarantine/unquarantine/re-enable, got %d (%+v)", et, eventCounts[et], eventCounts)
		}
	}
}

// Verify that countKillSwitchAffectedEntities aggregates across multiple orgs under FORCE RLS cleanly on PG16
// and restricts to the target org when instanceWide=false.
func TestDEF081_PostgresKillSwitchAffectedEntitiesCountAcrossOrgs(t *testing.T) {
	h := newPGAdminHarness(t)
	if h == nil {
		return
	}
	h.srv.installer.SetSyncAdaptersTx(h.srv.catalog.SyncExtensionProviderAdaptersTx)
	boot, ownerCookie := h.bootstrap(t)
	ctx := context.Background()

	org2, err := h.st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "r6-cnt-org2", DisplayName: "R6 Count Org 2"})
	if err != nil {
		t.Fatal(err)
	}

	var ollamaPkg *builtin.Package
	for _, p := range h.builtins.Packages() {
		if p.PackageID == "@tanpopo/ollama" {
			cp := p
			ollamaPkg = &cp
			break
		}
	}
	if ollamaPkg == nil {
		t.Fatal("missing @tanpopo/ollama builtin")
	}
	recStage := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri": ollamaPkg.SourceURI, "expected_sha256": ollamaPkg.ArchiveSHA256,
	})
	if recStage.Code != http.StatusAccepted {
		t.Fatalf("stage ollama: %d %s", recStage.Code, recStage.Body.String())
	}
	var stageResp struct{ InstallationID string `json:"installation_id"` }
	mustDecodeJSON(t, recStage.Body.Bytes(), &stageResp)
	recApp := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+stageResp.InstallationID+"/approve-permissions", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"enable_immediately": true, "acknowledge_unverified_external": false,
	})
	if recApp.Code != http.StatusOK {
		t.Fatalf("approve ollama: %d %s", recApp.Code, recApp.Body.String())
	}

	var adapterID string
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT id FROM provider_adapters WHERE installation_id = $1`, stageResp.InstallationID).Scan(&adapterID); err != nil {
		t.Fatal(err)
	}
	tx, err := h.st.RawDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	now := h.fk.Now().UTC().UnixMilli()
	_ = port.SetOrgRLSTx(ctx, tx, port.DialectPostgres, boot.OrgID)
	for i := 1; i <= 2; i++ {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO provider_connections (id, org_id, adapter_id, name, endpoint_base_url, data_region, billing_mode, auth_mode, state, version, created_at_ms, updated_at_ms)
			VALUES ($1, $2, $3, $4, 'http://127.0.0.1:11434', 'ON_PREM', 'SELF_HOSTED_INFRA', 'NONE', 'ACTIVE', 1, $5, $5)`,
			fmt.Sprintf("conn_cnt_a_%d", i), boot.OrgID, adapterID, fmt.Sprintf("Ollama A%d", i), now); err != nil {
			t.Fatal(err)
		}
	}
	_ = port.SetOrgRLSTx(ctx, tx, port.DialectPostgres, org2.ID)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO provider_connections (id, org_id, adapter_id, name, endpoint_base_url, data_region, billing_mode, auth_mode, state, version, created_at_ms, updated_at_ms)
		VALUES ('conn_cnt_b_1', $1, $2, 'Ollama B1', 'http://127.0.0.1:11434', 'ON_PREM', 'SELF_HOSTED_INFRA', 'NONE', 'ACTIVE', 1, $3, $3)`,
		org2.ID, adapterID, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if got := h.srv.countKillSwitchAffectedEntities(ctx, "", true, killswitch.LevelExtensionInstallation, stageResp.InstallationID); got != 3 {
		t.Fatalf("countKillSwitchAffectedEntities(instanceWide=true) = %d, want 3 across boot.OrgID+org2", got)
	}
	if got := h.srv.countKillSwitchAffectedEntities(ctx, boot.OrgID, false, killswitch.LevelExtensionInstallation, stageResp.InstallationID); got != 2 {
		t.Fatalf("countKillSwitchAffectedEntities(instanceWide=false, boot.OrgID) = %d, want 2", got)
	}
}

func TestDEF081_PostgresFourEyesCrossOrgSelfApprovalDenied(t *testing.T) {
	h := newPGAdminHarness(t)
	if h == nil {
		return
	}
	h.srv.installer.SetSyncAdaptersTx(h.srv.catalog.SyncExtensionProviderAdaptersTx)
	boot, ownerCookie := h.bootstrap(t)
	ctx := context.Background()

	org2, err := h.st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "r6-4eyes-org2", DisplayName: "R6 4Eyes Org 2"})
	if err != nil {
		t.Fatal(err)
	}
	// Enable Four-Eyes approval requirement on org2 (and verify cluster-wide enforcement across org partitions).
	tx, err := h.st.RawDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOperatorRLSTx(ctx, tx, port.DialectPostgres); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE organizations SET require_four_eyes_approval = TRUE WHERE id = $1`, org2.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var ollamaPkg *builtin.Package
	for _, p := range h.builtins.Packages() {
		if p.PackageID == "@tanpopo/ollama" {
			cp := p
			ollamaPkg = &cp
			break
		}
	}
	if ollamaPkg == nil {
		t.Fatal("missing @tanpopo/ollama builtin")
	}

	// Operator stages extension while scoped to boot.OrgID (audit event lands in boot.OrgID partition).
	recStage := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri": ollamaPkg.SourceURI, "expected_sha256": ollamaPkg.ArchiveSHA256,
	})
	if recStage.Code != http.StatusAccepted {
		t.Fatalf("stage ollama: %d %s", recStage.Code, recStage.Body.String())
	}
	var stageResp struct{ InstallationID string `json:"installation_id"` }
	mustDecodeJSON(t, recStage.Body.Bytes(), &stageResp)

	// Same operator attempts self-approval while switching org context to org2 via ?org_id= -> MUST fail 403 self_approval_forbidden!
	recApp := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+stageResp.InstallationID+"/approve-permissions?org_id="+org2.ID, ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"enable_immediately": true, "acknowledge_unverified_external": false,
	})
	if recApp.Code != http.StatusForbidden || !strings.Contains(recApp.Body.String(), "self_approval_forbidden") {
		t.Fatalf("expected 403 self_approval_forbidden across org partitions, got code=%d body=%s", recApp.Code, recApp.Body.String())
	}
}

func TestDEF081_PostgresExtensionBlastRadiusBootstrapOperator(t *testing.T) {
	h := newPGAdminHarness(t)
	if h == nil {
		return
	}
	h.srv.installer.SetSyncAdaptersTx(h.srv.catalog.SyncExtensionProviderAdaptersTx)
	boot, ownerCookie := h.bootstrap(t)

	var ollamaPkg *builtin.Package
	for _, p := range h.builtins.Packages() {
		if p.PackageID == "@tanpopo/ollama" {
			cp := p
			ollamaPkg = &cp
			break
		}
	}
	if ollamaPkg == nil {
		t.Fatal("missing @tanpopo/ollama builtin")
	}
	recStage := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri": ollamaPkg.SourceURI, "expected_sha256": ollamaPkg.ArchiveSHA256,
	})
	if recStage.Code != http.StatusAccepted {
		t.Fatalf("stage ollama: %d %s", recStage.Code, recStage.Body.String())
	}
	var stageResp struct{ InstallationID string `json:"installation_id"` }
	mustDecodeJSON(t, recStage.Body.Bytes(), &stageResp)
	recApp := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+stageResp.InstallationID+"/approve-permissions", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"enable_immediately": true, "acknowledge_unverified_external": false,
	})
	if recApp.Code != http.StatusOK {
		t.Fatalf("approve ollama: %d %s", recApp.Code, recApp.Body.String())
	}
	recQ := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+stageResp.InstallationID+"/quarantine", ownerCookie, boot.CSRFToken, `"v2"`, map[string]any{
		"reason": "blast radius test quarantine",
	})
	if recQ.Code != http.StatusOK {
		t.Fatalf("quarantine ollama: %d %s", recQ.Code, recQ.Body.String())
	}

	// Bootstrapped org-bound personal operator querying /admin/v1/blast-radius remains safely scoped to session org (200 OK).
	recBlast := h.doCookie(t, http.MethodGet, "/admin/v1/blast-radius?target_type=EXTENSION&target_id="+stageResp.InstallationID, ownerCookie, "", "", nil)
	if recBlast.Code != http.StatusOK {
		t.Fatalf("blast-radius extension code=%d body=%s", recBlast.Code, recBlast.Body.String())
	}
}
