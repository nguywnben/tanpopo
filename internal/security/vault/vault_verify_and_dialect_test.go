// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vault

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func TestVaultVerifyAllSecretAndTxEnvelopesSQLite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newVaultTestEnv(t, time.Minute)
	if _, err := env.sqlite.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{
		ID: "org_verify_1", DisplayName: "Verify Org 1", Slug: "verify-org-1",
	}); err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if _, err := env.sqlite.Tenancy().CreateProject(ctx, "org_verify_1", port.CreateProjectParams{
		ID: "prj_verify_1", DisplayName: "Verify Proj 1", Slug: "verify-proj-1",
	}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := env.vault.EnsureOrgDEK(ctx, "org_verify_1"); err != nil {
		t.Fatalf("EnsureOrgDEK: %v", err)
	}
	if _, err := env.vault.EnsureProjectDEK(ctx, "org_verify_1", "prj_verify_1"); err != nil {
		t.Fatalf("EnsureProjectDEK: %v", err)
	}
	if _, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: "org_verify_1", Name: "sec-org-1", SecretKind: SecretKindAPIKey,
		Plaintext: []byte("sk-org-secret"),
	}); err != nil {
		t.Fatalf("SealSecret(org): %v", err)
	}
	projID := "prj_verify_1"
	secProj, err := env.vault.SealSecret(ctx, SealSecretParams{
		OrgID: "org_verify_1", OwnerProjectID: &projID, Name: "sec-proj-1",
		SecretKind: SecretKindWebhookHMAC, Plaintext: []byte("whsec-proj-secret"),
	})
	if err != nil {
		t.Fatalf("SealSecret(proj): %v", err)
	}

	// Nil / invalid receiver & input checks.
	var nilStore *Store
	if _, err := nilStore.VerifyAllSecretEnvelopes(ctx); err == nil {
		t.Fatal("nilStore.VerifyAllSecretEnvelopes should fail")
	}
	if err := nilStore.VerifyAllDEKEnvelopesTx(ctx, nil, port.DialectSQLite); err == nil {
		t.Fatal("nilStore.VerifyAllDEKEnvelopesTx should fail")
	}
	if _, err := nilStore.VerifyAllSecretEnvelopesTx(ctx, nil, port.DialectSQLite); err == nil {
		t.Fatal("nilStore.VerifyAllSecretEnvelopesTx should fail")
	}

	// VerifyAllSecretEnvelopes on SQLite.
	n, err := env.vault.VerifyAllSecretEnvelopes(ctx)
	if err != nil || n != 2 {
		t.Fatalf("VerifyAllSecretEnvelopes = (%d, %v), want (2, nil)", n, err)
	}

	// VerifyAllDEKEnvelopesTx & VerifyAllSecretEnvelopesTx inside an active TX.
	tx, err := env.sqlite.RawDB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := env.vault.VerifyAllDEKEnvelopesTx(ctx, tx, port.DialectSQLite); err != nil {
		t.Fatalf("VerifyAllDEKEnvelopesTx: %v", err)
	}
	nTx, err := env.vault.VerifyAllSecretEnvelopesTx(ctx, tx, port.DialectSQLite)
	if err != nil || nTx != 2 {
		t.Fatalf("VerifyAllSecretEnvelopesTx = (%d, %v), want (2, nil)", nTx, err)
	}

	// Canceled context short-circuits cleanly.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := env.vault.VerifyAllDEKEnvelopesTx(cctx, tx, port.DialectSQLite); err == nil {
		t.Fatal("expected canceled error from VerifyAllDEKEnvelopesTx")
	}
	if _, err := env.vault.VerifyAllSecretEnvelopesTx(cctx, tx, port.DialectSQLite); err == nil {
		t.Fatal("expected canceled error from VerifyAllSecretEnvelopesTx")
	}
	if _, err := env.vault.VerifyAllSecretEnvelopes(cctx); err == nil {
		t.Fatal("expected canceled error from VerifyAllSecretEnvelopes")
	}

	// Tampering with secret ciphertext fails fail-closed; tampering project DEK fails DEK verification.
	if _, err := tx.ExecContext(ctx, `UPDATE secret_credentials SET ciphertext = X'deadbeef' WHERE id = ?`, secProj.ID); err != nil {
		t.Fatalf("tamper ciphertext: %v", err)
	}
	if _, err := env.vault.VerifyAllSecretEnvelopesTx(ctx, tx, port.DialectSQLite); err == nil {
		t.Fatal("expected AEAD failure on tampered ciphertext")
	}
	env.vault.EvictAllDEKs()
	if _, err := tx.ExecContext(ctx, `UPDATE project_encryption_keys SET wrapped_project_dek = X'deadbeef' WHERE project_id = 'prj_verify_1'`); err != nil {
		t.Fatalf("tamper wrapped_project_dek: %v", err)
	}
	if err := env.vault.VerifyAllDEKEnvelopesTx(ctx, tx, port.DialectSQLite); err == nil {
		t.Fatal("expected AEAD failure on tampered project DEK")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE org_encryption_keys SET wrapped_org_dek = X'deadbeef' WHERE org_id = 'org_verify_1'`); err != nil {
		t.Fatalf("tamper wrapped_org_dek: %v", err)
	}
	if err := env.vault.VerifyAllDEKEnvelopesTx(ctx, tx, port.DialectSQLite); err == nil {
		t.Fatal("expected AEAD failure on tampered org DEK")
	}
}

func TestVaultPostgresDialectPathsWithInMemoryDriver(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	key1 := make([]byte, KeySizeBytes)
	key2 := make([]byte, KeySizeBytes)
	for i := range key1 {
		key1[i] = byte(i + 1)
		key2[i] = byte(i + 101)
	}
	p, err := NewStaticKEKProvider("kek_v1", key1)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	defer p.Close()
	if err := p.RegisterKey("kek_v2", key2, false); err != nil {
		t.Fatalf("RegisterKey: %v", err)
	}

	orgDEK := make([]byte, KeySizeBytes)
	prjDEK := make([]byte, KeySizeBytes)
	for i := range orgDEK {
		orgDEK[i] = byte(0xA0 + i)
		prjDEK[i] = byte(0x50 + i)
	}
	orgNonce, orgWrapped, err := p.WrapDEK(ctx, "kek_v1", "org_pg_1", 1, orgDEK)
	if err != nil {
		t.Fatalf("WrapDEK: %v", err)
	}
	prjAAD, err := projectDEKWrapAAD("org_pg_1", "prj_pg_1", 1, 1)
	if err != nil {
		t.Fatalf("projectDEKWrapAAD: %v", err)
	}
	prjNonce, prjWrapped, err := sealAESGCM(orgDEK, prjAAD, prjDEK)
	if err != nil {
		t.Fatalf("sealAESGCM(projDEK): %v", err)
	}
	secAAD, err := ProjectRecordAAD("org_pg_1", "prj_pg_1", RecordKindCred, "sec_pg_1")
	if err != nil {
		t.Fatalf("ProjectRecordAAD: %v", err)
	}
	secNonce, secCipher, err := sealAESGCM(prjDEK, secAAD, []byte("pg-secret-val"))
	if err != nil {
		t.Fatalf("sealAESGCM(secret): %v", err)
	}

	stub := &pgStubState{
		orgID:      "org_pg_1",
		projID:     "prj_pg_1",
		orgKEKID:   "kek_v1",
		orgNonce:   orgNonce,
		orgWrapped: orgWrapped,
		prjNonce:   prjNonce,
		prjWrapped: prjWrapped,
		secID:      "sec_pg_1",
		secKind:    SecretKindAPIKey,
		secNonce:   secNonce,
		secCipher:  secCipher,
	}
	db := sql.OpenDB(&pgStubConnector{state: stub})
	defer func() { _ = db.Close() }()

	st, err := NewStore(Config{
		DB:            db,
		KEK:           p,
		PostShredHook: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer st.Close()
	if st.effectiveDialect(ctx) != port.DialectPostgres {
		t.Fatalf("effectiveDialect = %q, want postgres", st.effectiveDialect(ctx))
	}
	var nilStore *Store
	if d := nilStore.effectiveDialect(ctx); d != port.DialectSQLite {
		t.Fatalf("nilStore effectiveDialect = %q, want sqlite", d)
	}
	emptyStore := &Store{}
	if d := emptyStore.effectiveDialect(ctx); d != port.DialectSQLite {
		t.Fatalf("emptyStore effectiveDialect = %q, want sqlite", d)
	}

	if err := st.CheckServingReady(ctx); err != nil {
		t.Fatalf("CheckServingReady(PG): %v", err)
	}
	if err := st.VerifyAllDEKEnvelopes(ctx); err != nil {
		t.Fatalf("VerifyAllDEKEnvelopes(PG): %v", err)
	}
	if n, err := st.VerifyAllSecretEnvelopes(ctx); err != nil || n != 1 {
		t.Fatalf("VerifyAllSecretEnvelopes(PG) = (%d, %v), want (1, nil)", n, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := st.VerifyAllDEKEnvelopesTx(ctx, tx, port.DialectPostgres); err != nil {
		t.Fatalf("VerifyAllDEKEnvelopesTx(PG): %v", err)
	}
	if n, err := st.VerifyAllSecretEnvelopesTx(ctx, tx, port.DialectPostgres); err != nil || n != 1 {
		t.Fatalf("VerifyAllSecretEnvelopesTx(PG) = (%d, %v), want (1, nil)", n, err)
	}
	if epoch, err := appendVaultChangelogTx(ctx, tx, port.DialectPostgres, "org_pg_1", "org", "org_pg_1", port.MutationKeyRotate, true, 1234); err != nil || epoch != 7 {
		t.Fatalf("appendVaultChangelogTx(PG) = (%d, %v)", epoch, err)
	}
	prjName := "prj_pg_1"
	if gen, err := nextKeyGenerationDialectTx(ctx, tx, port.DialectPostgres, "scope_1", "org_pg_1", &prjName, 1234); err != nil || gen != 3 {
		t.Fatalf("nextKeyGenerationDialectTx(PG) = (%d, %v)", gen, err)
	}
	if err := clearPendingTuplesDialectTx(ctx, tx, port.DialectPostgres, []pendingCleanupTuple{{scopeKey: "s1", orgID: "org_pg_1", epoch: 7, targetGeneration: 3}}, 1234); err != nil {
		t.Fatalf("clearPendingTuplesDialectTx(PG): %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	summary, err := st.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2")
	if err != nil || summary.Rewrapped != 1 || summary.ConfigEpoch != 7 {
		t.Fatalf("RewrapAllOrgDEKs(PG) = (%+v, %v)", summary, err)
	}
	if stub.setConfigCalls() < 4 {
		t.Fatalf("expected PostgreSQL set_config RLS calls >= 4, got %d", stub.setConfigCalls())
	}

	// Verify set_config error propagation fails closed.
	stub.setFailSetConfig(true)
	if err := st.VerifyAllDEKEnvelopes(ctx); err == nil {
		t.Fatal("expected VerifyAllDEKEnvelopes to fail when set_config fails")
	}
	tx2, _ := db.BeginTx(ctx, nil)
	if _, err := tx2.ExecContext(ctx, `SELECT 1`); err == nil {
		if err := st.setOrgRLSTx(ctx, tx2, "org_pg_1"); err == nil {
			t.Fatal("expected setOrgRLSTx to fail when set_config fails")
		}
		if err := st.setOperatorRLSTx(ctx, tx2); err == nil {
			t.Fatal("expected setOperatorRLSTx to fail when set_config fails")
		}
		if _, err := listOrgsInTx(ctx, tx2, port.DialectPostgres); err == nil {
			t.Fatal("expected listOrgsInTx to fail when set_config fails")
		}
		if err := setOrgRLSTxQuery(ctx, tx2, port.DialectPostgres, "org_pg_1"); err == nil {
			t.Fatal("expected setOrgRLSTxQuery to fail when set_config fails")
		}
	}
	_ = tx2.Rollback()
	if err := st.withOrgReadTx(ctx, "", func(sqlQueryRowExecutor, port.Dialect) error { return nil }); err == nil {
		t.Fatal("expected withOrgReadTx to reject empty orgID under PG")
	}

	// Empty org_encryption_keys (no org DEK rows yet) -> CheckServingReady & RewrapAllOrgDEKs return nil/0.
	stub.setFailSetConfig(false)
	stub.noOrgKey = true
	if err := st.CheckServingReady(ctx); err != nil {
		t.Fatalf("CheckServingReady(PG empty): %v", err)
	}
	if sum, err := st.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2"); err != nil || sum.Rewrapped != 0 {
		t.Fatalf("RewrapAllOrgDEKs(PG empty) = (%+v, %v)", sum, err)
	}
	if sum, err := st.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v1"); !errors.Is(err, ErrInvalidInput) || sum.Rewrapped != 0 {
		t.Fatalf("RewrapAllOrgDEKs(same KEK) = (%+v, %v)", sum, err)
	}

	// Org-level secret + SealProjectBlob + SealOAuthSessionValue under DialectPostgres.
	stub.noOrgKey = false
	orgSecAAD, err := OrgSecretAAD("org_pg_1", "sec_pg_1", SecretKindAPIKey)
	if err != nil {
		t.Fatalf("OrgSecretAAD: %v", err)
	}
	orgSecNonce, orgSecCipher, err := sealAESGCM(orgDEK, orgSecAAD, []byte("sk-org-pg"))
	if err != nil {
		t.Fatalf("sealAESGCM(org secret): %v", err)
	}
	stub.secOrgOnly = true
	stub.secNonce = orgSecNonce
	stub.secCipher = orgSecCipher
	if n, err := st.VerifyAllSecretEnvelopes(ctx); err != nil || n != 1 {
		t.Fatalf("VerifyAllSecretEnvelopes(PG org-only) = (%d, %v), want (1, nil)", n, err)
	}
	if _, _, err := st.SealProjectBlob(ctx, "org_pg_1", "prj_pg_1", RecordKindDebugBlob, "req_pg", []byte("blob")); err != nil {
		t.Fatalf("SealProjectBlob(PG): %v", err)
	}
	if _, _, err := st.SealOAuthSessionValue(ctx, "org_pg_1", "sess_pg", "access_token", []byte("oauth-tok")); err != nil {
		t.Fatalf("SealOAuthSessionValue(PG): %v", err)
	}
	var noStore *Store
	if err := noStore.CheckServingReady(ctx); err == nil {
		t.Fatal("nil store CheckServingReady should fail")
	}
	// Trigger CheckServingReady mismatch and invalid-key error branches.
	stub.orgKEKID = "kek_other"
	if err := st.CheckServingReady(ctx); err == nil {
		t.Fatal("CheckServingReady should fail when active KEK does not match envelope")
	}
	stub.orgKEKID = "kek_v1"
	if _, err := st.RewrapAllOrgDEKs(ctx, "invalid kek!", "kek_v2"); err == nil {
		t.Fatal("RewrapAllOrgDEKs(invalid old) should fail")
	}
	if _, err := st.RewrapAllOrgDEKs(ctx, "kek_v1", "invalid kek!"); err == nil {
		t.Fatal("RewrapAllOrgDEKs(invalid new) should fail")
	}
	archNonce, archCT, err := p.WrapArchiveKey(ctx, "kek_v1", orgDEK)
	if err != nil {
		t.Fatalf("WrapArchiveKey: %v", err)
	}
	archPT, err := p.UnwrapArchiveKey(ctx, "kek_v1", archNonce, archCT)
	if err != nil || len(archPT) != KeySizeBytes {
		t.Fatalf("UnwrapArchiveKey = (%d, %v)", len(archPT), err)
	}
	clear(archPT)
	if _, err := p.UnwrapArchiveKey(ctx, "missing_kek", archNonce, archCT); err == nil {
		t.Fatal("UnwrapArchiveKey(missing_kek) should fail")
	}
	if _, err := p.UnwrapArchiveKey(ctx, "kek_v1", []byte("short"), archCT); err == nil {
		t.Fatal("UnwrapArchiveKey(short nonce) should fail")
	}
	stub.orgWrapped = []byte("corrupt")
	if err := st.CheckServingReady(ctx); err == nil {
		t.Fatal("CheckServingReady should fail on corrupt wrapped org DEK")
	}
	stub.setFailQuery(true)
	if err := st.CheckServingReady(ctx); err == nil {
		t.Fatal("CheckServingReady should fail on query error")
	}
	if err := st.VerifyAllDEKEnvelopes(ctx); err == nil {
		t.Fatal("VerifyAllDEKEnvelopes should fail on query error")
	}
	if _, err := st.VerifyAllSecretEnvelopes(ctx); err == nil {
		t.Fatal("VerifyAllSecretEnvelopes should fail on query error")
	}
	if _, err := st.RewrapAllOrgDEKs(ctx, "kek_v1", "kek_v2"); err == nil {
		t.Fatal("RewrapAllOrgDEKs should fail on query error")
	}
	stub.setFailQuery(false)
	stub.setFailExec(true)
	tx3, _ := db.BeginTx(ctx, nil)
	if _, err := appendVaultChangelogTx(ctx, tx3, port.DialectPostgres, "", "master_kek", "k1", port.MutationKeyRotate, false, 1); err == nil {
		t.Fatal("expected appendVaultChangelogTx fail on query/exec")
	}
	if _, err := nextKeyGenerationDialectTx(ctx, tx3, port.DialectPostgres, "scope_err", "org_pg_1", nil, 1); err == nil {
		t.Fatal("expected nextKeyGenerationDialectTx fail on exec error")
	}
	if err := clearPendingTuplesDialectTx(ctx, tx3, port.DialectPostgres, []pendingCleanupTuple{{scopeKey: "s1", orgID: "org_pg_1", epoch: 1, targetGeneration: 1}}, 1); err == nil {
		t.Fatal("expected clearPendingTuplesDialectTx fail on exec error")
	}
	_ = tx3.Rollback()
	stub.setFailExec(false)

	pClosed, _ := NewStaticKEKProvider("kek_c", key1)
	stClosed, _ := NewStore(Config{DB: db, KEK: pClosed, PostShredHook: func(context.Context) error { return nil }})
	pClosed.Close()
	if err := stClosed.CheckServingReady(ctx); err == nil {
		t.Fatal("CheckServingReady should fail on closed KEK provider")
	}
	stClosed.Close()

	// Empty ActiveKeyID + mapVaultSQLiteError branches.
	emptyKEK := &emptyActiveIDKEK{inner: p}
	stEmptyID, _ := NewStore(Config{DB: db, KEK: emptyKEK, PostShredHook: func(context.Context) error { return nil }})
	if err := stEmptyID.CheckServingReady(ctx); err == nil {
		t.Fatal("CheckServingReady should fail on empty active key id")
	}
	stEmptyID.Close()
	if !errors.Is(mapVaultSQLiteError(sql.ErrNoRows, false), port.ErrNotFound) {
		t.Fatal("expected ErrNotFound for sql.ErrNoRows")
	}
	if !errors.Is(mapVaultSQLiteError(errors.New("UNIQUE constraint failed: t.k"), true), port.ErrAlreadyExists) {
		t.Fatal("expected ErrAlreadyExists on unique constraint")
	}
	if !errors.Is(mapVaultSQLiteError(errors.New("FOREIGN KEY constraint failed"), true), port.ErrResourceInUse) {
		t.Fatal("expected ErrResourceInUse")
	}
	if !errors.Is(mapVaultSQLiteError(errors.New("CHECK constraint failed"), false), ErrInvalidInput) {
		t.Fatal("expected ErrInvalidInput")
	}
	for _, tc := range []struct {
		code   string
		del    bool
		target error
	}{
		{"23505", false, port.ErrAlreadyExists},
		{"23503", true, port.ErrResourceInUse},
		{"23503", false, port.ErrNotFound},
		{"23514", false, port.ErrInvalidInput},
	} {
		if !errors.Is(mapVaultSQLiteError(&pgconn.PgError{Code: tc.code}, tc.del), tc.target) {
			t.Fatalf("PgError(%q, del=%v) did not map to %v", tc.code, tc.del, tc.target)
		}
	}
	if mapVaultSQLiteError(nil, false) != nil {
		t.Fatal("expected nil error for nil input")
	}
}

type emptyActiveIDKEK struct{ inner KEKProvider }

func (e *emptyActiveIDKEK) ActiveKeyID(context.Context) (string, error) { return "  ", nil }
func (e *emptyActiveIDKEK) WrapDEK(ctx context.Context, k, o string, v int64, d []byte) ([]byte, []byte, error) {
	return e.inner.WrapDEK(ctx, k, o, v, d)
}
func (e *emptyActiveIDKEK) UnwrapDEK(ctx context.Context, k, o string, v int64, n, c []byte) ([]byte, error) {
	return e.inner.UnwrapDEK(ctx, k, o, v, n, c)
}
func (e *emptyActiveIDKEK) WrapArchiveKey(ctx context.Context, k string, d []byte) ([]byte, []byte, error) {
	return e.inner.WrapArchiveKey(ctx, k, d)
}
func (e *emptyActiveIDKEK) UnwrapArchiveKey(ctx context.Context, k string, n, c []byte) ([]byte, error) {
	return e.inner.UnwrapArchiveKey(ctx, k, n, c)
}

type pgStubState struct {
	mu            sync.Mutex
	orgID         string
	projID        string
	orgKEKID      string
	orgNonce      []byte
	orgWrapped    []byte
	prjNonce      []byte
	prjWrapped    []byte
	secID         string
	secKind       string
	secNonce      []byte
	secCipher     []byte
	secOrgOnly    bool
	noOrgKey      bool
	setConfigs    int
	failSetConfig bool
	failQuery     bool
	failExec      bool
}

func (s *pgStubState) setConfigCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setConfigs
}

func (s *pgStubState) setFailSetConfig(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failSetConfig = v
}

func (s *pgStubState) setFailQuery(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failQuery = v
}

func (s *pgStubState) setFailExec(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failExec = v
}

type pgStubConnector struct{ state *pgStubState }

func (c *pgStubConnector) Connect(context.Context) (driver.Conn, error) {
	return &pgStubConn{state: c.state}, nil
}
func (c *pgStubConnector) Driver() driver.Driver { return nil }

type pgStubConn struct{ state *pgStubState }

func (c *pgStubConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c *pgStubConn) Close() error                        { return nil }
func (c *pgStubConn) Begin() (driver.Tx, error)           { return &pgStubTx{}, nil }
func (c *pgStubConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &pgStubTx{}, nil
}
func (c *pgStubConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	failExec := c.state.failExec
	failSet := c.state.failSetConfig
	if strings.Contains(query, "set_config(") {
		c.state.setConfigs++
	}
	c.state.mu.Unlock()
	if failExec {
		return nil, errors.New("injected exec failure")
	}
	if strings.Contains(query, "set_config(") && failSet {
		return nil, errors.New("injected set_config failure")
	}
	return driver.RowsAffected(1), nil
}
func (c *pgStubConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	q := strings.ToLower(strings.Join(strings.Fields(query), " "))
	st := c.state
	st.mu.Lock()
	defer st.mu.Unlock()
	if (st.failQuery || st.failExec) && !strings.Contains(q, "current_setting('server_version_num'") {
		return nil, errors.New("injected query failure")
	}

	switch {
	case strings.Contains(q, "current_setting('server_version_num'"):
		return newStubRows([]string{"current_setting"}, [][]driver.Value{{"160004"}}), nil
	case strings.Contains(q, "from organizations"):
		return newStubRows([]string{"id"}, [][]driver.Value{{st.orgID}}), nil
	case strings.Contains(q, "select 1 from org_encryption_keys"):
		if st.noOrgKey {
			return newStubRows([]string{"?column?"}, nil), nil
		}
		return newStubRows([]string{"?column?"}, [][]driver.Value{{int64(1)}}), nil
	case strings.Contains(q, "select org_id, kek_key_id, dek_version, nonce, wrapped_org_dek from org_encryption_keys"):
		if st.noOrgKey {
			return newStubRows([]string{"org_id", "kek_key_id", "dek_version", "nonce", "wrapped_org_dek"}, nil), nil
		}
		return newStubRows(
			[]string{"org_id", "kek_key_id", "dek_version", "nonce", "wrapped_org_dek"},
			[][]driver.Value{{st.orgID, st.orgKEKID, int64(1), st.orgNonce, st.orgWrapped}},
		), nil
	case strings.Contains(q, "select org_id, dek_version, nonce, wrapped_org_dek from org_encryption_keys"):
		if st.noOrgKey {
			return newStubRows([]string{"org_id", "dek_version", "nonce", "wrapped_org_dek"}, nil), nil
		}
		return newStubRows(
			[]string{"org_id", "dek_version", "nonce", "wrapped_org_dek"},
			[][]driver.Value{{st.orgID, int64(1), st.orgNonce, st.orgWrapped}},
		), nil
	case strings.Contains(q, "select kek_key_id, nonce, wrapped_org_dek, dek_version, key_generation from org_encryption_keys"):
		if st.noOrgKey {
			return newStubRows([]string{"kek_key_id", "nonce", "wrapped_org_dek", "dek_version", "key_generation"}, nil), nil
		}
		return newStubRows(
			[]string{"kek_key_id", "nonce", "wrapped_org_dek", "dek_version", "key_generation"},
			[][]driver.Value{{st.orgKEKID, st.orgNonce, st.orgWrapped, int64(1), int64(1)}},
		), nil
	case strings.Contains(q, "select project_id from project_encryption_keys"):
		return newStubRows([]string{"project_id"}, [][]driver.Value{{st.projID}}), nil
	case strings.Contains(q, "select org_dek_version, nonce, wrapped_project_dek, dek_version, key_generation from project_encryption_keys"):
		return newStubRows(
			[]string{"org_dek_version", "nonce", "wrapped_project_dek", "dek_version", "key_generation"},
			[][]driver.Value{{int64(1), st.prjNonce, st.prjWrapped, int64(1), int64(1)}},
		), nil
	case strings.Contains(q, "from secret_credentials"):
		var ownerProj driver.Value = st.projID
		if st.secOrgOnly {
			ownerProj = nil
		}
		return newStubRows(
			[]string{"id", "owner_project_id", "secret_kind", "aead_alg", "nonce", "ciphertext"},
			[][]driver.Value{{st.secID, ownerProj, st.secKind, "AES-256-GCM", st.secNonce, st.secCipher}},
		), nil
	case strings.Contains(q, "from vault_key_generations"):
		return newStubRows([]string{"last_generation"}, [][]driver.Value{{int64(3)}}), nil
	case strings.Contains(q, "returning config_epoch"):
		return newStubRows([]string{"config_epoch"}, [][]driver.Value{{int64(7)}}), nil
	case strings.Contains(q, "select coalesce(max(config_epoch)"):
		return newStubRows([]string{"epoch"}, [][]driver.Value{{int64(7)}}), nil
	default:
		return newStubRows([]string{"col"}, nil), nil
	}
}

type pgStubTx struct{}

func (*pgStubTx) Commit() error   { return nil }
func (*pgStubTx) Rollback() error { return nil }

type stubRows struct {
	cols []string
	data [][]driver.Value
	idx  int
}

func newStubRows(cols []string, data [][]driver.Value) *stubRows {
	return &stubRows{cols: cols, data: data}
}
func (r *stubRows) Columns() []string { return r.cols }
func (r *stubRows) Close() error      { return nil }
func (r *stubRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.idx])
	r.idx++
	return nil
}
