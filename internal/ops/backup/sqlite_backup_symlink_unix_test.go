// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build linux

package backup

import (
	"context"
	"os"
	"path/filepath"
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
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedOutput := filepath.Join(root, "backups")
	if err := os.Symlink(outside, linkedOutput); err != nil {
		t.Fatalf("create backup directory symlink: %v", err)
	}
	if _, err := CreateSQLite(ctx, store, keys, linkedOutput, lockfile, config, 1_795_000_000_000); err == nil {
		t.Fatal("CreateSQLite accepted a symlink backup directory")
	}

	realOutput := filepath.Join(root, "real-backups")
	if err := os.Mkdir(realOutput, 0o700); err != nil {
		t.Fatal(err)
	}
	plainLockfile := filepath.Join(root, "plain-lockfile")
	if err := os.WriteFile(plainLockfile, []byte("lockfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedLockfile := filepath.Join(root, "tanpopo.lock")
	if err := os.Symlink(plainLockfile, linkedLockfile); err != nil {
		t.Fatalf("create lockfile symlink: %v", err)
	}
	if _, err := CreateSQLite(ctx, store, keys, realOutput, linkedLockfile, config, 1_795_000_000_000); err == nil {
		t.Fatal("CreateSQLite accepted a symlink lockfile")
	}
}
