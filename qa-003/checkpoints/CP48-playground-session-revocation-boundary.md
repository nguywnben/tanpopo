# CP48 — Playground end-session and logout revocation boundary

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated RC5 service `18116`; controlled loopback Ollama route; Org Owner; real Chromium, 1440×1000  
**Disposition:** no new defect confirmed. `FT-01`, `FT-09`, `WF-06`, and `SCR-15` receive narrow lifecycle evidence and remain **PARTIAL**. QA-003 remains in progress; `GATE-REL-07` remains **BLOCKED**.

## Real Playground interaction

The Owner selected `qa003-cp47-stream-route`, sent “Session lifecycle check.”, and received the local mock response. The active UI showed the session budget and expiry, transcript, `End session` action, and a zero-cost HTTP 200 trace. The visually inspected [active-session screenshot](../evidence/CP48-playground-session-active.png) captures that state. The sanitized interaction record is [session end result](../evidence/CP48-playground-session-end-result.json).

After clicking **End session**, the UI cleared the transcript and active-session controls, showed “Ephemeral session key cleared from this tab. Signing out revokes active Playground keys.”, and still displayed the previous execution trace below “No turns yet.” The latter is a visible cross-state observation: the transcript was cleared while trace history remained in the page. No documented contract found in this QA pass says End session must erase persisted trace history; therefore this is recorded for UX review, not as a confirmed defect. The actual [ended screenshot](../evidence/CP48-playground-session-ended.png) was visually inspected.

## Token and revocation checks

Immediately after End session, the same key remained `CREATED_ACTIVE` version 1 in the Admin Virtual Key inventory. A Data Plane request using the in-memory ephemeral key with `credentials: omit`—matching Playground's browser request context—returned HTTP 200. The sanitized [post-End acceptance record](../evidence/CP48-session-end-token-acceptance.json) does not contain the secret or request body/header values. This confirms that End session clears the current tab's UI state; it does not revoke an already minted backend key. This is consistent with the screen's explicit wording and the documented short TTL (up to 15 minutes), so it is not independently classified as a defect. Reuse by a copied key until expiry, browser tab closure, and TTL boundary behavior remain untested.

A separate session key was then left active while the Owner used the actual **Sign out** control. Logout returned HTTP 204. After fresh authentication, the same key was `REVOKED`, version 2. A no-secret inventory read of `default/dev` found **all 14** fixture rows labeled `Playground session` in `REVOKED` version 2, including keys minted earlier in the same fixture. The sanitized [logout result](../evidence/CP48-logout-revocation-result.json) and [14-key inventory](../evidence/CP48-playground-logout-inventory.json) preserve the state readback. The [post-logout sign-in screen](../evidence/CP48-after-logout.png) and [freshly authenticated Access & Keys screen](../evidence/CP48-key-revoked-after-logout.png) were visually inspected; the latter is an unscoped empty state and is not represented as visual proof of each revoked row. The state conclusion relies on authenticated API readback.

## Coverage and limits

- The actual browser showed successful response → End session → signed-out state; fresh login and API persistence were checked.
- `End session` did not revoke the server key immediately; the remaining short-TTL exposure and behavior after TTL, tab close, browser restart, multiple browser profiles, and copied-key use beyond this controlled check remain open.
- Logout revoked all 14 observed Playground-session keys in one Owner's `default/dev` inventory. Other roles, organizations, scopes, concurrent logout races, server restart and backend failures during revocation remain unverified.
- UI kept the prior execution trace visible after clearing the transcript. This may be intentional persisted trace history; no defect is confirmed without a stronger trace-clearing contract.
- Existing DEF-045 and DEF-046 remain unchanged; this pass neither closes nor expands their acceptance criteria.
- No source code was modified, no external provider was contacted, and external spend was USD 0.
