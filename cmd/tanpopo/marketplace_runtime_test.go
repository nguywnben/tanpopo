// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/registry"
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

type recordedMarketplaceQuarantine struct {
	installationID string
	reason         string
	actorID        string
}

func (r *recordedMarketplaceQuarantine) QuarantineInstallation(_ context.Context, installationID, reason, actorID string) error {
	r.installationID, r.reason, r.actorID = installationID, reason, actorID
	return nil
}

type recordedMarketplaceAudit struct{ events []port.AuditEvent }

func (r *recordedMarketplaceAudit) Append(_ context.Context, event port.AuditEvent) error {
	r.events = append(r.events, event)
	return nil
}

func TestRuntimeRevocationActionsQuarantineAbortStreamsAndEmitINC03(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "tanpopo.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snaps, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: store.RawDB()})
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	handle := snaps.Streams().Register("org_test", "project_test", "key_test", cancelStream)
	snaps.Streams().BindTarget("key_test", handle, "connection_test", "installation_test")
	quarantine := &recordedMarketplaceQuarantine{}
	audit := &recordedMarketplaceAudit{}
	actions := runtimeRevocationActions{installer: quarantine, snapshots: snaps, audit: audit}
	target := registry.RevocationTarget{
		InstallationID: "installation_test", PackageSlug: "community/bad-adapter",
		VersionOrSHA256: "1.0.0", Severity: "CRITICAL", Reason: "confirmed vulnerable",
	}
	if err := actions.QuarantineInstallation(ctx, target); err != nil {
		t.Fatal(err)
	}
	if quarantine.installationID != target.InstallationID || quarantine.actorID != "system_reaper" || !strings.Contains(quarantine.reason, target.Reason) {
		t.Fatalf("unexpected quarantine call: %+v", quarantine)
	}
	if err := actions.SeverInstallationStreams(ctx, target); err != nil {
		t.Fatal(err)
	}
	select {
	case <-streamCtx.Done():
	default:
		t.Fatal("revoked installation stream was not aborted")
	}
	if err := actions.EmitINC03(ctx, target); err != nil {
		t.Fatal(err)
	}
	if len(audit.events) != 1 || audit.events[0].EventType != "INC-03" || audit.events[0].TargetID != target.InstallationID {
		t.Fatalf("INC-03 audit event missing or misdirected: %+v", audit.events)
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(audit.events[0].MetadataJSON), &metadata); err != nil || metadata["incident"] != "INC-03" || metadata["package_slug"] != target.PackageSlug {
		t.Fatalf("INC-03 audit metadata malformed: %s err=%v", audit.events[0].MetadataJSON, err)
	}
}
