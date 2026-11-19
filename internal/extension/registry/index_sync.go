// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// IndexStore persists a verified index atomically with its anti-rollback cursor.
type IndexStore interface {
	LastIndexSerial(context.Context) (int64, error)
	ApplyIndexFeed(context.Context, int64, time.Time, []IndexEntry) error
	CachedIndex(context.Context) ([]IndexEntry, error)
}

// IndexSnapshot reports whether the returned entries came from a fresh signed
// feed or the durable offline cache. SyncError remains visible when the cache
// is served so operators can distinguish healthy sync from degraded service.
type IndexSnapshot struct {
	Entries   []IndexEntry
	FromCache bool
	SyncError error
}

type IndexSyncer struct {
	URL            string
	Keys           Keyring
	Store          IndexStore
	Now            func() time.Time
	clientForURL   func(string) (*http.Client, error)
}

// Sync verifies and persists a fresh index when reachable. On network or feed
// verification failure it returns the last durable cache and the failure for
// observability, preserving offline marketplace availability.
func (s *IndexSyncer) Sync(ctx context.Context) (IndexSnapshot, error) {
	if s == nil || s.Store == nil || s.Now == nil {
		return IndexSnapshot{}, fmt.Errorf("%w: index sync is not configured", ErrInvalidFeed)
	}
	clientForURL := s.clientForURL
	if clientForURL == nil {
		clientForURL = guardedFeedClient
	}
	client, syncErr := clientForURL(s.URL)
	if syncErr != nil {
		return s.cachedAfterSyncFailure(ctx, syncErr)
	}
	raw, syncErr := fetchSignedFeed(ctx, client, s.URL)
	if syncErr == nil {
		var serial int64
		serial, syncErr = s.Store.LastIndexSerial(ctx)
		if syncErr == nil {
			var feed *VerifiedFeed
			feed, syncErr = VerifyFeed(FeedIndex, raw, s.Keys, serial, s.Now().UTC())
			if syncErr == nil {
				var entries []IndexEntry
				entries, syncErr = DecodeIndexEntries(feed)
				if syncErr == nil {
					syncErr = s.Store.ApplyIndexFeed(ctx, feed.VersionSerial, feed.ExpiresAt, entries)
					if syncErr == nil {
						return IndexSnapshot{Entries: entries}, nil
					}
				}
			}
		}
	}
	return s.cachedAfterSyncFailure(ctx, syncErr)
}

func (s *IndexSyncer) cachedAfterSyncFailure(ctx context.Context, syncErr error) (IndexSnapshot, error) {
	cached, cacheErr := s.Store.CachedIndex(ctx)
	if cacheErr != nil {
		return IndexSnapshot{}, errors.Join(fmt.Errorf("marketplace index sync: %w", syncErr), fmt.Errorf("read cached marketplace index: %w", cacheErr))
	}
	if len(cached) == 0 {
		return IndexSnapshot{}, fmt.Errorf("marketplace index has no verified cache: %w", syncErr)
	}
	return IndexSnapshot{Entries: cached, FromCache: true, SyncError: syncErr}, nil
}
