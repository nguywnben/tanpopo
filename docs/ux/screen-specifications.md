# Danh mục Màn hình, Đặc tả Hợp đồng Giao diện & Rào chắn UX (Screen Inventory, Contracts & UX Guardrails) — Tanpopo

> **Tài liệu Đặc tả Màn hình Chi tiết (Phase ASTRA A06 — `DRAFT / READY_FOR_REVIEW`)**
> - **Mã tài liệu:** `SPEC-UX-03` | **Phiên bản:** `0.1.0-step06-draft` | **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Lead Product Designer & Security UX Architect
> - **Trạng thái quản trị:** **Đặc tả Thiết kế Đề xuất (`PROPOSED — NO CODE`)** — liên kết chặt chẽ với [`information-architecture.md`](./information-architecture.md), [`api-contracts.md`](../specifications/api-contracts.md) (`SPEC-API-01`), [`data-model.md`](../architecture/data-model.md) và [`accessibility-checklist.md`](../../.agents/references/accessibility-checklist.md).
> - **Truy vết Yêu cầu:** `TP-FR-0201`..`0904`, `TP-NFR-0402`, `TP-NFR-0404`, `TP-NFR-0701`, `TP-NFR-0702`, `TP-NFR-1001`, `TP-NFR-1002`, `DEC-004`.

---

## 1. Sáu Rào chắn Trải nghiệm Bắt buộc (`Mandatory UX Guardrails`: `UX-GR-01` .. `UX-GR-06`)

Mọi màn hình trong danh mục dưới đây đều phải tuân thủ 6 rào chắn bảo mật, pháp lý và trung thực kỹ thuật không có ngoại lệ:

| Mã Rào chắn | Nguyên tắc Cốt lõi | Hành vi Giao diện Bắt buộc & Điều bị Nghiêm cấm |
|---|---|---|
| **`UX-GR-01`**<br/>**Bí mật Ghi Một Chiều & Khóa Ảo Hiện Đúng Một Lần** | Không bao giờ hiển thị lại khóa bí mật nhạy cảm sau khi lưu. | - Mọi trường nhập API Key, Client Secret, Webhook Secret dùng kiểu mật khẩu một chiều; sau khi lưu chỉ hiển thị dấu vân tay cuối (ví dụ `sk-...9a2f`) và ngày tạo — **cấm tuyệt đối nút "Show / Reveal Secret"** gọi lấy lại bản rõ từ máy chủ.<br/>- Khi phát hành `Virtual Key` (`vk_tp_...`), hộp thoại `Dialog` chỉ hiện bản rõ **duy nhất 1 lần ngay lúc tạo** kèm cảnh báo *"Sao chép ngay; Tanpopo chỉ lưu mã băm SHA-256 và không thể hiển thị lại khóa này lần thứ hai."* |
| **`UX-GR-02`**<br/>**Thừa nhận Trung thực Tính năng Không Hỗ trợ** | Không lặng lẽ cắt bỏ tham số hay hứa hẹn tương thích `100%`. | - Trong `Model Catalog`, `Route Builder` và `Playground`, mỗi tính năng (`strict_json_schema`, `audio_input`, `reasoning`, `prompt_caching`) phải gắn thẻ cấp độ bảo chứng thực tế (`VERIFIED` / `TESTED` / `DECLARED` / `UNSUPPORTED`).<br/>- Nếu tính năng chỉ chạy nguyên vẹn qua đường giữ nguyên phương ngữ gốc (`/native/{dialect}/...`), giao diện phải ghi chú rõ ràng và cung cấp nút chuyển đổi sang đoạn mã `Native Passthrough` thay vì làm rơi trường dữ liệu ngầm. |
| **`UX-GR-03`**<br/>**Cấm Gắn Nhãn "Chính thức Hỗ trợ" Cho Kết nối Thuê bao Tiêu dùng** | Bảo toàn ranh giới pháp lý `DEC-004` & `ADR-0005`. | - Tuyệt đối **không bao giờ** gắn nhãn `Official`, `Verified`, `Supported` hay viết hướng dẫn biến các gói thuê bao cá nhân (`ChatGPT Plus/Pro`, `Codex`, `Claude Pro/Max`, `Google Antigravity`...) thành cổng API dùng chung.<br/>- Biểu mẫu `Provider Connection` chỉ có 2 chế độ thanh toán minh bạch: `Commercial API Billing` và `Self-Hosted Infrastructure`; không chấp nhận dán cookie trình duyệt hay token phiên cá nhân không được hãng cấp phép. |
| **`UX-GR-04`**<br/>**Không Giấu Rủi ro Cộng đồng Sau Một Nút Cài đặt** | Minh bạch quyền hạn, tác giả và nguồn gốc trước mọi thao tác. | - Trên danh sách `Extension Marketplace` và trình cài gói ngoài, nút hành động đầu tiên luôn là **"Inspect Permissions & Trust..."** (không bao giờ là nút cài đặt kích hoạt ngầm một chạm).<br/>- Hộp kiểm tra bắt buộc hiển thị `Publisher Identity`, `Origin`, `Trust Tier` (`Unverified External` có biểu tượng cảnh báo riêng), `Allowed Hosts` và `Secret Slots` trước khi nút xác nhận kích hoạt được mở khóa. |
| **`UX-GR-05`**<br/>**Cảnh báo Vận hành Chủ động Đúng Thời điểm** | Báo trước rủi ro mất dữ liệu, kịch trần quy mô hoặc lệch chi phí. | Thanh `Banner` trên `AppShell` hoặc thẻ cảnh báo ngữ cảnh tự động bật khi phát hiện:<br/>1. Chưa xác nhận sao lưu khóa chủ `TANPOPO_MASTER_KEY` ở môi trường sản xuất;<br/>2. Chế độ đơn nút SQLite vượt ngưỡng khuyến nghị (`> 100 RPS` duy trì hoặc tệp WAL `> 512 MiB`) → gợi ý di trú sang PostgreSQL;<br/>3. Nút cụm Data Plane lệch nhịp đồng bộ thu hồi `> 5 giây`;<br/>4. Phát sinh giao dịch `UNMETERED_FLAGGED` thiếu bảng giá;<br/>5. Có phần mở rộng đang dùng phiên bản hợp đồng cũ `N-1` sắp hết hạn hỗ trợ. |
| **`UX-GR-06`**<br/>**Kiểm soát Ghi đè Lạc quan & Phê duyệt Chống Bấm Nhầm** | Chống mất cấu hình khi nhiều quản trị viên thao tác cùng lúc. | - Mọi biểu mẫu chỉnh sửa (`PATCH`/`PUT`/`POST :approve`) đính kèm `If-Match: "<etag>"`; nếu nhận `409 version_conflict`, giao diện hiển thị bản đối chiếu chênh lệch (`Diff`) thay vì xóa công sức người dùng hoặc ghi đè mù.<br/>- Thao tác phá hủy diện rộng (`Quarantine Extension`, `Freeze Project`, `Delete Organization`) dùng `AlertDialog` yêu cầu nhập lại định danh (`slug`) và lý do bắt buộc (`>= 5` ký tự) để ghi kiểm toán. |

---

## 2. Chuẩn Chung Về Khả năng Tiếp cận (`WCAG 2.2 AA`) & Đáp ứng Đa Thiết bị (`Responsive Contract`)

Áp dụng đồng nhất cho toàn bộ **16 màn hình (`SCR-01`..`SCR-16`)** dưới đây (`TP-NFR-0701`, `TP-NFR-0702`, [`accessibility-checklist.md`](../../.agents/references/accessibility-checklist.md)):
1. **Tiếp cận Bàn phím & Trình đọc Màn hình (`WCAG 2.2 AA`):**
   - Khung `AppShell` cung cấp sẵn liên kết bỏ qua điều hướng (`Skip to main content`) ở điểm dừng `Tab` đầu tiên, vùng mốc `banner`, hai vùng `nav` có nhãn `label` riêng biệt và đúng một tiêu đề cấp 1 (`Heading level={1}`) trong vùng `main`.
   - Độ tương phản chữ thường `>= 4.5:1`, chữ lớn và đường viền thành phần tương tác `>= 3:1` trên cả chế độ Sáng và Tối của theme `neutral`.
   - **Không dùng màu sắc làm dấu hiệu duy nhất:** Mọi thẻ `Token` trạng thái đều ghép cặp `icon` đặc trưng + văn bản ngắn (ví dụ: biểu tượng dấu kiểm + `Healthy`, biểu tượng tam giác + `Degraded`, biểu tượng ổ khóa chắn + `Quarantined`).
   - Mọi thông báo thay đổi động (lọc bảng không ra kết quả, đã sao chép khóa, lưu thành công, lỗi xác thực biểu mẫu) đều phát qua hook `useAnnounce` (`polite` cho trạng thái, `assertive` cho lỗi/cảnh báo khẩn).
   - Mọi hộp thoại `Dialog` / `AlertDialog` giữ tiêu điểm bên trong khi mở, đóng bằng phím `Escape` và trả tiêu điểm về đúng nút đã kích hoạt khi đóng.
2. **Hợp đồng Đáp ứng Ba Ngưỡng Chiều Rộng (`Responsive Contract`):**
   - **Desktop (`>= 1280px`):** Hiển thị đầy đủ 3 vùng (`SideNav 256px` + `LayoutContent fill` + `LayoutPanel 380px` khi chọn dòng chi tiết).
   - **Tablet / Laptop hẹp (`768px – 1023px`):** Giữ `SideNav` (có thể thu gọn), chuyển ngăn chi tiết `LayoutPanel` sang hộp thoại nổi `Dialog` hoặc ngăn kéo `Drawer` có lớp mờ (`useMediaQuery('(max-width: 1024px)')`).
   - **Mobile (`375px – 767px`):** `SideNav` tự động gom vào ngăn kéo di động của `AppShell` (`mobileNav={{ breakpoint: 'md' }}`), lưới chỉ số tự động gập xuống 1 cột (`Grid columns={{ minWidth: 240, max: 4 }}`), kích thước vùng chạm tối thiểu `44x44 CSS px`. Các bảng cấu hình nhiều cột hỗ trợ cuộn ngang giữ cột định danh đầu tiên, riêng **4 luồng trực chiến khẩn cấp (`SCR-02`, `SCR-13`, `SCR-14`, `SCR-16`: xem cảnh báo, thu hồi `Virtual Key`, duyệt/từ chối lệnh `destructive`, kích hoạt `Kill-Switch`)** được tối ưu nút bấm toàn chiều rộng để hoàn tất dưới `15 giây` bằng một tay trên điện thoại.

---

## 3. Đặc tả Chi tiết 16 Màn hình Chuẩn (`SCR-01` .. `SCR-16`)

### Nhóm 1: Khởi tạo, Tổng quan & Cổng Mô hình (`SCR-01` .. `SCR-05`)

#### `SCR-01`: First-Run Setup Wizard & Admin Login (`/setup`, `/login`)
- **Mục tiêu:** Chuyển giao quyền sở hữu máy chủ an toàn trong lần chạy đầu tiên bằng `Bootstrap Token` và đưa người dùng tới yêu cầu suy luận thành công đầu tiên dưới `3 phút` (`UJ-01`), hoặc đăng nhập quản trị thường nhật (mật khẩu cục bộ / SSO OIDC).
- **Actor & Quyền:** `Personal Admin` / `OPERATOR` (lần đầu tại `/setup`); mọi vai trò Console tại `/login`.
- **Data Contracts:** `POST /admin/v1/setup:claim-bootstrap`, `POST /admin/v1/connections`, `POST /admin/v1/connections/{id}:validate`, `POST /admin/v1/virtual-keys`, `POST /v1/chat/completions` (kiểm tra luồng SSE đầu tiên), `POST /admin/v1/auth/login`.
- **Actions:** Xác thực mã mồi; đặt tài khoản `ORG_OWNER` đầu tiên; xác nhận đã lưu `TANPOPO_MASTER_KEY`; kết nối khóa API chính thức hoặc máy chủ `Ollama`/`vLLM` cục bộ; phát hành `Virtual Key` đầu tiên; bắn yêu cầu thử trực tiếp.
- **Validations:** `Bootstrap Token` đúng định dạng và còn hạn; mật khẩu quản trị `>= 12` ký tự; URL mô hình cục bộ hợp lệ và không trỏ vào địa chỉ metadata đám mây bị cấm (`169.254.169.254`).
- **Errors:** `401 bootstrap_token_expired_or_used` (hướng dẫn lệnh CLI sinh lại mã), `422 upstream_auth_failed`, `429 login_rate_limited`.
- **Confirmation:** Hộp kiểm bắt buộc xác nhận đã sao lưu khóa `Virtual Key` vừa sinh trước khi đóng bước cấp khóa (`UX-GR-01`).
- **Audit:** `system.bootstrap.claimed`, `iam.owner.created`, `connection.created`, `virtual_key.issued`.
- **Success Feedback:** Pháo hiệu trạng thái hoàn tất bước kèm thẻ tóm tắt `request_id`, độ trễ `TTFT` của cuộc gọi đầu tiên và nút chuyển vào `/overview`.
- **Accessibility & Responsive:** Mẫu `form-wizard-vertical` (tự xoay thanh bước sang ngang khi `< 768px`); `/login` dùng mẫu `login-sso` căn giữa tối giản, tự gán tiêu điểm vào ô đầu tiên.

#### `SCR-02`: Overview & System Posture Dashboard (`/overview`)
- **Mục tiêu:** Cung cấp bức tranh tức thời trong `5 giây` về lưu lượng, tỷ lệ lỗi, độ trễ `p95`, tốc độ đốt ngân sách `micro-USD`, sức khỏe các `Model Deployments` và danh sách cảnh báo cần hành động ngay.
- **Actor & Quyền:** Mọi vai trò (dữ liệu lọc tự động theo phạm vi `RBAC` của từng vai trò).
- **Data Contracts:** `GET /admin/v1/telemetry/overview?window=1h|24h|7d`, `GET /admin/v1/deployments?status=unhealthy`, `GET /admin/v1/alerts/active`.
- **Actions:** Đổi khung thời gian (`1h`, `24h`, `7d`, `30d`); bấm từ ô cảnh báo nhảy thẳng tới màn hình xử lý (`Re-authorize OAuth`, `Review Approval`, `Top-up Budget`, `Kill-Switch`).
- **Validations:** Khung thời gian truy vấn nằm trong giới hạn lưu giữ cấu hình.
- **Errors:** Khi kho số đo chậm hoặc mất kết nối OTLP ngoài, hiển thị dữ liệu cục bộ kèm `Banner` cảnh báo độ trễ đồng bộ.
- **Confirmation:** Không cần xác nhận cho thao tác lọc chỉ đọc.
- **Audit:** Không ghi kiểm toán cho truy vấn đọc tổng quan.
- **Success Feedback:** Thông báo tinh tế thời điểm cập nhật số liệu gần nhất (`Updated 5s ago`) và đọc tóm tắt cảnh báo mới qua `useAnnounce`.
- **Accessibility & Responsive:** Mẫu `dashboard-alert-rail`; lưới 4 ô chỉ số dùng `Grid columns={{ minWidth: 240, max: 4 }}` tự gập 2 cột ở `768px` và 1 cột ở `375px`, cột cảnh báo cuộn độc lập tự xếp xuống trên thiết bị hẹp.

#### `SCR-03`: Provider Adapters Directory (`/providers/adapters`)
- **Mục tiêu:** Xem toàn bộ trình điều khiển giao thức `Provider Adapters` đang có mặt trên máy chủ (cả gói đi kèm chính thức lẫn gói cài thêm), đối chiếu năng lực hỗ trợ và đi tới tạo `Provider Connection`.
- **Actor & Quyền:** Đọc: mọi vai trò quản trị; Cài mới / Cấu hình cấp máy chủ: `OPERATOR`.
- **Data Contracts:** `GET /admin/v1/provider-adapters?cursor=...&limit=50`, `GET /admin/v1/provider-adapters/{adapterId}`.
- **Actions:** Tìm kiếm theo tên hãng / giao thức (`OpenAI-Compatible`, `Anthropic Messages`, `Gemini`, `Custom`); mở ngăn chi tiết xem ma trận phương thức xác thực (`API Key`, `Authorized OAuth 2.1`, `AWS SigV4`) và bấm **"New Connection with this Adapter"**.
- **Validations:** Chỉ cho phép bấm tạo kết nối khi trạng thái gói mở rộng gốc là `ENABLED_ACTIVE`.
- **Errors:** Nếu gói mở rộng chứa Adapter đang bị `QUARANTINED`, hiển thị thẻ đỏ khóa nút tạo kết nối kèm liên kết tới sự kiện cách ly.
- **Confirmation:** Không áp dụng (chuyển sang `SCR-04` hoặc `SCR-06`).
- **Audit:** Không áp dụng cho thao tác xem.
- **Success Feedback:** Mở ngăn bên `LayoutPanel` mượt mà không tải lại trang.
- **Accessibility & Responsive:** Bảng `Table` (`table-page`), ngăn chi tiết chuyển thành `Dialog` khi `<= 1024px`.

#### `SCR-04`: Provider Connections & Authorized OAuth Vault (`/providers/connections`)
- **Mục tiêu:** Quản lý vòng đời các hồ sơ xác thực thật (`SM-02`, `SM-03`), nạp khóa một chiều vào `Write-Only Vault`, thực hiện luồng `Authorized OAuth 2.1 + PKCE`, kiểm tra kết nối sống và ngắt kết nối khi có sự cố.
- **Actor & Quyền:** `ORG_OWNER` (cấp `Org`), `TEAM_ADMIN` (cấp `Project`), `REVIEWER` (chỉ đọc siêu dữ liệu, `0` quyền xem/sửa khóa).
- **Data Contracts:** `GET /admin/v1/connections`, `POST /admin/v1/connections`, `PATCH /admin/v1/connections/{id}` (`If-Match`), `POST /admin/v1/connections/{id}:validate`, `POST /admin/v1/connections/{id}/oauth:start`, `GET /admin/v1/oauth/callback`.
- **Actions:** Tạo kết nối (`COMMERCIAL_API` hoặc `SELF_HOSTED_INFRA`); xoay khóa bí mật mới (ghi đè một chiều); khởi chạy / ủy quyền lại OAuth; bấm **"Smoke Test Connection"**; tạm ngưng hoặc xóa kết nối.
- **Validations:** Chế độ thanh toán bắt buộc thuộc `COMMERCIAL_API | SELF_HOSTED_INFRA` (`UX-GR-03`); tên miền máy chủ riêng phải khớp `allowed_hosts` của Adapter và vượt qua bộ lọc chống SSRF; khóa bí mật không được để trống khi tạo mới.
- **Errors:** `400 ssrf_blocked_private_ip`, `409 version_conflict`, `422 oauth_scope_insufficient`, trạng thái `AUTH_EXPIRED_REAUTH_REQUIRED` hiện `Token` màu cam kèm nút ủy quyền lại ngay trên dòng.
- **Confirmation:** Xóa hoặc vô hiệu hóa kết nối đang gắn với `Model Deployment` hoạt động mở `AlertDialog` liệt kê chính xác số lượng `Routes` sẽ chịu ảnh hưởng.
- **Audit:** `connection.created`, `connection.secret_rotated`, `connection.oauth_authorized`, `connection.disabled`, `connection.deleted`.
- **Success Feedback:** `Toast` xác nhận kèm độ trễ kiểm tra kết nối thành công (ví dụ `Connection verified in 142 ms`); trường bí mật lập tức xóa trắng khỏi bộ nhớ trình duyệt.
- **Accessibility & Responsive:** Bảng dòng dày đặc (`Table`), biểu mẫu tạo/xoay khóa mở trong `Drawer` có bẫy tiêu điểm bàn phím; trên điện thoại `375px` hỗ trợ nút ngắt khẩn cấp kết nối lỗi.

#### `SCR-05`: Model Catalog & Deployments Matrix (`/models`)
- **Mục tiêu:** Quản lý ánh xạ từ `Provider Connection` sang các `Model Deployments` vật lý (kèm `Data Region` & bảng giá `micro-USD`) và cung cấp danh mục tra cứu năng lực minh bạch cho kỹ sư (`UJ-04`).
- **Actor & Quyền:** Tạo/Sửa `Deployment`: `ORG_OWNER`, `TEAM_ADMIN`; Tra cứu danh mục & năng lực: `DEVELOPER`, `REVIEWER`.
- **Data Contracts:** `GET /admin/v1/models?env_id=...`, `GET /admin/v1/deployments?env_id=...`, `POST /admin/v1/deployments`, `PATCH /admin/v1/deployments/{id}` (`If-Match`).
- **Actions:** Lọc đa tiêu chí (`Capability`, `Assurance Level`, `Data Region`, `Health SM-04`); đăng ký `Deployment` mới; cập nhật bảng giá tùy chỉnh (hợp đồng doanh nghiệp riêng); sao chép `Model Slug` để gọi API.
- **Validations:** Đơn giá nhập vào phải `>= 0` (nếu là mô hình tự host chưa gắn đơn giá điện/GPU, bắt buộc tích chọn rõ `Self-Hosted Zero Rate` hoặc đánh dấu để không trộn vào chi phí thương mại `EXACT`); mã vùng dữ liệu bắt buộc khai báo rõ (`EU`, `US`, `APAC`, `ON_PREM`).
- **Errors:** Cảnh báo rõ các tính năng `UNSUPPORTED` hoặc chỉ `DECLARED` chưa qua kiểm thử (`UX-GR-02`); hiển thị trạng thái ngắt mạch `CIRCUIT_OPEN` thời gian thực.
- **Confirmation:** Hạ hoặc gỡ `Deployment` cuối cùng đang phục vụ một `Route` yêu cầu xác nhận qua `AlertDialog`.
- **Audit:** `deployment.created`, `deployment.pricing_updated`, `deployment.state_changed`.
- **Success Feedback:** `Toast` lưu thành công và làm mới hàng trong bảng giữ nguyên vị trí cuộn.
- **Accessibility & Responsive:** Mẫu `table-filter` có quản lý ẩn/hiện cột qua `useTableColumnSettingsState`; ngăn so sánh năng lực cuộn mượt bằng bàn phím.

---

### Nhóm 2: Hệ sinh thái Extension, Định tuyến & Mặt phẳng MCP/A2A (`SCR-06` .. `SCR-10`)

#### `SCR-06`: Extension Marketplace & Custom Source Installer (`/extensions/marketplace`)
- **Mục tiêu:** Duyệt danh mục gói mở rộng đã kiểm duyệt hoặc nạp gói từ Git commit SHA / URL `.tpkg` / Private OCI Registry với quy trình kiểm tra danh tính nhà phát hành và bảng quyền bắt buộc trước khi cài (`UJ-02`, `UX-GR-04`).
- **Actor & Quyền:** Xem danh mục: mọi quản trị viên; Nạp & gửi yêu cầu cài đặt: `OPERATOR`; Phê duyệt độc lập (khi bật 4 mắt): `REVIEWER`.
- **Data Contracts:** `GET /admin/v1/marketplace/catalog`, `POST /admin/v1/extensions:install` (giai đoạn dàn dựng `PENDING_PERMISSION_APPROVAL`), `POST /admin/v1/extensions/{id}:approve-permissions` (`If-Match`).
- **Actions:** Lọc theo loại mở rộng (`Provider Adapter`, `Auth Driver`, `Policy/Guardrail`, `Telemetry Exporter`) và `Trust Tier`; bấm **"Inspect Permissions & Trust..."** mở ngăn kiểm tra; dán URI + mã băm `SHA-256` 64 ký tự để kiểm định gói ngoài.
- **Validations:** `expected_sha256` bắt buộc khớp chuẩn `^[a-f0-9]{64}$`; từ chối gói thiếu khai báo quyền hoặc đòi quyền mạng ký tự đại diện `*`; gói `Unverified External` bắt buộc tích ô xác nhận rủi ro bên thứ ba.
- **Errors:** `400 hash_mismatch`, `400 contract_version_incompatible`, `403 reserved_namespace_spoofing`, cảnh báo đỏ nếu gói nằm trong danh sách thu hồi an ninh (`Revocation Advisory`).
- **Confirmation:** Hộp thoại `Dialog` **Permission Inspection Sheet** bắt buộc đối chiếu toàn bộ quyền mạng/bí mật/tài nguyên trước khi nút **"Approve & Activate"** sáng lên (`UX-GR-04`).
- **Audit:** `extension.staged`, `extension.permissions_approved`, `extension.install_rejected`.
- **Success Feedback:** `Toast` thông báo gói đã ghi vào `tanpopo.lock` và sẵn sàng sử dụng ngay không cần khởi động lại Core, kèm nút chuyển sang tạo `Connection`.
- **Accessibility & Responsive:** Bảng/danh sách rõ ràng với 3 thẻ `Token` (`Origin`, `Publisher Tier`, `Execution Tier`) có biểu tượng phân biệt; hộp kiểm tra quyền cuộn dọc đầy đủ trên màn hình nhỏ.

#### `SCR-07`: Installed Plugins, Lockfile Diff & Rollback (`/extensions/installed`, `/extensions/lockfile`)
- **Mục tiêu:** Quản trị vòng đời các phần mở rộng đang cài trên máy chủ (`SM-01`), rà soát nâng cấp bị chặn do đòi tăng quyền (`UPDATING_PENDING_RECONSENT` — `UJ-07`), quay lui phiên bản trong `< 5s` hoặc cách ly khẩn cấp (`QUARANTINE`), và xuất `tanpopo.lock` cho GitOps.
- **Actor & Quyền:** `OPERATOR` (quản trị cài đặt/quay lui/cách ly), `ORG_OWNER` (bật/tắt trong phạm vi `Org`), `REVIEWER` (duyệt tăng quyền).
- **Data Contracts:** `GET /admin/v1/extensions/installations`, `POST /admin/v1/extensions/{id}:upgrade`, `POST /admin/v1/extensions/{id}:rollback` (`If-Match`), `GET /admin/v1/extensions/lockfile`, `POST /admin/v1/kill-switches:trigger` (`EXTENSION`).
- **Actions:** Kiểm tra bản cập nhật; mở hộp **Side-by-Side Permission Diff** khi gói ở trạng thái `UPDATING_PENDING_RECONSENT`; bấm `Rollback` về bản ghim trước; bấm `Quarantine` khẩn cấp; tải xuống/sao chép tệp `tanpopo.lock`.
- **Validations:** Không cho phép kích hoạt bản nâng cấp có quyền mở rộng nếu chưa duyệt bảng chênh lệch quyền; không cho gỡ cài đặt cứng khi còn `Connection` tham chiếu (phải ngắt hoặc chuyển sang `DISABLED` trước).
- **Errors:** `409 version_conflict`, cảnh báo vàng `Deprecated Contract N-1` (`UX-GR-05`).
- **Confirmation:** `Rollback` và `Quarantine` dùng `AlertDialog` hiển thị số lượng `Connections` và `Projects` sẽ bị tạm ngưng tức thì.
- **Audit:** `extension.upgraded`, `extension.escalation_reconsented`, `extension.rolled_back`, `extension.quarantined`.
- **Success Feedback:** Cập nhật trạng thái dòng tức thời và hiển thị báo cáo bán kính tác động sau khi cách ly.
- **Accessibility & Responsive:** Bảng `Table` (`table-page`); bản so sánh quyền mới/cũ dùng ký hiệu dấu cộng `+` / dấu trừ `-` tường minh ngoài màu nền xanh/đỏ để người mù màu đọc rõ.

#### `SCR-08`: Capability Routes & Dry-Run `Route Explain` Studio (`/routes`, `/routes/explain`)
- **Mục tiêu:** Thiết kế các tuyến định tuyến thông minh có dự phòng trước luồng (`Priority`, `Weighted`, `Lowest-Cost`), khóa cứng vùng cư trú dữ liệu và kiểm thử khô cây quyết định định tuyến (`UJ-05`).
- **Actor & Quyền:** Tạo/Sửa `Route`: `ORG_OWNER`, `TEAM_ADMIN`; Chạy mô phỏng `Route Explain`: mọi vai trò (kể cả `DEVELOPER`).
- **Data Contracts:** `GET /admin/v1/routes?env_id=...`, `POST /admin/v1/routes`, `PUT /admin/v1/routes/{id}` (`If-Match`), `POST /admin/v1/routes:explain`.
- **Actions:** Sắp xếp thứ tự ưu tiên hoặc tỷ trọng phần trăm các `Model Deployments` (bằng nút lên/xuống hỗ trợ bàn phím bên cạnh kéo thả); đặt ngưỡng timeout và chính sách ngắt mạch; nhập kịch bản thử vào `Route Explain` để xem cây loại trừ từng bước.
- **Validations:** Tổng trọng số của tuyến `Weighted` phải bằng `100%`; mọi đích trong tuyến phải thuộc cùng `Environment` hoặc phạm vi `Org` hợp lệ; cảnh báo đỏ nếu tất cả đích đều xung đột với chính sách `Data Residency` cấp trên.
- **Errors:** `409 version_conflict`, `422 invalid_weight_sum`, mô phỏng khô hiển thị rõ mã `503 no_compliant_deployment_available` khi không còn ứng viên hợp lệ.
- **Confirmation:** Sửa tuyến đang nhận lưu lượng lớn ở môi trường `prod` hiện hộp thoại tóm tắt thay đổi kèm tùy chọn chạy `Route Explain` kiểm tra trước khi lưu.
- **Audit:** `route.created`, `route.updated`, `route.deleted`.
- **Success Feedback:** `Toast` xác nhận phiên bản `ConfigSnapshot` mới đã phát tán (`< 1s`).
- **Accessibility & Responsive:** Mẫu `form-two-column` trên Desktop; mọi thao tác đổi thứ tự ưu tiên đều có nút bấm bàn phím rõ ràng (không phụ thuộc chuột kéo thả).

#### `SCR-09`: Hierarchical Policies & Guardrails Configuration (`/policies`)
- **Mục tiêu:** Quản lý các chính sách kế thừa phân cấp (`Instance → Organization → Project → Environment`) về giới hạn tốc độ (`RPM`/`TPM`/`Concurrent Streams`), vùng cư trú dữ liệu bắt buộc, danh sách mô hình cho phép và các bộ lọc nội dung/PII (`Guardrails`).
- **Actor & Quyền:** Cấp `Instance`: `OPERATOR`; Cấp `Org`: `ORG_OWNER`; Cấp `Project/Env` (chỉ được thu hẹp chặt hơn, cấm nới lỏng trần cấp trên — `TP-BR-0501`): `TEAM_ADMIN`.
- **Data Contracts:** `GET /admin/v1/policies/effective?env_id=...`, `PUT /admin/v1/policies/{policyId}` (`If-Match`).
- **Actions:** Xem bảng hợp nhất **Effective Policy** (chỉ rõ mỗi quy tắc đang kế thừa từ tầng nào); thêm quy tắc thu hẹp ở dự án; cấu hình hành động khi bộ lọc PII/Guardrail gặp sự cố (`Fail-Closed` bắt buộc cho dữ liệu nhạy cảm vs `Fail-Open` ghi cảnh báo cho bộ lọc phụ).
- **Validations:** Chặn ngay trên giao diện và máy chủ nếu quản trị dự án cố đặt trần `RPM` cao hơn trần `Org` hoặc cố mở vùng dữ liệu `US` khi `Org` đã khóa cứng `EU Only`.
- **Errors:** `422 policy_relaxation_forbidden` (chỉ rõ quy tắc cha đang khóa ở tầng `Organization`), `409 version_conflict`.
- **Confirmation:** Bật quy tắc chặn diện rộng trên môi trường `prod` mở `AlertDialog` xác nhận.
- **Audit:** `policy.created`, `policy.updated`, `policy.guardrail_attached`.
- **Success Feedback:** Hiển thị ngay kết quả hợp nhất chính sách mới sau khi lưu.
- **Accessibility & Responsive:** Mẫu `table-tree` / `settings-sidebar` thể hiện rõ quan hệ cha-con bằng mức thụt lề và nhãn cấp độ văn bản.

#### `SCR-10`: MCP Governance Hub & A2A Agent Registry (`/mcp/*` — `v0.2`, `/a2a/*` — `v0.3`)
- **Mục tiêu:** Đăng ký máy chủ công cụ `MCP Server`, đóng dấu vân tay `schema_hash` từng công cụ, gán phân loại rủi ro (`read_only` / `mutating` / `destructive` — `UJ-06`), và (ở nhịp `v0.3`) quản lý danh bạ `A2A AgentCard` kèm trần độ sâu ủy quyền `hop_count <= 5`.
- **Actor & Quyền:** Quản trị đăng ký & ghim băm: `ORG_OWNER`, `TEAM_ADMIN`; Duyệt lại khi lệch schema: `ORG_OWNER` / `REVIEWER`; Xem danh mục công cụ được cấp: `DEVELOPER`.
- **Data Contracts:** `GET /admin/v1/mcp/servers`, `POST /admin/v1/mcp/servers`, `POST /admin/v1/mcp/servers/{id}:sync-tools`, `POST /admin/v1/mcp/tools/{toolId}:approve-schema` (`If-Match`), `GET /admin/v1/a2a/agents` (`v0.3`).
- **Actions:** Đồng bộ danh mục công cụ từ máy chủ MCP đích; gán nhãn rủi ro mặc định đóng (công cụ mới chưa phân loại không tự mở là `read_only`); mở hộp so sánh `Schema Diff` khi phát hiện máy chủ ngoài tự ý đổi mô tả/tham số công cụ; đặt trần ngân sách và số bước nhảy ủy quyền cho `A2A Agent`.
- **Validations:** URL máy chủ MCP/A2A phải vượt qua kiểm tra chống SSRF; `max_delegation_hops` giới hạn trong khoảng `1..5`.
- **Errors:** Cảnh báo đỏ `SCHEMA_DRIFT_LOCKED` tự động khóa công cụ bị đổi ngầm cho tới khi được duyệt lại; `502 mcp_upstream_unreachable`.
- **Confirmation:** Đổi phân loại một công cụ từ `destructive` xuống `read_only` (bỏ cổng duyệt con người) bắt buộc xác nhận trong `AlertDialog` kèm ghi lý do kiểm toán.
- **Audit:** `mcp.server_registered`, `mcp.tool_schema_pinned`, `mcp.schema_drift_detected`, `mcp.risk_tier_changed`, `a2a.agent_registered`.
- **Success Feedback:** `Toast` thông báo số lượng công cụ đã ghim `schema_hash` an toàn.
- **Accessibility & Responsive:** Hai tab `TabList` tách biệt rõ `MCP Servers & Tools` và `A2A Agents`; bảng công cụ dùng `Table` chuẩn không bọc thẻ rườm rà.

---

### Nhóm 3: Quan sát, Tài chính FinOps, Truy cập & Trực chiến Bảo mật (`SCR-11` .. `SCR-16`)

#### `SCR-11`: Request Traces & Waterfall Inspector (`/observability/traces`)
- **Mục tiêu:** Tra cứu nhanh nhật ký truy vết siêu dữ liệu theo thời gian thực và mổ xẻ dòng thời gian 12 chặng của từng `request_id` để chẩn đoán lỗi/độ trễ tức thì mà vẫn bảo vệ quyền riêng tư mặc định (`UJ-08`, `TP-BR-0802`).
- **Actor & Quyền:** `OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `REVIEWER`, `DEVELOPER` (chỉ thấy yêu cầu của khóa/dự án mình có quyền).
- **Data Contracts:** `GET /admin/v1/traces?env_id=...&status=...&model=...&cursor=...`, `GET /admin/v1/traces/{requestId}`.
- **Actions:** Tìm chính xác theo `request_id`; lọc theo mã trạng thái (`4xx`, `5xx`, `mid_stream_abort`), `Virtual Key`, `Route`, `Data Region` hoặc độ chính xác chi phí (`ESTIMATED`, `UNMETERED_FLAGGED`); mở ngăn `LayoutPanel` xem thanh thác nước (`Waterfall`) và bấm **"Replay Metadata in Route Explain"**.
- **Validations:** Giới hạn khung thời gian tìm kiếm hợp lệ; mặc định **không hiển thị nội dung văn bản prompt/completion** (nếu dự án có bật chế độ gỡ lỗi ngắn hạn `TTL <= 72h`, việc mở xem nội dung yêu cầu quyền riêng và tự sinh bản ghi kiểm toán).
- **Errors:** Không tiết lộ sự tồn tại của `request_id` thuộc tổ chức khác (`404 trace_not_found_in_scope`).
- **Confirmation:** Bấm xem nội dung gỡ lỗi nhạy cảm (khi tính năng debug payload đang bật) hiện cảnh báo xác nhận thao tác sẽ được ghi vào `Audit Log`.
- **Audit:** `trace.debug_payload_viewed` (khi mở xem nội dung gỡ lỗi).
- **Success Feedback:** Sao chép `request_id` hoặc đoạn chẩn đoán lỗi đã che bí mật với thông báo `Toast`.
- **Accessibility & Responsive:** Mẫu `table-filter` kết hợp `LayoutPanel`; thanh thời gian Waterfall kèm bảng số liệu mili-giây dạng văn bản cho trình đọc màn hình.

#### `SCR-12`: FinOps Budgets, Pre-Hold Monitor & Chargeback Ledger (`/finops/budgets`, `/finops/chargeback`)
- **Mục tiêu:** Thiết lập trần ngân sách cứng/mềm (`BIGINT micro-USD`), theo dõi trực quan cả phần đã quyết toán (`Settled`) lẫn phần đang tạm giữ cho các luồng mở (`Active Pre-Request Holds` — `SM-07`), và xuất báo cáo phân bổ chi phí minh bạch độ chính xác (`EXACT` vs `ESTIMATED` vs `UNMETERED_FLAGGED`).
- **Actor & Quyền:** Thiết lập/Nâng trần ngân sách: `ORG_OWNER` (cấp `Org`/`Project`), `TEAM_ADMIN` (chia nhỏ trong `Project`); Đọc báo cáo: `REVIEWER`, `DEVELOPER` (phạm vi cá nhân).
- **Data Contracts:** `GET /admin/v1/budgets`, `PUT /admin/v1/budgets/{id}` (`If-Match`), `GET /admin/v1/ledger/chargeback?group_by=project|env|key|model&from=...&to=...`, `GET /admin/v1/ledger/export.csv`.
- **Actions:** Đặt hạn mức theo ngày/tháng và ngưỡng cảnh báo sớm (`50%`, `80%`, `100%`); xử lý số dư âm nhẹ kết chuyển cuối luồng (`Negative Balance Carryover` — `BA-OQ-002`); lọc và xuất CSV/JSON báo cáo `Chargeback` tách riêng 3 cột `Exact Commercial Spend`, `Estimated Spend` và `Unmetered Requests Count`.
- **Validations:** Trần ngân sách con không được vượt trần ngân sách cha nếu tổ chức khóa phân bổ cứng; giá trị tiền tệ quy đổi chính xác sang đơn vị nguyên `micro-USD` không làm tròn dấu phẩy động sai lệch.
- **Errors:** `409 version_conflict`; hiển thị `Banner` cảnh báo nổi bật nếu có bất kỳ mô hình nào phát sinh `UNMETERED_FLAGGED` (thiếu bảng giá) để quản trị viên bổ sung ngay tại `SCR-05` (`UX-GR-05`).
- **Confirmation:** Nâng trần ngân sách vượt ngưỡng lớn hoặc xóa trần chặn cứng mở `AlertDialog` (hỗ trợ luồng 4 mắt nếu `Organization` bật kiểm soát tài chính chéo).
- **Audit:** `budget.created`, `budget.limit_changed`, `budget.carryover_cleared`, `ledger.exported`.
- **Success Feedback:** Cập nhật ngay thanh tiến độ ngân sách ba thành phần (`Settled` + `Reserved Hold` + `Remaining`).
- **Accessibility & Responsive:** Mẫu `dashboard-comparison` + `Table`; thanh tiến độ ngân sách luôn hiển thị con số phần trăm và giá trị `USD` bằng chữ ngay bên dưới để không phụ thuộc màu thanh.

#### `SCR-13`: Virtual API Keys, Team Members & RBAC (`/access/virtual-keys`, `/access/members`, `/access/roles`)
- **Mục tiêu:** Phát hành `Virtual Key` gắn cứng với **đúng 1 `Environment`** (`TP-BR-0103`), giới hạn danh sách `Models`/`Routes`/`MCP Tools` được gọi, thực hiện xoay vòng êm hai khóa song hành (`Graceful Dual-Key Rotation` — `WF-10`) hoặc thu hồi khẩn cấp cắt luồng (`Emergency Revoke` — `BA-OQ-003`), và quản lý thành viên theo 5 vai trò RBAC.
- **Actor & Quyền:** `ORG_OWNER` (toàn quyền `Org`), `TEAM_ADMIN` (quản lý khóa & thành viên trong `Project`), `DEVELOPER` (chỉ tạo/xoay khóa cá nhân nếu chính sách dự án bật).
- **Data Contracts:** `GET /admin/v1/virtual-keys?env_id=...`, `POST /admin/v1/virtual-keys`, `POST /admin/v1/virtual-keys/{id}:rotate`, `POST /admin/v1/virtual-keys/{id}:revoke`, `GET /admin/v1/members`, `POST /admin/v1/members`.
- **Actions:** Tạo `Virtual Key` (chọn `Environment`, hạn dùng, trần `RPM`/`TPM`/chi phí riêng, danh sách trắng `Models` & `MCP Tools`); bấm **"Rotate Key (Grace Window)"** đặt thời gian chồng lấp `1h..72h`; bấm **"Emergency Security Revoke"** cắt đứt ngay cả luồng đang mở; gán vai trò RBAC cho thành viên.
- **Validations:** Một `Virtual Key` không thể chọn nhiều `Environments`; thời hạn khóa không vượt quá trần tối đa của chính sách bảo mật tổ chức; người dùng không thể tự nâng quyền của chính mình (`Self-Privilege Escalation` bị chặn).
- **Errors:** `403 role_escalation_denied`, `409 version_conflict`.
- **Confirmation:**
  - Khi vừa tạo hoặc xoay khóa: `Dialog` hiển thị chuỗi khóa mới **đúng 1 lần duy nhất** (`UX-GR-01`).
  - Khi bấm thu hồi: `AlertDialog` bắt buộc người dùng chọn rõ giữa **Chế độ 1: Hết hạn êm (cho luồng dở chạy nốt `<= 60s`)** và **Chế độ 2: Thu hồi An ninh Khẩn cấp (cắt ngang mọi luồng SSE đang truyền trong `< 1s`/`< 5s`)**.
- **Audit:** `virtual_key.issued`, `virtual_key.rotated`, `virtual_key.revoked_graceful`, `virtual_key.revoked_emergency`, `iam.member_role_changed`.
- **Success Feedback:** `Toast` xác nhận thu hồi kèm số lượng luồng đang mở đã bị cắt đứt thành công.
- **Accessibility & Responsive:** Trên màn hình di động `375px`, danh sách khóa giữ nổi bật nút **Emergency Revoke** kích thước chạm `>= 44px` để kỹ sư trực chiến khóa ngay khóa lộ từ điện thoại.

#### `SCR-14`: Security Center — Approval Queue, Audit Log & 4-Level Kill-Switches (`/security/*`)
- **Mục tiêu:** Trung tâm chỉ huy bảo mật hợp nhất ba năng lực sống còn: (1) Hàng đợi phê duyệt con người cho lệnh `MCP destructive` và nâng quyền Plugin (`SM-06`), (2) Nhật ký kiểm toán bất biến nối chuỗi băm (`TP-FR-0704`), và (3) Bảng công tắc ngắt khẩn cấp 4 cấp độ (`OPS-04`).
- **Actor & Quyền:** Hàng đợi phê duyệt: `REVIEWER`, `ORG_OWNER`; Kill-Switch: `OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN` (theo phạm vi); Đọc & kiểm tra tính toàn vẹn `Audit Log`: `REVIEWER`, `ORG_OWNER`, `OPERATOR`.
- **Data Contracts:** `GET /admin/v1/approvals?status=PENDING`, `POST /admin/v1/approvals/{id}:decide` (`If-Match`), `GET /admin/v1/audit-events?actor=...&action=...&cursor=...`, `POST /admin/v1/audit-events:verify-chain`, `POST /admin/v1/kill-switches:trigger`.
- **Actions:** Duyệt hoặc Từ chối yêu cầu chờ (hiển thị rõ tham số JSON đã băm `SHA-256` không thể sửa đổi); kích hoạt 1 trong 4 công tắc khẩn cấp (`Revoke Key` / `Freeze Project` / `Disable Connection` / `Quarantine Extension`) kèm xem báo cáo bán kính ảnh hưởng (`Blast Radius Report`); lọc/xuất `Audit Log` và bấm kiểm chứng chuỗi băm chống sửa trộm.
- **Validations:** Người gửi yêu cầu không được tự phê duyệt yêu cầu của chính mình khi bật `Four-Eyes`; lý do từ chối hoặc kích hoạt Kill-Switch bắt buộc `>= 5` ký tự.
- **Errors:** `409 approval_already_decided_or_expired` (khi hết hạn chờ hoặc người khác đã xử lý trước), `403 self_approval_forbidden`.
- **Confirmation:** Mọi thao tác kích hoạt `Kill-Switch` cấp `Project`, `Connection` hoặc `Extension` mở `AlertDialog` yêu cầu gõ lại đúng tên định danh (`slug`) và chọn có cắt luồng đang chạy hay không.
- **Audit:** `approval.approved`, `approval.rejected`, `killswitch.triggered`, `audit.chain_verified`.
- **Success Feedback:** Phản hồi tức thì trạng thái đồng bộ toàn cụm (`Cluster Epoch #... acknowledged by all active nodes`) và báo cáo danh sách khóa/dự án chịu tác động.
- **Accessibility & Responsive:** Mẫu `incident-console`; ưu tiên hàng đầu khả năng thao tác hoàn chỉnh bằng 1 tay trên điện thoại `375px` (`<= 3` lần chạm từ thông báo cảnh báo tới lúc kích hoạt xong Kill-Switch).

#### `SCR-15`: Developer Workspace — Playground, API Explorer, SDK Snippets & Smoke Diagnostics (`/devtools/*`)
- **Mục tiêu:** Giúp kỹ sư tích hợp kiểm thử nhanh mô hình/tuyến đường ngay trên trình duyệt, tra cứu tài liệu OpenAPI tương tác, lấy đoạn mã mẫu điền sẵn đúng địa chỉ cổng & tên mô hình, và chạy kiểm tra khói (`Smoke Test`) khóa ảo.
- **Actor & Quyền:** `DEVELOPER`, `TEAM_ADMIN`, `ORG_OWNER` (`OPERATOR` có thêm tab xuất gói chẩn đoán `support-bundle`).
- **Data Contracts:** `POST /v1/chat/completions` (hoặc `/native/...` khi thử trong `Playground` bằng khóa phiên thử nghiệm có hạn mức), `GET /admin/v1/openapi.json`, `POST /admin/v1/diagnostics:smoke-test`, `POST /admin/v1/diagnostics:support-bundle` (`OPERATOR`).
- **Actions:** So sánh song song phản hồi streaming của 2 mô hình/tuyến trong `Playground` (hiển thị trực tiếp `TTFT`, `tokens/sec`, chi phí quyết toán và cây `Route Explain`); sao chép đoạn mã mẫu (`OpenAI Python/TS/Go`, `Anthropic/Gemini Native`, `curl`, cấu hình `MCP Client`); tải gói chẩn đoán `support-bundle` đã tự động che sạch mọi khóa bí mật và PII.
- **Validations:** `Playground` không tự động lưu trữ lịch sử câu hỏi nhạy cảm lên máy chủ (chỉ giữ tạm trong bộ nhớ phiên trình duyệt của chính kỹ sư và xóa ngay khi đóng tab hoặc bấm `Clear`).
- **Errors:** Hiển thị nguyên bản phong bì lỗi chuẩn `CanonicalErrorEnvelope` cạnh thẻ hướng dẫn cách sửa lỗi tương ứng (`Error Resolution Card`).
- **Confirmation:** Không áp dụng cho thử nghiệm thông thường; xuất `support-bundle` hiện danh mục những thông tin được thu thập và khẳng định `0` bí mật thô được đóng gói.
- **Audit:** `diagnostics.support_bundle_generated`.
- **Success Feedback:** `Toast` xác nhận đã sao chép đoạn mã SDK vào khay nhớ tạm.
- **Accessibility & Responsive:** Mẫu `ai-chat` (có khung kết quả bên cạnh có thể co giãn) cho `Playground` và `documentation-technical` cho kho đoạn mã SDK; trên di động xếp dọc gọn gàng.

#### `SCR-16`: Instance Operations, Backups, Upgrades & Privacy Settings (`/ops/*`, `/settings/*`)
- **Mục tiêu:** Cung cấp bảng điều khiển vận hành tự chủ cho `OPERATOR` và `ORG_OWNER`: kiểm tra sức khỏe từng thành phần (`/healthz`, `/readyz`, độ trễ đồng bộ cụm), chạy kiểm tra tương thích trước nâng cấp (`Upgrade Preflight`), quản lý sao lưu mã hóa, cài đặt thời hạn lưu giữ (`TTL`) và thực thi xóa sạch dữ liệu tổ chức/dự án (`Crypto-Shredding < 60s`).
- **Actor & Quyền:** `/ops/*`: chỉ `OPERATOR` (hoặc `Personal Admin`); `/settings/residency-retention`: `ORG_OWNER`.
- **Data Contracts:** `GET /admin/v1/ops/cluster-status`, `POST /admin/v1/ops/backups:trigger`, `POST /admin/v1/ops/upgrades:preflight`, `PUT /admin/v1/settings/retention` (`If-Match`), `DELETE /admin/v1/projects/{id}` / `DELETE /admin/v1/organizations/{id}`.
- **Actions:** Xem trạng thái từng nút Data Plane (`ConfigSnapshot Epoch`, kết nối DB/Valkey/KMS, mức dùng bộ nhớ hồ Wasm); kích hoạt chụp sao lưu nóng; tải tệp kiểm tra tương thích hợp đồng trước khi nâng cấp phiên bản Core (`SM-08`); chỉnh thời hạn tự xóa cho truy vết kỹ thuật (`7..30 ngày`) và sổ cái kiểm toán (`>= 365 ngày`); đóng và xóa vĩnh viễn dự án/tổ chức.
- **Validations:** Cấm đặt `TTL` của nội dung gỡ lỗi vượt quá `72 giờ`; cấm hạ thời gian lưu sổ cái kiểm toán xuống dưới ngưỡng chính sách tối thiểu của đơn vị; kiểm tra không còn luồng mở trước khi hủy khóa mã hóa dự án.
- **Errors:** Báo cáo rõ danh sách các plugin chặn nâng cấp (`BLOCKED_INCOMPATIBLE` — nêu rõ tên gói và phiên bản hợp đồng lệch); cảnh báo dung lượng đĩa thấp trước khi chụp sao lưu.
- **Confirmation:** Xóa vĩnh viễn `Project` hoặc `Organization` dùng `AlertDialog` cấp nguy hiểm cao nhất: yêu cầu gõ chính xác câu xác nhận `DELETE <slug>` và giải thích rõ khóa giải mã `DEK` sẽ bị hủy vĩnh viễn không thể khôi phục trong `< 60 giây`.
- **Audit:** `ops.backup_created`, `ops.upgrade_preflight_ran`, `settings.retention_updated`, `tenant.crypto_shredded`.
- **Success Feedback:** Biên nhận hoàn tất kèm mã băm tệp sao lưu hoặc biên bản xác nhận hủy khóa mật mã học.
- **Accessibility & Responsive:** Mẫu `settings-sidebar` + bảng trạng thái nút cụm `Table`; đọc rõ trạng thái sức khỏe từng nút cho trình đọc màn hình mà không phụ thuộc chấm màu.
