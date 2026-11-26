# DEF-052 — Request trace accuracy/cost diverges from durable settlement and FinOps

**Severity:** Major
**Status:** FAIL — reproduced across native success, provider-plane success, server-side partial-stream failure, and browser-initiated client cancellation in isolated RC6 synthetic journeys.
**Affected workflow:** Inference observability, support investigation, partial-stream diagnosis, FinOps reconciliation, audit and chargeback.

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

### Independent provider-plane retest (CP30)

A recovered local Ollama connection and newly activated deployment served one cookie-free `/v1/chat/completions` request through a temporary route. The upstream returned exact 7/4 usage; the real Traces UI waterfall displayed `Exact` and `$0.000000`. A separate FinOps `group_by=key` read for the same key reported one exact request and `exact_micro_usd=1`. The trace is request `trc_01m89hpm7ye0081h0g1g1bxnfq`; the real UI trace list and waterfall were visually inspected. This independently reproduces the trace-versus-settlement mismatch on a provider-plane route. See `evidence/CP30-provider-recovery-results.json` and `evidence/CP30-recovery-trace-inspect.png`.


### Independent native Generate retest (CP38)

A cookie-free native `POST /native/ollama/api/generate` returned HTTP 200 with exact provider counters, but its trace (`trc_01m89nyjake009aa209mbs3035`) showed `EXACT` / `$0` while the same key's FinOps row showed `ESTIMATED` / `$0.000001`. The real Traces and grouped Chargeback screenshots were visually inspected. This independently reproduces the projection mismatch on an additional native Ollama endpoint. See `evidence/CP38-native-generate-results.json`, `evidence/CP38-native-generate-trace.png`, and `evidence/CP38-native-generate-finops.png`.

### Independent client-cancellation retest (CP36)

A real browser issued a cookie-free streaming request through the existing single-target provider route, received the first SSE chunk, then called `AbortController.abort()`. The loopback fixture observed the disconnect after two upstream chunks and stopped the remaining stream. Tanpopo persisted trace `trc_01m89nfp99e00bbbk7n8ze6gsm` with HTTP 200, `mid_stream_abort`, `usage_accuracy=EXACT`, and `$0`; Chargeback for the same test Virtual Key reports 0 exact and 1 estimated request, with `$0.000001` estimated. Traces and Chargeback screenshots were visually inspected. This independently reproduces DEF-052 for a client-triggered cancellation, separate from the provider-side truncation in CP34. See `evidence/CP36-client-cancel-results.json`, `evidence/CP36-client-cancel-trace.png`, and `evidence/CP36-client-cancel-finops.png`.

### Independent partial-stream failure retest (CP34)

A two-target provider route sent a cookie-free streaming request to a local primary that emitted one SSE content chunk and then closed without usage or `[DONE]`. Tanpopo returned HTTP 200 with the partial chunk followed by a canonical `mid_stream_abort` error event and correctly made zero calls to the healthy secondary. The same request trace `trc_01m89m26w4e009j53p9m01tmav` reports `usage_accuracy=EXACT`, `total_cost_micros=0`; FinOps group-by-key reports 0 exact requests and 1 estimated request at 1 micro-USD. The real Traces and Chargeback screenshots were visually inspected. This broadens DEF-052 to failed partial streams while confirming the durable settlement is estimated. See `evidence/CP34-midstream-abort-results.json`, `evidence/CP34-midstream-trace.png`, and `evidence/CP34-finops-midstream.png`.

### Native Ollama tool-call and streaming retest (CP85/CP86)

CP85 native /api/chat tool-call response created a trace marked EXACT / $0, while the same key FinOps row showed 0 exact / 1 estimated request at $0.000001. CP86 repeated the mismatch across five native streaming requests: every trace was EXACT / $0, while Virtual Key Chargeback showed 0 exact / 5 estimated requests at $0.000005. The trace and FinOps screens were visually inspected. See CP85 and CP86 checkpoint and evidence links. These extend DEF-052; no duplicate ID is assigned.

CP87 /native/ollama/api/generate streaming again produced an EXACT/$0 trace while the same Virtual Key FinOps row showed 0 exact / 1 estimated request at $0.000001. This further extends DEF-052. See CP87 checkpoint and evidence.

## Business impact

Operators cannot rely on traces to reconcile customer charges or investigate requests. The UI asserts “Exact” for estimated usage, and request-level zero cost cannot be reconciled to successful nonzero settlements and summary totals.

## Root-cause hypothesis (not confirmed)

The trace projection may read a separate/empty usage field, default accuracy to exact, and omit or fail to join settlement cost. Confirm projection and aggregation data sources; no source modification was made during QA.

## Acceptance criteria

- Trace accuracy, effective usage, and cost are sourced from or reconciled with the durable settlement for the same trace/request identifier.
- Nonzero or partial-stream settlements never appear as zero in trace detail; estimated and partial rows remain marked estimated.
- Traces UI, API, FinOps summary/export, and persisted ledger reconcile per request and in aggregate.
- Regression covers estimated and exact native requests, streaming success and partial failure, provider-plane requests, errors, retries, and missing settlement rows.


CP96 independently reproduced the cross-screen mismatch during an idempotency lifecycle. Persistent Traces contains five rows for the key-A first dispatch, replay, payload-mismatch denial, later replay, and key-B first dispatch; all are Exact/$0, including cache-replay/error rows. Virtual Key Chargeback records one Estimated request / 1 micro-USD for each of the two keys, matching only the two actual fixture dispatches. This confirms no replay double charge but extends DEF-052 to replay and rejected-payload traces. The real Traces and FinOps Chargeback screenshots were visually inspected; see CP96 evidence.

CP97 independently exercised the unified `/v1/responses` API with two route-limited VKEYs. Each key produced one actual synthetic upstream dispatch, an identical-body 200 replay without egress, and a changed-payload 422 without egress; both keys were revoked. Persistent readback labels all six traces Exact/$0, including cached replays and rejected payloads, while Chargeback records one exact 1-micro-USD settlement per key. The real Traces rows and Chargeback group rows were visually inspected in [CP97](../checkpoints/CP97-responses-api-idempotency-and-settlement.md) and evidence `CP97-responses-traces-ui.png`, `CP97-responses-finops-ui.png`, and `CP97-responses-persistent-readback.json`. This further confirms the defect on `/v1/responses`; it does not add a defect ID.

CP98 independently completed a successful unified `/v1/responses` SSE lifecycle through the healthy isolated RC6 runtime. The deterministic stream reported 7 input and 4 output tokens. Its persisted trace says Exact/$0; the same key's FinOps Chargeback reports one exact request / $0.000001 for the sole fixture dispatch. Traces and Chargeback screenshots were visually inspected; see [CP98](../checkpoints/CP98-responses-api-streaming.md) and `CP98-responses-stream-success-results.json`, `CP98-responses-stream-persistent-readback.json`, `CP98-responses-stream-traces-ui.png`, and `CP98-responses-stream-finops-ui.png`. This further confirms DEF-052 for streaming Responses. No new defect ID is assigned. The response omitted `X-Tanpopo-Usage-Accuracy`; since OpenAPI does not mark the listed response header required and streaming usage is finalized in the terminal event, its contract status remains unverified.

### OpenAI Node SDK retest (CP102)

The official OpenAI Node SDK `7.64.0` completed synchronous and streamed `/v1/chat/completions` against the healthy local route. Synchronous usage was 7 input / 4 output tokens; a standards-correct synthetic SSE stream with `stream_options.include_usage=true` returned the text, `finish_reason=stop`, and 7/4/11 usage through the SDK. Each trace reports Exact/$0 while Chargeback records $0.000001 per actual dispatch. The actual Traces and Chargeback screens were visually inspected. This extends DEF-052 through the documented standard OpenAI-compatible route and an official SDK; no duplicate defect was created. See [CP102](../checkpoints/CP102-openai-node-sdk-chat-completions.md), `CP102-openai-sdk-results.json`, `CP102-openai-sdk-stream-results.json`, and the associated Traces/FinOps screenshots.

CP103 repeated the synchronous and standards-correct SSE lifecycle using the documented official OpenAI Python SDK `3.59.0`, installed under `/tmp` only. The SDK parsed the same 7/4 synchronous usage and the streamed `finish_reason=stop` plus opt-in 7/4/11 usage. The two persistent request traces still show Exact/$0 while Chargeback records two exact requests totaling $0.000002. The real Traces and Chargeback screenshots were visually inspected. This further confirms DEF-052 across a separate official SDK implementation; no additional defect ID was assigned. See [CP103](../checkpoints/CP103-openai-python-sdk-chat-completions.md), `CP103-openai-python-sdk-results.json`, and the accompanying readback/screenshots.


### Native multimodal image retest (CP119)

The successful instrumented `/native/ollama/api/chat` image request has a persistent trace marked `EXACT` with `$0`. The real Chargeback screen grouped by each temporary VKEY instead showed zero Exact and one Estimated request, at `$0` because the synthetic self-hosted deployment was zero-rate. Thus accuracy disagrees across the same native image workflow, further reproducing DEF-052. The second trace is from the earlier fixture misconfiguration attempt; it is not used to claim verified image forwarding. See [CP119](../checkpoints/CP119-ollama-native-multimodal-image.md), `../evidence/CP119-vision-traces.png`, and `../evidence/CP119-vision-chargeback-vkey.png`.
