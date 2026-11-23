# DEF-018 — FinOps presents Playground lifetime budgets as monthly

**Severity:** Moderate  
**Status:** Confirmed on exact QA-002 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Summary

The FinOps Budgets & Holds table and `GET /admin/v1/budgets` label internal Playground session Virtual Key budget ledgers `MONTHLY`, although the persisted records use `period_kind=TOTAL_LIFETIME`. The same five key IDs were present in the rendered table/API list and in read-only SQLite ledger state. This misstates the cap’s reset period in the primary cost governance screen and management API.

## Reproduction

1. Start from the fresh isolated SQLite baseline runtime at `127.0.0.1:18086` after CP22’s Playground sessions have each been assigned their TTL-bound lifetime budget.
2. Sign in as the test owner and open **FinOps → Budgets & Holds**.
3. Observe the five `VIRTUAL_KEY` Playground rows. The `Period` column shows `MONTHLY` for each; example key `vky_01m856b4ype0091h727havv8c7` is shown with `$0.000001` exact settled and a `500000` micro-USD cap. Visually inspected screenshot: `../evidence/CP26-finops-initial.png`.
4. The authenticated management API `GET /admin/v1/budgets?project_id=prj_01m853e561e00byw750arfmw8y&limit=100` returns `period:"MONTHLY"` for those same budget IDs and key IDs (`../evidence/CP26-budget-api.json`).
5. Read-only SQLite query of `budget_ledgers` by those IDs shows `scope_type=VIRTUAL_KEY` and `period_kind="TOTAL_LIFETIME"` (`../evidence/CP26-budget-ledger-state.json`).

## Expected versus actual

- **Expected:** FinOps and the management API accurately identify the persisted budget period. A total-lifetime cap must not appear as monthly. If the public API cannot expose lifetime periods, these internal rows need an accurate representation or exclusion from the monthly/daily budget resource path, with an explicit supported editing contract.
- **Actual:** UI and API serialize every non-`DAILY` DB period, including `TOTAL_LIFETIME`, as `MONTHLY`.

## Affected workflow and business impact

An operator reviewing Playground spend can incorrectly infer a reset at the next month boundary, compare unrelated lifetime caps as monthly limits, or misunderstand the remaining allowance. The Console exposes editable hard-limit controls beside these mislabeled rows, increasing the chance that a user edits a lifetime ledger under an incorrect period assumption. No evidence yet shows that a displayed monthly period was actually changed by saving one of these rows; this remains a required regression case.

## Facts and root-cause hypothesis

**Facts:** Same budget IDs are returned by the UI’s source Admin API as `MONTHLY`; the persisted rows use `TOTAL_LIFETIME`; `TOTAL_LIFETIME` rows correspond to Playground Virtual Keys and have their own start/end timestamps. See CP22/CP26 evidence links and `CP26-budget-ledger-state.json`.

**Hypothesis, not needed to establish the defect:** The Admin API DTO mapper appears to map `DAILY` explicitly and all other period kinds to `MONTHLY`, or otherwise uses a DTO that has no `TOTAL_LIFETIME` value. Confirming and fixing the implementation is an engineering task; QA did not change source.

## Acceptance criteria

- A Playground Virtual Key’s `TOTAL_LIFETIME` cap is accurately represented in the authenticated Admin API and FinOps UI; it never serializes or renders as `MONTHLY`.
- The UI communicates its exact expiration/period semantics and exposes only edits that preserve the intended lifetime cap.
- Saving a hard-limit edit cannot silently convert a lifetime ledger to monthly or reset its period window. Verify before and after database state, API response, FinOps rendering, and enforcement through key expiry.
- Existing daily/monthly project/environment/org budget representations and updates remain correct.
- Cover newly created and expired Playground sessions, budget list pagination, locale labels, and an operator who can read but not manage budgets.

## Evidence

- `../evidence/CP26-finops-initial.png` — visually inspected table showing five Virtual Key periods as MONTHLY.
- `../evidence/CP26-budget-api.json` — authenticated API summaries for the same IDs.
- `../evidence/CP26-budget-ledger-state.json` — sanitized DB rows showing `TOTAL_LIFETIME`.
- `../evidence/CP26-finops-after-budget-test.png` — same mismatch remains after a separate Playground session.

QA-002 remains in progress; no release approval is implied.
