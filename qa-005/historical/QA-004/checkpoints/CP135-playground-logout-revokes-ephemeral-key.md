# CP135 — Playground logout revokes its ephemeral key

**Date:** 2026-11-26  
**Baseline:** exact frozen RC6 commit `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite, `127.0.0.1:18128`  
**Result:** scoped PASS; full Playground coverage remains PARTIAL.

## Question

Does signing out in another browser tab also invalidate the Playground's temporary Virtual Key, so a previously minted credential cannot continue to send requests after the administrative session ends?

## Reproduction and evidence

1. Signed in as the synthetic Org Owner in real Chromium at the `QA004 Test Root Org / default / dev` scope and opened Playground.
2. Two exploratory sends first returned HTTP 429 at the existing 100 micro-USD project ceiling (one selected `qa004-cp121-priced-vision`, one `qa004-loopback`); both UI traces showed zero cost. To avoid mistaking budget setup for Playground behavior, I raised the synthetic project ceiling temporarily through FinOps, then reran with only the loopback route.
3. Selected the isolated `qa004-loopback` route and submitted the exact prompt `Reply with the exact words: CP135 logout revokes my temporary test key.` with a 1000 micro-USD session cap.
4. The Playground received the expected `Hello from synthetic Ollama.` reply. The exact project ledger moved from 2036 to 2037 micro-USD (a 1 micro-USD / $0.000001 delta); estimated usage remained 14, holds remained zero. This local fixture made no external provider request. FinOps displayed 2038 micro-USD in the Org Owner aggregate and 2037 on this project's row; these are distinct scopes, and no product defect is inferred from their difference.
5. Opened a second tab sharing the browser session and used the visible Sign out action. The logout endpoint returned 204. The first tab then rendered the `Session ended` sign-in page, and its administrative session endpoint returned 401.
6. Replayed the in-memory ephemeral credential without cookies against `/v1/chat/completions`. RC6 returned 401 `invalid_virtual_key`.
7. Reauthenticated and read persistent state: the Playground key was `REVOKED`, version 2, still scoped to the intended project/environment, with no plaintext secret in metadata. Exactly one matching `virtual_key.revoked` event targeted the key at audit sequence 775.
8. The synthetic project's 100 micro-USD hard limit was restored to 100 through FinOps after the successful request and confirmed by readback. Exact usage reflects the 1 micro-USD fixture call; no changed hard limit or live test credential was left active.

## Result and limits

All observed post-logout authorization and persistence checks passed. The screenshots show the successful synthetic response, the other-tab logout, and the first tab's session-ended state. This independently verifies one Org Owner/Chromium/logout path, supplementing the earlier scoped DEF-047 presentation/session test. It does not complete SCR-15 or establish behavior for other roles, browsers, active/in-flight requests, expiry, populated project switching, or error recovery. No defect was assigned.

## Evidence

- [Sanitized machine-readable results](../evidence/CP135-playground-logout-key-results.json)
- [Budget at 100 micro-USD](../evidence/CP135-budget-100.png)
- [Temporary budget at 5000 micro-USD](../evidence/CP135-budget-5000.png)
- [Exploratory priced-vision send blocked at budget ceiling](../evidence/CP135-playground-active-session.png)
- [Exploratory loopback send blocked at budget ceiling](../evidence/CP135-retest-playground-active.png)
- [Successful Playground response](../evidence/CP135-playground-successful-session.png)
- [Prompt and selected loopback route](../evidence/CP135-playground-before-send.png)
- [Second tab signed out](../evidence/CP135-second-tab-signed-out.png)
- [First tab after logout](../evidence/CP135-playground-first-tab-after-logout.png)

QA-004 remains ongoing; historical and new defect totals are unchanged. **GATE-REL-07 remains BLOCKED.**
