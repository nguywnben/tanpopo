# Hướng dẫn sử dụng Tanpopo

**Tanpopo** là cổng AI (AI Gateway) và trung tâm Model Context Protocol (MCP) Hub tự vận hành gói gọn trong một tệp thực thi duy nhất, được xây dựng cho định tuyến xác định, bảo mật thông tin xác thực chỉ ghi, kế toán FinOps chính xác đến micro-USD và khả năng hoạt động hoàn toàn ngoại tuyến.

---

## 1. Bắt đầu nhanh & Thiết lập lần đầu

1. Khởi chạy máy chủ Tanpopo:
   ```bash
   tanpopo serve --role all --listen 127.0.0.1:8080
   ```
2. Ở lần chạy đầu tiên, Tanpopo in ra một **mã khởi tạo (bootstrap token)** dùng một lần trong nhật ký máy chủ.
3. Mở `http://127.0.0.1:8080/setup` trên trình duyệt và hoàn tất 4 bước thiết lập:
   - **Tài khoản chủ:** Nhập mã khởi tạo, email quản trị, mật khẩu (tối thiểu 12 ký tự), tên tổ chức và tên dự án đầu tiên.
   - **Kết nối đầu tiên (tùy chọn):** Kết nối nhà cung cấp API thương mại (OpenAI, Anthropic, Google Gemini hoặc tương thích OpenAI) hay hệ thống tự vận hành cục bộ (vLLM, Ollama). Các điểm cuối cục bộ yêu cầu bật rõ quyền truy cập mạng nội bộ và liệt kê CIDR cụ thể không dùng wildcard (ví dụ `127.0.0.1/32`).
   - **Lưu giữ khóa chủ:** Xác nhận bạn đã sao lưu khóa mã hóa chủ của hệ thống (Master KEK). Nếu mất khóa chủ, các bí mật đã mã hóa trong kho sẽ không thể khôi phục.
   - **Xem lại & khởi tạo:** Hoàn tất thiết lập, lưu giữ an toàn Khóa ảo hiển thị một lần và kiểm tra yêu cầu luồng đầu tiên.

---

## 2. Ngôn ngữ & Giao diện

- **Web Console:** Dùng bộ chọn ngôn ngữ trên thanh trên cùng (hoặc trên trang Thiết lập và Đăng nhập). **Auto** là lựa chọn mặc định, tự động chọn ngôn ngữ được hỗ trợ đầu tiên theo danh sách ưu tiên của trình duyệt và rơi về English khi không khớp. Lựa chọn thủ công được ghi nhớ qua các lần tải lại và luôn có thể chuyển về **Auto**.
- **CLI:** Truyền cờ `--lang <mã>` (ví dụ `tanpopo --lang vi --help`) hoặc đặt biến môi trường `TANPOPO_LANG` / `LC_ALL` / `LC_MESSAGES` / `LANG`. Tên lệnh, tên cờ và đầu ra máy đọc giữ nguyên ổn định ở mọi ngôn ngữ.
- **Lỗi & Cảnh báo API:** Các điểm cuối Control Plane dưới `/admin/v1/*` tôn trọng header `Accept-Language` cho thông điệp đọc hiểu `error.message` và mô tả cảnh báo vận hành, trong khi giữ nguyên `error.code`, `error.type` và cấu trúc JSON.
- **Vận hành ngoại tuyến:** Toàn bộ 15 ngôn ngữ, phông chữ và biểu tượng đều được đóng gói sẵn trong tệp thực thi — không phát sinh bất kỳ yêu cầu CDN bên ngoài nào khi chạy.

---

## 3. Các luồng thao tác chính trên Console

### Tổng quan & Dấu vết
- **Tổng quan:** Hiển thị số lượng yêu cầu trực tiếp, tỷ lệ lỗi, độ trễ proxy p95, chi phí đã chốt theo micro-USD, tạm giữ hiện tại, các bản triển khai đã ngắt mạch và cảnh báo hiện tại kèm liên kết xử lý trực tiếp.
- **Dấu vết:** Ghi nhận dữ liệu thực thi chỉ gồm siêu dữ liệu (không lưu nội dung prompt hay phản hồi). Nhấp vào bất kỳ yêu cầu nào để xem biểu đồ phân tách từng giai đoạn và độ chính xác đo lường (`Chính xác`, `Ước tính`, `Chưa đo lường`).

### Nhà cung cấp, Mô hình & Định tuyến
- **Nhà cung cấp:** Niêm phong khóa API, thông tin xác thực AWS IAM, bí mật HMAC webhook hoặc chứng chỉ TLS vào kho chỉ ghi — bản rõ chỉ gửi một lần và không bao giờ trả lại. Gắn bộ chuyển đổi với điểm cuối upstream và chạy kiểm tra kết nối trực tiếp.
- **Mô hình & Bản triển khai:** Khai báo mô hình logic kèm năng lực và liên kết với kết nối nhà cung cấp bằng bảng đơn giá số nguyên **nano-USD** chính xác.
- **Tuyến & Chính sách:** Cấu hình chiến lược định tuyến xác định (`Chuyển dự phòng theo ưu tiên`, `Theo trọng số`, `Vòng tròn theo trọng số`, `Chi phí thấp nhất`, `Độ trễ thấp nhất (EWMA)`). Dùng **Mô phỏng tuyến** để chạy thử mà không gửi lưu lượng thật hay tạm giữ ngân sách. Cấu hình chính sách phân cấp Tổ chức → Dự án → Môi trường (cấp con chỉ có thể siết chặt thêm so với cấp cha).

### Tiện ích mở rộng, MCP Hub & Thử nghiệm
- **Tiện ích mở rộng:** Nạp gói từ Kho tiện ích ngoại tuyến hoặc qua URI ghim cố định kèm mã băm SHA-256 64 ký tự. Xem kỹ bảng chênh lệch quyền trước khi phê duyệt kích hoạt và cách ly ngay các gói có sự cố để cắt luồng đang chạy.
- **MCP Hub:** Đăng ký máy chủ MCP Streamable HTTP, đồng bộ công cụ, ghim mã băm lược đồ RFC 8785 JCS, phân loại rủi ro (`read_only`, `mutating`, `destructive`) và cấp quyền từng công cụ cho Khóa ảo.
- **Thử nghiệm:** Kiểm thử prompt trực tiếp với khóa phiên tạm thời có trần ngân sách và chỉ lưu trong thẻ trình duyệt hiện tại. Sao chép nhanh đoạn mã cURL, Python và TypeScript (OpenAI SDK).

### Truy cập, FinOps, Bảo mật & Vận hành
- **Truy cập & Khóa:** Cấp Khóa ảo theo môi trường, xoay vòng khóa với thời gian chuyển tiếp 24 giờ, hoặc thu hồi khóa bằng chế độ xả luồng êm hay cắt khẩn cấp. Quản lý phân quyền RBAC 6 vai trò (`Vận hành viên`, `Chủ tổ chức`, `Quản trị nhóm`, `Kiểm duyệt bảo mật`, `Xem FinOps`, `Lập trình viên`).
- **FinOps:** Theo dõi chi phí chốt chính xác, chi phí ước tính, tạm giữ trước khi gửi và yêu cầu chưa đo lường; đặt hạn mức cứng theo số nguyên micro-USD và xuất báo cáo phân bổ CSV/JSON.
- **Bảo mật:** Xử lý hàng đợi phê duyệt có người kiểm soát (HITL) cho lệnh MCP phá hủy, kích hoạt hoặc gỡ công tắc ngắt khẩn cấp 4 cấp và xác minh chuỗi băm kiểm toán RFC 8785 JCS.
- **Vận hành (vai trò `Vận hành viên`):** Giám sát `/readyz` và epoch bản chụp cụm, tạo bản sao lưu mã hóa `.tpop.enc`, chạy kiểm tra tương thích trước nâng cấp và đăng ký webhook ký HMAC.

---

## 4. Tóm tắt lệnh CLI

| Lệnh | Mục đích |
| --- | --- |
| `tanpopo serve` | Khởi chạy máy chủ Tanpopo (`--role all`, `control-plane`, `data-plane`). |
| `tanpopo doctor` | Kiểm tra tệp thực thi, kết nối cơ sở dữ liệu và quyền lưu trữ (hỗ trợ `--output json`). |
| `tanpopo support-bundle` | Thu thập gói chẩn đoán đã ẩn thông tin nhạy cảm (không bao gồm bí mật kho hay prompt). |
| `tanpopo admin reset-local-owner` | Đặt lại ngoại tuyến mật khẩu chủ tổ chức cục bộ (cần cờ `--db` / `--email`). |
| `tanpopo backup create / verify / restore` | Tạo hoặc khôi phục tệp sao lưu mã hóa `.tpop.enc`. |
| `tanpopo bundle export / verify / import` | Quản lý tiện ích đã ghim mã băm từ dòng lệnh. |
| `tanpopo upgrade preflight` | Xác minh tương thích ABI của tiện ích đã cài trước khi nâng cấp Tanpopo Core. |
| `tanpopo key / route / audit / killswitch` | Vận hành tài nguyên Control Plane từ xa qua `/admin/v1`. |
