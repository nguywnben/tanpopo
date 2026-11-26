# CP40 — Owner-provisioned embeddings end to end

**Status:** PASS for one scoped synthetic embeddings workflow; overall provider interoperability remains PARTIAL.
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`.
**Product source:** unchanged.

## Objective and setup

Continue the embeddings capability investigation without treating the Team Admin authorization failure as a reason to weaken authorization. In the test-root runtime on `18124`, an authenticated Org Owner registered an embeddings-capable logical model, created a healthy self-hosted zero-rate deployment on the existing isolated synthetic Ollama connection, configured a route, and issued a path-scoped PROVIDER Virtual Key through the Console.

The key allowed the PROVIDER plane and `/v1/embeddings`; it was revoked after the request. The provider endpoint was a deterministic local fixture, not a real external provider. The synthetic adapter was installed under the isolated test root used in earlier checkpoints; its test signature is not a Tanpopo production release signature.

## Reproduction and observed results

1. In the real Chromium Console as Org Owner, register model `qa004-embedding-vectors` with `embeddings=true`.
2. Add deployment `qa004-mock` on the healthy `QA004 synthetic Ollama` connection. Set all rates to zero and explicitly select the self-hosted zero-rate option.
3. Create route `qa004-embedding-vectors` targeting that deployment.
4. Issue a PROVIDER Virtual Key restricted to `/v1/embeddings`.
5. Without Console cookies, send `POST /v1/embeddings` with model `qa004-embedding-vectors` and a single synthetic text input.
6. Inspect the fixture record, response, persisted trace, and Chargeback view. Revoke the key and replay the request.

Observed:

- Model create returned HTTP 201 with embeddings capability. API readback and catalog state retained the capability but returned the slug rather than the submitted human display name, independently reproducing DEF-057.
- Deployment create returned HTTP 201 and `ACTIVE_HEALTHY`, with the selected active connection and self-hosted zero-rate pricing.
- The cookie-free embeddings request returned HTTP 200 and one vector `[0.01, 0.02, 0.03]`, `prompt_tokens=5`, `total_tokens=5`. The fixture received exactly one `/v1/embeddings` request for `qa004-mock`; the fixture has no provider credential and recorded no Authorization header.
- Durable trace `trc_01m89q0xsge00ac0xbr6md0p0j` records `PROVIDER`, route `qa004-embedding-vectors`, HTTP 200, `EXACT`, `$0.000000`, and `has_debug_payload=false`. The response's generic request header was a distinct `req_...` value; the Console's trace request identifier is `trc_...`, consistent with prior CP07/CP36/CP38 evidence that request and trace IDs are distinct.
- Chargeback grouped by Virtual Key recorded 1 exact request, 0 estimated requests, 0 unmetered requests, and exact/estimated cost of `$0.000000` for this key. No DEF-052 discrepancy occurred for this zero-rate successful call.
- Key revocation persisted state `REVOKED`, version 2. Cookie-free replay returned HTTP 401 `invalid_virtual_key`.
- The temporary route was deleted after evidence capture. The isolated model and deployment remain in the QA runtime as documented test data; no source code was changed. The temporary key plaintext remained outside the repository and was deleted after replay.

The route editor displayed “At least one target deployment does not declare tool_calling capability” while configuring this embeddings-only route. It did not prevent creation or dispatch. The warning is recorded as an unclassified UX observation: current evidence does not establish that this is incorrect under the route screen's full contract, so no defect ID is assigned.

## Evidence

- `evidence/CP40-embedding-model-form.png`, `CP40-embedding-model-created.png`, `CP40-embedding-model-result.json`
- `evidence/CP40-embedding-deployment-form.png`, `CP40-embedding-deployment-form-filled.png`, `CP40-embedding-deployment-created.png`, `CP40-embedding-deployment-result.json`
- `evidence/CP40-embedding-route-form.png`, `CP40-embedding-route-filled.png`, `CP40-embedding-route-created.png`, `CP40-route-deleted.png`
- `evidence/CP40-embedding-key-filled.png`, `CP40-embedding-key-active.png`, `CP40-embedding-key-revoke-confirm.png`, `CP40-embedding-key-revoked.png`, `CP40-embedding-key-revoked.json`
- `evidence/CP40-embedding-inference.json`, `CP40-embedding-trace-finops.json`, `CP40-embedding-traces.png`, `CP40-embedding-trace-detail.png`, `CP40-embedding-finops.png`
- `evidence/CP40-models-after-inference.png`, `CP40-route-cleanup.json`

Actual Console screenshots were inspected. No screenshot containing the one-time plaintext key is retained in the QA repository.

## Limits and remaining coverage

This is one successful Org Owner path against one local synthetic Ollama fixture, one string input, one vector, one route, and one project/environment. It does not prove Team Admin provisioning, external-provider interoperability, batch embeddings, invalid input handling, dimension/format limits, cancellation, rate limits, retry/timeout, multimodal, reranking, multi-provider fallback, or production-signed adapter readiness. DEF-056 remains FAIL for the documented Team Admin model write; successful Owner provisioning is not a substitute for that role boundary. DEF-057 remains FAIL for model display-name persistence. GATE-REL-07 remains BLOCKED.
