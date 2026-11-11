# ADR-0005: Sổ cái Ngân sách Hai Pha Số nguyên `Micro-USD`, Nhãn Độ chính xác & Tách biệt Tuyệt đối Thanh toán API Thương mại

## Status
**`PROPOSED`** *(Chờ Product Owner phê duyệt tại Gate G3 & Kiểm chứng tải qua `SPIKE-03`)*

## Date
`2026-11-11`

## Context
Trong các cổng AI hiện nay có 3 lỗi thiết kế tài chính nghiêm trọng thường gặp:
1. Chỉ kiểm tra ngân sách *trước* khi gửi và trừ tiền *sau* khi xong mà không khóa hạn mức tạm giữ, khiến 50 luồng song song cùng lọt qua cửa kiểm tra lúc tài khoản chỉ còn vài xu (`RSK-007`).
2. Dùng số thực dấu phẩy động (`FLOAT`/`DOUBLE`) gây sai số làm tròn tiền tệ, hoặc ghi nhận `0 USD` giả tạo khi luồng SSE đứt giữa chừng hay nhà cung cấp thiếu số đếm token (`TP-BR-0703`).
3. Nhập nhằng giữa tài khoản thanh toán API thương mại hợp pháp với các kết nối trích xuất gói thuê bao tiêu dùng cá nhân (`REJ-003`).

## Decision
1. **Số học tiền tệ nguyên tuyệt đối (`64-bit Integer Nano/Micro-USD`):** Lưu đơn giá theo `nano-USD / token` (`10^-9 USD`) và hạch toán sổ cái theo `micro-USD` (`10^-6 USD`, làm tròn trần bảo thủ), cấm hoàn toàn kiểu `FLOAT` trong sổ cái tài chính.
2. **Giao thức Giữ chỗ Ngân sách Hai Pha (`Two-Phase Hold` — `SM-07`):** Trước khi mở kết nối mạng ra ngoài, khóa nguyên tử khoản chi ước tính tối đa (`RESERVED_HOLD`) trên toàn bộ phân cấp `Virtual Key → Env → Project → Org` kèm thời hạn thuê (`lease_expires_at`). Khi luồng kết thúc, giải phóng phần giữ chỗ dư và ghi bút toán bất biến vào `usage_settlements`; nếu chi thực tế nhích nhẹ qua trần ở cuối luồng, ghi nhận trung thực vào `negative_carryover_micros` và chặn cứng từ yêu cầu kế tiếp (`BA-OQ-002`).
3. **Bắt buộc gắn nhãn độ chính xác 3 mức (`EXACT` | `ESTIMATED` | `UNMETERED_FLAGGED`):** Tách cột riêng trên mọi báo cáo đối soát `Chargeback` và trả tiêu đề `X-Tanpopo-Usage-Accuracy`.
4. **Ranh giới Thanh toán Cứng (`billing_mode`):** Chỉ chấp nhận `'COMMERCIAL_API'` và `'SELF_HOSTED_INFRA'`, tuyệt đối không thiết kế cơ chế gộp hay quy đổi gói thuê bao tiêu dùng cá nhân thành ngân sách API.

## Alternatives Considered

### Phương án B: Chỉ Kiểm tra Số dư Hậu kiểm Bất đồng bộ (`Post-Hoc Async Accounting`)
- *Ưu điểm:* Không tốn chi phí khóa tạm giữ trước yêu cầu.
- *Nhược điểm:* Cho phép các đợt gọi song song đốt vượt trần ngân sách hàng chục lần trước khi hệ thống kịp khóa (`UC-08`).

### Phương án C: Ngắt Cưỡng bức Giữa Luồng Ngay Giây Chạm Trần Ước tính (`Hard Mid-Stream Cut`)
- *Nhược điểm:* Cắt cụt câu trả lời hoặc JSON gọi hàm ở vài từ cuối cùng làm hỏng toàn bộ kết quả phía ứng dụng khách trong khi nhà cung cấp AI vẫn tính tiền toàn bộ phần đã sinh trước đó.

## Rationale
Giữ chỗ trước yêu cầu (`Pre-Request Hold`) chặn đứng bão song song ngay tại cổng vào, trong khi cơ chế `Negative Balance Carryover` bảo vệ tính toàn vẹn của các luồng đã được cấp phép hợp lệ mà không làm thất thoát quản trị tài chính.

## Consequences
- **Tích cực:** Cam kết kiểm soát trần chi phí đáng tin cậy dưới tải đồng thời cao; minh bạch hóa 100% sai số ước tính với bộ phận tài chính.
- **Tiêu cực / Đánh đổi:** Nếu ứng dụng khách không truyền `max_completion_tokens`, khoản giữ chỗ tạm thời sẽ dùng trần mặc định của chính sách (ví dụ `4096` tokens) nên có thể tạm chiếm hạn mức cao hơn mức thực dùng trong vài giây luồng đang chạy.

## Reversibility & Migration Implications
- **Mức độ đảo ngược:** **Khó đảo ngược** ở tầng lược đồ sổ cái `BIGINT micro-USD` sau khi có dữ liệu tài chính sản xuất, nhưng công thức ước tính hạn mức giữ chỗ ở Pha 1 có thể tinh chỉnh linh hoạt qua cấu hình chính sách mà không cần di trú dữ liệu.
