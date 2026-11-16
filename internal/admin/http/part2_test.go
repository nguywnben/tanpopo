// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/declarative"
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
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

type part2Harness struct {
	*adminTestHarness
	catalog     *catalog.Repository
	installer   *installer.LifecycleService
	policies    *policyengine.Store
	routeRes    *resolver.Resolver
	ledgers     *ledger.Service
	broker      *broker.BlindHostCredentialBroker
	provRuntime extport.ProviderRuntime
	rootPriv    ed25519.PrivateKey
	rootPub     ed25519.PublicKey
	rootKeyID   string
	lockPath    string
	logBuf      *canary.SafeBuffer

	archivesMu sync.RWMutex
	archives   map[string]signedTestArchive
}

type signedTestArchive struct {
	raw []byte
	sig []byte
}

func newPart2TestHarness(t *testing.T, probeLimitPerMin int) *part2Harness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	nowFn := func() int64 { return fk.Now().UTC().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(dir, "admin_p2.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	auditLog, err := audit.NewLogger(audit.LoggerConfig{
		DB:    st.RawDB(),
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}

	masterKey := make([]byte, vault.KeySizeBytes)
	for i := range masterKey {
		masterKey[i] = byte(i + 31)
	}
	kek, err := vault.NewStaticKEKProvider("kek_part2_v1", masterKey)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	vlt, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           kek,
		NowMs:         nowFn,
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}

	rootPub, rootPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	keyID := installer.PublicKeyFingerprint(rootPub)
	keyring := installer.TrustedKeyring{
		RootPublicKeys: map[string]ed25519.PublicKey{keyID: rootPub},
	}

	snapshots, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{
		DB:      st.RawDB(),
		NowMs:   nowFn,
		Keyring: keyring,
	})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}

	ksReg, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{
		DB:    st.RawDB(),
		Audit: auditLog,
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatalf("killswitch.NewRegistry: %v", err)
	}

	argonParams := auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32}
	authSvc, err := auth.NewLocalService(auth.ServiceConfig{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Argon2:    argonParams,
		AuditSink: auditLog,
	})
	if err != nil {
		t.Fatalf("auth.NewLocalService: %v", err)
	}

	tokenPath := filepath.Join(dir, "bootstrap.token")
	bootMgr, err := bootstrap.NewManager(bootstrap.ManagerConfig{
		DB:        st.RawDB(),
		TokenPath: tokenPath,
		NowMs:     nowFn,
		Argon2:    argonParams,
		AuditSink: auditLog,
	})
	if err != nil {
		t.Fatalf("bootstrap.NewManager: %v", err)
	}
	if _, err := bootMgr.EnsureToken(ctx); err != nil {
		t.Fatalf("bootMgr.EnsureToken: %v", err)
	}

	vkeySvc, err := vkey.NewService(vkey.ServiceConfig{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Snapshots: snapshots,
		AuditSink: auditLog,
	})
	if err != nil {
		t.Fatalf("vkey.NewService: %v", err)
	}

	catRepo, err := catalog.NewRepository(catalog.Config{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Snapshots: snapshots,
		Audit:     auditLog,
		Keyring:   keyring,
	})
	if err != nil {
		t.Fatalf("catalog.NewRepository: %v", err)
	}

	// DEC-020: Wire BOTH Config.Resolver and Config.PolicyResolver explicitly.
	brk, err := broker.NewBlindBroker(broker.Config{
		Resolver:       catRepo,
		PolicyResolver: catRepo,
		Vault:          vlt,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          auditLog,
		NowMs:          nowFn,
		DefaultTimeout: 5 * time.Second,
		MaxTimeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("broker.NewBlindBroker: %v", err)
	}

	provRuntime, err := declarative.CompileSpec(declarative.Spec{
		ID:                "openai_probe_spec",
		Slug:              "openai",
		SupportedDialects: []string{"openai_compatible"},
		AllowedHosts:      []string{"api.openai.com", "127.0.0.1"},
		DefaultBaseURL:    "https://api.openai.com",
		AuthBindings: []declarative.AuthBindingSpec{
			{
				SlotName:       "api_key",
				TargetLocation: extport.CredentialTargetLocationHeader,
				FieldName:      "Authorization",
				ValuePrefix:    "Bearer ",
			},
		},
		ValidationProbe: declarative.ValidationProbeSpec{
			Method:          http.MethodGet,
			PathTemplate:    "/v1/models",
			SuccessStatuses: []int32{http.StatusOK},
			ModelsArrayPath: "data",
			ModelIDField:    "id",
		},
		RequestMapping: declarative.RequestMappingSpec{
			Method:       http.MethodPost,
			PathTemplate: "/v1/chat/completions",
		},
		ResponseMapping: declarative.ResponseMappingSpec{
			ModelPath:        "model",
			FinishReasonPath: "choices.0.finish_reason",
			ContentPath:      "choices.0.message.content",
			RolePath:         "choices.0.message.role",
		},
	})
	if err != nil {
		t.Fatalf("declarative.CompileSpec: %v", err)
	}

	pluginsDir := filepath.Join(dir, "plugins")
	lockPath := filepath.Join(dir, "tanpopo.lock")
	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:           st.RawDB(),
		Audit:        auditLog,
		PluginsDir:   pluginsDir,
		LockfilePath: lockPath,
		Keyring:      keyring,
		NowMs:        nowFn,
	})
	if err != nil {
		t.Fatalf("installer.NewLifecycleService: %v", err)
	}

	polStore, err := policyengine.NewStore(policyengine.StoreConfig{
		DB:    st.RawDB(),
		Audit: auditLog,
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatalf("policyengine.NewStore: %v", err)
	}

	ledgerSvc, err := ledger.NewService(ledger.Config{
		DB:    st.RawDB(),
		Audit: auditLog,
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatalf("ledger.NewService: %v", err)
	}

	routeRes := resolver.New()

	base := &adminTestHarness{
		st:           st,
		fk:           fk,
		auditLog:     auditLog,
		kek:          kek,
		vault:        vlt,
		snapshots:    snapshots,
		killSwitch:   ksReg,
		authSvc:      authSvc,
		bootMgr:      bootMgr,
		vkeys:        vkeySvc,
		tokenPath:    tokenPath,
		scanner:      canary.NewScanner(),
		bearerTokens: make(map[string]*rbac.Actor),
	}

	h := &part2Harness{
		adminTestHarness: base,
		catalog:          catRepo,
		installer:        instSvc,
		policies:         polStore,
		routeRes:         routeRes,
		ledgers:          ledgerSvc,
		broker:           brk,
		provRuntime:      provRuntime,
		rootPriv:         rootPriv,
		rootPub:          rootPub,
		rootKeyID:        keyID,
		lockPath:         lockPath,
		logBuf:           canary.NewSafeBuffer(),
		archives:         make(map[string]signedTestArchive),
	}

	if probeLimitPerMin <= 0 {
		probeLimitPerMin = 20
	}

	pagSecret := []byte("0123456789abcdef0123456789abcdef")
	srv, err := NewServer(ServerConfig{
		Store:                   st,
		DB:                      st.RawDB(),
		Bootstrap:               bootMgr,
		Auth:                    authSvc,
		VKeys:                   vkeySvc,
		Vault:                   vlt,
		Ledgers:                 ledgerSvc,
		Snapshots:               snapshots,
		KillSwitch:              ksReg,
		Catalog:                 catRepo,
		Installer:               instSvc,
		Policies:                polStore,
		RouteResolver:           routeRes,
		Broker:                  brk,
		ProviderRuntime:         provRuntime,
		LockfilePath:            lockPath,
		ProbeRateLimitPerMinute: probeLimitPerMin,
		MarketplaceCatalog: []MarketplaceCatalogEntry{
			{
				PackageID:        "tanpopo/openai-compat",
				CanonicalName:    "OpenAI Official Adapter",
				Version:          "1.0.0",
				ExecutionTier:    "declarative",
				OriginTrustTier:  "official",
				EnterpriseSigned: false,
				SourceURI:        "https://marketplace.tanpopo.dev/openai-compat-1.0.0.tpkg",
				ExpectedSHA256:   strings.Repeat("b", 64),
				Summary:          "Official OpenAI compatible adapter",
				Capabilities:     []string{"provider_adapter"},
			},
		},
		ArchiveResolver: func(_ context.Context, sourceURI, expectedSHA256 string) (*installer.StageInstallRequest, func(), error) {
			h.archivesMu.RLock()
			entry, ok := h.archives[sourceURI]
			h.archivesMu.RUnlock()
			if !ok {
				return nil, nil, fmt.Errorf("%w: archive %q not registered", port.ErrNotFound, sourceURI)
			}
			req := &installer.StageInstallRequest{
				ArchiveReader: bytes.NewReader(entry.raw),
				Source: installer.SourceSpec{
					Kind:           installer.SourceKindURL,
					URI:            sourceURI,
					ExpectedSHA256: expectedSHA256,
				},
				Signature: installer.ProvenanceSignature{
					PublisherOrRootSig: entry.sig,
				},
			}
			return req, func() {}, nil
		},
		Audit:            auditLog,
		NowMs:            nowFn,
		PaginationSecret: pagSecret,
		TokenVerifier: func(_ context.Context, raw string) (*rbac.Actor, *SessionMetadata, error) {
			base.bearerMu.RLock()
			actor, ok := base.bearerTokens[raw]
			base.bearerMu.RUnlock()
			if !ok {
				return nil, nil, rbac.ErrUnauthenticated
			}
			return actor, &SessionMetadata{
				Email:       "service-token@example.test",
				ExpiresAtMs: nowFn() + 3600_000,
			}, nil
		},
		HostResolver: &countingResolver{
			fn: func(host string) ([]netip.Addr, error) {
				if strings.EqualFold(host, "private.internal.test") {
					return []netip.Addr{netip.MustParseAddr("10.0.0.42")}, nil
				}
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			},
		},
		BlockedAdminPorts: []int{9090},
		Version:           "0.1.0-part2",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	base.srv = srv
	return h
}

func (h *part2Harness) registerSignedProviderTPKG(t *testing.T, sourceURI, pkgID, ver string, allowedHosts []string, allowPrivIP, requireTLS bool) string {
	t.Helper()
	adapterYAML := []byte("id: " + pkgID + "\nslug: acme\n")
	adapterSum := sha256.Sum256(adapterYAML)
	adapterSHA := hex.EncodeToString(adapterSum[:])

	var hostsYAML strings.Builder
	for _, host := range allowedHosts {
		fmt.Fprintf(&hostsYAML, "      - %q\n", host)
	}
	manifest := []byte(fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: %q
  name: "Acme Adapter"
  version: %q
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"
runtime:
  tier: "declarative"
  entrypoint: "adapter.yaml"
  sha256: %q
capabilities:
  - type: "provider_adapter"
    adapterSlug: "acme"
    supportedDialects: ["openai_compatible"]
    authMethods:
      - id: "api_key_header"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
permissions:
  networkEgress:
    allowedHosts:
%s    allowPrivateIp: %t
    requireTls: %t
  hooks:
    - "provider:transform_request"
`, pkgID, ver, adapterSHA, hostsYAML.String(), allowPrivIP, requireTLS))

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, ent := range []struct {
		name string
		body []byte
	}{
		{"manifest.yaml", manifest},
		{"adapter.yaml", adapterYAML},
	} {
		hdr := &tar.Header{
			Name:     ent.name,
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(ent.body)),
			ModTime:  time.Unix(0, 0).UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader %s: %v", ent.name, err)
		}
		if _, err := tw.Write(ent.body); err != nil {
			t.Fatalf("Write %s: %v", ent.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}

	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	shaHex := hex.EncodeToString(sum[:])
	sig := ed25519.Sign(h.rootPriv, installer.SignatureSigningPayload(pkgID, ver, shaHex))

	h.archivesMu.Lock()
	h.archives[sourceURI] = signedTestArchive{raw: raw, sig: sig}
	h.archivesMu.Unlock()
	return shaHex
}

func (h *part2Harness) bootstrapOwner(t *testing.T, orgSlug, projSlug string) (bootstrapCompleteResponse, *http.Cookie) {
	t.Helper()
	tok := h.readBootstrapToken(t)
	rec := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                tok,
		"owner_email":                    "owner@example.test",
		"owner_password":                 "StrongPassPart2#2026!",
		"org_name":                       orgSlug,
		"project_name":                   projSlug,
		"master_kek_backup_acknowledged": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap/complete failed status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp bootstrapCompleteResponse
	mustDecodeJSON(t, rec.Body.Bytes(), &resp)
	cookie := findCookie(t, rec.Result().Cookies(), "__Host-tanpopo_admin_session")
	return resp, cookie
}

func seedLocalOpenAIAdapter(t *testing.T, h *part2Harness, host string) string {
	t.Helper()
	ctx := context.Background()
	now := h.fk.Now().UTC().UnixMilli()
	pkgID := "tanpopo.local_openai"
	exvID := idgen.MustNew(idgen.PrefixExtensionVersion)
	insID := idgen.MustNew(idgen.PrefixInstallation)
	adpID := idgen.MustNew(idgen.PrefixAdapter)

	_, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms)
		VALUES (?, ?, 'PROVIDER_ADAPTER', 'tanpopo', 'official', ?)`,
		pkgID, "tanpopo.local_openai", now,
	)
	if err != nil {
		t.Fatalf("insert extension_packages: %v", err)
	}
	_, err = h.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_versions (
			id, package_id, semver, contract_version, core_version_range, execution_tier,
			bundle_sha256, signature_verified, enterprise_signed, source_uri, manifest_json, created_at_ms
		) VALUES (?, ?, '1.0.0', 'v1', '>=0.1.0', 'DECLARATIVE_T1', ?, 1, 0, 'builtin://local', '{}', ?)`,
		exvID, pkgID, strings.Repeat("c", 64), now,
	)
	if err != nil {
		t.Fatalf("insert extension_versions: %v", err)
	}
	permsJSON, _ := json.Marshal(installer.PermissionSet{
		AllowedHosts:   []string{host, "api.openai.com"},
		AllowPrivateIP: true,
		RequireTLS:     false,
		SecretSlots:    []string{"api_key"},
	})
	_, err = h.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_installations (
			id, package_id, active_version_id, state, approved_permissions_json,
			approved_by_principal_id, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, 'ENABLED_ACTIVE', ?, NULL, 1, ?, ?)`,
		insID, pkgID, exvID, string(permsJSON), now, now,
	)
	if err != nil {
		t.Fatalf("insert extension_installations: %v", err)
	}
	hostsJSON, _ := json.Marshal([]string{host, "api.openai.com"})
	_, err = h.st.RawDB().ExecContext(ctx, `
		INSERT INTO provider_adapters (
			id, installation_id, adapter_slug, supported_dialects_json, allowed_hosts_json, version, created_at_ms
		) VALUES (?, ?, 'openai', '["openai_compatible"]', ?, 1, ?)`,
		adpID, insID, string(hostsJSON), now,
	)
	if err != nil {
		t.Fatalf("insert provider_adapters: %v", err)
	}
	return adpID
}

func TestPart2E2ELifecycleAndAuditChain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "E2E Org", "e2e-proj")

	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE organizations SET require_four_eyes_approval = 1 WHERE id = ?`, boot.OrgID); err != nil {
		t.Fatalf("enable four eyes: %v", err)
	}

	reviewerToken := "tok_reviewer_e2e"
	reviewerID := idgen.MustNew(idgen.PrefixPrincipal)
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'reviewer@example.test', 'ACTIVE', 1000, 1000)`,
		reviewerID, boot.OrgID,
	); err != nil {
		t.Fatalf("insert reviewer principal: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_e2e_rev', ?, ?, 'ORG', ?, 'SECURITY_REVIEWER', 1, 1000)`,
		boot.OrgID, reviewerID, boot.OrgID,
	); err != nil {
		t.Fatalf("insert reviewer role_binding: %v", err)
	}
	h.registerBearerToken(reviewerToken, &rbac.Actor{
		PrincipalID:     reviewerID,
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleSecurityReviewer}},
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Header.Get("Authorization") != "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini"}]}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)

	adpID := seedLocalOpenAIAdapter(t, h, upURL.Hostname())
	recAdapters := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/provider-adapters", ownerCookie, "", "", nil)
	if recAdapters.Code != http.StatusOK {
		t.Fatalf("GET /provider-adapters code=%d body=%s", recAdapters.Code, recAdapters.Body.String())
	}

	secVal := canary.DefaultCanarySecret()
	recSec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "primary-openai-key",
		"secret_kind":  "API_KEY",
		"secret_value": secVal,
	})
	if recSec.Code != http.StatusCreated {
		t.Fatalf("POST /secrets code=%d body=%s", recSec.Code, recSec.Body.String())
	}
	var secMeta secretMetadataDTO
	mustDecodeJSON(t, recSec.Body.Bytes(), &secMeta)
	h.scanner.AssertClean(t, canary.NamedData{Source: "POST /secrets", Data: recSec.Body.Bytes()})

	recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "local-openai",
		"adapter_id":       adpID,
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     upstream.URL,
		"data_region":      "US",
		"allow_private_ip": true,
		"secret_bindings":  map[string]string{"api_key": secMeta.CredentialID},
	})
	if recConn.Code != http.StatusCreated {
		t.Fatalf("POST /connections code=%d body=%s", recConn.Code, recConn.Body.String())
	}
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)
	h.scanner.AssertClean(t, canary.NamedData{Source: "POST /connections", Data: recConn.Body.Bytes()})

	recTestConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recTestConn.Code != http.StatusOK {
		t.Fatalf("POST /connections/{id}/test code=%d body=%s", recTestConn.Code, recTestConn.Body.String())
	}
	var testRes connectionTestResponseDTO
	mustDecodeJSON(t, recTestConn.Body.Bytes(), &testRes)
	if !testRes.Valid || len(testRes.DetectedModelIDs) != 2 {
		t.Fatalf("expected valid connection test with 2 detected models, got %+v", testRes)
	}
	h.scanner.AssertClean(t, canary.NamedData{Source: "POST /connections/test", Data: recTestConn.Body.Bytes()})

	v1URI := "https://marketplace.tanpopo.dev/acme-1.0.0.tpkg"
	v1SHA := h.registerSignedProviderTPKG(t, v1URI, "@tanpopo/acme-adapter", "1.0.0", []string{"api.openai.com"}, false, true)
	recInstall := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      v1URI,
		"expected_sha256": v1SHA,
	})
	if recInstall.Code != http.StatusAccepted {
		t.Fatalf("POST /extensions/install code=%d body=%s", recInstall.Code, recInstall.Body.String())
	}
	var stageDiff extensionPermissionDiffDTO
	mustDecodeJSON(t, recInstall.Body.Bytes(), &stageDiff)
	installETag := recInstall.Result().Header.Get("ETag")

	recDiff := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/extensions/"+stageDiff.InstallationID+"/permission-diff", ownerCookie, "", "", nil)
	if recDiff.Code != http.StatusOK {
		t.Fatalf("GET /permission-diff code=%d body=%s", recDiff.Code, recDiff.Body.String())
	}
	var diffDTO extensionPermissionDiffDTO
	mustDecodeJSON(t, recDiff.Body.Bytes(), &diffDTO)
	if len(diffDTO.AddedAllowedHosts) != 1 || diffDTO.AddedAllowedHosts[0] != "api.openai.com" {
		t.Fatalf("unexpected permission diff hosts: %+v", diffDTO.AddedAllowedHosts)
	}

	recSelfApp := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+stageDiff.InstallationID+"/approve-permissions", ownerCookie, boot.CSRFToken, installETag, map[string]any{
		"enable_immediately": true,
	})
	if recSelfApp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 self_approval_forbidden, got %d body=%s", recSelfApp.Code, recSelfApp.Body.String())
	}

	recApprove := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+stageDiff.InstallationID+"/approve-permissions", reviewerToken, installETag, map[string]any{
		"enable_immediately": true,
	})
	if recApprove.Code != http.StatusOK {
		t.Fatalf("POST /approve-permissions code=%d body=%s", recApprove.Code, recApprove.Body.String())
	}
	var extIns extensionInstallationDTO
	mustDecodeJSON(t, recApprove.Body.Bytes(), &extIns)

	v2URI := "https://marketplace.tanpopo.dev/acme-1.1.0.tpkg"
	v2SHA := h.registerSignedProviderTPKG(t, v2URI, "@tanpopo/acme-adapter", "1.1.0", []string{"api.openai.com", "eu.api.openai.com"}, false, true)
	recUp := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+extIns.ID+"/upgrade", ownerCookie, boot.CSRFToken, FormatETag(extIns.Version), map[string]any{
		"source_uri":      v2URI,
		"expected_sha256": v2SHA,
	})
	if recUp.Code != http.StatusAccepted {
		t.Fatalf("POST /upgrade code=%d body=%s", recUp.Code, recUp.Body.String())
	}
	var upDiff extensionPermissionDiffDTO
	mustDecodeJSON(t, recUp.Body.Bytes(), &upDiff)
	upETag := recUp.Result().Header.Get("ETag")

	recApproveUp := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+upDiff.InstallationID+"/approve-permissions", reviewerToken, upETag, map[string]any{
		"enable_immediately": true,
	})
	if recApproveUp.Code != http.StatusOK {
		t.Fatalf("POST /approve-permissions (upgrade) code=%d body=%s", recApproveUp.Code, recApproveUp.Body.String())
	}
	mustDecodeJSON(t, recApproveUp.Body.Bytes(), &extIns)

	rbStart := time.Now()
	recRB := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+extIns.ID+"/rollback", ownerCookie, boot.CSRFToken, FormatETag(extIns.Version), map[string]any{
		"reason": "Revert escalated endpoint for verification",
	})
	if recRB.Code != http.StatusOK {
		t.Fatalf("POST /rollback code=%d body=%s", recRB.Code, recRB.Body.String())
	}
	if elapsed := time.Since(rbStart); elapsed > 5*time.Second {
		t.Fatalf("rollback exceeded 5s SLO: %v", elapsed)
	}
	mustDecodeJSON(t, recRB.Body.Bytes(), &extIns)
	if extIns.ActiveVersion == nil || *extIns.ActiveVersion != "1.0.0" {
		t.Fatalf("expected rollback active_version 1.0.0, got %+v", extIns.ActiveVersion)
	}

	recOrgExtPol := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/organizations/"+boot.OrgID+"/extension-policies", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"min_plugin_trust": "OFFICIAL_ONLY",
		"policies": []map[string]any{
			{
				"package_id":             extIns.PackageID,
				"enabled":                true,
				"pinned_installation_id": extIns.ID,
			},
		},
	})
	if recOrgExtPol.Code != http.StatusOK {
		t.Fatalf("PUT /organizations/{orgId}/extension-policies code=%d body=%s", recOrgExtPol.Code, recOrgExtPol.Body.String())
	}
	recOrgExtPolOCC := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/organizations/"+boot.OrgID+"/extension-policies", ownerCookie, boot.CSRFToken, `"v99"`, map[string]any{
		"min_plugin_trust": "OFFICIAL_ONLY",
		"policies":         []map[string]any{},
	})
	if recOrgExtPolOCC.Code != http.StatusConflict {
		t.Fatalf("expected 409 stale ETag on PUT /extension-policies, got %d body=%s", recOrgExtPolOCC.Code, recOrgExtPolOCC.Body.String())
	}

	recModel := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/models", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":              "gpt-4o",
		"display_name":      "GPT-4o",
		"context_window":    128000,
		"max_output_tokens": 16384,
		"assurance_tier":    "VERIFIED",
		"capabilities": map[string]any{
			"tool_calling": true,
			"streaming":    true,
		},
	})
	if recModel.Code != http.StatusCreated {
		t.Fatalf("POST /models code=%d body=%s", recModel.Code, recModel.Body.String())
	}
	var mod logicalModelDTO
	mustDecodeJSON(t, recModel.Body.Bytes(), &mod)

	recDep := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/deployments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":              boot.EnvID,
		"logical_model_id":    mod.ID,
		"connection_id":       conn.ID,
		"upstream_model_name": "gpt-4o",
		"data_region":         "US",
		"rate_card": map[string]any{
			"input_token_nano_usd":  2500,
			"output_token_nano_usd": 10000,
		},
	})
	if recDep.Code != http.StatusCreated {
		t.Fatalf("POST /deployments code=%d body=%s", recDep.Code, recDep.Body.String())
	}
	var dep modelDeploymentDTO
	mustDecodeJSON(t, recDep.Body.Bytes(), &dep)

	recDepPatch := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/deployments/"+dep.ID, ownerCookie, boot.CSRFToken, FormatETag(dep.Version), map[string]any{
		"state": "ACTIVE_HEALTHY",
	})
	if recDepPatch.Code != http.StatusOK {
		t.Fatalf("PATCH /deployments code=%d body=%s", recDepPatch.Code, recDepPatch.Body.String())
	}

	recRoute := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":                 "chat-default",
		"env_id":               boot.EnvID,
		"strategy":             "PRIORITY_FAILOVER",
		"allowed_data_regions": []string{"US"},
		"max_failover_hops":    2,
		"request_timeout_ms":   30000,
		"targets": []map[string]any{
			{"deployment_id": dep.ID, "priority_order": 1, "weight": 100},
		},
	})
	if recRoute.Code != http.StatusCreated {
		t.Fatalf("POST /routes code=%d body=%s", recRoute.Code, recRoute.Body.String())
	}

	recExplain := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes/explain", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":       boot.EnvID,
		"target_model": "chat-default",
	})
	if recExplain.Code != http.StatusOK {
		t.Fatalf("POST /routes/explain code=%d body=%s", recExplain.Code, recExplain.Body.String())
	}

	polID := idgen.MustNew(idgen.PrefixPolicy)
	recPol := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/policies/"+polID, ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"scope_tier":               "ORGANIZATION",
		"scope_id":                 boot.OrgID,
		"allowed_data_regions":     []string{"US"},
		"min_plugin_trust":         "OFFICIAL_ONLY",
		"min_capability_assurance": "VERIFIED",
		"rpm_ceiling":              1000,
	})
	if recPol.Code != http.StatusOK {
		t.Fatalf("PUT /policies code=%d body=%s", recPol.Code, recPol.Body.String())
	}

	bdgID := idgen.MustNew(idgen.PrefixBudget)
	recBdg := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/budgets/"+bdgID, ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"scope_tier":            "PROJECT",
		"scope_id":              boot.ProjectID,
		"period":                "MONTHLY",
		"hard_limit_micros":     50000000,
		"soft_warning_percents": []int{50, 80, 100},
	})
	if recBdg.Code != http.StatusOK {
		t.Fatalf("PUT /budgets code=%d body=%s", recBdg.Code, recBdg.Body.String())
	}

	nowMs := h.fk.Now().UTC().UnixMilli()
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO usage_settlements (
			id, settled_at_ms, request_id, org_id, project_id, env_id, virtual_key_id,
			plane, logical_model_slug, billing_mode, usage_accuracy, total_cost_micros
		) VALUES ('stl_e2e_1', ?, 'req_e2e_1', ?, ?, ?, 'vk_e2e', 'PROVIDER', 'chat-default', 'COMMERCIAL_API', 'EXACT', 15000)`,
		nowMs, boot.OrgID, boot.ProjectID, boot.EnvID,
	); err != nil {
		t.Fatalf("insert usage_settlements: %v", err)
	}

	recExport := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/usage/export?format=csv", ownerCookie, "", "", nil)
	if recExport.Code != http.StatusOK || !strings.Contains(recExport.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("GET /usage/export code=%d ct=%q body=%s", recExport.Code, recExport.Header().Get("Content-Type"), recExport.Body.String())
	}

	recOv := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/overview/summary?window=24h", ownerCookie, "", "", nil)
	if recOv.Code != http.StatusOK {
		t.Fatalf("GET /overview/summary code=%d body=%s", recOv.Code, recOv.Body.String())
	}

	recKS := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/kill-switches", ownerCookie, boot.CSRFToken, "", map[string]any{
		"target_level":         "PROVIDER_CONNECTION",
		"target_id":            conn.ID,
		"abort_active_streams": true,
		"reason":               "E2E emergency drill containment",
	})
	if recKS.Code != http.StatusOK {
		t.Fatalf("POST /kill-switches code=%d body=%s", recKS.Code, recKS.Body.String())
	}

	recVerify := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/audit-events/verify", ownerCookie, boot.CSRFToken, "", nil)
	if recVerify.Code != http.StatusOK {
		t.Fatalf("POST /audit-events/verify code=%d body=%s", recVerify.Code, recVerify.Body.String())
	}
	var verDTO verifyAuditResponseDTO
	mustDecodeJSON(t, recVerify.Body.Bytes(), &verDTO)
	if !verDTO.Intact || verDTO.CheckedEventsCount < 5 || verDTO.FirstBrokenSeqNo != nil {
		t.Fatalf("expected intact hash chain with multiple write audit events, got %+v", verDTO)
	}
}

func TestPart2WriteOnlySecretsAndCanaryZeroLeak(t *testing.T) {
	t.Parallel()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "Canary Org", "canary-proj")

	canaryA := canary.DefaultCanarySecret()
	canaryB := "sk-tp-" + "canary-secondary-99887766"
	h.scanner.AddSecret(canaryA)
	h.scanner.AddSecret(canaryB)

	echoUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHdr := r.Header.Get("Authorization")
		_, _ = fmt.Fprintf(h.logBuf, "upstream saw request path=%s header_len=%d\n", r.URL.Path, len(authHdr))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"invalid key %s"}}`, authHdr)
	}))
	defer echoUpstream.Close()
	upURL, _ := url.Parse(echoUpstream.URL)
	adpID := seedLocalOpenAIAdapter(t, h, upURL.Hostname())

	recSec1 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "canary-secret-1",
		"secret_kind":  "API_KEY",
		"secret_value": canaryA,
	})
	if recSec1.Code != http.StatusCreated {
		t.Fatalf("POST /secrets 1 code=%d body=%s", recSec1.Code, recSec1.Body.String())
	}
	var sec1 secretMetadataDTO
	mustDecodeJSON(t, recSec1.Body.Bytes(), &sec1)
	if !strings.HasPrefix(sec1.TrailingFingerprint, "...") || len(sec1.TrailingFingerprint) != 7 || strings.HasSuffix(sec1.TrailingFingerprint, "7d6c") {
		t.Fatalf("expected irreversible 4-hex trailing fingerprint '...xxxx' (distinct from raw secret suffix), got %q", sec1.TrailingFingerprint)
	}

	recSec2 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "canary-secret-2",
		"secret_kind":  "HMAC_SECRET",
		"secret_value": canaryB,
	})
	if recSec2.Code != http.StatusCreated {
		t.Fatalf("POST /secrets 2 code=%d body=%s", recSec2.Code, recSec2.Body.String())
	}
	var sec2 secretMetadataDTO
	mustDecodeJSON(t, recSec2.Body.Bytes(), &sec2)

	recListSec := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/secrets", ownerCookie, "", "", nil)
	if recListSec.Code != http.StatusOK {
		t.Fatalf("GET /secrets code=%d body=%s", recListSec.Code, recListSec.Body.String())
	}

	recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "echo-check-conn",
		"adapter_id":       adpID,
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     echoUpstream.URL,
		"data_region":      "US",
		"allow_private_ip": true,
		"secret_bindings":  map[string]string{"api_key": sec1.CredentialID},
	})
	if recConn.Code != http.StatusCreated {
		t.Fatalf("POST /connections code=%d body=%s", recConn.Code, recConn.Body.String())
	}
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)

	recGetConn := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/connections", ownerCookie, "", "", nil)
	if recGetConn.Code != http.StatusOK {
		t.Fatalf("GET /connections code=%d body=%s", recGetConn.Code, recGetConn.Body.String())
	}

	recPatchConn := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+conn.ID, ownerCookie, boot.CSRFToken, FormatETag(conn.Version), map[string]any{
		"secret_bindings": map[string]string{"api_key": sec2.CredentialID},
	})
	if recPatchConn.Code != http.StatusOK {
		t.Fatalf("PATCH /connections code=%d body=%s", recPatchConn.Code, recPatchConn.Body.String())
	}

	recTestEcho := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recTestEcho.Code != http.StatusOK {
		t.Fatalf("POST /connections/{id}/test code=%d body=%s", recTestEcho.Code, recTestEcho.Body.String())
	}

	// NEG-1: Zero leaked secret bytes across EVERY secrets & connections payload and log buffer!
	h.scanner.AssertClean(t,
		canary.NamedData{Source: "POST /secrets", Data: recSec1.Body.Bytes()},
		canary.NamedData{Source: "POST /secrets 2", Data: recSec2.Body.Bytes()},
		canary.NamedData{Source: "GET /secrets", Data: recListSec.Body.Bytes()},
		canary.NamedData{Source: "POST /connections", Data: recConn.Body.Bytes()},
		canary.NamedData{Source: "GET /connections", Data: recGetConn.Body.Bytes()},
		canary.NamedData{Source: "PATCH /connections", Data: recPatchConn.Body.Bytes()},
		canary.NamedData{Source: "POST /connections/test (echoing upstream)", Data: recTestEcho.Body.Bytes()},
	)
	h.scanner.AssertCleanBuffer(t, "test-logs", h.logBuf)
}

func TestPart2CSVFormulaInjectionSanitization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "CSV Org", "csv-safe")

	dangerous := []struct {
		slug string
		name string
	}{
		{"evil-eq", `=HYPERLINK("http://evil.test","click")`},
		{"evil-plus", `+SUM(A1:A5)`},
		{"evil-minus", `-1+1`},
		{"evil-at", `@IMPORT("http://evil.test")`},
		{"evil-tab", "\t=cmd|' /C calc'!A0"},
		{"evil-ws", `  =1+1`},
	}

	now := h.fk.Now().UTC().UnixMilli()
	for idx, tc := range dangerous {
		recP := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
			"slug": tc.slug,
			"name": tc.name,
		})
		if recP.Code != http.StatusCreated {
			t.Fatalf("create project %s code=%d body=%s", tc.slug, recP.Code, recP.Body.String())
		}
		var proj projectDTO
		mustDecodeJSON(t, recP.Body.Bytes(), &proj)
		_, err := h.st.RawDB().ExecContext(ctx, `
			INSERT INTO usage_settlements (
				id, settled_at_ms, request_id, org_id, project_id, env_id, virtual_key_id,
				plane, logical_model_slug, billing_mode, usage_accuracy, total_cost_micros
			) VALUES
			(?, ?, ?, ?, ?, ?, 'vk_csv', 'PROVIDER', 'gpt-4o', 'COMMERCIAL_API', 'EXACT', 1200),
			(?, ?, ?, ?, ?, ?, 'vk_csv', 'PROVIDER', 'gpt-4o', 'COMMERCIAL_API', 'ESTIMATED', 800),
			(?, ?, ?, ?, ?, ?, 'vk_csv', 'PROVIDER', 'gpt-4o', 'COMMERCIAL_API', 'UNMETERED_FLAGGED', 0)`,
			fmt.Sprintf("stl_e_%d", idx), now, fmt.Sprintf("req_e_%d", idx), boot.OrgID, proj.ID, boot.EnvID,
			fmt.Sprintf("stl_s_%d", idx), now, fmt.Sprintf("req_s_%d", idx), boot.OrgID, proj.ID, boot.EnvID,
			fmt.Sprintf("stl_u_%d", idx), now, fmt.Sprintf("req_u_%d", idx), boot.OrgID, proj.ID, boot.EnvID,
		)
		if err != nil {
			t.Fatalf("insert settlements for %s: %v", tc.slug, err)
		}
	}

	recCSV := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/usage/export?format=csv&group_by=project", ownerCookie, "", "", nil)
	if recCSV.Code != http.StatusOK {
		t.Fatalf("GET /usage/export csv code=%d body=%s", recCSV.Code, recCSV.Body.String())
	}

	records, err := csv.NewReader(bytes.NewReader(recCSV.Body.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("parse exported CSV: %v", err)
	}
	if len(records) != 1+len(dangerous) {
		t.Fatalf("expected %d CSV rows, got %d", 1+len(dangerous), len(records))
	}
	header := records[0]
	wantHeader := []string{"group_key", "exact_micro_usd", "estimated_micro_usd", "unmetered_request_count"}
	for i := range wantHeader {
		if header[i] != wantHeader[i] {
			t.Fatalf("CSV header[%d]=%q want %q", i, header[i], wantHeader[i])
		}
	}
	for _, row := range records[1:] {
		cell := row[0]
		if !strings.HasPrefix(cell, "'") {
			t.Fatalf("formula injection cell %q was not prefixed with leading apostrophe", cell)
		}
		if row[1] != "1200" || row[2] != "800" || row[3] != "1" {
			t.Fatalf("expected strictly separated exact=1200 est=800 unmetered=1, got %v", row)
		}
	}
}

func TestPart2RBACRoleSeparationAndCrossTenant404(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "RBAC Org", "rbac-proj")
	now := h.fk.Now().UTC().UnixMilli()

	recP2 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "other-proj",
		"name": "Other Proj",
	})
	if recP2.Code != http.StatusCreated {
		t.Fatalf("POST /projects 2 code=%d body=%s", recP2.Code, recP2.Body.String())
	}

	org2, err := h.st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "tenant-b", DisplayName: "Tenant B"})
	if err != nil {
		t.Fatalf("CreateOrganization tenant-b: %v", err)
	}

	finopsTok := "tok_finops_viewer"
	h.registerBearerToken(finopsTok, &rbac.Actor{
		PrincipalID:     "pri_finops_1",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleFinOpsViewer}},
	})
	teamAdminTok := "tok_team_admin"
	h.registerBearerToken(teamAdminTok, &rbac.Actor{
		PrincipalID:     "pri_team_1",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleTeamAdmin}},
	})

	reqID := "trc_rbac_debug_1"
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO request_traces_meta (
			request_id, created_at_ms, started_at_ms, org_id, project_id, env_id, entry_point, route_slug,
			http_status, proxy_overhead_us, ttft_ms, total_duration_ms, waterfall_stages_json
		) VALUES (?, ?, ?, ?, ?, ?, 'unified_http', 'gpt-4o', 200, 120, 12, 45, '[]')`,
		reqID, now, now-45, boot.OrgID, boot.ProjectID, boot.EnvID,
	); err != nil {
		t.Fatalf("insert trace meta: %v", err)
	}
	nonce, ct, err := h.vault.SealProjectBlob(ctx, boot.OrgID, boot.ProjectID, vault.RecordKindDebugBlob, reqID, []byte(`{"prompt":"hi"}`))
	if err != nil {
		t.Fatalf("SealProjectBlob: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO debug_payload_blobs (request_id, org_id, project_id, nonce, encrypted_payload_aead, expires_at_ms, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		reqID, boot.OrgID, boot.ProjectID, nonce, ct, now+3600_000, now,
	); err != nil {
		t.Fatalf("insert debug blob: %v", err)
	}

	for _, path := range []string{"/admin/v1/usage/summary?group_by=project", "/admin/v1/usage/export?format=csv", "/admin/v1/budgets", "/admin/v1/overview/summary"} {
		rec := h.doBearer(t, http.MethodGet, path, finopsTok, "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("FINOPS_VIEWER GET %s expected 200, got %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	for _, tc := range []struct {
		method string
		path   string
		ifm    string
		body   any
	}{
		{http.MethodGet, "/admin/v1/secrets", "", nil},
		{http.MethodPost, "/admin/v1/secrets", "", map[string]any{"name": "x", "secret_kind": "API_KEY", "secret_value": "sk-1234"}},
		{http.MethodPut, "/admin/v1/budgets/" + idgen.MustNew(idgen.PrefixBudget), `"v1"`, map[string]any{"scope_tier": "PROJECT", "scope_id": boot.ProjectID, "period": "MONTHLY", "hard_limit_micros": 1000}},
		{http.MethodPost, "/admin/v1/extensions/install", "", map[string]any{"source_uri": "https://marketplace.tanpopo.dev/a.tpkg", "expected_sha256": strings.Repeat("a", 64)}},
		{http.MethodGet, "/admin/v1/traces/" + reqID + "/debug-payload", "", nil},
		{http.MethodPost, "/admin/v1/kill-switches", "", map[string]any{"target_level": "PROJECT", "target_id": boot.ProjectID, "abort_active_streams": true, "reason": "Unauthorized check"}},
	} {
		rec := h.doBearer(t, tc.method, tc.path, finopsTok, tc.ifm, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("FINOPS_VIEWER %s %s expected 403, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	recTeamOrgSec := h.doBearer(t, http.MethodPost, "/admin/v1/secrets", teamAdminTok, "", map[string]any{
		"name":         "org-secret-attempt",
		"secret_kind":  "API_KEY",
		"secret_value": "sk-team-denied-1234",
	})
	if recTeamOrgSec.Code != http.StatusForbidden {
		t.Fatalf("TEAM_ADMIN org-wide secret expected 403, got %d body=%s", recTeamOrgSec.Code, recTeamOrgSec.Body.String())
	}
	recTeamProjSec := h.doBearer(t, http.MethodPost, "/admin/v1/secrets", teamAdminTok, "", map[string]any{
		"name":         "proj-secret-ok",
		"project_id":   boot.ProjectID,
		"secret_kind":  "API_KEY",
		"secret_value": "sk-team-allowed-9876",
	})
	if recTeamProjSec.Code != http.StatusCreated {
		t.Fatalf("TEAM_ADMIN project secret expected 201, got %d body=%s", recTeamProjSec.Code, recTeamProjSec.Body.String())
	}
	var teamSec secretMetadataDTO
	mustDecodeJSON(t, recTeamProjSec.Body.Bytes(), &teamSec)

	tenantBTok := "tok_tenant_b_owner"
	h.registerBearerToken(tenantBTok, &rbac.Actor{
		PrincipalID:     "pri_tenant_b",
		SessionOrgID:    &org2.ID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &org2.ID, ScopeType: rbac.ScopeOrg, ScopeID: org2.ID, RoleName: rbac.RoleOrgOwner}},
	})
	for _, tc := range []struct {
		method string
		path   string
		ifm    string
		body   any
	}{
		{http.MethodDelete, "/admin/v1/secrets/" + teamSec.CredentialID, `"v1"`, nil},
		{http.MethodGet, "/admin/v1/traces/" + reqID, "", nil},
		{http.MethodGet, "/admin/v1/traces/" + reqID + "/debug-payload", "", nil},
		{http.MethodPost, "/admin/v1/kill-switches", "", map[string]any{"target_level": "PROJECT", "target_id": boot.ProjectID, "abort_active_streams": true, "reason": "cross tenant oracle check"}},
	} {
		rec := h.doBearer(t, tc.method, tc.path, tenantBTok, tc.ifm, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("AUTH-15 cross-tenant %s %s expected 404, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	recSpoof := h.doBearer(t, http.MethodPost, "/admin/v1/models", tenantBTok, "", map[string]any{
		"org_id":            boot.OrgID,
		"slug":              "spoofed-model",
		"display_name":      "Spoof",
		"context_window":    4096,
		"max_output_tokens": 1024,
	})
	if recSpoof.Code != http.StatusForbidden {
		t.Fatalf("expected 403 org_id_spoofing_forbidden, got %d body=%s", recSpoof.Code, recSpoof.Body.String())
	}
}

func TestPart2SSRFProbeRateLimitAndConnectionsSecurity(t *testing.T) {
	t.Parallel()
	h := newPart2TestHarness(t, 2)
	boot, ownerCookie := h.bootstrapOwner(t, "SSRF Org", "ssrf-proj")

	probeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"llama3"}]}`))
	}))
	defer probeSrv.Close()
	probeURL, _ := url.Parse(probeSrv.URL)
	adpID := seedLocalOpenAIAdapter(t, h, probeURL.Hostname())

	for _, badURL := range []string{
		"http://169.254.169.254/latest/meta-data",
		"http://private.internal.test/v1",
		"http://127.0.0.1:9090/v1",
	} {
		allowPriv := strings.Contains(badURL, "169.254") || strings.Contains(badURL, "9090")
		recBad := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
			"name":             "bad-ssrf",
			"adapter_id":       adpID,
			"billing_mode":     "SELF_HOSTED_INFRA",
			"endpoint_url":     badURL,
			"data_region":      "ON_PREM",
			"allow_private_ip": allowPriv,
		})
		if recBad.Code != http.StatusBadRequest && recBad.Code != http.StatusUnprocessableEntity {
			t.Fatalf("SSRF expected 400/422 for %s, got %d body=%s", badURL, recBad.Code, recBad.Body.String())
		}
	}

	recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "local-ollama",
		"adapter_id":       adpID,
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     probeSrv.URL,
		"data_region":      "ON_PREM",
		"allow_private_ip": true,
	})
	if recConn.Code != http.StatusCreated {
		t.Fatalf("POST /connections local expected 201, got %d body=%s", recConn.Code, recConn.Body.String())
	}
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)

	for i := 0; i < 2; i++ {
		rec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("probe #%d expected 200, got %d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	recLimited := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recLimited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 probe_rate_limited on 3rd probe, got %d body=%s", recLimited.Code, recLimited.Body.String())
	}
	var env CanonicalErrorEnvelope
	mustDecodeJSON(t, recLimited.Body.Bytes(), &env)
	if env.Error.Code != "probe_rate_limited" {
		t.Fatalf("expected probe_rate_limited code, got %s", env.Error.Code)
	}

	hNoRuntime := newPart2TestHarness(t, 10)
	hNoRuntime.srv.broker = nil
	bootNR, cookieNR := hNoRuntime.bootstrapOwner(t, "NoRuntime Org", "nr-proj")
	adpNR := seedLocalOpenAIAdapter(t, hNoRuntime, probeURL.Hostname())
	recConnNR := hNoRuntime.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", cookieNR, bootNR.CSRFToken, "", map[string]any{
		"name":             "nr-conn",
		"adapter_id":       adpNR,
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     probeSrv.URL,
		"data_region":      "ON_PREM",
		"allow_private_ip": true,
	})
	if recConnNR.Code != http.StatusCreated {
		t.Fatalf("POST /connections NR code=%d body=%s", recConnNR.Code, recConnNR.Body.String())
	}
	var connNR providerConnectionDTO
	mustDecodeJSON(t, recConnNR.Body.Bytes(), &connNR)
	recUnavail := hNoRuntime.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+connNR.ID+"/test", cookieNR, bootNR.CSRFToken, "", nil)
	if recUnavail.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 probe_runtime_unavailable, got %d body=%s", recUnavail.Code, recUnavail.Body.String())
	}
	var envUnavail CanonicalErrorEnvelope
	mustDecodeJSON(t, recUnavail.Body.Bytes(), &envUnavail)
	if envUnavail.Error.Code != "probe_runtime_unavailable" {
		t.Fatalf("expected probe_runtime_unavailable code, got %s", envUnavail.Error.Code)
	}
}

func TestPart2PoliciesRoutesModelsAndOCC(t *testing.T) {
	t.Parallel()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "Policy Org", "pol-proj")

	adpID := seedLocalOpenAIAdapter(t, h, "127.0.0.1")
	recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "us-conn",
		"adapter_id":       adpID,
		"billing_mode":     "COMMERCIAL_API",
		"endpoint_url":     "https://api.openai.com/v1",
		"data_region":      "US",
		"allow_private_ip": false,
	})
	if recConn.Code != http.StatusCreated {
		t.Fatalf("POST /connections code=%d body=%s", recConn.Code, recConn.Body.String())
	}
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)

	recMod := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/models", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":              "claude-sonnet",
		"display_name":      "Claude Sonnet",
		"context_window":    200000,
		"max_output_tokens": 8192,
		"assurance_tier":    "TESTED",
	})
	if recMod.Code != http.StatusCreated {
		t.Fatalf("POST /models code=%d body=%s", recMod.Code, recMod.Body.String())
	}
	var mod logicalModelDTO
	mustDecodeJSON(t, recMod.Body.Bytes(), &mod)

	recDep := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/deployments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":              boot.EnvID,
		"logical_model_id":    mod.ID,
		"connection_id":       conn.ID,
		"upstream_model_name": "claude-sonnet-4",
		"data_region":         "US",
		"rate_card": map[string]any{
			"input_token_nano_usd":  3000,
			"output_token_nano_usd": 15000,
		},
	})
	if recDep.Code != http.StatusCreated {
		t.Fatalf("POST /deployments code=%d body=%s", recDep.Code, recDep.Body.String())
	}
	var dep modelDeploymentDTO
	mustDecodeJSON(t, recDep.Body.Bytes(), &dep)

	orgPolID := idgen.MustNew(idgen.PrefixPolicy)
	recOrgPol := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/policies/"+orgPolID, ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"scope_tier":               "ORGANIZATION",
		"scope_id":                 boot.OrgID,
		"allowed_data_regions":     []string{"EU"},
		"min_plugin_trust":         "OFFICIAL_ONLY",
		"min_capability_assurance": "VERIFIED",
		"rpm_ceiling":              100,
	})
	if recOrgPol.Code != http.StatusOK {
		t.Fatalf("PUT org policy code=%d body=%s", recOrgPol.Code, recOrgPol.Body.String())
	}

	projPolID := idgen.MustNew(idgen.PrefixPolicy)
	recRelax := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/policies/"+projPolID, ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"scope_tier":               "PROJECT",
		"scope_id":                 boot.ProjectID,
		"allowed_data_regions":     []string{"US"},
		"min_plugin_trust":         "ANY_APPROVED",
		"min_capability_assurance": "DECLARED",
		"rpm_ceiling":              500,
	})
	if recRelax.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 policy_relaxation_forbidden, got %d body=%s", recRelax.Code, recRelax.Body.String())
	}
	var envErr CanonicalErrorEnvelope
	mustDecodeJSON(t, recRelax.Body.Bytes(), &envErr)
	if envErr.Error.Code != "policy_relaxation_forbidden" {
		t.Fatalf("expected policy_relaxation_forbidden code, got %s", envErr.Error.Code)
	}

	recOrgPol2 := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/policies/"+orgPolID, ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"scope_tier":               "ORGANIZATION",
		"scope_id":                 boot.OrgID,
		"allowed_data_regions":     []string{"US"},
		"min_plugin_trust":         "OFFICIAL_ONLY",
		"min_capability_assurance": "VERIFIED",
		"rpm_ceiling":              100,
	})
	if recOrgPol2.Code != http.StatusOK {
		t.Fatalf("PUT org policy v2 code=%d body=%s", recOrgPol2.Code, recOrgPol2.Body.String())
	}

	recBadWeight := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":     "weighted-bad",
		"env_id":   boot.EnvID,
		"strategy": "WEIGHTED",
		"targets":  []map[string]any{{"deployment_id": dep.ID, "priority_order": 1, "weight": 60}},
	})
	if recBadWeight.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 invalid_weight_sum, got %d body=%s", recBadWeight.Code, recBadWeight.Body.String())
	}

	recNoIfMatch := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+conn.ID, ownerCookie, boot.CSRFToken, "", map[string]any{"name": "renamed"})
	if recNoIfMatch.Code != http.StatusPreconditionRequired {
		t.Fatalf("expected 428 missing If-Match, got %d body=%s", recNoIfMatch.Code, recNoIfMatch.Body.String())
	}
	recStaleConn := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+conn.ID, ownerCookie, boot.CSRFToken, `"v99"`, map[string]any{"name": "stale"})
	if recStaleConn.Code != http.StatusConflict {
		t.Fatalf("expected 409 stale If-Match, got %d body=%s", recStaleConn.Code, recStaleConn.Body.String())
	}
	var errStale CanonicalErrorEnvelope
	mustDecodeJSON(t, recStaleConn.Body.Bytes(), &errStale)
	if errStale.Error.Code != "optimistic_lock_conflict" || errStale.Error.CurrentResource == nil {
		t.Fatalf("expected optimistic_lock_conflict + current_resource, got %+v", errStale.Error)
	}
}

func TestPart2TracesDebugPayloadKillSwitchAndAuditTamper(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "Ops Org", "ops-proj")
	now := h.fk.Now().UTC().UnixMilli()

	reqValid := "trc_dbg_valid_1"
	reqExpired := "trc_dbg_expired_1"
	nonceV, ctV, err := h.vault.SealProjectBlob(ctx, boot.OrgID, boot.ProjectID, vault.RecordKindDebugBlob, reqValid, []byte(`{"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("SealProjectBlob valid: %v", err)
	}
	nonceE, ctE, err := h.vault.SealProjectBlob(ctx, boot.OrgID, boot.ProjectID, vault.RecordKindDebugBlob, reqExpired, []byte(`{"messages":[]}`))
	if err != nil {
		t.Fatalf("SealProjectBlob expired: %v", err)
	}
	_, err = h.st.RawDB().ExecContext(ctx, `
		INSERT INTO debug_payload_blobs (request_id, org_id, project_id, nonce, encrypted_payload_aead, expires_at_ms, created_at_ms)
		VALUES
		(?, ?, ?, ?, ?, ?, ?),
		(?, ?, ?, ?, ?, ?, ?)`,
		reqValid, boot.OrgID, boot.ProjectID, nonceV, ctV, now+3600_000, now,
		reqExpired, boot.OrgID, boot.ProjectID, nonceE, ctE, now-1000, now-7200_000,
	)
	if err != nil {
		t.Fatalf("insert debug blobs: %v", err)
	}

	recDbg := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/traces/"+reqValid+"/debug-payload", ownerCookie, "", "", nil)
	if recDbg.Code != http.StatusOK {
		t.Fatalf("GET debug-payload code=%d body=%s", recDbg.Code, recDbg.Body.String())
	}
	var dbgResp debugPayloadResponseDTO
	mustDecodeJSON(t, recDbg.Body.Bytes(), &dbgResp)
	if dbgResp.RequestID != reqValid {
		t.Fatalf("unexpected request_id in debug payload response: %s", dbgResp.RequestID)
	}

	var viewedAuditCnt int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE org_id = ? AND event_type = 'trace.debug_payload_viewed' AND target_id = ?`, boot.OrgID, reqValid).Scan(&viewedAuditCnt); err != nil || viewedAuditCnt != 1 {
		t.Fatalf("expected 1 trace.debug_payload_viewed audit event, cnt=%d err=%v", viewedAuditCnt, err)
	}

	recExp := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/traces/"+reqExpired+"/debug-payload", ownerCookie, "", "", nil)
	if recExp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for expired debug payload, got %d body=%s", recExp.Code, recExp.Body.String())
	}

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()
	h.killSwitch.Streams().Register(killswitch.StreamRegistration{
		OrgID:     boot.OrgID,
		ProjectID: boot.ProjectID,
		Cancel:    streamCancel,
	})

	recKS := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/kill-switches", ownerCookie, boot.CSRFToken, "", map[string]any{
		"target_level":         "PROJECT",
		"target_id":            boot.ProjectID,
		"confirm_slug":         "ops-proj",
		"abort_active_streams": true,
		"reason":               "Abort active stream on runaway loop",
	})
	if recKS.Code != http.StatusOK {
		t.Fatalf("POST /kill-switches code=%d body=%s", recKS.Code, recKS.Body.String())
	}
	var ksResp killSwitchTriggerResponseDTO
	mustDecodeJSON(t, recKS.Body.Bytes(), &ksResp)
	if ksResp.SeveredStreamsCount != 1 || ksResp.ConfigEpoch < 1 {
		t.Fatalf("expected severed_streams_count=1 and config_epoch>=1, got %+v", ksResp)
	}
	select {
	case <-streamCtx.Done():
	default:
		t.Fatalf("expected active stream context canceled immediately on kill-switch trigger")
	}

	recKS2 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/kill-switches", ownerCookie, boot.CSRFToken, "", map[string]any{
		"target_level":         "PROJECT",
		"target_id":            boot.ProjectID,
		"confirm_slug":         "ops-proj",
		"abort_active_streams": true,
		"reason":               "Duplicate kill switch check",
	})
	if recKS2.Code != http.StatusConflict {
		t.Fatalf("expected 409 kill_switch_already_active, got %d body=%s", recKS2.Code, recKS2.Body.String())
	}

	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO request_traces_meta (
			request_id, created_at_ms, started_at_ms, org_id, project_id, env_id, virtual_key_id, connection_id,
			entry_point, route_slug, http_status, proxy_overhead_us, total_duration_ms, waterfall_stages_json
		) VALUES ('trc_br_1', ?, ?, ?, ?, ?, 'vk_br_1', 'conn_br_1', 'unified_http', 'gpt-4o', 200, 100, 15, '[]')`,
		now, now-15, boot.OrgID, boot.ProjectID, boot.EnvID,
	); err != nil {
		t.Fatalf("insert trace for blast radius: %v", err)
	}
	recBR := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/blast-radius?target_type=PROVIDER_CONNECTION&target_id=conn_br_1&since=24h", ownerCookie, "", "", nil)
	if recBR.Code != http.StatusOK {
		t.Fatalf("GET /blast-radius code=%d body=%s", recBR.Code, recBR.Body.String())
	}
	var brDTO blastRadiusResponseDTO
	mustDecodeJSON(t, recBR.Body.Bytes(), &brDTO)
	if len(brDTO.Connections) != 1 || brDTO.Connections[0] != "conn_br_1" || len(brDTO.VirtualKeys) != 1 || brDTO.VirtualKeys[0] != "vk_br_1" {
		t.Fatalf("unexpected blast radius payload: %+v", brDTO)
	}

	if _, err := h.st.RawDB().ExecContext(ctx, `DROP TRIGGER IF EXISTS trg_audit_events_no_update`); err != nil {
		t.Fatalf("drop audit trigger: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE audit_events SET entry_hash = 'tampered_hash_value' WHERE org_id = ? AND seq_no = 1`, boot.OrgID); err != nil {
		t.Fatalf("tamper audit event: %v", err)
	}
	recVerifyTampered := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/audit-events/verify", ownerCookie, boot.CSRFToken, "", nil)
	if recVerifyTampered.Code != http.StatusOK {
		t.Fatalf("POST /audit-events/verify code=%d body=%s", recVerifyTampered.Code, recVerifyTampered.Body.String())
	}
	var verTampered verifyAuditResponseDTO
	mustDecodeJSON(t, recVerifyTampered.Body.Bytes(), &verTampered)
	if verTampered.Intact || verTampered.FirstBrokenSeqNo == nil || *verTampered.FirstBrokenSeqNo != 1 {
		t.Fatalf("expected intact=false first_broken_seq_no=1, got %+v", verTampered)
	}
}

func TestPart2MultiTenantExtensionRBACAndFourEyesHardening(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "MT Org 1", "mt-proj-1")

	// Add a second active organization to enter multi-tenant mode (> 1 active org).
	org2, err := h.st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "mt-org-2", DisplayName: "MT Org 2"})
	if err != nil {
		t.Fatalf("CreateOrganization mt-org-2: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE organizations SET require_four_eyes_approval = 1 WHERE id = ?`, boot.OrgID); err != nil {
		t.Fatalf("enable four-eyes on org1: %v", err)
	}

	// Register an ORG_OWNER-only actor (no INSTANCE OPERATOR) and two INSTANCE OPERATOR actors.
	orgOwnerOnlyTok := "tok_mt_org_owner_only"
	h.registerBearerToken(orgOwnerOnlyTok, &rbac.Actor{
		PrincipalID:     "pri_mt_org_owner_only",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleOrgOwner}},
	})

	reviewerID := idgen.MustNew(idgen.PrefixPrincipal)
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'sec-mt@example.test', 'ACTIVE', 1000, 1000)`,
		reviewerID, boot.OrgID,
	); err != nil {
		t.Fatalf("insert reviewer: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_mt_rev', ?, ?, 'ORG', ?, 'SECURITY_REVIEWER', 1, 1000)`,
		boot.OrgID, reviewerID, boot.OrgID,
	); err != nil {
		t.Fatalf("insert reviewer binding: %v", err)
	}
	secRevTok := "tok_mt_sec_rev"
	h.registerBearerToken(secRevTok, &rbac.Actor{
		PrincipalID:     reviewerID,
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleSecurityReviewer}},
	})

	opStagerID := idgen.MustNew(idgen.PrefixPrincipal)
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, created_at_ms, updated_at_ms)
		VALUES (?, NULL, 'ADMIN_SERVICE_TOKEN', 'op-stager@example.test', 'ACTIVE', 1000, 1000)`,
		opStagerID,
	); err != nil {
		t.Fatalf("insert opStager: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_mt_op1', NULL, ?, 'INSTANCE', 'instance', 'OPERATOR', 1, 1000)`,
		opStagerID,
	); err != nil {
		t.Fatalf("insert op1 binding: %v", err)
	}
	opStagerTok := "tok_mt_op_stager"
	h.registerBearerToken(opStagerTok, &rbac.Actor{
		PrincipalID:     opStagerID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	})

	opApproverID := idgen.MustNew(idgen.PrefixPrincipal)
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, created_at_ms, updated_at_ms)
		VALUES (?, NULL, 'ADMIN_SERVICE_TOKEN', 'op-approver@example.test', 'ACTIVE', 1000, 1000)`,
		opApproverID,
	); err != nil {
		t.Fatalf("insert opApprover: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_mt_op2', NULL, ?, 'INSTANCE', 'instance', 'OPERATOR', 1, 1000)`,
		opApproverID,
	); err != nil {
		t.Fatalf("insert op2 binding: %v", err)
	}
	opApproverTok := "tok_mt_op_approver"
	h.registerBearerToken(opApproverTok, &rbac.Actor{
		PrincipalID:     opApproverID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	})

	v1URI := "https://marketplace.tanpopo.dev/mt-1.0.0.tpkg"
	v1SHA := h.registerSignedProviderTPKG(t, v1URI, "@tanpopo/mt-adapter", "1.0.0", []string{"api.openai.com"}, false, true)
	v2URI := "https://marketplace.tanpopo.dev/mt-1.1.0.tpkg"
	v2SHA := h.registerSignedProviderTPKG(t, v2URI, "@tanpopo/mt-adapter", "1.1.0", []string{"api.openai.com", "eu.api.openai.com"}, false, true)

	// ORG_OWNER cannot stage in multi-tenant mode without INSTANCE OPERATOR.
	recOrgStage := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/install", orgOwnerOnlyTok, "", map[string]any{
		"source_uri":      v1URI,
		"expected_sha256": v1SHA,
	})
	if recOrgStage.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for ORG_OWNER staging in multi-tenant mode, got %d body=%s", recOrgStage.Code, recOrgStage.Body.String())
	}

	// INSTANCE OPERATOR stages v1.
	recStage := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/install?org_id="+boot.OrgID, opStagerTok, "", map[string]any{
		"source_uri":      v1URI,
		"expected_sha256": v1SHA,
	})
	if recStage.Code != http.StatusAccepted {
		t.Fatalf("operator stage v1 expected 202, got %d body=%s", recStage.Code, recStage.Body.String())
	}
	var stageDiff extensionPermissionDiffDTO
	mustDecodeJSON(t, recStage.Body.Bytes(), &stageDiff)
	stageETag := recStage.Result().Header.Get("ETag")

	// Cross-org ?org_id=org2 bypass attempt for four-eyes self-approval MUST fail with 403.
	recSelfBypass := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+stageDiff.InstallationID+"/approve-permissions?org_id="+org2.ID, opStagerTok, stageETag, map[string]any{
		"enable_immediately": true,
	})
	if recSelfBypass.Code != http.StatusForbidden {
		t.Fatalf("expected 403 self_approval_forbidden across ?org_id bypass, got %d body=%s", recSelfBypass.Code, recSelfBypass.Body.String())
	}

	// Org-scoped SECURITY_REVIEWER cannot approve global extension in multi-tenant mode.
	recRevBlocked := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+stageDiff.InstallationID+"/approve-permissions", secRevTok, stageETag, map[string]any{
		"enable_immediately": true,
	})
	if recRevBlocked.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for SECURITY_REVIEWER in multi-tenant mode, got %d body=%s", recRevBlocked.Code, recRevBlocked.Body.String())
	}

	// Distinct INSTANCE OPERATOR approves v1.
	recAppV1 := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+stageDiff.InstallationID+"/approve-permissions?org_id="+boot.OrgID, opApproverTok, stageETag, map[string]any{
		"enable_immediately": true,
	})
	if recAppV1.Code != http.StatusOK {
		t.Fatalf("operator approve v1 expected 200, got %d body=%s", recAppV1.Code, recAppV1.Body.String())
	}
	var insV1 extensionInstallationDTO
	mustDecodeJSON(t, recAppV1.Body.Bytes(), &insV1)

	// Upgrade v1 -> v2 requires INSTANCE OPERATOR (ORG_OWNER blocked), then approved by distinct operator.
	recOrgUp := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+insV1.ID+"/upgrade", orgOwnerOnlyTok, FormatETag(insV1.Version), map[string]any{
		"source_uri":      v2URI,
		"expected_sha256": v2SHA,
	})
	if recOrgUp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for ORG_OWNER upgrade in multi-tenant mode, got %d body=%s", recOrgUp.Code, recOrgUp.Body.String())
	}
	recUp := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+insV1.ID+"/upgrade?org_id="+boot.OrgID, opStagerTok, FormatETag(insV1.Version), map[string]any{
		"source_uri":      v2URI,
		"expected_sha256": v2SHA,
	})
	if recUp.Code != http.StatusAccepted {
		t.Fatalf("operator upgrade expected 202, got %d body=%s", recUp.Code, recUp.Body.String())
	}
	upETag := recUp.Result().Header.Get("ETag")
	recAppV2 := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+insV1.ID+"/approve-permissions?org_id="+boot.OrgID, opApproverTok, upETag, map[string]any{
		"enable_immediately": true,
	})
	if recAppV2.Code != http.StatusOK {
		t.Fatalf("operator approve v2 expected 200, got %d body=%s", recAppV2.Code, recAppV2.Body.String())
	}
	var insV2 extensionInstallationDTO
	mustDecodeJSON(t, recAppV2.Body.Bytes(), &insV2)

	// Multi-tenant rollback & uninstall & extension kill-switch blocked for ORG_OWNER.
	recOrgRB := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+insV2.ID+"/rollback", orgOwnerOnlyTok, FormatETag(insV2.Version), map[string]any{"reason": "Valid rollback reason"})
	if recOrgRB.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for ORG_OWNER rollback in multi-tenant mode, got %d body=%s", recOrgRB.Code, recOrgRB.Body.String())
	}
	recOrgDel := h.doBearer(t, http.MethodDelete, "/admin/v1/extensions/"+insV2.ID, orgOwnerOnlyTok, FormatETag(insV2.Version), nil)
	if recOrgDel.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for ORG_OWNER uninstall in multi-tenant mode, got %d body=%s", recOrgDel.Code, recOrgDel.Body.String())
	}
	recOrgExtKS := h.doBearer(t, http.MethodPost, "/admin/v1/kill-switches", orgOwnerOnlyTok, "", map[string]any{
		"target_level":         "EXTENSION",
		"target_id":            insV2.ID,
		"abort_active_streams": true,
		"reason":               "Tenant owner extension kill-switch must fail in multi-tenant",
	})
	if recOrgExtKS.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for ORG_OWNER extension kill-switch in multi-tenant mode, got %d body=%s", recOrgExtKS.Code, recOrgExtKS.Body.String())
	}

	// Short rollback reason rejected (400), valid reason succeeds and emits rollback reason audit event.
	recShortRB := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+insV2.ID+"/rollback?org_id="+boot.OrgID, opStagerTok, FormatETag(insV2.Version), map[string]any{"reason": "bad"})
	if recShortRB.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on short rollback reason, got %d body=%s", recShortRB.Code, recShortRB.Body.String())
	}
	rbReason := "Post-upgrade regression containment reason"
	recGoodRB := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+insV2.ID+"/rollback?org_id="+boot.OrgID, opStagerTok, FormatETag(insV2.Version), map[string]any{"reason": rbReason})
	if recGoodRB.Code != http.StatusOK {
		t.Fatalf("operator rollback expected 200, got %d body=%s", recGoodRB.Code, recGoodRB.Body.String())
	}
	var rbAuditReason string
	if err := h.st.RawDB().QueryRowContext(ctx, `
		SELECT json_extract(metadata_json, '$.reason')
		FROM audit_events
		WHERE event_type = 'extension.rollback_reason_recorded' AND target_id = ?
		ORDER BY seq_no DESC LIMIT 1`, insV2.ID,
	).Scan(&rbAuditReason); err != nil || rbAuditReason != rbReason {
		t.Fatalf("expected extension.rollback_reason_recorded audit event with reason %q, got %q err=%v", rbReason, rbAuditReason, err)
	}

	// pinned_installation_id validation in PUT /organizations/{orgId}/extension-policies.
	recBadPin := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/organizations/"+boot.OrgID+"/extension-policies", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"min_plugin_trust": "OFFICIAL_ONLY",
		"policies": []map[string]any{
			{"package_id": insV2.PackageID, "enabled": true, "pinned_installation_id": "ins_nonexistent"},
		},
	})
	if recBadPin.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent pinned_installation_id, got %d body=%s", recBadPin.Code, recBadPin.Body.String())
	}

	// Operator can trigger EXTENSION kill-switch in multi-tenant mode.
	recOpExtKS := h.doBearer(t, http.MethodPost, "/admin/v1/kill-switches?org_id="+boot.OrgID, opStagerTok, "", map[string]any{
		"target_level":         "EXTENSION",
		"target_id":            insV2.ID,
		"abort_active_streams": true,
		"reason":               "Operator multi-tenant extension quarantine",
	})
	if recOpExtKS.Code != http.StatusOK {
		t.Fatalf("operator EXTENSION kill-switch expected 200, got %d body=%s", recOpExtKS.Code, recOpExtKS.Body.String())
	}
}

func TestPart2ConnectionPatchParityAndSSRFErrorRedaction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "ConnParity Org", "proj-a")

	recPB := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "proj-b",
		"name": "Project B",
	})
	if recPB.Code != http.StatusCreated {
		t.Fatalf("create proj-b: %d %s", recPB.Code, recPB.Body.String())
	}
	var projB projectDTO
	mustDecodeJSON(t, recPB.Body.Bytes(), &projB)

	// Closed local port for safe unreachable dial error redaction check.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	closedAddr := ln.Addr().String()
	_ = ln.Close()

	// Bad TLS server (self-signed untrusted cert) for TLS error redaction check.
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer tlsSrv.Close()

	adpID := seedLocalOpenAIAdapter(t, h, "127.0.0.1")
	rootFP := trust.PublicKeyFingerprint(h.rootPub)
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE extension_versions SET signing_key_fingerprint = ? WHERE package_id = 'tanpopo.local_openai'`, rootFP); err != nil {
		t.Fatalf("sign seeded local adapter: %v", err)
	}

	recSecA := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "sec-a",
		"project_id":   boot.ProjectID,
		"secret_kind":  "API_KEY",
		"secret_value": "sk-proj-a-12345678",
	})
	if recSecA.Code != http.StatusCreated {
		t.Fatalf("create sec-a: %d %s", recSecA.Code, recSecA.Body.String())
	}
	var secA secretMetadataDTO
	mustDecodeJSON(t, recSecA.Body.Bytes(), &secA)

	recSecB := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "sec-b",
		"project_id":   projB.ID,
		"secret_kind":  "API_KEY",
		"secret_value": "sk-proj-b-12345678",
	})
	if recSecB.Code != http.StatusCreated {
		t.Fatalf("create sec-b: %d %s", recSecB.Code, recSecB.Body.String())
	}
	var secB secretMetadataDTO
	mustDecodeJSON(t, recSecB.Body.Bytes(), &secB)

	recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "patch-target",
		"project_id":       boot.ProjectID,
		"adapter_id":       adpID,
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     "http://" + closedAddr + "/v1",
		"data_region":      "ON_PREM",
		"allow_private_ip": true,
		"allowed_cidrs":    []string{"127.0.0.1/32"},
		"secret_bindings":  map[string]string{"api_key": secA.CredentialID},
	})
	if recConn.Code != http.StatusCreated {
		t.Fatalf("create conn: %d %s", recConn.Code, recConn.Body.String())
	}
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)

	// Probe unreachable endpoint -> must redact raw IP/port/net.OpError from sanitized_message.
	recProbeConnRefused := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recProbeConnRefused.Code != http.StatusOK {
		t.Fatalf("probe closed port expected 200 ConnectionTestResponse, got %d body=%s", recProbeConnRefused.Code, recProbeConnRefused.Body.String())
	}
	var probeRefused connectionTestResponseDTO
	mustDecodeJSON(t, recProbeConnRefused.Body.Bytes(), &probeRefused)
	if probeRefused.Valid || probeRefused.SanitizedMessage == nil || strings.Contains(*probeRefused.SanitizedMessage, "127.0.0.1") || strings.Contains(*probeRefused.SanitizedMessage, "dial tcp") {
		t.Fatalf("expected sanitized transport failure message without raw dial/IP details, got %+v", probeRefused)
	}

	// Re-read connection via list endpoint to obtain live revision after failed validation probe (DRAFT -> VALIDATING -> DRAFT).
	recListConn := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/connections?project_id="+boot.ProjectID, ownerCookie, "", "", nil)
	if recListConn.Code != http.StatusOK {
		t.Fatalf("GET /connections expected 200, got %d body=%s", recListConn.Code, recListConn.Body.String())
	}
	var connList paginatedListEnvelope[providerConnectionDTO]
	mustDecodeJSON(t, recListConn.Body.Bytes(), &connList)
	foundConn := false
	for _, c := range connList.Data {
		if c.ID == conn.ID {
			conn = c
			foundConn = true
			break
		}
	}
	if !foundConn {
		t.Fatalf("connection %s not found in GET /connections", conn.ID)
	}

	// Invalid CIDR, wildcard 0.0.0.0/0, empty allowed_cidrs with allow_private_ip=true, unapproved host, port mismatch, cross-project secret binding -> all blocked.
	badPatches := []struct {
		name string
		body map[string]any
	}{
		{"invalid_cidr", map[string]any{"allowed_cidrs": []string{"not-a-cidr"}}},
		{"wildcard_cidr", map[string]any{"allowed_cidrs": []string{"0.0.0.0/0"}}},
		{"empty_cidrs_with_priv", map[string]any{"allowed_cidrs": []string{}}},
		{"unapproved_host", map[string]any{"endpoint_url": "https://evil.example.com/v1"}},
		{"cross_project_secret", map[string]any{"secret_bindings": map[string]string{"api_key": secB.CredentialID}}},
	}
	for _, tc := range badPatches {
		rec := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+conn.ID, ownerCookie, boot.CSRFToken, FormatETag(conn.Version), tc.body)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusNotFound {
			t.Fatalf("PATCH %s expected 400/404/422, got %d body=%s", tc.name, rec.Code, rec.Body.String())
		}
	}

	// Also point connection to untrusted TLS server and confirm sanitized TLS message hides internal IP/cert details.
	recPatchTLS := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+conn.ID, ownerCookie, boot.CSRFToken, FormatETag(conn.Version), map[string]any{
		"endpoint_url": tlsSrv.URL + "/v1",
	})
	if recPatchTLS.Code != http.StatusOK {
		t.Fatalf("PATCH endpoint to tlsSrv expected 200, got %d body=%s", recPatchTLS.Code, recPatchTLS.Body.String())
	}
	mustDecodeJSON(t, recPatchTLS.Body.Bytes(), &conn)

	recProbeTLS := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recProbeTLS.Code != http.StatusOK {
		t.Fatalf("probe untrusted TLS expected 200 ConnectionTestResponse, got %d body=%s", recProbeTLS.Code, recProbeTLS.Body.String())
	}
	var probeTLS connectionTestResponseDTO
	mustDecodeJSON(t, recProbeTLS.Body.Bytes(), &probeTLS)
	if probeTLS.Valid || probeTLS.SanitizedMessage == nil || !strings.Contains(*probeTLS.SanitizedMessage, "TLS") || strings.Contains(*probeTLS.SanitizedMessage, "127.0.0.1") {
		t.Fatalf("expected sanitized TLS handshake message without IP leak, got %+v", probeTLS)
	}

	// Endpoint port restriction from adapter (require_tls=true on extension allows port 443; non-443 rejected unless custom port allowed -> check https://api.openai.com:8443/v1).
	// Force port check by creating a strict TLS-required installation & adapter.
	pkgPort := "@tanpopo/strict-port"
	exvPort := idgen.MustNew(idgen.PrefixExtensionVersion)
	insPort := idgen.MustNew(idgen.PrefixInstallation)
	adpPort := idgen.MustNew(idgen.PrefixAdapter)
	now := h.fk.Now().UTC().UnixMilli()
	permsStrict, _ := json.Marshal(installer.PermissionSet{
		AllowedHosts:   []string{"api.openai.com:443"},
		AllowPrivateIP: false,
		RequireTLS:     true,
		SecretSlots:    []string{"api_key"},
	})
	if _, err := h.st.RawDB().ExecContext(ctx, `INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms) VALUES (?, ?, 'PROVIDER_ADAPTER', 'tanpopo', 'official', ?)`, pkgPort, pkgPort, now); err != nil {
		t.Fatalf("insert pkgPort: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `INSERT INTO extension_versions (id, package_id, semver, contract_version, core_version_range, execution_tier, bundle_sha256, signature_verified, enterprise_signed, signing_key_fingerprint, source_uri, manifest_json, created_at_ms) VALUES (?, ?, '1.0.0', 'v1', '>=0.1.0', 'DECLARATIVE_T1', ?, 1, 0, ?, 'builtin://strict', '{}', ?)`, exvPort, pkgPort, strings.Repeat("d", 64), rootFP, now); err != nil {
		t.Fatalf("insert exvPort: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `INSERT INTO extension_installations (id, package_id, active_version_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms) VALUES (?, ?, ?, 'ENABLED_ACTIVE', ?, 1, ?, ?)`, insPort, pkgPort, exvPort, string(permsStrict), now, now); err != nil {
		t.Fatalf("insert insPort: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `INSERT INTO provider_adapters (id, installation_id, adapter_slug, supported_dialects_json, allowed_hosts_json, version, created_at_ms) VALUES (?, ?, 'strict-openai', '["openai_compatible"]', '["api.openai.com:443"]', 1, ?)`, adpPort, insPort, now); err != nil {
		t.Fatalf("insert adpPort: %v", err)
	}
	recStrictConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "strict-conn",
		"adapter_id":       adpPort,
		"billing_mode":     "COMMERCIAL_API",
		"endpoint_url":     "https://api.openai.com/v1",
		"data_region":      "US",
		"allow_private_ip": false,
		"secret_bindings":  map[string]string{"api_key": secA.CredentialID},
		"project_id":       boot.ProjectID,
	})
	if recStrictConn.Code != http.StatusCreated {
		t.Fatalf("create strict conn: %d %s", recStrictConn.Code, recStrictConn.Body.String())
	}
	var strictConn providerConnectionDTO
	mustDecodeJSON(t, recStrictConn.Body.Bytes(), &strictConn)

	recPortMismatch := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+strictConn.ID, ownerCookie, boot.CSRFToken, FormatETag(strictConn.Version), map[string]any{
		"endpoint_url": "https://api.openai.com:8443/v1",
	})
	if recPortMismatch.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 disallowed_host on port 8443 under adapter/SM-01 port parity, got %d body=%s", recPortMismatch.Code, recPortMismatch.Body.String())
	}

	// Revoke strictConn in DB -> subsequent PATCH must be rejected (422/409).
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE provider_connections SET state = ? WHERE id = ?`, catalog.ConnStateRevoked, strictConn.ID); err != nil {
		t.Fatalf("revoke strictConn: %v", err)
	}
	recRevokedPatch := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+strictConn.ID, ownerCookie, boot.CSRFToken, FormatETag(strictConn.Version), map[string]any{
		"name": "mutating-revoked",
	})
	if recRevokedPatch.Code != http.StatusUnprocessableEntity && recRevokedPatch.Code != http.StatusConflict {
		t.Fatalf("expected 409/422 mutating REVOKED connection, got %d body=%s", recRevokedPatch.Code, recRevokedPatch.Body.String())
	}
}

func TestPart2RouteValidationAndIfMatchOrdering(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "RouteIfMatch Org", "proj-1")

	recP2 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "proj-2",
		"name": "Project 2",
	})
	if recP2.Code != http.StatusCreated {
		t.Fatalf("create proj-2: %d %s", recP2.Code, recP2.Body.String())
	}
	var proj2 projectDTO
	mustDecodeJSON(t, recP2.Body.Bytes(), &proj2)

	recEnv2 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects/"+proj2.ID+"/environments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "staging",
		"name": "Staging",
	})
	if recEnv2.Code != http.StatusCreated {
		t.Fatalf("create env2: %d %s", recEnv2.Code, recEnv2.Body.String())
	}
	var env2 environmentDTO
	mustDecodeJSON(t, recEnv2.Body.Bytes(), &env2)

	recEnv1Alt := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects/"+boot.ProjectID+"/environments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "prod-1",
		"name": "Prod 1",
	})
	if recEnv1Alt.Code != http.StatusCreated {
		t.Fatalf("create env1Alt: %d %s", recEnv1Alt.Code, recEnv1Alt.Body.String())
	}
	var env1Alt environmentDTO
	mustDecodeJSON(t, recEnv1Alt.Body.Bytes(), &env1Alt)

	adpID := seedLocalOpenAIAdapter(t, h, "127.0.0.1")
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE extension_versions SET signing_key_fingerprint = ? WHERE package_id = 'tanpopo.local_openai'`, trust.PublicKeyFingerprint(h.rootPub)); err != nil {
		t.Fatalf("sign seeded local adapter: %v", err)
	}
	recConn2 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "conn-proj-2",
		"project_id":       proj2.ID,
		"adapter_id":       adpID,
		"billing_mode":     "COMMERCIAL_API",
		"endpoint_url":     "https://api.openai.com/v1",
		"data_region":      "US",
		"allow_private_ip": false,
	})
	if recConn2.Code != http.StatusCreated {
		t.Fatalf("create conn2: %d %s", recConn2.Code, recConn2.Body.String())
	}
	var conn2 providerConnectionDTO
	mustDecodeJSON(t, recConn2.Body.Bytes(), &conn2)

	recMod := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/models", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":              "gpt-route",
		"display_name":      "GPT Route",
		"context_window":    32000,
		"max_output_tokens": 4096,
	})
	if recMod.Code != http.StatusCreated {
		t.Fatalf("create model: %d %s", recMod.Code, recMod.Body.String())
	}
	var mod logicalModelDTO
	mustDecodeJSON(t, recMod.Body.Bytes(), &mod)

	recDep2 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/deployments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":              env2.ID,
		"logical_model_id":    mod.ID,
		"connection_id":       conn2.ID,
		"upstream_model_name": "gpt-route",
		"data_region":         "US",
		"rate_card":           map[string]any{"input_token_nano_usd": 100, "output_token_nano_usd": 200},
	})
	if recDep2.Code != http.StatusCreated {
		t.Fatalf("create dep2: %d %s", recDep2.Code, recDep2.Body.String())
	}
	var dep2 modelDeploymentDTO
	mustDecodeJSON(t, recDep2.Body.Bytes(), &dep2)

	// Unreferenced legacy deployment with no stored env_id MUST NOT fall back to first org environment.
	legacyDepID := idgen.MustNew(idgen.PrefixDeployment)
	now := h.fk.Now().UTC().UnixMilli()
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO model_deployments (
			id, org_id, connection_id, logical_model_id, upstream_model_name,
			pricing_nano_usd_json, state, connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms,
			version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, 'gpt-legacy', '{"rate_card":{"input_token_nano_usd":1,"output_token_nano_usd":1}}', 'ACTIVE_HEALTHY', 5000, 30000, 15000, 1, ?, ?)`,
		legacyDepID, boot.OrgID, conn2.ID, mod.ID, now, now,
	); err != nil {
		t.Fatalf("insert legacy dep: %v", err)
	}
	if resolvedEnv := h.srv.resolveDeploymentEnvID(ctx, boot.OrgID, legacyDepID); resolvedEnv != "" {
		t.Fatalf("expected empty resolved env for unreferenced legacy deployment, got %q", resolvedEnv)
	}

	// >1 allowed_data_regions rejected with 400.
	recMultiReg := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":                 "multi-reg",
		"env_id":               env2.ID,
		"strategy":             "PRIORITY_FAILOVER",
		"allowed_data_regions": []string{"US", "EU"},
		"targets":              []map[string]any{{"deployment_id": dep2.ID, "priority_order": 1, "weight": 100}},
	})
	if recMultiReg.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on >1 allowed_data_regions, got %d body=%s", recMultiReg.Code, recMultiReg.Body.String())
	}

	// Cross-environment deployment rejected (env1Alt belongs to proj-1 while dep2 belongs to env2).
	recCrossEnv := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":                 "cross-env",
		"env_id":               env1Alt.ID,
		"strategy":             "PRIORITY_FAILOVER",
		"allowed_data_regions": []string{"US"},
		"targets":              []map[string]any{{"deployment_id": dep2.ID, "priority_order": 1, "weight": 100}},
	})
	if recCrossEnv.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on cross-env deployment target, got %d body=%s", recCrossEnv.Code, recCrossEnv.Body.String())
	}

	// Create valid route in env2 and deployment in boot.EnvID for If-Match ordering checks.
	recRoute := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":                 "valid-route",
		"env_id":               env2.ID,
		"strategy":             "PRIORITY_FAILOVER",
		"allowed_data_regions": []string{"US"},
		"targets":              []map[string]any{{"deployment_id": dep2.ID, "priority_order": 1, "weight": 100}},
	})
	if recRoute.Code != http.StatusCreated {
		t.Fatalf("create valid-route: %d %s", recRoute.Code, recRoute.Body.String())
	}
	var rt routeResourceDTO
	mustDecodeJSON(t, recRoute.Body.Bytes(), &rt)

	// Missing If-Match ordering: project-1-only TEAM_ADMIN targeting project-2 resources WITHOUT If-Match MUST get 403/404, NEVER 428!
	teamP1Tok := "tok_route_p1_team"
	h.registerBearerToken(teamP1Tok, &rbac.Actor{
		PrincipalID:     "pri_p1_team",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleTeamAdmin}},
	})
	finopsTok := "tok_route_finops"
	h.registerBearerToken(finopsTok, &rbac.Actor{
		PrincipalID:     "pri_route_fin",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleFinOpsViewer}},
	})

	for _, tc := range []struct {
		method string
		path   string
		tok    string
		body   any
	}{
		{http.MethodPatch, "/admin/v1/connections/" + conn2.ID, teamP1Tok, map[string]any{"name": "x"}},
		{http.MethodDelete, "/admin/v1/connections/" + conn2.ID, teamP1Tok, nil},
		{http.MethodPatch, "/admin/v1/deployments/" + dep2.ID, teamP1Tok, map[string]any{"state": "DISABLED"}},
		{http.MethodDelete, "/admin/v1/deployments/" + dep2.ID, teamP1Tok, nil},
		{http.MethodPut, "/admin/v1/routes/" + rt.ID, teamP1Tok, map[string]any{"slug": "valid-route", "env_id": env2.ID, "strategy": "PRIORITY_FAILOVER", "targets": []map[string]any{{"deployment_id": dep2.ID, "priority_order": 1, "weight": 100}}}},
		{http.MethodDelete, "/admin/v1/routes/" + rt.ID, teamP1Tok, nil},
		{http.MethodPut, "/admin/v1/policies/" + idgen.MustNew(idgen.PrefixPolicy), finopsTok, map[string]any{"scope_tier": "ORGANIZATION", "scope_id": boot.OrgID, "allowed_data_regions": []string{"US"}, "min_plugin_trust": "OFFICIAL_ONLY", "min_capability_assurance": "TESTED"}},
		{http.MethodPut, "/admin/v1/budgets/" + idgen.MustNew(idgen.PrefixBudget), finopsTok, map[string]any{"scope_tier": "PROJECT", "scope_id": boot.ProjectID, "period": "MONTHLY", "hard_limit_micros": 100}},
	} {
		rec := h.doBearer(t, tc.method, tc.path, tc.tok, "", tc.body)
		if rec.Code == http.StatusPreconditionRequired {
			t.Fatalf("%s %s leaked 428 before authorization/existence check! body=%s", tc.method, tc.path, rec.Body.String())
		}
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s expected 403/404 without If-Match, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// TEAM_ADMIN in proj-1 trying to reference dep2 (in proj-2) on route creation in boot.EnvID -> 404.
	recCrossProj := h.doBearer(t, http.MethodPost, "/admin/v1/routes", teamP1Tok, "", map[string]any{
		"slug":                 "cross-proj-route",
		"env_id":               boot.EnvID,
		"strategy":             "PRIORITY_FAILOVER",
		"allowed_data_regions": []string{"US"},
		"targets":              []map[string]any{{"deployment_id": dep2.ID, "priority_order": 1, "weight": 100}},
	})
	if recCrossProj.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-project deployment_id target, got %d body=%s", recCrossProj.Code, recCrossProj.Body.String())
	}
}

func TestPart2FilteredKeysetPaginationLoop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "PagLoop Org", "proj-a")

	recPB := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "proj-b",
		"name": "Project B",
	})
	if recPB.Code != http.StatusCreated {
		t.Fatalf("create proj-b: %d %s", recPB.Code, recPB.Body.String())
	}
	var projB projectDTO
	mustDecodeJSON(t, recPB.Body.Bytes(), &projB)

	adpID := seedLocalOpenAIAdapter(t, h, "127.0.0.1")
	now := h.fk.Now().UTC().UnixMilli()

	// Insert 3 visible proj-a connections, deployments, and budgets OLDER than 230 skipped proj-b items (> maxPageLimit*2).
	const visibleCnt = 3
	const hiddenCnt = 230
	modID := idgen.MustNew(idgen.PrefixLogicalModel)
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO logical_models (id, org_id, canonical_slug, modality_kind, context_window, max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'pag-model', 'CHAT', 4096, 1024, '{}', 1, ?, ?)`,
		modID, boot.OrgID, now, now,
	); err != nil {
		t.Fatalf("insert model: %v", err)
	}

	var connAFirst string
	for i := 0; i < visibleCnt+hiddenCnt; i++ {
		pid := projB.ID
		ts := now + int64(i) // newest items (last inserted) are in projB; oldest visibleCnt items are in projA!
		if i < visibleCnt {
			pid = boot.ProjectID
		}
		cid := idgen.MustNew(idgen.PrefixConnection)
		if i == 0 {
			connAFirst = cid
		}
		if _, err := h.st.RawDB().ExecContext(ctx, `
			INSERT INTO provider_connections (id, org_id, owner_project_id, shared_project_ids_json, adapter_id, name, endpoint_base_url, allow_private_ip, data_region, billing_mode, state, version, created_at_ms, updated_at_ms)
			VALUES (?, ?, ?, '[]', ?, ?, 'https://api.openai.com/v1', 0, 'US', 'COMMERCIAL_API', 'ACTIVE', 1, ?, ?)`,
			cid, boot.OrgID, pid, adpID, fmt.Sprintf("c-%03d", i), ts, ts,
		); err != nil {
			t.Fatalf("insert conn %d: %v", i, err)
		}
		did := idgen.MustNew(idgen.PrefixDeployment)
		if _, err := h.st.RawDB().ExecContext(ctx, `
			INSERT INTO model_deployments (id, org_id, connection_id, logical_model_id, upstream_model_name, pricing_nano_usd_json, state, connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms, version, created_at_ms, updated_at_ms)
			VALUES (?, ?, ?, ?, ?, '{"env_id":"","rate_card":{"input_token_nano_usd":1,"output_token_nano_usd":1}}', 'ACTIVE_HEALTHY', 5000, 30000, 15000, 1, ?, ?)`,
			did, boot.OrgID, cid, modID, fmt.Sprintf("up-%03d", i), ts, ts,
		); err != nil {
			t.Fatalf("insert dep %d: %v", i, err)
		}
		bid := idgen.MustNew(idgen.PrefixBudget)
		// budget_ledgers has UNIQUE(org_id, scope_type, scope_id) ONLY for period? Let's use distinct VIRTUAL_KEY scope IDs in each project!
		vkID := fmt.Sprintf("vk_pag_%03d", i)
		vkHash := sha256.Sum256([]byte(vkID))
		if _, err := h.st.RawDB().ExecContext(ctx, `
			INSERT INTO virtual_keys (id, org_id, project_id, env_id, name, key_prefix, last_four, active_key_hash, allowed_models_json, allowed_mcp_tools_json, state, version, created_at_ms, updated_at_ms)
			VALUES (?, ?, ?, ?, ?, 'vk_tp_pag', '0000', ?, '[]', '[]', 'CREATED_ACTIVE', 1, ?, ?)`,
			vkID, boot.OrgID, pid, boot.EnvID, vkID, vkHash[:], ts, ts,
		); err != nil {
			t.Fatalf("insert vkey %d: %v", i, err)
		}
		if _, err := h.st.RawDB().ExecContext(ctx, `
			INSERT INTO budget_ledgers (id, org_id, scope_type, scope_id, period_kind, period_start_ms, period_end_ms, hard_limit_micros, soft_alert_thresholds_pct_json, version, created_at_ms, updated_at_ms)
			VALUES (?, ?, 'VIRTUAL_KEY', ?, 'MONTHLY', ?, ?, 10000, '[80]', 1, ?, ?)`,
			bid, boot.OrgID, vkID, now, now+86400_000, ts, ts,
		); err != nil {
			t.Fatalf("insert budget %d: %v", i, err)
		}
	}
	_ = connAFirst

	teamATok := "tok_pag_team_a"
	h.registerBearerToken(teamATok, &rbac.Actor{
		PrincipalID:     "pri_pag_team_a",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleTeamAdmin}},
	})

	for _, ep := range []string{"/admin/v1/connections", "/admin/v1/deployments", "/admin/v1/budgets"} {
		collected := 0
		cursor := ""
		pages := 0
		for {
			path := ep + "?limit=2"
			if cursor != "" {
				path += "&cursor=" + url.QueryEscape(cursor)
			}
			rec := h.doBearer(t, http.MethodGet, path, teamATok, "", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s code=%d body=%s", path, rec.Code, rec.Body.String())
			}
			var page struct {
				Data       []json.RawMessage `json:"data"`
				NextCursor *string           `json:"next_cursor"`
				HasMore    bool              `json:"has_more"`
			}
			mustDecodeJSON(t, rec.Body.Bytes(), &page)
			collected += len(page.Data)
			pages++
			if !page.HasMore || page.NextCursor == nil {
				break
			}
			cursor = *page.NextCursor
			if pages > 10 {
				t.Fatalf("pagination loop exceeded expected pages on %s", ep)
			}
		}
		if collected != visibleCnt || pages != 2 {
			t.Fatalf("GET %s expected %d visible items across 2 pages past %d skipped rows, got %d across %d pages", ep, visibleCnt, hiddenCnt, collected, pages)
		}
	}
}

func TestPart2AuditPartitionAlertIsolationKillSwitchAndLimiterBounds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "ops-org-slug", "proj-a")
	now := h.fk.Now().UTC().UnixMilli()

	// 1. Audit partition auth: SYSTEM allowed only for INSTANCE OPERATOR; unknown partition 400.
	if err := h.auditLog.Append(ctx, port.AuditEvent{
		OrgID:      nil,
		ActorKind:  "SYSTEM",
		ActorID:    "sys_actor",
		EntryPoint: port.AuditEntryPointAdminAPI,
		EventType:  "system.boot_verified",
		TargetType: "instance",
		TargetID:   "instance",
	}); err != nil {
		t.Fatalf("Append SYSTEM event: %v", err)
	}
	orgOwnerOnlyTok := "tok_ops_org_owner_only"
	h.registerBearerToken(orgOwnerOnlyTok, &rbac.Actor{
		PrincipalID:     "pri_ops_org_owner_only",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleOrgOwner}},
	})
	recSysDenied := h.doBearer(t, http.MethodGet, "/admin/v1/audit-events?chain_partition=SYSTEM", orgOwnerOnlyTok, "", nil)
	if recSysDenied.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for ORG_OWNER querying SYSTEM audit partition, got %d body=%s", recSysDenied.Code, recSysDenied.Body.String())
	}
	recVerifyDenied := h.doBearer(t, http.MethodPost, "/admin/v1/audit-events/verify?chain_partition=SYSTEM", orgOwnerOnlyTok, "", nil)
	if recVerifyDenied.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for ORG_OWNER verifying SYSTEM audit partition, got %d body=%s", recVerifyDenied.Code, recVerifyDenied.Body.String())
	}
	recInvalidPart := h.doBearer(t, http.MethodGet, "/admin/v1/audit-events?chain_partition=org_other", orgOwnerOnlyTok, "", nil)
	if recInvalidPart.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid chain_partition, got %d body=%s", recInvalidPart.Code, recInvalidPart.Body.String())
	}

	opTok := "tok_ops_sys_aud"
	h.registerBearerToken(opTok, &rbac.Actor{
		PrincipalID:     boot.OwnerPrincipalID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	})
	recSysList := h.doBearer(t, http.MethodGet, "/admin/v1/audit-events?org_id="+boot.OrgID+"&chain_partition=SYSTEM", opTok, "", nil)
	if recSysList.Code != http.StatusOK {
		t.Fatalf("expected 200 for OPERATOR SYSTEM audit list, got %d body=%s", recSysList.Code, recSysList.Body.String())
	}
	recSysVer := h.doBearer(t, http.MethodPost, "/admin/v1/audit-events/verify?org_id="+boot.OrgID+"&chain_partition=SYSTEM", opTok, "", nil)
	if recSysVer.Code != http.StatusOK {
		t.Fatalf("expected 200 for OPERATOR SYSTEM audit verify, got %d body=%s", recSysVer.Code, recSysVer.Body.String())
	}
	var sysVer verifyAuditResponseDTO
	mustDecodeJSON(t, recSysVer.Body.Bytes(), &sysVer)
	if !sysVer.Intact || sysVer.CheckedEventsCount < 1 {
		t.Fatalf("expected intact SYSTEM audit chain, got %+v", sysVer)
	}

	// 2. Project-scoped alert isolation (proj-a FINOPS_VIEWER must NOT see proj-b alerts).
	recPB := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{"slug": "proj-b", "name": "Proj B"})
	if recPB.Code != http.StatusCreated {
		t.Fatalf("create proj-b: %d %s", recPB.Code, recPB.Body.String())
	}
	var projB projectDTO
	mustDecodeJSON(t, recPB.Body.Bytes(), &projB)

	bdgB := idgen.MustNew(idgen.PrefixBudget)
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO budget_ledgers (id, org_id, scope_type, scope_id, period_kind, period_start_ms, period_end_ms, hard_limit_micros, settled_exact_micros, soft_alert_thresholds_pct_json, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'PROJECT', ?, 'MONTHLY', ?, ?, 1000, 950, '[80]', 1, ?, ?)`,
		bdgB, boot.OrgID, projB.ID, now, now+86400_000, now, now,
	); err != nil {
		t.Fatalf("insert bdgB: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO usage_settlements (id, settled_at_ms, request_id, org_id, project_id, env_id, virtual_key_id, plane, logical_model_slug, billing_mode, usage_accuracy, total_cost_micros)
		VALUES ('stl_iso_b', ?, 'req_iso_b', ?, ?, ?, 'vk_b', 'PROVIDER', 'gpt', 'COMMERCIAL_API', 'UNMETERED_FLAGGED', 0)`,
		now, boot.OrgID, projB.ID, boot.EnvID,
	); err != nil {
		t.Fatalf("insert stl_iso_b: %v", err)
	}

	// Quarantined extension used ONLY by projB connection.
	adpB := seedLocalOpenAIAdapter(t, h, "127.0.0.1")
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE extension_installations SET state = 'QUARANTINED' WHERE id = (SELECT installation_id FROM provider_adapters WHERE id = ?)`, adpB); err != nil {
		t.Fatalf("quarantine adpB extension: %v", err)
	}
	connB := idgen.MustNew(idgen.PrefixConnection)
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO provider_connections (id, org_id, owner_project_id, shared_project_ids_json, adapter_id, name, endpoint_base_url, allow_private_ip, data_region, billing_mode, state, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, '[]', ?, 'conn-b-quar', 'https://api.openai.com/v1', 0, 'US', 'COMMERCIAL_API', 'ACTIVE', 1, ?, ?)`,
		connB, boot.OrgID, projB.ID, adpB, now, now,
	); err != nil {
		t.Fatalf("insert connB: %v", err)
	}

	finProjATok := "tok_fin_proj_a"
	h.registerBearerToken(finProjATok, &rbac.Actor{
		PrincipalID:     "pri_fin_proj_a",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleFinOpsViewer}},
	})
	recProjAAlerts := h.doBearer(t, http.MethodGet, "/admin/v1/alerts/active", finProjATok, "", nil)
	if recProjAAlerts.Code != http.StatusOK {
		t.Fatalf("GET /alerts/active projA code=%d body=%s", recProjAAlerts.Code, recProjAAlerts.Body.String())
	}
	var alertResp paginatedListEnvelope[alertItemDTO]
	mustDecodeJSON(t, recProjAAlerts.Body.Bytes(), &alertResp)
	if len(alertResp.Data) != 0 {
		t.Fatalf("expected 0 alerts visible to proj-a viewer when all alerts belong to proj-b, got %+v", alertResp.Data)
	}
	recOwnerAlerts := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/alerts/active", ownerCookie, "", "", nil)
	var ownerAlertResp paginatedListEnvelope[alertItemDTO]
	mustDecodeJSON(t, recOwnerAlerts.Body.Bytes(), &ownerAlertResp)
	if len(ownerAlertResp.Data) != 3 {
		t.Fatalf("expected 3 org-wide alerts for ORG_OWNER, got %d (%+v)", len(ownerAlertResp.Data), ownerAlertResp.Data)
	}

	// 3. Kill-switch mandatory confirm_slug & tenant-scoped SeveredStreamsCount.
	_, c1 := context.WithCancel(ctx)
	_, c2 := context.WithCancel(ctx)
	h.killSwitch.Streams().Register(killswitch.StreamRegistration{OrgID: boot.OrgID, ProjectID: boot.ProjectID, Cancel: c1})
	h.killSwitch.Streams().Register(killswitch.StreamRegistration{OrgID: "org_other_tenant", ProjectID: "prj_other", Cancel: c2})

	recBlankConfirm := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/kill-switches", ownerCookie, boot.CSRFToken, "", map[string]any{
		"target_level":         "PROJECT",
		"target_id":            boot.ProjectID,
		"confirm_slug":         "",
		"abort_active_streams": true,
		"reason":               "Missing confirm_slug check",
	})
	if recBlankConfirm.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 confirm_slug_mismatch for blank confirm_slug on PROJECT freeze, got %d body=%s", recBlankConfirm.Code, recBlankConfirm.Body.String())
	}

	recOrgBlankConfirm := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/kill-switches", ownerCookie, boot.CSRFToken, "", map[string]any{
		"target_level":         "PROJECT",
		"target_id":            boot.OrgID,
		"confirm_slug":         "wrong-slug",
		"abort_active_streams": true,
		"reason":               "Wrong org confirm_slug check",
	})
	if recOrgBlankConfirm.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 confirm_slug_mismatch on ORG freeze, got %d body=%s", recOrgBlankConfirm.Code, recOrgBlankConfirm.Body.String())
	}

	recOrgFreezeOK := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/kill-switches", ownerCookie, boot.CSRFToken, "", map[string]any{
		"target_level":         "PROJECT",
		"target_id":            boot.OrgID,
		"confirm_slug":         "ops-org-slug",
		"abort_active_streams": true,
		"reason":               "Emergency org freeze verified",
	})
	if recOrgFreezeOK.Code != http.StatusOK {
		t.Fatalf("expected 200 on ORG freeze with valid confirm_slug, got %d body=%s", recOrgFreezeOK.Code, recOrgFreezeOK.Body.String())
	}
	var ksOrgResp killSwitchTriggerResponseDTO
	mustDecodeJSON(t, recOrgFreezeOK.Body.Bytes(), &ksOrgResp)
	if ksOrgResp.SeveredStreamsCount != 1 {
		t.Fatalf("expected tenant-scoped severed_streams_count=1 (excluding other org stream), got %d", ksOrgResp.SeveredStreamsCount)
	}

	// 4. slidingWindowLimiter bounded buckets test.
	lim := newSlidingWindowLimiter(5, time.Minute)
	for i := 0; i < maxProbeLimiterBuckets+500; i++ {
		if i == maxProbeLimiterBuckets+10 {
			h.fk.Advance(2 * time.Minute)
		}
		_ = lim.Allow(fmt.Sprintf("conn_%05d", i), h.fk.Now().UTC().UnixMilli())
	}
	lim.mu.Lock()
	bucketLen := len(lim.buckets)
	lim.mu.Unlock()
	if bucketLen > maxProbeLimiterBuckets {
		t.Fatalf("expected limiter buckets <= %d, got %d", maxProbeLimiterBuckets, bucketLen)
	}
}

