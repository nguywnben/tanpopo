# `TP-IMP-0002` — Đóng Đặc tả Lô B: Hoàn thiện `runtime.proto`, Phong bì `blob_ref`, Đủ `51` Admin OpenAPI & Đồng bộ CLI/Metric

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0002` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes`) · `EP-00` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0001` & quyết định `CR-05, CR-10` — `GAP-01, 02, 03, 07`)* |
| **Quy mô (`Size`)** | `M` (`6` tệp đặc tả hợp đồng, UX & vận hành) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Khóa hợp đồng nhị phân Wasm/Sidecar, OpenAPI & quy tắc chống OOM)* |
| **Chủ sở hữu Kỹ thuật** | `Principal API/SDK Architect + UX/DX Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Làm cho toàn bộ hợp đồng giao diện của Tanpopo biên dịch được `100%` không phải đoán mò: hoàn thiện `ExtensionRuntimeService` (`runtime.proto`), bổ sung các trường còn thiếu và cơ chế truyền tham chiếu `blob_ref` ngoài bộ nhớ Wasm cho `NormalizedInferenceRequestEnvelope`, đặc tả đầy đủ `51` endpoint `/admin/v1/*` + `/v1/responses`, `/v1/embeddings`, `/v1/models` trong `SPEC-API-01`, và thống nhất 1 cây lệnh `tanpopo` CLI, 1 bộ tên metric Prometheus, 1 chuẩn `snake_case` JSON log cùng cấu hình Vite đóng gói phông `Figtree` nội bộ cho `Air-Gap`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `docs/specifications/api-contracts.md`
  - `docs/specifications/extension-contract.md` & `docs/specifications/provider-adapter.md`
  - `docs/specifications/api-protocol-compatibility.md` & `docs/specifications/usage-billing-observability.md`
  - `docs/operations/developer-experience.md` & `docs/ux/design-system-integration.md`
- **Out-of-Scope:** Lược đồ SQL vật lý và Valkey (thuộc `TP-IMP-0003`); chưa viết code thực thi.

## 3. Requirements, Dependencies & Design References
- **Requirements & Gaps Refs:** `GAP-02`, `GAP-03`, `GAP-07`, `GAP-10`, `GAP-12`, `GAP-13` (`AUD-001`, `002`, `005`, `009`, `015`..`022`, `027`, `028`); `TP-FR-0101`..`0105`, `0301`..`0305`, `0403`, `0901`..`0904`, `TP-NFR-1001`, `1002`.
- **Dependencies:** `TP-IMP-0001`.
- **Design / Audit Refs:** [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-05`, `CR-08`, `CR-10`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-01`..`16`).

## 4. API, Wire & Data Contracts
- **Protobuf (`tanpopo.extension.v1`):**
  - Định nghĩa tường minh `ExtractUsageInput`, `TransformResponse(TransformResponseInput) returns (NormalizedInferenceResponse)`, `InspectNativeRequest(NativeRequestInput) returns (NativeRoutingHints)`.
  - Đổi `ValidateConnection` sang 2 pha (`BuildValidationProbe` → `VerifyValidationResult`) và bổ sung `HostCryptoSignSpec` (`HMAC-SHA256`, `AWS_SIGV4`, `RSA-SHA256`); cấm `field_name` chứa `Cookie`/`Set-Cookie`; loại bỏ `scoped_secret_unseal` khỏi Wasm Tầng 2.
- **Multimodal `blob_ref` & JSON Envelope:** Thêm `tools`, `tool_choice`, `parallel_tool_calls`, `response_format`, `max_output_tokens`, `temperature`, `top_p`, `stop`, `reasoning_effort`, `dimensions`; thay chuỗi base64 `> 64 KB` bằng `blob_ref://...` trước khi qua biên Wasm `16 MiB`; tách công thức ước tính token văn bản khỏi định mức token khối phương tiện.
- **OpenAPI 3.1:** Đầy đủ `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/models`, `/mcp` (phản hồi JSON-RPC chuẩn cho duyệt bất đồng bộ), `/a2a/*`, `/metrics` (`:9090`), `POST /admin/v1/playground/sessions` và toàn bộ endpoint CRUD của `SCR-01..16`.
- **Chuẩn hóa CLI, Metric, Log & UI:** Một nhị phân `tanpopo` duy nhất (bổ sung nhánh `admin`, `upgrade`, `incident`), tên metric `tanpopo_http_requests_total` / `tanpopo_proxy_overhead_seconds` / `tanpopo_config_snapshot_age_seconds` (`0` nhãn `org_id`), log JSON `snake_case`, và cấu hình Vite alias `@astryxdesign/theme-neutral/fonts.css` sang `.woff2` nội bộ + dùng đúng 29 icon ngữ nghĩa của `@astryxdesign/core/Icon`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given khối Protobuf trích xuất từ `api-contracts.md`, When chạy kiểm tra cú pháp chuẩn `proto3`, Then biên dịch thành công với `0` kiểu dữ liệu thiếu và đủ cả luồng đồng bộ lẫn streaming.
- **`AC-2`:** Given danh mục 16 màn hình trong `screen-specifications.md`, When đối chiếu từng đường dẫn trong mục `Data Contracts`, Then `100%` đường dẫn khớp chính xác phương thức HTTP, tên tham số và schema trong `api-contracts.md`.
- **`AC-3`:** Given từ điển năng lực mô hình, mã lỗi `CanonicalError`, tên metric và lệnh CLI, When quét xuyên suốt `SPEC-EXT-*`, `SPEC-CORE-*`, `SPEC-OPS-*` và `SPEC-QA-*`, Then chỉ còn duy nhất 1 cách viết `snake_case` đồng nhất.
- **`NEG-1`:** Không để bất kỳ đường đi nào truyền nguyên mảng byte đa phương thức `32 MB` vào vùng nhớ tuyến tính `16 MiB` của Wasm.
- **`NEG-2`:** Không sửa tay tệp `astryx-themes.js` hay cho phép gọi `https://fonts.googleapis.com` lúc chạy.
- **`NEG-3`:** Không cho phép `CredentialInjectInstruction` tiêm tiêu đề `Cookie` nhằm lách `DEC-004`.

## 6. Test Commands & Required Evidence
- `buf lint proto/` & `npx --yes @redocly/cli lint openapi/admin-v1.yaml openapi/dataplane-v1.yaml` *(kiểm chứng khối Protobuf `proto3` và OpenAPI `3.1` trích xuất ra tệp chuẩn đạt `0` lỗi tham chiếu `$ref` hoặc kiểu dữ liệu thiếu)*.
- `rg -n "tanpopoctl|fonts\.googleapis\.com" docs/` *(xác nhận `0` tham chiếu lạc chuẩn CLI hoặc tải phông chữ ngoài sau khi đóng `GAP-12`, `GAP-13`)*.
- Chạy kịch bản đối chiếu danh sách đủ 57 endpoint giữa `SCR-01..16` và `api-contracts.md`.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Bảo toàn tuyệt đối *Zero Raw Secrets in Wasm Memory* và *Zero External Browser Requests in Air-Gap Mode*.
- **Rollback:** Revert nguyên tử trên nhánh tài liệu nếu phát hiện xung đột lược đồ với `TP-IMP-0003`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` đã hoàn tất và Product Owner đã phê duyệt `CR-05`, `CR-08`, `CR-10`.
- **DoD:** Đóng hoàn toàn `GAP-02`, `GAP-03`, `GAP-07`, `GAP-10`, `GAP-12`, `GAP-13`; vượt qua `REQUIRES_STRONG_REVIEW`.
