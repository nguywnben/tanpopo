// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package fakemcp provides a deterministic MCP Streamable HTTP test server.
package fakemcp

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
)

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type Call struct {
	Name      string
	Arguments json.RawMessage
}

type Server struct {
	mu       sync.Mutex
	tools    []Tool
	calls    []Call
	pageSize int
}

func New(tools []Tool, pageSize int) *Server {
	if pageSize <= 0 {
		pageSize = 100
	}
	return &Server{tools: cloneTools(tools), pageSize: pageSize}
}

func (s *Server) SetTools(tools []Tool) {
	s.mu.Lock()
	s.tools = cloneTools(tools)
	s.mu.Unlock()
}

func (s *Server) MutateDescription(name, description string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tools {
		if s.tools[i].Name == name {
			s.tools[i].Description = description
			return true
		}
	}
	return false
}

func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Call, len(s.calls))
	copy(out, s.calls)
	return out
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req) != nil || req.JSONRPC != "2.0" || req.Method == "" {
		writeError(w, req.ID, -32600, "Invalid Request")
		return
	}
	if req.Method == "initialize" {
		w.Header().Set("Mcp-Session-Id", "fake-session")
		writeResult(w, req.ID, map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "fake-mcp", "version": "1"}})
		return
	}
	if req.Method == "notifications/initialized" || req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch req.Method {
	case "tools/list":
		var params struct {
			Cursor string `json:"cursor"`
		}
		if len(req.Params) > 0 && json.Unmarshal(req.Params, &params) != nil {
			writeError(w, req.ID, -32602, "Invalid params")
			return
		}
		offset := 0
		if params.Cursor != "" {
			var err error
			offset, err = strconv.Atoi(params.Cursor)
			if err != nil || offset < 0 || offset > len(s.tools) {
				writeError(w, req.ID, -32602, "Invalid cursor")
				return
			}
		}
		end := min(offset+s.pageSize, len(s.tools))
		result := map[string]any{"tools": s.tools[offset:end]}
		if end < len(s.tools) {
			result["nextCursor"] = strconv.Itoa(end)
		}
		writeResult(w, req.ID, result)
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.Name == "" {
			writeError(w, req.ID, -32602, "Invalid params")
			return
		}
		for _, tool := range s.tools {
			if tool.Name == params.Name {
				s.calls = append(s.calls, Call{Name: params.Name, Arguments: append(json.RawMessage(nil), params.Arguments...)})
				writeResult(w, req.ID, map[string]any{"content": []any{map[string]string{"type": "text", "text": "ok"}}, "isError": false})
				return
			}
		}
		writeError(w, req.ID, -32602, "Unknown tool")
	default:
		writeError(w, req.ID, -32601, "Method not found")
	}
}

func writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func cloneTools(tools []Tool) []Tool {
	out := make([]Tool, len(tools))
	for i, tool := range tools {
		out[i] = tool
		out[i].InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
	}
	return out
}

var _ http.Handler = (*Server)(nil)
