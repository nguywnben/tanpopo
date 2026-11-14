# `TP-IMP-0014` — Bộ Khung Khởi tạo SDK Phần mở rộng (`plugin init/validate/pack`) & Trình Kiểm thử Băng ghi Ngoại tuyến (`plugin test`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0014` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0006`, `TP-IMP-0012`, `TP-IMP-0013` — `GAP-01, 02`)* |
| **Quy mô (`Size`)** | `M` (`sdk/plugin-schema/*`, `internal/cli/plugin/*`, `internal/extension/testharness/*`) |
| **Cấp độ Review** | `STANDARD_REVIEW` *(Công cụ kiểm chứng hợp đồng cho tác giả phần mở rộng)* |
| **Chủ sở hữu Kỹ thuật** | `Developer Experience & Extension SDK Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Cung cấp chuỗi công cụ SDK độc lập ngôn ngữ và khung kiểm thử hợp đồng ngoại tuyến (`Offline Cassette Replay Test Harness`) để bất kỳ nhà phát triển cộng đồng nào cũng có thể tạo, kiểm định schema, đóng gói `.tppkg` và chạy toàn bộ bộ kiểm thử chuẩn (`sync`, `SSE stream`, `tool_calls`, `error normalization`, `no-secret-echo`) chỉ trong vài giây mà **không cần gọi mạng ra ngoài hay có khóa API trả phí thật**, đồng thời nâng cấp nhãn năng lực mô hình từ `DECLARED` lên `TESTED` (`SPEC-EXT-02`, `SPEC-QA-01`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `sdk/plugin-schema/v1/tanpopo-plugin.schema.json` & `sdk/plugin-schema/v1/cassette.schema.json` (JSON Schema chính thức cho manifest gói mở rộng và tệp băng ghi kiểm thử).
  - `sdk/templates/declarative-provider/*` & `sdk/templates/wasm-provider-tinygo/*` (2 bộ khung khởi tạo mẫu tối giản).
  - `internal/extension/testharness/runner.go` (chạy băng ghi ngoại tuyến dưới `testkit/netguard` chặn `100%` socket ngoài `127.0.0.1`, phát lại luồng SSE bị chia mảnh ngẫu nhiên theo byte và quét `CanaryScanner` trên toàn bộ đầu ra).
  - `internal/cli/plugin/cmd.go` (`tanpopo plugin init`, `tanpopo plugin validate`, `tanpopo plugin test`, `tanpopo plugin pack`).
- **Out-of-Scope:** Ký gói qua `Fulcio/Rekor` trực tuyến và đẩy lên `OCI Registry` (thuộc `TP-IMP-0035`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0305`, `TP-FR-0903`, `TP-BR-0201`, `TP-BR-0401`, `CT-01`..`CT-18`.
- **Dependencies:** `TP-IMP-0006`, `TP-IMP-0012`, `TP-IMP-0013`.
- **Design Refs:** [`extension-contract.md`](../../specifications/extension-contract.md) (`SPEC-EXT-01`), [`developer-experience.md`](../../operations/developer-experience.md) (`SPEC-OPS-04`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`SPEC-QA-01`).

## 4. API, Wire & Data Contracts
- **Cấu trúc Băng ghi Chuẩn (`Cassette YAML/JSON`):** Gồm `{ case_id, capability, normalized_input, upstream_recorded_exchange (status, headers, raw_body | sse_frames), expected_normalized_output | expected_canonical_error }`.
- **Bảng Mã Thoát Cố định (`CLI Exit Codes`):** `0` = Đạt toàn bộ; `2` = Sai cú pháp / lỗi JSON Schema manifest; `3` = Vi phạm chính sách bảo mật / phát hiện lộ khóa canary; `4` = Sai lệch kết quả kiểm thử hợp đồng (`SPEC-OPS-04`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given môi trường tắt mạng hoàn toàn (`TANPOPO_TEST_HERMETIC=1`), When chạy `tanpopo plugin init my-llm --tier=declarative && tanpopo plugin validate ./my-llm && tanpopo plugin test ./my-llm && tanpopo plugin pack ./my-llm`, Then tất cả trả mã thoát `0`, tạo ra gói `.tppkg` tái lập mã băm `SHA-256` đồng nhất (reproducible archive timestamp `0`) và xuất báo cáo JSON nâng cờ năng lực lên `TESTED`.
- **`AC-2`:** Given tệp băng ghi SSE bị cắt ngang giữa ký tự UTF-8 nhiều byte và giữa từ khóa `data:`, When chạy `tanpopo plugin test`, Then bộ chạy phát hiện chính xác nếu Adapter ghép mảnh sai hoặc nuốt sự kiện cuối luồng.
- **`NEG-1`:** Adapter mẫu vô tình chép lại header `Authorization` hoặc biến cấu hình mật vào trường `error.message` → `CanaryScanner` trong `plugin test` bắt dính ngay lập tức và trả mã thoát `3`.
- **`NEG-2`:** Manifest khai báo thiếu `core_api_version` hoặc đòi quyền không xác định → `tanpopo plugin validate` trả mã thoát `2` kèm đường dẫn JSON Pointer chính xác tới dòng lỗi.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/extension/testharness/... ./internal/cli/plugin/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `tanpopo plugin test` luôn kích hoạt `testkit/netguard` mặc định — không bao giờ gửi lưu lượng ra Internet ngay cả khi tệp băng ghi ghi URL thật như `https://api.openai.com`.
- **Rollback:** Nhánh lệnh CLI độc lập, không tác động dữ liệu người dùng.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0006`, `TP-IMP-0012` và `TP-IMP-0013` đã hoàn tất.
- **DoD:** Chạy trọn chu trình `init -> validate -> test -> pack` cho cả mẫu Tầng 1 và Tầng 2 dưới `< 5 giây`, mã thoát đúng đặc tả.
