# CP17 — Localization and preference persistence

**Outcome:** PARTIAL. Every one of the 15 locale choices rendered a translated Overview in a real Chromium session. Vietnamese remained selected across FinOps navigation and browser refresh. No product defect was confirmed in this scope. GATE-REL-07 remains BLOCKED.

## User interactions and evidence

1. Signed in as the isolated project-scoped Developer and opened the language selector in the authenticated Console.
2. Selected each displayed option: Simplified Chinese, Traditional Chinese, German, English, Spanish, French, Indonesian, Italian, Japanese, Korean, Portuguese, Russian, Thai, Turkish, and Vietnamese.
3. For each selected locale, recorded the rendered Overview text and a screenshot in `evidence/CP17-locale-results.json` and `evidence/CP17-locale-*.png`. The localized page included the Overview heading, offline-mode notice, navigation, request metrics, and refresh action. Representative screenshots from Chinese, French, Japanese, Korean, Russian, Indonesian, Portuguese, Thai, Turkish, and Vietnamese were visually inspected; all showed legible text and usable layout at 1360×900.
4. Selected Vietnamese, navigated to FinOps → Chargeback, and refreshed. The language selector still displayed `Tiếng Việt`, and translated Chargeback controls and data remained rendered. `evidence/CP17-vietnamese-chargeback.png` is the inspected screen; before/after text is in the JSON record.

## Coverage limits

This establishes locale selection and persistence on the Overview plus a Vietnamese FinOps Chargeback slice. It does not validate professional translation quality, pluralization across all copy, long form labels, validation and API errors, every product screen, dates/number formatting across currencies and time zones, localized exports, keyboard access, or role-specific hidden/denied states in each locale. These remain UNVERIFIED. No defect is assigned from this sample.
