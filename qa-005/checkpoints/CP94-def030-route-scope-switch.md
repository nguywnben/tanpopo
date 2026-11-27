# CP94 — DEF-030 Routes & Policies scope switching

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite `127.0.0.1:18162`; Chromium 156; Owner account `qa005-owner@example.invalid`.

## Result

**PASS (scoped QA-005 retest of DEF-030's ordinary scope-switch behavior).** In the real Routes & Policies Console, the QA005 No Budget Project / QA005 Budgetless Dev scope displayed three routes; switching in place to Initial Project / dev displayed a distinct set of four routes; switching to Organization / No project / No env displayed an explicit scope-selection banner and zero routes; switching back restored the same original three QA-project routes. At each populated scope the real HTTP 200 environment-filtered route response IDs and slugs matched the rendered list. No stale route remained across the tested transitions. The screenshot sequence was inspected visually.

## Reproduction

1. Sign in as the Owner on the isolated exact-source SQLite runtime and open Routes & Policies at `QA005 No Budget Project / QA005 Budgetless Dev`.
2. Verify the three visible QA project route slugs against `GET /admin/v1/routes?env_id=env_01m8d6f78xe00a9w53vt41rnqg&limit=100` (HTTP 200, three rows).
3. Use the workspace selector to change to `Initial Project / dev`; verify the four different rows against the filtered HTTP 200 response for `env_01m8cj8phze00a0t3c590b5mdw`.
4. Change to `No project / No env`; verify the explicit banner, zero rows, disabled Create route action, and that no routes collection request is sent without an environment.
5. Return to the QA project/environment; verify the same three rows and IDs return.

Screenshots, all visually inspected: [QA project](../evidence/screenshots/CP94-def030-qa-project-routes.png), [Initial Project](../evidence/screenshots/CP94-def030-initial-project-routes.png), [organization only](../evidence/screenshots/CP94-def030-no-project-routes.png), and [return to QA project](../evidence/screenshots/CP94-def030-returned-qa-routes.png). Sanitized route IDs, request filters and UI counts: [CP94 evidence JSON](../evidence/CP94-def030-route-scope-switch.json).

## Limits and reconciliation

This is a scoped ordinary scope-transition pass for one Owner, SQLite, Chromium, two populated environments and one no-project scope. It did not deliberately hold an old-scope response across a switch, test hard refresh/navigation/repeated rapid switching/stale actions, or exercise other roles, PostgreSQL/Valkey or browser engines. The historical QA-002 failure and QA-004 partial result remain preserved. The organization-only view's zero rows are not evidence of an organization-wide route inventory contract. No product state was modified, no provider called, and no paid usage incurred. QA-005 remains in progress; `GATE-REL-07` remains BLOCKED.
