# CP66 — DEF-006 project-scoped member provisioning

**Source:** frozen unsigned RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`; runtime binary identity is the CP64/65 exact-source artifact (`/tmp/qa005-bin/tanpopo`, Linux/amd64, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`).
**Disposition:** **PASS (scoped QA-005 retest)** for creating an invited member with Team Admin and an explicit project binding. The test did not activate the invite or verify effective access boundaries. The form also retained its default Developer role, so this was a combined Developer + Team Admin principal, not a pure Team Admin acceptance test (consistent with existing PG-001).

## Reproduction

1. Cloned the CP64 fresh-install SQLite database into an isolated CP66 test directory with SQLite's online backup API. Reset only the disposable clone's synthetic Owner password using the exact-source local recovery command. Started exact RC7 on `127.0.0.1:18160`; no provider credentials/calls.
2. In real Chromium 156, signed in as the synthetic Owner and opened Access & Keys → Members & Roles. The organization member list showed only the Owner. Clicked **Add member**.
3. Entered `qa005-team-admin-cp66@example.invalid`, selected Team Admin, and kept the existing default Developer selection. The form showed the scoped project `QA005 CP64 Project (qa005-cp64-project)`. The role list permits multiple simultaneous choices. Visually inspected the form screenshot.
4. Clicked **Add member**. The Admin API returned HTTP 201; the Console showed “Member added”, a pending-activation row and an enrollment-invite dialog. The dialog contained a one-time token, so that screenshot was deleted and the token was not retained in QA evidence. The synthetic invite token, its recovery password, and test DB remain under `/tmp` only.
5. Closed the invite dialog, hard-reloaded, and confirmed the member row persisted with Developer and Team Admin roles. Authenticated `GET /admin/v1/members?limit=100` returned the synthetic principal with `roles=[DEVELOPER, TEAM_ADMIN]` and `project_ids=[prj_01m8dbkxhne009a8wxaz7fwcb6]`, matching the selected QA005 CP64 Project. The organization audit API returned matching `member.created` and `member.enrollment_issued` events; `member.created.target_id` matched this principal and its metadata recorded the same `project_ids` and roles.

## Classification and limits

This fresh RC7 run directly verifies that a project binding can be selected in the real member form, submitted, persisted after hard reload, read back from the authenticated API, and recorded in the organization audit partition. DEF-006's scoped creation criterion passes; historical QA-001/QA-002/QA-004 findings remain preserved. Invite activation, pure Team Admin effective permissions, sibling-project denial, cross-organization boundaries, update/removal/reinvite, other roles and alternate backends remain UNVERIFIED. The default Developer co-selection is covered separately by **PG-001**, not called a defect by this checkpoint.

Evidence: [sanitized evidence JSON](../evidence/CP66-def006-project-scoped-member.json), [form](../evidence/screenshots/CP66-team-admin-project-scope-form.png), [persisted member list](../evidence/screenshots/CP66-team-admin-persisted.png), and [Owner scope overview](../evidence/screenshots/CP66-owner-overview.png). All committed screenshots were visually inspected; no invitation token is included. No product/specification files changed, no provider was called, and no paid usage occurred. `GATE-REL-07` remains BLOCKED.
