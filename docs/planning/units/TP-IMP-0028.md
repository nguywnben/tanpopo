# `TP-IMP-0028` — Giao diện Danh mục & Phần mở rộng: Adapters (`SCR-03`), Kết nối Một chiều (`SCR-04`), Mô hình (`SCR-05`) & Duyệt Quyền/Quay lui Plugin (`SCR-06, 07`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0028` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0025`, `TP-IMP-0026` — `GAP-01, 12`)* |
| **Quy mô (`Size`)** | `L` (`web/console/src/pages/providers/*`, `connections/*`, `models/*`, `extensions/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Thực thi các rào chắn UX bảo mật cốt lõi `UX-GR-01..04`: cấm cài 1-chạm mù quyền & tách 3 chiều tin cậy)* |
| **Chủ sở hữu Kỹ thuật** | `Lead Product / Security UX Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Xây dựng cụm 5 màn hình quản lý danh mục kết nối AI và vòng đời phần mở rộng (`SCR-03`, `SCR-04`, `SCR-05`, `SCR-06`, `SCR-07`) theo đặc tả [`screen-specifications.md`](../../ux/screen-specifications.md) (`SPEC-UX-03`), thực thi nghiêm ngặt 4 rào chắn an toàn giao diện:
- **`UX-GR-01`:** Biểu mẫu bí mật trên `SCR-04` chỉ ghi một chiều (chỉ hiển thị dấu vân tay ẩn danh sau khi lưu).
- **`UX-GR-02` & `UX-GR-03`:** Hiển thị minh bạch 3 cấp bảo chứng năng lực (`DECLARED` / `TESTED` / `VERIFIED`) trên `SCR-05` và chỉ cho phép chế độ thanh toán hợp lệ (`COMMERCIAL_API` / `SELF_HOSTED_INFRA`), tuyệt đối không gắn nhãn chính thức hay hướng dẫn dùng thuê bao cá nhân (`DEC-004`).
- **`UX-GR-04`:** Trên `SCR-06` & `SCR-07`, tách rạch ròi 3 nhãn độc lập (`Origin`, `Publisher Trust`, `Execution Tier`) và **cấm cài đặt/nâng cấp một chạm ẩn quyền** — luôn mở bảng đối chiếu quyền chi tiết (`Permission & Delta Review Modal`: miền mạng `allowed_hosts`, khe bí mật `secretSlots`, tầng Sandbox) buộc quản trị viên duyệt tường minh trước khi gói chuyển sang `ENABLED_ACTIVE`, kèm nút quay lui 1 bước về phiên bản an toàn trước đó (`< 5s`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `web/console/src/pages/providers/AdaptersPage.tsx` (`SCR-03`), `ConnectionsPage.tsx` (`SCR-04`), `ModelsCatalogPage.tsx` (`SCR-05`).
  - `web/console/src/pages/extensions/ExtensionCatalogPage.tsx` (`SCR-06`) & `InstalledExtensionDetailPage.tsx` (`SCR-07` kèm bảng `PermissionDiffTable` hiển thị tập quyền mới $\Delta P$ khi nâng cấp và hành động `Rollback` / `Quarantine`).
  - Tất cả bảng dữ liệu dày đặc dùng `Table` / `List` của Astryx (không gói danh sách vào hàng loạt thẻ `Card`).
- **Out-of-Scope:** Luồng bấm nút ủy quyền chuyển hướng `OAuth 2.1` trên `SCR-04` (sẽ được nối thêm tại `TP-IMP-0034` / `0038` ở `M3`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0201`..`0204`, `TP-FR-0304`, `TP-FR-0306`, `TP-FR-0902`, `UJ-02`, `UJ-04`, `UJ-07`, `SCR-03`..`07`, `UX-GR-01`..`04`, `UX-AC-03`, `UX-AC-12`.
- **Dependencies:** `TP-IMP-0025`, `TP-IMP-0026`.
- **Design Refs:** [`user-flows.md`](../../ux/user-flows.md) (`UJ-02`, `UJ-04`, `UJ-07`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-03`..`07`), [`design-system-integration.md`](../../ux/design-system-integration.md) (`SPEC-UX-04`).

## 4. API, Wire & Data Contracts
- **Hiển thị Chênh lệch Quyền Khi Nâng cấp ($\Delta P$):** Khi `GET /admin/v1/extensions/{id}/permission-diff` trả về miền mạng mới hoặc nâng tầng từ `Declarative` lên `Wasm`, giao diện tô nổi bật các dòng quyền tăng thêm và vô hiệu hóa nút xác nhận cho đến khi người có vai trò `ORG_OWNER` / `OPERATOR` / `SECURITY_REVIEWER` tích xác nhận rà soát (`UX-AC-03`).
- **Xử lý Lỗi Xung đột `409`:** Khi cập nhật `Connection` hoặc `LogicalModel` gặp `409 optimistic_lock_conflict`, giao diện hiển thị thông báo có nút nạp lại phiên bản mới nhất thay vì xóa mất dữ liệu người dùng đang nhập.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`UX-AC-03` No Blind One-Click Install/Upgrade):** Given một gói cộng đồng chưa xác minh (`unverified-external`), When người dùng bấm cài đặt hoặc nâng cấp gói có $\Delta P \neq \emptyset$ trên `SCR-06`/`SCR-07`, Then gói không bao giờ kích hoạt ngay mà bắt buộc đi qua hộp đối chiếu 3 chiều tin cậy + danh sách quyền tường minh; khi bấm `Rollback`, trạng thái quay về bản trước đó thành công.
- **`AC-2` (`UX-AC-12` Capability Assurance Visibility):** Trên `SCR-05`, người dùng lọc và phân biệt rõ ràng giữa mô hình mới khai báo (`DECLARED`) và mô hình đã qua kiểm thử băng ghi (`TESTED`/`VERIFIED`).
- **`NEG-1`:** Người dùng có vai trò `DEVELOPER` mở `SCR-06`/`SCR-07` → các nút `Install`, `Approve Permissions`, `Quarantine` bị ẩn/vô hiệu hóa đúng ma trận RBAC (`SPEC-UX-01`).
- **`NEG-2`:** Chạy `npx astryx doctor --built dist` → `0 fail` (`0` thẻ `div`/`span` thô, `Badge` chỉ hiển thị số lượng).

## 6. Test Commands & Required Evidence
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Mọi chuỗi mô tả manifest do tác giả gói bên ngoài viết (`description`, `author`, `changelog`) chỉ được hiển thị dưới dạng văn bản thuần của React — **tuyệt đối không render HTML thô (`dangerouslySetInnerHTML`)** để chặn đứng `Stored XSS` từ gói độc hại!
- **Rollback:** Cụm trang danh mục độc lập trong `web/console/`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0025` và `TP-IMP-0026` đã hoàn tất.
- **DoD:** Xanh toàn bộ kiểm thử thành phần & hành trình `UJ-02, 04, 07`, kiểm tra chống XSS từ manifest ngoài, `astryx doctor --built dist` đạt `0 fail`, vượt qua `REQUIRES_STRONG_REVIEW`.
