# CP79 — DEF-021 local member enrollment retest

**Source:** exact unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite clone `127.0.0.1:18162`; Chromium 156; synthetic Owner and HUMAN Developer.

**Disposition:** **PASS (scoped QA-005 retest)** for Owner-created local HUMAN member → one-time enrollment → signed-in Developer Console → persistent Owner readback. The original no-enrollment behavior was not reproduced on RC7. The prior QA-002/003/004 failures remain historical evidence and are not erased.

## Reproduction and result

1. Signed in as the synthetic Owner in Chromium and opened **Access & Keys → Members & Roles → Add member**. Entered `qa005-cp79-def021@example.invalid`, selected Developer, and selected `QA005 No Budget Project`. The pre-submit form was captured and visually inspected in `evidence/screenshots/CP79-def021-add-member-form.png`.
2. The Owner Console created the HUMAN member. From that member's row, selected **Issue invite token**. A second isolated browser context opened the enrollment link with the one-time synthetic token held only in process memory; no token-bearing screenshot or output was retained.
3. Entered a synthetic password in the two activation fields and selected **Activate account**. The enrollment request returned HTTP 200. The new context reached Overview; its screenshot was visually inspected and shows the signed-in synthetic Developer identity and role in the Console: `evidence/screenshots/CP79-def021-member-overview.png`.
4. Closed that browser and signed in again as Owner. A fresh authenticated `/admin/v1/members?limit=100` read returned HTTP 200 and found the same principal as `kind=HUMAN`, `roles=[DEVELOPER]`, `enrollment_pending=false`, `has_password=true`, and bound only to the selected synthetic project. The Access & Keys table also showed **Password set** and Developer after the fresh Owner login; visually inspected screenshot: `evidence/screenshots/CP79-def021-persistent-member-row.png`.
5. Sanitized result fields are in [CP79 evidence JSON](../evidence/CP79-def021-local-enrollment.json). No token or password was written to evidence, screenshots, logs, or Git. The activation token was only in memory during the flow; the synthetic password was discarded with the browser process.

## Limits and disposition

This confirms one SQLite/Chromium local-auth enrollment path for an Owner-created, project-bound Developer. It does not establish external email delivery, token expiry or replay rejection, cross-organization token rejection, enrollment audit visibility, password recovery, role-specific allowed/forbidden writes, Team Admin/other role enrollment, PostgreSQL, alternate browsers, or keyboard/screen-reader behavior. These remain UNVERIFIED. The broad acceptance criteria are not fully closed; QA-005 records only this scoped PASS. No product or specification code was changed. `GATE-REL-07` remains BLOCKED.
