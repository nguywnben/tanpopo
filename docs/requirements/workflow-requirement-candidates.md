# Yêu cầu Vận hành, Ứng viên Yêu cầu Nghiệp vụ & Tự Kiểm tra (Operational Requirements & Workflow-to-Requirement Candidates) — Tanpopo

> **Tài liệu Phân tích Nghiệp vụ (Phase ASTRA A02 — `DRAFT / UNLOCKED SCOPE`)**
> - **Phiên bản:** `0.1.0-a02-draft`
> - **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Lead Business Analyst & Product Architect
> - **Trạng thái quản trị:** **Bản nháp chưa khóa phạm vi (`DRAFT — UNLOCKED SCOPE`)**. Do Product Owner chưa chính thức duyệt **Gate G1** (`READY_FOR_REVIEW`), toàn bộ danh mục ứng viên yêu cầu dưới đây đóng vai trò đầu vào phân tích cho bước lập PRD tiếp theo (**A03**), chưa khóa cứng phạm vi từng bản phát hành.

---

## 1. Chín Nhóm Yêu cầu Nghiệp vụ Vận hành (`OPS-01` .. `OPS-09`)

| Mã | Lĩnh vực vận hành | Yêu cầu hành vi từ góc nhìn người vận hành & tổ chức (`PROPOSED`) |
|---|---|---|
| **OPS-01** | **Onboarding & Khởi chạy Ngày đầu** | Cung cấp hai chế độ khởi tạo rõ ràng: (1) *Chế độ Cá nhân / Nhóm nhỏ* tự thiết lập không gian mặc định qua Setup Wizard trong dưới 3 phút (`WF-01`, `WF-02`), và (2) *Chế độ Khai báo Tự động (Declarative Bootstrap)* cho phép `Operator` nạp sẵn cấu trúc tổ chức, danh sách plugin đã ghim và chính sách từ tệp cấu hình chuẩn mà không cần thao tác tay qua trình duyệt. |
| **OPS-02** | **Mô hình Vai trò & Phân tách Nhiệm vụ (`RBAC`)** | Phân tách rạch ròi quyền quản trị hạ tầng (`Operator`), chủ sở hữu tổ chức (`Organization Owner`), quản trị dự án (`Team Admin`), thẩm định viên độc lập (`Reviewer`) và kỹ sư sử dụng (`Developer`). Cho phép tổ chức bật tùy chọn kiểm soát chéo hai người (*Four-Eyes Principle*) đối với thao tác duyệt plugin ngoài hoặc nâng trần ngân sách lớn, đồng thời cho phép gộp vai trò gọn nhẹ khi chạy một mình (`Personal Admin`). |
| **OPS-03** | **Kiểm toán (`Audit`) & Truy xuất Điều tra** | Ghi nhận bất biến (`TP-BR-0803`) toàn bộ thay đổi cấu hình, vòng đời khóa, quyết định phê duyệt/từ chối và sự kiện vi phạm bảo mật; hỗ trợ lọc theo thời gian, chủ thể, dự án, mã phần mở rộng và xuất ra định dạng chuẩn phục vụ lưu trữ kiểm toán dài hạn. |
| **OPS-04** | **Quản trị Sự cố (`Incident Response`)** | Cung cấp bộ công tắc khẩn cấp ở 4 cấp độ độc lập để giảm bán kính ảnh hưởng: (1) thu hồi một `Virtual Key`, (2) đóng băng tạm thời một `Project`, (3) ngắt một `Provider Connection`, và (4) cách ly toàn cục một `Extension` (`QUARANTINED` — `WF-12`), kèm báo cáo tức thì phạm vi các khóa/dự án đã chịu tác động. |
| **OPS-05** | **Sao lưu & Phục hồi Thảm họa (`Backup & Recovery`)** | Cho phép xuất và khôi phục an toàn toàn bộ cấu hình quản trị, danh mục phiên bản phần mở rộng đã ghim và sổ cái ngân sách; các thông tin xác thực trong bản sao lưu luôn được mã hóa bằng khóa bảo vệ chủ độc lập để bản sao lưu bị lộ cũng không làm lộ API keys gốc. |
| **OPS-06** | **Lưu giữ & Xóa Dữ liệu (`Retention & Privacy`)** | Áp dụng mặc định không lưu nội dung văn bản nhạy cảm (`TP-BR-0802`); cho phép thiết lập thời hạn sống tự động (`TTL`) riêng biệt cho: (a) nhật ký truy vết kỹ thuật ngắn hạn, (b) nội dung kiểm tra gỡ lỗi (nếu bật), và (c) bút toán tài chính/kiểm toán dài hạn; đảm bảo quy trình xóa sạch dữ liệu khi đóng `Project`/`Organization` (`WF-13`). |
| **OPS-07** | **Khả năng Hỗ trợ & Tự Chẩn đoán (`Supportability`)** | Trang bị công cụ tự kiểm tra sức khỏe toàn diện (tương tự lệnh chẩn đoán hệ thống) và trình giải thích quyết định định tuyến/chính sách khô (`Dry-Run Policy & Route Explain`) để `Operator` và `Developer` tự tìm ra nguyên nhân yêu cầu bị chặn mà không phải đọc mã nguồn hay bật chế độ gỡ lỗi lộ bí mật. |
| **OPS-08** | **Kiểm duyệt Marketplace & Cảnh báo Thu hồi (`Moderation`)** | Vận hành quy trình thẩm định công khai đối với nhãn `verified` (kiểm tra tự động tính tương thích hợp đồng + kiểm tra tuân thủ điều khoản `TP-BR-0302`); duy trì kênh tiếp nhận báo cáo lỗ hổng/vi phạm bản quyền/vi phạm điều khoản và phát hành bản tin thu hồi (`Revocation Advisory`) để các máy chủ tự vận hành nhận cảnh báo kịp thời đối với các gói độc hại. |
| **OPS-09** | **Báo cáo Tài chính & Chính sách Tương thích Hợp đồng (`Reporting & Compatibility Policy`)** | Cung cấp báo cáo phân bổ chi phí theo `Organization`/`Project`/`Environment`/`Virtual Key`/`Model` có tách rõ số liệu `EXACT` và `ESTIMATED`/`UNMETERED` (`TP-BR-0703`). Ban hành chính sách vòng đời Hợp đồng Plugin rõ ràng: khi nâng cấp phiên bản lớn của hệ thống, phải duy trì cửa sổ tương thích tối thiểu cho phiên bản hợp đồng liền trước (`N-1 Contract Support Window`) kèm cảnh báo ngưng hỗ trợ trước một khoảng thời gian công bố rõ ràng. |

---

## 2. Ma trận Ánh xạ Luồng Nghiệp vụ sang Ứng viên Yêu cầu (`Workflow-to-Requirement Candidates` — `DRAFT`)

Bảng dưới đây kết nối mạch lạc các phân hệ năng lực (theo gợi ý *Capability Map* chuẩn trước khi viết PRD) với các luồng `WF-*`, máy trạng thái `SM-*` và quy tắc `TP-BR-*` mà **chưa tự ý khóa cứng phạm vi phát hành**:

| Mã Ứng viên | Mô-đun Năng lực (`Module ID`) | Tên Ứng viên Yêu cầu Nghiệp vụ | Truy vết `WF` / `OPS` | Truy vết `SM` | Quy tắc Ràng buộc (`TP-BR`) | Gợi ý Nhịp dự kiến (`PROPOSED` — Chưa khóa) |
|---|---|---|---|---|---|---|
| **CAND-001** | `core-tenancy-iam` | Khởi tạo an toàn máy chủ lần đầu & trình hướng dẫn Setup Wizard ngày đầu | `WF-01`, `WF-02`, `OPS-01` | — | `TP-BR-0301`, `0802` | `v0.1` |
| **CAND-002** | `core-tenancy-iam` | Quản lý cây phân cấp `Organization` → `Project` → `Environment` và cô lập chéo tổ chức | `WF-06`, `WF-13` | — | `TP-BR-0101`..`0103` | `v0.1` (cơ bản) / `v0.3` (đa tổ chức đầy đủ) |
| **CAND-003** | `core-tenancy-iam` | Hệ thống phân quyền theo vai trò (`RBAC`) và tùy chọn phê duyệt chéo hai người | `OPS-02`, `WF-04`, `WF-09` | `SM-06` | `TP-BR-0102`, `0602` | `v0.1` (cốt lõi) / `v0.3` (mở rộng) |
| **CAND-004** | `credential-access` | Vòng đời `Virtual Key` gắn đơn môi trường, xoay vòng êm hai khóa song hành và thu hồi tức thì | `WF-02`, `WF-10`, `WF-12` | `SM-05` | `TP-BR-0103`, `0803` | `v0.1` |
| **CAND-005** | `credential-access` | Quản lý `Secret Credential` một chiều (API Key, định danh dịch vụ) và tiêm bí mật an toàn không phơi bày thô | `WF-03`, `WF-05` | `SM-02` | `TP-BR-0102`, `0304`, `0402` | `v0.1` |
| **CAND-006** | `credential-access` | Kết nối `Authorized OAuth` & định danh liên kết hợp pháp kèm làm mới đơn luồng và cảnh báo hết hạn | `WF-05` | `SM-02`, `SM-03` | `TP-BR-0301`..`0304` | `v0.2` |
| **CAND-007** | `extension-governance` | Cài đặt phần mở rộng từ 4 nguồn (Marketplace, Git, gói nén, kho riêng) kèm phân định 3 nhãn `Trust Tier` | `WF-04`, `OPS-08` | `SM-01` | `TP-BR-0201`..`0203`, `0302` | `v0.1` (Git/URL/Gói) / `v0.2` (Marketplace/Kho riêng) |
| **CAND-008** | `extension-governance` | Cô lập quyền hạn phần mở rộng mặc định đóng, chặn vượt quyền mạng lúc chạy và cách ly khẩn cấp (`Quarantine`) | `WF-04`, `WF-12`, `OPS-04` | `SM-01` | `TP-BR-0401`, `0402`, `0404` | `v0.1` |
| **CAND-009** | `extension-governance` | Cập nhật phần mở rộng không gián đoạn, chặn tự động kích hoạt khi tăng quyền (`Permission Escalation`) và quay lui tức thì | `WF-11`, `OPS-09` | `SM-01`, `SM-08` | `TP-BR-0403`, `0801` | `v0.1` – `v0.2` |
| **CAND-010** | `model-gateway-routing` | Quản lý chuỗi 4 thực thể tách biệt: `Provider Adapter` → `Provider Connection` → `Model` → `Model Deployment` | `WF-03` | `SM-02`, `SM-04` | `TP-BR-0102`, `0104` | `v0.1` |
| **CAND-011** | `model-gateway-routing` | Giao diện suy luận hợp nhất song hành cùng đường đi giữ nguyên phương ngữ gốc, xử lý timeout, hủy luồng và chống chuyển mạch sau `Partial Stream` | `WF-06` | `SM-04` | `TP-BR-0503`..`0505` | `v0.1` |
| **CAND-012** | `model-gateway-routing` | Định tuyến nhận thức năng lực, chuyển mạch dự phòng trước luồng, ngắt mạch tự động và ép buộc cứng vùng cư trú dữ liệu (`Fail-Closed`) | `WF-07` | `SM-04` | `TP-BR-0501`, `0502`, `0504` | `v0.1` (cơ bản) / `v0.2` (nâng cao + giải thích khô) |
| **CAND-013** | `finops-budget-ledger` | Sổ cái ngân sách phân cấp, giữ chỗ hạn mức trước khi mở luồng (`Pre-Execution Hold`) và quyết toán minh bạch `EXACT` / `ESTIMATED` / `UNMETERED` | `WF-08`, `OPS-09` | `SM-07` | `TP-BR-0701`..`0703` | `v0.1` (đơn nút) / `v0.3` (cụm phân tán) |
| **CAND-014** | `mcp-governance-plane` | Đăng ký `MCP Server`, lọc danh mục công cụ theo quyền khóa ảo và khóa dấu vân tay `schema_hash` chống sửa ngầm | `WF-09` | — | `TP-BR-0104`, `0601` | `v0.2` |
| **CAND-015** | `mcp-governance-plane` | Quy trình phê duyệt con người gắn tham số bất biến cho công cụ `destructive` và cấm tự động thử lại công cụ có tác động phụ | `WF-09` | `SM-06` | `TP-BR-0602`, `0603` | `v0.2` |
| **CAND-016** | `a2a-connectivity-plane` | Đăng ký `Agent Endpoint`, khám phá hồ sơ tác nhân và điều phối ủy quyền tác vụ bất đồng bộ chống khuếch đại đặc quyền | `OPS-02` | `SM-06` | `TP-BR-0104`, `0604` | `v0.3` |
| **CAND-017** | `audit-ops-lifecycle` | Nhật ký kiểm toán bất biến, chính sách quyền riêng tư mặc định không lưu nội dung nhạy cảm, xóa sạch theo vòng đời và sao lưu/nâng cấp an toàn | `WF-12`, `WF-13`, `OPS-03`..`07` | `SM-08` | `TP-BR-0801`..`0803` | `v0.1` (cốt lõi) → `v1.0` (hoàn chỉnh) |

---

## 3. Bảng Tự Kiểm tra Đầy đủ các Tình huống Phủ định & Tuân thủ (Mandatory Self-Check Verification)

| Hạng mục bắt buộc tự kiểm tra theo đề bài A02 | Trạng thái phủ | Nơi quy định chi tiết & Hành vi nghiệp vụ đã khóa chặt |
|---|---|---|
| **1. Untrusted external provider / plugin** | **ĐÃ ĐẢM BẢO** | [`workflows.md`](./workflows.md) (`WF-04`, `WF-06`), [`business-rules.md`](./business-rules.md) (`TP-BR-0201`..`0203`, `0401`, `0402`, `0404`) — Gắn nhãn `unverified-external`, bắt buộc duyệt bảng quyền trước khi chạy, không đưa khóa gốc thô khi không cần thiết, chặn kết nối ngoài danh sách trắng và cách ly `QUARANTINED`. |
| **2. Plugin yêu cầu quyền mới khi update** | **ĐÃ ĐẢM BẢO** | [`workflows.md`](./workflows.md) (`WF-11`), [`business-entity-map.md`](./business-entity-map.md) (`SM-01`), [`business-rules.md`](./business-rules.md) (`TP-BR-0403`) — Chuyển sang `UPDATING_PENDING_RECONSENT`, chặn kích hoạt ngầm bản mới, giữ bản cũ phục vụ an toàn cho tới khi quyền chênh lệch được duyệt. |
| **3. Credentials hết hạn / bị thu hồi** | **ĐÃ ĐẢM BẢO** | [`workflows.md`](./workflows.md) (`WF-05`), [`business-entity-map.md`](./business-entity-map.md) (`SM-02`, `SM-03`), [`business-rules.md`](./business-rules.md) (`TP-BR-0304`) — Làm mới đơn luồng đại diện, chuyển sang `AUTH_EXPIRED_REAUTH_REQUIRED`, phát cảnh báo quản trị và không làm lộ chuỗi token hỏng ra lỗi của người gọi. |
| **4. Truy cập chéo tổ chức (`Cross-tenant`)** | **ĐÃ ĐẢM BẢO** | [`workflows.md`](./workflows.md) (`WF-06`), [`business-rules.md`](./business-rules.md) (`TP-BR-0101`, `0102`) — Từ chối cứng không lộ thông tin sự tồn tại của tài nguyên bên kia và sinh bản ghi kiểm toán bảo mật. |
| **5. Công cụ có tính phá hủy (`Tool destructive`)** | **ĐÃ ĐẢM BẢO** | [`workflows.md`](./workflows.md) (`WF-09`), [`business-entity-map.md`](./business-entity-map.md) (`SM-06`), [`business-rules.md`](./business-rules.md) (`TP-BR-0602`, `0603`) — Bắt buộc chờ phê duyệt con người khớp chính xác tham số đã băm; hết giờ tự hủy; cấm tự động thử lại khi quá thời gian chờ mạng. |
| **6. Nhà cung cấp ngừng hoạt động (`Provider unavailable`)** | **ĐÃ ĐẢM BẢO** | [`workflows.md`](./workflows.md) (`WF-07`), [`business-entity-map.md`](./business-entity-map.md) (`SM-04`), [`business-rules.md`](./business-rules.md) (`TP-BR-0501`, `0502`) — Chuyển mạch dự phòng trước luồng giữa các đích khỏe mạnh cùng chuẩn tuân thủ; nếu chỉ còn đích trái vùng cư trú dữ liệu thì bắt buộc `Fail-Closed` (`503`). |
| **7. Fallback sau khi đã phát luồng một phần (`Fallback after partial stream`)** | **ĐÃ ĐẢM BẢO** | [`workflows.md`](./workflows.md) (`WF-06`, `WF-07`), [`business-rules.md`](./business-rules.md) (`TP-BR-0503`) — Nghiêm cấm gọi nhà cung cấp dự phòng ghép nối tiếp sau khi đã phát ít nhất một mảnh nội dung; phải đóng luồng bằng khung báo lỗi tường minh. |
| **8. Phân bổ chi phí khi thiếu số đo chính xác (`Chargeback without exact usage`)** | **ĐÃ ĐẢM BẢO** | [`workflows.md`](./workflows.md) (`WF-08`), [`business-entity-map.md`](./business-entity-map.md) (`SM-07`), [`business-rules.md`](./business-rules.md) (`TP-BR-0703`) — Áp công thức ước lượng chuẩn, gắn nhãn `ESTIMATED` hoặc `UNMETERED_FLAGGED`, tách cột riêng trên báo cáo tài chính thay vì ghi `0 USD` giả tạo. |
| **9. Kiểm tra nghiệp vụ Subscription (Không hứa `Plus/Pro = API credits miễn phí`)** | **ĐÃ ĐẢM BẢO** | [`glossary.md`](../product/glossary.md), [`workflows.md`](./workflows.md) (`WF-02`, `WF-04`), [`business-rules.md`](./business-rules.md) (`Mục 1`, `TP-BR-0301`, `0302`) — Tách bạch rõ khả năng kỹ thuật và quyền sử dụng được nhà cung cấp cấp phép; cấm mọi cam kết hay cơ chế biến thuê bao cá nhân thành tín dụng API dùng chung. |

---

## 4. Các Điểm Mơ hồ Nghiệp vụ Còn Mở & Đề xuất Quyết định (`BA-OQ-001` .. `BA-OQ-005`)

Qua quá trình mô hình hóa chi tiết ở bước A02, dưới đây là **5 điểm mơ hồ cấp nghiệp vụ** cần Product Owner làm rõ cùng (hoặc ngay sau) Cổng G1 trước khi đóng băng PRD ở bước A03 (đã đồng bộ sang [`docs/governance/decisions.md`](../governance/decisions.md)):

1. **`BA-OQ-001` — Mô hình Đa Tổ chức (`Multi-Organization`) trên Cùng Một Máy chủ Tự vận hành:**
   - *Mơ hồ:* Một bản cài đặt Tanpopo cần hỗ trợ nhiều `Organization` độc lập hoàn toàn ngay từ giai đoạn đầu, hay mặc định 1 `Organization` duy nhất chứa nhiều `Project`/`Team` là đủ cho 95% khách hàng tự host?
   - *Đề xuất (`PROPOSED`):* Giữ thực thể `Organization` làm gốc trong mô hình dữ liệu để không phải sửa lại về sau, nhưng ở giai đoạn `0.x` tối ưu giao diện mặc định cho **1 Organization có nhiều Projects & Environments** (chỉ mở quản trị nhiều Organizations trên cùng 1 Instance cho kịch bản tập đoàn lớn ở `v0.3`/`v1.0`).
2. **`BA-OQ-002` — Chính sách Xử lý Khoản Chênh lệch Giữa Giữ chỗ (`RESERVED_HOLD`) và Quyết toán Thực tế Vượt Dự kiến:**
   - *Mơ hồ:* Nếu một yêu cầu không truyền tham số giới hạn độ dài đầu ra (`max_tokens`), hệ thống nên giữ chỗ bao nhiêu và nếu số thực tế cuối luồng hơi vượt mức giữ chỗ ban đầu (làm số dư âm nhẹ vài xu) thì xử lý thế nào?
   - *Đề xuất (`PROPOSED`):* Áp dụng mức giữ chỗ mặc định cấu hình được trên mỗi `Route`/`Environment` (hoặc theo ngưỡng an toàn của mô hình); nếu quyết toán cuối luồng vượt nhẹ làm âm số dư khả dụng, vẫn ghi nhận trung thực khoản vượt vào sổ cái (`Negative Balance Carryover`) và chặn ngay từ yêu cầu kế tiếp cho tới khi nạp/cấp thêm hạn mức.
3. **`BA-OQ-003` — Hành vi Mặc định Đối với Các Luồng Streaming Đang Mở Khi Thu hồi Khóa (`Revoke Key`):**
   - *Mơ hồ:* Khi quản trị viên thu hồi một `Virtual Key`, các luồng phản hồi đang truyền dở có bị cắt đứt ngang xương ngay lập tức hay được cho phép chạy nốt vài chục giây cuối?
   - *Đề xuất (`PROPOSED`):* Tách thành 2 chế độ rõ ràng trên nút thao tác: **Xoay vòng/Hết hạn chuẩn** cho phép các luồng đang mở hoàn tất êm đẹp trong giới hạn ngắn; còn **Thu hồi Khẩn cấp An ninh (`Emergency Security Revoke`)** cắt đứt ngay lập tức cả các luồng đang truyền dở để chặn rò rỉ dữ liệu tức thì.
4. **`BA-OQ-004` — Kênh Thông báo và Chờ Phê duyệt cho Công cụ MCP Có Tính Phá hủy (`Destructive Tool Approval UX`):**
   - *Mơ hồ:* Khi một `Agent` gọi công cụ MCP cần con người duyệt (`SM-06`), giao thức gọi của `MCP Client` bên ngoài có thể bị quá hạn chờ nếu con người mất 10 phút mới bấm duyệt trên Web Console.
   - *Đề xuất (`PROPOSED`):* Hỗ trợ hai chế độ theo cấu hình chính sách: (a) *Chờ đồng bộ ngắn hạn (`<= 60 giây`)* ngay trong phiên đối với tác nhân tương tác trực tiếp cùng người dùng, và (b) *Từ chối ngay kèm cấp Mã Yêu cầu Phê duyệt trước (`Pre-Approval Token`)* để sau khi `Reviewer` duyệt xong trên Web Console/Webhook, tác nhân gọi lại lần hai sẽ được thông qua ngay trong cửa sổ hiệu lực ngắn.
5. **`BA-OQ-005` — Thời hạn Cam kết Hỗ trợ Ngược Hợp đồng Phần mở rộng (`Plugin Contract Deprecation Window`):**
   - *Mơ hồ:* Khi Tanpopo phát hành phiên bản hợp đồng phần mở rộng mới (từ mốc `v1.0` trở đi), các plugin cũ được đảm bảo chạy tiếp trong bao lâu?
   - *Đề xuất (`PROPOSED`):* Trước mốc `v1.0` (`0.x`), duy trì tương thích tối thiểu 1 phiên bản phụ liền trước (`N-1 minor`); kể từ `v1.0` trở đi, cam kết giữ tương thích ngược tối thiểu **12 tháng (hoặc toàn bộ nhánh `1.x`)** trước khi ngưng hỗ trợ bất kỳ trường hợp đồng nào.
