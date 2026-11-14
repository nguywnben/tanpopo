// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package admin implements the Control Plane REST API (/admin/v1/*) enforcing
// ETag / If-Match concurrency (409), X-CSRF-Token checks, RBAC, and HMAC cursors.
//
// DAG Boundary (Tier 6): May import domain engines (tiers <= 5); must never
// import cli or cmd/tanpopo or tools/spikes.
package admin
