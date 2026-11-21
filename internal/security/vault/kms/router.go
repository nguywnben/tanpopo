// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package kms

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nguywnben/tanpopo/internal/security/vault"
)

// RoutingProvider combines a primary instance/org KEKProvider with zero or more
// per-project external ProjectRootKeyProvider instances keyed by URI prefix or
// explicit project binding (DEC-017 / CR-12).
type RoutingProvider struct {
	primary     vault.KEKProvider
	mu          sync.RWMutex
	byProject   map[string]projectKeyBinding
	byScheme    map[string]vault.ProjectRootKeyProvider
}

type projectKeyBinding struct {
	keyRef   string
	provider vault.ProjectRootKeyProvider
}

// NewRoutingProvider constructs a RoutingProvider delegating primary Org KEK operations to primary.
func NewRoutingProvider(primary vault.KEKProvider) (*RoutingProvider, error) {
	if primary == nil {
		return nil, fmt.Errorf("%w: non-nil primary KEKProvider required", vault.ErrInvalidInput)
	}
	rp := &RoutingProvider{
		primary:   primary,
		byProject: make(map[string]projectKeyBinding),
		byScheme:  make(map[string]vault.ProjectRootKeyProvider),
	}
	if prp, ok := primary.(vault.ProjectRootKeyProvider); ok {
		rp.byScheme["default"] = prp
	}
	return rp, nil
}

// RegisterScheme registers an external ProjectRootKeyProvider for a key URI prefix
// (for example, "aws-kms://", "gcp-kms://", or "vault-transit://").
func (r *RoutingProvider) RegisterScheme(prefix string, provider vault.ProjectRootKeyProvider) {
	if strings.TrimSpace(prefix) == "" || provider == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byScheme[strings.TrimSpace(prefix)] = provider
}

// BindProjectKey associates (orgID, projectID) with a dedicated external root key ref and provider.
func (r *RoutingProvider) BindProjectKey(orgID, projectID, kmsKeyRef string, provider vault.ProjectRootKeyProvider) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(kmsKeyRef) == "" || provider == nil {
		return fmt.Errorf("%w: org_id, project_id, kms_key_ref, and provider required", vault.ErrInvalidInput)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byProject[projectMapKey(orgID, projectID)] = projectKeyBinding{
		keyRef:   strings.TrimSpace(kmsKeyRef),
		provider: provider,
	}
	return nil
}

// ActiveKeyID delegates to primary KEKProvider.
func (r *RoutingProvider) ActiveKeyID(ctx context.Context) (string, error) {
	return r.primary.ActiveKeyID(ctx)
}

// WrapDEK delegates to primary KEKProvider.
func (r *RoutingProvider) WrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, plaintextDEK []byte) ([]byte, []byte, error) {
	return r.primary.WrapDEK(ctx, kekKeyID, orgID, dekVersion, plaintextDEK)
}

// UnwrapDEK delegates to primary KEKProvider.
func (r *RoutingProvider) UnwrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, nonce, ciphertext []byte) ([]byte, error) {
	return r.primary.UnwrapDEK(ctx, kekKeyID, orgID, dekVersion, nonce, ciphertext)
}

// WrapArchiveKey delegates archive key wrapping to the primary KEKProvider.
func (r *RoutingProvider) WrapArchiveKey(ctx context.Context, kekKeyID string, plaintextKey []byte) ([]byte, []byte, error) {
	return r.primary.WrapArchiveKey(ctx, kekKeyID, plaintextKey)
}

// UnwrapArchiveKey delegates archive key unwrapping to the primary KEKProvider.
func (r *RoutingProvider) UnwrapArchiveKey(ctx context.Context, kekKeyID string, nonce, ciphertext []byte) ([]byte, error) {
	return r.primary.UnwrapArchiveKey(ctx, kekKeyID, nonce, ciphertext)
}

// ProjectKeyURI returns any dedicated external KMS key registered for (orgID, projectID).
func (r *RoutingProvider) ProjectKeyURI(ctx context.Context, orgID, projectID string) (string, bool, error) {
	r.mu.RLock()
	b, ok := r.byProject[projectMapKey(orgID, projectID)]
	primaryProj, hasPrimaryProj := r.primary.(vault.ProjectRootKeyProvider)
	r.mu.RUnlock()
	if ok && b.keyRef != "" {
		return b.keyRef, true, nil
	}
	if hasPrimaryProj {
		return primaryProj.ProjectKeyURI(ctx, orgID, projectID)
	}
	return "", false, nil
}

// WrapProjectDEK routes project DEK wrapping to the bound provider for kmsKeyRef or (orgID, projectID).
func (r *RoutingProvider) WrapProjectDEK(ctx context.Context, kmsKeyRef, orgID, projectID string, dekVersion int64, plaintextDEK []byte) ([]byte, error) {
	prov, rawRef, err := r.resolveProjectProvider(orgID, projectID, kmsKeyRef)
	if err != nil {
		return nil, err
	}
	return prov.WrapProjectDEK(ctx, rawRef, orgID, projectID, dekVersion, plaintextDEK)
}

// UnwrapProjectDEK routes project DEK unwrapping to the bound provider for kmsKeyRef or (orgID, projectID).
func (r *RoutingProvider) UnwrapProjectDEK(ctx context.Context, kmsKeyRef, orgID, projectID string, dekVersion int64, wrappedBlob []byte) ([]byte, error) {
	prov, rawRef, err := r.resolveProjectProvider(orgID, projectID, kmsKeyRef)
	if err != nil {
		return nil, err
	}
	return prov.UnwrapProjectDEK(ctx, rawRef, orgID, projectID, dekVersion, wrappedBlob)
}

func (r *RoutingProvider) resolveProjectProvider(orgID, projectID, kmsKeyRef string) (vault.ProjectRootKeyProvider, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if b, ok := r.byProject[projectMapKey(orgID, projectID)]; ok && b.provider != nil {
		raw := kmsKeyRef
		for prefix := range r.byScheme {
			if prefix != "default" && strings.HasPrefix(kmsKeyRef, prefix) {
				raw = strings.TrimPrefix(kmsKeyRef, prefix)
				break
			}
		}
		if raw == "" {
			raw = b.keyRef
		}
		return b.provider, raw, nil
	}
	for prefix, p := range r.byScheme {
		if prefix != "default" && strings.HasPrefix(kmsKeyRef, prefix) {
			return p, strings.TrimPrefix(kmsKeyRef, prefix), nil
		}
	}
	if p, ok := r.byScheme["default"]; ok && p != nil {
		return p, kmsKeyRef, nil
	}
	return nil, "", fmt.Errorf("%w: no external KMS provider resolved for key %q", vault.ErrProjectExternalKeyUnavailable, kmsKeyRef)
}
