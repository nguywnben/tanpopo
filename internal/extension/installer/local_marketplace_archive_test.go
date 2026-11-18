// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalMarketplaceArchiveResolverCopiesVerifiedArchive(t *testing.T) {
	root := t.TempDir()
	archive := []byte("verified package archive")
	if err := os.WriteFile(filepath.Join(root, "adapter.tpkg"), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	resolver, err := NewLocalMarketplaceArchiveResolver(root)
	if err != nil {
		t.Fatal(err)
	}

	req, cleanup, err := resolver.Resolve(context.Background(), "tanpopo-local://adapter.tpkg", hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()
	if req == nil || req.Source.Kind != SourceKindLocalArchive || req.Source.URI != "tanpopo-local://adapter.tpkg" || req.Source.ExpectedSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("unexpected staged source: %#v", req)
	}
	got, err := os.ReadFile(req.ArchivePath)
	if err != nil {
		t.Fatalf("read verified snapshot: %v", err)
	}
	if string(got) != string(archive) {
		t.Fatalf("snapshot bytes %q, want %q", got, archive)
	}
	if err := os.Chmod(filepath.Join(root, "adapter.tpkg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "adapter.tpkg"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(req.ArchivePath)
	if err != nil || string(got) != string(archive) {
		t.Fatalf("snapshot changed after source replacement: bytes=%q err=%v", got, err)
	}
}

func TestLocalMarketplaceArchiveResolverRejectsInvalidSourcesAndPins(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "adapter.tpkg"), []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewLocalMarketplaceArchiveResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	good := sha256.Sum256([]byte("archive"))
	wrong := sha256.Sum256([]byte("another"))
	for _, tc := range []struct {
		name, uri, digest string
		want              error
	}{
		{name: "remote source", uri: "https://example.test/adapter.tpkg", digest: hex.EncodeToString(good[:]), want: ErrInvalidArchive},
		{name: "path traversal", uri: "tanpopo-local://../adapter.tpkg", digest: hex.EncodeToString(good[:]), want: ErrArchivePathTraversal},
		{name: "nested path", uri: "tanpopo-local://sub/adapter.tpkg", digest: hex.EncodeToString(good[:]), want: ErrArchivePathTraversal},
		{name: "wrong digest", uri: "tanpopo-local://adapter.tpkg", digest: hex.EncodeToString(wrong[:]), want: ErrDigestMismatch},
		{name: "malformed digest", uri: "tanpopo-local://adapter.tpkg", digest: "bad", want: ErrDigestMismatch},
		{name: "missing archive", uri: "tanpopo-local://missing.tpkg", digest: hex.EncodeToString(good[:]), want: os.ErrNotExist},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if req, cleanup, err := resolver.Resolve(context.Background(), tc.uri, tc.digest); err == nil {
				if cleanup != nil {
					cleanup()
				}
				if req != nil {
					t.Fatalf("unexpected request: %#v", req)
				}
				t.Fatal("Resolve succeeded, want rejection")
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("Resolve error %v, want errors.Is(_, %v)", err, tc.want)
			}
		})
	}
}

func TestLocalMarketplaceArchiveResolverRejectsSymlinksAndNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.tpkg")
	if err := os.WriteFile(target, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.tpkg")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink fixture: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory.tpkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewLocalMarketplaceArchiveResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("archive"))
	for _, name := range []string{"linked.tpkg", "directory.tpkg"} {
		if req, cleanup, err := resolver.Resolve(context.Background(), "tanpopo-local://"+name, hex.EncodeToString(digest[:])); err == nil {
			if cleanup != nil {
				cleanup()
			}
			t.Fatalf("Resolve(%s) returned request %#v, want rejection", name, req)
		} else if !errors.Is(err, ErrInvalidArchive) {
			t.Fatalf("Resolve(%s) error %v, want ErrInvalidArchive", name, err)
		}
	}
}

func TestLocalMarketplaceArchiveResolverRequiresExplicitRoot(t *testing.T) {
	if _, err := NewLocalMarketplaceArchiveResolver(" "); !errors.Is(err, ErrMarketplaceArchiveResolverUnavailable) {
		t.Fatalf("empty root error %v, want unavailable", err)
	}
	var resolver *LocalMarketplaceArchiveResolver
	if req, cleanup, err := resolver.Resolve(context.Background(), "tanpopo-local://adapter.tpkg", ""); !errors.Is(err, ErrMarketplaceArchiveResolverUnavailable) || req != nil || cleanup != nil {
		t.Fatalf("nil resolver result: req=%#v cleanup=%v err=%v", req, cleanup != nil, err)
	}
}
