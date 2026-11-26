# CP132 — Virtual Key rotation grace expiry

**Baseline:** frozen RC6 \`2eb7f6f6e5ba703d44fbd273f0245ba441de74e1\`  
**Status:** FAIL (scoped lifecycle-state cleanup and documented error-code discrepancy)  
**Role / context:** Org Owner, real Chromium Access & Keys UI, exact-RC6 isolated runtime, route/model-restricted synthetic key.

## Procedure and result

1. Issued a PROVIDER key limited to \`GET /v1/models\` and \`qa004-cp121-priced-vision\` through the Console.
2. Started rotation through the real UI. A test-only Playwright route interceptor rewrote the outgoing API grace value from the UI default of 86,400 seconds to the documented API minimum of 60 seconds; this enabled boundary testing without changing product code. The rotation returned HTTP 200. Both credentials immediately returned 200.
3. At 62.317 seconds after the rotation request, the old credential returned HTTP 401 \`invalid_virtual_key\`; the current credential remained HTTP 200.
4. At that point, authenticated key-list readback still reported \`ROTATING_DUAL_VALID\`, version 2, and the real Access & Keys inventory still displayed “Rotating Dual Valid”. No \`virtual_key.rotation_finalized\` audit event was present; only \`virtual_key.rotated\` #740 had occurred.
5. The expected cleanup method is present in RC6 source and is described as clearing elapsed previous-key grace data and returning state to \`CREATED_ACTIVE\` ([\`FinalizeExpiredRotations\`](../../internal/iam/vkey/service.go#L1079), implementation at [line 1080](../../internal/iam/vkey/service.go#L1080)). Repository-wide Go-source search found no production call site. The frozen source unit test explicitly expects finalization after advancing past grace ([test](../../internal/iam/vkey/service_test.go#L216)).
6. The key was gracefully revoked after observation. Readback was \`REVOKED\`, version 3; both credentials returned 401. Audit then contained \`virtual_key.revoked\` #741. The post-expiry screenshot was captured before manual revoke and shows the stale dual-valid label; a separate final screenshot shows the revoked state.

The authorization boundary held: the old credential was rejected after expiry and the current credential continued to work. The defect is stale lifecycle state and missing cleanup/audit, not continued authorization. An initial exploratory CP132 run is retained in evidence as `CP132-dual-valid-before-expiry.png` and `CP132-old-expired-new-active.png`, but its stdout/result record was not captured; those images are not used for timing or HTTP assertions. Its temporary key was revoked. Only the repeatable CP132 retest is classified. No inference or budget mutation occurred.

## Contract discrepancy

Product acceptance says \`401 virtual_key_expired\` ([TP-IMP-0009 AC-2](../../docs/planning/units/TP-IMP-0009.md#L41)); the contract-test matrix says \`401 key_expired\` ([AUTH-05](../../docs/quality/contract-test-matrix.md#L100)). The observed Data Plane error was \`401 invalid_virtual_key\`, with the generic invalid/expired/revoked message. This may be intentional anti-enumeration policy, so a differentiated response must not be introduced without a security decision. Resolve the public contract while preserving its security intent.

## Evidence

- [sanitized results](../evidence/CP132-rotation-expiry-results.json)
- [dual-valid state before expiry](../evidence/CP132-retest-dual-valid-before-expiry.png)
- [expired old credential / stale state](../evidence/CP132-retest-old-expired-new-active.png)
- [final revoked state](../evidence/CP132-retest-final-revoked.png)
- [initial exploratory screenshots (not used for result assertions)](../evidence/CP132-dual-valid-before-expiry.png), [post-grace inventory](../evidence/CP132-old-expired-new-active.png)

All three screenshots were visually inspected. Plaintext credentials were held in memory only and are absent from evidence.

## Acceptance criteria

1. At/after configured grace expiry, reject the previous credential while continuing to accept the active credential.
2. Automatically run expired-rotation cleanup in the served runtime: remove previous credential state, clear expiry timestamp, transition inventory/API to \`CREATED_ACTIVE\`, increment version, and emit the documented \`virtual_key.rotation_finalized\` system audit event within a stated bounded interval.
3. Persist finalized state across restart and control/data-plane nodes; test expiry boundary races with concurrent rotations, updates and revocation.
4. Resolve the error-code discrepancy between AC-2, AUTH-05 and the generic Data Plane response. Preserve uniform authentication failures unless Security confirms a safe differentiated contract; update requirements and contract tests accordingly.

No Tanpopo source code was modified. **GATE-REL-07 remains BLOCKED.**
