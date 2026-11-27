# CP80 — DEF-022 role-revocation stale-session retest

**Source:** exact unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite clone `127.0.0.1:18162`; Chromium 156; synthetic Owner and Security Reviewer.

**Disposition:** **FAIL (fresh QA-005 reproduction)** for the accepted live role-revocation UI behavior. The server invalidated the downgraded user's session, but an already-open Console continued to show restricted Security content and controls until reload. No privileged action was submitted and no authorization bypass is claimed.

## Reproduction and result

1. In the real Owner Console, created `qa005-cp80-reviewer3@example.invalid` as a HUMAN Security Reviewer and issued an enrollment token. Activated it in a separate Chromium context. The Reviewer opened **Security → Approval Queue**, which showed an empty queue and the visible **Trigger kill switch** action. Visually inspected screenshot: `evidence/screenshots/CP80-def022-reviewer-before-downgrade.png`.
2. Refreshed the Owner's Members & Roles list after activation, opened **Edit roles**, deselected Security Reviewer, selected Developer, and saved. The real `PATCH /admin/v1/members/{principal_id}/roles` returned HTTP 200. An authenticated Owner readback showed `roles=[DEVELOPER]`, version 6, and `enrollment_pending=false`. The role picker before/after selection was visually inspected in `evidence/screenshots/CP80-def022-edit-reviewer-roles.png` and `evidence/screenshots/CP80-def022-role-selection-after.png`.
3. Without refreshing the Reviewer's browser, `/admin/v1/auth/session` returned HTTP 401. The old browser then navigated to **Security → Kill Switches**. Its URL and content changed, but the Security navigation remained; the page still displayed the Reviewer shell and a visible **Trigger kill switch** action, alongside a success-looking **No active kill switches** empty state. This stale state is visible in the inspected `evidence/screenshots/CP80-def022-after-session-invalidated.png`.
4. Did not submit any kill-switch action. Reloading the Reviewer page redirected to `/login`, removed Security navigation, and rendered the sign-in form. Visually inspected `evidence/screenshots/CP80-def022-after-reload.png`.

Sanitized status and environment details are in [CP80 evidence JSON](../evidence/CP80-def022-role-revocation.json). Invite token and synthetic password were only held in memory and were not saved to evidence or Git.

## Expected versus actual

- **Expected:** once the client receives an Admin API 401, clear cached identity, roles, protected page data and actions, then show a clear expired-session or sign-in state without reload. Unauthorized list failures must not render as a legitimate empty result.
- **Actual:** the server correctly invalidated the downgraded session, but the open browser retained Security navigation, the stale Reviewer identity, the Trigger kill switch control, and an empty-state message after the 401 and a subsequent protected-page interaction. A reload was required to clear the stale UI.

## Impact and limits

This is a misleading and potentially hazardous UX/security presentation defect: a demoted user is shown sensitive controls and a success-looking empty state while their session is rejected. The backend session check returned 401; no protected mutation was attempted, so this test does not show an authorization bypass. One Owner-to-Developer downgrade was tested on SQLite/Chromium. Other role changes, actual action rejection UI, other protected pages, PostgreSQL, other roles, browsers and recovery flows remain UNVERIFIED. Historical QA-002 reproduction is independently reconfirmed; no product code was changed. `GATE-REL-07` remains BLOCKED.
