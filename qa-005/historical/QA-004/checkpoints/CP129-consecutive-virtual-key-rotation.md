# CP129 — Consecutive Virtual Key rotations

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Status:** PASS (scoped)  
**Role / context:** Org Owner, exact-RC6 isolated runtime, real Chromium Access & Keys UI, cookie-free model discovery.

## Procedure and observed behavior

1. Created a fresh PROVIDER Virtual Key restricted to `GET /v1/models` and `qa004-cp121-priced-vision`.
2. Rotated once through the UI while holding the initial credential K0 and the once-displayed replacement K1 in process memory. Both returned HTTP 200 and only the allowed model.
3. Rotated again before the first grace interval elapsed, holding K0, K1, and replacement K2. The second UI/API action returned HTTP 200. K0 then returned HTTP 401 `invalid_virtual_key`; K1 and K2 returned HTTP 200 and only the allowed model. This demonstrates that a subsequent rotation replaces the previous grace credential, while the immediately previous and current credential remain accepted.
4. Gracefully revoked through the Console. Persistent readback reported `REVOKED`, version 4. K0, K1, and K2 all returned HTTP 401 `invalid_virtual_key`.
5. Audit readback contained two `virtual_key.rotated` events (sequences 726 and 727) and one `virtual_key.revoked` event (sequence 728), all linked to this key in the same organization partition.

The real Access & Keys screenshots were visually inspected: the post-first-rotation inventory showed `Rotating Dual Valid`; after the second rotation it remained dual-valid with an updated key prefix; after final revoke the row showed `Revoked`. The scopes shown remained `/v1/models`. No inference or budget mutation occurred. Secret values were only kept in process memory and are absent from screenshots/results.

A selector/setup retry before the successful run left temporary same-name test keys; these were all gracefully revoked and read back as `REVOKED` before the successful test. That harness correction is excluded from product findings. The successful CP129 test key is also revoked.

## Evidence

- [`sanitized results`](../evidence/CP129-consecutive-vkey-rotation-results.json)
- [first dual-valid state](../evidence/CP129-rotation-first-state.png)
- [second dual-valid state](../evidence/CP129-rotation-second-state.png)
- [final revoked state](../evidence/CP129-rotation-revoked-state.png)

## Coverage classification

PASS for two consecutive Org Owner rotations, credential-generation rollover (only K1/K2 remain valid after the second rotation), final invalidation of all generations, and target-linked audit records. This does not test concurrent rotations, stale ETag conflicts, exact expiration at 24 hours, restart during grace, other roles, or active stream behavior. The broader Virtual Key lifecycle coverage remains PARTIAL.

No Tanpopo implementation code was modified. **GATE-REL-07 remains BLOCKED.**
