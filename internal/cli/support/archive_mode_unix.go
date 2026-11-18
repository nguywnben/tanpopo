// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !windows

package support

import "os"

func createArchiveTemp(directory string) (*os.File, error) {
	return os.CreateTemp(directory, ".tanpopo-support-*.tmp")
}

func secureArchiveFile(file *os.File) error { return file.Chmod(0o600) }
