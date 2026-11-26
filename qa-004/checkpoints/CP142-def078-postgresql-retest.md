# CP142 — DEF-078 PostgreSQL retest

**Date:** 2026-11-26  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated restored PostgreSQL on `127.0.0.1:18124` (CP114 post-shred restore)  
**Outcome:** reproduces DEF-078 on a second backend; no new finding assigned.

## Reproduction and result

Signed in through real Chromium as the synthetic Org Owner and selected `QA004 Test Root Org / default / dev`. Read the current project budget and FinOps usage APIs, then opened both actual FinOps tabs. This runtime is separate from the SQLite instance used for CP139/140.

- Project budget API/UI: `MONTHLY`, hard limit 100 micro-USD, exact 1, estimated 1, reserved 0.
- Project summary API/UI: exact 2, estimated 2 for the single `default` row.
- The query filtered from `2026-11-01T00:00:00Z` still returned exact 2 / estimated 2.
- Budget UI visually showed `$0.000001` exact and `$0.000001` estimated; Chargeback showed `$0.000002` in both buckets. This repeats the same one-micro difference per accuracy bucket on the restored PostgreSQL backend.

No writes, inference requests, credentials, or provider calls were made. The PostgreSQL limit was already 100 and remained unchanged. This corroborates DEF-078 as a cross-backend discrepancy; the matched source row and root cause remain unknown.

## Evidence

- [Sanitized API readbacks](../evidence/CP142-postgres-budget-reconciliation.json)
- [PostgreSQL project budget UI](../evidence/CP142-postgres-project-budget.png)
- [PostgreSQL Chargeback UI](../evidence/CP142-postgres-chargeback.png)
- [DEF-078 report](../reports/DEF-078-finops-summary-exceeds-monthly-budget-ledger.md)

Screenshots were opened and visually inspected. All defect counts are unchanged. QA-004 remains incomplete. **GATE-REL-07 remains BLOCKED.**
