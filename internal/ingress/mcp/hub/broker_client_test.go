// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/testkit/fakemcp"
)

func TestBrokerClientInitializeSessionPaginationAndCall(t *testing.T) {
	tools := []fakemcp.Tool{
		{Name: "search", Description: "Find records", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "read", InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`)},
		{Name: "write", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	fake := fakemcp.New(tools, 1)
	var mu sync.Mutex
	var methods []string
	var sessions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream request: %v", err)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var request struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode upstream request: %v", err)
		}
		mu.Lock()
		methods = append(methods, request.Method)
		sessions = append(sessions, r.Header.Get(sessionHeader))
		mu.Unlock()
		fake.ServeHTTP(w, r)
	}))
	defer server.Close()

	client := &BrokerClient{Broker: newBrokerClientTestBroker(t, func() egress.Policy {
		return localTestPolicy(t, server.URL, true)
	})}
	definitions, err := client.ListTools(context.Background(), brokerClientServer(server.URL))
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(definitions) != len(tools) {
		t.Fatalf("ListTools returned %d tools, want %d", len(definitions), len(tools))
	}
	for i, want := range tools {
		if definitions[i].Name != want.Name {
			t.Fatalf("tool %d name = %q, want %q", i, definitions[i].Name, want.Name)
		}
	}

	result, err := client.CallTool(context.Background(), brokerClientServer(server.URL), "search", json.RawMessage(`{"query":"tanpopo"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !strings.Contains(string(result), `"isError":false`) {
		t.Fatalf("CallTool result = %s, want successful tool result", result)
	}
	if calls := fake.Calls(); len(calls) != 1 || calls[0].Name != "search" || string(calls[0].Arguments) != `{"query":"tanpopo"}` {
		t.Fatalf("fake MCP calls = %+v, want one search call with original arguments", calls)
	}

	mu.Lock()
	defer mu.Unlock()
	wantMethods := []string{
		"initialize", "notifications/initialized", "tools/list", "tools/list", "tools/list",
		"initialize", "notifications/initialized", "tools/call",
	}
	if fmt.Sprint(methods) != fmt.Sprint(wantMethods) {
		t.Fatalf("upstream methods = %v, want %v", methods, wantMethods)
	}
	if sessions[0] != "" {
		t.Fatalf("initialize unexpectedly had a session header %q", sessions[0])
	}
	for _, index := range []int{1, 2, 3, 4, 6, 7} {
		if sessions[index] != "fake-session" {
			t.Errorf("upstream request %q had session %q, want fake-session", methods[index], sessions[index])
		}
	}
}

func TestBrokerClientPolicyBlocksLoopbackAndUnapprovedHost(t *testing.T) {
	tests := []struct {
		name   string
		policy func(*testing.T, string) egress.Policy
	}{
		{
			name: "loopback without explicit private-network consent",
			policy: func(t *testing.T, target string) egress.Policy {
				policy := localTestPolicy(t, target, false)
				policy.AllowedCIDRs = nil
				return policy
			},
		},
		{
			name: "unapproved host",
			policy: func(t *testing.T, target string) egress.Policy {
				policy := localTestPolicy(t, target, true)
				policy.AllowedHosts = []string{"mcp-approved.invalid"}
				return policy
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				fakemcp.New(nil, 1).ServeHTTP(w, r)
			}))
			defer server.Close()
			client := &BrokerClient{Broker: newBrokerClientTestBroker(t, func() egress.Policy { return tt.policy(t, server.URL) })}
			_, err := client.ListTools(context.Background(), brokerClientServer(server.URL))
			if err == nil {
				t.Fatal("ListTools succeeded despite the egress policy")
			}
			if hits != 0 {
				t.Fatalf("upstream received %d requests despite egress denial", hits)
			}
		})
	}
}

func TestBrokerClientRejectsMismatchedJSONRPCResponseID(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":999,"result":{"protocolVersion":"2025-03-26"}}`)
	}))
	defer upstream.Close()
	client := &BrokerClient{Broker: newBrokerClientTestBroker(t, func() egress.Policy {
		return localTestPolicy(t, upstream.URL, true)
	})}
	if _, err := client.ListTools(context.Background(), brokerClientServer(upstream.URL)); err == nil {
		t.Fatal("ListTools accepted a JSON-RPC response with a mismatched request ID")
	}
}

func TestBrokerClientTimeoutBoundsFullUpstreamOperation(t *testing.T) {
	started := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(sessionHeader, "timeout-session")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
		}
	}))
	defer upstream.Close()
	client := &BrokerClient{Broker: newBrokerClientTestBroker(t, func() egress.Policy {
		return localTestPolicy(t, upstream.URL, true)
	}), Timeout: 50 * time.Millisecond}
	startedAt := time.Now()
	_, err := client.ListTools(context.Background(), brokerClientServer(upstream.URL))
	if err == nil {
		t.Fatal("ListTools succeeded after the upstream stalled its response body")
	}
	select {
	case <-started:
	default:
		t.Fatal("request did not reach the stalled response")
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("full operation deadline did not bound response-body read: %s", elapsed)
	}
}

func TestBrokerClientDoesNotForwardCallerHeaders(t *testing.T) {
	tools := []fakemcp.Tool{{Name: "tool_000", Description: "safe tool", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)}}
	fake := fakemcp.New(tools, 1)
	var mu sync.Mutex
	var outboundHeaders []http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		outboundHeaders = append(outboundHeaders, r.Header.Clone())
		mu.Unlock()
		fake.ServeHTTP(w, r)
	}))
	defer upstream.Close()

	h := newHubTestSetup(t, 1, false)
	endpoint, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.handler.cfg.DB.Exec(`UPDATE mcp_servers SET endpoint_url = ?, allow_private_ip = 1, allowed_cidrs_json = '["127.0.0.1/32"]'`, upstream.URL); err != nil {
		t.Fatalf("configure local MCP test endpoint: %v", err)
	}
	actualBroker := newBrokerClientTestBroker(t, func() egress.Policy {
		port := 80
		if endpoint.Port() != "" {
			_, _ = fmt.Sscanf(endpoint.Port(), "%d", &port)
		}
		return egress.Policy{AllowedHosts: []string{endpoint.Host}, AllowPrivateIP: true,
			AllowedCIDRs: []string{"127.0.0.1/32"}, AllowedPorts: []int{port}}
	})
	h.handler.cfg.Upstream = &BrokerClient{Broker: actualBroker}

	_, session, _ := h.post(t, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, "")
	if session.Get(sessionHeader) == "" {
		t.Fatal("initialize did not issue a client session")
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"github__tool_000","arguments":{"query":"ok"}}}`))
	request.Header.Set("Authorization", "Bearer "+h.key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set(sessionHeader, session.Get(sessionHeader))
	request.Header.Set("Idempotency-Key", "mcp-call-unique-01")
	request.Header.Set("Cookie", "session=caller-cookie")
	request.Header.Set("X-Caller-Private", "caller-arbitrary-header")
	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("tools/call status = %d, body = %s", response.Code, response.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(outboundHeaders) != 4 {
		t.Fatalf("upstream saw %d requests, want one-session initialize, notification, verify and call", len(outboundHeaders))
	}
	for i, headers := range outboundHeaders {
		for _, name := range []string{"Authorization", "Cookie", "X-Caller-Private"} {
			if value := headers.Get(name); value != "" {
				t.Errorf("upstream request %d received caller header %s=%q", i, name, value)
			}
		}
		if headers.Get("Content-Type") != "application/json" || !strings.Contains(headers.Get("Accept"), "application/json") {
			t.Errorf("upstream request %d missing BrokerClient protocol headers: %v", i, headers)
		}
		if i == len(outboundHeaders)-1 && headers.Get("Idempotency-Key") != "mcp-call-unique-01" {
			t.Errorf("tools/call did not propagate validated idempotency key: %v", headers)
		}
	}
}

type brokerClientTestResolver struct {
	policy func() egress.Policy
}

func (r brokerClientTestResolver) ResolveSlot(context.Context, string, string, string) (*broker.ResolvedSlot, error) {
	return nil, port.ErrNotFound
}

func (r brokerClientTestResolver) ResolveConnectionPolicy(context.Context, string, string) (*egress.Policy, error) {
	policy := r.policy()
	return &policy, nil
}

type brokerClientTestVault struct{}

func (brokerClientTestVault) UnsealWithCapability(context.Context, vault.UnsealCapability, string, string) (*vault.UnsealedSecret, error) {
	return nil, errors.New("unexpected credential unseal in no-credential test")
}

type brokerClientTestAudit struct{}

func (brokerClientTestAudit) Append(context.Context, port.AuditEvent) error { return nil }

func newBrokerClientTestBroker(t *testing.T, policy func() egress.Policy) *broker.BlindHostCredentialBroker {
	t.Helper()
	result, err := broker.NewBlindBroker(broker.Config{
		Resolver: brokerClientTestResolver{policy: policy},
		Vault:    brokerClientTestVault{},
		Audit:    brokerClientTestAudit{},
	})
	if err != nil {
		t.Fatalf("create BlindHostCredentialBroker: %v", err)
	}
	return result
}

func localTestPolicy(t *testing.T, target string, allowPrivate bool) egress.Policy {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse test MCP endpoint: %v", err)
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if u.Port() != "" {
		if _, err := fmt.Sscanf(u.Port(), "%d", &port); err != nil {
			t.Fatalf("parse endpoint port: %v", err)
		}
	}
	policy := egress.Policy{AllowedHosts: []string{u.Host}, AllowPrivateIP: allowPrivate, AllowedPorts: []int{port}}
	if allowPrivate {
		policy.AllowedCIDRs = []string{"127.0.0.1/32"}
	}
	return policy
}

func brokerClientServer(endpoint string) serverRecord {
	return serverRecord{ID: "mcp_server_test", OrgID: "org_test", ProjectID: "project_test", Slug: "test",
		Transport: "STREAMABLE_HTTP", Endpoint: endpoint, Status: "ACTIVE"}
}
