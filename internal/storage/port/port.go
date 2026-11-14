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
	"errors"
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
)

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
	RequireTestedCaps bool
}

// UpdateEnvironmentParams specifies mutable fields for an optimistic-locked Environment update.
type UpdateEnvironmentParams struct {
	Slug              *string
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
type LeaseRepository interface {
	AcquireOrRenew(ctx context.Context, leaseName, holderNodeID string, nowMs, ttlMs int64) (*DistributedLease, bool, error)
	Release(ctx context.Context, leaseName, holderNodeID string) error
	Get(ctx context.Context, leaseName string) (*DistributedLease, error)
}

// AuditSink writes immutable audit events (wired to full hash-chain persistence in TP-IMP-0023).
type AuditSink interface {
	Append(ctx context.Context, event AuditEvent) error
}
