# CP21 — DEF-078 FinOps reconciliation retest

**Source/runtime:** exact frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`, local binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`, isolated SQLite on `127.0.0.1:18145`. **Identity:** synthetic QA owner, `Initial Project`, November 2026 monthly period. **Interactions:** actual Chromium FinOps Chargeback/Budgets navigation and CSV/JSON export; no provider usage was added during this read-only retest.

## Result: PASS (scoped non-reproduction)

1. Opened FinOps in the authenticated browser and captured the Chargeback and Budgets & Holds views; both screenshots were visually inspected.
2. Read the rendered views' actual API responses: `/admin/v1/usage/summary?group_by=project` and `/admin/v1/budgets?project_id=<synthetic project>&include_ancestors=true&limit=100`, both HTTP 200.
3. Current project usage summary was exact `1`, estimated `0`, 3 exact requests, 0 estimated requests, 0 unmetered. The matching monthly project-budget row reported settled exact `1`, estimated `0`, active hold `0`, hard limit `1` micro-USD.
4. CP19 exports independently returned the same exact/estimated totals and request counts.

**Expected:** for the matching project and period, settled budget and chargeback totals should reconcile or expose a documented exclusion. **Actual:** exact and estimated values reconcile in this fresh low-volume fixture. This is a scoped non-reproduction of the RC6 discrepancy, not proof the defect is fully resolved. This fixture has no estimated settlement and only three exact requests; it does not exercise retries, concurrency, rollover, multiple scopes, prior one-micro boundary behavior, or sustained volume.

Sanitized API responses: `evidence/CP21-def078-ledger-comparison.json`. Screenshots: `evidence/screenshots/CP21-def078-chargeback.png` and `CP21-def078-project-budget.png` (visually inspected). Raw downloaded exports were kept only in `/tmp/qa005-runtime`.
