// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/nguywnben/tanpopo/internal/extension/builtin"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
)

func main() {
	outPath := flag.String("out", "", "path to write ephemeral release-signatures.json")
	flag.Parse()
	if *outPath == "" {
		fmt.Fprintln(os.Stderr, "--out is required")
		os.Exit(2)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate key: %v\n", err)
		os.Exit(1)
	}
	cat, err := builtin.NewCatalog()
	if err != nil {
		fmt.Fprintf(os.Stderr, "builtin catalog: %v\n", err)
		os.Exit(1)
	}
	sigs := make([]builtin.ReleaseSignatureRecord, 0, len(cat.Packages()))
	for _, pkg := range cat.Packages() {
		sig := ed25519.Sign(priv, installer.SignatureSigningPayload(pkg.PackageID, pkg.Version, pkg.ArchiveSHA256))
		sigs = append(sigs, builtin.ReleaseSignatureRecord{
			PackageID:     pkg.PackageID,
			Version:       pkg.Version,
			ArchiveSHA256: pkg.ArchiveSHA256,
			RootSignature: base64.StdEncoding.EncodeToString(sig),
		})
	}
	raw, err := json.MarshalIndent(map[string]any{"signatures": sigs}, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outPath, raw, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(hex.EncodeToString(pub))
}
