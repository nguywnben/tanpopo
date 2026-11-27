# CP49 — Cross-tab logout and stale Playground session

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated service `18116`; loopback mock-backed stream route; synthetic Org Owner; Chromium 1440×1000; two tabs sharing one browser context  
**Disposition:** new finding **DEF-047 Low, confirmed**. Server-side revocation works; the first tab's Playground state remains stale and fails on the next send. `DEF-045` was not adjudicated by the invalid-key trace placeholder. `GATE-REL-07` remains **BLOCKED**.

## Journey and observed states

Tab A signed in, selected `default/dev`, chose `qa003-cp47-stream-route`, minted a new Playground key and received the deterministic loopback mock reply. Its screenshot before logout shows the active cap/expiry and End session control: [CP49 pre-logout](../evidence/CP49-multitab-before-logout.png).

Tab B shared the same browser context/session and used the visible Sign out control. It navigated to `/login`; recorded response metadata shows `POST /admin/v1/auth/logout` returned HTTP 204. Tab A did not navigate or clear local state. Its Playgound view continued to show the active cap, expiry, End session action, and previous conversation.

Sending another prompt from tab A produced HTTP 401 `invalid_virtual_key`. The same screenshot shows the stale active session controls, previous turn, new failed turn, red “Virtual Key is invalid, expired, or revoked.” alert, and “Trace not available yet.” The response and sanitized UI evidence are in [CP49 result JSON](../evidence/CP49-multitab-logout-result.json) and [first-tab failure screenshot](../evidence/CP49-first-tab-after-other-tab-logout.png). No token or prompt request headers were saved. The key ID is a synthetic fixture identifier; no secret is stored.

This independently confirms the server invalidates the key on sign-out (same boundary as CP48), while a second tab retains misleading UI state and allows a request that the server rejects. It is classified as new DEF-047 Low, a cross-tab UI/recovery defect, not an authorization bypass. The UI displayed “Trace not available yet” after the revoked-key 401. A fresh authenticated read in the same `default/dev` scope returned 200 for the trace list but did not find this ID; the detail endpoint returned 404. Because this was a pre-authorization failure with no trace found, it does not reproduce DEF-045’s persisted-trace case and is inconclusive for that defect. See [scoped trace readback](../evidence/CP49-invalid-key-trace-readback.json).

## Defect and coverage status

- New finding: [DEF-047](../reports/DEF-047-playground-session-stale-after-cross-tab-logout.md), confirmed Low. User-visible stale state and failed retry; backend key revocation and 401 authorization held.
- Historical defect counts unchanged: 17 VERIFIED FIXED for scoped scenarios, 11 PARTIAL, 11 UNVERIFIED. DEF-045 remains open; CP49’s 401 pre-authorization rejection did not reproduce the persisted-trace condition and does not change its status.
- `FT-01`, `FT-09`, `WF-06`, `SCR-15` remain PARTIAL. Other roles, scopes, browsers, races, session expiry and integrations remain unverified.
- Screenshots were visually inspected during the run. Product code untouched; no external provider traffic/spend.
