# CP130 — Stale Virtual Key rotation request

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Status:** PASS (scoped)  
**Role / context:** Org Owner, isolated exact-RC6 runtime, real Chromium Console and cookie-free Provider requests.

## Procedure and result

1. Created a PROVIDER key scoped to `GET /v1/models` and `qa004-cp121-priced-vision` at version 1.
2. Rotated it through the real Access & Keys UI. The request returned HTTP 200 and ETag `"v2"`; readback was `ROTATING_DUAL_VALID`, version 2.
3. Replayed the captured rotation request with its original stale `If-Match: "v1"` and session-bound CSRF proof. RC6 returned HTTP 409 `optimistic_lock_conflict`, stated that the current revision is v2, and returned ETag `"v2"`.
4. Read back the key as `ROTATING_DUAL_VALID`, version 2. Both credentials valid for the current grace interval independently returned HTTP 200 and only the allowed model. The stale replay did not change state or invalidate either credential.
5. Gracefully revoked through the UI. Both credentials then returned HTTP 401 `invalid_virtual_key`; persistent state was `REVOKED`, version 3. Audit sequences 730 and 731 link the rotate and revoke to the same key and org partition.

The two real Console screenshots were visually inspected. The first shows the key in dual-valid state; the second shows the green revoke confirmation and `Revoked` row. No inference or budget mutation occurred; plaintext secrets stayed in process memory and were not included in evidence. The key is revoked.

## Evidence

- [`sanitized results`](../evidence/CP130-stale-rotation-conflict-results.json)
- [post-conflict dual-valid inventory](../evidence/CP130-stale-rotation-conflict-state.png)
- [final revoked inventory](../evidence/CP130-stale-rotation-revoked.png)

## Coverage classification

PASS for one stale-ETag rotation replay concurrent with a completed rotation: exact conflict response, unchanged persistent version/state, continued validity of both current credentials, and later revoke. This does not cover simultaneous live writes under load, other state mutations, other roles, or grace expiry/restart semantics. The broader key lifecycle remains PARTIAL.

No product code was modified. **GATE-REL-07 remains BLOCKED.**
