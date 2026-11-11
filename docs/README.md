# Tanpopo Documentation & Context Hub (Updated through Phase A02 — `DRAFT / UNLOCKED SCOPE`)

Chào mừng đến với trung tâm tài liệu chuẩn hóa bối cảnh, khám phá sản phẩm và phân tích nghiệp vụ của dự án **Tanpopo** (`https://github.com/nguywnben/tanpopo`). Bộ tài liệu này được thiết lập qua các bước **ASTRA A00**, **A01 (Product Discovery)** và **A02 (Business Analysis)** nhằm tạo dựng nguồn sự thật (Source of Truth) đáng tin cậy cho con người và các AI Agents ở những bước tiếp theo (**A03+**) làm việc chính xác mà không phải đoán mò.

> **Trạng thái Cổng Giai đoạn Hiện tại:**
> - **Gate G0 (ASTRA A00 — Chuẩn hóa bối cảnh):** `COMPLETED`
> - **Gate G1 (ASTRA A01 — Product Discovery):** **`READY_FOR_REVIEW`** *(Product Owner chưa duyệt `SD-G1-01`..`SD-G1-05`; không tự đánh dấu `APPROVED`).*
> - **Gate G2 (ASTRA A02 — Phân tích nghiệp vụ):** **`DRAFT / READY_FOR_REVIEW (Scope Unlocked)`** *(Bản nháp phân tích nghiệp vụ và quy tắc `TP-BR-xxxx`, chưa khóa scope do chờ phê duyệt Gate G1 & Gate G2).*

---

## 1. Bản đồ đọc tài liệu cho các phiên / Prompt tiếp theo (Reading Guide)

Trước khi thực hiện bất kỳ bước lập đặc tả sản phẩm (PRD), thiết kế kiến trúc, lập kế hoạch hay viết code nào cho Tanpopo, hãy đọc theo đúng thứ tự sau:

### 1.1. Tầng Quản trị & Từ điển Chuẩn (`docs/governance/` & `docs/product/`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **1** | [`docs/governance/project-charter.md`](./governance/project-charter.md) | Tầm nhìn sản phẩm, khác biệt `extension-first`, sự phân tách 4 lớp (`Provider` / `Plugin` / `MCP` / `A2A`), chính sách `Official vs Community` & ranh giới không hợp thức hóa consumer subscriptions. | **Luôn đọc đầu tiên** trong mọi phiên làm việc mới. |
| **2** | [`docs/governance/decisions.md`](./governance/decisions.md) | Trạng thái các Cổng (`G0`, `G1`, `G2`), quyết định đã chốt (`DEC-001`..`DEC-008`), hướng đã bác bỏ (`REJ-001`..`REJ-004`), 5 quyết định chiến lược (`SD-G1-01`..`05`), 7 câu hỏi kiến trúc (`OQ-001`..`007`) và 5 câu hỏi nghiệp vụ (`BA-OQ-001`..`005`). | Trước khi lập PRD, chọn tech stack, thiết kế kiến trúc hoặc tổ chức lại thư mục. |
| **3** | [`docs/product/glossary.md`](./product/glossary.md) | Từ điển chuẩn toàn dự án: 12 tác nhân, phân biệt chuỗi `Adapter` → `Connection` → `Model` → `Deployment`, nhãn nguồn gốc Plugin, phân tách khả năng kỹ thuật và quyền sử dụng hợp pháp. | Khi viết PRD, thiết kế schema dữ liệu, đặt tên API hoặc thiết kế nhãn giao diện. |
| **4** | [`docs/governance/assumptions.md`](./governance/assumptions.md) | Các giả thuyết làm việc tạm thời (`ASM-001`..`ASM-010`) gắn với thử nghiệm kiểm chứng (`EXP-01`..`EXP-06`) và danh sách điểm lệch thực tế trong repository (`ISS-001`..`ISS-007`). | Khi cần kiểm tra rủi ro giả định hoặc xử lý cấu hình hiện có trong repo. |

### 1.2. Tầng Phân tích Nghiệp vụ — Business Analysis (`docs/requirements/` — `DRAFT`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **5** | [`docs/requirements/business-entity-map.md`](./requirements/business-entity-map.md) | Ma trận 12 tác nhân, sơ đồ quan hệ thực thể (ER), quyền sở hữu/chia sẻ và **8 Máy trạng thái (`SM-01`..`SM-08`)** cho toàn bộ vòng đời hệ thống. | Khi thiết kế mô hình dữ liệu, phân quyền RBAC và API vòng đời trạng thái. |
| **6** | [`docs/requirements/workflows.md`](./requirements/workflows.md) | **13 Hành trình đầu-cuối (`WF-01`..`WF-13`)**, sơ đồ trình tự và toàn bộ tình huống phủ định/biên (partial stream, timeout, destructive tool, permission escalation, xóa dữ liệu...). | Khi viết User Stories, Acceptance Criteria và kịch bản kiểm thử tích hợp. |
| **7** | [`docs/requirements/business-rules.md`](./requirements/business-rules.md) | Bộ **quy tắc nghiệp vụ `TP-BR-0101`..`0803`** độc lập công nghệ kèm chủ sở hữu, ngoại lệ và tiêu chí kiểm thử sơ bộ. | Khi lập PRD, viết bộ kiểm tra chính sách, kiểm toán bảo mật và đối soát tài chính. |
| **8** | [`docs/requirements/workflow-requirement-candidates.md`](./requirements/workflow-requirement-candidates.md) | 9 nhóm yêu cầu vận hành (`OPS-01`..`OPS-09`), ma trận ánh xạ sang ứng viên yêu cầu (`CAND-001`..`017`), bảng tự kiểm tra và 5 điểm mơ hồ cần chốt. | Đầu vào trực tiếp để chuyển hóa thành PRD ở bước **ASTRA A03**. |

### 1.3. Tầng Khám phá Sản phẩm — Product Discovery (`docs/discovery/`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **9** | [`docs/discovery/positioning-and-success-metrics.md`](./discovery/positioning-and-success-metrics.md) | Định vị sản phẩm đề xuất, North Star Metric (`WGMR`), các chỉ tiêu SLO đo lường được (`TTFGR`, độ trễ, RAM), ranh giới sản phẩm, lộ trình `v0.1`→`v1.0` và `EXP-01`..`06`. | Khi xác định thước đo thành công và phân kỳ phiên bản. |
| **10** | [`docs/discovery/users-jobs-use-cases.md`](./discovery/users-jobs-use-cases.md) | Xếp hạng 5 phân khúc khách hàng, nhóm Anti-Personas, 4 vai trò chức năng và 9 Use Cases khám phá (`UC-01`..`UC-09`). | Khi đối chiếu nhu cầu người dùng mục tiêu. |
| **11** | [`docs/discovery/market-and-competitors.md`](./discovery/market-and-competitors.md) | Đối chiếu chi tiết 4 nhóm đối thủ thị trường 2025–2026 kèm nguồn dẫn chiếu. | Khi đối chiếu tính năng cạnh tranh. |
| **12** | [`docs/discovery/opportunity-and-feasibility.md`](./discovery/opportunity-and-feasibility.md) | Phân tầng Parity/Differentiators/Moats, phân vùng pháp lý/TOS và 7 ý tưởng giá trị cao (`IDEA-01`..`07`). | Khi thiết kế kiến trúc mở rộng và ranh giới tuân thủ. |
| **13** | [`docs/discovery/risk-register.md`](./discovery/risk-register.md) | Xếp hạng Top 10 rủi ro chiến lược & kỹ thuật (`RSK-001`..`RSK-012`) và biện pháp giảm thiểu. | Khi lập kế hoạch kiểm soát rủi ro. |

### 1.4. Quy ước Công cụ & Kỹ năng trong Workspace

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **14** | [`AGENTS.md`](../AGENTS.md) & [`GEMINI.md`](../GEMINI.md) | Quy ước bắt buộc của **Astryx Design System v0.7.13** (template-first, cấm thẻ `div` thô, dùng design tokens, chạy `astryx doctor`). | Khi thiết kế hoặc lập trình giao diện Web Console dùng Astryx. |
| **15** | [`.agents/skills/using-agent-skills/SKILL.md`](../.agents/skills/using-agent-skills/SKILL.md) | Quy trình kích hoạt bộ 25 kỹ năng kỹ thuật phần mềm theo từng giai đoạn phát triển (Define → Plan → Build → Verify → Review → Ship). | Khi bắt đầu một tác vụ kỹ thuật cụ thể theo quy trình chuẩn. |

---

## 2. Phân biệt rạch ròi: Đã tồn tại trong Code vs Chỉ có trong Định hướng Tài liệu

> **Kết luận cốt lõi (`VERIFIED_IN_REPO`):** Tính đến hết bước **A00**, repository **chưa có bất kỳ dòng mã nghiệp vụ AI Gateway nào** (chưa có server, chưa có Unified API, chưa có routing/policy/cost engine, chưa có plugin runtime/SDK, chưa có MCP/A2A, chưa có CLI và chưa có bundler chạy giao diện Web). Toàn bộ năng lực AI Gateway hiện nằm ở tầng **định hướng chiến lược (`CONFIRMED_BY_OWNER`)** chờ thiết kế và hiện thực hóa ở các pha sau.

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
| `docs/governance/*` & `docs/README.md` | **Tài liệu Quản trị & Bối cảnh (A00 → A02)** | Bao gồm `docs/README.md`, `docs/governance/project-charter.md`, `docs/governance/decisions.md`, `docs/governance/assumptions.md`. |
| `docs/discovery/*` | **Tài liệu Product Discovery (A01)** | Bao gồm 5 tệp phân tích khám phá sản phẩm: `market-and-competitors.md`, `users-jobs-use-cases.md`, `opportunity-and-feasibility.md`, `positioning-and-success-metrics.md`, `risk-register.md`. |
| `docs/product/*` & `docs/requirements/*` | **Tài liệu Phân tích Nghiệp vụ Nháp (Tạo mới ở A02 — `DRAFT`)** | Bao gồm `docs/product/glossary.md` và 4 tệp trong `docs/requirements/`: `business-entity-map.md`, `workflows.md`, `business-rules.md`, `workflow-requirement-candidates.md`. |

---

## 3. Báo cáo Xác minh Thực nghiệm 3 Nhiệm vụ Setup trước Phiên A00

| Nhiệm vụ trước A00 | Kết luận xác minh | Bằng chứng thực nghiệm (`VERIFIED_IN_REPO`) | Hệ quả & Lưu ý bảo toàn |
|---|---|---|---|
| **Setup 1:** Cài bộ Agent Skills từ `https://skills.addy.ie/` vào dự án | **HOÀN THÀNH ĐẦY ĐỦ** | `cmd /c npx -y skills list` xác nhận đủ 25/25 project skills từ `addyosmani/agent-skills`; đủ `.agents/references/` cho liên kết tương đối `../../references/*.md`. | Sẵn sàng phục vụ toàn bộ vòng đời phát triển tiếp theo. |
| **Setup 2:** Cài `@astryxdesign/core`, `@stylexjs/stylex`, `@astryxdesign/theme-neutral`, `@astryxdesign/cli` và chạy `init` | **HOÀN THÀNH (Kèm điểm lệch nhỏ cần lưu ý)** | `npm ls --depth=0` xác nhận đủ 4 gói; `AGENTS.md` và `GEMINI.md` có khối `ASTRYX:START..END`; `npx @astryxdesign/cli doctor` đạt `18 pass, 0 fail, 3 warn`. | Không tự động khóa dự án vào kiến trúc thuần Node.js/React ở cấp hệ thống; cần sửa `"license": "ISC"` thành `"Apache-2.0"` khi chốt `SD-G1-05` / `OQ-007`. |
| **Setup 3:** Hỏi look-and-feel, cài theme qua CLI (mặc định `neutral`), kết nối `<Theme>` và cho xem preview | **HOÀN THÀNH PHẦN CẤU HÌNH & CODE MẪU — CHƯA CÓ BẢN CHẠY XEM TRỰC QUAN (NO LIVE PREVIEW)** | Owner đã chọn `neutral`; `npx @astryxdesign/cli theme list` báo `neutral (added: yes, default: yes)`; `astryx-themes.js` và `src/App.tsx` vượt qua kiểm tra tĩnh của `astryx doctor`. Tuy nhiên **chưa có Vite/bundler/`index.html`** nên chưa từng dựng lên giao diện hiển thị trên trình duyệt. | Giữ nguyên cấu hình theme `neutral` đã cài; chỉ bổ sung bundler và dựng giao diện chuẩn từ Astryx template sau khi chốt kiến trúc và cấu trúc thư mục (`OQ-001`, `OQ-007`). |

---

## 4. Trạng thái Git hiện tại sau bước A02 (`DRAFT / UNLOCKED SCOPE`)

- **Nhánh làm việc:** `main` (đồng bộ với `origin/main`).
- **Tệp thay đổi/tạo mới qua A00 → A02:** Chỉ làm việc trong thư mục `docs/` (tổng cộng 14 tệp Markdown gồm `docs/README.md`, 3 tệp `docs/governance/*`, 5 tệp `docs/discovery/*`, 1 tệp `docs/product/glossary.md` và 4 tệp `docs/requirements/*`), **không chỉnh sửa bất kỳ tệp cấu hình, theme hay mã nguồn nào đã có trước đó**. Mỗi bước (`A00`, `A01`, `A02`, ...) đều được lưu mốc bằng một atomic Git commit riêng biệt theo quy tắc `DEC-008`.
