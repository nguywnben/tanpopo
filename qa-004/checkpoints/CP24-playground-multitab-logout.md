# CP24 — Playground session after cross-tab logout

**Historical finding:** DEF-047, Low. **QA-004 outcome:** PASS for one synthetic Chromium desktop flow. Other browsers, race timing, expired/rotated sessions, and alternate logout routes remain UNVERIFIED. GATE-REL-07 remains BLOCKED.

## Reproduction

In the isolated default project/environment, the Org Owner opened Playground with the local synthetic route `qa004-loopback`. A $0.001000 hard session budget cap (1,000 micro-USD) was the minimum accepted by the form; a 100-micro-USD trial displayed the expected inline range validation and did not enable Send. With the valid cap, a real prompt returned `Hello from synthetic Ollama.` and a persisted trace reported HTTP 200, zero charge, and 76 ms. This provided an active Playground session without live-provider traffic or material spend. Screenshots `CP24-playground-prompt.png` and `CP24-playground-session-active.png` were visually inspected.

With the active session page open, a second tab signed out through the actual profile **Sign out** control. The second tab navigated to the sign-in screen. The first tab was also invalidated: its visible state became the sign-in view with “Session ended — Your session expired or your permissions changed. Sign in again to continue.” Its Send action was no longer available. A direct `GET /admin/v1/auth/session` from that first tab returned HTTP 401 and `authenticated: false`. Sanitized browser interaction evidence is `evidence/CP24-multitab-logout.json`; screenshots of the active session and the second tab after logout are `evidence/CP24-multitab-before-logout.png` and `evidence/CP24-other-tab-after-logout.png`. Both screenshots were visually inspected.

## Disposition

DEF-047 does not reproduce in this tested RC6 flow. Logout in another tab expired the administrative session, cleared the first tab's active Playground presentation, and removed its ability to send another request. This is scoped to Chromium at 1440×1000 and one Org Owner / local Ollama route; it does not prove behavior under simultaneous requests, other roles, all browser engines, or every logout/session-expiry path. No product code was changed.
