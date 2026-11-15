// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package installer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/nguywnben/tanpopo/internal/security/trust"
	"gopkg.in/yaml.v3"
)

// Supported extension manifest constants (SPEC-EXT-01 / SPEC-EXT-02 / CR-02, CR-03, CR-05).
const (
	CanonicalAPIVersion      = "tanpopo.dev/extension/v1alpha1"
	KindExtensionManifest    = "ExtensionManifest"
	KindDeclarativeAdapter   = "DeclarativeProviderAdapter"
	ActiveContractVersion    = "0.1"
	ActiveCoreSemver         = "0.1.0"
	DefaultMaxYAMLDepth      = 32
	DefaultMaxYAMLNodes      = 10000
	DefaultMaxYAMLAliases        = 64
	MaxAllowedMemoryMibLimit     = 64
	MaxAllowedCpuMsPerCall       = 5000
	MaxAllowedStreamChunkCpuMs   = 1000
)

// Canonical SQL/system execution tiers matching extension_versions.execution_tier CHECK constraints.
const (
	ExecutionTierDeclarativeT1     = "DECLARATIVE_T1"
	ExecutionTierWasmSandboxT2     = "WASM_SANDBOX_T2"
	ExecutionTierIsolatedSidecarT3 = "ISOLATED_SIDECAR_T3"
)

// Canonical SQL/system extension kinds matching extension_packages.extension_kind CHECK constraints.
const (
	ExtensionKindProviderAdapter   = "PROVIDER_ADAPTER"
	ExtensionKindAuthDriver        = "AUTH_DRIVER"
	ExtensionKindGuardrailPolicy   = "GUARDRAIL_POLICY"
	ExtensionKindTelemetryExporter = "TELEMETRY_EXPORTER"
)

var (
	// ReservedNamespaceRe locks the ^@tanpopo(-[a-z0-9]+)?/ namespace exclusively for Tanpopo root-signed packages (CR-03 / CT-05).
	ReservedNamespaceRe = trust.ReservedNamespaceRe
	packageIDRe         = regexp.MustCompile(`^@[a-z0-9][a-z0-9-]{0,62}/[a-z0-9][a-z0-9._-]{0,62}$`)
	semverRe            = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
	sha256LowerRe       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Canonical manifest validation error sentinels.
var (
	ErrManifestInvalid            = errors.New("installer: invalid extension manifest")
	ErrUnsupportedContractVersion = errors.New("installer: unsupported contractVersion")
	ErrIncompatibleCoreVersion    = errors.New("installer: incompatible coreVersionRange")
	ErrReservedNamespaceSpoof     = errors.New("installer: reserved_namespace_spoofing")
	ErrProhibitedCookiePermission = errors.New("installer: prohibited_cookie_injection")
	ErrWildcardEgressForbidden    = errors.New("installer: wildcard_egress_forbidden")
	ErrProhibitedCapabilityTier   = errors.New("installer: scoped_secret_unseal is prohibited in Tier-1/Tier-2")
	ErrEntrypointMismatch         = errors.New("installer: runtime entrypoint missing or sha256 mismatch")
)

// ValidationPolicy configures contract version support and Core SemVer compatibility window (CT-02 / CT-24).
type ValidationPolicy struct {
	CoreVersion               string
	SupportedContractVersions []string
}

// DefaultValidationPolicy returns the default v0.1 validation policy (contractVersion=0.1, core=0.1.0).
func DefaultValidationPolicy() ValidationPolicy {
	return ValidationPolicy{
		CoreVersion:               ActiveCoreSemver,
		SupportedContractVersions: []string{ActiveContractVersion},
	}
}

// ExtensionManifest models the canonical manifest.yaml structure across ExtensionManifest
// and DeclarativeProviderAdapter packages (SPEC-EXT-01 §2.2 & SPEC-EXT-02 §3.1).
type ExtensionManifest struct {
	APIVersion    string             `yaml:"apiVersion" json:"apiVersion"`
	Kind          string             `yaml:"kind" json:"kind"`
	Metadata      ManifestMetadata   `yaml:"metadata" json:"metadata"`
	Compatibility ManifestCompat     `yaml:"compatibility" json:"compatibility"`
	Runtime       *ManifestRuntime   `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Capabilities  []ManifestCap      `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	ConfigSchema  map[string]any     `yaml:"configSchema,omitempty" json:"configSchema,omitempty"`
	SecretSlots   []ManifestSlotSpec `yaml:"secretSlots,omitempty" json:"secretSlots,omitempty"`
	Permissions   ManifestPerms      `yaml:"permissions" json:"permissions"`
	Adapter       map[string]any     `yaml:"adapter,omitempty" json:"adapter,omitempty"`
}

// ManifestMetadata holds extension identity and provenance-neutral display metadata.
type ManifestMetadata struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	License     string `yaml:"license" json:"license"`
	Homepage    string `yaml:"homepage,omitempty" json:"homepage,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	TrustTier   string `yaml:"originTrustTier,omitempty" json:"originTrustTier,omitempty"`
}

// ManifestCompat defines extension contract and Core SemVer version bounds.
type ManifestCompat struct {
	ContractVersion  string `yaml:"contractVersion" json:"contractVersion"`
	CoreVersionRange string `yaml:"coreVersionRange" json:"coreVersionRange"`
}

// ManifestRuntime specifies execution tier, entrypoint path/SHA-256, and quotas.
type ManifestRuntime struct {
	Tier       string         `yaml:"tier" json:"tier"`
	Entrypoint string         `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	SHA256     string         `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	Quotas     *RuntimeQuotas `yaml:"quotas,omitempty" json:"quotas,omitempty"`
}

// RuntimeQuotas caps sandbox memory and hook execution deadlines.
type RuntimeQuotas struct {
	MaxMemoryMib        int `yaml:"maxMemoryMib,omitempty" json:"maxMemoryMib,omitempty"`
	MaxCpuMsPerCall     int `yaml:"maxCpuMsPerCall,omitempty" json:"maxCpuMsPerCall,omitempty"`
	MaxStreamChunkCpuMs int `yaml:"maxStreamChunkCpuMs,omitempty" json:"maxStreamChunkCpuMs,omitempty"`
}

// ManifestCap declares an extension capability such as provider_adapter.
type ManifestCap struct {
	Type                    string               `yaml:"type" json:"type"`
	AdapterSlug             string               `yaml:"adapterSlug,omitempty" json:"adapterSlug,omitempty"`
	SupportedDialects       []string             `yaml:"supportedDialects,omitempty" json:"supportedDialects,omitempty"`
	NativePassthroughPrefix string               `yaml:"nativePassthroughPrefix,omitempty" json:"nativePassthroughPrefix,omitempty"`
	AuthMethods             []ManifestAuthMethod `yaml:"authMethods,omitempty" json:"authMethods,omitempty"`
}

// ManifestAuthMethod declares a supported upstream auth mode without raw secrets.
type ManifestAuthMethod struct {
	ID         string `yaml:"id" json:"id"`
	Mode       string `yaml:"mode" json:"mode"`
	HeaderName string `yaml:"headerName,omitempty" json:"headerName,omitempty"`
}

// ManifestSlotSpec declares a named vault secret slot reference.
type ManifestSlotSpec struct {
	Slot                   string   `yaml:"slot" json:"slot"`
	RequiredForAuthMethods []string `yaml:"requiredForAuthMethods,omitempty" json:"requiredForAuthMethods,omitempty"`
}

// ManifestPerms defines default-deny egress, hook, and payload inspection requests.
type ManifestPerms struct {
	NetworkEgress ManifestNetworkEgress  `yaml:"networkEgress" json:"networkEgress"`
	Hooks         []string               `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	PayloadAccess *ManifestPayloadAccess `yaml:"payloadAccess,omitempty" json:"payloadAccess,omitempty"`
}

// ManifestNetworkEgress lists requested allowed destination hosts and network policies.
type ManifestNetworkEgress struct {
	AllowedHosts   []string `yaml:"allowedHosts,omitempty" json:"allowedHosts,omitempty"`
	AllowPrivateIP bool     `yaml:"allowPrivateIp,omitempty" json:"allowPrivateIp,omitempty"`
	RequireTLS     bool     `yaml:"requireTls,omitempty" json:"requireTls,omitempty"`
}

// ManifestPayloadAccess declares request/response inspection bounds.
type ManifestPayloadAccess struct {
	InspectRequestBody     bool `yaml:"inspectRequestBody,omitempty" json:"inspectRequestBody,omitempty"`
	InspectResponseBody    bool `yaml:"inspectResponseBody,omitempty" json:"inspectResponseBody,omitempty"`
	MaxInlineMetadataBytes int  `yaml:"maxInlineMetadataBytes,omitempty" json:"maxInlineMetadataBytes,omitempty"`
}

// ValidatedManifest carries the parsed manifest alongside normalized SQL metadata.
type ValidatedManifest struct {
	Manifest              ExtensionManifest
	ExecutionTier         string // DECLARATIVE_T1 | WASM_SANDBOX_T2 | ISOLATED_SIDECAR_T3
	ExtensionKind         string // PROVIDER_ADAPTER | AUTH_DRIVER | GUARDRAIL_POLICY | TELEMETRY_EXPORTER
	PublisherID           string
	RequiresRootSignature bool
	Permissions           PermissionSet
}

// IsReservedTanpopoNamespace reports whether pkgID matches ^@tanpopo(-[a-z0-9]+)?/ (CT-05).
func IsReservedTanpopoNamespace(pkgID string) bool {
	return trust.IsReservedTanpopoNamespace(pkgID)
}

// ParseAndValidateManifest decodes YAML strictly (guarding depth, node count, alias bombs, and
// unknown fields) and validates schema, contractVersion (CT-02), coreVersionRange (CT-24),
// reserved namespaces, wildcard egress (CT-01), Cookie bans, and execution-tier constraints.
// archiveFiles may be nil when validating standalone YAML; when non-nil, entrypoint presence
// and SHA-256 parity are verified against the extracted archive contents.
func ParseAndValidateManifest(rawYAML []byte, policy ValidationPolicy, archiveFiles map[string]string) (*ValidatedManifest, error) {
	if len(bytes.TrimSpace(rawYAML)) == 0 {
		return nil, fmt.Errorf("%w: empty manifest", ErrManifestInvalid)
	}
	if len(rawYAML) > MaxManifestBytes {
		return nil, fmt.Errorf("%w: manifest exceeds %d byte cap", ErrManifestInvalid, MaxManifestBytes)
	}
	if len(policy.SupportedContractVersions) == 0 {
		policy = DefaultValidationPolicy()
	}
	if strings.TrimSpace(policy.CoreVersion) == "" {
		policy.CoreVersion = ActiveCoreSemver
	}

	// Step 1: AST depth/node/alias bomb check prior to decoding.
	var doc yaml.Node
	astDec := yaml.NewDecoder(bytes.NewReader(rawYAML))
	if err := astDec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: malformed YAML: %v", ErrManifestInvalid, err)
	}
	var extra yaml.Node
	if err := astDec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%w: multi-document YAML manifest forbidden", ErrManifestInvalid)
	}
	if err := inspectYAMLNodeBudget(&doc); err != nil {
		return nil, err
	}

	// Step 2: Decode with KnownFields(true) so unknown permission or top-level keys fail closed (CT-01).
	var m ExtensionManifest
	strictDec := yaml.NewDecoder(bytes.NewReader(rawYAML))
	strictDec.KnownFields(true)
	if err := strictDec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%w: schema violation: %v", ErrManifestInvalid, err)
	}

	return ValidateManifestStruct(m, policy, archiveFiles)
}

type yamlSubtreeBudget struct {
	nodes    int
	maxDepth int
}

func inspectYAMLNodeBudget(root *yaml.Node) error {
	aliases := 0
	visiting := make(map[*yaml.Node]bool)
	memo := make(map[*yaml.Node]yamlSubtreeBudget)

	var measure func(n *yaml.Node) (yamlSubtreeBudget, error)
	measure = func(n *yaml.Node) (yamlSubtreeBudget, error) {
		if n == nil {
			return yamlSubtreeBudget{}, nil
		}
		if n.Kind == yaml.AliasNode {
			aliases++
			if aliases > DefaultMaxYAMLAliases {
				return yamlSubtreeBudget{}, fmt.Errorf("%w: YAML alias count exceeds %d cap", ErrManifestInvalid, DefaultMaxYAMLAliases)
			}
			if n.Alias == nil {
				return yamlSubtreeBudget{nodes: 1, maxDepth: 0}, nil
			}
			if visiting[n.Alias] {
				return yamlSubtreeBudget{}, fmt.Errorf("%w: cyclic YAML alias reference forbidden", ErrManifestInvalid)
			}
			return measure(n.Alias)
		}
		if cached, ok := memo[n]; ok {
			return cached, nil
		}
		if visiting[n] {
			return yamlSubtreeBudget{}, fmt.Errorf("%w: cyclic YAML structure forbidden", ErrManifestInvalid)
		}
		visiting[n] = true
		defer delete(visiting, n)

		totalNodes := 1
		deepest := 0
		for _, child := range n.Content {
			cb, err := measure(child)
			if err != nil {
				return yamlSubtreeBudget{}, err
			}
			totalNodes += cb.nodes
			if totalNodes > DefaultMaxYAMLNodes {
				return yamlSubtreeBudget{}, fmt.Errorf("%w: expanded YAML node count exceeds %d cap", ErrManifestInvalid, DefaultMaxYAMLNodes)
			}
			if cb.maxDepth+1 > deepest {
				deepest = cb.maxDepth + 1
			}
			if deepest > DefaultMaxYAMLDepth {
				return yamlSubtreeBudget{}, fmt.Errorf("%w: expanded YAML nesting depth exceeds %d cap", ErrManifestInvalid, DefaultMaxYAMLDepth)
			}
		}
		res := yamlSubtreeBudget{nodes: totalNodes, maxDepth: deepest}
		memo[n] = res
		return res, nil
	}

	b, err := measure(root)
	if err != nil {
		return err
	}
	if b.maxDepth > DefaultMaxYAMLDepth {
		return fmt.Errorf("%w: YAML nesting depth %d exceeds %d cap", ErrManifestInvalid, b.maxDepth, DefaultMaxYAMLDepth)
	}
	if b.nodes > DefaultMaxYAMLNodes {
		return fmt.Errorf("%w: YAML node count %d exceeds %d cap", ErrManifestInvalid, b.nodes, DefaultMaxYAMLNodes)
	}
	return nil
}

// ValidateManifestStruct validates an already-decoded ExtensionManifest against policy and optional archiveFiles.
func ValidateManifestStruct(m ExtensionManifest, policy ValidationPolicy, archiveFiles map[string]string) (*ValidatedManifest, error) {
	if len(policy.SupportedContractVersions) == 0 {
		policy = DefaultValidationPolicy()
	}
	if strings.TrimSpace(policy.CoreVersion) == "" {
		policy.CoreVersion = ActiveCoreSemver
	}

	if m.APIVersion != CanonicalAPIVersion {
		return nil, fmt.Errorf("%w: unsupported apiVersion %q (expected %q)", ErrManifestInvalid, m.APIVersion, CanonicalAPIVersion)
	}
	if m.Kind != KindExtensionManifest && m.Kind != KindDeclarativeAdapter {
		return nil, fmt.Errorf("%w: unsupported kind %q", ErrManifestInvalid, m.Kind)
	}

	id := strings.TrimSpace(m.Metadata.ID)
	if !packageIDRe.MatchString(id) || len(id) > 128 {
		return nil, fmt.Errorf("%w: metadata.id %q must match @scope/name lowercase identifier", ErrManifestInvalid, m.Metadata.ID)
	}
	if !semverRe.MatchString(strings.TrimSpace(m.Metadata.Version)) {
		return nil, fmt.Errorf("%w: metadata.version %q is not valid SemVer", ErrManifestInvalid, m.Metadata.Version)
	}
	if m.Kind == KindExtensionManifest && strings.TrimSpace(m.Metadata.Name) == "" {
		return nil, fmt.Errorf("%w: metadata.name is required", ErrManifestInvalid)
	}
	if strings.TrimSpace(m.Metadata.License) == "" {
		return nil, fmt.Errorf("%w: metadata.license is required", ErrManifestInvalid)
	}

	// CT-02: Enforce supported contractVersion compatibility window and report supported versions clearly.
	cv := strings.TrimSpace(m.Compatibility.ContractVersion)
	if cv == "" {
		return nil, fmt.Errorf("%w: compatibility.contractVersion is required (supported contract versions: %v)", ErrManifestInvalid, policy.SupportedContractVersions)
	}
	cvOK := false
	for _, sup := range policy.SupportedContractVersions {
		if cv == sup {
			cvOK = true
			break
		}
	}
	if !cvOK {
		return nil, fmt.Errorf("%w: %q is outside supported contract versions %v", ErrUnsupportedContractVersion, cv, policy.SupportedContractVersions)
	}

	// CT-24: Enforce coreVersionRange against activeCoreSemver.
	coreRange := strings.TrimSpace(m.Compatibility.CoreVersionRange)
	if coreRange == "" {
		return nil, fmt.Errorf("%w: compatibility.coreVersionRange is required", ErrManifestInvalid)
	}
	matchRange, rerr := SemverMatchesRange(policy.CoreVersion, coreRange)
	if rerr != nil {
		return nil, fmt.Errorf("%w: invalid coreVersionRange %q: %v", ErrManifestInvalid, coreRange, rerr)
	}
	if !matchRange {
		return nil, fmt.Errorf("%w: core version %q does not satisfy required range %q", ErrIncompatibleCoreVersion, policy.CoreVersion, coreRange)
	}

	// Execution tier resolution & entrypoint/quota validation.
	var execTier string
	var maxMem, maxCpuMs, maxChunkCpuMs int
	tierStr := "declarative"
	if m.Runtime != nil && strings.TrimSpace(m.Runtime.Tier) != "" {
		tierStr = strings.TrimSpace(m.Runtime.Tier)
	} else if m.Kind == KindExtensionManifest && m.Runtime == nil {
		return nil, fmt.Errorf("%w: runtime block is required for ExtensionManifest", ErrManifestInvalid)
	}

	switch tierStr {
	case "declarative":
		execTier = ExecutionTierDeclarativeT1
	case "sandboxed_wasm":
		execTier = ExecutionTierWasmSandboxT2
	case "isolated_sidecar":
		execTier = ExecutionTierIsolatedSidecarT3
	default:
		return nil, fmt.Errorf("%w: unsupported runtime.tier %q", ErrManifestInvalid, tierStr)
	}

	if m.Runtime != nil {
		if m.Runtime.Quotas != nil {
			if m.Runtime.Quotas.MaxMemoryMib < 0 || m.Runtime.Quotas.MaxMemoryMib > MaxAllowedMemoryMibLimit {
				return nil, fmt.Errorf("%w: runtime.quotas.maxMemoryMib %d exceeds %d MiB cap", ErrManifestInvalid, m.Runtime.Quotas.MaxMemoryMib, MaxAllowedMemoryMibLimit)
			}
			if m.Runtime.Quotas.MaxCpuMsPerCall < 0 || m.Runtime.Quotas.MaxStreamChunkCpuMs < 0 {
				return nil, fmt.Errorf("%w: runtime quotas cannot be negative", ErrManifestInvalid)
			}
			if m.Runtime.Quotas.MaxCpuMsPerCall > MaxAllowedCpuMsPerCall {
				return nil, fmt.Errorf("%w: runtime.quotas.maxCpuMsPerCall %d exceeds %d ms cap", ErrManifestInvalid, m.Runtime.Quotas.MaxCpuMsPerCall, MaxAllowedCpuMsPerCall)
			}
			if m.Runtime.Quotas.MaxStreamChunkCpuMs > MaxAllowedStreamChunkCpuMs {
				return nil, fmt.Errorf("%w: runtime.quotas.maxStreamChunkCpuMs %d exceeds %d ms cap", ErrManifestInvalid, m.Runtime.Quotas.MaxStreamChunkCpuMs, MaxAllowedStreamChunkCpuMs)
			}
			maxMem = m.Runtime.Quotas.MaxMemoryMib
			maxCpuMs = m.Runtime.Quotas.MaxCpuMsPerCall
			maxChunkCpuMs = m.Runtime.Quotas.MaxStreamChunkCpuMs
		}
		ep := strings.TrimSpace(m.Runtime.Entrypoint)
		if execTier != ExecutionTierDeclarativeT1 && ep == "" {
			return nil, fmt.Errorf("%w: runtime.entrypoint is required for tier %q", ErrManifestInvalid, tierStr)
		}
		declSHA := strings.ToLower(strings.TrimSpace(m.Runtime.SHA256))
		if declSHA != "" && !sha256LowerRe.MatchString(declSHA) {
			return nil, fmt.Errorf("%w: runtime.sha256 %q must be 64 lowercase hex characters", ErrEntrypointMismatch, m.Runtime.SHA256)
		}
		if ep != "" {
			cleanEP, _, perr := sanitizeArchiveEntryPath(ep)
			if perr != nil || cleanEP == "" {
				return nil, fmt.Errorf("%w: invalid runtime.entrypoint %q", ErrManifestInvalid, ep)
			}
			if archiveFiles != nil {
				actualEPSha, ok := archiveFiles[cleanEP]
				if !ok {
					return nil, fmt.Errorf("%w: runtime.entrypoint %q absent from .tpkg archive", ErrEntrypointMismatch, cleanEP)
				}
				if execTier != ExecutionTierDeclarativeT1 && declSHA == "" {
					return nil, fmt.Errorf("%w: runtime.sha256 is required for non-declarative entrypoint %q", ErrEntrypointMismatch, cleanEP)
				}
				if declSHA != "" && declSHA != actualEPSha {
					return nil, fmt.Errorf("%w: runtime.entrypoint %q sha256 mismatch (declared=%s actual=%s)", ErrEntrypointMismatch, cleanEP, declSHA, actualEPSha)
				}
			}
		}
	}

	// CT-01: Reject wildcard network egress, invalid host entries, Cookie/Set-Cookie headers, and Tier-1/2 scoped_secret_unseal.
	for _, h := range m.Permissions.NetworkEgress.AllowedHosts {
		if err := validateManifestAllowedHost(h); err != nil {
			return nil, err
		}
	}
	for _, hook := range m.Permissions.Hooks {
		ht := strings.TrimSpace(hook)
		if ht == "" || strings.ContainsAny(ht, " \t\r\n\x00") {
			return nil, fmt.Errorf("%w: invalid hook name %q", ErrManifestInvalid, hook)
		}
		if strings.EqualFold(ht, "scoped_secret_unseal") && execTier != ExecutionTierIsolatedSidecarT3 {
			return nil, fmt.Errorf("%w: hook %q forbidden in %s", ErrProhibitedCapabilityTier, hook, execTier)
		}
	}
	if m.Permissions.PayloadAccess != nil {
		if m.Permissions.PayloadAccess.MaxInlineMetadataBytes < 0 || m.Permissions.PayloadAccess.MaxInlineMetadataBytes > 262144 {
			return nil, fmt.Errorf("%w: permissions.payloadAccess.maxInlineMetadataBytes %d must be in [0, 262144]", ErrManifestInvalid, m.Permissions.PayloadAccess.MaxInlineMetadataBytes)
		}
	}

	slotMap := make(map[string]bool)
	bindingModes := make(map[string]string)
	for _, s := range m.SecretSlots {
		sn := strings.TrimSpace(s.Slot)
		if sn == "" || strings.ContainsAny(sn, " \t\r\n\x00:") {
			return nil, fmt.Errorf("%w: invalid secretSlot %q", ErrManifestInvalid, s.Slot)
		}
		slotMap[sn] = true
	}

	extKind := ExtensionKindProviderAdapter
	for i, capEntry := range m.Capabilities {
		var mappedKind string
		switch strings.TrimSpace(capEntry.Type) {
		case "provider_adapter":
			mappedKind = ExtensionKindProviderAdapter
		case "auth_driver":
			mappedKind = ExtensionKindAuthDriver
		case "guardrail_policy":
			mappedKind = ExtensionKindGuardrailPolicy
		case "telemetry_exporter":
			mappedKind = ExtensionKindTelemetryExporter
		default:
			return nil, fmt.Errorf("%w: unsupported capability type %q at index %d", ErrManifestInvalid, capEntry.Type, i)
		}
		if i == 0 {
			extKind = mappedKind
		}
		for _, auth := range capEntry.AuthMethods {
			if strings.EqualFold(strings.TrimSpace(auth.HeaderName), "cookie") || strings.EqualFold(strings.TrimSpace(auth.HeaderName), "set-cookie") {
				return nil, fmt.Errorf("%w: headerName %q forbidden by DEC-004", ErrProhibitedCookiePermission, auth.HeaderName)
			}
			if strings.EqualFold(strings.TrimSpace(auth.Mode), "scoped_secret_unseal") && execTier != ExecutionTierIsolatedSidecarT3 {
				return nil, fmt.Errorf("%w: authMethod %q mode scoped_secret_unseal forbidden in %s", ErrProhibitedCapabilityTier, auth.ID, execTier)
			}
		}
	}

	// Validate DeclarativeProviderAdapter inline adapter map & adapter.auth entries safely.
	if m.Adapter != nil {
		if err := validateAdapterBlockSafe(m.Adapter, execTier); err != nil {
			return nil, err
		}
		if rawAuthVal, hasAuth := m.Adapter["auth"]; hasAuth {
			rawAuth, ok := rawAuthVal.([]any)
			if !ok {
				return nil, fmt.Errorf("%w: adapter.auth must be a list", ErrManifestInvalid)
			}
			for _, item := range rawAuth {
				am, ok := item.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%w: adapter.auth entry must be an object", ErrManifestInvalid)
				}
				hdr, _ := am["headerName"].(string)
				if strings.EqualFold(strings.TrimSpace(hdr), "cookie") || strings.EqualFold(strings.TrimSpace(hdr), "set-cookie") {
					return nil, fmt.Errorf("%w: adapter.auth headerName %q forbidden by DEC-004", ErrProhibitedCookiePermission, hdr)
				}
				mode := strings.TrimSpace(fmt.Sprint(am["mode"]))
				if am["mode"] == nil {
					mode = ""
				}
				if strings.EqualFold(mode, "scoped_secret_unseal") && execTier != ExecutionTierIsolatedSidecarT3 {
					return nil, fmt.Errorf("%w: adapter.auth scoped_secret_unseal forbidden in %s", ErrProhibitedCapabilityTier, execTier)
				}
				rawSlot, hasSlot := am["secretSlot"]
				if !hasSlot {
					rawSlot, hasSlot = am["slot"]
				}
				if hasSlot {
					slotName, ok := rawSlot.(string)
					sn := strings.TrimSpace(slotName)
					if !ok || sn == "" || strings.ContainsAny(sn, " \t\r\n\x00:") {
						return nil, fmt.Errorf("%w: invalid adapter.auth secretSlot %v", ErrManifestInvalid, rawSlot)
					}
					slotMap[sn] = true
					if mode != "" {
						bindingModes[sn] = mode
					}
				}
			}
		}
	}

	// Map slot binding modes from capabilities + all requiredForAuthMethods entries when declared.
	authModeByID := make(map[string]string)
	for _, capEntry := range m.Capabilities {
		for _, auth := range capEntry.AuthMethods {
			authModeByID[auth.ID] = strings.TrimSpace(auth.Mode)
		}
	}
	for _, s := range m.SecretSlots {
		sn := strings.TrimSpace(s.Slot)
		for _, authID := range s.RequiredForAuthMethods {
			mode, ok := authModeByID[strings.TrimSpace(authID)]
			if !ok || mode == "" {
				continue
			}
			if prev, exists := bindingModes[sn]; exists && prev != "" && prev != mode {
				bindingModes[sn] = prev + "," + mode
			} else {
				bindingModes[sn] = mode
			}
		}
	}

	slots := make([]string, 0, len(slotMap))
	for k := range slotMap {
		slots = append(slots, k)
	}
	sortStrings(slots)

	inspectReq, inspectResp := false, false
	if m.Permissions.PayloadAccess != nil {
		inspectReq = m.Permissions.PayloadAccess.InspectRequestBody
		inspectResp = m.Permissions.PayloadAccess.InspectResponseBody
	}

	scope, _, _ := strings.Cut(id, "/")
	publisherID := strings.TrimPrefix(scope, "@")

	permSet := NormalizePermissionSet(PermissionSet{
		AllowedHosts:        m.Permissions.NetworkEgress.AllowedHosts,
		AllowPrivateIP:      m.Permissions.NetworkEgress.AllowPrivateIP,
		RequireTLS:          m.Permissions.NetworkEgress.RequireTLS,
		ExecutionTier:       execTier,
		SecretSlots:         slots,
		SecretBindingModes:  bindingModes,
		Hooks:               m.Permissions.Hooks,
		MaxMemoryMib:        maxMem,
		MaxCpuMsPerCall:     maxCpuMs,
		MaxStreamChunkCpuMs: maxChunkCpuMs,
		InspectRequestBody:  inspectReq,
		InspectResponseBody: inspectResp,
	})

	return &ValidatedManifest{
		Manifest:              m,
		ExecutionTier:         execTier,
		ExtensionKind:         extKind,
		PublisherID:           publisherID,
		RequiresRootSignature: IsReservedTanpopoNamespace(id),
		Permissions:           permSet,
	}, nil
}

func validateAdapterBlockSafe(adapter map[string]any, execTier string) error {
	var walk func(v any) error
	walk = func(v any) error {
		switch node := v.(type) {
		case map[string]any:
			for k, val := range node {
				lk := strings.ToLower(strings.TrimSpace(k))
				switch lk {
				case "script", "eval", "exec", "postinstall", "install_hook", "preinstall", "shell":
					return fmt.Errorf("%w: adapter key %q is strictly forbidden (zero install/eval hooks)", ErrManifestInvalid, k)
				case "scoped_secret_unseal":
					if execTier != ExecutionTierIsolatedSidecarT3 {
						return fmt.Errorf("%w: adapter key %q forbidden in %s", ErrProhibitedCapabilityTier, k, execTier)
					}
				}
				if err := walk(val); err != nil {
					return err
				}
			}
		case []any:
			for _, elem := range node {
				if err := walk(elem); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(adapter)
}

var dnsLabelRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func validateManifestAllowedHost(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.ContainsRune(trimmed, '*') {
		return fmt.Errorf("%w: wildcard or empty allowedHosts entry %q is forbidden", ErrWildcardEgressForbidden, raw)
	}
	if strings.ContainsAny(trimmed, " \t\r\n\x00/\\@?#%") || strings.Contains(trimmed, "://") {
		return fmt.Errorf("%w: malformed allowedHosts entry %q (expected hostname or host:port)", ErrManifestInvalid, raw)
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] < 0x21 || trimmed[i] > 0x7e {
			return fmt.Errorf("%w: non-ASCII or control byte in allowedHosts entry %q (use punycode ASCII)", ErrManifestInvalid, raw)
		}
	}

	hostPart := trimmed
	if strings.HasPrefix(trimmed, "[") {
		if strings.HasSuffix(trimmed, "]") {
			ipStr := trimmed[1 : len(trimmed)-1]
			ip, err := netip.ParseAddr(ipStr)
			if err != nil || !ip.Is6() || ip.Zone() != "" {
				return fmt.Errorf("%w: invalid IPv6 allowedHosts entry %q", ErrManifestInvalid, raw)
			}
			return nil
		}
		host, portStr, err := net.SplitHostPort(trimmed)
		if err != nil {
			return fmt.Errorf("%w: malformed bracketed IPv6 allowedHosts entry %q", ErrManifestInvalid, raw)
		}
		port, perr := strconv.Atoi(portStr)
		if perr != nil || port < 1 || port > 65535 {
			return fmt.Errorf("%w: invalid port in allowedHosts entry %q", ErrManifestInvalid, raw)
		}
		ip, ierr := netip.ParseAddr(host)
		if ierr != nil || !ip.Is6() || ip.Zone() != "" {
			return fmt.Errorf("%w: invalid IPv6 host in allowedHosts entry %q", ErrManifestInvalid, raw)
		}
		return nil
	}

	if strings.ContainsRune(trimmed, '[') || strings.ContainsRune(trimmed, ']') {
		return fmt.Errorf("%w: stray bracket in allowedHosts entry %q", ErrManifestInvalid, raw)
	}
	if strings.ContainsRune(trimmed, ':') {
		host, portStr, err := net.SplitHostPort(trimmed)
		if err != nil {
			return fmt.Errorf("%w: invalid host:port in allowedHosts entry %q", ErrManifestInvalid, raw)
		}
		port, perr := strconv.Atoi(portStr)
		if perr != nil || port < 1 || port > 65535 {
			return fmt.Errorf("%w: invalid port in allowedHosts entry %q", ErrManifestInvalid, raw)
		}
		hostPart = host
	}

	if ip, err := netip.ParseAddr(hostPart); err == nil {
		if !ip.Is4() || ip.Zone() != "" {
			return fmt.Errorf("%w: bare IPv6 %q must be bracketed and zone-free", ErrManifestInvalid, raw)
		}
		return nil
	}
	if strings.HasPrefix(hostPart, ".") || strings.HasSuffix(hostPart, ".") || len(hostPart) > 253 {
		return fmt.Errorf("%w: invalid hostname %q in allowedHosts", ErrManifestInvalid, raw)
	}
	labels := strings.Split(hostPart, ".")
	for _, lbl := range labels {
		if !dnsLabelRe.MatchString(lbl) {
			return fmt.Errorf("%w: invalid DNS label %q in allowedHosts entry %q", ErrManifestInvalid, lbl, raw)
		}
	}
	return nil
}

// semverVersion implements deterministic SemVer comparison for coreVersionRange checks (CT-24).
type semverVersion struct {
	major int
	minor int
	patch int
	pre   string
}

func parseSemver(raw string) (semverVersion, error) {
	cleaned := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	m := semverRe.FindStringSubmatch(cleaned)
	if m == nil {
		return semverVersion{}, fmt.Errorf("invalid semver %q", raw)
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	pat, _ := strconv.Atoi(m[3])
	return semverVersion{major: maj, minor: min, patch: pat, pre: m[4]}, nil
}

func compareSemver(a, b semverVersion) int {
	if a.major != b.major {
		if a.major < b.major {
			return -1
		}
		return 1
	}
	if a.minor != b.minor {
		if a.minor < b.minor {
			return -1
		}
		return 1
	}
	if a.patch != b.patch {
		if a.patch < b.patch {
			return -1
		}
		return 1
	}
	if a.pre == b.pre {
		return 0
	}
	if a.pre == "" {
		return 1
	}
	if b.pre == "" {
		return -1
	}
	if a.pre < b.pre {
		return -1
	}
	return 1
}

// SemverMatchesRange evaluates whether version satisfies a space/comma/OR SemVer range expression.
func SemverMatchesRange(versionStr, rangeExpr string) (bool, error) {
	ver, err := parseSemver(versionStr)
	if err != nil {
		return false, err
	}
	expr := strings.TrimSpace(rangeExpr)
	if expr == "" {
		return false, fmt.Errorf("empty version range")
	}

	orBranches := strings.Split(expr, "||")
	for _, branch := range orBranches {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			return false, fmt.Errorf("empty OR branch in version range %q", rangeExpr)
		}
		clauses := strings.Fields(strings.ReplaceAll(branch, ",", " "))
		if len(clauses) == 0 {
			return false, fmt.Errorf("empty clause in version range %q", rangeExpr)
		}
		allMatch := true
		for _, clause := range clauses {
			ok, cerr := matchSingleComparator(ver, clause)
			if cerr != nil {
				return false, cerr
			}
			if !ok {
				allMatch = false
			}
		}
		if allMatch {
			return true, nil
		}
	}
	return false, nil
}

func belowExclusiveUpperTuple(ver, upper semverVersion) bool {
	if ver.major == upper.major && ver.minor == upper.minor && ver.patch == upper.patch {
		return false // Do not let 0.2.0-alpha.1 match <0.2.0 upper cap
	}
	return compareSemver(ver, upper) < 0
}

func matchSingleComparator(ver semverVersion, clause string) (bool, error) {
	c := strings.TrimSpace(clause)
	switch {
	case strings.HasPrefix(c, ">="):
		target, err := parseSemver(c[2:])
		return err == nil && compareSemver(ver, target) >= 0, err
	case strings.HasPrefix(c, "<="):
		target, err := parseSemver(c[2:])
		return err == nil && compareSemver(ver, target) <= 0, err
	case strings.HasPrefix(c, ">"):
		target, err := parseSemver(c[1:])
		return err == nil && compareSemver(ver, target) > 0, err
	case strings.HasPrefix(c, "<"):
		target, err := parseSemver(c[1:])
		return err == nil && compareSemver(ver, target) < 0, err
	case strings.HasPrefix(c, "="):
		target, err := parseSemver(c[1:])
		return err == nil && compareSemver(ver, target) == 0, err
	case strings.HasPrefix(c, "^"):
		base, err := parseSemver(c[1:])
		if err != nil {
			return false, err
		}
		var upper semverVersion
		if base.major > 0 {
			upper = semverVersion{major: base.major + 1}
		} else if base.minor > 0 {
			upper = semverVersion{major: 0, minor: base.minor + 1}
		} else {
			upper = semverVersion{major: 0, minor: 0, patch: base.patch + 1}
		}
		return compareSemver(ver, base) >= 0 && belowExclusiveUpperTuple(ver, upper), nil
	case strings.HasPrefix(c, "~"):
		base, err := parseSemver(c[1:])
		if err != nil {
			return false, err
		}
		upper := semverVersion{major: base.major, minor: base.minor + 1}
		return compareSemver(ver, base) >= 0 && belowExclusiveUpperTuple(ver, upper), nil
	default:
		target, err := parseSemver(c)
		if err != nil {
			return false, err
		}
		return compareSemver(ver, target) == 0, nil
	}
}
