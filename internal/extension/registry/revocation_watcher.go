// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var ErrRevocationWatcherUnavailable = errors.New("registry: revocation watcher is not configured")

type RevocationTarget struct {
	ActionID        string
	InstallationID  string
	PackageSlug     string
	VersionOrSHA256 string
	Severity        string
	Reason          string
}

// RevocationStore must atomically persist the new feed serial and entries,
// enqueue any matching installed critical revocations, and expose those rows
// until CompleteRevocationAction succeeds. This durable queue makes restart
// retries safe after a partial quarantine/stream-fence/alert operation.
type RevocationStore interface {
	LastRevocationSerial(context.Context) (int64, error)
	ApplyRevocationFeed(context.Context, int64, time.Time, []RevocationEntry) error
	PendingRevocationActions(context.Context) ([]RevocationTarget, error)
	CompleteRevocationAction(context.Context, string) error
}

type RevocationActions interface {
	QuarantineInstallation(context.Context, RevocationTarget) error
	SeverInstallationStreams(context.Context, RevocationTarget) error
	EmitINC03(context.Context, RevocationTarget) error
}

type RevocationWatcher struct {
	URL          string
	Keys         Keyring
	Store        RevocationStore
	Actions      RevocationActions
	Now          func() time.Time
	clientForURL func(string) (*http.Client, error)
	mu           sync.Mutex
}

// Poll fetches and verifies the configured signed feed through an exact-host
// SSRF-guarded client, then retries all durable pending enforcement actions.
func (w *RevocationWatcher) Poll(ctx context.Context) error {
	if w == nil || w.Store == nil || w.Actions == nil || w.Now == nil {
		return ErrRevocationWatcherUnavailable
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	clientForURL := w.clientForURL
	if clientForURL == nil {
		clientForURL = guardedFeedClient
	}
	client, err := clientForURL(w.URL)
	if err != nil {
		return errors.Join(err, w.reconcileLocked(ctx))
	}
	raw, err := fetchSignedFeed(ctx, client, w.URL)
	if err != nil {
		return errors.Join(err, w.reconcileLocked(ctx))
	}
	serial, err := w.Store.LastRevocationSerial(ctx)
	if err != nil {
		return errors.Join(fmt.Errorf("load revocation feed serial: %w", err), w.reconcileLocked(ctx))
	}
	feed, err := VerifyFeed(FeedRevocation, raw, w.Keys, serial, w.Now().UTC())
	if err != nil {
		return errors.Join(err, w.reconcileLocked(ctx))
	}
	entries, err := DecodeRevocationEntries(feed)
	if err != nil {
		return errors.Join(err, w.reconcileLocked(ctx))
	}
	if err := w.Store.ApplyRevocationFeed(ctx, feed.VersionSerial, feed.ExpiresAt, entries); err != nil {
		return errors.Join(fmt.Errorf("persist verified revocation feed: %w", err), w.reconcileLocked(ctx))
	}
	return w.reconcileLocked(ctx)
}

// ImportVerifiedBytes supports offline/air-gapped feed import using the same
// signature, expiry, anti-rollback, persistence, and enforcement path as Poll.
func (w *RevocationWatcher) ImportVerifiedBytes(ctx context.Context, raw []byte) error {
	if w == nil || w.Store == nil || w.Actions == nil || w.Now == nil {
		return ErrRevocationWatcherUnavailable
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.importLocked(ctx, raw)
}

func (w *RevocationWatcher) importLocked(ctx context.Context, raw []byte) error {
	serial, err := w.Store.LastRevocationSerial(ctx)
	if err != nil {
		return fmt.Errorf("load revocation feed serial: %w", err)
	}
	feed, err := VerifyFeed(FeedRevocation, raw, w.Keys, serial, w.Now().UTC())
	if err != nil {
		return err
	}
	entries, err := DecodeRevocationEntries(feed)
	if err != nil {
		return err
	}
	if err := w.Store.ApplyRevocationFeed(ctx, feed.VersionSerial, feed.ExpiresAt, entries); err != nil {
		return fmt.Errorf("persist verified revocation feed: %w", err)
	}
	return w.reconcileLocked(ctx)
}

// Reconcile retries the persisted enforcement queue. Each operation must be
// idempotent; actions are marked complete only after quarantine, stream sever,
// and the INC-03 alert all succeed.
func (w *RevocationWatcher) Reconcile(ctx context.Context) error {
	if w == nil || w.Store == nil || w.Actions == nil {
		return ErrRevocationWatcherUnavailable
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reconcileLocked(ctx)
}

func (w *RevocationWatcher) reconcileLocked(ctx context.Context) error {
	pending, err := w.Store.PendingRevocationActions(ctx)
	if err != nil {
		return fmt.Errorf("load pending revocation actions: %w", err)
	}
	var actionErrors []error
	for _, target := range pending {
		if target.Severity != "CRITICAL" && target.Severity != "CRITICAL_MALICIOUS" {
			continue
		}
		if err := w.Actions.QuarantineInstallation(ctx, target); err != nil {
			actionErrors = append(actionErrors, fmt.Errorf("quarantine revoked installation %s: %w", target.InstallationID, err))
			continue
		}
		if err := w.Actions.SeverInstallationStreams(ctx, target); err != nil {
			actionErrors = append(actionErrors, fmt.Errorf("sever revoked installation streams %s: %w", target.InstallationID, err))
			continue
		}
		if err := w.Actions.EmitINC03(ctx, target); err != nil {
			actionErrors = append(actionErrors, fmt.Errorf("emit INC-03 for installation %s: %w", target.InstallationID, err))
			continue
		}
		if err := w.Store.CompleteRevocationAction(ctx, target.ActionID); err != nil {
			actionErrors = append(actionErrors, fmt.Errorf("complete revocation action %s: %w", target.ActionID, err))
		}
	}
	return errors.Join(actionErrors...)
}
