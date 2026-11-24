# DEF-041 — Crypto-shred audit failure leaves an unlabeled destructive partial state

**Severity:** Moderate
**Status:** Confirmed in an isolated RC5 SQLite fault-injection scenario
**Baseline:** `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Area:** Operations → Organizations, organization/project/key lifecycle, audit and tenant-erasure operations

## Summary

When the final `organization.crypto_shredded` audit insert fails after the organization DEK has already been destroyed, the Console/API reports a generic operation failure while the tenant remains in `DELETING`. Project, environment, and Virtual Key rows remain, but their organization/project DEKs are gone. The audit history has no crypto-shred receipt yet. The Organizations screen does not identify the row’s destructive intermediate state or explain that retry is required; in the scoped Console it marks the selected row `Active` while project creation against that same selected tenant reports `organization is not active`.

A manual retry after the audit sink recovered did complete cleanup, append the tombstone event, and preserve a verifiable five-event chain. This limits impact and shows a recovery path exists, but the state and progress shown during the failure are incomplete and ambiguous.

## Reproduction

1. Build a private SQLite clone from the restored RC5 synthetic database and run it on an isolated loopback port. Install a test-only SQLite trigger that aborts inserts to `audit_events` only when `event_type='organization.crypto_shredded'`.
2. Through Chromium Console interactions, create a synthetic organization, project, environment, and provider-plane Virtual Key. Before deletion, verify rows exist for its org DEK, project DEK, project, environment, and key.
3. Open Operations → Organizations → Crypto-shred, enter the exact organization slug and a synthetic justification, then confirm.
4. The `DELETE /admin/v1/organizations/{id}` response is HTTP 500 with `failed recording shred audit event`. The UI displays that error. Read-only DB inspection shows `organizations.status='DELETING'`, zero org/project DEK rows, one project, one environment, one Virtual Key, and no `organization.crypto_shredded` audit event. A `vault_shred_requests` row is present with status `COMPLETED`.
5. Remove only the injected trigger and retry with the exact slug. The UI reports success, removes the organization, and reports one key revoked. Read-only DB inspection shows all target organization/resource/key rows absent. The retained event sequence ends with `organization.crypto_shredded`; authenticated Admin API audit verification returns HTTP 200, `intact=true`, `checked_events_count=5`.

## Evidence

- [Populated tenant and key after issuance](../evidence/CP18B-vkey-present.png)
- [Sanitized pre-failure population](../evidence/CP18B-population-result.json)
- [HTTP 500 and sanitized UI response](../evidence/CP18B-shred-audit-failure-ui.json)
- [Failure confirmation](../evidence/CP18B-shred-audit-failure-ready.png)
- [Visible error banner from equivalent empty-tenant reproduction](../evidence/CP18-shred-audit-failure-result.png)
- [Intermediate organization cannot create a project](../evidence/CP18-project-create-blocked-during-deleting.png)
- [Sanitized state counts and retry result](../evidence/CP18-shred-failure-recovery-result.json)
- [Successful retry](../evidence/CP18B-shred-retry-complete.png)
- [Post-retry chain verification](../evidence/CP18B-shred-audit-chain-result.json)

## Expected and actual

**Expected:** If final purge or receipt creation fails after irreversible DEK destruction, expose a durable, clearly labeled intermediate state such as `ERASURE_PENDING_FINALIZATION`; explain that encrypted payloads are already cryptographically inaccessible; show the pending operation and recovery step; allow a safe idempotent retry; and keep retrying or alerting until mutable rows are purged and the required anonymized receipt is appended. Do not suggest that no deletion work occurred.

**Actual:** The request returns `failed recording shred audit event`; the Console leaves the organization row and does not label it `DELETING` or explain that its DEKs were already destroyed. Child resource rows remain. The user can retry the destructive action, but the recovery requirement is implicit. The receipt appears only after manual retry.

## Business impact

During an audit-storage outage, data in the affected tenant becomes unrecoverable as soon as DEKs are destroyed, while mutable resource rows remain and the compliance receipt is absent. Operators receive a failure response with no explanation of the irreversible progress or how to complete cleanup. The observed manual retry recovered the tenant deletion and produced an intact audit chain; no plaintext disclosure or permanent stuck state was observed.

## Facts and root-cause hypothesis

**Observed facts:** the trigger blocked only the final hash-chained event; HTTP status was 500; the organization entered `DELETING`; org/project DEKs were removed before the error returned; resource rows remained; no terminal audit event was present until retry; after trigger removal, retry removed those rows and emitted a valid terminal event.

**Implementation observation:** RC5 handler source contains an initial teardown-fence commit, then calls the vault organization-DEK shred, then starts a second transaction to delete mutable records and append the audit receipt. This ordering explains why the audit transaction rollback does not restore the earlier DEK destruction.

**Root-cause hypothesis:** the failure UX was designed around final-transaction rollback and does not surface progress from the irreversible earlier phase. Automated retry/alert behavior for a `COMPLETED` vault shred request with an organization still `DELETING` was not established.

## Acceptance criteria

- Show a distinct `DELETING` / finalization-pending status immediately when the first teardown phase commits.
- For a final audit/purge failure, explicitly state that DEKs have already been destroyed, which resources may still have metadata rows, and what retry does.
- Preserve an idempotent resumable operation identifier and provide retry/recovery action without requiring users to repeat a potentially destructive workflow blindly.
- Retry or operationally alert until mutable tenant records are purged and the anonymized crypto-shred receipt is present; retain a verifiable audit chain.
- Verify successful deletion and injected failures before/after DEK destruction, restart recovery, repeated retries, and concurrent retries on SQLite and PostgreSQL.
- Confirm no secret plaintext or decryptable DEK remains in live storage, caches, logs, or backups consistent with the documented crypto-shred boundary.

## Contract references and limits

The accepted [tenant-isolation/security contract](../../docs/security/secrets-and-tenant-isolation.md#L119) requires destroying `org_encryption_keys` and emitting a `tenant.crypto_shredded` receipt. The [data model contract](../../docs/architecture/data-model.md#L349) also calls for removing mutable tenant configuration and recording the receipt. This report does not claim a compromise of the RC5 crypto-shred guarantee: the DEKs were destroyed and retry completed cleanup. It records the observed mismatch between the failed operation’s UI status and irreversible progress, plus the missing terminal receipt during the intermediate state. PostgreSQL, multi-node cache behavior, disk failure, crash recovery, and automated retries remain unverified.
