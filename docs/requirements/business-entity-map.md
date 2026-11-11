# Mô hình Tác nhân, Bản đồ Thực thể Nghiệp vụ & Máy Trạng thái (Actors, Business Entity Map & State Machines) — Tanpopo

> **Tài liệu Phân tích Nghiệp vụ (Phase ASTRA A02 — `DRAFT / UNLOCKED SCOPE`)**
> - **Phiên bản:** `0.1.0-a02-draft`
> - **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Lead Business Analyst & Product Architect
> - **Trạng thái quản trị:** **Bản nháp chưa khóa phạm vi (`DRAFT`)** — chờ Product Owner phê duyệt Cổng **Gate G1** và **Gate G2** tại [`docs/governance/decisions.md`](../governance/decisions.md). Không chứa giả định khóa cứng ngôn ngữ lập trình hay cơ sở dữ liệu.

---

## 1. Danh mục Đầy đủ 12 Tác nhân Nghiệp vụ (Actors Matrix)

Mỗi tác nhân dưới đây có phạm vi thẩm quyền, mục tiêu vận hành và ranh giới tin cậy độc lập:

| Mã | Tác nhân (Actor) | Bản chất & Cấp độ tin cậy | Quyền hạn cốt lõi (Allowed) | Ranh giới bị cấm (Prohibited) |
|---|---|---|---|---|
| **ACT-01** | **Personal Admin** | Con người — Tin cậy tối đa trên máy chủ cá nhân (`Single-Tenant Mode`) | Thực hiện trọn vẹn vai trò của `Operator`, `Organization Owner` và `Team Admin` trên bản cài đặt cá nhân của mình. | Không áp dụng trong chế độ tổ chức nhiều phòng ban nếu chính sách yêu cầu phân tách nhiệm vụ (Segregation of Duties). |
| **ACT-02** | **Organization Owner** | Con người — Thẩm quyền quản trị cao nhất trong một `Organization` | Thiết lập cấu trúc `Project`/`Environment`, ngân sách tổng, chính sách vùng dữ liệu, ngưỡng tin cậy Plugin, quy tắc lưu giữ/xóa dữ liệu và nút ngắt khẩn cấp cấp tổ chức. | Không xem được dữ liệu của `Organization` khác trên cùng `Instance`; không sửa đổi hạ tầng máy chủ gốc của `Operator`. |
| **ACT-03** | **Team Admin** | Con người — Thẩm quyền quản trị trong phạm vi một `Project` được giao | Quản lý thành viên nhóm, tạo/xoay vòng/thu hồi `Virtual Key`, tạo `Route`, chia nhỏ ngân sách nhóm, gắn `Provider Connection` của riêng nhóm (nếu tổ chức cho phép). | Không được tự ý cài đặt `Extension` mới lên hệ thống hoặc nâng trần ngân sách vượt quá hạn mức do `Organization Owner` cấp. |
| **ACT-04** | **Developer** | Con người — Người tiêu dùng nội bộ trong `Project` | Sử dụng khóa ảo được cấp, thử nghiệm trên Playground, xem danh mục `Model`/`MCP Tool` được phép và tra cứu log/trace trong `Environment` được phân quyền. | Không được đọc giá trị khóa gốc (`Credential`), không sửa chính sách an toàn hay phê duyệt quyền của chính mình. |
| **ACT-05** | **API Client** | Phần mềm tự động — Không tương tác giao diện người dùng | Gửi yêu cầu suy luận mô hình (Unified API hoặc phương ngữ gốc) kèm `Virtual Key` hợp lệ; nhận luồng phản hồi và mã lỗi chuẩn hóa. | Không được gọi API quản trị Control Plane; không được biết khóa hay cấu hình tài khoản nhà cung cấp phía sau. |
| **ACT-06** | **Extension Publisher** | Con người / Tổ chức phát triển bên trong hoặc bên ngoài — **Mặc định không tin cậy tuyệt đối** cho tới khi qua kiểm định | Đóng gói `Extension Version` kèm `Manifest` khai báo quyền, bộ kiểm thử hợp đồng và chữ ký nguồn gốc; phát hành lên Marketplace, Git hoặc kho riêng. | Không được yêu cầu quyền ẩn ngoài `Manifest`; bên thứ ba không được mạo danh nhãn `official` của Tanpopo. |
| **ACT-07** | **Reviewer** | Con người thẩm định độc lập (Cấp Marketplace công khai hoặc Cấp Bảo mật nội bộ) | *(Cấp Marketplace)* Duyệt/từ chối/thu hồi nhãn `verified`. *(Cấp Tổ chức)* Phê duyệt hoặc từ chối cài đặt/nâng cấp plugin tăng quyền và lệnh gọi công cụ `destructive`. | Không được tự phê duyệt yêu cầu nhạy cảm do chính mình khởi tạo nếu tổ chức bật chế độ kiểm soát hai người (Four-Eyes Principle). |
| **ACT-08** | **Operator** | Con người — Quản trị hạ tầng hệ thống (`Instance / SRE`) | Cài đặt ban đầu, nâng cấp/hạ cấp nhân Tanpopo, cấu hình kho bí mật chủ, sao lưu/phục hồi, giám sát sức khỏe hạ tầng và cách ly khẩn cấp plugin cấp hệ thống. | Không được bỏ qua nhật ký kiểm toán (`Audit Event`) khi can thiệp khẩn cấp vào trạng thái khóa hoặc kết nối. |
| **ACT-09** | **MCP Client** | Phần mềm (IDE, ứng dụng tác nhân) kết nối vào mặt phẳng MCP | Khám phá danh sách công cụ/tài nguyên đã lọc theo quyền và gửi yêu cầu thực thi công cụ kèm ngữ cảnh định danh. | Không được nhìn thấy hoặc gọi trực tiếp các công cụ nằm ngoài phạm vi cấp phép của `Virtual Key` / `Principal`. |
| **ACT-10** | **MCP Server** | Hệ thống công cụ đích bên dưới (nội bộ hoặc bên thứ ba) — **Bán tin cậy** | Công bố danh mục `Tools`/`Resources`/`Prompts` và thực thi lệnh gọi được Tanpopo chuyển tiếp hợp lệ. | Không được tự ý thay đổi mô tả/cấu trúc công cụ đã ghim (`schema_hash`) mà không bị cổng phát hiện và chặn kiểm duyệt lại. |
| **ACT-11** | **Agent** | Tác nhân tự chủ (nội bộ hoặc đối tác từ xa qua mặt phẳng A2A) | Thực hiện chuỗi tác vụ kết hợp mô hình + MCP hoặc gửi/nhận ủy quyền tác vụ bất đồng bộ (`Agent Task`) trong giới hạn ngân sách và thời gian sống được cấp. | Không được tự nới rộng quyền hạn ủy nhiệm (privilege amplification) vượt quá quyền của chủ thể gốc đã kích hoạt tác nhân. |
| **ACT-12** | **External Identity / AI Provider** | Dịch vụ xác thực (IdP/OAuth) và Nhà cung cấp mô hình bên ngoài — Ranh giới pháp nhân độc lập | *(IdP)* Xác thực danh tính và phát hành token ủy quyền hợp lệ. *(AI Provider)* Xử lý yêu cầu suy luận theo biểu giá, hạn mức và điều khoản thương mại riêng. | Không thuộc quyền kiểm soát vận hành của Tanpopo; sự cố hoặc thay đổi điều khoản từ phía họ phải được Tanpopo cô lập an toàn. |

---

## 2. Bản đồ Thực thể Nghiệp vụ & Quyền Sở hữu (Business Entity Map & Ownership)

### 2.1. Sơ đồ Quan hệ Thực thể Cốt lõi (Domain ER Diagram)

```mermaid
erDiagram
    INSTANCE ||--o{ ORGANIZATION : "hosts"
    INSTANCE ||--o{ EXTENSION_INSTALLATION : "activates"
    EXTENSION_PACKAGE ||--|{ EXTENSION_VERSION : "versions"
    EXTENSION_VERSION ||--o{ EXTENSION_INSTALLATION : "installed_as"
    EXTENSION_INSTALLATION ||--o| PROVIDER_ADAPTER : "exposes_adapter"

    ORGANIZATION ||--|{ PROJECT : "contains"
    ORGANIZATION ||--o{ PRINCIPAL : "manages"
    ORGANIZATION ||--o{ SECRET_CREDENTIAL : "owns_org_level"
    ORGANIZATION ||--o{ PROVIDER_CONNECTION : "owns_shared_or_dedicated"
    ORGANIZATION ||--|{ BUDGET_LEDGER : "governs_spend"
    ORGANIZATION ||--o{ POLICY : "enforces"

    PROJECT ||--|{ ENVIRONMENT : "segments"
    PROJECT ||--o{ VIRTUAL_KEY : "issues"
    PROJECT ||--o{ ROUTE : "defines"
    PROJECT ||--o{ MCP_SERVER_REGISTRATION : "connects"
    PROJECT ||--o{ AGENT_ENDPOINT : "registers_a2a"

    PROVIDER_ADAPTER ||--o{ PROVIDER_CONNECTION : "drives"
    SECRET_CREDENTIAL ||--o{ PROVIDER_CONNECTION : "authenticates"
    PROVIDER_CONNECTION ||--|{ MODEL_DEPLOYMENT : "serves"
    LOGICAL_MODEL ||--o{ MODEL_DEPLOYMENT : "implemented_by"
    ROUTE ||--|{ MODEL_DEPLOYMENT : "targets_ordered"

    MCP_SERVER_REGISTRATION ||--|{ MCP_TOOL : "exposes"
    VIRTUAL_KEY ||--o{ BUDGET_RESERVATION : "initiates"
    BUDGET_RESERVATION ||--|| USAGE_SETTLEMENT : "settles_into"
    PRINCIPAL ||--o{ APPROVAL_REQUEST : "decides"
    ORGANIZATION ||--o{ AUDIT_EVENT : "records_immutable"
```

### 2.2. Phân biệt Rạch ròi 4 Thực thể trong Chuỗi Mô hình (Tránh Nhầm lẫn Nghiệp vụ)

| Thực thể | Câu hỏi nghiệp vụ trả lời | Ví dụ thực tế | Chủ sở hữu & Vòng đời |
|---|---|---|---|
| **1. `Provider Adapter`** | *"Làm thế nào để nói chuyện đúng giao thức với dòng nhà cung cấp này?"* | Gói `anthropic-official@1.4.0` hoặc `corp-internal-llm@0.2.1` | Thuộc danh mục phần mở rộng cấp `Instance`/`Organization`. Không chứa khóa bí mật và không gắn với riêng dự án nào. |
| **2. `Provider Connection`** | *"Chúng ta chứng thực bằng tài khoản/khóa nào và đi tới địa chỉ dịch vụ nào?"* | Kết nối `"Anthropic Thương mại Prod - Khối Sản phẩm"` gắn với `API Key #A1` | Thuộc `Organization` (có thể chia sẻ có kiểm soát cho một số `Project`) hoặc thuộc riêng một `Project`. |
| **3. `Model`** *(Logical Model)* | *"Mô hình này có năng lực gì về mặt bản chất?"* | Mô hình logic `claude-sonnet-4-5` (hỗ trợ `200k context`, `vision`, `tool_calling`, `thinking`) | Danh mục chuẩn hóa từ `Provider Adapter` kết hợp khai báo quản trị; độc lập với tài khoản thanh toán. |
| **4. `Model Deployment`** | *"Đích vận hành cụ thể nào sẽ nhận lưu lượng ngay lúc này, ở vùng địa lý nào, giá bao nhiêu và đang sống hay chết?"* | `Deployment #1`: `claude-sonnet-4-5` qua kết nối `Bedrock Frankfurt (EU)` vs `Deployment #2`: qua `Anthropic Direct (US)` | Gắn 1-1 với một `Provider Connection`. **Mọi quyết định định tuyến, ngắt mạch (circuit breaker), vùng dữ liệu và tính giá đều thực thi trên `Model Deployment`.** |

### 2.3. Ma trận Quyền Sở hữu, Chia sẻ & Cô lập Dữ liệu (Ownership & Sharing Matrix)

| Thực thể nghiệp vụ | Cấp sở hữu gốc (Primary Owner) | Phạm vi chia sẻ hợp lệ | Hành vi khi xóa chủ sở hữu |
|---|---|---|---|
| `Organization (Tenant)` | `Organization Owner` | **Tuyệt đối không chia sẻ** bất kỳ tài nguyên sống nào sang `Organization` khác. | Chuyển sang trạng thái chờ đóng băng → thu hồi toàn bộ khóa/kết nối → xóa dữ liệu nghiệp vụ theo chính sách lưu giữ. |
| `Secret Credential` | `Organization` hoặc `Project` | Khóa cấp `Organization` chỉ cho phép `Project` *sử dụng gián tiếp* qua `Provider Connection` được cấp quyền, không bao giờ phơi bày chuỗi bí mật thô. | Cấm xóa cứng nếu đang gắn với `Provider Connection` đang hoạt động (phải gỡ liên kết hoặc thu hồi kết nối trước). |
| `Provider Connection` & `Model Deployment` | `Organization` (dùng chung có chọn lọc) hoặc `Project` (dùng riêng) | Có thể cấp quyền tiêu dùng xuống từng `Project` / `Environment` thông qua `Policy` và `Route`. | Ngừng nhận lưu lượng mới (`Draining`), giữ lại định danh lịch sử tối thiểu trong sổ cái chi phí để không làm hỏng báo cáo đối soát. |
| `Virtual Key` | `Environment` thuộc một `Project` | Chỉ thuộc duy nhất 1 `Environment` của 1 `Project` (không dùng chung một khóa ảo cho cả `dev` và `prod`). | Thu hồi tức thì hiệu lực xác thực; giữ bản ghi siêu dữ liệu khóa đã băm để tra cứu nhật ký kiểm toán. |
| `Extension Installation` | `Instance` (với chính sách bật/tắt theo `Organization`) | Gói cài đặt được kiểm soát bởi `Operator`; từng `Organization` chỉ được kích hoạt các plugin phù hợp chính sách tin cậy của mình. | Gỡ cài đặt chỉ thành công khi không còn `Provider Connection` hay `Policy` đang hoạt động nào phụ thuộc vào nó (hoặc phải cưỡng bức `Quarantine`). |
| `MCP Server Registration` & `MCP Tool` | `Organization` hoặc `Project` | Lọc quyền truy cập đến từng tên công cụ cụ thể (`Tool-level Scoping`) cho từng `Virtual Key` / vai trò. | Thu hồi phiên kết nối công cụ và hủy các yêu cầu chờ phê duyệt đang treo. |
| `Budget Ledger` & `Audit Event` | `Organization` (phân bổ hạn mức con cho `Project` / `Virtual Key`) | Chỉ đọc theo phân cấp quản trị (`Team Admin` chỉ thấy sổ cái của `Project` mình; `Org Owner` thấy toàn tổ chức). | **Bất biến (Append-only/Immutable)** trong suốt thời hạn lưu giữ pháp lý/tài chính ngay cả khi khóa ảo hay dự án con đã ngừng hoạt động. |

---

## 3. Tám Máy Trạng thái & Chuyển tiếp Nghiệp vụ (State Machines & Transitions)

### SM-01: Vòng đời Phần mở rộng (`Plugin / Extension Lifecycle`)

```mermaid
stateDiagram-v2
    [*] --> DISCOVERED
    DISCOVERED --> VERIFYING : "Chọn cài đặt (Marketplace/Git/ZIP/Registry)"
    VERIFYING --> REJECTED_INVALID : "Sai hợp đồng / hỏng chữ ký / vi phạm quy tắc nguồn"
    VERIFYING --> PENDING_PERMISSION_APPROVAL : "Hợp lệ kỹ thuật -> Trình bảng quyền & nhãn Trust Tier"
    PENDING_PERMISSION_APPROVAL --> INSTALLED_DISABLED : "Admin/Reviewer phê duyệt quyền"
    PENDING_PERMISSION_APPROVAL --> CANCELLED : "Từ chối cấp quyền"
    INSTALLED_DISABLED --> ENABLED_ACTIVE : "Kích hoạt cho lưu lượng"
    ENABLED_ACTIVE --> DRAINING : "Tạm dừng / Chuẩn bị nâng cấp hoặc gỡ bỏ"
    ENABLED_ACTIVE --> UPDATING_PENDING_RECONSENT : "Bản cập nhật mới đòi thêm quyền (Permission Escalation)"
    UPDATING_PENDING_RECONSENT --> DRAINING : "Phê duyệt quyền mới -> Chuyển phiên bản êm"
    UPDATING_PENDING_RECONSENT --> ENABLED_ACTIVE : "Từ chối quyền mới -> Giữ nguyên bản cũ đang chạy"
    DRAINING --> INSTALLED_DISABLED : "Các luồng đang chạy hoàn tất"
    DRAINING --> ENABLED_ACTIVE : "Kích hoạt bản mới (hoặc Rollback về bản ghim cũ)"
    ENABLED_ACTIVE --> QUARANTINED : "Sự cố bảo mật / Vượt quyền mạng / Thu hồi khẩn cấp"
    INSTALLED_DISABLED --> QUARANTINED : "Nhận cảnh báo độc hại"
    QUARANTINED --> INSTALLED_DISABLED : "Chỉ khi Operator/Owner gỡ cách ly có kiểm toán"
    INSTALLED_DISABLED --> UNINSTALLED : "Gỡ bỏ (khi không còn Connection phụ thuộc)"
    QUARANTINED --> UNINSTALLED : "Xóa bỏ gói độc hại sau khi giữ bằng chứng audit"
```

- **Điểm chặn nghiệp vụ cốt lõi:** Mọi bản cập nhật phát sinh quyền mới (`Permission Escalation`) **bắt buộc** dừng ở `UPDATING_PENDING_RECONSENT`, tuyệt đối không tự động nâng cấp ngầm. Trạng thái `QUARANTINED` cắt lập tức mọi luồng mới và quyền mạng mà không xóa nhật ký điều tra.

---

### SM-02: Vòng đời Kết nối Nhà cung cấp (`Provider Connection Lifecycle`)

```mermaid
stateDiagram-v2
    [*] --> DRAFT
    DRAFT --> VALIDATING : "Gắn Credential & kiểm tra kết nối thử"
    VALIDATING --> ACTIVE : "Xác thực hợp lệ & tuân thủ chính sách"
    VALIDATING --> DRAFT : "Sai thông tin cấu hình / mạng lỗi"
    ACTIVE --> DEGRADED : "Tỷ lệ lỗi hoặc độ trễ vượt ngưỡng cảnh báo"
    DEGRADED --> ACTIVE : "Sức khỏe phục hồi ổn định"
    ACTIVE --> AUTH_EXPIRED_REAUTH_REQUIRED : "Khóa hết hạn / OAuth bị thu hồi (401/403 xác thực)"
    DEGRADED --> AUTH_EXPIRED_REAUTH_REQUIRED : "Làm mới xác thực thất bại"
    AUTH_EXPIRED_REAUTH_REQUIRED --> VALIDATING : "Cập nhật khóa mới / Cấp quyền lại thành công"
    ACTIVE --> SUSPENDED_BY_ADMIN : "Tạm ngưng chủ động"
    SUSPENDED_BY_ADMIN --> VALIDATING : "Mở lại kết nối"
    ACTIVE --> REVOKED : "Thu hồi vĩnh viễn kết nối"
    AUTH_EXPIRED_REAUTH_REQUIRED --> REVOKED : "Hủy bỏ kết nối"
    SUSPENDED_BY_ADMIN --> REVOKED : "Hủy bỏ kết nối"
    REVOKED --> ARCHIVED : "Lưu trữ lịch sử đối soát chi phí"
```

---

### SM-03: Vòng đời OAuth Được Cấp phép & Tái Ủy quyền (`Authorized OAuth Token Lifecycle`)

```mermaid
stateDiagram-v2
    [*] --> UNBOUND
    UNBOUND --> AUTHORIZING : "Khởi tạo luồng cấp quyền chính thức (PKCE / Client Credentials)"
    AUTHORIZING --> TOKEN_VALID : "Nhận token hợp lệ & khớp phạm vi (scopes) được phép"
    AUTHORIZING --> UNBOUND : "Người dùng hủy / Sai phạm vi cấp phép"
    TOKEN_VALID --> REFRESHING_SINGLEFLIGHT : "Chạm ngưỡng sắp hết hạn (chỉ 1 luồng đại diện làm mới)"
    REFRESHING_SINGLEFLIGHT --> TOKEN_VALID : "Cấp token mới thành công"
    REFRESHING_SINGLEFLIGHT --> REFRESH_GRACE_DEGRADED : "Lỗi mạng tạm thời phía IdP (token hiện tại còn hạn ngắn)"
    REFRESH_GRACE_DEGRADED --> REFRESHING_SINGLEFLIGHT : "Thử lại theo lịch lùi (backoff)"
    REFRESHING_SINGLEFLIGHT --> REAUTH_REQUIRED : "Lỗi invalid_grant / Token bị thu hồi / Đổi mật khẩu"
    REFRESH_GRACE_DEGRADED --> REAUTH_REQUIRED : "Token chính thức hết hạn hoàn toàn"
    REAUTH_REQUIRED --> AUTHORIZING : "Quản trị viên thực hiện cấp quyền lại"
    TOKEN_VALID --> REVOKED : "Thu hồi chủ động từ Tanpopo"
    REAUTH_REQUIRED --> REVOKED : "Xóa liên kết xác thực"
```

---

### SM-04: Trạng thái Sức khỏe Đích Mô hình (`Model Deployment Health Lifecycle`)

```mermaid
stateDiagram-v2
    [*] --> PROVISIONED
    PROVISIONED --> HEALTHY : "Kiểm tra sẵn sàng đạt chuẩn"
    HEALTHY --> THROTTLED_COOLDOWN : "Nhận 429 Rate Limit / Retry-After từ nhà cung cấp"
    THROTTLED_COOLDOWN --> HEALTHY : "Hết thời gian chờ Retry-After"
    HEALTHY --> CIRCUIT_OPEN_UNHEALTHY : "Lỗi 5xx / Timeout liên tiếp vượt ngưỡng ngắt mạch"
    THROTTLED_COOLDOWN --> CIRCUIT_OPEN_UNHEALTHY : "Thất bại kéo dài"
    CIRCUIT_OPEN_UNHEALTHY --> HALF_OPEN_PROBING : "Hết chu kỳ cách ly -> Cho 1 lượng nhỏ request thăm dò"
    HALF_OPEN_PROBING --> HEALTHY : "Request thăm dò thành công liên tiếp"
    HALF_OPEN_PROBING --> CIRCUIT_OPEN_UNHEALTHY : "Request thăm dò tiếp tục lỗi"
    HEALTHY --> DRAINING_MAINTENANCE : "Bảo trì chủ động"
    DRAINING_MAINTENANCE --> DISABLED : "Hoàn tất rút lưu lượng"
    DISABLED --> PROVISIONED : "Bật lại Deployment"
```

---

### SM-05: Vòng đời Khóa Ảo (`Virtual Key Lifecycle`)

```mermaid
stateDiagram-v2
    [*] --> CREATED_ACTIVE
    CREATED_ACTIVE --> ROTATING_DUAL_VALID : "Kích hoạt xoay vòng khóa có ân hạn (cấp khóa mới, khóa cũ chờ hết hạn ngắn)"
    ROTATING_DUAL_VALID --> CREATED_ACTIVE : "Khóa cũ hết cửa sổ ân hạn -> Chỉ khóa mới còn hiệu lực"
    CREATED_ACTIVE --> QUOTA_PAUSED : "Chạm trần ngân sách kỳ hạn (tự mở lại khi sang kỳ mới hoặc tăng trần)"
    QUOTA_PAUSED --> CREATED_ACTIVE : "Đầu kỳ ngân sách mới / Quản trị viên cấp thêm hạn mức"
    CREATED_ACTIVE --> SUSPENDED : "Tạm khóa điều tra bất thường"
    SUSPENDED --> CREATED_ACTIVE : "Xác nhận an toàn, mở lại"
    CREATED_ACTIVE --> EXPIRED : "Chạm thời điểm hết hạn định trước (TTL)"
    CREATED_ACTIVE --> REVOKED : "Thu hồi khẩn cấp (mất hiệu lực ngay lập tức)"
    ROTATING_DUAL_VALID --> REVOKED : "Thu hồi khẩn cấp cả cặp khóa"
    SUSPENDED --> REVOKED : "Chốt hủy vĩnh viễn"
    EXPIRED --> PURGED_METADATA_RETAINED : "Xóa vật liệu xác thực, giữ mã định danh kiểm toán"
    REVOKED --> PURGED_METADATA_RETAINED : "Xóa vật liệu xác thực, giữ mã định danh kiểm toán"
```

---

### SM-06: Vòng đời Phê duyệt & Tác vụ Bất đồng bộ (`Approval & Task Lifecycle`)

```mermaid
stateDiagram-v2
    [*] --> CREATED
    CREATED --> EXECUTING : "Hành động nằm trong danh sách tự động cho phép"
    CREATED --> AWAITING_APPROVAL : "Chạm quy tắc bắt buộc phê duyệt (VD: MCP Tool destructive)"
    AWAITING_APPROVAL --> APPROVED : "Reviewer có thẩm quyền chấp thuận trong thời hạn"
    AWAITING_APPROVAL --> REJECTED : "Reviewer từ chối"
    AWAITING_APPROVAL --> APPROVAL_EXPIRED : "Quá thời gian chờ phê duyệt (TTL) -> Tự động hủy an toàn"
    AWAITING_APPROVAL --> CANCELLED : "Người gọi chủ động hủy yêu cầu"
    APPROVED --> EXECUTING : "Giải phóng lệnh thực thi đúng với tham số đã khóa băm"
    EXECUTING --> COMPLETED : "Thực thi thành công & ghi biên nhận"
    EXECUTING --> CANCELLED : "Hủy giữa chừng theo tín hiệu người gọi"
    EXECUTING --> FAILED : "Lỗi thực thi (không tự động gọi lặp lại nếu có tác động phụ)"
```

---

### SM-07: Vòng đời Tạm giữ & Quyết toán Ngân sách (`Budget Reservation & Settlement Lifecycle`)

```mermaid
stateDiagram-v2
    [*] --> CHECKED
    CHECKED --> REJECTED_INSUFFICIENT_BUDGET : "Số dư khả dụng (trừ các khoản đang giữ chỗ) không đủ"
    CHECKED --> RESERVED_HOLD : "Khóa tạm khoản chi ước tính tối đa của yêu cầu"
    RESERVED_HOLD --> RELEASED_UNUSED : "Yêu cầu bị từ chối ở bước Policy / Lỗi trước khi gửi đi"
    RESERVED_HOLD --> STREAMING_ACCRUING : "Đã mở kết nối tới nhà cung cấp đích"
    STREAMING_ACCRUING --> SETTLED_EXACT : "Hoàn tất có đầy đủ số đo tiêu thụ chính xác từ nhà cung cấp"
    STREAMING_ACCRUING --> SETTLED_ESTIMATED : "Đứt luồng giữa chừng / Nhà cung cấp thiếu số đo -> Áp công thức ước tính chuẩn"
    STREAMING_ACCRUING --> SETTLED_UNMETERED_FLAGGED : "Loại tác vụ không thể ước tính tự động -> Ghi cờ chờ đối soát"
    RESERVED_HOLD --> EXPIRED_RECLAIMED : "Sự cố sập nút mạng làm treo khoản giữ chỗ quá hạn -> Thu hồi có kiểm toán"
```

---

### SM-08: Vòng đời Phát hành & Nâng cấp Hệ thống (`Release & Upgrade Lifecycle`)

```mermaid
stateDiagram-v2
    [*] --> PREFLIGHT_COMPAT_CHECK
    PREFLIGHT_COMPAT_CHECK --> BLOCKED_INCOMPATIBLE : "Phát hiện Plugin đang chạy lệch phiên bản Hợp đồng không hỗ trợ"
    PREFLIGHT_COMPAT_CHECK --> BACKUP_SNAPSHOT : "Tương thích hợp lệ -> Chụp ảnh cấu hình & sổ cái an toàn"
    BACKUP_SNAPSHOT --> UPGRADING_CONTROL_PLANE : "Cập nhật cấu trúc quản trị tương thích ngược"
    UPGRADING_CONTROL_PLANE --> ROLLING_DATA_PLANE : "Chuyển lưu lượng mới sang tiến trình mới, rút êm luồng cũ"
    ROLLING_DATA_PLANE --> VERIFYING_HEALTH : "Chạy kiểm tra tự chẩn đoán đầu cuối"
    VERIFYING_HEALTH --> COMMITTED : "Mọi chỉ số sức khỏe & kết nối ổn định"
    UPGRADING_CONTROL_PLANE --> ROLLING_BACK : "Lỗi di trú cấu hình"
    ROLLING_DATA_PLANE --> ROLLING_BACK : "Tỷ lệ lỗi tăng vọt"
    VERIFYING_HEALTH --> ROLLING_BACK : "Kiểm tra sức khỏe thất bại"
    ROLLING_BACK --> ROLLED_BACK_SAFE : "Khôi phục bản chạy ổn định liền trước & báo cáo nguyên nhân"
```
