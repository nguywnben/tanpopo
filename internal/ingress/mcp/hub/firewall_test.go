// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package hub

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/ingress/mcp/approval"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/firewall"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/testkit/fakemcp"
)

func TestHubAmbiguousDispatchIsNeverRepeated(t *testing.T) {
	for _, tt := range []struct {
		class, failure string
		wantCalls      int
		wantAmbiguous  bool
	}{
		{"mutating", "drop", 1, true}, {"destructive", "drop", 1, true}, {"legacy", "drop", 1, true},
		{"mutating", "redirect", 1, true}, {"read_only", "drop", 1, false}, {"mutating", "before_call", 0, false},
	} {
		t.Run(tt.class+"_"+tt.failure, func(t *testing.T) {
			h := newHubTestSetup(t, 1, false)
			def := h.upstream.definitions[0]
			upstream := fakemcp.New([]fakemcp.Tool{{Name: def.Name, Description: def.Description, InputSchema: def.InputSchema}}, 1)
			edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				var rpc struct {
					Method string
					Params map[string]json.RawMessage
				}
				if err := json.Unmarshal(body, &rpc); err != nil {
					t.Error(err)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				if rpc.Method == "tools/call" {
					if len(rpc.Params) != 2 || rpc.Params["_meta"] != nil {
						t.Error("unexpected upstream parameters")
					}
					upstream.ServeHTTP(httptest.NewRecorder(), r)
					if tt.failure == "redirect" {
						http.Redirect(w, r, "/again", http.StatusTemporaryRedirect)
						return
					}
				} else if !(tt.failure == "before_call" && rpc.Method == "tools/list") {
					upstream.ServeHTTP(w, r)
					return
				}
				// The fake has accepted the operation but loses its response.
				conn, _, err := http.NewResponseController(w).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			}))
			defer edge.Close()
			policy := localTestPolicy(t, edge.URL, true)
			h.handler.cfg.Upstream = &BrokerClient{Broker: newBrokerClientTestBroker(t, func() egress.Policy { return policy }), Timeout: 2 * time.Second}
			db := h.handler.cfg.DB
			if _, err := db.Exec(`UPDATE mcp_servers SET endpoint_url=?,allow_private_ip=1`, edge.URL); err != nil {
				t.Fatal(err)
			}
			session := approvalSession(t, h)
			args := json.RawMessage(`{"query":"dev"}`)
			var meta map[string]any
			if tt.class == "destructive" {
				b := destructiveFixture(t, h)
				args = b.Arguments
				req := invokeMCP(t, h, session, "github__tool_000", args, nil).Result.Details
				if _, err := h.handler.approvals.Decide(t.Context(), req.ID, b, 1, "reviewer", approval.DecisionApprove, "reviewed"); err != nil {
					t.Fatal(err)
				}
				meta = map[string]any{"tanpopoApprovalToken": req.Token}
			} else {
				conn, err := db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				// Seed a legacy catalog value on this fixture connection only.
				if tt.class == "legacy" {
					if _, err = conn.ExecContext(t.Context(), "PRAGMA ignore_check_constraints=ON"); err != nil {
						t.Fatal(err)
					}
				}
				if _, err = conn.ExecContext(t.Context(), "UPDATE mcp_tools SET side_effect_class=?", tt.class); err != nil {
					t.Fatal(err)
				}
				if tt.class == "legacy" {
					if _, err = conn.ExecContext(t.Context(), "PRAGMA ignore_check_constraints=OFF"); err != nil {
						t.Fatal(err)
					}
				}
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
			}
			got := invokeMCP(t, h, session, "github__tool_000", args, meta)
			if tt.wantAmbiguous {
				if got.Status != http.StatusGatewayTimeout || got.Error.Code != firewall.AmbiguousSideEffectTimeoutCode || got.Error.Retryable == nil || *got.Error.Retryable {
					t.Fatalf("status=%d error=%v", got.Status, got.Error)
				}
			} else if got.Status != 200 || got.Error.Message != "tool_call_failed" {
				t.Fatalf("status=%d error=%v", got.Status, got.Error)
			}
			if tt.class == "destructive" {
				again := invokeMCP(t, h, session, "github__tool_000", args, meta)
				if again.Status != http.StatusForbidden || again.Error.Code != string(approval.CodeParameterHashMismatch) {
					t.Fatal("ambiguous execution retained approval", again.Error)
				}
			}
			if n := len(upstream.Calls()); n != tt.wantCalls {
				t.Fatalf("upstream accepted %d tool calls, want %d", n, tt.wantCalls)
			}
		})
	}
}
