// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

func TestLifecycleCommandTreeExposesRequiredCommands(t *testing.T) {
	format := "json"
	commands := NewCommands(Options{OutputFormat: &format, Stdout: &bytes.Buffer{}})
	root := make(map[string]map[string]bool, len(commands))
	for _, group := range commands {
		children := make(map[string]bool)
		for _, child := range group.Commands() {
			children[child.Name()] = true
		}
		root[group.Name()] = children
	}
	for group, child := range map[string]string{
		"backup": "create", "config": "reconcile", "upgrade": "preflight",
		"tenant": "shred", "vault": "rotate-kek",
	} {
		if !root[group][child] {
			t.Errorf("command tree is missing %s %s", group, child)
		}
	}
	for _, child := range []string{"create", "verify", "restore"} {
		if !root["backup"][child] {
			t.Errorf("backup command tree is missing %s", child)
		}
	}
}

func TestUpgradePreflightDoesNotCreateMissingDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "missing.sqlite")
	lockPath := filepath.Join(dir, "tanpopo.lock")
	if err := os.WriteFile(lockPath, []byte("lockfileVersion: 1\nextensions: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runPreflight(context.Background(), dbPath, lockPath, "0.2.0"); err == nil {
		t.Fatal("preflight accepted a missing database")
	}
	if _, err := os.Lstat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("preflight created or changed a missing database: %v", err)
	}
}

func TestUpgradePreflightBlocksIncompatibleExtensionWithoutSideEffects(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tanpopo.db")
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("open setup database: %v", err)
	}
	now := time.Now().UTC()
	nowMS := now.UnixMilli()
	if _, err := store.RawDB().ExecContext(ctx, `
		INSERT INTO extension_packages (id, canonical_name, publisher_id, origin_trust_tier, created_at_ms)
		VALUES ('ext_preflight', '@test/incompatible', 'test-publisher', 'verified', ?)`, nowMS); err != nil {
		_ = store.Close()
		t.Fatalf("seed extension package: %v", err)
	}
	if _, err := store.RawDB().ExecContext(ctx, `
		INSERT INTO extension_versions (id, package_id, semver, contract_version, core_version_range, execution_tier, bundle_sha256, source_uri, manifest_json, created_at_ms)
		VALUES ('exv_preflight', 'ext_preflight', '1.0.0', '0.1', '<0.2.0', 'WASM_SANDBOX_T2', ?, 'https://example.test/ext.tpkg', '{}', ?)`, strings.Repeat("a", 64), nowMS); err != nil {
		_ = store.Close()
		t.Fatalf("seed extension version: %v", err)
	}
	if _, err := store.RawDB().ExecContext(ctx, `
		INSERT INTO extension_installations (id, package_id, active_version_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms)
		VALUES ('ins_preflight', 'ext_preflight', 'exv_preflight', 'ENABLED_ACTIVE', '{}', 1, ?, ?)`, nowMS, nowMS); err != nil {
		_ = store.Close()
		t.Fatalf("seed active extension: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close setup database: %v", err)
	}
	lockPath := filepath.Join(dir, "tanpopo.lock")
	lf := lockfile.NewEmpty(now)
	lf.Extensions["@test/incompatible"] = lockfile.LockedEntry{
		Version: "1.0.0", Origin: "https://example.test/ext.tpkg", ArtifactSHA256: strings.Repeat("a", 64),
		ContractVersion: "0.1", TrustTier: lockfile.TrustTierVerified, ExecutionTier: "WASM_SANDBOX_T2",
		ApprovedPermissions: lockfile.ApprovedPermissions{ApprovedBy: "test-operator"},
	}
	if err := lockfile.WriteAtomic(lockPath, lf); err != nil {
		t.Fatalf("write setup lockfile: %v", err)
	}
	beforeDB, err := hashFileSHA256(dbPath)
	if err != nil {
		t.Fatalf("hash database before preflight: %v", err)
	}
	beforeLock, err := hashFileSHA256(lockPath)
	if err != nil {
		t.Fatalf("hash lockfile before preflight: %v", err)
	}

	var output bytes.Buffer
	format := "json"
	root := &cobra.Command{Use: "tanpopo"}
	for _, command := range NewCommands(Options{DBPath: dbPath, LockfilePath: lockPath, OutputFormat: &format, Stdout: &output}) {
		root.AddCommand(command)
	}
	root.SetArgs([]string{"upgrade", "preflight", "--target-version", "0.3.0"})
	err = root.ExecuteContext(ctx)
	if err == nil {
		t.Fatal("preflight accepted an incompatible N-2 extension")
	}
	if !strings.Contains(err.Error(), "BLOCKED_INCOMPATIBLE") {
		t.Fatalf("preflight error=%v, want deterministic BLOCKED_INCOMPATIBLE status", err)
	}
	if got := testharness.CodeOf(err); got != testharness.ExitContract {
		t.Fatalf("preflight exit code=%d, want deterministic contract-blocked code %d", got, testharness.ExitContract)
	}
	var result struct {
		Compatible bool   `json:"compatible"`
		State      string `json:"state"`
		Blocking   []struct {
			PackageID string `json:"package_id"`
		} `json:"blocking_extensions"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode preflight output %q: %v", output.String(), err)
	}
	if result.Compatible || result.State != "BLOCKED_INCOMPATIBLE" || len(result.Blocking) != 1 || result.Blocking[0].PackageID != "@test/incompatible" {
		t.Fatalf("preflight output=%s, want one incompatible extension blocker", output.String())
	}
	afterDB, err := hashFileSHA256(dbPath)
	if err != nil {
		t.Fatalf("hash database after preflight: %v", err)
	}
	afterLock, err := hashFileSHA256(lockPath)
	if err != nil {
		t.Fatalf("hash lockfile after preflight: %v", err)
	}
	if afterDB != beforeDB || afterLock != beforeLock {
		t.Fatalf("blocked preflight changed inputs: database %s -> %s, lockfile %s -> %s", beforeDB, afterDB, beforeLock, afterLock)
	}
}

func hashFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
