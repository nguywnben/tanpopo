# CP116 — A2A resume state delivered over the live SSE endpoint

**Source:** frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; exact-source binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
**Environment:** isolated SQLite clone at `127.0.0.1:18167`, A2A enabled, offline mode; deterministic loopback endpoint at `127.0.0.1:19188`; real Chromium 156 Console; Owner. No external or paid provider calls.
**Result:** PASS (scoped A2A event stream); no new defect. DEF-011 remains PASS (scoped). GATE-REL-07 remains BLOCKED.

## Test

1. In Access & Keys, issue an A2A-only Virtual Key scoped to `/a2a/*`. The key form initially preselects PROVIDER. The first two test setup keys were immediately revoked before any data-plane call after their grants included PROVIDER; the final form explicitly deselected PROVIDER. The final screenshot visibly showed A2A selected and PROVIDER/MCP unchecked; API readback matched `allowed_planes=["A2A"]`, `allowed_routes=["/a2a/*"]`.
2. Use that key to discover the loopback agent and create a task with a 10,000-micro-USD hold. The deterministic endpoint returns `input-required`, leaving the same task v3.
3. Subscribe to `GET /a2a/tasks/{taskId}` with `Accept: text/event-stream`. HTTP 200 and `Content-Type: text/event-stream` returned the initial `task.status` snapshot `input-required` v3.
4. Resume the task through MCP Hub in the real Console. While the endpoint deliberately delayed for 12 seconds, the same SSE connection emitted `working` v4; it then emitted `completed` v5 with a 2,000-micro-USD settlement.
5. Independent A2A GET and SQLite readback agreed on task ID, state/version, 10,000 hold, 2,000 settlement and one artifact. Audit rows show submitted → working → input-required → resumed working → completed. The endpoint recorded two requests, one completed resume and no forwarded Authorization header.

## Visual and cleanup evidence

Inspected screenshots show the input-required prompt and actions, filled resume dialog, spinner while pending, and success/Completed row after resumption. See `CP116-a2a-sse-input-required.png`, `CP116-a2a-sse-resume-filled.png`, `CP116-a2a-sse-resume-pending.png`, and `CP116-a2a-sse-completed.png`. Plane-choice screenshots preserve the initial default and corrected A2A-only grant: `CP116-a2a-plane-default-open.png` and `CP116-a2a-plane-only-before-issue.png`.

The final A2A-only key was revoked, read back as REVOKED v2, and replay returned HTTP 401. The audit chain verified intact at 711 events. Two earlier test setup keys (one not capturable from its one-time response; one accidentally dual-plane due to retaining the visible PROVIDER default) were revoked before any data-plane use. They are recorded as harness corrections, not product defects; no key secret is committed. Runtime and fixture were stopped. Sanitized details: [evidence](../evidence/CP116-a2a-sse-lifecycle.json).

## Limits

The stream subscription began after synchronous initial dispatch, so this independently establishes the input-required snapshot and resumed working/completed events, not SSE observation of the initial submitted/working events. SQLite, Owner, one loopback endpoint and Chromium only; no real A2A partner, other roles, PostgreSQL/Valkey stream, browser diversity, cancellation, failure/retry/timeout, or multi-hop workflow. This does not close A2A/MCP protocol breadth.
