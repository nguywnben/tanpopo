# CP62 — Delayed Access & Keys response across workspace scope change

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Result:** FAIL DEF-029 confirmed. A delayed environment-scoped response repopulated 10 stale keys after switching to No project / No env. No secret was exposed and no authorization bypass was observed.
**Release gate:** `GATE-REL-07 BLOCKED`.

## Reproduction and result

Signed in as the synthetic Org Owner in the isolated CP43 RC4 Console and selected `QA002 CP43 Secondary Project / qa002-cp43-env2`. I used Playwright’s browser routing only to hold the selected scope’s existing `GET /admin/v1/virtual-keys` response; no product code or server behavior was changed.

While the request was held, I opened Access & Keys and changed the real Console workspace chooser to **No project / No env**. Before release, the screen was consistent: informational banner requested a project/environment, count 0, empty state. The held response then returned HTTP 200 with 10 keys tied to the original project and environment. After that result settled, the workspace selector still showed No project / No env, yet the page rendered all ten old rows beneath the unchanged scope banner. Key secrets were never present in the UI response; only names, prefixes/last-four characters, plane/route and status appeared. Action cells were absent in the no-environment view.

This confirms a stale-response state defect in addition to DEF-023’s no-scope member-list clearing. The defects are related through workspace scope but have different symptoms and acceptance criteria: DEF-023 hides organization members whenever scope is absent; DEF-029 allows an older environment request to repopulate environment key metadata after scope is removed.

Recovery check: leaving Access & Keys for Overview and returning while keeping No project / No env caused the page to show zero keys again. Recovery by navigation is confirmed; response-order safety during all other scope changes is not.

## Evidence

- [`../evidence/CP62-access-scope-race.json`](../evidence/CP62-access-scope-race.json) — actual delayed URL query and 200 result, 10 sanitized environment-bound records, scope and UI before/after.
- [`../evidence/CP62-access-keys-loading-before-scope-switch.png`](../evidence/CP62-access-keys-loading-before-scope-switch.png), [`../evidence/CP62-access-keys-org-only-before-response.png`](../evidence/CP62-access-keys-org-only-before-response.png), and [`../evidence/CP62-access-keys-org-only-after-response.png`](../evidence/CP62-access-keys-org-only-after-response.png) — actual UI sequence, visually inspected.
- [`../evidence/CP62-access-scope-recovered-empty.png`](../evidence/CP62-access-scope-recovered-empty.png) and [`../evidence/CP62-access-after-navigation-recovery.png`](../evidence/CP62-access-after-navigation-recovery.png) — recovery through navigation, visually inspected.
- Related source path: `web/console/src/pages/access/AccessPage.tsx`; CP60 source review notes that the no-scope branch clears local lists but does not cancel earlier requests.

## Consolidated QA status

- **Historical DEF-001–015:** not retested in CP62. Retain prior scoped dispositions; DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Confirmed open defects:** DEF-016–028 remain open; new **DEF-029 Low** is confirmed. DEF-023 is independently confirmed; it should not be collapsed into DEF-029.
- **Coverage completed:** real workspace switch with an intentionally delayed key response; UI before and after response; direct response project/environment IDs; navigation recovery; screenshot inspection.
- **Blocked:** none for CP62. CP53 webhook delivery still lacks usable Operator credentials. A current Team Admin retest remains blocked by the intentionally suspended/cleared CP45 credential.
- **Remaining:** order-of-response tests for project-to-project and environment-to-environment switches; limited-role scope matrix; related member-list race; broader scope matrix on remaining Console screens; historical independent retests and the remaining features/platforms.
- **Engineering handoff:** add DEF-029 to the Access & Keys scope work with the deterministic hold/release acceptance test. Reconcile it alongside, but separately from, DEF-023.

QA-002 remains in progress. `GATE-REL-07` remains **BLOCKED**.
