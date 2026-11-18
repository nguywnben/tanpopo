// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build linux

package backup

func restoreScratchBase() (string, error) {
	if err := verifyLinuxVolatileStaging(); err != nil {
		return "", err
	}
	return "/dev/shm", nil
}
