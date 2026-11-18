// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build linux

package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateSQLiteRejectsSymlinkDestinationsAndLockfiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	keys := &testKeyWrapper{}
	store := testSnapshotter{content: []byte("snapshot")}
	lockfile := filepath.Join(root, "tanpopo.lock")
	if err := os.WriteFile(lockfile, []byte("lockfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "tanpopo.yaml")
	if err := os.WriteFile(config, []byte("data_dir: /var/lib/tanpopo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifactDir := filepath.Join(root, "plugins")
	if err := os.Mkdir(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedOutput := filepath.Join(root, "backups")
	if err := os.Symlink(outside, linkedOutput); err != nil {
		t.Fatalf("create backup directory symlink: %v", err)
	}
	if _, err := CreateSQLite(ctx, store, keys, linkedOutput, lockfile, config, artifactDir, 1_795_000_000_000); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("CreateSQLite err=%v; want symlink rejection for backup directory", err)
	}

	realOutput := filepath.Join(root, "real-backups")
	if err := os.Mkdir(realOutput, 0o700); err != nil {
		t.Fatal(err)
	}
	plainLockfile := filepath.Join(root, "plain-lockfile")
	if err := os.WriteFile(plainLockfile, []byte("lockfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedLockfile := filepath.Join(root, "linked-lockfile")
	if err := os.Symlink(plainLockfile, linkedLockfile); err != nil {
		t.Fatalf("create lockfile symlink: %v", err)
	}
	if _, err := CreateSQLite(ctx, store, keys, realOutput, linkedLockfile, config, artifactDir, 1_795_000_000_000); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("CreateSQLite err=%v; want symlink rejection for lockfile", err)
	}
	if err := os.Symlink(plainLockfile, filepath.Join(artifactDir, "linked.wasm")); err != nil {
		t.Fatalf("create extension artifact symlink: %v", err)
	}
	if _, err := CreateSQLite(ctx, store, keys, realOutput, lockfile, config, artifactDir, 1_795_000_000_000); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("CreateSQLite err=%v; want symlink rejection for extension artifact", err)
	}
}
