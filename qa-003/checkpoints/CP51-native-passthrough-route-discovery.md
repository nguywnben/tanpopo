# CP51 — Native passthrough route discovery and failure

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Runtime:** three isolated `--role all` SQLite runtimes (18114–18116); credentialed browser request on 18116; active loopback-only Ollama mock; Chromium 1440×1000
**Disposition:** new finding **DEF-048 High, confirmed**. Documented native endpoints are routed to the Console handler and return 405. `FT-02` is **FAIL for native endpoint availability**; lossless payload/response behavior remains unverified. `GATE-REL-07` remains **BLOCKED**.

## Setup and real interaction

The Owner selected `default/dev` and `qa003-cp47-stream-route` in the real Playground UI. The route was already `ACTIVE_HEALTHY` and had previously returned synthetic mock content. A new Playground session key was minted by the supported UI flow and held only in test process memory. The actual screenshot was inspected: [route-ready view](../evidence/CP51-native-ollama-route-ready.png) shows the selected logical route. No key material was persisted.

The browser then sent a `POST /native/ollama/api/chat` request using the same valid short-TTL key with `credentials: omit`, the provider-native Ollama path and a valid test message. Instead of native JSON, the browser received HTTP 405, `Allow: GET, HEAD`, `text/plain`, and `HTTP method not allowed for this console resource.` No Tanpopo request/trace ID was returned. [Sanitized request result](../evidence/CP51-native-ollama-result.json).

The three isolated runtime matrix then posted unauthenticated synthetic requests to Ollama, Anthropic and Gemini native prefixes. All nine returned the same generic Console 405 with no Tanpopo request or trace headers. These probes show common outer path routing but are not counted as authorization tests. [Matrix](../evidence/CP51-native-route-probe-matrix.json).

The actual Traces page screenshot was visually inspected: [Traces after the attempt](../evidence/CP51-native-ollama-traces.png). It displays the successful seed Playground request and prior rows; the attempted native request produced no new trace entry. This is consistent with it failing before reaching gateway ingress.

## Contract comparison and disposition

The frozen OpenAPI contract declares `POST /native/{dialect}/{upstreamPath}`; the protocol compatibility spec and Quickstart also describe native passthrough. The active loopback adapter mock supports `/api/chat`. Yet the runtime returns the Console's GET/HEAD-only static resource response, not a native gateway error/response. This is confirmed as a product endpoint-routing defect, DEF-048 High. The mock is local and no external provider/Google egress was involved.

**Root-cause hypothesis only:** the outer CLI mux routes `/v1/` into the Data Plane but may not mount `/native/`, causing fallback to the Console handler. Wire behavior is the evidence; this checkpoint does not assert the exact engineering fix.

## Coverage status

- `FT-02` native ingress availability: **FAIL** for the documented endpoint on tested `--role all` runtime; request payload passthrough is **UNVERIFIED** because the gateway never received the request.
- `FT-01` / `WF-06`: normalized Playground streaming remains **PARTIAL**; a native request is blocked at the outer route.
- `DEF-048`: [engineering handoff](../reports/DEF-048-native-passthrough-unreachable.md), High, confirmed.
- Still required: exact `data-plane` role/separate listener; valid and invalid credential matrix; native upstream response and body preservation; SSE, cancellation, budgets, audit/traces, allowlist, SSRF, and additional declared dialects after route reachability is restored.
- No product source changed; no provider egress/spend.
