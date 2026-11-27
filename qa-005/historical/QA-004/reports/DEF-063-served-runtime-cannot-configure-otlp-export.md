# DEF-063 — Served runtime cannot configure an OTLP exporter

**Severity:** Major
**Status:** FAIL
**First found:** QA-004 CP47
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Affected workflows:** deployment configuration, external trace collection, central observability, operations incident response.

## Summary

The telemetry package accepts an OTLP endpoint and constructs a guarded trace exporter when `PipelineConfig.OTLPEndpoint` is provided. The actual `tanpopo serve` path calls `NewPipeline` without that setting and exposes no corresponding CLI flag, runtime environment read, or documented configuration option. The normal server therefore has no configured exporter, and cannot send spans to an external collector through its supported runtime configuration.

This conflicts with FT-14's external OTLP export contract and operational documentation that describes collector backpressure/drop behavior. The local trace and Prometheus paths are separate and do not demonstrate external export.

## Reproduction

1. Start the frozen RC6 Linux amd64 artifact on a fresh isolated data directory, offline, with a synthetic loopback OTLP HTTP receiver listening on `127.0.0.1:4320`.
2. Set `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4320`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://127.0.0.1:4320`, and `TANPOPO_OTLP_ENDPOINT=http://127.0.0.1:4320` before `tanpopo serve --role all --listen 127.0.0.1:18131 --offline`.
3. Confirm `/healthz` and `/readyz` return 200.
4. In real Chromium, request `/v1/models`, then POST a synthetic chat request carrying a fabricated non-credential authorization marker and a prompt canary. The response includes `X-Tanpopo-Trace-Id` and returns 401 `invalid_credential_plane` before provider dispatch.
5. Query the receiver's read-only summary. It is healthy and reports zero POSTs, zero OTLP paths, and no prompt/auth markers. The runtime did create an ingress trace ID; the supplied endpoint values did not activate export.
6. Inspect the runtime wiring: `cmd/tanpopo/serve.go` passes only service name/version, DB, and Vault to `NewPipeline`. The repository has no runtime reads for the tested exporter variables and no telemetry endpoint field in the `serve` command configuration. The only non-test `PipelineConfig` call is this one.

Reproduction artifacts: [sanitized result](../evidence/CP47-otel-runtime-results.json), [models response screenshot](../evidence/CP47-otel-v1-models-error.png), [request response screenshot](../evidence/CP47-otel-request-response.png), [CP47](../checkpoints/CP47-otlp-runtime-export-and-privacy.md). Screenshots were opened and visually inspected.

## Expected and actual

- **Expected:** operators can configure a supported OTLP trace endpoint using a documented serve setting; the runtime sends standards-compliant trace batches through the same guarded egress policy used by the telemetry package. With a valid synthetic inference journey, the trace is visible at the configured collector.
- **Actual:** the supported `serve` runtime has no path to populate the endpoint/exporter configuration. Standard OTel endpoint variables and the candidate Tanpopo endpoint variable produced no receiver POSTs. The observed HTTP request was rejected at authentication, so successful inference-to-export remains unverified; source wiring independently shows the exporter is not enabled.

## Business impact

Self-hosted operators cannot connect the running product to an external trace backend using a documented or supported runtime setting. Incident teams cannot rely on the documented central trace visibility, collector-outage handling, or drop counters for this deployment path. Local trace/metrics UI remains available in the tested build but does not fulfill external export.

## Facts and root-cause hypothesis

**Verified facts:**

- Frozen executable reports version `0.0.1-snapshot`, commit `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; SHA-256 `9a62f8844a85aa1fd3fd9be22365b04e9ce655a470fbe9de670e19678644d34b`.
- Isolated server was ready; browser received a trace ID on a real Data Plane request.
- A local loopback receiver saw zero OTLP POSTs with standard OTel endpoint variables and `TANPOPO_OTLP_ENDPOINT` set.
- `serve.go` constructs `PipelineConfig` without exporter settings. Source search found the endpoint only in the telemetry package, with the only non-test `NewPipeline` call in `serve.go`.
- `PipelineConfig` endpoint setup constructs only an `otlptrace` exporter. A configured private collector would also require the intended explicit private-IP policy; CP47 did not weaken that control.

**Hypothesis:** exporter configuration was implemented at the telemetry-library boundary but omitted from the executable configuration/CLI integration. Confirm intended configuration UX and ownership during engineering triage.

## Acceptance criteria

1. `tanpopo serve` has a documented, validated way to configure OTLP export and passes the setting into `PipelineConfig`.
2. Endpoint authentication, TLS requirements, hostname allowlisting, private-IP controls, timeouts, and startup errors remain explicit and secure; configuration must not bypass the guarded egress layer.
3. A fresh runtime with a synthetic authorized inference route emits the corresponding trace to a local collector, and the Console/local trace record reconciles to the exported trace ID.
4. An unavailable collector remains non-blocking, respects the configured bounded queue, increments observable drop counters, and recovers after the collector returns.
5. Tests cover unset configuration, valid TLS collector, denied private target unless explicitly permitted by policy, malformed endpoint, collector outage, shutdown flush, and redaction.

## Scope boundary

CP47 did not run an authorized inference request because the fresh runtime had no provisioned Data Plane key/provider. It establishes the configuration omission through runtime behavior plus source wiring, not successful end-to-end provider export or privacy of emitted spans. Those remain UNVERIFIED.
