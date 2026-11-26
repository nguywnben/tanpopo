# CP97 — Unified Responses API idempotency and settlement

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Scope:** isolated `QA004 Test Root Org / default / dev`, one synthetic Ollama target, real Console-issued route-limited keys, loopback fixture only. No Tanpopo source was changed.

## Result

The unified `POST /v1/responses` endpoint accepted a real cookie-free request and returned HTTP 200 with a JSON `response` object and synthetic output text. Repeating the identical body with the same idempotency key returned HTTP 200 with the identical response-body SHA-256 and did not increment the fixture request count. Reusing that idempotency key with a changed body returned HTTP 422 `idempotency_key_reused_mismatch`, also without another fixture call. The exact-replay/mismatch behavior passed for this endpoint slice.

Both temporary VKEYs were revoked; authenticated readback showed `REVOKED` v2. Persistent readback showed, per key, one initial 200 route dispatch, one 200 replay, and one 422 mismatch. Each of the six rows is labeled `EXACT` with `total_cost_micros: 0` in traces. Chargeback records one exact request and 1 micro-USD for each key, matching the two fixture dispatches and showing no replay double charge. The real Traces and FinOps screenshots were visually inspected: Traces presents all three rows per key as Exact/$0; Chargeback presents the actual $0.000001 exact settlement for each key.

This is another independent reproduction and endpoint extension of **DEF-052** (trace accuracy/cost diverges from durable settlement/FinOps), and supports the broader usage reconciliation issue DEF-051 tracks. No new defect ID is assigned. The successful JSON and replay behavior does not establish streaming behavior, input-item compatibility, tool execution, background response retrieval, cross-runtime durability, or interoperability with external SDKs.

## Evidence

- [Sanitized request results](../evidence/CP97-responses-results.json)
- [Persistent trace and chargeback readback](../evidence/CP97-responses-persistent-readback.json)
- [Real Traces screen](../evidence/CP97-responses-traces-ui.png)
- [Real FinOps Chargeback screen](../evidence/CP97-responses-finops-ui.png)
- [Real Access & Keys revoked-state screen](../evidence/CP97-responses-key-revoked.png)

Screenshots are captured from the actual browser session. The Traces crop showed the two 200 Responses rows and 422 mismatch per key, each with Exact metering; the FinOps crop showed one exact 1-micro-USD row for each of the two CP97 keys.

## Coverage status

- `POST /v1/responses` single JSON completion: **PASS (scoped)**.
- Same-key identical idempotency replay, changed-payload rejection, and per-key isolation: **PASS (scoped)**.
- Revocation and post-revocation state persistence: **PASS (scoped)**.
- Trace-to-settlement reconciliation: **FAIL**, consistent with DEF-052.
- Streaming, background retrieval/cancel, tools, multimodal/input variants, SDK compatibility, concurrent claims, expiration, restart/distributed replay: **UNVERIFIED**.

QA remains ongoing. **GATE-REL-07 remains BLOCKED.**
