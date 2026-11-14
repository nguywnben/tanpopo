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
	"sync/atomic"
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
	if err := env.vault.ShredProjectDEK(ctx, orgA.ID, p1.ID, ShredRequest{RequestID: "ac3-shred-p1", ExpectedGeneration: 1}); err != nil {
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

	if err := env.vault.ShredOrgDEK(ctx, orgA.ID, ShredRequest{RequestID: "ac3-shred-orga", ExpectedGeneration: 1}); err != nil {
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
	if err := env.vault.ShredProjectDEK(ctx, orgA.ID, p1.ID, ShredRequest{RequestID: "ac3-dup-p1", ExpectedGeneration: 2}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on double ShredProjectDEK, got %v", err)
	}
	if err := env.vault.ShredOrgDEK(ctx, orgA.ID, ShredRequest{RequestID: "ac3-dup-orga", ExpectedGeneration: 1}); !errors.Is(err, port.ErrNotFound) {
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
	if _, err := LoadStaticKEKFromFile("kek_blank", "   "); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected blank keyfile path rejection, got %v", err)
	}
	if _, err := LoadStaticKEKFromFile("kek_missing", filepath.Join(keyDir, "does-not-exist.key")); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected missing keyfile rejection, got %v", err)
	}
	badContentPath := filepath.Join(keyDir, "bad.key")
	if err := os.WriteFile(badContentPath, []byte("not-a-valid-key"), 0o600); err != nil {
		t.Fatalf("write bad keyfile: %v", err)
	}
	if _, err := LoadStaticKEKFromFile("kek_badcontent", badContentPath); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected malformed keyfile rejection, got %v", err)
	}
	if _, err := LoadStaticKEKFromFile("kek_dir", keyDir); !errors.Is(err, ErrInsecureKeyfilePermissions) {
		t.Fatalf("expected directory keyfile rejection with ErrInsecureKeyfilePermissions, got %v", err)
	}
	if _, err := ParseMasterKeyString("  "); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected blank ParseMasterKeyString rejection, got %v", err)
	}
	if _, err := ParseMasterKeyString(hex.EncodeToString(raw32)); err != nil {
		t.Fatalf("expected valid hex ParseMasterKeyString, got %v", err)
	}
	if p, err := LoadStaticKEKFromEnv("kek_hex", func(k string) (string, bool) {
		if k == EnvMasterKey {
			return hexKey, true
		}
		return "", false
	}); err != nil {
		t.Fatalf("LoadStaticKEKFromEnv hex: %v", err)
	} else {
		p.Close()
	}
	if _, err := ParseMasterKeyString("!!!"); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected invalid base64 ParseMasterKeyString rejection, got %v", err)
	}
	if _, err := ParseMasterKeyString("AAAA"); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected short key ParseMasterKeyString rejection, got %v", err)
	}
	if _, err := openAESGCM(random32Key(t), nil, make([]byte, 12), []byte("tiny")); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed on short AES-GCM ciphertext, got %v", err)
	}
	if _, _, err := sealAESGCM([]byte("short"), nil, []byte("x")); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey on short key in sealAESGCM, got %v", err)
	}
	pValid, err := NewStaticKEKProvider("kek_check", random32Key(t))
	if err != nil {
		t.Fatalf("NewStaticKEKProvider kek_check: %v", err)
	}
	if err := pValid.RegisterKey("", random32Key(t), false); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected blank RegisterKey ID rejection, got %v", err)
	}
	if err := pValid.RegisterKey("kek_short", []byte("short"), false); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected short RegisterKey rejection, got %v", err)
	}
	pValid.Close()
	if _, _, err := pValid.WrapDEK(ctx, "kek_check", "org_x", 1, random32Key(t)); !errors.Is(err, ErrKEKNotFound) {
		t.Fatalf("expected WrapDEK on closed KEK rejection with ErrKEKNotFound, got %v", err)
	}
	emptyKeyFile := filepath.Join(keyDir, "empty.key")
	_ = os.WriteFile(emptyKeyFile, []byte(""), 0o600)
	if _, err := LoadStaticKEKFromFile("kek_empty", emptyKeyFile); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected empty keyfile rejection, got %v", err)
	}
	zeroBinFile := filepath.Join(keyDir, "zero32.key")
	_ = os.WriteFile(zeroBinFile, make([]byte, 32), 0o600)
	if _, err := LoadStaticKEKFromFile("kek_zero32", zeroBinFile); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected 32-byte all-zero keyfile rejection, got %v", err)
	}
	if _, err := orgDEKWrapAAD("", "org_1", 1); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected orgDEKWrapAAD blank kekID rejection, got %v", err)
	}
	if _, err := orgDEKWrapAAD("kek_1", "org:bad", 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected orgDEKWrapAAD bad orgID rejection, got %v", err)
	}
	if _, err := orgDEKWrapAAD("kek_1", "org_1", 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected orgDEKWrapAAD zero version rejection, got %v", err)
	}
	if err := checkPOSIXKeyfileMode("linux", 0o600); err != nil {
		t.Fatalf("expected checkPOSIXKeyfileMode 0600 on linux to pass, got %v", err)
	}
	if err := checkPOSIXKeyfileMode("linux", 0o644); !errors.Is(err, ErrInsecureKeyfilePermissions) {
		t.Fatalf("expected checkPOSIXKeyfileMode 0644 on linux to fail with ErrInsecureKeyfilePermissions, got %v", err)
	}
	if _, err := LoadStaticKEKFromEnv("kek_missfile", func(k string) (string, bool) {
		if k == EnvMasterKeyFile {
			return filepath.Join(keyDir, "nope.key"), true
		}
		return "", false
	}); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected missing keyfile via env to fail, got %v", err)
	}
	if _, err := LoadStaticKEKFromEnv("kek_badenvhex", func(k string) (string, bool) {
		if k == EnvMasterKey {
			return "not-hex-or-b64!!", true
		}
		return "", false
	}); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected invalid EnvMasterKey rejection, got %v", err)
	}
	if _, err := pValid.UnwrapDEK(ctx, "kek_nonexistent", "org_x", 1, make([]byte, 12), make([]byte, 32)); !errors.Is(err, ErrKEKNotFound) {
		t.Fatalf("expected UnwrapDEK missing key ErrKEKNotFound, got %v", err)
	}
	envRot := newVaultTestEnv(t, 0)
	if _, err := envRot.vault.RotateSecret(ctx, "org_1", "sec_1", 0, RotateSecretParams{Plaintext: []byte("x")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected RotateSecret expectedVersion=0 ErrInvalidInput, got %v", err)
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

	// Optimistic lock conflict on stale expectedVersion=1 + ClearBoundAudience on v2->v3, plus Org-scoped RotateSecret.
	if _, err := env.vault.RotateSecret(ctx, org.ID, meta.ID, 1, RotateSecretParams{Plaintext: []byte("x")}); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict on stale RotateSecret, got %v", err)
	}
	v3Meta, err := env.vault.RotateSecret(ctx, org.ID, meta.ID, 2, RotateSecretParams{Plaintext: v2Secret, ClearBoundAudience: true})
	if err != nil || v3Meta.BoundAudience != nil || v3Meta.Version != 3 {
		t.Fatalf("expected cleared bound_audience at v3: %+v err=%v", v3Meta, err)
	}
	orgSecV1, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: org.ID, Name: "org-rotatable", SecretKind: SecretKindAPIKey, Plaintext: v1Secret,
	})
	if err != nil {
		t.Fatalf("SealSecret org-rotatable: %v", err)
	}
	orgSecV2, err := env.vault.RotateSecret(ctx, org.ID, orgSecV1.ID, 1, RotateSecretParams{
		Plaintext: v2Secret, BoundAudience: &newAud,
	})
	if err != nil || orgSecV2.OwnerProjectID != nil || orgSecV2.Version != 2 {
		t.Fatalf("expected org-scoped RotateSecret success, got %+v err=%v", orgSecV2, err)
	}
	// FK error on SealSecret referencing nonexistent organization returns ErrNotFound.
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: idgen.MustNew(idgen.PrefixOrg), Name: "no-org", SecretKind: SecretKindAPIKey, Plaintext: v1Secret,
	}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on nonexistent org SealSecret, got %v", err)
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
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{ID: "bad_prefix", OrgID: org.ID, Name: "badid", SecretKind: SecretKindAPIKey, Plaintext: []byte("x")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on bad secret prefix: %v", err)
	}
	badSeg := "bad:seg"
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{OrgID: "org:bad", Name: "badorg", SecretKind: SecretKindAPIKey, Plaintext: []byte("x")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on colon in OrgID: %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{OrgID: org.ID, OwnerProjectID: &badSeg, Name: "badproj", SecretKind: SecretKindAPIKey, Plaintext: []byte("x")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on colon in OwnerProjectID: %v", err)
	}
	if _, err := projectDEKWrapAAD("org_1", "prj_1", 0, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on zero DEK version in projectDEKWrapAAD: %v", err)
	}
	if _, err := projectDEKWrapAAD("org:1", "prj_1", 1, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on invalid org in projectDEKWrapAAD: %v", err)
	}
	if _, err := OrgSecretAAD("org_1", "sec:bad", SecretKindAPIKey); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on bad cred ID in OrgSecretAAD: %v", err)
	}
	if _, err := OrgSecretAAD("org_1", "sec_1", "bad:kind"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on bad secret kind in OrgSecretAAD: %v", err)
	}
	if _, err := ProjectRecordAAD("org_1", "prj_1", "unsupported_kind", "rec_1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on bad record kind in ProjectRecordAAD: %v", err)
	}
	if _, err := ProjectRecordAAD("org_1", "prj_1", RecordKindCred, "rec:bad"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on bad record ID in ProjectRecordAAD: %v", err)
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
	if err := env.vault.ShredProjectDEK(ctx, "", prj.ID, ShredRequest{RequestID: "edge-blank-org", ExpectedGeneration: 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ShredProjectDEK blank: %v", err)
	}
	if err := env.vault.ShredProjectDEK(ctx, org.ID, prj.ID, ShredRequest{RequestID: "", ExpectedGeneration: 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ShredProjectDEK blank requestID: %v", err)
	}
	if err := env.vault.ShredProjectDEK(ctx, org.ID, prj.ID, ShredRequest{RequestID: "edge-zero-gen", ExpectedGeneration: 0}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ShredProjectDEK zero gen: %v", err)
	}
	if err := env.vault.ShredOrgDEK(ctx, "", ShredRequest{RequestID: "edge-blank-org2", ExpectedGeneration: 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ShredOrgDEK blank: %v", err)
	}
	if err := env.vault.ShredOrgDEK(ctx, org.ID, ShredRequest{RequestID: "", ExpectedGeneration: 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ShredOrgDEK blank requestID: %v", err)
	}
	if _, err := env.vault.GetOrgKeyMetadata(ctx, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("GetOrgKeyMetadata blank: %v", err)
	}
	if _, err := env.vault.GetProjectKeyMetadata(ctx, org.ID, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("GetProjectKeyMetadata blank: %v", err)
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
	// Rewrap fails atomically when an existing org DEK cannot be unwrapped under oldKEKID.
	rwEnv := newVaultTestEnv(t, 0)
	rwOrg, _ := rwEnv.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "rw-fail", DisplayName: "RW Fail"})
	if _, err := rwEnv.vault.SealSecret(ctx, SealSecretParams{OrgID: rwOrg.ID, Name: "rw-sec", SecretKind: SecretKindAPIKey, Plaintext: []byte("rw")}); err != nil {
		t.Fatalf("SealSecret rwOrg: %v", err)
	}
	if err := rwEnv.kek.RegisterKey("kek_old_tmp", random32Key(t), false); err != nil {
		t.Fatalf("RegisterKey kek_old_tmp: %v", err)
	}
	if _, err := rwEnv.vault.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_old_tmp"); err != nil {
		t.Fatalf("RewrapAllOrgDEKs to kek_old_tmp: %v", err)
	}
	_ = rwEnv.kek.RetireKey("kek_old_tmp")
	if _, err := rwEnv.vault.RewrapAllOrgDEKs(ctx, "kek_old_tmp", "kek_v1"); !errors.Is(err, ErrKEKNotFound) {
		t.Fatalf("expected ErrKEKNotFound when oldKEKID retired mid-rewrap, got %v", err)
	}
	if err := rwEnv.kek.RegisterKey("kek_old_tmp", random32Key(t), false); err != nil {
		t.Fatalf("RegisterKey mismatched kek_old_tmp: %v", err)
	}
	if _, err := rwEnv.vault.RewrapAllOrgDEKs(ctx, "kek_old_tmp", "kek_v1"); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed on mismatched old KEK during rewrap, got %v", err)
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
	env.vault.cache.putOrgDEKIfGen(org.ID, "kek_stale", 1, 1, staleOrgSlice, env.clock.Now().UnixMilli(), env.vault.cache.generation())
	staleProjSlice := random32Key(t)
	env.vault.cache.putProjectDEKIfGen(org.ID, prj.ID, 99, 1, 1, staleProjSlice, env.clock.Now().UnixMilli(), env.vault.cache.generation())
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: org.ID, OwnerProjectID: &prj.ID, Name: "refresh-after-stale", SecretKind: SecretKindAPIKey, Plaintext: []byte("ok"),
	}); err != nil {
		t.Fatalf("SealSecret stale cache overwrite: %v", err)
	}
	env.vault.cache.putOrgDEKIfGen(org.ID, "kek_stale", 1, 1, random32Key(t), env.clock.Now().UnixMilli(), env.vault.cache.generation())
	env.vault.cache.putProjectDEKIfGen(org.ID, prj.ID, 99, 1, 1, random32Key(t), env.clock.Now().UnixMilli(), env.vault.cache.generation())
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
	if err := checkPOSIXKeyfileMode("linux", 0o600); err != nil {
		t.Fatalf("expected checkPOSIXKeyfileMode 0600 on linux to pass, got %v", err)
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
	// RotateSecret missing secret returns ErrNotFound; malformed nonce in DB triggers ErrDecryptionFailed on unseal.
	if _, err := env.vault.RotateSecret(ctx, org.ID, "sec_nonexistent", 1, RotateSecretParams{Plaintext: []byte("x")}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing secret RotateSecret, got %v", err)
	}
	_, _ = env.sqlite.RawDB().ExecContext(ctx, `UPDATE secret_credentials SET nonce = X'deadbeef' WHERE org_id = ? AND id = ?`, org3.ID, sec3.ID)
	if _, err := env.vault.UnsealWithCapability(ctx, env.cap, org3.ID, sec3.ID); !errors.Is(err, ErrDecryptionFailed) && !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrDecryptionFailed on truncated nonce, got %v", err)
	}
	// Contended shredSem timeout on ShredProjectDEK & ShredOrgDEK, plus ShredOrgDEK post-hook DB close failure.
	env.vault.shredSem <- struct{}{}
	sdCtx, sdCancel := context.WithTimeout(ctx, 5*time.Millisecond)
	if err := env.vault.ShredProjectDEK(sdCtx, org3.ID, prj3.ID, ShredRequest{RequestID: "held-p3", ExpectedGeneration: 1}); !errors.Is(err, context.DeadlineExceeded) {
		sdCancel()
		<-env.vault.shredSem
		t.Fatalf("expected DeadlineExceeded on ShredProjectDEK with held shredSem, got %v", err)
	}
	sdCancel()
	sdCtx2, sdCancel2 := context.WithTimeout(ctx, 5*time.Millisecond)
	if err := env.vault.ShredOrgDEK(sdCtx2, org3.ID, ShredRequest{RequestID: "held-org3", ExpectedGeneration: 1}); !errors.Is(err, context.DeadlineExceeded) {
		sdCancel2()
		<-env.vault.shredSem
		t.Fatalf("expected DeadlineExceeded on ShredOrgDEK with held shredSem, got %v", err)
	}
	sdCancel2()
	<-env.vault.shredSem

	org3b, _ := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "shred-close-org", DisplayName: "SC"})
	_, _ = env.vault.SealSecret(ctx, SealSecretParams{OrgID: org3b.ID, Name: "sc", SecretKind: SecretKindAPIKey, Plaintext: []byte("sc")})
	closeOrgStore, _ := NewStore(Config{
		DB:  env.sqlite.RawDB(),
		KEK: env.kek,
		PostShredHook: func(ctx context.Context) error {
			return env.sqlite.Close()
		},
	})
	if err := closeOrgStore.ShredOrgDEK(ctx, org3b.ID, ShredRequest{RequestID: "close-org3b", ExpectedGeneration: 1}); err == nil {
		t.Fatalf("expected error clearing pending org cleanup after DB closed during PostShredHook")
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
	if err := env.vault.ShredProjectDEK(ctx, org.ID, prj.ID, ShredRequest{RequestID: "closed-proj", ExpectedGeneration: 1}); err == nil {
		t.Fatalf("expected closed DB error on ShredProjectDEK")
	}
	if err := env.vault.ShredOrgDEK(ctx, org.ID, ShredRequest{RequestID: "closed-org", ExpectedGeneration: 1}); err == nil {
		t.Fatalf("expected closed DB error on ShredOrgDEK")
	}
	if _, err := env.vault.GetOrgKeyMetadata(ctx, org.ID); err == nil {
		t.Fatalf("expected closed DB error on GetOrgKeyMetadata")
	}
	if _, err := env.vault.GetProjectKeyMetadata(ctx, org.ID, prj.ID); err == nil {
		t.Fatalf("expected closed DB error on GetProjectKeyMetadata")
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
	if err := env.vault.ShredProjectDEK(ctx, raceOrg.ID, raceProj.ID, ShredRequest{RequestID: "race-proj-1", ExpectedGeneration: 1}); err != nil {
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
	if env.vault.cache.putOrgDEKIfGen("stale_org", "kek_v1", 1, 1, random32Key(t), 1000, preGen) {
		t.Fatalf("expected stale putOrgDEKIfGen to be rejected after generation bump")
	}
	if env.vault.cache.putProjectDEKIfGen("stale_org", "stale_p", 1, 1, 1, random32Key(t), 1000, preGen) {
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
	if err := hookStore.ShredOrgDEK(ctx, orgRB.ID, ShredRequest{RequestID: "hook-orgrb-1", ExpectedGeneration: 1}); !errors.Is(err, hookErr) {
		t.Fatalf("expected wrapped PostShredHook error, got %v", err)
	}
	pHookPrj, _ := env.sqlite.Tenancy().CreateProject(ctx, orgEmpty.ID, port.CreateProjectParams{Slug: "hp", DisplayName: "HP"})
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{OrgID: orgEmpty.ID, OwnerProjectID: &pHookPrj.ID, Name: "hs", SecretKind: SecretKindAPIKey, Plaintext: []byte("hs")}); err != nil {
		t.Fatalf("seed hook project: %v", err)
	}
	if err := hookStore.ShredProjectDEK(ctx, orgEmpty.ID, pHookPrj.ID, ShredRequest{RequestID: "hook-proj-1", ExpectedGeneration: 1}); !errors.Is(err, ErrPostShredCleanupFailed) || !errors.Is(err, hookErr) {
		t.Fatalf("expected ErrPostShredCleanupFailed wrapping PostShredHook error on ShredProjectDEK, got %v", err)
	}
	// Retry ShredProjectDEK with healthy hookStore succeeds idempotently when PostShredHook recovers.
	if hookStore.PendingShredCleanups() != 2 {
		t.Fatalf("expected 2 pending cleanups (orgRB + pHookPrj), got %d", hookStore.PendingShredCleanups())
	}
	hookErr = nil
	if err := hookStore.ShredProjectDEK(ctx, orgEmpty.ID, pHookPrj.ID, ShredRequest{RequestID: "hook-proj-1", ExpectedGeneration: 1}); err != nil {
		t.Fatalf("retry ShredProjectDEK after hook recovery: %v", err)
	}
	if err := hookStore.ShredOrgDEK(ctx, orgRB.ID, ShredRequest{RequestID: "hook-orgrb-1", ExpectedGeneration: 1}); err != nil {
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
	if err := env.vault.ShredProjectDEK(ctx, orgA.ID, p1.ID, ShredRequest{RequestID: "adv-shred-p1", ExpectedGeneration: 1}); err != nil {
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
	busyReq := ShredRequest{RequestID: "busy-shred-p1", ExpectedGeneration: 1}
	shredErr := env.vault.ShredProjectDEK(ctx, org.ID, prj.ID, busyReq)
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

	if err := env.vault.ShredProjectDEK(ctx, org.ID, prj.ID, busyReq); err != nil {
		t.Fatalf("retry ShredProjectDEK after busy reader closed: %v", err)
	}
	if env.vault.PendingShredCleanups() != 0 {
		t.Fatalf("expected 0 pending cleanups after retry success, got %d", env.vault.PendingShredCleanups())
	}

	// 3. Replay of same completed RequestID returns idempotent nil while a distinct RequestID returns ErrNotFound,
	// and 0 deleted key bytes remain in .db/-wal/-shm.
	if err := env.vault.ShredProjectDEK(ctx, org.ID, prj.ID, busyReq); err != nil {
		t.Fatalf("expected idempotent nil replaying completed ShredRequest before recreate, got %v", err)
	}
	if err := env.vault.ShredProjectDEK(ctx, org.ID, prj.ID, ShredRequest{RequestID: "busy-shred-p1-dup", ExpectedGeneration: 1}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on fresh duplicate ShredProjectDEK, got %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if rawDisk, rerr := os.ReadFile(env.dbPath + suffix); rerr == nil {
			if bytes.Contains(rawDisk, pWrappedBytes) {
				t.Fatalf("residual deleted wrapped_project_dek bytes persisted in %s after retry recovery", env.dbPath+suffix)
			}
		}
	}
}

func TestVaultCrashRecoveryAcrossStoreRestartAndDEKRecreationBlock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "crashrec", "tanpopo.db")
	fc := clock.NewFakeClock(time.Date(2026, 11, 14, 21, 0, 0, 0, time.UTC))

	st1, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open st1: %v", err)
	}
	kek, err := NewStaticKEKProvider("kek_v1", random32Key(t))
	if err != nil {
		_ = st1.Close()
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	defer kek.Close()

	store1, err := NewStore(Config{
		DB:            st1.RawDB(),
		KEK:           kek,
		CacheTTL:      5 * time.Minute,
		NowMs:         func() int64 { return fc.Now().UnixMilli() },
		PostShredHook: st1.CheckpointWAL,
	})
	if err != nil {
		_ = st1.Close()
		t.Fatalf("NewStore store1: %v", err)
	}
	capToken := MintUnsealCapabilityForBroker()

	orgA, err := st1.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "cr-a", DisplayName: "CR A"})
	if err != nil {
		t.Fatalf("CreateOrg A: %v", err)
	}
	p1, err := st1.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p1", DisplayName: "P1"})
	if err != nil {
		t.Fatalf("CreateProj P1: %v", err)
	}
	p2, err := st1.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p2", DisplayName: "P2"})
	if err != nil {
		t.Fatalf("CreateProj P2: %v", err)
	}
	secP1, err := store1.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &p1.ID, Name: "p1-sec", SecretKind: SecretKindAPIKey, Plaintext: []byte("p1-secret-val"),
	})
	if err != nil {
		t.Fatalf("Seal P1: %v", err)
	}
	secP2, err := store1.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &p2.ID, Name: "p2-sec", SecretKind: SecretKindAPIKey, Plaintext: []byte("p2-secret-val"),
	})
	if err != nil {
		t.Fatalf("Seal P2: %v", err)
	}

	var p1WrappedBytes []byte
	if err := st1.RawDB().QueryRowContext(ctx,
		`SELECT wrapped_project_dek FROM project_encryption_keys WHERE org_id = ? AND project_id = ?`,
		orgA.ID, p1.ID).Scan(&p1WrappedBytes); err != nil {
		t.Fatalf("scan p1 wrapped_project_dek: %v", err)
	}

	// Keep an external WAL snapshot reader active across Store 1 close -> Store 2 open!
	readerDSN := "file:" + filepath.ToSlash(dbPath) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(25)"
	readerDB, err := sql.Open("sqlite", readerDSN)
	if err != nil {
		t.Fatalf("open readerDB: %v", err)
	}
	defer readerDB.Close()
	rtx, err := readerDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("reader BeginTx: %v", err)
	}
	var snapCount int
	if err := rtx.QueryRowContext(ctx, `SELECT count(*) FROM project_encryption_keys`).Scan(&snapCount); err != nil {
		t.Fatalf("reader snapshot select: %v", err)
	}
	if _, err := st1.RawDB().ExecContext(ctx, "PRAGMA busy_timeout = 25"); err != nil {
		t.Fatalf("set short busy_timeout st1: %v", err)
	}

	// 1. PostShredHook fails after DEK commit -> Store 1 fails closed.
	crP1Req := ShredRequest{RequestID: "cr-shred-p1", ExpectedGeneration: 1}
	if err := store1.ShredProjectDEK(ctx, orgA.ID, p1.ID, crP1Req); !errors.Is(err, ErrPostShredCleanupFailed) || !errors.Is(err, sqlite.ErrWALCheckpointBusy) {
		t.Fatalf("expected ErrPostShredCleanupFailed wrapping ErrWALCheckpointBusy, got %v", err)
	}
	if store1.PendingShredCleanups() != 1 {
		t.Fatalf("expected PendingShredCleanups=1 on store1, got %d", store1.PendingShredCleanups())
	}

	// 2. Attempting to re-create Project P1 DEK while WAL cleanup is pending MUST fail closed!
	if _, err := store1.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &p1.ID, Name: "p1-recreate-premature", SecretKind: SecretKindAPIKey, Plaintext: []byte("nope"),
	}); !errors.Is(err, ErrPostShredCleanupFailed) || !errors.Is(err, ErrPendingShredCleanup) {
		t.Fatalf("expected SealSecret on pending-cleanup project to fail closed with ErrPendingShredCleanup, got %v", err)
	}
	// Sibling Project P2 continues operating 100% normally.
	if u2, err := store1.UnsealWithCapability(ctx, capToken, orgA.ID, secP2.ID); err != nil || string(u2.Plaintext) != "p2-secret-val" {
		t.Fatalf("expected sibling P2 unseal to succeed, err=%v", err)
	} else {
		u2.Zeroize()
	}

	// 3. Simulate process restart: close Store 1 completely and instantiate brand-new Store 2!
	store1.Close()
	if err := st1.Close(); err != nil {
		t.Fatalf("close st1: %v", err)
	}

	st2, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open st2: %v", err)
	}
	defer st2.Close()
	if _, err := st2.RawDB().ExecContext(ctx, "PRAGMA busy_timeout = 25"); err != nil {
		t.Fatalf("set short busy_timeout st2: %v", err)
	}
	store2, err := NewStore(Config{
		DB:            st2.RawDB(),
		KEK:           kek,
		CacheTTL:      5 * time.Minute,
		NowMs:         func() int64 { return fc.Now().UnixMilli() },
		PostShredHook: st2.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("NewStore store2: %v", err)
	}
	defer store2.Close()

	// Store 2 immediately recovers durable pending cleanup state upon restart.
	if got := store2.PendingShredCleanups(); got != 1 {
		t.Fatalf("expected store2.PendingShredCleanups()=1 after restart, got %d", got)
	}

	// Retry on Store 2 MUST still fail closed (NOT ErrNotFound, NOT nil) while WAL reader stays open!
	if err := store2.ShredProjectDEK(ctx, orgA.ID, p1.ID, crP1Req); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("retry ShredProjectDEK on store2 while WAL busy expected ErrPostShredCleanupFailed, got %v", err)
	}
	if err := store2.FlushPendingShredCleanups(ctx); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("FlushPendingShredCleanups while WAL busy expected ErrPostShredCleanupFailed, got %v", err)
	}
	if _, err := store2.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &p1.ID, Name: "p1-recreate-after-restart", SecretKind: SecretKindAPIKey, Plaintext: []byte("still-nope"),
	}); !errors.Is(err, ErrPendingShredCleanup) {
		t.Fatalf("expected SealSecret after restart to block P1 re-creation while pending, got %v", err)
	}

	// 4. Close WAL snapshot reader -> retry on Store 2 completes physical WAL erasure!
	_ = rtx.Rollback()
	_ = readerDB.Close()

	if err := store2.ShredProjectDEK(ctx, orgA.ID, p1.ID, crP1Req); err != nil {
		t.Fatalf("retry ShredProjectDEK on store2 after WAL reader closed: %v", err)
	}
	if got := store2.PendingShredCleanups(); got != 0 {
		t.Fatalf("expected 0 pending cleanups after store2 recovery, got %d", got)
	}
	if err := store2.ShredProjectDEK(ctx, orgA.ID, p1.ID, ShredRequest{RequestID: "cr-shred-p1-dup", ExpectedGeneration: 1}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on completed duplicate ShredProjectDEK, got %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if rawDisk, rerr := os.ReadFile(dbPath + suffix); rerr == nil {
			if bytes.Contains(rawDisk, p1WrappedBytes) {
				t.Fatalf("residual deleted wrapped_project_dek persisted in %s after restarted Store 2 cleanup", dbPath+suffix)
			}
		}
	}

	// 5. Once cleanup is verified complete, P1 can cleanly create a brand-new Project DEK,
	// while old shredded secret remains permanently unrecoverable (ErrNotFound).
	if _, err := store2.UnsealWithCapability(ctx, capToken, orgA.ID, secP1.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("old secP1 must remain ErrNotFound after recovery, got %v", err)
	}
	secP1New, err := store2.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &p1.ID, Name: "p1-sec-new", SecretKind: SecretKindAPIKey, Plaintext: []byte("new-p1-secret"),
	})
	if err != nil {
		t.Fatalf("post-cleanup SealSecret on P1 failed: %v", err)
	}
	if uNew, err := store2.UnsealWithCapability(ctx, capToken, orgA.ID, secP1New.ID); err != nil || string(uNew.Plaintext) != "new-p1-secret" {
		t.Fatalf("expected post-cleanup new P1 secret to unseal cleanly, err=%v", err)
	} else {
		uNew.Zeroize()
	}

	// 6. Simulate hard crash between DB commit and PostShredHook return on ShredOrgDEK (Org B)
	// and verify Store restart + FlushPendingShredCleanups + Org DEK recreation block.
	orgB, _ := st2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "cr-b", DisplayName: "CR B"})
	prjB, _ := st2.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "pb", DisplayName: "PB"})
	secOrgB, err := store2.SealSecret(ctx, SealSecretParams{
		OrgID: orgB.ID, OwnerProjectID: &prjB.ID, Name: "orgb-sec", SecretKind: SecretKindAPIKey, Plaintext: []byte("orgb-val"),
	})
	if err != nil {
		t.Fatalf("Seal orgB: %v", err)
	}
	var orgBWrapped []byte
	if err := st2.RawDB().QueryRowContext(ctx, `SELECT wrapped_org_dek FROM org_encryption_keys WHERE org_id = ?`, orgB.ID).Scan(&orgBWrapped); err != nil {
		t.Fatalf("scan orgB wrapped_org_dek: %v", err)
	}

	var crashErr = errors.New("simulated hard process abort right after commit")
	panicStore, err := NewStore(Config{
		DB:  st2.RawDB(),
		KEK: kek,
		PostShredHook: func(context.Context) error {
			return crashErr
		},
	})
	if err != nil {
		t.Fatalf("NewStore panicStore: %v", err)
	}
	crOrgBReq := ShredRequest{RequestID: "cr-shred-orgb", ExpectedGeneration: 1}
	if err := panicStore.ShredOrgDEK(ctx, orgB.ID, crOrgBReq); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("expected ErrPostShredCleanupFailed on aborted ShredOrgDEK, got %v", err)
	}
	panicStore.Close() // Discard crashed store instance immediately.

	// Re-check from store2 (fresh store relative to panicStore) while simulated PostShredHook still fails.
	failingRecoverStore, _ := NewStore(Config{
		DB:  st2.RawDB(),
		KEK: kek,
		PostShredHook: func(context.Context) error {
			return errors.New("recovery wal failure")
		},
	})
	defer failingRecoverStore.Close()
	if failingRecoverStore.PendingShredCleanups() != 1 {
		t.Fatalf("expected 1 pending org shred cleanup on new store, got %d", failingRecoverStore.PendingShredCleanups())
	}
	// Attempting to seal an Org-scoped or Project-scoped secret under pending Org B shred must fail closed.
	if _, err := failingRecoverStore.SealSecret(ctx, SealSecretParams{
		OrgID: orgB.ID, Name: "orgb-recreate", SecretKind: SecretKindAPIKey, Plaintext: []byte("blocked"),
	}); !errors.Is(err, ErrPendingShredCleanup) {
		t.Fatalf("expected Org DEK recreation to fail closed with ErrPendingShredCleanup, got %v", err)
	}
	if _, err := failingRecoverStore.SealSecret(ctx, SealSecretParams{
		OrgID: orgB.ID, OwnerProjectID: &prjB.ID, Name: "prjb-recreate", SecretKind: SecretKindAPIKey, Plaintext: []byte("blocked"),
	}); !errors.Is(err, ErrPendingShredCleanup) {
		t.Fatalf("expected child Project DEK creation under pending Org shred to fail closed, got %v", err)
	}
	// Child project shred call while parent org WAL cleanup fails must NOT return ErrNotFound.
	if err := failingRecoverStore.ShredProjectDEK(ctx, orgB.ID, prjB.ID, ShredRequest{RequestID: "cr-child-prjb-fail", ExpectedGeneration: 1}); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("expected child ShredProjectDEK while parent org WAL pending to return ErrPostShredCleanupFailed, got %v", err)
	}
	if err := failingRecoverStore.ShredOrgDEK(ctx, orgB.ID, crOrgBReq); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("expected failing retry ShredOrgDEK to return ErrPostShredCleanupFailed, got %v", err)
	}

	// Healthy restarted store2 verifies child ShredProjectDEK returns ErrNotFound once WAL passes
	// while keeping org:orgB for ShredOrgDEK retry, and FlushPendingShredCleanups completes cleanly!
	if err := store2.ShredProjectDEK(ctx, orgB.ID, prjB.ID, ShredRequest{RequestID: "cr-child-prjb-ok", ExpectedGeneration: 1}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for child project when parent org WAL checkpoint succeeds, got %v", err)
	}
	if store2.PendingShredCleanups() != 1 {
		t.Fatalf("expected org:orgB pending entry preserved for ShredOrgDEK retry, got %d", store2.PendingShredCleanups())
	}
	if err := store2.ShredOrgDEK(ctx, orgB.ID, crOrgBReq); err != nil {
		t.Fatalf("retry ShredOrgDEK on store2: %v", err)
	}
	if store2.PendingShredCleanups() != 0 {
		t.Fatalf("expected PendingShredCleanups=0 after ShredOrgDEK recovery, got %d", store2.PendingShredCleanups())
	}
	if err := store2.ShredOrgDEK(ctx, orgB.ID, ShredRequest{RequestID: "cr-shred-orgb-dup", ExpectedGeneration: 1}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on completed duplicate ShredOrgDEK, got %v", err)
	}
	if err := store2.FlushPendingShredCleanups(ctx); err != nil {
		t.Fatalf("idempotent FlushPendingShredCleanups on empty outbox failed: %v", err)
	}
	if _, err := store2.UnsealWithCapability(ctx, capToken, orgB.ID, secOrgB.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on shredded Org B secret, got %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if rawDisk, rerr := os.ReadFile(dbPath + suffix); rerr == nil {
			if bytes.Contains(rawDisk, orgBWrapped) {
				t.Fatalf("residual deleted wrapped_org_dek persisted in %s after Org B recovery", dbPath+suffix)
			}
		}
	}
}

func TestVaultConcurrentShredRetryIdempotencyAndRecreateRace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, 5*time.Minute)

	org, err := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "conc-shred", DisplayName: "Conc"})
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	prj1, _ := env.sqlite.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "p1", DisplayName: "P1"})
	prj2, _ := env.sqlite.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "p2", DisplayName: "P2"})
	prj3, _ := env.sqlite.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "p3", DisplayName: "P3"})

	for _, pid := range []string{prj1.ID, prj2.ID, prj3.ID} {
		p := pid
		if _, err := env.vault.SealSecret(ctx, SealSecretParams{
			OrgID: org.ID, OwnerProjectID: &p, Name: "k", SecretKind: SecretKindAPIKey, Plaintext: []byte("sec-" + pid),
		}); err != nil {
			t.Fatalf("SealSecret %s: %v", pid, err)
		}
	}

	// 1. Concurrent callers on two independent *Store handles racing on flaky PostShredHook (first 3 fail).
	var attempts atomic.Int64
	flakyHook := func(ctx context.Context) error {
		if attempts.Add(1) <= 3 {
			return errors.New("transient wal busy")
		}
		return env.sqlite.CheckpointWAL(ctx)
	}
	storeA, err := NewStore(Config{DB: env.sqlite.RawDB(), KEK: env.kek, PostShredHook: flakyHook})
	if err != nil {
		t.Fatalf("storeA: %v", err)
	}
	defer storeA.Close()
	storeB, err := NewStore(Config{DB: env.sqlite.RawDB(), KEK: env.kek, PostShredHook: flakyHook})
	if err != nil {
		t.Fatalf("storeB: %v", err)
	}
	defer storeB.Close()

	const workers = 10
	errs := make([]error, workers)
	var wg sync.WaitGroup
	barrier := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-barrier
			st := storeA
			if idx%2 == 1 {
				st = storeB
			}
			errs[idx] = st.ShredProjectDEK(ctx, org.ID, prj1.ID, ShredRequest{
				RequestID:          fmt.Sprintf("conc-p1-%d", idx),
				ExpectedGeneration: 1,
			})
		}(i)
	}
	close(barrier)
	wg.Wait()

	var okCount, errFailCount, notFoundCount int
	for i, e := range errs {
		switch {
		case e == nil:
			okCount++
		case errors.Is(e, ErrPostShredCleanupFailed):
			errFailCount++
		case errors.Is(e, port.ErrNotFound):
			notFoundCount++
		default:
			t.Fatalf("unexpected concurrent shred error[%d]: %v", i, e)
		}
	}
	if okCount < 1 || errFailCount < 1 {
		t.Fatalf("expected >=1 nil and >=1 ErrPostShredCleanupFailed under transient failures, got nil=%d fail=%d notFound=%d",
			okCount, errFailCount, notFoundCount)
	}
	if got := env.vault.PendingShredCleanups(); got != 0 {
		t.Fatalf("expected 0 pending cleanups after concurrent recovery, got %d", got)
	}

	// 2. Concurrent healthy shred on prj2 -> strictly 1 succeeds, remaining 9 observe ErrNotFound.
	wg = sync.WaitGroup{}
	errs2 := make([]error, workers)
	barrier2 := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-barrier2
			errs2[idx] = env.vault.ShredProjectDEK(ctx, org.ID, prj2.ID, ShredRequest{
				RequestID:          fmt.Sprintf("conc-p2-%d", idx),
				ExpectedGeneration: 1,
			})
		}(i)
	}
	close(barrier2)
	wg.Wait()

	var s2OK, s2NF int
	for i, e := range errs2 {
		switch {
		case e == nil:
			s2OK++
		case errors.Is(e, port.ErrNotFound):
			s2NF++
		default:
			t.Fatalf("unexpected concurrent healthy shred error[%d]: %v", i, e)
		}
	}
	if s2OK != 1 || s2NF != workers-1 {
		t.Fatalf("expected exactly 1 nil and %d ErrNotFound, got ok=%d nf=%d", workers-1, s2OK, s2NF)
	}

	// 3. Pending cleanup FlushPendingShredCleanups + SealSecret recreation race on prj3:
	// First force prj3 into pending state using storeFail, then race FlushPendingShredCleanups
	// against SealSecret(org, prj3) to ensure recreation is either rejected fail-closed or happens
	// cleanly post-cleanup without being wiped by recovery.
	storeFail, _ := NewStore(Config{
		DB:            env.sqlite.RawDB(),
		KEK:           env.kek,
		PostShredHook: func(context.Context) error { return errors.New("busy") },
	})
	defer storeFail.Close()
	if err := storeFail.ShredProjectDEK(ctx, org.ID, prj3.ID, ShredRequest{RequestID: "conc-p3-fail", ExpectedGeneration: 1}); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("expected ErrPostShredCleanupFailed on prj3, got %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: org.ID, OwnerProjectID: &prj3.ID, Name: "pre-flush-blocked", SecretKind: SecretKindAPIKey, Plaintext: []byte("blocked"),
	}); !errors.Is(err, ErrPendingShredCleanup) {
		t.Fatalf("expected pre-flush SealSecret on prj3 to fail closed with ErrPendingShredCleanup, got %v", err)
	}

	var flushErr error
	var sealErr error
	var sealMeta *SecretMetadata
	var wgRace sync.WaitGroup
	startRace := make(chan struct{})
	wgRace.Add(2)
	go func() {
		defer wgRace.Done()
		<-startRace
		flushErr = env.vault.FlushPendingShredCleanups(ctx)
	}()
	go func() {
		defer wgRace.Done()
		<-startRace
		sealMeta, sealErr = env.vault.SealSecret(ctx, SealSecretParams{
			OrgID: org.ID, OwnerProjectID: &prj3.ID, Name: "race-recreate", SecretKind: SecretKindAPIKey, Plaintext: []byte("recreated"),
		})
	}()
	close(startRace)
	wgRace.Wait()

	if flushErr != nil {
		t.Fatalf("FlushPendingShredCleanups failed unexpectedly: %v", flushErr)
	}
	if env.vault.PendingShredCleanups() != 0 {
		t.Fatalf("expected 0 pending cleanups after flush, got %d", env.vault.PendingShredCleanups())
	}
	if sealErr == nil {
		// If SealSecret ran after flush cleared pending state, the newly sealed secret MUST be intact!
		u, uerr := env.vault.UnsealWithCapability(ctx, env.cap, org.ID, sealMeta.ID)
		if uerr != nil || string(u.Plaintext) != "recreated" {
			t.Fatalf("post-cleanup recreated secret must be unsealable, err=%v", uerr)
		}
		u.Zeroize()
		// Even if prj3 was recreated at generation 2, retrying old RequestID "conc-p3-fail"@gen=1 fails fenced!
		if err := env.vault.ShredProjectDEK(ctx, org.ID, prj3.ID, ShredRequest{RequestID: "conc-p3-fail", ExpectedGeneration: 1}); !errors.Is(err, ErrStaleShredGeneration) {
			t.Fatalf("expected ErrStaleShredGeneration retrying old RequestID after flush+recreate, got %v", err)
		}
	} else if !errors.Is(sealErr, ErrPendingShredCleanup) {
		t.Fatalf("unexpected concurrent SealSecret error during pending flush: %v", sealErr)
	}

	// 4. Context cancellation (both fast-path & contended shred slot) and closed-DB error branches.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := env.vault.ShredProjectDEK(cctx, org.ID, prj3.ID, ShredRequest{RequestID: "cancel-p3", ExpectedGeneration: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on canceled ShredProjectDEK, got %v", err)
	}
	if err := env.vault.ShredOrgDEK(cctx, org.ID, ShredRequest{RequestID: "cancel-org", ExpectedGeneration: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on canceled ShredOrgDEK, got %v", err)
	}
	if err := env.vault.FlushPendingShredCleanups(cctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on canceled FlushPendingShredCleanups, got %v", err)
	}
	// Contended shredSem select cancellation path:
	env.vault.shredSem <- struct{}{}
	wctx, wcancel := context.WithTimeout(ctx, 10*time.Millisecond)
	if err := env.vault.FlushPendingShredCleanups(wctx); !errors.Is(err, context.DeadlineExceeded) {
		wcancel()
		<-env.vault.shredSem
		t.Fatalf("expected context.DeadlineExceeded waiting on held shredSem, got %v", err)
	}
	wcancel()
	<-env.vault.shredSem

	// Post-PostShredHook DB clear failure path:
	prj4, _ := env.sqlite.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "p4", DisplayName: "P4"})
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: org.ID, OwnerProjectID: &prj4.ID, Name: "p4", SecretKind: SecretKindAPIKey, Plaintext: []byte("p4"),
	}); err != nil {
		t.Fatalf("SealSecret p4: %v", err)
	}
	closeOnShredStore, _ := NewStore(Config{
		DB:  env.sqlite.RawDB(),
		KEK: env.kek,
		PostShredHook: func(ctx context.Context) error {
			return env.sqlite.Close()
		},
	})
	if err := closeOnShredStore.ShredProjectDEK(ctx, org.ID, prj4.ID, ShredRequest{RequestID: "close-p4", ExpectedGeneration: 1}); err == nil {
		t.Fatalf("expected error clearing pending cleanup after DB closed during PostShredHook")
	}

	_ = env.sqlite.Close()
	if got := env.vault.PendingShredCleanups(); got != -1 {
		t.Fatalf("expected PendingShredCleanups()=-1 when DB is closed, got %d", got)
	}
	if err := env.vault.FlushPendingShredCleanups(ctx); err == nil {
		t.Fatalf("expected error on FlushPendingShredCleanups after DB close")
	}
}

func TestVaultStaleShredRetryFencingAfterRecreateProjectAndOrg(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fencing", "tanpopo.db")
	fc := clock.NewFakeClock(time.Date(2026, 11, 14, 21, 30, 0, 0, time.UTC))

	st1, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: dbPath,
		NowMs:  func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open st1: %v", err)
	}
	kek, err := NewStaticKEKProvider("kek_v1", random32Key(t))
	if err != nil {
		_ = st1.Close()
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	defer kek.Close()

	store1, err := NewStore(Config{
		DB:            st1.RawDB(),
		KEK:           kek,
		CacheTTL:      5 * time.Minute,
		NowMs:         func() int64 { return fc.Now().UnixMilli() },
		PostShredHook: st1.CheckpointWAL,
	})
	if err != nil {
		_ = st1.Close()
		t.Fatalf("NewStore store1: %v", err)
	}
	capToken := MintUnsealCapabilityForBroker()

	// === PART A: Project DEK full lifecycle (shred gen 1 -> cleanup -> recreate gen 2 -> late retry gen 1 -> valid shred gen 2) ===
	orgA, _ := st1.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fence-orga", DisplayName: "Fence A"})
	prj1, _ := st1.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "fence-p1", DisplayName: "Fence P1"})
	secP1Gen1, err := store1.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &prj1.ID, Name: "p1-gen1", SecretKind: SecretKindAPIKey, Plaintext: []byte("p1-gen1-secret"),
	})
	if err != nil {
		t.Fatalf("Seal P1 gen1: %v", err)
	}
	projMeta1, err := store1.GetProjectKeyMetadata(ctx, orgA.ID, prj1.ID)
	if err != nil || projMeta1.KeyGeneration != 1 || projMeta1.DEKVersion != 1 {
		t.Fatalf("expected Project gen=1, got %+v err=%v", projMeta1, err)
	}

	// Warm a second independent peerWarmStore with Gen 1 DEK before shredding on store1
	// so we also verify key_generation cache invalidation across stores when Gen 2 is recreated.
	peerWarmStore, _ := NewStore(Config{DB: st1.RawDB(), KEK: kek, CacheTTL: 5 * time.Minute, PostShredHook: st1.CheckpointWAL})
	defer peerWarmStore.Close()
	if uWarm, werr := peerWarmStore.UnsealWithCapability(ctx, capToken, orgA.ID, secP1Gen1.ID); werr != nil {
		t.Fatalf("warm peerWarmStore on gen1: %v", werr)
	} else {
		uWarm.Zeroize()
	}

	// Wrong generation MUST be rejected fail-closed while gen=1 is active.
	if err := store1.ShredProjectDEK(ctx, orgA.ID, prj1.ID, ShredRequest{RequestID: "wrong-proj-gen", ExpectedGeneration: 2}); !errors.Is(err, ErrStaleShredGeneration) || !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("expected ErrStaleShredGeneration/ErrVersionConflict for wrong expected gen=2, got %v", err)
	}

	// Shred gen 1 and verify WAL cleanup completes.
	projReq1 := ShredRequest{RequestID: "shred-p1-req-1", ExpectedGeneration: projMeta1.KeyGeneration}
	if err := store1.ShredProjectDEK(ctx, orgA.ID, prj1.ID, projReq1); err != nil {
		t.Fatalf("ShredProjectDEK gen1: %v", err)
	}
	if store1.PendingShredCleanups() != 0 {
		t.Fatalf("expected 0 pending cleanups after gen1 shred, got %d", store1.PendingShredCleanups())
	}
	// Reusing projReq1's RequestID on wrong scope/generation MUST be rejected with ErrInvalidInput.
	if err := store1.ShredProjectDEK(ctx, orgA.ID, prj1.ID, ShredRequest{RequestID: projReq1.RequestID, ExpectedGeneration: 2}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput reusing RequestID with mismatched generation, got %v", err)
	}

	// Recreate Project P1 DEK -> key_generation increments to 2 (even while dek_version resets to 1).
	secP1Gen2, err := store1.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &prj1.ID, Name: "p1-gen2", SecretKind: SecretKindAPIKey, Plaintext: []byte("p1-gen2-secret"),
	})
	if err != nil {
		t.Fatalf("Seal P1 gen2: %v", err)
	}
	projMeta2, err := store1.GetProjectKeyMetadata(ctx, orgA.ID, prj1.ID)
	if err != nil || projMeta2.KeyGeneration != 2 || projMeta2.DEKVersion != 1 {
		t.Fatalf("expected recreated Project KeyGeneration=2 DEKVersion=1, got %+v err=%v", projMeta2, err)
	}
	// peerWarmStore (which still holds Gen 1 in RAM cache) MUST detect key_generation=2 != 1 and overwrite cache cleanly.
	if uGen2Warm, werr := peerWarmStore.UnsealWithCapability(ctx, capToken, orgA.ID, secP1Gen2.ID); werr != nil || string(uGen2Warm.Plaintext) != "p1-gen2-secret" {
		t.Fatalf("peerWarmStore must detect key_generation=2 change and unseal gen2 cleanly, err=%v", werr)
	} else {
		uGen2Warm.Zeroize()
	}
	if _, serr := peerWarmStore.SealSecret(ctx, SealSecretParams{
		OrgID: orgA.ID, OwnerProjectID: &prj1.ID, Name: "p1-gen2-peer-seal", SecretKind: SecretKindAPIKey, Plaintext: []byte("peer-g2"),
	}); serr != nil {
		t.Fatalf("peerWarmStore SealSecret on recreated gen2 failed: %v", serr)
	}

	// Late retry of old RequestID projReq1 AND fresh RequestID targeting old gen 1 across Store 1 and a second independent Store!
	if err := store1.ShredProjectDEK(ctx, orgA.ID, prj1.ID, projReq1); !errors.Is(err, ErrStaleShredGeneration) || !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("stale replay of old Project RequestID must fail with ErrStaleShredGeneration, got %v", err)
	}
	if err := store1.ShredProjectDEK(ctx, orgA.ID, prj1.ID, ShredRequest{RequestID: "shred-p1-stale-gen1", ExpectedGeneration: 1}); !errors.Is(err, ErrStaleShredGeneration) {
		t.Fatalf("stale new RequestID with ExpectedGeneration=1 must fail with ErrStaleShredGeneration, got %v", err)
	}

	// Verify BackupTo + Restore preserves generation fencing (restored snapshot rejects old RequestID/gen=1 too).
	backupPath := filepath.Join(dir, "fencing-gen2.db")
	if err := st1.BackupTo(ctx, backupPath); err != nil {
		t.Fatalf("BackupTo gen2: %v", err)
	}
	stBak, err := sqlite.Open(ctx, sqlite.Config{DBPath: backupPath, NowMs: func() int64 { return fc.Now().UnixMilli() }})
	if err != nil {
		t.Fatalf("open restored gen2 backup: %v", err)
	}
	bakVault, _ := NewStore(Config{DB: stBak.RawDB(), KEK: kek, PostShredHook: stBak.CheckpointWAL})
	if err := bakVault.ShredProjectDEK(ctx, orgA.ID, prj1.ID, projReq1); !errors.Is(err, ErrStaleShredGeneration) {
		_ = stBak.Close()
		t.Fatalf("restored backup must reject stale Project shred retry, got %v", err)
	}
	if u, uerr := bakVault.UnsealWithCapability(ctx, capToken, orgA.ID, secP1Gen2.ID); uerr != nil || string(u.Plaintext) != "p1-gen2-secret" {
		_ = stBak.Close()
		t.Fatalf("restored gen2 secret must unseal after stale retry rejection, err=%v", uerr)
	} else {
		u.Zeroize()
	}
	bakVault.Close()
	_ = stBak.Close()

	// Concurrent multi-store stale retries racing against valid unsealing must NEVER touch Gen 2!
	peerStore, _ := NewStore(Config{DB: st1.RawDB(), KEK: kek, CacheTTL: 5 * time.Minute, PostShredHook: st1.CheckpointWAL})
	var concWG sync.WaitGroup
	for i := 0; i < 6; i++ {
		concWG.Add(1)
		go func(idx int) {
			defer concWG.Done()
			st := store1
			if idx%2 == 1 {
				st = peerStore
			}
			_ = st.ShredProjectDEK(ctx, orgA.ID, prj1.ID, ShredRequest{
				RequestID:          fmt.Sprintf("concurrent-stale-p1-%d", idx),
				ExpectedGeneration: 1,
			})
			_ = st.ShredProjectDEK(ctx, orgA.ID, prj1.ID, projReq1)
		}(i)
	}
	concWG.Wait()
	peerStore.Close()

	// Gen 2 secret MUST STILL be intact; Gen 1 secret MUST remain permanently unrecoverable!
	if u2, uerr := store1.UnsealWithCapability(ctx, capToken, orgA.ID, secP1Gen2.ID); uerr != nil || string(u2.Plaintext) != "p1-gen2-secret" {
		t.Fatalf("recreated P1 gen2 secret was wrongly affected by stale retries, err=%v", uerr)
	} else {
		u2.Zeroize()
	}
	if _, uerr := store1.UnsealWithCapability(ctx, capToken, orgA.ID, secP1Gen1.ID); !errors.Is(uerr, port.ErrNotFound) {
		t.Fatalf("old P1 gen1 secret must remain ErrNotFound, got %v", uerr)
	}

	// Simulate process restart before executing valid Gen 2 shred AND re-testing stale Gen 1 after restart!
	store1.Close()
	_ = st1.Close()

	st2, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath, NowMs: func() int64 { return fc.Now().UnixMilli() }})
	if err != nil {
		t.Fatalf("sqlite.Open st2: %v", err)
	}
	defer st2.Close()
	store2, _ := NewStore(Config{DB: st2.RawDB(), KEK: kek, CacheTTL: 5 * time.Minute, PostShredHook: st2.CheckpointWAL})
	defer store2.Close()

	if err := store2.ShredProjectDEK(ctx, orgA.ID, prj1.ID, projReq1); !errors.Is(err, ErrStaleShredGeneration) {
		t.Fatalf("restarted store2 must still reject stale P1 req1, got %v", err)
	}
	// Fresh, valid shred targeting current Gen 2 SUCCEEDS!
	projReq2 := ShredRequest{RequestID: "shred-p1-req-2", ExpectedGeneration: 2}
	if err := store2.ShredProjectDEK(ctx, orgA.ID, prj1.ID, projReq2); err != nil {
		t.Fatalf("valid ShredProjectDEK gen2 failed: %v", err)
	}
	if _, uerr := store2.UnsealWithCapability(ctx, capToken, orgA.ID, secP1Gen2.ID); !errors.Is(uerr, port.ErrNotFound) {
		t.Fatalf("expected P1 gen2 unrecoverable after valid gen2 shred, got %v", uerr)
	}
	// Even after Gen 2 is shredded, replaying req1 (gen1) remains rejected as superseded.
	if err := store2.ShredProjectDEK(ctx, orgA.ID, prj1.ID, projReq1); !errors.Is(err, ErrStaleShredGeneration) {
		t.Fatalf("expected ErrStaleShredGeneration for superseded req1 after gen2 shred, got %v", err)
	}
	if err := store2.ShredProjectDEK(ctx, orgA.ID, prj1.ID, ShredRequest{RequestID: "shred-p1-stale-after-g2", ExpectedGeneration: 1}); !errors.Is(err, ErrStaleShredGeneration) {
		t.Fatalf("expected ErrStaleShredGeneration for gen1 after gen2 superseded, got %v", err)
	}

	// === PART B: Org DEK full lifecycle (shred gen 1 -> cleanup -> recreate gen 2 -> late retry gen 1 -> valid shred gen 2) ===
	orgB, _ := st2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fence-orgb", DisplayName: "Fence B"})
	prjB, _ := st2.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "pb", DisplayName: "PB"})
	secOrgBGen1, err := store2.SealSecret(ctx, SealSecretParams{
		OrgID: orgB.ID, Name: "orgb-g1", SecretKind: SecretKindAPIKey, Plaintext: []byte("orgb-g1-val"),
	})
	if err != nil {
		t.Fatalf("Seal OrgB gen1: %v", err)
	}
	orgMeta1, err := store2.GetOrgKeyMetadata(ctx, orgB.ID)
	if err != nil || orgMeta1.KeyGeneration != 1 {
		t.Fatalf("expected OrgB gen=1, got %+v err=%v", orgMeta1, err)
	}
	peerOrgWarmStore, _ := NewStore(Config{DB: st2.RawDB(), KEK: kek, CacheTTL: 5 * time.Minute, PostShredHook: st2.CheckpointWAL})
	defer peerOrgWarmStore.Close()
	if uOrgWarm, oerr := peerOrgWarmStore.UnsealWithCapability(ctx, capToken, orgB.ID, secOrgBGen1.ID); oerr != nil {
		t.Fatalf("warm peerOrgWarmStore gen1: %v", oerr)
	} else {
		uOrgWarm.Zeroize()
	}

	// Wrong generation fails closed without deleting secret_credentials or org_encryption_keys.
	if err := store2.ShredOrgDEK(ctx, orgB.ID, ShredRequest{RequestID: "wrong-orgb-gen", ExpectedGeneration: 2}); !errors.Is(err, ErrStaleShredGeneration) {
		t.Fatalf("expected ErrStaleShredGeneration on wrong Org expected gen, got %v", err)
	}

	orgReq1 := ShredRequest{RequestID: "shred-orgb-req-1", ExpectedGeneration: 1}
	if err := store2.ShredOrgDEK(ctx, orgB.ID, orgReq1); err != nil {
		t.Fatalf("ShredOrgDEK gen1: %v", err)
	}
	// Idempotent replay BEFORE recreate returns nil; reusing orgReq1 with mismatched gen fails.
	if err := store2.ShredOrgDEK(ctx, orgB.ID, orgReq1); err != nil {
		t.Fatalf("expected idempotent nil on completed Org shred before recreate, got %v", err)
	}
	if err := store2.ShredOrgDEK(ctx, orgB.ID, ShredRequest{RequestID: orgReq1.RequestID, ExpectedGeneration: 2}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput reusing Org RequestID with mismatched gen, got %v", err)
	}

	// Recreate Org B DEK + project secret -> Org KeyGeneration becomes 2.
	secOrgBGen2, err := store2.SealSecret(ctx, SealSecretParams{
		OrgID: orgB.ID, Name: "orgb-g2", SecretKind: SecretKindAPIKey, Plaintext: []byte("orgb-g2-val"),
	})
	if err != nil {
		t.Fatalf("Seal OrgB gen2: %v", err)
	}
	secProjBGen2, err := store2.SealSecret(ctx, SealSecretParams{
		OrgID: orgB.ID, OwnerProjectID: &prjB.ID, Name: "prjb-g2", SecretKind: SecretKindAPIKey, Plaintext: []byte("prjb-g2-val"),
	})
	if err != nil {
		t.Fatalf("Seal PrjB gen2: %v", err)
	}
	orgMeta2, err := store2.GetOrgKeyMetadata(ctx, orgB.ID)
	if err != nil || orgMeta2.KeyGeneration != 2 {
		t.Fatalf("expected recreated OrgB KeyGeneration=2, got %+v err=%v", orgMeta2, err)
	}
	if uOrg2Warm, oerr := peerOrgWarmStore.UnsealWithCapability(ctx, capToken, orgB.ID, secOrgBGen2.ID); oerr != nil || string(uOrg2Warm.Plaintext) != "orgb-g2-val" {
		t.Fatalf("peerOrgWarmStore must refresh on OrgB gen2, err=%v", oerr)
	} else {
		uOrg2Warm.Zeroize()
	}
	if _, serr := peerOrgWarmStore.SealSecret(ctx, SealSecretParams{
		OrgID: orgB.ID, Name: "orgb-peer-seal", SecretKind: SecretKindAPIKey, Plaintext: []byte("orgb-peer-g2"),
	}); serr != nil {
		t.Fatalf("peerOrgWarmStore SealSecret gen2 failed: %v", serr)
	}

	// Late retry of orgReq1 OR stale ExpectedGeneration=1 MUST reject and MUST NOT wipe secOrgBGen2 / secProjBGen2!
	if err := store2.ShredOrgDEK(ctx, orgB.ID, orgReq1); !errors.Is(err, ErrStaleShredGeneration) || !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("stale Org replay req1 must fail with ErrStaleShredGeneration, got %v", err)
	}
	if err := store2.ShredOrgDEK(ctx, orgB.ID, ShredRequest{RequestID: "shred-orgb-stale-gen1", ExpectedGeneration: 1}); !errors.Is(err, ErrStaleShredGeneration) {
		t.Fatalf("stale Org request gen=1 must fail with ErrStaleShredGeneration, got %v", err)
	}
	if u, uerr := store2.UnsealWithCapability(ctx, capToken, orgB.ID, secOrgBGen2.ID); uerr != nil || string(u.Plaintext) != "orgb-g2-val" {
		t.Fatalf("OrgB gen2 secret must stay intact after stale ShredOrgDEK retry, err=%v", uerr)
	} else {
		u.Zeroize()
	}
	if u, uerr := store2.UnsealWithCapability(ctx, capToken, orgB.ID, secProjBGen2.ID); uerr != nil || string(u.Plaintext) != "prjb-g2-val" {
		t.Fatalf("PrjB gen2 secret must stay intact after stale ShredOrgDEK retry, err=%v", uerr)
	} else {
		u.Zeroize()
	}
	if _, uerr := store2.UnsealWithCapability(ctx, capToken, orgB.ID, secOrgBGen1.ID); !errors.Is(uerr, port.ErrNotFound) {
		t.Fatalf("old OrgB gen1 secret must remain ErrNotFound, got %v", uerr)
	}

	// Pending-cleanup generation fence check for both Project and Org:
	orgC, _ := st2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fence-orgc", DisplayName: "Fence C"})
	prjC, _ := st2.Tenancy().CreateProject(ctx, orgC.ID, port.CreateProjectParams{Slug: "pc", DisplayName: "PC"})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgC.ID, OwnerProjectID: &prjC.ID, Name: "pc-g1", SecretKind: SecretKindAPIKey, Plaintext: []byte("pc1")})
	_ = store2.ShredProjectDEK(ctx, orgC.ID, prjC.ID, ShredRequest{RequestID: "pc-shred-g1", ExpectedGeneration: 1})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgC.ID, OwnerProjectID: &prjC.ID, Name: "pc-g2", SecretKind: SecretKindAPIKey, Plaintext: []byte("pc2")})
	failStore, _ := NewStore(Config{DB: st2.RawDB(), KEK: kek, PostShredHook: func(context.Context) error { return errors.New("busy") }})
	if err := failStore.ShredProjectDEK(ctx, orgC.ID, prjC.ID, ShredRequest{RequestID: "pc-shred-g2", ExpectedGeneration: 2}); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("expected ErrPostShredCleanupFailed on PrjC gen2 shred, got %v", err)
	}
	if err := failStore.ShredProjectDEK(ctx, orgC.ID, prjC.ID, ShredRequest{RequestID: "pc-stale-while-pending", ExpectedGeneration: 1}); !errors.Is(err, ErrStaleShredGeneration) {
		t.Fatalf("expected ErrStaleShredGeneration on stale proj gen=1 while gen=2 pending, got %v", err)
	}
	if err := store2.FlushPendingShredCleanups(ctx); err != nil {
		t.Fatalf("FlushPendingShredCleanups PrjC: %v", err)
	}
	_ = store2.ShredOrgDEK(ctx, orgC.ID, ShredRequest{RequestID: "orgc-shred-g1", ExpectedGeneration: 1})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgC.ID, Name: "orgc-g2", SecretKind: SecretKindAPIKey, Plaintext: []byte("c2")})
	if err := failStore.ShredOrgDEK(ctx, orgC.ID, ShredRequest{RequestID: "orgc-shred-g2", ExpectedGeneration: 2}); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("expected ErrPostShredCleanupFailed on OrgC gen2 shred, got %v", err)
	}
	if err := failStore.ShredOrgDEK(ctx, orgC.ID, ShredRequest{RequestID: "orgc-stale-while-pending", ExpectedGeneration: 1}); !errors.Is(err, ErrStaleShredGeneration) {
		t.Fatalf("expected ErrStaleShredGeneration on stale gen=1 while gen=2 pending, got %v", err)
	}
	failStore.Close()
	if err := store2.FlushPendingShredCleanups(ctx); err != nil {
		t.Fatalf("FlushPendingShredCleanups OrgC: %v", err)
	}

	// Valid new Org B shred targeting Gen 2 SUCCEEDS!
	if err := store2.ShredOrgDEK(ctx, orgB.ID, ShredRequest{RequestID: "shred-orgb-req-2", ExpectedGeneration: 2}); err != nil {
		t.Fatalf("valid ShredOrgDEK gen2 failed: %v", err)
	}
	if _, err := store2.GetOrgKeyMetadata(ctx, orgB.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on GetOrgKeyMetadata after OrgB shred, got %v", err)
	}
	if _, err := store2.GetProjectKeyMetadata(ctx, orgB.ID, prjB.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on GetProjectKeyMetadata after OrgB shred, got %v", err)
	}
	if _, uerr := store2.UnsealWithCapability(ctx, capToken, orgB.ID, secOrgBGen2.ID); !errors.Is(uerr, port.ErrNotFound) {
		t.Fatalf("expected OrgB gen2 secret erased after valid gen2 shred, got %v", uerr)
	}
	if err := store2.ShredOrgDEK(ctx, orgB.ID, orgReq1); !errors.Is(err, ErrStaleShredGeneration) {
		t.Fatalf("expected ErrStaleShredGeneration on superseded orgReq1 after gen2 shred, got %v", err)
	}
	// Verify completed RequestID replay on OrgC (gen=2, not recreated again) returns idempotent nil,
	// and FlushPendingShredCleanups returns error if backing DB closes during PostShredHook.
	if err := store2.ShredOrgDEK(ctx, orgC.ID, ShredRequest{RequestID: "orgc-shred-g2", ExpectedGeneration: 2}); err != nil {
		t.Fatalf("expected idempotent nil replaying completed OrgC gen=2 request, got %v", err)
	}
	if err := store2.ShredOrgDEK(ctx, orgC.ID, ShredRequest{RequestID: "orgc-shred-g2", ExpectedGeneration: 3}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput reusing OrgC RequestID with gen=3, got %v", err)
	}
	// Test ShredOrgDEK when org_encryption_keys is absent while a child project cleanup is still pending:
	orgE, _ := st2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fence-orge", DisplayName: "Fence E"})
	prjE, _ := st2.Tenancy().CreateProject(ctx, orgE.ID, port.CreateProjectParams{Slug: "pe", DisplayName: "PE"})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgE.ID, OwnerProjectID: &prjE.ID, Name: "pe-g1", SecretKind: SecretKindAPIKey, Plaintext: []byte("pe1")})
	failStoreE, _ := NewStore(Config{DB: st2.RawDB(), KEK: kek, PostShredHook: func(context.Context) error { return errors.New("busy") }})
	_ = failStoreE.ShredProjectDEK(ctx, orgE.ID, prjE.ID, ShredRequest{RequestID: "pe-shred-fail", ExpectedGeneration: 1})
	_, _ = st2.RawDB().ExecContext(ctx, `DELETE FROM org_encryption_keys WHERE org_id = ?`, orgE.ID)
	if err := failStoreE.ShredOrgDEK(ctx, orgE.ID, ShredRequest{RequestID: "orge-child-pending-fail", ExpectedGeneration: 1}); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("expected ErrPostShredCleanupFailed when child cleanup pending under ShredOrgDEK, got %v", err)
	}
	failStoreE.Close()
	if err := store2.ShredOrgDEK(ctx, orgE.ID, ShredRequest{RequestID: "orge-child-pending-ok", ExpectedGeneration: 1}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after child-pending ShredOrgDEK WAL succeeds, got %v", err)
	}
	_ = store2.FlushPendingShredCleanups(ctx)

	// 1. Cross-tenant RequestID isolation: Org F and Org G can independently reuse identical RequestID ("shared-tenant-req-1").
	orgF, _ := st2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fence-orgf", DisplayName: "Fence F"})
	orgG, _ := st2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fence-orgg", DisplayName: "Fence G"})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgF.ID, Name: "f-sec", SecretKind: SecretKindAPIKey, Plaintext: []byte("f1")})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgG.ID, Name: "g-sec", SecretKind: SecretKindAPIKey, Plaintext: []byte("g1")})
	if err := store2.ShredOrgDEK(ctx, orgF.ID, ShredRequest{RequestID: "shared-tenant-req-1", ExpectedGeneration: 1}); err != nil {
		t.Fatalf("OrgF shred failed: %v", err)
	}
	if err := store2.ShredOrgDEK(ctx, orgG.ID, ShredRequest{RequestID: "shared-tenant-req-1", ExpectedGeneration: 1}); err != nil {
		t.Fatalf("OrgG reusing identical RequestID across tenant boundary must succeed, got %v", err)
	}

	// 2. Multi-store ShredOrgDEK completion race:
	// storePause (gen=1) pauses inside PostShredHook while storeFast completes flush, recreates gen=2,
	// and starts failing gen=2 shred (PENDING_CLEANUP). When storePause resumes, it MUST NOT mark gen=2 COMPLETED!
	orgR, _ := st2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fence-race-org", DisplayName: "Fence Race"})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgR.ID, Name: "r1", SecretKind: SecretKindAPIKey, Plaintext: []byte("r1")})
	hookEntered := make(chan struct{})
	releaseHook := make(chan struct{})
	storePause, _ := NewStore(Config{
		DB:  st2.RawDB(),
		KEK: kek,
		PostShredHook: func(ctx context.Context) error {
			close(hookEntered)
			<-releaseHook
			return st2.CheckpointWAL(ctx)
		},
	})
	defer storePause.Close()

	var pauseErr error
	var pauseWG sync.WaitGroup
	pauseWG.Add(1)
	go func() {
		defer pauseWG.Done()
		pauseErr = storePause.ShredOrgDEK(ctx, orgR.ID, ShredRequest{RequestID: "race-org-g1", ExpectedGeneration: 1})
	}()
	<-hookEntered

	// While storePause is mid-PostShredHook on gen=1, flush outbox cleanly, recreate gen=2, and fail gen=2 cleanup!
	if err := store2.FlushPendingShredCleanups(ctx); err != nil {
		t.Fatalf("store2 flush mid-pause: %v", err)
	}
	if _, err := store2.SealSecret(ctx, SealSecretParams{OrgID: orgR.ID, Name: "r2", SecretKind: SecretKindAPIKey, Plaintext: []byte("r2")}); err != nil {
		t.Fatalf("store2 recreate gen=2: %v", err)
	}
	failGen2Store, _ := NewStore(Config{DB: st2.RawDB(), KEK: kek, PostShredHook: func(context.Context) error { return errors.New("busy-gen2") }})
	gen2Req := ShredRequest{RequestID: "race-org-g2", ExpectedGeneration: 2}
	if err := failGen2Store.ShredOrgDEK(ctx, orgR.ID, gen2Req); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("expected ErrPostShredCleanupFailed on failing gen=2 shred, got %v", err)
	}
	failGen2Store.Close()

	// Resume storePause -> gen=1 cleanup finishes WITHOUT overwriting gen=2's PENDING_CLEANUP status!
	close(releaseHook)
	pauseWG.Wait()
	if pauseErr != nil {
		t.Fatalf("storePause gen=1 unexpected error: %v", pauseErr)
	}
	var gen2Status string
	if err := st2.RawDB().QueryRowContext(ctx,
		`SELECT status FROM vault_shred_requests WHERE org_id = ? AND request_id = ?`,
		orgR.ID, gen2Req.RequestID,
	).Scan(&gen2Status); err != nil || gen2Status != shredStatusPendingCleanup {
		t.Fatalf("expected gen=2 request status to stay %s after storeA gen=1 completed, got %q (err=%v)", shredStatusPendingCleanup, gen2Status, err)
	}
	// Retrying gen=2 on a still-failing store MUST still fail closed (NOT silently return nil)!
	failGen2Retry, _ := NewStore(Config{DB: st2.RawDB(), KEK: kek, PostShredHook: func(context.Context) error { return errors.New("still-busy") }})
	if err := failGen2Retry.ShredOrgDEK(ctx, orgR.ID, gen2Req); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("retried gen=2 must still fail closed when WAL hook fails, got %v", err)
	}
	failGen2Retry.Close()
	if err := store2.ShredOrgDEK(ctx, orgR.ID, gen2Req); err != nil {
		t.Fatalf("healthy retry of gen=2 failed: %v", err)
	}

	// 3. Stale ShredProjectDEK(gen=1) while parent orgKey is pending after project had gen=2 -> ErrStaleShredGeneration.
	orgH, _ := st2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fence-orgh", DisplayName: "Fence H"})
	prjH, _ := st2.Tenancy().CreateProject(ctx, orgH.ID, port.CreateProjectParams{Slug: "ph", DisplayName: "PH"})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgH.ID, OwnerProjectID: &prjH.ID, Name: "ph1", SecretKind: SecretKindAPIKey, Plaintext: []byte("ph1")})
	_ = store2.ShredProjectDEK(ctx, orgH.ID, prjH.ID, ShredRequest{RequestID: "ph-g1", ExpectedGeneration: 1})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgH.ID, OwnerProjectID: &prjH.ID, Name: "ph2", SecretKind: SecretKindAPIKey, Plaintext: []byte("ph2")})
	failStoreH, _ := NewStore(Config{DB: st2.RawDB(), KEK: kek, PostShredHook: func(context.Context) error { return errors.New("busy") }})
	_ = failStoreH.ShredOrgDEK(ctx, orgH.ID, ShredRequest{RequestID: "orgh-g1", ExpectedGeneration: 1})
	if err := failStoreH.ShredProjectDEK(ctx, orgH.ID, prjH.ID, ShredRequest{RequestID: "ph-stale-under-org", ExpectedGeneration: 1}); !errors.Is(err, ErrStaleShredGeneration) {
		t.Fatalf("expected ErrStaleShredGeneration for superseded proj gen=1 under pending org shred, got %v", err)
	}
	failStoreH.Close()
	_ = store2.FlushPendingShredCleanups(ctx)

	// 4. Verify inSavepointTx rollback prevents orphan vault_key_generations increments,
	// and hasPendingCleanupTx prevents premature nil return when a request row says COMPLETED while pending cleanup remains.
	spTx, _ := st2.RawDB().BeginTx(ctx, nil)
	_, spErr := inSavepointTx(ctx, spTx, "sp_test_rb", func() (bool, error) {
		_, _ = nextKeyGenerationTx(ctx, spTx, "org:sp_never", "sp_never", nil, 1000)
		return false, nil
	})
	_ = spTx.Commit()
	if spErr != nil {
		t.Fatalf("inSavepointTx rollback returned unexpected err: %v", spErr)
	}
	if _, spClosedErr := inSavepointTx(ctx, spTx, "sp_closed", func() (bool, error) { return true, nil }); spClosedErr == nil {
		t.Fatalf("expected inSavepointTx on committed tx to return error")
	}
	var spGen int64
	if qerr := st2.RawDB().QueryRowContext(ctx, `SELECT last_generation FROM vault_key_generations WHERE scope_key = 'org:sp_never'`).Scan(&spGen); !errors.Is(qerr, sql.ErrNoRows) {
		t.Fatalf("expected savepoint rollback to discard sp_never generation, got gen=%d err=%v", spGen, qerr)
	}
	sentinelCleanupErr := errors.New("cleanup tx abort")
	if err := store2.inPostShredCleanupTx(ctx, func(tx *sql.Tx) error { return sentinelCleanupErr }); !errors.Is(err, sentinelCleanupErr) {
		t.Fatalf("expected inPostShredCleanupTx rollback error, got %v", err)
	}
	if _, err := queryPendingCleanupTuples(ctx, st2.RawDB(), `SELECT bad_col FROM nonexistent_table`); err == nil {
		t.Fatalf("expected queryPendingCleanupTuples error on bad query")
	}
	badExecTx, _ := st2.RawDB().BeginTx(ctx, nil)
	_ = badExecTx.Rollback()
	if err := clearPendingTuplesTx(ctx, badExecTx, []pendingCleanupTuple{{scopeKey: "x", orgID: "o", epoch: 1, targetGeneration: 1}}, 1); err == nil {
		t.Fatalf("expected clearPendingTuplesTx error on rolled-back tx")
	}

	orgK, _ := st2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fence-orgk", DisplayName: "Fence K"})
	prjK, _ := st2.Tenancy().CreateProject(ctx, orgK.ID, port.CreateProjectParams{Slug: "pk", DisplayName: "PK"})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgK.ID, OwnerProjectID: &prjK.ID, Name: "pk1", SecretKind: SecretKindAPIKey, Plaintext: []byte("pk1")})
	failStoreK, _ := NewStore(Config{DB: st2.RawDB(), KEK: kek, PostShredHook: func(context.Context) error { return errors.New("busy") }})
	pkReq := ShredRequest{RequestID: "pk-half-done", ExpectedGeneration: 1}
	_ = failStoreK.ShredProjectDEK(ctx, orgK.ID, prjK.ID, pkReq)
	_, _ = st2.RawDB().ExecContext(ctx, `UPDATE vault_shred_requests SET status = 'COMPLETED' WHERE org_id = ? AND request_id = ?`, orgK.ID, pkReq.RequestID)
	if err := failStoreK.ShredProjectDEK(ctx, orgK.ID, prjK.ID, pkReq); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("expected ErrPostShredCleanupFailed even if request row was COMPLETED while pending cleanup existed, got %v", err)
	}
	failStoreK.Close()
	if err := store2.ShredProjectDEK(ctx, orgK.ID, prjK.ID, pkReq); err != nil {
		t.Fatalf("expected store2 to finish lingering pending cleanup on COMPLETED-marked request, got %v", err)
	}
	// Same guard for ShredOrgDEK:
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgK.ID, Name: "orgk1", SecretKind: SecretKindAPIKey, Plaintext: []byte("ok1")})
	failStoreKOrg, _ := NewStore(Config{DB: st2.RawDB(), KEK: kek, PostShredHook: func(context.Context) error { return errors.New("busy") }})
	orgKReq := ShredRequest{RequestID: "orgk-half-done", ExpectedGeneration: 1}
	_ = failStoreKOrg.ShredOrgDEK(ctx, orgK.ID, orgKReq)
	_, _ = st2.RawDB().ExecContext(ctx, `UPDATE vault_shred_requests SET status = 'COMPLETED' WHERE org_id = ? AND request_id = ?`, orgK.ID, orgKReq.RequestID)
	if err := failStoreKOrg.ShredOrgDEK(ctx, orgK.ID, orgKReq); !errors.Is(err, ErrPostShredCleanupFailed) {
		t.Fatalf("expected ShredOrgDEK ErrPostShredCleanupFailed while pending cleanup existed, got %v", err)
	}
	failStoreKOrg.Close()
	if err := store2.ShredOrgDEK(ctx, orgK.ID, orgKReq); err != nil {
		t.Fatalf("expected store2 ShredOrgDEK to finish lingering pending cleanup, got %v", err)
	}

	// Verify ShredOrgDEK retry when BOTH org pending (epoch=900) and a higher child pending epoch (epoch=901) exist:
	orgM, _ := st2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fence-orgm", DisplayName: "Fence M"})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgM.ID, Name: "orgm1", SecretKind: SecretKindAPIKey, Plaintext: []byte("m1")})
	failStoreM, _ := NewStore(Config{DB: st2.RawDB(), KEK: kek, PostShredHook: func(context.Context) error { return errors.New("busy") }})
	_ = failStoreM.ShredOrgDEK(ctx, orgM.ID, ShredRequest{RequestID: "orgm-max", ExpectedGeneration: 1})
	_, _ = st2.RawDB().ExecContext(ctx, `
		INSERT INTO vault_pending_shred_cleanups (scope_key, org_id, project_id, shred_epoch, target_generation, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'prj_fake', 999999, 1, 1000, 1000)`,
		projectPendingShredKey(orgM.ID, "prj_fake"), orgM.ID,
	)
	failStoreM.Close()
	if err := store2.ShredOrgDEK(ctx, orgM.ID, ShredRequest{RequestID: "orgm-max", ExpectedGeneration: 1}); err != nil {
		t.Fatalf("expected ShredOrgDEK retry to clear org + higher child epoch cleanly, got %v", err)
	}
	if store2.PendingShredCleanups() != 0 {
		t.Fatalf("expected 0 pending cleanups after orgM+child cleanup, got %d", store2.PendingShredCleanups())
	}

	orgD, _ := st2.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fence-orgd", DisplayName: "Fence D"})
	_, _ = store2.SealSecret(ctx, SealSecretParams{OrgID: orgD.ID, Name: "orgd-g1", SecretKind: SecretKindAPIKey, Plaintext: []byte("d1")})
	failStoreD, _ := NewStore(Config{DB: st2.RawDB(), KEK: kek, PostShredHook: func(context.Context) error { return errors.New("busy") }})
	_ = failStoreD.ShredOrgDEK(ctx, orgD.ID, ShredRequest{RequestID: "orgd-shred-g1", ExpectedGeneration: 1})
	failStoreD.Close()
	flushCloseStore, _ := NewStore(Config{
		DB:  st2.RawDB(),
		KEK: kek,
		PostShredHook: func(context.Context) error {
			return st2.Close()
		},
	})
	if err := flushCloseStore.FlushPendingShredCleanups(ctx); err == nil {
		t.Fatalf("expected error clearing flushed pending cleanups after DB close in PostShredHook")
	}
	flushCloseStore.Close()
}
