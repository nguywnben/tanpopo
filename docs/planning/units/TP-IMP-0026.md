# `TP-IMP-0026` — Nền tảng Web Console (`web/console/`): Vite + StyleX + Astryx `neutral`, Đóng gói Tĩnh Phông `Figtree` `Air-Gap`, Khung `AppShell` & API Client

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0026` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-06` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0005`, `TP-IMP-0024` — `GAP-01, 12`)* |
| **Quy mô (`Size`)** | `M` (`web/console/*`) |
| **Cấp độ Review** | `STANDARD_REVIEW` *(Tuân thủ tuyệt đối kỷ luật Astryx Design System v0.7.13 & vận hành mạng kín `0` tải ngoài)* |
| **Chủ sở hữu Kỹ thuật** | `Lead Frontend / Design System Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Thiết lập nền tảng kỹ thuật chuẩn xuất xưởng cho **Tanpopo Web Console (`web/console/`)** theo [`ADR-0004`](../../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md) và [`design-system-integration.md`](../../ux/design-system-integration.md) (`SPEC-UX-04`):
1. Di chuyển sạch bộ giữ chỗ gốc vào `web/console/` với trình đóng gói `Vite + TypeScript strict + StyleX compiler + @astryxdesign/core v0.7.13`, giữ nguyên gói giao diện mặc định **`neutral`** đã được Owner chọn (`DEC-007` — **cấm chạy `theme eject`**).
2. Khắc phục triệt để xung đột mạng kín `Air-Gap` (`GAP-12` / `CR-10`): cấu hình Vite alias ánh xạ phông chữ sang tệp `.woff2` `Figtree` nội bộ đóng gói sẵn trong bundle để khi chạy trong mạng hoàn toàn không có Internet sẽ phát sinh **`0` yêu cầu tải tài nguyên ra `fonts.googleapis.com` hay bất kỳ CDN ngoài nào**.
3. Khởi tạo khung điều hướng `AppShell` từ đúng template chuẩn của Astryx (`npx astryx template ...`), bộ khách API có kiểu dữ liệu sinh tự động từ `openapi/admin-v1.yaml` (tự động gắn `X-Tanpopo-CSRF` + `If-Match`), bộ chọn ngữ cảnh `Project / Environment` trên thanh đỉnh (ẩn bộ chọn đa `Org` ở `v0.1` đơn tổ chức theo `BA-OQ-001` / `UX-AC-10`) và các thành phần trạng thái chuẩn (`Loading`, `Empty`, `Error`, `Offline`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `web/console/package.json` (`"license": "Apache-2.0"`), `tsconfig.json`, `vite.config.ts` (kèm StyleX plugin & alias phông chữ `.woff2` nội bộ).
  - `web/console/astryx-themes.js` (kế thừa nguyên vẹn cấu hình `neutral` — không sửa token thủ công hay `:root`).
  - `web/console/src/main.tsx` (nhập đúng thứ tự `@astryxdesign/core/reset.css` và `@astryxdesign/core/astryx.css`).
  - `web/console/src/shell/*` (khởi tạo từ Astryx dashboard shell template, định tuyến 11 phân khu IA ẩn/hiện theo 6 vai trò RBAC, phím tắt bàn phím & điều hướng `WCAG 2.2 AA`).
  - `web/console/src/api/*` (trình khách gõ kiểu mạnh từ `openapi/admin-v1.yaml`).
- **Out-of-Scope:** Nội dung nghiệp vụ chi tiết của từng màn hình `SCR-01`..`16` (thuộc `TP-IMP-0027`..`0030` và `0038`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0902`, `TP-NFR-0701`, `TP-NFR-0702`, `TP-NFR-0801`, `TP-NFR-1001`, `DEC-007`, `UX-AC-07`, `UX-AC-10`, `GAP-12`.
- **Dependencies:** `TP-IMP-0005`, `TP-IMP-0024`.
- **Design Refs:** [`ADR-0004`](../../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md), [`information-architecture.md`](../../ux/information-architecture.md) (`SPEC-UX-01`), [`design-system-integration.md`](../../ux/design-system-integration.md) (`SPEC-UX-04`), [`AGENTS.md`](../../../AGENTS.md).

## 4. API, Wire & Data Contracts
- **Kỷ luật Bất khả Xâm phạm Của Astryx (`AGENTS.md` Rules):**
  1. Khởi tạo khung từ `npx astryx build` / `npx astryx template`, đọc props bằng `npx astryx component <Name>` trước khi dùng.
  2. **Tuyệt đối không dùng thẻ HTML `div` hoặc `span` thô**, không nhập tệp `.css` tùy biến ngoài 2 tệp chuẩn của Astryx, không gõ mã màu `#hex` hay số đo `px` cứng — chỉ dùng props của component và biến token `var(--color-*|--spacing-*|--radius-*)`.
  3. **`Badge` chỉ dùng cho con số đếm (`counts only`)**; nhãn trạng thái chữ (`ENABLED`, `QUARANTINED`, `VERIFIED`) dùng đúng component nhãn/chip ngữ nghĩa theo `SPEC-UX-04`.
  4. Trước khi hoàn tất phải chạy `npx astryx doctor --built dist` đạt **`0 fail`**.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given mã nguồn `web/console/`, When chạy `npm run typecheck && npm run build && npx astryx doctor --built dist`, Then quá trình đóng gói hoàn tất sạch lỗi với **`0 fail`**, kích thước gói khởi đầu nén gzip nằm trong ngưỡng ngân sách hiệu năng (`UX-AC-07`).
- **`AC-2` (`GAP-12` True Air-Gap Zero External Requests):** Given bản dựng tĩnh `dist/` mở trong trình duyệt kiểm thử chặn toàn bộ mạng ngoài `127.0.0.1`, When tải khung `AppShell`, Then bảng Network ghi nhận **`0` yêu cầu tới bất kỳ miền bên ngoài nào** (phông chữ hiển thị sắc nét từ tài nguyên nội bộ).
- **`NEG-1`:** Cố tình chèn một phần tử `div` thô, chuỗi `#ff0000` hoặc dùng `Badge` bọc chữ trạng thái trong bài test kiểm tra cổng CI → công cụ kiểm định đánh trượt ngay lập tức.
- **`NEG-2` (`Offline Banner`):** Ngắt kết nối tới `/admin/v1/*` → thanh cảnh báo mất kết nối xuất hiện rõ ràng mà không làm trắng trang (`ErrorBoundary`).

## 6. Test Commands & Required Evidence
- `cd web/console && npm ci --ignore-scripts && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Không lưu khóa hay mã phiên trong `localStorage` / `sessionStorage` (chỉ dùng cookie `HttpOnly` + biến CSRF trong bộ nhớ); cấu hình `Content-Security-Policy (CSP)` nghiêm ngặt `default-src 'self'; connect-src 'self'; font-src 'self'; object-src 'none'`.
- **Rollback:** Thư mục `web/console/` biệt lập, dễ dàng quay lui commit.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Product Owner phê duyệt `ADR-0004` & `CR-10` (cho phép dời tệp gốc sang `web/console/`); `TP-IMP-0005` và `TP-IMP-0024` đã hoàn tất.
- **DoD:** `npx astryx doctor --built dist` đạt `0 fail`, `0` yêu cầu mạng ngoài khi chạy `Air-Gap`, kiểm tra `axe-core` trên khung `AppShell` đạt `0` vi phạm nghiêm trọng.
