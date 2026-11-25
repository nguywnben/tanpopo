# DEF-047 — Playground session remains visibly active after logout in another tab

**Severity:** Low  
**Status:** Confirmed on frozen RC5 in two-tab Chromium and WebKit sessions
**Baseline:** `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Affected workflow:** Playground session lifecycle across browser tabs; sign-out and immediate inference retry  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Reproduction

1. Sign in as the synthetic Org Owner in Chromium. Select `Default Organization / default / dev` and use the controlled loopback route `qa003-cp47-stream-route` in Playground.
2. Send a prompt and wait for the deterministic mock reply. The Playground displays an active session cap, expiry and **End session** action.
3. Open a second tab in the same browser context. Sign out there using the visible Sign out action. The second tab navigates to `/login`; its request log records `POST /admin/v1/auth/logout` → HTTP 204.
4. Return to the first tab. It still displays the active cap, expiry, **End session** action, old transcript and prior successful trace.
5. Send a new prompt from the first tab. The Data Plane returns HTTP 401 `invalid_virtual_key`; the UI shows “Virtual Key is invalid, expired, or revoked.” and still shows “Trace not available yet.”

The two-tab flow was executed in Chromium (CP49) and independently in WebKit (CP50). [Sanitized two-tab record](../evidence/CP49-multitab-logout-result.json) contains only status/error metadata, the synthetic key ID, visible text and no secret. Visually inspected [pre-logout](../evidence/CP49-multitab-before-logout.png) and [first tab after logout and failed Send](../evidence/CP49-first-tab-after-other-tab-logout.png) screenshots show the state transition. The [CP49 checkpoint](../checkpoints/CP49-cross-tab-playground-logout.md) and [WebKit retest](../checkpoints/CP50-webkit-cross-tab-playground-logout.md) contains the test sequence and scope limits.

## Expected versus actual

- **Expected:** after logout invalidates the user session and Playground key, other same-origin tabs should clear or mark the affected session as signed out/revoked before offering further actions. The interface should explain that reauthentication is required.
- **Actual:** the other tab presents its Playground key as active and allows a send action. The server correctly rejects the revoked credential with HTTP 401, after which the UI displays an error and a trace-unavailable message.

## Impact

This creates a misleading active-session state and an avoidable failed request after a normal multi-tab logout. The transcript and prior trace remain visible in the same local browser tab. Server authorization held: the revoked Virtual Key was rejected, so the test did not demonstrate a post-logout inference bypass or cross-user exposure. The impact is a stale-session UX/recovery issue, not a security-boundary failure.

## Facts and root-cause hypothesis

**Observed:** same-browser tab B sign-out returned 204 and navigated to login; tab A retained its active Playground UI; a new tab-A send returned 401 `invalid_virtual_key`; screenshot-visible error and trace placeholder followed.

**Hypothesis (not proven):** logout state is not broadcast to other open Console tabs, and Playground does not revalidate its ephemeral key/session state before presenting the active controls. No source inspection is used to assert a root cause.

## Acceptance criteria

1. Signing out in one tab makes other same-origin Console tabs promptly clear or mark admin and Playground sessions as signed out/revoked.
2. A revoked Playground key must not be presented as active or allow a new send attempt; show clear reauthentication guidance.
3. On a protected request returning 401, clear the invalid ephemeral session state and distinguish authentication failure from provider/model failure.
4. Verify two and more tabs, sign-out from each tab, simultaneous send/logout races, multiple active Playground keys, refresh/reopen, and separate browser profiles (which should remain independently authenticated only if intended).
5. Preserve server-side revocation and verify all affected keys become unusable; do not weaken authorization to accommodate stale UI.
6. Validate error trace persistence and ensure the UI does not claim that a persisted trace is unavailable where it can be linked.

## Remaining limits

One Owner, one organization/project/environment, Chromium, same browser context and one controlled mock-backed route were exercised. WebKit/Firefox, other roles/scopes, private windows, cookie/session configurations, real providers, network loss and concurrent server failures remain unverified. No product implementation was modified and no external provider spend occurred.
