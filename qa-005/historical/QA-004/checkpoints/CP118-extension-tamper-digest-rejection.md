# CP118 — Tampered local extension archive rejected by pinned digest

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Overall result:** PARTIAL. PASS (scoped) for rejection of modified archive bytes against the original pinned digest. FAIL (DEF-073) for Console error presentation. Ed25519 signature mutation and a recomputed-digest/modified-manifest signature case remain UNVERIFIED. No source was changed and no production signing material was used.

## Reproduction

In the isolated exact-RC6 test-root runtime on `127.0.0.1:18129`, an Org Owner opened Extensions → Marketplace → Stage package. The source package was the existing synthetic unsigned `@qa004/lifecycle-canary` `0.1.0` fixture in the test archive directory. A disposable copy was created and one byte at its end was flipped. The original and modified SHA-256 values were respectively `59841233bee7df19e9a45a4de9e38d1cf7818bc1fc1e80951102add6232b9964` and `675dbba90b6c2534113eb529a1c475deb12b43011f7388448e571f87b313e3c1` (362 bytes each).

Using the actual Console form, the test submitted `tanpopo-local://cp118-tampered.tpkg` with the original archive's SHA-256. The exact RC6 endpoint returned HTTP 400 `hash_mismatch`; the error detail was `verify marketplace archive: installer: artifact_digest_mismatch`. The Console rejected the request before permission review/installation and retained both form values. This demonstrates the configured local archive digest pin catches this content mutation. It does not test a rehashed/tampered package against Ed25519 signature verification.

After the failed request, authenticated `GET /admin/v1/extensions/installations?limit=100` returned HTTP 200 and one existing installation. Its state remained `ENABLED_ACTIVE`, active version `0.1.0`, `candidate_version: null`, version 9. The real Installed view likewise showed the one existing active synthetic package, with no new candidate. Screenshots were visually inspected.

## DEF-073 — user-facing integrity error exposes internal wording

**Severity:** Low. **Status:** FAIL. **Affected workflow:** operator diagnosing a local extension archive rejected by its expected SHA-256 pin.

The integrity check behaved safely and gave the correct API error code, but the Console displayed the backend detail verbatim: “verify marketplace archive: installer: artifact_digest_mismatch”. The message includes internal implementation terminology and does not tell an operator that the supplied bytes do not match the expected digest or what to do next. This is separate from DEF-062, which concerns the duplicate-candidate `resource_already_exists` response being translated to an already-installed message. CP118’s response is `hash_mismatch` and concerns the archive trust/pinning step.

The UI-visible message and API body are confirmed facts. A likely cause is that this error code has no friendly Console translation and the generic error helper displays `Error.message`; confirm the exact shipped asset/API mapping before remediation. No broad claim about all error handling is made.

## Acceptance criteria for DEF-073

1. A byte-modified local package with the original expected digest remains rejected with `400 hash_mismatch`; no candidate, version, or active-package state changes.
2. The Console presents a concise integrity message, such as “Archive SHA-256 does not match the expected digest,” without exposing package/module error strings.
3. The message explains a safe recovery step: verify the archive source and recalculate the digest only when the operator intentionally trusts the changed artifact.
4. The source URI and expected digest remain available for correction or explicit cancellation, and the rejected candidate is never offered for permission approval.
5. The API continues to return a stable machine-readable `hash_mismatch` code and retains audit behavior.

## Limits

This is one valid local archive modified by one byte and submitted against its prior digest. CP41 independently showed a mutated expected digest is also rejected; CP118 specifically changed archive bytes. No Ed25519 signature was supplied on this external unsigned fixture. A modified archive with a freshly recomputed digest, invalid/mutated signature, changed manifest/file digest, signature registry mutation, alternate source kind, malformed archive, multi-tenant authorization, and retry/recovery after a corrected archive remain UNVERIFIED. No production private key or provider credential was accessed. QA continues; **GATE-REL-07 remains BLOCKED.**
