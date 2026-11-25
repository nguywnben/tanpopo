# CP50 — Activated Team Admin project boundary and member-list refresh

**Status:** PASS (scoped) for activated project-scoped Team Admin scope isolation, refresh persistence, and filtered member-list refresh. Historical DEF-006 and DEF-016 are independently PASS (scoped) on frozen RC6. **GATE-REL-07:** BLOCKED.
**Baseline:** frozen RC6 source `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; exact artifact run `45581179274`.
**Product source:** unchanged.

## Owner-created sibling project

1. In the isolated fresh runtime `127.0.0.1:18132`, signed in as the synthetic Org Owner created `QA-004 Cross-Scope Canary` (`qa004-cross-scope-canary`) through the actual workspace scope picker’s **New project** form.
2. The UI displayed the project name and changed the active workspace to the new project. The create request returned HTTP 201; authenticated project readback returned HTTP 200 with both the sibling and originally assigned project.
3. Screenshot captures the actual created-project Overview. The sibling has no environment and no configured data.

## Activated Team Admin effective scope

1. Signed in as `qa004-cp49-teamadmin@example.invalid`, activated through the real enrollment-token flow in CP50. The active workspace was `qa004-scope-invite-project / dev` and the principal displayed Developer and Team Admin. The member form retained its default Developer role when Team Admin was added, so this is a combined-role principal; Team-Admin-only behavior remains UNVERIFIED.
2. The real workspace scope picker listed only `qa004-scope-invite-project`; it did not list the sibling project or an organization-wide scope.
3. Authenticated `GET /admin/v1/projects?limit=100` returned HTTP 200 and exactly the assigned project. `GET /admin/v1/projects/{assigned}/environments` returned HTTP 200 and `dev`.
4. The corresponding read for the known same-organization sibling project returned HTTP 403 `insufficient_role_permission`. The project identity is known to the caller from the same-org Owner test; this is an authorization denial, not a foreign-tenant anti-oracle result.
5. `GET /admin/v1/organizations` and `GET /admin/v1/kill-switches` also returned HTTP 403, consistent with the project-only boundary.
6. Hard refresh settled to Overview with `qa004-scope-invite-project / dev` still active. The Overview loaded its counters and normal empty-state message.
7. Access & Keys → Members & Roles initially rendered the synthetic Team Admin. The Console’s actual request was `GET /admin/v1/members?project_id=<assigned-project>&limit=100` and returned HTTP 200 with that project membership. A direct unfiltered `GET /admin/v1/members?limit=100` returned HTTP 403 `insufficient_role_permission` and no members, confirming the UI honors the required project filter and the API rejects unscoped enumeration.

## Team Admin scoped write path

1. In the same Console, the Team Admin opened **Add member**. The form preselected the only authorized project, `qa004-scope-invite-project`; only the Developer role was selected for the synthetic member.
2. Submitted `qa004-cp50-project-dev@example.invalid`. The real `POST /admin/v1/members` returned HTTP 201. The invitation remained pending activation; its one-time enrollment token is excluded from repository evidence.
3. After a hard refresh, the filtered member list returned HTTP 200 with both the new Developer and the Team Admin, each carrying only the assigned project ID. The UI visibly retained the pending activation state for the new member.
4. Separately attempted a CSRF-protected `POST /admin/v1/members` as Team Admin with a new synthetic address bound only to the known sibling project. The server returned HTTP 403 `insufficient_role_permission`. A subsequent Org Owner readback returned HTTP 200 and confirmed the denied synthetic address was absent.

## Historical defect retests

- **DEF-006 — PASS (scoped).** CP49 established Owner invitation submission and persisted project binding. CP50 activated that membership, verified the resulting assigned project/environment scope, and denied sibling-project reads. The original form/persistence failure did not reproduce across this completed synthetic invitation path. Other role combinations, multi-project assignment, removal/revocation, expiration, and email delivery remain UNVERIFIED.
- **DEF-016 — PASS (scoped).** After a fresh Team Admin login, Members & Roles used the authorized `project_id` filter and returned the scoped member after refresh (HTTP 200). A deliberately unfiltered direct read was denied (HTTP 403), matching the permission boundary. After adding a second synthetic Developer, a subsequent refresh returned both assigned-project members. Additional scopes, pagination, and concurrent changes remain UNVERIFIED.

## Screenshots and sanitized evidence

- [Owner-created sibling project](../evidence/CP50-owner-created-sibling-project.png)
- [Activated Team Admin Overview](../evidence/CP50-teamadmin-overview-scoped.png)
- [Team Admin scope picker](../evidence/CP50-teamadmin-scope-picker.png)
- [Overview after refresh](../evidence/CP50-teamadmin-refresh.png)
- [Project-filtered Members & Roles](../evidence/CP50-teamadmin-members-scoped.png)
- [Team Admin Add member form with only its assigned project selected](../evidence/CP50-teamadmin-add-member-form.png)
- [New Developer membership after refresh](../evidence/CP50-teamadmin-members-after-create-refresh.png)
- [Sanitized API and scope results](../evidence/CP50-teamadmin-scope-results.json)

All screenshots were visually inspected. Synthetic accounts use `.invalid` addresses. Invitation tokens, session cookies, credentials, and master-key material are excluded.

## Coverage boundary

- **PASS (scoped):** project-scoped invitation activation; picker and project-list isolation; authorized environment read; same-org sibling project denial; organization and kill-switch list denial; refresh persistence; project-filtered member-list load and refresh; unfiltered member-list denial.
- **UNVERIFIED:** all Team Admin write APIs and resources not enumerated here; multiple project bindings; member role edit/revoke and invite redemption for the newly added Developer; pagination; rapid scope races; other browsers and all other roles; broader project data and cross-feature flows.
- No new product defect was assigned. No release acceptance or QA-004 completion is claimed.
