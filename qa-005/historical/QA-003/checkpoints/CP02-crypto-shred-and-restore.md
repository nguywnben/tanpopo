# CP02 — Crypto-shred, retained audit, backup, and restore

**Baseline:** RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Status:** Executed for the isolated SQLite path; broader QA-003 remains in progress. `GATE-REL-07` remains **BLOCKED**.

## Reproduction and actual result

1. Using Chromium 156 at 1440×1000, create synthetic tenant `QA-003 Backup Retest CP02`, a project, an environment, and a provider-plane virtual key through the Console. Read the newly created tenant in the UI and Admin API. The UI scope switcher retained the selected tenant across navigation. Its initial state had one project, one environment, and one virtual key.
2. Open Security → Audit Log, read the tenant's four provisioning events, then run **Verify hash chain**. The Admin API returned HTTP 200 and an intact chain.
3. Create and verify a pre-shred encrypted backup. Restore to a new empty destination with `--verify-secrets-decrypt`; it passed. The first restore attempt was intentionally rejected because the test harness had already created the destination directory; retry to a fresh path passed. This was test setup, not a product failure.
4. From Operations → Organizations, open Crypto-shred, submit a synthetic deletion reason and the exact tenant slug. The confirmation dialog warned that the organization DEK would be destroyed, secrets and keys purged, and sessions cancelled. After confirmation the tenant row disappeared and the UI returned to Default Organization. Screenshots: [confirmation](../evidence/CP02-crypto-shred-confirm.png), [ready state](../evidence/CP02-crypto-shred-ready.png), [completed state](../evidence/CP02-crypto-shred-complete.png).
5. Create a new encrypted backup from the post-shred DB. `backup verify` authenticated all four files; `backup restore --verify-secrets-decrypt` succeeded to a fresh directory. The exact sanitized result and artifact digest are in [evidence](../evidence/CP02-crypto-shred-restore.json). Archive and restored DB remain outside the repository.
6. Start the restored DB in a separate runtime on `127.0.0.1:18104`. `/healthz` returned HTTP 200. In a real Chromium session, owner login succeeded, Overview rendered, Default Organization remained available, and the deleted tenant was absent. A synthetic surviving credential remained listed as metadata; the response contained no plaintext secret. Screenshots: [restored Overview](../evidence/CP02-restored-overview.png) and [restored Audit Log](../evidence/CP02-restored-audit.png).
7. Query the restored DB read-only, selecting only record counts and audit event names (no ciphertext or credential values). The deleted organization row and all associated organization DEK, project, environment, virtual key, principal, role binding, secret, and project DEK rows were absent. Five tombstone audit events remained, ending with `organization.crypto_shredded`. POST to the product audit verification endpoint for the deleted tenant's retained chain returned HTTP 200, `intact: true`, five checked events.
8. Negative controls: backup verification with a different random master key was rejected for authentication failure. A one-byte-mutated archive failed restore authentication, and its new destination directory was not created. The original valid archive still verified/restored successfully.

## Defect disposition

- **DEF-038 — VERIFIED FIXED for this RC5 SQLite scenario.** Historical failure was valid audit history retained after organization crypto-shred being rejected by backup verification/restore. On RC5, the post-shred archive authenticated, restored with all secret envelopes checked, restored service started, and the preserved deleted-tenant chain verified intact. Incorrect key and tampering controls were rejected without publishing a partial restore.
- Other storage backends, restore during process crash/power loss, migration/upgrade interactions, and larger multi-tenant/multi-shred histories remain **UNVERIFIED**. The finding is not a blanket declaration of backup/restore reliability.
- **DEF-007 — PARTIAL.** Successful organization/project/environment/key creation events and a valid pre-shred audit chain were observed. Failure atomicity, audit append failure injection, and broader mutation coverage remain **UNVERIFIED**.
- No additional defect was confirmed in this scenario.

## Coverage updates

This phase gives **PARTIAL** coverage to FT-09 (tenant/project/environment/key lifecycle and scope), FT-12 (audit chain and crypto-shred evidence), FT-15 (Console, Admin API, CLI backup commands), FT-16 and SCR-16 (encrypted backup, restore and operational UI), SCR-02 (restored overview), SCR-13 (keys/workspace scope), SCR-14 (audit), SCR-15 (none; not exercised), and WF-13 (tenant deletion and retained tombstone audit). All other feature, screen, browser, role, platform, provider, failover and recovery cases remain UNVERIFIED unless another checkpoint explicitly states otherwise.

## Environment and safety

The original and restored runtimes use private synthetic SQLite databases under `/tmp`; the browser was real Chromium with inspected screenshots. Archive/database/key/credential files and logs are excluded. No Tanpopo implementation files were modified. No configured external provider was called; QA provider spend remains USD 0.
