# API & Wire Contract Design Specification: OpenAPI, JSON Schema & Protobuf — Tanpopo

> **Design-Level Interface Contract Document (ASTRA Phase A05 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — PRE-APPROVAL`)**
> - **Document ID:** `SPEC-API-01` | **Version:** `0.1.0-step05-draft` | **Last Updated:** `2026-11-11`
> - **Authoring Role:** Principal API & Platform Architect
> - **Governance Status:** **Design-Level Contract (`DESIGN-LEVEL CONTRACT — PRE-ADR`)** — defines type boundaries for (1) Data Plane API Clients, (2) Control Plane / Auth Administration, and (3) Extension IPC/ABI Boundaries. Not frozen into generated code stubs until the Product Owner approves Gate G3.
> - **Requirements Traceability:** `TP-FR-0101`..`0105`, `TP-FR-0205`, `TP-FR-0301`..`0306`, `TP-FR-0401`..`0406`, `TP-FR-0901`, `TP-FR-1001`, `SPEC-EXT-01`, `SPEC-EXT-06`.

---

## 1. Versioning, Timeouts, Cancellation & Idempotency Conventions

1. **Independent Contract Versioning Across Three Boundaries:**
   - **Data Plane Unified API (`/v1/*`):** Compatible with the standard OpenAI v1 client specification; Tanpopo metadata extensions are carried via `X-Tanpopo-*` headers so they never break static type checkers in existing client SDKs.
   - **Control Plane Admin API (`/admin/v1/*`):** Path-versioned under `/admin/v1` paired with an optional release-date header `X-Tanpopo-API-Version: 2026-11-01`; every change within the `v1` major line is strictly additive (`Additive-Only`), enforcing cursor-based pagination (`limit <= 200`, `cursor`) and optimistic concurrency control via `ETag` / `If-Match` (`409 version_conflict`).
   - **Extension Host-Guest Contract (`tanpopo.extension.v1`):** Negotiated via the `compatibility.contractVersion` field in the `Manifest`, guaranteeing `N-1` backward compatibility (during `0.x`) and `>= 12 months` deprecation windows (from `v1.0` onward — `TP-NFR-0902`).
2. **Streaming, Timeout & Cancellation Contract (`Streaming, Timeout & Cancellation`):**
   - An `API Client` may pass `X-Tanpopo-Timeout-Ms` to set a per-request deadline ceiling (clamped by the `Route`/`Deployment` upper bound, e.g., `<= 300000 ms`).
   - Over `text/event-stream` (SSE) streams, Tanpopo emits `: ping\n\n` heartbeats every `15 seconds` while awaiting deep-reasoning model generation to prevent idle timeouts across intermediate proxies and firewalls; if the client closes the TCP socket or resets the HTTP/2 stream, Core propagates context cancellation to both the Sandbox and the upstream provider connection within **`< 500 ms`** (`TP-FR-0104`).
   - The `Idempotency-Key` header (maximum `128` ASCII characters, `24-hour` retention TTL) applies to non-streaming `POST` requests and administrative resource/task creation commands (`SPEC-EXT-06` Section 3).

---

## 2. Design-Level OpenAPI 3.1 Contract (Core Data Plane & Control Plane)

```yaml
# DESIGN-LEVEL OPENAPI 3.1 SPECIFICATION (PROPOSED — STEP A05)
openapi: 3.1.0
info:
  title: Tanpopo AI Gateway — Data Plane & Control Plane API
  version: 0.1.0-draft
  license:
    name: Apache-2.0
    url: https://www.apache.org/licenses/LICENSE-2.0

security:
  - VirtualKeyBearer: []

components:
  securitySchemes:
    VirtualKeyBearer:
      type: http
      scheme: bearer
      description: "Data Plane Virtual Key (vk_tp_...). Strictly prohibited on /admin/v1/*."
    AdminSessionOrToken:
      type: http
      scheme: bearer
      description: "OIDC JWT / Admin Service Token (or HttpOnly Session Cookie paired with a CSRF token)."

  parameters:
    IfMatchVersion:
      name: If-Match
      in: header
      required: true
      schema: { type: string, examples: ['"v3"'] }
    IdempotencyKey:
      name: Idempotency-Key
      in: header
      required: false
      schema: { type: string, maxLength: 128 }
    DataRegionConstraint:
      name: X-Tanpopo-Data-Region
      in: header
      required: false
      schema: { type: string, enum: [EU, US, APAC, ON_PREM] }

  schemas:
    CanonicalErrorEnvelope:
      type: object
      required: [error]
      properties:
        error:
          type: object
          required: [code, type, message, request_id, retryable]
          properties:
            code: { type: string, examples: [capability_not_supported, project_budget_exceeded, mid_stream_abort] }
            type: { type: string, enum: [invalid_request_error, authentication_error, permission_error, conflict_error, rate_limit_error, upstream_error, residency_unavailable_error] }
            message: { type: string }
            request_id: { type: string }
            retryable: { type: boolean }

    ChatCompletionRequest:
      type: object
      required: [model, messages]
      properties:
        model: { type: string, description: "Route slug or Logical Model slug within the Virtual Key's Environment." }
        messages: { type: array, minItems: 1, items: { type: object } }
        stream: { type: boolean, default: false }
        max_completion_tokens: { type: integer, minimum: 1 }
        tools: { type: array, items: { type: object } }
        response_format:
          type: object
          properties:
            type: { type: string, enum: [text, json_object, json_schema] }
            json_schema:
              type: object
              properties:
                name: { type: string }
                strict: { type: boolean }
                schema: { type: object }

paths:
  # ==================== HEALTH & READINESS ====================
  /healthz:
    get:
      security: []
      summary: Liveness probe (process alive)
      responses:
        "200": { description: Process alive }
  /readyz:
    get:
      security: []
      summary: Readiness probe (DB, Vault KEK, ConfigSnapshot < 5s & Sandbox Pool ready)
      responses:
        "200": { description: Ready to serve traffic }
        "503": { description: Not ready or draining }

  # ==================== DATA PLANE: UNIFIED & NATIVE ====================
  /v1/chat/completions:
    post:
      summary: Unified Chat & Multimodal Inference (Sync JSON or SSE text/event-stream)
      parameters:
        - $ref: "#/components/parameters/IdempotencyKey"
        - $ref: "#/components/parameters/DataRegionConstraint"
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/ChatCompletionRequest" }
      responses:
        "200":
          description: Unified completion JSON or SSE stream
          headers:
            X-Tanpopo-Request-Id: { schema: { type: string } }
            X-Tanpopo-Usage-Accuracy: { schema: { type: string, enum: [EXACT, ESTIMATED, UNMETERED_FLAGGED] } }
        "400": { description: Capability not supported, content: { application/json: { schema: { $ref: "#/components/schemas/CanonicalErrorEnvelope" } } } }
        "429": { description: Rate limit or budget exceeded, content: { application/json: { schema: { $ref: "#/components/schemas/CanonicalErrorEnvelope" } } } }
        "503": { description: No compliant deployment in required Data Region, content: { application/json: { schema: { $ref: "#/components/schemas/CanonicalErrorEnvelope" } } } }

  /native/{dialect}/{upstreamPath}:
    post:
      summary: Lossless Native Dialect Passthrough (Anthropic, Gemini, Custom Adapter Prefix)
      parameters:
        - name: dialect
          in: path
          required: true
          schema: { type: string }
        - name: upstreamPath
          in: path
          required: true
          schema: { type: string }
      responses:
        "200": { description: Untouched upstream dialect JSON or SSE stream }

  # ==================== DATA PLANE: DEDICATED MCP & A2A PLANES ====================
  /mcp:
    post:
      summary: Dedicated MCP Governance Hub (Streamable HTTP — tools/list, tools/call)
      responses:
        "200": { description: Scoped MCP JSON-RPC response or SSE stream }
        "202": { description: Destructive tool paused awaiting human Reviewer approval (returns pre_approval_request_id) }
        "403": { description: Tool out of scope or schema_hash drift detected }

  /a2a/tasks:
    post:
      summary: Dedicated A2A Asynchronous Task Delegation (Bounded budget & hop_count <= 5)
      responses:
        "201": { description: Task accepted with attenuated delegation envelope }
        "400": { description: Delegation depth exceeded or privilege amplification blocked }

  # ==================== CONTROL PLANE: ADMIN, EXTENSIONS, AUTH & KILL-SWITCH ====================
  /admin/v1/extensions:install:
    post:
      security: [{ AdminSessionOrToken: [] }]
      summary: Stage and verify an external Extension Package (Marketplace, Git SHA, OCI, .tpkg)
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [source_uri, expected_sha256]
              properties:
                source_uri: { type: string }
                expected_sha256: { type: string, pattern: "^[a-f0-9]{64}$" }
      responses:
        "202": { description: Staged in PENDING_PERMISSION_APPROVAL with extracted permission diff }

  /admin/v1/extensions/{installationId}:approve-permissions:
    post:
      security: [{ AdminSessionOrToken: [] }]
      parameters:
        - name: installationId
          in: path
          required: true
          schema: { type: string }
        - $ref: "#/components/parameters/IfMatchVersion"
      summary: Explicitly consent to manifest permissions, write tanpopo.lock, and hot-activate
      responses:
        "200": { description: Transitioned to ENABLED_ACTIVE without Core restart }
        "409": { description: ETag / version conflict }

  /admin/v1/connections/{connectionId}/oauth:start:
    post:
      security: [{ AdminSessionOrToken: [] }]
      summary: Initiate Authorized OAuth 2.1 Authorization Code + PKCE S256 flow
      responses:
        "200": { description: Returns authorization_url and CSRF state bound to session }

  /admin/v1/oauth/callback:
    get:
      security: [{ AdminSessionOrToken: [] }]
      summary: Handle OAuth 2.1 callback, verify state + PKCE + allowed scopes, seal tokens in Write-Only Vault
      responses:
        "302": { description: Redirects back to Console Connection status page (never echoes tokens in URL/JSON) }

  /admin/v1/routes:explain:
    post:
      security: [{ AdminSessionOrToken: [] }]
      summary: Dry-run simulation returning full policy & capability routing decision tree
      responses:
        "200": { description: Decision tree JSON (0 upstream calls) }

  /admin/v1/kill-switches:trigger:
    post:
      security: [{ AdminSessionOrToken: [] }]
      summary: Emergency 4-level kill-switch (VIRTUAL_KEY | PROJECT | PROVIDER_CONNECTION | EXTENSION)
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [target_level, target_id, abort_active_streams, reason]
              properties:
                target_level: { type: string, enum: [VIRTUAL_KEY, PROJECT, PROVIDER_CONNECTION, EXTENSION] }
                target_id: { type: string }
                abort_active_streams: { type: boolean, default: true }
                reason: { type: string, minLength: 5 }
      responses:
        "200": { description: Propagated across cluster (< 1s standalone / < 5s HA) and logged to immutable audit }
```

---

## 3. Canonical JSON Schema for the Internal Envelope (`Normalized Inference Envelope`)

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://tanpopo.dev/schemas/v0.1/normalized-inference-request.schema.json",
  "title": "NormalizedInferenceRequestEnvelope",
  "type": "object",
  "additionalProperties": false,
  "required": ["contract_version", "request_id", "target_model", "modality", "messages", "stream"],
  "properties": {
    "contract_version": { "const": "0.1" },
    "request_id": { "type": "string", "pattern": "^req_[a-z0-9]{20,32}$" },
    "target_model": { "type": "string", "minLength": 1 },
    "modality": { "enum": ["chat", "responses", "embeddings"] },
    "stream": { "type": "boolean" },
    "messages": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["role", "content_blocks"],
        "properties": {
          "role": { "enum": ["system", "developer", "user", "assistant", "tool"] },
          "content_blocks": {
            "type": "array",
            "items": {
              "type": "object",
              "required": ["kind"],
              "properties": {
                "kind": { "enum": ["text", "image", "audio", "document", "tool_call", "tool_result", "reasoning_opaque"] },
                "text": { "type": "string" },
                "media_ref": { "type": "string", "description": "Sanitized HTTPS URL or size-bounded inline data URI." },
                "opaque_signature": { "type": "string", "description": "Preserves reasoning block cryptographic signatures across multi-turn conversations." }
              }
            }
          }
        }
      }
    },
    "secret_slot_refs": {
      "type": "object",
      "description": "Contains ONLY secret slot reference handles (e.g., primary_api_key -> slot_handle_01); MUST NEVER contain raw secret plaintext!",
      "additionalProperties": { "type": "string" }
    }
  }
}
```

---

## 4. Protobuf (`proto3` / ConnectRPC) Contract Between Host Core and Execution Extensions (`Tier-2` / `Tier-3`)

The binary wire contract below defines the IPC protocol between **Tanpopo Host Core** and an **Isolated Sidecar (`Tier-3` over Unix Domain Socket / `mTLS`)**, while simultaneously serving as the 1-to-1 structural data model equivalent for the **Wasm Sandbox (`Tier-2`)** Host-Guest ABI (`POC-01`, `POC-03`):

```protobuf
// DESIGN-LEVEL PROTOBUF CONTRACT (tanpopo/extension/v1/runtime.proto)
syntax = "proto3";

package tanpopo.extension.v1;

option go_package = "github.com/nguywnben/tanpopo/gen/extension/v1;extensionv1";

service ExtensionRuntimeService {
  // Validates connection configuration (Host executes bounded network probes within allowed_hosts on behalf of Guest).
  rpc ValidateConnection(ValidateConnectionRequest) returns (ValidateConnectionResponse);

  // Transforms a normalized request into an upstream HTTP specification (Guest NEVER receives raw API keys).
  rpc TransformRequest(TransformRequestEnvelope) returns (UpstreamHttpSpec);

  // Incrementally processes SSE stream chunks without full-response buffering.
  rpc TransformStreamChunk(StreamChunkInput) returns (StreamChunkOutput);

  // Extracts multi-component token usage counts and classifies metering accuracy.
  rpc ExtractUsage(ExtractUsageInput) returns (UsageReport);

  // Maps upstream HTTP failures onto the canonical SM-02 / SM-04 state-machine error taxonomy.
  rpc ClassifyError(ClassifyErrorInput) returns (CanonicalProviderError);

  // Evaluates policy filters / PII guardrails at pre-inference or post-inference hook stages.
  rpc EvaluateGuardrail(GuardrailInput) returns (GuardrailVerdict);

  // Signals session resource teardown on client disconnect (Host forcibly closes upstream sockets in < 500ms).
  rpc CancelExecution(CancelExecutionSignal) returns (CancelAck);
}

enum UsageAccuracy {
  USAGE_ACCURACY_UNSPECIFIED = 0;
  USAGE_ACCURACY_EXACT = 1;
  USAGE_ACCURACY_ESTIMATED = 2;
  USAGE_ACCURACY_UNMETERED_FLAGGED = 3;
}

message CredentialInjectInstruction {
  string secret_slot_name = 1; // e.g., "primary_api_key"
  string target_location = 2;  // "HEADER" | "QUERY_PARAM" | "HOST_HMAC_SIGN"
  string field_name = 3;       // e.g., "Authorization" or "X-Acme-Signature"
  string value_prefix = 4;     // e.g., "Bearer "
}

message UpstreamHttpSpec {
  string method = 1;
  string target_url = 2; // Host MUST re-verify against allowed_hosts and SSRF guards before opening a socket!
  map<string, string> safe_headers = 3;
  bytes body_bytes = 4;
  repeated CredentialInjectInstruction credential_injections = 5;
}

message StreamChunkInput {
  string request_id = 1;
  bytes session_state = 2; // Hard ceiling <= 65536 bytes
  bytes raw_upstream_chunk = 3;
  bool is_eof = 4;
}

message StreamChunkOutput {
  bytes next_session_state = 1;
  repeated bytes normalized_sse_frames = 2;
  UsageReport incremental_usage = 3;
  bool stream_completed = 4;
}

message UsageReport {
  int32 input_tokens = 1;
  int32 output_tokens = 2;
  int32 cache_read_tokens = 3;
  int32 cache_write_tokens = 4;
  int32 reasoning_tokens = 5;
  UsageAccuracy accuracy = 6;
}

message CanonicalProviderError {
  string canonical_code = 1;
  bool retryable_pre_stream = 2;
  int32 cooldown_ms = 3;
  bool trigger_connection_reauth = 4;
  string sanitized_public_message = 5;
}

message GuardrailInput {
  string request_id = 1;
  string stage = 2; // "PRE_REQUEST" | "POST_RESPONSE"
  bytes payload_json = 3;
}

message GuardrailVerdict {
  bool allowed = 1;
  string violation_code = 2;
  bytes redacted_payload_json = 3;
}

message ValidateConnectionRequest {
  string config_json = 1;
}

message ValidateConnectionResponse {
  bool valid = 1;
  repeated string detected_model_ids = 2;
  string failure_code = 3;
}

message TransformRequestEnvelope {
  string request_id = 1;
  string config_json = 2;
  bytes normalized_request_json = 3;
}

message ClassifyErrorInput {
  int32 http_status = 1;
  map<string, string> headers = 2;
  bytes sanitized_body = 3;
}

message CancelExecutionSignal {
  string request_id = 1;
  string reason = 2;
}

message CancelAck {
  bool released = 1;
}
```
