# CP79 — DEF-008 Console encrypted backup creation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated exact-RC6 SQLite runtime `127.0.0.1:18128`  
**Role/browser:** synthetic Operator + Org Owner, real Chromium at 1440×1000  
**Product source modified:** no

## Result

**PASS (scoped) — the historical Console-to-backup-runtime failure did not reproduce for local SQLite backup creation.** Operations → Backups started with zero receipts. Clicking **Create encrypted backup** displayed a success notice and one recorded receipt. The archive was present in the isolated runtime's `/tmp` backup directory; its actual size (142,704 bytes) and SHA-256 matched both the Console receipt and the `ops.backup_created` SYSTEM audit metadata. The targeted audit API returned the event as sequence 6, targeting the same backup ID, and SYSTEM chain verification returned HTTP 200, intact, six checked events.

The UI screenshots were visually inspected: the initial empty backup state and the resulting receipt/success state. The encrypted archive contains test runtime database material; it remains outside this repository and is excluded from the handoff branch.

## Reproduction

1. Sign in to the isolated exact-RC6 runtime as the synthetic Operator/Org Owner.
2. Open **Operations → Backups** and confirm zero receipts.
3. Click **Create encrypted backup**.
4. Inspect the Console success notice and receipt.
5. Read the instance `SYSTEM` audit partition using `GET /admin/v1/audit-events?chain_partition=SYSTEM&action=ops.backup_created&limit=50`.
6. Compare the event's target ID, filename, SHA-256 and size against the isolated runtime file. Verify the SYSTEM chain with `POST /admin/v1/audit-events/verify?chain_partition=SYSTEM`.

Sanitized values are in [CP79 results](../evidence/CP79-console-backup-results.json). Visually inspected screenshots: [initial empty state](../evidence/CP79-console-backups-initial.png) and [created receipt](../evidence/CP79-console-backup-created.png).

## Remaining verification boundary

**Exact artifact archive verify/restore: UNVERIFIED.** A verification attempt with the only accessible QA KEK candidate failed AEAD authentication while unwrapping the archive data key. The running server's master-key file binding could not be read from the restricted shell, so this result does not distinguish a wrong test key from archive corruption and is not classified as a product defect. Retesting verify/restore of this specific artifact requires the runtime-matching test KEK. CP28 and CP67 separately verified other RC6 archives; those results are not inherited for the CP79 artifact.

This checkpoint verifies only single-node SQLite create, local file persistence, receipt/hash consistency, and SYSTEM audit linkage. External backup targets, interrupted writes, low disk, concurrency/load, retention, key rotation, PostgreSQL/Valkey backup and disaster recovery remain UNVERIFIED. No backup artifact or database was added to the branch.

QA continues; **GATE-REL-07 remains BLOCKED**.
