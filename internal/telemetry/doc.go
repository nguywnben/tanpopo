// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package telemetry implements privacy-preserving Metadata-Only tracing, bounded
// non-blocking OTLP ring-buffer export, and cardinality-bounded Prometheus metrics.
//
// DAG Boundary (Tier 5): May import tiers <= 4; must never import ingress,
// admin, webui, ops, cli, or tools/spikes.
package telemetry
