# DEF-030 — Routes & Policies retains stale environment routes after a workspace scope switch

**Severity:** Moderate (P2)
**Status:** Confirmed on frozen QA-002 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`.
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Summary

A delayed route-list request for an environment can finish after switching the Console to **No project / No env**. The Routes & Policies page then shows the prior environment’s routes beneath a banner saying routes and policies require a project and environment. Each stale route still has Edit and Delete actions; the edit drawer opens and **Save route** is enabled. No change was submitted. This is a confirmed scope/state consistency defect with risk of editing or deleting a route for an unintended environment. No authorization bypass was observed.

## Reproduction

1. Sign in to the isolated RC4 Console as the synthetic ORG_OWNER. Select `QA002 CP43 Secondary Project / qa002-cp43-env2`.
2. Use browser-level Playwright routing to hold the first `GET /admin/v1/routes?env_id=env_01m85ry445e009xg6sr14c5q1z&limit=100` response.
3. Open **Routes & Policies → Routes**. The delayed route request returns HTTP 200 with three routes: `qa002-cp52-failover-route`, `qa002-cp51-org-budget-stream-route`, and `qa002-cp51-org-budget-route`, all scoped to the held environment.
4. Before releasing the request, switch the workspace selector to **No project / No env**. The page correctly clears to zero routes and displays “Select a project and environment in the top bar to manage routes and policies.” Capture `CP63-routes-org-only-before-response.png`.
5. Release the HTTP 200 response. The scope selector stays at No project / No env, but all three prior environment routes appear. The same no-scope banner remains. Capture `CP63-routes-org-only-after-response.png`.
6. Open the first row action menu. It offers **Edit route** and **Delete route**. Open Edit route; the drawer shows the prior environment’s two deployment targets and an enabled **Save route** button while the workspace still has no project/environment selected. Evidence: `CP63-stale-route-actions-org-only.png`, `CP63-stale-route-edit-org-only.png`, and `CP63-routing-stale-actions.json`.
7. Do not submit Save or Delete. No route, policy, or deployment was changed.

## Expected versus actual

- **Expected:** Route and deployment data may be rendered and acted upon only when they match the currently selected project/environment. A pending request from an earlier scope must be ignored after the user changes scope. No-project/no-environment state should not expose environment route edit/delete actions.
- **Actual:** The late response repopulates three environment-specific routes in organization-only state. The rows expose Edit/Delete; Edit opens with the previous environment targets and allows Save.

## Impact

Operators can mistake a route from a different environment for a valid organization-wide route and make a traffic-routing change against the wrong environment. The stale row is visible under contradictory scope instructions, and the editor permits Save. This test used an Org Owner who was authorized to manage both scopes; it does not establish cross-tenant or limited-role authorization bypass. No mutation was executed. Severity is Moderate because route edits/deletions can affect inference traffic and recovery.

## Facts and root-cause hypothesis

**Facts:** the delayed request URL included `env_id=env_01m85ry445e009xg6sr14c5q1z`, returned HTTP 200 with three routes, and the UI displayed those three after the chooser showed No project / No env. The list contained row actions; the first row’s menu exposed Edit/Delete; its edit drawer had two existing ON_PREM targets and Save route was enabled.

**Source observation / hypothesis:** `web/console/src/pages/routes/RoutingPage.tsx` clears `routes` and `deployments` in the no-scope early return, then an earlier `Promise.all` can resolve and unconditionally write the captured environment’s results. There is no visible request-generation or scope equality guard on those writes. This likely explains the controlled race; it is a source-based hypothesis consistent with runtime evidence.

## Acceptance criteria

- Bind each route/deployment/policy refresh result to the captured current scope and ignore it after project/environment changes or unmount.
- Under a held environment route response, switching to No project / No env keeps the route list empty after the old response is released.
- Route Edit/Delete actions and Save remain unavailable unless the current selected scope matches the resource’s project/environment.
- Switching between two environments under reordered response timing never shows or mutates the prior environment’s routes or deployments.
- Regression tests preserve organization/project policy views while preventing stale environment routes and actions. Retest DEF-029 Access & Keys separately; these manifestations share scope-race mechanics but have different UI/impact contracts.
- Preserve server-side authorization; do not broaden route mutation permissions to make the Console state pass.

## Evidence

- `../evidence/CP63-routing-scope-race.json` — actual request query, 200 response, route slugs/environment IDs and UI before/after.
- `../evidence/CP63-routing-stale-actions.json` — enabled Save state, menu actions, request scope and route list count.
- `../evidence/CP63-routes-loading-before-scope-switch.png` — selected environment with pending request.
- `../evidence/CP63-routes-org-only-before-response.png` — correct cleared state before response.
- `../evidence/CP63-routes-org-only-after-response.png` — visually inspected stale routes under No project / No env.
- `../evidence/CP63-stale-route-actions-org-only.png` and `../evidence/CP63-stale-route-edit-org-only.png` — visually inspected actions and enabled route editor.
- Related findings: DEF-023 and DEF-029; see [CP62](../checkpoints/CP62-access-keys-scope-race.md).

No Tanpopo implementation, server configuration, or QA database was modified for this race. Delay was injected only in the browser test client.
