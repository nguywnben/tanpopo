# Bản đồ Cột mốc, Tiêu chí Xuất xưởng & Cổng Tích hợp Giai đoạn (Milestone Map & Phase Gates) — Tanpopo

> **Tài liệu Kế hoạch Cột mốc Nháp (Phase ASTRA A09 — Gate G4: `READY_FOR_REVIEW` | `DRAFT — EXECUTION BASELINE UNLOCKED`)**
> - **Mã tài liệu:** `PLAN-MS-01` | **Phiên bản:** `0.1.0-step09-draft` | **Ngày lập:** `2026-11-14`
> - **Vai trò biên soạn:** Technical Program Manager & QA Lead
> - **Liên kết chặt chẽ:** [`roadmap.md`](./roadmap.md), [`dependency-graph.md`](./dependency-graph.md), [`implementation-index.md`](./implementation-index.md), [`release-scope.md`](../product/release-scope.md) và [`release-readiness.md`](../quality/release-readiness.md).

---

## Tổng quan 6 Cột mốc Thực thi & Cổng Kiểm soát Tích hợp (`M0` → `M5`)

Không một cột mốc tiếp theo nào được phép mở rộng quy mô nếu **Cổng Tích hợp (`Integration Gate`)** của cột mốc liền trước chưa vượt qua `100%` bằng lệnh kiểm chứng tự động và phê duyệt con người tương ứng:

---

### Cột mốc `M0`: Đóng Lỗ hổng Đặc tả A08 & Khóa Thực nghiệm Kỹ thuật (`Spec Closure & PoC Spikes`)
- **Mục tiêu (`Goal`):** Giải quyết triệt để toàn bộ `11 CRITICAL` và `14 MAJOR` issues từ kiểm toán A08 sau khi Product Owner chốt `CR-01`..`10`, đồng thời đo thực nghiệm `SPIKE-01`..`04` để khóa cứng giao diện nhị phân Wasm (`POC-01`) và ngữ pháp khai báo Tầng 1 (`POC-02`) trước khi viết dòng code sản phẩm đầu tiên.
- **Đơn vị thực thi bao hàm:** `TP-IMP-0001` .. `TP-IMP-0004` (`4` units).
- **Độ phủ Yêu cầu / Khoảng trống:** Đóng `GAP-01` .. `GAP-13`; xác thực giả thuyết `ASM-002`, `ASM-004`, `ASM-005`, `ASM-006`.
- **Sản phẩm Bàn giao Đo được (`Measurable Deliverables`):**
  1. Máy trạng thái `SM-01`..`09` không còn điểm chết; `srs.md` và `acceptance-criteria.md` đủ `100%` `AC` cho `TP-FR-0701`, `0702`, `0901`.
  2. Tệp `proto/tanpopo/extension/v1/runtime.proto` và `openapi/{admin-v1,dataplane-v1}.yaml` trích xuất kiểm tra cú pháp đạt `0 errors`.
  3. Lược đồ `data-model.md` đủ toàn bộ các bảng còn thiếu (`26` bảng chuẩn), khóa tổ hợp `(org_id, id)` + `FORCE RLS`, `Project DEK` và lược đồ khóa `tp:v1:{org_id}:*`.
  4. Báo cáo kết quả đo `SPIKE-01`..`04` cập nhật vào `ADR-0002` và `ADR-0005`.
- **Rủi ro chính:** Biên bộ nhớ Wasm trong `SPIKE-02` vượt trần `5 ms p95` khi gọi từng mảnh SSE → kích hoạt phương án dự phòng chỉ gọi Wasm đầu/cuối luồng và dùng bộ tách khung gốc của Host ở giữa.
- **Cổng Tích hợp `GATE-M0` (`Spec & Spike Gate`):**
  - Tự động: `0` lỗi cú pháp trên `.proto`, `OpenAPI 3.1`, DDL SQL thử nghiệm trên cả SQLite và PostgreSQL 16.
  - Con người: **Product Owner + Principal Architect** ký duyệt đóng `Audit A08` (`FAIL -> PASS`) và mở khóa **Gate G4** cho `M1`.

---

### Cột mốc `M1`: Nền tảng Hạt nhân & Lát cắt Dọc Chứng minh External Provider Adapter (`Foundation & Core Proof Vertical Slice`)
- **Mục tiêu (`Goal`):** Xây dựng khung Monorepo chuẩn `Apache-2.0`, đường ống CI khép kín cấm gọi Internet, kho khóa mã hóa phong bì, xác thực khóa ảo, `Blind Credential Broker` và **chứng minh đầu-cuối kịch bản sống còn `EXT-ACCEPT-01`: phát triển, kiểm thử băng ghi ngoại tuyến, cài đặt nóng, duyệt quyền và gọi suy luận SSE qua `External Provider Adapter` (cả Tầng 1 lẫn Tầng 2 Wasm) mà không sửa hay khởi động lại Tanpopo Core**.
- **Đơn vị thực thi bao hàm:** `TP-IMP-0005` .. `TP-IMP-0015` (`11` units).
- **Độ phủ Yêu cầu (`SRS`):** `TP-FR-0101` (chat SSE), `0103`, `0104`, `0201`..`0204`, `0301`..`0306`, `0501`..`0503` (cốt lõi), `0702` (SSRF), `0703`, `TP-NFR-0101`, `0102`, `0202`, `0401`..`0403`, `1001`.
- **Sản phẩm Bàn giao Đo được (`Measurable Deliverables`):**
  1. Cấu trúc monorepo sạch (`cmd/tanpopo`, `internal/*`, `sdk/*`, `web/console/`), `Stage 0..2 CI` chạy `testkit/netguard` chặn `100%` IP ngoài `127.0.0.1/::1`.
  2. Kho `SQLite WAL` + `Envelope Vault` (`Master KEK -> Org DEK -> Project DEK`) + xác thực `Virtual Key` `O(1)` trên `ConfigSnapshot`.
  3. Công cụ `tanpopo plugin init / validate / test / pack` và trình nạp nóng `Declarative` + `wazero` Sandbox + `Blind Broker`.
  4. Bài kiểm thử tự động lát cắt dọc cốt lõi `EXT-SLICE-M1` (các bước nền tảng của `EXT-ACCEPT-01`: cài nóng `.tpkg` → duyệt quyền → tiêm khóa mù → gọi `/v1/chat/completions` đồng bộ & SSE thành công không đổi mã băm nhị phân Core; trọn vẹn 10 bước bao gồm Định tuyến `EU`, Sổ cái `\$1.00` và `Blast-Radius` được khóa hoàn chỉnh tại `TP-IMP-0032` / `GATE-M2`).
- **Rủi ro chính:** Rò rỉ goroutine khi khách hủy luồng SSE đột ngột hoặc kẹt phiên `wazero` khi gặp vòng lặp vô hạn (`RSK-001`, `RSK-004`).
- **Cổng Tích hợp `GATE-M1` (`Vertical Slice Proof Gate`):**
  - Tự động: Vượt qua `CT-01`..`07`, `CT-10`..`11`, `CT-14`..`18`, `CT-24`..`26`, `FZ-01`..`03`, `FZ-05`, `EXT-SLICE-M1`, `go test -race ./...` xanh `100%` và quét `CanaryScanner` đạt `0` rò rỉ khóa.
  - Con người: `Security Lead + Platform Architect` rà soát sâu (`REQUIRES_STRONG_REVIEW`) toàn bộ ranh giới `wazero`, `Blind Broker` và `SSRFGuard`.

---

### Cột mốc `M2`: Hoàn thiện Toàn diện `MVP (v0.1)` — Định tuyến, Sổ cái `Micro-USD`, Astryx Console, CLI & Tự Vận hành Đơn nút (`Usable Self-Hosted MVP`)
- **Mục tiêu (`Goal`):** Đưa Tanpopo đạt trạng thái **dùng được ngay trong thực tế sản xuất quy mô nhỏ/vừa (`TTFGR < 3 phút`)** với đầy đủ giao diện hợp nhất + phương ngữ gốc, 5 Official Reference Adapters, định tuyến nhận thức năng lực + vùng dữ liệu `Fail-Closed`, sổ cái ngân sách hai pha `micro-USD`, OTel + kiểm toán chuỗi băm, công tắc khẩn cấp 4 cấp, giao diện Web Console **Astryx `neutral`** đạt `0 fail` trên `astryx doctor --built`, `tanpopo` CLI và bộ cài đặt đa nền tảng.
- **Đơn vị thực thi bao hàm:** `TP-IMP-0016` .. `TP-IMP-0033` (`18` units).
- **Độ phủ Yêu cầu (`SRS`):** **Đạt `100%` toàn bộ `36/36` `Must` FRs và `18/18` `Must` NFRs (cấp đơn nút `v0.1`)**, cộng thêm `TP-FR-0105` (`Native Passthrough`) và phần cốt lõi của `TP-FR-0803` (cảnh báo ngân sách mềm).
- **Sản phẩm Bàn giao Đo được (`Measurable Deliverables`):**
  1. Đầy đủ `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/models`, luồng `blob_ref` đa phương thức an toàn RAM, `/native/{dialect}/*` và 5 gói Official Adapters (`OpenAI`, `Anthropic`, `Gemini`, `OpenAI-Compatible/vLLM`, `Ollama`).
  2. Động cơ chính sách `Org ∩ Project ∩ Env`, giới hạn tốc độ `RPM/TPM/Concurrency`, bộ định tuyến 6 bước + `SM-04`, sổ cái `Two-Phase Hold` (`EXACT/ESTIMATED/UNMETERED_FLAGGED`), OTel `Drop-Oldest` và chuỗi băm `audit_events` + `Kill-Switch < 1s`.
  3. Toàn bộ `/admin/v1/*` (`ETag`/`409`), ứng dụng SPA Astryx `neutral` nhúng tĩnh trong binary `tanpopo` (14 màn hình `v0.1`, phông `Figtree` đóng gói nội bộ), và bộ tạo gói/ảnh container cho Linux, macOS, Windows kèm `CycloneDX SBOM` + chữ ký `Cosign`.
- **Rủi ro chính:** Ôm đồm chi tiết giao diện làm trễ mốc ra mắt (`RSK-002`) hoặc vi phạm quy tắc thẻ thô của Astryx.
- **Cổng Tích hợp `GATE-M2` (`MVP v0.1 Release Candidate Gate`):**
  - Tự động: Xanh `100%` trên `Stage 0..4 CI` phạm vi `v0.1` **bao gồm trọn vẹn 10/10 bước của `EXT-ACCEPT-01` với `PID` và `sha256sum(tanpopo)` bất biến**; `npx @astryxdesign/cli doctor --built web/console/dist` đạt `0 fail`; `@axe-core/playwright` đạt `0` lỗi `critical/serious`; kịch bản `UJ-01` hoàn tất `< 180s`; sao lưu/khôi phục (`.tpop.enc`) và `Crypto-Shredding < 60s` đạt chuẩn.
  - Con người: **Product Owner** nghiệm thu trực tiếp `GATE-REL-01`..`07` trước khi cho phép gắn thẻ `v0.1.0`.

---

### Cột mốc `M3`: Hệ sinh thái `v0.2` — `Authorized OAuth 2.1`, Marketplace/OCI, `MCP Governance Hub` & Công cụ Kỹ sư (`Agentic Tooling & Registry`)
- **Mục tiêu (`Goal`):** Mở rộng Tanpopo thành cổng điều phối thống nhất cho cả **Suy luận Mô hình + Công cụ MCP**, bổ sung luồng `Authorized OAuth 2.1` làm mới đơn luồng, kết nối Chỉ mục Marketplace tĩnh & Kho OCI riêng, công cụ `Dry-Run Route Explain`, `Playground` và thông báo `Webhooks`.
- **Đơn vị thực thi bao hàm:** `TP-IMP-0034` .. `TP-IMP-0038` (`5` units).
- **Độ phủ Yêu cầu (`SRS`):** Toàn bộ nhóm `Should` (`TP-FR-0205`, `0307`, `0401`..`0404`, `0604`, `0803` đầy đủ, `0904`, `TP-NFR-0602`) + `Bedrock`/`Azure`/`/v1/rerank` (`CR-01`).
- **Sản phẩm Bàn giao Đo được (`Measurable Deliverables`):**
  1. `OAuth 2.1` Broker (`PKCE S256`, `Client Credentials`, `Device Flow`) vượt qua bài bắn 100 luồng song song (`1` lệnh refresh duy nhất) và cô lập `RFC 8707`.
  2. Điểm cuối `/mcp` lọc quyền công cụ có phân trang `> 200` công cụ, tự khóa khi lệch `schema_hash` JCS, chặn retry `mutating`/`destructive` và duyệt con người tương thích trình khách `Claude Code` / `Cursor`.
  3. Màn hình `SCR-10` (MCP), `SCR-15` (`Playground` khóa phiên 15 phút), trình mô phỏng `Route Explain` (`SCR-08`) và trình gửi `Webhook` ký HMAC.
- **Rủi ro chính:** Lệch hành vi phiên `MCP Streamable HTTP` giữa các IDE tác nhân khác nhau (`RSK-006`).
- **Cổng Tích hợp `GATE-M3` (`v0.2 Release Gate`):**
  - Tự động: Vượt qua `CT-19`..`22`, `AUTH-09`, `10`, `12`, `16`, `FZ-04` và kiểm thử tương thích trình khách MCP chuẩn.
  - Con người: `Security Lead + Product Owner` ký duyệt tuân thủ `DEC-004` / `POL-01` trên mọi kết nối OAuth và cổng phát hành `v0.2.0`.

---

### Cột mốc `M4`: Quy mô Doanh nghiệp `v0.3` — Cụm Không Trạng thái `PostgreSQL 16 + Valkey`, IAM/KMS Nâng cao & Mặt phẳng `A2A` (`Stateless Scale-Out & Inter-Agent Plane`)
- **Mục tiêu (`Goal`):** Đưa Tanpopo lên kiến trúc cụm nhiều bản sao không trạng thái chịu tải cao (`>= 1.000` luồng SSE/nút `TARGET`), đồng bộ thu hồi khóa toàn cụm `< 5 giây`, di trú liền mạch từ SQLite lên PostgreSQL, tích hợp `OIDC SSO`, `Four-Eyes`, `Cloud KMS / Workload Identity`, `Tier-3 Isolated Sidecar` và mặt phẳng liên tác nhân `A2A`.
- **Đơn vị thực thi bao hàm:** `TP-IMP-0039` .. `TP-IMP-0042` (`4` units).
- **Độ phủ Yêu cầu (`SRS`):** Toàn bộ nhóm `Could` (`TP-FR-0206`, `0405`, `0406`, `0504`, `1005`, `TP-NFR-0201`, `0301`, `0302`) + `CR-02` (`Tier-3`), `CR-09` (`Response Cache` tùy chọn).
- **Sản phẩm Bàn giao Đo được (`Measurable Deliverables`):**
  1. Trình điều khiển `PostgreSQL 16+` (phân vùng bảng theo tháng + `FORCE RLS`), `Stage 2 Dual-DB CI` xanh đồng thời trên cả SQLite và Postgres, cùng lệnh `tanpopo storage migrate` khớp `100%` tổng kiểm.
  2. Bộ điều phối `Valkey` dùng hash-tag `tp:v1:{org_id}:*`, truyền tin thu hồi 3 lớp `< 5s`, ngắt mạch nút mù `> 10s` và bộ Helm Chart tách vai trò `control-plane` / `data-plane`.
  3. Đăng nhập quản trị `OIDC SSO`, quy tắc 4 mắt, bọc khóa `AWS/GCP KMS / Vault`, `Tier-3 Sidecar` `mTLS` và mặt phẳng `/a2a/*` giới hạn ngân sách con & `hop_count <= 5`.
- **Rủi ro chính:** Sai lệch hành vi giao dịch giữa SQLite và Postgres (`RSK-013`) hoặc trễ đồng bộ khi mạng cụm phân mảnh (`RSK-011`).
- **Cổng Tích hợp `GATE-M4` (`v0.3 HA & Enterprise Gate`):**
  - Tự động: Vượt qua `CT-23`, `CT-28`, `CT-32`, `AUTH-01`, `02`, `06`, `08`, `14`, `OPS-AC-03`, `06` và bài tiêm lỗi hỗn loạn cụm 3 nút (giết ngẫu nhiên 1 pod Data Plane / tắt Valkey).
  - Con người: `SRE Lead + Security Lead + Product Owner` phê duyệt cổng `v0.3.0`.

---

### Cột mốc `M5`: Cứng hóa Xuất xưởng `v1.0 GA` — Đóng băng Hợp đồng 12 Tháng, `Air-Gap Bundle`, Kiểm toán An ninh/Hiệu năng & Phát hành Công khai (`Production Freeze & Launch`)
- **Mục tiêu (`Goal`):** Thay thế toàn bộ nhãn `TARGET / UNVERIFIED` bằng bằng chứng đo tải và diễn tập khôi phục thực nghiệm, hoàn tất gói mạng kín `Air-Gap`, mở quản trị đa tổ chức đầy đủ, vượt qua kiểm thử xâm nhập hộp xám, đồng bộ `100%` tài liệu – mã nguồn và trình hồ sơ xuất xưởng để **Product Owner quyết định phát hành `v1.0.0`**.
- **Đơn vị thực thi bao hàm:** `TP-IMP-0043` .. `TP-IMP-0047` (`5` units).
- **Độ phủ Yêu cầu (`SRS`):** Hoàn thiện cam kết dài hạn của `TP-FR-0307` (`Air-Gap`), `TP-FR-0501` (`Multi-Org GA`), `TP-NFR-0902` (`12 tháng SLA`) và toàn bộ `GATE-REL-01`..`07`. *(Hai mục `Later` `Post-v1.0` là `TP-FR-0106` Realtime WebRTC và `TP-FR-0804` Đối chiếu hóa đơn tự động tiếp tục giữ ngoài phạm vi `v1.0` theo đúng `release-scope.md`).*
- **Sản phẩm Bàn giao Đo được (`Measurable Deliverables`):**
  1. Báo cáo benchmark `k6` công khai xác thực `SLO-01`..`08` và biên bản diễn tập bấm giờ `DR-01`..`04` (`RTO < 15m`).
  2. Chạy fuzzing `FZ-01`..`06` liên tục `>= 4 giờ` (`0` crash), công cụ `tanpopo plugin migrate-check` bảo hành `N-1`, và biên bản khắc phục `100%` lỗ hổng `Critical/High` từ kiểm thử xâm nhập.
  3. Công cụ `tanpopo bundle export/verify/import` vượt qua kiểm thử mạng cách ly hoàn toàn (`CT-31`, `OPS-AC-11`), cổng tài liệu người dùng/SDK khớp `100%` mã thực tế và hồ sơ `Release Readiness Dossier`.
- **Cổng Tích hợp `GATE-M5` (`v1.0 GA Human Launch Gate`):**
  - Tự động: Toàn bộ `GATE-REL-01`..`06` đạt xanh `100%`, `0` lỗi `Sev-0`/`Sev-1` mở.
  - Con người (**BẮT BUỘC**): **Product Owner ký duyệt trực tiếp tại `TP-IMP-0047` (`GATE-REL-07`)** — hệ thống CI/Agent **tuyệt đối không tự động phát hành công khai** nếu chưa có lệnh phê duyệt này.
