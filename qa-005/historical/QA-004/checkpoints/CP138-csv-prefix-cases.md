# CP138 — CSV neutralization for `+`, `-`, and `@` Virtual Key names

**Date:** 2026-11-26  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite at `127.0.0.1:18128`  
**Outcome:** PASS for three additional Virtual Key name prefixes in CSV. The tab, carriage return, Project/Model, and spreadsheet-application cases remain UNVERIFIED. No defect assigned.

## Reproduction and results

Using real Chromium as the synthetic Org Owner in `QA004 Test Root Org / default / dev`, temporarily raised the project hard limit from 100 to 5,000 micro-USD. Created three disposable Provider-plane keys named `+SUM(1,1)`, `-1+1`, and `@SUM(1,1)`, each restricted to `/v1/chat/completions`. Sent one cookie-free `/v1/chat/completions` request per key to the synthetic `qa004-loopback` model. All three returned HTTP 200. Each produced one persisted exact request and one micro-USD; the exact ledger rose from 2,038 to 2,041 micro-USD, estimated usage stayed 14, and external provider spend was zero.

Exported Virtual Key chargeback as CSV and JSON. All three rows appeared in both. CSV prefixed each formula-like cell with an apostrophe. The exact candidate lines and CSV/JSON SHA-256 hashes are recorded in the sanitized evidence. JSON preserved the raw names as ordinary strings. The UI screenshot shows all three persisted Chargeback rows.

Revoked all three keys through Access & Keys. Real UI readback showed their states as Revoked. Restored the project hard limit to 100 micro-USD; authenticated readback confirmed limit 100, exact 2,041, estimated 14, reserved 0. One results-capture script stopped on a JavaScript regular-expression error after the requests/export UI capture; this was a harness parsing error. The recovery script re-exported both formats, performed all revocations, restored the budget, and wrote the final sanitized evidence. No test key or elevated limit remains from this run.

## Scope limits

Together with CP137, the `=`, `+`, `-`, and `@` key-name cases are PASS. Leading tab and carriage return, Project/Model names, import/open behavior in spreadsheet applications, and mixed Unicode/control-character cases remain UNVERIFIED. No defect was assigned. DEF-077 remains open because the exported Virtual Key grouping lacks a stable identifier for repeated names.

## Evidence

- [Sanitized export rows, hashes, keys, and restored budget](../evidence/CP138-csv-prefix-results.json)
- [FinOps Chargeback UI showing `+`, `-`, and `@` rows](../evidence/CP138-prefix-chargeback.png)
- [Access & Keys UI after fixture revocation](../evidence/CP138-prefix-keys-revoked.png)

The screenshots were opened and visually inspected. No secrets are present in the evidence. Historical status remains 24 scoped PASS, six PARTIAL, one FAIL (DEF-025), and 19 UNVERIFIED. New defects remain 26 FAIL (DEF-051–075 and DEF-077) plus DEF-076 PARTIAL. QA-004 remains incomplete. **GATE-REL-07 remains BLOCKED.**
