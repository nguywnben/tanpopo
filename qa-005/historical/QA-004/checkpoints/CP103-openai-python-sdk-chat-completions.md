# CP103 — Official OpenAI Python SDK chat completions

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Coverage result:** official Python SDK synchronous and streaming chat **PASS (scoped)**; trace/settlement reconciliation **FAIL** under existing DEF-052; overall SDK interoperability remains **PARTIAL**.

## Execution

The documented OpenAI Python package was not preinstalled. `openai` Python `3.59.0` was installed under `/tmp/qa004-openai-python-sdk` only. On healthy runtime 18128 (`QA004 Test Root Org / default / dev`), the real Console issued a PROVIDER key restricted to `/v1/chat/completions`. The SDK used `OpenAI(api_key=..., base_url="http://127.0.0.1:18128/v1", max_retries=0)` and a cookie-free Python process.

Synchronous `chat.completions.create` returned `Hello from synthetic Ollama.`, `finish_reason=stop`, and 7 prompt + 4 completion tokens. A streaming SDK call returned the same text, standard terminal `stop`, and 7/4/11 token usage after opting in with `stream_options.include_usage=true`. Its deterministic temporary fixture emitted a standards-correct finish-reason chunk followed by the usage chunk. The upstream observed exactly two requests, including the opt-in stream option, and no client authorization header.

Persistent Traces labels both successful requests Exact/$0. The same key's Chargeback row shows two exact requests totaling `$0.000002`. This independently extends **DEF-052** to the documented official Python SDK, in addition to CP102's official Node SDK; no duplicate finding was created. The two screenshots of actual Traces and Chargeback rows, plus the revoked-key inventory, were visually inspected. Post-revocation SDK replay raised `AuthenticationError` with HTTP 401 and generated no additional fixture request.

The temporary standards-correct fixture was stopped and the original `/tmp/qa004-ollama-mock.py` was restored; its `/api/tags` returned `qa004-mock`. The SDK and its cache stayed under `/tmp`; no dependency or product code was changed in either repository. No external provider call or spend occurred.

## Evidence

- Sanitized [Python SDK completion, streaming, and fixture result](../evidence/CP103-openai-python-sdk-results.json)
- [Trace/Chargeback and revoked-key readback](../evidence/CP103-openai-python-sdk-readback.json), [HTTP 401 replay](../evidence/CP103-openai-python-sdk-replay.json)
- Visually inspected [Traces](../evidence/CP103-openai-python-sdk-traces-ui.png), [Chargeback](../evidence/CP103-openai-python-sdk-finops-ui.png), and [revoked key](../evidence/CP103-openai-python-sdk-key-revoked.png)

## Coverage status

- Official OpenAI Python SDK synchronous chat completion: **PASS (scoped)**.
- Official OpenAI Python SDK streaming text, terminal finish reason and opt-in usage: **PASS (scoped)**.
- Upstream credential isolation, route grant, key revocation and post-revocation denial: **PASS (scoped)**.
- Traces versus exact settlement reconciliation: **FAIL**, existing DEF-052.
- Other Python SDK versions, Responses/embeddings APIs, retries/abort, external providers, roles and platforms remain **UNVERIFIED**.

QA continues; this is not broad Python SDK or provider acceptance. **GATE-REL-07 remains BLOCKED.**
