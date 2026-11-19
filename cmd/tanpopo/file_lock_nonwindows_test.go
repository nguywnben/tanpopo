// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !windows

package main

func fileLockOwners(string) (string, error) {
	return "not available on this operating system", nil
}
