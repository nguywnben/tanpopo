# CP28 — Access & Keys delayed response across scope change

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium 156; synthetic Org Owner  
**Disposition:** DEF-029 stale rows were **not reproduced in this scoped case**; defect status remains **PARTIAL**. No keys were created, rotated, or revoked. `GATE-REL-07` remains **BLOCKED**.

## Reproduction attempt

1. Signed in through Chromium and selected `Default Organization / default / dev`. Access & Keys displayed two previously created synthetic Virtual Keys; both were already **Revoked**. The table exposed only metadata and the UI did not expose either secret. The screenshot redacts their key prefix/last-four values: [default scope with response pending](../evidence/CP28-access-keys-response-held.png).
2. Installed a browser-only Playwright route that fetched and held the next authenticated `GET /admin/v1/virtual-keys?project_id=…&env_id=…&limit=100` response. It returned HTTP 200 with those two revoked records. Evidence stores only synthetic key names and states, not token fragments or secret material.
3. While the response was held, changed the workspace selector to `Default Organization / No project / No env`. Before release, the page showed the scope prerequisite, a zero count, and “No Virtual Keys in this environment.” [No-project state before release](../evidence/CP28-access-no-project-before-release.png).
4. Released the old `default/dev` response. The selected scope remained `No project / No env`; the UI stayed at zero with the same prerequisite and empty-state copy. No stale row appeared. The final screenshot was visually inspected with credential fragments masked: [No-project state after release](../evidence/CP28-access-no-project-after-release.png). Full sanitized interaction summary: [JSON evidence](../evidence/CP28-def029-response-race.json).

## Reconciliation

The QA-002 CP62/CP64/CP65 record reproduced stale rows with ten revoked keys, including cross-project state. This RC5 attempt had only two revoked records, one held response, one browser, and one transition to no-project scope. Its non-reproduction does not establish the acceptance criteria across 10-key populated state, active keys, environment-to-environment ordering, cross-project switch, WebKit, other roles, repeated requests, or rapid navigation. DEF-029 therefore moves from UNVERIFIED to **PARTIAL** and remains open.

No credential was created or disclosed, no mutation was attempted, and no authorization boundary was crossed. No new defect was found.

## Coverage

- SCR-13 remains **PARTIAL**. This adds one delayed-response/no-project case in Chromium.
- Historical register totals: 17 VERIFIED FIXED for scoped RC5 scenarios, 6 PARTIAL (DEF-005, DEF-007, DEF-029, DEF-032, DEF-035, DEF-039), and 16 UNVERIFIED. These are scoped retest statuses, not complete acceptance closure.
- QA-003 remains incomplete; `GATE-REL-07` remains **BLOCKED**.
