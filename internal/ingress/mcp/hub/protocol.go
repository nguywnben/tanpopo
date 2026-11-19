// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package hub

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	protocolVersion = "2025-03-26"
	maxJSONRPCBytes = 1 << 20
	maxToolBytes    = 4 << 20
)

var errInvalidJSONRPC = errors.New("invalid JSON-RPC request")

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func parseRequest(raw []byte) (request, error) {
	var req request
	if len(raw) == 0 || len(raw) > maxJSONRPCBytes {
		return req, fmt.Errorf("%w: body size outside allowed range", errInvalidJSONRPC)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, fmt.Errorf("%w: %v", errInvalidJSONRPC, err)
	}
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" || !validID(req.ID) {
		return req, errInvalidJSONRPC
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return req, fmt.Errorf("%w: trailing JSON value", errInvalidJSONRPC)
	}
	return req, nil
}

func validID(id json.RawMessage) bool {
	if len(id) == 0 {
		return true // notification
	}
	trimmed := strings.TrimSpace(string(id))
	if trimmed == "null" || trimmed == "" || trimmed == "true" || trimmed == "false" || trimmed[0] == '{' || trimmed[0] == '[' {
		return false
	}
	var s string
	if json.Unmarshal(id, &s) == nil {
		return true
	}
	_, err := strconv.ParseFloat(trimmed, 64)
	return err == nil
}

func rpcResponse(id json.RawMessage, result any) response {
	return response{JSONRPC: "2.0", ID: cloneRaw(id), Result: result}
}

func rpcFailure(id json.RawMessage, code int, message string, data any) response {
	return response{JSONRPC: "2.0", ID: cloneRaw(id), Error: &rpcError{Code: code, Message: message, Data: data}}
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return append(json.RawMessage(nil), raw...)
}

func toolName(serverSlug, rawName string) (string, error) {
	if serverSlug == "" || rawName == "" || strings.Contains(serverSlug, "__") || strings.ContainsAny(serverSlug, "_/ \t\r\n") {
		return "", fmt.Errorf("invalid MCP tool namespace")
	}
	return serverSlug + "__" + rawName, nil
}

func splitToolName(value string) (string, string, bool) {
	i := strings.Index(value, "__")
	if i <= 0 || i+2 >= len(value) || strings.Contains(value[:i], "_") {
		return "", "", false
	}
	return value[:i], value[i+2:], true
}

func encodeCursor(key []byte, scope, filter string, offset int) string {
	payload, _ := json.Marshal(struct {
		Scope  string `json:"s"`
		Filter string `json:"f"`
		Offset int    `json:"o"`
	}{scope, filter, offset})
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func decodeCursor(key []byte, token, scope, filter string) (int, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return 0, errors.New("invalid cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, errors.New("invalid cursor")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, errors.New("invalid cursor")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return 0, errors.New("invalid cursor")
	}
	var value struct {
		Scope  string `json:"s"`
		Filter string `json:"f"`
		Offset int    `json:"o"`
	}
	if err := json.Unmarshal(payload, &value); err != nil || value.Scope != scope || value.Filter != filter || value.Offset < 0 {
		return 0, errors.New("invalid cursor")
	}
	return value.Offset, nil
}
