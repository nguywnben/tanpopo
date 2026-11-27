# DEF-075 — Expired Virtual Key rotations remain in dual-valid state

**Severity:** Moderate  
**Status:** FAIL (reproduced on frozen RC6; expiry denies the old credential but lifecycle cleanup does not occur)  
**Affected workflows:** Virtual Key rotation, Access & Keys inventory, credential cleanup, audit operations, administrator incident response.

## Reproduction

See [CP132](../checkpoints/CP132-virtual-key-rotation-expiry.md) and the sanitized [results](../evidence/CP132-rotation-expiry-results.json). An Org Owner created a scoped key and used the real Chromium Console Rotate action. A test-only browser interceptor changed the API grace value from the UI default of 86,400 to the supported minimum of 60 seconds; product source was not changed. Both credentials initially returned HTTP 200. After 62.317 seconds, the old key returned HTTP 401 \`invalid_virtual_key\` and the new key remained HTTP 200, but API readback and the visually inspected inventory still said \`ROTATING_DUAL_VALID\`, version 2. No rotation-finalized system event was present before manual revoke.

The key was then gracefully revoked, read back \`REVOKED\` version 3, and both credentials returned 401. Audit records link rotation #740 and revoke #741 to the same key. Sanitized evidence contains no credential values.

## Expected versus actual

The security behavior correctly rejects the previous key after its grace period. Lifecycle cleanup should then clear previous-key grace state, return the current key to \`CREATED_ACTIVE\`, advance the version, and record \`virtual_key.rotation_finalized\`. RC6 instead retains and displays \`ROTATING_DUAL_VALID\` after the old secret is rejected, with no finalization event in the tested interval.

Read-only source inspection confirms \`FinalizeExpiredRotations\` implements the transition and audit, while repository-wide Go-source search found no non-test call site. Source anchors and the frozen unit-test expectation are recorded in CP132. Whether the expired digest remains persisted was not directly inspected and is not asserted as a runtime fact.

## Impact

Operators see a key in dual-valid rotation after its grace has ended. This makes the security state and credential overlap difficult to reason about, obscures successful completion, and prevents designed cleanup/audit from running. The authorization boundary itself remained secure: old denied, current allowed.

## Acceptance criteria

- At expiry, the old credential becomes invalid and the new one stays valid.
- A served-runtime reaper finalizes expired rotations within a documented bounded delay, clears previous-key/grace fields, returns \`CREATED_ACTIVE\`, increments durable version, refreshes Console state, and emits one linked \`virtual_key.rotation_finalized\` event.
- Finalized state persists across control/data-plane nodes and restart; concurrent rotation/revoke races are tested.

**No implementation changes were made. GATE-REL-07 remains BLOCKED.**
