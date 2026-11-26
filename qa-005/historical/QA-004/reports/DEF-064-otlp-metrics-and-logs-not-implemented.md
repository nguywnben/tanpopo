# DEF-064 — FT-14 promises OTLP metrics and logs that the runtime does not export

**Severity:** Moderate
**Status:** FAIL
**First found:** QA-004 CP47
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Affected workflows:** external observability onboarding, metrics centralization, log aggregation, incident alerting.

## Summary

FT-14 explicitly promises standards-compliant OTLP Metrics, Traces, and Logs. In RC6, `PipelineConfig` exposes a `sdktrace.SpanExporter` and `NewPipeline` constructs an `otlptrace` exporter only. Metrics use a Prometheus exporter and `/metrics` scrape handler; log records go through the local JSON logger. There is no OTLP metric exporter or OTLP log exporter dependency or runtime path in the inspected source.

This finding is distinct from DEF-063. DEF-063 concerns the executable's inability to configure even the existing trace exporter. DEF-064 concerns missing OTLP signal implementations for metrics and logs, even if trace endpoint wiring is later added.

## Reproduction / evidence

1. Compare `docs/product/feature-catalog.md` FT-14 output contract (Metrics, Traces, Logs) with `internal/telemetry/otel/tracer.go`'s `PipelineConfig` and `NewPipeline`.
2. Inspect metric initialization in `internal/telemetry/otel/metrics.go`: it creates an OpenTelemetry-to-Prometheus exporter and serves Prometheus exposition; no OTLP metric exporter is constructed.
3. Inspect `NewPipeline`: the only external exporter construction is `otlptrace.New`; the pipeline logger is `privacy.NewJSONLogger`.
4. Inspect `go.mod`: the OTLP exporter dependency is `otlptrace`; no OTLP metric or log exporter module is declared.
5. CP47's isolated runtime/receiver experiment observed zero OTLP POSTs for the exercised request, consistent with the separate DEF-063 wiring failure. Runtime implementation gaps for metrics/logs are established from source; independent signal-specific runtime verification remains UNVERIFIED until configurable export exists.

Evidence: [CP47 source/runtime result](../evidence/CP47-otel-runtime-results.json), [feature contract](../../docs/product/feature-catalog.md#ft-14--opentelemetry-exporter--privacy-first-trace-explorer-otel--privacy-first-telemetry), [CP47 checkpoint](../checkpoints/CP47-otlp-runtime-export-and-privacy.md).

## Expected and actual

- **Expected:** all three signals named in FT-14 can be configured and delivered using standard OTLP protocol(s), with the published bounded, non-blocking failure policy and documented privacy guarantees.
- **Actual:** metrics are exposed by Prometheus scrape; logs use JSON logger output; only trace-export code exists, and the served runtime does not wire it (DEF-063).

## Business impact

Teams adopting Tanpopo as an observability source cannot route the promised metrics and logs through an OTLP-only collector. They must build separate scrape/log pipelines or lose the signals, and the current feature description overstates integration capability. This weakens unified alerting and incident correlation.

## Facts and root-cause hypothesis

**Verified facts:** the feature catalog says Metrics, Traces, and Logs; the pipeline exposes only a span exporter; metrics are connected to Prometheus; logs are connected to a JSON logger; `go.mod` declares `otlptrace` but no OTLP metric/log exporter.

**Hypothesis:** the FT-14 product contract was written for a broader telemetry design than the signal exporters implemented in RC6. Decide whether to complete the three-signal implementation or explicitly narrow the product contract and supported deployment guidance.

## Acceptance criteria

1. FT-14's three signal types each have a concrete, configurable OTLP exporter in the served runtime, or the product contract and user-facing documentation are explicitly narrowed before release.
2. A synthetic collector receives metrics, traces, and logs using documented endpoints/protocol; each signal has a stable service/resource identity and shutdown behavior.
3. Failure/backpressure tests establish bounded memory, non-blocking inference, drop/error counters, and recovery independently per signal.
4. Canary prompt and credential markers are absent from exported metrics, traces, and logs in metadata-only mode; debug capture remains explicitly opt-in, scoped, encrypted, and TTL-bound.
5. Console and operations documentation accurately distinguish local Prometheus/JSON sinks from external OTLP outputs.

## Scope boundary

The independent metrics/log signal integration paths were not dynamically exercised because the runtime exposes no OTLP metrics/log exporter configuration. Do not infer PASS from existing Prometheus metrics tests or local JSON log behavior.
