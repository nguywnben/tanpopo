// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

var (
	errMissingIfMatch = errors.New("admin: missing If-Match header")
	errInvalidIfMatch = errors.New("admin: invalid If-Match header")
)

// FormatETag formats a positive revision version into a canonical strong ETag ("v<n>").
func FormatETag(version int64) string {
	return fmt.Sprintf(`"v%d"`, version)
}

func parseIfMatchHeader(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return 0, errMissingIfMatch
	}
	if raw == "*" || strings.HasPrefix(raw, "W/") || strings.HasPrefix(raw, "w/") {
		return 0, errInvalidIfMatch
	}
	if !strings.HasPrefix(raw, `"v`) || !strings.HasSuffix(raw, `"`) || len(raw) < 4 {
		return 0, errInvalidIfMatch
	}
	numStr := raw[2 : len(raw)-1]
	for i := 0; i < len(numStr); i++ {
		if numStr[i] < '0' || numStr[i] > '9' {
			return 0, errInvalidIfMatch
		}
	}
	ver, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil || ver < 1 {
		return 0, errInvalidIfMatch
	}
	return ver, nil
}

func requireIfMatch(w http.ResponseWriter, r *http.Request) (int64, bool) {
	ver, err := parseIfMatchHeader(r)
	if err == nil {
		return ver, true
	}
	if errors.Is(err, errMissingIfMatch) {
		writeErrorEnvelope(w, r, http.StatusPreconditionRequired, "precondition_required", "invalid_request_error", "If-Match header is required for state-mutating requests", false, nil)
		return 0, false
	}
	writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_if_match", "invalid_request_error", `If-Match header must carry an exact strong ETag such as "v1"`, false, nil)
	return 0, false
}

func writeOptimisticConflict(w http.ResponseWriter, r *http.Request, currentVersion int64, currentResource any) {
	w.Header().Set("ETag", FormatETag(currentVersion))
	writeErrorEnvelope(
		w,
		r,
		http.StatusConflict,
		"optimistic_lock_conflict",
		"conflict_error",
		fmt.Sprintf("resource version conflict: current revision is v%d", currentVersion),
		true,
		currentResource,
	)
}
