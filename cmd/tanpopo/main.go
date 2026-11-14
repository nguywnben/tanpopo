// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package main provides the single canonical Tanpopo CLI and server entrypoint (`tanpopo`).
package main

import (
	"fmt"
	"io"
	"os"
)

// Build metadata populated via linker -ldflags during release builds.
var (
	version = "0.1.0-dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}

	switch args[0] {
	case "--version", "-v", "version":
		fmt.Fprintf(stdout, "tanpopo version %s (commit=%s built=%s)\n", version, commit, date)
		return 0
	case "--help", "-h", "help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "tanpopo: unknown command or flag %q (control/data plane commands unlock in M1-M2; run `tanpopo --help`)\n", args[0])
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "tanpopo - Open-source, self-hosted-first, extension-first AI Gateway")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  tanpopo [--version | --help]")
}
