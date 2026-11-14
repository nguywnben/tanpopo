# API & Wire Contract Design Specification: OpenAPI, JSON Schema & Protobuf — Tanpopo

> **Canonical Interface Contract Specification (Synchronized in `TP-IMP-0002` — Spec Closure Batch B)**
> - **Document ID:** `SPEC-API-01` | **Version:** `0.2.0-m0-closure` | **Last Updated:** `2026-11-14`
> - **Authoring Role:** Principal API & Platform Architect
> - **Authoritative Machine-Readable Artifacts (Tier-2 Source of Truth):**
>   - Buf Workspace Configuration: [`buf.yaml`](../../buf.yaml) (`version: v2`)
>   - Host–Guest / Sidecar Protobuf Contract: [`proto/tanpopo/extension/v1/runtime.proto`](../../proto/tanpopo/extension/v1/runtime.proto)
>   - Data Plane & Probes OpenAPI `3.1.0`: [`openapi/dataplane-v1.yaml`](../../openapi/dataplane-v1.yaml)
>   - Admin Control Plane OpenAPI `3.1.0`: [`openapi/admin-v1.yaml`](../../openapi/admin-v1.yaml)
> - **Measurement Honesty (`COND-PO-01`):** Every quantitative latency, timeout propagation, and propagation target in this specification (`< 500 ms`, `< 1 s`, `< 5 s`, `<= 15 ms`, `<= 5 ms`) is explicitly **`TARGET / UNVERIFIED`** pending empirical benchmark verification in `SPIKE-01..04` and `M5`.
> - **Requirements Traceability:** `TP-FR-0101`..`0105`, `TP-FR-0201`..`0206`, `TP-FR-0301`..`0308`, `TP-FR-0401`..`0406`, `TP-FR-0501`..`0504`, `TP-FR-0601`..`0604`, `TP-FR-0701`..`0705`, `TP-FR-0801`..`0806`, `TP-FR-0901`..`0904`, `TP-FR-1001`..`1005`, `CR-05`, `CR-08`, `CR-10`.

---

## 1. Versioning, Headers, Concurrency, Pagination & Multimodal Streaming Conventions

1. **Three Independently Versioned Contract Boundaries:**
   - **Data Plane Ingress (`/v1/*`, `/native/{dialect}/{upstreamPath}`, `/mcp`, `/a2a/*` — [`openapi/dataplane-v1.yaml`](../../openapi/dataplane-v1.yaml)):** Maintains wire compatibility with official OpenAI SDKs on `/v1/*`, vendor-native SDKs on `/native/*`, **MCP Specification `2025-03-26` (Streamable HTTP)** on `/mcp`, and **Agent2Agent (`A2A`, `SM-09`)** on `/a2a/*`. All Tanpopo governance extensions travel via optional headers so client static type checkers never break.
   - **Control Plane Admin API (`/admin/v1/*` — [`openapi/admin-v1.yaml`](../../openapi/admin-v1.yaml)):** Strictly isolated from Data Plane Virtual Keys (`vk_tp_*` presented on `/admin/v1/*` fails immediately with `401 invalid_credential_plane` — `AUTH-07`). Supports optional `X-Tanpopo-API-Version: 2026-11-01`, mandatory `X-CSRF-Token` verification on cookie-authenticated mutations (`AUTH-03`), `ETag` + `If-Match` optimistic concurrency control returning `409 optimistic_lock_conflict` (including `current_resource` for side-by-side diffing — `UX-AC-11`), and `org_id`-bound HMAC-signed cursor pagination (`limit <= 200`, default `50`, `cursor` -> `{ data, next_cursor, has_more }`). All action endpoints use normalized slash sub-resources (e.g., `/admin/v1/routes/explain`, `/admin/v1/extensions/install`, `/admin/v1/virtual-keys/{keyId}/revoke`) with zero `:action` path drift.
   - **Extension Host–Guest Runtime (`tanpopo.extension.v1` — [`runtime.proto`](../../proto/tanpopo/extension/v1/runtime.proto)):** Negotiated via `compatibility.contractVersion` in the extension `Manifest`, guaranteeing `N-1` minor backward compatibility during `0.x` and a minimum `12-month` deprecation window across `1.x` (`TP-NFR-0902`).

2. **Standardized Request & Response HTTP Headers:**
   - **Request Headers:**
     - `Authorization: Bearer vk_tp_<env>_<32_base62>` (Unified `/v1/*`, `/mcp`, `/a2a/*`, and `/native/*`), or native SDK headers `x-api-key`, `x-goog-api-key`, `X-Tanpopo-Virtual-Key` on `/native/{dialect}/{upstreamPath}` (stripped at ingress before `BlindBroker` injects upstream credentials).
     - `x-tanpopo-request-id`: Optional caller correlation ID (normalized/minted as `req_<26_crockford_base32>` if absent).
     - `X-Tanpopo-Timeout-Ms`: Per-request deadline ceiling in milliseconds (clamped by `Route`/`Deployment` maximum, `<= 300000 ms`).
     - `Idempotency-Key`: Client-generated idempotency key (`<= 128` ASCII characters, `24-hour` TTL) bound atomically to `(org_id, virtual_key_id, idempotency_key, sha256(payload))`.
     - `X-Tanpopo-Data-Region`: Narrowing residency constraint (`EU | US | APAC | ON_PREM`).
     - `X-Tanpopo-Trace-Root`: Cross-plane root correlation ID across `A2A -> Provider -> MCP` execution trees.
     - `X-Tanpopo-No-Cache`: Bypasses opt-in exact/semantic response caching (`v0.3+`).
     - `X-CSRF-Token` & `If-Match`: Control Plane CSRF and optimistic lock guards on `/admin/v1/*`.
   - **Response Headers:**
     - `x-tanpopo-request-id`: Canonical gateway correlation identifier emitted on every `2xx`/`4xx`/`5xx` response.
     - `X-Tanpopo-Usage-Accuracy`: `EXACT | ESTIMATED | UNMETERED_FLAGGED`.
     - `Retry-After`, `X-RateLimit-Remaining-Requests`, `X-RateLimit-Remaining-Tokens`, `X-RateLimit-Remaining-Streams`: Deterministic client backoff and quota visibility headers.
     - `ETag`: Current resource revision tag (e.g., `"v4"`) on mutable `/admin/v1/*` resources.

3. **Streaming Heartbeats, Cancellation & Out-of-Band Multimodal `blob_ref` Streaming (`CR-05`, `CR-09`):**
   - Over `text/event-stream` (SSE), Tanpopo emits `: ping\n\n` every `15 seconds` during upstream reasoning silence; upon client TCP close or HTTP/2 `RST_STREAM`, Host Core cancels the sandbox context and closes the upstream socket within **`< 500 ms` (`TARGET / UNVERIFIED`)** (`TP-FR-0104`).
   - For OpenAI-compatible adapters when `stream: true`, Tanpopo automatically injects `stream_options: {"include_usage": true}` upstream (`CR-09`) to maximize `EXACT` post-stream settlement.
   - **Zero `16 MiB` Wasm OOM on Multimodal Requests (`NEG-1`):** Ingress accepts multimodal requests up to `32 MiB` (configurable per policy), extracts any inline decoded media payload `> 64 KiB` into Host streaming buffers, and replaces it inside `NormalizedInferenceRequestEnvelope` with a compact `blob_ref` descriptor (`blob_ref://<request_id>/<part_index>`). The Wasm guest receives only compact JSON metadata (`< 256 KiB`), and Host splices blob bytes directly onto the outbound TLS socket on egress.

---

## 2. Canonical OpenAPI `3.1.0` Surface Index (`dataplane-v1.yaml` & `admin-v1.yaml`)

### 2.1. Data Plane & Probes Endpoints ([`openapi/dataplane-v1.yaml`](../../openapi/dataplane-v1.yaml))

| Plane / Tag | Method & Path | Summary & Key Contract Guarantees |
|---|---|---|
| **Probes** | `GET /healthz` | Unauthenticated process liveness probe; never queries SQLite/PostgreSQL, Valkey, or upstreams. |
| **Probes** | `GET /readyz` | Readiness probe (`200` ready; `503` when cold-booting, draining on `SIGTERM`, or revocation lag `> 10s`). |
| **Probes** | `GET /metrics` | Bounded Prometheus exposition (`:9090`); allows `{ org_id, project_id, adapter_id, logical_model, status_class, error_code, accuracy_tier }` with high-tenant `org_id`/`project_id` opt-out toggle (`0` `request_id`/`virtual_key_id`/`principal_id` labels). |
| **Unified (`/v1`)** | `POST /v1/chat/completions` | Sync JSON or SSE chat/multimodal/tool/reasoning inference; auto-injects `stream_options.include_usage=true`; terminal `mid_stream_abort` SSE event on post-chunk-1 failure (`0` mid-stream fallback). |
| **Unified (`/v1`)** | `POST /v1/responses` | OpenAI Responses-compatible multimodal, reasoning, and tool-calling endpoint (Sync JSON or SSE). |
| **Unified (`/v1`)** | `POST /v1/embeddings` | Single or batch vector embeddings (`float` / `base64`, optional `dimensions` validated against capability profile). |
| **Unified (`/v1`)** | `GET /v1/models` | Lists Logical Models & Routes permitted in the caller Virtual Key's `Environment` with `snake_case` capabilities. |
| **Unified (`/v1`)** | `POST /v1/rerank` | Document reranking endpoint returning relevance-ordered indices and search-unit usage (`v0.2` per `CR-01`). |
| **Native** | `POST /native/{dialect}/{upstreamPath}` | Lossless vendor wire passthrough (`anthropic`, `gemini`, custom adapter prefix); rejects cross-dialect targets with `400 dialect_mismatch`. |
| **MCP Hub** | `POST /mcp`, `GET /mcp`, `DELETE /mcp` | **MCP Specification `2025-03-26` Streamable HTTP** (`v0.2`); async HITL `destructive` approvals return standard HTTP `200` JSON-RPC (`isError: true`) bound to 5-tuple JCS hash (`CR-08`; IDE compatibility verified via `CT-36` per `COND-PO-04`). |
| **A2A Plane** | `GET /a2a/agents`, `POST /a2a/tasks`, `GET /a2a/tasks/{taskId}`, `POST /a2a/tasks/{taskId}/cancel` | Permission-scoped `AgentCard` discovery and stateful `SM-09` task delegation (`hop_count <= 5`, `400 delegation_depth_exceeded` — `v0.3`). |

### 2.2. Control Plane Admin API Endpoints ([`openapi/admin-v1.yaml`](../../openapi/admin-v1.yaml) — Backing `SCR-01..16`)

Every `/admin/v1/*` route in [`screen-specifications.md`](../ux/screen-specifications.md) maps 1-to-1 to [`openapi/admin-v1.yaml`](../../openapi/admin-v1.yaml):

| Functional Group | Backing Screens | Normalized REST Operations (`Method + Path`) |
|---|---|---|
| **A. Bootstrap & Auth** | `SCR-01` | `GET /admin/v1/bootstrap/status`, `POST /admin/v1/bootstrap/complete` (`410 bootstrap_already_completed` once sealed; supports `allow_private_ip` + atomic Connection/Deployment/Route/Key), `POST /admin/v1/auth/login`, `GET /admin/v1/auth/oidc/start`, `GET /admin/v1/auth/oidc/callback`, `GET /admin/v1/auth/session`, `POST /admin/v1/auth/logout` |
| **B. Tenancy & RBAC** | `SCR-13`, `SCR-16` | `GET /admin/v1/organizations`, `POST /admin/v1/organizations` (`OPERATOR`), `DELETE /admin/v1/organizations/{orgId}`, `GET /admin/v1/projects`, `POST /admin/v1/projects`, `DELETE /admin/v1/projects/{projectId}`, `GET /admin/v1/projects/{projectId}/environments`, `POST /admin/v1/projects/{projectId}/environments`, `GET /admin/v1/members`, `POST /admin/v1/members`, `PATCH /admin/v1/members/{memberId}/roles` |
| **C. Virtual Keys** | `SCR-01`, `SCR-13` | `GET /admin/v1/virtual-keys`, `POST /admin/v1/virtual-keys`, `POST /admin/v1/projects/{projectId}/virtual-keys`, `PATCH /admin/v1/virtual-keys/{keyId}`, `POST /admin/v1/virtual-keys/{keyId}/rotate`, `POST /admin/v1/virtual-keys/{keyId}/revoke` |
| **D. Overview & Alerts** | `SCR-02` | `GET /admin/v1/overview/summary`, `GET /admin/v1/alerts/active` |
| **E. Adapters, Vault & Connections** | `SCR-01`, `SCR-03`, `SCR-04` | `GET /admin/v1/provider-adapters`, `GET /admin/v1/provider-adapters/{adapterId}`, `GET /admin/v1/secrets`, `POST /admin/v1/secrets` (write-only), `DELETE /admin/v1/secrets/{credentialId}`, `GET /admin/v1/connections`, `POST /admin/v1/connections`, `PATCH /admin/v1/connections/{connectionId}`, `DELETE /admin/v1/connections/{connectionId}`, `POST /admin/v1/connections/{connectionId}/test`, `POST /admin/v1/connections/{connectionId}/oauth/start`, `GET /admin/v1/oauth/callback` |
| **F. Models & Deployments** | `SCR-01`, `SCR-02`, `SCR-05` | `GET /admin/v1/models`, `POST /admin/v1/models`, `GET /admin/v1/deployments`, `POST /admin/v1/deployments`, `PATCH /admin/v1/deployments/{deploymentId}`, `DELETE /admin/v1/deployments/{deploymentId}` |
| **G. Marketplace & Extensions** | `SCR-06`, `SCR-07` | `GET /admin/v1/marketplace/catalog`, `POST /admin/v1/extensions/install`, `GET /admin/v1/extensions/installations`, `GET /admin/v1/extensions/{installationId}/permission-diff`, `POST /admin/v1/extensions/{installationId}/approve-permissions`, `POST /admin/v1/extensions/{installationId}/upgrade`, `POST /admin/v1/extensions/{installationId}/rollback`, `DELETE /admin/v1/extensions/{installationId}`, `GET /admin/v1/extensions/lockfile`, `PUT /admin/v1/organizations/{orgId}/extension-policies` |
| **H. Routes & Policies** | `SCR-01`, `SCR-08`, `SCR-09`, `SCR-11`, `SCR-15` | `GET /admin/v1/routes`, `POST /admin/v1/routes`, `PUT /admin/v1/routes/{routeId}`, `DELETE /admin/v1/routes/{routeId}`, `POST /admin/v1/routes/explain`, `GET /admin/v1/policies/effective`, `PUT /admin/v1/policies/{policyId}` |
| **I. MCP Hub & A2A Registry** | `SCR-10` | `GET /admin/v1/mcp/servers`, `POST /admin/v1/mcp/servers`, `POST /admin/v1/mcp/servers/{serverId}/sync-tools`, `POST /admin/v1/mcp/tools/{toolId}/approve-schema`, `GET /admin/v1/a2a/agents`, `POST /admin/v1/a2a/agents` |
| **J. Traces, Budgets, Chargeback & Webhooks** | `SCR-11`, `SCR-12`, `SCR-14` | `GET /admin/v1/traces`, `GET /admin/v1/traces/{requestId}`, `GET /admin/v1/traces/{requestId}/debug-payload`, `GET /admin/v1/budgets`, `PUT /admin/v1/budgets/{budgetId}`, `GET /admin/v1/usage/summary`, `GET /admin/v1/usage/export`, `GET /admin/v1/webhooks`, `POST /admin/v1/webhooks` |
| **K. Approvals, Audit & Kill-Switches** | `SCR-07`, `SCR-14` | `GET /admin/v1/approvals`, `POST /admin/v1/approvals/{approvalId}/decide`, `GET /admin/v1/audit-events`, `POST /admin/v1/audit-events/verify`, `POST /admin/v1/kill-switches`, `GET /admin/v1/blast-radius` |
| **L. Playground, DevTools & Ops** | `SCR-15`, `SCR-16` | `POST /admin/v1/playground/sessions`, `GET /admin/v1/openapi.json`, `POST /admin/v1/diagnostics/smoke-test`, `POST /admin/v1/diagnostics/support-bundle`, `GET /admin/v1/ops/cluster-status`, `POST /admin/v1/ops/backups`, `POST /admin/v1/ops/upgrades/preflight`, `PUT /admin/v1/settings/retention` |

---

## 3. Canonical JSON Schema for `NormalizedInferenceRequestEnvelope` (`CR-05` — Multimodal `blob_ref` & Full Sampling/Tool Coverage)

Before invoking `TransformRequest` on a Tier-1/Tier-2/Tier-3 adapter, Host Core normalizes `/v1/*` requests into the compact envelope below (`< 256 KiB`). Decoded media attachments exceeding `64 KiB` are stored in Host streaming buffers and referenced exclusively via `blob_ref` handles (`blob_ref://<request_id>/<part_index>`), preventing `32 MiB` multimodal payloads from entering `16 MiB` Wasm linear memory (`NEG-1`) and keeping Phase-1 text token estimation (`ceil(utf8_byte_len(text_and_tools_only) / 3)`) decoupled from multimodal unit schedules (`media_usage_json`):

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://tanpopo.dev/schemas/v0.1/normalized-inference-request.schema.json",
  "title": "NormalizedInferenceRequestEnvelope",
  "type": "object",
  "additionalProperties": false,
  "required": ["contract_version", "request_id", "target_model", "modality", "stream"],
  "properties": {
    "contract_version": { "const": "0.1" },
    "request_id": { "type": "string", "pattern": "^req_[0-9a-hjkmnp-tv-z]{26}$" },
    "target_model": { "type": "string", "minLength": 1 },
    "modality": { "enum": ["chat", "responses", "embeddings", "rerank"] },
    "stream": { "type": "boolean" },
    "stream_options": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "include_usage": { "type": "boolean", "default": true }
      }
    },
    "max_output_tokens": { "type": "integer", "minimum": 1 },
    "temperature": { "type": "number", "minimum": 0, "maximum": 2 },
    "top_p": { "type": "number", "minimum": 0, "maximum": 1 },
    "stop": {
      "type": "array",
      "maxItems": 4,
      "items": { "type": "string" }
    },
    "reasoning_effort": { "enum": ["low", "medium", "high"] },
    "dimensions": { "type": "integer", "minimum": 1 },
    "encoding_format": { "enum": ["float", "base64"] },
    "tools": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["type", "function"],
        "additionalProperties": false,
        "properties": {
          "type": { "const": "function" },
          "function": {
            "type": "object",
            "required": ["name"],
            "additionalProperties": false,
            "properties": {
              "name": { "type": "string", "minLength": 1 },
              "description": { "type": "string" },
              "strict": { "type": "boolean" },
              "parameters": { "type": "object" }
            }
          }
        }
      }
    },
    "tool_choice": {
      "oneOf": [
        { "enum": ["none", "auto", "required"] },
        {
          "type": "object",
          "required": ["type", "function"],
          "additionalProperties": false,
          "properties": {
            "type": { "const": "function" },
            "function": {
              "type": "object",
              "required": ["name"],
              "additionalProperties": false,
              "properties": { "name": { "type": "string", "minLength": 1 } }
            }
          }
        }
      ]
    },
    "parallel_tool_calls": { "type": "boolean" },
    "response_format": {
      "type": "object",
      "required": ["type"],
      "additionalProperties": false,
      "properties": {
        "type": { "enum": ["text", "json_object", "json_schema"] },
        "json_schema": {
          "type": "object",
          "required": ["name", "schema"],
          "additionalProperties": false,
          "properties": {
            "name": { "type": "string", "minLength": 1 },
            "description": { "type": "string" },
            "strict": { "type": "boolean" },
            "schema": { "type": "object" }
          }
        }
      }
    },
    "messages": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["role", "content_blocks"],
        "additionalProperties": false,
        "properties": {
          "role": { "enum": ["system", "developer", "user", "assistant", "tool"] },
          "tool_call_id": { "type": "string" },
          "content_blocks": {
            "type": "array",
            "items": {
              "type": "object",
              "required": ["kind"],
              "additionalProperties": false,
              "properties": {
                "kind": { "enum": ["text", "image", "audio", "document", "tool_call", "tool_result", "reasoning_opaque"] },
                "text": { "type": "string" },
                "inline_data_uri": {
                  "type": "string",
                  "maxLength": 87384,
                  "description": "Permitted ONLY for small payloads <= 64 KiB decoded; all payloads > 64 KiB MUST use blob_ref."
                },
                "external_https_url": {
                  "type": "string",
                  "format": "uri",
                  "description": "Sanitized public HTTPS URL when upstream fetch is permitted by policy."
                },
                "blob_ref": {
                  "type": "object",
                  "required": ["blob_id", "uri", "mime_type", "byte_length", "sha256_hex"],
                  "additionalProperties": false,
                  "description": "Out-of-band Host streaming handle for payloads > 64 KiB (never enters 16 MiB Wasm RAM).",
                  "properties": {
                    "blob_id": { "type": "string" },
                    "uri": { "type": "string", "pattern": "^blob_ref://req_[0-9a-hjkmnp-tv-z]{26}/[0-9]+$" },
                    "mime_type": { "type": "string" },
                    "byte_length": { "type": "integer", "minimum": 1, "maximum": 33554432 },
                    "sha256_hex": { "type": "string", "pattern": "^[a-f0-9]{64}$" }
                  }
                },
                "tool_call": {
                  "type": "object",
                  "required": ["id", "name", "arguments_json"],
                  "additionalProperties": false,
                  "properties": {
                    "id": { "type": "string" },
                    "name": { "type": "string" },
                    "arguments_json": { "type": "string" }
                  }
                },
                "opaque_signature": {
                  "type": "string",
                  "description": "Preserves encrypted/signed reasoning block integrity across multi-turn conversations."
                }
              }
            }
          }
        }
      }
    },
    "embedding_inputs": {
      "type": "array",
      "items": { "type": "string" }
    },
    "rerank_spec": {
      "type": "object",
      "required": ["query", "documents"],
      "additionalProperties": false,
      "properties": {
        "query": { "type": "string" },
        "documents": { "type": "array", "items": { "type": "string" } },
        "top_n": { "type": "integer", "minimum": 1 },
        "return_documents": { "type": "boolean" }
      }
    },
    "secret_slot_refs": {
      "type": "object",
      "description": "Contains ONLY opaque secret slot handles (e.g., primary_api_key -> slot_handle_01); MUST NEVER contain plaintext secrets!",
      "additionalProperties": { "type": "string" }
    }
  }
}
```

---

## 4. Host–Guest & Sidecar Protobuf Contract ([`proto/tanpopo/extension/v1/runtime.proto`](../../proto/tanpopo/extension/v1/runtime.proto))

The canonical `proto3` definition extracted to [`proto/tanpopo/extension/v1/runtime.proto`](../../proto/tanpopo/extension/v1/runtime.proto) (governed by [`buf.yaml`](../../buf.yaml) `version: v2`) enforces four security and reliability invariants (`CR-05`):
1. **Two-Phase Blind Validation (`BuildValidationProbe` -> `VerifyValidationResult`):** Extensions never open raw network sockets during connection checks; Host executes the returned `UpstreamHttpSpec` through `BlindBroker` + `SSRFGuard` and passes only scrubbed status/headers/body back to `VerifyValidationResult`.
2. **Complete Sync, Stream & Native Coverage:** Defines `TransformRequest`, `TransformResponse` (for synchronous chat, responses, embeddings, rerank), `TransformStreamChunk`, `NormalizeError`, `ExtractUsage` (returning `NormalizedUsage` with `MediaUsage`), and `InspectNativeRequest` (for `/native/{dialect}/{upstreamPath}`).
3. **Structured Host Signing & Hard `Cookie` Ban (`DEC-004` / `NEG-3`):** `HostCryptoSignSpec` supports `HMAC_SHA256`, `AWS_SIGV4`, and `RSA_SHA256` inside Host Core; `CredentialInjectInstruction.field_name` unconditionally bans `Cookie` and `Set-Cookie` on outbound injection while Host strips `Set-Cookie`/`Cookie` echoes on inbound responses; `scoped_secret_unseal` is completely eliminated from Tier-1 and Tier-2 Wasm.
4. **Out-of-Band `BlobRef`:** Multimodal payloads `> 64 KiB` cross the boundary strictly as `BlobRef` metadata and are spliced onto the egress stream by Host Core outside Wasm's `16 MiB` memory ceiling.

```protobuf
syntax = "proto3";

package tanpopo.extension.v1;

option go_package = "github.com/nguywnben/tanpopo/gen/proto/extension/v1;extensionv1";

service ExtensionRuntimeService {
  rpc BuildValidationProbe(ValidationProbeInput) returns (UpstreamHttpSpec);
  rpc VerifyValidationResult(ValidationResultInput) returns (ValidationVerdict);
  rpc TransformRequest(TransformRequestInput) returns (UpstreamHttpSpec);
  rpc TransformResponse(TransformResponseInput) returns (NormalizedInferenceResponse);
  rpc TransformStreamChunk(StreamChunkInput) returns (StreamChunkOutput);
  rpc NormalizeError(NormalizeErrorInput) returns (CanonicalProviderError);
  rpc ExtractUsage(ExtractUsageInput) returns (NormalizedUsage);
  rpc InspectNativeRequest(NativeRequestInput) returns (NativeRoutingHints);
  rpc EvaluateGuardrail(GuardrailInput) returns (GuardrailVerdict);
  rpc CancelExecution(CancelExecutionSignal) returns (CancelAck);
}
```

*(See [`proto/tanpopo/extension/v1/runtime.proto`](../../proto/tanpopo/extension/v1/runtime.proto) for the full self-contained message and enum declarations compiled by CI contract linters.)*
