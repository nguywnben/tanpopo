# CP44 — DEF-068 active organization crypto-shred receipt

**Classification:** PASS (scoped QA-005 retest)  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`, exact-source unsigned RC7 Linux binary  
**Environment:** isolated SQLite clone `127.0.0.1:18148`, Chromium, synthetic QA005 Owner/Operator

## Reproduction

1. Authenticate in the real Console and open Operations → Organizations.
2. Create the synthetic tenant `qa005-cp44-shred-active` and switch into it.
3. Open its active-row Crypto-shred action, provide the QA reason and exact slug, then confirm.
4. Inspect the response, automatic scope switch, result banner and organization roster.

The UI issued a `DELETE` returning HTTP 200 with destroyed DEK `org_01m8czh2w1e00akchtn34kkw8c:v1:g1` and `revoked_virtual_keys_count: 0`. The Console automatically switched to the surviving QA005 Secondary Org and displayed “Organization crypto-shredded” with the DEK ID and revoked-key count. A subsequent organization-list read no longer included the target. This directly retests the historical missing-receipt case across the scope remount and reverses that RC6 behavior on this narrow RC7 Owner/SQLite path.

The confirmation, post-switch receipt and post-reload state were visually inspected: [filled confirmation](../evidence/screenshots/CP44-active-shred-confirmation-filled.png), [visible completion receipt](../evidence/screenshots/CP44-active-shred-result.png), and [after full reload](../evidence/screenshots/CP44-active-shred-after-reload.png). The receipt clears on full reload. A direct audit-verification request was rejected with `csrf_validation_failed` because that read-only probe omitted the application's CSRF header; no fresh audit-chain result is claimed. The deleted org was empty: this retest did not create project, secret or virtual-key state. See the [sanitized evidence](../evidence/CP44-def068-active-shred-receipt.json).

**Remaining:** populated-org erasure and audit-chain verification, other roles/backends, failed/pending paths, concurrent operations, and the promised erasure SLA. Historical evidence remains preserved.
