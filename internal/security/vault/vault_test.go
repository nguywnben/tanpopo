// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vault

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func random32Key(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, KeySizeBytes)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return k
}

func mustRawKeyForTest(t *testing.T, p *StaticKEKProvider, id string) []byte {
	t.Helper()
	cp, err := p.copyKey(id)
	if err != nil {
		t.Fatalf("copyKey(%q): %v", id, err)
	}
	return cp
}

func mustOrgWrapAAD(t *testing.T, kekID, orgID string, ver int64) []byte {
	t.Helper()
	aad, err := orgDEKWrapAAD(kekID, orgID, ver)
	if err != nil {
		t.Fatalf("orgDEKWrapAAD: %v", err)
	}
	return aad
}

type vaultTestEnv struct {
	dir     string
	dbPath  string
	sqlite  *sqlite.Store
	kek     *StaticKEKProvider
	vault   *Store
	clock   *clock.FakeClock
	cap     UnsealCapability
	scanner *canary.Scanner
}

func newVaultTestEnv(t *testing.T, ttl time.Duration) *vaultTestEnv {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vault", "tanpopo.db")
	fc := clock.NewFakeClock(time.Date(2026, 11, 14, 19, 30, 0, 0, time.UTC))

	st, err := sqlite.Open(context.Background(), sqlite.Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	kek, err := NewStaticKEKProvider("kek_v1", random32Key(t))
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	t.Cleanup(kek.Close)

	vs, err := NewStore(Config{
		DB:            st.RawDB(),
		KEK:           kek,
		CacheTTL:      ttl,
		NowMs:         func() int64 { return fc.Now().UnixMilli() },
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	t.Cleanup(vs.Close)

	return &vaultTestEnv{
		dir:     dir,
		dbPath:  dbPath,
		sqlite:  st,
		kek:     kek,
		vault:   vs,
		clock:   fc,
		cap:     MintUnsealCapabilityForBroker(),
		scanner: canary.NewScanner(),
	}
}

func TestVaultSealMetadataCanaryZeroHitsOnDiskAC1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, 0)

	org, err := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "vault-org", DisplayName: "Vault Org"})
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	prj, err := env.sqlite.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "proj-1", DisplayName: "Proj 1"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	secretBytes := []byte(canary.DefaultCanarySecret())
	aud := "https://api.openai.com"

	// 1. Seal Org-scoped secret (owner_project_id == nil) + Project-scoped secret.
	orgMeta, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID:         org.ID,
		Name:          "org-shared-openai",
		SecretKind:    SecretKindAPIKey,
		BoundAudience: &aud,
		Plaintext:     secretBytes,
	})
	if err != nil {
		t.Fatalf("SealSecret org: %v", err)
	}
	projMeta, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID:          org.ID,
		OwnerProjectID: &prj.ID,
		Name:           "proj-openai",
		SecretKind:     SecretKindOAuth2TokenSet,
		BoundAudience:  &aud,
		Plaintext:      secretBytes,
	})
	if err != nil {
		t.Fatalf("SealSecret project: %v", err)
	}
	if len(orgMeta.KeyFingerprint) != 64 || len(projMeta.KeyFingerprint) != 64 {
		t.Fatalf("expected 64-char SHA-256 fingerprints, got %d / %d", len(orgMeta.KeyFingerprint), len(projMeta.KeyFingerprint))
	}
	if orgMeta.KeyFingerprint == projMeta.KeyFingerprint {
		t.Fatalf("expected domain-separated per-record fingerprints even for identical plaintexts")
	}

	// 2. Write-only Control Plane lookups return zero canary secret leakage.
	gotOrgMeta, err := env.vault.GetSecretMetadata(ctx, org.ID, orgMeta.ID)
	if err != nil || gotOrgMeta.ID != orgMeta.ID || gotOrgMeta.OwnerProjectID != nil {
		t.Fatalf("GetSecretMetadata org: got=%+v err=%v", gotOrgMeta, err)
	}
	listAll, err := env.vault.ListSecrets(ctx, org.ID, nil)
	if err != nil || len(listAll) != 2 {
		t.Fatalf("ListSecrets org: len=%d err=%v", len(listAll), err)
	}
	listProj, err := env.vault.ListSecrets(ctx, org.ID, &prj.ID)
	if err != nil || len(listProj) != 1 || listProj[0].ID != projMeta.ID {
		t.Fatalf("ListSecrets project: got=%+v err=%v", listProj, err)
	}
	if findings, err := env.scanner.ScanJSON("metadata", listAll); err != nil || len(findings) != 0 {
		t.Fatalf("metadata JSON leaked canary: findings=%v err=%v", findings, err)
	}

	// 3. UnsealWithCapability recovers exact plaintext and Zeroize clears buffer.
	unsealedOrg, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, orgMeta.ID)
	if err != nil || !bytes.Equal(unsealedOrg.Plaintext, secretBytes) {
		t.Fatalf("UnsealWithCapability org failed: err=%v", err)
	}
	unsealedOrg.Zeroize()
	if unsealedOrg.Plaintext != nil {
		t.Fatalf("expected Zeroize to clear Plaintext")
	}
	unsealedProj, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, projMeta.ID)
	if err != nil || !bytes.Equal(unsealedProj.Plaintext, secretBytes) {
		t.Fatalf("UnsealWithCapability proj failed: err=%v", err)
	}
	ptCopy := unsealedProj.Plaintext
	unsealedProj.Zeroize()
	for i, b := range ptCopy {
		if b != 0 {
			t.Fatalf("expected zeroized plaintext byte[%d]=0, got %d", i, b)
		}
	}

	// 4. Flush WAL and scan raw on-disk SQLite bytes for canary secret -> 0 hits (AC-1).
	if _, err := env.sqlite.RawDB().ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("wal_checkpoint: %v", err)
	}
	_ = env.sqlite.Close()
	env.scanner.AssertCleanDir(t, filepath.Dir(env.dbPath))
}

func TestVaultRewrapAllOrgDEKsAcross10OrgsAC2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, 5*time.Minute)

	type orgRecord struct {
		orgID  string
		projID string
		secID  string
		secret []byte
	}
	orgs := make([]orgRecord, 10)
	for i := 0; i < 10; i++ {
		org, err := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
			Slug:        fmt.Sprintf("rewrap-org-%02d", i),
			DisplayName: fmt.Sprintf("Rewrap Org %02d", i),
		})
		if err != nil {
			t.Fatalf("CreateOrg[%d]: %v", i, err)
		}
		prj, err := env.sqlite.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{
			Slug:        "p1",
			DisplayName: "P1",
		})
		if err != nil {
			t.Fatalf("CreateProj[%d]: %v", i, err)
		}
		secBytes := []byte(fmt.Sprintf("sk-live-secret-for-org-%02d", i))
		meta, err := env.vault.SealSecret(ctx, SealSecretParams{
			OrgID:          org.ID,
			OwnerProjectID: &prj.ID,
			Name:           "api-key",
			SecretKind:     SecretKindAPIKey,
			Plaintext:      secBytes,
		})
		if err != nil {
			t.Fatalf("SealSecret[%d]: %v", i, err)
		}
		orgs[i] = orgRecord{orgID: org.ID, projID: prj.ID, secID: meta.ID, secret: secBytes}
	}

	// Snapshot project_encryption_keys and secret_credentials ciphertexts prior to rotation.
	var projWrappedBefore []byte
	if err := env.sqlite.RawDB().QueryRowContext(ctx,
		`SELECT wrapped_project_dek FROM project_encryption_keys WHERE org_id = ? AND project_id = ?`,
		orgs[0].orgID, orgs[0].projID).Scan(&projWrappedBefore); err != nil {
		t.Fatalf("query pre-rewrap project_encryption_keys: %v", err)
	}
	var secCTBefore []byte
	if err := env.sqlite.RawDB().QueryRowContext(ctx,
		`SELECT ciphertext FROM secret_credentials WHERE org_id = ? AND id = ?`,
		orgs[0].orgID, orgs[0].secID).Scan(&secCTBefore); err != nil {
		t.Fatalf("query pre-rewrap secret_credentials: %v", err)
	}

	// Hold reference to cached Org_0 DEK slice to prove pre-retirement RAM eviction zeroizes it.
	env.vault.cache.mu.RLock()
	org0CachedDEK := env.vault.cache.orgDEKs[orgs[0].orgID].dek
	env.vault.cache.mu.RUnlock()

	// Stage kek_v2 alongside kek_v1 (DR-03 Step 1), promote kek_v2 active, and execute RewrapAllOrgDEKs.
	kekV2 := random32Key(t)
	if err := env.kek.RegisterKey("kek_v2", kekV2, true); err != nil {
		t.Fatalf("RegisterKey kek_v2: %v", err)
	}
	summary, err := env.vault.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2")
	if err != nil {
		t.Fatalf("RewrapAllOrgDEKs: %v", err)
	}
	if summary.Rewrapped != 10 || summary.ConfigEpoch <= 0 {
		t.Fatalf("unexpected rewrap summary: %+v", summary)
	}

	// Verify cached kek_v1 Org DEK slice in RAM was overwritten with 0x00 (clear(buf)).
	for i, b := range org0CachedDEK {
		if b != 0 {
			t.Fatalf("cached Org_0 DEK under retired kek_v1 was not zeroized at byte %d", i)
		}
	}

	// Retire kek_v1 completely from memory and confirm all 10 orgs continue unsealing under kek_v2.
	if err := env.kek.RetireKey("kek_v1"); err != nil {
		t.Fatalf("RetireKey kek_v1: %v", err)
	}
	for i, rec := range orgs {
		unsealed, err := env.vault.UnsealWithCapability(ctx, env.cap, rec.orgID, rec.secID)
		if err != nil {
			t.Fatalf("unseal after KEK rotation[%d]: %v", i, err)
		}
		if !bytes.Equal(unsealed.Plaintext, rec.secret) {
			t.Fatalf("mismatch plaintext after rotation[%d]", i)
		}
		unsealed.Zeroize()
	}

	// Verify project_encryption_keys and secret_credentials ciphertexts were untouched (DR-03).
	var projWrappedAfter, secCTAfter []byte
	_ = env.sqlite.RawDB().QueryRowContext(ctx,
		`SELECT wrapped_project_dek FROM project_encryption_keys WHERE org_id = ? AND project_id = ?`,
		orgs[0].orgID, orgs[0].projID).Scan(&projWrappedAfter)
	_ = env.sqlite.RawDB().QueryRowContext(ctx,
		`SELECT ciphertext FROM secret_credentials WHERE org_id = ? AND id = ?`,
		orgs[0].orgID, orgs[0].secID).Scan(&secCTAfter)
	if !bytes.Equal(projWrappedBefore, projWrappedAfter) || !bytes.Equal(secCTBefore, secCTAfter) {
		t.Fatalf("RewrapAllOrgDEKs must not rewrite project_encryption_keys or secret_credentials")
	}
}

func TestVaultShredProjectDEKAndOrgDEKAC3AndCONDPO02(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, 5*time.Minute)

	orgA, err := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "shred-a", DisplayName: "Shred A"})
	if err != nil {
		t.Fatalf("CreateOrg A: %v", err)
	}
	orgB, err := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "shred-b", DisplayName: "Shred B"})
	if err != nil {
		t.Fatalf("CreateOrg B: %v", err)
	}
	p1, err := env.sqlite.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p1", DisplayName: "P1"})
	if err != nil {
		t.Fatalf("CreateProj P1: %v", err)
	}
	p2, err := env.sqlite.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p2", DisplayName: "P2"})
	if err != nil {
		t.Fatalf("CreateProj P2: %v", err)
	}
	p3, err := env.sqlite.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "p3", DisplayName: "P3"})
	if err != nil {
		t.Fatalf("CreateProj P3: %v", err)
	}

	secP1, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &p1.ID, Name: "p1-key", SecretKind: SecretKindAPIKey,
		Plaintext: []byte(canary.DefaultCanarySecret()),
	})
	if err != nil {
		t.Fatalf("Seal P1: %v", err)
	}
	secP2, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &p2.ID, Name: "p2-key", SecretKind: SecretKindHMACSecret,
		Plaintext: []byte("p2-secret-value"),
	})
	if err != nil {
		t.Fatalf("Seal P2: %v", err)
	}
	secOrgA, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, Name: "org-shared", SecretKind: SecretKindAWSSigV4Key,
		Plaintext: []byte("org-a-shared-value"),
	})
	if err != nil {
		t.Fatalf("Seal OrgA shared: %v", err)
	}
	secP3, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: orgB.ID, OwnerProjectID: &p3.ID, Name: "p3-key", SecretKind: SecretKindAPIKey,
		Plaintext: []byte("org-b-p3-value"),
	})
	if err != nil {
		t.Fatalf("Seal P3: %v", err)
	}

	// Capture hot SQLite backup PRIOR to shredding (COND-PO-02 drill).
	backupPath := filepath.Join(env.dir, "pre-shred.db")
	if err := env.sqlite.BackupTo(ctx, backupPath); err != nil {
		t.Fatalf("BackupTo: %v", err)
	}

	// Extract P1 ciphertext + nonce from pre-shred backup before destroying P1 DEK.
	backupStore, err := sqlite.Open(ctx, sqlite.Config{DBPath: backupPath, SkipMigrate: true})
	if err != nil {
		t.Fatalf("open backupStore: %v", err)
	}
	defer backupStore.Close()
	var oldNonce, oldCT, oldFP []byte
	var oldCreated int64
	if err := backupStore.RawDB().QueryRowContext(ctx,
		`SELECT nonce, ciphertext, key_fingerprint, created_at_ms FROM secret_credentials WHERE org_id = ? AND id = ?`,
		orgA.ID, secP1.ID).Scan(&oldNonce, &oldCT, &oldFP, &oldCreated); err != nil {
		t.Fatalf("read backup ciphertext: %v", err)
	}

	// Verify backup opened WITHOUT the Master KEK cannot decrypt anything (COND-PO-02).
	wrongKEK, err := NewStaticKEKProvider("kek_v1", random32Key(t))
	if err != nil {
		t.Fatalf("wrongKEK: %v", err)
	}
	defer wrongKEK.Close()
	detachedVault, err := NewStore(Config{
		DB:            backupStore.RawDB(),
		KEK:           wrongKEK,
		PostShredHook: backupStore.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("detachedVault: %v", err)
	}
	if _, err := detachedVault.UnsealWithCapability(ctx, env.cap, orgA.ID, secP1.ID); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected backup without real Master KEK to fail decryption, got %v", err)
	}

	// Grab raw wrapped DEK ciphertexts & cached slice refs prior to ShredProjectDEK / ShredOrgDEK (COND-PO-02 WAL/page residue check).
	var p1WrappedBytes, orgAWrappedBytes []byte
	if err := env.sqlite.RawDB().QueryRowContext(ctx,
		`SELECT wrapped_project_dek FROM project_encryption_keys WHERE org_id = ? AND project_id = ?`,
		orgA.ID, p1.ID).Scan(&p1WrappedBytes); err != nil || len(p1WrappedBytes) == 0 {
		t.Fatalf("read p1 wrapped_project_dek: err=%v", err)
	}
	if err := env.sqlite.RawDB().QueryRowContext(ctx,
		`SELECT wrapped_org_dek FROM org_encryption_keys WHERE org_id = ?`,
		orgA.ID).Scan(&orgAWrappedBytes); err != nil || len(orgAWrappedBytes) == 0 {
		t.Fatalf("read orgA wrapped_org_dek: err=%v", err)
	}
	env.vault.cache.mu.RLock()
	p1CachedSlice := env.vault.cache.projDEKs[projectDEKCacheKey{orgID: orgA.ID, projectID: p1.ID}].dek
	env.vault.cache.mu.RUnlock()

	// Execute single-project Crypto-Shredding on P1!
	if err := env.vault.ShredProjectDEK(ctx, orgA.ID, p1.ID); err != nil {
		t.Fatalf("ShredProjectDEK: %v", err)
	}

	// Verify 0 residue of deleted p1 wrapped_project_dek bytes across .db, -wal, and -shm files (PRAGMA secure_delete + wal_checkpoint(TRUNCATE)).
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if rawDisk, rerr := os.ReadFile(env.dbPath + suffix); rerr == nil {
			if bytes.Contains(rawDisk, p1WrappedBytes) {
				t.Fatalf("COND-PO-02 residue leak: deleted p1 wrapped_project_dek bytes persisted in %s", env.dbPath+suffix)
			}
		}
	}

	// 1. RAM DEK zeroization check.
	for i, b := range p1CachedSlice {
		if b != 0 {
			t.Fatalf("expected P1 cached DEK byte[%d]=0 after shred, got %d", i, b)
		}
	}

	// 2. P1 unseal fails closed; P2, OrgA shared, and OrgB/P3 work 100% normally.
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, orgA.ID, secP1.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for shredded P1 secret, got %v", err)
	}
	if u, err := env.vault.UnsealWithCapability(ctx, env.cap, orgA.ID, secP2.ID); err != nil || string(u.Plaintext) != "p2-secret-value" {
		t.Fatalf("sibling P2 failed after P1 shred: u=%+v err=%v", u, err)
	} else {
		u.Zeroize()
	}
	if u, err := env.vault.UnsealWithCapability(ctx, env.cap, orgA.ID, secOrgA.ID); err != nil || string(u.Plaintext) != "org-a-shared-value" {
		t.Fatalf("OrgA shared secret failed after P1 shred: err=%v", err)
	} else {
		u.Zeroize()
	}

	// 3. Re-seeding P1's pre-shred backup ciphertext into live post-shred DB (even after minting a new P1 DEK)
	// MUST permanently fail decryption because old P1 DEK is destroyed (AC-3 / COND-PO-02).
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &p1.ID, Name: "p1-reborn", SecretKind: SecretKindAPIKey, Plaintext: []byte("new"),
	}); err != nil {
		t.Fatalf("re-mint new P1 DEK: %v", err)
	}
	if _, err := env.sqlite.RawDB().ExecContext(ctx, `
		INSERT INTO secret_credentials (id, org_id, owner_project_id, name, secret_kind, aead_alg, nonce, ciphertext, key_fingerprint, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, 'restored-old', 'API_KEY', 'AES-256-GCM', ?, ?, ?, 1, ?, ?)`,
		secP1.ID, orgA.ID, p1.ID, oldNonce, oldCT, string(oldFP), oldCreated, oldCreated,
	); err != nil {
		t.Fatalf("insert pre-shred ciphertext into post-shred DB: %v", err)
	}
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, orgA.ID, secP1.ID); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed unsealing pre-shred ciphertext after ShredProjectDEK, got %v", err)
	}

	// 4. Full-Organization Shred (ShredOrgDEK) destroys OrgA root DEK, P2 DEK, and Org-shared secrets while leaving OrgB intact.
	env.vault.cache.mu.RLock()
	orgACachedSlice := env.vault.cache.orgDEKs[orgA.ID].dek
	env.vault.cache.mu.RUnlock()

	if err := env.vault.ShredOrgDEK(ctx, orgA.ID); err != nil {
		t.Fatalf("ShredOrgDEK: %v", err)
	}
	for i, b := range orgACachedSlice {
		if b != 0 {
			t.Fatalf("expected OrgA cached DEK byte[%d]=0 after ShredOrgDEK, got %d", i, b)
		}
	}
	// Verify 0 residue of deleted orgA wrapped_org_dek bytes across .db, -wal, and -shm files.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if rawDisk, rerr := os.ReadFile(env.dbPath + suffix); rerr == nil {
			if bytes.Contains(rawDisk, orgAWrappedBytes) {
				t.Fatalf("COND-PO-02 residue leak: deleted orgA wrapped_org_dek bytes persisted in %s", env.dbPath+suffix)
			}
		}
	}
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, orgA.ID, secP2.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected P2 absent after ShredOrgDEK, got %v", err)
	}
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, orgA.ID, secOrgA.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected OrgA shared secret absent after ShredOrgDEK, got %v", err)
	}
	if u, err := env.vault.UnsealWithCapability(ctx, env.cap, orgB.ID, secP3.ID); err != nil || string(u.Plaintext) != "org-b-p3-value" {
		t.Fatalf("expected OrgB untouched after OrgA shred, got err=%v", err)
	} else {
		u.Zeroize()
	}

	// Shredding missing Org/Project returns ErrNotFound.
	if err := env.vault.ShredProjectDEK(ctx, orgA.ID, p1.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on double ShredProjectDEK, got %v", err)
	}
	if err := env.vault.ShredOrgDEK(ctx, orgA.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on double ShredOrgDEK, got %v", err)
	}
}

func TestVaultAEADAADCiphertextTransplantNEG1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, 5*time.Minute)

	// Verify exact AAD string parity with SPEC-SEC-03 & data-model.md.
	aad1, err := OrgSecretAAD("org_1", "sec_1", SecretKindAPIKey)
	if err != nil || string(aad1) != "tanpopo:v1:cred:org_1:sec_1:API_KEY" {
		t.Fatalf("unexpected Formula 1 AAD: %q err=%v", aad1, err)
	}
	aad2, err := ProjectRecordAAD("org_1", "prj_1", RecordKindCred, "sec_2")
	if err != nil || string(aad2) != "tanpopo:v1:proj:org_1:prj_1:cred:sec_2" {
		t.Fatalf("unexpected Formula 2 AAD: %q err=%v", aad2, err)
	}

	orgA, _ := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "t-a", DisplayName: "T A"})
	orgB, _ := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "t-b", DisplayName: "T B"})
	p1, _ := env.sqlite.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p1", DisplayName: "P1"})
	p2, _ := env.sqlite.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p2", DisplayName: "P2"})

	secA, _ := env.vault.SealSecret(ctx, SealSecretParams{OrgID: orgA.ID, OwnerProjectID: &p1.ID, Name: "a", SecretKind: SecretKindAPIKey, Plaintext: []byte("secret-a")})
	secB, _ := env.vault.SealSecret(ctx, SealSecretParams{OrgID: orgB.ID, Name: "b", SecretKind: SecretKindAPIKey, Plaintext: []byte("secret-b")})
	secP2, _ := env.vault.SealSecret(ctx, SealSecretParams{OrgID: orgA.ID, OwnerProjectID: &p2.ID, Name: "p2", SecretKind: SecretKindAPIKey, Plaintext: []byte("secret-p2")})
	secSameProj, _ := env.vault.SealSecret(ctx, SealSecretParams{OrgID: orgA.ID, OwnerProjectID: &p1.ID, Name: "a2", SecretKind: SecretKindAPIKey, Plaintext: []byte("secret-a2")})

	// Cross-Org metadata & unseal isolation (0 cross-org leakage).
	if _, err := env.vault.GetSecretMetadata(ctx, orgA.ID, secB.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org GetSecretMetadata expected ErrNotFound, got %v", err)
	}
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, orgA.ID, secB.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org Unseal expected ErrNotFound, got %v", err)
	}

	// Copy nonce+ciphertext from P1/secA into P2/secP2 AND into OrgB/secB AND into P1/secSameProj.
	var nonceA, ctA []byte
	if err := env.sqlite.RawDB().QueryRowContext(ctx, `SELECT nonce, ciphertext FROM secret_credentials WHERE id = ?`, secA.ID).Scan(&nonceA, &ctA); err != nil {
		t.Fatalf("select secA ciphertext: %v", err)
	}

	// 1. Sibling Project P1 -> P2 transplant inside same OrgA -> GCM fail.
	if _, err := env.sqlite.RawDB().ExecContext(ctx, `UPDATE secret_credentials SET nonce = ?, ciphertext = ? WHERE id = ?`, nonceA, ctA, secP2.ID); err != nil {
		t.Fatalf("transplant P1->P2: %v", err)
	}
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, orgA.ID, secP2.ID); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("P1->P2 ciphertext transplant expected ErrDecryptionFailed, got %v", err)
	}

	// 2. Same Project (P1) different credential_id transplant -> AAD mismatch -> GCM fail.
	if _, err := env.sqlite.RawDB().ExecContext(ctx, `UPDATE secret_credentials SET nonce = ?, ciphertext = ? WHERE id = ?`, nonceA, ctA, secSameProj.ID); err != nil {
		t.Fatalf("transplant same project different id: %v", err)
	}
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, orgA.ID, secSameProj.ID); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("same-project credential_id transplant expected ErrDecryptionFailed, got %v", err)
	}

	// 3. Cross-Org transplant OrgA -> OrgB -> GCM fail.
	if _, err := env.sqlite.RawDB().ExecContext(ctx, `UPDATE secret_credentials SET nonce = ?, ciphertext = ? WHERE id = ?`, nonceA, ctA, secB.ID); err != nil {
		t.Fatalf("transplant OrgA->OrgB: %v", err)
	}
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, orgB.ID, secB.ID); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("cross-org ciphertext transplant expected ErrDecryptionFailed, got %v", err)
	}
}

func TestVaultCapabilityAndMasterKeyFailClosedNEG2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, 5*time.Minute)

	// 1. Forged zero-value UnsealCapability{} fails closed immediately.
	if _, err := env.vault.UnsealWithCapability(ctx, UnsealCapability{}, "org_any", "sec_any"); !errors.Is(err, ErrMissingUnsealCapability) {
		t.Fatalf("expected ErrMissingUnsealCapability for zero-value capability, got %v", err)
	}

	// 2. Reject invalid Master KEKs (< 32 bytes, all-zero, bad env/config).
	for name, val := range map[string]string{
		"empty":         "",
		"too_short_hex": hex.EncodeToString(make([]byte, 16)),
		"all_zero_hex":  hex.EncodeToString(make([]byte, 32)),
		"all_zero_b64":  base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"too_short_b64": base64.StdEncoding.EncodeToString([]byte("short-key")),
	} {
		if _, err := ParseMasterKeyString(val); !errors.Is(err, ErrInvalidMasterKey) {
			t.Fatalf("case %s expected ErrInvalidMasterKey, got %v", name, err)
		}
	}
	if _, err := NewStaticKEKProvider("kek_bad", make([]byte, 32)); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected all-zero raw key rejection, got %v", err)
	}
	if _, err := NewStaticKEKProvider("", random32Key(t)); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected blank kek ID rejection, got %v", err)
	}
	if _, err := LoadStaticKEKFromEnv("kek_v1", func(string) (string, bool) { return "", false }); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected unconfigured env failure, got %v", err)
	}

	// 3. Load from valid hex and base64 env + keyfile (0400/0600 enforced on POSIX, symlink rejected).
	raw32 := random32Key(t)
	hexKey := hex.EncodeToString(raw32)
	b64Key := base64.RawStdEncoding.EncodeToString(raw32)
	if p, err := LoadStaticKEKFromEnv("kek_env", func(k string) (string, bool) {
		if k == EnvMasterKey {
			return b64Key, true
		}
		return "", false
	}); err != nil {
		t.Fatalf("LoadStaticKEKFromEnv valid b64: %v", err)
	} else {
		p.Close()
	}

	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "master.key")
	if err := os.WriteFile(keyPath, []byte(hexKey+"\n"), 0o600); err != nil {
		t.Fatalf("write keyfile: %v", err)
	}
	if p, err := LoadStaticKEKFromEnv("kek_file", func(k string) (string, bool) {
		if k == EnvMasterKeyFile {
			return keyPath, true
		}
		return "", false
	}); err != nil {
		t.Fatalf("LoadStaticKEKFromFile valid 0600 hex: %v", err)
	} else {
		p.Close()
	}

	rawBinPath := filepath.Join(keyDir, "raw.key")
	if err := os.WriteFile(rawBinPath, raw32, 0o400); err != nil {
		t.Fatalf("write raw binary keyfile: %v", err)
	}
	if p, err := LoadStaticKEKFromFile("kek_raw", rawBinPath); err != nil {
		t.Fatalf("LoadStaticKEKFromFile raw 32 bytes: %v", err)
	} else {
		p.Close()
	}

	if runtime.GOOS != "windows" {
		badPermPath := filepath.Join(keyDir, "world.key")
		if err := os.WriteFile(badPermPath, []byte(hexKey), 0o644); err != nil {
			t.Fatalf("write bad perm keyfile: %v", err)
		}
		if _, err := LoadStaticKEKFromFile("kek_badperm", badPermPath); !errors.Is(err, ErrInsecureKeyfilePermissions) {
			t.Fatalf("expected ErrInsecureKeyfilePermissions on 0644 file, got %v", err)
		}
		symKeyPath := filepath.Join(keyDir, "sym.key")
		if os.Symlink(keyPath, symKeyPath) == nil {
			if _, err := LoadStaticKEKFromFile("kek_sym", symKeyPath); !errors.Is(err, ErrInsecureKeyfilePermissions) {
				t.Fatalf("expected symlink keyfile rejection, got %v", err)
			}
		}
	}
}

func TestVaultRotateSecretTTLEvictionConcurrencyAndErrorHygieneNEG3(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, 2*time.Minute)

	// Reject TTL > 5m and nil DB/KEK.
	if _, err := NewStore(Config{DB: env.sqlite.RawDB(), KEK: env.kek, PostShredHook: env.sqlite.CheckpointWAL, CacheTTL: 6 * time.Minute}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for TTL > 5m, got %v", err)
	}
	if _, err := NewStore(Config{DB: env.sqlite.RawDB(), KEK: env.kek}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected fail-closed ErrInvalidInput for nil PostShredHook, got %v", err)
	}
	if _, err := NewStore(Config{KEK: env.kek, PostShredHook: env.sqlite.CheckpointWAL}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nil DB, got %v", err)
	}
	if _, err := NewStore(Config{DB: env.sqlite.RawDB(), PostShredHook: env.sqlite.CheckpointWAL}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nil KEK, got %v", err)
	}

	org, _ := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "rot-org", DisplayName: "Rot Org"})
	prj, _ := env.sqlite.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "rot-p", DisplayName: "Rot P"})

	v1Secret := []byte(canary.DefaultCanarySecret())
	v2Secret := []byte("sk-tp-" + "canary-DO-NOT-LEAK-v2rotated")
	env.scanner.AddSecret(string(v2Secret))

	meta, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID:          org.ID,
		OwnerProjectID: &prj.ID,
		Name:           "rotatable",
		SecretKind:     SecretKindAPIKey,
		Plaintext:      v1Secret,
	})
	if err != nil {
		t.Fatalf("SealSecret: %v", err)
	}

	var nonce1 []byte
	_ = env.sqlite.RawDB().QueryRowContext(ctx, `SELECT nonce FROM secret_credentials WHERE id = ?`, meta.ID).Scan(&nonce1)

	newAud := "https://api.anthropic.com"
	rotated, err := env.vault.RotateSecret(ctx, org.ID, meta.ID, 1, RotateSecretParams{
		Plaintext:     v2Secret,
		BoundAudience: &newAud,
	})
	if err != nil || rotated.Version != 2 || rotated.BoundAudience == nil || *rotated.BoundAudience != newAud {
		t.Fatalf("RotateSecret v1->v2: got=%+v err=%v", rotated, err)
	}
	if rotated.KeyFingerprint == meta.KeyFingerprint {
		t.Fatalf("expected fingerprint update on RotateSecret")
	}

	var nonce2 []byte
	_ = env.sqlite.RawDB().QueryRowContext(ctx, `SELECT nonce FROM secret_credentials WHERE id = ?`, meta.ID).Scan(&nonce2)
	if bytes.Equal(nonce1, nonce2) {
		t.Fatalf("NEG-3 violation: RotateSecret reused nonce")
	}

	// Optimistic lock conflict on stale expectedVersion=1 + ClearBoundAudience on v2->v3.
	if _, err := env.vault.RotateSecret(ctx, org.ID, meta.ID, 1, RotateSecretParams{Plaintext: []byte("x")}); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict on stale RotateSecret, got %v", err)
	}
	v3Meta, err := env.vault.RotateSecret(ctx, org.ID, meta.ID, 2, RotateSecretParams{Plaintext: v2Secret, ClearBoundAudience: true})
	if err != nil || v3Meta.BoundAudience != nil || v3Meta.Version != 3 {
		t.Fatalf("expected cleared bound_audience at v3: %+v err=%v", v3Meta, err)
	}

	// TTL eviction check: keep ref to cached Project DEK slice, advance FakeClock by 3m (> 2m TTL), and unseal.
	env.vault.cache.mu.RLock()
	cachedProjSlice := env.vault.cache.projDEKs[projectDEKCacheKey{orgID: org.ID, projectID: prj.ID}].dek
	env.vault.cache.mu.RUnlock()

	env.clock.Advance(3 * time.Minute)
	unsealed, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, meta.ID)
	if err != nil || !bytes.Equal(unsealed.Plaintext, v2Secret) {
		t.Fatalf("unseal after TTL refresh failed: err=%v", err)
	}
	unsealed.Zeroize()
	for i, b := range cachedProjSlice {
		if b != 0 {
			t.Fatalf("expired Project DEK slice byte[%d] was not zeroized on TTL eviction", i)
		}
	}

	// Concurrent SealSecret & UnsealWithCapability across goroutines (-race safe).
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			m, err := env.vault.SealSecret(ctx, SealSecretParams{
				ID:             idgen.MustNew(idgen.PrefixSecret),
				OrgID:          org.ID,
				OwnerProjectID: &prj.ID,
				Name:           fmt.Sprintf("conc-%d", idx),
				SecretKind:     SecretKindWebhookHMAC,
				Plaintext:      []byte(fmt.Sprintf("conc-secret-%d", idx)),
			})
			if err != nil {
				t.Errorf("concurrent SealSecret[%d]: %v", idx, err)
				return
			}
			u, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, m.ID)
			if err != nil {
				t.Errorf("concurrent Unseal[%d]: %v", idx, err)
				return
			}
			u.Zeroize()
		}(g)
	}
	wg.Wait()

	// Decryption error message hygiene (NEG-3): zero secret/canary fragments in error strings.
	_, _ = env.sqlite.RawDB().ExecContext(ctx, `UPDATE secret_credentials SET ciphertext = X'deadbeef' WHERE id = ?`, meta.ID)
	_, decErr := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, meta.ID)
	if !errors.Is(decErr, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed, got %v", decErr)
	}
	env.scanner.AssertClean(t, canary.NamedData{Source: "decrypt_error", Data: []byte(decErr.Error())})

	// Explicit EvictAllDEKs & RetireKey error handling & input validation checks.
	env.vault.EvictAllDEKs()
	if err := env.kek.RetireKey("kek_v1"); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected active key retire rejection, got %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on empty SealSecretParams, got %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{OrgID: org.ID, Name: "n", SecretKind: "INVALID", Plaintext: []byte("x")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on invalid secret_kind, got %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{OrgID: org.ID, Name: "n", SecretKind: SecretKindAPIKey}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on empty plaintext, got %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{ID: "org_wrong", OrgID: org.ID, Name: "n", SecretKind: SecretKindAPIKey, Plaintext: []byte("x")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on wrong ID prefix, got %v", err)
	}
	if _, err := ProjectRecordAAD("org_1", "prj_1", "bad_kind", "id_1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on invalid ProjectRecordAAD kind, got %v", err)
	}
}

func TestVaultValidationCacheMissRollbackAndKEKEdgeBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, 5*time.Minute)

	// 1. AAD validation & RecordKind coverage (debug_blob, cache_entry).
	if aad, err := ProjectRecordAAD("org_1", "prj_1", RecordKindDebugBlob, "req_1"); err != nil || string(aad) != "tanpopo:v1:proj:org_1:prj_1:debug_blob:req_1" {
		t.Fatalf("debug_blob AAD: %q err=%v", aad, err)
	}
	if aad, err := ProjectRecordAAD("org_1", "prj_1", RecordKindCacheEntry, "sha_1"); err != nil || string(aad) != "tanpopo:v1:proj:org_1:prj_1:cache_entry:sha_1" {
		t.Fatalf("cache_entry AAD: %q err=%v", aad, err)
	}
	for _, fn := range []func() error{
		func() error { _, e := OrgSecretAAD("", "sec_1", SecretKindAPIKey); return e },
		func() error { _, e := OrgSecretAAD("org_1", "bad:id", SecretKindAPIKey); return e },
		func() error { _, e := OrgSecretAAD("org_1", "sec_1", ""); return e },
		func() error { _, e := ProjectRecordAAD("", "prj_1", RecordKindCred, "s"); return e },
		func() error { _, e := ProjectRecordAAD("org_1", "", RecordKindCred, "s"); return e },
		func() error { _, e := ProjectRecordAAD("org_1", "prj_1", RecordKindCred, ""); return e },
		func() error { _, e := projectDEKWrapAAD("", "p", 1, 1); return e },
		func() error { _, e := projectDEKWrapAAD("o", "", 1, 1); return e },
		func() error { _, e := projectDEKWrapAAD("o", "p", 0, 1); return e },
		func() error { _, e := orgDEKWrapAAD("", "o", 1); return e },
		func() error { _, e := orgDEKWrapAAD("k", "", 1); return e },
		func() error { _, e := orgDEKWrapAAD("k", "o", 0); return e },
	} {
		if err := fn(); err == nil {
			t.Fatalf("expected error on invalid AAD input")
		}
	}

	// 2. KEK provider edge cases (RegisterKey overwrite, Wrap/Unwrap invalid inputs & canceled ctx, Close).
	tmpKEK, err := NewStaticKEKProvider("k1", random32Key(t))
	if err != nil {
		t.Fatalf("tmpKEK: %v", err)
	}
	if err := tmpKEK.RegisterKey("k1", random32Key(t), false); err != nil {
		t.Fatalf("RegisterKey overwrite: %v", err)
	}
	if err := tmpKEK.RegisterKey("", random32Key(t), false); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey blank keyID: %v", err)
	}
	if err := tmpKEK.RegisterKey("k2", []byte("short"), false); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey short key: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := tmpKEK.WrapDEK(cctx, "k1", "org_1", 1, random32Key(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled WrapDEK: %v", err)
	}
	if _, err := tmpKEK.UnwrapDEK(cctx, "k1", "org_1", 1, make([]byte, 12), []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled UnwrapDEK: %v", err)
	}
	if _, _, err := tmpKEK.WrapDEK(ctx, "k1", "org_1", 1, []byte("short")); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey invalid plaintext DEK: %v", err)
	}
	if _, _, err := tmpKEK.WrapDEK(ctx, "missing", "org_1", 1, random32Key(t)); !errors.Is(err, ErrKEKNotFound) {
		t.Fatalf("expected ErrKEKNotFound WrapDEK: %v", err)
	}
	if _, err := tmpKEK.UnwrapDEK(ctx, "missing", "org_1", 1, make([]byte, 12), []byte("ct")); !errors.Is(err, ErrKEKNotFound) {
		t.Fatalf("expected ErrKEKNotFound UnwrapDEK: %v", err)
	}
	if _, err := tmpKEK.UnwrapDEK(ctx, "k1", "org_1", 1, []byte("bad"), []byte("ct")); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed bad nonce len: %v", err)
	}
	if _, err := LoadStaticKEKFromFile("k1", ""); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected empty file path rejection: %v", err)
	}
	if _, err := LoadStaticKEKFromFile("k1", filepath.Join(t.TempDir(), "missing.key")); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected missing keyfile rejection: %v", err)
	}
	badTextFile := filepath.Join(t.TempDir(), "bad.key")
	_ = os.WriteFile(badTextFile, []byte("not-hex-or-b64"), 0o600)
	if _, err := LoadStaticKEKFromFile("k1", badTextFile); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected malformed keyfile rejection: %v", err)
	}
	if _, err := LoadStaticKEKFromFile("kek_dir", t.TempDir()); !errors.Is(err, ErrInsecureKeyfilePermissions) {
		t.Fatalf("expected directory path ErrInsecureKeyfilePermissions: %v", err)
	}
	shortWrapNonce, shortWrapCT, err := sealAESGCM(mustRawKeyForTest(t, tmpKEK, "k1"), mustOrgWrapAAD(t, "k1", "org_1", 1), []byte("16-bytes-only!!"))
	if err != nil {
		t.Fatalf("seal short wrapped DEK: %v", err)
	}
	if _, err := tmpKEK.UnwrapDEK(ctx, "k1", "org_1", 1, shortWrapNonce, shortWrapCT); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey on wrong-length unwrapped Org DEK: %v", err)
	}
	tmpKEK.Close()
	if _, err := tmpKEK.ActiveKeyID(ctx); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey after Close(): %v", err)
	}

	// 3. Cache miss DB-unwrap path inside ensureOrgDEKTx/ensureProjectDEKTx + RotateSecret on Org-shared secret + Org DEK TTL expiry.
	org, _ := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "miss-org", DisplayName: "Miss Org"})
	prj, _ := env.sqlite.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "miss-p", DisplayName: "Miss P"})

	orgSec, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: org.ID, Name: "org-s", SecretKind: SecretKindAPIKey, Plaintext: []byte("org-val-1"),
	})
	if err != nil {
		t.Fatalf("SealSecret org: %v", err)
	}
	prjSec, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: org.ID, OwnerProjectID: &prj.ID, Name: "prj-s", SecretKind: SecretKindOCIRegistryAuth, Plaintext: []byte("prj-val-1"),
	})
	if err != nil {
		t.Fatalf("SealSecret proj: %v", err)
	}

	// Evict all cached DEKs in RAM -> next SealSecret & RotateSecret & Unseal MUST unwrap from DB cleanly!
	env.vault.EvictAllDEKs()
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: org.ID, OwnerProjectID: &prj.ID, Name: "prj-s2", SecretKind: SecretKindMTLSClientCert, Plaintext: []byte("prj-val-2"),
	}); err != nil {
		t.Fatalf("SealSecret after cache eviction (DB unwrap path): %v", err)
	}
	env.vault.EvictAllDEKs()
	if _, err := env.vault.RotateSecret(ctx, org.ID, orgSec.ID, 1, RotateSecretParams{Plaintext: []byte("org-val-2")}); err != nil {
		t.Fatalf("RotateSecret org-shared after eviction: %v", err)
	}
	env.vault.EvictAllDEKs()
	if u, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, prjSec.ID); err != nil || string(u.Plaintext) != "prj-val-1" {
		t.Fatalf("Unseal after full cache clear failed: err=%v", err)
	} else {
		u.Zeroize()
	}

	// Warm Org DEK cache, advance clock past 5m TTL, verify expired Org DEK eviction works cleanly on read.
	env.clock.Advance(6 * time.Minute)
	if u, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, orgSec.ID); err != nil || string(u.Plaintext) != "org-val-2" {
		t.Fatalf("Unseal org after TTL expiration failed: err=%v", err)
	} else {
		u.Zeroize()
	}

	// 4. Error paths: blank owner_project_id, duplicate ID, unknown org/proj FK, RewrapAllOrgDEKs bad args & rollback, Shred bad args, invalid input checks.
	blank := "   "
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{OrgID: org.ID, OwnerProjectID: &blank, Name: "n", SecretKind: SecretKindAPIKey, Plaintext: []byte("x")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected blank owner_project_id ErrInvalidInput: %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{ID: orgSec.ID, OrgID: org.ID, Name: "dup", SecretKind: SecretKindAPIKey, Plaintext: []byte("x")}); !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists on duplicate secret ID: %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{OrgID: "org_missing", Name: "f", SecretKind: SecretKindAPIKey, Plaintext: []byte("x")}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on nonexistent org FK: %v", err)
	}
	if _, err := env.vault.RotateSecret(ctx, "", orgSec.ID, 1, RotateSecretParams{Plaintext: []byte("x")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("RotateSecret blank org: %v", err)
	}
	if _, err := env.vault.RotateSecret(ctx, org.ID, orgSec.ID, 1, RotateSecretParams{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("RotateSecret empty plaintext: %v", err)
	}
	if _, err := env.vault.RotateSecret(ctx, org.ID, "sec_missing", 1, RotateSecretParams{Plaintext: []byte("x")}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("RotateSecret missing secret: %v", err)
	}
	if _, err := env.vault.GetSecretMetadata(ctx, "", orgSec.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("GetSecretMetadata blank: %v", err)
	}
	if _, err := env.vault.ListSecrets(ctx, "", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListSecrets blank: %v", err)
	}
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, "", orgSec.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UnsealWithCapability blank org: %v", err)
	}
	if err := env.vault.ShredProjectDEK(ctx, "", prj.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ShredProjectDEK blank: %v", err)
	}
	if err := env.vault.ShredOrgDEK(ctx, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ShredOrgDEK blank: %v", err)
	}
	if _, err := env.vault.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("RewrapAllOrgDEKs same keyID: %v", err)
	}
	if _, err := env.vault.RewrapAllOrgDEKs(ctx, "", "kek_v2"); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("RewrapAllOrgDEKs blank oldKEKID: %v", err)
	}
	if _, err := env.vault.RewrapAllOrgDEKs(ctx, "kek_v1", ""); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("RewrapAllOrgDEKs blank newKEKID: %v", err)
	}
	// Rewrap fails atomically when target KEK is not registered (kek_missing) and leaves kek_v1 active.
	if _, err := env.vault.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_unregistered"); !errors.Is(err, ErrKEKNotFound) {
		t.Fatalf("expected ErrKEKNotFound rollback when newKEKID missing, got %v", err)
	}
	if sum0, err := env.vault.RewrapAllOrgDEKs(ctx, "kek_unused", "kek_v1"); err != nil || sum0.Rewrapped != 0 {
		t.Fatalf("expected 0-row RewrapAllOrgDEKs noop, got %+v err=%v", sum0, err)
	}
	if mapVaultSQLiteError(nil, false) != nil {
		t.Fatalf("expected nil mapVaultSQLiteError")
	}
	if err := mapVaultSQLiteError(errors.New("FOREIGN KEY constraint failed"), true); !errors.Is(err, port.ErrResourceInUse) {
		t.Fatalf("expected ErrResourceInUse for delete FK, got %v", err)
	}
	if err := mapVaultSQLiteError(errors.New("CHECK constraint failed"), false); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for CHECK constraint, got %v", err)
	}

	// 5. Cache version/KEK mismatch overwrite + corrupted wrapped DEK + default NowMs + closed DB errors.
	_, err = env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: org.ID, OwnerProjectID: &prj.ID, Name: "warm", SecretKind: SecretKindAPIKey, Plaintext: []byte("warm-val"),
	})
	if err != nil {
		t.Fatalf("warm SealSecret: %v", err)
	}
	// Inject stale cache entry with old kek_key_id / old dekVersion so ensure* & load* detect mismatch, clear old, and overwrite cache.
	staleOrgSlice := random32Key(t)
	env.vault.cache.putOrgDEKIfGen(org.ID, "kek_stale", 1, staleOrgSlice, env.clock.Now().UnixMilli(), env.vault.cache.generation())
	staleProjSlice := random32Key(t)
	env.vault.cache.putProjectDEKIfGen(org.ID, prj.ID, 99, 1, staleProjSlice, env.clock.Now().UnixMilli(), env.vault.cache.generation())
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: org.ID, OwnerProjectID: &prj.ID, Name: "refresh-after-stale", SecretKind: SecretKindAPIKey, Plaintext: []byte("ok"),
	}); err != nil {
		t.Fatalf("SealSecret stale cache overwrite: %v", err)
	}
	env.vault.cache.putOrgDEKIfGen(org.ID, "kek_stale", 1, random32Key(t), env.clock.Now().UnixMilli(), env.vault.cache.generation())
	env.vault.cache.putProjectDEKIfGen(org.ID, prj.ID, 99, 1, random32Key(t), env.clock.Now().UnixMilli(), env.vault.cache.generation())
	if u, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, prjSec.ID); err != nil {
		t.Fatalf("UnsealWithCapability stale cache overwrite: %v", err)
	} else {
		u.Zeroize()
	}

	// Corrupted wrapped_project_dek triggers ErrDecryptionFailed inside loadProjectDEKReadOnly & ensureProjectDEKTx.
	var savedProjWrapped []byte
	_ = env.sqlite.RawDB().QueryRowContext(ctx, `SELECT wrapped_project_dek FROM project_encryption_keys WHERE org_id = ? AND project_id = ?`, org.ID, prj.ID).Scan(&savedProjWrapped)
	_, _ = env.sqlite.RawDB().ExecContext(ctx, `UPDATE project_encryption_keys SET wrapped_project_dek = X'deadbeef' WHERE org_id = ? AND project_id = ?`, org.ID, prj.ID)
	env.vault.EvictAllDEKs()
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, prjSec.ID); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed on corrupted wrapped_project_dek read, got %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: org.ID, OwnerProjectID: &prj.ID, Name: "corrupt-pdek", SecretKind: SecretKindAPIKey, Plaintext: []byte("x"),
	}); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed on corrupted wrapped_project_dek seal, got %v", err)
	}
	_, _ = env.sqlite.RawDB().ExecContext(ctx, `UPDATE project_encryption_keys SET wrapped_project_dek = ? WHERE org_id = ? AND project_id = ?`, savedProjWrapped, org.ID, prj.ID)

	// Corrupted wrapped_org_dek triggers ErrDecryptionFailed inside ensureOrgDEKTx, loadOrgDEKReadOnly, and rewrapSingleOrgTx.
	_, _ = env.sqlite.RawDB().ExecContext(ctx, `UPDATE org_encryption_keys SET wrapped_org_dek = X'deadbeef' WHERE org_id = ?`, org.ID)
	env.vault.EvictAllDEKs()
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, orgSec.ID); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed on corrupted wrapped_org_dek read, got %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{OrgID: org.ID, Name: "corrupt-odek", SecretKind: SecretKindAPIKey, Plaintext: []byte("x")}); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed on corrupted wrapped_org_dek seal, got %v", err)
	}
	if err := env.kek.RegisterKey("kek_v2", random32Key(t), false); err != nil {
		t.Fatalf("RegisterKey kek_v2: %v", err)
	}
	if _, err := env.vault.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2"); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed in RewrapAllOrgDEKs on corrupted wrapped_org_dek, got %v", err)
	}

	// LoadStaticKEKFromEnv bad env value + NUL path + openAESGCM/sealAESGCM invalid args + POSIX perm helper + closed DB failures.
	if err := checkPOSIXKeyfileMode("linux", 0o600); err != nil {
		t.Fatalf("expected 0600 on linux to pass: %v", err)
	}
	if err := checkPOSIXKeyfileMode("linux", 0o644); !errors.Is(err, ErrInsecureKeyfilePermissions) {
		t.Fatalf("expected 0644 on linux to fail: %v", err)
	}
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, prjSec.ID); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed unsealing project secret when Org DEK corrupted: %v", err)
	}
	if _, err := LoadStaticKEKFromEnv("kek_bad", func(k string) (string, bool) {
		if k == EnvMasterKey {
			return "bad-hex", true
		}
		return "", false
	}); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey on bad env key: %v", err)
	}
	if _, err := LoadStaticKEKFromEnv("kek_nul", func(k string) (string, bool) {
		if k == EnvMasterKeyFile {
			return "bad\x00path", true
		}
		return "", false
	}); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey on NUL file path: %v", err)
	}
	if _, _, err := sealAESGCM([]byte("short"), nil, []byte("x")); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("sealAESGCM short key: %v", err)
	}
	if _, _, err := sealAESGCM(make([]byte, 32), nil, []byte("x")); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("sealAESGCM all-zero key: %v", err)
	}
	if _, err := openAESGCM([]byte("short"), nil, make([]byte, 12), []byte("x")); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("openAESGCM short key: %v", err)
	}
	if _, err := openAESGCM(random32Key(t), nil, make([]byte, 12), nil); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("openAESGCM empty ciphertext: %v", err)
	}
	// Inject 16-byte valid-GCM wrapped_project_dek on org3 to verify validate256BitKey in loadProjectDEKReadOnly.
	org3, _ := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "short-pdek-org", DisplayName: "S3"})
	prj3, _ := env.sqlite.Tenancy().CreateProject(ctx, org3.ID, port.CreateProjectParams{Slug: "p3", DisplayName: "P3"})
	sec3, err := env.vault.SealSecret(ctx, SealSecretParams{OrgID: org3.ID, OwnerProjectID: &prj3.ID, Name: "s3", SecretKind: SecretKindAPIKey, Plaintext: []byte("ok")})
	if err != nil {
		t.Fatalf("SealSecret org3: %v", err)
	}
	_, realOrg3DEK, err := env.vault.loadOrgDEKReadOnly(ctx, org3.ID)
	if err != nil {
		t.Fatalf("loadOrgDEKReadOnly org3: %v", err)
	}
	p3AAD, _ := projectDEKWrapAAD(org3.ID, prj3.ID, 1, 1)
	badPNonce, badPCT, _ := sealAESGCM(realOrg3DEK, p3AAD, []byte("too-short-pdek"))
	clear(realOrg3DEK)
	_, _ = env.sqlite.RawDB().ExecContext(ctx, `UPDATE project_encryption_keys SET nonce = ?, wrapped_project_dek = ? WHERE org_id = ? AND project_id = ?`, badPNonce, badPCT, org3.ID, prj3.ID)
	env.vault.EvictAllDEKs()
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, org3.ID, sec3.ID); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey on wrong-length project DEK: %v", err)
	}
	if _, err := env.vault.RotateSecret(ctx, org3.ID, sec3.ID, 1, RotateSecretParams{Plaintext: []byte("new")}); !errors.Is(err, ErrDecryptionFailed) && !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected DEK error rotating with invalid project DEK: %v", err)
	}

	defaultNowVault, err := NewStore(Config{DB: env.sqlite.RawDB(), KEK: env.kek, PostShredHook: env.sqlite.CheckpointWAL})
	if err != nil || defaultNowVault.nowMs() <= 0 {
		t.Fatalf("expected valid default NowMs vault store, got err=%v", err)
	}
	// SealSecret & RotateSecret fail cleanly when KEK provider has been closed (ActiveKeyID / UnwrapDEK fail).
	org2, _ := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "closed-kek-org", DisplayName: "CK"})
	closedKEK, _ := NewStaticKEKProvider("kek_c", random32Key(t))
	closedKEK.Close()
	ckStore, _ := NewStore(Config{DB: env.sqlite.RawDB(), KEK: closedKEK, PostShredHook: env.sqlite.CheckpointWAL})
	if _, err := ckStore.SealSecret(ctx, SealSecretParams{OrgID: org2.ID, Name: "n", SecretKind: SecretKindAPIKey, Plaintext: []byte("x")}); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey when ActiveKeyID fails, got %v", err)
	}
	// RotateSecret returns ErrDecryptionFailed when underlying wrapped_org_dek is corrupted.
	if _, err := env.vault.RotateSecret(ctx, org.ID, orgSec.ID, 2, RotateSecretParams{Plaintext: []byte("x")}); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed rotating with corrupted Org DEK, got %v", err)
	}

	_ = env.sqlite.Close()
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{OrgID: org.ID, Name: "closed", SecretKind: SecretKindAPIKey, Plaintext: []byte("x")}); err == nil {
		t.Fatalf("expected closed DB error on SealSecret")
	}
	if _, err := env.vault.RotateSecret(ctx, org.ID, orgSec.ID, 1, RotateSecretParams{Plaintext: []byte("x")}); err == nil {
		t.Fatalf("expected closed DB error on RotateSecret")
	}
	if _, err := env.vault.ListSecrets(ctx, org.ID, nil); err == nil {
		t.Fatalf("expected closed DB error on ListSecrets")
	}
	if _, err := env.vault.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2"); err == nil {
		t.Fatalf("expected closed DB error on RewrapAllOrgDEKs")
	}
	if err := env.vault.ShredProjectDEK(ctx, org.ID, prj.ID); err == nil {
		t.Fatalf("expected closed DB error on ShredProjectDEK")
	}
	if err := env.vault.ShredOrgDEK(ctx, org.ID); err == nil {
		t.Fatalf("expected closed DB error on ShredOrgDEK")
	}
	if _, err := env.vault.GetSecretMetadata(ctx, org.ID, orgSec.ID); err == nil {
		t.Fatalf("expected closed DB error on GetSecretMetadata")
	}
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, orgSec.ID); err == nil {
		t.Fatalf("expected closed DB error on UnsealWithCapability")
	}
}

func TestVaultCacheRollbackAndConcurrentShredRace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, 5*time.Minute)

	// 1. Rollback of SealSecret on non-existent org / CHECK error must leave zero uncommitted DEKs cached in RAM.
	orgRB, _ := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "rb-org", DisplayName: "RB"})
	dupID := idgen.MustNew(idgen.PrefixSecret)
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		ID: dupID, OrgID: orgRB.ID, Name: "first", SecretKind: SecretKindAPIKey, Plaintext: []byte("ok"),
	}); err != nil {
		t.Fatalf("seed dupID on orgRB: %v", err)
	}
	orgEmpty, _ := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "rb-empty", DisplayName: "Empty"})
	// Trigger SealSecret failure after ensureOrgDEKTx mints a DEK on orgEmpty (UNIQUE (id) collision with dupID!).
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		ID: dupID, OrgID: orgEmpty.ID, Name: "collide", SecretKind: SecretKindAPIKey, Plaintext: []byte("fail"),
	}); !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists causing SealSecret rollback, got %v", err)
	}
	env.vault.cache.mu.RLock()
	leakedEntry := env.vault.cache.orgDEKs[orgEmpty.ID]
	env.vault.cache.mu.RUnlock()
	if leakedEntry != nil {
		t.Fatalf("SECURITY BUG: rolled-back SealSecret left uncommitted DEK cached in RAM for %s", orgEmpty.ID)
	}

	// 2. Concurrent UnsealWithCapability racing with ShredProjectDEK and RewrapAllOrgDEKs under -race
	// must never resurrect shredded DEKs into dekCache after ShredProjectDEK returns.
	raceOrg, _ := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "race-org", DisplayName: "Race Org"})
	raceProj, _ := env.sqlite.Tenancy().CreateProject(ctx, raceOrg.ID, port.CreateProjectParams{Slug: "race-p", DisplayName: "Race P"})
	raceSec, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: raceOrg.ID, OwnerProjectID: &raceProj.ID, Name: "race-sec", SecretKind: SecretKindAPIKey, Plaintext: []byte("race-secret"),
	})
	if err != nil {
		t.Fatalf("SealSecret race: %v", err)
	}
	if err := env.kek.RegisterKey("kek_v2", random32Key(t), false); err != nil {
		t.Fatalf("RegisterKey kek_v2: %v", err)
	}

	var wg sync.WaitGroup
	stopReaders := make(chan struct{})
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
					if u, err := env.vault.UnsealWithCapability(ctx, env.cap, raceOrg.ID, raceSec.ID); err == nil {
						u.Zeroize()
					}
				}
			}
		}()
	}
	if _, err := env.vault.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2"); err != nil {
		t.Fatalf("RewrapAllOrgDEKs during race: %v", err)
	}
	if err := env.vault.ShredProjectDEK(ctx, raceOrg.ID, raceProj.ID); err != nil {
		t.Fatalf("ShredProjectDEK during race: %v", err)
	}
	close(stopReaders)
	wg.Wait()

	// After ShredProjectDEK + readers quiesce, ensure zero resurrected entries exist in dekCache.
	env.vault.cache.mu.RLock()
	resurrected := env.vault.cache.projDEKs[projectDEKCacheKey{orgID: raceOrg.ID, projectID: raceProj.ID}]
	env.vault.cache.mu.RUnlock()
	if resurrected != nil {
		t.Fatalf("SECURITY BUG: concurrent reader resurrected shredded DEK in RAM cache")
	}
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, raceOrg.ID, raceSec.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound post-shred, got %v", err)
	}

	// 3. Verify generation-guard directly rejects stale putOrgDEKIfGen / putProjectDEKIfGen and ListSecrets blank projectID & canceled ctx & PostShredHook error.
	preGen := env.vault.cache.generation()
	env.vault.cache.evictProject(raceOrg.ID, "other")
	if env.vault.cache.putOrgDEKIfGen("stale_org", "kek_v1", 1, random32Key(t), 1000, preGen) {
		t.Fatalf("expected stale putOrgDEKIfGen to be rejected after generation bump")
	}
	if env.vault.cache.putProjectDEKIfGen("stale_org", "stale_p", 1, 1, random32Key(t), 1000, preGen) {
		t.Fatalf("expected stale putProjectDEKIfGen to be rejected after generation bump")
	}
	blankProj := "  "
	if _, err := env.vault.ListSecrets(ctx, orgRB.ID, &blankProj); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on blank *projectID in ListSecrets, got %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := env.vault.GetSecretMetadata(cctx, orgRB.ID, dupID); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on GetSecretMetadata, got %v", err)
	}

	// Test PostShredHook error propagation cleanly on an isolated store.
	hookErr := errors.New("checkpoint boom")
	hookStore, _ := NewStore(Config{
		DB:            env.sqlite.RawDB(),
		KEK:           env.kek,
		PostShredHook: func(context.Context) error { return hookErr },
	})
	if err := hookStore.ShredOrgDEK(ctx, orgRB.ID); !errors.Is(err, hookErr) {
		t.Fatalf("expected wrapped PostShredHook error, got %v", err)
	}
	pHookPrj, _ := env.sqlite.Tenancy().CreateProject(ctx, orgEmpty.ID, port.CreateProjectParams{Slug: "hp", DisplayName: "HP"})
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{OrgID: orgEmpty.ID, OwnerProjectID: &pHookPrj.ID, Name: "hs", SecretKind: SecretKindAPIKey, Plaintext: []byte("hs")}); err != nil {
		t.Fatalf("seed hook project: %v", err)
	}
	if err := hookStore.ShredProjectDEK(ctx, orgEmpty.ID, pHookPrj.ID); !errors.Is(err, ErrPostShredCleanupFailed) || !errors.Is(err, hookErr) {
		t.Fatalf("expected ErrPostShredCleanupFailed wrapping PostShredHook error on ShredProjectDEK, got %v", err)
	}
	// Retry ShredProjectDEK with healthy hookStore succeeds idempotently when PostShredHook recovers.
	if hookStore.PendingShredCleanups() != 2 {
		t.Fatalf("expected 2 pending cleanups (orgRB + pHookPrj), got %d", hookStore.PendingShredCleanups())
	}
	hookErr = nil
	if err := hookStore.ShredProjectDEK(ctx, orgEmpty.ID, pHookPrj.ID); err != nil {
		t.Fatalf("retry ShredProjectDEK after hook recovery: %v", err)
	}
	if err := hookStore.ShredOrgDEK(ctx, orgRB.ID); err != nil {
		t.Fatalf("retry ShredOrgDEK after hook recovery: %v", err)
	}
	if hookStore.PendingShredCleanups() != 0 {
		t.Fatalf("expected 0 pending cleanups after recovery, got %d", hookStore.PendingShredCleanups())
	}
}

// TestVaultAdversarialPreShredBackupRestoreWithIntactKEK empirically tests the AC-3 / COND-PO-02
// adversarial scenario: restoring an immutable pre-shred SQLite backup alongside the SAME intact
// shared Master KEK (kek_v1) that was active before shredding. This proves the mathematical limit
// documented in CR-12: deleting a live Project DEK row does NOT make an already-copied cold backup
// undecipherable under an intact shared KEK unless kek_v1 itself is retired/destroyed or external
// per-project root keys are used.
func TestVaultAdversarialPreShredBackupRestoreWithIntactKEK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, 5*time.Minute)

	orgA, err := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "adv-a", DisplayName: "Adv A"})
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	p1, err := env.sqlite.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "adv-p1", DisplayName: "Adv P1"})
	if err != nil {
		t.Fatalf("CreateProj P1: %v", err)
	}
	p2, err := env.sqlite.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "adv-p2", DisplayName: "Adv P2"})
	if err != nil {
		t.Fatalf("CreateProj P2: %v", err)
	}

	p1Secret := []byte("adversarial-p1-secret-material")
	secP1, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &p1.ID, Name: "p1-key", SecretKind: SecretKindAPIKey, Plaintext: p1Secret,
	})
	if err != nil {
		t.Fatalf("Seal P1: %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &p2.ID, Name: "p2-key", SecretKind: SecretKindAPIKey, Plaintext: []byte("p2-ok"),
	}); err != nil {
		t.Fatalf("Seal P2: %v", err)
	}

	// Capture pre-shred SQLite backup BEFORE shredding P1.
	preShredBackup := filepath.Join(env.dir, "adversarial-pre-shred.db")
	if err := env.sqlite.BackupTo(ctx, preShredBackup); err != nil {
		t.Fatalf("BackupTo: %v", err)
	}

	// Shred P1 on the live DB while retaining intact shared kek_v1 for P2.
	if err := env.vault.ShredProjectDEK(ctx, orgA.ID, p1.ID); err != nil {
		t.Fatalf("ShredProjectDEK: %v", err)
	}

	// Adversarial restore: restore pre-shred backup onto clean dir using the SAME intact Master KEK (env.kek = kek_v1).
	restoredStore, err := sqlite.Open(ctx, sqlite.Config{DBPath: preShredBackup, SkipMigrate: true})
	if err != nil {
		t.Fatalf("open restored pre-shred backup: %v", err)
	}
	defer restoredStore.Close()
	restoredVault, err := NewStore(Config{
		DB:            restoredStore.RawDB(),
		KEK:           env.kek,
		PostShredHook: restoredStore.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("restoredVault NewStore: %v", err)
	}
	defer restoredVault.Close()

	// Empirical proof for CR-12: with intact shared kek_v1, pre-shred snapshot STILL decrypts secP1!
	recovered, err := restoredVault.UnsealWithCapability(ctx, env.cap, orgA.ID, secP1.ID)
	if err != nil || !bytes.Equal(recovered.Plaintext, p1Secret) {
		t.Fatalf("expected adversarial restore with intact shared kek_v1 to demonstrate CR-12 recoverability, err=%v", err)
	}
	recovered.Zeroize()
	t.Log("CR-12 EMPIRICAL PROOF: Restoring pre-shred SQLite backup with intact shared kek_v1 still decrypts deleted Project P1 secret; AC-3 pre-shred backup non-recoverability CANNOT hold without per-project external KEK destruction or KEK rotation+retirement.")

	// Conversely, if kek_v1 is rotated to kek_v2 and retired/destroyed from the KEK provider,
	// restoring the pre-shred backup fails closed with ErrKEKNotFound.
	kekV2 := random32Key(t)
	if err := env.kek.RegisterKey("kek_v2", kekV2, true); err != nil {
		t.Fatalf("RegisterKey kek_v2: %v", err)
	}
	if _, err := env.vault.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2"); err != nil {
		t.Fatalf("RewrapAllOrgDEKs: %v", err)
	}
	if err := env.kek.RetireKey("kek_v1"); err != nil {
		t.Fatalf("RetireKey kek_v1: %v", err)
	}
	restoredVault.EvictAllDEKs()
	if _, err := restoredVault.UnsealWithCapability(ctx, env.cap, orgA.ID, secP1.ID); !errors.Is(err, ErrKEKNotFound) {
		t.Fatalf("expected ErrKEKNotFound against pre-shred backup once kek_v1 is retired, got %v", err)
	}
}

func TestVaultShredBusyReaderCleanupFailureAndRetryRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, 5*time.Minute)

	org, err := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "busy-shred-org", DisplayName: "Busy Shred"})
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	prj, err := env.sqlite.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "busy-prj", DisplayName: "Busy Prj"})
	if err != nil {
		t.Fatalf("CreateProj: %v", err)
	}
	sec, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: org.ID, OwnerProjectID: &prj.ID, Name: "busy-sec", SecretKind: SecretKindAPIKey, Plaintext: []byte("busy-secret"),
	})
	if err != nil {
		t.Fatalf("SealSecret: %v", err)
	}

	// Capture wrapped_project_dek bytes prior to shred so we can inspect WAL residue.
	var pWrappedBytes []byte
	if err := env.sqlite.RawDB().QueryRowContext(ctx,
		`SELECT wrapped_project_dek FROM project_encryption_keys WHERE org_id = ? AND project_id = ?`,
		org.ID, prj.ID).Scan(&pWrappedBytes); err != nil {
		t.Fatalf("scan wrapped_project_dek: %v", err)
	}

	// Hold an active WAL read snapshot on an independent connection so TRUNCATE checkpoint hits busy=1.
	readerDSN := "file:" + filepath.ToSlash(env.dbPath) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(25)"
	readerDB, err := sql.Open("sqlite", readerDSN)
	if err != nil {
		t.Fatalf("open readerDB: %v", err)
	}
	defer readerDB.Close()
	rtx, err := readerDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("reader BeginTx: %v", err)
	}
	var dummy int
	if err := rtx.QueryRowContext(ctx, `SELECT count(*) FROM project_encryption_keys`).Scan(&dummy); err != nil {
		t.Fatalf("reader snapshot select: %v", err)
	}

	// Lower busy_timeout on store handle so CheckpointWAL fails fast during busy reader test.
	if _, err := env.sqlite.RawDB().ExecContext(ctx, "PRAGMA busy_timeout = 25"); err != nil {
		t.Fatalf("set short busy_timeout: %v", err)
	}

	// 1. Attempt ShredProjectDEK while busy reader holds snapshot -> MUST fail closed with ErrPostShredCleanupFailed + ErrWALCheckpointBusy!
	shredErr := env.vault.ShredProjectDEK(ctx, org.ID, prj.ID)
	if !errors.Is(shredErr, ErrPostShredCleanupFailed) || !errors.Is(shredErr, sqlite.ErrWALCheckpointBusy) {
		t.Fatalf("expected ErrPostShredCleanupFailed wrapping ErrWALCheckpointBusy, got %v", shredErr)
	}
	if env.vault.PendingShredCleanups() != 1 {
		t.Fatalf("expected 1 pending shred cleanup after busy failure, got %d", env.vault.PendingShredCleanups())
	}
	// Live unseal MUST already fail closed because DB row was deleted and RAM cache was evicted.
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, sec.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on live unseal while WAL cleanup pending, got %v", err)
	}

	// 2. Release busy reader snapshot -> retry ShredProjectDEK recovers and truncates WAL cleanly.
	if err := rtx.Rollback(); err != nil {
		t.Fatalf("reader rollback: %v", err)
	}
	_ = readerDB.Close()

	if err := env.vault.ShredProjectDEK(ctx, org.ID, prj.ID); err != nil {
		t.Fatalf("retry ShredProjectDEK after busy reader closed: %v", err)
	}
	if env.vault.PendingShredCleanups() != 0 {
		t.Fatalf("expected 0 pending cleanups after retry success, got %d", env.vault.PendingShredCleanups())
	}

	// 3. Subsequent duplicate ShredProjectDEK returns ErrNotFound, and 0 deleted key bytes remain in .db/-wal/-shm.
	if err := env.vault.ShredProjectDEK(ctx, org.ID, prj.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on completed duplicate ShredProjectDEK, got %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if rawDisk, rerr := os.ReadFile(env.dbPath + suffix); rerr == nil {
			if bytes.Contains(rawDisk, pWrappedBytes) {
				t.Fatalf("residual deleted wrapped_project_dek bytes persisted in %s after retry recovery", env.dbPath+suffix)
			}
		}
	}
}
