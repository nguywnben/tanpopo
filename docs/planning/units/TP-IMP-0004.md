# `TP-IMP-0004` — Execute 4 Time-Boxed Empirical Spikes (`SPIKE-01`..`04`) & Freeze `POC-01` Wasm ABI / `POC-02`

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0004` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes` — **Gate `GATE-M0`**) · `EP-00` |
| **Status** | **`DONE`** *(Completed & verified offline on `2026-11-14` under Product Owner Stage 1 M0 approval `DEC-009` — locked `POC-01` & `POC-02` in `spike-results.md`)* |
| **Size** | `M` (`tools/spikes/*` with isolated `go.mod` + benchmark updates in `ADR-0002`, `ADR-0005`, `decisions.md`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Locks long-term Host–Wasm binary memory ABI architecture)* |
| **Technical Owner** | `Principal Platform Architect + Performance/Security Engineer` |

---

## 1. Intent
Execute the 4 time-boxed empirical engineering spikes (`SPIKE-01`..`04` defined in [`system-design.md`](../../architecture/system-design.md) Section 7 and [`release-readiness.md`](../../quality/release-readiness.md) Section 4.3) completely offline inside an isolated Go module (`tools/spikes/go.mod` — **never touching the root `go.mod` owned by `TP-IMP-0005` so both units execute in parallel with `0` merge conflicts**) to collect real benchmark measurements and definitively lock the Host–Wasm memory ABI (`POC-01`: `Linear-Memory Ring Buffer` vs `WIT Preview 2`) and Tier-1 Declarative operator set (`POC-02`) before building the `M1` production runtime.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `tools/spikes/go.mod`, `tools/spikes/go.sum` (`module github.com/nguywnben/tanpopo-spikes`, completely isolated from production Core; enforces its own loopback-only `127.0.0.1:0` socket guard inside `TestMain` without depending on `TP-IMP-0006`).
  - `tools/spikes/spike01_declarative_test.go` .. `spike04_oauth_jcs_test.go`.
  - Empirical benchmark report `docs/architecture/spike-results.md` and closure updates for `POC-01`, `POC-02` in `docs/architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md`, `docs/architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md`, `docs/specifications/extension-contract.md`, and `docs/governance/decisions.md`.
- **Out-of-Scope:** Do not create root `go.mod` (owned by `TP-IMP-0005`), production database code, Admin API, or Web Console; zero calls to paid external Internet AI APIs.

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0301`..`0303`, `TP-FR-0801`, `TP-NFR-0101`, `TP-NFR-0102`, `TP-NFR-0202`, `TP-NFR-0401`, `SLO-02`, `SLO-03`; `ASM-002`, `ASM-004`, `ASM-005`, `ASM-006`, `POC-01`, `POC-02`.
- **Dependencies:** `TP-IMP-0002`, `TP-IMP-0003`.
- **Design Refs:** [`system-design.md`](../../architecture/system-design.md)`#L316-L348`, [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`release-readiness.md`](../../quality/release-readiness.md)`#L92-L102`.

## 4. API, Wire & Data Contracts
- **`SPIKE-01`:** Model 10 representative AI provider APIs against the Tier-1 Declarative JSON Schema grammar (`<= 120 lines/manifest`, Turing-incomplete / loop-free).
- **`SPIKE-02`:** Benchmark on Go `1.24+` (`CGO_ENABLED=0`) + `wazero` (`16 MiB` linear-memory cap per instance per `SPEC-SEC-02`) under `1,000` simulated concurrent SSE streams (`50 chunks/stream`) over `127.0.0.1`: compare `p50/p95/p99` overhead, RSS footprint, and `while(1)` interrupt latency; inspect Wasm linear-memory dumps to verify `0 bytes` match canary secret `sk-tp-canary-...`.
- **`SPIKE-03`:** Fire `50` concurrent goroutines against a remaining budget capacity of exactly `5` requests on local `SQLite WAL` (`modernc.org/sqlite`) to measure `Pre-Hold` reservation throughput and `< 500ms` disconnect propagation.
- **`SPIKE-04`:** Fire a `100`-goroutine concurrent stampede into `Single-Flight Refresh` against a local mock OAuth server and benchmark `RFC 8785 JCS` canonicalization + hashing across `300` MCP tools.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `SPIKE-01`, When validating declarative manifests across 10 sample providers, Then at least `8/10` providers are expressed completely without compiling Wasm.
- **`AC-2`:** Given `SPIKE-02` under `1,000` local SSE streams constrained to `2 vCPU / 2 GiB RAM`, When benchmarking the selected Wasm ABI (or the hybrid fallback invoking Wasm at stream setup/teardown only), Then `p95` gateway overhead is `< 5.0 ms`, per-chunk SSE overhead is `< 1.0 ms`, and `0 bytes` of raw secrets appear in Wasm memory.
- **`AC-3`:** Given `SPIKE-03` and `SPIKE-04`, When executing concurrent contention benchmarks, Then exactly `5/50` goroutines acquire reservations (the remaining 45 receive `429 project_budget_exceeded` with `0` overspend) and exactly `1/100` requests hits `/oauth/token`.
- **`NEG-1`:** A Wasm guest executing `while(true){}` or attempting a `64 MiB` allocation beyond the `16 MiB` ceiling is cleanly terminated without panicking the host process.
- **`NEG-2`:** The socket guard inside `tools/spikes/` `TestMain` immediately fails the suite if any dial targets an address outside `127.0.0.1/::1`.
- **`NEG-3`:** If a candidate ABI misses the `< 5 ms p95` latency budget, the documented architectural pivot in `system-design.md` must be triggered rather than silently relaxing NFR thresholds.

## 6. Test Commands & Required Evidence
- Pure-Go (`CGO_ENABLED=0`) inside the isolated `tools/spikes` module:
  - POSIX/CI: `cd tools/spikes && CGO_ENABLED=0 go test -v -bench=. -benchmem ./...`
  - Windows PowerShell: `Push-Location tools/spikes; $env:CGO_ENABLED='0'; go test -v -bench=. -benchmem ./...; Pop-Location`
- Record latency percentile tables and memory `pprof` profiles in `docs/architecture/spike-results.md` and lock canonical ABI function signatures in `extension-contract.md`.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `floor-guard` in `TP-IMP-0005` strictly forbids any package under `cmd/` or `internal/` from importing `tools/spikes/`; all credentials used in spikes are synthetic `canary` tokens.
- **Rollback:** `tools/spikes/` is isolated behind its own `go.mod`, so reverting or archiving it has zero impact on root `go test ./...`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` and `TP-IMP-0003` are complete; Product Owner has explicitly authorized executing PoC code for `SPIKE-01`..`04`.
- **DoD:** `spike-results.md` published, `POC-01` & `POC-02` closed in `decisions.md`, and `GATE-M0` passed under `REQUIRES_STRONG_REVIEW`.
