# CP50 — WebKit cross-tab Playground logout

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Runtime:** isolated service `18116` through the local HTTPS test shim; WebKit; synthetic Org Owner; two tabs sharing one browser context; loopback mock-backed stream route
**Disposition:** DEF-047 independently reproduced in WebKit; remains Low. `GATE-REL-07` remains **BLOCKED**.

## Interaction and result

A real WebKit session signed in, selected `default/dev`, sent a prompt using `qa003-cp47-stream-route`, and received the deterministic synthetic response. The [before-logout screenshot](../evidence/CP50-webkit-multitab-before-logout.png) was visually inspected.

A second tab used the same browser context and signed out through the visible control. The recorded request was `POST /admin/v1/auth/logout` → HTTP 204; the second tab navigated to `/login`. The first tab continued to display the active session cap/expiry, **End session**, route and prior transcript. Sending a new prompt from that tab returned HTTP 401 `invalid_virtual_key`. The UI then displayed “Virtual Key is invalid, expired, or revoked.” while retaining active-session controls and conversation rows. No page exceptions were recorded. The [post-logout screenshot](../evidence/CP50-webkit-first-tab-after-logout.png) was visually inspected. Sanitized [result JSON](../evidence/CP50-webkit-multitab-result.json) contains status and rendered text only; no key material was saved.

## Disposition

This independently reproduces DEF-047 in a second browser engine after CP49's Chromium run. Server authorization held and rejected the revoked key. The defect remains a cross-tab stale UI/recovery problem, not an authorization bypass. This narrow WebKit case does not cover Firefox, private browsing, separate browser profiles, other roles/scopes, multiple simultaneous keys, or logout/send races. DEF-045 is not adjudicated by the “Trace not available yet” text from this invalid-key response: CP49's fresh scoped trace lookup did not find that trace.

No product source was modified and no external provider traffic or spend occurred.
