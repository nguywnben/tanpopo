// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package gitops

// Windows does not support FlushFileBuffers on directory handles. The config
// file itself is flushed before the atomic same-volume rename.
func syncDirectory(string) error { return nil }
