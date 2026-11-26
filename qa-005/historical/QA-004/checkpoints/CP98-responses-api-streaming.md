# CP98 — Unified Responses API SSE lifecycle

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Coverage result:** scoped stream journey **PASS** on healthy runtime 18128; usage reconciliation **FAIL** under DEF-052; overall `/v1/responses` acceptance remains **PARTIAL**.

## Execution

The initial attempt used isolated runtime 18124. It returned HTTP 503 `no_compliant_deployment_available` before egress. Authenticated state and the actual Security → Kill Switch screenshot confirmed the active `EXTENSION_INSTALLATION` switch for the CP42 quarantined adapter (`QA-004 adapter quarantine acceptance probe`). The switch was intentionally left unchanged; this is a test-runtime blocker, not a product finding. The project budget was 1 exact + 1 estimated micro-USD against a 100 micro-USD limit, and the active healthy deployment/route were present. See `CP98-quarantined-runtime-block.json`, the runtime diagnostics, budget readback and inspected kill-switch screenshot.

The first request on healthy runtime 18128 then reached the synthetic upstream, but returned HTTP 404 because the original loopback fixture did not implement streaming Responses requests. No product conclusion was drawn from that fixture mismatch. A temporary loopback-only fixture response was added for the test, emitted valid OpenAI Responses SSE lifecycle events, and was restored to the original fixture after the run.

On healthy 18128, a real Org Owner used the Console to issue a PROVIDER-plane VKEY limited to `/v1/responses`. A cookie-free POST with `stream:true` returned HTTP 200 `text/event-stream; charset=utf-8`. The synthetic upstream received exactly one `/v1/responses` request and recorded no client authorization header. The browser consumed the full stream: `response.created`, output item/content part added, two text deltas, text/content/item done, `response.completed` with 7 input and 4 output tokens, then `[DONE]`. The successful response preserved the synthetic text `Hello from synthetic Responses stream.` The temporary key and earlier CP98 setup keys were gracefully revoked; persistent readback shows all CP98 keys REVOKED v2.

## Reconciliation and findings

The durable trace for the successful request is `trc_01m8arzhxve0099stc0bhsv9ky`; it is HTTP 200 and labeled `EXACT` with `$0`. Virtual Key Chargeback for the same key shows one exact request and `$0.000001`, matching its single upstream dispatch. The actual Traces and FinOps screenshots were visually inspected. This independently extends **DEF-052** to Responses SSE; no new defect ID is assigned.

The response did not include `X-Tanpopo-Usage-Accuracy`, although the OpenAPI lists that header on the 200 response. The schema does not mark it required, and streaming accuracy may not be known when headers are sent; header semantics remain **UNVERIFIED** pending a contract decision. The persisted settlement and FinOps record do classify usage as exact.

## Evidence

- Successful stream result, parsed SSE events, synthetic fixture request and trace: [results](../evidence/CP98-responses-stream-success-results.json)
- Persistent trace and Chargeback readback: [readback](../evidence/CP98-responses-stream-persistent-readback.json)
- Key revocation readback: [cleanup](../evidence/CP98-responses-stream-cleanup.json)
- [Traces screen](../evidence/CP98-responses-stream-traces-ui.png), [FinOps Chargeback screen](../evidence/CP98-responses-stream-finops-ui.png), and [revoked keys screen](../evidence/CP98-responses-stream-key-revoked.png)
- Quarantined-runtime block: [state](../evidence/CP98-quarantined-runtime-block.json), [diagnostics](../evidence/CP98-responses-stream-diagnostics.json), [budget](../evidence/CP98-responses-stream-budget-state.json), and visually inspected [Kill Switch screen](../evidence/CP98-killswitch-active-ui.png)
- The non-streaming original fixture's 404 is recorded in [setup attempt results](../evidence/CP98-responses-stream-results.json); it is a fixture-contract mismatch, not a product defect.

## Coverage status

- `/v1/responses` SSE framing and completion lifecycle: **PASS (scoped)**.
- Cookie-free authorization, restricted route grant and one upstream dispatch: **PASS (scoped)**.
- Revocation/persistent state: **PASS (scoped)**.
- Trace-versus-Chargeback reconciliation: **FAIL**, existing DEF-052.
- Usage-accuracy response header contract: **UNVERIFIED**.
- JSON variants are separately covered in CP97. Tools, multimodal inputs, SDK interoperability, cancellation, disconnects, retries, concurrency and restart/distributed behavior remain **UNVERIFIED**.

QA remains ongoing; **GATE-REL-07 remains BLOCKED.**
