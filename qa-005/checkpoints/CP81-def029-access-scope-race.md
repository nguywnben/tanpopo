# CP81 — DEF-029 Access & Keys stale-response retest

**Source:** exact unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite clone `127.0.0.1:18162`; Chromium 156; synthetic Owner.

**Disposition:** **PASS (scoped QA-005 retest)** for one controlled populated environment → No project / No env response race. The held prior-scope response did not repopulate stale keys after scope changed. Historical QA-002 failure evidence is preserved; this does not close DEF-029.

## Reproduction and result

1. Signed in to the real Console and opened **Access & Keys → Virtual Keys** at `QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev`.
2. A browser-only Playwright route held the first authenticated `GET /admin/v1/virtual-keys?project_id=…&env_id=…` response. The upstream request returned HTTP 200 with nine rows, all revoked. Evidence records only key names/state; no key material was read or stored. The pre-switch skeleton and scope were visually inspected in `evidence/screenshots/CP81-def029-loading-old-scope.png`.
3. While the response was still held, used the workspace selector to choose **No project** and **No env**. Before releasing the old response, the page showed the no-scope information banner, count `0`, and **No Virtual Keys in this environment**. Screenshot visually inspected: `evidence/screenshots/CP81-def029-no-project-before-release.png`.
4. Released the original HTTP 200 response. After settling, the scope still showed **No project / No env**, count remained `0`, no old key names were visible, and the empty state remained. Screenshot visually inspected: `evidence/screenshots/CP81-def029-no-project-after-release.png`.

See [sanitized race evidence](../evidence/CP81-def029-access-scope-race.json) for the exact captured query scope and counts.

## Limits

This tests one Owner/SQLite/Chromium transition from a populated environment to no project/no environment. Cross-project and environment-to-environment out-of-order responses, WebKit/mobile, other roles, active-key metadata, current-scope request overlap, navigation recovery, and broader API tenant isolation remain UNVERIFIED. Earlier QA-002 Chromium/WebKit failures remain preserved and are not waived. No product code was modified. `GATE-REL-07` remains BLOCKED.
