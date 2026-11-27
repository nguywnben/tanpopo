# CP67 — Populated synthetic organization crypto-shred and receipt UX

**Baseline:** exact RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated offline SQLite RC6 runtime `127.0.0.1:18128`  
**Browser:** Chromium at 1440×1000; real Console interactions, screenshots visually inspected  
**Product source modified:** no

## Result

- **PASS (scoped):** Created a new synthetic organization, project, and environment through the Console. Created one project-scoped synthetic `API_KEY` credential through Providers → Seal secret. No provider connection or inference was configured. A fresh Vault Secrets view and scoped API read returned metadata only; the plaintext field was absent.
- **PASS (scoped):** Used the actual Operations → Organizations confirmation flow to crypto-shred the populated active canary. The delete returned HTTP 200 with a destroyed DEK identifier and zero revoked VKEYs. The organization no longer appeared in the directory. Its project and secret metadata reads returned zero records. Other organizations remained active and the root organization's two projects remained readable.
- **PASS (scoped):** The deleted tenant's `organization.crypto_shredded` audit event recorded the target and destroyed DEK identifier. Its five-event RFC 8785 tenant chain verified intact.
- **PASS (scoped; DEF-038 retest):** Created and verified a post-shred encrypted archive with the exact RC6 CLI. Restore with `--verify-secrets-decrypt` succeeded; restore's mandatory audit validation accepted the deleted tenant's retained crypto-shred partition and secret-envelope validation passed for all surviving organization/project DEKs. The restored SQLite database passed `PRAGMA integrity_check`, retained the target audit receipt, contained two surviving organizations, and had zero target organization, Org DEK, project, or secret metadata rows.
- **Initial test setup error resolved:** The first decrypt-check attempt used a different local key file and failed on a surviving root Org DEK. Retrying with the existing runtime-matching test-root KEK succeeded. This was a test-key selection mismatch, not a product failure.
- **FAIL — DEF-068:** The active-organization success receipt was not visible in the Operations Console after the page automatically switched scope. A second real-UI control deleted an inactive synthetic organization and displayed the receipt while preserving the active scope. Source confirms page-local receipt state is lost when the keyed workspace subtree remounts after switching away from the deleted active organization.
- **UNVERIFIED:** End-to-end `<60s` SLA; historical pre-shred cold-backup recovery of this particular canary (no pre-shred archive was retained; the shared-KEK pre-shred archive caveat remains); independent challenge against retained pre-shred ciphertext; interrupted/pending finalization; multi-node erasure; provider connection/key revocation counts beyond zero; other browsers/roles.

## Reproduction and checks

The populated test org was `qa004-cp67-crypto-shred` (`org_01m8a833jxe00ahft1pharg45y`); project `prj_01m8a8dat2e009xqwn19dakkc7`; environment `env_01m8a8gnqye00as8hejt158g0j`; and synthetic credential metadata ID `sec_01m8a8jww8e008554aeqmse8zm`. The project and environment were scoped through the workspace picker before credential creation. API reads used the same explicit `X-Tanpopo-Org-ID` context as the Console. The post-shred archive ID is `bc0e6549a5c42c9934ac34bb3546b49b`; it verified with 11 files and restored with `--verify-secrets-decrypt`. The restored audit validation is the fresh scoped retest for historical DEF-038. This verifies a post-shred archive, not recoverability of historical pre-shred archives under the shared KEK.

The inactive control organization was created and erased while another scope remained active. This distinguishes missing active-target feedback from a global receipt rendering problem.

## Evidence

- [Sanitized create, secret metadata, delete, tenant isolation and audit results](../evidence/CP67-crypto-shred-results.json)
- [Environment creation form](../evidence/CP67-environment-create-form.png)
- [Selected project/environment and offline overview after creation](../evidence/CP67-environment-created.png)
- [Write-only secret success notice](../evidence/CP67-secret-sealed-metadata.png)
- [Vault metadata after navigation/refresh](../evidence/CP67-vault-secret-after-refresh.png)
- [Active canary in tenant directory](../evidence/CP67-org-directory-before-shred.png)
- [Crypto-shred confirmation with exact slug](../evidence/CP67-crypto-shred-confirmation.png)
- [Active-org deletion: scope switched, success receipt absent](../evidence/CP67-active-org-shred-no-receipt.png)
- [Inactive-org control: receipt visible, existing scope preserved](../evidence/CP67-inactive-org-shred-receipt-visible.png)
- [Encrypted archive / restore outcomes and sanitized restored-database assertions](../evidence/CP67-crypto-shred-results.json)
- [Standalone defect report](../reports/DEF-068-active-org-shred-receipt-lost.md)

The `<60s` target was not timed. The restored DB itself and encrypted archive were removed from `/tmp` after sanitized checks; no database, key, archive or secret value is published. This checkpoint is not full crypto-erasure or disaster-recovery acceptance. QA continues and `GATE-REL-07` remains **BLOCKED**.
