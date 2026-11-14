# `TP-IMP-0006` — Khung Kiểm thử Khép kín (`internal/testkit`): Chốt Chặn Socket `netguard`, `FakeClock`, `CanaryScanner`, `wasmfixtures` & `FakeLLMServer`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0006` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0005` — `GAP-01`)* |
| **Quy mô (`Size`)** | `M` (`5` gói nội bộ trong `internal/testkit/*`) |
| **Cấp độ Review** | `STANDARD_REVIEW` |
| **Chủ sở hữu Kỹ thuật** | `QA Architect + Test Systems Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Xây dựng bộ khung kiểm thử khép kín dùng chung (`internal/testkit/*`) theo đúng [`test-strategy.md`](../../quality/test-strategy.md) Mục 3 & 4 trước khi viết bất kỳ trình kết nối mạng nào, đảm bảo **về mặt cơ học không một bài kiểm thử nào có thể vô tình gọi DNS hay mở socket ra API AI trả phí ngoài Internet**, mọi bài kiểm thử thời gian đều tất định không dùng `time.Sleep()`, sẵn sàng bộ mẫu nhị phân `.wasm` đối kháng dựng sẵn từ `.wat`, và mọi rò rỉ khóa bí mật đều bị phát hiện tự động.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/testkit/netguard/` (khi `TANPOPO_TEST_HERMETIC=1`: ghi đè cả `http.DefaultTransport`, `net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: loopbackOnlyDial}` chặn mọi truy vấn DNS ra ngoài như `8.8.8.8:53`, và xuất hàm `netguard.CheckLoopbackAddr(addr)` tích hợp thẳng vào `egress.NewGuardedHTTPClient` ở `TP-IMP-0010` để ngay cả `http.Transport` tùy biến cũng bị khóa cứng ở `127.0.0.0/8` và `::1` trong chế độ kiểm thử!).
  - `internal/testkit/clock/` (`Clock` interface + `RealClock` + `FakeClock` an toàn đồng thời có hàm `Advance(d)`).
  - `internal/testkit/canary/` (`CanaryScanner` quét bộ đệm log, tệp tạm và chuỗi JSON tìm `sk-tp-canary-DO-NOT-LEAK-9f8e7d6c` cả dạng thô lẫn `base64`/URL-encoded).
  - `internal/testkit/wasmfixtures/` (chứa mã nguồn `.wat` minh bạch kèm mảng byte `.wasm` đã biên dịch sẵn cho 5 mô-đun kiểm thử: `echo_transform.wasm`, `infinite_loop.wasm`, `oom_64mib.wasm`, `unreachable_trap.wasm`, `unapproved_wasi_import.wasm` — giúp mọi máy Windows/macOS/Linux chạy kiểm thử Wasm không cần cài `TinyGo`).
  - `internal/testkit/fakellm/` (`FakeLLMServer` chạy trên `127.0.0.1:0` hỗ trợ phương ngữ OpenAI/Anthropic/Gemini + điều khiển tiêm lỗi qua tiêu đề `X-Tanpopo-Fake-Fault`: `delay_first_byte_ms`, `tcp_rst_after_chunks`, `http_status=429;retry_after=12`, `omit_stream_usage`, `echo_auth_header_in_error`).
- **Out-of-Scope:** `FakeOAuthIdP` và `FakeMCPServer` (được bổ sung tương ứng trong `TP-IMP-0034` và `TP-IMP-0036` khi mở mặt phẳng `v0.2`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0305`, `TP-FR-0806`, `TP-NFR-0101`, `TP-NFR-0102`, `OPS-AC-10`.
- **Dependencies:** `TP-IMP-0005`.
- **Design Refs:** [`test-strategy.md`](../../quality/test-strategy.md) Mục 3 & 4, [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) Mục 1.

## 4. API, Wire & Data Contracts
- `netguard.InstallHermeticGuard(t testing.TB)` và `netguard.MustRunHermeticMain(m *testing.M)`: chặn cả TCP, UDP và truy vấn DNS (`net.DefaultResolver`) ra ngoài loopback, báo lỗi kèm toàn bộ ngăn xếp hàm gọi.
- `fakellm.NewServer(t testing.TB) *FakeLLMServer`: tự động đóng khi bài kiểm thử kết thúc (`t.Cleanup`), ghi lại danh sách yêu cầu đã nhận (kèm tiêu đề xác thực đã nhận và thời điểm đóng kết nối khi khách hủy luồng) để các bài `CT-06`..`CT-16` đối chiếu chính xác.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `TANPOPO_TEST_HERMETIC=1`, When gọi `FakeLLMServer` tại `http://127.0.0.1:<port>/v1/chat/completions` (cả đồng bộ và SSE 50 mảnh có `: ping`), Then nhận phản hồi tức thì dưới `5 ms` với `0` truy cập mạng ngoài.
- **`AC-2`:** Given tiêu đề `X-Tanpopo-Fake-Fault: tcp_rst_after_chunks=5`, When trình khách đọc luồng SSE từ `FakeLLMServer`, Then nhận đúng 5 mảnh dữ liệu chuẩn rồi gặp lỗi đứt kết nối TCP tất định.
- **`AC-3`:** Given `FakeClock`, When 10 goroutines chờ bộ hẹn giờ và luồng chính gọi `FakeClock.Advance(61 * time.Second)`, Then toàn bộ điểm chờ mở khóa tức thì trong `< 1 ms` thời gian thực.
- **`NEG-1`:** Cố tình gọi `http.Get("https://api.openai.com/v1/models")`, `net.LookupHost("api.openai.com")` hoặc mở socket UDP tới `8.8.8.8:53` khi đang bật `netguard` → bị chặn ngay trước khi gửi gói tin và đánh trượt ca kiểm thử.
- **`NEG-2`:** Đưa chuỗi `sk-tp-canary-DO-NOT-LEAK-9f8e7d6c` (kể cả dạng mã hóa `base64`) vào bộ đệm nhật ký kiểm thử → `CanaryScanner.AssertClean(t)` phát hiện và báo lỗi ngay.
- **`NEG-3`:** Không dùng bất kỳ lệnh `time.Sleep()` cố định nào trong mã kiểm thử của `testkit`.

## 6. Test Commands & Required Evidence
- Kiểm tra thuần Go (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -count=3 ./internal/testkit/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v -count=3 ./internal/testkit/...`
- Kiểm tra tranh chấp dữ liệu (`-race`, `CGO_ENABLED=1`): `go test -v -race -count=3 ./internal/testkit/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Kết hợp khóa kép với biến `OPENAI_API_KEY=CANARY_FORBIDDEN_CI_KEY` đã đặt trong `.github/workflows/ci.yml` ở `TP-IMP-0005`.
- **Rollback:** Gói hỗ trợ kiểm thử độc lập, có thể revert mà không ảnh hưởng nhị phân sản xuất.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0005` đã hoàn tất.
- **DoD:** Đạt độ phủ dòng `>= 90%` trên `internal/testkit/...`, chạy cả `CGO_ENABLED=0` và `-race -count=3` không chập chờn.
