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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestTP0023_Native_4LevelKillSwitch_SSE_Under1s(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760002000000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }
	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(tmp, "native_ks.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	auditLogger, err := audit.NewLogger(audit.LoggerConfig{
		DB:    st.RawDB(),
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	ksReg, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{
		DB:    st.RawDB(),
		Audit: auditLogger,
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

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
	kek, err := vault.NewStaticKEKProvider("kek-native-ks", rawMaster)
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
		Audit:     auditLogger,
	})
	if err != nil {
		t.Fatal(err)
	}

	brk, err := broker.NewBlindBroker(broker.Config{
		Resolver:       catRepo,
		PolicyResolver: catRepo,
		Vault:          vStore,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          auditLogger,
		NowMs:          nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:         st.RawDB(),
		PluginsDir: pluginsDir,
		Keyring:    keyring,
		Audit:      auditLogger,
		NowMs:      nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	vkSvc, err := vkey.NewService(vkey.ServiceConfig{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Snapshots: mgr,
		AuditSink: auditLogger,
	})
	if err != nil {
		t.Fatal(err)
	}

	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{
		Slug:           "native-ks-org",
		DisplayName:    "Native KS Org",
		MinPluginTrust: storageport.MinTrustVerifiedOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{
		Slug:        "native-ks-proj",
		DisplayName: "Native KS Proj",
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
		VALUES ('prc_op_natks', NULL, 'HUMAN_LOCAL', 'op_natks@acme.test', 'ACTIVE', 1, ?, ?)
	`, nowFn(), nowFn()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_op_natks', NULL, 'prc_op_natks', 'INSTANCE', 'instance', 'OPERATOR', 1, ?)
	`, nowFn()); err != nil {
		t.Fatal(err)
	}

	createdVK, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID:                org.ID,
		ProjectID:            proj.ID,
		EnvID:                env.ID,
		Name:                 "native-ks-vk",
		AllowedPlanes:        "PROVIDER",
		MaxConcurrentStreams: 16,
	})
	if err != nil {
		t.Fatal(err)
	}

	srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:            mgr,
		Broker:               brk,
		PluginsDir:           pluginsDir,
		Audit:                auditLogger,
		KillSwitch:           ksReg,
		NowMs:                nowFn,
		SSEHeartbeatInterval: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)

	client := netguard.NewHTTPClient()
	t.Cleanup(client.CloseIdleConnections)

	releaseCh := make(chan struct{})
	var closeOnce sync.Once
	t.Cleanup(func() { closeOnce.Do(func() { close(releaseCh) }) })

	up := newSpyUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		if bytes.Contains(body, []byte(`"stream":true`)) {
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			rc := http.NewResponseController(w)
			_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_nat_ks\",\"usage\":{\"input_tokens\":120,\"cache_read_input_tokens\":40,\"output_tokens\":1}}}\n\n"))
			_ = rc.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-releaseCh:
			}
			_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":55}}\n\n"))
			_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
			_ = rc.Flush()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_nat_ok","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":120,"output_tokens":55}}`))
	})

	wasmBytes := buildAnthropicNativeTestWasm()
	wasmSum := sha256.Sum256(wasmBytes)
	wasmHex := hex.EncodeToString(wasmSum[:])

	manifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/anthropic-native-ks"
  name: "Anthropic Native KS Wasm"
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
    adapterSlug: "anthropic-native-ks"
    supportedDialects: ["anthropic"]
    allowedNativePaths:
      - "^v1/messages(/count_tokens)?$"
    allowedNativeHeaders:
      - "anthropic-version"
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
	extDir := filepath.Join(tmp, "ext-native-ks")
	_ = os.MkdirAll(filepath.Join(extDir, "dist"), 0o755)
	_ = os.WriteFile(filepath.Join(extDir, "manifest.yaml"), []byte(manifest), 0o600)
	_ = os.WriteFile(filepath.Join(extDir, "dist", "adapter.wasm"), wasmBytes, 0o600)
	tpkgPath := filepath.Join(tmp, "nat-ks.tpkg")
	packRes, err := testharness.PackExtension(ctx, extDir, tpkgPath)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(pubPriv, installer.SignatureSigningPayload("@acme/anthropic-native-ks", "1.0.0", packRes.SHA256))
	staged, err := instSvc.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchivePath: tpkgPath,
		Source: installer.SourceSpec{
			Kind: installer.SourceKindLocalArchive, URI: "./nat-ks.tpkg", ExpectedSHA256: packRes.SHA256,
		},
		Signature: installer.ProvenanceSignature{PublisherOrRootSig: sig},
		ActorID:   "prc_op_natks",
		TargetOrgID: org.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := instSvc.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID: staged.Installation.InstallationID, ExpectedVersion: staged.Installation.Version,
		ApproverPrincipalID: "prc_op_natks", ActivateImmediately: true, OrgID: org.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	adp, err := catRepo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{
		InstallationID: rec.InstallationID, AdapterSlug: "anthropic-native-ks", SupportedDialects: []string{"anthropic"}, AllowedHosts: []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	secMeta, err := vStore.SealSecret(ctx, vault.SealSecretParams{
		OrgID: org.ID, Name: "nat-ks-secret", SecretKind: vault.SecretKindAPIKey, Plaintext: []byte(canary.DefaultCanarySecret()),
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
		OrgID: org.ID, AdapterID: adp.ID, Name: "conn-nat-ks", EndpointBaseURL: up.srv.URL,
		AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, DataRegion: "us",
		BillingMode: catalog.BillingModeCommercialAPI, AuthMode: catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "val")
	_, err = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "act")
	if err != nil {
		t.Fatal(err)
	}
	lm, err := catRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{
		OrgID: &org.ID, CanonicalSlug: "claude-sonnet-4-5", ModalityKind: catalog.ModalityChat, ContextWindow: 128000, MaxOutputTokens: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{
		OrgID: org.ID, ConnectionID: conn.ID, LogicalModelID: lm.ID, UpstreamModelName: "claude-sonnet-4-5",
		AssuranceLevel: catalog.AssuranceTested, State: catalog.DeployStateActiveHealthy,
		ConnectTimeoutMs: 5000, FirstByteTimeoutMs: 5000, IdleChunkTimeoutMs: 5000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	// Start 2 in-flight native SSE streams and sever them via ExtensionInstallation kill switch in < 1s.
	startedCh := make(chan struct{}, 2)
	doneCh := make(chan struct {
		body    string
		endedAt time.Time
	}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, httpSrv.URL+"/native/anthropic/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-api-key", createdVK.SecretKey)
			req.Header.Set("anthropic-version", "2023-06-01")
			resp, rerr := client.Do(req)
			if rerr != nil {
				doneCh <- struct {
					body    string
					endedAt time.Time
				}{body: rerr.Error(), endedAt: time.Now()}
				return
			}
			defer resp.Body.Close()
			rd := bufio.NewReader(resp.Body)
			var sb strings.Builder
			sentStarted := false
			for {
				line, err := rd.ReadString('\n')
				if len(line) > 0 {
					sb.WriteString(line)
					if !sentStarted && strings.Contains(line, "message_start") {
						sentStarted = true
						startedCh <- struct{}{}
					}
				}
				if err != nil {
					doneCh <- struct {
						body    string
						endedAt time.Time
					}{body: sb.String(), endedAt: time.Now()}
					return
				}
			}
		}()
	}

	for i := 0; i < 2; i++ {
		select {
		case <-startedCh:
		case <-time.After(10 * time.Second):
			t.Fatalf("native stream %d failed to start", i)
		}
	}

	trigAt := time.Now()
	ksExt, err := ksReg.Trigger(ctx, killswitch.TriggerParams{
		Level: killswitch.LevelExtensionInstallation, TargetID: rec.InstallationID,
		Reason: "native adapter kill switch containment", TriggeredByPrincipalID: "prc_op_natks",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case res := <-doneCh:
			if strings.Contains(res.body, "message_stop") {
				t.Fatalf("expected severed native stream without message_stop, got %s", res.body)
			}
			if res.endedAt.Sub(trigAt) >= time.Second {
				t.Fatalf("native stream took %v (>= 1s) to sever", res.endedAt.Sub(trigAt))
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("native SSE stream did not sever within < 1s")
		}
	}

	doNative := func() (int, string) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, httpSrv.URL+"/native/anthropic/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", createdVK.SecretKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		resp, rerr := client.Do(req)
		if rerr != nil {
			t.Fatal(rerr)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	if code, body := doNative(); code != http.StatusServiceUnavailable || !strings.Contains(body, `"extension_quarantined"`) {
		t.Fatalf("expected 503 extension_quarantined on native endpoint, got %d %s", code, body)
	}
	if _, err := ksReg.Clear(ctx, killswitch.ClearParams{
		KillSwitchID: ksExt.ID, ExpectedVersion: ksExt.Version,
		ClearedByPrincipalID: "prc_op_natks", ClearReason: "native adapter restored safely",
	}); err != nil {
		t.Fatal(err)
	}
	if code, body := doNative(); code != http.StatusOK {
		t.Fatalf("expected 200 after clear, got %d %s", code, body)
	}

	// Verify PROVIDER_CONNECTION, PROJECT_OR_ORG_FREEZE, and VIRTUAL_KEY on native endpoint.
	ksConn, err := ksReg.Trigger(ctx, killswitch.TriggerParams{
		OrgID: &org.ID, Level: killswitch.LevelProviderConnection, TargetID: conn.ID,
		Reason: "native conn killed", TriggeredByPrincipalID: "prc_op_natks",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := doNative(); code != http.StatusServiceUnavailable || !strings.Contains(body, `"provider_connection_killed"`) {
		t.Fatalf("expected 503 provider_connection_killed on native endpoint, got %d %s", code, body)
	}
	_, _ = ksReg.Clear(ctx, killswitch.ClearParams{
		OrgID: &org.ID, KillSwitchID: ksConn.ID, ExpectedVersion: ksConn.Version,
		ClearedByPrincipalID: "prc_op_natks", ClearReason: "native conn restored",
	})

	ksOrg, err := ksReg.Trigger(ctx, killswitch.TriggerParams{
		OrgID: &org.ID, Level: killswitch.LevelProjectOrOrgFreeze, TargetID: org.ID,
		Reason: "org freeze containment", TriggeredByPrincipalID: "prc_op_natks",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := doNative(); code != http.StatusServiceUnavailable || !strings.Contains(body, `"tenant_traffic_frozen"`) {
		t.Fatalf("expected 503 tenant_traffic_frozen on native endpoint, got %d %s", code, body)
	}
	_, _ = ksReg.Clear(ctx, killswitch.ClearParams{
		OrgID: &org.ID, KillSwitchID: ksOrg.ID, ExpectedVersion: ksOrg.Version,
		ClearedByPrincipalID: "prc_op_natks", ClearReason: "org freeze cleared",
	})

	ksVK, err := ksReg.Trigger(ctx, killswitch.TriggerParams{
		OrgID: &org.ID, Level: killswitch.LevelVirtualKey, TargetID: createdVK.Summary.ID,
		Reason: "native vk compromised", TriggeredByPrincipalID: "prc_op_natks",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := doNative(); code != http.StatusUnauthorized || !strings.Contains(body, `"invalid_virtual_key"`) {
		t.Fatalf("expected 401 invalid_virtual_key on native endpoint, got %d %s", code, body)
	}
	_, _ = ksReg.Clear(ctx, killswitch.ClearParams{
		OrgID: &org.ID, KillSwitchID: ksVK.ID, ExpectedVersion: ksVK.Version,
		ClearedByPrincipalID: "prc_op_natks", ClearReason: "native vk cleared",
	})
	_ = fmt.Sprint("")
}

func TestTP0023_Native_CrossProcessKillSwitchSyncAnd503PreStreamCancel(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760004000000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "native_mp_ks.db")
	pluginsDir := filepath.Join(tmp, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	stWrite, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stWrite.Close() })
	stServe, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath, NowMs: nowFn, SkipMigrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stServe.Close() })

	auditWriter, _ := audit.NewLogger(audit.LoggerConfig{DB: stWrite.RawDB(), NowMs: nowFn})
	ksWriter, _ := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{DB: stWrite.RawDB(), Audit: auditWriter, NowMs: nowFn})
	auditReader, _ := audit.NewLogger(audit.LoggerConfig{DB: stServe.RawDB(), NowMs: nowFn})
	ksReader, _ := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{DB: stServe.RawDB(), Audit: auditReader, NowMs: nowFn})

	pubPub, pubPriv, _ := ed25519.GenerateKey(rand.Reader)
	keyring := trust.TrustedKeyring{PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": pubPub}}
	mgr, _ := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: stServe.RawDB(), NowMs: nowFn, Keyring: keyring})

	rawMaster := make([]byte, 32)
	_, _ = rand.Read(rawMaster)
	kek, _ := vault.NewStaticKEKProvider("kek-nat-mp", rawMaster)
	t.Cleanup(kek.Close)
	vStore, _ := vault.NewStore(vault.Config{DB: stWrite.RawDB(), KEK: kek, NowMs: nowFn, PostShredHook: stWrite.CheckpointWAL})
	t.Cleanup(vStore.Close)

	catRepo, _ := catalog.NewRepository(catalog.Config{DB: stWrite.RawDB(), NowMs: nowFn, Snapshots: mgr, Keyring: keyring, Audit: auditWriter})
	brk, _ := broker.NewBlindBroker(broker.Config{Resolver: catRepo, PolicyResolver: catRepo, Vault: vStore, UnsealCap: vault.MintUnsealCapabilityForBroker(), Audit: auditWriter, NowMs: nowFn})
	instSvc, _ := installer.NewLifecycleService(installer.ServiceConfig{DB: stWrite.RawDB(), PluginsDir: pluginsDir, Keyring: keyring, Audit: auditWriter, NowMs: nowFn})
	vkSvc, _ := vkey.NewService(vkey.ServiceConfig{DB: stWrite.RawDB(), NowMs: nowFn, Snapshots: mgr, AuditSink: auditWriter})

	org, _ := stWrite.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "nat-mp-org", DisplayName: "Nat MP", MinPluginTrust: storageport.MinTrustVerifiedOnly})
	proj, _ := stWrite.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{Slug: "nat-mp-proj", DisplayName: "Nat MP Proj"})
	env, _ := stWrite.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, storageport.CreateEnvironmentParams{Slug: "prod"})
	_, _ = stWrite.RawDB().ExecContext(ctx, `INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms) VALUES ('prc_op_natmp', NULL, 'HUMAN_LOCAL', 'op_natmp@acme.test', 'ACTIVE', 1, ?, ?)`, nowFn(), nowFn())
	_, _ = stWrite.RawDB().ExecContext(ctx, `INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms) VALUES ('rb_op_natmp', NULL, 'prc_op_natmp', 'INSTANCE', 'instance', 'OPERATOR', 1, ?)`, nowFn())

	createdVK, err := vkSvc.Create(ctx, vkey.CreateParams{OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, Name: "nat-mp-vk", AllowedPlanes: "PROVIDER", MaxConcurrentStreams: 16})
	if err != nil {
		t.Fatal(err)
	}

	enteredUpstream := make(chan struct{}, 2)
	releaseHold := make(chan struct{})
	var closeHoldOnce sync.Once
	t.Cleanup(func() { closeHoldOnce.Do(func() { close(releaseHold) }) })

	up := newSpyUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		select {
		case enteredUpstream <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			return
		case <-releaseHold:
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_nat_mp","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`))
	})

	wasmBytes := buildAnthropicNativeTestWasm()
	wasmSum := sha256.Sum256(wasmBytes)
	wasmHex := hex.EncodeToString(wasmSum[:])
	manifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/anthropic-nat-mp"
  name: "Anthropic Native MP Wasm"
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
    adapterSlug: "anthropic-nat-mp"
    supportedDialects: ["anthropic"]
    allowedNativePaths:
      - "^v1/messages(/count_tokens)?$"
    allowedNativeHeaders:
      - "anthropic-version"
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
	extDir := filepath.Join(tmp, "ext-nat-mp")
	_ = os.MkdirAll(filepath.Join(extDir, "dist"), 0o755)
	_ = os.WriteFile(filepath.Join(extDir, "manifest.yaml"), []byte(manifest), 0o600)
	_ = os.WriteFile(filepath.Join(extDir, "dist", "adapter.wasm"), wasmBytes, 0o600)
	tpkgPath := filepath.Join(tmp, "nat-mp.tpkg")
	packRes, _ := testharness.PackExtension(ctx, extDir, tpkgPath)
	sig := ed25519.Sign(pubPriv, installer.SignatureSigningPayload("@acme/anthropic-nat-mp", "1.0.0", packRes.SHA256))
	staged, _ := instSvc.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchivePath: tpkgPath, Source: installer.SourceSpec{Kind: installer.SourceKindLocalArchive, URI: "./nat-mp.tpkg", ExpectedSHA256: packRes.SHA256},
		Signature: installer.ProvenanceSignature{PublisherOrRootSig: sig}, ActorID: "prc_op_natmp", TargetOrgID: org.ID,
	})
	rec, _ := instSvc.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID: staged.Installation.InstallationID, ExpectedVersion: staged.Installation.Version, ApproverPrincipalID: "prc_op_natmp", ActivateImmediately: true, OrgID: org.ID,
	})
	adp, _ := catRepo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{InstallationID: rec.InstallationID, AdapterSlug: "anthropic-nat-mp", SupportedDialects: []string{"anthropic"}, AllowedHosts: []string{"127.0.0.1"}})
	secMeta, _ := vStore.SealSecret(ctx, vault.SealSecretParams{OrgID: org.ID, Name: "nat-mp-sec", SecretKind: vault.SecretKindAPIKey, Plaintext: []byte(canary.DefaultCanarySecret())})
	conn, _ := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
		OrgID: org.ID, AdapterID: adp.ID, Name: "conn-nat-mp", EndpointBaseURL: up.srv.URL, AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, DataRegion: "us",
		BillingMode: catalog.BillingModeCommercialAPI, AuthMode: catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind}},
	})
	conn, _ = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "val")
	_, _ = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "act")
	lm, _ := catRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{OrgID: &org.ID, CanonicalSlug: "claude-sonnet-4-5", ModalityKind: catalog.ModalityChat, ContextWindow: 128000, MaxOutputTokens: 8192})
	_, _ = catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{OrgID: org.ID, ConnectionID: conn.ID, LogicalModelID: lm.ID, UpstreamModelName: "claude-sonnet-4-5", AssuranceLevel: catalog.AssuranceTested, State: catalog.DeployStateActiveHealthy, ConnectTimeoutMs: 5000, FirstByteTimeoutMs: 5000, IdleChunkTimeoutMs: 5000})
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots: mgr, Broker: brk, PluginsDir: pluginsDir, Audit: auditReader, KillSwitch: ksReader, NowMs: nowFn, SSEHeartbeatInterval: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)

	client := netguard.NewHTTPClient()
	t.Cleanup(client.CloseIdleConnections)

	type respRes struct {
		code int
		body string
		err  error
	}
	respCh := make(chan respRes, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, httpSrv.URL+"/native/anthropic/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", createdVK.SecretKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		resp, err := client.Do(req)
		if err != nil {
			respCh <- respRes{err: err}
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		respCh <- respRes{code: resp.StatusCode, body: string(b)}
	}()

	select {
	case <-enteredUpstream:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for native request to reach upstream")
	}

	if _, err := ksWriter.Trigger(ctx, killswitch.TriggerParams{
		Level: killswitch.LevelExtensionInstallation, TargetID: rec.InstallationID,
		Reason: "out-of-band native adapter halt", TriggeredByPrincipalID: "prc_op_natmp",
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatalf("mgr.Refresh: %v", err)
	}

	select {
	case res := <-respCh:
		if res.err != nil {
			t.Fatalf("request err: %v", res.err)
		}
		if res.code != http.StatusServiceUnavailable || !strings.Contains(res.body, `"extension_quarantined"`) {
			t.Fatalf("expected 503 extension_quarantined on pre-stream cancel, got %d %s", res.code, res.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for 503 extension_quarantined response")
	}
}

func TestTP0023_Native_BudgetReserveCleanReleaseOnKillSwitchAndClientCancel(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760006000000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "native_budget_ks.db")
	pluginsDir := filepath.Join(tmp, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	auditLogger, err := audit.NewLogger(audit.LoggerConfig{DB: st.RawDB(), NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	ksReg, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{DB: st.RawDB(), Audit: auditLogger, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	lSvc, err := ledger.NewService(ledger.Config{DB: st.RawDB(), Audit: auditLogger, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}

	pubPub, pubPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := trust.TrustedKeyring{PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": pubPub}}
	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB(), NowMs: nowFn, Keyring: keyring})
	if err != nil {
		t.Fatal(err)
	}

	rawMaster := make([]byte, 32)
	_, _ = rand.Read(rawMaster)
	kek, err := vault.NewStaticKEKProvider("kek-nat-bdg", rawMaster)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kek.Close)
	vStore, err := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: kek, NowMs: nowFn, PostShredHook: st.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vStore.Close)

	catRepo, _ := catalog.NewRepository(catalog.Config{DB: st.RawDB(), NowMs: nowFn, Snapshots: mgr, Keyring: keyring, Audit: auditLogger})
	brk, _ := broker.NewBlindBroker(broker.Config{Resolver: catRepo, PolicyResolver: catRepo, Vault: vStore, UnsealCap: vault.MintUnsealCapabilityForBroker(), Audit: auditLogger, NowMs: nowFn})
	instSvc, _ := installer.NewLifecycleService(installer.ServiceConfig{DB: st.RawDB(), PluginsDir: pluginsDir, Keyring: keyring, Audit: auditLogger, NowMs: nowFn})
	vkSvc, _ := vkey.NewService(vkey.ServiceConfig{DB: st.RawDB(), NowMs: nowFn, Snapshots: mgr, AuditSink: auditLogger})

	org, _ := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "nat-bdg-org", DisplayName: "Nat Bdg", MinPluginTrust: storageport.MinTrustVerifiedOnly})
	proj, _ := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{Slug: "nat-bdg-proj", DisplayName: "Nat Bdg Proj"})
	env, _ := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, storageport.CreateEnvironmentParams{Slug: "prod"})
	_, _ = st.RawDB().ExecContext(ctx, `INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms) VALUES ('prc_op_natbdg', NULL, 'HUMAN_LOCAL', 'op_natbdg@acme.test', 'ACTIVE', 1, ?, ?)`, nowFn(), nowFn())
	_, _ = st.RawDB().ExecContext(ctx, `INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms) VALUES ('rb_op_natbdg', NULL, 'prc_op_natbdg', 'INSTANCE', 'instance', 'OPERATOR', 1, ?)`, nowFn())

	if _, err := lSvc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		ID: "bdg_nat_ks", OrgID: org.ID, ScopeType: ledger.ScopeProject, ScopeID: proj.ID,
		PeriodKind: ledger.PeriodTotalLifetime, PeriodStartMs: 0, PeriodEndMs: nowFn() + 86_400_000,
		HardLimitMicros: 1_000_000,
	}); err != nil {
		t.Fatal(err)
	}

	createdVK, err := vkSvc.Create(ctx, vkey.CreateParams{OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, Name: "nat-bdg-vk", AllowedPlanes: "PROVIDER", MaxConcurrentStreams: 16})
	if err != nil {
		t.Fatal(err)
	}

	enteredUpstream := make(chan struct{}, 2)
	releaseHold := make(chan struct{})
	var closeHoldOnce sync.Once
	t.Cleanup(func() { closeHoldOnce.Do(func() { close(releaseHold) }) })

	up := newSpyUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		select {
		case enteredUpstream <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			return
		case <-releaseHold:
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_nat_bdg","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`))
	})

	wasmBytes := buildAnthropicNativeTestWasm()
	wasmSum := sha256.Sum256(wasmBytes)
	wasmHex := hex.EncodeToString(wasmSum[:])
	manifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/anthropic-nat-bdg"
  name: "Anthropic Native Bdg Wasm"
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
    adapterSlug: "anthropic-nat-bdg"
    supportedDialects: ["anthropic"]
    allowedNativePaths:
      - "^v1/messages(/count_tokens)?$"
    allowedNativeHeaders:
      - "anthropic-version"
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
	extDir := filepath.Join(tmp, "ext-nat-bdg")
	_ = os.MkdirAll(filepath.Join(extDir, "dist"), 0o755)
	_ = os.WriteFile(filepath.Join(extDir, "manifest.yaml"), []byte(manifest), 0o600)
	_ = os.WriteFile(filepath.Join(extDir, "dist", "adapter.wasm"), wasmBytes, 0o600)
	tpkgPath := filepath.Join(tmp, "nat-bdg.tpkg")
	packRes, _ := testharness.PackExtension(ctx, extDir, tpkgPath)
	sig := ed25519.Sign(pubPriv, installer.SignatureSigningPayload("@acme/anthropic-nat-bdg", "1.0.0", packRes.SHA256))
	staged, _ := instSvc.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchivePath: tpkgPath, Source: installer.SourceSpec{Kind: installer.SourceKindLocalArchive, URI: "./nat-bdg.tpkg", ExpectedSHA256: packRes.SHA256},
		Signature: installer.ProvenanceSignature{PublisherOrRootSig: sig}, ActorID: "prc_op_natbdg", TargetOrgID: org.ID,
	})
	rec, _ := instSvc.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID: staged.Installation.InstallationID, ExpectedVersion: staged.Installation.Version, ApproverPrincipalID: "prc_op_natbdg", ActivateImmediately: true, OrgID: org.ID,
	})
	adp, _ := catRepo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{InstallationID: rec.InstallationID, AdapterSlug: "anthropic-nat-bdg", SupportedDialects: []string{"anthropic"}, AllowedHosts: []string{"127.0.0.1"}})
	secMeta, _ := vStore.SealSecret(ctx, vault.SealSecretParams{OrgID: org.ID, Name: "nat-bdg-sec", SecretKind: vault.SecretKindAPIKey, Plaintext: []byte(canary.DefaultCanarySecret())})
	conn, _ := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
		OrgID: org.ID, AdapterID: adp.ID, Name: "conn-nat-bdg", EndpointBaseURL: up.srv.URL, AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, DataRegion: "us",
		BillingMode: catalog.BillingModeCommercialAPI, AuthMode: catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind}},
	})
	conn, _ = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "val")
	_, _ = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "act")
	lm, _ := catRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{OrgID: &org.ID, CanonicalSlug: "claude-sonnet-4-5", ModalityKind: catalog.ModalityChat, ContextWindow: 128000, MaxOutputTokens: 8192})
	depl, _ := catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{OrgID: org.ID, ConnectionID: conn.ID, LogicalModelID: lm.ID, UpstreamModelName: "claude-sonnet-4-5", AssuranceLevel: catalog.AssuranceTested, State: catalog.DeployStateActiveHealthy, ConnectTimeoutMs: 5000, FirstByteTimeoutMs: 5000, IdleChunkTimeoutMs: 5000})
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE model_deployments SET pricing_nano_usd_json = '{"input_per_1m":3000000000,"output_per_1m":15000000000}', version = version + 1 WHERE id = ?`, depl.ID); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots: mgr, Broker: brk, PluginsDir: pluginsDir, Audit: auditLogger, KillSwitch: ksReg,
		BudgetLedger: lSvc, DefaultHoldOutputTokens: 64, NowMs: nowFn, SSEHeartbeatInterval: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)

	client := netguard.NewHTTPClient()
	t.Cleanup(client.CloseIdleConnections)

	// 1. Trigger Extension kill switch while request holds initial budget reservation -> canonical 503 + zero leaked reserved_hold_micros.
	type respRes struct {
		code int
		body string
		err  error
	}
	respCh := make(chan respRes, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, httpSrv.URL+"/native/anthropic/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", createdVK.SecretKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		resp, rerr := client.Do(req)
		if rerr != nil {
			respCh <- respRes{err: rerr}
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		respCh <- respRes{code: resp.StatusCode, body: string(b)}
	}()

	select {
	case <-enteredUpstream:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for native budgeted request to reach upstream")
	}

	ksExt, err := ksReg.Trigger(ctx, killswitch.TriggerParams{
		Level: killswitch.LevelExtensionInstallation, TargetID: rec.InstallationID,
		Reason: "kill switch with active budget hold", TriggeredByPrincipalID: "prc_op_natbdg",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-respCh:
		if res.err != nil || res.code != http.StatusServiceUnavailable || !strings.Contains(res.body, `"extension_quarantined"`) {
			t.Fatalf("expected 503 extension_quarantined, got code=%d body=%q err=%v", res.code, res.body, res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for kill-switch response")
	}

	// Verify zero budget hold remains after clean abort.
	var reservedHold int64
	if err := st.RawDB().QueryRowContext(ctx, `SELECT reserved_hold_micros FROM budget_ledgers WHERE id = 'bdg_nat_ks'`).Scan(&reservedHold); err != nil {
		t.Fatal(err)
	}
	if reservedHold != 0 {
		t.Fatalf("expected reserved_hold_micros == 0 after kill-switch abort, got %d", reservedHold)
	}
	if _, err := ksReg.Clear(ctx, killswitch.ClearParams{
		KillSwitchID: ksExt.ID, ExpectedVersion: ksExt.Version,
		ClearedByPrincipalID: "prc_op_natbdg", ClearReason: "restored after budget kill check",
	}); err != nil {
		t.Fatal(err)
	}

	// 2. Client context cancellation (r.Context()) cleanly aborts without leaking any budget reservation.
	reqCtx, reqCancel := context.WithCancel(context.Background())
	cancelDone := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, httpSrv.URL+"/native/anthropic/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi cancel"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", createdVK.SecretKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		resp, rerr := client.Do(req)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		cancelDone <- rerr
	}()
	select {
	case <-enteredUpstream:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second native request to reach upstream")
	}
	reqCancel()
	select {
	case <-cancelDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for client cancel request completion")
	}
	// Poll briefly until server handler deferred cleanup releases the reservation.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := st.RawDB().QueryRowContext(ctx, `SELECT reserved_hold_micros FROM budget_ledgers WHERE id = 'bdg_nat_ks'`).Scan(&reservedHold); err != nil {
			t.Fatal(err)
		}
		if reservedHold == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reservedHold != 0 {
		t.Fatalf("expected reserved_hold_micros == 0 after client cancellation, got %d", reservedHold)
	}
}

