// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package sqlitemigrations embeds canonical SQLite WAL Expand-Only DDL migrations.
package sqlitemigrations

import "embed"

// FS embeds all versioned SQLite migration files (*.sql).
//
//go:embed *.sql
var FS embed.FS
