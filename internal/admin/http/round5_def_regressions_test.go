// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/builtin"
	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/bootstrap"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

var round5PGSeq atomic.Int64

type pgAdminHarness struct {
	st        *postgres.Store
	srv       *Server
	tokenPath string
	fk        *clock.FakeClock
	rootPriv  ed25519.PrivateKey
	archives  map[string]signedTestArchive
	builtins  *builtin.Catalog
}

func newPGAdminHarness(t *testing.T) *pgAdminHarness {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if baseDSN == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_DSN required")
		}
		t.Log("TANPOPO_TEST_PG_DSN not set; skipping live Postgres assertions")
		return nil
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("tp_admin_pg_%d_%d", time.Now().UnixNano(), round5PGSeq.Add(1))
	baseDB, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open base pg: %v", err)
	}
	if _, err := baseDB.ExecContext(ctx, `CREATE SCHEMA `+schemaName); err != nil {
		_ = baseDB.Close()
		t.Fatalf("create schema %s: %v", schemaName, err)
	}
	_ = baseDB.Close()

	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse pg url: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()

	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	nowFn := func() int64 { return fk.Now().UTC().UnixMilli() }

	st, err := postgres.Open(ctx, postgres.Options{
		DSN:   u.String(),
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		cleanDB, cerr := sql.Open("pgx", baseDSN)
		if cerr == nil {
			_, _ = cleanDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`)
			_ = cleanDB.Close()
		}
	})

	dir := t.TempDir()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{
		DB:    st.RawDB(),
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}
	if err := st.SetAuditSink(auditLog); err != nil {
		t.Fatalf("st.SetAuditSink: %v", err)
	}
	masterKey := make([]byte, vault.KeySizeBytes)
	for i := range masterKey {
		masterKey[i] = byte(i + 41)
	}
	kek, err := vault.NewStaticKEKProvider("kek_pg_r5_v1", masterKey)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	vlt, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           kek,
		NowMs:         nowFn,
		PostShredHook: st.Ping,
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}

	rootPub, rootPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	keyID := installer.PublicKeyFingerprint(rootPub)
	keyring := trust.TrustedKeyring{
		RootPublicKeys: map[string]ed25519.PublicKey{keyID: rootPub},
	}
	builtinCat, err := builtin.NewCatalog()
	if err != nil {
		t.Fatalf("builtin.NewCatalog: %v", err)
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
		RequestMapping:    declarative.RequestMappingSpec{Method: http.MethodPost, PathTemplate: "/v1/chat/completions"},
		ResponseMapping:   declarative.ResponseMappingSpec{ModelPath: "model"},
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
	h := &pgAdminHarness{
		st:        st,
		tokenPath: tokenPath,
		fk:        fk,
		rootPriv:  rootPriv,
		archives:  make(map[string]signedTestArchive),
		builtins:  builtinCat,
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
		RouteResolver:           resolver.New(),
		Broker:                  brk,
		ProviderRuntime:         provRuntime,
		LockfilePath:            lockPath,
		ProbeRateLimitPerMinute: 20,
		ArchiveResolver: func(_ context.Context, _ string, sourceURI, expectedSHA256 string) (*installer.StageInstallRequest, func(), error) {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(sourceURI)), builtin.SourceScheme) {
				return h.builtins.ResolveStageRequest(sourceURI, expectedSHA256, func(payload []byte) ([]byte, error) {
					return ed25519.Sign(h.rootPriv, payload), nil
				})
			}
			entry, ok := h.archives[sourceURI]
			if !ok {
				return nil, nil, fmt.Errorf("%w: archive %q not registered", port.ErrNotFound, sourceURI)
			}
			return &installer.StageInstallRequest{
				ArchiveReader: bytes.NewReader(entry.raw),
				Source: installer.SourceSpec{
					Kind:           installer.SourceKindURL,
					URI:            sourceURI,
					ExpectedSHA256: expectedSHA256,
				},
				Signature: installer.ProvenanceSignature{
					PublisherOrRootSig: entry.sig,
				},
			}, func() {}, nil
		},
		Audit:            auditLog,
		NowMs:            nowFn,
		PaginationSecret: pagSecret,
		HostResolver: &countingResolver{
			fn: func(host string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			},
		},
		BlockedAdminPorts: []int{9090},
		Version:           "0.1.0-pg-r5",
	})
	if err != nil {
		t.Fatalf("NewServer PG: %v", err)
	}
	h.srv = srv
	return h
}

func (h *pgAdminHarness) doCookie(t *testing.T, method, path string, cookie *http.Cookie, csrf, ifMatch string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, "https://control.tanpopo.local"+path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
		req.Header.Set("Origin", "https://control.tanpopo.local")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, req)
	return rec
}

type pgBootResp struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	EnvID     string `json:"env_id"`
	CSRFToken string `json:"csrf_token"`
}

func (h *pgAdminHarness) bootstrap(t *testing.T) (pgBootResp, *http.Cookie) {
	t.Helper()
	tokBytes, err := os.ReadFile(h.tokenPath)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	rec := h.doCookie(t, http.MethodPost, "/admin/v1/bootstrap/complete", nil, "", "", map[string]any{
		"bootstrap_token":                string(bytes.TrimSpace(tokBytes)),
		"owner_email":                    "owner@pg-test.local",
		"owner_password":                 "StrongPassword#2026!",
		"org_name":                       "PG Ext Org",
		"project_name":                   "pg-ext-proj",
		"master_kek_backup_acknowledged": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap code=%d body=%s", rec.Code, rec.Body.String())
	}
	var out pgBootResp
	mustDecodeJSON(t, rec.Body.Bytes(), &out)
	cookie := findCookie(t, rec.Result().Cookies(), "__Host-tanpopo_admin_session")
	return out, cookie
}

func TestDEF081_PostgresExtensionActivationAndAdapterSync(t *testing.T) {
	h := newPGAdminHarness(t)
	if h == nil {
		return
	}
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
		t.Fatalf("missing builtin @tanpopo/ollama")
	}

	recStage := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      ollamaPkg.SourceURI,
		"expected_sha256": ollamaPkg.ArchiveSHA256,
	})
	if recStage.Code != http.StatusAccepted {
		t.Fatalf("stage ollama code=%d body=%s", recStage.Code, recStage.Body.String())
	}
	var stageResp struct {
		InstallationID string `json:"installation_id"`
	}
	mustDecodeJSON(t, recStage.Body.Bytes(), &stageResp)

	recApprove := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+stageResp.InstallationID+"/approve-permissions", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"enable_immediately": true,
		"ack_unverified":     false,
	})
	if recApprove.Code != http.StatusOK {
		syncErr := h.srv.syncExtensionProviderAdapters(context.Background(), stageResp.InstallationID)
		t.Fatalf("DEF-081 reproduced! approve code=%d body=%s direct_syncErr=%v", recApprove.Code, recApprove.Body.String(), syncErr)
	}
	recAdapters := h.doCookie(t, http.MethodGet, "/admin/v1/provider-adapters", ownerCookie, "", "", nil)
	if recAdapters.Code != http.StatusOK {
		t.Fatalf("list adapters code=%d body=%s", recAdapters.Code, recAdapters.Body.String())
	}
	if !strings.Contains(recAdapters.Body.String(), `"ollama"`) {
		t.Fatalf("expected ollama adapter registered, got %s", recAdapters.Body.String())
	}
}

func TestDEF081_PostgresRollbackAndStartupReconcile(t *testing.T) {
	h := newPGAdminHarness(t)
	if h == nil {
		return
	}
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
		t.Fatalf("missing builtin @tanpopo/ollama")
	}

	recStage := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      ollamaPkg.SourceURI,
		"expected_sha256": ollamaPkg.ArchiveSHA256,
	})
	if recStage.Code != http.StatusAccepted {
		t.Fatalf("stage code=%d body=%s", recStage.Code, recStage.Body.String())
	}
	var stageResp struct {
		InstallationID string `json:"installation_id"`
	}
	mustDecodeJSON(t, recStage.Body.Bytes(), &stageResp)

	// 1. Injected SyncAdaptersTx failure MUST roll back ApproveAndActivate cleanly (no stuck ENABLED_ACTIVE row).
	h.srv.installer.SetSyncAdaptersTx(func(context.Context, *sql.Tx, string, int64) error {
		return errors.New("injected pg adapter sync failure")
	})
	recFail := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+stageResp.InstallationID+"/approve-permissions", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"enable_immediately": true,
		"ack_unverified":     false,
	})
	if recFail.Code == http.StatusOK {
		t.Fatalf("expected injected adapter failure to fail approval, got 200")
	}
	recAfterFail, _, gerr := h.srv.loadExtensionInstallationDTO(context.Background(), stageResp.InstallationID)
	if gerr != nil {
		t.Fatalf("loadExtensionInstallationDTO: %v", gerr)
	}
	if recAfterFail.State != installer.StatePendingPermissionApproval {
		t.Fatalf("DEF-081 atomic rollback violated: state = %s, want PENDING_PERMISSION_APPROVAL", recAfterFail.State)
	}

	// 2. Restore real SyncAdaptersTx, activate cleanly, delete adapter row to simulate legacy crash state,
	//    then verify ReconcileMissingProviderAdapters recovers the missing adapter idempotently.
	h.srv.installer.SetSyncAdaptersTx(h.srv.catalog.SyncExtensionProviderAdaptersTx)
	recOK := h.doCookie(t, http.MethodPost, "/admin/v1/extensions/"+stageResp.InstallationID+"/approve-permissions", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"enable_immediately": true,
		"ack_unverified":     false,
	})
	if recOK.Code != http.StatusOK {
		t.Fatalf("approve after restore code=%d body=%s", recOK.Code, recOK.Body.String())
	}

	tx, err := h.st.RawDB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = port.SetOperatorRLSTx(context.Background(), tx, port.DialectPostgres)
	if _, derr := tx.ExecContext(context.Background(), `DELETE FROM provider_adapters`); derr != nil {
		_ = tx.Rollback()
		t.Fatalf("delete provider_adapters: %v", derr)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	reconciled, err := h.srv.catalog.ReconcileMissingProviderAdapters(context.Background())
	if err != nil || reconciled != 1 {
		t.Fatalf("ReconcileMissingProviderAdapters = (%d, %v), want (1, nil)", reconciled, err)
	}
	recAdapters := h.doCookie(t, http.MethodGet, "/admin/v1/provider-adapters", ownerCookie, "", "", nil)
	if recAdapters.Code != http.StatusOK || !strings.Contains(recAdapters.Body.String(), `"ollama"`) {
		t.Fatalf("expected reconciled ollama adapter, code=%d body=%s", recAdapters.Code, recAdapters.Body.String())
	}
}

func TestDEF075_ImmediateVKeyRotationFinalizationOnAdminReadAndBoundary(t *testing.T) {
	runVKeyExpirySuite := func(t *testing.T, do func(method, path string, cookie *http.Cookie, csrf, ifMatch string, body any) *httptest.ResponseRecorder, db *sql.DB, dialect port.Dialect, snaps *snapshot.Manager, clk *clock.FakeClock, orgID, projID, envID, csrf string, cookie *http.Cookie) {
		t.Helper()
		// 1. Create active key v1.
		recCreate := do(http.MethodPost, "/admin/v1/projects/"+projID+"/virtual-keys", cookie, csrf, "", map[string]any{
			"env_id":         envID,
			"name":           "def075-key",
			"allowed_planes": []string{"PROVIDER"},
			"allowed_models": []string{"gpt-4o-mini"},
		})
		if recCreate.Code != http.StatusCreated {
			t.Fatalf("create vkey code=%d body=%s", recCreate.Code, recCreate.Body.String())
		}
		var created struct {
			ID        string `json:"id"`
			SecretKey string `json:"secret_key"`
			Version   int64  `json:"version"`
		}
		mustDecodeJSON(t, recCreate.Body.Bytes(), &created)

		// 2. Rotate key with 300s grace -> v2 ROTATING_DUAL_VALID.
		recRot := do(http.MethodPost, "/admin/v1/virtual-keys/"+created.ID+"/rotate", cookie, csrf, `"v1"`, map[string]any{
			"grace_window_seconds": 300,
		})
		if recRot.Code != http.StatusOK {
			t.Fatalf("rotate vkey code=%d body=%s", recRot.Code, recRot.Body.String())
		}
		var rotated struct {
			ID        string `json:"id"`
			SecretKey string `json:"secret_key"`
			State     string `json:"state"`
			Version   int64  `json:"version"`
		}
		mustDecodeJSON(t, recRot.Body.Bytes(), &rotated)
		if rotated.State != "ROTATING_DUAL_VALID" || rotated.Version != 2 {
			t.Fatalf("expected ROTATING_DUAL_VALID v2, got %+v", rotated)
		}

		// 3. T+270s: both old and new secrets authenticate validly.
		clk.Advance(270 * time.Second)
		if err := snaps.Refresh(context.Background()); err != nil {
			t.Fatalf("snaps.Refresh T+270s: %v", err)
		}
		if _, err := snaps.AuthenticateKey(created.SecretKey, envID); err != nil {
			t.Fatalf("expected old key valid at T+270s, got %v", err)
		}
		if _, err := snaps.AuthenticateKey(rotated.SecretKey, envID); err != nil {
			t.Fatalf("expected new key valid at T+270s, got %v", err)
		}

		// 4. Advance past 300s grace (T+301s): old secret fails closed IMMEDIATELY and immediate GET /admin/v1/virtual-keys
		// returns CREATED_ACTIVE v3 with cleared grace_expires_at_ms and audited virtual_key.rotation_finalized WITHOUT waiting on a ticker!
		clk.Advance(31 * time.Second)
		if err := snaps.Refresh(context.Background()); err != nil {
			t.Fatalf("snaps.Refresh T+301s: %v", err)
		}
		if _, err := snaps.AuthenticateKey(created.SecretKey, envID); err == nil {
			t.Fatalf("expected old key rejected after T+301s grace, got nil")
		}
		if _, err := snaps.AuthenticateKey(rotated.SecretKey, envID); err != nil {
			t.Fatalf("expected successor key valid after T+301s grace, got %v", err)
		}

		recList := do(http.MethodGet, "/admin/v1/virtual-keys", cookie, "", "", nil)
		if recList.Code != http.StatusOK {
			t.Fatalf("list vkeys code=%d body=%s", recList.Code, recList.Body.String())
		}
		var listed struct {
			Data []struct {
				ID      string `json:"id"`
				State   string `json:"state"`
				Version int64  `json:"version"`
			} `json:"data"`
		}
		mustDecodeJSON(t, recList.Body.Bytes(), &listed)
		var foundFinal bool
		for _, item := range listed.Data {
			if item.ID == created.ID {
				foundFinal = true
				if item.State != "CREATED_ACTIVE" || item.Version != 3 {
					t.Fatalf("DEF-075 immediate list expected CREATED_ACTIVE v3, got %+v", item)
				}
			}
		}
		if !foundFinal {
			t.Fatalf("key %s missing from list", created.ID)
		}

		tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		_ = port.SetOrgRLSTx(context.Background(), tx, dialect, orgID)
		var grace sql.NullInt64
		var auditCnt int
		if err := tx.QueryRowContext(context.Background(), port.Rebind(dialect, `SELECT grace_expires_at_ms FROM virtual_keys WHERE org_id = ? AND id = ?`), orgID, created.ID).Scan(&grace); err != nil {
			t.Fatalf("check grace_expires_at_ms: %v", err)
		}
		if grace.Valid {
			t.Fatalf("expected grace_expires_at_ms cleared (NULL), got %d", grace.Int64)
		}
		if err := tx.QueryRowContext(context.Background(), port.Rebind(dialect, `SELECT COUNT(1) FROM audit_events WHERE chain_key = ? AND event_type = 'virtual_key.rotation_finalized' AND target_id = ?`), orgID, created.ID).Scan(&auditCnt); err != nil {
			t.Fatalf("count audit: %v", err)
		}
		if auditCnt != 1 {
			t.Fatalf("expected 1 virtual_key.rotation_finalized audit event, got %d", auditCnt)
		}
	}

	t.Run("SQLite", func(t *testing.T) {
		h := newAdminTestHarness(t)
		doReq := func(method, path string, cookie *http.Cookie, csrf, ifMatch string, body any) *httptest.ResponseRecorder {
			t.Helper()
			var buf bytes.Buffer
			if body != nil {
				if err := json.NewEncoder(&buf).Encode(body); err != nil {
					t.Fatal(err)
				}
			}
			req := httptest.NewRequest(method, "https://control.tanpopo.local"+path, &buf)
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			if cookie != nil {
				req.AddCookie(cookie)
				req.Header.Set("Origin", "https://control.tanpopo.local")
			}
			if csrf != "" {
				req.Header.Set("X-CSRF-Token", csrf)
			}
			if ifMatch != "" {
				req.Header.Set("If-Match", ifMatch)
			}
			rec := httptest.NewRecorder()
			h.srv.ServeHTTP(rec, req)
			return rec
		}
		tokBytes, err := os.ReadFile(h.tokenPath)
		if err != nil {
			t.Fatal(err)
		}
		recBoot := doReq(http.MethodPost, "/admin/v1/bootstrap/complete", nil, "", "", map[string]any{
			"bootstrap_token":                string(bytes.TrimSpace(tokBytes)),
			"owner_email":                    "owner@sqlite-test.local",
			"owner_password":                 "StrongPassword#2026!",
			"org_name":                       "SQLite VKey Org",
			"project_name":                   "sqlite-vkey-proj",
			"master_kek_backup_acknowledged": true,
		})
		if recBoot.Code != http.StatusCreated {
			t.Fatalf("bootstrap code=%d body=%s", recBoot.Code, recBoot.Body.String())
		}
		var boot pgBootResp
		mustDecodeJSON(t, recBoot.Body.Bytes(), &boot)
		ownerCookie := findCookie(t, recBoot.Result().Cookies(), "__Host-tanpopo_admin_session")
		runVKeyExpirySuite(t, doReq, h.st.RawDB(), port.DialectSQLite, h.snapshots, h.fk, boot.OrgID, boot.ProjectID, boot.EnvID, boot.CSRFToken, ownerCookie)
	})

	t.Run("Postgres16", func(t *testing.T) {
		h := newPGAdminHarness(t)
		if h == nil {
			return
		}
		boot, ownerCookie := h.bootstrap(t)
		runVKeyExpirySuite(t, func(method, path string, cookie *http.Cookie, csrf, ifMatch string, body any) *httptest.ResponseRecorder {
			return h.doCookie(t, method, path, cookie, csrf, ifMatch, body)
		}, h.st.RawDB(), port.DialectPostgres, h.srv.snapshots, h.fk, boot.OrgID, boot.ProjectID, boot.EnvID, boot.CSRFToken, ownerCookie)
	})
}

