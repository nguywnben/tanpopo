// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package hub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nguywnben/tanpopo/internal/ingress/mcp/schema_pin"
	"github.com/nguywnben/tanpopo/internal/security/broker"
)

// BrokerClient sends MCP JSON-RPC only through BlindHostCredentialBroker. It never copies
// inbound Authorization, Cookie, or arbitrary client headers to an upstream server.
type BrokerClient struct {
	Broker  *broker.BlindHostCredentialBroker
	Timeout time.Duration
	seq     atomic.Uint64
}

const defaultMCPUpstreamTimeout = 30 * time.Second

func (c *BrokerClient) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultMCPUpstreamTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

type upstreamReply struct {
	Result    json.RawMessage
	SessionID string
	Status    int
	Type      string
}

func (c *BrokerClient) ListTools(ctx context.Context, server serverRecord) ([]ToolDefinition, error) {
	if c == nil || c.Broker == nil || server.Transport != "STREAMABLE_HTTP" {
		return nil, errors.New("MCP broker client is unavailable or transport unsupported")
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	init, err := c.rpc(ctx, server, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "tanpopo-mcp-hub", "version": "0.2"},
	}, "")
	if err != nil {
		return nil, err
	}
	sessionID := init.SessionID
	if _, err := c.notify(ctx, server, "notifications/initialized", map[string]any{}, sessionID); err != nil {
		return nil, err
	}
	return c.listToolsInSession(ctx, server, sessionID)
}

func (c *BrokerClient) listToolsInSession(ctx context.Context, server serverRecord, sessionID string) ([]ToolDefinition, error) {
	var all []ToolDefinition
	cursor := ""
	catalogBytes := 0
	for page := 0; page < 100; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		reply, err := c.rpc(ctx, server, "tools/list", params, sessionID)
		if err != nil {
			return nil, err
		}
		catalogBytes, err = schema_pin.AddCatalogPageBytes(catalogBytes, len(reply.Result))
		if err != nil {
			return nil, errors.New("MCP upstream tool catalog exceeds byte limit")
		}
		var value struct {
			Tools      []ToolDefinition `json:"tools"`
			NextCursor string           `json:"nextCursor"`
		}
		if err := json.Unmarshal(reply.Result, &value); err != nil {
			return nil, errors.New("invalid upstream tools/list response")
		}
		all = append(all, value.Tools...)
		if len(all) > 10000 {
			return nil, errors.New("upstream tool catalog exceeds limit")
		}
		if value.NextCursor == "" {
			return all, nil
		}
		if value.NextCursor == cursor {
			return nil, errors.New("upstream tools/list cursor did not advance")
		}
		cursor = value.NextCursor
	}
	return nil, errors.New("upstream tools/list exceeded pagination limit")
}

func (c *BrokerClient) VerifyAndCallTool(ctx context.Context, server serverRecord, name string, arguments json.RawMessage, approvedHash string, approved schema_pin.Definition) (schema_pin.Verification, json.RawMessage, error) {
	if c == nil || c.Broker == nil || server.Transport != "STREAMABLE_HTTP" {
		return schema_pin.Verification{}, nil, errors.New("MCP broker client is unavailable or transport unsupported")
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	init, err := c.rpc(ctx, server, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "tanpopo-mcp-hub", "version": "0.2"},
	}, "")
	if err != nil {
		return schema_pin.Verification{}, nil, err
	}
	if _, err := c.notify(ctx, server, "notifications/initialized", map[string]any{}, init.SessionID); err != nil {
		return schema_pin.Verification{}, nil, err
	}
	tools, err := c.listToolsInSession(ctx, server, init.SessionID)
	if err != nil {
		return schema_pin.Verification{}, nil, err
	}
	var current *ToolDefinition
	for index := range tools {
		if tools[index].Name == name {
			current = &tools[index]
			break
		}
	}
	if current == nil {
		return schema_pin.Verification{}, nil, errors.New("MCP tool is not present in upstream catalog")
	}
	verification, err := schema_pin.Verify(approvedHash, approved, schema_pin.Definition{
		Name: current.Name, Description: current.Description, InputSchema: current.InputSchema,
	})
	if err != nil || verification.Drift {
		return verification, nil, err
	}
	reply, err := c.rpc(ctx, server, "tools/call", map[string]any{"name": name, "arguments": arguments}, init.SessionID)
	if err != nil {
		return verification, nil, err
	}
	return verification, reply.Result, nil
}

func (c *BrokerClient) CallTool(ctx context.Context, server serverRecord, name string, arguments json.RawMessage) (json.RawMessage, error) {
	if c == nil || c.Broker == nil || server.Transport != "STREAMABLE_HTTP" {
		return nil, errors.New("MCP broker client is unavailable or transport unsupported")
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	init, err := c.rpc(ctx, server, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "tanpopo-mcp-hub", "version": "0.2"},
	}, "")
	if err != nil {
		return nil, err
	}
	if _, err := c.notify(ctx, server, "notifications/initialized", map[string]any{}, init.SessionID); err != nil {
		return nil, err
	}
	reply, err := c.rpc(ctx, server, "tools/call", map[string]any{"name": name, "arguments": arguments}, init.SessionID)
	return reply.Result, err
}

func (c *BrokerClient) rpc(ctx context.Context, server serverRecord, method string, params any, sessionID string) (upstreamReply, error) {
	id := c.seq.Add(1)
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return upstreamReply{}, err
	}
	reply, err := c.send(ctx, server, body, sessionID)
	if err != nil {
		return upstreamReply{}, err
	}
	if reply.Status != http.StatusOK {
		return upstreamReply{}, fmt.Errorf("MCP upstream returned HTTP %d", reply.Status)
	}
	payload := reply.Result
	if strings.Contains(strings.ToLower(reply.Type), "text/event-stream") {
		payload, err = lastSSEMessage(reply.Result)
		if err != nil {
			return upstreamReply{}, err
		}
	}
	if err := ValidateMCPResponseJSON(payload); err != nil {
		return upstreamReply{}, errors.New("invalid or overdeep MCP upstream JSON")
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	var responseID uint64
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.JSONRPC != "2.0" || json.Unmarshal(envelope.ID, &responseID) != nil || responseID != id {
		return upstreamReply{}, errors.New("invalid MCP upstream JSON-RPC response")
	}
	if envelope.Error != nil {
		return upstreamReply{}, fmt.Errorf("MCP upstream error %d", envelope.Error.Code)
	}
	if len(envelope.Result) == 0 {
		return upstreamReply{}, errors.New("MCP upstream response is missing result")
	}
	reply.Result = envelope.Result
	return reply, nil
}

func (c *BrokerClient) notify(ctx context.Context, server serverRecord, method string, params any, sessionID string) (upstreamReply, error) {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return upstreamReply{}, err
	}
	reply, err := c.send(ctx, server, body, sessionID)
	if err != nil {
		return upstreamReply{}, err
	}
	if reply.Status != http.StatusAccepted && reply.Status != http.StatusOK && reply.Status != http.StatusNoContent {
		return upstreamReply{}, fmt.Errorf("MCP upstream notification returned HTTP %d", reply.Status)
	}
	return reply, nil
}

func (c *BrokerClient) send(ctx context.Context, server serverRecord, body []byte, sessionID string) (upstreamReply, error) {
	if len(body) > maxJSONRPCBytes {
		return upstreamReply{}, errors.New("MCP upstream request exceeds body limit")
	}
	injections := []broker.CredentialInjection(nil)
	if server.CredentialID != "" {
		injections = []broker.CredentialInjection{{SecretSlotName: "mcp_server", Location: broker.TargetBearer}}
	}
	headers := map[string]string{"Accept": "application/json, text/event-stream", "Content-Type": "application/json"}
	if sessionID != "" {
		headers[sessionHeader] = sessionID
	}
	stream, err := c.Broker.ExecuteStreamWithHostConstraint(ctx, broker.EgressRequest{
		OrgID: server.OrgID, ConnectionID: server.ID, Method: http.MethodPost, TargetURL: server.Endpoint,
		SafeHeaders: headers, BodyBytes: body, CredentialInjections: injections,
		ActorKind: "SYSTEM", ActorID: "mcp_hub", EntryPoint: "data_plane_guard",
	}, broker.HostEgressConstraint{})
	if err != nil {
		return upstreamReply{}, errors.New("MCP upstream request failed")
	}
	defer stream.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(stream.Body, maxToolBytes+1))
	if err != nil {
		clear(payload)
		return upstreamReply{}, errors.New("MCP upstream response read failed")
	}
	if len(payload) > maxToolBytes {
		clear(payload)
		return upstreamReply{}, errors.New("MCP upstream response exceeds 4 MiB")
	}
	return upstreamReply{Result: payload, SessionID: stream.SanitizedHeaders.Get(sessionHeader),
		Status: stream.StatusCode, Type: stream.SanitizedHeaders.Get("Content-Type")}, nil
}

func lastSSEMessage(raw []byte) ([]byte, error) {
	var messages [][]byte
	var data bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), maxToolBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data.Len() > 0 {
				messages = append(messages, append([]byte(nil), data.Bytes()...))
				data.Reset()
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("invalid MCP SSE response")
	}
	if data.Len() > 0 {
		messages = append(messages, append([]byte(nil), data.Bytes()...))
	}
	if len(messages) == 0 {
		return nil, errors.New("MCP SSE response contains no data event")
	}
	return messages[len(messages)-1], nil
}
