# CP34 — Mid-stream abort, no fallback, and metering reconciliation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Outcome:** PASS (scoped) for pre-first-chunk-only failover: after the primary emitted content and disconnected, Tanpopo returned `mid_stream_abort` and did not call the secondary. FAIL for the existing DEF-052 trace/settlement projection mismatch on the partial stream. No new defect ID.  
**Product source:** unchanged. **Gate:** `GATE-REL-07` remains BLOCKED.

## Stream journey and failover boundary

A temporary local primary fixture on port 18125 returned an SSE response with one content chunk (`Primary partial answer.`) and then closed the connection without `[DONE]` or provider usage. A healthy secondary fixture remained available on port 18126. Through a temporary two-target `PRIORITY_FAILOVER` route and a UI-issued PROVIDER Virtual Key, a cookie-free `POST /v1/chat/completions` with `stream=true` returned HTTP 200 `text/event-stream`.

The client received the primary's partial chunk followed by an `event: error` frame with `mid_stream_abort`, `retryable=false`, and the message `upstream closed unexpectedly before stream completion`; the stream then ended. The secondary provider request count remained 8 before and after the call, proving no fallback was attempted after content reached the client. The primary fixture observed exactly one streaming call. The real Traces screenshot shows status 200 and `mid_stream_abort`; its waterfall opens from the persistent trace. This passes the no-mid-stream-failover behavior for one OpenAI-compatible streaming request.

## Trace and FinOps disagreement — DEF-052

The persisted trace `trc_01m89m26w4e009j53p9m01tmav` is status 200, error `mid_stream_abort`, usage accuracy `EXACT`, and total cost 0 micro-USD. The same Virtual Key's real FinOps Chargeback row reports 0 exact requests / $0.000000 and 1 estimated request / $0.000001. The Traces and Chargeback screenshots were visually inspected; the same request is therefore labeled Exact/$0 in Traces and Estimated/$0.000001 in FinOps.

This independently reproduces and broadens DEF-052 from successful native inference to a provider-plane partial stream failure. The runtime correctly estimates the partial-stream settlement, but the request trace projection does not reflect that settlement. No duplicate defect ID is created.

## Cleanup and status

The real Access & Keys UI revoked the temporary key; metadata persisted as `REVOKED` v2. A cookie-free replay returned 401 `invalid_virtual_key` with no additional fixture calls. The temporary route was deleted and the route list returned to `qa004-loopback`. The original successful provider fixture was restarted; the local endpoint responds to `/v1/models`. No external provider or external spend was used.

- Sanitized stream, counter, trace, FinOps, and cleanup record: [CP34 results](../evidence/CP34-midstream-abort-results.json).
- Visually inspected screenshots: [Traces](../evidence/CP34-midstream-trace.png) and [FinOps by Virtual Key](../evidence/CP34-finops-midstream.png).
- **PASS (scoped):** partial SSE chunk followed by canonical abort event; no secondary dispatch; key revocation and route cleanup.
- **FAIL (existing DEF-052):** trace says Exact/$0 while settlement/FinOps says Estimated/$0.000001.
- **UNVERIFIED:** client-triggered cancellation timing, disconnect at different chunk boundaries, native dialect streams, retries before first byte, and alternate providers/runtimes.
- QA-004 continues; no new defect ID.
