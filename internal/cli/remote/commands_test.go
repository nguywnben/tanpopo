// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package remote

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/spf13/cobra"
)

func TestReadCommandUsesAdminAPIAndMachineOutput(t *testing.T) {
	var gotAuth, gotOrg, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotOrg, gotPath = r.Header.Get("Authorization"), r.Header.Get("X-Tanpopo-Org-Id"), r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"name":"safe","status":"ready"}]}`))
	}))
	defer server.Close()

	format := "json"
	var stdout, stderr bytes.Buffer
	cmd := rootForTest(t, Options{
		OutputFormat: &format,
		Stdout:       &stdout,
		Stderr:       &stderr,
		Client:       server.Client(),
		LookupEnv: func(key string) (string, bool) {
			values := map[string]string{"TANPOPO_URL": server.URL, "TANPOPO_ADMIN_TOKEN": "test-token", "TANPOPO_ORG": "org_test"}
			value, ok := values[key]
			return value, ok
		},
	})
	cmd.SetArgs([]string{"provider", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if gotAuth != "Bearer test-token" || gotOrg != "org_test" || gotPath != "/admin/v1/provider-adapters" {
		t.Fatalf("unexpected Admin API request: auth=%q org=%q path=%q", gotAuth, gotOrg, gotPath)
	}
	var decoded any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not one JSON value: %v (%q)", err, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("machine output wrote progress to stderr: %q", stderr.String())
	}
}

func TestReadCommandRequiresTokenAndUsesCanonicalExitCodes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := rootForTest(t, Options{
		Stdout: &stdout, Stderr: &stderr,
		LookupEnv: func(key string) (string, bool) {
			if key == "TANPOPO_URL" {
				return "http://127.0.0.1:8080", true
			}
			return "", false
		},
	})
	cmd.SetArgs([]string{"config", "show"})
	err := cmd.Execute()
	if testharness.CodeOf(err) != testharness.ExitSecurity {
		t.Fatalf("expected security exit code, got %d (%v)", testharness.CodeOf(err), err)
	}
}

func TestTableOutputStripsTerminalEscapes(t *testing.T) {
	var output bytes.Buffer
	if err := writeValue(&output, "table", map[string]any{"data": []any{map[string]any{"name": "safe\x1b]52;c;clipboard\x07"}}}); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(output.String(), '\x1b') {
		t.Fatalf("terminal escape reached table output: %q", output.String())
	}
}

func rootForTest(t *testing.T, opts Options) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "tanpopo", SilenceErrors: true, SilenceUsage: true}
	for _, command := range NewCommands(opts) {
		root.AddCommand(command)
	}
	return root
}
