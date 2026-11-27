# CP37 — Cookie-free `/v1/models` discovery and Virtual Key route boundary

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Result:** PASS for this narrow endpoint and route-grant slice; campaign remains PARTIAL.  
**Product code:** unchanged.

## Journey and result

- A Team Admin issued a PROVIDER-plane Virtual Key through the real Access & Keys screen with route grant `/v1/models` only.
- Two cookie-free `GET /v1/models` requests returned HTTP 200. The response listed the one permitted logical model with context/output limits, `DECLARED` assurance, and `x_tanpopo_capabilities`; embeddings was correctly false for the existing chat model.
- A cookie-free `POST /v1/chat/completions` with the same path-restricted key was denied HTTP 403 `scope_or_plane_denied`. The request was rejected at the Virtual Key boundary.
- Traces persisted both model-list reads as HTTP 200, `EXACT`, `$0`, 0–1ms. FinOps returned no usage row for this zero-cost metadata-only key.
- After UI revocation the key was `REVOKED` v2; a post-revocation `GET /v1/models` returned HTTP 401 `invalid_virtual_key`.
- No model/route configuration was modified. The temporary one-time secret was removed. A separately captured Traces screenshot visibly shows both `/v1/models` rows; the Access & Keys screenshot shows the key revoked. Both were visually inspected.

## Evidence

- `evidence/CP37-model-discovery-results.json`
- `evidence/CP37-model-discovery-traces.png`
- `evidence/CP37-model-key-form.png` — shows the `/v1/models`-only route grant before key creation.
- `evidence/CP37-model-key-revoked.png`

## Coverage limits

This does not verify native `/v1/models` dialects, OpenAI SDK interoperability, invalid model filters, or every role and tenant boundary. Successful embeddings remain UNVERIFIED under CP35/DEF-056. No new defect was identified in this slice. GATE-REL-07 remains BLOCKED.

