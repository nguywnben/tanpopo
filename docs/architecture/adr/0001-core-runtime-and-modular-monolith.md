# ADR-0001: Chọn Go (`1.24+`) Modular Monolith Có Khả năng Tách Vai trò Cho Tanpopo Core & CLI

## Status
**`PROPOSED`** *(Chờ Product Owner phê duyệt tại Gate G3)*

## Date
`2026-11-11`

## Context
Tanpopo phải đồng thời đáp ứng:
1. Trải nghiệm tự vận hành đơn nút siêu gọn (`TP-FR-1001`, `TP-NFR-0103`): khởi động `< 3 giây`, RAM khi rỗi `< 128 MB`, phân phối dạng 1 tệp thực thi đa hệ điều hành (`linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64` — `TP-NFR-0901`).
2. Hiệu năng đường nóng cao (`TP-NFR-0101`..`0102`, `TP-NFR-0301`): độ trễ cộng thêm `< 2 ms` (`p95`) cho Gói Khai báo, chuyển tiếp mảnh SSE `< 1 ms` (`p95`) và giữ `>= 1.000` luồng SSE đồng thời trên `2 vCPU, 2 GB RAM`.
3. Vận hành đa mặt phẳng phong phú (`Unified/Native LLM`, `Plugin Sandbox`, `MCP Hub`, `A2A`, `OAuth 2.1`, `FinOps Ledger`, `Admin API`, `CLI`) mà vẫn có thể mở rộng ngang không trạng thái ở cụm doanh nghiệp (`TP-FR-1005`).

## Decision
1. Sử dụng **Go (`1.24+`, biên dịch tĩnh `CGO_ENABLED=0`)** làm ngôn ngữ thực thi duy nhất cho **Tanpopo Core (`tanpopod`)** và công cụ dòng lệnh **`tanpopo` CLI**.
2. Tổ chức kiến trúc theo mô hình **Plane-Separated Modular Monolith**: một mã nguồn chia gói một chiều nghiêm ngặt (`ingress`, `iam`, `policy`, `routing`, `finops`, `extension`, `security`, `telemetry`, `storage`), biên dịch thành 1 tệp thực thi hỗ trợ cờ `--role=all` (mặc định đơn nút), `--role=data-plane` và `--role=control-plane` (khi triển khai cụm HA).

## Alternatives Considered

### Phương án B: Rust (`Tokio` + `Axum` + `Wasmtime`)
- *Ưu điểm:* Hiệu năng tối đa, không có trễ thu gom rác (GC), tích hợp gốc Wasm Component Model nhanh nhất.
- *Nhược điểm:* Tốc độ phát triển các luồng nghiệp vụ phức tạp ở Control Plane (OAuth 2.1, RBAC, Admin CRUD, MCP/A2A, đối soát tài chính) chậm hơn đáng kể; rào cản đóng góp từ cộng đồng mã nguồn mở cao hơn; liên kết tĩnh C++ khi biên dịch chéo đa nền tảng phức tạp hơn `CGO_ENABLED=0` của Go.
- *Lý do không chọn:* Chi phí tốc độ phát triển ở giai đoạn `v0.1`–`v0.3` quá đắt trong khi Go đã dư sức đạt ngưỡng `< 2 ms` (`p95`).

### Phương án C: Full-Stack TypeScript (`Node.js 22+` hoặc `Bun`)
- *Ưu điểm:* Đồng nhất 100% ngôn ngữ với giao diện Web Console; hệ sinh thái SDK web/MCP dồi dào.
- *Nhược điểm:* Vòng lặp sự kiện đơn luồng dễ bị vọt độ trễ đuôi (`p99`) khi đồng thời phân tích JSON đa phương thức lớn và đẩy hàng nghìn luồng SSE; tệp đóng gói độc lập nặng (`~90–120 MB`), khó giữ mức RAM rỗi `< 128 MB`; đặc biệt tiềm ẩn cám dỗ nạp plugin JS trực tiếp trong tiến trình.
- *Lý do không chọn:* Kém ưu thế về đóng gói hạ tầng tự vận hành gọn nhẹ và ổn định độ trễ dưới tải streaming lớn.

### Phương án D: Python (`FastAPI` / `uvloop`) hoặc Kiến trúc Microservices Đa Dịch vụ ngay từ đầu
- *Nhược điểm:* Python chịu giới hạn thông dịch/GIL gây nghẽn thông lượng proxy dưới tải cao; kiến trúc tách rời nhiều microservices ngay ở `v0.1` buộc mọi người tự host phải nuôi hàng loạt dịch vụ mạng nội bộ không cần thiết.
- *Lý do bác bỏ:* Vi phạm trực tiếp mục tiêu hiệu năng và tính thực dụng tự vận hành.

## Rationale
Go với `CGO_ENABLED=0` (kết hợp động cơ Wasm thuần Go `wazero` và trình điều khiển SQLite thuần Go) là điểm cân bằng tối ưu nhất trên bảng chấm điểm trọng số (`9.05/10` tại `system-design.md`): vừa biên dịch ra 1 tệp thực thi tĩnh chạy mọi nơi và nhúng sẵn tài sản tĩnh Web Console qua `embed.FS`, vừa xử lý hàng nghìn kết nối SSE bằng goroutines siêu nhẹ, lại vừa dễ đọc, dễ bảo trì và thu hút đóng góp cộng đồng.

## Consequences
- **Tích cực:** Cài đặt trong 1 tệp duy nhất; RAM thấp; tách vai trò `data-plane` / `control-plane` ở chế độ HA mà không tốn công duy trì nhiều kho mã dịch vụ rời rạc.
- **Tiêu cực / Đánh đổi:** Tồn tại ranh giới hai ngôn ngữ trong monorepo (`Go` cho Backend/CLI và `TypeScript` cho Web Console + một bộ Plugin SDK); đội ngũ cần kiểm soát cấp phát bộ nhớ trên đường nóng (`sync.Pool`) để tránh trễ GC.

## Reversibility & Migration Implications
- **Mức độ đảo ngược:** **Khó đảo ngược (*One-Way Door*)** sau khi hoàn thành `v0.1` Core. Tuy nhiên, nhờ toàn bộ `Extension Contract` (`SPEC-EXT-01`) và `API Contracts` (`SPEC-API-01`) hoàn toàn độc lập ngôn ngữ, các gói phần mở rộng của cộng đồng và Web Console sẽ không bị ảnh hưởng kể cả khi thay đổi thành phần nội bộ.
- **Tác động di trú trên Repo hiện tại:** Chưa di chuyển tệp ngay trong bước A05; khi bắt đầu giai đoạn lập trình được duyệt, khởi tạo `go.mod` và tách gọn thư mục `cmd/`, `internal/`, `web/console/`.
