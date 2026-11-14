# Two-Phase Budget Ledger, Granular Pricing & Production Observability Specification — Tanpopo

> **Canonical FinOps & Observability Specification (Synchronized in `TP-IMP-0002` — Spec Closure Batch B)**
> - **Document ID:** `SPEC-CORE-02` | **Version:** `0.2.0-m0-closure` | **Last Updated:** `2026-11-14`
> - **Authoring Role:** Principal SRE & FinOps/Observability Architect
> - **Aligned Contracts:** [`ADR-0005`](../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md), [`proto/tanpopo/extension/v1/runtime.proto`](../../proto/tanpopo/extension/v1/runtime.proto) (`NormalizedUsage` / `MediaUsage`), [`openapi/dataplane-v1.yaml`](../../openapi/dataplane-v1.yaml) (`/metrics`), and [`monitoring-incidents.md`](../operations/monitoring-incidents.md) (`SPEC-OPS-03`).
> - **Measurement Honesty (`COND-PO-01`):** Every latency (`< 0.05 ms`, `< 1 ms`, `< 2 ms`, `< 5 ms`), revocation sync (`< 5 s`), and series ceiling (`<= 100,000`) figure in this document is **`TARGET / UNVERIFIED`** until empirical verification in `SPIKE-03` and `M5`.
> - **Requirements Traceability:** `TP-FR-0801`..`0806`, `TP-NFR-0101`..`0201`, `TP-NFR-0501`, `TP-BR-0701`..`0703`, `TP-BR-0802`, `SM-07`, `CR-05`, `CR-06`, `CR-07`.

---

## 1. Domain Firewall: Separating Commercial API Spend from Consumer Subscriptions (`DEC-004`)

In strict compliance with `DEC-004` and `REJ-003`, Tanpopo's `FinOps Ledger` models and settles exclusively two technical billing modes (`billing_mode`):
1. **`COMMERCIAL_API`:** Authorized pay-as-you-go commercial API connections (API Keys, Cloud IAM Bedrock/Vertex endpoints, or enterprise gateways with explicit per-token / per-media-unit rate cards).
2. **`SELF_HOSTED_INFRA`:** Self-operated GPU/CPU inference infrastructure (`Ollama`, `vLLM`, `SGLang`), metered at either `0 micro-USD` (`self_hosted_zero_rate = true`) or an administrator-configured internal chargeback rate.

**Unconditionally excluded from schemas & UI:** Tanpopo contains zero data structures for pooling commercial API spend with personal `ChatGPT Plus/Pro`, `Claude Pro/Max`, `Codex`, or `Antigravity` consumer subscriptions.

---

## 2. Integer `Micro-USD` Ledger Mathematics, Multimodal Estimation & Two-Phase Hold (`SM-07`)

### 2.1. Zero-Floating-Point Drift Pricing Arithmetic (`64-Bit Integer Nano/Micro-USD`)

Every rate card on a `Model Deployment` stores unit prices as **64-bit integer `nano-USD` per token or media unit** (`1 USD / 1,000,000 tokens = 1,000 nano-USD / token`; e.g., `\$0.015 / 1M tokens = 15 nano-USD / token`). When settling into `usage_settlements` (`1 USD = 1,000,000 micro-USD`, storing `applied_pricing_snapshot_json` and `media_usage_json` per `CR-06`), Tanpopo applies conservative `ceiling division`:

$$
\text{Cost}_{\text{nano}} = (T_{\text{in}} \cdot P_{\text{in}}) + (T_{\text{out}} \cdot P_{\text{out}}) + (T_{\text{cache\_r}} \cdot P_{\text{cache\_r}}) + (T_{\text{cache\_w}} \cdot P_{\text{cache\_w}}) + (T_{\text{reason}} \cdot P_{\text{reason}}) + C_{\text{media\_nano}}
$$

$$
C_{\text{media\_nano}} = (N_{\text{img}} \cdot P_{\text{img}}) + \lceil S_{\text{audio}} \cdot P_{\text{audio\_sec}} \rceil + (N_{\text{pages}} \cdot P_{\text{page}}) + (N_{\text{rerank}} \cdot P_{\text{rerank}})
$$

$$
\text{Cost}_{\text{micros}} = \left\lceil \frac{\text{Cost}_{\text{nano}}}{1000} \right\rceil = \frac{\text{Cost}_{\text{nano}} + 999}{1000} \quad (\text{64-bit integer division})
$$

### 2.2. Separate Text vs. Multimodal Pre-Hold Estimation & Cluster-Safe Reservation (`CR-05`, `CR-07`)

1. **Phase 1 — Pre-Stream Upper-Bound Reservation (`Pre-Request Hold`):**
   - **Text & Tool Schema Token Heuristic (Excluding Binary Media — `CR-05`):**
     - `est_text_input_tokens = ceil(utf8_byte_len(text_blocks_and_tool_schemas_only) / 3)` (`< 0.05 ms` `TARGET / UNVERIFIED` heuristic). Decoded multimodal payloads (`BlobRef` `> 64 KiB` and small inline media data URIs) are **never** passed into `utf8_byte_len / 3`, eliminating false `2.66M`-token inflation on `6 MiB` images (`AUD-002`).
   - **Multimodal Pre-Hold Schedule (`est_media_nano`):**
     - Computed directly from `BlobRef` / attachment metadata (`image_count`, `audio_input_seconds` bound, `document_pages`, `rerank_search_units`) multiplied by candidate `Deployment` media unit rates.
   - **Output Token Ceiling:**
     - `bound_output_tokens = min(req.max_output_tokens ?? policy.default_hold_output_tokens, deployment.max_output_tokens)` (default `default_hold_output_tokens = 4096`).
   - **Hierarchical Admission Check:**
     - Computes `hold_micros = max_{d in candidates} CostMicros(d, est_text_input_tokens, bound_output_tokens, est_media_nano)`.
     - Atomically verifies headroom across `Virtual Key`, `Environment`, `Project`, and `Organization` (in HA mode using single-slot `Valkey Cluster` hash-tag keys **`tp:v1:{<org_id>}:budget:*`** to prevent `CROSSSLOT` errors, paired with periodic **`valkey_hold_checkpoints`** heartbeats to PostgreSQL for safe cache-loss fallback per `CR-07`):
       `settled_exact_micros + settled_estimated_micros + negative_carryover_micros + reserved_hold_micros + hold_micros <= hard_limit_micros`.
     - Breaching any ceiling immediately rejects with **`429 project_budget_exceeded`** (`execution-baseline.md`) prior to opening upstream sockets. Otherwise, records `RESERVED_HOLD` (`lease_expires_at = now() + 90s`, renewed every `30s` during `STREAMING_ACCRUING`; `OrphanReservationReaper` reclaims both `RESERVED_HOLD` and crashed-pod `STREAMING_ACCRUING` holds to `EXPIRED_RECLAIMED` per `SM-07`).

2. **Phase 2 — Idempotent Post-Stream Settlement & `Negative Balance Carryover` (`BA-OQ-002`, `CR-06`):**
   - Atomically transitions `budget_reservations.state` (`WHERE state IN ('RESERVED_HOLD', 'STREAMING_ACCRUING')`) and inserts into `usage_settlements` (`UNIQUE (request_id, settled_at)`) in a single transaction, releasing `hold_micros` and recording actual spend under **`EXACT`**, **`ESTIMATED`**, or **`UNMETERED_FLAGGED`**.
   - **`Negative Balance Carryover`:** If end-of-stream actual spend slightly exceeds `hold_micros`, Tanpopo finishes the active stream cleanly, records the exact delta in `negative_carryover_micros`, and blocks `100%` of subsequent requests (`429 project_budget_exceeded`) until budget reset or top-up.

---

## 3. Production-Grade Observability (`OpenTelemetry Metrics, Traces & Structured Logs`)

### 3.1. Canonical `RED` & `USE` Prometheus Metrics Registry (`Bounded Cardinality` — `GAP-13`)

> **Bounded Label Allowlist & High-Tenant Toggle (`OPS-AC-12`):**
> - **Permitted Tenant/Domain Labels:** `{ org_id, project_id, adapter_id, logical_model, status_class, error_code, accuracy_tier }` (plus low-cardinality structural enums `plane`, `extension_tier`, `data_region`, `phase`, `state`, `trap_reason`, `sync_channel`, `signal`).
> - **High-Tenant Cardinality Guard:** Operators running large multi-tenant clusters can set `telemetry.metrics.include_tenant_labels = false` to omit `org_id` and `project_id` from Prometheus `/metrics` series while retaining full tenant attribution in OTel Traces, Structured Logs, and `usage_rollups_hourly`, keeping active series strictly `<= 100,000` (`TARGET / UNVERIFIED`).
> - **Unconditionally Banned Labels:** `request_id`, `virtual_key_id`, `principal_id`, `email`, raw URL paths, and free-form error messages.

| Canonical Metric Name | Type | Bounded Labels | On-Call Diagnostic Purpose |
|---|---|---|---|
| `tanpopo_http_requests_total` | Counter | `plane`, `org_id`*, `project_id`*, `logical_model`, `status_class` (`2xx`/`4xx`/`5xx`), `error_code` | Gateway request rate and internal vs. upstream error breakdown (`SLO-01`, `SLO-04`). |
| `tanpopo_proxy_overhead_seconds` | Histogram | `plane`, `extension_tier` (`declarative`/`wasm`/`sidecar`) | Pure internal Tanpopo processing overhead (`SLO-02`: `< 2.0 ms` / `< 5.0 ms` `p95` `TARGET / UNVERIFIED`). |
| `tanpopo_sse_chunk_relay_seconds` | Histogram | `extension_tier` | Per-chunk SSE forwarding delta (`SLO-03`: `< 1.0 ms p95` `TARGET / UNVERIFIED`). |
| `tanpopo_upstream_call_duration_seconds` | Histogram | `adapter_id`, `logical_model`, `data_region`, `status_class`, `phase` (`ttft`/`total`) | Upstream AI vendor response latency (`ttft` vs. end-to-end `total`). |
| `tanpopo_deployment_circuit_state` | Gauge | `adapter_id`, `data_region`, `state` (`healthy`/`cooldown`/`open`/`disabled`) | Live `SM-04` circuit-breaker posture across deployments. |
| `tanpopo_sandbox_traps_total` | Counter | `adapter_id`, `trap_reason` (`cpu_deadline`/`oom_16mib`/`egress_denied`/`abi_violation`) | Detects faulting or malicious extensions (`> 20 / 60s` -> `QUARANTINED`). |
| `tanpopo_active_streams` | Gauge / UpDownCounter | `plane` | Concurrent open SSE stream saturation per node (`SLO-06` & HPA scaling). |
| `tanpopo_config_snapshot_age_seconds` | Gauge | `sync_channel` (`valkey_pubsub`/`pg_notify`/`db_poll`) | Direct SLI tracking the `< 1s` / `< 5s` (`TARGET / UNVERIFIED`) revocation SLA (`SLO-07`). |
| `tanpopo_telemetry_dropped_total` | Counter | `signal` (`traces`/`metrics`/`logs`) | Detects `32 MiB` ring-buffer `Drop-Oldest` shedding during external OTLP collector outages. |

*(`org_id`* and `project_id`* are governed by `telemetry.metrics.include_tenant_labels`, defaulting to `true` for standalone/medium deployments and configurable to `false` on high-tenant clusters.)*

### 3.2. Secret-Safe `snake_case` JSON Structured Logging Contract (`TP-FR-0806`)

Every log line written to `stdout` (or OTLP Logs) is a flat single-line JSON object using **strictly `snake_case` keys**, always carrying `request_id` and `entry_point` (`unified_http`, `native_http`, `mcp_http`, `a2a_http`, `admin_api`, `cli`, `scheduler_reaper`):

```json
{
  "ts": "2026-11-14T16:20:05.142Z",
  "level": "WARN",
  "event": "gateway.routing.pre_stream_failover",
  "entry_point": "unified_http",
  "request_id": "req_01jc8x9p2m4n6q8r0s2t4v6w8y",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "org_id": "org_01jc8001",
  "project_id": "prj_01jc8009",
  "env_id": "env_01jc8010",
  "route_slug": "prod-default",
  "failed_deployment_id": "dep_01jc8021",
  "upstream_status": 429,
  "next_deployment_id": "dep_01jc8022",
  "attempt": 1,
  "overhead_ms": 1.84
}
```

- **Mandatory Redaction (`OPS-AC-10`):** Prompt/completion bodies (`messages`, `input`, `arguments`, `output`) are never written to standard logs; the serializer masks `Authorization`, `x-api-key`, `x-goog-api-key`, `X-Tanpopo-Virtual-Key`, `Cookie`, and `Set-Cookie` headers plus `vk_tp_*` / `sk-*` prefixes with `"[REDACTED]"`.

### 3.3. Symptom-Based Alert Alignment (`INC-01` .. `INC-07`)

All symptom alerts use canonical `ALT-*` identifiers synchronized 1-to-1 with [`monitoring-incidents.md`](../operations/monitoring-incidents.md):
- **`ALT-GATEWAY-BURN` (`PAGE`):** Internal Tanpopo `5xx` error-budget burn rate breach on `tanpopo_http_requests_total{status_class="5xx"}` (excluding upstream vendor `5xx`/`429`) -> `DR-04` rollback check.
- **`ALT-LATENCY-SLO` (`PAGE`):** `histogram_quantile(0.95, rate(tanpopo_proxy_overhead_seconds_bucket[5m])) > 0.010` sustained `5m` (`TARGET / UNVERIFIED`) -> `INC-03` / `INC-07`.
- **`ALT-REVOCATION-LAG` (`PAGE`):** `max(tanpopo_config_snapshot_age_seconds) > 5` sustained `> 30s` (`TARGET / UNVERIFIED`) -> `INC-07`.
- **`ALT-UNMETERED-SPEND` (`TICKET`):** New settlements with `accuracy_tier="UNMETERED_FLAGGED"` -> populate rate card on `SCR-05`.
