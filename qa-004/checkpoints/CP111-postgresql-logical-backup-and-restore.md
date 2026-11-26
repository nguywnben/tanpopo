# CP111 — PostgreSQL external backup and restore

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** PASS (scoped) for one PostgreSQL custom-format logical backup, restore to a new database, and RC6 application recovery. PITR, HA/failover, and production-scale DR remain UNVERIFIED.

## Backup and isolated restore

The Console correctly reports that its encrypted snapshot feature supports SQLite only and requires PostgreSQL deployments to use PostgreSQL backup tooling. Following that documented boundary, I used `pg_dump` custom format against the isolated PostgreSQL 16 test database, with ownership and ACL commands excluded. The PostgreSQL container has no default `postgres` role; the configured database administration role was used for the dump. The archive was 324,855 bytes, mode `0600`, SHA-256 `f76504781c365aaf1904c61fc14b8b9d1bde04b2d95c7f3343a790f38d56c167`, and `pg_restore --list` parsed 600 TOC entries.

Created a separate database and restored using the application role for object ownership. Before application startup, read-only SQL counts showed one organization, two principals, and 85 audit events. The dump remained at `/tmp/qa004-cp111-postgres.dump`, outside the QA repository. A byte-level check did not find the exact external Master KEK bytes or their hex encoding in this dump; this is a bounded material check, not a proof against every possible representation. The KEK file remained separately stored and was supplied to the restored runtime.

## Application recovery and browser verification

Started the same exact RC6 artifact against the restored database on `127.0.0.1:18135`. The runtime used the isolated three-node Valkey cluster configuration, returned HTTP 200 from `/readyz`, and the actual Owner account authenticated in Chromium. The real Console and authenticated API read back the organization, both projects, both environments, and both members, including the active project-bound Developer created in CP110.

The restored Operations screen reported the PostgreSQL backend, ready/serving, config epoch 9, and a healthy local node. Security → Audit Log then verified the RFC 8785 JCS chain through the real Console: HTTP 200 from `/admin/v1/audit-events/verify` and an intact 88-event chain. The additional events after the database restore were test logins; the initial restored snapshot contained 85 events.

The artifact was then started in a split-role configuration with a `control-plane` and a separate `data-plane`, both configured for the restored PostgreSQL database and Valkey cluster. Both listeners returned `/readyz` 200. The control-plane Operations view continued to list only one `primary` node; this cross-role discovery is recorded separately as CP112 and is not counted as a successful multi-node health inventory here.

## Evidence

- Sanitized [backup metadata, restore counts and KEK byte-check](../evidence/CP111-postgres-backup-restore-verification.json); the database archive itself is intentionally not in Git.
- [Restored application readback](../evidence/CP111-restored-runtime-readback.json), visually inspected [Owner Overview](../evidence/CP111-restored-db-owner-overview.png), [member roster](../evidence/CP111-restored-members.png), and [PostgreSQL Operations screen](../evidence/CP111-restored-operations.png).
- [Audit verification response](../evidence/CP111-restored-audit-verification.json), visually inspected [pre-verification audit rows](../evidence/CP111-restored-audit-before-verify.png) and [verified chain](../evidence/CP111-restored-audit-verified.png).

This verifies one manually executed full logical database recovery path. It does not exercise continuous WAL archiving, point-in-time selection, retention, restore interruption, cross-version restore, failover, or the documented `RPO < 5m` target. Application-specific Valkey rate-limit behavior and Valkey node failure under active traffic remain UNVERIFIED. No product implementation was changed. QA continues; **GATE-REL-07 remains BLOCKED.**
