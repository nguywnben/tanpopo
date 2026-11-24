# CP14 — FinOps export formats and selected dimensions

## Scope and result

Independent retest on frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`, using the isolated A2A-capable SQLite clone on `127.0.0.1:18107` and a synthetic settled ledger. No source was changed and no external provider was called. `GATE-REL-07` remains **BLOCKED**.

## Real Console journey

Authenticated as the synthetic organization owner in Chromium desktop (1440×1000), opened FinOps → Chargeback, and verified populated exact settled usage: `$0.002468`, two exact requests. Selected each available group: Project, Environment, Virtual Key, and Model. The table visibly updated to `default`, `dev`, `QA003 A2A lifecycle synthetic 2`, and `qa003-input-required-lifecycle`, respectively.

For each selected dimension, clicked **Export CSV** and **Export JSON**, awaited the real browser download, saved the file, and inspected the actual HTTP response and file. Every request carried the selected `group_by` value, returned HTTP 200, and the correct MIME type (`text/csv; charset=utf-8` or `application/json; charset=utf-8`). CSV files had the expected header and row; JSON files parsed as arrays with matching `group_key`. All eight downloads retained `exact_micro_usd: 2468`, `exact_request_count: 2`, zero estimated and unmetered amounts, consistent with the displayed table and totals. Evidence contains downloaded sanitized files, response details, and the full matrix at [CP14 export evidence](../evidence/CP14-chargeback-export-matrix.json); four [screenshots](../evidence/CP14-chargeback-project.png) ([environment](../evidence/CP14-chargeback-env.png), [key](../evidence/CP14-chargeback-key.png), [model](../evidence/CP14-chargeback-model.png)) were visually inspected. CSV downloads were usable browser downloads, not JSON parse attempts.

## Historical defect disposition

- **DEF-014 — VERIFIED FIXED for tested CSV/JSON exports.** The historical CSV path no longer attempts JSON parsing: both real downloads completed with the expected content type, filename, headers, and populated row. Other date ranges, locales, roles, pagination and larger multi-row exports remain open.
- **DEF-015 — VERIFIED FIXED for Project, Environment, Virtual Key, and Model groupings.** The selected UI dimension was sent in both formats and each exported key matched the corresponding visible table key. The one-record-per-dimension fixture does not prove multi-group ordering, pagination, or every filter combination.
- No additional defect confirmed in this phase. The historical registers describe scoped verification and do not imply blanket closure.

## Coverage update

FT-13, SCR-12, and WF-08 advance to **PARTIAL** with populated settlement and export evidence. Reservations, budget enforcement, period/date filters, custom ranges, locale formatting, role permissions, multi-row reconciliation, pagination, and other backends remain unverified. No tests were added or product files modified. External provider spend remains USD 0.

