# Định vị Sản phẩm, Thước đo Thành công, Phạm vi Phát hành & Thử nghiệm Kiểm chứng — Tanpopo

> **Tài liệu Discovery (Phase ASTRA A01 — Gate G1: `READY_FOR_REVIEW`)**
> - **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Product Intelligence Lead
> - **Trạng thái:** Đề xuất chiến lược sản phẩm (`PROPOSED`), trình Product Owner xem xét tại Cổng G1. Chưa khóa thành yêu cầu bắt buộc cho tới khi được phê duyệt.

---

## 1. Tuyên bố Định vị Đề xuất (Proposed Positioning Statement — `PROPOSED`)

### 1.1. Khung Định vị Cốt lõi

- **Dành cho:** Các **Nhóm phát triển sản phẩm (Dev Teams 5–50 kỹ sư)** và **Đội ngũ Hạ tầng AI (AI Platform Teams)** đang vận hành ứng dụng đa mô hình, công cụ MCP và tác nhân tự động trong thực tế,
- **Những người đang gặp vấn đề:** Bị kẹt giữa các AI Gateway mã nguồn mở đóng cứng hàng trăm tích hợp vào nhân (dễ lỗi khi nâng cấp, khóa tính năng quản trị bảo mật vào bản Enterprise đắt đỏ) và các cổng đám mây phức tạp đòi hỏi Kubernetes/Istio hoặc thu thuế phần trăm theo lưu lượng,
- **Tanpopo là:** Một **AI Gateway mã nguồn mở (Apache-2.0), ưu tiên tự vận hành (self-hosted-first) và lấy khả năng mở rộng an toàn làm trung tâm (extension-first)**,
- **Mang lại giá trị cốt lõi:** Thống nhất điều phối, chính sách bảo mật, giám sát và kiểm soát ngân sách trên cả 3 mặt phẳng lưu lượng tách biệt (**Model Providers, MCP Tools và Agent-to-Agent / A2A**), đồng thời cho phép cài đặt hoặc tự phát triển **Provider Adapters và Extensions** có cô lập bảo mật ngay từ Marketplace, Git, URL hoặc kho nội bộ **mà không cần nâng cấp Tanpopo Core**,
- **Khác với LiteLLM, Portkey, Agentgateway hay OpenRouter:** Tanpopo kết hợp trải nghiệm tự khởi chạy trong 1 lệnh kèm sẵn Web Console + CLI với kiến trúc Plugin có sandbox, kiểm thử hợp đồng tự động và cam kết tuân thủ các phương thức kết nối chính thống (không bẫy tường phí bảo mật cơ bản, không dựa vào vùng xám bẻ khóa gói thuê bao cá nhân).

### 1.2. Thông điệp Ngắn gọn (One-Line Pitch — `PROPOSED`)
> **"Tanpopo — Cổng AI tự vận hành mở rộng bằng Plugin cô lập: Quản trị thống nhất Models, MCP Tools và A2A Agents mà không cần chờ nâng cấp Core."**

---

## 2. Hệ thống Thước đo Thành công (North Star Metric & Measurable Outcomes — `PROPOSED`)

### 2.1. North Star Metric (NSM) cho Hạ tầng Mã nguồn mở Tự vận hành

Vì Tanpopo ưu tiên tự host và tôn trọng quyền riêng tư dữ liệu của người dùng (tuyệt đối không thu thập lén dữ liệu ngầm — *zero forced telemetry*), hệ thống đo lường được thiết kế trên 2 góc nhìn bổ trợ nhau:

1. **North Star Cấp độ Giá trị Sản phẩm (Hiển thị trực tiếp trên mỗi máy chủ Tanpopo & tổng hợp ẩn danh nếu người dùng chủ động bật opt-in):**
   - **Chỉ số:** **Số lượng Truy vấn Đa mặt phẳng Được Quản trị qua Extension Đạt chuẩn mỗi Tuần (`Weekly Governed Multi-Plane Requests — WGMR`)**.
   - *Định nghĩa:* Một truy vấn chỉ được tính vào `WGMR` khi đi qua đầy đủ chuỗi giá trị cốt lõi của Tanpopo: (1) xác thực Virtual Key/Identity hợp lệ, (2) kiểm tra Policy & Hạn mức ngân sách, (3) thực thi qua một Adapter/Plane có kiểm định hợp đồng (`Provider`, `MCP` hoặc `A2A`), và (4) ghi nhận thành công vào sổ cái chi phí/telemetry.
   - *Lý do chọn:* Phản ánh việc hệ thống thực sự được tin dùng cho lưu lượng hằng ngày chứ không phải lượt tải thử rồi bỏ xó (vanity stars/downloads).
2. **North Star Cấp độ Khác biệt Hệ sinh thái (Đo lường công khai không cần telemetry):**
   - **Chỉ số:** **Tỷ lệ Kết nối Nhà cung cấp/Mô hình Mới Không Cần Phát hành lại Core (`Zero-Core-Release Onboarding Rate`)** kết hợp **Số lượng Gói Adapter/Extension Đạt Kiểm thử Hợp đồng Tự động (`Verified Contract-Passing Extensions`)**.
   - *Mục tiêu khi đạt độ chín (`v1.0`):* `>= 90%` mô hình hoặc nhà cung cấp AI mới xuất hiện trên thị trường được cộng đồng/đội ngũ hỗ trợ chỉ bằng cách cập nhật hoặc cài mới gói Adapter mà không phải nâng cấp tệp thực thi Tanpopo Core.

### 2.2. Các Chỉ tiêu Kỹ thuật & Trải nghiệm Đo lường được (Measurable Outcomes / SLO Hypotheses)

| Nhóm mục tiêu | Thước đo cụ thể | Ngưỡng mục tiêu đề xuất (`PROPOSED`) | Ý nghĩa thực tế |
|---|---|---|---|
| **UX Khởi đầu (Onboarding)** | **Time-to-First-Governed-Request (`TTFGR`)** | **`< 3 phút`** từ lúc tải container/binary đến lúc gọi thành công request đầu tiên có ghi log & chi phí | Đảm bảo tính *"dùng được thực tế"* cho cả Solo Dev lẫn nhóm nhỏ mà không cần đọc tài liệu hàng giờ. |
| **UX Nhà phát triển Plugin** | **Time-to-First-Custom-Adapter (`TTFCA`)** | **`< 30 phút`** cho gói khai báo (Declarative Tier-1) và **`< 2 giờ`** cho gói Wasm (Tier-2) có sẵn khung SDK | Hạ thấp rào cản để phá vỡ bài toán "con gà – quả trứng" của Marketplace. |
| **Hiệu năng Đường dữ liệu** | **Gateway Internal Latency Overhead (`p95`)** | **`< 2 ms`** (Tier-1 Declarative) / **`< 5 ms`** (Tier-2 Wasm Sandbox), chưa tính độ trễ mạng của hãng AI | Không làm chậm tốc độ phản hồi streaming của mô hình hay tác nhân. |
| **Tài nguyên Tự vận hành** | **Mức tiêu thụ RAM khi rỗi (Standalone Mode)** | **`< 128 MB RAM`** (bao gồm cả tiến trình Core, cơ sở dữ liệu nhúng và tệp tĩnh Web Console) | Chạy nhẹ nhàng trên máy trạm cá nhân hoặc VPS giá rẻ nhất. |
| **An toàn Vận hành & Chi phí** | **Độ trễ Thu hồi Khóa / Cách ly Plugin Khẩn cấp** | **`< 1 giây`** (chế độ đơn lẻ) / **`< 5 giây`** (chế độ cụm phân tán HA) | Đáp ứng yêu cầu ứng phó sự cố thực tế (`UC-07`, `UC-09`). |
| **Độ chính xác Sổ cái Ngân sách** | **Sai số Vượt trần Ngân sách khi Bắn luồng Song song** | **`<= 1` luồng đang mở tối đa** nhờ cơ chế tạm giữ hạn mức trước khi cho qua (`Pre-reservation`) | Chặn đứng rủi ro cháy túi do vòng lặp agent lỗi (`UC-08`). |

---

## 3. Ranh giới Sản phẩm: Core vs Optional Extension vs Out-of-Scope (`PROPOSED`)

Việc vạch rõ ranh giới ngay từ bước Discovery giúp Tanpopo giữ cho phần nhân luôn gọn, nhanh, dễ kiểm toán bảo mật, đồng thời phát huy tối đa triết lý `extension-first`.

| Hạng mục năng lực | **1. Tanpopo Core (Nhân bắt buộc)** | **2. Optional Extensions (Official hoặc Community/Private)** | **3. Out-of-Scope (Ngoài phạm vi sản phẩm)** |
|---|---|---|---|
| **Kết nối Mô hình & Giao thức** | Đường ống HTTP/SSE Ingress (Unified API + Native Passthrough), Bộ điều phối MCP Plane, Bộ điều phối A2A Plane | **Tất cả Provider Adapters cụ thể** (OpenAI, Anthropic, Gemini, vLLM/Ollama, Bedrock, Azure, các hãng khu vực...) | Tự chạy mô hình suy luận trên GPU/CPU hoặc lưu trữ trọng số mô hình (vai trò của `vLLM`, `Ollama`) |
| **Cơ chế Mở rộng** | Trình nạp Manifest & Wasm Sandbox, Blind Credential Broker, Trình quản lý vòng đời & lockfile, CLI kiểm thử hợp đồng | Các gói Adapter tùy chỉnh nội bộ doanh nghiệp, bộ biến đổi xác thực đặc thù (Custom Enterprise Auth) | Chạy mã script trần không cô lập bảo mật trực tiếp trong tiến trình chủ của Core |
| **Định tuyến, Chính sách & Bảo mật** | Capability-Aware Router, Circuit Breaker, Rate Limiter, RBAC, Quản lý Virtual Keys, Chính sách vùng dữ liệu & lọc quyền MCP cơ bản | Các bộ lọc nội dung chuyên sâu (PII Redaction nâng cao, kết nối dịch vụ Guardrail ngoài, tích hợp KMS/Vault đặc thù) | Hệ thống phát hiện xâm nhập mạng cấp hệ điều hành hoặc tường lửa lớp L3/L4 |
| **Giám sát & Kiểm soát Chi phí** | Sổ cái token/chi phí phân cấp, Cơ chế tạm giữ ngân sách, Nhật ký kiểm toán (Audit Log), Xuất chuẩn OpenTelemetry (OTLP), Log cục bộ có giới hạn vòng đời | Bộ xuất dữ liệu chuyên biệt sang nền tảng phân tích bên thứ ba hoặc kho lưu trữ đám mây riêng | Nền tảng chấm điểm đánh giá chất lượng mô hình quy mô lớn (LLM-as-a-Judge Evals), gán nhãn dữ liệu hoặc huấn luyện tinh chỉnh (Fine-tuning) |
| **Giao diện & Phân phối** | Web Console quản trị (xây dựng chuẩn Astryx) và CLI `tanpopo`; trình đọc chỉ mục Registry/Git/URL | Các kho Private Registry tự dựng của từng tập đoàn; giao diện tùy biến riêng nếu cần | Cổng thanh toán mua bán lại tín dụng AI ăn chênh lệch hoặc các công cụ bẻ khóa/tái sử dụng gói thuê bao cá nhân trái TOS (`REJECTED`) |

---

## 4. Đề xuất Phạm vi Phát hành theo Giai đoạn (`0.x` đến `v1.0` — `PROPOSED`)

Lộ trình được chia thành các lát cắt dọc có thể dùng thật ở mỗi chặng, trong đó **mô hình dữ liệu nền tảng ngay từ `v0.1` đã tách sẵn 4 lớp (`Provider`, `Plugin`, `MCP`, `A2A`)** để không phải đập đi xây lại ở các bản sau:

| Phiên bản mục tiêu | Trọng tâm chiến lược | Phạm vi bàn giao cốt lõi (`PROPOSED`) | Tiêu chí hoàn thành (Exit Criteria) |
|---|---|---|---|
| **`v0.1` — Usable Core & Extension Engine (MVP thực dụng)** | Chứng minh kiến trúc `extension-first` chạy thật với các mô hình AI phổ biến nhất | - Khung miền chuẩn 4 lớp tách biệt<br>- Unified Model API + Native Dialect Passthrough + Streaming SSE<br>- Động cơ Plugin Hai Tầng (Declarative + Wasm Sandbox) + Blind Credential Broker + Lockfile<br>- Bộ 5–6 Official Reference Adapters (OpenAI, Anthropic, Gemini, OpenAI-Compatible cho Ollama/vLLM, Bedrock/Azure)<br>- Virtual Keys, Rate Limit, Capability-Aware Routing cơ bản, Sổ cái ngân sách & OTel cơ bản (chế độ Standalone SQLite)<br>- CLI `tanpopo` (chạy server, cài/test plugin) + Web Console Astryx cốt lõi (Providers, Plugins, Keys, Budgets, Logs) | Một nhóm kỹ sư tải về, chạy trong `< 3 phút`, định tuyến thành công qua 3 hãng AI + Ollama cục bộ và cài thêm được 1 Adapter từ URL/Git mà không cần biên dịch lại Core. |
| **`v0.2` — MCP Governance Plane & Registry Ecosystem** | Mở rộng sang quản trị công cụ tác nhân & hoàn thiện quy trình phát hành Plugin | - Mặt phẳng **MCP Plane** hạng nhất (Streamable HTTP/SSE, lọc danh sách tool theo khóa, tiêm thông tin xác thực ngầm, ghim `schema_hash` chống Rug-Pull)<br>- Hỗ trợ OAuth 2.1 được cấp phép & OIDC cơ bản trong SDK<br>- Chỉ mục Danh mục Extension tĩnh công khai + Hỗ trợ Private Registry nội bộ + Bộ công cụ ghi/phát lại kiểm thử `plugin test --replay`<br>- Công cụ mô phỏng định tuyến & chính sách (`/v1/route/explain`) | Tác nhân lập trình (IDE/CLI) kết nối cả LLM lẫn 5+ MCP servers qua duy nhất một cổng Tanpopo với phân quyền cấp công cụ chặt chẽ; bên thứ ba tự xuất bản được Adapter đạt chuẩn kiểm thử. |
| **`v0.3` — A2A Connectivity Plane & Clustered Scale-Out** | Sẵn sàng cho phối hợp đa tác nhân và chịu tải cao trong doanh nghiệp | - Mặt phẳng **A2A Plane** độc lập (Agent Card registry/discovery, ủy quyền tác vụ bất đồng bộ, áp chính sách liên tác nhân và truy vết xuyên luồng)<br>- Chế độ triển khai cụm **Scale-out HA** không trạng thái với PostgreSQL + Redis/Valkey (đồng bộ thu hồi khóa tức thì và hạn mức phân tán)<br>- RBAC nhiều nhóm/dự án, tích hợp kho bí mật ngoài (KMS/Vault extension) và gói cài đặt offline (`bundle export`) | Vận hành ổn định nhiều bản sao dưới tải streaming liên tục mà không mất đồng bộ ngân sách; điều phối được giao tiếp A2A có kiểm toán đầy đủ. |
| **`v1.0` — Production & Ecosystem Stability Guarantee** | Cam kết ổn định hợp đồng dài hạn & sẵn sàng cho tổ chức lớn | - Đóng băng cam kết tương thích ngược **Plugin Contract `v1.0`** và **Unified/Admin API `v1.0`**<br>- Kiểm toán bảo mật độc lập cho Wasm Sandbox & Credential Broker<br>- Hoàn thiện tài liệu vận hành sản xuất (Helm charts, Terraform/GitOps, kịch bản khôi phục thảm họa, SSO/OIDC quản trị viên) | Hợp đồng Plugin ổn định để cộng đồng và doanh nghiệp yên tâm đầu tư lâu dài; vượt qua bộ kiểm tra tải, bảo mật và độ tin cậy cấp sản xuất. |

---

## 5. Top 6 Thử nghiệm Khám phá & Kiểm chứng Sớm (Top Discovery Experiments)

Trước khi đầu tư hàng tháng viết toàn bộ hệ thống, cần chạy **6 thử nghiệm kiểm chứng ngắn hạn, chi phí thấp, có ngưỡng Đạt/Trượt rõ ràng** để loại bỏ các giả định rủi ro nhất:

| Mã | Giả thuyết cần kiểm chứng | Phương pháp thực hiện | Thời lượng ước tính | Tiêu chí Đạt (Pass) / Hướng xoay trục nếu Trượt (Pivot) |
|---|---|---|---|---|
| **EXP-01** | **Kiểm chứng Streaming qua Wasm Sandbox (`OQ-002`, `ASM-002`):** Plugin biên dịch ra `.wasm` có thể biến đổi luồng SSE từng mảnh (chunk-by-chunk) mà vẫn giữ độ trễ thấp và bộ nhớ nhỏ. | Viết 1 nguyên mẫu tối thiểu (spike) cho Host nhận luồng SSE giả lập tốc độ cao, đẩy qua 1 module Wasm biến đổi JSON sang chuẩn OpenAI trên 1.000 kết nối đồng thời, đo p95 latency và RAM. | 2 – 3 ngày | **Đạt:** Độ trễ cộng thêm `< 5 ms p95`, không rò rỉ bộ nhớ.<br>**Nếu Trượt:** Giữ Wasm cho biến đổi header/request/response theo khung sự kiện nhỏ hoặc đẩy nhiều khả năng hơn về tầng Declarative Engine chạy trực tiếp trong Host. |
| **EXP-02** | **Kiểm chứng Độ bao phủ của Declarative Adapter (`ASM-004`):** Cấu hình khai báo YAML/JSON có thể mô tả gọn gàng ít nhất 75% nhà cung cấp AI thương mại/mã nguồn mở phổ biến mà không cần viết mã. | Thử đặc tả nháp schema khai báo trên giấy/JSON Schema cho 10 nhà cung cấp thật (OpenAI, Groq, Together, Mistral, DeepSeek, Ollama, OpenRouter, Azure, Anthropic, Gemini) để tìm điểm gãy. | 1 – 2 ngày | **Đạt:** `>= 7/10` hãng biểu diễn trọn vẹn bằng cấu hình khai báo; chỉ 2–3 hãng có phương ngữ rất riêng mới cần Tier-2 Wasm.<br>**Nếu Trượt:** Tinh chỉnh lại ranh giới biểu thức ánh xạ trong manifest trước khi công bố SDK. |
| **EXP-03** | **Kiểm thử Trải nghiệm Tác giả Viết Adapter (`TTFCA < 30 phút`):** Một lập trình viên chưa từng đọc mã nguồn Core của Tanpopo có thể tự tạo một Adapter mới chỉ từ tài liệu mẫu trong dưới 30 phút. | Đưa tài liệu hợp đồng nháp (1 trang) và tệp mẫu cho 3–5 kỹ sư độc lập yêu cầu họ viết cấu hình kết nối cho 1 API mô hình bất kỳ và ghi nhận các chỗ họ bị vướng. | 2 ngày | **Đạt:** Ít nhất 4/5 người hoàn thành đúng dưới 30 phút không cần giải thích miệng.<br>**Nếu Trượt:** Đơn giản hóa cấu trúc manifest và thông báo lỗi kiểm tra hợp đồng. |
| **EXP-04** | **Kiểm chứng Chống Đua tranh Ngân sách Streaming (`UC-08`):** Cơ chế tạm giữ hạn mức hai pha ngăn chặn được lỗi vượt chi tiêu khi bắn hàng chục luồng SSE song song mà không làm nghẽn thông lượng. | Dựng mô hình kiểm thử bắn 100 luồng đồng thời vào một tài khoản chỉ còn hạn mức đủ cho 5 luồng, kèm kịch bản ngắt kết nối đột ngột giữa chừng. | 1 – 2 ngày | **Đạt:** Số dư quyết toán cuối cùng không vượt trần quá biên độ dự phòng của 1 luồng và toàn bộ khoản giữ chỗ treo được thu hồi sạch khi đứt mạng. |
| **EXP-05** | **Kiểm chứng Thực tế với Các MCP Clients Phổ biến (`UC-06`):** Mặt phẳng MCP trung gian của Tanpopo tương thích trơn tru với các công cụ thực tế (Claude Code, Cursor, Codex, MCP Inspector) khi bật lọc quyền công cụ và phân trang lớn (>150 tools). | Dựng điểm cuối MCP Streamable HTTP mẫu đứng trước 3 MCP servers thật, kiểm tra hành vi khám phá tool, gọi tool và phản ứng khi `schema_hash` thay đổi. | 2 ngày | **Đạt:** Mọi client phổ biến nhận diện và gọi đúng công cụ được cấp quyền, không gặp lỗi cắt cụt danh sách hay vỡ phiên.<br>**Nếu Trượt:** Bổ sung chế độ tương thích cho từng loại transport của client trước khi thiết kế chi tiết `v0.2`. |
| **EXP-06** | **Phỏng vấn Khám phá Nhu cầu Thực tế với Nhóm Mũi nhọn (`ASM-001`):** Xác nhận xem nỗi đau lớn nhất khiến các Dev Teams / AI Platform Teams sẵn sàng chuyển sang hoặc triển khai Tanpopo là gì. | Thực hiện 8–10 cuộc trao đổi ngắn (hoặc khảo sát kỹ thuật có cấu trúc) với các kỹ sư đang tự vận hành LiteLLM / One-API / cổng tự viết về sự cố nâng cấp, quản lý MCP và chính sách nội bộ. | 1 tuần (chạy song song) | **Đạt:** Xác nhận rõ thứ tự ưu tiên tính năng cho `v0.1` và `v0.2` dựa trên nỗi đau thật thay vì suy đoán chủ quan. |
