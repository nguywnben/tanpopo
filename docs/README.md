# Tanpopo Documentation & Context Hub (Phase A00 Baseline)

Chào mừng đến với trung tâm tài liệu chuẩn hóa bối cảnh của dự án **Tanpopo** (`https://github.com/nguywnben/tanpopo`). Bộ tài liệu này được thiết lập ở bước **ASTRA A00** nhằm tạo dựng nguồn sự thật (Source of Truth) đáng tin cậy, có kiểm chứng thực nghiệm để con người và các AI Agents ở những bước tiếp theo (**A01+**) làm việc chính xác mà không phải đoán mò.

---

## 1. Bản đồ đọc tài liệu cho các phiên / Prompt tiếp theo (Reading Guide)

Trước khi thực hiện bất kỳ bước phân tích yêu cầu (PRD), thiết kế kiến trúc, lập kế hoạch hay viết code nào cho Tanpopo, hãy đọc theo đúng thứ tự sau:

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **1** | [`docs/governance/project-charter.md`](./governance/project-charter.md) | Tầm nhìn sản phẩm, khác biệt `extension-first`, sự phân tách 4 lớp (`Provider` / `Plugin` / `MCP` / `A2A`), chính sách `Official vs Community` & ranh giới không hợp thức hóa consumer subscriptions, mục tiêu, phi mục tiêu, bảng thuật ngữ chuẩn. | **Luôn đọc đầu tiên** trong mọi phiên làm việc mới. |
| **2** | [`docs/governance/decisions.md`](./governance/decisions.md) | Danh mục các quyết định đã xác nhận (`DEC-001`..`DEC-007`), các hướng đã bác bỏ (`REJ-001`..`REJ-004`), và **7 câu hỏi mở có tính chặn (`OQ-001`..`OQ-007`)** kèm khuyến nghị & đánh đổi kiến trúc. | Trước khi lập PRD, chọn tech stack, thiết kế kiến trúc hoặc tổ chức lại thư mục. |
| **3** | [`docs/governance/assumptions.md`](./governance/assumptions.md) | Các giả thuyết làm việc tạm thời (`ASM-001`..`ASM-008`) và danh sách các điểm lệch/thiếu hụt thực tế trong repository (`ISS-001`..`ISS-007`). | Khi cần kiểm tra rủi ro giả định hoặc xử lý cấu hình hiện có trong repo. |
| **4** | [`AGENTS.md`](../AGENTS.md) & [`GEMINI.md`](../GEMINI.md) | Quy ước bắt buộc của **Astryx Design System v0.7.13** (template-first, cấm thẻ `div` thô, dùng design tokens, chạy `astryx doctor`). | Khi thiết kế hoặc lập trình giao diện Web Console dùng Astryx. |
| **5** | [`.agents/skills/using-agent-skills/SKILL.md`](../.agents/skills/using-agent-skills/SKILL.md) | Quy trình kích hoạt bộ 25 kỹ năng kỹ thuật phần mềm theo từng giai đoạn phát triển (Define → Plan → Build → Verify → Review → Ship). | Khi bắt đầu một tác vụ kỹ thuật cụ thể theo quy trình chuẩn. |

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
| `docs/*` | **Tài liệu Quản trị & Bối cảnh (Tạo mới ở A00)** | Bao gồm `docs/README.md`, `docs/governance/project-charter.md`, `docs/governance/decisions.md`, `docs/governance/assumptions.md`. |

---

## 3. Báo cáo Xác minh Thực nghiệm 3 Nhiệm vụ Setup trước Phiên A00

| Nhiệm vụ trước A00 | Kết luận xác minh | Bằng chứng thực nghiệm (`VERIFIED_IN_REPO`) | Hệ quả & Lưu ý bảo toàn |
|---|---|---|---|
| **Setup 1:** Cài bộ Agent Skills từ `https://skills.addy.ie/` vào dự án | **HOÀN THÀNH ĐẦY ĐỦ** | `cmd /c npx -y skills list` xác nhận đủ 25/25 project skills từ `addyosmani/agent-skills`; đủ `.agents/references/` cho liên kết tương đối `../../references/*.md`. | Sẵn sàng phục vụ toàn bộ vòng đời phát triển tiếp theo. |
| **Setup 2:** Cài `@astryxdesign/core`, `@stylexjs/stylex`, `@astryxdesign/theme-neutral`, `@astryxdesign/cli` và chạy `init` | **HOÀN THÀNH (Kèm điểm lệch nhỏ cần lưu ý)** | `npm ls --depth=0` xác nhận đủ 4 gói; `AGENTS.md` và `GEMINI.md` có khối `ASTRYX:START..END`; `npx @astryxdesign/cli doctor` đạt `18 pass, 0 fail, 3 warn`. | Không tự động khóa dự án vào kiến trúc thuần Node.js/React ở cấp hệ thống; cần sửa `"license": "ISC"` thành `"Apache-2.0"` khi chốt `OQ-007`. |
| **Setup 3:** Hỏi look-and-feel, cài theme qua CLI (mặc định `neutral`), kết nối `<Theme>` và cho xem preview | **HOÀN THÀNH PHẦN CẤU HÌNH & CODE MẪU — CHƯA CÓ BẢN CHẠY XEM TRỰC QUAN (NO LIVE PREVIEW)** | Owner đã chọn `neutral`; `npx @astryxdesign/cli theme list` báo `neutral (added: yes, default: yes)`; `astryx-themes.js` và `src/App.tsx` vượt qua kiểm tra tĩnh của `astryx doctor`. Tuy nhiên **chưa có Vite/bundler/`index.html`** nên chưa từng dựng lên giao diện hiển thị trên trình duyệt. | Giữ nguyên cấu hình theme `neutral` đã cài; chỉ bổ sung bundler và dựng giao diện chuẩn từ Astryx template sau khi chốt kiến trúc và cấu trúc thư mục (`OQ-001`, `OQ-007`). |

---

## 4. Trạng thái Git tại mốc hoàn thành bước A00

- **Nhánh làm việc:** `main` (đồng bộ với `origin/main`).
- **Tệp thay đổi/tạo mới trong bước A00:** Tạo mới 4 tệp quản trị bối cảnh trong `docs/` (`docs/README.md`, `docs/governance/project-charter.md`, `docs/governance/decisions.md`, `docs/governance/assumptions.md`), không chỉnh sửa bất kỳ tệp cấu hình, theme hay mã nguồn nào đã có trước đó.
