// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !linux && !darwin && !windows

package backup

import (
	"errors"
	"os"
)

func renameRootDirectoryNoReplace(*os.Root, string, string) error {
	return errors.New("backup: atomic no-replace directory publication is unsupported on this operating system")
}
