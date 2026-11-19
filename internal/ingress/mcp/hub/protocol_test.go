// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package hub

import (
	"encoding/json"
	"testing"
)

func TestParseRequestRejectsMalformedJSONRPC(t *testing.T) {
	t.Parallel()
	cases := []string{
		``, `null`, `{"jsonrpc":"1.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":null,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":{},"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"} trailing`,
		`{"jsonrpc":"2.0","id":1,"method":""}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","unknown":true}`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if _, err := parseRequest([]byte(input)); err == nil {
				t.Fatalf("parseRequest(%q) succeeded", input)
			}
		})
	}
}

func TestNamespacedToolsCannotShadowAcrossServers(t *testing.T) {
	t.Parallel()
	a, err := toolName("github", "create_issue")
	if err != nil {
		t.Fatal(err)
	}
	b, err := toolName("jira", "create_issue")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || a != "github__create_issue" || b != "jira__create_issue" {
		t.Fatalf("namespaced names collide or are malformed: %q %q", a, b)
	}
	slug, name, ok := splitToolName(a)
	if !ok || slug != "github" || name != "create_issue" {
		t.Fatalf("splitToolName(%q) = %q, %q, %v", a, slug, name, ok)
	}
	if _, err := toolName("bad__slug", "x"); err == nil {
		t.Fatal("server slug containing delimiter was accepted")
	}
}

func TestCursorBindsScopeFilterAndIntegrity(t *testing.T) {
	t.Parallel()
	key := []byte("0123456789abcdef0123456789abcdef")
	cursor := encodeCursor(key, "org:project:env:key", "tools/list", 125)
	offset, err := decodeCursor(key, cursor, "org:project:env:key", "tools/list")
	if err != nil || offset != 125 {
		t.Fatalf("decodeCursor() = %d, %v", offset, err)
	}
	for _, tc := range []struct{ scope, filter string }{{"other", "tools/list"}, {"org:project:env:key", "tools/call"}} {
		if _, err := decodeCursor(key, cursor, tc.scope, tc.filter); err == nil {
			t.Fatalf("cursor was accepted for scope=%q filter=%q", tc.scope, tc.filter)
		}
	}
	mutated := cursor[:len(cursor)-1] + "A"
	if _, err := decodeCursor(key, mutated, "org:project:env:key", "tools/list"); err == nil {
		t.Fatal("tampered cursor was accepted")
	}
}

func TestMCPContentNegotiationHonorsQualityValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		header string
		want   bool
	}{
		{"application/json, text/event-stream", true},
		{"application/json;q=0, text/event-stream", false},
		{"application/json;q=bogus, text/event-stream", false},
		{"application/jsonp, text/event-stream", false},
	}
	for _, tc := range cases {
		t.Run(tc.header, func(t *testing.T) {
			if got := acceptsMediaType([]string{tc.header}, "application/json"); got != tc.want {
				t.Fatalf("acceptsMediaType(%q) = %v, want %v", tc.header, got, tc.want)
			}
		})
	}
}

func FuzzMCPProtocol(f *testing.F) {
	for _, seed := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":null,"method":"tools/call"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":"!"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"a__b","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"} {}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		parsed, err := parseRequest(input)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(parsed)
		if err != nil {
			t.Fatalf("marshal parsed request: %v", err)
		}
		if len(encoded) > maxJSONRPCBytes*2 {
			t.Fatalf("unexpected expansion: %d bytes", len(encoded))
		}
	})
}
