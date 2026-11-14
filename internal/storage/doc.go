// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package storage houses storage/port domain persistence contracts and concrete
// dual-mode database drivers (SQLite WAL and PostgreSQL 16+ FORCE RLS) plus
// schema migration runners.
//
// DAG Boundary (Tier 1–2): May import only internal/pkg (and storage/port from
// concrete drivers); must never import security, config, domain services, ingress, or tools/spikes.
package storage
