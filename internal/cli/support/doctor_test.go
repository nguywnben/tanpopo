// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package support

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/spf13/cobra"
)

func TestDoctorCommandReportsEightEnvironmentalChecksAsJSON(t *testing.T) {
	t.Setenv("TANPOPO_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("TANPOPO_MASTER_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	t.Setenv("TANPOPO_EXPECTED_TIME", time.Now().UTC().Format(time.RFC3339))
	rootDir := t.TempDir()
	store, err := sqlite.Open(t.Context(), sqlite.Config{DBPath: filepath.Join(rootDir, "tanpopo.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lockfile.WriteAtomic(filepath.Join(rootDir, "tanpopo.lock"), lockfile.NewEmpty(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
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
	if len(report.Checks) != 8 {
		t.Fatalf("doctor returned %d checks; want the eight SPEC-OPS-01 checks: %#v", len(report.Checks), statuses)
	}
	if statuses["egress-dns-ssrf"] != "pass" || statuses["tls-posture"] != "warn" || statuses["master-kek-ready"] != "pass" {
		t.Fatalf("unexpected environmental doctor results: %#v", statuses)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "tanpopo.yaml")); !os.IsNotExist(err) {
		t.Fatalf("doctor mutated its data root: %v", err)
	}
}

func TestDoctorRejectsInsecureMasterKEKFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		return // Windows uses ACL-based key-file checks, covered by the Windows keyfile tests.
	}
	rootDir := t.TempDir()
	keyPath := filepath.Join(rootDir, "master.key")
	if err := os.WriteFile(keyPath, []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANPOPO_MASTER_KEY", "")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", keyPath)
	checks := make(map[string]string)
	for _, check := range mustCollectDoctorReport(t, rootDir).Checks {
		checks[check.Name] = check.Status
	}
	if checks["master-kek-ready"] != "fail" {
		t.Fatalf("doctor accepted insecure Master KEK permissions: %#v", checks)
	}
}

func mustCollectDoctorReport(t *testing.T, root string) doctorReportDocument {
	t.Helper()
	report, err := collectDoctorReport(t.Context(), root, nil)
	if err != nil {
		t.Fatalf("collect doctor report: %v", err)
	}
	return report
}

func TestDoctorDoesNotProbeNonLoopbackTANPOPOURL(t *testing.T) {
	t.Setenv("TANPOPO_URL", "https://admin.example.test")
	checks := probeLocalHealth(t.Context())
	if len(checks) != 2 || checks[0].Status != "not_probed_nonlocal" || checks[1].Status != "not_probed_nonlocal" {
		t.Fatalf("non-loopback URL was not excluded from probes: %#v", checks)
	}
}
