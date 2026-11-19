// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type memoryIndexStore struct {
	serial  int64
	entries []IndexEntry
}

func (s *memoryIndexStore) LastIndexSerial(context.Context) (int64, error) { return s.serial, nil }
func (s *memoryIndexStore) ApplyIndexFeed(_ context.Context, serial int64, _ time.Time, entries []IndexEntry) error {
	s.serial = serial
	s.entries = append([]IndexEntry(nil), entries...)
	return nil
}
func (s *memoryIndexStore) CachedIndex(context.Context) ([]IndexEntry, error) {
	return append([]IndexEntry(nil), s.entries...), nil
}

func TestIndexSyncerPersistsVerifiedIndexAndServesCacheOffline(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	digest := strings.Repeat("a", 64)
	entryJSON := `[{"package_slug":"community/adapter","version":"1.2.3","digest":"` + digest + `","oci_uri":"oci://registry.example.com/community/adapter@sha256:` + digest + `"}]`
	rawFeed := signedEnvelope(t, FeedIndex, 1, now.Add(time.Hour), entryJSON, "root", privateKey)
	client := &http.Client{Transport: registryRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://index.example.com/index.json" {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(rawFeed))), Request: req}, nil
		}
		return nil, errors.New("offline")
	})}
	store := &memoryIndexStore{}
	syncer := &IndexSyncer{Client: client, URL: "https://index.example.com/index.json", Keys: Keyring{"root": publicKey}, Store: store, Now: func() time.Time { return now }}

	fresh, err := syncer.Sync(context.Background())
	if err != nil || fresh.FromCache || fresh.SyncError != nil || len(fresh.Entries) != 1 || store.serial != 1 {
		t.Fatalf("fresh verified index result=%+v serial=%d err=%v", fresh, store.serial, err)
	}
	syncer.URL = "https://index.example.com/unavailable.json"
	offline, err := syncer.Sync(context.Background())
	if err != nil || !offline.FromCache || offline.SyncError == nil || len(offline.Entries) != 1 || offline.Entries[0].Digest != digest {
		t.Fatalf("offline cache result=%+v err=%v", offline, err)
	}
}
