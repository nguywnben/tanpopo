# Technical Feasibility Spike Results (`SPIKE-01`..`SPIKE-04`) — Tanpopo

> **Empirical Pre-Build Validation Record (Milestone `M0` — `TP-IMP-0004`, Closing `GAP-07`, `POC-01`, `POC-02`)**
> - **Document ID:** `ARCH-SPIKE-01` | **Version:** `1.0.0` | **Date:** `2026-11-14`
> - **Authorized By:** Product Owner Ruling on `GOV-OAS-01` (`DEC-009`, `COND-PO-01`..`05`)
> - **Isolated Harness Location:** `tools/spikes/` (`module github.com/nguywnben/tanpopo-spikes`, `CGO_ENABLED=0`, zero paid or external AI API calls)
> - **Epistemic Honesty Notice (`COND-PO-01`):** Numbers tagged **`SPIKE_MEASURED_ON_HOST`** below are empirical measurements of the isolated spike harness (`tools/spikes/*`) on the developer host documented in Section 1. Full end-to-end Gateway service-level objectives (`SLO-01`..`SLO-08` across full HTTP router + snapshot + storage + telemetry pipeline on reference `4 vCPU / 8 GB RAM` Linux infrastructure) **remain explicitly `TARGET / UNVERIFIED`** until measured in `TP-IMP-0035` and `TP-IMP-0043`.

---

## 1. Test Environment & Hermetic Isolation Guard

| Attribute | Exact Verified Value |
|---|---|
| **CPU** | `12th Gen Intel(R) Core(TM) i5-12450H` (`12` logical threads) |
| **OS / Architecture** | `windows/amd64` |
| **Go Toolchain** | `Go 1.27.4 windows/amd64` with `CGO_ENABLED=0` (`100%` pure Go, zero C/C++ FFI) |
| **Pinned Libraries** | `github.com/tetratelabs/wazero v1.9.0`, `modernc.org/sqlite v1.34.5`, `github.com/bufbuild/protocompile v0.14.1`, `gopkg.in/yaml.v3 v3.0.1` |
| **Hermetic Network Enforcement** | `tools/spikes/hermetic_main_test.go` overrides `http.DefaultTransport` and tests `hermeticControl` socket-level `net.Dialer.Control` hook: **blocks 100% of non-loopback (`127.0.0.1` / `::1`) TCP/UDP/TLS connections** (`TestHermeticGuardBlocksExternalEgress`: `1.1.1.1:443` TCP, `8.8.8.8:53` UDP, and `https://example.com` all deterministically rejected before `SYN`). Zero paid or external AI API calls occurred. |
| **Reproducible Command** | `cd tools/spikes && CGO_ENABLED=0 go test -v ./... && CGO_ENABLED=0 go test -run="^$" -bench="Benchmark" -benchmem ./...` |

---

## 2. Summary Verdict Matrix (`SPIKE-01`..`SPIKE-04`)

| Spike ID | Hypothesis Tested | Quantitative Pass Threshold (`TP-IMP-0004`) | Empirical Result (`SPIKE_MEASURED_ON_HOST`) | Verdict & Architectural Impact |
|---|---|---|---|---|
| **`SPIKE-01`** | Non-Turing-Complete Tier-1 Declarative Adapter Grammar (`POC-02`) | Express `>= 8 / 10` (`>= 80%`) representative providers in `<= 120` YAML lines (`0` arbitrary code); reject `> 32`-level JSON in `< 1 ms`. | **`9 / 10` (`90%`) providers expressed** (`OpenAI`, `Anthropic`, `Gemini`, `Groq`, `Together`, `Mistral`, `Fireworks`, `DeepSeek`, `Ollama`), max `48` lines (`<= 120`), **`5,367 ns/op` (`1,582 B/op`, `39 allocs/op`)**. `40`-level JSON rejected at depth `33` in `< 0.7 ms`. Only `AWS Bedrock` (`SigV4` + binary `EventStream`) routed to Tier-2 Wasm (`v0.2` per `CR-01`). | **`PASS`** — Locks **`POC-02`** non-Turing-complete declarative operator set (`TP-IMP-0012`). |
| **`SPIKE-02`** | Pure-Go `wazero` Tier-2 Wasm Linear-Memory Slice ABI + `1,000` Simultaneous SSE Streams + Blind Credential Broker (`POC-01`) | `1,000` concurrent SSE streams x `50` chunks (`50,000` chunk transforms): req overhead `p95 < 5 ms`, chunk `p95 < 1 ms`, heap `< 250 MB`, `0` canary bytes in Wasm RAM, infinite loop aborted cleanly, `64 MiB` `memory.grow` denied (`-1`). | **`3 / 3` consecutive uncached runs (`3,000` SSE streams, `150,000` Wasm chunk calls) succeeded with `0` errors** (`0.32–0.35s`/run); microbenchmark **`814.6 ns/op` (`0.81 µs` < `1 ms` target, `273 B/op`, `6 allocs/op`)**; request overhead `p95 <= 513.3 µs`, `p99 <= 1.02 ms` (`< 5 ms`); `HeapInuse = 48.98–64.09 MiB` (`< 250 MB`); **`0` canary bytes across all `64` pooled Wasm memory spaces**; `loop br 0 end` aborted in `15.29–15.52 ms`; `64 MiB` `memory.grow` returned `-1` (`65,536` bytes retained). | **`PASS` *(with 3 mandatory implementation invariants recorded in Section 3)* ** — Locks **`POC-01`** Linear-Memory Slice ABI (`TP-IMP-0013`), **zero Change Request (`CR`) needed (`COND-PO-05`)**. |
| **`SPIKE-03`** | Concurrent Two-Phase `micro-USD` Budget Pre-Hold (`SQLite WAL`), Over-Reservation Carryover & `< 500 ms` Upstream Abort | `50` simultaneous requests (`100,000 micro-USD` ceiling each) against `500,000 micro-USD` remaining: exact `5` admitted, `45` rejected `429`, `0` overrun; mid-stream disconnect aborts upstream `< 500 ms`. | `50`-goroutine race completed in **`1.57 ms`**: exact **`5 / 50` admitted**, **`45 / 50` rejected (`429 project_budget_exceeded`)**, **`0` `micro-USD` unauthorized overrun**; `105,000 micro-USD` actual stream settled to `-5,000 micro-USD` carryover blocking next request; client cancel at chunk `#15` aborted upstream in `< 1 ms` (`SETTLED_ESTIMATED`). | **`PASS`** — Validates `ADR-0005` SQLite WAL transaction pattern (`TP-IMP-0021`); HA Valkey/PG failover remains subject to `COND-PO-03` in `TP-IMP-0040` / `0043`. |
| **`SPIKE-04`** | `Authorized OAuth 2.1` Single-Flight Refresh Coalescing, `invalid_grant` Circuit Break & `300`-Tool RFC 8785 JCS `schema_hash` | `100` concurrent callers on expired token -> exact `1` `/oauth/token` refresh (`0` retry storm on `invalid_grant`); cross-`aud` rejected `403`; `300` MCP tool schemas JCS-hashed `< 5 ms`. | `100`-goroutine stampede issued **exact `1` `/oauth/token` call** (`100 / 100` callers received rotated token); `50`-goroutine `invalid_grant` burst issued **exact `1` call** -> `REAUTH_REQUIRED` (`AUTH-10`); cross-`aud` rejected `403 token_audience_violation`; **`300` MCP tool schemas JCS-canonicalized + `SHA-256` hashed in `1.07 ms`** (`< 5 ms`), catching subtle description tamper (`Rug-Pull`). | **`PASS`** — Validates `SM-03` single-flight coordinator (`TP-IMP-0030`) and synchronous MCP `tools/list` JCS guard (`TP-IMP-0029`). |

---

## 3. Critical Empirical Engineering Findings from `SPIKE-02` (`COND-PO-05` Accountability)

Running real `wazero v1.9.0` instances under a synchronized `1,000`-stream / `50,000`-chunk storm on `windows/amd64` surfaced **three concrete runtime behaviors** that were invisible on paper and MUST be enforced in `TP-IMP-0013` (`ADR-0002`):

1. **Mandatory Tainted-Module Eviction After `WithCloseOnContextDone(true)` Timeout or Trap:**
   - **Empirical Observation:** In `wazero`, enabling `RuntimeConfig.WithCloseOnContextDone(true)` interrupts a runaway `loop br 0 end` cleanly when `execCtx` expires, **but permanently closes that `api.Module` (`module closed with context deadline exceeded`)**. Returning that `api.Module` handle to a pool causes `100%` of subsequent callers borrowing that slot to fail immediately.
   - **Locked Production Rule (`TP-IMP-0013`):** Whenever any `api.Function.Call(execCtx, ...)` returns a non-nil error (timeout, trap, or invalid output slice), the pool wrapper **MUST** close the tainted `api.Module` (`_ = mod.Close(context.Background())`) and instantiate a fresh replacement from the pre-compiled `wazero.CompiledModule` before releasing the pool slot. On normal return, both input and output slices inside Wasm linear memory **MUST** be zeroed (`clear(buf)`).
2. **Strict Separation of Pool Queue Wait Time from Per-Call Wasm CPU Watchdog Time:**
   - **Empirical Observation:** Wrapping `<-pool.ch` inside the same short `context.WithTimeout` used for Wasm CPU execution causes requests queued behind a `1,000`-stream burst on a `64`-instance pool to expire while waiting for a slot and unnecessarily close healthy Wasm modules.
   - **Locked Production Rule (`TP-IMP-0013`):** Pool acquisition honors the parent HTTP request/queue admission budget, whereas `execCtx, cancel := context.WithTimeout(parentCtx, hookWatchdogTimeout)` starts **strictly after** acquiring the `wasmInstanceHandle` and writing input bytes into linear memory.
3. **Pool-Wide Atomic Slot Watchdog (`CloseWithExitCode`) Instead of Per-Chunk `context.WithTimeout` Goroutine Storms:**
   - **Empirical Observation:** In `wazero v1.9.0`, passing a cancellable `context.WithTimeout` (`ctx.Done() != nil`) into `Function.Call(ctx)` under `WithCloseOnContextDone(true)` allocates a timer and spawns a **new background watcher goroutine on every single call** (`50,000` timers + `50,000` goroutines across `1,000` simultaneous SSE streams x `50` chunks), causing scheduler thrashing and occasional tail timeouts under GC/OS preemption. Conversely, compiling with `WithCloseOnContextDone(true)` (which instruments Wasm loop back-edges to check the module's atomic `closed` flag) while invoking `Call(context.Background(), ...)` and arming a **single pool-wide watchdog goroutine** scanning `64` `atomic.Int64` slot deadlines (`mod.CloseWithExitCode(context.Background(), 1)` on breach) spawns **`0` goroutines and `0` timers per chunk**, cuts memory allocations from `658 B/op` (`11 allocs`) to **`273 B/op` (`6 allocs`, `814.6 ns/op`)**, passes `3 / 3` consecutive `50,000`-chunk storm runs with `0` errors, and interrupts `loop br 0 end` cleanly in `~15.4 ms`.
   - **Locked Production Rule (`ADR-0002`, `SPEC-EXT-01`, `TP-IMP-0013`):** Use **one shared pool watchdog goroutine** over `atomic.Int64` per-slot deadlines (`CloseWithExitCode`) rather than per-chunk `context.WithTimeout`, keeping **Latency SLO Targets** (`< 5 ms p95` request/response, `< 1 ms p95` SSE chunk — `TARGET / UNVERIFIED`) distinct from **Hard Destructive Watchdog Deadlines** (`>= 50–200 ms` above OS scheduler preemption quantum). Because per-chunk Linear-Memory Slice ABI achieved `814.6 ns/op` (`> 1,200x` faster than the `1 ms` budget), **no architectural fallback CR is required (`POC-01` is confirmed)**.

---

## 4. Machine-Readable Contract Verification (`TP-IMP-0002` & `TP-IMP-0003` Cross-Check)

Alongside `SPIKE-01`..`04`, `tools/spikes/contract_schema_validation_test.go` mechanically verifies the repository's canonical machine-readable contracts in pure Go (`CGO_ENABLED=0`):
1. **`proto/tanpopo/extension/v1/runtime.proto`:** Compiled via `github.com/bufbuild/protocompile` (`0` syntax/type/import errors; verifies `ExtensionControlService` + `ProviderExecutionService` RPCs including `ValidateConfigStatic`, `ProbeConnectionLive`, `TransformRequest`, `TransformResponse`, `TransformStreamChunk`, and `InspectNativeRequest`).
2. **`openapi/dataplane-v1.yaml` & `openapi/admin-v1.yaml`:** Parsed via `gopkg.in/yaml.v3` (`0` broken internal `#/components/...` `$ref`s; verifies presence of all Data Plane endpoints including `/v1/rerank` and Admin endpoints across `SCR-01`..`16` including `/admin/v1/playground/sessions`).
3. **`db/spec-check/sqlite_schema.sql` & `db/spec-check/postgres_schema.sql`:** Executes `sqlite_schema.sql` on live in-memory `modernc.org/sqlite` with `PRAGMA foreign_keys = ON;`, asserting all `26` canonical domain table groups exist in both dialects, composite `(org_id, id)` cross-tenant foreign keys reject mismatched `org_id` writes, and `audit_events` / `usage_settlements` immutability triggers reject `UPDATE` and `DELETE`.

---

## 5. Remaining Unverified Production Hypotheses (`COND-PO-01`..`04` Continuity)

Per Product Owner ruling `DEC-009`, passing `M0` spikes does **NOT** waive empirical production verification in `M1`–`M5`:
- **`SLO-01`..`SLO-08` (`COND-PO-01`):** Remain `TARGET / UNVERIFIED` until full-gateway load testing on reference hardware (`TP-IMP-0035`, `TP-IMP-0043`).
- **`Crypto-Shredding` & Cold Backup Non-Recoverability (`COND-PO-02`):** Remain `TARGET / UNVERIFIED` until full `Master KEK -> Org DEK -> Project DEK` lifecycle, RAM cache purge, and live backup/restore tests pass (`TP-IMP-0008`, `TP-IMP-0032`).
- **`Valkey / PostgreSQL` HA Ledger Failover Consistency (`COND-PO-03`):** `SPIKE-03` proved single-node SQLite WAL serialization; distributed Valkey primary kill, network split, and recovery consistency MUST be proven in `TP-IMP-0021`, `TP-IMP-0040`, and `TP-IMP-0043`.
- **`MCP 2025-03-26` Real Client Interoperability (`COND-PO-04`):** `SPIKE-04` proved JCS `schema_hash` speed and OAuth single-flight; real MCP reference client & IDE transport tests (`CT-36`) MUST pass in `TP-IMP-0028` and `TP-IMP-0029`.
