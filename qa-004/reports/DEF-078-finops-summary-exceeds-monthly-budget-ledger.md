# DEF-078 — FinOps usage totals exceed the matching monthly project budget ledger

**Severity:** Moderate  
**Status:** FAIL  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite `127.0.0.1:18128`  
**Affected scope:** synthetic Org Owner, `QA004 Test Root Org / default / dev`

## Summary

For the default project's current monthly budget, the Console/API budget ledger reports 2,043 exact and 14 estimated micro-USD settled, while FinOps project chargeback for November reports 2,044 exact and 15 estimated micro-USD. Both exact and estimated totals differ by one micro-USD. The same mismatch is visible in the real FinOps screens and was present in the CP136/CP137 historical snapshots before the three recent formula-prefix runs.

This creates a reconciliation gap between the spend used for project budget availability and the spend displayed/exported to Finance. Its exact monetary value is small in this test dataset; the concern is the unexplained missing amount and whether similar omissions can accumulate at higher volume.

## Reproduction

1. Start the isolated exact-RC6 runtime at `127.0.0.1:18128`; sign in as the synthetic Org Owner and select `default / dev`.
2. Request `GET /admin/v1/budgets?project_id=<default-project-id>&include_ancestors=true&limit=100` with `X-Tanpopo-Org-ID` from the authenticated session. Read the row with `scope_tier=PROJECT` and `period=MONTHLY`.
3. Request `GET /admin/v1/usage/summary?group_by=project` with the same organization header. Repeat with `from=2026-11-01T00:00:00Z` to compare the same November period.
4. Actual response: budget ledger exact `2043`, estimated `14`, reserved `0`, hard limit `100` micro-USD; project summary exact `2044`, estimated `15`, one row for `default`. The November-filtered summary is unchanged.
5. In real Chromium, open FinOps → Chargeback and then FinOps → Budgets & Holds for the same `default / dev` scope. The visually inspected Chargeback summary displays `$0.002044` exact / `$0.000015` estimated; the project budget row displays `$0.002043` exact / `$0.000014` estimated. The hard limit is restored to 100 micro-USD.
6. Historical corroboration: CP136 reports the same November project rollup `2038 / 15`; CP137's initial persisted project budget readback is `2037 / 14`, before CP137's request. CP138/CP139 each add their independently recorded local usage while preserving the same one-micro difference in both fields.
7. CP140 boundary confirmation: with budget at `2043 / 14` and Chargeback at `2044 / 15`, raised the hard limit to `2058` micro-USD. One `qa004-loopback` request returned HTTP 200. Budget settled at `2044 / 14` (exactly the `2058` limit), while Chargeback rose to `2045 / 15` (`2060` total). The key was revoked and the limit restored to 100. This confirms the visible Chargeback total can end two micro-USD above the configured cap when ledger headroom is one.
8. CP142 repeated the read-only comparison on a separate restored PostgreSQL runtime (`18124`): monthly project budget `1 / 1` exact/estimated versus November project summary `2 / 2`, including a `from=2026-11-01` query. The real budget screen showed `$0.000001 / $0.000001`; Chargeback showed `$0.000002 / $0.000002`. No writes or inference calls were made. This confirms the discrepancy is not limited to the 18128 SQLite runtime.

Sanitized readbacks are in [CP139 reconciliation results](../evidence/CP139-finops-budget-reconciliation-results.json), [CP140 boundary results](../evidence/CP140-budget-headroom-results.json), and [CP142 PostgreSQL comparison](../evidence/CP142-postgres-budget-reconciliation.json). Visually inspected real screens include [CP139 Chargeback](../evidence/CP139-control-chargeback.png), [CP139 project budget](../evidence/CP139-budget-vs-usage.png), [CP140 one-micro headroom](../evidence/CP140-budget-one-micro-headroom.png), and [CP140 post-request Chargeback](../evidence/CP140-chargeback-after-headroom.png), and [CP140 revoked key](../evidence/CP140-key-revoked.png), [CP142 PostgreSQL budget](../evidence/CP142-postgres-project-budget.png), and [CP142 PostgreSQL Chargeback](../evidence/CP142-postgres-chargeback.png). CP136 and CP137 historical evidence links are in the checkpoint records.

## Expected versus actual

- **Expected:** the current monthly project budget's settled exact and estimated amounts reconcile with usage settlements/FinOps totals for that same project and period. A documented exclusion or period boundary should explain any difference.
- **Actual:** the monthly budget ledger is lower by one micro-USD in exact and one micro-USD in estimated spend than the unfiltered and November-filtered project summary. The Console presents both as current spend without explaining the difference.

## Business impact

Finance and operations cannot reconcile budget availability to chargeback totals. A persistent undercount in the ledger can overstate remaining headroom; an overcount would incorrectly block inference. The QA dataset exhibits a two-micro-USD combined discrepancy. CP140 demonstrated that a boundary request can be accepted while the displayed Chargeback total ends two micro-USD above the configured hard limit. Customer impact magnitude and whether this accumulates remain unquantified.

## Facts and root-cause hypotheses

**Observed facts:**

- Both APIs returned HTTP 200 for the same organization and default project.
- The budget row is monthly and reports `2043` exact / `14` estimated; the usage summary since November 1 reports `2044` exact / `15` estimated.
- The UI shows those same different values. Project limit is restored to 100 micro-USD, with zero active reservations.
- Historical checkpoints show the same one-micro difference in both accuracy buckets.
- A separate restored PostgreSQL runtime shows the same one-micro difference in each bucket, with lower absolute totals; the discrepancy is not isolated to SQLite.
- Read-only frozen-source inspection shows the usage summary aggregates `usage_settlements`; the budget DTO reads `budget_ledgers`. No database mutation or source edit was performed for this finding.

**Hypotheses, not confirmed causes:** a settlement may not have updated the budget ledger, a period/scope mapping may exclude one row, or one projection may be stale. No single request has been attributed as the missing exact and estimated amount, and this report does not claim a source-level root cause.

## Acceptance criteria

1. For a fresh synthetic tenant and populated default project, monthly budget settled totals reconcile exactly with usage settlements for the same period, including exact and estimated buckets.
2. If a documented accounting rule causes a difference, the API and Console disclose the rule and expose enough period information to reproduce it.
3. Reconciliation is verified after exact and estimated settlements, errors, retries, scope changes, budget period rollover, and API/UI refresh.
4. Re-run the CP140 one-micro headroom boundary and prove admission uses the same total shown in Chargeback, with no missing or duplicated spend.

## Related findings and coverage

DEF-051/052 cover native provider metering and trace-versus-settlement projection mismatches; this report is a separate cross-view discrepancy between the monthly budget ledger and project chargeback summary. Root overlap is unknown. Remaining work includes request-level settlement-to-ledger attribution, other periods and scopes, and concurrency.

QA-004 remains incomplete. **GATE-REL-07 remains BLOCKED.**
