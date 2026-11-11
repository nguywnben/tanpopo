# Phân tích Người dùng, Jobs-to-be-Done & Use Cases Thực tế — Tanpopo

> **Tài liệu Discovery (Phase ASTRA A01 — Gate G1: `READY_FOR_REVIEW`)**
> - **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Product Intelligence Lead
> - **Trạng thái:** Phân tích khám phá (`PROPOSED` / `ASSUMED`), chưa phải đặc tả yêu cầu đã khóa.

---

## 1. Phân khúc Tổ chức & Xếp hạng Ưu tiên Tiếp cận (Target Segments Ranking)

Để tránh sai lầm phổ biến của các dự án hạ tầng mã nguồn mở là *"thiết kế cho ngân hàng 10.000 người ngay ngày đầu nên quá nặng với developer, nhưng lại thiếu chứng chỉ bảo mật để bán cho ngân hàng"*, chúng ta chấm điểm và xếp hạng 5 phân khúc theo 4 trục (thang 1–5): **Cường độ nỗi đau (Pain)**, **Khả năng chấp nhận sớm / ít rào cản mua sắm (Adoption Speed)**, **Giá trị lan tỏa hệ sinh thái Plugin (Ecosystem Flywheel)** và **Độ phù hợp năng lực đội ngũ giai đoạn 0.x (Fit)**.

| Hạng ưu tiên | Phân khúc người dùng | Pain (1–5) | Adoption Speed (1–5) | Ecosystem Flywheel (1–5) | Fit 0.x (1–5) | Tổng điểm | Vai trò chiến lược với Tanpopo (`PROPOSED`) |
|---|---|---|---|---|---|---|---|
| **#1 (Mũi nhọn / Beachhead)** | **Dev Teams (Nhóm sản phẩm / Startup AI 5–50 kỹ sư)** | **5** | **5** | **4** | **5** | **19/20** | **Người dùng mục tiêu số 1 giai đoạn đầu:** Đang kết nối 3–6 model providers + MCP servers vào sản phẩm thật & coding agents nội bộ; đau đầu vì chi phí vọt bất ngờ, lỗi đứt quãng hãng AI và rào cản tường phí Open-Core; có quyền quyết định cài thử ngay trong 1 buổi chiều. |
| **#2 (Đồng mũi nhọn mở rộng)** | **AI Platform Teams (Đội hạ tầng AI tại công ty công nghệ 50–500 kỹ sư)** | **5** | **4** | **5** | **4** | **18/20** | **Động cơ tạo Plugin chất lượng cao:** Cần tích hợp cổng nội bộ, mô hình tự host trên GPU (vLLM), AWS Bedrock/Vertex IAM và MCP nội bộ mà không muốn chờ PR vào repo upstream; chính là lực lượng viết và đóng góp **Provider Adapters / Private Extensions** mạnh nhất. |
| **#3 (Lan tỏa cộng đồng)** | **AI Power Users & Solo Developers (Chính thống — BYOK & Local Models)** | **3.5** | **5** | **4** | **5** | **17.5/20** | **Phễu lan tỏa tự nhiên (Bottom-up Adoption):** Dùng nhiều công cụ code/agent (Claude Code, Codex, Cursor, OpenCode...) kết hợp Ollama/vLLM tại nhà và hàng chục MCP tools; cần 1 cổng gọn nhẹ quản lý khóa, đặt trần chi tiêu tháng và theo dõi token. |
| **#4 (Mục tiêu v1.0+)** | **Enterprise truyền thống / Ngành chịu quản lý chặt (Tài chính, Y tế, Chính phủ >1.000 nhân sự)** | **5** | **1.5** | **2** | **2** | **10.5/20** | **Không làm mục tiêu v0.1–v0.2:** Chu kỳ thẩm định bảo mật 6–12 tháng, đòi hỏi FIPS, HSM/Vault chuyên sâu, SLA thương mại 24/7 và kiểm toán chuỗi cung ứng air-gapped. Chỉ phục vụ khi nền tảng đã chín muồi ở mốc `v1.0+`. |

### Chân dung Loại trừ (Explicit Anti-Personas — `CONFIRMED_BY_OWNER` / `REJECTED`)
1. **Người săn lùng công cụ "biến thuê bao chat cá nhân thành API miễn phí" (Subscription Arbitrage Seekers):** Nhóm người dùng chỉ muốn cắm cookie/OAuth của gói ChatGPT Plus, Claude Pro/Max hay Google Antigravity cá nhân để chạy vòng lặp tác nhân hàng loạt trái điều khoản. Phục vụ nhóm này mang lại rủi ro pháp lý trực diện và làm mất uy tín hoàn toàn trước các đội ngũ kỹ thuật nghiêm túc.
2. **Chợ đen bán lại API Key lậu (Grey-Market Key Resellers):** Cần hệ thống nạp thẻ cào, chia hoa hồng đại lý nhiều cấp thay vì một cổng kỹ thuật quản trị an toàn.

---

## 2. Phân biệt 4 Vai trò Chức năng & Jobs-to-be-Done (Functional Roles)

Trong thực tế, ở nhóm Solo Dev cả 4 vai trò nằm trong một người; ở Dev Team 20 người thường chia làm 2 người; còn ở AI Platform Team/Enterprise là 4 bộ phận độc lập. Thiết kế quyền hạn và UX của Tanpopo phải tách bạch rõ 4 vai trò này:

| Mã | Vai trò chức năng | Job-to-be-Done cốt lõi ("Khi..., tôi muốn..., để...") | Giao diện sử dụng chính | Nỗi sợ lớn nhất |
|---|---|---|---|---|
| **R1** | **Người vận hành (`Gateway Operator / Infra Admin`)** | *"Khi triển khai và duy trì cổng AI cho hệ thống, tôi muốn khởi chạy nhanh bằng cấu hình khai báo rõ ràng, cài thêm adapter mới không cần dừng dịch vụ hay nâng cấp nhân Core, để hệ thống đạt uptime cao và tốn ít công bảo trì."* | CLI (`tanpopo`), tệp cấu hình GitOps, Docker/Helm, trang Health trên Web Console | Nâng cấp cổng làm gãy ứng dụng đang chạy; plugin lỗi làm sập toàn bộ tiến trình Gateway. |
| **R2** | **Lập trình viên dùng API (`API Consumer / App & Agent Dev`)** | *"Khi viết tính năng AI hoặc cấu hình tác nhân, tôi muốn gọi một điểm cuối chuẩn với khóa ảo được cấp sẵn, giữ nguyên được các tính năng nâng cao của model và xem ngay log lỗi/trace khi gọi hỏng, để giao sản phẩm nhanh mà không phải quản lý 10 bộ SDK khác nhau."* | Unified API / Native Passthrough / MCP & A2A Endpoints, trang Playground & Trace Logs | Cổng trung gian làm mất tham số đặc thù (như thinking/caching), làm tăng độ trễ streaming hoặc trả về mã lỗi mù mờ khó gỡ lỗi. |
| **R3** | **Nhà phát triển phần mở rộng (`Plugin / Adapter Publisher`)** | *"Khi một nhà cung cấp AI mới ra mắt hoặc công ty tôi có dịch vụ mô hình nội bộ, tôi muốn khởi tạo khung adapter từ SDK, kiểm thử tự động với bộ contract test và phát hành ngay qua Git/Registry, để mọi người dùng được ngay trong ngày mà không phải chờ đội ngũ Tanpopo duyệt gộp code vào Core."* | Plugin SDK, CLI kiểm thử (`plugin init/test/pack`), Tài liệu hợp đồng (Contract Docs), Registry | Tài liệu hợp đồng mập mờ; Core đổi giao diện nội bộ làm hỏng plugin; quy trình biên dịch/đóng gói quá rắc rối. |
| **R4** | **Quản trị Bảo mật & Chi phí (`Security, Governance & FinOps Admin`)** | *"Khi hàng chục kỹ sư và tác nhân tự động gọi mô hình cùng công cụ MCP mỗi ngày, tôi muốn kiểm duyệt quyền hạn của từng plugin, khóa danh sách công cụ MCP được phép chạy, đặt trần ngân sách cứng theo nhóm và thu hồi khóa/plugin tức thì khi có sự cố, để không bị rò rỉ dữ liệu hay cháy ngân sách."* | Web Console (Policies, Budgets, Audit Logs, Plugin Permissions), Cảnh báo OTel/Webhook | Một plugin bên thứ ba âm thầm gửi trộm API key gốc ra ngoài; một vòng lặp agent lỗi đốt hàng nghìn USD trong đêm; MCP tool xóa nhầm dữ liệu thật. |

---

## 3. Chín Use Cases Vận hành Thực tế (Happy Path & Failure Path)

Mỗi tình huống dưới đây mô tả luồng thành công chuẩn (**Happy Path**), các kịch bản lỗi thực tế khắc nghiệt (**Failure / Edge Paths**) và hệ quả thiết kế rút ra cho Tanpopo.

---

### UC-01: Gọi Đa Nhà cung cấp qua Unified API & Bảo toàn Năng lực Gốc
- **Tác nhân:** `R2 (Developer)` gọi qua Gateway do `R1 (Operator)` cấu hình.
- **Tiền điều kiện:** Đã kích hoạt Official Adapters cho OpenAI, Anthropic và Google Gemini; `R2` được cấp một Virtual Key có quyền gọi nhóm mô hình được phép.
- **Happy Path:**
  1. Ứng dụng gửi request chuẩn `/v1/chat/completions` (hoặc `/v1/responses`) kèm Virtual Key tới Tanpopo.
  2. Tanpopo xác thực khóa, tra cứu adapter đích dựa trên định danh mô hình, chuyển đổi định dạng chuẩn xác, mở luồng SSE streaming về ứng dụng với độ trễ cộng thêm tối thiểu và ghi nhận chính xác số token tiêu thụ vào sổ cái khi luồng kết thúc.
  3. Với tác vụ chuyên sâu cần tính năng đặc thù của riêng Anthropic (ví dụ cấu trúc prompt cache chi tiết trên `/v1/messages`), `R2` gọi đường dẫn phương ngữ gốc qua Tanpopo; cổng giữ nguyên payload gốc mà vẫn áp dụng đầy đủ xác thực, ngân sách và giám sát.
- **Failure / Edge Paths:**
  - *Lỗi 1 — Đứt luồng giữa chừng (Mid-stream Upstream Disconnect):* Nhà cung cấp trả về `200 OK`, truyền được 40% câu trả lời qua SSE rồi ngắt kết nối đột ngột hoặc gửi sự kiện lỗi nội bộ. Gateway **không thể** âm thầm nối tiếp sang model dự phòng từ đầu vì client đã nhận một nửa văn bản! Gateway phải đóng luồng sạch bằng khung báo lỗi chuẩn SSE, hạch toán phần token ước tính/đã phát sinh và gắn cờ lỗi `mid_stream_abort` trong trace.
  - *Lỗi 2 — Lệch năng lực tham số (Unsupported Capability):* `R2` gửi yêu cầu ép định dạng `json_schema` nghiêm ngặt hoặc đính kèm tệp âm thanh tới một mô hình không hỗ trợ. Thay vì âm thầm cắt bỏ tham số khiến mô hình trả lời sai định dạng, Adapter kiểm tra ma trận năng lực (Capability Matrix) và trả về ngay lỗi `400 capability_not_supported` tường minh trước khi tốn tiền gọi ra ngoài.

---

### UC-02: Kết nối Mô hình Tự vận hành / Cục bộ (Self-Hosted / Local Models)
- **Tác nhân:** `R1 (Operator)` hoặc `R2 (Solo/Team Dev)`.
- **Tiền điều kiện:** Có máy chủ GPU trong mạng nội bộ chạy `vLLM`, `SGLang` hoặc máy trạm chạy `Ollama` / `llama.cpp`.
- **Happy Path:**
  1. `R1` cấu hình kết nối tới điểm cuối nội bộ (ví dụ `http://gpu-node.internal:8000/v1`) thông qua Adapter chuẩn tương thích OpenAI/Ollama, khai báo bảng chi phí nội bộ (ví dụ `$0` hoặc chi phí khấu hao GPU ước tính trên mỗi triệu token).
  2. Các ứng dụng và công cụ lập trình dùng chung một Virtual Key của Tanpopo để gọi cả mô hình cục bộ lẫn mô hình đám mây mà không cần mở cổng máy chủ GPU trực tiếp ra ngoài.
- **Failure / Edge Paths:**
  - *Lỗi 1 — Trễ nạp mô hình vào VRAM (Cold-Start Timeout):* `Ollama` mất 25–60 giây để nạp mô hình 32B từ ổ cứng lên GPU ở lần gọi đầu tiên. Nếu Gateway đặt timeout kết nối cứng như với API đám mây (5 giây) sẽ báo lỗi giả. Adapter cho mô hình cục bộ phải hỗ trợ cấu hình `first_byte_timeout` riêng biệt và kiểm tra trạng thái nạp mô hình.
  - *Lỗi 2 — Tràn bộ nhớ ngữ cảnh hoặc hỏng cú pháp Tool Call:* Mô hình lượng tử hóa (quantized model) trả về chuỗi JSON gọi công cụ bị thiếu dấu ngoặc hoặc máy chủ GPU trả lỗi `CUDA out of memory`. Gateway ghi nhận đúng phân loại lỗi hạ tầng nội bộ để kích hoạt điều hướng dự phòng (nếu chính sách cho phép chuyển ra đám mây) hoặc dừng an toàn (nếu dữ liệu thuộc diện cấm rời mạng nội bộ).

---

### UC-03: Cài đặt Provider Adapter từ Nguồn Ngoài mà Không Cần Nâng cấp Core
- **Tác nhân:** `R3 (Publisher)` xuất bản; `R4 (Security Admin)` duyệt quyền; `R1 (Operator)` cài đặt.
- **Tiền điều kiện:** Một nhà cung cấp AI khu vực mới (hoặc cổng AI riêng của tập đoàn) vừa ra mắt API riêng chưa từng có trong bản phát hành của Tanpopo Core.
- **Happy Path:**
  1. `R3` dùng bộ khung mẫu của Tanpopo tạo gói adapter, khai báo rõ trong manifest: phiên bản hợp đồng `v1`, chỉ xin quyền gọi HTTPS tới `api.new-provider.example` và nhận khóa xác thực qua cơ chế tiêm header của Host. `R3` chạy bộ kiểm thử tương thích tự động đạt chuẩn rồi đẩy lên Git release / Private Registry.
  2. `R1` chạy lệnh cài đặt từ URL/Registry. Tanpopo hiển thị bảng kê quyền hạn (permissions diff) để `R4` hoặc `R1` xác nhận, kiểm tra mã băm SHA-256 / chữ ký số, nạp plugin vào môi trường cô lập (sandbox) và kích hoạt ngay mà không cần khởi động lại hay biên dịch lại Core.
- **Failure / Edge Paths:**
  - *Lỗi 1 — Vượt quyền truy cập mạng (Sandbox Egress Violation):* Mã bên trong plugin cố tình mở kết nối ngầm tới `evil-collector.example` để tuồn nội dung prompt ra ngoài. Lớp cô lập của Tanpopo chặn đứng yêu cầu mạng không nằm trong danh sách `allowed_hosts` đã duyệt, hủy lệnh gọi, ghi cảnh báo bảo mật mức nghiêm trọng và có thể tự động cách ly plugin.
  - *Lỗi 2 — Lệch phiên bản hợp đồng hoặc treo vòng lặp vô hạn:* Gói cài yêu cầu hợp đồng Core `v2` trên máy chủ đang chạy `v1`, hoặc mã plugin rơi vào vòng lặp vô tận khi phân tích phản hồi lạ. Hệ thống từ chối cài đặt ngay từ bước kiểm tra manifest (với lỗi lệch phiên bản), hoặc ngắt cưỡng bức lần thực thi vượt giới hạn CPU/thời gian (fuel/timeout limit) mà không làm ảnh hưởng các luồng khác trên Gateway.

---

### UC-04: Kết nối qua OAuth Được Cấp phép & Định danh Doanh nghiệp (Authorized OAuth & Enterprise Identity)
- **Tác nhân:** `R1 (Operator)` / `R4 (Security Admin)`.
- **Tiền điều kiện:** Nhà cung cấp mô hình hoặc dịch vụ đám mây hỗ trợ chính thức cơ chế OAuth 2.1 (PKCE / Client Credentials) hoặc liên kết định danh đám mây (Workload Identity / OIDC / IAM Role) cho ứng dụng tích hợp.
- **Happy Path:**
  1. `R1` chọn phương thức xác thực `Authorized OAuth / Federated Identity` do Adapter khai báo hợp lệ, thực hiện luồng cấp quyền chuẩn trên trình duyệt hoặc gắn Service Account / OIDC role.
  2. Tanpopo lưu trữ mã làm mới (refresh token) ở dạng mã hóa trong kho bí mật, tự động làm mới access token trước khi hết hạn hoàn toàn trong suốt đối với `R2 (Developer)`, và chỉ tiêm token vào đúng miền đích được cấp phép.
- **Failure / Edge Paths:**
  - *Lỗi 1 — Thu hồi quyền hoặc hết hạn phiên làm mới (Token Revocation / Rotation Race):* Quản trị viên phía đám mây thu hồi quyền ứng dụng, hoặc 50 request đồng thời cùng phát hiện access token hết hạn và cùng gọi làm mới gây lỗi xung đột (refresh token reuse detection). Kho bí mật của Tanpopo phải có khóa đồng bộ hóa (single-flight token refresh) khi làm mới token, và khi nhận lỗi `invalid_grant` phải chuyển trạng thái kết nối sang `AUTH_REQUIRES_RECONSENT` đồng thời bắn cảnh báo vận hành ngay lập tức.
  - *Lỗi 2 — Cố tình cấu hình trái phép phiên thuê bao tiêu dùng:* Người dùng tìm cách nhét cookie phiên trình duyệt cá nhân hoặc yêu cầu Tanpopo cung cấp trình đăng nhập giả lập cho gói thuê bao cá nhân bị cấm. Kho Official Adapters và tài liệu chính thức hoàn toàn không cung cấp cơ chế này (`CONFIRMED_BY_OWNER` / `DEC-004`).

---

### UC-05: Định tuyến Thông minh Nhận thức Năng lực & Chuyển mạch Dự phòng (Capability-Aware Routing & Failover)
- **Tác nhân:** `R1 (Operator)` thiết lập quy tắc; `R2 (Developer)` gửi truy vấn.
- **Tiền điều kiện:** Cấu hình một nhóm định tuyến ảo (ví dụ `smart-production`) gồm nhà cung cấp chính (như Anthropic trên Vertex AI) và dự phòng (như Anthropic trực tiếp hoặc AWS Bedrock).
- **Happy Path:**
  1. Request yêu cầu `context = 150k tokens` + `tool_calling = true` + ràng buộc chính sách `data_region = EU`.
  2. Bộ định tuyến lọc bỏ các đích không đủ cửa sổ ngữ cảnh 150k hoặc nằm ngoài vùng EU, chọn đích tối ưu nhất đang khỏe mạnh; nếu đích số 1 trả lỗi `429 Rate Limit` hoặc `503 Overloaded` trước khi phát sinh dữ liệu trả về, Gateway tự động chuyển sang đích dự phòng hợp lệ số 2 trong vài mili-giây.
- **Failure / Edge Paths:**
  - *Lỗi 1 — Không có đích nào thỏa mãn giao điểm năng lực & chính sách:* Đích dự phòng duy nhất còn sống lại nằm ở vùng US trong khi chính sách bắt buộc giữ dữ liệu tại EU. Bộ định tuyến **tuyệt đối không được tự ý phá rào chính sách bảo mật để cứu uptime** (fail-closed đối với ràng buộc bảo mật/vùng dữ liệu), trả về lỗi `503 no_compliant_upstream_available` ghi rõ lý do chặn.
  - *Lỗi 2 — Bão thử lại dây chuyền (Retry Storm):* Khi mạng ra ngoài gặp sự cố diện rộng, việc thử lại liên tiếp làm nghẽn toàn bộ hàng đợi. Bộ ngắt mạch (Circuit Breaker) của từng đích phải tự mở tạm thời để giải phóng tài nguyên ngay lập tức.

---

### UC-06: Quản trị Công cụ MCP (MCP Tool Governance, Scoping & Credential Masking)
- **Tác nhân:** `R4 (Security Admin)` phân quyền; `R2 (Developer / Coding Agent)` sử dụng công cụ.
- **Tiền điều kiện:** Tanpopo kết nối tới các MCP server nội bộ (ví dụ: cơ sở dữ liệu đọc-chỉ-định, hệ thống quản lý ticket, kho tài liệu).
- **Happy Path:**
  1. Agent kết nối vào điểm cuối MCP Plane của Tanpopo bằng Virtual Key của nhóm phát triển.
  2. Tanpopo chỉ hiển thị danh sách công cụ đã được lọc theo quyền của khóa đó (ví dụ chỉ thấy `read_ticket`, `search_docs`; ẩn hoàn toàn `delete_project`, `exec_sql_write`), hỗ trợ phân trang chuẩn xác không giới hạn cứng số lượng công cụ, tự động tiêm thông tin xác thực dịch vụ đích ở phía sau và ghi nhật ký chi tiết mọi lần gọi tool.
- **Failure / Edge Paths:**
  - *Lỗi 1 — Tấn công thay đổi mô tả công cụ ("MCP Rug-Pull" / Tool Poisoning):* Một MCP server bên ngoài cập nhật ngầm phần mô tả văn bản của công cụ để chèn chỉ thị độc hại (prompt injection) dụ Agent đọc tệp nhạy cảm. Tanpopo phát hiện mã băm schema của công cụ (`tool_schema_hash`) bị thay đổi so với phiên bản đã được `R4` phê duyệt và tạm khóa công cụ đó chờ xác nhận lại.
  - *Lỗi 2 — Gọi trái phép hoặc treo công cụ:* Agent cố gọi trực tiếp tên công cụ bị ẩn hoặc MCP server đích bị treo không phản hồi. Tanpopo từ chối ngay với lỗi phân quyền (ghi vào nhật ký bảo mật) hoặc ngắt kết nối đúng hạn thời gian (timeout) để không làm treo phiên làm việc của Agent.

---

### UC-07: Ứng phó Sự cố Khẩn cấp (Security & Runaway Incident Response)
- **Tác nhân:** `R4 (Security Admin)` & `R1 (Operator)`.
- **Tiền điều kiện:** Phát hiện một Virtual Key bị lộ lên kho Git công khai hoặc một kịch bản tác nhân bị lỗi lặp vô hạn đang gửi 500 request/phút.
- **Happy Path:**
  1. Hệ thống phát hiện tốc độ tiêu thụ bất thường (anomaly/velocity limit) hoặc `R4` bấm nút thu hồi khóa / kích hoạt công tắc ngắt khẩn cấp (Kill-Switch) qua CLI/Web Console.
  2. Khóa bị vô hiệu hóa tức thì trên toàn bộ mặt phẳng dữ liệu; khóa kết nối với nhà cung cấp đích có thể được xoay vòng (rotate) tại một nơi duy nhất trên Tanpopo mà không cần sửa cấu hình của 20 ứng dụng nội bộ khác không liên quan.
- **Failure / Edge Paths:**
  - *Lỗi — Độ trễ đồng bộ trạng thái thu hồi trong mô hình chạy nhiều bản sao (Cluster Propagation Lag) hoặc mất kết nối kho dữ liệu trung tâm:* Nếu bộ nhớ đệm cục bộ giữ quyền khóa quá lâu (ví dụ 15 phút), kẻ tấn công vẫn tiếp tục khai thác được sau khi khóa đã bị thu hồi. Thiết kế phải đảm bảo kênh thông báo thu hồi tức thì (pub/sub hoặc TTL ngắn tính bằng giây cho trạng thái khóa) và chế độ `fail-closed` an toàn cho các khóa bị đánh dấu nghi vấn.

---

### UC-08: Kiểm soát Ngân sách Phân cấp & Hạch toán Chi phí Luồng Streaming
- **Tác nhân:** `R4 (FinOps/Security Admin)` đặt hạn mức; `R2 (Developer)` tiêu dùng.
- **Tiền điều kiện:** Nhóm A được cấp ngân sách cứng `100 USD / tháng` và cảnh báo mềm ở mức `80 USD`.
- **Happy Path:**
  1. Khi tổng chi tiêu chạm `80 USD`, Tanpopo phát sự kiện cảnh báo qua webhook/console.
  2. Khi chạm `100 USD`, các truy vấn phát sinh phí mới từ Nhóm A bị từ chối gọn gàng với mã lỗi `429 budget_exceeded` (hoặc tự động chuyển hướng sang mô hình nội bộ miễn phí nếu chính sách nhóm cấu hình chế độ `downgrade-on-budget-cap`).
- **Failure / Edge Paths:**
  - *Lỗi 1 — Đua tranh song song vượt trần ngân sách (Concurrent Streaming Overspend):* Khi ngân sách còn `0.50 USD`, một tác nhân bắn đồng loạt 40 luồng yêu cầu ngữ cảnh lớn cùng một mili-giây. Nếu chỉ trừ tiền *sau khi* luồng kết thúc, cả 40 luồng đều lọt qua cổng kiểm tra ban đầu và làm lố ngân sách hàng chục USD. Tanpopo cần cơ chế **tạm giữ hạn mức ước tính trước khi cho qua (pre-request quota reservation)** và quyết toán lại số dư thật khi luồng kết thúc.
  - *Lỗi 2 — Thiếu bảng giá hoặc nhà cung cấp không trả số đếm token khi đứt mạng:* Adapter phải có cơ chế đếm ước lượng dự phòng gắn cờ `estimated_usage = true` để sổ cái không bị bỏ trống chi phí.

---

### UC-09: Nâng cấp, Ghim phiên bản, Rollback & Thu hồi Khẩn cấp Plugin
- **Tác nhân:** `R1 (Operator)` & `R4 (Security Admin)`.
- **Tiền điều kiện:** Hệ thống đang dùng `provider-x-adapter@1.2.0` cho lưu lượng thật; bản `1.3.0` vừa phát hành (hoặc phát hiện lỗ hổng bảo mật trong một community plugin).
- **Happy Path:**
  1. `R1` kiểm tra bản cập nhật; hệ thống đối chiếu tính tương thích cấu hình hiện tại với schema của `1.3.0`, tải gói mới, chuyển lưu lượng mới sang bản `1.3.0` trong khi chờ các kết nối streaming đang chạy trên bản `1.2.0` hoàn tất êm đẹp (graceful drain).
  2. Nếu bản `1.3.0` phát sinh tỷ lệ lỗi cao, `R1` chạy một lệnh quay lui (rollback) về bản `1.2.0` đã ghim trong lockfile chỉ trong vài giây.
- **Failure / Edge Paths:**
  - *Lỗi — Thu hồi khẩn cấp plugin độc hại / vi phạm nghiêm trọng (Emergency Plugin Quarantine):* Khi phát hiện một plugin bên ngoài có lỗ hổng nghiêm trọng, `R4` thực hiện lệnh cách ly (`quarantine/revoke`). Tanpopo lập tức ngắt quyền thực thi và quyền mạng của plugin đó, vô hiệu hóa các tuyến phụ thuộc vào nó với thông báo tường minh cho ứng dụng gọi tới, và xuất danh sách toàn bộ khóa/dự án từng đi qua plugin đó trong nhật ký kiểm toán để phục vụ điều tra.
