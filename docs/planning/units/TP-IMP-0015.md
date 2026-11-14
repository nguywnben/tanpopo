# `TP-IMP-0015` — Ghép Lát cắt Dọc `/v1/chat/completions` (Đồng bộ + SSE `< 1ms/chunk`, Hủy `< 500ms`, Cấm Fallback Giữa luồng) & `EXT-ACCEPT-01`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0015` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice` — **Cổng `GATE-M1`**) · `EP-02` / `EP-03` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0009`, `TP-IMP-0012`, `TP-IMP-0013`, `TP-IMP-0014` — `GAP-01, 02`)* |
| **Quy mô (`Size`)** | `L` (`internal/gateway/http/*`, `internal/gateway/sse/*`, `tests/integration/ext_accept_01_test.go`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Đường nóng Data Plane SSE, quản lý bộ đệm hủy luồng & nghiệm thu kiến trúc cốt lõi `M1`)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Gateway / Data-Plane Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Khép kín **Lát cắt Dọc Chứng minh Kiến trúc Mở rộng Đầu-Cuối (`Milestone M1` / `EXT-ACCEPT-01`)**: tích hợp xác thực `Virtual Key` (`TP-IMP-0009`), tra cứu mô hình không khóa từ `ConfigSnapshot`, thực thi biến đổi qua cả Adapter Tầng 1 (`TP-IMP-0012`) lẫn Wasm Tầng 2 (`TP-IMP-0013`), gọi nhà cung cấp qua `SSRFGuard` + `BlindBroker` (`TP-IMP-0010`), truyền phát luồng `Server-Sent Events (SSE)` với phụ phí `< 1 ms/chunk` (`http.Flusher`), lan truyền tín hiệu hủy luồng `< 500 ms` và thực thi bất biến **tuyệt đối không chuyển đổi dự phòng sau khi đã gửi byte SSE đầu tiên (`No Mid-Stream Fallback` — `FMEA-01`)**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/gateway/http/server.go` (HTTP server Data Plane với `ReadHeaderTimeout`, giới hạn thân yêu cầu cấu hình được, sinh/giữ `X-Request-ID` & `traceparent`).
  - `internal/gateway/http/chat_completions.go` (xử lý `POST /v1/chat/completions` đồng bộ và `stream: true`, chuẩn hóa bảng mã lỗi `CanonicalError` không lộ chi tiết nội bộ nhà cung cấp).
  - `internal/gateway/sse/pump.go` (bơm khung `text/event-stream; charset=utf-8`, tái sử dụng vùng đệm `sync.Pool`, gọi `Flush()` ngay sau mỗi sự kiện, theo dõi cờ `streamStarted` khóa cứng cấm retry/fallback giữa luồng, gửi sự kiện lỗi chuẩn cuối luồng nếu đứt gãy sau byte đầu tiên và đóng socket đích `< 500 ms` khi khách ngắt kết nối).
  - `tests/integration/ext_accept_01_test.go` (bài kiểm thử tích hợp đầu-cuối `EXT-ACCEPT-01`: cài mới một gói Adapter bên ngoài **mà không sửa/biên dịch lại bất kỳ dòng code Core nào** → duyệt quyền → gắn khóa mù → gọi `/v1/chat/completions` đồng bộ + SSE + `tool_calls` thành công).
- **Out-of-Scope:** Các điểm cuối `/v1/responses`, `/v1/embeddings` (thuộc `TP-IMP-0016`); thuật toán định tuyến nhiều đích / ngắt mạch `SM-04` (thuộc `TP-IMP-0020`); sổ cái giữ chỗ hai pha (thuộc `TP-IMP-0021`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0101`, `TP-FR-0103`, `TP-FR-0104`, `TP-NFR-0101`, `TP-NFR-0102`, `TP-BR-0503`, `TP-BR-0504`, `CT-06`, `CT-07`, `CT-10`, `CT-11`, `FZ-03`, `FMEA-01`, `EXT-ACCEPT-01`.
- **Dependencies:** `TP-IMP-0009`, `TP-IMP-0012`, `TP-IMP-0013`, `TP-IMP-0014`.
- **Design Refs:** [`system-design.md`](../../architecture/system-design.md) Mục 4.1 & 4.2, [`api-protocol-compatibility.md`](../../specifications/api-protocol-compatibility.md) (`SPEC-EXT-06`), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-01`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`EXT-ACCEPT-01`).

## 4. API, Wire & Data Contracts
- **Bất biến Chống Ghép Rác Giữa Luồng (`FMEA-01` — `No Mid-Stream Retry/Fallback`):**
  - Trước khi ghi header `200 text/event-stream` và khung dữ liệu đầu tiên (`streamStarted == false`): lỗi kết nối có thể được trả thành HTTP `502/503/429` có cấu trúc JSON chuẩn.
  - Ngay khi byte SSE đầu tiên đã đẩy xuống `ResponseWriter` (`streamStarted == true`): mọi nỗ lực gọi lại/đổi đích bị khóa cứng `100%`; nếu luồng nhà cung cấp đứt ngang, Gateway phát đúng 1 sự kiện lỗi chuẩn `event: error` (hoặc khung lỗi tương thích kèm `finish_reason: "error"`) rồi đóng luồng sạch sẽ, không bao giờ gửi `data: [DONE]` giả tạo.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`EXT-ACCEPT-01` Zero-Core-Edit Proof):** Given nhị phân Core đã biên dịch sẵn, When cài gói `.tppkg` của một nhà cung cấp giả lập mới hoàn toàn (`AcmeAI`), duyệt quyền, nạp khóa `sk-tp-canary-...` và gọi `POST /v1/chat/completions` (`stream: false` & `stream: true` + `tool_calls`), Then phản hồi chuẩn OpenAI trả về chính xác, `FakeLLMServer` nhận khóa thật, và `CanaryScanner` xác nhận `0` vết khóa trong phản hồi/log.
- **`AC-2` (`CT-11` Downstream Abort Propagation `< 500 ms`):** Given `FakeLLMServer` đang nhỏ giọt 1 khung mỗi 100 ms, When khách hàng đóng kết nối TCP ở khung thứ 3, Then kết nối phía trên tới `FakeLLMServer` nhận tín hiệu đóng socket trong `< 500 ms` và `0` goroutine bị rò rỉ (`goleak.VerifyNone`).
- **`NEG-1` (`CT-10` / `FMEA-01` Mid-Stream Cut):** `FakeLLMServer` gửi được 2 khung văn bản rồi ngắt TCP đột ngột → khách hàng nhận khung thông báo lỗi đứt luồng tường minh, **không** có lệnh gọi sang máy chủ thứ hai và **không** có chuỗi `[DONE]` giả.
- **`NEG-2` (`FZ-03` Malformed / Oversized SSE Line):** Nhà cung cấp trả dòng SSE không xuống dòng dài `> 2 MiB` hoặc JSON hỏng → bộ đọc SSE giới hạn trần dòng an toàn, hủy luồng có kiểm soát mà không cấp phát tràn RAM.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/gateway/... ./tests/integration/...`
- Fuzzing bộ phân tích khung SSE: `go test -fuzz=FuzzSSEParser -fuzztime=30s ./internal/gateway/sse/...` (`FZ-03`).

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Lột bỏ mọi header nhảy chặng (`Hop-by-Hop`), không bao giờ phản chiếu nguyên văn thân lỗi thô của nhà cung cấp thượng nguồn nếu chứa thông tin nhạy cảm hoặc HTML nội bộ.
- **Rollback:** Cột mốc khép kín đầu tiên của Data Plane; có thể tắt điểm cuối qua cấu hình nghe cổng.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0009`, `TP-IMP-0012`, `TP-IMP-0013` và `TP-IMP-0014` đã xanh toàn bộ.
- **DoD:** Vượt qua bài kiểm thử Cổng `GATE-M1` (`EXT-ACCEPT-01`), `0` rò rỉ goroutine (`uber-go/goleak`), `FZ-03` sạch lỗi và đạt `REQUIRES_STRONG_REVIEW`.
