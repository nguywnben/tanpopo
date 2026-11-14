# `TP-IMP-0009` — Xác thực Quản trị Cục bộ, Middleware `RBAC` 6 Vai trò Chuẩn, Vòng đời `Virtual Key` (`vk_tp_`) & Ảnh chụp `ConfigSnapshot`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0009` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0007`, `TP-IMP-0008` — `GAP-01, 08, 09`)* |
| **Quy mô (`Size`)** | `L` (`go.mod`, `internal/iam/*`, `internal/config/snapshot/*`, `db/migrations/sqlite/0003_iam_keys.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Xác thực, phân quyền đa người thuê, thu hồi khóa `< 1s` & chống Oracle `404`)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Security / IAM Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa lõi danh tính, ủy quyền **6 vai trò chuẩn (`OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`)**, vòng đời `Virtual Key` tiền tố `vk_tp_<env>_<32_base62>` lưu mã băm `SHA-256` thuần trên 2 cột (`active_key_hash`, `previous_key_hash`) theo máy trạng thái chuẩn `SM-05`, và bộ nhớ cấu hình bất biến `Copy-on-Write ConfigSnapshot` (`atomic.Pointer`) theo dõi bảng `config_changelog` (`TP-IMP-0007`) giúp đường nóng suy luận đọc cấu hình trong `0` câu lệnh SQL và vô hiệu hóa khóa bị thu hồi dưới `< 1 giây` trên nút đơn (`ADR-0006`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (thêm `golang.org/x/crypto` cho `argon2id`).
  - `internal/iam/bootstrap/token.go` (sinh `Bootstrap Token` 1 lần vào tệp `0600` khi DB trống; tự hủy vĩnh viễn ngay sau khi tạo `ORG_OWNER`/`OPERATOR` đầu tiên và trả `410 bootstrap_already_completed` cho mọi lần gọi lại — `AUTH-02`).
  - `internal/iam/auth/local.go` (mật khẩu `Argon2id`, phiên quản trị cookie `HttpOnly; SameSite=Strict; Secure` + tiêu đề chống `CSRF` chuẩn `X-CSRF-Token` — `AUTH-03`; tách biệt cứng mặt phẳng凭证: từ chối dùng `Virtual Key` trên `/admin/v1/*` và từ chối dùng Cookie quản trị trên `/v1/*` với `401 invalid_credential_plane` — `AUTH-07`).
  - `internal/iam/rbac/authorizer.go` (kiểm tra quyền từng hành động `Authorize(actor, action, scope)` cho đủ 6 vai trò chuẩn; lờ đi mọi trường `org_id` giả mạo trong thân JSON bằng cách lấy `org_id` trực tiếp từ ngữ cảnh xác thực — `AUTH-13`; trả đồng nhất `404 resource_not_found` khi khác `org_id` để chống dò ID — `AUTH-15`).
  - `internal/iam/vkey/service.go` (quản lý `SM-05`: `CREATED_ACTIVE -> ROTATING_DUAL_VALID -> QUOTA_PAUSED | SUSPENDED | REVOKED | EXPIRED`; kiểm tra khớp môi trường `vk_tp_staging_...` gọi môi trường `prod` trả `403 environment_scope_denied` — `AUTH-04`; hai chế độ thu hồi: thu hồi thường vs thu hồi khẩn cấp cắt luồng đang mở qua `context.CancelFunc`).
  - `internal/config/snapshot/manager.go` (`atomic.Pointer[ConfigSnapshot]`, theo dõi `config_changelog` để hoán đổi ảnh chụp nguyên tử `< 1s`).
  - `db/migrations/sqlite/0003_iam_keys.sql` (`principals` kèm chỉ mục duy nhất một phần `WHERE org_id IS NULL` cho `OPERATOR`, `role_bindings`, `virtual_keys`).
- **Out-of-Scope:** Liên kết `Enterprise OIDC SSO` & quy tắc `Four-Eyes` (thuộc `TP-IMP-0041`); truyền tin thu hồi liên nút qua `Valkey Pub/Sub` (thuộc `TP-IMP-0040`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0501`, `TP-FR-0502`, `TP-FR-0503`, `TP-NFR-0404`, `SLO-07`, `TP-BR-0101`..`0104`, `SM-05`, `AUTH-02`..`AUTH-07`, `AUTH-11`, `AUTH-13`, `AUTH-15`.
- **Dependencies:** `TP-IMP-0007`, `TP-IMP-0008`.
- **Design Refs:** [`ADR-0006`](../../architecture/adr/0006-immutable-config-snapshots-and-fast-revocation.md), [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) Mục 2 & 3, [`business-entity-map.md`](../../requirements/business-entity-map.md) (`SM-05`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`AUTH-02`..`15`).

## 4. API, Wire & Data Contracts
- **Định dạng `Virtual Key` Chuẩn (`CR-06`):** `vk_tp_<env>_<32_base62_chars>`; lưu `active_key_hash = SHA-256(rawKey)` (`32 bytes`) và khi xoay vòng lưu khóa cũ vào `previous_key_hash` kèm `grace_expires_at_ms`; chỉ lưu `key_prefix` + `last_four` để hiển thị kiểm toán. So sánh mã băm bằng `subtle.ConstantTimeCompare`.
- **Hợp đồng `ConfigSnapshot`:** Cấu trúc bất biến chỉ đọc chứa bản đồ tra cứu `O(1)` từ `sha256_hash -> VirtualKeyView` và `(org_id, project_id, env_id) -> EffectiveConfig`; `0` khóa `Mutex` trên đường đọc.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`AUTH-02`):** Given máy chủ khởi tạo trống, When dùng `Bootstrap Token` tạo `ORG_OWNER` đầu tiên, Then lần gọi thứ hai với cùng `Bootstrap Token` bị từ chối cứng `410 bootstrap_already_completed`.
- **`AC-2` (`AUTH-05`):** Given `Virtual Key K1` đang `CREATED_ACTIVE`, When xoay vòng với `grace_seconds = 300`, Then trạng thái chuyển sang `ROTATING_DUAL_VALID` giúp cả `K1` (`previous_key_hash`) và khóa kế nhiệm `K2` (`active_key_hash`) đều hợp lệ trong 300 giây; khi `FakeClock.Advance(301s)`, `K1` tự động bị từ chối `401 virtual_key_expired`.
- **`AC-3` (`AUTH-06` & `SLO-07`):** Given một luồng SSE đang mở bằng `Virtual Key K1` (kể cả khóa đang ở `QUOTA_PAUSED`), When gọi `RevokeVirtualKey(K1, emergency=true)`, Then trạng thái chuyển dứt khoát sang `REVOKED`, `ConfigSnapshot` cập nhật qua `config_changelog` trong `< 1 giây`, luồng SSE bị hủy ngay và mọi yêu cầu mới nhận `401 virtual_key_revoked`.
- **`NEG-1` (`AUTH-04` & `AUTH-07` Scope / Plane Crossover):** Dùng khóa `vk_tp_staging_...` gọi tài nguyên môi trường `prod` → `403 environment_scope_denied`; dùng `vk_tp_live_...` gọi `/admin/v1/*` hoặc dùng Cookie quản trị gọi `/v1/chat/completions` → `401 invalid_credential_plane`.
- **`NEG-2` (`AUTH-11`, `AUTH-13` & `AUTH-15`):** Vai trò `DEVELOPER` hoặc `FINOPS_VIEWER` gọi hành động duyệt quyền Plugin → `403 insufficient_role_permission`; gửi thân JSON cài `"org_id": "<Org_B>"` từ phiên của `Org A` bị chặn/ghi đè theo phiên xác thực (`AUTH-13`); truy vấn ID của `Org B` trả đồng nhất `404 resource_not_found` (`AUTH-15`).
- **`NEG-3` (`AUTH-03` CSRF):** Gửi yêu cầu ghi bằng Cookie phiên trình duyệt mà thiếu hoặc sai `X-CSRF-Token` → từ chối `403 csrf_validation_failed`.

## 6. Test Commands & Required Evidence
- Kiểm tra thuần Go (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/iam/... ./internal/config/snapshot/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/iam/... ./internal/config/snapshot/...`
- Kiểm tra tranh chấp dữ liệu (`-race`) & Benchmark `0 allocs/op`:
  - `go test -v -race ./internal/iam/... ./internal/config/snapshot/...`
  - `go test -bench=BenchmarkSnapshotLookup -benchmem ./internal/config/snapshot/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Không bao giờ ghi `rawKey`, mật khẩu hay mã phiên vào DB, nhật ký hoặc span OTel; mọi điểm cuối quản trị mặc định `Deny` nếu chưa khai báo tường minh hằng số `Action`.
- **Rollback:** Bảng cộng thêm `0003_iam_keys.sql`; có thể vô hiệu hóa toàn bộ khóa qua cờ khẩn cấp mà không mất dữ liệu.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (khóa 6 vai trò & vá `SM-05`), `TP-IMP-0007` và `TP-IMP-0008` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca `AUTH-02..07, 11, 13, 15` dưới cả `CGO_ENABLED=0` và `-race`, `0 allocs/op` khi tra cứu `ConfigSnapshot`, vượt qua `REQUIRES_STRONG_REVIEW`.
