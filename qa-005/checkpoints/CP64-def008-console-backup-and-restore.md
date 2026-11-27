# CP64 — Fresh-install Console backup, audit linkage, and restored service

**Source:** frozen unsigned RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`.
**Disposition:** historical DEF-008 **PASS (scoped QA-005 retest)**. This expands the previous Console-create-only evidence with read-only receipt verification, system-audit verification, archive authentication, clean-destination restore and service recovery.

## Reproduction

1. Started a fresh exact-source SQLite runtime at `127.0.0.1:18158` with a test-only master key, valid empty `tanpopo.lock`, `tanpopo.yaml`, empty extension artifact store, offline mode and no configured provider.
2. In real Chromium 156, completed the actual first-install wizard with a synthetic Owner and tenant. Initial provider connection was skipped as the UI instructed because no adapters were enabled. Owner login succeeded with HTTP 200; the Operations Health & Cluster view showed `Ready` and `sqlite_wal`.
3. Opened Operations → Backups. The initial state displayed zero encrypted backups. Clicked **Create encrypted backup**. The Admin API returned HTTP 201; the Console displayed a success notice and one receipt. The exact artifact was under the isolated runtime's protected `backups` directory, 25,283 bytes, SHA-256 `1d38015669e9a01da2a2cb8ab20a8551458ea6e02cef3809027e321e9a2f6f16`. The file's actual length and digest matched the Console receipt. After a hard refresh, count 1 and the same metadata remained.
4. Used the exact-source `tanpopo backup verify` command on the file; it authenticated successfully and returned the matching ID with four files. A read-only database query and authenticated `GET /admin/v1/audit-events?chain_partition=SYSTEM&action=ops.backup_created&limit=20` identified the same backup ID at SYSTEM sequence 2 with filename, digest and size metadata. `POST /admin/v1/audit-events/verify?chain_partition=SYSTEM` returned HTTP 200, `intact:true`, checked count 2.
5. Restored that exact Console-created archive into a previously absent destination with `tanpopo backup restore --verify-secrets-decrypt`. Restore succeeded; restored SQLite `integrity_check` was `ok` with one org, project, environment and principal. Started the restored service at `127.0.0.1:18159`; health and readiness both returned HTTP 200. Real Chromium owner login returned HTTP 200 and Overview showed the same organization/project/environment and expected empty usage state.

## Classification, evidence, and limits

**DEF-008 — PASS (scoped):** the RC7 fresh-install Console action reaches its SQLite backup runtime, writes a real encrypted artifact and receipt, records a matching verified SYSTEM audit event, and the artifact authenticates, restores and serves the restored tenant. QA-004 CP79's scoped PASS is preserved; this is a fresh independent RC7 check.

All listed screenshots were visually opened and inspected. Sanitized artifact identity, API statuses, receipt, audit linkage and restore readback are in [CP64 evidence](../evidence/CP64-def008-console-backup-restore.json). The new DB had no provider credential, so credential-envelope decryption with active secrets was not exercised; the `--verify-secrets-decrypt` run covered the empty test vault only. Concurrency, low disk, tamper/interrupted writes, retention/key rotation, PostgreSQL/Valkey and Windows recovery remain UNVERIFIED.

The Operations receipt refers to the SYSTEM audit partition. Security → Audit Log's visible action filter queried the Owner's organization partition; filtering for `ops.backup_created` showed zero events. An authenticated API query with `chain_partition=SYSTEM` found the event and verified that chain. The UI exposed no partition selector in this tested state. Since no accepted Console requirement for browsing SYSTEM events was found, this is separately recorded as **PRODUCT_GAP PG-010**, not a defect.

No production implementation or specification changed; test credentials, KEK, database and encrypted archive remain under `/tmp` and were excluded. No paid provider call was made. QA-005 continues; `GATE-REL-07` remains BLOCKED.
