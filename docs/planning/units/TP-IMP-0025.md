# `TP-IMP-0025` — Admin Control Plane REST API (Phần 2): Phần mở rộng, Kết nối, Mô hình/Đích, Tuyến, Chính sách, Ngân sách/Đối soát, Vết & Vận hành

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0025` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0012`, `TP-IMP-0020`, `TP-IMP-0021`, `TP-IMP-0024` — `GAP-01, 07`)* |
| **Quy mô (`Size`)** | `L` (`internal/admin/http/extensions.go`, `connections.go`, `models.go`, `routes.go`, `policies.go`, `finops.go`, `traces.go`, `ops.go`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Ghi bí mật một chiều, duyệt quyền phần mở rộng, xuất báo cáo đối soát tài chính & kích hoạt Kill-Switch)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Control-Plane Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hoàn thiện toàn bộ các nhóm điểm cuối còn lại của `/admin/v1/*` phục vụ `Web Console` và `tanpopo` CLI trong `MVP (v0.1)`: Quản lý vòng đời `Extensions` (cài đặt, xem chênh lệch quyền $\Delta P$, phê duyệt, nâng cấp, quay lui, cách ly), `Secrets & Provider Connections` (ghi khóa một chiều + kiểm tra kết nối trực tiếp), `Logical Models & Deployments`, `Routes & Policies`, `Budgets & Chargeback Export (CSV/JSON)`, `Request Traces`, `Audit Log / Blast-Radius` và `Emergency Kill-Switches`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/admin/http/extensions.go` (`/admin/v1/extensions/*`: tải lên/cài đặt gói, lấy bảng so sánh quyền `permission-diff`, duyệt quyền, nâng cấp, quay lui `< 5s`).
  - `internal/admin/http/connections.go` & `secrets.go` (`/admin/v1/secrets` ghi một chiều không bao giờ trả khóa rõ, `/admin/v1/connections/*` kèm `POST .../test` kiểm tra sức khỏe qua `BlindBroker` + `SSRFGuard` và cờ cho phép CIDR cục bộ tường minh khi người dùng xác nhận trên Wizard).
  - `internal/admin/http/models.go`, `routes.go`, `policies.go` (CRUD có `ETag/If-Match` và xác thực không nới lỏng chính sách `422 policy_relaxation_forbidden`).
  - `internal/admin/http/finops.go` (`/admin/v1/budgets/*`, `/admin/v1/usage/summary`, `/admin/v1/usage/export` xuất luồng `CSV` / `JSON` tách rạch ròi 3 cột `exact_micro_usd`, `estimated_micro_usd`, `unmetered_request_count`).
  - `internal/admin/http/traces.go` & `ops.go` (`/admin/v1/traces`, `/admin/v1/audit-events`, `/admin/v1/audit-events/verify`, `/admin/v1/kill-switches`, `/admin/v1/blast-radius`, `/admin/v1/overview/summary`).
- **Out-of-Scope:** Các điểm cuối quản lý `MCP Servers / Tool Approvals` và `Playground Ephemeral Session` (thuộc `TP-IMP-0037` và `TP-IMP-0038` ở `M3 / v0.2`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0201`..`0204`, `TP-FR-0304`, `TP-FR-0306`, `TP-FR-0601`, `TP-FR-0701`, `TP-FR-0704`, `TP-FR-0705`, `TP-FR-0802`, `TP-FR-0803`, `TP-FR-0901`, `GAP-07`.
- **Dependencies:** `TP-IMP-0012`, `TP-IMP-0020`, `TP-IMP-0021`, `TP-IMP-0024`.
- **Design Refs:** [`api-contracts.md`](../../specifications/api-contracts.md) (`SPEC-API-01`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-01`..`16` Data Contracts).

## 4. API, Wire & Data Contracts
- **Bất biến Chống Phun Công thức CSV (`CSV Injection Sanitization`):** Khi xuất báo cáo đối soát `GET /admin/v1/usage/export?format=csv`, mọi chuỗi tên dự án/khóa/mô hình bắt đầu bằng `=`, `+`, `-`, `@`, `\t`, `\r` đều phải được tiền tố hóa an toàn để chống tấn công thực thi công thức khi mở trong bảng tính.
- **Kiểm chứng Hợp đồng Tự động:** Toàn bộ phản hồi của `TP-IMP-0024` và `TP-IMP-0025` được kiểm định tự động trong bài test đối chiếu trực tiếp với tệp `openapi/admin-v1.yaml` (`0` trường thiếu/sai kiểu).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given bộ kiểm thử tích hợp Admin API, When thực thi trọn vẹn luồng cấu hình từ nạp khóa mật → tạo kết nối → cài gói mở rộng → xem `permission-diff` → duyệt quyền → tạo mô hình/tuyến/chính sách/ngân sách → xuất đối soát CSV → bật/tắt Kill-Switch, Then `100%` phản hồi khớp lược đồ `openapi/admin-v1.yaml` và mọi thao tác ghi đều sinh bản ghi kiểm toán hợp lệ.
- **`NEG-1` (`Write-Only Secret Verification`):** Gọi mọi điểm cuối `GET /admin/v1/secrets*` và `GET /admin/v1/connections*` → quét `CanaryScanner` trên toàn bộ thân JSON trả về xác nhận `0` vết khóa mật.
- **`NEG-2` (`CSV Formula Injection`):** Đặt tên dự án là `=HYPERLINK("http://evil.test","click")` rồi xuất CSV → ô CSV được bọc thoát an toàn, không thể tự thực thi công thức.
- **`NEG-3` (`Role Separation`):** Vai trò `FINOPS_VIEWER` đọc được `/admin/v1/usage/summary` nhưng nhận `403` khi cố gọi `/admin/v1/secrets` hoặc `/admin/v1/kill-switches`.

## 6. Test Commands & Required Evidence
- Thuần Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/admin/http/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/admin/http/...`)*
- Tranh chấp dữ liệu (`-race`): `go test -v -race ./internal/admin/http/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Điểm cuối `POST /admin/v1/connections/test` cũng đi qua chính `SSRFGuardDialer` và giới hạn tốc độ chặt chẽ để ngăn kẻ có tài khoản cấp thấp biến tính năng "Test Connection" thành công cụ quét mạng nội bộ (`SSRF Oracle`).
- **Rollback:** Lớp HTTP handler không trạng thái trên các mô-đun miền đã kiểm thử.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (`openapi/admin-v1.yaml`), `TP-IMP-0012`, `TP-IMP-0020`, `TP-IMP-0021` và `TP-IMP-0024` đã hoàn tất.
- **DoD:** Xanh toàn bộ kiểm thử hợp đồng OpenAPI cho đủ các điểm cuối `v0.1`, vượt qua `REQUIRES_STRONG_REVIEW`.
