# ADR-0002: Mô hình Thực thi Phần mở rộng 3 Tầng Cô lập & Trình Tiêm Khóa Mù (`Blind Host Credential Broker`)

## Status
**`PROPOSED`** *(Chờ Product Owner phê duyệt tại Gate G3 & Kiểm chứng qua `SPIKE-01`, `SPIKE-02`)*

## Date
`2026-11-11`

## Context
Khác biệt sống còn của Tanpopo là người dùng cài đặt `Provider Adapters` và các phần mở rộng khác từ Marketplace, Git hoặc kho riêng mà không cần phát hành lại Core (`DEC-002`). Tuy nhiên, nếu nạp mã bên thứ ba trực tiếp vào tiến trình Core, chỉ một gói cộng đồng bị cài mã độc có thể đọc sạch toàn bộ API Keys trong bộ nhớ hoặc đánh sập cổng (`RSK-001`, `TP-NFR-0401`). Đồng thời, bắt buộc viết mã biên dịch phức tạp cho mọi nhà cung cấp đơn giản sẽ làm chậm sự phát triển hệ sinh thái (`RSK-005`).

## Decision
1. **Cấm tuyệt đối nạp mã không tin cậy chạy chung không gian quyền với Core (`Zero In-Process Untrusted Code`):** Cấm sử dụng `plugin.Open`/`dlopen`, trình thông dịch kịch bản có quyền hệ thống hoặc nạp mô-đun động không cách ly.
2. **Áp dụng Kiến trúc Mở rộng 3 Tầng Cô lập (`SPEC-EXT-01`):**
   - **Tầng 1 — `Declarative Adapter` (YAML/JSON tĩnh):** Xử lý trực tiếp bởi bộ thông dịch an toàn không Turing-complete của Core cho `~75–80%` nhà cung cấp chuẩn (`0` bề mặt thực thi mã tùy ý).
   - **Tầng 2 — `Sandboxed Wasm Hooks` (chạy trên `wazero` thuần Go, mặc định đóng `0` quyền FS/Env/Socket):** Phục vụ `~15–20%` nhà cung cấp có định dạng luồng/ký xác thực đặc thù, giới hạn cứng nhiên liệu CPU (`fuel/epoch`) và trần bộ nhớ tuyến tính (`16–64 MB`).
   - **Tầng 3 — `Isolated Sidecar` (Container/tiến trình cách ly giao tiếp qua `ConnectRPC / Protobuf` trên UDS hoặc `mTLS`):** Chỉ do `Operator` triển khai cho `~5%` tác vụ nặng cần thư viện gốc C/HSM/ML hoặc cầu nối `stdio` MCP.
3. **Bắt buộc `Blind Host Credential Broker`:** Phần mở rộng không bao giờ được truy cập kho bí mật tổng; với các phương thức xác thực chuẩn và ký HMAC do Host hỗ trợ, chuỗi khóa bí mật gốc chỉ được tiêm hoặc ký ngay tại biên mạng của Host cho đúng `allowedHosts` đã được phê duyệt.

## Alternatives Considered

### Phương án B: Nạp Thư viện Động Trong Tiến trình (`Go plugin` / `dlopen` / In-process JS VM)
- *Lý do bác bỏ tuyệt đối:* Phá vỡ hoàn toàn ranh giới bảo mật (`TP-NFR-0401`), không thể thu hồi bộ nhớ sạch khi quay lui phiên bản, làm sập toàn bộ tiến trình Gateway nếu plugin panic và không chạy xuyên hệ điều hành (`Go plugin` không hỗ trợ đầy đủ Windows/static builds).

### Phương án C: Mọi Phần mở rộng Đều Chạy Tiến trình Con Riêng (`go-plugin` Subprocess cho 100% Adapter)
- *Nhược điểm:* Cài 20 Provider Adapters đồng nghĩa nuôi 20 tiến trình con tốn hàng trăm MB RAM trên máy cá nhân; khó ép buộc cô lập mạng đồng nhất trên Windows/macOS không có container; phân phối tệp thực thi riêng cho từng cặp OS/CPU từ Git rất nặng nề.
- *Lý do không chọn:* Kém xa Wasm về tính di động 1 tệp duy nhất và kém xa Tầng 1 về độ nhẹ.

### Phương án D: Chỉ Hỗ trợ Tầng 1 Khai báo YAML (Không có Wasm hay Sidecar)
- *Nhược điểm:* Bất lực trước các cổng AI nội bộ doanh nghiệp có thuật toán gói tin/chữ ký riêng, buộc người dùng phải quay lại sửa mã nguồn Core.

## Rationale
Mô hình 3 tầng đặt đúng mức chi phí kỹ thuật vào đúng độ phức tạp nghiệp vụ: `0` rủi ro mã độc và độ trễ `< 2 ms` cho số đông nhà cung cấp chuẩn (Tầng 1); tính di động đa nền tảng 1 tệp `.wasm` có khóa chặt tài nguyên cho logic tùy biến (Tầng 2); và lối thoát an toàn qua mạng cục bộ cho tích hợp hạ tầng đặc thù (Tầng 3), kết hợp `Blind Broker` để vô hiệu hóa nguy cơ đánh cắp khóa ngay từ thiết kế.

## Consequences
- **Tích cực:** Thêm nhà cung cấp mới không cần biên dịch lại hay khởi động lại Core; khóa API gốc an toàn ngay cả khi logic biến đổi JSON trong `.wasm` có lỗi.
- **Tiêu cực / Đánh đổi:** Đội ngũ nền tảng phải xây dựng và duy trì bộ khung kiểm thử hợp đồng (`Cassette Harness`) và kiểm chứng kỹ giao diện truyền mảnh SSE qua Wasm (`POC-01` / `SPIKE-02`).

## Reversibility & Migration Implications
- **Mức độ đảo ngược:** **Trung bình trong giai đoạn `0.x`** (hỗ trợ cửa sổ `N-1`), chuyển thành **Khó đảo ngược từ `v1.0`** (cam kết hỗ trợ `>= 12 tháng`).
- **Yêu cầu trước khi đóng băng ABI:** Bắt buộc hoàn tất `SPIKE-01` và `SPIKE-02` sau khi được Product Owner cho phép để chốt chi tiết giao diện bộ nhớ Wasm (`POC-01`).
