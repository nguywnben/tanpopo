# `TP-IMP-0004` — Thực thi 4 Thử nghiệm Có Giới hạn Thời gian (`SPIKE-01`..`04`) & Khóa Cứng `POC-01` Wasm ABI / `POC-02`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0004` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes` — **Cổng `GATE-M0`**) · `EP-00` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0002, 0003` & giấy phép chạy PoC của Product Owner — `GAP-01`)* |
| **Quy mô (`Size`)** | `M` (`tools/spikes/*` có `go.mod` cô lập riêng + cập nhật kết quả vào `ADR-0002`, `ADR-0005`, `decisions.md`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Quyết định kiến trúc giao diện nhị phân Wasm dài hạn)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Platform Architect + Performance/Security Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Thực thi 4 bài thử nghiệm kỹ thuật ngắn hạn (`SPIKE-01`..`04` định nghĩa tại [`system-design.md`](../../architecture/system-design.md) Mục 7 và [`release-readiness.md`](../../quality/release-readiness.md) Mục 4.3) hoàn toàn ngoại tuyến trong mô-đun Go cô lập riêng (`tools/spikes/go.mod` — **không chạm tới `go.mod` gốc của `TP-IMP-0005` để 2 đơn vị chạy song song `0` xung đột**) nhằm thu thập số đo thật, từ đó khóa dứt điểm giao diện bộ nhớ Host–Wasm (`POC-01`: `Linear-Memory Ring Buffer` vs `WIT Preview 2`) và tập toán tử ánh xạ Gói Khai báo Tầng 1 (`POC-02`) trước khi xây dựng động cơ sản xuất ở `M1`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `tools/spikes/go.mod`, `tools/spikes/go.sum` (`module github.com/nguywnben/tanpopo-spikes` tách biệt hoàn toàn với nhân sản xuất; tự gắn chốt chặn chỉ cho phép mở cổng `127.0.0.1:0` ngay trong `TestMain` mà không phụ thuộc `TP-IMP-0006`).
  - `tools/spikes/spike01_declarative_test.go` .. `spike04_oauth_jcs_test.go`.
  - Báo cáo số đo thực nghiệm `docs/architecture/spike-results.md` và cập nhật trạng thái đóng `POC-01`, `POC-02` trong `docs/architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md`, `docs/architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md`, `docs/specifications/extension-contract.md` và `docs/governance/decisions.md`.
- **Out-of-Scope:** Không tạo `go.mod` ở thư mục gốc (thuộc `TP-IMP-0005`), không viết cơ sở dữ liệu sản xuất, Admin API hay Web Console; không gọi bất kỳ API AI trả phí ngoài Internet nào.

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0301`..`0303`, `TP-FR-0801`, `TP-NFR-0101`, `TP-NFR-0102`, `TP-NFR-0202`, `TP-NFR-0401`, `SLO-02`, `SLO-03`; `ASM-002`, `ASM-004`, `ASM-005`, `ASM-006`, `POC-01`, `POC-02`.
- **Dependencies:** `TP-IMP-0002`, `TP-IMP-0003`.
- **Design Refs:** [`system-design.md`](../../architecture/system-design.md)`#L316-L348`, [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`release-readiness.md`](../../quality/release-readiness.md)`#L92-L102`.

## 4. API, Wire & Data Contracts
- **`SPIKE-01`:** Biểu diễn thử trên JSON Schema 10 nhà cung cấp mẫu bằng ngữ pháp khai báo Tầng 1 (`<= 120 dòng/gói`, không vòng lặp).
- **`SPIKE-02`:** Đo trên Go `1.24+` (`CGO_ENABLED=0`) + `wazero` (trần bộ nhớ tuyến tính `16 MiB` / thể hiện theo `SPEC-SEC-02`) với `1.000` luồng SSE giả lập (`50 chunks/luồng`) qua `127.0.0.1`: so sánh `p50/p95/p99` overhead, mức RAM và tốc độ ngắt vòng lặp `while(1)`; chụp `memory dump` của Wasm xác nhận `0 byte` chứa khóa mồi `sk-tp-canary-...`.
- **`SPIKE-03`:** Bắn `50` luồng song song vào hạn mức còn đủ `5` luồng trên `SQLite WAL` (`modernc.org/sqlite`) cục bộ để đo thông lượng giữ chỗ `Pre-Hold` và hủy kết nối `< 500ms`.
- **`SPIKE-04`:** Bắn `100` luồng đồng thời vào khóa `Single-Flight Refresh` với máy chủ OAuth giả lập cục bộ và đo thời gian băm `RFC 8785 JCS` cho `300` công cụ MCP.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `SPIKE-01`, When kiểm tra lược đồ khai báo trên 10 hãng mẫu, Then tối thiểu `8/10` hãng biểu diễn trọn vẹn không cần biên dịch Wasm.
- **`AC-2`:** Given `SPIKE-02` dưới tải `1.000` luồng SSE cục bộ trên giới hạn `2 vCPU / 2 GiB RAM`, When đo giao diện Wasm được chọn (hoặc phương án lai chỉ gọi Wasm đầu/cuối luồng), Then đạt `p95` overhead `< 5.0 ms`, mảnh SSE `< 1.0 ms` và `0 byte` khóa thật trong bộ nhớ Wasm.
- **`AC-3`:** Given `SPIKE-03` và `SPIKE-04`, When bắn tải đua tranh đồng thời, Then đúng `5/50` luồng mở kết nối (còn lại nhận `429 project_budget_exceeded` với `0` vượt trần) và đúng `1/100` yêu cầu gọi `/oauth/token`.
- **`NEG-1`:** Mô-đun Wasm chạy `while(true){}` hoặc xin cấp phát `64 MiB` vượt trần `16 MiB` bị ngắt sạch không làm `panic` tiến trình chủ.
- **`NEG-2`:** Chốt chặn socket trong `TestMain` của `tools/spikes/` đánh trượt ngay nếu có bất kỳ nỗ lực kết nối nào ra ngoài `127.0.0.1/::1`.
- **`NEG-3`:** Nếu một giao diện ứng viên không đạt ngân sách `< 5 ms p95`, bắt buộc kích hoạt hướng xoay trục đã ghi trong `system-design.md` thay vì nới lỏng ngưỡng NFR ngầm.

## 6. Test Commands & Required Evidence
- Thuần Go (`CGO_ENABLED=0`) bên trong mô-đun cô lập `tools/spikes`:
  - POSIX/CI: `cd tools/spikes && CGO_ENABLED=0 go test -v -bench=. -benchmem ./...`
  - Windows PowerShell: `Push-Location tools/spikes; $env:CGO_ENABLED='0'; go test -v -bench=. -benchmem ./...; Pop-Location`
- Lưu biểu đồ phân vị độ trễ, `pprof` bộ nhớ vào `docs/architecture/spike-results.md` và cập nhật chữ ký hàm ABI chính thức vào `extension-contract.md`.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `floor-guard` ở `TP-IMP-0005` cấm tuyệt đối mọi gói trong `cmd/` hoặc `internal/` import bất kỳ gói nào từ `tools/spikes/`; mọi khóa dùng trong Spike là chuỗi giả `canary`.
- **Rollback:** Thư mục `tools/spikes/` tách biệt hoàn toàn bằng `go.mod` riêng nên không ảnh hưởng `go test ./...` ở thư mục gốc.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` và `TP-IMP-0003` đã xong; Product Owner đã cho phép tường minh việc viết mã chạy `SPIKE-01`..`04`.
- **DoD:** Hoàn thành `spike-results.md`, đóng `POC-01` & `POC-02` trong `decisions.md`, vượt qua cổng `GATE-M0` với `REQUIRES_STRONG_REVIEW`.
