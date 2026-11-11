# Product Requirements Document (PRD) — Tanpopo

> **Tài liệu Định nghĩa Sản phẩm (Phase ASTRA A03 — Gate G2: `READY_FOR_REVIEW` | `DRAFT — UNLOCKED SCOPE`)**
> - **Phiên bản:** `0.1.0-step03-draft`
> - **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Principal Requirements Engineer & Product Manager
> - **Trạng thái quản trị:** **Bản nháp chưa khóa phạm vi (`DRAFT`)** do Cổng **Gate G1** và **Gate G2** tại [`docs/governance/decisions.md`](../governance/decisions.md) đang ở trạng thái `READY_FOR_REVIEW` (chờ Product Owner phê duyệt). Tài liệu viết theo chuẩn kỹ thuật kiểm chứng được, không dùng ngôn ngữ quảng cáo.
> - **Liên kết Nguồn chân lý (SSOT):**
>   - Từ điển chuẩn: [`docs/product/glossary.md`](./glossary.md)
>   - Danh mục tính năng chi tiết: [`docs/product/feature-catalog.md`](./feature-catalog.md)
>   - Phân kỳ phiên bản: [`docs/product/release-scope.md`](./release-scope.md)
>   - Đặc tả yêu cầu hệ thống (`TP-FR` / `TP-NFR`): [`docs/requirements/srs.md`](../requirements/srs.md)
>   - Tiêu chí nghiệm thu (`TP-AC`): [`docs/requirements/acceptance-criteria.md`](../requirements/acceptance-criteria.md)

---

## 1. Tuyên bố Vấn đề Nghiệp vụ & Kỹ thuật (Problem Statement)

Các nhóm phát triển sản phẩm và đội ngũ hạ tầng AI đang vận hành nhiều mô hình (thương mại lẫn tự host), nhiều công cụ MCP và tác nhân A2A đang đối mặt với 4 nút thắt cấu trúc mà các cổng hiện tại chưa giải quyết trọn vẹn (dẫn chứng tại [`market-and-competitors.md`](../discovery/market-and-competitors.md)):

1. **Sự phụ thuộc cứng giữa tích hợp Provider và chu kỳ phát hành Core:** Các cổng mã nguồn mở phổ biến đóng cứng hàng trăm bộ chuyển đổi nhà cung cấp trong cùng một kho mã nguồn. Khi hãng AI ra mắt mô hình/tham số mới hoặc doanh nghiệp có cổng mô hình nội bộ riêng, người vận hành buộc phải chờ bản cập nhật Core mới (chịu rủi ro lỗi hồi quy toàn cục) hoặc tự duy trì bản fork tốn kém.
2. **Thiếu vùng cô lập an toàn cho mã mở rộng bên thứ ba:** Việc nạp script tùy chỉnh trực tiếp vào tiến trình Gateway tạo nguy cơ lộ toàn bộ khóa bí mật gốc (`Secret Credential`) hoặc rò rỉ dữ liệu prompt ra máy chủ trái phép; ngược lại, các gói cộng đồng thiếu bộ kiểm thử hợp đồng chuẩn rất nhanh hỏng ngầm khi nhà cung cấp thay đổi định dạng phản hồi.
3. **Phân mảnh quản trị giữa Suy luận (LLM), Công cụ (MCP) và Tác nhân (A2A) hoặc vướng rào cản hạ tầng/bản quyền:** Người dùng phải hoặc chấp nhận khóa các tính năng quản trị cơ bản (RBAC, ngân sách cứng, nhật ký kiểm toán, lọc quyền MCP) sau tường phí Enterprise thương mại, hoặc gánh độ phức tạp vận hành của cụm Kubernetes/Envoy/Istio chỉ để có một mặt phẳng chính sách thống nhất.
4. **Rủi ro pháp lý và độ tin cậy từ vùng xám thuê bao tiêu dùng:** Nhiều công cụ cộng đồng trộn lẫn cơ chế xác thực hợp pháp với việc trích xuất phiên đăng nhập của các gói thuê bao cá nhân giá cố định (`ChatGPT Plus/Pro`, `Codex`, `Claude Pro/Max`, `Antigravity`...), dẫn tới khóa tài khoản diện rộng và không thể đưa vào môi trường sản xuất nghiêm túc.

---

## 2. Chân dung Người dùng & Tác nhân Mục tiêu (Target Personas & Actors)

Chi tiết xếp hạng từ [`users-jobs-use-cases.md`](../discovery/users-jobs-use-cases.md) và 12 tác nhân tại [`business-entity-map.md`](../requirements/business-entity-map.md):

| Nhóm ưu tiên (`PROPOSED` — Chờ duyệt `SD-G1-01`) | Phân khúc tổ chức | Tác nhân trực tiếp (`Actors`) | Nhu cầu cốt lõi cần giải quyết ngay |
|---|---|---|---|
| **Mũi nhọn #1 (`P0`)** | **Dev Teams (5–50 kỹ sư)** | `Organization Owner`, `Team Admin`, `Developer`, `API Client`, `MCP Client` | Khởi chạy dưới 3 phút; gom khóa chính thức & mô hình cục bộ sau một cổng duy nhất; đặt trần ngân sách cứng không bị lố khi bắn luồng song song; bảo vệ quyền gọi công cụ MCP. |
| **Đồng mũi nhọn #2 (`P0/P1`)** | **AI Platform Teams (50–500 kỹ sư)** | `Operator`, `Extension Publisher`, `Reviewer`, `Team Admin` | Tự viết và cài đặt `Provider Adapter` nội bộ từ Git/Private Registry có cô lập bảo mật mà không cần sửa Core; ép buộc vùng cư trú dữ liệu (`Data Residency`) và xuất chuẩn OpenTelemetry/Audit. |
| **Lan tỏa cộng đồng #3 (`P1`)** | **Solo Developers & Power Users chính thống** | `Personal Admin`, `Developer`, `Agent` | Một tiến trình/container nhẹ (`< 128 MB RAM` khi rỗi) quản lý khóa BYOK thương mại, `Ollama`/`vLLM` cục bộ và danh sách công cụ MCP cho các tác nhân lập trình. |
| **Giai đoạn sau #4 (`v1.0+`)** | **Regulated Enterprise (>1.000 nhân sự)** | `Operator`, `Reviewer`, `External IdP` | Cụm HA đa vùng, HSM/KMS chuyên dụng, phê duyệt bốn mắt bắt buộc và gói cài đặt mạng kín (`Air-gapped Bundle`). |

- **Đối tượng loại trừ vĩnh viễn (`CONFIRMED_BY_OWNER` — `REJ-003`):** Người dùng tìm kiếm công cụ biến gói thuê bao tiêu dùng cá nhân thành tín dụng API miễn phí/dùng chung; các hệ thống chợ đen bán lại khóa API trái điều khoản.

---

## 3. Giá trị Cốt lõi, Mục tiêu & Phi Mục tiêu (Value Proposition, Goals & Non-Goals)

### 3.1. Giá trị Cốt lõi Kiểm chứng được (Core Value Proposition)
Tanpopo cung cấp một **mặt phẳng điều phối AI tự vận hành (self-hosted) duy nhất** cho phép:
1. Cài đặt, nâng cấp, ghim phiên bản và cách ly **Provider Adapters cùng các Extensions** từ Marketplace, Git, URL/gói nén hoặc Private Registry dưới mô hình cấp quyền mặc định đóng (`Default-Deny`) **mà không cần phát hành lại nhân Core**.
2. Thực thi thống nhất xác thực khóa ảo, định tuyến nhận thức năng lực, chính sách vùng dữ liệu, giữ chỗ ngân sách hai pha và nhật ký kiểm toán bất biến trên **4 lớp nghiệp vụ tách biệt rõ ràng: `Provider`, `Plugin`, `MCP`, và `A2A`**.
3. Duy trì ranh giới tuân thủ minh bạch giữa `Official Adapters` (ưu tiên API/phương thức xác thực chính thức) và `Community/Private Extensions` (có kiểm định quyền hạn và nhãn nguồn gốc rõ ràng, không bao giờ bảo chứng hay duy trì connector vi phạm điều khoản nhà cung cấp).

### 3.2. Mục tiêu Sản phẩm (`Product Goals`)
- **`PG-01` (`CONFIRMED_BY_OWNER`):** Vận hành thực dụng ở quy mô nhỏ (khởi chạy đơn giản kèm sẵn Web Console & CLI) và có khả năng mở rộng ngang không trạng thái khi lưu lượng tăng cao.
- **`PG-02` (`CONFIRMED_BY_OWNER`):** Cho phép mở rộng nhà cung cấp, giao thức xác thực và bộ lọc chính sách độc lập hoàn toàn với chu kỳ phát hành Tanpopo Core thông qua Hợp đồng Plugin có phiên bản, SDK và bộ kiểm thử tương thích.
- **`PG-03` (`CONFIRMED_BY_OWNER`):** Bảo toàn ranh giới mô hình dữ liệu, giao thức và phân quyền giữa 4 lớp `Provider`, `Plugin`, `MCP` và `A2A`.
- **`PG-04` (`PROPOSED`):** Ngăn chặn thất thoát tài chính và rò rỉ bí mật thông qua cơ chế giữ chỗ ngân sách trước khi mở luồng (`Pre-Request Hold`), tiêm khóa mù tại biên mạng (`Blind Credential Injection`) và phê duyệt con người cho công cụ có tính phá hủy.
- **`PG-05` (`PROPOSED`):** Đảm bảo tính trung thực tuyệt đối về năng lực giao thức và hạch toán tài chính (không cắt xén tham số ngầm, không chuyển mạch lén sau khi đã phát luồng một phần, không ghi nhận `0 USD` giả tạo khi thiếu số đo token).

### 3.3. Phi Mục tiêu (`Explicit Non-Goals`)
- **`NG-01` (`CONFIRMED_BY_OWNER` / `REJ-001`):** Không làm công cụ chỉ xoay vòng API key đơn giản thiếu routing, policy, telemetry, cost control và cơ chế mở rộng.
- **`NG-02` (`CONFIRMED_BY_OWNER` / `REJ-002`):** Không đóng cứng mọi tích hợp nhà cung cấp AI vào mã nguồn nhân Tanpopo Core.
- **`NG-03` (`CONFIRMED_BY_OWNER` / `REJ-003`):** Không phát triển, đóng gói, liệt kê trên kênh chính thức hoặc dùng hộp thoại "chấp nhận rủi ro" để hợp thức hóa việc biến gói thuê bao tiêu dùng cá nhân (`ChatGPT Plus/Pro`, `Codex`, `Claude Pro/Max`, `Antigravity`...) thành general-purpose API.
- **`NG-04` (`CONFIRMED_BY_OWNER` / `REJ-004`):** Không gộp chung `Provider`, `Plugin`, `MCP` và `A2A` thành một thực thể hay một giao thức vạn năng.
- **`NG-05` (`PROPOSED`):** Không lưu trữ trọng số mô hình hay trực tiếp chạy suy luận GPU/CPU (vai trò của `vLLM`, `Ollama`, `SGLang`).
- **`NG-06` (`PROPOSED`):** Không xây dựng khung lập trình luồng ứng dụng (như LangChain/LlamaIndex) hay hệ thống chấm điểm/tinh chỉnh mô hình (Evals/Fine-tuning) bên trong nhân Core.
- **`NG-07` (`PROPOSED`):** Không vận hành cổng thanh toán trung gian bán lại tín dụng AI thu phí phần trăm trong phạm vi dự án mã nguồn mở cốt lõi.
- **`NG-08` (`PROPOSED`):** Không thu thập dữ liệu đo xa bắt buộc (forced phone-home telemetry) từ máy chủ tự vận hành của người dùng.

---

## 4. Kết quả Đo lường Mục tiêu (Measurable Outcomes — *Mục tiêu dự kiến trước Benchmark*)

> **Lưu ý kiểm chứng:** Toàn bộ ngưỡng số lượng dưới đây là **mục tiêu thiết kế dự kiến (`PROPOSED Pre-Benchmark Targets`)**, sẽ được đo đạc thực nghiệm qua bộ kiểm thử tải trước khi chốt bản `v0.1` và `v1.0`.

| Mã chỉ tiêu | Thước đo kiểm chứng | Ngưỡng mục tiêu dự kiến | Phương pháp xác minh |
|---|---|---|---|
| **MO-01** | **North Star vận hành (`WGMR`)** | Hiển thị chính xác trên mỗi máy chủ số truy vấn đa mặt phẳng được quản trị qua Extension hợp lệ mỗi tuần | Kiểm thử đối chiếu sổ cái nội bộ & bộ xuất OpenTelemetry (`TP-FR-0801`). |
| **MO-02** | **Tỷ lệ thêm Provider chuẩn không cần nâng cấp Core** | `100%` đối với các nhà cung cấp tương thích hợp đồng hiện hành sau khi đóng băng Contract | Kiểm thử cài đặt gói Adapter mới trên tệp thực thi Core giữ nguyên không biên dịch lại (`TP-AC-F0301`). |
| **MO-03** | **Thời gian khởi chạy tới truy vấn đầu tiên (`TTFGR`)** | `< 3 phút` từ lúc tải gói tới lúc hoàn tất truy vấn đầu tiên qua Setup Wizard / CLI | Kịch bản kiểm thử bấm giờ người dùng mới trên môi trường sạch (`WF-01`, `WF-02`). |
| **MO-04** | **Thời gian tạo Adapter khai báo mới (`TTFCA`)** | `< 30 phút` với gói khai báo chuẩn (Tier-1) đạt `100%` bài kiểm thử hợp đồng mẫu | Thử nghiệm thực tế `EXP-03` cùng lệnh kiểm thử CLI (`TP-FR-0305`). |
| **MO-05** | **Độ trễ nội bộ cộng thêm tại Gateway (`p95 Overhead`)** | `< 2 ms` (gói khai báo) / `< 5 ms` (gói thực thi cô lập), chưa tính mạng nhà cung cấp | Đo tải tự động trên 1.000 luồng SSE đồng thời (`TP-NFR-0101`, `EXP-01`). |
| **MO-06** | **Độ trễ áp dụng lệnh thu hồi khóa / cách ly Plugin** | `< 1 giây` (chế độ đơn nút) / `< 5 giây` (chế độ cụm phân tán HA) | Kiểm thử tiêm sự cố thu hồi khẩn cấp khi đang có tải liên tục (`TP-NFR-0404`). |

---

## 5. Bản đồ Phân hệ Năng lực & Chiều Phụ thuộc (`Capability Map`)

Tuân thủ nguyên tắc phân rã phi chu trình (acyclic module boundaries) để từng phân hệ có thể đặc tả, kiểm thử và bàn giao độc lập:

| Module ID (Ổn định) | Trách nhiệm cốt lõi | Phụ thuộc trực tiếp (Một chiều) | Mặt phẳng kiến trúc |
|---|---|---|---|
| `selfhost-ops` | Khởi tạo máy chủ, cấu hình, lưu trữ, kho bí mật gốc, sao lưu & nâng cấp | — *(Tầng nền tảng)* | Hạ tầng & Vận hành |
| `iam-tenancy` | Tổ chức (`Org`/`Project`/`Env`), `Principal`, `RBAC`, `Virtual Key` | `selfhost-ops` | Control & Data Plane |
| `policy-security` | Động cơ đánh giá chính sách, vùng dữ liệu, phê duyệt `ApprovalRequest`, nhật ký `AuditEvent` | `selfhost-ops`, `iam-tenancy` | Xuyên suốt 4 lớp |
| `telemetry-finops` | Sổ cái `BudgetLedger`, giữ chỗ hai pha, quyết toán `EXACT`/`ESTIMATED`, xuất OTel | `selfhost-ops`, `iam-tenancy` | Xuyên suốt 4 lớp |
| `plugin-marketplace` | Vòng đời `Extension`, vùng cô lập, tiêm khóa mù, kiểm thử hợp đồng, kết nối Registry/Git/ZIP | `selfhost-ops`, `policy-security` | Lớp **Plugin / Extension** |
| `provider-credentials` | Quản lý `Secret Credential` (API Key, Authorized OAuth), `Provider Connection`, `Model`, `Model Deployment` | `iam-tenancy`, `policy-security`, `plugin-marketplace` | Lớp **Provider** |
| `routing-resilience` | Định tuyến nhận thức năng lực, chuyển mạch trước luồng, ngắt mạch (`Circuit Breaker`), mô phỏng khô | `provider-credentials`, `policy-security`, `telemetry-finops` | Lớp **Provider** |
| `gateway-protocols` | Điểm cuối Unified API, Native Dialect Passthrough, xử lý luồng SSE/Multimodal, chuẩn hóa lỗi | `iam-tenancy`, `routing-resilience`, `telemetry-finops` | Lớp **Provider Ingress** |
| `mcp-a2a` | Mặt phẳng **MCP** (đăng ký server, lọc tool, ghim `schema_hash`, chặn retry nguy hiểm) & Mặt phẳng **A2A** (`AgentCard`, ủy quyền tác vụ) | `iam-tenancy`, `policy-security`, `telemetry-finops`, `plugin-marketplace` | Lớp **MCP** & Lớp **A2A** |
| `console-cli-api` | Admin REST API, công cụ dòng lệnh `tanpopo` CLI và giao diện Web Console (Astryx) | Tất cả các phân hệ nghiệp vụ phía trên | Giao diện Quản trị & Tác giả |

---

## 6. Phụ thuộc & Ràng buộc Sản phẩm (Dependencies & Product Constraints)

| Mã | Phân loại | Mô tả ràng buộc / phụ thuộc bắt buộc | Nguồn / Trạng thái |
|---|---|---|---|
| **PC-01** | **Pháp lý & Bản quyền** | Toàn bộ mã nguồn dự án hướng tới giấy phép mở thống nhất (**Apache-2.0** tại [`LICENSE`](../../LICENSE); chờ chốt đồng bộ `package.json` tại `SD-G1-05`); không nhúng thư viện copyleft lây nhiễm (`GPL`/`AGPL`) vào nhân Core. | `VERIFIED_IN_REPO` / `OPEN` |
| **PC-02** | **Cô lập Bảo mật Plugin** | Mọi phần mở rộng từ nguồn ngoài phải bị giới hạn tài nguyên, mặc định không có quyền mạng/tệp/biến môi trường nếu không được khai báo trong Manifest và được quản trị viên phê duyệt. | `CONFIRMED_BY_OWNER` (`C-02`) |
| **PC-03** | **Tuân thủ Điều khoản Nhà cung cấp** | Tách biệt rõ khả năng kỹ thuật của SDK và quyền hợp pháp do Provider cấp; không duy trì hoặc quảng bá connector biến thuê bao tiêu dùng cá nhân thành general-purpose API. | `CONFIRMED_BY_OWNER` (`C-03`, `DEC-004`) |
| **PC-04** | **Bảo toàn Khởi tạo Giao diện** | Phần giao diện Web Console sử dụng **Astryx Design System v0.7.13** với gói giao diện mặc định `neutral` đã cài đặt sẵn (tuân thủ nghiêm quy tắc không dùng thẻ `div`/`span` thô, khởi tạo từ template chuẩn và vượt qua `astryx doctor`), đồng thời không ép buộc nhân Gateway Core phải viết cùng ngôn ngữ với giao diện. | `CONFIRMED_BY_OWNER` & `VERIFIED_IN_REPO` (`C-04`) |
| **PC-05** | **Vận hành Không Phụ thuộc Đám mây Độc quyền** | Chế độ tự vận hành cơ bản phải chạy hoàn chỉnh trong mạng riêng (bao gồm khả năng xuất gói cài offline ở giai đoạn trưởng thành) mà không yêu cầu tài khoản đám mây của dự án Tanpopo. | `CONFIRMED_BY_OWNER` (`DEC-001`) |
| **PC-06** | **Phụ thuộc Chuẩn Giao thức Mở** | Các mặt phẳng kết nối tuân thủ thông số kỹ thuật công khai của OpenAI API (cho giao diện hợp nhất), đặc tả gốc của từng hãng (cho Native Passthrough), chuẩn **Model Context Protocol (MCP)**, chuẩn **Agent2Agent (A2A)** và chuẩn **OpenTelemetry (OTLP)**. | `PROPOSED` |

---

## 7. Tóm tắt Phân kỳ Lộ trình (`MVP v0.1` → `v1.0` → `Post-v1.0`)

Chi tiết ma trận tính năng theo từng cột mốc được quy định duy nhất tại [`docs/product/release-scope.md`](./release-scope.md) (`DRAFT`):
- **`MVP (v0.1)`:** Tập trung giải quyết trọn vẹn bài toán cốt lõi số 1 — *Core Gateway + Unified/Passthrough API + Động cơ Plugin cô lập + Bộ Official Reference Adapters + Khóa ảo/Định tuyến/Ngân sách đơn nút + CLI & Web Console tối thiểu dùng được ngay*.
- **`v0.2`:** Bổ sung mặt phẳng quản trị **MCP Plane** (lọc quyền tool, ghim `schema_hash`, phê duyệt công cụ `destructive`), **Authorized OAuth 2.1**, và **Chỉ mục Marketplace tĩnh / Private Registry**.
- **`v0.3`:** Bổ sung mặt phẳng **A2A Plane** và chế độ **Scale-out HA đa bản sao** (cơ sở dữ liệu & bộ đếm phân tán).
- **`v1.0`:** Đóng băng cam kết tương thích ngược `Plugin Contract v1.0`, kiểm toán bảo mật độc lập, hỗ trợ gói mạng kín (`Air-gapped Bundle`) và quản trị đa tổ chức đầy đủ.
- **`Post-v1.0`:** Mở rộng các giao thức phương tiện luồng hai chiều thời gian thực (Realtime Voice/Video WebRTC/WebSocket nâng cao), chợ kiểm định đối tác mở rộng và tích hợp hệ sinh thái quan sát/đánh giá chuyên sâu bên ngoài.
