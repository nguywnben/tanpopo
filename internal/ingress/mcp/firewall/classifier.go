// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package firewall classifies MCP side effects and bounds high-risk admission.
package firewall

import (
	"errors"
	"strings"
)

// Class is the canonical side-effect class of an MCP tool.
type Class string

const (
	ReadOnly    Class = "read_only"
	Mutating    Class = "mutating"
	Destructive Class = "destructive"

	// AmbiguousSideEffectTimeoutCode is the canonical code returned when an
	// ambiguous transport failure follows a non-read-only invocation.
	AmbiguousSideEffectTimeoutCode = "ambiguous_side_effect_timeout"
)

// ErrAmbiguousSideEffectTimeout identifies an invocation that may have taken
// effect even though its transport response was lost or timed out.
var ErrAmbiguousSideEffectTimeout = errors.New(AmbiguousSideEffectTimeoutCode)

// NormalizeClass returns one of the canonical classes. Empty and unknown
// values fail closed to Mutating; whitespace and case are normalized.
func NormalizeClass(value string) Class {
	switch Class(strings.ToLower(strings.TrimSpace(value))) {
	case ReadOnly:
		return ReadOnly
	case Mutating:
		return Mutating
	case Destructive:
		return Destructive
	default:
		return Mutating
	}
}

// RetryAllowed reports whether a retry is safe under the side-effect policy.
// Ambiguous failures may be retried only for explicitly read-only tools.
func RetryAllowed(class Class, ambiguousTransportFailure bool) bool {
	if !ambiguousTransportFailure {
		return true
	}
	return NormalizeClass(string(class)) == ReadOnly
}
