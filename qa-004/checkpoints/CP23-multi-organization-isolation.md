# CP23 — Multi-organization switching and isolation

**Outcome:** PASS for one synthetic Org Owner's cross-tenant read/write boundary and the bootstrap Operator+Org Owner's organization switch/create flow. Full multi-organization coverage remains PARTIAL. No defect confirmed. GATE-REL-07 remains BLOCKED.

## Tenant setup and real UI workflow

The isolated runtime's existing bootstrap identity is explicitly a combined **Operator, Org Owner**. Through Operations → Organizations, it created a second synthetic tenant, `QA-004 Cross Tenant Boundary` (`qa004-cross-tenant-boundary`). The real UI showed the separate organization name and slug, confirmed creation, and listed both tenants. The scope picker then displayed both organizations; selecting the new tenant produced an empty workspace with **No project / No env**. A synthetic local Org Owner was invited there, activated through the real enrollment flow, and signed in. Its session reported only `ORG_OWNER`, with the new organization as `org_id`. The Owner saw a valid zero-traffic Overview after the initial loading state settled and its organization/project list stayed empty after a new browser context loaded the saved authenticated state.

Visually inspected screenshots: `CP23-organizations-tab.png`, `CP23-org-create-form.png`, `CP23-org-created.png`, `CP23-org-switcher.png`, `CP23-orgb-owner-invite-form.png`, `CP23-orgb-overview.png` (transient loading state), and `CP23-orgb-overview-settled.png` / `CP23-orgb-isolation-overview.png` (settled state). Readbacks: `CP23-org-created.json`, `CP23-orgb-owner-activation.json`, and `CP23-orgA-post-spoof-readback.json`.

## Cross-tenant attempts by the Org B Owner

The Org B Owner's session listed exactly one organization (Org B) and zero projects. The following independently tested requests produced:

- `GET /admin/v1/projects?limit=100` with `X-Tanpopo-Org-ID: <Org A>` → **404 `resource_not_found`**.
- `GET /admin/v1/projects/<Org A canary project>/environments` → **404 `resource_not_found`**, `project not found`.
- CSRF-protected `POST /admin/v1/projects` with Org A in the request body → **403 `org_id_spoofing_forbidden`**; a subsequent Org A Owner readback confirmed the project was not created.
- CSRF-protected `POST /admin/v1/organizations` → **403 `insufficient_role_permission`** for this `ORG_OWNER`-only principal.

Sanitized results: `CP23-orgb-isolation.json`. Owner A's fresh post-attempt readback in `CP23-orgA-post-spoof-readback.json` confirms both organizations remain present, the original two Org A projects remain, and the spoofed slug is absent. No cross-tenant data was mutated.

## Interpretation and limitations

No defect is confirmed in these paths. Org B's empty inventory, org list, foreign-org header, foreign project identifier, body spoofing, and org-create permission all behaved consistently with tenant isolation and role boundaries. The bootstrap identity combines OPERATOR and ORG_OWNER, so the successful tenant-creation action does not independently isolate Operator privilege. One Org B-only Owner and one foreign Org A project were probed; provider/secret/key/trace/audit/budget enumerations, write paths for each resource, pagination, concurrent org switching, stale requests, a dedicated standalone OPERATOR, and cross-org membership remain UNVERIFIED. A first Overview screenshot captured `Loading section...` at 800 ms; after waiting for async loads it rendered empty metrics normally. This transient alone is not classified as a defect.
