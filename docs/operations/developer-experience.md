# Trải nghiệm Nhà phát triển: CLI, Management API, SDK, Xử lý Lỗi & Chẩn đoán (Developer Experience, CLI, SDKs & Diagnostics) — Tanpopo

> **Tài liệu Trải nghiệm Kỹ sư & Hệ sinh thái Tích hợp (Phase ASTRA A06 — `DRAFT / READY_FOR_REVIEW`)**
> - **Mã tài liệu:** `SPEC-OPS-04` | **Phiên bản:** `0.1.0-step06-draft` | **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Developer Experience (DX) Lead & SDK Architect
> - **Trạng thái quản trị:** **Đặc tả Thiết kế Đề xuất (`PROPOSED — NO CODE`)** — đồng bộ 100% với hợp đồng giao diện tại [`api-contracts.md`](../specifications/api-contracts.md) (`SPEC-API-01`), đặc tả phần mở rộng [`extension-contract.md`](../specifications/extension-contract.md) (`SPEC-EXT-01`), [`api-protocol-compatibility.md`](../specifications/api-protocol-compatibility.md) (`SPEC-EXT-06`) và [`contract-test-matrix.md`](../quality/contract-test-matrix.md) (`SPEC-QA-01`).
> - **Truy vết Yêu cầu:** `OPS-07`, `OPS-09`, `TP-FR-0101`..`0105`, `TP-FR-0305`, `TP-FR-0604`, `TP-FR-0901`, `TP-FR-0903`, `TP-FR-0904`, `TP-NFR-0902`, `TP-NFR-1001`, `TP-NFR-1002`.

---

## 1. Triết lý Trải nghiệm Kỹ sư (`Zero-Lock-In & Self-Diagnosing DX`)

Tanpopo phục vụ ba nhóm kỹ sư với ba kỳ vọng rất khác nhau:
1. **Kỹ sư Ứng dụng AI (`API Consumer`):** Không muốn cài thêm một bộ SDK độc quyền nặng nề; chỉ muốn đổi `base_url` và `api_key` trong bộ OpenAI / Anthropic / Gemini / MCP SDK sẵn có là chạy được ngay, và khi lỗi thì thông báo lỗi phải chỉ rõ cách sửa trong 10 giây.
2. **Kỹ sư Nền tảng & DevOps (`Operator`):** Mọi thao tác làm được trên Web Console đều phải tự động hóa được `100%` qua CLI (`--output json`) và Management API có kiểm soát xung đột `ETag` an toàn cho Terraform/GitOps.
3. **Tác giả Phần mở rộng (`Extension Author`):** Có thể tạo, kiểm thử hợp đồng cục bộ (`CT-01`..`24`) và đóng gói một `Provider Adapter` mới trong dưới `30 phút` bằng gói khai báo Tầng 1 hoặc Wasm SDK Tầng 2 mà không cần tải hay biên dịch mã nguồn Tanpopo Core.

---

## 2. Thiết kế Công cụ Dòng lệnh `tanpopo` CLI (`Operator & Plugin Author CLI`)

### 2.1. Quy ước Chung Của CLI (`UNIX & Automation Friendly`)
- **Định dạng đầu ra:** Mặc định in bảng căn lề sạch sẽ cho con người (tôn trọng biến môi trường chuẩn `NO_COLOR=1`); truyền `--output json` hoặc `--output yaml` để xuất dữ liệu có cấu trúc ổn định cho kịch bản `jq` / CI/CD; thông báo tiến trình luôn ghi ra `stderr` để không làm bẩn dữ liệu JSON trên `stdout`.
- **Biến môi trường chuẩn:** `TANPOPO_URL` (địa chỉ Control Plane), `TANPOPO_ADMIN_TOKEN` (mã dịch vụ quản trị CI), `TANPOPO_ORG` / `TANPOPO_PROJECT` / `TANPOPO_ENV`.
- **Bảng Mã Thoát Tất định (`Deterministic Exit Codes`):**
  - `0`: Thành công hoàn toàn.
  - `2`: Sai cú pháp cờ hoặc tham số đầu vào cục bộ.
  - `10`: Lỗi xác thực hoặc không đủ quyền `RBAC` (`401` / `403`).
  - `14`: Xung đột phiên bản cấu hình (`409 version_conflict`) hoặc hết hạn phê duyệt.
  - `22`: **Chặn tiền kiểm (`Preflight / Contract / Hash Verification Failed`)** — ví dụ gói phần mở rộng lệch `SHA-256`, đòi quyền trái phép hoặc không tương thích phiên bản nâng cấp.
  - `30`: Kiểm tra kết nối / kiểm tra khói (`Smoke Test`) tới nhà cung cấp đích thất bại.

### 2.2. Cây Lệnh Chuẩn Hóa Của `tanpopo` CLI

```text
tanpopo
├── serve                       # Khởi chạy máy chủ (--role=all|data-plane|control-plane, --config)
├── doctor                      # Chạy 8 phép kiểm tra tiền điều kiện hạ tầng, mạng, kho khóa & tệp ghim
├── login / whoami              # Đăng nhập quản trị qua OIDC Device/Browser hoặc mã dịch vụ
├── key                         # Quản lý Virtual Keys (list, create, rotate --grace-period, revoke, test)
├── connection                  # Quản lý Provider Connections (list, create, oauth-login, test, disable)
├── model / route               # Tra cứu mô hình & định tuyến (route list, route apply, route explain)
├── policy / budget             # Đồng bộ chính sách phân cấp & kiểm tra hạn mức/khoản giữ chỗ
├── approval                    # Duyệt/từ chối yêu cầu MCP destructive hoặc tăng quyền từ dòng lệnh
├── killswitch                  # Kích hoạt công tắc ngắt khẩn cấp 4 cấp (--level, --target, --abort-streams)
├── plugin                      # Dành cho cả Operator & Tác giả Phần mở rộng:
│   ├── init <name> --tier 1|2  # Khởi tạo khung dự án Adapter Khai báo (YAML) hoặc Wasm SDK
│   ├── validate                # Kiểm tra tĩnh lược đồ Manifest, quyền mạng & khe bí mật
│   ├── test                    # Chạy bộ kiểm thử hợp đồng cục bộ (CT-01..24) với máy chủ giả lập
│   ├── pack / sign             # Đóng gói .tpkg tái lập được & ký số Cosign
│   └── install / upgrade / rollback / quarantine / lock
├── bundle                      # Xuất/nhập gói ngoại tuyến cho môi trường Air-Gapped (export, verify, import)
├── backup / storage / vault    # Sao lưu mã hóa, di trú SQLite->Postgres, xoay khóa chủ Master KEK
└── support-bundle              # Thu thập gói chẩn đoán đã tự động che sạch 100% bí mật & PII
```

---

## 3. Management API & Các Mẫu Tích hợp SDK Sẵn sàng Sao chép (`SDK & Wire Snippets`)

### 3.1. Công thái học Management API (`/admin/v1/*`)
- **Chống ghi đè mù:** Mọi phản hồi `GET` của tài nguyên cấu hình đều trả về tiêu đề `ETag: "v7"`. Khi cập nhật bằng `PUT`/`PATCH`, khách gửi kèm `If-Match: "v7"` để đảm bảo an toàn tuyệt đối khi nhiều kỹ sư hoặc pipeline GitOps chạy song song.
- **Phân trang con trỏ tốc độ không đổi:** Mọi danh sách lớn dùng `?limit=100&cursor=eyJ...` trả về `{ "data": [...], "next_cursor": "...", "has_more": true }` (không dùng `OFFSET` làm chậm cơ sở dữ liệu khi lật trang sâu).
- **Chống tạo trùng:** Hỗ trợ tiêu đề `Idempotency-Key` trên các lệnh `POST` tạo khóa, tạo kết nối và kích hoạt tác vụ.

### 3.2. Tích hợp Ứng dụng Suy luận Chuẩn Hợp nhất (Không Cần Cài SDK Riêng)

#### A. Python (`openai` SDK chuẩn — Chỉ đổi `base_url` và `api_key`)
```python
from openai import OpenAI

client = OpenAI(
    base_url="https://ai-gateway.corp.example.com/v1",
    api_key="vk_tp_live_example_only_not_real",  # Virtual Key gắn cứng 1 Environment
    default_headers={
        "X-Tanpopo-Data-Region": "EU",           # Tùy chọn: ép buộc cứng vùng dữ liệu (Fail-Closed)
    },
)

stream = client.chat.completions.create(
    model="prod-assistant-default",              # Gọi vào Route thông minh hoặc Logical Model slug
    messages=[{"role": "user", "content": "Tóm tắt báo cáo vận hành tuần này."}],
    stream=True,
)
for chunk in stream:
    if chunk.choices and chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
```

#### B. TypeScript / Node.js (`openai` SDK chuẩn + Khóa chống gọi trùng)
```typescript
import OpenAI from 'openai';

const client = new OpenAI({
  baseURL: 'https://ai-gateway.corp.example.com/v1',
  apiKey: process.env.TANPOPO_VIRTUAL_KEY,
});

const response = await client.chat.completions.create(
  {
    model: 'claude-sonnet-4',
    messages: [{ role: 'user', content: 'Kiểm tra cú pháp cấu hình mạng.' }],
  },
  {
    headers: {
      'Idempotency-Key': 'job-check-20261111-001',
      'X-Tanpopo-Timeout-Ms': '30000',
    },
  },
);
```

#### C. Đường Đi Giữ Nguyên Phương Ngữ Gốc Không Mất Tính năng (`Lossless Native Passthrough`)
Dành cho các tính năng chuyên sâu đặc thù của từng hãng (như `thinking` chữ ký nguyên bản hoặc `cache_control` chi tiết của Anthropic / Gemini) mà vẫn hưởng trọn vẹn kiểm soát khóa ảo, ngân sách `micro-USD` và kiểm toán của Tanpopo:
```python
import anthropic

client = anthropic.Anthropic(
    base_url="https://ai-gateway.corp.example.com/native/anthropic",
    api_key="vk_tp_live_example_only_not_real",  # Tanpopo tự xác thực khóa ảo & tiêm khóa thật ở Blind Broker!
)

msg = client.messages.create(
    model="claude-sonnet-4-20250514",
    max_tokens=2048,
    messages=[{"role": "user", "content": "Phân tích kiến trúc hệ thống phân tán."}],
)
```

#### D. Cấu hình Kết nối Cổng Quản trị Công cụ MCP (`Dedicated MCP Plane` — Nhịp `v0.2`)
```json
{
  "mcpServers": {
    "tanpopo-governed-hub": {
      "type": "streamable-http",
      "url": "https://ai-gateway.corp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${TANPOPO_VIRTUAL_KEY}"
      }
    }
  }
}
```

#### E. Kiểm tra Khói Nhanh Từ Dòng lệnh (`Smoke Testing Key, Connection & Route`)
```bash
# 1. Kiểm tra toàn diện hiệu lực Virtual Key, hạn mức còn lại và quyền mô hình (0 tốn tiền gọi hãng ngoài):
tanpopo key test --key "$TANPOPO_VIRTUAL_KEY"

# 2. Kiểm tra kết nối thật từ máy chủ Tanpopo tới nhà cung cấp đích (qua Blind Broker):
tanpopo connection test prod-vertex-eu-01

# 3. Giả lập quyết định định tuyến khô (Dry-Run Route Explain) ngay trên terminal:
tanpopo route explain prod-assistant-default --capability vision,tools --region EU
```

---

## 4. Kiến trúc Tài liệu Hướng dẫn, API Explorer & Nhật ký Có Cấu trúc (`Docs, Explorer & Logs`)

### 4.1. Cấu trúc Điều hướng Tài liệu Dành cho Người dùng & Tác giả (`Documentation IA`)
Tài liệu hướng dẫn công khai và bản nhúng ngoại tuyến tổ chức thành **4 hành trình đọc tách biệt** (đồng bộ với mẫu `documentation` & `documentation-technical`):
1. **Quickstart (`5 Phút`):** Chạy 1 câu lệnh duy nhất → hoàn tất Setup Wizard → gửi cuộc gọi đầu tiên.
2. **Application Developer Guide:** Dùng Unified API vs Native Passthrough, xử lý streaming/hủy luồng, mã lỗi chuẩn hóa, kết nối MCP Client & A2A.
3. **Self-Host Operator & SRE Guide:** Triển khai Linux/macOS/Windows/Docker/Helm, cấu hình TLS/Nginx SSE, sao lưu `Master KEK`, nâng cấp không gián đoạn và 7 sổ tay xử lý sự cố.
4. **Extension Author SDK Reference:** Hướng dẫn viết Gói Khai báo Tầng 1 trong 15 phút, viết Plugin Wasm Tầng 2, quy tắc kho khóa mù `Blind Broker`, bộ kiểm thử hợp đồng `tanpopo plugin test` và quy trình ký số đưa lên Marketplace/Kho OCI riêng.

### 4.2. Trình Khám phá API Tương tác (`Built-in API Explorer`)
- Điểm cuối `/devtools/api-explorer` trên Web Console kết xuất trực tiếp tệp đặc tả `OpenAPI 3.1` nhúng sẵn trong nhị phân (hoạt động `100%` không cần Internet), tự động điền địa chỉ máy chủ hiện tại và cho phép thử nghiệm các lệnh chỉ đọc hoặc chạy mô phỏng `Route Explain` ngay tại chỗ.

### 4.3. Chuẩn Nhật ký Có Cấu trúc Không Lộ Bí mật (`Structured JSON Log Contract`)
Mỗi dòng nhật ký xuất ra `stdout` là một đối tượng JSON phẳng duy nhất tuân thủ chặt chẽ danh sách kiểm tra của kỹ năng `observability-and-instrumentation` (luôn có `request_id` và `entry_point`, tuyệt đối không ghép chuỗi tự do và `0` bí mật/PII):
```json
{
  "ts": "2026-11-11T16:45:12.304Z",
  "level": "WARN",
  "event": "gateway.routing.pre_stream_failover",
  "entry_point": "http_data_plane",
  "request_id": "req_01jcd8k9p2v4x7m1n6q8r0s2t4",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "org_id": "org_01jc...",
  "project_id": "prj_01jc...",
  "env_id": "env_prod",
  "route": "prod-assistant-default",
  "failed_deployment": "dep_openai_eu_01",
  "upstream_status": 429,
  "selected_fallback": "dep_vertex_eu_02",
  "overhead_ms": 1.84
}
```

---

## 5. Bảng Tra cứu Giải quyết Mã Lỗi Chuẩn Hóa (`Canonical Error Resolution Guide`)

Mọi lỗi trả về từ Tanpopo đều mang phong bì chuẩn `CanonicalErrorEnvelope` kèm `request_id` và cờ `retryable` chính xác để ứng dụng khách biết ngay có nên thử lại hay không:

| Mã Lỗi (`error.code`) | HTTP | `retryable` | Nguyên nhân Gốc rễ | Cách Khắc phục Cụ thể Cho Kỹ sư / Quản trị viên |
|---|---|---|---|---|
| `capability_not_supported` | `400` | `false` | Yêu cầu gửi tính năng (ví dụ `json_schema` nghiêm ngặt hoặc `audio`) tới mô hình/tuyến không hỗ trợ năng lực đó. | Tra cứu năng lực tại `/models`, đổi sang `Route` có năng lực tương ứng hoặc gọi qua `/native/{dialect}/...` nếu hãng chỉ hỗ trợ ở phương ngữ gốc. |
| `virtual_key_invalid_or_revoked` | `401` | `false` | Khóa ảo sai định dạng, đã hết hạn hoặc vừa bị thu hồi. | Kiểm tra biến môi trường `TANPOPO_VIRTUAL_KEY` hoặc cấp khóa mới đúng `Environment` tại `SCR-13`. |
| `model_or_tool_not_in_key_scope` | `403` | `false` | Khóa ảo hợp lệ nhưng không được cấp quyền gọi mô hình hoặc công cụ MCP này. | Yêu cầu `TEAM_ADMIN` mở rộng danh sách cho phép của khóa tại `SCR-13` hoặc kiểm tra bằng `tanpopo key test`. |
| `schema_hash_drift_blocked` | `403` | `false` | Máy chủ MCP đích đã thay đổi cấu trúc công cụ khác với mã băm đã duyệt. | Quản trị viên mở `SCR-10 (/mcp/tools)` đối chiếu bản chênh lệch Schema và bấm ghim lại nếu an toàn (`INC-06`). |
| `parameter_hash_mismatch` | `403` | `false` | Tham số gửi kèm mã phê duyệt `destructive` không khớp 100% với tham số lúc `Reviewer` bấm duyệt. | Giữ nguyên chính xác đối tượng JSON tham số đã xin duyệt hoặc tạo yêu cầu phê duyệt mới (`UJ-06`). |
| `version_conflict` | `409` | `true` *(sau khi đọc lại)* | Tiêu đề `If-Match` cũ hơn phiên bản hiện tại trên máy chủ quản trị. | Gọi `GET` lấy tài nguyên và `ETag` mới nhất, hợp nhất thay đổi rồi gửi lại. |
| `project_budget_exceeded` | `429` | `false` *(ngắn hạn)* | Tổng chi phí quyết toán cộng các khoản giữ chỗ đang mở đã chạm trần ngân sách cứng của dự án/khóa. | Kiểm tra `SCR-12 (/finops/budgets)` xem có khóa nào lặp vô hạn không, hoặc nâng hạn mức ngân sách (`INC-01`). |
| `rate_limit_exceeded` | `429` | `true` *(theo `Retry-After`)* | Chạm trần `RPM` / `TPM` / `Max Concurrent Streams` của khóa ảo trên Tanpopo. | Đọc tiêu đề `Retry-After` để lùi theo cấp số nhân (`Exponential Backoff + Jitter`). |
| `no_compliant_deployment_available` | `503` | `true` *(có trễ)* | Tất cả các đích thỏa mãn đồng thời năng lực và vùng cư trú dữ liệu (`Data Residency`) hiện đều đang ngắt mạch hoặc ngừng hoạt động. | Chạy `tanpopo route explain` để xem đích nào đang `CIRCUIT_OPEN` và đích nào bị loại do khác vùng dữ liệu (`INC-04`). |
| `mid_stream_abort` *(Sự kiện cuối luồng SSE)* | `200` *(đã mở luồng)* | `false` *(không ghép nối)* | Kết nối tới nhà cung cấp đích bị đứt sau khi đã phát ít nhất một mảnh nội dung về phía khách (`TP-BR-0503`). | Ứng dụng khách hiển thị thông báo gián đoạn rõ ràng và cho người dùng gửi yêu cầu mới từ đầu (Tanpopo không bao giờ ghép bừa phần đuôi từ mô hình khác). |
| `ambiguous_side_effect_timeout` | `504` | **`false`** | Lệnh gọi công cụ MCP có tác động ghi/xóa bị quá thời gian chờ mạng nên không chắc đích đã thực thi hay chưa (`TP-BR-0603`). | Kiểm tra trạng thái trên hệ thống đích hoặc truyền khóa chống trùng trước khi quyết định gọi lại thủ công. |

---

## 6. Gói Chẩn đoán Tự động Che Bí mật (`tanpopo support-bundle`) & Chính sách Tương thích SDK

### 6.1. Cấu trúc Gói Chẩn đoán An toàn (`Redacted Diagnostics Bundle`)
Khi cần gửi thông tin gỡ lỗi cho đội ngũ hạ tầng nội bộ hoặc đính kèm vào báo cáo lỗi mã nguồn mở trên GitHub mà **không sợ lộ khóa bí mật hay dữ liệu khách hàng**, người vận hành chạy:
```bash
tanpopo support-bundle --since 2h --output ./tanpopo-diag-20261111.tar.gz
```
Gói nén sinh ra chỉ chứa dữ liệu chẩn đoán kỹ thuật đã qua bộ lọc tẩy xóa nhiều lớp (`OPS-AC-10`):
- `doctor-report.json`: Kết quả 8 phép kiểm tra tiền điều kiện, phiên bản hệ điều hành/nhân, phiên bản nhị phân `tanpopod`, dung lượng đĩa và độ lệch đồng hồ.
- `config-redacted.yaml` & `tanpopo.lock`: Toàn bộ cây cấu hình tuyến, chính sách và danh sách mã băm gói mở rộng đang cài, trong đó mọi giá trị bí mật/URL nội bộ nhạy cảm được thay cứng bằng `[REDACTED_BY_TANPOPO]`.
- `cluster-epochs.json` & `metrics-snapshot.prom`: Ảnh chụp các bộ đếm Prometheus, độ trễ hồ Wasm, trạng thái ngắt mạch `SM-04` của các đích và độ trễ đồng bộ `ConfigSnapshot`.
- `recent-errors-redacted.jsonl`: Tối đa `500` dòng sự kiện lỗi cấu trúc gần nhất (chỉ giữ mã lỗi, độ trễ và `request_id`; không bao giờ chứa tiêu đề `Authorization`, `Cookie` hay thân văn bản yêu cầu).

### 6.2. Chính sách Phiên bản SDK & Bảo hành Tương thích Hợp đồng (`SDK & Contract Compatibility Policy`)
1. **Không Khóa Cứng Ứng dụng Khách:** Giao diện Data Plane `/v1/*` duy trì tương thích với các bản cập nhật không phá vỡ của hệ sinh thái OpenAI SDK chính thống; mọi tính năng quản trị cộng thêm của Tanpopo truyền qua tiêu đề `X-Tanpopo-*` tùy chọn.
2. **Phiên bản Hóa Ngữ nghĩa (`SemVer 2.0`) Cho Plugin SDK & Hợp đồng `tanpopo.extension.v1`:**
   - **Bản vá (`Patch x.y.Z`):** Sửa lỗi, không thay đổi bất kỳ cấu trúc JSON Schema hay hàm ABI nào.
   - **Bản phụ (`Minor x.Y.0`):** Chỉ bổ sung thêm khả năng hoặc trường tùy chọn mới (`Additive-Only`); mọi gói phần mở rộng biên dịch ở bản `x.(Y-1)` tiếp tục chạy nguyên vẹn không cần đóng gói lại.
   - **Cửa sổ Hỗ trợ Ngược & Cảnh báo Ngưng Hỗ trợ (`N-1` / `12 Tháng` — `TP-NFR-0902`, `BA-OQ-005`):** Trước cột mốc `v1.0`, bảo toàn tối thiểu `1` bản phụ liền trước (`N-1 minor`); kể từ `v1.0` trở đi, cam kết duy trì tương thích ngược tối thiểu **`12 tháng`** kèm cảnh báo sớm trên `tanpopo upgrade preflight` và màn hình `SCR-07` để cộng đồng luôn yên tâm đầu tư xây dựng hệ sinh thái Adapter lâu dài.
