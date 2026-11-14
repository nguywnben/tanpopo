# `TP-IMP-0011` — Trình Giải nén Gói An toàn, Xác minh Xuất xứ/Chữ ký, Cổng Duyệt Quyền `SM-01`, Chặn Tăng Quyền & `tanpopo.lock`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0011` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0007`, `TP-IMP-0009` — `GAP-01, 04, 08, 09`)* |
| **Quy mô (`Size`)** | `L` (`internal/extension/installer/*`, `internal/extension/lockfile/*`, `db/migrations/sqlite/0004_extensions.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Chuỗi cung ứng phần mở rộng `TB-5`, chống Zip-Slip/Zip-Bomb & chống leo thang đặc quyền)* |
| **Chủ sở hữu Kỹ thuật** | `Platform Supply-Chain & Security Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Xây dựng đường ống cài đặt, nâng cấp, quay lui và cách ly (`Quarantine`) gói phần mở rộng `.tppkg` (từ kho cục bộ, URL ghim băm, nhánh Git ghim cứng mã băm commit `40-hex SHA`) mà **không cần biên dịch lại Tanpopo Core**, thực thi đầy đủ máy trạng thái vòng đời `SM-01`, phân tách rạch ròi 3 chiều tin cậy (`Package Origin ≠ Publisher Trust ≠ Runtime Safety`), chặn đứng leo thang quyền ngầm $\Delta P \neq \emptyset$ khi nâng cấp và ghi khóa bất biến vào `tanpopo.lock`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/extension/installer/archive_guard.go` (giải nén an toàn giới hạn cứng tổng dung lượng giải nén `<= 64 MiB`, số tệp `<= 512`, tỷ lệ nén `<= 50:1`, từ chối mọi đường dẫn `..`, đường dẫn tuyệt đối, liên kết tượng trưng `symlink`/`hardlink`, thiết bị đặc biệt; xóa thư mục cài đặt có kiểm tra `filepath.EvalSymlinks` nằm gọn trong `plugins_dir`).
  - `internal/extension/installer/manifest_validator.go` (kiểm định `tanpopo-plugin.yaml` theo JSON Schema nghiêm ngặt, kiểm tra dải tương thích `core_api_version`, cấm mạo danh không gian tên dành riêng `@tanpopo/*` hoặc `official/*` nếu thiếu chữ ký khóa gốc dự án).
  - `internal/extension/installer/provenance.go` (xác minh mã băm `SHA-256` từng tệp + chữ ký `Ed25519 / Cosign` ngoại tuyến; gắn nhãn `official`, `verified`, hoặc `unverified-external`).
  - `internal/extension/installer/lifecycle.go` (quản lý `SM-01`: `STAGED -> PERMISSION_REVIEW_REQUIRED -> ENABLED -> DISABLED / QUARANTINED / ROLLED_BACK`, tính toán tập chênh lệch quyền $\Delta P = P_{\text{new}} \setminus P_{\text{approved}}$, giữ nguyên tối thiểu 1 bản tốt liền trước trên đĩa để quay lui `< 5 giây`).
  - `internal/extension/lockfile/lockfile.go` (đọc/ghi nguyên tử `tanpopo.lock` ghi tên gói, phiên bản, nguồn, `sha256` gói và danh sách quyền đã duyệt).
  - `db/migrations/sqlite/0004_extensions.sql` (`extension_packages`, `extension_installations`, `extension_permission_grants`, `extension_revocations`).
- **Out-of-Scope:** Đồng bộ chỉ mục Marketplace từ xa & kéo từ `Private OCI Registry` (thuộc `TP-IMP-0035`); xuất/nhập gói tổng hợp `Air-Gap Bundle` (thuộc `TP-IMP-0045`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0304`, `TP-FR-0306`, `TP-FR-0703`, `TP-FR-1002`, `TP-FR-1004`, `TP-NFR-0403`, `TP-NFR-0902`, `SM-01`, `CT-01`..`CT-05`, `CT-18`, `CT-24`, `CT-25`, `FZ-02`.
- **Dependencies:** `TP-IMP-0007`, `TP-IMP-0009`.
- **Design Refs:** [`marketplace-lifecycle.md`](../../specifications/marketplace-lifecycle.md), [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Mục 1, 2 & 5, [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-01`..`05`, `CT-18`, `CT-24`, `CT-25`, `FZ-02`).

## 4. API, Wire & Data Contracts
- **Phép toán Cổng Duyệt Quyền & Chống Leo thang ($\Delta P$):**
  - Cài mới bất kỳ gói cộng đồng/ngoài nào đòi quyền mạng `allowed_hosts` hoặc khe `credential_slots` → luôn dừng ở `PERMISSION_REVIEW_REQUIRED`, không bao giờ tự chạy ngay sau lệnh tải gói (`UX-GR-04`).
  - Nâng cấp `v1.0.0 -> v1.1.0`: nếu $\Delta P \neq \emptyset$ (ví dụ thêm miền mới hoặc đổi từ `Tier-1 Declarative` sang `Tier-2 Wasm`), hệ thống giữ nguyên `v1.0.0` phục vụ lưu lượng và đặt `v1.1.0` vào `PERMISSION_REVIEW_REQUIRED` cho đến khi `org_admin` / `security_reviewer` ký duyệt tường minh (`CT-18`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given gói `.tppkg` hợp lệ từ thư mục cục bộ hoặc Git SHA 40 ký tự, When cài đặt và quản trị viên duyệt tập quyền `P`, Then trạng thái chuyển `STAGED -> PERMISSION_REVIEW_REQUIRED -> ENABLED` và tệp `tanpopo.lock` ghi chính xác `sha256` cùng danh sách quyền `P`.
- **`AC-2`:** Given gói `v1.1.0` đang `ENABLED` bị phát hiện lỗi hoặc nằm trong danh sách thu hồi, When gọi `RollbackExtension`, Then lưu lượng chuyển ngược về gói `v1.0.0` đã lưu sẵn trong `< 5 giây` mà không cần tải lại từ mạng.
- **`NEG-1` (`CT-01..03` Zip-Slip / Symlink / Zip-Bomb):** Gói nén chứa mục `../../etc/passwd`, `symlink` trỏ ra ngoài hoặc luồng nén `10 KiB -> 200 MiB` → bộ giải nén hủy bỏ ngay lập tức, `0` tệp nào lọt ra ngoài thư mục tạm cô lập.
- **`NEG-2` (`CT-04, 05` Tampered SHA / Namespace Spoofing):** Gói bị sửa 1 byte sau khi ký hoặc gói cộng đồng tự xưng `@tanpopo/openai` không có chữ ký khóa gốc → từ chối cứng `422 package_integrity_or_namespace_violation`.
- **`NEG-3` (`CT-24` Mutable Git Ref Rejection):** Lệnh cài từ Git truyền nhánh động `main` hoặc `latest` mà không ghim commit `40-hex SHA` (khi chính sách bảo mật bật chuẩn mặc định) → từ chối `422 unpinned_git_reference`.
- **`NEG-4` (`CT-18` Silent Permission Escalation):** Bản cập nhật âm thầm thêm `telemetry-collector.evil.test` vào `allowed_hosts` → bị chặn kích hoạt tự động `100%`.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/extension/installer/... ./internal/extension/lockfile/...`
- Fuzzing bộ đọc gói & manifest (khói 30 giây): `go test -fuzz=FuzzArchiveAndManifest -fuzztime=30s ./internal/extension/installer/...` (`FZ-02`).

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Tuyệt đối không thực thi bất kỳ hook kịch bản cài đặt nào (`postinstall`, shell script, Makefile) khi giải nén hoặc cài gói `.tppkg`.
- **Rollback:** Quay lui phiên bản gói nguyên tử bằng con trỏ phiên bản trong `ConfigSnapshot` + bảng `extension_installations`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (vá `SM-01`), `TP-IMP-0003` (DDL bảng `extension_revocations`), `TP-IMP-0007` và `TP-IMP-0009` đã hoàn tất.
- **DoD:** Xanh toàn bộ `CT-01..05, 18, 24, 25` + `FZ-02`, hoàn tất `REQUIRES_STRONG_REVIEW`.
