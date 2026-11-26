# CP29 — Team Admin route catalog and unverified-provider route guard

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Outcome:** FAIL for a Team Admin's Routes & Policies inventory under DEF-054; PASS for the scoped Route Explain exclusion of an unverified provider under DEF-002. Provider recovery and live Data Plane dispatch remain UNVERIFIED.  
**Product source:** unchanged.

## Team Admin catalog consistency (DEF-054 scope expansion)

Signed in as a project-scoped `TEAM_ADMIN` in `default / dev`. The exact project/environment APIs show one logical model, two deployments, and one configured route (`qa004-loopback`). The real Models screen correctly displays one model and both deployment rows. The real Routes screen, after navigation and a hard reload, repeatedly shows `insufficient role permission`, `Configured routes 0`, and `No routes in this environment` despite `GET /admin/v1/routes?env_id=<dev>&limit=100` returning HTTP 200 with that route. Its Create route drawer has no deployment choices and cannot be saved.

The page requests `GET /admin/v1/policies/effective` without a project scope; for this Team Admin it returns 403 `insufficient_role_permission`. Project-scoped and environment-scoped policy calls return 200. In the frozen RC6 `RoutingPage.tsx` (lines 141–152, 154–160), all list and policy calls are awaited in one `Promise.all`, and the authorized route/deployment/model state is committed only after that aggregate resolves. This source and runtime evidence identify the failure mechanism: the denied organization-policy lookup rejects the page refresh before otherwise authorized catalog state is set. The documented screen contract grants Team Admin route create/edit/delete (`docs/ux/screen-specifications.md`, SCR-08) and names the routes APIs.

This is an additional role/screen reproduction of DEF-054's existing all-or-nothing catalog failure, not a separate defect ID. The observed effect also blocks the real UI route creation journey because its target selector has no choices. The observed organization policy 403 remains a valid authorization boundary; the defect is the loss of independently authorized route data and the misleading page-level empty state.

Evidence: [sanitized runtime results](../evidence/CP29-teamadmin-route-catalog.json), [Models screenshot](../evidence/CP29-teamadmin-models.png), [Routes screenshots](../evidence/CP29-teamadmin-routes-repro.png), [Create route drawer](../evidence/CP29-teamadmin-route-create-drawer.png). Screenshots were visually inspected during this run.

## DEF-002 route guard retest

Using the same isolated Team Admin and `default / dev`, created a temporary route targeting only the existing `PROVISIONED` deployment backed by the failed synthetic provider. Creation returned 201, so route configuration itself permits the target. `POST /admin/v1/routes/explain` for that route returned HTTP 200 with `dry_run: true`, zero selected candidates, and the deployment rejected at `HARD_COMPLIANCE_FILTER` with `CONNECTION_INACTIVE`. The temporary route was deleted (204), and a fresh route-list read returned the original single `qa004-loopback` route. Route Explain performs no upstream socket dispatch.

This verifies the dry-run candidate filter for this failed-provider case. The earlier `ACTIVE_HEALTHY` promotion denial remains intact. Actual gateway dispatch against the configured failed route, recovery after the provider is repaired, other adapter types, and failover are still UNVERIFIED; this checkpoint does not claim full DEF-002 acceptance.

## Coverage update

- **FAIL:** Team Admin Routes & Policies catalog / create-target availability in a populated authorized environment (DEF-054 scope extension).
- **PASS (scoped):** Route Explain excluded a configured `PROVISIONED` deployment whose provider connection was inactive; no provider candidate was selected.
- **UNVERIFIED:** Live Data Plane request against an unverified-only route, provider recovery, alternative provider adapters, retry and failover.
- **No new defect ID:** Same confirmed all-or-nothing catalog failure already tracked as DEF-054.
- **Gate:** `GATE-REL-07` remains BLOCKED; QA-004 continues.
