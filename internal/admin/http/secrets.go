// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type secretMetadataDTO struct {
	CredentialID        string  `json:"credential_id"`
	ProjectID           *string `json:"project_id"`
	Name                string  `json:"name"`
	SecretKind          string  `json:"secret_kind"`
	TrailingFingerprint string  `json:"trailing_fingerprint"`
	Version             int64   `json:"version"`
	CreatedAt           string  `json:"created_at"`
}

type createSecretRequest struct {
	Name          string  `json:"name"`
	ProjectID     *string `json:"project_id"`
	SecretKind    string  `json:"secret_kind"`
	SecretValue   string  `json:"secret_value"`
	BoundAudience *string `json:"bound_audience"`
}

func formatTrailingFingerprint(keyFingerprint string) string {
	trimmed := strings.TrimSpace(keyFingerprint)
	if len(trimmed) >= 4 {
		return "..." + trimmed[len(trimmed)-4:]
	}
	return "..." + trimmed
}

func normalizeSecretKindInput(raw string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "API_KEY":
		return vault.SecretKindAPIKey, true
	case "HMAC_SECRET":
		return vault.SecretKindHMACSecret, true
	case "OAUTH_CLIENT_SECRET", "OAUTH2_TOKEN_SET":
		return vault.SecretKindOAuth2TokenSet, true
	case "WEBHOOK_SECRET", "WEBHOOK_HMAC_SECRET":
		return vault.SecretKindWebhookHMAC, true
	case "AWS_SIGV4_KEY":
		return vault.SecretKindAWSSigV4Key, true
	case "MTLS_CLIENT_CERT":
		return vault.SecretKindMTLSClientCert, true
	case "OCI_REGISTRY_AUTH":
		return vault.SecretKindOCIRegistryAuth, true
	default:
		return "", false
	}
}

func ptrStrValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	projFilter := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projFilter != "" {
		if !s.authorize(w, r, sc, rbac.ActionSecretReadMeta, rbac.TargetScope{OrgID: orgID, ProjectID: projFilter}) {
			return
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionSecretReadMeta, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
			return
		}
	}

	filterHash := sha256HexString("secrets|" + projFilter)
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "secrets", orgID, filterHash)
	if !ok {
		return
	}

	orgWide := actorHasOrgScopeBinding(sc.Actor, orgID)
	projIDs := actorProjectIDs(sc.Actor, orgID)

	var q strings.Builder
	args := []any{orgID}
	q.WriteString(`
		SELECT id, owner_project_id, name, secret_kind, key_fingerprint, version, created_at_ms
		FROM secret_credentials
		WHERE org_id = ?`)
	if projFilter != "" {
		q.WriteString(` AND owner_project_id = ?`)
		args = append(args, projFilter)
	} else if !orgWide {
		if len(projIDs) == 0 {
			writeJSON(w, http.StatusOK, paginatedListEnvelope[secretMetadataDTO]{Data: []secretMetadataDTO{}, HasMore: false})
			return
		}
		q.WriteString(` AND owner_project_id IN (` + strings.Repeat("?,", len(projIDs)-1) + `?)`)
		for _, pid := range projIDs {
			args = append(args, pid)
		}
	}
	if cur != nil {
		q.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}
	q.WriteString(` ORDER BY created_at_ms DESC, id DESC LIMIT ?`)
	args = append(args, limit+1)

	type rowItem struct {
		dto       secretMetadataDTO
		createdMs int64
	}
	items := make([]rowItem, 0, limit+1)
	var scanFailed bool
	err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		rows, qerr := dbq.QueryContext(r.Context(), s.rebind(q.String()), args...)
		if qerr != nil {
			return qerr
		}
		defer rows.Close()
		for rows.Next() {
			var (
				id, name, kind, fp string
				projNS             sql.NullString
				ver, createdMs     int64
			)
			if scanErr := rows.Scan(&id, &projNS, &name, &kind, &fp, &ver, &createdMs); scanErr != nil {
				scanFailed = true
				return scanErr
			}
			var projPtr *string
			if projNS.Valid {
				v := projNS.String
				projPtr = &v
			}
			items = append(items, rowItem{
				dto: secretMetadataDTO{
					CredentialID:        id,
					ProjectID:           projPtr,
					Name:                name,
					SecretKind:          kind,
					TrailingFingerprint: formatTrailingFingerprint(fp),
					Version:             ver,
					CreatedAt:           formatTimeRFC3339Ms(createdMs),
				},
				createdMs: createdMs,
			})
		}
		return rows.Err()
	})
	if scanFailed {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning secret metadata", true, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing secrets", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]secretMetadataDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "secrets", orgID, filterHash, last.createdMs, last.dto.CredentialID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[secretMetadataDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleCreateSecret(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req createSecretRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	var ownerProjPtr *string
	if req.ProjectID != nil {
		if trimmed := strings.TrimSpace(*req.ProjectID); trimmed != "" {
			ownerProjPtr = &trimmed
		}
	}
	if !s.authorize(w, r, sc, rbac.ActionSecretWrite, rbac.TargetScope{OrgID: orgID, ProjectID: ptrStrValue(ownerProjPtr)}) {
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 128 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "name (1..128 chars) is required", false, nil)
		return
	}
	if req.SecretValue == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "secret_value is required", false, nil)
		return
	}
	kind, validKind := normalizeSecretKindInput(req.SecretKind)
	if !validKind {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "invalid secret_kind", false, nil)
		return
	}
	if s.vault == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "vault_unavailable", "upstream_error", "credential vault is not configured", true, nil)
		return
	}

	if ownerProjPtr != nil {
		if !s.requireActiveOrgAndProject(w, r, orgID, *ownerProjPtr) {
			return
		}
	} else {
		if !s.requireActiveOrg(w, r, orgID) {
			return
		}
	}

	var boundAud *string
	if req.BoundAudience != nil {
		if trimmed := strings.TrimSpace(*req.BoundAudience); trimmed != "" {
			boundAud = &trimmed
		}
	}

	secretBuf := []byte(req.SecretValue)
	defer clear(secretBuf)

	meta, err := s.vault.SealSecret(r.Context(), vault.SealSecretParams{
		OrgID:          orgID,
		OwnerProjectID: ownerProjPtr,
		Name:           name,
		SecretKind:     kind,
		BoundAudience:  boundAud,
		Plaintext:      secretBuf,
	})
	if err != nil {
		switch {
		case errors.Is(err, port.ErrAlreadyExists):
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "credential already exists", false, nil)
		case errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization or project not found", false, nil)
		case errors.Is(err, vault.ErrInvalidInput), errors.Is(err, port.ErrInvalidInput):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed sealing credential", true, nil)
		}
		return
	}

	dto := secretMetadataDTO{
		CredentialID:        meta.ID,
		ProjectID:           meta.OwnerProjectID,
		Name:                meta.Name,
		SecretKind:          meta.SecretKind,
		TrailingFingerprint: formatTrailingFingerprint(meta.KeyFingerprint),
		Version:             meta.Version,
		CreatedAt:           formatTimeRFC3339Ms(meta.CreatedAtMs),
	}

	if audID, aerr := idgen.New(idgen.PrefixAudit); aerr == nil && s.audit != nil {
		rawMeta, _ := json.Marshal(map[string]any{
			"name":                 dto.Name,
			"secret_kind":          dto.SecretKind,
			"project_id":           dto.ProjectID,
			"trailing_fingerprint": dto.TrailingFingerprint,
		})
		_ = s.audit.Append(r.Context(), port.AuditEvent{
			ID:           audID,
			CreatedAtMs:  s.nowMs(),
			OrgID:        &orgID,
			ActorKind:    "PRINCIPAL",
			ActorID:      sc.Actor.PrincipalID,
			EntryPoint:   port.AuditEntryPointAdminAPI,
			EventType:    "secret_credential.created",
			TargetType:   "secret_credential",
			TargetID:     dto.CredentialID,
			RequestID:    ptrOrNil(requestIDFromContext(r.Context())),
			MetadataJSON: string(rawMeta),
		})
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	w.Header().Set("ETag", FormatETag(dto.Version))
	writeJSON(w, http.StatusCreated, dto)
}

func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	credID := strings.TrimSpace(r.PathValue("credentialId"))
	if credID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "secret credential not found", false, nil)
		return
	}

	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionSecretWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}

	cur, found, err := s.loadSecretMetadataDTO(r, orgID, credID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading secret credential", true, nil)
		return
	}
	if !found {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "secret credential not found", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionSecretWrite, rbac.TargetScope{OrgID: orgID, ProjectID: ptrStrValue(cur.ProjectID)}) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, ptrStrValue(cur.ProjectID)) {
		return
	}

	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	if expectedVer != cur.Version {
		writeOptimisticConflict(w, r, cur.Version, cur)
		return
	}

	now := s.nowMs()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting secret delete tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(r.Context(), tx, s.dialect, orgID); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scoping secret delete tx", true, nil)
		return
	}

	var bindingCount, connCount, webhookCount int
	if err := tx.QueryRowContext(r.Context(), s.rebind(`
		SELECT COUNT(1) FROM connection_secret_bindings WHERE org_id = ? AND credential_id = ?`),
		orgID, credID,
	).Scan(&bindingCount); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking credential slot bindings", true, nil)
		return
	}
	if err := tx.QueryRowContext(r.Context(), s.rebind(`
		SELECT COUNT(1) FROM provider_connections WHERE org_id = ? AND credential_id = ?`),
		orgID, credID,
	).Scan(&connCount); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking provider connection references", true, nil)
		return
	}
	if err := tx.QueryRowContext(r.Context(), s.rebind(`
		SELECT COUNT(1) FROM webhook_subscriptions WHERE org_id = ? AND (signing_secret_credential_id = ? OR target_url_credential_id = ?)`),
		orgID, credID, credID,
	).Scan(&webhookCount); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking webhook references", true, nil)
		return
	}
	if bindingCount > 0 || connCount > 0 || webhookCount > 0 {
		writeErrorEnvelope(w, r, http.StatusConflict, "credential_in_use", "conflict_error", fmt.Sprintf("credential %s is still bound to active connections or webhooks", credID), false, cur)
		return
	}

	res, err := tx.ExecContext(r.Context(), s.rebind(`
		DELETE FROM secret_credentials WHERE org_id = ? AND id = ? AND version = ?`),
		orgID, credID, expectedVer,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if (errors.As(err, &pgErr) && pgErr.Code == "23503") || strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			writeErrorEnvelope(w, r, http.StatusConflict, "credential_in_use", "conflict_error", "credential is still referenced by another resource", false, cur)
			return
		}
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed deleting secret credential", true, nil)
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		_ = tx.Rollback()
		latest, exists, lerr := s.loadSecretMetadataDTO(r, orgID, credID)
		if lerr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading secret credential", true, nil)
			return
		}
		if !exists {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "secret credential not found", false, nil)
			return
		}
		writeOptimisticConflict(w, r, latest.Version, latest)
		return
	}

	epoch, err := s.appendChangelogTx(r.Context(), tx, orgID, "secret_credential", credID, port.MutationDelete, false, now)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording changelog", true, nil)
		return
	}

	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting audit id", true, nil)
		return
	}
	rawMeta, _ := json.Marshal(map[string]any{
		"name":        cur.Name,
		"secret_kind": cur.SecretKind,
		"project_id":  cur.ProjectID,
	})
	if err := s.txAudit.AppendTx(r.Context(), tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        &orgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      sc.Actor.PrincipalID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "secret_credential.deleted",
		TargetType:   "secret_credential",
		TargetID:     credID,
		RequestID:    ptrOrNil(requestIDFromContext(r.Context())),
		MetadataJSON: string(rawMeta),
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed appending audit event", true, nil)
		return
	}

	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing secret delete", true, nil)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}
	s.publishChange(r.Context(), snapshot.ChangeEvent{
		ConfigEpoch:  epoch,
		OrgID:        orgID,
		EntityKind:   "secret_credential",
		EntityID:     credID,
		MutationType: port.MutationDelete,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) loadSecretMetadataDTO(r *http.Request, orgID, credID string) (secretMetadataDTO, bool, error) {
	var (
		dto       secretMetadataDTO
		projNS    sql.NullString
		fp        string
		createdMs int64
	)
	err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
		return q.QueryRowContext(r.Context(), s.rebind(`
			SELECT id, owner_project_id, name, secret_kind, key_fingerprint, version, created_at_ms
			FROM secret_credentials
			WHERE org_id = ? AND id = ?`),
			orgID, credID,
		).Scan(&dto.CredentialID, &projNS, &dto.Name, &dto.SecretKind, &fp, &dto.Version, &createdMs)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return secretMetadataDTO{}, false, nil
	}
	if err != nil {
		return secretMetadataDTO{}, false, err
	}
	if projNS.Valid {
		v := projNS.String
		dto.ProjectID = &v
	}
	dto.TrailingFingerprint = formatTrailingFingerprint(fp)
	dto.CreatedAt = formatTimeRFC3339Ms(createdMs)
	return dto, true, nil
}
