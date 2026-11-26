# CP17–19 — Mobile, localization, and FinOps export regression

**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`  
**Runtime:** QA-005 local Linux amd64 binary, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; SQLite runtime `127.0.0.1:18145`.  
**Environment:** Chromium with real Playwright interactions; synthetic local data only. No external provider calls.

## CP17 — Small viewport inventory

Authenticated as the synthetic owner, set a 375×812 viewport, opened Access & Keys and inspected the actual screenshot. The document width equaled the viewport width (375px); the six inventory rows were displayed as cards without horizontal overflow. This is a scoped PASS for layout overflow only. Screen-reader, touch-target, keyboard, mobile navigation and other screens remain unverified. Evidence: `evidence/screenshots/CP17-mobile-access-keys-375.png` (visually inspected).

## CP18 — Vietnamese locale persistence and consistency

Changed language through the visible locale selector in a real Chromium session. Overview heading, navigation and metric labels rendered in Vietnamese; locale remained selected after hard reload. Navigated to FinOps and inspected the screenshot: primary labels were translated, while terms such as `FinOps`, `PROJECT`, `VIRTUAL_KEY`, and micro-USD terminology remained in English. This is an observed localization consistency/product-contract question, not a defect absent an accepted completeness requirement. Added PG-003. Mobile locale selection remains unverified. Evidence: `evidence/screenshots/CP18-vietnamese-overview.png`, `CP18-vietnamese-after-refresh.png`, and `CP18-vietnamese-finops.png` (the latter two and overview visually inspected).

## CP19 — Chargeback export reconciliation

Using a fresh authenticated browser context, opened FinOps, exported CSV and JSON through their actual buttons, and compared their contents with the on-screen exact-settled total and request count. Both files reported project `Initial Project`, exact settled `1` micro-USD, estimated `0`, exact request count `3`, estimated count `0`, and unmetered count `0`. CSV and JSON values matched. The UI showed exact settled `$0.000001` and three budget rows, consistent with the export aggregation. This is a scoped PASS for populated project-level chargeback export consistency, not for schema compatibility, locale-specific formatting, larger pagination, authorization, data isolation or accounting migrations. Downloaded raw exports remained in `/tmp/qa005-runtime` and are not committed. Evidence: `evidence/screenshots/CP19-finops-exports.png` (visually inspected); raw export identity/content summaries were captured during execution.
