# CP22 — Operator tenant provisioning, scope, and Developer isolation

**Baseline:** RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`

**Status:** Selected instance Operator, Org Owner, and Developer journeys were exercised in real Chromium against the isolated SQLite clone at `127.0.0.1:18116`. QA-003 remains in progress; `GATE-REL-07` remains **BLOCKED**.

## Operator and tenant provisioning

1. Signed in through the bootstrap account, whose real UI footer showed `Operator, Org Owner`. Operations → Health rendered **Ready**, `sqlite_wal`, one standalone node, and healthy status. The initial direct deep-link capture was taken before the screen finished loading; the navigation-based retry rendered the complete dashboard and is the retained health evidence. See [health screenshot](../evidence/CP22-operator-operations-recheck.png).
2. Opened Operations → Organizations. The table listed the Default Organization and exposed **Create organization**. Visually inspected the initial [organization list](../evidence/CP22-operator-organizations-list.png).
3. Created synthetic `QA003 Operator CP22` (`qa003-operator-cp22`) through the Console. POST returned HTTP 201; the list/read endpoint returned HTTP 200; the row remained after UI Refresh. The visually inspected [create form](../evidence/CP22-operator-create-form.png) and [created row](../evidence/CP22-operator-org-created.png) show the result. The endpoint and refresh readback are in [sanitized result](../evidence/CP22-operator-org-result.json).
4. The new tenant initially had zero members. The Operator selected it, then used Access & Keys → Members & Roles to provision a synthetic Org Owner. The real invite/activation flow completed; a fresh browser session authenticated as `ORG_OWNER`, received the new organization ID in its session, and saw Overview plus the one saved member. Password and enrollment token stayed in memory. Evidence: [member form](../evidence/CP22-operator-owner-invite-form.png), [pending state](../evidence/CP22-operator-owner-pending.png), [new owner Overview](../evidence/CP22-operator-org-owner-login.png), and [activation/readback](../evidence/CP22-operator-owner-provisioning-result.json).
5. After the Operator switched to the new organization, the top scope selector identified the new organization and the Members screen showed its two provisioned synthetic members. A page refresh returned to the bootstrap session's Default Organization. The authenticated session is the source used on initial load in the inspected implementation; no explicit acceptance requirement says an Operator's last-selected organization must persist through reload. Record this behavior as an observation, not a defect. See [post-switch screenshot](../evidence/CP22-operator-switched-scope.png) and [switch/readback result](../evidence/CP22-operator-scope-switch-result.json).

## Developer cross-tenant boundary

Created and activated a synthetic Developer in `qa003-operator-cp22`, then used a separate signed-in Chromium context. The session showed only `DEVELOPER`; own-project listing returned HTTP 200. Requests to read projects and members with the Default Organization's explicit `X-Tanpopo-Org-ID` returned HTTP 404 `resource_not_found`. The Developer navigation omitted Operations. The visually inspected [Developer Overview](../evidence/CP22-developer-crossorg-result.png) and [sanitized API results](../evidence/CP22-crossorg-isolation-result.json) show the scope and outcomes. No cross-tenant row was returned.

## DEF-042 — organization member form exposes an instance-only role

**Severity: Low.** On the synthetic tenant, the organization-scoped Add member role picker exposed `Operator`. Selecting that option and submitting a synthetic email caused `POST /admin/v1/members` to return HTTP 403 `role_escalation_denied` with `OPERATOR role cannot be bound inside an organization member`. The member was not created; a scoped `X-Tanpopo-Org-ID` read remained at two members. The form stayed open and continued to show the unsupported role selected, alongside the server error. See [role menu](../evidence/CP22-owner-role-menu.png), [selected role](../evidence/CP22-operator-role-option-form.png), [rejected state](../evidence/CP22-operator-role-rejected.png), and [scoped result](../evidence/CP22-operator-role-ui-result.json).

**Expected:** An organization-member form should omit/disable instance-only `Operator` or explain that it must be provisioned through an instance-level flow before submission.

**Actual:** The form offers it as an assignable org member role, and only rejects it after submission. Backend authorization behaved correctly; [`rbac.go`](../../internal/admin/http/rbac.go#L926-L927) explicitly rejects instance Operator inside an organization member binding. This is a UI/business-contract mismatch, not an authorization bypass. The error is explicit but arrives after a doomed action.

**Impact:** Operators can waste time retrying a membership action that can never succeed and may assume the role is assignable inside the tenant. No privilege elevation or member creation occurred. See [standalone engineering handoff](../reports/DEF-042-operator-role-offered-in-org-member-form.md).

**Acceptance criteria:** (1) Scope role options to roles valid for an organization binding; (2) retain server-side rejection as defense in depth; (3) if the role must remain visible, label its instance-only scope before submission and provide the supported provisioning path; (4) verify the form and API agree for all six canonical roles.

## Coverage and limitations

Extends **PARTIAL** coverage for FT-09, FT-15, SCR-13, SCR-16, and Chromium 156 desktop/Linux. CP22 verifies selected Operator organization creation/scope/member provisioning and selected Developer cross-tenant denials, not the full Operator or Developer matrix. Organization deletion/crypto-shred by Operator, backups/upgrades, cross-org key/data-plane probes, role editing, rapid concurrent switches, persisted active-org preference, PostgreSQL/Valkey, and other browsers remain open. A role-picker test probe initially used an unscoped Operator API list that defaulted to the bootstrap session's Default Organization (16 members); this was corrected with explicit `X-Tanpopo-Org-ID`, which read two synthetic-tenant members and confirmed the rejected canary absent. This harness correction is not product evidence.

Historical defect totals remain unchanged: 17 scoped VERIFIED FIXED, 3 PARTIAL, and 19 UNVERIFIED. Existing DEF-040 and DEF-041 remain open; DEF-042 is a newly confirmed Low-severity UX/business-contract defect. No historical defect is closed by this checkpoint.
