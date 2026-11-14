// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package testkit provides deterministic offline test doubles (FakeLLMServer,
// FakeOAuthIdP, FakeMCPServer, FakeRegistry), FakeClock, CanaryScanner, prebuilt
// Wasm fixtures, and dual-layer loopback socket guards (netguard).
//
// DAG Boundary: Test-support package for hermetic verification; never imported by
// production runtime code paths in cmd/tanpopo.
package testkit
