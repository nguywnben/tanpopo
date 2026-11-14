// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package finops implements the integer BIGINT micro-USD two-phase reservation
// ledger (SM-07), Negative Balance Carryover, OrphanReservationReaper, and
// EXACT / ESTIMATED / UNMETERED_FLAGGED billing settlements.
//
// DAG Boundary (Tier 5): May import tiers <= 4; must never import ingress,
// admin, webui, ops, cli, or tools/spikes.
package finops
