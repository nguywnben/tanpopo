# `TP-IMP-0002` — Đóng Đặc tả Lô B: Hoàn thiện `runtime.proto`, Phong bì `blob_ref`, Đủ `51` Admin OpenAPI & Đồng bộ CLI/Metric

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0002` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes`) · `EP-00` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0001` & quyết định `CR-05, CR-10` — `GAP-01, 02, 03, 07`)* |
| **Quy mô (`Size`)** | `L` (Trích xuất tệp chuẩn `proto/*`, `openapi/*` + đồng bộ `13` tệp đặc tả hợp đồng, UX, QA & vận hành) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Khóa hợp đồng nhị phân Wasm/Sidecar, OpenAPI & quy tắc chống OOM)* |
| **Chủ sở hữu Kỹ thuật** | `Principal API/SDK Architect + UX/DX Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Làm cho toàn bộ hợp đồng giao diện của Tanpopo biên dịch và kiểm định tự động được `100%` không phải đoán mò: trích xuất và hoàn thiện tệp hợp đồng máy đọc được `proto/tanpopo/extension/v1/runtime.proto` + `openapi/{dataplane-v1,admin-v1}.yaml` đồng bộ với tài liệu Markdown, bổ sung cơ chế truyền tham chiếu `blob_ref` ngoài bộ nhớ Wasm cho `NormalizedInferenceRequestEnvelope`, đặc tả đầy đủ toàn bộ `57` endpoint `/admin/v1/*` (bao gồm `51` endpoint từng thiếu ở A08), và thống nhất duy nhất **1 tên nhị phân `tanpopo`** (xóa bỏ sự phân mảnh `tanpopod`/`tanpopoctl`), 1 chuẩn tên lệnh CLI (`tanpopo db migrate-engine`), 1 quy tắc nhãn metric Prometheus và cấu hình Vite đóng gói phông `Figtree` nội bộ cho `Air-Gap`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - Hợp đồng máy đọc được (chưa chứa code thực thi): `buf.yaml`, `proto/tanpopo/extension/v1/runtime.proto`, `openapi/dataplane-v1.yaml`, `openapi/admin-v1.yaml`.
  - Đặc tả hợp đồng: `docs/specifications/api-contracts.md`, `docs/specifications/extension-contract.md`, `docs/specifications/provider-adapter.md`, `docs/specifications/api-protocol-compatibility.md`, `docs/specifications/usage-billing-observability.md`, `docs/specifications/mcp-a2a-boundaries.md`.
  - Đặc tả UX, Vận hành & QA cần đồng bộ tên nhị phân/cờ/metric: `docs/ux/screen-specifications.md`, `docs/ux/design-system-integration.md`, `docs/operations/developer-experience.md`, `docs/operations/installer-upgrades.md`, `docs/operations/monitoring-incidents.md`, `docs/quality/nonfunctional-slo.md`, `docs/quality/test-strategy.md`, `docs/reviews/unresolved-gaps.md`.
- **Out-of-Scope:** Lược đồ SQL vật lý và Valkey (thuộc `TP-IMP-0003`); chưa viết bất kỳ tệp mã nguồn thực thi `.go` hay `.ts` nào.

## 3. Requirements, Dependencies & Design References
- **Requirements & Gaps Refs:** `GAP-02`, `GAP-03`, `GAP-07`, `GAP-10`, `GAP-12`, `GAP-13` (`AUD-001`, `002`, `005`, `009`, `015`..`022`, `027`, `028`); `TP-FR-0101`..`0105`, `0301`..`0305`, `0403`, `0901`..`0904`, `TP-NFR-1001`, `1002`.
- **Dependencies:** `TP-IMP-0001`.
- **Design / Audit Refs:** [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-05`, `CR-08`, `CR-10`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-01`..`16`).

## 4. API, Wire & Data Contracts
- **Protobuf (`proto/tanpopo/extension/v1/runtime.proto`, `go_package = "github.com/nguywnben/tanpopo/gen/proto/extension/v1;extensionv1"`):**
  - Định nghĩa tường minh `ExtractUsage(ExtractUsageInput) returns (NormalizedUsage)`, `TransformResponse(TransformResponseInput) returns (NormalizedInferenceResponse)`, `TransformStreamChunk`, `NormalizeError`, `InspectNativeRequest(NativeRequestInput) returns (NativeRoutingHints)`.
  - Đổi `ValidateConnection` sang 2 pha (`BuildValidationProbe` → `VerifyValidationResult`) và bổ sung `HostCryptoSignSpec` (`HMAC_SHA256`, `AWS_SIGV4`, `RSA_SHA256`); **cấm cứng `field_name` chứa `Cookie`/`Set-Cookie` ở cả chiều đi lẫn chiều về**; loại bỏ `scoped_secret_unseal` khỏi Wasm Tầng 2.
- **Multimodal `blob_ref` & JSON Envelope:** Thêm `tools`, `tool_choice`, `parallel_tool_calls`, `response_format`, `max_output_tokens`, `temperature`, `top_p`, `stop`, `reasoning_effort`, `dimensions`; thay chuỗi base64 `> 64 KiB` bằng `blob_ref://...` trước khi qua biên Wasm `16 MiB`; tách công thức ước tính token văn bản khỏi định mức đơn vị phương tiện (`media_usage_json`).
- **OpenAPI 3.1 (`openapi/dataplane-v1.yaml` & `openapi/admin-v1.yaml`):** Đầy đủ `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/models`, `/v1/rerank`, `/native/{dialect}/{upstreamPath}`, `/mcp` (phản hồi JSON-RPC chuẩn cho duyệt bất đồng bộ), `/a2a/*`, `/metrics`, `POST /admin/v1/playground/sessions`, `POST /admin/v1/routes/explain` và toàn bộ 57 endpoint CRUD của `SCR-01..16`.
- **Chuẩn hóa CLI, Metric, Mã lỗi & UI:**
  - Duy nhất **1 nhị phân `tanpopo`** (`tanpopo serve --role=...`, `tanpopo db migrate-engine`, `tanpopo admin reset-local-owner`, `tanpopo admin verify-state`, `tanpopo budget inspect`, `tanpopo upgrade preflight`, `tanpopo bundle ...`); định dạng gói mở rộng chuẩn **`.tpkg`**, bản sao lưu **`.tpop.enc`**.
  - Danh sách nhãn metric Prometheus hữu hạn đồng nhất với `monitoring-incidents.md`: chỉ cho phép `{ org_id, project_id, adapter_id, logical_model, status_class, error_code, accuracy_tier }` (có cờ tắt nhãn `org_id`/`project_id` khi cụm có hàng nghìn dự án; **cấm tuyệt đối** `request_id`, `virtual_key_id`, `principal_id`).
  - Cấu hình Vite alias `@astryxdesign/theme-neutral/fonts.css` sang tệp `.woff2` `Figtree` nội bộ + dùng đúng danh mục icon ngữ nghĩa của `@astryxdesign/core/Icon`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `proto/tanpopo/extension/v1/runtime.proto` và `openapi/{dataplane-v1,admin-v1}.yaml`, When chạy bộ kiểm tra cấu trúc hợp đồng, Then đạt `0` lỗi tham chiếu kiểu/schema thiếu và đủ cả luồng đồng bộ lẫn streaming.
- **`AC-2`:** Given danh mục 16 màn hình trong `screen-specifications.md`, When đối chiếu từng đường dẫn trong mục `Data Contracts`, Then `100%` (`57/57`) đường dẫn khớp chính xác phương thức HTTP, tên tham số và schema trong `openapi/admin-v1.yaml` và `api-contracts.md`.
- **`AC-3`:** Given các thư mục đặc tả quy chuẩn (`docs/specifications`, `docs/operations`, `docs/ux`, `docs/quality`, `docs/architecture`), When quét tên nhị phân, tên lệnh di trú DB và định dạng `.tpkg`/`.tpop.enc`, Then đồng nhất `100%` không còn `tanpopod` hay `tanpopoctl`.
- **`NEG-1`:** Không để bất kỳ đường đi nào truyền nguyên mảng byte đa phương thức `32 MiB` vào vùng nhớ tuyến tính `16 MiB` của Wasm.
- **`NEG-2`:** Không sửa tay tệp `astryx-themes.js` hay cho phép gọi `https://fonts.googleapis.com` lúc chạy.
- **`NEG-3`:** Không cho phép `CredentialInjectInstruction` tiêm tiêu đề `Cookie` nhằm lách `DEC-004`.

## 6. Test Commands & Required Evidence
- Kiểm tra cấu trúc hợp đồng & không còn tên nhị phân cũ trên đặc tả quy chuẩn (chạy trực tiếp bằng Node.js sẵn có trên mọi OS, cộng `buf lint` trên CI):
  - `node -e "const fs=require('fs'),p=require('path');['proto/tanpopo/extension/v1/runtime.proto','openapi/dataplane-v1.yaml','openapi/admin-v1.yaml'].forEach(f=>{if(!fs.existsSync(f))throw new Error('Missing '+f)});const dirs=['specifications','architecture','ux','operations','quality'];let bad=0;const walk=d=>fs.readdirSync(d,{withFileTypes:true}).forEach(e=>{const f=p.join(d,e.name);e.isDirectory()?walk(f):f.endsWith('.md')&&/\btanpopod\b|\btanpopoctl\b/.test(fs.readFileSync(f,'utf8'))&&(console.error('Legacy binary ref:',f),bad++)});dirs.forEach(d=>walk(p.join('docs',d)));process.exit(bad?1:0)"`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Bảo toàn tuyệt đối *Zero Raw Secrets in Wasm Memory*, *No Outbound Cookie Injection* và *Zero External Browser Requests in Air-Gap Mode*.
- **Rollback:** Revert nguyên tử trên nhánh hợp đồng/tài liệu nếu phát hiện xung đột lược đồ với `TP-IMP-0003`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` đã hoàn tất và Product Owner đã phê duyệt `CR-05`, `CR-08`, `CR-10`.
- **DoD:** Đóng hoàn toàn `GAP-02`, `GAP-03`, `GAP-07`, `GAP-10`, `GAP-12`, `GAP-13` trong [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md); vượt qua `REQUIRES_STRONG_REVIEW`.
