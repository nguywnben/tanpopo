# CP87 — WebKit Members & Roles accessibility slice

**Disposition:** PASS (scoped accessibility check).
**Source/runtime:** exact frozen RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`, binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; SQLite `127.0.0.1:18162` through temporary HTTPS loopback; WPE WebKit 26.5 build 2336; Chromium was not substituted.

Axe Core 4.14.1 scanned the real 320×844 Access & Keys → Members & Roles document against WCAG 2.0 A/AA, 2.1 A/AA, and 2.2 AA tags. It reported **zero violations**. The accessibility tree exposes the “Access management views” tablist with “Virtual Keys 12” and selected “Members & Roles 12” tabs.

The visible round chevron control is a `button` with `aria-hidden=true` and `tabindex=-1`, intentionally outside the accessibility tree. A real pointer click changed horizontal tab-list scroll from 15px to 0px without changing the selected tab; this revealed the first tab and showed the opposite-direction control. The tablist itself remains named and exposes both tabs to assistive technology. Therefore the visual chevron was not classified as an unnamed-control defect. Axe ran through a Playwright init script; the application source/CSP were not changed. The screenshot and sanitized result are [here](../evidence/screenshots/CP87-webkit-members-accessibility-320px.png) and [here](../evidence/CP87-webkit-mobile-a11y.json).

This is one screen/viewport check, not a whole-app accessibility certification. Full keyboard-only journeys, screen readers, all screens, roles, locales, themes, engines and physical devices remain UNVERIFIED. `GATE-REL-07` remains BLOCKED.
