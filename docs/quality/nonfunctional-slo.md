# Reliability Targets, Non-Functional SLOs, Overload Control & Deep FMEA — Tanpopo

> **Reliability & SRE Engineering Specification (Phase ASTRA A07 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — NO CODE`)**
> - **Document ID:** `SPEC-QA-03` | **Version:** `0.1.0-step07-draft` | **Last Updated:** `2026-11-12`
> - **Authoring Role:** Principal SRE, Performance Engineer & Resilience Architect
> - **Measurement Honesty Discipline:** Through the completion of phase **A07**, **zero production runtime code has been written and zero live load benchmarks have been executed**. Accordingly, **every latency, throughput, memory footprint, `MTTR`, and `SLO` figure in this document is strictly labeled `TARGET / UNVERIFIED`** and may transition to `VERIFIED` only after automated benchmark evidence from `SPIKE-01`..`04` and `Stage 4 CI` is recorded.
> - **Requirements Traceability:** `TP-NFR-0101`..`1002`, `TP-AC-N01`..`10`, `OPS-AC-01`..`12`, `ARCH-SYS-01`, `ARCH-OPS-01`, `ADR-0001`..`0006`.

---

## 1. Service Level Objectives (`SLOs`), Gateway Overhead & Capacity Targets (`TARGET / UNVERIFIED`)

| Target ID | Measured Service Level Indicator (`SLI`) | Proposed Commitment Threshold | Verification Status | Automated Verification Method (`Stage 4 CI / PoC`) |
|---|---|---|---|---|
| **`SLO-01`** | **Data-Plane Internal Availability** — Fraction of valid inference requests free of internal Tanpopo `500` faults (excluding upstream AI vendor `429/5xx` responses). | **`>= 99.9%` / month** *(Error budget: `43.2 min/month`)* | **`TARGET / UNVERIFIED`** | Continuous monitoring of `tanpopo_gateway_internal_errors_total / tanpopo_requests_total` plus `24h` soak load testing. |
| **`SLO-02`** | **Added Gateway Proxy Overhead (excluding upstream LLM wait time):**<br/>- `Tier 1 Declarative` path<br/>- `Tier 2 Wasm Sandbox` path | - Tier 1: **`p95 < 2.0 ms`**, **`p99 < 5.0 ms`**<br/>- Tier 2: **`p95 < 5.0 ms`**, **`p99 < 12.0 ms`** | **`TARGET / UNVERIFIED`** | `k6` driving `500 RPS` against `FakeLLMServer` (`0 ms` upstream delay) on a `2 vCPU / 2 GiB RAM` baseline runner (`SPIKE-01`, `SPIKE-02`). |
| **`SLO-03`** | **Per-Chunk `SSE` Forwarding Latency & Cancellation Propagation:**<br/>- Gateway forwarding delta per `data:` frame<br/>- Upstream socket close latency upon client cancel | - SSE chunk: **`p95 < 1.0 ms`**, **`p99 < 2.5 ms`**<br/>- Upstream cancel: **`< 500 ms`** | **`TARGET / UNVERIFIED`** | Nanosecond timestamp delta between `FakeLLMServer` and client reader across `1,000` concurrent open streams (`CT-07`, `CT-11`). |
| **`SLO-04`** | **Internal Error Rate Under Rated Load** | **`< 0.05%`** | **`TARGET / UNVERIFIED`** | Load test interleaved with continuous configuration mutations over `30 min`. |
| **`SLO-05`** | **Cold-Start Time & Idle Memory Footprint (`Cold Start & Idle RSS`):**<br/>- Process launch to `/readyz = 200`<br/>- Idle `RSS` memory (single-node standalone) | - Cold start: **`< 3.0 s`**<br/>- Idle RSS: **`< 128 MiB`** | **`TARGET / UNVERIFIED`** | Automated measurement inside a clean resource-capped Linux container (`OPS-AC-01`). |
| **`SLO-06`** | **Single-Node Rated Capacity (`2 vCPU / 2 GiB RAM`)** | **`>= 1,000` concurrent SSE streams** + **`>= 500 RPS` short requests** | **`TARGET / UNVERIFIED`** | Step-load `k6` profile asserting zero goroutine leaks and bounded non-linear heap growth. |
| **`SLO-07`** | **Key Revocation / Plugin Quarantine Propagation Latency (`Revocation SLA`)** | - Single-node: **`< 1.0 s`**<br/>- Multi-node cluster: **`p99 < 5.0 s`** | **`TARGET / UNVERIFIED`** | `AUTH-06` & `OPS-AC-03` continuous request probe while firing `Emergency Revoke`. |
| **`SLO-08`** | **Disaster Recovery & Incident Restoration (`MTTR / RPO / RTO`)** | - `MTTR`: **`< 15 min`**<br/>- `RTO`: **`< 15 min`**<br/>- `RPO`: **`<= 15 min`** (SQLite backup) / **`<= 1 min`** (Postgres WAL) | **`TARGET / UNVERIFIED`** | Timed automated drills across all 4 runbooks `DR-01`..`04` on clean infrastructure (`OPS-AC-07`). |

---

## 2. Overload Control, Concurrent Config Mutations, Backups, Upgrades & HA Cluster Resilience

1. **Multi-Tier Rate Limiting & Proactive Load Shedding (`Rate Limiting & Graceful Load Shedding`):**
   - Simultaneously enforces token-bucket/sliding-window limits across `Virtual Key`, `Project`, and `Organization` scopes (`RPM`, estimated input `TPM`, and `Max Concurrent Streams`).
   - **Protective Load Shedding:** When active open streams on a node reach **`90%` of its configured ceiling** or Go heap usage reaches **`85%` of `GOMEMLIMIT`**, the ingress sheds newly arriving low-priority requests immediately at the edge with **`503 gateway_overloaded`** and `Retry-After: 2` (with randomized jitter to prevent thundering-herd retries), preserving `100%` of in-flight SSE streams and keeping `/healthz` safe from `OOMKilled`.
   - When the outbound `OpenTelemetry` export buffer hits its `32 MiB` cap due to a dead external collector, Tanpopo drops the oldest buffered spans (`Drop-Oldest`) and increments `tanpopo_telemetry_dropped_total` — **never blocking inference traffic due to observability backend outages (`RSK-015`)**.
2. **Concurrent Configuration Mutations Under Heavy Load:**
   - Two administrators editing the same `Route` or `Policy` with a stale `If-Match` ETag immediately receive `409 conflict_version_mismatch`.
   - On the Data-Plane read path, active configuration is published via an immutable `atomic.Pointer[ConfigSnapshot]`: stress tests firing `50` route updates/sec alongside `1,000 RPS` inference traffic must observe **`0` mutex contention and `0` torn partial-config reads**.
3. **Backup/Restore Drills, Migration Safety & Zero-Downtime Upgrades (`Startup / Upgrade / Rollback / HA`):**
   - **Graceful Startup & Drain:** Upon `SIGTERM`, a node immediately flips `/readyz` to `503 draining` (stopping load-balancer new connections), keeps active SSE streams alive until natural completion or `drain_timeout` (default `60s`), settles remaining budget holds, and exits cleanly (`exit 0`).
   - **Multi-Replica HA Chaos Matrix:** Fault-injection tests validate 3 cluster failure scenarios: (a) Abrupt `Valkey` kill → nodes degrade gracefully to local per-node quota slices and `PG LISTEN/NOTIFY` without dropping inference requests; (b) `PostgreSQL Primary` failover → connection pool reconnects within seconds while the Data Plane continues serving reads from in-RAM `ConfigSnapshot`; (c) Abrupt `SIGKILL` of `1` of `3` `tanpopod` replicas mid-stream → remaining `2` replicas absorb all new traffic seamlessly.

---

## 3. Deep Failure Mode & Effects Analysis (`FMEA-01 .. FMEA-08`)

The analysis below locks down deterministic runtime behavior across the **8 most hazardous production failure modes** — leaving zero ambiguity for engineers during implementation:

### `FMEA-01` — Mid-Stream Disconnect & Post-First-Chunk Failover Prohibition (`Partial Stream + Fallback`)
- **Trigger Scenario:** Primary upstream deployment returns `HTTP 200 text/event-stream`, flushes `k >= 1` content or tool-call delta chunks to the `API Client`, and subsequently drops the TCP socket or emits an upstream mid-stream error.
- **Hazard if Mishandled:** Silently failing over to a backup provider and appending a fresh completion stream onto the partial stream produces corrupted chimeric text or executes duplicate `tool_calls`.
- **Invariant Handling Rules:**
  1. **Before flushing the first content byte (`k = 0`):** Connection errors, `TTFT` timeouts, `429`, or `5xx` responses **ARE PERMITTED** to fail over to the next candidate in the `Route` (`SM-04`).
  2. **After flushing the first content chunk (`k >= 1`):** **ALL automatic retries and fallback transitions are STRICTLY PROHIBITED.** The gateway emits a canonical terminal SSE error frame (`event: error` carrying `mid_stream_abort`, `request_id`, and flushed chunk count) and closes the HTTP stream cleanly so client SDKs detect the incomplete stream.
- **Settlement & Audit:** Counts actually flushed tokens via the local tokenizer fallback, settles the ledger hold as **`SETTLED_ESTIMATED`** (releasing unused hold remainder), and records a health penalty against the aborted `Deployment`.
- **Mandatory Verification:** `CT-10`, `EXT-ACCEPT-01` (Step 6).

### `FMEA-02` — Retrying Tool Calls with External Side Effects (`Retrying Tool Calls with Side Effects`)
- **Trigger Scenario:** An agent invokes a tool through `MCP Hub` (e.g., `charge_credit_card`, `create_jira_issue`, `delete_cluster`) and the network connection to the MCP server hits a `Read Timeout` or socket reset after request bytes have left Tanpopo's NIC.
- **Hazard if Mishandled:** Naive HTTP proxy retries dispatch the mutation a second time, double-charging customers or deleting unintended resources.
- **Invariant Handling Rules:**
  1. Every MCP tool carries an explicit side-effect classification (`read_only`, `mutating`, `destructive` — unclassified tools default fail-closed to **`mutating`**).
  2. Only `read_only` tools permit at most `1` retry on initial connection failure. For **`mutating` and `destructive` tools, automatic retry count is hard-locked to `0`** once the first request byte is written to the socket.
  3. Tanpopo propagates an `Idempotency-Key` header end-to-end to the MCP server and returns **`504 ambiguous_side_effect_timeout`** instructing the caller to inspect upstream state before deciding whether to re-invoke.
  4. For `destructive` tools, the human `Pre-Approval Token` is cryptographically bound to the `SHA-256` digest of JCS-canonicalized arguments and enforced as a **`Single-Use Nonce`** (exact-once consumption).
- **Mandatory Verification:** `CT-22`.

### `FMEA-03` — Response Cache Privacy Bleed & Cross-Tenant Poisoning (`Response Cache Privacy`)
- **Trigger Scenario:** Response caching (`Exact` or `Semantic Cache`) is enabled on a multi-project gateway; `Project A` submits a prompt semantically matching an earlier `Project B` prompt that carried confidential context or elevated tool permissions.
- **Hazard if Mishandled:** Returning `Project B`'s confidential completion to `Project A` (*Cross-Tenant Cache Bleed*), or caching truncated/PII-tainted completions.
- **Invariant Handling Rules:**
  1. **Disabled by default;** requires explicit opt-in configuration per `Route` / `Project`.
  2. **Zero cache sharing across `Organization`, `Project`, or `Environment` boundaries:** Lookup keys must be derived as `HMAC-SHA256(org_de_salt, org_id || project_id || env_id || deployment_id || canonical_prompt_and_params || policy_version_hash || allowed_tools_hash)`.
  3. Automatically bypasses both cache read and cache write whenever `temperature > 0` (unless explicitly overridden on the route), `Cache-Control: no-store` / `X-Tanpopo-No-Cache: true` is present, dynamic tool outputs are included, the stream aborted (`mid_stream_abort`), or output guardrails detect secrets/PII.
  4. Cached entries are encrypted under `Org DEK` — executing `Crypto-Shredding` instantaneously invalidates the entire tenant's cache.
- **Mandatory Verification:** `CT-28`.

### `FMEA-04` — Missing or Malformed Upstream Token Usage (`Missing or Estimated Provider Usage`)
- **Trigger Scenario:** A self-hosted upstream (`vLLM`, internal adapter) or streaming response finishes without emitting a final `usage` block, or returns negative/malformed token integers.
- **Hazard if Mishandled:** Settling ledger entries at `0 USD` creates an unbounded free-spend loophole, whereas disguising estimates as exact figures corrupts internal financial chargeback reports.
- **Invariant Handling Rules:**
  1. **Recording `SETTLED_EXACT = 0` when non-empty output was generated without upstream `usage` is strictly prohibited.**
  2. Applies a 3-tier fallback ladder: (Tier 1) Count tokens locally using the model's declared tokenizer + `Deployment` pricing table → mark **`SETTLED_ESTIMATED`**; (Tier 2) If tokenizer is unknown, estimate conservatively from UTF-8 byte length (`ceil(bytes / 3)`) → mark **`SETTLED_ESTIMATED`**; (Tier 3) If the `Deployment` has no configured commercial unit price → record estimated token counts with **`UNMETERED_FLAGGED`**.
  3. `FinOps Chargeback` reports separate `Exact Spend`, `Estimated Spend`, and `Unmetered Requests` into distinct columns; if non-exact requests exceed `> 5%` over `15 min`, Tanpopo fires `ALT-UNMETERED-SPEND` (`INC-01`).
- **Mandatory Verification:** `CT-12`, `FZ-06`.

### `FMEA-05` — Extension Crash, Infinite CPU Loop or Memory Exhaustion (`Plugin Crash / CPU / OOM`)
- **Trigger Scenario:** A Tier-2 (`.wasm`) or Tier-3 (`Sidecar`) extension triggers division-by-zero, `wasm unreachable trap`, catastrophic backtracking (*ReDoS*), or per-request memory leaks.
- **Hazard if Mishandled:** Panics the entire `tanpopod` process, starves goroutines serving unrelated providers, or exhausts host RAM.
- **Invariant Handling Rules:**
  1. Every Wasm invocation runs inside an ABI boundary `recover` wrapper, a dedicated linear memory cap (`16 MiB`), and a hard context deadline (`<= 50 ms` per request transform, `<= 5 ms` per SSE chunk — `TARGET / UNVERIFIED`).
  2. Whenever a Wasm instance traps, exceeds its CPU deadline, or hits its memory ceiling, **that instance is destroyed immediately and never returned to the warm `Instance Pool`**; the in-flight request receives `502 extension_execution_fault` (and if zero SSE chunks were flushed, the router may fail over to a healthy backup deployment using another adapter).
  3. If a single extension exceeds its fault threshold (`> 20 faults / 60s`), its dedicated per-extension breaker trips (`ALT-PLUGIN-TRAP`), protecting `100%` of latency on co-hosted providers.
- **Mandatory Verification:** `CT-17`, `FZ-05`, `SPIKE-02`.

### `FMEA-06` — Mid-Flight Schema Upgrade or Database Engine Migration Failure (`Data Migration Failure`)
- **Trigger Scenario:** Power loss or legacy constraint violation occurs mid-SQL migration during version upgrade; or `SQLite` to `PostgreSQL` migration drops network connectivity at table 8 of 15.
- **Hazard if Mishandled:** Leaves the database half-migrated so the new binary refuses to run while rolling back to the previous binary also crashes.
- **Invariant Handling Rules:**
  1. **Two-Phase `Expand -> Contract` Schema Compatibility:** Release `N` performs *additive expand-only* DDL (new tables, nullable/defaulted columns) so binary `N-1` continues reading and writing cleanly against the upgraded schema during emergency rollback; destructive column drops are deferred to `N+1`.
  2. Automatically snapshots a pre-migration backup before applying pending migrations; wraps PostgreSQL DDL steps inside transactional `pg_advisory_lock` guards; if a migration fails leaving `dirty = true`, startup halts immediately (`exit 31`) with explicit recovery instructions rather than serving against corrupted state.
  3. `tanpopo db migrate-engine` (`SQLite -> Postgres`) writes exclusively into an empty target database in transactional batches, verifying `100%` row counts, `micro-USD` ledger sums, `audit_events` hash chains, and sample `AEAD AAD` decryption before cutover is permitted.
- **Mandatory Verification:** `CT-24`, `CT-32`, `OPS-AC-05`, `OPS-AC-06`.

### `FMEA-07` — Cluster Revocation Channel Partition (`Revocation Propagation Failure`)
- **Trigger Scenario:** An administrator triggers emergency revocation of a leaked `Virtual Key` or quarantines a malicious plugin while a `Data Plane` node suffers a transient network partition from both `Valkey Pub/Sub` and `PostgreSQL`.
- **Hazard if Mishandled:** The partitioned node continues serving unbounded requests to an attacker using the revoked key from its stale in-memory snapshot.
- **Invariant Handling Rules (`Stale-Node Fail-Closed Circuit Breaker` — `ADR-0006`):**
  1. Propagates revocations across **3 independent channels**: (1) real-time `Valkey Pub/Sub`, (2) `PostgreSQL LISTEN/NOTIFY`, and (3) direct DB polling of `config_epoch` + `revocation_watermark` every `2s`.
  2. Every node tracks `last_verified_epoch_at`. If a node cannot verify `revocation_watermark` against the source of truth for longer than **`max_stale_revocation_window = 10s`** (`TARGET / UNVERIFIED`), it immediately flips `/readyz` to **`503 stale_security_state`** (causing load balancers to drain traffic away from the partitioned node) and fails closed on sensitive operations rather than serving stale security state indefinitely.
- **Mandatory Verification:** `AUTH-06`, `OPS-AC-03`, `OPS-AC-04`.

### `FMEA-08` — Concurrent `OAuth` Refresh Stampede or Identity Provider Outage (`OAuth Stampede & IdP Outage`)
- **Trigger Scenario:** An `Authorized OAuth 2.1` `Provider Connection` serving `200` RPS hits `access_token` expiry, or the upstream OAuth server returns `503` / revokes the `refresh_token` (`invalid_grant`).
- **Hazard if Mishandled:** All `200` goroutines fire `200` simultaneous token refresh requests, triggering upstream rate-limit bans or invalidating single-use *Rotating Refresh Tokens* through race conditions.
- **Invariant Handling Rules:**
  1. **Proactive Background Refresh at `80% TTL`:** A background worker renews tokens at `80%` lifetime — if the IdP experiences transient `503` errors, production traffic continues unaffected on the remaining `20%` token validity window.
  2. **Single-Flight Refresh Lock:** If a token is already expired, an atomic lock (in-process and distributed on `Valkey` in cluster mode) guarantees **exactly `1` refresh request** reaches the IdP while concurrent callers await the shared result up to `<= 5s`.
  3. Upon a deterministic upstream auth rejection (`400 invalid_grant` / `401`), Tanpopo **halts retries immediately**, transitions the connection to `REAUTH_REQUIRED` (`SM-03`), fires `ALT-OAUTH-REAUTH` (`INC-05`), and allows the router to fail over to any healthy backup deployment.
- **Mandatory Verification:** `CT-19`, `AUTH-10`.
