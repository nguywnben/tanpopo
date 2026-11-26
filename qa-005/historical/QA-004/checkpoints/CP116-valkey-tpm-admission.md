# CP116 — Valkey-backed Virtual Key TPM admission

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)  
**Result:** PASS (scoped) for one pre-route TPM limit rejection and persisted graceful revocation. No new defect was assigned.

## Reproduction

Against the isolated post-shred PostgreSQL restore and three-primary Valkey cluster, an authenticated Owner created a project/environment-bound PROVIDER Virtual Key through the Admin API with `tpm_limit: 1` and one synthetic model that has no route or deployment. The one-time key value was retained only in process memory and is redacted from evidence.

The test sent one cookie-free `/v1/chat/completions` request to the separate exact-RC6 Data Plane on port 18138. The body contained a deliberately longer synthetic prompt. RC6 returned HTTP 429 `rate_limit_exceeded` with `Retry-After: 60`. Because the model has no configured deployment, no upstream exists and no provider egress or spend was possible. This demonstrates that the low TPM allowance rejects the request before route dispatch for this bounded case; it does not validate the token estimator's exact count or settlement behavior.

The Owner then used the real Access & Keys → Revoke key → Revoke (graceful drain) action. The UI showed its success banner; API readback returned `state: REVOKED`, version 2, and the retained `tpm_limit: 1`. The after-revocation screenshot was visually inspected. A browser automation pass first encountered an ambiguous page locator after the request result; a fresh authenticated page completed the revoke and readback. This was test-harness recovery, not a Tanpopo failure.

## Evidence and limits

Sanitized request and revoke outcomes are in [results](../evidence/CP116-valkey-tpm-admission-results.json); the visible final state is in [Access & Keys after revoke](../evidence/CP116-tpm-key-revoked.png).

Only one TPM threshold and one synthetic model were tested. Estimator precision, token reconciliation after successful upstream responses, window rollover, multi-node TPM fairness, out-of-order concurrency, and failure recovery remain UNVERIFIED. No source was changed. QA continues; **GATE-REL-07 remains BLOCKED.**
