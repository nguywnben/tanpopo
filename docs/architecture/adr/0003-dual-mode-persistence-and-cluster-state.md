# ADR-0003: Kiến trúc Lưu trữ Hai Chế độ Qua Giao diện Trừu tượng (`SQLite WAL` Đơn nút & `PostgreSQL + Valkey` Cụm HA)

## Status
**`PROPOSED`** *(Chờ Product Owner phê duyệt tại Gate G3)*

## Date
`2026-11-11`

## Context
Nhóm người dùng đầu tiên của Tanpopo trải dài từ kỹ sư cá nhân / nhóm nhỏ muốn chạy ngay trong 60 giây không cần quản trị cơ sở dữ liệu ngoài (`TP-FR-1001`), tới đội ngũ nền tảng doanh nghiệp cần chạy cụm nhiều bản sao không trạng thái đạt độ sẵn sàng `>= 99.9%` (`TP-FR-1005`, `TP-NFR-0201`) với sổ cái tài chính ACID và nhật ký kiểm toán bất biến lưu giữ `>= 365 ngày` (`TP-NFR-0501`).

## Decision
1. Thiết kế tầng truy cập dữ liệu tách rời hoàn toàn sau hai cổng giao diện nội bộ (`RelationalStore` và `FastStateCoordinator`).
2. **Chế độ Đơn nút Mặc định (`Standalone Mode` — `v0.1`):** Sử dụng **SQLite nhúng ở chế độ `WAL` (`busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`)** kết hợp bộ đếm/khóa giữ chỗ nguyên tử trong bộ nhớ RAM của tiến trình.
3. **Chế độ Cụm Sẵn sàng Cao (`Clustered HA Mode` — `v0.3+`):** Sử dụng **PostgreSQL `16+`** (bật phân vùng bảng theo thời gian cho `usage_settlements`, `request_traces_meta`, `audit_events` và bật `Row-Level Security` theo `org_id`) kết hợp **Valkey `8+` / Redis `7.2+`** cho kịch bản Lua giới hạn tốc độ, giữ chỗ ngân sách tốc độ cao và kênh phát tín hiệu thu hồi `Pub/Sub`.
4. **Không bắt buộc ClickHouse / Kafka trong Core:** Sử dụng bảng tổng hợp lũy kế (`usage_rollups_hourly`) trong SQL + xuất chuẩn **OpenTelemetry (`OTLP`)** để các tổ chức lớn tự nối vào kho phân tích sẵn có của họ.

## Alternatives Considered

### Phương án B: Bắt buộc PostgreSQL + Redis ở mọi bản cài đặt (kể cả cục bộ)
- *Ưu điểm:* Không cần kiểm thử tương thích SQLite.
- *Nhược điểm:* Tiêu diệt hoàn toàn khả năng chạy thử bằng 1 tệp thực thi duy nhất (`tanpopo serve`), đẩy ma sát onboarding lên cao.

### Phương án C: Chỉ dùng SQLite kết hợp nhân bản Raft nhúng (`rqlite` / `dqlite`) cho cả cụm HA
- *Nhược điểm:* Ghi sổ cái chi phí và vết lưu lượng liên tục qua đồng thuận Raft dễ nghẽn thông lượng ghi, vận hành khôi phục thảm họa khó hơn nhiều so với hệ sinh thái PostgreSQL chuẩn mà mọi đội SRE doanh nghiệp đều đã thành thạo.

### Phương án D: Tích hợp cứng ClickHouse + Kafka làm kho bắt buộc cho Log & Sổ cái
- *Nhược điểm:* Tăng mức tiêu thụ RAM tối thiểu lên nhiều GB, biến việc tự host thành gánh nặng hạ tầng khổng lồ (*over-engineering*).

## Rationale
Kiến trúc hai chế độ qua cổng trừu tượng giữ trọn lời hứa *"chạy đơn giản ngày đầu, mở rộng vững chắc ngày sau"*. Bằng cách tuân thủ 6 quy tắc khả chuyển tại [`data-model.md`](../data-model.md) (khóa `UUIDv7` chuỗi, tiền tệ `BIGINT micro-USD`, không phụ thuộc thủ tục lưu trữ đặc thù ở tầng nghiệp vụ), chi phí bảo trì song song hai bộ chuyển đổi SQL nằm trong tầm kiểm soát chặt chẽ.

## Consequences
- **Tích cực:** Khởi chạy `0` phụ thuộc ngoài ở `v0.1`; chịu tải ngang chuẩn công nghiệp ở `v0.3+`; cắt bỏ dữ liệu hết hạn không gây khóa bảng nhờ `DROP PARTITION` trên PostgreSQL.
- **Tiêu cực / Đánh đổi:** Mọi PR thay đổi lược đồ dữ liệu bắt buộc chạy kiểm thử tự động song song trên cả SQLite và PostgreSQL container trong CI để chặn sớm sai lệch cú pháp (`RSK-013`).

## Reversibility & Migration Implications
- **Mức độ đảo ngược:** **Trung bình** (do toàn bộ câu lệnh SQL nằm cô lập trong gói `internal/storage/{sqlite,postgres}`).
- **Công cụ nâng cấp giữa hai chế độ:** Cung cấp lệnh `tanpopo storage migrate-to-postgres --from sqlite://... --to postgres://...` để các nhóm đang chạy đơn nút chuyển toàn bộ cấu hình và lịch sử sổ cái lên cụm HA không mất dữ liệu.
