# CP16 — Team Admin member creation and scoped refresh

## Scope and result

Independent retest on frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902` using isolated SQLite runtime `127.0.0.1:18103`, real Chromium sessions, and synthetic `.invalid` principals. No Tanpopo implementation changed.

## Real role journey

1. As Org Owner, opened Access & Keys → Members & Roles → Add member, assigned `Developer` plus `Team Admin`, and kept the `default` project scope. The form displayed both roles and the scoped project. `POST /admin/v1/members` returned HTTP 201 with that project assignment. The one-time enrollment token was used only in memory to activate the account in a separate browser context; its value is redacted from all committed evidence.
2. Signed in to a clean browser session as the activated project-scoped Team Admin. At `default / dev`, the actual UI listed the Team Admin and existing project members. The UI's member list request was `GET /admin/v1/members?project_id=<default-project>&limit=100` and returned HTTP 200.
3. Through the Team Admin UI, added `qa003-team-admin-created-016@example.invalid` as a Developer in the selected project. The POST returned HTTP 201 with `project_ids` containing only the authorized project. The Console showed **Member added**, count 4, and the new pending-activation row; there was no permission-error banner.
4. Opened a fresh Team Admin session and refreshed Access & Keys → Members & Roles. The project-scoped request returned HTTP 200 and the new member remained visible. A direct unfiltered `GET /admin/v1/members?limit=100` from the same Team Admin session returned HTTP 403 `insufficient_role_permission`; the project-filtered list returned HTTP 200 with the four authorized members. This is the intended authorization boundary and is not counted as a defect.

Evidence: [sanitized create/session results](../evidence/CP16-def016-team-admin-result.json), [refresh/readback](../evidence/CP16-def016-refresh-result.json), visually inspected [role selection form](../evidence/CP16-team-admin-form-selected.png), [initial Team Admin list](../evidence/CP16-team-admin-initial-list.png), and [post-refresh list](../evidence/CP16-team-admin-after-create.png). No screenshot contains the enrollment token or password.

## Historical defect disposition

- **DEF-006 — VERIFIED FIXED for project-scoped Team Admin creation.** The real Team Admin form submitted the authorized project ID and the server persisted the new Developer with exactly that project assignment.
- **DEF-016 — VERIFIED FIXED for one authorized project, create, and refresh.** The Console consistently used project-scoped member reads; successful creation appeared immediately and after a fresh-session refresh. A forbidden unfiltered request remains rejected by the server, confirming the boundary rather than a product failure. Multi-project assignments, other roles and pagination remain open.
- No new defect confirmed. No project outside the authorized scope was queried or mutated.

## Coverage update

FT-09 and SCR-13 remain **PARTIAL**: Org Owner and Team Admin journeys now cover their respective organization/project responsibilities, but Developer authorization beyond first login, Operator, Security Reviewer and FinOps Viewer permissions, broader project sets, role escalation, revocation and concurrency remain unverified. DEF-006 and DEF-016 acceptance is verified only for this scoped workflow; `GATE-REL-07` remains **BLOCKED** and QA-003 remains in progress.

