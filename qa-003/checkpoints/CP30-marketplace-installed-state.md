# CP30 — Marketplace installed-state reconciliation

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium 156; synthetic Org Owner  
**Disposition:** DEF-036 prior duplicate action/error was not reproduced for the installed `@tanpopo/ollama` state; **PARTIAL**. No extension action or persistent package state was changed. `GATE-REL-07` remains **BLOCKED**.

## Journey and result

1. Signed in to RC5 and opened Extensions → Installed. The authenticated installation list returned HTTP 200 with one installed package. The UI showed its lifecycle as Active and origin as Official.
2. Opened Marketplace. The real Console listed the built-in catalog including `@tanpopo/ollama`, `@tanpopo/gemini`, and other official adapters. The already-installed Ollama row displayed **Installed** instead of **Stage for review**; the other uninstalled packages retained the staging action. The screenshot was visually inspected: [Marketplace catalog](../evidence/CP30-marketplace-catalog.png). The Installed view was also visually inspected: [installed inventory](../evidence/CP30-marketplace-inventory.png).
3. No stage/install action was selected. No storage conflict or raw error was induced, and the installed package record remained unchanged.

## Reconciliation with DEF-036

QA-002 CP99 reproduced a duplicate Stage for review action on an already active `@tanpopo/gemini`, followed by HTTP 409 and raw storage/unique-constraint language. RC5 currently has only the Ollama package installed, and that same-version catalog item is correctly labeled Installed. This demonstrates correct behavior for one installed package identity but does not test the historical Gemini identity or whether all catalog rows reconcile installation state.

## Disposition and remaining coverage

- DEF-036 moves from UNVERIFIED to **PARTIAL**; the tested installed Ollama state has no duplicate staging action. The historical Gemini duplicate/error path, upgrade available, stale catalog/race, quarantined/disabled state, and recovery UX remain unverified.
- FT-06 and SCR-06 remain **PARTIAL**. No new defect was filed and no package was mutated.
- Historical register totals: 17 VERIFIED FIXED for scoped RC5 scenarios, 8 PARTIAL (DEF-005, DEF-007, DEF-029, DEF-032, DEF-035, DEF-036, DEF-037, DEF-039), and 14 UNVERIFIED.
- QA-003 remains incomplete; `GATE-REL-07` remains **BLOCKED**.
