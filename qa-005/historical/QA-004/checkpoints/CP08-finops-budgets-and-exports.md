# CP08 — FinOps first budget, pre-egress limit, and selected-dimension exports

**Outcome:** PARTIAL. Four historical findings receive narrow fresh QA-004 PASS outcomes (DEF-003, DEF-012, DEF-014, DEF-015). Budget pre-egress enforcement passed one project-scope slice. Rate cards, traces, and other untested FinOps workflows remain open.

## First budget and persistence (DEF-003)

- Started from a real fresh test-root tenant with no budget rows. Opened FinOps and inspected the empty state; the Console showed a working first-budget path.
- Created a monthly PROJECT budget of 100 micro-USD from the Console drawer. Success notice identified the project; authenticated API readback returned the first row and expected tier, scope, period, limit, version.
- Hard-refreshed the browser and read the same budget through the API; it remained at 100 micro-USD (version 5 after updates below). Screenshot `evidence/CP08-finops-budget-persisted-refresh.png` was visually inspected.
- This passes only first project/monthly creation and refresh persistence. Organization/environment/key scopes, duplicate creation, thresholds, and rollover remain open.

## Historical settlements without budget rows (DEF-012)

Before creating the budget, two synthetic requests had durable exact and estimated settlements while the budget list was empty. The actual FinOps UI showed exact `$0.000001`, estimated `$0.000001`, and zero budget ceilings; the API aggregate agreed. The screenshot `evidence/CP07-native-finops-after-stream.png` and API evidence `evidence/CP07-native-ollama-persistent-usage.json` were visually/reproducibly checked. This passes the historical headline omission slice; it does not establish every billing mode or filter.

**Open observation:** once a budget was created after the two prior settlements, its settled row started at zero. A subsequent exact request raised it to one micro-USD while the overall summary remained two exact / one estimated micro-USD. Current investigation has no explicit contract establishing whether a new budget should count earlier activity in the same period. This is preserved as an observation, not classified as a product defect. Confirm intended effective-start semantics before using budgets for mid-period financial reconciliation.

## Hard budget limit

- Updated the project hard limit to 1 micro-USD through the inline Console control; API returned limit 1 and exact settled balance 1.
- Attempted a real cookie-free native request using the authorized PROVIDER Virtual Key. Tanpopo returned HTTP 429 `project_budget_exceeded`; test mock request count remained unchanged (3 before and after), confirming rejection before upstream dispatch.
- Restored limit to 100 through the Console and confirmed the value survived a full page reload and API read (version 5). This checks one serial exact-cap case only. Organization/environment/key hierarchy, estimated holds, concurrency, stream reservation, overrun, warnings, reset boundary, restart, and multiple role cases remain UNVERIFIED.

## Chargeback CSV/JSON and group selection (DEF-014/015)

- On the actual Chargeback UI selected `Model`; visible row was `qa004-mock`, exact 2 micro-USD/2 requests, estimated 1 micro-USD/1 request.
- Downloaded CSV and JSON from UI buttons. Model CSV contains valid header and row `qa004-mock,2,1,2,1,0`; JSON contains the same model totals. No JSON parse error appeared for CSV.
- Changed selection to `Project`; actual CSV filename was `tanpopo-chargeback-project.csv` and group key was `default`. It differed from Model output and matched the visible Project row. Both formats used the requested grouping for tested cases.
- This passes the single-row Project/Model selection and actual CSV parsing slices. Environment and Virtual Key groupings, larger/multiple rows, malicious CSV cells, filtering, pagination, locale, and RBAC remain open.
- Evidence with exact download bytes and hashes: `evidence/CP08-finops-functional-readback.json`; screenshots `CP08-chargeback-after-refresh.png`, `CP08-chargeback-model.png`, and `CP08-finops-budget-persisted-refresh.png`. All were visually inspected. Other evidence records the first budget and pre-egress cap response.

## QA-004 disposition summary

Fresh QA-004 scoped PASS: historical DEF-003, DEF-012, DEF-014, DEF-015, and previously scoped DEF-037. PARTIAL: historical DEF-043, DEF-048, DEF-049. FAIL: new DEF-051, DEF-052, DEF-053. Remaining 42 of 50 historical defects are UNVERIFIED. No prior campaign PASS was inherited; no product implementation was changed; no real external provider was called.
