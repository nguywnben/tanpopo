# CP88 — Team Admin multi-scope and member visibility

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated exact-source SQLite service on `127.0.0.1:18162` through ephemeral loopback HTTPS `127.0.0.1:19443`; Chromium 156. Synthetic pure `TEAM_ADMIN`, bound to one project in the primary synthetic organization. No provider traffic, paid usage, protected data, production credentials, or product writes.

## Result

**PASS (scoped tenant/RBAC boundary); PRODUCT_GAP observation PG-011.** The Team Admin could enumerate only its assigned project and its members. Cross-organization project enumeration returned generic `404 resource_not_found`; organization enumeration and sibling-project/unscoped member enumeration returned `403 insufficient_role_permission`. The real Console showed seven members for the bound project and did not expose a sibling project in the scope menu.

## Reproduction and evidence

1. Sign in as `qa005-teamadmin-def056@example.invalid`, a pure `TEAM_ADMIN` assigned to `QA005 No Budget Project` in the primary synthetic organization.
2. Read `/admin/v1/projects?limit=100`: HTTP 200, exactly the assigned project. Repeat with the primary organization header: same result. Substitute a synthetic secondary organization ID: HTTP 404 `resource_not_found`.
3. Read `/admin/v1/organizations?limit=100`: HTTP 403. Read `/admin/v1/members?project_id=<assigned-project>&limit=100`: HTTP 200, seven rows. Read the same collection with the sibling project ID and with no project filter: HTTP 403 `insufficient_role_permission`.
4. In the actual Console, inspect Access & Keys → Members & Roles. The page showed the seven assigned-project rows. The workspace menu exposed only the bound project, `No project`, the assigned environment, and `No env`; it did not expose the sibling project or secondary organization.
5. Select `No project / No env`. The screen retained the previously loaded seven-row list and its heading remained “Organization members.” No new `/admin/v1/members` request was observed during this selector transition. Restore the assigned project/environment; the same seven rows remained visible.

Visually inspected screenshots: [bound project list](../evidence/screenshots/CP88-teamadmin-members-bound-project.png), [scope menu](../evidence/screenshots/CP88-teamadmin-scope-menu.png), [no-project selection](../evidence/screenshots/CP88-teamadmin-no-project-members.png), and [restored scope](../evidence/screenshots/CP88-teamadmin-bound-restored.png). Sanitized route counts, statuses, role, and reproduction metadata: [CP88 JSON](../evidence/CP88-teamadmin-isolation.json).

## Interpretation and limits

The authorization boundary passed for these collection-read cases. SCR-13 describes Team Admin members as project-scoped, but does not specify whether changing the global workspace picker should refetch this role's fixed project-bound membership list or how that list should be labeled with no active workspace project. The stale prior list plus “Organization members” heading is therefore retained as **PG-011**, a contract/UX observation, not a new defect. It must not be interpreted as evidence of a cross-tenant or cross-project disclosure.

This does not cover all nested item-ID routes, writes, pagination, multiple assignments, other role combinations, backend engines, or browser engines. No source/specification was modified. `GATE-REL-07` remains BLOCKED.
