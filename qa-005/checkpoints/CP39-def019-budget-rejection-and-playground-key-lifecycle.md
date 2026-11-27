# CP39 — DEF-019 budget rejection UX and Playground key lifecycle

**Status:** PASS (scoped retest of DEF-019); PRODUCT_GAP PG-006 recorded  
**Source:** frozen RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; compiled local artifact identity is recorded in CP00.  
**Runtime:** isolated exact-source SQLite Console/Data Plane at `127.0.0.1:18145`; local deterministic OpenAI-compatible fixture at `127.0.0.1:19090`; Chromium, 1440×1000. No paid provider was used.

## Reproduction and observations

1. Set the synthetic project's hard monthly ceiling to 1 micro-USD after its settled usage reached 1 micro-USD. Configure the test route to the loopback fixture.
2. Open Playground in the real Console, set the route, enter a synthetic budget-boundary prompt, and submit.
3. Observe the routed call return HTTP 429 `project_budget_exceeded` with `retryable:false`. The trace shows HTTP 429, `$0.000000`, and 0 ms. The Console renders the visible reason “Request blocked by budget ceiling: Budget limit exceeded across organization, project, environment, or virtual key scope.” and provides “Inspect budgets”. The rendered state is captured in `evidence/screenshots/CP39-playground-budget-rejection.png` and visually inspected.
4. Start another Playground session and select End Session. The tab displays “Ephemeral session key cleared from this tab. Signing out revokes active Playground keys.” Its synthetic key remained `Active` in the isolated Access & Keys inventory immediately after this action. See the visually inspected `CP39-playground-end-session-ui.png` and `CP39-playground-end-session-key-active.png`.
5. Sign out through the Console and inspect Access & Keys. The tested session keys appeared `Revoked`; the post-logout table is `CP39-playground-key-after-logout.png` and was visually inspected. This independently confirms the documented logout revocation path. Each test key was revoked after its lifecycle check. No credential value is retained in this evidence package.

## Defect disposition and limits

DEF-019 is a scoped PASS: a real rejected Playground request explains the budget boundary in the UI and exposes a recovery/navigation action. This covers one exhausted project hard ceiling and one synthetic route. Organization/environment/key limits, mixed-ceiling precedence, concurrent reservations, retry/recovery after replenishment, and other roles remain UNVERIFIED. Provider fixture hit count was not independently asserted for this check.

The End Session behavior is recorded as PG-006, not a defect. The screen tells users that tab-local clearing and logout revocation are separate; the reviewed SCR-15 contract requires a short-lived key and logout revocation but does not require immediate revocation on End Session. TEEP-01 should decide whether the desired business contract should revoke on either action. Key lifetime was bounded by the configured 15-minute TTL; exact expiry invalidation was not the purpose of this retest.

GATE-REL-07 remains BLOCKED. This checkpoint does not certify release readiness.
