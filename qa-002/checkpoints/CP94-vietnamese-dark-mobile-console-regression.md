# CP94 — Vietnamese dark-theme mobile Console regression

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169` (frozen RC4)
**Runtime:** isolated QA server `18084`; organization owner session
**Browser:** Chromium, 375 × 844 viewport
**Product changes:** none

## Scenarios and results

1. On a real narrow viewport, opened navigation, selected Vietnamese, enabled dark theme, and visited Overview, Providers, and FinOps.
2. Reloaded the Console and checked that `lang=vi` and the dark background persisted.
3. Compared viewport/document widths on all three pages and after reload.
4. Visually inspected each current screenshot. The translated navigation, page headings, labels, metric cards, provider cards, and budget summary rendered legibly without clipped horizontal content. Duplicate Google provider rows remain visually indistinguishable in their identity fields, consistent with existing DEF-032; CP94 does not independently reproduce the record-management consequences.

Evidence is in `evidence/CP94-vietnamese-mobile-state.json` and `evidence/CP94-*-vietnamese-*-mobile.png`. Values: document width equals viewport width (375 px) on the three pages and after reload; document language remains `vi`; dark background is `rgb(17, 17, 19)`. Overview metrics were localized (`50,00%`, `5,517 ms`, `$0,012001`) and visually legible. Providers and FinOps content was readable. FinOps continues to show `PROJECT` and `MONTHLY` as enum values; no requirement was found establishing these as localized labels, so this is not classified as a defect.

Initial exploratory screenshots are retained for evidence history, but the screenshot named `CP94-vietnamese-selector-open-mobile.png` captures the open selector and is not the post-navigation result. The four final-state screenshots are the `*-vietnamese-dark-mobile.png` files and `CP94-vietnamese-navigation-mobile.png`; these were inspected directly.

## Disposition

- **Coverage result:** scoped PASS for Vietnamese + dark theme persistence, rendered Overview/Providers/FinOps at 375 px, and absence of horizontal overflow in these states.
- **Defect result:** no new defect. Existing duplicate-connection concern DEF-032 is visible in the provider list but remains separately tracked.
- **Not covered:** other 14 locales, light theme, other viewport widths, mobile Safari/WebKit, keyboard and screen-reader behavior, all other Console screens, and locale formatting of exports/API data.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
