// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package policy evaluates monotonic hierarchical policies across
// Instance ∩ Org ∩ Project ∩ Environment boundaries (422 policy_relaxation_forbidden).
//
// DAG Boundary (Tier 5): May import tiers <= 4; must never import ingress,
// admin, webui, ops, cli, or tools/spikes.
package policy
