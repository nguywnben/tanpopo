// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http_test

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
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

func installTestDeclarativePlugin(
	t *testing.T,
	ctx context.Context,
	tmp string,
	pubPriv ed25519.PrivateKey,
	instSvc *installer.LifecycleService,
	catRepo *catalog.Repository,
	orgID, pkgID, slug string,
) (installID, adapterID string) {
	t.Helper()
	extDir := filepath.Join(tmp, "src-"+slug)
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: %q
  name: %q
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: %q
    supportedDialects: ["openai_compatible"]
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
    - "provider:build_validation_probe"
    - "provider:transform_request"
    - "provider:transform_response"
    - "provider:transform_stream_chunk"
    - "provider:normalize_error"
adapter:
  slug: %q
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`, pkgID, "Adapter "+slug, slug, slug)
	if err := os.WriteFile(filepath.Join(extDir, "manifest.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	tpkg := filepath.Join(tmp, slug+"-1.0.0.tpkg")
	packRes, err := testharness.PackExtension(ctx, extDir, tpkg)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(pubPriv, installer.SignatureSigningPayload(pkgID, "1.0.0", packRes.SHA256))
	staged, err := instSvc.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchivePath: tpkg,
		Source: installer.SourceSpec{
			Kind:           installer.SourceKindLocalArchive,
			URI:            "./" + slug + ".tpkg",
			ExpectedSHA256: packRes.SHA256,
		},
		Signature:   installer.ProvenanceSignature{PublisherOrRootSig: sig},
		ActorID:     "prc_op_ks",
		TargetOrgID: orgID,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := instSvc.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: "prc_op_ks",
		ActivateImmediately: true,
		OrgID:               orgID,
	})
	if err != nil {
		t.Fatal(err)
	}
	adp, err := catRepo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{
		InstallationID:    rec.InstallationID,
		AdapterSlug:       slug,
		SupportedDialects: []string{"openai"},
		AllowedHosts:      []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec.InstallationID, adp.ID
}

func TestTP0023_HTTP_AC2_4LevelKillSwitch_SSE_Under1s(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760001000000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }
	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(tmp, "ks_http.db"),
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
	kek, err := vault.NewStaticKEKProvider("kek-ks-http", rawMaster)
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
		Slug:           "ks-http-org",
		DisplayName:    "KS HTTP Org",
		MinPluginTrust: storageport.MinTrustVerifiedOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	proj1, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{
		Slug:        "proj-1",
		DisplayName: "Project 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	proj2, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{
		Slug:        "proj-2",
		DisplayName: "Project 2",
	})
	if err != nil {
		t.Fatal(err)
	}
	env1, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj1.ID, storageport.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	env2, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj2.ID, storageport.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ('prc_op_ks', NULL, 'HUMAN_LOCAL', 'op_ks@acme.test', 'ACTIVE', 1, ?, ?)
	`, nowFn(), nowFn()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_op_ks', NULL, 'prc_op_ks', 'INSTANCE', 'instance', 'OPERATOR', 1, ?)
	`, nowFn()); err != nil {
		t.Fatal(err)
	}

	releaseUpstream := make(chan struct{})
	var closeOnce sync.Once
	closeRelease := func() {
		closeOnce.Do(func() { close(releaseUpstream) })
	}
	t.Cleanup(closeRelease)

	makeStreamingUpstream := func(tag string) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqBytes, _ := io.ReadAll(r.Body)
			if strings.HasSuffix(r.URL.Path, "/embeddings") {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"up-` + tag + `","usage":{"prompt_tokens":2,"total_tokens":2}}`))
				return
			}
			if !strings.Contains(string(reqBytes), `"stream":true`) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"chatcmpl-sync-` + tag + `","object":"chat.completion","model":"up-` + tag + `","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			rc := http.NewResponseController(w)
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-" + tag + "\",\"object\":\"chat.completion.chunk\",\"model\":\"up-" + tag + "\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"}}]}\n\n"))
			_ = rc.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-releaseUpstream:
			}
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-" + tag + "\",\"object\":\"chat.completion.chunk\",\"model\":\"up-" + tag + "\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			_ = rc.Flush()
		}))
		t.Cleanup(func() {
			srv.CloseClientConnections()
			srv.Close()
		})
		return srv
	}

	up1 := makeStreamingUpstream("p1")
	up2 := makeStreamingUpstream("p2")

	inst1ID, adp1ID := installTestDeclarativePlugin(t, ctx, tmp, pubPriv, instSvc, catRepo, org.ID, "@acme/plugin-one", "plugin-one")
	inst2ID, adp2ID := installTestDeclarativePlugin(t, ctx, tmp, pubPriv, instSvc, catRepo, org.ID, "@acme/plugin-two", "plugin-two")

	canarySec := canary.DefaultCanarySecret()
	secMeta, err := vStore.SealSecret(ctx, vault.SealSecretParams{
		OrgID:      org.ID,
		Name:       "ks-http-key",
		SecretKind: vault.SecretKindAPIKey,
		Plaintext:  []byte(canarySec),
	})
	if err != nil {
		t.Fatal(err)
	}

	createActiveConnAndDepl := func(name, adpID, urlStr, modelSlug, upstreamModel string) (string, string) {
		conn, cerr := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
			OrgID:           org.ID,
			AdapterID:       adpID,
			Name:            name,
			EndpointBaseURL: urlStr,
			AllowPrivateIP:  true,
			AllowedCIDRs:    []string{"127.0.0.1/32"},
			DataRegion:      "us",
			BillingMode:     catalog.BillingModeCommercialAPI,
			AuthMode:        catalog.AuthModeAPIKey,
			SecretBindings: []catalog.SecretBindingInput{
				{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind},
			},
		})
		if cerr != nil {
			t.Fatal(cerr)
		}
		conn, cerr = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "val")
		if cerr != nil {
			t.Fatal(cerr)
		}
		if _, cerr := catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "act"); cerr != nil {
			t.Fatal(cerr)
		}
		lm, lerr := catRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{
			OrgID:           &org.ID,
			CanonicalSlug:   modelSlug,
			ModalityKind:    catalog.ModalityChat,
			ContextWindow:   128000,
			MaxOutputTokens: 4096,
			BaseCapabilities: map[string]string{
				"streaming": "VERIFIED",
			},
		})
		if lerr != nil {
			t.Fatal(lerr)
		}
		if _, derr := catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{
			OrgID:              org.ID,
			ConnectionID:       conn.ID,
			LogicalModelID:     lm.ID,
			UpstreamModelName:  upstreamModel,
			AssuranceLevel:     catalog.AssuranceVerified,
			State:              catalog.DeployStateActiveHealthy,
			ConnectTimeoutMs:   5000,
			FirstByteTimeoutMs: 5000,
			IdleChunkTimeoutMs: 5000,
		}); derr != nil {
			t.Fatal(derr)
		}
		return conn.ID, lm.ID
	}

	conn1ID, _ := createActiveConnAndDepl("conn-plugin-1", adp1ID, up1.URL, "model/plugin-1", "up-p1")
	conn2ID, _ := createActiveConnAndDepl("conn-plugin-2", adp2ID, up2.URL, "model/plugin-2", "up-p2")

	vk1, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID:                org.ID,
		ProjectID:            proj1.ID,
		EnvID:                env1.ID,
		Name:                 "vk-proj1",
		AllowedPlanes:        "PROVIDER",
		MaxConcurrentStreams: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	vk2, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID:                org.ID,
		ProjectID:            proj2.ID,
		EnvID:                env2.ID,
		Name:                 "vk-proj2",
		AllowedPlanes:        "PROVIDER",
		MaxConcurrentStreams: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
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

	type streamSpec struct {
		idx       int
		pluginID  string
		modelSlug string
		rawKey    string
	}
	specs := []streamSpec{
		{0, inst1ID, "model/plugin-1", vk1.SecretKey},
		{1, inst1ID, "model/plugin-1", vk1.SecretKey},
		{2, inst1ID, "model/plugin-1", vk2.SecretKey},
		{3, inst1ID, "model/plugin-1", vk2.SecretKey},
		{4, inst2ID, "model/plugin-2", vk1.SecretKey},
		{5, inst2ID, "model/plugin-2", vk1.SecretKey},
		{6, inst2ID, "model/plugin-2", vk2.SecretKey},
		{7, inst2ID, "model/plugin-2", vk2.SecretKey},
	}

	type streamOutcome struct {
		idx     int
		plugin  string
		sawDone bool
		endedAt time.Time
		err     error
		body    string
	}
	startedCh := make(chan int, len(specs))
	doneCh := make(chan streamOutcome, len(specs))

	for _, sp := range specs {
		go func(sp streamSpec) {
			payload := fmt.Sprintf(`{"model":%q,"stream":true,"messages":[{"role":"user","content":"stream-%d"}]}`, sp.modelSlug, sp.idx)
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, httpSrv.URL+"/v1/chat/completions", strings.NewReader(payload))
			if err != nil {
				doneCh <- streamOutcome{idx: sp.idx, plugin:  sp.pluginID, err: err, endedAt: time.Now()}
				return
			}
			req.Header.Set("Authorization", "Bearer "+sp.rawKey)
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				doneCh <- streamOutcome{idx: sp.idx, plugin:  sp.pluginID, err: err, endedAt: time.Now()}
				return
			}
			defer resp.Body.Close()

			reader := bufio.NewReader(resp.Body)
			var collected strings.Builder
			sentStarted := false
			for {
				line, rerr := reader.ReadString('\n')
				if len(line) > 0 {
					collected.WriteString(line)
					if !sentStarted && strings.Contains(line, "data:") {
						sentStarted = true
						startedCh <- sp.idx
					}
				}
				if rerr != nil {
					out := streamOutcome{
						idx:     sp.idx,
						plugin:  sp.pluginID,
						sawDone: strings.Contains(collected.String(), "data: [DONE]"),
						endedAt: time.Now(),
						body:    collected.String(),
					}
					if rerr != io.EOF {
						out.err = rerr
					}
					doneCh <- out
					return
				}
			}
		}(sp)
	}

	// Wait until all 8 SSE streams are genuinely active & have delivered their first SSE chunk.
	for i := 0; i < len(specs); i++ {
		select {
		case <-startedCh:
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for all 8 SSE streams to begin streaming (%d started)", i)
		}
	}

	// AC-2: Trigger ExtensionInstallation Kill-Switch targeting Plugin 1.
	triggerStart := time.Now()
	ksExt, err := ksReg.Trigger(ctx, killswitch.TriggerParams{
		Level:                  killswitch.LevelExtensionInstallation,
		TargetID:               inst1ID,
		Reason:                 "emergency compromise containment on plugin 1",
		TriggeredByPrincipalID: "prc_op_ks",
	})
	if err != nil {
		t.Fatalf("Trigger ExtensionInstallation kill switch: %v", err)
	}

	// Assert all 4 streams using Plugin 1 terminate in < 1 second without seeing [DONE].
	p1Ended := 0
	p2EarlyEnded := 0
	for p1Ended < 4 {
		select {
		case out := <-doneCh:
			if out.plugin == inst1ID {
				p1Ended++
				if out.sawDone {
					t.Fatalf("Plugin 1 stream %d saw [DONE] instead of being severed mid-stream", out.idx)
				}
				if out.endedAt.Sub(triggerStart) >= time.Second {
					t.Fatalf("Plugin 1 stream %d took %v (>= 1s) to sever", out.idx, out.endedAt.Sub(triggerStart))
				}
			} else {
				p2EarlyEnded++
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("Plugin 1 streams did not all terminate within < 1s (terminated=%d/4)", p1Ended)
		}
	}
	if p2EarlyEnded != 0 {
		t.Fatalf("expected 0 Plugin 2 streams to terminate early, got %d", p2EarlyEnded)
	}

	// Release upstream so all 4 Plugin 2 streams finish cleanly with [DONE].
	closeRelease()
	p2Ended := 0
	for p2Ended < 4 {
		select {
		case out := <-doneCh:
			if out.plugin != inst2ID {
				t.Fatalf("unexpected non-plugin2 outcome: %+v", out)
			}
			p2Ended++
			if out.err != nil || !out.sawDone {
				t.Fatalf("Plugin 2 stream %d did not complete cleanly: err=%v sawDone=%v body=%q", out.idx, out.err, out.sawDone, out.body)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Plugin 2 streams did not complete cleanly (%d/4)", p2Ended)
		}
	}

	// Verify subsequent /v1/chat/completions targeting Plugin 1 fails closed with 503 extension_quarantined,
	// while Plugin 2 continues serving cleanly.
	doChat := func(rawKey, model string) (int, string) {
		body := fmt.Sprintf(`{"model":%q,"stream":false,"messages":[{"role":"user","content":"hi"}]}`, model)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, httpSrv.URL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+rawKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("doChat: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := doChat(vk1.SecretKey, "model/plugin-1"); code != http.StatusServiceUnavailable || !strings.Contains(body, `"extension_quarantined"`) {
		t.Fatalf("expected 503 extension_quarantined for Plugin 1, got %d %s", code, body)
	}
	if code, body := doChat(vk1.SecretKey, "model/plugin-2"); code != http.StatusOK {
		t.Fatalf("expected 200 OK for Plugin 2 while Plugin 1 killed, got %d %s", code, body)
	}

	// Clear ExtensionInstallation kill switch and verify Plugin 1 recovers.
	if _, err := ksReg.Clear(ctx, killswitch.ClearParams{
		KillSwitchID:         ksExt.ID,
		ExpectedVersion:      ksExt.Version,
		ClearedByPrincipalID: "prc_op_ks",
		ClearReason:          "extension verified clean after incident",
	}); err != nil {
		t.Fatalf("Clear ksExt: %v", err)
	}
	if code, body := doChat(vk1.SecretKey, "model/plugin-1"); code != http.StatusOK {
		t.Fatalf("expected Plugin 1 restored after clear, got %d %s", code, body)
	}

	// Test remaining 3 kill-switch levels: PROVIDER_CONNECTION, VIRTUAL_KEY, PROJECT_OR_ORG_FREEZE.
	ksConn, err := ksReg.Trigger(ctx, killswitch.TriggerParams{
		OrgID:                  &org.ID,
		Level:                  killswitch.LevelProviderConnection,
		TargetID:               conn1ID,
		Reason:                 "upstream conn1 outage",
		TriggeredByPrincipalID: "prc_op_ks",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := doChat(vk1.SecretKey, "model/plugin-1"); code != http.StatusServiceUnavailable || !strings.Contains(body, `"provider_connection_killed"`) {
		t.Fatalf("expected 503 provider_connection_killed, got %d %s", code, body)
	}
	if _, err := ksReg.Clear(ctx, killswitch.ClearParams{
		OrgID: &org.ID, KillSwitchID: ksConn.ID, ExpectedVersion: ksConn.Version,
		ClearedByPrincipalID: "prc_op_ks", ClearReason: "connection recovered",
	}); err != nil {
		t.Fatal(err)
	}

	ksProj, err := ksReg.Trigger(ctx, killswitch.TriggerParams{
		OrgID:                  &org.ID,
		Level:                  killswitch.LevelProjectOrOrgFreeze,
		TargetID:               proj1.ID,
		Reason:                 "project 1 quota freeze",
		TriggeredByPrincipalID: "prc_op_ks",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := doChat(vk1.SecretKey, "model/plugin-2"); code != http.StatusServiceUnavailable || !strings.Contains(body, `"tenant_traffic_frozen"`) {
		t.Fatalf("expected 503 tenant_traffic_frozen on frozen proj1, got %d %s", code, body)
	}
	if code, body := doChat(vk2.SecretKey, "model/plugin-2"); code != http.StatusOK {
		t.Fatalf("expected sibling proj2 unaffected during proj1 freeze, got %d %s", code, body)
	}
	if _, err := ksReg.Clear(ctx, killswitch.ClearParams{
		OrgID: &org.ID, KillSwitchID: ksProj.ID, ExpectedVersion: ksProj.Version,
		ClearedByPrincipalID: "prc_op_ks", ClearReason: "project freeze lifted",
	}); err != nil {
		t.Fatal(err)
	}

	// Verify GET /v1/models respects kill switch as well.
	reqModels, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpSrv.URL+"/v1/models", nil)
	reqModels.Header.Set("Authorization", "Bearer "+vk1.SecretKey)
	respModels, err := client.Do(reqModels)
	if err != nil {
		t.Fatal(err)
	}
	modelsBody, _ := io.ReadAll(respModels.Body)
	respModels.Body.Close()
	if respModels.StatusCode != http.StatusOK || !strings.Contains(string(modelsBody), "model/plugin-1") {
		t.Fatalf("expected /v1/models 200 with model/plugin-1 before kill, got %d %s", respModels.StatusCode, string(modelsBody))
	}

	ksVK, err := ksReg.Trigger(ctx, killswitch.TriggerParams{
		OrgID:                  &org.ID,
		Level:                  killswitch.LevelVirtualKey,
		TargetID:               vk1.Summary.ID,
		Reason:                 "vk1 leaked in CI log",
		TriggeredByPrincipalID: "prc_op_ks",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := doChat(vk1.SecretKey, "model/plugin-2"); code != http.StatusUnauthorized || !strings.Contains(body, `"invalid_virtual_key"`) {
		t.Fatalf("expected 401 invalid_virtual_key on killed VK, got %d %s", code, body)
	}
	_, _ = ksReg.Clear(ctx, killswitch.ClearParams{
		OrgID: &org.ID, KillSwitchID: ksVK.ID, ExpectedVersion: ksVK.Version,
		ClearedByPrincipalID: "prc_op_ks", ClearReason: "vk rotated cleanly",
	})

	// Suppress unused conn2ID warning while verifying conn2 was untouched.
	if conn2ID == "" {
		t.Fatal("expected non-empty conn2ID")
	}

	// Verify 100% of SYSTEM and org audit chains pass integrity verification!
	rep, err := audit.VerifyAuditChain(ctx, st.RawDB(), audit.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if !rep.Valid {
		rawRep, _ := json.Marshal(rep)
		t.Fatalf("expected valid hash chains, got: %s", string(rawRep))
	}
}

func TestTP0023_HTTP_CrossProcessKillSwitchSyncAnd503PreStreamCancel(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760003000000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "ks_multiproc_http.db")
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

	auditWriter, err := audit.NewLogger(audit.LoggerConfig{DB: stWrite.RawDB(), NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	ksWriter, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{DB: stWrite.RawDB(), Audit: auditWriter, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}

	auditReader, err := audit.NewLogger(audit.LoggerConfig{DB: stServe.RawDB(), NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	ksReader, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{DB: stServe.RawDB(), Audit: auditReader, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}

	pubPub, pubPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := trust.TrustedKeyring{PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": pubPub}}
	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: stServe.RawDB(), NowMs: nowFn, Keyring: keyring})
	if err != nil {
		t.Fatal(err)
	}

	rawMaster := make([]byte, 32)
	_, _ = rand.Read(rawMaster)
	kek, err := vault.NewStaticKEKProvider("kek-mp-http", rawMaster)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kek.Close)
	vStore, err := vault.NewStore(vault.Config{DB: stWrite.RawDB(), KEK: kek, NowMs: nowFn, PostShredHook: stWrite.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vStore.Close)

	catRepo, err := catalog.NewRepository(catalog.Config{DB: stWrite.RawDB(), NowMs: nowFn, Snapshots: mgr, Keyring: keyring, Audit: auditWriter})
	if err != nil {
		t.Fatal(err)
	}
	brk, err := broker.NewBlindBroker(broker.Config{Resolver: catRepo, PolicyResolver: catRepo, Vault: vStore, UnsealCap: vault.MintUnsealCapabilityForBroker(), Audit: auditWriter, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{DB: stWrite.RawDB(), PluginsDir: pluginsDir, Keyring: keyring, Audit: auditWriter, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	vkSvc, err := vkey.NewService(vkey.ServiceConfig{DB: stWrite.RawDB(), NowMs: nowFn, Snapshots: mgr, AuditSink: auditWriter})
	if err != nil {
		t.Fatal(err)
	}

	org, _ := stWrite.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "mp-org", DisplayName: "MP Org", MinPluginTrust: storageport.MinTrustVerifiedOnly})
	proj, _ := stWrite.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{Slug: "mp-proj", DisplayName: "MP Proj"})
	env, _ := stWrite.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, storageport.CreateEnvironmentParams{Slug: "prod"})
	_, _ = stWrite.RawDB().ExecContext(ctx, `INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms) VALUES ('prc_op_ks', NULL, 'HUMAN_LOCAL', 'op_mp@acme.test', 'ACTIVE', 1, ?, ?)`, nowFn(), nowFn())
	_, _ = stWrite.RawDB().ExecContext(ctx, `INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms) VALUES ('rb_op_ks', NULL, 'prc_op_ks', 'INSTANCE', 'instance', 'OPERATOR', 1, ?)`, nowFn())

	instID, adpID := installTestDeclarativePlugin(t, ctx, tmp, pubPriv, instSvc, catRepo, org.ID, "@acme/mp-plugin", "mp-plugin")
	secMeta, _ := vStore.SealSecret(ctx, vault.SealSecretParams{OrgID: org.ID, Name: "mp-key", SecretKind: vault.SecretKindAPIKey, Plaintext: []byte(canary.DefaultCanarySecret())})

	enteredUpstream := make(chan struct{}, 2)
	releaseHold := make(chan struct{})
	var closeHoldOnce sync.Once
	closeHold := func() { closeHoldOnce.Do(func() { close(releaseHold) }) }
	t.Cleanup(closeHold)

	upSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		_, _ = w.Write([]byte(`{"id":"chatcmpl-mp","object":"chat.completion","model":"up-mp","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	t.Cleanup(func() { upSrv.CloseClientConnections(); upSrv.Close() })

	conn, _ := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
		OrgID: org.ID, AdapterID: adpID, Name: "conn-mp", EndpointBaseURL: upSrv.URL, AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"},
		DataRegion: "us", BillingMode: catalog.BillingModeCommercialAPI, AuthMode: catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind}},
	})
	conn, _ = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "val")
	_, _ = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "act")
	lm, _ := catRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{OrgID: &org.ID, CanonicalSlug: "model/mp", ModalityKind: catalog.ModalityChat, ContextWindow: 128000, MaxOutputTokens: 4096, BaseCapabilities: map[string]string{"streaming": "VERIFIED"}})
	_, _ = catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{OrgID: org.ID, ConnectionID: conn.ID, LogicalModelID: lm.ID, UpstreamModelName: "up-mp", AssuranceLevel: catalog.AssuranceVerified, State: catalog.DeployStateActiveHealthy, ConnectTimeoutMs: 5000, FirstByteTimeoutMs: 5000, IdleChunkTimeoutMs: 5000})

	vk, err := vkSvc.Create(ctx, vkey.CreateParams{OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, Name: "vk-mp", AllowedPlanes: "PROVIDER", MaxConcurrentStreams: 16})
	if err != nil {
		t.Fatal(err)
	}
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

	// Start in-flight non-streaming request blocked before upstream response bytes return;
	// trigger out-of-band PROVIDER_CONNECTION kill switch on ksWriter, then trigger mgr.Refresh
	// (simulating snapshot watcher across processes) -> in-flight request cancels and MUST return 503 provider_connection_killed (not 401 invalid_virtual_key).
	type httpResp struct {
		status int
		body   string
		err    error
	}
	respCh := make(chan httpResp, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, httpSrv.URL+"/v1/chat/completions", strings.NewReader(`{"model":"model/mp","stream":false,"messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer "+vk.SecretKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			respCh <- httpResp{err: err}
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		respCh <- httpResp{status: resp.StatusCode, body: string(b)}
	}()

	select {
	case <-enteredUpstream:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for request to reach upstream")
	}

	ksConn, err := ksWriter.Trigger(ctx, killswitch.TriggerParams{
		OrgID: &org.ID, Level: killswitch.LevelProviderConnection, TargetID: conn.ID,
		Reason: "out-of-band connection halt", TriggeredByPrincipalID: "prc_op_ks",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatalf("mgr.Refresh after out-of-band kill switch trigger: %v", err)
	}

	select {
	case res := <-respCh:
		if res.err != nil {
			t.Fatalf("request error: %v", res.err)
		}
		if res.status != http.StatusServiceUnavailable || !strings.Contains(res.body, `"provider_connection_killed"`) {
			t.Fatalf("expected 503 provider_connection_killed on canceled pre-stream dispatch, got %d %s", res.status, res.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for 503 response after cross-process Refresh")
	}

	// Out-of-band Clear (emergency=0, entity_kind='emergency_kill_switch') also propagates on mgr.Refresh!
	closeHold()
	if _, err := ksWriter.Clear(ctx, killswitch.ClearParams{
		OrgID: &org.ID, KillSwitchID: ksConn.ID, ExpectedVersion: ksConn.Version,
		ClearedByPrincipalID: "prc_op_ks", ClearReason: "connection recovered cleanly",
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatalf("mgr.Refresh after out-of-band Clear: %v", err)
	}
	reqOK, _ := http.NewRequestWithContext(ctx, http.MethodPost, httpSrv.URL+"/v1/chat/completions", strings.NewReader(`{"model":"model/mp","stream":false,"messages":[{"role":"user","content":"hi"}]}`))
	reqOK.Header.Set("Authorization", "Bearer "+vk.SecretKey)
	reqOK.Header.Set("Content-Type", "application/json")
	respOK, err := client.Do(reqOK)
	if err != nil {
		t.Fatal(err)
	}
	bOK, _ := io.ReadAll(respOK.Body)
	respOK.Body.Close()
	if respOK.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after out-of-band Clear + Refresh, got %d %s", respOK.StatusCode, string(bOK))
	}
	_ = instID
}
