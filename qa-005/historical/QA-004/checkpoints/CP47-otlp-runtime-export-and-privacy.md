# CP47 — OTLP runtime export wiring and three-signal contract

**Status:** FAIL for served-runtime trace exporter configuration (DEF-063) and advertised OTLP metrics/logs (DEF-064); UNVERIFIED for authorized inference export, exported-span privacy, outage recovery, and per-signal runtime behavior. **GATE-REL-07:** BLOCKED.
**Baseline:** frozen RC6 source `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; artifact run `45581179274`.
**Product source:** unchanged.

## Isolated environment and artifact

- Used a fresh RC6 Linux amd64 executable and fresh SQLite data under `/tmp/qa004-otel-runtime-2/data` on `127.0.0.1:18131`; `/healthz` and `/readyz` both returned HTTP 200.
- The local test collector listened only on `127.0.0.1:4320`. Tanpopo was run offline except for loopback test traffic. No provider credentials or production endpoints were used.
- The executable reports version `0.0.1-snapshot`, exact frozen source commit, SHA-256 `9a62f8844a85aa1fd3fd9be22365b04e9ce655a470fbe9de670e19678644d34b`.
- Before server start, set the standard `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, plus candidate `TANPOPO_OTLP_ENDPOINT`, all pointed at the local collector.

## Real browser request and collector result

Used real headless Chromium against the live RC6 process and opened both captured screenshots for visual inspection:

1. `GET /v1/models` returned 401 `invalid_virtual_key` and included an `X-Tanpopo-Trace-Id`.
2. `POST /v1/chat/completions` with a fabricated auth marker and synthetic prompt canary returned 401 `invalid_credential_plane` before provider dispatch. The response included trace ID `trc_01m89yh7d7e008d7f6gv2pgtme`.
3. Queried the synthetic collector's sanitized summary: 0 POST requests, 0 paths, no prompt marker, and no auth marker.

This proves the runtime creates ingress trace identity for the request but does not prove an authorized provider request or successful export. There was no valid Data Plane key or configured provider in this fresh instance.

## Contract and implementation review

- `FT-14` promises OTLP Metrics, Traces, and Logs from external observability backends and describes collector outage/drop behavior.
- `cmd/tanpopo/serve.go` constructs `PipelineConfig` with only service name/version, DB, and Vault. Source search found no runtime path reading the tested endpoint variables and no `serve` telemetry endpoint option.
- The telemetry package can construct an `otlptrace` exporter if a caller directly fills `PipelineConfig.OTLPEndpoint`. The only non-test `NewPipeline` caller is `serve.go`, and that call leaves exporter settings empty.
- `PipelineConfig` exposes only a `sdktrace.SpanExporter`; `NewPipeline` builds the OTLP trace exporter. Metrics use an OpenTelemetry-to-Prometheus exporter and local scrape path, while logs use `privacy.NewJSONLogger`. The declared OTLP exporter dependency is `otlptrace`; no OTLP metric or log exporter path was found.
- No end-to-end privacy PASS is assigned. The attempted prompt/auth canaries were rejected before inference, and the collector received no spans to inspect. Metadata-only source safeguards do not substitute for exported payload inspection.

## New defects

- [DEF-063](../reports/DEF-063-served-runtime-cannot-configure-otlp-export.md) — **Major, FAIL:** the normal served runtime has no supported way to enable the existing OTLP trace exporter; tested environment variables resulted in no collector requests.
- [DEF-064](../reports/DEF-064-otlp-metrics-and-logs-not-implemented.md) — **Moderate, FAIL:** FT-14 advertises OTLP metrics and logs, but RC6 has only trace exporter construction; metrics/logs use separate Prometheus and JSON sinks.

## Evidence

- [Sanitized runtime result](../evidence/CP47-otel-runtime-results.json)
- [Live `/v1/models` 401 screenshot](../evidence/CP47-otel-v1-models-error.png)
- [Live request trace-ID / authorization error screenshot](../evidence/CP47-otel-request-response.png)

The screenshots show the actual rendered HTTP response pages. The trace-ID header and collector count are also captured in the evidence JSON.

## Coverage update and gaps

- OTel runtime exporter wiring: **FAIL** (DEF-063).
- OTLP metrics/logs compared with the product contract: **FAIL** (DEF-064).
- Authorized inference to a real/synthetic provider and trace export: **UNVERIFIED**.
- Privacy of exported spans/logs/metrics: **UNVERIFIED**. Canary markers were only submitted on the pre-auth rejected request; no external payload was observed.
- Collector backpressure, bounded queue, retry/recovery, shutdown flush, mTLS/auth, endpoint validation, private-address policy and fail-open impact on successful streaming inference: **UNVERIFIED**.
- This checkpoint does not complete observability or product acceptance. Continue unrelated executable QA areas.
