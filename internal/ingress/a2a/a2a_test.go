// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package a2a_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/ingress/a2a"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type a2aAuditSink struct {
	buf canary.SafeBuffer
}

func (a *a2aAuditSink) Append(_ context.Context, ev port.AuditEvent) error {
	b, _ := json.Marshal(ev)
	_, _ = a.buf.Write(append(b, '\n'))
	return nil
}

func (a *a2aAuditSink) AppendTx(ctx context.Context, _ *sql.Tx, ev port.AuditEvent) error {
	return a.Append(ctx, ev)
}

type hermeticPublicResolver struct{}

func (hermeticPublicResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

type a2aHarness struct {
	t         *testing.T
	ctx       context.Context
	clk       *clock.FakeClock
	db        *sql.DB
	snapshots *snapshot.Manager
	ledgerSvc *ledger.Service
	vkeys     *vkey.Service
	reg       *a2a.Registry
	coord     *a2a.TaskCoordinator
	srv       *httptest.Server

	orgID     string
	projID    string
	envID     string
	vkRaw     string
	vkID      string
	vkMiniRaw string
}

func newA2AHarness(t *testing.T, enabled bool) *a2aHarness {
	t.Helper()
	t.Cleanup(func() { goleak.VerifyNone(t) })

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1_760_000_000_000).UTC())
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "a2a.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	audit := &a2aAuditSink{}
	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "a2a-org", DisplayName: "A2A Org"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, port.CreateProjectParams{Slug: "a2a-proj", DisplayName: "A2A Proj"})
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
	kek, _ := vault.NewStaticKEKProvider("kek_a2a", rawMaster)
	t.Cleanup(kek.Close)
	vs, _ := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: kek, NowMs: nowFn, PostShredHook: st.CheckpointWAL})
	t.Cleanup(vs.Close)

	a2aResolver := a2a.BrokerResolver{DB: st.RawDB()}
	brk, err := broker.NewBlindBroker(broker.Config{
		Resolver:       a2aResolver,
		PolicyResolver: a2aResolver,
		Vault:          vs,
		Audit:          audit,
		NowMs:          nowFn,
	})
	if err != nil {
		t.Fatalf("broker.NewBlindBroker: %v", err)
	}

	ldg, err := ledger.NewService(ledger.Config{DB: st.RawDB(), Audit: audit, NowMs: nowFn})
	if err != nil {
		t.Fatalf("ledger.NewService: %v", err)
	}
	if _, err := ldg.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           org.ID,
		ScopeType:       ledger.ScopeProject,
		ScopeID:         proj.ID,
		PeriodKind:      ledger.PeriodTotalLifetime,
		PeriodStartMs:   nowFn() - 60_000,
		PeriodEndMs:     nowFn() + 86_400_000_000,
		HardLimitMicros: 1_000_000, // $1.00 headroom
	}); err != nil {
		t.Fatalf("UpsertLedger: %v", err)
	}

	vkSvc, err := vkey.NewService(vkey.ServiceConfig{DB: st.RawDB(), NowMs: nowFn, Snapshots: mgr, AuditSink: audit})
	if err != nil {
		t.Fatalf("vkey.NewService: %v", err)
	}
	fullKey, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, Name: "orchestrator-key",
		AllowedPlanes: "PROVIDER,MCP,A2A", AllowedModels: []string{"gpt-4o-mini", "gpt-4o"},
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey full: %v", err)
	}
	miniKey, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, Name: "restricted-mini-key",
		AllowedPlanes: "A2A", AllowedModels: []string{"gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey mini: %v", err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatalf("snapshot Refresh: %v", err)
	}

	reg, err := a2a.NewRegistry(a2a.RegistryConfig{DB: st.RawDB(), Audit: audit, Now: clk.Now, HostResolver: hermeticPublicResolver{}})
	if err != nil {
		t.Fatalf("a2a.NewRegistry: %v", err)
	}
	coord, err := a2a.NewTaskCoordinator(a2a.CoordinatorConfig{
		Enabled:   enabled,
		DB:        st.RawDB(),
		Registry:  reg,
		Snapshots: mgr,
		Broker:    brk,
		Ledger:    ldg,
		Audit:     audit,
		Now:       clk.Now,
	})
	if err != nil {
		t.Fatalf("a2a.NewTaskCoordinator: %v", err)
	}
	srv := httptest.NewServer(coord.Handler())
	t.Cleanup(srv.Close)

	return &a2aHarness{
		t:         t,
		ctx:       ctx,
		clk:       clk,
		db:        st.RawDB(),
		snapshots: mgr,
		ledgerSvc: ldg,
		vkeys:     vkSvc,
		reg:       reg,
		coord:     coord,
		srv:       srv,
		orgID:     org.ID,
		projID:    proj.ID,
		envID:     env.ID,
		vkRaw:     fullKey.SecretKey,
		vkID:      fullKey.Summary.ID,
		vkMiniRaw: miniKey.SecretKey,
	}
}

func (h *a2aHarness) doJSON(method, path, vk string, hdrs map[string]string, body any) *http.Response {
	h.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(h.ctx, method, h.srv.URL+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if vk != "" {
		req.Header.Set("Authorization", "Bearer "+vk)
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}

func TestA2ADisabledByDefaultFailsClosed(t *testing.T) {
	h := newA2AHarness(t, false)
	resp := h.doJSON(http.MethodGet, "/a2a/agents", h.vkRaw, nil, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 when a2a.enabled=false, got %d", resp.StatusCode)
	}
}

func TestA2ALifecycleSSEAndBudgetSettlementAC2(t *testing.T) {
	h := newA2AHarness(t, true)
	agentB, err := h.reg.RegisterAgent(h.ctx, a2a.RegisterAgentParams{
		OrgID: h.orgID, ProjectID: h.projID, AgentSlug: "finance-worker",
		Name: "Finance Worker", EndpointURL: "https://worker.example.test/a2a",
		DataRegion: "EU", MaxDelegationHops: 5,
		Skills:         []a2a.AgentSkill{{ID: "refund", Name: "Issue Refund"}},
		RequiredModels: []string{"gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Discover agents matching EU residency.
	dResp := h.doJSON(http.MethodGet, "/a2a/agents", h.vkRaw, map[string]string{a2a.HeaderDataRegion: "EU"}, nil)
	if dResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /a2a/agents status=%d", dResp.StatusCode)
	}
	var listOut struct {
		Data []a2a.AgentCard `json:"data"`
	}
	_ = json.NewDecoder(dResp.Body).Decode(&listOut)
	_ = dResp.Body.Close()
	if len(listOut.Data) != 1 || listOut.Data[0].AgentID != agentB.ID {
		t.Fatalf("expected Finance Worker agent card, got %+v", listOut.Data)
	}

	// Create async SM-09 task with 500,000 micro-USD sub-budget hold.
	cResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, map[string]string{
		a2a.HeaderDataRegion: "EU",
		a2a.HeaderTraceRoot:  "trc_root_ac2",
	}, a2a.TaskCreateRequest{
		TargetAgentID:       agentB.ID,
		SubBudgetHoldMicros: 500_000,
		DispatchAsync:       true,
		InputParts:          []map[string]any{{"text": "analyze invoice INV-9"}},
	})
	if cResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /a2a/tasks status=%d", cResp.StatusCode)
	}
	var created a2a.TaskView
	_ = json.NewDecoder(cResp.Body).Decode(&created)
	_ = cResp.Body.Close()
	if created.State != a2a.StateSubmitted || created.ReservationID == "" {
		t.Fatalf("expected submitted task with hold reservation, got %+v", created)
	}

	// Subscribe to SSE progress stream and drive full SM-09 lifecycle:
	// submitted -> working -> input-required -> working -> completed (spent 125,000 micro-USD).
	sseReq, _ := http.NewRequestWithContext(h.ctx, http.MethodGet, h.srv.URL+"/a2a/tasks/"+created.TaskID, nil)
	sseReq.Header.Set("Authorization", "Bearer "+h.vkRaw)
	sseReq.Header.Set("Accept", "text/event-stream")
	sseResp, err := h.srv.Client().Do(sseReq)
	if err != nil {
		t.Fatalf("SSE subscribe: %v", err)
	}
	defer func() { _ = sseResp.Body.Close() }()

	statesCh := make(chan string, 16)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		sc := bufio.NewScanner(sseResp.Body)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data: ") {
				var tv a2a.TaskView
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &tv) == nil {
					statesCh <- tv.State
				}
			}
		}
	}()

	// Data Plane caller HTTP POST /transition must not be exposed (404), preventing spend/state forgery.
	forgedResp := h.doJSON(http.MethodPost, "/a2a/tasks/"+created.TaskID+"/transition", h.vkRaw, nil, a2a.TaskTransitionRequest{State: a2a.StateCompleted, SpentMicros: 0})
	_ = forgedResp.Body.Close()
	if forgedResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected HTTP /transition to return 404, got %d", forgedResp.StatusCode)
	}

	// Cross-key IDOR on GET & POST /cancel must fail closed with 404.
	idorGet := h.doJSON(http.MethodGet, "/a2a/tasks/"+created.TaskID, h.vkMiniRaw, nil, nil)
	_ = idorGet.Body.Close()
	if idorGet.StatusCode != http.StatusNotFound {
		t.Fatalf("expected cross-key GET to return 404, got %d", idorGet.StatusCode)
	}
	idorCancel := h.doJSON(http.MethodPost, "/a2a/tasks/"+created.TaskID+"/cancel", h.vkMiniRaw, nil, nil)
	_ = idorCancel.Body.Close()
	if idorCancel.StatusCode != http.StatusNotFound {
		t.Fatalf("expected cross-key cancel to return 404, got %d", idorCancel.StatusCode)
	}

	steps := []a2a.TaskTransitionRequest{
		{State: a2a.StateWorking},
		{State: a2a.StateInputRequired, PromptMessage: "Need invoice PDF confirmation"},
		{State: a2a.StateWorking},
		{State: a2a.StateCompleted, SpentMicros: 125_000, Artifacts: []map[string]any{{"name": "credit_note", "amount": 50}}},
	}
	for _, stReq := range steps {
		if _, tErr := h.coord.TransitionTask(h.ctx, h.orgID, h.projID, created.TaskID, stReq); tErr != nil {
			t.Fatalf("TransitionTask to %s failed: %v", stReq.State, tErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	<-readDone
	close(statesCh)
	var seen []string
	for s := range statesCh {
		seen = append(seen, s)
	}
	joined := strings.Join(seen, " -> ")
	for _, expected := range []string{a2a.StateSubmitted, a2a.StateWorking, a2a.StateInputRequired, a2a.StateCompleted} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("SSE stream missing SM-09 state %q: %s", expected, joined)
		}
	}

	// Verify a fresh coordinator instance reads persisted settled_cost_micros and artifacts from DB.
	freshCoord, fErr := a2a.NewTaskCoordinator(a2a.CoordinatorConfig{Enabled: true, DB: h.db})
	if fErr != nil {
		t.Fatalf("NewTaskCoordinator(fresh): %v", fErr)
	}
	persisted, pErr := freshCoord.GetTask(h.ctx, h.orgID, h.projID, created.TaskID)
	if pErr != nil {
		t.Fatalf("fresh GetTask: %v", pErr)
	}
	if persisted.SettledCostMicros != 125_000 || len(persisted.Artifacts) != 1 {
		t.Fatalf("expected persisted settled_cost=125000 and 1 artifact, got %+v", persisted)
	}

	// Verify exact settlement (125,000 spent, remaining 375,000 released from reserved hold).
	res, err := h.ledgerSvc.GetReservation(h.ctx, h.orgID, created.ReservationID)
	if err != nil {
		t.Fatalf("GetReservation: %v", err)
	}
	if res.State != ledger.StateSettledExact {
		t.Fatalf("expected reservation state SETTLED_EXACT, got %s", res.State)
	}
}

func TestA2ALoopHopCeilingResidencyAndPrivilegeNarrowingNEG1(t *testing.T) {
	h := newA2AHarness(t, true)
	agentA, err := h.reg.RegisterAgent(h.ctx, a2a.RegisterAgentParams{
		OrgID: h.orgID, ProjectID: h.projID, AgentSlug: "agent-a",
		Name: "Agent A", EndpointURL: "https://a.example.test",
		DataRegion: "EU", MaxDelegationHops: 5,
	})
	if err != nil {
		t.Fatalf("RegisterAgent A: %v", err)
	}
	agentB, err := h.reg.RegisterAgent(h.ctx, a2a.RegisterAgentParams{
		OrgID: h.orgID, ProjectID: h.projID, AgentSlug: "agent-b",
		Name: "Agent B", EndpointURL: "https://b.example.test",
		DataRegion: "EU", MaxDelegationHops: 5,
	})
	if err != nil {
		t.Fatalf("RegisterAgent B: %v", err)
	}
	restrictedAgent, err := h.reg.RegisterAgent(h.ctx, a2a.RegisterAgentParams{
		OrgID: h.orgID, ProjectID: h.projID, AgentSlug: "opus-worker",
		Name: "Opus Worker", EndpointURL: "https://opus.example.test",
		DataRegion: "EU", MaxDelegationHops: 5,
		RequiredModels: []string{"claude-3-opus"},
	})
	if err != nil {
		t.Fatalf("RegisterAgent restricted: %v", err)
	}

	// 1. NEG-1 Cycle detection (Agent A -> Agent B -> Agent A) => 400 delegation_depth_exceeded & 0 spend.
	loopResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID:       agentA.ID,
		LineageAgentIDs:     []string{agentA.ID, agentB.ID},
		HopCount:            3,
		SubBudgetHoldMicros: 100_000,
		DispatchAsync:       true,
		InputParts:          []map[string]any{{"text": "ping-pong"}},
	})
	_ = loopResp.Body.Close()
	if loopResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("NEG-1 cycle expected 400 delegation_depth_exceeded, got %d", loopResp.StatusCode)
	}

	// 2. NEG-1 Hop > 5 chain (A1 -> A2 -> ... -> A6) => 400 delegation_depth_exceeded.
	hopResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, map[string]string{a2a.HeaderA2AHopCount: "6"}, a2a.TaskCreateRequest{
		TargetAgentID:       agentB.ID,
		HopCount:            6,
		SubBudgetHoldMicros: 100_000,
		DispatchAsync:       true,
		InputParts:          []map[string]any{{"text": "too-deep"}},
	})
	_ = hopResp.Body.Close()
	if hopResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("NEG-1 hop=6 expected 400 delegation_depth_exceeded, got %d", hopResp.StatusCode)
	}

	// Verify 0 budget reservations or spend leaked from rejected calls.
	var resCount int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM budget_reservations WHERE org_id = ?`, h.orgID).Scan(&resCount); err != nil {
		t.Fatal(err)
	}
	if resCount != 0 {
		t.Fatalf("NEG-1: expected 0 budget reservations on rejected loops, got %d", resCount)
	}

	// 3. Residency intersection denial (US caller targeting EU-only Agent A) => 403 residency_incompatible.
	resResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, map[string]string{a2a.HeaderDataRegion: "US"}, a2a.TaskCreateRequest{
		TargetAgentID: agentA.ID,
		DispatchAsync: true,
		InputParts:    []map[string]any{{"text": "cross-region"}},
	})
	_ = resResp.Body.Close()
	if resResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 residency_incompatible, got %d", resResp.StatusCode)
	}

	// 4. CT-23 Confused-Deputy privilege narrowing: gpt-4o-mini key cannot delegate to claude-3-opus agent.
	privResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkMiniRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID: restrictedAgent.ID,
		DispatchAsync: true,
		InputParts:    []map[string]any{{"text": "escalate model"}},
	})
	_ = privResp.Body.Close()
	if privResp.StatusCode != http.StatusForbidden {
		t.Fatalf("CT-23 expected 403 budget_or_privilege_exceeded, got %d", privResp.StatusCode)
	}

	// 5. Cancel task & release unused sub-budget hold cleanly.
	pResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID:       agentA.ID,
		SubBudgetHoldMicros: 200_000,
		DispatchAsync:       true,
		InputParts:          []map[string]any{{"text": "cancellable"}},
	})
	var pTask a2a.TaskView
	_ = json.NewDecoder(pResp.Body).Decode(&pTask)
	_ = pResp.Body.Close()

	// Child sub-budget > parent remaining balance must be rejected with 403.
	childOver := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID:       agentB.ID,
		ParentTaskID:        pTask.TaskID,
		SubBudgetHoldMicros: 250_000,
		DispatchAsync:       true,
		InputParts:          []map[string]any{{"text": "too expensive child"}},
	})
	_ = childOver.Body.Close()
	if childOver.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 when child sub_budget > parent remaining balance, got %d", childOver.StatusCode)
	}

	cancelResp := h.doJSON(http.MethodPost, "/a2a/tasks/"+pTask.TaskID+"/cancel", h.vkRaw, nil, nil)
	if cancelResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /cancel status=%d", cancelResp.StatusCode)
	}
	var canceled a2a.TaskView
	_ = json.NewDecoder(cancelResp.Body).Decode(&canceled)
	_ = cancelResp.Body.Close()
	if canceled.State != a2a.StateCanceled {
		t.Fatalf("expected canceled state, got %s", canceled.State)
	}
	rAfter, _ := h.ledgerSvc.GetReservation(h.ctx, h.orgID, pTask.ReservationID)
	if rAfter.State != ledger.StateReleasedUnused {
		t.Fatalf("expected reservation RELEASED_UNUSED on cancel, got %s", rAfter.State)
	}
}

func TestA2APostgresForceRLSIsolation(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	if dsn == "" {
		if os.Getenv("TANPOPO_REQUIRE_POSTGRES_TESTS") == "1" {
			t.Fatal("TANPOPO_TEST_PG_DSN required")
		}
		return
	}
	ctx := context.Background()
	st, err := postgres.Open(ctx, postgres.Options{DSN: dsn})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "pg-a2a-a-" + suffix, DisplayName: "A2A PG A"})
	if err != nil {
		t.Fatalf("CreateOrganization A: %v", err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "pg-a2a-b-" + suffix, DisplayName: "A2A PG B"})
	if err != nil {
		t.Fatalf("CreateOrganization B: %v", err)
	}
	projA, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "pa-" + suffix, DisplayName: "Proj A"})
	if err != nil {
		t.Fatalf("CreateProject A: %v", err)
	}

	reg, err := a2a.NewRegistry(a2a.RegistryConfig{DB: st.RawDB(), HostResolver: hermeticPublicResolver{}})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := reg.RegisterAgent(ctx, a2a.RegisterAgentParams{
		OrgID: orgA.ID, ProjectID: projA.ID, AgentSlug: "pg-agent-" + suffix,
		Name: "PG Agent", EndpointURL: "https://pg-agent.example.test",
		DataRegion: "EU", MaxDelegationHops: 4,
	})
	if err != nil {
		t.Fatalf("RegisterAgent PG: %v", err)
	}
	if _, err := reg.GetAgent(ctx, orgB.ID, ep.ID); !errors.Is(err, a2a.ErrAgentNotFound) {
		t.Fatalf("expected PG FORCE RLS to hide Org A agent from Org B, got %v", err)
	}
	cardsB, err := reg.ListAdminAgents(ctx, orgB.ID, nil, true, 50)
	if err != nil || len(cardsB) != 0 {
		t.Fatalf("expected 0 Org B agents under FORCE RLS, got %d (err=%v)", len(cardsB), err)
	}
}

func TestA2AParentCarvingZeroHoldAndModelDenyFailClosed(t *testing.T) {
	h := newA2AHarness(t, true)

	euAgent, err := h.reg.RegisterAgent(h.ctx, a2a.RegisterAgentParams{
		OrgID: h.orgID, ProjectID: h.projID, AgentSlug: "eu-parent",
		Name: "EU Parent", EndpointURL: "https://eu-parent.example.test/a2a",
		DataRegion: "EU", MaxDelegationHops: 5, RequiredModels: []string{"gpt-4o"},
	})
	if err != nil {
		t.Fatalf("RegisterAgent EU: %v", err)
	}
	usAgent, err := h.reg.RegisterAgent(h.ctx, a2a.RegisterAgentParams{
		OrgID: h.orgID, ProjectID: h.projID, AgentSlug: "us-child",
		Name: "US Child", EndpointURL: "https://us-child.example.test/a2a",
		DataRegion: "US", MaxDelegationHops: 5,
	})
	if err != nil {
		t.Fatalf("RegisterAgent US: %v", err)
	}
	euChild, err := h.reg.RegisterAgent(h.ctx, a2a.RegisterAgentParams{
		OrgID: h.orgID, ProjectID: h.projID, AgentSlug: "eu-child",
		Name: "EU Child", EndpointURL: "https://eu-child.example.test/a2a",
		DataRegion: "EU", MaxDelegationHops: 5,
	})
	if err != nil {
		t.Fatalf("RegisterAgent EU Child: %v", err)
	}

	// Empty AllowedModels must deny agents requiring models (fail-closed, not fail-open).
	emptyModelCards, err := h.reg.ListDiscoverableAgents(h.ctx, a2a.DiscoverFilter{
		OrgID: h.orgID, ProjectID: h.projID, AllowedModels: nil, Limit: 50,
	})
	if err != nil {
		t.Fatalf("ListDiscoverableAgents: %v", err)
	}
	for _, c := range emptyModelCards {
		if c.AgentID == euAgent.ID {
			t.Fatalf("empty AllowedModels must NOT expose model-requiring agent %s", euAgent.ID)
		}
	}

	// Negative hop_count must fail with 400 delegation_depth_exceeded.
	negHopResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID: euAgent.ID, HopCount: -1, SubBudgetHoldMicros: 1000, InputParts: []map[string]any{{"text": "hi"}},
	})
	_ = negHopResp.Body.Close()
	if negHopResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected negative hop_count -> 400, got %d", negHopResp.StatusCode)
	}

	// Zero-hold task must reject positive SpentMicros at transition time.
	zeroHoldResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID: euChild.ID, SubBudgetHoldMicros: 0, InputParts: []map[string]any{{"text": "free"}}, DispatchAsync: true,
	})
	if zeroHoldResp.StatusCode != http.StatusCreated {
		t.Fatalf("create zero-hold task status=%d", zeroHoldResp.StatusCode)
	}
	var zeroTask a2a.TaskView
	_ = json.NewDecoder(zeroHoldResp.Body).Decode(&zeroTask)
	_ = zeroHoldResp.Body.Close()
	if _, err := h.coord.TransitionTask(h.ctx, h.orgID, h.projID, zeroTask.TaskID, a2a.TaskTransitionRequest{
		State: a2a.StateWorking,
	}); err != nil {
		t.Fatalf("transition zero-hold task working: %v", err)
	}
	if _, err := h.coord.TransitionTask(h.ctx, h.orgID, h.projID, zeroTask.TaskID, a2a.TaskTransitionRequest{
		State: a2a.StateCompleted, SpentMicros: 500,
	}); !errors.Is(err, a2a.ErrPrivilegeOrBudget) {
		t.Fatalf("expected ErrPrivilegeOrBudget on 0-hold spend > 0, got %v", err)
	}

	// Parent task reserves 300k; child cannot escape EU -> US even without header, nor exceed remaining balance.
	pResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, map[string]string{a2a.HeaderDataRegion: "EU"}, a2a.TaskCreateRequest{
		TargetAgentID: euAgent.ID, SubBudgetHoldMicros: 300_000, InputParts: []map[string]any{{"text": "root"}}, DispatchAsync: true,
	})
	if pResp.StatusCode != http.StatusCreated {
		t.Fatalf("create parent task status=%d", pResp.StatusCode)
	}
	var parentTask a2a.TaskView
	_ = json.NewDecoder(pResp.Body).Decode(&parentTask)
	_ = pResp.Body.Close()

	// Residency escape EU -> US without header must fail with 403 residency_incompatible.
	escResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID: usAgent.ID, ParentTaskID: parentTask.TaskID, SubBudgetHoldMicros: 50_000, InputParts: []map[string]any{{"text": "child"}},
	})
	_ = escResp.Body.Close()
	if escResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected parent residency escape EU->US -> 403, got %d", escResp.StatusCode)
	}

	// Child carves 200k cleanly without double-reserving ledger.
	cResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID: euChild.ID, ParentTaskID: parentTask.TaskID, SubBudgetHoldMicros: 200_000, InputParts: []map[string]any{{"text": "child1"}}, DispatchAsync: true,
	})
	if cResp.StatusCode != http.StatusCreated {
		t.Fatalf("create child1 status=%d", cResp.StatusCode)
	}
	var child1 a2a.TaskView
	_ = json.NewDecoder(cResp.Body).Decode(&child1)
	_ = cResp.Body.Close()
	if child1.ReservationID != "" {
		t.Fatalf("child task must carve parent hold without double-reserving, got reservation=%q", child1.ReservationID)
	}

	// Second child exceeding remaining 100k headroom must fail with 403.
	overResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID: euChild.ID, ParentTaskID: parentTask.TaskID, SubBudgetHoldMicros: 150_000, InputParts: []map[string]any{{"text": "child2"}}, DispatchAsync: true,
	})
	_ = overResp.Body.Close()
	if overResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected child exceeding parent headroom -> 403, got %d", overResp.StatusCode)
	}

	// Terminal parent must reject child delegation.
	if _, err := h.coord.CancelTask(h.ctx, h.orgID, h.projID, parentTask.TaskID, ""); err != nil {
		t.Fatalf("CancelTask parent: %v", err)
	}
	termResp := h.doJSON(http.MethodPost, "/a2a/tasks", h.vkRaw, nil, a2a.TaskCreateRequest{
		TargetAgentID: euChild.ID, ParentTaskID: parentTask.TaskID, SubBudgetHoldMicros: 10_000, InputParts: []map[string]any{{"text": "after-cancel"}}, DispatchAsync: true,
	})
	_ = termResp.Body.Close()
	if termResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected terminal parent delegation -> 403, got %d", termResp.StatusCode)
	}
}

type stubResumeTransport struct {
	calls int
}

func (s *stubResumeTransport) DispatchTask(_ context.Context, _ *a2a.AgentEndpoint, _ a2a.TaskView, inputParts []map[string]any) (a2a.UpstreamTaskResponse, error) {
	s.calls++
	if s.calls == 1 {
		return a2a.UpstreamTaskResponse{
			State:         a2a.StateInputRequired,
			PromptMessage: "Provide customer approval ticket",
		}, nil
	}
	return a2a.UpstreamTaskResponse{
		State:       a2a.StateCompleted,
		SpentMicros: 42000,
		Artifacts:   []map[string]any{{"resumed_with": inputParts}},
	}, nil
}

func TestDEF011_DataPlaneA2ATaskResumeWorkflow(t *testing.T) {
	h := newA2AHarness(t, true)
	tr := &stubResumeTransport{}
	coord, err := a2a.NewTaskCoordinator(a2a.CoordinatorConfig{
		Enabled:    true,
		DB:         h.db,
		Registry:   h.reg,
		Snapshots:  h.snapshots,
		Ledger:     h.ledgerSvc,
		Transport:  tr,
		Now:        h.clk.Now,
	})
	if err != nil {
		t.Fatalf("NewTaskCoordinator: %v", err)
	}
	srv := httptest.NewServer(coord.Handler())
	defer srv.Close()

	// Seed provider-only Virtual Key (must be rejected on A2A plane with 403).
	provOnly, err := h.vkeys.Create(h.ctx, vkey.CreateParams{
		OrgID:     h.orgID,
		ProjectID: h.projID,
		EnvID:     h.envID,
		Name:      "def011-provider-only",
	})
	if err != nil {
		t.Fatalf("create provider-only key: %v", err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatalf("refresh snapshots: %v", err)
	}

	agent, err := h.reg.RegisterAgent(h.ctx, a2a.RegisterAgentParams{
		OrgID: h.orgID, ProjectID: h.projID, AgentSlug: "approval-agent",
		Name: "Approval Agent", EndpointURL: "https://approval.example.test/a2a",
		DataRegion: "EU", MaxDelegationHops: 4,
	})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	doCustom := func(method, path, rawKey string, body any) *http.Response {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequestWithContext(h.ctx, method, srv.URL+path, &buf)
		if rawKey != "" {
			req.Header.Set("Authorization", "Bearer "+rawKey)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, derr := srv.Client().Do(req)
		if derr != nil {
			t.Fatalf("%s %s: %v", method, path, derr)
		}
		return resp
	}

	// 1. Initial task dispatch stops in input-required with prompt_message.
	cResp := doCustom(http.MethodPost, "/a2a/tasks", h.vkRaw, a2a.TaskCreateRequest{
		TargetAgentID:       agent.ID,
		SubBudgetHoldMicros: 200_000,
		InputParts:          []map[string]any{{"text": "approve order"}},
	})
	if cResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /a2a/tasks status=%d", cResp.StatusCode)
	}
	var paused a2a.TaskView
	_ = json.NewDecoder(cResp.Body).Decode(&paused)
	_ = cResp.Body.Close()
	if paused.State != a2a.StateInputRequired || paused.PromptMessage != "Provide customer approval ticket" {
		t.Fatalf("expected input-required with prompt_message, got %+v", paused)
	}

	// 2. Public POST /transition stays 404 (prevents forged state/spend).
	forged := doCustom(http.MethodPost, "/a2a/tasks/"+paused.TaskID+"/transition", h.vkRaw, map[string]any{"state": "completed", "spent_micros": 0})
	_ = forged.Body.Close()
	if forged.StatusCode != http.StatusNotFound {
		t.Fatalf("expected /transition -> 404, got %d", forged.StatusCode)
	}

	// 3. Security checks on POST /a2a/tasks/{id}/resume:
	// - Forged extra field ("state") rejected with 400
	forgeField := doCustom(http.MethodPost, "/a2a/tasks/"+paused.TaskID+"/resume", h.vkRaw, map[string]any{"input_parts": []map[string]any{{"ok": true}}, "state": "completed"})
	_ = forgeField.Body.Close()
	if forgeField.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on unknown field state forgery, got %d", forgeField.StatusCode)
	}
	// - Provider-only key rejected with 403
	provDenial := doCustom(http.MethodPost, "/a2a/tasks/"+paused.TaskID+"/resume", provOnly.SecretKey, a2a.ResumeTaskRequest{InputParts: []map[string]any{{"ticket": "T-1"}}})
	_ = provDenial.Body.Close()
	if provDenial.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for provider-only key resuming A2A task, got %d", provDenial.StatusCode)
	}
	// - Different A2A key in same project fails closed with 404 (owner isolation)
	otherDenial := doCustom(http.MethodPost, "/a2a/tasks/"+paused.TaskID+"/resume", h.vkMiniRaw, a2a.ResumeTaskRequest{InputParts: []map[string]any{{"ticket": "T-1"}}})
	_ = otherDenial.Body.Close()
	if otherDenial.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-key resume attempt, got %d", otherDenial.StatusCode)
	}

	// 4. Legitimate caller resume completes task atomically, clears prompt_message, and settles ledger.
	resResp := doCustom(http.MethodPost, "/a2a/tasks/"+paused.TaskID+"/resume", h.vkRaw, a2a.ResumeTaskRequest{
		InputParts: []map[string]any{{"ticket": "APPROVED-123"}},
	})
	if resResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /a2a/tasks/{id}/resume status=%d", resResp.StatusCode)
	}
	var done a2a.TaskView
	_ = json.NewDecoder(resResp.Body).Decode(&done)
	_ = resResp.Body.Close()
	if done.State != a2a.StateCompleted || done.SettledCostMicros != 42000 || done.PromptMessage != "" || len(done.Artifacts) != 1 {
		t.Fatalf("expected completed task with 42000 cost and 1 artifact, got %+v", done)
	}

	// 5. Repeat resume on completed task must return 409 task_not_resumable.
	dupResp := doCustom(http.MethodPost, "/a2a/tasks/"+paused.TaskID+"/resume", h.vkRaw, a2a.ResumeTaskRequest{
		InputParts: []map[string]any{{"ticket": "AGAIN"}},
	})
	_ = dupResp.Body.Close()
	if dupResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 repeating resume on terminal task, got %d", dupResp.StatusCode)
	}
}

func TestCoordinator_SecurityHardeningRound1(t *testing.T) {
	h := newA2AHarness(t, true)
	auditSink := &a2aAuditSink{}

	otherEnv := "env_other_round1"
	if _, err := h.db.ExecContext(h.ctx, `
		INSERT INTO environments (id, org_id, project_id, slug, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, 'staging-round1', 1, ?, ?)`,
		otherEnv, h.orgID, h.projID, h.clk.Now().UnixMilli(), h.clk.Now().UnixMilli(),
	); err != nil {
		t.Fatalf("seed other env: %v", err)
	}
	otherEnvKey, err := h.vkeys.Create(h.ctx, vkey.CreateParams{
		OrgID:         h.orgID,
		ProjectID:     h.projID,
		EnvID:         otherEnv,
		Name:          "a2a-other-env",
		AllowedPlanes: "A2A",
	})
	if err != nil {
		t.Fatalf("create other env vkey: %v", err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatalf("snapshots.Refresh: %v", err)
	}
	snap := h.snapshots.Current()
	if snap == nil {
		t.Fatalf("snapshots.Current returned nil")
	}
	mainCaller, err := snap.AuthenticateKey(h.vkRaw, "", h.clk.Now().UnixMilli())
	if err != nil {
		t.Fatalf("AuthenticateKey main: %v", err)
	}
	otherEnvCaller, err := snap.AuthenticateKey(otherEnvKey.SecretKey, "", h.clk.Now().UnixMilli())
	if err != nil {
		t.Fatalf("AuthenticateKey otherEnv: %v", err)
	}

	reg, err := a2a.NewRegistry(a2a.RegistryConfig{
		DB:           h.db,
		Audit:        auditSink,
		HostResolver: hermeticPublicResolver{},
		Now:          h.clk.Now,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	agent, err := reg.RegisterAgent(h.ctx, a2a.RegisterAgentParams{
		OrgID: h.orgID, ProjectID: h.projID, AgentSlug: "sec-agent",
		Name: "Security Agent", EndpointURL: "https://sec-agent.example.test/a2a",
		DataRegion: "EU", MaxDelegationHops: 4,
	})
	if err != nil {
		t.Fatalf("RegisterAgent parent: %v", err)
	}
	childAgent, err := reg.RegisterAgent(h.ctx, a2a.RegisterAgentParams{
		OrgID: h.orgID, ProjectID: h.projID, AgentSlug: "sec-child-agent",
		Name: "Security Child Agent", EndpointURL: "https://sec-child.example.test/a2a",
		DataRegion: "EU", MaxDelegationHops: 4,
	})
	if err != nil {
		t.Fatalf("RegisterAgent child: %v", err)
	}
	coord, err := a2a.NewTaskCoordinator(a2a.CoordinatorConfig{
		Enabled:   true,
		DB:        h.db,
		Registry:  reg,
		Snapshots: h.snapshots,
		Ledger:    h.ledgerSvc,
		Now:       h.clk.Now,
	})
	if err != nil {
		t.Fatalf("NewTaskCoordinator: %v", err)
	}
	srv := httptest.NewServer(coord.Handler())
	defer srv.Close()

	// 1. Conflicting credential headers must fail closed with 401.
	reqBody, _ := json.Marshal(a2a.TaskCreateRequest{
		TargetAgentID:       agent.ID,
		SubBudgetHoldMicros: 50_000,
		InputParts:          []map[string]any{{"text": "hi"}},
	})
	req, _ := http.NewRequestWithContext(h.ctx, http.MethodPost, srv.URL+"/a2a/tasks", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.vkRaw)
	req.Header.Set("X-Api-Key", h.vkRaw)
	cResp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do conflicting headers: %v", err)
	}
	_ = cResp.Body.Close()
	if cResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on duplicate/conflicting credential headers, got %d", cResp.StatusCode)
	}

	// 2. Parent + Child task creation and active-child termination ordering.
	parent, err := coord.CreateTask(h.ctx, mainCaller, a2a.TaskCreateRequest{
		TargetAgentID:       agent.ID,
		SubBudgetHoldMicros: 100_000,
		InputParts:          []map[string]any{{"text": "root"}},
	}, "", "")
	if err != nil {
		t.Fatalf("CreateTask parent: %v", err)
	}
	child, err := coord.CreateTask(h.ctx, mainCaller, a2a.TaskCreateRequest{
		ParentTaskID:        parent.TaskID,
		TargetAgentID:       childAgent.ID,
		SubBudgetHoldMicros: 40_000,
		InputParts:          []map[string]any{{"text": "child-1"}},
	}, "", "")
	if err != nil {
		t.Fatalf("CreateTask child: %v", err)
	}

	// Cross-env caller spawning under parent must fail with ErrPrivilegeOrBudget.
	_, err = coord.CreateTask(h.ctx, otherEnvCaller, a2a.TaskCreateRequest{
		ParentTaskID:        parent.TaskID,
		TargetAgentID:       childAgent.ID,
		SubBudgetHoldMicros: 10_000,
		InputParts:          []map[string]any{{"text": "cross-env"}},
	}, "", "")
	if !errors.Is(err, a2a.ErrPrivilegeOrBudget) {
		t.Fatalf("expected ErrPrivilegeOrBudget for cross-env child spawn, got %v", err)
	}

	// Completing parent while child is non-terminal must fail with ErrInvalidTaskStateMove
	// and write zero failure-case audit events.
	if _, err := coord.TransitionTask(h.ctx, h.orgID, h.projID, parent.TaskID, a2a.TaskTransitionRequest{State: a2a.StateWorking}); err != nil {
		t.Fatalf("parent -> working: %v", err)
	}
	beforeAudit := auditSink.buf.String()
	_, err = coord.TransitionTask(h.ctx, h.orgID, h.projID, parent.TaskID, a2a.TaskTransitionRequest{
		State: a2a.StateCompleted,
	})
	if !errors.Is(err, a2a.ErrInvalidTaskStateMove) {
		t.Fatalf("expected ErrInvalidTaskStateMove completing parent while child active, got %v", err)
	}
	if auditSink.buf.String() != beforeAudit {
		t.Fatalf("expected zero audit events emitted on failed transitions, delta=%q", strings.TrimPrefix(auditSink.buf.String(), beforeAudit))
	}

	// Admin-canceling parent must atomically cascade-cancel active child and emit PRINCIPAL/admin_api attribution.
	afterFail := auditSink.buf.String()
	canceledParent, err := coord.CancelAdminTask(h.ctx, h.orgID, h.projID, parent.TaskID, "usr_admin_round1")
	if err != nil {
		t.Fatalf("CancelAdminTask cascade: %v", err)
	}
	if canceledParent.State != a2a.StateCanceled {
		t.Fatalf("expected canceled parent, got %+v", canceledParent)
	}
	canceledChild, err := coord.GetTask(h.ctx, h.orgID, h.projID, child.TaskID)
	if err != nil || canceledChild.State != a2a.StateCanceled {
		t.Fatalf("expected child atomically canceled with parent, err=%v child=%+v", err, canceledChild)
	}
	adminDelta := strings.TrimPrefix(auditSink.buf.String(), afterFail)
	if !strings.Contains(adminDelta, `"EventType":"a2a.task.canceled_by_admin"`) ||
		!strings.Contains(adminDelta, `"ActorID":"usr_admin_round1"`) ||
		!strings.Contains(adminDelta, `"ActorKind":"PRINCIPAL"`) ||
		!strings.Contains(adminDelta, `"EntryPoint":"`+port.AuditEntryPointAdminAPI+`"`) {
		t.Fatalf("expected atomic PRINCIPAL/%s cancel audit, got %s", port.AuditEntryPointAdminAPI, adminDelta)
	}
}

