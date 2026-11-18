// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build !linux

package backup

import (
	"errors"
	"os"
)

func openStagingRoot(*os.Root) (*os.Root, func() error, error) {
	return nil, nil, errors.New("backup: volatile staging filesystem is unsupported")
}
