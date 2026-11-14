# `TP-IMP-0017` — Đường Đi Giữ Nguyên Phương Ngữ Gốc Không Mất Mát (`/native/{dialect}/{upstreamPath}`) & Chốt Chặn Cùng Phương Ngữ

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0017` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-03` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0015` — `GAP-01, 02`)* |
| **Quy mô (`Size`)** | `M` (`internal/gateway/native/*`) |
| **Cấp độ Review** | `STANDARD_REVIEW` *(Ràng buộc định tuyến cùng phương ngữ & trích xuất token không làm biến dạng thân gốc)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Gateway / Protocol Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Cung cấp đường đi **Giữ Nguyên Phương Ngữ Gốc Không Mất Mát (`Lossless Native Dialect Passthrough` — `TP-FR-0105`)** tại không gian đường dẫn `/native/{dialect}/{upstreamPath...}` (ví dụ `/native/anthropic/v1/messages`, `/native/gemini/v1beta/models/...`) để các SDK đặc thù và công cụ lập trình giữ nguyên `100%` tính năng chuyên sâu của từng hãng (`prompt caching`, `thinking/signature blocks`, `citations`, `batch`) nhưng **vẫn đi qua đầy đủ đường ống xác thực `Virtual Key`, chính sách, `SSRFGuard`, `BlindBroker`, sổ cái chi phí và vết OpenTelemetry**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/gateway/native/handler.go` (chuẩn hóa đường dẫn chống leo thư mục `path.Clean` / từ chối `..` và chuỗi truy vấn chèn lậu, xác thực `Virtual Key` qua header `Authorization: Bearer tpk_...` hoặc `x-api-key: tpk_...` / `x-goog-api-key: tpk_...` rồi lột sạch khóa `tpk_*` trước khi giao cho `BlindBroker` tiêm khóa thật của nhà cung cấp).
  - `internal/gateway/native/inspector.go` (chỉ đọc nhẹ tên `model`, cờ `stream` và móc trích xuất lượng tiêu thụ token `ExtractNativeUsage` ở cuối phản hồi/luồng mà **không giải mã-ghi lại làm mất trường lạ hay đổi thứ tự nội dung khối ký số**).
- **Out-of-Scope:** Không bao giờ thực hiện dịch chéo phương ngữ trên nhánh `/native/*` (ví dụ cấm tự dịch `/native/anthropic/v1/messages` sang định dạng OpenAI).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0105`, `TP-NFR-0102`, `TP-BR-0101`, `TP-BR-0503`, `CT-13`.
- **Dependencies:** `TP-IMP-0015`.
- **Design Refs:** [`api-protocol-compatibility.md`](../../specifications/api-protocol-compatibility.md) (`SPEC-EXT-06`), [`api-contracts.md`](../../specifications/api-contracts.md) (`SPEC-API-01`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-05`).

## 4. API, Wire & Data Contracts
- **Bất biến Cùng Phương Ngữ (`Same-Dialect Routing Invariant`):** Yêu cầu vào `/native/anthropic/*` chỉ được phép định tuyến hoặc chuyển đổi dự phòng trước luồng sang các `ModelDeployment` có `native_dialect == "anthropic"` (như Anthropic trực tiếp hoặc đối tác lưu trữ cùng phương ngữ Anthropic hợp lệ); nếu cấu hình trỏ sang phương ngữ khác → từ chối `400 incompatible_native_dialect`.
- **Chống Xâm nhập Đường dẫn (`Upstream Path Allowlist`):** `upstreamPath` phải khớp mẫu đường dẫn được Adapter của phương ngữ đó khai báo cho phép (ví dụ `^v1/messages(/count_tokens)?$`), chặn việc lợi dụng `/native/*` để gọi API quản lý tài khoản/thanh toán của hãng thượng nguồn.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`CT-13` Byte-Preserving Native Passthrough):** Given thân yêu cầu Anthropic `/native/anthropic/v1/messages` chứa `cache_control: {"type": "ephemeral"}`, khối `thinking` có `signature` và một trường thử nghiệm tương lai `x_future_beta_field`, When gửi qua Gateway (cả đồng bộ và SSE `message_start / content_block_delta / message_delta`), Then `FakeLLMServer` nhận nguyên vẹn từng trường không rơi một byte nào, khách hàng nhận nguyên vẹn luồng sự kiện gốc và Gateway vẫn trích xuất chính xác `input_tokens`, `cache_read_input_tokens`, `output_tokens`.
- **`NEG-1` (`Virtual Key Leak Prevention`):** Gửi khóa ảo qua `x-api-key: tpk_live_canary123` → `FakeLLMServer` chỉ nhận khóa thật của hãng do `BlindBroker` gắn, tuyệt đối không nhận chuỗi `tpk_live_canary123`.
- **`NEG-2` (`Path Traversal / Admin Endpoint Abuse`):** Gửi `/native/openai/v1/../organization/admin_api_keys` → bị chặn `403 native_path_not_allowed` trước khi mở kết nối đi ra ngoài.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/gateway/native/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Danh sách đường dẫn con cho phép (`allowed_native_paths`) trên mỗi phương ngữ mặc định đóng (`Default-Deny`); cấm chuyển tiếp các header quản trị đặc quyền ngoài danh sách trắng của Adapter.
- **Rollback:** Tắt cờ `gateway.native_passthrough.enabled` đưa `/native/*` về `404` an toàn mà không ảnh hưởng `/v1/*`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (khóa móc `ExtractNativeUsage` trong hợp đồng mở rộng) và `TP-IMP-0015` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca `CT-13` cho các phương ngữ `openai`, `anthropic`, `gemini` dưới `-race`.
