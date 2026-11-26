# CP64 — WebKit mobile Access & Keys scope race

**Result:** FAIL — DEF-029 independently reproduced in WebKit at 375×844.  
**Baseline:** frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Method and result

Using the isolated RC4 Console as the synthetic organization owner, the browser-only Playwright harness held the HTTP 200 response for `GET /admin/v1/virtual-keys` scoped to project `prj_01m85ry2gme00an7wydh1pd1gc` and environment `env_01m85ry445e009xg6sr14c5q1z`. The response contained ten keys. While it was pending, the real workspace selector was changed to **No project / No env**.

Before releasing the response, Access & Keys showed the no-scope notice and zero environment keys. After release, the same no-scope selection and notice remained, but the page showed ten prior-environment rows, all with **Revoked** state in this dataset. The browser displayed no key secrets or mutation controls in the captured rows. Measured viewport, document, and body widths were all 375px; no horizontal overflow was observed.

I visually inspected both screenshots. The before image shows the selected No project / No env menu and empty state. The after image shows that same selected scope with a count of 10 and prior-environment key rows. This confirms a cross-browser, narrow-viewport reproduction of DEF-029; it does not establish an authorization bypass.

## Evidence

- `../evidence/CP64-webkit-access-race.json` — request scope, status, sanitized UI text, counts, dimensions, and stale-row assertion.
- `../evidence/CP64-webkit-access-before-release-375.png` — visually inspected no-scope empty state before response release.
- `../evidence/CP64-webkit-access-after-release-375.png` — visually inspected stale rows after response release.
- Detailed defect and acceptance criteria: [DEF-029](../reports/DEF-029-access-keys-stale-after-workspace-scope-change.md).

## Coverage update

DEF-029 remains confirmed Low (P3); this is an independent WebKit/mobile replication, not a new defect. Chromium and WebKit now reproduce the same delayed-response inconsistency. Other mobile widths, environment-to-environment out-of-order responses, limited-role behavior, and other Access & Keys async loads remain unverified. Continue with separate Console surfaces and role contexts.

No product code, database, or server configuration was changed. The only delay was injected in the test browser.
