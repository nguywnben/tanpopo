# `TP-IMP-0009` — Xác thực Quản trị Cục bộ, Middleware `RBAC` 6 Vai trò, Vòng đời `Virtual Key` (`SHA-256`) & Ảnh chụp `ConfigSnapshot`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0009` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0007`, `TP-IMP-0008` — `GAP-01, 08, 09`)* |
| **Quy mô (`Size`)** | `L` (`internal/iam/*`, `internal/config/snapshot/*`, `db/migrations/sqlite/0003_iam_keys.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Xác thực, phân quyền đa người thuê, thu hồi khóa `< 1s` & chống Oracle `404`)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Security / IAM Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa lõi danh tính, ủy quyền 6 vai trò chuẩn (`org_owner`, `org_admin`, `security_reviewer`, `project_admin`, `developer`, `billing_viewer`), vòng đời `Virtual Key` chỉ lưu mã băm `SHA-256` kèm tiền tố nhận diện (`tpk_live_` / `tpk_test_`) theo máy trạng thái `SM-05`, và bộ nhớ cấu hình bất biến `Copy-on-Write ConfigSnapshot` (`atomic.Pointer`) giúp đường nóng suy luận đọc cấu hình trong `0` câu lệnh SQL và vô hiệu hóa khóa bị thu hồi dưới `< 1 giây` trên nút đơn (`ADR-0006`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/iam/bootstrap/token.go` (sinh `Bootstrap Token` 1 lần vào tệp `0600` hoặc in ẩn danh khi DB trống; tự hủy vĩnh viễn ngay sau khi tạo tài khoản `org_owner` đầu tiên).
  - `internal/iam/auth/local.go` (mật khẩu `Argon2id`, phiên quản trị cookie `HttpOnly; SameSite=Strict; Secure` + token chống `CSRF` cho mọi lệnh ghi).
  - `internal/iam/rbac/authorizer.go` (kiểm tra quyền từng hành động `Authorize(actor, action, scope)` trả đồng nhất `404 resource_not_found` khi khác `org_id` để chống dò ID — `AUTH-13`).
  - `internal/iam/vkey/service.go` (tạo khóa trả rõ đúng 1 lần duy nhất, xoay khóa có cửa sổ ân hạn `ROTATING_GRACE`, hết hạn tự động và hai chế độ thu hồi: thu hồi thường vs thu hồi khẩn cấp cắt luồng đang mở qua `context.CancelFunc`).
  - `internal/config/snapshot/manager.go` (`atomic.Pointer[ConfigSnapshot]`, kênh thông báo `SQLite config_revisions` cục bộ để hoán đổi ảnh chụp `< 1s`).
  - `db/migrations/sqlite/0003_iam_keys.sql` (`principals`, `role_bindings`, `virtual_keys`, `config_revisions`).
- **Out-of-Scope:** Liên kết `Enterprise OIDC SSO` & quy tắc `Four-Eyes` (thuộc `TP-IMP-0041`); truyền tin thu hồi liên nút qua `Valkey Pub/Sub` (thuộc `TP-IMP-0040`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0501`, `TP-FR-0502`, `TP-FR-0503`, `TP-NFR-0404`, `TP-BR-0101`..`0104`, `SM-05`, `AUTH-03`..`AUTH-07`, `AUTH-11`, `AUTH-13`, `AUTH-15`.
- **Dependencies:** `TP-IMP-0007`, `TP-IMP-0008`.
- **Design Refs:** [`ADR-0006`](../../architecture/adr/0006-immutable-config-snapshots-and-fast-revocation.md), [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) Mục 2 & 3, [`business-entity-map.md`](../../requirements/business-entity-map.md) (`SM-05`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`AUTH-03`..`15`).

## 4. API, Wire & Data Contracts
- **Định dạng `Virtual Key`:** `tpk_<env>_<32_base62_chars>`; cột DB `key_hash = hex(SHA-256("tanpopo:vkey:v1:" + rawKey))`, chỉ lưu `key_prefix` (12 ký tự đầu) + `last_four` để tra cứu kiểm toán. So sánh mã băm bằng `subtle.ConstantTimeCompare`.
- **Hợp đồng `ConfigSnapshot`:** Cấu trúc bất biến chỉ đọc (`Read-Only Struct`) chứa bản đồ tra cứu `O(1)` từ `key_hash -> VirtualKeyView` và `org_id/project_id/env -> EffectiveConfig`; tuyệt đối không khóa `Mutex` trên đường đọc.
- **Chống Oracle Chéo Tổ chức (`AUTH-13`):** Khi `Principal` của `Org A` truyền `resource_id` có thật của `Org B`, tầng lưu trữ + `Authorizer` lọc cứng theo `actor.OrgID` nên luôn trả về cùng mã lỗi và thông điệp `404 resource_not_found` như một ID ngẫu nhiên không tồn tại.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given máy chủ khởi tạo trống, When dùng `Bootstrap Token` tạo `org_owner` đầu tiên, Then lần gọi thứ hai với cùng `Bootstrap Token` bị từ chối cứng `401 bootstrap_already_completed` (`AUTH-02`).
- **`AC-2`:** Given `Virtual Key K1` đang `ACTIVE`, When xoay vòng với `grace_seconds = 300`, Then cả `K1` (`ROTATING_GRACE`) và khóa kế nhiệm `K2` đều xác thực hợp lệ trong 300 giây; khi `FakeClock.Advance(301s)`, `K1` tự động chuyển `EXPIRED` và trả `401 virtual_key_expired` (`AUTH-05`).
- **`AC-3`:** Given một luồng SSE đang mở bằng `Virtual Key K1`, When gọi `RevokeVirtualKey(K1, emergency=true)`, Then `ConfigSnapshot` cập nhật trong `< 1 giây`, luồng SSE đang chạy bị hủy ngay lập tức và mọi yêu cầu mới dùng `K1` nhận `401 virtual_key_revoked` (`AUTH-06`).
- **`NEG-1` (`AUTH-11` Privilege Escalation):** Tác nhân vai trò `developer` hoặc `billing_viewer` gọi hành động duyệt quyền Plugin hoặc xem danh mục bí mật → bị từ chối `403 insufficient_role_permission` kèm bản ghi kiểm toán.
- **`NEG-2` (`AUTH-13` Cross-Tenant ID Probing):** Dùng `Virtual Key` hoặc phiên của `Org A` truy vấn ID hợp lệ của `Org B` → nhận `404` có thời gian phản hồi và thân JSON giống hệt ID không tồn tại.
- **`NEG-3` (`AUTH-03` CSRF):** Gửi yêu cầu ghi bằng Cookie phiên trình duyệt mà thiếu hoặc sai `X-Tanpopo-CSRF` → từ chối `403 csrf_validation_failed`.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/iam/... ./internal/config/snapshot/...`
- Benchmark đường đọc ảnh chụp: `go test -bench=BenchmarkSnapshotLookup -benchmem ./internal/config/snapshot/...` (`0 allocs/op`).

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Không bao giờ ghi `rawKey`, mật khẩu hay mã phiên vào DB, nhật ký hoặc span OTel; mọi điểm cuối quản trị mặc định `Deny` nếu chưa khai báo tường minh quyền `Action` yêu cầu.
- **Rollback:** Bảng cộng thêm `0003_iam_keys.sql`; có thể vô hiệu hóa toàn bộ khóa qua cờ khẩn cấp mà không mất dữ liệu.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (vá chuyển tiếp `SM-05`), `TP-IMP-0007` và `TP-IMP-0008` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca `AUTH-03..07, 11, 13, 15` dưới `-race`, `0 allocs/op` khi tra cứu `ConfigSnapshot`, vượt qua `REQUIRES_STRONG_REVIEW`.
