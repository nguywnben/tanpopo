# API, Multimodal & Wire Error Compatibility Specification — Tanpopo

> **Wire Protocol Specification Document (ASTRA Phase A04 — `DRAFT / ILLUSTRATIVE CANDIDATE SPEC`)**
> - **Document ID:** `SPEC-EXT-06` | **Version:** `0.1.0-step04-draft` | **Last Updated:** `2026-11-11`
> - **Authoring Role:** Platform Architect & Security-Minded SDK Designer
> - **Standardization Status Statement:** The HTTP header conventions, mid-stream SSE abort events, and canonical error taxonomy below are **illustrative candidate designs (`ILLUSTRATIVE — PRE-ADR`)** serving as the baseline for the contract verification suite in [`contract-test-matrix.md`](../quality/contract-test-matrix.md).
> - **Requirements Traceability:** `TP-FR-0101`..`0106`, `TP-FR-0601`..`0603`, `TP-FR-0702`, `TP-FR-0802`, `TP-NFR-0101`..`0102`, `TP-NFR-1002`, `TP-BR-0503`..`0505`, `TP-BR-0703`.

---

## 1. Multi-Plane Client Ingress Surface (`Ingress Surface Overview`)

So mainstream SDK libraries can connect immediately by changing only `base_url` and `api_key` without conflating the four distinct architectural planes (`DEC-003`), Tanpopo strictly partitions its ingress route prefixes:

| Plane | Illustrative Route Prefixes | Drop-In Compatible Client SDKs & Tools | Tanpopo `Virtual Key` Delivery |
|---|---|---|---|
| **1. Unified Provider Ingress** | `/v1/chat/completions`<br/>`/v1/responses`<br/>`/v1/embeddings`<br/>`/v1/models` | OpenAI Python/TS/Go SDKs, LangChain, LlamaIndex, Vercel AI SDK, and OpenAI-compatible coding IDEs. | `Authorization: Bearer vk_...` |
| **2. Lossless Native Passthrough** | `/native/anthropic/v1/messages`<br/>`/native/gemini/v1beta/...`<br/>`/native/<adapter_slug>/...` | Official Anthropic SDK, official Google GenAI SDK, Claude Code, and tools requiring newly launched vendor-specific features. | Accepts either the native SDK's default header (`x-api-key` / `x-goog-api-key`) **or** `x-tanpopo-virtual-key` (Core strips the Virtual Key completely before injecting the upstream provider's real credential!). |
| **3. MCP Governance Hub** | `/mcp` *(or `/mcp/<workspace_slug>`)* | Standard **MCP Streamable HTTP** / SSE clients inside IDEs and agent frameworks. | `Authorization: Bearer vk_...` (or OAuth 2.1 tokens scoped to `aud` = Tanpopo Hub). |
| **4. A2A Connectivity Plane** | `/a2a/agents` & `/a2a/tasks` | Autonomous agents compliant with the open **Agent2Agent (A2A)** protocol. | `Authorization: Bearer vk_...` (or short-lived attenuated task delegation keys). |
| **5. Control Plane & Ops** | `/admin/v1/...`, `/healthz`, `/readyz` | `tanpopo` CLI, Web Console (Astryx), and CI/CD automation pipelines. | Secure `httpOnly` admin sessions / OIDC / dedicated Admin Service Tokens (`API Client` inference keys are strictly barred from calling `/admin/v1/*`). |

---

## 2. Per-Capability Model Compatibility Matrix & Practical Boundaries

| Capability | Normalized Behavior on Unified Path (`/v1/*`) | Behavior on Native Dialect Path (`/native/*`) | `Fail-Loudly` Rejection & Unsupported Boundaries |
|---|---|---|---|
| **Chat & Responses** | Bidirectional mapping of roles (`system`/`developer`, `user`, `assistant`, `tool`), `max_output_tokens`, `temperature`, `top_p`, and `stop`. | Preserves 100% of the vendor's native request/response block structure untouched. | Sending a required parameter that the target deployment declares unsupported → returns `400 capability_not_supported` (`TP-BR-0504`). |
| **Embeddings** | Supports single-string or string-array inputs, `encoding_format` (`float` / `base64`), and dimensionality reduction via `dimensions` (when supported by the model). | Preserves the vendor's native embedding payload structure. | Rejects with `400` if the route targets a text-generation model or a model that does not support custom `dimensions`. |
| **Images, Audio & Documents (`Vision / Audio / PDF`)** | Normalizes multimodal content blocks (`image_url`, `input_audio`, `file`/`document`); enforces request body size limits (default `<= 32 MB`) and maximum attachment counts per `Capability Profile`. | Forwards native multimodal file/block structures losslessly (e.g., Anthropic cache-annotated PDF blocks). | **SSRF Guardrail:** By default, Tanpopo either forwards public HTTPS image URLs for upstream fetching or requires `API Client` `base64` payloads; if Gateway-assisted URL-to-`base64` fetching is enabled, every URL must pass a strict anti-SSRF filter (`TP-FR-0702`). |
| **Tool Calling** | Normalizes `tools`, `tool_choice` (`auto`, `none`, `required`, or named function pinning), and `parallel_tool_calls`; preserves exact argument delta chunk ordering during streaming. | Preserves vendor-specific built-in tool definitions (e.g., native computer-use or web-search tools). | Never auto-executes tools inside the `/v1/*` inference plane (tool execution is governed separately on the `/mcp` plane to preserve the 4-plane boundary!). |
| **Structured Outputs (`JSON / Strict Schema`)** | Strictly distinguishes two assurance levels: `json_object` (basic JSON mode) and `json_schema` with `strict: true` (constrained grammar enforcement). | Preserves the upstream provider's native schema-enforcement structure. | **Zero Silent Downgrade:** If `strict: true` is requested and all candidate deployments in the route declare `strictJsonSchema = false`, immediately returns `400 capability_not_supported`. |
| **Deep Reasoning (`Reasoning / Thinking`)** | Forwards reasoning budget controls (`reasoning_effort`), extracts `reasoning_tokens` into the ledger, and preserves encrypted reasoning blocks/signatures (`encrypted_content` / `signature`) so multi-turn follow-ups succeed seamlessly. | Preserves every native `thinking_delta` stream event and cryptographic thinking-block signature intact. | Never forces providers that redact raw chain-of-thought text to emit synthetic thinking strings when the upstream API exposes only token counts. |
| **SSE Streaming (`Streaming`) & Stream Cancellation** | Relays event chunks immediately upon arrival (`< 1 ms` target added relay latency); normalizes upstream keep-alives into standard `: ping\n\n` SSE comments; closes upstream streams within `< 500 ms` on client disconnect. | Preserves each vendor's native SSE event names (`event: content_block_delta`, etc.). | Upstream stream failure after `>= 1` content chunk has been emitted → emits a canonical `mid_stream_abort` error event frame and closes the connection; **strictly forbids splicing a fallback model onto a partial stream** (`TP-BR-0503`). |
| **Bidirectional Realtime Voice/Video (`Realtime WebRTC/WS`)** | **Out of scope in `v0.1`–`v1.0` (`TP-FR-0106` — `Later`).** | Not supported over the HTTP REST gateway. | Immediately returns `400 unsupported_protocol_upgrade` rather than hanging the connection. |

---

## 3. Idempotency Contract (`Idempotency-Key`) & Rate-Limit Headers (`Rate-Limit Headers`)

Applicable to non-streaming requests requiring protection against duplicate billing over flaky networks (as well as A2A task creation and administrative mutations):
1. **Atomic Claim & Payload Hash Guard (`Atomic Claim & Payload Hash Guard`):**
   - When an `API Client` supplies an `Idempotency-Key` header (client-generated per distinct business intent), Tanpopo atomically claims the tuple `(tenant_id, virtual_key_id, idempotency_key)` alongside the `SHA-256` digest of the request payload **before** dispatching any upstream call.
   - If the same `Idempotency-Key` is replayed with a payload whose `SHA-256` digest differs from the original claim → immediately rejects with `422 idempotency_key_reused_mismatch`.
   - If the initial request is still `in_flight` when a duplicate arrives → returns `409 request_in_progress` to prevent duplicate upstream execution.
2. **Standardized Tanpopo Response Headers:**
   - `x-tanpopo-request-id`: System-wide unique correlation identifier.
   - `x-tanpopo-usage-accuracy`: `EXACT` | `ESTIMATED` | `UNMETERED_FLAGGED` (exposes metering fidelity directly to the caller).
   - `retry-after`, `x-ratelimit-remaining-requests`, `x-ratelimit-remaining-tokens`: Normalized quota headers enabling client SDKs to back off deterministically.

---

## 4. Unified Error Envelope & Machine-Readable Error Taxonomy (`Canonical Error Taxonomy`)

Every pre-stream failure returns a single canonical JSON envelope (never leaking internal stack traces or raw credential strings):

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

*(Note on mid-stream SSE aborts after the first chunk: because HTTP `200 OK` headers have already been flushed, Tanpopo emits a terminal SSE event containing an `error` object with `"code": "mid_stream_abort"` and immediately closes the TCP stream).*

| HTTP Status | Canonical Error Code (`error.code`) | Retryable? | Verifiable Semantics |
|---|---|---|---|
| `400` | `capability_not_supported` | No | Request requires a capability (`vision`, `audio_input`, `strict_json_schema`, etc.) unsupported by target deployments (`TP-FR-0102`). |
| `400` | `dialect_mismatch` | No | Invoked `/native/<dialect>` against a route resolving to a deployment of a different wire dialect (`TP-FR-0105`). |
| `400` | `delegation_depth_exceeded` | No | A2A inter-agent delegation chain exceeded maximum hop count or formed a cycle (`TP-FR-0406`). |
| `401` | `invalid_virtual_key` | No | Virtual Key does not exist, has expired, or has been revoked (`SM-05`). |
| `403` | `scope_or_plane_denied` | No | Virtual Key attempted cross-environment (`dev` → `prod`) or cross-plane (`Provider` → `MCP`/`A2A` — `TP-BR-0104`) access. |
| `403` | `parameter_hash_mismatch` | No | Arguments supplied to a `destructive` MCP tool do not match the human-approved JCS parameter digest (`TP-BR-0602`). |
| `409` | `version_conflict` / `request_in_progress` | Yes (after re-read / backoff) | Stale configuration write (`ETag` mismatch) or duplicate `Idempotency-Key` currently `in_flight`. |
| `422` | `idempotency_key_reused_mismatch` | No | Reused an existing `Idempotency-Key` with a different request payload digest. |
| `429` | `rate_limit_exceeded` / `project_budget_exceeded` | Per `Retry-After` / No | Hit RPM/TPM rate limits or exhausted a hard hierarchical budget ceiling (`TP-BR-0701`). |
| `502` | `upstream_malformed_response` | Policy-dependent | Upstream provider returned a contract-violating payload or truncated/malformed tool-call JSON. |
| `503` | `no_compliant_deployment_available` | Yes (with backoff) | Zero healthy deployments satisfy hard `Data Residency` constraints (`Fail-Closed` — `TP-BR-0501`). |
| `503` | `extension_quarantined` | No | The extension serving this connection is under emergency security quarantine (`SM-01`). |
| `504` | `ambiguous_side_effect_timeout` | **Auto-retry FORBIDDEN** | A `mutating`/`destructive` MCP tool call timed out after dispatch; caller must reconcile actual upstream state (`TP-BR-0603`). |
