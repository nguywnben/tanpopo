# CP86 — Native Ollama streaming, tool call and cancellation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Outcome:** FAIL for Ollama native stream media-type/framing mismatch (DEF-070). PASS (scoped) for streaming tool-call body fidelity and client cancellation reaching the upstream fixture. Existing DEF-051/052 are independently extended.  
**Product source:** unchanged. **Gate:** `GATE-REL-07` remains BLOCKED.

## Full native stream and tool call

A route-limited PROVIDER key authorized only `/native/ollama/*`. The isolated synthetic Ollama fixture returned four newline-delimited JSON messages from `/api/chat`: two assistant content chunks, an assistant `lookup_order` tool call with `{"order_id":"QA-106"}`, and a final `done:true` frame with `prompt_eval_count:17` / `eval_count:4`. RC6 forwarded all four body lines without altering the JSON objects. No tool executed against a real service and no provider credential reached the fixture.

The client explicitly sent `Accept: application/x-ndjson`. RC6 still returned HTTP 200 with `Content-Type: text/event-stream; charset=utf-8`; the response body consists of four valid JSON lines with no `data:` SSE prefixes. A strict check confirmed `allLinesAreJson=true` and `sseDataPrefix=false`. This is repeatable across three full streams. Machine evidence: [NDJSON content-type check](../evidence/CP86-ndjson-content-type-check.json), [full stream and fixture records](../evidence/CP86-native-stream-results.json).

## DEF-070 — native Ollama stream MIME/framing mismatch

The current OpenAPI `POST /native/{dialect}/{upstreamPath}` contract advertises native JSON or `text/event-stream`; SRS TP-FR-0105 says native JSON/SSE payloads should be forwarded losslessly. Ollama `/api/chat` streaming here is newline-delimited JSON, not SSE framing. Returning raw NDJSON under `text/event-stream` makes the response framing disagree with its MIME type and the client's explicit `Accept`. See [DEF-070](../reports/DEF-070-native-ollama-stream-media-type-mismatch.md). No root cause is confirmed; whether the generic native stream adapter overwrites the upstream media type is a hypothesis only.

## Native client cancellation and usage reconciliation

Two cookie-free clients canceled a second streamed request after reading two NDJSON chunks. In both runs the synthetic fixture detected the downstream disconnect after sending three chunks; it did not finish the remaining 21 chunks. The trace list shows the full streams as HTTP 200 and the canceled streams as HTTP 200 `mid_stream_abort`: [Traces screenshot](../evidence/CP86-native-stream-traces.png). This is PASS for the tested cancellation slice.

Five native stream requests under this key produced five NATIVE HTTP 200 trace rows labeled `EXACT` with `$0`. Real FinOps grouped by Virtual Key reports 0 exact requests / `$0.000000` and 5 estimated requests / `$0.000005`; [FinOps screenshot](../evidence/CP86-native-stream-finops.png) was visually inspected. Stream responses lacked `X-Tanpopo-Usage-Accuracy` (the header read was null) even though terminal frames had Ollama counters. This extends existing DEF-051 (Ollama counters are not settled as exact) and DEF-052 (trace projection disagrees with durable FinOps), not a new defect ID. Machine reconciliation: [stream metering](../evidence/CP86-native-stream-metering.json).

## Authorization and cleanup

Org Owner revoked the temporary key through Access & Keys. Readback is `REVOKED` v2; replay returned HTTP 401 `invalid_virtual_key`, and the fixture log line count did not change. The revoked-key screenshot was visually inspected: [key state](../evidence/CP86-native-key-revoked.png). The key secret was held only in mode-0600 `/tmp`, then deleted. The NDJSON fixture stopped and the original `qa004-mock` on 18125 was restored; its `/v1/models` endpoint returned the expected synthetic catalog. Cleanup: [revocation evidence](../evidence/CP86-native-key-revocation.json).

## Coverage boundary

- **PASS (scoped):** native Ollama `/api/chat` full NDJSON stream, one streamed function tool call, JSON body fidelity, client cancellation propagation after two client chunks, key revocation and replay denial.
- **FAIL:** DEF-070 MIME/framing mismatch; existing DEF-051 and DEF-052 stream-metering disagreement.
- **UNVERIFIED:** Ollama native Generate streaming, multi-tool streaming, function-call fragments, reconnect/resume, malformed NDJSON, multiple client implementations, concurrency, other native dialects and settlement with nonzero configured pricing.

QA-004 continues; **GATE-REL-07 remains BLOCKED**.
