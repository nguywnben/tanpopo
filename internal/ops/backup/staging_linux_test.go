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

func TestVerifyVolatileStagingPreconditionsFailClosed(t *testing.T) {
	headerOnly := []byte("Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n")
	if err := verifyVolatileStagingPreconditions(linuxTmpfsMagic, headerOnly, false); err != nil {
		t.Fatalf("swap-off tmpfs preconditions should pass: %v", err)
	}
	activeSwap := []byte("Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n/swap.img                               file            1048572 0       -2\n")
	if err := verifyVolatileStagingPreconditions(linuxTmpfsMagic, activeSwap, false); err == nil {
		t.Fatal("active swap was unexpectedly accepted for plaintext staging")
	}
	if err := verifyVolatileStagingPreconditions(linuxTmpfsMagic, nil, false); err == nil {
		t.Fatal("empty /proc/swaps was unexpectedly accepted for plaintext staging")
	}
	if err := verifyVolatileStagingPreconditions(0xef53, headerOnly, false); err == nil {
		t.Fatal("non-tmpfs /dev/shm was unexpectedly accepted for plaintext staging")
	}
}

func TestOpenStagingRootFailsClosedWhenHostUnsupported(t *testing.T) {
	if PlatformSupported() {
		return
	}
	outputRoot, err := openOutputRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer outputRoot.Close()
	if _, cleanup, err := openStagingRoot(outputRoot); err == nil {
		if cleanup != nil {
			_ = cleanup()
		}
		t.Fatal("openStagingRoot unexpectedly succeeded on a host without swap-off tmpfs preconditions")
	}
}
