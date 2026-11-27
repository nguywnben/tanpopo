# CP110 — DEF-073 tampered extension archive error

**Disposition:** The RC6 internal-wording/UI finding did not reproduce in the tested exact-RC7 digest-mismatch path. DEF-073 becomes **PASS (scoped)**; historical QA-004 FAIL evidence remains preserved. `GATE-REL-07` remains **BLOCKED**.

## Baseline and environment

- Exact source: `737079ddb847d97dbcf832f610ed0048556cb65b`.
- Local runtime binary SHA-256: `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
- Isolated SQLite online-backup clone at `127.0.0.1:18164`, Chromium 156, synthetic QA Owner. The shared QA-005 baseline database was not mutated.
- The archive was a manifest-only `@qa004/lifecycle-canary` test package. A single trailing byte was flipped without changing the original expected digest. Package file and all state remained external to the QA branch.

## Reproduction and observed result

1. Opened Extensions → Stage package in the real Console.
2. Submitted `tanpopo-local://cp109-tampered.tpkg` with the original lowercase SHA-256 pin `59841233bee7df19e9a45a4de9e38d1cf7818bc1fc1e80951102add6232b9964`; the modified bytes hash to `675dbba90b6c2534113eb529a1c475deb12b43011f7388448e571f87b313e3c1`.
3. The real API returned HTTP 400 `hash_mismatch`, with a stable message that the package SHA-256 digest does not match the expected value.
4. The real Console displayed: **“The archive bytes do not match the expected SHA-256 digest. Re-verify the source package and only recalculate the digest for an intentionally trusted artifact.”** While the error was open, the submitted URI and expected digest remained in the form. Both screenshots were visually opened and inspected; one shows retained form values and one shows the full unobscured error.
5. Authenticated installation readback returned HTTP 200 with the existing canary still `ENABLED_ACTIVE` at 0.1.0, no candidate, and no new installation. The rest of the clone’s three installed rows were unchanged. No package code or provider traffic ran.

## Expected versus actual

- Expected: reject a mutated archive at the digest boundary, preserve active/candidate state, display a concise actionable integrity message without internal package/module wording, and retain submitted values for correction.
- Actual: all of those behaviors were observed in this exact-RC7 local archive scenario. No content was approved or executed.

**Result: PASS (scoped).** The historical RC6 `verify marketplace archive: installer: artifact_digest_mismatch` UI string is not reproduced. This does not validate Git/OCI resolvers, archive parser attacks, signature tampering, other roles/browsers/backends, or every integrity error translation. Preserve QA-004’s failure as historical evidence.

Evidence: [sanitized request/readback and hashes](../evidence/CP110-def073-tampered-archive.json), [retained form](../evidence/screenshots/CP110-def073-tampered-archive-form-retained.png), and [full error/readback](../evidence/screenshots/CP110-def073-tampered-archive-error.png).
