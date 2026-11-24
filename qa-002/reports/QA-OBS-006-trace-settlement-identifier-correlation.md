# QA-OBS-006 — Inference trace and exact settlement use different request identifiers

**Classification:** unresolved data-contract and audit-correlation observation; not a confirmed defect.
**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`.
**Environment:** disposable SQLite runtime, real Console-issued synthetic Provider key, local Ollama-compatible fixture.

## Reproduction

1. In the Console, issue a Provider-plane key scoped to the disposable test environment.
2. Send one streaming `/v1/chat/completions` request using a configured synthetic route and model.
3. Inspect the HTTP response, then read only the corresponding trace and usage-settlement metadata from the isolated SQLite database by Virtual Key ID and timestamp.

## Observed evidence

- Data-plane response was HTTP 200 with valid SSE and synthetic completion. `evidence/CP35-key-live-inference.json` records the sanitized response.
- Trace row: `trc_01m85eb8nre00aqm8awr540zxc`, route `qa002-cp22-streamroute`, status 200, test key `vky_01m85e9trde00ab28c9p9a7mqw`, timestamp `1795479151308` ms.
- Settlement row nine milliseconds earlier: request ID `trc_01m85eb8nse00b8bwess9qdpfg`, same Virtual Key, Provider plane, model `qa002-cp22-streaming`, 13 input tokens, 10 output tokens, EXACT usage and one micro-USD.
- `evidence/CP35-key-persistent-state.json` contains the sanitized state query. No credential or prompt is persisted in the report.
- CP22 previously recorded the same broad behavior: separately generated trace and settlement identifiers and a trace detail total of zero beside a one-micro settlement.

## Expected question and actual behavior

**Contract question:** are `request_traces_meta.request_id` and `usage_settlements.request_id` intended to identify and join the same inference attempt, or are they independently scoped identifiers with another documented correlation field?

**Actual:** both rows represent the same key and successful synthetic route/model use at matching timestamps, but their IDs differ. CP35 does not establish a lost settlement or incorrect aggregate; the FinOps amount is consistent with the exact settlement. Without the identifier contract, a product defect is not asserted.

## Potential impact and follow-up

If these identifiers are expected to join, operators may not be able to reconcile an individual trace to its exact charge. Confirm the intended join with the data/audit contract, then verify UI/API exports use that relation consistently. This observation overlaps the general request/trace correlation risks documented in DEF-017 but is not evidence that DEF-017's Playground lookup cause is identical.
