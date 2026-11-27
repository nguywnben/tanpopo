# CP67 — DEF-006 activation and DEF-016 Team Admin member-list boundary

**Source:** frozen unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; exact source binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9` (linux/amd64, `vcs.modified=false`).
**Disposition:** **PASS (scoped QA-005 retest)** for pure project-scoped Team Admin invitation/activation and the project-filtered member-list boundary. No broader sibling-project or tenant isolation claim.

## Reproduction

1. Created a fresh isolated CP67 SQLite clone of the CP64 synthetic tenant using SQLite's online backup API. Reset only the disposable clone's synthetic Owner password and started exact RC7 on `127.0.0.1:18161`.
2. In Chromium 156, signed in as Owner and opened Access & Keys → Members & Roles → Add member. The form defaulted to Developer. Removed Developer, selected only Team Admin, kept the explicitly displayed `QA005 CP64 Project (qa005-cp64-project)` project scope, and entered `qa005-pure-teamadmin-cp67@example.invalid`. The form screenshot was visually inspected.
3. Submitted via the Console. `POST /admin/v1/members` returned HTTP 201 and the response carried `roles=[TEAM_ADMIN]`, the selected `project_ids` and pending activation. The enrollment dialog was closed without capturing or retaining its one-time token. In a second isolated browser context, activated the synthetic invite through the actual public `/admin/v1/auth/enroll` endpoint with a synthetic strong password; HTTP 200 identified the same principal and only `TEAM_ADMIN`.
4. Signed in as that user. The real Access & Keys → Members & Roles page displayed the activated principal and the active project/environment scope. The browser's actual `/admin/v1/members` reads used `project_id=prj_01m8dbkxhne009a8wxaz7fwcb6&limit=100` and returned HTTP 200. Hard reload returned the same project-filtered request and UI row.
5. From the authenticated Team Admin page, explicitly queried `GET /admin/v1/members?limit=100` without `project_id`; it returned HTTP 403 `insufficient_role_permission`. The explicitly scoped `GET /admin/v1/members?project_id=<assigned project>&limit=100` returned HTTP 200. The response recorded the same scope filter. Screenshot after refresh was visually inspected.

## Classification and limits

DEF-006 passes for a pure Team Admin invitation, activation and project binding on exact RC7 SQLite. DEF-016 passes for successful filtered listing and hard-refresh behavior, and a denied unscoped-list attempt. QA-004 CP50 evidence remains historical and is not used to claim these RC7 results.

Only one project exists in this isolated tenant; sibling-project visibility, multiple project assignments, cross-organization access, pagination, concurrent changes, writes by Team Admin, other roles, alternate storage backends and browsers remain UNVERIFIED. The one-time enrollment token and activation password were used only in memory and omitted from all retained evidence. No provider calls, paid usage, source/spec changes or production credentials.

Evidence: [sanitized JSON](../evidence/CP67-def006-016-teamadmin-boundary.json) and four visually inspected [form](../evidence/screenshots/CP67-pure-teamadmin-invite-form.png), [Owner persisted row](../evidence/screenshots/CP67-owner-member-persisted.png), [Team Admin member list](../evidence/screenshots/CP67-teamadmin-members-scoped.png) and [post-refresh member list](../evidence/screenshots/CP67-teamadmin-members-after-refresh.png). `GATE-REL-07` remains BLOCKED.
