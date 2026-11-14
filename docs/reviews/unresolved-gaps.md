# Sổ Đăng ký Các Khoảng Trống Chưa Đóng, Đề xuất Change Requests (`CR`) & Tiêu chí Nghiệm thu Đóng Lỗ hổng — Tanpopo

> **Tài liệu Quản trị Khắc phục Sau Kiểm toán (Phase ASTRA A08 — Companion to [`integrated-audit.md`](./integrated-audit.md))**
> - **Mã tài liệu:** `REV-GAP-01` | **Phiên bản:** `0.1.0-step08` | **Ngày lập:** `2026-11-14`
> - **Vai trò biên soạn:** Independent Product / Architecture / Security Reviewer
> - **Nguyên tắc quản trị bất biến:** Kiểm toán viên **tuyệt đối không tự ý sửa đổi phạm vi sản phẩm, ngăn xếp công nghệ, lược đồ hợp đồng hay chính sách bảo mật**. Mọi thay đổi kiến trúc/phạm vi/chính sách đều được đóng gói thành **Đề xuất Thay đổi (`CR-01` .. `CR-10`)** trình **Product Owner** quyết định, kèm **Tiêu chí Đóng Đo lường Được (`Closure Criteria`)** cho từng khoảng trống.

---

## Phần A — Mười Đề xuất Quyết định Thay đổi & Bổ sung ADR (`CR-01` .. `CR-10` — Trình Product Owner Phê duyệt)

### `CR-01` — Khóa Danh mục `Official Reference Adapters` Cho `MVP (v0.1)` & Quyết định Điểm cuối `/v1/rerank`
- **Liên kết lỗ hổng:** `AUD-007`, `AUD-023` | **Tác động:** Phạm vi `Gate G1 / G2` (`srs.md`, `release-scope.md`, `decisions.md`).
- **Vấn đề:** Tài liệu hiện mâu thuẫn giữa `2–3`, `5`, `5–6` và `6–8` Official Adapters ở `v0.1`; đặc biệt `Bedrock` được nhắc ở `v0.1` nhưng lại phụ thuộc xác thực AWS IAM/SigV4 (`TP-FR-0206`, đang xếp ở `v0.3`). Ngoài ra `rerank` có trong `Charter` nhưng vắng mặt trong `SRS`.
- **Các phương án trình Product Owner chọn:**
  - **Phương án A *(Khuyến nghị)*:** Khóa cứng đúng **5 Official Reference Adapters trong `MVP (v0.1)`** dùng xác thực khóa tĩnh/tiêu đề chuẩn hoặc nội bộ gồm: **(1) `OpenAI`, (2) `Anthropic`, (3) `Google Gemini`, (4) `OpenAI-Compatible (vLLM / Groq / Together / DeepSeek)`, (5) `Ollama`** (trong đó `OpenAI`, `OpenAI-Compatible`, `Ollama` dùng Tầng 1 Khai báo; `Anthropic` và `Gemini` dùng Tầng 2 Wasm để kiểm chứng thực chiến cả 2 tầng!). Đưa **`AWS Bedrock` và `Azure OpenAI` (cần SigV4 / Entra ID)** sang **`v0.2`**, và bổ sung `/v1/rerank` làm yêu cầu `Should` ở `v0.2`.
  - **Phương án B:** Đẩy sớm nguyên hàm ký `HOST_AWS_SIGV4` từ `v0.3` về `v0.1` để giao kèm cả `AWS Bedrock` ngay trong `MVP (v0.1)` (6 Adapters), chấp nhận tăng khối lượng công việc `v0.1` thêm ~1 tuần.
- **Tiêu chí đóng (`Closure Criteria`):** Đồng bộ cùng một con số và tên hãng cụ thể trên `decisions.md`, `release-scope.md`, `srs.md` (`TP-FR-0301`/`0302`) và thêm kịch bản nghiệm thu danh mục Official Adapters vào `acceptance-criteria.md`.

---

### `CR-02` — Giải quyết Phân liệt Mô hình Thực thi `2-Tier` (G1/G2/SRS) vs `3-Tier Isolated Sidecar` (`ADR-0002`)
- **Liên kết lỗ hổng:** `AUD-008` | **Tác động:** `SD-G1-02`, `TD-G2-04`, `srs.md`, `release-scope.md`, `ADR-0002`.
- **Vấn đề:** Nếu Product Owner duyệt `SD-G1-02` (2 tầng) và `ADR-0002` (3 tầng) như hiện tại thì hai quyết định tự mâu thuẫn nhau, và `Tier-3 Sidecar` không có yêu cầu `TP-FR-*` hay mốc phiên bản nào.
- **Các phương án trình Product Owner chọn:**
  - **Phương án A *(Khuyến nghị)*:** Xác nhận kiến trúc đích là **3 tầng (`ADR-0002`)**, nhưng **phân kỳ triển khai rõ trong `SRS` & `release-scope.md`**: `v0.1` chỉ bắt buộc xây dựng **`Tier-1 Declarative` (`TP-FR-0301`) + `Tier-2 Wasm Sandbox` (`TP-FR-0302`)**; bổ sung mới mã yêu cầu **`TP-FR-0308 (Could — v0.3): Động cơ Thực thi Tầng 3 Isolated Sidecar qua ConnectRPC UDS/mTLS`** (dùng chung cho cầu nối `stdio` MCP và HSM doanh nghiệp).
  - **Phương án B:** Loại bỏ hoàn toàn `Tier-3 Sidecar` khỏi phạm vi trước `v1.0` để giữ kiến trúc thuần 2 tầng gọn nhẹ tuyệt đối (khi đó các máy chủ MCP `stdio` phải được người vận hành bọc thành `Streamable HTTP` bên ngoài Tanpopo).
- **Tiêu chí đóng (`Closure Criteria`):** Cập nhật thống nhất `SD-G1-02`, `TD-G2-04`, `ADR-0002`, `srs.md` và `release-scope.md`; bổ sung `SPIKE` kiểm chứng IPC nếu giữ `Tier-3`.

---

### `CR-03` — Thống nhất Mô hình Nhãn Nguồn gốc (`3` vs `4` `Trust Tiers`), Kiểu Chữ Enum & Không gian Tên Khóa Độc quyền
- **Liên kết lỗ hổng:** `AUD-012`, `AUD-013` | **Tác động:** `TP-BR-0201`, `data-model.md`, `routing-and-policy.md`, `SPEC-UX-01/04`, `SPEC-SEC-02`.
- **Vấn đề:** Xung đột giữa bất biến 3 nhãn của `TP-BR-0201` (`official`, `verified`, `unverified-external` + cờ nội bộ) với cột `origin_trust_tier` 4 giá trị (`PRIVATE_ENTERPRISE`), đồng thời lệch bảo vệ `@tanpopo/*` vs `@tanpopo-official/*`.
- **Các phương án trình Product Owner chọn:**
  - **Phương án A *(Khuyến nghị — Giữ đúng tinh thần `TP-BR-0201`)*:** Giữ cố định **3 giá trị `origin_trust_tier`** công cộng (`official`, `verified`, `unverified_external`), và thêm cột độc lập **`enterprise_signed BOOLEAN NOT NULL DEFAULT FALSE` + `signing_key_fingerprint VARCHAR(64)`** trên `extension_versions` (cho phép chính sách `min_plugin_trust` đặt `OFFICIAL_ONLY | VERIFIED_OR_ENTERPRISE_SIGNED | VERIFIED_ONLY | ANY_APPROVED`).
  - **Phương án B:** Sửa chính thức `TP-BR-0201`, `glossary.md` và `TP-FR-0304` từ 3 nhãn lên **4 nhãn phân cấp tường minh** (`official > verified > private_enterprise > unverified_external`) xuyên suốt mọi tài liệu và cột DB.
  - *Đi kèm bắt buộc cho cả A & B:* Khóa cứng biểu thức chính quy không gian tên dành riêng **`^@tanpopo(-[a-z0-9]+)?/`** (chặn mạo danh cả `@tanpopo/*` lẫn `@tanpopo-official/*`).
- **Tiêu chí đóng (`Closure Criteria`):** Một tập giá trị enum duy nhất khớp `100%` giữa `TP-BR-0201`, DDL `CHECK (...)`, JSON Schema `tanpopo.lock` và thẻ `Token` trên giao diện.

---

### `CR-04` — Chốt Bộ Vai trò `RBAC`, Bảng Bật/Tắt Plugin Theo `Organization` & Hiệu chỉnh Cổng Xuất xưởng `v0.1`
- **Liên kết lỗ hổng:** `AUD-007`, `AUD-014` | **Tác động:** `data-model.md`, `release-scope.md`, `release-readiness.md`, `contract-test-matrix.md`.
- **Đề xuất trình duyệt:**
  1. **Vai trò `RBAC`:** Chốt `role_bindings.role_name` gồm **6 vai trò chuẩn** (`OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`; chế độ `Personal Admin` tự động gán `OPERATOR + ORG_OWNER` cho người dùng đầu tiên).
  2. **Bật/Tắt Plugin Cấp Tổ chức (`TP-BR-0202`):** Thêm bảng liên kết `org_extension_policies (org_id, package_id, enabled, pinned_installation_id, version)` và đổi ràng buộc trên `provider_adapters` thành `UNIQUE (installation_id, adapter_slug)` để nhiều phiên bản cùng tồn tại êm khi cuộn nâng cấp.
  3. **Gỡ kẹt Cổng Xuất xưởng `MVP (v0.1)`:**
     - Cho phép tạo `Organization` thứ hai qua CLI quản trị/API (`POST /admin/v1/organizations` dành cho `OPERATOR`) ngay ở `v0.1` để kiểm thử tự động `TP-AC-SEC-07` chạy thật 100%, trong khi giao diện chuyển đổi đa tổ chức nâng cao trên Web Console vẫn giữ lộ trình `v0.3`/`v1.0`.
     - Tách điều kiện diễn tập gói `Air-Gap` trong `GATE-REL-06` và thu hồi cụm `< 5s` trong `TP-AC-N04` áp dụng từ cột mốc tương ứng (`v0.3`/`v1.0`), còn `v0.1` kiểm định thu hồi đơn nút `< 1s` và chạy Web Console ngoại tuyến không gọi CDN ngoài (`UX-AC-10`).
- **Tiêu chí đóng (`Closure Criteria`):** `MVP (v0.1)` có tập tiêu chí xuất xưởng tự nhất quán 100% không phụ thuộc tính năng của `v0.3`/`v1.0`.

---

### `CR-05` — Tái Thiết kế Hợp đồng `ExtensionRuntimeService` (`runtime.proto`), Tham chiếu Luồng Đa phương thức & Cổng Khóa Mù
- **Liên kết lỗ hổng:** `AUD-001`, `AUD-002`, `AUD-016`, `AUD-021` | **Tác động:** `SPEC-API-01`, `SPEC-EXT-01/02/03`, `SPEC-CORE-02`, `SPEC-SEC-01/02`.
- **Đề xuất kỹ thuật trình duyệt (bổ sung vào `ADR-0002` & `SPEC-API-01`):**
  1. **Hoàn thiện `runtime.proto` biên dịch sạch (`0 errors`):** Định nghĩa đầy đủ `ExtractUsageInput`, bổ sung **`rpc TransformResponse(TransformResponseInput) returns (NormalizedInferenceResponse)`** cho luồng đồng bộ/embeddings, bổ sung **`rpc InspectNativeRequest(NativeRequestInput) returns (NativeRoutingHints)`** cho `/native/{dialect}/*`, và tách `ValidateConnection` thành giao thức 2 pha (`BuildValidationProbe -> UpstreamHttpSpec` do Host thực thi qua `Blind Broker` → `VerifyValidationResult`) để Guest không cần mở socket mạng.
  2. **Tách Tệp Đa phương thức Khỏi Bộ nhớ Tuyến tính `16 MiB` Wasm (`Out-of-Band Blob Handles`):** Khi yêu cầu chứa ảnh/âm thanh/PDF base64 lớn, Host thay thế chuỗi base64 bằng mã tham chiếu `blob_ref://req_.../part_0` trước khi đưa JSON siêu dữ liệu (`< 256 KB`) vào Wasm `TransformRequest`, sau đó Host ghép luồng trực tiếp khi gửi đi; đồng thời công thức ước tính token tách riêng văn bản (`utf8_bytes / 3`) và định mức token cố định/theo trang cho từng khối `image`/`audio`/`document`!
  3. **Hoàn thiện `HostCryptoSignSpec` & Đóng Cửa `scoped_secret_unseal` Trên Wasm Cộng đồng:** Bổ sung cấu trúc ký động (`HMAC-SHA256`, `AWS_SIGV4`, `RSA-SHA256` kèm `canonical_payload` và `output_encoding`) vào `CredentialInjectInstruction`; cấm cứng việc tiêm tiêu đề `Cookie` / `Set-Cookie` trong `Blind Broker` (`DEC-004`); và **loại bỏ hoàn toàn `scoped_secret_unseal` khỏi Tầng 1 & Tầng 2** (nếu doanh nghiệp bắt buộc dùng thư viện ký đóng riêng thì chỉ chạy qua `Tier-3 Isolated Sidecar` nội bộ do `OPERATOR` quản lý) để giữ nguyên vẹn 100% bất biến *Zero Raw Secrets in Wasm Memory*.
- **Tiêu chí đóng (`Closure Criteria`):** Tệp `.proto` trích xuất từ `SPEC-API-01` vượt qua `buf lint` / `protoc`, và `NormalizedInferenceRequestEnvelope` chứa đầy đủ mọi trường của `TP-FR-0101`..`0105`.

---

### `CR-06` — Cứng Hóa Lược đồ Dữ liệu Vật lý (`ARCH-DATA-01`): 8 Bảng Còn Thiếu, Mật mã Phong bì `Project DEK` & Chuỗi Kiểm toán Phân vùng
- **Liên kết lỗ hổng:** `AUD-003`, `AUD-004`, `AUD-010`, `AUD-026`, `AUD-029` | **Tác động:** `data-model.md`, `secrets-and-tenant-isolation.md`, `backups-recovery.md`, `ADR-0003`, `ADR-0005`.
- **Đề xuất kỹ thuật trình duyệt:**
  1. **Bổ sung DDL cho các bảng còn thiếu:** `config_changelog (epoch BIGSERIAL PRIMARY KEY, ...)`, `distributed_leases`, `usage_rollups_hourly`, `oauth_authorization_sessions` (lưu PKCE/state/Device Flow có `expires_at`), `connection_secret_bindings (connection_id, slot_name, credential_id)` (hỗ trợ đa khe bí mật), `extension_revocations`, và (ở nhịp tương ứng) `a2a_tasks` + `webhook_endpoints`.
  2. **Sửa các lỗi ràng buộc SQL & Đa thuê bao:** Cho phép `extension_installations.active_version_id` nhận `NULL` khi đang `PENDING_PERMISSION_APPROVAL`; cho phép `approval_requests.org_id` / `project_id` nhận `NULL` đối với hành động cấp `Instance`; thêm chỉ mục duy nhất một phần `WHERE org_id IS NULL` cho `principals`; áp dụng khóa tổ hợp `(org_id, id)` trên toàn bộ bảng con thuê bao kèm câu lệnh mẫu `ALTER TABLE ... ENABLE / FORCE ROW LEVEL SECURITY`; khóa kiểu thời gian trên SQLite thống nhất là **`BIGINT epoch_ms`**.
  3. **Đồng nhất Mật mã & Giải quyết Triệt để `Crypto-Shredding` Cấp `Project`:**
     - Chốt **1 công thức `AEAD AAD` duy nhất** khớp cột thật: `"tanpopo:v1:cred:" || org_id || ":" || credential_id || ":" || secret_kind`.
     - Chốt băm `Virtual Key` bằng **`SHA-256` thuần trên khóa ngẫu nhiên CSPRNG 256-bit** (không phụ thuộc `Master KEK` nên xoay `KEK` không bao giờ làm chết khóa ảo).
     - Bổ sung bảng **`project_encryption_keys (project_id, org_id, wrapped_project_dek)`** (bọc bởi `Org DEK`) để mã hóa các bí mật cấp `Project` và `debug_payload_blobs` (thêm cột `project_id`) — biến cam kết `Crypto-Shredding < 60s` ở cấp `Project` (`OPS-AC-09`) thành **đúng đắn 100% về mặt toán học kể cả trên bản sao lưu lạnh**!
  4. **Chống Ghi Đúp Sổ cái & Chống Phân Nhánh Chuỗi Băm Kiểm toán Ở Cụm HA:**
     - Thêm ràng buộc duy nhất `UNIQUE (request_id, settled_at)` kết hợp chuyển trạng thái nguyên tử `UPDATE budget_reservations SET state = 'SETTLED_...' WHERE id = ? AND state IN ('RESERVED_HOLD', 'STREAMING_ACCRUING')` trong cùng giao dịch, đồng thời thêm cột `media_usage_json` và `applied_pricing_snapshot_json` vào `usage_settlements`.
     - Đổi chuỗi băm `audit_events` sang **chuỗi tuần tự theo từng `org_id` (và 1 chuỗi `SYSTEM` riêng cho `org_id IS NULL`) có cột `seq_no BIGINT` và băm trên chuỗi `RFC 8785 JCS(metadata)`**: vừa cho phép `ORG_OWNER` tự kiểm tra toàn vẹn chuỗi của tổ chức mình dưới `FORCE RLS` mà không lộ dữ liệu `Org B`, vừa không bị gãy khi di trú `SQLite <-> Postgres`!
- **Tiêu chí đóng (`Closure Criteria`):** Lược đồ SQL chạy sạch trên cả `SQLite` và `PostgreSQL 16` không còn bất kỳ bảng tham chiếu mồ côi hay mâu thuẫn mật mã nào.

---

### `CR-07` — Khóa Lược đồ Khóa `Valkey Cluster` Có Hash-Tag `{org_id}` & Đồng bộ Nguồn Chân lý Ngân sách Khi Mất Cache
- **Liên kết lỗ hổng:** `AUD-004` | **Tác động:** `deployment-topology.md`, `usage-billing-observability.md`, `ADR-0003`, `ADR-0005`.
- **Đề xuất kỹ thuật trình duyệt:**
  1. Bắt buộc mọi khóa trên `Valkey/Redis` sử dụng cú pháp **`Hash-Tag` chuẩn Redis Cluster `tp:v1:{<org_id>}:...`** (ví dụ `tp:v1:{org_01jc...}:budget:prj_01jc...`, `tp:v1:{org_01jc...}:rl:vk_01jc...`). Nhờ cặp ngoặc nhọn `{<org_id>}`, toàn bộ phân cấp `Org -> Project -> Env -> Virtual Key` của cùng một tổ chức luôn nằm trên đúng **1 hash slot duy nhất**, cho phép kịch bản Lua nguyên tử chạy `100%` không bao giờ gặp lỗi `CROSSSLOT`!
  2. Để ngăn chi vượt trần khi `Valkey` sập giữa chừng: mỗi nút Data Plane định kỳ đồng bộ tổng mức giữ chỗ đang mở (`active_node_hold_micros`) kèm nhịp `heartbeat` `5 giây` xuống bảng nhẹ trên PostgreSQL; khi chuyển sang chế độ dự phòng đọc trực tiếp PostgreSQL, hệ thống cộng thêm biên độ bảo hiểm của các luồng đang mở trên cụm trước khi cấp phép yêu cầu mới.
- **Tiêu chí đóng (`Closure Criteria`):** Cập nhật đầy đủ bảng khóa Valkey vào `ARCH-OPS-01` và kiểm chứng trong `SPIKE-03`.

---

### `CR-08` — Thiết kế Lại Giao thức Duyệt Công cụ MCP `destructive` Tương thích Chuẩn IDE & Tách Máy Trạng thái `SM-09` Cho `A2A Task`
- **Liên kết lỗ hổng:** `AUD-006` | **Tác động:** `mcp-a2a-boundaries.md`, `business-entity-map.md`, `api-contracts.md`, `user-flows.md`.
- **Đề xuất kỹ thuật trình duyệt:**
  1. **Tương thích 100% với `Claude Code`, `Cursor`, `Codex` trên `POST /mcp`:** Thay vì trả HTTP `202` + đòi tiêu đề HTTP lạ `X-Tanpopo-Approval-Token` (làm gãy trình khách MCP chuẩn), ở chế độ bất đồng bộ Tanpopo trả về phản hồi **HTTP `200` JSON-RPC chuẩn của MCP** với `isError: true` chứa cấu trúc hướng dẫn rõ mã `approval_request_id` và cung cấp công cụ nội bộ hệ thống `tanpopo__check_approval_and_resume(approval_request_id)` (hoặc cho phép truyền qua trường chuẩn `_meta.tanpopoApprovalToken` sau khi gọi API lấy phiếu đã duyệt) — đồng thời sửa công thức băm tham số gắn vào `(org_id, virtual_key_id, server_slug, tool_name, JCS(arguments))` (không dùng `principal_id` rỗng).
  2. **Tách riêng `SM-09 (A2A Task Lifecycle)`:** Giữ `SM-06` chuyên biệt cho `ApprovalRequest`, và tạo `SM-09` ánh xạ 1-1 với trạng thái chuẩn của giao thức `Agent2Agent` (`submitted -> working -> input-required -> completed | failed | canceled`) kèm chốt một chuẩn giao thức đường truyền duy nhất (JSON-RPC 2.0 qua HTTP/SSE theo chuẩn A2A gốc).
- **Tiêu chí đóng (`Closure Criteria`):** Xác nhận bằng `EXP-05` với trình khách MCP tiêu chuẩn không cần viết bản vá riêng phía client.

---

### `CR-09` — Bổ sung Các Năng lực Vận hành Tự Host Còn Thiếu & Quyết định Số phận Của `Webhooks` / `Response Cache`
- **Liên kết lỗ hổng:** `AUD-023`, `AUD-024` | **Tác động:** `srs.md`, `release-scope.md`, `provider-adapter.md`, `installer-upgrades.md`, `data-model.md`.
- **Đề xuất trình Product Owner chọn:**
  1. **Bổ sung ngay vào `Must (v0.1)` (Nhẹ, thiết yếu để sống còn khi tự host):**
     - Lệnh cứu hộ cục bộ yêu cầu quyền truy cập máy chủ vật lý/container: **`tanpopo admin reset-local-owner`** (khôi phục khi quên mật khẩu hoặc lỗi cấu hình OIDC).
     - Quy tắc tự động chèn `stream_options: {"include_usage": true}` trên trình thông dịch OpenAI-Compatible khi `stream: true` để đạt tỷ lệ quyết toán `EXACT` tối đa.
  2. **Quyết định rõ về `Webhooks` và `Response Cache`:**
     - *Webhooks cảnh báo ngân sách / duyệt MCP:* Chính thức thêm bảng `webhook_subscriptions` + worker gửi có chữ ký HMAC vào nhịp **`v0.2` (`Should`)** (ở `v0.1` cảnh báo hiển thị trên `Banner` Console và nhật ký/metrics OTel).
     - *Response Cache (`Exact/Semantic Cache`):* Chọn **Phương án A (Khuyến nghị):** Ghi rõ `Response Cache` là **`Later / Could (v0.3+)`** trong `srs.md` (chưa làm ở `v0.1–v0.2` để tránh phình phạm vi, giữ `FMEA-03`/`CT-28` làm cổng điều kiện khi bật ở `v0.3+`); hoặc **Phương án B:** Bổ sung `Exact Match Cache` đơn giản theo `Org DEK` ở `v0.2`.
- **Tiêu chí đóng (`Closure Criteria`):** Không còn bất kỳ tính năng nào xuất hiện trong bài test/runbook mà không có mã `TP-FR-*` và lược đồ tương ứng.

---

### `CR-10` — Hoàn thiện Mặt phẳng `Admin OpenAPI`, Luồng `Playground`, Cho phép `Ollama` Cục bộ Ngày đầu, Cây Lệnh CLI Đơn & Đóng gói Phông chữ `Air-Gap`
- **Liên kết lỗ hổng:** `AUD-005`, `AUD-009`, `AUD-017`, `AUD-018`, `AUD-022` | **Tác động:** `api-contracts.md`, `screen-specifications.md`, `design-system-integration.md`, `developer-experience.md`, `ADR-0001`, `ADR-0004`.
- **Đề xuất kỹ thuật trình duyệt:**
  1. **Một nhị phân duy nhất `tanpopo`:** Chuẩn hóa tên tệp thực thi duy nhất là `tanpopo` (`tanpopo serve --role=...`, có thể tạo symlink `tanpopod -> tanpopo` trong gói `.deb`/container để tương thích dịch vụ Linux) và cập nhật đầy đủ các nhánh lệnh `admin`, `upgrade`, `incident` vào cây CLI chuẩn tại `SPEC-OPS-04`.
  2. **Gỡ kẹt `Ollama` Cục bộ ở `UJ-01` (`TTFGR < 3m`):** Trong `Setup Wizard` (`SCR-01`) và `SCR-04`, khi chính `OPERATOR` / `Personal Admin` nhập địa chỉ mô hình cục bộ và tích xác nhận `Allow Private/Loopback Upstream`, Control Plane lưu thẳng vào `provider_connections.allow_private_ip = TRUE` (mà vẫn khóa cứng không bao giờ mở `169.254.169.254` / cổng quản trị của chính Tanpopo!), đồng thời Wizard tự tạo luôn `Model Deployment (HEALTHY)` + `Default Route` trong 1 giao dịch.
  3. **Chuẩn hóa `Playground` (`SCR-15`):** Web Console gọi `POST /admin/v1/playground/sessions` (kiểm tra RBAC + CSRF) để nhận một khóa phiên ngắn hạn `TTL = 15 phút` có hạn mức nhỏ, sau đó gọi qua đường chuẩn Data Plane để kiểm chứng trung thực `100%` đường nóng giống hệt SDK bên ngoài.
  4. **Giải pháp Đóng gói Phông chữ `Figtree` Cho `Air-Gap` Không Vi phạm `astryx doctor`:** Giữ nguyên `100%` tệp `astryx-themes.js` không chỉnh sửa tay (để vượt qua kiểm tra `theme-module` của `astryx doctor`), và cấu hình plugin/alias trong `vite.config.ts` ánh xạ `@astryxdesign/theme-neutral/fonts.css` sang tệp `@font-face` nội bộ trỏ tới các tệp `.woff2` của `Figtree` đóng gói tĩnh trong `web/console/public/fonts/`, đồng thời sử dụng đúng tập 29 biểu tượng ngữ nghĩa sẵn có của `@astryxdesign/core/Icon` (hoặc thêm gói `@phosphor-icons/react` / `lucide-react` giấy phép MIT sạch vào `ADR-0004`).
- **Tiêu chí đóng (`Closure Criteria`):** Đủ `100%` schema OpenAPI cho `SCR-01..16`, `tsc --noEmit` xanh, `astryx doctor --built` đạt `0 fail` và trình duyệt mở trong mạng ngắt Internet có `0` yêu cầu mạng thất bại.

---

## Phần B — Bảng Theo dõi Đóng Lỗ hổng Bắt buộc Trước Khi Viết Code (`Unresolved Gaps Closure Tracker`)

| Mã Khoảng trống | Liên kết Audit / CR | Mức độ | Chặn Cột mốc Nào? | Tiêu chí Kiểm chứng Đóng Hoàn toàn (`Verifiable Closure Criteria`) | Trạng thái |
|---|---|---|---|---|---|
| **`GAP-01`** | `AUD-011` / Cổng `G1–G3` | **`CRITICAL`** | **Chặn mọi dòng code `v0.1` & PoC** | Product Owner phản hồi phê duyệt `SD-G1-01..05`, `TD-G2-01..06`, `ADR-0001..0006`, `CR-01..10` và cấp phép chạy `SPIKE-01..04` trong [`decisions.md`](../governance/decisions.md). | `OPEN — AWAITING OWNER` |
| **`GAP-02`** | `AUD-001` / `CR-05` | **`CRITICAL`** | **Chặn `v0.1` Core & SDK** | `runtime.proto` định nghĩa đủ `ExtractUsageInput`, `TransformResponse`, `InspectNativeRequest`, `ValidateConnection` 2 pha và `HostCryptoSignSpec`; biên dịch `protoc` thành công `0` lỗi. | `OPEN` |
| **`GAP-03`** | `AUD-002` / `CR-05` | **`CRITICAL`** | **Chặn `v0.1` Gateway & Wasm** | `NormalizedInferenceRequestEnvelope` có đủ trường công cụ/JSON/sampling; đặc tả truyền `blob_ref` ngoài vùng nhớ `16 MiB` Wasm và sửa công thức ước tính token đa phương thức. | `OPEN` |
| **`GAP-04`** | `AUD-003` / `CR-06` | **`CRITICAL`** | **Chặn `v0.1` DB Migration** | `data-model.md` bổ sung đủ các bảng `config_changelog`, `distributed_leases`, `usage_rollups_hourly`, `oauth_authorization_sessions`, `connection_secret_bindings`, `org_extension_policies`, `extension_revocations`, sửa lỗi `NOT NULL` trên `extension_installations`/`approval_requests` và khóa tổ hợp `(org_id, id)` + `FORCE RLS`. | `OPEN` |
| **`GAP-05`** | `AUD-010` / `CR-06` | **`CRITICAL`** | **Chặn `v0.1` Vault, Backup & Audit** | Đồng nhất 1 công thức `AEAD AAD` duy nhất, băm `SHA-256` cho `Virtual Key`, thêm `project_encryption_keys` cho `Crypto-Shredding` cấp dự án, hoàn thiện quy trình làm mới cache ở `DR-03` và đổi `audit_events` sang chuỗi băm theo `org_id` + `seq_no` + `JCS`. | `OPEN` |
| **`GAP-06`** | `AUD-004` / `CR-06`, `07` | **`CRITICAL`** | **Chặn `v0.1` Ledger & `v0.3` HA** | Bổ sung khóa chống ghi đúp trên `usage_settlements`, cột đơn vị đa phương thức/bản chụp đơn giá, lược đồ khóa Valkey `{org_id}` chống `CROSSSLOT` và cơ chế đồng bộ giữ chỗ khi sập Valkey. | `OPEN` |
| **`GAP-07`** | `AUD-005` / `CR-10` | **`CRITICAL`** | **Chặn `v0.1` Admin API & UI** | `api-contracts.md` định nghĩa đầy đủ đường dẫn và schema cho `/v1/responses`, `/v1/embeddings`, `/v1/models` và toàn bộ `51` endpoint `/admin/v1/*` của `SCR-01..16` (kể cả luồng `Playground`). | `OPEN` |
| **`GAP-08`** | `AUD-006` / `CR-08` | **`CRITICAL`** | **Chặn `v0.1` Keys/Ops & `v0.2` MCP** | Sửa máy trạng thái `SM-01`, `SM-02`, `SM-04`, `SM-05` (`QUOTA_PAUSED -> REVOKED`), `SM-07` (`STREAMING_ACCRUING -> EXPIRED_RECLAIMED`), `SM-08`; tách `SM-09` cho A2A; đổi luồng duyệt MCP sang JSON-RPC tương thích IDE chuẩn. | `OPEN` |
| **`GAP-09`** | `AUD-007`, `008` / `CR-01..04` | **`CRITICAL`** | **Chặn khóa phạm vi `v0.1`** | Chốt số lượng Official Adapters `v0.1`, phân kỳ `Tier-3 Sidecar`, thống nhất `Trust Tier` + tiền tố `@tanpopo(-*)/*`, chốt 6 vai trò RBAC và hiệu chỉnh cổng thoát `v0.1`. | `OPEN — AWAITING OWNER` |
| **`GAP-10`** | `AUD-009` / `CR-10` | **`CRITICAL`** | **Chặn `v0.1` Onboarding `UJ-01`** | Hợp nhất quy tắc `allow_private_ip` cho `Ollama`/`vLLM`/LAN MCP và bổ sung bước tự tạo `Deployment (HEALTHY)` + `Route` trong Setup Wizard `SCR-01`. | `OPEN` |
| **`GAP-11`** | `AUD-011` | **`CRITICAL`** | **Chặn cổng kiểm thử `v0.1`** | Viết bổ sung `TP-AC-*` và ca kiểm thử `CT-*`/`AUTH-*` cho giới hạn tốc độ `RPM/TPM/Concurrency` (`TP-FR-0702`), chống nới lỏng chính sách con (`TP-FR-0701`), phân trang con trỏ & `409 Conflict` (`TP-FR-0901`), và nâng `traceability.md` lên ánh xạ `1-1` theo từng mã yêu cầu. | `OPEN` |
| **`GAP-12`** | `AUD-017`, `022` / `CR-10` | **`MAJOR`** | **Chặn `v0.1` Web Console Build** | Đặc tả cấu hình Vite đóng gói tĩnh phông chữ `Figtree` cho `Air-Gap`, chốt danh mục biểu tượng có thật, xử lý `ISS-001..005` khi mở pha code và sửa quy tắc chữ ký `offline_mode`. | `OPEN` |
| **`GAP-13`** | `AUD-015`, `018`..`021`, `023`..`030` | **`MAJOR / MINOR`** | **Chặn trước khi đóng băng `Contract v0.1`** | Đồng bộ từ điển khóa năng lực `snake_case`, 1 cây lệnh `tanpopo` CLI (kèm `reset-local-owner` và mở khóa sau `freeze-writes`), 1 bảng hằng số thời gian chờ/RAM, 1 bộ tên metric Prometheus/JSON log, cấm tiêm `Cookie` ở `Blind Broker`, khép dải màn hình `1024–1279px` và bổ sung runner macOS/Windows + đa trình duyệt vào CI. | `OPEN` |
