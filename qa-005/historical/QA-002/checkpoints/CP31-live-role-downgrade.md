# CP31 — Live session after role downgrade

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Runtime:** isolated SQLite Tanpopo at `127.0.0.1:18086`, two real Chromium browser contexts
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Result — FAIL DEF-022; server-side role invalidation PASS

- Logged in as a synthetic Security Reviewer in one browser and loaded Security → Approval Queue. In a separate owner browser, changed the member's only role to Developer through the real Access & Keys editor. UI returned 200 / “Member roles updated”; the member list showed Developer only at version 3.
- The existing reviewer session became invalid: `/admin/v1/auth/session` returned `401 unauthenticated`. However, before reload the same client continued to show “Security Reviewer”, Security navigation, a visible “Trigger kill switch” action, and “Approval queue clear.”
- After a full reload the restricted navigation disappeared; the session still returned 401. This confirms server-side revocation, while stale cached front-end state is DEF-022. No unauthorized mutation was attempted or observed.
- The screenshot of the stale state was visually inspected. The post-reload transition screenshot was discarded because capture occurred during the transient “Loading sign-in…” state; the reload/nav result remains recorded in sanitized JSON.

## Evidence

See `../reports/DEF-022-console-keeps-revoked-role-ui-until-reload.md` and `../evidence/CP31-role-downgrade.json` with the four referenced screenshots.

## Coverage update

Live role mutation invalidation passes for this owner→Security Reviewer downgrade. Immediate front-end session clearing, other role changes, other sensitive screens, and attempted actions after revocation remain UNVERIFIED. This checkpoint does not demonstrate an authorization bypass.
