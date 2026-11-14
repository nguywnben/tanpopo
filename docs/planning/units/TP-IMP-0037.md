# `TP-IMP-0037` — Tường lửa Tác động Phụ MCP (`0` Retry `mutating`/`destructive`), Phê duyệt Con người Chế độ Kép Chuẩn JSON-RPC & Trình Gửi `Webhook` Ký HMAC

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0037` |
| **Milestone / Epic** | `M3` (`v0.2 MCP Governance & Authorized OAuth 2.1`) · `EP-07` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0023`, `TP-IMP-0036` — `GAP-01, 08`)* |
| **Quy mô (`Size`)** | `L` (`internal/ingress/mcp/firewall/*`, `internal/ingress/mcp/approval/*`, `internal/webhook/*`, `db/migrations/sqlite/0013_approvals_webhooks.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Chống nhân đôi giao dịch tác động phụ `FMEA-02`, phê duyệt gắn băm tham số & gửi Webhook chống SSRF)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Security / Agent Governance Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hoàn thiện lớp kiểm soát an toàn thực thi cho công cụ MCP và hệ thống thông báo sự kiện ngoại vi (`TP-FR-0403`, `TP-FR-0404`, `TP-FR-0803`, `CR-08`, `CR-09`):
1. **Tường lửa Phân loại Tác động Phụ (`Side-Effect Classification Firewall` — `FMEA-02`):** Phân loại mọi công cụ MCP thành `read_only`, `mutating`, hoặc `destructive` (công cụ chưa được phân loại tường minh mặc định coi là **`mutating`** theo đúng `FMEA-02`: cấm retry mù mà không làm tê liệt UX bằng hộp duyệt tay trên mọi công cụ!). Khóa cứng **`max_retries = 0` tuyệt đối** đối với mọi công cụ `mutating` và `destructive` khi gặp lỗi mạng lấp lửng (`timeout` / `connection reset`), trả đúng mã chuẩn **`ambiguous_side_effect_timeout`** (`TP-FR-0404`).
2. **Phê duyệt Con người Chế độ Kép Tương thích `100%` Trình khách MCP Chuẩn (`Dual-Mode HITL Approval` — `SM-06`, `CR-08`):**
   - *Chế độ 1 (Chờ đồng bộ ngắn hạn `<= 60 giây`)* kèm nhịp thông báo tiến trình MCP hợp chuẩn cho tác nhân đang trực tuyến.
   - *Chế độ 2 (Bất đồng bộ chuẩn JSON-RPC — `CR-08`):* Trả về kết quả/lỗi cấu trúc chuẩn JSON-RPC của MCP bên trong HTTP `200`, cung cấp `approval_request_id` và `pre_approval_token` gắn chặt với **bộ 5 thành phần `SHA-256(JCS({ org_id, virtual_key_id, server_slug, tool_name, arguments }))`** (`CR-08`) dùng đúng 1 lần sau khi `SECURITY_REVIEWER` / `TEAM_ADMIN` / `ORG_OWNER` bấm duyệt.
3. **Trình Gửi `Webhook` Sự kiện Có Chữ ký `HMAC-SHA256` (`CR-09`):** Bắn thông báo tin cậy (ngưỡng ngân sách `50%/80%/100%`, yêu cầu chờ duyệt MCP, cách ly plugin, Kill-Switch) qua `SSRFGuardDialer` kèm chữ ký `X-Tanpopo-Signature-256` và `X-Tanpopo-Timestamp` chống phát lại.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/ingress/mcp/firewall/classifier.go` (chốt chặn `0` retry cho `mutating`/`destructive` và giới hạn tốc độ gọi riêng cho công cụ nguy hiểm).
  - `internal/ingress/mcp/approval/workflow.go` (máy trạng thái `SM-06`: `PENDING -> APPROVED -> CONSUMED | REJECTED | EXPIRED`, kiểm chứng khớp tuyệt đối mã băm bộ 5 thành phần `JCS` khi gọi lại — sai bất kỳ trường nào đều trả **`parameter_hash_mismatch`** theo `CT-22`).
  - `internal/webhook/dispatcher.go` (hàng đợi gửi bất đồng bộ có thoái lui mũ giới hạn, đi qua `SSRFGuardDialer`, ký `HMAC-SHA256(secret, timestamp + "." + raw_body)`).
  - `internal/admin/http/mcp_approvals.go` & `webhooks.go` (`/admin/v1/mcp/*`, `/admin/v1/approvals/*`, `/admin/v1/webhooks/*`).
  - `db/migrations/sqlite/0013_approvals_webhooks.sql` (`approval_requests` có `org_id`/`project_id` cho phép `NULL` ở cấp Instance, `webhook_subscriptions`, `webhook_deliveries` — khớp 100% `TP-IMP-0003`).
- **Out-of-Scope:** Máy trạng thái tác vụ liên tác nhân `A2A` (`SM-09` tách riêng theo `CR-08` thuộc `TP-IMP-0042`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0403`, `TP-FR-0404`, `TP-FR-0803`, `TP-BR-0603`, `SM-06`, `CR-08`, `CR-09`, `CT-22`, `FMEA-02`.
- **Dependencies:** `TP-IMP-0023`, `TP-IMP-0036`.
- **Design Refs:** [`mcp-a2a-boundaries.md`](../../specifications/mcp-a2a-boundaries.md) (`SPEC-EXT-05`), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-02`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-08`, `CR-09`).

## 4. API, Wire & Data Contracts
- **Bất biến Ràng buộc Bộ 5 Thành phần (`5-Tuple Bound Single-Use Approval` — `CT-22` & `CR-08`):** Mã phê duyệt chỉ mở khóa được đúng lệnh gọi khớp cả `org_id`, `virtual_key_id`, `server_slug`, `tool_name` và `JCS(arguments)` rồi tự chuyển sang `CONSUMED` ngay trong giao dịch nguyên tử đầu tiên.
- **Chống Tấn công Tự Phê duyệt:** Tác nhân/khóa tạo yêu cầu công cụ không thể tự gọi API quản trị duyệt cho chính mình (chỉ `SECURITY_REVIEWER` / `TEAM_ADMIN` / `ORG_OWNER` hợp lệ mới có quyền `mcp:approval:decide`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`FMEA-02` Zero Blind Retry on Mutating/Destructive Tool):** Given công cụ `stripe__refund_charge` (`mutating`) trên `FakeMCPServer` nhận được lệnh rồi cố tình ngắt TCP trước khi trả lời, When gọi qua `/mcp`, Then `FakeMCPServer` ghi nhận **đúng 1 lệnh gọi duy nhất** (`0` lần thử lại) và khách hàng nhận mã chuẩn **`ambiguous_side_effect_timeout`**.
- **`AC-2` (`CT-22` Sync & Async JSON-RPC Approval Flow):** Kiểm chứng trọn vẹn cả luồng chờ trực tiếp `< 60s` được duyệt kịp thời lẫn luồng quá hạn chuyển sang mã phê duyệt bất đồng bộ chuẩn JSON-RPC, nhận thông báo `Webhook` ký HMAC hợp lệ, bấm duyệt qua Admin API và gọi lại thành công đúng 1 lần.
- **`NEG-1` (`CT-22` Parameter or Key Bait-and-Switch):** Tác nhân xin duyệt với `{"env": "dev"}`, sau khi được duyệt lại truyền token đó kèm `{"env": "prod"}` (hoặc dùng `virtual_key_id` khác) → bị từ chối cứng **`parameter_hash_mismatch`** kèm cảnh báo kiểm toán.
- **`NEG-2` (`Webhook SSRF`):** Đăng ký URL Webhook trỏ vào `http://169.254.169.254/...` hoặc `http://127.0.0.1:6379` → bị `SSRFGuardDialer` chặn cứng.

## 6. Test Commands & Required Evidence
- Thuần Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/ingress/mcp/... ./internal/webhook/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/ingress/mcp/... ./internal/webhook/...`)*
- Tranh chấp dữ liệu (`-race`): `go test -v -race ./internal/ingress/mcp/... ./internal/webhook/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Bí mật ký `Webhook` lưu trong `vault` có `AEAD AAD`; thân gói tin Webhook đi qua `privacy.Redactor` để không làm rò rỉ dữ liệu nhạy cảm ra kênh Slack/PagerDuty bên ngoài.
- **Rollback:** Bảng cộng thêm `0013_approvals_webhooks.sql`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` & `TP-IMP-0002` (khóa `SM-06` tách khỏi `SM-09` & hợp đồng JSON-RPC `CR-08`), `TP-IMP-0023` và `TP-IMP-0036` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca `CT-22`, `FMEA-02` và kiểm thử chữ ký/SSRF của Webhook dưới `-race`, vượt qua `REQUIRES_STRONG_REVIEW`.
