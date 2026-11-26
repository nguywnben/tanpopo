# CP99 — Embeddings batch and input validation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Coverage result:** valid three-item batch and empty-input rejection **PASS (scoped)**; display-name persistence **FAIL** under existing DEF-057; batch embeddings acceptance overall **PARTIAL**.

## Execution

Used healthy isolated runtime 18128 in `QA004 Test Root Org / default / dev`, an Org Owner session, and the deterministic loopback Ollama fixture. Initial empty-input probes were rejected with HTTP 400 before egress. A valid-input attempt was initially blocked with 503 `no_compliant_deployment_available` because the test environment had no embeddings deployment/route; no defect was inferred. The Owner then registered a synthetic embeddings-capable logical model, a healthy ON_PREM zero-rate deployment, and a route through the real Console. This setup allowed a meaningful request without an external provider or provider expenditure. Deployment creation returned 201 and the real Models screen showed it `ACTIVE_HEALTHY`; the initial evidence script's GET filter expected a `logical_model_slug` field and yielded an empty filtered list. The UI row, route target readback, and successful inference establish that the deployment was active. The filtered empty result was a harness query limitation, not a product defect.

The model form accepted display name `QA004 CP99 Embedding Batch`, slug `qa004-cp99-embedding-batch`, and the `embeddings` capability. POST returned 201 and the Console success banner used the slug. Authenticated list readback returned the slug for `display_name`; after refresh the Logical Models screen shows the slug in both model-name lines. This independently reproduces existing **DEF-057**; no duplicate defect was added.

A real Console-issued PROVIDER key restricted to `/v1/embeddings` was used from browser `fetch` with cookies omitted. An empty array returned HTTP 400 `invalid_request_error`; the fixture request count did not change. A three-string batch returned HTTP 200 JSON with three distinct three-dimensional vectors at indexes 0, 1, and 2, 15 prompt tokens, and `X-Tanpopo-Usage-Accuracy: EXACT`. The fixture recorded exactly one array request and no authorization header. The successful request was persisted in Traces. Three temporary CP99 keys were revoked; a replay using the final revoked key returned HTTP 401 without a second fixture call.

The batch had one synthetic upstream dispatch at zero configured rate. Chargeback recorded one exact zero-cost request. Traces labeled that success `EXACT`/$0, so this zero-rate case does not expand the existing nonzero-cost DEF-052 discrepancy. Actual screenshots of the loaded Traces row, Chargeback row, revoked-key inventory, and refreshed Logical Models catalog were visually inspected. The first Traces capture showed a loading state and was replaced with the loaded screenshot before checkpointing.

## Cleanup and environment state

The temporary route and deployment were deleted using their real Console action menus and confirmed absent in authenticated readback. The original `/tmp/qa004-ollama-mock.py` loopback fixture was restored and `/api/tags` returned `qa004-mock`. All three CP99 keys read back REVOKED v2. RC6 exposes no logical-model deletion control in the Console and the admin router has no logical-model DELETE endpoint, so the unused synthetic logical-model registry row remains; it has no route or deployment and is not serving traffic. No implementation or database was modified.

## Evidence

- [Batch request, validation, replay, shape, traces and Chargeback](../evidence/CP99-embeddings-batch-results.json)
- [Model registration readback](../evidence/CP99-embeddings-model-readback.json) and [post-cleanup state](../evidence/CP99-cleanup-readback.json)
- Visually inspected [model form](../evidence/CP99-embeddings-model-form.png), [model registration](../evidence/CP99-embeddings-model-created.png), [refreshed model catalog](../evidence/CP99-embeddings-model-readback-ui.png), [loaded Traces](../evidence/CP99-embeddings-batch-traces-ui.png), [Chargeback](../evidence/CP99-embeddings-batch-finops-ui.png), and [revoked keys](../evidence/CP99-embeddings-batch-key-revoked.png)
- Setup screens and readback: [deployment form](../evidence/CP99-embeddings-deployment-form.png), [deployment creation](../evidence/CP99-embeddings-deployment-created.png), [deployment readback](../evidence/CP99-embeddings-deployment-readback.json), [route options](../evidence/CP99-embeddings-route-options.png), [route creation](../evidence/CP99-embeddings-route-created.png), [route readback](../evidence/CP99-embeddings-route-readback.json)
- Cleanup confirmations: [route deletion](../evidence/CP99-route-cleanup.png), [deployment deletion](../evidence/CP99-deployment-cleanup.png), and authenticated [final readback](../evidence/CP99-cleanup-readback.json)

## Coverage status

- `/v1/embeddings` valid string-array batch, response ordering/shape and one upstream dispatch: **PASS (scoped)**.
- Empty batch validation before upstream egress: **PASS (scoped)**.
- Cookie-free, route-restricted provider authorization; key revocation and post-revocation denial: **PASS (scoped)**.
- Model display-name persistence through API and UI refresh: **FAIL**, existing DEF-057.
- Nonzero-rate usage reconciliation: **UNVERIFIED**; this test's synthetic deployment was zero-rate.
- `encoding_format`, requested dimensions, large batches, token-array input, malformed/mixed types, limits, timeouts, retry/fallback, cross-role behavior, external provider and SDK compatibility remain **UNVERIFIED**.

QA continues; this checkpoint is not full embeddings acceptance. **GATE-REL-07 remains BLOCKED.**
