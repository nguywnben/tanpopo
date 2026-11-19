// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package hub implements the fail-closed MCP Streamable HTTP governance plane.
package hub

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/schema_pin"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	sessionHeader         = "Mcp-Session-Id"
	maxActiveSessions     = 4096
	maxSessionsPerKey     = 64
	maxSessionQueueSize   = 32
	maxSSEStreams         = 256
	maxSessionSSEStreams  = 2
	maxProgressTokenBytes = 128
	maxActiveToolCalls    = 512
	defaultKeyToolCalls   = 16
)

// ToolDefinition is the upstream MCP tool shape used during sync and invocation checks.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// UpstreamClient performs a single scoped request to a registered MCP server. Implementations
// must use the SSRF-guarded BlindHostCredentialBroker and never forward inbound client headers.
type UpstreamClient interface {
	ListTools(context.Context, serverRecord) ([]ToolDefinition, error)
	CallTool(context.Context, serverRecord, string, json.RawMessage) (json.RawMessage, error)
	VerifyAndCallTool(context.Context, serverRecord, string, json.RawMessage, string, schema_pin.Definition) (schema_pin.Verification, json.RawMessage, error)
}

// Config contains the minimum dependencies for the MCP Hub.
type Config struct {
	DB         *sql.DB
	Snapshots  *snapshot.Manager
	Broker     *broker.BlindHostCredentialBroker
	Audit      port.AuditSink
	Upstream   UpstreamClient
	Now        func() time.Time
	CursorKey  []byte
	SessionTTL time.Duration
	BodyLimit  int64
}

type session struct {
	keyID     string
	orgID     string
	projectID string
	envID     string
	expiresAt time.Time
	events    chan []byte
	done      chan struct{}
	streams   int
	timer     *time.Timer
}

// Server serves the MCP 2025-03-26 Streamable HTTP transport at /mcp.
type Server struct {
	cfg             Config
	catalog         *catalog
	cursor          []byte
	mu              sync.Mutex
	sessions        map[string]session
	requests        map[string]map[string]context.CancelFunc
	activeStreams   int
	activeToolCalls int
	toolCallsByKey  map[string]int
}

// NewServer creates an MCP handler and fails closed when required dependencies are absent.
func NewServer(cfg Config) (*Server, error) {
	if cfg.DB == nil || cfg.Snapshots == nil || cfg.Broker == nil || cfg.Audit == nil || cfg.Upstream == nil {
		return nil, errors.New("MCP Hub requires database, snapshots, credential broker, audit sink, and upstream client")
	}
	cat, err := newCatalog(cfg.DB)
	if err != nil {
		return nil, err
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 30 * time.Minute
	}
	if cfg.BodyLimit <= 0 || cfg.BodyLimit > maxJSONRPCBytes {
		cfg.BodyLimit = maxJSONRPCBytes
	}
	key := append([]byte(nil), cfg.CursorKey...)
	if len(key) < 32 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("initialize MCP cursor key: %w", err)
		}
	}
	return &Server{cfg: cfg, catalog: cat, cursor: key, sessions: make(map[string]session), requests: make(map[string]map[string]context.CancelFunc), toolCallsByKey: make(map[string]int)}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handlePost(w, r)
	case http.MethodGet:
		s.handleGet(w, r)
	case http.MethodDelete:
		s.handleDelete(w, r)
	default:
		w.Header().Set("Allow", "POST, GET, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	identity, err := s.authenticate(r)
	if err != nil {
		writeHTTPError(w, http.StatusUnauthorized, "invalid_virtual_key")
		return
	}
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaErr != nil || !strings.EqualFold(mediaType, "application/json") {
		writeHTTPError(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return
	}
	if !acceptsMediaType(r.Header.Values("Accept"), "application/json") || !acceptsMediaType(r.Header.Values("Accept"), "text/event-stream") {
		writeHTTPError(w, http.StatusNotAcceptable, "unsupported_accept")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.BodyLimit)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_json_rpc")
		return
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_json_rpc")
		return
	}
	req, err := parseRequest(raw)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_json_rpc")
		return
	}
	if req.Method == "initialize" {
		s.initialize(w, r, identity, req)
		return
	}
	if err := s.validateSession(r, identity); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_session")
		return
	}
	if len(req.ID) == 0 {
		if req.Method == "notifications/cancelled" {
			s.cancelRequest(r.Header.Get(sessionHeader), req.Params)
		}
		if req.Method == "notifications/initialized" || req.Method == "notifications/cancelled" || strings.HasPrefix(req.Method, "notifications/") {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeHTTPError(w, http.StatusBadRequest, "invalid_json_rpc")
		return
	}

	var result response
	switch req.Method {
	case "tools/list":
		result = s.listTools(r.Context(), identity, req)
	case "tools/call":
		limit := int(identity.match.Key.MaxConcurrentStreams)
		if limit <= 0 {
			limit = defaultKeyToolCalls
		}
		if limit > maxActiveToolCalls {
			limit = maxActiveToolCalls
		}
		ctx, finish, admitted := s.trackRequest(r.Context(), r.Header.Get(sessionHeader), string(req.ID), identity.scope.KeyID, limit)
		if admitted {
			result = s.callTool(ctx, identity, r.Header.Get(sessionHeader), req)
			finish()
		} else {
			result = rpcFailure(req.ID, -32000, "too_many_concurrent_calls", nil)
		}
	default:
		result = rpcFailure(req.ID, -32601, "Method not found", nil)
	}
	writeRPC(w, http.StatusOK, result)
}

type identity struct {
	match snapshot.KeyMatchResult
	scope scope
}

func (s *Server) authenticate(r *http.Request) (identity, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	kind, raw, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(kind, "Bearer") || raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return identity{}, errors.New("missing bearer virtual key")
	}
	match, err := s.cfg.Snapshots.AuthenticateKey(raw, "")
	if err != nil || match.Key == nil || match.Config == nil || !match.Config.IsOperational() || !containsPlane(match.Key.AllowedPlanes, "MCP") {
		return identity{}, errors.New("virtual key is not allowed to use the MCP plane")
	}
	allowed := make(map[string]struct{}, len(match.Key.AllowedMCPTools))
	for _, value := range match.Key.AllowedMCPTools {
		if strings.TrimSpace(value) != "" {
			allowed[value] = struct{}{}
		}
	}
	return identity{match: match, scope: scope{OrgID: match.Key.OrgID, ProjectID: match.Key.ProjectID,
		EnvID: match.Key.EnvID, KeyID: match.Key.ID, Tools: allowed}}, nil
}

func containsPlane(planes, wanted string) bool {
	for _, value := range strings.Split(strings.ToUpper(planes), ",") {
		if strings.TrimSpace(value) == wanted {
			return true
		}
	}
	return false
}

func (s *Server) initialize(w http.ResponseWriter, r *http.Request, id identity, req request) {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &params) != nil {
		writeRPC(w, http.StatusOK, rpcFailure(req.ID, -32602, "Invalid params", nil))
		return
	}
	sessionID, err := randomToken(32)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	m := id.match.Key
	now := s.cfg.Now()
	s.mu.Lock()
	keySessions := 0
	for token, existing := range s.sessions {
		if !existing.expiresAt.After(now) {
			s.deleteSessionLocked(token, existing.done)
			continue
		}
		if existing.keyID == m.ID {
			keySessions++
		}
	}
	if len(s.sessions) >= maxActiveSessions {
		s.mu.Unlock()
		writeHTTPError(w, http.StatusServiceUnavailable, "session_capacity_exceeded")
		return
	}
	if keySessions >= maxSessionsPerKey {
		s.mu.Unlock()
		writeHTTPError(w, http.StatusTooManyRequests, "session_key_limit_exceeded")
		return
	}
	done := make(chan struct{})
	expiresAt := now.Add(s.cfg.SessionTTL)
	timer := time.AfterFunc(s.cfg.SessionTTL, func() {
		s.mu.Lock()
		s.deleteSessionLocked(sessionID, done)
		s.mu.Unlock()
	})
	s.sessions[sessionID] = session{keyID: m.ID, orgID: m.OrgID, projectID: m.ProjectID, envID: m.EnvID,
		expiresAt: expiresAt, events: make(chan []byte, maxSessionQueueSize), done: done, timer: timer}
	s.mu.Unlock()
	w.Header().Set(sessionHeader, sessionID)
	writeRPC(w, http.StatusOK, rpcResponse(req.ID, map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]string{"name": "tanpopo-mcp-hub", "version": "0.2"},
	}))
}

func (s *Server) deleteSessionLocked(token string, expectedDone chan struct{}) {
	current, ok := s.sessions[token]
	if !ok || current.done != expectedDone {
		return
	}
	delete(s.sessions, token)
	if current.timer != nil {
		current.timer.Stop()
	}
	close(current.done)
}

func (s *Server) validateSession(r *http.Request, id identity) error {
	token := strings.TrimSpace(r.Header.Get(sessionHeader))
	if token == "" {
		return errors.New("Mcp-Session-Id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[token]
	if !ok || !current.expiresAt.After(s.cfg.Now()) || current.keyID != id.scope.KeyID || current.orgID != id.scope.OrgID || current.projectID != id.scope.ProjectID || current.envID != id.scope.EnvID {
		if ok {
			s.deleteSessionLocked(token, current.done)
		}
		return errors.New("MCP session is invalid or expired")
	}
	return nil
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := s.authenticate(r)
	if err != nil || s.validateSession(r, id) != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_session")
		return
	}
	s.mu.Lock()
	token := r.Header.Get(sessionHeader)
	if current, ok := s.sessions[token]; ok {
		s.deleteSessionLocked(token, current.done)
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	if !acceptsMediaType(r.Header.Values("Accept"), "text/event-stream") {
		writeHTTPError(w, http.StatusNotAcceptable, "unsupported_accept")
		return
	}
	id, err := s.authenticate(r)
	if err != nil || s.validateSession(r, id) != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_session")
		return
	}
	token := r.Header.Get(sessionHeader)
	s.mu.Lock()
	current, exists := s.sessions[token]
	if !exists || !current.expiresAt.After(s.cfg.Now()) {
		s.mu.Unlock()
		writeHTTPError(w, http.StatusBadRequest, "invalid_session")
		return
	}
	if current.streams >= maxSessionSSEStreams || s.activeStreams >= maxSSEStreams {
		s.mu.Unlock()
		writeHTTPError(w, http.StatusTooManyRequests, "too_many_event_streams")
		return
	}
	current.streams++
	s.sessions[token] = current
	s.activeStreams++
	s.mu.Unlock()
	defer s.releaseStream(token)
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeHTTPError(w, http.StatusInternalServerError, "streaming_unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	timer := time.NewTimer(time.Until(current.expiresAt))
	defer timer.Stop()
	for {
		select {
		case event := <-current.events:
			if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", event); err != nil {
				return
			}
			flusher.Flush()
		case <-current.done:
			return
		case <-timer.C:
			s.mu.Lock()
			s.deleteSessionLocked(token, current.done)
			s.mu.Unlock()
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) releaseStream(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeStreams > 0 {
		s.activeStreams--
	}
	if current, ok := s.sessions[token]; ok && current.streams > 0 {
		current.streams--
		s.sessions[token] = current
	}
}

func (s *Server) trackRequest(parent context.Context, sessionID, requestID, keyID string, limit int) (context.Context, func(), bool) {
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	current, ok := s.sessions[sessionID]
	if !ok || s.requests[sessionID] != nil && s.requests[sessionID][requestID] != nil || s.activeToolCalls >= maxActiveToolCalls || s.toolCallsByKey[keyID] >= limit {
		s.mu.Unlock()
		cancel()
		return ctx, func() {}, false
	}
	requests := s.requests[sessionID]
	if requests == nil {
		requests = make(map[string]context.CancelFunc)
		s.requests[sessionID] = requests
	}
	requests[requestID] = cancel
	s.activeToolCalls++
	s.toolCallsByKey[keyID]++
	done := current.done
	s.mu.Unlock()

	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-done:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		s.mu.Lock()
		if requests := s.requests[sessionID]; requests != nil {
			delete(requests, requestID)
			if len(requests) == 0 {
				delete(s.requests, sessionID)
			}
		}
		if s.activeToolCalls > 0 {
			s.activeToolCalls--
		}
		if s.toolCallsByKey[keyID] > 1 {
			s.toolCallsByKey[keyID]--
		} else {
			delete(s.toolCallsByKey, keyID)
		}
		s.mu.Unlock()
		cancel()
		<-watchDone
	}, true
}

func (s *Server) cancelRequest(sessionID string, params json.RawMessage) {
	var notification struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if len(params) == 0 || json.Unmarshal(params, &notification) != nil || len(notification.RequestID) == 0 {
		return
	}
	s.mu.Lock()
	cancel := s.requests[sessionID][string(notification.RequestID)]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) publishProgress(sessionID string, progressToken json.RawMessage, progress, total int) {
	if len(progressToken) == 0 {
		return
	}
	message, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress", "params": map[string]any{
		"progressToken": progressToken, "progress": progress, "total": total,
	}})
	if err != nil {
		return
	}
	s.mu.Lock()
	current, ok := s.sessions[sessionID]
	if ok && current.expiresAt.After(s.cfg.Now()) {
		select {
		case current.events <- message:
		default:
		}
	}
	s.mu.Unlock()
}

func acceptsMediaType(headers []string, wanted string) bool {
	for _, header := range headers {
		for _, value := range strings.Split(header, ",") {
			mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(value))
			if err != nil || !strings.EqualFold(mediaType, wanted) {
				continue
			}
			if rawQuality, exists := params["q"]; exists {
				quality, err := strconv.ParseFloat(rawQuality, 64)
				if err != nil || quality <= 0 || quality > 1 {
					continue
				}
			}
			return true
		}
	}
	return false
}

func (s *Server) listTools(ctx context.Context, id identity, req request) response {
	var params struct {
		Cursor string `json:"cursor"`
	}
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &params) != nil {
		return rpcFailure(req.ID, -32602, "Invalid params", nil)
	}
	tools, err := s.catalog.listGrantedTools(ctx, id.scope)
	if err != nil {
		return rpcFailure(req.ID, -32603, "Internal error", nil)
	}
	scopeKey := id.scope.OrgID + ":" + id.scope.ProjectID + ":" + id.scope.EnvID + ":" + id.scope.KeyID
	start := 0
	if params.Cursor != "" {
		start, err = decodeCursor(s.cursor, params.Cursor, scopeKey, "tools/list")
		if err != nil {
			return rpcFailure(req.ID, -32602, "Invalid cursor", nil)
		}
	}
	if start > len(tools) {
		return rpcFailure(req.ID, -32602, "Invalid cursor", nil)
	}
	const pageSize = 100
	end := start + pageSize
	if end > len(tools) {
		end = len(tools)
	}
	visible := make([]map[string]any, 0, end-start)
	for _, record := range tools[start:end] {
		var schema struct {
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		}
		if json.Unmarshal(record.Schema, &schema) != nil || len(schema.InputSchema) == 0 {
			continue
		}
		name, err := toolName(record.Server.Slug, record.Name)
		if err != nil {
			continue
		}
		visible = append(visible, map[string]any{"name": name, "description": schema.Description, "inputSchema": schema.InputSchema})
	}
	result := map[string]any{"tools": visible}
	if end < len(tools) {
		result["nextCursor"] = encodeCursor(s.cursor, scopeKey, "tools/list", end)
	}
	return rpcResponse(req.ID, result)
}

func (s *Server) callTool(ctx context.Context, id identity, sessionID string, req request) response {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Meta      struct {
			ProgressToken json.RawMessage `json:"progressToken"`
		} `json:"_meta"`
	}
	if json.Unmarshal(req.Params, &params) != nil || len(req.Params) == 0 || params.Name == "" || len(params.Arguments) == 0 {
		return rpcFailure(req.ID, -32602, "Invalid params", nil)
	}
	slug, rawName, ok := splitToolName(params.Name)
	if !ok {
		return rpcFailure(req.ID, -32602, "Invalid namespaced tool name", nil)
	}
	record, err := s.catalog.getGrantedTool(ctx, id.scope, slug, rawName)
	if err != nil {
		return rpcFailure(req.ID, -32003, "tool_access_denied", nil)
	}
	if record.DriftLocked {
		return rpcFailure(req.ID, -32003, "schema_hash_drift_blocked", nil)
	}
	if record.Server.Transport != "STREAMABLE_HTTP" {
		return rpcFailure(req.ID, -32003, "unsupported_transport", nil)
	}
	if s.cfg.Upstream == nil {
		return rpcFailure(req.ID, -32603, "Internal error", nil)
	}
	if err := validateArgumentsContext(ctx, record.Schema, params.Arguments); err != nil {
		return rpcFailure(req.ID, -32602, "Invalid tool arguments", nil)
	}
	if !validProgressToken(params.Meta.ProgressToken) {
		return rpcFailure(req.ID, -32602, "Invalid progress token", nil)
	}
	s.publishProgress(sessionID, params.Meta.ProgressToken, 0, 2)
	verification, result, err := s.cfg.Upstream.VerifyAndCallTool(ctx, record.Server, record.Name, params.Arguments, record.ApprovedHash, definitionFromStored(record))
	if err != nil {
		if errors.Is(err, schema_pin.ErrInvalidDefinition) || errors.Is(err, schema_pin.ErrSchemaTooLarge) || errors.Is(err, schema_pin.ErrApprovedHash) {
			observedHash := verification.Hash
			if observedHash == "" {
				observedHash = record.ObservedHash
			}
			if lockErr := s.catalog.lockSchemaDrift(ctx, record, observedHash); lockErr != nil {
				return rpcFailure(req.ID, -32603, "Internal error", nil)
			}
			if auditErr := s.auditDrift(ctx, record, observedHash, "invalid_schema_definition"); auditErr != nil {
				return rpcFailure(req.ID, -32603, "Internal error", nil)
			}
			return rpcFailure(req.ID, -32003, "schema_hash_drift_blocked", nil)
		}
		return rpcFailure(req.ID, -32000, "tool_call_failed", nil)
	}
	if verification.Drift || verification.Hash != record.ObservedHash {
		if lockErr := s.catalog.lockSchemaDrift(ctx, record, verification.Hash); lockErr != nil {
			return rpcFailure(req.ID, -32603, "Internal error", nil)
		}
		if auditErr := s.auditDrift(ctx, record, verification.Hash, "schema_hash_mismatch"); auditErr != nil {
			return rpcFailure(req.ID, -32603, "Internal error", nil)
		}
		return rpcFailure(req.ID, -32003, "schema_hash_drift_blocked", map[string]any{"approved": record.ApprovedHash, "observed": verification.Hash, "diff": verification.Diff})
	}
	s.publishProgress(sessionID, params.Meta.ProgressToken, 1, 2)
	if len(result) > maxToolBytes {
		return rpcFailure(req.ID, -32000, "upstream_response_too_large", nil)
	}
	if err := rejectDuplicateJSONKeys(result); err != nil {
		return rpcFailure(req.ID, -32000, "invalid_upstream_response", nil)
	}
	var value any
	if json.Unmarshal(result, &value) != nil {
		return rpcFailure(req.ID, -32000, "invalid_upstream_response", nil)
	}
	return rpcResponse(req.ID, value)
}

func validProgressToken(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	if len(raw) > maxProgressTokenBytes {
		return false
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return false
	}
	switch value.(type) {
	case string, json.Number:
		return true
	default:
		return false
	}
}

func (s *Server) auditDrift(ctx context.Context, record toolRecord, observed, reason string) error {
	org := record.Server.OrgID
	id, err := idgen.New("aud")
	if err != nil {
		return err
	}
	meta, _ := json.Marshal(map[string]string{"server_slug": record.Server.Slug, "tool_name": record.Name,
		"approved_schema_sha256": record.ApprovedHash, "observed_schema_sha256": observed, "reason": reason})
	return s.cfg.Audit.Append(ctx, port.AuditEvent{
		ID: id, CreatedAtMs: s.cfg.Now().UnixMilli(), OrgID: &org,
		ActorKind: "SYSTEM", ActorID: "mcp_hub", EntryPoint: "data_plane_guard",
		EventType: "mcp.tool_schema_drift_detected", TargetType: "mcp_tool", TargetID: record.ID,
		MetadataJSON: string(meta),
	})
}

func definitionFromStored(record toolRecord) schema_pin.Definition {
	var stored struct {
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	if json.Unmarshal(record.Schema, &stored) != nil {
		return schema_pin.Definition{}
	}
	return schema_pin.Definition{Name: record.Name, Description: stored.Description, InputSchema: stored.InputSchema}
}

func writeRPC(w http.ResponseWriter, status int, value response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeHTTPError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
