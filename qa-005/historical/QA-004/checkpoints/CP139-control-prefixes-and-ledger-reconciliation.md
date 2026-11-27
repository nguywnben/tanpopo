# CP139 — control-prefix normalization and budget/FinOps reconciliation

**Date:** 2026-11-26  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite at `127.0.0.1:18128`  
**Outcome:** CSV security cases are PASS only after observing that leading tab/CR are normalized before persistence; those raw-prefix cases remain UNVERIFIED. A persistent project budget-versus-FinOps mismatch is confirmed as DEF-078 Moderate FAIL.

## Control-character input and export journey

Using real Chromium as the synthetic Org Owner in `default / dev`, raised the project hard limit from 100 to 5,000 micro-USD. Created two disposable keys through Access & Keys, named with a leading tab plus `=CP139-tab` and a leading carriage return plus `=CP139-cr`. The real form accepted each input, but readback normalized the leading controls: tab was stripped by the service, and the browser transformed carriage return to a leading space that the service trimmed. The API returned persisted names beginning with `=`, not tab/CR. Each key completed one cookie-free loopback chat request, HTTP 200; the project exact budget increased from 2,041 to 2,043 micro-USD and estimated remained 14.

Actual CSV and JSON exports contained both normalized names. The CSV bytes prefixed both formula-like values with an apostrophe; JSON returned the normalized strings unchanged. Since neither control prefix survived storage, this does **not** validate the sanitizer against a persisted leading tab or CR. The two keys were revoked in the real UI and the project hard limit restored to 100 micro-USD. No provider spend occurred.

## DEF-078 — budget and usage summary diverge

After cleanup, queried the current monthly default-project budget and project usage summary as the same Owner. The budget API and FinOps Budgets UI showed 2,043 exact / 14 estimated micro-USD. The usage summary and real Chargeback UI showed 2,044 exact / 15 estimated. The same mismatch remained when the usage query was restricted to `from=2026-11-01T00:00:00Z`. CP136's historical summary and CP137's pre-request budget readback corroborate the pre-existing offset. This is DEF-078, Moderate FAIL; details, expected criteria and uncertainty about root cause are documented in the standalone report.

Visual inspection confirmed the control-normalized Chargeback rows and both conflicting totals. Current sanitized API values are in the evidence JSON. One QA capture harness elsewhere in the run failed on a JavaScript regex and was recovered in CP138; no product requests failed in that harness incident.

## Evidence

- [Control-prefix request/export results](../evidence/CP139-control-prefix-results.json)
- [FinOps budget and summary readbacks](../evidence/CP139-finops-budget-reconciliation-results.json)
- [Chargeback UI screenshot](../evidence/CP139-control-chargeback.png)
- [Budget UI screenshot](../evidence/CP139-budget-vs-usage.png)
- [Tab-prefixed key form](../evidence/CP139-tab-create.png)
- [Carriage-return-prefixed key form](../evidence/CP139-cr-create.png)
- [DEF-078 report](../reports/DEF-078-finops-summary-exceeds-monthly-budget-ledger.md)

## Coverage and status

For formula handling, `=`, `+`, `-`, and `@` Virtual Key names are PASS in actual CSV exports (CP137/CP138). Leading tab/CR are normalized before the exported value exists; sanitizer behavior on raw stored tab/CR remains UNVERIFIED. Project/Model names and spreadsheet-app opening remain UNVERIFIED.

Historical status remains 24 scoped PASS, six PARTIAL, one FAIL (DEF-025), and 19 UNVERIFIED. New findings are 27 FAIL (DEF-051–075 and DEF-077–078) plus DEF-076 PARTIAL. QA-004 remains incomplete. **GATE-REL-07 remains BLOCKED.**
