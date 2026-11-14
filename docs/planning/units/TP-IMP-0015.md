# `TP-IMP-0015` — Ghép Lát cắt Dọc `/v1/chat/completions` (Đồng bộ + SSE `< 1ms/chunk`, Hủy `< 500ms`, Cấm Fallback Giữa luồng) & `EXT-SLICE-M1`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0015` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice` — **Cổng `GATE-M1`**) · `EP-03` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0009`, `TP-IMP-0012`, `TP-IMP-0013`, `TP-IMP-0014` — `GAP-01, 02`)* |
| **Quy mô (`Size`)** | `L` (`go.mod`, `internal/ingress/http/*`, `internal/ingress/sse/*`, `tests/integration/ext_accept_01_test.go`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Đường nóng Data Plane SSE, quản lý bộ đệm hủy luồng & nghiệm thu lát cắt kiến trúc `M1`)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Gateway / Data-Plane Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Khép kín **Lát cắt Dọc Chứng minh Kiến trúc Mở rộng Cốt lõi của `Milestone M1` (`EXT-SLICE-M1`)**: tích hợp xác thực `Virtual Key` (`vk_tp_`, `TP-IMP-0009`), tra cứu trực tiếp `LogicalModel -> primary active ModelDeployment` từ `ConfigSnapshot` (quy tắc chọn đích mặc định của `M1` trước khi bộ định tuyến đa đích `RouteResolver` được lắp vào ở `TP-IMP-0020`), thực thi biến đổi qua cả Adapter Tầng 1 (`TP-IMP-0012`) lẫn Wasm Tầng 2 (`TP-IMP-0013`), gọi nhà cung cấp qua `SSRFGuard` + `BlindBroker` (`TP-IMP-0010`), trả tiêu đề `x-tanpopo-request-id` chuẩn trên mọi phản hồi (`TP-FR-0101`), truyền phát luồng `SSE` với phụ phí `< 1 ms/chunk` (`SLO-03`), lan truyền tín hiệu hủy luồng `< 500 ms` và thực thi bất biến **tuyệt đối không chuyển đổi dự phòng sau khi đã gửi byte SSE đầu tiên (`mid_stream_abort` — `FMEA-01`)**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (thêm phụ thuộc kiểm thử `go.uber.org/goleak` mang giấy phép `MIT`).
  - `internal/ingress/http/server.go` (HTTP server Data Plane đúng gói `internal/ingress` của `ADR-0001`, giới hạn thân yêu cầu cấu hình được, từ chối rõ ràng yêu cầu nâng cấp giao thức WebSocket/WebRTC chưa hỗ trợ theo `TP-FR-0106`, sinh và đính kèm tiêu đề `x-tanpopo-request-id` + `traceparent` — `CT-06`).
  - `internal/ingress/http/chat_completions.go` (`POST /v1/chat/completions` đồng bộ và `stream: true`, chọn `ModelDeployment` hoạt động có `priority` cao nhất của `LogicalModel` trên `ConfigSnapshot`, chuẩn hóa bảng lỗi `CanonicalError`).
  - `internal/ingress/sse/pump.go` (bơm khung `text/event-stream; charset=utf-8`, vùng đệm `sync.Pool`, gọi `Flush()` ngay sau mỗi sự kiện, khóa cờ `streamStarted` cấm retry/fallback giữa luồng, phát sự kiện lỗi `mid_stream_abort` nếu đứt gãy sau byte đầu tiên và đóng socket đích `< 500 ms` khi khách ngắt kết nối).
  - `tests/integration/ext_accept_01_test.go` (hiện thực hóa bài kiểm thử lát cắt `EXT-SLICE-M1` chứng minh nạp nóng gói `.tpkg` của hãng mới `AcmeAI` cho cả Tầng 1 và Tầng 2 **mà không sửa/biên dịch lại mã Core** → duyệt quyền → gắn khóa mù → gọi `/v1/chat/completions` đồng bộ + SSE + `tool_calls` + hủy luồng thành công).
- **Out-of-Scope:** Các điểm cuối `/v1/responses`, `/v1/embeddings` (`TP-IMP-0016`); các bước mở rộng của `EXT-ACCEPT-01` phụ thuộc vào Định tuyến `EU` (`TP-IMP-0020`), Sổ cái `\$1.00` (`TP-IMP-0021`), Truy vấn `Blast-Radius` (`TP-IMP-0023`) và nhị phân chạy nền `tanpopo serve` (`TP-IMP-0031`) — sẽ được nâng cấp lên trọn vẹn **10/10 bước `EXT-ACCEPT-01`** tại `TP-IMP-0032` (`GATE-M2`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0101`, `TP-FR-0103`, `TP-FR-0104`, `TP-NFR-0101`, `TP-NFR-0102`, `TP-NFR-1002`, `SLO-02`, `SLO-03`, `TP-BR-0503`, `TP-BR-0504`, `CT-06`, `CT-07`, `CT-10`, `CT-11`, `FZ-03`, `FMEA-01`, `EXT-ACCEPT-01` *(phần cốt lõi `EXT-SLICE-M1`)*.
- **Dependencies:** `TP-IMP-0009`, `TP-IMP-0012`, `TP-IMP-0013`, `TP-IMP-0014`.
- **Design Refs:** [`system-design.md`](../../architecture/system-design.md) Mục 4.1 & 4.2, [`api-protocol-compatibility.md`](../../specifications/api-protocol-compatibility.md) (`SPEC-EXT-06`), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-01`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`EXT-ACCEPT-01`).

## 4. API, Wire & Data Contracts
- **Quy tắc Chọn Đích Mặc định Ở `M1`:** Khi chưa có bảng `route_policies`, `ConfigSnapshot.ResolveDefaultDeployment(orgID, envID, modelSlug)` trả về `ModelDeployment` ở trạng thái `ACTIVE_HEALTHY` (`SM-04`) gắn với `ProviderConnection` đang `ACTIVE` (`SM-02`) có thứ tự ưu tiên cao nhất; nếu không có → trả `503 no_compliant_deployment_available`.
- **Bất biến Chống Ghép Rác Giữa Luồng (`FMEA-01`):** Ngay khi byte SSE đầu tiên đã ghi xuống `ResponseWriter` (`streamStarted == true`), mọi nỗ lực gọi lại/đổi đích bị khóa cứng `100%`; nếu thượng nguồn đứt kết nối, phát đúng 1 sự kiện lỗi mang mã `mid_stream_abort` rồi đóng luồng, không bao giờ gửi `data: [DONE]` giả tạo.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`EXT-SLICE-M1` Zero-Core-Edit Proof):** Given tiến trình HTTP Data Plane đang chạy, When đóng gói `.tpkg` bằng `TP-IMP-0014` cho hãng giả lập mới hoàn toàn (`AcmeAI`), cài đặt qua `TP-IMP-0011`, duyệt quyền sang `ENABLED_ACTIVE`, nạp khóa `sk-tp-canary-...` và gọi `POST /v1/chat/completions` bằng khóa `vk_tp_live_...` (`stream: false` & `stream: true` + `tool_calls`), Then mọi phản hồi mang tiêu đề `x-tanpopo-request-id` (`CT-06`), khớp chuẩn OpenAI (`CT-07`), `FakeLLMServer` nhận khóa thật và `CanaryScanner` xác nhận `0` vết khóa rò rỉ.
- **`AC-2` (`CT-11` Downstream Abort Propagation `< 500 ms`):** Given `FakeLLMServer` nhỏ giọt 1 khung mỗi 100 ms, When khách hàng đóng kết nối TCP ở khung thứ 3, Then kết nối phía trên tới `FakeLLMServer` nhận tín hiệu đóng socket trong `< 500 ms` và `goleak.VerifyNone(t)` xác nhận `0` goroutine rò rỉ.
- **`NEG-1` (`CT-10` / `FMEA-01` Mid-Stream Cut):** `FakeLLMServer` gửi được 2 khung văn bản rồi ngắt TCP đột ngột → khách hàng nhận khung lỗi `mid_stream_abort` tường minh, `0` lệnh gọi sang đích khác và `0` chuỗi `[DONE]` giả.
- **`NEG-2` (`FZ-03` Malformed / Oversized SSE Line):** Nhà cung cấp trả dòng SSE không xuống dòng dài `> 2 MiB` hoặc JSON hỏng → bộ đọc SSE giới hạn trần dòng an toàn, hủy luồng có kiểm soát mà không cấp phát tràn RAM.

## 6. Test Commands & Required Evidence
- Kiểm tra thuần Go (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/ingress/... ./tests/integration/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/ingress/... ./tests/integration/...`
- Kiểm tra `-race` & Fuzzing bộ phân tích khung SSE (`FZ-03`):
  - `go test -v -race ./internal/ingress/... ./tests/integration/...`
  - `go test -fuzz=FuzzSSEParser -fuzztime=30s ./internal/ingress/sse/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Lột bỏ mọi tiêu đề nhảy chặng (`Hop-by-Hop`) và khóa ảo `vk_tp_*` trước khi ra biên mạng; không bao giờ phản chiếu nguyên văn thân lỗi thô của thượng nguồn.
- **Rollback:** Cột mốc khép kín đầu tiên của Data Plane; có thể tắt điểm cuối qua cấu hình nghe cổng.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0009`, `TP-IMP-0012`, `TP-IMP-0013` và `TP-IMP-0014` đã xanh toàn bộ.
- **DoD:** Vượt qua bài kiểm thử Cổng `GATE-M1` (`EXT-SLICE-M1`), `0` rò rỉ goroutine (`go.uber.org/goleak`), `FZ-03` sạch lỗi và đạt `REQUIRES_STRONG_REVIEW`.
