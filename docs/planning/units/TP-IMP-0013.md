# `TP-IMP-0013` — Hồ Thực thi Tầng 2 `wazero` Wasm Sandbox (`0` FS/Env/Socket, Trần `16 MiB`, Cắt CPU `<= 5ms/chunk` & Ngắt mạch `FMEA-05`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0013` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0004`, `TP-IMP-0010`, `TP-IMP-0011` — `GAP-01, 02` + `POC-01`)* |
| **Quy mô (`Size`)** | `L` (`go.mod`, `internal/extension/wasm/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Biên giới cô lập mã không tin cậy `TB-4` — không để sập hay treo tiến trình Go Core)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Runtime & Sandbox Security Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa hồ chứa thực thi mã mở rộng Tầng 2 triển khai giao diện chung `extension/port.ProviderRuntime` (`TP-IMP-0010`) bằng `github.com/tetratelabs/wazero` thuần Go (`CGO_ENABLED=0`) với chế độ **Từ chối Mặc định Tuyệt đối (`Zero-Ambient-Authority Sandbox`)**: `0` quyền truy cập hệ thống tệp (`FS`), `0` biến môi trường (`Env`), `0` đồng hồ thời gian thực chưa kiểm soát, `0` khả năng mở socket mạng trực tiếp (mọi yêu cầu mạng phải ủy quyền qua `BlindHostCredentialBroker`), giới hạn cứng bộ nhớ tuyến tính mỗi thể hiện `<= 16 MiB` (`256` trang Wasm), ngắt thực thi tức thì khi quá hạn CPU/thời gian và tự động cách ly (`QUARANTINED`) nếu Plugin sập lặp lại theo đúng ngưỡng `FMEA-05`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (thêm phụ thuộc thuần Go `github.com/tetratelabs/wazero`).
  - `internal/extension/wasm/runtime.go` (khởi tạo `wazero.Runtime` có `WithCloseOnContextDone(true)` và `WithMemoryLimitPages(256)` = `16 MiB`, biên dịch trước `CompiledModule` một lần khi kích hoạt gói).
  - `internal/extension/wasm/pool.go` (hồ tái sử dụng thể hiện có giới hạn đồng thời, hủy & tạo mới thể hiện ngay khi gặp `trap`/`timeout`/vượt ngưỡng trang bộ nhớ để chống rò rỉ trạng thái chéo yêu cầu).
  - `internal/extension/wasm/abi_host.go` (triển khai đầy đủ 8 phương thức của `extension/port.ProviderRuntime` đã khóa ở `TP-IMP-0002` & `POC-01`: `TransformRequest`, `TransformResponse`, `TransformStreamChunk`, `NormalizeError`, `ExtractUsage`, `InspectNativeRequest`, `BuildValidationProbe`, `VerifyValidationResult` + callback gọi HTTP mù qua `TP-IMP-0010`).
  - `internal/extension/wasm/fault_guard.go` (bộ đếm lỗi trượt theo `FMEA-05`: nếu một `extension_installation_id` gặp **`> 20` lỗi `trap/timeout/OOM` trong cửa sổ trượt `60 giây`**, tự động gọi callback `QuarantineFunc` của `TP-IMP-0011` chuyển `SM-01` sang `QUARANTINED`, phát cảnh báo `ALT-PLUGIN-TRAP` và không làm ảnh hưởng đến các Adapter khác).
- **Out-of-Scope:** Động cơ `Tier-3 Isolated Sidecar` qua `ConnectRPC` UDS/mTLS (thuộc `TP-IMP-0042` ở `v0.3`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0302`, `TP-FR-0303`, `TP-NFR-0101`, `TP-NFR-0202`, `TP-NFR-0401`, `SLO-02`, `TP-BR-0401`, `TP-BR-0404`, `CT-17`, `FZ-05`, `FMEA-05`.
- **Dependencies:** `TP-IMP-0004`, `TP-IMP-0010`, `TP-IMP-0011`.
- **Design Refs:** [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Mục 2, [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-05`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-17`, `FZ-05`).

## 4. API, Wire & Data Contracts
- **Giới hạn Tài nguyên & Thời gian Cứng (`FMEA-05` & `system-design.md`):**
  - Bộ nhớ tuyến tính tối đa mỗi thể hiện Wasm: **`16 MiB`** (`256 * 64 KiB`); các tệp đa phương thức lớn đi qua `blob_ref` của Host (`CR-05`).
  - Thời gian cắt thực thi: **`<= 15 ms`** cho hook đầu/cuối yêu cầu (`TransformRequest`, `TransformResponse`, `ExtractUsage`) và **`<= 5 ms`** cho từng mảnh luồng (`TransformStreamChunk`) để bảo vệ ngân sách `< 1 ms/chunk p95`.
  - Mọi lỗi `trap`/`timeout`/`OOM` của Wasm được bọc thành mã lỗi chuẩn **`502 extension_execution_fault`** — tuyệt đối không bao giờ làm `panic` tiến trình chính của Tanpopo.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given mô-đun hợp lệ `echo_transform.wasm` từ `internal/testkit/wasmfixtures` (`TP-IMP-0006` — không cần cài `TinyGo` trên máy chạy test), When chạy đồng thời 100 luồng biến đổi yêu cầu + 50 khung SSE mỗi luồng qua hồ `wazero`, Then `0` tranh chấp dữ liệu (`-race`), toàn bộ khung trả ra đúng thứ tự và bộ nhớ RSS ổn định.
- **`NEG-1` (`CT-17` & `FMEA-05` Infinite Loop / Memory Bomb / Quarantine Threshold):**
  1. Nạp `infinite_loop.wasm` → bị cắt đúng hạn `<= 15 ms` (`<= 5 ms` cho chunk), trả `502 extension_execution_fault`, CPU giải phóng ngay.
  2. Nạp `oom_64mib.wasm` (`memory.grow` đòi `64 MiB`) → bị từ chối ở ngưỡng `16 MiB`, thể hiện bị hủy sạch sẽ, tiến trình Core bình an vô sự.
  3. Bắn 21 lần kích hoạt `unreachable_trap.wasm` trong `60 giây` (`FakeClock`) → ở lần vượt ngưỡng `> 20`, gói tự động chuyển sang `QUARANTINED` (`FMEA-05`), trong khi các Adapter lành mạnh khác vẫn phục vụ `100%` lưu lượng bình thường.
- **`NEG-2` (`Zero Ambient Authority`):** Nạp `unapproved_wasi_import.wasm` cố nhập khẩu hàm mở tệp (`path_open`), đọc biến môi trường (`environ_get`) hoặc mở socket (`sock_open`) → trình nạp từ chối liên kết ngay từ bước khởi tạo (`unapproved_wasi_import`).
- **`NEG-3` (`Cross-Request Memory Bleed`):** Yêu cầu A của `Org 1` hoàn tất hoặc gặp lỗi giữa chừng → yêu cầu B của `Org 2` mượn thể hiện từ hồ không đọc được bất kỳ byte dữ liệu thừa nào của Yêu cầu A.

## 6. Test Commands & Required Evidence
- Kiểm tra thuần Go (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/extension/wasm/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/extension/wasm/...`
- Kiểm tra `-race` & Fuzzing ranh giới con trỏ Wasm (`FZ-05`):
  - `go test -v -race ./internal/extension/wasm/...`
  - `go test -fuzz=FuzzWasmABIBoundary -fuzztime=30s ./internal/extension/wasm/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Không bao giờ bật `wasi_snapshot_preview1` có gắn thư mục hay biến môi trường của Host; mọi con trỏ `(offset, length)` do khách Wasm trả về đều kiểm tra tràn số nguyên `uint64(offset)+uint64(length) <= uint64(mem.Size())` trước khi cắt slice.
- **Rollback:** Tắt cờ `runtime.wasm.enabled = false` trong cấu hình khẩn cấp để chỉ giữ lại các gói Khai báo Tầng 1.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (khóa `runtime.proto` & `blob_ref`), `TP-IMP-0004` (chốt `POC-01` Wasm ABI), `TP-IMP-0006` (`wasmfixtures`), `TP-IMP-0010` và `TP-IMP-0011` đã hoàn tất.
- **DoD:** Xanh toàn bộ bài kiểm thử đối kháng `CT-17`, `FMEA-05` và `FZ-05` dưới cả `CGO_ENABLED=0` và `-race`, vượt qua `REQUIRES_STRONG_REVIEW`.
