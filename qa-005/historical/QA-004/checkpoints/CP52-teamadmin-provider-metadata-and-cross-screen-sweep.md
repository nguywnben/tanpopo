# CP52 — Team Admin provider metadata and cross-screen sweep

**Status:** FAIL (scoped) for Provider secret metadata readback after refresh (DEF-066); PASS (scoped) for empty Models, FinOps, and Traces states and the project-only authorization boundary. **GATE-REL-07:** BLOCKED.
**Baseline:** frozen RC6 source `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; exact artifact run `45581179274`.
**Product source:** unchanged.

## Role and cross-screen context

Used the activated CP51 principal with exactly `TEAM_ADMIN`, bound only to `qa004-scope-invite-project / dev` in fresh offline runtime `127.0.0.1:18132`.

- **Models:** UI showed a normal zero-deployment empty state; authenticated model and deployment lists returned HTTP 200 with zero rows.
- **Providers:** Connections list returned HTTP 200 with zero rows; adapters list returned HTTP 200 with zero rows. An unfiltered secret-metadata request returned the expected HTTP 403 `insufficient_role_permission`; the screen displayed that error globally.
- **FinOps:** UI showed zero spend, holds, unmetered requests and budgets; usage summary and project-filtered budgets returned HTTP 200 with zero rows.
- **Traces:** UI showed no matching requests; the scoped trace list returned HTTP 200 with zero rows.

These unpopulated screens were real Chromium interactions. The project has no model/deployment, provider connection, spend, budget, or trace records.

## DEF-066 — persisted project secret hidden after refresh

1. On **Providers → Seal secret**, the pure Team Admin created a project-owned `API_KEY` metadata canary with a fake non-provider value. The actual `POST /admin/v1/secrets` returned HTTP 201. No screenshot was taken while the value was in the field, and the value is excluded from evidence.
2. Immediately after success, the Console displayed one row with safe name/type/fingerprint/credential ID metadata and no plaintext. The screenshot was visually inspected.
3. Hard-refreshed and reopened **Vault Secrets**. The Console showed `insufficient role permission`, `Write-only vault credentials 0`, and `No vault credentials sealed`.
4. In the same session, authorized `GET /admin/v1/secrets?project_id=<assigned-project>&limit=100` returned HTTP 200 with the persisted row and no `secret_value` field. Unfiltered `GET /admin/v1/secrets?limit=100` returned HTTP 403 `insufficient_role_permission`.
5. The role boundary is correct. The Console request omits the active project filter, and the screen’s `Promise.all` rejects the entire reload when that unscoped request is denied. This is DEF-066, not an authorization defect.

## Evidence

- [Team Admin Models](../evidence/CP52-teamadmin-models.png)
- [Team Admin Providers before secret creation](../evidence/CP52-teamadmin-providers-connections.png)
- [Safe secret form](../evidence/CP52-teamadmin-seal-secret-form.png)
- [Metadata visible immediately after creation](../evidence/CP52-teamadmin-secret-immediate.png)
- [Metadata absent after refresh](../evidence/CP52-teamadmin-secret-after-refresh.png)
- [FinOps](../evidence/CP52-teamadmin-finops.png) and [Traces](../evidence/CP52-teamadmin-traces.png)
- [Sanitized API and screen results](../evidence/CP52-teamadmin-screen-results.json)
- [Standalone DEF-066 report](../reports/DEF-066-teamadmin-provider-secret-metadata-hidden-after-refresh.md)

All screenshots were visually inspected. The evidence contains no secret values, session tokens, or passwords. A synthetic fake value remains encrypted only in the isolated test database; no provider connection or upstream request was made.

## Coverage boundary

- **PASS (scoped):** Team-Admin-only empty Models, FinOps, and Traces states reconcile with their HTTP 200 empty lists; project-filtered secret metadata API returns safe persisted metadata; organization-wide secret enumeration is denied.
- **FAIL (scoped):** Providers hides the persisted project secret after refresh and shows a global error because it requests secret metadata without the allowed project filter (DEF-066).
- **UNVERIFIED:** populated connection/adapter catalogs for this role; provider connection create/probe/bind; secret rotate/delete and owner cross-project visibility; other roles, projects, organizations, browsers, pagination, and concurrent mutation.
- QA-004 remains ongoing. This is not product acceptance; `GATE-REL-07` remains **BLOCKED**.
