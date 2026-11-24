# DEF-023 — Organization members are hidden until a project is selected

**Status:** CONFIRMED on QA-002 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Severity:** Moderate  
**Affected workflow:** Fresh organization provisioning, org-owner member administration, role assignment

## Reproduction

1. In the real Operations UI, create a synthetic organization and switch into it.
2. As an `ORG_OWNER` with no `OPERATOR` role, add one HUMAN_LOCAL member with only the `ORG_OWNER` role. The Console confirms “Member added”; persistence and the authenticated session verify that the organization has one member.
3. In that account, open Access & Keys → Members & Roles with no project selected. The page labels the section “Organization members”, displays count `0`, and says “No members found”. The visible blue notice only says a project and environment are needed to manage Virtual Keys; it does not state a prerequisite for organization member administration.
4. Query `GET /admin/v1/members` in the same org-owner session: status 200, exactly one member, the newly added owner. The persisted database row belongs to the test organization and has the expected HUMAN_LOCAL principal and ORG_OWNER binding.
5. Create a project and environment through the UI/API, select them, then reload Members & Roles. The same screen shows count `1` and the owner row.

## Evidence

- `../evidence/CP39-org-owner-member-created.png` — inspected confirmation after the real UI add-member action.
- `../evidence/CP39-org-owner-members.png` — inspected empty state at no-project context, despite one persisted org member.
- `../evidence/CP39-org-owner-empty-members-api.json` and `../evidence/CP39-org-owner-isolation.json` — sanitized authenticated API results; org-owner role only, collection count one, old-tenant budget write denied with 404, and no mutation.
- `../evidence/CP39-org-owner-members-with-project.png` — inspected same member visible after project/environment selection.
- `../evidence/CP39-org2-project-environment.json` — sanitized IDs and successful synthetic project/environment creation.

## Expected vs actual

- **Expected:** An org owner can see the organization-wide member collection and manage organization-wide roles without first creating/selecting a project. If project context is a genuine prerequisite, the screen explains it and does not represent the member collection as empty.
- **Actual:** With no project selected, the UI silently presents an empty organization member collection while the same principal's authorized API returns the persisted member. Selecting any project/environment makes the member appear.

## Business impact

A newly provisioned tenant owner can add an organization member but cannot see or edit the member list until a project and environment are created and selected. This misrepresents persisted tenant state and makes the first-run organization administration flow depend on unrelated product provisioning. A user may add duplicate principals or conclude that the operation failed. No cross-tenant access or authorization bypass was observed.

## Root-cause hypothesis (not established)

The Members & Roles page appears coupled to the selected-project context or shares a query gated by project selection, even though the displayed collection is organization-scoped. This is inferred from the paired UI states and API result; implementation cause has not been investigated as this campaign does not change product code.

## Acceptance criteria

1. An org-only owner in a fresh organization sees the exact organization member collection without a project/environment selected.
2. Create, reload, role edit, and revoke operations remain consistent between API and UI in no-project and selected-project contexts.
3. Empty state is shown only when the persisted collection is actually empty; failed/blocked loads are rendered as an explicit error, not an empty list.
4. The page's scope and any selection prerequisites are explicit and accurate.
5. An org-only owner cannot read or mutate another tenant's members, projects, environments, keys, or budgets; appropriate not-found/forbidden behavior is preserved.

## Limits

The paired screenshots/API and disposable SQLite state confirm the no-project discrepancy for this org-only owner. Additional browser, locale, other-role, and concurrent-mutation coverage is open. Cross-tenant project/environment routes were not classified because the exploratory paths returned route-level 404/405; the scoped old-tenant budget update did return 404 and left state unchanged. No implementation diagnosis is asserted.


## CP43 independent expansion

Using the same isolated organization owner, CP43 reproduced the false empty organization member list both with no project/environment and with a project selected but no environment. The same-session organization members API continued to return the persisted owner row. Selecting an active project/environment showed the row. See [CP43 checkpoint](../checkpoints/CP43-console-workspace-scope-and-finops.md) and visually reviewed screenshots `../evidence/CP43-org-only-access-members.png` and `../evidence/CP43-project-no-env-members.png`.

## CP71 independent role and scope retest

On the enabled RC4 test runtime, an Org Owner account with three persisted organization members was tested across organization-only, project/no-environment, and project/environment scopes in real Chromium. The same-session `GET /admin/v1/members?limit=100` returned HTTP 200 with three principals. Members & Roles displayed count 0 / “No members found” in both organization-only and project/no-environment scopes, then displayed all three records when `stage` was selected. The selected project and environment were visible in each inspected screenshot. No member record was changed. Evidence: `../evidence/CP71-members-organization-only.png`, `../evidence/CP71-members-project-no-env.png`, `../evidence/CP71-members-project-env.png`, and sanitized `../evidence/CP71-members-scope-comparison.json`.

The same surface was checked in a separately authenticated project-scoped Team Admin session. The scope chooser exposed only the assigned project and environment (no organization choices). The member endpoint returned HTTP 403 `insufficient_role_permission`; the Console displayed that error. It also displayed a zero count and “No members found” below the error. This is not evidence of an authorization bypass: the read remained denied, and the role’s narrower scope chooser matches its project assignment. Preserve this as an adjacent UX observation; classify the permission denial as PASS for this tested boundary. Evidence: `../evidence/CP71-team-admin-members.png` and `../evidence/CP71-team-admin-scope-options.png`.

These results independently reconfirm DEF-023 under a second synthetic organization/runtime dataset. Navigation into Access & Keys was exercised; refresh/default-scope, other Console screens, cross-project member visibility for this role, and broader browser/role coverage remain open. No new defect ID was assigned.


## CP100 fresh scope, refresh, and navigation retest

In a new real Chromium session on runtime 18084, the Org Owner selected organization-only, project-only (`QA002 CP78 Fresh Budget / No env`), and project/environment (`QA002 Audit mwto47wa / stage`) contexts. In each state, the same-session `GET /admin/v1/members?limit=100` returned HTTP 200 with four rows. The UI showed 0 / “No members found” in organization-only and project/no-environment states, but showed the same four rows when `stage` was selected. The same false-empty state persisted after navigation away/back and after hard refresh to the runtime-default no-environment scope. Reselecting `stage` restored the four UI rows. No member or role was changed.

CP100 also compared Providers, MCP Hub, Security → Approval Queue, Operations → Health & Cluster, and Playground in those three contexts. The owner-visible provider/MCP/security/operations inventories were stable across the selected scopes; Playground explicitly requested an active environment and exposed a Send button with `aria-disabled=true` without one. No route request or inference occurred in the no-environment state. These are narrow owner-scope observations; other roles and the remaining Console surfaces are not closed by this pass.

A separate WebKit 375 × 844 session repeated the same three scopes. The owner UI counts were 0/0/4 while `GET /admin/v1/members?limit=100` returned 200/count 4 in all three. `document.documentElement.scrollWidth` remained 375. The WebKit run used the QA-only HTTPS forwarder because the active test server exposes HTTP and this browser did not retain its secure session cookie on that origin; no product change or data mutation was involved. See `../evidence/CP100-webkit-scope-members.json` and the visually inspected [org-only screenshot](../evidence/CP100-webkit-org-members-mobile.png), [project-only screenshot](../evidence/CP100-webkit-proj-members-mobile.png), and [environment screenshot](../evidence/CP100-webkit-env-members-mobile.png).

After hard refresh, the workspace returned to the runtime-default `QA002 CP78 Fresh Budget / No env` even when the prior selection was `stage`. This is a recurrence of QA-OBS-009, whose persistence contract is unresolved; no new defect is assigned from the reset alone. Evidence: [CP100 scope/API matrix](../evidence/CP100-workspace-scope-sweep.json), [member API/refresh/navigation comparison](../evidence/CP100-members-scope-api-refresh.json), and visually inspected screenshots `../evidence/CP100-org-access-members.png`, `../evidence/CP100-proj-access-members.png`, `../evidence/CP100-env-access-members.png`, and `../evidence/CP100-members-env-after-hard-refresh-default.png`; full screenshot set is linked from [CP100](../checkpoints/CP100-console-workspace-scope-cross-surface-regression.md).


## CP101 organization-switch retest

In a fresh authenticated owner session, the workspace selector explicitly switched to a second authorized organization, `QA002 CP18 Audit Status Org`. In organization-only context, Members & Roles displayed 0 / “No members found,” while the same-session member API returned HTTP 200 with four members and the project API returned three. This confirms DEF-023 is not limited to the primary `QA-002 A2A Org` dataset. No project/member mutation or unauthorized scope request was made. See [CP101](../checkpoints/CP101-organization-switch-membership-scope.md), sanitized [API/UI result](../evidence/CP101-org-switch-members.json), and visually inspected [screen](../evidence/CP101-audit-org-members.png).


## CP80 independent fresh-project/no-environment retest

In real Chromium, the Org Owner opened Members & Roles with project `QA002 CP78 Fresh Budget` selected and no environment. The screen displayed `Organization members 0` and `No members found`. An explicit same-session `GET /admin/v1/members?limit=100` returned HTTP 200 with four persisted principals. Switching through the Console scope selector to `QA002 Audit mwto47wa / stage` made the same page show those four rows. No records or roles were changed. This further confirms the project/no-environment case and the context-dependent false empty state; it does not establish the cause or alter the authorization boundary. Evidence: `../evidence/CP80-members-no-environment-empty.png`, `CP80-members-no-environment-api.json`, `CP80-members-project-environment-populated.png`, and `CP80-members-project-environment-api.json`; see [CP80](../checkpoints/CP80-members-scope-and-keyboard-accessibility.md).


## CP107 independent populated-organization retest

On 2026-11-24, a new disposable tenant was created through Operations. The Add member flow returned HTTP 201 for a HUMAN member with `DEVELOPER` and `ORG_OWNER` roles and displayed “Member added,” while Members & Roles remained at count 0 / “No members found” in organization-only scope. An authenticated `GET /admin/v1/members` explicitly scoped with `X-Tanpopo-Org-ID` to the new organization returned one persisted member. After using the real workspace selector to create/select a project and `stage` environment, the same page displayed that member and both role labels. The organization was crypto-shredded only after all coverage was complete. Screenshots: `../evidence/CP107-neworg-member-added.png` and `../evidence/CP107-member-visible-with-env.png`; API/role state: `../evidence/CP107-populated-org-before-shred.json`; see [CP107](../checkpoints/CP107-populated-org-crypto-shred-and-scope-consistency.md). No cross-tenant data or unauthorized access was observed. This is a further reproduction of DEF-023, not a new defect ID.
