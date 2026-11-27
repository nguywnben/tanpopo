# CP85 — Narrow viewport Members & Roles workflow

**Disposition:** PASS (scoped responsive workflow).
**Source:** exact unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
**Environment:** isolated SQLite `127.0.0.1:18162`; Chromium 156; synthetic Owner; active organization; 12 member rows.

## User journey and result

At 320×844 the authenticated Owner used the accessible **Open navigation menu** control and navigated to Access & Keys → Members & Roles. The page rendered all 12 member rows in stacked mobile form. Document, body, and main content widths were each 320 CSS pixels; no page-level horizontal overflow occurred. At 375×844, the document width was 375 and all 12 rows remained present.

At 320px, the tab strip initially scrolled 15px, partly clipping the first tab label. A real horizontal wheel gesture moved it to `scrollLeft=0`, revealing the Virtual Keys tab; the responsive next-tab arrow was visible in the inspected screenshot. The Add member drawer opened at x=56 with width 264 ending at the 320px viewport edge. Email, role and project controls, Cancel and Add member remained within the drawer; the form was not submitted. No page errors occurred.

Visual evidence was inspected during interaction: [320px member list](../evidence/screenshots/CP85-members-320px.png), [375px member list](../evidence/screenshots/CP85-members-375px.png), [320px tab-strip scroll](../evidence/screenshots/CP85-members-320px-tab-scroll.png), and [320px Add member drawer](../evidence/screenshots/CP85-add-member-320px.png). Sanitized layout measurements are in [CP85 JSON](../evidence/CP85-mobile-members-responsive.json).

## Limits

This is one Owner/Chromium responsive slice. WebKit, Firefox, physical devices, touch/screen-reader/keyboard accessibility, other roles/tables/locales and form submission at mobile width remain UNVERIFIED.
