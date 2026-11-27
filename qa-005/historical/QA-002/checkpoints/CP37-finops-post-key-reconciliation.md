# CP37 — Reconcile key inference settlements with FinOps and retest DEF-018

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Runtime:** isolated SQLite Tanpopo at `127.0.0.1:18086`, synthetic provider fixtures, real Chromium Console
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Journey and reconciliation

- Signed in as Org Owner, opened FinOps → Budgets & Holds, then Chargeback, and used the real Group by control to select **Model**.
- The settled overview showed `$0.000010` Exact settled, `$0.000000` Estimated settled and `$0.000000` active holds. The Chargeback table showed `qa002-cp22-streaming` at `$0.000010`, `10 / 0` exact/estimated requests and zero unmetered requests. Screenshots were visually inspected.
- CP35's provider-key inference contributed one exact one-micro settlement. CP36's pre-rotation request plus old/new credentials used during the dual-valid window contributed three exact one-micro settlements. These four synthetic calls appear in the current ledger aggregate, whose database total is ten exact one-micro settlements and $0.000010. The aggregate reconciles.
- FinOps Budgets & Holds visually marked the visible Virtual Key budgets `MONTHLY`. Authenticated `GET /admin/v1/budgets` returned 15 rows: 14 `scope_tier=VIRTUAL_KEY, period=MONTHLY`, and one `scope_tier=PROJECT, period=MONTHLY`.
- Read-only SQLite matching by all 15 budget IDs showed the single Project row has `period_kind=MONTHLY`, but all 14 Virtual Key rows have `period_kind=TOTAL_LIFETIME`. This independently reproduces DEF-018 and expands it from CP26's five exact rows to the entire current set of 14 Virtual Key budgets. No source was changed.

## Evidence

- `../evidence/CP37-finops-budget-summary.png` — visually inspected Budgets & Holds page.
- `../evidence/CP37-finops-chargeback.png` — visually inspected current default Project grouping.
- `../evidence/CP37-finops-chargeback-model.png` — visually inspected Model grouping and exact total.
- `../evidence/CP37-finops-cross-feature.json` — rendered summary/chargeback text and read outcomes.
- `../evidence/CP37-finops-budget-api.json` — authenticated API scope/period summary.
- `../evidence/CP37-finops-budget-api-rows.json` — sanitized list of all API budget IDs and values used for matching.
- `../evidence/CP37-finops-budget-db.json` — read-only database period counts by scope.
- `../evidence/CP37-finops-budget-period-crosscheck.json` — all-ID API/database match and mismatch counts.

## Result and remaining limits

- **PASS**: current synthetic settlement aggregate and Model-grouped chargeback view reconcile exactly for this accumulated database.
- **FAIL DEF-018**: every current Virtual Key `TOTAL_LIFETIME` budget row in this runtime is presented as `MONTHLY` by API/UI; Project period representation matches.
- Other group dimensions, export after this data set, budget edits, period rollover, locale, non-owner finance roles and production-provider settlement remain UNVERIFIED. QA-002 continues; `GATE-REL-07` remains BLOCKED.
