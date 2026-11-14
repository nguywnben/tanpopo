// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package webui serves the static Astryx Web Console SPA compiled from
// web/console/dist and embedded into the single tanpopo binary via embed.FS.
//
// DAG Boundary (Tier 6): May import tiers <= 5 for CSP / header wiring; must
// never import cli or cmd/tanpopo or tools/spikes.
package webui
