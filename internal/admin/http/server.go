// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package http implements Tanpopo's Control Plane Administration REST API
// (/admin/v1/*) matching openapi/admin-v1.yaml (SPEC-API-01 / TP-IMP-0024 / TP-IMP-0025).
//
// Security & Concurrency Invariants:
//   - Fail-closed startup via rbac.ValidateRoutePermissions on the single
//     canonical route table backing both HTTP dispatch and introspection (AUTH-11).
//   - Strict plane isolation rejecting Data Plane Virtual Keys (vk_tp_*) on
//     every /admin/v1/* endpoint with 401 invalid_credential_plane (AUTH-07).
//   - Session cookie + double-submit X-CSRF-Token & Origin enforcement on
//     state-mutating browser requests (AUTH-03), alongside pluggable Bearer auth.
//   - Top-level JSON body org_id anti-spoofing rejection (403) prioritizing
//     authenticated session org context via rbac.ResolveSessionOrgID (AUTH-13).
//   - Uniform 404 resource_not_found across tenants (AUTH-15), ETag/"vN"
//     + If-Match 428/409 optimistic locking with current_resource reconciliation,
//     and HMAC-SHA256 org-bound keyset cursor pagination (GAP-11).
package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/bootstrap"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	// RequestIDHeader carries the Control Plane request correlation ID.
	RequestIDHeader = "X-Tanpopo-Request-Id"
	// DefaultMaxRequestBodyBytes caps Control Plane JSON bodies at 2 MiB.
	DefaultMaxRequestBodyBytes int64 = 2 << 20
	// DefaultServerVersion reports the server build identifier when unconfigured.
	DefaultServerVersion = "0.1.0"
)

// ErrInvalidServerConfig indicates missing or invalid Admin Server configuration.
var ErrInvalidServerConfig = errors.New("admin: invalid server configuration")

// BearerTokenVerifier validates AdminBearerToken credentials (ADMIN_SERVICE_TOKEN / ADMIN_OIDC).
type BearerTokenVerifier func(ctx context.Context, rawToken string) (*rbac.Actor, *SessionMetadata, error)

// SessionMetadata describes non-cookie Admin session metadata resolved by BearerTokenVerifier.
type SessionMetadata struct {
	Email       string
	ExpiresAtMs int64
}

type rawDBProvider interface {
	RawDB() *sql.DB
}

// ServerConfig configures the Control Plane Admin HTTP server fail-closed.
type ServerConfig struct {
	Store               port.Store
	DB                  *sql.DB
	Bootstrap           *bootstrap.Manager
	Auth                *auth.LocalService
	VKeys               *vkey.Service
	Vault               *vault.Store
	Snapshots           *snapshot.Manager
	KillSwitch          *killswitch.Registry
	Audit               port.AuditSink
	NowMs               func() int64
	MaxRequestBodyBytes int64
	PaginationSecret    []byte
	TokenVerifier       BearerTokenVerifier
	HostResolver        egress.HostResolver
	BlockedAdminPorts   []int
	OfflineMode         bool
	Version             string
}

type routeEntry struct {
	Method  string
	Path    string
	Public  bool
	Action  rbac.Action
	Handler func(w http.ResponseWriter, r *http.Request)
}

// Server serves /admin/v1/* Control Plane REST endpoints.
type Server struct {
	db            *sql.DB
	storage       port.Store
	bootstrap     *bootstrap.Manager
	auth          *auth.LocalService
	vkeys         *vkey.Service
	vault         *vault.Store
	snapshots     *snapshot.Manager
	killSwitch    *killswitch.Registry
	audit         port.AuditSink
	txAudit       port.TxAuditSink
	nowMs         func() int64
	maxBodyBytes  int64
	cursorSecret  []byte
	tokenVerifier BearerTokenVerifier
	hostResolver  egress.HostResolver
	blockedPorts  map[int]bool
	offlineMode   bool
	version       string

	routes  []routeEntry
	handler http.Handler
}

// NewServer validates Control Plane dependencies, enforces explicit RBAC route
// metadata via rbac.ValidateRoutePermissions, and constructs the HTTP handler.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: non-nil port.Store is required", ErrInvalidServerConfig)
	}
	db := cfg.DB
	if db == nil {
		if provider, ok := cfg.Store.(rawDBProvider); ok {
			db = provider.RawDB()
		}
	}
	if db == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB (or RawDB provider) is required", ErrInvalidServerConfig)
	}
	if cfg.Bootstrap == nil {
		return nil, fmt.Errorf("%w: non-nil *bootstrap.Manager is required", ErrInvalidServerConfig)
	}
	if cfg.Auth == nil {
		return nil, fmt.Errorf("%w: non-nil *auth.LocalService is required", ErrInvalidServerConfig)
	}
	if cfg.VKeys == nil {
		return nil, fmt.Errorf("%w: non-nil *vkey.Service is required", ErrInvalidServerConfig)
	}
	auditSink := cfg.Audit
	if auditSink == nil {
		auditSink = cfg.Store.Audit()
	}
	if auditSink == nil {
		return nil, fmt.Errorf("%w: non-nil port.TxAuditSink is required", ErrInvalidServerConfig)
	}
	txAudit, ok := auditSink.(port.TxAuditSink)
	if !ok {
		return nil, fmt.Errorf("%w: Audit must implement port.TxAuditSink", ErrInvalidServerConfig)
	}

	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	maxBody := cfg.MaxRequestBodyBytes
	if maxBody <= 0 {
		maxBody = DefaultMaxRequestBodyBytes
	}

	cursorSecret := make([]byte, len(cfg.PaginationSecret))
	copy(cursorSecret, cfg.PaginationSecret)
	if len(cursorSecret) < 32 {
		cursorSecret = make([]byte, 32)
		if _, err := rand.Read(cursorSecret); err != nil {
			return nil, fmt.Errorf("%w: mint pagination HMAC secret: %w", ErrInvalidServerConfig, err)
		}
	}

	version := strings.TrimSpace(cfg.Version)
	if version == "" {
		version = DefaultServerVersion
	}

	blockedPorts := make(map[int]bool, len(cfg.BlockedAdminPorts))
	for _, p := range cfg.BlockedAdminPorts {
		if p > 0 && p <= 65535 {
			blockedPorts[p] = true
		}
	}

	s := &Server{
		db:            db,
		storage:       cfg.Store,
		bootstrap:     cfg.Bootstrap,
		auth:          cfg.Auth,
		vkeys:         cfg.VKeys,
		vault:         cfg.Vault,
		snapshots:     cfg.Snapshots,
		killSwitch:    cfg.KillSwitch,
		audit:         auditSink,
		txAudit:       txAudit,
		nowMs:         nowFn,
		maxBodyBytes:  maxBody,
		cursorSecret:  cursorSecret,
		tokenVerifier: cfg.TokenVerifier,
		hostResolver:  cfg.HostResolver,
		blockedPorts:  blockedPorts,
		offlineMode:   cfg.OfflineMode,
		version:       version,
	}

	s.routes = []routeEntry{
		// Group A: Bootstrap & Session/CSRF
		{Method: http.MethodGet, Path: "/admin/v1/bootstrap/status", Public: true, Handler: s.handleBootstrapStatus},
		{Method: http.MethodPost, Path: "/admin/v1/bootstrap/complete", Public: true, Handler: s.handleBootstrapComplete},
		{Method: http.MethodPost, Path: "/admin/v1/auth/login", Public: true, Handler: s.handleAuthLogin},
		{Method: http.MethodGet, Path: "/admin/v1/auth/session", Action: rbac.ActionSessionSelf, Handler: s.handleAuthSession},
		{Method: http.MethodPost, Path: "/admin/v1/auth/logout", Action: rbac.ActionSessionSelf, Handler: s.handleAuthLogout},

		// Group B: Organizations, Projects & Environments
		{Method: http.MethodGet, Path: "/admin/v1/organizations", Action: rbac.ActionOrgRead, Handler: s.handleListOrganizations},
		{Method: http.MethodPost, Path: "/admin/v1/organizations", Action: rbac.ActionOrgCreate, Handler: s.handleCreateOrganization},
		{Method: http.MethodDelete, Path: "/admin/v1/organizations/{orgId}", Action: rbac.ActionOrgDelete, Handler: s.handleDeleteOrganization},
		{Method: http.MethodGet, Path: "/admin/v1/projects", Action: rbac.ActionProjectRead, Handler: s.handleListProjects},
		{Method: http.MethodPost, Path: "/admin/v1/projects", Action: rbac.ActionProjectWrite, Handler: s.handleCreateProject},
		{Method: http.MethodDelete, Path: "/admin/v1/projects/{projectId}", Action: rbac.ActionProjectWrite, Handler: s.handleDeleteProject},
		{Method: http.MethodGet, Path: "/admin/v1/projects/{projectId}/environments", Action: rbac.ActionProjectRead, Handler: s.handleListProjectEnvironments},
		{Method: http.MethodPost, Path: "/admin/v1/projects/{projectId}/environments", Action: rbac.ActionEnvironmentWrite, Handler: s.handleCreateProjectEnvironment},

		// Group C: RBAC Members & Virtual Keys
		{Method: http.MethodGet, Path: "/admin/v1/members", Action: rbac.ActionMemberRead, Handler: s.handleListMembers},
		{Method: http.MethodPost, Path: "/admin/v1/members", Action: rbac.ActionMemberWrite, Handler: s.handleCreateMember},
		{Method: http.MethodPatch, Path: "/admin/v1/members/{memberId}/roles", Action: rbac.ActionMemberWrite, Handler: s.handleUpdateMemberRoles},
		{Method: http.MethodGet, Path: "/admin/v1/virtual-keys", Action: rbac.ActionVKeyRead, Handler: s.handleListVirtualKeys},
		{Method: http.MethodPost, Path: "/admin/v1/virtual-keys", Action: rbac.ActionVKeyCreate, Handler: s.handleCreateVirtualKey},
		{Method: http.MethodPost, Path: "/admin/v1/projects/{projectId}/virtual-keys", Action: rbac.ActionVKeyCreate, Handler: s.handleCreateProjectVirtualKey},
		{Method: http.MethodPatch, Path: "/admin/v1/virtual-keys/{keyId}", Action: rbac.ActionVKeyCreate, Handler: s.handleUpdateVirtualKey},
		{Method: http.MethodPost, Path: "/admin/v1/virtual-keys/{keyId}/rotate", Action: rbac.ActionVKeyRotate, Handler: s.handleRotateVirtualKey},
		{Method: http.MethodPost, Path: "/admin/v1/virtual-keys/{keyId}/revoke", Action: rbac.ActionVKeyRevoke, Handler: s.handleRevokeVirtualKey},
	}

	if err := rbac.ValidateRoutePermissions(s.Routes()); err != nil {
		return nil, err
	}

	s.handler = s.buildHandler()
	return s, nil
}

// Routes returns the complete explicit RBAC permission table backing all registered routes (AUTH-11).
func (s *Server) Routes() []rbac.RoutePermissionSpec {
	out := make([]rbac.RoutePermissionSpec, len(s.routes))
	for i, r := range s.routes {
		out[i] = rbac.RoutePermissionSpec{
			Method: r.Method,
			Path:   r.Path,
			Public: r.Public,
			Action: r.Action,
		}
	}
	return out
}

// ServeHTTP dispatches Control Plane HTTP requests through outer request ID,
// panic recovery, body-cap, credential-plane isolation, and route guards.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) buildHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := resolveOrMintRequestID(r.Header.Get(RequestIDHeader))
		w.Header().Set(RequestIDHeader, reqID)
		r = r.WithContext(contextWithRequestID(r.Context(), reqID))

		defer func() {
			if rec := recover(); rec != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "internal server error", false, nil)
			}
		}()

		// AUTH-07: Data Plane Virtual Keys (vk_tp_*) must NEVER be accepted on /admin/v1/*.
		if hasDataPlaneCredential(r) {
			writeErrorEnvelope(w, r, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "data plane virtual key is not accepted on control plane endpoints", false, nil)
			return
		}

		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
		}

		matched, allowedMethods := s.matchRoute(r.Method, r.URL.Path, r)
		if matched == nil {
			if len(allowedMethods) > 0 {
				w.Header().Set("Allow", strings.Join(allowedMethods, ", "))
				writeErrorEnvelope(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "method not allowed", false, nil)
				return
			}
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "resource not found", false, nil)
			return
		}

		matched.Handler(w, r)
	})
}

func (s *Server) matchRoute(method, reqPath string, r *http.Request) (*routeEntry, []string) {
	normPath := strings.TrimSuffix(reqPath, "/")
	if normPath == "" {
		normPath = "/"
	}
	reqSegs := splitPathSegments(normPath)
	var allowed []string

	for i := range s.routes {
		rt := &s.routes[i]
		patSegs := splitPathSegments(rt.Path)
		if len(patSegs) != len(reqSegs) {
			continue
		}
		matched := true
		var params []struct{ key, val string }
		for j := range patSegs {
			p := patSegs[j]
			if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") && len(p) > 2 {
				val := reqSegs[j]
				if strings.TrimSpace(val) == "" {
					matched = false
					break
				}
				params = append(params, struct{ key, val string }{key: p[1 : len(p)-1], val: val})
				continue
			}
			if p != reqSegs[j] {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		allowed = append(allowed, rt.Method)
		if rt.Method == method {
			for _, kv := range params {
				r.SetPathValue(kv.key, kv.val)
			}
			return rt, nil
		}
	}
	return nil, allowed
}

func splitPathSegments(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

type reqIDCtxKey struct{}

func contextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, reqIDCtxKey{}, id)
}

func requestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(reqIDCtxKey{}).(string); ok && v != "" {
		return v
	}
	return ""
}

func resolveOrMintRequestID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" && len(trimmed) <= 40 && isSafeTokenChars(trimmed) {
		return trimmed
	}
	if id, err := idgen.New(idgen.PrefixAudit); err == nil {
		return "req_" + id[len(idgen.PrefixAudit)+1:]
	}
	return "req_fallback"
}

func isSafeTokenChars(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func hasDataPlaneCredential(r *http.Request) bool {
	if strings.HasPrefix(strings.TrimSpace(r.Header.Get("X-Tanpopo-Virtual-Key")), "vk_tp_") {
		return true
	}
	if strings.HasPrefix(strings.TrimSpace(r.Header.Get("x-api-key")), "vk_tp_") {
		return true
	}
	if strings.HasPrefix(strings.TrimSpace(r.Header.Get("x-goog-api-key")), "vk_tp_") {
		return true
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(authz, "vk_tp_") {
		return true
	}
	if len(authz) > 7 && strings.EqualFold(authz[:7], "Bearer ") {
		token := strings.TrimSpace(authz[7:])
		if strings.HasPrefix(token, "vk_tp_") {
			return true
		}
	}
	return false
}

// CanonicalError matches components.schemas.CanonicalError in openapi/admin-v1.yaml.
type CanonicalError struct {
	Code            string `json:"code"`
	Type            string `json:"type"`
	Message         string `json:"message"`
	RequestID       string `json:"request_id"`
	Retryable       bool   `json:"retryable"`
	CurrentResource any    `json:"current_resource,omitempty"`
}

// CanonicalErrorEnvelope wraps CanonicalError matching components.schemas.CanonicalErrorEnvelope.
type CanonicalErrorEnvelope struct {
	Error CanonicalError `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		http.Error(w, `{"error":{"code":"internal_error","type":"upstream_error","message":"encode error","retryable":false}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeErrorEnvelope(w http.ResponseWriter, r *http.Request, status int, code, errType, message string, retryable bool, currentResource any) {
	reqID := requestIDFromContext(r.Context())
	if reqID == "" {
		reqID = resolveOrMintRequestID(r.Header.Get(RequestIDHeader))
		w.Header().Set(RequestIDHeader, reqID)
	}
	writeJSON(w, status, CanonicalErrorEnvelope{
		Error: CanonicalError{
			Code:            code,
			Type:            errType,
			Message:         message,
			RequestID:       reqID,
			Retryable:       retryable,
			CurrentResource: currentResource,
		},
	})
}

type authSessionContext struct {
	Actor       *rbac.Actor
	Email       string
	ExpiresAtMs int64
	CSRFToken   string
	OrgID       string
	IsOperator  bool
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*authSessionContext, bool) {
	if hasDataPlaneCredential(r) {
		writeErrorEnvelope(w, r, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "data plane virtual key is not accepted on control plane endpoints", false, nil)
		return nil, false
	}

	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authz) > 7 && strings.EqualFold(authz[:7], "Bearer ") {
		rawToken := strings.TrimSpace(authz[7:])
		if rawToken == "" || s.tokenVerifier == nil {
			writeErrorEnvelope(w, r, http.StatusUnauthorized, "unauthenticated", "authentication_error", "invalid or unsupported bearer token", false, nil)
			return nil, false
		}
		actor, meta, err := s.tokenVerifier(r.Context(), rawToken)
		if err != nil || actor == nil {
			s.writeAuthError(w, r, err)
			return nil, false
		}
		email := "service-account"
		expMs := s.nowMs() + int64(time.Hour/time.Millisecond)
		if meta != nil {
			if strings.TrimSpace(meta.Email) != "" {
				email = meta.Email
			}
			if meta.ExpiresAtMs > 0 {
				expMs = meta.ExpiresAtMs
			}
		}
		return s.buildSessionContext(actor, email, "", expMs), true
	}

	actor, sess, err := s.auth.AuthenticateAdminHTTPRequest(r)
	if err != nil {
		s.writeAuthError(w, r, err)
		return nil, false
	}
	return s.buildSessionContext(actor, sess.Email, sess.CSRFToken, sess.ExpiresAtMs), true
}

func (s *Server) buildSessionContext(actor *rbac.Actor, email, csrf string, expiresAtMs int64) *authSessionContext {
	orgID, _ := rbac.ResolveSessionOrgID(actor, "")
	return &authSessionContext{
		Actor:       actor,
		Email:       email,
		ExpiresAtMs: expiresAtMs,
		CSRFToken:   csrf,
		OrgID:       orgID,
		IsOperator:  actorHasOperator(actor),
	}
}

func actorHasOperator(actor *rbac.Actor) bool {
	if actor == nil {
		return false
	}
	for _, b := range actor.Bindings {
		if b.ScopeType == rbac.ScopeInstance && b.RoleName == rbac.RoleOperator && b.OrgID == nil {
			return true
		}
	}
	return false
}

func actorHasOrgScopeBinding(actor *rbac.Actor, orgID string) bool {
	if actorHasOperator(actor) {
		return true
	}
	if actor == nil || orgID == "" {
		return false
	}
	for _, b := range actor.Bindings {
		if b.ScopeType == rbac.ScopeOrg && b.OrgID != nil && *b.OrgID == orgID {
			return true
		}
	}
	return false
}

func actorProjectIDs(actor *rbac.Actor, orgID string) []string {
	if actor == nil || orgID == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, b := range actor.Bindings {
		if b.ScopeType == rbac.ScopeProject && b.OrgID != nil && *b.OrgID == orgID && b.ScopeID != "" {
			if _, ok := seen[b.ScopeID]; !ok {
				seen[b.ScopeID] = struct{}{}
				out = append(out, b.ScopeID)
			}
		}
	}
	return out
}

func (s *Server) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentialPlane), errors.Is(err, rbac.ErrInvalidCredentialPlane):
		writeErrorEnvelope(w, r, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "credential belongs to wrong plane", false, nil)
	case errors.Is(err, auth.ErrCSRFValidationFailed):
		writeErrorEnvelope(w, r, http.StatusForbidden, "csrf_validation_failed", "permission_error", "CSRF token or origin validation failed", false, nil)
	case errors.Is(err, auth.ErrPrincipalInactive):
		writeErrorEnvelope(w, r, http.StatusUnauthorized, "principal_inactive", "authentication_error", "administrator principal is suspended or disabled", false, nil)
	case errors.Is(err, rbac.ErrPermissionDenied):
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "insufficient role permission", false, nil)
	case errors.Is(err, rbac.ErrResourceNotFound):
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "resource not found", false, nil)
	default:
		writeErrorEnvelope(w, r, http.StatusUnauthorized, "unauthenticated", "authentication_error", "authentication required", false, nil)
	}
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request, sc *authSessionContext, action rbac.Action, target rbac.TargetScope) bool {
	err := rbac.Authorize(sc.Actor, action, target)
	if err == nil {
		return true
	}
	switch {
	case errors.Is(err, rbac.ErrInvalidCredentialPlane):
		writeErrorEnvelope(w, r, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "invalid credential plane", false, nil)
	case errors.Is(err, rbac.ErrUnauthenticated):
		writeErrorEnvelope(w, r, http.StatusUnauthorized, "unauthenticated", "authentication_error", "authentication required", false, nil)
	case errors.Is(err, rbac.ErrResourceNotFound):
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "resource not found", false, nil)
	default:
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "insufficient role permission", false, nil)
	}
	return false
}

// decodeJSONAndCheckOrgSpoofing decodes the request body into dst while enforcing
// AUTH-13 anti-spoofing: if top-level JSON contains "org_id" differing from the
// caller's authenticated organization, immediately fail with 403 and emit a security audit event.
func (s *Server) decodeJSONAndCheckOrgSpoofing(w http.ResponseWriter, r *http.Request, sc *authSessionContext, dst any) bool {
	if r.Body == nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "request body is required", false, nil)
		return false
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeErrorEnvelope(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "invalid_request_error", "request body exceeds maximum allowed size", false, nil)
			return false
		}
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "failed reading request body", false, nil)
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "request body cannot be empty", false, nil)
		return false
	}

	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &topLevel); err != nil || topLevel == nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "request body must be a JSON object", false, nil)
		return false
	}

	if sc != nil {
		if rawOrg, hasOrg := topLevel["org_id"]; hasOrg && !bytes.Equal(bytes.TrimSpace(rawOrg), []byte("null")) {
			var bodyOrg string
			if err := json.Unmarshal(rawOrg, &bodyOrg); err != nil {
				writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "org_id must be a string", false, nil)
				return false
			}
			bodyOrg = strings.TrimSpace(bodyOrg)
			if bodyOrg != "" && bodyOrg != sc.OrgID {
				s.emitSecurityAudit(r.Context(), sc, "security.org_id_spoofing_blocked", map[string]any{
					"spoofed_org_id": bodyOrg,
					"session_org_id": sc.OrgID,
					"path":           r.URL.Path,
				})
				writeErrorEnvelope(w, r, http.StatusForbidden, "org_id_spoofing_forbidden", "permission_error", "request body org_id cannot override authenticated session organization", false, nil)
				return false
			}
		}
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	if err := dec.Decode(dst); err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "malformed JSON request payload", false, nil)
		return false
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "trailing data after JSON object is forbidden", false, nil)
		return false
	}
	return true
}

func decodePublicJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "request body is required", false, nil)
		return false
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeErrorEnvelope(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "invalid_request_error", "request body exceeds maximum allowed size", false, nil)
			return false
		}
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "failed reading request body", false, nil)
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "request body cannot be empty", false, nil)
		return false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "request body must be a JSON object", false, nil)
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	if err := dec.Decode(dst); err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "malformed JSON request payload", false, nil)
		return false
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "trailing data after JSON object is forbidden", false, nil)
		return false
	}
	return true
}

func (s *Server) emitSecurityAudit(ctx context.Context, sc *authSessionContext, eventType string, meta map[string]any) {
	if s.audit == nil {
		return
	}
	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return
	}
	var orgPtr *string
	actorKind := "SYSTEM"
	actorID := "admin_http"
	if sc != nil {
		if sc.OrgID != "" {
			v := sc.OrgID
			orgPtr = &v
		}
		if sc.Actor != nil && sc.Actor.PrincipalID != "" {
			actorKind = "PRINCIPAL"
			actorID = sc.Actor.PrincipalID
		}
	}
	rawMeta, _ := json.Marshal(meta)
	_ = s.audit.Append(ctx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  s.nowMs(),
		OrgID:        orgPtr,
		ActorKind:    actorKind,
		ActorID:      actorID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    eventType,
		TargetType:   "control_plane",
		TargetID:     actorID,
		RequestID:    ptrOrNil(requestIDFromContext(ctx)),
		MetadataJSON: string(rawMeta),
	})
}

func ptrOrNil(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	c := v
	return &c
}

func formatTimeRFC3339Ms(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
}

func sha256HexString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func validateEndpointSSRF(ctx context.Context, rawURL string, allowPrivateIP bool, resolver egress.HostResolver, blockedPorts map[int]bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("%w: valid http or https endpoint_url required", port.ErrInvalidInput)
	}
	portNum := 443
	if parsed.Scheme == "http" {
		portNum = 80
	}
	if p := parsed.Port(); p != "" {
		var pv int
		if _, serr := fmt.Sscanf(p, "%d", &pv); serr != nil || pv < 1 || pv > 65535 {
			return fmt.Errorf("%w: invalid endpoint port", port.ErrInvalidInput)
		}
		portNum = pv
	}
	if blockedPorts[portNum] {
		return egress.ErrPortDenied
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if ip, perr := netip.ParseAddr(host); perr == nil {
		return classifyEndpointIP(ip, allowPrivateIP)
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal" {
		if host == "metadata.google.internal" {
			return egress.ErrMetadataBlocked
		}
		if !allowPrivateIP {
			return egress.ErrPrivateIPBlocked
		}
	}
	if resolver != nil {
		addrs, err := resolver.LookupNetIP(ctx, "ip", host)
		if err == nil {
			for _, ip := range addrs {
				if cerr := classifyEndpointIP(ip, allowPrivateIP); cerr != nil {
					return cerr
				}
			}
		}
	}
	return nil
}

func classifyEndpointIP(ip netip.Addr, allowPrivateIP bool) error {
	ip = ip.Unmap()
	metaPrefixes := []netip.Prefix{
		netip.MustParsePrefix("169.254.0.0/16"),
		netip.MustParsePrefix("100.100.100.200/32"),
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("::/128"),
		netip.MustParsePrefix("fe80::/10"),
		netip.MustParsePrefix("fd00:ec2::254/128"),
		netip.MustParsePrefix("224.0.0.0/4"),
		netip.MustParsePrefix("ff00::/8"),
	}
	for _, pfx := range metaPrefixes {
		if pfx.Contains(ip) {
			return egress.ErrMetadataBlocked
		}
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		if !allowPrivateIP {
			return egress.ErrPrivateIPBlocked
		}
	}
	cgnat := netip.MustParsePrefix("100.64.0.0/10")
	if cgnat.Contains(ip) && !allowPrivateIP {
		return egress.ErrPrivateIPBlocked
	}
	return nil
}
