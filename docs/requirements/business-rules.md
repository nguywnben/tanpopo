# Danh mục Quy tắc Nghiệp vụ Chuẩn hóa (Canonical Business Rules) — Tanpopo

> **Tài liệu Phân tích Nghiệp vụ (Phase ASTRA A02 — `DRAFT / UNLOCKED SCOPE`)**
> - **Phiên bản:** `0.1.0-a02-draft`
> - **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Lead Business Analyst & Product Architect
> - **Trạng thái quản trị:** **Bản nháp chưa khóa phạm vi (`DRAFT`)** — chờ Product Owner duyệt **Gate G1** và **Gate G2** tại [`docs/governance/decisions.md`](../governance/decisions.md).
> - **Nguyên tắc biên soạn:** Mỗi quy tắc chỉ định nghĩa **bất biến nghiệp vụ, trách nhiệm, ranh giới tuân thủ và hành vi có thể kiểm chứng từ bên ngoài**, tuyệt đối không trộn lẫn lựa chọn công nghệ triển khai nội bộ trừ các ràng buộc sản phẩm đã xác nhận (`C-01`..`C-04`).

---

## 1. Nguyên lý Nền tảng: Phân tách Khả năng Kỹ thuật và Quyền Sử dụng Hợp pháp (`CONFIRMED_BY_OWNER`)

Một trụ cột tư duy xuyên suốt toàn bộ quy tắc nghiệp vụ của Tanpopo là sự phân biệt tuyệt đối giữa hai khái niệm thường bị đánh đồng sai lầm trên thị trường:

| Khía cạnh | **1. Cơ chế Kỹ thuật Có thể Chạy (Technical Execution Capability)** | **2. Quyền Sử dụng Được Nhà cung cấp Cấp phép (Provider-Granted Entitlement)** |
|---|---|---|
| **Bản chất** | Hệ thống phần mềm có khả năng gửi gói tin, đính kèm mã xác thực, trao đổi OAuth hoặc phân tích phản hồi thành công mà không báo lỗi cú pháp. | Hợp đồng thương mại, điều khoản dịch vụ (TOS) và chính sách nhà phát triển của nhà cung cấp đích cho phép chủ thể dùng thông tin xác thực đó qua một cổng điều phối tự động. |
| **Ví dụ điển hình** | Bộ công cụ phát triển Plugin của Tanpopo hỗ trợ nguyên hàm OAuth 2.1, tiêu đề tùy chỉnh và định danh liên kết để phục vụ doanh nghiệp nối cổng SSO nội bộ. | Khóa API thương mại trả theo mức dùng (`Commercial API Key`) hoặc ứng dụng OAuth được nhà cung cấp đăng ký và cấp phép rõ ràng cho tích hợp bên thứ ba. |
| **Ví dụ Không Hợp lệ (`REJECTED`)** | Một đoạn mã bên ngoài trích xuất thành công cookie/OAuth của gói thuê bao cá nhân (`ChatGPT Plus/Pro`, `Codex` cá nhân, `Claude Pro/Max`, `Google Antigravity`...) và gọi ra chữ được. | **KHÔNG CÓ QUYỀN HỢP PHÁP:** Gói thuê bao cá nhân chỉ cấp quyền cho người dùng thao tác trong ứng dụng chính hãng, không cấp quyền biến thành điểm cuối API dùng chung hay miễn phí tín dụng API. |

---

## 2. Danh mục Quy tắc Nghiệp vụ Trọng yếu (`TP-BR-0101` .. `TP-BR-0803`)

### Nhóm 01: Đa người thuê, Sở hữu & Chia sẻ Phạm vi (`Tenancy & Scope Sharing`)

#### `TP-BR-0101` — Cô lập Tuyệt đối Giữa các Tổ chức (`Hard Cross-Tenant Isolation`)
- **Owner:** `Organization Owner` & `Operator` | **Impact:** `Critical` (Bảo mật & Pháp lý) | **Source / Status:** `PROPOSED` (Từ `UC-06`, `WF-06`)
- **Quy tắc:** Mọi tài nguyên nghiệp vụ (`Secret Credential`, `Provider Connection`, `Route`, `Virtual Key`, `MCP Server Registration`, `Agent Endpoint`, `Budget Ledger`, `Usage Record`, `Audit Event`) thuộc một `Organization` hoàn toàn vô hình và không thể truy cập, tham chiếu hoặc suy đoán sự tồn tại bởi bất kỳ chủ thể nào thuộc `Organization` khác trên cùng `Instance`.
- **Ngoại lệ:** Chỉ `Operator` cấp hệ thống khi thực hiện bảo trì hạ tầng mới thấy trạng thái vận hành tổng quát của máy chủ (nhưng mọi thao tác can thiệp trực tiếp vào phạm vi một `Organization` đều bắt buộc ghi nhật ký kiểm toán của `Organization` đó).
- **Testable Criteria sơ bộ:**
  - *Given:* `Khóa ảo K_A` thuộc `Org A` và `Tuyến R_B` thuộc `Org B`.
  - *When:* `API Client` dùng `K_A` gửi yêu cầu chỉ định `R_B` (hoặc tra cứu mã truy vết của `Org B`).
  - *Then:* Hệ thống từ chối yêu cầu với mã lỗi không tiết lộ thông tin (tương đương không tìm thấy trong phạm vi của `Org A`), không thực thi bất kỳ logic nào trên tài nguyên của `Org B` và ghi một bản ghi cảnh báo bảo mật.

#### `TP-BR-0102` — Chia sẻ Một chiều Có Kiểm soát Kết nối Nhà cung cấp (`Controlled Downward Connection Sharing`)
- **Owner:** `Organization Owner` | **Impact:** `High` | **Source / Status:** `PROPOSED`
- **Quy tắc:** Một `Provider Connection` do cấp `Organization` sở hữu có thể được cấp quyền *sử dụng* xuống một hoặc nhiều `Project` con bên trong cùng `Organization` đó, nhưng các `Team Admin` và `Developer` của `Project` thụ hưởng không bao giờ được đọc, xuất hoặc sửa đổi `Secret Credential` gốc gắn với kết nối đó. Kết nối do riêng một `Project` tự tạo không được chia sẻ ngang sang `Project` khác nếu không chuyển quyền sở hữu lên cấp `Organization`.
- **Ngoại lệ:** Không có ngoại lệ đối với việc xuất ngược chuỗi bí mật thô ra giao diện người dùng sau khi đã lưu.
- **Testable Criteria sơ bộ:**
  - *Given:* `Org Owner` chia sẻ `Connection C_Org` cho `Project P1`.
  - *When:* `Team Admin` của `P1` lập `Route` trỏ vào `Model Deployment` của `C_Org`, sau đó gọi giao diện xem chi tiết `C_Org`.
  - *Then:* Việc tạo `Route` và gọi suy luận thành công, nhưng trường thông tin xác thực chỉ hiển thị định danh ẩn danh/trạng thái hợp lệ, không trả về giá trị khóa gốc và từ chối quyền sửa khóa gốc.

#### `TP-BR-0103` — Ràng buộc Đơn Môi trường cho Khóa Ảo (`Single-Environment Virtual Key Scope`)
- **Owner:** `Team Admin` | **Impact:** `Medium-High` | **Source / Status:** `PROPOSED`
- **Quy tắc:** Mỗi `Virtual Key` chỉ được gắn với duy nhất một `Environment` (ví dụ `development`, `staging` hoặc `production`) thuộc duy nhất một `Project` để ngăn chặn sự cố kịch bản thử nghiệm làm cạn kiệt hạn mức hoặc làm bẩn dữ liệu giám sát của môi trường sản xuất thật.
- **Ngoại lệ:** Chế độ cá nhân đơn giản (`Personal Mode`) sử dụng một môi trường mặc định duy nhất nhưng khóa ảo vẫn gắn tường minh vào môi trường mặc định đó.
- **Testable Criteria sơ bộ:**
  - *Given:* `Virtual Key K_Dev` được phát hành trong môi trường `development`.
  - *When:* Dùng `K_Dev` gọi tới một `Route` hoặc `MCP Tool` bị giới hạn chỉ dành cho môi trường `production`.
  - *Then:* Yêu cầu bị từ chối do lệch phạm vi môi trường.

#### `TP-BR-0104` — Bảo toàn Ranh giới Độc lập giữa 4 Lớp Nghiệp vụ (`Four-Layer Domain Separation`)
- **Owner:** `Product Owner` & `Product Architect` | **Impact:** `Critical` (Kiến trúc nghiệp vụ gốc) | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-003`, `REJ-004`)
- **Quy tắc:** `Provider` (suy luận mô hình), `Plugin/Extension` (đơn vị đóng gói mở rộng), `MCP` (công cụ/tài nguyên ngữ cảnh) và `A2A` (ủy quyền liên tác nhân) phải có danh mục thực thể, hợp đồng dữ liệu, phạm vi quyền hạn và chính sách kiểm soát độc lập; việc cấp quyền gọi một mô hình không bao giờ tự động cấp quyền gọi công cụ MCP hay tác nhân A2A và ngược lại.
- **Ngoại lệ:** Không có ngoại lệ (`REJECTED` mọi thiết kế gộp chung).
- **Testable Criteria sơ bộ:**
  - *Given:* Một `Virtual Key` chỉ được cấp quyền gọi suy luận mô hình thuộc nhóm `Provider`.
  - *When:* Cùng khóa đó gửi yêu cầu liệt kê hoặc thực thi công cụ trên mặt phẳng `MCP` hoặc gửi tác vụ trên mặt phẳng `A2A`.
  - *Then:* Yêu cầu bị từ chối với lỗi thiếu quyền trên mặt phẳng tương ứng.

---

### Nhóm 02: Phân loại Nguồn gốc, Độ tin cậy Nhà phát triển & Sự Đồng thuận (`Classification, Publisher Trust & Consent`)

#### `TP-BR-0201` — Phân cấp Minh bạch Ba Mức Tin cậy Phần mở rộng (`Three-Tier Extension Provenance`)
- **Owner:** `Product Owner` & `Reviewer` | **Impact:** `High` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-004`) & `PROPOSED` (chi tiết 3 nhãn)
- **Quy tắc:** Mọi phần mở rộng trong hệ thống bắt buộc hiển thị một và chỉ một nhãn nguồn gốc không thể làm giả:
  1. `official`: Do dự án Tanpopo chính thức phát triển, ký xác nhận và duy trì dựa trên API/phương thức chính thức của nhà cung cấp.
  2. `verified`: Do bên thứ ba phát triển, đã vượt qua thẩm định tuân thủ điều khoản và bộ kiểm thử tương thích tự động trên danh mục chính thức của Tanpopo.
  3. `unverified-external`: Mọi gói cài đặt trực tiếp từ Git, URL, tệp tải lên hoặc kho riêng chưa qua chứng nhận của dự án Tanpopo.
- **Ngoại lệ:** Tổ chức tự vận hành có thể thêm nhãn phê duyệt nội bộ riêng (`Org-Approved`), nhưng không được ghi đè thành nhãn `official` của Tanpopo.
- **Testable Criteria sơ bộ:**
  - *Given:* Một gói phần mở rộng được `Operator` tải lên từ tệp nén nội bộ mà trong tệp khai báo tự ghi thuộc tính nguồn gốc là `"official"` nhưng không có chữ ký phát hành hợp lệ của Tanpopo.
  - *When:* Hệ thống kiểm tra gói cài đặt.
  - *Then:* Hệ thống bác bỏ tuyên bố giả mạo (hoặc hạ nhãn cưỡng bức xuống `unverified-external` kèm cảnh báo giả mạo nguồn gốc).

#### `TP-BR-0202` — Kiểm soát Chính sách Nguồn Cài đặt Cấp Tổ chức (`Organization Plugin Trust Policy`)
- **Owner:** `Organization Owner` | **Impact:** `High` | **Source / Status:** `PROPOSED`
- **Quy tắc:** Mỗi `Organization` có quyền cấu hình ngưỡng tin cậy tối thiểu đối với phần mở rộng được phép phục vụ lưu lượng của mình (ví dụ: *"Chỉ cho phép `official`"* hoặc *"Cho phép `official` + `verified`"* hoặc *"Cho phép thêm danh sách gói nội bộ đã duyệt cụ thể"*).
- **Ngoại lệ:** Mặc định khi tạo tổ chức sản xuất mới, các gói `unverified-external` không tự động khả dụng cho các `Project` nếu `Organization Owner` chưa bật cho phép.
- **Testable Criteria sơ bộ:**
  - *Given:* `Org A` đặt chính sách chỉ chấp nhận `official` và `verified`; trên máy chủ có cài sẵn một gói `Adapter X` mang nhãn `unverified-external`.
  - *When:* `Team Admin` của `Org A` cố tạo `Provider Connection` dùng `Adapter X`.
  - *Then:* Thao tác bị chặn bởi chính sách tin cậy của tổ chức.

#### `TP-BR-0203` — Cổng Phê duyệt Quyền Tường minh Trước khi Kích hoạt (`Explicit Informed Consent Gate`)
- **Owner:** `Operator` & `Organization Owner` | **Impact:** `Critical` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-002`)
- **Quy tắc:** Không một phần mở rộng nào được chuyển sang trạng thái hoạt động nếu người có thẩm quyền chưa xem và chấp thuận tường minh bảng kê khai toàn bộ quyền hạn mà gói đó yêu cầu (danh sách địa chỉ mạng đích được phép gọi, loại thông tin xác thực được phép sử dụng, phạm vi dữ liệu đi qua).
- **Ngoại lệ:** Chế độ tự động hóa hạ tầng bằng tệp khai báo (GitOps) chỉ được bỏ qua hộp thoại tương tác tay khi tệp khóa cấu hình do quản trị viên ký/duyệt đã ghi rõ danh sách quyền và mã băm chính xác của phiên bản gói đó.
- **Testable Criteria sơ bộ:**
  - *Given:* Gói `Plugin P` khai báo cần kết nối tới tên miền `api.example.com`.
  - *When:* Lệnh cài đặt được gọi mà chưa có bước xác nhận quyền tương tác hoặc chưa khớp bản ghi phê duyệt quyền trong tệp khóa.
  - *Then:* Gói dừng ở trạng thái `PENDING_PERMISSION_APPROVAL` và không thể xử lý bất kỳ lưu lượng nào.

---

### Nhóm 03: Quyền Sử dụng Hợp pháp & Giới hạn Thuê bao Tiêu dùng (`Entitlement & Subscription Restrictions`)

#### `TP-BR-0301` — Không Đánh đồng Sự Đồng ý Chấp nhận Rủi ro với Tính Hợp pháp Điều khoản (`No Legalization by Disclaimer`)
- **Owner:** `Product Owner` | **Impact:** `Critical` (Pháp lý & Định vị cốt lõi) | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-004`, `REJ-003`)
- **Quy tắc:** Sự tồn tại của một plugin bên thứ ba hoặc hành động người dùng đánh dấu vào ô *"Tôi hiểu và tự chịu rủi ro"* hoàn toàn **không** biến một phương thức kết nối vi phạm điều khoản nhà cung cấp thành hợp lệ trên phương diện sản phẩm của Tanpopo. Hệ thống tuyệt đối không thiết kế luồng "bấm chấp nhận rủi ro để mở khóa chế độ biến thuê bao cá nhân thành API".
- **Ngoại lệ:** Không có ngoại lệ.
- **Testable Criteria sơ bộ:**
  - *Given:* Toàn bộ giao diện quản trị, Setup Wizard, tài liệu hướng dẫn và danh mục chính thức của Tanpopo.
  - *When:* Rà soát toàn bộ luồng cấu hình kết nối và thông điệp sản phẩm.
  - *Then:* Không tồn tại bất kỳ tùy chọn, mẫu cấu hình hay lời hứa nào cho phép chuyển đổi gói thuê bao tiêu dùng cá nhân thành hạn mức API thông qua hộp thoại miễn trừ trách nhiệm.

#### `TP-BR-0302` — Cấm Tuyệt đối Hứa hẹn hoặc Duy trì Connector Lách Gói Thuê bao Tiêu dùng (`Zero Consumer Subscription Arbitrage`)
- **Owner:** `Product Owner` | **Impact:** `Critical` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-004`, `REJ-003`)
- **Quy tắc:** Tanpopo không phát triển, không đóng gói kèm, không liệt kê trên Marketplace chính thức và không bao giờ tuyên bố rằng các gói thuê bao tiêu dùng cá nhân trả phí cố định (như `ChatGPT Plus/Pro`, `Codex` cá nhân, `Claude Pro/Max`, `Google AI Pro/Ultra/Antigravity`...) tương đương với tín dụng API miễn phí hoặc có thể gom lại làm API dùng chung cho ứng dụng/nhóm.
- **Ngoại lệ:** Nếu chính nhà cung cấp mô hình ban hành chính thức một chương trình cấp quyền API/OAuth hợp pháp đi kèm gói doanh nghiệp hoặc nhà phát triển có tài liệu điều khoản công khai cho phép tích hợp cổng bên thứ ba, phương thức đó mới được xét duyệt theo quy trình chính thống (`TP-BR-0303`).
- **Testable Criteria sơ bộ:**
  - *Given:* Một `Extension Publisher` gửi yêu cầu đưa lên Marketplace một gói adapter sử dụng khóa phiên trình duyệt hoặc giả lập định danh ứng dụng đóng của gói thuê bao cá nhân.
  - *When:* `Reviewer` thực hiện kiểm định tuân thủ.
  - *Then:* Yêu cầu bị từ chối dứt khoát và mã gói đó không được cấp nhãn `official` hay `verified`.

#### `TP-BR-0303` — Điều kiện Hợp lệ của Kết nối OAuth và Định danh Liên kết (`Authorized OAuth Legitimacy`)
- **Owner:** `Organization Owner` & `Reviewer` | **Impact:** `High` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-004`)
- **Quy tắc:** Các kết nối sử dụng OAuth hoặc định danh liên kết trên kênh chính thức phải sử dụng mã định danh ứng dụng (`client_id` do tổ chức tự đăng ký hợp lệ hoặc được nhà cung cấp cấp phép công khai) cùng phạm vi quyền (`scopes`) đúng mục đích công bố; không được nhúng lén mã định danh nội bộ rút trích trái phép từ phần mềm đóng của bên thứ ba.
- **Ngoại lệ:** Các hệ thống cổng nội bộ tự chủ hoàn toàn của chính doanh nghiệp tự vận hành (do doanh nghiệp đó làm chủ cả hai đầu xác thực).
- **Testable Criteria sơ bộ:**
  - *Given:* Cấu hình một kết nối `Authorized OAuth` trên `Official Adapter`.
  - *When:* Hệ thống khởi tạo yêu cầu cấp quyền tới `External IdP`.
  - *Then:* Yêu cầu sử dụng đúng tham số ứng dụng được cấu hình minh bạch và kiểm tra khớp tập quyền trả về trước khi chuyển kết nối sang `ACTIVE`.

#### `TP-BR-0304` — Cô lập Trạng thái Hết hạn hoặc Thu hồi Thông tin Xác thực (`Expired Credential Containment`)
- **Owner:** `Operator` & `Team Admin` | **Impact:** `High` | **Source / Status:** `PROPOSED` (Từ `WF-05`, `SM-02`, `SM-03`)
- **Quy tắc:** Khi thông tin xác thực của một `Provider Connection` hết hạn không thể làm mới tự động hoặc bị nhà cung cấp thu hồi, hệ thống phải: (1) chuyển ngay kết nối sang trạng thái cần xác thực lại để ngưng gửi yêu cầu mới vô ích làm khóa tài khoản, (2) chỉ thực hiện tối đa một luồng làm mới đại diện tại một thời điểm cho mỗi kết nối, và (3) trả mã lỗi chuẩn hóa an toàn về cho `API Client` mà không bao giờ đính kèm chuỗi khóa/token hỏng của nhà cung cấp trong thông báo lỗi.
- **Ngoại lệ:** Không có ngoại lệ đối với quy tắc che giấu bí mật trong phản hồi lỗi.
- **Testable Criteria sơ bộ:**
  - *Given:* `Provider Connection C1` có mã làm mới OAuth đã bị thu hồi phía nhà cung cấp và nhận 50 yêu cầu đồng thời.
  - *When:* Token truy cập chạm ngưỡng hết hạn.
  - *Then:* Chỉ có một lệnh làm mới được gửi đi; ngay khi nhận phản hồi từ chối quyền, `C1` chuyển sang `AUTH_EXPIRED_REAUTH_REQUIRED`, phát cảnh báo quản trị và không một phản hồi nào trả về cho `API Client` chứa giá trị token hay khóa gốc.

---

### Nhóm 04: Ranh giới Quyền hạn & Kiềm tỏa Plugin (`Permission Boundaries & Untrusted Plugin Containment`)

#### `TP-BR-0401` — Nguyên tắc Đặc quyền Tối thiểu Mặc định Đóng cho Phần mở rộng (`Default-Deny Plugin Permissions`)
- **Owner:** `Operator` & `Security Reviewer` | **Impact:** `Critical` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-002`, `C-02`)
- **Quy tắc:** Một phần mở rộng khi thực thi mặc định không có quyền kết nối mạng ra ngoài, không có quyền đọc tệp hệ thống chủ, không có quyền đọc biến môi trường máy chủ và không có quyền truy cập thông tin xác thực của bất kỳ kết nối nào khác ngoài những gì đã được khai báo trong `Extension Manifest` và được quản trị viên phê duyệt cho đúng lần gọi đó.
- **Ngoại lệ:** Không có ngoại lệ.
- **Testable Criteria sơ bộ:**
  - *Given:* `Plugin P` chỉ được phê duyệt quyền gọi mạng tới `api.vendor-a.example`.
  - *When:* Trong lúc xử lý yêu cầu, `Plugin P` cố mở kết nối tới `collector.unknown.example` hoặc đọc cấu hình của `Connection Vendor B`.
  - *Then:* Hành động bị chặn tức khắc trước khi gói tin rời khỏi hệ thống và phát sinh sự kiện vi phạm bảo mật.

#### `TP-BR-0402` — Biên giới Tiếp xúc Bí mật Xác thực (`Zero Unnecessary Raw Secret Exposure`)
- **Owner:** `Operator` | **Impact:** `Critical` | **Source / Status:** `PROPOSED` (`IDEA-01`, `C-02`)
- **Quy tắc:** Đối với các phương thức xác thực tiêu chuẩn (gắn khóa vào tiêu đề hoặc tham số chuẩn), hệ thống chủ phải tự thực hiện đính kèm thông tin xác thực khi gửi tới đúng địa chỉ mạng đích đã phê duyệt mà không giao chuỗi bí mật gốc cho phần mở rộng bên thứ ba nắm giữ. Chỉ khi phương thức xác thực tùy biến bắt buộc phải tính toán chữ ký động bên trong phần mở rộng thì quyền tiếp cận khóa của riêng kết nối đó mới được yêu cầu tường minh và cảnh báo mức rủi ro cao khi phê duyệt.
- **Ngoại lệ:** Các phần mở rộng dùng cơ chế xác thực tùy biến đã được quản trị viên phê duyệt quyền tiếp cận bí mật cục bộ của riêng kết nối chỉ định.
- **Testable Criteria sơ bộ:**
  - *Given:* Một `Community Adapter` dùng xác thực khóa API tiêu chuẩn.
  - *When:* Thực hiện truy vấn qua kết nối gắn với adapter đó và kiểm tra toàn bộ dữ liệu đầu vào truyền cho phần mở rộng.
  - *Then:* Phần mở rộng không nhận được chuỗi giá trị thật của API key, nhưng nhà cung cấp đích hợp lệ vẫn nhận đủ tiêu đề xác thực chính xác.

#### `TP-BR-0403` — Chặn Tự động Kích hoạt khi Bản Cập nhật Đòi Thêm Quyền (`Permission Escalation Block on Update`)
- **Owner:** `Organization Owner` & `Operator` | **Impact:** `Critical` | **Source / Status:** `PROPOSED` (`WF-11`, `SM-01`)
- **Quy tắc:** Mọi quy trình nâng cấp phần mở rộng (dù thủ công hay theo lịch tự động) phải so sánh tập quyền của phiên bản mới với tập quyền đã được phê duyệt ở phiên bản hiện hành. Nếu phiên bản mới yêu cầu bất kỳ quyền mới hoặc mở rộng phạm vi nào chưa từng được duyệt, phiên bản mới tuyệt đối không được tự động nhận lưu lượng mà phải dừng chờ phê duyệt lại (`UPDATING_PENDING_RECONSENT`).
- **Ngoại lệ:** Nếu phiên bản mới thu hẹp bớt quyền (yêu cầu ít quyền hơn bản cũ) và tương thích hợp đồng, việc nâng cấp được phép diễn ra bình thường.
- **Testable Criteria sơ bộ:**
  - *Given:* `Plugin P@1.0.0` đang hoạt động với quyền gọi `a.example.com`; bản `P@1.1.0` thêm `b.example.com` vào manifest.
  - *When:* Chạy lệnh cập nhật tự động không kèm xác nhận quyền mới.
  - *Then:* Bản `1.1.0` bị giữ ở trạng thái `UPDATING_PENDING_RECONSENT`, lưu lượng hiện tại tiếp tục đi qua `1.0.0` không gián đoạn và quản trị viên nhận thông báo yêu cầu duyệt quyền mới.

#### `TP-BR-0404` — Cô lập Lỗi và Cách ly Khẩn cấp Phần mở rộng (`Plugin Fault Isolation & Quarantine`)
- **Owner:** `Operator` | **Impact:** `High` | **Source / Status:** `CONFIRMED_BY_OWNER` (`C-02`) & `PROPOSED`
- **Quy tắc:** Sự cố treo, lặp vô hạn, tiêu thụ quá mức tài nguyên hoặc lỗi ngoại lệ bên trong một phần mở rộng chỉ được phép làm thất bại yêu cầu đơn lẻ đang gọi phần mở rộng đó, tuyệt đối không được làm sập hệ thống Tanpopo hay làm gián đoạn lưu lượng của các `Provider Connection` khác. Khi quản trị viên kích hoạt lệnh `Quarantine`, mọi lưu lượng qua phần mở rộng đó phải bị ngắt ngay lập tức trên toàn hệ thống.
- **Ngoại lệ:** Không có ngoại lệ.
- **Testable Criteria sơ bộ:**
  - *Given:* `Adapter X` bị lỗi treo vô hạn trong khi `Adapter Y` đang phục vụ bình thường.
  - *When:* Gửi đồng thời yêu cầu vào cả `Adapter X` và `Adapter Y`.
  - *Then:* Yêu cầu qua `Adapter X` bị ngắt đúng ngưỡng thời gian giới hạn và trả lỗi cô lập; yêu cầu qua `Adapter Y` hoàn thành bình thường với độ trễ không bị ảnh hưởng.

---

### Nhóm 05: Định tuyến, Vùng Dữ liệu, Luồng Streaming & Hành vi Khi Lỗi (`Routing, Residency, Streaming & Fail Posture`)

#### `TP-BR-0501` — Ràng buộc Cứng về Vùng Cư trú Dữ liệu (`Hard Data Residency Enforcement`)
- **Owner:** `Organization Owner` | **Impact:** `Critical` (Tuân thủ dữ liệu) | **Source / Status:** `PROPOSED` (`UC-05`, `WF-07`)
- **Quy tắc:** Khi một `Organization`, `Project`, `Route` hoặc yêu cầu truy vấn có gắn chính sách giới hạn vùng địa lý xử lý dữ liệu (ví dụ chỉ cho phép vùng `EU` hoặc chỉ mạng nội bộ `On-Premise`), bộ định tuyến bắt buộc loại bỏ mọi `Model Deployment` ngoài vùng cho phép ở cả lượt chọn chính lẫn mọi lượt chuyển mạch dự phòng (`Failover`).
- **Ngoại lệ:** Không bao giờ được tự động phá rào vùng dữ liệu để cứu tỷ lệ thành công kỹ thuật.
- **Testable Criteria sơ bộ:**
  - *Given:* `Route R1` gắn chính sách `Data Region = EU`, gồm `Deployment D_EU` (đang sập `503`) và `Deployment D_US` (đang khỏe mạnh).
  - *When:* Gửi yêu cầu vào `Route R1`.
  - *Then:* Hệ thống từ chối chuyển sang `D_US` và trả lỗi `503 no_compliant_deployment_available`.

#### `TP-BR-0502` — Quy tắc Phân định Chế độ `Fail-Closed` và `Fail-Open` (`Failure Posture Matrix`)
- **Owner:** `Organization Owner` & `Product Architect` | **Impact:** `Critical` | **Source / Status:** `PROPOSED`
- **Quy tắc:**
  1. **Bắt buộc luôn `Fail-Closed` (không cho phép cấu hình bỏ qua):** Xác thực danh tính/khóa ảo, cô lập chéo tổ chức, quyền hạn của Plugin, ràng buộc vùng cư trú dữ liệu, phê duyệt công cụ `destructive`, và trần ngân sách cứng (`Hard Budget Cap`).
  2. **Cho phép cấu hình `Fail-Open` kèm cảnh báo kiểm toán (do `Organization Owner` quyết định):** Các bước làm giàu thông tin phụ trợ không trọng yếu hoặc xuất dữ liệu giám sát thứ cấp ra bên ngoài (khi hệ thống thu thập log bên ngoài tạm thời gián đoạn, lưu lượng kinh doanh chính vẫn được phép chạy nhưng phải đếm số bản ghi bị rớt).
- **Ngoại lệ:** Không cho phép bật `Fail-Open` trên bất kỳ hạng mục nào thuộc nhóm (1).
- **Testable Criteria sơ bộ:**
  - *Given:* Thành phần kiểm tra ngân sách cứng hoặc xác thực khóa không thể xác minh quyền hợp lệ của một yêu cầu.
  - *When:* Yêu cầu đi tới cổng.
  - *Then:* Yêu cầu bị từ chối an toàn (`Fail-Closed`), không được lọt qua nhà cung cấp đích.

#### `TP-BR-0503` — Nghiêm cấm Chuyển mạch Dự phòng Ngầm sau khi Đã Phát Luồng Một phần (`No Silent Mid-Stream Fallback`)
- **Owner:** `Product Architect` | **Impact:** `High` (Tính toàn vẹn dữ liệu ứng dụng) | **Source / Status:** `PROPOSED` (`UC-01`, `WF-06`, `WF-07`)
- **Quy tắc:** Chuyển mạch tự động sang `Model Deployment` dự phòng chỉ được phép thực hiện **trước khi** mảnh dữ liệu nội dung hoặc lệnh gọi công cụ đầu tiên của phản hồi được gửi về cho `API Client`. Một khi đã phát ít nhất một mảnh nội dung (`Partial Stream`), nếu nhà cung cấp đích gặp lỗi hoặc đứt kết nối, hệ thống bắt buộc đóng luồng bằng sự kiện báo lỗi tường minh thay vì âm thầm gọi lại mô hình khác ghép nối vào luồng đang dở.
- **Ngoại lệ:** Các thông điệp giữ kết nối rỗng (keep-alive heartbeat) chưa mang dữ liệu nghiệp vụ thực tế có thể cấu hình không tính là đã phát nội dung nếu giao thức cho phép rõ ràng.
- **Testable Criteria sơ bộ:**
  - *Given:* `Route` có `D1` (chính) và `D2` (dự phòng); `D1` trả `200 OK`, phát được 20 từ đầu tiên rồi ngắt kết nối đột ngột.
  - *When:* `API Client` đang đọc luồng phản hồi.
  - *Then:* `D2` hoàn toàn không bị gọi; `API Client` nhận thông báo lỗi ngắt luồng giữa chừng chuẩn xác và phần token đã phát từ `D1` được đưa vào bước quyết toán ước tính.

#### `TP-BR-0504` — Kiểm tra Tương thích Năng lực Tường minh Trước khi Gọi (`Strict Capability Verification`)
- **Owner:** `Product Architect` | **Impact:** `Medium-High` | **Source / Status:** `PROPOSED` (`UC-01`)
- **Quy tắc:** Khi yêu cầu của người gọi đòi hỏi các năng lực cốt lõi có ảnh hưởng trực tiếp tới tính đúng đắn đầu ra (ví dụ: ép cấu trúc `json_schema` nghiêm ngặt, gọi công cụ, xử lý hình ảnh/âm thanh), hệ thống không được âm thầm cắt bỏ tham số đó để gửi bừa tới một mô hình không hỗ trợ mà phải lọc chọn đích có đủ năng lực hoặc từ chối ngay với mã lỗi tường minh.
- **Ngoại lệ:** Quản trị viên cấu hình rõ chế độ cho phép biến đổi tương đương trong `Route` cho các tham số gợi ý không trọng yếu.
- **Testable Criteria sơ bộ:**
  - *Given:* Một tuyến chỉ chứa mô hình thuần văn bản không hỗ trợ đọc ảnh (`vision = false`).
  - *When:* `API Client` gửi yêu cầu có đính kèm hình ảnh đầu vào.
  - *Then:* Hệ thống trả ngay lỗi `400 capability_not_supported` mà không tốn tiền gọi ra nhà cung cấp ngoài.

#### `TP-BR-0505` — Thử lại An toàn và Lan truyền Hủy Yêu cầu (`Safe Retry & Cancellation Propagation`)
- **Owner:** `Product Architect` | **Impact:** `High` | **Source / Status:** `PROPOSED`
- **Quy tắc:** Hệ thống chỉ tự động thử lại các lỗi tạm thời (mạng lỗi kết nối, quá tải `429`/`503` trước khi nhận nội dung) trong số lần giới hạn tối đa của chính sách tuyến. Khi `API Client` chủ động hủy yêu cầu đang chạy, hệ thống phải hủy ngay yêu cầu tương ứng đang gửi tới nhà cung cấp đích để dừng phát sinh thêm chi phí.
- **Ngoại lệ:** Các lỗi phía dữ liệu đầu vào (`400`, `401`, `403`, `422`) không bao giờ được tự động thử lại.
- **Testable Criteria sơ bộ:**
  - *Given:* Một yêu cầu sinh văn bản dài đang truyền luồng ở giây thứ 3.
  - *When:* `API Client` đóng kết nối hủy yêu cầu.
  - *Then:* Kết nối phía nhà cung cấp đích bị đóng ngay lập tức (không chạy tiếp tới hết bài văn 60 giây) và khoản giữ chỗ ngân sách được quyết toán ngay.

---

### Nhóm 06: Quản trị Công cụ MCP, Phê duyệt & Liên Tác nhân A2A (`MCP, Approvals & A2A`)

#### `TP-BR-0601` — Phân quyền Cấp Công cụ & Khóa Dấu vân tay Cấu trúc MCP (`MCP Tool Scoping & Schema Pinning`)
- **Owner:** `Organization Owner` & `Team Admin` | **Impact:** `Critical` | **Source / Status:** `PROPOSED` (`UC-06`, `IDEA-03`)
- **Quy tắc:** `MCP Client` chỉ nhìn thấy và gọi được đúng danh sách các công cụ đã được cấp quyền cho `Virtual Key` / chủ thể đó. Mọi công cụ được phép sử dụng trong môi trường kiểm soát chặt phải được ghi nhận dấu vân tay mô tả và cấu trúc tham số (`schema_hash`) tại lúc phê duyệt; nếu `MCP Server` đích tự ý thay đổi mô tả/tham số làm lệch dấu vân tay, công cụ đó phải tự động bị tạm ngưng phơi bày cho tới khi quản trị viên duyệt lại.
- **Ngoại lệ:** Môi trường phát triển cục bộ (`development`) có thể bật chế độ cảnh báo thay vì chặn cứng khi tác giả đang chủ động sửa code công cụ của chính mình.
- **Testable Criteria sơ bộ:**
  - *Given:* Công cụ `read_customer` đã được duyệt với dấu vân tay `H1` trong môi trường `production`.
  - *When:* `MCP Server` cập nhật phần mô tả văn bản của `read_customer` (dấu vân tay đổi thành `H2`) mà chưa qua quản trị viên duyệt lại.
  - *Then:* Hệ thống tạm khóa việc gọi `read_customer` và phát cảnh báo thay đổi cấu trúc công cụ.

#### `TP-BR-0602` — Bắt buộc Phê duyệt Con người Gắn Tham số Bất biến cho Công cụ Phá hủy (`Destructive Tool Approval Binding`)
- **Owner:** `Organization Owner` & `Reviewer` | **Impact:** `Critical` | **Source / Status:** `PROPOSED` (`WF-09`, `SM-06`)
- **Quy tắc:** Mọi lệnh gọi công cụ MCP được phân loại rủi ro là `destructive` (hoặc chạm ngưỡng rủi ro cao theo chính sách dự án) bắt buộc phải tạo `Approval Request` và chỉ được thực thi sau khi một `Reviewer` có thẩm quyền phê duyệt trong thời hạn hiệu lực. Quyết định phê duyệt bị khóa chặt vào đúng tên công cụ, chủ thể gọi và toàn bộ giá trị tham số đầu vào; bất kỳ thay đổi tham số nào cũng làm vô hiệu phê duyệt đó.
- **Ngoại lệ:** Không cho phép tác nhân tự phê duyệt yêu cầu của chính mình.
- **Testable Criteria sơ bộ:**
  - *Given:* `Reviewer` phê duyệt lệnh gọi `delete_record(id="123")`.
  - *When:* Tác nhân cố dùng mã phê duyệt đó để thực thi `delete_record(id="456")` hoặc thực thi sau khi yêu cầu phê duyệt đã hết hạn TTL.
  - *Then:* Lệnh thực thi bị từ chối hoàn toàn và ghi sự kiện vi phạm kiểm toán.

#### `TP-BR-0603` — Cấm Tự động Thử lại Lệnh Thực thi Công cụ Có Tác động Phụ (`No Blind Retry on Side-Effecting Tools`)
- **Owner:** `Product Architect` | **Impact:** `High` | **Source / Status:** `PROPOSED` (`WF-09`)
- **Quy tắc:** Đối với các công cụ MCP thuộc nhóm `mutating` hoặc `destructive` (và các tác vụ A2A không khai báo tính bất biến khi gọi lặp), nếu xảy ra đứt mạng hoặc quá thời gian chờ sau khi lệnh đã được gửi tới máy chủ đích, hệ thống **nghiêm cấm** tự động gửi lặp lại lệnh đó để tránh nhân đôi hậu quả thực tế, mà phải trả về trạng thái kết quả chưa xác định (`ambiguous_outcome`) kèm mã đối chiếu.
- **Ngoại lệ:** Chỉ các công cụ được xác nhận rõ là `read-only` không gây tác động phụ mới được phép áp dụng chính sách thử lại tự động khi lỗi mạng tạm thời.
- **Testable Criteria sơ bộ:**
  - *Given:* Công cụ `send_invoice_email` được phân loại là `mutating`.
  - *When:* `MCP Server` không phản hồi kịp trong thời gian chờ sau khi nhận lệnh.
  - *Then:* Hệ thống gửi đúng 1 lần duy nhất, không thử lại lần thứ 2 và trả mã lỗi cảnh báo cần kiểm tra trạng thái thực tế.

#### `TP-BR-0604` — Chống Khuếch đại Đặc quyền khi Ủy quyền Liên Tác nhân (`No A2A Privilege Amplification`)
- **Owner:** `Organization Owner` | **Impact:** `High` | **Source / Status:** `PROPOSED`
- **Quy tắc:** Khi một `Agent` ủy quyền tác vụ con cho một `Agent` khác qua mặt phẳng A2A, phạm vi quyền truy cập dữ liệu, thời gian sống tối đa và trần ngân sách cấp cho tác vụ con không bao giờ được vượt quá phần quyền và số dư ngân sách còn lại của tác vụ cha đã khởi xướng.
- **Ngoại lệ:** Không có ngoại lệ.
- **Testable Criteria sơ bộ:**
  - *Given:* Tác nhân cha chỉ còn ngân sách khả dụng `2 USD` và chỉ có quyền trên vùng dữ liệu `EU`.
  - *When:* Tác nhân cha khởi tạo tác vụ A2A yêu cầu hạn mức `10 USD` hoặc gọi tác nhân đích ngoài vùng `EU`.
  - *Then:* Mặt phẳng A2A từ chối khởi tạo tác vụ ủy quyền vượt quyền.

---

### Nhóm 07: Báo cáo Sử dụng, Ngân sách & Đối soát Chi phí (`Usage, Budgets & Chargeback`)

#### `TP-BR-0701` — Ràng buộc Bất biến Trần Ngân sách Phân cấp (`Hierarchical Budget Ceiling`)
- **Owner:** `Organization Owner` & `Team Admin` | **Impact:** `High` | **Source / Status:** `PROPOSED` (`UC-08`, `WF-08`)
- **Quy tắc:** Một yêu cầu phát sinh chi phí chỉ được phép thông qua khi đồng thời còn đủ hạn mức khả dụng tại tất cả các cấp áp dụng trên đường đi của nó (`Virtual Key` → `Environment` → `Project` → `Organization`). Dù `Virtual Key` vẫn còn hạn mức riêng nhưng ngân sách tổng của `Project` hoặc `Organization` đã chạm trần cứng, yêu cầu bắt buộc bị chặn.
- **Ngoại lệ:** Các mô hình tự vận hành nội bộ được quản trị viên cấu hình biểu giá bằng `0` (hoặc nằm trong danh sách miễn trần ngân sách tiền mặt khi hạ cấp khẩn cấp).
- **Testable Criteria sơ bộ:**
  - *Given:* `Virtual Key K1` có trần `50 USD` (mới dùng `10 USD`), nhưng `Project P1` chứa `K1` có trần `100 USD` và tổng các khóa trong `P1` đã dùng đủ `100 USD`.
  - *When:* `K1` gửi yêu cầu mới tới mô hình trả phí.
  - *Then:* Yêu cầu bị từ chối với lỗi `429 project_budget_exceeded`.

#### `TP-BR-0702` — Giữ chỗ Ngân sách Trước khi Thực thi (`Mandatory Pre-Execution Budget Hold`)
- **Owner:** `Organization Owner` | **Impact:** `High` | **Source / Status:** `PROPOSED` (`UC-08`, `SM-07`)
- **Quy tắc:** Đối với mọi tài khoản có gắn trần ngân sách cứng, hệ thống phải khấu trừ tạm thời một khoản dự phòng ước tính vào số dư khả dụng **trước khi** chuyển tiếp yêu cầu ra nhà cung cấp ngoài, nhằm ngăn chặn hàng loạt yêu cầu song song cùng lúc vượt qua cửa kiểm tra khi số dư sắp cạn.
- **Ngoại lệ:** Các yêu cầu bị từ chối ngay ở bước xác thực/chính sách phải giải phóng tức thì khoản giữ chỗ nếu có (`RELEASED_UNUSED`).
- **Testable Criteria sơ bộ:**
  - *Given:* Ngân sách cứng của dự án chỉ còn đúng `0.10 USD`; mỗi yêu cầu ước tính tốn tối đa `0.05 USD`.
  - *When:* Bắn đồng thời 20 yêu cầu song song vào cùng một thời điểm.
  - *Then:* Tối đa chỉ 2 yêu cầu được giữ chỗ và đi tiếp; 18 yêu cầu còn lại bị chặn ngay lập tức trước khi gọi ra nhà cung cấp ngoài.

#### `TP-BR-0703` — Minh bạch Độ chính xác Số đo trong Sổ cái và Báo cáo Phân bổ Chi phí (`Explicit Usage Accuracy Classification`)
- **Owner:** `Organization Owner` | **Impact:** `High` (Độ tin cậy tài chính) | **Source / Status:** `PROPOSED` (`WF-08`)
- **Quy tắc:** Mọi bản ghi tiêu thụ và bút toán chi phí bắt buộc gắn nhãn độ chính xác trung thực:
  - `EXACT`: Có số đếm xác nhận đầy đủ từ giao thức chuẩn của nhà cung cấp đích.
  - `ESTIMATED`: Được tính toán ước lượng bằng bộ quy tắc chuẩn hóa của hệ thống khi luồng đứt giữa chừng hoặc nhà cung cấp không trả số đếm.
  - `UNMETERED_FLAGGED`: Không thể xác định tự động số lượng tiêu thụ hoặc thiếu biểu giá; bắt buộc đánh dấu cần đối soát.
  Báo cáo tài chính và phân bổ chi phí (`Chargeback`) **cấm** ghi nhận ngầm các yêu cầu bị đứt luồng hoặc thiếu số đo thành `0 USD` hoàn hảo mà không tách bạch phần ước tính/cần đối soát.
- **Ngoại lệ:** Không có ngoại lệ.
- **Testable Criteria sơ bộ:**
  - *Given:* Một luồng phản hồi đã truyền được 500 từ thì mất kết nối với nhà cung cấp đích nên không nhận được bản tin tổng kết token cuối luồng.
  - *When:* Hệ thống đóng luồng và xuất báo cáo chi phí của dự án.
  - *Then:* Giao dịch được quyết toán với nhãn `ESTIMATED` lớn hơn `0` dựa trên dữ liệu đã đi qua và hiển thị rõ ở mục chi phí ước tính trong báo cáo.

---

### Nhóm 08: Đồng thời, Quyền Riêng tư Lưu giữ & Nhật ký Kiểm toán (`Concurrency, Retention & Audit`)

#### `TP-BR-0801` — Ngăn chặn Ghi đè Mù khi Cập nhật Cấu hình Đồng thời (`Optimistic Configuration Concurrency`)
- **Owner:** `Operator` & `Team Admin` | **Impact:** `Medium-High` | **Source / Status:** `PROPOSED` (`WF-11`)
- **Quy tắc:** Khi nhiều quản trị viên hoặc tiến trình tự động cùng cập nhật một thực thể quản trị nhạy cảm (`Route`, `Policy`, `Budget`, `Extension Installation`), hệ thống chỉ chấp nhận cập nhật khớp với phiên bản hiện hành mới nhất và từ chối thao tác dựa trên dữ liệu cũ nhằm ngăn chặn việc vô tình xóa mất quy tắc bảo mật vừa được người khác thêm vào.
- **Ngoại lệ:** Lệnh ngắt khẩn cấp (`Revoke` / `Quarantine` / `Suspend`) luôn được ưu tiên thực thi ngay lập tức để bảo vệ an toàn hệ thống.
- **Testable Criteria sơ bộ:**
  - *Given:* Quản trị viên A và B cùng mở cấu hình `Policy P` ở phiên bản `v3`; A lưu thành công lên `v4`.
  - *When:* B bấm lưu thay đổi của mình vẫn dựa trên gốc `v3`.
  - *Then:* Hệ thống từ chối thao tác của B với thông báo xung đột phiên bản để B xem lại bản `v4` trước khi ghi đè.

#### `TP-BR-0802` — Quyền Riêng tư Mặc định & Toàn vẹn Quyền Xóa Dữ liệu (`Privacy-by-Default & Clean Deletion`)
- **Owner:** `Organization Owner` | **Impact:** `High` | **Source / Status:** `PROPOSED` (`WF-13`)
- **Quy tắc:** Ở cấu hình mặc định, hệ thống không lưu trữ lâu dài toàn văn nội dung câu hỏi/câu trả lời nhạy cảm của người dùng cuối (chỉ lưu siêu dữ liệu vận hành và số đo chi phí) trừ khi quản trị viên chủ động bật chính sách ghi nhật ký nội dung kèm thời hạn tự động xóa rõ ràng. Khi thực hiện xóa `Project` hoặc `Organization`, toàn bộ khóa bí mật và nội dung truy vấn đã lưu của đơn vị đó phải bị xóa không thể khôi phục sau khi đã đóng sạch các luồng đang chạy.
- **Ngoại lệ:** Các bản ghi tổng hợp bút toán sổ cái và sự kiện kiểm toán bảo mật không chứa nội dung prompt nhạy cảm được giữ lại theo thời hạn tuân thủ kiểm toán của tổ chức.
- **Testable Criteria sơ bộ:**
  - *Given:* Khởi tạo một `Project` mới với cấu hình mặc định và gửi một truy vấn chứa văn bản nhạy cảm.
  - *When:* Kiểm tra dữ liệu nhật ký lưu trữ bền vững của hệ thống.
  - *Then:* Chỉ thấy siêu dữ liệu (thời gian, mô hình, độ trễ, số token, chi phí, mã trạng thái), không lưu văn bản nội dung gốc của câu hỏi và câu trả lời.

#### `TP-BR-0803` — Nhật ký Kiểm toán Bất biến cho Mọi Hành động Quản trị và Bảo mật (`Immutable Security & Governance Audit`)
- **Owner:** `Organization Owner` & `Operator` | **Impact:** `Critical` | **Source / Status:** `PROPOSED`
- **Quy tắc:** Mọi hành động tạo/sửa/xoay vòng/thu hồi khóa, cài đặt/cấp quyền/nâng cấp/cách ly plugin, thay đổi chính sách/ngân sách, phê duyệt/từ chối công cụ nhạy cảm và mọi sự kiện chặn vi phạm bảo mật (truy cập chéo tổ chức, plugin vượt quyền mạng, lệch dấu vân tay công cụ) bắt buộc sinh bản ghi `Audit Event` bất biến (không quản trị viên nào được phép sửa hoặc xóa lẻ tẻ từng dòng nhật ký kiểm toán khi chưa hết hạn lưu giữ).
- **Ngoại lệ:** Không có ngoại lệ.
- **Testable Criteria sơ bộ:**
  - *Given:* Một `Team Admin` thu hồi `Virtual Key` hoặc một `Operator` cài đặt một `unverified-external` plugin.
  - *When:* Tra cứu sổ nhật ký kiểm toán và thử gửi yêu cầu sửa/xóa bản ghi kiểm toán đó.
  - *Then:* Bản ghi chứa đầy đủ chủ thể thực hiện, thời điểm, đối tượng tác động, nhãn nguồn gốc và hệ thống từ chối mọi thao tác chỉnh sửa hay xóa đơn lẻ bản ghi đó.
