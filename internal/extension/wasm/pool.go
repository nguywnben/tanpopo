// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package wasm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/nguywnben/tanpopo/internal/extension/port"
)

const (
	exitCodeWatchdogTimeout uint32 = 1
	exitCodeCallerCanceled  uint32 = 2
	exitCodePoolClosed      uint32 = 3

	abortReasonNone           int32 = 0
	abortReasonWatchdog       int32 = 1
	abortReasonCallerCanceled int32 = 2
	abortReasonPoolClosed     int32 = 3
	abortReasonCompleted      int32 = 4
)

type wasmPoolConfig struct {
	size                   int
	inputOffset            uint32
	maxOutputBytes         uint32
	maxLinearMemBytes      uint64
	defaultTimeout         time.Duration
	watchdogInterval       time.Duration
	initTimeout            time.Duration
	reinstantiateAfterCall bool
}

type slotState struct {
	idx               int
	epoch             uint64
	mod               api.Module
	mem               api.Memory
	exports           map[string]api.Function
	allocFn           api.Function
	initialMem        []byte
	initialMemNonZero bool
	initialMemSize    uint32
}

type inflightCall struct {
	mu         sync.Mutex
	disarmed   bool
	mod        api.Module
	callCancel context.CancelFunc
	deadlineNs atomic.Int64
	reason     atomic.Int32
}

func (c *inflightCall) abort(reason int32, exitCode uint32) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disarmed || !c.reason.CompareAndSwap(abortReasonNone, reason) {
		return false
	}
	c.deadlineNs.Store(0)
	if c.callCancel != nil {
		c.callCancel()
	}
	_ = c.mod.CloseWithExitCode(context.Background(), exitCode)
	return true
}

func (c *inflightCall) disarm() (int32, bool) {
	if c == nil {
		return abortReasonNone, true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadlineNs.Store(0)
	if c.disarmed {
		return abortReasonNone, true
	}
	if c.reason.CompareAndSwap(abortReasonNone, abortReasonCompleted) {
		c.disarmed = true
		return abortReasonNone, true
	}
	return c.reason.Load(), false
}

type wasmPool struct {
	rt                     wazero.Runtime
	compiled               wazero.CompiledModule
	size                   int
	inputOffset            uint32
	maxOutputBytes         uint32
	maxLinearMemBytes      uint64
	defaultTimeout         time.Duration
	watchdogInterval       time.Duration
	initTimeout            time.Duration
	reinstantiateAfterCall bool

	slots      chan *slotState
	active     []atomic.Pointer[inflightCall]
	evictEpoch atomic.Uint64

	callsMu    sync.Mutex
	callsByReq map[string][]*inflightCall

	replacedCount atomic.Int64

	closeMu    sync.RWMutex
	closed     atomic.Bool
	closeCh    chan struct{}
	closeOnce  sync.Once
	inFlightWG sync.WaitGroup
	watchdogWG sync.WaitGroup
}

func newWasmPool(ctx context.Context, rt wazero.Runtime, compiled wazero.CompiledModule, cfg wasmPoolConfig) (*wasmPool, error) {
	p := &wasmPool{
		rt:                     rt,
		compiled:               compiled,
		size:                   cfg.size,
		inputOffset:            cfg.inputOffset,
		maxOutputBytes:         cfg.maxOutputBytes,
		maxLinearMemBytes:      cfg.maxLinearMemBytes,
		defaultTimeout:         cfg.defaultTimeout,
		watchdogInterval:       cfg.watchdogInterval,
		initTimeout:            cfg.initTimeout,
		reinstantiateAfterCall: cfg.reinstantiateAfterCall,
		slots:                  make(chan *slotState, cfg.size),
		active:                 make([]atomic.Pointer[inflightCall], cfg.size),
		callsByReq:             make(map[string][]*inflightCall),
		closeCh:                make(chan struct{}),
	}

	cleanupOnFail := func() {
		for {
			select {
			case s := <-p.slots:
				if s != nil && s.mod != nil {
					_ = s.mod.Close(context.Background())
				}
			default:
				return
			}
		}
	}

	for i := 0; i < cfg.size; i++ {
		slot, err := p.instantiateSlot(ctx, i)
		if err != nil {
			cleanupOnFail()
			return nil, err
		}
		p.slots <- slot
	}

	p.watchdogWG.Add(1)
	go p.watchdogLoop()
	return p, nil
}

func (p *wasmPool) instantiateSlot(parentCtx context.Context, idx int) (*slotState, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	initCtx, cancel := context.WithTimeout(parentCtx, p.initTimeout)
	defer cancel()

	epoch := p.evictEpoch.Load()
	mod, err := p.rt.InstantiateModule(initCtx, p.compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "import") || strings.Contains(msg, "unknown") {
			return nil, fmt.Errorf("%w: %v", ErrUnapprovedWASIImport, err)
		}
		return nil, &ExecutionFaultError{Kind: FaultKindTrap, Reason: "instantiate wasm module failed", Cause: err}
	}

	mem := mod.Memory()
	if mem == nil {
		_ = mod.Close(context.Background())
		return nil, fmt.Errorf("%w: module missing exported linear memory", ErrInvalidRuntimeConfig)
	}
	memSize := mem.Size()
	if memSize < p.inputOffset {
		_ = mod.Close(context.Background())
		return nil, fmt.Errorf("%w: linear memory (%d bytes) smaller than input offset %d", ErrInvalidRuntimeConfig, memSize, p.inputOffset)
	}

	rawMem, ok := mem.Read(0, memSize)
	if !ok {
		_ = mod.Close(context.Background())
		return nil, &ExecutionFaultError{Kind: FaultKindABIViolation, Reason: "unable to snapshot initial linear memory"}
	}

	var initSnap []byte
	nonZero := bytesContainsNonZero(rawMem)
	if nonZero {
		initSnap = make([]byte, len(rawMem))
		copy(initSnap, rawMem)
	}

	exports := make(map[string]api.Function, len(p.compiled.ExportedFunctions()))
	for name, def := range p.compiled.ExportedFunctions() {
		if fn := mod.ExportedFunction(name); fn != nil && isValidSliceABISig(def) {
			exports[name] = fn
		}
	}

	var allocFn api.Function
	if fnDef, ok := p.compiled.ExportedFunctions()["tanpopo_alloc"]; ok {
		pr := fnDef.ParamTypes()
		rr := fnDef.ResultTypes()
		if len(pr) == 1 && pr[0] == api.ValueTypeI32 && len(rr) == 1 && rr[0] == api.ValueTypeI32 {
			allocFn = mod.ExportedFunction("tanpopo_alloc")
		}
	}

	return &slotState{
		idx:               idx,
		epoch:             epoch,
		mod:               mod,
		mem:               mem,
		exports:           exports,
		allocFn:           allocFn,
		initialMem:        initSnap,
		initialMemNonZero: nonZero,
		initialMemSize:    memSize,
	}, nil
}

func bytesContainsNonZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}

func (p *wasmPool) watchdogLoop() {
	defer p.watchdogWG.Done()
	ticker := time.NewTicker(p.watchdogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.closeCh:
			return
		case now := <-ticker.C:
			nowNs := now.UnixNano()
			for i := range p.active {
				c := p.active[i].Load()
				if c == nil {
					continue
				}
				dl := c.deadlineNs.Load()
				if dl > 0 && nowNs >= dl {
					c.abort(abortReasonWatchdog, exitCodeWatchdogTimeout)
				}
			}
		}
	}
}

func (p *wasmPool) enterPool() bool {
	p.closeMu.RLock()
	defer p.closeMu.RUnlock()
	if p.closed.Load() {
		return false
	}
	p.inFlightWG.Add(1)
	return true
}

func (p *wasmPool) exec(ctx context.Context, reqID string, fnNames []string, input []byte, timeout time.Duration, expectUTF8 bool) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !p.enterPool() {
		return nil, &ExecutionFaultError{Kind: FaultKindClosed, Reason: "wasm pool closed", Cause: ErrWasmRuntimeClosed}
	}
	defer p.inFlightWG.Done()

	if uint64(len(input)) > uint64(MaxGuestPayloadBytes) {
		return nil, fmt.Errorf("%w: guest input %d bytes exceeds max %d", port.ErrExecutionFault, len(input), MaxGuestPayloadBytes)
	}
	if p.maxLinearMemBytes > 0 && uint64(len(input)) > p.maxLinearMemBytes {
		return nil, fmt.Errorf("%w: guest input %d bytes exceeds module max linear memory %d", port.ErrExecutionFault, len(input), p.maxLinearMemBytes)
	}

	// Stage 1 (SPIKE-02 Invariant #2): Acquire slot under admission context BEFORE arming CPU watchdog.
	var slot *slotState
	select {
	case <-p.closeCh:
		return nil, &ExecutionFaultError{Kind: FaultKindClosed, Reason: "wasm pool closed", Cause: ErrWasmRuntimeClosed}
	case <-ctx.Done():
		return nil, ctx.Err()
	case slot = <-p.slots:
	}

	if slot == nil || slot.mod == nil || slot.epoch != p.evictEpoch.Load() {
		if slot != nil && slot.mod != nil {
			_ = slot.mod.Close(context.Background())
		}
		if p.closed.Load() {
			return nil, &ExecutionFaultError{Kind: FaultKindClosed, Reason: "wasm pool closed", Cause: ErrWasmRuntimeClosed}
		}
		idx := 0
		if slot != nil {
			idx = slot.idx
		}
		fresh, repErr := p.instantiateSlot(context.Background(), idx)
		if repErr != nil {
			p.slots <- &slotState{idx: idx}
			return nil, repErr
		}
		p.replacedCount.Add(1)
		slot = fresh
	}

	tainted := false
	var ic *inflightCall
	defer func() {
		if ic != nil {
			if _, ok := ic.disarm(); !ok {
				tainted = true
			}
		}
		p.active[slot.idx].Store(nil)
		if p.closed.Load() {
			if slot.mod != nil {
				_ = slot.mod.Close(context.Background())
			}
			return
		}
		if tainted || p.reinstantiateAfterCall || slot.epoch != p.evictEpoch.Load() {
			if slot.mod != nil {
				_ = slot.mod.Close(context.Background())
			}
			fresh, repErr := p.instantiateSlot(context.Background(), slot.idx)
			if repErr != nil {
				p.slots <- &slotState{idx: slot.idx}
				return
			}
			p.replacedCount.Add(1)
			p.slots <- fresh
			return
		}
		p.slots <- slot
	}()

	var fn api.Function
	for _, candidate := range fnNames {
		if found, ok := slot.exports[candidate]; ok && found != nil {
			fn = found
			break
		}
	}
	if fn == nil {
		return nil, &ExecutionFaultError{
			Kind:   FaultKindABIViolation,
			Reason: fmt.Sprintf("missing exported slice function (tried %v)", fnNames),
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.closed.Load() {
		return nil, &ExecutionFaultError{Kind: FaultKindClosed, Reason: "wasm pool closed", Cause: ErrWasmRuntimeClosed}
	}

	if timeout <= 0 {
		timeout = p.defaultTimeout
	}

	inPtr := p.inputOffset
	if slot.allocFn == nil {
		if uint64(inPtr)+uint64(len(input)) > uint64(slot.mem.Size()) {
			return nil, fmt.Errorf("%w: input (%d bytes at %d) exceeds linear memory size (%d)", port.ErrExecutionFault, len(input), inPtr, slot.mem.Size())
		}
		if len(input) > 0 && !slot.mem.Write(inPtr, input) {
			tainted = true
			return nil, &ExecutionFaultError{
				Kind:   FaultKindABIViolation,
				Reason: "failed writing input bytes into guest linear memory",
			}
		}
	}

	// Forward trusted EgressScope into wazero Call context without attaching parentCtx cancellation,
	// avoiding per-chunk watcher goroutines; context.AfterFunc triggers immediate abort on cancellation.
	baseCallCtx, cancelCall := context.WithCancel(context.Background())
	defer cancelCall()
	callCtx := baseCallCtx
	if scope, ok := egressScopeFromContext(ctx); ok {
		callCtx = WithEgressScope(callCtx, scope)
	}

	ic = &inflightCall{mod: slot.mod, callCancel: cancelCall}
	ic.deadlineNs.Store(time.Now().Add(timeout).UnixNano())
	p.active[slot.idx].Store(ic)
	reqID = strings.TrimSpace(reqID)
	if reqID != "" {
		p.registerInflight(reqID, ic)
		defer p.unregisterInflight(reqID, ic)
	}

	stopCancel := context.AfterFunc(ctx, func() {
		ic.abort(abortReasonCallerCanceled, exitCodeCallerCanceled)
	})
	defer stopCancel()

	// Barrier check paired with Close()'s closed.Store(true) before scanning p.active.
	if p.closed.Load() {
		tainted = true
		ic.abort(abortReasonPoolClosed, exitCodePoolClosed)
		return nil, &ExecutionFaultError{Kind: FaultKindClosed, Reason: "wasm runtime closed during execution", Cause: ErrWasmRuntimeClosed}
	}

	if slot.allocFn != nil && len(input) > 0 {
		allocRes, allocErr := slot.allocFn.Call(callCtx, uint64(uint32(len(input))))
		if allocErr != nil {
			tainted = true
			if reason, ok := ic.disarm(); !ok {
				return nil, p.classifyAbortReason(ctx, reason)
			}
			return nil, p.classifyExecutionError(allocErr)
		}
		ptr32 := uint32(allocRes[0])
		if int32(ptr32) < 0 || uint64(ptr32)+uint64(len(input)) > uint64(slot.mem.Size()) {
			tainted = true
			return nil, &ExecutionFaultError{
				Kind:   FaultKindOOM,
				Reason: fmt.Sprintf("tanpopo_alloc returned invalid pointer %d for %d bytes", ptr32, len(input)),
			}
		}
		inPtr = ptr32
		if !slot.mem.Write(inPtr, input) {
			tainted = true
			return nil, &ExecutionFaultError{
				Kind:   FaultKindABIViolation,
				Reason: "failed writing input bytes into allocated guest linear memory",
			}
		}
	}

	results, callErr := fn.Call(callCtx, uint64(inPtr), uint64(uint32(len(input))))
	reason, disarmedCleanly := ic.disarm()
	if callErr != nil {
		tainted = true
		if !disarmedCleanly {
			return nil, p.classifyAbortReason(ctx, reason)
		}
		return nil, p.classifyExecutionError(callErr)
	}
	if !disarmedCleanly {
		tainted = true
		return nil, p.classifyAbortReason(ctx, reason)
	}

	outPtr, outLen, err := DecodePackedSlice(results[0], slot.mem.Size(), p.maxOutputBytes)
	if err != nil {
		tainted = true
		return nil, err
	}

	var outCopy []byte
	var outView []byte
	if outLen > 0 {
		var ok bool
		outView, ok = slot.mem.Read(outPtr, outLen)
		if !ok {
			tainted = true
			return nil, &ExecutionFaultError{
				Kind:   FaultKindABIViolation,
				Reason: "guest output slice out of linear memory bounds",
			}
		}
		if expectUTF8 && !utf8.Valid(outView) {
			tainted = true
			return nil, &ExecutionFaultError{
				Kind:   FaultKindABIViolation,
				Reason: "guest output slice contains invalid UTF-8",
			}
		}
		outCopy = make([]byte, len(outView))
		copy(outCopy, outView)
	}

	// If the guest grew linear memory pages or uses mutable globals/tables/allocators, taint
	// the instance so the pool recreates a clean instance rather than leaking state across requests.
	if p.reinstantiateAfterCall || slot.mem.Size() != slot.initialMemSize {
		tainted = true
		if len(input) > 0 {
			if inView, ok := slot.mem.Read(inPtr, uint32(len(input))); ok {
				clear(inView)
			}
		}
		clear(outView)
		return outCopy, nil
	}

	// Zero both input and output slices explicitly (POC-01), then restore full initial
	// linear memory so 0 Tenant A scratch bytes ever survive into Tenant B (NEG-3).
	if len(input) > 0 {
		if inView, ok := slot.mem.Read(inPtr, uint32(len(input))); ok {
			clear(inView)
		}
	}
	clear(outView)
	if fullView, ok := slot.mem.Read(0, slot.mem.Size()); ok {
		if !slot.initialMemNonZero {
			clear(fullView)
		} else if len(slot.initialMem) == len(fullView) {
			copy(fullView, slot.initialMem)
		} else {
			tainted = true
		}
	} else {
		tainted = true
	}

	return outCopy, nil
}

// DecodePackedSlice validates POC-01 packed `(uint64(outPtr) << 32) | uint64(outLen)`
// against negative values, output size caps, and uint64 overflow past linear memory bounds (FZ-05).
func DecodePackedSlice(packed uint64, memSize uint32, maxOutputBytes uint32) (uint32, uint32, error) {
	outPtr := uint32(packed >> 32)
	outLen := uint32(packed & 0xffffffff)

	if int32(outPtr) < 0 || int32(outLen) < 0 {
		return 0, 0, &ExecutionFaultError{
			Kind:   FaultKindABIViolation,
			Reason: fmt.Sprintf("negative packed abi pointer/length (%d, %d)", int32(outPtr), int32(outLen)),
		}
	}
	if maxOutputBytes > 0 && outLen > maxOutputBytes {
		return 0, 0, &ExecutionFaultError{
			Kind:   FaultKindABIViolation,
			Reason: fmt.Sprintf("guest output length %d exceeds max %d", outLen, maxOutputBytes),
		}
	}
	if uint64(outPtr)+uint64(outLen) > uint64(memSize) {
		return 0, 0, &ExecutionFaultError{
			Kind:   FaultKindABIViolation,
			Reason: fmt.Sprintf("guest output [%d..%d) overflows linear memory size %d", outPtr, uint64(outPtr)+uint64(outLen), memSize),
		}
	}
	return outPtr, outLen, nil
}

func (p *wasmPool) classifyExecutionError(err error) error {
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "out of bounds memory") || strings.Contains(lower, "memory") && strings.Contains(lower, "limit") {
		return &ExecutionFaultError{Kind: FaultKindOOM, Reason: "wasm guest memory access/growth violation", Cause: err}
	}
	return &ExecutionFaultError{Kind: FaultKindTrap, Reason: "wasm guest execution trapped", Cause: err}
}

func (p *wasmPool) classifyAbortReason(ctx context.Context, reason int32) error {
	switch reason {
	case abortReasonCallerCanceled:
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return context.Canceled
	case abortReasonWatchdog:
		return &ExecutionFaultError{Kind: FaultKindTimeout, Reason: "wasm pool watchdog timeout exceeded"}
	case abortReasonPoolClosed:
		return &ExecutionFaultError{Kind: FaultKindClosed, Reason: "wasm runtime closed during execution", Cause: ErrWasmRuntimeClosed}
	default:
		return &ExecutionFaultError{Kind: FaultKindTrap, Reason: "wasm execution aborted"}
	}
}

func (p *wasmPool) registerInflight(reqID string, ic *inflightCall) {
	p.callsMu.Lock()
	p.callsByReq[reqID] = append(p.callsByReq[reqID], ic)
	p.callsMu.Unlock()
}

func (p *wasmPool) unregisterInflight(reqID string, ic *inflightCall) {
	p.callsMu.Lock()
	list := p.callsByReq[reqID]
	for i, item := range list {
		if item == ic {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(p.callsByReq, reqID)
	} else {
		p.callsByReq[reqID] = list
	}
	p.callsMu.Unlock()
}

// CancelRequest immediately aborts all active Wasm calls matching reqID.
func (p *wasmPool) CancelRequest(reqID string) bool {
	reqID = strings.TrimSpace(reqID)
	if reqID == "" {
		return false
	}
	p.callsMu.Lock()
	list := append([]*inflightCall(nil), p.callsByReq[reqID]...)
	p.callsMu.Unlock()

	released := false
	for _, ic := range list {
		if ic.abort(abortReasonCallerCanceled, exitCodeCallerCanceled) {
			released = true
		}
	}
	return released
}

// EvictAllWarm bumps the pool epoch and replaces currently idle warm instances.
func (p *wasmPool) EvictAllWarm() {
	if p == nil || !p.enterPool() {
		return
	}
	defer p.inFlightWG.Done()

	newEpoch := p.evictEpoch.Add(1)
	for i := 0; i < p.size; i++ {
		select {
		case slot := <-p.slots:
			if slot == nil {
				slot = &slotState{}
			}
			if slot.epoch < newEpoch {
				if slot.mod != nil {
					_ = slot.mod.Close(context.Background())
				}
				slot = &slotState{idx: slot.idx}
				if !p.closed.Load() {
					if fresh, err := p.instantiateSlot(context.Background(), slot.idx); err == nil {
						slot = fresh
						p.replacedCount.Add(1)
					}
				}
			}
			if !p.closed.Load() {
				p.slots <- slot
			} else if slot.mod != nil {
				_ = slot.mod.Close(context.Background())
			}
		default:
			return
		}
	}
}

func (p *wasmPool) stats() PoolStats {
	return PoolStats{
		Capacity:      p.size,
		Available:     len(p.slots),
		ReplacedCount: p.replacedCount.Load(),
	}
}

func (p *wasmPool) inspectIdleMemory(match func([]byte) bool) error {
	if !p.enterPool() {
		return errors.New("wasm pool closed")
	}
	defer p.inFlightWG.Done()

	checked := make([]*slotState, 0, p.size)
	defer func() {
		for _, s := range checked {
			p.slots <- s
		}
	}()

	for i := 0; i < p.size; i++ {
		select {
		case s := <-p.slots:
			checked = append(checked, s)
			if s != nil && s.mem != nil {
				if raw, ok := s.mem.Read(0, s.mem.Size()); ok && match(raw) {
					return fmt.Errorf("unexpected secret or cross-tenant residue detected in slot %d memory", s.idx)
				}
			}
		default:
			return nil
		}
	}
	return nil
}

// Close aborts active calls, drains idle module instances, waits for the watchdog
// goroutine to exit, and ensures 0 goroutine or unclosed module leaks.
func (p *wasmPool) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	var closeErr error
	p.closeOnce.Do(func() {
		p.closeMu.Lock()
		p.closed.Store(true)
		close(p.closeCh)
		p.closeMu.Unlock()

		for i := range p.active {
			if c := p.active[i].Load(); c != nil {
				c.abort(abortReasonPoolClosed, exitCodePoolClosed)
			}
		}

		p.inFlightWG.Wait()
		p.watchdogWG.Wait()

		var errs []error
		for {
			select {
			case s := <-p.slots:
				if s != nil && s.mod != nil {
					if err := s.mod.Close(ctx); err != nil {
						errs = append(errs, err)
					}
				}
			default:
				closeErr = errors.Join(errs...)
				return
			}
		}
	})
	return closeErr
}
