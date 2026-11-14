# `TP-IMP-0029` — Giao diện Điều phối & Phân quyền: Trình Tạo Tuyến (`SCR-08`), Cây Chính sách (`SCR-09`) & Quản lý `Virtual Keys` / `RBAC` (`SCR-13`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0029` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0025`, `TP-IMP-0026` — `GAP-01, 12`)* |
| **Quy mô (`Size`)** | `M` (`web/console/src/pages/routes/*`, `policies/*`, `access/*`) |
| **Cấp độ Review** | `STANDARD_REVIEW` *(Rào chắn xác nhận hành động phá hủy `UX-GR-06` & hiển thị kế thừa chính sách đơn điệu)* |
| **Chủ sở hữu Kỹ thuật** | `Lead Frontend / Product Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Xây dựng cụm 3 màn hình quản trị định tuyến, chính sách và danh tính truy cập cốt lõi của `v0.1` bằng Astryx Design System (`neutral`):
1. **Trình Quản lý Tuyến & Dự phòng (`SCR-08 Routes`):** Tạo/sửa `RoutePolicy` (`PRIORITY_FAILOVER`, `WEIGHTED_ROUND_ROBIN`, `LOWEST_COST`, `LOWEST_LATENCY_EWMA`), hiển thị trực quan trạng thái cầu dao `SM-04` (`ACTIVE_HEALTHY`/`THROTTLED_COOLDOWN`/`CIRCUIT_OPEN_UNHEALTHY`/`HALF_OPEN_PROBING`) trên từng đích và cảnh báo ngay trên biểu mẫu nếu đích dự phòng lệch năng lực hoặc lệch vùng cư trú dữ liệu.
2. **Trình Quản lý Cây Chính sách Phân cấp (`SCR-09 Policies`):** Hiển thị rõ ràng phần chính sách kế thừa không thể nới lỏng từ cấp cha (`Org -> Project -> Env`) so với phần giới hạn siết thêm ở cấp hiện tại (`RPM`, `TPM`, `MaxConcurrentStreams`, `Data Residency`, danh sách mô hình).
3. **Trình Quản lý `Virtual Keys` & `RBAC` 6 Vai trò (`SCR-13 Access`):** Tạo khóa ảo `vk_tp_...` hiển thị 1 lần, xoay vòng có thời gian ân hạn `ROTATING_DUAL_VALID`, phân biệt rõ hai hành động *Xoay vòng êm* vs *Thu hồi Khẩn cấp Cắt Luồng (`Emergency Security Revoke`)* kèm hộp xác nhận nhập tên khóa (`UX-GR-06`), và gán 6 vai trò chuẩn cho thành viên.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `web/console/src/pages/routes/RoutesPage.tsx` (`SCR-08`).
  - `web/console/src/pages/policies/PoliciesPage.tsx` (`SCR-09`).
  - `web/console/src/pages/access/AccessAndKeysPage.tsx` (`SCR-13`).
- **Out-of-Scope:** Khung mô phỏng thử nghiệm `Dry-Run Route Explain` nâng cao trên `SCR-08` (thuộc `TP-IMP-0038` ở `M3`); cấu hình `Enterprise OIDC` & `Four-Eyes` (thuộc `TP-IMP-0041` / `0045`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0502`, `TP-FR-0503`, `TP-FR-0601`, `TP-FR-0701`, `TP-FR-0902`, `UJ-05`, `SCR-08`, `SCR-09`, `SCR-13`, `UX-GR-06`, `UX-AC-11`.
- **Dependencies:** `TP-IMP-0025`, `TP-IMP-0026`.
- **Design Refs:** [`user-flows.md`](../../ux/user-flows.md) (`UJ-05`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-08`, `SCR-09`, `SCR-13`), [`design-system-integration.md`](../../ux/design-system-integration.md) (`SPEC-UX-04`).

## 4. API, Wire & Data Contracts
- **Xác nhận Hành động Nguy hiểm (`UX-GR-06`):** Thu hồi khẩn cấp `Virtual Key` đang có lưu lượng hoạt động hoặc xóa `RoutePolicy` bắt buộc hiển thị số yêu cầu gần nhất sẽ bị ảnh hưởng và yêu cầu xác nhận tường minh trước khi gửi lệnh.
- **Đồng bộ `ETag`:** Mọi biểu mẫu chỉnh sửa trên `SCR-08`, `SCR-09`, `SCR-13` truyền `If-Match` và xử lý êm `409 Conflict`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`Route Guardrails` & `UX-AC-11` `409 Conflict` Draft-Preserving Diff Modal):** Khi cấu hình tuyến có tổng trọng số khác `100` hoặc chọn đích dự phòng thiếu năng lực `tool_calling`, biểu mẫu cảnh báo tại chỗ; khi lưu biểu mẫu trên `SCR-08`/`SCR-09` gặp lỗi `409 optimistic_lock_conflict` do quản trị viên khác vừa sửa, giao diện mở hộp đối chiếu 2 phiên bản giữ nguyên bản nháp người dùng đang gõ (`UX-AC-11`), tuyệt đối không làm mất dữ liệu nhập.
- **`AC-2`:** Khi `TEAM_ADMIN` mở `SCR-09`, các miền địa lý/mô hình đã bị `Org` khóa xuất hiện ở trạng thái khóa kế thừa (kèm chú giải nguồn khóa từ `Org Policy`) và không thể bật lại.
- **`NEG-1`:** Kiểm định `npx astryx doctor --built dist` đạt `0 fail` (`0` thẻ `div`/`span` thô, dùng `Table` cho danh sách khóa ảo và thành viên, `Badge` chỉ dùng cho con số đếm).

## 6. Test Commands & Required Evidence
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Giao diện không bao giờ tự lưu tạm `secret_key` mới tạo vào bộ nhớ đệm toàn cục của trình duyệt; xóa biến React state ngay khi đóng hộp thoại.
- **Rollback:** Cụm 3 màn hình độc lập trong `web/console/`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0025` và `TP-IMP-0026` đã hoàn tất.
- **DoD:** Xanh toàn bộ bài kiểm thử thành phần và hành trình `UJ-05`, `astryx doctor --built dist` đạt `0 fail`, `axe-core` sạch lỗi.
