# ADR-0006: Ảnh Chụp Cấu Hình Bất Biến (`Copy-on-Write Snapshot`), Truyền Tin Thu Hồi Kép & Tư Thế Lỗi Phân Tầng

## Status
**`PROPOSED`** *(Chờ Product Owner phê duyệt tại Gate G3)*

## Date
`2026-11-11`

## Context
Trên đường nóng suy luận của Tanpopo, mỗi yêu cầu phải đối chiếu hàng loạt dữ liệu cấu hình (`Virtual Key`, `Org/Project/Env`, `Policy`, `Route`, `Model Deployment`, `Extension Permissions`, `MCP Schema Hash`) trong ngân sách tổng cộng dưới `< 1 ms`. Đồng thời, yêu cầu an ninh `TP-NFR-0404` bắt buộc khi thu hồi khẩn cấp khóa bị lộ hoặc cách ly phần mở rộng độc hại, hiệu lực chặn phải đạt **`< 1 giây`** ở chế độ đơn nút và **`< 5 giây`** trên toàn cụm phân tán, kể cả cắt đứt các luồng SSE đang mở (`BA-OQ-003`, `RSK-011`).

## Decision
1. **Ảnh chụp Cấu hình Bất biến trong RAM (`Immutable Copy-on-Write ConfigSnapshot`):** Mỗi nút Data Plane giữ toàn bộ cấu hình hoạt động trong một cây đối tượng bất biến truy xuất không khóa qua `atomic.Pointer[ConfigSnapshot]`, đi kèm bảng `ActiveStreamRegistry` ánh xạ khóa/kết nối/plugin sang hàm hủy luồng `context.CancelFunc`.
2. **Đường Truyền Thu hồi Ba Lớp Bảo hiểm (`Triple-Redundant Revocation Propagation`):**
   - *Lớp 1:* Kênh **`Valkey/Redis Pub/Sub`** (`< 100 ms`).
   - *Lớp 2:* Kênh **`PostgreSQL LISTEN/NOTIFY`** dự phòng trực tiếp từ kho chính khi Valkey gặp sự cố (`< 300 ms`).
   - *Lớp 3:* Nhịp đối chiếu `config_epoch` nhẹ mỗi **`2 giây`** kết hợp ngắt mạch tự cô lập nút nếu mất liên lạc quá `10 giây`.
3. **Tư thế Xử lý Lỗi Phân tầng Rõ ràng (`Tiered Failure Posture`):**
   - **`FAIL_CLOSED` cứng:** Xác thực, cô lập tổ chức, vùng dữ liệu `Data Residency`, quyền phần mở rộng, ngân sách cứng và phê duyệt công cụ `destructive`.
   - **`FAIL_OPEN` có kiểm soát:** Xuất giám sát `OpenTelemetry` bên ngoài (xả vòng đệm cũ nhất kèm tăng bộ đếm) và hạ bộ đếm tốc độ `RPM/TPM` về trần cục bộ bảo thủ `limit / N_nodes` khi Valkey mất kết nối.

## Alternatives Considered

### Phương án B: Truy vấn Trực tiếp PostgreSQL / Valkey Cho Mọi Yêu cầu Cấu hình (Không Snapshot RAM)
- *Nhược điểm:* Phát sinh nhiều vòng mạng trên mỗi yêu cầu, khó đạt `< 2 ms` (`p95`) và biến cơ sở dữ liệu thành điểm nghẽn cổ chai đơn lẻ ngay khi lưu lượng tăng đột biến.

### Phương án C: Đệm Cục bộ Theo Thời gian Sống Tĩnh (`Passive TTL Cache = 30–60 giây`)
- *Nhược điểm:* Vi phạm trực tiếp cam kết an ninh `TP-NFR-0404` (`< 5 giây`) và không thể cắt đứt các luồng đang mở khi xảy ra sự cố lộ khóa.

## Rationale
Kết hợp đọc không khóa từ `ConfigSnapshot` với cơ chế đẩy sự kiện đa kênh + chốt chặn đối chiếu `2 giây` mang lại tốc độ đọc nhanh như cấu hình tĩnh biên dịch cứng nhưng vẫn đảm bảo thu hồi khẩn cấp xuyên cụm dưới `5 giây` ngay cả khi một kênh truyền tin trung gian bị đứt.

## Consequences
- **Tích cực:** Độ trễ tra cứu cấu hình gần bằng `0` (`< 0.1 ms`), bảo vệ cơ sở dữ liệu khỏi tải đọc của Data Plane, đáp ứng chặt SLA thu hồi khẩn cấp.
- **Tiêu cực / Đánh đổi:** Mức tiêu thụ RAM trên mỗi nút tỷ lệ thuận với tổng kích thước cấu hình đang hoạt động (với quy mô `10.000` khóa ảo và `1.000` tuyến, ảnh chụp chỉ chiếm khoảng `~15–25 MB` RAM — hoàn toàn nằm gọn trong ngân sách).

## Reversibility & Migration Implications
- **Mức độ đảo ngược:** **Dễ đảo ngược nội bộ** vì hoàn toàn nằm bên trong cơ chế đồng bộ giữa Control Plane và Data Plane, không làm thay đổi API hay lược đồ đối ngoại.
