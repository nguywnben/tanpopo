// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/oidc"
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
	PrincipalID     string   `json:"principal_id"`
	Email           string   `json:"email"`
	OrgID           *string  `json:"org_id"`
	Roles           []string `json:"roles"`
	CSRFToken       string   `json:"csrf_token"`
	ExpiresAt       string   `json:"expires_at"`
	MultiOrgEnabled bool     `json:"multi_org_enabled"`
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
		PrincipalID:     info.PrincipalID,
		Email:           info.Email,
		OrgID:           info.OrgID,
		Roles:           nonEmptyRoleSlice(info.Roles),
		CSRFToken:       info.CSRFToken,
		ExpiresAt:       formatTimeRFC3339Ms(info.ExpiresAtMs),
		MultiOrgEnabled: !s.multiOrgDisabled,
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
		PrincipalID:     sc.Actor.PrincipalID,
		Email:           sc.Email,
		OrgID:           sc.Actor.SessionOrgID,
		Roles:           roles,
		CSRFToken:       sc.CSRFToken,
		ExpiresAt:       formatTimeRFC3339Ms(sc.ExpiresAtMs),
		MultiOrgEnabled: !s.multiOrgDisabled,
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
	tExpr := port.BoolExpr(s.dialect, true)

	type keyRef struct{ id, orgID string }
	var keys []keyRef

	if s.dialect == port.DialectPostgres {
		if err := s.withOperatorTx(ctx, func(tx *sql.Tx) error {
			orgIDs, err := port.ListOrgIDsTx(ctx, tx, s.dialect)
			if err != nil {
				return err
			}
			findQ := s.rebind(`
				SELECT id FROM virtual_keys
				WHERE org_id = ? AND owner_principal_id = ? AND is_ephemeral_playground = ` + tExpr + `
				  AND state IN ('CREATED_ACTIVE', 'ROTATING_DUAL_VALID', 'QUOTA_PAUSED', 'SUSPENDED')`)
			for _, oid := range orgIDs {
				if err := port.SetOrgRLSTx(ctx, tx, s.dialect, oid); err != nil {
					return err
				}
				rows, err := tx.QueryContext(ctx, findQ, oid, principalID)
				if err != nil {
					return err
				}
				for rows.Next() {
					var id string
					if serr := rows.Scan(&id); serr != nil {
						_ = rows.Close()
						return serr
					}
					keys = append(keys, keyRef{id: id, orgID: oid})
				}
				rerr := rows.Err()
				_ = rows.Close()
				if rerr != nil {
					return rerr
				}
			}
			return nil
		}); err != nil {
			return err
		}
	} else {
		if err := s.withOperatorReadTx(ctx, func(q adminDBQuerier) error {
			rows, qerr := q.QueryContext(ctx, s.rebind(`
			SELECT id, org_id FROM virtual_keys
			WHERE owner_principal_id = ? AND is_ephemeral_playground = `+port.BoolExpr(s.dialect, true)+`
			  AND state IN ('CREATED_ACTIVE', 'ROTATING_DUAL_VALID', 'QUOTA_PAUSED', 'SUSPENDED')`),
				principalID,
			)
			if qerr != nil {
				return qerr
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var k keyRef
				if scanErr := rows.Scan(&k.id, &k.orgID); scanErr != nil {
					return scanErr
				}
				keys = append(keys, k)
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				return rowsErr
			}
			return rows.Close()
		}); err != nil {
			return err
		}
	}
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
	updQ := s.rebind(`
		UPDATE virtual_keys
		SET state = 'REVOKED', previous_key_hash = NULL, grace_expires_at_ms = NULL,
		    revoked_at_ms = ?, revocation_mode = 'STANDARD', version = version + 1, updated_at_ms = ?
		WHERE org_id = ? AND id = ? AND is_ephemeral_playground = ` + tExpr)
	for _, k := range keys {
		if err := port.SetOrgRLSTx(ctx, tx, s.dialect, k.orgID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, updQ, now, now, k.orgID, k.id); err != nil {
			return err
		}
		epoch, err := s.appendChangelogTx(ctx, tx, k.orgID, "virtual_key", k.id, port.MutationRevoke, false, now)
		if err != nil {
			return err
		}
		if epoch > committedEpoch {
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

func (s *Server) oidcRequestIsHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return s.isTrustedProxy(r.RemoteAddr) && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func (s *Server) oidcStateCookieName(r *http.Request) string {
	if s.oidcRequestIsHTTPS(r) {
		return "__Host-tanpopo_oidc_state"
	}
	return "tanpopo_oidc_state"
}

func (s *Server) computeOIDCStateCookieMAC(state string) string {
	mac := hmac.New(sha256.New, s.cursorSecret)
	_, _ = mac.Write([]byte("tanpopo-oidc-state\x00" + state))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) handleAuthOIDCStart(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "oidc_not_configured", "api_error", "enterprise OIDC SSO is not configured", true, nil)
		return
	}
	authURL, stateVal, err := s.oidc.StartAuth(r.Context())
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "oidc_start_failed", "upstream_error", "failed initializing OIDC login flow", true, nil)
		return
	}
	isHTTPS := s.oidcRequestIsHTTPS(r)
	http.SetCookie(w, &http.Cookie{
		Name:     s.oidcStateCookieName(r),
		Value:    s.computeOIDCStateCookieMAC(stateVal),
		Path:     "/admin/v1/auth/oidc/callback",
		HttpOnly: true,
		Secure:   isHTTPS,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleAuthOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "oidc_not_configured", "api_error", "enterprise OIDC SSO is not configured", true, nil)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	isHTTPS := s.oidcRequestIsHTTPS(r)
	cookieName := s.oidcStateCookieName(r)
	stateCookie, cerr := r.Cookie(cookieName)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/admin/v1/auth/oidc/callback",
		HttpOnly: true,
		Secure:   isHTTPS,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	if cerr != nil || state == "" {
		writeErrorEnvelope(w, r, http.StatusUnauthorized, "invalid_oidc_token", "authentication_error", "missing or invalid OIDC login state cookie", false, nil)
		return
	}
	expectedMAC := s.computeOIDCStateCookieMAC(state)
	if !hmac.Equal([]byte(strings.TrimSpace(stateCookie.Value)), []byte(expectedMAC)) {
		writeErrorEnvelope(w, r, http.StatusUnauthorized, "invalid_oidc_token", "authentication_error", "OIDC login state binding mismatch", false, nil)
		return
	}
	res, err := s.oidc.HandleCallback(r.Context(), code, state)
	if err != nil {
		switch {
		case errors.Is(err, oidc.ErrInvalidOIDCToken), errors.Is(err, oidc.ErrInvalidOIDCState):
			writeErrorEnvelope(w, r, http.StatusUnauthorized, "invalid_oidc_token", "authentication_error", "invalid or untrusted OIDC identity token", false, nil)
		case errors.Is(err, oidc.ErrGroupMappingDenied):
			writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "OIDC identity has no mapped organization role", false, nil)
		case errors.Is(err, auth.ErrPrincipalInactive):
			writeErrorEnvelope(w, r, http.StatusForbidden, "principal_inactive", "permission_error", "OIDC principal is suspended or disabled", false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "oidc_callback_failed", "upstream_error", "failed completing OIDC SSO login", true, nil)
		}
		return
	}
	http.SetCookie(w, res.Cookie)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", "/overview")
	w.WriteHeader(http.StatusFound)
}

func (s *Server) handleAuthOIDCRefresh(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionSessionSelf, rbac.TargetScope{OrgID: sc.OrgID}) {
		return
	}
	if sc.Actor.CredentialPlane != rbac.CredentialPlaneAdminOIDC {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "OIDC session refresh requires an OIDC-authenticated session", false, nil)
		return
	}
	if s.oidc == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "oidc_not_configured", "api_error", "enterprise OIDC SSO is not configured", true, nil)
		return
	}
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if strings.TrimSpace(req.RefreshToken) == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "refresh_token is required", false, nil)
		return
	}
	res, err := s.oidc.RefreshSession(r.Context(), sc.Actor.PrincipalID, req.RefreshToken)
	if err != nil {
		switch {
		case errors.Is(err, oidc.ErrInvalidOIDCToken):
			writeErrorEnvelope(w, r, http.StatusUnauthorized, "invalid_oidc_token", "authentication_error", "invalid or expired OIDC token", false, nil)
		case errors.Is(err, oidc.ErrGroupMappingDenied):
			writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "OIDC identity has no mapped organization role", false, nil)
		case errors.Is(err, auth.ErrPrincipalInactive):
			writeErrorEnvelope(w, r, http.StatusForbidden, "principal_inactive", "permission_error", "OIDC principal is suspended or disabled", false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "oidc_refresh_failed", "upstream_error", "failed refreshing OIDC session", true, nil)
		}
		return
	}
	http.SetCookie(w, res.Cookie)
	writeJSON(w, http.StatusOK, adminSessionResponse{
		PrincipalID:     res.Session.PrincipalID,
		Email:           res.Session.Email,
		OrgID:           res.Session.OrgID,
		Roles:           nonEmptyRoleSlice(res.Session.Roles),
		MultiOrgEnabled: !s.multiOrgDisabled,
		CSRFToken:       res.Session.CSRFToken,
		ExpiresAt:       formatTimeRFC3339Ms(res.Session.ExpiresAtMs),
	})
}

