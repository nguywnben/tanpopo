# CP39 — Fresh organization owner, member visibility, and tenant boundary

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Result:** FAIL DEF-023 for Members & Roles no-project view; PASS for the scoped tenant-isolation negative check; additional routes UNVERIFIED.  
**Product source changes:** none. Synthetic records are confined to disposable QA runtime data.

## Actions and evidence

- In Chromium, used Operations to create and switch to synthetic organization `qa002-cp39-isolation` (ID `org_01m85fqs3ae009a5406gxzx57g`). UI screenshots were visually inspected.
- In that organization, added `qa002-cp39-org-owner@example.invalid` as a HUMAN_LOCAL principal with only ORG_OWNER role. The UI said “Member added”; the org-only owner login succeeded. Credentials were temporary QA data and are being removed after the test.
- Opened Access & Keys → Members & Roles with no project selected. `CP39-org-owner-members.png` was visually inspected: the heading says “Organization members”, count is zero, and “No members found” appears. The same authenticated principal's `GET /admin/v1/members` returned 200 with one persisted member.
- Created a synthetic project and environment in the same organization. After selecting them, `CP39-org-owner-members-with-project.png` was visually inspected: count one and the same owner row are shown.
- As the org-only owner, attempted to update a known old-tenant project budget by ID using a valid session/CSRF request. It returned 404 `resource_not_found`; a follow-up collection read and database state showed no cross-tenant mutation. This is a scoped negative result only.
- Two speculative project subresource paths returned 404/405 and were excluded as route-level probes, not treated as access-control evidence.

## Classification

- **FAIL — DEF-023:** UI/API state disagreement and misleading empty organization member list until project context exists. Full reproduction and acceptance criteria are in `../reports/DEF-023-org-members-hidden-until-project-selected.md`.
- **PASS (scoped):** direct cross-tenant budget update was denied with 404 and did not change the target. This does not establish complete tenant isolation.
- **UNVERIFIED:** other tenant resource routes, org owner role edit/revoke, other browsers, concurrent UI/API updates, and the intended project prerequisite contract beyond available screen copy.

## Cleanup

Temporary login credential removed; synthetic principal is suspended and its password hash cleared in the disposable database. Test org/project/environment remain as sanitized synthetic QA state in the local disposable DB for reproducibility; the runtime is not a shared production environment.

## Related files

- `../evidence/CP39-organization-created.png`
- `../evidence/CP39-org-owner-member-created.png`
- `../evidence/CP39-org-owner-members.png`
- `../evidence/CP39-org-owner-isolation.json`
- `../evidence/CP39-org2-project-environment.json`
- `../evidence/CP39-org-owner-members-with-project.png`
- `../reports/DEF-023-org-members-hidden-until-project-selected.md`

This checkpoint does not complete QA-002. GATE-REL-07 remains **BLOCKED**.
