# CP35 — Playground failed request and trace visibility

**Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium 156; synthetic Org Owner  
**Disposition:** Playground error journey exercised; DEF-045 **CONFIRMED, Low**. FT-01, WF-06 and SCR-15 are **PARTIAL** for the named synthetic failure path. SCR-11 remains **PARTIAL**. No real provider or paid inference was used. `GATE-REL-07` remains **BLOCKED**.

## Real-user journey and evidence

1. Signed in as the synthetic owner, selected `Default Organization / default / dev`, and opened Playground. Visually inspected its initial state. The UI explained the ephemeral key, hard micro-USD budget cap, and that prompts stay in the browser tab.
2. Entered a synthetic prompt and nonexistent route slug `qa003-no-such-route-cp35`. Send minted an ephemeral key and the streaming request returned HTTP 503 `no_compliant_deployment_available`, with a trace ID. The page displayed the structured error and retained the transcript turn. Visually inspected [Playground error result](../evidence/CP35-playground-error.png); the sanitized API/UI response is in [first run data](../evidence/CP35-playground-error-result.json).
3. Waited and independently fetched authenticated `GET /admin/v1/traces/{trace-id}`. It returned HTTP 200 with status 503, exact zero cost, a Policy stage, and no prompt/completion/message/content fields. The persisted trace appeared in the Traces table. Clicking its row opened a waterfall showing the recorded error and metadata. Visually inspected [trace list](../evidence/CP35-traces-list.png) and [trace waterfall](../evidence/CP35-trace-detail.png); extracted UI text is in [trace view result](../evidence/CP35-trace-view-result.json).
4. Repeated the journey using a second synthetic nonexistent route. Its data-plane 503 arrived in 87 ms. After 1.1 seconds the Playground still said “Trace not available yet”; a subsequent authenticated trace read returned HTTP 200. Visually inspected [repeat Playground state](../evidence/CP35-playground-trace-timing.png); sanitized timing and status are in [repeat result](../evidence/CP35-trace-timing-result.json).

## Finding and coverage

- DEF-045 is confirmed twice: the Playground error state shows an ID with “Trace not available yet,” while that trace is persisted and readable on Traces and through the detail API. Full reproducibility, impact, root-cause hypothesis, and acceptance criteria: [DEF-045 report](../reports/DEF-045-playground-failed-request-trace-not-loaded.md).
- The inspected failure traces contain operational metadata and did not contain prompt/completion fields. This is scoped evidence only; it does not prove at-rest payload privacy or OTel exporter behavior.
- This does not cover successful streaming, cancellation, timeouts, budget denial, comparison mode, or other roles. FT-01, WF-06, and SCR-15 move from UNVERIFIED to PARTIAL for the exercised failure flow. FT-14 remains UNVERIFIED; SCR-11 remains PARTIAL.
- The initial Playground attempt left its ephemeral credential to expire under the advertised 15-minute TTL; the credential itself was never logged or stored as evidence. No source code was changed, and QA-003 remains incomplete.
