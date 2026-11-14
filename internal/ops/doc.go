// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package ops implements operational SRE workflows: health/readiness probes,
// tanpopo doctor, GitOps lockfile sync, encrypted .tpop.enc backup/restore,
// Crypto-Shredding erasure, upgrade preflight, and SQLite->Postgres migration.
//
// DAG Boundary (Tier 6): May import tiers <= 5; must never import cli or cmd.
package ops
