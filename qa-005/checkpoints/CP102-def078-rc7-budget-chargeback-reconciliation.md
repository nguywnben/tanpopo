# CP102 — DEF-078 exact-RC7 budget/chargeback reconciliation

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite through TLS proxy `https://127.0.0.1:19444`; Chromium 156; `ORG_OWNER`; QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev.

## Result

**PARTIAL (fresh read-only RC7 comparison; historical discrepancy not reproduced in this scope).** The real Console Budget & Holds view and same-project monthly budget API reported 3,000,000 exact, 0 estimated and 0 active holds for November 2026. The Chargeback view displayed the same `QA005 No Budget Project` at 3,000,000 exact and 0 estimated. A same-project API comparison filtered from `2026-11-01T00:00:00Z` returned the identical values, with three exact requests and zero estimated requests. Each API returned HTTP 200. Both actual screenshots were visually inspected. No budget, usage, key or other record was modified and no provider call occurred.

The project budget hard limit was 2,500,000 micro-USD and the ledger showed 500,000 micro-USD negative carryover; this comparison establishes reconciliation only, not the correctness of this pre-existing over-limit chronology or admission decision.

## Reproduction

1. Sign in to the exact-source Owner Console and set scope to QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev.
2. Open FinOps → Budgets & Holds. Read the November 2026 project row and note exact/estimated/hold totals and hard limit.
3. Open Chargeback. Compare the same project's displayed exact and estimated totals.
4. From the authenticated Console session, read `GET /admin/v1/budgets?project_id=prj_01m8d6dbtxe008w7fphkgp2e24&include_ancestors=true&limit=100` and `GET /admin/v1/usage/summary?group_by=project&from=2026-11-01T00:00:00Z`. Compare the matching project row, settlement buckets and period.

Screenshots: [Budget & Holds](../evidence/screenshots/CP102-finops-initial.png) and [Chargeback](../evidence/screenshots/CP102-finops-chargeback.png). Sanitized readback: [CP102 JSON](../evidence/CP102-def078-rc7-reconciliation.json).

## Reconciliation and remaining scope

This fresh exact-RC7 project comparison did not reproduce the QA-004 one-micro discrepancy. It does not overturn the historical SQLite and PostgreSQL evidence: QA-004's `default/dev` readbacks and Console screens showed Chargeback greater than the budget ledger by one micro-USD in both accuracy buckets, including a boundary case that exceeded the configured cap. The current RC7 comparison uses a different, pre-existing QA005 project and has no estimated settlements. Keep DEF-078 PARTIAL. Fresh default/seeded-project recreation, exact plus estimated budgeted settlements, request-level attribution, period rollover, concurrent settlement, boundary admission and other backends remain UNVERIFIED. `GATE-REL-07` remains BLOCKED.
