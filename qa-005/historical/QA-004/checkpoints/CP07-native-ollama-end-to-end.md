# CP07 — Native Ollama JSON/SSE end-to-end inference and persistence

**Outcome:** PARTIAL; two successful inference paths were exercised. Three new confirmed defects (DEF-051–053) were recorded. This does not constitute release acceptance.

## Journey

On the isolated RC6 test-root runtime and loopback mock established in CP05–06, a fresh Owner used the real Console and APIs to create a PROVIDER-plane Virtual Key. A cookie-free client then called the actual native Data Plane. The mock had no provider key and logged only request paths and safe metadata.

### Non-stream native Ollama JSON

`POST /native/ollama/api/chat` with `model=qa004-loopback`, a user message, `stream:false`, `keep_alive`, and sampling/context options returned HTTP 200. The response was valid Ollama-native JSON with exact upstream counters `prompt_eval_count=6`, `eval_count=3`, retained body digest `fe72d046…95fd`, and distinct request/trace IDs. Response header reported `ESTIMATED`, while durable settlement persisted fallback 45/45 estimated tokens and one micro-USD. The exact counters were not consumed: DEF-051.

### Native SSE path

`POST /native/ollama/v1/chat/completions` with `stream:true` and `stream_options.include_usage=true` returned HTTP 200 and SSE content. Chunks preserved the synthetic response text, final usage 7 input / 4 output, and `[DONE]`. The adapter received no upstream authorization secret. The persisted settlement shows exact 7/4 tokens and one micro-USD. The stream response had no `X-Tanpopo-Usage-Accuracy` header; this is recorded as an observation for further contract review, not a defect disposition yet.

### Cross-screen and durable-state check

After both calls, the trace API returned two successful NATIVE rows marked `EXACT`, each with cost 0. FinOps reported one exact and one estimated request, each settled at one micro-USD. Direct read-only SQLite inspection confirmed settlement rows: (estimated, 45/45, 1 micro-USD) and (exact, 7/4, 1 micro-USD). The real Traces UI visibly labeled both rows “Exact”; FinOps visibly showed `$0.000001` for exact and estimated totals. This mismatch is DEF-052.

At configuration values 1000 and 2000 displayed/named as nano-USD per token, exact 7/4 usage represents 15,000 nano-USD or 15 micro-USD under the published contract. Actual is 1 micro-USD. The calculator uses per-million field names and denominator, conflicting with documentation/UI/API field names: DEF-053.

## Evidence and visual review

- Calls/responses: `evidence/CP07-native-ollama-first.json`, `evidence/CP07-native-ollama-stream.json`.
- Trace/aggregate readback: `evidence/CP07-native-ollama-persistent-usage.json`, `evidence/CP07-native-ollama-stream-state.json`.
- Durable settlement rows: `evidence/CP07-native-ollama-settlements.json`.
- Actual screenshots visually inspected: `evidence/CP07-native-traces-after-stream.png`, `evidence/CP07-native-finops-after-stream.png` (also before/first call variants `CP07-native-traces-ui.png`, `CP07-native-finops-ui.png`).
- Reproducible defect analyses: `reports/DEF-051-ollama-native-counters-estimated.md`, `reports/DEF-052-native-traces-disagree-with-finops.md`, and `reports/DEF-053-token-price-unit-contract-mismatch.md`.

## Coverage classification

PASS for this narrow slice: authorized native request dispatch to a loopback test provider, JSON and SSE response delivery, provider probing, and persistence of both settlement rows. FAIL: usage extraction, cross-system trace reconciliation, and pricing contract as detailed in DEF-051–053. UNVERIFIED: Anthropic/Gemini, all provider-native dialects beyond the tested paths, multimodal/tools/embeddings, key role matrix, retries/failover, timeout/cancellation, budgets/exports, supported runtime role modes, external provider interoperability, and failure recovery.

No production code was changed; no live provider was called; no external spend occurred. Test credentials, key material, runtime databases, and service logs remain outside the QA handoff.
