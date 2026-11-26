# CP101 — Responses API function-tool handoff lifecycle

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Coverage result:** one synchronous function-call and tool-output round trip **PASS (scoped)**; complete Responses tool acceptance remains **PARTIAL**.

## Execution

In healthy isolated runtime 18128 (`QA004 Test Root Org / default / dev`), an Org Owner registered a logical model declaring `tool_calling`, a healthy zero-rate Ollama deployment, and a one-target route through real Console forms. The refreshed model registry confirmed `tool_calling: true`; the deployment form and route table showed the selected local Ollama target Healthy. A VKEY restricted to `/v1/responses` was issued through Access & Keys and requests were sent from Chromium with cookies omitted.

The browser submitted a Responses request for `Find order QA-104.` with one strict `lookup_order` function schema. The deterministic local fixture returned an OpenAI Responses `function_call` item with `call_id=call_qa101_lookup`, function name `lookup_order`, and JSON arguments `{"order_id":"QA-104"}`. The browser test client parsed the call and invoked only a synthetic local lookup implementation, which returned `{"order_id":"QA-104","status":"shipped"}`. It then submitted the original user input, function-call item, and a matching `function_call_output`. The fixture returned a completed assistant message, `Order QA-104 is shipped.`

Two complete tool turns were executed: the first pair completed before a one-time-key acknowledgment dialog blocked UI navigation, and the same exact flow was then repeated with the acknowledgment dismissed so results and screenshots could be captured. Across all four requests, the fixture saw exactly four `/v1/responses` calls, no client authorization header, two tool definitions, and two function outputs on the follow-up turns. All returned HTTP 200. Persisted Traces shows four successful Exact/$0 rows; Chargeback shows four exact requests and $0 for this zero-rate test deployment. The revoked-key replay returned HTTP 401 and caused no fifth fixture call. The real Traces, FinOps and revoked-key screenshots were visually inspected.

## Interpretation and limits

This verifies that the gateway forwards a function-capable Responses request, preserves a returned function-call item, accepts a subsequent `function_call_output`, and returns a final response. The function itself was simulated in the browser test client; Tanpopo did not execute a real external tool or mutate a business system. All traffic remained on loopback, the upstream received no client key, and no external spend occurred. No new product defect was confirmed.

The test model, deployment and route were created only for this slice. The route and deployment were deleted through the real Console and authenticated readback confirmed both absent. The original `/tmp/qa004-ollama-mock.py` fixture was restored and its `/api/tags` again returned `qa004-mock`. The VKEY reads REVOKED v2. RC6 provides no logical-model deletion control or admin DELETE endpoint, so the unused synthetic logical-model row remains without a route or deployment.

## Evidence

- Sanitized [tool lifecycle requests, fixture observations, traces, Chargeback, and revocation](../evidence/CP101-responses-tools-results.json)
- [Model registration](../evidence/CP101-responses-tools-model.json), [deployment creation](../evidence/CP101-responses-tools-deployment.json), [route creation](../evidence/CP101-responses-tools-route.json), and [final cleanup readback](../evidence/CP101-cleanup-readback.json)
- Visually inspected [model form](../evidence/CP101-responses-tools-model-form.png), [registered model](../evidence/CP101-responses-tools-model-created.png), [deployment form](../evidence/CP101-responses-tools-deployment-form.png), [healthy deployment](../evidence/CP101-responses-tools-deployment-created.png), [route form](../evidence/CP101-responses-tools-route-form.png), [created route](../evidence/CP101-responses-tools-route-created.png), [loaded Traces rows](../evidence/CP101-responses-tools-traces-ui.png), [Chargeback](../evidence/CP101-responses-tools-finops-ui.png), and [revoked VKEY](../evidence/CP101-responses-tools-key-revoked.png)
- Cleanup evidence: [route deleted](../evidence/CP101-route-cleanup.png), [deployment deleted](../evidence/CP101-deployment-cleanup.png)

## Coverage status

- Responses function-call item and arguments: **PASS (scoped)**.
- Client function execution followed by function-call output and assistant completion: **PASS (scoped)**.
- Cookie-free route-restricted authorization, upstream credential isolation, key revocation and replay denial: **PASS (scoped)**.
- Traces and Chargeback for this zero-rate path: **PASS (scoped)**.
- Multiple/parallel tools, refusal and malformed tool calls, streaming tool lifecycle, real SDKs, real external tool execution, nonzero-rate settlement, role variations, retries and distributed operation remain **UNVERIFIED**.

QA continues. This is not full tool or Responses API acceptance. **GATE-REL-07 remains BLOCKED.**
