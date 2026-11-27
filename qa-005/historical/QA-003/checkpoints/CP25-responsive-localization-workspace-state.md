# CP25 — Responsive localization and mobile workspace state

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium 156 with responsive CSS viewports, synthetic Operator/Org Owner tenant
**Status:** scoped Chromium responsive/localization and workspace paths **PASS**; full browser/platform/localization acceptance remains **PARTIAL**. No new product defect confirmed. `GATE-REL-07` remains **BLOCKED**.

## Locale control and Vietnamese user journey

1. Opened the visible Language control in authenticated Chromium and enumerated 15 options: Simplified Chinese, Traditional Chinese, German, English, Spanish, French, Indonesian, Italian, Japanese, Korean, Portuguese, Russian, Thai, Turkish, and Vietnamese. Evidence: [language menu screenshot](../evidence/CP25-language-menu.png), [responsive/localization results](../evidence/CP25-responsive-locale-result.json).
2. Selected **Tiếng Việt** through the real control. Overview rendered `Tổng quan`, localized metric labels and alert copy, and Vietnamese numeric decimal separators. Providers rendered `Nhà cung cấp`, localized create/seal actions, billing/state captions and status copy. The language remained selected across route navigation from Overview to Providers. Screenshots: [Vietnamese Overview desktop](../evidence/CP25-overview-vi-desktop.png), [Vietnamese Providers mobile 375px](../evidence/CP25-providers-vi-375.png).
3. Only Vietnamese and English were visually exercised in this phase. The other 13 menu choices were confirmed as available but their strings, truncation, bidi behavior and workflows are **UNVERIFIED**. No RTL locale was selected.

## Responsive layout, 320–1440 CSS px

Set real browser viewport widths and inspected each captured page image after load. Overview had no document/body horizontal overflow at **320, 375, 768, 1024 and 1440 px**; Providers had no document/body horizontal overflow at **320 and 375 px**. Browser-reported document width matched viewport width in each sample. Evidence: [sanitized measurements](../evidence/CP25-responsive-locale-result.json), Overview screenshots [320](../evidence/CP25-overview-vi-320.png), [375](../evidence/CP25-overview-vi-375.png), [768](../evidence/CP25-overview-vi-768.png), [1024](../evidence/CP25-overview-vi-1024.png), [1440](../evidence/CP25-overview-vi-1440.png), and Providers [320](../evidence/CP25-providers-vi-320.png), [375](../evidence/CP25-providers-vi-375.png).

At 320 px the scope, search and emergency controls collapse into icon-sized toolbar buttons; the scope button retains a useful full-scope `title`. Provider records become stacked rows. The Connections/Vault Secrets/Adapters tabs are only partly visible, with a scroll chevron. Keyboard navigation moved tab focus with ArrowRight from Connections to Vault Secrets to Adapters, and Enter activated Adapters; the page switched to the adapter list without overflow. See [mobile adapter view](../evidence/CP25-providers-mobile-adapters.png) and [keyboard result](../evidence/CP25-mobile-tab-keyboard-result.json). This confirms keyboard reachability of the hidden tab, not touch-target, screen-reader, or physical-device acceptance.

These are Chromium CSS viewport emulations, not Android Chrome, iOS Safari or a native device. 1920 px, tablet touch behavior, all remaining screens, orientation change, virtual keyboard interaction, dark theme and cross-browser layout remain unverified.

## Mobile scope switching and downstream data

On a 320 px viewport, opened the icon-only scope button. The visible popover showed **QA003 Operator CP22** and **Default Organization**, plus project/environment levels. Selecting the synthetic Operator-created organization updated scope to `QA003 Operator CP22 / No project / No env`; Providers settled to its empty state (`Configured connections 0`, “No provider connections”). The test then refreshed. As observed in CP22 on desktop, the bootstrap Operator session returned to `Default Organization / default / dev` on reload, where the three original synthetic provider connections reappeared. Manually switching back to Default also restored the same three rows. Evidence: [scope menu](../evidence/CP25-mobile-scope-menu.png), [cross-scope UI readback](../evidence/CP25-mobile-scope-switch-result.json), [empty organization screen](../evidence/CP25-providers-vi-mobile-operator-org.png), [post-refresh default](../evidence/CP25-providers-vi-mobile-refresh.png), and [restored default](../evidence/CP25-providers-vi-mobile-restored.png).

This reproduces the earlier scope-reset behavior; no requirement says the bootstrap Operator's last selected tenant must persist, so it remains an observation, not a new defect. The UI showed no stale provider rows when switched into the empty organization and returned the existing three rows after switching back. The zero horizontal-scroll finding is scoped to the named screens and widths only.

## Coverage and disposition

- CP25 adds **PASS for tested cases**: Chrome CSS viewport/no-page-overflow checks on Overview at five widths, Providers at two mobile widths, Vietnamese rendering on those screens, keyboard tab navigation at 320 px, and UI workspace switch/readback/reload behavior.
- FT-09/SCR-13, SCR-01/SCR-03 and Chromium platform coverage remain **PARTIAL**; CP25 adds mobile scope/read-only Provider checks and two localized screens. It does not close broader screen, role or device coverage.
- No new defect filed. Existing defects DEF-040–DEF-044 remain open. Historical defects remain 17 **VERIFIED FIXED for scoped RC5 cases**, 3 **PARTIAL**, and 19 **UNVERIFIED**; earlier campaign results are not inherited.
- QA-003 is incomplete; `GATE-REL-07` remains **BLOCKED**.
