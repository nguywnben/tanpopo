# `TP-IMP-0007` — Giao diện `storage/port`, Trình điều khiển `SQLite WAL`, Bộ Chạy Migration *Expand-Only* & Bảng Đa Tổ chức Gốc

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0007` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0003, 0005` — `GAP-01, 04`)* |
| **Quy mô (`Size`)** | `M` (`internal/storage/port`, `internal/storage/sqlite`, `db/migrations/sqlite/0001_init.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Thiết lập bất biến cô lập đa tổ chức & cơ chế di trú lược đồ)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Backend / Storage Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa giao diện lưu trữ trừu tượng `internal/storage/port` và trình điều khiển nhúng thuần Go `SQLite WAL` (`CGO_ENABLED=0`) cùng bộ chạy migration kiểm tra mã băm an toàn theo [`data-model.md`](../../architecture/data-model.md) và [`ADR-0003`](../../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md), khởi tạo lược đồ cốt lõi cho `organizations`, `projects`, `environments`, `config_changelog` và `distributed_leases` kèm bộ sinh định danh có thứ tự thời gian `UUIDv7` (`org_...`, `prj_...`, `env_...`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/pkg/idgen/` (sinh `UUIDv7` có tiền tố miền).
  - `internal/storage/port/` (giao diện `Tx`, `TenancyRepository`, `ChangelogRepository`, mã lỗi chuẩn `ErrNotFound`, `ErrVersionConflict`).
  - `internal/storage/migrate/` (trình chạy tệp SQL tuần tự có bảng `schema_migrations`, kiểm tra checksum và chặn cờ `dirty` với mã thoát `31` theo `FMEA-06`).
  - `internal/storage/sqlite/` (kết nối `WAL`, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`, kiểm tra từ chối ổ mạng NFS/SMB, nguyên hàm `VACUUM INTO` cho sao lưu nóng) & `db/migrations/sqlite/0001_foundation.sql`.
- **Out-of-Scope:** Trình điều khiển `PostgreSQL 16+` (thuộc `TP-IMP-0039`); bảng kho khóa bí mật (thuộc `TP-IMP-0008`); bảng sổ cái (thuộc `TP-IMP-0021`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0501`, `TP-FR-0901` (cột `version`), `TP-FR-1001`, `TP-NFR-0103`, `TP-BR-0101`, `RSK-013`, `FMEA-06`.
- **Dependencies:** `TP-IMP-0003`, `TP-IMP-0005`.
- **Design Refs:** [`data-model.md`](../../architecture/data-model.md) Mục 1 & 3.1, [`ADR-0003`](../../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-06`).

## 4. API, Wire & Data Contracts
- **Quy tắc Khả chuyển Cứng:** Thời gian lưu bằng `BIGINT epoch_ms` UTC; khóa chính/ngoại tổ hợp `(org_id, id)` trên `projects` và `environments`; mọi hàm cập nhật nhận `expectedVersion int64` và thực thi `WHERE org_id = ? AND id = ? AND version = ?` (nếu `rows == 0` → trả `port.ErrVersionConflict` tương ứng `409`).
- **Tăng `config_epoch` Nguyên tử:** Mọi giao dịch thay đổi cấu hình ghi kèm 1 dòng vào `config_changelog` để thông báo cho `ConfigSnapshot`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given thư mục dữ liệu trống, When khởi tạo `sqlite.Open` với `CGO_ENABLED=0`, Then bật thành công `journal_mode=wal`, `foreign_keys=1`, áp dụng migration `0001` dưới `100 ms` và tăng `config_epoch` khi tạo `Org -> Project -> Env`.
- **`AC-2`:** Given bản ghi `Project` ở `version = 1`, When hai goroutines cùng gọi `UpdateProject(..., expectedVersion=1)`, Then đúng 1 luồng thành công lên `version = 2` và luồng còn lại nhận `port.ErrVersionConflict`.
- **`AC-3`:** Given dữ liệu của `Org A` và `Org B`, When truy vấn `GetProject(ctx, orgA, projectIDOfOrgB)`, Then trả về `port.ErrNotFound` đồng nhất (`0` rò rỉ chéo tổ chức).
- **`NEG-1`:** Cố tình tạo `Environment` gắn `org_id = Org_A` nhưng `project_id` thuộc `Org_B` → khóa ngoại tổ hợp `(org_id, id)` của SQLite chặn cứng lỗi ràng buộc ngay ở tầng DB.
- **`NEG-2`:** Sửa đổi 1 ký tự trong tệp migration đã chạy hoặc đánh dấu `dirty = true` → trình khởi động từ chối mở cổng với lỗi tương ứng `exit 31` (`FMEA-06`).
- **`NEG-3`:** Không dùng bất kỳ kiểu `FLOAT`/`REAL` hay hàm SQL phụ thuộc riêng một hệ quản trị ngoài gói `internal/storage/sqlite`.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/pkg/idgen/... ./internal/storage/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Bắt buộc truyền `org_id` làm tham số đầu tiên trên mọi phương thức của `TenancyRepository` (không bao giờ có hàm `GetProjectByIDOnly(id)`).
- **Rollback:** Mọi migration tuân thủ quy tắc *Expand-Only*; đi kèm kiểm thử mở lại tệp `.db` sau khi đóng đột ngột.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` và `TP-IMP-0005` đã hoàn tất.
- **DoD:** Đạt `>= 85%` độ phủ kiểm thử trên `internal/storage/...`; vượt qua rà soát `REQUIRES_STRONG_REVIEW`.
