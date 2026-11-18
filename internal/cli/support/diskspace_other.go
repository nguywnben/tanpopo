// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package support

import "errors"

func diskFreeBytes(string) (uint64, error) { return 0, errors.New("filesystem capacity probe is unsupported") }
