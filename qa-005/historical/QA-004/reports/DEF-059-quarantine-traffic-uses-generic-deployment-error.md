# DEF-059 — Quarantined-extension traffic reports a generic deployment error

**Severity:** Moderate
**Status:** FAIL
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Affected workflow:** native and OpenAI-compatible inference, incident response, client retry and monitoring.

## Summary

RC6 correctly prevents traffic from reaching a quarantined extension, but native Ollama and provider-compatible inference return `no_compliant_deployment_available` (`residency_unavailable_error`, retryable) instead of the documented HTTP 503 `extension_quarantined` (`extension_error`, non-retryable). Persisted traces use the same generic code. The fail-closed behavior passes; this defect is the diagnostic/error-contract mismatch.

## Reproduction

1. In the isolated test-root runtime `127.0.0.1:18124`, use the existing synthetic Ollama model/route and a cookie-free PROVIDER-plane key.
2. Quarantine `@tanpopo/ollama` through the real Console.
3. POST `/native/ollama/api/generate` with valid key and request body. Observe HTTP 503 `no_compliant_deployment_available`.
4. POST `/v1/chat/completions` with the same authorized test setup. Observe HTTP 503 `no_compliant_deployment_available`.
5. Confirm the synthetic fixture call count remains zero and persisted Traces show 503 with the generic code for both planes.

## Expected and actual behavior

**Expected:** `docs/specifications/api-protocol-compatibility.md` defines HTTP 503 `extension_quarantined`, type `extension_error`, `retryable:false` when the backing extension is `QUARANTINED`.

**Actual:** both requests return HTTP 503 `no_compliant_deployment_available`, type `residency_unavailable_error`, `retryable:true`; the traces persist `no_compliant_deployment_available`. No fixture call or usage occurs, so the security containment itself is effective.

## Business impact

Clients and operators cannot distinguish emergency quarantine from residency/policy eligibility failure. A generic retryable response may trigger retries or fallback behavior that is inappropriate during a security incident, while dashboards and alert routing lose the explicit quarantine signal.

## Evidence

- `../checkpoints/CP42-extension-quarantine-recovery.md`
- `../evidence/CP42-quarantine-traffic-details.json` — both response bodies, request IDs, fixture count 0, and persisted native/provider traces.
- `../evidence/CP42-quarantine-failure-traces.png` and `CP42-quarantine-failure-trace-detail.png` — visually inspected real Traces screen and detail drawer.
- Expected error semantics: `docs/specifications/api-protocol-compatibility.md`, HTTP 503 row for `extension_quarantined`.

## Facts and root-cause hypothesis

**Facts:** the installation readback is QUARANTINED; both endpoint families return the generic no-compliant-deployment code; traces persist it; fixture egress remains zero. The API compatibility contract explicitly names `extension_quarantined` for a quarantined backing extension.

**Root-cause hypothesis:** route candidate filtering may remove quarantined deployments before error selection, leaving the resolver unable to distinguish quarantine from other causes of zero eligible deployments. This is consistent with the response but requires engineering confirmation.

## Acceptance criteria

1. For an otherwise eligible request whose backing extension is quarantined, return HTTP 503 `extension_quarantined`, type `extension_error`, and `retryable:false` on native and provider-compatible APIs.
2. Persist the same error code/status in Traces and any correlated operational event while retaining metadata-only privacy.
3. Do not dispatch to a quarantined extension or silently activate it; fixture call count and usage stay zero.
4. Keep `no_compliant_deployment_available` for actual residency/policy ineligibility that is not caused by quarantine.
5. Verify healthy eligible failover targets still behave according to the documented security policy and error-selection rules.
