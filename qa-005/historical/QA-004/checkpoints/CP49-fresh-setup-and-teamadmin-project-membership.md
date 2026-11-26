# CP49 — Fresh setup and Team Admin project-scoped membership

**Status:** PASS (scoped) for first-time setup completion, invitation creation, project scope submission, and persistence. Team Admin activation/login and complete permission boundary remain UNVERIFIED. Historical DEF-006 is PASS (scoped). **GATE-REL-07:** BLOCKED.
**Baseline:** frozen RC6 source `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; exact artifact run `45581179274`.
**Product source:** unchanged.

## Environment and fresh-install journey

- Used a separate fresh SQLite instance on `127.0.0.1:18132`, with offline mode and synthetic test-only master key. The same isolated runtime was used for CP48; no other evidence runtime was changed.
- Real Chromium opened first-time setup. Created a synthetic owner and organization `QA-004 Scope Invite Org`, first project `qa004-scope-invite-project`; left the optional provider connection skipped because no adapter was enabled.
- Reviewed and completed the setup wizard; the UI redirected to Overview with the expected owner and active org/project/dev scope. A separate Chromium login using the synthetic Owner credentials returned to Overview and the Access & Keys screen showed the same organization/project.
- The setup flow required acknowledging master-key custody. Only the ephemeral test instance key was used; no production key material was involved.

## DEF-006 — Team Admin project scope

1. In the real Console as Org Owner, open **Access & Keys → Members & Roles → Add member**.
2. Add synthetic address `qa004-cp49-teamadmin@example.invalid`, retain the default Developer role, select Team Admin, and leave the scoped project set to the visible project `qa004-scope-invite-project`.
3. Submit. The actual `POST /admin/v1/members` returned HTTP 201.
4. Hard-refresh the Console. The row remains **Pending activation**, showing Developer and Team Admin roles.
5. Read `/admin/v1/members` through the authenticated browser. It returned HTTP 200 and persistent membership data included roles `DEVELOPER` and `TEAM_ADMIN` and `project_ids` containing `prj_01m89zaw43e008zhc0q6yyeshk`.
6. Read `/admin/v1/projects` through the same authenticated browser. It returned HTTP 200 and mapped that exact ID to `qa004-scope-invite-project`, confirming the persisted binding matches the project chosen in the form.

The project scope control is visible with Team Admin selected, the form submits without a project-scope validation error, and API state retains the exact chosen project binding after refresh. This resolves the historical failure for this exact Owner → project-scoped Team Admin invitation path; it does not establish every role-combination or membership-edit path.

The form is multi-select and Developer was preselected. Selecting Team Admin added it without clearing Developer. This was recorded as observed UI behavior; no defect is inferred because the available contract permits role assignment and this test did not establish that the extra role is unintended. The pending invitation was not activated, so Team Admin login, effective access checks, cancellation/reissue, invite expiry, and email delivery remain UNVERIFIED. The one-time invitation token was not retained in evidence.

## Evidence

- [Setup first-connection state](../evidence/CP49-setup-first-connection.png)
- [Setup review](../evidence/CP49-setup-review.png)
- [Completed setup and Overview](../evidence/CP49-setup-completed.png)
- [Team Admin form with selected project visible](../evidence/CP49-teamadmin-scope-form.png)
- [Membership roles after hard refresh](../evidence/CP49-member-scope-persisted.png)
- [Sanitized API and persistence result](../evidence/CP49-member-scope-results.json)

All screenshots were opened and visually inspected. Synthetic accounts use the reserved `.invalid` domain. Passwords, bootstrap token, master key, session cookies, and one-time invite token are excluded.

## Coverage boundary

- **PASS (scoped):** fresh Owner setup; successful member invitation POST; project scope persisted after Console refresh and authenticated API readback; no unscoped grant observed.
- **UNVERIFIED:** activated Team Admin session; project-only behavior across routes/models/providers/keys; cross-project denial; multiple selected projects; Team Admin-only role combination; membership edit/revoke and audit; external mail transport; other browsers and roles.
- This checkpoint does not complete RBAC, onboarding, historical defect, or QA-004 acceptance.
