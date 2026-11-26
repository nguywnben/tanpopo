# CP102 — Official OpenAI Node SDK chat completions

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Coverage result:** official SDK synchronous and streaming chat text **PASS (scoped)**; trace/settlement reconciliation **FAIL** under existing DEF-052; overall SDK interoperability remains **PARTIAL**.

## Execution

The documented OpenAI Node client was not preinstalled, so official `openai` npm package `7.64.0` was installed only under `/tmp/qa004-openai-sdk`; no product source, lockfile, package metadata or workspace dependency changed. On healthy runtime 18128 (`QA004 Test Root Org / default / dev`), an Org Owner issued PROVIDER keys through the real Access & Keys Console, each limited to `/v1/chat/completions`. The SDK used `baseURL=http://127.0.0.1:18128/v1`, the one-time key, and zero automatic retries.

The first SDK call was synchronous and returned the expected `chat.completion` object, assistant content `Hello from synthetic Ollama.`, `finish_reason=stop`, and provider usage of 7 prompt + 4 completion tokens. A streaming call returned the same text through `AsyncIterable`. The first stream fixture emitted text and a usage chunk but omitted the protocol's terminal finish-reason chunk; this is a fixture-contract issue, not a product finding. A corrected temporary fixture emitted the standard `finish_reason=stop` chunk and usage chunk. With `stream_options.include_usage=true`, the OpenAI SDK parsed the full text, `stop` finish reason, and 7/4/11 token usage. The active Tanpopo runtime and product source were unchanged by this fixture correction.

The synchronous and first streaming calls used one temporary key; the corrected stream used a second key. Each real dispatch produced exactly one unauthenticated-to-upstream fixture call. Both temporary keys were revoked; each post-revocation SDK replay raised HTTP 401 and produced no additional upstream call. Persistent readback and the visually inspected actual Traces/Chargeback UI show the two initial requests and the corrected stream request as Exact/$0 in Traces, while Chargeback records `$0.000002` for the two initial dispatches and `$0.000001` for the corrected stream dispatch. This independently extends **DEF-052** from Responses/native to the standard OpenAI-compatible `/v1/chat/completions` path through the official SDK. No duplicate defect ID was assigned.

The temporary standards-correct fixture was stopped, original `/tmp/qa004-ollama-mock.py` restored, and its `/api/tags` returned `qa004-mock`. The integration used loopback only and incurred no external provider spend.

## Evidence

- [Initial synchronous/stream SDK result and persistent readback](../evidence/CP102-openai-sdk-results.json)
- [Corrected standard stream, finish reason and usage result](../evidence/CP102-openai-sdk-stream-results.json)
- Visually inspected [Traces](../evidence/CP102-openai-sdk-traces-ui.png), [Chargeback](../evidence/CP102-openai-sdk-finops-ui.png), [initial revoked key](../evidence/CP102-openai-sdk-key-revoked.png), [corrected stream Traces](../evidence/CP102-openai-sdk-stream-traces-ui.png), [corrected stream Chargeback](../evidence/CP102-openai-sdk-stream-finops-ui.png), and [stream key revoked](../evidence/CP102-openai-sdk-stream-key-revoked.png)
- No SDK dependency or credential is committed; SDK package/cache and one-time key files stayed under `/tmp` and the temporary keys are revoked.

## Coverage status

- Official OpenAI Node SDK synchronous chat completion: **PASS (scoped)**.
- Official OpenAI Node SDK streaming text, terminal finish reason and opt-in usage: **PASS (scoped)** with a standards-correct deterministic fixture.
- Exact billing and usage in Traces versus Chargeback: **FAIL**, existing DEF-052.
- Cookie-free authentication, exact route grant, upstream credential isolation and key revocation: **PASS (scoped)**.
- Python SDK, other Node SDK versions, embeddings/Responses SDK calls, SDK retries/timeouts/abort behavior, externally hosted providers, roles and platforms remain **UNVERIFIED**.

QA continues; this is not broad SDK or provider interoperability acceptance. **GATE-REL-07 remains BLOCKED.**
