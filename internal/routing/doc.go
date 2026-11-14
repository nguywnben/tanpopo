// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package routing implements the 6-step capability-aware Route Resolver,
// fail-closed Data Residency fences (503 no_compliant_deployment_available),
// sliding-window SM-04 circuit breakers, and zero-side-effect Route Explain.
//
// DAG Boundary (Tier 5): May import tiers <= 4; must never import ingress,
// admin, webui, ops, cli, or tools/spikes.
package routing
