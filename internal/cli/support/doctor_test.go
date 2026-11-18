// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package support

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestDoctorCommandReportsLoopbackHealthProbesAsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/readyz":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("TANPOPO_URL", server.URL)
	rootDir := t.TempDir()
	var output bytes.Buffer
	format := "json"
	root := &cobra.Command{Use: "tanpopo"}
	root.AddCommand(NewDoctorCommand(Options{RootDir: rootDir, OutputFormat: &format, Stdout: &output}))
	root.SetArgs([]string{"doctor"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var report doctorReportDocument
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("doctor did not emit one JSON report: %v (%q)", err, output.String())
	}
	statuses := make(map[string]string, len(report.Checks))
	for _, check := range report.Checks {
		statuses[check.Name] = check.Status
	}
	if statuses["process_liveness"] != "ok" || statuses["service_readiness"] != "not_ready" {
		t.Fatalf("unexpected health probe results: %#v", statuses)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "tanpopo.yaml")); !os.IsNotExist(err) {
		t.Fatalf("doctor mutated its data root: %v", err)
	}
}

func TestDoctorDoesNotProbeNonLoopbackTANPOPOURL(t *testing.T) {
	t.Setenv("TANPOPO_URL", "https://admin.example.test")
	checks := probeLocalHealth(t.Context())
	if len(checks) != 2 || checks[0].Status != "not_probed_nonlocal" || checks[1].Status != "not_probed_nonlocal" {
		t.Fatalf("non-loopback URL was not excluded from probes: %#v", checks)
	}
}
