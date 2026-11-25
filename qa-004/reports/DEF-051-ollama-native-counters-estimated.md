# DEF-051 — Native Ollama token counters are ignored during settlement

**Severity:** Major  
**Status:** FAIL — reproduced on frozen RC6 in an isolated synthetic provider journey.  
**Affected workflow:** Ollama native `/api/chat` and `/api/generate` inference, usage settlement, billing and budgets.

## Reproduction

1. On a fresh isolated RC6 runtime, use the test-only ephemeral signature root to install the bundled Ollama adapter. The temporary root is not a Tanpopo production release key and no production signing claim is made.
2. Create a loopback-only synthetic Ollama-compatible upstream, validate it through the real Providers probe, register a logical model, create a healthy deployment and route, and issue a PROVIDER-plane Virtual Key.
3. With a cookie-free client, call `POST /native/ollama/api/chat` for `qa004-loopback`, `stream:false`. The mock returns valid Ollama JSON with `prompt_eval_count:6` and `eval_count:3`.
4. Read the persisted `usage_settlements` row and compare it with the response, Traces, and FinOps.

## Expected / actual

Expected: the documented Ollama counters are recognized as exact usage (6 input, 3 output), and the settled amount is computed from those counters and the configured price card.

Actual: the response is HTTP 200 and faithfully preserves the Ollama JSON; its `X-Tanpopo-Usage-Accuracy` header is `ESTIMATED`. The persisted settlement is 45 input / 45 output tokens and `ESTIMATED`, which is the fallback byte estimate. The exact counters returned by the upstream are ignored.

## Evidence

- `../evidence/CP07-native-ollama-first.json` — exact request/response, response headers, IDs and response digest.
- `../evidence/CP07-native-ollama-settlements.json` — persisted settlement rows (sanitized).
- `../evidence/CP07-native-traces-after-stream.png` and `../evidence/CP07-native-finops-after-stream.png` — visually inspected real Console views; these also reveal DEF-052.
- Test fixture request log is held outside the handoff repository; it records `/api/chat` and no upstream authorization value.

## Independent native Generate retest (CP38)

A second authorized cookie-free native dialect journey called `POST /native/ollama/api/generate` on the same frozen RC6 baseline. The fixture returned `prompt_eval_count:6` and `eval_count:3`; Tanpopo preserved both fields in the HTTP 200 Ollama JSON response but marked the response `ESTIMATED`. FinOps shows one estimated request at `$0.000001`. This confirms the defect extends beyond `/api/chat` to `/api/generate`; no new defect ID is assigned. See `../evidence/CP38-native-generate-results.json`, `../evidence/CP38-native-generate-trace.png`, and `../evidence/CP38-native-generate-finops.png`.

## Business impact

Usage-based billing, budget consumption, chargeback, and audit figures can materially diverge from provider-reported Ollama usage while the inference itself succeeds. Byte estimates can understate or overstate token counts and may lead to incorrect customer charges or budget decisions.

## Root-cause hypothesis (not confirmed)

The Ollama manifest's usage extraction may only map OpenAI-style `usage.prompt_tokens` and `usage.completion_tokens`, without mapping `prompt_eval_count` and `eval_count`. Confirm against the adapter contract and all supported native dialects.

## Acceptance criteria

- Ollama `prompt_eval_count` and `eval_count` are persisted as exact usage for successful non-stream and stream responses when present and valid.
- Missing, malformed, overflow, and conflicting counters are handled explicitly and reported as estimated/unmetered according to the published policy; never silently labeled exact.
- Response headers, traces, settlement rows, and FinOps aggregates agree on usage accuracy and token counts.
- Regression covers zero counters, stream terminal usage, interrupted streams, provider errors, and counter overflow.
