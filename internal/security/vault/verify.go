// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vault

import (
	"context"
	"fmt"
)

// VerifyAllDEKEnvelopes authenticates every persisted organization and project
// DEK envelope without creating, rotating, or otherwise modifying key rows.
func (s *Store) VerifyAllDEKEnvelopes(ctx context.Context) error {
	if s == nil || s.db == nil || s.kek == nil {
		return fmt.Errorf("vault: store, database, and KEK provider are required")
	}
	orgRows, err := s.db.QueryContext(ctx, `SELECT org_id FROM org_encryption_keys ORDER BY org_id`)
	if err != nil {
		return fmt.Errorf("vault: list organization DEK envelopes: %w", err)
	}
	var orgIDs []string
	for orgRows.Next() {
		var orgID string
		if err := orgRows.Scan(&orgID); err != nil {
			_ = orgRows.Close()
			return fmt.Errorf("vault: read organization DEK envelope key: %w", err)
		}
		orgIDs = append(orgIDs, orgID)
	}
	if err := orgRows.Err(); err != nil {
		_ = orgRows.Close()
		return fmt.Errorf("vault: iterate organization DEK envelopes: %w", err)
	}
	if err := orgRows.Close(); err != nil {
		return fmt.Errorf("vault: close organization DEK query: %w", err)
	}
	for _, orgID := range orgIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, dek, err := s.loadOrgDEKReadOnly(ctx, orgID); err != nil {
			return fmt.Errorf("vault: verify organization %q DEK envelope: %w", orgID, err)
		} else {
			clear(dek)
		}
		projectRows, err := s.db.QueryContext(ctx, `SELECT project_id FROM project_encryption_keys WHERE org_id = ? ORDER BY project_id`, orgID)
		if err != nil {
			return fmt.Errorf("vault: list project DEK envelopes for organization %q: %w", orgID, err)
		}
		var projectIDs []string
		for projectRows.Next() {
			var projectID string
			if err := projectRows.Scan(&projectID); err != nil {
				_ = projectRows.Close()
				return fmt.Errorf("vault: read project DEK envelope key: %w", err)
			}
			projectIDs = append(projectIDs, projectID)
		}
		if err := projectRows.Err(); err != nil {
			_ = projectRows.Close()
			return fmt.Errorf("vault: iterate project DEK envelopes: %w", err)
		}
		if err := projectRows.Close(); err != nil {
			return fmt.Errorf("vault: close project DEK query: %w", err)
		}
		for _, projectID := range projectIDs {
			if _, _, dek, err := s.loadProjectDEKReadOnly(ctx, orgID, projectID); err != nil {
				return fmt.Errorf("vault: verify project %q DEK envelope for organization %q: %w", projectID, orgID, err)
			} else {
				clear(dek)
			}
		}
	}
	s.EvictAllDEKs()
	return nil
}
