# CP43 — Adjacent pre-RC4 source candidate upgrade to RC5

**Older source tested:** `a771311107c4e4c2c7d9b53a3f5a0926f114b799` (PR #42 merge; first-parent predecessor of RC4 merge PR #43)<br>
**Target:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`<br>
**Runtime:** isolated SQLite database and synthetic KEK in `/tmp`; Chromium at 1440×1000<br>
**Disposition:** **PASS, scoped** for the named adjacent source commit; the exact official RC3 baseline is **UNVERIFIED**. QA-003 remains incomplete; `GATE-REL-07` remains **BLOCKED**.

## Why this is not labeled RC3

The Git repository has no RC tags, and the public GitHub releases endpoint returned no releases. QA-002 identifies `40bba5b55c2da6fc00b107b315c9b3167fb20169` as RC4. Commit `a771311…` is the immediately preceding PR #42 merge, but no evidence establishes it as the exact RC3 source or release artifact. It is therefore reported as an adjacent pre-RC4 source candidate rather than silently relabeled RC3. The official RC4 artifact remains unavailable in the separate DevBox and unverified.

## Reproduction and results

1. Built the named historical commit and RC5 from exact Git archives with Go `1.27.4`, `-mod=readonly`, and `-trimpath`; no source or dependency files were modified. Upgraded the fresh RC5 build to the same metadata-injected binary and hash recorded in [CP42](CP42-rc4-to-rc5-upgrade-migration.md).
2. Started the older source on isolated listener `19196`, completed real Chromium first-run setup with a synthetic Owner/organization/project/environment, and sealed one synthetic API-key credential. The vault displayed only metadata and fingerprint `...2086`; plaintext was not read back. Screenshots: [initial Setup](../evidence/CP43-pre-rc4-setup-initial.png), [no-adapter step](../evidence/CP43-pre-rc4-setup-no-adapters.png), [Setup review](../evidence/CP43-pre-rc4-setup-review.png), and [vault metadata](../evidence/CP43-pre-rc4-secret-metadata.png).
3. Before migration, Owner authentication succeeded, one organization and project remained, and the audit verifier returned HTTP 200 / intact with four events. The pre-upgrade audit UI is [here](../evidence/CP43-pre-rc4-audit-before.png); its API event count was five after a subsequent login.
4. Stopped the older server and started RC5 on the same SQLite database and master-key file. `/healthz` and `/readyz` returned HTTP 200. One schema migration ran (`schema_migrations` 22→23; tables 56→57; SQLite `user_version` remained 0).
5. A fresh Chromium Owner login succeeded after upgrade. Organization/project counts remained one each; the credential ID, `API_KEY` kind, version 1, and fingerprint `...2086` matched; the audit chain verified intact with five events. After another RC5 restart using the metadata-injected exact-source binary, readiness remained HTTP 200 and Owner login, credential metadata, and audit integrity were retained. See [loaded post-migration Overview](../evidence/CP43-rc5-after-migration-overview.png) and [sanitized result](../evidence/CP43-migration-result.json).

## Coverage and limits

- **PASS, scoped:** migration and one restart from commit `a771311…` into RC5 for fresh SQLite setup, Owner session, workspace, one encrypted credential’s metadata/fingerprint, and audit chain.
- **UNVERIFIED:** exact official RC3 SHA/artifact, broader RC3 data shape, existing members/keys, secret decryption/use, interruption/rollback, PostgreSQL/Valkey, and production restore/upgrade conditions.
- No migration defect was confirmed for this source candidate. This does not close the RC3 upgrade requirement.
