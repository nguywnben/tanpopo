# CP131 — Virtual Key grant change during rotation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Status:** PASS (scoped)  
**Role / context:** Org Owner, real Chromium Access & Keys UI, isolated exact-RC6 runtime.

## Procedure and result

1. Created a PROVIDER key limited to `GET /v1/models` and model `qa004-cp121-priced-vision`.
2. Rotated it through the UI. Both pre-rotation and replacement credentials returned HTTP 200 for `/v1/models` during the dual-valid interval.
3. While in `ROTATING_DUAL_VALID`, opened Edit plane & route grants and changed the allowed route from `/v1/models` to `/v1/chat/completions`. The UI showed the new pattern and its save confirmation; persistent metadata read back the new grant and key version 3.
4. Repeated cookie-free `GET /v1/models` using each credential. Both were denied with HTTP 403 `scope_or_plane_denied`, showing that the updated route grant applies to both credential generations. No allowed route was invoked, so no inference or spend occurred.
5. Gracefully revoked the key. Both credentials then returned HTTP 401 `invalid_virtual_key`, and readback showed `REVOKED`, version 4.
6. Audit records `virtual_key.rotated` (#733), `virtual_key.updated` (#734), and `virtual_key.revoked` (#735) link to the same key and organization partition.

The real edit dialog and post-save inventory screenshots were visually inspected. The displayed grant changed to `/v1/chat/completions`, the key remained `Rotating Dual Valid`, and final state displayed `Revoked`. Plaintext credentials are absent from the evidence and result file. The key is revoked.

## Evidence

- [`sanitized results`](../evidence/CP131-rotation-scope-edit-results.json)
- [grant edit dialog](../evidence/CP131-edit-grants-dialog.png)
- [updated scope while dual-valid](../evidence/CP131-dual-key-updated-scope.png)
- [revoked key](../evidence/CP131-dual-key-revoked.png)

## Coverage classification

PASS for one route-grant change during dual-valid rotation, with updated scope applied equally to old/new credentials, persistent version/readback, linked audit, and revocation. Other planes, model changes, project/environment changes, stale update conflicts and concurrent grant writes remain unverified. The broad key lifecycle/access area remains PARTIAL.

No product code was modified. **GATE-REL-07 remains BLOCKED.**
