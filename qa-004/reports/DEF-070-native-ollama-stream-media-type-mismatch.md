# DEF-070 — Native Ollama stream body is NDJSON but response is labeled SSE

**Severity:** Moderate  
**Status:** FAIL on frozen RC6  
**Baseline:** `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Discovered:** QA-004 CP86  
**Affected workflows:** native Ollama `/api/chat` streaming, tool-call clients, response negotiation and stream parsing

## Summary

The native Ollama `/api/chat` stream returns newline-delimited JSON frames, but Tanpopo labels the HTTP response `text/event-stream`. The request explicitly accepted `application/x-ndjson`; the response still used the SSE media type. The body contains no SSE `data:` framing, so the response type does not describe the bytes that follow.

## Reproduction

1. Start the frozen RC6 isolated runtime with an Ollama-compatible loopback fixture. Configure its `/api/chat` streaming response as `Content-Type: application/x-ndjson`, with one JSON object per line and a final `done:true` object. The CP86 fixture supplied a tool-call frame and terminal usage counters.
2. In real Chromium, sign in as Org Owner in `default/dev`, issue a PROVIDER Virtual Key restricted to `/native/ollama/*`, and retain its one-time secret only outside the repository.
3. Send a cookie-free `POST /native/ollama/api/chat` with model `qa004-loopback`, `stream:true`, one synthetic message and a function tool. Set `Accept: application/x-ndjson`.
4. Read the response headers and body. Repeat with the same isolated fixture; CP86 recorded three full /api/chat streams with the same response type; CP87 independently reproduced the mismatch on /api/generate.
5. Read the body as lines. Each line parses as JSON, and none starts with `data:`.
6. Revoke the test key, verify replay is denied, and restore the original fixture.

Actual response: HTTP 200, `Content-Type: text/event-stream; charset=utf-8`; four plain NDJSON lines containing assistant chunks, `lookup_order` arguments and final usage counters. See [CP86 content-type result](../evidence/CP86-ndjson-content-type-check.json), [CP86 stream results](../evidence/CP86-native-stream-results.json), [CP87 Generate results](../evidence/CP87-generate-stream-results.json), and visually inspected [CP86 Traces](../evidence/CP86-native-stream-traces.png) and [CP87 Traces](../evidence/CP87-generate-stream-traces.png). No external provider or real tool was used.

## Expected versus actual

- **Expected:** native Ollama streaming preserves the upstream NDJSON representation and media type, or performs a documented transformation that makes the response a valid SSE stream. The advertised response type and delivered framing must match the client’s negotiation.
- **Actual:** the response is labeled `text/event-stream` while its bytes remain newline-delimited JSON objects. The explicit `Accept: application/x-ndjson` did not change the response type.

## Business impact and severity

Native Ollama clients and generic HTTP consumers can choose a parser based on the advertised media type. An SSE parser expects `data:`-framed events and will not parse these raw JSON lines as SSE, which can prevent streamed content, tool calls and terminal usage from being consumed. Non-streaming native JSON remained unaffected in CP85. Severity is Moderate because this breaks one supported native streaming workflow while the body itself is intact and client cancellation still propagated in the tested slice.

## Root-cause hypothesis (not confirmed)

A generic stream response path may be setting `text/event-stream` for native streams without preserving the upstream media type. Confirm the adapter and ingress behavior before selecting a fix.

## Acceptance criteria

- Native Ollama `/api/chat` and `/api/generate` streaming responses use a media type consistent with their framing and honor supported `Accept` negotiation.
- If Tanpopo preserves Ollama NDJSON, return `application/x-ndjson` and preserve one JSON object per line including final `done:true` usage. If it transforms the stream, emit valid framing and document that contract.
- Preserve tool-call names, arguments, chunk order and terminal fields; do not relabel raw NDJSON as SSE.
- Regression covers non-stream JSON and streaming success/error/cancellation for Ollama, plus SSE-native adapters to prevent regressions in their framing.

## Related findings

DEF-051 and DEF-052 remain separate usage accuracy and trace/FinOps reconciliation findings. CP86 independently reproduces both for full and canceled native streams. The native client cancellation itself passed this tested slice and is not part of DEF-070.
