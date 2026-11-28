# 08.02 — Functional Domain Research: Routing, Reliability, AI FinOps & Caching/Performance (`DOM-04` .. `DOM-07`)

- **Document ID:** `TEEP-01-DOC-08-02`
- **Domains Covered:** `DOM-04` (Routing & Orchestration), `DOM-05` (Reliability Engineering), `DOM-06` (AI FinOps), `DOM-07` (Caching & Performance)
- **Candidate Capabilities:** `CAP-023`..`CAP-052`

---

## 1. `DOM-04` — Routing and Orchestration (`CAP-023` .. `CAP-030`)

### 1.1 RC9 Baseline vs Enterprise Routing Gaps (`[VERIFIED_FACT]`)
- **Keep Core (`CAP-023`):** Deterministic 6-step `Resolver.Resolve` (`internal/routing/resolver/resolver.go`), SWRR weights, monotonic `X-Tanpopo-Data-Residency` narrowing, and `RequireTestedCaps` gate.
- **High-Leverage Additions & Fixes:**
  1. **Dual-Mode Prefix / KV-Cache Affinity Routing (`CAP-024`, `INN-07`):** Naive SWRR scatters multi-turn conversations across connections/pods, destroying `50–97.5%` vendor prompt-cache read discounts and `vLLM`/`llm-d` KV locality. Add `PREFIX_CACHE_AFFINITY` using keyed in-RAM `SipHash` (`prefix_hash_64`, `0` prompt persistence) + **Bounded-Load Consistent Hashing (`c = 1.25`)** + optional upstream `prompt_cache_key` / `GAIE` session-affinity header injection.
  2. **TTFT / TPOT SLO-Aware & Multi-Objective Scoring (`CAP-025`):** RC9's `LOWEST_LATENCY` tracks total completion duration (penalizing long reasoning outputs!). Track separate EWMA **Time-To-First-Token (`TTFT`)** and **Time-Per-Output-Token (`TPOT`)** histograms (`:nitro` throughput, `:floor` effective cache-adjusted cost, `:exacto` tool-call success rate, inspired by OpenRouter's multi-signal routing without SaaS lock-in).
  3. **Conditional Header, Canary Split, Shadow Mirroring & Complexity Tiers (`CAP-026`):** Support CEL/header traffic splits (`5%` staging canary), budget-capped async `1%` shadow mirroring (`Drop-Response` for `INN-02` model migrations), and deterministic token-length/modality complexity routing without requiring Plano-style GPU orchestrator models.
  4. **Sandboxed Custom `ROUTING_STRATEGY` Plugins (`CAP-027`):** Pure-function Tier-1/Tier-2 Wasm candidate scorers (`<= 1 ms`, `0` network calls) running strictly after Core residency/trust pruning.
  5. **Route Explain Equivalence, Regional UI Controls & Tiered Capability Warnings (`CAP-028`, `DEF-027`, `DEF-028`, `DEF-050`):** Expose `allowed_data_regions` in `RoutingPage.tsx`, partition capability warnings by `Primary` vs `Fallback #k`, render full deployment provenance on saved target rows, and prove `Explain == Select` parity.
  6. **Cross-Cloud Burst & Sovereign Fallback Orchestration (`CAP-029`, `CAP-030`):** Overflow from internal `ON_PREM` `llm-d`/`vLLM` pools (or exhausted Azure `PTU`s) to compliant `DataZone`/`Bedrock` cloud capacity **only when** effective `AllowedDataRegions` permits, with automatic pre-request `chat -> responses` dialect adaptation.

---

## 2. `DOM-05` — Reliability Engineering (`CAP-031` .. `CAP-036`)

1. **Cluster-Coordinated `SM-04` Circuit Breakers & Semantic Stall Detection (`CAP-031`):** Share breaker state (`CIRCUIT_OPEN_UNHEALTHY`, `THROTTLED_COOLDOWN` `Retry-After`) across pods via Valkey Pub/Sub + EWMA gossip, and add semantic generation probes detecting silent `SGLang`/`vLLM` NIXL hangs where `/health` still returns `200 OK` (`sglang#51980` `[VERIFIED_FACT]`).
2. **Wire Per-Route, Per-Stage & Header Deadlines (`CAP-032`):** Fix RC9's dead `route_policies.request_timeout_ms`, `X-Tanpopo-Timeout-Ms`, and `failover.Config.RequestDeadline` (`ENUM_OR_CONTRACT_ONLY` in RC9!), adding distinct **TTFT timeout** (`ttft_timeout_ms`), **inter-chunk stall timeout** (`idle_chunk_timeout_ms`), and **total request deadline**.
3. **Hierarchical Aggregate Bulkheads & `Gradient2` Adaptive Concurrency (`CAP-033`):** Add shared `ORG` / `ORG_UNIT` / `PROJECT` Valkey Lua rate/token/byte buckets above per-key limits + `Gradient2` RTT-inflation load shedding by `priority_class` (`MISSION_CRITICAL > INTERACTIVE > BATCH_BACKFILL`).
4. **FinOps-Safe Pre-First-Token Speculative Hedging (`CAP-034`, `INN-07`):** Opt-in backup dispatch on tail-TTFT stalls (`p95 * 1.5`) gated on atomic dual pre-hold (`hold(Primary) + hold(Hedge)`), locking on first SSE byte and canceling the loser in `< 1 ms`.
5. **Valkey-Accelerated Idempotency & Graceful Zero-Drop Drain (`CAP-035`, `CAP-036`):** Move `Idempotency-Key` hot locks from SQLite/PG to Valkey `{<org_id>}` with SQL fallback, and enforce `preStop` readiness fencing + SSE stream draining to eliminate RC9's `1/575` (`0.17%`) rolling-restart drop (`COND-PO-01`).

---

## 3. `DOM-06` — AI FinOps (`CAP-037` .. `CAP-045`)

### 3.1 Preserving Exact Integer Math While Unlocking Enterprise Scale & Chargeback
1. **Two-Phase `u128` Integer Settlement Kernel (`CAP-037` — `KEEP`):** Preserve zero-float `Reserve -> DeltaReserve -> Settle/Release` + `90s` orphan reaper (`SM-07`).
2. **Hierarchical Escrow-Backed Pod Budget Leases (`CAP-038`, `INN-06`, `[DESIGN_PROPOSAL — TEEP-03]`):** Reduce per-request SQL lock contention by leasing pre-escrowed slices into pod RAM (`~30 ns` `[HYPOTHESIS]` `atomic.Int64` above `10%` Low-Water Mark; Valkey Lua cutover below `10%` to preserve configured hard budget ceilings).
3. **Extended Budget Scopes, Delete Lifecycle & Burn-Rate Forecasting (`CAP-039`):** Add `ENTERPRISE`, `ORG_UNIT` (`BU`/`DEPT`/`TEAM`), and `PRINCIPAL`/`USER` budgets alongside `ORG`/`PROJECT`/`ENV`/`VIRTUAL_KEY`; add `DELETE /admin/v1/budgets/{id}` and configurable `soft_warning_percents` edit in UI; and add EWMA spend-velocity anomaly alerts (`ALT-BUDGET-BURN-SPIKE`).
4. **Automatic Period Rollover Cron & Unified Half-Open Attribution (`CAP-040`, `DEF-078`):** Background rollover worker automatically minting next `DAILY`/`MONTHLY`/`QUARTERLY` `budget_ledgers` rows (`carrying negative_carryover_micros`), stamping `billing_period_ref_ms` on `usage_settlements`, and enforcing half-open `[fromMs, toMs)` across both Budgets and Chargeback (`+` required `from`/`to` date-range pickers in `FinOpsPage.tsx`).
5. **Multi-Dimensional High-Precision `RateCard` & Self-Hosted GPU Amortization (`CAP-041`, `[DESIGN_PROPOSAL — TEEP-03]`):** Candidate `nano_usd_per_1m_tokens` across `5m`/`1h` cache writes, context cliffs, batch/priority/flex tiers, search queries, and GPU node/PTU hourly amortization.
6. **Wired Streaming/Hourly Usage Rollups (`CAP-042`, `DEF-078`):** Schedule `rollup.Aggregator` in `serve.go` (expanding `usage_rollups_hourly` with `exact_request_count`, `estimated_request_count`, `env_id`, `virtual_key_id`, `cost_center`) so `/usage/summary` and `/usage/export` execute in `< 20 ms` (`HYPOTHESIS / UNVERIFIED`) without scanning raw `usage_settlements`.
7. **Cost-Center Tag Governance, Internal Chargeback Markups & `FOCUS 1.1` Export (`CAP-043`, `DEF-077`):** Mandatory/inherited `cost_center` & `attribution_tags_json`, `platform_fee_micros` basis-point markups separate from `provider_cost_micros`, and native **FinOps `FOCUS 1.1`** CSV/JSONL/Parquet exports.
8. **Cryptographic Chargeback Receipts & Out-of-Band Vendor Invoice Reconciliation (`CAP-044`, `CAP-045`, `INN-06`):** Ed25519-signed JCS monthly project receipts (`tanpopo finops verify-invoice`) plus optional reconciliation connectors polling OpenAI `/v1/organization/costs`, AWS Cost Explorer, and Azure Cost Management to flag billing drift.

---

## 4. `DOM-07` — Caching and Performance (`CAP-046` .. `CAP-052`)

1. **Epoch-Fenced Keep-Alive & HTTP/2 Upstream Connection Pooling (`CAP-046`):** Replace `DisableKeepAlives: true` in `blind_broker.go:885` with pooled transports guarded by `~200 ns` pre-write `ConfigSnapshot` + peer `netip.Addr` verification and instant `CloseIdleConnections()` on revocation.
2. **Split SQLite Read/Write Pools & Group-Commit WAL / `pgx.CopyFrom` Batcher (`CAP-047`):** Separate `readerDB` (`MaxOpenConns = NumCPU`) from `writerDB` (`1`) and coalesce settlements/traces into `5–10 ms` micro-batches (`1 fsync` per `256` calls).
3. **Incremental `ConfigSnapshot` Epoch Delta Sync (`CAP-048`, `ARCH-OBS-01`):** Reload only mutated `(org_id, entity_kind, entity_id)` rows (`WHERE config_epoch > last_epoch`), eliminating the `7 * N_orgs` cluster thundering herd.
4. **Two-Tier (`RAM TinyLFU + Valkey`) Tenant-Sealed Exact Cache & Console Controls (`CAP-049`):** Move `ExactCache` (`exact_cache.go`) off synchronous SQL onto in-RAM + Valkey `{<org_id>}` sealed under `Project DEK`, extend to `/v1/responses` & `/v1/embeddings`, honor `X-Tanpopo-No-Cache`, and expose `response_cache_*` fields in `RoutingPage.tsx`.
5. **Opt-In Tenant-Isolated Semantic Cache via `CACHE_STRATEGY` (`CAP-050`):** Support local quantized ONNX embedding / Valkey Vector similarity caching where the Host forcibly prepends `(org_id, project_id, env_id, policy_rev, model_family, tools_hash)` and encrypts payloads under `Project DEK` (`0` cross-tenant leakage).
6. **Automatic Upstream Prompt-Cache Optimizer (`CAP-051`):** Adapter-level auto-placement of Anthropic 4-slot `cache_control` (`5m`/`1h`), Bedrock `cachePoint`, and OpenAI `prompt_cache_key` + deterministic `tools` key sorting so multi-turn agent loops achieve `90%+` upstream cache hit rates without app rewrites.
7. **Hot-Path Zero-Allocation Buffer Pooling (`CAP-052`):** Eliminate the `88–95 allocs/op` (`11.4 KiB/op`) measured in `ResolveRoute` & `Declarative` via `sync.Pool` request arenas and flat slice indexing.
