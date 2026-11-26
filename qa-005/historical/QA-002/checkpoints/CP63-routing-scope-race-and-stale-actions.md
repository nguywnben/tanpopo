# CP63 — Delayed Routes & Policies response after workspace scope switch

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Result:** FAIL DEF-030 confirmed. A delayed environment route response repopulated three routes after changing to No project / No env; row Edit/Delete remained available and Save route was enabled. No mutation was submitted.
**Release gate:** `GATE-REL-07 BLOCKED`.

## Test and outcome

Used the real owner Console in Chromium. The browser test client held only the `GET /admin/v1/routes` response for environment `env_01m85ry445e009xg6sr14c5q1z`; the product binary and server were unchanged. The response returned 200 with three persistent routes from the selected project/environment.

Before releasing the response, I changed the real workspace chooser to **No project / No env**. The page showed its scope banner and an empty route state. When the original response was released, the selected scope remained unchanged but the page populated all three old routes and their target health. This reproduces an out-of-order UI state write.

The issue is more actionable than the Access & Keys race (DEF-029): under no scope, each stale route still exposed Edit/Delete; Edit opened a drawer with environment-specific targets and an enabled Save route button. This proves the UI permits preparing a route mutation from a contradictory workspace state. I inspected the controls but did not save, delete, or alter any persisted route. No authorization bypass was tested or observed.

## Evidence

- [`../evidence/CP63-routing-scope-race.json`](../evidence/CP63-routing-scope-race.json) — HTTP 200 request/response scope and page text before/after.
- [`../evidence/CP63-routing-stale-actions.json`](../evidence/CP63-routing-stale-actions.json) — actions, enabled Save and route scope.
- [`../evidence/CP63-routes-loading-before-scope-switch.png`](../evidence/CP63-routes-loading-before-scope-switch.png), [`../evidence/CP63-routes-org-only-before-response.png`](../evidence/CP63-routes-org-only-before-response.png), and [`../evidence/CP63-routes-org-only-after-response.png`](../evidence/CP63-routes-org-only-after-response.png) — visually inspected loading, empty, and stale list states.
- [`../evidence/CP63-stale-route-actions-org-only.png`](../evidence/CP63-stale-route-actions-org-only.png) and [`../evidence/CP63-stale-route-edit-org-only.png`](../evidence/CP63-stale-route-edit-org-only.png) — visually inspected Edit/Delete menu and enabled route drawer.
- Related: DEF-023 source/empty-state issue and DEF-029 key-list race. They should be fixed with scope-bound async state but tracked as separate screens and acceptance criteria.

## Consolidated QA status

- **Historical DEF-001–015:** not retested in CP63. Preserve previous scoped QA-002 results; DEF-005 remains partial.
- **Confirmed open defects:** DEF-016–029 remain open; new **DEF-030 Moderate** is confirmed. No QA-001 PASS is inherited.
- **Coverage completed:** delayed environment route response, no-scope transition, persisted environment identity, visible stale actions, editor with enabled Save, and screenshot inspection.
- **Blocked:** none in CP63. Fresh Team Admin retest remains blocked by its suspended/cleared disposable account; CP53 webhooks remain blocked on Operator access.
- **Remaining:** route response order between environments, role-limited mutation boundary, safe navigation recovery, and similar races in remaining scope-sensitive screens.
- **Engineering handoff:** prioritize DEF-030 with DEF-029/DEF-023 scope consistency work, but keep separate acceptance tests; no implementation change was made.

QA-002 remains in progress. `GATE-REL-07` remains **BLOCKED**.
