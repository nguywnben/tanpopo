// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package port defines the dialect-neutral relational storage ports, domain
// entities, and canonical error sentinels shared across SQLite WAL and
// PostgreSQL 16+ drivers.
//
// DAG Boundary (Tier 1): May import stdlib and internal/pkg only; must never
// import concrete storage drivers or higher domain/ingress layers.
package port

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Canonical storage domain errors (ARCH-DATA-01 / TP-IMP-0007).
var (
	ErrNotFound          = errors.New("storage: resource not found")
	ErrVersionConflict   = errors.New("storage: optimistic lock version conflict")
	ErrResourceInUse     = errors.New("storage: resource is referenced and cannot be removed")
	ErrAlreadyExists     = errors.New("storage: unique constraint conflict")
	ErrInvalidInput      = errors.New("storage: invalid domain input")
	ErrDirtyMigration    = errors.New("storage: dirty or checksum-tampered migration state")
	ErrNetworkFilesystem = errors.New("storage: SQLite WAL refuses network filesystems (NFS/SMB/CIFS/UNC)")
	ErrPrivilegedRole    = errors.New("storage: PostgreSQL role must not have SUPERUSER or BYPASSRLS")
)

// Dialect identifies the active relational database backend for dual-DB modules.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

// DetectDialect inspects db with a lightweight engine probe so dual-DB services
// (audit, finops/ledger, vault, ops) can adapt SQL syntax and locking without
// importing concrete database driver packages.
func DetectDialect(ctx context.Context, db *sql.DB) Dialect {
	if db == nil {
		return DialectSQLite
	}
	var version string
	if err := db.QueryRowContext(ctx, "SELECT current_setting('server_version_num', true)").Scan(&version); err == nil && version != "" {
		return DialectPostgres
	}
	return DialectSQLite
}

// Rebind translates positional '?' placeholders into PostgreSQL '$1, $2, ...'
// placeholders when dialect is DialectPostgres, ignoring string literals and comments.
func Rebind(dialect Dialect, query string) string {
	if dialect != DialectPostgres || !strings.ContainsRune(query, '?') {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 16)
	argNum := 1
	inSingleQuote := false
	inLineComment := false
	inBlockComment := false
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if inLineComment {
			b.WriteByte(ch)
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			b.WriteByte(ch)
			if ch == '*' && i+1 < len(query) && query[i+1] == '/' {
				b.WriteByte('/')
				i++
				inBlockComment = false
			}
			continue
		}
		if inSingleQuote {
			b.WriteByte(ch)
			if ch == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					b.WriteByte('\'')
					i++
				} else {
					inSingleQuote = false
				}
			}
			continue
		}
		switch {
		case ch == '\'':
			inSingleQuote = true
			b.WriteByte(ch)
		case ch == '-' && i+1 < len(query) && query[i+1] == '-':
			inLineComment = true
			b.WriteString("--")
			i++
		case ch == '/' && i+1 < len(query) && query[i+1] == '*':
			inBlockComment = true
			b.WriteString("/*")
			i++
		case ch == '?':
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(argNum))
			argNum++
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

// FlexBool scans both SQLite INTEGER (0/1) and PostgreSQL BOOLEAN (true/false) columns seamlessly.
type FlexBool bool

// Scan implements sql.Scanner for cross-engine boolean compatibility.
func (b *FlexBool) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*b = false
		return nil
	case bool:
		*b = FlexBool(v)
		return nil
	case int64:
		*b = v != 0
		return nil
	case int32:
		*b = v != 0
		return nil
	case int:
		*b = v != 0
		return nil
	case []byte:
		s := strings.TrimSpace(strings.ToLower(string(v)))
		*b = s == "1" || s == "t" || s == "true"
		return nil
	case string:
		s := strings.TrimSpace(strings.ToLower(v))
		*b = s == "1" || s == "t" || s == "true"
		return nil
	default:
		return fmt.Errorf("port: unsupported bool scan type %T", src)
	}
}

// BoolVal returns a Go bool on PostgreSQL or int (0/1) on SQLite for parameterized bindings.
func BoolVal(d Dialect, v bool) any {
	if d == DialectPostgres {
		return v
	}
	if v {
		return 1
	}
	return 0
}

// BoolExpr returns a portable SQL boolean literal ("TRUE"/"FALSE" vs "1"/"0").
func BoolExpr(d Dialect, v bool) string {
	if d == DialectPostgres {
		if v {
			return "TRUE"
		}
		return "FALSE"
	}
	if v {
		return "1"
	}
	return "0"
}

// SetOperatorRLSTx activates instance-operator RLS scope on PostgreSQL transactions (no-op on SQLite).
func SetOperatorRLSTx(ctx context.Context, tx *sql.Tx, d Dialect) error {
	if d != DialectPostgres {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', '', true), set_config('app.is_instance_operator', 'true', true)`); err != nil {
		return fmt.Errorf("port: set operator RLS context: %w", err)
	}
	return nil
}

// SetOrgRLSTx activates strict single-organization FORCE RLS scope on PostgreSQL transactions (no-op on SQLite).
func SetOrgRLSTx(ctx context.Context, tx *sql.Tx, d Dialect, orgID string) error {
	if d != DialectPostgres {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'false', true), set_config('app.current_org_id', $1, true)`, orgID); err != nil {
		return fmt.Errorf("port: set org %q RLS context: %w", orgID, err)
	}
	return nil
}

// ListOrgIDsTx enumerates all organization IDs inside an operator-scoped transaction.
func ListOrgIDsTx(ctx context.Context, tx *sql.Tx, d Dialect) ([]string, error) {
	if err := SetOperatorRLSTx(ctx, tx, d); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM organizations ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("port: list organizations: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if serr := rows.Scan(&id); serr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("port: scan organization id: %w", serr)
		}
		ids = append(ids, id)
	}
	if rerr := rows.Err(); rerr != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("port: iterate organizations: %w", rerr)
	}
	_ = rows.Close()
	return ids, nil
}

// AppendConfigChangelogTx inserts a config_changelog mutation across SQLite and PostgreSQL
// (emitting PostgreSQL LISTEN/NOTIFY on tanpopo_config_changelog) and returns the committed epoch.
func AppendConfigChangelogTx(ctx context.Context, tx *sql.Tx, d Dialect, orgID, entityKind, entityID, mutationType string, emergency bool, nowMs int64) (int64, error) {
	var orgArg any
	if strings.TrimSpace(orgID) != "" {
		orgArg = orgID
	}
	if d == DialectPostgres {
		var epoch int64
		err := tx.QueryRowContext(ctx,
			`INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES ($1, $2, $3, $4, $5, $6) RETURNING config_epoch`,
			orgArg, entityKind, entityID, mutationType, emergency, nowMs,
		).Scan(&epoch)
		if err != nil {
			return 0, err
		}
		payload, merr := json.Marshal(struct {
			ConfigEpoch  int64  `json:"config_epoch"`
			OrgID        string `json:"org_id,omitempty"`
			EntityKind   string `json:"entity_kind"`
			EntityID     string `json:"entity_id"`
			MutationType string `json:"mutation_type"`
			Emergency    bool   `json:"emergency,omitempty"`
		}{
			ConfigEpoch:  epoch,
			OrgID:        strings.TrimSpace(orgID),
			EntityKind:   entityKind,
			EntityID:     entityID,
			MutationType: mutationType,
			Emergency:    emergency,
		})
		if merr == nil {
			_, _ = tx.ExecContext(ctx, `SELECT pg_notify('tanpopo_config_changelog', $1)`, string(payload))
		}
		return epoch, nil
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, ?, ?, ?, ?, ?)`,
		orgArg, entityKind, entityID, mutationType, BoolVal(d, emergency), nowMs,
	)
	if err != nil {
		return 0, err
	}
	epoch, _ := res.LastInsertId()
	return epoch, nil
}

// Organization lifecycle statuses matching SQLite and PostgreSQL CHECK constraints.
const (
	OrgStatusActive         = "ACTIVE"
	OrgStatusSuspended      = "SUSPENDED"
	OrgStatusDeleting       = "DELETING"
	OrgStatusPendingErasure = "PENDING_ERASURE"
)

// Organization plugin trust minimums matching SQLite and PostgreSQL CHECK constraints.
const (
	MinTrustOfficialOnly               = "OFFICIAL_ONLY"
	MinTrustVerifiedOrEnterpriseSigned = "VERIFIED_OR_ENTERPRISE_SIGNED"
	MinTrustVerifiedOnly               = "VERIFIED_ONLY"
	MinTrustAnyApproved                = "ANY_APPROVED"
)

// Project lifecycle statuses matching SQLite and PostgreSQL CHECK constraints.
const (
	ProjectStatusActive          = "ACTIVE"
	ProjectStatusEmergencyHalted = "EMERGENCY_HALTED"
	ProjectStatusDeleting        = "DELETING"
	ProjectStatusPendingErasure  = "PENDING_ERASURE"
)

// Config changelog mutation types matching config_changelog CHECK constraints.
const (
	MutationUpsert     = "UPSERT"
	MutationDelete     = "DELETE"
	MutationRevoke     = "REVOKE"
	MutationQuarantine = "QUARANTINE"
	MutationKeyRotate  = "KEY_ROTATE"
	MutationShred      = "SHRED"
)

// Organization is the root tenant boundary in Tanpopo.
type Organization struct {
	ID                      string
	Slug                    string
	DisplayName             string
	Status                  string
	DefaultDataRegion       *string
	MinPluginTrust          string
	RequireFourEyesApproval bool
	Version                 int64
	CreatedAtMs             int64
	UpdatedAtMs             int64
}

// Project belongs strictly to an Organization via composite key (org_id, id).
type Project struct {
	ID             string
	OrgID          string
	Slug           string
	DisplayName    string
	Status         string
	EnforcedRegion *string
	Version        int64
	CreatedAtMs    int64
	UpdatedAtMs    int64
}

// Environment belongs strictly to a (org_id, project_id) via composite FK.
type Environment struct {
	ID                string
	OrgID             string
	ProjectID         string
	Slug              string
	DisplayName       string
	RequireTestedCaps bool
	Version           int64
	CreatedAtMs       int64
	UpdatedAtMs       int64
}

// ChangelogEntry records an atomic config_epoch increment in config_changelog.
type ChangelogEntry struct {
	ConfigEpoch  int64
	OrgID        *string
	EntityKind   string
	EntityID     string
	MutationType string
	Emergency    bool
	CreatedAtMs  int64
}

// DistributedLease represents a singleton leader lease in distributed_leases.
type DistributedLease struct {
	LeaseName    string
	HolderNodeID string
	FencingToken int64
	AcquiredAtMs int64
	ExpiresAtMs  int64
}

// IsExpired reports whether the lease has reached or passed its expiry timestamp at nowMs.
func (l *DistributedLease) IsExpired(nowMs int64) bool {
	return l == nil || nowMs >= l.ExpiresAtMs
}

// AuditEvent represents an immutable JCS-hashed audit entry for AuditSink callers.
type AuditEvent struct {
	ID           string
	CreatedAtMs  int64
	OrgID        *string
	ActorKind    string
	ActorID      string
	EntryPoint   string
	EventType    string
	TargetType   string
	TargetID     string
	RequestID    *string
	MetadataJSON string
}

// CreateOrgParams specifies fields for creating a root Organization.
type CreateOrgParams struct {
	ID                      string
	Slug                    string
	DisplayName             string
	DefaultDataRegion       *string
	MinPluginTrust          string
	RequireFourEyesApproval bool
}

// UpdateOrgParams specifies mutable fields for an optimistic-locked Organization update.
type UpdateOrgParams struct {
	DisplayName             *string
	Status                  *string
	DefaultDataRegion       *string
	ClearDefaultDataRegion  bool
	MinPluginTrust          *string
	RequireFourEyesApproval *bool
}

// CreateProjectParams specifies fields for creating an Organization-scoped Project.
type CreateProjectParams struct {
	ID             string
	Slug           string
	DisplayName    string
	EnforcedRegion *string
}

// UpdateProjectParams specifies mutable fields for an optimistic-locked Project update.
type UpdateProjectParams struct {
	DisplayName         *string
	Status              *string
	EnforcedRegion      *string
	ClearEnforcedRegion bool
}

// CreateEnvironmentParams specifies fields for creating a Project-scoped Environment.
type CreateEnvironmentParams struct {
	ID                string
	Slug              string
	DisplayName       string
	RequireTestedCaps bool
}

// UpdateEnvironmentParams specifies mutable fields for an optimistic-locked Environment update.
type UpdateEnvironmentParams struct {
	Slug              *string
	DisplayName       *string
	RequireTestedCaps *bool
}

// Tx represents an atomic relational storage transaction with scoped repositories.
type Tx interface {
	Commit() error
	Rollback() error
	Tenancy() TenancyRepository
	Changelog() ChangelogRepository
	Leases() LeaseRepository
	Audit() AuditSink
}

// Store is the top-level dialect-neutral persistence entrypoint.
type Store interface {
	Tenancy() TenancyRepository
	Changelog() ChangelogRepository
	Leases() LeaseRepository
	Audit() AuditSink
	BeginTx(ctx context.Context) (Tx, error)
	InTx(ctx context.Context, fn func(tx Tx) error) error
	Ping(ctx context.Context) error
	Close() error
}

// TenancyRepository manages Organizations, Projects, and Environments with
// mandatory org_id parameter isolation on every tenant-scoped lookup and mutation.
// Security invariant: never expose GetProjectByIDOnly or GetEnvironmentByIDOnly.
type TenancyRepository interface {
	CreateOrganization(ctx context.Context, params CreateOrgParams) (*Organization, error)
	GetOrganization(ctx context.Context, orgID string) (*Organization, error)
	GetOrganizationBySlug(ctx context.Context, slug string) (*Organization, error)
	ListOrganizations(ctx context.Context, limit int) ([]Organization, error)
	UpdateOrganization(ctx context.Context, orgID string, expectedVersion int64, params UpdateOrgParams) (*Organization, error)

	CreateProject(ctx context.Context, orgID string, params CreateProjectParams) (*Project, error)
	GetProject(ctx context.Context, orgID, projectID string) (*Project, error)
	GetProjectBySlug(ctx context.Context, orgID, slug string) (*Project, error)
	ListProjects(ctx context.Context, orgID string, limit int) ([]Project, error)
	UpdateProject(ctx context.Context, orgID, projectID string, expectedVersion int64, params UpdateProjectParams) (*Project, error)
	DeleteProject(ctx context.Context, orgID, projectID string, expectedVersion int64) error

	CreateEnvironment(ctx context.Context, orgID, projectID string, params CreateEnvironmentParams) (*Environment, error)
	GetEnvironment(ctx context.Context, orgID, envID string) (*Environment, error)
	ListEnvironments(ctx context.Context, orgID, projectID string) ([]Environment, error)
	UpdateEnvironment(ctx context.Context, orgID, envID string, expectedVersion int64, params UpdateEnvironmentParams) (*Environment, error)
	DeleteEnvironment(ctx context.Context, orgID, envID string, expectedVersion int64) error
}

// ChangelogRepository accesses the canonical config_changelog table.
type ChangelogRepository interface {
	Append(ctx context.Context, entry ChangelogEntry) (int64, error)
	LatestEpoch(ctx context.Context) (int64, error)
	ListSince(ctx context.Context, afterEpoch int64, limit int) ([]ChangelogEntry, error)
}

// LeaseRepository manages distributed_leases singleton fencing locks.
//
// Fencing & Expiration Semantics:
//   - AcquireOrRenew acquires an unheld/released/expired lease or renews the caller's lease,
//     always monotonically incrementing FencingToken across acquisitions, renewals, and releases.
//   - Release requires matching (leaseName, holderNodeID, fencingToken) so a stalled caller
//     holding a superseded token cannot release a newer lease owned by the same or another node
//     (returning ErrVersionConflict). Already-released or nonexistent leases return ErrNotFound.
//   - Get returns the current un-released DistributedLease record (or ErrNotFound if absent or
//     released). Because expiration depends on caller time nowMs (and SQL remains vendor-neutral),
//     Get does NOT suppress expired un-released rows; callers must check lease.IsExpired(nowMs).
type LeaseRepository interface {
	AcquireOrRenew(ctx context.Context, leaseName, holderNodeID string, nowMs, ttlMs int64) (*DistributedLease, bool, error)
	Release(ctx context.Context, leaseName, holderNodeID string, fencingToken int64) error
	Get(ctx context.Context, leaseName string) (*DistributedLease, error)
}

// Canonical audit_events.entry_point values matching SQLite/PostgreSQL CHECK constraints.
const (
	AuditEntryPointAdminAPI       = "admin_api"
	AuditEntryPointCLI            = "cli"
	AuditEntryPointGitOpsBoot     = "gitops_boot"
	AuditEntryPointSystemReaper   = "system_reaper"
	AuditEntryPointDataPlaneGuard = "data_plane_guard"
)

// AuditSink writes immutable audit events (wired to full hash-chain persistence in TP-IMP-0023).
type AuditSink interface {
	Append(ctx context.Context, event AuditEvent) error
}

// TxAuditSink writes an immutable audit event inside an caller-managed *sql.Tx
// BEFORE tx.Commit(), enforcing fail-closed transactional audit atomicity (CR-06 / TP-IMP-0023).
type TxAuditSink interface {
	AuditSink
	AppendTx(ctx context.Context, tx *sql.Tx, event AuditEvent) error
}

// AuditActor attributes storage-level mutations initiated by authenticated control-plane callers.
type AuditActor struct {
	ActorKind  string
	ActorID    string
	EntryPoint string
	RequestID  string
}

type auditActorContextKey struct{}

// WithAuditActor attaches caller audit attribution to ctx for downstream storage mutations.
func WithAuditActor(ctx context.Context, actor AuditActor) context.Context {
	return context.WithValue(ctx, auditActorContextKey{}, actor)
}

// AuditActorFromContext extracts caller audit attribution from ctx when present.
func AuditActorFromContext(ctx context.Context) (AuditActor, bool) {
	if ctx == nil {
		return AuditActor{}, false
	}
	actor, ok := ctx.Value(auditActorContextKey{}).(AuditActor)
	if !ok || strings.TrimSpace(actor.ActorID) == "" {
		return AuditActor{}, false
	}
	return actor, true
}
