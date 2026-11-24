# DEF-038 — Backup verification and restore reject retained audit history for deleted organizations

**Severity:** High  
**Status:** Confirmed  
**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Checkpoint:** [CP108](../checkpoints/CP108-backup-after-organization-crypto-shred.md)

## Summary

After an organization is successfully crypto-shredded, Tanpopo retains its `organization.crypto_shredded` audit event in that organization’s own chain partition and can verify that chain intact. A subsequent encrypted database backup can be created, but `backup verify` and `backup restore` both reject it because the audit partition no longer matches a row in the active organizations table. The archive creation is therefore not sufficient to produce a verifiable/restorable recovery point after tenant deletion.

## Reproduction

1. Use the frozen QA-002 baseline. The 18084 database contains the successful crypto-shred history from CP85 and the populated crypto-shred from CP107. CP85 confirms its deleted organization ID `org_01m86b839me00bb5zw6w29exhv` has a retained `organization.crypto_shredded` event in the same `chain_partition`; the two-event chain is intact. CP107 independently confirms a six-event retained chain for another deleted tenant.
2. Create an encrypted SQLite backup of the actual 18084 isolated QA database using the matching Master KEK file. CLI returns success, archive ID `961a158174c7f472183dd8b76fc4f59f`, size 141,095 bytes.
3. Run `tanpopo backup verify --archive <archive>`. It exits 14 with: `Backup verification failed: backup: audit chain partition "org_01m86b839me00bb5zw6w29exhv" does not match any organization or SYSTEM`.
4. Run `tanpopo backup restore --archive <archive> --destination <new-empty-path> --verify-secrets-decrypt`. It exits 14 with the same audit-partition error. The destination path does not exist after failure.
5. As a separate positive control, create and verify an archive from another isolated clean QA database with no deleted-organization audit partition. That archive verifies successfully and reports four files.

The exact archive and restore output are not retained in the repository; the evidence file stores sanitized exit results only. No database, encrypted archive, Master KEK, admin token, or raw runtime logs are included.

## Expected and actual

- **Expected:** A backup containing valid retained crypto-shred audit history can be verified and restored while preserving that immutable history. Verification should distinguish an authenticated deleted-organization partition from an arbitrary unknown partition and validate its chain integrity.
- **Actual:** The backup verifier derives allowed audit chain keys only from currently active `organizations` rows plus `SYSTEM`. It rejects the retained partition for a successfully deleted organization before chain validation, so both verify and restore fail even though the deleted tenant’s chain independently verifies intact.

## Impact and affected workflows

After any organization crypto-shred leaves its audit partition retained, later system-wide backup archives may be created but cannot pass verification or restore. Operators cannot establish a validated recovery point for the whole instance through the documented CLI workflow. This undermines backup readiness after a normal tenant deletion and can block incident recovery or migration. No archive corruption or audit tampering was observed; the input archive was created successfully and the error is a partition-to-current-organization consistency check.

## Evidence

- `../evidence/CP108-backup-crypto-shredded-audit-partition.json` — sanitized create/verify/restore outcomes, clean control and environment corrections.
- [CP85](../checkpoints/CP85-operations-org-crypto-shred.md), `../evidence/CP85-org-shred-audit-sanitized.json` and `../evidence/CP85-org-shred-chain-verify.json` — deleted organization’s retained crypto-shred event and intact chain.
- [CP107](../checkpoints/CP107-populated-org-crypto-shred-and-scope-consistency.md), `../evidence/CP107-populated-shred-audit.json` — populated deleted organization’s six-event chain remains intact.
- Frozen source inspection: `internal/ops/backup/archive_restore.go` builds its allowed-chain set from current `organizations` IDs plus `SYSTEM`, then returns the observed mismatch error for any other audit partition. This explains the reproduced behavior; it is not a source modification.

## Root-cause facts and hypotheses

**Facts:** A valid archive was created from the actual 18084 database. Verify and restore fail with the same deleted-organization chain-partition mismatch. That partition corresponds to a documented successful crypto-shred event retained under the deleted organization ID. Its chain verifies intact. The separate clean control archive passes verification. The backup validation code allows only active organization IDs and `SYSTEM`.

**Hypothesis:** The backup validator’s active-organization allowlist does not account for intentionally retained audit chains after organization crypto-shred. The product contract should preserve tamper detection for unknown partitions while recognizing a deleted tenant’s authenticated shred history; a safe tombstone/receipt-based validation rule may be needed. No implementation fix was attempted.

## Acceptance criteria

1. After successful organization crypto-shred, create → verify → restore works for a full-system backup that retains the deleted tenant’s audit events.
2. The retained deleted-tenant chain is cryptographically verified and remains readable in the restored audit history.
3. Verification does not silently discard, merge, or rewrite deleted-organization audit events or weaken tamper detection for arbitrary unknown partitions.
4. The verifier accepts only a deleted partition supported by an authentic crypto-shred/tombstone record and correctly validates its complete sequence/hash chain.
5. Corrupting a retained chain still fails verification and restore; valid post-shred archives restore atomically and boot with existing non-deleted tenant data intact.
6. Regression coverage includes multiple deleted organizations, mixed active/deleted/system chains, empty and populated crypto-shred cases, wrong keys, archive corruption, and partial-destination cleanup.

## Scope limits

This test does not establish physical DEK destruction, backup ciphertext key-rotation/retention behavior, or restoration semantics for archives produced before crypto-shred. The initial mismatch-key check and wrong-database login attempt were environment setup errors and are excluded from product results.
