// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package sdk embeds Tanpopo's canonical v1 extension JSON Schemas and starter
// templates (Tier-1 Declarative and Tier-2 Sandboxed Wasm) so the `tanpopo plugin`
// toolchain operates 100% offline from a single binary (TP-IMP-0014).
package sdk

import "embed"

// EmbeddedAssets contains plugin-schema/v1/*.json and starter templates under templates/.
//
//go:embed all:plugin-schema all:templates
var EmbeddedAssets embed.FS
