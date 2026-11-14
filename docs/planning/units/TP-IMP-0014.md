# `TP-IMP-0014` — Bộ Khung Khởi tạo SDK Phần mở rộng (`plugin init/validate/pack`) & Trình Kiểm thử Băng ghi Ngoại tuyến (`plugin test`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0014` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0006`, `TP-IMP-0012`, `TP-IMP-0013` — `GAP-01, 02`)* |
| **Quy mô (`Size`)** | `M` (`cmd/tanpopo/main.go`, `sdk/*`, `internal/cli/plugin/*`, `internal/extension/testharness/*`) |
| **Cấp độ Review** | `STANDARD_REVIEW` *(Công cụ kiểm chứng hợp đồng cho tác giả phần mở rộng)* |
| **Chủ sở hữu Kỹ thuật** | `Developer Experience & Extension SDK Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Cung cấp chuỗi công cụ SDK độc lập ngôn ngữ và khung kiểm thử hợp đồng ngoại tuyến (`Offline Cassette Replay Test Harness`) gắn trực tiếp vào nhánh lệnh `tanpopo plugin` của nhị phân `tanpopo` để bất kỳ nhà phát triển cộng đồng nào cũng có thể tạo, kiểm định schema, đóng gói **`.tpkg`** và chạy toàn bộ bộ kiểm thử chuẩn (`sync`, `SSE stream`, `tool_calls`, `error normalization`, `no-secret-echo`) chỉ trong vài giây mà **không cần gọi mạng ra ngoài hay có khóa API trả phí thật**, đồng thời nâng cấp nhãn năng lực mô hình từ `DECLARED` lên `TESTED` (`SPEC-EXT-02`, `SPEC-QA-01`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (thêm thư viện phân tích cờ CLI gọn nhẹ như `spf13/cobra` mang giấy phép `Apache-2.0`).
  - `cmd/tanpopo/main.go` (gắn nhánh lệnh `tanpopo plugin init|validate|test|pack` ngay tại `M1`; các nhánh lệnh quản trị từ xa khác sẽ bổ sung ở `TP-IMP-0031`).
  - `sdk/plugin-schema/v1/extension-manifest.schema.json` & `sdk/plugin-schema/v1/cassette.schema.json`.
  - `sdk/templates/tier1-declarative/*` & `sdk/templates/tier2-wasm/*` (kèm tệp `.wasm` mẫu biên dịch sẵn từ `.wat` bên trong khung mẫu để `tanpopo plugin test` chạy được ngay dưới `< 5s` mà không bắt buộc máy trạm cài sẵn `TinyGo`).
  - `internal/extension/testharness/runner.go` (chạy băng ghi ngoại tuyến dưới `testkit/netguard` chặn `100%` socket ngoài `127.0.0.1`, phát lại luồng SSE bị chia mảnh ngẫu nhiên theo byte và quét `CanaryScanner` trên toàn bộ đầu ra).
  - `internal/cli/plugin/cmd.go` (`tanpopo plugin init --tier 1|2` kèm bí danh `declarative|wasm`, `validate`, `test`, `pack`).
- **Out-of-Scope:** Ký gói qua `Fulcio/Rekor` trực tuyến và đẩy lên `OCI Registry` (thuộc `TP-IMP-0035`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0305`, `TP-FR-0903`, `TP-BR-0201`, `TP-BR-0401`, `CT-01`..`CT-18`.
- **Dependencies:** `TP-IMP-0006`, `TP-IMP-0012`, `TP-IMP-0013`.
- **Design Refs:** [`extension-contract.md`](../../specifications/extension-contract.md) (`SPEC-EXT-01`), [`developer-experience.md`](../../operations/developer-experience.md) (`SPEC-OPS-04`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`SPEC-QA-01`).

## 4. API, Wire & Data Contracts
- **Cấu trúc Băng ghi Chuẩn (`Cassette YAML/JSON`):** Gồm `{ case_id, capability, normalized_input, upstream_recorded_exchange (status, headers, raw_body | sse_frames), expected_normalized_output | expected_canonical_error }`.
- **Bảng Mã Thoát Khớp `100%` `SPEC-OPS-04` (`developer-experience.md`):**
  - `0` = Đạt toàn bộ kiểm định và băng ghi hợp đồng.
  - `2` = Sai cú pháp cờ dòng lệnh hoặc lỗi lược đồ JSON Schema của `manifest.yaml`.
  - `14` = Vi phạm quyền/bảo mật hoặc phát hiện rò rỉ khóa mồi `CanaryScanner`.
  - `22` = Sai lệch kết quả kiểm thử hợp đồng băng ghi hoặc mã băm gói `.tpkg`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given môi trường tắt mạng hoàn toàn (`TANPOPO_TEST_HERMETIC=1`), When biên dịch `tanpopo` và chạy chu trình `init --tier 1 -> validate -> test -> pack` (và tương tự cho `--tier 2`), Then tất cả trả mã thoát `0`, tạo ra gói `.tpkg` tái lập mã băm `SHA-256` đồng nhất (timestamp `0`, thứ tự mục tệp cố định) và xuất báo cáo JSON nâng cờ năng lực lên `TESTED`.
- **`AC-2`:** Given tệp băng ghi SSE bị cắt ngang giữa ký tự UTF-8 nhiều byte và giữa từ khóa `data:`, When chạy `tanpopo plugin test`, Then bộ chạy phát hiện chính xác nếu Adapter ghép mảnh sai hoặc nuốt sự kiện cuối luồng.
- **`NEG-1`:** Adapter mẫu vô tình chép lại tiêu đề `Authorization` hoặc biến cấu hình mật vào trường `error.message` → `CanaryScanner` trong `plugin test` bắt dính ngay và trả mã thoát `14`.
- **`NEG-2`:** Manifest khai báo thiếu `contractVersion` → `tanpopo plugin validate` trả mã thoát `2`; kết quả đầu ra lệch băng ghi chuẩn → `tanpopo plugin test` trả mã thoát `22`.

## 6. Test Commands & Required Evidence
- Kiểm tra thuần Go (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./cmd/tanpopo/... ./internal/extension/testharness/... ./internal/cli/plugin/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./cmd/tanpopo/... ./internal/extension/testharness/... ./internal/cli/plugin/...`
- Kiểm tra tranh chấp dữ liệu (`-race`): `go test -v -race ./internal/extension/testharness/... ./internal/cli/plugin/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `tanpopo plugin test` luôn kích hoạt `testkit/netguard` mặc định — không bao giờ gửi lưu lượng ra Internet ngay cả khi tệp băng ghi ghi URL thật như `https://api.openai.com`.
- **Rollback:** Nhánh lệnh CLI độc lập, không tác động dữ liệu người dùng.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0006`, `TP-IMP-0012` và `TP-IMP-0013` đã hoàn tất.
- **DoD:** Chạy trọn chu trình `init -> validate -> test -> pack` cho cả mẫu `--tier 1` và `--tier 2` dưới `< 5 giây`, mã thoát khớp đúng `0 / 2 / 14 / 22` của `SPEC-OPS-04`.
