# `TP-IMP-0027` — Giao diện Khởi tạo & Toàn cảnh: Setup Wizard Lần đầu (`SCR-01` Cho phép GPU Cục bộ & `TTFGR < 3m`), Đăng nhập & Bảng `Overview` (`SCR-02`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0027` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0025`, `TP-IMP-0026` — `GAP-01, 10, 12`)* |
| **Quy mô (`Size`)** | `M` (`web/console/src/pages/setup/*`, `login/*`, `overview/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Hành trình thiết lập quản trị viên đầu tiên, xác nhận bảo mật mở cổng GPU cục bộ `CR-10` & hiển thị khóa 1 lần)* |
| **Chủ sở hữu Kỹ thuật** | `Lead Product / Frontend Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa hành trình **Từ Con Số Không Đến Yêu Cầu Thành Công Đầu Tiên Dưới 3 Phút (`UJ-01` — `TTFGR < 3m`)** qua 3 màn hình đầu tiên sử dụng đúng template chuẩn của Astryx:
1. **Màn hình Đăng nhập & Cổng `Bootstrap` (`SCR-01`):** Đổi `Bootstrap Token` lấy tài khoản `ORG_OWNER` đầu tiên, dẫn qua trình hướng dẫn 4 bước gọn nhẹ (Tạo Kết nối Nhà cung cấp đầu tiên → Khai báo Mô hình → Sinh `Virtual Key` hiển thị đúng 1 lần kèm nút sao chép đoạn lệnh `curl` / Python / TS SDK → Bắn yêu cầu kiểm tra đầu tiên). Đặc biệt giải quyết `GAP-10` / `CR-10`: khi người dùng chọn `Ollama` / `vLLM` nội bộ, Wizard hiển thị hộp kiểm xác nhận bảo mật tường minh để cấp quyền `allow_private_ip = true` cho đúng cổng đích (ví dụ `127.0.0.1:11434`) mà không phá vỡ `SSRFGuard` mặc định.
2. **Bảng Điều khiển Tổng quan (`SCR-02 Overview`):** Hiển thị sức khỏe lưu lượng, độ trễ `p50/p95`, tỷ lệ lỗi, mức tiêu thụ ngân sách và cảnh báo phần mở rộng/sự cố nổi bật với đủ 5 trạng thái (`Happy`, `Loading`, `Empty`, `Error`, `Offline`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `web/console/src/pages/setup/SetupWizardPage.tsx` (khởi tạo từ Astryx onboarding/wizard template).
  - `web/console/src/pages/auth/LoginPage.tsx` (khởi tạo từ Astryx login/auth template).
  - `web/console/src/pages/overview/OverviewPage.tsx` (khởi tạo từ Astryx dashboard overview template).
  - `web/console/src/components/ShowOnceSecretDialog.tsx` (hộp thoại hiển thị `Virtual Key` một lần duy nhất kèm cảnh báo rõ ràng rằng khóa sẽ vĩnh viễn không thể xem lại sau khi đóng — `UX-GR-01`).
- **Out-of-Scope:** Đăng nhập nút `OIDC SSO` doanh nghiệp (thuộc `TP-IMP-0041`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0902`, `UJ-01`, `SCR-01`, `SCR-02`, `UX-GR-01`, `UX-AC-01`, `UX-AC-04`, `GAP-10`.
- **Dependencies:** `TP-IMP-0025`, `TP-IMP-0026`.
- **Design Refs:** [`user-flows.md`](../../ux/user-flows.md) (`UJ-01`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-01`, `SCR-02`), [`design-system-integration.md`](../../ux/design-system-integration.md) (`SPEC-UX-04`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-10`).

## 4. API, Wire & Data Contracts
- **Quy tắc Chống Lộ Khóa Trên Màn hình (`UX-GR-01`):** Ô nhập khóa API của nhà cung cấp luôn ở chế độ che ký tự mặc định, xóa sạch bộ nhớ state ngay sau khi gửi `POST /admin/v1/secrets`; hộp hiển thị `Virtual Key` mới sinh không lưu vào lịch sử URL hay bộ nhớ bền vững.
- **Kỷ luật Astryx:** `0` thẻ `div`/`span` thô; dùng `Table` / `List` cho danh sách cảnh báo dày đặc; `Badge` chỉ dùng cho con số đếm.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`UX-AC-01` Zero-to-First-Request Flow):** Given máy chủ trống chạy cục bộ cùng `FakeLLMServer` (cả chế độ API đám mây giả lập lẫn chế độ `Ollama` loopback có bật hộp xác nhận), When chạy kịch bản Playwright hoàn tất `SCR-01` và chuyển sang `SCR-02`, Then toàn bộ luồng hoàn tất mượt mà dưới `< 60 giây` tự động (tương đương `< 3 phút` thao tác người thật).
- **`AC-2` (`UX-AC-04` Show-Once Enforcement):** Sau khi đóng `ShowOnceSecretDialog` và tải lại trang, không còn bất kỳ phần tử DOM hay gói JSON mạng nào chứa chuỗi khóa rõ `vk_tp_live_...`.
- **`NEG-1`:** Chọn `Ollama (http://127.0.0.1:11434)` nhưng **không** tích hộp xác nhận cho phép kết nối địa chỉ nội bộ → nút tiếp tục bị khóa kèm giải thích bảo mật chống SSRF rõ ràng.
- **`NEG-2`:** Kiểm tra đáp ứng ở 3 độ rộng màn hình (`1280`, `768`, `375`) và kiểm tra `axe-core` → `0` lỗi tràn ngang, `0` lỗi tiếp cận nghiêm trọng.

## 6. Test Commands & Required Evidence
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `Bootstrap Token` trên `SCR-01` gửi qua thân JSON `POST` (không bao giờ đính trên chuỗi truy vấn URL để tránh rò rỉ vào `Referer` hay lịch sử trình duyệt).
- **Rollback:** Mô-đun trang giao diện độc lập trên nền `AppShell`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0025` và `TP-IMP-0026` đã hoàn tất.
- **DoD:** Xanh bài kiểm thử E2E hành trình `UJ-01`, `npx astryx doctor --built dist` đạt `0 fail`, vượt qua `REQUIRES_STRONG_REVIEW`.
