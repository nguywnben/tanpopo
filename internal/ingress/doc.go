// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package ingress implements hot-path Data Plane traffic planes: Unified LLM API
// (/v1/*), SSE streaming, native passthrough (/native/{dialect}/*), idempotency,
// out-of-band BlobRef handling, MCP Hub (/mcp), and A2A Plane (/a2a/*).
//
// DAG Boundary (Tier 6): May import domain engines (tiers <= 5); must never
// query relational configuration tables directly on the hot path or import cli/cmd.
package ingress
