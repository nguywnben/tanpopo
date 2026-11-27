# CP77 — DEF-047 cross-tab Playground logout retest

**Source:** frozen unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; disposable SQLite clone `127.0.0.1:18162`; Chromium 156; two pages sharing one browser context; synthetic Org Owner; `QA005 Org / Initial Project / dev`.

**Disposition:** **PASS (scoped QA-005 retest)** for cross-tab sign-out clearing the first tab's active Playground presentation and for server-side rejection of the captured ephemeral key after logout. This does not resolve historical QA-003/QA-004 variance for every route, browser or session path.

## Reproduction and result

1. Signed in in tab A and selected `Initial Project / dev`. Opened Playground and sent the synthetic canary using slug `qa005-playground-openai`. The request returned HTTP 503 `no_compliant_deployment_available` with trace `trc_01m8djwqdee00bj0ypx0beaxhe`; a route fixture was visible by name but no compliant active deployment was available in this scope. No provider request or paid usage occurred. Despite that route failure, the Playground presented its ephemeral session cap/expiry and Send/End session controls. The browser captured the request's Authorization header in memory only; the credential is not recorded in evidence.
2. Opened tab B in the same Chromium context at Overview, then used the visible **Sign out** control. Tab B navigated to `/login`.
3. Without refreshing tab A, its UI changed to the sign-in page with **Session ended** and the reauthentication explanation; the Playground Send control was absent. Visually inspected [tab A before logout](../evidence/screenshots/CP77-def047-playground-active.png) and [tab A after tab B signed out](../evidence/screenshots/CP77-def047-first-tab-after-logout.png).
4. From tab A, `GET /admin/v1/auth/session` returned HTTP 401. Replayed the captured ephemeral key directly to the loopback Data Plane without cookies; it returned HTTP 401 `invalid_virtual_key`. The key was not printed or persisted. Machine-readable sanitized results: [CP77 evidence JSON](../evidence/CP77-def047-multitab-logout.json).

## Scope and limits

This RC7 run confirms the stale active-tab state was not reproduced in one Owner/Chromium/same-context case; the administrative cookie and ephemeral key were rejected after logout. The pre-logout inference did not succeed because the selected route had no compliant active deployment, so this is not a successful inference/session correlation test. Successful-route precondition, simultaneous send/logout races, more tabs, role/scope combinations, expiry/rotation paths and WebKit/Firefox remain UNVERIFIED. Historical QA-003 stale-state evidence remains preserved; the QA-004 scoped pass is consistent with this observed state-clearing behavior. No source/specification change. `GATE-REL-07` remains BLOCKED.
