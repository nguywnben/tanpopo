# CP35 — Embeddings capability guard and Team Admin model authorization

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Status:** Partial; one new confirmed defect, embeddings dispatch remains UNVERIFIED.  
**Product code:** unchanged.

## Work performed

- Issued a temporary Team Admin UI Virtual Key limited to `/v1/*` and created a temporary route to the existing synthetic Ollama deployment.
- Sent four cookie-free real HTTP `POST /v1/embeddings` requests. Traces persisted all four as HTTP 400 `capability_not_supported`, `EXACT`, `$0`, 0 ms, with no selected deployment. The mock provider log has zero `/v1/embeddings` requests. This is consistent with the existing logical model's chat-only capability declaration; it is not evidence of an embeddings implementation defect.
- Attempted to create a synthetic logical model declaring `embeddings` from the Team Admin's real Models screen. The page exposed **Register model** and the form accepted the capability selection, but the API returned HTTP 403 `insufficient_role_permission` and did not persist the model. SCR-05 explicitly grants `TEAM_ADMIN` model/deployment mutations. Recorded this as DEF-056.
- The available Org Owner storage state was not authenticated against this isolated RC6 database; an owner-assisted embeddings provisioning path was therefore unavailable. Do not infer embeddings dispatch success or failure from the capability-guard responses.
- Revoked the temporary key through the real Access & Keys UI; API readback is `REVOKED` v2. Deleted the temporary route (HTTP 204). FinOps shows no usage row for the key. No temporary route remains.
- Visually inspected the actual Models registration/error, Traces, and loaded FinOps screenshots. Traces show the four 400 rows; FinOps remains otherwise populated. No prompts, credentials, key secrets, or database material are in committed evidence.

## Result and coverage update

- **FAIL:** Team Admin model registration is denied despite SCR-05 and the visible Console action (DEF-056).
- **PASS, scoped guard only:** unsupported embeddings capability was rejected before dispatch, with zero fixture requests and zero cost.
- **UNVERIFIED:** successful embeddings request/response fidelity, response vector shape, usage extraction, settlement, route trace correlation, native adapter dialect, and embeddings persistence end to end.
- **BLOCKED:** provisioning a suitable model in this runtime through the documented Team Admin workflow until DEF-056 is remediated; a valid Org Owner session for this DB was unavailable.

## Evidence

- `evidence/CP35-embeddings-and-model-rbac-results.json`
- `evidence/CP35-embeddings-trace.png`
- `evidence/CP35-embeddings-finops.png`
- `evidence/CP35-models-before.png`
- `evidence/CP35-model-register-form.png`
- `evidence/CP35-model-register-result.png`
- `evidence/CP35-access-key-revoked.png`

Continue independent QA outside the blocked provisioning path. This checkpoint does not complete QA-004. GATE-REL-07 remains BLOCKED.

