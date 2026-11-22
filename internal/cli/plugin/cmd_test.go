// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package plugin_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/nguywnben/tanpopo/internal/cli/plugin"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func runPluginCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	var outFmt string

	root := &cobra.Command{Use: "tanpopo", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&outFmt, "output", "table", "")
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.AddCommand(plugin.NewCommand(plugin.Options{
		OutputFormat: &outFmt,
		Stdout:       &stdout,
		Stderr:       &stderr,
	}))
	root.SetArgs(append([]string{"plugin"}, args...))

	err := root.Execute()
	return stdout.String(), stderr.String(), testharness.CodeOf(err)
}

func TestPluginCLITierAliasesAndJSONOutput(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "my-cli-adapter")
	out, stderr, code := runPluginCLI(t, "--output", "json", "init", "my-cli-adapter", "--tier", "wasm", "--dir", workDir)
	if code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, stderr)
	}
	var initRes testharness.ScaffoldResult
	if err := json.Unmarshal([]byte(out), &initRes); err != nil {
		t.Fatalf("decode init json: %v\nout=%s", err, out)
	}
	if initRes.Tier != "sandboxed_wasm" {
		t.Fatalf("expected tier sandboxed_wasm, got %q", initRes.Tier)
	}

	_, stderr, code = runPluginCLI(t, "validate", workDir)
	if code != 0 {
		t.Fatalf("validate failed: code=%d stderr=%s", code, stderr)
	}

	out, stderr, code = runPluginCLI(t, "--output", "json", "test", workDir)
	if code != 0 {
		t.Fatalf("test failed: code=%d stderr=%s", code, stderr)
	}
	var report testharness.TestReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode test report json: %v\nout=%s", err, out)
	}
	if report.Status != "PASSED" || report.CapabilityBadges["chat_stream"] != "TESTED" {
		t.Fatalf("unexpected test report: %+v", report)
	}

	tpkgPath := filepath.Join(t.TempDir(), "bundle.tpkg")
	out, stderr, code = runPluginCLI(t, "--output", "json", "pack", workDir, "-o", tpkgPath)
	if code != 0 {
		t.Fatalf("pack failed: code=%d stderr=%s", code, stderr)
	}
	var packRes testharness.PackResult
	if err := json.Unmarshal([]byte(out), &packRes); err != nil {
		t.Fatalf("decode pack json: %v\nout=%s", err, out)
	}
	if packRes.SHA256 == "" {
		t.Fatalf("expected non-empty SHA-256")
	}

	out, stderr, code = runPluginCLI(t, "--output", "json", "migrate-check", workDir)
	if code != 0 {
		t.Fatalf("migrate-check failed: code=%d stderr=%s", code, stderr)
	}
	var migRep testharness.MigrateCheckReport
	if err := json.Unmarshal([]byte(out), &migRep); err != nil {
		t.Fatalf("decode migrate-check json: %v\nout=%s", err, out)
	}
	if !migRep.Compatible || migRep.Status != "COMPATIBLE" {
		t.Fatalf("unexpected migrate-check report: %+v", migRep)
	}

	_, _, badCode := runPluginCLI(t, "--output", "json", "migrate-check", workDir, "--target-core", "9.0.0")
	if badCode != testharness.ExitContract {
		t.Fatalf("expected ExitContract (%d) on incompatible target-core, got %d", testharness.ExitContract, badCode)
	}

	mPath := filepath.Join(workDir, "manifest.yaml")
	rawManifest, err := os.ReadFile(mPath)
	if err != nil {
		t.Fatal(err)
	}
	widened := strings.Replace(string(rawManifest), ">=0.1.0 <0.2.0", ">=0.1.0 <0.3.0", 1)
	if err := os.WriteFile(mPath, []byte(widened), 0o600); err != nil {
		t.Fatal(err)
	}
	yamlOut, yamlErr, n1Code := runPluginCLI(t, "--output", "yaml", "migrate-check", workDir, "--target-core", "0.2.0", "--supported-contracts", "0.2,0.1")
	if n1Code != 0 || !strings.Contains(yamlOut, "COMPATIBLE_WITH_DEPRECATION_WARNING") {
		t.Fatalf("expected N-1 deprecation warning in yaml output, code=%d stderr=%s out=%s", n1Code, yamlErr, yamlOut)
	}

	sanitized := testharness.SanitizeTerminalText("hello\x1b]52;c;ZGF0YQ==\x07world\x1b[31m!")
	if strings.Contains(sanitized, "\x1b") || sanitized != "helloworld!" {
		t.Fatalf("CT-27 terminal escape stripping failed: %q", sanitized)
	}
}
