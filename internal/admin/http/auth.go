// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	OrgID    string `json:"org_id,omitempty"`
}

type adminSessionResponse struct {
	PrincipalID string   `json:"principal_id"`
	Email       string   `json:"email"`
	OrgID       *string  `json:"org_id"`
	Roles       []string `json:"roles"`
	CSRFToken   string   `json:"csrf_token"`
	ExpiresAt   string   `json:"expires_at"`
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodePublicJSON(w, r, &req) {
		return
	}

	clientIP := extractClientIP(r)
	info, cookie, err := s.auth.LoginForOrg(r.Context(), clientIP, req.OrgID, req.Email, req.Password)
	if err != nil {
		var rlErr *auth.RateLimitError
		if errors.As(err, &rlErr) {
			if rlErr.RetryAfterSeconds > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(rlErr.RetryAfterSeconds))
			}
			writeErrorEnvelope(w, r, http.StatusTooManyRequests, "auth_rate_limited", "rate_limit_error", "too many failed login attempts", true, nil)
			return
		}
		if errors.Is(err, auth.ErrAmbiguousTenantLogin) {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "ambiguous_tenant_login", "invalid_request_error", "ambiguous email across organizations; org_id required", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusUnauthorized, "invalid_credentials", "authentication_error", "invalid email or password", false, nil)
		return
	}

	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusOK, adminSessionResponse{
		PrincipalID: info.PrincipalID,
		Email:       info.Email,
		OrgID:       info.OrgID,
		Roles:       nonEmptyRoleSlice(info.Roles),
		CSRFToken:   info.CSRFToken,
		ExpiresAt:   formatTimeRFC3339Ms(info.ExpiresAtMs),
	})
}

func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionSessionSelf, rbac.TargetScope{OrgID: sc.OrgID}) {
		return
	}
	roles := collectUniqueActorRoles(sc.Actor)
	writeJSON(w, http.StatusOK, adminSessionResponse{
		PrincipalID: sc.Actor.PrincipalID,
		Email:       sc.Email,
		OrgID:       sc.Actor.SessionOrgID,
		Roles:       roles,
		CSRFToken:   sc.CSRFToken,
		ExpiresAt:   formatTimeRFC3339Ms(sc.ExpiresAtMs),
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionSessionSelf, rbac.TargetScope{OrgID: sc.OrgID}) {
		return
	}

	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		s.auth.Logout(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	// Revoke any active ephemeral playground keys owned by the logging-out principal.
	if err := s.revokeEphemeralPlaygroundKeys(r, sc.Actor.PrincipalID); err != nil {
		s.emitSecurityAudit(r.Context(), sc, "auth.logout_playground_revoke_failed", map[string]any{
			"error": err.Error(),
		})
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) revokeEphemeralPlaygroundKeys(r *http.Request, principalID string) error {
	if strings.TrimSpace(principalID) == "" {
		return nil
	}
	ctx := r.Context()
	now := s.nowMs()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, org_id FROM virtual_keys
		WHERE owner_principal_id = ? AND is_ephemeral_playground = 1
		  AND state IN ('CREATED_ACTIVE', 'ROTATING_DUAL_VALID', 'QUOTA_PAUSED', 'SUSPENDED')`,
		principalID,
	)
	if err != nil {
		return err
	}
	type keyRef struct{ id, orgID string }
	var keys []keyRef
	for rows.Next() {
		var k keyRef
		if scanErr := rows.Scan(&k.id, &k.orgID); scanErr != nil {
			_ = rows.Close()
			return scanErr
		}
		keys = append(keys, k)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		_ = rows.Close()
		return rowsErr
	}
	_ = rows.Close()
	if len(keys) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var committedEpoch int64
	reqIDPtr := ptrOrNil(requestIDFromContext(ctx))
	for _, k := range keys {
		if _, err := tx.ExecContext(ctx, `
			UPDATE virtual_keys
			SET state = 'REVOKED', previous_key_hash = NULL, grace_expires_at_ms = NULL,
			    revoked_at_ms = ?, revocation_mode = 'STANDARD', version = version + 1, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND is_ephemeral_playground = 1`,
			now, now, k.orgID, k.id,
		); err != nil {
			return err
		}
		resEpoch, err := tx.ExecContext(ctx, `
			INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
			VALUES (?, 'virtual_key', ?, ?, 0, ?)`,
			k.orgID, k.id, port.MutationRevoke, now,
		)
		if err != nil {
			return err
		}
		if epoch, epochErr := resEpoch.LastInsertId(); epochErr == nil && epoch > committedEpoch {
			committedEpoch = epoch
		}
		audID, audErr := idgen.New(idgen.PrefixAudit)
		if audErr != nil {
			return audErr
		}
		metaJSON, _ := json.Marshal(map[string]any{
			"mode":   vkey.RevokeModeStandard,
			"reason": "logout_ephemeral_playground_revoke",
		})
		if err := s.txAudit.AppendTx(ctx, tx, port.AuditEvent{
			ID:           audID,
			CreatedAtMs:  now,
			OrgID:        &k.orgID,
			ActorKind:    "PRINCIPAL",
			ActorID:      principalID,
			EntryPoint:   port.AuditEntryPointAdminAPI,
			EventType:    "virtual_key.revoked",
			TargetType:   "virtual_key",
			TargetID:     k.id,
			RequestID:    reqIDPtr,
			MetadataJSON: string(metaJSON),
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.snapshots != nil {
		if streams := s.snapshots.Streams(); streams != nil {
			for _, k := range keys {
				streams.AbortKey(k.id)
			}
		}
		if refErr := s.snapshots.Refresh(ctx); refErr != nil {
			for _, k := range keys {
				s.snapshots.FenceKeyRevocation(k.id, vkey.RevokeModeStandard, now, committedEpoch)
				if s.audit != nil {
					if audID, aerr := idgen.New(idgen.PrefixAudit); aerr == nil {
						metaJSON, _ := json.Marshal(map[string]any{
							"error":           refErr.Error(),
							"committed_epoch": committedEpoch,
						})
						_ = s.audit.Append(ctx, port.AuditEvent{
							ID:           audID,
							CreatedAtMs:  now,
							OrgID:        &k.orgID,
							ActorKind:    "PRINCIPAL",
							ActorID:      principalID,
							EntryPoint:   port.AuditEntryPointAdminAPI,
							EventType:    "virtual_key.snapshot_sync_pending",
							TargetType:   "virtual_key",
							TargetID:     k.id,
							RequestID:    reqIDPtr,
							MetadataJSON: string(metaJSON),
						})
					}
				}
			}
			return refErr
		}
	}
	return nil
}

func collectUniqueActorRoles(actor *rbac.Actor) []string {
	if actor == nil {
		return []string{}
	}
	seen := make(map[string]struct{}, len(actor.Bindings))
	out := make([]string, 0, len(actor.Bindings))
	for _, b := range actor.Bindings {
		if b.RoleName == "" {
			continue
		}
		if _, ok := seen[b.RoleName]; !ok {
			seen[b.RoleName] = struct{}{}
			out = append(out, b.RoleName)
		}
	}
	return out
}

func nonEmptyRoleSlice(roles []string) []string {
	if len(roles) == 0 {
		return []string{}
	}
	out := make([]string, len(roles))
	copy(out, roles)
	return out
}
