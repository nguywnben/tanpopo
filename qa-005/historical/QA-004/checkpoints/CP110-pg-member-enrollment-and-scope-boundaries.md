# CP110 — PostgreSQL member enrollment and scope visibility

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Results:** PASS (scoped) for DEF-021 local invitation activation; PASS (scoped) for DEF-023 Owner member visibility on PostgreSQL; expected Developer denial for organization member enumeration.

## Member creation, activation, and role boundary

Using real Chromium against the isolated PostgreSQL 16 runtime on `127.0.0.1:18133`, the Org Owner added `qa004-cp110-developer@example.invalid` as a project-bound Developer in `QA004 CP107 PostgreSQL Scope`. The authenticated `GET /admin/v1/members?limit=100` returned HTTP 200 with two records. The new principal had the Developer role and only the CP107 project ID. It initially appeared as **Pending activation**.

The owner opened the member action menu, selected **Issue invite token**, and followed the documented `/login#enroll=…` activation flow in a separate browser session. The Developer set a password and the application redirected into its authenticated Overview. A fresh Owner readback then showed `enrollment_pending: false` and `has_password: true`; the membership retained its project binding. The one-time invite token and password were not retained in QA evidence. The activation-form screenshot was omitted because the UI prefilled the one-time token; the authenticated Developer Overview and subsequent access-denial screen were retained instead.

As that active Developer, Access & Keys displayed the explicit message that Virtual Key metadata can be inspected but organization members cannot be listed. The corresponding `GET /admin/v1/members?limit=100` returned HTTP 403 `insufficient_role_permission`. This is the documented authorization boundary, not a defect.

This completes the previously unverified historical DEF-021 slice: the Console issues and consumes a local one-time activation invite for a synthetic HUMAN member. External email delivery, token expiry and replay, password recovery, and other enrollment paths remain UNVERIFIED.

## DEF-023 scope coverage on PostgreSQL

The Owner’s real **Access & Keys → Members & Roles** screen was exercised at organization-only (`No project / No env`), project-only (`QA004 CP107 PostgreSQL Scope / No env`), and environment (`QA004 CP107 PostgreSQL Scope / qa004-cp107-qa`) scope. In each scope the UI showed the same two organization members and the API returned HTTP 200 with the same two records. After a full browser refresh, both members remained visible and the API still returned two records. Overview-to-Access navigation was also exercised before activation. The project-bound Developer remained present in the organization roster at each selected workspace scope, as expected for an organization member with a project-scoped role.

This is a second-backend scoped PASS for historical DEF-023. Sibling projects with distinct memberships, pagination, other roles’ list visibility, concurrent membership updates, and rapid asynchronous scope switching remain UNVERIFIED.

## Evidence

- Visually inspected scope screens: [organization-only](../evidence/CP110-members-org-only-two-members.png), [project/no environment](../evidence/CP110-members-project-no-env-two-members.png), and [named environment](../evidence/CP110-members-environment-two-members.png).
- [Scope/API matrix](../evidence/CP110-access-scope-matrix.json), [post-refresh readback](../evidence/CP110-members-after-refresh.json), and [Owner’s post-activation membership readback](../evidence/CP110-owner-member-readback.json).
- Visually inspected [active member and assigned role](../evidence/CP110-owner-activated-member.png), [authenticated Developer denial](../evidence/CP110-developer-members-boundary.png), and [post-activation Developer Overview](../evidence/CP110-member-activation-result.png). The [activation result metadata](../evidence/CP110-member-activation-and-boundary.json) contains no token or password.
- [Sanitized member create/readback](../evidence/CP110-member-create-readback.json); the token-bearing screenshot was deleted and is not part of the QA branch.

No new product defect was identified. The synthetic member remains active in the isolated QA database for future role testing. No implementation code was changed. QA remains in progress; **GATE-REL-07 remains BLOCKED.**
