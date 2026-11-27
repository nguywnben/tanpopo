# CP86 — WebKit mobile Members & Roles workflow

**Disposition:** PASS (scoped WPE WebKit responsive workflow).
**Source:** unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
**Runtime:** isolated SQLite `127.0.0.1:18162`, exposed to the test browser by a loopback HTTPS reverse proxy at `127.0.0.1:19443`; a one-day self-signed localhost test certificate was stored only in `/dev/shm`.
**Browser:** Playwright WPE WebKit 26.5, build 2336. The existing QA browser cache and previously extracted test libraries were reused; no host package installation occurred.

## User workflow and result

1. At 320×844, sign in as the synthetic Owner over HTTPS and confirm `/admin/v1/auth/session` returns HTTP 200.
2. Use the mobile navigation menu to open Access & Keys → Members & Roles. The real screen rendered all 12 rows in a stacked layout.
3. At 375×844, verify the same 12 rows and responsive content remain within the viewport.
4. Return to 320px and open **Add member** without submitting. The drawer spans x=56..320 (264px wide); email, role, project, Cancel and Add member controls are visible and the drawer has no internal horizontal overflow.

At both viewports, document width equaled viewport width and no page-level horizontal overflow occurred. No WebKit page errors were recorded. The real browser screenshots were visually inspected: [320px Members](../evidence/screenshots/CP86-webkit-members-320px.png), [375px Members](../evidence/screenshots/CP86-webkit-members-375px.png), and [Add member drawer](../evidence/screenshots/CP86-webkit-add-member-320px.png). The 320px scroll-control interaction is in [this screenshot](../evidence/screenshots/CP86-webkit-tab-arrow-click.png). Full sanitized measurements are in [CP86 JSON](../evidence/CP86-webkit-mobile-members.json).

## Limits

This is WPE WebKit, not actual Safari or a physical Apple device. It covers one Owner screen and does not test member submission, touch hardware, keyboard-only completion, localization or other pages/roles. See CP87 for the scoped Axe and tablist accessibility check. `GATE-REL-07` remains BLOCKED.
