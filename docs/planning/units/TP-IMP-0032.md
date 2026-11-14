# `TP-IMP-0032` — Vòng đời Tự Vận hành Đơn nút: `/healthz` vs `/readyz`, `tanpopo doctor`, GitOps Khai báo, Sao lưu Nóng, `Crypto-Shredding < 60s` & `Upgrade Preflight`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0032` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-06` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0008`, `TP-IMP-0011`, `TP-IMP-0031` — `GAP-01, 05`)* |
| **Quy mô (`Size`)** | `L` (`internal/ops/health/*`, `doctor/*`, `gitops/*`, `backup/*`, `erasure/*`, `upgrade/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Sao lưu nóng tách rời `Master KEK`, hủy khóa mật mã học `< 60s` & an toàn nâng cấp `SM-08`)* |
| **Chủ sở hữu Kỹ thuật** | `Principal SRE / Storage Reliability Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa trọn vẹn bộ công cụ vận hành sản xuất cấp SRE cho chế độ tự vận hành đơn nút (`TP-FR-1001`..`1004`, `SPEC-OPS-01`..`03`):
1. **Phân tách Sống/Sẵn sàng (`/healthz` vs `/readyz`) & Xả luồng Êm (`Graceful Drain`):** `/healthz` kiểm tra tiến trình còn sống; `/readyz` kiểm tra CSDL, `Master KEK` và `ConfigSnapshot` sẵn sàng nhận tải — khi nhận `SIGTERM`, `/readyz` chuyển ngay sang `503 draining` trong khi chờ các luồng SSE đang mở kết thúc êm trong hạn `drain_timeout` (`OPS-AC-02`).
2. **8 Phép Tiền kiểm `tanpopo doctor` & Chế độ Khai báo `GitOps`:** Kiểm tra quyền tệp khóa (`0400/0600`), dung lượng đĩa, chế độ `SQLite WAL`, đồng hồ hệ thống, cổng mạng và nạp cấu hình khai báo `tanpopo.yaml` + `tanpopo.lock` bất biến (`OPS-AC-01`).
3. **Sao lưu Nóng `.tpop.enc` & Phục hồi Kiểm chứng (`tanpopo backup create / verify / restore`):** Sao lưu trực tuyến nhất quán (`VACUUM INTO` cho SQLite xuất tệp chuẩn **`.tpop.enc`**) **tuyệt đối không đính kèm `Master KEK` vào cùng tệp sao lưu** (`SPEC-OPS-02`), kèm kiểm tra mã băm và chuỗi kiểm toán sau khi khôi phục (`SLO-08`, `DR-01`, `OPS-AC-07`).
4. **Xóa sổ Mật mã học Cấp Dự án / Tổ chức Dưới `< 60 giây` (`Crypto-Shredding` — `DR-03`, `OPS-AC-09`):** Hủy vĩnh viễn `Project DEK` hoặc `Org DEK` (`TP-IMP-0008`), xóa sạch vùng đệm RAM và xóa bản ghi liên quan.
5. **Tiền kiểm Nâng cấp (`tanpopo upgrade preflight` — `SM-08`, `CT-24`, `DR-04`, `OPS-AC-05`) & Hoàn thiện Trọn vẹn 10/10 Bước `EXT-ACCEPT-01`:** Mở rộng `tests/integration/ext_accept_01_test.go` (từ lát cắt `M1`) lên trọn vẹn **10 bước bắt buộc** chạy trên tiến trình `tanpopo serve` thật với `PID` và `sha256sum(tanpopo)` bất biến (bao gồm định tuyến cư trú `EU`, sổ cái `\$1.00` `SETTLED_EXACT`/`ESTIMATED`, chặn tăng quyền `UPDATING_PENDING_RECONSENT`, `Rollback`, `Quarantine < 1s` và truy vấn `Blast-Radius`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/ops/health/probes.go` & `internal/ops/lifecycle/drain.go`.
  - `internal/ops/doctor/checks.go` (8 phép kiểm tra môi trường).
  - `internal/ops/gitops/reconciler.go` (áp dụng nguyên tử cấu hình khai báo dùng tham chiếu `${ENV:SECRET_NAME}`).
  - `internal/ops/backup/sqlite_backup.go` (`create`, `verify`, `restore` cho tệp `.tpop.enc`).
  - `internal/ops/erasure/shredder.go` (`tanpopo tenant shred --project|--org` hoàn tất dưới `< 60 giây`).
  - `internal/ops/upgrade/preflight.go` (`SM-08`: đánh giá ma trận `coreVersionRange` `N-1` của mọi gói trong `tanpopo.lock` — `CT-24`).
  - `tests/integration/ext_accept_01_test.go` (hoàn chỉnh đủ 10/10 bước `EXT-ACCEPT-01`).
- **Out-of-Scope:** Sao lưu `pg_dump` / PITR cho cụm PostgreSQL và di trú `SQLite -> Postgres` (thuộc `TP-IMP-0039`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-1001`, `TP-FR-1002`, `TP-FR-1003`, `TP-FR-1004`, `TP-NFR-0501`, `TP-NFR-0502`, `TP-NFR-0601`, `SLO-08`, `SM-08`, `CT-24`, `EXT-ACCEPT-01`, `DR-01`, `DR-03`, `DR-04`, `OPS-AC-01`, `OPS-AC-02`, `OPS-AC-05`, `OPS-AC-07`, `OPS-AC-09`.
- **Dependencies:** `TP-IMP-0008`, `TP-IMP-0011`, `TP-IMP-0031`.
- **Design Refs:** [`installer-upgrades.md`](../../operations/installer-upgrades.md) (`SPEC-OPS-01`), [`backups-recovery.md`](../../operations/backups-recovery.md) (`SPEC-OPS-02`), [`monitoring-incidents.md`](../../operations/monitoring-incidents.md) (`SPEC-OPS-03`).

## 4. API, Wire & Data Contracts
- **Bất biến Tách Khóa Khỏi Bản Sao lưu (`KEK Separation Invariant`):** Kho lưu trữ `.tpop.enc` chứa ảnh chụp SQLite (bản mã), `tanpopo.lock` và tệp manifest kiểm tra `SHA-256`; nếu phát hiện biến môi trường hoặc tệp `Master KEK` nằm trong danh sách đóng gói → hủy thao tác sao lưu ngay lập tức.
- **Bảo toàn Sổ cái & Kiểm toán Khi Hủy `Project`:** Khi thực thi `Crypto-Shredding` một `Project`, các khóa mật và thân gỡ lỗi của `Project` đó bị vô hiệu hóa mật mã học vĩnh viễn trong `< 60 giây`, trong khi chuỗi băm `audit_events` cấp `Org` vẫn liền mạch không đứt gãy (`CR-06`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`OPS-AC-07` & `DR-01` Online Backup `.tpop.enc` & Clean Restore Under Load):** Trong khi 20 goroutines đang liên tục ghi sổ cái và kiểm toán, chạy `tanpopo backup create` → khôi phục sang thư mục mới bằng `tanpopo backup restore` cùng `Master KEK` gốc → `PRAGMA integrity_check = ok` và `VerifyAuditChain` đạt `100%` hợp lệ.
- **`AC-2` (`OPS-AC-09` & `DR-03` `< 60s` Crypto-Shredding Proof):** Tạo bản sao lưu `B1.tpop.enc`, sau đó chạy lệnh xóa mật mã học cho `Project P1` → thao tác hoàn tất trong `< 60 giây`, xóa cache RAM tức thì, và dữ liệu mật của `P1` trong `B1.tpop.enc` vĩnh viễn không giải mã được nữa, còn `Project P2` vẫn giải mã bình thường.
- **`AC-3` (`EXT-ACCEPT-01` Full 10-Step Suite & `OPS-AC-02` Graceful Drain):** Vượt qua trọn 10/10 bước của `EXT-ACCEPT-01` với `PID` và `sha256sum(tanpopo)` giữ nguyên không đổi; khi nhận `SIGTERM`, `/readyz` trả `503` ngay lập tức và luồng SSE đang mở truyền nốt tới `[DONE]`.
- **`NEG-1` (`OPS-AC-05` & `CT-24` Incompatible `N-2` Plugin Blocks Upgrade):** Giả lập 1 gói mở rộng `N-2` ngoài dải `coreVersionRange` khi chạy `tanpopo upgrade preflight` → lệnh trả mã thoát lỗi tường minh chặn nâng cấp (`BLOCKED_INCOMPATIBLE`).
- **`NEG-2` (`Insecure KEK File Permissions`):** Đặt quyền tệp khóa `0644` → `tanpopo doctor` và trình khởi động sản xuất từ chối chạy.

## 6. Test Commands & Required Evidence
- Thuần Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/ops/... ./tests/integration/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/ops/... ./tests/integration/...`)*
- Tranh chấp dữ liệu (`-race`): `go test -v -race ./internal/ops/... ./tests/integration/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Tệp cấu hình `GitOps` từ chối chuỗi khóa API trần (bắt buộc dùng tham chiếu `${ENV:...}` hoặc kho khóa mật) để ngăn quản trị viên vô tình đẩy khóa rõ lên Git.
- **Rollback:** Quy trình khôi phục từ bản `.tpop.enc` và quay lui nhị phân `< 60s` (`DR-04`) được kiểm chứng tự động ngay trong đơn vị này.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (vá `SM-08`), `TP-IMP-0003` (`Project DEK`), `TP-IMP-0008`, `TP-IMP-0011` và `TP-IMP-0031` đã hoàn tất.
- **DoD:** Xanh toàn bộ `OPS-AC-01, 02, 05, 07, 09` dưới `-race`, vượt qua `REQUIRES_STRONG_REVIEW`.
