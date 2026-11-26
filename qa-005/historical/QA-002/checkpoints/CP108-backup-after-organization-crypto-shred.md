# CP108 — Backup verification after organization crypto-shred

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** existing isolated RC4 QA runtime on 18084, using its matching QA database and Master KEK configuration  
**Result:** **FAIL — DEF-038 High confirmed**  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## Objective and method

Test whether the documented encrypted backup lifecycle remains usable after a tenant is successfully crypto-shredded, while preserving the deleted tenant's audit history. This follows CP85/CP107; it does not repeat their UI shred steps. The correct 18084 database was identified by the owner account and matched to its auxiliary Master KEK file. No production or other runtime data was changed.

Created a new encrypted archive from that database. Creation returned success (archive ID `961a158174c7f472183dd8b76fc4f59f`, 141095 bytes). Then ran `backup verify` and `backup restore --verify-secrets-decrypt` against a new destination. Both returned exit 14 with the same error: `audit chain partition org_01m86b839me00bb5zw6w29exhv does not match any organization or SYSTEM`. Failed restore did not create a destination. Archive itself was removed and is not retained in the repository.

The error partition is CP85's successfully shredded organization. Its retained `organization.crypto_shredded` event remains in that organization's partition and the two-event chain verified intact. CP107 independently established the same retention behavior for a populated deleted organization with six intact events. A separate isolated clean database with no deleted-org partition was used only as a CLI positive control: its archive verified successfully (four files). That control is not a same-database comparison.

## Source and outcome

Read-only inspection of frozen `internal/ops/backup/archive_restore.go` shows the validator derives allowed audit chain partitions from active organization rows plus `SYSTEM`, then rejects any other partition before accepting the archive. This matches the observed mismatch error. Source is unchanged.

This is **DEF-038, High, confirmed**: post-crypto-shred backup creation succeeds, but both verification and restore fail on valid retained audit history. Facts are separated from the hypothesis and acceptance requirements in [the standalone defect report](../reports/DEF-038-backup-restore-rejects-deleted-organization-audit-partition.md). Sanitized outcomes and the explicit exclusion of incorrect-database/key setup attempts are recorded in [CP108 evidence](../evidence/CP108-backup-crypto-shredded-audit-partition.json). No archive, database, key, token, checksum, or raw log is retained.

## Coverage disposition

- **FAIL:** complete backup lifecycle after a successful organization crypto-shred; verify and restore both reject the resulting archive.
- **PASS, scoped:** backup archive creation; ordinary clean-state archive verification on another isolated database; failed restore left no destination; deleted-org audit partition remained verifiable in its existing audit tests.
- **UNVERIFIED:** restore of valid post-shred archive; atomicity for failures occurring later in restore; wrong-key/corrupt archive matrix; pre-shred archives restored after later shred; cluster/PostgreSQL/Valkey and multi-node recovery; proof of physical DEK and backup ciphertext destruction.
- **Excluded setup noise:** an early wrong-database/key attempt and a login attempt against a different QA account returned setup errors and are not product evidence. The active 18084 runtime was not stopped or modified.

## Consolidated status

- **Historical DEF-001–015:** retain scoped QA-002 dispositions only; DEF-001–004, DEF-006, DEF-008–015 have limited passing scenarios; DEF-005 remains partial; DEF-007 has a populated successful shred path but injected-fault atomicity and independent physical/backup erasure proof remain unverified. No QA-001 PASS is inherited.
- **Open findings:** DEF-016–037 remain preserved/open, including DEF-023 reconfirmed CP107; CP108 adds DEF-038 High. No earlier finding is removed or downgraded.
- **Completed this phase:** exact-source CLI backup creation, verify/restore failure reproduction, clean isolated positive control, source-path reconciliation, sanitized evidence. No screenshot was applicable to this CLI-only phase; previous CP107 visual evidence is preserved.
- **Blocked/remaining:** backup fault/corruption and other storage modes; complete independent DEF-001–015 acceptance; remaining DEF-005 and DEF-007 acceptance; all open defect regression and broad Console/API/CLI/integration/browser/platform/security/operations coverage.
- **Recommended handoff:** prioritize DEF-038 with DEF-007 because normal crypto-shred currently prevents validated post-deletion backups; preserve the retained audit chain and reject arbitrary tampering. Continue DEF-023/029/030/035 scope handoff and earlier priorities.

QA-002 is in progress and coverage remains incomplete. `GATE-REL-07` remains **BLOCKED**.
