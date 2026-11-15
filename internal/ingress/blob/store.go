// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package blob implements Host-side out-of-band multimodal payload offloading
// and streaming splicing (`blob_ref://`) for Tanpopo Data Plane ingress so
// attachments up to 32 MiB never enter Wasm linear memory (CR-05 / TP-IMP-0016).
package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/security/egress"
)

const (
	// MaxInlineBlobBytes enforces CR-05's 64 KiB boundary above which inline
	// multimodal payloads are offloaded to Host buffers via blob_ref:// handles.
	MaxInlineBlobBytes = 64 * 1024
	// DefaultMaxBlobBytes enforces the bounded 32 MiB per-request multimodal ceiling.
	DefaultMaxBlobBytes int64 = 32 << 20
	// DefaultRemoteFetchTimeout bounds individual policy-enabled remote media fetches.
	DefaultRemoteFetchTimeout = 10 * time.Second
	// DefaultRemoteMediaTotalTimeout bounds cumulative remote media fetches per request.
	DefaultRemoteMediaTotalTimeout = 30 * time.Second
	// MaxRemoteMediaURLsPerRequest caps the number of remote media URLs fetched per request.
	MaxRemoteMediaURLsPerRequest = 16
)

var (
	// ErrRemoteMediaDisabled indicates remote media URLs are disallowed by default.
	ErrRemoteMediaDisabled = errors.New("blob: remote media URLs disabled")
	// ErrSSRFBlockedIP indicates SSRF/egress policy blocked a remote media fetch target.
	ErrSSRFBlockedIP = errors.New("blob: remote media target blocked by SSRF policy")
	// ErrPayloadTooLarge indicates multimodal attachments exceeded bounded RAM limits.
	ErrPayloadTooLarge = errors.New("blob: multimodal payload exceeds maximum byte limit")
	// ErrInvalidPayload indicates malformed multimodal fields or reserved blob_ref injection.
	ErrInvalidPayload = errors.New("blob: invalid multimodal request payload")
)

// Error captures a structured multimodal processing failure with HTTP status & canonical code.
type Error struct {
	HTTPStatus int
	Code       string
	ErrType    string
	Message    string
	Err        error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s (%d): %s", e.Code, e.HTTPStatus, e.Message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// RemoteMediaPolicy controls optional Host-side fetching of http(s) media URLs.
// By default Enabled is false so remote media URLs fail closed with 400 remote_media_url_disabled.
// When Enabled is true, AllowedHosts must be non-empty unless AllowAnyPublicHost is explicitly set,
// and TLS is required by default unless AllowInsecureHTTP is explicitly true.
type RemoteMediaPolicy struct {
	Enabled            bool
	AllowedHosts       []string
	AllowAnyPublicHost bool
	AllowedPorts       []int
	AllowPrivateIP     bool
	AllowedCIDRs       []string
	AllowInsecureHTTP  bool
	MaxBytesPerBlob    int64
	MaxTotalBytes      int64
	MaxURLsPerRequest  int
	RequestTimeout     time.Duration
	TotalFetchTimeout  time.Duration
	Resolver           egress.HostResolver
	ControlHook        func(network, address string, c syscall.RawConn) error
}

// OffloadResult contains lightweight JSON carrying blob_ref:// handles alongside
// Host-owned binary buffers. Callers must defer result.Scrub().
type OffloadResult struct {
	NormalizedJSON []byte
	BlobRefs       []port.BlobRef
	HostBlobs      map[string][]byte
	TotalBlobBytes int64
}

// Scrub immediately zeroizes all Host blob buffers and the normalized JSON copy.
func (r *OffloadResult) Scrub() {
	if r == nil {
		return
	}
	ScrubHostBlobs(r.HostBlobs)
	clear(r.NormalizedJSON)
}

// ScrubHostBlobs zeroizes and removes all byte slices stored in hostBlobs.
func ScrubHostBlobs(hostBlobs map[string][]byte) {
	for k, v := range hostBlobs {
		clear(v)
		delete(hostBlobs, k)
	}
}

const maxStrictJSONDepth = 64

// DecodeStrictJSONObject decodes raw as a single JSON object while rejecting
// trailing tokens, excessive nesting (>64), and duplicate or case-colliding object keys
// so Go struct parsers and case-sensitive policy/offload inspectors always agree.
func DecodeStrictJSONObject(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("decode strict JSON root: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("expected JSON object root")
	}
	obj, err := parseStrictJSONObject(dec, 1)
	if err != nil {
		return nil, err
	}
	if _, extraErr := dec.Token(); extraErr != io.EOF {
		return nil, fmt.Errorf("unexpected trailing JSON content")
	}
	return obj, nil
}

func parseStrictJSONObject(dec *json.Decoder, depth int) (map[string]any, error) {
	if depth > maxStrictJSONDepth {
		return nil, fmt.Errorf("JSON nesting exceeds maximum depth %d", maxStrictJSONDepth)
	}
	out := make(map[string]any)
	seenFold := make(map[string]struct{})
	for dec.More() {
		ktok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := ktok.(string)
		if !ok {
			return nil, fmt.Errorf("expected JSON object key string")
		}
		folded := strings.ToLower(key)
		if _, dup := seenFold[folded]; dup {
			return nil, fmt.Errorf("duplicate or case-colliding JSON key %q", key)
		}
		seenFold[folded] = struct{}{}
		val, err := parseStrictJSONValue(dec, depth)
		if err != nil {
			return nil, err
		}
		out[key] = val
	}
	endTok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if endDelim, ok := endTok.(json.Delim); !ok || endDelim != '}' {
		return nil, fmt.Errorf("expected closing brace for JSON object")
	}
	return out, nil
}

func parseStrictJSONArray(dec *json.Decoder, depth int) ([]any, error) {
	if depth > maxStrictJSONDepth {
		return nil, fmt.Errorf("JSON nesting exceeds maximum depth %d", maxStrictJSONDepth)
	}
	out := make([]any, 0)
	for dec.More() {
		val, err := parseStrictJSONValue(dec, depth)
		if err != nil {
			return nil, err
		}
		out = append(out, val)
	}
	endTok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if endDelim, ok := endTok.(json.Delim); !ok || endDelim != ']' {
		return nil, fmt.Errorf("expected closing bracket for JSON array")
	}
	return out, nil
}

func parseStrictJSONValue(dec *json.Decoder, depth int) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); ok {
		switch delim {
		case '{':
			return parseStrictJSONObject(dec, depth+1)
		case '[':
			return parseStrictJSONArray(dec, depth+1)
		default:
			return nil, fmt.Errorf("unexpected JSON delimiter %q", delim)
		}
	}
	return tok, nil
}

// OffloadMultimodalRequest normalizes upstreamModel/stream fields, extracts inline payloads > 64 KiB
// into Host-resident blob_ref:// handles, rejects reserved client-injected blob_ref:// handles,
// and blocks remote media URLs fail-closed unless explicitly policy-enabled via SSRFGuardDialer.
func OffloadMultimodalRequest(
	ctx context.Context,
	reqID string,
	rawBody []byte,
	upstreamModel string,
	setStreamTrue bool,
	maxTotalBytes int64,
	remotePolicy RemoteMediaPolicy,
) (*OffloadResult, error) {
	return OffloadEndpointMultimodalRequest(
		ctx,
		reqID,
		rawBody,
		upstreamModel,
		setStreamTrue,
		maxTotalBytes,
		remotePolicy,
		"",
	)
}

// OffloadEndpointMultimodalRequest behaves like OffloadMultimodalRequest while restricting
// multimodal container traversal to endpointRootField ("messages", "input", "none", or "" for both).
func OffloadEndpointMultimodalRequest(
	ctx context.Context,
	reqID string,
	rawBody []byte,
	upstreamModel string,
	setStreamTrue bool,
	maxTotalBytes int64,
	remotePolicy RemoteMediaPolicy,
	endpointRootField string,
) (*OffloadResult, error) {
	if maxTotalBytes <= 0 {
		maxTotalBytes = DefaultMaxBlobBytes
	}
	obj, err := DecodeStrictJSONObject(rawBody)
	if err != nil {
		return nil, &Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       "invalid_request_error",
			ErrType:    "invalid_request_error",
			Message:    "Expected strict JSON object request body without duplicate or case-colliding keys.",
			Err:        ErrInvalidPayload,
		}
	}
	if strings.TrimSpace(upstreamModel) != "" {
		obj["model"] = strings.TrimSpace(upstreamModel)
	}
	if setStreamTrue {
		obj["stream"] = true
	}

	var nonceBytes [16]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		return nil, err
	}
	blobPrefix := fmt.Sprintf("blob_ref://%s-%s/", reqID, hex.EncodeToString(nonceBytes[:]))
	if bytes.Contains(rawBody, []byte(blobPrefix)) {
		return nil, &Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       "invalid_request_error",
			ErrType:    "invalid_request_error",
			Message:    "Request payload contains reserved blob_ref handle prefix.",
			Err:        ErrInvalidPayload,
		}
	}

	res := &OffloadResult{
		HostBlobs: make(map[string][]byte),
	}
	nextIdx := 0

	addBlob := func(payload []byte, mimeType string) (string, error) {
		nextTotal := res.TotalBlobBytes + int64(len(payload))
		if nextTotal > maxTotalBytes {
			return "", &Error{
				HTTPStatus: http.StatusRequestEntityTooLarge,
				Code:       "payload_too_large",
				ErrType:    "invalid_request_error",
				Message:    fmt.Sprintf("Multimodal blob payload exceeds %d byte limit.", maxTotalBytes),
				Err:        ErrPayloadTooLarge,
			}
		}
		res.TotalBlobBytes = nextTotal
		sum := sha256.Sum256(payload)
		uri := fmt.Sprintf("%s%d", blobPrefix, nextIdx)
		blobID := fmt.Sprintf("blob_%d", nextIdx)
		nextIdx++
		res.BlobRefs = append(res.BlobRefs, port.BlobRef{
			BlobID:     blobID,
			URI:        uri,
			MimeType:   mimeType,
			ByteLength: int64(len(payload)),
			SHA256Hex:  hex.EncodeToString(sum[:]),
		})
		res.HostBlobs[uri] = payload
		return uri, nil
	}

	maxURLs := remotePolicy.MaxURLsPerRequest
	if maxURLs <= 0 {
		maxURLs = MaxRemoteMediaURLsPerRequest
	}
	totalTimeout := remotePolicy.TotalFetchTimeout
	if totalTimeout <= 0 {
		totalTimeout = DefaultRemoteMediaTotalTimeout
	}
	remoteFetchCount := 0
	var remoteTotalBytes int64
	var (
		fetchCtx    context.Context
		cancelFetch context.CancelFunc
	)
	defer func() {
		if cancelFetch != nil {
			cancelFetch()
		}
	}()

	processValue := func(val string, fallbackMime string, encodeRemoteAsDataURI bool) (string, bool, error) {
		trimmed := strings.TrimSpace(val)
		if strings.HasPrefix(strings.ToLower(trimmed), "blob_ref://") {
			return "", false, &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "invalid_request_error",
				ErrType:    "invalid_request_error",
				Message:    "Client-supplied blob_ref:// handles are prohibited.",
				Err:        ErrInvalidPayload,
			}
		}
		if isHTTPURL(trimmed) {
			if !remotePolicy.Enabled {
				return "", false, &Error{
					HTTPStatus: http.StatusBadRequest,
					Code:       "remote_media_url_disabled",
					ErrType:    "invalid_request_error",
					Message:    "Fetching remote media URLs is disabled by gateway policy.",
					Err:        ErrRemoteMediaDisabled,
				}
			}
			remoteFetchCount++
			if remoteFetchCount > maxURLs {
				return "", false, &Error{
					HTTPStatus: http.StatusBadRequest,
					Code:       "remote_media_url_limit_exceeded",
					ErrType:    "invalid_request_error",
					Message:    fmt.Sprintf("Request exceeds maximum of %d remote media URLs.", maxURLs),
					Err:        ErrInvalidPayload,
				}
			}
			remBudget := maxTotalBytes - res.TotalBlobBytes
			if remotePolicy.MaxTotalBytes > 0 {
				remRemote := remotePolicy.MaxTotalBytes - remoteTotalBytes
				if remRemote < remBudget {
					remBudget = remRemote
				}
			}
			if remBudget <= 0 {
				return "", false, &Error{
					HTTPStatus: http.StatusRequestEntityTooLarge,
					Code:       "payload_too_large",
					ErrType:    "invalid_request_error",
					Message:    "Remote media payload exceeds request budget.",
					Err:        ErrPayloadTooLarge,
				}
			}
			if fetchCtx == nil {
				fetchCtx, cancelFetch = context.WithTimeout(ctx, totalTimeout)
			}
			fetchedBytes, mimeType, ferr := fetchBoundedRemoteMedia(fetchCtx, trimmed, remBudget, remotePolicy, fallbackMime)
			if ferr != nil {
				return "", false, ferr
			}
			if remotePolicy.MaxTotalBytes > 0 && remoteTotalBytes+int64(len(fetchedBytes)) > remotePolicy.MaxTotalBytes {
				clear(fetchedBytes)
				return "", false, &Error{
					HTTPStatus: http.StatusRequestEntityTooLarge,
					Code:       "payload_too_large",
					ErrType:    "invalid_request_error",
					Message:    fmt.Sprintf("Remote media total bytes exceed %d limit.", remotePolicy.MaxTotalBytes),
					Err:        ErrPayloadTooLarge,
				}
			}
			remoteTotalBytes += int64(len(fetchedBytes))
			var stored []byte
			if encodeRemoteAsDataURI {
				encoded := make([]byte, len("data:")+len(mimeType)+len(";base64,")+base64.StdEncoding.EncodedLen(len(fetchedBytes)))
				off := copy(encoded, "data:")
				off += copy(encoded[off:], mimeType)
				off += copy(encoded[off:], ";base64,")
				base64.StdEncoding.Encode(encoded[off:], fetchedBytes)
				clear(fetchedBytes)
				stored = encoded
			} else {
				stored = fetchedBytes
			}
			uri, aerr := addBlob(stored, mimeType)
			if aerr != nil {
				clear(stored)
				return "", false, aerr
			}
			return uri, true, nil
		}

		if len(val) <= MaxInlineBlobBytes {
			return val, false, nil
		}
		mimeType := parseDataURIMimeType(val, fallbackMime)
		rawBytes := []byte(val)
		uri, aerr := addBlob(rawBytes, mimeType)
		if aerr != nil {
			clear(rawBytes)
			return "", false, aerr
		}
		return uri, true, nil
	}

	offloadField := func(m map[string]any, key, fallbackMime string, encodeRemoteAsDataURI bool) error {
		if m == nil {
			return nil
		}
		s, ok := m[key].(string)
		if !ok {
			return nil
		}
		next, changed, err := processValue(s, fallbackMime, encodeRemoteAsDataURI)
		if err != nil {
			return err
		}
		if changed {
			m[key] = next
		}
		return nil
	}

	handlePartMap := func(partMap map[string]any) error {
		if partMap == nil {
			return nil
		}
		ptype := ""
		if rawType, ok := partMap["type"].(string); ok {
			ptype = strings.ToLower(strings.TrimSpace(rawType))
		}

		for _, imgKey := range []string{"image_url", "image"} {
			switch imgVal := partMap[imgKey].(type) {
			case string:
				next, changed, err := processValue(imgVal, "application/octet-stream", true)
				if err != nil {
					return err
				}
				if changed {
					partMap[imgKey] = next
				}
			case map[string]any:
				if err := offloadField(imgVal, "url", "application/octet-stream", true); err != nil {
					return err
				}
				if err := offloadField(imgVal, "data", "application/octet-stream", false); err != nil {
					return err
				}
				if srcMap, ok := imgVal["source"].(map[string]any); ok {
					if err := offloadField(srcMap, "url", "application/octet-stream", true); err != nil {
						return err
					}
					if err := offloadField(srcMap, "data", "application/octet-stream", false); err != nil {
						return err
					}
				}
			}
		}
		if ptype == "input_image" || ptype == "image_url" || ptype == "image" {
			if err := offloadField(partMap, "url", "application/octet-stream", true); err != nil {
				return err
			}
			if err := offloadField(partMap, "data", "application/octet-stream", false); err != nil {
				return err
			}
			if srcMap, ok := partMap["source"].(map[string]any); ok {
				if err := offloadField(srcMap, "url", "application/octet-stream", true); err != nil {
					return err
				}
				if err := offloadField(srcMap, "data", "application/octet-stream", false); err != nil {
					return err
				}
			}
		}

		for _, audioKey := range []string{"input_audio", "audio"} {
			switch audioVal := partMap[audioKey].(type) {
			case string:
				next, changed, err := processValue(audioVal, "audio/wav", false)
				if err != nil {
					return err
				}
				if changed {
					partMap[audioKey] = next
				}
			case map[string]any:
				if err := offloadField(audioVal, "url", "audio/wav", true); err != nil {
					return err
				}
				if err := offloadField(audioVal, "data", "audio/wav", false); err != nil {
					return err
				}
			}
		}
		if ptype == "input_audio" || ptype == "audio" {
			if err := offloadField(partMap, "url", "audio/wav", true); err != nil {
				return err
			}
			if err := offloadField(partMap, "data", "audio/wav", false); err != nil {
				return err
			}
		}

		for _, docKey := range []string{"file", "input_file", "document"} {
			if docMap, ok := partMap[docKey].(map[string]any); ok {
				if err := offloadField(docMap, "file_url", "application/pdf", true); err != nil {
					return err
				}
				if err := offloadField(docMap, "url", "application/pdf", true); err != nil {
					return err
				}
				if err := offloadField(docMap, "file_data", "application/octet-stream", false); err != nil {
					return err
				}
				if err := offloadField(docMap, "data", "application/pdf", false); err != nil {
					return err
				}
				if srcMap, ok := docMap["source"].(map[string]any); ok {
					if err := offloadField(srcMap, "url", "application/pdf", true); err != nil {
						return err
					}
					if err := offloadField(srcMap, "data", "application/pdf", false); err != nil {
						return err
					}
				}
			}
		}
		if ptype == "file" || ptype == "input_file" || ptype == "document" {
			if err := offloadField(partMap, "file_url", "application/pdf", true); err != nil {
				return err
			}
			if err := offloadField(partMap, "url", "application/pdf", true); err != nil {
				return err
			}
			if err := offloadField(partMap, "file_data", "application/octet-stream", false); err != nil {
				return err
			}
			if err := offloadField(partMap, "data", "application/pdf", false); err != nil {
				return err
			}
			if srcMap, ok := partMap["source"].(map[string]any); ok {
				if err := offloadField(srcMap, "url", "application/pdf", true); err != nil {
					return err
				}
				if err := offloadField(srcMap, "data", "application/pdf", false); err != nil {
					return err
				}
			}
		}
		return nil
	}

	walkMessageOrItemList := func(items []any) error {
		for _, item := range items {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if err := handlePartMap(itemMap); err != nil {
				return err
			}
			if parts, ok := itemMap["content"].([]any); ok {
				for _, p := range parts {
					partMap, ok := p.(map[string]any)
					if !ok {
						continue
					}
					if err := handlePartMap(partMap); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}

	walkMessages := endpointRootField == "" || endpointRootField == "messages"
	walkInput := endpointRootField == "" || endpointRootField == "input"
	if walkMessages {
		if msgs, ok := obj["messages"].([]any); ok {
			if err := walkMessageOrItemList(msgs); err != nil {
				res.Scrub()
				return nil, err
			}
		}
	}
	if walkInput {
		if inputs, ok := obj["input"].([]any); ok {
			if err := walkMessageOrItemList(inputs); err != nil {
				res.Scrub()
				return nil, err
			}
		}
	}

	encoded, err := json.Marshal(obj)
	if err != nil {
		res.Scrub()
		return nil, err
	}
	res.NormalizedJSON = encoded
	if len(res.HostBlobs) == 0 {
		res.HostBlobs = nil
	}
	return res, nil
}

// SpliceHostBlobs replaces Host-managed blob_ref:// placeholders (and #raw_base64 variants)
// inside adapter-transformed outbound payloads directly before network egress while enforcing
// total byte bounds.
func SpliceHostBlobs(body []byte, hostBlobs map[string][]byte, maxBytes int64) ([]byte, error) {
	if len(hostBlobs) == 0 {
		return append([]byte(nil), body...), nil
	}
	out := append([]byte(nil), body...)
	for uri, rawVal := range hostBlobs {
		quotedPlaceholder, err := json.Marshal(uri)
		if err != nil {
			clear(out)
			return nil, err
		}
		quotedRawB64Placeholder, err := json.Marshal(uri + "#raw_base64")
		if err != nil {
			clear(out)
			return nil, err
		}
		verbatimCount := bytes.Count(out, quotedPlaceholder)
		rawB64Count := bytes.Count(out, quotedRawB64Placeholder)
		totalCount := verbatimCount + rawB64Count
		if totalCount == 0 {
			continue
		}
		if totalCount != 1 {
			clear(out)
			return nil, fmt.Errorf("%w: duplicate or unexpected blob reference %q", ErrInvalidPayload, uri)
		}

		activePlaceholder := quotedPlaceholder
		var quotedPayload []byte
		if rawB64Count == 1 {
			activePlaceholder = quotedRawB64Placeholder
			rawB64Str := extractOrEncodeBase64Payload(rawVal)
			quotedPayload, err = json.Marshal(rawB64Str)
			rawB64Str = ""
		} else {
			quotedPayload, err = json.Marshal(string(rawVal))
		}
		if err != nil {
			clear(out)
			return nil, err
		}
		projected := int64(len(out)) + (int64(len(quotedPayload)) - int64(len(activePlaceholder)))
		if maxBytes > 0 && projected > maxBytes+4096 {
			clear(quotedPayload)
			clear(out)
			return nil, fmt.Errorf("%w: spliced payload exceeds bounded request limit", ErrPayloadTooLarge)
		}
		next := bytes.Replace(out, activePlaceholder, quotedPayload, 1)
		clear(quotedPayload)
		clear(out)
		out = next
	}
	return out, nil
}

func extractOrEncodeBase64Payload(rawVal []byte) string {
	if bytes.HasPrefix(rawVal, []byte("data:")) {
		if idx := bytes.Index(rawVal, []byte(";base64,")); idx >= 0 {
			return string(rawVal[idx+len(";base64,"):])
		}
		if comma := bytes.IndexByte(rawVal, ','); comma >= 0 {
			return string(rawVal[comma+1:])
		}
	}
	if len(rawVal) > 0 && isPrintableBase64(rawVal) {
		return string(rawVal)
	}
	return base64.StdEncoding.EncodeToString(rawVal)
}

func isPrintableBase64(b []byte) bool {
	for _, c := range b {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '+' || c == '/' || c == '=' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func isHTTPURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func parseDataURIMimeType(val, fallback string) string {
	if !strings.HasPrefix(val, "data:") {
		return fallback
	}
	rest := val[len("data:"):]
	if idx := strings.IndexByte(rest, ','); idx >= 0 {
		meta := rest[:idx]
		if semi := strings.IndexByte(meta, ';'); semi >= 0 {
			meta = meta[:semi]
		}
		if strings.TrimSpace(meta) != "" {
			return strings.TrimSpace(meta)
		}
	}
	return fallback
}

func fetchBoundedRemoteMedia(
	ctx context.Context,
	rawURL string,
	remainingBudget int64,
	policy RemoteMediaPolicy,
	fallbackMime string,
) ([]byte, string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" || u.User != nil {
		return nil, "", &Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       "invalid_request_error",
			ErrType:    "invalid_request_error",
			Message:    "Malformed remote media URL.",
			Err:        ErrInvalidPayload,
		}
	}

	allowedHosts := append([]string(nil), policy.AllowedHosts...)
	if len(allowedHosts) == 0 {
		if !policy.AllowAnyPublicHost {
			return nil, "", &Error{
				HTTPStatus: http.StatusForbidden,
				Code:       "ssrf_blocked_ip",
				ErrType:    "permission_error",
				Message:    "Remote media URL blocked by SSRF egress policy (no AllowedHosts configured).",
				Err:        errors.Join(ErrSSRFBlockedIP, egress.ErrHostDenied),
			}
		}
		allowedHosts = []string{u.Hostname()}
	}
	allowedPorts := append([]int(nil), policy.AllowedPorts...)

	timeout := policy.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRemoteFetchTimeout
	}
	ep := egress.Policy{
		AllowedHosts:   allowedHosts,
		AllowedPorts:   allowedPorts,
		AllowPrivateIP: policy.AllowPrivateIP,
		AllowedCIDRs:   policy.AllowedCIDRs,
		RequireTLS:     !policy.AllowInsecureHTTP,
	}
	client, cerr := egress.NewGuardedHTTPClient(egress.ClientConfig{
		Policy:                   ep,
		Timeout:                  timeout,
		MaxRedirects:             -1, // forbid redirects on remote media fetches
		DenyCrossOriginRedirects: true,
		Resolver:                 policy.Resolver,
		ControlHook:              policy.ControlHook,
	})
	if cerr != nil {
		return nil, "", mapEgressErrToMediaError(cerr)
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", &Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       "invalid_request_error",
			ErrType:    "invalid_request_error",
			Message:    "Invalid remote media URL.",
			Err:        ErrInvalidPayload,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", mapEgressErrToMediaError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", &Error{
			HTTPStatus: http.StatusBadGateway,
			Code:       "remote_media_fetch_failed",
			ErrType:    "upstream_error",
			Message:    fmt.Sprintf("Remote media fetch returned HTTP %d.", resp.StatusCode),
			Err:        errors.New("remote media HTTP error"),
		}
	}

	capBytes := remainingBudget
	if capBytes <= 0 {
		return nil, "", &Error{
			HTTPStatus: http.StatusRequestEntityTooLarge,
			Code:       "payload_too_large",
			ErrType:    "invalid_request_error",
			Message:    "Remote media payload exceeds request budget.",
			Err:        ErrPayloadTooLarge,
		}
	}
	if policy.MaxBytesPerBlob > 0 && policy.MaxBytesPerBlob < capBytes {
		capBytes = policy.MaxBytesPerBlob
	}

	body, rerr := io.ReadAll(io.LimitReader(resp.Body, capBytes+1))
	if rerr != nil {
		clear(body)
		return nil, "", &Error{
			HTTPStatus: http.StatusBadGateway,
			Code:       "remote_media_fetch_failed",
			ErrType:    "upstream_error",
			Message:    "Failed reading remote media response body.",
			Err:        rerr,
		}
	}
	if int64(len(body)) > capBytes {
		clear(body)
		return nil, "", &Error{
			HTTPStatus: http.StatusRequestEntityTooLarge,
			Code:       "payload_too_large",
			ErrType:    "invalid_request_error",
			Message:    fmt.Sprintf("Remote media attachment exceeds %d byte cap.", capBytes),
			Err:        ErrPayloadTooLarge,
		}
	}

	ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if ct != "" {
		parsed, _, perr := mime.ParseMediaType(ct)
		if perr != nil || parsed == "" {
			clear(body)
			return nil, "", &Error{
				HTTPStatus: http.StatusBadRequest,
				Code:       "invalid_request_error",
				ErrType:    "invalid_request_error",
				Message:    fmt.Sprintf("Invalid remote media Content-Type %q.", ct),
				Err:        ErrInvalidPayload,
			}
		}
		ct = parsed
	}
	if ct == "" && len(body) > 0 {
		ct = http.DetectContentType(body)
	}
	if !isAllowedRemoteMediaMIME(ct) {
		clear(body)
		return nil, "", &Error{
			HTTPStatus: http.StatusBadRequest,
			Code:       "invalid_request_error",
			ErrType:    "invalid_request_error",
			Message:    fmt.Sprintf("Unsupported remote media Content-Type %q.", ct),
			Err:        ErrInvalidPayload,
		}
	}
	if ct == "" {
		ct = fallbackMime
	}
	return body, ct, nil
}

func isAllowedRemoteMediaMIME(ct string) bool {
	if ct == "" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(ct))
	if lower == "image/svg+xml" {
		return false
	}
	return strings.HasPrefix(lower, "image/") ||
		strings.HasPrefix(lower, "audio/") ||
		lower == "application/pdf" ||
		lower == "application/octet-stream"
}

func mapEgressErrToMediaError(err error) error {
	var eErr *egress.Error
	if errors.As(err, &eErr) ||
		errors.Is(err, egress.ErrMetadataBlocked) ||
		errors.Is(err, egress.ErrPrivateIPBlocked) ||
		errors.Is(err, egress.ErrHostDenied) ||
		errors.Is(err, egress.ErrTLSRequired) ||
		errors.Is(err, egress.ErrRedirectBlocked) {
		return &Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "ssrf_blocked_ip",
			ErrType:    "permission_error",
			Message:    "Remote media URL blocked by SSRF egress policy.",
			Err:        errors.Join(ErrSSRFBlockedIP, err),
		}
	}
	return &Error{
		HTTPStatus: http.StatusBadGateway,
		Code:       "remote_media_fetch_failed",
		ErrType:    "upstream_error",
		Message:    "Failed to fetch remote media attachment.",
		Err:        err,
	}
}
