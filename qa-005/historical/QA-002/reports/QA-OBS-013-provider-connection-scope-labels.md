# QA-OBS-013 — Provider Connections shows organization-wide rows without owner-project context

- **Classification:** Unresolved UI/business-contract observation; not a confirmed defect or authorization finding
- **First evidence:** CP77

## Observation

An authenticated Org Owner opened Providers → Connections while `qa-002-a2a-project / dev` was selected. The page displayed five connections. Sanitized `GET /admin/v1/connections?limit=100` data showed all five records owned by a different project, `QA002 Audit mwto47wa`; none were owned by the selected project. Switching the workspace to `QA002 Audit mwto47wa / stage` left the same five rows visible. The table has no owner-project or shared-project column, although the API records include project ownership and `shared_project_ids`.

The UI uses the same organization-wide list endpoint without a `project_id` filter. Source inspection of the frozen baseline shows the endpoint intentionally supports organization-wide listing for an organization-scoped actor, and applies project visibility filtering to non-organization-scoped actors. Thus CP77 did **not** demonstrate unauthorized access or cross-tenant leakage. The owner can administer the organization-wide resource collection.

Visually reviewed evidence: `../evidence/CP77-providers-a2a-project.png` and `CP77-providers-audit-project.png`. Sanitized UI/API comparison is `../evidence/CP77-providers-scope-comparison.json`; the API row summary is `CP77-providers-scope-api.json`. No connection was created, probed, changed, disabled or deleted, and no provider endpoint was contacted.

## Uncertainty and follow-up

The shell presents a selected project/environment, and the New connection flow targets the selected project. The list itself is an owner-authorized organization-wide registry. No normative UI contract found in this test establishes whether the list should remain global or filter by selected project. If the global view is intended, label each record's owning/shared project and make action scope clear. If it is intended to be contextual, filter the collection by selected project while preserving existing authorization checks. Test both Org Owner and project-scoped Team Admin with resources in two projects before classifying as a product defect. Do not narrow Org Owner permissions or infer an authorization bug from the global list.
