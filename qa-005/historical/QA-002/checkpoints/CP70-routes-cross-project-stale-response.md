# CP70 — Routes & Policies cross-project stale response

**Result:** FAIL — DEF-030 expanded from organization-only to a different valid project/environment.  
**Baseline:** frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Method and result

In Chromium at 1440×900, signed in as the synthetic organization owner on the isolated RC4 runtime, I held the HTTP 200 route-list response for `QA002 CP43 Secondary Project / qa002-cp43-env2`. It returned three routes: `qa002-cp52-failover-route`, `qa002-cp51-org-budget-stream-route`, and `qa002-cp51-org-budget-route`.

While that request was pending, I switched the real workspace selector to `QA002 CP39 Tenant Project / qa002-cp39-env`. The page showed **Configured routes 0** and “No routes in this environment.” I released the old response. With the tenant project/environment still selected, the page changed to **Configured routes 3** and displayed the three secondary-project routes. I visually inspected the before/after screenshots. Opening the first row’s action menu exposed **Edit route** and **Delete route**. No action was selected or submitted.

The run used an Org Owner authorized for both projects; it proves page state/resource-scope inconsistency, not a cross-tenant or limited-role authorization bypass. No route mutation occurred.

## One-run transition observation

In the first run only, the page also displayed `env_id does not belong to supplied project_id` during the project switch; the stale list still appeared after release. A repeat project switch recorded two old-environment route requests and one selected-environment request, all HTTP 200, settled at zero routes, and showed no error. The first run screenshot was overwritten by the repeat and its request/response details were not retained. I record this as **QA-OBS-012 (UNCLASSIFIED, one transient occurrence)**, not as another confirmed defect. Details and limitations: `../evidence/CP70-project-switch-observation.json` and `../evidence/CP70-project-switch-transition.json`.

## Evidence

- `../evidence/CP70-routes-cross-project.json` — held response, exact old environment, three rows, selected new scope, and visible Edit/Delete actions.
- `../evidence/CP70-routes-source-scope-loading.png` — source project/environment with the old request pending.
- `../evidence/CP70-routes-project1-before-release.png` — visually inspected tenant project/environment empty state.
- `../evidence/CP70-routes-project1-after-release.png` — visually inspected three old-project routes under tenant-project scope.
- `../evidence/CP70-routes-project1-action-menu.png` — visually inspected actionable Edit/Delete menu under the wrong project context.
- `../evidence/CP70-project-switch-transition.json` and `CP70-project-switch-settled.png` — follow-up transition requests and settled empty state.
- `../evidence/CP70-project-switch-observation.json` — QA-OBS-012 reconciliation and screenshot limitation.
- Updated finding and acceptance criteria: [DEF-030](../reports/DEF-030-routing-stale-after-workspace-scope-change.md).
- No-scope reproduction remains in [CP63](CP63-routing-scope-race-and-stale-actions.md).

## Coverage update

DEF-030 remains Moderate (P2) and now has confirmed Chromium evidence for both no-scope and valid project-to-project switches. Route Edit/Delete stay visible after stale rows arrive under the tenant-project selector. Limited-role visibility, environment-to-environment switching within one project, action submission after stale load, and other page loaders remain unverified. Preserve server-side authorization; do not mutate a route to test this inconsistent state.

No product code, database, or server configuration was changed. Only the browser response order was controlled.
