# `TP-IMP-0043` — `k6` Load Benchmark Suite (`1,000` SSE Streams), Empirical `SLO-01..08` Verification & Timed `FMEA-01..08` Chaos / `DR-01..04` Recovery Drills

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0043` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release`) · `EP-09` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0040`, `TP-IMP-0042` — `GAP-01`)* |
| **Size** | `L` (`tests/perf/k6/*`, `tests/chaos/*`, `docs/quality/benchmark-evidence.md`) |
| **Review Tier** | `STANDARD_REVIEW` *(Replacing all `TARGET / UNVERIFIED` labels with honest empirical measurements on standardized reference hardware)* |
| **Technical Owner** | `Principal Performance & Chaos SRE Lead` |

---

## 1. Intent
Execute the high-concurrency load, tail-latency, overload-shedding, and timed chaos/disaster-recovery verification campaign in a standardized hermetic environment (`0` live external AI calls — backed by programmable-latency `FakeLLMServer` clusters) per [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`SPEC-QA-03`) and [`backups-recovery.md`](../../operations/backups-recovery.md) (`SPEC-OPS-02`), **converting every `SLO-01`..`08` metric currently marked `TARGET / UNVERIFIED` into reproducible empirical evidence** ahead of `v1.0 GA` gate review.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `tests/perf/k6/sse_1000_streams.js`, `declar_vs_wasm_overhead.js`, `overload_shedding_503.js`, `rolling_restart_zero_drop.js` (`OPS-AC-06` measuring zero-drop 3-node rolling restarts under `100` active SSE streams).
  - `tests/chaos/fmea_suite_test.go` (automated reproduction and verification of all **8 deep failure modes**: `FMEA-01`, `FMEA-02`, `FMEA-03`, `FMEA-04`, `FMEA-05`, `FMEA-06`, `FMEA-07`, `FMEA-08`).
  - `tests/chaos/dr_drills_test.go` (wall-clock timed execution of all **4 disaster-recovery scenarios**: `DR-01` SQLite disk corruption, `DR-02` PostgreSQL PITR + `tanpopo admin verify-state`, `DR-03` emergency `Master KEK` rotation & RAM cache purge, and `DR-04` `< 60 seconds` failed-upgrade rollback — confirming `RTO < 15 minutes` per `SLO-08`).
  - `docs/quality/benchmark-evidence.md` (recording honest empirical measurements across `SLO-01`, `SLO-02`, `SLO-03`, `SLO-04`, `SLO-05`, `SLO-06`, `SLO-07`, and `SLO-08` — never cherry-picking or inflating numbers).
- **Out-of-Scope:** Grey-box penetration testing and multi-hour fuzzing campaigns (covered in parallel unit `TP-IMP-0044`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-NFR-0101`..`0103`, `TP-NFR-0201`, `TP-NFR-0202`, `TP-NFR-0301`, `TP-NFR-0302`, `TP-NFR-0601`, `SLO-01`..`08`, `FMEA-01`..`08`, `DR-01`..`04`, `OPS-AC-01`..`12`, `GATE-REL-03`, `GATE-REL-06`.
- **Dependencies:** `TP-IMP-0040`, `TP-IMP-0042`.
- **Design Refs:** [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`SPEC-QA-03`), [`backups-recovery.md`](../../operations/backups-recovery.md) (`SPEC-OPS-02`), [`monitoring-incidents.md`](../../operations/monitoring-incidents.md) (`SPEC-OPS-03`).

## 4. API, Wire & Data Contracts
- **Zero Resource Leak Under Sustained Soak (`Soak Stability`):** Following continuous soak testing, goroutine count and post-`runtime.GC()` heap footprint MUST return to baseline levels (`0` linear memory/goroutine growth slope and `0` leaked file descriptors).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`SLO-01..08` Empirical Verification on Strict `2 vCPU / 2 GiB RAM` Floor):** Meets all target thresholds on the exact **`2 vCPU / 2 GiB RAM`** reference floor for single-node mode (per `srs.md` `TP-NFR-0301` & `nonfunctional-slo.md` — never inflating RAM limits to mask memory pressure) and a 3-node cluster for HA mode, backed by raw `k6` JSON summaries and `pprof` profiles.
- **`AC-2` (`FMEA-01..08`, `DR-01..04` & `OPS-AC-06` Pass):** All 8 FMEA chaos tests, all 4 DR recovery drills (`RTO < 15 minutes`, `DR-04` rollback `< 60 seconds`), and the 3-node rolling restart test (`OPS-AC-06` with `0` `502/503` errors) pass automatically with `0` FinOps ledger drift.
- **`NEG-1` (`3x Overload Shedding`):** Driving `300%` overload beyond queue capacity keeps Gateway RSS within its hard ceiling, sheds excess load cleanly via `< 5 ms` `503` responses, and recovers instantaneously once traffic subsides (zero panics or deadlocks).

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -timeout=30m ./tests/chaos/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v -timeout=30m ./tests/chaos/...`)*
- Race detector (`-race`): `TANPOPO_TEST_HERMETIC=1 go test -v -race -timeout=30m ./tests/chaos/...` *(PowerShell: `$env:TANPOPO_TEST_HERMETIC='1'; go test -v -race -timeout=30m ./tests/chaos/...`)*
- `k6 run tests/perf/k6/sse_1000_streams.js`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** All `k6` and `chaos` harnesses execute strictly over isolated local loopback/container networks against `FakeLLMServer` — `0` risk of accidental outbound Internet load generation.
- **Rollback:** Adds load/chaos test harnesses and empirical benchmark documentation only.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0040` and `TP-IMP-0042` are complete.
- **DoD:** `docs/quality/benchmark-evidence.md` completed with `100%` real empirical measurements, and all 8 FMEA + 4 DR + `k6` suites passing.
