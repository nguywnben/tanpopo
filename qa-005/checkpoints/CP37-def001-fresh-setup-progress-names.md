# CP37 — DEF-001 fresh-setup progress accessible names

**Baseline:** exact source `737079ddb847d97dbcf832f610ed0048556cb65b`; local Linux amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; new isolated SQLite runtime `127.0.0.1:18150`; health and bootstrap-status endpoints each returned HTTP 200; Chromium 1440×1000. The bootstrap remained uninitialized and no form was submitted.

## Result: PASS (scoped DEF-001)

Opened `/setup` in a clean browser context and inspected the rendered step controls and their `aria-label` values. The owner account, first connection, key custody, and review buttons had accessible labels `Go to step 1: Owner account`, `Go to step 2: First connection`, `Go to step 3: Key custody`, and `Go to step 4: Review`. Each accessible label includes the matching visible step title. Steps 2–4 were disabled with `aria-disabled=true` until prerequisites are complete; step 1 was available. Visually inspected screenshot: [`CP37-fresh-setup-start.png`](../evidence/screenshots/CP37-fresh-setup-start.png).

This is fresh exact-RC7 Chromium evidence for the accessible-name slice. It does not retest subsequent wizard transitions, keyboard progression through completed steps, axe-core rules, screen readers, Firefox/WebKit, localization, or narrow layouts. Historical UI/library test disagreements remain preserved; no result is inferred from them.
