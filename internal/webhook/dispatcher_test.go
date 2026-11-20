// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func openWebhookDB(t *testing.T) *sql.DB {
	return openWebhookDBWithJournal(t, "WAL", 5000)
}

func openWebhookDBWithJournal(t *testing.T, journal string, busyTimeoutMs int) *sql.DB {
	t.Helper()
	dbDir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dbDir, "webhook.db")+"?_pragma=busy_timeout("+strconv.Itoa(busyTimeoutMs)+")&_pragma=journal_mode("+journal+")")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() {
		stats := db.Stats()
		if stats.InUse != 0 {
			t.Errorf("webhook test left %d SQLite connection(s) in use at cleanup", stats.InUse)
		}
		if err := db.Close(); err != nil {
			t.Errorf("close webhook test database: %v", err)
		}
		var removeErr error
		for attempt := 0; attempt < 200; attempt++ {
			removeErr = os.RemoveAll(dbDir)
			if removeErr == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Errorf("remove webhook test database directory after close: %v", removeErr)
	})
	for _, ddl := range []string{
		`CREATE TABLE organizations (id TEXT PRIMARY KEY)`,
		`CREATE TABLE secret_credentials (id TEXT NOT NULL, org_id TEXT NOT NULL, owner_project_id TEXT, bound_audience TEXT, PRIMARY KEY(org_id,id))`,
		`CREATE TABLE webhook_subscriptions (id TEXT NOT NULL, org_id TEXT NOT NULL, project_id TEXT, name TEXT NOT NULL, target_url TEXT NOT NULL, target_url_credential_id TEXT NOT NULL, allow_private_ip INTEGER NOT NULL DEFAULT 0, allowed_cidrs_json TEXT NOT NULL DEFAULT '[]', signing_secret_credential_id TEXT NOT NULL, event_types_json TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1, created_at_ms INTEGER NOT NULL, updated_at_ms INTEGER NOT NULL, PRIMARY KEY (org_id,id))`,
		`CREATE TABLE webhook_deliveries (id TEXT NOT NULL, org_id TEXT NOT NULL, subscription_id TEXT NOT NULL, event_id TEXT NOT NULL, event_type TEXT NOT NULL, payload_json TEXT NOT NULL, attempt_count INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'PENDING', next_attempt_at_ms INTEGER NOT NULL, last_http_status INTEGER, last_error TEXT, created_at_ms INTEGER NOT NULL, delivered_at_ms INTEGER, terminal_at_ms INTEGER, PRIMARY KEY (org_id,id))`,
		`CREATE UNIQUE INDEX idx_webhook_delivery_event_subscription ON webhook_deliveries(org_id,subscription_id,event_id)`,
		`CREATE TABLE webhook_event_outbox (id TEXT PRIMARY KEY,org_id TEXT NOT NULL,project_id TEXT,event_type TEXT NOT NULL,payload_json TEXT NOT NULL,created_at_ms INTEGER NOT NULL,dispatched_at_ms INTEGER)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO organizations(id) VALUES ('org_test')`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestEnqueueCommitBusyDoesNotLeakTransactionToPool(t *testing.T) {
	db := openWebhookDBWithJournal(t, "DELETE", 25)
	insertSubscription(t, db, "https://hooks.example.test/path", false, `[]`)
	reader, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var orgID string
	if err := reader.QueryRowContext(t.Context(), `SELECT id FROM organizations WHERE id='org_test'`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{DB: db, Unseal: func(context.Context, string, string) ([]byte, func(), error) { return []byte("k"), func() {}, nil }, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return nil, nil })})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Enqueue(t.Context(), Event{ID: "commit-busy", OrgID: orgID, Type: "budget.threshold", Payload: json.RawMessage(`{"threshold":80}`)}); err == nil {
		t.Fatal("expected reader lock to force an enqueue commit failure")
	}
	if err := reader.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Enqueue(t.Context(), Event{ID: "after-commit-busy", OrgID: orgID, Type: "budget.threshold", Payload: json.RawMessage(`{"threshold":80}`)}); err != nil {
		t.Fatalf("connection remained contaminated after failed commit: %v", err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM webhook_deliveries WHERE event_id='after-commit-busy'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("post-lock enqueue count=%d err=%v", count, err)
	}
}

func TestTransactionalOutboxRecoversWithoutDuplicatingFanout(t *testing.T) {
	db := openWebhookDB(t)
	insertSubscription(t, db, "https://hooks.example.test/path", false, `[]`)
	d, err := New(Config{DB: db, Unseal: func(context.Context, string, string) ([]byte, func(), error) { return []byte("k"), func() {}, nil }, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.5")}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	id := StableEventID("budget:ledger:ALT-BUDGET-80")
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.RecordEventTx(t.Context(), tx, Event{ID: id, OrgID: "org_test", Type: "budget.threshold", Payload: json.RawMessage(`{"code":"ALT-BUDGET-80"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	d.dispatchOutbox(t.Context())
	d.dispatchOutbox(t.Context())
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries WHERE event_id=?`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("stable outbox fanout count=%d err=%v", count, err)
	}
	var dispatched sql.NullInt64
	if err := db.QueryRow(`SELECT dispatched_at_ms FROM webhook_event_outbox WHERE id=?`, id).Scan(&dispatched); err != nil || !dispatched.Valid {
		t.Fatalf("outbox event not acknowledged: %v %v", dispatched, err)
	}
}

func TestConcurrentOutboxAdmissionHonorsOrganizationCapacity(t *testing.T) {
	db := openWebhookDB(t)
	d, err := New(Config{DB: db, Unseal: func(context.Context, string, string) ([]byte, func(), error) { return []byte("k"), func() {}, nil }, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return nil, nil })})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`WITH RECURSIVE seq(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM seq WHERE x<4999)
		INSERT INTO webhook_event_outbox(id,org_id,event_type,payload_json,created_at_ms)
		SELECT 'old'||x,'org_test','budget.threshold','{}',1 FROM seq`); err != nil {
		t.Fatal(err)
	}
	const attempts = 12
	start := make(chan struct{})
	results := make(chan error, attempts)
	var workers sync.WaitGroup
	for i := 0; i < attempts; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			tx, err := db.BeginTx(context.Background(), nil)
			if err == nil {
				err = d.RecordEventTx(context.Background(), tx, Event{ID: "outbox-" + strconv.Itoa(i), OrgID: "org_test", Type: "budget.threshold", Payload: json.RawMessage(`{"threshold":80}`)})
			}
			if err == nil {
				err = tx.Commit()
			} else if tx != nil {
				_ = tx.Rollback()
			}
			results <- err
		}(i)
	}
	close(start)
	workers.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if strings.Contains(err.Error(), "outbox capacity reached") {
			rejected++
		} else {
			t.Fatalf("concurrent outbox admission returned unexpected error: %v", err)
		}
	}
	var persisted int
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_event_outbox WHERE org_id='org_test' AND dispatched_at_ms IS NULL`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if accepted != 1 || rejected != attempts-1 || persisted != maxOutstandingPerOrg {
		t.Fatalf("concurrent outbox admission exceeded capacity: accepted=%d rejected=%d persisted=%d", accepted, rejected, persisted)
	}
}

func TestPostgresDispatcherRequiresAssumedWorkerRole(t *testing.T) {
	db := openWebhookDB(t)
	_, err := New(Config{DB: db, Postgres: true,
		Unseal:   func(context.Context, string, string) ([]byte, func(), error) { return []byte("k"), func() {}, nil },
		Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return nil, nil }),
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("PostgreSQL dispatcher must fail closed without verified worker role, got %v", err)
	}
}

type roleVerificationFailureDriver struct{ closed atomic.Int32 }

func (d *roleVerificationFailureDriver) Open(string) (driver.Conn, error) {
	return &roleVerificationFailureConn{driver: d}, nil
}

type roleVerificationFailureConn struct {
	driver *roleVerificationFailureDriver
}

func (c *roleVerificationFailureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (c *roleVerificationFailureConn) Close() error {
	c.driver.closed.Add(1)
	return nil
}
func (c *roleVerificationFailureConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected Begin")
}
func (c *roleVerificationFailureConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if query == `SET ROLE tanpopo_webhook_worker` {
		return driver.RowsAffected(0), nil
	}
	return nil, errors.New("unexpected ExecContext")
}
func (c *roleVerificationFailureConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if query == `SELECT current_user` {
		return nil, errors.New("verification query failed")
	}
	return nil, errors.New("unexpected QueryContext")
}

var roleFailureDriver = &roleVerificationFailureDriver{}
var registerRoleFailureDriver sync.Once

func TestPostgresDispatcherDiscardsConnectionAfterRoleVerificationFailure(t *testing.T) {
	registerRoleFailureDriver.Do(func() { sql.Register("tanpopo-webhook-role-failure", roleFailureDriver) })
	db, err := sql.Open("tanpopo-webhook-role-failure", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = New(Config{DB: db, Postgres: true,
		Unseal:   func(context.Context, string, string) ([]byte, func(), error) { return []byte("k"), func() {}, nil },
		Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return nil, nil }),
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("failed role verification must fail closed, got %v", err)
	}
	if got := roleFailureDriver.closed.Load(); got == 0 {
		t.Fatal("connection with an unverified role was returned to the pool")
	}
}

func TestRunCancellationStopsClaimBeforeCommitGate(t *testing.T) {
	db := openWebhookDB(t)
	insertSubscription(t, db, "https://hooks.example.test/path", false, `[]`)
	d := testDispatcher(t, db, resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, nil
	}), 3)
	if _, err := d.Enqueue(t.Context(), Event{ID: "cancelled-claim", OrgID: "org_test", Type: "budget.threshold", Payload: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	d.beforeClaimCommit = func() {
		cancel()
		deadline := time.Now().Add(time.Second)
		for d.claimGate.Load() == claimGateActive && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if d.claimGate.Load() != claimGateStopping {
			t.Errorf("Run cancellation did not win the claim gate before timeout; state=%d", d.claimGate.Load())
		}
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("dispatcher Run did not finish after cancellation")
	}
	var attempts int
	var status string
	if err := db.QueryRow(`SELECT attempt_count, status FROM webhook_deliveries WHERE event_id='cancelled-claim'`).Scan(&attempts, &status); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || status != statusPending {
		t.Fatalf("shutdown consumed a canceled delivery attempt: attempts=%d status=%s", attempts, status)
	}
}

func TestStopClaimingWinsClaimCommitGate(t *testing.T) {
	db := openWebhookDB(t)
	insertSubscription(t, db, "https://hooks.example.test/path", false, `[]`)
	d := testDispatcher(t, db, resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, nil
	}), 3)
	if _, err := d.Enqueue(t.Context(), Event{ID: "stopped-claim", OrgID: "org_test", Type: "budget.threshold", Payload: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	d.beforeClaimCommit = func() {
		go func() {
			d.StopClaiming()
			close(stopped)
		}()
		deadline := time.Now().Add(time.Second)
		for d.claimGate.Load() == claimGateActive && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if d.claimGate.Load() != claimGateStopping {
			t.Errorf("StopClaiming did not win the commit gate before timeout; state=%d", d.claimGate.Load())
		}
	}
	item, err := d.claim(t.Context())
	if err != nil || item != nil {
		t.Fatalf("claim after shutdown won commit gate = (%v, %v), want (nil, nil)", item, err)
	}
	var attempts int
	var status string
	if err := db.QueryRow(`SELECT attempt_count, status FROM webhook_deliveries WHERE event_id='stopped-claim'`).Scan(&attempts, &status); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || status != statusPending {
		t.Fatalf("shutdown consumed a delivery attempt: attempts=%d status=%s", attempts, status)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("StopClaiming did not finish after the claim transaction rolled back")
	}
}

func TestClaimCommitGateWinsBeforeStopClaiming(t *testing.T) {
	db := openWebhookDB(t)
	insertSubscription(t, db, "https://hooks.example.test/path", false, `[]`)
	d := testDispatcher(t, db, resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, nil
	}), 3)
	if _, err := d.Enqueue(t.Context(), Event{ID: "committing-claim", OrgID: "org_test", Type: "budget.threshold", Payload: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	d.afterClaimCommitGate = func() {
		go func() {
			d.StopClaiming()
			close(stopped)
		}()
		deadline := time.Now().Add(time.Second)
		for d.claimGate.Load() == claimGateCommitting && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if d.claimGate.Load() != claimGateStopAfterCommit {
			t.Errorf("StopClaiming did not wait behind an already-started commit; state=%d", d.claimGate.Load())
		}
	}
	item, err := d.claim(t.Context())
	if err != nil || item == nil {
		t.Fatalf("claim whose commit gate won = (%v, %v), want a committed claim", item, err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("StopClaiming did not finish after the in-flight claim committed")
	}
	var attempts int
	if err := db.QueryRow(`SELECT attempt_count FROM webhook_deliveries WHERE event_id='committing-claim'`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("in-flight committed claim attempts = %d, want 1", attempts)
	}
	item, err = d.claim(t.Context())
	if err != nil || item != nil {
		t.Fatalf("claim after StopClaiming = (%v, %v), want (nil, nil)", item, err)
	}
}

func TestDispatcherRunIsOneShot(t *testing.T) {
	db := openWebhookDB(t)
	d := testDispatcher(t, db, resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, nil
	}), 3)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := d.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.Run(t.Context()); err == nil {
		t.Fatal("second Run succeeded after shutdown closed the claim boundary")
	}
}

func TestFanoutChecksRemainingCapacityForAllSubscriptions(t *testing.T) {
	db := openWebhookDB(t)
	insertSubscription(t, db, "https://hooks.example.test/path", false, `[]`)
	if _, err := db.Exec(`INSERT INTO webhook_subscriptions(id,org_id,name,target_url,target_url_credential_id,signing_secret_credential_id,event_types_json,created_at_ms,updated_at_ms)
		VALUES('whk_second','org_test','second','https://hooks.example.test','sec_target','sec_test','["budget.threshold"]',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`WITH RECURSIVE seq(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM seq WHERE x<4999)
		INSERT INTO webhook_deliveries(id,org_id,subscription_id,event_id,event_type,payload_json,attempt_count,status,next_attempt_at_ms,created_at_ms)
		SELECT 'd'||x,'org_test','whk_test','old'||x,'budget.threshold','{}',0,'PENDING',1,1 FROM seq`); err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{DB: db, Unseal: func(context.Context, string, string) ([]byte, func(), error) { return []byte("k"), func() {}, nil }, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return nil, nil })})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Enqueue(t.Context(), Event{OrgID: "org_test", Type: "budget.threshold", Payload: json.RawMessage(`{"threshold":80}`)}); err == nil {
		t.Fatal("fan-out exceeding remaining capacity was accepted")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries WHERE org_id='org_test' AND status='PENDING'`).Scan(&count); err != nil || count != 4999 {
		t.Fatalf("capacity failure partially inserted deliveries, count=%d err=%v", count, err)
	}
}

func TestConcurrentFanoutAdmissionHonorsOrganizationCapacity(t *testing.T) {
	db := openWebhookDB(t)
	insertSubscription(t, db, "https://hooks.example.test/path", false, `[]`)
	if _, err := db.Exec(`WITH RECURSIVE seq(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM seq WHERE x<4999)
		INSERT INTO webhook_deliveries(id,org_id,subscription_id,event_id,event_type,payload_json,attempt_count,status,next_attempt_at_ms,created_at_ms)
		SELECT 'd'||x,'org_test','whk_test','old'||x,'budget.threshold','{}',0,'PENDING',1,1 FROM seq`); err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{DB: db, Unseal: func(context.Context, string, string) ([]byte, func(), error) { return []byte("k"), func() {}, nil }, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return nil, nil })})
	if err != nil {
		t.Fatal(err)
	}
	const attempts = 12
	start := make(chan struct{})
	results := make(chan error, attempts)
	var workers sync.WaitGroup
	for i := 0; i < attempts; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			_, err := d.Enqueue(context.Background(), Event{ID: "concurrent-" + strconv.Itoa(i), OrgID: "org_test", Type: "budget.threshold", Payload: json.RawMessage(`{"threshold":80}`)})
			results <- err
		}(i)
	}
	close(start)
	workers.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if strings.Contains(err.Error(), "delivery capacity reached") {
			rejected++
		} else {
			t.Fatalf("concurrent fan-out returned unexpected error: %v", err)
		}
	}
	var persisted int
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries WHERE org_id='org_test' AND status='PENDING'`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if accepted != 1 || rejected != attempts-1 || persisted != maxOutstandingPerOrg {
		t.Fatalf("concurrent fan-out exceeded capacity: accepted=%d rejected=%d persisted=%d", accepted, rejected, persisted)
	}
}

func TestCleanupRetainsLiveWorkAndExpiresTerminalRows(t *testing.T) {
	db := openWebhookDB(t)
	now := time.Unix(1_800_000_000, 0).UTC()
	d, err := New(Config{
		DB: db, Now: func() time.Time { return now },
		Limits:   Limits{TerminalRetention: time.Hour},
		Unseal:   func(context.Context, string, string) ([]byte, func(), error) { return []byte("k"), func() {}, nil },
		Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return nil, nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-2 * time.Hour).UnixMilli()
	for _, row := range []struct {
		id, status string
		terminal   any
	}{{"delivered", statusDelivered, now.Add(-30 * time.Minute).UnixMilli()}, {"dead", statusDead, old}, {"retry", statusRetry, nil}} {
		if _, err := db.Exec(`INSERT INTO webhook_deliveries(id,org_id,subscription_id,event_id,event_type,payload_json,attempt_count,status,next_attempt_at_ms,created_at_ms,terminal_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, row.id, "org_test", "whk_test", row.id, "budget.threshold", `{}`, 1, row.status, old, old, row.terminal); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO webhook_event_outbox(id,org_id,event_type,payload_json,created_at_ms,dispatched_at_ms) VALUES('old','org_test','budget.threshold','{}',?,?)`, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO webhook_event_outbox(id,org_id,event_type,payload_json,created_at_ms) VALUES('pending','org_test','budget.threshold','{}',?)`, old); err != nil {
		t.Fatal(err)
	}
	d.cleanupTerminal(t.Context())
	var deliveries, outbox int
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries`).Scan(&deliveries); err != nil || deliveries != 2 {
		t.Fatalf("expected recent terminal and retry rows to remain, deliveries=%d err=%v", deliveries, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_event_outbox`).Scan(&outbox); err != nil || outbox != 1 {
		t.Fatalf("expected pending outbox row to remain only, outbox=%d err=%v", outbox, err)
	}
}

func insertSubscription(t *testing.T, db *sql.DB, target string, allowPrivate bool, cidrs string) {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	origin := u.Scheme + "://" + u.Host
	if _, err := db.Exec(`INSERT INTO secret_credentials(id,org_id,owner_project_id,bound_audience) VALUES(?,?,NULL,?)`, "sec_test", "org_test", origin); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO secret_credentials(id,org_id,owner_project_id,bound_audience) VALUES(?,?,NULL,?)`, "sec_target", "org_test", target); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO webhook_subscriptions (id,org_id,name,target_url,target_url_credential_id,allow_private_ip,allowed_cidrs_json,signing_secret_credential_id,event_types_json,enabled,created_at_ms,updated_at_ms) VALUES (?,?,?,?,?,?,?,?,?,1,1,1)`,
		"whk_test", "org_test", "test", origin, "sec_target", allowPrivate, cidrs, "sec_test", `["budget.threshold"]`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEnqueueRedactsBeforePersistAndDeliverySignsExactStoredBody(t *testing.T) {
	db := openWebhookDB(t)
	var captureMu sync.Mutex
	var gotBody []byte
	var gotSignature, gotTimestamp string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captureMu.Lock()
		gotBody = body
		gotSignature = r.Header.Get("X-Tanpopo-Signature-256")
		gotTimestamp = r.Header.Get("X-Tanpopo-Timestamp")
		captureMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	target := strings.Replace(srv.URL, "127.0.0.1", "hooks.test", 1)
	insertSubscription(t, db, target, true, `["127.0.0.0/8"]`)
	fixed := time.Unix(1_800_000_000, 0).UTC()
	secret := []byte("unit-test-signing-key")
	zeroed := atomic.Bool{}
	d, err := New(Config{
		DB: db,
		Unseal: func(_ context.Context, _, credentialID string) ([]byte, func(), error) {
			plain := append([]byte(nil), secret...)
			if credentialID == "sec_target" {
				plain = []byte(target)
			}
			return plain, func() { clear(plain); zeroed.Store(true) }, nil
		},
		Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}),
		Now:    func() time.Time { return fixed },
		Limits: Limits{Workers: 1, PollInterval: 2 * time.Millisecond, RequestTimeout: time.Second, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"api_key":"secret-value","message":"hello"}`)
	n, err := d.Enqueue(context.Background(), Event{ID: "evt_test", OrgID: "org_test", Type: "budget.threshold", Payload: raw})
	if err != nil || n != 1 {
		t.Fatalf("Enqueue = %d, %v", n, err)
	}
	var persisted string
	if err := db.QueryRow(`SELECT payload_json FROM webhook_deliveries`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(persisted, "secret-value") || !strings.Contains(persisted, "[REDACTED]") {
		t.Fatalf("payload not redacted before persistence: %s", persisted)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	waitDelivery(t, db, statusDelivered)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	captureMu.Lock()
	defer captureMu.Unlock()
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(gotTimestamp + "."))
	_, _ = mac.Write(gotBody)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSignature != want {
		t.Fatalf("signature mismatch: got %q want %q", gotSignature, want)
	}
	if string(gotBody) != persisted {
		t.Fatalf("signed body differs from persisted body: %q != %q", gotBody, persisted)
	}
	if gotTimestamp != strconv.FormatInt(fixed.Unix(), 10) {
		t.Fatalf("timestamp = %q", gotTimestamp)
	}
	if !zeroed.Load() {
		t.Fatal("unsealed signing key zeroizer was not called")
	}
}

func TestEnqueueScopeAndEventFilter(t *testing.T) {
	db := openWebhookDB(t)
	insertSubscription(t, db, "https://hooks.example.test/path", false, `[]`)
	if _, err := db.Exec(`INSERT INTO webhook_subscriptions (id,org_id,name,target_url,target_url_credential_id,signing_secret_credential_id,event_types_json,enabled,created_at_ms,updated_at_ms) VALUES ('whk_other','org_else','other','https://hooks.example.test','sec_target','sec_test','["budget.threshold"]',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{DB: db, Unseal: func(context.Context, string, string) ([]byte, func(), error) { return []byte("k"), func() {}, nil }, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.5")}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	n, err := d.Enqueue(context.Background(), Event{ID: "evt1", OrgID: "org_test", Type: "unmatched", Payload: json.RawMessage(`{"ok":true}`)})
	if err != nil || n != 0 {
		t.Fatalf("unmatched Enqueue = %d, %v", n, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cross-org or unmatched delivery inserted: count=%d err=%v", count, err)
	}
}

func TestWorkerRejectsMetadataAndCrossOriginRedirectWithoutLeakingTarget(t *testing.T) {
	t.Run("metadata", func(t *testing.T) {
		db := openWebhookDB(t)
		insertSubscription(t, db, "http://hooks.test:8080/hook?token=private", false, `[]`)
		d := testDispatcher(t, db, resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
		}), 1)
		enqueueAndRunToStatus(t, d, db, statusDead)
		var safeErr string
		if err := db.QueryRow(`SELECT last_error FROM webhook_deliveries`).Scan(&safeErr); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(safeErr, "token") || strings.Contains(safeErr, "169.254") {
			t.Fatalf("unsafe stored error: %q", safeErr)
		}
	})
	t.Run("cross-origin redirect", func(t *testing.T) {
		db := openWebhookDB(t)
		var destinationHits atomic.Int32
		destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationHits.Add(1) }))
		defer destination.Close()
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
		}))
		defer origin.Close()
		target := strings.Replace(origin.URL, "127.0.0.1", "hooks.test", 1)
		insertSubscription(t, db, target, true, `["127.0.0.0/8"]`)
		resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		})
		d := testDispatcher(t, db, resolver, 1)
		enqueueAndRunToStatus(t, d, db, statusDead)
		if destinationHits.Load() != 0 {
			t.Fatalf("cross-origin redirect reached destination %d times", destinationHits.Load())
		}
	})
}

func TestMissingSecretZeroizerFailsClosed(t *testing.T) {
	db := openWebhookDB(t)
	insertSubscription(t, db, "https://hooks.example.test/hook", false, `[]`)
	d := testDispatcher(t, db, resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.5")}, nil
	}), 1)
	d.cfg.Unseal = func(context.Context, string, string) ([]byte, func(), error) { return []byte("secret"), nil, nil }
	enqueueAndRunToStatus(t, d, db, statusDead)
	var attempts int
	if err := db.QueryRow(`SELECT attempt_count FROM webhook_deliveries`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("attempt count=%d", attempts)
	}
}

func testDispatcher(t *testing.T, db *sql.DB, resolver egressResolver, maxAttempts int) *Dispatcher {
	t.Helper()
	d, err := New(Config{
		DB: db,
		Unseal: func(context.Context, string, string) ([]byte, func(), error) {
			return []byte("test-key"), func() {}, nil
		},
		Resolver: resolver,
		Now:      time.Now,
		Limits:   Limits{Workers: 1, MaxAttempts: maxAttempts, PollInterval: time.Millisecond, RequestTimeout: time.Second, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

type egressResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

func enqueueAndRunToStatus(t *testing.T, d *Dispatcher, db *sql.DB, status string) {
	t.Helper()
	if _, err := d.Enqueue(context.Background(), Event{ID: "evt_test", OrgID: "org_test", Type: "budget.threshold", Payload: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	waitDelivery(t, db, status)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func waitDelivery(t *testing.T, db *sql.DB, status string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var got string
		err := db.QueryRow(`SELECT status FROM webhook_deliveries LIMIT 1`).Scan(&got)
		if err == nil && got == status {
			return
		}
		time.Sleep(time.Millisecond)
	}
	var got string
	err := db.QueryRow(`SELECT status FROM webhook_deliveries LIMIT 1`).Scan(&got)
	t.Fatalf("delivery status did not reach %s: got=%s err=%v", status, got, err)
}
