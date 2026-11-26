# CP75 — Independent DEF-014/015 FinOps export retest

- **Baseline:** frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`; no implementation changes.
- **Result:** PASS for the selected Model-grouping CSV/JSON export slice; no new defect.
- **Runtime/browser:** isolated A2A-enabled runtime `127.0.0.1:18084`, real Chromium, synthetic QA owner.

## Work performed

1. Opened FinOps → Chargeback in the selected QA project/environment. Visually inspected the table and exact-settled KPI.
2. Changed the Group by control from Project to Model. The Console issued export requests with `group_by=model` for both CSV and JSON.
3. Downloaded both exports through the actual UI buttons. CSV response was HTTP 200 with `Content-Type: text/csv; charset=utf-8` and a `.csv` download filename. The file had a CSV header and four data rows. JSON contained the corresponding four objects.
4. Compared the displayed model rows, CSV and JSON: model keys and exact/estimated/request counts matched. Exact totals summed to 12,001 micro-USD, matching the visible `$0.012001` exact-settled KPI. Estimated total was zero. No inference, budget mutation, or provider dispatch was performed.

## Evidence

- `../evidence/CP75-finops-initial.png` — inspected Budgets & Holds default state.
- `../evidence/CP75-finops-chargeback.png` — inspected Chargeback Project grouping before selection.
- `../evidence/CP75-finops-chargeback-model.png` — visually inspected Model grouping and four rows.
- `../evidence/CP75-finops-model.csv` — actual UI download; valid CSV.
- `../evidence/CP75-finops-model.json` — actual UI download; valid JSON.
- `../evidence/CP75-csv-headers.json` — CSV MIME, content-disposition, filename, header and row count.
- `../evidence/CP75-finops-export-results.json` — request URLs and sanitized paired export contents.

## Disposition and limits

- The first automation attempt waited for the download before clicking Export CSV and timed out without initiating an export. The rerun attached the download listener, clicked the real button, and captured both files. This was a harness sequencing error, not a product failure.
- **DEF-014:** CSV export remains verified fixed for this group-by-model scenario; response is `text/csv` and file is parseable CSV.
- **DEF-015:** export used `group_by=model`, and the data rows match the visible Model grouping.
- Other group dimensions, date ranges, pagination, locale, non-owner roles, larger/empty datasets, and other browsers remain UNVERIFIED. This scoped PASS does not close those regression requirements.
- `GATE-REL-07` remains **BLOCKED**; QA-002 remains in progress.
