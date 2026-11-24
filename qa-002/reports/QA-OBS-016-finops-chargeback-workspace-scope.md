# QA-OBS-016 — Chargeback scope is unclear under a selected project

**Classification:** UX/business-contract observation; no defect assigned.  
**First observed:** CP87, frozen RC4 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Account/context:** Chromium, QA-002 A2A organization owner; workspace selector displayed `QA-002 A2A Org / QA002 CP78 Fresh Budget / No env`.

## Reproduction

1. Sign in as the organization owner in the isolated RC4 runtime.
2. Select project `QA002 CP78 Fresh Budget` with no environment.
3. Open **FinOps → Chargeback**.
4. Observe the selected-project workspace header and report table.
5. Read the chargeback API with `group_by=project`, then compare a valid recent window, an invalid date, a reversed window, and a `project_id` for the selected project.

## Actual evidence

- The UI showed exact settled `$0.012001`, estimated `$0`, active holds `$0`, and five exact requests for project `QA002 Audit mwto47wa`, although the active project selector showed `QA002 CP78 Fresh Budget`.
- `GET /admin/v1/usage/summary?group_by=project` returned the matching organization-wide row (12,001 micros; five exact requests).
- Adding the selected project's `project_id` did not narrow the result: the same organization-wide row was returned. The handler scopes this report by organization; the API response and rendered row agree.
- A valid one-hour RFC3339 range returned HTTP 200 with no rows. Malformed and reversed ranges returned HTTP 400 `invalid_request`.
- The inspected Chargeback screen had no visible date/from/to control. This is not classified as a product defect: the feature catalog (FT-13) promises multi-dimensional chargeback reports, but does not specify a date-range control or selected-project filtering contract.

## Why record this

When an operator selects one project in the global workspace selector, the organization-wide chargeback report can show other projects without an explicit scope label. The data is internally consistent, but the relationship between global selection and report scope is ambiguous. Confirm intended scope and label it in the UI/API contract; no request is made to weaken authorization or force project scoping.

## Evidence

- `evidence/qa002-cp87-chargeback-initial.png` — initial screen, visually inspected.
- `evidence/qa002-cp87-chargeback-api-windows.png` — selected project and organization-wide row, visually inspected.
- `evidence/CP87-finops-chargeback-scope-and-date-windows.json` — sanitized API and UI observations.

## Status

Observation only. No data mutation was performed. Organization-wide API behavior and rendered report matched. Project-specific filtering contract and date-range UI support remain **UNVERIFIED**.

## CP91 cross-screen expansion

In a separate real Chromium owner session, the workspace selector was set to `QA002 CP78 Fresh Budget / No env`. The project-environment API confirmed this project had no environments; `QA002 Audit mwto47wa` had `stage`. Both rows returned by `GET /admin/v1/traces?limit=100` referenced the Audit project’s stage environment, and the same two requests matched the 24-hour Overview API/UI totals (two requests, 50% errors, 12,001 exact micros). The organization-wide views agree with each other and the Org Owner’s organization-level authorization, so this is not classified as a defect. The prominent project selector is not accompanied by an explicit organization-wide scope label on Overview or Traces. See `checkpoints/CP91-trace-and-overview-scope-under-project-selection.md` and its sanitized JSON/screenshots.
