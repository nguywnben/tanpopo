// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package a2a_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/ingress/a2a"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

func TestDEF085_ZeroHoldAndExhaustedBudgetFailBeforeDispatch(t *testing.T) {
	h := newA2AHarness(t, true)

	var dispatchCalls atomic.Int64
	var mu sync.Mutex
	seenMode := "input_required_overhold"
	upSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatchCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		mode := seenMode
		mu.Unlock()
		switch mode {
		case "over_hold":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":        "completed",
				"spent_micros": 5_000,
			})
		case "exact_hold":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":        "completed",
				"spent_micros": 400,
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":          "input-required",
				"prompt_message": "need confirmation",
				"spent_micros":   1_000,
			})
		}
	}))
	defer upSrv.Close()

	agent, err := h.reg.RegisterAgent(h.ctx, a2a.RegisterAgentParams{
		OrgID:             h.orgID,
		ProjectID:         h.projID,
		AgentSlug:         "def085-agent",
		Name:              "DEF085 Agent",
		EndpointURL:       upSrv.URL + "/a2a",
		DataRegion:        "EU",
		MaxDelegationHops: 4,
		AllowPrivateIP:    true,
		AllowedCIDRs:      []string{"127.0.0.1/32"},
	})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// 1. SubBudgetHoldMicros=0 synchronous request MUST fail closed BEFORE downstream dispatch (0 calls).
	resp0 := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID:       agent.ID,
		SubBudgetHoldMicros: 0,
		InputParts:          []map[string]any{{"text": "should not reach agent"}},
	})
	_ = resp0.Body.Close()
	if resp0.StatusCode != http.StatusForbidden {
		t.Fatalf("DEF-085 zero-hold status = %d, want 403", resp0.StatusCode)
	}
	if got := dispatchCalls.Load(); got != 0 {
		t.Fatalf("DEF-085 zero-hold dispatched %d upstream calls, want 0", got)
	}

	// 2. Request exceeding remaining hard limit fails closed BEFORE downstream dispatch (0 calls).
	respTooBig := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID:       agent.ID,
		SubBudgetHoldMicros: 2_000_000, // project hard limit is 1_000_000
		InputParts:          []map[string]any{{"text": "exceeds budget"}},
	})
	_ = respTooBig.Body.Close()
	if respTooBig.StatusCode != http.StatusForbidden {
		t.Fatalf("DEF-085 over-budget status = %d, want 403", respTooBig.StatusCode)
	}
	if got := dispatchCalls.Load(); got != 0 {
		t.Fatalf("DEF-085 over-budget dispatched %d upstream calls, want 0", got)
	}

	// 3. Positive hold <= limit succeeds and settles exact spend.
	mu.Lock()
	seenMode = "exact_hold"
	mu.Unlock()
	respOK := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID:       agent.ID,
		SubBudgetHoldMicros: 1_000,
		InputParts:          []map[string]any{{"text": "valid work"}},
	})
	var viewOK a2a.TaskView
	_ = json.NewDecoder(respOK.Body).Decode(&viewOK)
	_ = respOK.Body.Close()
	if respOK.StatusCode != http.StatusCreated || viewOK.State != a2a.StateCompleted || viewOK.SettledCostMicros != 400 {
		t.Fatalf("DEF-085 valid spend = (%d) %+v", respOK.StatusCode, viewOK)
	}
	if dispatchCalls.Load() != 1 {
		t.Fatalf("dispatchCalls = %d, want 1", dispatchCalls.Load())
	}

	// 4. Over-hold upstream response fails closed, leaves settled_cost_micros=0, and releases reservation.
	mu.Lock()
	seenMode = "over_hold"
	mu.Unlock()
	respOverHold := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID:       agent.ID,
		SubBudgetHoldMicros: 1_000,
		InputParts:          []map[string]any{{"text": "agent tries overcharge"}},
	})
	_ = respOverHold.Body.Close()
	if respOverHold.StatusCode != http.StatusForbidden {
		t.Fatalf("DEF-085 over-hold response status = %d, want 403", respOverHold.StatusCode)
	}
	page, lerr := h.coord.ListAdminTasksPage(h.ctx, h.orgID, []string{h.projID}, false, 20, nil, nil)
	if lerr != nil {
		t.Fatalf("ListAdminTasksPage: %v", lerr)
	}
	foundFailedOverHold := false
	for _, tk := range page.Items {
		if tk.State == a2a.StateFailed && tk.SettledCostMicros == 0 && tk.SubBudgetHoldMicros == 1_000 {
			foundFailedOverHold = true
			res, gerr := h.ledgerSvc.GetReservation(h.ctx, h.orgID, tk.ReservationID)
			if gerr != nil || res.State != ledger.StateReleasedUnused {
				t.Fatalf("expected failed over-hold reservation RELEASED_UNUSED, got res=%+v err=%v", res, gerr)
			}
		}
	}
	if !foundFailedOverHold {
		t.Fatalf("expected auditable failed zero-settled task on over-hold response, got %+v", page.Items)
	}
}

func TestDEF069_DownstreamCancelPropagationAndPartialSettlement(t *testing.T) {
	runCancelPropagationSuite := func(t *testing.T, coord *a2a.TaskCoordinator, reg *a2a.Registry, ldg *ledger.Service, db *sql.DB, orgID, projID, vkRaw, vkID string) {
		t.Helper()
		ctx := context.Background()

		var (
			startedCh   = make(chan string, 2)
			cancelCh    = make(chan string, 2)
			stopWorkCh  = make(chan struct{})
			cancelMode  atomic.Int32 // 0 = 200 acknowledged + partial spend 350, 1 = 404 unsupported
		)
		var closeOnce sync.Once
		agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/cancel") && r.Method == http.MethodPost {
				var body struct {
					TaskID string `json:"task_id"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				cancelCh <- body.TaskID
				closeOnce.Do(func() { close(stopWorkCh) })
				w.Header().Set("Content-Type", "application/json")
				if cancelMode.Load() == 1 {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"state":        "canceled",
					"spent_micros": 350,
					"message":      "stopped remote worker cleanly",
				})
				return
			}
			var reqBody struct {
				TaskID string `json:"task_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&reqBody)
			startedCh <- reqBody.TaskID
			select {
			case <-r.Context().Done():
			case <-stopWorkCh:
			case <-time.After(8 * time.Second):
			}
		}))
		defer agentSrv.Close()

		agent, err := reg.RegisterAgent(ctx, a2a.RegisterAgentParams{
			OrgID:             orgID,
			ProjectID:         projID,
			AgentSlug:         "cancel-prop-agent",
			Name:              "Cancel Prop Agent",
			EndpointURL:       agentSrv.URL + "/a2a",
			DataRegion:        "EU",
			MaxDelegationHops: 4,
			AllowPrivateIP:    true,
			AllowedCIDRs:      []string{"127.0.0.1/32"},
		})
		if err != nil {
			t.Fatalf("RegisterAgent: %v", err)
		}

		dpSrv := httptest.NewServer(coord.Handler())
		defer dpSrv.Close()

		// Case 1: Downstream acknowledges /cancel and reports partial spend=350 on hold=1000.
		var (
			createMu   sync.Mutex
			createCode int
			createView a2a.TaskView
			createDone = make(chan struct{})
		)
		go func() {
			defer close(createDone)
			body, _ := json.Marshal(a2a.TaskCreateRequest{
				TargetAgentID:       agent.ID,
				SubBudgetHoldMicros: 1_000,
				InputParts:          []map[string]any{{"text": "long remote job"}},
			})
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, dpSrv.URL+"/a2a/tasks", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+vkRaw)
			req.Header.Set("Content-Type", "application/json")
			resp, derr := dpSrv.Client().Do(req)
			if derr != nil {
				t.Errorf("POST /a2a/tasks: %v", derr)
				return
			}
			defer resp.Body.Close()
			var tv a2a.TaskView
			_ = json.NewDecoder(resp.Body).Decode(&tv)
			createMu.Lock()
			createCode = resp.StatusCode
			createView = tv
			createMu.Unlock()
		}()

		var activeTaskID string
		select {
		case activeTaskID = <-startedCh:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for downstream /tasks request")
		}

		// Admin/operator cancels via CancelAdminTask while in-flight.
		canceledView, err := coord.CancelAdminTask(ctx, orgID, projID, activeTaskID, "usr_admin_cancel")
		if err != nil {
			t.Fatalf("CancelAdminTask: %v", err)
		}
		select {
		case gotCancelID := <-cancelCh:
			if gotCancelID != activeTaskID {
				t.Fatalf("downstream cancel task_id=%q want %q", gotCancelID, activeTaskID)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("DEF-069: downstream /tasks/{id}/cancel was never received")
		}
		if canceledView.State != a2a.StateCanceled ||
			canceledView.DownstreamCancelStatus != a2a.CancelStatusAcknowledged ||
			canceledView.SettledCostMicros != 350 {
			t.Fatalf("unexpected canceledView: %+v", canceledView)
		}

		// Create call MUST resolve to HTTP 200 canceled with identical downstream status/cost (never 400).
		select {
		case <-createDone:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for POST /a2a/tasks to resolve after cancel")
		}
		createMu.Lock()
		gotCode, gotView := createCode, createView
		createMu.Unlock()
		if gotCode != http.StatusOK || gotView.State != a2a.StateCanceled || gotView.DownstreamCancelStatus != a2a.CancelStatusAcknowledged {
			t.Fatalf("POST /a2a/tasks resolved to (%d) %+v, want 200 canceled+acknowledged", gotCode, gotView)
		}
		if res, rerr := ldg.GetReservation(ctx, orgID, canceledView.ReservationID); rerr != nil || res.State != ledger.StateSettledExact {
			t.Fatalf("expected reservation SETTLED_EXACT for partial spend, got res=%+v err=%v", res, rerr)
		}

		// Case 2: Downstream returns 404 on /cancel -> unsupported, hold released, settled=0.
		cancelMode.Store(1)
		stopWorkCh = make(chan struct{})
		closeOnce = sync.Once{}
		createDone2 := make(chan struct{})
		go func() {
			defer close(createDone2)
			body, _ := json.Marshal(a2a.TaskCreateRequest{
				TargetAgentID:       agent.ID,
				SubBudgetHoldMicros: 1_000,
				InputParts:          []map[string]any{{"text": "unsupported cancel endpoint"}},
			})
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, dpSrv.URL+"/a2a/tasks", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+vkRaw)
			req.Header.Set("Content-Type", "application/json")
			resp, derr := dpSrv.Client().Do(req)
			if derr == nil {
				_ = resp.Body.Close()
			}
		}()

		var task2ID string
		select {
		case task2ID = <-startedCh:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for case 2 downstream start")
		}
		cView2, err := coord.CancelTask(ctx, orgID, projID, task2ID, vkID)
		if err != nil {
			t.Fatalf("CancelTask case 2: %v", err)
		}
		if cView2.State != a2a.StateCanceled ||
			cView2.DownstreamCancelStatus != a2a.CancelStatusUnsupported ||
			cView2.SettledCostMicros != 0 {
			t.Fatalf("case 2 unexpected view: %+v", cView2)
		}
		if res2, rerr := ldg.GetReservation(ctx, orgID, cView2.ReservationID); rerr != nil || res2.State != ledger.StateReleasedUnused {
			t.Fatalf("case 2 expected RELEASED_UNUSED reservation, got %+v err=%v", res2, rerr)
		}
		<-cancelCh
		<-createDone2
		_ = db
	}

	t.Run("SQLite", func(t *testing.T) {
		h := newA2AHarness(t, true)
		runCancelPropagationSuite(t, h.coord, h.reg, h.ledgerSvc, h.db, h.orgID, h.projID, h.vkRaw, h.vkID)
	})

	t.Run("Postgres16", func(t *testing.T) {
		dsn := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
		if dsn == "" {
			if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
				t.Fatal("TANPOPO_TEST_PG_DSN required")
			}
			return
		}
		ctx := context.Background()
		clk := clock.NewFakeClock(time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC))
		nowFn := func() int64 { return clk.Now().UnixMilli() }
		st, err := postgres.Open(ctx, postgres.Options{DSN: dsn, NowMs: nowFn})
		if err != nil {
			t.Fatalf("postgres.Open: %v", err)
		}
		defer func() { _ = st.Close() }()

		audit := &a2aAuditSink{}
		suffix := fmt.Sprintf("%d", time.Now().UnixNano())
		org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "pg069-" + suffix, DisplayName: "PG069 Org"})
		if err != nil {
			t.Fatalf("CreateOrganization: %v", err)
		}
		proj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "prj069-" + suffix, DisplayName: "PG069 Proj"})
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, port.CreateEnvironmentParams{Slug: "prod"})
		if err != nil {
			t.Fatalf("CreateEnvironment: %v", err)
		}
		mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB(), NowMs: nowFn})
		if err != nil {
			t.Fatalf("snapshot.NewManager: %v", err)
		}
		rawMaster := make([]byte, 32)
		_, _ = rand.Read(rawMaster)
		kek, _ := vault.NewStaticKEKProvider("kek_pg069", rawMaster)
		defer kek.Close()
		vs, _ := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: kek, NowMs: nowFn, PostShredHook: func(context.Context) error { return nil }})
		defer vs.Close()

		a2aRes := a2a.BrokerResolver{DB: st.RawDB()}
		brk, err := broker.NewBlindBroker(broker.Config{
			Resolver: a2aRes, PolicyResolver: a2aRes, Vault: vs, Audit: audit, NowMs: nowFn,
		})
		if err != nil {
			t.Fatalf("NewBlindBroker: %v", err)
		}
		ldg, err := ledger.NewService(ledger.Config{DB: st.RawDB(), Audit: audit, NowMs: nowFn})
		if err != nil {
			t.Fatalf("ledger.NewService: %v", err)
		}
		if _, err := ldg.UpsertLedger(ctx, ledger.UpsertLedgerParams{
			OrgID: org.ID, ScopeType: ledger.ScopeProject, ScopeID: proj.ID,
			PeriodKind: ledger.PeriodTotalLifetime, PeriodStartMs: nowFn() - 60_000, PeriodEndMs: nowFn() + 86_400_000_000,
			HardLimitMicros: 1_000_000,
		}); err != nil {
			t.Fatalf("UpsertLedger: %v", err)
		}
		vkSvc, err := vkey.NewService(vkey.ServiceConfig{DB: st.RawDB(), NowMs: nowFn, Snapshots: mgr, AuditSink: audit})
		if err != nil {
			t.Fatalf("vkey.NewService: %v", err)
		}
		vk, err := vkSvc.Create(ctx, vkey.CreateParams{
			OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, Name: "pg069-key",
			AllowedPlanes: "A2A", AllowedModels: []string{"gpt-4o"},
		})
		if err != nil {
			t.Fatalf("vkey.Create: %v", err)
		}
		if err := mgr.Refresh(ctx); err != nil {
			t.Fatalf("mgr.Refresh: %v", err)
		}
		reg, err := a2a.NewRegistry(a2a.RegistryConfig{DB: st.RawDB(), Audit: audit, Now: clk.Now})
		if err != nil {
			t.Fatalf("NewRegistry: %v", err)
		}
		coord, err := a2a.NewTaskCoordinator(a2a.CoordinatorConfig{
			Enabled: true, DB: st.RawDB(), Registry: reg, Snapshots: mgr,
			Broker: brk, Ledger: ldg, Audit: audit, Now: clk.Now,
		})
		if err != nil {
			t.Fatalf("NewTaskCoordinator: %v", err)
		}
		runCancelPropagationSuite(t, coord, reg, ldg, st.RawDB(), org.ID, proj.ID, vk.SecretKey, vk.Summary.ID)
	})
}
