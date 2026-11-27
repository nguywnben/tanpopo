# CP58 — DEF-017 Playground request-to-trace correlation

**Source:** frozen unsigned RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`.
**Artifact:** exact-source Linux/amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; `vcs.modified=false`.
**Runtime:** isolated SQLite Control/Data Plane at `127.0.0.1:18145`; local deterministic OpenAI-compatible SSE fixture at `127.0.0.1:19090`; Chromium Linux; synthetic `ORG_OWNER`; `QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev`.
**Disposition:** **PASS (scoped)** for historical DEF-017. No external provider or paid usage. The campaign remains in progress; `GATE-REL-07` remains BLOCKED.

## Reproduction and observation

1. In the actual Extensions Console, stage `@tanpopo/openai-compatible`, review its official signature and permission diff, and activate it. The permission diff includes loopback/private-IP access; the test connection independently narrowed network access to `127.0.0.1/32`.
2. In Providers, create `CP58 OpenAI SSE loopback fixture` at `http://127.0.0.1:19090`, explicitly choose `SELF_HOSTED_INFRA` / `ON_PREM`, and set the allowed CIDR to `127.0.0.1/32`. The actual probe returned HTTP 200, `valid=true`, detected `qa005-model`, and moved the connection to ACTIVE.
3. In Models & Deployments, configure `qa005-model` as a zero-rate self-hosted deployment and verify `ACTIVE_HEALTHY`. In Routes & Policies, create `qa005-playground-openai` targeting that deployment.
4. In actual Chromium Playground, set a 5,000,000 micro-USD ephemeral-session cap and submit a synthetic prompt. The real UI rendered `QA005 streamed fixture response`; its Execution traces card displayed `trc_01m8d8fs6be00ab3qe5m6bv25m`, HTTP 200, `$0.000000`, and Exact.
5. The browser made three `GET /admin/v1/traces/trc_01m8d8fs6be00ab3qe5m6bv25m` lookups, each HTTP 200. An independent authenticated trace read returned the same ID and route, status 200, null error, Exact usage, zero cost, 3 ms TTFT, 5 ms duration, and no debug payload. Clicking **Inspect trace** opened that same record; the screenshot shows the identifier, status and trace waterfall together.
6. Revoked the temporary Playground keys in the real Access & Keys UI. API readback confirmed the successful-session key transitioned `CREATED_ACTIVE` v1 → `REVOKED` v2; UI showed zero active streams severed. The two earlier exploratory Playground session keys were also revoked. No one-time plaintext key is in repository evidence.

## Classification and retained contradictory evidence

The historical `req_…` versus persisted `trc_…` mismatch did not reproduce on this successful, protocol-matched current-baseline path. The Playground used `trc_…` consistently for the card, repeated direct metadata lookups, and Inspect Trace. This is a scoped PASS only: one runtime, Chromium, Owner, SQLite, adapter, route and short stream. Other roles, adapters, browsers, cancellation, long streams, concurrency and browser restart remain UNVERIFIED, and historical failures are retained.

During setup, an earlier attempt used the Ollama adapter against the native Ollama fixture for a Playground OpenAI-compatible streaming call. It returned HTTP 502 `upstream_malformed_response`; its trace ID was still `trc_…`, direct lookup returned 200, and Inspect trace opened. The fixture only served native Ollama `/api/*` endpoints, so this was a fixture/protocol mismatch, not evidence of a Tanpopo trace-correlation defect. The OpenAI-compatible fixture and adapter above were installed/configured only within this isolated QA tenant and then passed. No product code changed.

Two earlier Playground calls at the historically exhausted project scope returned HTTP 429 `project_budget_exceeded` / Unsettled. Both rendered `trc_…`, direct trace lookup returned 200 and Inspect trace resolved. These were budget-rejection paths; no upstream inference was attempted.

## Evidence

- Sanitized workflow, artifact/runtime identity, fixture permissions/framing, exact trace readback, classification and limitations: [CP58 evidence](../evidence/CP58-def017-playground-trace-correlation.json).
- Visually inspected: `CP58-openai-extension-review.png`, `CP58-openai-connection-form.png`, `CP58-openai-connection-created.png`, `CP58-openai-probed.png`, `CP58-openai-deployment-form.png`, `CP58-openai-deployment-created.png`, `CP58-openai-route-form.png`, `CP58-openai-route-created.png`, `CP58-openai-playground-prompt.png`, `CP58-openai-playground-result.png`, `CP58-openai-playground-inspect-trace.png`, and `CP58-all-playground-session-keys-revoked.png`. Error-path screenshots `CP58-playground-result.png` and `CP58-playground-budgetless-result.png` preserve the 429 states; `CP58-playground-success-result.png` preserves the Ollama fixture mismatch.

No release decision is made. The coverage campaign continues.
