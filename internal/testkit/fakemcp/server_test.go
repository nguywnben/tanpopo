// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package fakemcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamableHTTPInitializePaginateAndCall(t *testing.T) {
	tools := make([]Tool, 251)
	for i := range tools {
		tools[i] = Tool{Name: fmt.Sprintf("tool_%03d", i), Description: "safe", InputSchema: json.RawMessage(`{"type":"object"}`)}
	}
	server := New(tools, 100)
	request := func(value any, session string) (int, http.Header, map[string]json.RawMessage) {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		resp := httptest.NewRecorder()
		server.ServeHTTP(resp, req)
		var payload map[string]json.RawMessage
		if resp.Code == http.StatusOK && json.Unmarshal(resp.Body.Bytes(), &payload) != nil {
			t.Fatal("invalid JSON-RPC response")
		}
		return resp.Code, resp.Header(), payload
	}

	initStatus, initHeader, initPayload := request(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-03-26"}}, "")
	if initStatus != http.StatusOK || initHeader.Get("Mcp-Session-Id") == "" || len(initPayload["result"]) == 0 {
		t.Fatalf("initialize failed: status=%d session=%q", initStatus, initHeader.Get("Mcp-Session-Id"))
	}
	session := initHeader.Get("Mcp-Session-Id")
	request(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, session)

	var cursor string
	count := 0
	for page := 0; page < 3; page++ {
		params := map[string]string{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		_, _, payload := request(map[string]any{"jsonrpc": "2.0", "id": page + 2, "method": "tools/list", "params": params}, session)
		var result struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(payload["result"], &result); err != nil {
			t.Fatal(err)
		}
		count += len(result.Tools)
		cursor = result.NextCursor
		if cursor == "" {
			break
		}
	}
	if count != 251 {
		t.Fatalf("expected all 251 tools across pages, got %d", count)
	}
	_, _, callPayload := request(map[string]any{"jsonrpc": "2.0", "id": 8, "method": "tools/call", "params": map[string]any{"name": "tool_000", "arguments": map[string]any{}}}, session)
	if len(callPayload["result"]) == 0 || len(server.Calls()) != 1 || server.Calls()[0].Name != "tool_000" {
		t.Fatal("tools/call did not reach fake server")
	}
}
