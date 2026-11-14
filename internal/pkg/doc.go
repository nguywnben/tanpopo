// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package pkg holds shared pure domain primitives, deterministic ID generators,
// canonical errors, and zero-dependency utilities at the root of the internal DAG.
//
// DAG Boundary (Tier 0): May depend only on the Go standard library; must never
// import any sibling internal/* package or tools/spikes.
package pkg
