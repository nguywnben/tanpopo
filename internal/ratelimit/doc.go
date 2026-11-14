// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package ratelimit implements RPM, TPM, and MaxConcurrentStreams enforcement
// (429 rate_limit_exceeded + Retry-After) across standalone RAM and Valkey Lua.
//
// DAG Boundary (Tier 5): May import tiers <= 4; must never import ingress,
// admin, webui, ops, cli, or tools/spikes.
package ratelimit
