// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SQLStore struct {
	db  *sql.DB
	now func() time.Time
}

func NewSQLStore(db *sql.DB, now func() time.Time) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("registry: database is required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SQLStore{db: db, now: now}, nil
}

func (s *SQLStore) LastRevocationSerial(ctx context.Context) (int64, error) {
	return s.lastSerial(ctx, FeedRevocation)
}

func (s *SQLStore) LastIndexSerial(ctx context.Context) (int64, error) {
	return s.lastSerial(ctx, FeedIndex)
}

func (s *SQLStore) lastSerial(ctx context.Context, kind FeedKind) (int64, error) {
	var serial int64
	err := s.db.QueryRowContext(ctx, `SELECT version_serial FROM marketplace_feed_cursors WHERE feed_kind = ?`, string(kind)).Scan(&serial)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return serial, err
}

func (s *SQLStore) ApplyIndexFeed(ctx context.Context, serial int64, expiresAt time.Time, entries []IndexEntry) error {
	if serial <= 0 || !expiresAt.After(s.now()) {
		return fmt.Errorf("%w: invalid index serial or expiry", ErrInvalidFeed)
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.PackageSlug) == "" || strings.TrimSpace(entry.Version) == "" || decodeDigest(entry.Digest) != nil || !validOCIURI(entry.OCIURI, entry.Digest) {
			return fmt.Errorf("%w: invalid index entry", ErrInvalidFeed)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := advanceCursor(ctx, tx, FeedIndex, serial, expiresAt, s.now()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM marketplace_index_entries`); err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_index_entries (package_slug, version, digest, oci_uri, updated_at_ms) VALUES (?, ?, ?, ?, ?)`, entry.PackageSlug, entry.Version, entry.Digest, entry.OCIURI, s.now().UnixMilli()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLStore) CachedIndex(ctx context.Context) ([]IndexEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT package_slug, version, digest, oci_uri FROM marketplace_index_entries ORDER BY package_slug, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []IndexEntry
	for rows.Next() {
		var entry IndexEntry
		if err := rows.Scan(&entry.PackageSlug, &entry.Version, &entry.Digest, &entry.OCIURI); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *SQLStore) ApplyRevocationFeed(ctx context.Context, serial int64, expiresAt time.Time, entries []RevocationEntry) error {
	if serial <= 0 || !expiresAt.After(s.now()) {
		return fmt.Errorf("%w: invalid revocation serial or expiry", ErrInvalidFeed)
	}
	if _, err := DecodeRevocationEntries(&VerifiedFeed{Kind: FeedRevocation, Entries: marshalEntries(entries)}); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := advanceCursor(ctx, tx, FeedRevocation, serial, expiresAt, s.now()); err != nil {
		return err
	}
	for _, entry := range entries {
		packageSlug := canonicalPackageSlug(entry.PackageSlug)
		selector := canonicalRevocationSelector(entry.VersionOrSHA256)
		_, err := tx.ExecContext(ctx, `INSERT INTO extension_revocations (id, package_slug, version_or_sha256, severity, version_serial, reason, feed_expires_at_ms, imported_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(package_slug, version_or_sha256) DO UPDATE SET severity = excluded.severity, version_serial = excluded.version_serial, reason = excluded.reason, feed_expires_at_ms = excluded.feed_expires_at_ms, imported_at_ms = excluded.imported_at_ms
			WHERE excluded.version_serial > extension_revocations.version_serial`,
			revocationID(packageSlug, selector), packageSlug, selector, entry.Severity, serial, entry.Reason, expiresAt.UnixMilli(), s.now().UnixMilli())
		if err != nil {
			return err
		}
		if entry.Severity != "CRITICAL" && entry.Severity != "CRITICAL_MALICIOUS" {
			continue
		}
		rows, err := tx.QueryContext(ctx, `SELECT ei.id
			FROM extension_installations ei
			JOIN extension_packages ep ON ep.id = ei.package_id
			JOIN extension_versions ev ON ev.id = ei.active_version_id
			WHERE ep.canonical_name = ?
			  AND ei.state NOT IN ('UNINSTALLED', 'QUARANTINED')
			  AND (ev.semver = ? OR lower(ev.bundle_sha256) = lower(?))`,
			packageSlug, selector, selector)
		if err != nil {
			return err
		}
		var installationIDs []string
		for rows.Next() {
			var installationID string
			if err := rows.Scan(&installationID); err != nil {
				rows.Close()
				return err
			}
			installationIDs = append(installationIDs, installationID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, installationID := range installationIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO extension_revocation_actions (id, installation_id, package_slug, version_or_sha256, severity, reason, created_at_ms)
				VALUES (?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(installation_id, package_slug, version_or_sha256) DO UPDATE SET severity = excluded.severity, reason = excluded.reason
				WHERE extension_revocation_actions.completed_at_ms IS NULL`,
					revocationActionID(installationID, packageSlug, selector), installationID, packageSlug, selector, entry.Severity, entry.Reason, s.now().UnixMilli()); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *SQLStore) PendingRevocationActions(ctx context.Context) ([]RevocationTarget, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, installation_id, package_slug, version_or_sha256, severity, reason
		FROM extension_revocation_actions WHERE completed_at_ms IS NULL ORDER BY created_at_ms, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []RevocationTarget
	for rows.Next() {
		var target RevocationTarget
		if err := rows.Scan(&target.ActionID, &target.InstallationID, &target.PackageSlug, &target.VersionOrSHA256, &target.Severity, &target.Reason); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (s *SQLStore) CompleteRevocationAction(ctx context.Context, actionID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE extension_revocation_actions SET completed_at_ms = ? WHERE id = ? AND completed_at_ms IS NULL`, s.now().UnixMilli(), actionID)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil {
		return err
	} else if count == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM extension_revocation_actions WHERE id = ? AND completed_at_ms IS NOT NULL`, actionID).Scan(&exists); err != nil {
			return err
		}
	}
	return nil
}

func advanceCursor(ctx context.Context, tx *sql.Tx, kind FeedKind, serial int64, expiresAt, now time.Time) error {
	var last int64
	err := tx.QueryRowContext(ctx, `SELECT version_serial FROM marketplace_feed_cursors WHERE feed_kind = ?`, string(kind)).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && serial <= last {
		return fmt.Errorf("%w: got %d after %d", ErrFeedRollback, serial, last)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO marketplace_feed_cursors (feed_kind, version_serial, feed_expires_at_ms, updated_at_ms) VALUES (?, ?, ?, ?)
		ON CONFLICT(feed_kind) DO UPDATE SET version_serial = excluded.version_serial, feed_expires_at_ms = excluded.feed_expires_at_ms, updated_at_ms = excluded.updated_at_ms
		WHERE excluded.version_serial > marketplace_feed_cursors.version_serial`, string(kind), serial, expiresAt.UnixMilli(), now.UnixMilli())
	return err
}

func canonicalPackageSlug(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && !strings.HasPrefix(value, "@") {
		return "@" + value
	}
	return value
}

func canonicalRevocationSelector(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "sha256:") {
		value = value[len("sha256:"):]
	}
	if len(value) == 64 && decodeDigest(strings.ToLower(value)) == nil {
		return strings.ToLower(value)
	}
	return value
}

func revocationID(packageSlug, version string) string {
	return "rev_" + stableID(packageSlug+"\x00"+version)
}

func revocationActionID(installationID, packageSlug, version string) string {
	return "rva_" + stableID(installationID+"\x00"+packageSlug+"\x00"+version)
}

func stableID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:13])
}

func marshalEntries(entries []RevocationEntry) []byte {
	// The structures only contain JSON strings; marshal keeps SQLStore validation
	// aligned with the public feed decoder without retaining caller-owned slices.
	encoded, _ := json.Marshal(entries)
	return encoded
}
