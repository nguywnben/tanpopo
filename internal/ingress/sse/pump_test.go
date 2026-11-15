// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package sse_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/testkit/fakellm"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func testDeclarativeOpenAIRuntime(t testing.TB) *declarative.Engine {
	t.Helper()
	yamlSpec := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: ext_sse_test
  name: Acme SSE Test
permissions:
  networkEgress:
    allowedHosts: ["127.0.0.1"]
adapter:
  slug: acme-sse
  nativePrefix: /native/acme
  baseUrl: "http://127.0.0.1"
  wireProtocol: openai_chat_v1
  endpoints:
    chat: "/v1/chat/completions"
  streamMapping:
    doneSentinel: "[DONE]"
    deltaContentPath: "choices.0.delta.content"
    deltaToolCallsPath: "choices.0.delta.tool_calls"
    finishReasonPath: "choices.0.finish_reason"
    modelPath: "model"
  usageExtraction:
    inputTokensPath: "usage.prompt_tokens"
    outputTokensPath: "usage.completion_tokens"
`
	eng, err := declarative.ParseDeclarativeManifestYAML([]byte(yamlSpec))
	if err != nil {
		t.Fatalf("ParseDeclarativeManifestYAML: %v", err)
	}
	return eng
}

func TestSSEPumpNormalStreamingAndLatencyBudget(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	srv := fakellm.NewServerWithOptions(t, fakellm.Options{StreamChunks: 8})
	u, _ := url.Parse(srv.URL())
	client, err := egress.NewGuardedHTTPClient(egress.ClientConfig{
		Policy: egress.Policy{
			AllowedHosts:   []string{u.Host},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
		},
		DisableTotalTimeout: true,
		Timeout:             5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	rt := testDeclarativeOpenAIRuntime(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upCtx, cancelUp := context.WithCancel(r.Context())
		defer cancelUp()
		upReq, _ := http.NewRequestWithContext(upCtx, http.MethodPost, srv.URL()+"/v1/chat/completions", strings.NewReader(`{"model":"fake-gpt-4o","stream":true}`))
		upReq.Header.Set("Content-Type", "application/json")
		upResp, derr := client.Do(upReq)
		if derr != nil {
			http.Error(w, derr.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = upResp.Body.Close() }()

		res, perr := sse.Pump(r.Context(), sse.PumpConfig{
			RequestID:           "req_sse_normal",
			Upstream:            upResp.Body,
			CancelUpstream:      cancelUp,
			Runtime:             rt,
			Writer:              w,
			HeartbeatInterval:   -1,
			RequireDoneSentinel: true,
		})
		if perr != nil {
			t.Errorf("unexpected Pump error: %v", perr)
		}
		if !res.StreamStarted || !res.SawDone || res.FramesEmitted < 9 {
			t.Errorf("unexpected PumpResult: %+v", res)
		}
		if res.Usage == nil || res.Usage.InputTokens != 10 || res.Usage.OutputTokens != 8 {
			t.Errorf("unexpected extracted usage: %+v", res.Usage)
		}
	})

	proxySrv := httptest.NewServer(handler)
	t.Cleanup(proxySrv.Close)

	httpClient := netguard.NewHTTPClient()
	t.Cleanup(httpClient.CloseIdleConnections)
	resp, err := httpClient.Post(proxySrv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("unexpected response status=%d ct=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "data: [DONE]\n\n") {
		t.Fatalf("expected data: [DONE] in relayed body, got %q", string(body))
	}
	if strings.Contains(string(body), "mid_stream_abort") {
		t.Fatalf("unexpected mid_stream_abort in normal stream: %q", string(body))
	}
}

func TestSSEPumpClientDisconnectPropagatesUnder500ms(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	// Dedicated trickle HTTP server guarantees steady 80ms inter-frame intervals while watching disconnect.
	disconnectCh := make(chan time.Time, 1)
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		if fl != nil {
			fl.Flush()
		}
		for i := 0; i < 40; i++ {
			select {
			case <-r.Context().Done():
				disconnectCh <- time.Now()
				return
			case <-time.After(80 * time.Millisecond):
			}
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"c-%d\"}}]}\n\n", i)
			if fl != nil {
				fl.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if fl != nil {
			fl.Flush()
		}
	}))
	t.Cleanup(upstreamSrv.Close)

	u, _ := url.Parse(upstreamSrv.URL)
	client, err := egress.NewGuardedHTTPClient(egress.ClientConfig{
		Policy: egress.Policy{
			AllowedHosts:   []string{u.Host},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
		},
		DisableTotalTimeout: true,
		Timeout:             5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	rt := testDeclarativeOpenAIRuntime(t)

	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upCtx, cancelUp := context.WithCancel(r.Context())
		defer cancelUp()
		upReq, _ := http.NewRequestWithContext(upCtx, http.MethodPost, upstreamSrv.URL+"/v1/chat/completions", nil)
		upResp, derr := client.Do(upReq)
		if derr != nil {
			return
		}
		defer func() { _ = upResp.Body.Close() }()
		_, _ = sse.Pump(r.Context(), sse.PumpConfig{
			RequestID:           "req_sse_cancel",
			Upstream:            upResp.Body,
			CancelUpstream:      cancelUp,
			Runtime:             rt,
			Writer:              w,
			HeartbeatInterval:   -1,
			RequireDoneSentinel: true,
		})
	}))
	t.Cleanup(proxySrv.Close)

	httpClient := netguard.NewHTTPClient()
	t.Cleanup(httpClient.CloseIdleConnections)
	resp, err := httpClient.Get(proxySrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(resp.Body)
	chunksRead := 0
	for chunksRead < 3 {
		line, rerr := reader.ReadString('\n')
		if rerr != nil {
			t.Fatalf("read chunk before disconnect: %v", rerr)
		}
		if strings.HasPrefix(line, "data:") {
			chunksRead++
		}
	}

	abortAt := time.Now()
	_ = resp.Body.Close()

	select {
	case disconnAt := <-disconnectCh:
		elapsed := disconnAt.Sub(abortAt)
		if elapsed >= 500*time.Millisecond {
			t.Fatalf("upstream disconnect propagation took %v (>= 500ms limit)", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream disconnect notification")
	}
}

func TestSSEPumpMidStreamAbortAndPreStreamError(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	srv := fakellm.NewServerWithOptions(t, fakellm.Options{StreamChunks: 5})
	u, _ := url.Parse(srv.URL())
	client, err := egress.NewGuardedHTTPClient(egress.ClientConfig{
		Policy: egress.Policy{
			AllowedHosts:   []string{u.Host},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
		},
		DisableTotalTimeout: true,
		Timeout:             5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	rt := testDeclarativeOpenAIRuntime(t)

	// 1. Mid-stream TCP RST after 2 chunks -> emits mid_stream_abort, ZERO fake [DONE].
	rec := httptest.NewRecorder()
	upCtx, cancelUp := context.WithCancel(context.Background())
	upReq, _ := http.NewRequestWithContext(upCtx, http.MethodPost, srv.URL()+"/v1/chat/completions", strings.NewReader(`{"model":"fake-gpt-4o","stream":true}`))
	upReq.Header.Set(fakellm.FaultHeader, "tcp_rst_after_chunks=2")
	upResp, err := client.Do(upReq)
	if err != nil {
		t.Fatal(err)
	}
	res, pumpErr := sse.Pump(upCtx, sse.PumpConfig{
		RequestID:           "req_mid_abort",
		Upstream:            upResp.Body,
		CancelUpstream:      cancelUp,
		Runtime:             rt,
		Writer:              rec,
		HeartbeatInterval:   -1,
		RequireDoneSentinel: true,
	})
	_ = upResp.Body.Close()
	if !errors.Is(pumpErr, sse.ErrMidStreamAbort) || !res.StreamStarted || res.SawDone {
		t.Fatalf("expected ErrMidStreamAbort after stream started without [DONE], res=%+v err=%v", res, pumpErr)
	}
	wire := rec.Body.String()
	if !strings.Contains(wire, "event: error\ndata: ") || !strings.Contains(wire, `"code":"mid_stream_abort"`) {
		t.Fatalf("expected canonical mid_stream_abort event, got %q", wire)
	}
	if strings.Contains(wire, "[DONE]") {
		t.Fatalf("mid_stream_abort must NEVER emit synthetic [DONE], got %q", wire)
	}

	// 2. TCP RST at chunk 0 (before first frame) -> StreamStarted=false so HTTP JSON 502 is still possible.
	rec0 := httptest.NewRecorder()
	upCtx0, cancelUp0 := context.WithCancel(context.Background())
	upReq0, _ := http.NewRequestWithContext(upCtx0, http.MethodPost, srv.URL()+"/v1/chat/completions", strings.NewReader(`{"model":"fake-gpt-4o","stream":true}`))
	upReq0.Header.Set(fakellm.FaultHeader, "tcp_rst_after_chunks=0")
	upResp0, err := client.Do(upReq0)
	if err != nil {
		t.Fatal(err)
	}
	res0, pumpErr0 := sse.Pump(upCtx0, sse.PumpConfig{
		RequestID:           "req_pre_abort",
		Upstream:            upResp0.Body,
		CancelUpstream:      cancelUp0,
		Runtime:             rt,
		Writer:              rec0,
		HeartbeatInterval:   -1,
		RequireDoneSentinel: true,
	})
	_ = upResp0.Body.Close()
	if pumpErr0 == nil || res0.StreamStarted {
		t.Fatalf("expected pre-first-byte failure with StreamStarted=false, res=%+v err=%v", res0, pumpErr0)
	}
	if rec0.Body.Len() != 0 {
		t.Fatalf("expected empty ResponseWriter body before first byte, got %q", rec0.Body.String())
	}
}

func TestSSEPumpOversizedLineAbortsBoundedRAM(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	pr, pw := io.Pipe()
	go func() {
		chunk := bytes.Repeat([]byte("a"), 64*1024)
		for i := 0; i < 40; i++ { // 2.5 MiB without '\n'
			if _, err := pw.Write(chunk); err != nil {
				return
			}
		}
		_ = pw.Close()
	}()

	rec := httptest.NewRecorder()
	rt := testDeclarativeOpenAIRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	res, err := sse.Pump(ctx, sse.PumpConfig{
		RequestID:           "req_neg2",
		Upstream:            pr,
		CancelUpstream:      cancel,
		Runtime:             rt,
		Writer:              rec,
		MaxLineBytes:        sse.DefaultMaxLineBytes,
		HeartbeatInterval:   -1,
		RequireDoneSentinel: true,
	})
	if !errors.Is(err, sse.ErrLineTooLarge) || res.StreamStarted {
		t.Fatalf("expected ErrLineTooLarge before stream start, got res=%+v err=%v", res, err)
	}
}

func TestSSEPumpIdleChunkTimeoutPreAndPostStream(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	rt := testDeclarativeOpenAIRuntime(t)

	// 1. Pre-stream stall: upstream sends zero frames within IdleChunkTimeout -> ErrIdleChunkTimeout, StreamStarted=false.
	pr1, pw1 := io.Pipe()
	t.Cleanup(func() { _ = pw1.Close() })
	rec1 := httptest.NewRecorder()
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()

	res1, err1 := sse.Pump(ctx1, sse.PumpConfig{
		RequestID:           "req_idle_pre",
		Upstream:            pr1,
		CancelUpstream:      cancel1,
		Runtime:             rt,
		Writer:              rec1,
		HeartbeatInterval:   -1,
		IdleChunkTimeout:    30 * time.Millisecond,
		RequireDoneSentinel: true,
	})
	if !errors.Is(err1, sse.ErrIdleChunkTimeout) || res1.StreamStarted {
		t.Fatalf("expected pre-stream ErrIdleChunkTimeout with StreamStarted=false, got res=%+v err=%v", res1, err1)
	}

	// 2. Post-first-byte stall: upstream emits 1 chunk then stalls -> mid_stream_abort, 0 [DONE].
	pr2, pw2 := io.Pipe()
	t.Cleanup(func() { _ = pw2.Close() })
	go func() {
		_, _ = pw2.Write([]byte("data: {\"id\":\"1\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"))
	}()
	rec2 := httptest.NewRecorder()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	res2, err2 := sse.Pump(ctx2, sse.PumpConfig{
		RequestID:           "req_idle_post",
		Upstream:            pr2,
		CancelUpstream:      cancel2,
		Runtime:             rt,
		Writer:              rec2,
		HeartbeatInterval:   -1,
		IdleChunkTimeout:    40 * time.Millisecond,
		RequireDoneSentinel: true,
	})
	if !errors.Is(err2, sse.ErrMidStreamAbort) || !errors.Is(err2, sse.ErrIdleChunkTimeout) || !res2.StreamStarted {
		t.Fatalf("expected ErrMidStreamAbort wrapping ErrIdleChunkTimeout after stream started, got res=%+v err=%v", res2, err2)
	}
	wire := rec2.Body.String()
	if !strings.Contains(wire, `"code":"mid_stream_abort"`) || strings.Contains(wire, "[DONE]") {
		t.Fatalf("expected mid_stream_abort without [DONE], got %q", wire)
	}
}

type blockingErrReader struct {
	firstFrameSent bool
	canceledReady  <-chan struct{}
}

func (b *blockingErrReader) Read(p []byte) (int, error) {
	if !b.firstFrameSent {
		b.firstFrameSent = true
		return copy(p, []byte("data: {\"id\":\"1\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")), nil
	}
	<-b.canceledReady
	return 0, io.ErrUnexpectedEOF
}

func (b *blockingErrReader) Close() error { return nil }

type streamCompletedRuntime struct{ port.ProviderRuntime }

func (streamCompletedRuntime) TransformStreamChunk(_ context.Context, in port.StreamChunkInput) (port.StreamChunkOutput, error) {
	return port.StreamChunkOutput{
		NormalizedSSEFrames: [][]byte{in.RawUpstreamChunk},
		StreamCompleted:     true,
	}, nil
}

type notifyResponseWriter struct {
	*httptest.ResponseRecorder
	once  sync.Once
	wrote chan struct{}
}

func (n *notifyResponseWriter) Write(p []byte) (int, error) {
	n.once.Do(func() { close(n.wrote) })
	return n.ResponseRecorder.Write(p)
}

func (n *notifyResponseWriter) Flush() {
	n.ResponseRecorder.Flush()
}

func TestSSEPumpCanceledContextAndStreamCompleted(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	// 1. Outbound StreamCompleted marks SawDone=true even without literal [DONE] frame.
	rec1 := httptest.NewRecorder()
	res1, err1 := sse.Pump(context.Background(), sse.PumpConfig{
		RequestID:           "req_stream_completed",
		Upstream:            io.NopCloser(strings.NewReader("data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"}}]}\n\n")),
		Runtime:             streamCompletedRuntime{},
		Writer:              rec1,
		HeartbeatInterval:   -1,
		RequireDoneSentinel: true,
	})
	if err1 != nil || !res1.SawDone {
		t.Fatalf("expected nil error and SawDone=true on StreamCompleted, got res=%+v err=%v", res1, err1)
	}

	// 2. When ctx is canceled concurrently with upstream Read returning an error, ClientDisconnected wins
	// and Pump must NOT emit a spurious mid_stream_abort event.
	canceledReady := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	_, cancelUp := context.WithCancel(ctx)
	defer cancelUp()
	rec2 := httptest.NewRecorder()
	nw := &notifyResponseWriter{
		ResponseRecorder: rec2,
		wrote:            make(chan struct{}),
	}
	rt := testDeclarativeOpenAIRuntime(t)
	rdr := &blockingErrReader{canceledReady: canceledReady}

	doneCh := make(chan struct{})
	var (
		res2 sse.PumpResult
		err2 error
	)
	go func() {
		defer close(doneCh)
		res2, err2 = sse.Pump(ctx, sse.PumpConfig{
			RequestID:           "req_cancel_race",
			Upstream:            rdr,
			CancelUpstream:      cancelUp,
			Runtime:             rt,
			Writer:              nw,
			HeartbeatInterval:   -1,
			RequireDoneSentinel: true,
		})
	}()
	select {
	case <-nw.wrote:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first frame write")
	}
	cancel()
	close(canceledReady)
	<-doneCh

	if !res2.ClientDisconnected || !errors.Is(err2, context.Canceled) {
		t.Fatalf("expected ClientDisconnected=true and context.Canceled, got res=%+v err=%v", res2, err2)
	}
	if strings.Contains(rec2.Body.String(), "mid_stream_abort") {
		t.Fatalf("spurious mid_stream_abort emitted on client cancel race: %q", rec2.Body.String())
	}
}

type noopChunkRuntime struct{ port.ProviderRuntime }

func (noopChunkRuntime) TransformStreamChunk(_ context.Context, in port.StreamChunkInput) (port.StreamChunkOutput, error) {
	return port.StreamChunkOutput{
		NormalizedSSEFrames: [][]byte{in.RawUpstreamChunk},
		StreamCompleted:     bytes.Contains(in.RawUpstreamChunk, []byte("[DONE]")),
	}, nil
}

func BenchmarkSSEPumpPerChunkOverhead(b *testing.B) {
	chunk := []byte("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
	payload := make([]byte, 0, len(chunk)*101)
	for i := 0; i < 100; i++ {
		payload = append(payload, chunk...)
	}
	payload = append(payload, []byte("data: [DONE]\n\n")...)

	rec := httptest.NewRecorder()
	ctx := context.Background()
	rt := noopChunkRuntime{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec.Body.Reset()
		_, err := sse.Pump(ctx, sse.PumpConfig{
			RequestID:           "req_bench",
			Upstream:            io.NopCloser(bytes.NewReader(payload)),
			Runtime:             rt,
			Writer:              rec,
			HeartbeatInterval:   -1,
			RequireDoneSentinel: true,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

var _ = json.Valid
