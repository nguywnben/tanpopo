// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package a2a

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// Dedicated SM-09 A2A Task Lifecycle states (CR-08 — strictly decoupled from SM-06).
const (
	StateSubmitted     = "submitted"
	StateWorking       = "working"
	StateInputRequired = "input-required"
	StateCompleted     = "completed"
	StateFailed        = "failed"
	StateCanceled      = "canceled"

	dbStateSubmitted     = "SUBMITTED"
	dbStateWorking       = "WORKING"
	dbStateInputRequired = "INPUT_REQUIRED"
	dbStateCompleted     = "COMPLETED"
	dbStateFailed        = "FAILED"
	dbStateCanceled      = "CANCELED"

	HeaderTraceRoot   = "X-Tanpopo-Trace-Root"
	HeaderA2AHopCount = "X-Tanpopo-A2A-Hop-Count"
	HeaderDataRegion  = "X-Tanpopo-Data-Region"
	HeaderRequestID   = "X-Tanpopo-Request-Id"
)

var (
	ErrA2ADisabled          = errors.New("a2a: a2a.enabled is false (A2A plane disabled by default)")
	ErrDelegationDepth      = errors.New("a2a: 400 delegation_depth_exceeded")
	ErrResidencyDenied      = errors.New("a2a: 403 residency_incompatible")
	ErrPrivilegeOrBudget    = errors.New("a2a: 403 budget_or_privilege_exceeded")
	ErrInvalidTaskStateMove = errors.New("a2a: invalid SM-09 state transition")
)

// TaskCreateRequest models POST /a2a/tasks matching openapi/dataplane-v1.yaml A2aTaskCreateRequest.
type TaskCreateRequest struct {
	TargetAgentID        string           `json:"target_agent_id"`
	ParentTaskID         string           `json:"parent_task_id,omitempty"`
	SubBudgetHoldMicros  int64            `json:"sub_budget_hold_micros"`
	LineageAgentIDs      []string         `json:"lineage_agent_ids,omitempty"`
	HopCount             int              `json:"hop_count,omitempty"`
	InputParts           []map[string]any `json:"input_parts"`
	DispatchAsync        bool             `json:"dispatch_async,omitempty"`
}

// TaskTransitionRequest updates a non-terminal SM-09 A2A task state or provides input-required resumption data.
type TaskTransitionRequest struct {
	State             string           `json:"state"`
	SpentMicros       int64            `json:"spent_micros,omitempty"`
	Artifacts         []map[string]any `json:"artifacts,omitempty"`
	PromptMessage     string           `json:"prompt_message,omitempty"`
	InputParts        []map[string]any `json:"input_parts,omitempty"`
}

// ResumeTaskRequest provides input_parts to resume an input-required A2A task without allowing state or cost forgery.
type ResumeTaskRequest struct {
	InputParts []map[string]any `json:"input_parts"`
}

// TaskView models the OpenAPI A2aTask schema returned by /a2a/tasks* and SSE events.
type TaskView struct {
	TaskID              string           `json:"task_id"`
	TargetAgentID       string           `json:"target_agent_id"`
	TargetAgentSlug     string           `json:"target_agent_slug,omitempty"`
	State               string           `json:"state"`
	PromptMessage       string           `json:"prompt_message,omitempty"`
	HopCount            int              `json:"hop_count"`
	CallChainIDs        []string         `json:"call_chain_ids,omitempty"`
	TraceRootID         string           `json:"trace_root_id,omitempty"`
	EnforcedRegion      string           `json:"enforced_region,omitempty"`
	SubBudgetHoldMicros int64            `json:"sub_budget_hold_micros"`
	ReservationID       string           `json:"reservation_id,omitempty"`
	SettledCostMicros   int64            `json:"settled_cost_micros,omitempty"`
	Artifacts           []map[string]any `json:"artifacts,omitempty"`
	Version             int64            `json:"version"`
	CreatedAt           string           `json:"created_at"`
	UpdatedAt           string           `json:"updated_at"`
	CompletedAt         string           `json:"completed_at,omitempty"`
}

// UpstreamTaskResponse captures an agent endpoint's execution verdict and exact micro-USD cost.
type UpstreamTaskResponse struct {
	State         string           `json:"state,omitempty"` // working | input-required | completed | failed
	PromptMessage string           `json:"prompt_message,omitempty"`
	SpentMicros   int64            `json:"spent_micros,omitempty"`
	Artifacts     []map[string]any `json:"artifacts,omitempty"`
}

// UpstreamTransport dispatches an attenuated delegation envelope to a registered AgentEndpoint.
type UpstreamTransport interface {
	DispatchTask(ctx context.Context, agent *AgentEndpoint, task TaskView, inputParts []map[string]any) (UpstreamTaskResponse, error)
}

// BrokerUpstreamTransport executes outbound A2A task calls strictly through BlindHostCredentialBroker
// (stripping caller Virtual Keys/Cookies, injecting agent credentials at egress, propagating trace headers).
type BrokerUpstreamTransport struct {
	Broker  *broker.BlindHostCredentialBroker
	Timeout time.Duration
}

func (t *BrokerUpstreamTransport) DispatchTask(ctx context.Context, agent *AgentEndpoint, task TaskView, inputParts []map[string]any) (UpstreamTaskResponse, error) {
	if t == nil || t.Broker == nil || agent == nil {
		return UpstreamTaskResponse{}, errors.New("a2a: broker upstream transport unavailable")
	}
	payload, err := json.Marshal(map[string]any{
		"task_id":                task.TaskID,
		"trace_root_id":          task.TraceRootID,
		"hop_count":              task.HopCount,
		"lineage_agent_ids":      task.CallChainIDs,
		"enforced_region":        task.EnforcedRegion,
		"sub_budget_hold_micros": task.SubBudgetHoldMicros,
		"input_parts":            inputParts,
	})
	if err != nil {
		return UpstreamTaskResponse{}, err
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var injections []broker.CredentialInjection
	if agent.CredentialID != "" {
		injections = []broker.CredentialInjection{{
			SecretSlotName: AgentBrokerSlotName,
			Location:       broker.TargetHeader,
			FieldName:      "Authorization",
			ValuePrefix:    "Bearer ",
		}}
	}
	resp, err := t.Broker.Execute(callCtx, broker.EgressRequest{
		OrgID:        agent.OrgID,
		ConnectionID: agent.ID,
		RequestID:    task.TaskID,
		Method:       http.MethodPost,
		TargetURL:    strings.TrimRight(agent.EndpointURL, "/") + "/tasks",
		SafeHeaders: map[string]string{
			"Content-Type":    "application/json",
			HeaderTraceRoot:   task.TraceRootID,
			HeaderA2AHopCount: strconv.Itoa(task.HopCount),
		},
		BodyBytes:            payload,
		CredentialInjections: injections,
		ActorKind:            "VIRTUAL_KEY",
		EntryPoint:           "a2a_http",
	})
	if err != nil {
		return UpstreamTaskResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UpstreamTaskResponse{State: StateFailed}, fmt.Errorf("a2a upstream returned status %d", resp.StatusCode)
	}
	var parsed UpstreamTaskResponse
	if len(bytes.TrimSpace(resp.Body)) > 0 {
		_ = json.Unmarshal(resp.Body, &parsed)
	}
	if strings.TrimSpace(parsed.State) == "" {
		parsed.State = StateCompleted
	}
	return parsed, nil
}

// CoordinatorConfig configures the A2A Task Coordinator and Data Plane HTTP/SSE handler.
// Enabled defaults to false (disabled) per TP-IMP-0042 Security Invariant & Rollback Plan.
type CoordinatorConfig struct {
	Enabled    bool
	DB         *sql.DB
	Registry   *Registry
	Snapshots  *snapshot.Manager
	KillSwitch *killswitch.Registry
	Broker     *broker.BlindHostCredentialBroker
	Ledger     *ledger.Service
	Audit      port.AuditSink
	Transport  UpstreamTransport
	Now        func() time.Time
	BodyLimit  int64
}

type sseSubscriber struct {
	ch chan []byte
}

// TaskCoordinator orchestrates stateful SM-09 A2A tasks across PG16 FORCE RLS and SQLite.
type TaskCoordinator struct {
	cfg     CoordinatorConfig
	dialect port.Dialect
	reg     *Registry
	now     func() time.Time

	subMu       sync.Mutex
	subscribers map[string]map[*sseSubscriber]struct{}
}

// NewTaskCoordinator constructs an A2A TaskCoordinator.
func NewTaskCoordinator(cfg CoordinatorConfig) (*TaskCoordinator, error) {
	if cfg.DB == nil {
		return nil, errors.New("a2a: non-nil database required")
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	reg := cfg.Registry
	if reg == nil {
		var err error
		reg, err = NewRegistry(RegistryConfig{DB: cfg.DB, Audit: cfg.Audit, Now: nowFn})
		if err != nil {
			return nil, err
		}
	}
	if cfg.BodyLimit <= 0 {
		cfg.BodyLimit = 2 << 20
	}
	if cfg.Transport == nil && cfg.Broker != nil {
		cfg.Transport = &BrokerUpstreamTransport{Broker: cfg.Broker}
	}
	return &TaskCoordinator{
		cfg:         cfg,
		dialect:     port.DetectDialect(context.Background(), cfg.DB),
		reg:         reg,
		now:         nowFn,
		subscribers: make(map[string]map[*sseSubscriber]struct{}),
	}, nil
}

// Enabled reports whether the A2A Data Plane runtime is active.
func (c *TaskCoordinator) Enabled() bool {
	return c != nil && c.cfg.Enabled
}

// Handler returns the net/http handler mounted on Data Plane (/a2a/* and /.well-known/agent.json).
func (c *TaskCoordinator) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", c.handleWellKnown)
	mux.HandleFunc("/a2a/agents", c.handleDiscoverAgents)
	mux.HandleFunc("/a2a/tasks", c.handleCreateTask)
	mux.HandleFunc("/a2a/tasks/", c.handleTaskSubroutes)
	return mux
}

func (c *TaskCoordinator) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	if !c.Enabled() {
		writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "A2A plane is disabled.")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeA2AError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "Method not allowed.")
		return
	}
	id, reqCtx, cleanup, ok := c.authenticateDataPlane(w, r, "/.well-known/agent.json")
	if !ok {
		return
	}
	defer cleanup()
	effModels, effRegions, pErr := c.resolveEffectivePolicyConstraints(id)
	if pErr != nil {
		writeA2AError(w, r, http.StatusServiceUnavailable, "policy_unavailable", "upstream_error", "Effective policy resolution unavailable.")
		return
	}
	cards, err := c.reg.ListDiscoverableAgents(reqCtx, DiscoverFilter{
		OrgID:              id.Key.OrgID,
		ProjectID:          id.Key.ProjectID,
		CallerRegion:       r.Header.Get(HeaderDataRegion),
		AllowedDataRegions: effRegions,
		AllowedModels:      effModels,
		Limit:              50,
	})
	if err != nil {
		writeA2AError(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "Failed loading A2A agents.")
		return
	}
	writeA2AJSON(w, http.StatusOK, map[string]any{
		"name":        "tanpopo-a2a-gateway",
		"description": "Tanpopo A2A Inter-Agent Gateway",
		"agents":      cards,
	})
}

func (c *TaskCoordinator) handleDiscoverAgents(w http.ResponseWriter, r *http.Request) {
	if !c.Enabled() {
		writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "A2A plane is disabled.")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeA2AError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "Method not allowed.")
		return
	}
	id, reqCtx, cleanup, ok := c.authenticateDataPlane(w, r, "/a2a/agents")
	if !ok {
		return
	}
	defer cleanup()
	limit := 50
	if rawLim := strings.TrimSpace(r.URL.Query().Get("limit")); rawLim != "" {
		if parsed, err := strconv.Atoi(rawLim); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	effModels, effRegions, pErr := c.resolveEffectivePolicyConstraints(id)
	if pErr != nil {
		writeA2AError(w, r, http.StatusServiceUnavailable, "policy_unavailable", "upstream_error", "Effective policy resolution unavailable.")
		return
	}
	cards, err := c.reg.ListDiscoverableAgents(reqCtx, DiscoverFilter{
		OrgID:              id.Key.OrgID,
		ProjectID:          id.Key.ProjectID,
		CallerRegion:       r.Header.Get(HeaderDataRegion),
		AllowedDataRegions: effRegions,
		AllowedModels:      effModels,
		Limit:              limit,
	})
	if err != nil {
		writeA2AError(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "Failed discovering A2A agents.")
		return
	}
	writeA2AJSON(w, http.StatusOK, map[string]any{
		"data":        cards,
		"next_cursor": nil,
		"has_more":    false,
	})
}

func (c *TaskCoordinator) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if !c.Enabled() {
		writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "A2A plane is disabled.")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeA2AError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "Method not allowed.")
		return
	}
	id, reqCtx, cleanup, ok := c.authenticateDataPlane(w, r, "/a2a/tasks")
	if !ok {
		return
	}
	defer cleanup()
	r.Body = http.MaxBytesReader(w, r.Body, c.cfg.BodyLimit)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeA2AError(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "Failed reading task body.")
		return
	}
	var req TaskCreateRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeA2AError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "Malformed A2A task JSON.")
		return
	}
	if req.HopCount < 0 {
		writeA2AError(w, r, http.StatusBadRequest, "delegation_depth_exceeded", "invalid_request_error", "hop_count cannot be negative.")
		return
	}
	headerHop := 0
	if rawHop := strings.TrimSpace(r.Header.Get(HeaderA2AHopCount)); rawHop != "" {
		v, err := strconv.Atoi(rawHop)
		if err != nil || v < 1 {
			writeA2AError(w, r, http.StatusBadRequest, "delegation_depth_exceeded", "invalid_request_error", "Invalid X-Tanpopo-A2A-Hop-Count header.")
			return
		}
		headerHop = v
	}
	traceRoot := sanitizeA2ACorrelationID(r.Header.Get(HeaderTraceRoot))
	reqRegion := strings.TrimSpace(r.Header.Get(HeaderDataRegion))
	if req.HopCount == 0 && headerHop > 0 {
		req.HopCount = headerHop
	} else if headerHop > req.HopCount {
		req.HopCount = headerHop
	}

	view, err := c.CreateTask(reqCtx, id, req, reqRegion, traceRoot)
	if err != nil {
		switch {
		case errors.Is(err, ErrDelegationDepth):
			writeA2AError(w, r, http.StatusBadRequest, "delegation_depth_exceeded", "invalid_request_error", err.Error())
		case errors.Is(err, ErrResidencyDenied):
			writeA2AError(w, r, http.StatusForbidden, "residency_incompatible", "permission_error", err.Error())
		case errors.Is(err, ErrPrivilegeOrBudget):
			writeA2AError(w, r, http.StatusForbidden, "budget_or_privilege_exceeded", "permission_error", err.Error())
		case errors.Is(err, ErrAgentNotFound), errors.Is(err, port.ErrNotFound):
			writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "Target A2A agent or parent task not found.")
		default:
			writeA2AError(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error())
		}
		return
	}
	writeA2AJSON(w, http.StatusCreated, view)
}

func (c *TaskCoordinator) handleTaskSubroutes(w http.ResponseWriter, r *http.Request) {
	if !c.Enabled() {
		writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "A2A plane is disabled.")
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/a2a/tasks/")
	if suffix == "" || strings.Contains(suffix, "..") {
		writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "A2A task not found.")
		return
	}
	routePath := "/a2a/tasks/" + suffix
	id, reqCtx, cleanup, ok := c.authenticateDataPlane(w, r, routePath)
	if !ok {
		return
	}
	defer cleanup()
	if strings.HasSuffix(suffix, "/cancel") && r.Method == http.MethodPost {
		taskID := strings.TrimSuffix(suffix, "/cancel")
		if taskID == "" || strings.Contains(taskID, "/") {
			writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "A2A task not found.")
			return
		}
		view, err := c.CancelTask(reqCtx, id.Key.OrgID, id.Key.ProjectID, taskID, id.Key.ID)
		if err != nil {
			if errors.Is(err, port.ErrNotFound) {
				writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "A2A task not found.")
				return
			}
			writeA2AError(w, r, http.StatusConflict, "invalid_state_transition", "invalid_request_error", err.Error())
			return
		}
		writeA2AJSON(w, http.StatusOK, view)
		return
	}
	if strings.HasSuffix(suffix, "/resume") && r.Method == http.MethodPost {
		taskID := strings.TrimSuffix(suffix, "/resume")
		if taskID == "" || strings.Contains(taskID, "/") {
			writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "A2A task not found.")
			return
		}
		var req ResumeTaskRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeA2AError(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "Invalid A2A task resume payload.")
			return
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			writeA2AError(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "Unexpected trailing JSON data.")
			return
		}
		if len(req.InputParts) == 0 {
			writeA2AError(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "input_parts must be a non-empty JSON array.")
			return
		}
		view, err := c.ResumeTask(reqCtx, id, taskID, req.InputParts)
		if err != nil {
			switch {
			case errors.Is(err, port.ErrNotFound):
				writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "A2A task not found.")
			case errors.Is(err, ErrInvalidTaskStateMove):
				writeA2AError(w, r, http.StatusConflict, "task_not_resumable", "invalid_request_error", err.Error())
			case errors.Is(err, ErrPrivilegeOrBudget):
				writeA2AError(w, r, http.StatusForbidden, "budget_or_privilege_exceeded", "permission_error", err.Error())
			default:
				writeA2AError(w, r, http.StatusBadGateway, "upstream_error", "upstream_error", err.Error())
			}
			return
		}
		writeA2AJSON(w, http.StatusOK, view)
		return
	}
	if !strings.Contains(suffix, "/") && r.Method == http.MethodGet {
		taskID := suffix
		view, err := c.getTaskForCaller(reqCtx, id.Key.OrgID, id.Key.ProjectID, taskID, id.Key.ID)
		if err != nil {
			writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "A2A task not found.")
			return
		}
		if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
			c.streamTaskSSE(w, reqCtx, id.Key.OrgID, id.Key.ProjectID, view)
			return
		}
		writeA2AJSON(w, http.StatusOK, view)
		return
	}
	writeA2AError(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "A2A endpoint not found.")
}

func (c *TaskCoordinator) streamTaskSSE(w http.ResponseWriter, ctx context.Context, orgID, projectID string, initial TaskView) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeA2AJSON(w, http.StatusOK, initial)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	sub := &sseSubscriber{ch: make(chan []byte, 16)}
	c.registerSubscriber(initial.TaskID, sub)
	defer c.unregisterSubscriber(initial.TaskID, sub)

	emit := func(data []byte) {
		_, _ = fmt.Fprintf(w, "event: task.status\ndata: %s\n\n", data)
		flusher.Flush()
	}

	initBytes, _ := json.Marshal(initial)
	emit(initBytes)
	if isTerminalState(initial.State) {
		return
	}

	poll := time.NewTicker(1 * time.Second)
	defer poll.Stop()
	lastVer := initial.Version
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-sub.ch:
			var parsed TaskView
			if json.Unmarshal(msg, &parsed) == nil && parsed.Version > lastVer {
				lastVer = parsed.Version
				emit(msg)
				if isTerminalState(parsed.State) {
					return
				}
			}
		case <-poll.C:
			cur, err := c.GetTask(ctx, orgID, projectID, initial.TaskID)
			if err != nil {
				return
			}
			if cur.Version > lastVer {
				lastVer = cur.Version
				b, _ := json.Marshal(cur)
				emit(b)
				if isTerminalState(cur.State) {
					return
				}
			}
		}
	}
}

// CreateTask creates and optionally dispatches an SM-09 A2A task enforcing CT-23 anti-loop,
// hop_count <= 5, residency intersection, privilege narrowing, and atomic SM-07 sub-budget carving.
func (c *TaskCoordinator) CreateTask(ctx context.Context, caller snapshot.KeyMatchResult, req TaskCreateRequest, requestedRegion, traceRoot string) (TaskView, error) {
	if caller.Key == nil {
		return TaskView{}, fmt.Errorf("%w: caller virtual key required", ErrPrivilegeOrBudget)
	}
	if strings.TrimSpace(req.TargetAgentID) == "" {
		return TaskView{}, errors.New("a2a: target_agent_id is required")
	}
	if len(req.InputParts) == 0 {
		return TaskView{}, errors.New("a2a: input_parts cannot be empty")
	}
	if req.SubBudgetHoldMicros < 0 {
		return TaskView{}, fmt.Errorf("%w: sub_budget_hold_micros cannot be negative", ErrPrivilegeOrBudget)
	}
	if req.HopCount < 0 {
		return TaskView{}, fmt.Errorf("%w: hop_count cannot be negative", ErrDelegationDepth)
	}

	agent, err := c.reg.GetAgentInProject(ctx, caller.Key.OrgID, caller.Key.ProjectID, req.TargetAgentID)
	if err != nil {
		return TaskView{}, err
	}
	if agent.ProjectID != caller.Key.ProjectID || agent.Status != AgentStatusActive {
		return TaskView{}, ErrAgentNotFound
	}

	effModels, effRegions, pErr := c.resolveEffectivePolicyConstraints(caller)
	if pErr != nil {
		return TaskView{}, fmt.Errorf("%w: %v", ErrPrivilegeOrBudget, pErr)
	}
	if !regionAllowed(agent.DataRegion, requestedRegion, effRegions) {
		return TaskView{}, fmt.Errorf("%w: requested/policy residency conflicts with target agent region %q", ErrResidencyDenied, agent.DataRegion)
	}
	if !callerPrivilegeAllowsCard(effModels, agent.Card) {
		return TaskView{}, fmt.Errorf("%w: caller virtual key models do not permit target agent capabilities", ErrPrivilegeOrBudget)
	}

	hasParent := strings.TrimSpace(req.ParentTaskID) != ""
	taskID, err := idgen.New(idgen.PrefixTask)
	if err != nil {
		return TaskView{}, err
	}

	var tx *sql.Tx
	rollbackAndRelease := func(resID string) {
		if tx != nil {
			_ = tx.Rollback()
			tx = nil
		}
		c.releaseHoldBestEffort(context.WithoutCancel(ctx), caller.Key.OrgID, resID)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var parentChain []string
	var parentHop int
	var parentTraceRoot string
	var parentEnforcedRegion string
	var parentBudgetCap int64
	if hasParent {
		tx, err = c.cfg.DB.BeginTx(ctx, nil)
		if err != nil {
			return TaskView{}, err
		}
		if err := port.SetOrgRLSTx(ctx, tx, c.dialect, caller.Key.OrgID); err != nil {
			return TaskView{}, err
		}
		if c.dialect == port.DialectSQLite {
			// Acquire SQLite RESERVED write lock before reading parent headroom so concurrent child spawns cannot race.
			if _, lErr := tx.ExecContext(ctx, `UPDATE a2a_tasks SET updated_at_ms = updated_at_ms WHERE org_id = ? AND id = ?`, caller.Key.OrgID, req.ParentTaskID); lErr != nil {
				return TaskView{}, lErr
			}
		}
		parent, parentVKeyID, parentEnvID, _, err := c.loadTaskWithMetaTx(ctx, tx, caller.Key.OrgID, caller.Key.ProjectID, req.ParentTaskID, true)
		if err != nil {
			return TaskView{}, err
		}
		if parentEnvID != caller.Key.EnvID || (parentVKeyID != "" && parentVKeyID != caller.Key.ID) {
			return TaskView{}, fmt.Errorf("%w: parent task %s belongs to a different virtual key or environment", ErrPrivilegeOrBudget, req.ParentTaskID)
		}
		if isTerminalState(parent.State) {
			return TaskView{}, fmt.Errorf("%w: parent task %s is terminal (%s)", ErrPrivilegeOrBudget, parent.TaskID, parent.State)
		}
		if parent.EnforcedRegion != "" && !strings.EqualFold(parent.EnforcedRegion, agent.DataRegion) {
			return TaskView{}, fmt.Errorf("%w: parent region %q conflicts with target agent region %q", ErrResidencyDenied, parent.EnforcedRegion, agent.DataRegion)
		}
		parentChain = append(parentChain, parent.CallChainIDs...)
		parentHop = parent.HopCount
		parentTraceRoot = parent.TraceRootID
		parentEnforcedRegion = parent.EnforcedRegion
		childCommitted, cErr := c.sumCommittedChildSubBudgetsTx(ctx, tx, caller.Key.OrgID, req.ParentTaskID)
		if cErr != nil {
			return TaskView{}, cErr
		}
		parentBudgetCap = parent.SubBudgetHoldMicros - parent.SettledCostMicros - childCommitted
		if req.SubBudgetHoldMicros > parentBudgetCap {
			return TaskView{}, fmt.Errorf("%w: requested sub_budget_hold_micros %d exceeds parent remaining balance %d", ErrPrivilegeOrBudget, req.SubBudgetHoldMicros, parentBudgetCap)
		}
	}

	combinedChain := make([]string, 0, len(parentChain)+len(req.LineageAgentIDs)+1)
	seenChain := make(map[string]struct{})
	for _, entry := range append(parentChain, req.LineageAgentIDs...) {
		clean := strings.TrimSpace(entry)
		if clean == "" {
			continue
		}
		if _, dup := seenChain[clean]; dup {
			return TaskView{}, fmt.Errorf("%w: call_chain contains delegation loop (%s)", ErrDelegationDepth, clean)
		}
		seenChain[clean] = struct{}{}
		combinedChain = append(combinedChain, clean)
	}
	if _, cycleByID := seenChain[agent.ID]; cycleByID {
		return TaskView{}, fmt.Errorf("%w: delegation cycle detected for target agent %s", ErrDelegationDepth, agent.ID)
	}
	if _, cycleBySlug := seenChain[agent.AgentSlug]; cycleBySlug {
		return TaskView{}, fmt.Errorf("%w: delegation cycle detected for target agent %s", ErrDelegationDepth, agent.AgentSlug)
	}
	combinedChain = append(combinedChain, agent.ID)

	effectiveHop := 1
	if parentHop > 0 {
		effectiveHop = parentHop + 1
	}
	if req.HopCount > effectiveHop {
		effectiveHop = req.HopCount
	}
	if len(combinedChain) > effectiveHop {
		effectiveHop = len(combinedChain)
	}
	if effectiveHop > MaxDelegationHopsCeiling || effectiveHop > agent.MaxDelegationHops {
		return TaskView{}, fmt.Errorf("%w: hop_count %d exceeds ceiling (%d)", ErrDelegationDepth, effectiveHop, minInt(MaxDelegationHopsCeiling, agent.MaxDelegationHops))
	}

	enforcedRegion := agent.DataRegion
	if parentEnforcedRegion != "" {
		enforcedRegion = parentEnforcedRegion
	}

	resolvedTraceRoot := sanitizeA2ACorrelationID(traceRoot)
	if resolvedTraceRoot == "" {
		resolvedTraceRoot = parentTraceRoot
	}
	if resolvedTraceRoot == "" {
		resolvedTraceRoot, err = idgen.New(idgen.PrefixTrace)
		if err != nil {
			return TaskView{}, err
		}
	}

	// Reserve root task sub-budget in FinOps ledger once before opening any DB tx;
	// child tasks carve atomically from the locked parent's existing hold without double-reserving.
	var reservationID string
	if !hasParent && c.cfg.Ledger != nil && req.SubBudgetHoldMicros > 0 {
		res, rErr := c.cfg.Ledger.Reserve(ctx, ledger.ReserveRequest{
			OrgID:        caller.Key.OrgID,
			ProjectID:    caller.Key.ProjectID,
			EnvID:        caller.Key.EnvID,
			VirtualKeyID: caller.Key.ID,
			RequestID:    taskID,
			HoldMicros:   req.SubBudgetHoldMicros,
		})
		if rErr != nil {
			if errors.Is(rErr, ledger.ErrBudgetExceeded) {
				return TaskView{}, fmt.Errorf("%w: %v", ErrPrivilegeOrBudget, rErr)
			}
			return TaskView{}, rErr
		}
		reservationID = res.ID
	}

	if tx == nil {
		tx, err = c.cfg.DB.BeginTx(ctx, nil)
		if err != nil {
			rollbackAndRelease(reservationID)
			return TaskView{}, err
		}
		if err := port.SetOrgRLSTx(ctx, tx, c.dialect, caller.Key.OrgID); err != nil {
			rollbackAndRelease(reservationID)
			return TaskView{}, err
		}
	}

	chainBytes, _ := json.Marshal(combinedChain)
	nowMs := c.now().UTC().UnixMilli()
	var parentArg, resArg any
	if hasParent {
		parentArg = req.ParentTaskID
	}
	if reservationID != "" {
		resArg = reservationID
	}
	if _, err := tx.ExecContext(ctx, port.Rebind(c.dialect, `
		INSERT INTO a2a_tasks
			(id, org_id, project_id, env_id, agent_endpoint_id, caller_virtual_key_id,
			 parent_task_id, trace_root_id, hop_count, call_chain_ids_json,
			 sub_budget_cap_micros, settled_cost_micros, reservation_id, enforced_region, artifacts_json, state, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, '[]', 'SUBMITTED', 1, ?, ?)`),
		taskID, caller.Key.OrgID, caller.Key.ProjectID, caller.Key.EnvID, agent.ID, caller.Key.ID,
		parentArg, resolvedTraceRoot, effectiveHop, string(chainBytes),
		req.SubBudgetHoldMicros, resArg, enforcedRegion, nowMs, nowMs,
	); err != nil {
		rollbackAndRelease(reservationID)
		return TaskView{}, fmt.Errorf("a2a: insert a2a_tasks: %w", err)
	}
	_ = c.reg.appendAudit(ctx, tx, caller.Key.OrgID, caller.Key.ID, "VIRTUAL_KEY", port.AuditEntryPointDataPlaneGuard,
		"a2a.task.submitted", "a2a_task", taskID, taskID, map[string]any{
			"target_agent_id":        agent.ID,
			"hop_count":              effectiveHop,
			"trace_root_id":          resolvedTraceRoot,
			"enforced_region":        enforcedRegion,
			"sub_budget_hold_micros": req.SubBudgetHoldMicros,
		})
	if err := tx.Commit(); err != nil {
		rollbackAndRelease(reservationID)
		return TaskView{}, err
	}
	tx = nil

	view := TaskView{
		TaskID:              taskID,
		TargetAgentID:       agent.ID,
		TargetAgentSlug:     agent.AgentSlug,
		State:               StateSubmitted,
		HopCount:            effectiveHop,
		CallChainIDs:        combinedChain,
		TraceRootID:         resolvedTraceRoot,
		EnforcedRegion:      enforcedRegion,
		SubBudgetHoldMicros: req.SubBudgetHoldMicros,
		ReservationID:       reservationID,
		Version:             1,
		CreatedAt:           time.UnixMilli(nowMs).UTC().Format(time.RFC3339Nano),
		UpdatedAt:           time.UnixMilli(nowMs).UTC().Format(time.RFC3339Nano),
	}

	// Synchronous / immediate upstream dispatch: transition SUBMITTED -> WORKING -> terminal/input-required.
	if c.cfg.Transport != nil && !req.DispatchAsync {
		working, tErr := c.TransitionTask(ctx, caller.Key.OrgID, caller.Key.ProjectID, taskID, TaskTransitionRequest{State: StateWorking})
		if tErr != nil {
			return view, tErr
		}
		view = working
		upResp, dErr := c.cfg.Transport.DispatchTask(ctx, agent, view, req.InputParts)
		nextState := normalizeSM09State(upResp.State)
		if dErr != nil {
			nextState = StateFailed
		} else if nextState == "" {
			nextState = StateCompleted
		}
		doneView, trErr := c.TransitionTask(ctx, caller.Key.OrgID, caller.Key.ProjectID, taskID, TaskTransitionRequest{
			State:         nextState,
			PromptMessage: upResp.PromptMessage,
			SpentMicros:   upResp.SpentMicros,
			Artifacts:     upResp.Artifacts,
		})
		if trErr != nil {
			if failedView, fErr := c.TransitionTask(context.WithoutCancel(ctx), caller.Key.OrgID, caller.Key.ProjectID, taskID, TaskTransitionRequest{State: StateFailed}); fErr == nil {
				view = failedView
			}
			return view, trErr
		}
		view = doneView
	}

	return view, nil
}

// GetTask loads an A2A TaskView strictly within (orgID, projectID).
func (c *TaskCoordinator) GetTask(ctx context.Context, orgID, projectID, taskID string) (TaskView, error) {
	if c == nil || c.cfg.DB == nil || strings.TrimSpace(orgID) == "" || strings.TrimSpace(taskID) == "" {
		return TaskView{}, port.ErrNotFound
	}
	tx, err := c.cfg.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TaskView{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, c.dialect, orgID); err != nil {
		return TaskView{}, err
	}
	view, err := c.loadTaskTx(ctx, tx, orgID, projectID, taskID)
	if err != nil {
		return TaskView{}, err
	}
	return view, nil
}

func (c *TaskCoordinator) getTaskForCaller(ctx context.Context, orgID, projectID, taskID, callerVKeyID string) (TaskView, error) {
	if c == nil || c.cfg.DB == nil || strings.TrimSpace(orgID) == "" || strings.TrimSpace(taskID) == "" {
		return TaskView{}, port.ErrNotFound
	}
	tx, err := c.cfg.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TaskView{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, c.dialect, orgID); err != nil {
		return TaskView{}, err
	}
	view, ownerKeyID, _, _, err := c.loadTaskWithMetaTx(ctx, tx, orgID, projectID, taskID, false)
	if err != nil {
		return TaskView{}, err
	}
	if strings.TrimSpace(callerVKeyID) != "" && ownerKeyID != callerVKeyID {
		return TaskView{}, port.ErrNotFound
	}
	return view, nil
}

// ListAdminTasks returns tenant/project A2A tasks for the SCR-10 Console tab.
func (c *TaskCoordinator) ListAdminTasks(ctx context.Context, orgID string, projectIDs []string, orgWide bool, limit int) ([]TaskView, error) {
	if c == nil || c.cfg.DB == nil || strings.TrimSpace(orgID) == "" {
		return nil, port.ErrInvalidInput
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if !orgWide && len(projectIDs) == 0 {
		return []TaskView{}, nil
	}
	tx, err := c.cfg.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, c.dialect, orgID); err != nil {
		return nil, err
	}

	q := `SELECT t.id, t.agent_endpoint_id, COALESCE(e.agent_slug,''), t.state, t.hop_count, t.call_chain_ids_json,
	             t.trace_root_id, COALESCE(t.enforced_region,''), t.sub_budget_cap_micros, t.settled_cost_micros,
	             COALESCE(t.reservation_id,''), t.artifacts_json, t.version, t.created_at_ms, t.updated_at_ms, t.completed_at_ms
	      FROM a2a_tasks t
	      LEFT JOIN agent_endpoints e ON e.org_id = t.org_id AND e.id = t.agent_endpoint_id
	      WHERE t.org_id = ?`
	args := []any{orgID}
	if !orgWide {
		q += ` AND t.project_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(projectIDs)), ",") + `)`
		for _, pid := range projectIDs {
			args = append(args, pid)
		}
	}
	q += ` ORDER BY t.created_at_ms DESC, t.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := tx.QueryContext(ctx, port.Rebind(c.dialect, q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TaskView{}
	for rows.Next() {
		var id, agentID, agentSlug, dbState, chainJSON, traceRoot, region, resID, artifactsJSON string
		var hop int
		var hold, settled, ver, createdMs, updatedMs int64
		var completedMs sql.NullInt64
		if err := rows.Scan(&id, &agentID, &agentSlug, &dbState, &hop, &chainJSON, &traceRoot, &region, &hold, &settled, &resID, &artifactsJSON, &ver, &createdMs, &updatedMs, &completedMs); err != nil {
			return nil, err
		}
		var chain []string
		_ = json.Unmarshal([]byte(chainJSON), &chain)
		var artifacts []map[string]any
		_ = json.Unmarshal([]byte(artifactsJSON), &artifacts)
		v := TaskView{
			TaskID:              id,
			TargetAgentID:       agentID,
			TargetAgentSlug:     agentSlug,
			State:               dbStateToAPI(dbState),
			HopCount:            hop,
			CallChainIDs:        chain,
			TraceRootID:         traceRoot,
			EnforcedRegion:      region,
			SubBudgetHoldMicros: hold,
			SettledCostMicros:   settled,
			ReservationID:       resID,
			Artifacts:           artifacts,
			Version:             ver,
			CreatedAt:           time.UnixMilli(createdMs).UTC().Format(time.RFC3339Nano),
			UpdatedAt:           time.UnixMilli(updatedMs).UTC().Format(time.RFC3339Nano),
		}
		if completedMs.Valid {
			v.CompletedAt = time.UnixMilli(completedMs.Int64).UTC().Format(time.RFC3339Nano)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// AdminTaskPage holds a keyset-paginated slice of A2A tasks for Admin HTTP.
type AdminTaskPage struct {
	Items   []TaskView
	HasMore bool
	LastMs  int64
	LastID  string
}

// ListAdminTasksPage returns keyset-paginated A2A tasks ordered by (created_at_ms DESC, id DESC).
func (c *TaskCoordinator) ListAdminTasksPage(ctx context.Context, orgID string, projectIDs []string, orgWide bool, limit int, cursorCreatedAtMs *int64, cursorID *string) (AdminTaskPage, error) {
	if c == nil || c.cfg.DB == nil || strings.TrimSpace(orgID) == "" {
		return AdminTaskPage{}, port.ErrInvalidInput
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if !orgWide && len(projectIDs) == 0 {
		return AdminTaskPage{Items: []TaskView{}}, nil
	}

	tx, err := c.cfg.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminTaskPage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, c.dialect, orgID); err != nil {
		return AdminTaskPage{}, err
	}

	q := `SELECT t.id, t.agent_endpoint_id, COALESCE(e.agent_slug,''), t.state, t.hop_count, t.call_chain_ids_json,
	             t.trace_root_id, COALESCE(t.enforced_region,''), t.sub_budget_cap_micros, t.settled_cost_micros,
	             COALESCE(t.reservation_id,''), t.artifacts_json, t.version, t.created_at_ms, t.updated_at_ms, t.completed_at_ms
	      FROM a2a_tasks t
	      LEFT JOIN agent_endpoints e ON e.org_id = t.org_id AND e.id = t.agent_endpoint_id
	      WHERE t.org_id = ?`
	args := []any{orgID}
	if !orgWide {
		q += ` AND t.project_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(projectIDs)), ",") + `)`
		for _, pid := range projectIDs {
			args = append(args, pid)
		}
	}
	if cursorCreatedAtMs != nil && cursorID != nil && strings.TrimSpace(*cursorID) != "" {
		q += ` AND (t.created_at_ms < ? OR (t.created_at_ms = ? AND t.id < ?))`
		args = append(args, *cursorCreatedAtMs, *cursorCreatedAtMs, strings.TrimSpace(*cursorID))
	}
	q += ` ORDER BY t.created_at_ms DESC, t.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := tx.QueryContext(ctx, port.Rebind(c.dialect, q), args...)
	if err != nil {
		return AdminTaskPage{}, err
	}
	defer rows.Close()

	out := []TaskView{}
	var timestamps []int64
	for rows.Next() {
		var id, agentID, agentSlug, dbState, chainJSON, traceRoot, region, resID, artifactsJSON string
		var hop int
		var hold, settled, ver, createdMs, updatedMs int64
		var completedMs sql.NullInt64
		if err := rows.Scan(&id, &agentID, &agentSlug, &dbState, &hop, &chainJSON, &traceRoot, &region, &hold, &settled, &resID, &artifactsJSON, &ver, &createdMs, &updatedMs, &completedMs); err != nil {
			return AdminTaskPage{}, err
		}
		var chain []string
		_ = json.Unmarshal([]byte(chainJSON), &chain)
		promptMsg, artifacts := splitPromptAndArtifacts(artifactsJSON)
		v := TaskView{
			TaskID:              id,
			TargetAgentID:       agentID,
			TargetAgentSlug:     agentSlug,
			State:               dbStateToAPI(dbState),
			PromptMessage:       promptMsg,
			HopCount:            hop,
			CallChainIDs:        chain,
			TraceRootID:         traceRoot,
			EnforcedRegion:      region,
			SubBudgetHoldMicros: hold,
			SettledCostMicros:   settled,
			ReservationID:       resID,
			Artifacts:           artifacts,
			Version:             ver,
			CreatedAt:           time.UnixMilli(createdMs).UTC().Format(time.RFC3339Nano),
			UpdatedAt:           time.UnixMilli(updatedMs).UTC().Format(time.RFC3339Nano),
		}
		if completedMs.Valid {
			v.CompletedAt = time.UnixMilli(completedMs.Int64).UTC().Format(time.RFC3339Nano)
		}
		out = append(out, v)
		timestamps = append(timestamps, createdMs)
	}
	if err := rows.Err(); err != nil {
		return AdminTaskPage{}, err
	}
	hasMore := false
	if len(out) > limit {
		hasMore = true
		out = out[:limit]
		timestamps = timestamps[:limit]
	}
	page := AdminTaskPage{Items: out, HasMore: hasMore}
	if len(out) > 0 {
		page.LastMs = timestamps[len(out)-1]
		page.LastID = out[len(out)-1].TaskID
	}
	return page, nil
}

type taskAuditAttribution struct {
	actorID    string
	actorKind  string
	entryPoint string
	eventType  string
}

func defaultDataPlaneTaskAudit() taskAuditAttribution {
	return taskAuditAttribution{
		actorKind:  "VIRTUAL_KEY",
		entryPoint: port.AuditEntryPointDataPlaneGuard,
		eventType:  "a2a.task.transitioned",
	}
}

// TransitionTask advances SM-09 state atomically, persists artifacts & settled_cost_micros,
// settles/releases FinOps sub-budget holds fail-closed before marking terminal, and broadcasts SSE updates.
func (c *TaskCoordinator) TransitionTask(ctx context.Context, orgID, projectID, taskID string, req TaskTransitionRequest) (TaskView, error) {
	return c.transitionTaskInternal(ctx, orgID, projectID, taskID, "", defaultDataPlaneTaskAudit(), req)
}

func (c *TaskCoordinator) transitionTaskInternal(ctx context.Context, orgID, projectID, taskID, requiredCallerKeyID string, auditMeta taskAuditAttribution, req TaskTransitionRequest) (TaskView, error) {
	targetAPI := normalizeSM09State(req.State)
	if targetAPI == "" {
		return TaskView{}, fmt.Errorf("%w: unknown state %q", ErrInvalidTaskStateMove, req.State)
	}
	if req.SpentMicros < 0 {
		return TaskView{}, fmt.Errorf("%w: spent_micros cannot be negative", ErrPrivilegeOrBudget)
	}
	nowMs := c.now().UTC().UnixMilli()

	// Step 1: Read current task state & validate transition and hold bounds before ledger settlement.
	var cur TaskView
	var callerVKeyID, envID, agentSlug string
	{
		rtx, err := c.cfg.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return TaskView{}, err
		}
		if err := port.SetOrgRLSTx(ctx, rtx, c.dialect, orgID); err != nil {
			_ = rtx.Rollback()
			return TaskView{}, err
		}
		cur, callerVKeyID, envID, agentSlug, err = c.loadTaskWithMetaTx(ctx, rtx, orgID, projectID, taskID, false)
		if err == nil && (targetAPI == StateCompleted || targetAPI == StateFailed) {
			err = c.ensureNoActiveChildrenTx(ctx, rtx, orgID, taskID)
		}
		_ = rtx.Rollback()
		if err != nil {
			return TaskView{}, err
		}
	}
	if requiredCallerKeyID != "" && callerVKeyID != requiredCallerKeyID {
		return TaskView{}, port.ErrNotFound
	}
	if !validSM09Transition(cur.State, targetAPI) {
		return TaskView{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTaskStateMove, cur.State, targetAPI)
	}
	subHold := cur.SubBudgetHoldMicros
	if req.SpentMicros > subHold {
		return TaskView{}, fmt.Errorf("%w: spent_micros %d exceeds sub_budget_hold_micros %d", ErrPrivilegeOrBudget, req.SpentMicros, subHold)
	}

	// Step 2: Settle or release FinOps ledger hold fail-closed BEFORE committing terminal DB state (idempotent by taskID/reservationID).
	settledCost := cur.SettledCostMicros
	if isTerminalState(targetAPI) {
		if c.cfg.Ledger != nil {
			if req.SpentMicros > 0 && (targetAPI == StateCompleted || targetAPI == StateFailed) {
				sRes, sErr := c.cfg.Ledger.Settle(ctx, ledger.SettleRequest{
					OrgID:            orgID,
					ReservationID:    cur.ReservationID,
					RequestID:        taskID,
					ProjectID:        projectID,
					EnvID:            envID,
					VirtualKeyID:     callerVKeyID,
					Plane:            ledger.PlaneA2A,
					DeploymentID:     cur.TargetAgentID,
					LogicalModelSlug: agentSlug,
					Rate: pricing.RateCard{
						BillingMode: pricing.BillingModeCommercialAPI,
					},
					Usage: pricing.UsageInput{
						DirectMicroUSD:       req.SpentMicros,
						UpstreamUsagePresent: true,
						UpstreamAccuracy:     "EXACT",
					},
				})
				if sErr != nil {
					return TaskView{}, fmt.Errorf("a2a: settle ledger failed: %w", sErr)
				}
				if sRes != nil {
					settledCost = sRes.TotalCostMicros
				}
			} else if cur.ReservationID != "" {
				if _, rErr := c.cfg.Ledger.Release(ctx, orgID, cur.ReservationID); rErr != nil {
					return TaskView{}, fmt.Errorf("a2a: release ledger hold failed: %w", rErr)
				}
			}
		} else if req.SpentMicros > 0 && (targetAPI == StateCompleted || targetAPI == StateFailed) {
			settledCost = req.SpentMicros
		}
	}

	mergedArtifacts := append([]map[string]any(nil), cur.Artifacts...)
	if len(req.Artifacts) > 0 {
		mergedArtifacts = append(mergedArtifacts, req.Artifacts...)
	}
	nextPrompt := ""
	if targetAPI == StateInputRequired {
		nextPrompt = strings.TrimSpace(req.PromptMessage)
		if nextPrompt == "" {
			nextPrompt = cur.PromptMessage
		}
	}
	toPersist := encodeArtifactsWithPrompt(mergedArtifacts, nextPrompt)
	artifactsBytes, err := json.Marshal(toPersist)
	if err != nil {
		return TaskView{}, err
	}

	// Step 3: Commit optimistic-concurrency versioned SM-09 transition + artifacts + settled_cost_micros.
	targetDB := apiStateToDB(targetAPI)
	var completedAt any
	if isTerminalState(targetAPI) {
		completedAt = nowMs
	}
	tx, err := c.cfg.DB.BeginTx(ctx, nil)
	if err != nil {
		return TaskView{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, c.dialect, orgID); err != nil {
		return TaskView{}, err
	}
	if targetAPI == StateCompleted || targetAPI == StateFailed {
		if err := c.ensureNoActiveChildrenTx(ctx, tx, orgID, taskID); err != nil {
			return TaskView{}, err
		}
	}
	res, err := tx.ExecContext(ctx, port.Rebind(c.dialect, `
		UPDATE a2a_tasks
		SET state = ?, settled_cost_micros = ?, artifacts_json = ?, version = version + 1, updated_at_ms = ?, completed_at_ms = COALESCE(completed_at_ms, ?)
		WHERE org_id = ? AND id = ? AND version = ?`),
		targetDB, settledCost, string(artifactsBytes), nowMs, completedAt, orgID, taskID, cur.Version,
	)
	if err != nil {
		return TaskView{}, err
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		return TaskView{}, port.ErrVersionConflict
	}
	if targetAPI == StateCanceled {
		if err := c.cascadeCancelDescendantsTx(ctx, tx, orgID, taskID, nowMs); err != nil {
			return TaskView{}, err
		}
	}
	actorID := strings.TrimSpace(auditMeta.actorID)
	if actorID == "" {
		actorID = callerVKeyID
	}
	actorKind := strings.TrimSpace(auditMeta.actorKind)
	if actorKind == "" {
		actorKind = "VIRTUAL_KEY"
	}
	entryPoint := auditMeta.entryPoint
	if entryPoint == "" {
		entryPoint = port.AuditEntryPointDataPlaneGuard
	}
	eventType := strings.TrimSpace(auditMeta.eventType)
	if eventType == "" {
		eventType = "a2a.task.transitioned"
	}
	if err := c.reg.appendAudit(ctx, tx, orgID, actorID, actorKind, entryPoint,
		eventType, "a2a_task", taskID, taskID, map[string]any{
			"from_state":   cur.State,
			"to_state":     targetAPI,
			"spent_micros": req.SpentMicros,
		}); err != nil {
		return TaskView{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskView{}, err
	}

	updated := cur
	updated.State = targetAPI
	updated.PromptMessage = nextPrompt
	updated.Version = cur.Version + 1
	updated.UpdatedAt = time.UnixMilli(nowMs).UTC().Format(time.RFC3339Nano)
	if isTerminalState(targetAPI) {
		updated.CompletedAt = updated.UpdatedAt
	}
	updated.SettledCostMicros = settledCost
	updated.Artifacts = mergedArtifacts
	c.broadcastTask(updated)

	// Resume upstream dispatch when transitioning input-required -> working with new InputParts.
	if cur.State == StateInputRequired && targetAPI == StateWorking && len(req.InputParts) > 0 && c.cfg.Transport != nil {
		ep, epErr := c.reg.GetAgentInProject(ctx, orgID, projectID, cur.TargetAgentID)
		if epErr != nil {
			return updated, epErr
		}
		upResp, dErr := c.cfg.Transport.DispatchTask(ctx, ep, updated, req.InputParts)
		nextState := normalizeSM09State(upResp.State)
		if dErr != nil {
			nextState = StateFailed
		} else if nextState == "" {
			nextState = StateCompleted
		}
		nextView, trErr := c.transitionTaskInternal(ctx, orgID, projectID, taskID, requiredCallerKeyID, auditMeta, TaskTransitionRequest{
			State:         nextState,
			PromptMessage: upResp.PromptMessage,
			SpentMicros:   upResp.SpentMicros,
			Artifacts:     upResp.Artifacts,
		})
		if trErr != nil {
			if failedView, fErr := c.transitionTaskInternal(context.WithoutCancel(ctx), orgID, projectID, taskID, requiredCallerKeyID, auditMeta, TaskTransitionRequest{State: StateFailed}); fErr == nil {
				updated = failedView
			}
			return updated, trErr
		}
		return nextView, nil
	}

	return updated, nil
}

// ResumeTask resumes an input-required task owned by the authenticated Data Plane caller Virtual Key.
func (c *TaskCoordinator) ResumeTask(ctx context.Context, caller snapshot.KeyMatchResult, taskID string, inputParts []map[string]any) (TaskView, error) {
	if caller.Key == nil {
		return TaskView{}, fmt.Errorf("%w: caller virtual key required", ErrPrivilegeOrBudget)
	}
	if len(inputParts) == 0 {
		return TaskView{}, errors.New("a2a: input_parts cannot be empty")
	}
	cur, err := c.getTaskForCaller(ctx, caller.Key.OrgID, caller.Key.ProjectID, taskID, caller.Key.ID)
	if err != nil {
		return TaskView{}, err
	}
	if cur.State != StateInputRequired {
		return TaskView{}, fmt.Errorf("%w: task in state %s cannot be resumed (expected input-required)", ErrInvalidTaskStateMove, cur.State)
	}
	if !routeAllowlistPermitsA2A(caller.Key.AllowedRoutes, "/a2a/tasks/"+taskID+"/resume") {
		return TaskView{}, fmt.Errorf("%w: virtual key route allowlist does not permit resuming task", ErrPrivilegeOrBudget)
	}
	return c.transitionTaskInternal(ctx, caller.Key.OrgID, caller.Key.ProjectID, taskID, caller.Key.ID, taskAuditAttribution{
		actorID:    caller.Key.ID,
		actorKind:  "VIRTUAL_KEY",
		entryPoint: port.AuditEntryPointDataPlaneGuard,
		eventType:  "a2a.task.transitioned",
	}, TaskTransitionRequest{
		State:      StateWorking,
		InputParts: inputParts,
	})
}

// ResumeAdminTask resumes an input-required A2A task from Control Plane Admin API within verified project scope.
func (c *TaskCoordinator) ResumeAdminTask(ctx context.Context, orgID, projectID, taskID, actorID string, inputParts []map[string]any) (TaskView, error) {
	if len(inputParts) == 0 {
		return TaskView{}, errors.New("a2a: input_parts cannot be empty")
	}
	cur, err := c.GetTask(ctx, orgID, projectID, taskID)
	if err != nil {
		return TaskView{}, err
	}
	if cur.State != StateInputRequired {
		return TaskView{}, fmt.Errorf("%w: task in state %s cannot be resumed (expected input-required)", ErrInvalidTaskStateMove, cur.State)
	}
	return c.transitionTaskInternal(ctx, orgID, projectID, taskID, "", taskAuditAttribution{
		actorID:    actorID,
		actorKind:  "PRINCIPAL",
		entryPoint: port.AuditEntryPointAdminAPI,
		eventType:  "a2a.task.resumed",
	}, TaskTransitionRequest{
		State:      StateWorking,
		InputParts: inputParts,
	})
}

// CancelAdminTask cancels a non-terminal A2A task from Control Plane Admin API within verified project scope.
func (c *TaskCoordinator) CancelAdminTask(ctx context.Context, orgID, projectID, taskID, actorID string) (TaskView, error) {
	return c.transitionTaskInternal(ctx, orgID, projectID, taskID, "", taskAuditAttribution{
		actorID:    actorID,
		actorKind:  "PRINCIPAL",
		entryPoint: port.AuditEntryPointAdminAPI,
		eventType:  "a2a.task.canceled_by_admin",
	}, TaskTransitionRequest{State: StateCanceled})
}

// CancelTask cancels a non-terminal A2A task owned by callerVKeyID and releases any held sub-budget.
func (c *TaskCoordinator) CancelTask(ctx context.Context, orgID, projectID, taskID, callerVKeyID string) (TaskView, error) {
	return c.transitionTaskInternal(ctx, orgID, projectID, taskID, strings.TrimSpace(callerVKeyID), defaultDataPlaneTaskAudit(), TaskTransitionRequest{State: StateCanceled})
}

func (c *TaskCoordinator) loadTaskTx(ctx context.Context, tx *sql.Tx, orgID, projectID, taskID string) (TaskView, error) {
	v, _, _, _, err := c.loadTaskWithMetaTx(ctx, tx, orgID, projectID, taskID, false)
	return v, err
}

func (c *TaskCoordinator) loadTaskTxForUpdate(ctx context.Context, tx *sql.Tx, orgID, projectID, taskID string) (TaskView, error) {
	v, _, _, _, err := c.loadTaskWithMetaTx(ctx, tx, orgID, projectID, taskID, true)
	return v, err
}

func (c *TaskCoordinator) loadTaskWithMetaTx(ctx context.Context, tx *sql.Tx, orgID, projectID, taskID string, forUpdate bool) (TaskView, string, string, string, error) {
	q := `SELECT t.id, t.project_id, t.env_id, t.agent_endpoint_id, COALESCE(e.agent_slug,''),
	             t.caller_virtual_key_id, t.trace_root_id, t.hop_count, t.call_chain_ids_json,
	             t.sub_budget_cap_micros, t.settled_cost_micros, COALESCE(t.reservation_id,''), COALESCE(t.enforced_region,''),
	             t.artifacts_json, t.state, t.version, t.created_at_ms, t.updated_at_ms, t.completed_at_ms
	      FROM a2a_tasks t
	      LEFT JOIN agent_endpoints e ON e.org_id = t.org_id AND e.id = t.agent_endpoint_id
	      WHERE t.org_id = ? AND t.id = ?`
	args := []any{orgID, taskID}
	if strings.TrimSpace(projectID) != "" {
		q += ` AND t.project_id = ?`
		args = append(args, projectID)
	}
	if forUpdate && c.dialect == port.DialectPostgres {
		q += ` FOR UPDATE OF t`
	}
	var id, pID, envID, agentID, agentSlug, vkeyID, traceRoot, chainJSON, resID, region, artifactsJSON, dbState string
	var hop int
	var hold, settled, ver, createdMs, updatedMs int64
	var completedMs sql.NullInt64
	err := tx.QueryRowContext(ctx, port.Rebind(c.dialect, q), args...).Scan(
		&id, &pID, &envID, &agentID, &agentSlug, &vkeyID, &traceRoot, &hop, &chainJSON,
		&hold, &settled, &resID, &region, &artifactsJSON, &dbState, &ver, &createdMs, &updatedMs, &completedMs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskView{}, "", "", "", port.ErrNotFound
	}
	if err != nil {
		return TaskView{}, "", "", "", err
	}
	var chain []string
	_ = json.Unmarshal([]byte(chainJSON), &chain)
	promptMsg, artifacts := splitPromptAndArtifacts(artifactsJSON)
	v := TaskView{
		TaskID:              id,
		TargetAgentID:       agentID,
		TargetAgentSlug:     agentSlug,
		State:               dbStateToAPI(dbState),
		PromptMessage:       promptMsg,
		HopCount:            hop,
		CallChainIDs:        chain,
		TraceRootID:         traceRoot,
		EnforcedRegion:      region,
		SubBudgetHoldMicros: hold,
		SettledCostMicros:   settled,
		ReservationID:       resID,
		Artifacts:           artifacts,
		Version:             ver,
		CreatedAt:           time.UnixMilli(createdMs).UTC().Format(time.RFC3339Nano),
		UpdatedAt:           time.UnixMilli(updatedMs).UTC().Format(time.RFC3339Nano),
	}
	if completedMs.Valid {
		v.CompletedAt = time.UnixMilli(completedMs.Int64).UTC().Format(time.RFC3339Nano)
	}
	return v, vkeyID, envID, agentSlug, nil
}

const promptArtifactKey = "__tanpopo_prompt_message"

func encodeArtifactsWithPrompt(artifacts []map[string]any, promptMsg string) []map[string]any {
	out := make([]map[string]any, 0, len(artifacts)+1)
	for _, a := range artifacts {
		if a == nil {
			continue
		}
		if _, isPrompt := a[promptArtifactKey]; isPrompt {
			continue
		}
		out = append(out, a)
	}
	if trimmed := strings.TrimSpace(promptMsg); trimmed != "" {
		out = append(out, map[string]any{promptArtifactKey: trimmed})
	}
	return out
}

func splitPromptAndArtifacts(artifactsJSON string) (string, []map[string]any) {
	var raw []map[string]any
	_ = json.Unmarshal([]byte(artifactsJSON), &raw)
	promptMsg := ""
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if item == nil {
			continue
		}
		if v, ok := item[promptArtifactKey]; ok {
			if s, okStr := v.(string); okStr {
				promptMsg = strings.TrimSpace(s)
			}
			continue
		}
		out = append(out, item)
	}
	return promptMsg, out
}

func (c *TaskCoordinator) registerSubscriber(taskID string, sub *sseSubscriber) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	if c.subscribers[taskID] == nil {
		c.subscribers[taskID] = make(map[*sseSubscriber]struct{})
	}
	c.subscribers[taskID][sub] = struct{}{}
}

func (c *TaskCoordinator) unregisterSubscriber(taskID string, sub *sseSubscriber) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	if set := c.subscribers[taskID]; set != nil {
		delete(set, sub)
		if len(set) == 0 {
			delete(c.subscribers, taskID)
		}
	}
}

func (c *TaskCoordinator) broadcastTask(v TaskView) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.subMu.Lock()
	defer c.subMu.Unlock()
	for sub := range c.subscribers[v.TaskID] {
		select {
		case sub.ch <- data:
		default:
		}
	}
}

func (c *TaskCoordinator) releaseHoldBestEffort(ctx context.Context, orgID, reservationID string) {
	if c != nil && c.cfg.Ledger != nil && reservationID != "" {
		_, _ = c.cfg.Ledger.Release(context.WithoutCancel(ctx), orgID, reservationID)
	}
}

func hasAdminSessionCookie(r *http.Request) bool {
	if r == nil {
		return false
	}
	for _, c := range r.Cookies() {
		name := strings.TrimSpace(c.Name)
		if name == "__Host-tanpopo_admin_session" || name == "tanpopo_admin_session" {
			return true
		}
	}
	return false
}

func (c *TaskCoordinator) authenticateDataPlane(w http.ResponseWriter, r *http.Request, routePath string) (snapshot.KeyMatchResult, context.Context, func(), bool) {
	noop := func() {}
	if c.cfg.Snapshots == nil {
		writeA2AError(w, r, http.StatusServiceUnavailable, "service_unavailable", "upstream_error", "Snapshot manager unavailable.")
		return snapshot.KeyMatchResult{}, r.Context(), noop, false
	}
	if err := c.cfg.Snapshots.CheckServingReady(); err != nil {
		writeA2AError(w, r, http.StatusServiceUnavailable, "stale_security_state", "upstream_error", "Gateway security snapshot not ready.")
		return snapshot.KeyMatchResult{}, r.Context(), noop, false
	}
	if hasAdminSessionCookie(r) {
		writeA2AError(w, r, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "Admin session cookies are prohibited on A2A endpoints; authenticate with a Virtual Key Bearer token.")
		return snapshot.KeyMatchResult{}, r.Context(), noop, false
	}
	raw := extractBearerVirtualKey(r)
	if raw == "" {
		writeA2AError(w, r, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "A2A endpoints require a Data Plane Virtual Key.")
		return snapshot.KeyMatchResult{}, r.Context(), noop, false
	}
	if !strings.HasPrefix(raw, "vk_tp_") {
		writeA2AError(w, r, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "Presented credential is not a Data Plane Virtual Key.")
		return snapshot.KeyMatchResult{}, r.Context(), noop, false
	}

	streamCtx, cancelRaw := context.WithCancel(r.Context())
	targetEnvID := strings.TrimSpace(r.Header.Get("X-Tanpopo-Env-Id"))
	match, handleID, err := c.cfg.Snapshots.RegisterAuthenticatedStream(raw, targetEnvID, cancelRaw)
	if err != nil || match.Key == nil || match.Config == nil || !match.Config.IsOperational() {
		if match.Key != nil && handleID != 0 {
			c.cfg.Snapshots.Streams().Unregister(match.Key.ID, handleID)
		}
		cancelRaw()
		writeA2AError(w, r, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Invalid or inactive Virtual Key.")
		return snapshot.KeyMatchResult{}, r.Context(), noop, false
	}

	var ksHandleID uint64
	if c.cfg.KillSwitch != nil {
		q := killswitch.AdmissionQuery{
			OrgID:        match.Key.OrgID,
			ProjectID:    match.Key.ProjectID,
			VirtualKeyID: match.Key.ID,
		}
		if ks, blocked := c.cfg.KillSwitch.CheckAdmission(q); blocked {
			c.cfg.Snapshots.Streams().Unregister(match.Key.ID, handleID)
			cancelRaw()
			writeA2AError(w, r, http.StatusServiceUnavailable, "kill_switch_active", "permission_error", fmt.Sprintf("Request blocked by emergency kill switch (%s): %s", ks.Level, ks.Reason))
			return snapshot.KeyMatchResult{}, r.Context(), noop, false
		}
		ksHandleID = c.cfg.KillSwitch.RegisterStream(killswitch.StreamRegistration{
			OrgID:        match.Key.OrgID,
			ProjectID:    match.Key.ProjectID,
			VirtualKeyID: match.Key.ID,
			Cancel:       cancelRaw,
		})
		if ks, blocked := c.cfg.KillSwitch.CheckAdmission(q); blocked {
			c.cfg.KillSwitch.UnregisterStream(ksHandleID)
			c.cfg.Snapshots.Streams().Unregister(match.Key.ID, handleID)
			cancelRaw()
			writeA2AError(w, r, http.StatusServiceUnavailable, "kill_switch_active", "permission_error", fmt.Sprintf("Request blocked by emergency kill switch (%s): %s", ks.Level, ks.Reason))
			return snapshot.KeyMatchResult{}, r.Context(), noop, false
		}
	}

	cleanup := func() {
		if c.cfg.KillSwitch != nil && ksHandleID != 0 {
			c.cfg.KillSwitch.UnregisterStream(ksHandleID)
		}
		c.cfg.Snapshots.Streams().Unregister(match.Key.ID, handleID)
		cancelRaw()
	}

	if !containsPlane(match.Key.AllowedPlanes, "A2A") {
		cleanup()
		writeA2AError(w, r, http.StatusForbidden, "scope_or_plane_denied", "permission_error", "Virtual Key is not permitted on A2A plane.")
		return snapshot.KeyMatchResult{}, r.Context(), noop, false
	}
	if !routeAllowlistPermitsA2A(match.Key.AllowedRoutes, routePath) {
		cleanup()
		writeA2AError(w, r, http.StatusForbidden, "scope_or_plane_denied", "permission_error", fmt.Sprintf("Virtual Key route allowlist denies %s.", routePath))
		return snapshot.KeyMatchResult{}, r.Context(), noop, false
	}
	return match, streamCtx, cleanup, true
}

func routeAllowlistPermitsA2A(allowedRoutes []string, routePath string) bool {
	if len(allowedRoutes) == 0 {
		return false
	}
	for _, entry := range allowedRoutes {
		tr := strings.TrimSpace(entry)
		if tr == "*" || tr == "/a2a/*" || tr == "a2a" || tr == routePath {
			return true
		}
		if strings.HasSuffix(tr, "/*") && strings.HasPrefix(routePath, strings.TrimSuffix(tr, "*")) {
			return true
		}
	}
	return false
}

func (c *TaskCoordinator) resolveEffectivePolicyConstraints(caller snapshot.KeyMatchResult) ([]string, []string, error) {
	if caller.Key == nil {
		return nil, nil, ErrPrivilegeOrBudget
	}
	models := append([]string(nil), caller.Key.AllowedModels...)
	var regions []string
	if caller.Config != nil {
		if caller.Config.EnforcedRegion != nil && strings.TrimSpace(*caller.Config.EnforcedRegion) != "" {
			regions = []string{strings.ToUpper(strings.TrimSpace(*caller.Config.EnforcedRegion))}
		} else if caller.Config.DefaultDataRegion != nil && strings.TrimSpace(*caller.Config.DefaultDataRegion) != "" {
			regions = []string{strings.ToUpper(strings.TrimSpace(*caller.Config.DefaultDataRegion))}
		}
	}
	if c != nil && c.cfg.Snapshots != nil {
		if snap := c.cfg.Snapshots.Current(); snap != nil {
			eff, err := policyengine.MergeEffectiveFromSnapshot(snap, caller.Config, caller.Key)
			if err != nil {
				return nil, nil, err
			}
			if eff.HasAllowedModels {
				models = append([]string(nil), eff.AllowedModels...)
			}
			if len(eff.DeniedModels) > 0 && len(models) > 0 {
				denied := make(map[string]struct{}, len(eff.DeniedModels))
				for _, dm := range eff.DeniedModels {
					denied[strings.TrimSpace(dm)] = struct{}{}
				}
				filtered := make([]string, 0, len(models))
				for _, m := range models {
					if _, isDenied := denied[strings.TrimSpace(m)]; !isDenied {
						filtered = append(filtered, m)
					}
				}
				models = filtered
			}
			if eff.HasAllowedDataRegions {
				regions = append([]string(nil), eff.AllowedDataRegions...)
			}
		}
	}
	return models, regions, nil
}

func extractBearerVirtualKey(r *http.Request) string {
	var candidates []string
	for _, hdr := range []string{"X-Tanpopo-Virtual-Key", "X-Api-Key"} {
		for _, raw := range r.Header.Values(hdr) {
			v := strings.TrimSpace(raw)
			if v == "" {
				continue
			}
			if !strings.HasPrefix(v, "vk_tp_") {
				return ""
			}
			candidates = append(candidates, v)
		}
	}
	for _, rawAuth := range r.Header.Values("Authorization") {
		header := strings.TrimSpace(rawAuth)
		if header == "" {
			continue
		}
		kind, raw, found := strings.Cut(header, " ")
		if !found || !strings.EqualFold(kind, "Bearer") {
			return ""
		}
		tok := strings.TrimSpace(raw)
		if tok == "" || strings.ContainsAny(tok, " \t\r\n") {
			return ""
		}
		candidates = append(candidates, tok)
	}
	if len(candidates) != 1 {
		return ""
	}
	return candidates[0]
}

func containsPlane(allowed, want string) bool {
	for _, part := range strings.Split(allowed, ",") {
		if strings.EqualFold(strings.TrimSpace(part), want) {
			return true
		}
	}
	return false
}

func validSM09Transition(from, to string) bool {
	switch from {
	case StateSubmitted:
		return to == StateWorking || to == StateFailed || to == StateCanceled
	case StateWorking:
		return to == StateInputRequired || to == StateCompleted || to == StateFailed || to == StateCanceled
	case StateInputRequired:
		return to == StateWorking || to == StateCompleted || to == StateFailed || to == StateCanceled
	default:
		return false
	}
}

func isTerminalState(state string) bool {
	return state == StateCompleted || state == StateFailed || state == StateCanceled
}

func normalizeSM09State(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case StateSubmitted:
		return StateSubmitted
	case StateWorking:
		return StateWorking
	case StateInputRequired, "input_required":
		return StateInputRequired
	case StateCompleted:
		return StateCompleted
	case StateFailed:
		return StateFailed
	case StateCanceled, "cancelled":
		return StateCanceled
	default:
		return ""
	}
}

func apiStateToDB(api string) string {
	switch api {
	case StateSubmitted:
		return dbStateSubmitted
	case StateWorking:
		return dbStateWorking
	case StateInputRequired:
		return dbStateInputRequired
	case StateCompleted:
		return dbStateCompleted
	case StateFailed:
		return dbStateFailed
	case StateCanceled:
		return dbStateCanceled
	default:
		return ""
	}
}

func dbStateToAPI(db string) string {
	switch strings.ToUpper(strings.TrimSpace(db)) {
	case dbStateSubmitted:
		return StateSubmitted
	case dbStateWorking:
		return StateWorking
	case dbStateInputRequired:
		return StateInputRequired
	case dbStateCompleted:
		return StateCompleted
	case dbStateFailed:
		return StateFailed
	case dbStateCanceled:
		return StateCanceled
	default:
		return StateSubmitted
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var a2aCorrelationIDRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,64}$`)

func sanitizeA2ACorrelationID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" && a2aCorrelationIDRe.MatchString(trimmed) {
		return trimmed
	}
	return ""
}

func resolveOrMintA2ARequestID(r *http.Request) string {
	if r != nil {
		if sanitized := sanitizeA2ACorrelationID(r.Header.Get(HeaderRequestID)); sanitized != "" {
			return sanitized
		}
	}
	if fresh, err := idgen.New(idgen.PrefixTrace); err == nil {
		return fresh
	}
	return "req_a2a"
}

func writeA2AJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeA2AError(w http.ResponseWriter, r *http.Request, status int, code, errType, message string) {
	reqID := resolveOrMintA2ARequestID(r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set(HeaderRequestID, reqID)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":       code,
			"type":       errType,
			"message":    message,
			"request_id": reqID,
		},
	})
}

// IsValidEndpointHost parses URL safely for diagnostics.
func IsValidEndpointHost(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.Host != ""
}

func (c *TaskCoordinator) sumCommittedChildSubBudgetsTx(ctx context.Context, tx *sql.Tx, orgID, parentTaskID string) (int64, error) {
	var sum int64
	if err := tx.QueryRowContext(ctx, port.Rebind(c.dialect, `
		SELECT COALESCE(SUM(
			CASE WHEN state IN ('COMPLETED', 'FAILED', 'CANCELED')
			     THEN settled_cost_micros
			     ELSE sub_budget_cap_micros
			END
		), 0)
		FROM a2a_tasks
		WHERE org_id = ? AND parent_task_id = ?`),
		orgID, parentTaskID,
	).Scan(&sum); err != nil {
		return 0, err
	}
	return sum, nil
}

func (c *TaskCoordinator) ensureNoActiveChildrenTx(ctx context.Context, tx *sql.Tx, orgID, parentTaskID string) error {
	var activeChildren int64
	if err := tx.QueryRowContext(ctx, port.Rebind(c.dialect, `
		SELECT COUNT(1)
		FROM a2a_tasks
		WHERE org_id = ? AND parent_task_id = ?
		  AND state NOT IN ('COMPLETED', 'FAILED', 'CANCELED')`),
		orgID, parentTaskID,
	).Scan(&activeChildren); err != nil {
		return err
	}
	if activeChildren > 0 {
		return fmt.Errorf("%w: parent task %s has %d non-terminal child task(s)", ErrInvalidTaskStateMove, parentTaskID, activeChildren)
	}
	return nil
}

func (c *TaskCoordinator) cascadeCancelDescendantsTx(ctx context.Context, tx *sql.Tx, orgID, rootTaskID string, nowMs int64) error {
	_, err := tx.ExecContext(ctx, port.Rebind(c.dialect, `
		WITH RECURSIVE tree AS (
			SELECT id FROM a2a_tasks WHERE org_id = ? AND parent_task_id = ?
			UNION ALL
			SELECT t.id FROM a2a_tasks t
			JOIN tree ON t.parent_task_id = tree.id
			WHERE t.org_id = ?
		)
		UPDATE a2a_tasks
		SET state = 'CANCELED',
		    version = version + 1,
		    updated_at_ms = ?,
		    completed_at_ms = COALESCE(completed_at_ms, ?)
		WHERE org_id = ?
		  AND id IN (SELECT id FROM tree)
		  AND state NOT IN ('COMPLETED', 'FAILED', 'CANCELED')`),
		orgID, rootTaskID, orgID, nowMs, nowMs, orgID,
	)
	return err
}
