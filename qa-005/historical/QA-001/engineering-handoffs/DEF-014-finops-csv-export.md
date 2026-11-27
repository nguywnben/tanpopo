# Engineering handoff — DEF-014: FinOps CSV export parses CSV as JSON

**Severity:** Moderate (operator reporting workflow unavailable)  
**Status:** Confirmed on isolated 8083; JSON export remains functional; no product code changed.

## Reproduction

1. Sign in as Org Owner and open FinOps → Chargeback.
2. Click **Export CSV**.
3. Observe the API response is HTTP 200 with `Content-Type: text/csv` and valid CSV, but no file downloads and the page displays `Unexpected token 'g', "group_key,"... is not valid JSON`.
4. Click **Export JSON** as a control; it downloads one row containing `exact_micro_usd: 1`.

## Evidence

- `../evidence/phase61-export-csv-ui.png`: visually inspected page with parsing error and no download.
- `../evidence/phase61-chargeback-export-csv.json`: HTTP 200 text/csv response body and missing download result.
- `../evidence/phase62-export-json.json` and `phase62-export-json-ui.png`: working JSON export with the same exact row.

## Expected vs actual

- **Expected:** the CSV response is consumed as text/Blob and downloaded as `tanpopo-chargeback.csv`.
- **Actual:** the generated API client attempts JSON parsing on the CSV response. A parsing exception reaches the error banner; no file is downloaded.

## Affected workflow

Operators and finance users exporting chargeback to spreadsheet/ledger tooling cannot complete the CSV reporting path from the Console. The JSON path and direct backend CSV response work.

## Suggested investigation

`web/console/src/api/managementClient.ts` calls `api.GET('/admin/v1/usage/export', ...)` for both formats. Ensure CSV uses a raw/text response path and preserves the CSV content type before constructing the download Blob; retain JSON parsing for JSON output. Add a UI regression covering both export formats.

GATE-REL-07 remains BLOCKED.
