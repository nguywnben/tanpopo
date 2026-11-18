// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func TestResetLocalOwnerIsAuditedAndInvalidatesSessions(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "owner.db")
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	db := store.RawDB()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	service, err := auth.NewLocalService(auth.ServiceConfig{
		DB: db, AuditSink: auditLog,
		Argon2: auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
	})
	if err != nil {
		t.Fatal(err)
	}
	org, err := store.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "owner-recovery", DisplayName: "Owner Recovery"})
	if err != nil {
		t.Fatal(err)
	}
	masterKey := []byte("0123456789abcdef0123456789abcdef")
	kek, err := vault.NewStaticKEKProvider("kek_v1", masterKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kek.Close)
	vaultStore, err := vault.NewStore(vault.Config{DB: db, KEK: kek, PostShredHook: store.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vaultStore.EnsureOrgDEK(ctx, org.ID); err != nil {
		t.Fatal(err)
	}
	const oldPassword = "owner-password-before-reset-99"
	principal, err := service.CreatePrincipal(ctx, auth.CreatePrincipalParams{OrgID: &org.ID, Kind: auth.PrincipalKindHumanLocal, EmailOrSub: "owner@example.test", Password: oldPassword})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindRole(ctx, auth.BindRoleParams{OrgID: &org.ID, PrincipalID: principal.ID, ScopeType: rbac.ScopeOrg, ScopeID: org.ID, RoleName: rbac.RoleOrgOwner}); err != nil {
		t.Fatal(err)
	}
	other, err := service.CreatePrincipal(ctx, auth.CreatePrincipalParams{OrgID: &org.ID, Kind: auth.PrincipalKindHumanLocal, EmailOrSub: "operator@example.test", Password: "operator-password-before-reset-88"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindRole(ctx, auth.BindRoleParams{OrgID: &org.ID, PrincipalID: other.ID, ScopeType: rbac.ScopeOrg, ScopeID: org.ID, RoleName: rbac.RoleDeveloper}); err != nil {
		t.Fatal(err)
	}
	_, cookie, err := service.Login(ctx, "127.0.0.1", "owner@example.test", oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, otherCookie, err := service.Login(ctx, "127.0.0.1", "operator@example.test", "operator-password-before-reset-88")
	if err != nil {
		t.Fatal(err)
	}

	keyPath := filepath.Join(t.TempDir(), "master.key")
	otherOrg, err := store.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "wrong-owner-scope", DisplayName: "Wrong Owner Scope"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE role_bindings SET org_id = ?, scope_id = ? WHERE principal_id = ? AND role_name = ?`, otherOrg.ID, otherOrg.ID, principal.ID, rbac.RoleOrgOwner); err != nil {
		t.Fatalf("create mismatched owner binding fixture: %v", err)
	}
	if err := os.WriteFile(keyPath, masterKey, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANPOPO_MASTER_KEY_FILE", keyPath)
	t.Setenv("TANPOPO_MASTER_KEY", "")
	var mismatchedOut bytes.Buffer
	mismatchedRoot := &cobra.Command{Use: "tanpopo", SilenceErrors: true, SilenceUsage: true}
	mismatchedRoot.AddCommand(NewCommand(&mismatchedOut))
	mismatchedRoot.SetArgs([]string{"admin", "reset-local-owner", "--db", dbPath, "--email", "owner@example.test"})
	if err := mismatchedRoot.Execute(); testharness.CodeOf(err) != testharness.ExitInternal {
		t.Fatalf("mismatched ORG_OWNER scope must not authorize recovery, got %v", err)
	}
	if mismatchedOut.Len() != 0 {
		t.Fatalf("mismatched role binding emitted a recovery credential: %q", mismatchedOut.String())
	}
	if _, err := db.ExecContext(ctx, `UPDATE role_bindings SET org_id = ?, scope_id = ? WHERE principal_id = ? AND role_name = ?`, org.ID, org.ID, principal.ID, rbac.RoleOrgOwner); err != nil {
		t.Fatalf("restore valid owner binding fixture: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), 0o600); err != nil {
		t.Fatal(err)
	}

	var wrongOut bytes.Buffer
	wrongRoot := &cobra.Command{Use: "tanpopo", SilenceErrors: true, SilenceUsage: true}
	wrongRoot.AddCommand(NewCommand(&wrongOut))
	wrongRoot.SetArgs([]string{"admin", "reset-local-owner", "--db", dbPath, "--email", "owner@example.test"})
	if err := wrongRoot.Execute(); testharness.CodeOf(err) != testharness.ExitInternal {
		t.Fatalf("unrelated readable key should fail against the database KEK, got %v", err)
	}
	if wrongOut.Len() != 0 {
		t.Fatalf("failed key proof emitted a recovery credential: %q", wrongOut.String())
	}
	if err := os.WriteFile(keyPath, masterKey, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	root := &cobra.Command{Use: "tanpopo", SilenceErrors: true, SilenceUsage: true}
	format := "json"
	root.AddCommand(NewCommand(&stdout, &format))
	root.SetArgs([]string{"admin", "reset-local-owner", "--db", dbPath, "--email", "owner@example.test"})
	root.SetErr(&stderr)
	if err := root.Execute(); err != nil {
		t.Fatalf("reset command failed: %v (%s)", err, stderr.String())
	}
	var result map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("expected a single JSON output object, got %q: %v", stdout.String(), err)
	}
	newPassword := result["recovery_password"]
	if len(newPassword) < auth.MinOwnerPasswordLength {
		t.Fatalf("recovery password is too short: %d", len(newPassword))
	}
	if _, _, err := service.Login(ctx, "127.0.0.1", "owner@example.test", oldPassword); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("old password still authenticates: %v", err)
	}
	if _, _, err := service.Login(ctx, "127.0.0.1", "owner@example.test", newPassword); err != nil {
		t.Fatalf("recovery password does not authenticate: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "https://gateway.local/admin/v1/auth/session", nil)
	req.AddCookie(cookie)
	if _, _, err := service.AuthenticateAdminHTTPRequest(req); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("old session was not invalidated: %v", err)
	}
	otherReq := httptest.NewRequest(http.MethodGet, "https://gateway.local/admin/v1/auth/session", nil)
	otherReq.AddCookie(otherCookie)
	if _, _, err := service.AuthenticateAdminHTTPRequest(otherReq); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("another active browser session was not invalidated: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = ? AND chain_key = 'SYSTEM'`, "LOCAL_BREAK_GLASS_OWNER_RESET").Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected one SYSTEM reset audit event, count=%d err=%v", count, err)
	}
	report, err := audit.VerifyAuditChain(ctx, db, audit.VerifyOptions{})
	if err != nil || !report.Valid {
		t.Fatalf("SYSTEM audit chain failed verification: report=%+v err=%v", report, err)
	}
}

func TestResetLocalOwnerRequiresReadableRestrictedMasterKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(keyPath, []byte("0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANPOPO_MASTER_KEY_FILE", keyPath)
	t.Setenv("TANPOPO_MASTER_KEY", "")
	var stdout bytes.Buffer
	root := &cobra.Command{Use: "tanpopo", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(NewCommand(&stdout))
	root.SetArgs([]string{"admin", "reset-local-owner", "--db", filepath.Join(t.TempDir(), "missing.db")})
	err := root.Execute()
	if testharness.CodeOf(err) != testharness.ExitSecurity {
		t.Fatalf("expected security exit code, got %d (%v)", testharness.CodeOf(err), err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed recovery leaked output: %q", stdout.String())
	}
}
