# `TP-IMP-0007` — Giao diện `storage/port`, Trình điều khiển `SQLite WAL` (`modernc.org/sqlite`), Bộ Chạy Migration *Expand-Only* & Bảng Đa Tổ chức Gốc

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0007` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0003, 0005` — `GAP-01, 04`)* |
| **Quy mô (`Size`)** | `M` (`go.mod`, `internal/pkg/idgen`, `internal/storage/*`, `db/migrations/sqlite/0001_foundation.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Thiết lập bất biến cô lập đa tổ chức & cơ chế di trú lược đồ)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Backend / Storage Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa giao diện lưu trữ trừu tượng `internal/storage/port` và trình điều khiển nhúng thuần Go `SQLite WAL` sử dụng **`modernc.org/sqlite` (`CGO_ENABLED=0`)** cùng bộ chạy migration kiểm tra mã băm an toàn theo [`data-model.md`](../../architecture/data-model.md) và [`ADR-0003`](../../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md), khởi tạo lược đồ nền tảng (`db/migrations/sqlite/0001_foundation.sql`) cho `organizations`, `projects`, `environments`, `config_changelog` và `distributed_leases` kèm bộ sinh định danh có thứ tự thời gian `UUIDv7` mã hóa Crockford Base32 (`<prefix>_<26_chars>`, tổng `<= 32` ký tự vừa khít `VARCHAR(40)`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (thêm phụ thuộc thuần Go được duyệt `modernc.org/sqlite`).
  - `internal/pkg/idgen/` (sinh `UUIDv7` 128-bit từ `crypto/rand` + `epoch_ms`, mã hóa 26 ký tự chữ thường Crockford Base32 kèm bảng tiền tố chuẩn toàn hệ thống: `org_`, `prj_`, `env_`, `prc_`, `vky_`, `sec_`, `ext_`, `exv_`, `ins_`, `adp_`, `con_`, `lmd_`, `dpl_`, `pol_`, `rte_`, `bdg_`, `rsv_`, `set_`, `trc_`, `aud_`, `ksw_`, `mcp_`, `apr_`, `whk_`, `agt_`, `tsk_`).
  - `internal/storage/port/` (giao diện `Tx`, `TenancyRepository`, `ChangelogRepository`, `AuditSink`, mã lỗi chuẩn `ErrNotFound`, `ErrVersionConflict`, `ErrResourceInUse`).
  - `internal/storage/migrate/` (trình chạy tệp SQL tuần tự có bảng `schema_migrations`, kiểm tra checksum SHA-256 và chặn cờ `dirty` với mã thoát `31` theo `FMEA-06`).
  - `internal/storage/sqlite/` (kết nối `modernc.org/sqlite` với `_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)`, kiểm tra từ chối ổ mạng NFS/SMB, nguyên hàm `VACUUM INTO` cho sao lưu nóng) & `db/migrations/sqlite/0001_foundation.sql`.
- **Out-of-Scope:** Trình điều khiển `PostgreSQL 16+` (thuộc `TP-IMP-0039`); bảng kho khóa bí mật (thuộc `TP-IMP-0008`); bảng sổ cái (thuộc `TP-IMP-0021`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0501`, `TP-FR-0901` (cột `version`), `TP-FR-1001`, `TP-NFR-0103`, `SLO-05`, `TP-BR-0101`, `RSK-013`, `FMEA-06`.
- **Dependencies:** `TP-IMP-0003`, `TP-IMP-0005`.
- **Design Refs:** [`data-model.md`](../../architecture/data-model.md) Mục 1 & 3.1, [`ADR-0003`](../../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-06`).

## 4. API, Wire & Data Contracts
- **Quy tắc Khả chuyển Cứng:** Thời gian lưu bằng `BIGINT epoch_ms` UTC; khóa chính/ngoại tổ hợp `(org_id, id)` trên `projects` và `environments`; mọi hàm cập nhật nhận `expectedVersion int64` và thực thi `WHERE org_id = ? AND id = ? AND version = ?` (nếu `rows == 0` → trả `port.ErrVersionConflict` tương ứng `409`).
- **Tăng `config_epoch` Nguyên tử Qua Duy nhất Bảng `config_changelog`:** Mọi giao dịch thay đổi cấu hình ghi kèm 1 dòng vào `config_changelog` để thông báo cho `ConfigSnapshot` (duy nhất một bảng nhật ký cấu hình toàn hệ thống).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given thư mục dữ liệu trống, When khởi tạo `sqlite.Open` với `CGO_ENABLED=0`, Then bật thành công `journal_mode=wal`, `foreign_keys=1`, áp dụng `0001_foundation.sql` dưới `100 ms` và tăng `config_epoch` trong `config_changelog` khi tạo `Org -> Project -> Env`.
- **`AC-2`:** Given 10.000 mã sinh từ `idgen.New("con")`, When kiểm tra độ dài và thứ tự từ điển, Then mọi chuỗi dài đúng `30` ký tự (`<= 40`), duy nhất 100% và sắp xếp tăng dần theo thời gian tạo.
- **`AC-3`:** Given bản ghi `Project` ở `version = 1`, When hai goroutines cùng gọi `UpdateProject(..., expectedVersion=1)`, Then đúng 1 luồng thành công lên `version = 2` và luồng còn lại nhận `port.ErrVersionConflict`; truy vấn `GetProject(ctx, orgA, projectIDOfOrgB)` trả về `port.ErrNotFound` đồng nhất (`0` rò rỉ chéo tổ chức).
- **`NEG-1`:** Cố tình tạo `Environment` gắn `org_id = Org_A` nhưng `project_id` thuộc `Org_B` → khóa ngoại tổ hợp `(org_id, id)` của SQLite chặn cứng lỗi ràng buộc ngay ở tầng DB.
- **`NEG-2`:** Sửa đổi 1 ký tự trong tệp migration đã chạy hoặc đánh dấu `dirty = true` → trình khởi động từ chối mở cổng với lỗi tương ứng `exit 31` (`FMEA-06`).
- **`NEG-3`:** Không dùng bất kỳ kiểu `FLOAT`/`REAL` hay hàm SQL phụ thuộc riêng một hệ quản trị ngoài gói `internal/storage/sqlite`.

## 6. Test Commands & Required Evidence
- Kiểm tra thuần Go (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/pkg/idgen/... ./internal/storage/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/pkg/idgen/... ./internal/storage/...`
- Kiểm tra tranh chấp dữ liệu (`-race`, `CGO_ENABLED=1`): `go test -v -race ./internal/pkg/idgen/... ./internal/storage/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Bắt buộc truyền `org_id` làm tham số đầu tiên trên mọi phương thức của `TenancyRepository` (không bao giờ có hàm `GetProjectByIDOnly(id)`).
- **Rollback:** Mọi migration tuân thủ quy tắc *Expand-Only*; đi kèm kiểm thử mở lại tệp `.db` sau khi đóng đột ngột.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` và `TP-IMP-0005` đã hoàn tất.
- **DoD:** Đạt `>= 85%` độ phủ kiểm thử trên `internal/storage/...` (cả `CGO_ENABLED=0` và `-race`); vượt qua rà soát `REQUIRES_STRONG_REVIEW`.
