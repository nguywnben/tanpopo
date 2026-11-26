# CP44 — Access & Keys delayed response across workspace scopes

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`<br>
**Runtime:** isolated QA RC5 service on `18116`, synthetic Owner, Chromium 156/Linux at 1440×1000<br>
**Disposition:** DEF-029 stale-row behavior **not reproduced in two scoped RC5 cases**; remains **PARTIAL**. `SCR-13` remains **PARTIAL**. QA-003 is incomplete; `GATE-REL-07` remains **BLOCKED**.

## Setup and method

The authenticated Owner `default/dev` environment had ten Virtual Key rows: eight `CREATED_ACTIVE` and two `REVOKED`. Three active synthetic fixture keys were created through the real Access & Keys UI; the sanitized setup record contains names, HTTP creation statuses, lifecycle state and versions only. No key plaintext was captured. See [fixture result](../evidence/CP44-key-fixture-result.json) and [visually inspected populated list](../evidence/CP44-ten-key-populated-active.png).

In two separate real Chromium sessions, Playwright intercepted the scoped `GET /admin/v1/virtual-keys` for `default/dev`, fetched the exact HTTP 200 response, and held it before browser fulfillment. The response body contained all ten mixed-state records. While that request remained unresolved, the Owner used the actual Console workspace switcher to change first to `No project / No env` and then, in a separate reproduction, to the empty synthetic project `QA003 Empty Project DEF035 / No env`. After confirming the new scope rendered, the held response was released. This tests a delayed prior-scope list response against two distinct target scopes.

## Observed results

- In both runs, the active scope changed to the selected target and the page presented the “Select a project and environment” banner and “No Virtual Keys in this environment” empty state.
- The list remained at zero rows before and after releasing the ten-record prior-scope response. No active or revoked record from `default/dev` appeared in either target scope.
- The actual before/after screenshots were visually inspected. The screenshots show the correct workspace selector, scope-specific banner and empty state; the held request screenshot shows the loading skeleton while the prior-scope response is pending.
- Sanitized transition records, including API status, query scope, key names/state and rendered text/row counts: [no-project run](../evidence/CP44-def029-response-race.json), [cross-project run](../evidence/CP44-def029-cross-project-race.json). Screenshots: [held request](../evidence/CP44-active-revoked-response-held.png), [no-project before release](../evidence/CP44-no-project-before-release.png), [no-project after release](../evidence/CP44-no-project-after-release.png), [cross-project held request](../evidence/CP44-cross-project-response-held.png), [empty-project before release](../evidence/CP44-empty-project-before-release.png), [empty-project after release](../evidence/CP44-empty-project-after-release.png).

## Defect and coverage disposition

DEF-029 remains **PARTIAL**: the prior QA-002 reproduction is preserved, and this RC5 pass now covers a ten-row populated list with active and revoked keys, one delayed old-scope response, Chromium Owner, and both organization-only/no-project and cross-project/no-environment target scopes. No stale row was reproduced in these cases. This is not closure. Repeated and out-of-order requests, environment-to-environment switching, a populated second project, other authorized roles, WebKit, refresh/back navigation races and full acceptance criteria remain **UNVERIFIED**.

`SCR-13` remains **PARTIAL**. No product defect was added by this checkpoint. The synthetic active keys are isolated QA fixtures; no authorization boundary was weakened. No product source was modified.

## QA status

- Historical defect totals remain: 17 verified fixed for scoped RC5 scenarios, 8 partial, 14 unverified. These are retest dispositions, not full closure.
- New defects DEF-040–046 remain open as previously documented; no additional defect was confirmed here.
- GATE-REL-07 remains **BLOCKED**.
