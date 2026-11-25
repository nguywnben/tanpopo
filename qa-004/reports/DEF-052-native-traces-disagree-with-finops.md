# DEF-052 — Successful native traces report exact zero cost while FinOps records nonzero cost

**Severity:** Major  
**Status:** FAIL — reproduced in the same isolated RC6 synthetic Ollama journey.  
**Affected workflow:** Native inference observability, support investigation, FinOps reconciliation, audit and chargeback.

## Reproduction

1. On the isolated test-root runtime described in DEF-051, call one successful non-stream `POST /native/ollama/api/chat` request. The adapter falls back to estimated token usage and records one micro-USD in the settlement.
2. Call one successful streamed `POST /native/ollama/v1/chat/completions` request. The synthetic upstream ends the SSE stream with exact usage 7 input / 4 output; the persisted settlement records exact usage and one micro-USD.
3. Fetch `/admin/v1/traces?model=qa004-loopback&limit=20`, inspect both records, then inspect the Console Traces and FinOps screens.

## Expected / actual

Expected: each trace's usage accuracy and cost match its durable settlement, and aggregates reconcile to those same rows.

Actual: both trace records report `usage_accuracy=EXACT` and `total_cost_micros=0`. The SQLite settlement rows report respectively `ESTIMATED`, 45/45 tokens, 1 micro-USD and `EXACT`, 7/4 tokens, 1 micro-USD. FinOps shows `$0.000001` exact settled and `$0.000001` estimated settled. The Traces UI labels both rows “Exact.” Thus one successful trace is mislabeled and both trace costs disagree with ledger and aggregate.

## Evidence

- `../evidence/CP07-native-ollama-first.json` — response identifies the first synchronous call and `ESTIMATED` header.
- `../evidence/CP07-native-ollama-stream.json` and `../evidence/CP07-native-ollama-stream-state.json` — actual SSE response and trace records.
- `../evidence/CP07-native-ollama-settlements.json` — authoritative persisted settlement values.
- `../evidence/CP07-native-ollama-persistent-usage.json` — trace and FinOps API readback.
- `../evidence/CP07-native-traces-after-stream.png`, `../evidence/CP07-native-finops-after-stream.png` — actual browser screenshots, visually inspected.

## Business impact

Operators cannot rely on traces to reconcile customer charges or investigate requests. The UI asserts “Exact” for estimated usage, and request-level zero cost cannot be reconciled to successful nonzero settlements and summary totals.

## Root-cause hypothesis (not confirmed)

The trace projection may read a separate/empty usage field, default accuracy to exact, and omit or fail to join settlement cost. Confirm projection and aggregation data sources; no source modification was made during QA.

## Acceptance criteria

- Trace accuracy, effective usage, and cost are sourced from or reconciled with the durable settlement for the same trace/request identifier.
- Successful nonzero settlements never appear as zero in trace detail; estimated rows remain marked estimated.
- Traces UI, API, FinOps summary/export, and persisted ledger reconcile per request and in aggregate.
- Regression covers estimated and exact native requests, streaming, provider-plane requests, failures, retries, and missing settlement rows.
