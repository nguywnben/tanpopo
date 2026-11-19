// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package firewall

import (
	"errors"
	"fmt"
	"testing"
)

func TestNormalizeClassFailClosed(t *testing.T) {
	tests := []struct {
		input string
		want  Class
	}{
		{"read_only", ReadOnly},
		{"mutating", Mutating},
		{"destructive", Destructive},
		{"", Mutating},
		{" READ_ONLY ", ReadOnly},
		{" unknown ", Mutating},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := NormalizeClass(tt.input); got != tt.want {
				t.Fatalf("NormalizeClass(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRetryAllowedAmbiguousFailure(t *testing.T) {
	for _, class := range []Class{ReadOnly, Mutating, Destructive, "", "unrecognized"} {
		want := class == ReadOnly
		if got := RetryAllowed(class, true); got != want {
			t.Errorf("RetryAllowed(%q, ambiguous) = %t, want %t", class, got, want)
		}
		if !RetryAllowed(class, false) {
			t.Errorf("RetryAllowed(%q, definitive failure) = false, want true", class)
		}
	}
}

func TestAmbiguousSideEffectTimeoutCode(t *testing.T) {
	if got := ErrAmbiguousSideEffectTimeout.Error(); got != AmbiguousSideEffectTimeoutCode {
		t.Fatalf("error code = %q, want %q", got, AmbiguousSideEffectTimeoutCode)
	}
	if !errors.Is(fmt.Errorf("wrapped: %w", ErrAmbiguousSideEffectTimeout), ErrAmbiguousSideEffectTimeout) {
		t.Fatal("canonical timeout error should support errors.Is")
	}
}
