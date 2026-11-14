# Decision Register & Blocking Open Questions — Tanpopo

> **Tài liệu nguồn (Canonical Decision Log — Updated through Phase A08 Independent Audit)**
> - **Ngày cập nhật:** `2026-11-14`
> - **Nguyên tắc quản trị:** Không một gợi ý nào từ AI Agent/Assistant được phép nâng trạng thái lên `CONFIRMED_BY_OWNER`, `ACCEPTED` hoặc `APPROVED` nếu Product Owner chưa trực tiếp phê duyệt. Toàn bộ phạm vi sản phẩm (`G1`/`G2`), đặc tả hợp đồng (`Baseline A04`), kiến trúc kỹ thuật/ADRs (`Gate G3 — ADR-0001..0006`), thiết kế UX/Ops (`Baseline A06`) và chiến lược Bảo mật/Kiểm thử (`Baseline A07`) hiện đều ở trạng thái **`READY_FOR_REVIEW` / `PROPOSED`**; kết quả kiểm toán độc lập **ASTRA A08** đánh giá **`Specification Completeness = FAIL`** do còn các lỗ hổng chặn (`GAP-01`..`13`) và **10 Yêu cầu Thay đổi (`CR-01`..`CR-10`)** chờ Product Owner quyết định tại [`unresolved-gaps.md`](../reviews/unresolved-gaps.md).

---

## Bảng Theo dõi Cổng Phê duyệt Giai đoạn (Stage-Gate Status Tracker)

| Cổng / Pha | Pha tương ứng | Nội dung kiểm định | Trạng thái hiện tại | Ghi chú quản trị |
|---|---|---|---|---|
| **Gate G0** | **ASTRA A00** — Tiếp nhận & Chuẩn hóa Bối cảnh | Kiểm kê hiện trạng repo, xác minh 3 nhiệm vụ setup, tách bạch quyết định thật và giả định. | `COMPLETED` | Đã thiết lập baseline bối cảnh tại `docs/governance/*` và `docs/README.md`. |
| **Gate G1** | **ASTRA A01** — Product Discovery | Định vị sản phẩm, phân khúc người dùng ưu tiên, đối thủ, cơ hội/khả thi, rủi ro pháp lý/TOS, thước đo thành công, phạm vi release và 5 quyết định chiến lược (`SD-G1-01`..`SD-G1-05`). | **`READY_FOR_REVIEW`** | **Product Owner chưa phê duyệt.** Giữ nguyên trạng thái chờ duyệt, không tự động nâng lên `APPROVED`. |
| **Baseline A02** | **ASTRA A02** — Phân tích Nghiệp vụ | 12 tác nhân, bản đồ thực thể, 8 máy trạng thái (`SM-01`..`08`), 13 hành trình (`WF-01`..`13`), 30 quy tắc `TP-BR-0101`..`0803`, `OPS-01`..`09`, từ điển chuẩn và `BA-OQ-001`..`005`. | `DRAFT_COMPLETED` | Đã tích hợp và truy vết 100% sang bộ PRD/SRS, Kiến trúc Hệ thống và UX/Ops. |
| **Gate G2** | **ASTRA A03** — Product Definition, PRD/SRS & Đặc tả Tính năng | Bộ PRD, Danh mục 16 tính năng theo 10 miền (`FT-01`..`16`), Phạm vi phát hành (`MVP v0.1` → `v1.0` → `Post-v1`), SRS (`TP-FR-0101`..`1005`, `TP-NFR-0101`..`1002`), Tiêu chí nghiệm thu (`TP-AC-*`), Ma trận truy vết và 6 đánh đổi (`TD-G2-01`..`06`). | **`READY_FOR_REVIEW`** | **Bản nháp `DRAFT — UNLOCKED SCOPE`**; đã vượt qua toàn bộ tự kiểm tra Gate G2; chờ Product Owner phê duyệt. |
| **Baseline A04** | **ASTRA A04** — Đặc tả Sâu Extension Platform & Connectivity | Bộ 6 đặc tả `SPEC-EXT-01`..`06` (`docs/specifications/*`) và Ma trận kiểm thử hợp đồng `SPEC-QA-01` (`CT-01`..`24`, `EXT-ACCEPT-01`): 3 tầng thực thi, kho khóa mù, Marketplace, MCP Hub/A2A và tương thích giao thức. | **`READY_FOR_REVIEW`** | **Đặc tả ứng viên minh họa (`ILLUSTRATIVE — PRE-ADR & PRE-POC`)**; gắn liền với các điểm chờ PoC/chính sách hãng (`POC-01`..`03`, `POL-01`..`02`). |
| **Gate G3** | **ASTRA A05** — Solution Architecture & Technical Design | Kiến trúc tổng thể `ARCH-SYS-01`, Lược đồ vật lý hai chế độ `ARCH-DATA-01`, Topo cụm HA `ARCH-OPS-01`, Đặc tả định tuyến/chính sách `SPEC-CORE-01`, Sổ cái `micro-USD`/OTel `SPEC-CORE-02`, Hợp đồng OpenAPI/JSON Schema/Protobuf `SPEC-API-01`, giao thức PoC `SPIKE-01`..`04` và bộ **6 ADRs (`ADR-0001`..`0006`)**. | **`READY_FOR_REVIEW`** | **Thiết kế Đề xuất (`PROPOSED — PRE-APPROVAL`)**. Đã vượt qua toàn bộ 6 mục tự kiểm tra kiến trúc A05; **chưa viết code ứng dụng hay code PoC** chờ Product Owner phê duyệt. |
| **Baseline A06** | **ASTRA A06** — UX/UI, Developer Experience & Vận hành SRE | Bộ 4 đặc tả UX/UI `SPEC-UX-01`..`04` (`docs/ux/*`: 11 phân khu IA, 8 hành trình `UJ-01`..`08` đủ 5 trạng thái, 16 màn hình `SCR-01`..`16`, 6 rào chắn `UX-GR`, tích hợp Astryx `neutral` & `UX-AC-01`..`12`) và 4 đặc tả DX/Vận hành `SPEC-OPS-01`..`04` (`docs/operations/*`: cài đặt đa nền tảng/Air-Gap, sao lưu/DR `RTO < 15m`, giám sát/7 sổ tay sự cố `INC-01`..`07`, `tanpopo` CLI/SDK & `OPS-AC-01`..`12`). | **`READY_FOR_REVIEW`** | **Thiết kế Trải nghiệm & Vận hành Đề xuất (`PROPOSED — NO CODE`)**. Giữ nguyên `neutral` mặc định (`DEC-007`), **không khóa theme chưa duyệt, không chạy `theme eject` và chưa lập trình frontend/backend**. |
| **Gate G3 (Tổng hợp A05–A07)** | **ASTRA A07** — Security, Reliability & Verification Strategy | Bộ 3 đặc tả bảo mật `SPEC-SEC-01`..`03` (`docs/security/*`: Threat Model 13 mối đe dọa `TM-01`..`13`, Sandbox/Supply-Chain & Mã hóa phong bì `AEAD AAD` + cô lập 4 lớp + ranh giới `DEC-004`) và 4 đặc tả chất lượng/độ tin cậy `SPEC-QA-01`..`04` (`docs/quality/*`: `CT-01`..`32`, `AUTH-01`..`16`, `FZ-01`..`06`, CI khép kín `0` gọi AI ngoài, `SLO-01`..`08` `TARGET / UNVERIFIED`, 8 ca `FMEA-01`..`08`, 7 cổng xuất xưởng `GATE-REL-01`..`07` và Gói Trình Duyệt Gate G3). | **`READY_FOR_REVIEW`** | **Chiến lược Bảo mật, Độ tin cậy & Kiểm định Đề xuất (`PROPOSED — NO CODE`)**. **Không tự nâng Gate G3 lên `APPROVED`**; xin phép tường minh Product Owner tại [`release-readiness.md`](../quality/release-readiness.md) Mục 4 trước khi viết bất kỳ dòng mã PoC hay mã ứng dụng nào. |
| **Audit Gate A08** | **ASTRA A08** — Independent Specification & Architecture Audit | Phản biện độc lập ngang + 8 lát cắt dọc trên toàn bộ 54 tài liệu A00–A07 ([`integrated-audit.md`](../reviews/integrated-audit.md) — `AUD-001`..`030`: `11 CRITICAL`, `14 MAJOR`, `5 MINOR`; [`unresolved-gaps.md`](../reviews/unresolved-gaps.md) — `CR-01`..`10` & `GAP-01`..`13`). | **`FAIL` *(Blocking Gaps Open)*** | **Chặn chuyển sang lập kế hoạch đơn vị mã nguồn (`STOP — NO IMPLEMENTATION`)**. Yêu cầu Product Owner chốt `Gate G1–G3` + `CR-01`..`10` và hoàn tất vòng quay lại vá có mục tiêu trên `A02–A07`. |

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
| **DEC-008** | Kỷ luật & Chuẩn Message Git Commit theo từng bước | Thực hiện atomic commit riêng biệt sau khi hoàn thành mỗi bước theo chuẩn Conventional Commits **`<type>: step <NN> - <mô tả>`** (hoặc `<type>(<module>): step <NN> - <mô tả>`, ví dụ `docs: step 03 - ...`; không dùng tiền tố `a` như `a00`/`a01` và không đặt số bước làm tên scope); bảo toàn lịch sử đã push trên `main` (không force-push khi không cần thiết). | `CONFIRMED_BY_OWNER` | Yêu cầu trực tiếp của Product Owner về quy ước commit |

---

## Phần B — Các hướng đi đã bị bác bỏ (Rejected Directions)

| Mã | Hướng đi bị bác bỏ | Lý do bác bỏ | Trạng thái |
|---|---|---|---|
| **REJ-001** | Thu hẹp Tanpopo thành công cụ gom/xoay vòng AI API keys đơn thuần. | Trái với định vị nền tảng AI Gateway toàn diện (routing, policies, monitoring, cost, MCP, A2A, extensions). | `REJECTED` |
| **REJ-002** | Đóng cứng danh sách AI Providers vào mã nguồn Tanpopo Core (mỗi provider mới cần Core release mới). | Xóa bỏ khác biệt cốt lõi `extension-first` của Tanpopo. | `REJECTED` |
| **REJ-003** | Tự duy trì connector trái điều khoản hoặc dùng miễn trừ trách nhiệm ("user tự chịu rủi ro") / plugin cộng đồng để hợp thức hóa việc biến gói thuê bao cá nhân (ChatGPT Plus/Pro, Codex, Antigravity...) thành general-purpose API. | Rủi ro pháp lý, vi phạm TOS nhà cung cấp và đi ngược định hướng chính thống của dự án. | `REJECTED` |
| **REJ-004** | Gộp chung MCP, A2A, Provider và Plugin thành một khái niệm hoặc một bảng/hợp đồng vạn năng duy nhất. | Làm hỏng biên giới giao thức, vòng đời trạng thái và mô hình phân quyền bảo mật. | `REJECTED` |

---

## Phần C — Năm Quyết định Chiến lược Cần Product Owner Phê duyệt tại Gate G1 (`SD-G1-01`..`SD-G1-05`)

> **Trạng thái Gate G1:** `READY_FOR_REVIEW` (Chưa `APPROVED`). Product Owner chỉ cần chốt 5 quyết định chiến lược cấp sản phẩm dưới đây để mở khóa pha Đặc tả Yêu cầu Sản phẩm (PRD) tiếp theo.

### SD-G1-01: Chọn Phân khúc Người dùng Mũi nhọn (Beachhead Persona) & Mô hình Khởi chạy Tự vận hành
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — **Option 1**)
- **Liên kết phân tích:** [`users-jobs-use-cases.md`](../discovery/users-jobs-use-cases.md), `ASM-001`, `OQ-004`.
- **Các lựa chọn:**
  1. **Option 1 (Khuyến nghị):** **Mũi nhọn kép vào Dev Teams (5–50 kỹ sư) & AI Platform Teams (50–500 kỹ sư)** thông qua kiến trúc hai chế độ lưu trữ: mặc định chạy ngay trong 1 lệnh không phụ thuộc ngoài (SQLite nhúng + sẵn Web Console/CLI — thu hút cả nhóm nhỏ lẫn Solo Dev chính thống), và mở rộng ngang liền mạch sang PostgreSQL + Redis khi lên quy mô lớn; hoãn phân khúc Ngân hàng/Chính phủ quy mô >1.000 người đòi hỏi chứng chỉ chuyên biệt sang `v1.0+`.
  2. **Option 2:** **Nhắm thẳng vào Tập đoàn Lớn chịu quản lý chặt (Regulated Enterprise) ngay từ `v0.1`** — bắt buộc cụm PostgreSQL + Redis + Kubernetes + HSM/Vault ngay từ bản cài tối thiểu (hy sinh tốc độ lan tỏa cộng đồng và độ dễ dùng).
  3. **Option 3:** **Chỉ tối ưu cho Cá nhân / Solo Developer chạy cục bộ** — lược bỏ thiết kế phân quyền nhiều nhóm, ngân sách phòng ban và chế độ chạy cụm phân tán (dễ làm nhanh nhưng sớm kịch trần quy mô).
- **Recommendation:** Chọn **Option 1** để vừa đạt tốc độ áp dụng tự nhiên từ dưới lên (`TTFGR < 3 phút`), vừa giữ đúng cam kết *"dùng được thực tế và có thể mở rộng quy mô"*.

---

### SD-G1-02: Mô hình Thực thi Plugin Cô lập & Hạ tầng Phân phối Marketplace
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — **Option 1**)
- **Liên kết phân tích:** [`opportunity-and-feasibility.md`](../discovery/opportunity-and-feasibility.md), `OQ-002`, `RSK-001`, `RSK-004`, `RSK-005`.
- **Các lựa chọn:**
  1. **Option 1 (Khuyến nghị):** **Động cơ Hai Tầng Kết hợp (Declarative Pack cho ~75–80% hãng chuẩn + Wasm Capability Sandbox cho giao thức/xác thực phức tạp) đi kèm Blind Credential Broker và Danh mục Marketplace Tĩnh (Git/OCI/URL + Private Registry)**.
  2. **Option 2:** **Bắt buộc biên dịch mã (Wasm hoặc tiến trình con Subprocess) cho mọi Adapter** và tự vận hành máy chủ Marketplace SaaS động tập trung (rào cản viết adapter mới cao hơn và phát sinh chi phí nuôi máy chủ chợ hàng tháng).
  3. **Option 3:** **Chỉ hỗ trợ cấu hình Khai báo YAML/JSON thuần túy** mà không có môi trường lập trình sandbox (an toàn và nhẹ nhất nhưng bất lực trước các luồng ký số/xác thực nội bộ đặc thù của doanh nghiệp).
- **Recommendation:** Chọn **Option 1** (kiểm chứng kỹ thuật sớm qua `EXP-01` và `EXP-02`) vì giải quyết đồng thời bài toán bảo mật khóa gốc, tốc độ tạo adapter dưới 30 phút và chi phí vận hành hạ tầng gần bằng 0.

---

### SD-G1-03: Phân kỳ Phạm vi Phát hành từ `v0.1` đến `v1.0` (Release Scope & Phasing)
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — **Option 1**)
- **Liên kết phân tích:** [`positioning-and-success-metrics.md`](../discovery/positioning-and-success-metrics.md), `OQ-006`, `RSK-002`.
- **Các lựa chọn:**
  1. **Option 1 (Khuyến nghị):** **Thiết kế Khung Miền 4 Lớp Ngay từ Đầu, Giao hàng Cuốn chiếu 3 Nhịp:**
     - `v0.1`: Core + Unified/Passthrough LLM API + Plugin Engine + 5–6 Official Adapters + Routing/Budget/Telemetry cơ bản + Web Console & CLI cốt lõi.
     - `v0.2`: Mặt phẳng quản trị **MCP Plane** (lọc quyền tool, ghim `schema_hash`, OAuth 2.1 được cấp phép) + Chỉ mục Registry & công cụ replay test.
     - `v0.3`: Mặt phẳng kết nối **A2A Plane** + Chế độ cụm **Scale-out HA** (Postgres/Redis) → tiến tới đóng băng hợp đồng ở `v1.0`.
  2. **Option 2:** **Đưa cả Mặt phẳng MCP vào ngay bản `v0.1` đầu tiên**, chỉ lùi A2A và HA sang `v0.2` (có điểm nhấn MCP sớm ngay ngày ra mắt đầu tiên nhưng kéo dài thời gian phát triển `v0.1` thêm khoảng 40–50%).
  3. **Option 3:** **Làm đồng loạt cả 4 mặt phẳng (`Provider + Plugin + MCP + A2A + HA`) ngay trước khi phát hành bản đầu tiên** (rủi ro trễ tiến độ và loãng chất lượng rất cao — `RSK-002`).
- **Recommendation:** Chọn **Option 1** để sớm có sản phẩm chạy thật kiểm chứng hệ thống Plugin, trong khi kiến trúc nền đã chừa sẵn khớp nối chuẩn cho MCP ở ngay nhịp kế tiếp `v0.2`.

---

### SD-G1-04: Quản trị Hệ sinh thái Extension Bên ngoài & Thực thi Ranh giới Pháp lý/TOS
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — **Option 1**)
- **Liên kết phân tích:** [`opportunity-and-feasibility.md`](../discovery/opportunity-and-feasibility.md), `DEC-004`, `OQ-005`, `RSK-003`.
- **Các lựa chọn:**
  1. **Option 1 (Khuyến nghị):** **Kiểm duyệt Nghiêm ngặt trên Kênh Chính thức + Runtime Tự vận hành Minh bạch Quyền & Nguồn gốc:** Chỉ cấp nhãn `official` / `verified` và liệt kê trên danh mục của Tanpopo các gói tuân thủ API/OAuth hợp lệ (Vùng 1 & Vùng 2); với gói do quản trị viên tự cài từ URL/kho nội bộ, Runtime yêu cầu duyệt quyền mạng/bí mật tường minh, đóng dấu `unverified-external` trong nhật ký kiểm toán, bảo vệ nhãn hiệu chống mạo danh và hỗ trợ danh sách cảnh báo thu hồi (`revocation feed`).
  2. **Option 2:** **Khóa cứng Runtime chỉ cho chạy các gói đã có chữ ký số phê duyệt của dự án Tanpopo** (loại trừ rủi ro plugin lạ nhưng phá vỡ tính tự chủ của doanh nghiệp muốn chạy cổng nội bộ riêng tư không công bố ra ngoài).
  3. **Option 3:** **Nhúng danh sách chặn cứng (Hardcoded Blocklist) tên miền/header vào mã nguồn Core** (dễ bị sửa/fork trong phần mềm nguồn mở và có nguy cơ chặn nhầm tích hợp doanh nghiệp hợp pháp).
- **Recommendation:** Chọn **Option 1** vì bảo vệ vững chắc uy tín pháp lý của dự án Tanpopo theo đúng `DEC-004` mà vẫn giữ trọn giá trị tự chủ cho doanh nghiệp self-hosted.

---

### SD-G1-05: Chiến lược Giấy phép Mã nguồn Mở & Ranh giới Tính năng Tự vận hành
- **Trạng thái:** `OPEN` (Đề xuất: `PROPOSED` — **Option 1**)
- **Liên kết phân tích:** [`market-and-competitors.md`](../discovery/market-and-competitors.md), `C-01`, `ASM-008`, `OQ-007`, `ISS-002`.
- **Các lựa chọn:**
  1. **Option 1 (Khuyến nghị):** **Thuần Apache License 2.0 trên toàn bộ dự án** (Core, CLI, SDK, Official Adapters và Web Console quản trị bao gồm RBAC, ngân sách, audit log, MCP/A2A governance tự host), đồng bộ lại trường `"license": "ISC"` hiện có trong `package.json` về `"Apache-2.0"`.
  2. **Option 2:** **Mô hình Open-Core Hai Giấy phép** — giữ nhân proxy là Apache-2.0 nhưng tách các tính năng RBAC, SSO, Audit Log, Private Registry sang thư mục thương mại đóng giống LiteLLM / Bifrost / Portkey.
  3. **Option 3:** **Giấy phép Source-Available có điều kiện hạn chế thương mại** (kiểu Modified Apache-2.0 như Dify hoặc BSL/AGPLv3).
- **Recommendation:** Chọn **Option 1** để tạo lợi thế cạnh tranh đối kháng trực diện (*counter-positioning*) trước các đối thủ Open-Core đang khóa tính năng quản trị bảo mật tự host sau tường phí Enterprise, giúp xóa bỏ mọi rào cản pháp lý khi các đội kỹ thuật đưa Tanpopo vào doanh nghiệp.

---

## Phần D — Danh mục Câu hỏi Kiến trúc Kỹ thuật Chi tiết (`OQ-001`..`OQ-007`)

> Các câu hỏi kiến trúc thiết lập từ bước A00 dưới đây tiếp tục giữ trạng thái `OPEN` / `PROPOSED` và sẽ được chốt chi tiết cùng bước Kiến trúc Hệ thống sau khi Product Owner duyệt 5 quyết định chiến lược ở Phần C.

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

---

## Phần E — Các Quyết định Nghiệp vụ Bổ sung Cần Làm rõ từ Pha A02 (`BA-OQ-001`..`BA-OQ-005`)

> Chi tiết phân tích nằm tại [`docs/requirements/workflow-requirement-candidates.md`](../requirements/workflow-requirement-candidates.md). Toàn bộ đang ở trạng thái `OPEN` / `PROPOSED` chờ Product Owner quyết định trước khi khóa PRD ở bước A03:

| Mã | Vấn đề nghiệp vụ cần chốt | Phương án Khuyến nghị (`PROPOSED`) | Đánh đổi nếu chọn khác |
|---|---|---|---|
| **BA-OQ-001** | **Đa Tổ chức (`Multi-Organization`) trên cùng một máy chủ tự vận hành** | Giữ thực thể `Organization` trong mô hình gốc, nhưng giai đoạn `0.x` mặc định tối ưu UX cho **1 Organization chứa nhiều Projects & Environments**; mở quản trị nhiều Organizations trên 1 Instance ở `v0.3`/`v1.0`. | Mở đa tổ chức đầy đủ ngay `v0.1` làm phức tạp giao diện quản trị ban đầu cho 95% nhóm tự host đơn vị. |
| **BA-OQ-002** | **Xử lý khi chi phí quyết toán cuối luồng vượt mức giữ chỗ ước tính (`Over-Reservation Overage`)** | Cho phép hoàn tất luồng đang chạy, ghi nhận trung thực số dư âm nhẹ vào sổ cái (`Negative Balance Carryover`) và chặn cứng ngay từ yêu cầu kế tiếp cho tới khi được cấp thêm hạn mức. | Cắt ngang luồng đang truyền ở từ cuối cùng gây trải nghiệm tệ và vẫn bị nhà cung cấp đích tính tiền phần đã sinh. |
| **BA-OQ-003** | **Hành vi đối với các luồng đang mở khi thu hồi `Virtual Key`** | Tách 2 chế độ: *Xoay vòng/Hết hạn chuẩn* cho phép luồng đang dở kết thúc êm trong giới hạn ngắn; *Thu hồi Khẩn cấp An ninh (`Emergency Security Revoke`)* cắt đứt ngay lập tức cả các luồng đang truyền dở. | Nếu chỉ có 1 chế độ duy nhất thì hoặc kém an toàn khi bị tấn công, hoặc gây lỗi oan khi xoay khóa định kỳ. |
| **BA-OQ-004** | **Cơ chế tương tác chờ con người phê duyệt cho công cụ MCP `destructive`** | Hỗ trợ song song: (1) *Chờ đồng bộ ngắn hạn (`<= 60 giây`)* cho tác nhân tương tác trực tiếp, và (2) *Từ chối tạm thời kèm Mã Yêu cầu Phê duyệt trước (`Pre-Approval Token`)* để tác nhân gọi lại sau khi `Reviewer` duyệt qua Console/Webhook. | Nếu chỉ giữ kết nối mạng chờ hàng giờ sẽ làm treo và đứt phiên giao thức của hầu hết MCP Clients hiện tại. |
| **BA-OQ-005** | **Thời hạn bảo toàn tương thích ngược cho Hợp đồng Phần mở rộng (`Deprecation Policy`)** | Giai đoạn `0.x` giữ tương thích tối thiểu 1 bản phụ liền trước (`N-1 minor`); từ `v1.0` trở đi cam kết tương thích ngược tối thiểu **12 tháng (hoặc trọn nhánh `1.x`)** trước khi ngưng hỗ trợ trường hợp đồng cũ. | Thiếu cam kết rõ ràng khiến cộng đồng và đối tác ngại đầu tư viết và duy trì Adapter lâu dài. |

---

## Phần F — Sáu Đánh đổi Trọng yếu Trình Phê duyệt tại Gate G2 (`TD-G2-01`..`TD-G2-06`)

> Phân tích chi tiết nằm duy nhất tại Mục 5 của [`docs/product/release-scope.md`](../product/release-scope.md). Toàn bộ ở trạng thái `OPEN` / `PROPOSED` chờ Product Owner phê duyệt để khóa phạm vi trước khi sang pha Thiết kế Kiến trúc (**A04**):

| Mã | Chủ đề đánh đổi | Đề xuất hiện tại trong PRD/SRS (`PROPOSED`) | Trạng thái |
|---|---|---|---|
| **TD-G2-01** | Ranh giới `MVP (v0.1)` đối với Mặt phẳng MCP | Đặt mặt phẳng lưu lượng MCP (`FT-07`) ở `v0.2` (`Should`), giữ `v0.1` tinh gọn để chốt chất lượng Core + Plugin Engine + LLM Gateway + Console/CLI. | `OPEN` (`PROPOSED`) |
| **TD-G2-02** | Giao diện Suy luận: Dịch chuẩn chung vs Phương ngữ gốc | Kết hợp Unified OpenAI-Compatible (`Must`) + Lossless Native Dialect Passthrough (`Should` ngay `v0.1`) + kiểm tra năng lực nghiêm ngặt trả `400` thay vì cắt tham số ngầm. | `OPEN` (`PROPOSED`) |
| **TD-G2-03** | Kiểm soát Ngân sách Song song vs Trải nghiệm Cuối luồng | Giữ chỗ hạn mức trước khi mở luồng (`Pre-Request Hold`) kết hợp kết chuyển số dư âm nhẹ (`Negative Balance Carryover`) thay vì cắt ngang luồng đang truyền ở vài từ cuối. | `OPEN` (`PROPOSED`) |
| **TD-G2-04** | Mô hình Thực thi Phần mở rộng & Bảo vệ Khóa gốc | Hai tầng kết hợp (`Declarative Pack` + `Default-Deny Sandbox`) đi kèm `Blind Host Credential Broker` ngay từ `v0.1`. | `OPEN` (`PROPOSED`) |
| **TD-G2-05** | Tương tác Phê duyệt Con người cho Công cụ MCP `destructive` | Hỗ trợ chế độ kép: chờ đồng bộ ngắn hạn (`<= 60s`) và cấp `Pre-Approval Token` gắn băm tham số cho phê duyệt bất đồng bộ. | `OPEN` (`PROPOSED`) |
| **TD-G2-06** | Khởi chạy Đơn nút Ngày đầu vs Mở rộng Cụm Đa Bản sao | Mặc định `v0.1` đơn nút kho nhúng (tối ưu 1 `Org` đa `Project`), trừu tượng hóa tầng lưu trữ để mở rộng sang cụm PostgreSQL + Redis & đa `Org` ở `v0.3`/`v1.0`. | `OPEN` (`PROPOSED`) |

---

## Phần G — Các Điểm Kỹ thuật & Chính sách Chưa Thể Kết luận Trước Khi Chạy PoC / Chốt ADR (Từ Pha A04)

> Chi tiết kỹ thuật nằm tại [`extension-contract.md`](../specifications/extension-contract.md) và [`authentication-connections.md`](../specifications/authentication-connections.md). Các mục dưới đây cần được kiểm chứng thực nghiệm trước khi đóng băng chuẩn `Manifest` và `ABI`:

| Mã | Lĩnh vực chưa kết luận | Nguyên nhân phụ thuộc thực tế | Cách giải quyết ở bước tiếp theo |
|---|---|---|---|
| **POC-01** | Chuẩn giao diện nhị phân Wasm cụ thể (`WASI Preview 2 WIT` vs `Extism/Linear-Memory ABI`) | Phụ thuộc hiệu năng truyền luồng SSE tần suất cao (`< 5 ms p95`, `< 1 ms/chunk`) và độ ổn định trình biên dịch TS/Python sang Wasm. | Đo tải thực nghiệm qua PoC `EXP-01` và chốt bằng ADR ở pha Kiến trúc Hệ thống. |
| **POC-02** | Ngôn ngữ ánh xạ an toàn cho Gói Khai báo (Tầng 1) | Cần xác định tập toán tử JSON/template nhỏ nhất vẫn phủ được `>= 75%` nhà cung cấp mà không biến YAML thành ngôn ngữ lập trình rườm rà. | Đối chiếu trên 10 nhà cung cấp mẫu trong `EXP-02` & `EXP-03`. |
| **POC-03** | Giao thức IPC chuẩn cho `Isolated Sidecar` (Tầng 3) | Lựa chọn giữa `gRPC / ConnectRPC` và `HTTP/2 JSON + SSE` qua Unix Domain Socket / mTLS gắn liền với ngôn ngữ nhân Core (`OQ-001`). | Chốt đồng thời cùng ADR chọn ngôn ngữ Core (`OQ-001`). |
| **POL-01** | Danh sách hãng AI thương mại cho phép `Authorized OAuth` / `Device Flow` bên thứ ba | Chính sách cấp phép OAuth thay đổi riêng theo từng hãng; không thể giả định phổ quát. | Chỉ bật trên `Official Adapter` khi có tài liệu điều khoản công khai hợp lệ của chính hãng đó. |
| **POL-02** | Chi tiết đăng ký ứng dụng động trong đặc tả Ủy quyền MCP (`DCR` vs `Client Metadata`) | Chuẩn ủy quyền của hệ sinh thái MCP đang cập nhật nhanh trong năm 2025–2026. | Giữ mô-đun cấu hình máy chủ ủy quyền MCP tách rời để chốt chuẩn mới nhất khi làm `v0.2`. |

---

## Phần H — Sáu Quyết định Kiến trúc Hệ thống (`ADR-0001`..`0006`) & Bốn Giao thức Thử nghiệm (`SPIKE-01`..`04`) Trình Duyệt Tại Gate G3 (Từ Pha A05)

> Toàn bộ tài liệu kiến trúc chi tiết nằm tại [`docs/architecture/adr/`](../architecture/adr/README.md) và [`docs/architecture/system-design.md`](../architecture/system-design.md). Giữ nguyên trạng thái **`PROPOSED`** chờ Product Owner phê duyệt rõ ràng trước khi chuyển sang viết code hoặc chạy PoC thực nghiệm:

| Mã ADR | Quyết định kiến trúc đề xuất (`PROPOSED`) | Câu hỏi mở được giải quyết | Trạng thái & Điều kiện mở khóa |
|---|---|---|---|
| [**`ADR-0001`**](../architecture/adr/0001-core-runtime-and-modular-monolith.md) | Chọn **Go (`1.24+`, `CGO_ENABLED=0`) Modular Monolith** có cờ tách vai trò `--role=all \| data-plane \| control-plane` cho Tanpopo Core & CLI. | `OQ-001` | `PROPOSED` — Cần Owner phê duyệt. |
| [**`ADR-0002`**](../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md) | Thực thi phần mở rộng 3 tầng cô lập (`Declarative` + `Pure-Go Wazero Wasm Sandbox` + `ConnectRPC Isolated Sidecar`) + `Blind Host Credential Broker`; `0` mã không tin cậy chạy in-process. | `OQ-002`, `SD-G1-02`, `POC-01`..`03` | `PROPOSED` — Cần Owner duyệt & cho phép chạy `SPIKE-01`, `SPIKE-02`. |
| [**`ADR-0003`**](../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md) | Lưu trữ hai chế độ sau `storage/port`: **SQLite WAL** (đơn nút) & **PostgreSQL `16+` + Valkey/Redis** (cụm HA, phân vùng bảng theo tháng + `RLS`). | `OQ-004`, `SD-G1-01`, `TD-G2-06` | `PROPOSED` — Cần Owner phê duyệt. |
| [**`ADR-0004`**](../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md) | Phê chuẩn qua đánh giá độc lập stack **SPA tĩnh `TypeScript + React 19 + Vite + StyleX + Astryx v0.7.13 (neutral)`** nhúng vào binary Go (`embed.FS`), dời về `web/console/` và đồng bộ giấy phép **Apache-2.0**. | `OQ-007`, `ASM-003`, `ASM-008`, `ISS-001`..`005` | `PROPOSED` — Cần Owner phê duyệt trước khi dời thư mục gốc. |
| [**`ADR-0005`**](../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md) | Sổ cái ngân sách hai pha số nguyên `BIGINT micro-USD`, gắn nhãn độ chính xác `EXACT / ESTIMATED / UNMETERED_FLAGGED`, tách biệt cứng thanh toán API thương mại khỏi thuê bao cá nhân. | `BA-OQ-002`, `TD-G2-03`, `DEC-004` | `PROPOSED` — Cần Owner duyệt & kiểm chứng `SPIKE-03`. |
| [**`ADR-0006`**](../architecture/adr/0006-immutable-config-snapshots-and-fast-revocation.md) | Ảnh chụp cấu hình bất biến `Copy-on-Write ConfigSnapshot` + truyền tin thu hồi 3 lớp (`Valkey Pub/Sub` + `PG LISTEN/NOTIFY` + nhịp `2s`) đạt SLA `< 1s` / `< 5s` + chế độ lỗi phân tầng. | `BA-OQ-003`, `TP-NFR-0404`, `RSK-011` | `PROPOSED` — Cần Owner phê duyệt. |

---

## Phần I — Mười Đề xuất Thay đổi (`CR-01`..`CR-10`) Từ Kiểm toán Độc lập Bước A08 Trình Product Owner Quyết định

> Phân tích chi tiết các phương án đánh đổi và tiêu chí đóng nằm tại [`docs/reviews/unresolved-gaps.md`](../reviews/unresolved-gaps.md). Kiểm toán viên **không tự ý quyết định** bất kỳ mục nào dưới đây:

| Mã CR | Vấn đề Mâu thuẫn / Lỗ hổng Kiến trúc Cần Chốt | Khuyến nghị Của Kiểm toán viên (`PROPOSED`) | Trạng thái |
|---|---|---|---|
| **`CR-01`** | Danh sách chính xác `Official Reference Adapters` trong `MVP (v0.1)` (`Bedrock` vướng IAM `v0.3`) & điểm cuối `/v1/rerank`. | Khóa `5` hãng ở `v0.1` (`OpenAI`, `Anthropic`, `Gemini`, `OpenAI-Compatible`, `Ollama`); đưa `Bedrock`, `Azure` và `/v1/rerank` sang `v0.2`. | `OPEN` |
| **`CR-02`** | Phân liệt `2-Tier` (`G1/G2/SRS`) vs `3-Tier Isolated Sidecar` (`ADR-0002` thiếu mã `TP-FR-*`). | Giữ đích `3-Tier`, nhưng `v0.1` chỉ xây `Tier-1 + Tier-2`; thêm `TP-FR-0308 (Could — v0.3)` cho `Tier-3 Sidecar`. | `OPEN` |
| **`CR-03`** | Xung đột `3` vs `4` nhãn `Trust Tier` (`PRIVATE_ENTERPRISE`) & tiền tố không gian tên `@tanpopo/*` vs `@tanpopo-official/*`. | Giữ `3` nhãn gốc + cột `enterprise_signed BOOLEAN` (hoặc nâng chính thức `TP-BR-0201` lên `4` nhãn); khóa cả `^@tanpopo(-[a-z0-9]+)?/`. | `OPEN` |
| **`CR-04`** | Lệch `5` vs `6` vai trò `RBAC`, thiếu bảng bật/tắt Plugin cấp `Org` & nghịch lý cổng thoát `v0.1` (`Multi-Org`, `Air-Gap`). | Chốt `6` vai trò RBAC, thêm `org_extension_policies` và hiệu chỉnh cổng thoát `v0.1` khớp phạm vi đơn nút. | `OPEN` |
| **`CR-05`** | `runtime.proto` lỗi biên dịch, thiếu `TransformResponse`, thiếu móc `/native/*`, `32 MB` đa phương thức làm tràn `16 MiB` Wasm & đóng `scoped_secret_unseal`. | Hoàn thiện `runtime.proto`, tách luồng `blob_ref` ngoài bộ nhớ Wasm, cấm tiêm `Cookie` và bỏ `scoped_secret_unseal` khỏi Wasm Tầng 2. | `OPEN` |
| **`CR-06`** | Thiếu `8` bảng SQL, `3` công thức `AEAD AAD` lệch nhau, thiếu `Project DEK` cho `Crypto-Shredding` & đứt chuỗi băm `audit_events` khi ghi HA. | Bổ sung đủ `8` bảng, khóa tổ hợp `(org_id, id)` + `FORCE RLS`, `project_encryption_keys`, `SHA-256` cho khóa ảo và băm `audit_events` theo `org_id` + `JCS`. | `OPEN` |
| **`CR-07`** | Split-brain giữ chỗ ngân sách giữa `Valkey` và `PostgreSQL` + lỗi `CROSSSLOT` trên `Valkey Cluster`. | Khóa lược đồ khóa `tp:v1:{<org_id>}:...` dùng hash-tag `{<org_id>}` và nhịp đồng bộ mức giữ chỗ xuống PostgreSQL. | `OPEN` |
| **`CR-08`** | HTTP `202` + `X-Tanpopo-Approval-Token` phá vỡ trình khách MCP chuẩn (`Claude Code`/`Cursor`) & gộp chung `SM-06` với `A2A Task`. | Dùng phản hồi JSON-RPC chuẩn của MCP cho phê duyệt bất đồng bộ và tách máy trạng thái `SM-09` riêng cho `A2A`. | `OPEN` |
| **`CR-09`** | Thiếu lệnh cứu hộ khi quên mật khẩu quản trị (`Break-Glass`), thiếu `stream_options.include_usage`, chưa định nghĩa `Webhooks` & `Response Cache`. | Thêm `tanpopo admin reset-local-owner` + `include_usage` vào `v0.1`; xếp `Webhooks` vào `v0.2` và `Response Cache` vào `v0.3+`. | `OPEN` |
| **`CR-10`** | Thiếu `51/57` endpoint OpenAPI quản trị, xung đột `Playground`, `SSRFGuard` chặn `Ollama` ở `UJ-01`, 2 tên nhị phân CLI & phông chữ `Air-Gap` Astryx. | Hoàn thiện `SPEC-API-01`, cấp khóa phiên thử nghiệm `Playground`, mở xác nhận loopback trên Wizard, chuẩn hóa 1 nhị phân `tanpopo` và Vite alias phông `Figtree`. | `OPEN` |



