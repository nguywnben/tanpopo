# CP28 — Encrypted SQLite backup, verification, restore, and tamper detection

**Outcome:** PASS for one isolated RC6 SQLite backup roundtrip and encrypted-frame tamper rejection. Production-scale, external database, interrupted, and cross-version restore cases remain UNVERIFIED. GATE-REL-07 remains BLOCKED.

## Reproduction

Using the exact RC6 binary identified in CP27 and the isolated offline test-root's local Master KEK, the CLI created a consistent encrypted backup from runtime 18128. The command returned backup ID `c6dc3da3040b42429019ee32cde872ee`, a `.tpop.enc` archive, SHA-256 `b79c75097cba578ef2b39591c1769a7cb386aadafac445ed39423fc6d492f76c`, and 69,254 bytes. The archive verified successfully and listed four entries. Restore with `--verify-secrets-decrypt` succeeded to a separate temporary destination; the restored SQLite database returned `PRAGMA integrity_check = ok` and contained 57 application tables.

Two independent tamper probes were rejected. Altering a plaintext header byte caused verification to fail with `backup: invalid archive creation time`. Altering a byte in encrypted frame 0 caused verification to fail with AES-GCM `cipher: message authentication failed`. Neither modified the original archive or runtime database.

Sanitized results and command outcomes are in `evidence/CP28-backup-cli-roundtrip.json`. The archive, restored database, copied/tampered archive, and temporary directories were kept outside the repository and removed after the checks. No database, key material, credentials, or logs are included in QA documentation.

## Coverage limits

This is a local single-node SQLite happy-path plus encrypted-frame tamper check. Backup interruption, concurrent writes/load, file permissions, low disk space, retention, key rotation, failed secret unwrap, restore atomicity/failure cleanup, upgrades/downgrades, PostgreSQL/Valkey, and cross-platform artifacts remain UNVERIFIED.
