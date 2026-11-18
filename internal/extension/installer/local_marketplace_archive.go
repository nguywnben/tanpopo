// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package installer

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const LocalMarketplaceArchiveRootEnv = "TANPOPO_MARKETPLACE_ARCHIVE_ROOT"

var localMarketplaceFilenameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.tpkg$`)

var ErrMarketplaceArchiveResolverUnavailable = errors.New("installer: local marketplace archive resolver is not configured")

// LocalMarketplaceArchiveResolver serves only explicitly configured, local .tpkg
// archives addressed as tanpopo-local://<single-filename>. It snapshots bytes into
// a private temporary file so the digest checked here is the exact content handed
// to the installer, even if the configured source file is later replaced.
type LocalMarketplaceArchiveResolver struct {
	root string
}

func NewLocalMarketplaceArchiveResolver(root string) (*LocalMarketplaceArchiveResolver, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, ErrMarketplaceArchiveResolverUnavailable
	}
	return &LocalMarketplaceArchiveResolver{root: root}, nil
}

// Resolve returns a staged install request and a cleanup function for its private
// verified snapshot. The caller must invoke cleanup after the installer consumes it.
func (r *LocalMarketplaceArchiveResolver) Resolve(ctx context.Context, sourceURI, expectedSHA256 string) (*StageInstallRequest, func(), error) {
	if r == nil || strings.TrimSpace(r.root) == "" {
		return nil, nil, ErrMarketplaceArchiveResolverUnavailable
	}
	name, err := localMarketplaceArchiveName(sourceURI)
	if err != nil {
		return nil, nil, err
	}
	want, err := hex.DecodeString(expectedSHA256)
	if err != nil || len(want) != sha256.Size || strings.ToLower(expectedSHA256) != expectedSHA256 {
		return nil, nil, fmt.Errorf("%w: expected SHA-256 must be lowercase 64-hex", ErrDigestMismatch)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	root, err := os.OpenRoot(r.root)
	if err != nil {
		return nil, nil, fmt.Errorf("open configured marketplace archive root: %w", err)
	}
	defer root.Close()

	before, err := root.Lstat(name)
	if err != nil {
		return nil, nil, fmt.Errorf("stat marketplace archive: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%w: marketplace archive must be a regular non-symlink file", ErrInvalidArchive)
	}
	if before.Size() < 0 || before.Size() > MaxArchiveCompressedBytes {
		return nil, nil, fmt.Errorf("%w: marketplace archive exceeds %d bytes", ErrArchiveBomb, MaxArchiveCompressedBytes)
	}

	source, err := root.Open(name)
	if err != nil {
		return nil, nil, fmt.Errorf("open marketplace archive: %w", err)
	}
	defer source.Close()
	opened, err := source.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat opened marketplace archive: %w", err)
	}
	after, err := root.Lstat(name)
	if err != nil || after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(before, after) {
		return nil, nil, fmt.Errorf("%w: marketplace archive changed while opening", ErrInvalidArchive)
	}

	snapshot, err := os.CreateTemp("", "tanpopo-marketplace-*.tpkg")
	if err != nil {
		return nil, nil, fmt.Errorf("create private marketplace archive snapshot: %w", err)
	}
	snapshotPath := snapshot.Name()
	cleanup := func() { _ = os.Remove(snapshotPath) }
	if err := snapshot.Chmod(0o600); err != nil {
		_ = snapshot.Close()
		cleanup()
		return nil, nil, fmt.Errorf("secure marketplace archive snapshot: %w", err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(snapshot, hasher), io.LimitReader(source, MaxArchiveCompressedBytes+1))
	if copyErr == nil && written > MaxArchiveCompressedBytes {
		copyErr = fmt.Errorf("%w: marketplace archive exceeds %d bytes", ErrArchiveBomb, MaxArchiveCompressedBytes)
	}
	if copyErr == nil && written != before.Size() {
		copyErr = fmt.Errorf("%w: marketplace archive changed while reading", ErrInvalidArchive)
	}
	if copyErr == nil && subtle.ConstantTimeCompare(hasher.Sum(nil), want) != 1 {
		copyErr = ErrDigestMismatch
	}
	if copyErr == nil {
		copyErr = snapshot.Sync()
	}
	closeErr := snapshot.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		cleanup()
		return nil, nil, fmt.Errorf("verify marketplace archive: %w", copyErr)
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return nil, nil, err
	}

	return &StageInstallRequest{
		ArchivePath: snapshotPath,
		Source: SourceSpec{
			Kind:           SourceKindLocalArchive,
			URI:            sourceURI,
			ExpectedSHA256: expectedSHA256,
		},
	}, cleanup, nil
}

func localMarketplaceArchiveName(sourceURI string) (string, error) {
	const prefix = "tanpopo-local://"
	if !strings.HasPrefix(sourceURI, prefix) {
		return "", fmt.Errorf("%w: only tanpopo-local:// archive sources are supported", ErrInvalidArchive)
	}
	name := strings.TrimPrefix(sourceURI, prefix)
	if !localMarketplaceFilenameRE.MatchString(name) {
		return "", fmt.Errorf("%w: source must name one .tpkg filename", ErrArchivePathTraversal)
	}
	return name, nil
}
