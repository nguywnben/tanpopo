// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package webhook implements HMAC-SHA256 signed event dispatching for budget
// thresholds, MCP HITL approval requests, plugin quarantines, and Kill-Switches.
//
// DAG Boundary (Tier 5): May import tiers <= 4; must never import ingress,
// admin, webui, ops, cli, or tools/spikes.
package webhook
