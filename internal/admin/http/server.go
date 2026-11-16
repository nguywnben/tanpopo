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
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	extport "github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/bootstrap"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
	"github.com/nguywnben/tanpopo/internal/security/broker"
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
	// DefaultProbeRateLimitPerMinute caps POST /connections/{id}/test per (org, principal) per minute.
	DefaultProbeRateLimitPerMinute = 20
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

// MarketplaceCatalogEntry describes an available marketplace extension package for GET /marketplace/catalog.
type MarketplaceCatalogEntry struct {
	PackageID            string   `json:"package_id"`
	CanonicalName        string   `json:"canonical_name"`
	Version              string   `json:"version"`
	ExecutionTier        string   `json:"execution_tier"`
	OriginTrustTier      string   `json:"origin_trust_tier"`
	EnterpriseSigned     bool     `json:"enterprise_signed"`
	SourceURI            string   `json:"source_uri"`
	ExpectedSHA256       string   `json:"expected_sha256"`
	Summary              string   `json:"summary,omitempty"`
	Capabilities         []string `json:"capabilities,omitempty"`
}

// ExtensionArchiveResolver resolves staged archive streams + provenance metadata for Control Plane installs/upgrades.
type ExtensionArchiveResolver func(ctx context.Context, sourceURI, expectedSHA256 string) (*installer.StageInstallRequest, func(), error)

type rawDBProvider interface {
	RawDB() *sql.DB
}

// ServerConfig configures the Control Plane Admin HTTP server fail-closed.
type ServerConfig struct {
	Store                    port.Store
	DB                       *sql.DB
	Bootstrap                *bootstrap.Manager
	Auth                     *auth.LocalService
	VKeys                    *vkey.Service
	Vault                    *vault.Store
	Ledgers                  *ledger.Service
	Snapshots                *snapshot.Manager
	KillSwitch               *killswitch.Registry
	Catalog                  *catalog.Repository
	Installer                *installer.LifecycleService
	Policies                 *policyengine.Store
	RouteResolver            *resolver.Resolver
	Broker                   *broker.BlindHostCredentialBroker
	ProviderRuntime          extport.ProviderRuntime
	MarketplaceCatalog       []MarketplaceCatalogEntry
	ArchiveResolver          ExtensionArchiveResolver
	LockfilePath             string
	ProbeRateLimitPerMinute  int
	Audit                    port.AuditSink
	NowMs                    func() int64
	MaxRequestBodyBytes      int64
	PaginationSecret         []byte
	TokenVerifier            BearerTokenVerifier
	HostResolver             egress.HostResolver
	BlockedAdminPorts        []int
	OfflineMode              bool
	Version                  string
}

type routeEntry struct {
	Method  string
	Path    string
	Public  bool
	Action  rbac.Action
	Handler func(w http.ResponseWriter, r *http.Request)
}

const (
	maxProbeLimiterBuckets      = 4096
	probeLimiterSweepThreshold  = 256
)

type slidingWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  int64
	buckets map[string][]int64
}

func newSlidingWindowLimiter(limit int, window time.Duration) *slidingWindowLimiter {
	if limit <= 0 {
		limit = DefaultProbeRateLimitPerMinute
	}
	return &slidingWindowLimiter{
		limit:   limit,
		window:  window.Milliseconds(),
		buckets: make(map[string][]int64),
	}
}

func (l *slidingWindowLimiter) Allow(key string, nowMs int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := nowMs - l.window
	if len(l.buckets) > probeLimiterSweepThreshold {
		for k, times := range l.buckets {
			kept := times[:0]
			for _, ts := range times {
				if ts > cutoff {
					kept = append(kept, ts)
				}
			}
			if len(kept) == 0 {
				delete(l.buckets, k)
			} else {
				l.buckets[k] = kept
			}
		}
	}
	prev := l.buckets[key]
	kept := prev[:0]
	for _, ts := range prev {
		if ts > cutoff {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= l.limit {
		l.buckets[key] = kept
		return false
	}
	if _, exists := l.buckets[key]; !exists && len(l.buckets) >= maxProbeLimiterBuckets {
		var oldestKey string
		var oldestTS int64 = nowMs + 1
		for k, times := range l.buckets {
			last := int64(0)
			if len(times) > 0 {
				last = times[len(times)-1]
			}
			if oldestKey == "" || last < oldestTS {
				oldestKey = k
				oldestTS = last
			}
		}
		if oldestKey != "" {
			delete(l.buckets, oldestKey)
		}
	}
	kept = append(kept, nowMs)
	l.buckets[key] = kept
	return true
}

// Server serves /admin/v1/* Control Plane REST endpoints.
type Server struct {
	db              *sql.DB
	storage         port.Store
	bootstrap       *bootstrap.Manager
	auth            *auth.LocalService
	vkeys           *vkey.Service
	vault           *vault.Store
	ledgers         *ledger.Service
	snapshots       *snapshot.Manager
	killSwitch      *killswitch.Registry
	catalog         *catalog.Repository
	installer       *installer.LifecycleService
	policies        *policyengine.Store
	routeResolver   *resolver.Resolver
	broker          *broker.BlindHostCredentialBroker
	providerRuntime extport.ProviderRuntime
	marketplace     []MarketplaceCatalogEntry
	archiveResolver ExtensionArchiveResolver
	lockfilePath    string
	probeLimiter    *slidingWindowLimiter
	audit           port.AuditSink
	txAudit         port.TxAuditSink
	nowMs           func() int64
	maxBodyBytes    int64
	cursorSecret    []byte
	tokenVerifier   BearerTokenVerifier
	hostResolver    egress.HostResolver
	blockedPorts    map[int]bool
	offlineMode     bool
	version         string

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

	if len(cfg.PaginationSecret) > 0 && len(cfg.PaginationSecret) < 32 {
		return nil, fmt.Errorf("%w: PaginationSecret must be at least 32 bytes when provided", ErrInvalidServerConfig)
	}
	var cursorSecret []byte
	if len(cfg.PaginationSecret) == 0 {
		cursorSecret = make([]byte, 32)
		if _, err := rand.Read(cursorSecret); err != nil {
			return nil, fmt.Errorf("%w: mint pagination HMAC secret: %w", ErrInvalidServerConfig, err)
		}
	} else {
		cursorSecret = make([]byte, len(cfg.PaginationSecret))
		copy(cursorSecret, cfg.PaginationSecret)
	}

	ledgerSvc := cfg.Ledgers
	if ledgerSvc == nil {
		var lerr error
		ledgerSvc, lerr = ledger.NewService(ledger.Config{
			DB:    db,
			Audit: auditSink,
			NowMs: nowFn,
		})
		if lerr != nil {
			return nil, fmt.Errorf("%w: initialize ledger service: %w", ErrInvalidServerConfig, lerr)
		}
	}

	catalogRepo := cfg.Catalog
	if catalogRepo == nil {
		var cerr error
		catalogRepo, cerr = catalog.NewRepository(catalog.Config{
			DB:                        db,
			NowMs:                     nowFn,
			Snapshots:                 cfg.Snapshots,
			AllowDetachedControlPlane: cfg.Snapshots == nil,
			Audit:                     auditSink,
		})
		if cerr != nil {
			return nil, fmt.Errorf("%w: initialize catalog repository: %w", ErrInvalidServerConfig, cerr)
		}
	}

	policyStore := cfg.Policies
	if policyStore == nil {
		var perr error
		policyStore, perr = policyengine.NewStore(policyengine.StoreConfig{
			DB:    db,
			Audit: auditSink,
			NowMs: nowFn,
		})
		if perr != nil {
			return nil, fmt.Errorf("%w: initialize policy store: %w", ErrInvalidServerConfig, perr)
		}
	}

	routeResolver := cfg.RouteResolver
	if routeResolver == nil {
		routeResolver = resolver.New()
	}

	hostResolver := cfg.HostResolver
	if hostResolver == nil {
		hostResolver = egress.NewStdHostResolver()
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

	marketplace := append([]MarketplaceCatalogEntry(nil), cfg.MarketplaceCatalog...)

	s := &Server{
		db:              db,
		storage:         cfg.Store,
		bootstrap:       cfg.Bootstrap,
		auth:            cfg.Auth,
		vkeys:           cfg.VKeys,
		vault:           cfg.Vault,
		ledgers:         ledgerSvc,
		snapshots:       cfg.Snapshots,
		killSwitch:      cfg.KillSwitch,
		catalog:         catalogRepo,
		installer:       cfg.Installer,
		policies:        policyStore,
		routeResolver:   routeResolver,
		broker:          cfg.Broker,
		providerRuntime: cfg.ProviderRuntime,
		marketplace:     marketplace,
		archiveResolver: cfg.ArchiveResolver,
		lockfilePath:    strings.TrimSpace(cfg.LockfilePath),
		probeLimiter:    newSlidingWindowLimiter(cfg.ProbeRateLimitPerMinute, time.Minute),
		audit:           auditSink,
		txAudit:         txAudit,
		nowMs:           nowFn,
		maxBodyBytes:    maxBody,
		cursorSecret:    cursorSecret,
		tokenVerifier:   cfg.TokenVerifier,
		hostResolver:    hostResolver,
		blockedPorts:    blockedPorts,
		offlineMode:     cfg.OfflineMode,
		version:         version,
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

		// Group E: Provider Adapters, Write-Only Secrets & Connections
		{Method: http.MethodGet, Path: "/admin/v1/provider-adapters", Action: rbac.ActionConnectionRead, Handler: s.handleListProviderAdapters},
		{Method: http.MethodGet, Path: "/admin/v1/provider-adapters/{adapterId}", Action: rbac.ActionConnectionRead, Handler: s.handleGetProviderAdapter},
		{Method: http.MethodGet, Path: "/admin/v1/secrets", Action: rbac.ActionSecretReadMeta, Handler: s.handleListSecrets},
		{Method: http.MethodPost, Path: "/admin/v1/secrets", Action: rbac.ActionSecretWrite, Handler: s.handleCreateSecret},
		{Method: http.MethodDelete, Path: "/admin/v1/secrets/{credentialId}", Action: rbac.ActionSecretWrite, Handler: s.handleDeleteSecret},
		{Method: http.MethodGet, Path: "/admin/v1/connections", Action: rbac.ActionConnectionRead, Handler: s.handleListConnections},
		{Method: http.MethodPost, Path: "/admin/v1/connections", Action: rbac.ActionConnectionWrite, Handler: s.handleCreateConnection},
		{Method: http.MethodPatch, Path: "/admin/v1/connections/{connectionId}", Action: rbac.ActionConnectionWrite, Handler: s.handleUpdateConnection},
		{Method: http.MethodDelete, Path: "/admin/v1/connections/{connectionId}", Action: rbac.ActionConnectionWrite, Handler: s.handleDeleteConnection},
		{Method: http.MethodPost, Path: "/admin/v1/connections/{connectionId}/test", Action: rbac.ActionConnectionWrite, Handler: s.handleTestConnection},

		// Group F: Logical Models & Model Deployments
		{Method: http.MethodGet, Path: "/admin/v1/models", Action: rbac.ActionModelRead, Handler: s.handleListAdminModels},
		{Method: http.MethodPost, Path: "/admin/v1/models", Action: rbac.ActionModelWrite, Handler: s.handleCreateAdminModel},
		{Method: http.MethodGet, Path: "/admin/v1/deployments", Action: rbac.ActionModelRead, Handler: s.handleListDeployments},
		{Method: http.MethodPost, Path: "/admin/v1/deployments", Action: rbac.ActionModelWrite, Handler: s.handleCreateDeployment},
		{Method: http.MethodPatch, Path: "/admin/v1/deployments/{deploymentId}", Action: rbac.ActionModelWrite, Handler: s.handleUpdateDeployment},
		{Method: http.MethodDelete, Path: "/admin/v1/deployments/{deploymentId}", Action: rbac.ActionModelWrite, Handler: s.handleDeleteDeployment},

		// Group G: Marketplace, Extensions & Lockfile (static paths before {installationId})
		{Method: http.MethodGet, Path: "/admin/v1/marketplace/catalog", Action: rbac.ActionExtensionRead, Handler: s.handleListMarketplaceCatalog},
		{Method: http.MethodPost, Path: "/admin/v1/extensions/install", Action: rbac.ActionExtensionStage, Handler: s.handleStageExtensionInstall},
		{Method: http.MethodGet, Path: "/admin/v1/extensions/installations", Action: rbac.ActionExtensionRead, Handler: s.handleListExtensionInstallations},
		{Method: http.MethodGet, Path: "/admin/v1/extensions/lockfile", Action: rbac.ActionExtensionRead, Handler: s.handleGetExtensionLockfile},
		{Method: http.MethodGet, Path: "/admin/v1/extensions/{installationId}/permission-diff", Action: rbac.ActionExtensionRead, Handler: s.handleGetExtensionPermissionDiff},
		{Method: http.MethodPost, Path: "/admin/v1/extensions/{installationId}/approve-permissions", Action: rbac.ActionExtensionApprove, Handler: s.handleApproveExtensionPermissions},
		{Method: http.MethodPost, Path: "/admin/v1/extensions/{installationId}/upgrade", Action: rbac.ActionExtensionStage, Handler: s.handleUpgradeExtension},
		{Method: http.MethodPost, Path: "/admin/v1/extensions/{installationId}/rollback", Action: rbac.ActionExtensionApprove, Handler: s.handleRollbackExtension},
		{Method: http.MethodDelete, Path: "/admin/v1/extensions/{installationId}", Action: rbac.ActionExtensionApprove, Handler: s.handleUninstallOrDiscardExtension},
		{Method: http.MethodPut, Path: "/admin/v1/organizations/{orgId}/extension-policies", Action: rbac.ActionExtensionOrgPolicy, Handler: s.handlePutOrgExtensionPolicies},

		// Group H: Routing Rules, Dry-Run Explain & Hierarchical Policies (static paths before params)
		{Method: http.MethodPost, Path: "/admin/v1/routes/explain", Action: rbac.ActionRouteExplain, Handler: s.handleExplainRoute},
		{Method: http.MethodGet, Path: "/admin/v1/routes", Action: rbac.ActionRouteRead, Handler: s.handleListRoutes},
		{Method: http.MethodPost, Path: "/admin/v1/routes", Action: rbac.ActionRouteWrite, Handler: s.handleCreateRoute},
		{Method: http.MethodPut, Path: "/admin/v1/routes/{routeId}", Action: rbac.ActionRouteWrite, Handler: s.handleUpdateRoute},
		{Method: http.MethodDelete, Path: "/admin/v1/routes/{routeId}", Action: rbac.ActionRouteWrite, Handler: s.handleDeleteRoute},
		{Method: http.MethodGet, Path: "/admin/v1/policies/effective", Action: rbac.ActionPolicyRead, Handler: s.handleGetEffectivePolicy},
		{Method: http.MethodPut, Path: "/admin/v1/policies/{policyId}", Action: rbac.ActionPolicyProjectWrite, Handler: s.handleUpdatePolicy},

		// Group D: Control Plane Overview & Active Alerts
		{Method: http.MethodGet, Path: "/admin/v1/overview/summary", Action: rbac.ActionBudgetRead, Handler: s.handleGetOverviewSummary},
		{Method: http.MethodGet, Path: "/admin/v1/alerts/active", Action: rbac.ActionBudgetRead, Handler: s.handleListActiveAlerts},

		// Group J: Observability Traces, Debug Payloads, Budgets & FinOps Usage
		{Method: http.MethodGet, Path: "/admin/v1/traces", Action: rbac.ActionTraceReadMeta, Handler: s.handleListTraces},
		{Method: http.MethodGet, Path: "/admin/v1/traces/{requestId}", Action: rbac.ActionTraceReadMeta, Handler: s.handleGetTraceWaterfall},
		{Method: http.MethodGet, Path: "/admin/v1/traces/{requestId}/debug-payload", Action: rbac.ActionTraceReadDebugBlob, Handler: s.handleGetTraceDebugPayload},
		{Method: http.MethodGet, Path: "/admin/v1/budgets", Action: rbac.ActionBudgetRead, Handler: s.handleListBudgets},
		{Method: http.MethodPut, Path: "/admin/v1/budgets/{budgetId}", Action: rbac.ActionBudgetWrite, Handler: s.handleUpdateBudget},
		{Method: http.MethodGet, Path: "/admin/v1/usage/summary", Action: rbac.ActionBudgetRead, Handler: s.handleGetUsageSummary},
		{Method: http.MethodGet, Path: "/admin/v1/usage/export", Action: rbac.ActionBudgetRead, Handler: s.handleExportUsage},

		// Group K: Audit Log, Hash-Chain Verification, Kill Switch & Blast Radius
		{Method: http.MethodPost, Path: "/admin/v1/audit-events/verify", Action: rbac.ActionAuditReadVerify, Handler: s.handleVerifyAuditHashChain},
		{Method: http.MethodGet, Path: "/admin/v1/audit-events", Action: rbac.ActionAuditReadVerify, Handler: s.handleListAuditEvents},
		{Method: http.MethodPost, Path: "/admin/v1/kill-switches", Action: rbac.ActionKillSwitchTrigger, Handler: s.handleTriggerKillSwitch},
		{Method: http.MethodGet, Path: "/admin/v1/blast-radius", Action: rbac.ActionAuditReadVerify, Handler: s.handleQueryBlastRadius},
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
	for _, hdr := range []string{"X-Tanpopo-Virtual-Key", "x-api-key", "x-goog-api-key"} {
		for _, val := range r.Header.Values(hdr) {
			if strings.HasPrefix(strings.TrimSpace(val), "vk_tp_") {
				return true
			}
		}
	}
	for _, rawAuth := range r.Header.Values("Authorization") {
		authz := strings.TrimSpace(rawAuth)
		if strings.HasPrefix(authz, "vk_tp_") {
			return true
		}
		if len(authz) > 7 && strings.EqualFold(authz[:7], "Bearer ") {
			token := strings.TrimSpace(authz[7:])
			if strings.HasPrefix(token, "vk_tp_") {
				return true
			}
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
	if len(r.Header.Values("Authorization")) > 1 {
		writeErrorEnvelope(w, r, http.StatusUnauthorized, "unauthenticated", "authentication_error", "multiple Authorization headers are not permitted", false, nil)
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

func actorIsOrgOwnerOrOperator(actor *rbac.Actor, orgID string) bool {
	if actorHasOperator(actor) {
		return true
	}
	if actor == nil || orgID == "" {
		return false
	}
	for _, b := range actor.Bindings {
		if b.ScopeType == rbac.ScopeOrg && b.OrgID != nil && *b.OrgID == orgID && b.RoleName == rbac.RoleOrgOwner {
			return true
		}
	}
	return false
}

func (s *Server) resolveTargetOrgID(w http.ResponseWriter, r *http.Request, sc *authSessionContext) (string, bool) {
	orgID := sc.OrgID
	if sc.IsOperator {
		if qOrg := strings.TrimSpace(r.URL.Query().Get("org_id")); qOrg != "" {
			orgID = qOrg
		}
	}
	if orgID == "" {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "organization scope required", false, nil)
		return "", false
	}
	return orgID, true
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

func requireJSONContentType(w http.ResponseWriter, r *http.Request) bool {
	ct := strings.TrimSpace(r.Header.Get("Content-Type"))
	if ct == "" {
		writeErrorEnvelope(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "invalid_request_error", "Content-Type header must be application/json", false, nil)
		return false
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeErrorEnvelope(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "invalid_request_error", "Content-Type must be application/json", false, nil)
		return false
	}
	return true
}

// decodeJSONAndCheckOrgSpoofing decodes the request body into dst while enforcing
// AUTH-13 anti-spoofing: if top-level JSON contains "org_id" differing from the
// caller's authenticated organization, immediately fail with 403 and emit a security audit event.
func (s *Server) decodeJSONAndCheckOrgSpoofing(w http.ResponseWriter, r *http.Request, sc *authSessionContext, dst any) bool {
	if !requireJSONContentType(w, r) {
		return false
	}
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
			expectedOrg := sc.OrgID
			if sc.IsOperator {
				if qOrg := strings.TrimSpace(r.URL.Query().Get("org_id")); qOrg != "" {
					expectedOrg = qOrg
				}
			}
			if bodyOrg != "" && (!sc.IsOperator || expectedOrg != "") && bodyOrg != expectedOrg {
				s.emitSecurityAudit(r.Context(), sc, "security.org_id_spoofing_blocked", map[string]any{
					"spoofed_org_id": bodyOrg,
					"session_org_id": expectedOrg,
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
	if !requireJSONContentType(w, r) {
		return false
	}
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
	return egress.ValidateStaticEndpointURL(ctx, rawURL, allowPrivateIP, resolver, blockedPorts)
}

func (s *Server) requireActiveOrg(w http.ResponseWriter, r *http.Request, orgID string) bool {
	var status string
	err := s.db.QueryRowContext(r.Context(), `SELECT status FROM organizations WHERE id = ?`, orgID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
		return false
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying organization status", true, nil)
		return false
	}
	if status != port.OrgStatusActive {
		writeErrorEnvelope(w, r, http.StatusConflict, "resource_deleting", "conflict_error", "organization is not active", false, nil)
		return false
	}
	return true
}

func (s *Server) requireActiveOrgAndProject(w http.ResponseWriter, r *http.Request, orgID, projectID string) bool {
	if strings.TrimSpace(projectID) == "" {
		return s.requireActiveOrg(w, r, orgID)
	}
	var orgStatus, projStatus string
	err := s.db.QueryRowContext(r.Context(), `
		SELECT o.status, p.status
		FROM projects p
		JOIN organizations o ON o.id = p.org_id
		WHERE p.org_id = ? AND p.id = ?`,
		orgID, projectID,
	).Scan(&orgStatus, &projStatus)
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return false
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying project status", true, nil)
		return false
	}
	if orgStatus != port.OrgStatusActive || projStatus != port.ProjectStatusActive {
		writeErrorEnvelope(w, r, http.StatusConflict, "resource_deleting", "conflict_error", "organization or project is not active", false, nil)
		return false
	}
	return true
}
