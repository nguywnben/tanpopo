# CP13 — Console encrypted backup and isolated restore

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Source runtime:** isolated SQLite at `127.0.0.1:18084`; restored copy booted separately at `127.0.0.1:18085`. Master key and archive remain under `/tmp`; neither is committed. No Tanpopo source changed.

## DEF-008 retest

Operations → Backups displayed an enabled Create encrypted backup action. Clicking it created `tanpopo-backup-1ba5d9a7c78c7622e9dd41f200467e1a.tpop.enc`, size 43,830 bytes, SHA-256 `f28b1ab85278c8eb49b10578096c22c0f956659d3de7102b40aa765bc4d5e357`; the success banner appeared and receipt row persisted in the Console. Screenshot `../evidence/CP13-backups-after-create.png` was visually inspected. Archive location: `/tmp/tanpopo-qa002-aux-data/backups/`.

Built baseline CLI verified the encrypted archive (`backup_id` matches; six files). Restore into new path `/tmp/qa002-restored-backup-CP13` with `--verify-secrets-decrypt` succeeded and emitted a separate restored tree containing `database.sqlite`, `tanpopo.yaml`, `tanpopo.lock`, `manifest.json`, and bundled extension artifacts. The archive, restored DB, and master key stay outside Git.

Started the restored data on port 18085 with the same test-only master key. `/healthz` and `/readyz` both returned 200. A fresh Chromium context logged in through the normal Console login and reopened Models & Deployments and FinOps → Chargeback. The model, both deployments, and `$0.006001` exact settled state survived restoration. Screenshots `CP13-backup-restored-models.png` and `CP13-backup-restored-finops.png` were visually inspected.

**Result: DEF-008 VERIFIED FIXED (scoped).** Console backup creation, cryptographic verification, organization/project secret-envelope decryption, separate restore, process start and fresh login/persisted-state recovery all succeeded. PostgreSQL/cluster backup, damaged archive behavior, target storage failures and full production migration remain UNVERIFIED.

GATE-REL-07 remains **BLOCKED**. QA-002 continues.
