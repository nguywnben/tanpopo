# CP107 — DEF-036 Marketplace installed-version state

**Date:** 2026-11-27  
**Disposition:** **PARTIAL (fresh RC7 same-version state; duplicate error path not reachable)**  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b` (unsigned RC7)  
**Runtime:** exact-source Linux binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite on `18145`; real Console via TLS proxy `https://127.0.0.1:19444`  
**Browser / role:** Chromium 156, real Playwright UI, Org Owner, 1440×1000

## Reproduction

1. Sign in and open Extensions → Installed. The Console shows two active official packages, including `@tanpopo/ollama` at v0.1.0.
2. Open Marketplace and inspect the Ollama catalog row. The row offers **Installed** (no Stage for review action). The catalog API and installations API each returned HTTP 200; the catalog lists `@tanpopo/ollama` v0.1.0 as official/signature-verified, and the matching installation is `ENABLED_ACTIVE` v0.1.0 with no candidate version.
3. Visually inspect the installed list and marketplace row. No Stage action was clicked, no write occurred, and no installation state changed. No provider call or paid usage occurred.

## Result and limits

The same-version duplicate staging action observed on QA-002 for `@tanpopo/gemini` is absent for the currently installed `@tanpopo/ollama` v0.1.0 row on RC7. This is a positive scoped same-version state, but DEF-036 remains PARTIAL because the historic Gemini duplicate-error path was not reachable without a visible action, a newer catalog version was unavailable, and conflict-race/error presentation plus upgrade/permission-consent recovery were not tested. The historical QA-002 raw storage 409 remains preserved; no defect is silently closed.

Evidence: [sanitized API/UI result](../evidence/CP107-def036-marketplace-installed-state.json), [installed packages](../evidence/screenshots/CP107-def036-installed-state.png), [Marketplace catalog](../evidence/screenshots/CP107-marketplace-inventory.png), and the [installed Ollama row](../evidence/screenshots/CP107-def036-marketplace-installed-package.png). All three screenshots were visually inspected.

`GATE-REL-07` remains **BLOCKED**; QA-005 continues.
