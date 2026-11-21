// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vault

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
)

var vaultPGSeq atomic.Int64

func livePGStoreForVault(t *testing.T) *postgres.Store {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if baseDSN == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatalf("TANPOPO_TEST_PG_DSN is required when TANPOPO_REQUIRE_POSTGRES_TESTS=1")
		}
		return nil
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("tp_pgvault_%d_%d", time.Now().UnixNano(), vaultPGSeq.Add(1))
	db, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open pg dsn: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, schemaName)); err != nil {
		t.Fatalf("create schema %s: %v", schemaName, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, schemaName))
	})
	sep := "?"
	if strings.Contains(baseDSN, "?") {
		sep = "&"
	}
	isoDSN := fmt.Sprintf("%s%soptions=-c%%20search_path%%3D%s", baseDSN, sep, schemaName)
	pgStore, err := postgres.Open(ctx, postgres.Options{DSN: isoDSN})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = pgStore.Close() })
	return pgStore
}

func TestVaultPostgresForceRLSAndFullLifecycle(t *testing.T) {
	pgStore := livePGStoreForVault(t)
	if pgStore == nil {
		return
	}
	ctx := context.Background()
	nowMs := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC).UnixMilli()

	org1, err := pgStore.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "vpg-1", DisplayName: "Vault PG 1",
	})
	if err != nil {
		t.Fatalf("CreateOrganization 1: %v", err)
	}
	org2, err := pgStore.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: idgen.MustNew(idgen.PrefixOrg), Slug: "vpg-2", DisplayName: "Vault PG 2",
	})
	if err != nil {
		t.Fatalf("CreateOrganization 2: %v", err)
	}
	prj1, err := pgStore.Tenancy().CreateProject(ctx, org1.ID, port.CreateProjectParams{
		ID: idgen.MustNew(idgen.PrefixProject), Slug: "p1", DisplayName: "Prj 1",
	})
	if err != nil {
		t.Fatalf("CreateProject 1: %v", err)
	}

	kek, err := NewStaticKEKProvider("kek_v1", random32Key(t))
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	defer kek.Close()

	st, err := NewStore(Config{
		DB:            pgStore.DB(),
		KEK:           kek,
		NowMs:         func() int64 { return nowMs },
		PostShredHook: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	defer st.Close()

	if err := st.CheckServingReady(ctx); err != nil {
		t.Fatalf("CheckServingReady(empty): %v", err)
	}

	// EnsureOrgDEK + EnsureProjectDEK across orgs & projects under PG FORCE RLS.
	if _, err := st.EnsureOrgDEK(ctx, org1.ID); err != nil {
		t.Fatalf("EnsureOrgDEK org1: %v", err)
	}
	if _, err := st.EnsureOrgDEK(ctx, org2.ID); err != nil {
		t.Fatalf("EnsureOrgDEK org2: %v", err)
	}
	prjMeta, err := st.EnsureProjectDEK(ctx, org1.ID, prj1.ID)
	if err != nil || prjMeta.KeyGeneration != 1 {
		t.Fatalf("EnsureProjectDEK: %+v err=%v", prjMeta, err)
	}
	if err := st.CheckServingReady(ctx); err != nil {
		t.Fatalf("CheckServingReady(provisioned): %v", err)
	}

	// SealSecret, UnsealWithCapability, and RotateSecret on org1/prj1 and org2.
	sec, err := st.SealSecret(ctx, SealSecretParams{
		OrgID:          org1.ID,
		OwnerProjectID: &prj1.ID,
		Name:           "pg-api-key",
		SecretKind:     SecretKindAPIKey,
		Plaintext:      []byte("sk-pg-initial"),
	})
	if err != nil {
		t.Fatalf("SealSecret: %v", err)
	}
	capTok := MintUnsealCapabilityForBroker()
	unsealed, err := st.UnsealWithCapability(ctx, capTok, org1.ID, sec.ID)
	if err != nil || !bytes.Equal(unsealed.Plaintext, []byte("sk-pg-initial")) {
		t.Fatalf("UnsealWithCapability initial: err=%v pt=%q", err, unsealed.Plaintext)
	}
	unsealed.Zeroize()

	rot, err := st.RotateSecret(ctx, org1.ID, sec.ID, sec.Version, RotateSecretParams{
		Plaintext: []byte("sk-pg-rotated"),
	})
	if err != nil || rot.Version != 2 {
		t.Fatalf("RotateSecret: %+v err=%v", rot, err)
	}
	unsealedRot, err := st.UnsealWithCapability(ctx, capTok, org1.ID, sec.ID)
	if err != nil || !bytes.Equal(unsealedRot.Plaintext, []byte("sk-pg-rotated")) {
		t.Fatalf("UnsealWithCapability rotated: err=%v", err)
	}
	unsealedRot.Zeroize()

	// Cross-org isolation: org2 cannot unseal org1 secret.
	if _, err := st.UnsealWithCapability(ctx, capTok, org2.ID, sec.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org Unseal = %v, want port.ErrNotFound", err)
	}

	// SealProjectBlob & SealOAuthSessionValue under PostgreSQL.
	bNonce, bCT, err := st.SealProjectBlob(ctx, org1.ID, prj1.ID, RecordKindDebugBlob, "req_1", []byte("trace-blob"))
	if err != nil {
		t.Fatalf("SealProjectBlob: %v", err)
	}
	bPT, err := st.UnsealProjectBlob(ctx, org1.ID, prj1.ID, RecordKindDebugBlob, "req_1", bNonce, bCT)
	if err != nil || !bytes.Equal(bPT, []byte("trace-blob")) {
		t.Fatalf("UnsealProjectBlob: err=%v pt=%q", err, bPT)
	}
	oNonce, oCT, err := st.SealOAuthSessionValue(ctx, org2.ID, "sess_pg_1", "pkce_verifier", []byte("verifier-xyz"))
	if err != nil {
		t.Fatalf("SealOAuthSessionValue: %v", err)
	}
	oPT, err := st.UnsealOAuthSessionValue(ctx, org2.ID, "sess_pg_1", "pkce_verifier", oNonce, oCT)
	if err != nil || !bytes.Equal(oPT, []byte("verifier-xyz")) {
		t.Fatalf("UnsealOAuthSessionValue: err=%v pt=%q", err, oPT)
	}

	// VerifyAllDEKEnvelopes & VerifyAllSecretEnvelopes + RewrapAllOrgDEKs under kek_v1 -> kek_v2.
	if err := st.VerifyAllDEKEnvelopes(ctx); err != nil {
		t.Fatalf("VerifyAllDEKEnvelopes: %v", err)
	}
	if n, err := st.VerifyAllSecretEnvelopes(ctx); err != nil || n != 1 {
		t.Fatalf("VerifyAllSecretEnvelopes = (%d, %v), want (1, nil)", n, err)
	}
	if err := kek.RegisterKey("kek_v2", random32Key(t), true); err != nil {
		t.Fatalf("RegisterKey kek_v2: %v", err)
	}
	rewrapSum, err := st.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2")
	if err != nil || rewrapSum.Rewrapped != 2 {
		t.Fatalf("RewrapAllOrgDEKs = %+v err=%v, want Rewrapped=2", rewrapSum, err)
	}
	if err := st.CheckServingReady(ctx); err != nil {
		t.Fatalf("CheckServingReady(post-rewrap): %v", err)
	}

	// Cryptographic ShredProjectDEK & ShredOrgDEK + FlushPendingShredCleanups.
	if err := st.ShredProjectDEK(ctx, org1.ID, prj1.ID, ShredRequest{
		ExpectedGeneration: 1, RequestID: "shred-prj-pg-1",
	}); err != nil {
		t.Fatalf("ShredProjectDEK: %v", err)
	}
	if _, err := st.UnsealProjectBlob(ctx, org1.ID, prj1.ID, RecordKindDebugBlob, "req_1", bNonce, bCT); err == nil {
		t.Fatalf("expected UnsealProjectBlob failure after ShredProjectDEK")
	}
	if err := st.FlushPendingShredCleanups(ctx); err != nil {
		t.Fatalf("FlushPendingShredCleanups: %v", err)
	}
	if err := st.ShredOrgDEK(ctx, org2.ID, ShredRequest{
		ExpectedGeneration: 1, RequestID: "shred-org-pg-2",
	}); err != nil {
		t.Fatalf("ShredOrgDEK: %v", err)
	}
	if _, err := st.UnsealOAuthSessionValue(ctx, org2.ID, "sess_pg_1", "pkce_verifier", oNonce, oCT); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("UnsealOAuth after ShredOrgDEK = %v, want port.ErrNotFound", err)
	}
}
