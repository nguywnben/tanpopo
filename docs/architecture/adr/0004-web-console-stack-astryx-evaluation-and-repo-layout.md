# ADR-0004: Đánh giá Độc lập Stack Web Console (`React 19 + Vite + StyleX + Astryx neutral`) Nhúng Tĩnh & Quy hoạch Monorepo Apache-2.0

## Status
**`PROPOSED`** *(Chờ Product Owner phê duyệt tại Gate G3)*

## Date
`2026-11-11`

## Context
Trong repository hiện tại đã có sẵn [`package.json`](../../../package.json), [`astryx-themes.js`](../../../astryx-themes.js), [`src/App.tsx`](../../../src/App.tsx) và [`src/main.tsx`](../../../src/main.tsx) với các gói `@astryxdesign/core@^0.7.13`, `@astryxdesign/theme-neutral`, `@astryxdesign/cli` và `@stylexjs/stylex` (`DEC-007`). Tuy nhiên:
1. Kỷ luật kiến trúc yêu cầu **không mặc định coi các gói cài sẵn là quyết định cuối cùng**, mà phải đánh giá công bằng về hiệu năng đóng gói, khả năng tự vận hành, tiếp cận `WCAG 2.2 AA` và chi phí bảo trì.
2. Hiện trạng repo đang tồn tại các điểm lệch thực tế (`ISS-001`..`005`): chưa có trình đóng gói/bundler, `package.json` đặt ngay thư mục gốc ghi `"license": "ISC"` lệch với [`LICENSE`](../../../LICENSE) `Apache-2.0`, và `src/App.tsx` chỉ là tệp giữ chỗ viết tay chưa dựng từ template Astryx.

## Decision
1. **Chấp thuận có đánh giá kỹ thuật (`PROPOSED`) việc sử dụng `TypeScript + React 19 + Vite + StyleX + Astryx v0.7.13 (theme neutral)` cho ứng dụng Web Console quản trị**, xuất ra gói tài sản tĩnh (`SPA Static Assets`) và **nhúng trực tiếp vào tệp thực thi Go (`embed.FS`)** để phục vụ từ cùng cổng quản trị của Control Plane mà không cần chạy máy chủ Node.js lúc vận hành.
2. **Quy hoạch lại cấu trúc thư mục Monorepo sạch sẽ (chỉ thực hiện khi bắt đầu pha code được duyệt):**
   - Dời toàn bộ mã nguồn và cấu hình giao diện về `web/console/` (kèm cấu hình Vite + trình biên dịch StyleX chuẩn, khai báo tường minh `react` & `react-dom`, đưa `@astryxdesign/cli` về `devDependencies` và khởi tạo lại màn hình chuẩn từ `npx astryx template ...` thay cho khung giữ chỗ viết tay).
   - Đồng bộ toàn bộ trường bản quyền trên mọi tệp cấu hình về **`Apache-2.0`** khớp với `LICENSE` gốc (`OQ-007`, `RSK-012`).

## Alternatives Considered

### Phương án B: Chuyển sang `Next.js App Router (SSR)` chạy tiến trình Node.js riêng
- *Nhược điểm:* Giao diện quản trị nội bộ đứng sau màn hình đăng nhập không cần SEO; việc bắt người dùng tự host phải chạy thêm một container Node.js SSR bên cạnh Go Core làm tăng gấp đôi dấu chân bộ nhớ và phức tạp hóa cấu hình mạng.
- *Lý do bác bỏ:* Gia tăng độ phức tạp hạ tầng mà không mang lại giá trị nghiệp vụ.

### Phương án C: Bỏ Astryx để chuyển sang `Tailwind CSS + shadcn/ui`
- *Ưu điểm:* Phổ biến rộng rãi.
- *Nhược điểm:* Đánh mất cơ chế kiểm tra tự động nghiêm ngặt của `astryx doctor --built` (vốn ngăn chặn hiệu quả lỗi AI sinh thẻ `div` lộn xộn, mã màu cứng và vi phạm cấu trúc bảng dữ liệu dày đặc) và đi ngược lựa chọn cảm quan `neutral` đã thiết lập ở `DEC-007`.

### Phương án D: Giao diện kết xuất phía máy chủ bằng `Go Templates + HTMX`
- *Ưu điểm:* Không cần chuỗi công cụ biên dịch Frontend JS.
- *Nhược điểm:* Rất khó duy trì trạng thái tương tác mượt cho các tính năng như `Playground` so sánh luồng SSE trực tiếp, cây phân tích `Route Explain` nhiều nhánh và hộp thoại đối chiếu quyền JSON Schema.

## Rationale
Sự kết hợp giữa **StyleX + Astryx** (biên dịch CSS tĩnh lúc build, `0` chi phí tính toán kiểu lúc chạy, ép buộc dùng `Table`/`List` cho dữ liệu dày đặc và đạt chuẩn `WCAG 2.2 AA`) với **Vite SPA nhúng vào Go `embed.FS`** mang lại ưu thế kép: trải nghiệm giao diện quản trị hiện đại nhất cho người dùng, nhưng ở góc độ vận hành SRE thì vẫn chỉ là **1 tệp thực thi Go duy nhất không phụ thuộc Node.js trên máy chủ sản xuất**.

## Consequences
- **Tích cực:** Xử lý dứt điểm toàn bộ `ISS-001`..`005`; không phát sinh tiến trình phụ lúc chạy; kiểm soát chất lượng giao diện tự động trong CI qua `npx astryx doctor --built`.
- **Tiêu cực / Đánh đổi:** Quy trình CI phát hành cần chạy bước `npm ci && npm run build` trong `web/console/` trước khi chạy `go build` để nhúng thư mục `dist/` vào nhị phân.

## Reversibility & Migration Implications
- **Mức độ đảo ngược:** **Dễ – Trung bình** vì toàn bộ Web Console chỉ giao tiếp với Control Plane qua giao diện chuẩn `/admin/v1/*` (`SPEC-API-01`), hoàn toàn không dính chặt vào cơ sở dữ liệu nội bộ.
- **Lưu ý thực thi:** Giữ nguyên hiện trạng các tệp gốc trong bước A05 (không tự ý di chuyển tệp mã nguồn trước khi Owner duyệt Gate G3 và mở pha triển khai).
