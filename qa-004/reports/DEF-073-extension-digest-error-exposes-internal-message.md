# DEF-073 — Extension digest mismatch exposes an internal error string

**Severity:** Low
**Status:** FAIL
**First found:** QA-004 CP118
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Affected workflow:** diagnosing an extension archive rejected by the operator-supplied SHA-256 pin.

## Summary

The integrity boundary works: exact RC6 rejects a modified local `.tpkg` when the operator supplies the original archive digest. However, the real Console displays the backend error detail verbatim: `verify marketplace archive: installer: artifact_digest_mismatch`. The wording exposes implementation terminology and does not explain that the bytes differ from the pinned digest or how to recover.

## Reproduction

1. Use an isolated exact-RC6 test runtime and authenticate as an Org Owner.
2. In Extensions → Marketplace → Stage package, submit a synthetic local archive URI with its original lowercase SHA-256.
3. In the test archive root, flip one byte in a disposable copy while retaining the original archive. The test used 362-byte `@qa004/lifecycle-canary` version `0.1.0` files: original SHA-256 `59841233bee7df19e9a45a4de9e38d1cf7818bc1fc1e80951102add6232b9964`; modified SHA-256 `675dbba90b6c2534113eb529a1c475deb12b43011f7388448e571f87b313e3c1`.
4. Submit `tanpopo-local://cp118-tampered.tpkg` with the original digest.
5. Observe HTTP 400 `hash_mismatch`; the Console renders `verify marketplace archive: installer: artifact_digest_mismatch` in its error banner and retains the submitted form values.
6. Read authenticated `GET /admin/v1/extensions/installations?limit=100`: HTTP 200, one existing installation, `ENABLED_ACTIVE`, active `0.1.0`, no candidate, version 9. The real Installed screen also shows only that existing active package.

Evidence: [tamper request and sanitized readback](../evidence/CP118-tampered-archive-results.json), [visually inspected Console rejection](../evidence/CP118-tampered-archive-rejected.png), and [visually inspected installation list](../evidence/CP118-tampered-archive-installations.png). The disposable modified copy was removed after the test.

## Expected versus actual

- **Expected:** fail closed, preserve all active/candidate state, and tell the operator that archive bytes do not match the expected digest, with a safe corrective action.
- **Actual:** the API correctly returns `hash_mismatch` and no candidate is created, but the Console displays the low-level phrase `installer: artifact_digest_mismatch` without actionable explanation.

## Impact

This is a Low-severity diagnostic and usability issue. The failed package is not staged or activated, so this test found no integrity bypass. Operators may not know whether to re-download the archive, correct a stale digest, or investigate a real tamper event. It is distinct from DEF-062: DEF-062 concerns an immutable duplicate-candidate 409 that is misdescribed as already installed; DEF-073 concerns the 400 archive digest integrity boundary.

## Facts and hypothesis

**Verified facts:** the Console screenshot displays the backend detail; the API error code is `hash_mismatch`; the existing installation remained active with no candidate after the attempt.

**Root-cause hypothesis:** this error code lacks a dedicated Console translation, and a generic error handler renders the returned `Error.message`. Confirm the exact shipped asset-to-API mapping before changing it. This single result does not establish that every error is presented this way.

## Acceptance criteria

1. Keep modified archive bytes rejected with HTTP 400 `hash_mismatch` when they do not match the expected digest.
2. Keep active and candidate state unchanged and do not create an installation or permission-review candidate.
3. Show a concise user-facing integrity error without internal package/module wording.
4. Provide safe recovery guidance to verify the source and only recalculate a digest for an intentionally trusted artifact.
5. Retain the form values for correction or cancellation; preserve stable API code and audit behavior.

## Limits

CP118 validates a one-byte content mutation against the original pin. CP41 separately validated rejection of a mutated expected digest. Recomputed-digest changes, Ed25519 invalid/mutated signatures, modified manifest/file digests, alternate sources, archive parser attacks, and corrected-artifact retry remain UNVERIFIED. No production signing key or credential was used. QA continues; **GATE-REL-07 remains BLOCKED.**
