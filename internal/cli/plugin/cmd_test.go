// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package plugin_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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

func TestPluginReleaseSignAndVerifyRoundtrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pkix, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	privFile := filepath.Join(tmp, "root.key.pem")
	pubFile := filepath.Join(tmp, "root.pub.pem")
	sigFile := filepath.Join(tmp, "release-signatures.json")
	if err := os.WriteFile(privFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pubFile, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkix}), 0o644); err != nil {
		t.Fatal(err)
	}

	// Missing private key fails closed without generating or embedding a key.
	if _, _, badCode := runPluginCLI(t, "release-sign", "--builtin", "--out", sigFile); badCode == 0 {
		t.Fatalf("expected missing --private-key-file to fail")
	}

	out, stderr, code := runPluginCLI(t, "--output", "json", "release-sign",
		"--private-key-file", privFile,
		"--builtin",
		"--out", sigFile,
	)
	if code != 0 {
		t.Fatalf("release-sign --builtin failed: code=%d stderr=%s", code, stderr)
	}
	var signRes struct {
		SignedCount int `json:"signed_count"`
	}
	if err := json.Unmarshal([]byte(out), &signRes); err != nil || signRes.SignedCount == 0 {
		t.Fatalf("unexpected release-sign json: err=%v out=%s", err, out)
	}

	vOut, vErr, vCode := runPluginCLI(t, "--output", "json", "release-verify",
		"--signatures", sigFile,
		"--root-pubkey-file", pubFile,
		"--builtin",
	)
	if vCode != 0 {
		t.Fatalf("release-verify --builtin failed: code=%d stderr=%s out=%s", vCode, vErr, vOut)
	}
	var verRes struct {
		Status        string `json:"status"`
		VerifiedCount int    `json:"verified_count"`
	}
	if err := json.Unmarshal([]byte(vOut), &verRes); err != nil || verRes.Status != "VERIFIED" || verRes.VerifiedCount != signRes.SignedCount {
		t.Fatalf("unexpected release-verify result: err=%v out=%s", err, vOut)
	}

	// Wrong public key must fail verification.
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	otherPKIX, _ := x509.MarshalPKIXPublicKey(otherPub)
	wrongPubFile := filepath.Join(tmp, "wrong.pub.pem")
	_ = os.WriteFile(wrongPubFile, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: otherPKIX}), 0o644)
	if _, _, badVer := runPluginCLI(t, "release-verify", "--signatures", sigFile, "--root-pubkey-file", wrongPubFile, "--builtin"); badVer == 0 {
		t.Fatalf("expected release-verify with mismatched root pubkey to fail")
	}

	// Standalone archive signing & verification roundtrip.
	tpkgFile := filepath.Join(tmp, "custom.tpkg")
	if err := os.WriteFile(tpkgFile, []byte("synthetic-tpkg-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	customSig := filepath.Join(tmp, "custom-sigs.json")
	if _, stderr, code := runPluginCLI(t, "release-sign", "--private-key-file", privFile, "--package-id", "@tanpopo/custom", "--version", "1.2.3", "--archive", tpkgFile, "--out", customSig); code != 0 {
		t.Fatalf("standalone release-sign failed: code=%d stderr=%s", code, stderr)
	}
	if _, stderr, code := runPluginCLI(t, "release-verify", "--signatures", customSig, "--root-pubkey-file", pubFile, "--package-id", "@tanpopo/custom", "--version", "1.2.3", "--archive", tpkgFile); code != 0 {
		t.Fatalf("standalone release-verify failed: code=%d stderr=%s", code, stderr)
	}

	// Duplicate/stale record in built-in release manifest must fail release-verify --builtin.
	staleSigFile := filepath.Join(tmp, "stale-sigs.json")
	rawSigs, err := os.ReadFile(sigFile)
	if err != nil {
		t.Fatal(err)
	}
	var parsedManifest struct {
		Signatures []map[string]any `json:"signatures"`
	}
	if err := json.Unmarshal(rawSigs, &parsedManifest); err != nil {
		t.Fatal(err)
	}
	parsedManifest.Signatures = append(parsedManifest.Signatures, map[string]any{
		"package_id":            "@tanpopo/stale",
		"version":               "0.0.1",
		"archive_sha256":        strings.Repeat("b", 64),
		"root_signature_base64": parsedManifest.Signatures[0]["root_signature_base64"],
	})
	staleRaw, _ := json.Marshal(parsedManifest)
	if err := os.WriteFile(staleSigFile, staleRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, badStale := runPluginCLI(t, "release-verify", "--signatures", staleSigFile, "--root-pubkey-file", pubFile, "--builtin"); badStale == 0 {
		t.Fatalf("expected release-verify --builtin to reject stale/orphan signature record")
	}

	// Re-signing --builtin over an existing manifest with stale entries replaces authoritatively.
	if _, stderr, code := runPluginCLI(t, "release-sign", "--private-key-file", privFile, "--builtin", "--out", staleSigFile); code != 0 {
		t.Fatalf("re-sign --builtin failed: code=%d stderr=%s", code, stderr)
	}
	if _, stderr, code := runPluginCLI(t, "release-verify", "--signatures", staleSigFile, "--root-pubkey-file", pubFile, "--builtin"); code != 0 {
		t.Fatalf("release-verify after authoritative --builtin re-sign failed: code=%d stderr=%s", code, stderr)
	}

	// Re-signing an updated --archive with same package_id@version replaces prior digest cleanly.
	if err := os.WriteFile(tpkgFile, []byte("updated-tpkg-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runPluginCLI(t, "release-sign", "--private-key-file", privFile, "--package-id", "@tanpopo/custom", "--version", "1.2.3", "--archive", tpkgFile, "--out", customSig); code != 0 {
		t.Fatalf("re-sign --archive failed: code=%d stderr=%s", code, stderr)
	}
	if _, stderr, code := runPluginCLI(t, "release-verify", "--signatures", customSig, "--root-pubkey-file", pubFile, "--package-id", "@tanpopo/custom", "--version", "1.2.3", "--archive", tpkgFile); code != 0 {
		t.Fatalf("release-verify after archive re-sign failed: code=%d stderr=%s", code, stderr)
	}
}

