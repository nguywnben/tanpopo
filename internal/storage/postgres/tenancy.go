// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type sqlExecQuery interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Tenancy returns the Store's TenancyRepository.
func (s *Store) Tenancy() port.TenancyRepository {
	return &tenancyRepo{store: s, exec: s.db, nowMs: s.nowMs}
}

// Changelog returns the Store's ChangelogRepository.
func (s *Store) Changelog() port.ChangelogRepository {
	return &changelogRepo{store: s, exec: s.db, nowMs: s.nowMs}
}

// Leases returns the Store's LeaseRepository.
func (s *Store) Leases() port.LeaseRepository {
	return &leaseRepo{store: s, exec: s.db}
}

// Audit returns the Store's AuditSink.
func (s *Store) Audit() port.AuditSink {
	return &storeAuditSink{sink: s.getAuditSink()}
}

// BeginTx starts a relational PostgreSQL transaction scoped to port.Tx.
func (s *Store) BeginTx(ctx context.Context) (port.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &pgTx{store: s, tx: tx}, nil
}

// InTx executes fn inside an atomic transaction, rolling back on error or panic.
func (s *Store) InTx(ctx context.Context, fn func(tx port.Tx) error) error {
	if fn == nil {
		return fmt.Errorf("%w: nil transaction callback", port.ErrInvalidInput)
	}
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

type pgTx struct {
	store *Store
	tx    *sql.Tx
}

func (t *pgTx) Commit() error   { return t.tx.Commit() }
func (t *pgTx) Rollback() error { return t.tx.Rollback() }
func (t *pgTx) Tenancy() port.TenancyRepository {
	return &tenancyRepo{store: t.store, tx: t.tx, exec: t.tx, nowMs: t.store.nowMs}
}
func (t *pgTx) Changelog() port.ChangelogRepository {
	return &changelogRepo{store: t.store, tx: t.tx, exec: t.tx, nowMs: t.store.nowMs}
}
func (t *pgTx) Leases() port.LeaseRepository {
	return &leaseRepo{store: t.store, tx: t.tx, exec: t.tx}
}
func (t *pgTx) Audit() port.AuditSink {
	return &txAuditSink{tx: t.tx, sink: t.store.getAuditSink()}
}

type storeAuditSink struct {
	sink port.AuditSink
}

func (a *storeAuditSink) Append(ctx context.Context, ev port.AuditEvent) error {
	if a.sink == nil {
		return nil
	}
	return a.sink.Append(ctx, ev)
}

type txAuditSink struct {
	tx   *sql.Tx
	sink port.AuditSink
}

func (a *txAuditSink) Append(ctx context.Context, ev port.AuditEvent) error {
	if a.sink == nil {
		return nil
	}
	txSink, ok := a.sink.(port.TxAuditSink)
	if !ok {
		return fmt.Errorf("%w: AuditSink must implement port.TxAuditSink", port.ErrInvalidInput)
	}
	return txSink.AppendTx(ctx, a.tx, ev)
}

func validateCallerID(id, expectedPrefix string) error {
	if strings.ContainsAny(id, " :\t\r\n\x00") || len(id) > 40 || !strings.HasPrefix(id, expectedPrefix+"_") || len(id) <= len(expectedPrefix)+1 {
		return fmt.Errorf("%w: caller-supplied id %q must use prefix %q_ and length <= 40 without whitespace or ':'", port.ErrInvalidInput, id, expectedPrefix)
	}
	return nil
}

func emitTenancyAuditTx(
	ctx context.Context,
	exec sqlExecQuery,
	auditSink port.AuditSink,
	orgID *string,
	eventType, targetType, targetID string,
	meta map[string]any,
	now int64,
) error {
	if auditSink == nil {
		return nil
	}
	txSink, ok := auditSink.(port.TxAuditSink)
	if !ok {
		return fmt.Errorf("%w: AuditSink must implement port.TxAuditSink", port.ErrInvalidInput)
	}
	sqlTx, isTx := exec.(*sql.Tx)
	if !isTx {
		return fmt.Errorf("%w: tenancy audit requires an active *sql.Tx", port.ErrInvalidInput)
	}
	auditID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return err
	}
	metaJSON := "{}"
	if len(meta) > 0 {
		raw, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		metaJSON = string(raw)
	}
	actorKind := "SYSTEM"
	actorID := "storage"
	entryPoint := port.AuditEntryPointAdminAPI
	var reqID *string
	if actor, ok := port.AuditActorFromContext(ctx); ok {
		if strings.TrimSpace(actor.ActorKind) != "" {
			actorKind = actor.ActorKind
		}
		actorID = actor.ActorID
		if strings.TrimSpace(actor.EntryPoint) != "" {
			entryPoint = actor.EntryPoint
		}
		if trimmedReq := strings.TrimSpace(actor.RequestID); trimmedReq != "" {
			reqID = &trimmedReq
		}
	}
	return txSink.AppendTx(ctx, sqlTx, port.AuditEvent{
		ID:           auditID,
		CreatedAtMs:  now,
		OrgID:        orgID,
		ActorKind:    actorKind,
		ActorID:      actorID,
		EntryPoint:   entryPoint,
		EventType:    eventType,
		TargetType:   targetType,
		TargetID:     targetID,
		RequestID:    reqID,
		MetadataJSON: metaJSON,
	})
}

type tenancyRepo struct {
	store *Store
	tx    *sql.Tx
	exec  sqlExecQuery
	nowMs func() int64
}

func (r *tenancyRepo) withOrgExec(ctx context.Context, orgID string, fn func(exec sqlExecQuery) error) error {
	if strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("%w: org_id is required", port.ErrInvalidInput)
	}
	if r.tx != nil {
		if _, err := r.tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'false', true), set_config('app.current_org_id', $1, true)`, orgID); err != nil {
			return mapPGError(err)
		}
		return fn(r.tx)
	}
	return r.store.WithOrgTx(ctx, orgID, func(tx *sql.Tx) error {
		return fn(tx)
	})
}

func (r *tenancyRepo) withOperatorExec(ctx context.Context, fn func(exec sqlExecQuery) error) error {
	if r.tx != nil {
		if _, err := r.tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', '', true), set_config('app.is_instance_operator', 'true', true)`); err != nil {
			return mapPGError(err)
		}
		return fn(r.tx)
	}
	return r.store.WithOperatorTx(ctx, func(tx *sql.Tx) error {
		return fn(tx)
	})
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
	minTrust := strings.TrimSpace(params.MinPluginTrust)
	if minTrust == "" {
		minTrust = port.MinTrustAnyApproved
	}
	now := r.nowMs()
	var out *port.Organization
	err := r.withOperatorExec(ctx, func(exec sqlExecQuery) error {
		row := exec.QueryRowContext(
			ctx,
			`INSERT INTO organizations (
				id, slug, display_name, status, default_data_region,
				min_plugin_trust, require_four_eyes_approval, version,
				created_at_ms, updated_at_ms
			) VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8, $8)
			RETURNING id, slug, display_name, status, default_data_region,
				min_plugin_trust, require_four_eyes_approval, version,
				created_at_ms, updated_at_ms`,
			id, params.Slug, params.DisplayName, port.OrgStatusActive,
			nullStringPtr(params.DefaultDataRegion), minTrust, params.RequireFourEyesApproval, now,
		)
		org, scanErr := scanOrganization(row)
		if scanErr != nil {
			return scanErr
		}
		if err := insertChangelogExec(ctx, exec, &org.ID, "organization", org.ID, port.MutationUpsert, now); err != nil {
			return err
		}
		if err := emitTenancyAuditTx(ctx, exec, r.store.getAuditSink(), &org.ID, "organization.created", "organization", org.ID, map[string]any{"slug": org.Slug}, now); err != nil {
			return err
		}
		out = org
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) GetOrganization(ctx context.Context, orgID string) (*port.Organization, error) {
	if strings.TrimSpace(orgID) == "" {
		return nil, fmt.Errorf("%w: org_id is required", port.ErrInvalidInput)
	}
	var out *port.Organization
	err := r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		row := exec.QueryRowContext(
			ctx,
			`SELECT id, slug, display_name, status, default_data_region,
				min_plugin_trust, require_four_eyes_approval, version,
				created_at_ms, updated_at_ms
			 FROM organizations WHERE id = $1`,
			orgID,
		)
		org, scanErr := scanOrganization(row)
		if scanErr != nil {
			return scanErr
		}
		out = org
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) GetOrganizationBySlug(ctx context.Context, slug string) (*port.Organization, error) {
	if strings.TrimSpace(slug) == "" {
		return nil, fmt.Errorf("%w: slug is required", port.ErrInvalidInput)
	}
	var out *port.Organization
	err := r.withOperatorExec(ctx, func(exec sqlExecQuery) error {
		row := exec.QueryRowContext(
			ctx,
			`SELECT id, slug, display_name, status, default_data_region,
				min_plugin_trust, require_four_eyes_approval, version,
				created_at_ms, updated_at_ms
			 FROM organizations WHERE slug = $1`,
			slug,
		)
		org, scanErr := scanOrganization(row)
		if scanErr != nil {
			return scanErr
		}
		out = org
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) ListOrganizations(ctx context.Context, limit int) ([]port.Organization, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []port.Organization
	err := r.withOperatorExec(ctx, func(exec sqlExecQuery) error {
		rows, err := exec.QueryContext(
			ctx,
			`SELECT id, slug, display_name, status, default_data_region,
				min_plugin_trust, require_four_eyes_approval, version,
				created_at_ms, updated_at_ms
			 FROM organizations ORDER BY id ASC LIMIT $1`,
			limit,
		)
		if err != nil {
			return mapPGError(err)
		}
		defer rows.Close()
		for rows.Next() {
			org, err := scanOrganization(rows)
			if err != nil {
				return err
			}
			out = append(out, *org)
		}
		return mapPGError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) UpdateOrganization(ctx context.Context, orgID string, expectedVersion int64, params port.UpdateOrgParams) (*port.Organization, error) {
	if strings.TrimSpace(orgID) == "" || expectedVersion < 1 {
		return nil, fmt.Errorf("%w: org_id and expectedVersion >= 1 required", port.ErrInvalidInput)
	}
	var out *port.Organization
	err := r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		cur, err := scanOrganization(exec.QueryRowContext(
			ctx,
			`SELECT id, slug, display_name, status, default_data_region,
				min_plugin_trust, require_four_eyes_approval, version,
				created_at_ms, updated_at_ms
			 FROM organizations WHERE id = $1 FOR UPDATE`,
			orgID,
		))
		if err != nil {
			return err
		}
		if cur.Version != expectedVersion {
			return port.ErrVersionConflict
		}
		displayName := cur.DisplayName
		if params.DisplayName != nil {
			if strings.TrimSpace(*params.DisplayName) == "" {
				return fmt.Errorf("%w: display_name cannot be empty", port.ErrInvalidInput)
			}
			displayName = *params.DisplayName
		}
		status := cur.Status
		if params.Status != nil {
			status = *params.Status
		}
		reg := cur.DefaultDataRegion
		if params.ClearDefaultDataRegion {
			reg = nil
		} else if params.DefaultDataRegion != nil {
			reg = params.DefaultDataRegion
		}
		minTrust := cur.MinPluginTrust
		if params.MinPluginTrust != nil {
			minTrust = *params.MinPluginTrust
		}
		fourEyes := cur.RequireFourEyesApproval
		if params.RequireFourEyesApproval != nil {
			fourEyes = *params.RequireFourEyesApproval
		}
		now := r.nowMs()
		row := exec.QueryRowContext(
			ctx,
			`UPDATE organizations
			 SET display_name = $1, status = $2, default_data_region = $3,
				 min_plugin_trust = $4, require_four_eyes_approval = $5,
				 version = version + 1, updated_at_ms = $6
			 WHERE id = $7 AND version = $8
			 RETURNING id, slug, display_name, status, default_data_region,
				min_plugin_trust, require_four_eyes_approval, version,
				created_at_ms, updated_at_ms`,
			displayName, status, nullStringPtr(reg), minTrust, fourEyes, now, orgID, expectedVersion,
		)
		updated, err := scanOrganization(row)
		if err != nil {
			if errors.Is(err, port.ErrNotFound) {
				return port.ErrVersionConflict
			}
			return err
		}
		if err := insertChangelogExec(ctx, exec, &updated.ID, "organization", updated.ID, port.MutationUpsert, now); err != nil {
			return err
		}
		if err := emitTenancyAuditTx(ctx, exec, r.store.getAuditSink(), &updated.ID, "organization.updated", "organization", updated.ID, map[string]any{"version": updated.Version}, now); err != nil {
			return err
		}
		out = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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
	var out *port.Project
	err := r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		row := exec.QueryRowContext(
			ctx,
			`INSERT INTO projects (
				id, org_id, slug, display_name, status, enforced_region,
				version, created_at_ms, updated_at_ms
			) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $7)
			RETURNING id, org_id, slug, display_name, status, enforced_region,
				version, created_at_ms, updated_at_ms`,
			id, orgID, params.Slug, params.DisplayName, port.ProjectStatusActive,
			nullStringPtr(params.EnforcedRegion), now,
		)
		prj, scanErr := scanProject(row)
		if scanErr != nil {
			return scanErr
		}
		if err := insertChangelogExec(ctx, exec, &orgID, "project", prj.ID, port.MutationUpsert, now); err != nil {
			return err
		}
		if err := emitTenancyAuditTx(ctx, exec, r.store.getAuditSink(), &orgID, "project.created", "project", prj.ID, map[string]any{"slug": prj.Slug}, now); err != nil {
			return err
		}
		out = prj
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) GetProject(ctx context.Context, orgID, projectID string) (*port.Project, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("%w: org_id and project_id are required", port.ErrInvalidInput)
	}
	var out *port.Project
	err := r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		row := exec.QueryRowContext(
			ctx,
			`SELECT id, org_id, slug, display_name, status, enforced_region,
				version, created_at_ms, updated_at_ms
			 FROM projects WHERE org_id = $1 AND id = $2`,
			orgID, projectID,
		)
		prj, scanErr := scanProject(row)
		if scanErr != nil {
			return scanErr
		}
		out = prj
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) GetProjectBySlug(ctx context.Context, orgID, slug string) (*port.Project, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(slug) == "" {
		return nil, fmt.Errorf("%w: org_id and slug are required", port.ErrInvalidInput)
	}
	var out *port.Project
	err := r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		row := exec.QueryRowContext(
			ctx,
			`SELECT id, org_id, slug, display_name, status, enforced_region,
				version, created_at_ms, updated_at_ms
			 FROM projects WHERE org_id = $1 AND slug = $2`,
			orgID, slug,
		)
		prj, scanErr := scanProject(row)
		if scanErr != nil {
			return scanErr
		}
		out = prj
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) ListProjects(ctx context.Context, orgID string, limit int) ([]port.Project, error) {
	if strings.TrimSpace(orgID) == "" {
		return nil, fmt.Errorf("%w: org_id is required", port.ErrInvalidInput)
	}
	if limit <= 0 {
		limit = 100
	}
	var out []port.Project
	err := r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		rows, err := exec.QueryContext(
			ctx,
			`SELECT id, org_id, slug, display_name, status, enforced_region,
				version, created_at_ms, updated_at_ms
			 FROM projects WHERE org_id = $1 ORDER BY id ASC LIMIT $2`,
			orgID, limit,
		)
		if err != nil {
			return mapPGError(err)
		}
		defer rows.Close()
		for rows.Next() {
			prj, err := scanProject(rows)
			if err != nil {
				return err
			}
			out = append(out, *prj)
		}
		return mapPGError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) UpdateProject(ctx context.Context, orgID, projectID string, expectedVersion int64, params port.UpdateProjectParams) (*port.Project, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || expectedVersion < 1 {
		return nil, fmt.Errorf("%w: org_id, project_id, and expectedVersion >= 1 required", port.ErrInvalidInput)
	}
	var out *port.Project
	err := r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		cur, err := scanProject(exec.QueryRowContext(
			ctx,
			`SELECT id, org_id, slug, display_name, status, enforced_region,
				version, created_at_ms, updated_at_ms
			 FROM projects WHERE org_id = $1 AND id = $2 FOR UPDATE`,
			orgID, projectID,
		))
		if err != nil {
			return err
		}
		if cur.Version != expectedVersion {
			return port.ErrVersionConflict
		}
		displayName := cur.DisplayName
		if params.DisplayName != nil {
			if strings.TrimSpace(*params.DisplayName) == "" {
				return fmt.Errorf("%w: display_name cannot be empty", port.ErrInvalidInput)
			}
			displayName = *params.DisplayName
		}
		status := cur.Status
		if params.Status != nil {
			status = *params.Status
		}
		reg := cur.EnforcedRegion
		if params.ClearEnforcedRegion {
			reg = nil
		} else if params.EnforcedRegion != nil {
			reg = params.EnforcedRegion
		}
		now := r.nowMs()
		row := exec.QueryRowContext(
			ctx,
			`UPDATE projects
			 SET display_name = $1, status = $2, enforced_region = $3,
				 version = version + 1, updated_at_ms = $4
			 WHERE org_id = $5 AND id = $6 AND version = $7
			 RETURNING id, org_id, slug, display_name, status, enforced_region,
				version, created_at_ms, updated_at_ms`,
			displayName, status, nullStringPtr(reg), now, orgID, projectID, expectedVersion,
		)
		updated, err := scanProject(row)
		if err != nil {
			if errors.Is(err, port.ErrNotFound) {
				return port.ErrVersionConflict
			}
			return err
		}
		if err := insertChangelogExec(ctx, exec, &orgID, "project", updated.ID, port.MutationUpsert, now); err != nil {
			return err
		}
		if err := emitTenancyAuditTx(ctx, exec, r.store.getAuditSink(), &orgID, "project.updated", "project", updated.ID, map[string]any{"version": updated.Version}, now); err != nil {
			return err
		}
		out = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) DeleteProject(ctx context.Context, orgID, projectID string, expectedVersion int64) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || expectedVersion < 1 {
		return fmt.Errorf("%w: org_id, project_id, and expectedVersion >= 1 required", port.ErrInvalidInput)
	}
	return r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		res, err := exec.ExecContext(
			ctx,
			`DELETE FROM projects WHERE org_id = $1 AND id = $2 AND version = $3`,
			orgID, projectID, expectedVersion,
		)
		if err != nil {
			return mapPGErrorForDelete(err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n > 0 {
			now := r.nowMs()
			if err := insertChangelogExec(ctx, exec, &orgID, "project", projectID, port.MutationDelete, now); err != nil {
				return err
			}
			return emitTenancyAuditTx(ctx, exec, r.store.getAuditSink(), &orgID, "project.deleted", "project", projectID, nil, now)
		}
		var ver int64
		checkErr := exec.QueryRowContext(ctx, `SELECT version FROM projects WHERE org_id = $1 AND id = $2`, orgID, projectID).Scan(&ver)
		if errors.Is(checkErr, sql.ErrNoRows) {
			return port.ErrNotFound
		}
		if checkErr != nil {
			return mapPGError(checkErr)
		}
		return port.ErrVersionConflict
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
	var out *port.Environment
	err := r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		row := exec.QueryRowContext(
			ctx,
			`INSERT INTO environments (
				id, org_id, project_id, slug, require_tested_caps,
				version, created_at_ms, updated_at_ms
			) VALUES ($1, $2, $3, $4, $5, 1, $6, $6)
			RETURNING id, org_id, project_id, slug, require_tested_caps,
				version, created_at_ms, updated_at_ms`,
			id, orgID, projectID, params.Slug, params.RequireTestedCaps, now,
		)
		env, scanErr := scanEnvironment(row)
		if scanErr != nil {
			return scanErr
		}
		if err := insertChangelogExec(ctx, exec, &orgID, "environment", env.ID, port.MutationUpsert, now); err != nil {
			return err
		}
		if err := emitTenancyAuditTx(ctx, exec, r.store.getAuditSink(), &orgID, "environment.created", "environment", env.ID, map[string]any{"slug": env.Slug}, now); err != nil {
			return err
		}
		out = env
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) GetEnvironment(ctx context.Context, orgID, envID string) (*port.Environment, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(envID) == "" {
		return nil, fmt.Errorf("%w: org_id and env_id are required", port.ErrInvalidInput)
	}
	var out *port.Environment
	err := r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		row := exec.QueryRowContext(
			ctx,
			`SELECT id, org_id, project_id, slug, require_tested_caps,
				version, created_at_ms, updated_at_ms
			 FROM environments WHERE org_id = $1 AND id = $2`,
			orgID, envID,
		)
		env, scanErr := scanEnvironment(row)
		if scanErr != nil {
			return scanErr
		}
		out = env
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) ListEnvironments(ctx context.Context, orgID, projectID string) ([]port.Environment, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("%w: org_id and project_id are required", port.ErrInvalidInput)
	}
	var out []port.Environment
	err := r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		rows, err := exec.QueryContext(
			ctx,
			`SELECT id, org_id, project_id, slug, require_tested_caps,
				version, created_at_ms, updated_at_ms
			 FROM environments WHERE org_id = $1 AND project_id = $2
			 ORDER BY id ASC`,
			orgID, projectID,
		)
		if err != nil {
			return mapPGError(err)
		}
		defer rows.Close()
		for rows.Next() {
			env, err := scanEnvironment(rows)
			if err != nil {
				return err
			}
			out = append(out, *env)
		}
		return mapPGError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) UpdateEnvironment(ctx context.Context, orgID, envID string, expectedVersion int64, params port.UpdateEnvironmentParams) (*port.Environment, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(envID) == "" || expectedVersion < 1 {
		return nil, fmt.Errorf("%w: org_id, env_id, and expectedVersion >= 1 required", port.ErrInvalidInput)
	}
	var out *port.Environment
	err := r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		cur, err := scanEnvironment(exec.QueryRowContext(
			ctx,
			`SELECT id, org_id, project_id, slug, require_tested_caps,
				version, created_at_ms, updated_at_ms
			 FROM environments WHERE org_id = $1 AND id = $2 FOR UPDATE`,
			orgID, envID,
		))
		if err != nil {
			return err
		}
		if cur.Version != expectedVersion {
			return port.ErrVersionConflict
		}
		slug := cur.Slug
		if params.Slug != nil {
			if strings.TrimSpace(*params.Slug) == "" {
				return fmt.Errorf("%w: slug cannot be empty", port.ErrInvalidInput)
			}
			slug = *params.Slug
		}
		reqCaps := cur.RequireTestedCaps
		if params.RequireTestedCaps != nil {
			reqCaps = *params.RequireTestedCaps
		}
		now := r.nowMs()
		row := exec.QueryRowContext(
			ctx,
			`UPDATE environments
			 SET slug = $1, require_tested_caps = $2,
				 version = version + 1, updated_at_ms = $3
			 WHERE org_id = $4 AND id = $5 AND version = $6
			 RETURNING id, org_id, project_id, slug, require_tested_caps,
				version, created_at_ms, updated_at_ms`,
			slug, reqCaps, now, orgID, envID, expectedVersion,
		)
		updated, err := scanEnvironment(row)
		if err != nil {
			if errors.Is(err, port.ErrNotFound) {
				return port.ErrVersionConflict
			}
			return err
		}
		if err := insertChangelogExec(ctx, exec, &orgID, "environment", updated.ID, port.MutationUpsert, now); err != nil {
			return err
		}
		if err := emitTenancyAuditTx(ctx, exec, r.store.getAuditSink(), &orgID, "environment.updated", "environment", updated.ID, map[string]any{"version": updated.Version}, now); err != nil {
			return err
		}
		out = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *tenancyRepo) DeleteEnvironment(ctx context.Context, orgID, envID string, expectedVersion int64) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(envID) == "" || expectedVersion < 1 {
		return fmt.Errorf("%w: org_id, env_id, and expectedVersion >= 1 required", port.ErrInvalidInput)
	}
	return r.withOrgExec(ctx, orgID, func(exec sqlExecQuery) error {
		res, err := exec.ExecContext(
			ctx,
			`DELETE FROM environments WHERE org_id = $1 AND id = $2 AND version = $3`,
			orgID, envID, expectedVersion,
		)
		if err != nil {
			return mapPGErrorForDelete(err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n > 0 {
			now := r.nowMs()
			if err := insertChangelogExec(ctx, exec, &orgID, "environment", envID, port.MutationDelete, now); err != nil {
				return err
			}
			return emitTenancyAuditTx(ctx, exec, r.store.getAuditSink(), &orgID, "environment.deleted", "environment", envID, nil, now)
		}
		var ver int64
		checkErr := exec.QueryRowContext(ctx, `SELECT version FROM environments WHERE org_id = $1 AND id = $2`, orgID, envID).Scan(&ver)
		if errors.Is(checkErr, sql.ErrNoRows) {
			return port.ErrNotFound
		}
		if checkErr != nil {
			return mapPGError(checkErr)
		}
		return port.ErrVersionConflict
	})
}

func insertChangelogExec(ctx context.Context, exec sqlExecQuery, orgID *string, entityKind, entityID, mutationType string, nowMs int64) error {
	_, err := exec.ExecContext(
		ctx,
		`INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		 VALUES ($1, $2, $3, $4, FALSE, $5)`,
		nullStringPtr(orgID), entityKind, entityID, mutationType, nowMs,
	)
	return mapPGError(err)
}

type changelogRepo struct {
	store *Store
	tx    *sql.Tx
	exec  sqlExecQuery
	nowMs func() int64
}

func (c *changelogRepo) Append(ctx context.Context, entry port.ChangelogEntry) (int64, error) {
	if strings.TrimSpace(entry.EntityKind) == "" || strings.TrimSpace(entry.EntityID) == "" || strings.TrimSpace(entry.MutationType) == "" {
		return 0, fmt.Errorf("%w: entity_kind, entity_id, and mutation_type required", port.ErrInvalidInput)
	}
	createdAt := entry.CreatedAtMs
	if createdAt <= 0 {
		createdAt = c.nowMs()
	}
	var epoch int64
	err := c.exec.QueryRowContext(
		ctx,
		`INSERT INTO config_changelog (
			org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING config_epoch`,
		nullStringPtr(entry.OrgID), entry.EntityKind, entry.EntityID,
		entry.MutationType, entry.Emergency, createdAt,
	).Scan(&epoch)
	if err != nil {
		return 0, mapPGError(err)
	}
	return epoch, nil
}

func (c *changelogRepo) LatestEpoch(ctx context.Context) (int64, error) {
	var epoch int64
	if err := c.exec.QueryRowContext(ctx, `SELECT COALESCE(MAX(config_epoch), 0) FROM config_changelog`).Scan(&epoch); err != nil {
		return 0, mapPGError(err)
	}
	return epoch, nil
}

func (c *changelogRepo) ListSince(ctx context.Context, afterEpoch int64, limit int) ([]port.ChangelogEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := c.exec.QueryContext(
		ctx,
		`SELECT config_epoch, org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms
		 FROM config_changelog WHERE config_epoch > $1 ORDER BY config_epoch ASC LIMIT $2`,
		afterEpoch, limit,
	)
	if err != nil {
		return nil, mapPGError(err)
	}
	defer rows.Close()

	var out []port.ChangelogEntry
	for rows.Next() {
		var (
			item  port.ChangelogEntry
			orgID sql.NullString
		)
		if err := rows.Scan(
			&item.ConfigEpoch, &orgID, &item.EntityKind, &item.EntityID,
			&item.MutationType, &item.Emergency, &item.CreatedAtMs,
		); err != nil {
			return nil, mapPGError(err)
		}
		if orgID.Valid {
			v := orgID.String
			item.OrgID = &v
		}
		out = append(out, item)
	}
	return out, mapPGError(rows.Err())
}

type leaseRepo struct {
	store *Store
	tx    *sql.Tx
	exec  sqlExecQuery
}

func (l *leaseRepo) AcquireOrRenew(ctx context.Context, leaseName, holderNodeID string, nowMs, ttlMs int64) (*port.DistributedLease, bool, error) {
	if strings.TrimSpace(leaseName) == "" || strings.TrimSpace(holderNodeID) == "" || ttlMs < 1 {
		return nil, false, fmt.Errorf("%w: lease_name, holder_node_id, and ttlMs >= 1 required", port.ErrInvalidInput)
	}

	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		out, acquired, retry, err := l.acquireOrRenewOnce(ctx, leaseName, holderNodeID, nowMs, ttlMs)
		if err == nil && !retry {
			return out, acquired, nil
		}
		if !retry {
			return nil, false, err
		}
	}
	return nil, false, fmt.Errorf("postgres leases: exceeded retry attempts acquiring %q", leaseName)
}

func (l *leaseRepo) acquireOrRenewOnce(ctx context.Context, leaseName, holderNodeID string, nowMs, ttlMs int64) (*port.DistributedLease, bool, bool, error) {
	run := func(exec sqlExecQuery) (*port.DistributedLease, bool, error) {
		var cur port.DistributedLease
		err := exec.QueryRowContext(
			ctx,
			`SELECT lease_name, holder_node_id, fencing_token, acquired_at_ms, expires_at_ms
			 FROM distributed_leases WHERE lease_name = $1 FOR UPDATE`,
			leaseName,
		).Scan(&cur.LeaseName, &cur.HolderNodeID, &cur.FencingToken, &cur.AcquiredAtMs, &cur.ExpiresAtMs)

		if errors.Is(err, sql.ErrNoRows) {
			exp := nowMs + ttlMs
			if _, insErr := exec.ExecContext(
				ctx,
				`INSERT INTO distributed_leases (lease_name, holder_node_id, fencing_token, acquired_at_ms, expires_at_ms)
				 VALUES ($1, $2, 1, $3, $4)`,
				leaseName, holderNodeID, nowMs, exp,
			); insErr != nil {
				return nil, false, mapPGError(insErr)
			}
			return &port.DistributedLease{
				LeaseName:    leaseName,
				HolderNodeID: holderNodeID,
				FencingToken: 1,
				AcquiredAtMs: nowMs,
				ExpiresAtMs:  exp,
			}, true, nil
		}
		if err != nil {
			return nil, false, mapPGError(err)
		}

		if cur.HolderNodeID != "" && cur.HolderNodeID != holderNodeID && !cur.IsExpired(nowMs) {
			return &cur, false, nil
		}

		nextToken := cur.FencingToken + 1
		exp := nowMs + ttlMs
		if _, updErr := exec.ExecContext(
			ctx,
			`UPDATE distributed_leases
			 SET holder_node_id = $1, fencing_token = $2, acquired_at_ms = $3, expires_at_ms = $4
			 WHERE lease_name = $5`,
			holderNodeID, nextToken, nowMs, exp, leaseName,
		); updErr != nil {
			return nil, false, mapPGError(updErr)
		}
		return &port.DistributedLease{
			LeaseName:    leaseName,
			HolderNodeID: holderNodeID,
			FencingToken: nextToken,
			AcquiredAtMs: nowMs,
			ExpiresAtMs:  exp,
		}, true, nil
	}

	if l.tx != nil {
		out, acquired, err := run(l.tx)
		return out, acquired, false, err
	}

	tx, err := l.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, false, err
	}
	defer func() { _ = tx.Rollback() }()

	out, acquired, err := run(tx)
	if err != nil {
		if isRetryablePGError(err) || errors.Is(err, port.ErrAlreadyExists) {
			return nil, false, true, err
		}
		return nil, false, false, err
	}
	if err := tx.Commit(); err != nil {
		if isRetryablePGError(err) {
			return nil, false, true, err
		}
		return nil, false, false, mapPGError(err)
	}
	return out, acquired, false, nil
}

func (l *leaseRepo) Release(ctx context.Context, leaseName, holderNodeID string, fencingToken int64) error {
	if strings.TrimSpace(leaseName) == "" || strings.TrimSpace(holderNodeID) == "" || fencingToken < 1 {
		return fmt.Errorf("%w: lease_name, holder_node_id, and fencing_token >= 1 required", port.ErrInvalidInput)
	}
	run := func(exec sqlExecQuery) error {
		res, err := exec.ExecContext(
			ctx,
			`UPDATE distributed_leases
			 SET holder_node_id = '', acquired_at_ms = 0, expires_at_ms = 1
			 WHERE lease_name = $1 AND holder_node_id = $2 AND fencing_token = $3 AND holder_node_id <> ''`,
			leaseName, holderNodeID, fencingToken,
		)
		if err != nil {
			return mapPGErrorForDelete(err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		var (
			curHolder string
			curToken  int64
		)
		checkErr := exec.QueryRowContext(
			ctx,
			`SELECT holder_node_id, fencing_token FROM distributed_leases WHERE lease_name = $1`,
			leaseName,
		).Scan(&curHolder, &curToken)
		if errors.Is(checkErr, sql.ErrNoRows) {
			return port.ErrNotFound
		}
		if checkErr != nil {
			return mapPGError(checkErr)
		}
		if curHolder == "" && curToken == fencingToken {
			return port.ErrNotFound
		}
		return port.ErrVersionConflict
	}

	if l.tx != nil {
		return run(l.tx)
	}
	tx, err := l.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := run(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (l *leaseRepo) Get(ctx context.Context, leaseName string) (*port.DistributedLease, error) {
	if strings.TrimSpace(leaseName) == "" {
		return nil, fmt.Errorf("%w: lease_name required", port.ErrInvalidInput)
	}
	var out port.DistributedLease
	err := l.exec.QueryRowContext(
		ctx,
		`SELECT lease_name, holder_node_id, fencing_token, acquired_at_ms, expires_at_ms
		 FROM distributed_leases WHERE lease_name = $1 AND holder_node_id <> ''`,
		leaseName,
	).Scan(&out.LeaseName, &out.HolderNodeID, &out.FencingToken, &out.AcquiredAtMs, &out.ExpiresAtMs)
	if err != nil {
		return nil, mapPGError(err)
	}
	return &out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOrganization(row rowScanner) (*port.Organization, error) {
	var (
		org port.Organization
		reg sql.NullString
	)
	if err := row.Scan(
		&org.ID, &org.Slug, &org.DisplayName, &org.Status, &reg,
		&org.MinPluginTrust, &org.RequireFourEyesApproval, &org.Version,
		&org.CreatedAtMs, &org.UpdatedAtMs,
	); err != nil {
		return nil, mapPGError(err)
	}
	if reg.Valid {
		v := reg.String
		org.DefaultDataRegion = &v
	}
	return &org, nil
}

func scanProject(row rowScanner) (*port.Project, error) {
	var (
		prj port.Project
		reg sql.NullString
	)
	if err := row.Scan(
		&prj.ID, &prj.OrgID, &prj.Slug, &prj.DisplayName, &prj.Status,
		&reg, &prj.Version, &prj.CreatedAtMs, &prj.UpdatedAtMs,
	); err != nil {
		return nil, mapPGError(err)
	}
	if reg.Valid {
		v := reg.String
		prj.EnforcedRegion = &v
	}
	return &prj, nil
}

func scanEnvironment(row rowScanner) (*port.Environment, error) {
	var env port.Environment
	if err := row.Scan(
		&env.ID, &env.OrgID, &env.ProjectID, &env.Slug, &env.RequireTestedCaps,
		&env.Version, &env.CreatedAtMs, &env.UpdatedAtMs,
	); err != nil {
		return nil, mapPGError(err)
	}
	return &env, nil
}

func nullStringPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func mapPGError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return port.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", port.ErrAlreadyExists, pgErr.Message)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: %s", port.ErrNotFound, pgErr.Message)
		case "23514", "22001", "23502": // check_violation, string_data_right_truncation, not_null_violation
			return fmt.Errorf("%w: %s", port.ErrInvalidInput, pgErr.Message)
		case "42501": // insufficient_privilege / RLS policy violation
			return fmt.Errorf("%w: %s", port.ErrInvalidInput, pgErr.Message)
		}
	}
	return err
}

func mapPGErrorForDelete(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return fmt.Errorf("%w: %s", port.ErrResourceInUse, pgErr.Message)
	}
	return mapPGError(err)
}

func isRetryablePGError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	return false
}
