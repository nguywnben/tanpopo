# Từ điển Thuật ngữ Nghiệp vụ & Sản phẩm Chuẩn (Canonical Product Glossary) — Tanpopo

> **Tài liệu Phân tích Nghiệp vụ (Phase ASTRA A02 — `DRAFT / UNLOCKED SCOPE`)**
> - **Phiên bản:** `0.1.0-a02-draft`
> - **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Lead Business Analyst & Product Architect
> - **Trạng thái quản trị:** **Bản nháp chưa khóa phạm vi (`DRAFT`)** do Cổng **Gate G1** tại [`docs/governance/decisions.md`](../governance/decisions.md) hiện đang ở trạng thái `READY_FOR_REVIEW` (chờ Product Owner phê duyệt chính thức). Các định nghĩa cốt lõi thuộc tầm nhìn gốc giữ nhãn `CONFIRMED_BY_OWNER`; các khái niệm mô hình hóa nghiệp vụ chi tiết mang nhãn `PROPOSED`.

---

## 1. Nguyên tắc Sử dụng Thuật ngữ Bất biến (`CONFIRMED_BY_OWNER`)

1. **Bốn lớp khái niệm độc lập (`DEC-003`):** `Provider`, `Plugin (Extension)`, `MCP` và `A2A` là bốn thực thể có bản chất, vòng đời và ranh giới phân quyền hoàn toàn khác nhau — nghiêm cấm dùng lẫn lộn làm từ đồng nghĩa trong tài liệu, giao diện hay API.
2. **Bốn thực thể tách rời trong chuỗi kết nối mô hình (`PROPOSED`):** `Provider Adapter` (gói phần mềm tích hợp) ≠ `Provider Connection` (liên kết tài khoản/xác thực cụ thể) ≠ `Model Deployment` (đích vận hành cụ thể của một model trên một kết nối/vùng) ≠ `Model` (đặc tả năng lực mô hình logic).
3. **Cơ chế kỹ thuật ≠ Quyền sử dụng hợp pháp (`DEC-004`):** Việc một phương thức xác thực hoặc một plugin *có thể chạy được về mặt kỹ thuật* (Technical Capability) hoàn toàn tách biệt với việc nhà cung cấp dịch vụ đích *có cấp quyền pháp lý/điều khoản* cho hành vi đó hay không (Provider Entitlement).

---

## 2. Nhóm Thuật ngữ Tác nhân (Actors)

| Thuật ngữ chuẩn | Định nghĩa nghiệp vụ trong Tanpopo |
|---|---|
| **Personal Admin** | Quản trị viên duy nhất trong mô hình tự triển khai cá nhân (single-user self-hosted), nắm toàn quyền vận hành máy chủ, cài đặt plugin, kết nối nhà cung cấp và quản lý ngân sách của chính mình. |
| **Organization Owner** | Chủ sở hữu cấp cao nhất của một Tổ chức (`Organization / Tenant`), chịu trách nhiệm cuối cùng về chính sách tuân thủ, hạn mức ngân sách tổng, quy tắc tin cậy plugin, lưu trữ/xóa dữ liệu và quyền ngắt khẩn cấp toàn tổ chức. |
| **Team Admin** | Quản trị viên cấp Dự án/Nhóm (`Project / Team`), được ủy quyền quản lý thành viên nhóm, khóa ảo (`Virtual Key`), tuyến định tuyến (`Route`), ngân sách nhóm và các kết nối trong phạm vi dự án được giao. |
| **Developer** | Kỹ sư xây dựng ứng dụng hoặc cấu hình tác nhân AI; sử dụng các kết nối, tuyến, công cụ MCP đã được cấp quyền thông qua khóa ảo, thử nghiệm trên Playground và tra cứu nhật ký lỗi/truy vết của dự án mình. |
| **API Client** | Phần mềm ứng dụng, dịch vụ phụ trợ (backend), IDE hoặc công cụ dòng lệnh gọi tự động vào các điểm cuối API của Tanpopo bằng thông tin định danh máy (`Virtual Key` hoặc định danh dịch vụ). |
| **Extension Publisher** | Cá nhân, nhà cung cấp AI đối tác, đội ngũ chính thức của Tanpopo hoặc nhóm hạ tầng nội bộ doanh nghiệp thực hiện đóng gói, khai báo quyền hạn và phát hành các phần mở rộng (`Extension Package`). |
| **Reviewer** | Tác nhân thẩm định độc lập ở hai ngữ cảnh: (1) *Marketplace Reviewer* kiểm duyệt tính tuân thủ và tương thích cho các gói công khai nhãn `verified`, và (2) *Internal Governance Approver* phê duyệt quyền hạn plugin mới hoặc hành động nhạy cảm (như gọi công cụ MCP có tính phá hủy) bên trong một tổ chức. |
| **Operator** | Quản trị viên vận hành hạ tầng hệ thống Tanpopo (`Instance Operator / SRE`), chịu trách nhiệm cài đặt, nâng cấp phiên bản, sao lưu, phục hồi sự cố, giám sát sức khỏe hệ thống và bảo vệ kho bí mật gốc. |
| **MCP Client** | Ứng dụng, IDE hoặc khung tác nhân đóng vai trò bên tiêu thụ chuẩn Model Context Protocol, gửi yêu cầu khám phá và gọi công cụ/tài nguyên/mẫu lệnh qua mặt phẳng MCP của Tanpopo. |
| **MCP Server** | Dịch vụ đích cung cấp năng lực thực thi công cụ (`Tools`), tài nguyên dữ liệu (`Resources`) hoặc mẫu nhắc (`Prompts`) theo chuẩn Model Context Protocol đứng phía sau Tanpopo. |
| **Agent** | Tác nhân phần mềm tự chủ hoặc bán tự chủ có khả năng lập kế hoạch nhiều bước (gọi kết hợp mô hình và công cụ MCP) hoặc tham gia ủy quyền/nhận ủy quyền tác vụ bất đồng bộ qua mặt phẳng A2A. |
| **External Identity Provider (IdP)** | Hệ thống xác thực bên ngoài (như OIDC/SAML doanh nghiệp hoặc máy chủ cấp quyền OAuth 2.1 chính thức) xác minh danh tính người dùng, dịch vụ hoặc cấp quyền ủy nhiệm hợp lệ. |
| **External AI Provider** | Tổ chức thương mại bên ngoài hoặc hệ thống máy chủ suy luận mô hình tự vận hành cung cấp dịch vụ mô hình AI kèm điều khoản dịch vụ, giới hạn tốc độ và biểu giá riêng. |

---

## 3. Nhóm Thuật ngữ Kiến trúc Nghiệp vụ & Chuỗi Kết nối

| Thuật ngữ chuẩn | Định nghĩa nghiệp vụ & Sự khác biệt cốt lõi |
|---|---|
| **Instance** | Một bản cài đặt hệ thống Tanpopo tự vận hành độc lập (có thể chứa một tổ chức duy nhất hoặc nhiều tổ chức tách biệt tùy cấu hình quản trị). |
| **Organization (Tenant)** | Đơn vị ranh giới cô lập dữ liệu, tài chính, khóa xác thực và kiểm toán cấp cao nhất dành cho một pháp nhân hoặc một khối độc lập. Dữ liệu và bí mật không bao giờ tự động chia sẻ xuyên qua hai `Organization`. |
| **Project (Team / Workspace)** | Đơn vị tổ chức nghiệp vụ nằm trong một `Organization`, gom nhóm các ứng dụng, thành viên, khóa ảo, tuyến định tuyến và hạn mức ngân sách của một sản phẩm hoặc phòng ban. |
| **Environment** | Phân vùng vòng đời vận hành bên trong một `Project` (điển hình: `development`, `staging`, `production`) với khóa ảo, chính sách an toàn và trần chi phí riêng biệt. |
| **Principal** | Chủ thể định danh được hệ thống nhận diện khi thực hiện hành động quản trị hoặc gọi API (có thể là người dùng thực, tài khoản dịch vụ hoặc tác nhân được định danh). |
| **Virtual Key** | Mã thông báo truy cập do Tanpopo phát hành cho `API Client` / `Developer` / `Agent`, gắn chặt với một `Environment`/`Project` cùng tập quyền hạn và hạn mức cụ thể; giúp che giấu hoàn toàn khóa thật của nhà cung cấp đích. |
| **Credential (Secret Binding)** | Thông tin bí mật dùng để chứng thực với hệ thống đích (API Key thương mại, bộ token OAuth được cấp phép, định danh đám mây IAM/OIDC, chứng chỉ mTLS) do Tanpopo bảo quản và kiểm soát mục đích sử dụng. |
| **Provider** | Thực thể dịch vụ cung cấp năng lực suy luận mô hình AI (văn bản, hình ảnh, âm thanh, embedding, rerank...). |
| **Provider Adapter** | Một loại `Extension` chứa quy tắc/mã chuyển đổi giao thức, ánh xạ danh mục mô hình, chuẩn hóa mã lỗi và trích xuất mức tiêu thụ cho một dòng `Provider` cụ thể. **Adapter là gói phần mềm tĩnh, không chứa khóa bí mật của khách hàng.** |
| **Provider Connection** | Một cấu hình kết nối nghiệp vụ sống động kết hợp một `Provider Adapter` với một `Credential` cụ thể và điểm cuối mạng (ví dụ: *"Tài khoản OpenAI Thương mại Nhóm Sản phẩm A"*). |
| **Model** | Khái niệm mô hình AI ở mức danh mục logic kèm hồ sơ năng lực (`Capability Profile`: độ dài ngữ cảnh, hỗ trợ gọi công cụ, hình ảnh, suy luận...), độc lập với việc mô hình đó được gọi qua nhà cung cấp đám mây nào. |
| **Model Deployment** | Một đích phục vụ mô hình cụ thể gắn với một `Provider Connection`, một vùng địa lý (`Data Region`), biểu giá và trạng thái sức khỏe riêng (ví dụ: cùng mô hình logic `Claude Sonnet` nhưng có 2 `Deployment` khác nhau tại `Bedrock EU` và `Anthropic US`). |
| **Route** | Quy tắc điều phối lưu lượng ánh xạ một tên gọi mô hình ảo (hoặc điều kiện truy vấn) tới danh sách ưu tiên các `Model Deployment` thỏa mãn năng lực và chính sách bảo mật. |
| **Policy** | Tập hợp các ràng buộc quản trị áp đặt trên lưu lượng hoặc hành động quản trị (phân quyền truy cập, vùng cư trú dữ liệu, giới hạn tốc độ, lọc công cụ MCP, phê duyệt thủ công, hành vi đóng/mở khi lỗi). |

---

## 4. Nhóm Thuật ngữ Hệ sinh thái Phần mở rộng (Plugins / Extensions)

| Thuật ngữ chuẩn | Định nghĩa nghiệp vụ |
|---|---|
| **Extension (Plugin)** | Đơn vị mở rộng đóng gói độc lập có hợp đồng (`Contract`), khai báo quyền (`Permissions`) và vòng đời quản trị rõ ràng để bổ sung `Provider Adapter`, bộ lọc chính sách, cơ chế xác thực hoặc bộ xuất giám sát mà không cần cập nhật nhân Tanpopo Core. |
| **Extension Manifest** | Bản khai báo siêu dữ liệu bất biến đi kèm mỗi phiên bản plugin, nêu rõ định danh, phiên bản hợp đồng tương thích, năng lực cung cấp và danh sách quyền tối thiểu yêu cầu (như tên miền mạng đích, loại thông tin xác thực). |
| **Trust Tier (Cấp độ tin cậy nguồn gốc)** | Nhãn phân loại nguồn gốc minh bạch của một phần mở rộng:<br>- `official`: Do dự án Tanpopo trực tiếp phát triển, kiểm định tuân thủ API chính thức và duy trì.<br>- `verified`: Do đối tác/cộng đồng phát triển, vượt qua kiểm thử tương thích và thẩm định điều khoản trên danh mục chính thức.<br>- `unverified-external`: Do người vận hành tự cài đặt từ Git/URL/tệp nén/kho riêng bên ngoài phạm vi bảo chứng của Tanpopo. |
| **Permission Escalation** | Tình huống phiên bản mới của một plugin yêu cầu thêm quyền hạn rộng hơn (thêm tên miền mạng mới, thêm quyền tiếp cận loại thông tin xác thực mới...) so với bản quyền đã được quản trị viên phê duyệt trước đó. |
| **Quarantine (Cách ly khẩn cấp)** | Trạng thái khóa cưỡng bức ngay lập tức quyền thực thi và kết nối mạng của một plugin đã cài đặt khi phát hiện lỗ hổng bảo mật, hành vi vượt quyền hoặc thu hồi khẩn cấp, đồng thời bảo toàn dấu vết phục vụ điều tra. |

---

## 5. Nhóm Thuật ngữ MCP, A2A, Tài chính (FinOps) & Tuân thủ

| Thuật ngữ chuẩn | Định nghĩa nghiệp vụ |
|---|---|
| **MCP Plane** | Mặt phẳng nghiệp vụ chuyên trách đăng ký `MCP Server`, lọc danh mục công cụ theo quyền người gọi, kiểm tra tính toàn vẹn mô tả công cụ, yêu cầu phê duyệt hành động nhạy cảm và ghi nhận nhật ký thực thi công cụ. |
| **Tool Risk Class** | Phân loại mức độ tác động phụ (side-effect) của một công cụ MCP: `read-only` (chỉ đọc, không đổi trạng thái ngoài), `mutating` (tạo/sửa dữ liệu thông thường), và `destructive` (xóa dữ liệu, chuyển tiền, thay đổi quyền hạn hệ thống hoặc hành động không thể hoàn tác). |
| **Schema Hash Pinning** | Cơ chế khóa dấu vân tay nội dung mô tả và cấu trúc tham số của công cụ MCP tại thời điểm được phê duyệt nhằm phát hiện và chặn đứng hành vi tự ý thay đổi mô tả ngầm từ phía máy chủ công cụ ngoài (*MCP Rug-Pull*). |
| **A2A Plane** | Mặt phẳng nghiệp vụ chuyên trách khám phá hồ sơ năng lực tác nhân (`Agent Card`), kiểm soát ủy quyền tác vụ bất đồng bộ nhiều bước và theo dõi trạng thái/ngân sách liên tác nhân. |
| **Provider Entitlement** | Quyền sử dụng hợp pháp được nhà cung cấp dịch vụ đích cho phép theo hợp đồng hoặc điều khoản dịch vụ thương mại (ví dụ: dùng khóa API trả theo mức dùng cho ứng dụng máy chủ). |
| **Consumer Subscription** | Gói thuê bao cá nhân trả phí cố định dành cho người dùng cuối tương tác trực tiếp trên giao diện chính hãng (ví dụ: ChatGPT Plus/Pro, Codex cá nhân, Claude Pro/Max, Google Antigravity...). **Không phải là `Provider Entitlement` cho cổng API dùng chung.** |
| **Pre-Request Budget Reservation** | Nghiệp vụ khóa tạm một khoản hạn mức chi phí ước tính tối đa trước khi chuyển tiếp yêu cầu ra nhà cung cấp ngoài nhằm ngăn chặn nhiều luồng song song làm vượt trần ngân sách cứng. |
| **Settlement (Quyết toán sử dụng)** | Bước đối chiếu và ghi sổ chi phí cuối cùng sau khi yêu cầu hoàn tất hoặc kết thúc sớm: giải phóng phần hạn mức tạm giữ còn dư và ghi nhận độ chính xác của số liệu (`EXACT`, `ESTIMATED` hoặc `UNMETERED_FLAGGED`). |
| **Partial Stream** | Trạng thái luồng phản hồi theo thời gian thực đã gửi ít nhất một phần dữ liệu nội dung hoặc lệnh gọi công cụ về cho `API Client` trước khi gặp sự cố ngắt quãng ở phía nhà cung cấp. |
| **Fail-Closed vs Fail-Open** | Hai chế độ xử lý khi thành phần kiểm tra chính sách gặp lỗi hoặc không tìm thấy đích hợp lệ: `Fail-Closed` từ chối yêu cầu để bảo vệ tuyệt đối an toàn/tuân thủ; `Fail-Open` cho phép yêu cầu đi tiếp kèm cảnh báo để ưu tiên tính sẵn sàng đối với các kiểm tra phụ trợ không trọng yếu. |
