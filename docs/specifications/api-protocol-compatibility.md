# Đặc tả Tương thích Giao thức, Đa phương thức & Chuẩn hóa Lỗi Đường truyền (API & Protocol Compatibility Specification) — Tanpopo

> **Tài liệu Đặc tả Giao thức Đường truyền (Phase ASTRA A04 — `DRAFT / ILLUSTRATIVE CANDIDATE SPEC`)**
> - **Mã tài liệu:** `SPEC-EXT-06` | **Phiên bản:** `0.1.0-step04-draft` | **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Platform Architect & Security-Minded SDK Designer
> - **Tuyên bố Trạng thái Chuẩn hóa:** Cấu trúc tiêu đề HTTP, sự kiện SSE khi lỗi giữa luồng và bảng mã lỗi dưới đây **mang tính minh họa thiết kế ứng viên (`ILLUSTRATIVE — PRE-ADR`)**, làm cơ sở cho bộ kiểm thử hợp đồng tại [`contract-test-matrix.md`](../quality/contract-test-matrix.md).
> - **Truy vết Yêu cầu:** `TP-FR-0101`..`0106`, `TP-FR-0601`..`0603`, `TP-FR-0702`, `TP-FR-0802`, `TP-NFR-0101`..`0102`, `TP-NFR-1002`, `TP-BR-0503`..`0505`, `TP-BR-0703`.

---

## 1. Bề mặt Kết nối Đa Mặt phẳng cho Ứng dụng Khách (`Ingress Surface Overview`)

Để các thư viện SDK phổ biến kết nối ngay chỉ bằng cách đổi `base_url` và `api_key` mà không trộn lẫn 4 lớp nghiệp vụ (`DEC-003`), Tanpopo phân tách rõ các nhóm đường dẫn:

| Mặt phẳng | Nhóm đường dẫn minh họa | SDK / Công cụ khách tương thích trực tiếp | Cách truyền `Virtual Key` của Tanpopo |
|---|---|---|---|
| **1. Unified Provider Ingress** | `/v1/chat/completions`<br/>`/v1/responses`<br/>`/v1/embeddings`<br/>`/v1/models` | OpenAI Python/TS/Go SDK, LangChain, LlamaIndex, Vercel AI SDK, các IDE lập trình chuẩn OpenAI. | `Authorization: Bearer vk_...` |
| **2. Lossless Native Passthrough** | `/native/anthropic/v1/messages`<br/>`/native/gemini/v1beta/...`<br/>`/native/<adapter_slug>/...` | Anthropic SDK gốc, Google GenAI SDK gốc, Claude Code và các công cụ cần tính năng đặc thù hãng vừa ra mắt. | Hỗ trợ cả tiêu đề mặc định của chính SDK đó (`x-api-key` / `x-goog-api-key`) **hoặc** `x-tanpopo-virtual-key` (Core tự lột sạch khóa ảo trước khi tiêm khóa thật của nhà cung cấp!). |
| **3. MCP Governance Hub** | `/mcp` *(hoặc `/mcp/<workspace_slug>`)* | Các trình khách chuẩn **MCP Streamable HTTP** / SSE trong IDE và khung tác nhân. | `Authorization: Bearer vk_...` (hoặc OAuth 2.1 có `aud` là Tanpopo Hub). |
| **4. A2A Connectivity Plane** | `/a2a/agents` & `/a2a/tasks` | Các tác nhân tuân thủ giao thức mở **Agent2Agent (A2A)**. | `Authorization: Bearer vk_...` (hoặc khóa tác vụ ủy quyền ngắn hạn). |
| **5. Control Plane & Ops** | `/admin/v1/...`, `/healthz`, `/readyz` | `tanpopo` CLI, Web Console (Astryx) và luồng tự động hóa CI/CD. | Phiên quản trị `httpOnly` an toàn / OIDC / Admin Service Token riêng biệt (khóa suy luận `API Client` bị cấm gọi `/admin/v1/*`). |

---

## 2. Đặc tả Tương thích Chi tiết Từng Chức năng Mô hình & Giới hạn Thực tế

| Chức năng | Hành vi Chuẩn hóa trên Đường Unified (`/v1/*`) | Hành vi trên Đường Phương ngữ Gốc (`/native/*`) | Ranh giới Chặn Lỗi & Không Hỗ trợ (`Fail-Loudly`) |
|---|---|---|---|
| **Chat & Responses** | Ánh xạ hai chiều vai trò (`system`/`developer`, `user`, `assistant`, `tool`), `max_output_tokens`, `temperature`, `top_p`, `stop`. | Giữ nguyên 100% cấu trúc khối gốc của nhà cung cấp. | Nếu gửi tham số bắt buộc mà đích khai báo không hỗ trợ → trả `400 capability_not_supported` (`TP-BR-0504`). |
| **Embeddings** | Hỗ trợ đầu vào chuỗi đơn hoặc mảng chuỗi, `encoding_format` (`float` / `base64`) và tham số rút gọn chiều `dimensions` (nếu model hỗ trợ). | Giữ nguyên cấu trúc nhúng gốc của hãng. | Từ chối `400` nếu tuyến trỏ vào mô hình sinh chữ hoặc mô hình không hỗ trợ đổi `dimensions`. |
| **Hình ảnh, Âm thanh & Tài liệu (`Vision / Audio / PDF`)** | Chuẩn hóa các khối nội dung đa phương thức (`image_url`, `input_audio`, `file`/`document`); kiểm tra giới hạn kích thước thân gói tin (mặc định `<= 32 MB`) và số lượng tệp tối đa theo `Capability Profile`. | Chuyển tiếp nguyên bản cấu trúc tệp/khối đa phương thức gốc (như PDF có đánh dấu bộ nhớ đệm của Anthropic). | **Cảnh báo SSRF:** Mặc định Tanpopo chuyển URL ảnh công khai cho nhà cung cấp tự tải hoặc yêu cầu `API Client` gửi `base64`; nếu bật chế độ Gateway tải hộ sang `base64`, URL bắt buộc đi qua bộ lọc chống SSRF nghiêm ngặt (`TP-FR-0702`). |
| **Gọi hàm (`Tool Calling`)** | Chuẩn hóa danh sách `tools`, `tool_choice` (`auto`, `none`, `required`, hoặc chỉ định hàm cụ thể) và `parallel_tool_calls`; giữ đúng thứ tự mảnh delta tham số khi truyền luồng. | Giữ nguyên định nghĩa công cụ đặc thù hãng (như công cụ máy tính/trình duyệt tích hợp sẵn của hãng). | Không tự động thực thi công cụ bên trong mặt phẳng `/v1/*` (việc thực thi công cụ được quản trị tách biệt trên mặt phẳng `/mcp` để bảo toàn ranh giới 4 lớp!). |
| **Đầu ra Có Cấu trúc (`JSON / Strict Schema`)** | Phân biệt rạch ròi 2 mức: `json_object` (chế độ JSON cơ bản) và `json_schema` có `strict: true` (ép ngữ pháp nghiêm ngặt). | Giữ nguyên cấu trúc ép lược đồ gốc của nhà cung cấp. | **Nghiêm cấm hạ cấp ngầm:** Nếu yêu cầu `strict: true` mà mọi đích trong tuyến đều khai báo `strictJsonSchema = false`, trả ngay `400 capability_not_supported`. |
| **Suy luận Sâu (`Reasoning / Thinking`)** | Truyền mức độ suy luận (`reasoning_effort`), bóc tách `reasoning_tokens` vào sổ cái; bảo toàn chữ ký/khối suy nghĩ mã hóa (`encrypted_content` / `signature`) để ứng dụng gửi lại ở lượt hội thoại kế tiếp không bị lỗi. | Giữ nguyên vẹn từng sự kiện luồng `thinking_delta` và chữ ký xác thực khối suy nghĩ của hãng. | Không ép các hãng ẩn chuỗi suy nghĩ phải xuất văn bản suy nghĩ thô nếu API gốc của hãng chỉ trả số đếm token. |
| **Truyền luồng SSE (`Streaming`) & Hủy luồng** | Truyền thẳng từng mảnh sự kiện ngay khi nhận (`< 1 ms` trễ cộng thêm mục tiêu); chuyển các nhịp giữ kết nối thành dòng chú thích chuẩn `: ping\n\n`; khi người gọi ngắt kết nối, đóng ngay luồng upstream trong `< 500 ms`. | Giữ nguyên tên sự kiện SSE gốc của từng hãng (`event: content_block_delta`, v.v.). | Đứt luồng sau khi đã phát `>= 1` mảnh dữ liệu → phát khung sự kiện lỗi chuẩn `mid_stream_abort` rồi đóng kết nối, **tuyệt đối không nối mô hình dự phòng vào luồng dở** (`TP-BR-0503`). |
| **Luồng Thoại/Video Hai chiều (`Realtime WebRTC/WS`)** | **Chưa hỗ trợ trong `v0.1`–`v1.0` (`TP-FR-0106` — `Later`).** | Chưa hỗ trợ qua cổng HTTP REST. | Trả ngay `400 unsupported_protocol_upgrade` thay vì treo kết nối. |

---

## 3. Hợp đồng Chống Gọi Trùng (`Idempotency-Key`) & Tiêu đề Hạn mức (`Rate-Limit Headers`)

Áp dụng cho các yêu cầu không truyền luồng có yêu cầu chống tính phí hai lần khi mạng chập chờn (và các lệnh tạo tác vụ A2A / quản trị):
1. **Chiếm khóa nguyên tử & Bảo vệ thân gói tin (`Atomic Claim & Payload Hash Guard`):**
   - Khi `API Client` gửi tiêu đề `Idempotency-Key` (do chính ứng dụng khách sinh theo ý định nghiệp vụ), Tanpopo ghi nhận nguyên tử bộ ba `(tenant_id, virtual_key_id, idempotency_key)` kèm mã băm `SHA-256` của thân yêu cầu **trước khi** gọi ra ngoài.
   - Nếu cùng một `Idempotency-Key` được gửi lại nhưng thân yêu cầu khác mã băm ban đầu → từ chối ngay với `422 idempotency_key_reused_mismatch`.
   - Nếu yêu cầu đầu tiên vẫn đang chạy (`in_flight`) khi bản sao thứ hai ập tới → trả `409 request_in_progress` để ngăn gọi đúp sang nhà cung cấp đích.
2. **Tiêu đề Phản hồi Chuẩn hóa từ Tanpopo:**
   - `x-tanpopo-request-id`: Mã truy vết duy nhất toàn hệ thống.
   - `x-tanpopo-usage-accuracy`: `EXACT` | `ESTIMATED` | `UNMETERED_FLAGGED` (giúp ứng dụng khách biết ngay độ chính xác số đo của lượt gọi).
   - `retry-after`, `x-ratelimit-remaining-requests`, `x-ratelimit-remaining-tokens`: Chuẩn hóa thông tin hạn mức để SDK khách tự lùi nhịp thử lại hợp lý.

---

## 4. Khung Phản hồi Lỗi Đồng nhất & Bảng Mã Lỗi Máy Đọc được (`Canonical Error Taxonomy`)

Mọi lỗi phát sinh trước khi mở luồng đều trả về cùng một cấu trúc JSON duy nhất (không bao giờ chứa ngăn xếp lỗi nội bộ hay chuỗi khóa xác thực):

```json
{
  "error": {
    "code": "capability_not_supported",
    "type": "invalid_request_error",
    "message": "Target route 'prod-default' has no healthy deployment supporting 'strict_json_schema'.",
    "request_id": "req_01jc8x9p2m4n6q8r0s2t4v6w8y",
    "retryable": false
  }
}
```

*(Lưu ý khi đang truyền luồng SSE dở dang mà đứt kết nối sau mảnh đầu tiên: vì tiêu đề HTTP `200 OK` đã gửi từ trước, Tanpopo phát một sự kiện SSE cuối cùng chứa đối tượng `error` với `"code": "mid_stream_abort"` và đóng kết nối TCP ngay sau đó).*

| Mã HTTP | Mã lỗi chuẩn (`error.code`) | Cho phép Retry? | Ý nghĩa kiểm chứng được |
|---|---|---|---|
| `400` | `capability_not_supported` | Không | Yêu cầu đòi hỏi năng lực (`vision`, `audio_input`, `strict_json_schema`...) mà đích không hỗ trợ (`TP-FR-0102`). |
| `400` | `dialect_mismatch` | Không | Gọi đường dẫn `/native/<dialect>` nhưng tuyến trỏ sang triển khai khác phương ngữ (`TP-FR-0105`). |
| `400` | `delegation_depth_exceeded` | Không | Chuỗi ủy quyền liên tác nhân A2A vượt quá số bước nhảy tối đa hoặc bị lặp vòng (`TP-FR-0406`). |
| `401` | `invalid_virtual_key` | Không | Khóa ảo không tồn tại, đã hết hạn hoặc đã bị thu hồi (`SM-05`). |
| `403` | `scope_or_plane_denied` | Không | Khóa ảo gọi trái môi trường (`dev` → `prod`) hoặc gọi chéo mặt phẳng (`Provider` → `MCP`/`A2A` — `TP-BR-0104`). |
| `403` | `parameter_hash_mismatch` | Không | Tham số gọi công cụ MCP `destructive` không khớp với bản đã được con người phê duyệt (`TP-BR-0602`). |
| `409` | `version_conflict` / `request_in_progress` | Có (sau khi đọc lại) | Lưu đè cấu hình cũ (`ETag` lệch) hoặc trùng `Idempotency-Key` đang xử lý dở. |
| `422` | `idempotency_key_reused_mismatch` | Không | Tái sử dụng cùng một `Idempotency-Key` cho hai nội dung yêu cầu khác nhau. |
| `429` | `rate_limit_exceeded` / `project_budget_exceeded` | Theo `Retry-After` / Không | Chạm giới hạn tốc độ RPM/TPM hoặc chạm trần ngân sách cứng phân cấp (`TP-BR-0701`). |
| `502` | `upstream_malformed_response` | Tùy chính sách | Nhà cung cấp đích trả dữ liệu sai chuẩn hợp đồng hoặc gãy JSON gọi công cụ. |
| `503` | `no_compliant_deployment_available` | Có (sau lùi nhịp) | Không còn đích nào khỏe mạnh thỏa mãn ràng buộc vùng dữ liệu `Data Residency` (`Fail-Closed` — `TP-BR-0501`). |
| `503` | `extension_quarantined` | Không | Phần mở rộng phục vụ kết nối này đang bị cách ly an ninh khẩn cấp (`SM-01`). |
| `504` | `ambiguous_side_effect_timeout` | **CẤM tự động retry** | Lệnh gọi công cụ `mutating`/`destructive` bị quá thời gian chờ sau khi đã gửi đi; cần kiểm tra trạng thái thực tế (`TP-BR-0603`). |
