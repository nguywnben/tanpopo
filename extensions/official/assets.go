// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package official

import (
	"embed"
	"io/fs"
)

//go:embed */manifest.yaml */README.md */dist/*.wasm
var officialFS embed.FS

var catalogSlugs = []string{
	"openai",
	"openai-compatible",
	"ollama",
	"anthropic",
	"gemini",
	"azure-openai",
	"aws-bedrock",
	"cohere-rerank",
}

// FS returns the embedded read-only filesystem containing the built-in official
// adapter packages (manifest.yaml, README.md, and compiled Wasm entrypoints).
func FS() fs.FS {
	return officialFS
}

// CatalogSlugs returns the ordered list of built-in official adapter directories
// embedded inside FS().
func CatalogSlugs() []string {
	out := make([]string, len(catalogSlugs))
	copy(out, catalogSlugs)
	return out
}
