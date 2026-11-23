# DEF-017 — Playground trace metadata stays pending because it looks up a different request ID

**Severity:** Moderate  
**Status:** Confirmed on baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`; QA-only synthetic environment; no product code changed.

## Summary

After a successful streaming Playground inference, the result panel displays `Primary · req_…` and indefinitely says `Trace metadata pending…`. The Console repeatedly requests `/admin/v1/traces/{req_…}` and receives HTTP 404 `trace not found`. The same successful operation is persisted under a `trc_…` identifier: the Admin API returns HTTP 200 for `/admin/v1/traces/{trc_…}`, and the Traces screen lists that record with HTTP 200 and exact metering.

This defect prevents a Playground user from following the successful response to its trace metadata in that screen. The standalone Traces list remains usable.

## Reproduction

1. On the isolated SQLite runtime at `127.0.0.1:18086`, install and activate the official-origin `@tanpopo/ollama` adapter using the displayed permission review and explicit consent.
2. Create and probe the synthetic loopback connection `QA002 CP22 Synthetic Ollama`; configure model `qa002-cp22-streaming`, an active healthy deployment, and route `qa002-cp22-streamroute`.
3. In Playground, send `Summarize the test environment status in one sentence.` and wait at least six seconds.
4. The actual `/v1/chat/completions` response is HTTP 200 and the page shows `Synthetic inference completed.` The Execution traces card displays `req_01m856b4yye009p484h39hr8ea` and remains at `Trace metadata pending…`.
5. Browser network evidence records five retries of `GET /admin/v1/traces/req_01m856b4yye009p484h39hr8ea`, all HTTP 404. An authenticated direct lookup of that ID also returns 404 `resource_not_found` / `trace not found`.
6. The corresponding persisted record is `trc_01m856b4yze00960j2ygdcaha2`. An authenticated lookup of `/admin/v1/traces/trc_01m856b4yze00960j2ygdcaha2` returns HTTP 200 with status 200, route `qa002-cp22-streamroute`, exact usage, and no debug payload. The Traces screen lists the same `trc_…` record as a successful request.

## Expected versus actual

- **Expected:** after a Playground response completes, the associated trace card resolves to the persisted trace metadata for that response, or reports a clear terminal lookup error.
- **Actual:** the card polls an identifier the trace endpoint cannot resolve and remains pending after five HTTP 404 responses, even though the successful trace is persisted and discoverable under another identifier.

## Affected workflows and impact

Playground inference review, latency/error inspection, privacy/debug metadata review, and linking an interactive result to request telemetry. A user may reasonably conclude trace persistence is delayed or broken and cannot inspect the corresponding trace from the result.

## Facts and root-cause hypothesis

**Observed facts:** the Playground display and five lookup requests use the same `req_…` value; those lookups return 404. The Admin API and Traces list resolve a `trc_…` value for the successful operation. The request and trace identifiers are observably different.

**Hypothesis, not confirmed:** Playground retains the ingress request identifier while the Admin trace endpoint is keyed by a separately generated trace-record identifier. The UI may need the trace identifier returned by the inference response, or a supported request-to-trace lookup, rather than polling `req_…` against the trace-record endpoint.

## Acceptance criteria

- A successful Playground result resolves metadata for the same persisted trace shown in Traces, using a valid identifier or explicit mapping.
- The card does not retry a permanent 404 indefinitely; terminal errors are visible and actionable.
- The displayed link/identifier opens the associated trace and preserves metadata-only privacy guarantees.
- Repeat for successful non-streaming and streaming requests, provider errors, missing deployments, and parallel comparison requests; ensure each response maps to its own trace.

## Evidence

- `../evidence/CP22-playground-stream-result.png` — visually inspected successful response and pending metadata after six seconds.
- `../evidence/CP22-playground-stream-result.json` — response HTTP 200 and five trace lookup HTTP 404s.
- `../evidence/CP22-playground-trace-id-correlation.json` — authenticated 404 for the displayed `req_…` identifier and 200 for the persisted `trc_…` identifier.
- `../evidence/CP22-traces-after-success.png` and `.json` — visually inspected successful trace in the Traces screen; list endpoint HTTP 200.
- `../evidence/CP22-synthetic-provider-persistence.json` — sanitized SQLite-backed state for the route, deployment, successful request and settlement.

GATE-REL-07 remains **BLOCKED**. QA-002 remains in progress.
