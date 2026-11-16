// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package native_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/ingress/native"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type memAuditSink struct {
	mu   sync.Mutex
	evs  []storageport.AuditEvent
	raws *canary.SafeBuffer
}

func (a *memAuditSink) Append(_ context.Context, ev storageport.AuditEvent) error {
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.evs = append(a.evs, ev)
	a.mu.Unlock()
	_, _ = a.raws.Write(append(line, '\n'))
	return nil
}

func (a *memAuditSink) events() []storageport.AuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]storageport.AuditEvent, len(a.evs))
	copy(out, a.evs)
	return out
}

type recordedUpstreamReq struct {
	Method  string
	Path    string
	RawURI  string
	Headers http.Header
	Body    []byte
}

type spyUpstream struct {
	srv     *httptest.Server
	mu      sync.Mutex
	reqs    []recordedUpstreamReq
	hits    atomic.Int64
	handler func(w http.ResponseWriter, r *http.Request, body []byte)
}

func newSpyUpstream(t *testing.T, fn func(w http.ResponseWriter, r *http.Request, body []byte)) *spyUpstream {
	t.Helper()
	s := &spyUpstream{handler: fn}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.hits.Add(1)
		s.mu.Lock()
		s.reqs = append(s.reqs, recordedUpstreamReq{
			Method:  r.Method,
			Path:    r.URL.Path,
			RawURI:  r.RequestURI,
			Headers: r.Header.Clone(),
			Body:    append([]byte(nil), body...),
		})
		s.mu.Unlock()
		if s.handler != nil {
			s.handler(w, r, body)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *spyUpstream) lastReq() recordedUpstreamReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		return recordedUpstreamReq{}
	}
	return s.reqs[len(s.reqs)-1]
}

type nativeHarness struct {
	t         *testing.T
	ctx       context.Context
	tmp       string
	clk       *clock.FakeClock
	store     *sqlite.Store
	snapshots *snapshot.Manager
	catalog   *catalog.Repository
	vault     *vault.Store
	broker    *broker.BlindHostCredentialBroker
	installer *installer.LifecycleService
	vkeys     *vkey.Service
	audit     *memAuditSink
	org       *storageport.Organization
	project   *storageport.Project
	env       *storageport.Environment
	vkeyRaw   string
	vkeyID    string
	secret    string
	pubPriv   ed25519.PrivateKey
	usageMu   sync.Mutex
	usages    []native.UsageRecord
	srv       *ingresshttp.Server
	httpSrv   *httptest.Server
	client    *http.Client
}

func setupNativeHarness(t *testing.T, extraCfg func(*ingresshttp.ServerConfig)) *nativeHarness {
	t.Helper()

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000300000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }
	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(tmp, "tanpopo.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	audit := &memAuditSink{raws: canary.NewSafeBuffer()}
	pubPub, pubPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := trust.TrustedKeyring{
		PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": pubPub},
	}

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{
		DB:      st.RawDB(),
		NowMs:   nowFn,
		Keyring: keyring,
	})
	if err != nil {
		t.Fatal(err)
	}

	rawMaster := make([]byte, 32)
	if _, err := rand.Read(rawMaster); err != nil {
		t.Fatal(err)
	}
	kek, err := vault.NewStaticKEKProvider("kek-native-1", rawMaster)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kek.Close)

	vStore, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           kek,
		NowMs:         nowFn,
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vStore.Close)

	catRepo, err := catalog.NewRepository(catalog.Config{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Snapshots: mgr,
		Keyring:   keyring,
		Audit:     audit,
	})
	if err != nil {
		t.Fatal(err)
	}

	brk, err := broker.NewBlindBroker(broker.Config{
		Resolver:       catRepo,
		PolicyResolver: catRepo, // DEC-020 explicit PolicyResolver
		Vault:          vStore,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          audit,
		NowMs:          nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:         st.RawDB(),
		PluginsDir: pluginsDir,
		Keyring:    keyring,
		Audit:      audit,
		NowMs:      nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	vkSvc, err := vkey.NewService(vkey.ServiceConfig{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Snapshots: mgr,
		AuditSink: audit,
	})
	if err != nil {
		t.Fatal(err)
	}

	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{
		Slug:           "native-org",
		DisplayName:    "Native Org",
		MinPluginTrust: storageport.MinTrustVerifiedOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{
		Slug:        "native-proj",
		DisplayName: "Native Proj",
	})
	if err != nil {
		t.Fatal(err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, storageport.CreateEnvironmentParams{
		Slug: "prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ('prc_op_1', NULL, 'HUMAN_LOCAL', 'op@acme.test', 'ACTIVE', 1, ?, ?)
	`, nowFn(), nowFn()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_op_1', NULL, 'prc_op_1', 'INSTANCE', 'instance', 'OPERATOR', 1, ?)
	`, nowFn()); err != nil {
		t.Fatal(err)
	}

	createdVK, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID:                org.ID,
		ProjectID:            proj.ID,
		EnvID:                env.ID,
		Name:                 "native-live-vk",
		AllowedPlanes:        "PROVIDER",
		MaxConcurrentStreams: 16,
	})
	if err != nil {
		t.Fatal(err)
	}

	canarySec := canary.DefaultCanarySecret()
	h := &nativeHarness{
		t:         t,
		ctx:       ctx,
		tmp:       tmp,
		clk:       clk,
		store:     st,
		snapshots: mgr,
		catalog:   catRepo,
		vault:     vStore,
		broker:    brk,
		installer: instSvc,
		vkeys:     vkSvc,
		audit:     audit,
		org:       org,
		project:   proj,
		env:       env,
		vkeyRaw:   createdVK.SecretKey,
		vkeyID:    createdVK.Summary.ID,
		secret:    canarySec,
		pubPriv:   pubPriv,
		client:    netguard.NewHTTPClient(),
	}

	cfg := ingresshttp.ServerConfig{
		Snapshots:            mgr,
		Broker:               brk,
		PluginsDir:           pluginsDir,
		Audit:                audit,
		NowMs:                nowFn,
		IdempotencyDB:        st.RawDB(),
		SSEHeartbeatInterval: 200 * time.Millisecond,
		NativeUsageObserver: func(rec native.UsageRecord) {
			h.usageMu.Lock()
			h.usages = append(h.usages, rec)
			h.usageMu.Unlock()
		},
	}
	if extraCfg != nil {
		extraCfg(&cfg)
	}

	srv, err := ingresshttp.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.srv = srv
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	h.httpSrv = httptest.NewServer(srv)
	t.Cleanup(h.httpSrv.Close)
	t.Cleanup(h.client.CloseIdleConnections)
	return h
}

func (h *nativeHarness) lastUsage() native.UsageRecord {
	h.usageMu.Lock()
	defer h.usageMu.Unlock()
	if len(h.usages) == 0 {
		return native.UsageRecord{}
	}
	return h.usages[len(h.usages)-1]
}

func (h *nativeHarness) registerAdapterDeployment(
	id string,
	slug string,
	dialect string,
	modelSlug string,
	upstreamModel string,
	baseURL string,
	manifestYAML string,
	wasmBytes []byte,
) {
	h.t.Helper()
	extDir := filepath.Join(h.tmp, "ext-"+slug)
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, "manifest.yaml"), []byte(manifestYAML), 0o600); err != nil {
		h.t.Fatal(err)
	}
	if len(wasmBytes) > 0 {
		distDir := filepath.Join(extDir, "dist")
		if err := os.MkdirAll(distDir, 0o755); err != nil {
			h.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(distDir, "adapter.wasm"), wasmBytes, 0o600); err != nil {
			h.t.Fatal(err)
		}
	}

	tpkgPath := filepath.Join(h.tmp, slug+"-1.0.0.tpkg")
	packRes, err := testharness.PackExtension(h.ctx, extDir, tpkgPath)
	if err != nil {
		h.t.Fatalf("PackExtension %s: %v", id, err)
	}
	sig := ed25519.Sign(h.pubPriv, installer.SignatureSigningPayload(id, "1.0.0", packRes.SHA256))
	staged, err := h.installer.StageFreshInstall(h.ctx, installer.StageInstallRequest{
		ArchivePath: tpkgPath,
		Source: installer.SourceSpec{
			Kind:           installer.SourceKindLocalArchive,
			URI:            "./" + slug + "-1.0.0.tpkg",
			ExpectedSHA256: packRes.SHA256,
		},
		Signature:   installer.ProvenanceSignature{PublisherOrRootSig: sig},
		ActorID:     "prc_op_1",
		RequestID:   "req_stage_" + slug,
		TargetOrgID: h.org.ID,
	})
	if err != nil {
		h.t.Fatalf("StageFreshInstall %s: %v", id, err)
	}
	rec, err := h.installer.ApproveAndActivate(h.ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: "prc_op_1",
		ActivateImmediately: true,
		OrgID:               h.org.ID,
		RequestID:           "req_approve_" + slug,
	})
	if err != nil {
		h.t.Fatalf("ApproveAndActivate %s: %v", id, err)
	}

	adp, err := h.catalog.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID:    rec.InstallationID,
		AdapterSlug:       slug,
		SupportedDialects: []string{dialect},
		AllowedHosts:      []string{"127.0.0.1"},
	})
	if err != nil {
		h.t.Fatalf("CreateProviderAdapter %s: %v", slug, err)
	}

	secMeta, err := h.vault.SealSecret(h.ctx, vault.SealSecretParams{
		OrgID:      h.org.ID,
		Name:       "sec-" + slug,
		SecretKind: vault.SecretKindAPIKey,
		Plaintext:  []byte(h.secret),
	})
	if err != nil {
		h.t.Fatalf("SealSecret %s: %v", slug, err)
	}

	conn, err := h.catalog.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           h.org.ID,
		AdapterID:       adp.ID,
		Name:            "conn-" + slug,
		EndpointBaseURL: baseURL,
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{
			{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind},
		},
	})
	if err != nil {
		h.t.Fatalf("CreateProviderConnection %s: %v", slug, err)
	}
	conn, err = h.catalog.TransitionConnectionState(h.ctx, h.org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "validate")
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.catalog.TransitionConnectionState(h.ctx, h.org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "activate"); err != nil {
		h.t.Fatal(err)
	}

	lmod, err := h.catalog.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		OrgID:           &h.org.ID,
		CanonicalSlug:   modelSlug,
		ModalityKind:    catalog.ModalityChat,
		ContextWindow:   128000,
		MaxOutputTokens: 8192,
	})
	if err != nil {
		h.t.Fatalf("CreateLogicalModel %s: %v", modelSlug, err)
	}

	if _, err := h.catalog.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID:              h.org.ID,
		ConnectionID:       conn.ID,
		LogicalModelID:     lmod.ID,
		UpstreamModelName:  upstreamModel,
		AssuranceLevel:     catalog.AssuranceTested,
		State:              catalog.DeployStateActiveHealthy,
		ConnectTimeoutMs:   5000,
		FirstByteTimeoutMs: 5000,
		IdleChunkTimeoutMs: 5000,
	}); err != nil {
		h.t.Fatalf("CreateModelDeployment %s: %v", modelSlug, err)
	}

	if err := h.snapshots.Refresh(h.ctx); err != nil {
		h.t.Fatal(err)
	}
}

// --- Deterministic Wasm builder for Tier-2 Anthropic Native Passthrough fixture ---

func uleb(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func sleb64(v int64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		more := !((v == 0 && (b&0x40) == 0) || (v == -1 && (b&0x40) != 0))
		if more {
			b |= 0x80
		}
		out = append(out, b)
		if !more {
			return out
		}
	}
}

func wSection(id byte, payload []byte) []byte {
	var out []byte
	out = append(out, id)
	out = append(out, uleb(uint32(len(payload)))...)
	out = append(out, payload...)
	return out
}

func wString(s string) []byte {
	var out []byte
	out = append(out, uleb(uint32(len(s)))...)
	out = append(out, s...)
	return out
}

// buildAnthropicNativeTestWasm compiles a self-contained Tier-2 Wasm guest exporting:
// - memory (2 pages initial)
// - alloc(size) -> ptr
// - inspect_native_request(ptr, len) -> i64 returning NativeRoutingHints with credential injection for x-api-key
// - extract_usage(ptr, len) -> i64 matching message_start / message_delta / sync usage markers
// - normalize_error(ptr, len) -> i64 returning canonical upstream error envelope
func buildAnthropicNativeTestWasm() []byte {
	type dataSeg struct {
		offset uint32
		data   []byte
	}
	const (
		needleStreamOff uint32 = 8192
		needleStartOff  uint32 = 8256
		needleDeltaOff  uint32 = 8320

		hintsSyncOff   uint32 = 8448
		hintsStreamOff uint32 = 8960

		usageSyncOff  uint32 = 9600
		usageStartOff uint32 = 9792
		usageDeltaOff uint32 = 9984
		normErrOff    uint32 = 10240
	)

	needleStream := []byte(`"stream":true`)
	needleStart := []byte(`"type":"message_start"`)
	needleDelta := []byte(`"type":"message_delta"`)

	hintsSync := []byte(`{"target_model":"claude-sonnet-4-5","stream":false,"credential_injections":[{"secret_slot_name":"api_key","target_location":"CREDENTIAL_TARGET_LOCATION_HEADER","field_name":"x-api-key","value_prefix":""}]}`)
	hintsStream := []byte(`{"target_model":"claude-sonnet-4-5","stream":true,"credential_injections":[{"secret_slot_name":"api_key","target_location":"CREDENTIAL_TARGET_LOCATION_HEADER","field_name":"x-api-key","value_prefix":""}]}`)

	usageSync := []byte(`{"input_tokens":120,"output_tokens":55,"cache_read_tokens":40,"accuracy":"USAGE_ACCURACY_EXACT"}`)
	usageStart := []byte(`{"input_tokens":120,"output_tokens":1,"cache_read_tokens":40,"accuracy":"USAGE_ACCURACY_EXACT"}`)
	usageDelta := []byte(`{"input_tokens":0,"output_tokens":55,"accuracy":"USAGE_ACCURACY_EXACT"}`)
	normErr := []byte(`{"canonical_code":"upstream_overloaded","error_type":"upstream_error","retryable_pre_stream":true,"sanitized_public_message":"Anthropic upstream failed."}`)

	segs := []dataSeg{
		{offset: needleStreamOff, data: needleStream},
		{offset: needleStartOff, data: needleStart},
		{offset: needleDeltaOff, data: needleDelta},
		{offset: hintsSyncOff, data: hintsSync},
		{offset: hintsStreamOff, data: hintsStream},
		{offset: usageSyncOff, data: usageSync},
		{offset: usageStartOff, data: usageStart},
		{offset: usageDeltaOff, data: usageDelta},
		{offset: normErrOff, data: normErr},
	}

	packPtrLen := func(ptr uint32, length int) []byte {
		var b []byte
		b = append(b, 0x41)
		b = append(b, sleb64(int64(ptr))...)
		b = append(b, 0xad) // i64.extend_i32_u
		b = append(b, 0x42, 0x20, 0x86) // i64.shl 32
		b = append(b, 0x41)
		b = append(b, sleb64(int64(length))...)
		b = append(b, 0xad, 0x84) // i64.or
		return b
	}

	// Emits code to check if mem[needleOff:needleOff+needleLen] is in mem[inPtr:inPtr+inLen], setting matchL=1 or 0.
	emitHasSubstr := func(b []byte, inPtrL, inLenL, needleOff uint32, needleLen uint32, matchL, iL, kL uint32) []byte {
		b = append(b, 0x41, 0x00, 0x21)
		b = append(b, uleb(matchL)...) // matchL = 0
		b = append(b, 0x20)
		b = append(b, uleb(inLenL)...)
		b = append(b, 0x41)
		b = append(b, sleb64(int64(needleLen))...)
		b = append(b, 0x4f) // >=
		b = append(b, 0x04, 0x40)
		b = append(b, 0x41, 0x00, 0x21)
		b = append(b, uleb(iL)...)
		b = append(b, 0x02, 0x40) // block outer
		b = append(b, 0x03, 0x40) // loop i
		b = append(b, 0x20)
		b = append(b, uleb(iL)...)
		b = append(b, 0x20)
		b = append(b, uleb(inLenL)...)
		b = append(b, 0x41)
		b = append(b, sleb64(int64(needleLen))...)
		b = append(b, 0x6b, 0x4b, 0x0d, 0x01) // i > inLen-needleLen -> break
		b = append(b, 0x41, 0x00, 0x21)
		b = append(b, uleb(kL)...)
		b = append(b, 0x02, 0x40) // block inner
		b = append(b, 0x03, 0x40) // loop k
		b = append(b, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x41)
		b = append(b, sleb64(int64(needleLen))...)
		b = append(b, 0x4f, 0x04, 0x40) // k >= needleLen -> found!
		b = append(b, 0x41, 0x01, 0x21)
		b = append(b, uleb(matchL)...)
		b = append(b, 0x0c, 0x04, 0x0b)
		b = append(b, 0x20)
		b = append(b, uleb(inPtrL)...)
		b = append(b, 0x20)
		b = append(b, uleb(iL)...)
		b = append(b, 0x6a)
		b = append(b, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x6a, 0x2d, 0x00, 0x00)
		b = append(b, 0x41)
		b = append(b, sleb64(int64(needleOff))...)
		b = append(b, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x6a, 0x2d, 0x00, 0x00)
		b = append(b, 0x47, 0x0d, 0x01)
		b = append(b, 0x20)
		b = append(b, uleb(kL)...)
		b = append(b, 0x41, 0x01, 0x6a, 0x21)
		b = append(b, uleb(kL)...)
		b = append(b, 0x0c, 0x00, 0x0b, 0x0b)
		b = append(b, 0x20)
		b = append(b, uleb(iL)...)
		b = append(b, 0x41, 0x01, 0x6a, 0x21)
		b = append(b, uleb(iL)...)
		b = append(b, 0x0c, 0x00, 0x0b, 0x0b, 0x0b)
		return b
	}

	// Func 0: alloc(i32) -> i32 (bump global 0)
	var allocBody []byte
	allocBody = append(allocBody, 0x01, 0x01, 0x7f) // local 1: prev
	allocBody = append(allocBody, 0x23, 0x00, 0x21, 0x01)
	allocBody = append(allocBody, 0x20, 0x01, 0x20, 0x00, 0x6a, 0x24, 0x00)
	allocBody = append(allocBody, 0x20, 0x01, 0x0b)

	// Func 1: inspect_native_request(ptr, len) -> i64
	var inspBody []byte
	inspBody = append(inspBody, 0x01, 0x03, 0x7f) // 2:match, 3:i, 4:k
	inspBody = emitHasSubstr(inspBody, 0, 1, needleStreamOff, uint32(len(needleStream)), 2, 3, 4)
	inspBody = append(inspBody, 0x20, 0x02, 0x04, 0x7e) // if match -> i64
	inspBody = append(inspBody, packPtrLen(hintsStreamOff, len(hintsStream))...)
	inspBody = append(inspBody, 0x05)
	inspBody = append(inspBody, packPtrLen(hintsSyncOff, len(hintsSync))...)
	inspBody = append(inspBody, 0x0b, 0x0b)

	// Func 2: extract_usage(ptr, len) -> i64
	var usageBody []byte
	usageBody = append(usageBody, 0x01, 0x03, 0x7f) // 2:match, 3:i, 4:k
	usageBody = emitHasSubstr(usageBody, 0, 1, needleStartOff, uint32(len(needleStart)), 2, 3, 4)
	usageBody = append(usageBody, 0x20, 0x02, 0x04, 0x7e)
	usageBody = append(usageBody, packPtrLen(usageStartOff, len(usageStart))...)
	usageBody = append(usageBody, 0x05)
	usageBody = emitHasSubstr(usageBody, 0, 1, needleDeltaOff, uint32(len(needleDelta)), 2, 3, 4)
	usageBody = append(usageBody, 0x20, 0x02, 0x04, 0x7e)
	usageBody = append(usageBody, packPtrLen(usageDeltaOff, len(usageDelta))...)
	usageBody = append(usageBody, 0x05)
	usageBody = append(usageBody, packPtrLen(usageSyncOff, len(usageSync))...)
	usageBody = append(usageBody, 0x0b, 0x0b, 0x0b)

	// Func 3: normalize_error(ptr, len) -> i64
	var errBody []byte
	errBody = append(errBody, 0x00)
	errBody = append(errBody, packPtrLen(normErrOff, len(normErr))...)
	errBody = append(errBody, 0x0b)

	// Types: 0=(i32)->i32, 1=(i32,i32)->i64
	typeSec := []byte{
		0x02,
		0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e,
	}
	funcSec := []byte{0x04, 0x00, 0x01, 0x01, 0x01}
	memSec := []byte{0x01, 0x00, 0x02} // min 2 pages (128 KiB)
	globalSec := append([]byte{0x01, 0x7f, 0x01, 0x41}, sleb64(16384)...)
	globalSec = append(globalSec, 0x0b)

	var expSec []byte
	expSec = append(expSec, 0x05)
	expSec = append(expSec, wString("memory")...)
	expSec = append(expSec, 0x02, 0x00)
	expSec = append(expSec, wString("alloc")...)
	expSec = append(expSec, 0x00, 0x00)
	expSec = append(expSec, wString("inspect_native_request")...)
	expSec = append(expSec, 0x00, 0x01)
	expSec = append(expSec, wString("extract_usage")...)
	expSec = append(expSec, 0x00, 0x02)
	expSec = append(expSec, wString("normalize_error")...)
	expSec = append(expSec, 0x00, 0x03)

	var codeSec []byte
	codeSec = append(codeSec, 0x04)
	for _, body := range [][]byte{allocBody, inspBody, usageBody, errBody} {
		codeSec = append(codeSec, uleb(uint32(len(body)))...)
		codeSec = append(codeSec, body...)
	}

	var dataSec []byte
	dataSec = append(dataSec, uleb(uint32(len(segs)))...)
	for _, seg := range segs {
		dataSec = append(dataSec, 0x00, 0x41)
		dataSec = append(dataSec, sleb64(int64(seg.offset))...)
		dataSec = append(dataSec, 0x0b)
		dataSec = append(dataSec, uleb(uint32(len(seg.data)))...)
		dataSec = append(dataSec, seg.data...)
	}

	var mod []byte
	mod = append(mod, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	mod = append(mod, wSection(1, typeSec)...)
	mod = append(mod, wSection(3, funcSec)...)
	mod = append(mod, wSection(5, memSec)...)
	mod = append(mod, wSection(6, globalSec)...)
	mod = append(mod, wSection(7, expSec)...)
	mod = append(mod, wSection(10, codeSec)...)
	mod = append(mod, wSection(11, dataSec)...)
	return mod
}

func TestNativePassthroughAnthropicCT13SyncAndSSE(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	h := setupNativeHarness(t, nil)

	syncUpstreamResp := []byte(`{"id":"msg_01_native","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"thinking","thinking":"step-by-step","signature":"sig_exact_999"},{"type":"text","text":"Native OK"}],"stop_reason":"end_turn","x_upstream_beta":{"preserved":true},"usage":{"input_tokens":120,"cache_read_input_tokens":40,"output_tokens":55}}`)
	sseEvents := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_sse_1\",\"usage\":{\"input_tokens\":120,\"cache_read_input_tokens\":40,\"output_tokens\":1}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"pondering\",\"signature\":\"sig_chunk_2\"}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":55}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}

	up := newSpyUpstream(t, func(w http.ResponseWriter, _ *http.Request, body []byte) {
		if bytes.Contains(body, []byte(`"stream":true`)) {
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			rc := http.NewResponseController(w)
			for _, ev := range sseEvents {
				_, _ = w.Write([]byte(ev))
				_ = rc.Flush()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(syncUpstreamResp)
	})

	wasmBytes := buildAnthropicNativeTestWasm()
	wasmSum := sha256.Sum256(wasmBytes)
	wasmHex := hex.EncodeToString(wasmSum[:])

	manifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/anthropic-native"
  name: "Anthropic Native Wasm"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "sandboxed_wasm"
  entrypoint: "dist/adapter.wasm"
  sha256: "` + wasmHex + `"
  quotas:
    maxMemoryMib: 16
    maxCpuMsPerCall: 2000
capabilities:
  - type: "provider_adapter"
    adapterSlug: "anthropic-native"
    supportedDialects: ["anthropic"]
    allowedNativePaths:
      - "^v1/messages(/count_tokens)?$"
    allowedNativeHeaders:
      - "anthropic-version"
      - "anthropic-beta"
    authMethods:
      - id: "api_key_hdr"
        mode: "host_blind_inject"
        headerName: "x-api-key"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["api_key_hdr"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
    - "provider:normalize_error"
`
	h.registerAdapterDeployment("@acme/anthropic-native", "anthropic-native", "anthropic", "claude-sonnet-4-5", "claude-sonnet-4-5", up.srv.URL, manifest, wasmBytes)

	// 1. Synchronous byte-preserving request + usage extraction.
	rawSyncReq := []byte(`{"model":"claude-sonnet-4-5",  "max_tokens":1024, "thinking":{"type":"enabled","budget_tokens":512,"signature":"sig_req_42"}, "messages":[{"role":"user","content":[{"type":"text","text":"Hi","cache_control":{"type":"ephemeral"}}]}], "x_future_beta_field":{"flag":true,"list":[3,2,1]}}`)
	req, err := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/anthropic/v1/messages", bytes.NewReader(rawSyncReq))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", h.vkeyRaw)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	req.Header.Set("X-Evil-Admin", "escalate-privilege")

	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on native sync, got %d: %s", resp.StatusCode, string(respBody))
	}
	if !bytes.Equal(respBody, syncUpstreamResp) {
		t.Fatalf("response mutated!\nwant: %s\ngot:  %s", string(syncUpstreamResp), string(respBody))
	}

	gotUp := up.lastReq()
	if gotUp.Path != "/v1/messages" {
		t.Fatalf("expected upstream path /v1/messages, got %q", gotUp.Path)
	}
	if !bytes.Equal(gotUp.Body, rawSyncReq) {
		t.Fatalf("request body mutated!\nwant: %s\ngot:  %s", string(rawSyncReq), string(gotUp.Body))
	}
	if gotUp.Headers.Get("x-api-key") != h.secret {
		t.Fatalf("expected BlindBroker secret %q in x-api-key, got %q", h.secret, gotUp.Headers.Get("x-api-key"))
	}
	if strings.Contains(gotUp.Headers.Get("x-api-key"), "vk_tp_") || gotUp.Headers.Get("Authorization") != "" {
		t.Fatalf("virtual key leaked in headers: %+v", gotUp.Headers)
	}
	if gotUp.Headers.Get("anthropic-beta") != "prompt-caching-2024-07-31" || gotUp.Headers.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("expected allowlisted anthropic headers forwarded, got %+v", gotUp.Headers)
	}
	if gotUp.Headers.Get("X-Evil-Admin") != "" {
		t.Fatalf("unapproved header X-Evil-Admin leaked upstream")
	}

	recUsage := h.lastUsage()
	if recUsage.Usage.InputTokens != 120 || recUsage.Usage.CacheReadTokens != 40 || recUsage.Usage.OutputTokens != 55 || recUsage.Usage.Accuracy != "USAGE_ACCURACY_EXACT" {
		t.Fatalf("unexpected sync usage record: %+v", recUsage)
	}

	// 2. SSE streaming byte-preserving relay + merged message_start/message_delta usage.
	rawSSEReq := []byte(`{"model":"claude-sonnet-4-5","stream":true,"max_tokens":512,"messages":[{"role":"user","content":"stream"}]}`)
	sseReq, err := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/anthropic/v1/messages", bytes.NewReader(rawSSEReq))
	if err != nil {
		t.Fatal(err)
	}
	sseReq.Header.Set("Content-Type", "application/json")
	sseReq.Header.Set("x-api-key", h.vkeyRaw)
	sseResp, err := h.client.Do(sseReq)
	if err != nil {
		t.Fatal(err)
	}
	sseBytes, _ := io.ReadAll(sseResp.Body)
	_ = sseResp.Body.Close()

	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on native SSE, got %d: %s", sseResp.StatusCode, string(sseBytes))
	}
	wantSSE := strings.Join(sseEvents, "")
	if string(sseBytes) != wantSSE {
		t.Fatalf("SSE stream mutated!\nwant:\n%s\ngot:\n%s", wantSSE, string(sseBytes))
	}

	sseUsage := h.lastUsage()
	if sseUsage.Usage.InputTokens != 120 || sseUsage.Usage.CacheReadTokens != 40 || sseUsage.Usage.OutputTokens != 55 || sseUsage.Usage.Accuracy != "USAGE_ACCURACY_EXACT" {
		t.Fatalf("expected merged multi-event Anthropic SSE usage (120/40/55), got %+v", sseUsage)
	}

	// Zero virtual keys or plaintext secrets anywhere in audit logs.
	canary.NewScanner(h.secret, h.vkeyRaw).AssertCleanBuffer(t, "audit", h.audit.raws)
}

func TestNativePassthroughOpenAIAndGeminiDialects(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	h := setupNativeHarness(t, nil)

	openAISyncResp := []byte(`{"id":"resp_native_1","object":"response","status":"completed","model":"gpt-4o","output":[{"type":"message","content":[{"type":"output_text","text":"Responses exact"}]}],"x_future_resp_field":99,"usage":{"input_tokens":33,"output_tokens":14}}`)
	geminiSSEPayload := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Gemini ok\"}]}}],\"usageMetadata\":{\"promptTokenCount\":21,\"candidatesTokenCount\":9,\"cachedContentTokenCount\":5}}\n\n"

	up := newSpyUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if strings.HasPrefix(r.URL.Path, "/v1beta/models/") {
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(geminiSSEPayload))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openAISyncResp)
	})

	openAIManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/openai-native"
  name: "OpenAI Native"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "openai-native"
    supportedDialects: ["openai"]
    allowedNativePaths:
      - "^v1/(chat/completions|responses)$"
    allowedNativeHeaders:
      - "OpenAI-Beta"
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["bearer"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
adapter:
  slug: "openai-native"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	geminiManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/gemini-native"
  name: "Gemini Native"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "gemini-native"
    supportedDialects: ["gemini"]
    allowedNativePaths:
      - "^v1beta/models/[A-Za-z0-9._-]+:(generateContent|streamGenerateContent)$"
    authMethods:
      - id: "goog_key"
        mode: "host_blind_inject"
        headerName: "x-goog-api-key"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["goog_key"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
adapter:
  slug: "gemini-native"
  auth:
    - id: "goog_key"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "x-goog-api-key"
  usageExtraction:
    inputTokensPath: "usageMetadata.promptTokenCount"
    outputTokensPath: "usageMetadata.candidatesTokenCount"
    cacheReadTokensPath: "usageMetadata.cachedContentTokenCount"
`

	h.registerAdapterDeployment("@acme/openai-native", "openai-native", "openai", "gpt-4o", "gpt-4o", up.srv.URL, openAIManifest, nil)
	h.registerAdapterDeployment("@acme/gemini-native", "gemini-native", "gemini", "gemini-2.5-flash", "gemini-2.5-flash", up.srv.URL, geminiManifest, nil)

	// OpenAI /native/openai/v1/responses sync
	oaiBody := []byte(`{"model":"gpt-4o","input":"ping","x_custom_opt":"keep-me"}`)
	oaiReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/responses", bytes.NewReader(oaiBody))
	oaiReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	oaiReq.Header.Set("OpenAI-Beta", "assistants=v2")
	oaiResp, err := h.client.Do(oaiReq)
	if err != nil {
		t.Fatal(err)
	}
	oaiRespBytes, _ := io.ReadAll(oaiResp.Body)
	_ = oaiResp.Body.Close()
	if oaiResp.StatusCode != http.StatusOK || !bytes.Equal(oaiRespBytes, openAISyncResp) {
		t.Fatalf("unexpected OpenAI responses result (%d): %s", oaiResp.StatusCode, string(oaiRespBytes))
	}
	if u := h.lastUsage(); u.Usage.InputTokens != 33 || u.Usage.OutputTokens != 14 {
		t.Fatalf("expected OpenAI Responses usage 33/14, got %+v", u)
	}
	if up.lastReq().Headers.Get("Authorization") != "Bearer "+h.secret {
		t.Fatalf("expected BlindBroker Bearer header, got %q", up.lastReq().Headers.Get("Authorization"))
	}

	// Gemini :streamGenerateContent?alt=sse (no "stream": true in JSON, model parsed from URL path)
	gemBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	gemReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/gemini/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse", bytes.NewReader(gemBody))
	gemReq.Header.Set("x-goog-api-key", h.vkeyRaw)
	gemResp, err := h.client.Do(gemReq)
	if err != nil {
		t.Fatal(err)
	}
	gemRespBytes, _ := io.ReadAll(gemResp.Body)
	_ = gemResp.Body.Close()
	if gemResp.StatusCode != http.StatusOK || string(gemRespBytes) != geminiSSEPayload {
		t.Fatalf("unexpected Gemini SSE result (%d): %s", gemResp.StatusCode, string(gemRespBytes))
	}
	gotGem := up.lastReq()
	if gotGem.RawURI != "/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse" {
		t.Fatalf("expected clean Gemini SSE URI, got %q", gotGem.RawURI)
	}
	if gotGem.Headers.Get("x-goog-api-key") != h.secret || strings.Contains(gotGem.Headers.Get("x-goog-api-key"), "vk_tp_") {
		t.Fatalf("expected upstream x-goog-api-key injected without vk_tp_ leak, got %q", gotGem.Headers.Get("x-goog-api-key"))
	}
	if u := h.lastUsage(); u.Usage.InputTokens != 21 || u.Usage.OutputTokens != 9 || u.Usage.CacheReadTokens != 5 {
		t.Fatalf("expected Gemini usage 21/9/5, got %+v", u)
	}

	// Negative query smuggling: ?key=smuggled -> 403 native_path_not_allowed, 0 upstream hits.
	hitsBefore := up.hits.Load()
	smugReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/gemini/v1beta/models/gemini-2.5-flash:generateContent?key=smuggled", bytes.NewReader(gemBody))
	smugReq.Header.Set("x-goog-api-key", h.vkeyRaw)
	smugResp, err := h.client.Do(smugReq)
	if err != nil {
		t.Fatal(err)
	}
	smugBytes, _ := io.ReadAll(smugResp.Body)
	_ = smugResp.Body.Close()
	if smugResp.StatusCode != http.StatusForbidden || !strings.Contains(string(smugBytes), "native_path_not_allowed") {
		t.Fatalf("expected 403 native_path_not_allowed for query smuggling, got %d: %s", smugResp.StatusCode, string(smugBytes))
	}
	if up.hits.Load() != hitsBefore {
		t.Fatalf("smuggled query hit upstream unexpectedly")
	}
}

func TestNativeNeg1AndNeg2TraversalDefaultDenyAndDialectMismatch(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	h := setupNativeHarness(t, nil)

	up := newSpyUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	openAIManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/openai-strict"
  name: "OpenAI Strict"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "openai-strict"
    supportedDialects: ["openai"]
    allowedNativePaths:
      - "^v1/chat/completions$"
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["bearer"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
adapter:
  slug: "openai-strict"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	// Second adapter intentionally omits allowedNativePaths to verify Default-Deny.
	defaultDenyManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/anthropic-deny"
  name: "Anthropic Default Deny"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "anthropic-deny"
    supportedDialects: ["anthropic"]
    authMethods:
      - id: "api_key_hdr"
        mode: "host_blind_inject"
        headerName: "x-api-key"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["api_key_hdr"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
adapter:
  slug: "anthropic-deny"
  auth:
    - id: "api_key_hdr"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "x-api-key"
`
	h.registerAdapterDeployment("@acme/openai-strict", "openai-strict", "openai", "gpt-4o", "gpt-4o", up.srv.URL, openAIManifest, nil)
	h.registerAdapterDeployment("@acme/anthropic-deny", "anthropic-deny", "anthropic", "claude-3-7", "claude-3-7", up.srv.URL, defaultDenyManifest, nil)

	// NEG-1: Dialect mismatch (/native/anthropic/v1/messages routing to an OpenAI deployment).
	misReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/anthropic/v1/messages", bytes.NewReader([]byte(`{"model":"gpt-4o"}`)))
	misReq.Header.Set("x-api-key", h.vkeyRaw)
	misResp, err := h.client.Do(misReq)
	if err != nil {
		t.Fatal(err)
	}
	misBytes, _ := io.ReadAll(misResp.Body)
	_ = misResp.Body.Close()
	if misResp.StatusCode != http.StatusBadRequest || !strings.Contains(string(misBytes), "dialect_mismatch") {
		t.Fatalf("expected 400 dialect_mismatch, got %d: %s", misResp.StatusCode, string(misBytes))
	}

	// NEG-1: Admin session cookie rejected on /native/* with 401 invalid_credential_plane.
	cookReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o"}`)))
	cookReq.AddCookie(&http.Cookie{Name: "tanpopo_session", Value: "sess_admin_secret"})
	cookResp, err := h.client.Do(cookReq)
	if err != nil {
		t.Fatal(err)
	}
	cookBytes, _ := io.ReadAll(cookResp.Body)
	_ = cookResp.Body.Close()
	if cookResp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(cookBytes), "invalid_credential_plane") {
		t.Fatalf("expected 401 invalid_credential_plane, got %d: %s", cookResp.StatusCode, string(cookBytes))
	}

	// NEG-2: Default-Deny when adapter omits allowedNativePaths.
	denyReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/anthropic/v1/messages", bytes.NewReader([]byte(`{"model":"claude-3-7"}`)))
	denyReq.Header.Set("x-api-key", h.vkeyRaw)
	denyResp, err := h.client.Do(denyReq)
	if err != nil {
		t.Fatal(err)
	}
	denyBytes, _ := io.ReadAll(denyResp.Body)
	_ = denyResp.Body.Close()
	if denyResp.StatusCode != http.StatusForbidden || !strings.Contains(string(denyBytes), "native_path_not_allowed") {
		t.Fatalf("expected 403 native_path_not_allowed on default-deny adapter, got %d: %s", denyResp.StatusCode, string(denyBytes))
	}

	// NEG-2: Non-allowlisted admin path even without '..'.
	admReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/organization/admin_api_keys", bytes.NewReader([]byte(`{"model":"gpt-4o"}`)))
	admReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	admResp, err := h.client.Do(admReq)
	if err != nil {
		t.Fatal(err)
	}
	admBytes, _ := io.ReadAll(admResp.Body)
	_ = admResp.Body.Close()
	if admResp.StatusCode != http.StatusForbidden || !strings.Contains(string(admBytes), "native_path_not_allowed") {
		t.Fatalf("expected 403 native_path_not_allowed on unapproved admin path, got %d: %s", admResp.StatusCode, string(admBytes))
	}

	// NEG-2: Raw TCP traversal tests so Go's HTTP client URL parser doesn't auto-normalize "..".
	addr := strings.TrimPrefix(h.httpSrv.URL, "http://")
	traversalPaths := []string{
		"/native/openai/v1/../organization/admin_api_keys",
		"/native/openai/v1/%2e%2e/organization/admin_api_keys",
		"/native/openai/v1/%252e%252e/organization/admin_api_keys",
		"/native/openai/v1/%25252e%25252e/organization/admin_api_keys",
		"/native/openai/v1/%20chat/completions",
		"/native/openai/v1/%5corganization/admin_api_keys",
		"/native/openai/v1//chat/completions",
	}
	for _, tp := range traversalPaths {
		dialCtx, cancelDial := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := netguard.NewDialer().DialContext(dialCtx, "tcp", addr)
		cancelDial()
		if err != nil {
			t.Fatal(err)
		}
		rawReq := "POST " + tp + " HTTP/1.1\r\nHost: " + addr + "\r\nAuthorization: Bearer " + h.vkeyRaw + "\r\nContent-Type: application/json\r\nContent-Length: 18\r\nConnection: close\r\n\r\n{\"model\":\"gpt-4o\"}"
		if _, err := conn.Write([]byte(rawReq)); err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		rawResp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		body, _ := io.ReadAll(rawResp.Body)
		_ = rawResp.Body.Close()
		_ = conn.Close()
		if rawResp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "native_path_not_allowed") {
			t.Fatalf("path %q expected 403 native_path_not_allowed, got %d: %s", tp, rawResp.StatusCode, string(body))
		}
	}

	if up.hits.Load() != 0 {
		t.Fatalf("expected 0 upstream sockets/requests for NEG-1/NEG-2 cases, got %d", up.hits.Load())
	}
}

func TestNativeIdempotencyDisconnectAndDisableFlag(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	// 1. Verify DisableNativePassthrough returns 404 on /native/* while leaving /v1/* functional.
	hDisabled := setupNativeHarness(t, func(cfg *ingresshttp.ServerConfig) {
		cfg.DisableNativePassthrough = true
	})
	disReq, _ := http.NewRequest(http.MethodPost, hDisabled.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o"}`)))
	disReq.Header.Set("Authorization", "Bearer "+hDisabled.vkeyRaw)
	disResp, err := hDisabled.client.Do(disReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = disResp.Body.Close()
	if disResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 when DisableNativePassthrough=true, got %d", disResp.StatusCode)
	}

	// 2. Active harness for Idempotency-Key + client disconnect (<500ms cancel).
	h := setupNativeHarness(t, nil)
	upstreamCanceled := make(chan struct{}, 1)
	var hitCount atomic.Int64

	up := newSpyUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		n := hitCount.Add(1)
		if bytes.Contains(body, []byte(`"stream":true`)) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"id\":\"1\"}\n\n"))
			http.NewResponseController(w).Flush()
			select {
			case <-r.Context().Done():
				select {
				case upstreamCanceled <- struct{}{}:
				default:
				}
			case <-time.After(3 * time.Second):
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"idem_resp_` + string(rune('0'+n)) + `","usage":{"prompt_tokens":4,"completion_tokens":2}}`))
	})

	manifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/openai-idem"
  name: "OpenAI Idem"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "openai-idem"
    supportedDialects: ["openai"]
    allowedNativePaths:
      - "^v1/chat/completions$"
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["bearer"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
adapter:
  slug: "openai-idem"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	h.registerAdapterDeployment("@acme/openai-idem", "openai-idem", "openai", "gpt-4o", "gpt-4o", up.srv.URL, manifest, nil)

	// Idempotency replay
	bodyA := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"idem"}]}`)
	req1, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader(bodyA))
	req1.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	req1.Header.Set("Idempotency-Key", "native-idem-key-1")
	r1, err := h.client.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := io.ReadAll(r1.Body)
	_ = r1.Body.Close()

	req2, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader(bodyA))
	req2.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	req2.Header.Set("Idempotency-Key", "native-idem-key-1")
	r2, err := h.client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := io.ReadAll(r2.Body)
	_ = r2.Body.Close()
	if !bytes.Equal(b1, b2) || r2.Header.Get("Idempotent-Replayed") != "true" || hitCount.Load() != 1 {
		t.Fatalf("expected exact idempotent replay with 1 upstream hit; hits=%d replayed=%q", hitCount.Load(), r2.Header.Get("Idempotent-Replayed"))
	}

	// Idempotency payload mismatch -> 422
	bodyB := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"changed"}]}`)
	req3, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader(bodyB))
	req3.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	req3.Header.Set("Idempotency-Key", "native-idem-key-1")
	r3, err := h.client.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	b3, _ := io.ReadAll(r3.Body)
	_ = r3.Body.Close()
	if r3.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(b3), "idempotency_key_reused_mismatch") {
		t.Fatalf("expected 422 idempotency_key_reused_mismatch, got %d: %s", r3.StatusCode, string(b3))
	}

	// Client disconnect propagates to upstream < 500ms.
	ctxCancel, cancel := context.WithCancel(context.Background())
	reqDC, _ := http.NewRequestWithContext(ctxCancel, http.MethodPost, h.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o","stream":true}`)))
	reqDC.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	rDC, err := h.client.Do(reqDC)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	_, _ = rDC.Body.Read(buf)
	startDC := time.Now()
	cancel()
	_ = rDC.Body.Close()

	select {
	case <-upstreamCanceled:
		if time.Since(startDC) > 500*time.Millisecond {
			t.Fatalf("upstream cancel exceeded 500ms: %v", time.Since(startDC))
		}
	case <-time.After(600 * time.Millisecond):
		t.Fatal("upstream was not canceled within 500ms of client disconnect")
	}
}

func TestNativeLargePayloadPreservationAndMidStreamAbort(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	h := setupNativeHarness(t, nil)

	up := newSpyUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.URL.Path == "/v1/drop" {
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijack", 500)
				return
			}
			conn, rw, err := hj.Hijack()
			if err != nil {
				return
			}
			_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n19\r\ndata: {\"chunk\":\"first\"}\n\r\n")
			_ = rw.Flush()
			// Abruptly close TCP socket mid-chunked stream without terminating chunk 0\r\n\r\n!
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"large_ok","usage":{"prompt_tokens":10,"completion_tokens":2}}`))
	})

	manifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/openai-large"
  name: "OpenAI Large"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "openai-large"
    supportedDialects: ["openai"]
    allowedNativePaths:
      - "^v1/(chat/completions|drop)$"
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["bearer"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
adapter:
  slug: "openai-large"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	h.registerAdapterDeployment("@acme/openai-large", "openai-large", "openai", "gpt-4o", "gpt-4o", up.srv.URL, manifest, nil)

	// >300 KiB payload (exceeds 256 KiB InspectNativeRequest bound): Wasm/inspect sees bounded metadata
	// while BlindBroker sends every single byte intact to upstream.
	bigFiller := strings.Repeat("A", 300*1024)
	bigPayload := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"` + bigFiller + `"}],"x_tail_field":"keep-100-pct"}`)
	bounded := native.BuildBoundedInspectPayload(bigPayload, native.RawBodyHints{Model: "gpt-4o"})
	if len(bounded) > 512 {
		t.Fatalf("expected bounded inspect payload <= 512 bytes, got %d", len(bounded))
	}

	reqBig, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader(bigPayload))
	reqBig.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	respBig, err := h.client.Do(reqBig)
	if err != nil {
		t.Fatal(err)
	}
	_ = respBig.Body.Close()
	if respBig.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on >300KiB payload, got %d", respBig.StatusCode)
	}
	if !bytes.Equal(up.lastReq().Body, bigPayload) {
		t.Fatalf("large native payload was truncated or mutated (%d != %d bytes)", len(up.lastReq().Body), len(bigPayload))
	}

	// Abrupt upstream chunked stream close after chunk 1 emits mid_stream_abort SSE error event.
	dropReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/drop", bytes.NewReader([]byte(`{"model":"gpt-4o","stream":true}`)))
	dropReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	dropResp, err := h.client.Do(dropReq)
	if err != nil {
		t.Fatal(err)
	}
	dropBytes, _ := io.ReadAll(dropResp.Body)
	_ = dropResp.Body.Close()
	if !strings.Contains(string(dropBytes), `"mid_stream_abort"`) {
		t.Fatalf("expected mid_stream_abort SSE frame after unexpected upstream drop, got: %s", string(dropBytes))
	}
}

func TestNativeUpstreamErrorNormalizationAndAUTH06PreFirstByteRevocation(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	h := setupNativeHarness(t, nil)

	var mode atomic.Int32 // 0=429 sync, 1=500 stream, 2=hold until canceled (AUTH-06)
	reqEntered := make(chan struct{}, 2)

	up := newSpyUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch mode.Load() {
		case 0:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Upstream-Leak", h.secret)
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit hit secret=` + h.secret + `"}}`))
		case 1:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Upstream-Leak", h.secret)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"internal upstream trace=` + h.secret + `"}}`))
		case 2:
			select {
			case reqEntered <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
			}
		}
	})

	wasmBytes := buildAnthropicNativeTestWasm()
	wasmSum := sha256.Sum256(wasmBytes)
	wasmHex := hex.EncodeToString(wasmSum[:])
	manifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/anthropic-err-auth06"
  name: "Anthropic Native Error & Auth06"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "sandboxed_wasm"
  entrypoint: "dist/adapter.wasm"
  sha256: "` + wasmHex + `"
  quotas:
    maxMemoryMib: 16
    maxCpuMsPerCall: 2000
capabilities:
  - type: "provider_adapter"
    adapterSlug: "anthropic-err-auth06"
    supportedDialects: ["anthropic"]
    allowedNativePaths:
      - "^v1/messages$"
    authMethods:
      - id: "api_key_hdr"
        mode: "host_blind_inject"
        headerName: "x-api-key"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["api_key_hdr"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
    - "provider:normalize_error"
`
	h.registerAdapterDeployment("@acme/anthropic-err-auth06", "anthropic-err-auth06", "anthropic", "claude-sonnet-4-5", "claude-sonnet-4-5", up.srv.URL, manifest, wasmBytes)

	// 1. Upstream 429 on sync -> normalized 429 canonical envelope, zero secret leak.
	mode.Store(0)
	req429, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/anthropic/v1/messages", bytes.NewReader([]byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"err429"}]}`)))
	req429.Header.Set("x-api-key", h.vkeyRaw)
	resp429, err := h.client.Do(req429)
	if err != nil {
		t.Fatal(err)
	}
	body429, _ := io.ReadAll(resp429.Body)
	_ = resp429.Body.Close()
	if resp429.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(body429), `"upstream_overloaded"`) || strings.Contains(string(body429), h.secret) {
		t.Fatalf("unexpected 429 normalized response (%d): %s", resp429.StatusCode, string(body429))
	}

	// SM-04 places single deployment into short THROTTLED_COOLDOWN after 429; advance FakeClock past cooldown before 500 subtest.
	h.clk.Advance(5 * time.Second)

	// 2. Upstream 500 on streaming -> mapped 502 Bad Gateway canonical envelope, zero secret leak.
	mode.Store(1)
	req500, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/anthropic/v1/messages", bytes.NewReader([]byte(`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"err500"}]}`)))
	req500.Header.Set("x-api-key", h.vkeyRaw)
	resp500, err := h.client.Do(req500)
	if err != nil {
		t.Fatal(err)
	}
	body500, _ := io.ReadAll(resp500.Body)
	_ = resp500.Body.Close()
	if resp500.StatusCode != http.StatusBadGateway || !strings.Contains(string(body500), `"upstream_overloaded"`) || strings.Contains(string(body500), h.secret) {
		t.Fatalf("unexpected 500->502 streaming normalized response (%d): %s", resp500.StatusCode, string(body500))
	}

	// 3. AUTH-06 pre-first-byte Virtual Key emergency revocation terminates in-flight stream with 401 invalid_virtual_key.
	mode.Store(2)
	type auth06Result struct {
		status int
		body   string
		err    error
	}
	resCh := make(chan auth06Result, 1)
	go func() {
		reqHold, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/anthropic/v1/messages", bytes.NewReader([]byte(`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"auth06"}]}`)))
		reqHold.Header.Set("x-api-key", h.vkeyRaw)
		respHold, err := h.client.Do(reqHold)
		if err != nil {
			resCh <- auth06Result{err: err}
			return
		}
		b, _ := io.ReadAll(respHold.Body)
		_ = respHold.Body.Close()
		resCh <- auth06Result{status: respHold.StatusCode, body: string(b)}
	}()

	select {
	case <-reqEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for in-flight request to reach upstream")
	}

	if _, err := h.vkeys.Revoke(h.ctx, vkey.RevokeParams{
		OrgID:           h.org.ID,
		KeyID:           h.vkeyID,
		ExpectedVersion: 1,
		Mode:            vkey.RevokeModeEmergency,
		ActorID:         "prc_op_1",
	}); err != nil {
		t.Fatalf("Revoke emergency failed: %v", err)
	}

	select {
	case got := <-resCh:
		if got.err != nil {
			t.Fatalf("in-flight request failed: %v", got.err)
		}
		if got.status != http.StatusUnauthorized || !strings.Contains(got.body, `"invalid_virtual_key"`) {
			t.Fatalf("expected AUTH-06 401 invalid_virtual_key, got %d: %s", got.status, got.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for AUTH-06 pre-first-byte cancellation response")
	}

	canary.NewScanner(h.secret, h.vkeyRaw).AssertCleanBuffer(t, "audit", h.audit.raws)
}

func TestNativeMultiDialectFailClosedQueryIdempotencyAndHeaderStrip(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	h := setupNativeHarness(t, nil)

	up := newSpyUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if strings.HasPrefix(r.URL.Path, "/v1beta/models/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"custom_auth_ok","usage":{"prompt_tokens":2,"completion_tokens":1}}`))
	})

	// 1. Register a multi-dialect adapter without explicit Deployment.NativeDialect or Adapter.NativePrefix -> fails closed (400 dialect_mismatch).
	multiManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/multi-dialect"
  name: "Multi Dialect"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "multi-dialect"
    supportedDialects: ["openai", "anthropic"]
    allowedNativePaths:
      - "^v1/(chat/completions|messages)$"
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["bearer"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
adapter:
  slug: "multi-dialect"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	multiAdpExtDir := filepath.Join(h.tmp, "ext-multi-dialect")
	if err := os.MkdirAll(multiAdpExtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(multiAdpExtDir, "manifest.yaml"), []byte(multiManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	tpkgPath := filepath.Join(h.tmp, "multi-dialect-1.0.0.tpkg")
	packRes, err := testharness.PackExtension(h.ctx, multiAdpExtDir, tpkgPath)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(h.pubPriv, installer.SignatureSigningPayload("@acme/multi-dialect", "1.0.0", packRes.SHA256))
	staged, err := h.installer.StageFreshInstall(h.ctx, installer.StageInstallRequest{
		ArchivePath: tpkgPath,
		Source:      installer.SourceSpec{Kind: installer.SourceKindLocalArchive, URI: "./multi.tpkg", ExpectedSHA256: packRes.SHA256},
		Signature:   installer.ProvenanceSignature{PublisherOrRootSig: sig},
		ActorID:     "prc_op_1",
		RequestID:   "req_stage_multi",
		TargetOrgID: h.org.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := h.installer.ApproveAndActivate(h.ctx, installer.ApproveInstallRequest{
		InstallationID: staged.Installation.InstallationID, ExpectedVersion: staged.Installation.Version,
		ApproverPrincipalID: "prc_op_1", ActivateImmediately: true, OrgID: h.org.ID, RequestID: "req_app_multi",
	})
	if err != nil {
		t.Fatal(err)
	}
	multiAdp, err := h.catalog.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID:    rec.InstallationID,
		AdapterSlug:       "multi-dialect",
		SupportedDialects: []string{"openai", "anthropic"},
		AllowedHosts:      []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	secMulti, err := h.vault.SealSecret(h.ctx, vault.SealSecretParams{OrgID: h.org.ID, Name: "sec-multi", SecretKind: vault.SecretKindAPIKey, Plaintext: []byte(h.secret)})
	if err != nil {
		t.Fatal(err)
	}
	connMulti, err := h.catalog.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID: h.org.ID, AdapterID: multiAdp.ID, Name: "conn-multi", EndpointBaseURL: up.srv.URL,
		AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, DataRegion: "us",
		BillingMode: catalog.BillingModeCommercialAPI, AuthMode: catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secMulti.ID, InjectMode: catalog.InjectModeHostBlind}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connMulti, _ = h.catalog.TransitionConnectionState(h.ctx, h.org.ID, connMulti.ID, catalog.ConnStateValidating, connMulti.Version, "validate")
	_, _ = h.catalog.TransitionConnectionState(h.ctx, h.org.ID, connMulti.ID, catalog.ConnStateActive, connMulti.Version, "activate")
	lmodMulti, _ := h.catalog.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{OrgID: &h.org.ID, CanonicalSlug: "multi-model", ModalityKind: catalog.ModalityChat, ContextWindow: 8192, MaxOutputTokens: 2048})
	_, _ = h.catalog.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID: h.org.ID, ConnectionID: connMulti.ID, LogicalModelID: lmodMulti.ID, UpstreamModelName: "multi-model",
		AssuranceLevel: catalog.AssuranceTested, State: catalog.DeployStateActiveHealthy,
	})

	// 2. Register OpenAI deployment with custom allowlisted credential header `x-provider-token` (proving collision stripping) and Gemini for query idempotency.
	customAuthManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/custom-hdr"
  name: "Custom Header Auth"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "custom-hdr"
    supportedDialects: ["openai"]
    allowedNativePaths:
      - "^v1/chat/completions$"
    allowedNativeHeaders:
      - "x-provider-token"
      - "OpenAI-Beta"
    authMethods:
      - id: "tok_hdr"
        mode: "host_blind_inject"
        headerName: "x-provider-token"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["tok_hdr"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
adapter:
  slug: "custom-hdr"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "tok_hdr"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "x-provider-token"
`
	geminiQueryManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/gemini-qid"
  name: "Gemini Query Idem"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "gemini-qid"
    supportedDialects: ["gemini"]
    allowedNativePaths:
      - "^v1beta/models/[A-Za-z0-9._-]+:generateContent$"
    authMethods:
      - id: "goog_key"
        mode: "host_blind_inject"
        headerName: "x-goog-api-key"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["goog_key"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
adapter:
  slug: "gemini-qid"
  auth:
    - id: "goog_key"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "x-goog-api-key"
  usageExtraction:
    inputTokensPath: "usageMetadata.promptTokenCount"
    outputTokensPath: "usageMetadata.candidatesTokenCount"
`
	h.registerAdapterDeployment("@acme/custom-hdr", "custom-hdr", "openai", "gpt-4o", "gpt-4o", up.srv.URL, customAuthManifest, nil)
	h.registerAdapterDeployment("@acme/gemini-qid", "gemini-qid", "gemini", "gemini-2.5-flash", "gemini-2.5-flash", up.srv.URL, geminiQueryManifest, nil)

	// Multi-dialect adapter without explicit binding -> 400 dialect_mismatch and 0 upstream hits.
	for _, path := range []string{"/native/openai/v1/chat/completions", "/native/anthropic/v1/messages"} {
		req, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+path, bytes.NewReader([]byte(`{"model":"multi-model"}`)))
		req.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(b), `"dialect_mismatch"`) {
			t.Fatalf("path %s multi-dialect without binding expected 400 dialect_mismatch, got %d: %s", path, resp.StatusCode, string(b))
		}
	}
	if up.hits.Load() != 0 {
		t.Fatalf("multi-dialect rejected calls hit upstream (%d)", up.hits.Load())
	}

	// Spoofed allowlisted credential header `x-provider-token` must be stripped cleanly and replaced by BlindBroker secret.
	hdrReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hdr"}]}`)))
	hdrReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	hdrReq.Header.Set("X-Provider-Token", "client-spoofed-value")
	hdrReq.Header.Set("OpenAI-Beta", "assistants=v2")
	hdrResp, err := h.client.Do(hdrReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = hdrResp.Body.Close()
	if hdrResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on custom auth header test, got %d", hdrResp.StatusCode)
	}
	vals := up.lastReq().Headers.Values("X-Provider-Token")
	if len(vals) != 1 || vals[0] != h.secret {
		t.Fatalf("expected single BlindBroker secret in X-Provider-Token without spoofed collision, got %#v", vals)
	}

	// Query-aware idempotency: same key across different cleanQuery must mismatch (422), not replay.
	qBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"q1"}]}]}`)
	qReq1, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/gemini/v1beta/models/gemini-2.5-flash:generateContent?alt=json", bytes.NewReader(qBody))
	qReq1.Header.Set("x-goog-api-key", h.vkeyRaw)
	qReq1.Header.Set("Idempotency-Key", "gem-q-idem-1")
	qResp1, err := h.client.Do(qReq1)
	if err != nil {
		t.Fatal(err)
	}
	_ = qResp1.Body.Close()
	if qResp1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on initial Gemini query idempotency call, got %d", qResp1.StatusCode)
	}

	qReq2, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/gemini/v1beta/models/gemini-2.5-flash:generateContent", bytes.NewReader(qBody))
	qReq2.Header.Set("x-goog-api-key", h.vkeyRaw)
	qReq2.Header.Set("Idempotency-Key", "gem-q-idem-1")
	qResp2, err := h.client.Do(qReq2)
	if err != nil {
		t.Fatal(err)
	}
	qBody2, _ := io.ReadAll(qResp2.Body)
	_ = qResp2.Body.Close()
	if qResp2.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(qBody2), "idempotency_key_reused_mismatch") {
		t.Fatalf("expected 422 idempotency_key_reused_mismatch across distinct query strings, got %d: %s", qResp2.StatusCode, string(qBody2))
	}
}

type inspectOverrideRuntime struct {
	hints port.NativeRoutingHints
}

func (r *inspectOverrideRuntime) BuildValidationProbe(context.Context, port.ValidationProbeInput) (port.UpstreamHttpSpec, error) {
	return port.UpstreamHttpSpec{}, nil
}
func (r *inspectOverrideRuntime) VerifyValidationResult(context.Context, port.ValidationResultInput) (port.ValidationVerdict, error) {
	return port.ValidationVerdict{}, nil
}
func (r *inspectOverrideRuntime) TransformRequest(context.Context, port.TransformRequestInput) (port.UpstreamHttpSpec, error) {
	return port.UpstreamHttpSpec{}, nil
}
func (r *inspectOverrideRuntime) TransformResponse(context.Context, port.TransformResponseInput) (port.NormalizedInferenceResponse, error) {
	return port.NormalizedInferenceResponse{}, nil
}
func (r *inspectOverrideRuntime) TransformStreamChunk(context.Context, port.StreamChunkInput) (port.StreamChunkOutput, error) {
	return port.StreamChunkOutput{}, nil
}
func (r *inspectOverrideRuntime) NormalizeError(context.Context, port.NormalizeErrorInput) (port.CanonicalProviderError, error) {
	return port.CanonicalProviderError{CanonicalCode: "upstream_error", ErrorType: "upstream_error"}, nil
}
func (r *inspectOverrideRuntime) ExtractUsage(context.Context, port.ExtractUsageInput) (port.NormalizedUsage, error) {
	return port.NormalizedUsage{Accuracy: port.UsageAccuracyExact}, nil
}
func (r *inspectOverrideRuntime) InspectNativeRequest(context.Context, port.NativeRequestInput) (port.NativeRoutingHints, error) {
	return r.hints, nil
}

func TestNativeAuthModeNoneRequiredCapabilitiesAndModelReresolution(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	h := setupNativeHarness(t, nil)

	up := newSpyUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ollama_ok","usage":{"prompt_tokens":4,"completion_tokens":2}}`))
	})

	// 1. Register an Ollama-style declarative adapter WITH optional auth slot, wired to AUTH_MODE=NONE connection (0 SecretBindings).
	noneManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/ollama-none"
  name: "Ollama None Auth"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "ollama-none"
    supportedDialects: ["openai"]
    allowedNativePaths:
      - "^v1/chat/completions$"
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["bearer"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
adapter:
  slug: "ollama-none"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	extDir := filepath.Join(h.tmp, "ext-ollama-none")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, "manifest.yaml"), []byte(noneManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	tpkg := filepath.Join(h.tmp, "ollama-none-1.0.0.tpkg")
	packRes, err := testharness.PackExtension(h.ctx, extDir, tpkg)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(h.pubPriv, installer.SignatureSigningPayload("@acme/ollama-none", "1.0.0", packRes.SHA256))
	staged, err := h.installer.StageFreshInstall(h.ctx, installer.StageInstallRequest{
		ArchivePath: tpkg,
		Source:      installer.SourceSpec{Kind: installer.SourceKindLocalArchive, URI: "./ollama-none.tpkg", ExpectedSHA256: packRes.SHA256},
		Signature:   installer.ProvenanceSignature{PublisherOrRootSig: sig},
		ActorID:     "prc_op_1",
		RequestID:   "req_stage_ollama",
		TargetOrgID: h.org.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := h.installer.ApproveAndActivate(h.ctx, installer.ApproveInstallRequest{
		InstallationID: staged.Installation.InstallationID, ExpectedVersion: staged.Installation.Version,
		ApproverPrincipalID: "prc_op_1", ActivateImmediately: true, OrgID: h.org.ID, RequestID: "req_app_ollama",
	})
	if err != nil {
		t.Fatal(err)
	}
	adp, err := h.catalog.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID:    rec.InstallationID,
		AdapterSlug:       "ollama-none",
		SupportedDialects: []string{"openai"},
		AllowedHosts:      []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	connNone, err := h.catalog.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID: h.org.ID, AdapterID: adp.ID, Name: "conn-ollama-none", EndpointBaseURL: up.srv.URL,
		AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, DataRegion: "us",
		BillingMode: catalog.BillingModeSelfHostedInfra, AuthMode: catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	connNone, _ = h.catalog.TransitionConnectionState(h.ctx, h.org.ID, connNone.ID, catalog.ConnStateValidating, connNone.Version, "validate")
	_, _ = h.catalog.TransitionConnectionState(h.ctx, h.org.ID, connNone.ID, catalog.ConnStateActive, connNone.Version, "activate")
	lmodOllama, _ := h.catalog.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{OrgID: &h.org.ID, CanonicalSlug: "llama3.2", ModalityKind: catalog.ModalityChat, ContextWindow: 8192, MaxOutputTokens: 2048})
	_, _ = h.catalog.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID: h.org.ID, ConnectionID: connNone.ID, LogicalModelID: lmodOllama.ID, UpstreamModelName: "llama3.2",
		AssuranceLevel: catalog.AssuranceTested, State: catalog.DeployStateActiveHealthy,
	})

	// Also register an Anthropic deployment to test TargetModel re-resolution -> 400 dialect_mismatch.
	anthropicManifest := strings.ReplaceAll(noneManifest, "ollama-none", "anthropic-other")
	anthropicManifest = strings.ReplaceAll(anthropicManifest, `supportedDialects: ["openai"]`, `supportedDialects: ["anthropic"]`)
	anthropicManifest = strings.ReplaceAll(anthropicManifest, "Ollama None Auth", "Anthropic Other")
	h.registerAdapterDeployment("@acme/anthropic-other", "anthropic-other", "anthropic", "claude-only", "claude-only", up.srv.URL, anthropicManifest, nil)

	reqNone, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"llama3.2","messages":[{"role":"user","content":"hi"}]}`)))
	reqNone.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	respNone, err := h.client.Do(reqNone)
	if err != nil {
		t.Fatal(err)
	}
	bNone, _ := io.ReadAll(respNone.Body)
	_ = respNone.Body.Close()
	if respNone.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on AUTH_MODE=NONE native passthrough, got %d: %s", respNone.StatusCode, string(bNone))
	}
	if up.lastReq().Headers.Get("Authorization") != "" {
		t.Fatalf("expected zero Authorization header on AUTH_MODE=NONE, got %q", up.lastReq().Headers.Get("Authorization"))
	}

	// 2. Direct Handler test covering RequiredCapabilities (`vision` missing -> 400 capability_not_supported) and TargetModel re-resolution (`claude-only` -> 400 dialect_mismatch).
	hitsBefore := up.hits.Load()
	stubRT := &inspectOverrideRuntime{}
	customHandler, err := native.NewHandler(native.Config{
		Snapshots: h.snapshots,
		Broker:    h.broker,
		Audit:     h.audit,
		NowMs:     func() int64 { return h.clk.Now().UnixMilli() },
		AcquireRuntime: func(context.Context, *snapshot.ResolvedTarget) (port.ProviderRuntime, func(), error) {
			return stubRT, func() {}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	customMux := http.NewServeMux()
	customMux.Handle("/native/", customHandler)
	customSrv := httptest.NewServer(customMux)
	t.Cleanup(customSrv.Close)

	stubRT.hints = port.NativeRoutingHints{
		TargetModel:          "llama3.2",
		RequiredCapabilities: []string{"vision"},
	}
	capReq, _ := http.NewRequest(http.MethodPost, customSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"llama3.2"}`)))
	capReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	capResp, err := h.client.Do(capReq)
	if err != nil {
		t.Fatal(err)
	}
	capBody, _ := io.ReadAll(capResp.Body)
	_ = capResp.Body.Close()
	if capResp.StatusCode != http.StatusBadRequest || !strings.Contains(string(capBody), `"capability_not_supported"`) {
		t.Fatalf("expected 400 capability_not_supported, got %d: %s", capResp.StatusCode, string(capBody))
	}

	stubRT.hints = port.NativeRoutingHints{
		TargetModel: "claude-only",
	}
	reReq, _ := http.NewRequest(http.MethodPost, customSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"llama3.2"}`)))
	reReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	reResp, err := h.client.Do(reReq)
	if err != nil {
		t.Fatal(err)
	}
	reBody, _ := io.ReadAll(reResp.Body)
	_ = reResp.Body.Close()
	if reResp.StatusCode != http.StatusBadRequest || !strings.Contains(string(reBody), `"dialect_mismatch"`) {
		t.Fatalf("expected 400 dialect_mismatch on TargetModel override to anthropic deployment, got %d: %s", reResp.StatusCode, string(reBody))
	}
	if up.hits.Load() != hitsBefore {
		t.Fatalf("rejected capability/override calls unexpectedly hit upstream")
	}
}

type inspectMultiRuntime struct {
	fn func(context.Context, port.NativeRequestInput) (port.NativeRoutingHints, error)
}

func (r *inspectMultiRuntime) BuildValidationProbe(context.Context, port.ValidationProbeInput) (port.UpstreamHttpSpec, error) {
	return port.UpstreamHttpSpec{}, nil
}
func (r *inspectMultiRuntime) VerifyValidationResult(context.Context, port.ValidationResultInput) (port.ValidationVerdict, error) {
	return port.ValidationVerdict{}, nil
}
func (r *inspectMultiRuntime) TransformRequest(context.Context, port.TransformRequestInput) (port.UpstreamHttpSpec, error) {
	return port.UpstreamHttpSpec{}, nil
}
func (r *inspectMultiRuntime) TransformResponse(context.Context, port.TransformResponseInput) (port.NormalizedInferenceResponse, error) {
	return port.NormalizedInferenceResponse{}, nil
}
func (r *inspectMultiRuntime) TransformStreamChunk(context.Context, port.StreamChunkInput) (port.StreamChunkOutput, error) {
	return port.StreamChunkOutput{}, nil
}
func (r *inspectMultiRuntime) NormalizeError(context.Context, port.NormalizeErrorInput) (port.CanonicalProviderError, error) {
	return port.CanonicalProviderError{CanonicalCode: "upstream_error", ErrorType: "upstream_error"}, nil
}
func (r *inspectMultiRuntime) ExtractUsage(context.Context, port.ExtractUsageInput) (port.NormalizedUsage, error) {
	return port.NormalizedUsage{Accuracy: port.UsageAccuracyExact}, nil
}
func (r *inspectMultiRuntime) InspectNativeRequest(ctx context.Context, in port.NativeRequestInput) (port.NativeRoutingHints, error) {
	return r.fn(ctx, in)
}

func TestNativeConflictingKeysRouteAllowlistAndAdapterSwitch(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	h := setupNativeHarness(t, nil)

	upA := newSpyUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"up_a","usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	})
	upB := newSpyUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"up_b","usage":{"prompt_tokens":2,"completion_tokens":3}}`))
	})

	manifestTpl := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "%s"
  name: "%s"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "%s"
    supportedDialects: ["openai"]
    allowedNativePaths:
      - "^v1/chat/completions$"
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["bearer"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
adapter:
  slug: "%s"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	mA := fmt.Sprintf(manifestTpl, "@acme/adp-a", "Adapter A", "adp-a", "adp-a")
	mB := fmt.Sprintf(manifestTpl, "@acme/adp-b", "Adapter B", "adp-b", "adp-b")
	h.registerAdapterDeployment("@acme/adp-a", "adp-a", "openai", "model-a", "model-a", upA.srv.URL, mA, nil)
	h.registerAdapterDeployment("@acme/adp-b", "adp-b", "openai", "model-b", "model-b", upB.srv.URL, mB, nil)

	// Second Virtual Key scoped strictly to /v1/* routes.
	v1OnlyVK, err := h.vkeys.Create(h.ctx, vkey.CreateParams{
		OrgID:                h.org.ID,
		ProjectID:            h.project.ID,
		EnvID:                h.env.ID,
		Name:                 "v1-only-vk",
		AllowedPlanes:        "PROVIDER",
		AllowedRoutes:        []string{"/v1/*"},
		MaxConcurrentStreams: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	// 1. Conflicting Virtual Keys in Authorization and x-api-key -> 401 invalid_virtual_key.
	confReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"model-a"}`)))
	confReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	confReq.Header.Set("x-api-key", v1OnlyVK.SecretKey)
	confResp, err := h.client.Do(confReq)
	if err != nil {
		t.Fatal(err)
	}
	confBody, _ := io.ReadAll(confResp.Body)
	_ = confResp.Body.Close()
	if confResp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(confBody), `"invalid_virtual_key"`) {
		t.Fatalf("expected 401 invalid_virtual_key for conflicting VK headers, got %d: %s", confResp.StatusCode, string(confBody))
	}

	// 1b. Conflicting duplicate Authorization headers -> 401 invalid_virtual_key.
	dupAuthReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"model-a"}`)))
	dupAuthReq.Header.Add("Authorization", "Bearer "+h.vkeyRaw)
	dupAuthReq.Header.Add("Authorization", "Bearer "+v1OnlyVK.SecretKey)
	dupAuthResp, err := h.client.Do(dupAuthReq)
	if err != nil {
		t.Fatal(err)
	}
	dupAuthBody, _ := io.ReadAll(dupAuthResp.Body)
	_ = dupAuthResp.Body.Close()
	if dupAuthResp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(dupAuthBody), `"invalid_virtual_key"`) {
		t.Fatalf("expected 401 invalid_virtual_key for duplicate conflicting Authorization headers, got %d: %s", dupAuthResp.StatusCode, string(dupAuthBody))
	}

	// 2. AllowedRoutes: ["/v1/*"] denied on /native/* -> 403 scope_or_plane_denied.
	routeReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"model-a"}`)))
	routeReq.Header.Set("Authorization", "Bearer "+v1OnlyVK.SecretKey)
	routeResp, err := h.client.Do(routeReq)
	if err != nil {
		t.Fatal(err)
	}
	routeBody, _ := io.ReadAll(routeResp.Body)
	_ = routeResp.Body.Close()
	if routeResp.StatusCode != http.StatusForbidden || !strings.Contains(string(routeBody), `"scope_or_plane_denied"`) {
		t.Fatalf("expected 403 scope_or_plane_denied for /v1/* VK on /native/*, got %d: %s", routeResp.StatusCode, string(routeBody))
	}

	// 3. adapterChanged == true during TargetModel override:
	//    (a) Reject second-hop model redirect (model-b -> model-c) with 502 extension_execution_fault.
	var secondHopMode atomic.Int64 // 0=redirect, 1=echo, 2=empty TargetModel
	inj := []port.CredentialInjectInstruction{{
		SecretSlotName: "api_key",
		TargetLocation: port.CredentialTargetLocationHeader,
		FieldName:      "Authorization",
		ValuePrefix:    "Bearer ",
	}}
	var callIdx atomic.Int64
	rtSwitch := &inspectMultiRuntime{
		fn: func(_ context.Context, _ port.NativeRequestInput) (port.NativeRoutingHints, error) {
			n := callIdx.Add(1)
			if n%2 == 1 {
				return port.NativeRoutingHints{
					TargetModel:          "model-b",
					CredentialInjections: inj,
				}, nil
			}
			switch secondHopMode.Load() {
			case 0:
				return port.NativeRoutingHints{
					TargetModel:          "model-c-unverified",
					CredentialInjections: inj,
				}, nil
			case 1:
				return port.NativeRoutingHints{
					TargetModel:          "model-b",
					CredentialInjections: inj,
				}, nil
			default:
				return port.NativeRoutingHints{
					TargetModel:          "",
					CredentialInjections: inj,
				}, nil
			}
		},
	}

	customHandler, err := native.NewHandler(native.Config{
		Snapshots: h.snapshots,
		Broker:    h.broker,
		Audit:     h.audit,
		NowMs:     func() int64 { return h.clk.Now().UnixMilli() },
		AcquireRuntime: func(context.Context, *snapshot.ResolvedTarget) (port.ProviderRuntime, func(), error) {
			return rtSwitch, func() {}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/native/", customHandler)
	customSrv := httptest.NewServer(mux)
	t.Cleanup(customSrv.Close)

	redirReq, _ := http.NewRequest(http.MethodPost, customSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"model-a"}`)))
	redirReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	redirResp, err := h.client.Do(redirReq)
	if err != nil {
		t.Fatal(err)
	}
	redirBody, _ := io.ReadAll(redirResp.Body)
	_ = redirResp.Body.Close()
	if redirResp.StatusCode != http.StatusBadGateway || !strings.Contains(string(redirBody), `"extension_execution_fault"`) {
		t.Fatalf("expected 502 extension_execution_fault on second-hop TargetModel redirect, got %d: %s", redirResp.StatusCode, string(redirBody))
	}
	if upA.hits.Load() != 0 || upB.hits.Load() != 0 {
		t.Fatalf("expected 0 upstream hits on rejected second-hop redirect, got A=%d B=%d", upA.hits.Load(), upB.hits.Load())
	}

	//    (b) Second adapter agrees on "model-b" -> routes to upB cleanly with 200 OK.
	secondHopMode.Store(1)
	okReq, _ := http.NewRequest(http.MethodPost, customSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"model-a"}`)))
	okReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	okResp, err := h.client.Do(okReq)
	if err != nil {
		t.Fatal(err)
	}
	okBody, _ := io.ReadAll(okResp.Body)
	_ = okResp.Body.Close()
	if okResp.StatusCode != http.StatusOK || !bytes.Contains(okBody, []byte(`"up_b"`)) {
		t.Fatalf("expected 200 OK from up_b when switched adapter agrees on TargetModel, got %d: %s", okResp.StatusCode, string(okBody))
	}
	if upA.hits.Load() != 0 || upB.hits.Load() != 1 {
		t.Fatalf("expected upA=0 upB=1 after adapter switch, got A=%d B=%d", upA.hits.Load(), upB.hits.Load())
	}

	//    (c) Second adapter returns TargetModel="" (relying on resolved reRawHints.Model) -> 200 OK from upB.
	secondHopMode.Store(2)
	emptyReq, _ := http.NewRequest(http.MethodPost, customSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"model-a"}`)))
	emptyReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	emptyResp, err := h.client.Do(emptyReq)
	if err != nil {
		t.Fatal(err)
	}
	emptyBody, _ := io.ReadAll(emptyResp.Body)
	_ = emptyResp.Body.Close()
	if emptyResp.StatusCode != http.StatusOK || !bytes.Contains(emptyBody, []byte(`"up_b"`)) {
		t.Fatalf("expected 200 OK from up_b when switched adapter omits TargetModel, got %d: %s", emptyResp.StatusCode, string(emptyBody))
	}
	if upA.hits.Load() != 0 || upB.hits.Load() != 2 {
		t.Fatalf("expected upA=0 upB=2 after empty-TargetModel adapter switch, got A=%d B=%d", upA.hits.Load(), upB.hits.Load())
	}

	// 4. Standalone handler rejects HTTP protocol upgrades (400 unsupported_protocol_upgrade).
	upgReq, _ := http.NewRequest(http.MethodGet, customSrv.URL+"/native/openai/v1/realtime", nil)
	upgReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	upgReq.Header.Set("Connection", "Upgrade")
	upgReq.Header.Set("Upgrade", "websocket")
	upgResp, err := h.client.Do(upgReq)
	if err != nil {
		t.Fatal(err)
	}
	upgBody, _ := io.ReadAll(upgResp.Body)
	_ = upgResp.Body.Close()
	if upgResp.StatusCode != http.StatusBadRequest || !strings.Contains(string(upgBody), `"unsupported_protocol_upgrade"`) {
		t.Fatalf("expected 400 unsupported_protocol_upgrade on standalone handler, got %d: %s", upgResp.StatusCode, string(upgBody))
	}
}
