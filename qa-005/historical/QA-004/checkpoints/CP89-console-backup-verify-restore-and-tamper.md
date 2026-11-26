# CP89 — Console encrypted backup, exact-artifact verification, restore, and corruption rejection

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Outcome:** PASS (scoped) for one real Console-created SQLite backup, matching immutable receipt, CLI verification, secret-envelope trial-decrypt restore, and encrypted-frame tamper rejection. No defect assigned.
**Source:** unmodified. **Gate:** GATE-REL-07 remains BLOCKED.

## Console creation and receipt reconciliation

In the isolated SQLite runtime `127.0.0.1:18129`, the real Chromium Owner opened Operations → Backups. The initial screen showed zero receipts and the empty-state text. Clicking **Create encrypted backup** returned HTTP 201 and changed the page to a success notice and one row. The success screen was visually inspected.

The file was 28,647 bytes with SHA-256 `007d2635d2a5219c4180614e2bbe27293a3890291dc6539f449a71d1967102e2`. The real `ops.backup_created` SYSTEM event #2 targeted backup `38f9c5c57cc187d26e09283ce6dabbca` and recorded the same filename, digest, and size. See the [UI/API/audit readback](../evidence/CP89-backup-ui.json) and visually inspected [empty state](../evidence/CP89-backup-initial.png) and [created receipt](../evidence/CP89-backup-created.png).

## CLI verification and restore

Using the same runtime's test-only local KEK, CLI `backup verify` returned exit 0 and reported all nine files verified. `backup restore --verify-secrets-decrypt` returned exit 0 to a new temporary destination. The restored database contained 57 application tables and passed SQLite `PRAGMA integrity_check`. The authenticated restore included the manifest, config, lockfile, database, and five manifest-only extension artifacts. No provider or production credentials were used.

A copy of the archive was modified by flipping one byte inside encrypted frame 0. Verification returned exit 14 with `cipher: message authentication failed`. The original remained valid during the test. Full sanitized CLI output and file inventory are in [verification results](../evidence/CP89-backup-verification.json).

The original encrypted archive, tampered copy, restored database and temporary working directories were removed after checks. The immutable Console receipt and audit record remain in the disposable QA runtime; the archive is not included in this branch.

## Coverage boundary

This closes one fresh exact-artifact Console-to-CLI verification/restore journey and confirms encrypted frame authentication for SQLite. CP79's distinct archive remains unverified because its matching KEK was unavailable; CP89 does not inherit or resolve that case. PostgreSQL/Valkey backup, concurrent writes, interrupted creation, retention, key rotation/failure, restore atomicity under injected errors, cross-version/platform behavior, and disaster recovery remain UNVERIFIED. QA continues and **GATE-REL-07 remains BLOCKED**.
