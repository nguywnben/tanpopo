# CP95 — DEF-030 delayed prior-scope routes response

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite `127.0.0.1:18162`; Chromium 156; Owner account `qa005-owner@example.invalid`.

## Result

**PASS (scoped QA-005 stale-response race retest).** A real HTTP 200 route response for Initial Project/dev containing four rows was fetched and held at the browser boundary. While it remained held, the real Console workspace selector switched to QA005 No Budget Project / QA005 Budgetless Dev. That scope's unmocked HTTP 200 route request completed and its three distinct rows rendered. After releasing the delayed four-row response, the current QA project continued to display exactly its same three rows. No old-scope slug appeared, and there were no page errors or product writes.

## Reproduction

1. Sign in as Owner on the isolated exact-source runtime and select Initial Project/dev.
2. Install a Playwright route handler that calls `route.fetch()` for the exact Initial Project environment, captures the genuine HTTP 200 response, and delays only delivery of that response to the page.
3. Open Routes & Policies and confirm the Initial Project response has four rows while the page is still waiting on the delayed browser response.
4. Switch in the real workspace picker to QA005 No Budget Project / QA005 Budgetless Dev.
5. Wait for its real environment-filtered HTTP 200 response and verify its three rows render.
6. Release the already-fetched Initial Project response. Verify the UI remains on the QA project with the same three slugs and no Initial Project rows.

Visually inspected screenshots: [held prior-scope response](../evidence/screenshots/CP95-def030-held-initial-request.png), [new-scope rows before release](../evidence/screenshots/CP95-def030-new-scope-before-release.png), and [new-scope rows after release](../evidence/screenshots/CP95-def030-new-scope-after-release.png). Sanitized response metadata and row identities: [CP95 evidence JSON](../evidence/CP95-def030-held-route-response.json).

## Limits and reconciliation

This is one real delayed-response race on Owner/SQLite/Chromium with two populated project environments. It directly retests the prior-scope contamination behavior and supplements CP94's ordinary switching result. Hard refresh, navigation-away/back, repeated rapid switches, stale row actions, other roles, PostgreSQL/Valkey, and other browser engines remain UNVERIFIED. The historic QA-002 failure and QA-004 partial outcome remain intact. No provider requests, paid usage, or product writes occurred. `GATE-REL-07` remains BLOCKED.
