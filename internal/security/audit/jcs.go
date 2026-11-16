// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package audit implements immutable per-Organization and SYSTEM RFC 8785 (JCS)
// hash-chained audit logging, tamper detection (ALT-AUDIT-TAMPER), and forensic
// blast-radius correlation queries (TP-IMP-0023 / CR-06).
//
// DAG Boundary (Tier 3): May import stdlib, internal/pkg, and internal/storage/* only.
package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrInvalidCanonicalJSON indicates input could not be parsed or serialized strictly under RFC 8785.
var ErrInvalidCanonicalJSON = errors.New("audit: invalid JSON for RFC 8785 JCS canonicalization")

// CanonicalizeJSON parses raw UTF-8 JSON bytes and emits deterministic RFC 8785 (JCS) output.
func CanonicalizeJSON(raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("%w: invalid UTF-8 input", ErrInvalidCanonicalJSON)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: empty JSON input", ErrInvalidCanonicalJSON)
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()

	var val any
	if err := dec.Decode(&val); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCanonicalJSON, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing non-whitespace tokens", ErrInvalidCanonicalJSON)
	}

	var buf bytes.Buffer
	buf.Grow(len(trimmed))
	if err := writeCanonicalValue(&buf, val); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CanonicalizeValue serializes a Go value (primitives, nil, []any, map[string]any, or json.Number)
// strictly according to RFC 8785 (JCS).
func CanonicalizeValue(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCanonicalValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonicalValue(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
		return nil
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case string:
		return writeCanonicalString(buf, x)
	case json.Number:
		f, err := strconv.ParseFloat(x.String(), 64)
		if err != nil {
			return fmt.Errorf("%w: parse number %q: %v", ErrInvalidCanonicalJSON, x.String(), err)
		}
		return writeCanonicalFloat(buf, f)
	case float64:
		return writeCanonicalFloat(buf, x)
	case float32:
		return writeCanonicalFloat(buf, float64(x))
	case int:
		return writeCanonicalInt64(buf, int64(x))
	case int64:
		return writeCanonicalInt64(buf, x)
	case int32:
		return writeCanonicalInt64(buf, int64(x))
	case uint64:
		if x <= 9007199254740991 {
			buf.WriteString(strconv.FormatUint(x, 10))
			return nil
		}
		return writeCanonicalFloat(buf, float64(x))
	case uint32:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
		return nil
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonicalValue(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return CompareUTF16(keys[i], keys[j]) < 0
		})
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonicalString(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeCanonicalValue(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	default:
		// Marshal arbitrary structs/typed slices cleanly via JSON first, then canonicalize.
		raw, err := json.Marshal(x)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidCanonicalJSON, err)
		}
		canon, err := CanonicalizeJSON(raw)
		if err != nil {
			return err
		}
		buf.Write(canon)
		return nil
	}
}

func writeCanonicalInt64(buf *bytes.Buffer, n int64) error {
	// IEEE-754 exact safe integer window [-9007199254740991, 9007199254740991].
	if n >= -9007199254740991 && n <= 9007199254740991 {
		buf.WriteString(strconv.FormatInt(n, 10))
		return nil
	}
	return writeCanonicalFloat(buf, float64(n))
}

// writeCanonicalString escapes strictly per RFC 8785 Section 3.2.2.2 without
// HTML-escaping '<', '>', '&', or '/' and preserving valid UTF-8 sequences verbatim.
func writeCanonicalString(buf *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: invalid UTF-8 string", ErrInvalidCanonicalJSON)
	}
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if r >= 0 && r < 0x20 {
				buf.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
	return nil
}

// writeCanonicalFloat formats finite float64 values using ECMAScript Number::toString (RFC 8785 Section 3.2.2.3).
func writeCanonicalFloat(buf *bytes.Buffer, f float64) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("%w: non-finite number %v forbidden in JSON", ErrInvalidCanonicalJSON, f)
	}
	if f == 0 {
		buf.WriteByte('0')
		return nil
	}
	if f < 0 {
		buf.WriteByte('-')
		f = -f
	}

	// Shortest round-trip decimal representation in scientific form: d[.<digits>]e<+/-exp>.
	sci := strconv.FormatFloat(f, 'e', -1, 64)
	eIdx := strings.IndexByte(sci, 'e')
	mantissa := sci[:eIdx]
	exp, err := strconv.Atoi(sci[eIdx+1:])
	if err != nil {
		return fmt.Errorf("%w: parse exponent %q: %v", ErrInvalidCanonicalJSON, sci, err)
	}

	digits := strings.ReplaceAll(mantissa, ".", "")
	k := len(digits)
	n := exp + 1

	switch {
	case k <= n && n <= 21:
		buf.WriteString(digits)
		for i := 0; i < n-k; i++ {
			buf.WriteByte('0')
		}
	case 0 < n && n <= 21:
		buf.WriteString(digits[:n])
		buf.WriteByte('.')
		buf.WriteString(digits[n:])
	case -6 < n && n <= 0:
		buf.WriteString("0.")
		for i := 0; i < -n; i++ {
			buf.WriteByte('0')
		}
		buf.WriteString(digits)
	case k == 1:
		buf.WriteString(digits)
		writeES6Exponent(buf, n-1)
	default:
		buf.WriteByte(digits[0])
		buf.WriteByte('.')
		buf.WriteString(digits[1:])
		writeES6Exponent(buf, n-1)
	}
	return nil
}

func writeES6Exponent(buf *bytes.Buffer, exp int) {
	buf.WriteByte('e')
	if exp >= 0 {
		buf.WriteByte('+')
	} else {
		buf.WriteByte('-')
		exp = -exp
	}
	buf.WriteString(strconv.Itoa(exp))
}

// CompareUTF16 compares two UTF-8 strings lexicographically by UTF-16 code units (RFC 8785 Section 3.2.3).
func CompareUTF16(a, b string) int {
	if a == b {
		return 0
	}
	ua := utf16.Encode([]rune(a))
	ub := utf16.Encode([]rune(b))
	minLen := len(ua)
	if len(ub) < minLen {
		minLen = len(ub)
	}
	for i := 0; i < minLen; i++ {
		if ua[i] < ub[i] {
			return -1
		}
		if ua[i] > ub[i] {
			return 1
		}
	}
	if len(ua) < len(ub) {
		return -1
	}
	return 1
}
