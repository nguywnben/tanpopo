# CP128 — Virtual Key rotation grace window and revocation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Status:** PASS (scoped)  
**Roles / context:** Org Owner in `QA004 Test Root Org / default / dev`, real Chromium Console and cookie-free Provider requests.

## Procedure and result

1. Created a route- and model-restricted PROVIDER Virtual Key through Access & Keys: only `GET /v1/models` and model `qa004-cp121-priced-vision` were allowed.
2. Before rotation, the old credential returned HTTP 200 and only the permitted model.
3. Used the real Console Rotate action. The confirmation dialog stated that both credentials remain valid for a 24-hour grace window and that the new plaintext is shown once. The API readback returned `ROTATING_DUAL_VALID`, version 2.
4. During that state, cookie-free requests using both the old and new credentials independently returned HTTP 200 and only the permitted model.
5. Gracefully revoked the key through the Console. Readback returned `REVOKED`, version 3. Both old and new credentials then returned HTTP 401 `invalid_virtual_key`.
6. Queried the audit API for both actions. `virtual_key.rotated` sequence 717 and `virtual_key.revoked` sequence 718 point to the same target key in the organization partition.

The UI screenshots were inspected at readable scale: the creation form shows the precise route/model restrictions; the rotate dialog shows the stated grace contract; the inventory shows `Rotating Dual Valid`; and the post-action screen shows a successful revoke confirmation and `Revoked` inventory state. No plaintext key is included in evidence. No inference or budget-setting change occurred. Both temporary test keys from the successful and abandoned selector attempt are revoked.

## Evidence

- [`sanitized result`](../evidence/CP128-key-rotation-results.json)
- [creation and restriction form](../evidence/CP128-key-rotation-create-form.png)
- [rotation confirmation](../evidence/CP128-key-rotation-confirmation.png)
- [dual-valid inventory](../evidence/CP128-key-rotated-inventory.png)
- [revoked inventory](../evidence/CP128-key-revoked-inventory.png)

## Coverage classification

This is a scoped PASS for Org Owner create → rotate → dual-valid grace → revoke → old/new denial, and linked target audit events. It is not evidence for other roles, concurrent use during rotation, the timer's expiry behavior at 24 hours, process/database restart during grace, scoped-key changes during rotation, or active-stream revocation. The broader Virtual Key lifecycle/access coverage row remains PARTIAL.

No product code was modified. **GATE-REL-07 remains BLOCKED.**
