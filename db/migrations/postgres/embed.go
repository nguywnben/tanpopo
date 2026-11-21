// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package postgresmigrations embeds canonical PostgreSQL 16+ Expand-Only DDL migrations.
package postgresmigrations

import "embed"

// FS embeds all versioned PostgreSQL migration files (*.sql).
//
//go:embed *.sql
var FS embed.FS
