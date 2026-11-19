// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"testing"
)

func TestConfiguredMarketplaceKeyringAndGuardedFeedClient(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(publicKey)
	keys, err := configuredMarketplaceKeyring(fmt.Sprintf(`{"release-2026":%q}`, encoded))
	if err != nil {
		t.Fatal(err)
	}
	if got := keys["release-2026"]; len(got) != ed25519.PublicKeySize {
		t.Fatalf("decoded public key length = %d", len(got))
	}
	for _, raw := range []string{
		`{"bad":"not-base64"}`,
		`{"bad":"` + base64.StdEncoding.EncodeToString([]byte("short")) + `"}`,
		`{"ok":"` + encoded + `"} {}`,
	} {
		if _, err := configuredMarketplaceKeyring(raw); err == nil {
			t.Errorf("invalid public key configuration accepted: %q", raw)
		}
	}
	if _, err := guardedFeedClient("http://feeds.example.test/index.json"); err == nil {
		t.Fatal("plain HTTP feed URL accepted")
	}
	if _, err := guardedFeedClient("https://user:password@feeds.example.test/index.json"); err == nil {
		t.Fatal("credential-bearing feed URL accepted")
	}
	if client, err := guardedFeedClient("https://feeds.example.test/index.json"); err != nil || client == nil || client.Transport == nil {
		t.Fatalf("valid feed URL did not get guarded client: client=%v err=%v", client, err)
	}
}
