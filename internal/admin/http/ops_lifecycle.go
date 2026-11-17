// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/ops/backup"
	"github.com/nguywnben/tanpopo/internal/ops/upgrade"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type sqliteWALReporter interface {
	WALBytes() (int64, error)
}

type sqliteSnapshotter interface {
	DBPath() string
	BackupTo(ctx context.Context, destPath string) error
}

func (s *Server) requireInstanceOperator(w http.ResponseWriter, r *http.Request, sc *authSessionContext, action rbac.Action) bool {
	if !s.authorize(w, r, sc, action, rbac.TargetScope{}) {
		return false
	}
	if !sc.IsOperator {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "instance OPERATOR role is required", false, nil)
		return false
	}
	return true
}

func (s *Server) handleGetClusterOpsStatus(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok || !s.requireInstanceOperator(w, r, sc, rbac.ActionOpsRead) {
		return
	}
	reporter, ok := s.storage.(sqliteWALReporter)
	if !ok {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "ops_backend_unavailable", "upstream_error", "WAL status is unavailable for this storage backend", true, nil)
		return
	}
	walBytes, err := reporter.WALBytes()
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "ops_backend_unavailable", "upstream_error", "failed reading SQLite WAL status", true, nil)
		return
	}
	var epoch int64
	if err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(seq_no), 0) FROM config_changelog`).Scan(&epoch); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading current configuration epoch", true, nil)
		return
	}
	if s.snapshots != nil {
		if current := s.snapshots.Current(); current != nil {
			epoch = current.Epoch
		}
	}
	health := "unknown"
	if s.snapshots != nil {
		health = "ok"
		if readinessErr := s.snapshots.CheckServingReady(); readinessErr != nil {
			health = "unhealthy"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current_epoch": epoch,
		"storage_engine": "sqlite_wal",
		"nodes": []map[string]any{{"id": "standalone", "config_epoch": epoch, "health": health, "wal_bytes": walBytes}},
	})
}

func (s *Server) handleCreateEncryptedBackup(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok || !s.requireInstanceOperator(w, r, sc, rbac.ActionOpsWrite) {
		return
	}
	store, ok := s.storage.(sqliteSnapshotter)
	if !ok || s.vault == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "ops_backend_unavailable", "upstream_error", "encrypted SQLite backup dependencies are unavailable", true, nil)
		return
	}
	dataDir, err := filepath.EvalSymlinks(filepath.Dir(store.DBPath()))
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "backup_failed", "upstream_error", "failed resolving the SQLite data directory", true, nil)
		return
	}
	backupDir := filepath.Join(dataDir, "backups")
	if info, err := os.Lstat(backupDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "ops_backend_unavailable", "upstream_error", "backup destination must be a real directory under the SQLite data directory", true, nil)
			return
		}
	} else if !os.IsNotExist(err) {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "backup_failed", "upstream_error", "failed inspecting protected backup directory", true, nil)
		return
	}
	lockfilePath := s.lockfilePath
	if lockfilePath != "" {
		if _, err := os.Stat(lockfilePath); err != nil && !os.IsNotExist(err) {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "backup_failed", "upstream_error", "failed inspecting extension lockfile", true, nil)
			return
		}
	}
	receipt, err := backup.CreateSQLite(r.Context(), store, s.vault, backupDir, lockfilePath, s.nowMs())
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "backup_failed", "upstream_error", "encrypted backup creation failed", true, nil)
		return
	}
	resolvedBackupDir, err := filepath.EvalSymlinks(backupDir)
	if err != nil || filepath.Clean(resolvedBackupDir) != filepath.Clean(backupDir) {
		_ = os.Remove(filepath.Join(backupDir, receipt.ArchiveFilename))
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "backup_failed", "upstream_error", "backup destination changed during the operation", true, nil)
		return
	}
	if err := s.appendOpsAudit(r, sc.Actor.PrincipalID, "ops.backup_created", receipt.BackupID, map[string]any{
		"archive_filename": receipt.ArchiveFilename,
		"sha256_hex":       receipt.SHA256Hex,
		"size_bytes":       receipt.SizeBytes,
	}); err != nil {
		_ = os.Remove(filepath.Join(backupDir, receipt.ArchiveFilename))
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "audit_failed", "upstream_error", "backup was rolled back because its audit record could not be written", true, nil)
		return
	}
	writeJSON(w, http.StatusCreated, receipt)
}

func (s *Server) handleRunUpgradePreflight(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok || !s.requireInstanceOperator(w, r, sc, rbac.ActionOpsWrite) {
		return
	}
	var req struct {
		TargetCoreVersion string `json:"target_core_version"`
	}
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if strings.TrimSpace(req.TargetCoreVersion) == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "target_core_version is required", false, nil)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT ei.id, ei.package_id, COALESCE(ev.semver, ''), COALESCE(ev.core_version_range, '')
		FROM extension_installations ei
		LEFT JOIN extension_versions ev ON ev.id = ei.active_version_id
		WHERE ei.state = 'ENABLED_ACTIVE'
		ORDER BY ei.package_id, ei.id`)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading active extension compatibility", true, nil)
		return
	}
	installed := make([]upgrade.InstalledExtension, 0)
	for rows.Next() {
		var extension upgrade.InstalledExtension
		if err := rows.Scan(&extension.InstallationID, &extension.PackageID, &extension.Version, &extension.CoreRange); err != nil {
			_ = rows.Close()
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading active extension compatibility", true, nil)
			return
		}
		installed = append(installed, extension)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating active extension compatibility", true, nil)
		return
	}
	_ = rows.Close()
	result, err := upgrade.Evaluate(req.TargetCoreVersion, installed)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}
	if err := s.appendOpsAudit(r, sc.Actor.PrincipalID, "ops.upgrade_preflight_ran", req.TargetCoreVersion, map[string]any{
		"compatible":                result.Compatible,
		"state":                     result.State,
		"blocking_extension_count": len(result.BlockingExtensions),
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "audit_failed", "upstream_error", "preflight result could not be recorded", true, nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) appendOpsAudit(r *http.Request, actorID, eventType, targetID string, metadata map[string]any) error {
	if s.audit == nil {
		return errors.New("audit sink is unavailable")
	}
	auditID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return err
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return s.audit.Append(r.Context(), port.AuditEvent{
		ID: auditID, CreatedAtMs: s.nowMs(), ActorKind: "PRINCIPAL",
		ActorID: actorID, EntryPoint: port.AuditEntryPointAdminAPI,
		EventType: eventType, TargetType: "instance_ops", TargetID: targetID,
		MetadataJSON: string(metadataJSON),
	})
}

