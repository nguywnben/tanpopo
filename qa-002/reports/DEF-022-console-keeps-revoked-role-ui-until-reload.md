# DEF-022 — Console keeps revoked role UI until a reload

**Severity:** Moderate
**Status:** Confirmed on exact QA-002 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`; no authorization bypass observed.
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Summary

When an owner removes `SECURITY_REVIEWER` from a user with an active browser session, the server invalidates that session. The already-open Console does not react to the resulting `401 unauthenticated`; it continues to render the former reviewer identity and Security page, including a visible **Trigger kill switch** button and the empty approval state. A full page reload removes the privileged navigation and the session remains rejected.

## Reproduction

1. Sign in as `qa002-cp28-reviewer@example.invalid` in a separate Chromium browser context and open Security → Approval Queue. The reviewer shell and approval queue are visible (`../evidence/CP31-reviewer-before-downgrade.png`).
2. In another authenticated owner context, open Access & Keys → Members & Roles → Edit roles for that reviewer. Change the sole role from `Security Reviewer` to `Developer`, then save. The Console returns HTTP `200` and “Member roles updated.”; the member list now shows only Developer (`CP31-role-edit-before.png`, `CP31-role-edit-after-selection.png`, `CP31-role-downgraded.png`).
3. Return to the existing reviewer tab without reloading. `GET /admin/v1/auth/session` returns `401 unauthenticated`, but the rendered page still says Security Reviewer and continues to show Security tabs, **Trigger kill switch**, and “Approval queue clear.” (`CP31-reviewer-stale-session-security.png`).
4. Reloading removes the Security navigation and the session endpoint remains `401`; the sanitized state is in `CP31-role-downgrade.json`. No kill-switch action was submitted and no privilege bypass was observed.

## Expected versus actual

- **Expected:** when the session becomes invalid, clear the cached principal/role state and protected-page data, then redirect to sign-in or show an explicit expired-session screen. Do not represent failed unauthorized loads as a legitimate empty approval queue or leave restricted controls visible.
- **Actual:** the API rejects the session, but the live Console continues displaying the cached role and protected controls until a hard reload. Its empty queue does not distinguish “no pending approvals” from “session invalid.”

## Impact

A demoted reviewer can be misled into believing the account remains authorized and that approvals are clear. A visible kill-switch affordance invites a failed operation and delays recovery; the tested backend did reject the session check, and no unauthorized state change was observed. The defect is stale and misleading UI/session handling, not evidence of an authorization bypass.

## Facts and root-cause hypothesis

**Facts:** owner role update returned `200`; persisted member roles became only `DEVELOPER`, version 3; the already-authenticated browser's session check returned `401 unauthenticated`; the Security page screenshot still showed the previous role and controls; a full reload removed restricted navigation.

**Hypothesis:** the Console caches authentication roles and page data but does not centrally react to `401` responses from in-page requests. Security page loading may also render a default empty collection when its authenticated request is rejected. This is a hypothesis; browser behavior and captured API responses establish the defect.

## Acceptance criteria

- On any Admin API `401 unauthenticated`, invalidate the client session and role state, clear protected cached data, and route to a clear sign-in/session-expired state without requiring reload.
- Never convert unauthorized/failed list loads into a success-looking empty state.
- Remove inaccessible navigation and action controls as soon as the client learns the session is invalid.
- Retest role downgrade/removal in a second live session; verify visible state clears, follow-on protected requests remain rejected, and no prior role can trigger approvals or kill switches.

## Evidence

- `../evidence/CP31-reviewer-before-downgrade.png` — active reviewer Security screen.
- `../evidence/CP31-role-edit-before.png` and `CP31-role-edit-after-selection.png` — real role edit form.
- `../evidence/CP31-role-downgraded.png` — owner confirmation and persisted Developer-only role.
- `../evidence/CP31-reviewer-stale-session-security.png` — stale Security screen after session invalidation.
- `../evidence/CP31-role-downgrade.json` — sanitized role persistence, session `401`, and stale page text/nav observations.

QA-002 remains in progress; no release approval is implied.
