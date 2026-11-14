// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package extension implements safe .tpkg archive verification, SM-01 capability
// gating, tanpopo.lock management, Tier-1 Declarative evaluation, Tier-2 wazero
// Wasm pooling, and Tier-3 ConnectRPC sidecar hosting.
//
// DAG Boundary (Tier 5): May import tiers <= 4 (pkg, storage, security, config);
// must never import ingress, admin, webui, ops, cli, or tools/spikes.
package extension
