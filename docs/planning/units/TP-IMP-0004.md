# `TP-IMP-0004` — Thực thi 4 Thử nghiệm Có Giới hạn Thời gian (`SPIKE-01`..`04`) & Khóa Cứng `POC-01` Wasm ABI / `POC-02`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0004` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes`) · `EP-00` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0002, 0003` & giấy phép chạy PoC của Product Owner — `GAP-01`)* |
| **Quy mô (`Size`)** | `M` (Mã đo kiểm cô lập tạm thời + cập nhật kết quả vào `ADR-0002`, `ADR-0005`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Quyết định kiến trúc giao diện nhị phân Wasm dài hạn)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Platform Architect + Performance/Security Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Thực thi 4 bài thử nghiệm kỹ thuật ngắn hạn (`SPIKE-01`..`04` định nghĩa tại [`system-design.md`](../../architecture/system-design.md) Mục 7 và [`release-readiness.md`](../../quality/release-readiness.md) Mục 4.3) hoàn toàn ngoại tuyến để thu thập số đo thật, từ đó khóa dứt điểm giao diện bộ nhớ Host–Wasm (`POC-01`: `Linear-Memory Ring Buffer` vs `WIT Preview 2`) và tập toán tử ánh xạ Gói Khai báo Tầng 1 (`POC-02`) trước khi xây dựng động cơ sản xuất ở `M1`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - Mã thử nghiệm cô lập trong nhánh riêng hoặc thư mục tách biệt `tools/spikes/` (không nhập vào nhân sản xuất; dọn sạch hoặc đóng băng tham chiếu sau khi ghi báo cáo).
  - Báo cáo số đo thực nghiệm `docs/architecture/spike-results.md` và cập nhật trạng thái `POC-01`, `POC-02` trong `docs/architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md`, `0005-two-phase-budget-ledger-and-commercial-billing-separation.md` và `docs/specifications/extension-contract.md`.
- **Out-of-Scope:** Không viết cơ sở dữ liệu sản xuất, Admin API hay giao diện Web Console; không gọi bất kỳ API AI trả phí ngoài Internet nào.

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0301`..`0303`, `TP-FR-0801`, `TP-NFR-0101`, `TP-NFR-0102`, `TP-NFR-0202`, `TP-NFR-0401`; `ASM-002`, `ASM-004`, `ASM-005`, `ASM-006`, `POC-01`, `POC-02`.
- **Dependencies:** `TP-IMP-0002`, `TP-IMP-0003`.
- **Design Refs:** [`system-design.md`](../../architecture/system-design.md)`#L316-L348`, [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`release-readiness.md`](../../quality/release-readiness.md)`#L86-L99`.

## 4. API, Wire & Data Contracts
- **`SPIKE-01`:** Biểu diễn thử trên JSON Schema 10 nhà cung cấp mẫu bằng ngữ pháp khai báo Tầng 1 (`<= 120 dòng/gói`, không vòng lặp).
- **`SPIKE-02`:** Đo trên Go `1.24+` (`CGO_ENABLED=0`) + `wazero` với `1.000` luồng SSE giả lập (`50 chunks/luồng`) qua `127.0.0.1`: so sánh `p50/p95/p99` overhead, mức RAM và tốc độ ngắt vòng lặp `while(1)` giữa giao diện bộ nhớ tuyến tính và WIT; chụp `memory dump` của Wasm xác nhận `0 byte` chứa khóa mồi `sk-tp-canary-...`.
- **`SPIKE-03`:** Bắn `50` luồng song song vào hạn mức còn đủ `5` luồng trên `SQLite WAL` cục bộ để đo thông lượng giữ chỗ `Pre-Hold` và hủy kết nối `< 500ms`.
- **`SPIKE-04`:** Bắn `100` luồng đồng thời vào khóa `Single-Flight Refresh` với `FakeOAuthIdP` và đo thời gian băm `RFC 8785 JCS` cho `300` công cụ MCP.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `SPIKE-01`, When kiểm tra lược đồ khai báo trên 10 hãng mẫu, Then tối thiểu `8/10` hãng biểu diễn trọn vẹn không cần biên dịch Wasm.
- **`AC-2`:** Given `SPIKE-02` dưới tải `1.000` luồng SSE cục bộ trên giới hạn `2 vCPU / 2 GiB RAM`, When đo giao diện Wasm được chọn (hoặc phương án lai chỉ gọi Wasm đầu/cuối luồng), Then đạt `p95` overhead `< 5.0 ms`, mảnh SSE `< 1.0 ms` và `0 byte` khóa thật trong bộ nhớ Wasm.
- **`AC-3`:** Given `SPIKE-03` và `SPIKE-04`, When bắn tải đua tranh đồng thời, Then đúng `5/50` luồng mở kết nối (còn lại nhận `429 project_budget_exceeded` với `0` vượt trần) và đúng `1/100` yêu cầu gọi `/oauth/token`.
- **`NEG-1`:** Mô-đun Wasm chạy `while(true){}` hoặc xin cấp phát `64 MiB` vượt trần `16 MiB` bị ngắt sạch không làm `panic` tiến trình chủ.
- **`NEG-2`:** Tuyệt đối cấm gọi mạng ra ngoài `127.0.0.1` trong suốt quá trình chạy 4 Spikes.
- **`NEG-3`:** Nếu một giao diện ứng viên không đạt ngân sách `< 5 ms p95`, bắt buộc kích hoạt hướng xoay trục đã ghi trong `system-design.md` thay vì nới lỏng ngưỡng NFR ngầm.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -bench=. -benchmem ./tools/spikes/...`
- Lưu biểu đồ phân vị độ trễ, `pprof` bộ nhớ và cập nhật chữ ký hàm ABI chính thức vào `extension-contract.md`.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Không copy mã nháp chưa qua kiểm thử đầy đủ của `tools/spikes/` thẳng vào `internal/`; mọi khóa dùng trong Spike là chuỗi giả `canary`.
- **Rollback:** Tách riêng trong `tools/spikes/` và tài liệu ADR — dễ dàng loại bỏ thư mục nháp sau khi chốt chuẩn.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` và `TP-IMP-0003` đã xong; Product Owner đã cho phép tường minh việc viết mã chạy `SPIKE-01`..`04`.
- **DoD:** Hoàn thành `spike-results.md`, đóng `POC-01` & `POC-02`, vượt qua cổng `GATE-M0` với `REQUIRES_STRONG_REVIEW`.
