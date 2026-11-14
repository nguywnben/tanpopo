# `TP-IMP-0011` — Trình Giải nén Gói `.tpkg` An toàn, Xác minh Xuất xứ/Chữ ký, Cổng Duyệt Quyền `SM-01`, Chặn Tăng Quyền & `tanpopo.lock`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0011` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0007`, `TP-IMP-0009` — `GAP-01, 04, 08, 09`)* |
| **Quy mô (`Size`)** | `L` (`go.mod`, `internal/extension/installer/*`, `internal/extension/lockfile/*`, `db/migrations/sqlite/0004_extensions.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Chuỗi cung ứng phần mở rộng `TB-5`, chống Zip-Slip/Archive-Bomb & chống leo thang đặc quyền)* |
| **Chủ sở hữu Kỹ thuật** | `Platform Supply-Chain & Security Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Xây dựng đường ống cài đặt, nâng cấp, quay lui và cách ly (`QUARANTINED`) gói phần mở rộng chuẩn **`.tpkg`** (`gzipped tar` tái lập được, `<= 256` tệp, `<= 64 MiB`) từ tệp cục bộ, URL ghim băm, hoặc kho Git ghim cứng mã băm commit `40-hex SHA` mà **không cần biên dịch lại Tanpopo Core**, thực thi đầy đủ máy trạng thái chuẩn `SM-01`, phân tách 3 chiều tin cậy (`Package Origin ≠ Publisher Trust ≠ Runtime Safety`), kiểm tra chính sách nguồn gốc tối thiểu của `Organization` (`org_extension_policies` — `CT-20`), chặn đứng leo thang quyền ngầm $\Delta P \neq \emptyset$ khi nâng cấp (`CT-18`) và ghi khóa bất biến vào `tanpopo.lock`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (thêm thư viện kiểm định YAML / JSON Schema thuần Go đã kiểm tra giấy phép `Apache-2.0`/`MIT`/`BSD`).
  - `internal/extension/installer/archive_guard.go` (giải nén `.tpkg` giới hạn cứng tổng dung lượng giải nén `<= 64 MiB`, số mục tệp `<= 256`, tỷ lệ nén `<= 50:1`, từ chối mọi đường dẫn `..`, đường dẫn tuyệt đối, `symlink`/`hardlink`/thiết bị đặc biệt; xóa thư mục có kiểm tra `filepath.EvalSymlinks` nằm gọn trong `plugins_dir` — `CT-04`).
  - `internal/extension/installer/manifest_validator.go` (kiểm định `manifest.yaml` theo `ExtensionManifest` chuẩn gồm `contractVersion`, `coreVersionRange`, `secretSlots`, `allowed_hosts`; từ chối `contractVersion` không hỗ trợ như `"9.0"` — `CT-02`, kiểm tra cửa sổ tương thích `N-1` — `CT-24`, và cấm mạo danh không gian tên khớp biểu thức `^@tanpopo(-[a-z0-9]+)?/` nếu thiếu chữ ký khóa gốc dự án — `CT-05`).
  - `internal/extension/installer/provenance.go` (xác minh mã băm `SHA-256` khớp `tanpopo.lock` — `CT-03` + chữ ký `Ed25519` ngoại tuyến; gắn nhãn `official`, `verified`, `unverified-external` và kiểm tra ngưỡng cho phép trong `org_extension_policies` — `CT-20`).
  - `internal/extension/installer/lifecycle.go` (quản lý đúng các trạng thái chuẩn `SM-01`: `DISCOVERED -> VERIFYING -> PENDING_PERMISSION_APPROVAL -> INSTALLED_DISABLED | ENABLED_ACTIVE -> UPDATING_PENDING_RECONSENT | DRAINING | ROLLED_BACK | QUARANTINED`, xuất hàm `QuarantineInstallation` cho `fault_guard` ở `0013`, giữ nguyên tối thiểu 1 bản tốt liền trước để quay lui `< 5 giây`).
  - `internal/extension/lockfile/lockfile.go` (đọc/ghi nguyên tử `tanpopo.lock`).
  - `db/migrations/sqlite/0004_extensions.sql` (`extension_packages`, `extension_versions`, `extension_installations` có `active_version_id` cho phép `NULL` lúc chờ duyệt, `org_extension_policies`, `extension_revocations`).
- **Out-of-Scope:** Đồng bộ chỉ mục Marketplace từ xa & kéo từ `Private OCI Registry` (thuộc `TP-IMP-0035`); xuất/nhập gói tổng hợp `Air-Gap Bundle` (thuộc `TP-IMP-0045`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0304`, `TP-FR-0306`, `TP-FR-0703`, `TP-FR-1002`, `TP-FR-1004`, `TP-NFR-0403`, `TP-NFR-0902`, `SM-01`, `CT-01`, `CT-02`, `CT-03`, `CT-04`, `CT-05`, `CT-18`, `CT-20`, `CT-24`, `CT-25`, `FZ-02`.
- **Dependencies:** `TP-IMP-0007`, `TP-IMP-0009`.
- **Design Refs:** [`extension-contract.md`](../../specifications/extension-contract.md) (`SPEC-EXT-01`), [`marketplace-lifecycle.md`](../../specifications/marketplace-lifecycle.md) (`SPEC-EXT-04`), [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-01`..`05`, `CT-18`, `CT-20`, `CT-24`, `CT-25`, `FZ-02`).

## 4. API, Wire & Data Contracts
- **Phép toán Cổng Duyệt Quyền & Chống Leo thang ($\Delta P$):**
  - Cài mới gói đòi quyền mạng `allowed_hosts` hoặc khe `secretSlots` → dừng ở `PENDING_PERMISSION_APPROVAL` (chỉ `ORG_OWNER` / `OPERATOR` / `SECURITY_REVIEWER` mới có quyền duyệt chuyển sang `INSTALLED_DISABLED` hoặc `ENABLED_ACTIVE`).
  - Nâng cấp `v1.0.0 -> v1.1.0`: nếu $\Delta P = P_{\text{new}} \setminus P_{\text{approved}} \neq \emptyset$ (thêm miền mới, thêm `secretSlots`, hoặc nâng từ Tầng 1 lên Tầng 2 Wasm), bản `v1.0.0` tiếp tục chạy bình thường còn `v1.1.0` bị giữ ở `UPDATING_PENDING_RECONSENT` (`CT-18`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given gói `.tpkg` hợp lệ hoặc tham chiếu Git SHA 40 ký tự, When cài đặt và `SECURITY_REVIEWER` duyệt tập quyền `P`, Then trạng thái đi đúng `VERIFYING -> PENDING_PERMISSION_APPROVAL -> ENABLED_ACTIVE`, cập nhật `config_changelog` và tệp `tanpopo.lock` ghi chính xác `sha256` cùng danh sách quyền `P`; khi gọi `Rollback`, lưu lượng quay về bản trước trong `< 5 giây`.
- **`NEG-1` (`CT-01`, `CT-02`, `CT-24` Schema & Contract Range):** Manifest sai cú pháp (`CT-01`), khai báo `contractVersion: "9.0"` không hỗ trợ (`CT-02`), hoặc nằm ngoài cửa sổ `N-1` của `coreVersionRange` (`CT-24`) → bị từ chối tường minh với mã lỗi chuẩn.
- **`NEG-2` (`CT-03`, `CT-05`, `CT-20` Integrity, Namespace & Org Trust Policy):** Sửa 1 byte gói `.tpkg` lệch `tanpopo.lock` (`CT-03`), gói ngoài mạo danh `@tanpopo/openai` hoặc `@tanpopo-official/x` thiếu chữ ký gốc (`CT-05`), hoặc cài gói `unverified-external` vào `Org` khóa chính sách chỉ nhận `official`/`verified` (`CT-20`) → bị chặn cứng `100%`.
- **`NEG-3` (`CT-04` Archive Traversal / Symlink / Bomb):** Gói nén chứa `../../etc/passwd`, `symlink` trỏ ra ngoài, `> 256` tệp hoặc luồng nén phình đại `10 KiB -> 200 MiB` → hủy giải nén ngay lập tức, `0` tệp lọt ra ngoài.
- **`NEG-4` (`CT-18` & `CT-25` Escalation & Mutable Git Ref):** Bản nâng cấp thêm ngầm miền mạng mới bị giữ ở `UPDATING_PENDING_RECONSENT` (`CT-18`); truyền nhánh động `main`/`latest` thay vì commit `40-hex SHA` bị từ chối `422 unpinned_git_reference` (`CT-25`).

## 6. Test Commands & Required Evidence
- Kiểm tra thuần Go (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/extension/installer/... ./internal/extension/lockfile/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/extension/installer/... ./internal/extension/lockfile/...`
- Kiểm tra `-race` & Fuzzing khói (`FZ-02`):
  - `go test -v -race ./internal/extension/installer/... ./internal/extension/lockfile/...`
  - `go test -fuzz=FuzzArchiveAndManifest -fuzztime=30s ./internal/extension/installer/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Tuyệt đối không thực thi bất kỳ hook kịch bản cài đặt nào (`postinstall`, shell script) khi giải nén hoặc cài gói `.tpkg`.
- **Rollback:** Quay lui phiên bản gói nguyên tử bằng con trỏ `active_version_id` trong `extension_installations` + `config_changelog`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (vá `SM-01` & chốt regex `^@tanpopo(-[a-z0-9]+)?/`), `TP-IMP-0003` (DDL 5 bảng mở rộng), `TP-IMP-0007` và `TP-IMP-0009` đã hoàn tất.
- **DoD:** Xanh toàn bộ `CT-01..05, 18, 20, 24, 25` + `FZ-02`, hoàn tất `REQUIRES_STRONG_REVIEW`.
