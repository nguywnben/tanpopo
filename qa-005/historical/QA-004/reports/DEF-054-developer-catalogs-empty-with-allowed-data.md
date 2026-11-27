# DEF-054 — Console hides authorized model and route data after unrelated permission errors

**Severity:** Moderate
**Status:** FAIL (confirmed current RC6)
**Area:** Developer and Team Admin RBAC, model/route catalogs, workspace scope, Console error handling

## Summary

A project-scoped `DEVELOPER` can read logical models, deployments, routes, and use Route Explain through the documented interfaces. In the real Console, Models & Deployments and Routes & Policies instead show an `insufficient role permission` banner and empty states. The same session's relevant list endpoints return records, and Route Explain identifies the route. This contradicts the visible catalogs and can mislead a Developer into believing the project has no configured models or routes.

## Reproduction

1. On the isolated RC6 clone, sign in as the synthetic Org Owner. In Access & Keys → Members, add a synthetic `.invalid` account with only the `Developer` role and the `default` project selected under Scoped projects. Activate its one-time invitation outside the screenshot evidence and sign in as that Developer.
2. At `/models`, wait for the page requests to complete. The UI displays `insufficient role permission`, `Environment deployments 0`, and `No deployments found`. Open Logical Models: the same banner appears and the registry says zero / no logical models.
3. In the same Developer session, the Console requests `GET /admin/v1/models?limit=100` (HTTP 200, one row) and `GET /admin/v1/deployments?env_id=…&project_id=…&limit=100` (HTTP 200, two rows). The accompanying `/admin/v1/organizations?limit=100` and `/admin/v1/connections?limit=100` requests return 403 `insufficient_role_permission`.
4. At `/routes`, the UI reports the same permission banner and `No routes in this environment`. Its `GET /admin/v1/routes?env_id=…&limit=100` returns HTTP 200 with one row; the environment-scoped deployments request returns HTTP 200 with two rows.
5. Open Route Explain, enter `qa004-mock`, choose data residency `US`, and click Explain route. The real UI displays a successful dry-run matched to `qa004-mock`, with two deployment candidates rejected for `DATA_RESIDENCY_MISMATCH` and `CONNECTION_INACTIVE`. Return to Routes: the inventory still says zero.

The role assignment form, populated project scope, model and route empty states, API row counts, and real dry-run are documented in `evidence/CP12-developer-*`. The test used no real provider or upstream egress. A screenshot is visual evidence of the rendered page; API counts and the dry-run response are separately recorded so neither substitutes for the other.

### Independent second-project retest (CP18)

1. As the synthetic Org Owner, used the real workspace picker to create a sibling project, `QA-004 Isolated Canary`, then added `qa004-scope-canary@example.invalid` with only the Developer role and that project selected. Activated the one-time invitation through the public Activate invite form. Owner readback confirms the account is enrolled and bound only to `qa004-isolation-canary` (`evidence/CP18-owner-persistent-readback.json`).
2. Signed in as that project-only Developer in Chromium. The initial header shows `QA-004 Isolated Canary / No env`; after the Owner creates two environments, the Developer selects `qa004-human-label-check` through the real workspace picker. The scope picker lists only the canary and its environments, not the populated `default` project. `GET /admin/v1/projects` returns only `qa004-isolation-canary`; `GET /admin/v1/organizations` and `/admin/v1/connections` return 403 `insufficient_role_permission`.
3. In the selected environment, scoped overview/FinOps, usage summary, budgets, deployments, and routes return zero data. `GET /admin/v1/models` returns HTTP 200 with one logical model (`qa004-mock`). On the actual Models page, both the Deployments and Logical Models tabs show a red `insufficient role permission` banner; Logical Models says zero / “No logical models registered” despite the same role's model-list request returning one row. The actual Routes page shows the generic permission banner and zero routes while its authorized `GET /admin/v1/routes` returns HTTP 200 with zero rows. The no-route inventory itself is correct; the generic permission error is a misleading global page state.

This clean sibling-project case confirms the UI/API error mismatch around the allowed model catalog and strengthens the original DEF-054 finding. It also provides a separate positive isolation result: the Developer did not enumerate `default`, read organization/provider connections, or see its request/usage/budget/deployment/route data. The form, first-session overview, Deployments and Logical Models states, Routes, FinOps, Traces, and scope views are captured in `evidence/CP18-*` and visually inspected. No new defect ID is assigned for this repeat of DEF-054.


### Team Admin Routes & Policies reproduction (CP29)

A project-scoped `TEAM_ADMIN` in populated `default / dev` provides a second, role-specific reproduction of the same all-or-nothing catalog failure:

1. `GET /admin/v1/routes?env_id=<dev>&limit=100` returns HTTP 200 with the persisted `qa004-loopback` route. The same scope returns one model and two deployments.
2. The real Models page correctly renders one model and both deployment rows. Routes & Policies instead shows `insufficient role permission`, count `0`, and `No routes in this environment`; navigation and a hard reload reproduced it.
3. The route page's unscoped `GET /admin/v1/policies/effective` returns 403 for this Team Admin, while project- and environment-scoped effective-policy calls return 200. In the exact frozen RC6 source, `RoutingPage.tsx` awaits the route, deployment, model, connection, trace, and effective-policy requests in one `Promise.all` (lines 141–152); it commits the authorized list data only after the aggregate succeeds (lines 154–160). The denied organization-scope lookup therefore prevents route rows from reaching page state.
4. The visible Create route drawer has no deployment choices and cannot be saved, despite the role's documented route-management permission. The 403 itself is an intended organization-policy access boundary; losing the successful route data and rendering a misleading empty state is the defect.

Screenshots and sanitized status/body evidence are in `evidence/CP29-teamadmin-*`; all screenshots were visually inspected. The real Models screen is a useful same-account control: its permitted records render, while the adjacent Routes screen does not. This extends DEF-054 to Team Admin route management; it is not assigned a duplicate ID.

### Team-Admin-only retest (CP51)

A new synthetic account was created with only `TEAM_ADMIN` and bound to `qa004-scope-invite-project`, then activated through the real invite flow. This separates the defect from the earlier combined Developer + Team Admin principal. In the role-only session, Routes & Policies again renders the global `insufficient role permission` banner. The actual scoped Routes, Models, and Deployments reads each return HTTP 200 with zero rows in this intentionally empty project; the unscoped `/admin/v1/policies/effective` read returns 403, while project- and environment-scoped effective-policy reads return 200. Navigating through Route Explain and back preserves the banner. This corroborates the ancillary-policy error handling issue; because the project has no configured route/model/deployment, this run does not claim an additional hidden-row loss. See [CP51](../checkpoints/CP51-teamadmin-only-role-and-route-error.md), [sanitized results](../evidence/CP51-teamadmin-only-results.json), and the [visually inspected screenshot](../evidence/CP51-teamadmin-only-routes-error-banner.png). No duplicate defect ID was assigned.

## Expected behavior

The business entity contract grants Developers read-only model/MCP catalogs, dry-run Route Explain, Playground execution, and scoped trace inspection; SCR-08 explicitly grants Team Admin route create/edit/delete. For the authorized project/environment, model, deployment, and route inventories should render the records returned by their permitted list endpoints. A denied organization-policy, organization-switcher, or credential-connection request must not erase successful, authorized catalog results or leave a generic fatal error on otherwise usable screens. Restricted actions should remain denied and be described as such.

## Actual behavior

The Console conflates an ancillary 403 with the permitted model/route screen state. It displays empty inventory counts even though its model, deployment, and route requests succeeded with data. Route Explain proves that a configured route exists, but the adjacent inventory continues to claim none exists.

## Business impact

Developers cannot reliably inspect project configuration intended for their role. Team Admins also lose route inventory and the deployment choices needed by the route creation form. Users may misdiagnose a deployment or routing issue, escalate a false “not configured” state, or make decisions from empty Console data. A successful simulator result contradicting the adjacent inventory reduces confidence in both surfaces.

## Root-cause evidence

For the Team Admin Routes page, the failure mechanism is confirmed against the exact frozen RC6 source and a matching runtime request trace. `RoutingPage.refresh` awaits the successful route/deployment/model requests and effective-policy calls together in one `Promise.all`; the unscoped organization-policy call is denied for the Team Admin. The catch path sets the page error, and the list state assignments occur only after the aggregate succeeds. This explains the observed authorized API response paired with a zero-row UI and empty route-target selector.

The Developer Models/Routes instances show the same externally visible all-or-nothing failure pattern when ancillary calls are denied. Their exact failing member of the page's combined request set has not been isolated for every route/role/scope variant, so the identical trigger across all Developer cases remains a hypothesis. The root cause is not used to infer untested role or scope outcomes.

## Acceptance criteria

- A Developer or Team Admin scoped to a project can load model, deployment, and route rows returned by the corresponding authorized scoped APIs. A Team Admin can use the documented route-management screen to inspect configured targets and select available deployment targets.
- The list counts and visible rows reconcile with those responses on initial load, hard refresh, and navigation between Routes, Route Explain, and Models.
- A denied ancillary organization or connection request does not clear authorized catalog data or present a misleading empty state. Any genuinely unavailable selector is labeled locally and the rest of the page remains usable.
- Route Explain and the Routes inventory agree about the matched route. This does not grant the Developer route/policy mutation, credential access, budget mutation, member management, or approval permissions.
- Existing 403 boundaries for organization enumeration, provider connection data, member management, and writes remain intact.
- Regression coverage includes all six RBAC roles, organization/project/environment scopes, scope switching, refresh, and cross-screen consistency. For the Team Admin path, an organization-policy 403 must not suppress otherwise successful project/environment catalogs or leave the route editor without its authorized deployment choices.
