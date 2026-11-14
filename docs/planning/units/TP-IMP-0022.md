# `TP-IMP-0022` — `OpenTelemetry` Exporter (Bounded Cardinality, `32 MiB` `Drop-Oldest` Ring Buffer) & `Metadata-Only` Privacy Redactor

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0022` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-04` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0015`, `TP-IMP-0021` — `GAP-01, 13`)* |
| **Size** | `M` (`go.mod`, `internal/telemetry/otel/*`, `internal/telemetry/privacy/*`, `deploy/monitoring/alerts.yaml`, `db/migrations/sqlite/0009_traces_blobs.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(`0-byte` default prompt privacy & hot-path inference protection under OTLP collector blackholes)* |
| **Technical Owner** | `Staff Observability & Privacy Engineer` |

---

## 1. Intent
Implement the unified observability plane over **OpenTelemetry (`Metrics`, `Traces`, `Structured JSON Logs`)** paired with the Prometheus `/metrics` scrape endpoint, guaranteeing two existential invariants (`TP-FR-0805`, `TP-FR-0806`, `TP-NFR-0501`):
1. **Privacy by Default (`Metadata-Only Telemetry by Default`):** Persist **strictly `0 bytes`** of prompt bodies, model completions, or tool-call arguments across DB tables, OTel spans, and structured logs by default; only when an administrator enables time-bounded debug capture on a narrow scope are debug payloads encrypted under `Project DEK` with a hard auto-expiry `TTL <= 72 hours` (`debug_payload_blobs`).
2. **Zero Inference Hot-Path Blocking (`Non-Blocking Bounded Ring Buffer`):** Bound the OTLP export queue strictly to `<= 32 MiB` with `Drop-Oldest` eviction incrementing `tanpopo_telemetry_dropped_total` — if an external OTLP collector hangs or blackholes TCP traffic, caller inference latency **increases by `< 1 ms` and RAM never grows unboundedly (`OPS-AC-12`)**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (add official OpenTelemetry Go SDK & Prometheus exporter packages).
  - `internal/telemetry/privacy/redactor.go` (recursive sanitizer scrubbing `Authorization`, `api_key`, `vk_tp_*`, `sk-*`, `Bearer `, `Cookie`, `Set-Cookie`, and eliding `messages`/`input`/`output` content whenever `capture_payloads == false`).
  - `internal/telemetry/otel/metrics.go` & `deploy/monitoring/alerts.yaml` (register canonical metrics enforcing strict `Bounded Cardinality` label allowlists and ship Prometheus alert rules cross-linked to all **7 incident runbooks `INC-01`..`INC-07`**).
  - `internal/telemetry/otel/tracer.go` (multi-stage distributed spans `Ingress -> Policy -> RouteAttempt -> ExtensionHook -> Egress`, non-blocking `32 MiB` ring buffer, and metadata trace persistence into `request_traces_meta` including `waterfall_stages_json`).
  - `internal/telemetry/ttl/cleaner.go` (background TTL reaper purging expired `debug_payload_blobs` where `expires_at <= now` and aging out historical trace metadata per retention policy).
  - `db/migrations/sqlite/0009_traces_blobs.sql` (`request_traces_meta`, `debug_payload_blobs` scoped with `project_id`).
- **Out-of-Scope:** Interactive waterfall trace viewer UI in the Web Console (owned by `TP-IMP-0030`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0805`, `TP-FR-0806`, `TP-NFR-0501`, `TP-NFR-0502`, `TP-BR-0802`, `TP-AC-F0804`, `OPS-AC-10`, `OPS-AC-12`, `INC-01`..`INC-07`.
- **Dependencies:** `TP-IMP-0015`, `TP-IMP-0021`.
- **Design Refs:** [`usage-billing-observability.md`](../../specifications/usage-billing-observability.md) (`SPEC-CORE-02` Sections 4 & 5), [`monitoring-incidents.md`](../../operations/monitoring-incidents.md) (`SPEC-OPS-03` — `INC-01`..`07`), [`backups-recovery.md`](../../operations/backups-recovery.md) Section 2.

## 4. API, Wire & Data Contracts
- **Strict Metric Label Allowlist (`Strict Metric Label Allowlist`):** Permit exclusively `{ org_id, project_id, adapter_id, logical_model, status_class, error_code, accuracy_tier }` (with an opt-out toggle for `org_id`/`project_id` on high-tenant clusters matching `TP-IMP-0002`); **strictly ban** `request_id`, `virtual_key_id`, `user_id`, or `prompt_hash`.
- **`debug_payload_blobs` Bounds:** Enabled only under an active time-bounded debug session with `expires_at <= created_at + 72h`, truncated to `<= 256 KiB/request`, and sealed under `Project DEK` (`TP-IMP-0008`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`OPS-AC-10` Zero Prompt Byte Default):** Given default configuration, When dispatching a request containing sensitive string `"SECRET_PATIENT_RECORD_998877"` and secret `sk-tp-canary-...`, Then querying `request_traces_meta`, `debug_payload_blobs`, structured JSON logs, and exported OTLP spans yields **`0` occurrences** of either string.
- **`AC-2` (`TP-AC-F0804` Dead OTLP Collector Resilience):** Given a mock OTLP collector deliberately blackholing TCP connections indefinitely, When firing 2,000 inference requests through the Gateway, Then `100%` of inference requests complete normally, OTLP export buffer memory stays `<= 32 MiB`, and `tanpopo_telemetry_dropped_total` increments accurately.
- **`AC-3` (`OPS-AC-12` `100,000` Unique `request_id` Bounded Series Proof):** Emitting `100,000` telemetry events carrying `100,000` distinct `request_id` and `virtual_key_id` values → scraping `/metrics` confirms a flat, constant Prometheus `time-series count` independent of request volume.
- **`NEG-1` (`Cardinality Explosion Guard`):** Attempting to register a metric carrying label `request_id` or `virtual_key_id` → immediately fails the architectural guard test.
- **`NEG-2` (`TTL Max > 72h Rejection`):** Attempting to create a debug payload capture session with `ttl_hours = 168` → rejected with `422 debug_capture_ttl_exceeds_72h`.

## 6. Test Commands & Required Evidence
- Pure-Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/telemetry/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/telemetry/...`)*
- Data-race (`-race`): `go test -v -race ./internal/telemetry/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `privacy.Redactor` sits at the lowest sink boundary ahead of the OTLP exporter, structured JSON logger, and `request_traces_meta` persistence writer.
- **Rollback:** Additive schema `0009_traces_blobs.sql`; external OTLP export can be disabled via config while retaining internal `/metrics`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (`GAP-13` metric-name alignment), `TP-IMP-0015`, and `TP-IMP-0021` are complete.
- **DoD:** `100%` green across `OPS-AC-10`, `OPS-AC-12`, `CanaryScanner` + `Zero Prompt Byte` assertions, and `REQUIRES_STRONG_REVIEW`.
