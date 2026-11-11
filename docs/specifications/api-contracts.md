# Đặc tả Thiết kế Hợp đồng Giao diện Lập trình: OpenAPI, JSON Schema & Protobuf (API & Wire Contracts) — Tanpopo

> **Tài liệu Hợp đồng Giao diện Mức Thiết kế (Phase ASTRA A05 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — PRE-APPROVAL`)**
> - **Mã tài liệu:** `SPEC-API-01` | **Phiên bản:** `0.1.0-step05-draft` | **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Principal API & Platform Architect
> - **Trạng thái quản trị:** **Hợp đồng Mức Thiết kế (`DESIGN-LEVEL CONTRACT — PRE-ADR`)** — định nghĩa ranh giới kiểu dữ liệu cho (1) Ứng dụng khách Data Plane, (2) Quản trị Control Plane / Auth, và (3) Biên giới IPC/ABI của Phần mở rộng. Chưa đóng băng thành mã sinh tự động cho tới khi Owner duyệt Gate G3.
> - **Truy vết Yêu cầu:** `TP-FR-0101`..`0105`, `TP-FR-0205`, `TP-FR-0301`..`0306`, `TP-FR-0401`..`0406`, `TP-FR-0901`, `TP-FR-1001`, `SPEC-EXT-01`, `SPEC-EXT-06`.

---

## 1. Quy ước Phiên bản Hóa, Thời gian chờ, Hủy Kết nối & Chống Gọi Trùng

1. **Phiên bản Hợp đồng Độc lập trên 3 Biên giới:**
   - **Data Plane Unified API (`/v1/*`):** Tương thích chuẩn khách OpenAI v1; các mở rộng siêu dữ liệu của Tanpopo đi qua tiêu đề `X-Tanpopo-*` để không bao giờ phá vỡ trình kiểm tra kiểu của SDK có sẵn.
   - **Control Plane Admin API (`/admin/v1/*`):** Phiên bản đường dẫn `/admin/v1` kết hợp tiêu đề ngày phát hành tùy chọn `X-Tanpopo-API-Version: 2026-11-01`; mọi thay đổi trong nhánh `v1` chỉ cộng thêm trường tùy chọn (`Additive-Only`), phân trang con trỏ bắt buộc (`limit <= 200`, `cursor`), và kiểm soát ghi đè bằng `ETag` / `If-Match` (`409 version_conflict`).
   - **Extension Host-Guest Contract (`tanpopo.extension.v1`):** Thương lượng qua trường `compatibility.contractVersion` trong `Manifest`, bảo hành tương thích lùi `N-1` (giai đoạn `0.x`) và `>= 12 tháng` (từ `v1.0` — `TP-NFR-0902`).
2. **Hợp đồng Thời gian Chờ, Truyền Luồng & Hủy Kết nối (`Streaming, Timeout & Cancellation`):**
   - `API Client` có thể gửi `X-Tanpopo-Timeout-Ms` để đặt trần thời gian tối đa cho yêu cầu (bị kẹp trần bởi giới hạn của `Route`/`Deployment`, ví dụ `<= 300000 ms`).
   - Trên luồng `text/event-stream` (SSE), Tanpopo gửi nhịp `: ping\n\n` mỗi `15 giây` khi chờ mô hình suy luận sâu để giữ kết nối qua tường lửa trung gian; nếu khách đóng kết nối TCP/HTTP2 stream reset, Core kích hoạt hủy ngữ cảnh tới cả Sandbox lẫn kết nối nhà cung cấp đích trong **`< 500 ms`** (`TP-FR-0104`).
   - Khóa chống gọi trùng `Idempotency-Key` (tối đa `128` ký tự ASCII, hiệu lực lưu giữ `24 giờ`) áp dụng cho các lệnh `POST` không truyền luồng và các lệnh tạo tài nguyên/tác vụ quản trị (`SPEC-EXT-06` Mục 3).

---

## 2. Hợp đồng OpenAPI 3.1 Mức Thiết kế (Data Plane & Control Plane Cốt lõi)

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
      description: "Data Plane Virtual Key (vk_tp_...). Cấm tuyệt đối dùng trên /admin/v1/*."
    AdminSessionOrToken:
      type: http
      scheme: bearer
      description: "OIDC JWT / Admin Service Token (hoặc HttpOnly Session Cookie kèm CSRF token)."

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
        model: { type: string, description: "Route slug hoặc Logical Model slug trong Environment của khóa ảo." }
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
      summary: Liveness probe (tiến trình sống)
      responses:
        "200": { description: Process alive }
  /readyz:
    get:
      security: []
      summary: Readiness probe (DB, Vault KEK, ConfigSnapshot < 5s & Sandbox Pool sẵn sàng)
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
      summary: Emergency 4-level kill-switch (VIRTUAL_KEY | PROJECT | CONNECTION | EXTENSION_QUARANTINE)
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

## 3. Lược đồ JSON Schema Chuẩn Hóa Cho Phong Bì Nội bộ (`Normalized Inference Envelope`)

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
                "media_ref": { "type": "string", "description": "HTTPS URL đã lọc hoặc chuỗi dữ liệu nội tuyến kiểm soát kích thước." },
                "opaque_signature": { "type": "string", "description": "Bảo toàn chữ ký khối suy luận qua nhiều lượt hội thoại." }
              }
            }
          }
        }
      }
    },
    "secret_slot_refs": {
      "type": "object",
      "description": "CHỈ chứa tên khe tham chiếu (VD: primary_api_key -> slot_handle_01), TUYỆT ĐỐI KHÔNG chứa chuỗi bí mật thô!",
      "additionalProperties": { "type": "string" }
    }
  }
}
```

---

## 4. Hợp đồng Protobuf (`proto3` / ConnectRPC) Giữa Host Core và Extension Thực thi (`Tier-2` / `Tier-3`)

Hợp đồng nhị phân dưới đây định nghĩa giao thức truyền thông giữa **Tanpopo Host Core** và **Isolated Sidecar (`Tier-3` qua Unix Domain Socket / `mTLS`)**, đồng thời đóng vai trò mô hình dữ liệu tương đương 1-1 cho giao diện Host-Guest của **Wasm Sandbox (`Tier-2`)** (`POC-01`, `POC-03`):

```protobuf
// DESIGN-LEVEL PROTOBUF CONTRACT (tanpopo/extension/v1/runtime.proto)
syntax = "proto3";

package tanpopo.extension.v1;

option go_package = "github.com/nguywnben/tanpopo/gen/extension/v1;extensionv1";

service ExtensionRuntimeService {
  // Kiểm tra cấu hình kết nối (Host thực hiện probe mạng hộ theo yêu cầu trong phạm vi allowed_hosts).
  rpc ValidateConnection(ValidateConnectionRequest) returns (ValidateConnectionResponse);

  // Biến đổi yêu cầu chuẩn hóa thành đặc tả HTTP đích (Guest KHÔNG nhận API key gốc).
  rpc TransformRequest(TransformRequestEnvelope) returns (UpstreamHttpSpec);

  // Xử lý cuốn chiếu từng mảnh luồng SSE không gom đệm toàn bộ phản hồi.
  rpc TransformStreamChunk(StreamChunkInput) returns (StreamChunkOutput);

  // Trích xuất số đếm token đa thành phần và phân loại độ chính xác.
  rpc ExtractUsage(ExtractUsageInput) returns (UsageReport);

  // Chuẩn hóa lỗi HTTP từ nhà cung cấp sang bảng mã lỗi máy trạng thái SM-02 / SM-04.
  rpc ClassifyError(ClassifyErrorInput) returns (CanonicalProviderError);

  // Đánh giá bộ lọc chính sách / PII Guardrail tại điểm móc trước hoặc sau suy luận.
  rpc EvaluateGuardrail(GuardrailInput) returns (GuardrailVerdict);

  // Tín hiệu giải phóng tài nguyên phiên khi khách hủy kết nối (Host tự đóng socket trong < 500ms).
  rpc CancelExecution(CancelExecutionSignal) returns (CancelAck);
}

enum UsageAccuracy {
  USAGE_ACCURACY_UNSPECIFIED = 0;
  USAGE_ACCURACY_EXACT = 1;
  USAGE_ACCURACY_ESTIMATED = 2;
  USAGE_ACCURACY_UNMETERED_FLAGGED = 3;
}

message CredentialInjectInstruction {
  string secret_slot_name = 1; // VD: "primary_api_key"
  string target_location = 2;  // "HEADER" | "QUERY_PARAM" | "HOST_HMAC_SIGN"
  string field_name = 3;       // VD: "Authorization" hoặc "X-Acme-Signature"
  string value_prefix = 4;     // VD: "Bearer "
}

message UpstreamHttpSpec {
  string method = 1;
  string target_url = 2; // Host BẮT BUỘC kiểm tra lại khớp allowed_hosts & chống SSRF trước khi mở socket!
  map<string, string> safe_headers = 3;
  bytes body_bytes = 4;
  repeated CredentialInjectInstruction credential_injections = 5;
}

message StreamChunkInput {
  string request_id = 1;
  bytes session_state = 2; // Giới hạn cứng <= 65536 bytes
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