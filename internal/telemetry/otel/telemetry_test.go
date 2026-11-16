// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package otel_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"
	"gopkg.in/yaml.v3"

	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/telemetry/otel"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func random32Key(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, vault.KeySizeBytes)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return k
}

func TestMetricLabelAllowlistNEG1AndTenantToggle(t *testing.T) {
	reg, err := otel.NewMetricsRegistry(otel.MetricsConfig{})
	if err != nil {
		t.Fatalf("NewMetricsRegistry: %v", err)
	}

	// NEG-1: Reject banned high-cardinality labels.
	for _, badLabel := range []string{"request_id", "virtual_key_id", "user_id", "prompt_hash", "email", "url", "prompt"} {
		if err := otel.ValidateMetricLabels("org_id", badLabel); !errors.Is(err, otel.ErrDisallowedMetricLabel) {
			t.Fatalf("expected ErrDisallowedMetricLabel for %q, got %v", badLabel, err)
		}
		if _, err := reg.RegisterCustomCounter("tanpopo_bad_custom_total", "bad", badLabel); !errors.Is(err, otel.ErrDisallowedMetricLabel) {
			t.Fatalf("expected RegisterCustomCounter rejection for %q, got %v", badLabel, err)
		}
	}

	// Allowed labels succeed.
	if err := otel.ValidateMetricLabels("org_id", "project_id", "adapter_id", "logical_model", "status_class", "error_code", "accuracy_tier"); err != nil {
		t.Fatalf("expected allowed labels to pass: %v", err)
	}
	if _, err := reg.RegisterCustomCounter("tanpopo_good_custom_total", "good", "org_id", "logical_model"); err != nil {
		t.Fatalf("expected valid custom counter to succeed: %v", err)
	}

	// OPS-AC-12 cluster toggle: IncludeTenantLabels = false removes org_id/project_id from exposition.
	f := false
	noTenantReg, err := otel.NewMetricsRegistry(otel.MetricsConfig{IncludeTenantLabels: &f})
	if err != nil {
		t.Fatalf("NewMetricsRegistry(IncludeTenantLabels=false): %v", err)
	}
	noTenantReg.RecordHTTPRequest("unified_http", "org_secret", "prj_secret", "gpt-4o", 200, "")
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	noTenantReg.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /metrics, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `org_id="`) || strings.Contains(body, `project_id="`) {
		t.Fatalf("expected org_id and project_id omitted when IncludeTenantLabels=false, got:\n%s", body)
	}
}

func TestOPSAC12BoundedSeriesAcross100kUniqueRequests(t *testing.T) {
	reg, err := otel.NewMetricsRegistry(otel.MetricsConfig{
		ScrapeBearerToken: "scrape-secret-tok",
	})
	if err != nil {
		t.Fatalf("NewMetricsRegistry: %v", err)
	}

	// Emit 100,000 events across 100,000 simulated unique request_id/virtual_key_id combinations
	// against bounded tenant/model/status dimensions and prove series count plateaus completely flat.
	for i := 0; i < 10000; i++ {
		reg.RecordHTTPRequest("unified_http", "org_1", "prj_1", "gpt-4o", 200, "")
		reg.RecordProxyOverhead("unified_http", "wasm", 250*time.Microsecond)
		reg.RecordUpstreamCall("adp_openai", "gpt-4o", "US", 200, "total", 20*time.Millisecond)
		reg.RecordUsageSettlement("org_1", "prj_1", "gpt-4o", "EXACT")
	}
	baseCount, err := reg.TimeSeriesCount()
	if err != nil || baseCount == 0 {
		t.Fatalf("unexpected base TimeSeriesCount=%d err=%v", baseCount, err)
	}

	for i := 10000; i < 100000; i++ {
		reg.RecordHTTPRequest("unified_http", "org_1", "prj_1", "gpt-4o", 200, "")
		reg.RecordProxyOverhead("unified_http", "wasm", 300*time.Microsecond)
		reg.RecordUpstreamCall("adp_openai", "gpt-4o", "US", 200, "total", 25*time.Millisecond)
		reg.RecordUsageSettlement("org_1", "prj_1", "gpt-4o", "EXACT")
	}
	after100kCount, err := reg.TimeSeriesCount()
	if err != nil {
		t.Fatalf("TimeSeriesCount after 100k: %v", err)
	}
	if after100kCount != baseCount {
		t.Fatalf("metric series grew with request volume: base=%d after100k=%d", baseCount, after100kCount)
	}

	// Verify /metrics bearer token guard + Prometheus text output.
	unauthRec := httptest.NewRecorder()
	reg.ServeHTTP(unauthRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", unauthRec.Code)
	}

	badRec := httptest.NewRecorder()
	badReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	badReq.Header.Set("Authorization", "Bearer wrong")
	reg.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with bad token, got %d", badRec.Code)
	}

	postRec := httptest.NewRecorder()
	reg.ServeHTTP(postRec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if postRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 on POST /metrics, got %d", postRec.Code)
	}

	okRec := httptest.NewRecorder()
	okReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	okReq.Header.Set("Authorization", "Bearer scrape-secret-tok")
	reg.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on authenticated /metrics scrape, got %d", okRec.Code)
	}
	if !strings.Contains(okRec.Body.String(), otel.MetricHTTPRequestsTotal) || !strings.Contains(okRec.Body.String(), "tanpopo_http_requests_total") {
		t.Fatalf("expected canonical counter in /metrics output:\n%s", okRec.Body.String())
	}
}

func TestDebugCaptureTTLNEG2AndProjectDEKBlobRoundtrip(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})

	ctx := context.Background()
	fc := clock.NewFakeClock(time.UnixMilli(1760000000000))
	nowFn := func() int64 { return fc.Now().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "otel_debug.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	kek, err := vault.NewStaticKEKProvider("kek_v1", random32Key(t))
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	t.Cleanup(kek.Close)

	vs, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           kek,
		NowMs:         nowFn,
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	t.Cleanup(vs.Close)

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "dbg-org", DisplayName: "Dbg Org"})
	if err != nil {
		t.Fatal(err)
	}
	prj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "dbg-prj", DisplayName: "Dbg Prj"})
	if err != nil {
		t.Fatal(err)
	}

	var logBuf bytes.Buffer
	pipe, err := otel.NewPipeline(otel.PipelineConfig{
		DB:     st.RawDB(),
		Vault:  vs,
		LogOut: &logBuf,
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer func() { _ = pipe.Shutdown(context.Background()) }()

	// NEG-2: ttl_hours = 168 rejected with HTTP 422 debug_capture_ttl_exceeds_72h.
	if _, err := pipe.CreateDebugCaptureSession("sess_bad", org.ID, prj.ID, "", 168); err == nil {
		t.Fatalf("expected NEG-2 rejection for 168h TTL")
	} else {
		var dce *otel.DebugCaptureError
		if !errors.As(err, &dce) || dce.HTTPStatus != http.StatusUnprocessableEntity || dce.Code != "debug_capture_ttl_exceeds_72h" {
			t.Fatalf("expected 422 debug_capture_ttl_exceeds_72h, got %v", err)
		}
	}

	// Default (no active session) persists 0 bytes.
	stored, err := pipe.StoreDebugBlobIfAuthorized(ctx, org.ID, prj.ID, "vk_1", "req_no_sess", []byte(`{"messages":[{"content":"secret"}]}`))
	if err != nil || stored {
		t.Fatalf("expected stored=false without active debug session, got stored=%v err=%v", stored, err)
	}

	// Activate valid 24h session -> blob sealed under Project DEK, capped <= 256 KiB, credentials scrubbed.
	if _, err := pipe.CreateDebugCaptureSession("sess_ok", org.ID, prj.ID, "vk_1", 24); err != nil {
		t.Fatalf("CreateDebugCaptureSession 24h: %v", err)
	}
	// Duplicate active session ID must be rejected cleanly with 409.
	if _, err := pipe.CreateDebugCaptureSession("sess_ok", org.ID, prj.ID, "vk_1", 24); !errors.Is(err, otel.ErrDuplicateDebugCaptureSession) {
		t.Fatalf("expected ErrDuplicateDebugCaptureSession on active duplicate, got %v", err)
	}

	oversized := strings.Repeat("x", 300*1024)
	canarySk := "sk-tp-" + "canary-998877"
	rawPayload := fmt.Sprintf(`{"api_key":%q,"azure_api_key":"azure-nonprefixed-secret","messages":[{"role":"user","content":"debug context"}],"body":%q}`, canarySk, oversized)
	stored, err = pipe.StoreDebugBlobIfAuthorized(ctx, org.ID, prj.ID, "vk_1", "req_with_sess", []byte(rawPayload))
	if err != nil || !stored {
		t.Fatalf("StoreDebugBlobIfAuthorized: stored=%v err=%v", stored, err)
	}

	var nonce, ct []byte
	var expMs, crMs int64
	if err := st.RawDB().QueryRowContext(ctx, `
		SELECT nonce, encrypted_payload_aead, created_at_ms, expires_at_ms
		FROM debug_payload_blobs WHERE request_id = 'req_with_sess'`).Scan(&nonce, &ct, &crMs, &expMs); err != nil {
		t.Fatalf("query stored debug blob: %v", err)
	}
	if len(ct) > otel.MaxDebugBlobCiphertextBytes {
		t.Fatalf("ciphertext exceeded 256 KiB cap: %d bytes", len(ct))
	}
	if expMs-crMs != 24*int64(time.Hour/time.Millisecond) {
		t.Fatalf("unexpected expiry delta: %d", expMs-crMs)
	}

	pt, err := vs.UnsealProjectBlob(ctx, org.ID, prj.ID, vault.RecordKindDebugBlob, "req_with_sess", nonce, ct)
	if err != nil {
		t.Fatalf("UnsealProjectBlob: %v", err)
	}
	for _, forbidden := range []string{canarySk, "azure-nonprefixed-secret"} {
		if strings.Contains(string(pt), forbidden) {
			t.Fatalf("debug blob plaintext leaked secret %q even after Redactor scrubbing", forbidden)
		}
	}

	// Once expired, session ID is pruned and can be recreated; invalid OTLP URLs fail closed.
	fc.Advance(25 * time.Hour)
	if _, err := pipe.CreateDebugCaptureSession("sess_ok", org.ID, prj.ID, "vk_1", 1); err != nil {
		t.Fatalf("expected expired session ID to be pruned and reusable, got %v", err)
	}
	for _, badEndpoint := range []string{
		"ftp://collector.local/v1/traces",
		"file:///tmp/traces",
		"https://alice:hunter2@collector.local/v1/traces",
	} {
		if _, perr := otel.NewPipeline(otel.PipelineConfig{OTLPEndpoint: badEndpoint}); perr == nil {
			t.Fatalf("expected NewPipeline to reject unsafe OTLP endpoint %q", badEndpoint)
		}
	}

	// Shredding Project DEK renders debug_payload_blobs permanently unreadable.
	if err := vs.ShredProjectDEK(ctx, org.ID, prj.ID, vault.ShredRequest{RequestID: "shred_dbg_1", ExpectedGeneration: 1}); err != nil {
		t.Fatalf("ShredProjectDEK: %v", err)
	}
	if _, err := vs.UnsealProjectBlob(ctx, org.ID, prj.ID, vault.RecordKindDebugBlob, "req_with_sess", nonce, ct); err == nil {
		t.Fatalf("expected UnsealProjectBlob failure after Project DEK crypto-shredding")
	}
}

func TestRingBufferDropOldestAgainstDeadBlackholeCollector(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})

	// Deliberately blackhole TCP connections (Accept and hold open without replying).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		var held []net.Conn
		defer func() {
			for _, c := range held {
				_ = c.Close()
			}
		}()
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			held = append(held, c)
		}
	}()
	defer func() {
		_ = ln.Close()
		<-acceptDone
	}()

	const maxBuf = 64 * 1024
	pipe, err := otel.NewPipeline(otel.PipelineConfig{
		OTLPEndpoint:       "http://" + ln.Addr().String() + "/v1/traces",
		OTLPAllowPrivateIP: true,
		OTLPAllowedCIDRs:   []string{"127.0.0.1/32"},
		OTLPControlHook:    netguard.Control,
		MaxRingBufferBytes: maxBuf,
		ExportTimeout:      150 * time.Millisecond,
		FlushInterval:      10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}

	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 600; i++ {
		sCtx, root, _ := pipe.StartRequestSpan(ctx, "", fmt.Sprintf("req_bh_%d", i), "unified_http")
		cCtx, child := pipe.StartStageSpan(sCtx, "Egress")
		_ = cCtx
		pipe.EndSpan(child, nil)
		pipe.EndSpan(root, nil)
	}
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("non-blocking ring buffer stalled under blackhole OTLP collector: %v", elapsed)
	}

	_, peakBytes, droppedSpans := pipe.Ring.BufferStats()
	if peakBytes > maxBuf {
		t.Fatalf("ring buffer exceeded cap: peak=%d max=%d", peakBytes, maxBuf)
	}
	if droppedSpans == 0 || pipe.Metrics.DroppedCount("traces") == 0 {
		t.Fatalf("expected non-zero dropped telemetry count: ringDropped=%d metricsDropped=%d",
			droppedSpans, pipe.Metrics.DroppedCount("traces"))
	}

	if err := pipe.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestAlertsYAMLCrossLinkedToAllRunbooks(t *testing.T) {
	alertsPath := filepath.Join("..", "..", "..", "deploy", "monitoring", "alerts.yaml")
	raw, err := os.ReadFile(alertsPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", alertsPath, err)
	}

	var doc struct {
		Groups []struct {
			Name  string `yaml:"name"`
			Rules []struct {
				Alert       string            `yaml:"alert"`
				Expr        string            `yaml:"expr"`
				Labels      map[string]string `yaml:"labels"`
				Annotations map[string]string `yaml:"annotations"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("yaml.Unmarshal alerts.yaml: %v", err)
	}

	expectedAlerts := map[string]bool{
		"ALT-GATEWAY-BURN":   false,
		"ALT-LATENCY-SLO":    false,
		"ALT-REVOCATION-LAG": false,
		"ALT-AUDIT-TAMPER":   false,
		"ALT-PLUGIN-TRAP":    false,
		"ALT-BUDGET-80":      false,
		"ALT-OAUTH-EXPIRY":   false,
		"ALT-UNMETERED-SPEND": false,
		"ALT-TELEMETRY-DROP": false,
	}
	seenRunbooks := make(map[string]bool)

	for _, g := range doc.Groups {
		for _, r := range g.Rules {
			if _, ok := expectedAlerts[r.Alert]; ok {
				expectedAlerts[r.Alert] = true
			}
			if strings.TrimSpace(r.Expr) == "" {
				t.Fatalf("alert %q has empty expr", r.Alert)
			}
			rb := r.Labels["runbook"]
			if !strings.HasPrefix(rb, "INC-") {
				t.Fatalf("alert %q missing INC-xx runbook label: %+v", r.Alert, r.Labels)
			}
			seenRunbooks[rb] = true
			if !strings.Contains(r.Annotations["runbook_ref"], "docs/operations/monitoring-incidents.md#") {
				t.Fatalf("alert %q missing runbook_ref annotation: %+v", r.Alert, r.Annotations)
			}
		}
	}

	for name, seen := range expectedAlerts {
		if !seen {
			t.Fatalf("missing canonical alert %q in deploy/monitoring/alerts.yaml", name)
		}
	}
	for i := 1; i <= 7; i++ {
		id := fmt.Sprintf("INC-%02d", i)
		if !seenRunbooks[id] {
			t.Fatalf("expected alert cross-link for runbook %s", id)
		}
	}
}
