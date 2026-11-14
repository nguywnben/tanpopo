# Reliability Targets, Non-Functional SLOs, Overload Control & Deep FMEA — Tanpopo

> **Canonical Reliability & SRE Engineering Specification (Synchronized in `TP-IMP-0002` — Spec Closure Batch B)**
> - **Document ID:** `SPEC-QA-03` | **Version:** `0.2.0-m0-closure` | **Last Updated:** `2026-11-14`
> - **Authoring Role:** Principal SRE, Performance Engineer & Resilience Architect
> - **Measurement Honesty Discipline (`COND-PO-01`):** Zero production runtime code or live load benchmarks have executed yet. **Every `SLO`, latency (`< 1.0 ms`, `< 2.0 ms`, `< 5.0 ms`, `<= 15 ms`, `< 500 ms`), throughput (`500 RPS`, `1,000` streams), memory (`< 128 MiB`, `16 MiB`), propagation (`< 1.0 s`, `< 5.0 s`), and recovery (`< 15 min`) figure in this document is strictly `TARGET / UNVERIFIED`** until empirical benchmark evidence from `SPIKE-01`..`04` (`TP-IMP-0004`) and `M5` (`TP-IMP-0043`) is recorded.
> - **Requirements Traceability:** `TP-NFR-0101`..`1002`, `TP-AC-N01`..`10`, `OPS-AC-01`..`12`, `ARCH-SYS-01`, `ARCH-OPS-01`, `ADR-0001`..`0006`, `CR-05`..`10`, `GAP-13`.

---

## 1. Service Level Objectives (`SLOs`), Gateway Overhead & Capacity Targets (`TARGET / UNVERIFIED`)

| Target ID | Measured Service Level Indicator (`SLI` — Canonical Prometheus Metrics) | Proposed Target Threshold | Verification Status | Automated Verification Method (`M0 Spikes / Stage 4 CI`) |
|---|---|---|---|---|
| **`SLO-01`** | **Data-Plane Internal Availability** — Fraction of valid requests on `tanpopo_http_requests_total` free of internal Tanpopo `5xx` faults (excluding upstream AI vendor `429`/`5xx` errors). | **`>= 99.9%` / month** *(Error budget: `43.2 min/month`)* | **`TARGET / UNVERIFIED`** | Continuous tracking of internal `status_class="5xx"` ratio on `tanpopo_http_requests_total` + `24h` soak testing. |
| **`SLO-02`** | **Added Gateway Proxy Overhead (`tanpopo_proxy_overhead_seconds`, excluding upstream LLM time):**<br/>- `Tier-1 Declarative` path<br/>- `Tier-2 wazero Wasm` path | - Tier 1: **`p95 < 2.0 ms`**, **`p99 < 5.0 ms`**<br/>- Tier 2: **`p95 < 5.0 ms`**, **`p99 < 12.0 ms`** | **`TARGET / UNVERIFIED`** | `k6` driving `500 RPS` against `FakeLLMServer` (`0 ms` upstream delay) on `2 vCPU / 2 GiB RAM` (`SPIKE-01`, `SPIKE-02`). |
| **`SLO-03`** | **Per-Chunk SSE Forwarding (`tanpopo_sse_chunk_relay_seconds`) & Cancel Propagation:**<br/>- Gateway relay delta per `data:` frame<br/>- Upstream socket close latency upon client cancel | - SSE chunk: **`p95 < 1.0 ms`**, **`p99 < 2.5 ms`** *(Wasm hook ceiling `<= 5.0 ms`)*<br/>- Upstream cancel: **`< 500 ms`** | **`TARGET / UNVERIFIED`** | Timestamp delta between `FakeLLMServer` and client across `1,000` concurrent SSE streams (`CT-07`, `CT-11`). |
| **`SLO-04`** | **Internal Error Rate Under Rated Load** | **`< 0.05%`** | **`TARGET / UNVERIFIED`** | `30 min` load run interleaved with `50` config mutations/sec. |
| **`SLO-05`** | **Cold-Start Time & Idle Memory (`tanpopo serve`):**<br/>- Process launch to `/readyz = 200`<br/>- Standalone idle `RSS` | - Cold start: **`< 3.0 s`**<br/>- Idle RSS: **`< 128 MiB`** | **`TARGET / UNVERIFIED`** | Automated measurement in clean Linux/macOS/Windows environments (`OPS-AC-01`). |
| **`SLO-06`** | **Single-Node Rated Capacity (`2 vCPU / 2 GiB RAM`)** | **`>= 1,000` concurrent SSE streams** + **`>= 500 RPS` short requests** | **`TARGET / UNVERIFIED`** | Step-load `k6` profile asserting zero goroutine leaks (`tanpopo_active_streams`). |
| **`SLO-07`** | **Emergency Revocation / Quarantine Propagation (`tanpopo_config_snapshot_age_seconds`)** | - Single-node (`v0.1`): **`< 1.0 s`**<br/>- Multi-node HA (`v0.3`): **`p99 < 5.0 s`** | **`TARGET / UNVERIFIED`** | `AUTH-06` & `OPS-AC-03` continuous probe during `Emergency Revoke`. |
| **`SLO-08`** | **Disaster Recovery & Restoration (`MTTR / RPO / RTO`)** | - `MTTR`: **`< 15 min`** · `RTO`: **`< 15 min`**<br/>- `RPO`: **`<= 15 min`** (SQLite `.tpop.enc`) / **`<= 1 min`** (Postgres WAL) | **`TARGET / UNVERIFIED`** | Timed automated drills `DR-01`..`04` (`OPS-AC-07`). |

---

## 2. Overload Control, Lock-Free Snapshots & `300s` Graceful Reasoning Drain

1. **Multi-Tier Rate Limiting & Protective Load Shedding:**
   - Enforces `RPM`, `TPM`, and `Max Concurrent Streams` across `Virtual Key`, `Environment`, `Project`, and `Organization` scopes (`tp:v1:{<org_id>}:rl:*` hash-tags in HA mode).
   - **Protective Shedding:** When node open streams reach **`90%`** of ceiling or Go heap reaches **`85%`** of `GOMEMLIMIT`, ingress sheds excess low-priority arrivals immediately with **`503 gateway_overloaded`** and `Retry-After: 2` (with jitter), protecting `100%` of in-flight SSE streams and keeping `/healthz` responsive.
   - **Non-Blocking OTLP Export (`32 MiB` Ring Buffer):** When an external OTel collector stalls, `tanpopo` drops oldest spans (`Drop-Oldest`) and increments **`tanpopo_telemetry_dropped_total`** — never blocking inference traffic (`RSK-015`).
2. **Optimistic Concurrency & Lock-Free Hot-Path Reads:**
   - Concurrent `/admin/v1/*` writers presenting a stale `If-Match` ETag receive **`409 optimistic_lock_conflict`** (with `error.current_resource`).
   - Data Plane reads active configuration via `atomic.Pointer[ConfigSnapshot]` (`0` mutex contention, `0` DB queries on the hot path).
3. **Graceful `SIGTERM` Drain Configurable up to `300s` & HA Chaos Matrix (`GAP-13`):**
   - Upon `SIGTERM`, `tanpopo` immediately flips `/readyz` to `503 draining`, holds open SSE streams until natural completion or **`server.drain_timeout`** (default `90s`, **configurable up to `300s`** so long-horizon `reasoning` model streams finish cleanly without `502` cutoffs — `OPS-AC-06`), settles budget holds, and exits `0`.
   - **HA Chaos Scenarios (`v0.3`):** (a) Abrupt `Valkey` kill -> degrades to `PG LISTEN/NOTIFY`, per-node rate slices, and PostgreSQL holds guarded by `valkey_hold_checkpoints` (`CR-07`); (b) `PostgreSQL Primary` failover -> Data Plane continues reading in-RAM `ConfigSnapshot`; (c) Abrupt `SIGKILL` of `1` of `3` `tanpopo` replicas -> remaining `2` replicas absorb new traffic while `OrphanReservationReaper` reclaims stalled `STREAMING_ACCRUING` holds (`SM-07`).

---

## 3. Deep Failure Mode & Effects Analysis (`FMEA-01 .. FMEA-08`)

### `FMEA-01` — Mid-Stream Disconnect & Post-First-Chunk Failover Prohibition
- **Before first content chunk (`k = 0`):** Pre-stream failover is permitted across up to `1 + max_failover_hops` (`<= 4`) candidates (`SM-04`).
- **After flushing `k >= 1` content/tool-call chunks:** **ALL retries and fallback transitions are STRICTLY PROHIBITED.** Emits terminal SSE `event: error` (`mid_stream_abort`, `request_id`), closes stream cleanly, and settles hold as **`SETTLED_ESTIMATED`**.
- **Verification:** `CT-10`, `EXT-ACCEPT-01` (Step 6).

### `FMEA-02` — Retrying MCP Tool Calls with External Side Effects (`CR-08`)
- Tools declare `risk_tier` in `{ read_only, mutating, destructive }` (unclassified defaults fail-closed to **`mutating`**).
- **`mutating` and `destructive` tools enforce `0` automatic retries** once the first request byte leaves `tanpopo`, propagating `Idempotency-Key` and returning **`504 ambiguous_side_effect_timeout`** on socket drop/timeout.
- `destructive` HITL approvals bind a **`Single-Use Nonce`** to the canonical **5-tuple RFC 8785 JCS digest**:
  `SHA-256(JCS({org_id, virtual_key_id, server_slug, tool_name, arguments}))`.
- **Verification:** `CT-22`, `CT-36` (`COND-PO-04`).

### `FMEA-03` — Opt-In Response Cache Privacy Bleed (`Scheduled Could — v0.3+` per `CR-09`)
- **Phasing Note (`CR-09`):** Response caching is omitted from `v0.1`–`v0.2` and formally scheduled as an opt-in `Could` capability in **`v0.3+`** (`TP-IMP-0041`).
- **Mandatory Activation Gate When Enabled in `v0.3+`:** Disabled by default; partitioned strictly by `HMAC-SHA256(project_cache_salt, org_id || project_id || env_id || deployment_id || canonical_request || policy_hash || allowed_tools_hash)`; bypassed on `temperature > 0` (unless overridden), `X-Tanpopo-No-Cache: true`, `mid_stream_abort`, or PII guardrail hits; encrypted under `Project DEK` / `Org DEK` for instant crypto-shredding.
- **Verification:** `CT-28`.

### `FMEA-04` — Missing or Malformed Upstream Usage (`Separate Text & Media Fallbacks`)
- **Recording `SETTLED_EXACT = 0` on non-empty output lacking upstream `usage` is strictly prohibited.**
- Applies a 3-tier fallback ladder: (1) Model tokenizer + `Deployment` integer `nano-USD` rate card -> **`SETTLED_ESTIMATED`**; (2) Text-only UTF-8 byte bound `ceil(text_utf8_bytes / 3)` (excluding `BlobRef` binary bytes) + `BlobRef` media unit schedule -> **`SETTLED_ESTIMATED`**; (3) Missing rate card -> **`UNMETERED_FLAGGED`** (`ALT-UNMETERED-SPEND`).
- **Verification:** `CT-12`, `FZ-06`.

### `FMEA-05` — Extension Crash, Infinite Loop or `16 MiB` OOM (`Canonical Sandbox Constants`)
- **Hazard:** Faulty `.wasm` or `Sidecar` crashing the `tanpopo` process or starving co-hosted providers.
- **Canonical Invariant Rules (`CR-05`, `GAP-13`):**
  1. Every Tier-2 `wazero` invocation runs inside a boundary `recover` guard, a hard **`16 MiB`** linear memory ceiling (`> 64 KiB` blobs externalized via `BlobRef`), and hard deadlines of **`<= 15 ms`** per request/response/probe/error hook and **`<= 5 ms`** per stream chunk (`< 1.0 ms p95` target — `TARGET / UNVERIFIED`).
  2. Any trap, deadline expiry, or OOM **destroys that Wasm instance immediately (`0` warm-pool reuse)**, increments `tanpopo_sandbox_traps_total`, and returns **`502 extension_execution_fault`** (permitting pre-stream failover to a healthy backup adapter).
  3. Exceeding **`> 20` faults / `60s`** trips `ALT-PLUGIN-TRAP` and auto-transitions the extension to **`QUARANTINED`** (`SM-01`).
- **Verification:** `CT-17`, `FZ-05`, `SPIKE-02`.

### `FMEA-06` — Mid-Flight Schema Upgrade or `tanpopo db migrate-engine` Failure
- Enforces two-phase **`Expand -> Contract`** additive SQL migrations wrapped in pre-migration `.tpop.enc` backup + `pg_advisory_lock`.
- If a migration fails leaving `dirty = true`, `tanpopo serve` halts immediately with **`exit 31`** and explicit recovery steps.
- `tanpopo db migrate-engine` (`SQLite WAL -> PostgreSQL 16+`) pre-creates monthly partitions for historical timestamps and verifies `100%` row counts, `micro-USD` sums, `Per-Org + SYSTEM` JCS audit hash chains, and `AEAD AAD` unsealing prior to `tanpopo admin unfreeze-writes`.
- **Verification:** `CT-24`, `CT-32`, `OPS-AC-05`, `OPS-AC-06`.

### `FMEA-07` — Cluster Revocation Partition (`Stale-Node Fail-Closed Breaker` — `ADR-0006`)
- Propagates revocations across **3 channels**: (1) `Valkey Pub/Sub`, (2) `PostgreSQL LISTEN/NOTIFY`, and (3) `2s` `config_changelog` polling.
- If `tanpopo_config_snapshot_age_seconds` exceeds **`max_stale_revocation_window = 10s`** (`TARGET / UNVERIFIED`), `/readyz` flips immediately to **`503 stale_security_state`** to drain load-balancer traffic and fails closed on sensitive operations.
- **Verification:** `AUTH-06`, `OPS-AC-03`, `OPS-AC-04`.

### `FMEA-08` — Concurrent `OAuth 2.1` Refresh Stampede or Upstream Revocation
- Renews tokens proactively at `80% TTL` and serializes expired-token refreshes behind a **`Single-Flight` lock** (`<= 5s` waiter bound `TARGET / UNVERIFIED`, backed by `oauth_authorization_sessions` / Valkey in HA mode).
- On deterministic `400 invalid_grant` / `401`, halts retries immediately (`0` retry storm), transitions to `AUTH_EXPIRED_REAUTH_REQUIRED` (`SM-02` / `SM-03`), and fires **`ALT-OAUTH-EXPIRY`** (`INC-05`).
- **Verification:** `CT-19`, `AUTH-10`.
