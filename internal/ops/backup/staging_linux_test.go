//go:build linux

// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package backup

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCleanupAbandonedStagingRootsOnlyRemovesMatchingNamespace(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close fixture root: %v", err)
		}
	})
	staleNamespace := strings.Repeat("a", 64)
	otherNamespace := strings.Repeat("b", 64)
	staleName := fmt.Sprintf(".tanpopo-backup-%d-%s-%032x", os.Geteuid(), staleNamespace, 1)
	otherName := fmt.Sprintf(".tanpopo-backup-%d-%s-%032x", os.Geteuid(), otherNamespace, 2)
	for _, name := range []string{staleName, otherName} {
		if err := root.Mkdir(name, 0o700); err != nil {
			t.Fatal(err)
		}
		f, err := root.OpenFile(name+"/database.sqlite", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("temporary snapshot")); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = removeTree(root, otherName)
	})
	if err := cleanupAbandonedStagingRoots(root, staleNamespace); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Lstat(staleName); !os.IsNotExist(err) {
		t.Fatalf("abandoned staging directory remains, stat err=%v", err)
	}
	if _, err := root.Lstat(otherName); err != nil {
		t.Fatalf("other output namespace was removed: %v", err)
	}
}
