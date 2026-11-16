// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

type cursorPayload struct {
	Version    int    `json:"v"`
	Resource   string `json:"res"`
	OrgID      string `json:"org"`
	FilterHash string `json:"fh"`
	CreatedMs  int64  `json:"ts"`
	LastID     string `json:"id"`
}

func parsePageParams(w http.ResponseWriter, r *http.Request, secret []byte, resource, orgID, filterHash string) (int, *cursorPayload, bool) {
	limit := defaultPageLimit
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > maxPageLimit {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_pagination_limit", "invalid_request_error", fmt.Sprintf("limit must be between 1 and %d", maxPageLimit), false, nil)
			return 0, nil, false
		}
		limit = parsed
	}

	rawCursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if rawCursor == "" {
		return limit, nil, true
	}

	cur, err := verifyPaginationCursor(secret, rawCursor, resource, orgID, filterHash)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_pagination_cursor", "invalid_request_error", "invalid, expired, or cross-tenant pagination cursor", false, nil)
		return 0, nil, false
	}
	return limit, cur, true
}

func encodePaginationCursor(secret []byte, resource, orgID, filterHash string, createdAtMs int64, lastID string) (string, error) {
	p := cursorPayload{
		Version:    1,
		Resource:   resource,
		OrgID:      orgID,
		FilterHash: filterHash,
		CreatedMs:  createdAtMs,
		LastID:     lastID,
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(raw)
	mac := computeCursorMAC(secret, encodedPayload)
	return encodedPayload + "." + base64.RawURLEncoding.EncodeToString(mac), nil
}

func verifyPaginationCursor(secret []byte, token, expectedResource, expectedOrgID, expectedFilterHash string) (*cursorPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("malformed cursor")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(sig) != sha256.Size {
		return nil, fmt.Errorf("invalid cursor signature encoding")
	}
	expectedMAC := computeCursorMAC(secret, parts[0])
	if !hmac.Equal(sig, expectedMAC) {
		return nil, fmt.Errorf("cursor HMAC mismatch")
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid cursor payload encoding")
	}
	var p cursorPayload
	if err := json.Unmarshal(rawPayload, &p); err != nil {
		return nil, fmt.Errorf("invalid cursor json")
	}
	if p.Version != 1 || p.Resource != expectedResource || p.OrgID != expectedOrgID || p.FilterHash != expectedFilterHash || strings.TrimSpace(p.LastID) == "" {
		return nil, fmt.Errorf("cursor scope or filter mismatch")
	}
	return &p, nil
}

func computeCursorMAC(secret []byte, encodedPayload string) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("tanpopo:admin:cursor:v1:"))
	_, _ = mac.Write([]byte(encodedPayload))
	return mac.Sum(nil)
}

type paginatedListEnvelope[T any] struct {
	Data       []T     `json:"data"`
	NextCursor *string `json:"next_cursor,omitempty"`
	HasMore    bool    `json:"has_more"`
}
