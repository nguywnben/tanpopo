// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package provider implements the 4-entity provider catalog (Adapter -> Connection ->
// LogicalModel -> Deployment) and bundled Official Reference Adapter fixtures.
//
// DAG Boundary (Tier 5): May import tiers <= 4 (and extension port contracts if
// needed); must never import ingress, admin, webui, ops, cli, or tools/spikes.
package provider
