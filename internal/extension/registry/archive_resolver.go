// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
)

// ErrOCIArtifactNotIndexed means the requested immutable artifact is not part
// of the currently cached, signature-verified marketplace index.
var ErrOCIArtifactNotIndexed = errors.New("registry: OCI artifact is not present in the verified marketplace index")

type RegistryCredentialResolver interface {
	CredentialsForRegistry(context.Context, string) (*RegistryCredentials, error)
}

// OCIArchiveResolver resolves only artifacts present in the durable verified
// index, then snapshots their digest-verified bytes into a private temp file for
// the existing installer lifecycle.
type OCIArchiveResolver struct {
	Client      *OCIClient
	Index       IndexStore
	Credentials RegistryCredentialResolver
}

// Resolve returns an installer request backed by a private verified snapshot.
// The caller must invoke the cleanup function after the installer consumes it.
func (r *OCIArchiveResolver) Resolve(ctx context.Context, sourceURI, expectedSHA256 string) (*installer.StageInstallRequest, func(), error) {
	if r == nil || r.Client == nil || r.Index == nil {
		return nil, nil, fmt.Errorf("%w: OCI archive resolver is not configured", ErrInvalidOCIReference)
	}
	registryHost, _, digest, err := parseOCIReference(sourceURI)
	if err != nil {
		return nil, nil, err
	}
	want, err := decodeSHA256(expectedSHA256)
	pinned, _ := decodeSHA256(digest)
	if err != nil || subtle.ConstantTimeCompare(want, pinned) != 1 {
		return nil, nil, fmt.Errorf("%w: expected digest does not match pinned OCI reference", installer.ErrDigestMismatch)
	}
	entries, err := r.Index.CachedIndex(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load verified marketplace index: %w", err)
	}
	indexed := false
	for _, entry := range entries {
		if entry.OCIURI == sourceURI && entry.Digest == digest {
			indexed = true
			break
		}
	}
	if !indexed {
		return nil, nil, ErrOCIArtifactNotIndexed
	}
	var credentials *RegistryCredentials
	if r.Credentials != nil {
		credentials, err = r.Credentials.CredentialsForRegistry(ctx, registryHost)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve registry credentials: %w", err)
		}
	}
	body, err := r.Client.Pull(ctx, sourceURI, credentials)
	if err != nil {
		return nil, nil, err
	}
	defer clear(body)
	archive, err := os.CreateTemp("", "tanpopo-oci-*.tpkg")
	if err != nil {
		return nil, nil, fmt.Errorf("create private OCI archive snapshot: %w", err)
	}
	archivePath := archive.Name()
	cleanup := func() { _ = os.Remove(archivePath) }
	if err := archive.Chmod(0o600); err != nil {
		_ = archive.Close()
		cleanup()
		return nil, nil, fmt.Errorf("secure OCI archive snapshot: %w", err)
	}
	if _, err := archive.Write(body); err != nil {
		_ = archive.Close()
		cleanup()
		return nil, nil, fmt.Errorf("write OCI archive snapshot: %w", err)
	}
	if err := archive.Sync(); err != nil {
		_ = archive.Close()
		cleanup()
		return nil, nil, fmt.Errorf("sync OCI archive snapshot: %w", err)
	}
	if err := archive.Close(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("close OCI archive snapshot: %w", err)
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return nil, nil, err
	}
	return &installer.StageInstallRequest{
		ArchivePath: archivePath,
		Source: installer.SourceSpec{Kind: installer.SourceKindOCI, URI: sourceURI, ExpectedSHA256: digest},
	}, cleanup, nil
}
