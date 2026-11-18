// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package testharness implements offline extension manifest validation,
// reproducible .tpkg packaging, and deterministic hermetic cassette replay
// verification matching SPEC-OPS-04 and SPEC-QA-01 (TP-IMP-0014).
package testharness

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Canonical CLI exit codes synchronized 100% with SPEC-OPS-04 (developer-experience.md).
const (
	ExitOK         = 0
	ExitValidation = 2
	ExitNetwork    = 10
	ExitSecurity   = 14
	ExitContract   = 22
	ExitInternal   = 30
)

// ExitError pairs a failure with its canonical SPEC-OPS-04 exit code.
type ExitError struct {
	Code    int
	Message string
	Cause   error
}

// Error implements error without exposing raw secrets.
func (e *ExitError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil && e.Message != "" {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Message
}

// Unwrap exposes the underlying typed error cause.
func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// CodeOf returns the canonical SPEC-OPS-04 exit code for err (0 if nil, 2/14/22 for typed failures).
func CodeOf(err error) int {
	if err == nil {
		return ExitOK
	}
	var ex *ExitError
	if errors.As(err, &ex) && ex.Code != 0 {
		return ex.Code
	}
	return ExitValidation
}

func validationErr(msg string, cause error) error {
	return &ExitError{Code: ExitValidation, Message: msg, Cause: cause}
}

func securityErr(msg string, cause error) error {
	return &ExitError{Code: ExitSecurity, Message: msg, Cause: cause}
}

func contractErr(msg string, cause error) error {
	return &ExitError{Code: ExitContract, Message: msg, Cause: cause}
}

// SanitizeTerminalText strips ANSI CSI, OSC-52, C0 (\r included), DEL, and UTF-8 C1 (U+0080..U+009F)
// control characters from untrusted manifest or upstream text prior to terminal emission (CT-27).
func SanitizeTerminalText(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	n := len(s)
	for i := 0; i < n; {
		c := s[i]
		if c == 0x1b { // ESC
			if i+1 < n {
				nxt := s[i+1]
				if nxt == '[' { // CSI sequence: ESC [ ... final byte 0x40..0x7E
					j := i + 2
					for j < n && !(s[j] >= 0x40 && s[j] <= 0x7E) {
						j++
					}
					if j < n {
						i = j + 1
						continue
					}
					break
				}
				if nxt == ']' { // OSC sequence (e.g. OSC-52): ESC ] ... BEL or ESC \
					j := i + 2
					for j < n {
						if s[j] == 0x07 {
							j++
							break
						}
						if s[j] == 0x1b && j+1 < n && s[j+1] == '\\' {
							j += 2
							break
						}
						j++
					}
					i = j
					continue
				}
			}
			i++
			continue
		}
		if c < 0x80 {
			if (c < 0x20 && c != '\n' && c != '\t') || c == 0x7f {
				i++
				continue
			}
			b.WriteByte(c)
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		if r >= 0x80 && r <= 0x9f {
			i += size
			continue
		}
		b.WriteRune(r)
		i += size
	}
	return b.String()
}
