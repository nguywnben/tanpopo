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
