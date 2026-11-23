// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/extension/testharness"
)

func TestDEF004_ConfigShowAndReconcileNoShadowing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/v1/overview/summary" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"window":"24h","total_requests":7}`))
	}))
	defer srv.Close()

	t.Setenv("TANPOPO_URL", srv.URL)
	t.Setenv("TANPOPO_ADMIN_TOKEN", "test-token")

	var stdout, stderr bytes.Buffer
	code := runWithContext(context.Background(), []string{"--output", "json", "config", "show"}, &stdout, &stderr)
	if code != testharness.ExitOK {
		t.Fatalf("tanpopo config show failed: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"total_requests":7`) {
		t.Fatalf("expected overview summary payload from config show, got %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	helpCode := runWithContext(context.Background(), []string{"config", "--help"}, &stdout, &stderr)
	if helpCode != testharness.ExitOK {
		t.Fatalf("tanpopo config --help failed: exit=%d stderr=%q", helpCode, stderr.String())
	}
	helpOut := stdout.String()
	if !strings.Contains(helpOut, "show") || !strings.Contains(helpOut, "reconcile") {
		t.Fatalf("expected both show and reconcile under tanpopo config --help, got:\n%s", helpOut)
	}

	// Also verify reconcile remains reachable (validation failure proves subcommand resolved instead of Help).
	tmp := t.TempDir()
	missingConfig := filepath.Join(tmp, "missing.yaml")
	if _, err := os.Stat(missingConfig); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	recCode := runWithContext(context.Background(), []string{"config", "reconcile", "--desired", missingConfig, "--applied", filepath.Join(tmp, "applied.yaml"), "--lockfile", filepath.Join(tmp, "tanpopo.lock")}, &stdout, &stderr)
	if recCode == testharness.ExitOK {
		t.Fatalf("expected reconcile on missing desired file to fail closed, got OK")
	}
}
