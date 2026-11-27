# CP95 — Native same-dialect mismatch guard

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Outcome:** PASS (scoped) — a credentialed Anthropic native request targeting the existing Ollama-only route failed fast with contractual HTTP 400 `dialect_mismatch`. No new defect assigned.
**Gate:** GATE-REL-07 remains BLOCKED. Product source unchanged.

## Corrected scope and actual route

CP92's first Anthropic attempt was made in `QA-004 Isolated Canary`, a project/environment with no provider route. Its 503 `no_compliant_deployment_available` was a scope mismatch and did not test dialect compatibility. In CP95, I switched through the real workspace picker to `QA004 Test Root Org / default / dev`, confirmed `qa004-loopback` had one route target, and used data region `ON_PREM`.

The real Access & Keys form issued an environment-bound PROVIDER key restricted to `/native/anthropic/*`. A cookie-free authenticated `POST /native/anthropic/v1/messages` named the existing logical model `qa004-mock`. That model's only available route target is the Ollama native deployment. RC6 returned HTTP 400 `dialect_mismatch` (`invalid_request_error`), matching the OpenAPI and TP-IMP-0017 contract. The request was rejected before any Anthropic upstream was configured or contacted. The temporary key was revoked (HTTP 200, persisted `REVOKED` v2).

Evidence: [correct default/dev scope and route](../evidence/CP95-native-mismatch-results.json), visually inspected [scope picker](../evidence/CP95-default-dev-native-mismatch-scope.png) and [revoked key inventory](../evidence/CP95-native-key-revoked.png). CP92's route-grant 403 and encoded traversal 403 remain scoped PASS results. The original 503 is preserved in [CP92 probe evidence](../evidence/CP92-native-dialect-results.json) but reclassified as a no-route scope setup result, not a provider failure.

## Coverage boundary

This verifies the negative same-dialect guard only. It does not verify Anthropic or Gemini SDK compatibility, successful native dispatch, lossless vendor JSON/SSE, auth brokerage, usage extraction, fallback, or production signing. Anthropic/Gemini positive flows remain UNVERIFIED; no product defect is assigned. QA continues; **GATE-REL-07 remains BLOCKED**.
