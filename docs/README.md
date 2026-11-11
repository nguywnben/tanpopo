# Tanpopo Documentation & Context Hub (Updated through Phase A06 — `DRAFT / PROPOSED ARCHITECTURE, UX & OPS`)

Chào mừng đến với trung tâm tài liệu chuẩn hóa bối cảnh, khám phá sản phẩm, phân tích nghiệp vụ, đặc tả yêu cầu, nền tảng mở rộng, kiến trúc giải pháp, trải nghiệm người dùng/nhà phát triển và vận hành SRE của dự án **Tanpopo** (`https://github.com/nguywnben/tanpopo`). Bộ tài liệu này được thiết lập qua các bước **ASTRA A00**, **A01 (Product Discovery)**, **A02 (Business Analysis)**, **A03 (Product Definition, PRD/SRS & Feature Specs)**, **A04 (Extension Platform & Connectivity Deep Specifications)**, **A05 (Solution Architecture, Data Model, Topology, Contracts & ADRs)** và **A06 (UX/UI Architecture, Astryx Integration, Developer Experience & SRE Operations)** nhằm tạo dựng nguồn sự thật duy nhất (Single Source of Truth) cho con người và các AI Agents ở những bước tiếp theo (**A07+**) làm việc chính xác mà không phải đoán mò.

> **Trạng thái Cổng Giai đoạn Hiện tại:**
> - **Gate G0 (ASTRA A00 — Chuẩn hóa bối cảnh):** `COMPLETED`
> - **Gate G1 (ASTRA A01 — Product Discovery):** **`READY_FOR_REVIEW`** *(Product Owner chưa duyệt `SD-G1-01`..`SD-G1-05`; không tự đánh dấu `APPROVED`).*
> - **Baseline A02 (ASTRA A02 — Phân tích nghiệp vụ):** `DRAFT_COMPLETED` *(Đã tích hợp và truy vết 100% sang A03–A06).*
> - **Gate G2 (ASTRA A03 — Product Definition, PRD/SRS & Feature Specs):** **`READY_FOR_REVIEW`** *(Bộ đặc tả `DRAFT — UNLOCKED SCOPE` kèm 6 đánh đổi `TD-G2-01`..`06`; chờ Product Owner phê duyệt).*
> - **Baseline A04 (ASTRA A04 — Extension Platform & Connectivity Specs):** **`READY_FOR_REVIEW`** *(Bộ đặc tả `ILLUSTRATIVE CANDIDATE — PRE-ADR & PRE-POC` cho Extension Platform, Auth/Connections, Marketplace, MCP/A2A, API Protocols và Contract Test Matrix).*
> - **Gate G3 (ASTRA A05 — Solution Architecture & Technical Design):** **`READY_FOR_REVIEW`** *(Bộ thiết kế kiến trúc `PROPOSED — PRE-APPROVAL` gồm `system-design.md`, `data-model.md`, `deployment-topology.md`, `routing-and-policy.md`, `usage-billing-observability.md`, `api-contracts.md`, 6 ADRs `ADR-0001`..`0006` và 4 giao thức thử nghiệm `SPIKE-01`..`04`).*
> - **Baseline A06 (ASTRA A06 — UX/UI, Developer Experience & SRE Operations):** **`READY_FOR_REVIEW`** *(Bộ đặc tả trải nghiệm & vận hành `PROPOSED — NO CODE` gồm 4 tệp `docs/ux/*` và 4 tệp `docs/operations/*`; giữ nguyên theme `neutral` mặc định, không khóa theme chưa duyệt, không chạy `theme eject` và chưa viết code ứng dụng).*

---

## 1. Bản đồ đọc tài liệu cho các phiên / Prompt tiếp theo (Reading Guide)

Trước khi thực hiện bất kỳ bước lập kế hoạch triển khai chi tiết, thử nghiệm PoC hay viết code nào cho Tanpopo, hãy đọc theo đúng thứ tự sau:

### 1.1. Tầng Quản trị & Từ điển Chuẩn (`docs/governance/` & `docs/product/glossary.md`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **1** | [`docs/governance/project-charter.md`](./governance/project-charter.md) | Tầm nhìn sản phẩm, khác biệt `extension-first`, sự phân tách 4 lớp (`Provider` / `Plugin` / `MCP` / `A2A`), chính sách `Official vs Community` & ranh giới không hợp thức hóa consumer subscriptions. | **Luôn đọc đầu tiên** trong mọi phiên làm việc mới. |
| **2** | [`docs/governance/decisions.md`](./governance/decisions.md) | Trạng thái các Cổng (`G0`, `G1`, `G2`, `A04`, `Gate G3`, `A06`), quyết định đã chốt (`DEC-001`..`DEC-008`), hướng đã bác bỏ (`REJ-001`..`REJ-004`), `SD-G1-01`..`05`, `OQ-001`..`007`, `BA-OQ-001`..`005`, `TD-G2-01`..`06`, `POC/POL` và danh mục `ADR-0001`..`0006`. | Trước khi lập kế hoạch xây dựng, chạy PoC hoặc tổ chức lại thư mục. |
| **3** | [`docs/product/glossary.md`](./product/glossary.md) | Từ điển chuẩn toàn dự án: 12 tác nhân, phân biệt chuỗi `Adapter` → `Connection` → `Model` → `Deployment`, nhãn nguồn gốc Plugin, phân tách khả năng kỹ thuật và quyền sử dụng hợp pháp. | Khi thiết kế schema dữ liệu, đặt tên API hoặc thiết kế nhãn giao diện. |
| **4** | [`docs/governance/assumptions.md`](./governance/assumptions.md) | Các giả thuyết làm việc chưa xác thực thực nghiệm (`ASM-001`..`ASM-010`) gắn với giao thức `SPIKE-01`..`04` và danh sách điểm lệch thực tế trong repository (`ISS-001`..`ISS-007`). | Khi kiểm tra rủi ro giả định hoặc xử lý cấu hình hiện có trong repo. |

### 1.2. Tầng Kiến trúc Giải pháp, Lược đồ Dữ liệu, Hạ tầng SRE & ADRs (`docs/architecture/` — `PROPOSED`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **5** | [`docs/architecture/system-design.md`](./architecture/system-design.md) | **`ARCH-SYS-01`:** Kiến trúc Go Modular Monolith tách vai trò (`--role`), sơ đồ C4 Level 1–3, ma trận chấm điểm tech stack, đường nóng 12 bước (`Request -> Provider`), đường cài đặt Plugin không rebuild Core, bảo mật chống SSRF/DNS-Rebinding, SLO/Error Budgets, 6 tác vụ nền và 4 giao thức PoC (`SPIKE-01`..`04`). | **Tài liệu kiến trúc gốc** cho mọi kỹ sư Backend, Security, Frontend và SRE. |
| **6** | [`docs/architecture/data-model.md`](./architecture/data-model.md) | **`ARCH-DATA-01`:** Quy tắc khả chuyển `SQLite WAL` ↔ `PostgreSQL 16+`, lược đồ vật lý 15 bảng cốt lõi (`UUIDv7`, sổ cái `BIGINT micro-USD`, `RLS`, `Write-Only Vault`, `Hash-Chained Audit`), chỉ mục theo query plan, di trú `Expand/Contract`, xóa sạch `< 60s` (*Crypto-Shredding*) và sao lưu tách `Master KEK`. | Khi viết migration SQL, tầng `storage/port` và quy trình sao lưu/phục hồi. |
| **7** | [`docs/architecture/deployment-topology.md`](./architecture/deployment-topology.md) | **`ARCH-OPS-01`:** Ba chế độ triển khai (Standalone, Compose, Multi-AZ K8s HA), cơ chế `Copy-on-Write ConfigSnapshot` + truyền tin thu hồi 3 lớp (`< 1s` / `< 5s`), ma trận bộ nhớ đệm, giới hạn tốc độ Lua phân tán, `PgBouncer` và nâng cấp không gián đoạn (`SM-08`). | Khi thiết kế Docker/Helm, cấu hình cân bằng tải SSE, bộ nhớ đệm và kịch bản vận hành HA. |
| **8** | [`docs/architecture/adr/README.md`](./architecture/adr/README.md) | **Bộ 6 ADRs (`ADR-0001` .. `ADR-0006`):** Ghi lại toàn bộ lý do, phương án thay thế, mức độ đảo ngược và tác động di trú cho ngôn ngữ Core (`Go`), Sandbox 3 tầng (`wazero`), Lưu trữ kép (`SQLite/Postgres`), giao diện `Astryx` nhúng tĩnh, Sổ cái hai pha `micro-USD` và Ảnh chụp cấu hình nhanh. | Trước khi đề xuất thay đổi bất kỳ quyết định nền tảng hoặc thư viện cốt lõi nào. |

### 1.3. Tầng Thiết kế Trải nghiệm Người dùng (UX/UI) & Tích hợp Astryx (`docs/ux/` — `PROPOSED`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **9** | [`docs/ux/information-architecture.md`](./ux/information-architecture.md) | **`SPEC-UX-01`:** Khung `AppShell` (`TopNav` ngữ cảnh + `SideNav` 5 nhóm), bản đồ URL 11 phân khu IA, ma trận hiển thị theo 6 vai trò RBAC, chuẩn nhất quán danh từ miền và hiển thị 3 chiều tin cậy (`Origin`, `Publisher Tier`, `Execution Tier`) chống cài mù một chạm. | Khi tổ chức điều hướng, định tuyến SPA và kiểm tra thuật ngữ giao diện. |
| **10** | [`docs/ux/user-flows.md`](./ux/user-flows.md) | **`SPEC-UX-02`:** Đặc tả **8 hành trình đầu-cuối (`UJ-01`..`UJ-08`)** kèm sơ đồ và ma trận đủ **5 trạng thái (`Happy`, `Loading`, `Empty`, `Error`, `Offline`)** từ `TTFGR < 3 phút` tới duyệt MCP `destructive`, nâng cấp tăng quyền và gỡ lỗi `request_id`. | Khi lập trình luồng tương tác màn hình và viết kịch bản kiểm thử E2E. |
| **11** | [`docs/ux/screen-specifications.md`](./ux/screen-specifications.md) | **`SPEC-UX-03`:** Bộ **6 rào chắn UX (`UX-GR-01`..`06`)**, hợp đồng tiếp cận `WCAG 2.2 AA` / đáp ứng 3 ngưỡng (`1280px` / `768px` / `375px`) và đặc tả chi tiết **16 màn hình (`SCR-01`..`16`)** với đủ 11 thuộc tính kiểm chứng được. | Khi xây dựng từng trang giao diện, biểu mẫu, hộp xác nhận và kết nối API `/admin/v1/*`. |
| **12** | [`docs/ux/design-system-integration.md`](./ux/design-system-integration.md) | **`SPEC-UX-04`:** Kiểm kê trung thực setup Astryx v0.7.13 hiện có vs đề xuất `ADR-0004` (giữ `neutral` mặc định, cấm `theme eject`), phân vai `Badge` (chỉ đếm số) vs `Token` (biểu tượng + nhãn trạng thái), ánh xạ 16 màn hình sang `Astryx Page Templates` thật và **12 tiêu chí khả dụng định lượng (`UX-AC-01`..`12`)**. | Khi khởi tạo template Astryx, cấu hình Vite + StyleX và kiểm định `astryx doctor --built`. |

### 1.4. Tầng Vận hành Tự chủ SRE & Trải nghiệm Nhà phát triển (`docs/operations/` — `PROPOSED`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **13** | [`docs/operations/installer-upgrades.md`](./operations/installer-upgrades.md) | **`SPEC-OPS-01`:** Khởi chạy đa nền tảng (`Linux` / `macOS` / `Windows`), 8 phép tiền kiểm `tanpopo doctor`, `Bootstrap Token` vs GitOps không giao diện, TLS/Nginx SSE, tường lửa & danh sách trắng GPU nội bộ chống SSRF, di trú `SQLite -> Postgres`, nâng cấp không gián đoạn `SM-08` và gói ngoại tuyến `Air-Gap`. | Khi đóng gói bộ cài, viết đơn vị `systemd`/Docker/Helm và kịch bản nâng cấp. |
| **14** | [`docs/operations/backups-recovery.md`](./operations/backups-recovery.md) | **`SPEC-OPS-02`:** Kiến trúc sao lưu tách rời `Master KEK`, lịch lưu giữ phân tầng (`0 byte` prompt mặc định, `TTL <= 72h` gỡ lỗi, `>= 365d` sổ cái/kiểm toán), xóa vĩnh viễn `< 60s` (*Crypto-Shredding*), **4 sổ tay phục hồi thảm họa (`DR-01`..`04`, `RTO < 15m`)** và ma trận hỗ trợ nền tảng/trình duyệt/hợp đồng `N-1` (`12m`). | Khi lập trình công cụ `tanpopo backup/vault`, tác vụ dọn TTL và diễn tập khôi phục thảm họa. |
| **15** | [`docs/operations/monitoring-incidents.md`](./operations/monitoring-incidents.md) | **`SPEC-OPS-03`:** Bảng chân trị `/healthz` vs `/readyz` vs `/metrics`, cảnh báo triệu chứng (`PAGE` vs `TICKET`), **7 sổ tay ứng phó sự cố trực chiến (`INC-01`..`07`)** (cạn hạn mức, lộ khóa, cách ly plugin + `blast-radius`, sập hãng đích, lệch schema MCP, suy giảm cụm) và **12 tiêu chí vận hành kiểm thử được (`OPS-AC-01`..`12`)**. | Khi cấu hình Prometheus/OTel alerts, kiểm thử chịu lỗi và trực chiến sản xuất. |
| **16** | [`docs/operations/developer-experience.md`](./operations/developer-experience.md) | **`SPEC-OPS-04`:** Cây lệnh và bảng mã thoát tất định của `tanpopo` CLI, công thái học Management API (`ETag`/`If-Match`, con trỏ), đoạn mã mẫu đa ngôn ngữ (OpenAI SDK, Native Passthrough, MCP), kiến trúc tài liệu, bảng giải quyết lỗi chuẩn `CanonicalError`, gói chẩn đoán che sạch bí mật `tanpopo support-bundle` và chính sách SemVer SDK. | Khi lập trình `tanpopo` CLI, viết tài liệu tích hợp SDK và bộ lọc che bí mật chẩn đoán. |

### 1.5. Tầng Đặc tả Sâu Extension Platform, Định tuyến, Tài chính & Hợp đồng API (`docs/specifications/` & `docs/quality/`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **17** | [`docs/specifications/api-contracts.md`](./specifications/api-contracts.md) | **`SPEC-API-01`:** Hợp đồng mức thiết kế **OpenAPI 3.1** (Data Plane + Admin API + Auth/OAuth + Kill-Switch), **JSON Schema** (Normalized Envelope) và **Protobuf `proto3`** (`ExtensionRuntimeService`), quy tắc timeout/cancel/idempotency. | Khi sinh kiểu dữ liệu API, viết handler HTTP/ConnectRPC và viết client CLI/Web Console. |
| **18** | [`docs/specifications/routing-and-policy.md`](./specifications/routing-and-policy.md) | **`SPEC-CORE-01`:** Đại số hợp nhất chính sách phân cấp (`Org ∩ Project ∩ Env`), ma trận `Fail-Closed` vs `Fail-Open`, thuật toán định tuyến 6 bước, toán học ngắt mạch `SM-04` và hợp đồng `Dry-Run Route Explain`. | Khi lập trình `policy/engine`, `routing/resolver` và màn hình mô phỏng tuyến. |
| **19** | [`docs/specifications/usage-billing-observability.md`](./specifications/usage-billing-observability.md) | **`SPEC-CORE-02`:** Ranh giới cứng thanh toán `COMMERCIAL_API` / `SELF_HOSTED_INFRA`, toán học số nguyên `nano/micro-USD`, giữ chỗ hai pha `SM-07`, chuẩn chỉ số/vết/nhật ký **OpenTelemetry** kiểm soát cardinality, che bí mật và cảnh báo kèm Runbook. | Khi lập trình `finops/ledger`, `telemetry/otel` và thiết lập cảnh báo giám sát. |
| **20** | [`docs/specifications/extension-contract.md`](./specifications/extension-contract.md) | **`SPEC-EXT-01`:** Taxonomy 8 thực thể mở rộng vs 7 phần Core Kernel bất khả xâm phạm; SDK/API Contract đa ngôn ngữ (`N-1` / `12m`); manifest mẫu; 3 tầng thực thi cấm mã không tin cậy chạy in-process. | Khi thiết kế SDK phần mở rộng, hồ chứa Wasm và trình nạp gói. |
| **21** | [`docs/specifications/provider-adapter.md`](./specifications/provider-adapter.md) | **`SPEC-EXT-02`:** Phân tách `Adapter → Connection → LogicalModel → Deployment`; 3 cấp độ bảo chứng năng lực (`DECLARED` / `TESTED` / `VERIFIED`); gói Declarative mẫu và ma trận fallback. | Khi viết Official Adapters và trình thông dịch Gói Khai báo Tầng 1. |
| **22** | [`docs/specifications/authentication-connections.md`](./specifications/authentication-connections.md) | **`SPEC-EXT-03`:** Hợp đồng xác thực (`Zero Full-Vault Access`, `Write-Only Plaintext`, `No Token Cross-Audience`); 5 phương thức xác thực; single-flight refresh và **Case Study rủi ro Consumer Subscriptions** không có cơ chế lách ToS. | Khi lập trình Credential Vault, OAuth Broker và màn hình kết nối. |
| **23** | [`docs/specifications/marketplace-lifecycle.md`](./specifications/marketplace-lifecycle.md) | **`SPEC-EXT-04`:** Phân tách `Package Origin ≠ Publisher Trust ≠ Runtime Safety`; 4 nguồn cài đặt; đóng gói khép kín; luồng 9 bước cài đặt/rollback/quarantine; `tanpopo.lock`, SBOM và `revocations.json`. | Khi lập trình trình cài đặt Extension, khóa `tanpopo.lock` và kênh Marketplace. |
| **24** | [`docs/specifications/mcp-a2a-boundaries.md`](./specifications/mcp-a2a-boundaries.md) | **`SPEC-EXT-05`:** Ranh giới 4 mặt phẳng; MCP Hub (`<server>__<tool>`, ACL từng công cụ, cô lập audience RFC 8707, ghim `schema_hash` JCS, phê duyệt con người sync/async, cấm retry mù); và mặt phẳng A2A (`hop_count <= 5`). | Khi lập trình MCP Gateway Hub, phê duyệt con người và kết nối tác tử A2A. |
| **25** | [`docs/specifications/api-protocol-compatibility.md`](./specifications/api-protocol-compatibility.md) | **`SPEC-EXT-06`:** Tương thích chi tiết từng năng lực mô hình, đa phương thức chống SSRF, SSE `< 1 ms` & hủy `< 500 ms`, `Idempotency-Key` và bảng mã lỗi chuẩn `Canonical Error Taxonomy`. | Khi kiểm tra chi tiết hành vi giao thức đường truyền và xử lý biên. |
| **26** | [`docs/quality/contract-test-matrix.md`](./quality/contract-test-matrix.md) | **`SPEC-QA-01`:** Kiến trúc Cassette Test Harness ngoại tuyến, ma trận 24 bài kiểm thử hợp đồng & tấn công sandbox (`CT-01`..`CT-24`) và kịch bản nghiệm thu `EXT-ACCEPT-01`. | Khi xây dựng hạ tầng kiểm thử hợp đồng và cổng kiểm định CI. |

### 1.6. Tầng Định nghĩa Sản phẩm & Đặc tả Hệ thống — PRD/SRS (`docs/product/` & `docs/requirements/` — `DRAFT`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **27** | [`docs/product/prd.md`](./product/prd.md) | Vấn đề, chân dung người dùng, giá trị cốt lõi, `PG-01`..`05`, `NG-01`..`08`, chỉ tiêu đo lường dự kiến `MO-01`..`06`, bản đồ 10 phân hệ phi chu trình (`Capability Map`) và ràng buộc `PC-01`..`06`. | Khi đối chiếu mục tiêu sản phẩm và ranh giới phi mục tiêu. |
| **28** | [`docs/product/release-scope.md`](./product/release-scope.md) | Phân kỳ `MVP (v0.1)` → `v0.2` → `v0.3` → `v1.0` → `Post-v1.0`, tiêu chí xuất xưởng từng cột mốc và 6 đánh đổi trọng yếu (`TD-G2-01`..`06`). | Khi lập kế hoạch triển khai theo giai đoạn, tránh nhồi nhét tính năng vào `v0.1`. |
| **29** | [`docs/product/feature-catalog.md`](./product/feature-catalog.md) | Danh mục 16 tính năng (`FT-01`..`FT-16`) trải trên 10 miền nghiệp vụ với đủ 14 thuộc tính kiểm chứng được và phân biệt rõ hành vi nhà cung cấp hỗ trợ / không hỗ trợ. | Khi thiết kế chi tiết từng phân hệ, CLI và màn hình giao diện. |
| **30** | [`docs/requirements/srs.md`](./requirements/srs.md) | **Nguồn chân lý đặc tả hệ thống:** Ma trận Giao thức/Năng lực 14 dòng, `53` yêu cầu chức năng (`TP-FR-0101`..`1005` — Must/Should/Could/Later) và `22` yêu cầu phi chức năng đo lường được (`TP-NFR-0101`..`1002` — mục tiêu dự kiến). | Khi đối chiếu yêu cầu bắt buộc và ngưỡng NFR mục tiêu. |
| **31** | [`docs/requirements/acceptance-criteria.md`](./requirements/acceptance-criteria.md) | Kịch bản nghiệm thu `Given/When/Then` cho toàn bộ chức năng (`TP-AC-F*`), 8 nhóm rủi ro bảo mật/tuân thủ (`TP-AC-SEC-01`..`08`) và xác minh phi chức năng (`TP-AC-N01`..`10`). | Khi thiết kế chiến lược kiểm thử, viết test tự động và nghiệm thu Cổng chất lượng. |
| **32** | [`docs/requirements/traceability.md`](./requirements/traceability.md) | Ma trận truy vết đầu-cuối hai chiều (`UC/WF/OPS → TP-BR/SM → FT → TP-FR/NFR → TP-AC/UX-AC/OPS-AC → ARCH/ADR/SPEC → Placeholders MOD/TST`) cùng bảng tự kiểm tra Gate G2, Gate G3 & A06. | Khi cập nhật gói mã (`MOD-*`) và bộ test (`TST-*`). |

### 1.7. Tầng Phân tích Nghiệp vụ & Khám phá Sản phẩm (`docs/requirements/` & `docs/discovery/`)

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **33** | [`docs/requirements/business-entity-map.md`](./requirements/business-entity-map.md) | Ma trận 12 tác nhân, sơ đồ quan hệ thực thể (ER), quyền sở hữu/chia sẻ và **8 Máy trạng thái (`SM-01`..`SM-08`)** cho toàn bộ vòng đời hệ thống. | Khi cài đặt RBAC và máy trạng thái vòng đời. |
| **34** | [`docs/requirements/workflows.md`](./requirements/workflows.md) | **13 Hành trình đầu-cuối (`WF-01`..`WF-13`)**, sơ đồ trình tự và toàn bộ tình huống phủ định/biên. | Khi cài đặt luồng điều phối chi tiết và xử lý ngoại lệ. |
| **35** | [`docs/requirements/business-rules.md`](./requirements/business-rules.md) | Bộ **30 quy tắc nghiệp vụ `TP-BR-0101`..`0803`** độc lập công nghệ kèm chủ sở hữu và ngoại lệ. | Khi cài đặt động cơ chính sách, kiểm toán bảo mật và sổ cái ngân sách. |
| **36** | [`docs/requirements/workflow-requirement-candidates.md`](./requirements/workflow-requirement-candidates.md) | **9 nhóm yêu cầu vận hành (`OPS-01`..`OPS-09`)**, lịch sử ánh xạ ứng viên và 5 điểm mơ hồ nghiệp vụ (`BA-OQ-001`..`005`). | Khi đối chiếu yêu cầu vận hành gốc. |
| **37** | [`docs/discovery/positioning-and-success-metrics.md`](./discovery/positioning-and-success-metrics.md) | Định vị sản phẩm đề xuất, North Star Metric (`WGMR`), ranh giới sản phẩm và các thử nghiệm `EXP-01`..`06`. | Khi đối chiếu định vị chiến lược và thiết kế thử nghiệm PoC. |
| **38** | [`docs/discovery/users-jobs-use-cases.md`](./discovery/users-jobs-use-cases.md) | Xếp hạng 5 phân khúc khách hàng, nhóm Anti-Personas, 4 vai trò chức năng và 9 Use Cases khám phá (`UC-01`..`UC-09`). | Khi đối chiếu ngữ cảnh người dùng thực tế. |
| **39** | [`docs/discovery/market-and-competitors.md`](./discovery/market-and-competitors.md) | Đối chiếu chi tiết 4 nhóm đối thủ thị trường 2025–2026 kèm nguồn dẫn chiếu. | Khi đối chiếu tính năng cạnh tranh. |
| **40** | [`docs/discovery/opportunity-and-feasibility.md`](./discovery/opportunity-and-feasibility.md) | Phân tầng Parity/Differentiators/Moats, phân vùng pháp lý/TOS và 7 ý tưởng giá trị cao (`IDEA-01`..`07`). | Khi đối chiếu lợi thế cạnh tranh và ranh giới tuân thủ. |
| **41** | [`docs/discovery/risk-register.md`](./discovery/risk-register.md) | Xếp hạng 15 rủi ro chiến lược, bảo mật & kỹ thuật (`RSK-001`..`RSK-015`) kèm biện pháp kiểm soát kiến trúc. | Khi lập kế hoạch kiểm soát rủi ro kỹ thuật và kiểm thử an ninh. |

### 1.8. Quy ước Công cụ & Kỹ năng trong Workspace

| Thứ tự | Đường dẫn thực tế | Nội dung cốt lõi | Khi nào bắt buộc đọc |
|---|---|---|---|
| **42** | [`AGENTS.md`](../AGENTS.md) & [`GEMINI.md`](../GEMINI.md) | Quy ước bắt buộc của **Astryx Design System v0.7.13** (template-first, cấm thẻ `div` thô, `Badge` chỉ dùng cho số đếm, dùng design tokens, chạy `astryx doctor`). | Khi thiết kế hoặc lập trình giao diện Web Console dùng Astryx. |
| **43** | [`.agents/skills/using-agent-skills/SKILL.md`](../.agents/skills/using-agent-skills/SKILL.md) | Quy trình kích hoạt bộ 25 kỹ năng kỹ thuật phần mềm theo từng giai đoạn phát triển (Define → Plan → Build → Verify → Review → Ship). | Khi bắt đầu một tác vụ kỹ thuật cụ thể theo quy trình chuẩn. |

---

## 2. Phân biệt rạch ròi: Đã tồn tại trong Code vs Chỉ có trong Thiết kế Tài liệu

> **Kết luận cốt lõi (`VERIFIED_IN_REPO`):** Tính đến hết bước **A06**, repository **vẫn chưa có bất kỳ dòng mã thực thi nghiệp vụ AI Gateway, mã giao diện màn hình mới hay mã PoC nào** (chưa có server, chưa có Unified API, chưa có routing/policy/cost engine, chưa có plugin runtime/SDK, chưa có MCP/A2A, chưa có CLI và chưa thêm bundler). Toàn bộ hệ thống hiện được định nghĩa đầy đủ ở tầng **kiến trúc giải pháp, lược đồ dữ liệu, hợp đồng giao thức, thiết kế UX/UI Astryx, công thái học DX và sổ tay vận hành SRE (`PROPOSED — PRE-APPROVAL`)** chờ Product Owner phê duyệt các cổng mở trước khi bước sang lập kế hoạch công việc và lập trình.

### 2.1. Kiểm kê toàn bộ hiện trạng Repository (`Repository Inventory`)

| Đường dẫn trong Repo | Phân loại thực tế | Chi tiết kiểm chứng (`VERIFIED_IN_REPO`) |
|---|---|---|
| [`LICENSE`](../LICENSE) | **Tệp pháp lý gốc** | Giấy phép **Apache License 2.0** (201 dòng, tạo từ commit khởi đầu `0a35b74`). |
| [`.gitignore`](../.gitignore) | **Cấu hình Git** | Loại trừ `node_modules/`, `dist/`, `build/`, `out/`, `coverage/`, `.env*`, logs và tệp tạm IDE. |
| [`skills-lock.json`](../skills-lock.json) | **Lockfile công cụ Agent** | Ghi nhận phiên bản và mã băm SHA-256 của 25 skills sao chép từ `addyosmani/agent-skills`. |
| [`.agents/skills/*`](../.agents/skills) | **Quy trình Agent (25 skills)** | 25 thư mục kỹ năng (`SKILL.md` kèm tài liệu/script cục bộ). Hoạt động đầy đủ. |
| [`.agents/references/*`](../.agents/references) | **Checklist kỹ thuật (7 tệp)** | Các danh mục kiểm tra dùng chung (`security`, `performance`, `accessibility`, `observability`, `testing-patterns`, `orchestration-patterns`, `definition-of-done`). |
| [`.agents/agents/*`](../.agents/agents) | **Định nghĩa Subagents (4 tệp)** | Personas chuyên trách: `code-reviewer`, `security-auditor`, `test-engineer`, `web-performance-auditor`. |
| [`.agents/plugin.json`](../.agents/plugin.json) | **Siêu dữ liệu tham khảo** | Tệp manifest gốc của `agent-skills` (`v0.6.15`, giấy phép MIT). |
| [`.gemini/commands/*.toml`](../.gemini/commands) | **Cấu hình lệnh Gemini CLI** | 9 tệp `.toml` định nghĩa lệnh tắt cho Gemini CLI. |
| [`AGENTS.md`](../AGENTS.md) & [`GEMINI.md`](../GEMINI.md) | **Quy ước AI Agent cho UI** | Khối cấu hình tự động sinh bởi `npx @astryxdesign/cli init` cho Astryx v0.7.13 (174 components). |
| [`package.json`](../package.json) & [`package-lock.json`](../package-lock.json) | **Cấu hình gói Node.js sơ khai** | Cài đặt 4 gói trực tiếp: `@astryxdesign/core@^0.7.13`, `@astryxdesign/theme-neutral@^0.7.13`, `@astryxdesign/cli@^0.7.13`, `@stylexjs/stylex@^0.19.1`. Chưa có script `dev`/`build`, đang ghi `"license": "ISC"`. |
| [`astryx-themes.js`](../astryx-themes.js) | **Module Theme sinh tự động** | Sinh bởi `npx astryx theme add neutral`, xuất `themes` và `defaultThemeSlug = "neutral"`. **Giữ nguyên 100%, không chạy `theme eject`.** |
| [`src/App.tsx`](../src/App.tsx) & [`src/main.tsx`](../src/main.tsx) | **Mã nguồn giữ chỗ tĩnh (Unbundled Placeholder)** | 2 tệp React/TSX (34 dòng và 13 dòng) kết nối `Theme` `neutral` với các component mẫu của Astryx. **Chưa chỉnh sửa ở bước A06.** |
| `docs/governance/*` & `docs/README.md` | **Tài liệu Quản trị & Bối cảnh (A00 → A06)** | Bao gồm `docs/README.md`, `project-charter.md`, `decisions.md`, `assumptions.md`. |
| `docs/discovery/*` | **Tài liệu Product Discovery & Rủi ro (A01 → A05)** | Bao gồm 5 tệp: `market-and-competitors.md`, `users-jobs-use-cases.md`, `opportunity-and-feasibility.md`, `positioning-and-success-metrics.md`, `risk-register.md`. |
| `docs/product/*` | **Tài liệu Định nghĩa Sản phẩm Nháp (A02 → A03 — `DRAFT`)** | Bao gồm 4 tệp: `glossary.md`, `prd.md`, `feature-catalog.md`, `release-scope.md`. |
| `docs/requirements/*` | **Tài liệu Nghiệp vụ, SRS & Truy vết (A02 → A06 — `DRAFT`)** | Bao gồm 7 tệp: `business-entity-map.md`, `workflows.md`, `business-rules.md`, `workflow-requirement-candidates.md`, `srs.md`, `acceptance-criteria.md`, `traceability.md`. |
| `docs/specifications/*` | **Tài liệu Đặc tả Sâu Nền tảng, Định tuyến, Tài chính & API Contracts (A04 → A05)** | Bao gồm 9 tệp: `extension-contract.md`, `provider-adapter.md`, `authentication-connections.md`, `marketplace-lifecycle.md`, `mcp-a2a-boundaries.md`, `api-protocol-compatibility.md`, `routing-and-policy.md`, `usage-billing-observability.md`, `api-contracts.md`. |
| `docs/architecture/*` & `docs/architecture/adr/*` | **Tài liệu Kiến trúc Hệ thống, Lược đồ Vật lý, Hạ tầng & ADRs (A05 — `PROPOSED`)** | Bao gồm 10 tệp: `system-design.md`, `data-model.md`, `deployment-topology.md`, `adr/README.md` và `ADR-0001`..`ADR-0006`. |
| `docs/ux/*` | **Tài liệu Thiết kế UX/UI & Tích hợp Astryx (A06 — `PROPOSED`)** | Bao gồm 4 tệp: `information-architecture.md`, `user-flows.md`, `screen-specifications.md`, `design-system-integration.md`. |
| `docs/operations/*` | **Tài liệu Vận hành SRE, Cài đặt & Developer Experience (A06 — `PROPOSED`)** | Bao gồm 4 tệp: `installer-upgrades.md`, `backups-recovery.md`, `monitoring-incidents.md`, `developer-experience.md`. |
| `docs/quality/*` | **Tài liệu Chất lượng & Ma trận Kiểm thử Hợp đồng (A04 — `PRE-ADR`)** | Bao gồm 1 tệp: `contract-test-matrix.md` (`CT-01`..`CT-24` & `EXT-ACCEPT-01`). |

---

## 3. Báo cáo Xác minh Thực nghiệm 3 Nhiệm vụ Setup trước Phiên A00

| Nhiệm vụ trước A00 | Kết luận xác minh | Bằng chứng thực nghiệm (`VERIFIED_IN_REPO`) | Hệ quả & Lưu ý bảo toàn |
|---|---|---|---|
| **Setup 1:** Cài bộ Agent Skills từ `https://skills.addy.ie/` vào dự án | **HOÀN THÀNH ĐẦY ĐỦ** | `cmd /c npx -y skills list` xác nhận đủ 25/25 project skills từ `addyosmani/agent-skills`; đủ `.agents/references/` cho liên kết tương đối `../../references/*.md`. | Sẵn sàng phục vụ toàn bộ vòng đời phát triển tiếp theo. |
| **Setup 2:** Cài `@astryxdesign/core`, `@stylexjs/stylex`, `@astryxdesign/theme-neutral`, `@astryxdesign/cli` và chạy `init` | **HOÀN THÀNH (Kèm điểm lệch nhỏ cần lưu ý)** | `npm ls --depth=0` xác nhận đủ 4 gói; `AGENTS.md` và `GEMINI.md` có khối `ASTRYX:START..END`; `npx @astryxdesign/cli doctor` đạt `18 pass, 0 fail, 3 warn`. | Không tự động khóa dự án vào kiến trúc thuần Node.js/React ở cấp hệ thống; phương án nhúng SPA tĩnh và sửa `"license": "Apache-2.0"` được trình duyệt tại `ADR-0004` & `SPEC-UX-04`. |
| **Setup 3:** Hỏi look-and-feel, cài theme qua CLI (mặc định `neutral`), kết nối `Theme` và cho xem preview | **HOÀN THÀNH PHẦN CẤU HÌNH & CODE MẪU — CHƯA CÓ BẢN CHẠY XEM TRỰC QUAN (NO LIVE PREVIEW)** | Owner đã chọn `neutral`; `npx @astryxdesign/cli theme list` báo `neutral (maintained: yes, added: yes, default: yes)`; `astryx-themes.js` và `src/App.tsx` vượt qua kiểm tra tĩnh của `astryx doctor`. Tuy nhiên **chưa có Vite/bundler/`index.html`** nên chưa từng dựng lên giao diện hiển thị trên trình duyệt. | Giữ nguyên cấu hình theme `neutral` đã cài; không `theme eject` và không tự ý khóa theme khác; chỉ dựng trang chuẩn từ `npx astryx template <id>` sau khi Owner phê duyệt. |

---

## 4. Trạng thái Git hiện tại sau bước A06 (`DRAFT / PROPOSED ARCHITECTURE, UX & OPS`)

- **Nhánh làm việc:** `main` (đồng bộ với `origin/main`).
- **Tệp thay đổi/tạo mới qua A00 → A06:** Chỉ làm việc trong thư mục `docs/` (tổng cộng **48 tệp Markdown** gồm `docs/README.md`, 3 tệp `docs/governance/*`, 5 tệp `docs/discovery/*`, 4 tệp `docs/product/*`, 7 tệp `docs/requirements/*`, 9 tệp `docs/specifications/*`, 10 tệp `docs/architecture/*`, 4 tệp `docs/ux/*`, 4 tệp `docs/operations/*` và 1 tệp `docs/quality/*`), **không chỉnh sửa bất kỳ tệp cấu hình, theme hay mã nguồn nào đã có trước đó**. Mỗi bước được lưu mốc bằng một atomic Git commit riêng biệt theo quy ước `DEC-008` (`docs: step <NN> - ...`).
