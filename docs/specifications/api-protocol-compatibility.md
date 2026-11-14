# API, Multimodal & Wire Error Compatibility Specification — Tanpopo

> **Canonical Wire Protocol Compatibility Specification (Synchronized in `TP-IMP-0002` — Spec Closure Batch B)**
> - **Document ID:** `SPEC-EXT-06` | **Version:** `0.2.0-m0-closure` | **Last Updated:** `2026-11-14`
> - **Authoring Role:** Platform Architect & Security-Minded SDK Designer
> - **Machine-Readable Contract Reference:** [`openapi/dataplane-v1.yaml`](../../openapi/dataplane-v1.yaml), [`openapi/admin-v1.yaml`](../../openapi/admin-v1.yaml), and [`proto/tanpopo/extension/v1/runtime.proto`](../../proto/tanpopo/extension/v1/runtime.proto).
> - **Measurement Honesty (`COND-PO-01`):** Per-chunk SSE forwarding (`p95 < 1.0 ms`, ceiling `<= 5.0 ms`) and client-disconnect upstream cancellation (`< 500 ms`) are **`TARGET / UNVERIFIED`** design targets until empirical verification in `SPIKE-02` and `M5`.
> - **Requirements Traceability:** `TP-FR-0101`..`0106`, `TP-FR-0601`..`0603`, `TP-FR-0702`, `TP-FR-0802`, `TP-NFR-0101`..`0102`, `TP-NFR-1002`, `TP-BR-0503`..`0505`, `TP-BR-0703`, `CR-01`, `CR-05`, `CR-08`, `CR-09`.

---

## 1. Multi-Plane Client Ingress Surface

So mainstream SDK libraries connect immediately by changing only `base_url` and `api_key` without conflating the four distinct architectural planes (`DEC-003`), Tanpopo strictly partitions its ingress route prefixes:

| Plane | Canonical Route Prefixes | Compatible Client SDKs & Tools | Tanpopo Credential Delivery |
|---|---|---|---|
| **1. Unified Provider Ingress** | `POST /v1/chat/completions`<br/>`POST /v1/responses`<br/>`POST /v1/embeddings`<br/>`GET /v1/models`<br/>`POST /v1/rerank` *(`v0.2` — `CR-01`)* | OpenAI Python/TS/Go SDKs, LangChain, LlamaIndex, Vercel AI SDK, Cohere/Jina-compatible rerank callers (`v0.2`), and OpenAI-compatible IDEs. | `Authorization: Bearer vk_tp_<env>_<32_base62>` |
| **2. Lossless Native Passthrough** | `POST /native/anthropic/v1/messages`<br/>`POST /native/gemini/v1beta/...`<br/>`POST /native/{dialect}/{upstreamPath}` | Official Anthropic SDK, official Google GenAI SDK, and workloads requiring day-zero vendor-specific blocks. | Accepts `Authorization: Bearer vk_tp_...`, native SDK default headers (`x-api-key`, `x-goog-api-key`), or `X-Tanpopo-Virtual-Key` (stripped completely at ingress before `BlindBroker` injects the real upstream credential). |
| **3. MCP Governance Hub** | `POST /mcp`, `GET /mcp`, `DELETE /mcp` *(`v0.2`)* | Clients implementing **MCP Specification `2025-03-26` (Streamable HTTP)** (IDE/client interoperability verified via `CT-36` per `COND-PO-04`). | `Authorization: Bearer vk_tp_...` (or OAuth 2.1 tokens scoped strictly to `aud = tanpopo-mcp-hub`). |
| **4. A2A Connectivity Plane** | `GET /a2a/agents`<br/>`POST /a2a/tasks`<br/>`GET /a2a/tasks/{taskId}`<br/>`POST /a2a/tasks/{taskId}/cancel` *(`v0.3`)* | Autonomous agents compliant with **Agent2Agent (`A2A`)** JSON-RPC / HTTP+SSE (`SM-09`). | `Authorization: Bearer vk_tp_...` (or short-lived attenuated task delegation keys). |
| **5. Control Plane & Probes** | `/admin/v1/*`, `GET /healthz`, `GET /readyz`, `GET /metrics` | Unified `tanpopo` CLI, Astryx Web Console, Kubernetes probes, and GitOps pipelines. | `HttpOnly; Secure; SameSite=Strict` admin cookie + `X-CSRF-Token` or dedicated Admin Bearer Token (`Virtual Keys` `vk_tp_*` are rejected on `/admin/v1/*` with `401 invalid_credential_plane` — `AUTH-07`). |

---

## 2. Per-Capability Model Compatibility Matrix & Practical Boundaries

| Capability (`snake_case` Key) | Normalized Behavior on Unified Path (`/v1/*`) | Behavior on Native Dialect Path (`/native/*`) | `Fail-Loudly` Rejection & Unsupported Boundaries |
|---|---|---|---|
| **Chat & Responses** (`streaming`) | Bidirectional mapping of roles (`system`/`developer`, `user`, `assistant`, `tool`), `max_output_tokens`, `temperature`, `top_p`, `stop`; auto-injects `stream_options.include_usage = true` on OpenAI-compatible streams (`CR-09`). | Preserves `100%` of the vendor's native request/response block structure untouched (`InspectNativeRequest`). | Unsupported required parameter -> returns `400 capability_not_supported` (`TP-BR-0504`); zero silent parameter dropping (`CT-08`). |
| **Embeddings & Rerank** (`embeddings`, `rerank`) | Supports single/array embedding inputs, `encoding_format` (`float`/`base64`), `dimensions`, and (`v0.2`) `/v1/rerank` `query` + `documents` + `top_n`. | Preserves native vendor embedding/rerank payloads. | Rejects with `400 capability_not_supported` if target deployment lacks `embeddings` / `rerank` or custom `dimensions`. |
| **Multimodal Attachments** (`vision`, `audio_input`, `document_input`) | Normalizes `image_url`, `input_audio`, and `file`/`document` blocks (request body cap default `<= 32 MiB`). Payloads `> 64 KiB` are extracted into Host streaming buffers and passed to adapters as `BlobRef` (`blob_ref://...`) handles outside `16 MiB` Wasm memory (`CR-05`). | Forwards native multimodal structures losslessly while streaming `> 64 KiB` attachments via Host `BlobRef` buffers. | **Anti-SSRF Guardrail:** Gateway-assisted HTTPS URL fetching is disabled unless permitted by policy and validated at `connect()` time by `SSRFGuard` (`CT-26`). |
| **Tool Calling** (`tool_calling`, `parallel_tool_calls`) | Normalizes `tools`, `tool_choice` (`none`, `auto`, `required`, or named function), and `parallel_tool_calls`; preserves exact delta `index` order on streams. | Preserves vendor-specific built-in tool definitions (e.g., native web search or computer use). | Never auto-executes tools inside `/v1/*` (tool governance belongs exclusively to `/mcp` — `DEC-003`). |
| **Structured Outputs** (`json_object`, `strict_json_schema`) | Distinguishes best-effort `json_object` from grammar-constrained `json_schema` (`strict: true`). | Preserves native vendor schema-enforcement structures. | **Zero Silent Downgrade:** If `strict: true` is requested and all route candidates declare `strict_json_schema = false`, returns `400 capability_not_supported`. |
| **Deep Reasoning** (`reasoning`) | Forwards `reasoning_effort` (`low | medium | high`), extracts `reasoning_tokens` into the ledger, and preserves `opaque_signature` across multi-turn conversations. | Preserves native `thinking_delta` SSE events and cryptographic signatures intact. | Never fabricates synthetic chain-of-thought strings when an upstream vendor exposes only reasoning token counts. |
| **SSE Streaming & Cancellation** (`streaming`) | Immediate chunk relay (`p95 < 1.0 ms` target — `TARGET / UNVERIFIED`), `: ping\n\n` heartbeats every `15s`, and `< 500 ms` (`TARGET / UNVERIFIED`) upstream socket close on client disconnect. | Preserves vendor-native SSE event names (`event: content_block_delta`, etc.). | Failure after `>= 1` emitted content chunk emits terminal SSE error `mid_stream_abort` and closes the socket; **splicing a fallback model mid-stream is strictly forbidden** (`TP-BR-0503`, `FMEA-01`). |
| **Realtime WebRTC / WebSocket** (`TP-FR-0106`) | **Out of scope in `v0.1`–`v1.0` (`Later`).** | Not supported over the HTTP gateway. | Immediately returns `400 unsupported_protocol_upgrade`. |

---

## 3. Idempotency Contract (`Idempotency-Key`) & Rate-Limit / Timeout Headers

1. **Atomic Claim & Payload Digest Guard:**
   - When a client supplies `Idempotency-Key` (`<= 128` ASCII chars, `24h` TTL) on non-streaming `POST` requests, A2A task creation, or `/admin/v1/*` mutations, Tanpopo atomically claims `(org_id, virtual_key_id_or_actor_id, idempotency_key)` paired with `SHA-256(request_body)` prior to upstream dispatch.
   - Replaying the same `Idempotency-Key` with a different payload digest fails immediately with `422 idempotency_key_reused_mismatch`.
   - Concurrent duplicate arrival while the first execution is still `in_flight` returns `409 request_in_progress`.
2. **Standardized Request & Response Headers ([`openapi/dataplane-v1.yaml`](../../openapi/dataplane-v1.yaml)):**
   - Request: `X-Tanpopo-Timeout-Ms`, `Idempotency-Key`, `X-Tanpopo-Data-Region`, `x-tanpopo-request-id`, `X-Tanpopo-Trace-Root`, `X-Tanpopo-No-Cache`.
   - Response: `x-tanpopo-request-id`, `X-Tanpopo-Usage-Accuracy` (`EXACT | ESTIMATED | UNMETERED_FLAGGED`), `Retry-After`, `X-RateLimit-Remaining-Requests`, `X-RateLimit-Remaining-Tokens`, `X-RateLimit-Remaining-Streams`.

---

## 4. Authoritative `CanonicalErrorEnvelope` & Error Code Registry

Every pre-stream failure across Data Plane and Control Plane returns `CanonicalErrorEnvelope` (never leaking stack traces or raw credentials):

```json
{
  "error": {
    "code": "capability_not_supported",
    "type": "invalid_request_error",
    "message": "Target route 'prod-default' has no healthy deployment supporting 'strict_json_schema'.",
    "request_id": "req_01jc8x9p2m4n6q8r0s2t4v6w8y",
    "retryable": false
  }
}
```

*(For mid-stream SSE aborts after `>= 1` content chunk has already flushed HTTP `200 OK` headers, Tanpopo emits `event: error` carrying `{"error":{"code":"mid_stream_abort",...,"retryable":false}}` and closes the stream cleanly.)*

| HTTP Status | Canonical Error Code (`error.code`) | `error.type` | Retryable? | Authoritative Semantics |
|---|---|---|---|---|
| `400` | `capability_not_supported` | `invalid_request_error` | `false` | Target deployments lack a required `snake_case` capability (`strict_json_schema`, `vision`, `audio_input`, `document_input`, `tool_calling`, `reasoning`, etc.). |
| `400` | `dialect_mismatch` | `invalid_request_error` | `false` | `/native/{dialect}/*` invoked against a route resolving to a different wire dialect (`TP-FR-0105`, `CT-13`). |
| `400` | `delegation_depth_exceeded` | `invalid_request_error` | `false` | A2A task chain formed a cycle or exceeded `max_delegation_hops` (`<= 5` — `TP-FR-0406`, `CT-23`). |
| `400` | `ssrf_blocked_private_ip` | `invalid_request_error` | `false` | Upstream URL resolved to loopback, RFC1918, or cloud metadata IP without authorized opt-in (`169.254.169.254` always blocked). |
| `400` | `invalid_pagination_cursor` | `invalid_request_error` | `false` | Malformed or cross-org tampered cursor supplied to a paginated endpoint (`AUTH-15`). |
| `400` | `unsupported_protocol_upgrade` | `invalid_request_error` | `false` | Unsupported WebSocket/WebRTC upgrade request (`TP-FR-0106`). |
| `401` | `invalid_virtual_key` | `authentication_error` | `false` | Virtual Key is unknown, expired, or `REVOKED` (`SM-05`). |
| `401` | `invalid_credential_plane` | `authentication_error` | `false` | Virtual Key presented on `/admin/v1/*`, or admin session cookie presented directly on `/v1/*` (`AUTH-07`). |
| `403` | `scope_or_plane_denied` | `permission_error` | `false` | Cross-environment (`staging` key -> `prod` route), model/tool out of key allowlist, or cross-plane violation (`AUTH-04`). |
| `403` | `csrf_validation_failed` | `permission_error` | `false` | Missing or invalid `X-CSRF-Token` / foreign `Origin` on cookie-authenticated `/admin/v1/*` mutation (`AUTH-03`). |
| `403` | `schema_hash_drift_blocked` | `permission_error` | `false` | Upstream MCP tool description/schema mutated away from pinned JCS `schema_hash` (`CT-21`, `INC-06`). |
| `403` | `parameter_hash_mismatch` | `permission_error` | `false` | Replay arguments for an approved `destructive` MCP call differ from the approved 5-tuple JCS digest (`CT-22`). |
| `403` | `self_approval_forbidden` | `permission_error` | `false` | Requester attempted to approve their own submission while `Four-Eyes` is active (`AUTH-12`). |
| `403` | `reserved_namespace_spoofing` | `permission_error` | `false` | External package claimed `^@tanpopo(-[a-z0-9]+)?/` or `official` tier without Tanpopo root signature (`CT-05`). |
| `404` | `resource_not_found` | `not_found_error` | `false` | Resource absent or belongs to another tenant (`AUTH-15` uniform anti-IDOR response). |
| `409` | `optimistic_lock_conflict` | `conflict_error` | `true` *(after re-read)* | Stale `If-Match` ETag on `/admin/v1/*` mutation (returns `current_resource` snapshot — `UX-AC-11`). |
| `409` | `request_in_progress` | `conflict_error` | `true` *(with backoff)* | Duplicate `Idempotency-Key` arrived while original execution is still in flight. |
| `409` | `approval_already_decided_or_expired` | `conflict_error` | `false` | HITL approval request was already consumed, decided, or past `expires_at`. |
| `410` | `bootstrap_already_completed` | `conflict_error` | `false` | `/admin/v1/bootstrap/complete` invoked after initial instance setup permanently sealed (`SCR-01`). |
| `422` | `idempotency_key_reused_mismatch` | `invalid_request_error` | `false` | Existing `Idempotency-Key` replayed with a modified request body digest. |
| `422` | `policy_relaxation_forbidden` | `invalid_request_error` | `false` | Child policy attempted to widen a parent `Organization`/`Project` ceiling (`TP-FR-0701`). |
| `429` | `project_budget_exceeded` | `rate_limit_error` | `false` | Settled spend + carryover + active pre-holds reached hard `micro-USD` budget ceiling (`CT-29`, `INC-01`). |
| `429` | `rate_limit_exceeded` | `rate_limit_error` | `true` *(honor `Retry-After`)* | Caller exceeded configured `RPM`, `TPM`, or `Max Concurrent Streams` limit (`TP-FR-0702`). |
| `429` | `auth_rate_limited` | `rate_limit_error` | `true` *(honor `Retry-After`)* | Brute-force protection triggered on `/admin/v1/auth/login` (`AUTH-02`). |
| `502` | `extension_execution_fault` | `extension_error` | `true` *(pre-stream backup)* | Tier-2 Wasm / Tier-3 Sidecar extension trapped, timed out (`> 15 ms` / `> 5 ms`), or hit `16 MiB` OOM (`FMEA-05`). |
| `502` | `upstream_malformed_response` | `upstream_error` | `false` | Upstream AI provider returned unparseable JSON or truncated non-streaming tool-call brackets (`CT-09`). |
| `503` | `no_compliant_deployment_available` | `residency_unavailable_error` | `true` *(with backoff)* | Zero healthy deployments satisfy hard `Data Residency` and policy constraints (`Fail-Closed` — `INC-04`). |
| `503` | `extension_quarantined` | `extension_error` | `false` | Backing extension is locked in `QUARANTINED` state (`SM-01`, `INC-03`). |
| `503` | `gateway_overloaded` | `upstream_error` | `true` *(honor `Retry-After`)* | Protective node load shedding triggered at `90%` stream ceiling or `85%` `GOMEMLIMIT` (`SPEC-QA-03`). |
| `504` | `ambiguous_side_effect_timeout` | `side_effect_timeout_error` | **`false`** | `mutating` / `destructive` MCP tool timed out post-dispatch (`0` automatic retries — `FMEA-02`). |
