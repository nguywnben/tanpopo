# CP106 — DEF-039 project policy without an environment

**Date:** 2026-11-27  
**Disposition:** **PASS (scoped; real project policy write at No env)**  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b` (unsigned RC7)  
**Runtime:** exact-source Linux binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite runtime on `18145`, real Console via TLS proxy `https://127.0.0.1:19444`  
**Browser / role:** Chromium 156, real Playwright UI, Org Owner, 1440×1000

## Reproduction

1. Sign in as the isolated QA Org Owner; select `QA005 Org / QA005 No Budget Project / No env` using the actual workspace picker.
2. Open Routes & Policies → Policies. The effective policy and editable scope selector are present. Scope to edit is Project; the form and Save policy control are enabled without selecting an environment.
3. Starting from the effective all-region policy, narrow the project editor to `ON_PREM` only. The real Console Save returned HTTP 200 and displayed “Saved Project policy.” The effective policy API returned only `ON_PREM` at project version 1; the UI provenance changed to Project floor.
4. Restore all four regions (`EU`, `US`, `APAC`, `ON_PREM`) in the project editor and save. The Console returned HTTP 200; effective project policy readback returned all four at version 2. A hard refresh initially restored the saved workspace’s environment selection; after explicitly selecting `No env` again, the project editor remained usable and API readback still returned the all-region value at `ETag: "v2"`.
5. In Security → Audit Log, confirm the same policy target has `policy.created` at sequence 567 and `policy.updated` at 568. The actual Verify hash chain action returned HTTP 200 and `intact:true` across 574 events. 6. Open Routes at No env: the page presents the zero-route scoped empty state. No route or deployment was created, edited, or deleted; no inference was sent.

The policy value is semantically restored to all four regions. The first write created the project policy row at version 1; the restoration updated it to version 2, so the isolated QA database now retains an explicit project policy with the same effective all-region value. This test-only state change is disclosed rather than represented as byte-for-byte database restoration. Existing QA deployments are ON_PREM; no live provider traffic was generated.

## Result and limits

The original exact-RC4/QA-002 environment prerequisite is not reproduced on exact RC7: a project policy can be narrowed and saved at project scope with `No env` active. The value persists across fresh page load and explicit scope reselection. The earlier RC6 CP81 pass and RC4/RC5 failures/conflicts remain in history. This is a scoped Owner/SQLite/Chromium pass, not proof of Team Admin, concurrency, inheritance-relaxation, guardrail lifecycle, PostgreSQL/Valkey, other-browser or keyboard/screen-reader behavior.

Evidence: [sanitized write/readback record](../evidence/CP106-def039-project-policy-no-environment.json), [No env project editor](../evidence/screenshots/CP106-policy-no-environment.png), [narrowed draft](../evidence/screenshots/CP106-def039-project-policy-narrowed-draft.png), [saved narrow policy](../evidence/screenshots/CP106-def039-project-policy-narrowed-saved.png), [restored policy](../evidence/screenshots/CP106-def039-project-policy-restored.png), and [fresh reload with No env reselected](../evidence/screenshots/CP106-def039-project-policy-fresh-reload.png), [audit events](../evidence/screenshots/CP106-policy-audit-log.png), and [verified chain](../evidence/screenshots/CP106-policy-audit-chain-verified.png). All listed screenshots were visually inspected.

`GATE-REL-07` remains **BLOCKED**; QA-005 continues.
