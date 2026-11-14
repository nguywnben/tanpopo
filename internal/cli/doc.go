// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package cli implements the unified tanpopo command tree (serve, plugin, admin,
// db, backup, upgrade, bundle, doctor, support-bundle) and canonical exit codes.
//
// DAG Boundary (Tier 7): May import tiers <= 6 (including ingress, admin, webui,
// ops); must never import cmd/tanpopo or tools/spikes.
package cli
