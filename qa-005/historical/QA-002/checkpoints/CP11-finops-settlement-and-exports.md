# CP11 — Exact usage with no budget ledger; FinOps summary and exports

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Environment:** Isolated SQLite runtime `127.0.0.1:18084`; synthetic Ollama-compatible local HTTP fixture; real Console setup and provider Data Plane request. No Tanpopo implementation changed.

## Controlled scenario

Using the active QA Ollama connection, created logical model `qa002-ollama-model`, active healthy deployment `qa002:latest` with integer input/output rates, and route `qa002-ollama-route` through the real Console forms. A synthetic local upstream returned a successful OpenAI-compatible response with 13 prompt and 10 completion tokens. A provider-plane virtual key sent an actual `POST /v1/chat/completions`; gateway returned HTTP 200 and normalized content/usage. The settled usage row is `EXACT`, `SELF_HOSTED_INFRA`, 13 input + 10 output tokens, total `1` micro-USD. Read-only SQLite showed zero `budget_ledgers` and one provider settlement in addition to the earlier A2A settlements. Sanitized response: `../evidence/CP11-inference-response.json`.

## DEF-012 — VERIFIED FIXED (scoped)

FinOps → Chargeback at Project grouping showed Exact settled `$0.006001` and the project row also `$0.006001` (6,001 micros total: 6,000 from two earlier completed A2A tasks and 1 from this provider request). Estimated `$0`, active holds `$0`, and unmetered requests 0. This occurred with zero budget ledger rows, reproducing the historical no-budget condition while proving the headline is sourced consistently with the chargeback summary. SQLite independently corroborated the 1-micro exact provider settlement and absence of budget ledgers. Screenshot: `../evidence/CP11-finops-project-summary.png`; model-group screenshot: `CP11-finops-model-summary.png`.

## DEF-014 — VERIFIED FIXED (scoped)

Clicked the real Console Export CSV control. Browser observed HTTP 200 `text/csv; charset=utf-8`, received an actual download, and the UI showed “Exported CSV chargeback report.” The saved file has a CSV header and parseable rows; no JSON parse error appeared. The JSON control also produced an actual download and success notice. Evidence: `CP11-finops-csv-success.png`, `CP11-finops-json-success.png`, and downloaded `CP11-finops-csv.csv` / `CP11-finops-json.json`.

## DEF-015 — VERIFIED FIXED (scoped)

Changed Group by from Project to Model. The live table showed `qa002-local-agent` at 6,000 micros and `qa002-ollama-model` at 1 micro. Network-observed summary and both export requests used `group_by=model`; the CSV and JSON contents contain the same model group keys and exact amounts as the visible table. `CP11-finops-csv.csv` and `CP11-finops-json.json` are the raw downloaded, non-sensitive QA outputs.

## Limits and follow-up

Only one project and one environment are present, so multiple-tenant/project aggregation and all Environment/Virtual Key grouping dimensions were not tested. Budget enforcement, quota-exhaustion, warning thresholds, estimated/unmetered usage and holds remain UNVERIFIED. Visual screenshots were inspected and actual UI clicks/downloads were used; this is not a source-only or static result.

GATE-REL-07 remains **BLOCKED**. QA-002 continues.
