# CP51 — Team Admin-only role and Routes page permission handling

**Status:** PASS (scoped) for activation, assigned-project membership creation, refresh persistence, and sibling-project denial. FAIL (scoped) for the existing DEF-054 Routes page permission banner when an unscoped organization-policy request is denied. No new defect ID. **GATE-REL-07:** BLOCKED.
**Baseline:** frozen RC6 source `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; exact artifact run `45581179274`.
**Product source:** unchanged.

## Team-Admin-only membership lifecycle

1. In the real Owner Console on isolated runtime `127.0.0.1:18132`, opened **Access & Keys → Members & Roles → Add member**. The visible multi-select initially had Developer selected. Removed Developer and selected only Team Admin; the form showed only the assigned `qa004-scope-invite-project` under Scoped projects. The submit returned HTTP 201 and the Owner membership list retained the exact role/project binding.
2. Used the real member Actions menu to issue a fresh one-time invite token (HTTP 201), then opened **Activate invite** and submitted the token plus a synthetic password through the actual activation form (HTTP 200). The resulting authenticated session reports exactly `TEAM_ADMIN` and redirects to Overview. Token material was used in memory only and is excluded from the evidence package.
3. The real Team Admin scope picker and authenticated project list contain only `qa004-scope-invite-project`; the assigned `dev` environment reads HTTP 200. Reading the same-org sibling project's environments returns HTTP 403 `insufficient_role_permission`.
4. Used this Team-Admin-only account’s Console **Add member** form to add `qa004-cp51-teamadmin-only-added@example.invalid` with the default Developer role. The only scoped project offered was `qa004-scope-invite-project`. The UI showed “Member added”; after hard refresh, the filtered list returned both the Team Admin and new Developer, with the Developer pending activation and bound to the assigned project.
5. Direct unfiltered `/admin/v1/members?limit=100` returns HTTP 403; `?project_id=<assigned-project>&limit=100` returns HTTP 200 with four scoped principals. A CSRF-protected attempt to create another Developer bound only to the known sibling project returns HTTP 403 `insufficient_role_permission` with no created member ID.

This account is independent of CP50’s Developer + Team Admin account, so these observations specifically establish the Team Admin role’s scoped membership workflow. The token and password are omitted from captured artifacts.

## DEF-054 — Team Admin-only Routes screen support

1. In the same Team-Admin-only session, navigated to **Routes & Policies**, then navigated to Route Explain and back to Routes.
2. The real Routes UI displays a red `insufficient role permission` banner above its zero-route empty state.
3. Captured requests show the environment-scoped `GET /admin/v1/routes?env_id=<assigned-env>&limit=100`, `GET /admin/v1/models?limit=100`, and `GET /admin/v1/deployments?env_id=<assigned-env>&limit=100` all return HTTP 200 with zero rows in this isolated project. The unscoped `GET /admin/v1/policies/effective` returns HTTP 403 `insufficient_role_permission`, while project-scoped and environment-scoped effective-policy reads each return HTTP 200.
4. This fresh project has no route, model, or deployment rows, so this run does not establish further catalog data loss. It does reproduce the misleading global page error caused by a denied ancillary organization-policy read. Earlier populated Team Admin evidence in DEF-054 already demonstrated authorized route rows hidden from the Console; this is corroborating evidence for the same defect, not a duplicate finding.

## Evidence

- [Owner’s Team Admin-only assignment form](../evidence/CP51-owner-teamadmin-only-form.png)
- [Activated Team Admin-only Overview](../evidence/CP51-teamadmin-only-overview.png)
- [Team Admin-only Developer invitation form](../evidence/CP51-teamadmin-only-add-developer-form.png)
- [Team Admin-only membership list after refresh](../evidence/CP51-teamadmin-only-members-after-create.png)
- [Team Admin-only Routes page permission banner](../evidence/CP51-teamadmin-only-routes-error-banner.png)
- [Sanitized role, scope, and API results](../evidence/CP51-teamadmin-only-results.json)
- [DEF-054 full defect report](../reports/DEF-054-developer-catalogs-empty-with-allowed-data.md)

All screenshots were visually inspected. Synthetic accounts use `.invalid` addresses. One-time tokens, passwords, session cookies and master-key material are excluded.

## Coverage boundary

- **PASS (scoped):** Owner creates a Team-Admin-only project binding; invite-token issue and activation; exact session role; assigned project/environment read; same-project Developer invitation and persistent membership; unfiltered member enumeration denial; sibling project read/write denial.
- **FAIL (scoped):** DEF-054 Routes page displays a fatal-looking permission banner despite successful scoped empty inventory reads; an ancillary organization-policy 403 is the observed trigger.
- **UNVERIFIED:** nonempty catalogs for this pure-role account; route creation/edit/delete with configured targets; all other Team Admin write resources; multiple project bindings; role edits/revoke; invite expiry; pagination and races; other roles, browsers and environment types.
- This checkpoint does not complete role, catalog, historical defect, or product acceptance. QA-004 remains ongoing and `GATE-REL-07` remains **BLOCKED**.
