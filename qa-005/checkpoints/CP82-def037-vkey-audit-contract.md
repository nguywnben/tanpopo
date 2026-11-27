# CP82 — DEF-037 Virtual Key audit contract retest

**Source:** exact unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite clone `127.0.0.1:18162`; Chromium 156; synthetic Owner.

**Disposition:** **PASS (scoped QA-005 retest)** for one Console issue → audit filter → graceful revoke → chain verify lifecycle. The earlier `virtual_key.issued` discrepancy is not present in the frozen RC7 documentation/runtime contract: the current source specification documents and the runtime emits `virtual_key.created`. Prior QA-002 evidence remains preserved as the historical baseline finding.

## Reproduction and result

1. Inspected frozen RC7 `docs/ux/screen-specifications.md`, which lists `virtual_key.created` for key issuance. Signed in as Owner and opened **Access & Keys → Virtual Keys** in a populated synthetic project/environment. The real issue-key form specified that plaintext is shown once; its initial form screenshot was visually inspected at `evidence/screenshots/CP82-def037-issue-key-form.png`.
2. Created a least-use test key named `CP82 DEF037 audit vocabulary` through the Console. The create request returned HTTP 201. The one-time secret was not read into evidence, captured in a screenshot, or printed. The created row showed only name, truncated prefix/last four, grants and Active state; this screenshot was visually inspected at `evidence/screenshots/CP82-def037-created-key-list.png`.
3. Authenticated API filters returned HTTP 200. `action=virtual_key.created` returned the event for target `vky_01m8dnc6xwe009yd8wns4g68sn` at sequence 720; `action=virtual_key.issued` returned zero records. The issue metadata contained the expected allowed plane and environment ID, with no plaintext key field.
4. Revoked the key through the Console using **Revoke (graceful drain)**. The list showed the key as Revoked and the UI confirmed zero active streams severed; screenshot visually inspected at `evidence/screenshots/CP82-def037-revoked-key-list.png`. API filter `virtual_key.revoked` returned the matching target at sequence 726 with actor and organization partition consistent with the create event.
5. In **Security → Audit Log**, clicked **Verify hash chain**. The UI showed “Cryptographic RFC 8785 JCS chain intact across 726 events.” The screenshot was visually inspected at `evidence/screenshots/CP82-def037-audit-chain.png`.

Sanitized event fields and filter results are in [CP82 evidence JSON](../evidence/CP82-def037-audit-contract.json).

## Limits

One Owner/SQLite issue-revoke pair, exact action filters and global audit-chain verification were tested. Other roles, PostgreSQL, pagination beyond the returned filter window, downstream consumers, storage-failure atomicity, tenant partitions, tamper detection and other key lifecycle actions remain UNVERIFIED. This scoped PASS does not close broader DEF-007 audit atomicity. No product code or specification was changed; `GATE-REL-07` remains BLOCKED.
