// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package iam implements local/OIDC principal authentication, canonical 6-role RBAC
// permission matrices, and SHA-256 Virtual Key lifecycle management (SM-05).
//
// DAG Boundary (Tier 5): May import tiers <= 4 (pkg, storage, security, config);
// must never import ingress, admin, webui, ops, cli, or tools/spikes.
package iam
