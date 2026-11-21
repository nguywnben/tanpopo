// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package kms

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type mockExternalKMSServer struct {
	mu   sync.RWMutex
	keys map[string][]byte
	srv  *httptest.Server
}

func newMockExternalKMSServer(t *testing.T, initialKeys ...string) *mockExternalKMSServer {
	t.Helper()
	m := &mockExternalKMSServer{
		keys: make(map[string][]byte),
	}
	for _, k := range initialKeys {
		raw := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, raw); err != nil {
			t.Fatalf("rand: %v", err)
		}
		m.keys[k] = raw
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockExternalKMSServer) destroyKey(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if raw, ok := m.keys[id]; ok {
		clear(raw)
		delete(m.keys, id)
	}
}

func (m *mockExternalKMSServer) localEgressPolicy() *egress.Policy {
	u, _ := url.Parse(m.srv.URL)
	return &egress.Policy{
		AllowedHosts:   []string{u.Host},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.0/8"},
		RequireTLS:     false,
	}
}

func (m *mockExternalKMSServer) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	amzTarget := r.Header.Get("X-Amz-Target")
	switch {
	case amzTarget == "TrentService.Encrypt" || amzTarget == "TrentService.Decrypt":
		m.handleAWS(w, r, amzTarget, body)
	case strings.Contains(r.URL.Path, "/v1/transit/encrypt/") || strings.Contains(r.URL.Path, "/v1/transit/decrypt/"):
		m.handleVaultTransit(w, r, body)
	case strings.HasSuffix(r.URL.Path, ":encrypt") || strings.HasSuffix(r.URL.Path, ":decrypt"):
		m.handleGCP(w, r, body)
	default:
		http.Error(w, "unknown kms route", http.StatusNotFound)
	}
}

func canonicalAWSContextAAD(ctx map[string]string) []byte {
	keys := make([]string, 0, len(ctx))
	for k := range ctx {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(ctx[k])
		b.WriteByte(';')
	}
	return []byte(b.String())
}

func sealWithMockKey(key, aad, pt []byte) ([]byte, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, pt, aad)
	return append(nonce, ct...), nil
}

func openWithMockKey(key, aad, blob []byte) ([]byte, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	if len(blob) <= gcm.NonceSize() {
		return nil, errors.New("short ciphertext")
	}
	return gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], aad)
}

func (m *mockExternalKMSServer) handleAWS(w http.ResponseWriter, r *http.Request, target string, body []byte) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") || r.Header.Get("X-Amz-Date") == "" {
		http.Error(w, "missing SigV4", http.StatusUnauthorized)
		return
	}
	var req struct {
		KeyID             string            `json:"KeyId"`
		Plaintext         string            `json:"Plaintext"`
		CiphertextBlob    string            `json:"CiphertextBlob"`
		EncryptionContext map[string]string `json:"EncryptionContext"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	m.mu.RLock()
	rawKey := m.keys[req.KeyID]
	m.mu.RUnlock()
	if len(rawKey) != 32 {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}
	aad := canonicalAWSContextAAD(req.EncryptionContext)
	if target == "TrentService.Encrypt" {
		pt, _ := base64.StdEncoding.DecodeString(req.Plaintext)
		blob, err := sealWithMockKey(rawKey, aad, pt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"KeyId":          req.KeyID,
			"CiphertextBlob": base64.StdEncoding.EncodeToString(blob),
		})
		return
	}
	ct, _ := base64.StdEncoding.DecodeString(req.CiphertextBlob)
	pt, err := openWithMockKey(rawKey, aad, ct)
	if err != nil {
		http.Error(w, "decrypt failed", http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"KeyId":     req.KeyID,
		"Plaintext": base64.StdEncoding.EncodeToString(pt),
	})
}

func (m *mockExternalKMSServer) handleGCP(w http.ResponseWriter, r *http.Request, body []byte) {
	if r.Header.Get("Authorization") != "Bearer gcp-test-token" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	isEncrypt := strings.HasSuffix(r.URL.Path, ":encrypt")
	keyName := strings.TrimPrefix(r.URL.Path, "/v1/")
	keyName = strings.TrimSuffix(strings.TrimSuffix(keyName, ":encrypt"), ":decrypt")

	var req struct {
		Plaintext                   string `json:"plaintext"`
		Ciphertext                  string `json:"ciphertext"`
		AdditionalAuthenticatedData string `json:"additionalAuthenticatedData"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	aad, _ := base64.StdEncoding.DecodeString(req.AdditionalAuthenticatedData)
	m.mu.RLock()
	rawKey := m.keys[keyName]
	m.mu.RUnlock()
	if len(rawKey) != 32 {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}
	if isEncrypt {
		pt, _ := base64.StdEncoding.DecodeString(req.Plaintext)
		blob, err := sealWithMockKey(rawKey, aad, pt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ciphertext": base64.StdEncoding.EncodeToString(blob),
		})
		return
	}
	ct, _ := base64.StdEncoding.DecodeString(req.Ciphertext)
	pt, err := openWithMockKey(rawKey, aad, ct)
	if err != nil {
		http.Error(w, "decrypt failed", http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"plaintext": base64.StdEncoding.EncodeToString(pt),
	})
}

func (m *mockExternalKMSServer) handleVaultTransit(w http.ResponseWriter, r *http.Request, body []byte) {
	if r.Header.Get("X-Vault-Token") != "hvs.test-token" {
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}
	isEncrypt := strings.Contains(r.URL.Path, "/v1/transit/encrypt/")
	keyName := strings.TrimPrefix(r.URL.Path, "/v1/transit/encrypt/")
	keyName = strings.TrimPrefix(keyName, "/v1/transit/decrypt/")

	var req struct {
		Plaintext  string `json:"plaintext"`
		Ciphertext string `json:"ciphertext"`
		Context    string `json:"context"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	aad, _ := base64.StdEncoding.DecodeString(req.Context)
	m.mu.RLock()
	rawKey := m.keys[keyName]
	m.mu.RUnlock()
	if len(rawKey) != 32 {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}
	if isEncrypt {
		pt, _ := base64.StdEncoding.DecodeString(req.Plaintext)
		blob, err := sealWithMockKey(rawKey, aad, pt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"ciphertext": "vault:v1:" + base64.StdEncoding.EncodeToString(blob),
			},
		})
		return
	}
	rawCT := strings.TrimPrefix(req.Ciphertext, "vault:v1:")
	ct, _ := base64.StdEncoding.DecodeString(rawCT)
	pt, err := openWithMockKey(rawKey, aad, ct)
	if err != nil {
		http.Error(w, "decrypt failed", http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"plaintext": base64.StdEncoding.EncodeToString(pt),
		},
	})
}

func TestExternalKMSProvidersWrapUnwrapAndContextBinding(t *testing.T) {
	ctx := context.Background()
	mock := newMockExternalKMSServer(t,
		"arn:aws:kms:us-east-1:111122223333:key/aws-master",
		"projects/tp/locations/global/keyRings/kr/cryptoKeys/gcp-master",
		"transit-master",
	)
	pol := mock.localEgressPolicy()

	awsProv, err := NewAWSKMSProvider(AWSKMSConfig{
		EndpointURL:  mock.srv.URL,
		Region:       "us-east-1",
		ActiveKeyID:  "arn:aws:kms:us-east-1:111122223333:key/aws-master",
		Creds:        StaticAWSCredentials{Creds: AWSCredentials{AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secretkeyexample1234"}},
		EgressPolicy: pol,
	})
	if err != nil {
		t.Fatalf("NewAWSKMSProvider: %v", err)
	}

	gcpProv, err := NewGCPKMSProvider(GCPKMSConfig{
		EndpointURL:   mock.srv.URL,
		ActiveKeyName: "projects/tp/locations/global/keyRings/kr/cryptoKeys/gcp-master",
		TokenSource:   StaticBearerToken("gcp-test-token"),
		EgressPolicy:  pol,
	})
	if err != nil {
		t.Fatalf("NewGCPKMSProvider: %v", err)
	}

	vaultProv, err := NewVaultTransitProvider(VaultTransitConfig{
		Address:       mock.srv.URL,
		Token:         "hvs.test-token",
		Mount:         "transit",
		ActiveKeyName: "transit-master",
		EgressPolicy:  pol,
	})
	if err != nil {
		t.Fatalf("NewVaultTransitProvider: %v", err)
	}

	dek := sha256.Sum256([]byte("sample-256-bit-dek"))
	for _, tc := range []struct {
		name string
		prov vault.KEKProvider
	}{
		{"aws", awsProv},
		{"gcp", gcpProv},
		{"vault", vaultProv},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keyID, err := tc.prov.ActiveKeyID(ctx)
			if err != nil {
				t.Fatalf("ActiveKeyID: %v", err)
			}
			nonce, ct, err := tc.prov.WrapDEK(ctx, keyID, "org_a", 1, dek[:])
			if err != nil {
				t.Fatalf("WrapDEK: %v", err)
			}
			pt, err := tc.prov.UnwrapDEK(ctx, keyID, "org_a", 1, nonce, ct)
			if err != nil {
				t.Fatalf("UnwrapDEK: %v", err)
			}
			if string(pt) != string(dek[:]) {
				t.Fatalf("unwrapped DEK mismatch")
			}
			if _, err := tc.prov.UnwrapDEK(ctx, keyID, "org_b", 1, nonce, ct); !errors.Is(err, vault.ErrDecryptionFailed) {
				t.Fatalf("expected ErrDecryptionFailed on cross-org AAD mismatch, got %v", err)
			}
		})
	}
}

func TestPerProjectExternalRootKeyDestructionRendersColdBackupUndecryptableAC3(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "live.db")
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	mock := newMockExternalKMSServer(t, "proj1-transit-key", "proj2-transit-key")
	vaultTransit, err := NewVaultTransitProvider(VaultTransitConfig{
		Address:       mock.srv.URL,
		Token:         "hvs.test-token",
		Mount:         "transit",
		ActiveKeyName: "proj1-transit-key",
		EgressPolicy:  mock.localEgressPolicy(),
	})
	if err != nil {
		t.Fatalf("NewVaultTransitProvider: %v", err)
	}

	masterRaw := sha256.Sum256([]byte("global-master-kek-unchanged"))
	staticKEK, err := vault.NewStaticKEKProvider("kek_global_v1", masterRaw[:])
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	router, err := NewRoutingProvider(staticKEK)
	if err != nil {
		t.Fatalf("NewRoutingProvider: %v", err)
	}

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "kms-org-a", DisplayName: "KMS Org A"})
	if err != nil {
		t.Fatalf("CreateOrganization A: %v", err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "kms-org-b", DisplayName: "KMS Org B"})
	if err != nil {
		t.Fatalf("CreateOrganization B: %v", err)
	}
	p1, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p1", DisplayName: "Project 1"})
	if err != nil {
		t.Fatalf("CreateProject 1: %v", err)
	}
	p2, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p2", DisplayName: "Project 2"})
	if err != nil {
		t.Fatalf("CreateProject 2: %v", err)
	}
	pB, err := st.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "pb", DisplayName: "Project B"})
	if err != nil {
		t.Fatalf("CreateProject B: %v", err)
	}

	if err := router.BindProjectKey(orgA.ID, p1.ID, "vault-transit://proj1-transit-key", vaultTransit); err != nil {
		t.Fatalf("BindProjectKey p1: %v", err)
	}
	if err := router.BindProjectKey(orgA.ID, p2.ID, "vault-transit://proj2-transit-key", vaultTransit); err != nil {
		t.Fatalf("BindProjectKey p2: %v", err)
	}
	router.RegisterScheme("vault-transit://", vaultTransit)

	vs, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           router,
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}

	// Seal cache blobs in p1, p2 (both external KMS), and pB (global static Master KEK -> Org B DEK).
	p1Key := idgen.MustNew(idgen.PrefixTrace)
	p1Nonce, p1CT, err := vs.SealProjectBlob(ctx, orgA.ID, p1.ID, vault.RecordKindCacheEntry, p1Key, []byte(`{"answer":"project1-secret-reply"}`))
	if err != nil {
		t.Fatalf("SealProjectBlob p1: %v", err)
	}
	p2Key := idgen.MustNew(idgen.PrefixTrace)
	p2Nonce, p2CT, err := vs.SealProjectBlob(ctx, orgA.ID, p2.ID, vault.RecordKindCacheEntry, p2Key, []byte(`{"answer":"project2-secret-reply"}`))
	if err != nil {
		t.Fatalf("SealProjectBlob p2: %v", err)
	}
	pBKey := idgen.MustNew(idgen.PrefixTrace)
	pBNonce, pBCT, err := vs.SealProjectBlob(ctx, orgB.ID, pB.ID, vault.RecordKindCacheEntry, pBKey, []byte(`{"answer":"orgB-secret-reply"}`))
	if err != nil {
		t.Fatalf("SealProjectBlob pB: %v", err)
	}

	// Take a pre-shred cold backup DB snapshot while p1's database row still exists intact!
	if err := st.CheckpointWAL(ctx); err != nil {
		t.Fatalf("CheckpointWAL: %v", err)
	}
	coldPath := filepath.Join(tmpDir, "cold-pre-shred.db")
	rawLive, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read live db: %v", err)
	}
	if err := os.WriteFile(coldPath, rawLive, 0o600); err != nil {
		t.Fatalf("write cold backup: %v", err)
	}

	// Destroy ONLY p1's dedicated external root key ("proj1-transit-key").
	mock.destroyKey("proj1-transit-key")
	vs.EvictProjectDEKCache(orgA.ID, p1.ID)

	// Restore cold pre-shred DB backup in a clean process Store -> p1 must be permanently undecryptable,
	// while p2 (external sibling key) & pB (global Master KEK) remain 100% decryptable without global KEK rotation!
	coldStore, err := sqlite.Open(ctx, sqlite.Config{DBPath: coldPath})
	if err != nil {
		t.Fatalf("open cold backup: %v", err)
	}
	defer func() { _ = coldStore.Close() }()
	coldVault, err := vault.NewStore(vault.Config{
		DB:            coldStore.RawDB(),
		KEK:           router,
		PostShredHook: coldStore.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("cold vault.NewStore: %v", err)
	}

	if _, err := coldVault.UnsealProjectBlob(ctx, orgA.ID, p1.ID, vault.RecordKindCacheEntry, p1Key, p1Nonce, p1CT); !errors.Is(err, vault.ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed reading cold backup of p1 after external key destruction, got %v", err)
	}
	p2Plain, err := coldVault.UnsealProjectBlob(ctx, orgA.ID, p2.ID, vault.RecordKindCacheEntry, p2Key, p2Nonce, p2CT)
	if err != nil || string(p2Plain) != `{"answer":"project2-secret-reply"}` {
		t.Fatalf("sibling project p2 impacted by p1 key destruction: err=%v val=%q", err, string(p2Plain))
	}
	pBPlain, err := coldVault.UnsealProjectBlob(ctx, orgB.ID, pB.ID, vault.RecordKindCacheEntry, pBKey, pBNonce, pBCT)
	if err != nil || string(pBPlain) != `{"answer":"orgB-secret-reply"}` {
		t.Fatalf("sibling org pB impacted by p1 key destruction: err=%v val=%q", err, string(pBPlain))
	}
}
