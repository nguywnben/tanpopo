# Two-Phase Budget Ledger, Granular Pricing & Production Observability Specification — Tanpopo

> **Core Technical Specification Document (ASTRA Phase A05 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — PRE-APPROVAL`)**
> - **Document ID:** `SPEC-CORE-02` | **Version:** `0.1.0-step05-draft` | **Last Updated:** `2026-11-11`
> - **Authoring Role:** Principal SRE & FinOps/Observability Architect
> - **Governance Status:** **Proposed Design Specification (`PROPOSED`)** — aligned with [`ADR-0005`](../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md) and production telemetry standards in `observability-and-instrumentation`.
> - **Requirements Traceability:** `TP-FR-0801`..`0806`, `TP-NFR-0101`..`0201`, `TP-NFR-0501`, `TP-BR-0701`..`0703`, `TP-BR-0802`, `SM-07`.

---

## 1. Domain Firewall: Separating Commercial API Spend from Consumer Subscriptions

In strict compliance with `DEC-004`, `REJ-003`, and ASTRA Phase A05 architectural guardrails, Tanpopo's `FinOps Ledger` models and settles exclusively two legitimate technical billing modes (`billing_mode`):
1. **`COMMERCIAL_API`:** Traffic routed over authorized pay-as-you-go commercial API connections (commercial API Keys, Cloud IAM Bedrock/Vertex endpoints, or enterprise gateways with explicit per-token/per-unit pricing).
2. **`SELF_HOSTED_INFRA`:** Traffic routed to self-operated internal GPU clusters (`Ollama`, `vLLM`, `SGLang`), metered at either `0 micro-USD` or an administrator-configured `internal chargeback rate`.

**Unconditionally excluded from data models & UI:** The platform contains zero concepts for *"pooling commercial API Key spend with personal ChatGPT Plus/Claude Pro subscription message caps"*, zero synthetic consumer-plan message-quota estimators, and never amortizes personal monthly consumer subscriptions into shared API unit rates.

---

## 2. Integer `Micro-USD` Ledger Mathematics & `Two-Phase Budget Hold` Protocol

### 2.1. Zero-Floating-Point Drift Pricing Arithmetic (`Integer Nano/Micro-USD Arithmetic`)

Every rate card on a `Model Deployment` stores unit prices as **64-bit integer `nano-USD` per token** (`1 USD / 1,000,000 tokens = 1,000 nano-USD / token`, representing ultra-low-cost tiers such as `0.015 USD / 1M tokens = 15 nano-USD / token` with exact precision). When settling into the `micro-USD` ledger (`1 USD = 1,000,000 micro-USD`), Tanpopo applies conservative `ceiling division` so fractional remainders never undercount against hard budget ceilings:

$$
\text{Cost}_{\text{nano}} = (T_{\text{in}} \cdot P_{\text{in}}) + (T_{\text{out}} \cdot P_{\text{out}}) + (T_{\text{cache\_r}} \cdot P_{\text{cache\_r}}) + (T_{\text{cache\_w}} \cdot P_{\text{cache\_w}}) + (T_{\text{reason}} \cdot P_{\text{reason}}) + C_{\text{media\_nano}}
$$

$$
\text{Cost}_{\text{micros}} = \left\lceil \frac{\text{Cost}_{\text{nano}}}{1000} \right\rceil = \frac{\text{Cost}_{\text{nano}} + 999}{1000} \quad (\text{64-bit integer division})
$$

### 2.2. Two-Phase Hold & Settlement Against Concurrent Budget Exhaustion (`SM-07`)

1. **Phase 1 — Pre-Stream Upper-Bound Reservation (`Pre-Request Hold`):**
   - Computes a conservative upper-bound cost across the resolved routing candidate set:
     - `est_input_tokens = ceil(utf8_byte_len(prompt_and_tools) / 3)` *(fast `< 0.05 ms` conservative heuristic requiring zero heavyweight vendor tokenizer tables on the hot path)*.
     - `bound_output_tokens = min(req.max_output_tokens ?? policy.default_hold_output_tokens, deployment.max_output_tokens)` *(defaults to `default_hold_output_tokens = 4096` when `max_tokens` is omitted)*.
     - `hold_micros = max_{d in candidates} CostMicros(d, est_input_tokens, bound_output_tokens)`.
   - Atomically verifies available headroom across all four hierarchical scopes (`Virtual Key`, `Environment`, `Project`, `Organization`):
     - Admission invariant at every scope `S`: `settled_exact_micros + settled_estimated_micros + negative_carryover_micros + reserved_hold_micros + hold_micros <= hard_limit_micros`.
     - If any scope breaches its ceiling → immediately rejects with **`429 <scope>_budget_exceeded`** before opening any upstream provider connection. Otherwise → increments `reserved_hold_micros` by `hold_micros` and creates a `RESERVED_HOLD` lease with `lease_expires_at = now() + 90s` (automatically renewed every `30s` while an SSE stream remains active).
2. **Phase 2 — Honest Post-Stream Settlement & Minor Overrun Carryover (`Settlement & Carryover`):**
   - Upon stream completion (normal EOF, mid-stream abort, or client cancellation), releases the exact `hold_micros` from `reserved_hold_micros` and posts `actual_cost_micros` under the corresponding metering accuracy tier:
     - **`EXACT`:** Upstream returned a valid, complete `usage` block matched against an active deployment rate card.
     - **`ESTIMATED`:** Stream aborted mid-flight or the upstream engine omitted trailing `usage` counters → metered from actual streamed UTF-8 payload bytes and stamped `X-Tanpopo-Usage-Accuracy: ESTIMATED`.
     - **`UNMETERED_FLAGGED`:** Newly introduced modality lacking a configured unit rate or unparseable response → records `total_cost_micros = 0` while **mandatorily** flagging `UNMETERED_FLAGGED` in reconciliation reports and emitting a configuration alert to the `Team Admin` (`TP-BR-0703`).
   - **`Negative Balance Carryover` (`BA-OQ-002`):** If actual post-stream spend slightly exceeds the initial pre-request hold and nudges cumulative spend marginally past `hard_limit_micros`, Tanpopo **does not truncate the final words of an in-flight response**; instead, it records the exact overrun in `negative_carryover_micros` and hard-blocks `100%` of subsequent requests until the next budget period resets or an administrator raises the cap.

---

## 3. Production-Grade Observability Architecture (`OpenTelemetry Metrics, Traces & Structured Logs`)

### 3.1. Four Core `On-Call Questions` Driving Every Telemetry Signal

Every metric, trace span, and structured log line in Tanpopo is engineered so an on-call operator can answer four diagnostic questions within `< 2 minutes`:
1. *Do elevated errors or tail latencies originate **inside Tanpopo** (DB, Wasm Sandbox, Rate Limiter) or from an **overloaded upstream AI vendor**?*
2. *Which `Route` or `Model Deployment` currently has an open circuit breaker (`CIRCUIT_OPEN`), and to which backup target has traffic failed over?*
3. *Why was a specific customer request `req_...` rejected (`Policy`, `Data Residency`, `Budget`, `Capability`, or `MCP Schema Drift`)?*
4. *Is any installed extension exhausting CPU fuel, hitting linear memory limits, or attempting unauthorized network egress?*

### 3.2. Canonical `RED` & `USE` Metrics Catalog (`Bounded Cardinality`)

> **Anti-Cardinality-Explosion Invariant (`Zero High-Cardinality Labels`):** Attaching `request_id`, `virtual_key_id`, `principal_id`, `email`, raw URL paths, or free-form error strings as Metric labels is **strictly prohibited**. High-cardinality correlation belongs exclusively in `Structured Logs` and `Traces`.

| Metric Name (OTel / Prometheus Standard) | Type | Allowed Bounded Labels (`Allowed Bounded Labels`) | On-Call Diagnostic Purpose |
|---|---|---|---|
| `tanpopo_http_requests_total` | Counter | `plane`, `route_template`, `status_class` (`2xx`/`4xx`/`5xx`), `error_code` | Gateway-wide request rate and canonical error breakdown (`RED - Rate & Errors`). |
| `tanpopo_proxy_overhead_seconds` | Histogram | `plane`, `extension_tier` (`declarative`/`wasm`/`sidecar`) | Pure internal Tanpopo processing overhead (`p50`/`p95`/`p99` tracked against `< 2ms`/`< 5ms` SLOs). |
| `tanpopo_sse_chunk_relay_seconds` | Histogram | `extension_tier` | Per-chunk SSE relay latency (`p95 < 1ms` — `TP-NFR-0102`). |
| `tanpopo_upstream_call_duration_seconds` | Histogram | `adapter_slug`, `data_region`, `status_class`, `phase` (`ttft`/`total`) | Upstream AI provider response latency (`ttft` vs. end-to-end `total`). |
| `tanpopo_deployment_circuit_state` | Gauge | `adapter_slug`, `data_region`, `state` (`healthy`/`cooldown`/`open`) | Live circuit-breaker state across model deployments. |
| `tanpopo_sandbox_traps_total` | Counter | `extension_id`, `trap_reason` (`cpu_fuel`/`oom`/`egress_denied`/`panic`) | Immediate detection of faulty or misbehaving extensions. |
| `tanpopo_active_streams` | UpDownCounter | `plane` | Concurrent open SSE stream saturation per node (`USE - Saturation`). |
| `tanpopo_config_snapshot_age_seconds` | Gauge | `sync_channel` | Direct SLI monitoring the `< 5 second` emergency revocation SLA (`TP-NFR-0404`). |
| `tanpopo_telemetry_dropped_total` | Counter | `signal` (`traces`/`metrics`/`logs`) | Detects external OTLP collector backpressure triggering `Fail-Open` ring-buffer drops. |

### 3.3. JSON `Structured Logging` & Privacy-by-Default

Every log record emitted to `stdout` (or OTLP Logs) is a flat JSON object with stable field names, mandatorily carrying both **`requestId`** and **`entryPoint`** (`unified_http`, `native_http`, `mcp_http`, `a2a_http`, `admin_api`, `cli`, `scheduler_reaper`) from the point of ingress:

```json
{
  "ts": "2026-11-11T16:20:05.142Z",
  "level": "warn",
  "event": "upstream_prestream_failover",
  "entryPoint": "unified_http",
  "requestId": "req_01jc8x9p2m4n6q8r0s2t4v6w8y",
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736",
  "orgId": "org_01jc8001",
  "projectId": "prj_01jc8009",
  "routeSlug": "prod-default",
  "failedDeploymentId": "dep_01",
  "upstreamStatus": 429,
  "nextDeploymentId": "dep_02",
  "attempt": 1
}
```

- **Multi-Layer Secret Redaction (`Mandatory Redaction` — `TP-FR-0806`):** Request and response bodies (`messages`, `input`, `arguments`, `output`) are never written to standard logs; the log serializer automatically masks `Authorization`, `x-api-key`, `x-goog-api-key`, `Cookie`, and `Set-Cookie` headers as well as known token prefixes (`vk_tp_*`, `sk-*`) with `"[REDACTED]"`.

### 3.4. Actionable `Symptom Alerts & Runbooks`

Alerting uses strictly two actionable severities (**`PAGE`** for immediate on-call wakeup and **`TICKET`** for next-business-day triage), each hard-linked to operational runbooks in [`docs/operations/monitoring-incidents.md`](../operations/monitoring-incidents.md) (`INC-01`..`07`):

1. **`PAGE — TanpopoInternalHighErrorBurn`:** Internal Tanpopo `5xx` error-budget burn rate `>= 14.4x` over `5m/1h`.
   - *Runbook Summary:* **Meaning:** Storage tier degradation or a faulty Tanpopo release is causing internal `5xx` failures. **First Check:** Inspect `tanpopo_http_requests_total{status_class="5xx"}` grouped by `error_code` and verify `PgBouncer/PostgreSQL` pool health. **Escalation:** Gateway Infrastructure On-Call (`Operator`) to execute a `Rolling Rollback`.
2. **`PAGE — RevocationSyncLagBreached`:** `max(tanpopo_config_snapshot_age_seconds) > 5` sustained for `> 30 seconds`.
   - *Runbook Summary:* **Meaning:** One or more Data Plane nodes are failing to receive real-time key revocation / kill-switch snapshots. **First Check:** Inspect `Valkey` pub/sub cluster health and `PostgreSQL` connectivity from lagging pods. **Escalation:** Drain and isolate stale pods from the Load Balancer.
3. **`TICKET — UnmeteredUsageDetected`:** New ledger settlements recorded with `usage_accuracy = "UNMETERED_FLAGGED"` within `15 minutes`.
   - *Runbook Summary:* **Meaning:** Traffic hit a deployment lacking a configured unit rate card or an Adapter failed to extract usage metrics. **First Check:** Query `usage_settlements WHERE usage_accuracy = 'UNMETERED_FLAGGED'` to identify the `deployment_id` and update its pricing table.
