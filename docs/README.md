# Tanpopo Documentation & Context Hub (Updated through Phase A04 — `DRAFT / UNLOCKED SCOPE`)

Chào mừng đến với trung tâm tài liệu chuẩn hóa bối cảnh, khám phá sản phẩm, phân tích nghiệp vụ, đặc tả yêu cầu và đặc tả nền tảng mở rộng của dự án **Tanpopo** (`https://github.com/nguywnben/tanpopo`). Bộ tài liệu này được thiết lập qua các bước **ASTRA A00**, **A01 (Product Discovery)**, **A02 (Business Analysis)**, **A03 (Product Definition, PRD/SRS & Feature Specs)** và **A04 (Extension Platform & Connectivity Deep Specifications)** nhằm tạo dựng nguồn sự thật duy nhất (Single Source of Truth) cho con người và các AI Agents ở những bước tiếp theo (**A05+**) làm việc chính xác mà không phải đoán mò.

> **Trạng thái Cổng Giai đoạn Hiện tại:**
> - **Gate G0 (ASTRA A00 — Chuẩn hóa bối cảnh):** `COMPLETED`
> - **Gate G1 (ASTRA A01 — Product Discovery):** **`READY_FOR_REVIEW`** *(Product Owner chưa duyệt `SD-G1-01`..`SD-G1-05`; không tự đánh dấu `APPROVED`).*
> - **Baseline A02 (ASTRA A02 — Phân tích nghiệp vụ):** `DRAFT_COMPLETED` *(Đã tích hợp và truy vết 100% sang A03/A04).*
> - **Gate G2 (ASTRA A03 — Product Definition, PRD/SRS & Feature Specs):** **`READY_FOR_REVIEW`** *(Bộ đặc tả `DRAFT — UNLOCKED SCOPE` kèm 6 đánh đổi `TD-G2-01`..`06`; chờ Product Owner phê duyệt Gate G1 & Gate G2 trước khi khóa cứng phạm vi).*
> - **Baseline A04 (ASTRA A04 — Extension Platform & Connectivity Specs):** **`READY_FOR_REVIEW`** *(Bộ đặc tả kỹ thuật sâu `ILLUSTRATIVE CANDIDATE — PRE-ADR & PRE-POC` cho Extension Platform, Auth/Connections, Marketplace, MCP/A2A, API Protocols và Contract Test Matrix; kèm các điểm chờ PoC/chính sách `POC-01`..`03` và `POL-01`..`02`).*

---

## 1. Bản đồ đọc tài liệu cho các phiên / Prompt tiếp theo (Reading Guide)

Trước khi thực hiện bất kỳ bước thiết kế kiến trúc nội bộ, lập kế hoạch hay viết code nào cho Tanpopo, hãy đọc theo đúng thứ tự sau:

### 1.1. Tầng Quản trị & Từ điển Chuẩn (`docs/governance/` & `docs/product/glossary.md`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **1** | [`docs/governance/project-charter.md`](./governance/project-charter.md) | Tầm nhìn sản phẩm, khác biệt `extension-first`, sự phân tách 4 lớp (`Provider` / `Plugin` / `MCP` / `A2A`), chính sách `Official vs Community` & ranh giới không hợp thức hóa consumer subscriptions. | **Luôn đọc đầu tiên** trong mọi phiên làm việc mới. |
| **2** | [`docs/governance/decisions.md`](./governance/decisions.md) | Trạng thái các Cổng (`G0`, `G1`, `G2`, `Baseline A04`), quyết định đã chốt (`DEC-001`..`DEC-008`), hướng đã bác bỏ (`REJ-001`..`REJ-004`), `SD-G1-01`..`05`, `OQ-001`..`007`, `BA-OQ-001`..`005`, `TD-G2-01`..`06` và các điểm chờ PoC/chính sách (`POC-01`..`03`, `POL-01`..`02`). | Trước khi thiết kế kiến trúc, viết ADR, chọn tech stack hoặc tổ chức lại thư mục. |
| **3** | [`docs/product/glossary.md`](./product/glossary.md) | Từ điển chuẩn toàn dự án: 12 tác nhân, phân biệt chuỗi `Adapter` → `Connection` → `Model` → `Deployment`, nhãn nguồn gốc Plugin, phân tách khả năng kỹ thuật và quyền sử dụng hợp pháp. | Khi thiết kế schema dữ liệu, đặt tên API hoặc thiết kế nhãn giao diện. |
| **4** | [`docs/governance/assumptions.md`](./governance/assumptions.md) | Các giả thuyết làm việc tạm thời (`ASM-001`..`ASM-010`) gắn với thử nghiệm kiểm chứng (`EXP-01`..`EXP-06`) và danh sách điểm lệch thực tế trong repository (`ISS-001`..`ISS-007`). | Khi kiểm tra rủi ro giả định hoặc xử lý cấu hình hiện có trong repo. |

### 1.2. Tầng Đặc tả Sâu Extension Platform, Kết nối & Kiểm thử Hợp đồng (`docs/specifications/` & `docs/quality/` — `ILLUSTRATIVE PRE-ADR`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **5** | [`docs/specifications/extension-contract.md`](./specifications/extension-contract.md) | **`SPEC-EXT-01`:** Taxonomy 8 thực thể mở rộng vs 7 phần Core Kernel bất khả xâm phạm; SDK/API Contract đa ngôn ngữ có phiên bản (`N-1` / `12m`); manifest mẫu minh họa; 3 tầng thực thi (`Declarative`, `Sandboxed Wasm`, `Isolated Sidecar`) cấm tuyệt đối mã không tin cậy chạy in-process. | Khi thiết kế kiến trúc Core Host, SDK, Wasm/Sidecar runtime và quy hoạch mô-đun. |
| **6** | [`docs/specifications/provider-adapter.md`](./specifications/provider-adapter.md) | **`SPEC-EXT-02`:** Phân tách `Adapter → Connection → LogicalModel → Deployment`; 3 cấp độ bảo chứng năng lực (`DECLARED` / `TESTED` / `VERIFIED`); cấu trúc Declarative Pack mẫu; quy tắc dịch giao thức, streaming, retry và ma trận fallback cấm rơi ngầm giữa dòng stream hoặc rò rỉ vùng dữ liệu. | Khi thiết kế Router, Provider Adapter SDK và các gói tích hợp nhà cung cấp AI. |
| **7** | [`docs/specifications/authentication-connections.md`](./specifications/authentication-connections.md) | **`SPEC-EXT-03`:** Hợp đồng xác thực (`Zero Full-Vault Access`, `Write-Only Plaintext`, `No Token Cross-Audience`); 5 phương thức xác thực (Blind Inject, OAuth 2.1 + PKCE, Device Flow có điều kiện, mTLS/OIDC, Custom Auth giới hạn); single-flight refresh; ranh giới `Official/Community/Private` và **Case Study rủi ro Consumer Subscriptions** không có cơ chế lách ToS. | Khi thiết kế Credential Vault, OAuth Broker, RBAC kết nối và màn hình cảnh báo rủi ro. |
| **8** | [`docs/specifications/marketplace-lifecycle.md`](./specifications/marketplace-lifecycle.md) | **`SPEC-EXT-04`:** Phân tách 3 trục (`Package Origin ≠ Publisher Trust ≠ Runtime Safety`); 4 nguồn cài đặt (Marketplace, Git SHA, Signed `.tpkg`, OCI `@sha256`); quy tắc đóng gói khép kín không phụ thuộc bắc cầu; luồng 9 bước cài đặt/cập nhật/rollback/cách ly; `tanpopo.lock`, SBOM, SLSA provenance và thu hồi khẩn cấp (`revocations.json`). | Khi thiết kế trình quản lý gói Extension, Marketplace Registry, CI xuất bản và CLI cài đặt. |
| **9** | [`docs/specifications/mcp-a2a-boundaries.md`](./specifications/mcp-a2a-boundaries.md) | **`SPEC-EXT-05`:** Ranh giới cấu trúc 4 mặt phẳng (`Provider` / `Plugin` / `MCP` / `A2A`); MCP Hub (`Streamable HTTP` & `stdio` cách ly, namespace `<server>__<tool>`, ACL từng công cụ, cô lập audience RFC 8707, ghim `schema_hash` JCS, phê duyệt con người sync/async, cấm retry mù); và mặt phẳng A2A (`AgentCard`, task bất đồng bộ, thu hẹp quyền đơn điệu, `hop_count <= 5`). | Khi thiết kế MCP Gateway Hub, hệ thống phê duyệt Human-in-the-Loop và kết nối tác tử A2A. |
| **10** | [`docs/specifications/api-protocol-compatibility.md`](./specifications/api-protocol-compatibility.md) | **`SPEC-EXT-06`:** Mặt phẳng giao thức ngõ vào (`/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, multimodal + chống SSRF, tool calling, strict `json_schema`, SSE `< 1 ms` & hủy `< 500 ms`, khóa `Idempotency-Key` nguyên tử và khung lỗi chuẩn `Canonical Error Envelope`). | Khi thiết kế HTTP Data Plane, OpenAPI/JSON Schema công khai và client SDK tương thích. |
| **11** | [`docs/quality/contract-test-matrix.md`](./quality/contract-test-matrix.md) | **`SPEC-QA-01`:** Kiến trúc Cassette Test Harness ngoại tuyến tất định, ma trận 24 bài kiểm thử hợp đồng & tấn công sandbox (`CT-01`..`CT-24`) và kịch bản nghiệm thu đầu-cuối 10 bước (`EXT-ACCEPT-01`) chứng minh cài Adapter ngoài độc lập 100% với bản phát hành Core. | Khi thiết kế hạ tầng kiểm thử tự động, cổng CI kiểm duyệt gói mở rộng và nghiệm thu MVP. |

### 1.3. Tầng Định nghĩa Sản phẩm & Đặc tả Hệ thống — PRD/SRS (`docs/product/` & `docs/requirements/` — `DRAFT`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **12** | [`docs/product/prd.md`](./product/prd.md) | Vấn đề, chân dung người dùng, giá trị cốt lõi, `PG-01`..`05`, `NG-01`..`08`, chỉ tiêu đo lường dự kiến `MO-01`..`06`, bản đồ 10 phân hệ phi chu trình (`Capability Map`) và ràng buộc `PC-01`..`06`. | Khi thiết kế kiến trúc tổng thể và định hình ranh giới hệ thống. |
| **13** | [`docs/product/release-scope.md`](./product/release-scope.md) | Phân kỳ `MVP (v0.1)` → `v0.2` → `v0.3` → `v1.0` → `Post-v1.0`, tiêu chí xuất xưởng từng cột mốc và 6 đánh đổi trọng yếu (`TD-G2-01`..`06`). | Khi lập kế hoạch triển khai theo giai đoạn, tránh nhồi nhét tính năng vào `v0.1`. |
| **14** | [`docs/product/feature-catalog.md`](./product/feature-catalog.md) | Danh mục 16 tính năng (`FT-01`..`FT-16`) trải trên 10 miền nghiệp vụ với đủ 14 thuộc tính kiểm chứng được và phân biệt rõ hành vi nhà cung cấp hỗ trợ / không hỗ trợ. | Khi thiết kế chi tiết từng phân hệ, API, CLI và màn hình giao diện. |
| **15** | [`docs/requirements/srs.md`](./requirements/srs.md) | **Nguồn chân lý đặc tả hệ thống:** Ma trận Giao thức/Năng lực 14 dòng, `53` yêu cầu chức năng (`TP-FR-0101`..`1005` — Must/Should/Could/Later) và `22` yêu cầu phi chức năng đo lường được (`TP-NFR-0101`..`1002` — mục tiêu dự kiến). | Khi thiết kế giao diện mô-đun, kiến trúc bảo mật/hiệu năng và viết code. |
| **16** | [`docs/requirements/acceptance-criteria.md`](./requirements/acceptance-criteria.md) | Kịch bản nghiệm thu `Given/When/Then` cho toàn bộ chức năng (`TP-AC-F*`), 8 nhóm rủi ro bảo mật/tuân thủ (`TP-AC-SEC-01`..`08`) và xác minh phi chức năng (`TP-AC-N01`..`10`). | Khi thiết kế chiến lược kiểm thử, viết test tự động và nghiệm thu Cổng chất lượng. |
| **17** | [`docs/requirements/traceability.md`](./requirements/traceability.md) | Ma trận truy vết đầu-cuối hai chiều (`UC/WF/OPS → TP-BR/SM → FT → TP-FR/NFR → TP-AC → SPEC-EXT/CT → Placeholders ARCH/MOD/TST`). | Khi cập nhật thiết kế kiến trúc (`ARCH-*`), gói mã (`MOD-*`) và bộ test (`TST-*`). |

### 1.4. Tầng Phân tích Nghiệp vụ Nền tảng — Business Analysis (`docs/requirements/` — `DRAFT`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **18** | [`docs/requirements/business-entity-map.md`](./requirements/business-entity-map.md) | Ma trận 12 tác nhân, sơ đồ quan hệ thực thể (ER), quyền sở hữu/chia sẻ và **8 Máy trạng thái (`SM-01`..`SM-08`)** cho toàn bộ vòng đời hệ thống. | Khi thiết kế cơ sở dữ liệu, RBAC và máy trạng thái vòng đời. |
| **19** | [`docs/requirements/workflows.md`](./requirements/workflows.md) | **13 Hành trình đầu-cuối (`WF-01`..`WF-13`)**, sơ đồ trình tự và toàn bộ tình huống phủ định/biên. | Khi thiết kế luồng điều phối chi tiết và xử lý ngoại lệ. |
| **20** | [`docs/requirements/business-rules.md`](./requirements/business-rules.md) | Bộ **30 quy tắc nghiệp vụ `TP-BR-0101`..`0803`** độc lập công nghệ kèm chủ sở hữu và ngoại lệ. | Khi cài đặt động cơ chính sách, kiểm toán bảo mật và sổ cái ngân sách. |
| **21** | [`docs/requirements/workflow-requirement-candidates.md`](./requirements/workflow-requirement-candidates.md) | **9 nhóm yêu cầu vận hành (`OPS-01`..`OPS-09`)**, lịch sử ánh xạ ứng viên và 5 điểm mơ hồ nghiệp vụ (`BA-OQ-001`..`005`). | Khi thiết kế quy trình vận hành, sao lưu, hỗ trợ và kiểm duyệt Marketplace. |

### 1.5. Tầng Khám phá Sản phẩm — Product Discovery (`docs/discovery/`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **22** | [`docs/discovery/positioning-and-success-metrics.md`](./discovery/positioning-and-success-metrics.md) | Định vị sản phẩm đề xuất, North Star Metric (`WGMR`), ranh giới sản phẩm và các thử nghiệm `EXP-01`..`06`. | Khi đối chiếu định vị chiến lược và thiết kế thử nghiệm PoC. |
| **23** | [`docs/discovery/users-jobs-use-cases.md`](./discovery/users-jobs-use-cases.md) | Xếp hạng 5 phân khúc khách hàng, nhóm Anti-Personas, 4 vai trò chức năng và 9 Use Cases khám phá (`UC-01`..`UC-09`). | Khi đối chiếu ngữ cảnh người dùng thực tế. |
| **24** | [`docs/discovery/market-and-competitors.md`](./discovery/market-and-competitors.md) | Đối chiếu chi tiết 4 nhóm đối thủ thị trường 2025–2026 kèm nguồn dẫn chiếu. | Khi đối chiếu tính năng cạnh tranh. |
| **25** | [`docs/discovery/opportunity-and-feasibility.md`](./discovery/opportunity-and-feasibility.md) | Phân tầng Parity/Differentiators/Moats, phân vùng pháp lý/TOS và 7 ý tưởng giá trị cao (`IDEA-01`..`07`). | Khi thiết kế kiến trúc mở rộng và ranh giới tuân thủ. |
| **26** | [`docs/discovery/risk-register.md`](./discovery/risk-register.md) | Xếp hạng Top 12 rủi ro chiến lược & kỹ thuật (`RSK-001`..`RSK-012`) và biện pháp giảm thiểu. | Khi lập kế hoạch kiểm soát rủi ro kỹ thuật. |

### 1.6. Quy ước Công cụ & Kỹ năng trong Workspace

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **27** | [`AGENTS.md`](../AGENTS.md) & [`GEMINI.md`](../GEMINI.md) | Quy ước bắt buộc của **Astryx Design System v0.7.13** (template-first, cấm thẻ `div` thô, dùng design tokens, chạy `astryx doctor`). | Khi thiết kế hoặc lập trình giao diện Web Console dùng Astryx. |
| **28** | [`.agents/skills/using-agent-skills/SKILL.md`](../.agents/skills/using-agent-skills/SKILL.md) | Quy trình kích hoạt bộ 25 kỹ năng kỹ thuật phần mềm theo từng giai đoạn phát triển (Define → Plan → Build → Verify → Review → Ship). | Khi bắt đầu một tác vụ kỹ thuật cụ thể theo quy trình chuẩn. |

---

## 2. Phân biệt rạch ròi: Đã tồn tại trong Code vs Chỉ có trong Định hướng Tài liệu

> **Kết luận cốt lõi (`VERIFIED_IN_REPO`):** Tính đến hết bước **A04**, repository **vẫn chưa có bất kỳ dòng mã nghiệp vụ AI Gateway nào** (chưa có server, chưa có Unified API, chưa có routing/policy/cost engine, chưa có plugin runtime/SDK, chưa có MCP/A2A, chưa có CLI và chưa có bundler chạy giao diện Web). Toàn bộ năng lực AI Gateway hiện nằm ở tầng **định hướng chiến lược và đặc tả kỹ thuật (`DRAFT / PRE-ADR`)** chờ thiết kế kiến trúc nội bộ, thử nghiệm PoC và hiện thực hóa ở các pha sau.

### 2.1. Kiểm kê toàn bộ hiện trạng Repository (`Repository Inventory`)

| Đường dẫn trong Repo | Phân loại thực tế | Chi tiết kiểm chứng (`VERIFIED_IN_REPO`) |
|---|---|---|
| [`LICENSE`](../LICENSE) | **Tệp pháp lý gốc** | Giấy phép **Apache License 2.0** (201 dòng, tạo từ commit khởi đầu `0a35b74`). |
| [`.gitignore`](../.gitignore) | **Cấu hình Git** | Loại trừ `node_modules/`, `dist/`, `build/`, `out/`, `coverage/`, `.env*`, logs và tệp tạm IDE. |
| [`skills-lock.json`](../skills-lock.json) | **Lockfile công cụ Agent** | Ghi nhận phiên bản và mã băm SHA-256 của 25 skills sao chép từ `addyosmani/agent-skills`. |
| [`.agents/skills/*`](../.agents/skills) | **Quy trình Agent (25 skills)** | 25 thư mục kỹ năng (`SKILL.md` kèm tài liệu/script cục bộ như `quality-gate.mjs`, `floor-guard.md`, `hardening-patterns.md`, `optimization-patterns.md`). Hoạt động đầy đủ. |
| [`.agents/references/*`](../.agents/references) | **Checklist kỹ thuật (7 tệp)** | Các danh mục kiểm tra dùng chung (`security`, `performance`, `accessibility`, `observability`, `testing-patterns`, `orchestration-patterns`, `definition-of-done`). |
| [`.agents/agents/*`](../.agents/agents) | **Định nghĩa Subagents (4 tệp)** | Personas chuyên trách: `code-reviewer`, `security-auditor`, `test-engineer`, `web-performance-auditor`. |
| [`.agents/plugin.json`](../.agents/plugin.json) | **Siêu dữ liệu tham khảo** | Tệp manifest gốc của `agent-skills` (`v0.6.15`, giấy phép MIT). |
| [`.gemini/commands/*.toml`](../.gemini/commands) | **Cấu hình lệnh Gemini CLI** | 9 tệp `.toml` định nghĩa lệnh tắt cho Gemini CLI (`spec`, `planning`, `build`, `test`, `constraints`, `review`, `code-simplify`, `webperf`, `ship`). |
| [`AGENTS.md`](../AGENTS.md) & [`GEMINI.md`](../GEMINI.md) | **Quy ước AI Agent cho UI** | Khối cấu hình tự động sinh bởi `npx @astryxdesign/cli init` cho Astryx v0.7.13 (174 components). |
| [`package.json`](../package.json) & [`package-lock.json`](../package-lock.json) | **Cấu hình gói Node.js sơ khai** | Cài đặt 4 gói trực tiếp: `@astryxdesign/core@^0.7.13`, `@astryxdesign/theme-neutral@^0.7.13`, `@astryxdesign/cli@^0.7.13`, `@stylexjs/stylex@^0.19.1` (kéo theo `react@19.3.1` và `react-dom@19.3.1` qua peerDependencies). Chưa có script `dev`/`build`, đang ghi `"license": "ISC"`. |
| [`astryx-themes.js`](../astryx-themes.js) | **Module Theme sinh tự động** | Sinh bởi `npx astryx theme add neutral`, xuất `themes` và `defaultThemeSlug = "neutral"`. |
| [`src/App.tsx`](../src/App.tsx) & [`src/main.tsx`](../src/main.tsx) | **Mã nguồn giữ chỗ tĩnh (Unbundled Placeholder)** | 2 tệp React/TSX (34 dòng và 13 dòng) kết nối `<Theme theme={themes[defaultThemeSlug]}>` với các component mẫu của Astryx. **Chưa có bundler, chưa có `index.html`, chưa chạy thành trang web trực quan được.** |
| `docs/governance/*` & `docs/README.md` | **Tài liệu Quản trị & Bối cảnh (A00 → A04)** | Bao gồm `docs/README.md`, `docs/governance/project-charter.md`, `docs/governance/decisions.md`, `docs/governance/assumptions.md`. |
| `docs/discovery/*` | **Tài liệu Product Discovery (A01)** | Bao gồm 5 tệp phân tích khám phá sản phẩm: `market-and-competitors.md`, `users-jobs-use-cases.md`, `opportunity-and-feasibility.md`, `positioning-and-success-metrics.md`, `risk-register.md`. |
| `docs/product/*` | **Tài liệu Định nghĩa Sản phẩm Nháp (A02 → A03 — `DRAFT`)** | Bao gồm 4 tệp: `glossary.md`, `prd.md`, `feature-catalog.md`, `release-scope.md`. |
| `docs/requirements/*` | **Tài liệu Nghiệp vụ & Đặc tả Hệ thống Nháp (A02 → A04 — `DRAFT`)** | Bao gồm 7 tệp: `business-entity-map.md`, `workflows.md`, `business-rules.md`, `workflow-requirement-candidates.md`, `srs.md`, `acceptance-criteria.md`, `traceability.md`. |
| `docs/specifications/*` | **Tài liệu Đặc tả Sâu Extension Platform & Kết nối (A04 — `PRE-ADR`)** | Bao gồm 6 tệp: `extension-contract.md`, `provider-adapter.md`, `authentication-connections.md`, `marketplace-lifecycle.md`, `mcp-a2a-boundaries.md`, `api-protocol-compatibility.md`. |
| `docs/quality/*` | **Tài liệu Chất lượng & Ma trận Kiểm thử Hợp đồng (A04 — `PRE-ADR`)** | Bao gồm 1 tệp: `contract-test-matrix.md` (`CT-01`..`CT-24` & `EXT-ACCEPT-01`). |

---

## 3. Báo cáo Xác minh Thực nghiệm 3 Nhiệm vụ Setup trước Phiên A00

| Nhiệm vụ trước A00 | Kết luận xác minh | Bằng chứng thực nghiệm (`VERIFIED_IN_REPO`) | Hệ quả & Lưu ý bảo toàn |
|---|---|---|---|
| **Setup 1:** Cài bộ Agent Skills từ `https://skills.addy.ie/` vào dự án | **HOÀN THÀNH ĐẦY ĐỦ** | `cmd /c npx -y skills list` xác nhận đủ 25/25 project skills từ `addyosmani/agent-skills`; đủ `.agents/references/` cho liên kết tương đối `../../references/*.md`. | Sẵn sàng phục vụ toàn bộ vòng đời phát triển tiếp theo. |
| **Setup 2:** Cài `@astryxdesign/core`, `@stylexjs/stylex`, `@astryxdesign/theme-neutral`, `@astryxdesign/cli` và chạy `init` | **HOÀN THÀNH (Kèm điểm lệch nhỏ cần lưu ý)** | `npm ls --depth=0` xác nhận đủ 4 gói; `AGENTS.md` và `GEMINI.md` có khối `ASTRYX:START..END`; `npx @astryxdesign/cli doctor` đạt `18 pass, 0 fail, 3 warn`. | Không tự động khóa dự án vào kiến trúc thuần Node.js/React ở cấp hệ thống; cần sửa `"license": "ISC"` thành `"Apache-2.0"` khi chốt `SD-G1-05` / `OQ-007`. |
| **Setup 3:** Hỏi look-and-feel, cài theme qua CLI (mặc định `neutral`), kết nối `<Theme>` và cho xem preview | **HOÀN THÀNH PHẦN CẤU HÌNH & CODE MẪU — CHƯA CÓ BẢN CHẠY XEM TRỰC QUAN (NO LIVE PREVIEW)** | Owner đã chọn `neutral`; `npx @astryxdesign/cli theme list` báo `neutral (added: yes, default: yes)`; `astryx-themes.js` và `src/App.tsx` vượt qua kiểm tra tĩnh của `astryx doctor`. Tuy nhiên **chưa có Vite/bundler/`index.html`** nên chưa từng dựng lên giao diện hiển thị trên trình duyệt. | Giữ nguyên cấu hình theme `neutral` đã cài; chỉ bổ sung bundler và dựng giao diện chuẩn từ Astryx template sau khi chốt kiến trúc và cấu trúc thư mục (`OQ-001`, `OQ-007`). |

---

## 4. Trạng thái Git hiện tại sau bước A04 (`DRAFT / UNLOCKED SCOPE`)

- **Nhánh làm việc:** `main` (đồng bộ với `origin/main`).
- **Tệp thay đổi/tạo mới qua A00 → A04:** Chỉ làm việc trong thư mục `docs/` (tổng cộng 27 tệp Markdown gồm `docs/README.md`, 3 tệp `docs/governance/*`, 5 tệp `docs/discovery/*`, 4 tệp `docs/product/*`, 7 tệp `docs/requirements/*`, 6 tệp `docs/specifications/*` và 1 tệp `docs/quality/*`), **không chỉnh sửa bất kỳ tệp cấu hình, theme hay mã nguồn nào đã có trước đó**. Mỗi bước được lưu mốc bằng một atomic Git commit riêng biệt theo quy ước `DEC-008` (`docs: step <NN> - ...`).
