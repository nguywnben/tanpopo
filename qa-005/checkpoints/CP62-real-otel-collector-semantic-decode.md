# CP62 — Real Collector semantic decode of RC7 OTLP signals

**Source:** frozen unsigned RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`.
**Disposition:** DEF-063 and DEF-064 **PASS (scoped)**. This follow-up strengthens CP61's synthetic-sink reachability result with a real OTLP receiver and protobuf decoding; it does not close broad telemetry/privacy coverage.

## Reproduction

1. Obtained OpenTelemetry Collector Contrib 0.130.0 Linux/amd64 release asset from the upstream GitHub release and verified its SHA-256 (`292ac0d698e80282ec3d848238ffd6976c3cb97c3c169dec955754efc2293e71`) against that asset's GitHub release metadata digest.
2. Configured its OTLP/HTTP receiver on loopback `127.0.0.1:4318` with the standard debug exporter in traces, metrics and logs pipelines. Collector startup reported “Everything is ready.”
3. Started the exact RC7 binary, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`, on isolated populated SQLite clone `127.0.0.1:18156`, with `--otlp-endpoint http://127.0.0.1:4318 --otlp-allow-private-ip` and `TANPOPO_OTLP_ALLOWED_CIDRS=127.0.0.1/32`. The clone passed `PRAGMA integrity_check` and was kept separate from other test data.
4. Health and readiness both returned HTTP 200. Chromium 156.0.8078.79 loaded `/v1/models` without a bearer key and rendered HTTP 401 `invalid_virtual_key`. The screenshot was visually inspected.
5. The Collector decoded one corresponding `Ingress` server span with Error / `invalid_virtual_key`, one WARN `gateway_request` log with status 401 and matching request/trace correlation, and periodic metrics. Decoded metric descriptors included `tanpopo_config_snapshot_age_seconds`, `tanpopo_http_requests_total` and `tanpopo_proxy_overhead_seconds`. Collector counts and selected attributes are in [sanitized evidence](../evidence/CP62-real-otel-collector-semantics.json); raw collector logs are excluded.
6. Stopped the Collector and Tanpopo runtime after the check. No provider key, prompt or paid integration was used.

## Classification and limits

- **DEF-063 — PASS (scoped):** configurable OTLP trace export is accepted and decoded by a pinned real Collector against the exact RC7 runtime.
- **DEF-064 — PASS (scoped):** trace, metric and log OTLP/HTTP protobuf signals were received and decoded by Collector Contrib 0.130.0. Selected signal structure and metric descriptors are verified.
- **Privacy — UNVERIFIED beyond CP61's limited probe:** this request contained no prompt or credential. CP61's canary was checked only as raw bytes at a synthetic non-decoding sink. No claim is made about sensitive data in encoded semantic attributes.

TLS/mTLS, authentication, export retries/backoff, collector outage and queue pressure, shutdown flushing, remote production collectors, other versions/platforms, and actual sensitive-payload minimization remain UNVERIFIED. The Collector used a debug exporter and is only a protocol integration oracle, not production backend evidence. No source/spec changes or paid calls were made. Campaign remains in progress; `GATE-REL-07` remains BLOCKED.
