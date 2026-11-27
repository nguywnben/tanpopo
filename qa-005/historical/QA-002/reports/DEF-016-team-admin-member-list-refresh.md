# DEF-016 — Team Admin member creation succeeds but member list reload fails

**Severity:** Moderate  
**Status:** Confirmed on baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`; no product code changed.

## Summary

A project-scoped Team Admin can add a project-scoped Developer member. The mutation returns HTTP 201 and the member persists, but the Console then reloads the collection without a `project_id`. The server correctly denies that organization-wide listing with HTTP 403. The screen shows both “Member added.” and “Members: insufficient role permission,” while the table shows zero members. The Team Admin cannot verify, edit, or manage the member it just created through this screen.

## Reproduction

1. In the isolated QA runtime, an Org Owner used Access & Keys → Members & Roles → Add member to create `qa002-team-admin@example.invalid` with Developer + Team Admin and the current project selected in Scoped projects. The create request returned 201 and persisted only project-scoped role bindings.
2. The member endpoint does not provision local login credentials or send an invitation in this runtime. To exercise the role with a real session, QA assigned a random test password by updating only the isolated QA principal's Argon2id hash column using Tanpopo's Argon2 helper. The password and hash remain in `/tmp` and are excluded from evidence. This setup limitation is disclosed; role checks below use real login/session, not a mocked role.
3. Sign in as the Team Admin and open Access & Keys → Members & Roles. The UI displays “Members: insufficient role permission,” a count of zero, and “No members found.” Screenshot `../evidence/CP14-def006-team-admin-session.png`.
4. Click Add member. The form shows a scoped project selection defaulted to the current authorized project. Enter `qa002-team-admin-created@example.invalid` and keep Developer. The real browser request is HTTP 201 with `project_ids=["prj_01m84ymb7we009px5hb5pfwsns"]`.
5. After the successful mutation, `refresh()` again receives 403 for `GET /admin/v1/members` without a filter. The UI simultaneously displays “Member added.” and the permission error, with an empty table. Screenshot `../evidence/CP14-def006-team-admin-add-result.png`.
6. Under the same authenticated Team Admin session, `GET /admin/v1/members?limit=100` returned 403 `insufficient_role_permission`; `GET /admin/v1/members?project_id=prj_01m84ymb7we009px5hb5pfwsns&limit=100` returned 200 and included both project-scoped principals, including the just-created member. Sanitized outcomes: `../evidence/CP14-team-admin-membership.json`.
7. Negative role-boundary checks returned 403 `role_escalation_denied` for an Org Owner grant and 403 `insufficient_role_permission` for a different project. SQLite confirmed neither denied principal was created.

## Expected vs actual

- **Expected:** a Team Admin sees and refreshes only members in authorized project scope; after adding a member, the list reflects the persisted result without exposing other projects.
- **Actual:** create uses the selected project ID and succeeds, but both initial and post-create refresh use the unfiltered organization collection and are denied. The UI reports an empty list plus contradictory success/error notices.

## Affected workflows and business impact

Scoped teammate onboarding, role review, editing, and ongoing project access management for Team Admins. The server-side create itself succeeds and authorization boundaries held, but the primary UI cannot confirm the created state or manage that member afterward.

## Root-cause hypothesis (not confirmed)

`AccessPage` allows `TEAM_ADMIN` in `canReadMembers`, but its refresh calls `listMembers()` without project scope. `managementClient.listMembers()` issues `GET /admin/v1/members` without `project_id`. The Admin API requires a project-scoped collection read for Team Admin and returns 403 unless the request supplies a project they can manage. Evidence directly confirms those request/response outcomes; the code path is a hypothesis for the observed Console behavior.

## Acceptance criteria

- Project-scoped Team Admin member list requests carry an authorized project scope, or the UI offers an explicit authorized project filter.
- The list shows only members with role bindings in the authorized project set; no organization-wide or cross-project members leak.
- Successful add/edit refreshes show the saved member and scoped roles without contradictory error banners.
- Org Owner grants and out-of-scope project membership attempts remain denied and create no principal/role binding.
- Cover single and multiple authorized projects, empty list, post-create refresh, edit, pagination, and denied scope.

## Relationship to DEF-006

The original DEF-006 failure to submit project scope did not reproduce: the current Team Admin form exposed an authorized project selection, sent `project_ids`, and the create returned 201. That acceptance slice is **VERIFIED FIXED (scoped)**. The independent list/refresh failure is tracked separately as DEF-016 and prevents marking the overall Team Admin member-management workflow PASS.

GATE-REL-07 remains **BLOCKED**. QA-002 remains in progress.

## CP72 independent retest

On RC4 runtime `127.0.0.1:18084`, signed in as the synthetic project-scoped Team Admin, opened Access & Keys → Members & Roles, and submitted another scoped Developer member `qa002-cp71-def016@example.invalid` through the real Console. The request returned HTTP 201 with the expected single project binding. The subsequent unscoped list reload returned HTTP 403 `insufficient_role_permission`; the UI showed the green “Member added.” notice and red permission error simultaneously, with the member count at zero. As a separate Org Owner session, the same principal appeared in the persisted organization list with its scoped Developer binding. This independently reproduces DEF-016 and verifies the created state is durable. Screenshots `../evidence/CP71-def016-submit.png`, `../evidence/CP71-def016-after-create.png`, `../evidence/CP71-def016-owner-sees-created-member.png`; sanitized request results `../evidence/CP71-def016-create-result.json`.

The authorized project-filtered read is the control that separates intended authorization from the UI defect: the API returns only the three allowed project principals, while the Console asks for an unfiltered organization list and is denied. The Console exposes no member-delete action in the owner’s role editor, and the documented Admin API specifies create/list/role-update but no delete operation. The added principal is therefore retained as labelled synthetic QA data in this isolated runtime. It has Developer only, scoped to the QA project, no credentials were provisioned, and it was not used to sign in. This evidence-time test data is not product data and does not change implementation.
