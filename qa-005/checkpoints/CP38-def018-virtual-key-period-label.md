# CP38 — DEF-018 Virtual Key budget period label

**Baseline:** exact source `737079ddb847d97dbcf832f610ed0048556cb65b`; local build SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite runtime `127.0.0.1:18145`; Chromium 1440×1000; Owner / Operator; primary project scope.

## Result: PASS (scoped fresh readback)

Opened FinOps → Budgets & Holds. The authenticated project-scope request returned two Virtual Key budget rows with `period=TOTAL_LIFETIME`, and the actual Console displayed **Total lifetime** for both rows. The persisted key-budget records therefore agree with both API and visible UI for the two currently listed keys. The screenshot was visually inspected: [`CP38-finops-vkey-period.png`](../evidence/screenshots/CP38-finops-vkey-period.png). Sanitized response scope and limitations: [`CP38-finops-vkey-period.json`](../evidence/CP38-finops-vkey-period.json).

This is a narrow current-state non-reproduction of DEF-018 on two key rows. Historical QA-002/QA-004 records reported 5 and later 14 Virtual Key rows presented as MONTHLY despite `TOTAL_LIFETIME` persisted state. Those failures remain preserved. This run did not reconstruct that larger set or test other roles, APIs/exports, expired Playground keys, pagination, or PostgreSQL; it does not establish broad resolution.
