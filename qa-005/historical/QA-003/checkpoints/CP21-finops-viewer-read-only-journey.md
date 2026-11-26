# CP21 — FinOps Viewer read-only and export permissions

**Baseline:** RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`

**Status:** Organization-scoped FinOps Viewer dashboard, exports and selected denied actions were exercised in real Chromium on the isolated SQLite clone. QA-003 remains in progress; `GATE-REL-07` remains **BLOCKED**.

## Journey and observed behavior

1. Added three synthetic `FINOPS_VIEWER` principals under the Org Owner, activated them through the local invite flow, and used independent signed-in reviewer sessions. Enrollment tokens and passwords stayed in memory. Evidence: [invite form](../evidence/CP21-finops-viewer-invite-form.png) and [pending account](../evidence/CP21-finops-viewer-pending.png).
2. Opened FinOps → Budgets & Holds. The page displayed organization and project monthly ceilings as read-only values, `$0.000000` exact/estimated/active-hold totals, zero unmetered requests, and both Export CSV and Export JSON actions. The screen was visually inspected at [dashboard screenshot](../evidence/CP21-finops-viewer-overview.png).
3. Opened Chargeback. The UI rendered its explicit “No usage recorded” state. Used the real Export CSV and Export JSON buttons to download both files. CSV retained the six documented headers and zero data rows; JSON was `{}` (2 bytes). With no settlements in the tenant, no usage values were exported. Evidence: [Chargeback screenshot](../evidence/CP21-finops-viewer-chargeback.png), [after exports](../evidence/CP21-finops-viewer-exports.png), and [sanitized result](../evidence/CP21-finops-viewer-result.json). The empty JSON shape is recorded for later comparison; the available contract does not define an empty-export envelope, so this is not classified as a defect.
4. A same-origin budget update attempt using the session CSRF token returned HTTP 403 `insufficient_role_permission`; `GET /admin/v1/budgets` returned HTTP 200. `GET /admin/v1/members` and `GET /admin/v1/virtual-keys` returned HTTP 403, and no write controls were offered on the FinOps screen.
5. The left navigation included Access & Keys, but opening it returned `Virtual keys: insufficient role permission`; the pane then also displayed “No Virtual Keys in this environment.” The route exposed no key-issue control. This paired error and empty-state presentation may confuse a viewer into reading an authorization denial as an empty environment, although the 403 banner is visible. Captured in [Access & Keys state](../evidence/CP21-finops-viewer-access-restricted.png). This remains a low-priority UX observation pending review of intended navigation treatment, not a confirmed defect.
6. Security was absent from the sidebar; direct navigation to `/security/kill-switches` showed a red “Access restricted” message naming the FinOps Viewer role. See [restricted route screenshot](../evidence/CP21-finops-viewer-security-denied.png).
7. A separate manual `GET /admin/v1/usage/summary` without `group_by`, `from`, or `to` returned HTTP 400. SCR-12 documents these filters. This invalid query is **not** treated as a permission failure; the actual dashboard displayed its aggregate totals.

## Role-contract reconciliation

SCR-12 assigns FinOps viewers read/export access to reports and reserves budget configuration for Org Owners and Team Admins. The observed 200 reads, CSV/JSON downloads, absent budget-write controls, and 403 budget update align with that contract. Member/Virtual Key reads and Security access were denied. No new product defect is confirmed in this phase. The sidebar Access & Keys route remains a minor UX observation because it is visible despite both member/key reads being denied; no data or mutation was exposed.

## Coverage updates

Extends **PARTIAL** coverage for FT-09, FT-13, SCR-12, SCR-13, and WF-08, plus Chromium 156 desktop/Linux. This covers only one organization-scoped FinOps Viewer and an empty usage ledger. Project-scoped FinOps viewers, populated CSV/JSON by role, other permission combinations, pagination and date-filtered totals, and PostgreSQL/Valkey remain open. The manual missing-filter 400 response and empty JSON export shape remain recorded as limited/inconclusive observations. Historical defect totals remain unchanged: 17 scoped VERIFIED FIXED, 3 PARTIAL, 19 UNVERIFIED; DEF-040 and DEF-041 remain open. QA-003 is not complete.
