// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package bootstrap

import "os"

func verifyOwnerMatch(os.FileInfo) error {
	return nil
}
