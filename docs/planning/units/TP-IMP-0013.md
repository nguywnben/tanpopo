# `TP-IMP-0013` — Tier-2 `wazero` Wasm Sandbox Pool (`0` FS/Env/Socket, `16 MiB` Cap, `<= 5ms/chunk` CPU Deadline & `FMEA-05` Quarantine)

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0013` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0004`, `TP-IMP-0010`, `TP-IMP-0011` — `GAP-01, 02` + `POC-01`)* |
| **Size** | `L` (`go.mod`, `internal/extension/wasm/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Untrusted guest execution boundary `TB-4` — must never crash or stall the Go Core process)* |
| **Technical Owner** | `Principal Runtime & Sandbox Security Engineer` |

---

## 1. Intent
Implement the Tier-2 extension sandbox pool satisfying the shared `extension/port.ProviderRuntime` contract (`TP-IMP-0010`) using pure-Go `github.com/tetratelabs/wazero` (`CGO_ENABLED=0`) under a **Default-Deny `Zero-Ambient-Authority Sandbox`**: `0` filesystem (`FS`) access, `0` environment variable (`Env`) access, `0` unmediated wall-clock access, `0` raw network socket access (all outbound HTTP calls delegate exclusively through `BlindHostCredentialBroker`), a hard linear-memory ceiling of `<= 16 MiB` (`256` Wasm pages) per instance, immediate CPU/deadline interruption on runaway loops, and automatic quarantine (`QUARANTINED`) on repeated guest faults per `FMEA-05`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (add pure-Go dependency `github.com/tetratelabs/wazero`).
  - `internal/extension/wasm/runtime.go` (initialize `wazero.Runtime` with `WithCloseOnContextDone(true)` and `WithMemoryLimitPages(256)` = `16 MiB`, pre-compiling `CompiledModule` once upon package activation).
  - `internal/extension/wasm/pool.go` (bounded-concurrency instance pool that immediately destroys and replaces any instance hitting a `trap`, `timeout`, or page-growth high-water mark to eliminate cross-request state bleed).
  - `internal/extension/wasm/abi_host.go` (implement all 8 methods of `extension/port.ProviderRuntime` locked in `TP-IMP-0002` & `POC-01`: `TransformRequest`, `TransformResponse`, `TransformStreamChunk`, `NormalizeError`, `ExtractUsage`, `InspectNativeRequest`, `BuildValidationProbe`, `VerifyValidationResult` + blind HTTP egress host callback via `TP-IMP-0010`).
  - `internal/extension/wasm/fault_guard.go` (sliding-window fault tracker per `FMEA-05`: if an `extension_installation_id` triggers **`> 20` `trap/timeout/OOM` faults within a `60-second` sliding window**, automatically invoke `QuarantineFunc` from `TP-IMP-0011` to move `SM-01` to `QUARANTINED`, emit `ALT-PLUGIN-TRAP`, and isolate healthy sibling Adapters).
- **Out-of-Scope:** `Tier-3 Isolated Sidecar` engine over `ConnectRPC` UDS/mTLS (owned by `TP-IMP-0042` in `v0.3`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0302`, `TP-FR-0303`, `TP-NFR-0101`, `TP-NFR-0202`, `TP-NFR-0401`, `SLO-02`, `TP-BR-0401`, `TP-BR-0404`, `CT-17`, `FZ-05`, `FMEA-05`.
- **Dependencies:** `TP-IMP-0004`, `TP-IMP-0010`, `TP-IMP-0011`.
- **Design Refs:** [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Section 2, [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-05`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-17`, `FZ-05`).

## 4. API, Wire & Data Contracts
- **Hard Resource & Execution-Time Caps (`FMEA-05` & `system-design.md`):**
  - Maximum linear memory per Wasm instance: **`16 MiB`** (`256 * 64 KiB`); large multimodal payloads bypass Wasm linear memory via Host `blob_ref` handles (`CR-05`).
  - Execution deadlines: **`<= 15 ms`** for request/response boundary hooks (`TransformRequest`, `TransformResponse`, `ExtractUsage`) and **`<= 5 ms`** per streaming frame (`TransformStreamChunk`) to protect the `< 1 ms/chunk p95` SLO budget.
  - Every Wasm `trap`/`timeout`/`OOM` fault is translated into canonical error **`502 extension_execution_fault`** — strictly never panicking the Tanpopo Core host process.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given valid fixture `echo_transform.wasm` from `internal/testkit/wasmfixtures` (`TP-IMP-0006` — zero host `TinyGo` installation required), When running 100 concurrent request transforms + 50 SSE frames per stream across the `wazero` pool, Then `0` data races occur (`-race`), all frames arrive in order, and RSS memory remains bounded.
- **`NEG-1` (`CT-17` & `FMEA-05` Infinite Loop / Memory Bomb / Quarantine Threshold):**
  1. Loading `infinite_loop.wasm` → interrupted strictly within `<= 15 ms` (`<= 5 ms` per chunk), returning `502 extension_execution_fault` and releasing CPU immediately.
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
