// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package privacy implements Tanpopo's Metadata-Only privacy redactor and
// secret-safe flat single-line snake_case structured JSON logger (SPEC-CORE-02 /
// TP-FR-0805 / TP-FR-0806 / OPS-AC-10 / TP-IMP-0022).
//
// Invariants:
//   - Prompt bodies, completions, and tool arguments (messages, input, output,
//     arguments, prompt, completion, content, choices, etc.) are elided whenever
//     CapturePayloads == false (the strict default).
//   - Authorization headers, provider keys (x-api-key, x-goog-api-key, api_key),
//     Virtual Keys (X-Tanpopo-Virtual-Key, vk_tp_*), sk-* tokens, Bearer tokens,
//     cookies, and URL userinfo credentials are ALWAYS redacted with "[REDACTED]".
//   - Every JSON log entry is emitted as a flat single-line snake_case JSON object
//     with mandatory request_id and entry_point correlation fields.
package privacy

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	// RedactedMarker replaces any sensitive credential or secret token in logs, spans, and headers.
	RedactedMarker = "[REDACTED]"
	// ElidedMarker replaces payload fields when metadata-only default posture is active.
	ElidedMarker = "[ELIDED_PAYLOAD]"
	// maxRecursionDepth bounds recursive sanitization against deeply nested adversarial inputs.
	maxRecursionDepth = 32
	// maxStringBytes bounds emitted log/span attribute strings cleanly.
	maxStringBytes = 16 * 1024
)

var (
	bearerPattern    = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	basicPattern     = regexp.MustCompile(`(?i)\bBasic\s+[A-Za-z0-9+/=]+`)
	vkeyPattern      = regexp.MustCompile(`\bvk_tp_[A-Za-z0-9_-]+`)
	skPattern        = regexp.MustCompile(`\bsk-[A-Za-z0-9._-]{4,}`)
	googleKeyPattern = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{16,}`)
	ghTokenPattern   = regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{16,}`)
	urlUserInfoRe    = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^:/@\s]+):([^@/\s]+)@`)
	cookieAssignRe   = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9._-])([A-Za-z0-9._-]*(?:authorization|api[_-]?key|token|secret|password|session|cookie))=([^\s;&,"']+)`)
	jsonSecretKVRe   = regexp.MustCompile(`(?i)"([^"]*(?:authorization|api[_-]?key|apikey|secret|password|passwd|credential|token|cookie)[^"]*)"\s*:\s*"([^"]*)"`)
)

// sensitiveKeys contains canonical normalized lowercase underscore keys whose values
// must ALWAYS be replaced with "[REDACTED]".
var sensitiveKeys = map[string]bool{
	"authorization":         true,
	"proxy_authorization":   true,
	"x_api_key":             true,
	"x_goog_api_key":        true,
	"x_tanpopo_virtual_key": true,
	"cookie":                true,
	"set_cookie":            true,
	"api_key":               true,
	"apikey":                true,
	"secret":                true,
	"client_secret":         true,
	"access_token":          true,
	"refresh_token":         true,
	"id_token":              true,
	"private_key":           true,
	"password":              true,
	"passwd":                true,
	"credential":            true,
	"credentials":           true,
	"bearer_token":          true,
	"x_auth_token":          true,
	"openai_api_key":        true,
	"anthropic_api_key":     true,
	"tanpopo_master_key":    true,
	"token":                 true,
}

// payloadKeys contains keys carrying prompt, completion, or tool-argument content
// that must be elided whenever CapturePayloads == false.
var payloadKeys = map[string]bool{
	"messages":      true,
	"input":         true,
	"arguments":     true,
	"output":        true,
	"prompt":        true,
	"prompts":       true,
	"completion":    true,
	"completions":   true,
	"choices":       true,
	"candidates":    true,
	"content":       true,
	"text":          true,
	"delta":         true,
	"raw_body":      true,
	"raw_response":  true,
	"request_body":  true,
	"response_body": true,
	"tool_calls":    true,
	"function_call": true,
	"system":        true,
	"instructions":  true,
	"embedding":     true,
	"embeddings":    true,
}

// Redactor scrubs credentials unconditionally and elides prompt/completion payloads by default.
type Redactor struct {
	CapturePayloads bool
}

// NewDefaultRedactor constructs the strict Metadata-Only Redactor (CapturePayloads = false).
func NewDefaultRedactor() *Redactor {
	return &Redactor{CapturePayloads: false}
}

func normalizeKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	prevUnderscore := false
	for i, r := range trimmed {
		if r == '-' || r == '_' || r == '.' || r == ' ' {
			if !prevUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				prevUnderscore = true
			}
			continue
		}
		if unicode.IsUpper(r) && i > 0 && !prevUnderscore {
			prev := rune(trimmed[i-1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
		prevUnderscore = false
	}
	return strings.Trim(b.String(), "_")
}

// NormalizeSnakeCase converts an arbitrary field name into canonical snake_case.
func NormalizeSnakeCase(key string) string {
	norm := normalizeKey(key)
	if norm == "" {
		return "field"
	}
	var b strings.Builder
	b.Grow(len(norm))
	for i := 0; i < len(norm); i++ {
		c := norm[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "field"
	}
	return out
}

// IsSensitiveKey reports whether key identifies a credential, cookie, or secret field.
func IsSensitiveKey(key string) bool {
	nk := normalizeKey(key)
	if nk == "" {
		return false
	}
	if sensitiveKeys[nk] {
		return true
	}
	if strings.HasSuffix(nk, "_api_key") ||
		strings.HasSuffix(nk, "_secret") ||
		strings.HasSuffix(nk, "_token") ||
		strings.HasSuffix(nk, "_password") ||
		strings.HasSuffix(nk, "_cookie") {
		// Permit explicitly non-secret counters/types such as token_count, input_tokens, output_tokens.
		if strings.HasSuffix(nk, "_tokens") || strings.Contains(nk, "token_count") || strings.Contains(nk, "max_tokens") {
			return false
		}
		return true
	}
	return false
}

// IsPayloadKey reports whether key carries prompt, completion, or tool-argument content.
func IsPayloadKey(key string) bool {
	return payloadKeys[normalizeKey(key)]
}

// SanitizeString scrubs inline credential patterns (Bearer, vk_tp_*, sk-*, cookies, URL userinfo)
// and flattens control characters so logs/spans never split lines or leak secrets.
func (r *Redactor) SanitizeString(s string) string {
	return r.SanitizeStringBounded(s, maxStringBytes)
}

// SanitizeStringBounded behaves like SanitizeString with a caller-supplied maximum byte limit
// (falling back to 16 KiB when maxBytes <= 0).
func (r *Redactor) SanitizeStringBounded(s string, maxBytes int) string {
	if s == "" {
		return ""
	}
	if maxBytes <= 0 {
		maxBytes = maxStringBytes
	}
	out := s
	out = urlUserInfoRe.ReplaceAllString(out, "${1}"+RedactedMarker+":"+RedactedMarker+"@")
	out = bearerPattern.ReplaceAllString(out, "Bearer "+RedactedMarker)
	out = basicPattern.ReplaceAllString(out, "Basic "+RedactedMarker)
	out = vkeyPattern.ReplaceAllString(out, RedactedMarker)
	out = skPattern.ReplaceAllString(out, RedactedMarker)
	out = googleKeyPattern.ReplaceAllString(out, RedactedMarker)
	out = ghTokenPattern.ReplaceAllString(out, RedactedMarker)
	out = cookieAssignRe.ReplaceAllString(out, "${1}${2}="+RedactedMarker)
	out = strings.Map(func(ch rune) rune {
		if ch == '\r' || ch == '\n' || ch == '\t' {
			return ' '
		}
		if ch < 0x20 || ch == 0x7f {
			return -1
		}
		return ch
	}, out)
	if len(out) > maxBytes {
		out = out[:maxBytes]
	}
	return out
}

// SanitizeDebugPayload scrubs all credentials and secrets from an authorized opt-in debug
// payload (supporting arbitrary non-prefixed JSON credential fields and malformed JSON fallback)
// while preserving non-secret prompt/completion context up to maxBytes.
func (r *Redactor) SanitizeDebugPayload(raw []byte, maxBytes int) []byte {
	if len(raw) == 0 {
		return nil
	}
	if maxBytes <= 0 {
		maxBytes = 256*1024 - 16
	}
	capRed := &Redactor{CapturePayloads: true}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		scrubbed := capRed.sanitizeValueWithLimit(decoded, 0, maxBytes)
		if encoded, merr := json.Marshal(scrubbed); merr == nil {
			if len(encoded) > maxBytes {
				truncated := encoded[:maxBytes]
				out := make([]byte, len(truncated))
				copy(out, truncated)
				clear(encoded)
				return out
			}
			return encoded
		}
	}

	// Fallback for non-JSON or truncated JSON bodies: scrub JSON key-values + inline tokens.
	scrubbedText := jsonSecretKVRe.ReplaceAllStringFunc(string(raw), func(match string) string {
		sub := jsonSecretKVRe.FindStringSubmatch(match)
		if len(sub) >= 2 && IsSensitiveKey(sub[1]) {
			return `"` + sub[1] + `":"` + RedactedMarker + `"`
		}
		return match
	})
	bounded := capRed.SanitizeStringBounded(scrubbedText, maxBytes)
	return []byte(bounded)
}

// SanitizeHeaders returns a redacted header map safe for logs and spans.
func (r *Redactor) SanitizeHeaders(hdrs http.Header) map[string]string {
	if len(hdrs) == 0 {
		return nil
	}
	out := make(map[string]string, len(hdrs))
	for k, vals := range hdrs {
		nk := strings.ToLower(strings.TrimSpace(k))
		if nk == "" {
			continue
		}
		if IsSensitiveKey(nk) {
			out[nk] = RedactedMarker
			continue
		}
		joined := strings.Join(vals, ", ")
		out[nk] = r.SanitizeString(joined)
	}
	return out
}

// SanitizeAttribute sanitizes a single key/value attribute for spans or structured logs.
// When ok == false, callers must omit the attribute (used when eliding payload keys).
func (r *Redactor) SanitizeAttribute(key string, val any) (any, bool) {
	if IsSensitiveKey(key) {
		return RedactedMarker, true
	}
	capture := r != nil && r.CapturePayloads
	if !capture && IsPayloadKey(key) {
		return nil, false
	}
	return r.sanitizeValue(val, 0), true
}

// SanitizeMap recursively clones and sanitizes a map up to maxRecursionDepth.
func (r *Redactor) SanitizeMap(m map[string]any) map[string]any {
	return r.sanitizeMapDepth(m, 0)
}

func (r *Redactor) sanitizeMapDepth(m map[string]any, depth int) map[string]any {
	return r.sanitizeMapDepthWithLimit(m, depth, maxStringBytes)
}

func (r *Redactor) sanitizeMapDepthWithLimit(m map[string]any, depth int, maxStrBytes int) map[string]any {
	if len(m) == 0 {
		return map[string]any{}
	}
	if depth >= maxRecursionDepth {
		return map[string]any{"_truncated": true}
	}
	capture := r != nil && r.CapturePayloads
	out := make(map[string]any, len(m))
	for k, v := range m {
		if IsSensitiveKey(k) {
			out[k] = RedactedMarker
			continue
		}
		if !capture && IsPayloadKey(k) {
			out[k] = ElidedMarker
			continue
		}
		out[k] = r.sanitizeValueWithLimit(v, depth+1, maxStrBytes)
	}
	return out
}

func (r *Redactor) sanitizeValue(val any, depth int) any {
	return r.sanitizeValueWithLimit(val, depth, maxStringBytes)
}

func (r *Redactor) sanitizeValueWithLimit(val any, depth int, maxStrBytes int) any {
	if val == nil {
		return nil
	}
	if depth >= maxRecursionDepth {
		return ElidedMarker
	}
	switch v := val.(type) {
	case string:
		return r.SanitizeStringBounded(v, maxStrBytes)
	case []byte:
		return r.SanitizeStringBounded(string(v), maxStrBytes)
	case map[string]any:
		return r.sanitizeMapDepthWithLimit(v, depth+1, maxStrBytes)
	case map[string]string:
		out := make(map[string]string, len(v))
		capture := r != nil && r.CapturePayloads
		for k, sv := range v {
			if IsSensitiveKey(k) {
				out[k] = RedactedMarker
				continue
			}
			if !capture && IsPayloadKey(k) {
				out[k] = ElidedMarker
				continue
			}
			out[k] = r.SanitizeStringBounded(sv, maxStrBytes)
		}
		return out
	case []string:
		out := make([]string, len(v))
		for i, s := range v {
			out[i] = r.SanitizeStringBounded(s, maxStrBytes)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = r.sanitizeValueWithLimit(item, depth+1, maxStrBytes)
		}
		return out
	case error:
		return r.SanitizeStringBounded(v.Error(), maxStrBytes)
	default:
		return v
	}
}

// Canonical structured JSON log levels matching SPEC-CORE-02 Section 3.2.
const (
	LevelDebug = "DEBUG"
	LevelInfo  = "INFO"
	LevelWarn  = "WARN"
	LevelError = "ERROR"
)

// LogRecord describes a flat single-line snake_case structured JSON log event.
type LogRecord struct {
	Level      string
	Event      string
	EntryPoint string
	RequestID  string
	TraceID    string
	Fields     map[string]any
}

// JSONLogger writes flat single-line snake_case JSON logs protected at the sink boundary
// by privacy.Redactor (TP-FR-0806 / OPS-AC-10).
type JSONLogger struct {
	mu       sync.Mutex
	out      io.Writer
	redactor *Redactor
	nowFn    func() time.Time
}

// NewJSONLogger constructs a thread-safe flat JSON structured logger writing to out.
func NewJSONLogger(out io.Writer, redactor *Redactor, nowFn func() time.Time) *JSONLogger {
	if out == nil {
		out = io.Discard
	}
	if redactor == nil {
		redactor = NewDefaultRedactor()
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	return &JSONLogger{
		out:      out,
		redactor: redactor,
		nowFn:    nowFn,
	}
}

// Log writes a single flat JSON line with strictly snake_case top-level keys.
// Nested maps/slices supplied in rec.Fields are flattened into snake_case keys
// (e.g. prefix_child) and sanitized via Redactor at the lowest sink boundary.
func (l *JSONLogger) Log(rec LogRecord) error {
	if l == nil {
		return nil
	}
	level := strings.ToUpper(strings.TrimSpace(rec.Level))
	switch level {
	case LevelDebug, LevelInfo, LevelWarn, LevelError:
	default:
		level = LevelInfo
	}
	entryPoint := NormalizeSnakeCase(strings.TrimSpace(rec.EntryPoint))
	if entryPoint == "field" || entryPoint == "" {
		entryPoint = "system"
	}
	reqID := l.redactor.SanitizeString(strings.TrimSpace(rec.RequestID))
	event := l.redactor.SanitizeString(strings.TrimSpace(rec.Event))
	if event == "" {
		event = "gateway.event"
	}

	payload := map[string]any{
		"ts":          l.nowFn().UTC().Format("2006-01-02T15:04:05.000Z"),
		"level":       level,
		"event":       event,
		"entry_point": entryPoint,
		"request_id":  reqID,
	}
	if tid := strings.TrimSpace(rec.TraceID); tid != "" {
		payload["trace_id"] = l.redactor.SanitizeString(tid)
	}

	if len(rec.Fields) > 0 {
		l.flattenFields("", rec.Fields, payload, 0)
	}

	line, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	l.mu.Lock()
	_, werr := l.out.Write(line)
	l.mu.Unlock()
	return werr
}

func (l *JSONLogger) flattenFields(prefix string, src map[string]any, dst map[string]any, depth int) {
	if depth >= maxRecursionDepth {
		return
	}
	keys := make([]string, 0, len(src))
	for k := range src {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	reserved := map[string]bool{
		"ts": true, "level": true, "event": true, "entry_point": true, "request_id": true, "trace_id": true,
	}

	for _, rawKey := range keys {
		nk := NormalizeSnakeCase(rawKey)
		fullKey := nk
		if prefix != "" {
			fullKey = prefix + "_" + nk
		}
		if reserved[fullKey] {
			continue
		}
		if IsSensitiveKey(rawKey) || IsSensitiveKey(fullKey) {
			dst[fullKey] = RedactedMarker
			continue
		}
		if !l.redactor.CapturePayloads && (IsPayloadKey(rawKey) || IsPayloadKey(nk)) {
			// Metadata-only mode omits prompt/completion payloads completely from structured logs.
			continue
		}
		val := src[rawKey]
		switch nested := val.(type) {
		case map[string]any:
			l.flattenFields(fullKey, nested, dst, depth+1)
		case map[string]string:
			conv := make(map[string]any, len(nested))
			for mk, mv := range nested {
				conv[mk] = mv
			}
			l.flattenFields(fullKey, conv, dst, depth+1)
		default:
			sanitized, ok := l.redactor.SanitizeAttribute(rawKey, val)
			if ok {
				if m, isMap := sanitized.(map[string]any); isMap {
					l.flattenFields(fullKey, m, dst, depth+1)
				} else {
					dst[fullKey] = sanitized
				}
			}
		}
	}
}
