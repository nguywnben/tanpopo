# DEF-054 — Developer Console hides allowed model and route data after unrelated permission errors

**Severity:** Moderate
**Status:** FAIL (confirmed current RC6)
**Area:** Developer RBAC, model/route catalogs, workspace scope, Console error handling

## Summary

A project-scoped `DEVELOPER` can read logical models, deployments, routes, and use Route Explain through the documented interfaces. In the real Console, Models & Deployments and Routes & Policies instead show an `insufficient role permission` banner and empty states. The same session's relevant list endpoints return records, and Route Explain identifies the route. This contradicts the visible catalogs and can mislead a Developer into believing the project has no configured models or routes.

## Reproduction

1. On the isolated RC6 clone, sign in as the synthetic Org Owner. In Access & Keys → Members, add a synthetic `.invalid` account with only the `Developer` role and the `default` project selected under Scoped projects. Activate its one-time invitation outside the screenshot evidence and sign in as that Developer.
2. At `/models`, wait for the page requests to complete. The UI displays `insufficient role permission`, `Environment deployments 0`, and `No deployments found`. Open Logical Models: the same banner appears and the registry says zero / no logical models.
3. In the same Developer session, the Console requests `GET /admin/v1/models?limit=100` (HTTP 200, one row) and `GET /admin/v1/deployments?env_id=…&project_id=…&limit=100` (HTTP 200, two rows). The accompanying `/admin/v1/organizations?limit=100` and `/admin/v1/connections?limit=100` requests return 403 `insufficient_role_permission`.
4. At `/routes`, the UI reports the same permission banner and `No routes in this environment`. Its `GET /admin/v1/routes?env_id=…&limit=100` returns HTTP 200 with one row; the environment-scoped deployments request returns HTTP 200 with two rows.
5. Open Route Explain, enter `qa004-mock`, choose data residency `US`, and click Explain route. The real UI displays a successful dry-run matched to `qa004-mock`, with two deployment candidates rejected for `DATA_RESIDENCY_MISMATCH` and `CONNECTION_INACTIVE`. Return to Routes: the inventory still says zero.

The role assignment form, populated project scope, model and route empty states, API row counts, and real dry-run are documented in `evidence/CP12-developer-*`. The test used no real provider or upstream egress. A screenshot is visual evidence of the rendered page; API counts and the dry-run response are separately recorded so neither substitutes for the other.

## Expected behavior

The business entity contract grants Developers read-only model/MCP catalogs, dry-run Route Explain, Playground execution, and scoped trace inspection. For the authorized project/environment, model, deployment, and route inventories should render the records returned by their permitted list endpoints. A denied organization-switcher or credential-connection request must not erase successful, authorized catalog results or leave a generic fatal error on otherwise usable screens. Restricted actions should remain denied and be described as such.

## Actual behavior

The Console conflates an ancillary 403 with the permitted model/route screen state. It displays empty inventory counts even though its model, deployment, and route requests succeeded with data. Route Explain proves that a configured route exists, but the adjacent inventory continues to claim none exists.

## Business impact

Developers cannot reliably inspect the project configuration that their role is intended to use. They may misdiagnose a deployment or routing issue, escalate a false “not configured” state, or make decisions from stale/empty Console data. A successful simulator result contradicting the adjacent inventory reduces confidence in both surfaces.

## Root-cause hypothesis (not established)

The pages likely treat a rejected organization or provider-connection lookup as a fatal refresh error and clear or suppress catalog results that were successfully returned. The network sequence and rendered state support this hypothesis, but the exact state transition has not been traced and is not claimed as proven.

## Acceptance criteria

- A Developer scoped to a project can load model, deployment, and route rows returned by the corresponding authorized scoped APIs.
- The list counts and visible rows reconcile with those responses on initial load, hard refresh, and navigation between Routes, Route Explain, and Models.
- A denied ancillary organization or connection request does not clear authorized catalog data or present a misleading empty state. Any genuinely unavailable selector is labeled locally and the rest of the page remains usable.
- Route Explain and the Routes inventory agree about the matched route. This does not grant the Developer route/policy mutation, credential access, budget mutation, member management, or approval permissions.
- Existing 403 boundaries for organization enumeration, provider connection data, member management, and writes remain intact.
- Regression coverage includes all six RBAC roles, organization/project/environment scopes, scope switching, refresh, and cross-screen consistency.
