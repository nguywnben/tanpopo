# Đặc tả Sổ cái Ngân sách Hai Pha, Định giá Tinh vi & Hệ thống Quan sát Chuẩn Sản xuất (Usage, Billing & Observability Specification) — Tanpopo

> **Tài liệu Đặc tả Kỹ thuật Cốt lõi (Phase ASTRA A05 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — PRE-APPROVAL`)**
> - **Mã tài liệu:** `SPEC-CORE-02` | **Phiên bản:** `0.1.0-step05-draft` | **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Principal SRE & FinOps/Observability Architect
> - **Trạng thái quản trị:** **Đặc tả Thiết kế Đề xuất (`PROPOSED`)** — gắn với [`ADR-0005`](../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md) và tiêu chuẩn vận hành tại `observability-and-instrumentation`.
> - **Truy vết Yêu cầu:** `TP-FR-0801`..`0806`, `TP-NFR-0101`..`0201`, `TP-NFR-0501`, `TP-BR-0701`..`0703`, `TP-BR-0802`, `SM-07`.

---

## 1. Bức Tường Lửa Nghiệp vụ: Tách biệt Chi phí API Thương mại Khỏi Khái niệm Thuê bao Tiêu dùng

Tuân thủ nghiêm ngặt `DEC-004`, `REJ-003` và yêu cầu tự kiểm tra kiến trúc A05, Sổ cái Tài chính (`FinOps Ledger`) của Tanpopo chỉ ghi nhận và hạch toán hai chế độ thanh toán kỹ thuật hợp lệ (`billing_mode`):
1. **`COMMERCIAL_API`:** Lưu lượng đi qua kết nối API trả theo mức tiêu thụ hợp pháp (API Key thương mại, Cloud IAM Bedrock/Vertex, hoặc cổng doanh nghiệp có đơn giá token/đơn vị rõ ràng).
2. **`SELF_HOSTED_INFRA`:** Lưu lượng đi tới cụm GPU nội bộ do chính đơn vị tự vận hành (`Ollama`, `vLLM`, `SGLang`), tính theo giá `0 micro-USD` hoặc đơn giá phân bổ nội bộ (`internal chargeback rate`) do quản trị viên tự cấu hình.

**Cấm tuyệt đối trong mô hình dữ liệu & giao diện:** Hệ thống không có khái niệm *"gộp chung tiền API Key với hạn mức gói thuê bao cá nhân ChatGPT Plus/Claude Pro"*, không có bộ đếm giả lập hạn mức tin nhắn gói tiêu dùng cá nhân và không bao giờ quy đổi phí thuê bao tháng cá nhân thành đơn giá API dùng chung.

---

## 2. Toán học Sổ cái Số nguyên `Micro-USD` & Giao thức Giữ chỗ Hai Pha (`Two-Phase Budget Hold`)

### 2.1. Công thức Tính Chi phí Không Sai số Dấu phẩy Động (`Integer Nano/Micro-USD Arithmetic`)

Mọi biểu giá trên `Model Deployment` được lưu bằng **số nguyên `nano-USD` trên 1 token** (`1 USD / 1.000.000 tokens = 1.000 nano-USD / token`; hỗ trợ chính xác tới cả các mô hình siêu rẻ `0.015 USD / 1M tokens = 15 nano-USD / token`). Khi quyết toán vào sổ cái `micro-USD` (`1 USD = 1.000.000 micro-USD`), hệ thống làm tròn lên bảo thủ theo phần dư (`ceiling division`) để không bao giờ hụt trần ngân sách:

$$
\text{Cost}_{\text{nano}} = (T_{\text{in}} \cdot P_{\text{in}}) + (T_{\text{out}} \cdot P_{\text{out}}) + (T_{\text{cache\_r}} \cdot P_{\text{cache\_r}}) + (T_{\text{cache\_w}} \cdot P_{\text{cache\_w}}) + (T_{\text{reason}} \cdot P_{\text{reason}}) + C_{\text{media\_nano}}
$$

$$
\text{Cost}_{\text{micros}} = \left\lceil \frac{\text{Cost}_{\text{nano}}}{1000} \right\rceil = \frac{\text{Cost}_{\text{nano}} + 999}{1000} \quad (\text{phép chia nguyên 64-bit})
$$

### 2.2. Hai Pha Giữ chỗ & Quyết toán Chống Cháy Ngân sách Song song (`SM-07`)

1. **Pha 1 — Tạm giữ Trước khi Mở Luồng (`Pre-Request Hold`):**
   - Ước tính chi phí trần của yêu cầu trên tập ứng viên định tuyến:
     - `est_input_tokens = ceil(utf8_byte_len(prompt_and_tools) / 3)` *(hệ số an toàn bảo thủ nhanh `< 0.05 ms` không cần tải bộ từ vựng nặng của từng hãng)*.
     - `bound_output_tokens = min(req.max_output_tokens ?? policy.default_hold_output_tokens, deployment.max_output_tokens)` *(mặc định `default_hold_output_tokens = 4096` nếu khách không truyền `max_tokens`)*.
     - `hold_micros = max_{d in candidates} CostMicros(d, est_input_tokens, bound_output_tokens)`.
   - Kiểm tra nguyên tử đồng thời trên cả 4 cấp (`Virtual Key`, `Environment`, `Project`, `Organization`):
     - Điều kiện cho qua tại mọi cấp `S`: `settled_exact_micros + settled_estimated_micros + negative_carryover_micros + reserved_hold_micros + hold_micros <= hard_limit_micros`.
     - Nếu bất kỳ cấp nào không đủ → từ chối ngay **`429 <scope>_budget_exceeded`** mà không chạm tới nhà cung cấp AI. Nếu đủ → cộng `hold_micros` vào `reserved_hold_micros` và tạo bản ghi `RESERVED_HOLD` có `lease_expires_at = now() + 90s` (tự động gia hạn mỗi `30s` khi luồng SSE còn đang truyền).
2. **Pha 2 — Quyết toán Trung thực & Xử lý Vượt Trần Nhẹ Cuối Luồng (`Settlement & Carryover`):**
   - Khi luồng kết thúc (thành công, đứt giữa chừng hoặc khách hủy), giải phóng đúng `hold_micros` khỏi `reserved_hold_micros` và ghi cộng `actual_cost_micros` vào cột tương ứng với nhãn độ chính xác:
     - **`EXACT`:** Nhà cung cấp trả về đầy đủ khung `usage` hợp lệ và có bảng giá khớp.
     - **`ESTIMATED`:** Luồng đứt giữa chừng hoặc máy chủ mô hình không trả `usage` ở mảnh cuối → đếm từ số byte văn bản thực tế đã nhận qua đường ống kèm cờ `X-Tanpopo-Usage-Accuracy: ESTIMATED`.
     - **`UNMETERED_FLAGGED`:** Loại phương tiện mới chưa cấu hình đơn giá hoặc lỗi giải mã không thể ước tính → ghi `total_cost_micros = 0` nhưng **bắt buộc** bật cờ `UNMETERED_FLAGGED` tách riêng trên báo cáo đối soát và phát sự kiện cảnh báo cấu hình cho `Team Admin` (`TP-BR-0703`).
   - **Kết chuyển Số dư Âm nhẹ (`Negative Balance Carryover` — `BA-OQ-002`):** Nếu chi phí thực tế cuối luồng lớn hơn khoản giữ chỗ ước tính ban đầu khiến tổng chi hơi nhích qua `hard_limit_micros`, Tanpopo **không cắt cụt câu trả lời ở vài từ cuối**, mà ghi nhận phần vượt thực tế vào `negative_carryover_micros` và chặn cứng `100%` yêu cầu mới kế tiếp cho tới khi sang kỳ ngân sách mới hoặc được nâng trần.

---

## 3. Thiết kế Quan sát Hệ thống Chuẩn Sản xuất (`OpenTelemetry Metrics, Traces & Structured Logs`)

### 3.1. Bốn Câu hỏi Trực chiến Cốt lõi (`On-Call Questions`) Định hướng Toàn bộ Tín hiệu

Mọi chỉ số, vết và dòng nhật ký trong Tanpopo được thiết kế để kỹ sư trực vận hành trả lời ngay 4 câu hỏi sau trong `< 2 phút`:
1. *Lỗi hay độ trễ cao hiện tại nằm ở **bên trong Tanpopo** (DB, Wasm Sandbox, Rate Limiter) hay do **nhà cung cấp AI phía sau** đang quá tải?*
2. *Tuyến nào, `Model Deployment` nào đang bị mở ngắt mạch (`CIRCUIT_OPEN`) và lưu lượng đã chuyển mạch sang đích dự phòng nào?*
3. *Vì sao yêu cầu `req_...` cụ thể của khách hàng bị chặn (`Policy`, `Data Residency`, `Budget`, `Capability` hay `MCP Schema Drift`)?*
4. *Có bất kỳ phần mở rộng nào đang bị kẹt nhiên liệu CPU, chạm trần bộ nhớ hay cố gọi ra miền mạng trái phép không?*

### 3.2. Danh mục Chỉ số Chuẩn `RED` & `USE` (Kiểm soát Chặt Số chiều `Bounded Cardinality`)

> **Quy tắc Chống Nổ Chuỗi Thời gian (`Zero High-Cardinality Labels`):** Tuyệt đối **cấm** gắn `request_id`, `virtual_key_id`, `principal_id`, `email`, đường dẫn URL thô hay thông điệp lỗi tự do làm nhãn (`label`) của Metric. Các trường chi tiết đó chỉ nằm trong `Structured Logs` và `Traces`.

| Tên Metric (Chuẩn OTel / Prometheus) | Loại | Tập Nhãn Cố định Nhỏ (`Allowed Bounded Labels`) | Câu hỏi trả lời |
|---|---|---|---|
| `tanpopo_http_requests_total` | Counter | `plane`, `route_template`, `status_class` (`2xx`/`4xx`/`5xx`), `error_code` | Tỷ lệ yêu cầu và mã lỗi chuẩn toàn cổng (`RED - Rate & Errors`). |
| `tanpopo_proxy_overhead_seconds` | Histogram | `plane`, `extension_tier` (`declarative`/`wasm`/`sidecar`) | Độ trễ nội bộ thuần của Tanpopo (`p50`/`p95`/`p99` đối chiếu mục tiêu `< 2ms`/`< 5ms`). |
| `tanpopo_sse_chunk_relay_seconds` | Histogram | `extension_tier` | Độ trễ chuyển tiếp từng mảnh SSE (`p95 < 1ms` — `TP-NFR-0102`). |
| `tanpopo_upstream_call_duration_seconds` | Histogram | `adapter_slug`, `data_region`, `status_class`, `phase` (`ttft`/`total`) | Thời gian phản hồi của chính nhà cung cấp AI đích. |
| `tanpopo_deployment_circuit_state` | Gauge | `adapter_slug`, `data_region`, `state` (`healthy`/`cooldown`/`open`) | Trạng thái ngắt mạch hiện thời của các đích mô hình. |
| `tanpopo_sandbox_traps_total` | Counter | `extension_id`, `trap_reason` (`cpu_fuel`/`oom`/`egress_denied`/`panic`) | Phát hiện tức thì phần mở rộng lỗi hoặc có hành vi đáng ngờ. |
| `tanpopo_active_streams` | UpDownCounter | `plane` | Mức bão hòa luồng SSE đang mở trên từng nút (`USE - Saturation`). |
| `tanpopo_config_snapshot_age_seconds` | Gauge | `sync_channel` | Giám sát trực tiếp cam kết thu hồi khẩn cấp `< 5 giây` (`TP-NFR-0404`). |
| `tanpopo_telemetry_dropped_total` | Counter | `signal` (`traces`/`metrics`/`logs`) | Phát hiện bộ thu OTLP bên ngoài đang nghẽn phải xả đệm `Fail-Open`. |

### 3.3. Nhật ký Cấu trúc JSON (`Structured Logging`) & Quyền Riêng tư Mặc định

Mọi dòng nhật ký xuất ra `stdout` (hoặc OTLP Logs) là một đối tượng JSON phẳng có trường định danh ổn định, bắt buộc mang cặp **`requestId`** và **`entryPoint`** (`unified_http`, `native_http`, `mcp_http`, `a2a_http`, `admin_api`, `cli`, `scheduler_reaper`) ngay từ điểm khởi phát:

```json
{
  "ts": "2026-11-11T16:20:05.142Z",
  "level": "warn",
  "event": "upstream_prestream_failover",
  "entryPoint": "unified_http",
  "requestId": "req_01jc8x9p2m4n6q8r0s2t4v6w8y",
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736",
  "orgId": "org_01jc8001",
  "projectId": "prj_01jc8009",
  "routeSlug": "prod-default",
  "failedDeploymentId": "dep_01",
  "upstreamStatus": 429,
  "nextDeploymentId": "dep_02",
  "attempt": 1
}
```

- **Bộ lọc Che Bí mật Đa Lớp (`Mandatory Redaction` — `TP-FR-0806`):** Cấm ghi thân yêu cầu/phản hồi (`messages`, `input`, `arguments`, `output`) vào dòng nhật ký chuẩn; bộ tuần tự hóa tự động thay thế mọi giá trị của tiêu đề `Authorization`, `x-api-key`, `x-goog-api-key`, `Cookie`, `Set-Cookie` và mọi mẫu tiền tố khóa (`vk_tp_*`, `sk-*`) bằng `"[REDACTED]"`.

### 3.4. Quy tắc Cảnh báo Theo Triệu chứng & Khung Runbook Tối thiểu (`Symptom Alerts & Runbooks`)

Chỉ sử dụng 2 mức độ cảnh báo hành động (**`PAGE`** gọi trực chiến ngay và **`TICKET`** xử lý trong tuần), mỗi cảnh báo gắn cứng liên kết tới `docs/runbooks/<alert-slug>.md`:

1. **`PAGE — TanpopoInternalHighErrorBurn`:** Tỷ lệ lỗi nội bộ `5xx` của Tanpopo đốt ngân sách `>= 14.4x` trong `5m/1h`.
   - *Runbook tóm tắt:* **Ý nghĩa:** Tầng lưu trữ hoặc bản triển khai mới của Tanpopo gặp lỗi nội bộ. **Kiểm tra đầu tiên:** Xem `tanpopo_http_requests_total{status_class="5xx"}` nhóm theo `error_code` và kiểm tra kết nối `PgBouncer/PostgreSQL`. **Leo thang:** Đội trực Hạ tầng Gateway (`Operator`) để thực hiện `Rolling Rollback`.
2. **`PAGE — RevocationSyncLagBreached`:** `max(tanpopo_config_snapshot_age_seconds) > 5` kéo dài quá `30 giây`.
   - *Runbook tóm tắt:* **Ý nghĩa:** Một hoặc nhiều nút Data Plane không nhận được bản cập nhật thu hồi khóa mới nhất. **Kiểm tra đầu tiên:** Kiểm tra sức khỏe cụm `Valkey` và kết nối `PostgreSQL` từ pod bị trễ. **Leo thang:** Cô lập pod bị kẹt khỏi Load Balancer.
3. **`TICKET — UnmeteredUsageDetected`:** Xuất hiện bút toán `usage_accuracy = "UNMETERED_FLAGGED"` mới trong `15 phút`.
   - *Runbook tóm tắt:* **Ý nghĩa:** Có lưu lượng đi qua mô hình chưa khai báo bảng giá hoặc Adapter không trích xuất được số đo. **Kiểm tra đầu tiên:** Truy vấn bảng `usage_settlements WHERE usage_accuracy = 'UNMETERED_FLAGGED'` để lấy `deployment_id` và cập nhật biểu giá.