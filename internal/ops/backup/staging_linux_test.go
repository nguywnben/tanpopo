//go:build linux

// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package backup

import (
	"fmt"
	"os"
	"testing"
)

func TestCleanupAbandonedStagingRootsOnlyRemovesDeadOwners(t *testing.T) {
	root, err := os.OpenRoot("/dev/shm")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	deadName := fmt.Sprintf(".tanpopo-backup-%d-2147483647-%032x", os.Geteuid(), 1)
	activeName := fmt.Sprintf(".tanpopo-backup-%d-%d-%032x", os.Geteuid(), os.Getpid(), 2)
	for _, name := range []string{deadName, activeName} {
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
		_ = removeTree(root, activeName)
	})
	if err := cleanupAbandonedStagingRoots(root); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Lstat(deadName); !os.IsNotExist(err) {
		t.Fatalf("dead-process staging directory remains, stat err=%v", err)
	}
	if _, err := root.Lstat(activeName); err != nil {
		t.Fatalf("active-process staging directory was removed: %v", err)
	}
}
