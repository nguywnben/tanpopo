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
	}
	// Enumerate project envelopes independently so a malformed/orphaned row
	// cannot hide behind an organization-key-driven nested scan.
	projectRows, err := s.db.QueryContext(ctx, `SELECT org_id, project_id FROM project_encryption_keys ORDER BY org_id, project_id`)
	if err != nil {
		return fmt.Errorf("vault: list project DEK envelopes: %w", err)
	}
	type projectKey struct{ orgID, projectID string }
	var projects []projectKey
	for projectRows.Next() {
		var project projectKey
		if err := projectRows.Scan(&project.orgID, &project.projectID); err != nil {
			_ = projectRows.Close()
			return fmt.Errorf("vault: read project DEK envelope key: %w", err)
		}
		projects = append(projects, project)
	}
	if err := projectRows.Err(); err != nil {
		_ = projectRows.Close()
		return fmt.Errorf("vault: iterate project DEK envelopes: %w", err)
	}
	if err := projectRows.Close(); err != nil {
		return fmt.Errorf("vault: close project DEK query: %w", err)
	}
	for _, project := range projects {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, _, dek, err := s.loadProjectDEKReadOnly(ctx, project.orgID, project.projectID); err != nil {
			return fmt.Errorf("vault: verify project %q DEK envelope for organization %q: %w", project.projectID, project.orgID, err)
		} else {
			clear(dek)
		}
	}
	s.EvictAllDEKs()
	return nil
}
