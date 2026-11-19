// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

func TestConfiguredMarketplaceKeyring(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(publicKey)
	keys, err := configuredMarketplaceKeyring(fmt.Sprintf(`{"release-2026":%q}`, encoded))
	if err != nil {
		t.Fatal(err)
	}
	if got := keys["release-2026"]; len(got) != ed25519.PublicKeySize {
		t.Fatalf("decoded public key length = %d", len(got))
	}
	for _, raw := range []string{
		`{"bad":"not-base64"}`,
		`{"bad":"` + base64.StdEncoding.EncodeToString([]byte("short")) + `"}`,
		`{"ok":"` + encoded + `"} {}`,
	} {
		if _, err := configuredMarketplaceKeyring(raw); err == nil {
			t.Errorf("invalid public key configuration accepted: %q", raw)
		}
	}
}

func TestParseOCIRegistryAuthRequiresExactUnescapedFields(t *testing.T) {
	username, password, err := parseOCIRegistryAuth([]byte(`{"username":"robot","password":"token:part"}`))
	if err != nil || string(username) != "robot" || string(password) != "token:part" {
		t.Fatalf("parse OCI credentials username=%q password=%q err=%v", username, password, err)
	}
	clear(username)
	clear(password)
	for _, raw := range []string{
		`{"username":"robot"}`,
		`{"username":"robot","password":"x","password":"y"}`,
		`{"username":"robot","password":"x",}`,
		`{"username":"robot","password":"x","extra":"y"}`,
		`{"username":"ro\"bot","password":"x"}`,
	} {
		if _, _, err := parseOCIRegistryAuth([]byte(raw)); err == nil {
			t.Errorf("invalid OCI registry credential object accepted: %s", raw)
		}
	}
}

func TestVaultOCIRegistryCredentialResolverIsOrganizationAndAuthorityScoped(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "tanpopo.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	org, err := store.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "registry-credentials", DisplayName: "Registry Credentials"})
	if err != nil {
		t.Fatal(err)
	}
	otherOrg, err := store.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "other-registry-credentials", DisplayName: "Other Registry Credentials"})
	if err != nil {
		t.Fatal(err)
	}
	kek, err := vault.NewStaticKEKProvider("kek_v1", []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	defer kek.Close()
	vaultStore, err := vault.NewStore(vault.Config{DB: store.RawDB(), KEK: kek, PostShredHook: store.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	defer vaultStore.Close()
	registryHost := "registry.example.test:5443"
	plaintext := []byte(`{"username":"robot","password":"token:part"}`)
	_, err = vaultStore.SealSecret(ctx, vault.SealSecretParams{
		OrgID: org.ID, Name: "private registry", SecretKind: vault.SecretKindOCIRegistryAuth,
		BoundAudience: &registryHost, Plaintext: plaintext,
	})
	clear(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	resolver := vaultOCIRegistryCredentialResolver{db: store.RawDB(), vault: vaultStore, cap: vault.MintUnsealCapabilityForBroker()}
	credentials, err := resolver.CredentialsForRegistry(ctx, org.ID, registryHost)
	if err != nil {
		t.Fatal(err)
	}
	if credentials == nil || string(credentials.Username) != "robot" || string(credentials.Password) != "token:part" {
		t.Fatalf("unexpected resolved registry credentials: %#v", credentials)
	}
	credentials.Zeroize()
	if credentials, err := resolver.CredentialsForRegistry(ctx, org.ID, "registry.example.test"); err != nil || credentials != nil {
		t.Fatalf("credential was returned for a different registry authority: credentials=%v err=%v", credentials, err)
	}
	if credentials, err := resolver.CredentialsForRegistry(ctx, otherOrg.ID, registryHost); err != nil || credentials != nil {
		t.Fatalf("credential crossed organization boundary: credentials=%v err=%v", credentials, err)
	}
}
