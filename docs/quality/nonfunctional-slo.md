# Chỉ tiêu Độ tin cậy, NFR, Chịu tải Quá ngưỡng & Phân tích Chế độ Lỗi Sâu (Reliability, SLOs & Deep FMEA) — Tanpopo

> **Tài liệu Kỹ thuật Độ tin cậy & SRE (Phase ASTRA A07 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — NO CODE`)**
> - **Mã tài liệu:** `SPEC-QA-03` | **Phiên bản:** `0.1.0-step07-draft` | **Ngày cập nhật:** `2026-11-12`
> - **Vai trò biên soạn:** Principal SRE, Performance Engineer & Resilience Architect
> - **Kỷ luật trung thực đo lường:** Tính đến hết bước **A07**, dự án **chưa viết mã nguồn thực thi và chưa chạy thử nghiệm đo tải thực tế**. Do đó, **mọi con số độ trễ, thông lượng, dung lượng bộ nhớ, `MTTR` và `SLO` trong tài liệu này đều bắt buộc mang nhãn `TARGET / UNVERIFIED`** và chỉ được chuyển sang `VERIFIED` sau khi có báo cáo đo tự động từ `SPIKE-01`..`04` và `Stage 4 CI`.
> - **Truy vết Yêu cầu:** `TP-NFR-0101`..`1002`, `TP-AC-N01`..`10`, `OPS-AC-01`..`12`, `ARCH-SYS-01`, `ARCH-OPS-01`, `ADR-0001`..`0006`.

---

## 1. Bảng Mục tiêu Cấp độ Dịch vụ (`SLOs`), Độ trễ Cộng thêm & Dung lượng (`TARGET / UNVERIFIED`)

| Mã Chỉ tiêu | Hạng mục Đo lường (`SLI` — Chỉ số Thực tế) | Ngưỡng Cam kết Đề xuất | Trạng thái Xác thực | Phương pháp Kiểm chứng Tự động (`Stage 4 CI / PoC`) |
|---|---|---|---|---|
| **`SLO-01`** | **Độ sẵn sàng Nội tại Của Data Plane (`Internal Availability`)** — Tỷ lệ truy vấn hợp lệ không gặp lỗi nội bộ `500` của Tanpopo (loại trừ lỗi `429/5xx` từ hãng AI đích). | **`>= 99.9%` / tháng** *(Ngân sách lỗi: `43,2 phút/tháng`)* | **`TARGET / UNVERIFIED`** | Giám sát liên tục qua chỉ số `tanpopo_gateway_internal_errors_total / tanpopo_requests_total` và bài đo tải bền `24h`. |
| **`SLO-02`** | **Độ trễ Cộng thêm Của Gateway (`Proxy Overhead` không tính thời gian chờ hãng AI):**<br/>- Nhánh `Tier 1 Declarative`<br/>- Nhánh `Tier 2 Wasm Sandbox` | - Tier 1: **`p95 < 2,0 ms`**, **`p99 < 5,0 ms`**<br/>- Tier 2: **`p95 < 5,0 ms`**, **`p99 < 12,0 ms`** | **`TARGET / UNVERIFIED`** | Kịch bản `k6` bắn `500 RPS` vào `FakeLLMServer` (`0 ms` delay) trên máy chuẩn `2 vCPU / 2 GiB RAM` (`SPIKE-01`, `SPIKE-02`). |
| **`SLO-03`** | **Độ trễ Chuyển tiếp Từng Mảnh `SSE` & Phản ứng Hủy luồng:**<br/>- Độ trễ qua cổng mỗi mảnh `data:`<br/>- Thời gian đóng kết nối đích khi khách hủy | - Mảnh SSE: **`p95 < 1,0 ms`**, **`p99 < 2,5 ms`**<br/>- Hủy upstream: **`< 500 ms`** | **`TARGET / UNVERIFIED`** | Đo chênh lệch dấu thời gian nano-giây giữa `FakeLLMServer` và trình đọc khách trên `1.000` luồng mở đồng thời (`CT-07`, `CT-11`). |
| **`SLO-04`** | **Tỷ lệ Lỗi Nội bộ Dưới Tải Định mức (`Internal Error Rate`)** | **`< 0,05%`** | **`TARGET / UNVERIFIED`** | Bài kiểm thử tải kết hợp thay đổi cấu hình liên tục trong `30 phút`. |
| **`SLO-05`** | **Thời gian Khởi động Lạnh & Dấu chân Bộ nhớ Tĩnh (`Cold Start & Idle RSS`):**<br/>- Từ lúc chạy lệnh tới `/readyz = 200`<br/>- Bộ nhớ `RSS` khi nghỉ (chế độ đơn nút) | - Khởi động: **`< 3,0 giây`**<br/>- Bộ nhớ nghỉ: **`< 128 MiB`** | **`TARGET / UNVERIFIED`** | Đo tự động trên container Linux sạch giới hạn tài nguyên (`OPS-AC-01`). |
| **`SLO-06`** | **Sức chịu tải Định mức Trên Một Nút (`Single-Node Capacity` — `2 vCPU / 2 GiB RAM`)** | **`>= 1.000` luồng SSE đồng thời** + **`>= 500 RPS` truy vấn ngắn** | **`TARGET / UNVERIFIED`** | Bài đo tải bậc thang (`Step-Load k6`) kiểm tra không rò rỉ goroutine hay tăng bộ nhớ tuyến tính. |
| **`SLO-07`** | **Độ trễ Lan truyền Lệnh Thu hồi Khóa / Cách ly Plugin (`Revocation SLA`)** | - Đơn nút: **`< 1,0 giây`**<br/>- Cụm đa nút: **`p99 < 5,0 giây`** | **`TARGET / UNVERIFIED`** | Kịch bản `AUTH-06` & `OPS-AC-03` bắn truy vấn liên tục trong lúc gọi API `Emergency Revoke`. |
| **`SLO-08`** | **Khôi phục Thảm họa & Khắc phục Sự cố (`MTTR / RPO / RTO`)** | - `MTTR`: **`< 15 phút`**<br/>- `RTO`: **`< 15 phút`**<br/>- `RPO`: **`<= 15 phút`** (SQLite backup) / **`<= 1 phút`** (Postgres WAL) | **`TARGET / UNVERIFIED`** | Diễn tập bấm giờ tự động 4 sổ tay `DR-01`..`04` trên môi trường sạch (`OPS-AC-07`). |

---

## 2. Kiểm soát Quá tải, Đua tranh Cấu hình, Sao lưu, Nâng cấp & Chế độ Cụm HA

1. **Giới hạn Tốc độ Đa Tầng & Xả tải Chủ động Khi Quá ngưỡng (`Rate Limiting & Graceful Load Shedding`):**
   - Áp dụng đồng thời bộ đếm tốc độ theo `Virtual Key`, `Project` và `Organization` (`RPM`, `TPM` ước tính đầu vào và `Max Concurrent Streams`).
   - **Cơ chế Xả tải Bảo vệ Sống còn (`Load Shedding`):** Khi số luồng đang mở trên một nút chạm **`90%` trần cấu hình** hoặc bộ nhớ heap Go chạm **`85%` `GOMEMLIMIT`**, cổng tự động từ chối sớm các yêu cầu mới độ ưu tiên thấp ngay tại biên mạng với mã **`503 gateway_overloaded`** kèm tiêu đề `Retry-After: 2` (có cộng nhiễu ngẫu nhiên chống dội tải đồng loạt), bảo toàn `100%` các luồng SSE đang truyền dở và mặt phẳng quản trị `/healthz` không bị `OOMKilled`.
   - Khi hàng đợi xuất `OpenTelemetry` đầy trần `32 MiB` do bộ thu ngoài chết, hệ thống xả bỏ bản ghi cũ nhất (`Drop-Oldest`) và tăng bộ đếm `tanpopo_telemetry_dropped_total` — **tuyệt đối không bao giờ chặn luồng suy luận vì lỗi hệ thống giám sát (`RSK-015`)**.
2. **Đột biến Cấu hình Đồng thời Dưới Tải Cao (`Concurrent Config Mutations`):**
   - Hai quản trị viên sửa cùng một `Route` hoặc `Policy` không gửi đúng `If-Match` sẽ nhận ngay `409 conflict_version_mismatch`.
   - Ở đường đọc Data Plane, cấu hình được đóng gói thành con trỏ bất biến `atomic.Pointer[ConfigSnapshot]`: bài kiểm thử căng thẳng bắn `50` lần cập nhật tuyến/giây song song với `1.000 RPS` lưu lượng suy luận phải đạt **`0` tranh chấp khóa (`mutex contention`) và `0` truy vấn đọc phải cấu hình nửa vời**.
3. **Diễn tập Sao lưu / Khôi phục, An toàn Di trú & Nâng cấp Không Gián đoạn (`Startup / Upgrade / Rollback / HA`):**
   - **Khởi động & Tắt Êm (`Graceful Drain`):** Khi nhận `SIGTERM`, nút lập tức chuyển `/readyz` sang `503 draining` (để cân bằng tải ngưng gửi kết nối mới), giữ sống các luồng SSE đang truyền tới khi kết thúc tự nhiên hoặc chạm `drain_timeout` (mặc định `60s`), quyết toán nốt các khoản giữ chỗ ngân sách rồi mới thoát `exit 0`.
   - **Chế độ Cụm Đa Bản sao (`HA Chaos Matrix`):** Bộ kiểm thử tiêm lỗi cụm kiểm chứng 3 kịch bản thực tế: (a) Giết đột ngột `Valkey` → các nút tự hạ cấp về bộ đếm tốc độ cục bộ chia theo số nút ước tính và kênh `PG LISTEN/NOTIFY`, không rớt truy vấn; (b) Chuyển đổi dự phòng `PostgreSQL Primary` → hồ kết nối tự nối lại trong vài giây, Data Plane vẫn phục vụ đọc từ `ConfigSnapshot` trong RAM; (c) Giết bất ngờ `1` trong `3` bản sao `tanpopod` giữa luồng → `2` bản sao còn lại gánh toàn bộ lưu lượng mới.

---

## 3. Phân tích Chiều Sâu Tám Chế độ Hỏng hóc Trọng yếu (`Deep Failure Mode & Effects Analysis — FMEA-01 .. FMEA-08`)

Bảng phân tích dưới đây khóa chặt hành vi tất định của Tanpopo trước **8 ca hỏng hóc nguy hiểm nhất trong sản xuất** — không để lập trình viên phải tự suy đoán cách xử lý ngoại lệ lúc viết code:

### `FMEA-01` — Đứt Luồng Giữa Chừng & Lệnh Cấm Chuyển Mạch Sau Mảnh Đầu Tiên (`Partial Stream + Fallback`)
- **Tình huống kích hoạt:** Nhà cung cấp chính (`Primary Deployment`) trả `HTTP 200 text/event-stream`, đã truyền thành công `k >= 1` mảnh văn bản/gọi hàm về phía `API Client`, sau đó kết nối TCP bị đứt ngang hoặc hãng trả lỗi giữa chừng.
- **Hiểm họa nếu xử lý sai:** Nếu Gateway âm thầm gọi sang nhà cung cấp dự phòng (`Fallback`) và đổ tiếp luồng mới nối đuôi vào luồng cũ, ứng dụng khách sẽ nhận một câu trả lời lắp ghép vô nghĩa hoặc thực thi lặp lại lệnh gọi công cụ `tool_calls` hai lần.
- **Quy tắc xử lý bất biến:**
  1. **Trước khi gửi byte nội dung đầu tiên (`k = 0`):** Lỗi kết nối, quá hạn `TTFT`, `429` hoặc `5xx` → **ĐƯỢC PHÉP** chuyển mạch sang đích dự phòng kế tiếp trong `Route` (`SM-04`).
  2. **Sau khi đã gửi mảnh đầu tiên (`k >= 1`):** **CẤM TUYỆT ĐỐI mọi hành vi retry hoặc chuyển mạch dự phòng.** Gateway phát một khung sự kiện lỗi chuẩn hóa cuối luồng (`event: error` kèm mã `mid_stream_abort`, `request_id` và số mảnh đã phát) rồi đóng kết nối HTTP rõ ràng để SDK phía khách nhận biết luồng chưa hoàn chỉnh.
- **Quyết toán & Kiểm toán:** Đếm số token đã phát thực tế qua bộ ước lượng cục bộ, chốt bút toán sổ cái ở trạng thái **`SETTLED_ESTIMATED`** (giải phóng phần tiền giữ chỗ còn dư) và ghi nhận điểm phạt sức khỏe cho `Deployment` vừa đứt.
- **Bài kiểm thử bắt buộc:** `CT-10`, `EXT-ACCEPT-01` (Bước 6).

### `FMEA-02` — Thử Lại Các Lệnh Gọi Công cụ Có Tác động Phụ (`Retrying Tool Calls with Side Effects`)
- **Tình huống kích hoạt:** Tác nhân gọi công cụ qua mặt phẳng `MCP Hub` (ví dụ `charge_credit_card`, `create_jira_issue`, `delete_cluster`) và kết nối mạng tới máy chủ MCP bị quá hạn đọc (`Read Timeout`) hoặc rớt kết nối sau khi gói tin yêu cầu đã rời khỏi card mạng của Tanpopo.
- **Hiểm họa nếu xử lý sai:** Cơ chế retry tự động mặc định của proxy HTTP sẽ gửi lệnh lần thứ 2 làm trừ tiền khách hàng hai lần hoặc xóa nhầm tài nguyên tiếp theo.
- **Quy tắc xử lý bất biến:**
  1. Mọi công cụ MCP đều mang phân loại tác động (`read_only`, `mutating`, `destructive` — mặc định nếu chưa phân loại thì coi là **`mutating`** để đóng an toàn).
  2. Chỉ công cụ `read_only` mới được phép thử lại tối đa `1` lần khi lỗi mạng kết nối ban đầu. Với **`mutating` và `destructive`, số lần tự động thử lại bị khóa cứng bằng `0`** một khi byte đầu tiên của yêu cầu đã được ghi ra socket.
  3. Tanpopo đính kèm tiêu đề `Idempotency-Key` xuyên suốt xuống máy chủ MCP và trả về cho tác nhân mã lỗi tường minh **`504 ambiguous_side_effect_timeout`** kèm hướng dẫn kiểm tra trạng thái thực tế trước khi quyết định gọi lại.
  4. Với công cụ `destructive`, phiếu phê duyệt `Pre-Approval Token` bị gắn cứng với mã băm `SHA-256` của tham số chuẩn hóa JCS và chỉ có giá trị sử dụng **đúng `1` lần (`Single-Use Nonce`)**.
- **Bài kiểm thử bắt buộc:** `CT-22`.

### `FMEA-03` — Rò rỉ Quyền Riêng tư & Đầu độc Chéo Thuê bao Qua Bộ nhớ Đệm (`Response Cache Privacy`)
- **Tình huống kích hoạt:** Bật bộ nhớ đệm phản hồi (`Exact` hoặc `Semantic Cache`) trên cổng dùng chung nhiều dự án/phòng ban; `Project A` hỏi câu hỏi trùng khớp ngữ nghĩa với câu hỏi trước đó của `Project B` có đính kèm ngữ cảnh mật hoặc quyền công cụ cao hơn.
- **Hiểm họa nếu xử lý sai:** Trả nhầm câu trả lời chứa dữ liệu mật của `Project B` sang `Project A` (*Cross-Tenant Cache Bleed*), hoặc lưu đệm một phản hồi bị cắt cụt/chứa thông tin cá nhân nhạy cảm.
- **Quy tắc xử lý bất biến:**
  1. **Mặc định tắt (`Disabled by Default`);** chỉ bật khi quản trị viên cấu hình tường minh trên từng `Route` / `Project`.
  2. **Cấm tuyệt đối chia sẻ cache chéo `Organization`, chéo `Project` hoặc chéo `Environment`:** Khóa tra cứu bắt buộc tính bằng `HMAC-SHA256(org_de_salt, org_id || project_id || env_id || deployment_id || canonical_prompt_and_params || policy_version_hash || allowed_tools_hash)`.
  3. Tự động bỏ qua không lưu và không đọc cache khi: truy vấn có `temperature > 0` (trừ khi tuyến ép buộc), có tiêu đề `Cache-Control: no-store` / `X-Tanpopo-No-Cache: true`, chứa kết quả gọi công cụ động, luồng bị ngắt dở (`mid_stream_abort`), hoặc bộ lọc đầu ra phát hiện dấu hiệu bí mật/PII.
  4. Mọi mục cache được mã hóa bằng `Org DEK` — khi chạy `Crypto-Shredding`, toàn bộ cache của tổ chức đó lập tức vô hiệu hóa.
- **Bài kiểm thử bắt buộc:** `CT-28`.

### `FMEA-04` — Thiếu Số Liệu Token Từ Nhà Cung cấp Hoặc Đếm Ước lượng (`Missing or Estimated Provider Usage`)
- **Tình huống kích hoạt:** Một mô hình tự host (`vLLM` cũ, adapter nội bộ) hoặc một luồng streaming kết thúc mà không gửi khối `usage` chuẩn ở mảnh cuối cùng; hoặc nhà cung cấp trả số âm/dị dạng.
- **Hiểm họa nếu xử lý sai:** Quyết toán sổ cái bằng `0 USD` tạo lỗ hổng cho người dùng tiêu xài vô hạn không bị trừ hạn mức, hoặc ngược lại báo cáo sai lệch thành con số chính xác trên hóa đơn kế toán nội bộ.
- **Quy tắc xử lý bất biến:**
  1. **Cấm tuyệt đối ghi nhận `SETTLED_EXACT = 0` khi có nội dung sinh ra mà thiếu trường `usage`.**
  2. Áp dụng thang dự phòng 3 bậc: (Bậc 1) Đếm token cục bộ theo bộ từ vựng khai báo của mô hình + bảng giá của `Deployment` → gắn nhãn **`SETTLED_ESTIMATED`**; (Bậc 2) Nếu không biết bộ từ vựng, ước tính bảo thủ theo số byte UTF-8 (`ceil(bytes / 3)`) → gắn nhãn **`SETTLED_ESTIMATED`**; (Bậc 3) Nếu `Deployment` chưa cấu hình đơn giá thương mại → chốt số lượng ước tính với cờ **`UNMETERED_FLAGGED`**.
  3. Báo cáo `FinOps Chargeback` tách riêng 3 cột `Exact Spend`, `Estimated Spend` và `Unmetered Requests`; nếu tỷ lệ không chính xác vượt `> 5%` trong `15 phút`, hệ thống bắn cảnh báo `ALT-UNMETERED-SPEND` (`INC-01`).
- **Bài kiểm thử bắt buộc:** `CT-12`, `FZ-06`.

### `FMEA-05` — Phần Mở rộng Bị Sập, Treo Vòng lặp CPU Hoặc Cạn kiệt Bộ nhớ (`Plugin Crash / CPU / OOM`)
- **Tình huống kích hoạt:** Một `Extension` Tầng 2 (`.wasm`) hoặc Tầng 3 (`Sidecar`) có lỗi chia cho 0, giải tham chiếu con trỏ rỗng (`wasm unreachable trap`), rơi vào vòng lặp vô hạn biểu thức chính quy (*ReDoS*), hoặc rò rỉ bộ nhớ trên mỗi yêu cầu.
- **Hiểm họa nếu xử lý sai:** Làm sập (`panic`) toàn bộ tiến trình `tanpopod`, treo cứng các goroutine xử lý cho mọi nhà cung cấp khác, hoặc ăn hết RAM của máy chủ.
- **Quy tắc xử lý bất biến:**
  1. Mỗi lần gọi vào phiên Wasm được bọc bởi bộ phục hồi lỗi (`recover` biên ABI), trần bộ nhớ tuyến tính riêng (`16 MiB`) và đồng hồ cắt ngữ cảnh cứng (`<= 50 ms` cho biến đổi yêu cầu, `<= 5 ms` cho mảnh SSE — `TARGET / UNVERIFIED`).
  2. Ngay khi một phiên Wasm bị `trap`, quá hạn CPU hoặc chạm trần bộ nhớ, phiên đó **bị hủy bỏ hoàn toàn và không bao giờ đưa trở lại hồ tái sử dụng (`Instance Pool`)**; truy vấn hiện tại nhận lỗi chuẩn `502 extension_execution_fault` (nếu chưa phát mảnh SSE nào thì bộ định tuyến được phép chuyển sang đích dự phòng dùng Adapter khác khỏe mạnh).
  3. Nếu cùng một gói mở rộng gây lỗi vượt ngưỡng (`> 20 lần / 60 giây`), bộ ngắt mạch riêng của gói đó tự mở (`ALT-PLUGIN-TRAP`), bảo vệ `100%` độ trễ của các gói khác trên cùng máy chủ.
- **Bài kiểm thử bắt buộc:** `CT-17`, `FZ-05`, `SPIKE-02`.

### `FMEA-06` — Lỗi Giữa Chừng Khi Nâng cấp Lược đồ Hoặc Di trú Cơ sở Dữ liệu (`Data Migration Failure`)
- **Tình huống kích hoạt:** Đang chạy nâng cấp phiên bản mới có thay đổi bảng SQL thì mất điện/ràng buộc dữ liệu cũ bị lỗi; hoặc quá trình chuyển từ `SQLite` sang `PostgreSQL` bị ngắt mạng ở bảng thứ 8/15.
- **Hiểm họa nếu xử lý sai:** Cơ sở dữ liệu rơi vào trạng thái nửa nạc nửa mỡ khiến bản mới không chạy được mà quay lui (`Rollback`) về nhị phân bản cũ cũng chết đứng.
- **Quy tắc xử lý bất biến:**
  1. **Kỷ luật Tương thích Lược đồ Hai Bước (`Expand -> Contract`):** Bản phát hành `N` chỉ được phép *mở rộng* (thêm bảng mới, thêm cột cho phép `NULL` hoặc có giá trị mặc định) sao cho nhị phân bản `N-1` vẫn đọc/ghi bình thường trên lược đồ đã nâng cấp nếu phải quay lui khẩn cấp; việc xóa cột cũ chỉ được thực hiện ở bản `N+1`.
  2. Tự động chụp bản sao lưu an toàn trước khi chạy bất kỳ tệp migration nào; trên PostgreSQL bọc mỗi bước trong giao dịch DDL có khóa tư vấn (`pg_advisory_lock`); nếu một migration thất bại và để lại cờ `dirty = true`, tiến trình mới dừng khởi động ngay (`exit 31`) kèm hướng dẫn khôi phục rõ ràng thay vì chạy liều trên dữ liệu hỏng.
  3. Lệnh `tanpopo db migrate-engine` (`SQLite -> Postgres`) chỉ ghi vào cơ sở dữ liệu đích trống theo từng lô giao dịch, kiểm tra khớp `100%` tổng số dòng, tổng sổ cái `micro-USD`, chuỗi băm `audit_events` và thử giải mã mẫu `AEAD AAD` thành công rồi mới cho phép chuyển lưu lượng.
- **Bài kiểm thử bắt buộc:** `CT-24`, `CT-32`, `OPS-AC-05`, `OPS-AC-06`.

### `FMEA-07` — Đứt Kênh Truyền Tin Thu hồi Khóa / Quyền Trên Cụm Phân tán (`Revocation Propagation Failure`)
- **Tình huống kích hoạt:** Quản trị viên bấm thu hồi khẩn cấp một `Virtual Key` vừa bị lộ hoặc cách ly một Plugin độc hại, nhưng một nút `Data Plane` trong cụm bị phân mảnh mạng tạm thời với `Valkey Pub/Sub` và `PostgreSQL`.
- **Hiểm họa nếu xử lý sai:** Nút bị mù thông tin vẫn tiếp tục cho phép kẻ tấn công dùng chiếc khóa đã thu hồi để hút dữ liệu và tiêu tiền không giới hạn từ ảnh chụp bộ nhớ cũ.
- **Quy tắc xử lý bất biến (`Stale-Node Fail-Closed Circuit Breaker` — `ADR-0006`):**
  1. Truyền tin thu hồi qua **3 kênh độc lập**: (1) `Valkey Pub/Sub` tức thì, (2) `PostgreSQL LISTEN/NOTIFY`, và (3) vòng lặp đối chiếu `config_epoch` + `revocation_watermark` trực tiếp từ DB mỗi `2 giây`.
  2. Mỗi nút duy trì dấu thời gian `last_verified_epoch_at`. Nếu nút không thể xác nhận `revocation_watermark` với nguồn chân lý quá **`max_stale_revocation_window = 10 giây`** (`TARGET / UNVERIFIED`), nút đó lập tức chuyển `/readyz` sang **`503 stale_security_state`** (để cân bằng tải cắt lưu lượng vào nút mù) và đóng an toàn (`Fail-Closed`) đối với các thao tác nhạy cảm thay vì phục vụ mù quáng vô thời hạn.
- **Bài kiểm thử bắt buộc:** `AUTH-06`, `OPS-AC-03`, `OPS-AC-04`.

### `FMEA-08` — Bão Làm Mới Token `OAuth` Đồng loạt Hoặc Sập Máy chủ Định danh (`OAuth Stampede & IdP Outage`)
- **Tình huống kích hoạt:** Một `Provider Connection` dùng `Authorized OAuth 2.1` phục vụ `200` truy vấn/giây chạm mốc hết hạn `access_token`; hoặc máy chủ OAuth của đối tác gặp sự cố `503` / thu hồi `refresh_token` (`invalid_grant`).
- **Hiểm họa nếu xử lý sai:** Cả `200` goroutine cùng bắn `200` yêu cầu làm mới token một lúc làm đối tác khóa tài khoản vì lạm dụng tốc độ, hoặc chiếc `refresh_token` dùng một lần (*Rotating Refresh Token*) bị vô hiệu hóa do đua tranh.
- **Quy tắc xử lý bất biến:**
  1. **Làm mới Chủ động Sớm (`Proactive Refresh at 80% TTL`):** Tác vụ nền tự động gia hạn khi token đạt `80%` vòng đời — nếu lúc đó máy chủ IdP chập chờn `503`, lưu lượng sản xuất vẫn chạy êm trên `20%` thời gian sống còn lại của token hiện hành mà khách không hề hay biết.
  2. **Khóa Đơn Luồng (`Single-Flight Refresh Lock`):** Khi token đã hết hạn hẳn, khóa nguyên tử (trong tiến trình và trên `Valkey` khi chạy cụm) đảm bảo chỉ **đúng `1` yêu cầu duy nhất** gọi tới IdP; các yêu cầu đồng thời khác chờ kết quả chung trong giới hạn `<= 5 giây`.
  3. Nếu IdP trả lỗi xác thực dứt khoát (`400 invalid_grant` / `401`), hệ thống **dừng ngay lập tức không thử lại**, chuyển kết nối sang `REAUTH_REQUIRED` (`SM-03`), phát cảnh báo `ALT-OAUTH-REAUTH` (`INC-05`) và cho phép bộ định tuyến chuyển lưu lượng sang đích dự phòng hợp lệ khác (nếu có).
- **Bài kiểm thử bắt buộc:** `CT-19`, `AUTH-10`.