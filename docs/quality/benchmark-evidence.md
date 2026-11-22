# Tanpopo Empirical Benchmark, Load, Chaos & Disaster-Recovery Evidence (`TP-IMP-0043`)

> **Status:** `RECORDED (WITH HONEST PASS / FAIL / UNVERIFIED BOUNDARIES PER COND-PO-01)`
> **Unit:** [`TP-IMP-0043`](../planning/units/TP-IMP-0043.md) (`M5` / `EP-09`)
> **Recorded Date:** `2026-11-22`
> **Recorder:** **Gemini 4 Hydrogen**
> **Reference Design Contracts:** [`nonfunctional-slo.md`](./nonfunctional-slo.md) (`SPEC-QA-03`), [`backups-recovery.md`](../operations/backups-recovery.md) (`SPEC-OPS-02`), [`monitoring-incidents.md`](../operations/monitoring-incidents.md) (`SPEC-OPS-03`)

---

## 1. Test Environment, Hardware & Container Floor Configuration

All automated load and disaster-recovery drills were executed strictly inside an isolated local VM / Docker bridge network against programmable-latency `FakeLLMServer` (`0` external AI API calls). No hardware specifications were inflated and no failed or degraded measurements were omitted (`COND-PO-01`).

### 1.1. Host & Container Reference Floor
| Component | Measured Configuration | Notes |
|---|---|---|
| **Host CPU** | `Intel(R) Xeon(R) CPU @ 2.30GHz` (`2` logical vCPUs) | Single VM hosts Docker daemon, `FakeLLMServer` harness, `grafana/k6`, DB/Valkey containers, and `tanpopo` SUT simultaneously |
| **Host Memory** | `7,959 MiB` RAM · `8,191 MiB` swap | Linux VM |
| **Single-Node SUT Cgroup Floor** | Docker `--cpus=2 --memory=2g --memory-swap=2g` | `GOMAXPROCS=2`, `GOMEMLIMIT=1750MiB`, pure-Go static binary `/tmp/tanpopo-bench` (`CGO_ENABLED=0`, `68M`) |
| **PostgreSQL 16+ & Valkey Cluster** | `postgres:16-alpine` (`127.0.0.1:5432`) + 3-shard `valkey/valkey:8-alpine` Cluster (`ports 7001/7002/7003`, `cluster_state:ok`, `16384` slots) | Used for live `DR-02` `verify-state`, PG16 `FORCE RLS`, and multi-node revocation (`TestTPIMP0040`) |
| **Load & Chaos Tooling** | `grafana/k6:latest`, Go `go test -race` & `CGO_ENABLED=0`, `tests/perf/harness/main.go` (`seed`, `fakellm`, `lb`) | Hermetic loopback/bridge traffic only |

---

## 2. Verification Summary Matrix (`SLO-01..08`, `NEG-1`, `OPS-AC-06`, `FMEA-01..08`, `DR-01..04`)

| Area / ID | Target / Scope | Empirical Result | Verdict |
|---|---|---|---|
| **Isolated Hot-Path Benchmarks (`task-5946`)** | Component latency (`GOMAXPROCS=2`) | `BenchmarkEvaluateEffective-2`: **`256.2 ns/op` (`0 B/op`, `0 allocs/op`)**<br/>`BenchmarkSnapshotLookup-2`: **`475.7 ns/op` (`0 B/op`, `0 allocs/op`)**<br/>`BenchmarkResolveRoute-2`: **`20,034 ns/op` (`11,449 B/op`, `95 allocs/op`)**<br/>`BenchmarkDeclarativeTransformRequest-2`: **`18,769 ns/op`** (`5,824 B/op`) · stream chunk: **`11,959 ns/op`**<br/>`BenchmarkWasmTransformRequest-2`: **`14,567 ns/op`** (`1,808 B/op`) · stream chunk: **`9,554 ns/op`**<br/>`BenchmarkSSEPumpPerChunkOverhead-2`: **`39,456 ns/op`** (`11,191 B/op`) | **`PASS`** *(isolated sub-ms core)* |
| **E2E Tier-1 vs Tier-2 Full-Stack Load (`declar_vs_wasm.json`)** | `500 RPS` attempted on single-host `2 vCPU / 2 GiB` SQLite standalone | `4,265` completed reqs (**`103.69 req/s`**), `0.00%` HTTP errors (`3,725/3,725` `200 OK` checks), `11,339` dropped `k6` iterations.<br/>Tier-1 `tanpopo_tier1_declar_ms`: `min=16.44 ms`, `med=3,469.53 ms`, **`p95=3,705.98 ms`**.<br/>Tier-2 `tanpopo_tier2_wasm_ms`: `min=84.01 ms`, `med=3,712.74 ms`, **`p95=3,973.60 ms`**. | **`FAIL (E2E saturation under 2 vCPU SQLite standalone)`** *(see Root Cause §3.2)* |
| **`1,000` Concurrent SSE Streams (`sse_1000.json`)** | Hold `1,000` concurrent SSE streams + `+500 RPS` short requests on `2 vCPU / 2 GiB` | **SSE streams:** **`1,715 / 1,715` (`100.00%`)** `200 OK` and `[DONE]`, **`0`** internal `5xx`, `tanpopo_sse_ttfb_ms` `min=84.39 ms`, `med=10,616.05 ms`, `p95=14,563.52 ms`, Post-1,000 SSE RSS = **`221.4 MiB / 2 GiB`**.<br/>**Simultaneous `+500 RPS` short reqs:** `0 / 600` succeeded within `10s` client timeout (`600` timed out at `med=10,000.82 ms`, `6,302` `k6` dropped iterations). | **`PARTIAL`** *(1,000 SSE streams completed cleanly with 0 5xx and 221.4 MiB RSS; simultaneous +500 RPS queue saturated 2-vCPU single host)* |
| **Cold Start & Memory Budget (`meta.txt`, `SLO-05`)** | Startup `/readyz = 200 < 3.0 s` & Idle RSS `< 128 MiB` on `2 vCPU / 2 GiB` | Cold start = **`1,045 ms`**; Idle RSS = **`74.71 MiB / 2 GiB`**; Post-overhead RSS = **`99.42 MiB / 2 GiB`**; Post-1,000 SSE RSS = **`221.4 MiB / 2 GiB`**; Post-overload RSS = **`34.89 MiB / 2 GiB`**. | **`PASS`** |
| **`3x` Overload Shedding (`NEG-1`, `overload_503.json`)** | `TANPOPO_MAX_CONCURRENT_STREAMS=30`, `50` holding VUs + `300 RPS` burst + `/healthz` + recovery | **Go unit test (`TestProtectiveOverloadSheddingHeapAndStreamsNEG1`, `-race`):** `< 1 ms` `503 gateway_overloaded` on heap ratio & stream cap.<br/>**2-vCPU Docker `k6` (`overload_503.json`):** `1,443 / 1,443` (`100.00%`) checks pass (`364/364` admitted SSE `[DONE]`, **`2,863`** shed via **`503 gateway_overloaded`** + `Retry-After` at `tanpopo_shed_503_ms` **`med=4.00 ms`, `p95=27.86 ms`**, **`101/101`** `/healthz 200 OK`, **`151/151`** post-burst recovery `200 OK`, RSS = **`34.89 MiB / 2 GiB`**). | **`PASS` (Go unit `< 1ms` & shedding/recovery)** · **`PARTIAL` (Docker `p95=27.86ms` > `5ms` target under 2-vCPU contention)** |
| **Cluster Revocation & PG16 `FORCE RLS` (`task-5942`, `SLO-01`)** | Multi-node cluster revocation (`< 5s`) + PG16 `FORCE RLS` + Valkey Cluster | `TestTPIMP0040MultiNodeClusterPG16AndValkeyAC1ToAC4` (**`7.67s`**), `TestTPIMP0040GracefulDrainReadyzAndSSE` (**`0.79s`**), `TestTPIMP0040PG16ForceRLSEndToEnd` (**`5.13s`**) — all **`PASS` (`-race`)**. | **`PASS`** |
| **3-Node Rolling Restart (`OPS-AC-06`)** | `100` active SSE streams during 3-node rolling restart | **In-process 3-node Go chaos suite (`TestOPSAC06ThreeNodeRollingRestartZeroDrops100SSE`):** **`100 / 100`** streams completed with **`0`** `502/503` drops (`0.78s`).<br/>**Multi-container Docker `k6` (`rolling_restart.json` + `40ms` `/readyz` LB poll):** **`1,406 / 1,410` (`99.71%`)** `[DONE]`, **`4`** pre-connect `connection refused`/EOF errors during `40ms` poll window without Kubernetes `preStop` wait (`http_req_duration` `med=1.01s`). | **`PASS` (Go Graceful Drain) · `PARTIAL` (Docker LB poll race without K8s `preStop`)** |
| **8 Chaos Failure Modes (`FMEA-01..08`)** | Automated reproduction in `tests/chaos/fmea_suite_test.go` | All **`8 / 8`** (`FMEA-01`..`FMEA-08`) passed under both `CGO_ENABLED=0` (`2.673s` suite) and `-race` (`25.309s` total chaos suite). | **`PASS`** *(scoped unit/subsystem chaos drills)* |
| **4 Disaster-Recovery Drills (`DR-01..04`, `SLO-08`)** | Timed recovery (`RTO < 15m`, `DR-04 < 60s`) | `DR-01`: **`36.29 ms`** (CGO0) / **`141.4 ms`** (`-race`)<br/>`DR-02`: **`30.18 ms`** (CGO0) / **`235.6 ms`** (`-race`) + live PG16+Valkey `tanpopo admin verify-state` **`278 ms`** (`audit_chain_valid=true`, `audit_events_verified=11`, `valkey_rebuilt=true`)<br/>`DR-03`: **`43.66 ms`** (CGO0) / **`66.4 ms`** (`-race`) (`epoch=3` cache eviction)<br/>`DR-04`: **`4.50 ms`** (CGO0) / **`113.9 ms`** (`-race`) (`< 60s` rollback target) | **`PASS`** *(in-cluster/local host drills)* |
| **Multi-Host Physical HA & Sustained `>= 4h` Soak** | Multi-node Kubernetes network, multi-AZ PG/Valkey failover, `4h`–`24h` memory soak | Not executed inside single-VM devbox environment (`2 vCPU` single VM). | **`UNVERIFIED`** |

---

## 3. Detailed Benchmark Evidence & Root-Cause Findings

### 3.1. Isolated Pipeline Benchmarks (`go test -bench -benchmem`, `GOMAXPROCS=2`)
Command: `go test -run=^$ -bench=... -benchmem` on `Intel(R) Xeon(R) CPU @ 2.30GHz` (`task-5946.log`):
```text
BenchmarkDeclarativeTransformRequest-2         125186     18769 ns/op    5824 B/op    88 allocs/op
BenchmarkDeclarativeTransformStreamChunk-2     202765     11959 ns/op    4408 B/op    70 allocs/op
BenchmarkWasmTransformRequest-2                185036     14567 ns/op    1808 B/op    27 allocs/op
BenchmarkWasmTransformStreamChunk-2            239125      9554 ns/op     744 B/op    17 allocs/op
BenchmarkEvaluateEffective-2                  9409483       256.2 ns/op     0 B/op     0 allocs/op
BenchmarkResolveRoute-2                        112089     20034 ns/op   11449 B/op    95 allocs/op
BenchmarkSnapshotLookup-2                     4767938       475.7 ns/op     0 B/op     0 allocs/op
BenchmarkSSEPumpPerChunkOverhead-2              57522     39456 ns/op   11191 B/op    17 allocs/op
```

### 3.2. `declar_vs_wasm.json` Saturation Root Cause
While isolated request transformation executes in **`14.57 µs`–`18.77 µs`**, full end-to-end `500 RPS` throughput on a standalone `2 vCPU / 2 GiB` container with all components sharing the same 2-vCPU VM reached **`103.69 req/s`** (`4,265` completions, `0` HTTP `5xx`/`4xx`, `p95` **`3,705.98 ms`** Tier-1 / **`3,973.60 ms`** Tier-2). Two verified code-level serialization points plus shared-VM CPU contention explain this ceiling:
1. **`BlindHostCredentialBroker` upstream HTTP dialer (`internal/security/broker/blind_broker.go`):** Sets `DisableKeepAlives: true` on upstream transports for strict SSRF pin-per-dial isolation, forcing a fresh TCP socket setup per upstream request instead of pooled keep-alive reuse.
2. **Standalone SQLite connection serialization (`internal/storage/sqlite/store.go`):** Sets `db.SetMaxOpenConns(1)` for SQLite writer safety, serializing synchronous budget ledger reservations and usage settlements across all concurrent HTTP workers on a single connection while `k6` and `FakeLLMServer` compete for the same 2 host vCPUs.

### 3.3. `sse_1000.json` Concurrency & Memory Profile
- **SSE Scenario:** `1,715 / 1,715` streaming requests returned HTTP `200 OK` and terminal `data: [DONE]` with **`0` internal `5xx` errors** while RSS stayed at **`221.4 MiB`** under the **`2 GiB`** cgroup limit (`< 11%` of memory cap).
- **Concurrent Short-Request Contention:** Under `1,000` simultaneously active SSE streams plus an attempted `+500 RPS` short-request arrival rate on the same 2-vCPU VM, TCP/SQLite queues backed up past the `10s` client timeout (`600 / 600` short requests timed out at `med=10,000.82 ms`, `6,302` `k6` dropped iterations).

### 3.4. `overload_503.json` (`NEG-1` Protective Shedding Verification)
With `TANPOPO_MAX_CONCURRENT_STREAMS=30`, `50` sustained holding VUs, `300 RPS` burst VUs, concurrent `/healthz` probes, and post-burst recovery probes:
- **`checks`:** `1,443 / 1,443` (`100.00%` pass)
- **Admitted SSE (`364 / 364`):** `100.00%` `[DONE]`
- **Shed Excess (`2,863` reqs):** `100%` HTTP `503 gateway_overloaded` with `Retry-After` header (`tanpopo_shed_503_ms`: `min=-1.55 ms`, `med=4.00 ms`, `avg=7.80 ms`, `p95=27.86 ms`; note: minor negative `min` reflects `grafana/k6` sub-millisecond Docker clock jitter, and `p95=27.86 ms` exceeded `< 5 ms` under 2-vCPU Docker contention while `med=4.00 ms` and the isolated Go unit check remained `< 5 ms`).
- **Health & Recovery:** `101 / 101` `/healthz` probes `200 OK`; `151 / 151` post-burst recovery requests `200 OK`; final RSS **`34.89 MiB / 2 GiB`**.
- **Accounting Boundary Note:** `Server.activeStreams` tracks active streaming handlers after upstream headers establish (`post-TTFB`), and `native.Handler` currently runs on its own mux in `cmd/tanpopo/serve.go` rather than inside `Server`.

---

## 4. Chaos (`FMEA-01..08`) & Disaster Recovery (`DR-01..04`) Evidence

### 4.1. Automated Chaos Suite (`tests/chaos/...`)
Verified green under both `CGO_ENABLED=0` (`2.673s` total) and `-race` (`25.309s` total, `0` data races):
- **`FMEA-01` (`TestFMEA01MidStreamDisconnectProhibitsFailover`):** Confirms mid-stream upstream reset after emitting the first SSE frame aborts cleanly without replaying onto any backup deployment (`backupCalls == 0`). *(HTTP `ESTIMATED` mid-stream ledger settlement was verified separately in `TP-IMP-0021`.)*
- **`FMEA-02` (`TestFMEA02SideEffectRetryProhibitionAndSingleUseNonce`):** Enforces `0` automatic retries on mutating/destructive MCP tools and single-use HITL approval nonce burn.
- **`FMEA-03` (`TestFMEA03OptInResponseCacheIsolationAndCryptoShredding`):** Verifies cross-org/cross-project cache isolation and immediate post-shred `0`-hit purge.
- **`FMEA-04` (`TestFMEA04MissingOrMalformedUsageFallbackLadder`):** Verifies `pricing.Calculate` suppresses `EXACT=0` when tokens are absent on non-empty text (falling back to `ESTIMATED`), and emits `UNMETERED_FLAGGED` when rate card is absent. *(Zero-drift hold release in `ledger.Service` was verified in `TP-IMP-0021`.)*
- **`FMEA-05` (`TestFMEA05ExtensionSandboxTrapOOMAndAutoQuarantine`):** Invokes `wasm.Runtime.TransformRawSlice` directly to confirm `<= 15ms` infinite-loop deadline (`FaultKindTimeout`), `16 MiB` OOM rejection, and `> 20 faults / 60s` auto-quarantine (`FaultKindQuarantined`). *(HTTP `502 extension_execution_fault` envelope was verified in `TP-IMP-0015` / `internal/ingress/http`.)*
- **`FMEA-06` (`TestFMEA06DirtyMigrationExit31`):** Confirms dirty/failed schema migration halts boot fail-closed with exit code `31`.
- **`FMEA-07` (`TestFMEA07ClusterRevocationPartitionStaleBreaker`):** Confirms >10s Valkey/control-plane partition triggers fail-closed blind-node shedding until sync resumes.
- **`FMEA-08` (`TestFMEA08OAuthSingleFlightAndInvalidGrantHalt`):** Verifies OAuth 2.1 single-flight refresh serialization and immediate halt (`AUTH-10`) on `invalid_grant`.

### 4.2. Timed Recovery Drills (`DR-01..04` & Live `PG16` + `Valkey`)
- **`DR-01` (SQLite corruption backup/restore):** **`36.29 ms`** (`CGO_ENABLED=0`) / **`141.4 ms`** (`-race`), `audit_events=1` verified.
- **`DR-02` (Orphan hold reconciliation + `verify-state`):** **`30.18 ms`** (`CGO_ENABLED=0`) / **`235.6 ms`** (`-race`); live PG16 + 3-shard Valkey Cluster `tanpopo admin verify-state`: **`278 ms`** (`{"status":"ok","storage_engine":"postgres","audit_chain_checked":true,"audit_chain_valid":true,"audit_events_verified":11,"orphans_reconciled":0,"valkey_rebuilt":true,"valkey_holds_restored":0}`).
- **`DR-03` (Emergency `Master KEK` online rotation & cache eviction):** **`43.66 ms`** (`CGO_ENABLED=0`) / **`66.4 ms`** (`-race`), epoch `3` active.
- **`DR-04` (`< 60s` N-1 Expand-Only & extension rollback):** **`4.50 ms`** (`CGO_ENABLED=0`) / **`113.9 ms`** (`-race`).
- **`OPS-AC-06` (3-node rolling restart):** In-process 3-node graceful drain test completed **`100 / 100`** active SSE streams with **`0`** `502/503` drops; multi-container Docker `k6` simulation reached **`1,406 / 1,410` (`99.71%`)** `[DONE]` with **`4`** pre-connect socket errors in the `40ms` `/readyz` LB polling gap due to absent Kubernetes `preStop` delay.
