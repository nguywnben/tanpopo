// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package installer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/wasmfixtures"
)

var instPGSeq atomic.Int64

type instPGHarness struct {
	st        *postgres.Store
	svc       *LifecycleService
	fk        *clock.FakeClock
	rootPriv  ed25519.PrivateKey
	orgAID    string
	orgBID    string
	prnOwnerA string
	prnBootOp string
	prnSuspOp string
	aud       *auditCapture
}

func newInstallerPGHarness(t *testing.T) *instPGHarness {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if baseDSN == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_DSN required when TANPOPO_REQUIRE_POSTGRES_TESTS=1")
		}
		t.Log("TANPOPO_TEST_PG_DSN not set; skipping live Postgres installer RLS tests")
		return nil
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("tp_inst_pg_%d_%d", time.Now().UnixNano(), instPGSeq.Add(1))
	baseDB, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open base pg: %v", err)
	}
	if _, err := baseDB.ExecContext(ctx, `CREATE SCHEMA `+schemaName); err != nil {
		_ = baseDB.Close()
		t.Fatalf("create schema %s: %v", schemaName, err)
	}
	_ = baseDB.Close()
	t.Cleanup(func() {
		cleanDB, cerr := sql.Open("pgx", baseDSN)
		if cerr == nil {
			_, _ = cleanDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`)
			_ = cleanDB.Close()
		}
	})

	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()

	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	nowMs := func() int64 { return fk.Now().UTC().UnixMilli() }
	st, err := postgres.Open(ctx, postgres.Options{DSN: u.String(), NowMs: nowMs})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Create two active organizations so ensureGlobalApproverRoleTx exercises the multi-tenant (>1 org) OPERATOR gate.
	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "inst-pg-a", DisplayName: "Inst PG A"})
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "inst-pg-b", DisplayName: "Inst PG B"})
	if err != nil {
		t.Fatal(err)
	}

	now := nowMs()
	prnOwnerA := "prn_inst_owner_a"
	prnBootOp := "prn_inst_boot_op"
	prnSuspOp := "prn_inst_susp_op"

	tx, err := st.RawDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, port.DialectPostgres, orgA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ($1, $2, 'HUMAN_LOCAL', 'owner-a@inst.pg', 'ACTIVE', 1, $3, $3),
		       ($4, $2, 'HUMAN_LOCAL', 'boot-op@inst.pg', 'ACTIVE', 1, $3, $3),
		       ($5, $2, 'HUMAN_LOCAL', 'susp-op@inst.pg', 'SUSPENDED', 1, $3, $3)`,
		prnOwnerA, orgA.ID, now, prnBootOp, prnSuspOp,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_inst_owner_a', $1, $2, 'ORG', $1, 'ORG_OWNER', 1, $3)`,
		orgA.ID, prnOwnerA, now,
	); err != nil {
		t.Fatal(err)
	}
	if err := port.SetOperatorRLSTx(ctx, tx, port.DialectPostgres); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_inst_boot_op', NULL, $1, 'INSTANCE', 'instance', 'OPERATOR', 1, $3),
		       ('rb_inst_susp_op', NULL, $2, 'INSTANCE', 'instance', 'OPERATOR', 1, $3)`,
		prnBootOp, prnSuspOp, now,
	); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	rootPub, rootPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kr := trust.TrustedKeyring{RootPublicKeys: map[string]ed25519.PublicKey{
		PublicKeyFingerprint(rootPub): rootPub,
	}}
	aud := &auditCapture{}
	dir := t.TempDir()
	svc, err := NewLifecycleService(ServiceConfig{
		DB:           st.RawDB(),
		Audit:        aud,
		PluginsDir:   filepath.Join(dir, "plugins"),
		LockfilePath: filepath.Join(dir, "tanpopo.lock"),
		Keyring:      kr,
		NowMs:        nowMs,
	})
	if err != nil {
		t.Fatal(err)
	}

	return &instPGHarness{
		st:        st,
		svc:       svc,
		fk:        fk,
		rootPriv:  rootPriv,
		orgAID:    orgA.ID,
		orgBID:    orgB.ID,
		prnOwnerA: prnOwnerA,
		prnBootOp: prnBootOp,
		prnSuspOp: prnSuspOp,
		aud:       aud,
	}
}

func TestDEF081_PostgresMultiOrgBootstrapOperatorExtensionLifecycleAndDenials(t *testing.T) {
	ctx := context.Background()
	h := newInstallerPGHarness(t)
	if h == nil {
		return
	}

	wasmBytes := wasmfixtures.EchoTransformWASM()
	wasmSHA := shaHex(wasmBytes)
	m1 := makeWasmManifestYAML("@tanpopo/pg-op-test", "1.0.0", "0.1", ">=0.1.0 <0.3.0", wasmSHA, []string{"api.pgop.example.com"}, []string{"primary_api_key"}, 16)
	archive1, sha1 := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: m1},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	sig1 := ed25519.Sign(h.rootPriv, SignatureSigningPayload("@tanpopo/pg-op-test", "1.0.0", sha1))

	staged, err := h.svc.StageFreshInstall(ctx, StageInstallRequest{
		ArchiveReader: bytes.NewReader(archive1),
		Source:        SourceSpec{Kind: SourceKindURL, URI: "https://downloads.tanpopo.ai/pg-op-test-1.0.0.tpkg", ExpectedSHA256: sha1},
		Signature:     ProvenanceSignature{PublisherOrRootSig: sig1},
		ActorID:       h.prnBootOp,
		TargetOrgID:   h.orgAID,
	})
	if err != nil {
		t.Fatalf("StageFreshInstall v1: %v", err)
	}

	// Negative 1: Multi-tenant deployment denies non-operator ORG_OWNER on ApproveAndActivate.
	if _, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: h.prnOwnerA,
		ActivateImmediately: true,
		OrgID:               h.orgAID,
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected multi-org non-operator ApproveAndActivate = ErrPermissionDenied, got %v", err)
	}

	// Negative 2: Suspended org-bound INSTANCE OPERATOR denied fail-closed.
	if _, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: h.prnSuspOp,
		ActivateImmediately: true,
		OrgID:               "",
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected suspended operator ApproveAndActivate = ErrPermissionDenied, got %v", err)
	}

	// Positive: Org-bound bootstrapped INSTANCE OPERATOR succeeds in multi-org PG forced RLS with empty or non-empty OrgID!
	activeV1, err := h.svc.ApproveAndActivate(ctx, ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: h.prnBootOp,
		ActivateImmediately: true,
		OrgID:               "",
	})
	if err != nil {
		t.Fatalf("bootstrap operator multi-org ApproveAndActivate v1: %v", err)
	}

	// Org-bound INSTANCE OPERATOR can manage an official extension policy in orgB even without orgB role binding,
	// because ensureApproverRoleTx resolves the active org-bound INSTANCE OPERATOR across PG forced RLS.
	if err := h.svc.SetOrgExtensionPolicy(ctx, h.orgBID, activeV1.PackageID, true, &activeV1.InstallationID, h.prnBootOp); err != nil {
		t.Fatalf("bootstrap operator SetOrgExtensionPolicy(orgB): %v", err)
	}

	// Stage non-escalating v2 upgrade -> cut over directly -> rollback to v1 -> quarantine -> unquarantine -> disable/enable/uninstall!
	m2 := makeWasmManifestYAML("@tanpopo/pg-op-test", "1.0.1", "0.1", ">=0.1.0 <0.3.0", wasmSHA, []string{"api.pgop.example.com"}, []string{"primary_api_key"}, 16)
	archive2, sha2 := buildTPKGBytes(t, []archiveEntrySpec{
		{name: "manifest.yaml", body: m2},
		{name: "dist/adapter.wasm", body: wasmBytes},
	})
	sig2 := ed25519.Sign(h.rootPriv, SignatureSigningPayload("@tanpopo/pg-op-test", "1.0.1", sha2))
	upV2, err := h.svc.StageUpgrade(ctx, StageUpgradeRequest{
		InstallationID:  activeV1.InstallationID,
		ExpectedVersion: activeV1.Version,
		ArchiveReader:   bytes.NewReader(archive2),
		Source:          SourceSpec{Kind: SourceKindURL, URI: "https://downloads.tanpopo.ai/pg-op-test-1.0.1.tpkg", ExpectedSHA256: sha2},
		Signature:       ProvenanceSignature{PublisherOrRootSig: sig2},
		ActorID:         h.prnBootOp,
		OrgID:           h.orgBID,
	})
	if err != nil {
		t.Fatalf("bootstrap operator multi-org StageUpgrade v2: %v", err)
	}
	if upV2.Escalated || upV2.Installation.ActiveSemver == nil || *upV2.Installation.ActiveSemver != "1.0.1" {
		t.Fatalf("unexpected StageUpgrade result: %+v", upV2.Installation)
	}

	rolledBack, err := h.svc.RollbackInstallation(ctx, upV2.Installation.InstallationID, upV2.Installation.Version, h.prnBootOp, "", "req_rb")
	if err != nil {
		t.Fatalf("bootstrap operator multi-org RollbackInstallation: %v", err)
	}
	if rolledBack.ActiveSemver == nil || *rolledBack.ActiveSemver != "1.0.0" {
		t.Fatalf("expected active version rolled back to 1.0.0, got %+v", rolledBack.ActiveSemver)
	}

	// Quarantine -> Unquarantine -> Disable policy -> Uninstall under multi-org forced RLS.
	if err := h.svc.QuarantineInstallationIfVersion(ctx, rolledBack.InstallationID, rolledBack.Version, "pg multi-org test quarantine", h.prnBootOp); err != nil {
		t.Fatalf("QuarantineInstallationIfVersion: %v", err)
	}
	quar, err := h.svc.GetInstallation(ctx, rolledBack.InstallationID)
	if err != nil || quar.State != StateQuarantined {
		t.Fatalf("expected QUARANTINED, got (%+v, %v)", quar, err)
	}
	if err := h.svc.UnquarantineInstallationIfVersion(ctx, quar.InstallationID, quar.Version, "pg multi-org test recovery", h.prnBootOp); err != nil {
		t.Fatalf("UnquarantineInstallationIfVersion: %v", err)
	}
	unq, err := h.svc.GetInstallation(ctx, quar.InstallationID)
	if err != nil || unq.State != StateInstalledDisabled {
		t.Fatalf("expected INSTALLED_DISABLED after unquarantine, got (%+v, %v)", unq, err)
	}
	reEnabled, err := h.svc.EnableDisabledInstallation(ctx, unq.InstallationID, unq.Version, h.prnBootOp, "", "req_reen")
	if err != nil || reEnabled.State != StateEnabledActive {
		t.Fatalf("expected ENABLED_ACTIVE after EnableDisabledInstallation, got (%+v, %v)", reEnabled, err)
	}
	if err := h.svc.SetOrgExtensionPolicy(ctx, h.orgBID, reEnabled.PackageID, false, nil, h.prnBootOp); err != nil {
		t.Fatalf("disable orgB policy: %v", err)
	}
	if err := h.svc.QuarantineInstallationIfVersion(ctx, reEnabled.InstallationID, reEnabled.Version, "prepare uninstall", h.prnBootOp); err != nil {
		t.Fatal(err)
	}
	q2, err := h.svc.GetInstallation(ctx, reEnabled.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.UninstallInstallation(ctx, q2.InstallationID, q2.Version, "", h.prnBootOp, ""); err != nil {
		t.Fatalf("bootstrap operator multi-org UninstallInstallation: %v", err)
	}
}
