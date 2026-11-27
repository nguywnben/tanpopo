# CP101 — DEF-084 mobile Overview alert overflow

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite served through `https://127.0.0.1:19444`; real Chromium 156; `ORG_OWNER`; populated QA005 org/project/environment; no production provider.

## Result

**FAIL — DEF-084 (Low, fresh QA-005 reproduction).** On the loaded Overview page, an active synthetic project-budget alert contains an unbroken budget identifier. At a 375px viewport, `document.documentElement.scrollWidth` and `body.scrollWidth` were 413px (38px horizontal overflow). At 320px, both remained 413px (93px overflow). The measured alert description span begins at x=27 and ends at x=413; computed `white-space`, `word-break` and `overflow-wrap` are all `normal`, so the long underscore-delimited token does not wrap. Actual screenshots show the alert ID clipped by the viewport and the whole page wider than the device. A hard refresh retained the same alert and overflow. The overview summary returned HTTP 200, no relevant Admin API request failed, and there were no browser page errors.

The Remediate button remains on-screen; this finding is a mobile readability/navigation defect, not a data-integrity, authorization or budget-enforcement result. No write or provider request was made. The Overview figures and alert are existing synthetic QA state, not evidence of paid provider usage.

## Reproduction

1. Sign in to the exact-source Console as `ORG_OWNER` and open Overview with the QA005 budget alert present.
2. Wait for `/admin/v1/overview/summary` to return HTTP 200 and the summary cards/alert to render.
3. Set the viewport to 375×844. Observe the alert's long `bgt_project_…_monthly` identifier extending beyond the right edge. Record `document.documentElement.scrollWidth` (413px).
4. Hard refresh, set the viewport to 320×844 and wait for the overview to finish rendering. Confirm the same alert remains and page width is still 413px.

Visually inspected screenshots: [375px](../evidence/screenshots/CP101-overview-mobile-375.png) and [320px after hard refresh](../evidence/screenshots/CP101-overview-mobile-320.png). The 375px accessible text, geometry and API results are in the [sanitized CP101 evidence](../evidence/CP101-overview-mobile-alert-overflow.json).

## Expected behavior and acceptance criteria

- On narrow viewports, arbitrary alert identifiers should wrap, truncate with a usable disclosure, or otherwise remain readable without widening the page beyond the viewport.
- At 320px and 375px, `documentElement.scrollWidth` should not exceed `innerWidth` for Overview with this long alert.
- The full alert meaning and budget context should remain available to keyboard and screen-reader users, and Remediate should continue to target the same budget.
- Regress with long organization/project/environment/budget identifiers, other locales, alert types, and both narrow and desktop layouts.

## Scope limits

Only one existing synthetic budget alert was exercised in Chromium on exact RC7 SQLite. Other alert shapes/locales, keyboard/screen-reader behavior, WebKit/Firefox, desktop comparison, dynamic updates and backend variants remain UNVERIFIED. No product source or specification was changed. `GATE-REL-07` remains BLOCKED.
