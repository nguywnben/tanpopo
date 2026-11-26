# CP42 — DEF-023 populated members at organization-only and no-environment scopes

**Status:** PASS (scoped fresh QA-005 retest)  
**Source:** frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; compiled artifact identity in CP00  
**Runtime:** isolated exact-source SQLite Console/Admin API at `127.0.0.1:18145`; Chromium 1440×1000; synthetic QA Org Owner and synthetic members only.

## Reproduction and result

1. Sign in as the synthetic Org Owner and open Access & Keys → Members & Roles at `QA005 Org / Initial Project / dev`. The page renders three organization members, including one pending project-bound Developer/FinOps Viewer. A fresh member list call returns HTTP 200 with three records.
2. Open the real workspace picker, select **No project**. The picker shows `QA005 Org / No project / No env`; the Members & Roles table retains all three members, including the project-bound pending member. The page still issues the unscoped organization member read (`GET /admin/v1/members?limit=100`, HTTP 200, 3 records).
3. Start a fresh browser session, select **No env** while retaining `QA005 Org / Initial Project`. The same three member records remain visible and the same organization-level read returns HTTP 200 with three records.

Visually inspected screenshots: `evidence/screenshots/CP42-members-project-scope.png`, `CP42-members-org-only.png`, and `CP42-members-project-no-env.png`. Sanitized API/UI readbacks: `evidence/CP42-def023-org-only-members.json` and `evidence/CP42-def023-project-noenv.json`.

## Disposition and limits

This fresh run passes DEF-023 for one Org Owner and one populated organization. It specifically exercises organization-only state with a project-bound member, which the earlier CP27 empty-secondary-organization test could not establish. Sibling projects with differing membership, a project-only Team Admin, other roles, pagination, simultaneous edits, other browsers, and PostgreSQL remain UNVERIFIED. No membership was created or changed in CP42. `GATE-REL-07` remains BLOCKED.
