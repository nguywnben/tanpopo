// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import "testing"

func TestEstimatedOutputTextBytesExcludesResponseEnvelope(t *testing.T) {
	response := []byte(`{"id":"chatcmpl-test","model":"accept-chat","choices":[{"message":{"role":"assistant","content":"deterministic estimated usage response"}}]}`)
	if got := estimatedOutputTextBytes(response); got != 38 {
		t.Fatalf("estimated output text bytes=%d, want 38 (excluding IDs, model, and JSON fields)", got)
	}
}

func TestEstimatedOutputTextBytesCountsNestedResponseText(t *testing.T) {
	response := []byte(`{"output":[{"content":[{"type":"output_text","text":"hello"},{"type":"output_text","text":"world"}]}]}`)
	if got := estimatedOutputTextBytes(response); got != 10 {
		t.Fatalf("estimated nested output text bytes=%d, want 10", got)
	}
}
