// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
)

func TestCR13InstanceOpsHandlersReturnCompletedResults(t *testing.T) {
	h := newAdminTestHarness(t)
	h.registerBearerToken("tok_cr13_operator", &rbac.Actor{
		PrincipalID:     "prc_cr13_operator",
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	})

	status := h.doBearer(t, http.MethodGet, "/admin/v1/ops/cluster-status", "tok_cr13_operator", "", nil)
	if status.Code != http.StatusOK {
		t.Fatalf("GET cluster status code=%d body=%s", status.Code, status.Body.String())
	}
	var cluster struct {
		Nodes []struct {
			WALBytes int64 `json:"wal_bytes"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &cluster); err != nil {
		t.Fatalf("decode cluster status: %v", err)
	}
	if len(cluster.Nodes) != 1 || cluster.Nodes[0].WALBytes < 0 {
		t.Fatalf("expected measured non-negative WAL bytes, got %s", status.Body.String())
	}

	backup := h.doBearer(t, http.MethodPost, "/admin/v1/ops/backups", "tok_cr13_operator", "", nil)
	if backup.Code != http.StatusCreated {
		t.Fatalf("POST backups code=%d body=%s", backup.Code, backup.Body.String())
	}
	var receipt struct {
		BackupID        string `json:"backup_id"`
		ArchiveFilename string `json:"archive_filename"`
		SHA256Hex       string `json:"sha256_hex"`
		SizeBytes       int64  `json:"size_bytes"`
	}
	if err := json.Unmarshal(backup.Body.Bytes(), &receipt); err != nil {
		t.Fatalf("decode backup receipt: %v", err)
	}
	if receipt.BackupID == "" || receipt.ArchiveFilename == "" || len(receipt.SHA256Hex) != 64 || receipt.SizeBytes <= 0 {
		t.Fatalf("backup returned incomplete receipt: %s", backup.Body.String())
	}
	archivePath := filepath.Join(filepath.Dir(h.st.DBPath()), "backups", receipt.ArchiveFilename)
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("backup receipt points to missing archive %q: %v", archivePath, err)
	}
	var backupAuditCount int
	if err := h.st.RawDB().QueryRow(`SELECT COUNT(*) FROM audit_events WHERE event_type = 'ops.backup_created' AND target_id = ?`, receipt.BackupID).Scan(&backupAuditCount); err != nil {
		t.Fatalf("query backup audit event: %v", err)
	}
	if backupAuditCount != 1 {
		t.Fatalf("expected one immutable backup audit event, got %d", backupAuditCount)
	}

	preflight := h.doBearer(t, http.MethodPost, "/admin/v1/ops/upgrades/preflight", "tok_cr13_operator", "", map[string]any{
		"target_core_version": "v0.2.0",
	})
	if preflight.Code != http.StatusOK {
		t.Fatalf("POST preflight code=%d body=%s", preflight.Code, preflight.Body.String())
	}
	var verdict struct {
		Compatible bool   `json:"compatible"`
		State      string `json:"state"`
	}
	if err := json.Unmarshal(preflight.Body.Bytes(), &verdict); err != nil {
		t.Fatalf("decode preflight verdict: %v", err)
	}
	if !verdict.Compatible || verdict.State != "READY_TO_UPGRADE" {
		t.Fatalf("expected computed compatible preflight result, got %s", preflight.Body.String())
	}
	var preflightAuditCount int
	if err := h.st.RawDB().QueryRow(`SELECT COUNT(*) FROM audit_events WHERE event_type = 'ops.upgrade_preflight' AND target_id = 'v0.2.0'`).Scan(&preflightAuditCount); err != nil {
		t.Fatalf("query preflight audit event: %v", err)
	}
	if preflightAuditCount != 1 {
		t.Fatalf("expected one immutable preflight audit event, got %d", preflightAuditCount)
	}
}
