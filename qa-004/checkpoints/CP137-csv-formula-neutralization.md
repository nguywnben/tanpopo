# CP137 — CSV formula neutralization through chargeback export

**Date:** 2026-11-26  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite at `127.0.0.1:18128`  
**Outcome:** PASS for one `=`-prefixed Virtual Key name in CSV, JSON preservation, and cleanup. The remaining contracted prefix cases are UNVERIFIED. No defect assigned.

## Reproduction

Using real Chromium as the synthetic Org Owner in `QA004 Test Root Org / default / dev`, temporarily raised the project hard limit from 100 to 5,000 micro-USD. Issued a disposable Provider-plane Virtual Key named `=HYPERLINK("http://example.invalid","CP137")`, restricted to `/v1/chat/completions`, and sent one cookie-free request to the synthetic `qa004-loopback` adapter. The adapter returned HTTP 200; the exact settled ledger rose from 2,037 to 2,038 micro-USD, estimated usage remained 14, and the request generated no external provider spend.

Exported Virtual Key chargeback as CSV and JSON from FinOps. Both exports contained the fixture row. CSV encoded the cell as `'=HYPERLINK(...)` inside a quoted CSV cell, so the value no longer starts with `=` when opened as a spreadsheet cell. JSON retained the original string, as expected for a non-spreadsheet format. The exports and visually inspected Console state are preserved in the evidence list below.

Revoked the disposable key through Access & Keys. The active fixture showed `Revoked` in the real UI; authenticated API readback confirmed state `REVOKED`, version 2, and no plaintext field. Restored the project limit to 100 micro-USD; authenticated readback confirmed limit 100, exact 2,038, estimated 14, reserved 0, version 167. The failed initial harness attempt used an Admin cookie on the data-plane request and received HTTP 401 `invalid_credential_plane`; this was correctly classified as test-harness credential-plane misuse, not a product defect. That earlier disposable key was also revoked. No secret was retained in evidence.

## Acceptance scope and remaining uncertainty

TP-IMP-0025 requires formula neutralization for names beginning with `=`, `+`, `-`, `@`, tab, or carriage return, and refers to project, key, or model names. Only a Virtual Key name beginning with `=` was exercised against an actual export in this checkpoint. The other five prefixes, project/model labels, spreadsheet applications, import round trips, and non-ASCII/control-character combinations remain UNVERIFIED. Therefore the overall CSV formula-injection contract is not marked fully passed.

No new finding was assigned. The observed successful row was not individually distinguishable by a stable key identifier in chargeback; that separate issue is already DEF-077 and remains open.

## Evidence

- [Sanitized results and export hashes](../evidence/CP137-csv-formula-injection-results.json)
- [Issue form with the disposable formula-like key name](../evidence/CP137-formula-key-create-form.png)
- [Chargeback UI showing the escaped row](../evidence/CP137-chargeback-formula-key-ui.png)
- [Access & Keys UI showing the fixture revoked](../evidence/CP137-formula-key-revoked.png)
- [Temporary budget increase, before the probe](../evidence/CP137-budget-raised.png)
- [Older exploratory key row before revocation](../evidence/CP137-formula-key-before-revoke.png)

Screenshots were opened and visually inspected during the test. Export bytes were hashed before cleanup; hashes and candidate rows are in the sanitized JSON evidence. A one-time-secret screenshot from the first failed harness attempt was deleted immediately and is not included.

Historical status remains 24 scoped PASS, six PARTIAL, one FAIL (DEF-025), and 19 UNVERIFIED. New defects remain 26 FAIL (DEF-051–075 and DEF-077) plus DEF-076 PARTIAL. QA-004 remains incomplete. **GATE-REL-07 remains BLOCKED.**
