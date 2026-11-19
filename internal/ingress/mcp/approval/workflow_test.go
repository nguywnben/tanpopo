// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package approval

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/storage/port"
	_ "modernc.org/sqlite"
)

type approvalTestAudit struct{}

func (approvalTestAudit) Append(context.Context, port.AuditEvent) error            { return nil }
func (approvalTestAudit) AppendTx(context.Context, *sql.Tx, port.AuditEvent) error { return nil }

func newApprovalTestStore(t *testing.T) (*Store, *sql.DB, func() time.Time) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "approval.db")+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE approval_requests (
		id VARCHAR(40) PRIMARY KEY,
		org_id VARCHAR(40),
		project_id VARCHAR(40),
		action_kind VARCHAR(32) NOT NULL CHECK (action_kind IN ('MCP_DESTRUCTIVE_CALL','EXTENSION_ESCALATION','BUDGET_OVERRIDE','DEBUG_CAPTURE_ENABLE')),
		requester_virtual_key_id VARCHAR(40),
		requester_principal_id VARCHAR(40),
		requester_credential_principal_id VARCHAR(40),
		approved_mcp_server_id VARCHAR(40),
		approved_mcp_tool_id VARCHAR(40),
		approved_schema_sha256 CHAR(64),
		target_identifier VARCHAR(160) NOT NULL,
		payload_preview_json TEXT NOT NULL,
		params_jcs_sha256 CHAR(64) NOT NULL,
		state VARCHAR(24) NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING','APPROVED','CONSUMED','REJECTED','EXPIRED','CANCELLED')),
		pre_approval_token_hash BLOB UNIQUE,
		decided_by_principal_id VARCHAR(40),
		decision_reason TEXT,
		version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
		expires_at_ms BIGINT NOT NULL,
		created_at_ms BIGINT NOT NULL,
		decided_at_ms BIGINT,
		consumed_at_ms BIGINT,
		CHECK (decided_by_principal_id IS NULL OR requester_principal_id IS NULL OR decided_by_principal_id <> requester_principal_id)
	)`)
	if err != nil {
		t.Fatalf("create schema fixture: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE organizations (id VARCHAR(40) PRIMARY KEY)`); err != nil {
		t.Fatalf("create organization admission lock fixture: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO organizations(id) VALUES ('org_test')`); err != nil {
		t.Fatalf("seed organization admission lock fixture: %v", err)
	}
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	store, err := NewStore(Config{DB: db, Audit: approvalTestAudit{}, Now: nowFn, PollInterval: time.Millisecond})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store, db, nowFn
}

func sampleBinding() Binding {
	return Binding{
		OrgID: "org_test", VirtualKeyID: "vky_caller", ServerSlug: "payments",
		ToolName: "refund", Arguments: json.RawMessage(`{"amount":12.5,"charge":"ch_123"}`),
	}
}

func createTestRequest(t *testing.T, store *Store, now time.Time) CreatedRequest {
	t.Helper()
	created, err := store.Create(context.Background(), sampleBinding(), "prc_requester", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return created
}

func TestCreateJCSBindingAndPersistsOnlyTokenHash(t *testing.T) {
	store, db, now := newApprovalTestStore(t)
	created := createTestRequest(t, store, now())
	if created.State != StatePending || created.Version != 1 || created.PreApprovalToken == "" {
		t.Fatalf("unexpected creation result: %+v", created)
	}
	var storedHash []byte
	var digest string
	var state string
	if err := db.QueryRow(`SELECT pre_approval_token_hash, params_jcs_sha256, state FROM approval_requests WHERE id=?`, created.ID).Scan(&storedHash, &digest, &state); err != nil {
		t.Fatal(err)
	}
	wantTokenHash := sha256.Sum256([]byte(created.PreApprovalToken))
	if len(storedHash) != sha256.Size || string(storedHash) != string(wantTokenHash[:]) {
		t.Fatalf("persisted token hash does not match token digest: %x", storedHash)
	}
	if string(storedHash) == created.PreApprovalToken || state != string(StatePending) {
		t.Fatalf("raw token persisted or wrong initial state: state=%s hash=%x", state, storedHash)
	}
	_, wantDigest, _, err := validateAndBind(sampleBinding())
	if err != nil || digest != wantDigest {
		t.Fatalf("tuple digest mismatch: got %s want %s err=%v", digest, wantDigest, err)
	}
	lookup, err := store.Lookup(context.Background(), "org_test", "vky_caller", created.ID)
	if err != nil {
		t.Fatalf("lookup failed: request=%+v err=%v", lookup, err)
	}
	if _, err := store.Lookup(context.Background(), "org_test", "vky_other", created.ID); !hasCode(err, CodeNotFound) {
		t.Fatalf("wrong key lookup should be concealed as not found, got %v", err)
	}
}

func TestCreateBoundsPendingApprovalsAndPrunesExpiredData(t *testing.T) {
	store, db, now := newApprovalTestStore(t)
	store.maxPendingPerKey = 1
	first := createTestRequest(t, store, now())
	if _, err := store.Create(context.Background(), sampleBinding(), "prc_requester", now().Add(time.Minute)); !hasCode(err, CodeCapacity) {
		t.Fatalf("second live approval should hit per-key capacity, got %v", err)
	}
	later := now().Add(31 * 24 * time.Hour)
	store.now = func() time.Time { return later }
	second, err := store.Create(context.Background(), sampleBinding(), "prc_requester", later.Add(time.Hour))
	if err != nil {
		t.Fatalf("expired approval should release capacity: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM approval_requests WHERE id=?`, first.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired approval beyond retention remains persisted, count=%d err=%v", count, err)
	}
	var preview string
	if err := db.QueryRow(`SELECT payload_preview_json FROM approval_requests WHERE id=?`, second.ID).Scan(&preview); err != nil || preview == "{}" {
		t.Fatalf("live approval preview was pruned, preview=%q err=%v", preview, err)
	}
}

func TestConcurrentApprovalAdmissionHonorsCapacity(t *testing.T) {
	store, db, now := newApprovalTestStore(t)
	db.SetMaxOpenConns(8)
	store.maxPendingPerOrg = 3
	store.maxPendingPerKey = 10
	const attempts = 12
	start := make(chan struct{})
	results := make(chan error, attempts)
	var workers sync.WaitGroup
	for i := 0; i < attempts; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := store.Create(context.Background(), sampleBinding(), "prc_requester", now().Add(time.Minute))
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if hasCode(err, CodeCapacity) {
			rejected++
		} else {
			t.Fatalf("concurrent admission returned unexpected error: %v", err)
		}
	}
	var persisted int
	if err := db.QueryRow(`SELECT COUNT(*) FROM approval_requests WHERE org_id='org_test' AND state IN ('PENDING','APPROVED')`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if accepted != 3 || rejected != attempts-3 || persisted != 3 {
		t.Fatalf("concurrent admission exceeded capacity: accepted=%d rejected=%d persisted=%d", accepted, rejected, persisted)
	}
}

func TestCleanupExpiredRunsIndependentlyOfNewAdmissions(t *testing.T) {
	store, db, now := newApprovalTestStore(t)
	created := createTestRequest(t, store, now())
	base := now()
	store.now = func() time.Time { return base.Add(2 * time.Minute) }
	if err := store.CleanupExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	var preview string
	var tokenHash []byte
	if err := db.QueryRow(`SELECT payload_preview_json,pre_approval_token_hash FROM approval_requests WHERE id=?`, created.ID).Scan(&preview, &tokenHash); err != nil || preview != "{}" || tokenHash != nil {
		t.Fatalf("expired preview/token not minimized by sweeper: preview=%q token=%x err=%v", preview, tokenHash, err)
	}
	store.now = func() time.Time { return base.Add(31 * 24 * time.Hour) }
	if err := store.CleanupExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM approval_requests WHERE id=?`, created.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("old expired approval not pruned without new admission, count=%d err=%v", count, err)
	}
}

func TestDecideBindingVersionExpiryAndSelfApproval(t *testing.T) {
	store, _, now := newApprovalTestStore(t)
	ctx := context.Background()
	t.Run("self approval", func(t *testing.T) {
		created := createTestRequest(t, store, now())
		if _, err := store.Decide(ctx, created.ID, sampleBinding(), 1, "prc_requester", DecisionApprove, ""); !hasCode(err, CodeSelfApprovalForbidden) {
			t.Fatalf("expected self approval rejection, got %v", err)
		}
	})
	t.Run("binding mismatch", func(t *testing.T) {
		created := createTestRequest(t, store, now())
		changed := sampleBinding()
		changed.Arguments = json.RawMessage(`{"amount":12.5,"charge":"other"}`)
		if _, err := store.Decide(ctx, created.ID, changed, 1, "prc_reviewer", DecisionApprove, ""); !hasCode(err, CodeParameterHashMismatch) {
			t.Fatalf("expected binding mismatch, got %v", err)
		}
	})
	t.Run("version conflict", func(t *testing.T) {
		created := createTestRequest(t, store, now())
		if _, err := store.Decide(ctx, created.ID, sampleBinding(), 2, "prc_reviewer", DecisionApprove, ""); !hasCode(err, CodeVersionConflict) {
			t.Fatalf("expected version conflict, got %v", err)
		}
	})
	t.Run("expired", func(t *testing.T) {
		created, err := store.Create(ctx, sampleBinding(), "prc_requester", now().Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		// Move the injected clock beyond expiry without waiting.
		store.now = func() time.Time { return now().Add(2 * time.Second) }
		if _, err := store.Decide(ctx, created.ID, sampleBinding(), 1, "prc_reviewer", DecisionApprove, ""); !hasCode(err, CodeExpired) {
			t.Fatalf("expected expiry rejection, got %v", err)
		}
		store.now = now
		request, err := store.Lookup(ctx, "org_test", "vky_caller", created.ID)
		if err != nil || request.State != StateExpired || request.Version != 2 {
			t.Fatalf("expired transition missing: request=%+v err=%v", request, err)
		}
	})
}

func TestConsumeRequiresExactTupleAndIsSingleUse(t *testing.T) {
	store, db, now := newApprovalTestStore(t)
	ctx := context.Background()
	created := createTestRequest(t, store, now())
	approved, err := store.Decide(ctx, created.ID, sampleBinding(), 1, "prc_reviewer", DecisionApprove, "approved")
	if err != nil || approved.State != StateApproved || approved.Version != 2 {
		t.Fatalf("Decide: request=%+v err=%v", approved, err)
	}
	changed := sampleBinding()
	changed.ServerSlug = "other-server"
	if _, err := store.Consume(ctx, created.PreApprovalToken, changed); !hasCode(err, CodeParameterHashMismatch) {
		t.Fatalf("expected parameter hash mismatch, got %v", err)
	}
	consumed, err := store.Consume(ctx, created.PreApprovalToken, sampleBinding())
	if err != nil || consumed.State != StateConsumed || consumed.Version != 3 || consumed.ConsumedAt == nil {
		t.Fatalf("Consume: request=%+v err=%v", consumed, err)
	}
	if _, err := store.Consume(ctx, created.PreApprovalToken, sampleBinding()); !hasCode(err, CodeParameterHashMismatch) {
		t.Fatalf("second consume should fail as parameter mismatch, got %v", err)
	}
	var tokenHash []byte
	var state string
	if err := db.QueryRow(`SELECT pre_approval_token_hash, state FROM approval_requests WHERE id=?`, created.ID).Scan(&tokenHash, &state); err != nil || state != string(StateConsumed) || len(tokenHash) != sha256.Size {
		t.Fatalf("unexpected persisted consumed record: state=%s hash=%x err=%v", state, tokenHash, err)
	}
}

func TestConcurrentConsumeAllowsOnlyOneExecution(t *testing.T) {
	store, _, now := newApprovalTestStore(t)
	ctx := context.Background()
	created := createTestRequest(t, store, now())
	if _, err := store.Decide(ctx, created.ID, sampleBinding(), 1, "prc_reviewer", DecisionApprove, ""); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Consume(ctx, created.PreApprovalToken, sampleBinding())
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes, failures := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if hasCode(err, CodeInvalidState) || hasCode(err, CodeVersionConflict) || hasCode(err, CodeParameterHashMismatch) {
			failures++
		} else {
			t.Errorf("unexpected consume error: %v", err)
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("single-use invariant failed: successes=%d failures=%d", successes, failures)
	}
}

func TestCanonicalArgumentsRejectDuplicateKeysAndWaitIsBounded(t *testing.T) {
	if _, err := strictCanonicalJSON([]byte(`{"a":1,"a":2}`)); err == nil {
		t.Fatal("duplicate JSON keys must fail closed")
	}
	store, _, now := newApprovalTestStore(t)
	created := createTestRequest(t, store, now())
	request, err := store.Wait(context.Background(), "org_test", "vky_caller", created.ID, 5*time.Millisecond)
	if !hasCode(err, CodeWaitTimeout) || request.State != StatePending {
		t.Fatalf("expected bounded pending wait, request=%+v err=%v", request, err)
	}
	if MaxSynchronousWait != 60*time.Second {
		t.Fatalf("unexpected sync wait cap: %s", MaxSynchronousWait)
	}
}

func TestCanonicalArgumentsRejectPrecisionSensitiveIntegers(t *testing.T) {
	if got, err := CanonicalizeArguments([]byte(`{"id":9007199254740993}`)); err == nil {
		t.Fatalf("precision-sensitive integer was accepted as %s", got)
	}
	got, err := CanonicalizeArguments([]byte(`{"id":9007199254740991}`))
	if err != nil || string(got) != `{"id":9007199254740991}` {
		t.Fatalf("safe integer canonicalization = %s, err=%v", got, err)
	}
}

func TestTupleHashUsesJCSAndBindsEveryField(t *testing.T) {
	base := sampleBinding()
	base.Arguments = json.RawMessage(`{"outer":{"a":1,"b":2},"list":[true,"x"]}`)
	_, baseHash, _, err := validateAndBind(base)
	if err != nil {
		t.Fatal(err)
	}
	reordered := base
	reordered.Arguments = json.RawMessage(`{"list":[true,"x"],"outer":{"b":2,"a":1}}`)
	_, reorderedHash, _, err := validateAndBind(reordered)
	if err != nil || reorderedHash != baseHash {
		t.Fatalf("equivalent JCS arguments must have the same hash: %q %q err=%v", baseHash, reorderedHash, err)
	}
	mutations := []func(*Binding){
		func(b *Binding) { b.OrgID = "org_other" },
		func(b *Binding) { b.VirtualKeyID = "vky_other" },
		func(b *Binding) { b.ServerSlug = "other-server" },
		func(b *Binding) { b.ToolName = "other-tool" },
		func(b *Binding) { b.Arguments = json.RawMessage(`{"outer":{"a":9,"b":2},"list":[true,"x"]}`) },
	}
	for i, mutate := range mutations {
		changed := base
		mutate(&changed)
		_, hash, _, err := validateAndBind(changed)
		if err != nil || hash == baseHash {
			t.Errorf("binding field %d was not cryptographically bound: hash=%q err=%v", i, hash, err)
		}
	}
}

func TestLookupExpiresPendingRequest(t *testing.T) {
	store, _, now := newApprovalTestStore(t)
	created, err := store.Create(context.Background(), sampleBinding(), "prc_requester", now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now().Add(2 * time.Second) }
	request, err := store.Lookup(context.Background(), "org_test", "vky_caller", created.ID)
	if err != nil || request.State != StateExpired || request.Version != 2 {
		t.Fatalf("lookup did not expire pending request: request=%+v err=%v", request, err)
	}
}

func TestInvalidTokenAndRejectedApprovalCannotConsume(t *testing.T) {
	store, _, now := newApprovalTestStore(t)
	ctx := context.Background()
	created := createTestRequest(t, store, now())
	if _, err := store.Consume(ctx, "not-a-token", sampleBinding()); !hasCode(err, CodeNotFound) {
		t.Fatalf("unknown token should be rejected, got %v", err)
	}
	if _, err := store.Decide(ctx, created.ID, sampleBinding(), 1, "prc_reviewer", DecisionReject, "denied"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Consume(ctx, created.PreApprovalToken, sampleBinding()); !hasCode(err, CodeInvalidState) {
		t.Fatalf("rejected approval must not consume, got %v", err)
	}
	if code, ok := ErrorCodeOf(errors.New("other")); ok || code != "" {
		t.Fatal("untyped error returned an approval code")
	}
}

func hasCode(err error, expected ErrorCode) bool {
	code, ok := ErrorCodeOf(err)
	return ok && code == expected
}
