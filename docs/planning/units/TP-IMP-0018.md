# `TP-IMP-0018` — Bộ 5 `Official Reference Adapters` Cốt lõi `v0.1` (`OpenAI`, `Anthropic`, `Gemini`, `OpenAI-Compatible/vLLM`, `Ollama` + `include_usage`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0018` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-03` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0014`, `TP-IMP-0016`, `TP-IMP-0017` — `GAP-01, 09`)* |
| **Quy mô (`Size`)** | `L` (`extensions/official/openai/*`, `anthropic/*`, `gemini/*`, `openai-compatible/*`, `ollama/*`) |
| **Cấp độ Review** | `STANDARD_REVIEW` *(Tuân thủ 100% hợp đồng công khai — `0` lối tắt nội bộ trong Core)* |
| **Chủ sở hữu Kỹ thuật** | `Platform Integrations Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Xây dựng và đóng gói bộ **5 `Official Reference Adapters` đầu tiên của `MVP (v0.1)`** theo đề xuất chuẩn hóa `CR-01` (`@tanpopo/openai`, `@tanpopo/anthropic`, `@tanpopo/gemini`, `@tanpopo/openai-compatible` cho `vLLM`/`LM Studio`/TGI, và `@tanpopo/ollama`), tuân thủ kỷ luật kiến trúc *dogfooding* tuyệt đối: **cả 5 gói đều dùng đúng định dạng manifest, cơ chế Tầng 1/Tầng 2 và công cụ `tanpopo plugin test` y hệt như gói của cộng đồng** (Core không chứa bất kỳ nhánh `if provider == "openai"` đặc quyền nào), đồng thời tự động bật `stream_options.include_usage = true` (hoặc cơ chế tương đương của từng hãng) ở phía thượng nguồn để sổ cái ngân sách luôn nhận được số đếm token chính xác ở cuối luồng SSE (`CR-09`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `extensions/official/openai/*` (Unified `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings` + `/native/openai/*` + tự động tiêm `stream_options: {"include_usage": true}` khi khách không bật nhưng sổ cái cần quyết toán, rồi lọc lại khung cuối nếu khách không yêu cầu hiển thị).
  - `extensions/official/anthropic/*` (Ánh xạ hai chiều `messages`, `system`, `tool_use`/`tool_result`, ảnh/PDF `blob_ref`, chuẩn hóa khung SSE `message_start`/`content_block_delta`/`message_delta` + `/native/anthropic/v1/messages`).
  - `extensions/official/gemini/*` (Ánh xạ `contents`, `systemInstruction`, `functionDeclarations`/`functionCall`, `streamGenerateContent?alt=sse` với khóa tiêm qua header `x-goog-api-key` thay vì nối lộ trên chuỗi URL truy vấn + `/native/gemini/*`).
  - `extensions/official/openai-compatible/*` (Gói chuẩn cho `vLLM`, `SGLang`, `TGI`, `LiteLLM upstream`, cổng nội bộ tương thích OpenAI có cấu hình `base_url` + chính sách CIDR nội bộ tường minh).
  - `extensions/official/ollama/*` (Gói kết nối mô hình cục bộ `Ollama` với `billing_mode = SELF_HOSTED_INFRA`, `auth_mode = NONE | BEARER` và yêu cầu quản trị viên xác nhận tường minh quyền loopback/LAN).
  - Bộ băng ghi kiểm thử chuẩn (`cassettes/*.yaml`) cho cả 5 hãng.
- **Out-of-Scope:** `@tanpopo/aws-bedrock`, `@tanpopo/azure-openai` và năng lực `/v1/rerank` (được lên lịch tại `TP-IMP-0035` ở `M3 / v0.2` theo `CR-01`); **tuyệt đối không xây dựng bất kỳ connector nào giả mạo trình duyệt/CLI của các gói thuê bao cá nhân (`DEC-004`, `REJ-003`)**.

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0202`, `TP-FR-0301`, `TP-FR-0302`, `DEC-004`, `CR-01`, `CR-09`, `CT-06`..`CT-13`.
- **Dependencies:** `TP-IMP-0014`, `TP-IMP-0016`, `TP-IMP-0017`.
- **Design Refs:** [`provider-adapter.md`](../../specifications/provider-adapter.md) (`SPEC-EXT-02`), [`api-protocol-compatibility.md`](../../specifications/api-protocol-compatibility.md) (`SPEC-EXT-06`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-01`, `CR-09`).

## 4. API, Wire & Data Contracts
- **Không lộ Khóa Trên URL (`Zero Query-String Secret`):** Riêng với `@tanpopo/gemini`, `BlindHostCredentialBroker` luôn gắn khóa qua header `x-goog-api-key` thay vì tham số `?key=...` để ngăn nguy cơ rò rỉ khóa vào nhật ký proxy trung gian.
- **Chuẩn Hóa Đếm Token Cuối Luồng (`CR-09`):** Cả 5 Adapter đảm bảo trả về `NormalizedUsage { InputTokens, OutputTokens, CachedInputTokens, ReasoningTokens }` từ khung cuối luồng SSE khi nhà cung cấp hỗ trợ, hạn chế tối đa việc phải rơi về chế độ ước tính `ESTIMATED`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given môi trường kiểm thử khép kín `TANPOPO_TEST_HERMETIC=1`, When chạy `tanpopo plugin test` lần lượt trên cả 5 thư mục `extensions/official/*`, Then cả 5 gói đạt `100%` ca kiểm thử (`sync`, `SSE`, `tool_calls`, `vision/multimodal`, `error 401/429/500`, `native passthrough`) với `0` cuộc gọi mạng ra Internet.
- **`AC-2`:** Given khách gọi `POST /v1/chat/completions` với `stream: true` mà **không** truyền `stream_options`, When đi qua `@tanpopo/openai` hoặc `@tanpopo/openai-compatible`, Then Adapter vẫn thu được `usage` chính xác ở cuối luồng cho sổ cái Tanpopo mà không phá vỡ định dạng khung mà khách mong đợi.
- **`NEG-1`:** Quét tĩnh toàn bộ thư mục `internal/` của Core tìm hằng số tên hãng đặc quyền (`"api.openai.com"`, `"api.anthropic.com"`) ngoài danh sách kiểm thử → phải bằng `0` (chứng minh Core hoàn toàn trung lập với hãng).
- **`NEG-2`:** Gói `@tanpopo/gemini` không bao giờ tạo chuỗi URL chứa `?key=`.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./extensions/official/...`
- Chạy vòng lặp kiểm định gói: `for p in openai anthropic gemini openai-compatible ollama; do tanpopo plugin validate extensions/official/$p && tanpopo plugin test extensions/official/$p; done`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Chỉ sử dụng tài liệu API thương mại/máy chủ tự host chính thống của từng hãng (`DEC-004`); mỗi gói có `allowed_hosts` tối thiểu riêng biệt.
- **Rollback:** Từng gói `.tppkg` độc lập nhau, có thể hạ cấp hoặc tắt riêng lẻ mà không cần biên dịch lại Core.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Product Owner chốt danh sách 5 gói tại `CR-01`; `TP-IMP-0014`, `TP-IMP-0016` và `TP-IMP-0017` đã hoàn tất.
- **DoD:** Cả 5 gói đóng gói `.tppkg` thành công, đạt chứng nhận `VERIFIED` trên bộ băng ghi chuẩn.
