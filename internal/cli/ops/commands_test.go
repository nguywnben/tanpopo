// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package ops

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
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
