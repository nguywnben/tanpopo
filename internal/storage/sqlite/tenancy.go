// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type tenancyRepo struct {
	store *Store // non-nil on root Store; nil inside an explicit sqliteTx
	exec  sqlExecQuery
	nowMs func() int64
}

func (r *tenancyRepo) withWriteTx(ctx context.Context, fn func(exec sqlExecQuery) error) error {
	if r.store == nil {
		return fn(r.exec)
	}
	return r.store.InTx(ctx, func(tx port.Tx) error {
		stx := tx.(*sqliteTx)
		return fn(stx.tx)
	})
}

func appendChangelogSQL(ctx context.Context, exec sqlExecQuery, orgID *string, entityKind, entityID, mutationType string, emergency bool, now int64) (int64, error) {
	emInt := 0
	if emergency {
		emInt = 1
	}
	res, err := exec.ExecContext(ctx,
		`INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, ?, ?, ?, ?, ?)`,
		nullStringPtr(orgID), entityKind, entityID, mutationType, emInt, now,
	)
	if err != nil {
		return 0, mapSQLiteError(err)
	}
	epoch, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("sqlite: read config_epoch: %w", err)
	}
	return epoch, nil
}

func validateCallerID(id, expectedPrefix string) error {
	if strings.ContainsAny(id, " \t\r\n\x00") || len(id) > 40 || !strings.HasPrefix(id, expectedPrefix+"_") || len(id) <= len(expectedPrefix)+1 {
		return fmt.Errorf("%w: caller-supplied id %q must use prefix %q_ and length <= 40 without whitespace", port.ErrInvalidInput, id, expectedPrefix)
	}
	return nil
}

func (r *tenancyRepo) CreateOrganization(ctx context.Context, params port.CreateOrgParams) (*port.Organization, error) {
	if strings.TrimSpace(params.Slug) == "" || strings.TrimSpace(params.DisplayName) == "" {
		return nil, fmt.Errorf("%w: slug and display_name are required", port.ErrInvalidInput)
	}
	id := params.ID
	if id == "" {
		var err error
		id, err = idgen.New(idgen.PrefixOrg)
		if err != nil {
			return nil, err
		}
	} else if err := validateCallerID(id, idgen.PrefixOrg); err != nil {
		return nil, err
	}
	minTrust := params.MinPluginTrust
	if minTrust == "" {
		minTrust = port.MinTrustAnyApproved
	}
	now := r.nowMs()
	fourEyes := 0
	if params.RequireFourEyesApproval {
		fourEyes = 1
	}

	err := r.withWriteTx(ctx, func(exec sqlExecQuery) error {
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO organizations (
				id, slug, display_name, status, default_data_region,
				min_plugin_trust, require_four_eyes_approval, version, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
			id, params.Slug, params.DisplayName, port.OrgStatusActive,
			nullStringPtr(params.DefaultDataRegion), minTrust, fourEyes, now, now,
		); err != nil {
			return mapSQLiteError(err)
		}
		_, err := appendChangelogSQL(ctx, exec, &id, "organization", id, port.MutationUpsert, false, now)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &port.Organization{
		ID:                      id,
		Slug:                    params.Slug,
		DisplayName:             params.DisplayName,
		Status:                  port.OrgStatusActive,
		DefaultDataRegion:       cloneStringPtr(params.DefaultDataRegion),
		MinPluginTrust:          minTrust,
		RequireFourEyesApproval: params.RequireFourEyesApproval,
		Version:                 1,
		CreatedAtMs:             now,
		UpdatedAtMs:             now,
	}, nil
}

func (r *tenancyRepo) GetOrganization(ctx context.Context, orgID string) (*port.Organization, error) {
	if strings.TrimSpace(orgID) == "" {
		return nil, fmt.Errorf("%w: org_id required", port.ErrInvalidInput)
	}
	return scanOrganization(r.exec.QueryRowContext(ctx, `
		SELECT id, slug, display_name, status, default_data_region,
		       min_plugin_trust, require_four_eyes_approval, version, created_at_ms, updated_at_ms
		FROM organizations WHERE id = ?`, orgID))
}

func (r *tenancyRepo) GetOrganizationBySlug(ctx context.Context, slug string) (*port.Organization, error) {
	if strings.TrimSpace(slug) == "" {
		return nil, fmt.Errorf("%w: slug required", port.ErrInvalidInput)
	}
	return scanOrganization(r.exec.QueryRowContext(ctx, `
		SELECT id, slug, display_name, status, default_data_region,
		       min_plugin_trust, require_four_eyes_approval, version, created_at_ms, updated_at_ms
		FROM organizations WHERE slug = ?`, slug))
}

func (r *tenancyRepo) ListOrganizations(ctx context.Context, limit int) ([]port.Organization, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.exec.QueryContext(ctx, `
		SELECT id, slug, display_name, status, default_data_region,
		       min_plugin_trust, require_four_eyes_approval, version, created_at_ms, updated_at_ms
		FROM organizations ORDER BY id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	var out []port.Organization
	for rows.Next() {
		org, err := scanOrganization(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *org)
	}
	return out, rows.Err()
}

func (r *tenancyRepo) UpdateOrganization(ctx context.Context, orgID string, expectedVersion int64, params port.UpdateOrgParams) (*port.Organization, error) {
	if strings.TrimSpace(orgID) == "" || expectedVersion < 1 {
		return nil, fmt.Errorf("%w: org_id and expected_version >= 1 required", port.ErrInvalidInput)
	}
	if params.DisplayName != nil && strings.TrimSpace(*params.DisplayName) == "" {
		return nil, fmt.Errorf("%w: display_name cannot be blank", port.ErrInvalidInput)
	}
	now := r.nowMs()
	var updated *port.Organization

	err := r.withWriteTx(ctx, func(exec sqlExecQuery) error {
		cur, err := scanOrganization(exec.QueryRowContext(ctx, `
			SELECT id, slug, display_name, status, default_data_region,
			       min_plugin_trust, require_four_eyes_approval, version, created_at_ms, updated_at_ms
			FROM organizations WHERE id = ?`, orgID))
		if err != nil {
			return err
		}
		if cur.Version != expectedVersion {
			return port.ErrVersionConflict
		}

		nextName := cur.DisplayName
		if params.DisplayName != nil {
			nextName = *params.DisplayName
		}
		nextStatus := cur.Status
		if params.Status != nil {
			nextStatus = *params.Status
		}
		nextRegion := cur.DefaultDataRegion
		if params.ClearDefaultDataRegion {
			nextRegion = nil
		} else if params.DefaultDataRegion != nil {
			nextRegion = cloneStringPtr(params.DefaultDataRegion)
		}
		nextTrust := cur.MinPluginTrust
		if params.MinPluginTrust != nil {
			nextTrust = *params.MinPluginTrust
		}
		nextFourEyes := cur.RequireFourEyesApproval
		if params.RequireFourEyesApproval != nil {
			nextFourEyes = *params.RequireFourEyesApproval
		}
		fourEyesInt := 0
		if nextFourEyes {
			fourEyesInt = 1
		}

		res, err := exec.ExecContext(ctx, `
			UPDATE organizations
			SET display_name = ?, status = ?, default_data_region = ?,
			    min_plugin_trust = ?, require_four_eyes_approval = ?,
			    version = version + 1, updated_at_ms = ?
			WHERE id = ? AND version = ?`,
			nextName, nextStatus, nullStringPtr(nextRegion), nextTrust, fourEyesInt, now, orgID, expectedVersion,
		)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return port.ErrVersionConflict
		}
		if _, err := appendChangelogSQL(ctx, exec, &orgID, "organization", orgID, port.MutationUpsert, false, now); err != nil {
			return err
		}
		updated, err = scanOrganization(exec.QueryRowContext(ctx, `
			SELECT id, slug, display_name, status, default_data_region,
			       min_plugin_trust, require_four_eyes_approval, version, created_at_ms, updated_at_ms
			FROM organizations WHERE id = ?`, orgID))
		return err
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *tenancyRepo) CreateProject(ctx context.Context, orgID string, params port.CreateProjectParams) (*port.Project, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(params.Slug) == "" || strings.TrimSpace(params.DisplayName) == "" {
		return nil, fmt.Errorf("%w: org_id, slug, and display_name are required", port.ErrInvalidInput)
	}
	id := params.ID
	if id == "" {
		var err error
		id, err = idgen.New(idgen.PrefixProject)
		if err != nil {
			return nil, err
		}
	} else if err := validateCallerID(id, idgen.PrefixProject); err != nil {
		return nil, err
	}
	now := r.nowMs()

	err := r.withWriteTx(ctx, func(exec sqlExecQuery) error {
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO projects (
				id, org_id, slug, display_name, status, enforced_region, version, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
			id, orgID, params.Slug, params.DisplayName, port.ProjectStatusActive,
			nullStringPtr(params.EnforcedRegion), now, now,
		); err != nil {
			return mapSQLiteError(err)
		}
		_, err := appendChangelogSQL(ctx, exec, &orgID, "project", id, port.MutationUpsert, false, now)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &port.Project{
		ID:             id,
		OrgID:          orgID,
		Slug:           params.Slug,
		DisplayName:    params.DisplayName,
		Status:         port.ProjectStatusActive,
		EnforcedRegion: cloneStringPtr(params.EnforcedRegion),
		Version:        1,
		CreatedAtMs:    now,
		UpdatedAtMs:    now,
	}, nil
}

func (r *tenancyRepo) GetProject(ctx context.Context, orgID, projectID string) (*port.Project, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("%w: org_id and project_id required", port.ErrInvalidInput)
	}
	return scanProject(r.exec.QueryRowContext(ctx, `
		SELECT id, org_id, slug, display_name, status, enforced_region, version, created_at_ms, updated_at_ms
		FROM projects WHERE org_id = ? AND id = ?`, orgID, projectID))
}

func (r *tenancyRepo) GetProjectBySlug(ctx context.Context, orgID, slug string) (*port.Project, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(slug) == "" {
		return nil, fmt.Errorf("%w: org_id and slug required", port.ErrInvalidInput)
	}
	return scanProject(r.exec.QueryRowContext(ctx, `
		SELECT id, org_id, slug, display_name, status, enforced_region, version, created_at_ms, updated_at_ms
		FROM projects WHERE org_id = ? AND slug = ?`, orgID, slug))
}

func (r *tenancyRepo) ListProjects(ctx context.Context, orgID string, limit int) ([]port.Project, error) {
	if strings.TrimSpace(orgID) == "" {
		return nil, fmt.Errorf("%w: org_id required", port.ErrInvalidInput)
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.exec.QueryContext(ctx, `
		SELECT id, org_id, slug, display_name, status, enforced_region, version, created_at_ms, updated_at_ms
		FROM projects WHERE org_id = ? ORDER BY id ASC LIMIT ?`, orgID, limit)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	var out []port.Project
	for rows.Next() {
		prj, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *prj)
	}
	return out, rows.Err()
}

func (r *tenancyRepo) UpdateProject(ctx context.Context, orgID, projectID string, expectedVersion int64, params port.UpdateProjectParams) (*port.Project, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || expectedVersion < 1 {
		return nil, fmt.Errorf("%w: org_id, project_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	if params.DisplayName != nil && strings.TrimSpace(*params.DisplayName) == "" {
		return nil, fmt.Errorf("%w: display_name cannot be blank", port.ErrInvalidInput)
	}
	now := r.nowMs()
	var updated *port.Project

	err := r.withWriteTx(ctx, func(exec sqlExecQuery) error {
		cur, err := scanProject(exec.QueryRowContext(ctx, `
			SELECT id, org_id, slug, display_name, status, enforced_region, version, created_at_ms, updated_at_ms
			FROM projects WHERE org_id = ? AND id = ?`, orgID, projectID))
		if err != nil {
			return err // Uniform ErrNotFound when missing under orgID (0 cross-org leak).
		}
		if cur.Version != expectedVersion {
			return port.ErrVersionConflict
		}

		nextName := cur.DisplayName
		if params.DisplayName != nil {
			nextName = *params.DisplayName
		}
		nextStatus := cur.Status
		if params.Status != nil {
			nextStatus = *params.Status
		}
		nextRegion := cur.EnforcedRegion
		if params.ClearEnforcedRegion {
			nextRegion = nil
		} else if params.EnforcedRegion != nil {
			nextRegion = cloneStringPtr(params.EnforcedRegion)
		}

		res, err := exec.ExecContext(ctx, `
			UPDATE projects
			SET display_name = ?, status = ?, enforced_region = ?, version = version + 1, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND version = ?`,
			nextName, nextStatus, nullStringPtr(nextRegion), now, orgID, projectID, expectedVersion,
		)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return port.ErrVersionConflict
		}
		if _, err := appendChangelogSQL(ctx, exec, &orgID, "project", projectID, port.MutationUpsert, false, now); err != nil {
			return err
		}
		updated, err = scanProject(exec.QueryRowContext(ctx, `
			SELECT id, org_id, slug, display_name, status, enforced_region, version, created_at_ms, updated_at_ms
			FROM projects WHERE org_id = ? AND id = ?`, orgID, projectID))
		return err
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *tenancyRepo) DeleteProject(ctx context.Context, orgID, projectID string, expectedVersion int64) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || expectedVersion < 1 {
		return fmt.Errorf("%w: org_id, project_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	now := r.nowMs()
	return r.withWriteTx(ctx, func(exec sqlExecQuery) error {
		res, err := exec.ExecContext(ctx,
			`DELETE FROM projects WHERE org_id = ? AND id = ? AND version = ?`,
			orgID, projectID, expectedVersion,
		)
		if err != nil {
			return mapSQLiteErrorForDelete(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			var dummy int
			checkErr := exec.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE org_id = ? AND id = ?`, orgID, projectID).Scan(&dummy)
			if errors.Is(checkErr, sql.ErrNoRows) {
				return port.ErrNotFound
			}
			if checkErr != nil {
				return checkErr
			}
			return port.ErrVersionConflict
		}
		_, err = appendChangelogSQL(ctx, exec, &orgID, "project", projectID, port.MutationDelete, false, now)
		return err
	})
}

func (r *tenancyRepo) CreateEnvironment(ctx context.Context, orgID, projectID string, params port.CreateEnvironmentParams) (*port.Environment, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(params.Slug) == "" {
		return nil, fmt.Errorf("%w: org_id, project_id, and slug are required", port.ErrInvalidInput)
	}
	id := params.ID
	if id == "" {
		var err error
		id, err = idgen.New(idgen.PrefixEnv)
		if err != nil {
			return nil, err
		}
	} else if err := validateCallerID(id, idgen.PrefixEnv); err != nil {
		return nil, err
	}
	now := r.nowMs()
	reqTested := 0
	if params.RequireTestedCaps {
		reqTested = 1
	}

	err := r.withWriteTx(ctx, func(exec sqlExecQuery) error {
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO environments (
				id, org_id, project_id, slug, require_tested_caps, version, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
			id, orgID, projectID, params.Slug, reqTested, now, now,
		); err != nil {
			return mapSQLiteError(err)
		}
		_, err := appendChangelogSQL(ctx, exec, &orgID, "environment", id, port.MutationUpsert, false, now)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &port.Environment{
		ID:                id,
		OrgID:             orgID,
		ProjectID:         projectID,
		Slug:              params.Slug,
		RequireTestedCaps: params.RequireTestedCaps,
		Version:           1,
		CreatedAtMs:       now,
		UpdatedAtMs:       now,
	}, nil
}

func (r *tenancyRepo) GetEnvironment(ctx context.Context, orgID, envID string) (*port.Environment, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(envID) == "" {
		return nil, fmt.Errorf("%w: org_id and env_id required", port.ErrInvalidInput)
	}
	return scanEnvironment(r.exec.QueryRowContext(ctx, `
		SELECT id, org_id, project_id, slug, require_tested_caps, version, created_at_ms, updated_at_ms
		FROM environments WHERE org_id = ? AND id = ?`, orgID, envID))
}

func (r *tenancyRepo) ListEnvironments(ctx context.Context, orgID, projectID string) ([]port.Environment, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("%w: org_id and project_id required", port.ErrInvalidInput)
	}
	rows, err := r.exec.QueryContext(ctx, `
		SELECT id, org_id, project_id, slug, require_tested_caps, version, created_at_ms, updated_at_ms
		FROM environments WHERE org_id = ? AND project_id = ? ORDER BY id ASC`, orgID, projectID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	var out []port.Environment
	for rows.Next() {
		env, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *env)
	}
	return out, rows.Err()
}

func (r *tenancyRepo) UpdateEnvironment(ctx context.Context, orgID, envID string, expectedVersion int64, params port.UpdateEnvironmentParams) (*port.Environment, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(envID) == "" || expectedVersion < 1 {
		return nil, fmt.Errorf("%w: org_id, env_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	if params.Slug != nil && strings.TrimSpace(*params.Slug) == "" {
		return nil, fmt.Errorf("%w: slug cannot be blank", port.ErrInvalidInput)
	}
	now := r.nowMs()
	var updated *port.Environment

	err := r.withWriteTx(ctx, func(exec sqlExecQuery) error {
		cur, err := scanEnvironment(exec.QueryRowContext(ctx, `
			SELECT id, org_id, project_id, slug, require_tested_caps, version, created_at_ms, updated_at_ms
			FROM environments WHERE org_id = ? AND id = ?`, orgID, envID))
		if err != nil {
			return err
		}
		if cur.Version != expectedVersion {
			return port.ErrVersionConflict
		}

		nextSlug := cur.Slug
		if params.Slug != nil {
			nextSlug = *params.Slug
		}
		nextTested := cur.RequireTestedCaps
		if params.RequireTestedCaps != nil {
			nextTested = *params.RequireTestedCaps
		}
		reqInt := 0
		if nextTested {
			reqInt = 1
		}

		res, err := exec.ExecContext(ctx, `
			UPDATE environments
			SET slug = ?, require_tested_caps = ?, version = version + 1, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND version = ?`,
			nextSlug, reqInt, now, orgID, envID, expectedVersion,
		)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return port.ErrVersionConflict
		}
		if _, err := appendChangelogSQL(ctx, exec, &orgID, "environment", envID, port.MutationUpsert, false, now); err != nil {
			return err
		}
		updated, err = scanEnvironment(exec.QueryRowContext(ctx, `
			SELECT id, org_id, project_id, slug, require_tested_caps, version, created_at_ms, updated_at_ms
			FROM environments WHERE org_id = ? AND id = ?`, orgID, envID))
		return err
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *tenancyRepo) DeleteEnvironment(ctx context.Context, orgID, envID string, expectedVersion int64) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(envID) == "" || expectedVersion < 1 {
		return fmt.Errorf("%w: org_id, env_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	now := r.nowMs()
	return r.withWriteTx(ctx, func(exec sqlExecQuery) error {
		res, err := exec.ExecContext(ctx,
			`DELETE FROM environments WHERE org_id = ? AND id = ? AND version = ?`,
			orgID, envID, expectedVersion,
		)
		if err != nil {
			return mapSQLiteErrorForDelete(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			var dummy int
			checkErr := exec.QueryRowContext(ctx, `SELECT 1 FROM environments WHERE org_id = ? AND id = ?`, orgID, envID).Scan(&dummy)
			if errors.Is(checkErr, sql.ErrNoRows) {
				return port.ErrNotFound
			}
			if checkErr != nil {
				return checkErr
			}
			return port.ErrVersionConflict
		}
		_, err = appendChangelogSQL(ctx, exec, &orgID, "environment", envID, port.MutationDelete, false, now)
		return err
	})
}

type changelogRepo struct {
	exec  sqlExecQuery
	nowMs func() int64
}

func (c *changelogRepo) Append(ctx context.Context, entry port.ChangelogEntry) (int64, error) {
	now := entry.CreatedAtMs
	if now == 0 {
		now = c.nowMs()
	}
	return appendChangelogSQL(ctx, c.exec, entry.OrgID, entry.EntityKind, entry.EntityID, entry.MutationType, entry.Emergency, now)
}

func (c *changelogRepo) LatestEpoch(ctx context.Context) (int64, error) {
	var epoch sql.NullInt64
	if err := c.exec.QueryRowContext(ctx, `SELECT MAX(config_epoch) FROM config_changelog`).Scan(&epoch); err != nil {
		return 0, mapSQLiteError(err)
	}
	if !epoch.Valid {
		return 0, nil
	}
	return epoch.Int64, nil
}

func (c *changelogRepo) ListSince(ctx context.Context, afterEpoch int64, limit int) ([]port.ChangelogEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := c.exec.QueryContext(ctx, `
		SELECT config_epoch, org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms
		FROM config_changelog
		WHERE config_epoch > ?
		ORDER BY config_epoch ASC
		LIMIT ?`, afterEpoch, limit)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	var out []port.ChangelogEntry
	for rows.Next() {
		var item port.ChangelogEntry
		var orgNull sql.NullString
		var emInt int64
		if scanErr := rows.Scan(&item.ConfigEpoch, &orgNull, &item.EntityKind, &item.EntityID, &item.MutationType, &emInt, &item.CreatedAtMs); scanErr != nil {
			return nil, scanErr
		}
		if orgNull.Valid {
			v := orgNull.String
			item.OrgID = &v
		}
		item.Emergency = emInt != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

type leaseRepo struct {
	store *Store
	exec  sqlExecQuery
}

func (l *leaseRepo) withWriteTx(ctx context.Context, fn func(exec sqlExecQuery) error) error {
	if l.store == nil {
		return fn(l.exec)
	}
	return l.store.InTx(ctx, func(tx port.Tx) error {
		stx := tx.(*sqliteTx)
		return fn(stx.tx)
	})
}

func (l *leaseRepo) AcquireOrRenew(ctx context.Context, leaseName, holderNodeID string, nowMs, ttlMs int64) (*port.DistributedLease, bool, error) {
	if strings.TrimSpace(leaseName) == "" || strings.TrimSpace(holderNodeID) == "" || ttlMs <= 0 {
		return nil, false, fmt.Errorf("%w: lease_name, holder_node_id, and ttlMs > 0 required", port.ErrInvalidInput)
	}
	expiresAt := nowMs + ttlMs
	var (
		out      *port.DistributedLease
		acquired bool
	)
	err := l.withWriteTx(ctx, func(exec sqlExecQuery) error {
		res, err := exec.ExecContext(ctx, `
			INSERT INTO distributed_leases (lease_name, holder_node_id, fencing_token, acquired_at_ms, expires_at_ms)
			VALUES (?, ?, 1, ?, ?)
			ON CONFLICT(lease_name) DO UPDATE SET
				holder_node_id = excluded.holder_node_id,
				fencing_token  = distributed_leases.fencing_token + 1,
				acquired_at_ms = excluded.acquired_at_ms,
				expires_at_ms  = excluded.expires_at_ms
			WHERE distributed_leases.holder_node_id = ''
			   OR distributed_leases.holder_node_id = excluded.holder_node_id
			   OR distributed_leases.expires_at_ms <= excluded.acquired_at_ms`,
			leaseName, holderNodeID, nowMs, expiresAt,
		)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		cur, err := getLeaseSQL(ctx, exec, leaseName, false)
		if err != nil {
			return err
		}
		out = cur
		acquired = affected > 0
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, acquired, nil
}

func (l *leaseRepo) Release(ctx context.Context, leaseName, holderNodeID string) error {
	if strings.TrimSpace(leaseName) == "" || strings.TrimSpace(holderNodeID) == "" {
		return fmt.Errorf("%w: lease_name and holder_node_id required", port.ErrInvalidInput)
	}
	// Preserve the row and its monotonically incremented fencing_token while marking it released
	// (holder_node_id = '' and expires_at_ms > acquired_at_ms satisfying CHECK constraint).
	res, err := l.exec.ExecContext(ctx,
		`UPDATE distributed_leases SET holder_node_id = '', acquired_at_ms = 0, expires_at_ms = 1 WHERE lease_name = ? AND holder_node_id = ? AND holder_node_id <> ''`,
		leaseName, holderNodeID,
	)
	if err != nil {
		return mapSQLiteErrorForDelete(err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return port.ErrNotFound
	}
	return nil
}

func (l *leaseRepo) Get(ctx context.Context, leaseName string) (*port.DistributedLease, error) {
	if strings.TrimSpace(leaseName) == "" {
		return nil, fmt.Errorf("%w: lease_name required", port.ErrInvalidInput)
	}
	return getLeaseSQL(ctx, l.exec, leaseName, true)
}

func getLeaseSQL(ctx context.Context, exec sqlExecQuery, leaseName string, activeOnly bool) (*port.DistributedLease, error) {
	query := `SELECT lease_name, holder_node_id, fencing_token, acquired_at_ms, expires_at_ms FROM distributed_leases WHERE lease_name = ?`
	if activeOnly {
		query += ` AND holder_node_id <> ''`
	}
	var out port.DistributedLease
	err := exec.QueryRowContext(ctx, query, leaseName).Scan(&out.LeaseName, &out.HolderNodeID, &out.FencingToken, &out.AcquiredAtMs, &out.ExpiresAtMs)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	return &out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOrganization(row rowScanner) (*port.Organization, error) {
	var org port.Organization
	var reg sql.NullString
	var fourEyes int64
	if err := row.Scan(
		&org.ID, &org.Slug, &org.DisplayName, &org.Status, &reg,
		&org.MinPluginTrust, &fourEyes, &org.Version, &org.CreatedAtMs, &org.UpdatedAtMs,
	); err != nil {
		return nil, mapSQLiteError(err)
	}
	if reg.Valid {
		v := reg.String
		org.DefaultDataRegion = &v
	}
	org.RequireFourEyesApproval = fourEyes != 0
	return &org, nil
}

func scanProject(row rowScanner) (*port.Project, error) {
	var prj port.Project
	var reg sql.NullString
	if err := row.Scan(
		&prj.ID, &prj.OrgID, &prj.Slug, &prj.DisplayName, &prj.Status,
		&reg, &prj.Version, &prj.CreatedAtMs, &prj.UpdatedAtMs,
	); err != nil {
		return nil, mapSQLiteError(err)
	}
	if reg.Valid {
		v := reg.String
		prj.EnforcedRegion = &v
	}
	return &prj, nil
}

func scanEnvironment(row rowScanner) (*port.Environment, error) {
	var env port.Environment
	var req int64
	if err := row.Scan(
		&env.ID, &env.OrgID, &env.ProjectID, &env.Slug, &req,
		&env.Version, &env.CreatedAtMs, &env.UpdatedAtMs,
	); err != nil {
		return nil, mapSQLiteError(err)
	}
	env.RequireTestedCaps = req != 0
	return &env, nil
}

func nullStringPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func cloneStringPtr(s *string) *string {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}
