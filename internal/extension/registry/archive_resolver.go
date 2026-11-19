// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/security/egress"
)

// ErrOCIArtifactNotIndexed means the requested immutable artifact is not part
// of the currently cached, signature-verified marketplace index.
var ErrOCIArtifactNotIndexed = errors.New("registry: OCI artifact is not present in the verified marketplace index")

type RegistryCredentialResolver interface {
	CredentialsForRegistry(context.Context, string, string) (*RegistryCredentials, error)
}

// OCIArchiveResolver resolves only artifacts present in the durable verified
// index, then snapshots their digest-verified bytes into a private temp file for
// the existing installer lifecycle.
type OCIArchiveResolver struct {
	index             IndexStore
	credentials       RegistryCredentialResolver
	resolver          egress.HostResolver
	clientForRegistry func(context.Context, string) (*OCIClient, error)
}

// NewOCIArchiveResolver creates an OCI resolver whose production transport is
// constructed with the exact registry authority as its SSRF allowlist.
func NewOCIArchiveResolver(index IndexStore, credentials RegistryCredentialResolver, hostResolver egress.HostResolver) *OCIArchiveResolver {
	return &OCIArchiveResolver{index: index, credentials: credentials, resolver: hostResolver}
}

// Resolve returns an installer request backed by a private verified snapshot.
// The caller must invoke the cleanup function after the installer consumes it.
func (r *OCIArchiveResolver) Resolve(ctx context.Context, orgID, sourceURI, expectedSHA256 string) (*installer.StageInstallRequest, func(), error) {
	if r == nil || r.index == nil {
		return nil, nil, fmt.Errorf("%w: OCI archive resolver is not configured", ErrInvalidOCIReference)
	}
	if strings.TrimSpace(orgID) == "" {
		return nil, nil, fmt.Errorf("%w: organization scope is required", ErrInvalidOCIReference)
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
	entries, err := r.index.CachedIndex(ctx)
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
	if r.credentials != nil {
		credentials, err = r.credentials.CredentialsForRegistry(ctx, orgID, registryHost)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve registry credentials: %w", err)
		}
	}
	client, err := r.guardedClientForRegistry(ctx, registryHost)
	if err != nil {
		return nil, nil, fmt.Errorf("configure guarded OCI client: %w", err)
	}
	if client == nil {
		return nil, nil, fmt.Errorf("%w: guarded OCI client is not configured", ErrInvalidOCIReference)
	}
	body, err := client.Pull(ctx, sourceURI, credentials)
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

func (r *OCIArchiveResolver) guardedClientForRegistry(ctx context.Context, authority string) (*OCIClient, error) {
	if r.clientForRegistry != nil {
		return r.clientForRegistry(ctx, authority)
	}
	u, err := url.Parse("https://" + authority)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrInvalidOCIReference
	}
	port := 443
	if rawPort := u.Port(); rawPort != "" {
		port, err = strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return nil, ErrInvalidOCIReference
		}
	}
	client, err := egress.NewGuardedHTTPClient(egress.ClientConfig{
		Policy: egress.Policy{AllowedHosts: []string{u.Hostname()}, AllowedPorts: []int{port}, RequireTLS: true},
		Resolver: r.resolver, Timeout: 30 * time.Second, MaxRedirects: 0, DenyCrossOriginRedirects: true,
	})
	if err != nil {
		return nil, err
	}
	return &OCIClient{HTTPClient: client}, nil
}
