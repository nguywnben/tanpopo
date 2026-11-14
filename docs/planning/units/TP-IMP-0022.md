# `TP-IMP-0022` — Bộ Xuất Chuẩn `OpenTelemetry` (Giới hạn Cardinality, Vòng đệm `32 MiB` `Drop-Oldest`) & Trình Lọc Quyền Riêng tư `Metadata-Only`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0022` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0015`, `TP-IMP-0021` — `GAP-01, 13`)* |
| **Quy mô (`Size`)** | `M` (`internal/telemetry/otel/*`, `internal/telemetry/privacy/*`, `db/migrations/sqlite/0009_traces_blobs.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Quyền riêng tư `0 byte` nội dung prompt mặc định & bảo vệ đường nóng suy luận khi bộ thu OTLP sập)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Observability & Privacy Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa tầng quan sát thống nhất theo chuẩn **OpenTelemetry (`Metrics`, `Traces`, `Structured JSON Logs`)** kết hợp điểm cuối `/metrics` Prometheus, đảm bảo hai bất biến sống còn (`TP-FR-0805`, `TP-FR-0806`, `TP-NFR-0501`):
1. **Quyền Riêng tư Mặc định (`Metadata-Only Telemetry by Default`):** Mặc định lưu **đúng `0 byte`** nội dung câu lệnh (`prompt`), câu trả lời mô hình (`completion`) hay đối số gọi công cụ trên cả CSDL, vết OTel lẫn nhật ký; chỉ khi quản trị viên bật gỡ lỗi có thời hạn trên một phạm vi hẹp, thân gỡ lỗi mới được mã hóa bằng `Project DEK` với hạn tự hủy cứng `TTL <= 72 giờ` (`debug_payload_blobs`).
2. **Không Chặn Đường nóng Suy luận (`Non-Blocking Bounded Ring Buffer`):** Hàng đợi xuất OTLP giới hạn cứng bộ nhớ `<= 32 MiB` với chính sách tràn `Drop-Oldest` + tăng bộ đếm `tanpopo_telemetry_dropped_total` — nếu máy chủ thu thập OTLP bên ngoài chết hoặc treo mạng, độ trễ suy luận AI của khách hàng **không tăng quá `1 ms` và không bao giờ tràn RAM (`OPS-AC-12`)**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/telemetry/privacy/redactor.go` (bộ lọc đệ quy che sạch mọi trường `Authorization`, `api_key`, `tpk_*`, `sk-*`, `Bearer `, `Cookie`, `Set-Cookie`, và cắt bỏ nội dung `messages`/`input`/`output` khi `capture_payloads == false`).
  - `internal/telemetry/otel/metrics.go` (đăng ký bộ chỉ số chuẩn với kiểm soát nghiêm ngặt số lượng nhãn `Bounded Cardinality`: **cấm tuyệt đối** gắn `request_id`, `virtual_key_id`, `user_id` hay `prompt_hash` làm nhãn metric Prometheus!).
  - `internal/telemetry/otel/tracer.go` (vết phân tán đa chặng `Ingress -> Policy -> RouteAttempt -> ExtensionHook -> Egress`, vòng đệm `32 MiB` không chặn và ghi tóm tắt vết siêu dữ liệu vào bảng `request_traces`).
  - `internal/telemetry/ttl/cleaner.go` (tác vụ nền dọn sạch các bản ghi `debug_payload_blobs` quá hạn `expires_at <= now` và vết cũ theo lịch lưu giữ).
  - `db/migrations/sqlite/0009_traces_blobs.sql` (`request_traces`, `debug_payload_blobs`).
- **Out-of-Scope:** Giao diện biểu đồ thác nước truy vết trên Web Console (thuộc `TP-IMP-0030`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0805`, `TP-FR-0806`, `TP-NFR-0501`, `TP-NFR-0502`, `TP-BR-0802`, `OPS-AC-10`, `OPS-AC-12`.
- **Dependencies:** `TP-IMP-0015`, `TP-IMP-0021`.
- **Design Refs:** [`usage-billing-observability.md`](../../specifications/usage-billing-observability.md) (`SPEC-CORE-02` Mục 4 & 5), [`monitoring-incidents.md`](../../operations/monitoring-incidents.md) (`SPEC-OPS-03`), [`backups-recovery.md`](../../operations/backups-recovery.md) Mục 2.

## 4. API, Wire & Data Contracts
- **Danh sách Nhãn Metric Được Phép (`Strict Metric Label Allowlist`):** Chỉ cho phép các nhãn hữu hạn `{ org_id, project_id, adapter_id, logical_model, status_class, error_code, accuracy_tier }`; bộ kiểm thử đơn vị tự động quét mọi descriptor metric và đánh trượt build nếu xuất hiện nhãn không nằm trong danh sách trắng.
- **Giới hạn `debug_payload_blobs`:** Chỉ kích hoạt khi có phiên gỡ lỗi có hạn `expires_at <= created_at + 72h`, kích thước cắt ngọn `<= 256 KiB/request`, mã hóa bằng `Project DEK` (`TP-IMP-0008`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`Zero Prompt Byte Default`):** Given cấu hình mặc định, When gửi yêu cầu chứa chuỗi nhạy cảm `"SECRET_PATIENT_RECORD_998877"` và khóa `sk-tp-canary-...`, Then truy vấn toàn bộ bảng `request_traces`, `debug_payload_blobs`, đầu ra nhật ký JSON và span OTLP thu được trong `FakeOTLPCollector` xác nhận **`0` lần xuất hiện** của cả hai chuỗi (`OPS-AC-10`).
- **`AC-2` (`OPS-AC-12` Dead OTLP Collector Resilience):** Given `FakeOTLPCollector` cố tình treo kết nối TCP vô hạn (blackhole), When bắn 2.000 yêu cầu suy luận qua Gateway, Then `100%` yêu cầu suy luận vẫn hoàn tất bình thường, bộ nhớ hàng đợi OTLP không vượt `32 MiB` và chỉ số `tanpopo_telemetry_dropped_total` tăng chính xác.
- **`NEG-1` (`Cardinality Explosion Guard`):** Cố gắng đăng ký một metric mang nhãn `request_id` hoặc `virtual_key_id` → bài kiểm thử bảo vệ kiến trúc đánh trượt ngay lập tức.
- **`NEG-2` (`TTL Max > 72h Rejection`):** Cố tạo cấu hình bắt nội dung gỡ lỗi với `ttl_hours = 168` → bị từ chối `422 debug_capture_ttl_exceeds_72h`.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/telemetry/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `privacy.Redactor` đứng chắn ở tầng ghi thấp nhất trước cả bộ xuất OTLP, bộ ghi tệp nhật ký và bảng `request_traces`.
- **Rollback:** Bảng cộng thêm `0009_traces_blobs.sql`; có thể tắt xuất OTLP ngoài bằng cấu hình mà vẫn giữ `/metrics` nội bộ.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (đồng bộ tên metric `GAP-13`), `TP-IMP-0015` và `TP-IMP-0021` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca `OPS-AC-10`, `OPS-AC-12`, kiểm tra `CanaryScanner` + `Zero Prompt Byte`, vượt qua `REQUIRES_STRONG_REVIEW`.
