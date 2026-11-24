# CP82 — FinOps CSV/JSON export grouping regression

- **Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`; product source unchanged.
- **Runtime/browser:** isolated `127.0.0.1:18084`, Org Owner, real Chromium at 1440×900.
- **Result:** PASS for actual UI CSV and JSON exports under all four supported grouping dimensions. DEF-014/015 remain scoped passes; no new defect.

## Procedure and results

1. Opened FinOps → Chargeback in the real Console. The Project grouping showed one row for `QA002 Audit mwto47wa`; the exact-settled KPI was `$0.012001`.
2. Used the Group by control for Project, Environment, Virtual Key and Model. For each grouping, captured the rendered table, clicked the actual Export CSV and Export JSON buttons, waited for the file downloads, and saved the downloaded content under `evidence/`.
3. All eight export requests returned HTTP 200. CSV responses had `text/csv; charset=utf-8`; JSON responses had `application/json; charset=utf-8`. The downloaded CSV files had the expected six-column header and the JSON files parsed as arrays of objects.
4. Compared rows with the visible table for each grouping. Project produced one group, Environment (`stage`) one group, Virtual Key four groups, and Model four groups. Every grouping reconciled to 12,001 exact micro-USD, 0 estimated micro-USD, five exact requests, zero estimated requests and zero unmetered requests. The totals matched the visible exact-settled KPI.
5. The initial capture runner waited for `group_by=environment`, but the documented API enum is `env`; it timed out before capturing that export. No product failure was inferred. The corrected runner used the documented mappings (`project`, `env`, `key`, `model`) and completed all exports through the UI.
6. Visually inspected the actual Project, Environment, Virtual Key and Model grouping screenshots. No inference or budget mutation occurred.

Evidence:

- `../evidence/CP82-chargeback-project.png`, `.csv`, `.json`
- `../evidence/CP82-chargeback-env.png`, `.csv`, `.json`
- `../evidence/CP82-chargeback-key.png`, `.csv`, `.json`
- `../evidence/CP82-chargeback-model.png`, `.csv`, `.json`
- `../evidence/CP82-chargeback-export-dimensions.json` — sanitized UI text and actual downloaded contents/statuses.
- `../evidence/CP82-chargeback-environment.png` and `.csv` — duplicate first-pass environment capture retained as additional QA evidence.

## Defect and coverage disposition

- **DEF-014:** remains VERIFIED FIXED (scoped). CSV is a valid downloadable CSV for all four grouping dimensions in this run.
- **DEF-015:** remains VERIFIED FIXED (scoped). Each selected dimension controlled both export requests and grouped rows matched the visible table.
- No new finding or state mutation. Locale, pagination, date-range filtering, non-owner roles, empty/large datasets and additional browsers remain UNVERIFIED.

## Consolidated phase status

- **Historical defects:** CP82 does not change other DEF-001–015 dispositions; acceptance coverage remains incomplete overall. DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Open findings:** DEF-016–033 remain confirmed open. QA-OBS-001–014 remain preserved, including unresolved QA-OBS-003. No new defect ID was assigned.
- **Coverage completed:** four supported grouping values; actual CSV/JSON UI downloads; MIME/HTTP checks; row, request-count and exact-total reconciliation; visual inspection.
- **Coverage blocked / remaining:** 18445 A2A plane remains configuration-disabled; webhook delivery lacks usable Operator access and a controlled receiver/signing setup; RC4 artifact verification remains UNVERIFIED. Additional export ranges, roles, locales, pagination and browsers remain open alongside broader regression.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
