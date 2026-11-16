// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package installer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// Canonical SM-01 extension installation lifecycle states matching 0006_extensions.sql CHECK constraints.
const (
	StateDiscovered                = "DISCOVERED"
	StateVerifying                 = "VERIFYING"
	StateRejectedInvalid           = "REJECTED_INVALID"
	StatePendingPermissionApproval = "PENDING_PERMISSION_APPROVAL"
	StateCancelled                 = "CANCELLED"
	StateInstalledDisabled         = "INSTALLED_DISABLED"
	StateEnabledActive             = "ENABLED_ACTIVE"
	StateUpdatingPendingReconsent  = "UPDATING_PENDING_RECONSENT"
	StateDraining                  = "DRAINING"
	StateRolledBack                = "ROLLED_BACK"
	StateQuarantined               = "QUARANTINED"
	StateUninstalled               = "UNINSTALLED"
)

// Canonical lifecycle error sentinels.
var (
	ErrInvalidLifecycleConfig = errors.New("installer: invalid lifecycle service config")
	ErrInvalidStateTransition = errors.New("installer: invalid SM-01 state transition")
	ErrPermissionDenied       = errors.New("installer: principal lacks required OPERATOR / ORG_OWNER / SECURITY_REVIEWER role")
	ErrRevokedExtension       = errors.New("installer: extension matched CRITICAL revocation feed")
	ErrNoRollbackTarget       = errors.New("installer: no previous_pinned_version_id retained for rollback")
	// ErrLockfileIntegrity indicates tanpopo.lock verification failed or lockfile is malformed/symlinked.
	ErrLockfileIntegrity = errors.New("installer: tanpopo.lock verification failed")
	// ErrLockfileSyncFailed indicates SQLite committed the state transition but post-commit tanpopo.lock sync failed.
	ErrLockfileSyncFailed = errors.New("installer: committed in database but tanpopo.lock sync failed")
)

var validSM01Transitions = map[string]map[string]bool{
	StateDiscovered: {
		StateVerifying:   true,
		StateQuarantined: true,
	},
	StateVerifying: {
		StateRejectedInvalid:           true,
		StatePendingPermissionApproval: true,
		StateQuarantined:               true,
	},
	StatePendingPermissionApproval: {
		StateInstalledDisabled: true,
		StateEnabledActive:     true,
		StateCancelled:         true,
		StateQuarantined:       true,
	},
	StateInstalledDisabled: {
		StateEnabledActive: true,
		StateQuarantined:   true,
		StateUninstalled:   true,
	},
	StateEnabledActive: {
		StateDraining:                 true,
		StateUpdatingPendingReconsent: true,
		StateRolledBack:               true,
		StateQuarantined:              true,
	},
	StateUpdatingPendingReconsent: {
		StateDraining:      true,
		StateEnabledActive: true,
		StateQuarantined:   true,
	},
	StateDraining: {
		StateInstalledDisabled: true,
		StateEnabledActive:     true,
		StateRolledBack:        true,
		StateQuarantined:       true,
	},
	StateRolledBack: {
		StateEnabledActive:     true,
		StateInstalledDisabled: true,
		StateQuarantined:       true,
	},
	StateQuarantined: {
		StateInstalledDisabled: true,
		StateUninstalled:       true,
	},
}

// IsValidSM01Transition enforces canonical SM-01 transition arcs per business-entity-map.md.
func IsValidSM01Transition(from, to string) bool {
	targets, ok := validSM01Transitions[from]
	if !ok {
		return false
	}
	return targets[to]
}

// PermissionSet represents the normalized capability set P governed by consent & escalation algebra (CT-18).
type PermissionSet struct {
	AllowedHosts        []string          `json:"allowed_hosts,omitempty"`
	AllowPrivateIP      bool              `json:"allow_private_ip,omitempty"`
	RequireTLS          bool              `json:"require_tls,omitempty"`
	ExecutionTier       string            `json:"execution_tier"`
	SecretSlots         []string          `json:"secret_slots,omitempty"`
	SecretBindingModes  map[string]string `json:"secret_binding_modes,omitempty"`
	Hooks               []string          `json:"hooks,omitempty"`
	MaxMemoryMib        int               `json:"max_memory_mib,omitempty"`
	MaxCpuMsPerCall     int               `json:"max_cpu_ms_per_call,omitempty"`
	MaxStreamChunkCpuMs int               `json:"max_stream_chunk_cpu_ms,omitempty"`
	InspectRequestBody  bool              `json:"inspect_request_body,omitempty"`
	InspectResponseBody bool              `json:"inspect_response_body,omitempty"`
}

// NormalizePermissionSet canonicalizes host casing and sorts slice sets deterministically.
func NormalizePermissionSet(p PermissionSet) PermissionSet {
	out := p
	if len(p.AllowedHosts) > 0 {
		seen := make(map[string]bool, len(p.AllowedHosts))
		out.AllowedHosts = make([]string, 0, len(p.AllowedHosts))
		for _, h := range p.AllowedHosts {
			canon := strings.ToLower(strings.TrimSpace(h))
			if canon != "" && !seen[canon] {
				seen[canon] = true
				out.AllowedHosts = append(out.AllowedHosts, canon)
			}
		}
		sort.Strings(out.AllowedHosts)
	}
	if len(p.SecretSlots) > 0 {
		seen := make(map[string]bool, len(p.SecretSlots))
		out.SecretSlots = make([]string, 0, len(p.SecretSlots))
		for _, s := range p.SecretSlots {
			tr := strings.TrimSpace(s)
			if tr != "" && !seen[tr] {
				seen[tr] = true
				out.SecretSlots = append(out.SecretSlots, tr)
			}
		}
		sort.Strings(out.SecretSlots)
	}
	if len(p.Hooks) > 0 {
		seen := make(map[string]bool, len(p.Hooks))
		out.Hooks = make([]string, 0, len(p.Hooks))
		for _, h := range p.Hooks {
			tr := strings.TrimSpace(h)
			if tr != "" && !seen[tr] {
				seen[tr] = true
				out.Hooks = append(out.Hooks, tr)
			}
		}
		sort.Strings(out.Hooks)
	}
	if len(p.SecretBindingModes) > 0 {
		out.SecretBindingModes = make(map[string]string, len(p.SecretBindingModes))
		for k, v := range p.SecretBindingModes {
			out.SecretBindingModes[k] = v
		}
	}
	return out
}

// PermissionDelta reports any capability expansion DeltaP = P_new \ P_approved (CT-18).
type PermissionDelta struct {
	AddedHosts             []string `json:"added_hosts,omitempty"`
	AddedSecretSlots       []string `json:"added_secret_slots,omitempty"`
	AddedHooks             []string `json:"added_hooks,omitempty"`
	BindingModeChanged     bool     `json:"binding_mode_changed,omitempty"`
	TierEscalated          bool     `json:"tier_escalated,omitempty"`
	PrivateIPEscalated     bool     `json:"private_ip_escalated,omitempty"`
	TLSDowngraded          bool     `json:"tls_downgraded,omitempty"`
	MemoryIncreased        bool     `json:"memory_increased,omitempty"`
	CpuQuotaIncreased      bool     `json:"cpu_quota_increased,omitempty"`
	PayloadAccessEscalated bool     `json:"payload_access_escalated,omitempty"`
}

// HasEscalation reports whether DeltaP != empty set.
func (d PermissionDelta) HasEscalation() bool {
	return len(d.AddedHosts) > 0 ||
		len(d.AddedSecretSlots) > 0 ||
		len(d.AddedHooks) > 0 ||
		d.BindingModeChanged ||
		d.TierEscalated ||
		d.PrivateIPEscalated ||
		d.TLSDowngraded ||
		d.MemoryIncreased ||
		d.CpuQuotaIncreased ||
		d.PayloadAccessEscalated
}

func tierRank(tier string) int {
	switch tier {
	case ExecutionTierDeclarativeT1:
		return 1
	case ExecutionTierWasmSandboxT2:
		return 2
	case ExecutionTierIsolatedSidecarT3:
		return 3
	default:
		return 99
	}
}

// ComputePermissionDelta computes DeltaP = P_new \ P_approved deterministically (CT-18).
func ComputePermissionDelta(approved, candidate PermissionSet) PermissionDelta {
	a := NormalizePermissionSet(approved)
	c := NormalizePermissionSet(candidate)

	var delta PermissionDelta

	aHosts := make(map[string]bool, len(a.AllowedHosts))
	for _, h := range a.AllowedHosts {
		aHosts[h] = true
	}
	for _, h := range c.AllowedHosts {
		if !aHosts[h] {
			delta.AddedHosts = append(delta.AddedHosts, h)
		}
	}

	aSlots := make(map[string]bool, len(a.SecretSlots))
	for _, s := range a.SecretSlots {
		aSlots[s] = true
	}
	for _, s := range c.SecretSlots {
		if !aSlots[s] {
			delta.AddedSecretSlots = append(delta.AddedSecretSlots, s)
		}
	}

	aHooks := make(map[string]bool, len(a.Hooks))
	for _, h := range a.Hooks {
		aHooks[h] = true
	}
	for _, h := range c.Hooks {
		if !aHooks[h] {
			delta.AddedHooks = append(delta.AddedHooks, h)
		}
	}
	for slot, cMode := range c.SecretBindingModes {
		aMode, ok := a.SecretBindingModes[slot]
		if !ok || strings.TrimSpace(aMode) != strings.TrimSpace(cMode) {
			delta.BindingModeChanged = true
			break
		}
	}

	// A retained slot without an explicit mode defaults to blind injection.
	for _, slot := range c.SecretSlots {
		if a.SecretBindingModes[slot] != "" && c.SecretBindingModes[slot] == "" {
			delta.BindingModeChanged = true
			break
		}
	}

	if tierRank(c.ExecutionTier) > tierRank(a.ExecutionTier) {
		delta.TierEscalated = true
	}
	if !a.AllowPrivateIP && c.AllowPrivateIP {
		delta.PrivateIPEscalated = true
	}
	if a.RequireTLS && !c.RequireTLS {
		delta.TLSDowngraded = true
	}
	aMem := a.MaxMemoryMib
	if aMem == 0 && a.ExecutionTier == ExecutionTierWasmSandboxT2 {
		aMem = 16
	}
	cMem := c.MaxMemoryMib
	if cMem == 0 && c.ExecutionTier == ExecutionTierWasmSandboxT2 {
		cMem = 16
	}
	if cMem > aMem {
		delta.MemoryIncreased = true
	}
	aCpu := a.MaxCpuMsPerCall
	if aCpu == 0 && a.ExecutionTier == ExecutionTierWasmSandboxT2 {
		aCpu = 50
	}
	cCpu := c.MaxCpuMsPerCall
	if cCpu == 0 && c.ExecutionTier == ExecutionTierWasmSandboxT2 {
		cCpu = 50
	}
	aChunkCpu := a.MaxStreamChunkCpuMs
	if aChunkCpu == 0 && a.ExecutionTier == ExecutionTierWasmSandboxT2 {
		aChunkCpu = min(aCpu, 1000)
	}
	cChunkCpu := c.MaxStreamChunkCpuMs
	if cChunkCpu == 0 && c.ExecutionTier == ExecutionTierWasmSandboxT2 {
		cChunkCpu = min(cCpu, 1000)
	}
	if cCpu > aCpu || cChunkCpu > aChunkCpu {
		delta.CpuQuotaIncreased = true
	}
	if (!a.InspectRequestBody && c.InspectRequestBody) || (!a.InspectResponseBody && c.InspectResponseBody) {
		delta.PayloadAccessEscalated = true
	}
	return delta
}

// InstallationRecord mirrors the persisted extension_installations + package/version metadata.
type InstallationRecord struct {
	InstallationID          string
	PackageID               string
	CanonicalName           string
	OriginTrustTier         string
	ActiveVersionID         *string
	ActiveSemver            *string
	PreviousPinnedVersionID *string
	CandidateVersionID      *string
	CandidateSemver         *string
	State                   string
	ApprovedPermissions     PermissionSet
	ApprovedByPrincipalID   *string
	ApprovedAtMs            *int64
	QuarantineReason        *string
	Version                 int64
	ExtractedStagingDir     string
	CreatedAtMs             int64
	UpdatedAtMs             int64
}

// ServiceConfig configures LifecycleService fail-closed.
type ServiceConfig struct {
	DB               *sql.DB
	Audit            port.AuditSink
	AuditEntryPoint  string // optional; defaults to "admin_api" (must match audit_events.entry_point CHECK)
	PluginsDir       string
	LockfilePath     string
	Keyring          TrustedKeyring
	ValidationPolicy ValidationPolicy
	NowMs            func() int64
}

// LifecycleService manages extension extraction, validation, provenance/org trust checks,
// SM-01 consent transitions, upgrade escalation fencing, rollback, quarantine, and tanpopo.lock.
type LifecycleService struct {
	mu              sync.Mutex
	db              *sql.DB
	audit           port.AuditSink
	auditEntryPoint string
	pluginsDir      string
	lockfilePath    string
	keyring         TrustedKeyring
	valPolicy       ValidationPolicy
	nowMs           func() int64
}

// NewLifecycleService constructs a LifecycleService fail-closed on missing DB, Audit, or PluginsDir.
func NewLifecycleService(cfg ServiceConfig) (*LifecycleService, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", ErrInvalidLifecycleConfig)
	}
	if cfg.Audit == nil {
		return nil, fmt.Errorf("%w: non-nil port.TxAuditSink is required", ErrInvalidLifecycleConfig)
	}
	if _, ok := cfg.Audit.(port.TxAuditSink); !ok {
		return nil, fmt.Errorf("%w: Audit must implement port.TxAuditSink", ErrInvalidLifecycleConfig)
	}
	entryPoint := strings.TrimSpace(cfg.AuditEntryPoint)
	if entryPoint == "" {
		entryPoint = "admin_api"
	}
	switch entryPoint {
	case "admin_api", "cli", "gitops_boot", "system_reaper", "data_plane_guard":
	default:
		return nil, fmt.Errorf("%w: unsupported AuditEntryPoint %q", ErrInvalidLifecycleConfig, entryPoint)
	}
	canonPluginsDir, err := ensureConfinedRootDir(cfg.PluginsDir)
	if err != nil {
		return nil, err
	}
	valPolicy := cfg.ValidationPolicy
	if len(valPolicy.SupportedContractVersions) == 0 {
		valPolicy = DefaultValidationPolicy()
	}
	if strings.TrimSpace(valPolicy.CoreVersion) == "" {
		valPolicy.CoreVersion = ActiveCoreSemver
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	return &LifecycleService{
		db:              cfg.DB,
		audit:           cfg.Audit,
		auditEntryPoint: entryPoint,
		pluginsDir:      canonPluginsDir,
		lockfilePath:    strings.TrimSpace(cfg.LockfilePath),
		keyring:         cfg.Keyring,
		valPolicy:       valPolicy,
		nowMs:           nowFn,
	}, nil
}

// StageInstallRequest supplies archive stream/path, immutable source pin, signatures, and optional org gate check.
type StageInstallRequest struct {
	ArchiveReader io.Reader
	ArchivePath   string
	Source        SourceSpec
	Signature     ProvenanceSignature
	SBOMDigest    string
	ActorID       string
	RequestID     string
	// Optional TargetOrgID enforces CheckOrgMinTrustPolicy immediately at staging time if supplied (CT-20).
	TargetOrgID string
}

// StageInstallResult reports the newly staged extension installation parked in PENDING_PERMISSION_APPROVAL.
type StageInstallResult struct {
	Installation InstallationRecord
	Manifest     ValidatedManifest
	Provenance   ProvenanceVerdict
	Archive      ExtractedArchive
}

// StageFreshInstall validates source pinning (CT-25), unpacks .tpkg safely (CT-04), validates manifest
// (CT-01, CT-02, CT-24), verifies Ed25519 provenance & reserved namespace (CT-03, CT-05), checks
// revocations & optional org min_plugin_trust (CT-20), and halts in PENDING_PERMISSION_APPROVAL
// with active_version_id = NULL. Zero guest code is executed.
func (s *LifecycleService) StageFreshInstall(ctx context.Context, req StageInstallRequest) (*StageInstallResult, error) {
	canonSourceURI, err := ValidateSourceRef(req.Source)
	if err != nil {
		s.emitAudit(ctx, req.TargetOrgID, req.ActorID, req.RequestID, "extension.source_rejected", "extension_package", req.Source.URI, map[string]any{
			"reason": err.Error(),
		})
		return nil, err
	}

	extracted, err := s.extractRequestArchive(req.ArchiveReader, req.ArchivePath)
	if err != nil {
		s.emitAudit(ctx, req.TargetOrgID, req.ActorID, req.RequestID, "extension.archive_rejected", "extension_package", canonSourceURI, map[string]any{
			"reason": err.Error(),
		})
		return nil, err
	}
	cleanupOnErr := true
	defer func() {
		if cleanupOnErr {
			_ = SafeRemovePackageDir(s.pluginsDir, extracted.StagingDir)
		}
	}()

	vm, err := ParseAndValidateManifest(extracted.ManifestBytes, s.valPolicy, extracted.FileSHA256)
	if err != nil {
		s.emitAudit(ctx, req.TargetOrgID, req.ActorID, req.RequestID, "extension.manifest_rejected", "extension_package", canonSourceURI, map[string]any{
			"reason": err.Error(),
		})
		return nil, err
	}

	prov, err := VerifyProvenance(vm, extracted.BundleSHA256, req.Source.ExpectedSHA256, req.Signature, s.keyring)
	if err != nil {
		s.emitAudit(ctx, req.TargetOrgID, req.ActorID, req.RequestID, "extension.provenance_rejected", "extension_package", vm.Manifest.Metadata.ID, map[string]any{
			"reason": err.Error(),
		})
		return nil, err
	}

	if req.TargetOrgID != "" {
		if err := s.checkOrgTrustInDB(ctx, req.TargetOrgID, prov.OriginTrustTier, prov.EnterpriseSigned); err != nil {
			s.emitAudit(ctx, req.TargetOrgID, req.ActorID, req.RequestID, "extension.org_trust_policy_denied", "extension_package", vm.Manifest.Metadata.ID, map[string]any{
				"origin_trust_tier": prov.OriginTrustTier,
				"enterprise_signed": prov.EnterpriseSigned,
			})
			return nil, err
		}
	}

	if err := s.checkRevocations(ctx, vm.Manifest.Metadata.ID, vm.Manifest.Metadata.Version, extracted.BundleSHA256); err != nil {
		s.emitAudit(ctx, req.TargetOrgID, req.ActorID, req.RequestID, "extension.revocation_blocked", "extension_package", vm.Manifest.Metadata.ID, map[string]any{
			"version": vm.Manifest.Metadata.Version,
			"sha256":  extracted.BundleSHA256,
		})
		return nil, err
	}

	manifestJSON, err := json.Marshal(vm.Manifest)
	if err != nil {
		return nil, fmt.Errorf("installer: marshal manifest_json: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pkgID := idgen.MustNew(idgen.PrefixExtension)
	verID := idgen.MustNew(idgen.PrefixExtensionVersion)
	insID := idgen.MustNew(idgen.PrefixInstallation)

	pkgInstallDir := filepath.Join(s.pluginsDir, verID)
	if err := os.Rename(extracted.StagingDir, pkgInstallDir); err != nil {
		return nil, fmt.Errorf("installer: finalize version dir %q: %w", verID, err)
	}
	extracted.StagingDir = pkgInstallDir
	cleanupFinalDir := true
	defer func() {
		if cleanupFinalDir {
			_ = SafeRemovePackageDir(s.pluginsDir, pkgInstallDir)
		}
	}()

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("installer: begin StageFreshInstall tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var (
		existingPkg       string
		reinstallInsID    string
		reinstallState    string
		reinstallVer      int64
		reusingPkg        bool
		resetDowngradeOrg []string
	)
	err = tx.QueryRowContext(ctx, `SELECT id FROM extension_packages WHERE canonical_name = ?`, vm.Manifest.Metadata.ID).Scan(&existingPkg)
	switch {
	case err == nil:
		err = tx.QueryRowContext(ctx, `
			SELECT id, state, version
			FROM extension_installations
			WHERE package_id = ?`, existingPkg,
		).Scan(&reinstallInsID, &reinstallState, &reinstallVer)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil && reinstallState != StateUninstalled && reinstallState != StateRejectedInvalid && reinstallState != StateCancelled {
			return nil, fmt.Errorf("%w: package %q is already registered in state %s (use StageUpgrade)", port.ErrAlreadyExists, vm.Manifest.Metadata.ID, reinstallState)
		}
		reusingPkg = true
		pkgID = existingPkg
		if perr := checkEnabledOrgPoliciesForPackageTx(ctx, tx, pkgID, prov.OriginTrustTier, prov.EnterpriseSigned); perr != nil {
			_ = tx.Rollback()
			s.emitAudit(ctx, req.TargetOrgID, req.ActorID, req.RequestID, "extension.org_trust_policy_denied", "extension_package", vm.Manifest.Metadata.ID, map[string]any{
				"origin_trust_tier": prov.OriginTrustTier,
				"enterprise_signed": prov.EnterpriseSigned,
			})
			return nil, perr
		}
		if prov.OriginTrustTier == lockfile.TrustTierUnverifiedExternal && !prov.EnterpriseSigned {
			var rerr error
			resetDowngradeOrg, rerr = resetEnabledOrgPoliciesOnUnsignedTransitionTx(ctx, tx, pkgID, now)
			if rerr != nil {
				return nil, rerr
			}
		}
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}

	if !reusingPkg {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms)
			VALUES (?, ?, ?, ?, ?, ?)`,
			pkgID, vm.Manifest.Metadata.ID, vm.ExtensionKind, vm.PublisherID, prov.OriginTrustTier, now,
		); err != nil {
			if isUniqueConstraintErr(err) {
				return nil, fmt.Errorf("%w: package %q already registered", port.ErrAlreadyExists, vm.Manifest.Metadata.ID)
			}
			return nil, fmt.Errorf("installer: insert extension_packages: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE extension_packages
			SET extension_kind = ?, publisher_id = ?, origin_trust_tier = ?
			WHERE id = ?`,
			vm.ExtensionKind, vm.PublisherID, prov.OriginTrustTier, pkgID,
		); err != nil {
			return nil, fmt.Errorf("installer: update extension_packages for reinstall: %w", err)
		}
		if reinstallInsID != "" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE extension_installations
				SET active_version_id = NULL, previous_pinned_version_id = NULL, candidate_version_id = NULL
				WHERE id = ?`, reinstallInsID,
			); err != nil {
				return nil, err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM extension_versions WHERE package_id = ?`, pkgID); err != nil {
			return nil, fmt.Errorf("installer: prune old uninstalled versions: %w", err)
		}
	}

	sigVerInt := boolToInt(prov.SignatureVerified)
	entSigInt := boolToInt(prov.EnterpriseSigned)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO extension_versions (
			id, package_id, semver, contract_version, core_version_range, execution_tier,
			bundle_sha256, signature_verified, enterprise_signed, signing_key_fingerprint, enterprise_key_fingerprint,
			source_uri, manifest_json, sbom_digest, created_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		verID, pkgID, vm.Manifest.Metadata.Version, vm.Manifest.Compatibility.ContractVersion,
		vm.Manifest.Compatibility.CoreVersionRange, vm.ExecutionTier, extracted.BundleSHA256,
		sigVerInt, entSigInt, nullStr(prov.SigningKeyFingerprint), nullStr(prov.EnterpriseKeyFingerprint), canonSourceURI,
		string(manifestJSON), nullStr(req.SBOMDigest), now,
	); err != nil {
		if isUniqueConstraintErr(err) {
			return nil, fmt.Errorf("%w: extension version already exists: %v", port.ErrAlreadyExists, err)
		}
		return nil, fmt.Errorf("installer: insert extension_versions: %w", err)
	}

	// Stage strictly with active_version_id = NULL and state = PENDING_PERMISSION_APPROVAL (SPEC-SEC-02).
	insVersion := int64(1)
	if reusingPkg && reinstallInsID != "" {
		insID = reinstallInsID
		insVersion = reinstallVer + 1
		if _, err := tx.ExecContext(ctx, `
			UPDATE extension_installations
			SET active_version_id = NULL, previous_pinned_version_id = NULL, candidate_version_id = ?,
			    state = ?, approved_permissions_json = '{}', approved_by_principal_id = NULL,
			    approved_at_ms = NULL, quarantine_reason = NULL, version = version + 1, updated_at_ms = ?
			WHERE id = ? AND version = ?`,
			verID, StatePendingPermissionApproval, now, insID, reinstallVer,
		); err != nil {
			return nil, fmt.Errorf("installer: reset extension_installations for reinstall: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO extension_installations (
				id, package_id, active_version_id, previous_pinned_version_id, candidate_version_id,
				state, approved_permissions_json, approved_by_principal_id, approved_at_ms,
				quarantine_reason, version, created_at_ms, updated_at_ms
			) VALUES (?, ?, NULL, NULL, ?, ?, '{}', NULL, NULL, NULL, 1, ?, ?)`,
			insID, pkgID, verID, StatePendingPermissionApproval, now, now,
		); err != nil {
			if isUniqueConstraintErr(err) {
				return nil, fmt.Errorf("%w: installation already exists: %v", port.ErrAlreadyExists, err)
			}
			return nil, fmt.Errorf("installer: insert extension_installations: %w", err)
		}
	}

	if _, err := appendExtensionChangelogTx(ctx, tx, "extension_installation", insID, port.MutationUpsert, false, now); err != nil {
		return nil, err
	}
	for _, resetOrgID := range resetDowngradeOrg {
		if _, aerr := s.emitAuditTx(ctx, tx, resetOrgID, req.ActorID, req.RequestID, "extension.org_policy_reset_on_trust_downgrade", "org_extension_policy", pkgID, map[string]any{
			"canonical_name":    vm.Manifest.Metadata.ID,
			"origin_trust_tier": prov.OriginTrustTier,
			"enterprise_signed": prov.EnterpriseSigned,
		}, now); aerr != nil {
			return nil, aerr
		}
	}
	if _, aerr := s.emitAuditTx(ctx, tx, req.TargetOrgID, req.ActorID, req.RequestID, "extension.staged_pending_approval", "extension_installation", insID, map[string]any{
		"canonical_name":    vm.Manifest.Metadata.ID,
		"version":           vm.Manifest.Metadata.Version,
		"origin_trust_tier": prov.OriginTrustTier,
		"enterprise_signed": prov.EnterpriseSigned,
		"execution_tier":    vm.ExecutionTier,
		"allowed_hosts":     vm.Permissions.AllowedHosts,
		"secret_slots":      vm.Permissions.SecretSlots,
	}, now); aerr != nil {
		return nil, aerr
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("installer: commit StageFreshInstall: %w", err)
	}
	committed = true
	cleanupFinalDir = false
	cleanupOnErr = false

	candSemver := vm.Manifest.Metadata.Version
	return &StageInstallResult{
		Installation: InstallationRecord{
			InstallationID:      insID,
			PackageID:           pkgID,
			CanonicalName:       vm.Manifest.Metadata.ID,
			OriginTrustTier:     prov.OriginTrustTier,
			CandidateVersionID:  &verID,
			CandidateSemver:     &candSemver,
			State:               StatePendingPermissionApproval,
			Version:             insVersion,
			ExtractedStagingDir: extracted.StagingDir,
			CreatedAtMs:         now,
			UpdatedAtMs:         now,
		},
		Manifest:   *vm,
		Provenance: *prov,
		Archive:    *extracted,
	}, nil
}

// ApproveInstallRequest carries principal identity, expected optimistic lock version, and target active state.
type ApproveInstallRequest struct {
	InstallationID      string
	ExpectedVersion     int64
	ApproverPrincipalID string
	ActivateImmediately bool // true -> ENABLED_ACTIVE; false -> INSTALLED_DISABLED
	OrgID               string
	RequestID           string
}

// ApproveAndActivate transitions PENDING_PERMISSION_APPROVAL -> ENABLED_ACTIVE | INSTALLED_DISABLED,
// verifies approver has OPERATOR / ORG_OWNER / SECURITY_REVIEWER role, re-checks extension_revocations
// inside the transaction (preventing staging-to-approval TOCTOU), records approved permissions,
// increments config_changelog, emits an AuditEvent, and atomically updates tanpopo.lock.
// Note: Optional organization-level Four-Eyes dual-approver governance (AUTH-12 / admin_change_requests)
// is owned and gated by TP-IMP-0041 (M4 v0.3).
func (s *LifecycleService) ApproveAndActivate(ctx context.Context, req ApproveInstallRequest) (*InstallationRecord, error) {
	if strings.TrimSpace(req.InstallationID) == "" || strings.TrimSpace(req.ApproverPrincipalID) == "" || req.ExpectedVersion < 1 {
		return nil, fmt.Errorf("%w: installation_id, approver_principal_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	targetState := StateInstalledDisabled
	if req.ActivateImmediately {
		targetState = StateEnabledActive
	}

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("installer: begin ApproveAndActivate tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureGlobalApproverRoleTx(ctx, tx, req.ApproverPrincipalID, req.OrgID); err != nil {
		return nil, err
	}

	rowData, err := loadInstallationForTx(ctx, tx, req.InstallationID)
	if err != nil {
		return nil, err
	}
	if rowData.version != req.ExpectedVersion {
		return nil, port.ErrVersionConflict
	}
	if rowData.state != StatePendingPermissionApproval || !IsValidSM01Transition(rowData.state, targetState) {
		return nil, fmt.Errorf("%w: cannot approve from %s -> %s", ErrInvalidStateTransition, rowData.state, targetState)
	}
	if rowData.candidateVerID == nil {
		return nil, fmt.Errorf("%w: installation missing candidate_version_id", ErrInvalidStateTransition)
	}

	candInfo, err := loadVersionInfoTx(ctx, tx, *rowData.candidateVerID)
	if err != nil {
		return nil, err
	}
	if rerr := checkRevocationsTx(ctx, tx, rowData.canonicalName, candInfo.semver, candInfo.bundleSHA256); rerr != nil {
		_ = tx.Rollback()
		s.emitAudit(ctx, req.OrgID, req.ApproverPrincipalID, req.RequestID, "extension.activation_blocked_revoked", "extension_installation", req.InstallationID, map[string]any{
			"canonical_name":    rowData.canonicalName,
			"candidate_version": candInfo.semver,
			"candidate_sha256":  candInfo.bundleSHA256,
		})
		return nil, rerr
	}
	candTrustTier := deriveTrustTierFromVersion(rowData.canonicalName, candInfo, s.keyring)
	candEntSigned := deriveEnterpriseSignedFromVersion(candInfo, s.keyring)
	if perr := checkEnabledOrgPoliciesForPackageTx(ctx, tx, rowData.packageID, candTrustTier, candEntSigned); perr != nil {
		_ = tx.Rollback()
		s.emitAudit(ctx, req.OrgID, req.ApproverPrincipalID, req.RequestID, "extension.org_trust_policy_denied", "extension_installation", req.InstallationID, map[string]any{
			"canonical_name":    rowData.canonicalName,
			"origin_trust_tier": candTrustTier,
			"enterprise_signed": candEntSigned,
		})
		return nil, perr
	}
	vm, err := ValidateManifestStruct(candInfo.manifest, s.valPolicy, nil)
	if err != nil {
		return nil, err
	}
	permsJSON, err := json.Marshal(vm.Permissions)
	if err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE extension_installations
		SET active_version_id = ?, candidate_version_id = NULL, state = ?,
		    approved_permissions_json = ?, approved_by_principal_id = ?, approved_at_ms = ?,
		    version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ? AND state = ?`,
		*rowData.candidateVerID, targetState, string(permsJSON), req.ApproverPrincipalID, now, now,
		req.InstallationID, req.ExpectedVersion, StatePendingPermissionApproval,
	)
	if err != nil {
		return nil, fmt.Errorf("installer: update installation on approve: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, port.ErrVersionConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE extension_packages SET origin_trust_tier = ? WHERE id = ?`, candTrustTier, rowData.packageID); err != nil {
		return nil, fmt.Errorf("installer: sync package origin_trust_tier on approve: %w", err)
	}

	if _, err := appendExtensionChangelogTx(ctx, tx, "extension_installation", req.InstallationID, port.MutationUpsert, false, now); err != nil {
		return nil, err
	}
	if err := s.preflightLockfile(); err != nil {
		return nil, err
	}
	if _, aerr := s.emitAuditTx(ctx, tx, req.OrgID, req.ApproverPrincipalID, req.RequestID, "extension.permissions_approved", "extension_installation", req.InstallationID, map[string]any{
		"canonical_name":    rowData.canonicalName,
		"activated_version": candInfo.semver,
		"new_state":         targetState,
		"allowed_hosts":     vm.Permissions.AllowedHosts,
		"secret_slots":      vm.Permissions.SecretSlots,
	}, now); aerr != nil {
		return nil, aerr
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("installer: commit ApproveAndActivate: %w", err)
	}
	committed = true

	if lerr := s.syncLockfileEntry(rowData.canonicalName, candTrustTier, candInfo, vm.Permissions, req.ApproverPrincipalID, now, nil); lerr != nil {
		s.emitAudit(ctx, req.OrgID, req.ApproverPrincipalID, req.RequestID, "extension.lockfile_sync_failed", "extension_installation", req.InstallationID, map[string]any{
			"canonical_name": rowData.canonicalName,
			"reason":         lerr.Error(),
		})
		return nil, fmt.Errorf("%w: %v", ErrLockfileSyncFailed, lerr)
	}

	return s.GetInstallation(ctx, req.InstallationID)
}

// StageUpgradeRequest initiates an extension upgrade from an active or disabled installation.
type StageUpgradeRequest struct {
	InstallationID  string
	ExpectedVersion int64
	ArchiveReader   io.Reader
	ArchivePath     string
	Source          SourceSpec
	Signature       ProvenanceSignature
	SBOMDigest      string
	ActorID         string
	OrgID           string
	RequestID       string
}

// StageUpgradeResult reports whether the upgrade cut over directly (DeltaP == 0) or
// halted at UPDATING_PENDING_RECONSENT with the prior active version serving live traffic (CT-18).
type StageUpgradeResult struct {
	Installation InstallationRecord
	Escalated    bool
	Delta        PermissionDelta
	Archive      ExtractedArchive
}

// StageUpgrade extracts & verifies a candidate version, computes DeltaP = P_new \ P_approved (CT-18):
//   - If DeltaP != 0: keeps active_version_id untouched and transitions to UPDATING_PENDING_RECONSENT.
//   - If DeltaP == 0: retains previous_pinned_version_id for < 5s rollback, activates v_new, and updates tanpopo.lock.
func (s *LifecycleService) StageUpgrade(ctx context.Context, req StageUpgradeRequest) (*StageUpgradeResult, error) {
	if strings.TrimSpace(req.InstallationID) == "" || strings.TrimSpace(req.ActorID) == "" || req.ExpectedVersion < 1 {
		return nil, fmt.Errorf("%w: installation_id, actor_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	canonSourceURI, err := ValidateSourceRef(req.Source)
	if err != nil {
		return nil, err
	}

	extracted, err := s.extractRequestArchive(req.ArchiveReader, req.ArchivePath)
	if err != nil {
		return nil, err
	}
	cleanupOnErr := true
	defer func() {
		if cleanupOnErr {
			_ = SafeRemovePackageDir(s.pluginsDir, extracted.StagingDir)
		}
	}()

	vm, err := ParseAndValidateManifest(extracted.ManifestBytes, s.valPolicy, extracted.FileSHA256)
	if err != nil {
		return nil, err
	}
	prov, err := VerifyProvenance(vm, extracted.BundleSHA256, req.Source.ExpectedSHA256, req.Signature, s.keyring)
	if err != nil {
		return nil, err
	}
	if err := s.checkRevocations(ctx, vm.Manifest.Metadata.ID, vm.Manifest.Metadata.Version, extracted.BundleSHA256); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	newVerID := idgen.MustNew(idgen.PrefixExtensionVersion)
	newVerDir := filepath.Join(s.pluginsDir, newVerID)
	if err := os.Rename(extracted.StagingDir, newVerDir); err != nil {
		return nil, fmt.Errorf("installer: finalize version dir %q: %w", newVerID, err)
	}
	extracted.StagingDir = newVerDir
	cleanupUpgradeDir := true
	defer func() {
		if cleanupUpgradeDir {
			_ = SafeRemovePackageDir(s.pluginsDir, newVerDir)
		}
	}()

	now := s.nowMs()
	manifestJSON, err := json.Marshal(vm.Manifest)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("installer: begin StageUpgrade tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureGlobalApproverRoleTx(ctx, tx, req.ActorID, req.OrgID); err != nil {
		return nil, err
	}

	rowData, err := loadInstallationForTx(ctx, tx, req.InstallationID)
	if err != nil {
		return nil, err
	}
	if rowData.version != req.ExpectedVersion {
		return nil, port.ErrVersionConflict
	}
	if rowData.canonicalName != vm.Manifest.Metadata.ID {
		return nil, fmt.Errorf("%w: package name mismatch (installed=%q candidate=%q)", port.ErrInvalidInput, rowData.canonicalName, vm.Manifest.Metadata.ID)
	}
	if rowData.state != StateEnabledActive && rowData.state != StateInstalledDisabled {
		return nil, fmt.Errorf("%w: upgrade requires ENABLED_ACTIVE or INSTALLED_DISABLED (got %s)", ErrInvalidStateTransition, rowData.state)
	}
	if rowData.activeVerID == nil {
		return nil, fmt.Errorf("%w: installation missing active_version_id", ErrInvalidStateTransition)
	}

	oldActiveInfo, err := loadVersionInfoTx(ctx, tx, *rowData.activeVerID)
	if err != nil {
		return nil, err
	}
	curSV, cerr1 := parseSemver(oldActiveInfo.semver)
	candSV, cerr2 := parseSemver(vm.Manifest.Metadata.Version)
	if cerr1 != nil || cerr2 != nil || compareSemver(candSV, curSV) <= 0 {
		return nil, fmt.Errorf("%w: candidate version %q must be strictly newer than active version %q (use RollbackInstallation for downgrades)", port.ErrInvalidInput, vm.Manifest.Metadata.Version, oldActiveInfo.semver)
	}

	// Enforce CT-20 across any enabled org_extension_policies so upgrades cannot strip enterprise_signed protection.
	if err := checkEnabledOrgPoliciesForPackageTx(ctx, tx, rowData.packageID, prov.OriginTrustTier, prov.EnterpriseSigned); err != nil {
		return nil, err
	}
	oldActiveTrust := deriveTrustTierFromVersion(rowData.canonicalName, oldActiveInfo, s.keyring)
	oldActiveEnt := deriveEnterpriseSignedFromVersion(oldActiveInfo, s.keyring)
	prevActiveUnsigned := oldActiveTrust == lockfile.TrustTierUnverifiedExternal && !oldActiveEnt
	var resetDowngradeOrg []string
	if prov.OriginTrustTier == lockfile.TrustTierUnverifiedExternal && !prov.EnterpriseSigned && !prevActiveUnsigned {
		var rerr error
		resetDowngradeOrg, rerr = resetEnabledOrgPoliciesOnUnsignedTransitionTx(ctx, tx, rowData.packageID, now)
		if rerr != nil {
			return nil, rerr
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO extension_versions (
			id, package_id, semver, contract_version, core_version_range, execution_tier,
			bundle_sha256, signature_verified, enterprise_signed, signing_key_fingerprint, enterprise_key_fingerprint,
			source_uri, manifest_json, sbom_digest, created_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newVerID, rowData.packageID, vm.Manifest.Metadata.Version, vm.Manifest.Compatibility.ContractVersion,
		vm.Manifest.Compatibility.CoreVersionRange, vm.ExecutionTier, extracted.BundleSHA256,
		boolToInt(prov.SignatureVerified), boolToInt(prov.EnterpriseSigned), nullStr(prov.SigningKeyFingerprint), nullStr(prov.EnterpriseKeyFingerprint),
		canonSourceURI, string(manifestJSON), nullStr(req.SBOMDigest), now,
	); err != nil {
		if isUniqueConstraintErr(err) {
			return nil, fmt.Errorf("%w: candidate version %q already registered: %v", port.ErrAlreadyExists, vm.Manifest.Metadata.Version, err)
		}
		return nil, fmt.Errorf("installer: insert candidate version: %w", err)
	}

	delta := ComputePermissionDelta(rowData.approvedPerms, vm.Permissions)
	if delta.HasEscalation() {
		if rowData.state != StateEnabledActive {
			return nil, fmt.Errorf("%w: escalating upgrade requires ENABLED_ACTIVE or explicit staging (got %s)", ErrInvalidStateTransition, rowData.state)
		}
		// CT-18: Keep active_version_id UNCHANGED so live traffic continues seamlessly on v_old.
		res, uerr := tx.ExecContext(ctx, `
			UPDATE extension_installations
			SET candidate_version_id = ?, state = ?, version = version + 1, updated_at_ms = ?
			WHERE id = ? AND version = ?`,
			newVerID, StateUpdatingPendingReconsent, now, req.InstallationID, req.ExpectedVersion,
		)
		if uerr != nil {
			return nil, uerr
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			return nil, port.ErrVersionConflict
		}
		if _, cerr := appendExtensionChangelogTx(ctx, tx, "extension_installation", req.InstallationID, port.MutationUpsert, false, now); cerr != nil {
			return nil, cerr
		}
		for _, resetOrgID := range resetDowngradeOrg {
			if _, aerr := s.emitAuditTx(ctx, tx, resetOrgID, req.ActorID, req.RequestID, "extension.org_policy_reset_on_trust_downgrade", "org_extension_policy", rowData.packageID, map[string]any{
				"canonical_name":    rowData.canonicalName,
				"origin_trust_tier": prov.OriginTrustTier,
				"enterprise_signed": prov.EnterpriseSigned,
			}, now); aerr != nil {
				return nil, aerr
			}
		}
		if _, aerr := s.emitAuditTx(ctx, tx, req.OrgID, req.ActorID, req.RequestID, "extension.upgrade_pending_reconsent", "extension_installation", req.InstallationID, map[string]any{
			"canonical_name":    rowData.canonicalName,
			"active_version":    oldActiveInfo.semver,
			"candidate_version": vm.Manifest.Metadata.Version,
			"delta":             delta,
		}, now); aerr != nil {
			return nil, aerr
		}
		if cerr := tx.Commit(); cerr != nil {
			return nil, cerr
		}
		committed = true
		cleanupUpgradeDir = false
		cleanupOnErr = false

		rec, gerr := s.GetInstallation(ctx, req.InstallationID)
		if gerr != nil {
			return nil, gerr
		}
		rec.ExtractedStagingDir = extracted.StagingDir
		return &StageUpgradeResult{
			Installation: *rec,
			Escalated:    true,
			Delta:        delta,
			Archive:      *extracted,
		}, nil
	}

	// Non-escalating upgrade (DeltaP == empty): retain previous_pinned_version_id, synchronize normalized approved permissions, and cut over.
	candInfo, err := loadVersionInfoTx(ctx, tx, newVerID)
	if err != nil {
		return nil, err
	}
	permsJSON, err := json.Marshal(vm.Permissions)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE extension_installations
		SET previous_pinned_version_id = ?, active_version_id = ?, candidate_version_id = NULL,
		    state = ?, approved_permissions_json = ?, version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ?`,
		*rowData.activeVerID, newVerID, rowData.state, string(permsJSON), now, req.InstallationID, req.ExpectedVersion,
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, port.ErrVersionConflict
	}
	// CT-20: Keep extension_packages.origin_trust_tier bound to the newly activated version's verified provenance.
	if _, err := tx.ExecContext(ctx, `UPDATE extension_packages SET origin_trust_tier = ? WHERE id = ?`, prov.OriginTrustTier, rowData.packageID); err != nil {
		return nil, fmt.Errorf("installer: sync package origin_trust_tier: %w", err)
	}
	if _, err := appendExtensionChangelogTx(ctx, tx, "extension_installation", req.InstallationID, port.MutationUpsert, false, now); err != nil {
		return nil, err
	}
	if err := s.preflightLockfile(); err != nil {
		return nil, err
	}
	for _, resetOrgID := range resetDowngradeOrg {
		if _, aerr := s.emitAuditTx(ctx, tx, resetOrgID, req.ActorID, req.RequestID, "extension.org_policy_reset_on_trust_downgrade", "org_extension_policy", rowData.packageID, map[string]any{
			"canonical_name":    rowData.canonicalName,
			"origin_trust_tier": prov.OriginTrustTier,
			"enterprise_signed": prov.EnterpriseSigned,
		}, now); aerr != nil {
			return nil, aerr
		}
	}
	if _, aerr := s.emitAuditTx(ctx, tx, req.OrgID, req.ActorID, req.RequestID, "extension.upgraded", "extension_installation", req.InstallationID, map[string]any{
		"canonical_name":   rowData.canonicalName,
		"previous_version": oldActiveInfo.semver,
		"active_version":   candInfo.semver,
	}, now); aerr != nil {
		return nil, aerr
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	cleanupUpgradeDir = false
	cleanupOnErr = false

	approver := derefStr(rowData.approvedBy)
	if approver == "" {
		approver = req.ActorID
	}
	if lerr := s.syncLockfileEntry(
		rowData.canonicalName, prov.OriginTrustTier, candInfo, vm.Permissions, approver, now,
		&lockfile.PinnedRollback{Version: oldActiveInfo.semver, ArtifactSHA256: oldActiveInfo.bundleSHA256},
	); lerr != nil {
		s.emitAudit(ctx, req.OrgID, req.ActorID, req.RequestID, "extension.lockfile_sync_failed", "extension_installation", req.InstallationID, map[string]any{
			"canonical_name": rowData.canonicalName,
			"reason":         lerr.Error(),
		})
		return nil, fmt.Errorf("%w: %v", ErrLockfileSyncFailed, lerr)
	}

	rec, gerr := s.GetInstallation(ctx, req.InstallationID)
	if gerr != nil {
		return nil, gerr
	}
	rec.ExtractedStagingDir = extracted.StagingDir
	return &StageUpgradeResult{
		Installation: *rec,
		Escalated:    false,
		Delta:        delta,
		Archive:      *extracted,
	}, nil
}

// RequestActivePermissionReconsent stages an explicit SM-01 permission re-consent request
// (ENABLED_ACTIVE -> UPDATING_PENDING_RECONSENT) when an existing installation's active version
// manifest declares capabilities/secret slots missing from approved_permissions_json (legacy rows),
// without modifying approved_permissions_json prior to explicit approval.
func (s *LifecycleService) RequestActivePermissionReconsent(
	ctx context.Context,
	installationID string,
	expectedVersion int64,
	actorID string,
	orgID string,
	requestID string,
) (*InstallationRecord, PermissionDelta, error) {
	if strings.TrimSpace(installationID) == "" || strings.TrimSpace(actorID) == "" || expectedVersion < 1 {
		return nil, PermissionDelta{}, fmt.Errorf("%w: installation_id, actor_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, PermissionDelta{}, fmt.Errorf("installer: begin RequestActivePermissionReconsent tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureGlobalApproverRoleTx(ctx, tx, actorID, orgID); err != nil {
		return nil, PermissionDelta{}, err
	}

	rowData, err := loadInstallationForTx(ctx, tx, installationID)
	if err != nil {
		return nil, PermissionDelta{}, err
	}
	if rowData.version != expectedVersion {
		return nil, PermissionDelta{}, port.ErrVersionConflict
	}
	if rowData.state != StateEnabledActive || rowData.activeVerID == nil {
		return nil, PermissionDelta{}, fmt.Errorf("%w: active permission re-consent requires ENABLED_ACTIVE with an active version (state=%s)", ErrInvalidStateTransition, rowData.state)
	}
	if !IsValidSM01Transition(rowData.state, StateUpdatingPendingReconsent) {
		return nil, PermissionDelta{}, fmt.Errorf("%w: %s -> %s", ErrInvalidStateTransition, rowData.state, StateUpdatingPendingReconsent)
	}

	activeInfo, err := loadVersionInfoTx(ctx, tx, *rowData.activeVerID)
	if err != nil {
		return nil, PermissionDelta{}, err
	}
	if rerr := checkRevocationsTx(ctx, tx, rowData.canonicalName, activeInfo.semver, activeInfo.bundleSHA256); rerr != nil {
		_ = tx.Rollback()
		s.emitAudit(ctx, orgID, actorID, requestID, "extension.reconsent_blocked_revoked", "extension_installation", installationID, map[string]any{
			"canonical_name": rowData.canonicalName,
			"active_version": activeInfo.semver,
		})
		return nil, PermissionDelta{}, rerr
	}

	activeTrustTier := deriveTrustTierFromVersion(rowData.canonicalName, activeInfo, s.keyring)
	activeEntSigned := deriveEnterpriseSignedFromVersion(activeInfo, s.keyring)
	if err := checkEnabledOrgPoliciesForPackageTx(ctx, tx, rowData.packageID, activeTrustTier, activeEntSigned); err != nil {
		return nil, PermissionDelta{}, err
	}

	vm, err := ValidateManifestStruct(activeInfo.manifest, s.valPolicy, nil)
	if err != nil {
		return nil, PermissionDelta{}, err
	}
	delta := ComputePermissionDelta(rowData.approvedPerms, vm.Permissions)
	if !delta.HasEscalation() {
		return nil, PermissionDelta{}, fmt.Errorf("%w: active version has no unapproved permissions or slots requiring re-consent", ErrInvalidStateTransition)
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE extension_installations
		SET candidate_version_id = ?, state = ?, version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ? AND state = ?`,
		*rowData.activeVerID, StateUpdatingPendingReconsent, now,
		installationID, expectedVersion, StateEnabledActive,
	)
	if err != nil {
		return nil, PermissionDelta{}, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, PermissionDelta{}, port.ErrVersionConflict
	}
	if _, err := appendExtensionChangelogTx(ctx, tx, "extension_installation", installationID, port.MutationUpsert, false, now); err != nil {
		return nil, PermissionDelta{}, err
	}
	if _, aerr := s.emitAuditTx(ctx, tx, orgID, actorID, requestID, "extension.active_permission_reconsent_requested", "extension_installation", installationID, map[string]any{
		"canonical_name": rowData.canonicalName,
		"active_version": activeInfo.semver,
		"delta":          delta,
	}, now); aerr != nil {
		return nil, PermissionDelta{}, aerr
	}
	if err := tx.Commit(); err != nil {
		return nil, PermissionDelta{}, err
	}
	committed = true

	rec, gerr := s.GetInstallation(ctx, installationID)
	if gerr != nil {
		return nil, PermissionDelta{}, gerr
	}
	return rec, delta, nil
}

// ApproveEscalatedUpgrade grants DeltaP consent on an UPDATING_PENDING_RECONSENT installation,
// re-checks extension_revocations inside the transaction (preventing TOCTOU), cuts over via
// DRAINING -> ENABLED_ACTIVE, retains previous_pinned_version_id, updates package trust tier,
// and updates tanpopo.lock.
// Note: Optional organization-level Four-Eyes dual-approver governance (AUTH-12 / admin_change_requests)
// is owned and gated by TP-IMP-0041 (M4 v0.3).
func (s *LifecycleService) ApproveEscalatedUpgrade(ctx context.Context, installationID string, expectedVersion int64, approverPrincipalID, orgID, requestID string) (*InstallationRecord, error) {
	if strings.TrimSpace(installationID) == "" || strings.TrimSpace(approverPrincipalID) == "" || expectedVersion < 1 {
		return nil, fmt.Errorf("%w: invalid arguments", port.ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureGlobalApproverRoleTx(ctx, tx, approverPrincipalID, orgID); err != nil {
		return nil, err
	}

	rowData, err := loadInstallationForTx(ctx, tx, installationID)
	if err != nil {
		return nil, err
	}
	if rowData.version != expectedVersion {
		return nil, port.ErrVersionConflict
	}
	if rowData.state != StateUpdatingPendingReconsent || rowData.activeVerID == nil || rowData.candidateVerID == nil {
		return nil, fmt.Errorf("%w: installation is not awaiting escalation re-consent (state=%s)", ErrInvalidStateTransition, rowData.state)
	}

	oldInfo, err := loadVersionInfoTx(ctx, tx, *rowData.activeVerID)
	if err != nil {
		return nil, err
	}
	candInfo, err := loadVersionInfoTx(ctx, tx, *rowData.candidateVerID)
	if err != nil {
		return nil, err
	}
	if rerr := checkRevocationsTx(ctx, tx, rowData.canonicalName, candInfo.semver, candInfo.bundleSHA256); rerr != nil {
		_ = tx.Rollback()
		s.emitAudit(ctx, orgID, approverPrincipalID, requestID, "extension.activation_blocked_revoked", "extension_installation", installationID, map[string]any{
			"canonical_name":    rowData.canonicalName,
			"candidate_version": candInfo.semver,
			"candidate_sha256":  candInfo.bundleSHA256,
		})
		return nil, rerr
	}
	candTrustTier := deriveTrustTierFromVersion(rowData.canonicalName, candInfo, s.keyring)
	candEntSigned := deriveEnterpriseSignedFromVersion(candInfo, s.keyring)
	if err := checkEnabledOrgPoliciesForPackageTx(ctx, tx, rowData.packageID, candTrustTier, candEntSigned); err != nil {
		return nil, err
	}
	vm, err := ValidateManifestStruct(candInfo.manifest, s.valPolicy, nil)
	if err != nil {
		return nil, err
	}
	permsJSON, err := json.Marshal(vm.Permissions)
	if err != nil {
		return nil, err
	}

	sameVersionReconsent := *rowData.candidateVerID == *rowData.activeVerID
	prevPinnedForUpdate := rowData.activeVerID
	if sameVersionReconsent {
		prevPinnedForUpdate = rowData.prevPinnedVerID
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE extension_installations
		SET previous_pinned_version_id = ?, active_version_id = ?, candidate_version_id = NULL,
		    state = ?, approved_permissions_json = ?, approved_by_principal_id = ?, approved_at_ms = ?,
		    version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ?`,
		nullStrPtr(prevPinnedForUpdate), *rowData.candidateVerID, StateEnabledActive,
		string(permsJSON), approverPrincipalID, now, now, installationID, expectedVersion,
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, port.ErrVersionConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE extension_packages SET origin_trust_tier = ? WHERE id = ?`, candTrustTier, rowData.packageID); err != nil {
		return nil, fmt.Errorf("installer: sync package origin_trust_tier: %w", err)
	}
	if _, err := appendExtensionChangelogTx(ctx, tx, "extension_installation", installationID, port.MutationUpsert, false, now); err != nil {
		return nil, err
	}
	var rollbackPin *lockfile.PinnedRollback
	if !sameVersionReconsent {
		rollbackPin = &lockfile.PinnedRollback{Version: oldInfo.semver, ArtifactSHA256: oldInfo.bundleSHA256}
	} else if rowData.prevPinnedVerID != nil {
		if prevInfo, perr := loadVersionInfoTx(ctx, tx, *rowData.prevPinnedVerID); perr == nil {
			rollbackPin = &lockfile.PinnedRollback{Version: prevInfo.semver, ArtifactSHA256: prevInfo.bundleSHA256}
		} else if !errors.Is(perr, port.ErrNotFound) {
			return nil, perr
		}
	}
	if err := s.preflightLockfile(); err != nil {
		return nil, err
	}
	if _, aerr := s.emitAuditTx(ctx, tx, orgID, approverPrincipalID, requestID, "extension.escalated_upgrade_approved", "extension_installation", installationID, map[string]any{
		"canonical_name":   rowData.canonicalName,
		"previous_version": oldInfo.semver,
		"active_version":   candInfo.semver,
	}, now); aerr != nil {
		return nil, aerr
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	if lerr := s.syncLockfileEntry(
		rowData.canonicalName, candTrustTier, candInfo, vm.Permissions, approverPrincipalID, now,
		rollbackPin,
	); lerr != nil {
		s.emitAudit(ctx, orgID, approverPrincipalID, requestID, "extension.lockfile_sync_failed", "extension_installation", installationID, map[string]any{
			"canonical_name": rowData.canonicalName,
			"reason":         lerr.Error(),
		})
		return nil, fmt.Errorf("%w: %v", ErrLockfileSyncFailed, lerr)
	}

	return s.GetInstallation(ctx, installationID)
}

// RejectEscalatedUpgrade discards candidate_version_id and restores ENABLED_ACTIVE on the prior version.
func (s *LifecycleService) RejectEscalatedUpgrade(ctx context.Context, installationID string, expectedVersion int64, actorID, orgID, requestID string) (*InstallationRecord, error) {
	if strings.TrimSpace(installationID) == "" || strings.TrimSpace(actorID) == "" || expectedVersion < 1 {
		return nil, fmt.Errorf("%w: installation_id, actor_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureGlobalApproverRoleTx(ctx, tx, actorID, orgID); err != nil {
		return nil, err
	}

	rowData, err := loadInstallationForTx(ctx, tx, installationID)
	if err != nil {
		return nil, err
	}
	if rowData.version != expectedVersion {
		return nil, port.ErrVersionConflict
	}
	if rowData.state != StateUpdatingPendingReconsent {
		return nil, fmt.Errorf("%w: expected UPDATING_PENDING_RECONSENT, got %s", ErrInvalidStateTransition, rowData.state)
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE extension_installations
		SET candidate_version_id = NULL, state = ?, version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ?`,
		StateEnabledActive, now, installationID, expectedVersion,
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, port.ErrVersionConflict
	}
	if _, err := appendExtensionChangelogTx(ctx, tx, "extension_installation", installationID, port.MutationUpsert, false, now); err != nil {
		return nil, err
	}
	if _, aerr := s.emitAuditTx(ctx, tx, orgID, actorID, requestID, "extension.escalated_upgrade_rejected", "extension_installation", installationID, map[string]any{
		"canonical_name": rowData.canonicalName,
	}, now); aerr != nil {
		return nil, aerr
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	return s.GetInstallation(ctx, installationID)
}

// RollbackInstallation atomically swaps active_version_id back to previous_pinned_version_id (< 5s target),
// checks extension_revocations against the rollback target (CR-06), clears previous_pinned_version_id to
// prevent unintended double-rollback ping-pong, and updates config_changelog, AuditSink, and tanpopo.lock.
func (s *LifecycleService) RollbackInstallation(ctx context.Context, installationID string, expectedVersion int64, actorID, orgID, requestID string) (*InstallationRecord, error) {
	if strings.TrimSpace(installationID) == "" || strings.TrimSpace(actorID) == "" || expectedVersion < 1 {
		return nil, fmt.Errorf("%w: installation_id, actor_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureGlobalApproverRoleTx(ctx, tx, actorID, orgID); err != nil {
		return nil, err
	}

	rowData, err := loadInstallationForTx(ctx, tx, installationID)
	if err != nil {
		return nil, err
	}
	if rowData.version != expectedVersion {
		return nil, port.ErrVersionConflict
	}
	if !IsValidSM01Transition(rowData.state, StateRolledBack) {
		return nil, fmt.Errorf("%w: cannot roll back from state %s", ErrInvalidStateTransition, rowData.state)
	}
	if rowData.prevPinnedVerID == nil || rowData.activeVerID == nil {
		return nil, ErrNoRollbackTarget
	}

	targetPrevInfo, err := loadVersionInfoTx(ctx, tx, *rowData.prevPinnedVerID)
	if err != nil {
		return nil, err
	}
	curActiveInfo, err := loadVersionInfoTx(ctx, tx, *rowData.activeVerID)
	if err != nil {
		return nil, err
	}

	// CR-06: Never allow rollback to re-activate a CRITICAL / CRITICAL_MALICIOUS revoked version.
	if rerr := checkRevocationsTx(ctx, tx, rowData.canonicalName, targetPrevInfo.semver, targetPrevInfo.bundleSHA256); rerr != nil {
		_ = tx.Rollback()
		s.emitAudit(ctx, orgID, actorID, requestID, "extension.rollback_blocked_revoked", "extension_installation", installationID, map[string]any{
			"canonical_name": rowData.canonicalName,
			"target_version": targetPrevInfo.semver,
			"target_sha256":  targetPrevInfo.bundleSHA256,
		})
		return nil, rerr
	}

	restoredTrustTier := deriveTrustTierFromVersion(rowData.canonicalName, targetPrevInfo, s.keyring)
	restoredEntSigned := deriveEnterpriseSignedFromVersion(targetPrevInfo, s.keyring)
	if err := checkEnabledOrgPoliciesForPackageTx(ctx, tx, rowData.packageID, restoredTrustTier, restoredEntSigned); err != nil {
		return nil, err
	}
	curActiveTrust := deriveTrustTierFromVersion(rowData.canonicalName, curActiveInfo, s.keyring)
	curActiveEnt := deriveEnterpriseSignedFromVersion(curActiveInfo, s.keyring)
	curActiveUnsigned := curActiveTrust == lockfile.TrustTierUnverifiedExternal && !curActiveEnt
	var resetDowngradeOrg []string
	if restoredTrustTier == lockfile.TrustTierUnverifiedExternal && !restoredEntSigned && !curActiveUnsigned {
		var rerr error
		resetDowngradeOrg, rerr = resetEnabledOrgPoliciesOnUnsignedTransitionTx(ctx, tx, rowData.packageID, now)
		if rerr != nil {
			return nil, rerr
		}
	}

	vm, err := ValidateManifestStruct(targetPrevInfo.manifest, s.valPolicy, nil)
	if err != nil {
		return nil, err
	}
	permsJSON, err := json.Marshal(vm.Permissions)
	if err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE extension_installations
		SET active_version_id = ?, previous_pinned_version_id = NULL, candidate_version_id = NULL,
		    state = ?, approved_permissions_json = ?, version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ?`,
		targetPrevInfo.id, StateEnabledActive, string(permsJSON), now, installationID, expectedVersion,
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, port.ErrVersionConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE extension_packages SET origin_trust_tier = ? WHERE id = ?`, restoredTrustTier, rowData.packageID); err != nil {
		return nil, fmt.Errorf("installer: restore package origin_trust_tier on rollback: %w", err)
	}
	if _, err := appendExtensionChangelogTx(ctx, tx, "extension_installation", installationID, port.MutationUpsert, true, now); err != nil {
		return nil, err
	}
	if err := s.preflightLockfile(); err != nil {
		return nil, err
	}
	for _, resetOrgID := range resetDowngradeOrg {
		if _, aerr := s.emitAuditTx(ctx, tx, resetOrgID, actorID, requestID, "extension.org_policy_reset_on_trust_downgrade", "org_extension_policy", rowData.packageID, map[string]any{
			"canonical_name":    rowData.canonicalName,
			"origin_trust_tier": restoredTrustTier,
			"enterprise_signed": targetPrevInfo.enterpriseSigned,
		}, now); aerr != nil {
			return nil, aerr
		}
	}
	if _, aerr := s.emitAuditTx(ctx, tx, orgID, actorID, requestID, "extension.rolled_back", "extension_installation", installationID, map[string]any{
		"canonical_name":   rowData.canonicalName,
		"rolled_back_from": curActiveInfo.semver,
		"restored_version": targetPrevInfo.semver,
	}, now); aerr != nil {
		return nil, aerr
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	approver := derefStr(rowData.approvedBy)
	if approver == "" {
		approver = actorID
	}
	if lerr := s.syncLockfileEntry(
		rowData.canonicalName, restoredTrustTier, targetPrevInfo, vm.Permissions, approver, now, nil,
	); lerr != nil {
		s.emitAudit(ctx, orgID, actorID, requestID, "extension.lockfile_sync_failed", "extension_installation", installationID, map[string]any{
			"canonical_name": rowData.canonicalName,
			"reason":         lerr.Error(),
		})
		return nil, fmt.Errorf("%w: %v", ErrLockfileSyncFailed, lerr)
	}

	return s.GetInstallation(ctx, installationID)
}

// QuarantineInstallation transitions any non-terminal SM-01 installation immediately to QUARANTINED,
// emits an emergency config_changelog entry (MUTATION='QUARANTINE', emergency=1), and records a
// high-severity AuditEvent. Exported for fault_guard integration in TP-IMP-0013 (FMEA-05).
func (s *LifecycleService) QuarantineInstallation(ctx context.Context, installationID, reason, actorID string) error {
	if strings.TrimSpace(installationID) == "" || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: installation_id and quarantine reason are required", port.ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	rowData, err := loadInstallationForTx(ctx, tx, installationID)
	if err != nil {
		return err
	}
	if rowData.state == StateQuarantined {
		return nil // idempotent quarantine
	}
	if !IsValidSM01Transition(rowData.state, StateQuarantined) {
		return fmt.Errorf("%w: cannot quarantine installation in state %s", ErrInvalidStateTransition, rowData.state)
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE extension_installations
		SET state = ?, quarantine_reason = ?, version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ?`,
		StateQuarantined, reason, now, installationID, rowData.version,
	)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return port.ErrVersionConflict
	}
	if _, err := appendExtensionChangelogTx(ctx, tx, "extension_installation", installationID, port.MutationQuarantine, true, now); err != nil {
		return err
	}
	if _, aerr := s.emitAuditTx(ctx, tx, "", actorID, "", "extension.quarantined", "extension_installation", installationID, map[string]any{
		"canonical_name":    rowData.canonicalName,
		"previous_state":    rowData.state,
		"quarantine_reason": reason,
	}, now); aerr != nil {
		return aerr
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true

	return nil
}

// VerifyPinnedArchiveOnLoad verifies an archive SHA-256 against tanpopo.lock and records
// a high-severity security AuditEvent if tampered by even 1 byte (CT-03).
func (s *LifecycleService) VerifyPinnedArchiveOnLoad(ctx context.Context, packageName, actualBundleSHA256, actorID string) error {
	if strings.TrimSpace(s.lockfilePath) == "" {
		return fmt.Errorf("%w: lockfilePath is not configured", ErrLockfileIntegrity)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	lf, err := lockfile.Read(s.lockfilePath)
	if err != nil {
		s.emitAudit(ctx, "", actorID, "", "extension.lockfile_read_failed", "extension_package", packageName, map[string]any{
			"reason": err.Error(),
		})
		return err
	}
	if err := lf.VerifyArtifactDigest(packageName, actualBundleSHA256); err != nil {
		s.emitAudit(ctx, "", actorID, "", "extension.artifact_digest_mismatch", "extension_package", packageName, map[string]any{
			"actual_sha256": actualBundleSHA256,
			"reason":        err.Error(),
		})
		return err
	}
	return nil
}

// SetOrgExtensionPolicy enforces organizations.min_plugin_trust (CT-20 / CR-03) and RBAC before
// enabling or pinning an extension package inside orgID via org_extension_policies.
func (s *LifecycleService) SetOrgExtensionPolicy(ctx context.Context, orgID, packageID string, enabled bool, pinnedInstallationID *string, actorID string) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(packageID) == "" || strings.TrimSpace(actorID) == "" {
		return fmt.Errorf("%w: org_id, package_id, and actor_id required", port.ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureApproverRoleTx(ctx, tx, actorID, orgID); err != nil {
		return err
	}

	var minTrust string
	if err := tx.QueryRowContext(ctx, `SELECT min_plugin_trust FROM organizations WHERE id = ?`, orgID).Scan(&minTrust); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return port.ErrNotFound
		}
		return err
	}
	var canonicalName string
	if err := tx.QueryRowContext(ctx, `SELECT canonical_name FROM extension_packages WHERE id = ?`, packageID).Scan(&canonicalName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return port.ErrNotFound
		}
		return err
	}

	// Never fall back to stale package-level origin_trust_tier when no active/candidate version exists,
	// and inspect BOTH active_version_id and candidate_version_id so staged downgrades cannot bypass CT-20 or TP-BR-0202.
	type versionTrustCheck struct {
		originTrust      string
		enterpriseSigned bool
	}
	var checks []versionTrustCheck
	seenVerIDs := make(map[string]struct{})
	var pinnedParam *string

	collectVersions := func(actID, candID sql.NullString) error {
		for _, nid := range []sql.NullString{actID, candID} {
			if !nid.Valid || strings.TrimSpace(nid.String) == "" {
				continue
			}
			vid := strings.TrimSpace(nid.String)
			if _, ok := seenVerIDs[vid]; ok {
				continue
			}
			seenVerIDs[vid] = struct{}{}
			verInfo, verr := loadVersionInfoTx(ctx, tx, vid)
			if verr != nil {
				return verr
			}
			checks = append(checks, versionTrustCheck{
				originTrust:      deriveTrustTierFromVersion(canonicalName, verInfo, s.keyring),
				enterpriseSigned: deriveEnterpriseSignedFromVersion(verInfo, s.keyring),
			})
		}
		return nil
	}

	if pinnedInstallationID != nil && strings.TrimSpace(*pinnedInstallationID) != "" {
		pinID := strings.TrimSpace(*pinnedInstallationID)
		pinnedParam = &pinID
		var actID, candID sql.NullString
		err := tx.QueryRowContext(ctx, `
			SELECT ei.active_version_id, ei.candidate_version_id
			FROM extension_installations ei
			WHERE ei.id = ? AND ei.package_id = ? AND ei.state NOT IN (?, ?, ?)`,
			pinID, packageID, StateUninstalled, StateRejectedInvalid, StateCancelled,
		).Scan(&actID, &candID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return port.ErrNotFound
			}
			return err
		}
		if cerr := collectVersions(actID, candID); cerr != nil {
			return cerr
		}
	} else {
		rows, qerr := tx.QueryContext(ctx, `
			SELECT ei.active_version_id, ei.candidate_version_id
			FROM extension_installations ei
			WHERE ei.package_id = ? AND ei.state NOT IN (?, ?, ?)`,
			packageID, StateUninstalled, StateRejectedInvalid, StateCancelled,
		)
		if qerr != nil {
			return qerr
		}
		type verPair struct{ act, cand sql.NullString }
		var pairs []verPair
		for rows.Next() {
			var p verPair
			if serr := rows.Scan(&p.act, &p.cand); serr != nil {
				_ = rows.Close()
				return serr
			}
			pairs = append(pairs, p)
		}
		if rerr := rows.Err(); rerr != nil {
			_ = rows.Close()
			return rerr
		}
		_ = rows.Close()
		for _, p := range pairs {
			if cerr := collectVersions(p.act, p.cand); cerr != nil {
				return cerr
			}
		}
	}
	if len(checks) == 0 {
		checks = append(checks, versionTrustCheck{
			originTrust:      lockfile.TrustTierUnverifiedExternal,
			enterpriseSigned: false,
		})
	}

	orgOwnerOptIn := false
	if enabled {
		requiresOrgOwner := false
		for _, chk := range checks {
			if cerr := CheckOrgMinTrustPolicy(minTrust, chk.originTrust, chk.enterpriseSigned); cerr != nil {
				_ = tx.Rollback()
				s.emitAudit(ctx, orgID, actorID, "", "extension.org_trust_policy_denied", "extension_package", packageID, map[string]any{
					"min_plugin_trust":  minTrust,
					"origin_trust_tier": chk.originTrust,
					"enterprise_signed": chk.enterpriseSigned,
				})
				return cerr
			}
			if chk.originTrust == lockfile.TrustTierUnverifiedExternal && !chk.enterpriseSigned {
				requiresOrgOwner = true
			}
		}
		// TP-BR-0202: Enabling unsigned unverified_external packages in an organization requires explicit ORG_OWNER role.
		// Only explicit ORG_OWNER enablement under requiresOrgOwner grants org_owner_opt_in; prior non-owner enablements
		// (e.g., SECURITY_REVIEWER enabling a verified package before key withdrawal) keep org_owner_opt_in = 0.
		if requiresOrgOwner {
			if oerr := ensureOrgOwnerRoleTx(ctx, tx, actorID, orgID); oerr != nil {
				return oerr
			}
			orgOwnerOptIn = true
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO org_extension_policies (org_id, package_id, enabled, org_owner_opt_in, pinned_installation_id, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT (org_id, package_id) DO UPDATE SET
			enabled = excluded.enabled,
			org_owner_opt_in = excluded.org_owner_opt_in,
			pinned_installation_id = excluded.pinned_installation_id,
			version = org_extension_policies.version + 1,
			updated_at_ms = excluded.updated_at_ms`,
		orgID, packageID, boolToInt(enabled), boolToInt(orgOwnerOptIn), nullStrPtr(pinnedParam), now, now,
	); err != nil {
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("%w: org_extension_policy conflict: %v", port.ErrAlreadyExists, err)
		}
		return err
	}

	if _, err := appendExtensionChangelogTx(ctx, tx, "org_extension_policy", orgID+":"+packageID, port.MutationUpsert, false, now); err != nil {
		return err
	}
	if _, aerr := s.emitAuditTx(ctx, tx, orgID, actorID, "", "extension.org_policy_updated", "org_extension_policy", packageID, map[string]any{
		"enabled":          enabled,
		"org_owner_opt_in": orgOwnerOptIn,
	}, now); aerr != nil {
		return aerr
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true

	return nil
}

// UninstallInstallation enforces SM-01 uninstall preconditions (INSTALLED_DISABLED or QUARANTINED),
// commits UNINSTALLED atomically in SQLite BEFORE pruning files inside pluginsDir (CT-04) and removing tanpopo.lock entry.
func (s *LifecycleService) UninstallInstallation(ctx context.Context, installationID string, expectedVersion int64, packageDir, actorID, orgID string) error {
	if strings.TrimSpace(installationID) == "" || strings.TrimSpace(actorID) == "" || expectedVersion < 1 {
		return fmt.Errorf("%w: installation_id, actor_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureGlobalApproverRoleTx(ctx, tx, actorID, orgID); err != nil {
		return err
	}

	rowData, err := loadInstallationForTx(ctx, tx, installationID)
	if err != nil {
		return err
	}
	if rowData.version != expectedVersion {
		return port.ErrVersionConflict
	}
	if !IsValidSM01Transition(rowData.state, StateUninstalled) {
		return fmt.Errorf("%w: cannot uninstall from state %s (must be INSTALLED_DISABLED or QUARANTINED)", ErrInvalidStateTransition, rowData.state)
	}

	// Refuse uninstall if active provider_connections or enabled org policies depend on this installation.
	if err := checkNoDependentConnectionsTx(ctx, tx, installationID); err != nil {
		return err
	}
	// Pre-validate lockfile path before committing DB state so malformed/symlinked lockfiles fail before commit.
	if err := s.preflightLockfile(); err != nil {
		return err
	}
	// Collect version directories to clean up post-commit if caller didn't supply packageDir.
	verIDsToPrune, perr := listPackageVersionIDsTx(ctx, tx, rowData.packageID)
	if perr != nil {
		return perr
	}
	// Validate caller-supplied packageDir confinement AND ownership by this package/installation BEFORE committing DB state (CT-04).
	if strings.TrimSpace(packageDir) != "" {
		if verr := verifyOwnedPackageDir(s.pluginsDir, packageDir, verIDsToPrune, installationID); verr != nil {
			return verr
		}
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE extension_installations
		SET active_version_id = NULL, previous_pinned_version_id = NULL, candidate_version_id = NULL,
		    state = ?, version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ?`,
		StateUninstalled, now, installationID, expectedVersion,
	)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return port.ErrVersionConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE extension_packages SET origin_trust_tier = ? WHERE id = ?`, lockfile.TrustTierUnverifiedExternal, rowData.packageID); err != nil {
		return fmt.Errorf("installer: reset package origin_trust_tier on uninstall: %w", err)
	}
	if _, err := appendExtensionChangelogTx(ctx, tx, "extension_installation", installationID, port.MutationDelete, false, now); err != nil {
		return err
	}
	if _, aerr := s.emitAuditTx(ctx, tx, orgID, actorID, "", "extension.uninstalled", "extension_installation", installationID, map[string]any{
		"canonical_name": rowData.canonicalName,
	}, now); aerr != nil {
		return aerr
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true

	// Mutate filesystem and tanpopo.lock ONLY after SQLite tx.Commit succeeds.
	var syncErrs []string
	if strings.TrimSpace(packageDir) != "" {
		if rerr := SafeRemovePackageDir(s.pluginsDir, packageDir); rerr != nil {
			syncErrs = append(syncErrs, rerr.Error())
		}
	}
	for _, vid := range verIDsToPrune {
		verDir := filepath.Join(s.pluginsDir, vid)
		if rerr := SafeRemovePackageDir(s.pluginsDir, verDir); rerr != nil {
			syncErrs = append(syncErrs, rerr.Error())
		}
	}
	if lerr := s.removeLockfileEntry(rowData.canonicalName, now); lerr != nil {
		syncErrs = append(syncErrs, lerr.Error())
	}
	if len(syncErrs) > 0 {
		msg := strings.Join(syncErrs, "; ")
		s.emitAudit(ctx, orgID, actorID, "", "extension.lockfile_sync_failed", "extension_installation", installationID, map[string]any{
			"canonical_name": rowData.canonicalName,
			"reason":         msg,
		})
		return fmt.Errorf("%w: %s", ErrLockfileSyncFailed, msg)
	}

	return nil
}

// GetInstallation loads the current InstallationRecord scoped by installationID.
func (s *LifecycleService) GetInstallation(ctx context.Context, installationID string) (*InstallationRecord, error) {
	if strings.TrimSpace(installationID) == "" {
		return nil, fmt.Errorf("%w: installation_id required", port.ErrInvalidInput)
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT ei.id, ei.package_id, ep.canonical_name, ep.origin_trust_tier,
		       ei.active_version_id, va.semver,
		       ei.previous_pinned_version_id, ei.candidate_version_id, vc.semver,
		       ei.state, ei.approved_permissions_json, ei.approved_by_principal_id,
		       ei.approved_at_ms, ei.quarantine_reason, ei.version, ei.created_at_ms, ei.updated_at_ms
		FROM extension_installations ei
		JOIN extension_packages ep ON ep.id = ei.package_id
		LEFT JOIN extension_versions va ON va.id = ei.active_version_id
		LEFT JOIN extension_versions vc ON vc.id = ei.candidate_version_id
		WHERE ei.id = ?`, installationID)

	var (
		rec         InstallationRecord
		activeID    sql.NullString
		activeVer   sql.NullString
		prevID      sql.NullString
		candID      sql.NullString
		candVer     sql.NullString
		permsRaw    string
		approvedBy  sql.NullString
		approvedAt  sql.NullInt64
		quarantineR sql.NullString
	)
	if err := row.Scan(
		&rec.InstallationID, &rec.PackageID, &rec.CanonicalName, &rec.OriginTrustTier,
		&activeID, &activeVer, &prevID, &candID, &candVer,
		&rec.State, &permsRaw, &approvedBy, &approvedAt, &quarantineR,
		&rec.Version, &rec.CreatedAtMs, &rec.UpdatedAtMs,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, port.ErrNotFound
		}
		return nil, err
	}
	rec.ActiveVersionID = nullToPtr(activeID)
	rec.ActiveSemver = nullToPtr(activeVer)
	rec.PreviousPinnedVersionID = nullToPtr(prevID)
	rec.CandidateVersionID = nullToPtr(candID)
	rec.CandidateSemver = nullToPtr(candVer)
	rec.ApprovedByPrincipalID = nullToPtr(approvedBy)
	if approvedAt.Valid {
		v := approvedAt.Int64
		rec.ApprovedAtMs = &v
	}
	rec.QuarantineReason = nullToPtr(quarantineR)
	if strings.TrimSpace(permsRaw) != "" {
		if jerr := json.Unmarshal([]byte(permsRaw), &rec.ApprovedPermissions); jerr != nil {
			return nil, fmt.Errorf("installer: corrupt approved_permissions_json for installation %s: %w", installationID, jerr)
		}
	}
	return &rec, nil
}

func (s *LifecycleService) extractRequestArchive(r io.Reader, path string) (*ExtractedArchive, error) {
	if r != nil {
		return ExtractSafeArchive(r, s.pluginsDir)
	}
	if strings.TrimSpace(path) != "" {
		return ExtractSafeArchiveFromFile(path, s.pluginsDir)
	}
	return nil, fmt.Errorf("%w: either ArchiveReader or ArchivePath is required", ErrInvalidArchive)
}

func (s *LifecycleService) checkOrgTrustInDB(ctx context.Context, orgID, originTrustTier string, enterpriseSigned bool) error {
	var minTrust string
	if err := s.db.QueryRowContext(ctx, `SELECT min_plugin_trust FROM organizations WHERE id = ?`, orgID).Scan(&minTrust); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return port.ErrNotFound
		}
		return err
	}
	return CheckOrgMinTrustPolicy(minTrust, originTrustTier, enterpriseSigned)
}

func (s *LifecycleService) checkRevocations(ctx context.Context, pkgSlug, semver, bundleSHA256 string) error {
	var severity, reason string
	err := s.db.QueryRowContext(ctx, `
		SELECT severity, reason
		FROM extension_revocations
		WHERE package_slug = ? AND (version_or_sha256 = ? OR version_or_sha256 = ?)
		  AND severity IN ('CRITICAL', 'CRITICAL_MALICIOUS')
		ORDER BY version_serial DESC LIMIT 1`,
		pkgSlug, semver, bundleSHA256,
	).Scan(&severity, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: severity=%s reason=%s", ErrRevokedExtension, severity, reason)
}

func (s *LifecycleService) syncLockfileEntry(
	canonicalName, originTrustTier string,
	ver *versionRowInfo,
	perms PermissionSet,
	approvedBy string,
	nowMs int64,
	rollback *lockfile.PinnedRollback,
) error {
	if s.lockfilePath == "" {
		return nil
	}
	now := time.UnixMilli(nowMs).UTC()
	lf, err := lockfile.Read(s.lockfilePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		lf = lockfile.NewEmpty(now)
	}
	lf.GeneratedAt = now.Format(time.RFC3339)
	lf.Extensions[canonicalName] = lockfile.LockedEntry{
		Version:                      ver.semver,
		Origin:                       ver.sourceURI,
		ArtifactSHA256:               ver.bundleSHA256,
		ContractVersion:              ver.contractVersion,
		TrustTier:                    originTrustTier,
		EnterpriseSigned:             deriveEnterpriseSignedFromVersion(ver, s.keyring),
		OrgInternalSignatureVerified: deriveEnterpriseSignedFromVersion(ver, s.keyring),
		SigningKeyFingerprint:        ver.fingerprint,
		ExecutionTier:                ver.executionTier,
		ApprovedPermissions: lockfile.ApprovedPermissions{
			ApprovedBy:          approvedBy,
			ApprovedAt:          now.Format(time.RFC3339),
			AllowedHosts:        append([]string(nil), perms.AllowedHosts...),
			AllowPrivateIP:      perms.AllowPrivateIP,
			RequireTLS:          perms.RequireTLS,
			ExecutionTier:       perms.ExecutionTier,
			SecretSlots:         append([]string(nil), perms.SecretSlots...),
			SecretBindingModes:  perms.SecretBindingModes,
			Hooks:               append([]string(nil), perms.Hooks...),
			MaxMemoryMib:        perms.MaxMemoryMib,
			InspectRequestBody:  perms.InspectRequestBody,
			InspectResponseBody: perms.InspectResponseBody,
		},
		PreviousPinnedRollback: rollback,
	}
	return lockfile.WriteAtomic(s.lockfilePath, lf)
}

func (s *LifecycleService) removeLockfileEntry(canonicalName string, nowMs int64) error {
	if s.lockfilePath == "" {
		return nil
	}
	lf, err := lockfile.Read(s.lockfilePath)
	if err != nil {
		if isFileNotExist(err) {
			return nil
		}
		return err
	}
	delete(lf.Extensions, canonicalName)
	lf.GeneratedAt = time.UnixMilli(nowMs).UTC().Format(time.RFC3339)
	return lockfile.WriteAtomic(s.lockfilePath, lf)
}

type installationRowInfo struct {
	id              string
	packageID       string
	canonicalName   string
	originTrustTier string
	activeVerID     *string
	prevPinnedVerID *string
	candidateVerID  *string
	state           string
	approvedPerms   PermissionSet
	approvedBy      *string
	version         int64
}

func loadInstallationForTx(ctx context.Context, tx *sql.Tx, installationID string) (*installationRowInfo, error) {
	var (
		info     installationRowInfo
		actID    sql.NullString
		prevID   sql.NullString
		candID   sql.NullString
		permsRaw string
		appBy    sql.NullString
	)
	err := tx.QueryRowContext(ctx, `
		SELECT ei.id, ei.package_id, ep.canonical_name, ep.origin_trust_tier,
		       ei.active_version_id, ei.previous_pinned_version_id, ei.candidate_version_id,
		       ei.state, ei.approved_permissions_json, ei.approved_by_principal_id, ei.version
		FROM extension_installations ei
		JOIN extension_packages ep ON ep.id = ei.package_id
		WHERE ei.id = ?`, installationID,
	).Scan(
		&info.id, &info.packageID, &info.canonicalName, &info.originTrustTier,
		&actID, &prevID, &candID, &info.state, &permsRaw, &appBy, &info.version,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, port.ErrNotFound
		}
		return nil, err
	}
	info.activeVerID = nullToPtr(actID)
	info.prevPinnedVerID = nullToPtr(prevID)
	info.candidateVerID = nullToPtr(candID)
	info.approvedBy = nullToPtr(appBy)
	if strings.TrimSpace(permsRaw) != "" {
		if jerr := json.Unmarshal([]byte(permsRaw), &info.approvedPerms); jerr != nil {
			return nil, fmt.Errorf("installer: corrupt approved_permissions_json: %w", jerr)
		}
	}
	return &info, nil
}

type versionRowInfo struct {
	id                    string
	semver                string
	contractVersion       string
	executionTier         string
	bundleSHA256          string
	signatureVerified     bool
	enterpriseSigned      bool
	fingerprint           string
	enterpriseFingerprint string
	sourceURI             string
	manifest              ExtensionManifest
}

func loadVersionInfoTx(ctx context.Context, tx *sql.Tx, versionID string) (*versionRowInfo, error) {
	var (
		v      versionRowInfo
		sigInt int
		entInt int
		fp     sql.NullString
		entFP  sql.NullString
		mJSON  string
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, semver, contract_version, execution_tier, bundle_sha256,
		       signature_verified, enterprise_signed, signing_key_fingerprint, COALESCE(enterprise_key_fingerprint, ''),
		       source_uri, manifest_json
		FROM extension_versions WHERE id = ?`, versionID,
	).Scan(&v.id, &v.semver, &v.contractVersion, &v.executionTier, &v.bundleSHA256, &sigInt, &entInt, &fp, &entFP, &v.sourceURI, &mJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, port.ErrNotFound
		}
		return nil, err
	}
	v.signatureVerified = sigInt == 1
	v.enterpriseSigned = entInt == 1
	v.fingerprint = fp.String
	v.enterpriseFingerprint = entFP.String
	if err := json.Unmarshal([]byte(mJSON), &v.manifest); err != nil {
		return nil, fmt.Errorf("installer: corrupt manifest_json for version %s: %w", versionID, err)
	}
	return &v, nil
}

func deriveTrustTierFromVersion(canonicalName string, ver *versionRowInfo, keyring TrustedKeyring) string {
	if ver == nil {
		return lockfile.TrustTierUnverifiedExternal
	}
	return keyring.DerivePublicTrustTier(canonicalName, ver.signatureVerified, ver.fingerprint)
}

func deriveEnterpriseSignedFromVersion(ver *versionRowInfo, keyring TrustedKeyring) bool {
	if ver == nil {
		return false
	}
	return keyring.DeriveEnterpriseSigned(ver.signatureVerified, ver.enterpriseSigned, ver.fingerprint, ver.enterpriseFingerprint)
}

func (s *LifecycleService) preflightLockfile() error {
	if strings.TrimSpace(s.lockfilePath) == "" {
		return nil
	}
	absPath, err := filepath.Abs(s.lockfilePath)
	if err != nil {
		return fmt.Errorf("lockfile: resolve %q: %w", s.lockfilePath, err)
	}
	dir := filepath.Dir(absPath)
	if dfi, derr := os.Lstat(dir); derr == nil && dfi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: parent dir %q cannot be a symlink", lockfile.ErrSymlinkTarget, dir)
	}
	_, rerr := lockfile.Read(absPath)
	if rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return rerr
	}
	return nil
}

func checkRevocationsTx(ctx context.Context, tx *sql.Tx, pkgSlug, semver, bundleSHA256 string) error {
	var severity, reason string
	err := tx.QueryRowContext(ctx, `
		SELECT severity, reason
		FROM extension_revocations
		WHERE package_slug = ? AND (version_or_sha256 = ? OR version_or_sha256 = ?)
		  AND severity IN ('CRITICAL', 'CRITICAL_MALICIOUS')
		ORDER BY version_serial DESC LIMIT 1`,
		pkgSlug, semver, bundleSHA256,
	).Scan(&severity, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: severity=%s reason=%s", ErrRevokedExtension, severity, reason)
}

func checkEnabledOrgPoliciesForPackageTx(ctx context.Context, tx *sql.Tx, packageID, originTrustTier string, enterpriseSigned bool) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT o.min_plugin_trust
		FROM org_extension_policies oep
		JOIN organizations o ON o.id = oep.org_id
		WHERE oep.package_id = ? AND oep.enabled = 1`,
		packageID,
	)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var minTrust string
		if serr := rows.Scan(&minTrust); serr != nil {
			return serr
		}
		if perr := CheckOrgMinTrustPolicy(minTrust, originTrustTier, enterpriseSigned); perr != nil {
			return perr
		}
	}
	return rows.Err()
}

// resetEnabledOrgPoliciesOnUnsignedTransitionTx invalidates prior org_extension_policies enablements
// when transitioning a package into unsigned unverified_external trust so non-ORG_OWNER enablements
// granted under higher trust cannot carry forward without explicit ORG_OWNER opt-in (TP-BR-0202).
func resetEnabledOrgPoliciesOnUnsignedTransitionTx(ctx context.Context, tx *sql.Tx, packageID string, now int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT org_id
		FROM org_extension_policies
		WHERE package_id = ? AND enabled = 1`,
		packageID,
	)
	if err != nil {
		return nil, err
	}
	var orgIDs []string
	for rows.Next() {
		var oid string
		if serr := rows.Scan(&oid); serr != nil {
			_ = rows.Close()
			return nil, serr
		}
		orgIDs = append(orgIDs, oid)
	}
	if rerr := rows.Err(); rerr != nil {
		_ = rows.Close()
		return nil, rerr
	}
	_ = rows.Close()
	if len(orgIDs) == 0 {
		return nil, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE org_extension_policies
		SET enabled = 0, org_owner_opt_in = 0, version = version + 1, updated_at_ms = ?
		WHERE package_id = ? AND (enabled = 1 OR org_owner_opt_in = 1)`,
		now, packageID,
	); err != nil {
		return nil, fmt.Errorf("installer: reset org_extension_policies on trust downgrade: %w", err)
	}
	for _, oid := range orgIDs {
		if _, err := appendExtensionChangelogTx(ctx, tx, "org_extension_policy", oid+":"+packageID, port.MutationUpsert, false, now); err != nil {
			return nil, err
		}
	}
	return orgIDs, nil
}

func listPackageVersionIDsTx(ctx context.Context, tx *sql.Tx, packageID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM extension_versions WHERE package_id = ?`, packageID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if serr := rows.Scan(&id); serr != nil {
			return nil, serr
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func verifyOwnedPackageDir(pluginsDir, pkgDir string, ownedVersionIDs []string, installationID string) error {
	canonRoot, err := filepath.EvalSymlinks(pluginsDir)
	if err != nil {
		return fmt.Errorf("%w: resolve pluginsDir %q: %v", ErrPathNotConfined, pluginsDir, err)
	}
	var resolvedTarget string
	fi, lerr := os.Lstat(pkgDir)
	switch {
	case lerr == nil:
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: refusing to remove symlink target %q", ErrPathNotConfined, pkgDir)
		}
		canonTarget, err := filepath.EvalSymlinks(pkgDir)
		if err != nil {
			return fmt.Errorf("%w: resolve pkgDir %q: %v", ErrPathNotConfined, pkgDir, err)
		}
		if !isStrictlyInside(canonRoot, canonTarget) {
			return fmt.Errorf("%w: target %q is not strictly inside %q", ErrPathNotConfined, canonTarget, canonRoot)
		}
		resolvedTarget = canonTarget
	case os.IsNotExist(lerr):
		absTarget, aerr := filepath.Abs(pkgDir)
		if aerr != nil {
			return fmt.Errorf("%w: resolve pkgDir %q: %v", ErrPathNotConfined, pkgDir, aerr)
		}
		if !isStrictlyInside(canonRoot, absTarget) {
			return fmt.Errorf("%w: target %q is not strictly inside %q", ErrPathNotConfined, absTarget, canonRoot)
		}
		resolvedTarget = filepath.Clean(absTarget)
	default:
		return fmt.Errorf("installer: lstat target %q: %w", pkgDir, lerr)
	}

	for _, vid := range ownedVersionIDs {
		ownedDir := filepath.Clean(filepath.Join(canonRoot, vid))
		if resolvedTarget == ownedDir {
			return nil
		}
	}
	return fmt.Errorf("%w: packageDir %q does not belong to installation %q", ErrPathNotConfined, pkgDir, installationID)
}

func ensureOrgOwnerRoleTx(ctx context.Context, tx *sql.Tx, principalID, orgID string) error {
	if strings.TrimSpace(principalID) == "" || strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("%w: org_id and principal_id are required for ORG_OWNER opt-in", ErrPermissionDenied)
	}
	var count int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM role_bindings rb
		JOIN principals p ON p.id = rb.principal_id
		WHERE rb.principal_id = ?
		  AND p.status = 'ACTIVE'
		  AND rb.scope_type = 'ORG'
		  AND rb.scope_id = ?
		  AND rb.org_id = ?
		  AND rb.role_name = 'ORG_OWNER'`,
		principalID, orgID, orgID,
	).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: active principal %q requires ORG(%s) ORG_OWNER role to enable unsigned unverified_external package (TP-BR-0202)", ErrPermissionDenied, principalID, orgID)
	}
	return nil
}

func ensureGlobalApproverRoleTx(ctx context.Context, tx *sql.Tx, principalID, orgID string) error {
	if strings.TrimSpace(principalID) == "" {
		return fmt.Errorf("%w: approver principal_id is required", ErrPermissionDenied)
	}
	var orgCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM organizations WHERE status NOT IN ('DELETING', 'PENDING_ERASURE')`).Scan(&orgCount); err != nil {
		return err
	}
	// Multi-tenant deployments (> 1 active organization) or empty orgID restrict global extension lifecycle
	// mutations exclusively to active INSTANCE-scoped OPERATOR principals.
	if orgCount > 1 || strings.TrimSpace(orgID) == "" {
		var count int
		err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM role_bindings rb
			JOIN principals p ON p.id = rb.principal_id
			WHERE rb.principal_id = ?
			  AND p.status = 'ACTIVE'
			  AND rb.scope_type = 'INSTANCE'
			  AND rb.role_name = 'OPERATOR'`,
			principalID,
		).Scan(&count)
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("%w: active principal %q requires INSTANCE OPERATOR role for global operation", ErrPermissionDenied, principalID)
		}
		return nil
	}
	return ensureApproverRoleTx(ctx, tx, principalID, orgID)
}

func ensureApproverRoleTx(ctx context.Context, tx *sql.Tx, principalID, orgID string) error {
	if strings.TrimSpace(principalID) == "" {
		return fmt.Errorf("%w: approver principal_id is required", ErrPermissionDenied)
	}
	var count int
	if strings.TrimSpace(orgID) != "" {
		err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM role_bindings rb
			JOIN principals p ON p.id = rb.principal_id
			WHERE rb.principal_id = ?
			  AND p.status = 'ACTIVE'
			  AND ((rb.scope_type = 'INSTANCE' AND rb.role_name = 'OPERATOR')
			       OR (rb.scope_type = 'ORG' AND rb.org_id = ? AND rb.role_name IN ('ORG_OWNER', 'SECURITY_REVIEWER')))`,
			principalID, orgID,
		).Scan(&count)
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("%w: active principal %q requires INSTANCE OPERATOR or ORG(%s) ORG_OWNER/SECURITY_REVIEWER role", ErrPermissionDenied, principalID, orgID)
		}
		return nil
	}

	// Instance-global action (orgID == ""): require active INSTANCE-scoped OPERATOR role (forbid org/project privilege escalation).
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM role_bindings rb
		JOIN principals p ON p.id = rb.principal_id
		WHERE rb.principal_id = ?
		  AND p.status = 'ACTIVE'
		  AND rb.scope_type = 'INSTANCE'
		  AND rb.role_name = 'OPERATOR'`,
		principalID,
	).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: active principal %q requires INSTANCE OPERATOR role for global operation", ErrPermissionDenied, principalID)
	}
	return nil
}

func checkNoDependentConnectionsTx(ctx context.Context, tx *sql.Tx, installationID string) error {
	var pinnedEnabled int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM org_extension_policies
		WHERE pinned_installation_id = ? AND enabled = 1`,
		installationID,
	).Scan(&pinnedEnabled); err != nil {
		return fmt.Errorf("installer: query org_extension_policies dependencies: %w", err)
	}
	if pinnedEnabled > 0 {
		return fmt.Errorf("%w: %d enabled org_extension_policies pin installation %q", port.ErrResourceInUse, pinnedEnabled, installationID)
	}

	var tblCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name IN ('provider_connections', 'provider_adapters')`,
	).Scan(&tblCount); err != nil {
		return fmt.Errorf("installer: inspect connection tables: %w", err)
	}
	if tblCount < 2 {
		return nil
	}
	var activeDeps int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM provider_connections pc
		JOIN provider_adapters pa ON pa.id = pc.adapter_id
		WHERE pa.installation_id = ? AND pc.state NOT IN ('REVOKED', 'ARCHIVED')`,
		installationID,
	).Scan(&activeDeps)
	if err != nil {
		return fmt.Errorf("installer: query provider_connections dependencies: %w", err)
	}
	if activeDeps > 0 {
		return fmt.Errorf("%w: %d active provider_connections depend on installation %q", port.ErrResourceInUse, activeDeps, installationID)
	}
	return nil
}

func appendExtensionChangelogTx(ctx context.Context, tx *sql.Tx, entityKind, entityID, mutationType string, emergency bool, now int64) (int64, error) {
	emInt := 0
	if emergency {
		emInt = 1
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (NULL, ?, ?, ?, ?, ?)`,
		entityKind, entityID, mutationType, emInt, now,
	)
	if err != nil {
		return 0, fmt.Errorf("installer: append config_changelog: %w", err)
	}
	return res.LastInsertId()
}

func (s *LifecycleService) emitAuditTx(ctx context.Context, tx *sql.Tx, orgID, actorID, reqID, eventType, targetType, targetID string, meta map[string]any, nowMs int64) (bool, error) {
	if s.audit == nil {
		return false, fmt.Errorf("%w: non-nil port.TxAuditSink is required", ErrInvalidLifecycleConfig)
	}
	txSink, ok := s.audit.(port.TxAuditSink)
	if !ok {
		return false, fmt.Errorf("%w: Audit must implement port.TxAuditSink", ErrInvalidLifecycleConfig)
	}
	id, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return true, err
	}
	var orgPtr *string
	if strings.TrimSpace(orgID) != "" {
		o := strings.TrimSpace(orgID)
		orgPtr = &o
	}
	var reqPtr *string
	if strings.TrimSpace(reqID) != "" {
		r := strings.TrimSpace(reqID)
		reqPtr = &r
	}
	actor := strings.TrimSpace(actorID)
	if actor == "" {
		actor = "system"
	}
	if meta == nil {
		meta = map[string]any{}
	}
	rawMeta, merr := json.Marshal(meta)
	if merr != nil {
		return true, merr
	}
	return true, txSink.AppendTx(ctx, tx, port.AuditEvent{
		ID:           id,
		CreatedAtMs:  nowMs,
		OrgID:        orgPtr,
		ActorKind:    "PRINCIPAL",
		ActorID:      actor,
		EntryPoint:   s.auditEntryPoint,
		EventType:    eventType,
		TargetType:   targetType,
		TargetID:     targetID,
		RequestID:    reqPtr,
		MetadataJSON: string(rawMeta),
	})
}

func (s *LifecycleService) emitAudit(ctx context.Context, orgID, actorID, reqID, eventType, targetType, targetID string, meta map[string]any) {
	id, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return
	}
	var orgPtr *string
	if strings.TrimSpace(orgID) != "" {
		o := strings.TrimSpace(orgID)
		orgPtr = &o
	}
	var reqPtr *string
	if strings.TrimSpace(reqID) != "" {
		r := strings.TrimSpace(reqID)
		reqPtr = &r
	}
	actor := strings.TrimSpace(actorID)
	if actor == "" {
		actor = "system"
	}
	if meta == nil {
		meta = map[string]any{}
	}
	rawMeta, merr := json.Marshal(meta)
	if merr != nil {
		rawMeta = []byte(`{}`)
	}
	auditCtx := context.WithoutCancel(ctx)
	_ = s.audit.Append(auditCtx, port.AuditEvent{
		ID:           id,
		CreatedAtMs:  s.nowMs(),
		OrgID:        orgPtr,
		ActorKind:    "PRINCIPAL",
		ActorID:      actor,
		EntryPoint:   s.auditEntryPoint,
		EventType:    eventType,
		TargetType:   targetType,
		TargetID:     targetID,
		RequestID:    reqPtr,
		MetadataJSON: string(rawMeta),
	})
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") || strings.Contains(msg, "constraint failed: unique")
}

func isFileNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

func sortStrings(s []string) { sort.Strings(s) }
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
func nullStr(s string) sql.NullString {
	if strings.TrimSpace(s) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
func nullStrPtr(p *string) sql.NullString {
	if p == nil || strings.TrimSpace(*p) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}
func nullToPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
