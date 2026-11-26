# CP34 — DEF-060 disabled rollback guidance

**Baseline:** exact source `737079ddb847d97dbcf832f610ed0048556cb65b`; local build SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite runtime `127.0.0.1:18149`, local Operator / Org Owner, real Chromium 1440×1000. No source or runtime data mutations were made.

## Result: PARTIAL

Opened Extensions → Installed and the action menu for the `@tanpopo/ollama` installation. The fresh HTTP 200 readback returned `previous_rollback_version:null` and version 4. In the real menu, **Rollback version** was present with `aria-disabled="true"`, plus the explanatory text “No previous pin exists. Use Quarantine for emergency cutoff.” The screenshot was visually inspected and confirms the disabled visual treatment and the safe alternative.

This is fresh evidence that the previously omitted control and reason are now rendered for an installation with no prior pin. Focused the disabled menu item and sent Enter; the browser issued no rollback request, and an authenticated HTTP 200 inventory read remained `INSTALLED_DISABLED`, version 4, with no prior pin. This is a partial result because CP34's row was `INSTALLED_DISABLED` after CP33's explicit recovery, rather than the initial ACTIVE state in the historical reproduction; screen-reader announcement and the enabled rollback path after a real upgrade were not tested. Historical QA-004 DEF-060 FAIL remains preserved.

Evidence: [`CP34-disabled-rollback-guidance.png`](../evidence/screenshots/CP34-disabled-rollback-guidance.png) (visually inspected). Browser script and credentials remain outside the repository.
