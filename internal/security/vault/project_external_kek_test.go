// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type testExternalProjectRootProvider struct {
	*StaticKEKProvider
	byProject map[string]string
	wrapErr   error
	unwrapErr error
}

func newTestExternalProjectRootProvider(t *testing.T) *testExternalProjectRootProvider {
	t.Helper()
	raw := sha256.Sum256([]byte("test-external-project-master"))
	static, err := NewStaticKEKProvider("kek_ext_master", raw[:])
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	return &testExternalProjectRootProvider{
		StaticKEKProvider: static,
		byProject:         make(map[string]string),
	}
}

func (p *testExternalProjectRootProvider) ProjectKeyURI(_ context.Context, orgID, projectID string) (string, bool, error) {
	ref, ok := p.byProject[orgID+":"+projectID]
	return ref, ok && ref != "", nil
}

func (p *testExternalProjectRootProvider) WrapProjectDEK(_ context.Context, kmsKeyRef, orgID, projectID string, dekVersion int64, plaintextDEK []byte) ([]byte, error) {
	if p.wrapErr != nil {
		return nil, p.wrapErr
	}
	sum := sha256.Sum256([]byte("ext-wrap|" + kmsKeyRef + "|" + orgID + "|" + projectID))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	aad := []byte(orgID + ":" + projectID + ":" + string(rune(dekVersion)))
	ct := aead.Seal(nonce, nonce, plaintextDEK, aad)
	return ct, nil
}

func (p *testExternalProjectRootProvider) UnwrapProjectDEK(_ context.Context, kmsKeyRef, orgID, projectID string, dekVersion int64, wrappedBlob []byte) ([]byte, error) {
	if p.unwrapErr != nil {
		return nil, p.unwrapErr
	}
	sum := sha256.Sum256([]byte("ext-wrap|" + kmsKeyRef + "|" + orgID + "|" + projectID))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := aead.NonceSize()
	if len(wrappedBlob) <= ns {
		return nil, ErrDecryptionFailed
	}
	aad := []byte(orgID + ":" + projectID + ":" + string(rune(dekVersion)))
	pt, err := aead.Open(nil, wrappedBlob[:ns], wrappedBlob[ns:], aad)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return pt, nil
}

func TestExternalProjectDEKEnvelopeCodecAndStoreBinding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Exercise envelope validation & decode failures directly.
	if _, err := encodeExternalProjectDEKEnvelope("", []byte("blob")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty keyRef, got %v", err)
	}
	if _, err := encodeExternalProjectDEKEnvelope("kms://k1", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty blob, got %v", err)
	}
	if _, _, err := decodeExternalProjectDEKEnvelope([]byte("not-prefixed")); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed on missing prefix, got %v", err)
	}
	if _, _, err := decodeExternalProjectDEKEnvelope([]byte(tpkmsEnvelopePrefix + "{invalid-json")); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed on invalid json, got %v", err)
	}
	if _, _, err := decodeExternalProjectDEKEnvelope([]byte(tpkmsEnvelopePrefix + `{"k":"","b":"AQ=="}`)); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed on incomplete envelope, got %v", err)
	}
	nonce, err := generateRandomNonce()
	if err != nil || len(nonce) != NonceSizeBytes {
		t.Fatalf("generateRandomNonce = len %d, err %v", len(nonce), err)
	}

	env := newVaultTestEnv(t, time.Minute)
	if _, err := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: "org_ext_1", DisplayName: "Ext Org 1", Slug: "ext-org-1",
	}); err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if _, err := env.sqlite.Tenancy().CreateProject(ctx, "org_ext_1", port.CreateProjectParams{
		ID: "prj_ext_1", DisplayName: "Ext Proj 1", Slug: "ext-proj-1",
	}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Store without ProjectRootKeyProvider must fail closed on BindProjectExternalRootKey.
	if _, err := env.vault.BindProjectExternalRootKey(ctx, "org_ext_1", "prj_ext_1", "vault-transit://k1"); !errors.Is(err, ErrProjectExternalKeyUnavailable) {
		t.Fatalf("expected ErrProjectExternalKeyUnavailable without provider, got %v", err)
	}
	if _, err := env.vault.BindProjectExternalRootKey(ctx, "", "prj_ext_1", "vault-transit://k1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty org, got %v", err)
	}

	// Open second Store wired with external project root provider sharing the same SQLite DB.
	extProv := newTestExternalProjectRootProvider(t)
	extStore, err := NewStore(Config{
		DB:            env.sqlite.RawDB(),
		KEK:           extProv,
		PostShredHook: env.sqlite.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Missing input validations.
	if _, err := extStore.BindProjectExternalRootKey(ctx, "org_ext_1", "", "vault-transit://k1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for blank project, got %v", err)
	}

	// Seal under default Org DEK first, then bind external root key directly via Store.
	if _, _, err := extStore.SealProjectBlob(ctx, "org_ext_1", "prj_ext_1", RecordKindCacheEntry, "rec_before", []byte("before-bind")); err != nil {
		t.Fatalf("SealProjectBlob before bind: %v", err)
	}

	// Failed external wrap must leave version cleanly unadvanced so retry succeeds.
	extProv.wrapErr = errors.New("kms transient")
	if _, err := extStore.BindProjectExternalRootKey(ctx, "org_ext_1", "prj_ext_1", "vault-transit://p1"); err == nil || !strings.Contains(err.Error(), "kms transient") {
		t.Fatalf("expected kms transient error, got %v", err)
	}
	extProv.wrapErr = nil

	// Auto-binding during EnsureProjectDEK when ProjectKeyURI returns true before manual Bind.
	extProv.byProject["org_ext_1:prj_ext_auto"] = "vault-transit://p_auto"
	if _, err := env.sqlite.Tenancy().CreateProject(ctx, "org_ext_1", port.CreateProjectParams{
		ID: "prj_ext_auto", DisplayName: "Ext Proj Auto", Slug: "ext-proj-auto",
	}); err != nil {
		t.Fatalf("CreateProject auto: %v", err)
	}
	if _, err := extStore.EnsureProjectDEK(ctx, "org_ext_1", "prj_ext_auto"); err != nil {
		t.Fatalf("EnsureProjectDEK auto external bind: %v", err)
	}

	meta, err := extStore.BindProjectExternalRootKey(ctx, "org_ext_1", "prj_ext_1", "vault-transit://p1")
	if err != nil {
		t.Fatalf("BindProjectExternalRootKey: %v", err)
	}
	if meta.DEKVersion < 2 {
		t.Fatalf("expected DEKVersion >= 2 after external bind, got %+v", meta)
	}
	extStore.EvictProjectDEKCache("org_ext_1", "prj_ext_1")

	n2, ct2, err := extStore.SealProjectBlob(ctx, "org_ext_1", "prj_ext_1", RecordKindCacheEntry, "rec_after", []byte("after-bind"))
	if err != nil {
		t.Fatalf("SealProjectBlob after bind: %v", err)
	}
	extStore.EvictProjectDEKCache("org_ext_1", "prj_ext_1")
	pt2, err := extStore.UnsealProjectBlob(ctx, "org_ext_1", "prj_ext_1", RecordKindCacheEntry, "rec_after", n2, ct2)
	if err != nil || string(pt2) != "after-bind" {
		t.Fatalf("UnsealProjectBlob after bind: err=%v pt=%q", err, string(pt2))
	}

	// Reading an externally wrapped project row from a store lacking ProjectRootKeyProvider must fail closed.
	env.vault.EvictProjectDEKCache("org_ext_1", "prj_ext_1")
	if _, err := env.vault.UnsealProjectBlob(ctx, "org_ext_1", "prj_ext_1", RecordKindCacheEntry, "rec_after", n2, ct2); !errors.Is(err, ErrProjectExternalKeyUnavailable) {
		t.Fatalf("expected ErrProjectExternalKeyUnavailable on store without provider, got %v", err)
	}

	// VerifyAllDEKEnvelopes passes on cleanly bound external project DEK before tamper.
	if err := extStore.VerifyAllDEKEnvelopes(ctx); err != nil {
		t.Fatalf("VerifyAllDEKEnvelopes clean: %v", err)
	}

	// Tampered persisted envelope must fail unwrapPersistedProjectDEK and VerifyAllDEKEnvelopes.
	if _, err := env.sqlite.RawDB().ExecContext(ctx, `UPDATE project_encryption_keys SET wrapped_project_dek = ? WHERE org_id = ? AND project_id = ?`, []byte(tpkmsEnvelopePrefix+`{"k":"bad"`), "org_ext_1", "prj_ext_1"); err != nil {
		t.Fatalf("tamper envelope: %v", err)
	}
	extStore.EvictProjectDEKCache("org_ext_1", "prj_ext_1")
	if _, err := extStore.UnsealProjectBlob(ctx, "org_ext_1", "prj_ext_1", RecordKindCacheEntry, "rec_after", n2, ct2); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed on tampered external envelope, got %v", err)
	}
	if err := extStore.VerifyAllDEKEnvelopes(ctx); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed from VerifyAllDEKEnvelopes after tamper, got %v", err)
	}

	// Unwrapping with provider failure returns ErrDecryptionFailed / provider error.
	if _, err := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{ID: "org_ext_2", DisplayName: "O2", Slug: "o2"}); err != nil {
		t.Fatalf("CreateOrg 2: %v", err)
	}
	if _, err := env.sqlite.Tenancy().CreateProject(ctx, "org_ext_2", port.CreateProjectParams{ID: "prj_ext_2", DisplayName: "P2", Slug: "p2"}); err != nil {
		t.Fatalf("CreateProj 2: %v", err)
	}
	if _, err := extStore.BindProjectExternalRootKey(ctx, "org_ext_2", "prj_ext_2", "vault-transit://p2"); err != nil {
		t.Fatalf("BindProjectExternalRootKey p2: %v", err)
	}
	if _, _, err := extStore.SealProjectBlob(ctx, "org_ext_2", "prj_ext_2", RecordKindCacheEntry, "r2", []byte("hi")); err != nil {
		t.Fatalf("SealProjectBlob p2: %v", err)
	}
	extProv.unwrapErr = errors.New("kms unwrap boom")
	extStore.EvictProjectDEKCache("org_ext_2", "prj_ext_2")
	if _, err := extStore.UnsealProjectBlob(ctx, "org_ext_2", "prj_ext_2", RecordKindCacheEntry, "r2", n2, ct2); err == nil {
		t.Fatal("expected error when UnwrapProjectDEK fails")
	}
	extProv.unwrapErr = nil

	// RotateSecret coverage.
	sec, err := extStore.SealSecret(ctx, SealSecretParams{OrgID: "org_ext_2", Name: "rot-s1", SecretKind: SecretKindAPIKey, Plaintext: []byte("sk-1")})
	if err != nil {
		t.Fatalf("SealSecret: %v", err)
	}
	if _, err := extStore.RotateSecret(ctx, sec.OrgID, sec.ID, sec.Version, RotateSecretParams{Plaintext: []byte("sk-2")}); err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}
	if _, err := extStore.RotateSecret(ctx, sec.OrgID, sec.ID, sec.Version, RotateSecretParams{Plaintext: []byte("sk-stale")}); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict on stale RotateSecret, got %v", err)
	}
	if _, _, err := extStore.SealProjectBlob(ctx, "", "p", RecordKindCacheEntry, "id", []byte("x")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SealProjectBlob empty org: %v", err)
	}
}

