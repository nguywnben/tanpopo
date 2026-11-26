# CP114 — PostgreSQL crypto-shred and DEF-068 backend retest

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)  
**Result:** PASS (scoped) for PostgreSQL crypto-shred persistence and audit integrity; **DEF-068 reproduced** on PostgreSQL. No new defect was assigned.

## Synthetic populated tenant

Using real Chromium against the isolated restored PostgreSQL runtime, the Owner created organization `qa004-cp114-pg-crypto-shred`, project `qa004-cp114-canary`, and environment `qa004-cp114-synthetic`. Creation responses and subsequent readbacks supplied the organization, project, and environment IDs recorded in [sanitized results](../evidence/CP114-postgres-crypto-shred-results.json). The scope picker screenshot confirms that the intended canary project and environment were selected before Vault use.

The environment form visibly contained the distinct name `QA004 CP114 Synthetic Environment` and slug `qa004-cp114-synthetic`. The subsequent authenticated list returned both `name` and `slug` as `qa004-cp114-synthetic`, and the workspace picker displayed the slug. This is another observation consistent with existing DEF-055 on PostgreSQL; it does not receive a new defect ID. See the [distinct-value creation form](../evidence/CP114-environment-create-form.png), [readback](../evidence/CP114-project-env-results.json), and [selected scope](../evidence/CP114-canary-project-environment-scope.png).

Through Providers → Vault Secrets, the Owner sealed one project-scoped API-key-kind credential with a synthetic, non-routable test value. The UI returned HTTP 201 and stated plaintext could not be read again. After a page refresh, the API and UI returned only credential ID, project ID, name, kind, trailing fingerprint, version and creation time. The value itself was absent. A read-only PostgreSQL check found one credential metadata row, one org DEK and one project DEK; stored ciphertext was 57 bytes and did not contain the synthetic plaintext bytes. No provider connection or inference request was made. Screenshots of the secret form, successful seal, and refreshed metadata were visually inspected.

## Crypto-shred and receipt outcome

The canary was the currently selected organization. The Owner used Operations → Organizations → Crypto-shred, entered a synthetic justification and the exact slug, then confirmed. The actual request returned HTTP 200 with target ID `org_01m8b09bt7e00bs9e9hqzn2a88`, destroyed DEK ID `org_01m8b09bt7e00bs9e9hqzn2a88:v1:g1`, zero revoked Virtual Keys, and timestamp `2026-11-26T04:12:27.906Z`.

The Console removed the canary organization and switched to the surviving CP105 organization. The resulting Operations screenshot was visually inspected: it showed only the surviving organization and **no success receipt or banner**. This reproduces DEF-068 on PostgreSQL, in addition to its prior SQLite reproduction. It confirms the existing finding on a second storage backend; no separate defect is assigned.

Read-only PostgreSQL checks after completion found zero target organization, organization DEK, project, environment, project DEK, and secret metadata rows; exactly one `organization.crypto_shredded` event and five total events in the target audit partition remained. The target-partition `POST /admin/v1/audit-events/verify?org_id=…` returned HTTP 200, `intact: true`, and `checked_events_count: 5`. The surviving root organization remained present. The real Security → Audit Log view in the surviving organization was also opened and visually inspected; its rows are for that surviving tenant, while the deleted tenant's partition verification is the explicit API result above.

## Post-shred PostgreSQL logical restore

To check the retained shred receipt across a database recovery boundary, I created an external PostgreSQL custom-format dump after the deletion (330,378 bytes; SHA-256 `8a9733b551d7f3615049d9a59bb48aa27fe411eb1c7e0299e5174d8be678ac62`; mode `0600`, outside Git) and restored it to a new isolated database. The first disposable restore attempt failed before table creation because the test application role lacked `USAGE`/`CREATE` on the new database's `public` schema; I dropped that partial database, granted those schema permissions only on the isolated restore database, and repeated the restore successfully. This was test-infrastructure configuration, not a product defect.

The exact RC6 artifact then started against the restored database on a separate port and returned HTTP 200 from `/readyz`. An Owner authenticated successfully; read-only database checks found one surviving organization, no target tenant or secret rows, and the target's one shred receipt in its five-event partition. Authenticated target-partition audit verification returned HTTP 200, `intact: true`, and five checked events. The real Operations → Organizations page showed only the surviving tenant after restore; both restored-runtime screenshots were visually inspected. This is one scoped PASS for post-shred PostgreSQL logical restore acceptance and adds backend coverage to DEF-038. It does not exercise pre-shred cold-backup recovery.

## Finding update

DEF-068 remains **FAIL (confirmed)**. The successful active-org deletion response and persistent audit receipt are not surfaced in the Operations UI after the automatic scope switch. The active target and inactive-control distinction, impact, source-confirmed remount mechanism, and acceptance criteria are unchanged from [DEF-068](../reports/DEF-068-active-org-shred-receipt-lost.md). CP114 adds independent PostgreSQL reproduction evidence and confirms the deletion's persisted effects and intact audit partition on that backend.

## Evidence and limits

- [Sanitized response, API metadata fields, and pre/post PostgreSQL row counts](../evidence/CP114-postgres-crypto-shred-results.json).
- Visually inspected screenshots: [distinct environment form](../evidence/CP114-environment-create-form.png), [canary scope](../evidence/CP114-canary-project-environment-scope.png), [Vault credential creation](../evidence/CP114-vault-secret-sealed.png), [metadata after refresh](../evidence/CP114-vault-secret-after-refresh.png), [destructive confirmation](../evidence/CP114-crypto-shred-confirmation-filled.png), [active-org result with missing receipt](../evidence/CP114-active-organization-shred-result.png), [surviving-scope Audit Log UI](../evidence/CP114-audit-ui-surviving-scope.png), and [restored Operations roster](../evidence/CP114-postshred-restore-organizations.png).
- `CP114-org-create-results.json` and `CP114-project-env-results.json` record the sanitized provisioning readbacks.

Historical pre-shred cold-backup recovery, the `<60s` SLA, crash/retry behavior, concurrent shredding, and production multi-node erasure remain UNVERIFIED. CP111's baseline PostgreSQL restore and this post-shred restore do not establish PITR, WAL continuity, HA, or production disaster recovery. Synthetic data only; no product source changes. QA continues; **GATE-REL-07 remains BLOCKED.**
