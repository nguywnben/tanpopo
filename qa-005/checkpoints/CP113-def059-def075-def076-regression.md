# CP113 — DEF-059, DEF-075 and DEF-076 exact-RC7 retest

**Status:** PASS scoped for DEF-059 and DEF-076; FAIL for the DEF-075 rotation-finalization lifecycle.

**Frozen source:** `737079ddb847d97dbcf832f610ed0048556cb65b`  
**Runtime:** exact-source Linux/amd64 binary, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite clone at `127.0.0.1:18164`; local deterministic Ollama-compatible fixture at `127.0.0.1:19096`; synthetic Org Owner.  
**Browser:** Chromium 156.0.8078.79. No external provider credentials, production signers, or paid provider calls were used.

## DEF-059 — quarantine error contract: PASS (scoped)

1. Through the real Data Plane, `/native/ollama/api/generate` and `/v1/chat/completions` were each sent a valid request through their existing loopback routes while `@tanpopo/ollama` was active. Both returned HTTP 200 (request IDs are in the sanitized evidence).
2. The Org Owner quarantined the actual `@tanpopo/ollama` installation in Extensions and confirmed the exact package ID in the modal. Persistent readback reported `QUARANTINED`.
3. Replaying the same authorized requests returned HTTP 503 `extension_quarantined`, type `extension_error`, `retryable:false` on both native and provider-compatible ingress.
4. `GET /admin/v1/traces/{trace_id}` persisted HTTP 503 and `extension_quarantined` for both planes, with `deployment_id:null` and zero cost. The real Traces UI lookup displayed the same code and zero-cost result for each request; both detail screenshots were visually inspected.
5. The adapter was restored through the real unquarantine, permission-review and approve/activate flow. It returned to `ENABLED_ACTIVE`; a post-restore native canary returned HTTP 200. All CP113 temporary keys were revoked and readback showed no active CP113 key. Both Org audit chains verified intact, including 663 events for the primary QA Org; the quarantined, unquarantined, permission-approval and enable events are present.

The historical RC6 error mismatch is not reproduced on this RC7 slice. This is a scoped pass for the two tested ingress paths. The routes each had one target; a compliant failover-target scenario and an independent residency-ineligible control were not tested. No fixture request counter was available for this run, so the evidence makes no exact counter claim; the matching traces have no selected deployment and only a Policy stage, consistent with rejection before dispatch.

## DEF-076 — expired-key response contract: PASS (scoped RC7 contract)

On the isolated SQLite clone, a newly issued PROVIDER key was first validated against a loopback route, then rotated through the real Console with the exact five-minute grace duration from the frozen `TP-IMP-0009` criterion. The test correlated the old credential digest with `previous_key_hash` and the successor with `active_key_hash`, and confirmed the key scope matched the route. Both old and successor credentials returned HTTP 200 at approximately T+270 seconds. At T+301.086 seconds, the old credential returned HTTP 401 `invalid_virtual_key`, while the successor returned HTTP 200. The test key was revoked and the final admin readback showed no active CP113 test keys.

The RC6 QA-004 report’s claimed three-way documentation disagreement (`virtual_key_expired`, `key_expired`, and runtime `invalid_virtual_key`) is preserved historically. It does not exist in the frozen RC7 documents read for this campaign: TP-IMP-0009 AC-2, AUTH-05 and the API compatibility table consistently require the uniform anti-enumeration `invalid_virtual_key` response. The live RC7 response matches that contract. This does not revise any specification.

## DEF-075 — post-expiry rotation finalization: FAIL (fresh RC7 observation; conflicting evidence retained)

The same five-minute rotation boundary test observed the old-key authorization cutoff, but the immediate post-expiry Admin and SQLite reads remained `ROTATING_DUAL_VALID` v2, the grace deadline remained set, and `virtual_key.rotation_finalized` was absent. The key was revoked for cleanup. This independently repeats the lifecycle failure first recorded at CP69. CP69 also preserves other exact-source runs in which finalization occurred; those conflicting observations are not discarded. The combination means post-expiry lifecycle state and reaper timing remain nondeterministic/unresolved and DEF-075 remains open. The expired-key error contract itself is separately scoped PASS under DEF-076.

## Coverage limits

One Linux SQLite clone, one synthetic Owner and one local Ollama-compatible provider were used. Native and OpenAI-compatible quarantine responses, their persisted traces, the extension restore path, 300-second key expiry and audit verification were exercised. No PostgreSQL/Valkey, multi-node failover, alternative role, other browser engine, residency-denial control, successful alternate compliant failover, real-provider interoperability, paid use, production signature or release signing was tested. Native Safari remains unverified. Historical QA-001 through QA-004 records remain intact.

## Evidence

- Sanitized result bundle: [CP113 evidence](../evidence/CP113-def059-def075-def076.json)
- Real Traces screenshots: `evidence/screenshots/CP113-def059-native-trace-detail.png`, `evidence/screenshots/CP113-def059-provider-trace-detail.png`
- Quarantine/restore screenshots: `evidence/screenshots/CP113-quarantined-state.png`, `evidence/screenshots/CP113-def059-permission-review.png`, `evidence/screenshots/CP113-def059-restored-disabled.png`
- Rotation UI screenshot: `evidence/screenshots/CP113-rotation-started.png`
- Frozen acceptance: `docs/planning/units/TP-IMP-0009.md:41`, `docs/quality/contract-test-matrix.md:100`, `docs/specifications/api-protocol-compatibility.md:79`
- Historical findings remain at [DEF-059](historical/QA-004/reports/DEF-059-quarantine-traffic-uses-generic-deployment-error.md), [DEF-075](historical/QA-004/checkpoints/CP69-def075-rotation-grace-boundary.md), and [DEF-076](historical/QA-004/reports/DEF-076-expired-key-error-contract-conflict.md).

QA-005 continues. No readiness verdict is assigned at CP113. `GATE-REL-07` remains BLOCKED.
