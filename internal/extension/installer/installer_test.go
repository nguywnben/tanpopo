// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
	"github.com/nguywnben/tanpopo/internal/testkit/wasmfixtures"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type auditCapture struct {
	mu     sync.Mutex
	events []port.AuditEvent
}

func (a *auditCapture) Append(_ context.Context, ev port.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, ev)
	return nil
}

func (a *auditCapture) AppendTx(ctx context.Context, _ *sql.Tx, ev port.AuditEvent) error {
	return a.Append(ctx, ev)
}

func (a *auditCapture) hasEventType(et string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ev := range a.events {
		if ev.EventType == et {
			return true
		}
	}
	return false
}

type archiveEntrySpec struct {
	name     string
	typeflag byte
	body     []byte
	linkname string
	mode     int64
}

func buildTPKGBytes(t testing.TB, entries []archiveEntrySpec) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		tf := e.typeflag
		if tf == 0 {
			tf = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			if tf == tar.TypeDir {
				mode = 0o755
			} else {
				mode = 0o644
			}
		}
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: tf,
			Mode:     mode,
			Size:     int64(len(e.body)),
			Linkname: e.linkname,
			ModTime:  time.Unix(0, 0).UTC(),
		}
		if tf != tar.TypeReg {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%q): %v", e.name, err)
		}
		if tf == tar.TypeReg && len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatalf("Write(%q): %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}
	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func makeWasmManifestYAML(pkgID, ver, contractVer, coreRange string, wasmSHA string, allowedHosts []string, secretSlots []string, memMib int) []byte {
	var hostsYAML strings.Builder
	for _, h := range allowedHosts {
		fmt.Fprintf(&hostsYAML, "      - %q\n", h)
	}
	var slotsYAML strings.Builder
	for _, sl := range secretSlots {
		fmt.Fprintf(&slotsYAML, "  - slot: %q\n", sl)
	}
	return []byte(fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: %q
  name: "Test Provider Adapter"
  version: %q
  license: "Apache-2.0"
compatibility:
  contractVersion: %q
  coreVersionRange: %q
runtime:
  tier: "sandboxed_wasm"
  entrypoint: "dist/adapter.wasm"
  sha256: %q
  quotas:
    maxMemoryMib: %d
    maxCpuMsPerCall: 15
    maxStreamChunkCpuMs: 5
capabilities:
  - type: "provider_adapter"
    adapterSlug: "acme"
    supportedDialects: ["openai_compatible"]
    authMethods:
      - id: "api_key_header"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
%spermissions:
  networkEgress:
    allowedHosts:
%s    allowPrivateIp: false
    requireTls: true
  hooks:
    - "provider:transform_request"
`, pkgID, ver, contractVer, coreRange, wasmSHA, memMib, slotsYAML.String(), hostsYAML.String()))
}

type installerHarness struct {
	st         *sqlite.Store
	svc        *LifecycleService
	audit      *auditCapture
	fc         *clock.FakeClock
	pluginsDir string
	lockPath   string
	rootPriv   ed25519.PrivateKey
	pubPriv    ed25519.PrivateKey
	entPriv    ed25519.PrivateKey
	orgID      string
	opID       string
	orgOwnerID string
	secRevID   string
	devID      string
}

func newInstallerHarness(t *testing.T, orgMinTrust string) *installerHarness {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	fc := clock.NewFakeClock(clock.DefaultFakeStart())

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(tmp, "data", "tanpopo.db"),
		NowMs:  func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if orgMinTrust == "" {
		orgMinTrust = port.MinTrustAnyApproved
	}
	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		Slug:           "org-ext-test",
		DisplayName:    "Org Ext Test",
		MinPluginTrust: orgMinTrust,
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	opID := idgen.MustNew(idgen.PrefixPrincipal)
	orgOwnerID := idgen.MustNew(idgen.PrefixPrincipal)
	secRevID := idgen.MustNew(idgen.PrefixPrincipal)
	devID := idgen.MustNew(idgen.PrefixPrincipal)
	now := fc.Now().UnixMilli()
	for _, p := range []struct {
		id    string
		email string
		scope string
		sid   string
		org   *string
		role  string
	}{
		{opID, "op@example.com", "INSTANCE", "instance", nil, "OPERATOR"},
		{orgOwnerID, "owner@example.com", "ORG", org.ID, &org.ID, "ORG_OWNER"},
		{secRevID, "sec@example.com", "ORG", org.ID, &org.ID, "SECURITY_REVIEWER"},
		{devID, "dev@example.com", "ORG", org.ID, &org.ID, "DEVELOPER"},
	} {
		var orgNull interface{}
		if p.org != nil {
			orgNull = *p.org
		}
		if _, err := st.RawDB().ExecContext(ctx, `
			INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
			VALUES (?, ?, 'HUMAN_LOCAL', ?, 'ACTIVE', 1, ?, ?)`,
			p.id, orgNull, p.email, now, now,
		); err != nil {
			t.Fatalf("insert principal: %v", err)
		}
		if _, err := st.RawDB().ExecContext(ctx, `
			INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
			"rb_"+strings.TrimPrefix(p.id, "prc_"), orgNull, p.id, p.scope, p.sid, p.role, now,
		); err != nil {
			t.Fatalf("insert role_binding: %v", err)
		}
	}

	rootPub, rootPriv, _ := ed25519.GenerateKey(rand.Reader)
	pubPub, pubPriv, _ := ed25519.GenerateKey(rand.Reader)
	entPub, entPriv, _ := ed25519.GenerateKey(rand.Reader)

	pluginsDir := filepath.Join(tmp, "plugins")
	lockPath := filepath.Join(tmp, "tanpopo.lock")
	audit := &auditCapture{}

	svc, err := NewLifecycleService(ServiceConfig{
		DB:           st.RawDB(),
		Audit:        audit,
		PluginsDir:   pluginsDir,
		LockfilePath: lockPath,
		Keyring: TrustedKeyring{
			RootPublicKeys:       map[string]ed25519.PublicKey{"root": rootPub},
			PublisherPublicKeys:  map[string]ed25519.PublicKey{"pub": pubPub},
			EnterprisePublicKeys: map[string]ed25519.PublicKey{"ent": entPub},
		},
		NowMs: func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("NewLifecycleService: %v", err)
	}

	return &installerHarness{
		st:         st,
		svc:        svc,
		audit:      audit,
		fc:         fc,
		pluginsDir: pluginsDir,
		lockPath:   lockPath,
		rootPriv:   rootPriv,
		pubPriv:    pubPriv,
		entPriv:    entPriv,
		orgID:      org.ID,
		opID:       opID,
		orgOwnerID: orgOwnerID,
		secRevID:   secRevID,
		devID:      devID,
	}
}

func TestInstallerAC1FullLifecycleAndFastRollback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newInstallerHarness(t, port.MinTrustVerifiedOrEnterpriseSigned)

	wasmBytes := wasmfixtures.EchoTransformWASM()
	wasmSHA := shaHex(wasmBytes)

	// 1. Build v1.0.0 .tpkg signed by Enterprise key + immutable 40-hex Git SHA source.
	m1 := makeWasmManifestYAML("@acme-cloud/regional", "1.0.0", "0.1", ">=0.1.0 <0.3.0", wasmSHA, []string{"api.acme.example"}, []string{"primary_api_key"}, 16)
	tpkg1, sha1 := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: m1},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	gitSHA1 := "0123456789abcdef0123456789abcdef01234567"
	sig1 := ed25519.Sign(h.entPriv, SignatureSigningPayload("@acme-cloud/regional", "1.0.0", sha1))

	staged, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkg1),
		Source:        SourceSpec{Kind: SourceKindGit, URI: "https://github.com/acme/adapter.git", Ref: gitSHA1, ExpectedSHA256: sha1},
		Signature:     ProvenanceSignature{EnterpriseSig: sig1},
		ActorID:       h.devID,
		TargetOrgID:   h.orgID,
	})
	if err != nil {
		t.Fatalf("StageFreshInstall v1.0.0: %v", err)
	}
	if staged.Installation.State != StatePendingPermissionApproval || staged.Installation.ActiveVersionID != nil {
		t.Fatalf("expected PENDING_PERMISSION_APPROVAL with NULL active_version_id, got %+v", staged.Installation)
	}
	if staged.Provenance.OriginTrustTier != lockfile.TrustTierUnverifiedExternal || !staged.Provenance.EnterpriseSigned {
		t.Fatalf("unexpected orthogonal provenance flags: %+v", staged.Provenance)
	}

	// Non-privileged DEVELOPER cannot approve capabilities.
	if _, rerr := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: h.devID,
		ActivateImmediately: true,
		OrgID:               h.orgID,
	}); !errors.Is(rerr, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for DEVELOPER approval, got %v", rerr)
	}

	// SECURITY_REVIEWER approves -> ENABLED_ACTIVE, config_changelog incremented, tanpopo.lock written.
	epochBefore, _ := h.st.Changelog().LatestEpoch(ctx)
	activated, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: h.secRevID,
		ActivateImmediately: true,
		OrgID:               h.orgID,
	})
	if err != nil {
		t.Fatalf("ApproveAndActivate: %v", err)
	}
	if activated.State != StateEnabledActive || activated.ActiveVersionID == nil {
		t.Fatalf("expected ENABLED_ACTIVE with non-nil active_version_id, got %+v", activated)
	}
	epochAfter, _ := h.st.Changelog().LatestEpoch(ctx)
	if epochAfter <= epochBefore {
		t.Fatalf("expected config_changelog epoch increment (%d -> %d)", epochBefore, epochAfter)
	}
	if err := h.svc.VerifyPinnedArchiveOnLoad(ctx, "@acme-cloud/regional", sha1, h.secRevID); err != nil {
		t.Fatalf("VerifyPinnedArchiveOnLoad: %v", err)
	}

	// 2. Non-escalating upgrade (v1.0.0 -> v1.0.1 with identical permissions) activates smoothly with rollback retained.
	m101 := makeWasmManifestYAML("@acme-cloud/regional", "1.0.1", "0.1", ">=0.1.0 <0.3.0", wasmSHA, []string{"api.acme.example"}, []string{"primary_api_key"}, 16)
	tpkg101, sha101 := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: m101},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	sig101 := ed25519.Sign(h.entPriv, SignatureSigningPayload("@acme-cloud/regional", "1.0.1", sha101))
	upRes, err := h.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  activated.InstallationID,
		ExpectedVersion: activated.Version,
		ArchiveReader:   bytes.NewReader(tpkg101),
		Source:          SourceSpec{Kind: SourceKindGit, URI: "https://github.com/acme/adapter.git", Ref: "fedcba9876543210fedcba9876543210fedcba98"},
		Signature:       ProvenanceSignature{EnterpriseSig: sig101},
		ActorID:         h.secRevID,
		OrgID:           h.orgID,
	})
	if err != nil || upRes.Escalated || *upRes.Installation.ActiveSemver != "1.0.1" {
		t.Fatalf("expected non-escalating cutover to 1.0.1, got res=%+v err=%v", upRes, err)
	}

	// 3. Rollback (< 5s SLA) restores v1.0.0 atomically and updates tanpopo.lock.
	rbStart := time.Now()
	rolledBack, err := h.svc.RollbackInstallation(ctx, upRes.Installation.InstallationID, upRes.Installation.Version, h.secRevID, h.orgID, "req_rb")
	rbElapsed := time.Since(rbStart)
	if err != nil {
		t.Fatalf("RollbackInstallation: %v", err)
	}
	if rbElapsed >= 5*time.Second || rolledBack.State != StateEnabledActive || *rolledBack.ActiveSemver != "1.0.0" {
		t.Fatalf("unexpected rollback result (%v): %+v", rbElapsed, rolledBack)
	}
	if rolledBack.PreviousPinnedVersionID != nil {
		t.Fatalf("expected previous_pinned_version_id cleared after rollback to avoid ping-pong")
	}
	if err := h.svc.VerifyPinnedArchiveOnLoad(ctx, "@acme-cloud/regional", sha1, h.secRevID); err != nil {
		t.Fatalf("expected tanpopo.lock reverted to v1.0.0 digest: %v", err)
	}

	// 4. Emergency Quarantine (exported for fault_guard in TP-IMP-0013) emits emergency=1 changelog.
	if err := h.svc.QuarantineInstallationIfVersion(ctx, rolledBack.InstallationID, rolledBack.Version-1, "stale quarantine request", "fault_guard"); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("expected stale quarantine ETag to conflict, got %v", err)
	}
	stillActive, err := h.svc.GetInstallation(ctx, rolledBack.InstallationID)
	if err != nil || stillActive.Version != rolledBack.Version || stillActive.State != StateEnabledActive {
		t.Fatalf("stale quarantine changed installation: %+v err=%v", stillActive, err)
	}
	outboxErr := errors.New("transactional webhook outbox unavailable")
	h.svc.recordQuarantineEventTx = func(_ context.Context, tx *sql.Tx, installationID, packageID, state string) error {
		if installationID != rolledBack.InstallationID || packageID == "" || state != StateQuarantined {
			t.Fatalf("unexpected quarantine event args: installation=%q package=%q state=%q", installationID, packageID, state)
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM extension_installations WHERE id=? AND state=?`, installationID, StateQuarantined).Scan(&count); err != nil || count != 1 {
			t.Fatalf("quarantine outbox callback must share mutation transaction, count=%d err=%v", count, err)
		}
		return outboxErr
	}
	if err := h.svc.QuarantineInstallationIfVersion(ctx, rolledBack.InstallationID, rolledBack.Version, "outbox rollback test", "fault_guard"); !errors.Is(err, outboxErr) {
		t.Fatalf("expected outbox failure to abort quarantine, got %v", err)
	}
	stillActive, err = h.svc.GetInstallation(ctx, rolledBack.InstallationID)
	if err != nil || stillActive.Version != rolledBack.Version || stillActive.State != StateEnabledActive {
		t.Fatalf("failed outbox write committed quarantine: %+v err=%v", stillActive, err)
	}
	h.svc.recordQuarantineEventTx = nil
	if err := h.svc.QuarantineInstallationIfVersion(ctx, rolledBack.InstallationID, rolledBack.Version, "exceeded 20 traps / 60s (FMEA-05)", "fault_guard"); err != nil {
		t.Fatalf("QuarantineInstallation: %v", err)
	}
	qRec, err := h.svc.GetInstallation(ctx, rolledBack.InstallationID)
	if err != nil || qRec.State != StateQuarantined {
		t.Fatalf("expected QUARANTINED state, got %+v err=%v", qRec, err)
	}
	if err := h.svc.UninstallInstallation(ctx, qRec.InstallationID, qRec.Version, staged.Archive.StagingDir, h.secRevID, h.orgID); err != nil {
		t.Fatalf("UninstallInstallation from QUARANTINED: %v", err)
	}
	if _, statErr := os.Stat(staged.Archive.StagingDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected package staging directory purged cleanly on uninstall")
	}
}

func TestInstallerNEG1_CT01_CT02_CT24_ManifestSchemaAndCompatibility(t *testing.T) {
	t.Parallel()
	pol := DefaultValidationPolicy()

	// CT-01a: Unknown permission key rejected via KnownFields.
	badUnknownKey := []byte(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/bad"
  name: "Bad"
  version: "0.1.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"
runtime:
  tier: "declarative"
permissions:
  secretUnsealBackdoor: true
  networkEgress:
    allowedHosts: ["api.example.com"]
`)
	if _, err := ParseAndValidateManifest(badUnknownKey, pol, nil); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected ErrManifestInvalid on unknown permission key (CT-01), got %v", err)
	}

	// CT-01b: Wildcard egress rejected.
	wildcardYAML := makeWasmManifestYAML("@acme/wild", "0.1.0", "0.1", ">=0.1.0", "", []string{"*.example.com"}, nil, 16)
	if _, err := ParseAndValidateManifest(wildcardYAML, pol, nil); !errors.Is(err, ErrWildcardEgressForbidden) {
		t.Fatalf("expected ErrWildcardEgressForbidden (CT-01), got %v", err)
	}

	// CT-01c: Cookie auth header & scoped_secret_unseal forbidden in Tier-2 Wasm.
	cookieYAML := strings.ReplaceAll(string(makeWasmManifestYAML("@acme/cookie", "0.1.0", "0.1", ">=0.1.0", "", []string{"api.example.com"}, nil, 16)), `headerName: "Authorization"`, `headerName: "Cookie"`)
	if _, err := ParseAndValidateManifest([]byte(cookieYAML), pol, nil); !errors.Is(err, ErrProhibitedCookiePermission) {
		t.Fatalf("expected ErrProhibitedCookiePermission (DEC-004), got %v", err)
	}
	unsealYAML := strings.ReplaceAll(string(makeWasmManifestYAML("@acme/unseal", "0.1.0", "0.1", ">=0.1.0", "", []string{"api.example.com"}, nil, 16)), `mode: "host_blind_inject"`, `mode: "scoped_secret_unseal"`)
	if _, err := ParseAndValidateManifest([]byte(unsealYAML), pol, nil); !errors.Is(err, ErrProhibitedCapabilityTier) {
		t.Fatalf("expected ErrProhibitedCapabilityTier on scoped_secret_unseal in Wasm, got %v", err)
	}

	// CT-02: Unsupported contractVersion "9.0" rejected with clear supported list.
	badContract := makeWasmManifestYAML("@acme/future", "0.1.0", "9.0", ">=0.1.0", "", []string{"api.example.com"}, nil, 16)
	if _, err := ParseAndValidateManifest(badContract, pol, nil); !errors.Is(err, ErrUnsupportedContractVersion) || !strings.Contains(err.Error(), "0.1") {
		t.Fatalf("expected ErrUnsupportedContractVersion listing [0.1] (CT-02), got %v", err)
	}

	// CT-24: Incompatible coreVersionRange rejected, while N-1 contract window succeeds.
	incompatRange := makeWasmManifestYAML("@acme/incompat", "0.1.0", "0.1", ">=2.0.0 <3.0.0", "", []string{"api.example.com"}, nil, 16)
	if _, err := ParseAndValidateManifest(incompatRange, pol, nil); !errors.Is(err, ErrIncompatibleCoreVersion) {
		t.Fatalf("expected ErrIncompatibleCoreVersion (CT-24), got %v", err)
	}
	nMinusOnePolicy := ValidationPolicy{CoreVersion: "0.2.0", SupportedContractVersions: []string{"0.1", "0.2"}}
	nMinusOneManifest := makeWasmManifestYAML("@acme/nminus1", "0.1.0", "0.1", ">=0.1.0 <0.3.0", "", []string{"api.example.com"}, nil, 16)
	if _, err := ParseAndValidateManifest(nMinusOneManifest, nMinusOnePolicy, nil); err != nil {
		t.Fatalf("expected N-1 contract package to validate cleanly on Core 0.2.0: %v", err)
	}
}

func TestEndpointHostSuffixPermissionIsValidatedAndEscalates(t *testing.T) {
	validated, err := validatedEndpointHostSuffixes(map[string]any{"endpointHostSuffixes": []any{".openai.azure.com"}})
	if err != nil || len(validated) != 1 || validated[0] != ".openai.azure.com" {
		t.Fatalf("valid endpoint suffix rejected: %v %v", validated, err)
	}
	for _, invalid := range [][]any{{"*.openai.azure.com"}, {"openai.azure.com"}, {".bad..example"}, {".openai.azure.com:443"}} {
		if _, err := validatedEndpointHostSuffixes(map[string]any{"endpointHostSuffixes": invalid}); !errors.Is(err, ErrManifestInvalid) {
			t.Errorf("invalid endpoint suffix accepted: %v (err=%v)", invalid, err)
		}
	}
	approved := PermissionSet{EndpointHostSuffixes: []string{".openai.azure.com"}}
	candidate := PermissionSet{EndpointHostSuffixes: []string{".openai.azure.com", ".example.net"}}
	delta := ComputePermissionDelta(approved, candidate)
	if !delta.HasEscalation() || len(delta.AddedEndpointHostSuffixes) != 1 || delta.AddedEndpointHostSuffixes[0] != ".example.net" {
		t.Fatalf("endpoint suffix expansion should require re-consent: %+v", delta)
	}
}

func TestInstallerNEG2_CT03_CT05_CT20_IntegrityNamespaceAndOrgTrustPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Part A: CT-05 Reserved namespace spoofing (^@tanpopo(-[a-z0-9]+)?/) even with publisher signature!
	h := newInstallerHarness(t, port.MinTrustAnyApproved)
	wasmBytes := wasmfixtures.EchoTransformWASM()
	wasmSHA := shaHex(wasmBytes)

	for _, spoofID := range []string{"@tanpopo/openai", "@tanpopo-official/anthropic", "@tanpopo-labs/custom"} {
		mSpoof := makeWasmManifestYAML(spoofID, "0.1.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.openai.com"}, nil, 16)
		// Also test manifest self-declaring originTrustTier: official without root signature.
		mSpoof = []byte(strings.ReplaceAll(string(mSpoof), `license: "Apache-2.0"`, "license: \"Apache-2.0\"\n  originTrustTier: \"official\""))
		tpkgSpoof, shaSpoof := buildTPKGBytes(t, []archiveEntrySpec{
			{name: "manifest.yaml", body: mSpoof},
			{name: "dist/adapter.wasm", body: wasmBytes},
		})
		pubSig := ed25519.Sign(h.pubPriv, SignatureSigningPayload(spoofID, "0.1.0", shaSpoof))

		_, spoofErr := h.svc.StageFreshInstall(ctx, StageInstallRequest{
			ArchiveReader: bytes.NewReader(tpkgSpoof),
			Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./spoof.tpkg"},
			Signature:     ProvenanceSignature{PublisherOrRootSig: pubSig},
		})
		if !errors.Is(spoofErr, ErrReservedNamespaceSpoof) {
			t.Fatalf("spoofID %q MUST fail with ErrReservedNamespaceSpoof (CT-05), got %v", spoofID, spoofErr)
		}
	}

	// Valid @tanpopo/openai signed with RootPublicKey succeeds as official.
	mValidRoot := makeWasmManifestYAML("@tanpopo/openai", "0.1.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.openai.com"}, nil, 16)
	tpkgRoot, shaRoot := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mValidRoot},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	rootSig := ed25519.Sign(h.rootPriv, SignatureSigningPayload("@tanpopo/openai", "0.1.0", shaRoot))
	resRoot, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgRoot),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./official.tpkg"},
		Signature:     ProvenanceSignature{PublisherOrRootSig: rootSig},
	})
	if err != nil || resRoot.Provenance.OriginTrustTier != lockfile.TrustTierOfficial {
		t.Fatalf("expected root-signed @tanpopo/openai -> official, got res=%+v err=%v", resRoot, err)
	}
	actRoot, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      resRoot.Installation.InstallationID,
		ExpectedVersion:     resRoot.Installation.Version,
		ApproverPrincipalID: h.secRevID,
		ActivateImmediately: true,
		OrgID:               h.orgID,
	})
	if err != nil {
		t.Fatalf("ApproveAndActivate root: %v", err)
	}

	// CT-03: Mutate 1 byte of locked bundle -> ErrDigestMismatch + security AuditEvent!
	mutatedTPKG := append([]byte(nil), tpkgRoot...)
	mutatedTPKG[len(mutatedTPKG)/2] ^= 0x01
	if derr := h.svc.VerifyPinnedArchiveOnLoad(ctx, "@tanpopo/openai", shaHex(mutatedTPKG), "runtime"); !errors.Is(derr, lockfile.ErrDigestMismatch) {
		t.Fatalf("expected lockfile.ErrDigestMismatch on 1-byte archive tamper (CT-03), got %v", derr)
	}
	if !h.audit.hasEventType("extension.artifact_digest_mismatch") {
		t.Fatalf("expected security AuditEvent on digest mismatch (CT-03)")
	}

	// Part B: CT-20 Org minimum trust policy enforcement (OFFICIAL_ONLY & VERIFIED_ONLY block unverified_external).
	hVerified := newInstallerHarness(t, port.MinTrustVerifiedOnly)
	mExt := makeWasmManifestYAML("@external/untrusted", "0.1.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com"}, nil, 16)
	tpkgExt, shaExt := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mExt},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	// Install globally (without TargetOrgID), then verify SetOrgExtensionPolicy blocks Org enablement!
	stagedExt, err := hVerified.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgExt),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./unverified.tpkg"},
	})
	if err != nil {
		t.Fatalf("global stage unverified_external: %v", err)
	}
	if perr := hVerified.svc.SetOrgExtensionPolicy(ctx, hVerified.orgID, stagedExt.Installation.PackageID, true, nil, hVerified.secRevID); !errors.Is(perr, ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected ErrOrgTrustPolicyDenied for unverified_external under VERIFIED_ONLY (CT-20), got %v", perr)
	}
	// Also test TargetOrgID direct rejection at StageFreshInstall time, plus enterprise_signed bypass under VERIFIED_OR_ENTERPRISE_SIGNED!
	if _, directErr := hVerified.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgExt),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./unverified2.tpkg"},
		TargetOrgID:   hVerified.orgID,
	}); !errors.Is(directErr, ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected ErrOrgTrustPolicyDenied at stage time for VERIFIED_ONLY org, got %v", directErr)
	}

	// Under VERIFIED_OR_ENTERPRISE_SIGNED, enterprise-signed unverified_external package succeeds!
	hEnt := newInstallerHarness(t, port.MinTrustVerifiedOrEnterpriseSigned)
	entSig := ed25519.Sign(hEnt.entPriv, SignatureSigningPayload("@external/untrusted", "0.1.0", shaExt))
	stagedEnt, err := hEnt.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgExt),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./ent.tpkg"},
		Signature:     ProvenanceSignature{EnterpriseSig: entSig},
		TargetOrgID:   hEnt.orgID,
	})
	if err != nil {
		t.Fatalf("expected enterprise-signed package admitted under VERIFIED_OR_ENTERPRISE_SIGNED, got %v", err)
	}
	if err := hEnt.svc.SetOrgExtensionPolicy(ctx, hEnt.orgID, stagedEnt.Installation.PackageID, true, &stagedEnt.Installation.InstallationID, hEnt.secRevID); err != nil {
		t.Fatalf("expected SetOrgExtensionPolicy success for enterprise-signed package: %v", err)
	}
	_ = actRoot
}

func TestInstallerNEG3_CT04_ArchiveTraversalSymlinkEntryCapBombAndSafeRemove(t *testing.T) {
	t.Parallel()
	pluginsDir := filepath.Join(t.TempDir(), "plugins")

	// 1. Path traversal entries (../, backslash, absolute, Windows drive).
	for _, badPath := range []string{"../../etc/passwd", `..\..\windows\win.ini`, "/etc/shadow", "C:/evil.txt"} {
		badArchive, _ := buildTPKGBytes(t, []archiveEntrySpec{
			{name: "manifest.yaml", body: []byte("apiVersion: tanpopo.dev/extension/v1alpha1\n")},
			{name: badPath, body: []byte("owned")},
		})
		if _, err := ExtractSafeArchive(bytes.NewReader(badArchive), pluginsDir); !errors.Is(err, ErrArchivePathTraversal) {
			t.Fatalf("path %q expected ErrArchivePathTraversal (CT-04), got %v", badPath, err)
		}
	}

	// 2. Symlink and hardlink entries rejected.
	for _, tf := range []byte{tar.TypeSymlink, tar.TypeLink} {
		symArchive, _ := buildTPKGBytes(t, []archiveEntrySpec{
			{name: "manifest.yaml", body: []byte("apiVersion: tanpopo.dev/extension/v1alpha1\n")},
			{name: "escape", typeflag: tf, linkname: "/etc/passwd"},
		})
		if _, err := ExtractSafeArchive(bytes.NewReader(symArchive), pluginsDir); !errors.Is(err, ErrProhibitedArchiveEntry) {
			t.Fatalf("tar type %q expected ErrProhibitedArchiveEntry (CT-04), got %v", string(tf), err)
		}
	}

	// 3. Entry cap (> 256) rejected.
	var many []archiveEntrySpec
	many = append(many, archiveEntrySpec{name: "manifest.yaml", body: []byte("ok\n")})
	for i := 0; i < MaxArchiveEntries; i++ {
		many = append(many, archiveEntrySpec{name: fmt.Sprintf("f_%03d.txt", i), body: []byte("x")})
	}
	manyArchive, _ := buildTPKGBytes(t, many)
	if _, err := ExtractSafeArchive(bytes.NewReader(manyArchive), pluginsDir); !errors.Is(err, ErrTooManyArchiveEntries) {
		t.Fatalf("expected ErrTooManyArchiveEntries (>256), got %v", err)
	}

	// 4. Compression ratio / size bomb (~20 MiB zeros compresses to < 30 KiB -> >> 50:1 ratio & aborted cleanly!).
	var bombBuf bytes.Buffer
	gw := gzip.NewWriter(&bombBuf)
	tw := tar.NewWriter(gw)
	hugeSize := int64(20 << 20) // 20 MiB uncompressed zeros compresses to ~20 KiB -> > 500:1 ratio!
	_ = tw.WriteHeader(&tar.Header{Name: "bomb.bin", Typeflag: tar.TypeReg, Mode: 0o644, Size: hugeSize})
	zeros := make([]byte, 1<<20)
	for i := 0; i < 20; i++ {
		_, _ = tw.Write(zeros)
	}
	_ = tw.Close()
	_ = gw.Close()
	if _, err := ExtractSafeArchive(bytes.NewReader(bombBuf.Bytes()), pluginsDir); !errors.Is(err, ErrArchiveBomb) {
		t.Fatalf("expected ErrArchiveBomb on high-ratio bomb stream (CT-04), got %v", err)
	}

	// Confirm 0 escaped files left inside pluginsDir after rejections!
	leftover, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatalf("ReadDir(pluginsDir): %v", err)
	}
	if len(leftover) != 0 {
		t.Fatalf("expected 0 leftover staging directories after aborted extractions, got %d", len(leftover))
	}

	// 5. SafeRemovePackageDir rejects removal outside pluginsDir or via symlinks.
	outsideDir := filepath.Join(filepath.Dir(pluginsDir), "outside-precious")
	if err := os.MkdirAll(outsideDir, 0o700); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := SafeRemovePackageDir(pluginsDir, outsideDir); !errors.Is(err, ErrPathNotConfined) {
		t.Fatalf("expected ErrPathNotConfined removing directory outside pluginsDir, got %v", err)
	}
	if err := SafeRemovePackageDir(pluginsDir, pluginsDir); !errors.Is(err, ErrPathNotConfined) {
		t.Fatalf("expected ErrPathNotConfined removing pluginsDir root itself, got %v", err)
	}
	if runtime.GOOS != "windows" {
		symDir := filepath.Join(pluginsDir, "sym-escape")
		if err := os.Symlink(outsideDir, symDir); err == nil {
			if rerr := SafeRemovePackageDir(pluginsDir, symDir); !errors.Is(rerr, ErrPathNotConfined) {
				t.Fatalf("expected ErrPathNotConfined on symlink target, got %v", rerr)
			}
		}
	}
}

func TestInstallerNEG4_CT18_CT25_EscalationGuardAndGitPinning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newInstallerHarness(t, port.MinTrustAnyApproved)

	// CT-25: Mutable git refs (main, latest, v1.0.0) & unpinned OCI / URL rejected with 422.
	for _, ref := range []string{"", "main", "latest", "v1.0.0", "HEAD", "12345"} {
		if _, err := ValidateSourceRef(SourceSpec{
			Kind: SourceKindGit,
			URI:  "https://github.com/acme/ext.git",
			Ref:  ref,
		}); !errors.Is(err, ErrUnpinnedGitReference) {
			t.Fatalf("git ref %q expected ErrUnpinnedGitReference (CT-25), got %v", ref, err)
		}
	}
	if _, err := ValidateSourceRef(SourceSpec{
		Kind: SourceKindOCI,
		URI:  "oci://registry.internal/ext/acme:latest",
	}); !errors.Is(err, ErrUnpinnedOCIDigest) {
		t.Fatalf("mutable OCI tag :latest expected ErrUnpinnedOCIDigest, got %v", err)
	}
	if _, err := ValidateSourceRef(SourceSpec{
		Kind: SourceKindURL,
		URI:  "https://packages.example.com/acme.tpkg",
	}); !errors.Is(err, ErrUnpinnedReference) {
		t.Fatalf("URL without ExpectedSHA256 expected ErrUnpinnedReference, got %v", err)
	}

	// CT-18: Install v1.0.0 Declarative -> upgrade silently adding allowedHost/secretSlot/Wasm parks in UPDATING_PENDING_RECONSENT.
	mV1 := []byte(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@acme/decl"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"
permissions:
  networkEgress:
    allowedHosts: ["api.acme.example"]
    allowPrivateIp: false
    requireTls: true
`)
	tpkgV1, _ := buildTPKGBytes(t, []archiveEntrySpec{{name: "manifest.yaml", body: mV1}})
	staged, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgV1),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./v1.tpkg"},
	})
	if err != nil {
		t.Fatalf("StageFreshInstall v1: %v", err)
	}
	active, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: h.secRevID,
		ActivateImmediately: true,
		OrgID:               h.orgID,
	})
	if err != nil {
		t.Fatalf("ApproveAndActivate v1: %v", err)
	}
	origActiveID := *active.ActiveVersionID

	// Candidate v1.1.0 silently adds evil.exfil.example -> DeltaP != empty -> UPDATING_PENDING_RECONSENT while v1.0.0 stays live!
	mV11 := []byte(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@acme/decl"
  version: "1.1.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"
permissions:
  networkEgress:
    allowedHosts: ["api.acme.example", "extra.newdomain.example"]
    allowPrivateIp: false
    requireTls: true
`)
	tpkgV11, _ := buildTPKGBytes(t, []archiveEntrySpec{{name: "manifest.yaml", body: mV11}})
	upRes, err := h.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  active.InstallationID,
		ExpectedVersion: active.Version,
		ArchiveReader:   bytes.NewReader(tpkgV11),
		Source:          SourceSpec{Kind: SourceKindLocalArchive, URI: "./v1.1.tpkg"},
		ActorID:         h.secRevID,
		OrgID:           h.orgID,
	})
	if err != nil {
		t.Fatalf("StageUpgrade v1.1: %v", err)
	}
	if !upRes.Escalated || len(upRes.Delta.AddedHosts) != 1 || upRes.Delta.AddedHosts[0] != "extra.newdomain.example" {
		t.Fatalf("expected DeltaP host escalation detected, got %+v", upRes.Delta)
	}
	if upRes.Installation.State != StateUpdatingPendingReconsent {
		t.Fatalf("expected state UPDATING_PENDING_RECONSENT, got %s", upRes.Installation.State)
	}
	if upRes.Installation.ActiveVersionID == nil || *upRes.Installation.ActiveVersionID != origActiveID {
		t.Fatalf("v1.0.0 MUST continue serving live traffic uninterrupted during re-consent wait!")
	}

	// Reject escalated upgrade -> returns cleanly to ENABLED_ACTIVE on v1.0.0!
	reverted, err := h.svc.RejectEscalatedUpgrade(ctx, upRes.Installation.InstallationID, upRes.Installation.Version, h.secRevID, h.orgID, "req_reject")
	if err != nil || reverted.State != StateEnabledActive || *reverted.ActiveSemver != "1.0.0" {
		t.Fatalf("expected RejectEscalatedUpgrade to keep 1.0.0 live, got %+v err=%v", reverted, err)
	}

	// Stage escalating Wasm tier upgrade + approve with SECURITY_REVIEWER re-consent.
	wasmBytes := wasmfixtures.EchoTransformWASM()
	mV2 := makeWasmManifestYAML("@acme/decl", "2.0.0", "0.1", ">=0.1.0", shaHex(wasmBytes), []string{"api.acme.example", "extra.newdomain.example"}, []string{"new_slot"}, 16)
	tpkgV2, _ := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mV2},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	upV2, err := h.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  reverted.InstallationID,
		ExpectedVersion: reverted.Version,
		ArchiveReader:   bytes.NewReader(tpkgV2),
		Source:          SourceSpec{Kind: SourceKindLocalArchive, URI: "./v2.tpkg"},
		ActorID:         h.secRevID,
		OrgID:           h.orgID,
	})
	if err != nil || !upV2.Escalated || !upV2.Delta.TierEscalated || len(upV2.Delta.AddedSecretSlots) != 1 {
		t.Fatalf("expected tier + secretSlot escalation detected on v2, got %+v err=%v", upV2, err)
	}
	approvedV2, err := h.svc.ApproveEscalatedUpgrade(ctx, upV2.Installation.InstallationID, upV2.Installation.Version, h.secRevID, h.orgID, "req_reconsent")
	if err != nil || approvedV2.State != StateEnabledActive || *approvedV2.ActiveSemver != "2.0.0" {
		t.Fatalf("ApproveEscalatedUpgrade v2 failed: %+v err=%v", approvedV2, err)
	}
}

func TestInstallerRevocationYAMLBombsSemverRangesAndEdgeCases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. YAML Depth (> 32) and Alias Bomb guards (FZ-02).
	var deepYAML strings.Builder
	deepYAML.WriteString("apiVersion: tanpopo.dev/extension/v1alpha1\nkind: ExtensionManifest\n")
	for i := 0; i < 40; i++ {
		deepYAML.WriteString(strings.Repeat(" ", i*2) + "n:\n")
	}
	if _, err := ParseAndValidateManifest([]byte(deepYAML.String()), DefaultValidationPolicy(), nil); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected YAML depth bomb rejected with ErrManifestInvalid, got %v", err)
	}

	var aliasBomb strings.Builder
	aliasBomb.WriteString("apiVersion: tanpopo.dev/extension/v1alpha1\nbase: &b\n  k: 1\nitems:\n")
	for i := 0; i < 80; i++ {
		aliasBomb.WriteString("  - *b\n")
	}
	if _, err := ParseAndValidateManifest([]byte(aliasBomb.String()), DefaultValidationPolicy(), nil); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected YAML alias bomb rejected with ErrManifestInvalid, got %v", err)
	}

	// 2. SemverMatchesRange ^, ~, ||, <=, >=, prerelease checks.
	for _, tc := range []struct {
		ver   string
		expr  string
		match bool
	}{
		{"0.1.0", "^0.1.0", true},
		{"0.1.9", "^0.1.0", true},
		{"0.2.0", "^0.1.0", false},
		{"1.2.3", "~1.2.0", true},
		{"1.3.0", "~1.2.0", false},
		{"0.1.5", ">=0.1.0 <=0.1.5", true},
		{"0.2.0", "<0.1.0 || >=0.2.0", true},
		{"0.1.0-alpha.1", ">=0.1.0", false},
	} {
		ok, err := SemverMatchesRange(tc.ver, tc.expr)
		if err != nil || ok != tc.match {
			t.Fatalf("SemverMatchesRange(%q, %q): got ok=%v err=%v, want %v", tc.ver, tc.expr, ok, err, tc.match)
		}
	}

	// 3. All DeltaP escalation dimensions covered.
	base := PermissionSet{
		AllowedHosts:   []string{"api.a.example"},
		AllowPrivateIP: false,
		RequireTLS:     true,
		ExecutionTier:  ExecutionTierDeclarativeT1,
		SecretSlots:    []string{"s1"},
		Hooks:          []string{"provider:transform_request"},
		MaxMemoryMib:   16,
	}
	if d := ComputePermissionDelta(base, PermissionSet{
		AllowedHosts:        []string{"api.a.example"},
		AllowPrivateIP:      true,
		RequireTLS:          false,
		ExecutionTier:       ExecutionTierWasmSandboxT2,
		SecretSlots:         []string{"s1", "s2"},
		Hooks:               []string{"provider:transform_request", "provider:extract_usage"},
		MaxMemoryMib:        32,
		InspectRequestBody:  true,
		InspectResponseBody: true,
	}); !d.PrivateIPEscalated || !d.TLSDowngraded || !d.TierEscalated || !d.MemoryIncreased || !d.PayloadAccessEscalated || len(d.AddedSecretSlots) != 1 || len(d.AddedHooks) != 1 {
		t.Fatalf("expected all DeltaP flags triggered, got %+v", d)
	}

	// 4. CRITICAL_MALICIOUS revocation feed entry blocks StageFreshInstall & OFFICIAL_ONLY rejects verified publisher.
	hOfficial := newInstallerHarness(t, port.MinTrustOfficialOnly)
	wasmBytes := wasmfixtures.EchoTransformWASM()
	wasmSHA := shaHex(wasmBytes)
	mPub := makeWasmManifestYAML("@partner/verified-ext", "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.partner.example"}, nil, 16)
	tpkgPub, shaPub := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mPub},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	pubSig := ed25519.Sign(hOfficial.pubPriv, SignatureSigningPayload("@partner/verified-ext", "1.0.0", shaPub))

	// Verified publisher denied under OFFICIAL_ONLY org trust policy (CT-20).
	if _, err := hOfficial.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgPub),
		Source:        SourceSpec{Kind: SourceKindOCI, URI: "oci://reg.example.com/partner/ext@sha256:" + shaPub},
		Signature:     ProvenanceSignature{PublisherOrRootSig: pubSig},
		TargetOrgID:   hOfficial.orgID,
	}); !errors.Is(err, ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected ErrOrgTrustPolicyDenied for verified package in OFFICIAL_ONLY org, got %v", err)
	}

	// Insert CRITICAL_MALICIOUS revocation entry -> StageFreshInstall blocked!
	if _, err := hOfficial.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_revocations (id, package_slug, version_or_sha256, severity, version_serial, reason, feed_expires_at_ms, imported_at_ms)
		VALUES ('rev_01test', '@partner/verified-ext', ?, 'CRITICAL_MALICIOUS', 1, 'compromised upstream key', 9999999999999, 1000)`,
		shaPub,
	); err != nil {
		t.Fatalf("insert revocation: %v", err)
	}
	if _, err := hOfficial.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgPub),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./revoked.tpkg"},
		Signature:     ProvenanceSignature{PublisherOrRootSig: pubSig},
	}); !errors.Is(err, ErrRevokedExtension) {
		t.Fatalf("expected ErrRevokedExtension for revoked SHA256, got %v", err)
	}

	// 5. Missing entrypoint / sha256 mismatch inside archive rejected cleanly.
	mBadEP := makeWasmManifestYAML("@acme/badep", "1.0.0", "0.1", ">=0.1.0", strings.Repeat("b", 64), []string{"api.acme.example"}, nil, 16)
	badEPArchive, _ := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mBadEP},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	if _, err := hOfficial.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(badEPArchive),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./badep.tpkg"},
	}); !errors.Is(err, ErrEntrypointMismatch) {
		t.Fatalf("expected ErrEntrypointMismatch on wrong entrypoint sha256, got %v", err)
	}
}

func TestInstallerConcurrentOperationsRaceSafe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newInstallerHarness(t, port.MinTrustAnyApproved)
	wasmBytes := wasmfixtures.EchoTransformWASM()
	wasmSHA := shaHex(wasmBytes)

	const n = 4
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pkgName := fmt.Sprintf("@concurrent/pkg-%d", idx)
			m := makeWasmManifestYAML(pkgName, "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com"}, []string{"api_key"}, 16)
			raw, _ := buildTPKGBytes(t, []archiveEntrySpec{
				{name: "manifest.yaml", body: m},
				{name: "dist/adapter.wasm", body: wasmBytes},
			})
			stRes, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
				ArchiveReader: bytes.NewReader(raw),
				Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: fmt.Sprintf("./pkg-%d.tpkg", idx)},
			})
			if err != nil {
				errs <- err
				return
			}
			if _, aerr := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
				InstallationID:      stRes.Installation.InstallationID,
				ExpectedVersion:     stRes.Installation.Version,
				ApproverPrincipalID: h.secRevID,
				ActivateImmediately: true,
				OrgID:               h.orgID,
			}); aerr != nil {
				errs <- aerr
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent installer error: %v", err)
	}

	// Concurrent same-installation mutation vs other-package uninstall must stay lockfile-safe & optimistic-lock safe.
	lf, rerr := lockfile.Read(h.lockPath)
	if rerr != nil || len(lf.Extensions) != n {
		t.Fatalf("expected all %d concurrent packages preserved in tanpopo.lock, got len=%d err=%v", n, len(lf.Extensions), rerr)
	}
}

func TestInstallerSecurityRemediations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pluginsDir := filepath.Join(t.TempDir(), "plugins")

	// 1. Exponential YAML anchor DAG bomb (<= 64 aliases, depth 20 -> 2^20 nodes) caught prior to decode!
	var dagYAML strings.Builder
	dagYAML.WriteString("apiVersion: tanpopo.dev/extension/v1alpha1\nkind: ExtensionManifest\n")
	dagYAML.WriteString("metadata:\n  id: \"@acme/dag\"\n  name: \"DAG\"\n  version: \"0.1.0\"\n  license: \"Apache-2.0\"\n")
	dagYAML.WriteString("compatibility:\n  contractVersion: \"0.1\"\n  coreVersionRange: \">=0.1.0\"\n")
	dagYAML.WriteString("runtime:\n  tier: \"declarative\"\npermissions:\n  networkEgress:\n    allowedHosts: [\"api.example.com\"]\nconfigSchema:\n")
	dagYAML.WriteString("  a0: &a0 [\"x\", \"y\"]\n")
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&dagYAML, "  a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	if _, err := ParseAndValidateManifest([]byte(dagYAML.String()), DefaultValidationPolicy(), nil); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected exponential YAML alias DAG rejected with ErrManifestInvalid, got %v", err)
	}

	// 2. Archive guard hardenings: multi-member concatenated gzip, post-tar trailer bomb, truncated entry,
	//    case-folded duplicate manifest, NTFS ADS ':', out-of-order nested tar.TypeDir, symlink archive source.
	part1, _ := buildTPKGBytes(t, []archiveEntrySpec{{name: "manifest.yaml", body: []byte("k: 1\n")}})
	part2, _ := buildTPKGBytes(t, []archiveEntrySpec{{name: "evil.txt", body: []byte("hidden")}})
	concatenated := append(append([]byte(nil), part1...), part2...)
	if _, err := ExtractSafeArchive(bytes.NewReader(concatenated), pluginsDir); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("expected concatenated multi-member gzip rejected, got %v", err)
	}

	// Post-tar EOF uncompressed trailer bomb inside member 1.
	var postTarBuf bytes.Buffer
	pgw := gzip.NewWriter(&postTarBuf)
	ptw := tar.NewWriter(pgw)
	_ = ptw.WriteHeader(&tar.Header{Name: "manifest.yaml", Typeflag: tar.TypeReg, Mode: 0o644, Size: 5})
	_, _ = ptw.Write([]byte("k: 1\n"))
	_ = ptw.Close()
	_, _ = pgw.Write(bytes.Repeat([]byte{0}, 10<<20))
	_ = pgw.Close()
	if _, err := ExtractSafeArchive(bytes.NewReader(postTarBuf.Bytes()), pluginsDir); !errors.Is(err, ErrArchiveBomb) {
		t.Fatalf("expected post-tar EOF trailer bomb rejected with ErrArchiveBomb, got %v", err)
	}

	// Case-folded duplicate collision ("manifest.yaml" + "Manifest.yaml").
	caseDup, _ := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: []byte("k: 1\n")},
		{name: "Manifest.yaml", body: []byte("k: 2\n")},
	})
	if _, err := ExtractSafeArchive(bytes.NewReader(caseDup), pluginsDir); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("expected case-folded duplicate archive collision rejected, got %v", err)
	}

	// Truncated tar entry payload.
	var truncRaw bytes.Buffer
	tgw := gzip.NewWriter(&truncRaw)
	ttw := tar.NewWriter(tgw)
	_ = ttw.WriteHeader(&tar.Header{Name: "manifest.yaml", Typeflag: tar.TypeReg, Mode: 0o644, Size: 100})
	_, _ = ttw.Write([]byte("short"))
	// Close gzip immediately without finishing tar entry padding/EOF -> truncated!
	_ = tgw.Close()
	if _, err := ExtractSafeArchive(bytes.NewReader(truncRaw.Bytes()), pluginsDir); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("expected truncated tar entry rejected with ErrInvalidArchive, got %v", err)
	}

	// Windows NTFS ADS ':' and reserved device name rejected.
	for _, bad := range []string{"foo:Zone.Identifier", "dist/CON.txt"} {
		badADS, _ := buildTPKGBytes(t, []archiveEntrySpec{
			{name: "manifest.yaml", body: []byte("k: 1\n")},
			{name: bad, body: []byte("x")},
		})
		if _, err := ExtractSafeArchive(bytes.NewReader(badADS), pluginsDir); !errors.Is(err, ErrArchivePathTraversal) {
			t.Fatalf("entry %q expected ErrArchivePathTraversal, got %v", bad, err)
		}
	}

	// Out-of-order nested tar.TypeDir (dist/sub/ before dist/) unpacks cleanly inside confinement!
	nestedDirTPKG, _ := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "dist/sub/", typeflag: tar.TypeDir},
		{name: "dist/sub/a.txt", body: []byte("ok")},
		{name: "manifest.yaml", body: []byte("k: 1\n")},
	})
	extNested, nerr := ExtractSafeArchive(bytes.NewReader(nestedDirTPKG), pluginsDir)
	if nerr != nil {
		t.Fatalf("expected out-of-order nested TypeDir to succeed, got %v", nerr)
	}
	_ = SafeRemovePackageDir(pluginsDir, extNested.StagingDir)

	if runtime.GOOS != "windows" {
		tmpSrc := t.TempDir()
		realArch := filepath.Join(tmpSrc, "real.tpkg")
		_ = os.WriteFile(realArch, part1, 0o600)
		symArch := filepath.Join(tmpSrc, "sym.tpkg")
		if err := os.Symlink(realArch, symArch); err == nil {
			if _, serr := ExtractSafeArchiveFromFile(symArch, pluginsDir); !errors.Is(serr, ErrPathNotConfined) {
				t.Fatalf("expected ErrPathNotConfined on symlink archive source, got %v", serr)
			}
		}
	}

	// 3. Manifest & URI edge validations: bad allowedHosts, hook case-fold, adapter script, bad git creds / oci.
	for _, badHost := range []string{"api.example.com:bad:extra", "[::1", "api.example.com.", "api.example.com#frag", "αpi.example.com"} {
		y := makeWasmManifestYAML("@acme/h", "0.1.0", "0.1", ">=0.1.0", "", []string{badHost}, nil, 16)
		if _, err := ParseAndValidateManifest(y, DefaultValidationPolicy(), nil); !errors.Is(err, ErrManifestInvalid) {
			t.Fatalf("bad host %q expected ErrManifestInvalid, got %v", badHost, err)
		}
	}
	hookUpper := strings.ReplaceAll(string(makeWasmManifestYAML("@acme/hook", "0.1.0", "0.1", ">=0.1.0", "", []string{"api.example.com"}, nil, 16)), `"provider:transform_request"`, `"SCOPED_SECRET_UNSEAL"`)
	if _, err := ParseAndValidateManifest([]byte(hookUpper), DefaultValidationPolicy(), nil); !errors.Is(err, ErrProhibitedCapabilityTier) {
		t.Fatalf("expected uppercase SCOPED_SECRET_UNSEAL hook blocked in Tier-2 Wasm, got %v", err)
	}
	scriptManifest := []byte(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@acme/script"
  version: "0.1.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
permissions:
  networkEgress:
    allowedHosts: ["api.example.com"]
adapter:
  postinstall: "rm -rf /"
`)
	if _, err := ParseAndValidateManifest(scriptManifest, DefaultValidationPolicy(), nil); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected adapter.postinstall rejected with ErrManifestInvalid, got %v", err)
	}
	if _, err := ValidateSourceRef(SourceSpec{Kind: SourceKindGit, URI: "git+https://user:secret@github.com/acme/repo.git", Ref: "0123456789abcdef0123456789abcdef01234567"}); !errors.Is(err, ErrUnpinnedGitReference) {
		t.Fatalf("expected git embedded credentials rejected, got %v", err)
	}
	if _, err := ValidateSourceRef(SourceSpec{Kind: SourceKindOCI, URI: "@sha256:" + strings.Repeat("a", 64)}); !errors.Is(err, ErrUnpinnedOCIDigest) {
		t.Fatalf("expected bare @sha256 without oci://host/repo rejected, got %v", err)
	}

	// 4. Lifecycle RBAC, Suspended principal, Global Operator boundary, Revoked Rollback block,
	//    CT-20 Upgrade Enterprise-Sig Strip block, SecretBindingModes DeltaP, Clean Re-install after UNINSTALLED.
	h := newInstallerHarness(t, port.MinTrustVerifiedOrEnterpriseSigned)
	wasmBytes := wasmfixtures.EchoTransformWASM()
	wasmSHA := shaHex(wasmBytes)
	m1 := makeWasmManifestYAML("@ext/rbac", "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com", "extra.example.com"}, []string{"k1"}, 16)
	tpkg1, sha1 := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: m1},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	sigEnt1 := ed25519.Sign(h.entPriv, SignatureSigningPayload("@ext/rbac", "1.0.0", sha1))

	st1, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkg1),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./rbac-v1.tpkg"},
		Signature:     ProvenanceSignature{EnterpriseSig: sigEnt1},
		ActorID:       h.devID,
		TargetOrgID:   h.orgID,
	})
	if err != nil {
		t.Fatalf("StageFreshInstall: %v", err)
	}
	// Org-scoped secRevID CANNOT approve instance-global (OrgID == "") operation!
	if _, aerr := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      st1.Installation.InstallationID,
		ExpectedVersion:     st1.Installation.Version,
		ApproverPrincipalID: h.secRevID,
		ActivateImmediately: true,
		OrgID:               "",
	}); !errors.Is(aerr, ErrPermissionDenied) {
		t.Fatalf("expected ORG SECURITY_REVIEWER blocked from instance-global approval (orgID==\"\"), got %v", aerr)
	}
	// Instance OPERATOR approves cleanly even with OrgID == ""!
	act1, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      st1.Installation.InstallationID,
		ExpectedVersion:     st1.Installation.Version,
		ApproverPrincipalID: h.opID,
		ActivateImmediately: true,
		OrgID:               "",
	})
	if err != nil {
		t.Fatalf("ApproveAndActivate by INSTANCE OPERATOR: %v", err)
	}
	if err := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, act1.PackageID, true, &act1.InstallationID, h.secRevID); err != nil {
		t.Fatalf("SetOrgExtensionPolicy: %v", err)
	}
	// Note AuditEntryPoint must be schema-valid "admin_api"!
	for _, ev := range h.audit.events {
		if ev.EntryPoint != "admin_api" {
			t.Fatalf("expected schema-valid EntryPoint=admin_api, got %q", ev.EntryPoint)
		}
	}

	// CT-20 upgrade check: unsigned v1.1.0 cannot strip enterprise_signed while VERIFIED_OR_ENTERPRISE_SIGNED org has policy enabled!
	mUnsigned := makeWasmManifestYAML("@ext/rbac", "1.1.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com"}, []string{"k1"}, 16)
	tpkgUnsigned, _ := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mUnsigned},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	if _, uerr := h.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  act1.InstallationID,
		ExpectedVersion: act1.Version,
		ArchiveReader:   bytes.NewReader(tpkgUnsigned),
		Source:          SourceSpec{Kind: SourceKindLocalArchive, URI: "./rbac-v11-unsigned.tpkg"},
		ActorID:         h.opID,
	}); !errors.Is(uerr, ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected unsigned upgrade blocked by enabled VERIFIED_OR_ENTERPRISE_SIGNED org policy, got %v", uerr)
	}

	// Signed narrowed v1.1.0 (dropped extra.example.com) cuts over and updates approved_permissions_json!
	_, shaU1 := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mUnsigned},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	sigEntU1 := ed25519.Sign(h.entPriv, SignatureSigningPayload("@ext/rbac", "1.1.0", shaU1))
	act11Res, err := h.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  act1.InstallationID,
		ExpectedVersion: act1.Version,
		ArchiveReader:   bytes.NewReader(tpkgUnsigned),
		Source:          SourceSpec{Kind: SourceKindLocalArchive, URI: "./rbac-v11.tpkg"},
		Signature:       ProvenanceSignature{EnterpriseSig: sigEntU1},
		ActorID:         h.opID,
	})
	if err != nil || act11Res.Escalated || len(act11Res.Installation.ApprovedPermissions.AllowedHosts) != 1 {
		t.Fatalf("expected non-escalating narrowed upgrade updating approved permissions, got res=%+v err=%v", act11Res, err)
	}

	// Re-adding extra.example.com in v1.2.0 must now trigger UPDATING_PENDING_RECONSENT (not bypass via stale JSON)!
	m12 := makeWasmManifestYAML("@ext/rbac", "1.2.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com", "extra.example.com"}, []string{"k1"}, 16)
	tpkg12, sha12 := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: m12},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	sigEnt12 := ed25519.Sign(h.entPriv, SignatureSigningPayload("@ext/rbac", "1.2.0", sha12))
	esc12, err := h.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  act11Res.Installation.InstallationID,
		ExpectedVersion: act11Res.Installation.Version,
		ArchiveReader:   bytes.NewReader(tpkg12),
		Source:          SourceSpec{Kind: SourceKindLocalArchive, URI: "./rbac-v12.tpkg"},
		Signature:       ProvenanceSignature{EnterpriseSig: sigEnt12},
		ActorID:         h.secRevID,
		OrgID:           h.orgID,
	})
	if err != nil || !esc12.Escalated {
		t.Fatalf("expected re-added host after narrowed upgrade to require re-consent, got res=%+v err=%v", esc12, err)
	}

	// Suspended approver cannot ApproveEscalatedUpgrade!
	_, _ = h.st.RawDB().ExecContext(ctx, `UPDATE principals SET status = 'SUSPENDED' WHERE id = ?`, h.secRevID)
	if _, serr := h.svc.ApproveEscalatedUpgrade(ctx, esc12.Installation.InstallationID, esc12.Installation.Version, h.secRevID, h.orgID, "req_susp"); !errors.Is(serr, ErrPermissionDenied) {
		t.Fatalf("expected SUSPENDED approver rejected with ErrPermissionDenied, got %v", serr)
	}
	_, _ = h.st.RawDB().ExecContext(ctx, `UPDATE principals SET status = 'ACTIVE' WHERE id = ?`, h.secRevID)
	// Reject escalated upgrade using active secRevID, then mark previous v1.0.0 CRITICAL_MALICIOUS and verify RollbackInstallation blocks!
	rej12, err := h.svc.RejectEscalatedUpgrade(ctx, esc12.Installation.InstallationID, esc12.Installation.Version, h.secRevID, h.orgID, "req_rej")
	if err != nil {
		t.Fatalf("RejectEscalatedUpgrade: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_revocations (id, package_slug, version_or_sha256, severity, version_serial, reason, feed_expires_at_ms, imported_at_ms)
		VALUES ('rev_rb1', '@ext/rbac', '1.0.0', 'CRITICAL_MALICIOUS', 1, 'bad prior release', 9999999999999, 1000)`,
	); err != nil {
		t.Fatalf("insert revocation: %v", err)
	}
	if _, rerr := h.svc.RollbackInstallation(ctx, rej12.InstallationID, rej12.Version, h.opID, "", "req_rb_blocked"); !errors.Is(rerr, ErrRevokedExtension) {
		t.Fatalf("expected RollbackInstallation blocked onto CRITICAL_MALICIOUS v1.0.0, got %v", rerr)
	}
	if !h.audit.hasEventType("extension.rollback_blocked_revoked") {
		t.Fatalf("expected extension.rollback_blocked_revoked AuditEvent")
	}

	// Quarantine + Disable Org policy + Uninstall + Clean Re-install!
	if err := h.svc.QuarantineInstallation(ctx, rej12.InstallationID, "test quarantine", h.opID); err != nil {
		t.Fatalf("QuarantineInstallation: %v", err)
	}
	// Uninstall blocked while pinned enabled in org_extension_policies!
	qRec, _ := h.svc.GetInstallation(ctx, rej12.InstallationID)
	if uerr := h.svc.UninstallInstallation(ctx, qRec.InstallationID, qRec.Version, "", h.opID, ""); !errors.Is(uerr, port.ErrResourceInUse) {
		t.Fatalf("expected ErrResourceInUse when enabled org policy pins installation, got %v", uerr)
	}
	if err := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, qRec.PackageID, false, nil, h.secRevID); err != nil {
		t.Fatalf("disable org policy: %v", err)
	}
	if err := h.svc.UninstallInstallation(ctx, qRec.InstallationID, qRec.Version, "", h.opID, ""); err != nil {
		t.Fatalf("UninstallInstallation after unpin: %v", err)
	}
	// Clean re-install of UNINSTALLED package succeeds!
	reinstalled, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkg12),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./reinstall-v12.tpkg"},
		Signature:     ProvenanceSignature{EnterpriseSig: sigEnt12},
		ActorID:       h.devID,
		TargetOrgID:   h.orgID,
	})
	if err != nil || reinstalled.Installation.State != StatePendingPermissionApproval {
		t.Fatalf("expected clean re-install after UNINSTALLED, got res=%+v err=%v", reinstalled, err)
	}

	// SecretBindingModes change in DeltaP must escalate!
	deltaMode := ComputePermissionDelta(
		PermissionSet{ExecutionTier: ExecutionTierWasmSandboxT2, SecretBindingModes: map[string]string{"k1": "host_blind_inject"}},
		PermissionSet{ExecutionTier: ExecutionTierWasmSandboxT2, SecretBindingModes: map[string]string{"k1": "host_crypto_sign"}},
	)
	if !deltaMode.BindingModeChanged || !deltaMode.HasEscalation() {
		t.Fatalf("expected SecretBindingModes change detected in DeltaP, got %+v", deltaMode)
	}
	implicitMode := ComputePermissionDelta(
		PermissionSet{ExecutionTier: ExecutionTierWasmSandboxT2, SecretSlots: []string{"k1"}, SecretBindingModes: map[string]string{"k1": "host_crypto_sign"}},
		PermissionSet{ExecutionTier: ExecutionTierWasmSandboxT2, SecretSlots: []string{"k1"}},
	)
	if !implicitMode.BindingModeChanged || !implicitMode.HasEscalation() {
		t.Fatalf("expected implicit blind injection to require reconsent, got %+v", implicitMode)
	}

	// PROJECT-scoped SECURITY_REVIEWER cannot approve global (OrgID == "") or cross-org actions!
	projSecID := idgen.MustNew(idgen.PrefixPrincipal)
	projID := idgen.MustNew(idgen.PrefixProject)
	_, _ = h.st.RawDB().ExecContext(ctx, `INSERT INTO projects (id, org_id, slug, display_name, default_env, version, created_at_ms, updated_at_ms) VALUES (?, ?, 'p1', 'P1', 'dev', 1, 1000, 1000)`, projID, h.orgID)
	_, _ = h.st.RawDB().ExecContext(ctx, `INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms) VALUES (?, ?, 'HUMAN_LOCAL', 'projsec@example.com', 'ACTIVE', 1, 1000, 1000)`, projSecID, h.orgID)
	_, _ = h.st.RawDB().ExecContext(ctx, `INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms) VALUES (?, ?, ?, 'PROJECT', ?, 'SECURITY_REVIEWER', 1, 1000)`, "rb_projsec", h.orgID, projSecID, projID)
	if _, perr := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      reinstalled.Installation.InstallationID,
		ExpectedVersion:     reinstalled.Installation.Version,
		ApproverPrincipalID: projSecID,
		ActivateImmediately: false,
		OrgID:               "",
	}); !errors.Is(perr, ErrPermissionDenied) {
		t.Fatalf("expected PROJECT-scoped SECURITY_REVIEWER denied on global install, got %v", perr)
	}

	// Stale ExpectedVersion returns ErrVersionConflict!
	if _, verr := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      reinstalled.Installation.InstallationID,
		ExpectedVersion:     999,
		ApproverPrincipalID: h.opID,
		ActivateImmediately: false,
	}); !errors.Is(verr, port.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict for stale ExpectedVersion, got %v", verr)
	}

	// ActivateImmediately=false -> INSTALLED_DISABLED; test provider_connections dependency block & clean uninstall from INSTALLED_DISABLED!
	disabledRec, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      reinstalled.Installation.InstallationID,
		ExpectedVersion:     reinstalled.Installation.Version,
		ApproverPrincipalID: h.secRevID,
		ActivateImmediately: false,
		OrgID:               h.orgID,
	})
	if err != nil || disabledRec.State != StateInstalledDisabled {
		t.Fatalf("expected INSTALLED_DISABLED, got rec=%+v err=%v", disabledRec, err)
	}
	// Real provider_adapters + active provider_connections from 0007_providers_models.sql block uninstall with ErrResourceInUse.
	if _, err := h.st.RawDB().ExecContext(ctx, `INSERT INTO provider_adapters (id, installation_id, adapter_slug, version, created_at_ms) VALUES ('adp_1', ?, 'acme', 1, 1000)`, disabledRec.InstallationID); err != nil {
		t.Fatalf("insert provider_adapters: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `INSERT INTO provider_connections (id, org_id, adapter_id, name, endpoint_base_url, data_region, billing_mode, auth_mode, state, version, created_at_ms, updated_at_ms) VALUES ('con_1', ?, 'adp_1', 'Test', 'https://api.acme.example', 'us', 'COMMERCIAL_API', 'NONE', 'ACTIVE', 1, 1000, 1000)`, h.orgID); err != nil {
		t.Fatalf("insert provider_connections: %v", err)
	}
	if derr := h.svc.UninstallInstallation(ctx, disabledRec.InstallationID, disabledRec.Version, "", h.opID, ""); !errors.Is(derr, port.ErrResourceInUse) {
		t.Fatalf("expected ErrResourceInUse when active provider_connections depend on installation, got %v", derr)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE provider_connections SET state = 'REVOKED' WHERE id = 'con_1'`); err != nil {
		t.Fatalf("revoke provider_connections: %v", err)
	}
	if derr := h.svc.UninstallInstallation(ctx, disabledRec.InstallationID, disabledRec.Version, "", h.opID, ""); derr != nil {
		t.Fatalf("expected UninstallInstallation from INSTALLED_DISABLED to succeed once connections revoked, got %v", derr)
	}

	// Corrupt approved_permissions_json in DB fails GetInstallation cleanly (never silently swallowed).
	_, _ = h.st.RawDB().ExecContext(ctx, `UPDATE extension_installations SET approved_permissions_json = '{bad-json' WHERE id = ?`, disabledRec.InstallationID)
	if _, gerr := h.svc.GetInstallation(ctx, disabledRec.InstallationID); gerr == nil {
		t.Fatalf("expected GetInstallation to fail closed on corrupt approved_permissions_json")
	}

	// Declarative adapter.auth Cookie injection & scoped_secret_unseal blocked cleanly.
	cookieAdapter := []byte(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@acme/decl-cookie"
  version: "0.1.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
permissions:
  networkEgress:
    allowedHosts: ["api.example.com"]
adapter:
  auth:
    - mode: "header"
      headerName: "Cookie"
`)
	if _, err := ParseAndValidateManifest(cookieAdapter, DefaultValidationPolicy(), nil); !errors.Is(err, ErrProhibitedCookiePermission) {
		t.Fatalf("expected ErrProhibitedCookiePermission on adapter.auth Cookie, got %v", err)
	}
	unsealAdapter := strings.ReplaceAll(strings.ReplaceAll(string(cookieAdapter), `headerName: "Cookie"`, `headerName: "Authorization"`), `mode: "header"`, `mode: "scoped_secret_unseal"`)
	if _, err := ParseAndValidateManifest([]byte(unsealAdapter), DefaultValidationPolicy(), nil); !errors.Is(err, ErrProhibitedCapabilityTier) {
		t.Fatalf("expected ErrProhibitedCapabilityTier on adapter.auth scoped_secret_unseal, got %v", err)
	}

	// ExtractSafeArchiveFromFile valid file round-trip.
	tpkgFile := filepath.Join(t.TempDir(), "valid.tpkg")
	if err := os.WriteFile(tpkgFile, part1, 0o600); err != nil {
		t.Fatalf("write tpkgFile: %v", err)
	}
	fromFileExt, ferr := ExtractSafeArchiveFromFile(tpkgFile, pluginsDir)
	if ferr != nil {
		t.Fatalf("ExtractSafeArchiveFromFile valid: %v", ferr)
	}
	_ = SafeRemovePackageDir(pluginsDir, fromFileExt.StagingDir)
}

func TestInstallerThreeAuditFindingsRepro(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. CT-20 Provenance / Trust Drift across StageUpgrade & SetOrgExtensionPolicy & tanpopo.lock.
	h1 := newInstallerHarness(t, port.MinTrustVerifiedOnly)
	wasmBytes := wasmfixtures.EchoTransformWASM()
	wasmSHA := shaHex(wasmBytes)
	mV1 := makeWasmManifestYAML("@drift/pkg", "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com"}, []string{"k1"}, 16)
	tpkgV1, shaV1 := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mV1},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	pubSigV1 := ed25519.Sign(h1.pubPriv, SignatureSigningPayload("@drift/pkg", "1.0.0", shaV1))

	st1, err := h1.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgV1),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./drift-v1.tpkg"},
		Signature:     ProvenanceSignature{PublisherOrRootSig: pubSigV1},
		ActorID:       h1.devID,
		TargetOrgID:   h1.orgID,
	})
	if err != nil {
		t.Fatalf("StageFreshInstall v1: %v", err)
	}
	act1, err := h1.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      st1.Installation.InstallationID,
		ExpectedVersion:     st1.Installation.Version,
		ApproverPrincipalID: h1.secRevID,
		ActivateImmediately: true,
		OrgID:               h1.orgID,
	})
	if err != nil {
		t.Fatalf("ApproveAndActivate v1: %v", err)
	}

	// Non-escalating upgrade to unsigned v1.1.0 (unverified_external): MUST update active trust tier to unverified_external!
	mV11 := makeWasmManifestYAML("@drift/pkg", "1.1.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com"}, []string{"k1"}, 16)
	tpkgV11, _ := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mV11},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	up11, err := h1.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  act1.InstallationID,
		ExpectedVersion: act1.Version,
		ArchiveReader:   bytes.NewReader(tpkgV11),
		Source:          SourceSpec{Kind: SourceKindLocalArchive, URI: "./drift-v11.tpkg"},
		ActorID:         h1.opID,
	})
	if err != nil {
		t.Fatalf("StageUpgrade v1.1 unsigned: %v", err)
	}
	if up11.Installation.OriginTrustTier != lockfile.TrustTierUnverifiedExternal {
		t.Fatalf("expected OriginTrustTier updated to unverified_external after unsigned upgrade, got %q", up11.Installation.OriginTrustTier)
	}
	lf1, err := lockfile.Read(h1.lockPath)
	if err != nil {
		t.Fatalf("lockfile.Read: %v", err)
	}
	if gotTier := lf1.Extensions["@drift/pkg"].TrustTier; gotTier != lockfile.TrustTierUnverifiedExternal {
		t.Fatalf("expected tanpopo.lock trustTier=unverified_external, got %q", gotTier)
	}
	if perr := h1.svc.SetOrgExtensionPolicy(ctx, h1.orgID, act1.PackageID, true, &act1.InstallationID, h1.secRevID); !errors.Is(perr, ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected VERIFIED_ONLY org denied when active version is unverified_external, got %v", perr)
	}

	// 2. CT-04 Uninstall Ownership: UninstallInstallation on package A MUST NOT delete package B's directory inside pluginsDir!
	h2 := newInstallerHarness(t, port.MinTrustAnyApproved)
	mA := makeWasmManifestYAML("@own/pkg-a", "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.a.example"}, nil, 16)
	tpkgA, _ := buildTPKGBytes(t, []archiveEntrySpec{{name: "manifest.yaml", body: mA}, {name: "dist/adapter.wasm", body: wasmBytes}})
	stA, err := h2.svc.StageFreshInstall(ctx, StageInstallRequest{ArchiveReader: bytes.NewReader(tpkgA), Source: SourceSpec{Kind: SourceKindLocalArchive, URI: "./a.tpkg"}})
	if err != nil {
		t.Fatalf("StageFreshInstall A: %v", err)
	}
	disA, err := h2.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      stA.Installation.InstallationID,
		ExpectedVersion:     stA.Installation.Version,
		ApproverPrincipalID: h2.secRevID,
		ActivateImmediately: false,
		OrgID:               h2.orgID,
	})
	if err != nil {
		t.Fatalf("Approve A: %v", err)
	}

	mB := makeWasmManifestYAML("@own/pkg-b", "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.b.example"}, nil, 16)
	tpkgB, _ := buildTPKGBytes(t, []archiveEntrySpec{{name: "manifest.yaml", body: mB}, {name: "dist/adapter.wasm", body: wasmBytes}})
	stB, err := h2.svc.StageFreshInstall(ctx, StageInstallRequest{ArchiveReader: bytes.NewReader(tpkgB), Source: SourceSpec{Kind: SourceKindLocalArchive, URI: "./b.tpkg"}})
	if err != nil {
		t.Fatalf("StageFreshInstall B: %v", err)
	}
	if _, err := h2.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      stB.Installation.InstallationID,
		ExpectedVersion:     stB.Installation.Version,
		ApproverPrincipalID: h2.secRevID,
		ActivateImmediately: true,
		OrgID:               h2.orgID,
	}); err != nil {
		t.Fatalf("Approve B: %v", err)
	}

	// Attempt to uninstall A while passing B's active directory path!
	if uerr := h2.svc.UninstallInstallation(ctx, disA.InstallationID, disA.Version, stB.Archive.StagingDir, h2.opID, ""); !errors.Is(uerr, ErrPathNotConfined) {
		t.Fatalf("expected ErrPathNotConfined when packageDir belongs to another installation, got %v", uerr)
	}
	if _, statErr := os.Stat(stB.Archive.StagingDir); statErr != nil {
		t.Fatalf("package B directory MUST remain intact on disk, stat err=%v", statErr)
	}

	// 3. Revocation TOCTOU between staging and ApproveAndActivate / ApproveEscalatedUpgrade.
	h3 := newInstallerHarness(t, port.MinTrustAnyApproved)
	mR1 := makeWasmManifestYAML("@rev/toctou", "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.r.example"}, nil, 16)
	tpkgR1, shaR1 := buildTPKGBytes(t, []archiveEntrySpec{{name: "manifest.yaml", body: mR1}, {name: "dist/adapter.wasm", body: wasmBytes}})
	stR1, err := h3.svc.StageFreshInstall(ctx, StageInstallRequest{ArchiveReader: bytes.NewReader(tpkgR1), Source: SourceSpec{Kind: SourceKindLocalArchive, URI: "./r1.tpkg"}})
	if err != nil {
		t.Fatalf("StageFreshInstall R1: %v", err)
	}
	// Revoke R1 while it is parked in PENDING_PERMISSION_APPROVAL!
	_, _ = h3.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_revocations (id, package_slug, version_or_sha256, severity, version_serial, reason, feed_expires_at_ms, imported_at_ms)
		VALUES ('rev_toctou1', '@rev/toctou', ?, 'CRITICAL_MALICIOUS', 1, 'revoked during approval wait', 9999999999999, 1000)`, shaR1)
	if _, aerr := h3.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      stR1.Installation.InstallationID,
		ExpectedVersion:     stR1.Installation.Version,
		ApproverPrincipalID: h3.opID,
		ActivateImmediately: true,
	}); !errors.Is(aerr, ErrRevokedExtension) {
		t.Fatalf("expected ApproveAndActivate blocked by ErrRevokedExtension on TOCTOU revocation, got %v", aerr)
	}

	// Also test TOCTOU revocation during ApproveEscalatedUpgrade!
	mR1Clean := makeWasmManifestYAML("@rev/toctou-up", "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.r.example"}, nil, 16)
	tpkgR1Clean, _ := buildTPKGBytes(t, []archiveEntrySpec{{name: "manifest.yaml", body: mR1Clean}, {name: "dist/adapter.wasm", body: wasmBytes}})
	stR1C, _ := h3.svc.StageFreshInstall(ctx, StageInstallRequest{ArchiveReader: bytes.NewReader(tpkgR1Clean), Source: SourceSpec{Kind: SourceKindLocalArchive, URI: "./r1c.tpkg"}})
	actR1C, _ := h3.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      stR1C.Installation.InstallationID,
		ExpectedVersion:     stR1C.Installation.Version,
		ApproverPrincipalID: h3.opID,
		ActivateImmediately: true,
	})
	mR2 := makeWasmManifestYAML("@rev/toctou-up", "2.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.r.example", "extra.r.example"}, nil, 16)
	tpkgR2, shaR2 := buildTPKGBytes(t, []archiveEntrySpec{{name: "manifest.yaml", body: mR2}, {name: "dist/adapter.wasm", body: wasmBytes}})
	upR2, err := h3.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  actR1C.InstallationID,
		ExpectedVersion: actR1C.Version,
		ArchiveReader:   bytes.NewReader(tpkgR2),
		Source:          SourceSpec{Kind: SourceKindLocalArchive, URI: "./r2.tpkg"},
		ActorID:         h3.opID,
	})
	if err != nil || !upR2.Escalated {
		t.Fatalf("StageUpgrade R2: %+v err=%v", upR2, err)
	}
	_, _ = h3.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_revocations (id, package_slug, version_or_sha256, severity, version_serial, reason, feed_expires_at_ms, imported_at_ms)
		VALUES ('rev_toctou2', '@rev/toctou-up', ?, 'CRITICAL', 1, 'revoked before reconsent approval', 9999999999999, 1000)`, shaR2)
	if _, aerr := h3.svc.ApproveEscalatedUpgrade(ctx, upR2.Installation.InstallationID, upR2.Installation.Version, h3.opID, "", "req_toctou2"); !errors.Is(aerr, ErrRevokedExtension) {
		t.Fatalf("expected ApproveEscalatedUpgrade blocked by ErrRevokedExtension on TOCTOU revocation, got %v", aerr)
	}
}

func TestInstallerCT20ResidualUnpinnedPolicyAndNoVersionFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newInstallerHarness(t, port.MinTrustVerifiedOnly)
	wasmBytes := wasmfixtures.EchoTransformWASM()
	wasmSHA := shaHex(wasmBytes)

	// 1. Install verified v1.0.0 -> enable unpinned org policy (pinned_installation_id = NULL) -> uninstall.
	mV1 := makeWasmManifestYAML("@ct20/residual", "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com"}, nil, 16)
	tpkgV1, shaV1 := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mV1},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	pubSigV1 := ed25519.Sign(h.pubPriv, SignatureSigningPayload("@ct20/residual", "1.0.0", shaV1))

	st1, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgV1),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./residual-v1.tpkg"},
		Signature:     ProvenanceSignature{PublisherOrRootSig: pubSigV1},
		ActorID:       h.devID,
		TargetOrgID:   h.orgID,
	})
	if err != nil {
		t.Fatalf("StageFreshInstall verified v1: %v", err)
	}
	dis1, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      st1.Installation.InstallationID,
		ExpectedVersion:     st1.Installation.Version,
		ApproverPrincipalID: h.secRevID,
		ActivateImmediately: false,
		OrgID:               h.orgID,
	})
	if err != nil {
		t.Fatalf("ApproveAndActivate disabled: %v", err)
	}
	if err := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, dis1.PackageID, true, nil, h.secRevID); err != nil {
		t.Fatalf("SetOrgExtensionPolicy unpinned verified: %v", err)
	}
	if err := h.svc.UninstallInstallation(ctx, dis1.InstallationID, dis1.Version, "", h.opID, ""); err != nil {
		t.Fatalf("UninstallInstallation: %v", err)
	}

	// 2. When no active/candidate version exists (UNINSTALLED), SetOrgExtensionPolicy MUST NOT
	//    fall back to stale package-level origin_trust_tier='verified' to grant enablement,
	//    nor allow pinning the uninstalled installation ID.
	if perr := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, dis1.PackageID, true, nil, h.secRevID); !errors.Is(perr, ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected ErrOrgTrustPolicyDenied when no active/candidate version exists, got %v", perr)
	}
	if pinErr := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, dis1.PackageID, true, &dis1.InstallationID, h.secRevID); !errors.Is(pinErr, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when pinning UNINSTALLED installation, got %v", pinErr)
	}
	if _, uerr := h.st.RawDB().ExecContext(ctx, `UPDATE extension_packages SET origin_trust_tier = 'verified' WHERE id = ?`, dis1.PackageID); uerr != nil {
		t.Fatalf("simulate stale package tier: %v", uerr)
	}
	if perr := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, dis1.PackageID, true, nil, h.secRevID); !errors.Is(perr, ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected ErrOrgTrustPolicyDenied even with stale package-level origin_trust_tier='verified', got %v", perr)
	}

	// 3. Reinstall unsigned v2.0.0 with TargetOrgID="" while unpinned VERIFIED_ONLY org policy is still enabled!
	//    StageFreshInstall MUST block unverified_external for a package enabled in a VERIFIED_ONLY org.
	mV2 := makeWasmManifestYAML("@ct20/residual", "2.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com"}, nil, 16)
	tpkgV2, shaV2 := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mV2},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	if _, stErr := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgV2),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./residual-v2-unsigned.tpkg"},
		ActorID:       h.devID,
		TargetOrgID:   "",
	}); !errors.Is(stErr, ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected ErrOrgTrustPolicyDenied on unsigned reinstall with enabled VERIFIED_ONLY policy, got %v", stErr)
	}

	// 4. Legitimate verified reinstall + approve still succeeds cleanly!
	pubSigV2 := ed25519.Sign(h.pubPriv, SignatureSigningPayload("@ct20/residual", "2.0.0", shaV2))
	st2Valid, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgV2),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./residual-v2-signed.tpkg"},
		Signature:     ProvenanceSignature{PublisherOrRootSig: pubSigV2},
		ActorID:       h.devID,
		TargetOrgID:   "",
	})
	if err != nil {
		t.Fatalf("expected legitimate verified reinstall to succeed, got %v", err)
	}
	act2Valid, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      st2Valid.Installation.InstallationID,
		ExpectedVersion:     st2Valid.Installation.Version,
		ApproverPrincipalID: h.opID,
		ActivateImmediately: true,
	})
	if err != nil || act2Valid.State != StateEnabledActive || act2Valid.OriginTrustTier != lockfile.TrustTierVerified {
		t.Fatalf("expected verified reinstall approved and active, got rec=%+v err=%v", act2Valid, err)
	}

	// 5. TOCTOU: if an unsigned installation is staged BEFORE org trust is tightened (ANY_APPROVED -> VERIFIED_ONLY),
	//    ApproveAndActivate MUST still block activation with ErrOrgTrustPolicyDenied.
	hTOCTOU := newInstallerHarness(t, port.MinTrustAnyApproved)
	mTOCTOU := makeWasmManifestYAML("@ct20/toctou", "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com"}, nil, 16)
	tpkgTOCTOU, _ := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mTOCTOU},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	stTOCTOU, err := hTOCTOU.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgTOCTOU),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./toctou-unsigned.tpkg"},
		ActorID:       hTOCTOU.devID,
		TargetOrgID:   "",
	})
	if err != nil {
		t.Fatalf("StageFreshInstall unsigned under ANY_APPROVED: %v", err)
	}
	// SECURITY_REVIEWER cannot opt an org into unsigned unverified_external packages (requires ORG_OWNER per TP-BR-0202).
	if perr := hTOCTOU.svc.SetOrgExtensionPolicy(ctx, hTOCTOU.orgID, stTOCTOU.Installation.PackageID, true, nil, hTOCTOU.secRevID); !errors.Is(perr, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for SECURITY_REVIEWER enabling unsigned package, got %v", perr)
	}
	if err := hTOCTOU.svc.SetOrgExtensionPolicy(ctx, hTOCTOU.orgID, stTOCTOU.Installation.PackageID, true, nil, hTOCTOU.orgOwnerID); err != nil {
		t.Fatalf("SetOrgExtensionPolicy unpinned by ORG_OWNER under ANY_APPROVED: %v", err)
	}
	if _, err := hTOCTOU.st.RawDB().ExecContext(ctx, `UPDATE organizations SET min_plugin_trust = ? WHERE id = ?`, port.MinTrustVerifiedOnly, hTOCTOU.orgID); err != nil {
		t.Fatalf("tighten org min_plugin_trust to VERIFIED_ONLY: %v", err)
	}
	if _, aerr := hTOCTOU.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      stTOCTOU.Installation.InstallationID,
		ExpectedVersion:     stTOCTOU.Installation.Version,
		ApproverPrincipalID: hTOCTOU.opID,
		ActivateImmediately: true,
	}); !errors.Is(aerr, ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected ErrOrgTrustPolicyDenied on ApproveAndActivate after org trust tightened, got %v", aerr)
	}
	stillPending, err := hTOCTOU.svc.GetInstallation(ctx, stTOCTOU.Installation.InstallationID)
	if err != nil || stillPending.State != StatePendingPermissionApproval {
		t.Fatalf("expected installation to remain PENDING_PERMISSION_APPROVAL, got rec=%+v err=%v", stillPending, err)
	}
}

// FZ-02: Native Go fuzzer verifying zero panics and zero path escapes across arbitrary archive/YAML bytes.
func FuzzArchiveAndManifest(f *testing.F) {
	f.Add([]byte("not-a-gzip"))
	f.Add(makeWasmManifestYAML("@acme/seed", "0.1.0", "0.1", ">=0.1.0 <0.3.0", "", []string{"api.example.com"}, []string{"api_key"}, 16))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseAndValidateManifest(data, DefaultValidationPolicy(), nil)
		_, _ = ExtractSafeArchive(bytes.NewReader(data), t.TempDir())
	})
}

func TestInstallerLegacyMissingSlotReconsentAndOrgOwnerOptIn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newInstallerHarness(t, port.MinTrustAnyApproved)

	wasmBytes := wasmfixtures.EchoTransformWASM()
	wasmSHA := shaHex(wasmBytes)
	mYAML := makeWasmManifestYAML("@legacy/reconsent", "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com"}, []string{"api_key", "org_secret"}, 16)
	// Add prior v0.9.0 first, then upgrade to v1.0.0 so previous_pinned_version_id is non-NULL,
	// and simulate a legacy row missing secret_slots from approved_permissions_json.
	mPrevYAML := makeWasmManifestYAML("@legacy/reconsent", "0.9.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com"}, []string{"api_key", "org_secret"}, 16)
	pkgPrev, _ := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mPrevYAML},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	st0, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(pkgPrev),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./legacy-0.9.tpkg"},
		ActorID:       h.opID,
	})
	if err != nil {
		t.Fatal(err)
	}
	act0, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      st0.Installation.InstallationID,
		ExpectedVersion:     st0.Installation.Version,
		ApproverPrincipalID: h.opID,
		ActivateImmediately: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	pkgCur, _ := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: mYAML},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	up, err := h.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  act0.InstallationID,
		ExpectedVersion: act0.Version,
		ArchiveReader:   bytes.NewReader(pkgCur),
		Source:          SourceSpec{Kind: SourceKindLocalArchive, URI: "./legacy-1.0.tpkg"},
		ActorID:         h.opID,
	})
	if err != nil || up.Escalated {
		t.Fatalf("expected non-escalating upgrade v0.9->v1.0, err=%v escalated=%v", err, up != nil && up.Escalated)
	}
	prevPinBefore := derefStr(up.Installation.PreviousPinnedVersionID)
	if prevPinBefore == "" {
		t.Fatal("expected previous_pinned_version_id set after upgrade")
	}

	// Simulate legacy installation row where approved_permissions_json omitted manifest secret slots.
	legacyApproved := `{"allowed_hosts":["api.example.com"],"require_tls":true,"execution_tier":"WASM_SANDBOX_T2","hooks":["provider:transform_request"],"max_memory_mib":16}`
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE extension_installations SET approved_permissions_json = ? WHERE id = ?`, legacyApproved, up.Installation.InstallationID); err != nil {
		t.Fatal(err)
	}

	// 1. Unauthorized DEVELOPER cannot request re-consent.
	if _, _, derr := h.svc.RequestActivePermissionReconsent(ctx, up.Installation.InstallationID, up.Installation.Version, h.devID, h.orgID, "req_dev"); !errors.Is(derr, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for developer reconsent request, got %v", derr)
	}

	// 2. RequestActivePermissionReconsent moves installation to UPDATING_PENDING_RECONSENT WITHOUT auto-granting slots.
	cur, err := h.svc.GetInstallation(ctx, up.Installation.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	stagedReconsent, delta, err := h.svc.RequestActivePermissionReconsent(ctx, cur.InstallationID, cur.Version, h.secRevID, h.orgID, "req_reconsent_1")
	if err != nil {
		t.Fatalf("RequestActivePermissionReconsent failed: %v", err)
	}
	if stagedReconsent.State != StateUpdatingPendingReconsent {
		t.Fatalf("expected UPDATING_PENDING_RECONSENT, got %s", stagedReconsent.State)
	}
	if len(delta.AddedSecretSlots) != 2 || delta.AddedSecretSlots[0] != "api_key" || delta.AddedSecretSlots[1] != "org_secret" {
		t.Fatalf("expected delta.AddedSecretSlots=[api_key org_secret], got %+v", delta.AddedSecretSlots)
	}
	if len(stagedReconsent.ApprovedPermissions.SecretSlots) != 0 {
		t.Fatalf("SM-01 consent boundary violated: slots must remain unapproved before explicit approval, got %v", stagedReconsent.ApprovedPermissions.SecretSlots)
	}

	// 3. RejectEscalatedUpgrade restores ENABLED_ACTIVE and leaves secret slots STILL unapproved.
	rejected, err := h.svc.RejectEscalatedUpgrade(ctx, stagedReconsent.InstallationID, stagedReconsent.Version, h.secRevID, h.orgID, "req_rej")
	if err != nil {
		t.Fatalf("RejectEscalatedUpgrade failed: %v", err)
	}
	if rejected.State != StateEnabledActive || len(rejected.ApprovedPermissions.SecretSlots) != 0 {
		t.Fatalf("expected rejection to preserve legacy missing slots & ENABLED_ACTIVE, got state=%s slots=%v", rejected.State, rejected.ApprovedPermissions.SecretSlots)
	}

	// 4. Re-stage and ApproveEscalatedUpgrade grants consent, preserves previous_pinned_version_id, and syncs lockfile.
	restaged, _, err := h.svc.RequestActivePermissionReconsent(ctx, rejected.InstallationID, rejected.Version, h.secRevID, h.orgID, "req_reconsent_2")
	if err != nil {
		t.Fatalf("second RequestActivePermissionReconsent failed: %v", err)
	}
	approved, err := h.svc.ApproveEscalatedUpgrade(ctx, restaged.InstallationID, restaged.Version, h.opID, "", "req_approve_reconsent")
	if err != nil {
		t.Fatalf("ApproveEscalatedUpgrade for legacy reconsent failed: %v", err)
	}
	if approved.State != StateEnabledActive || len(approved.ApprovedPermissions.SecretSlots) != 2 {
		t.Fatalf("expected ENABLED_ACTIVE with 2 approved slots, got state=%s slots=%v", approved.State, approved.ApprovedPermissions.SecretSlots)
	}
	if derefStr(approved.PreviousPinnedVersionID) != prevPinBefore {
		t.Fatalf("expected rollback history previous_pinned_version_id preserved (%s), got %v", prevPinBefore, approved.PreviousPinnedVersionID)
	}
	lf, err := lockfile.Read(h.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if entry := lf.Extensions["@legacy/reconsent"]; len(entry.ApprovedPermissions.SecretSlots) != 2 || entry.PreviousPinnedRollback == nil || entry.PreviousPinnedRollback.Version != "0.9.0" {
		t.Fatalf("expected tanpopo.lock synced with slots and rollback 0.9.0, got %+v", entry)
	}

	// 5. ORG_OWNER vs non-owner role boundary for unsigned unverified_external SetOrgExtensionPolicy (TP-BR-0202).
	if perr := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, approved.PackageID, true, nil, h.devID); !errors.Is(perr, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for DEVELOPER, got %v", perr)
	}
	if perr := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, approved.PackageID, true, nil, h.secRevID); !errors.Is(perr, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for SECURITY_REVIEWER enabling unsigned unverified_external, got %v", perr)
	}
	if perr := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, approved.PackageID, true, nil, h.opID); !errors.Is(perr, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for non-ORG_OWNER INSTANCE OPERATOR enabling unsigned package, got %v", perr)
	}
	if err := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, approved.PackageID, true, nil, h.orgOwnerID); err != nil {
		t.Fatalf("expected ORG_OWNER to opt org into unsigned unverified_external package cleanly, got %v", err)
	}
}

func TestInstallerTPBR0202TrustDowngradeInvalidatesNonOwnerPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newInstallerHarness(t, port.MinTrustAnyApproved)

	wasmBytes := []byte("wasm-tpbr0202-downgrade")
	wSum := sha256.Sum256(wasmBytes)
	wasmSHA := hex.EncodeToString(wSum[:])

	buildArchive := func(ver string, hosts []string, slots []string) ([]byte, string) {
		m := makeWasmManifestYAML("@dg/policy-reset", ver, "0.1", ">=0.1.0", wasmSHA, hosts, slots, 16)
		return buildTPKGBytes(t, []archiveEntrySpec{
			{name: "manifest.yaml", body: m},
			{name: "dist/adapter.wasm", body: wasmBytes},
		})
	}

	policyFlags := func(pkgID string) (bool, bool) {
		var enabledInt, optInInt int
		err := h.st.RawDB().QueryRowContext(ctx, `SELECT enabled, org_owner_opt_in FROM org_extension_policies WHERE org_id = ? AND package_id = ?`, h.orgID, pkgID).Scan(&enabledInt, &optInInt)
		if err != nil {
			t.Fatalf("query org_extension_policies: %v", err)
		}
		return enabledInt == 1, optInInt == 1
	}
	isOrgEnabled := func(pkgID string) bool {
		en, _ := policyFlags(pkgID)
		return en
	}

	// A. Verified v1.0.0 enabled by non-ORG_OWNER SECURITY_REVIEWER.
	b1, s1 := buildArchive("1.0.0", []string{"api.example.com"}, nil)
	st1, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(b1),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./dg-1.0.tpkg"},
		Signature:     ProvenanceSignature{PublisherOrRootSig: ed25519.Sign(h.pubPriv, SignatureSigningPayload("@dg/policy-reset", "1.0.0", s1))},
		ActorID:       h.devID,
	})
	if err != nil {
		t.Fatal(err)
	}
	act1, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      st1.Installation.InstallationID,
		ExpectedVersion:     st1.Installation.Version,
		ApproverPrincipalID: h.opID,
		ActivateImmediately: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, act1.PackageID, true, nil, h.secRevID); err != nil {
		t.Fatalf("SECURITY_REVIEWER should be allowed to enable verified package: %v", err)
	}
	if en, optIn := policyFlags(act1.PackageID); !en || optIn {
		t.Fatalf("expected enabled=1 and org_owner_opt_in=0 after SECURITY_REVIEWER verified enablement, got en=%v optIn=%v", en, optIn)
	}

	// B. Upgrade to unsigned v1.1.0 (escalated -> UPDATING_PENDING_RECONSENT):
	//    policy must immediately reset to enabled=0, and SECURITY_REVIEWER cannot re-enable while candidate is unsigned!
	b2, _ := buildArchive("1.1.0", []string{"api.example.com"}, []string{"api_key"})
	up2, err := h.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  act1.InstallationID,
		ExpectedVersion: act1.Version,
		ArchiveReader:   bytes.NewReader(b2),
		Source:          SourceSpec{Kind: SourceKindLocalArchive, URI: "./dg-1.1-unsigned.tpkg"},
		ActorID:         h.opID,
	})
	if err != nil || !up2.Escalated {
		t.Fatalf("expected escalating StageUpgrade to unsigned 1.1.0, err=%v esc=%v", err, up2 != nil && up2.Escalated)
	}
	if isOrgEnabled(act1.PackageID) {
		t.Fatal("security bypass: org_extension_policies.enabled remained 1 after staging unsigned downgrade")
	}
	if perr := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, act1.PackageID, true, nil, h.secRevID); !errors.Is(perr, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for SECURITY_REVIEWER while unsigned candidate is staged, got %v", perr)
	}
	// Approve unsigned v1.1.0, verify still disabled until ORG_OWNER explicitly re-opts in.
	act2, err := h.svc.ApproveEscalatedUpgrade(ctx, up2.Installation.InstallationID, up2.Installation.Version, h.opID, "", "req_dg2")
	if err != nil {
		t.Fatal(err)
	}
	if isOrgEnabled(act1.PackageID) {
		t.Fatal("expected policy to remain disabled after approving unsigned v1.1.0 without ORG_OWNER opt-in")
	}
	if err := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, act2.PackageID, true, nil, h.orgOwnerID); err != nil {
		t.Fatalf("ORG_OWNER opt-in failed: %v", err)
	}
	if en, optIn := policyFlags(act2.PackageID); !en || !optIn {
		t.Fatalf("expected enabled=1 and org_owner_opt_in=1 after ORG_OWNER opt-in, got en=%v optIn=%v", en, optIn)
	}

	// C. Same-tier unsigned upgrade (1.1.0 unsigned -> 1.1.1 unsigned) preserves prior ORG_OWNER opt-in.
	b2p, _ := buildArchive("1.1.1", []string{"api.example.com"}, []string{"api_key"})
	up2p, err := h.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  act2.InstallationID,
		ExpectedVersion: act2.Version,
		ArchiveReader:   bytes.NewReader(b2p),
		Source:          SourceSpec{Kind: SourceKindLocalArchive, URI: "./dg-1.1.1-unsigned.tpkg"},
		ActorID:         h.opID,
	})
	if err != nil || up2p.Escalated {
		t.Fatalf("expected non-escalating unsigned upgrade 1.1.0->1.1.1, err=%v esc=%v", err, up2p != nil && up2p.Escalated)
	}
	if !isOrgEnabled(act1.PackageID) {
		t.Fatal("expected ORG_OWNER opt-in preserved across unsigned->unsigned upgrade")
	}

	// D. Upgrade to verified v2.0.0 + non-owner re-enable, then RollbackInstallation back to unsigned v1.1.1 -> resets enabled=0!
	b3, s3 := buildArchive("2.0.0", []string{"api.example.com"}, []string{"api_key"})
	up3, err := h.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  up2p.Installation.InstallationID,
		ExpectedVersion: up2p.Installation.Version,
		ArchiveReader:   bytes.NewReader(b3),
		Source:          SourceSpec{Kind: SourceKindLocalArchive, URI: "./dg-2.0-verified.tpkg"},
		Signature:       ProvenanceSignature{PublisherOrRootSig: ed25519.Sign(h.pubPriv, SignatureSigningPayload("@dg/policy-reset", "2.0.0", s3))},
		ActorID:         h.opID,
	})
	if err != nil || up3.Escalated {
		t.Fatalf("expected non-escalating verified upgrade 1.1.1->2.0.0, err=%v", err)
	}
	if err := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, act1.PackageID, true, nil, h.secRevID); err != nil {
		t.Fatal(err)
	}
	rb, err := h.svc.RollbackInstallation(ctx, up3.Installation.InstallationID, up3.Installation.Version, h.opID, "", "req_rb_unsigned")
	if err != nil {
		t.Fatalf("RollbackInstallation failed: %v", err)
	}
	if isOrgEnabled(act1.PackageID) {
		t.Fatal("security bypass: RollbackInstallation from verified -> unsigned left org_extension_policies.enabled = 1")
	}

	// E. Non-owner enables (if restored to verified? disable->uninstall while enabled unpinned):
	//    Re-test Uninstall while unpinned enabled (via verified reinstall or ORG_OWNER enable -> unsigned StageFreshInstall).
	if err := h.svc.SetOrgExtensionPolicy(ctx, h.orgID, rb.PackageID, true, nil, h.orgOwnerID); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.QuarantineInstallation(ctx, rb.InstallationID, "prepare uninstall", h.opID); err != nil {
		t.Fatal(err)
	}
	dis, err := h.svc.GetInstallation(ctx, rb.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.UninstallInstallation(ctx, dis.InstallationID, dis.Version, "", h.opID, ""); err != nil {
		t.Fatal(err)
	}
	b4, _ := buildArchive("3.0.0", []string{"api.example.com"}, nil)
	if _, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(b4),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./dg-3.0-unsigned.tpkg"},
		ActorID:       h.devID,
	}); err != nil {
		t.Fatal(err)
	}
	if isOrgEnabled(rb.PackageID) {
		t.Fatal("security bypass: stale unpinned org_extension_policies.enabled=1 survived uninstall -> unsigned StageFreshInstall")
	}
}

func TestCPUQuotaCapsAndEscalation(t *testing.T) {
	t.Parallel()

	overCall := []byte(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/over-cpu"
  name: "Over CPU"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "sandboxed_wasm"
  entrypoint: "dist/adapter.wasm"
  sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  quotas:
    maxCpuMsPerCall: 5001
permissions:
  networkEgress:
    allowedHosts: ["api.acme.example"]
`)
	if _, err := ParseAndValidateManifest(overCall, DefaultValidationPolicy(), nil); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected ErrManifestInvalid for maxCpuMsPerCall > 5000, got %v", err)
	}

	overChunk := strings.ReplaceAll(string(overCall), "maxCpuMsPerCall: 5001", "maxCpuMsPerCall: 500\n    maxStreamChunkCpuMs: 1001")
	if _, err := ParseAndValidateManifest([]byte(overChunk), DefaultValidationPolicy(), nil); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected ErrManifestInvalid for maxStreamChunkCpuMs > 1000, got %v", err)
	}

	deltaCall := ComputePermissionDelta(
		PermissionSet{ExecutionTier: ExecutionTierWasmSandboxT2, MaxCpuMsPerCall: 50, MaxStreamChunkCpuMs: 25},
		PermissionSet{ExecutionTier: ExecutionTierWasmSandboxT2, MaxCpuMsPerCall: 100, MaxStreamChunkCpuMs: 25},
	)
	if !deltaCall.CpuQuotaIncreased || !deltaCall.HasEscalation() {
		t.Fatalf("expected MaxCpuMsPerCall increase to trigger CpuQuotaIncreased escalation, got %+v", deltaCall)
	}

	deltaChunk := ComputePermissionDelta(
		PermissionSet{ExecutionTier: ExecutionTierWasmSandboxT2, MaxCpuMsPerCall: 50, MaxStreamChunkCpuMs: 25},
		PermissionSet{ExecutionTier: ExecutionTierWasmSandboxT2, MaxCpuMsPerCall: 50, MaxStreamChunkCpuMs: 50},
	)
	if !deltaChunk.CpuQuotaIncreased || !deltaChunk.HasEscalation() {
		t.Fatalf("expected MaxStreamChunkCpuMs increase to trigger CpuQuotaIncreased escalation, got %+v", deltaChunk)
	}

	deltaSame := ComputePermissionDelta(
		PermissionSet{ExecutionTier: ExecutionTierWasmSandboxT2, MaxCpuMsPerCall: 100, MaxStreamChunkCpuMs: 50},
		PermissionSet{ExecutionTier: ExecutionTierWasmSandboxT2, MaxCpuMsPerCall: 50, MaxStreamChunkCpuMs: 25},
	)
	if deltaSame.CpuQuotaIncreased || deltaSame.HasEscalation() {
		t.Fatalf("expected decreased CPU quotas not to trigger escalation, got %+v", deltaSame)
	}
}

func TestAllowedNativePathsAndHeadersValidation(t *testing.T) {
	base := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/native-cap"
  name: "Native Cap"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "native-cap"
    supportedDialects: ["anthropic"]
    allowedNativePaths:
      - "^v1/messages(/count_tokens)?$"
    allowedNativeHeaders:
      - "anthropic-beta"
permissions:
  networkEgress:
    allowedHosts: ["api.anthropic.com"]
`
	if _, err := ParseAndValidateManifest([]byte(base), DefaultValidationPolicy(), nil); err != nil {
		t.Fatalf("expected valid allowedNativePaths/Headers manifest to pass, got %v", err)
	}

	for _, badPat := range []string{`[unclosed-regex`, `^v1/messages$)|(.*)|(?:x`} {
		badRegex := strings.ReplaceAll(base, `^v1/messages(/count_tokens)?$`, badPat)
		if _, err := ParseAndValidateManifest([]byte(badRegex), DefaultValidationPolicy(), nil); !errors.Is(err, ErrManifestInvalid) {
			t.Fatalf("expected ErrManifestInvalid on bad/breakout allowedNativePaths regex %q, got %v", badPat, err)
		}
	}

	cookieHdr := strings.ReplaceAll(base, `anthropic-beta`, `Cookie`)
	if _, err := ParseAndValidateManifest([]byte(cookieHdr), DefaultValidationPolicy(), nil); !errors.Is(err, ErrProhibitedCookiePermission) {
		t.Fatalf("expected ErrProhibitedCookiePermission on Cookie in allowedNativeHeaders, got %v", err)
	}

	for _, forbidden := range []string{"Authorization", "x-api-key", "X-Tanpopo-Env-Id", "Host", "Accept-Encoding", "Bad Header"} {
		cand := strings.ReplaceAll(base, `anthropic-beta`, forbidden)
		if _, err := ParseAndValidateManifest([]byte(cand), DefaultValidationPolicy(), nil); !errors.Is(err, ErrManifestInvalid) {
			t.Fatalf("expected ErrManifestInvalid for forbidden allowedNativeHeaders %q, got %v", forbidden, err)
		}
	}
}

type failingInstallerAuditSink struct {
	mu   sync.Mutex
	fail bool
}

func (f *failingInstallerAuditSink) setFail(v bool) {
	f.mu.Lock()
	f.fail = v
	f.mu.Unlock()
}

func (f *failingInstallerAuditSink) Append(_ context.Context, _ port.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("injected installer audit failure")
	}
	return nil
}

func (f *failingInstallerAuditSink) AppendTx(_ context.Context, _ *sql.Tx, _ port.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("injected installer tx audit failure")
	}
	return nil
}

func TestNEG2TransactionalAuditRollbackInstaller(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmp := t.TempDir()
	fc := clock.NewFakeClock(clock.DefaultFakeStart())

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(tmp, "inst_neg2.db"),
		NowMs:  func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	opID := idgen.MustNew(idgen.PrefixPrincipal)
	now := fc.Now().UnixMilli()
	if _, err := st.RawDB().ExecContext(ctx, `INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms) VALUES (?, NULL, 'HUMAN_LOCAL', 'op_neg2@example.com', 'ACTIVE', 1, ?, ?)`, opID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms) VALUES ('rb_op_neg2', NULL, ?, 'INSTANCE', 'instance', 'OPERATOR', 1, ?)`, opID, now); err != nil {
		t.Fatal(err)
	}

	sink := &failingInstallerAuditSink{}
	svc, err := NewLifecycleService(ServiceConfig{
		DB:           st.RawDB(),
		Audit:        sink,
		PluginsDir:   filepath.Join(tmp, "plugins"),
		LockfilePath: filepath.Join(tmp, "tanpopo.lock"),
		NowMs:        func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}

	wasmBytes := wasmfixtures.EchoTransformWASM()
	wasmSHA := shaHex(wasmBytes)
	m := makeWasmManifestYAML("@neg2/rollback-ext", "1.0.0", "0.1", ">=0.1.0", wasmSHA, []string{"api.example.com"}, nil, 16)
	raw, _ := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: m},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})

	staged, err := svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(raw),
		Source:        SourceSpec{Kind: SourceKindLocalArchive, URI: "./neg2.tpkg"},
		ActorID:       opID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Failing AppendTx during ApproveAndActivate rolls back activation completely.
	sink.setFail(true)
	if _, err := svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: opID,
		ActivateImmediately: true,
	}); err == nil {
		t.Fatalf("expected ApproveAndActivate to roll back on AppendTx failure")
	}
	afterFailApprove, err := svc.GetInstallation(ctx, staged.Installation.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailApprove.State != StatePendingPermissionApproval || afterFailApprove.Version != staged.Installation.Version {
		t.Fatalf("expected rollback to PENDING_PERMISSION_APPROVAL v%d, got state=%s v%d", staged.Installation.Version, afterFailApprove.State, afterFailApprove.Version)
	}

	// 2. Succeed with ApproveAndActivate, then inject failure during QuarantineInstallation -> rolls back cleanly.
	sink.setFail(false)
	active, err := svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: opID,
		ActivateImmediately: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sink.setFail(true)
	if err := svc.QuarantineInstallation(ctx, active.InstallationID, "compromised plugin", opID); err == nil {
		t.Fatalf("expected QuarantineInstallation to roll back on AppendTx failure")
	}
	afterFailQuar, err := svc.GetInstallation(ctx, active.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailQuar.State != StateEnabledActive || afterFailQuar.Version != active.Version {
		t.Fatalf("expected rollback to ENABLED_ACTIVE v%d, got state=%s v%d", active.Version, afterFailQuar.State, afterFailQuar.Version)
	}
}

type nonTxInstallerAuditSink struct{}

func (nonTxInstallerAuditSink) Append(context.Context, port.AuditEvent) error { return nil }

func TestInstallerNewLifecycleServiceRejectsMissingOrNonTxAuditSink(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	st, err := sqlite.Open(context.Background(), sqlite.Config{DBPath: filepath.Join(tmp, "inst_failclosed.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := NewLifecycleService(ServiceConfig{
		DB:         st.RawDB(),
		PluginsDir: filepath.Join(tmp, "plugins"),
		Audit:      nil,
	}); !errors.Is(err, ErrInvalidLifecycleConfig) {
		t.Fatalf("expected NewLifecycleService nil Audit to fail ErrInvalidLifecycleConfig, got %v", err)
	}
	if _, err := NewLifecycleService(ServiceConfig{
		DB:         st.RawDB(),
		PluginsDir: filepath.Join(tmp, "plugins"),
		Audit:      nonTxInstallerAuditSink{},
	}); !errors.Is(err, ErrInvalidLifecycleConfig) {
		t.Fatalf("expected NewLifecycleService non-TxAuditSink to fail ErrInvalidLifecycleConfig, got %v", err)
	}
}
