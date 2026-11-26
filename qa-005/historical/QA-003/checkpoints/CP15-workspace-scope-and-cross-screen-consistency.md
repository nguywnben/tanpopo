# CP15 — Workspace scope and cross-screen consistency

## Scope and result

Independent real-user checks on frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`, local isolated SQLite runtime `127.0.0.1:18103`, Chromium desktop at 1440×1000. Tanpopo source was not modified. `GATE-REL-07` remains **BLOCKED**.

## Scope switching, refresh, and FinOps

Signed in as synthetic Org Owner with `Default Organization / default / dev` active. Changed the scope selector to `No project / No env`, navigated to FinOps, and observed both organization and project budget rows with their respective IDs and limits. The unfiltered Admin API returned the same two persisted records (organization 20,000,000 micros; project 2,500,000 micros). The project row remains visible to this owner at organization scope, consistent with the unfiltered API; this scenario establishes behavior, not an unauthorized disclosure.

Without closing the scope popover, selected `default` and `dev`; the active scope updated to `Default Organization / default / dev`. FinOps showed the same two budget records, this time through the project-filtered `include_ancestors=true` request. After a page refresh, the project/environment scope remained selected and the same two budgets were rendered. Evidence and exact requests/readbacks: [scope round trip](../evidence/CP15-scope-roundtrip.json), [organization-only FinOps image](../evidence/CP15-finops-org-only.png), [project return](../evidence/CP15-finops-project-return.png), and [after refresh](../evidence/CP15-finops-after-refresh.png). Screenshots were visually inspected.

## Members and Virtual Keys across scopes

At organization-only scope, Access & Keys → Virtual Keys showed a clear “Select a project and environment” notice, an empty environment-key list, and disabled **Issue key** action. The separate **Members & Roles** tab remained available, rendered all six organization members, and showed their current activation state and role. After switching to `default / dev`, the UI showed the same six members; after refresh, the same list remained. Each screen requested `/admin/v1/members?limit=100` and returned the same six synthetic principals. Machine evidence is [member scope and readback](../evidence/CP15-members-scope.json), with visually inspected [organization-only](../evidence/CP15-members-org-only.png), [project/environment](../evidence/CP15-members-project-env.png), and [post-refresh](../evidence/CP15-members-after-refresh.png) screenshots. [Virtual Key organization-only screen evidence](../evidence/CP15-access-keys-org-only.json) and [screenshot](../evidence/CP15-access-keys-org-only.png) record the scope-specific key restriction and available member tab.

## Historical defect and coverage disposition

- **DEF-023 — VERIFIED FIXED for organization-only, project/environment, navigation, and refresh in the tested Org Owner journey.** Six members remained visible in both scopes and after refresh. This extends, but does not replace, CP03's prior evidence. Team Admin and other role boundaries remain unverified.
- **DEF-016 — UNVERIFIED.** The scenario used Org Owner, not Team Admin. It does not resolve the historical Team Admin list-refresh permission contract.
- No new defect confirmed. Project/environment-scoped Virtual Key creation is intentionally unavailable with no project/environment selected and the UI explains that requirement.
- FT-09, SCR-13 and workspace scope consistency remain **PARTIAL**: only an Org Owner and two scopes were exercised. The six-role matrix, concurrent scope changes, rapid navigation, delayed responses, organization switching, other browsers/locales and all other Console screens remain open.

## Harness notes and evidence hygiene

A transient screenshot captured the immediate post-login “Loading section…” placeholder. Five seconds later the Overview rendered, and bootstrap/project/organization/summary/alerts endpoints returned HTTP 200; no stuck loading defect is inferred. Two initial automation attempts toggled the still-open scope popover closed before selecting the project; the corrected journey selected project and environment sequentially while the popover remained open. These were harness sequencing errors, not product failures. Evidence includes only synthetic `.invalid` principals and test-tenant IDs; no credentials or production data are included.

