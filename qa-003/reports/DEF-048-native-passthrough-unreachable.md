# DEF-048 — Documented native Data Plane endpoints are unreachable in supported runtime roles

**Severity:** High
**Status:** Confirmed on frozen RC5; credentialed Ollama reproduction on runtime 18116; nine path probes across three isolated role-all runtimes; separated Data Plane role also returns 404
**Baseline:** `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Affected:** Native passthrough clients using Ollama, Anthropic, Gemini or other documented `/native/{dialect}/{upstreamPath}` routes in `--role all` runtimes
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Reproduction

1. Start the frozen RC5 service in the supported `--role all` configuration. Runtime 18116 was independently started from this exact source baseline.
2. Through the actual Console, select `Default Organization / default / dev`, choose `qa003-cp47-stream-route` and send a successful Playground prompt. The route is active and reaches a controlled, authorized, loopback-only Ollama-compatible provider. The visually inspected [Playground route screenshot](../evidence/CP51-native-ollama-route-ready.png) records the selected route. The successful setup request minted a short-TTL Playground Virtual Key; its secret remained in process memory and was never saved.
3. Using that same valid key from a real browser `fetch` with `credentials: omit`, send `POST /native/ollama/api/chat` with a valid Ollama chat body, including the logical route/model slug. The loopback mock implements `/api/chat` and no external egress is needed.
4. Observe HTTP **405**, `Content-Type: text/plain`, `Allow: GET, HEAD`, body `HTTP method not allowed for this console resource.` There is no `X-Tanpopo-Request-Id` or `X-Tanpopo-Trace-Id`; the expected upstream is not called and no native request appears in the trace list.
5. The same POST paths for `ollama`, `anthropic` (`/native/anthropic/v1/messages`) and `gemini` (`/native/gemini/v1beta/models/gemini-2.5-pro:generateContent`) returned the same 405 across isolated RC5 runtimes 18114, 18115 and 18116. These nine unauthenticated probes only corroborate that the path is intercepted before the native gateway; they do not replace the valid-key Ollama reproduction. CP52 started a fresh `--role data-plane` runtime: `/v1/chat/completions` reached the handler and returned structured 401, while Ollama and Anthropic `/native/*` returned bare 404 with no Tanpopo request ID. See [CP52](../checkpoints/CP52-data-plane-role-native-ingress.md) and [role control evidence](../evidence/CP52-data-plane-role-native-results.json).

Sanitized [credentialed Ollama request result](../evidence/CP51-native-ollama-result.json), [three-runtime/dialect matrix](../evidence/CP51-native-route-probe-matrix.json), and [Traces screenshot](../evidence/CP51-native-ollama-traces.png) are preserved. The screenshot was visually inspected: it shows the successful setup chat traces but no row for the attempted native call. This is a routing failure, not a Google Cloud egress failure; all requests were loopback-local.

## Contract and expected versus actual

The frozen OpenAPI contract declares `POST /native/{dialect}/{upstreamPath}` and documents native dialect passthrough. The API protocol compatibility spec and Quickstart also describe `/native/*` as the supported lossless native request path. `POST /native/ollama/api/chat` should reach Tanpopo's native ingress, authenticate the Virtual Key, enforce policy/SSRF/rate/budget controls, forward to the active Ollama adapter, preserve the native response, and emit a trace. A successful request should return the synthetic mock response with HTTP 200.

Actual RC5 behavior returns a generic static Console resource 405 before native authorization or routing. No provider request, gateway trace or native response occurs. This makes all documented native clients unavailable on the tested standard runtime, even though the OpenAI-compatible Playground route succeeds.

## Impact

Clients using native provider SDKs or vendor-specific fields cannot use the advertised lossless passthrough path in the tested deployment. This affects workflows that need provider-native schema/feature support and is broader than the Ollama fixture: Anthropic and Gemini prefixes showed the same outer-router behavior. The normalized `/v1/*` route remains available in the tested loopback scenario. No credential bypass or external traffic was observed.

## Facts and root-cause hypothesis

**Observed facts:** a valid ephemeral key and healthy synthetic route were available; credentialed browser POST to the documented native Ollama path returned the Console's GET/HEAD-only 405; nine corroborating unauthenticated probes showed the same response across three `--role all` runtimes and three dialect prefixes; no Tanpopo request/trace headers or provider call were present.

**Root-cause hypothesis, not a fix assertion:** the CLI's outer listener appears to route `/v1/` to the Data Plane server while unmatched paths fall through to the Web Console handler; `/native/` may not be mounted to that Data Plane server. Frozen-source inspection of the runtime router is consistent with this, but the runtime wire behavior is the basis of this defect.

## Acceptance criteria

1. In each supported role/deployment mode, requests to documented `/native/{dialect}/{upstreamPath}` endpoints must reach the native ingress handler rather than the Console static handler.
2. With a valid key, an active Ollama fixture, and loopback-only endpoint, `POST /native/ollama/api/chat` returns HTTP 200 with the synthetic Ollama-native response; the mock records the exact expected upstream path and no credential leaks.
3. Repeat valid-key requests for supported Anthropic, Gemini and other declared native dialects against deterministic mocks, verifying byte/field preservation, stream and non-stream responses, usage metering, trace persistence, audit/policy/rate/budget behavior and route selection.
4. Verify invalid, expired and wrong-plane credentials return documented structured authorization errors, never the Console HTML/text handler; verify denied keys do not reach upstream.
5. Test role `all`, `data-plane` and any documented separated-listener deployment; confirm Console GET routes remain unaffected.
6. Re-run `FT-02` lossless-field, streaming, cancellation, SSRF, native path allowlist, role/RBAC and observability regressions. Do not weaken the native path allowlist or SSRF protections to make the endpoint reachable.

## Remaining limits

The accepted native request never reached provider routing, so lossless body handling, native response parsing, provider dispatch, usage accounting, cancellation, SSRF controls, and native authorization remain unverified. Only runtime role `all` was used for credentialed end-to-end reproduction; CP52 confirms the separated Data Plane role also lacks the native route, but native authentication cannot be evaluated until routing is available. Other separate-listener deployment configurations remain open. No product implementation was modified and external provider spend was USD 0.
