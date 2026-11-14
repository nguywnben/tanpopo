# `TP-IMP-0039` — Trình điều khiển `PostgreSQL 16+` (Phân vùng Tháng, Khóa `(org_id, id)`, `FORCE RLS`), Kiểm thử Kép `Dual-DB` & Di trú `SQLite -> Postgres`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0039` |
| **Milestone / Epic** | `M4` (`v0.3 Clustered HA, Enterprise Security & A2A`) · `EP-08` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0007`, `TP-IMP-0021`, `TP-IMP-0023` — `GAP-01, 04, 05`)* |
| **Quy mô (`Size`)** | `L` (`go.mod`, `internal/storage/postgres/*`, `db/migrations/postgres/*`, `internal/ops/migrate/sqlite_to_pg.go`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Cô lập đa người thuê cấp CSDL `FORCE ROW LEVEL SECURITY`, nối chuỗi kiểm toán đồng thời trên HA & di trú dữ liệu không thất thoát `CT-32`)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Database & Storage Architect` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Mở rộng tầng lưu trữ `storage/port` sang **`PostgreSQL 16+`** (`pgx/v5`) phục vụ chế độ cụm chịu tải cao (`TP-FR-1005`, `ADR-0003`, `ARCH-DATA-01`, `CR-06`):
1. **Cô lập Cứng Ở Tầng Động cơ CSDL (`ENABLE + FORCE ROW LEVEL SECURITY`):** Tài khoản ứng dụng chạy dưới vai trò không có quyền `BYPASSRLS`; mọi giao dịch kết nối bắt buộc đặt biến phiên cục bộ `SET LOCAL app.current_org_id = '<uuid>'` (nếu quên đặt biến này, chính sách RLS từ chối trả về `0` dòng thay vì làm lộ toàn bộ bảng!).
2. **Phân vùng Theo Tháng Tự động & Khóa Tổ hợp `(org_id, id)`:** Phân vùng `RANGE (created_at)` theo tháng cho 3 bảng lưu lượng lớn (`usage_settlements`, `request_traces_meta`, `audit_events`) kèm trình tạo phân vùng trước 3 tháng và khóa tuần tự theo `chain_key` (`org_id` hoặc `"SYSTEM"`, dùng `pg_advisory_xact_lock`) khi nối chuỗi băm `audit_events` từ nhiều nút đồng thời (`CR-06`).
3. **Bộ Kiểm thử Đối xứng Kép (`Dual-DB Contract Suite`) & Công cụ Di trú Chuẩn `tanpopo db migrate-engine` (`SQLite -> PostgreSQL` — `CT-32`, `DR-02`):** Chạy cùng một bộ bài kiểm thử hợp đồng lưu trữ trên cả `SQLite WAL` lẫn `PostgreSQL 16+` để chứng minh tương đương hành vi 100%, và cung cấp lệnh chuẩn `tanpopo db migrate-engine` có kiểm chứng tổng kiểm (`row counts`, `sum(settled_micro_usd)`, `VerifyAuditChain`, giải mã thử `vault`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (thêm trình điều khiển thuần Go `github.com/jackc/pgx/v5`).
  - `internal/storage/postgres/driver.go` (hồ kết nối `pgxpool`, hook bắt buộc `SET LOCAL app.current_org_id`, trình chạy migration có `pg_advisory_lock` toàn cụm chống đua khi khởi động 10 pod cùng lúc — `FMEA-06`).
  - `db/migrations/postgres/0001_full_schema_rls_partitions.sql` (đầy đủ `26` bảng chuẩn đã khóa ở `TP-IMP-0003`, `FORCE RLS`, khóa ngoại tổ hợp `(org_id, ...)` và trigger cấm `UPDATE/DELETE` trên `usage_settlements` & `audit_events`).
  - `internal/storage/postgres/partition_manager.go` (tự động tạo trước phân vùng tháng kế tiếp và tách phân vùng cũ phục vụ lưu trữ).
  - `internal/ops/migrate/sqlite_to_pg.go` (`tanpopo db migrate-engine --from sqlite://... --to postgres://... --verify`).
- **Out-of-Scope:** Đồng bộ bộ đếm tốc độ và thu hồi khóa qua `Valkey` (thuộc đơn vị kế tiếp `TP-IMP-0040`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-1005`, `ADR-0003`, `ARCH-DATA-01`, `CR-06`, `CT-32`, `AUTH-14`, `FMEA-06`, `DR-02`, `GATE-REL-02`.
- **Dependencies:** `TP-IMP-0007`, `TP-IMP-0021`, `TP-IMP-0023`.
- **Design Refs:** [`ADR-0003`](../../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md), [`data-model.md`](../../architecture/data-model.md) (`ARCH-DATA-01`), [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) Mục 2, [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-06`).

## 4. API, Wire & Data Contracts
- **Bất biến Chống Đứt Chuỗi Băm Khi Ghi Từ Nhiều Nút (`HA Per-Org Audit Serialization`):** Khi 4 nút Control Plane cùng ghi `audit_events` cho `Org A`, giao dịch lấy khóa `pg_advisory_xact_lock(hashtextextended('audit:' || org_id::text, 0))` trong vài mili-giây để đảm bảo `seq_no` và `prev_hash` của `Org A` tuyến tính tuyệt đối mà không làm nghẽn `Org B`.
- **Kiểm chứng Di trú Không Mất Mát (`CT-32`):** Nếu bất kỳ bảng nào lệch dù chỉ 1 dòng, lệch `1 micro-USD` tổng sổ cái, hoặc đứt chuỗi băm kiểm toán sau khi chép từ SQLite sang PostgreSQL, lệnh `tanpopo db migrate-engine` hủy bỏ giao dịch đích và trả mã thoát lỗi `22`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`Dual-DB Behavioral Parity`):** Toàn bộ bộ kiểm thử `storage/port`, `finops/ledger` (`CT-29`) và `security/audit` (`CT-30` dưới 8 nút ghi song song) chạy xanh `100%` trên cả `SQLite WAL` lẫn `PostgreSQL 16`.
- **`AC-2` (`CT-32` Lossless SQLite-to-PostgreSQL Migration):** Nạp dữ liệu hỗn hợp đa dự án, khóa mật `AEAD AAD`, 1.000 bút toán `usage_settlements` và 500 sự kiện kiểm toán trên SQLite → chạy `tanpopo db migrate-engine --verify` sang PostgreSQL → tổng số dòng, tổng chi phí `micro-USD`, giải mã `SecretCredential` và `VerifyAuditChain` khớp tuyệt đối `100%`.
- **`NEG-1` (`RLS Enforcement Proof`):** Trong bài kiểm thử đối kháng, thực thi trực tiếp câu lệnh `SELECT * FROM secret_credentials` bằng kết nối ứng dụng khi chưa đặt `app.current_org_id` hoặc đặt `Org A` để tìm dòng của `Org B` → PostgreSQL RLS trả về **đúng `0` dòng**.
- **`NEG-2` (`FMEA-06` Concurrent Pod Migration):** Khởi chạy đồng thời 8 tiến trình chạy migration trên cùng cơ sở dữ liệu trống → đúng 1 tiến trình thực thi DDL, 7 tiến trình chờ khóa êm ái và không xảy ra lỗi tranh chấp bảng.

## 6. Test Commands & Required Evidence
- `TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/storage/postgres/... ./internal/ops/migrate/...` *(chạy với PostgreSQL 16 cục bộ trong container cô lập mạng)*.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Từ chối khởi động ở chế độ sản xuất nếu tài khoản PostgreSQL được cấp quyền `SUPERUSER` hoặc `BYPASSRLS` (`tanpopo doctor` kiểm tra trực tiếp `pg_roles`).
- **Rollback:** Tệp SQLite gốc được mở ở chế độ chỉ đọc khi di trú sang PostgreSQL nên luôn giữ nguyên vẹn 100% để quay lui tức thì nếu cần.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` (hoàn thiện DDL 8 bảng thiếu + khóa phân vùng `CR-06`), `TP-IMP-0007`, `TP-IMP-0021` và `TP-IMP-0023` đã hoàn tất.
- **DoD:** Xanh toàn bộ kiểm thử kép `Dual-DB`, `CT-32`, `FMEA-06`, kiểm chứng `FORCE RLS`, vượt qua `REQUIRES_STRONG_REVIEW`.
