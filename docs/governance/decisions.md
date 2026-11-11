# Decision Register & Blocking Open Questions — Tanpopo

> **Tài liệu nguồn (Canonical Decision Log — Phase A00)**
> - **Ngày cập nhật:** `2026-11-11`
> - **Nguyên tắc quản trị:** Không một gợi ý nào từ AI Agent/Assistant được phép nâng trạng thái lên `CONFIRMED_BY_OWNER` nếu Product Owner chưa trực tiếp phê duyệt. Các thành phần kiến trúc, tech stack, UI chi tiết và scope phát hành hiện đều ở trạng thái `OPEN` / `PROPOSED`.

---

## Phần A — Các quyết định đã xác nhận (Confirmed Baseline Decisions)

| Mã | Chủ đề | Nội dung quyết định | Trạng thái | Bằng chứng / Nguồn |
|---|---|---|---|---|
| **DEC-001** | Định vị cốt lõi | Tanpopo là **AI Gateway mã nguồn mở, ưu tiên self-hosted, extension-first, dùng được thực tế và có thể mở rộng quy mô**; bao quát Unified API, routing, policies, monitoring, cost control, MCP tools, agent/A2A connectivity, Web Console và CLI. | `CONFIRMED_BY_OWNER` | Prompt ASTRA A00 |
| **DEC-002** | Cơ chế mở rộng độc lập | Người dùng tự cài **Provider Adapters và các loại extension khác** từ marketplace, Git repository, URL/gói cài đặt hoặc private registry mà không cần Tanpopo Core phát hành bản cập nhật mới. Extension phải có contract, SDK, lifecycle, security isolation, compatibility tests và quyền hạn rõ ràng. | `CONFIRMED_BY_OWNER` | Prompt ASTRA A00 |
| **DEC-003** | Tách biệt 4 lớp kiến trúc | **MCP, A2A, Provider và Plugin** là 4 lớp khái niệm độc lập, không nhập làm một trong mô hình nghiệp vụ, giao thức hay thiết kế hệ thống. | `CONFIRMED_BY_OWNER` | Prompt ASTRA A00 |
| **DEC-004** | Chính sách Official vs Community & Tuân thủ | Official adapters ưu tiên APIs và phương thức truy cập chính thức của provider; Community/Private adapters do bên thứ ba tự phát triển và phân phối. SDK có thể hỗ trợ API Key, OAuth được cấp phép, identity/credential types và custom auth. Không coi plugin cộng đồng hoặc việc user chấp nhận rủi ro là bằng chứng hợp pháp hóa consumer subscriptions thành general-purpose API; Tanpopo không tự duy trì connector trái điều khoản. | `CONFIRMED_BY_OWNER` | Prompt ASTRA A00 |
| **DEC-005** | Kho mã nguồn & Định danh Git | Làm việc trên repository chính thức `https://github.com/nguywnben/tanpopo` (nhánh `main`), cấu hình Git author `Nguyễn Công Ben <nben940665@gmail.com>`. | `CONFIRMED_BY_OWNER` & `VERIFIED_IN_REPO` | Git remote, commit `0a35b74`, `6b73bcd` |
| **DEC-006** | Công cụ quy trình phát triển cho Agent | Cài đặt cục bộ bộ 25 kỹ năng kỹ thuật phần mềm từ `addyosmani/agent-skills` (`https://skills.addy.ie/`) vào `.agents/skills/` kèm tài liệu tham chiếu `.agents/references/` và personas `.agents/agents/`. | `CONFIRMED_BY_OWNER` & `VERIFIED_IN_REPO` | `skills-lock.json`, `.agents/`, commit `6b73bcd` |
| **DEC-007** | Khởi tạo Design System & Cảm quan màu sắc mặc định | Đã cài đặt `@astryxdesign/core`, `@stylexjs/stylex`, `@astryxdesign/theme-neutral`, `@astryxdesign/cli`, chạy `astryx init` (`AGENTS.md`, `GEMINI.md`) và chọn gói cảm quan giao diện mặc định là **`neutral`** (`astryx-themes.js`). **Lưu ý phạm vi:** Quyết định này chỉ thiết lập thư viện/theme nền cho phần UI sử dụng Astryx, **chưa khóa** kiến trúc hệ thống tổng thể, ngôn ngữ lập trình Core hay cấu trúc thư mục dự án. | `CONFIRMED_BY_OWNER` & `VERIFIED_IN_REPO` | `package.json`, `astryx-themes.js`, `AGENTS.md`, phiên hỏi đáp look-and-feel |

---

## Phần B — Các hướng đi đã bị bác bỏ (Rejected Directions)

| Mã | Hướng đi bị bác bỏ | Lý do bác bỏ | Trạng thái |
|---|---|---|---|
| **REJ-001** | Thu hẹp Tanpopo thành công cụ gom/xoay vòng AI API keys đơn thuần. | Trái với định vị nền tảng AI Gateway toàn diện (routing, policies, monitoring, cost, MCP, A2A, extensions). | `REJECTED` |
| **REJ-002** | Đóng cứng danh sách AI Providers vào mã nguồn Tanpopo Core (mỗi provider mới cần Core release mới). | Xóa bỏ khác biệt cốt lõi `extension-first` của Tanpopo. | `REJECTED` |
| **REJ-003** | Tự duy trì connector trái điều khoản hoặc dùng miễn trừ trách nhiệm ("user tự chịu rủi ro") / plugin cộng đồng để hợp thức hóa việc biến gói thuê bao cá nhân (ChatGPT Plus/Pro, Codex, Antigravity...) thành general-purpose API. | Rủi ro pháp lý, vi phạm TOS nhà cung cấp và đi ngược định hướng chính thống của dự án. | `REJECTED` |
| **REJ-004** | Gộp chung MCP, A2A, Provider và Plugin thành một khái niệm hoặc một bảng/hợp đồng vạn năng duy nhất. | Làm hỏng biên giới giao thức, vòng đời trạng thái và mô hình phân quyền bảo mật. | `REJECTED` |

---

## Phần C — Danh mục Câu hỏi Mở có tính chất Chặn (Blocking Open Questions)

> Các câu hỏi dưới đây có tác động kiến trúc sâu rộng (chi phí đảo ngược cao nếu đoán sai). Agent **không tự suy diễn** và trình bày rõ phương án đề xuất (`PROPOSED`) kèm đánh đổi để Product Owner chốt ở bước tiếp theo.

---

### OQ-001: Ngôn ngữ & Runtime chủ đạo cho Tanpopo Core (Data Plane + Control Plane)
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — Phương án A)
- **Tác động:** Quyết định hiệu năng proxy streaming (SSE/WebSocket/HTTP2), mức tiêu thụ RAM khi tự host, khả năng đóng gói 1 file thực thi/1 Docker image gọn nhẹ, và cách phân chia thư mục repository (hiện `package.json` đang nằm ở thư mục gốc).
- **Các lựa chọn & Đánh đổi (Tradeoffs):**
  1. **Phương án A — Go Core (`Tanpopo Daemon/CLI`) + TypeScript/React/Astryx cho Web Console:**
     - *Ưu điểm:* Đóng gói single binary cực gọn cho người dùng self-hosted (có thể nhúng tĩnh bản build Web Console vào trong binary Go); goroutine xử lý hàng chục nghìn luồng SSE/streaming đồng thời với độ trễ proxy tính bằng sub-millisecond và RAM thấp; hệ sinh thái cloud-native/networking/OTel rất mạnh.
     - *Nhược điểm:* Tồn tại ranh giới hai ngôn ngữ giữa Core (Go) và Web Console (TypeScript); Plugin SDK đa ngôn ngữ cần giao tiếp qua biên giới chuẩn (như WebAssembly hoặc IPC/gRPC) thay vì `import()` trực tiếp.
  2. **Phương án B — Rust Core + TypeScript/React/Astryx cho Web Console:**
     - *Ưu điểm:* Hiệu năng tối đa, không có trễ GC, tích hợp runtime Wasm (`wasmtime`) ở mức nguyên bản tốt nhất.
     - *Nhược điểm:* Tốc độ phát triển tính năng nghiệp vụ ở Control Plane (OAuth, RBAC, CRUD, MCP/A2A) chậm hơn đáng kể; rào cản đóng góp mã nguồn mở cao hơn so với Go/TypeScript.
  3. **Phương án C — Full-stack TypeScript (Node.js >= 22 hoặc Bun) cho cả Core, CLI, SDK và Web Console:**
     - *Ưu điểm:* Đồng nhất 100% ngôn ngữ và kiểu dữ liệu; tốc độ dựng nguyên mẫu và SDK cho cộng đồng JS/TS nhanh nhất; tận dụng tốt các thư viện MCP hiện có.
     - *Nhược điểm:* Áp lực bộ nhớ và độ trễ đuôi (p99 latency) trên luồng đơn khi tải streaming lớn; việc đóng gói single-binary đa nền tảng nặng nề hơn; **rủi ro bảo mật cao nếu nạp plugin bằng JS `import()` trong cùng tiến trình** (vẫn bắt buộc dùng Wasm hoặc tiến trình con cách ly).
- **Khuyến nghị của Principal Architect (`PROPOSED`):** Chọn **Phương án A (Go cho Core Gateway & CLI + TypeScript/Astryx cho Web Console trong cấu trúc monorepo phân tách rõ `core/` và `web/` hoặc `apps/console/`)** để đạt chuẩn vàng về trải nghiệm self-hosted gọn nhẹ lẫn khả năng chịu tải thực tế.

---

### OQ-002: Kiến trúc thực thi & Cô lập bảo mật cho Plugin / Extension (Plugin Runtime & Isolation Model)
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — Phương án C)
- **Tác động:** Quyết định trực tiếp tính khả thi của khác biệt cốt lõi (`extension-first` có `security isolation`, `explicit permissions`, `SDK`, cài từ Git/URL/registry mà không sợ lộ master API keys của hệ thống).
- **Các lựa chọn & Đánh đổi (Tradeoffs):**
  1. **Phương án A — Thuần WebAssembly Sandbox (Wasm Component Model / WIT / Extism):**
     - *Ưu điểm:* Cô lập bộ nhớ tuyệt đối theo cơ chế capability-based (plugin không thể tự mở socket mạng hay đọc biến môi trường nếu Host không cấp quyền theo manifest); một tệp `.wasm` chạy trên mọi hệ điều hành/kiến trúc CPU; tác giả có thể viết plugin bằng TS, Go, Rust, Python.
     - *Nhược điểm:* Thiết kế giao diện truyền luồng dữ liệu thời gian thực (bidirectional streaming SSE/chunked) qua biên giới Host-Wasm phức tạp hơn; các provider đơn giản cũng phải qua bước biên dịch ra `.wasm`.
  2. **Phương án B — Tiến trình con tách biệt (Out-of-process Subprocess qua gRPC / stdio JSON-RPC kiểu `go-plugin`):**
     - *Ưu điểm:* Hỗ trợ mạng và streaming bất đồng bộ tự nhiên; dễ gỡ lỗi.
     - *Nhược điểm:* Khó ép buộc giới hạn mạng/tệp tin đồng nhất trên cả Linux, macOS, Windows nếu không có container lồng nhau; tốn RAM trên mỗi tiến trình plugin; phân phối tệp thực thi đa nền tảng từ Git/URL phức tạp.
  3. **Phương án C — Mô hình Hai Tầng Kết Hợp (Two-Tier Hybrid: Declarative Manifest + Wasm Capability Sandbox):**
     - *Tầng 1 (Declarative Provider/Extension Pack — YAML/JSON chuẩn hóa):* Dành cho ~70–80% các provider chuẩn REST/SSE tương thích họ OpenAI hoặc biến đổi header/payload/model-map/pricing đơn giản. **Không thực thi mã tùy ý** → bề mặt tấn công bằng 0, cài đặt tức thì từ Git/URL.
     - *Tầng 2 (Programmable Wasm Sandbox Plugin):* Dành cho các Provider có giao thức/luồng xác thực đặc thù, custom auth doanh nghiệp, bộ lọc guardrail/policy tùy biến. Chạy trong Wasm sandbox có giới hạn CPU/RAM, danh sách trắng tên miền mạng (`allowed_hosts`) và cơ chế tiêm thông tin xác thực qua host proxy (plugin không cần nhìn thấy khóa gốc nếu không được cấp quyền).
- **Khuyến nghị của Principal Architect (`PROPOSED`):** Chọn **Phương án C (Mô hình Hai Tầng Kết Hợp)**. Tầng 1 giúp cộng đồng tạo adapter mới trong vài phút mà tuyệt đối an toàn; Tầng 2 giải quyết triệt để bài toán mở rộng phức tạp với chuẩn cô lập bảo mật nghiêm ngặt.

---

### OQ-003: Hợp đồng Unified API & Ranh giới định tuyến Giao thức (API & Protocol Surface)
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — Phương án A)
- **Tác động:** Định hình cách ứng dụng khách (SDK có sẵn, IDE, frameworks) giao tiếp với Tanpopo mà không bị mất mát tính năng đặc thù của từng mô hình hay trộn lẫn 4 lớp `Provider`, `Plugin`, `MCP`, `A2A`.
- **Các lựa chọn & Đánh đổi (Tradeoffs):**
  1. **Phương án A — Đa mặt phẳng tách biệt (Multi-Plane Ingress: OpenAI-Compatible Unified LLM API + Native Dialect Passthrough + Dedicated MCP Plane + Dedicated A2A Plane):**
     - *Mặt phẳng Model/Provider:* Hỗ trợ chuẩn tương thích OpenAI (`/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/models`) làm giao diện hợp nhất mặc định, **đồng thời** cho phép định tuyến giữ nguyên phương ngữ gốc (ví dụ `/v1/messages` của Anthropic, phương ngữ Gemini) đi qua cùng đường ống Auth/Policy/Cost/Telemetry để không làm rơi tính năng chuyên sâu (caching, thinking blocks, multimodal).
     - *Mặt phẳng MCP:* Endpoint riêng (ví dụ `/mcp/...` theo chuẩn Streamable HTTP/SSE) quản lý danh mục tool, lọc quyền cấp tool và nhật ký gọi tool.
     - *Mặt phẳng A2A:* Endpoint riêng phục vụ Agent Card discovery và vòng đời tác vụ liên agent (JSON-RPC/SSE).
     - *Ưu điểm:* Tương thích tức thì với 95% công cụ hiện hữu, không làm suy giảm tính năng mô hình mới, giữ đúng nguyên tắc tách biệt 4 lớp.
     - *Nhược điểm:* Đội ngũ phát triển phải thiết kế mô hình sự kiện nội bộ (internal telemetry/cost envelope) đủ tổng quát cho cả 3 mặt phẳng lưu lượng.
  2. **Phương án B — Chỉ duy nhất một chuẩn Schema độc quyền của Tanpopo cho mọi tác vụ:**
     - *Ưu điểm:* Đơn giản hóa bộ chuyển đổi nội bộ.
     - *Nhược điểm:* Buộc mọi người dùng phải sửa code hoặc cài thêm client SDK riêng của Tanpopo; ma sát áp dụng thực tế rất lớn.
- **Khuyến nghị của Principal Architect (`PROPOSED`):** Chọn **Phương án A**.

---

### OQ-004: Mô hình Lưu trữ & Topo Triển khai Self-Hosted (Storage & Scalability Topology)
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — Phương án A)
- **Tác động:** Giải quyết bài toán cân bằng giữa *"dễ tự host thực tế"* (không ép người dùng cá nhân phải dựng cụm 4–5 dịch vụ cơ sở dữ liệu) và *"có thể mở rộng quy mô"* (chạy nhiều bản sao chịu tải cao trong doanh nghiệp).
- **Các lựa chọn & Đánh đổi (Tradeoffs):**
  1. **Phương án A — Kiến trúc Hai Chế độ Lưu trữ qua Giao diện Trừu tượng (Standalone Embedded -> Clustered Scale-out):**
     - *Chế độ Standalone (Mặc định):* Chạy độc lập không phụ thuộc ngoài nhờ cơ sở dữ liệu nhúng (SQLite ở chế độ WAL + bộ đếm bộ nhớ trong + mã hóa khóa bằng master key cục bộ) và hỗ trợ khai báo cấu hình dạng tệp (declarative YAML/TOML cho GitOps).
     - *Chế độ Scale-out / HA:* Nhiều node Data Plane không trạng thái (stateless) dùng chung PostgreSQL (cấu hình, khóa, sổ cái chi phí) + Redis/Valkey (rate-limit phân tán, kiểm soát hạn mức thời gian thực) + xuất chuẩn OpenTelemetry (OTLP) ra hệ thống giám sát ngoài.
     - *Ưu điểm:* Trải nghiệm khởi chạy đầu tiên dưới 60 giây nhưng không bị kịch trần khi đưa lên môi trường production lớn.
     - *Nhược điểm:* Cần kỷ luật trừu tượng hóa tầng lưu trữ (`Store` / `RateLimiter` interface) ngay từ đầu, tránh viết truy vấn phụ thuộc cứng vào một hệ quản trị duy nhất.
  2. **Phương án B — Bắt buộc PostgreSQL + Redis ngay từ bản cài đặt tối thiểu:**
     - *Ưu điểm:* Chỉ cần duy trì một nhánh triển khai lưu trữ.
     - *Nhược điểm:* Cản trở mạnh mẽ việc dùng thử và tự host quy mô nhỏ.
- **Khuyến nghị của Principal Architect (`PROPOSED`):** Chọn **Phương án A**.

---

### OQ-005: Cơ chế Thực thi Quản trị đối với Community Plugins & Ranh giới Tuân thủ (Registry & Runtime Governance)
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — Phương án A)
- **Tác động:** Chuyển hóa nguyên tắc chính sách đã chốt ở **DEC-004** thành hành vi kỹ thuật rõ ràng trong Marketplace, SDK, tài liệu và Runtime mà không mâu thuẫn với tính mở của phần mềm self-hosted.
- **Các lựa chọn & Đánh đổi (Tradeoffs):**
  1. **Phương án A — Kiểm duyệt chặt ở Kênh Chính thức + Runtime Trung lập Minh bạch Quyền (Strict Official Curation + Transparent Neutral Runtime):**
     - Kho chính thức (Official Marketplace / Official Adapters) chỉ chấp nhận, ký số (`official` / `verified`) và hiển thị các extension dùng API/phương thức xác thực hợp lệ, có kiểm tra tuân thủ rõ ràng.
     - Tài liệu, ví dụ mẫu và thông điệp sản phẩm tuyệt đối không hướng dẫn hay cổ súy việc dùng consumer subscriptions làm API.
     - Ở phía máy chủ tự vận hành của người dùng, khi quản trị viên chủ động cài extension từ URL/Git/private registry riêng (ví dụ cổng AI nội bộ doanh nghiệp dùng custom SSO/OAuth), Runtime chỉ kiểm tra tính hợp lệ kỹ thuật (chữ ký gói nếu bật policy nội bộ, hợp đồng schema, quyền mạng/bí mật khai báo trong manifest) và gắn nhãn nguồn gốc `unverified / external` rõ ràng trong nhật ký kiểm toán.
     - *Ưu điểm:* Bảo vệ trọn vẹn tính chính danh pháp lý của dự án Tanpopo, đồng thời không làm hỏng khả năng tích hợp hệ thống nội bộ hợp pháp của doanh nghiệp tự host.
  2. **Phương án B — Đưa danh sách chặn cứng (Hardcoded Blocklist) các tên miền/header vào mã nguồn Core:**
     - *Ưu điểm:* Thể hiện sự ngăn chặn chủ động ở tầng runtime.
     - *Nhược điểm:* Không hiệu quả thực chất trong phần mềm mã nguồn mở (dễ dàng sửa/fork), tốn công chạy đua cập nhật danh sách chặn và có nguy cơ chặn nhầm các tích hợp OAuth/Enterprise hợp lệ được nhà cung cấp cấp phép trong tương lai.
- **Khuyến nghị của Principal Architect (`PROPOSED`):** Chọn **Phương án A**.

---

### OQ-006: Phân kỳ Lộ trình (Phasing / MVP Vertical Slice) cho Giai đoạn Thiết kế Sản phẩm (A01+)
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — Phương án A)
- **Tác động:** Ngăn chặn rủi ro dàn trải làm cùng lúc 8 phân hệ lớn khiến dự án không thể đạt trạng thái *"dùng được thực tế"* sớm, nhưng vẫn bảo đảm kiến trúc nền không phải đập đi xây lại khi thêm MCP và A2A.
- **Các lựa chọn & Đánh đổi (Tradeoffs):**
  1. **Phương án A — Thiết kế Mô hình Nền tảng Đủ Rộng, Triển khai Cuốn chiếu theo 3 Nhịp (Foundation-First Phased Delivery):**
     - *Thiết kế khung (Architecture & Domain Model):* Định nghĩa sẵn ranh giới cho cả 4 lớp (`Provider`, `Plugin`, `MCP`, `A2A`) ngay từ bản thiết kế kiến trúc tổng thể.
     - *Nhịp 1 (v0.1 — Usable Core + Extension Engine):* Unified Model API + Plugin Contract/SDK/Sandbox + 2–3 Official Reference Adapters + Quản lý Credential/Virtual Keys + Routing/Basic Policy + Cost/Telemetry cơ bản + CLI & Web Console tối thiểu (cài plugin, cấu hình provider, tạo key, xem log/chi phí).
     - *Nhịp 2 (v0.2 — MCP Plane & Advanced Policies):* Tích hợp cổng kết nối/quản trị MCP Tools, bộ lọc bảo mật nâng cao, private registry.
     - *Nhịp 3 (v0.3 — A2A Plane & Scale-out HA):* Kết nối liên tác nhân A2A, chế độ cụm PostgreSQL/Redis HA, kiểm toán doanh nghiệp nâng cao.
  2. **Phương án B — Triển khai đồng loạt mọi mặt phẳng (Provider + MCP + A2A + Marketplace đầy đủ) ngay trong bản phát hành đầu tiên:**
     - *Ưu điểm:* Ra mắt đủ mặt tính năng trên lý thuyết ngay từ đầu.
     - *Nhược điểm:* Chu kỳ phát triển kéo dài, độ phức tạp kiểm thử tích hợp tăng theo cấp số nhân, rủi ro chất lượng thực tế thấp ở từng phân hệ.
- **Khuyến nghị của Principal Architect (`PROPOSED`):** Chọn **Phương án A**.

---

### OQ-007: Chuẩn hóa Giấy phép Mã nguồn & Tổ chức Cấu trúc Thư mục cho Web Console đã khởi tạo
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — Phương án A)
- **Tác động:** Giải quyết mâu thuẫn thực tế đang tồn tại trong repository (`VERIFIED_IN_REPO`) trước khi khởi tạo cấu trúc dự án chính thức.
- **Hiện trạng cần xử lý:**
  1. Tệp [`LICENSE`](../../LICENSE) ở gốc là **Apache-2.0**, nhưng [`package.json`](../../package.json) đang ghi `"license": "ISC"`.
  2. Các tệp giao diện thử nghiệm ([`package.json`](../../package.json), [`astryx-themes.js`](../../astryx-themes.js), [`src/App.tsx`](../../src/App.tsx), [`src/main.tsx`](../../src/main.tsx)) đang đặt ngay thư mục gốc và **chưa có công cụ đóng gói/chạy thử (bundler/dev server/TypeScript config)**.
- **Các lựa chọn & Đánh đổi (Tradeoffs):**
  1. **Phương án A:** Xác nhận **Apache-2.0** là giấy phép chính thức duy nhất của dự án Tanpopo; giữ nguyên cấu hình Astryx + theme `neutral` hiện tại và dời gọn phần Web Console vào thư mục con chuyên biệt (ví dụ `web/` hoặc `apps/console/`) kèm trình đóng gói chuẩn (như Vite + StyleX plugin theo tài liệu Astryx) **sau khi chốt OQ-001**.
  2. **Phương án B:** Đổi giấy phép dự án sang MIT/ISC hoặc giữ cấu trúc phẳng tại thư mục gốc.
- **Khuyến nghị của Principal Architect (`PROPOSED`):** Chọn **Phương án A**.
