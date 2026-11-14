// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package canary provides CanaryScanner to detect unintended leakage of test
// canary tokens across logs, files, and JSON payloads in raw, base64, and
// URL-encoded forms without ever echoing secret bytes in failure diagnostics.
package canary

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// DefaultCanarySecret returns the canonical Tanpopo test canary token without
// storing an unbroken matching literal in source files.
func DefaultCanarySecret() string {
	return "sk-tp-" + "canary-DO-NOT-LEAK-9f8e7d6c"
}

// DefaultForbiddenCIKey returns the CI trap token configured in GitHub Actions.
func DefaultForbiddenCIKey() string {
	return "CANARY_FORBIDDEN_" + "CI_KEY"
}

var (
	canaryPrefixRe  = regexp.MustCompile(`sk-tp-` + `canary-[A-Za-z0-9_-]{4,}`)
	base64RunRe     = regexp.MustCompile(`[A-Za-z0-9+/_-]{12,}={0,2}`)
	unicodeEscRe    = regexp.MustCompile(`\\u[0-9a-fA-F]{4}`)
	validPercentRe  = regexp.MustCompile(`%[0-9a-fA-F]{2}`)
)

// Finding describes a single detected canary leak without disclosing secret bytes.
type Finding struct {
	Source  string
	Variant string
	Offset  int
}

func (f Finding) String() string {
	return fmt.Sprintf("[REDACTED:canary_leak variant=%s source=%q offset=%d]", f.Variant, f.Source, f.Offset)
}

// NamedData pairs a logical source label with byte contents for batch scanning.
type NamedData struct {
	Source string
	Data   []byte
}

// SafeBuffer is a concurrency-safe byte buffer suitable for capturing logs under -race.
type SafeBuffer struct {
	mu  sync.RWMutex
	buf bytes.Buffer
}

// NewSafeBuffer allocates an empty concurrency-safe log buffer.
func NewSafeBuffer() *SafeBuffer {
	return &SafeBuffer{}
}

func (b *SafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *SafeBuffer) Bytes() []byte {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cp := make([]byte, b.buf.Len())
	copy(cp, b.buf.Bytes())
	return cp
}

func (b *SafeBuffer) String() string {
	return string(b.Bytes())
}

func (b *SafeBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

// Scanner inspects in-memory buffers, JSON values, readers, and directories for secret leaks.
type Scanner struct {
	mu      sync.RWMutex
	secrets []string
}

// NewScanner initializes a Scanner for custom secrets, or DefaultCanarySecret +
// DefaultForbiddenCIKey when called with no arguments.
func NewScanner(secrets ...string) *Scanner {
	s := &Scanner{}
	if len(secrets) == 0 {
		s.secrets = []string{DefaultCanarySecret(), DefaultForbiddenCIKey()}
		return s
	}
	for _, sec := range secrets {
		if sec != "" {
			s.secrets = append(s.secrets, sec)
		}
	}
	return s
}

// AddSecret registers an additional canary token on the scanner.
func (s *Scanner) AddSecret(secret string) {
	if secret == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.secrets {
		if existing == secret {
			return
		}
	}
	s.secrets = append(s.secrets, secret)
}

func (s *Scanner) snapshotSecrets() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.secrets))
	copy(out, s.secrets)
	return out
}

// ScanString inspects text for any raw, base64, URL-encoded, or JSON-escaped canary secret.
func (s *Scanner) ScanString(source, text string) []Finding {
	secrets := s.snapshotSecrets()
	var findings []Finding

	if loc := canaryPrefixRe.FindStringIndex(text); loc != nil {
		findings = append(findings, Finding{Source: source, Variant: "canary-prefix-pattern", Offset: loc[0]})
	}

	for _, sec := range secrets {
		if idx := strings.Index(text, sec); idx >= 0 {
			findings = append(findings, Finding{Source: source, Variant: "raw", Offset: idx})
		}
		for _, variant := range []struct {
			name string
			enc  string
		}{
			{"base64-std", base64.StdEncoding.EncodeToString([]byte(sec))},
			{"base64-raw", base64.RawStdEncoding.EncodeToString([]byte(sec))},
			{"base64-url", base64.URLEncoding.EncodeToString([]byte(sec))},
			{"base64-rawurl", base64.RawURLEncoding.EncodeToString([]byte(sec))},
			{"url-query", url.QueryEscape(sec)},
			{"url-path", url.PathEscape(sec)},
		} {
			if idx := strings.Index(text, variant.enc); idx >= 0 {
				findings = append(findings, Finding{Source: source, Variant: variant.name, Offset: idx})
			}
		}
	}

	// Decode base64-like tokens across byte alignments (offsets 0, 1, 2) to catch
	// embedded prefixes such as base64("Authorization: Bearer <canary>").
	for _, match := range base64RunRe.FindAllStringIndex(text, -1) {
		run := text[match[0]:match[1]]
		if containsDecodedBase64Secret(run, secrets) {
			findings = append(findings, Finding{Source: source, Variant: "base64-decoded", Offset: match[0]})
		}
	}

	// Handle lowercase/double URL-encoding safely even when stray '%' characters appear elsewhere in logs.
	if strings.ContainsRune(text, '%') {
		if decoded := decodeURLMultiPass(text, 2); decoded != text {
			for _, f := range s.scanPlainSecretsOnly(source, "url-decoded", decoded, secrets) {
				findings = append(findings, f)
			}
		}
	}

	// Handle JSON \uXXXX string escapes (e.g. \u002d for '-').
	if unicodeEscRe.MatchString(text) {
		unescaped := decodeJSONUnicodeEscapes(text)
		if unescaped != text {
			for _, f := range s.scanPlainSecretsOnly(source, "json-unicode-escaped", unescaped, secrets) {
				findings = append(findings, f)
			}
		}
	}

	return dedupFindings(findings)
}

func (s *Scanner) scanPlainSecretsOnly(source, variant, text string, secrets []string) []Finding {
	var out []Finding
	if loc := canaryPrefixRe.FindStringIndex(text); loc != nil {
		out = append(out, Finding{Source: source, Variant: variant, Offset: loc[0]})
	}
	for _, sec := range secrets {
		if idx := strings.Index(text, sec); idx >= 0 {
			out = append(out, Finding{Source: source, Variant: variant, Offset: idx})
		}
	}
	return out
}

func containsDecodedBase64Secret(run string, secrets []string) bool {
	encodings := []*base64.Encoding{
		base64.RawStdEncoding,
		base64.RawURLEncoding,
	}
	base := strings.TrimRight(run, "=")
	maxShift := 3
	if len(base) <= maxShift {
		maxShift = len(base) - 1
	}
	for shift := 0; shift <= maxShift; shift++ {
		sub := base[shift:]
		if rem := len(sub) % 4; rem == 1 {
			sub = sub[:len(sub)-1]
		}
		if len(sub) < 12 {
			continue
		}
		for _, enc := range encodings {
			raw, err := enc.DecodeString(sub)
			if err != nil {
				continue
			}
			decoded := string(raw)
			if canaryPrefixRe.MatchString(decoded) {
				return true
			}
			for _, sec := range secrets {
				if strings.Contains(decoded, sec) {
					return true
				}
			}
		}
	}
	return false
}

func decodeURLMultiPass(text string, passes int) string {
	cur := text
	for i := 0; i < passes; i++ {
		next := validPercentRe.ReplaceAllStringFunc(cur, func(esc string) string {
			u, err := url.PathUnescape(esc)
			if err != nil {
				return esc
			}
			return u
		})
		if next == cur {
			break
		}
		cur = next
	}
	return cur
}

func decodeJSONUnicodeEscapes(text string) string {
	return unicodeEscRe.ReplaceAllStringFunc(text, func(tok string) string {
		var s string
		if err := json.Unmarshal([]byte(`"`+tok+`"`), &s); err == nil {
			return s
		}
		return tok
	})
}

func dedupFindings(in []Finding) []Finding {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]Finding, 0, len(in))
	for _, f := range in {
		k := fmt.Sprintf("%s|%s|%d", f.Source, f.Variant, f.Offset)
		if !seen[k] {
			seen[k] = true
			out = append(out, f)
		}
	}
	return out
}

// ScanBytes inspects byte data for canary leaks.
func (s *Scanner) ScanBytes(source string, data []byte) []Finding {
	return s.ScanString(source, string(data))
}

// ScanReader drains r and inspects its contents for canary leaks.
func (s *Scanner) ScanReader(source string, r io.Reader) ([]Finding, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("canary scan read %q: %w", source, err)
	}
	return s.ScanBytes(source, data), nil
}

// ScanFile reads and inspects a single file path for canary leaks.
func (s *Scanner) ScanFile(path string) ([]Finding, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("canary scan file %q: %w", path, err)
	}
	return s.ScanBytes(path, raw), nil
}

// ScanDir recursively scans all regular files in dir for canary leaks.
func (s *Scanner) ScanDir(dir string) ([]Finding, error) {
	var all []Finding
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		f, err := s.ScanFile(path)
		if err != nil {
			return err
		}
		all = append(all, f...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// ScanJSON serializes a Go value as JSON (or scans raw bytes if already json.RawMessage)
// and inspects both the raw wire JSON and decoded representation.
func (s *Scanner) ScanJSON(source string, v any) ([]Finding, error) {
	var raw []byte
	switch tv := v.(type) {
	case json.RawMessage:
		raw = tv
	case []byte:
		raw = tv
	case string:
		raw = []byte(tv)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("canary scan json marshal %q: %w", source, err)
		}
		raw = b
	}
	return s.ScanBytes(source, raw), nil
}

// Check inspects data and returns a redacted error if any leak is detected.
func (s *Scanner) Check(source string, data []byte) error {
	findings := s.ScanBytes(source, data)
	if len(findings) == 0 {
		return nil
	}
	msgs := make([]string, len(findings))
	for i, f := range findings {
		msgs[i] = f.String()
	}
	return fmt.Errorf("canary secret leak detected (%d finding(s)): %s", len(findings), strings.Join(msgs, "; "))
}

// AssertClean fails t immediately (without printing raw or encoded secrets)
// if any provided NamedData target contains a canary token.
func (s *Scanner) AssertClean(t testing.TB, targets ...NamedData) {
	t.Helper()
	var msgs []string
	for _, tgt := range targets {
		for _, f := range s.ScanBytes(tgt.Source, tgt.Data) {
			msgs = append(msgs, f.String())
		}
	}
	if len(msgs) > 0 {
		t.Fatalf("canary leak assertion failed (%d finding(s)): %s", len(msgs), strings.Join(msgs, "; "))
	}
}

// AssertCleanBuffer asserts a SafeBuffer contains no canary token leaks.
func (s *Scanner) AssertCleanBuffer(t testing.TB, source string, buf *SafeBuffer) {
	t.Helper()
	s.AssertClean(t, NamedData{Source: source, Data: buf.Bytes()})
}

// AssertCleanDir asserts all files in dir contain no canary token leaks.
func (s *Scanner) AssertCleanDir(t testing.TB, dir string) {
	t.Helper()
	findings, err := s.ScanDir(dir)
	if err != nil {
		t.Fatalf("canary scan dir %q: %v", dir, err)
	}
	if len(findings) > 0 {
		msgs := make([]string, len(findings))
		for i, f := range findings {
			msgs[i] = f.String()
		}
		t.Fatalf("canary leak assertion failed in dir %q (%d finding(s)): %s", dir, len(findings), strings.Join(msgs, "; "))
	}
}
