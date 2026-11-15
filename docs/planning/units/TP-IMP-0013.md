# `TP-IMP-0013` — Tier-2 `wazero` Wasm Sandbox Pool (`0` FS/Env/Socket, `16 MiB` Cap, `50 ms` Watchdog / `< 1ms p95` Chunk SLO & `FMEA-05` Quarantine)

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0013` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Status** | **`READY_FOR_REVIEW`** *(Implemented & verified locally `2026-11-15`; pending independent `REQUIRES_STRONG_REVIEW` sign-off and delegated Owner **Carbon** acceptance under [`DEC-022`](../../governance/decisions.md))* |
| **Size** | `L` (`go.mod`, `internal/extension/wasm/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Untrusted guest execution boundary `TB-4` — must never crash or stall the Go Core process)* |
| **Technical Owner** | `Principal Runtime & Sandbox Security Engineer` |

---

## 1. Intent
Implement the Tier-2 extension sandbox pool satisfying the unified 10-method `extension/port.ExtensionRuntime` contract (`TP-IMP-0002` / `TP-IMP-0010`) using pure-Go `github.com/tetratelabs/wazero` (`CGO_ENABLED=0`) under a **Default-Deny `Zero-Ambient-Authority Sandbox`**: `0` filesystem (`FS`) access, `0` environment variable (`Env`) access, `0` unmediated wall-clock access, `0` raw network socket access (all outbound HTTP calls delegate exclusively through `BlindHostCredentialBroker`), a hard linear-memory ceiling of `<= 16 MiB` (`256` Wasm pages) per instance, immediate CPU/deadline interruption on runaway loops, and automatic quarantine (`QUARANTINED`) on repeated guest faults per `FMEA-05`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (add pure-Go dependency `github.com/tetratelabs/wazero`).
  - `internal/extension/wasm/runtime.go` (initialize `wazero.Runtime` with `WithCloseOnContextDone(true)` and `WithMemoryLimitPages(256)` = `16 MiB`, validate imports/tables/globals, enforce `SM-01` host/TLS/CIDR/port/secret-slot defaults, expose `SetWasmEnabled`/`IsWasmEnabled` + `EnabledFunc` backing `runtime.wasm.enabled`, pre-compiling `CompiledModule` once upon package activation).
  - `internal/extension/wasm/pool.go` (bounded-concurrency instance pool that serializes `abort()` vs `disarm()`, restores linear memory or re-instantiates slots containing mutable globals/tables/`tanpopo_alloc`, and immediately destroys and replaces any instance hitting a `trap`, `timeout`, or page-growth high-water mark to eliminate cross-request state bleed).
  - `internal/extension/wasm/abi_host.go` (implement all 10 methods of `extension/port.ExtensionRuntime` locked in `TP-IMP-0002` / `TP-IMP-0010` & `POC-01`: `BuildValidationProbe`, `VerifyValidationResult`, `TransformRequest`, `TransformResponse`, `TransformStreamChunk`, `NormalizeError`, `ExtractUsage`, `InspectNativeRequest`, `EvaluateGuardrail`, `CancelExecution` + blind HTTP egress host callback via `BlindHostCredentialBroker`).
  - `internal/extension/wasm/fault_guard.go` (sliding-window fault tracker per `FMEA-05`: if an `extension_installation_id` triggers **`> 20` `trap/timeout/OOM` faults within a `60-second` sliding window**, automatically invoke `QuarantineFunc` from `TP-IMP-0011` to move `SM-01` to `QUARANTINED`, emit `ALT-PLUGIN-TRAP` using `data_plane_guard`, and isolate healthy sibling Adapters).
- **Out-of-Scope:** `Tier-3 Isolated Sidecar` engine over `ConnectRPC` UDS/mTLS (owned by `TP-IMP-0042` in `v0.3`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0302`, `TP-FR-0303`, `TP-NFR-0101`, `TP-NFR-0202`, `TP-NFR-0401`, `SLO-02`, `TP-BR-0401`, `TP-BR-0404`, `CT-17`, `FZ-05`, `FMEA-05`.
- **Dependencies:** `TP-IMP-0004`, `TP-IMP-0010`, `TP-IMP-0011`.
- **Design Refs:** [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`spike-results.md`](../../architecture/spike-results.md) (`SPIKE-02` / `POC-01`), [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Section 2, [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-05`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-17`, `FZ-05`).

## 4. API, Wire & Data Contracts
- **Hard Resource, Pool Eviction & Execution-Time Caps (`FMEA-05` & `SPIKE-02` Empirical Invariants):**
  - Maximum linear memory per Wasm instance: **`16 MiB`** (`256 * 64 KiB`); large multimodal payloads bypass Wasm linear memory via Host `blob_ref` handles (`CR-05`).
  - **Locked `POC-01` Binary ABI:** Linear-Memory Slice ABI `(ptr: i32, len: i32) -> packed u64 ((outPtr << 32) | outLen)` with mandatory zeroing (`clear(buf)`) of both input and output slices before releasing an un-faulted module back to the pool.
  - **Mandatory Tainted-Module Eviction (`SPIKE-02` Invariant #1):** Because `wazero.NewRuntimeConfig().WithCloseOnContextDone(true)` permanently closes `api.Module` upon context deadline expiry or trap, `pool.go` **MUST** close any module whose `Call()` returns a non-nil error (`_ = mod.Close(context.Background())`) and instantiate a fresh module from pre-compiled `CompiledModule` before returning the slot to the pool.
  - **Queue Admission, In-Flight Cancellation & Zero-Alloc Pool Watchdog (`SPIKE-02` Invariants #2 & #3):** Acquire a pool slot (`<-pool.ch`) under the caller's admission context first; once acquired, register `context.AfterFunc(parentCtx, ...)` so caller cancellation (e.g., HTTP disconnect / `CancelExecution`) **immediately aborts an in-flight Wasm call** via `mod.CloseWithExitCode(context.Background(), 2)` (`0` goroutines spawned on normal calls), arm an `atomic.Int64` per-slot deadline scanned by **1 pool-wide watchdog goroutine** (`CloseWithExitCode(..., 1)` on timeout), and invoke `Call(context.Background(), ...)`. Keep **SLO Latency Targets** (`< 5 ms p95` request/response, `< 1 ms p95` chunk — `TARGET / UNVERIFIED` per `COND-PO-01`; isolated `wazero` microbenchmarks are **not** proof of full Gateway SLOs) distinct from **Hard Destructive Watchdog Deadlines** (`>= 50–200 ms`, above OS `15.6 ms` scheduler preemption quantum).
  - Every Wasm `trap`/`timeout`/`OOM` fault is translated into canonical error **`502 extension_execution_fault`** (and in-flight caller cancellation returns `context.Canceled`) — strictly never panicking the Tanpopo Core host process.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`Concurrent Transform & Zero Secret Exposure`):** Given valid fixture `echo_transform.wasm` from `internal/testkit/wasmfixtures` (`TP-IMP-0006` — zero host `TinyGo` installation required), When running 100 concurrent request transforms + 50 SSE frames per stream across the `wazero` pool, Then `0` data races occur (`-race`), all frames match expected bytes in order, and `0` canary secret bytes ever appear in Wasm linear memory.
- **`AC-2` (`In-Flight Wasm Call Cancellation`):** Given a pooled Wasm instance actively executing a long/infinite loop (`infinite_loop.wasm`) under a `5s` watchdog, When the caller cancels `parentCtx` after `10 ms` (simulating mid-request client disconnect or `CancelExecution`), Then the running Wasm call aborts **immediately (`< 100 ms`, without waiting for the `5s` watchdog)**, returns `context.Canceled`, closes and replaces the tainted `api.Module` from `CompiledModule`, and restores the pool slot cleanly.
- **`AC-3` (`Tainted-Module Replacement, Full Pool Recovery & Bounded Resource Consumption`):** Given a bounded pool of `N` Wasm instances subjected to `>= 3N` consecutive mixed faults (in-flight cancellations, watchdog timeouts, guest `unreachable` traps, and `64 MiB` `memory.grow` denials), When the fault burst completes, Then `100%` of tainted modules have been evicted and replaced (`len(pool) == N`, `0` lost/dead slots), `2N` subsequent healthy calls succeed `100%`, per-instance linear memory never exceeds **`16 MiB` (`256` pages)**, and `goleak` / `runtime.NumGoroutine()` confirms **`0` leaked goroutines or unclosed modules** upon `pool.Close()`. *(Note per `COND-PO-01`: unit/microbenchmark numbers in `TP-IMP-0013` do **not** claim full Gateway `SLO-02`/`SLO-03` compliance, which remains gated on `TP-IMP-0043`.)*
- **`NEG-1` (`CT-17` & `FMEA-05` Watchdog Timeout / Memory Bomb / Quarantine Threshold):**
  1. Loading `infinite_loop.wasm` with an un-cancelled `parentCtx` → interrupted cleanly by the pool watchdog (`<= 200 ms`), tainted `api.Module` evicted and replaced from `CompiledModule` so the very next healthy request on that pool slot succeeds `100%`, returning `502 extension_execution_fault` and releasing CPU immediately.
  2. Loading `oom_64mib.wasm` (`memory.grow` requesting `64 MiB`) → denied at the `16 MiB` cap, instance cleanly recycled, Core host process unaffected.
  3. Triggering `unreachable_trap.wasm` 21 times within `60 seconds` (`FakeClock`) → upon crossing `> 20`, the installation automatically transitions to `QUARANTINED` (`FMEA-05`), while healthy Adapters continue serving `100%` of traffic uninterrupted.
- **`NEG-2` (`Zero Ambient Authority`):** Loading `unapproved_wasi_import.wasm` attempting to import filesystem (`path_open`), environment (`environ_get`), or socket (`sock_open`) syscalls → rejected at module instantiation time (`unapproved_wasi_import`).
- **`NEG-3` (`Cross-Request Memory Bleed`):** After Request A from `Org 1` completes or traps mid-call → Request B from `Org 2` borrowing from the pool reads `0` residual bytes from Request A.

## 6. Test Commands & Required Evidence
- Pure-Go verification (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/extension/wasm/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/extension/wasm/...`
- Data-race (`-race`) & Wasm pointer-boundary fuzzing (`FZ-05`):
  - `go test -v -race ./internal/extension/wasm/...`
  - `go test -fuzz=FuzzWasmABIBoundary -fuzztime=30s ./internal/extension/wasm/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Never instantiate `wasi_snapshot_preview1` with mounted Host directories or environment variables; validate every guest-returned `(offset, length)` pointer pair against integer-overflow bounds `uint64(offset)+uint64(length) <= uint64(mem.Size())` before slicing linear memory.
- **Rollback:** Emergency config toggle `runtime.wasm.enabled = false` disables Tier-2 Wasm execution while keeping Tier-1 Declarative Adapters online.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (`runtime.proto` & `blob_ref` lock), `TP-IMP-0004` (`POC-01` Wasm ABI freeze), `TP-IMP-0006` (`wasmfixtures`), `TP-IMP-0010`, and `TP-IMP-0011` are complete.
- **DoD:** `100%` green across adversarial suites `CT-17`, `FMEA-05`, and `FZ-05` under both `CGO_ENABLED=0` and `-race`; passes `REQUIRES_STRONG_REVIEW`.

---

## 9. Verification Evidence (`2026-11-15`)

- **Delivered Files (`CGO_ENABLED=0` Pure-Go `wazero v1.11.0`):**
  - [`go.mod`](../../../go.mod), [`go.sum`](../../../go.sum)
  - [`internal/extension/wasm/runtime.go`](../../../internal/extension/wasm/runtime.go) (Zero-Ambient-Authority `wazero.Runtime` with `CoreFeatureReferenceTypes` disabled, `MaxWasmBinaryBytes` `64 MiB` & `MaxWasmCodeBytes` `16 MiB` caps, `256`-page / `16 MiB` default memory cap & non-zero `effectiveMaxPages` enforcement, `ErrUnapprovedWASIImport` + fail-closed pre-compile Wasm binary validator across Sections `0..12` enforcing section ordering/duplication/func-code/datacount counts, bounded vector counts & 5th-byte `readULEB32` `> 0x0f` rejection, Sec 10 compressed locals bounds [`MaxWasmFunctionLocals=65536`, `MaxWasmTotalLocals=1048576`], Sec 4 bounded tables [`MaxWasmTables=1`, `MaxWasmTableElements=65536`] & non-`externref` enforcement, and passive `data.drop`/`memory.init` state bleed rejection; `tanpopo_alloc` `(i32)->(i32)` signature validation, `ErrWasmRuntimeClosed`, `runtime.wasm.enabled` kill-switch, default-deny `SM-01` host/TLS/egress/strict-secret-slot validation)
  - [`internal/extension/wasm/pool.go`](../../../internal/extension/wasm/pool.go) (bounded slot pool, admission-before-deadline queueing, single pool-wide watchdog goroutine, `inflightCall.disarm()` + `p.closed` barrier, `context.AfterFunc` + `CancelRequest` immediate `callCtx` + `CloseWithExitCode`, full memory restoration / `clear()`, tainted-module eviction & replacement, caller-size and `maxLinearMemBytes` rejections separated from guest `FaultKindOOM`)
  - [`internal/extension/wasm/abi_host.go`](../../../internal/extension/wasm/abi_host.go) (10-method `port.ExtensionRuntime` bridge, unified `nil`-safe `preflightGate` with `closed` check across all RPC entrypoints/slice/envelope/probe, `tanpopo_host_v1.blind_http_egress` -> `BlindHostCredentialBroker` with canonical `(host, port)` + CIDR prefix-aware `intersectEgressPolicies` narrowing, `64 KiB` stream state cap)
  - [`internal/extension/wasm/fault_guard.go`](../../../internal/extension/wasm/fault_guard.go) (`FMEA-05` `> 20` trap/timeout/OOM faults in `60s` sliding window -> pool warm eviction, atomic `QuarantineFunc` `QUARANTINED`, `ALT-PLUGIN-TRAP` `data_plane_guard` audit/alert + `OnError` callback)
  - [`internal/extension/wasm/wasm_test.go`](../../../internal/extension/wasm/wasm_test.go) (`AC-1..3`, `NEG-1..3`, `CT-16`, `CT-17`, 10-RPC & kill-switch coverage, cancel/watchdog/evict race stress, mutable-global/table/passive-data/import/reference-types hardening, pre-compile Sec `0..12` / Sec 10 compressed locals / `MaxWasmCodeBytes` / `readULEB32` / `max=0` memory DoS tests, non-quarantine caller oversize & small-`maxLinearMemBytes` checks, `nil` `*Runtime` safety, physical SQLite `audit_events` `CHECK` test, `FuzzWasmABIBoundary` [`FZ-05`], microbenchmarks)
- **Windows Primary Verification (`CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1`):**
  - `go test -count=1 -cover ./internal/extension/wasm/...` -> **`PASS` (`0.926s`, `85.4%` statement coverage >= `85.0%` `CONSTRAINTS.md` threshold)**
  - `go test -count=1 ./...` -> **`PASS` (all packages green; `internal/extension/wasm` `3.187s`)**
  - `go vet ./...` -> **`PASS` (exit `0`)**
  - `go run ./scripts/ci/check.go --all` -> **`PASS: all selected Tanpopo quality gates succeeded`**
  - `tools/spikes` (`go test -count=1 -run "TestContract|OpenAPI|SQLite|PhysicalSQLite" ./...`) -> **`PASS` (`0.319s`)**
  - `go test -run=^$ -fuzz=^FuzzWasmABIBoundary$ -fuzztime=30s ./internal/extension/wasm/...` (`FZ-05` on finalized Go runtime code) -> **`PASS` (`30.482s`, `47,580` execs, `92` interesting inputs from `74` baseline, `0` crashes/OOB reads)**
  - `go test -run=^$ -bench=^BenchmarkWasm -benchmem ./internal/extension/wasm/...` (`12th Gen Intel(R) Core(TM) i5-12450H`):
    - `BenchmarkWasmTransformRequest-12`: **`7572 ns/op`**, `1808 B/op`, `27 allocs/op`
    - `BenchmarkWasmTransformStreamChunk-12`: **`5659 ns/op`**, `744 B/op`, `17 allocs/op`
- **Linux VPS Race Verification (`ip-142-248-83-44`, SHA-256 Patch `b0ec3cfdc3c2055df2893c52cb934110bbe2d7164dcbbf4e47d14d2cc6184bab`):**
  - `CGO_ENABLED=1 TANPOPO_TEST_HERMETIC=1 go test -v -race -count=1 ./internal/extension/wasm/...` -> **`PASS` (`3.038s`, `0` races)**
  - `CGO_ENABLED=1 TANPOPO_TEST_HERMETIC=1 go test -race -count=1 ./...` -> **`PASS` (`4.987s` in `internal/extension/wasm`; all packages green)**
  - `go vet ./...` -> **`PASS`**
  - `go run ./scripts/ci/check.go --all` -> **`PASS: all selected Tanpopo quality gates succeeded`**
- **`UNVERIFIED` Scope Boundaries (`COND-PO-01`):**
  - Isolated `wazero` microbenchmarks do **not** claim full Data Plane Gateway `SLO-02`/`SLO-03` (`1,000` SSE streams on `2 vCPU / 2 GiB`, owned by `TP-IMP-0043`).
  - Downstream SDK/CLI (`TP-IMP-0014`), HTTP `/v1/chat/completions` vertical slice (`TP-IMP-0015`), `Tier-3 Sidecar` (`TP-IMP-0042`), and `PostgreSQL 16` (`TP-IMP-0039`) remain unverified within standalone `TP-IMP-0013`.
