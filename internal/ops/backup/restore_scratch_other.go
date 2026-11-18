// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !linux

package backup

import "os"

func restoreScratchBase() (string, error) { return os.TempDir(), nil }
