# CP87 — Native Ollama Generate streaming

**Baseline:** frozen RC6 2eb7f6f6e5ba703d44fbd273f0245ba441de74e1  
**Outcome:** PASS (scoped) for authorized Ollama Generate stream dispatch and body fidelity. FAIL for the same DEF-070 NDJSON/SSE media-type mismatch; existing DEF-051/052 extend to this endpoint.  
**Product source:** unchanged. **Gate:** GATE-REL-07 remains BLOCKED.

## Generate stream journey

An Org Owner issued a PROVIDER key restricted to /native/ollama/* and sent a cookie-free POST /native/ollama/api/generate with model qa004-loopback, stream:true, and Accept: application/x-ndjson. The loopback fixture handled one /api/generate request with no authorization header and returned three newline-delimited JSON objects: two response chunks and one done:true frame with prompt_eval_count:8 / eval_count:3.

RC6 returned HTTP 200, preserved the three NDJSON objects, but labeled them text/event-stream; charset=utf-8; the explicit NDJSON Accept header did not affect the response type. The stream contained no SSE data: prefix. This independently extends DEF-070 from native /api/chat to /api/generate. Raw evidence: [Generate stream results](../evidence/CP87-generate-stream-results.json).

## Usage reconciliation

The persisted NATIVE trace is HTTP 200, EXACT / $0; its Traces row was visually inspected: [trace screenshot](../evidence/CP87-generate-stream-traces.png). The real Chargeback UI grouped by Virtual Key shows 0 exact / 1 estimated request at $0.000001; the screenshot was visually inspected: [FinOps screenshot](../evidence/CP87-generate-stream-finops.png). The stream response omitted X-Tanpopo-Usage-Accuracy. This independently extends DEF-051/052 to native /api/generate streaming. See [FinOps readback](../evidence/CP87-generate-stream-finops.json).

## Authorization and cleanup

The Owner revoked the temporary key through Access & Keys. Readback is REVOKED v2; cookie-free replay returned HTTP 401 invalid_virtual_key and the fixture log did not grow. The state screenshot was visually inspected: [revoked key](../evidence/CP87-generate-key-revoked.png). The one-time secret remained in mode-0600 /tmp and was deleted. The temporary fixture was stopped; the original qa004-mock was restored and /v1/models returned the synthetic catalog. Cleanup evidence: [key revocation](../evidence/CP87-generate-key-revocation.json).

## Coverage boundary

- **PASS (scoped):** native Generate streaming, NDJSON body preservation, terminal usage fields, credential-free upstream dispatch, VKEY revoke and replay denial.
- **FAIL:** DEF-070 MIME/framing mismatch; DEF-051/052 usage header/trace/FinOps disagreement.
- **UNVERIFIED:** native Generate client compatibility, cancellation for this endpoint, reconnect, malformed/oversized lines, other dialects and non-loopback/provider deployments.

QA continues; **GATE-REL-07 remains BLOCKED**.
