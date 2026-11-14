// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package fakellm provides FakeLLMServer, an offline loopback-only HTTP/SSE test
// double supporting OpenAI, Anthropic, and Gemini wire dialects alongside
// deterministic fault injection via X-Tanpopo-Fake-Fault for CT-06..CT-16.
package fakellm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

// FaultHeader is the HTTP header carrying semicolon/comma-separated fault directives.
const FaultHeader = "X-Tanpopo-Fake-Fault"

// StreamChunksHeader overrides the number of streamed SSE chunks emitted per response.
const StreamChunksHeader = "X-Tanpopo-Fake-Stream-Chunks"

// Options customizes FakeLLMServer behavior.
type Options struct {
	Clock        clock.Clock
	StreamChunks int
}

// RecordedRequest stores exact metadata captured for each request hitting FakeLLMServer.
type RecordedRequest struct {
	Method         string
	Path           string
	RawQuery       string
	Headers        http.Header
	Body           []byte
	ReceivedAt     time.Time
	CompletedAt    time.Time
	Disconnected   bool
	DisconnectedAt time.Time
	ChunksSent     int
	FaultApplied   string
}

type faultSpec struct {
	raw              string
	delayFirstByte   time.Duration
	tcpRSTAfter      int
	hasTCPRST        bool
	httpStatus       int
	retryAfter       string
	omitStreamUsage  bool
	echoAuthInError  bool
	malformedToolArg bool
}

// FakeLLMServer emulates loopback AI provider APIs for hermetic tests.
type FakeLLMServer struct {
	listener net.Listener
	server   *http.Server
	clock    clock.Clock
	client   *http.Client

	defaultChunks int

	mu           sync.Mutex
	requests     []RecordedRequest
	defaultFault string
	faultQueue   []string
	disconnectCh chan time.Time
	closed       bool
}

// NewServer starts a FakeLLMServer on 127.0.0.1:0 and registers automatic teardown via t.Cleanup.
func NewServer(t testing.TB) *FakeLLMServer {
	t.Helper()
	return NewServerWithOptions(t, Options{})
}

// NewServerWithOptions starts a FakeLLMServer on 127.0.0.1:0 with custom Options.
func NewServerWithOptions(t testing.TB, opts Options) *FakeLLMServer {
	t.Helper()
	if opts.Clock == nil {
		opts.Clock = clock.NewRealClock()
	}
	if opts.StreamChunks <= 0 {
		opts.StreamChunks = 5
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakellm listen 127.0.0.1:0: %v", err)
	}

	s := &FakeLLMServer{
		listener:      ln,
		clock:         opts.Clock,
		client:        netguard.NewHTTPClient(),
		defaultChunks: opts.StreamChunks,
		disconnectCh:  make(chan time.Time, 64),
	}
	s.server = &http.Server{
		Handler:           http.HandlerFunc(s.serveHTTP),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		_ = s.server.Serve(ln)
	}()

	t.Cleanup(s.Close)
	return s
}

// URL returns the base HTTP loopback URL (e.g. "http://127.0.0.1:12345").
func (s *FakeLLMServer) URL() string {
	return "http://" + s.listener.Addr().String()
}

// Addr returns the listener's host:port string.
func (s *FakeLLMServer) Addr() string {
	return s.listener.Addr().String()
}

// Client returns a netguard-protected *http.Client suitable for querying this server.
func (s *FakeLLMServer) Client() *http.Client {
	return s.client
}

// Close shuts down the HTTP server and closes the loopback listener idempotently.
func (s *FakeLLMServer) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	if s.client != nil {
		s.client.CloseIdleConnections()
	}
	if s.server != nil {
		_ = s.server.Close()
	}
}

// SetDefaultFault sets a server-side fault directive applied to subsequent requests
// that do not specify X-Tanpopo-Fake-Fault.
func (s *FakeLLMServer) SetDefaultFault(directive string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultFault = directive
}

// EnqueueFault pushes one-shot fault directives applied FIFO on subsequent requests.
func (s *FakeLLMServer) EnqueueFault(directives ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faultQueue = append(s.faultQueue, directives...)
}

// Requests returns a deep copy of all requests received so far.
func (s *FakeLLMServer) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RecordedRequest, len(s.requests))
	for i, r := range s.requests {
		cp := r
		cp.Headers = r.Headers.Clone()
		cp.Body = append([]byte(nil), r.Body...)
		out[i] = cp
	}
	return out
}

// ResetRequests clears recorded request telemetry.
func (s *FakeLLMServer) ResetRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
}

// DisconnectEvents returns a channel signaled with the disconnect timestamp whenever
// an active streaming client cancels its connection.
func (s *FakeLLMServer) DisconnectEvents() <-chan time.Time {
	return s.disconnectCh
}

func (s *FakeLLMServer) nextFaultDirective(headerVal string) string {
	if strings.TrimSpace(headerVal) != "" {
		return headerVal
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.faultQueue) > 0 {
		next := s.faultQueue[0]
		s.faultQueue = s.faultQueue[1:]
		return next
	}
	return s.defaultFault
}

func parseFaultSpec(raw string) faultSpec {
	spec := faultSpec{raw: raw}
	if strings.TrimSpace(raw) == "" {
		return spec
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == ','
	})
	for _, f := range fields {
		part := strings.TrimSpace(f)
		if part == "" {
			continue
		}
		k, v, hasEq := strings.Cut(part, "=")
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch k {
		case "delay_first_byte_ms":
			if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
				spec.delayFirstByte = time.Duration(ms) * time.Millisecond
			}
		case "tcp_rst_after_chunks":
			if cnt, err := strconv.Atoi(v); err == nil && cnt >= 0 {
				spec.tcpRSTAfter = cnt
				spec.hasTCPRST = true
			}
		case "http_status":
			if code, err := strconv.Atoi(v); err == nil && code >= 100 && code <= 599 {
				spec.httpStatus = code
			}
		case "retry_after":
			spec.retryAfter = v
		case "omit_stream_usage":
			if !hasEq || strings.EqualFold(v, "true") || v == "1" {
				spec.omitStreamUsage = true
			}
		case "echo_auth_header_in_error":
			if !hasEq || strings.EqualFold(v, "true") || v == "1" {
				spec.echoAuthInError = true
			}
		case "malformed_tool_json":
			if !hasEq || strings.EqualFold(v, "true") || v == "1" {
				spec.malformedToolArg = true
			}
		}
	}
	return spec
}

func (s *FakeLLMServer) recordStart(r *http.Request, body []byte, fault string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := len(s.requests)
	s.requests = append(s.requests, RecordedRequest{
		Method:       r.Method,
		Path:         r.URL.Path,
		RawQuery:     r.URL.RawQuery,
		Headers:      r.Header.Clone(),
		Body:         append([]byte(nil), body...),
		ReceivedAt:   s.clock.Now(),
		FaultApplied: fault,
	})
	return idx
}

func (s *FakeLLMServer) recordEnd(idx, chunks int, disconnected bool, disconnAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < 0 || idx >= len(s.requests) {
		return
	}
	s.requests[idx].ChunksSent = chunks
	s.requests[idx].CompletedAt = s.clock.Now()
	if disconnected {
		s.requests[idx].Disconnected = true
		s.requests[idx].DisconnectedAt = disconnAt
	}
}

func (s *FakeLLMServer) notifyDisconnect(ts time.Time) {
	select {
	case s.disconnectCh <- ts:
	default:
	}
}

type baseReqPayload struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func (s *FakeLLMServer) resolveChunkCount(r *http.Request) int {
	if hv := strings.TrimSpace(r.Header.Get(StreamChunksHeader)); hv != "" {
		if n, err := strconv.Atoi(hv); err == nil && n > 0 {
			return n
		}
	}
	if qv := strings.TrimSpace(r.URL.Query().Get("chunks")); qv != "" {
		if n, err := strconv.Atoi(qv); err == nil && n > 0 {
			return n
		}
	}
	return s.defaultChunks
}

func (s *FakeLLMServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	_ = r.Body.Close()

	faultStr := s.nextFaultDirective(r.Header.Get(FaultHeader))
	fault := parseFaultSpec(faultStr)
	reqIdx := s.recordStart(r, body, faultStr)

	if fault.delayFirstByte > 0 {
		tm := s.clock.NewTimer(fault.delayFirstByte)
		select {
		case <-r.Context().Done():
			tm.Stop()
			ts := s.clock.Now()
			s.recordEnd(reqIdx, 0, true, ts)
			s.notifyDisconnect(ts)
			return
		case <-tm.C():
		}
	}

	if fault.echoAuthInError {
		status := fault.httpStatus
		if status == 0 {
			status = http.StatusUnauthorized
		}
		inboundAuth := r.Header.Get("Authorization")
		if inboundAuth == "" {
			inboundAuth = r.Header.Get("x-api-key")
		}
		if inboundAuth == "" {
			inboundAuth = r.Header.Get("x-goog-api-key")
		}
		writeJSONError(w, status, fault.retryAfter, "invalid_api_key", fmt.Sprintf("Incorrect API key provided: %s", inboundAuth))
		s.recordEnd(reqIdx, 0, false, time.Time{})
		return
	}

	if fault.httpStatus > 0 {
		writeJSONError(w, fault.httpStatus, fault.retryAfter, "injected_fault", fmt.Sprintf("FakeLLMServer injected HTTP %d fault", fault.httpStatus))
		s.recordEnd(reqIdx, 0, false, time.Time{})
		return
	}

	var parsed baseReqPayload
	if len(body) > 0 {
		_ = json.Unmarshal(body, &parsed)
	}
	if parsed.Model == "" {
		parsed.Model = "fake-gpt-4o"
	}

	path := r.URL.Path
	switch {
	case path == "/v1/models" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "fake-gpt-4o", "object": "model", "owned_by": "fakellm"},
				{"id": "fake-claude-3-7-sonnet", "object": "model", "owned_by": "fakellm"},
				{"id": "fake-gemini-2-5-flash", "object": "model", "owned_by": "fakellm"},
			},
		})
		s.recordEnd(reqIdx, 0, false, time.Time{})

	case path == "/v1/embeddings" && r.Method == http.MethodPost:
		writeJSON(w, http.StatusOK, map[string]any{
			"object": "list",
			"model":  parsed.Model,
			"data":   []map[string]any{{"object": "embedding", "index": 0, "embedding": []float64{0.125, -0.25, 0.5}}},
			"usage":  map[string]int{"prompt_tokens": 6, "total_tokens": 6},
		})
		s.recordEnd(reqIdx, 0, false, time.Time{})

	case path == "/v1/rerank" && r.Method == http.MethodPost:
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "rerank_fake_1",
			"model":   parsed.Model,
			"results": []map[string]any{{"index": 0, "relevance_score": 0.98}},
			"usage":   map[string]int{"total_tokens": 12},
		})
		s.recordEnd(reqIdx, 0, false, time.Time{})

	case path == "/v1/chat/completions" && r.Method == http.MethodPost:
		if parsed.Stream {
			s.serveOpenAIChatStream(w, r, reqIdx, parsed.Model, fault)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "chatcmpl_fake_1",
			"object":  "chat.completion",
			"created": s.clock.Now().Unix(),
			"model":   parsed.Model,
			"choices": []map[string]any{
				{
					"index":         0,
					"message":       map[string]any{"role": "assistant", "content": "ok-from-fakellm"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
		s.recordEnd(reqIdx, 0, false, time.Time{})

	case path == "/v1/responses" && r.Method == http.MethodPost:
		if parsed.Stream {
			s.serveOpenAIResponsesStream(w, r, reqIdx, parsed.Model, fault)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "resp_fake_1",
			"object": "response",
			"model":  parsed.Model,
			"status": "completed",
			"output": []map[string]any{
				{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": "ok-from-responses"},
					},
				},
			},
			"usage": map[string]int{"input_tokens": 10, "output_tokens": 4, "total_tokens": 14},
		})
		s.recordEnd(reqIdx, 0, false, time.Time{})

	case path == "/v1/messages" && r.Method == http.MethodPost:
		if parsed.Stream {
			s.serveAnthropicStream(w, r, reqIdx, parsed.Model, fault)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "msg_fake_1",
			"type":        "message",
			"role":        "assistant",
			"model":       parsed.Model,
			"content":     []map[string]any{{"type": "text", "text": "ok-from-anthropic"}},
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 6},
		})
		s.recordEnd(reqIdx, 0, false, time.Time{})

	case strings.HasPrefix(path, "/v1beta/models/") && strings.HasSuffix(path, ":streamGenerateContent") && r.Method == http.MethodPost:
		s.serveGeminiStream(w, r, reqIdx, parsed.Model, fault)
		return

	case strings.HasPrefix(path, "/v1beta/models/") && strings.HasSuffix(path, ":generateContent") && r.Method == http.MethodPost:
		writeJSON(w, http.StatusOK, map[string]any{
			"candidates": []map[string]any{
				{
					"index": 0,
					"content": map[string]any{
						"role":  "model",
						"parts": []map[string]any{{"text": "ok-from-gemini"}},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]int{"promptTokenCount": 9, "candidatesTokenCount": 4, "totalTokenCount": 13},
		})
		s.recordEnd(reqIdx, 0, false, time.Time{})

	default:
		writeJSONError(w, http.StatusNotFound, "", "not_found", fmt.Sprintf("unsupported fake route %s %s", r.Method, path))
		s.recordEnd(reqIdx, 0, false, time.Time{})
	}
}

func (s *FakeLLMServer) writeAndFlushSSE(w http.ResponseWriter, rc *http.ResponseController, r *http.Request, reqIdx, sent int, data []byte) bool {
	if r.Context().Err() != nil {
		ts := s.clock.Now()
		s.recordEnd(reqIdx, sent, true, ts)
		s.notifyDisconnect(ts)
		return false
	}
	if _, err := w.Write(data); err != nil {
		ts := s.clock.Now()
		s.recordEnd(reqIdx, sent, true, ts)
		s.notifyDisconnect(ts)
		return false
	}
	_ = rc.Flush()
	return true
}

func (s *FakeLLMServer) serveOpenAIChatStream(w http.ResponseWriter, r *http.Request, reqIdx int, model string, fault faultSpec) {
	rc := http.NewResponseController(w)
	prepareSSEHeaders(w)
	if err := rc.Flush(); err != nil {
		s.recordEnd(reqIdx, 0, false, time.Time{})
		return
	}

	targetChunks := s.resolveChunkCount(r)
	malformedIdx := 1
	if targetChunks <= 1 {
		malformedIdx = 0
	}
	sent := 0
	var buf bytes.Buffer

	for i := 0; i < targetChunks; i++ {
		if r.Context().Err() != nil {
			ts := s.clock.Now()
			s.recordEnd(reqIdx, sent, true, ts)
			s.notifyDisconnect(ts)
			return
		}
		if fault.hasTCPRST && sent >= fault.tcpRSTAfter {
			s.hijackAndResetTCP(rc)
			s.recordEnd(reqIdx, sent, false, time.Time{})
			return
		}

		buf.Reset()
		if i > 0 && i%5 == 0 {
			buf.WriteString(": ping\n\n")
		}
		if fault.malformedToolArg && i == malformedIdx {
			fmt.Fprintf(&buf, "data: {\"id\":\"chatcmpl_fake_1\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"Tokyo\"}}]}}]}\n\n", model)
		} else {
			fmt.Fprintf(&buf, "data: {\"id\":\"chatcmpl_fake_1\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"chunk-%d\"}}]}\n\n", model, i)
		}
		if !s.writeAndFlushSSE(w, rc, r, reqIdx, sent, buf.Bytes()) {
			return
		}
		sent++
	}

	if fault.hasTCPRST && sent >= fault.tcpRSTAfter {
		s.hijackAndResetTCP(rc)
		s.recordEnd(reqIdx, sent, false, time.Time{})
		return
	}

	buf.Reset()
	if fault.omitStreamUsage {
		fmt.Fprintf(&buf, "data: {\"id\":\"chatcmpl_fake_1\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n", model)
	} else {
		fmt.Fprintf(&buf, "data: {\"id\":\"chatcmpl_fake_1\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\n", model, sent, 10+sent)
	}
	buf.WriteString("data: [DONE]\n\n")
	if !s.writeAndFlushSSE(w, rc, r, reqIdx, sent, buf.Bytes()) {
		return
	}
	s.recordEnd(reqIdx, sent, false, time.Time{})
}

func (s *FakeLLMServer) serveOpenAIResponsesStream(w http.ResponseWriter, r *http.Request, reqIdx int, model string, fault faultSpec) {
	rc := http.NewResponseController(w)
	prepareSSEHeaders(w)
	if err := rc.Flush(); err != nil {
		s.recordEnd(reqIdx, 0, false, time.Time{})
		return
	}

	targetChunks := s.resolveChunkCount(r)
	sent := 0
	var buf bytes.Buffer
	for i := 0; i < targetChunks; i++ {
		if r.Context().Err() != nil {
			ts := s.clock.Now()
			s.recordEnd(reqIdx, sent, true, ts)
			s.notifyDisconnect(ts)
			return
		}
		if fault.hasTCPRST && sent >= fault.tcpRSTAfter {
			s.hijackAndResetTCP(rc)
			s.recordEnd(reqIdx, sent, false, time.Time{})
			return
		}
		buf.Reset()
		if i > 0 && i%5 == 0 {
			buf.WriteString(": ping\n\n")
		}
		fmt.Fprintf(&buf, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"model\":%q,\"delta\":\"tok-%d\"}\n\n", model, i)
		if !s.writeAndFlushSSE(w, rc, r, reqIdx, sent, buf.Bytes()) {
			return
		}
		sent++
	}
	if fault.hasTCPRST && sent >= fault.tcpRSTAfter {
		s.hijackAndResetTCP(rc)
		s.recordEnd(reqIdx, sent, false, time.Time{})
		return
	}
	buf.Reset()
	if fault.omitStreamUsage {
		fmt.Fprintf(&buf, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fake_1\",\"status\":\"completed\"}}\n\n")
	} else {
		fmt.Fprintf(&buf, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fake_1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":8,\"output_tokens\":%d,\"total_tokens\":%d}}}\n\n", sent, 8+sent)
	}
	if !s.writeAndFlushSSE(w, rc, r, reqIdx, sent, buf.Bytes()) {
		return
	}
	s.recordEnd(reqIdx, sent, false, time.Time{})
}

func (s *FakeLLMServer) serveAnthropicStream(w http.ResponseWriter, r *http.Request, reqIdx int, model string, fault faultSpec) {
	rc := http.NewResponseController(w)
	prepareSSEHeaders(w)
	startFrame := fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_fake_1\",\"role\":\"assistant\",\"model\":%q}}\n\n", model)
	if !s.writeAndFlushSSE(w, rc, r, reqIdx, 0, []byte(startFrame)) {
		return
	}

	targetChunks := s.resolveChunkCount(r)
	sent := 0
	var buf bytes.Buffer
	for i := 0; i < targetChunks; i++ {
		if r.Context().Err() != nil {
			ts := s.clock.Now()
			s.recordEnd(reqIdx, sent, true, ts)
			s.notifyDisconnect(ts)
			return
		}
		if fault.hasTCPRST && sent >= fault.tcpRSTAfter {
			s.hijackAndResetTCP(rc)
			s.recordEnd(reqIdx, sent, false, time.Time{})
			return
		}
		buf.Reset()
		if i > 0 && i%5 == 0 {
			buf.WriteString(": ping\n\n")
		}
		fmt.Fprintf(&buf, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"chunk-%d\"}}\n\n", i)
		if !s.writeAndFlushSSE(w, rc, r, reqIdx, sent, buf.Bytes()) {
			return
		}
		sent++
	}
	if fault.hasTCPRST && sent >= fault.tcpRSTAfter {
		s.hijackAndResetTCP(rc)
		s.recordEnd(reqIdx, sent, false, time.Time{})
		return
	}
	buf.Reset()
	if fault.omitStreamUsage {
		buf.WriteString("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n")
	} else {
		fmt.Fprintf(&buf, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":%d}}\n\n", sent)
	}
	buf.WriteString("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	if !s.writeAndFlushSSE(w, rc, r, reqIdx, sent, buf.Bytes()) {
		return
	}
	s.recordEnd(reqIdx, sent, false, time.Time{})
}

func (s *FakeLLMServer) serveGeminiStream(w http.ResponseWriter, r *http.Request, reqIdx int, model string, fault faultSpec) {
	rc := http.NewResponseController(w)
	prepareSSEHeaders(w)
	if err := rc.Flush(); err != nil {
		s.recordEnd(reqIdx, 0, false, time.Time{})
		return
	}

	targetChunks := s.resolveChunkCount(r)
	sent := 0
	var buf bytes.Buffer
	for i := 0; i < targetChunks; i++ {
		if r.Context().Err() != nil {
			ts := s.clock.Now()
			s.recordEnd(reqIdx, sent, true, ts)
			s.notifyDisconnect(ts)
			return
		}
		if fault.hasTCPRST && sent >= fault.tcpRSTAfter {
			s.hijackAndResetTCP(rc)
			s.recordEnd(reqIdx, sent, false, time.Time{})
			return
		}
		buf.Reset()
		if i > 0 && i%5 == 0 {
			buf.WriteString(": ping\n\n")
		}
		if i == targetChunks-1 && !fault.omitStreamUsage {
			fmt.Fprintf(&buf, "data: {\"modelVersion\":%q,\"candidates\":[{\"index\":0,\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"gem-%d\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":%d,\"totalTokenCount\":%d}}\n\n", model, i, sent+1, 7+sent+1)
		} else {
			fmt.Fprintf(&buf, "data: {\"modelVersion\":%q,\"candidates\":[{\"index\":0,\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"gem-%d\"}]}}]}\n\n", model, i)
		}
		if !s.writeAndFlushSSE(w, rc, r, reqIdx, sent, buf.Bytes()) {
			return
		}
		sent++
	}
	if fault.hasTCPRST && sent >= fault.tcpRSTAfter {
		s.hijackAndResetTCP(rc)
		s.recordEnd(reqIdx, sent, false, time.Time{})
		return
	}
	s.recordEnd(reqIdx, sent, false, time.Time{})
}

func (s *FakeLLMServer) hijackAndResetTCP(rc *http.ResponseController) {
	conn, _, err := rc.Hijack()
	if err != nil {
		return
	}
	// Hijacking skips Go's HTTP chunked terminator ("0\r\n\r\n") and closes the
	// TCP socket abruptly. Keeping default linger prevents the kernel from
	// discarding already-flushed SSE chunks while guaranteeing io.ErrUnexpectedEOF
	// deterministically on the next client read across Windows, macOS, and Linux.
	_ = conn.Close()
}

func prepareSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, retryAfter, errType, message string) {
	if retryAfter != "" {
		w.Header().Set("Retry-After", retryAfter)
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"type":    errType,
			"message": message,
		},
	})
}
