// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package wasm implements Tanpopo's Tier-2 pure-Go wazero Wasm Sandbox Pool
// satisfying the shared extension/port.ExtensionRuntime contract (TP-IMP-0013,
// ADR-0002, SPIKE-02 / POC-01) under a Default-Deny Zero-Ambient-Authority
// boundary: zero filesystem access, zero environment variable inheritance,
// zero raw sockets, <= 16 MiB default linear memory ceiling, in-flight
// caller cancellation, pooled atomic watchdogs, and FMEA-05 auto-quarantine.
//
// DAG Boundary (Tier 5): Imports Go standard library, github.com/tetratelabs/wazero,
// internal/pkg/idgen, internal/storage/port, internal/security/{broker,egress},
// and internal/extension/port. Never imports internal/testkit or higher layers.
package wasm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
)

const (
	// WasmPageBytes is the fixed WebAssembly linear-memory page size (64 KiB).
	WasmPageBytes uint32 = 65536
	// DefaultMemoryLimitPages enforces the 16 MiB (256 * 64 KiB) per-instance cap.
	DefaultMemoryLimitPages uint32 = 256
	// AbsoluteMaxMemoryLimitPages caps explicitly approved extended-memory guests at 64 MiB.
	AbsoluteMaxMemoryLimitPages uint32 = 1024

	// DefaultPoolSize bounds concurrent pooled Wasm instances per extension runtime.
	DefaultPoolSize = 16
	// MaxPoolSize caps configured pool concurrency.
	MaxPoolSize = 256

	// DefaultCallTimeout bounds guest CPU execution after slot acquisition (>= 50 ms watchdog floor).
	DefaultCallTimeout = 50 * time.Millisecond
	// DefaultWatchdogInterval controls how frequently the single pool-wide watchdog checks deadlines.
	DefaultWatchdogInterval = 5 * time.Millisecond
	// DefaultInitTimeout bounds module instantiation and any guest (start) section.
	DefaultInitTimeout = 2 * time.Second

	// MaxGuestPayloadBytes caps serialized Host<->Guest payload exchanges in linear memory (4 MiB).
	MaxGuestPayloadBytes uint32 = 4 * 1024 * 1024
	// MaxStreamSessionStateBytes enforces the 64 KiB per-stream scratch state limit (SPEC-EXT-01).
	MaxStreamSessionStateBytes = 64 * 1024
	// MaxWasmBinaryBytes caps raw untrusted .wasm modules before pre-compile inspection (64 MiB).
	MaxWasmBinaryBytes = 64 * 1024 * 1024
	// MaxWasmCodeBytes caps total Section 10 (Code) payload bytes to bound CompileModule CPU/RAM (16 MiB).
	MaxWasmCodeBytes = 16 * 1024 * 1024
	// MaxWasmTables caps declared WebAssembly tables per module (Section 4).
	MaxWasmTables uint32 = 1
	// MaxWasmTableElements caps declared WebAssembly table element entries per slot (512 KiB max).
	MaxWasmTableElements uint32 = 65536
	// MaxWasmTypes caps declared function signatures per module (Section 1).
	MaxWasmTypes uint32 = 16384
	// MaxWasmImports caps declared imports per module (Section 2).
	MaxWasmImports uint32 = 64
	// MaxWasmFunctions caps declared defined functions per module (Sections 3 and 10).
	MaxWasmFunctions uint32 = 16384
	// MaxWasmMemories caps declared linear memories per module (Section 5).
	MaxWasmMemories uint32 = 1
	// MaxWasmGlobals caps declared globals per module (Section 6).
	MaxWasmGlobals uint32 = 4096
	// MaxWasmExports caps declared exports per module (Section 7).
	MaxWasmExports uint32 = 4096
	// MaxWasmElements caps declared element segments per module (Section 9).
	MaxWasmElements uint32 = 4096
	// MaxWasmDataSegments caps declared data segments per module (Sections 11 and 12).
	MaxWasmDataSegments uint32 = 4096
	// MaxWasmFunctionLocals caps total local slots in any single Wasm function body (Section 10).
	MaxWasmFunctionLocals uint32 = 65536
	// MaxWasmTotalLocals caps total local slots summed across all functions in a module (Section 10).
	MaxWasmTotalLocals uint64 = 1048576

	// HostModuleName is the sole optional Host import namespace for BlindBroker HTTP egress.
	HostModuleName = "tanpopo_host_v1"
	// HostFuncBlindHTTPEgress executes outbound HTTP calls exclusively via Host BlindBroker + SSRFGuard.
	HostFuncBlindHTTPEgress = "blind_http_egress"
)

var globalWasmEnabled atomic.Bool

func init() {
	globalWasmEnabled.Store(true)
}

// SetWasmEnabled toggles the global emergency kill-switch (`runtime.wasm.enabled`, TP-IMP-0013).
// Disabling Tier-2 Wasm fails new Wasm invocations closed while keeping Tier-1 Declarative Adapters online.
func SetWasmEnabled(enabled bool) {
	globalWasmEnabled.Store(enabled)
}

// IsWasmEnabled reports whether Tier-2 Wasm runtime execution is enabled globally.
func IsWasmEnabled() bool {
	return globalWasmEnabled.Load()
}

// Canonical errors returned by the Tier-2 Wasm sandbox runtime.
var (
	// ErrInvalidRuntimeConfig indicates malformed RuntimeConfig or missing guest entrypoints.
	ErrInvalidRuntimeConfig = fmt.Errorf("%w: invalid wasm runtime configuration", port.ErrInvalidSpec)
	// ErrUnapprovedWASIImport indicates a module attempted to import WASI or other forbidden host capabilities.
	ErrUnapprovedWASIImport = fmt.Errorf("%w: unapproved_wasi_import", port.ErrInvalidSpec)
	// ErrWasmRuntimeDisabled indicates `runtime.wasm.enabled = false` emergency rollback is active.
	ErrWasmRuntimeDisabled = fmt.Errorf("%w: runtime.wasm.enabled=false (tier-2 wasm disabled)", port.ErrExecutionFault)
	// ErrWasmRuntimeClosed indicates the Wasm pool/runtime was closed before or during execution.
	ErrWasmRuntimeClosed = fmt.Errorf("%w: wasm runtime closed", port.ErrExecutionFault)
)

// FaultKind classifies isolated Tier-2 Wasm execution failures.
type FaultKind string

const (
	FaultKindTrap         FaultKind = "trap"
	FaultKindTimeout      FaultKind = "timeout"
	FaultKindOOM          FaultKind = "oom"
	FaultKindABIViolation FaultKind = "abi_violation"
	FaultKindQuarantined  FaultKind = "quarantined"
	FaultKindClosed       FaultKind = "closed"
)

// ExecutionFaultError maps isolated Wasm guest traps, timeouts, memory exhaustion,
// ABI violations, or post-threshold quarantine onto HTTP 502 extension_execution_fault.
type ExecutionFaultError struct {
	Kind   FaultKind
	Reason string
	Cause  error
}

// Error implements error without exposing untrusted secret material.
func (e *ExecutionFaultError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("extension_execution_fault (502, %s): %s: %v", e.Kind, e.Reason, e.Cause)
	}
	return fmt.Sprintf("extension_execution_fault (502, %s): %s", e.Kind, e.Reason)
}

// Unwrap links ExecutionFaultError to port.ErrExecutionFault (and underlying Cause when present).
func (e *ExecutionFaultError) Unwrap() []error {
	if e == nil {
		return nil
	}
	if e.Cause != nil {
		return []error{port.ErrExecutionFault, e.Cause}
	}
	return []error{port.ErrExecutionFault}
}

// HTTPStatus returns canonical HTTP status 502 for ingress error mapping.
func (e *ExecutionFaultError) HTTPStatus() int { return http.StatusBadGateway }

// CanonicalCode returns canonical Data Plane error code extension_execution_fault.
func (e *ExecutionFaultError) CanonicalCode() string { return "extension_execution_fault" }

// ErrorType returns canonical SPEC-API error_type extension_error.
func (e *ExecutionFaultError) ErrorType() string { return "extension_error" }

// ABIMode selects how Host RPC payloads map onto the guest's Linear-Memory Slice ABI.
type ABIMode string

const (
	// ABIModeCanonicalEnvelope exchanges canonical ExtensionRuntime JSON envelopes over (ptr, len) -> u64.
	ABIModeCanonicalEnvelope ABIMode = "canonical_envelope"
	// ABIModeSliceTransform passes raw request body / SSE chunk slices directly into `"transform"` (POC-01).
	ABIModeSliceTransform ABIMode = "slice_transform"
)

// SliceTransformConfig configures Host-managed metadata for ABIModeSliceTransform modules
// such as minimal linear-memory slice transforms (`echo_transform.wasm`).
type SliceTransformConfig struct {
	Method               string
	TargetURL            string
	SafeHeaders          map[string]string
	CredentialInjections []port.CredentialInjectInstruction
}

// RuntimeConfig configures a single compiled & pooled Tier-2 Wasm extension module.
type RuntimeConfig struct {
	WasmBytes           []byte
	InstallationID      string
	PoolSize            int
	MemoryLimitPages    uint32
	AllowExtendedMemory bool
	CallTimeout         time.Duration
	StreamChunkTimeout  time.Duration
	WatchdogInterval    time.Duration
	InitTimeout         time.Duration
	InputOffset         uint32
	MaxOutputBytes      uint32

	ABIMode        ABIMode
	SliceTransform *SliceTransformConfig

	// Permissions & BlindBroker integration (SM-01 approved subset).
	AllowedHosts         []string
	AllowPrivateIP       bool
	AllowedCIDRs         []string
	AllowedPorts         []int
	AllowInsecureHTTP    bool
	ApprovedSecretSlots  []string
	AllowHostBlindEgress bool
	BlindBroker          *broker.BlindHostCredentialBroker
	DefaultEgressScope   *EgressScope
	FaultGuard           *FaultGuard
	EnabledFunc          func() bool
	CompilationCache     wazero.CompilationCache
}

// EgressScope identifies the trusted Host Org/Connection identity executing an adapter call.
// Untrusted Wasm guests can never set or override these identifiers.
type EgressScope struct {
	OrgID        string
	ConnectionID string
	ActorKind    string
	ActorID      string
	EntryPoint   string
	RequestID    string
	NarrowPolicy *egress.Policy
}

type egressScopeContextKey struct{}

// WithEgressScope binds a trusted Host EgressScope onto ctx for blind_http_egress calls.
func WithEgressScope(ctx context.Context, scope EgressScope) context.Context {
	return context.WithValue(ctx, egressScopeContextKey{}, scope)
}

func egressScopeFromContext(ctx context.Context) (EgressScope, bool) {
	if ctx == nil {
		return EgressScope{}, false
	}
	sc, ok := ctx.Value(egressScopeContextKey{}).(EgressScope)
	return sc, ok
}

// Runtime manages a Zero-Ambient-Authority wazero Runtime, pre-compiled module,
// bounded instance pool, and optional FMEA-05 FaultGuard for one Tier-2 extension.
type Runtime struct {
	installationID string
	abiMode        ABIMode
	sliceCfg       *SliceTransformConfig

	callTimeout        time.Duration
	streamChunkTimeout time.Duration

	requireTLS   bool
	narrowPolicy *egress.Policy

	approvedSlots map[string]struct{}

	allowHostBlindEgress bool
	blindBroker          *broker.BlindHostCredentialBroker
	defaultScope         *EgressScope
	faultGuard           *FaultGuard
	enabledFunc          func() bool

	wazeroRT  wazero.Runtime
	compiled  wazero.CompiledModule
	pool      *wasmPool
	closeOnce sync.Once
	closeErr  error
}

// NewRuntime compiles wasmBytes once under Zero-Ambient-Authority wazero isolation,
// validates all imports (rejecting unapproved WASI or socket imports), and warms up
// a bounded pool of memory-capped module instances.
func NewRuntime(ctx context.Context, cfg RuntimeConfig) (*Runtime, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(cfg.WasmBytes) == 0 {
		return nil, fmt.Errorf("%w: non-empty WasmBytes is required", ErrInvalidRuntimeConfig)
	}
	if len(cfg.WasmBytes) > MaxWasmBinaryBytes {
		return nil, fmt.Errorf("%w: wasm binary %d bytes exceeds max %d", ErrInvalidRuntimeConfig, len(cfg.WasmBytes), MaxWasmBinaryBytes)
	}
	poolSize := cfg.PoolSize
	if poolSize == 0 {
		poolSize = DefaultPoolSize
	}
	if poolSize < 1 || poolSize > MaxPoolSize {
		return nil, fmt.Errorf("%w: pool size %d out of range [1..%d]", ErrInvalidRuntimeConfig, poolSize, MaxPoolSize)
	}

	memPages := cfg.MemoryLimitPages
	if memPages == 0 {
		memPages = DefaultMemoryLimitPages
	}
	maxCap := DefaultMemoryLimitPages
	if cfg.AllowExtendedMemory {
		maxCap = AbsoluteMaxMemoryLimitPages
	}
	if memPages > maxCap {
		return nil, fmt.Errorf("%w: memory limit pages %d exceeds maximum %d", ErrInvalidRuntimeConfig, memPages, maxCap)
	}

	callTimeout := cfg.CallTimeout
	if callTimeout <= 0 {
		callTimeout = DefaultCallTimeout
	}
	chunkTimeout := cfg.StreamChunkTimeout
	if chunkTimeout <= 0 {
		chunkTimeout = callTimeout
	}
	watchdogInterval := cfg.WatchdogInterval
	if watchdogInterval <= 0 {
		watchdogInterval = DefaultWatchdogInterval
	}
	initTimeout := cfg.InitTimeout
	if initTimeout <= 0 {
		initTimeout = DefaultInitTimeout
	}
	maxOutput := cfg.MaxOutputBytes
	if maxOutput == 0 {
		maxOutput = MaxGuestPayloadBytes
	}

	abiMode := cfg.ABIMode
	if abiMode == "" {
		abiMode = ABIModeCanonicalEnvelope
	}
	var sliceCopy *SliceTransformConfig
	if abiMode == ABIModeSliceTransform {
		if cfg.SliceTransform == nil {
			return nil, fmt.Errorf("%w: SliceTransform config is required for ABIModeSliceTransform", ErrInvalidRuntimeConfig)
		}
		sc := *cfg.SliceTransform
		sc.SafeHeaders = cloneStringMap(cfg.SliceTransform.SafeHeaders)
		sc.CredentialInjections = append([]port.CredentialInjectInstruction(nil), cfg.SliceTransform.CredentialInjections...)
		probeSpec := port.UpstreamHttpSpec{
			Method:               sc.Method,
			TargetURL:            sc.TargetURL,
			SafeHeaders:          sc.SafeHeaders,
			CredentialInjections: sc.CredentialInjections,
		}
		if err := probeSpec.Validate(); err != nil {
			return nil, fmt.Errorf("%w: invalid SliceTransform spec: %v", ErrInvalidRuntimeConfig, err)
		}
		sliceCopy = &sc
	} else if abiMode != ABIModeCanonicalEnvelope {
		return nil, fmt.Errorf("%w: unsupported ABI mode %q", ErrInvalidRuntimeConfig, abiMode)
	}

	if err := validateNetworkConfig(cfg.AllowedPorts, cfg.AllowPrivateIP, cfg.AllowedCIDRs); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRuntimeConfig, err)
	}
	if len(cfg.AllowedHosts) == 0 && (len(cfg.AllowedPorts) > 0 || len(cfg.AllowedCIDRs) > 0 || cfg.AllowPrivateIP) {
		return nil, fmt.Errorf("%w: allowed_ports, allowed_cidrs, and allow_private_ip require non-empty AllowedHosts", ErrInvalidRuntimeConfig)
	}
	var narrowPolicy *egress.Policy
	if len(cfg.AllowedHosts) > 0 {
		narrowPolicy = &egress.Policy{
			AllowedHosts:   slices.Clone(cfg.AllowedHosts),
			AllowPrivateIP: cfg.AllowPrivateIP,
			AllowedCIDRs:   slices.Clone(cfg.AllowedCIDRs),
			AllowedPorts:   slices.Clone(cfg.AllowedPorts),
			RequireTLS:     !cfg.AllowInsecureHTTP,
		}
		if _, err := egress.NewDialer(egress.DialerConfig{Policy: *narrowPolicy}); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidRuntimeConfig, err)
		}
	}

	if cfg.AllowHostBlindEgress {
		if cfg.BlindBroker == nil {
			return nil, fmt.Errorf("%w: non-nil BlindBroker is required when AllowHostBlindEgress is enabled", ErrInvalidRuntimeConfig)
		}
		if len(cfg.AllowedHosts) == 0 {
			return nil, fmt.Errorf("%w: non-empty AllowedHosts is required when AllowHostBlindEgress is enabled", ErrInvalidRuntimeConfig)
		}
	}

	approvedSlots := make(map[string]struct{}, len(cfg.ApprovedSecretSlots))
	for _, s := range cfg.ApprovedSecretSlots {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" {
			approvedSlots[trimmed] = struct{}{}
		}
	}

	var defaultScopeCopy *EgressScope
	if cfg.DefaultEgressScope != nil {
		cp := *cfg.DefaultEgressScope
		if cp.NarrowPolicy != nil {
			npCopy := cloneEgressPolicy(cp.NarrowPolicy)
			cp.NarrowPolicy = npCopy
		}
		defaultScopeCopy = &cp
	}

	allowBlind := cfg.AllowHostBlindEgress

	hasMutableState, err := inspectWasmBinary(cfg.WasmBytes, allowBlind)
	if err != nil {
		return nil, err
	}

	coreFeatures := api.CoreFeaturesV2.SetEnabled(api.CoreFeatureReferenceTypes, false)
	rCfg := wazero.NewRuntimeConfig().
		WithCoreFeatures(coreFeatures).
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(memPages).
		WithDebugInfoEnabled(false)
	if cfg.CompilationCache != nil {
		rCfg = rCfg.WithCompilationCache(cfg.CompilationCache)
	}

	wrt := wazero.NewRuntimeWithConfig(ctx, rCfg)
	closeOnErr := func() {
		_ = wrt.Close(context.Background())
	}

	compiled, err := wrt.CompileModule(ctx, cfg.WasmBytes)
	if err != nil {
		closeOnErr()
		return nil, fmt.Errorf("%w: compile wasm module: %v", ErrInvalidRuntimeConfig, err)
	}

	maxLinearMemBytes, err := validateCompiledModule(compiled, memPages, allowBlind, abiMode)
	if err != nil {
		_ = compiled.Close(context.Background())
		closeOnErr()
		return nil, err
	}

	_, hasAlloc := compiled.ExportedFunctions()["tanpopo_alloc"]
	reinstantiateAfterCall := hasMutableState || hasAlloc

	rt := &Runtime{
		installationID:       strings.TrimSpace(cfg.InstallationID),
		abiMode:              abiMode,
		sliceCfg:             sliceCopy,
		callTimeout:          callTimeout,
		streamChunkTimeout:   chunkTimeout,
		requireTLS:           !cfg.AllowInsecureHTTP,
		narrowPolicy:         narrowPolicy,
		approvedSlots:        approvedSlots,
		allowHostBlindEgress: allowBlind,
		blindBroker:          cfg.BlindBroker,
		defaultScope:         defaultScopeCopy,
		faultGuard:           cfg.FaultGuard,
		enabledFunc:          cfg.EnabledFunc,
		wazeroRT:             wrt,
		compiled:             compiled,
	}

	if abiMode == ABIModeSliceTransform {
		probeSpec := rt.sliceSpecFromConfig(nil, nil)
		if err := rt.validateOutboundSpec(probeSpec); err != nil {
			_ = compiled.Close(context.Background())
			closeOnErr()
			return nil, fmt.Errorf("%w: SliceTransform violates outbound policy: %v", ErrInvalidRuntimeConfig, err)
		}
	}

	if allowBlind {
		if _, err := wrt.NewHostModuleBuilder(HostModuleName).
			NewFunctionBuilder().
			WithGoModuleFunction(api.GoModuleFunc(rt.hostBlindHTTPEgress),
				[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
				[]api.ValueType{api.ValueTypeI32},
			).
			Export(HostFuncBlindHTTPEgress).
			Instantiate(ctx); err != nil {
			_ = compiled.Close(context.Background())
			closeOnErr()
			return nil, fmt.Errorf("%w: instantiate host egress bridge: %v", ErrInvalidRuntimeConfig, err)
		}
	}

	pool, err := newWasmPool(ctx, wrt, compiled, wasmPoolConfig{
		size:                   poolSize,
		inputOffset:            cfg.InputOffset,
		maxOutputBytes:         maxOutput,
		maxLinearMemBytes:      maxLinearMemBytes,
		defaultTimeout:         callTimeout,
		watchdogInterval:       watchdogInterval,
		initTimeout:            initTimeout,
		reinstantiateAfterCall: reinstantiateAfterCall,
	})
	if err != nil {
		_ = compiled.Close(context.Background())
		closeOnErr()
		return nil, err
	}
	rt.pool = pool

	if rt.faultGuard != nil && rt.installationID != "" {
		rt.faultGuard.registerPool(rt.installationID, pool)
	}
	return rt, nil
}

// Close shuts down the pool, evicts all Wasm instances, releases the compiled module,
// and closes the underlying wazero Runtime cleanly without leaking goroutines.
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		if r.faultGuard != nil && r.installationID != "" && r.pool != nil {
			r.faultGuard.unregisterPool(r.installationID, r.pool)
		}
		var errs []error
		if r.pool != nil {
			if err := r.pool.Close(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		if r.compiled != nil {
			if err := r.compiled.Close(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		if r.wazeroRT != nil {
			if err := r.wazeroRT.Close(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		r.closeErr = errors.Join(errs...)
	})
	return r.closeErr
}

// PoolStats returns runtime observability metrics from the underlying module pool.
type PoolStats struct {
	Capacity      int
	Available     int
	ReplacedCount int64
}

// Stats returns current pool capacity, available slots, and tainted-module replacements.
func (r *Runtime) Stats() PoolStats {
	if r == nil || r.pool == nil {
		return PoolStats{}
	}
	return r.pool.stats()
}

// InspectLinearMemory scans idle pooled module instances for substr (used to verify zero
// secret or cross-tenant residue inside Wasm linear memory without bypassing safety locks).
func (r *Runtime) InspectLinearMemory(match func([]byte) bool) error {
	if r == nil || r.pool == nil || match == nil {
		return nil
	}
	return r.pool.inspectIdleMemory(match)
}

func (r *Runtime) isEnabled() bool {
	if r != nil && r.enabledFunc != nil {
		return r.enabledFunc()
	}
	return IsWasmEnabled()
}

func (r *Runtime) validateOutboundSpec(spec port.UpstreamHttpSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if r.requireTLS {
		u, err := url.Parse(strings.TrimSpace(spec.TargetURL))
		if err != nil || !strings.EqualFold(u.Scheme, "https") {
			return fmt.Errorf("%w: https scheme is required when AllowInsecureHTTP is false", port.ErrInvalidSpec)
		}
	}
	if r.narrowPolicy == nil {
		return fmt.Errorf("%w: target_url denied by empty SM-01 allowed_hosts", port.ErrInvalidSpec)
	}
	if _, err := r.narrowPolicy.ValidateTargetURL(spec.TargetURL); err != nil {
		return fmt.Errorf("%w: %v", port.ErrInvalidSpec, err)
	}
	if err := r.validateSecretSlots(spec.CredentialInjections); err != nil {
		return err
	}
	return nil
}

func (r *Runtime) validateSecretSlots(injections []port.CredentialInjectInstruction) error {
	for _, inj := range injections {
		if err := inj.Validate(); err != nil {
			return err
		}
		if _, ok := r.approvedSlots[inj.SecretSlotName]; !ok {
			return fmt.Errorf("%w: unapproved secret_slot_name %q", port.ErrInvalidSpec, inj.SecretSlotName)
		}
	}
	return nil
}

func validateNetworkConfig(ports []int, allowPrivateIP bool, cidrs []string) error {
	for _, pt := range ports {
		if pt < 1 || pt > 65535 {
			return fmt.Errorf("invalid allowed port %d", pt)
		}
	}
	if allowPrivateIP && len(cidrs) == 0 {
		return errors.New("allow_private_ip=true requires explicit non-empty allowed_cidrs narrowing")
	}
	for _, raw := range cidrs {
		pfx, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("invalid allowed_cidrs entry %q: %w", raw, err)
		}
		if pfx.Masked().Bits() == 0 {
			return fmt.Errorf("unrestricted wildcard CIDR %q is forbidden", raw)
		}
	}
	return nil
}

func cloneEgressPolicy(p *egress.Policy) *egress.Policy {
	if p == nil {
		return nil
	}
	cp := *p
	cp.AllowedHosts = slices.Clone(p.AllowedHosts)
	cp.AllowedCIDRs = slices.Clone(p.AllowedCIDRs)
	cp.AllowedPorts = slices.Clone(p.AllowedPorts)
	cp.SensitiveHeaders = slices.Clone(p.SensitiveHeaders)
	return &cp
}

func validateCompiledModule(compiled wazero.CompiledModule, memLimitPages uint32, allowHostBlindEgress bool, mode ABIMode) (uint64, error) {
	if mems := compiled.ImportedMemories(); len(mems) > 0 {
		return 0, fmt.Errorf("%w: imported linear memories are forbidden (%d)", ErrUnapprovedWASIImport, len(mems))
	}
	for _, fn := range compiled.ImportedFunctions() {
		modName, fnName, isImport := fn.Import()
		if !isImport {
			continue
		}
		if allowHostBlindEgress && modName == HostModuleName && fnName == HostFuncBlindHTTPEgress {
			params := fn.ParamTypes()
			results := fn.ResultTypes()
			if len(params) == 4 &&
				params[0] == api.ValueTypeI32 &&
				params[1] == api.ValueTypeI32 &&
				params[2] == api.ValueTypeI32 &&
				params[3] == api.ValueTypeI32 &&
				len(results) == 1 && results[0] == api.ValueTypeI32 {
				continue
			}
			return 0, fmt.Errorf("%w: %s.%s signature mismatch", ErrUnapprovedWASIImport, modName, fnName)
		}
		return 0, fmt.Errorf("%w: forbidden import %s.%s", ErrUnapprovedWASIImport, modName, fnName)
	}

	expMems := compiled.ExportedMemories()
	memDef, ok := expMems["memory"]
	if !ok {
		return 0, fmt.Errorf("%w: guest module must export linear memory named \"memory\"", ErrInvalidRuntimeConfig)
	}
	if memDef.Min() > memLimitPages {
		return 0, fmt.Errorf("%w: guest declared min pages %d exceeds limit %d", ErrInvalidRuntimeConfig, memDef.Min(), memLimitPages)
	}
	effectiveMaxPages := memLimitPages
	if modMax, hasMax := memDef.Max(); hasMax && modMax < effectiveMaxPages {
		effectiveMaxPages = modMax
	}
	if effectiveMaxPages == 0 {
		return 0, fmt.Errorf("%w: guest declared zero max linear memory pages", ErrInvalidRuntimeConfig)
	}
	maxLinearMemBytes := uint64(effectiveMaxPages) * uint64(WasmPageBytes)

	expFuncs := compiled.ExportedFunctions()
	if allocDef, hasAlloc := expFuncs["tanpopo_alloc"]; hasAlloc {
		p := allocDef.ParamTypes()
		res := allocDef.ResultTypes()
		if len(p) != 1 || p[0] != api.ValueTypeI32 || len(res) != 1 || res[0] != api.ValueTypeI32 {
			return 0, fmt.Errorf("%w: exported tanpopo_alloc must have signature (i32) -> i32", ErrInvalidRuntimeConfig)
		}
	}
	if mode == ABIModeSliceTransform {
		fnDef, hasTransform := expFuncs["transform"]
		if !hasTransform || !isValidSliceABISig(fnDef) {
			return 0, fmt.Errorf("%w: ABIModeSliceTransform requires exported func transform(i32, i32) -> i64", ErrInvalidRuntimeConfig)
		}
	} else {
		hasCallable := false
		for name, fnDef := range expFuncs {
			if name == "tanpopo_alloc" {
				continue
			}
			if isValidSliceABISig(fnDef) {
				hasCallable = true
				break
			}
		}
		if !hasCallable {
			return 0, fmt.Errorf("%w: guest module must export at least one POC-01 (i32, i32) -> i64 entrypoint", ErrInvalidRuntimeConfig)
		}
	}
	return maxLinearMemBytes, nil
}

func isValidSliceABISig(fn api.FunctionDefinition) bool {
	if fn == nil {
		return false
	}
	p := fn.ParamTypes()
	r := fn.ResultTypes()
	return len(p) == 2 && p[0] == api.ValueTypeI32 && p[1] == api.ValueTypeI32 &&
		len(r) == 1 && r[0] == api.ValueTypeI64
}

// inspectWasmBinary performs fail-closed, zero-allocation validation across all Wasm sections
// (IDs 0..12) prior to invoking wazero's compiler, neutralizing oversized LEB128 vector count
// pre-allocations, Section 10 compressed locals bombs, malformed framing, duplicate/out-of-order
// sections, unapproved imports, unbounded/oversized tables, and tracking mutable state.
func inspectWasmBinary(wasmBytes []byte, allowHostBlindEgress bool) (bool, error) {
	if len(wasmBytes) < 8 ||
		wasmBytes[0] != 0x00 || wasmBytes[1] != 0x61 || wasmBytes[2] != 0x73 || wasmBytes[3] != 0x6d ||
		wasmBytes[4] != 0x01 || wasmBytes[5] != 0x00 || wasmBytes[6] != 0x00 || wasmBytes[7] != 0x00 {
		return true, fmt.Errorf("%w: invalid wasm binary magic or version", ErrInvalidRuntimeConfig)
	}

	var seen [13]bool
	lastOrder := 0
	hasMutableState := false
	var funcSecCount uint32
	var codeSecCount uint32
	var dataSecCount uint32
	var dataCountVal uint32
	var hasDataCount bool

	pos := 8
	for pos < len(wasmBytes) {
		secID := wasmBytes[pos]
		pos++
		secLen, ok := readULEB32(wasmBytes, &pos)
		if !ok || uint64(secLen) > uint64(len(wasmBytes)-pos) {
			return true, fmt.Errorf("%w: truncated or malformed wasm section %d header", ErrInvalidRuntimeConfig, secID)
		}
		payload := wasmBytes[pos : pos+int(secLen)]
		pos += int(secLen)

		if secID == 0 {
			cp := 0
			if !skipWasmName(payload, &cp) {
				return true, fmt.Errorf("%w: malformed custom section name", ErrInvalidRuntimeConfig)
			}
			continue
		}
		rank := wasmSectionOrder(secID)
		if rank == 0 {
			return true, fmt.Errorf("%w: unknown wasm section id %d", ErrInvalidRuntimeConfig, secID)
		}
		if seen[secID] || rank <= lastOrder {
			return true, fmt.Errorf("%w: duplicate or out-of-order wasm section %d", ErrInvalidRuntimeConfig, secID)
		}
		seen[secID] = true
		lastOrder = rank

		switch secID {
		case 1: // Type section
			if err := validateWasmTypeSection(payload); err != nil {
				return true, err
			}
		case 2: // Import section
			if err := validateWasmImportSection(payload, allowHostBlindEgress); err != nil {
				return true, err
			}
		case 3: // Function section
			n, err := validateWasmFunctionSection(payload)
			if err != nil {
				return true, err
			}
			funcSecCount = n
		case 4: // Table section
			hasTable, err := validateWasmTableSection(payload)
			if err != nil {
				return true, err
			}
			if hasTable {
				hasMutableState = true
			}
		case 5: // Memory section
			if err := validateWasmMemorySection(payload); err != nil {
				return true, err
			}
		case 6: // Global section
			hasMut, err := validateWasmGlobalSection(payload)
			if err != nil {
				return true, err
			}
			if hasMut {
				hasMutableState = true
			}
		case 7: // Export section
			if err := validateWasmExportSection(payload); err != nil {
				return true, err
			}
		case 8: // Start section
			sp := 0
			if _, ok := readULEB32(payload, &sp); !ok || sp != len(payload) {
				return true, fmt.Errorf("%w: malformed start section", ErrInvalidRuntimeConfig)
			}
			hasMutableState = true
		case 9: // Element section
			hasElem, err := validateWasmElementSection(payload)
			if err != nil {
				return true, err
			}
			if hasElem {
				hasMutableState = true
			}
		case 10: // Code section
			n, err := validateWasmCodeSection(payload)
			if err != nil {
				return true, err
			}
			codeSecCount = n
		case 11: // Data section
			hasPassive, n, err := validateWasmDataSection(payload)
			if err != nil {
				return true, err
			}
			dataSecCount = n
			if hasPassive {
				hasMutableState = true
			}
		case 12: // DataCount section
			dp := 0
			count, dok := readULEB32(payload, &dp)
			if !dok || count > MaxWasmDataSegments || dp != len(payload) {
				return true, fmt.Errorf("%w: malformed datacount section", ErrInvalidRuntimeConfig)
			}
			hasDataCount = true
			dataCountVal = count
			if count > 0 {
				hasMutableState = true
			}
		}
	}
	if funcSecCount != codeSecCount {
		return true, fmt.Errorf("%w: function count %d mismatches code count %d", ErrInvalidRuntimeConfig, funcSecCount, codeSecCount)
	}
	if hasDataCount && dataCountVal != dataSecCount {
		return true, fmt.Errorf("%w: datacount %d mismatches data section count %d", ErrInvalidRuntimeConfig, dataCountVal, dataSecCount)
	}
	return hasMutableState, nil
}

func wasmSectionOrder(secID byte) int {
	switch secID {
	case 1, 2, 3, 4, 5, 6, 7, 8, 9:
		return int(secID)
	case 12:
		return 10
	case 10:
		return 11
	case 11:
		return 12
	default:
		return 0
	}
}

func readBoundedVecCount(payload []byte, pos *int, maxCount uint32, minEntryBytes uint64, secName string) (uint32, error) {
	count, ok := readULEB32(payload, pos)
	if !ok {
		return 0, fmt.Errorf("%w: malformed %s section count", ErrInvalidRuntimeConfig, secName)
	}
	rem := uint64(len(payload) - *pos)
	if count > maxCount {
		return 0, fmt.Errorf("%w: %s count %d exceeds max %d", ErrInvalidRuntimeConfig, secName, count, maxCount)
	}
	if uint64(count)*minEntryBytes > rem {
		return 0, fmt.Errorf("%w: %s count %d exceeds remaining section bytes %d", ErrInvalidRuntimeConfig, secName, count, rem)
	}
	return count, nil
}

func isWasmValType(b byte) bool {
	return isSimpleWasmValType(b) || b == 0x70 || b == 0x6f
}

func validateWasmTypeSection(payload []byte) error {
	pos := 0
	count, err := readBoundedVecCount(payload, &pos, MaxWasmTypes, 3, "type")
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		if pos >= len(payload) || payload[pos] != 0x60 {
			return fmt.Errorf("%w: malformed type entry %d", ErrInvalidRuntimeConfig, i)
		}
		pos++
		paramCount, err := readBoundedVecCount(payload, &pos, 1024, 1, "type params")
		if err != nil {
			return err
		}
		for p := uint32(0); p < paramCount; p++ {
			if !isWasmValType(payload[pos]) {
				return fmt.Errorf("%w: invalid type param valtype 0x%02x", ErrInvalidRuntimeConfig, payload[pos])
			}
			pos++
		}
		resCount, err := readBoundedVecCount(payload, &pos, 1024, 1, "type results")
		if err != nil {
			return err
		}
		for r := uint32(0); r < resCount; r++ {
			if !isWasmValType(payload[pos]) {
				return fmt.Errorf("%w: invalid type result valtype 0x%02x", ErrInvalidRuntimeConfig, payload[pos])
			}
			pos++
		}
	}
	if pos != len(payload) {
		return fmt.Errorf("%w: trailing bytes in type section", ErrInvalidRuntimeConfig)
	}
	return nil
}

func validateWasmFunctionSection(payload []byte) (uint32, error) {
	pos := 0
	count, err := readBoundedVecCount(payload, &pos, MaxWasmFunctions, 1, "function")
	if err != nil {
		return 0, err
	}
	for i := uint32(0); i < count; i++ {
		if _, ok := readULEB32(payload, &pos); !ok {
			return 0, fmt.Errorf("%w: malformed function typeidx %d", ErrInvalidRuntimeConfig, i)
		}
	}
	if pos != len(payload) {
		return 0, fmt.Errorf("%w: trailing bytes in function section", ErrInvalidRuntimeConfig)
	}
	return count, nil
}

func validateWasmTableSection(payload []byte) (bool, error) {
	pos := 0
	count, err := readBoundedVecCount(payload, &pos, MaxWasmTables, 3, "table")
	if err != nil {
		return true, err
	}
	var totalMax uint64
	for i := uint32(0); i < count; i++ {
		if pos+2 > len(payload) {
			return true, fmt.Errorf("%w: truncated table entry %d", ErrInvalidRuntimeConfig, i)
		}
		refType := payload[pos]
		flags := payload[pos+1]
		pos += 2
		if refType != 0x70 {
			return true, fmt.Errorf("%w: unsupported table element type 0x%02x", ErrInvalidRuntimeConfig, refType)
		}
		if flags != 0x01 {
			return true, fmt.Errorf("%w: table %d must declare bounded maximum (flags=0x01)", ErrInvalidRuntimeConfig, i)
		}
		minVal, mok := readULEB32(payload, &pos)
		maxVal, xok := readULEB32(payload, &pos)
		if !mok || !xok || maxVal < minVal || maxVal > MaxWasmTableElements {
			return true, fmt.Errorf("%w: table %d limits [%d..%d] exceed max %d elements", ErrInvalidRuntimeConfig, i, minVal, maxVal, MaxWasmTableElements)
		}
		totalMax += uint64(maxVal)
		if totalMax > uint64(MaxWasmTableElements) {
			return true, fmt.Errorf("%w: total table elements %d exceed max %d", ErrInvalidRuntimeConfig, totalMax, MaxWasmTableElements)
		}
	}
	if pos != len(payload) {
		return true, fmt.Errorf("%w: trailing bytes in table section", ErrInvalidRuntimeConfig)
	}
	return count > 0, nil
}

func validateWasmMemorySection(payload []byte) error {
	pos := 0
	count, err := readBoundedVecCount(payload, &pos, MaxWasmMemories, 2, "memory")
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		if pos >= len(payload) {
			return fmt.Errorf("%w: truncated memory flags %d", ErrInvalidRuntimeConfig, i)
		}
		flags := payload[pos]
		pos++
		if flags != 0x00 && flags != 0x01 {
			return fmt.Errorf("%w: unsupported memory flags 0x%02x", ErrInvalidRuntimeConfig, flags)
		}
		minPages, mok := readULEB32(payload, &pos)
		if !mok || minPages > 65536 {
			return fmt.Errorf("%w: invalid memory min pages %d", ErrInvalidRuntimeConfig, minPages)
		}
		if flags == 0x01 {
			maxPages, xok := readULEB32(payload, &pos)
			if !xok || maxPages < minPages || maxPages > 65536 {
				return fmt.Errorf("%w: invalid memory limits [%d..%d]", ErrInvalidRuntimeConfig, minPages, maxPages)
			}
		}
	}
	if pos != len(payload) {
		return fmt.Errorf("%w: trailing bytes in memory section", ErrInvalidRuntimeConfig)
	}
	return nil
}

func validateWasmExportSection(payload []byte) error {
	pos := 0
	count, err := readBoundedVecCount(payload, &pos, MaxWasmExports, 3, "export")
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		if !skipWasmName(payload, &pos) || pos >= len(payload) {
			return fmt.Errorf("%w: malformed export entry %d", ErrInvalidRuntimeConfig, i)
		}
		kind := payload[pos]
		pos++
		if kind > 0x03 {
			return fmt.Errorf("%w: unknown export kind 0x%02x", ErrInvalidRuntimeConfig, kind)
		}
		if _, ok := readULEB32(payload, &pos); !ok {
			return fmt.Errorf("%w: malformed export index %d", ErrInvalidRuntimeConfig, i)
		}
	}
	if pos != len(payload) {
		return fmt.Errorf("%w: trailing bytes in export section", ErrInvalidRuntimeConfig)
	}
	return nil
}

func validateWasmElementSection(payload []byte) (bool, error) {
	pos := 0
	count, err := readBoundedVecCount(payload, &pos, MaxWasmElements, 2, "element")
	if err != nil {
		return true, err
	}
	for i := uint32(0); i < count; i++ {
		flags, ok := readULEB32(payload, &pos)
		if !ok {
			return true, fmt.Errorf("%w: malformed element segment %d flags", ErrInvalidRuntimeConfig, i)
		}
		if flags >= 4 {
			return true, fmt.Errorf("%w: disallowed reference-types element segment flags %d", ErrInvalidRuntimeConfig, flags)
		}
		if flags == 2 {
			if _, ok := readULEB32(payload, &pos); !ok {
				return true, fmt.Errorf("%w: malformed element tableidx %d", ErrInvalidRuntimeConfig, i)
			}
		}
		if flags == 0 || flags == 2 {
			if !skipWasmConstInitExpr(payload, &pos) {
				return true, fmt.Errorf("%w: malformed element offset expr %d", ErrInvalidRuntimeConfig, i)
			}
		}
		if flags != 0 {
			if pos >= len(payload) || payload[pos] != 0x00 {
				return true, fmt.Errorf("%w: malformed element kind %d", ErrInvalidRuntimeConfig, i)
			}
			pos++
		}
		elemCount, err := readBoundedVecCount(payload, &pos, MaxWasmTableElements, 1, "element indices")
		if err != nil {
			return true, err
		}
		for e := uint32(0); e < elemCount; e++ {
			if _, ok := readULEB32(payload, &pos); !ok {
				return true, fmt.Errorf("%w: malformed element funcidx", ErrInvalidRuntimeConfig)
			}
		}
	}
	if pos != len(payload) {
		return true, fmt.Errorf("%w: trailing bytes in element section", ErrInvalidRuntimeConfig)
	}
	return count > 0, nil
}

func validateWasmCodeSection(payload []byte) (uint32, error) {
	if len(payload) > MaxWasmCodeBytes {
		return 0, fmt.Errorf("%w: code section %d bytes exceeds max %d", ErrInvalidRuntimeConfig, len(payload), MaxWasmCodeBytes)
	}
	pos := 0
	funcCount, err := readBoundedVecCount(payload, &pos, MaxWasmFunctions, 2, "code")
	if err != nil {
		return 0, err
	}
	var totalLocals uint64
	for i := uint32(0); i < funcCount; i++ {
		bodyLen, bok := readULEB32(payload, &pos)
		if !bok || bodyLen == 0 || uint64(bodyLen) > uint64(len(payload)-pos) {
			return 0, fmt.Errorf("%w: truncated or empty code body %d", ErrInvalidRuntimeConfig, i)
		}
		body := payload[pos : pos+int(bodyLen)]
		pos += int(bodyLen)
		bp := 0
		localGroups, lok := readULEB32(body, &bp)
		if !lok || uint64(localGroups)*2 > uint64(len(body)-bp) {
			return 0, fmt.Errorf("%w: invalid local declaration count in func %d", ErrInvalidRuntimeConfig, i)
		}
		var fnLocals uint64
		for g := uint32(0); g < localGroups; g++ {
			n, nok := readULEB32(body, &bp)
			if !nok || bp >= len(body) {
				return 0, fmt.Errorf("%w: truncated local entry in func %d", ErrInvalidRuntimeConfig, i)
			}
			bp++ // valtype byte
			fnLocals += uint64(n)
			totalLocals += uint64(n)
			if fnLocals > uint64(MaxWasmFunctionLocals) || totalLocals > MaxWasmTotalLocals {
				return 0, fmt.Errorf("%w: function %d locals exceed bounds", ErrInvalidRuntimeConfig, i)
			}
		}
		if bp >= len(body) {
			return 0, fmt.Errorf("%w: empty expression in code body %d", ErrInvalidRuntimeConfig, i)
		}
	}
	if pos != len(payload) {
		return 0, fmt.Errorf("%w: trailing bytes in code section", ErrInvalidRuntimeConfig)
	}
	return funcCount, nil
}

func validateWasmDataSection(payload []byte) (bool, uint32, error) {
	pos := 0
	count, err := readBoundedVecCount(payload, &pos, MaxWasmDataSegments, 2, "data")
	if err != nil {
		return true, 0, err
	}
	hasPassive := false
	for i := uint32(0); i < count; i++ {
		mode, mok := readULEB32(payload, &pos)
		if !mok || mode > 2 {
			return true, 0, fmt.Errorf("%w: invalid data segment mode %d", ErrInvalidRuntimeConfig, mode)
		}
		switch mode {
		case 0:
			if !skipWasmConstInitExpr(payload, &pos) {
				return true, 0, fmt.Errorf("%w: malformed active data offset expr", ErrInvalidRuntimeConfig)
			}
		case 1:
			hasPassive = true
		case 2:
			memIdx, ok := readULEB32(payload, &pos)
			if !ok || memIdx != 0 || !skipWasmConstInitExpr(payload, &pos) {
				return true, 0, fmt.Errorf("%w: malformed memory-indexed data segment", ErrInvalidRuntimeConfig)
			}
		}
		vecLen, lok := readULEB32(payload, &pos)
		if !lok || uint64(vecLen) > uint64(len(payload)-pos) {
			return true, 0, fmt.Errorf("%w: truncated data segment payload", ErrInvalidRuntimeConfig)
		}
		pos += int(vecLen)
	}
	if pos != len(payload) {
		return true, 0, fmt.Errorf("%w: trailing bytes in data section", ErrInvalidRuntimeConfig)
	}
	return hasPassive, count, nil
}

func validateWasmImportSection(payload []byte, allowHostBlindEgress bool) error {
	pos := 0
	count, err := readBoundedVecCount(payload, &pos, MaxWasmImports, 4, "import")
	if err != nil {
		return fmt.Errorf("%w: %s", ErrUnapprovedWASIImport, err.Error())
	}
	for i := uint32(0); i < count; i++ {
		modName, mok := readWasmName(payload, &pos)
		fnName, fok := readWasmName(payload, &pos)
		if !mok || !fok || pos >= len(payload) {
			return fmt.Errorf("%w: malformed import entry", ErrUnapprovedWASIImport)
		}
		kind := payload[pos]
		pos++
		switch kind {
		case 0x00: // func type index (signature validated against CompiledModule.ImportedFunctions())
			if _, ok := readULEB32(payload, &pos); !ok {
				return fmt.Errorf("%w: malformed function import %s.%s", ErrUnapprovedWASIImport, modName, fnName)
			}
			if !(allowHostBlindEgress && modName == HostModuleName && fnName == HostFuncBlindHTTPEgress) {
				return fmt.Errorf("%w: forbidden import %s.%s", ErrUnapprovedWASIImport, modName, fnName)
			}
		case 0x01:
			return fmt.Errorf("%w: forbidden imported table %s.%s", ErrUnapprovedWASIImport, modName, fnName)
		case 0x02:
			return fmt.Errorf("%w: forbidden imported memory %s.%s", ErrUnapprovedWASIImport, modName, fnName)
		case 0x03:
			return fmt.Errorf("%w: forbidden imported global %s.%s", ErrUnapprovedWASIImport, modName, fnName)
		default:
			return fmt.Errorf("%w: forbidden import %s.%s (kind 0x%02x)", ErrUnapprovedWASIImport, modName, fnName, kind)
		}
	}
	if pos != len(payload) {
		return fmt.Errorf("%w: trailing bytes in import section", ErrUnapprovedWASIImport)
	}
	return nil
}

func skipWasmName(b []byte, pos *int) bool {
	n, ok := readULEB32(b, pos)
	if !ok || uint64(n) > uint64(len(b)-*pos) {
		return false
	}
	*pos += int(n)
	return true
}

func readWasmName(b []byte, pos *int) (string, bool) {
	n, ok := readULEB32(b, pos)
	if !ok || uint64(n) > uint64(len(b)-*pos) {
		return "", false
	}
	s := string(b[*pos : *pos+int(n)])
	*pos += int(n)
	return s, true
}

func validateWasmGlobalSection(payload []byte) (bool, error) {
	pos := 0
	count, err := readBoundedVecCount(payload, &pos, MaxWasmGlobals, 3, "global")
	if err != nil {
		return true, err
	}
	hasMutable := false
	for i := uint32(0); i < count; i++ {
		if pos+2 > len(payload) {
			return true, fmt.Errorf("%w: truncated global entry %d", ErrInvalidRuntimeConfig, i)
		}
		valType := payload[pos]
		mut := payload[pos+1]
		pos += 2
		if !isSimpleWasmValType(valType) {
			return true, fmt.Errorf("%w: disallowed global valtype 0x%02x", ErrInvalidRuntimeConfig, valType)
		}
		if mut == 0x01 {
			hasMutable = true
		} else if mut != 0x00 {
			return true, fmt.Errorf("%w: invalid global mutability 0x%02x", ErrInvalidRuntimeConfig, mut)
		}
		if !skipWasmConstInitExpr(payload, &pos) {
			return true, fmt.Errorf("%w: malformed global init expr %d", ErrInvalidRuntimeConfig, i)
		}
	}
	if pos != len(payload) {
		return true, fmt.Errorf("%w: trailing bytes in global section", ErrInvalidRuntimeConfig)
	}
	return hasMutable, nil
}

func isSimpleWasmValType(b byte) bool {
	switch b {
	case 0x7f, 0x7e, 0x7d, 0x7c, 0x7b:
		return true
	default:
		return false
	}
}

func skipWasmConstInitExpr(b []byte, pos *int) bool {
	for *pos < len(b) {
		op := b[*pos]
		*pos++
		switch op {
		case 0x0b: // end
			return true
		case 0x41, 0x42: // i32.const / i64.const (sLEB128)
			if !skipLEB(b, pos, 10) {
				return false
			}
		case 0x43: // f32.const
			if *pos+4 > len(b) {
				return false
			}
			*pos += 4
		case 0x44: // f64.const
			if *pos+8 > len(b) {
				return false
			}
			*pos += 8
		case 0x23: // global.get (uLEB32)
			if _, ok := readULEB32(b, pos); !ok {
				return false
			}
		case 0xfd: // v128.const (0xfd 0x0c <16 bytes>)
			if *pos >= len(b) || b[*pos] != 0x0c || *pos+17 > len(b) {
				return false
			}
			*pos += 17
		default:
			return false
		}
	}
	return false
}

func readULEB32(b []byte, pos *int) (uint32, bool) {
	var val uint32
	var shift uint
	for i := 0; i < 5; i++ {
		if *pos >= len(b) {
			return 0, false
		}
		by := b[*pos]
		*pos++
		if i == 4 && by > 0x0f {
			return 0, false
		}
		val |= uint32(by&0x7f) << shift
		if by&0x80 == 0 {
			return val, true
		}
		shift += 7
	}
	return 0, false
}

func skipLEB(b []byte, pos *int, maxBytes int) bool {
	for i := 0; i < maxBytes; i++ {
		if *pos >= len(b) {
			return false
		}
		by := b[*pos]
		*pos++
		if by&0x80 == 0 {
			return true
		}
	}
	return false
}

func cloneStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
