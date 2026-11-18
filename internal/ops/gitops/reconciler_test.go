// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package gitops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
)

func TestReconcileAtomicallyAppliesConfigAndLeavesLockImmutable(t *testing.T) {
	dir := t.TempDir()
	opts, lockBefore := makeFixture(t, dir, "provider:\n  api_key: ${ENV:PROVIDER_API_KEY}\n")
	if err := Reconcile(context.Background(), opts); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got, err := os.ReadFile(opts.AppliedConfigPath)
	if err != nil || string(got) != "provider:\n  api_key: ${ENV:PROVIDER_API_KEY}\n" {
		t.Fatalf("applied config = %q, err=%v", got, err)
	}
	lockAfter, err := os.ReadFile(opts.LockfilePath)
	if err != nil || string(lockAfter) != string(lockBefore) {
		t.Fatalf("immutable lock changed, err=%v", err)
	}
}

func TestReconcileRejectsInlineAPIKeyWithoutLeakingValue(t *testing.T) {
	dir := t.TempDir()
	const secret = "inline-secret-value-never-echo-this"
	opts, _ := makeFixture(t, dir, "provider:\n  api_key: "+secret+"\n")
	err := Reconcile(context.Background(), opts)
	if !errors.Is(err, ErrUnsafeConfig) {
		t.Fatalf("Reconcile error = %v, want ErrUnsafeConfig", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("error exposed inline secret")
	}
	got, readErr := os.ReadFile(opts.AppliedConfigPath)
	if readErr != nil || string(got) != "old: applied\n" {
		t.Fatalf("invalid config modified applied file: %q, %v", got, readErr)
	}
}

func TestReconcileRejectsPartialAndMalformedEnvironmentReferences(t *testing.T) {
	for _, value := range []string{"prefix-${ENV:KEY}", "${ENV:BAD-NAME}", "${ENV:KEY}/suffix"} {
		t.Run(value, func(t *testing.T) {
			err := validateConfig([]byte("provider:\n  api_key: \"" + value + "\"\n"))
			if !errors.Is(err, ErrUnsafeConfig) {
				t.Fatalf("validateConfig(%q) = %v", value, err)
			}
		})
	}
}

func TestConfigRejectsNonStringSecretValuesAndDuplicateKeys(t *testing.T) {
	for _, raw := range []string{
		"provider:\n  api_key: true\n",
		"provider:\n  api_key: ${ENV:KEY}\n  api_key: ${ENV:OTHER}\n",
	} {
		if err := validateConfig([]byte(raw)); !errors.Is(err, ErrUnsafeConfig) {
			t.Fatalf("validateConfig(%q) = %v, want ErrUnsafeConfig", raw, err)
		}
	}
}

func TestReconcileRejectsTamperedPackageBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	opts, _ := makeFixture(t, dir, "service: demo\n")
	if err := os.WriteFile(opts.ArtifactPaths["@example/plugin"], []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Reconcile(context.Background(), opts)
	if !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("Reconcile error = %v, want ErrArtifactMismatch", err)
	}
	got, readErr := os.ReadFile(opts.AppliedConfigPath)
	if readErr != nil || string(got) != "old: applied\n" {
		t.Fatalf("digest failure modified applied file: %q, %v", got, readErr)
	}
}

func TestReconcileRejectsNonRegularAppliedTarget(t *testing.T) {
	dir := t.TempDir()
	opts, _ := makeFixture(t, dir, "service: demo\n")
	if err := os.Remove(opts.AppliedConfigPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(opts.AppliedConfigPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Reconcile(context.Background(), opts); !errors.Is(err, ErrApply) {
		t.Fatalf("Reconcile error = %v, want ErrApply", err)
	}
	info, err := os.Stat(opts.AppliedConfigPath)
	if err != nil || !info.IsDir() {
		t.Fatalf("non-regular target changed: info=%v, err=%v", info, err)
	}
}

func makeFixture(t *testing.T, dir, desired string) (Options, []byte) {
	t.Helper()
	desiredPath := filepath.Join(dir, "desired.yaml")
	appliedPath := filepath.Join(dir, "applied.yaml")
	lockPath := filepath.Join(dir, "tanpopo.lock")
	artifactPath := filepath.Join(dir, "plugin.tpkg")
	artifact := []byte("package archive bytes")
	if err := os.WriteFile(desiredPath, []byte(desired), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appliedPath, []byte("old: applied\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, artifact, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(artifact)
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	lf := lockfile.NewEmpty(now)
	lf.Extensions["@example/plugin"] = lockfile.LockedEntry{
		Version: "1.0.0", Origin: "https://example.invalid/plugin.tpkg",
		ArtifactSHA256: hex.EncodeToString(digest[:]), ContractVersion: "0.1",
		TrustTier: lockfile.TrustTierOfficial, ExecutionTier: "DECLARATIVE_T1",
		ApprovedPermissions: lockfile.ApprovedPermissions{ApprovedBy: "test"},
	}
	if err := lockfile.WriteAtomic(lockPath, lf); err != nil {
		t.Fatal(err)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	return Options{
		DesiredConfigPath: desiredPath,
		AppliedConfigPath: appliedPath,
		LockfilePath:      lockPath,
		ArtifactPaths:     map[string]string{"@example/plugin": artifactPath},
	}, lockBytes
}
