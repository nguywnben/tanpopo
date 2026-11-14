// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !linux && !darwin && !windows

package sqlite

func detectRemoteFilesystem(string) (string, error) {
	return "", nil
}
