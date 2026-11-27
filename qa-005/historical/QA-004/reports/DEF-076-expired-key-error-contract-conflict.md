# DEF-076 — Expired-key error contract conflicts with generic authentication response

**Severity:** Low  
**Status:** PARTIAL — runtime behavior is repeatable, but the intended security contract needs owner resolution.  
**Affected workflows:** key rotation and client credential rollover, SDK error handling, support diagnosis, security documentation.

## Evidence

CP132 tested the documented rotation-expiry boundary. After the old credential's grace elapsed, the Data Plane returned HTTP 401 \`invalid_virtual_key\` (generic message: invalid, expired, or revoked). The old credential was rejected and the current one remained usable. See [CP132](../checkpoints/CP132-virtual-key-rotation-expiry.md) and [sanitized results](../evidence/CP132-rotation-expiry-results.json).

Frozen product documents disagree on the expected code: TP-IMP-0009 AC-2 says \`virtual_key_expired\`; the AUTH-05 contract matrix says \`key_expired\`. The runtime uses \`invalid_virtual_key\`. The generic response may be intentional anti-enumeration behavior, so this report does not recommend exposing a more specific credential state without a security decision.

## Impact and resolution criteria

Clients and operators cannot distinguish a grace-expired key from other invalid credentials using the documented error code. Resolve this as a contract decision: either use the documented expiry code only if Security approves that disclosure, or retain uniform \`invalid_virtual_key\` and revise AC-2/AUTH-05 and client guidance to match. Keep HTTP 401, do not make the expired secret usable, and add contract tests for both old and active credentials at the boundary.

No source change was made. **GATE-REL-07 remains BLOCKED.**
