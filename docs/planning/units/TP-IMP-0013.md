# `TP-IMP-0013` — Hồ Thực thi Tầng 2 `wazero` Wasm Sandbox (`0` FS/Env/Socket, Trần `16 MiB`, Cắt CPU & Ngắt mạch Lỗi Plugin)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0013` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0004`, `TP-IMP-0010`, `TP-IMP-0011` — `GAP-01, 02` + `POC-01`)* |
| **Quy mô (`Size`)** | `L` (`internal/extension/wasm/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Biên giới cô lập mã không tin cậy `TB-4` — không để sập hay treo tiến trình Go Core)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Runtime & Sandbox Security Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa hồ chứa thực thi mã mở rộng Tầng 2 sử dụng `wazero` thuần Go (`CGO_ENABLED=0`) với chế độ **Từ chối Mặc định Tuyệt đối (`Zero-Ambient-Authority Sandbox`)**: `0` quyền truy cập hệ thống tệp (`FS`), `0` biến môi trường (`Env`), `0` đồng hồ thời gian thực chưa kiểm soát, `0` khả năng mở socket mạng trực tiếp (mọi yêu cầu mạng phải ủy quyền cho Host thực hiện qua `BlindHostCredentialBroker` ở `TP-IMP-0010`), giới hạn cứng bộ nhớ tuyến tính mỗi thể hiện `<= 16 MiB` (`256` trang Wasm), ngắt thực thi tức thì khi quá hạn CPU/thời gian và tự động cách ly (`QUARANTINED`) nếu Plugin sập lặp lại (`FMEA-05`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/extension/wasm/runtime.go` (khởi tạo `wazero.Runtime` có `WithCloseOnContextDone(true)` và `WithMemoryLimitPages(256)` = `16 MiB`, biên dịch trước `CompiledModule` một lần khi bật Plugin).
  - `internal/extension/wasm/pool.go` (hồ tái sử dụng thể hiện có giới hạn đồng thời, hủy & tạo mới thể hiện ngay khi gặp `trap`/`panic`/vượt ngưỡng trang bộ nhớ để chống rò rỉ trạng thái chéo yêu cầu).
  - `internal/extension/wasm/abi_host.go` (hàm cầu nối Host ABI tối giản đã chốt tại `POC-01` / `TP-IMP-0004`: `TransformRequest`, `TransformResponse`, `TransformStreamChunk`, `NormalizeError` và callback ủy quyền gọi HTTP mù qua `TP-IMP-0010`).
  - `internal/extension/wasm/fault_guard.go` (bộ đếm lỗi trượt cho từng `extension_installation_id`: nếu gặp `>= 5` lỗi `trap/timeout/OOM` liên tiếp trong `60 giây` → tự động chuyển trạng thái `SM-01` sang `QUARANTINED`, phát cảnh báo `ALT-PLUGIN-TRAP` và không làm ảnh hưởng đến bất kỳ yêu cầu nào của các Adapter khác).
- **Out-of-Scope:** Động cơ `Tier-3 Isolated Sidecar` qua `ConnectRPC` UDS/mTLS (thuộc `TP-IMP-0042` ở `v0.3`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0302`, `TP-FR-0303`, `TP-NFR-0101`, `TP-NFR-0202`, `TP-NFR-0401`, `TP-BR-0401`, `TP-BR-0404`, `CT-17`, `FZ-05`, `FMEA-05`.
- **Dependencies:** `TP-IMP-0004`, `TP-IMP-0010`, `TP-IMP-0011`.
- **Design Refs:** [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Mục 2, [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-05`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-17`, `FZ-05`).

## 4. API, Wire & Data Contracts
- **Giới hạn Tài nguyên Cứng Của Mỗi Lần Gọi Hook (`Hard Sandbox Quotas`):**
  - Bộ nhớ tuyến tính tối đa mỗi thể hiện Wasm: **`16 MiB`** (`256 * 64 KiB`); các tệp đính kèm đa phương thức lớn đi qua tham chiếu `blob_ref` của Host (`CR-05` / `TP-IMP-0002`), không sao chép nguyên mảng byte lớn vào bộ nhớ Wasm.
  - Thời gian cắt thực thi mỗi lần biến đổi header/JSON/chunk: mặc định **`50 ms`** (cấu hình tối đa `<= 200 ms`), ngắt cưỡng bức bằng `WithCloseOnContextDone(true)` khi hết hạn hoặc khi khách hủy kết nối.
  - Bắt trọn mọi lỗi `trap` của Wasm thành lỗi có cấu trúc `502 plugin_execution_trapped` mà **không bao giờ** làm sập goroutine hay tiến trình chính của Tanpopo.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given một gói Adapter Tầng 2 biên dịch ra `.wasm` hợp lệ, When chạy đồng thời 100 luồng biến đổi yêu cầu + 50 khung SSE mỗi luồng qua hồ `wazero`, Then `0` lỗi tranh chấp dữ liệu (`-race`), toàn bộ khung trả ra đúng thứ tự và bộ nhớ RSS ổn định.
- **`NEG-1` (`CT-17` Infinite Loop / Memory Bomb / Panic Trap):**
  1. Tệp `.wasm` độc hại chạy vòng lặp vô tận `loop br 0 end` → bị cắt đúng hạn `50 ms`, trả `502 plugin_execution_timeout`, CPU giải phóng ngay lập tức.
  2. Tệp `.wasm` gọi `memory.grow` đòi `64 MiB` → bị từ chối ở ngưỡng `16 MiB`, thể hiện bị thu hồi sạch sẽ, tiến trình Core bình an vô sự.
  3. Tệp `.wasm` kích hoạt `unreachable` liên tiếp 5 lần → lần thứ 5 tự động khóa gói vào trạng thái `QUARANTINED` (`FMEA-05`), các Adapter khác vẫn phục vụ `100%` lưu lượng bình thường.
- **`NEG-2` (`Zero Ambient Authority`):** Tệp `.wasm` cố nhập khẩu hàm WASI mở tệp (`path_open`), đọc biến môi trường (`environ_get`) hoặc mở socket (`sock_open`) → trình nạp từ chối liên kết mô-đun ngay từ bước khởi tạo (`unapproved_wasi_import`).
- **`NEG-3` (`Cross-Request Memory Bleed`):** Yêu cầu A của `Org 1` hoàn tất hoặc gặp lỗi giữa chừng → yêu cầu B của `Org 2` mượn thể hiện từ hồ không đọc được bất kỳ byte dữ liệu thừa nào của Yêu cầu A trong vùng trao đổi bộ nhớ.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/extension/wasm/...`
- Fuzzing ranh giới truyền mảng byte Host ↔ Wasm: `go test -fuzz=FuzzWasmABIBoundary -fuzztime=30s ./internal/extension/wasm/...` (`FZ-05`).

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Không bao giờ bật `wasi_snapshot_preview1` đầy đủ có gắn thư mục thật hay biến môi trường thật của Host; mọi con trỏ `(offset, length)` do khách Wasm trả về đều phải kiểm tra tràn số nguyên `uint64(offset)+uint64(length) <= uint64(mem.Size())` trước khi cắt slice.
- **Rollback:** Tắt cờ cho phép chạy gói Tầng 2 (`runtime.wasm.enabled = false`) trong cấu hình khẩn cấp để chỉ giữ lại các gói Khai báo Tầng 1 mà không cần khởi động lại cụm CSDL.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (khóa `TransformResponse` & `blob_ref`), `TP-IMP-0004` (chốt `POC-01` Wasm ABI), `TP-IMP-0010` và `TP-IMP-0011` đã hoàn tất.
- **DoD:** Xanh toàn bộ bài kiểm thử đối kháng `CT-17`, `FMEA-05` và `FZ-05` dưới `-race`, vượt qua `REQUIRES_STRONG_REVIEW`.
