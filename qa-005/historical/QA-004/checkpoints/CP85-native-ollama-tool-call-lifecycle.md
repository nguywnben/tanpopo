# CP85 — Native Ollama `/api/chat` tool-call round trip

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Outcome:** PASS (scoped) for authorized native tool-call dispatch and JSON response fidelity. Existing DEF-051 and DEF-052 FAIL are independently reproduced; no duplicate defect.  
**Product source:** unchanged. **Gate:** `GATE-REL-07` remains BLOCKED.

## Request, upstream dispatch and response

In real Chromium, an Org Owner issued a PROVIDER Virtual Key limited to `/native/ollama/*`. A cookie-free `POST /native/ollama/api/chat` used model `qa004-loopback`, a synthetic user message, and a `lookup_order` function tool with an `order_id` schema. RC6 returned HTTP 200 `application/json`, with `X-Tanpopo-Usage-Accuracy: ESTIMATED` and preserved the native Ollama JSON response: assistant role, empty content, tool call `lookup_order`, argument `{"order_id":"QA-104"}`, `done:true`, `prompt_eval_count:17`, and `eval_count:4`.

The isolated provider fixture observed exactly one upstream request to `/api/chat`, model `qa004-loopback`, one `lookup_order` tool, and one user message. It received no authorization header. The tool was only returned as a synthetic model result; it was not executed against any real system. Raw sanitized results: [native tool-call results](../evidence/CP85-native-tool-call-results.json).

## Usage, trace and FinOps reconciliation

For the same request, persisted trace `trc_01m8ajdam8e008jaw7v80nma1a` is NATIVE, HTTP 200, status error null, usage `EXACT`, total cost 0 micro-USD. The Traces screenshot was visually inspected: [trace waterfall](../evidence/CP85-native-tool-traces.png).

The real FinOps Chargeback UI, grouped by Virtual Key, displays `QA004 CP85 native Ollama tool calls` as 0 exact requests / `$0.000000` and 1 estimated request / `$0.000001`; its screenshot was visually inspected: [FinOps by key](../evidence/CP85-native-tool-finops.png). This is consistent with DEF-051 (native provider token counters remain estimated) and independently extends DEF-052 (trace `EXACT`/$0 disagrees with FinOps estimated metering). No new defect ID is assigned.

## Authorization and cleanup

The Owner revoked the synthetic key in the Console. Readback is `REVOKED` v2; a cookie-free replay returned HTTP 401 `invalid_virtual_key`, and the fixture call count remained one: [revocation evidence](../evidence/CP85-native-key-revocation.json). The key secret stayed in a mode-0600 `/tmp` file, was never committed, and was deleted. The temporary tool-call fixture was stopped; the original `qa004-mock` on port 18125 was restored and `/v1/models` returned the expected catalog.

## Coverage boundary

- **PASS (scoped):** Native Ollama `api/chat`, one function tool, one JSON response, PROVIDER-plane route authorization, cookie-free dispatch, upstream key isolation, trace/FinOps inspection, revoke and replay denial.
- **FAIL (existing):** DEF-051 usage classification and DEF-052 trace-versus-FinOps settlement projection.
- **UNVERIFIED:** native streaming, multiple/parallel tool calls, tool-result follow-up turns, invalid tool schemas, model-specific argument edge cases, native Generate variants, other dialects/adapters and multimodal/tool execution by a client.

QA-004 continues. This is not full native dialect, provider, tool, integration or release acceptance. **GATE-REL-07 remains BLOCKED.**
