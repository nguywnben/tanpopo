# Đặc tả Hợp đồng Nền tảng Phần mở rộng & Mô hình Thực thi Cô lập (Extension Contract & Isolation Architecture) — Tanpopo

> **Tài liệu Đặc tả Kiến trúc Phần mở rộng (Phase ASTRA A04 — `DRAFT / ILLUSTRATIVE CANDIDATE SPEC`)**
> - **Mã tài liệu:** `SPEC-EXT-01` | **Phiên bản:** `0.1.0-step04-draft` | **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Platform Architect & Security-Minded SDK Designer
> - **Tuyên bố Trạng thái Chuẩn hóa:** Toàn bộ cấu trúc `Manifest`, lược đồ JSON/ABI, hạn mức tài nguyên và sơ đồ trình tự trong tài liệu này **mang tính minh họa thiết kế ứng viên (`ILLUSTRATIVE — PRE-ADR & PRE-POC`)**, **chưa phải chuẩn đóng băng cuối cùng** cho tới khi Product Owner phê duyệt Cổng **Gate G1/G2** và hoàn tất thử nghiệm kỹ thuật (`EXP-01`..`EXP-03`) tại [`decisions.md`](../governance/decisions.md).
> - **Truy vết Yêu cầu:** `TP-FR-0301`..`0306`, `TP-NFR-0101`..`0102`, `TP-NFR-0202`, `TP-NFR-0401`..`0403`, `TP-NFR-0902`, `TP-BR-0104`, `TP-BR-0401`..`0404`.

---

## 1. Phân loại Học Hệ sinh thái (`Canonical Extension Taxonomy`) & Ranh giới Nhân Core

Để giải quyết bài toán cốt lõi — **cộng đồng và doanh nghiệp tự thêm nhà cung cấp và tính năng mới mà không cần sửa hay phát hành lại Tanpopo Core** — hệ thống tách bạch tuyệt đối **Danh tính đóng gói (`Identity`)**, **Cơ chế thực thi (`Runtime`)** và **Vòng đời quản trị (`Lifecycle`)**:

### 1.1. Bảng Phân tách 8 Thực thể Kiến trúc

| Thực thể | Danh tính (`Identity`) | Nơi chạy (`Runtime`) | Vòng đời & Sở hữu (`Lifecycle`) | Thuộc Nhân Core hay Extension? |
|---|---|---|---|---|
| **1. `Extension Package`** | Gói tạo tác bất biến có định danh `@publisher/name`, phiên bản `SemVer` và mã băm `SHA-256`. | Không tự chạy; đóng vai trò đơn vị phân phối chứa `Manifest`, quy tắc khai báo hoặc mã thực thi cô lập. | Cấp `Instance` (`SM-01`), bật/tắt theo chính sách `Organization` (`TP-BR-0202`). | **Đơn vị mở rộng bên ngoài** (nạp động vào Core). |
| **2. `Provider Adapter`** | Năng lực (`Capability`) bên trong một `Extension Package` định nghĩa cách giao tiếp với một dòng nhà cung cấp AI. | Chạy ở Tầng 1 (`Declarative`), Tầng 2 (`Sandboxed Hook`) hoặc Tầng 3 (`Isolated Sidecar`). **Hoàn toàn không chứa khóa của khách hàng.** | Theo vòng đời của `Extension Installation` (`SM-01`). | **Mở rộng độc lập** (kể cả `Official Adapters` cũng tuân thủ đúng hợp đồng này). |
| **3. `Provider Connection`** | Bản ghi cấu hình nghiệp vụ (`conn_id`) ghép 1 `Provider Adapter` + 1 tham chiếu bí mật (`secret_ref`) + URL đích + `Data Region`. | Được quản lý và thực thi kiểm tra quyền trực tiếp bên trong **Tanpopo Core**. | Thuộc `Organization` hoặc `Project` (`SM-02`). | **Thuộc Nhân Core** (dữ liệu cấu hình sống, không phải gói mã). |
| **4. `Logical Model` & `Model Deployment`** | `Logical Model`: hồ sơ năng lực chuẩn (`model_id`).<br/>`Model Deployment`: đích vận hành thực tế (`deploy_id`) gắn 1-1 với `Provider Connection`. | Bộ định tuyến, ngắt mạch và sổ cái của **Tanpopo Core** tính toán trực tiếp trên `Model Deployment`. | Cấp `Organization` / `Project` (`SM-04`). | **Thuộc Nhân Core** (nhận siêu dữ liệu gợi ý từ Adapter nhưng quản trị tại Core). |
| **5. `Auth Driver`** | Cơ chế chứng thực với dịch vụ đích: bộ nguyên hàm chuẩn của Host hoặc trình ký tùy biến giới hạn trong `Extension Package`. | Nguyên hàm chuẩn (Bearer, OAuth 2.1, mTLS, HMAC Host-Sign) chạy trong **Host Egress Broker**; biến đổi đặc thù chạy trong Sandbox bị khóa biên. | Theo cấu hình `Secret Credential` & `Provider Connection` (`SM-02`, `SM-03`). | **Lai có kiểm soát:** Kho khóa & tiêm khóa thuộc **Core**; công thức tạo chữ ký lạ có thể mở rộng. |
| **6. `MCP Server` & `MCP Tool`** | Dịch vụ ngữ cảnh/công cụ độc lập tuân thủ giao thức **Model Context Protocol** và từng công cụ con (`<server>__<tool>`). | Chạy trên tiến trình/máy chủ đích bên ngoài (hoặc Sidecar cách ly), đi qua mặt phẳng **Core MCP Hub** (`/mcp/...`). | Đăng ký cấp `Organization`/`Project`; công cụ khóa theo `schema_hash` & `SM-06`. | **Mặt phẳng MCP riêng biệt** (`DEC-003`) — **không phải** là `Provider Adapter`. |
| **7. `Agent Endpoint` & `A2A Task`** | Tác nhân tự chủ công bố `AgentCard` và đơn vị tác vụ bất đồng bộ có trạng thái nhiều bước trên mặt phẳng **A2A**. | Tác nhân chạy độc lập bên ngoài; mặt phẳng **Core A2A Plane** (`/a2a/...`) kiểm soát ủy quyền, bước nhảy và trần ngân sách. | Đăng ký cấp `Organization`/`Project`; tác vụ theo `SM-06` & `SM-07`. | **Mặt phẳng A2A riêng biệt** (`DEC-003`) — **không lẫn** vào `Provider` hay `MCP`. |
| **8. `Routing / Policy / Exporter Extension`** | Các năng lực mở rộng đường ống: chấm điểm định tuyến tùy chỉnh, bộ lọc nội dung/PII (`Guardrail`), hoặc bộ xuất sự kiện tùy chỉnh. | Chạy trong Tầng 2 (`Sandboxed Hook`) hoặc Tầng 3 (`Isolated Sidecar`) tại các điểm móc (`Hook Stages`) xác định. | Cấp `Instance` / `Organization` (`SM-01`). | **Mở rộng độc lập**, nhưng **không được ghi đè** các rào chắn `Fail-Closed` cứng của Core. |

### 1.2. Hạt nhân Bất khả Xâm phạm của Tanpopo Core (`Non-Overridable Kernel`)

Dù cài đặt bất kỳ phần mở rộng nào, **7 thành phần sau luôn nằm cố định trong Tanpopo Core và không một Extension nào có quyền tắt hoặc đi vòng qua**:
1. Xác thực danh tính `Principal` / `Virtual Key`, phân quyền `RBAC` và cô lập cứng đa tổ chức (`TP-BR-0101`).
2. Kho bí mật gốc (`Master Secret Vault`) và Trình tiêm khóa mù tại biên mạng (`Blind Host Credential Broker` — `TP-BR-0402`).
3. Sổ cái ngân sách giữ chỗ hai pha (`Two-Phase Budget Hold & Settlement` — `TP-BR-0701`, `0702`).
4. Ràng buộc cứng vùng cư trú dữ liệu (`Hard Data Residency`) và tư thế `Fail-Closed` bắt buộc (`TP-BR-0501`, `0502`).
5. Bộ cưỡng chế cô lập Sandbox, danh sách trắng `allowed_hosts`, chống SSRF và ngắt mạch tài nguyên (`TP-BR-0401`, `0404`).
6. Cổng phê duyệt con người cho công cụ MCP `destructive` và giới hạn khuếch đại đặc quyền A2A (`TP-BR-0602`, `0604`).
7. Nhật ký kiểm toán bất biến (`Append-Only Audit Sink`) và Hệ thống công tắc ngắt khẩn cấp 4 cấp (`TP-BR-0803`).

---

## 2. Hợp đồng SDK & Giao diện Nhị phân/Dữ liệu Đa Ngôn ngữ (`Versioned Language-Agnostic Contract`)

### 2.1. Nguyên lý Không Phụ thuộc Ngôn ngữ Lập trình
- **Tách rời ngôn ngữ Core và ngôn ngữ viết Extension:** Hợp đồng giao tiếp giữa Host (Tanpopo Core) và Guest (Extension) được định nghĩa qua **lược đồ dữ liệu chuẩn hóa có phiên bản (`contract_version`)** và **biên giới giao diện trung lập** (ứng viên ưu tiên: **WebAssembly Component Model / WIT** cho Tầng 2 và **HTTP/gRPC Protobuf/JSON qua Unix Domain Socket / mTLS** cho Tầng 3).
- Nhờ đó, ngay cả khi Tanpopo Core được viết bằng Go hay Rust (`OQ-001`), tác giả cộng đồng vẫn có thể viết phần mở rộng bằng **YAML/JSON thuần** (Tầng 1), hoặc biên dịch từ **TypeScript/JavaScript, Rust, Go, Python** sang gói chuẩn (Tầng 2/Tầng 3) dùng chung một bộ kiểm thử hợp đồng.

### 2.2. Ví dụ Minh họa `Extension Manifest` (`ILLUSTRATIVE — CHƯA PHẢI CHUẨN ĐÓNG BĂNG`)

```yaml
# MINH HỌA ỨNG VIÊN (ILLUSTRATIVE CANDIDATE v0.1-draft — Chờ chốt ADR)
apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest

metadata:
  id: "@acme-cloud/regional-llm-adapter"
  name: "Acme Regional Sovereign AI Adapter"
  version: "0.2.0"
  license: "Apache-2.0"
  homepage: "https://github.com/acme-cloud/tanpopo-regional-llm"
  description: "Provider adapter for Acme Sovereign EU/JP inference endpoints."

compatibility:
  contractVersion: "0.1"              # Phiên bản hợp đồng Host-Guest yêu cầu
  coreVersionRange: ">=0.1.0 <0.3.0"  # Khoảng phiên bản Tanpopo Core tương thích

runtime:
  tier: "sandboxed_wasm"              # Enum: declarative | sandboxed_wasm | isolated_sidecar
  entrypoint: "dist/adapter.wasm"
  sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  quotas:
    maxMemoryMb: 32
    maxCpuMsPerCall: 15
    maxStreamChunkCpuMs: 2

capabilities:
  - type: "provider_adapter"
    adapterSlug: "acme-regional"
    supportedDialects:
      - "openai_compatible"
    nativePassthroughPrefix: "/native/acme"
    authMethods:
      - id: "api_key_header"
        mode: "host_blind_inject"     # Host tự gắn khóa ở biên mạng; Wasm KHÔNG thấy khóa gốc
        headerName: "X-Acme-Api-Key"
      - id: "enterprise_hmac"
        mode: "host_crypto_sign"      # Wasm gửi chuỗi cần ký; Host ký HMAC trả chữ ký

configSchema:                         # JSON Schema cho cấu hình phi bí mật trên Connection
  type: "object"
  required: ["regionCode"]
  additionalProperties: false
  properties:
    regionCode:
      type: "string"
      enum: ["eu-central-1", "ap-northeast-1"]
    customSubdomain:
      type: "string"
      pattern: "^[a-z0-9-]{1,32}$"

secretSlots:                          # Chỉ định nghĩa tên khe tham chiếu, KHÔNG chứa giá trị bí mật
  - slot: "primary_api_key"
    requiredForAuthMethods: ["api_key_header"]
  - slot: "hmac_signing_secret"
    requiredForAuthMethods: ["enterprise_hmac"]

permissions:                          # Mặc định đóng (Default-Deny); mọi quyền ngoài mục này bị chặn cứng
  networkEgress:
    allowedHosts:
      - "inference.eu-central-1.acme-ai.example"
      - "inference.ap-northeast-1.acme-ai.example"
    allowPrivateIp: false
    requireTls: true
  hooks:
    - "provider:validate_connection"
    - "provider:transform_request"
    - "provider:transform_stream_chunk"
    - "provider:extract_usage"
    - "provider:classify_error"
  payloadAccess:
    inspectRequestBody: true          # Bắt buộc để dịch schema JSON suy luận
    inspectResponseBody: true
    maxPayloadBytes: 16777216         # 16 MB
```

### 2.3. Hợp đồng Điểm vào (`Entrypoints / Hooks`), Luồng Dữ liệu, Hủy & Đo đếm

Mọi giao diện giữa Core và Extension đều truyền qua các **Phong bì Chuẩn hóa (`Normalized Envelopes`)** bất biến theo phiên bản hợp đồng:

| Nhóm giao diện (`Hook / Contract`) | Đầu vào từ Host truyền vào Guest | Đầu ra từ Guest trả về Host | Bất biến Bảo mật & Độ tin cậy |
|---|---|---|---|
| **`validate_connection(cfg, host_probe)`** | Cấu hình phi bí mật đã khớp `configSchema` + tay cầm gọi thử qua Host. | `ValidationResult { ok, detected_models[], error_code? }`. | Không nhận chuỗi khóa gốc; lệnh kiểm tra mạng vẫn bị khóa chặt trong `allowedHosts`. |
| **`transform_request(req_envelope, ctx)`** | Yêu cầu chuẩn hóa (hoặc khung passthrough), `request_id`, `capabilities_required`, cấu hình kết nối, danh sách `secret_slot_refs`. | `UpstreamHttpSpec { method, url, headers, body_mode, credential_inject_instruction }`. | Host kiểm tra lại `url` của `UpstreamHttpSpec` trước khi mở socket: nếu lệch `allowedHosts` hoặc dính IP nội bộ → hủy tức khắc (`TP-AC-SEC-02`). |
| **`transform_stream_chunk(state, raw_chunk)`** | Trạng thái luồng cục bộ có giới hạn dung lượng (`<= 64 KB`) + mảnh byte/SSE từ nhà cung cấp. | `StreamEmit { normalized_events[], state_next, usage_delta?, terminal_status? }`. | Xử lý cuốn chiếu từng mảnh (`zero full-stream buffering`); nếu gặp lỗi sau khi `chunks_emitted >= 1`, Host khóa cứng không cho phép gọi lại đích khác (`TP-BR-0503`). |
| **`on_cancel(cancel_reason)`** | Tín hiệu hủy khi `API Client` ngắt kết nối hoặc hết hạn `timeout`. | Giải phóng trạng thái tạm của phiên trong `< 50 ms`. | Việc đóng socket TCP/HTTP2 tới nhà cung cấp do **Host** cưỡng chế trực tiếp trong `< 500 ms` (`TP-FR-0104`), không phụ thuộc sự hợp tác của Plugin! |
| **`extract_usage(resp_or_stream_summary)`** | Phản hồi cuối hoặc bộ tích lũy luồng (kèm đếm byte/ký tự dự phòng của Host). | `UsageReport { input_tokens, output_tokens, cache_read, cache_write, reasoning_tokens, accuracy: EXACT \| ESTIMATED \| UNMETERED_FLAGGED }`. | Nếu Plugin trả thiếu số đo hoặc luồng đứt giữa chừng, Host tự động ép nhãn tối đa là `ESTIMATED` hoặc `UNMETERED_FLAGGED`, không bao giờ chấp nhận `EXACT = 0` giả tạo (`TP-BR-0703`). |
| **`classify_error(http_status, headers, sanitized_body)`** | Mã HTTP, tiêu đề `Retry-After` và thân lỗi đã được Host lọc sơ bộ khóa nhạy cảm. | `CanonicalError { category, retryable_pre_stream, cooldown_ms, trigger_reauth, public_code }`. | Chuẩn hóa về bảng mã lỗi chung của Tanpopo để bộ định tuyến và máy trạng thái `SM-02`/`SM-04` quyết định ngắt mạch hay chuyển mạch. |

### 2.4. Quy tắc Phiên bản hóa & Tương thích Ngược (`Compatibility & Deprecation Policy`)
- **Nguyên tắc mở rộng chỉ cộng thêm (`Additive Evolution`):** Trong cùng một nhánh hợp đồng (ví dụ `0.1` → `0.2` hoặc `1.0` → `1.x`), chỉ được phép thêm trường tùy chọn mới (`optional fields`) có giá trị mặc định an toàn; cấm đổi kiểu dữ liệu hoặc xóa trường hiện hữu.
- **Cửa sổ hỗ trợ lùi (`N-1` / `12 tháng` — `TP-NFR-0902`):** Giai đoạn `0.x`, Tanpopo Core duy trì bộ chuyển đổi tương thích (`Contract Adapter Shim`) cho tối thiểu `N-1` phiên bản hợp đồng liền trước. Từ `v1.0` trở đi, mọi thay đổi ngưng hỗ trợ (`Compulsory Deprecation`) phải công bố cảnh báo trước tối thiểu **12 tháng** kèm công cụ kiểm tra di trú `tanpopo plugin migrate-check`.

---

## 3. Ba Mô hình Thực thi Mở rộng & Ranh giới Cô lập Bảo mật (`Three Extensibility Tiers`)

> **Bất biến An ninh Tuyệt đối (`TP-NFR-0401`):** Mã nguồn từ phần mở rộng bên thứ ba hoặc chưa tin cậy **không bao giờ được chạy chung không gian tiến trình/bộ nhớ (`in-process`)** với nhân Tanpopo Core (cấm tuyệt đối `dlopen` thư viện động tùy ý, `eval()`, hoặc `import()` trực tiếp mã JS/Python không cách ly).

### 3.1. Bảng So sánh & Đánh đổi Ba Kiểu Thực thi

| Tiêu chí | **Kiểu 1: Gói Khai báo (`Declarative Adapter`)** | **Kiểu 2: Móc Thực thi Cô lập (`Sandboxed Hooks — Wasm`)** | **Kiểu 3: Tiến trình Phụ Cách ly (`Isolated Sidecar`)** |
|---|---|---|---|
| **Bản chất kỹ thuật** | Tệp dữ liệu cấu hình tĩnh (YAML/JSON + biểu thức ánh xạ an toàn không Turing-complete). | Mô-đun biên dịch chạy trong máy ảo cô lập dựa trên năng lực (WebAssembly Component Sandbox). | Dịch vụ độc lập đóng gói OCI Container / tiến trình hệ điều hành riêng giao tiếp qua UDS / mTLS loopback. |
| **Khi nào sử dụng** | ~75–80% nhà cung cấp REST/SSE tương thích họ OpenAI hoặc chỉ khác tên đường dẫn, header, bảng giá, tên model. | ~15–20% nhà cung cấp có cấu trúc JSON/SSE riêng biệt, chữ ký xác thực tùy biến hoặc bộ lọc chính sách nhanh. | ~5% tích hợp doanh nghiệp nặng: cần thư viện C/C++/PKCS#11 HSM gốc, mô hình ML cục bộ quét PII, hoặc cầu nối `stdio` MCP. |
| **Ưu điểm lớn nhất** | Bề mặt tấn công thực thi mã **bằng `0`**; độ trễ cực thấp (`< 2 ms`); tạo mới trong `< 30 phút`. | Một tệp nhị phân chạy trên mọi OS/CPU; cô lập bộ nhớ tuyến tính tuyệt đối; dễ phân phối qua Git/ZIP. | Không bị giới hạn bởi hệ sinh thái biên dịch Wasm; tận dụng được mọi thư viện gốc có sẵn của doanh nghiệp. |
| **Đánh đổi / Chi phí** | Không xử lý được các thuật toán biến đổi luồng có trạng thái phức tạp. | Cần bước biên dịch SDK sang `.wasm`; phải kiểm chứng kỹ chi phí copy bộ nhớ khi streaming tải cao (`POC-01`). | Tốn bộ nhớ nền riêng cho mỗi Sidecar; đòi hỏi hạ tầng điều phối Container/cgroups/NetworkPolicy của `Operator`. |
| **Ai được phép cài?** | `Operator` / `Org Owner` từ mọi nguồn hợp lệ sau khi duyệt quyền mạng. | `Operator` / `Org Owner` sau khi duyệt `Manifest` & hạn mức sandbox. | **Chỉ `Operator` hạ tầng** mới có quyền triển khai và phê duyệt cấu hình mạng/container cho Sidecar. |

### 3.2. Ma trận Miền Tin cậy, Quyền Hệ thống & Hạn mức Tài nguyên (`Trust Domains & Quotas`)

| Ranh giới kiểm soát | Kiểu 1: `Declarative` | Kiểu 2: `Sandboxed Hooks (Wasm)` | Kiểu 3: `Isolated Sidecar` |
|---|---|---|---|
| **Truy cập Hệ thống Tệp (`Filesystem`)** | **Không** (`0` quyền tệp). | **Không** (không gắn WASI filesystem của máy chủ; chỉ làm việc trên bộ nhớ đệm truyền vào). | Hệ thống tệp gốc chỉ đọc (`read-only rootfs`), người dùng phi đặc quyền (`non-root UID`), chỉ có thư mục tạm `/tmp` giới hạn dung lượng trong bộ nhớ. |
| **Biến Môi trường Máy chủ (`Host Env`)** | **Không** (cấm đọc `os.Environ` của Core). | **Không** (mặc định rỗng hoàn toàn). | Chỉ nhận đúng các biến cấu hình phi nhạy cảm được `Operator` khai báo tường minh cho Sidecar đó. |
| **Kết nối Mạng Ra ngoài (`Network Egress`)** | Chỉ thông qua **Host Egress Proxy** tới đúng `allowedHosts`. | Không có quyền mở socket TCP thô; mọi lệnh gọi HTTP phải đi qua hàm Host bị khóa cứng vào `allowedHosts` + bộ lọc chống SSRF. | Bắt buộc khóa bằng chính sách mạng cấp container/OS: chặn toàn bộ mạng nội bộ, chỉ cho phép đi qua cổng kiểm soát Egress của Tanpopo hoặc danh sách IP/miền đã duyệt. |
| **Giới hạn CPU & Thời gian Chờ (*Mục tiêu dự kiến*)** | `< 1 ms` ánh xạ cấu trúc. | Đếm lệnh thực thi (`fuel / epoch interruption`): mặc định `<= 15 ms CPU` mỗi lượt biến đổi khung và `<= 2 ms CPU` mỗi mảnh SSE. | Giới hạn `cgroups` CPU (ví dụ `0.5–2.0 vCPU`) + thời gian chờ cứng mỗi lệnh IPC từ Core (`<= 50 ms` cho hook chính sách, có ngắt mạch riêng). |
| **Giới hạn Bộ nhớ (*Mục tiêu dự kiến*)** | Không cấp phát vùng nhớ riêng. | Trần bộ nhớ tuyến tính cứng trên mỗi thực thể (`16 MB – 64 MB` cấu hình theo Manifest; vượt trần hủy phiên tức thì). | Trần bộ nhớ container cứng (`OOM-kill` cô lập bên trong container Sidecar, không ảnh hưởng tiến trình Core). |
| **Nhật ký Kiểm toán (`Audit`)** | Ghi vết cài đặt, thay đổi phiên bản và vi phạm miền mạng. | Ghi vết nạp gói, từng lần bị ngắt do vượt hạn mức CPU/RAM và mọi nỗ lực gọi host ngoài danh sách trắng (`TP-BR-0803`). | Ghi vết khởi tạo, chứng chỉ mTLS nội bộ, trạng thái sức khỏe và mọi lỗi ngắt mạch giao tiếp Sidecar. |

---

## 4. Các Điểm Chưa Thể Kết luận Khi Chưa Chạy PoC / Chốt ADR (`Open Technical Unknowns`)

Tuân thủ kỷ luật minh bạch kiến trúc, các chi tiết dưới đây được ghi nhận là **chưa đóng băng** và cần kiểm chứng thực nghiệm ở bước tiếp theo trước khi chốt chuẩn `1.0`:
1. **`POC-01` — Chuẩn Giao diện Wasm cụ thể (`WASI Preview 2 / Component Model WIT` vs ABI tối giản kiểu `Extism`):** Phụ thuộc vào độ trưởng thành của trình biên dịch đa ngôn ngữ (đặc biệt là TypeScript/Python sang Wasm) và độ trễ truyền mảnh SSE thực tế trong thử nghiệm `EXP-01` (`ASM-002`).
2. **`POC-02` — Ngôn ngữ Biểu thức An toàn trong Gói Khai báo (Tầng 1):** Cần đo đạc thực tế trên 10 nhà cung cấp AI khác nhau (`EXP-02`) để chọn tập toán tử ánh xạ JSON/template tối giản nhất mà vẫn đạt độ phủ `>= 75%` (`ASM-004`), tránh tự phát minh lại một ngôn ngữ lập trình phức tạp trong YAML.
3. **`POC-03` — Giao thức IPC giữa Core và Isolated Sidecar (Tầng 3):** Lựa chọn giữa `ConnectRPC / gRPC` và `HTTP/2 JSON + SSE` qua Unix Domain Socket / mTLS sẽ được chốt đồng bộ cùng ngôn ngữ Core (`OQ-001`).
