# CP74 — DEF-009 WebKit mobile webhook drawer retest

- **Baseline:** frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`; no product implementation changed.
- **Result:** PASS for the 375×844 webhook drawer layout slice; no registration or delivery performed.
- **Runtime/browser:** isolated A2A-enabled runtime 18084, real WebKit, viewport 375×844, mobile/touch emulation. Authenticated as synthetic Operator + Org Owner through a temporary QA HTTPS proxy targeting only runtime 18084.

## Work performed

1. The first direct HTTP WebKit login did not retain the Secure session cookie. The existing TLS proxy at 18445 reached a different runtime and did not accept this test account. A temporary QA TLS proxy on 18446 was started for runtime 18084; the owner/operator login then succeeded over HTTPS. The proxy was test infrastructure only and not committed.
2. Opened the mobile navigation menu, selected Operations, and opened Webhooks.
3. Opened Register webhook subscription. The drawer displayed the receiver URL, signing-secret selector, event selector, Cancel, and Register webhook button. No form submission was attempted.
4. Visually inspected the screenshot. At 375 pixels, the drawer occupies the mobile viewport without horizontal document overflow; its footer controls remain visible at the bottom. Browser measurement returned `innerWidth=375` and `documentElement.scrollWidth=375`.

## Evidence

- `../evidence/CP74-webkit-mobile-home.png` — inspected responsive app shell after successful HTTPS login.
- `../evidence/CP74-webkit-mobile-nav-open.png` — inspected mobile navigation drawer.
- `../evidence/CP74-webkit-operations.png` — Operations loaded in mobile WebKit.
- `../evidence/CP74-webkit-webhooks-before.png` — inspected empty Webhooks state.
- `../evidence/CP74-webkit-webhook-drawer.png` — inspected registration drawer; footer buttons visible.
- `../evidence/CP74-webkit-webhook-drawer-metrics.json` — viewport/document widths and visible footer measurement.

## Disposition and limits

- DEF-009's offscreen drawer did not reproduce on this RC4 mobile WebKit pass, independently of CP07. The previous conflicting QA-001 evidence remains preserved; this is a scoped positive result, not a global clearance.
- The signing-secret selector had no usable credential exposed in this screen. No receiver was registered, no webhook secret was created, and no public delivery was attempted. CP53's delivery prerequisite remains BLOCKED pending usable Operator-controlled receiver access.
- Desktop, additional mobile widths, keyboard/screen-reader behavior, and actual webhook delivery/signature/retry remain UNVERIFIED.
- No new defect ID was assigned. `GATE-REL-07` remains **BLOCKED** and QA-002 remains in progress.
