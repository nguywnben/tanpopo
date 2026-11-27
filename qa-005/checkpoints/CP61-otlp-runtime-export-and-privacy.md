# CP61 — OTLP runtime export, signal paths, and limited privacy probe

**Source:** frozen unsigned RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`.
**Artifact:** local exact-source Linux/amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; build metadata reports the frozen revision and `vcs.modified=false`.
**Disposition:** DEF-063 **PASS (scoped)**; DEF-064 **PASS (scoped)**; privacy behavior **PARTIAL**. This is a runtime-focused verification, not a full telemetry certification.

## Reproduction and observations

1. Inspected `tanpopo serve --help` on the exact-source artifact. It exposes `--otlp-endpoint` and `--otlp-allow-private-ip`.
2. Started two isolated SQLite runtimes: a fresh test DB at `127.0.0.1:18154` and a coherent test clone of the populated QA DB at `127.0.0.1:18155`. Both used a local-only synthetic OTLP receiver at `127.0.0.1:4320`, the exact binary, and offline mode. Their `/healthz` and `/readyz` endpoints returned HTTP 200.
3. First tried enabling private-IP export without `TANPOPO_OTLP_ALLOWED_CIDRS`. Startup failed closed with `egress_invalid_policy` because `allow_private_ip=true` requires an explicit non-empty CIDR restriction. After setting the test-only allowlist to `127.0.0.1/32`, the runtime started and the loopback collector target was permitted. This is expected SSRF/egress protection, not a defect.
4. On the fresh runtime, opened `http://127.0.0.1:18154/v1/models` in real Chromium without a bearer key. The browser showed HTTP 401 `invalid_virtual_key`; the server returned a trace ID and emitted telemetry to the receiver.
5. On the populated runtime, used the real Owner Playground in Chromium with a deterministic, protocol-matched OpenAI-compatible SSE fixture. Two local-only requests returned HTTP 200, `Exact`, `$0.000000`. For the canary prompt request, trace `trc_01m8d9rwv9e00ak2ja3bev06bk` resolved via direct admin trace lookup and the UI's Inspect Trace using the same ID. The screenshot shows the streamed fixture response and trace card; the inspected trace shows HTTP 200 and exact zero cost.
6. Revoked both test Playground keys and verified their `REVOKED` state in the UI/API. Stopped both Tanpopo runtimes after capture.

## Receiver results and limitations

The temporary receiver saw POSTs for `/v1/traces` (10), `/v1/metrics` (306), and `/v1/logs` (27), all with `application/x-protobuf`; observed payload sizes are recorded in [sanitized evidence](../evidence/CP61-def063-064-otlp-runtime.json). It was only an HTTP sink: it returned 200 by design and did **not** decode protobuf or validate OpenTelemetry semantic correctness. The 1-second metrics interval was explicitly set to make metrics observable and caused a high request count; it is not representative of default volume.

The receiver's simple byte scan found zero occurrences of the synthetic prompt and authorization canary strings in any body. This does not prove semantic privacy because protobuf was not decoded, nor does it cover other sensitive fields or encodings. Accordingly privacy is PARTIAL and the broader OTLP data-minimization question remains open.

The first failed start without an explicit CIDR and the successful run with `127.0.0.1/32` demonstrate the SSRF safeguard. The required environment variable is not mentioned in `serve --help` or the operator documentation found in this baseline. This is documented separately as PRODUCT_GAP PG-008 (operator discoverability), not a defect absent an accepted documentation contract.

## Defect classifications

- **DEF-063 — PASS (scoped):** current exact RC7 exposes runtime OTLP endpoint configuration and sent trace signal requests to the configured loopback endpoint. The historical RC6 failure remains preserved. A real collector, other deployment modes, TLS/authentication, retry and failure behavior remain UNVERIFIED.
- **DEF-064 — PASS (scoped):** exact RC7 emitted metrics, trace and log OTLP protobuf HTTP posts. The prior finding that only Prometheus/JSON mechanisms existed did not reproduce for the configured runtime. Protobuf semantics, exporter lifecycle/backpressure, real collector interoperability, production topology and platform coverage remain UNVERIFIED.
- **Privacy probe — PARTIAL:** no raw synthetic canary bytes were found, but there is no semantic decoding and no complete sensitive-attribute inventory.
- **Egress safeguard — PASS (scoped):** loopback is denied unless an explicit CIDR narrows private-IP access; authorized loopback succeeded.

All screenshots referenced by this checkpoint were opened and visually inspected. Full sanitized test facts and trace IDs are in [CP61 evidence](../evidence/CP61-def063-064-otlp-runtime.json). No product implementation or specification changed, no production credential was accessed, and no paid provider was called. Campaign remains in progress; `GATE-REL-07` remains BLOCKED.
