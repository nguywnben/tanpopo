# Danh mục Đặc tả Tính năng Toàn diện theo Phân hệ (Feature Catalog) — Tanpopo

> **Tài liệu Định nghĩa Sản phẩm (Phase ASTRA A03 — Gate G2: `READY_FOR_REVIEW` | `DRAFT — UNLOCKED SCOPE`)**
> - **Phiên bản:** `0.1.0-step03-draft`
> - **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Principal Requirements Engineer & Product Manager
> - **Trạng thái quản trị:** **Bản nháp chưa khóa phạm vi (`DRAFT`)**. Mỗi phân hệ dưới đây đặc tả đầy đủ 14 thuộc tính kiểm chứng được và tách bạch rõ hành vi mà nhà cung cấp đích (`Upstream Provider`) hỗ trợ nguyên bản, hỗ trợ một phần hoặc không hỗ trợ.

---

## Domain 01: Gateway, Giao thức & Đa phương thức (`gateway-protocols`)

### `FT-01` — Điểm cuối Suy luận Hợp nhất & Truyền luồng Đa phương thức (`Unified Inference & Multimodal Streaming`)
- **Actors:** `API Client`, `Developer`, `Agent`.
- **Preconditions:** Có ít nhất một `Model Deployment` ở trạng thái `HEALTHY` được ánh xạ qua `Route`; người gọi có `Virtual Key` ở trạng thái `CREATED_ACTIVE` hoặc `ROTATING_DUAL_VALID`.
- **Trigger:** Gửi yêu cầu HTTP/HTTPS tới các đường dẫn chuẩn hóa (`/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/models`).
- **Inputs:** Tiêu đề xác thực `Virtual Key`, tên mô hình/tuyến đích, danh sách thông điệp (văn bản, ảnh URL/base64, tệp âm thanh/tài liệu), cấu hình công cụ (`tools`), định dạng đầu ra (`response_format`/`json_schema`), cờ `stream`, `Idempotency-Key` tùy chọn.
- **Outputs:** Phản hồi JSON đồng bộ hoặc luồng sự kiện `text/event-stream` (SSE) chuẩn hóa kèm mã truy vết `x-tanpopo-request-id`, số đo token (nếu có) và khung lỗi cấu trúc đồng nhất khi thất bại.
- **States:** Tham chiếu `SM-04` (Deployment Health) & `SM-07` (Budget Reservation/Settlement).
- **Business Rules:** `TP-BR-0101`, `TP-BR-0103`, `TP-BR-0503` (cấm fallback sau partial stream), `TP-BR-0504` (kiểm tra năng lực), `TP-BR-0505` (hủy luồng).
- **Permissions:** Quyền `inference:invoke` gắn trên `Virtual Key` đối với `Environment` và danh sách mô hình/tuyến tương ứng.
- **Success Flow:** Xác thực khóa → Kiểm tra Policy & Năng lực mô hình → Khóa tạm ngân sách (`RESERVED_HOLD`) → Chuyển đổi qua `Provider Adapter` → Truyền luồng từng mảnh độ trễ thấp → Quyết toán sổ cái (`SETTLED_EXACT`) → Ghi siêu dữ liệu truy vết.
- **Error & Negative Flows:**
  - Tham số yêu cầu năng lực mà đích không hỗ trợ (ví dụ gửi tệp âm thanh hoặc `json_schema` nghiêm ngặt tới mô hình chỉ nhận chữ thuần) → Trả ngay `400 capability_not_supported` trước khi gọi ra ngoài.
  - Nhà cung cấp đứt kết nối sau khi đã phát `>= 1` mảnh nội dung → Gửi sự kiện lỗi kết thúc luồng `mid_stream_abort`, quyết toán `SETTLED_ESTIMATED`, **không** tự động gọi mô hình dự phòng ghép nối tiếp.
  - Người gọi đóng kết nối giữa chừng → Hủy ngay kết nối phía nhà cung cấp trong vòng `< 500 ms` và giải phóng phần ngân sách dư.
- **Limits:** Kích thước thân gói tin tối đa mặc định `32 MB` (cấu hình được theo tuyến cho đa phương thức); giới hạn số mảnh công cụ song song và `Max Request Duration` cấu hình được (mặc định `300 giây` cho mô hình suy luận sâu).
- **Expected UX:** `API Client` chỉ cần đổi `base_url` và `api_key` trên các thư viện OpenAI SDK tiêu chuẩn mà không phải cài thư viện riêng; tiêu đề phản hồi trả kèm tên `Model Deployment` thực sự đã xử lý (nếu chính sách dự án cho phép hiển thị).
- **Compatibility & Phân biệt Hành vi Nhà cung cấp:**
  - *Hỗ trợ chuẩn tương đương:* Văn bản, streaming SSE, gọi hàm cơ bản (`function calling`), `temperature`/`max_tokens`.
  - *Khác biệt giữa các hãng (Adapter phải khai báo rõ trong `Capability Profile`):* Khối suy luận nội tâm (`reasoning`/`thinking` tokens — hãng trả nội dung chữ, hãng chỉ trả bản tóm tắt mã hóa hoặc chỉ trả số đếm token), kích thước ảnh/PDF tối đa, kiểm tra `json_schema` nghiêm ngặt so với gợi ý mềm.
  - *Không hỗ trợ qua giao diện Chat REST chuẩn:* Luồng thoại/video hai chiều thời gian thực độ trễ cực thấp qua WebSocket/WebRTC (`Realtime Media`) — được tách thành yêu cầu lộ trình sau `v1.0` (`TP-FR-0106` — `Later`), tuyệt đối không hứa hẹn giả tạo là chạy chung qua `/v1/chat/completions`.
- **SRS & AC:** `TP-FR-0101`..`0104`, `TP-FR-0106` (`Later`), `TP-AC-F0101`..`F0104`.

### `FT-02` — Đường đi Giữ nguyên Phương ngữ Gốc Không Mất mát (`Lossless Native Dialect Passthrough`)
- **Actors:** `API Client` (ví dụ công cụ lập trình chuyên biệt gọi thẳng chuẩn Anthropic `/v1/messages` hoặc chuẩn Gemini gốc), `Developer`.
- **Preconditions:** Đã bật `Provider Connection` tương ứng với họ giao thức gốc đó và cấp quyền trên `Virtual Key`.
- **Trigger:** Gửi yêu cầu tới nhóm đường dẫn phương ngữ gốc được định danh rõ (ví dụ `/native/anthropic/v1/messages`, `/native/gemini/v1beta/...`).
- **Inputs:** Nguyên bản thân gói tin và tiêu đề đặc thù tính năng của nhà cung cấp đó (ví dụ điểm đánh dấu `cache_control` nhiều tầng, khối `thinking` chi tiết, tài liệu đính kèm gốc) kèm `Virtual Key` của Tanpopo.
- **Outputs:** Nguyên bản cấu trúc phản hồi/luồng SSE của nhà cung cấp đích (sau khi đã lọc bỏ tiêu đề nội bộ nhạy cảm).
- **States:** `SM-04`, `SM-07`.
- **Business Rules:** `TP-BR-0101`, `TP-BR-0304`, `TP-BR-0501`, `TP-BR-0503`, `TP-BR-0702`.
- **Permissions:** Quyền `inference:invoke:native` trên phạm vi tương ứng.
- **Success Flow:** Xác thực `Virtual Key`, lột bỏ khóa ảo khỏi gói tin đi tiếp, kiểm tra ngân sách & vùng dữ liệu, tiêm khóa thật của `Provider Connection` tại biên mạng, truyền nguyên vẹn gói tin không ép qua bộ dịch trung gian và trích xuất số đo token/cache từ phản hồi gốc để quyết toán.
- **Error & Negative Flows:** Chỉ cho phép chuyển mạch dự phòng giữa các `Model Deployment` **cùng chia sẻ chính xác phương ngữ gốc đó** (ví dụ từ Anthropic trực tiếp sang Anthropic trên đám mây cùng phiên bản schema); từ chối `400 dialect_mismatch` nếu tuyến trỏ nhầm sang nhà cung cấp khác họ giao thức.
- **Limits:** Không biến đổi các trường mở rộng mới do nhà cung cấp vừa tung ra trong ngày.
- **Expected UX:** Các công cụ chuyên sâu giữ được `100%` tính năng đặc thù mới nhất của hãng AI trong khi tổ chức vẫn kiểm soát trọn vẹn khóa, ngân sách và nhật ký.
- **Compatibility:** Chỉ áp dụng cho các `Provider Adapter` khai báo hỗ trợ phương ngữ gốc tương ứng.
- **SRS & AC:** `TP-FR-0105`, `TP-AC-F0105`.

---

## Domain 02: Nhà cung cấp, Mô hình, Triển khai & Thông tin Xác thực (`provider-credentials`)

### `FT-03` — Quản lý Chuỗi 4 Thực thể Mô hình (`Adapter → Connection → Logical Model → Deployment`)
- **Actors:** `Organization Owner`, `Team Admin`, `Operator`.
- **Preconditions:** Đã có ít nhất một `Provider Adapter` hợp lệ được kích hoạt.
- **Trigger:** Tạo mới hoặc cập nhật `Provider Connection` và bật các `Model Deployment` trên Web Console, CLI hoặc tệp cấu hình.
- **Inputs:** Định danh `Provider Adapter`, tham chiếu `Secret Credential`, địa chỉ điểm cuối (cho mô hình tự chạy hoặc đám mây riêng), vùng địa lý (`Data Region`), danh sách `Model Deployment` kèm biểu giá ghi đè và ngưỡng timeout riêng (như `first_byte_timeout` dài cho `Ollama`/`vLLM` nạp mô hình nguội).
- **Outputs:** Thực thể `Provider Connection` và các `Model Deployment` sẵn sàng nhận lưu lượng định tuyến kèm trạng thái sức khỏe.
- **States:** `SM-02` (Provider Connection) & `SM-04` (Model Deployment Health).
- **Business Rules:** `TP-BR-0102`, `TP-BR-0104`, `TP-BR-0501`.
- **Permissions:** `connection:manage` (cấp `Organization` hoặc `Project`); `connection:share` (chỉ `Organization Owner`).
- **Success Flow:** Lưu cấu hình → Chạy kiểm tra xác thực thực tế → Khởi tạo danh mục `Model Deployment` gắn đúng vùng dữ liệu và bảng giá → Chuyển sang `ACTIVE` / `HEALTHY`.
- **Error & Negative Flows:** Cấm xóa cứng `Provider Connection` khi vẫn đang được tham chiếu bởi `Route` đang bật; lỗi nạp mô hình chậm trên máy chủ GPU cục bộ không bị đánh nhầm là chết mạng nếu nằm trong `cold_start_timeout` đã khai báo.
- **Limits:** Hỗ trợ cấu hình nhiều `Provider Connection` độc lập cho cùng một `Provider Adapter` (ví dụ 5 tài khoản thương mại khác nhau cho 5 phòng ban).
- **Expected UX:** Phân tách rõ trên giao diện: cài *Adapter* một lần, tạo nhiều *Connections* theo tài khoản, và định tuyến tới từng *Deployment* cụ thể.
- **Compatibility:** Tương thích cả API thương mại đám mây lẫn máy chủ suy luận tự vận hành (`vLLM`, `SGLang`, `Ollama`, `llama.cpp`).
- **SRS & AC:** `TP-FR-0201`..`0203`, `TP-AC-F0201`..`F0202`.

### `FT-04` — Kho Bí mật Một chiều, OAuth 2.1 Được Cấp phép & Định danh Liên kết (`Secret Vault & Authorized OAuth`)
- **Actors:** `Organization Owner`, `Team Admin`, `Operator`, `External IdP`.
- **Preconditions:** Kho bí mật chủ của hệ thống đã được khởi tạo khóa bảo vệ (`WF-01`).
- **Trigger:** Nhập khóa mới, khởi chạy luồng cấp quyền OAuth 2.1 được cấp phép, gắn định danh đám mây (IAM/OIDC) hoặc xoay vòng khóa.
- **Inputs:** Giá trị khóa bí mật (chỉ ghi một chiều) hoặc cấu hình OAuth hợp lệ (`authorization_endpoint`, `token_endpoint`, `client_id` đăng ký hợp pháp, danh sách `scopes` được nhà cung cấp cho phép, PKCE).
- **Outputs:** Bản ghi `Secret Credential` đã mã hóa (chỉ hiển thị 4 ký tự cuối hoặc dấu vân tay ẩn danh), trạng thái phiên OAuth (`TOKEN_VALID`) và cơ chế tự động làm mới đơn luồng (`single-flight`).
- **States:** `SM-02`, `SM-03`.
- **Business Rules:** `TP-BR-0102`, `TP-BR-0301`..`0304`, `TP-BR-0803`.
- **Permissions:** `secret:write`, `secret:bind`, `secret:revoke` (không tồn tại quyền `secret:read_plaintext` cho bất kỳ người dùng nào trên UI/API).
- **Success Flow:** Mã hóa ngay khi nhận → Kiểm tra hiệu lực → Khi sắp hết hạn chỉ cử 1 luồng đại diện xin cấp lại token mới → Thay thế liền mạch không làm rớt truy vấn đang chạy.
- **Error & Negative Flows:**
  - Nhận lỗi `invalid_grant` hoặc `401` xác thực → Chuyển ngay sang `AUTH_EXPIRED_REAUTH_REQUIRED`, phát cảnh báo quản trị và ẩn hoàn toàn chi tiết nhạy cảm khỏi phản hồi trả về cho `API Client`.
  - Mọi nỗ lực cấu hình lách điều khoản thuê bao cá nhân qua phiên trình duyệt đều không được hỗ trợ trong các gói chính thức (`TP-BR-0302`).
- **Limits:** Thời gian khóa đồng bộ làm mới token tối đa `10 giây`; không lưu nhật ký giá trị bí mật ở bất kỳ mức độ gỡ lỗi nào.
- **Expected UX:** Cảnh báo sớm trước khi khóa có thời hạn sắp hết hiệu lực; luồng kết nối lại OAuth 1 cú nhấp khi bị thu hồi quyền.
- **Compatibility:** Hỗ trợ khóa tĩnh (API Key/Bearer), OAuth 2.1 (Authorization Code + PKCE, Client Credentials), mTLS, Cloud Workload Identity/IAM và nguyên hàm xác thực doanh nghiệp tùy chỉnh qua SDK. **Không đồng nghĩa với việc mọi tài khoản OAuth người dùng cuối của các hãng AI đều được phép dùng làm cổng API chung.**
- **SRS & AC:** `TP-FR-0204`..`0206`, `TP-AC-F0203`, `TP-AC-SEC-01`, `TP-AC-SEC-03`, `TP-AC-SEC-06`.

---

## Domain 03: Động cơ Phần mở rộng & Marketplace (`plugin-marketplace`)

### `FT-05` — Vùng Cô lập Phần mở rộng Hai Tầng & Trình Tiêm Khóa Mù (`Isolated Plugin Engine & Blind Credential Broker`)
- **Actors:** `Operator`, `Extension Publisher` (gián tiếp qua gói mã).
- **Preconditions:** Phần mở rộng đã vượt qua kiểm tra `Manifest` và được phê duyệt quyền (`SM-01`: `ENABLED_ACTIVE`).
- **Trigger:** Có yêu cầu lưu lượng đi qua một `Provider Adapter` hoặc bộ lọc chính sách/xác thực mở rộng.
- **Inputs:** Dữ liệu yêu cầu đã chuẩn hóa theo phiên bản `Plugin Contract` và bối cảnh cấu hình phi bí mật của kết nối.
- **Outputs:** Yêu cầu/phản hồi biến đổi hợp lệ hoặc quyết định chính sách; thông tin xác thực thật được tiêm tự động tại biên mạng của Host.
- **States:** `SM-01`.
- **Business Rules:** `TP-BR-0401`, `TP-BR-0402`, `TP-BR-0404`.
- **Permissions:** Thực thi nghiêm ngặt theo bản chụp quyền đã duyệt (`Approved Permission Grant`: chỉ mở kết nối HTTPS tới đúng danh sách `allowed_hosts`, không đọc tệp chủ, không đọc biến môi trường).
- **Success Flow:**
  - *Tầng 1 (Gói Khai báo — Declarative Pack):* Ánh xạ tiêu đề, đường dẫn, tên mô hình, bảng giá và luồng SSE trực tiếp không thực thi mã tùy ý.
  - *Tầng 2 (Gói Thực thi Cô lập — Sandbox Plugin):* Thực thi mã biến đổi trong vùng cô lập giới hạn bộ nhớ và thời gian CPU; khi gửi yêu cầu ra ngoài tới tên miền thuộc `allowed_hosts`, Host tự đính kèm khóa xác thực thật mà không lộ chuỗi khóa vào bộ nhớ plugin.
- **Error & Negative Flows:**
  - Plugin cố gọi tên miền lạ hoặc địa chỉ nội bộ cấm (SSRF tới `127.0.0.1`, `169.254.169.254`...) → Chặn tức khắc, hủy yêu cầu, ghi `AuditEvent` mức nghiêm trọng và cách ly `QUARANTINED` theo cấu hình.
  - Plugin treo vòng lặp vô hạn hoặc ngốn vượt trần bộ nhớ → Ngắt cưỡng bức phiên thực thi đơn lẻ đó mà không ảnh hưởng các luồng khác trên cổng.
- **Limits:** Giới hạn bộ nhớ mặc định trên mỗi thực thể sandbox và giới hạn thời gian xử lý mỗi sự kiện biến đổi (`< 50 ms` cho biến đổi khung tiêu đề/JSON thông thường).
- **Expected UX:** Minh bạch tuyệt đối trên Console/CLI về loại gói (Khai báo an toàn hay Mã cô lập) và danh sách chính xác tên miền mà gói được phép chạm tới.
- **Compatibility:** Phiên bản hóa `Plugin Contract` rõ ràng (`contract_version`); kiểm tra tương thích trước khi nạp và trước khi nâng cấp Core (`SM-08`).
- **SRS & AC:** `TP-FR-0301`..`0303`, `TP-NFR-0401`..`0403`, `TP-AC-F0301`, `TP-AC-SEC-02`.

### `FT-06` — Trình Cài đặt Đa Nguồn, Khóa Phiên bản, SDK & Bộ Kiểm thử Hợp đồng (`Multi-Source Installer, Registry & Contract Harness`)
- **Actors:** `Operator`, `Organization Owner`, `Reviewer`, `Extension Publisher`.
- **Preconditions:** Nguồn phân phối truy cập được (hoặc tệp gói ngoại tuyến hợp lệ).
- **Trigger:** Cài đặt mới, kiểm tra cập nhật, quay lui (`Rollback`) hoặc chạy lệnh kiểm thử phát triển plugin (`plugin test`).
- **Inputs:** Địa chỉ nguồn (Marketplace công khai, kho Git kèm tag/commit, URL/tệp nén ZIP/archive, Private Registry nội bộ), chữ ký số/mã băm SHA-256.
- **Outputs:** Bản cài đặt được ghi nhận vào tệp khóa `tanpopo.lock`, nhãn nguồn gốc (`official` | `verified` | `unverified-external`), hoặc báo cáo kết quả kiểm thử tương thích hợp đồng từ các tệp băng ghi mẫu (`Cassettes`).
- **States:** `SM-01`.
- **Business Rules:** `TP-BR-0201`..`0203`, `TP-BR-0302`, `TP-BR-0403`, `TP-BR-0801`.
- **Permissions:** `extension:install`, `extension:approve_permissions`, `extension:quarantine`.
- **Success Flow:** Tải gói → Xác minh mã băm & chữ ký → Gắn nhãn `Trust Tier` trung thực → Hiển thị bảng đối chiếu quyền hạn → Duyệt quyền → Kích hoạt êm không dừng hệ thống.
- **Error & Negative Flows:**
  - Bản cập nhật mới đòi thêm quyền (`Permission Escalation`) → Chặn tự động kích hoạt, giữ ở `UPDATING_PENDING_RECONSENT` chờ duyệt lại, lưu lượng hiện hành vẫn đi qua bản cũ ổn định (`WF-11`).
  - Lỗi sau khi lên bản mới → Lệnh `Rollback` đưa lưu lượng quay về bản ghim trước đó trong vài giây.
- **Limits:** Kích thước gói tải lên giới hạn an toàn (chống tấn công giải nén phình đại — *zip bomb*).
- **Expected UX:** Tác giả bên ngoài khởi tạo khung mẫu, ghi/phát lại kiểm thử hợp đồng và đóng gói chỉ bằng một công cụ CLI duy nhất; người vận hành thấy rõ phần chênh lệch quyền hạn (`Permissions Diff`) như trên trình duyệt hiện đại.
- **Compatibility:** Hỗ trợ xuất trọn bộ các gói đã ghim thành tệp đóng gói ngoại tuyến (`Offline Bundle`) cho môi trường mạng kín.
- **SRS & AC:** `TP-FR-0304`..`0307`, `TP-AC-F0302`..`F0304`.

---

## Domain 04: Mặt phẳng MCP & Mặt phẳng Liên tác nhân A2A (`mcp-a2a`)

### `FT-07` — Quản trị Công cụ MCP, Khóa Dấu vân tay Cấu trúc & Phê duyệt Tác động Phụ (`MCP Governance Plane`)
- **Actors:** `MCP Client`, `Agent`, `Team Admin`, `Reviewer`, `MCP Server`.
- **Preconditions:** `MCP Server` đã được đăng ký, phân loại mức độ rủi ro công cụ (`read-only`, `mutating`, `destructive`) và phê duyệt dấu vân tay cấu trúc (`schema_hash`).
- **Trigger:** `MCP Client` gửi yêu cầu khám phá (`tools/list`, `resources/list`, `prompts/list`) hoặc gọi thực thi (`tools/call`) qua điểm cuối MCP Plane của Tanpopo.
- **Inputs:** Yêu cầu giao thức MCP (Streamable HTTP / SSE) kèm `Virtual Key` của người gọi.
- **Outputs:** Danh sách công cụ đã lọc gọn đúng quyền (hỗ trợ phân trang chuẩn không cắt cụt ở mốc 100 công cụ), kết quả gọi công cụ hoặc yêu cầu chờ phê duyệt `ApprovalRequest`.
- **States:** `SM-06` (Approval & Task Lifecycle).
- **Business Rules:** `TP-BR-0104`, `TP-BR-0601`..`0603`, `TP-BR-0803`.
- **Permissions:** `mcp:discover` và quyền cấp từng công cụ `mcp:invoke:<server>:<tool>`.
- **Success Flow:** Lọc chỉ hiển thị công cụ được phép (giúp tiết kiệm cửa sổ ngữ cảnh cho mô hình) → Kiểm tra khớp `schema_hash` → Tiêm thông tin xác thực đích phía sau → Nếu là công cụ `destructive`, kiểm tra/tạo `ApprovalRequest` gắn băm tham số → Thực thi đúng 1 lần và ghi nhật ký kiểm toán.
- **Error & Negative Flows:**
  - `MCP Server` sửa ngầm mô tả công cụ (`schema_hash` thay đổi) → Khóa ngay công cụ đó không cho gọi và phát cảnh báo an ninh (`MCP Rug-Pull Blocked`).
  - Lệnh gọi công cụ `mutating`/`destructive` bị quá hạn thời gian mạng → **Cấm tự động thử lại**, trả mã `ambiguous_side_effect_timeout`.
  - Cố sửa tham số sau khi đã được duyệt hoặc quá hạn phê duyệt → Từ chối `403`.
- **Limits:** Giới hạn kích thước phản hồi của công cụ MCP trả về để tránh làm tràn bộ nhớ ngữ cảnh của tác nhân.
- **Expected UX:** Tác nhân trong các IDE/CLI phổ biến kết nối qua 1 URL duy nhất trên Tanpopo thay vì phải cấu hình và phân phát 10 khóa bí mật dịch vụ nội bộ xuống từng máy cá nhân của lập trình viên.
- **Compatibility:** Tuân thủ chuẩn giao thức **MCP Streamable HTTP** (và tương thích luồng SSE kế thừa).
- **SRS & AC:** `TP-FR-0401`..`0404`, `TP-AC-F0401`..`F0403`, `TP-AC-SEC-08`.

### `FT-08` — Mặt phẳng Kết nối Liên Tác nhân A2A (`A2A Connectivity & Bounded Delegation Plane`)
- **Actors:** `Agent` (tác nhân gọi & tác nhân đích), `Team Admin`, `Organization Owner`.
- **Preconditions:** `Agent Endpoint` đích đã được đăng ký kèm hồ sơ năng lực `Agent Card` và vùng tin cậy hợp lệ.
- **Trigger:** Khám phá `Agent Card` hoặc khởi tạo/cập nhật/hủy tác vụ bất đồng bộ trên điểm cuối A2A Plane.
- **Inputs:** Yêu cầu tác vụ chuẩn A2A (JSON-RPC / truyền luồng cập nhật trạng thái), mã truy vết liên thông, trần ngân sách con và thời gian sống tối đa của tác vụ.
- **Outputs:** Trạng thái vòng đời tác vụ liên tác nhân, kết quả bàn giao (`Artifacts`) và bản ghi chi phí/kiểm toán gộp theo chuỗi ủy quyền.
- **States:** `SM-06`, `SM-07`.
- **Business Rules:** `TP-BR-0104`, `TP-BR-0501`, `TP-BR-0604` (chống khuếch đại đặc quyền).
- **Permissions:** `a2a:discover`, `a2a:delegate:<agent_id>`.
- **Success Flow:** Xác minh quyền ủy nhiệm → Đảm bảo phạm vi dữ liệu và hạn mức ngân sách cấp cho tác nhân con không vượt quá phần còn lại của tác nhân cha → Định tuyến thông điệp tác vụ bất đồng bộ và ghi vết xuyên suốt (`Correlation ID`).
- **Error & Negative Flows:** Chặn chuỗi ủy quyền vòng tròn lặp vô tận giữa các tác nhân (`Max Delegation Hop Depth`, mặc định `<= 5` bước) và từ chối mọi yêu cầu nâng quyền vượt phạm vi gốc.
- **Limits:** Giới hạn độ sâu chuỗi ủy quyền và thời gian sống tối đa của tác vụ treo.
- **Expected UX:** Quan sát trực quan cây ủy quyền liên tác nhân và tổng chi phí cộng dồn của một tác vụ phức tạp trên một màn hình truy vết duy nhất.
- **Compatibility:** Thiết kế bám sát đặc tả mở **Agent2Agent (A2A) Protocol**.
- **SRS & AC:** `TP-FR-0405`..`0406`, `TP-AC-F0404`.

---

## Domain 05: Xác thực, Đa người thuê & Phân quyền (`iam-tenancy`)

### `FT-09` — Phân cấp Tổ chức (`Org → Project → Env`), RBAC & Vòng đời Khóa Ảo (`Tenancy, RBAC & Virtual Keys`)
- **Actors:** `Personal Admin`, `Organization Owner`, `Team Admin`, `Developer`, `Operator`, `External IdP`.
- **Preconditions:** Hệ thống đã hoàn tất khởi tạo quản trị gốc.
- **Trigger:** Đăng nhập quản trị, phân quyền thành viên, tạo `Project`/`Environment` hoặc phát hành/xoay vòng/thu hồi `Virtual Key`.
- **Inputs:** Thông tin định danh người dùng (tài khoản cục bộ có băm mật khẩu mạnh hoặc liên kết SSO/OIDC doanh nghiệp), vai trò phân quyền, cấu hình phạm vi của `Virtual Key` (môi trường, danh sách mô hình/tuyến/công cụ MCP được phép, hạn mức RPM/TPM, trần ngân sách, hạn sử dụng).
- **Outputs:** Phiên quản trị an toàn hoặc chuỗi `Virtual Key` (chỉ hiển thị duy nhất 1 lần lúc tạo, về sau chỉ lưu mã băm mật mã).
- **States:** `SM-05` (Virtual Key Lifecycle).
- **Business Rules:** `TP-BR-0101`..`0104`, `TP-BR-0803`.
- **Permissions:** Kiểm soát phân cấp chặt chẽ theo ma trận 12 tác nhân tại [`business-entity-map.md`](../requirements/business-entity-map.md).
- **Success Flow:** Xác thực danh tính → Áp đúng phạm vi `Organization`/`Project`/`Environment` → Hỗ trợ xoay vòng hai khóa song hành (`ROTATING_DUAL_VALID`) không làm gián đoạn dịch vụ hoặc thu hồi khẩn cấp tức thì.
- **Error & Negative Flows:** Chặn mọi truy cập chéo tổ chức hoặc dùng khóa `development` gọi tài nguyên `production`; phân biệt chế độ xoay khóa êm và thu hồi an ninh khẩn cấp cắt luồng đang mở.
- **Limits:** Giới hạn số lần đăng nhập sai liên tiếp để chống dò mật khẩu; khóa ảo bắt buộc lưu ở dạng mã băm một chiều (ngay cả khi lộ cơ sở dữ liệu cũng không đọc ngược được `Virtual Key` đang lưu hành).
- **Expected UX:** Chế độ cá nhân tự động ẩn bớt sự phức tạp đa phòng ban nhưng không phá vỡ mô hình dữ liệu chuẩn khi người dùng mở rộng thành nhóm lớn về sau.
- **Compatibility:** Hỗ trợ xác thực cục bộ độc lập không cần mạng ngoài và tích hợp chuẩn OIDC cho tổ chức.
- **SRS & AC:** `TP-FR-0501`..`0504`, `TP-AC-F0501`..`F0503`, `TP-AC-SEC-07`.

---

## Domain 06: Định tuyến Nhận thức Năng lực & Chịu lỗi (`routing-resilience`)

### `FT-10` — Bộ Định tuyến Nhận thức Năng lực, Chuyển mạch Trước Luồng & Mô phỏng Khô (`Capability Router & Explain Simulator`)
- **Actors:** `Team Admin`, `Organization Owner`, `Developer`, `API Client`.
- **Preconditions:** Đã cấu hình ít nhất một `Route` gắn với một hoặc nhiều `Model Deployment`.
- **Trigger:** Yêu cầu suy luận thực tế đi tới `Route` hoặc quản trị viên chạy lệnh chẩn đoán mô phỏng khô (`/v1/route/explain` / CLI / Console).
- **Inputs:** Điều kiện truy vấn (năng lực cần thiết, kích thước ngữ cảnh ước tính, ràng buộc vùng dữ liệu, chiến lược chọn đích: thứ tự ưu tiên cố định, phân tải trọng số hoặc tối ưu chi phí trong nhóm đạt chuẩn).
- **Outputs:** `Model Deployment` hợp lệ được chọn để thực thi, hoặc bản giải trình chi tiết từng đích được chọn/bị loại kèm lý do chính xác trong chế độ mô phỏng khô.
- **States:** `SM-04`.
- **Business Rules:** `TP-BR-0501`..`0505`.
- **Permissions:** `route:manage` (cấu hình); `route:explain` (kiểm tra thử cho `Developer` & `Team Admin`).
- **Success Flow:** Lọc bỏ các đích vi phạm vùng dữ liệu (`Data Residency`) → Lọc bỏ các đích thiếu năng lực yêu cầu hoặc không đủ cửa sổ ngữ cảnh → Loại các đích đang mở ngắt mạch (`CIRCUIT_OPEN_UNHEALTHY`) → Chọn đích tối ưu; nếu gặp lỗi mạng/`429`/`5xx` **trước khi phát mảnh nội dung đầu tiên**, tự động chuyển sang đích hợp lệ kế tiếp trong vài mili-giây.
- **Error & Negative Flows:** Không còn đích nào vừa khỏe mạnh vừa tuân thủ vùng dữ liệu → Trả ngay `503 no_compliant_deployment_available` (`Fail-Closed`); tuyệt đối không chuyển mạch sau khi đã phát `Partial Stream`.
- **Limits:** Giới hạn số bước nhảy chuyển mạch tối đa trên một yêu cầu (mặc định `<= 3` đích) để ngăn bão thử lại dây chuyền.
- **Expected UX:** Trình mô phỏng khô chỉ rõ trên giao diện lý do tại sao đích A được chọn và đích B bị loại mà không tốn một đồng chi phí API nào.
- **Compatibility:** Hoạt động thống nhất cho cả mô hình đám mây lẫn mô hình tự chạy nội bộ.
- **SRS & AC:** `TP-FR-0601`..`0604`, `TP-AC-F0601`..`F0603`.

---

## Domain 07: Động cơ Chính sách & Bảo mật Vận hành (`policy-security`)

### `FT-11` — Kiểm soát Chính sách Đường truyền, Giới hạn Tốc độ & Phòng vệ Biên mạng (`Policy Engine & SSRF Defense`)
- **Actors:** `Organization Owner`, `Team Admin`, `Operator`.
- **Preconditions:** Các chính sách mặc định cấp hệ thống và tổ chức đã nạp.
- **Trigger:** Mọi yêu cầu quản trị cấu hình URL ngoài hoặc mọi yêu cầu lưu lượng đi qua 3 mặt phẳng (`Provider`, `MCP`, `A2A`).
- **Inputs:** Bối cảnh chủ thể, siêu dữ liệu yêu cầu, địa chỉ mạng đích, tốc độ gọi hiện thời.
- **Outputs:** Quyết định `ALLOW`, `DENY` (kèm mã lý do chuẩn) hoặc `REQUIRE_APPROVAL`.
- **States:** `SM-06`.
- **Business Rules:** `TP-BR-0202`, `TP-BR-0501`, `TP-BR-0502`, `TP-BR-0601`..`0604`.
- **Permissions:** `policy:manage` (`Organization Owner` / `Team Admin` trong phạm vi con không được nới lỏng quy tắc cứng của cấp trên).
- **Success Flow:** Áp giới hạn tốc độ đa chiều (RPM, TPM, số luồng mở đồng thời tối đa), kiểm tra danh sách mô hình/công cụ cho phép, kiểm tra vùng dữ liệu và chặn các địa chỉ IP nội bộ/siêu dữ liệu đám mây (`169.254.169.254`, loopback) đối với mọi URL bên ngoài trừ khi `Operator` mở danh sách trắng tường minh cho máy chủ mô hình nội bộ.
- **Error & Negative Flows:** Mọi lỗi ở khâu xác thực, kiểm tra vùng dữ liệu, quyền plugin hoặc trần ngân sách cứng đều bắt buộc dừng an toàn (`Fail-Closed`).
- **Limits:** Thời gian đánh giá chính sách nội tại tối thiểu hóa để không phá ngân sách độ trễ chung.
- **Expected UX:** Chính sách cấp tổ chức đóng vai trò "hàng rào trần" rõ ràng để các nhóm sản phẩm tự chủ bên trong mà không sợ vi phạm quy định chung.
- **Compatibility:** Cho phép gắn thêm các bộ lọc nội dung chuyên sâu (như ẩn thông tin cá nhân PII) dưới dạng `Extension` tùy chọn.
- **SRS & AC:** `TP-FR-0701`..`0703`, `TP-NFR-0402`, `TP-AC-SEC-04`, `TP-AC-SEC-05`.

### `FT-12` — Nhật ký Kiểm toán Bất biến & Hệ thống Công tắc Ngắt Khẩn cấp (`Immutable Audit & Kill-Switches`)
- **Actors:** `Organization Owner`, `Operator`, `Reviewer`, `Team Admin`.
- **Preconditions:** Hệ thống đang vận hành.
- **Trigger:** Phát sinh bất kỳ thao tác thay đổi quyền/khóa/plugin/chính sách nào, phát hiện vi phạm bảo mật hoặc quản trị viên bấm nút ngắt khẩn cấp.
- **Inputs:** Sự kiện quản trị/an ninh hoặc lệnh ngắt khẩn cấp ở 4 cấp (`Virtual Key`, `Project`, `Provider Connection`, `Extension Quarantine`).
- **Outputs:** Bản ghi `AuditEvent` chỉ ghi nối tiếp (append-only) và hiệu lực chặn tức thì trên mặt phẳng dữ liệu.
- **States:** `SM-01`, `SM-02`, `SM-05`.
- **Business Rules:** `TP-BR-0404`, `TP-BR-0803`.
- **Permissions:** `audit:read`, `audit:export`, `emergency:killswitch`.
- **Success Flow:** Ghi nhận đầy đủ ai làm gì, lúc nào, trạng thái trước/sau, nhãn nguồn gốc; khi kích hoạt ngắt khẩn cấp, trạng thái đồng bộ tới toàn bộ tiến trình xử lý trong ngưỡng cam kết.
- **Error & Negative Flows:** Từ chối `403` mọi yêu cầu sửa hoặc xóa lẻ tẻ bản ghi kiểm toán qua API.
- **Limits:** Hỗ trợ xuất luồng sự kiện kiểm toán liên tục ra kho lưu trữ bất biến bên ngoài.
- **Expected UX:** Có bảng tra cứu tác động sự cố tức thì (*"Những dự án và khóa nào đã đi qua Plugin X trong 7 ngày qua?"*).
- **Compatibility:** Chuẩn hóa cấu trúc JSON sự kiện kiểm toán dễ tích hợp SIEM.
- **SRS & AC:** `TP-FR-0704`..`0705`, `TP-NFR-0404`, `TP-AC-F0701`.

---

## Domain 08: Quan sát Vận hành, Sổ cái Ngân sách & Tài chính (`telemetry-finops`)

### `FT-13` — Sổ cái Ngân sách Hai Pha & Báo cáo Đối soát Chi phí Trung thực (`Two-Phase Budget Ledger & Chargeback`)
- **Actors:** `Organization Owner`, `Team Admin`, `API Client`, `Agent`.
- **Preconditions:** Đã khai báo biểu giá mô hình (từ `Provider Adapter` hoặc bảng giá tùy chỉnh của tổ chức) và cấu hình hạn mức ngân sách.
- **Trigger:** Trước mỗi yêu cầu phát sinh phí (bước giữ chỗ) và ngay khi yêu cầu kết thúc/hủy/lỗi (bước quyết toán).
- **Inputs:** Ước tính trần tiêu thụ đầu vào/đầu ra lúc bắt đầu; số đo thực tế từ nhà cung cấp (bao gồm token đầu vào, đầu ra, token đọc/ghi bộ nhớ đệm `cache_read`/`cache_write`, suy luận) lúc kết thúc.
- **Outputs:** Khoản giữ chỗ `BudgetReservation`, bút toán quyết toán gắn cờ độ chính xác (`EXACT`, `ESTIMATED`, `UNMETERED_FLAGGED`), cảnh báo ngưỡng mềm (webhook/UI) và báo cáo phân bổ chi phí.
- **States:** `SM-07`.
- **Business Rules:** `TP-BR-0701`..`0703`.
- **Permissions:** `budget:manage`, `billing:report:read`.
- **Success Flow:** Kiểm tra đồng thời số dư khả dụng trên toàn cây phân cấp (`Key → Env → Project → Org`) → Khóa tạm hạn mức → Quyết toán số thực khi hoàn tất và hoàn lại phần giữ chỗ thừa; định kỳ thu hồi các khoản giữ chỗ mồ côi quá hạn do sự cố sập nút mạng.
- **Error & Negative Flows:**
  - Thiếu số đo token từ nhà cung cấp (hoặc đứt luồng giữa chừng) → Bắt buộc tính ước lượng chuẩn và gắn nhãn `ESTIMATED` (hoặc `UNMETERED_FLAGGED` nếu chưa có biểu giá/không đếm được), **tuyệt đối không tuyên bố tính đúng tuyệt đối 100% hay ghi `0 USD` lấp liếm**.
  - Khoản quyết toán cuối luồng vượt nhẹ khoản giữ chỗ → Ghi nhận số dư âm chuyển tiếp (`Negative Balance Carryover` — chờ chốt `BA-OQ-002`) và chặn ngay từ yêu cầu kế tiếp.
- **Limits:** Sai số vượt trần khi bắn đồng thời nhiều luồng bị giới hạn tối đa trong biên độ của các luồng đã được giữ chỗ hợp lệ.
- **Expected UX:** Báo cáo chi phí tách rõ cột *"Chi phí xác thực chính xác (`Exact`)"* và *"Chi phí ước tính / Chờ đối soát (`Estimated / Unmetered`)"*.
- **Compatibility:** Hỗ trợ biểu giá đa thành phần (token chữ, ảnh, âm thanh, cache, hoặc giá `0 USD` / giá khấu hao nội bộ cho mô hình tự chạy).
- **SRS & AC:** `TP-FR-0801`..`0804`, `TP-AC-F0801`..`F0803`.

### `FT-14` — Xuất Chuẩn OpenTelemetry & Trình Tra cứu Truy vết Tôn trọng Quyền Riêng tư (`OTel & Privacy-First Telemetry`)
- **Actors:** `Developer`, `Team Admin`, `Operator`.
- **Preconditions:** Hệ thống đang xử lý lưu lượng.
- **Trigger:** Phát sinh yêu cầu qua mặt phẳng `Provider`, `MCP` hoặc `A2A`.
- **Inputs:** Siêu dữ liệu chu kỳ sống của yêu cầu (độ trễ tổng, thời gian tới mảnh đầu tiên `TTFT`, số lần chuyển mạch, mã lỗi, định danh tuyến/công cụ).
- **Outputs:** Chỉ số (Metrics), vết phân tán (Traces) và nhật ký xuất theo chuẩn OTLP ra hệ thống giám sát ngoài, cùng danh sách nhật ký vòng đời ngắn trên Web Console.
- **States:** —
- **Business Rules:** `TP-BR-0101`, `TP-BR-0802`.
- **Permissions:** `telemetry:read` (chỉ thấy dữ liệu thuộc phạm vi `Project`/`Organization` của mình).
- **Success Flow:** Mặc định chỉ ghi siêu dữ liệu vận hành (không ghi nội dung câu hỏi/trả lời gốc và tuyệt đối che tất cả tiêu đề xác thực); chỉ khi quản trị viên bật chế độ gỡ lỗi có thời hạn xóa tự động (`TTL`) thì nội dung mới được lưu cục bộ trong phạm vi dự án đó.
- **Error & Negative Flows:** Hệ thống nhận OTLP bên ngoài bị sập tạm thời → Bộ đệm xuất dữ liệu tự động bỏ bớt gói tin cũ kèm tăng bộ đếm cảnh báo (`Fail-Open` có kiểm soát cho luồng giám sát phụ trợ — `TP-BR-0502`), không làm nghẽn hay sập luồng gọi AI chính.
- **Limits:** Giới hạn dung lượng/thời gian lưu nhật ký cục bộ tự động xoay vòng để không làm đầy ổ cứng máy chủ tự vận hành.
- **Expected UX:** Lập trình viên dán `x-tanpopo-request-id` vào thanh tìm kiếm là thấy ngay dòng thời gian chi tiết của từng chặng kiểm tra chính sách và gọi nhà cung cấp.
- **Compatibility:** Tuân thủ quy ước ngữ nghĩa OpenTelemetry cho hệ thống GenAI.
- **SRS & AC:** `TP-FR-0805`..`0806`, `TP-NFR-0501`..`0502`, `TP-AC-F0804`.

---

## Domain 09: Giao diện Web Console, CLI & Admin API (`console-cli-api`)

### `FT-15` — Web Console Chuẩn Astryx, Công cụ Dòng lệnh `tanpopo` CLI & Admin API (`Unified Management Surfaces`)
- **Actors:** Toàn bộ các tác nhân con người (`Personal Admin`, `Organization Owner`, `Team Admin`, `Developer`, `Operator`, `Extension Publisher`, `Reviewer`).
- **Preconditions:** Người dùng xác thực hợp lệ vào Control Plane (hoặc chạy CLI cục bộ cho tác vụ phát triển/chẩn đoán).
- **Trigger:** Thao tác trên trình duyệt Web, gọi lệnh `tanpopo ...` trên thiết bị đầu cuối hoặc gọi REST API quản trị để tự động hóa.
- **Inputs:** Lệnh thao tác quản trị, tệp khai báo cấu hình hoặc thao tác tương tác trên giao diện.
- **Outputs:** Thay đổi cấu hình nhất quán (có kiểm tra xung đột phiên bản `ETag`/`version` — `TP-BR-0801`), giao diện phản hồi trực quan hoặc đầu ra định dạng bảng/JSON chuẩn trên CLI.
- **States:** Hiển thị và điều khiển đầy đủ `SM-01`..`SM-08`.
- **Business Rules:** `TP-BR-0201`, `TP-BR-0203`, `TP-BR-0801`, quy ước giao diện Astryx tại [`AGENTS.md`](../../AGENTS.md).
- **Permissions:** Đồng nhất một cơ chế kiểm tra quyền duy nhất tại Control Plane API (giao diện Web chỉ ẩn/hiện hỗ trợ trải nghiệm, không bao giờ dùng kiểm tra phía trình duyệt làm hàng rào bảo mật).
- **Success Flow:**
  - *Web Console:* Xây dựng từ các mẫu chuẩn của **Astryx Design System** với giao diện `neutral`, dùng bảng/danh sách gọn gàng cho dữ liệu dày đặc (nhật ký, khóa, danh sách công cụ MCP, sổ cái), hỗ trợ Setup Wizard, duyệt quyền plugin, Playground thử nghiệm và công tắc khẩn cấp.
  - *CLI:* Quản lý vòng đời máy chủ, chẩn đoán sức khỏe (`doctor`), cài/ghim/rollback plugin, khởi tạo và kiểm thử hợp đồng plugin (`plugin init/test/pack`), xuất gói ngoại tuyến.
- **Error & Negative Flows:** Hai quản trị viên lưu đè cùng một cấu hình → Trả lỗi `409 Conflict` yêu cầu tải lại bản mới nhất; phiên đăng nhập hết hạn → Yêu cầu xác thực lại an toàn không mất nháp cấu hình phi nhạy cảm.
- **Limits:** Mọi danh sách dữ liệu lớn trên API và UI bắt buộc phân trang chuẩn xác.
- **Expected UX:** Không một tác vụ quản trị cốt lõi nào chỉ làm được trên chuột mà không tự động hóa được bằng CLI/API, và ngược lại các tác vụ hằng ngày đều trực quan trên Web Console.
- **Compatibility:** Đáp ứng chuẩn khả năng tiếp cận WCAG 2.2 AA và các trình duyệt hiện đại (`TP-NFR-0701`, `TP-NFR-0801`).
- **SRS & AC:** `TP-FR-0901`..`0904`, `TP-AC-F0901`..`F0902`.

---

## Domain 10: Tự Vận hành, Cài đặt, Sao lưu & Nâng cấp (`selfhost-ops`)

### `FT-16` — Khởi chạy Gọn nhẹ, Khóa Cấu hình GitOps, Sao lưu Mã hóa & Nâng cấp Không Gián đoạn (`Self-Host Lifecycle & Operations`)
- **Actors:** `Operator`, `Personal Admin`.
- **Preconditions:** Môi trường máy chủ/container đáp ứng ma trận hỗ trợ hệ điều hành (`TP-NFR-0901`).
- **Trigger:** Cài đặt mới, nạp cấu hình từ tệp khai báo, sao lưu/khôi phục hoặc nâng cấp phiên bản hệ thống.
- **Inputs:** Tệp cấu hình khai báo, tệp khóa `tanpopo.lock`, khóa bảo vệ kho bí mật gốc, gói bản sao lưu hoặc phiên bản phần mềm mới.
- **Outputs:** Hệ thống vận hành khỏe mạnh (có điểm cuối `/healthz` và `/readyz` riêng biệt), bản sao lưu mã hóa toàn vẹn hoặc trạng thái quay lui an toàn nếu nâng cấp gặp sự cố.
- **States:** `SM-08` (Release & Upgrade Lifecycle).
- **Business Rules:** `TP-BR-0203`, `TP-BR-0802`, `TP-BR-0803`.
- **Permissions:** Thẩm quyền hạ tầng của `Operator`.
- **Success Flow:** Hỗ trợ chạy đơn nút không cần dịch vụ cơ sở dữ liệu ngoài ở quy mô khởi đầu và chuyển đổi sang chế độ nhiều bản sao không trạng thái ở quy mô lớn; trước khi nâng cấp tự động kiểm tra tính tương thích hợp đồng của toàn bộ các `Extension` đang cài (`Preflight Compatibility Check`).
- **Error & Negative Flows:** Phát hiện một `Extension` trọng yếu đang chạy không tương thích với bản Core mới → Chặn nâng cấp ngay từ bước kiểm tra đầu tiên (`BLOCKED_INCOMPATIBLE`) kèm hướng dẫn nâng cấp Adapter trước thay vì nâng cấp xong mới làm gãy lưu lượng sản xuất.
- **Limits:** Bản sao lưu không chứa khóa bảo vệ gốc ở dạng văn bản rõ (khóa giải mã phải được lưu giữ tách biệt).
- **Expected UX:** Một lệnh khởi chạy cho người dùng mới; quy trình GitOps bất biến dựa trên `tanpopo.lock` cho đội ngũ vận hành chuyên nghiệp.
- **Compatibility:** Cam kết cửa sổ hỗ trợ hợp đồng lùi (`N-1`) theo chính sách phát hành.
- **SRS & AC:** `TP-FR-1001`..`1005`, `TP-NFR-0601`..`0602`, `TP-AC-F1001`..`F1003`.
