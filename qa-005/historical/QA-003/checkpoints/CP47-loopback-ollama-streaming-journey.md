# CP47 — Loopback Ollama connection, route explanation, and streamed Playground request

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`<br>
**Runtime:** isolated RC5 service `18116`; local mock at `127.0.0.1:19271`; Owner; real Chromium 156/Linux, 1440×1000<br>
**Disposition:** DEF-028 **PARTIAL — historical mismatch not reproduced with the controlled healthy Ollama mock**. `FT-01`, `FT-03`, `FT-10`, `WF-06`, and `SCR-15` gain scoped live integration coverage and remain **PARTIAL**. `GATE-REL-07` remains **BLOCKED**.

## Controlled loopback provider setup

A synthetic Ollama-compatible HTTP service was started on loopback. It returned one deterministic model and OpenAI-compatible streamed completion chunks; it stored request method/path/byte-count only and received no provider credential. No traffic left the local environment.

Through the real Providers Console, an Org Owner created `QA003 CP47 Ollama loopback mock` with endpoint `http://127.0.0.1:19271`, Self-hosted infra billing, On-prem residency, no credential, explicit private-access opt-in, and `127.0.0.1/32`. The initial state was `CREATED_UNVERIFIED`; the real Probe connection action succeeded in 18 ms and state persisted as `ACTIVE`. The synthetic service saw `GET /api/tags`. See [connection creation](../evidence/CP47-loopback-connection-result.json), [probe result](../evidence/CP47-loopback-probe-result.json), and visually inspected [probe screenshot](../evidence/CP47-loopback-probe-result.png).

The Owner created a new logical model `qa003-mock-ollama-stream` with the explicit `streaming` capability, then registered a zero-rate deployment for `qa003-mock-ollama:latest` through the loopback connection. The Console activated it as `ACTIVE_HEALTHY`. No API key/secret was attached. See [model/deployment result](../evidence/CP47-stream-model-deployment-result.json) and visually inspected [active deployment list](../evidence/CP47-stream-deployment-created.png).

## Capability-gated failure and recovery

Before using the dedicated streaming-capable model, the Owner sent a real Playground prompt through `qa003-cp47-ollama-route`, whose model declaration had no streaming capability. The ephemeral session was minted (HTTP 201), then `/v1/chat/completions` returned HTTP 400 `capability_not_supported` (“Deployment … does not support streaming.”). This is expected capability enforcement, not a defect; the provider mock was not called for this rejected request. The trace persisted and was read through the trace detail/list APIs, though the Playground continued to show “Trace not available yet,” another observation consistent with existing DEF-045. See [initial capability failure record](../evidence/CP47-playground-attempt-result.json) and visually inspected [failure state](../evidence/CP47-playground-after-attempt.png).

The Owner created `qa003-cp47-stream-route` targeting the newly `ACTIVE_HEALTHY` streaming deployment. A new Playground session sent the prompt “Reply with a brief acknowledgment.” The browser received streamed content `QA003 loopback mock reply`; `/v1/chat/completions` returned HTTP 200; the ephemeral session creation returned HTTP 201; the trace detail and list reads returned HTTP 200. The UI displayed the prompt, response, route, HTTP 200, `$0.000000`, and 14 ms. [Sanitized request/UI/trace record](../evidence/CP47-stream-route-playground-result.json) and visually inspected [ready](../evidence/CP47-playground-stream-ready.png) and [successful response](../evidence/CP47-playground-stream-success.png) screenshots preserve the workflow. The same trace appeared in the cross-screen Traces list and its detail API returned HTTP 200; see [trace cross-screen record](../evidence/CP47-traces-cross-screen-result.json). The sanitized [mock request log](../evidence/CP47-mock-request-log.json) records method/path/byte-count only: the probe, one separate direct control request, and the real Playground request; no payload or header values were retained.

## DEF-028 — working Ollama route dry-run

The Owner opened Route Explain for the same real Playground route (`qa003-cp47-stream-route`), selected On-prem, and ran separate dry runs for `Unified normalized (/v1/*)` and `ollama`. Both returned HTTP 200, `dry_run: true`, matched the selected route, selected the same single `ACTIVE_HEALTHY` deployment, and had zero rejected candidates. No provider dispatch occurred during either dry run. The actual Route Explain screenshots were visually inspected: [normalized](../evidence/CP47-stream-route-explain-unified.png) and [native Ollama](../evidence/CP47-stream-route-explain-ollama.png); the subsequent [Traces page](../evidence/CP47-traces-cross-screen.png) visibly retained the same route slug, HTTP 200, Exact metering, and 14 ms result after navigation. [sanitized API result](../evidence/CP47-stream-route-explain.json).

This does not reproduce QA-002 CP54's working-Ollama `DIALECT_MISMATCH` result on its exact live baseline/upstream. It exercises the RC5 selector against a controlled healthy Ollama adapter and shows both supported ingress dialects eligible. DEF-028 therefore changes from UNVERIFIED to **PARTIAL**, not verified fixed. A real external Ollama server, other dialects/adapters, route failover, and the original historical fixture remain unverified.

## Coverage and defect disposition

- `FT-01` / `WF-06` / `SCR-15`: one successful streamed mock-backed Playground interaction and its persisted trace are now **PARTIAL** evidence. Multimodal, cancellation, timeout, retry, budget enforcement, comparison, live/provider latency, and non-mock integration remain open.
- `FT-03`: RC5 loopback provider probe, capability declaration, active deployment, and request lifecycle worked in this synthetic flow; other adapters, credentialed providers, recovery, and rotation remain open.
- `FT-10`: two supported ingress dialects selected one healthy Ollama mock deployment in dry-run; real failover, multi-target health, and real provider operation remain open.
- Historical defects: 17 VERIFIED FIXED for scoped RC5 tests, 11 PARTIAL (DEF-028 included), 11 UNVERIFIED. These do not imply acceptance closure.
- DEF-045's inline trace message appeared again on the pre-dispatch capability failure while the trace was readable; no new finding was added.
- No product source was modified. External spend was USD 0. `GATE-REL-07` remains **BLOCKED**.
