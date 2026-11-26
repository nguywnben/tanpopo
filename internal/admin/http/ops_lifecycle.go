// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/ops/backup"
	"github.com/nguywnben/tanpopo/internal/ops/upgrade"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const clusterNodeStaleThresholdMs int64 = 15_000

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

func (s *Server) loadClusterNodes(ctx context.Context, currentEpoch int64) ([]map[string]any, error) {
	nowMs := s.nowMs()
	var nodes []map[string]any
	err := s.withOperatorReadTx(ctx, func(q adminDBQuerier) error {
		rows, qerr := q.QueryContext(ctx, s.rebind(`
			SELECT node_id, role, listen_addr, config_epoch, snapshot_age_ms, health, wal_bytes, last_heartbeat_ms
			FROM cluster_nodes
			ORDER BY last_heartbeat_ms DESC, node_id ASC`))
		if qerr != nil {
			// Backward-compatible fallback if running on a test fixture schema without 0024_cluster_nodes.
			if strings.Contains(strings.ToLower(qerr.Error()), "no such table") || strings.Contains(strings.ToLower(qerr.Error()), "does not exist") {
				return nil
			}
			return qerr
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var (
				nodeID, role, listenAddr, health string
				nodeEpoch, snapAgeMs, walBytes   int64
				lastHeartbeatMs                  int64
			)
			if serr := rows.Scan(&nodeID, &role, &listenAddr, &nodeEpoch, &snapAgeMs, &health, &walBytes, &lastHeartbeatMs); serr != nil {
				return serr
			}
			stale := nowMs-lastHeartbeatMs > clusterNodeStaleThresholdMs
			effectiveHealth := health
			if stale {
				effectiveHealth = "stale"
			} else if currentEpoch > 0 && nodeEpoch < currentEpoch {
				effectiveHealth = "lagging"
			}
			nodes = append(nodes, map[string]any{
				"id":                nodeID,
				"role":              role,
				"listen_addr":       listenAddr,
				"config_epoch":      nodeEpoch,
				"snapshot_age_ms":   snapAgeMs,
				"health":            effectiveHealth,
				"wal_bytes":         walBytes,
				"last_heartbeat_ms": lastHeartbeatMs,
				"stale":             stale,
			})
		}
		return rows.Err()
	})
	return nodes, err
}

func (s *Server) handleGetClusterOpsStatus(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok || !s.requireInstanceOperator(w, r, sc, rbac.ActionOpsRead) {
		return
	}
	storageEngine := "sqlite_wal"
	var walBytes int64
	if s.dialect == port.DialectPostgres {
		storageEngine = "postgres"
	} else {
		reporter, ok := s.storage.(sqliteWALReporter)
		if !ok {
			writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "ops_backend_unavailable", "upstream_error", "WAL status is unavailable for this storage backend", true, nil)
			return
		}
		var werr error
		walBytes, werr = reporter.WALBytes()
		if werr != nil {
			writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "ops_backend_unavailable", "upstream_error", "failed reading SQLite WAL status", true, nil)
			return
		}
	}
	var dbEpoch int64
	if err := s.withOperatorReadTx(r.Context(), func(q adminDBQuerier) error {
		return q.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(config_epoch), 0) FROM config_changelog`).Scan(&dbEpoch)
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading current configuration epoch", true, nil)
		return
	}
	epoch := dbEpoch
	var selfSnapAgeMs int64
	health := "unknown"
	if s.snapshots != nil {
		if current := s.snapshots.Current(); current != nil {
			epoch = current.Epoch
		}
		health = "ok"
		if readinessErr := s.snapshots.CheckServingReady(); readinessErr != nil {
			health = "unhealthy"
		} else if age, ok := s.snapshots.SnapshotAge(); ok && age > 0 {
			selfSnapAgeMs = age.Milliseconds()
		}
	}
	backupSupported, backupReason := s.backupAvailability()
	nodes, err := s.loadClusterNodes(r.Context(), dbEpoch)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading cluster node inventory", true, nil)
		return
	}
	if len(nodes) == 0 {
		nodeID := "standalone"
		if s.dialect == port.DialectPostgres {
			nodeID = "primary"
		}
		nodes = []map[string]any{{
			"id":              nodeID,
			"role":            "all",
			"config_epoch":    epoch,
			"snapshot_age_ms": selfSnapAgeMs,
			"health":          health,
			"wal_bytes":       walBytes,
			"stale":           false,
		}}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current_epoch":             epoch,
		"storage_engine":            storageEngine,
		"backup_supported":          backupSupported,
		"backup_unavailable_reason": backupReason,
		"nodes":                     nodes,
	})
}

func (s *Server) handleRetireMasterKEK(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok || !s.requireInstanceOperator(w, r, sc, rbac.ActionOpsWrite) {
		return
	}
	var req struct {
		KeyID string `json:"key_id"`
	}
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	keyID := strings.TrimSpace(req.KeyID)
	if keyID == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "key_id is required", false, nil)
		return
	}
	if s.vault == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "ops_backend_unavailable", "upstream_error", "vault is not initialized", true, nil)
		return
	}

	var currentEpoch int64
	var remainingDEKs int
	if err := s.withOperatorReadTx(r.Context(), func(q adminDBQuerier) error {
		if err := q.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(config_epoch), 0) FROM config_changelog`).Scan(&currentEpoch); err != nil {
			return err
		}
		err := q.QueryRowContext(r.Context(), s.rebind(`SELECT COUNT(*) FROM org_encryption_keys WHERE kek_key_id = ?`), keyID).Scan(&remainingDEKs)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking remaining encrypted org DEKs", true, nil)
		return
	}
	if remainingDEKs > 0 {
		writeErrorEnvelope(w, r, http.StatusConflict, "org_deks_not_rewrapped", "conflict_error", fmt.Sprintf("cannot retire Master KEK %q while %d organization DEK(s) remain wrapped under it", keyID, remainingDEKs), false, nil)
		return
	}

	nodes, err := s.loadClusterNodes(r.Context(), currentEpoch)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed inspecting cluster nodes before KEK retirement", true, nil)
		return
	}
	verifiedNodes := make([]string, 0, len(nodes))
	var laggingNodes []string
	for _, node := range nodes {
		id, _ := node["id"].(string)
		health, _ := node["health"].(string)
		stale, _ := node["stale"].(bool)
		nodeEpoch, _ := node["config_epoch"].(int64)
		if stale || health != "ok" || nodeEpoch < currentEpoch {
			laggingNodes = append(laggingNodes, fmt.Sprintf("%s(%s@%d)", id, health, nodeEpoch))
			continue
		}
		verifiedNodes = append(verifiedNodes, id)
	}
	if len(laggingNodes) > 0 {
		writeErrorEnvelope(w, r, http.StatusConflict, "cluster_node_sync_incomplete", "conflict_error", fmt.Sprintf("cannot retire Master KEK %q until 100%% of cluster nodes synchronize to epoch %d: %s", keyID, currentEpoch, strings.Join(laggingNodes, ", ")), true, map[string]any{"incomplete_nodes": laggingNodes, "required_epoch": currentEpoch})
		return
	}

	if err := s.vault.RetireKEK(r.Context(), keyID); err != nil {
		if errors.Is(err, vault.ErrInvalidMasterKey) {
			writeErrorEnvelope(w, r, http.StatusConflict, "cannot_retire_active_key", "conflict_error", err.Error(), false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}
	if err := s.appendOpsAudit(r, sc.Actor.PrincipalID, "ops.kek_retired", keyID, map[string]any{
		"verified_nodes": verifiedNodes,
		"required_epoch": currentEpoch,
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "audit_failed", "upstream_error", "failed recording KEK retirement audit event", true, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"retired_key_id": keyID,
		"verified_nodes": verifiedNodes,
	})
}

func (s *Server) backupAvailability() (bool, string) {
	if !backup.PlatformSupported() {
		return false, backup.PlatformSupportUnavailableReason()
	}
	if _, ok := s.storage.(sqliteSnapshotter); !ok {
		return false, "Console snapshot backups currently support SQLite storage only; use PostgreSQL backup tooling for PostgreSQL deployments"
	}
	if s.vault == nil {
		return false, "Encrypted backup requires an initialized KMS/vault"
	}
	if strings.TrimSpace(s.lockfilePath) == "" || strings.TrimSpace(s.configPath) == "" || s.installer == nil || strings.TrimSpace(s.installer.ArtifactRoot()) == "" {
		return false, "backup requires configured tanpopo.lock, tanpopo.yaml, and extension artifact store"
	}
	for _, filePath := range []string{s.lockfilePath, s.configPath} {
		info, err := os.Lstat(filePath)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return false, "backup requires regular non-symlink tanpopo.lock and tanpopo.yaml files on disk"
		}
	}
	artInfo, err := os.Lstat(s.installer.ArtifactRoot())
	if err != nil || artInfo.Mode()&os.ModeSymlink != 0 || !artInfo.IsDir() {
		return false, "backup requires a real non-symlink extension plugins directory on disk"
	}
	return true, ""
}

func (s *Server) handleCreateEncryptedBackup(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok || !s.requireInstanceOperator(w, r, sc, rbac.ActionOpsWrite) {
		return
	}
	backupSupported, backupReason := s.backupAvailability()
	if !backupSupported {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "ops_backend_unavailable", "upstream_error", backupReason, true, nil)
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
	configPath := s.configPath
	if strings.TrimSpace(lockfilePath) == "" || strings.TrimSpace(configPath) == "" || s.installer == nil || strings.TrimSpace(s.installer.ArtifactRoot()) == "" {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "ops_backend_unavailable", "upstream_error", "backup requires configured tanpopo.lock, tanpopo.yaml, and extension artifact store", true, nil)
		return
	}
	receipt, err := backup.CreateSQLite(r.Context(), store, s.vault, backupDir, lockfilePath, configPath, s.installer.ArtifactRoot(), s.nowMs())
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "backup_failed", "upstream_error", "encrypted backup creation failed", true, nil)
		return
	}
	resolvedBackupDir, err := filepath.EvalSymlinks(backupDir)
	if err != nil || filepath.Clean(resolvedBackupDir) != filepath.Clean(backupDir) {
		removePinnedBackupArchive(dataDir, receipt.ArchiveFilename)
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "backup_failed", "upstream_error", "backup destination changed during the operation", true, nil)
		return
	}
	if err := s.appendOpsAudit(r, sc.Actor.PrincipalID, "ops.backup_created", receipt.BackupID, map[string]any{
		"archive_filename": receipt.ArchiveFilename,
		"sha256_hex":       receipt.SHA256Hex,
		"size_bytes":       receipt.SizeBytes,
	}); err != nil {
		removePinnedBackupArchive(dataDir, receipt.ArchiveFilename)
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "audit_failed", "upstream_error", "backup was rolled back because its audit record could not be written", true, nil)
		return
	}
	writeJSON(w, http.StatusCreated, receipt)
}

func removePinnedBackupArchive(dataDir, filename string) {
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		return
	}
	defer root.Close()
	bRoot, err := root.OpenRoot("backups")
	if err != nil {
		return
	}
	defer bRoot.Close()
	_ = bRoot.Remove(filename)
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
		"compatible":               result.Compatible,
		"state":                    result.State,
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
		RequestID: ptrOrNil(requestIDFromContext(r.Context())),
		EventType: eventType, TargetType: "instance_ops", TargetID: targetID,
		MetadataJSON: string(metadataJSON),
	})
}
