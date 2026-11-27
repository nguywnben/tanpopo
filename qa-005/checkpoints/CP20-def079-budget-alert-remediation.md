# CP20 — DEF-079 budget-alert remediation route retest

**Source:** exact RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; locally compiled runtime SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`. **Environment:** isolated SQLite runtime `127.0.0.1:18145`; Chromium/Playwright, synthetic owner and project budget alert.

## Result: PASS (scoped historical regression)

1. Signed in through the real login form as the QA-only owner and opened Overview. The populated page showed one `ALT-BUDGET-80` alert and its Remediate button. Captured and visually inspected `evidence/screenshots/CP20-budget-alert-overview.png`.
2. From the authenticated browser context, read `GET /admin/v1/alerts/active?limit=100`: HTTP 200; the alert's `remediation_route` was `/finops/budgets`.
3. Clicked Remediate. The browser navigated to `/finops/budgets`; heading was `FinOps`, the FinOps nav item was active, and the budget ledger displayed the corresponding project row. Captured and visually inspected `evidence/screenshots/CP20-budget-alert-remediation.png`.
4. Hard-refreshed the route. It continued to render FinOps and kept the FinOps nav item active. No browser page errors were recorded.

**Expected:** alert remediation loads the budget-management screen, and direct refresh continues to render it. **Actual:** those criteria passed in this synthetic project scope. This fresh RC7 result is independent of the historical RC6 FAIL and is not proof for other alert route types, organizations, role restrictions, deep links from other screens, or mobile layout. The test was read-only.
