// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestServeArchiveResolverIsOptInAndUsesConfiguredRoot(t *testing.T) {
	if got := newServeArchiveResolver(""); got != nil {
		t.Fatal("unset archive root must leave the Admin API resolver disabled")
	}
	root := t.TempDir()
	archive := []byte("package")
	if err := os.WriteFile(filepath.Join(root, "adapter.tpkg"), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	resolve := newServeArchiveResolver(root)
	digest := sha256.Sum256(archive)
	req, cleanup, err := resolve(context.Background(), "org_test", "tanpopo-local://adapter.tpkg", hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatalf("configured resolver: %v", err)
	}
	defer cleanup()
	if req.ArchivePath == "" {
		t.Fatal("configured resolver returned no archive path")
	}
}
