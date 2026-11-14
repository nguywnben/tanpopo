// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package config manages configuration loading and lock-free immutable
// Copy-on-Write ConfigSnapshot publication via atomic.Pointer for the Data Plane.
//
// DAG Boundary (Tier 4): May import internal/pkg, internal/storage/*, and
// internal/security/*; must never import hot-path domain engines or ingress.
package config
