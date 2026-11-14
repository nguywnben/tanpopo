package spikes_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

var errBudgetExceeded = errors.New("429 project_budget_exceeded")

func openSpikeSQLiteWAL(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "spike03_ledger.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// SQLite WAL supports 1 writer at a time; serialized write connection pool eliminates SQLITE_BUSY races deterministically.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	ddl := `
	CREATE TABLE budget_ledgers (
		id TEXT PRIMARY KEY,
		limit_micro_usd INTEGER NOT NULL,
		settled_micro_usd INTEGER NOT NULL DEFAULT 0,
		reserved_micro_usd INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE budget_reservations (
		id TEXT PRIMARY KEY,
		ledger_id TEXT NOT NULL REFERENCES budget_ledgers(id),
		held_micro_usd INTEGER NOT NULL,
		state TEXT NOT NULL
	);
	CREATE TABLE usage_settlements (
		id TEXT PRIMARY KEY,
		ledger_id TEXT NOT NULL REFERENCES budget_ledgers(id),
		request_id TEXT NOT NULL UNIQUE,
		settled_micro_usd INTEGER NOT NULL,
		accuracy_tier TEXT NOT NULL CHECK (accuracy_tier IN ('EXACT','ESTIMATED','UNMETERED_FLAGGED'))
	);`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("create ledger ddl: %v", err)
	}
	return db
}

func reserveMicroUSD(ctx context.Context, db *sql.DB, ledgerID, resID string, holdMicros int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE budget_ledgers
		SET reserved_micro_usd = reserved_micro_usd + ?
		WHERE id = ? AND (limit_micro_usd - settled_micro_usd - reserved_micro_usd) >= ?`,
		holdMicros, ledgerID, holdMicros,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errBudgetExceeded
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO budget_reservations(id, ledger_id, held_micro_usd, state) VALUES (?, ?, ?, 'RESERVED_HOLD')`,
		resID, ledgerID, holdMicros); err != nil {
		return err
	}
	return tx.Commit()
}

func settleMicroUSD(ctx context.Context, db *sql.DB, ledgerID, resID, reqID string, actualMicros int64, tier string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var held int64
	if err := tx.QueryRowContext(ctx, `SELECT held_micro_usd FROM budget_reservations WHERE id = ? AND state = 'RESERVED_HOLD'`, resID).Scan(&held); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE budget_reservations SET state = 'SETTLED' WHERE id = ?`, resID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE budget_ledgers SET reserved_micro_usd = reserved_micro_usd - ?, settled_micro_usd = settled_micro_usd + ? WHERE id = ?`,
		held, actualMicros, ledgerID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_settlements(id, ledger_id, request_id, settled_micro_usd, accuracy_tier) VALUES (?, ?, ?, ?, ?)`,
		"set_"+resID, ledgerID, reqID, actualMicros, tier); err != nil {
		return err
	}
	return tx.Commit()
}

func TestSpike03ConcurrentTwoPhaseHoldAndAbortLatency(t *testing.T) {
	ctx := context.Background()
	db := openSpikeSQLiteWAL(t)

	// Limit = 12,500 micro-USD; each request holds 2,500 micro-USD -> capacity for EXACTLY 5 of 50 concurrent requests!
	if _, err := db.Exec(`INSERT INTO budget_ledgers(id, limit_micro_usd) VALUES ('bdg_proj_1', 12500)`); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	const totalContenders = 50
	var (
		admitted atomic.Int32
		rejected atomic.Int32
		otherErr atomic.Int32
		admitIDs sync.Map
	)

	startGate := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(totalContenders)
	tStart := time.Now()

	for i := 0; i < totalContenders; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startGate
			resID := fmt.Sprintf("rsv_%02d", idx)
			err := reserveMicroUSD(ctx, db, "bdg_proj_1", resID, 2500)
			if err == nil {
				admitted.Add(1)
				admitIDs.Store(resID, idx)
			} else if errors.Is(err, errBudgetExceeded) {
				rejected.Add(1)
			} else {
				otherErr.Add(1)
			}
		}(i)
	}
	close(startGate)
	wg.Wait()
	raceElapsed := time.Since(tStart)

	if otherErr.Load() != 0 {
		t.Fatalf("unexpected DB errors during 50-goroutine contention: %d", otherErr.Load())
	}
	if admitted.Load() != 5 || rejected.Load() != 45 {
		t.Fatalf("SPIKE-03 AC-3 FAILED: expected 5 admitted & 45 rejected (429), got admitted=%d rejected=%d", admitted.Load(), rejected.Load())
	}

	var reservedNow int64
	_ = db.QueryRow(`SELECT reserved_micro_usd FROM budget_ledgers WHERE id = 'bdg_proj_1'`).Scan(&reservedNow)
	if reservedNow != 12500 {
		t.Fatalf("expected reserved_micro_usd=12500, got %d", reservedNow)
	}

	// Settle the 5 admitted streams (including 1 slight Negative Carryover overage to 12,600 micro-USD total) and verify 6th request is hard-blocked!
	i := 0
	admitIDs.Range(func(key, _ any) bool {
		resID := key.(string)
		actual := int64(2500)
		tier := "EXACT"
		if i == 4 {
			actual = 2600 // +100 micro-USD Negative Balance Carryover at tail of stream
			tier = "ESTIMATED"
		}
		if err := settleMicroUSD(ctx, db, "bdg_proj_1", resID, "req_"+resID, actual, tier); err != nil {
			t.Fatalf("settle %s: %v", resID, err)
		}
		i++
		return true
	})

	if err := reserveMicroUSD(ctx, db, "bdg_proj_1", "rsv_after_carryover", 100); !errors.Is(err, errBudgetExceeded) {
		t.Fatalf("expected post-carryover request to be hard-blocked with 429, got %v", err)
	}

	// Measure downstream client disconnect -> upstream context cancel propagation (< 500ms budget).
	upstreamCanceled := make(chan time.Time, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			fmt.Fprint(w, "data: {\"chunk\":1}\n\n")
			flusher.Flush()
			<-r.Context().Done()
			upstreamCanceled <- time.Now()
		}),
	}
	go srv.Serve(ln)
	defer srv.Close()

	reqCtx, cancelReq := context.WithCancel(ctx)
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, "http://"+ln.Addr().String()+"/v1/chat/completions", nil)
	resp, err := newHermeticHTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	_, _ = resp.Body.Read(buf)

	abortAt := time.Now()
	cancelReq()
	_ = resp.Body.Close()

	select {
	case seenAt := <-upstreamCanceled:
		cancelLatency := seenAt.Sub(abortAt)
		t.Logf("SPIKE-03 EMPIRICAL: 50-way SQLite WAL Pre-Hold race finished in %v (5/50 admitted, 45/50 429, 0 overrun); stream cancel propagated in %v (<500ms budget)", raceElapsed, cancelLatency)
		if cancelLatency >= 500*time.Millisecond {
			t.Fatalf("cancel propagation %v exceeded 500ms budget", cancelLatency)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for upstream SSE context cancellation")
	}
}
