// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/registry"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
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

func TestMarketplaceRuntimeRetriesPendingActionsWithoutFeedPolling(t *testing.T) {
	target := registry.RevocationTarget{ActionID: "action-1", InstallationID: "install-1", Severity: "CRITICAL"}
	for _, failFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("fail_first_%t", failFirst), func(t *testing.T) {
			store := &runtimeRevocationStore{pending: []registry.RevocationTarget{target}, completed: make(chan string, 1)}
			actions := &runtimeRevocationActionsProbe{}
			actions.failFirst.Store(failFirst)
			watcher := &registry.RevocationWatcher{Store: store, Actions: actions, Now: time.Now}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				runMarketplaceRuntimeWithInterval(ctx, nil, watcher, false, time.Millisecond)
				close(done)
			}()
			select {
			case id := <-store.completed:
				if id != target.ActionID {
					t.Fatalf("completed action = %q, want %q", id, target.ActionID)
				}
			case <-time.After(2 * time.Second):
				cancel()
				<-done
				t.Fatal("pending action was not reconciled")
			}
			cancel()
			<-done
			if failFirst && actions.quarantineCalls.Load() < 2 {
				t.Fatalf("quarantine calls = %d, want retry after transient failure", actions.quarantineCalls.Load())
			}
			if actions.severed.Load() != 1 || actions.alerted.Load() != 1 {
				t.Fatalf("pending action was not fully reconciled: severed=%d alerted=%d", actions.severed.Load(), actions.alerted.Load())
			}
		})
	}
}

type runtimeRevocationStore struct {
	pending   []registry.RevocationTarget
	completed chan string
}

func (*runtimeRevocationStore) LastRevocationSerial(context.Context) (int64, error) { return 0, nil }
func (*runtimeRevocationStore) ApplyRevocationFeed(context.Context, int64, time.Time, []registry.RevocationEntry) error {
	return nil
}
func (s *runtimeRevocationStore) PendingRevocationActions(context.Context) ([]registry.RevocationTarget, error) {
	return append([]registry.RevocationTarget(nil), s.pending...), nil
}
func (s *runtimeRevocationStore) CompleteRevocationAction(_ context.Context, id string) error {
	s.pending = nil
	s.completed <- id
	return nil
}

type runtimeRevocationActionsProbe struct {
	quarantineCalls atomic.Int32
	severed         atomic.Int32
	alerted         atomic.Int32
	failFirst       atomic.Bool
}

func (a *runtimeRevocationActionsProbe) QuarantineInstallation(context.Context, registry.RevocationTarget) error {
	if a.quarantineCalls.Add(1) == 1 && a.failFirst.Load() {
		return errors.New("temporary quarantine failure")
	}
	return nil
}
func (a *runtimeRevocationActionsProbe) SeverInstallationStreams(context.Context, registry.RevocationTarget) error {
	a.severed.Add(1)
	return nil
}
func (a *runtimeRevocationActionsProbe) EmitINC03(context.Context, registry.RevocationTarget) error {
	a.alerted.Add(1)
	return nil
}

func TestParseOCIRegistryAuthRequiresExactUnescapedFields(t *testing.T) {
	username, password, cidrs, err := parseOCIRegistryAuth([]byte(`{"username":"robot","password":"token:part","allowed_cidrs":["10.20.0.0/16"]}`))
	if err != nil || string(username) != "robot" || string(password) != "token:part" || len(cidrs) != 1 || cidrs[0] != "10.20.0.0/16" {
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
		`{"username":"robot","password":"x","allowed_cidrs":["not-a-cidr"]}`,
		`{"username":"robot","password":"x","allowed_cidrs":["0.0.0.0/0"]}`,
		`{"username":"robot","password":"x","allowed_cidrs":["10.0.0.0/8",]}`,
	} {
		if _, _, _, err := parseOCIRegistryAuth([]byte(raw)); err == nil {
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
	plaintext := []byte(`{"username":"robot","password":"token:part","allowed_cidrs":["10.20.0.0/16"]}`)
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
	if credentials == nil || string(credentials.Username) != "robot" || string(credentials.Password) != "token:part" || len(credentials.AllowedCIDRs) != 1 || credentials.AllowedCIDRs[0] != "10.20.0.0/16" {
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

func TestRuntimeRevocationActionsEnforceAgainstSQLiteBackedInstallation(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "tanpopo.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db := store.RawDB()

	org, err := store.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "revocation-acceptance", DisplayName: "Revocation Acceptance"})
	if err != nil {
		t.Fatal(err)
	}
	approverID := idgen.MustNew(idgen.PrefixPrincipal)
	now := time.Now().UTC().UnixMilli()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'security-reviewer@example.test', 'ACTIVE', 1, ?, ?)`,
		approverID, org.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES (?, ?, ?, 'ORG', ?, 'SECURITY_REVIEWER', 1, ?)`,
		"rb_"+strings.TrimPrefix(approverID, "prc_"), org.ID, approverID, org.ID, now); err != nil {
		t.Fatal(err)
	}

	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	rootPublic, rootPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pluginsDir := filepath.Join(t.TempDir(), "plugins")
	lifecycle, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB: db, Audit: auditLog, AuditEntryPoint: "system_reaper",
		PluginsDir: pluginsDir, LockfilePath: filepath.Join(t.TempDir(), "tanpopo.lock"),
		Keyring: installer.TrustedKeyring{RootPublicKeys: map[string]ed25519.PublicKey{"test-root": rootPublic}},
	})
	if err != nil {
		t.Fatal(err)
	}

	const manifest = `apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@community/revocation-acceptance"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"
permissions:
  networkEgress:
    allowedHosts: ["api.example.test"]
    allowPrivateIp: false
    requireTls: true
`
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gz)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "manifest.yaml", Mode: 0o644, Size: int64(len(manifest)), ModTime: time.Unix(0, 0), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	archiveBytes := archive.Bytes()
	digest := sha256.Sum256(archiveBytes)
	digestHex := hex.EncodeToString(digest[:])
	signature := ed25519.Sign(rootPrivate, installer.SignatureSigningPayload("@community/revocation-acceptance", "1.0.0", digestHex))
	staged, err := lifecycle.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchiveReader: bytes.NewReader(archiveBytes),
		Source:        installer.SourceSpec{Kind: installer.SourceKindLocalArchive, URI: "./revocation-acceptance.tpkg"},
		Signature:     installer.ProvenanceSignature{PublisherOrRootSig: signature},
	})
	if err != nil {
		t.Fatalf("StageFreshInstall: %v", err)
	}
	active, err := lifecycle.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID: staged.Installation.InstallationID, ExpectedVersion: staged.Installation.Version,
		ApproverPrincipalID: approverID, ActivateImmediately: true, OrgID: org.ID,
	})
	if err != nil {
		t.Fatalf("ApproveAndActivate: %v", err)
	}
	if active.State != installer.StateEnabledActive {
		t.Fatalf("installation did not become active: state=%q", active.State)
	}

	snaps, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	matchedCtx, cancelMatched := context.WithCancel(ctx)
	defer cancelMatched()
	unrelatedCtx, cancelUnrelated := context.WithCancel(ctx)
	defer cancelUnrelated()
	matchedHandle := snaps.Streams().Register("org_test", "project_test", "matched_key", cancelMatched)
	snaps.Streams().BindTarget("matched_key", matchedHandle, "connection_test", active.InstallationID)
	unrelatedHandle := snaps.Streams().Register("org_test", "project_test", "unrelated_key", cancelUnrelated)
	snaps.Streams().BindTarget("unrelated_key", unrelatedHandle, "connection_test", "another_installation")

	actions := runtimeRevocationActions{installer: lifecycle, snapshots: snaps, audit: auditLog}
	target := registry.RevocationTarget{
		InstallationID: active.InstallationID, PackageSlug: "community/revocation-acceptance",
		VersionOrSHA256: "1.0.0", Severity: "CRITICAL_MALICIOUS", Reason: "signed acceptance-test advisory",
	}
	if err := actions.QuarantineInstallation(ctx, target); err != nil {
		t.Fatalf("QuarantineInstallation: %v", err)
	}
	quarantined, err := lifecycle.GetInstallation(ctx, active.InstallationID)
	if err != nil || quarantined.State != installer.StateQuarantined {
		t.Fatalf("installation state after revocation = %v, err=%v; want %s", quarantined, err, installer.StateQuarantined)
	}
	if err := actions.SeverInstallationStreams(ctx, target); err != nil {
		t.Fatalf("SeverInstallationStreams: %v", err)
	}
	select {
	case <-matchedCtx.Done():
	default:
		t.Fatal("stream bound to the revoked installation remained active")
	}
	select {
	case <-unrelatedCtx.Done():
		t.Fatal("revocation aborted an unrelated installation stream")
	default:
	}
	if err := actions.EmitINC03(ctx, target); err != nil {
		t.Fatalf("EmitINC03: %v", err)
	}
	events, err := auditLog.List(ctx, audit.ListFilter{ChainKey: audit.SystemChainKey, EventType: "INC-03", TargetType: "extension_installation", TargetID: active.InstallationID})
	if err != nil {
		t.Fatalf("read persisted INC-03 event: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("persisted INC-03 events = %d, want 1", len(events))
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(events[0].MetadataJSON), &metadata); err != nil {
		t.Fatalf("decode persisted event metadata: %v", err)
	}
	if events[0].ActorID != "system_reaper" || metadata["incident"] != "INC-03" || metadata["package_slug"] != target.PackageSlug || metadata["severity"] != target.Severity {
		t.Fatalf("persisted INC-03 event has unexpected actor or metadata: %+v metadata=%+v", events[0], metadata)
	}
	if _, err := audit.VerifyAuditChain(ctx, db, audit.VerifyOptions{}); err != nil {
		t.Fatalf("persisted audit chain failed verification: %v", err)
	}
}
