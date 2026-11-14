# `TP-IMP-0024` — Admin Control Plane REST API (Phần 1): Khởi tạo Bootstrap, Phiên/CSRF, Tổ chức/Dự án/Môi trường, RBAC & `Virtual Keys` (`ETag`/`409`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0024` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-06` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0009`, `TP-IMP-0023` — `GAP-01, 07, 11`)* |
| **Quy mô (`Size`)** | `L` (`internal/admin/http/server.go`, `bootstrap.go`, `auth.go`, `tenancy.go`, `vkeys.go`, `pagination.go`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Mặt phẳng quản trị `/admin/v1/*`, kiểm soát tranh chấp `ETag/409`, phân trang con trỏ & ủy quyền từng hành động)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Control-Plane / Security Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa khung máy chủ HTTP của **Admin Control Plane (`/admin/v1/*`)** cùng nhóm điểm cuối nền tảng (Khởi tạo lần đầu `Bootstrap`, Đăng nhập/Phiên/CSRF, Quản lý `Organizations` / `Projects` / `Environments`, Gán quyền `RBAC` 6 vai trò và Quản lý vòng đời `Virtual Keys`), tuân thủ nghiêm ngặt đặc tả `OpenAPI 3.1` đã khóa ở `TP-IMP-0002`, kiểm soát đồng thời lạc quan bằng `ETag` + `If-Match` trả `409 optimistic_lock_conflict` (`TP-FR-0901`), và phân trang con trỏ mờ có ký HMAC theo `org_id` (`GAP-11`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/admin/http/server.go` (middleware bắt buộc đính kèm `ActionPermission` cho mọi route `/admin/v1/*`, kiểm tra `CSRF`, giới hạn tốc độ chống dò mật khẩu trên `/admin/v1/auth/login`, chuẩn hóa lỗi `CanonicalError`).
  - `internal/admin/http/etag.go` (sinh `ETag: "v<revision>"` trên mọi tài nguyên cấu hình; từ chối `PUT/PATCH/DELETE` thiếu hoặc lệch `If-Match` với `428 Precondition Required` / `409 Conflict`).
  - `internal/admin/http/pagination.go` (con trỏ `cursor` mã hóa `base64url` + chữ ký HMAC gắn `org_id` và bộ lọc truy vấn để chặn giả mạo con trỏ nhảy trang sang tổ chức khác; `limit` mặc định `50`, tối đa `200`).
  - `internal/admin/http/bootstrap.go`, `auth.go`, `tenancy.go`, `rbac.go`, `vkeys.go`.
- **Out-of-Scope:** Các điểm cuối quản lý Phần mở rộng, Kết nối, Tuyến, Ngân sách, Vết (thuộc `TP-IMP-0025`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0501`, `TP-FR-0502`, `TP-FR-0503`, `TP-FR-0901`, `AUTH-02`..`AUTH-07`, `AUTH-11`, `AUTH-13`, `AUTH-15`, `GAP-07`, `GAP-11`.
- **Dependencies:** `TP-IMP-0009`, `TP-IMP-0023`.
- **Design Refs:** [`api-contracts.md`](../../specifications/api-contracts.md) (`SPEC-API-01`), [`developer-experience.md`](../../operations/developer-experience.md) (`SPEC-OPS-04`), [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md).

## 4. API, Wire & Data Contracts
- **Quy tắc Hiển thị Khóa 1 Lần Duy Nhất (`Show-Once Plaintext Key`):** Chỉ phản hồi `201 Created` của `POST /admin/v1/projects/{projectId}/virtual-keys` và `POST .../rotate` mới chứa trường `secret_key` một lần duy nhất; mọi lệnh `GET` danh sách hoặc chi tiết sau đó chỉ trả `{ id, name, key_prefix, last_four, state, expires_at }`.
- **Kiểm soát Ghi Tranh chấp (`Optimistic Concurrency Control`):** Hai quản trị viên cùng đọc phiên bản `ETag: "v3"` và cùng gửi cập nhật → người gửi thứ hai bắt buộc nhận `409 optimistic_lock_conflict` kèm bản chụp hiện tại để hợp nhất, không bao giờ ghi đè âm thầm.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given máy chủ mới khởi động, When kiểm tra `GET /admin/v1/bootstrap/status` (`initialized: false`), gửi `POST /admin/v1/bootstrap/complete` kèm `Bootstrap Token` hợp lệ, tạo `Project`, `Environment` và `Virtual Key`, Then mọi bước ghi đầy đủ vào `audit_events` (`TP-IMP-0023`) và cập nhật `ConfigSnapshot` tức thì.
- **`AC-2` (`GAP-11` Cursor Pagination Determinism):** Given 125 `Virtual Keys`, When duyệt tuần tự với `limit=50` qua 3 trang bằng `next_cursor`, Then nhận đủ đúng 125 bản ghi không trùng, không sót ngay cả khi có bản ghi mới chèn vào trang đầu.
- **`NEG-1` (`Cross-Tenant Cursor Tampering`):** Dùng `next_cursor` hợp lệ sinh ra từ phiên của `Org A` truyền vào phiên của `Org B` → từ chối `400 invalid_pagination_cursor`.
- **`NEG-2` (`Stale If-Match`):** Gửi `PATCH` với `If-Match: "v1"` lên tài nguyên đang ở `"v2"` → trả `409 optimistic_lock_conflict`.
- **`NEG-3` (`Brute-Force Login Rate Limit`):** Gửi quá 10 lần sai mật khẩu liên tiếp từ cùng địa chỉ IP → trả `429 auth_rate_limited` kèm bản ghi kiểm toán bảo mật.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/admin/http/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Bài kiểm thử phản xạ route (`TestAllAdminRoutesHaveExplicitPermission`) quét bảng định tuyến HTTP và đánh trượt CI nếu có bất kỳ route `/admin/v1/*` nào (ngoài `/bootstrap/status` và `/auth/login`) không gắn rõ hằng số quyền `RBAC`.
- **Rollback:** Lớp HTTP tách rời bên trên tầng dịch vụ `iam` và `audit`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (hoàn thiện đặc tả `OpenAPI 3.1` cho `GAP-07`), `TP-IMP-0009` và `TP-IMP-0023` đã hoàn tất.
- **DoD:** Khớp 100% lược đồ `OpenAPI 3.1`, xanh toàn bộ ca `AUTH-02..07, 11, 13, 15`, vượt qua `REQUIRES_STRONG_REVIEW`.
