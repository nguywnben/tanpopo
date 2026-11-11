# Đặc tả Tiêu chí Nghiệm thu Chức năng, Bảo mật & Phi Chức năng (Acceptance Criteria) — Tanpopo

> **Tài liệu Kiểm chứng Nghiệm thu (Phase ASTRA A03 — Gate G2: `READY_FOR_REVIEW` | `DRAFT — UNLOCKED SCOPE`)**
> - **Phiên bản:** `0.1.0-step03-draft`
> - **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Principal Requirements Engineer & Product Manager
> - **Trạng thái quản trị:** **Bản nháp chưa khóa phạm vi (`DRAFT`)**. Mọi tiêu chí dưới đây được viết theo cấu trúc `Given / When / Then` có thể tự động hóa thành bài kiểm thử tích hợp, kiểm thử hợp đồng và kiểm thử thâm nhập bảo mật. **100% yêu cầu `Must` (`P0`) trong [`srs.md`](./srs.md) đều có tiêu chí nghiệm thu trực tiếp.**

---

## Phần A — Tiêu chí Nghiệm thu Chức năng theo Phân hệ (`TP-AC-F0101` .. `TP-AC-F1003`)

### Domain 01: Gateway, Giao thức & Đa phương thức (`gateway-protocols`)

- **`TP-AC-F0101` — Điểm cuối Unified Inference & Truyền luồng SSE (`TP-FR-0101`):**
  - *Given:* `Route R1` ánh xạ tới `Model Deployment` đang `HEALTHY` và `API Client` mang `Virtual Key` hợp lệ.
  - *When:* Gửi yêu cầu chuẩn OpenAI SDK tới `/v1/chat/completions`, `/v1/responses` (cả `stream: false` và `stream: true`) và `/v1/embeddings`.
  - *Then:* Hệ thống trả `200 OK` đúng định dạng JSON hoặc `text/event-stream` kết thúc chuẩn, đính kèm `x-tanpopo-request-id` trên tiêu đề phản hồi và ghi nhận bút toán sử dụng vào sổ cái.
- **`TP-AC-F0102` — Chặn Sớm Yêu cầu Vượt Năng lực Mô hình Đích (`TP-FR-0102`):**
  - *Given:* Tuyến đích chỉ chứa mô hình khai báo `audio_input = false` hoặc `strict_json_schema = false` trong `Capability Profile`.
  - *When:* `API Client` gửi yêu cầu đính kèm tệp âm thanh hoặc yêu cầu `response_format` có `json_schema` với `strict: true`.
  - *Then:* Hệ thống từ chối ngay với `400 capability_not_supported` chỉ rõ tên năng lực thiếu, **không mở bất kỳ kết nối mạng nào ra nhà cung cấp ngoài** và giải phóng khoản giữ chỗ ngân sách (`RELEASED_UNUSED`).
- **`TP-AC-F0103` — Cấm Tuyệt đối Chuyển mạch Ngầm Sau Khi Đã Phát Luồng Một phần (`TP-FR-0103`):**
  - *Given:* `Route R1` có `Deployment D1` (chính) và `D2` (dự phòng khỏe mạnh); `D1` phát thành công 15 mảnh dữ liệu SSE đầu tiên về `API Client` rồi ngắt kết nối đột ngột.
  - *When:* Gateway phát hiện kết nối tới `D1` bị đứt giữa luồng.
  - *Then:* Gateway phát khung sự kiện lỗi chuẩn `mid_stream_abort` để đóng luồng SSE, **hoàn toàn không gửi yêu cầu sang `D2`**, và quyết toán phần dữ liệu đã truyền của `D1` với nhãn `ESTIMATED`.
- **`TP-AC-F0104` — Lan truyền Hủy Kết nối Tức thì & Khung Lỗi Đồng nhất (`TP-FR-0104`):**
  - *Given:* Yêu cầu sinh văn bản dài đang truyền luồng SSE ở giây thứ 2.
  - *When:* `API Client` chủ động đóng kết nối TCP/HTTP hủy yêu cầu.
  - *Then:* Gateway hủy yêu cầu tương ứng phía nhà cung cấp đích trong vòng `< 500 ms` và tất toán ngay khoản giữ chỗ ngân sách.
- **`TP-AC-F0105` — Giữ nguyên Phương ngữ Gốc Không Mất mát & Bảo vệ Cùng Phương ngữ (`TP-FR-0105`):**
  - *Given:* `API Client` gọi `/native/anthropic/v1/messages` kèm cấu trúc đặc thù (`cache_control`, `thinking` blocks) và một trường mở rộng mới chưa từng định nghĩa trong Core.
  - *When:* Yêu cầu đi qua tuyến trỏ tới triển khai phương ngữ Anthropic (trường hợp 1) hoặc cấu hình sai trỏ sang triển khai khác phương ngữ (trường hợp 2).
  - *Then:* Ở trường hợp 1, nhà cung cấp đích nhận nguyên vẹn 100% các trường JSON gốc không bị gọt bỏ và sổ cái vẫn trích xuất đúng token cache/suy luận; ở trường hợp 2, hệ thống từ chối ngay với `400 dialect_mismatch`.

### Domain 02: Nhà cung cấp, Mô hình & Thông tin Xác thực (`provider-credentials`)

- **`TP-AC-F0201` — Phân tách 4 Thực thể, Chia sẻ Một chiều & Chặn Xóa Kết nối Đang dùng (`TP-FR-0201`, `TP-FR-0203`):**
  - *Given:* Một `Provider Adapter` duy nhất được dùng để tạo 2 `Provider Connection` (`C_Org` cấp Tổ chức và `C_Proj` cấp Dự án), trong đó `C_Org` được chia sẻ xuống `Project P1` và gắn vào `Route R_Active`.
  - *When:* `Team Admin` của `P1` sử dụng `C_Org` để định tuyến, cố đọc/sửa khóa của `C_Org`, hoặc quản trị viên cố xóa cứng `C_Org` khi `R_Active` đang bật.
  - *Then:* Định tuyến hoạt động bình thường; thao tác đọc/sửa khóa của `Team Admin` bị từ chối `403`; lệnh xóa cứng `C_Org` bị chặn `409 resource_in_use` cho tới khi gỡ khỏi `R_Active`.
- **`TP-AC-F0202` — Hỗ trợ Nạp Nguội Mô hình Cục bộ (`Ollama`/`vLLM`) & Biểu giá Tùy chỉnh (`TP-FR-0202`):**
  - *Given:* `Model Deployment` nội bộ cấu hình `first_byte_timeout = 60s` và đơn giá `0 USD` (hoặc đơn giá khấu hao nội bộ), máy chủ GPU mất `35 giây` nạp mô hình vào VRAM trước khi trả mảnh đầu tiên.
  - *When:* Gửi yêu cầu suy luận lần đầu tới mô hình nội bộ đó.
  - *Then:* Yêu cầu không bị ngắt nhầm ở mốc 5–10 giây mặc định của đám mây, triển khai vẫn giữ trạng thái `HEALTHY` và sổ cái ghi nhận đúng đơn giá nội bộ đã cấu hình.
- **`TP-AC-F0203` — Làm mới Đơn luồng `Authorized OAuth 2.1` (`TP-FR-0205`):**
  - *Given:* Kết nối `Authorized OAuth` có `access_token` vừa chạm ngưỡng hết hạn và nhận đồng thời 50 yêu cầu suy luận song song.
  - *When:* Cơ chế làm mới tự động kích hoạt.
  - *Then:* Đúng 1 yêu cầu làm mới duy nhất được gửi tới `token_endpoint` của nhà cung cấp; 49 luồng còn lại chờ nhận token mới và cả 50 yêu cầu suy luận hoàn tất thành công không phát sinh lỗi xung đột thu hồi token.
- **`TP-AC-F0204` — Định danh Liên kết Đám mây & Khóa Bọc KMS Ngoài (`TP-FR-0206`):**
  - *Given:* Kết nối cấu hình dùng `Workload Identity` / OIDC hoặc kho khóa bọc KMS bên ngoài.
  - *When:* Thực hiện khởi động máy chủ và gọi suy luận tới đám mây đích.
  - *Then:* Hệ thống xác thực thành công mà không cần lưu khóa API tĩnh lâu dài trên đĩa cục bộ; nếu KMS bị thu hồi quyền giải mã, máy chủ chuyển `/readyz` sang không sẵn sàng an toàn (`Fail-Closed`).

### Domain 03: Động cơ Phần mở rộng & Marketplace (`plugin-marketplace`)

- **`TP-AC-F0301` — Cài đặt Nóng Provider Adapter Mới Không Cần Biên dịch Lại Core (`TP-FR-0301`, `TP-FR-0302`):**
  - *Given:* Tiến trình Tanpopo Core đang chạy ổn định (giữ nguyên mã băm tệp thực thi Core); có một nhà cung cấp giả lập mới `Vendor-Z` chưa từng tồn tại trong mã nguồn Core.
  - *When:* Cài đặt và phê duyệt quyền một `Declarative Pack` (hoặc `Sandbox Plugin`) cho `Vendor-Z`, tạo `Connection` và gọi suy luận.
  - *Then:* Truy vấn hoàn tất thành công qua `Vendor-Z` mà không phải biên dịch lại hay khởi động lại toàn bộ tiến trình Core.
- **`TP-AC-F0302` — Đa Nguồn Cài đặt, 3 Nhãn Nguồn gốc, Duyệt Quyền & Khóa `tanpopo.lock` (`TP-FR-0304`, `TP-FR-0307`):**
  - *Given:* Gói phần mở rộng tải từ kho Git/URL/ZIP/Private Registry chưa có chữ ký phát hành của dự án Tanpopo và khai báo cần gọi `api.vendor-z.example`.
  - *When:* Chạy lệnh cài đặt khi chưa xác nhận bảng quyền hạn, sau đó thực hiện phê duyệt quyền.
  - *Then:* Trước khi duyệt, gói nằm yên ở `PENDING_PERMISSION_APPROVAL` và không nhận lưu lượng; sau khi duyệt, gói mang nhãn `unverified-external` (hoặc `verified`/`official` đúng theo chữ ký mật mã hợp lệ) và mã băm SHA-256 cùng tập quyền được ghi chính xác vào `tanpopo.lock`.
- **`TP-AC-F0303` — Khung Kiểm thử Hợp đồng Tự động bằng Tệp Băng ghi (`TP-FR-0305`, `TP-FR-0903`):**
  - *Given:* Tác giả tạo gói phần mở rộng mới bằng `tanpopo plugin init` kèm bộ băng ghi mẫu (`Cassettes`) cho 5 kịch bản (đồng bộ, SSE streaming, lỗi `401`, lỗi `429`, đứt luồng).
  - *When:* Chạy lệnh `tanpopo plugin test` hoàn toàn ngoại tuyến (không cần khóa thật).
  - *Then:* Công cụ kiểm tra tự động tính hợp lệ của `Manifest`, schema đầu vào/ra, phân loại lỗi và xuất báo cáo đạt/trượt rõ ràng với mã thoát chuẩn cho CI.
- **`TP-AC-F0304` — Chặn Tự động Kích hoạt Khi Tăng Quyền & Quay lui Tức thì (`TP-FR-0306`):**
  - *Given:* `Plugin P@1.0.0` đang chạy với quyền gọi `a.example.com`; bản `P@1.1.0` bổ sung thêm `b.example.com` vào `Manifest`.
  - *When:* Kích hoạt cập nhật không kèm phê duyệt quyền mới (bước 1), sau đó duyệt lên `1.1.0` rồi gọi lệnh `Rollback` về `1.0.0` khi đang có luồng truyền dở (bước 2).
  - *Then:* Ở bước 1, bản `1.1.0` bị chặn ở `UPDATING_PENDING_RECONSENT` và `100%` lưu lượng vẫn đi qua `1.0.0`; ở bước 2, các luồng mới quay về `1.0.0` trong vài giây trong khi luồng đang chạy trên `1.1.0` được rút êm (`Graceful Drain`).

### Domain 04: Mặt phẳng MCP & Mặt phẳng Liên tác nhân A2A (`mcp-a2a`)

- **`TP-AC-F0401` — Lọc Danh mục Công cụ MCP Theo Khóa & Phân trang Không Cắt cụt (`TP-FR-0401`):**
  - *Given:* Tổ chức đăng ký các `MCP Server` có tổng cộng 250 công cụ; `Virtual Key K_Dev` chỉ được cấp quyền trên 120 công cụ chỉ đọc.
  - *When:* `MCP Client` dùng `K_Dev` gọi `tools/list` qua con trỏ phân trang và sau đó thử gọi trực tiếp 1 công cụ nằm trong nhóm 130 công cụ không được cấp quyền.
  - *Then:* `tools/list` trả đủ đúng 120 công cụ qua các trang (không bị cắt cụt ở mốc 100 và ẩn hoàn toàn 130 công cụ kia); lệnh gọi trái phép bị chặn `403` và ghi sự kiện kiểm toán.
- **`TP-AC-F0402` — Chặn Tức thì Khi Lệch Dấu vân tay Cấu trúc Công cụ MCP (`TP-FR-0402`):**
  - *Given:* Công cụ `query_crm` đã được duyệt trong môi trường `production` với `schema_hash = H1`.
  - *When:* `MCP Server` phía sau tự ý sửa chuỗi mô tả văn bản của `query_crm` (`schema_hash` đổi sang `H2`) và tác nhân gửi lệnh gọi `query_crm`.
  - *Then:* Mặt phẳng MCP chặn đứng lệnh gọi với mã lỗi lệch dấu vân tay cấu trúc và phát cảnh báo an ninh cho quản trị viên.
- **`TP-AC-F0403` — Phê duyệt Con người Chế độ Kép Cho Công cụ `destructive` (`TP-FR-0403`, `TP-FR-0904`):**
  - *Given:* Công cụ `drop_staging_table` gắn mức rủi ro `destructive`.
  - *When:* Tác nhân gọi công cụ ở chế độ chờ ngắn (được `Reviewer` bấm duyệt trong 15 giây) hoặc chế độ bất đồng bộ (nhận mã chờ, `Reviewer` duyệt trên Web Console sau 5 phút rồi tác nhân gọi lại kèm `Pre-Approval Token` đúng tham số).
  - *Then:* Cả hai luồng đều chỉ thực thi công cụ đúng 1 lần sau khi có chữ ký phê duyệt hợp lệ của `Reviewer` và ghi đầy đủ vào `AuditEvent`.
- **`TP-AC-F0404` — Khám phá `AgentCard`, Ủy quyền A2A Bất đồng bộ & Giới hạn Bước nhảy (`TP-FR-0405`, `TP-FR-0406`):**
  - *Given:* Hai `Agent Endpoint` hợp lệ được đăng ký trên mặt phẳng A2A với `Max Delegation Hop Depth = 5`.
  - *When:* Tác nhân A ủy quyền tác vụ hợp lệ cho Tác nhân B (trường hợp 1) hoặc tạo chuỗi ủy quyền vòng lặp sang bước nhảy thứ 6 (trường hợp 2).
  - *Then:* Trường hợp 1 hoàn tất luồng trạng thái và gộp chi phí dưới cùng một mã truy vết gốc; trường hợp 2 bị từ chối ngay ở bước nhảy vượt ngưỡng với mã lỗi `delegation_depth_exceeded`.

### Domain 05: Xác thực, Đa người thuê & Phân quyền (`iam-tenancy`)

- **`TP-AC-F0501` — Phân cấp `Org → Project → Env` & Nâng cấp Từ Chế độ Cá nhân (`TP-FR-0501`):**
  - *Given:* Người dùng khởi tạo ở `Personal Mode` (tự sinh 1 `Organization`, 1 `Project`, 1 `Environment` mặc định), sau đó tạo thêm `Project` thứ hai và môi trường `staging`/`production`.
  - *When:* Chuyển sang giao diện làm việc nhóm.
  - *Then:* Toàn bộ khóa, kết nối và sổ cái cũ vẫn giữ nguyên vẹn không cần di chuyển lại cơ sở dữ liệu.
- **`TP-AC-F0502` — Kiểm soát Quyền RBAC Tập trung & Chống Dò Mật khẩu (`TP-FR-0502`):**
  - *Given:* Tài khoản có vai trò `Developer` trong `Project P1` và một nguồn bên ngoài gửi liên tiếp các lần đăng nhập sai mật khẩu vượt ngưỡng.
  - *When:* `Developer` gọi trực tiếp Admin REST API để cài phần mở rộng mới hoặc tăng trần ngân sách của `P1`.
  - *Then:* API từ chối `403 Forbidden` (không phụ thuộc giao diện Web); nguồn đăng nhập sai liên tiếp bị khóa/giới hạn tốc độ tạm thời và ghi nhật ký bảo mật.
- **`TP-AC-F0503` — Xoay Khóa Kép Không Gián đoạn vs Thu hồi Khẩn cấp (`TP-FR-0503`):**
  - *Given:* `Virtual Key K1` đang phục vụ lưu lượng liên tục.
  - *When:* Thực hiện lệnh xoay khóa (`Rotate` với cửa sổ 24h) để sinh `K2`, sau đó kiểm tra dữ liệu lưu trong bảng khóa.
  - *Then:* Cả `K1` và `K2` đều hoạt động hợp lệ trong cửa sổ chuyển giao, cơ sở dữ liệu chỉ lưu mã băm một chiều (không chứa chuỗi khóa rõ), và `K1` tự động hết hiệu lực khi hết hạn cửa sổ xoay vòng.
- **`TP-AC-F0504` — Liên kết `OIDC SSO` & Quy tắc Bốn Mắt (`TP-FR-0504`):**
  - *Given:* Tổ chức bật đăng nhập OIDC và bật quy tắc `Four-Eyes` cho thao tác duyệt phần mở rộng bên ngoài.
  - *When:* Quản trị viên A tải gói phần mở rộng lên và tự bấm nút phê duyệt bằng chính tài khoản của mình.
  - *Then:* Hệ thống từ chối tự phê duyệt, yêu cầu một quản trị viên/thẩm định viên B độc lập thứ hai xác nhận mới chuyển sang `ENABLED_ACTIVE`.

### Domain 06: Định tuyến Nhận thức Năng lực & Chịu lỗi (`routing-resilience`)

- **`TP-AC-F0601` — Định tuyến Năng lực, Chuyển mạch Trước Luồng & Ngắt mạch (`TP-FR-0601`, `TP-FR-0603`):**
  - *Given:* `Route R1` gồm `D1` (cửa sổ 32k), `D2` (128k, chính) và `D3` (128k, dự phòng); `D2` liên tục trả lỗi `503` trước khi sinh dữ liệu.
  - *When:* Gửi loạt yêu cầu có ngữ cảnh 60k token vào `R1`.
  - *Then:* `D1` bị bỏ qua do thiếu cửa sổ ngữ cảnh; yêu cầu tự động chuyển từ `D2` sang `D3` thành công; sau khi chạm ngưỡng lỗi, `D2` chuyển sang `CIRCUIT_OPEN_UNHEALTHY` để các yêu cầu tiếp theo đi thẳng tới `D3` không tốn thời gian chờ `D2`.
- **`TP-AC-F0602` — Ép buộc Cứng Vùng Cư trú Dữ liệu Không Phá rào Khi Lỗi (`TP-FR-0602`):**
  - *Given:* Chính sách bắt buộc `Data Region = EU`; `D_EU` đang mở ngắt mạch do sự cố nhà cung cấp trong khi `D_US` hoàn toàn khỏe mạnh.
  - *When:* Gửi yêu cầu suy luận vào tuyến đó.
  - *Then:* Bộ định tuyến từ chối dùng `D_US` và trả ngay `503 no_compliant_deployment_available`.
- **`TP-AC-F0603` — Trình Mô phỏng Giải thích Khô Không Tốn Chi phí (`TP-FR-0604`, `TP-FR-0904`):**
  - *Given:* Cấu hình tuyến phức tạp gồm 4 triển khai mô hình và 3 quy tắc chính sách.
  - *When:* Gọi `/v1/route/explain` (hoặc trên Console/CLI) với thông số yêu cầu giả lập.
  - *Then:* Hệ thống trả về danh sách đầy đủ cả 4 triển khai kèm lý do đạt/loại chính xác cho từng cái, với `0` gói tin gọi ra nhà cung cấp ngoài và `0 USD` trừ vào sổ cái.

### Domain 07: Động cơ Chính sách & Bảo mật Vận hành (`policy-security`)

- **`TP-AC-F0701` — Nhật ký Kiểm toán Bất biến & Công tắc Ngắt Khẩn cấp 4 Cấp (`TP-FR-0704`, `TP-FR-0705`):**
  - *Given:* Hệ thống đã ghi nhận các sự kiện cài đặt plugin, tạo khóa và vi phạm chính sách.
  - *When:* Quản trị viên thử gọi lệnh sửa/xóa dòng nhật ký kiểm toán qua API (bước 1), sau đó kích hoạt cách ly khẩn cấp `Extension Quarantine` trên một plugin và chạy truy vấn bán kính ảnh hưởng (bước 2).
  - *Then:* Bước 1 bị từ chối `403`; bước 2 ngắt ngay lưu lượng qua plugin đó và trả về danh sách chính xác toàn bộ các `Project` và `Virtual Key` từng gọi qua plugin trong khung thời gian tra cứu.

### Domain 08: Quan sát Vận hành, Sổ cái Ngân sách & Tài chính (`telemetry-finops`)

- **`TP-AC-F0801` — Giữ chỗ Hai Pha Chống Đua tranh Vượt Trần Ngân sách Song song (`TP-FR-0801`):**
  - *Given:* Ngân sách cứng khả dụng của `Project P1` chỉ còn đúng `0.10 USD`; mỗi yêu cầu ước tính giữ chỗ `0.05 USD`.
  - *When:* Bắn đồng thời 30 yêu cầu streaming song song cùng một thời điểm vào `P1`.
  - *Then:* Tối đa chỉ 2 yêu cầu được cấp giữ chỗ `RESERVED_HOLD` và đi ra nhà cung cấp ngoài; tối thiểu 28 yêu cầu còn lại bị chặn ngay tại cửa với `429 project_budget_exceeded`; nếu một tiến trình gặp sự cố làm treo khoản giữ chỗ quá hạn TTL, bộ dọn dẹp tự động thu hồi về trạng thái chuẩn.
- **`TP-AC-F0802` — Quyết toán Đa thành phần & Kết chuyển Số dư Âm Nhẹ Cuối luồng (`TP-FR-0802`):**
  - *Given:* Số dư khả dụng còn `0.04 USD`, yêu cầu cuối cùng giữ chỗ hợp lệ `0.04 USD` nhưng khi luồng kết thúc nhà cung cấp báo thực tế tiêu tốn `0.05 USD` (gồm cả token suy luận/cache).
  - *When:* Luồng kết thúc và ứng dụng gửi tiếp một yêu cầu mới ngay sau đó.
  - *Then:* Luồng đang chạy không bị cắt cụt ở từ cuối cùng, sổ cái ghi nhận trung thực thực chi `0.05 USD` (số dư khả dụng thành `-0.01 USD`) và yêu cầu kế tiếp bị chặn cứng cho tới khi được cấp thêm ngân sách.
- **`TP-AC-F0803` — Phân loại Trung thực `EXACT` / `ESTIMATED` / `UNMETERED_FLAGGED` (`TP-FR-0802`, `TP-FR-0803`):**
  - *Given:* Trong kỳ có 3 giao dịch: (1) hoàn tất đủ số đếm chuẩn từ hãng, (2) đứt luồng giữa chừng sau khi truyền 400 từ không có bản tin tổng kết cuối luồng, và (3) đi qua cổng tùy chỉnh chưa khai báo bảng giá.
  - *When:* Xuất báo cáo đối soát chi phí (`Chargeback Report`) của dự án.
  - *Then:* Giao dịch (1) mang nhãn `EXACT`, giao dịch (2) tính chi phí ước lượng `> 0` mang nhãn `ESTIMATED`, giao dịch (3) mang nhãn `UNMETERED_FLAGGED`; báo cáo tách riêng các cột này và **không nơi nào tuyên bố tổng chi phí chính xác tuyệt đối 100% khi đang tồn tại giao dịch (2) hoặc (3)**.
- **`TP-AC-F0804` — Mặc định Không Lưu Nội dung Gốc & Xuất OTLP `Fail-Open` Có Kiểm soát (`TP-FR-0805`, `TP-FR-0806`):**
  - *Given:* Dự án ở cấu hình mặc định và bộ thu thập OpenTelemetry bên ngoài bị tắt đột ngột.
  - *When:* Gửi truy vấn chứa chuỗi dữ liệu khách hàng đặc trưng `SECRET_PII_MARKER_99` kèm khóa xác thực.
  - *Then:* Truy vấn suy luận vẫn thành công bình thường (bộ đếm gói OTLP bị rớt tăng lên cảnh báo, không làm treo cổng), và tìm kiếm toàn văn trong kho nhật ký bền vững của Tanpopo trả về `0` kết quả cho cả `SECRET_PII_MARKER_99` lẫn giá trị khóa xác thực.

### Domain 09: Giao diện Web Console, CLI & Admin API (`console-cli-api`)

- **`TP-AC-F0901` — Phân trang Chuẩn & Chống Ghi đè Đồng thời `409 Conflict` (`TP-FR-0901`):**
  - *Given:* Quản trị viên A và B cùng tải cấu hình `Route R1` ở phiên bản `v2`; A lưu thay đổi lên `v3`.
  - *When:* B gửi lệnh cập nhật `R1` vẫn đính kèm phiên bản cũ `v2`.
  - *Then:* Admin API từ chối yêu cầu của B với mã `409 Conflict` mà không làm mất thay đổi `v3` của A.
- **`TP-AC-F0902` — Trải nghiệm Khởi tạo Ngày đầu Đồng bộ Giữa Web Console và CLI (`TP-FR-0902`, `TP-FR-0903`):**
  - *Given:* Máy chủ mới khởi chạy lần đầu tiên chưa có cấu hình.
  - *When:* Người vận hành thực hiện Setup Wizard trên Web Console (Astryx `neutral`) để bật 1 Official Adapter, nhập khóa, tạo `Route`, phát hành `Virtual Key` và chạy thử 1 truy vấn.
  - *Then:* Toàn bộ luồng hoàn tất liền mạch trong `< 3 phút`, và mọi tài nguyên vừa tạo đều tra cứu/quản trị nhất quán qua lệnh `tanpopo` CLI.

### Domain 10: Tự Vận hành, Cài đặt, Sao lưu & Nâng cấp (`selfhost-ops`)

- **`TP-AC-F1001` — Khởi chạy Đơn nút Tự chứa, `/healthz` `/readyz` & Khởi tạo GitOps (`TP-FR-1001`, `TP-FR-1002`, `TP-FR-0903`):**
  - *Given:* Máy chủ sạch không cài sẵn PostgreSQL hay Redis ngoài, chỉ có tệp cấu hình khai báo và `tanpopo.lock`.
  - *When:* Chạy lệnh khởi động Tanpopo.
  - *Then:* Hệ thống tự khởi tạo kho nhúng, xác minh `tanpopo.lock`, chuyển `/healthz` và `/readyz` sang `200 OK` và sẵn sàng phục vụ lưu lượng đúng theo cấu hình khai báo.
- **`TP-AC-F1002` — Chặn Nâng cấp Khi Lệch Hợp đồng (`Preflight Check`) & Xóa Sạch Đơn vị Thuê (`TP-FR-1003`, `TP-FR-1004`, `TP-FR-0307`):**
  - *Given:* Hệ thống đang bật một `Extension` chỉ tương thích hợp đồng `v1`, trong khi bản Core nâng cấp yêu cầu tối thiểu `v3` (kịch bản 1); hoặc quản trị viên ra lệnh xóa vĩnh viễn `Project P_Old` (kịch bản 2).
  - *When:* Chạy trình kiểm tra nâng cấp (kịch bản 1) hoặc xác nhận xóa `P_Old` (kịch bản 2).
  - *Then:* Kịch bản 1 dừng an toàn ở trạng thái `BLOCKED_INCOMPATIBLE` liệt kê rõ tên phần mở rộng cần cập nhật trước; kịch bản 2 rút sạch luồng đang chạy và xóa không thể khôi phục toàn bộ khóa bí mật cùng dữ liệu truy vấn của `P_Old`.
- **`TP-AC-F1003` — Mở rộng Ngang Không Trạng thái Nhiều Bản sao (`TP-FR-1005`):**
  - *Given:* Cụm gồm 3 bản sao Data Plane đứng sau bộ cân bằng tải vòng tròn (`Round-Robin`, không gán dính phiên) dùng chung kho dữ liệu và bộ đếm phân tán.
  - *When:* Gửi tải liên tục đồng thời tắt đột ngột 1 trong 3 bản sao.
  - *Then:* Hạn mức ngân sách và tốc độ toàn cục vẫn được thực thi chính xác không bị nhân 3 lần, và toàn bộ yêu cầu mới tự động được 2 bản sao còn sống xử lý bình thường.

---

## Phần B — Bộ Tiêu chí Nghiệm thu Bảo mật, Ranh giới Tin cậy & Tuân thủ (`TP-AC-SEC-01` .. `TP-AC-SEC-08`)

| Mã AC Bảo mật | Hạng mục rủi ro trọng yếu | Kịch bản kiểm thử tấn công / Lạm dụng (`Given / When / Then`) |
|---|---|---|
| **`TP-AC-SEC-01`** | **1. Bảo vệ Kho Bí mật Gốc (`Secrets`)** | *Given:* Đã lưu API Key thật `sk-live-REAL999` vào kho bí mật và gắn với một `Community Adapter` chuẩn.<br/>*When:* (a) Quản trị viên gọi mọi điểm cuối Admin API/CLI/UI để xem chi tiết kết nối; (b) Bên trong mã của phần mở rộng cố đọc toàn bộ bộ nhớ đầu vào được truyền vào; (c) Nhà cung cấp đích trả về lỗi `401` có nhại lại chuỗi khóa.<br/>*Then:* Cả (a), (b) và (c) đều **không bao giờ** xuất hiện chuỗi `sk-live-REAL999` ở dạng văn bản rõ (Host tự tiêm khóa ở biên mạng và tự lọc sạch chuỗi khóa khỏi mọi phản hồi lỗi/nhật ký). |
| **`TP-AC-SEC-02`** | **2. Kiềm tỏa Phần mở rộng Cộng đồng / Không Tin cậy (`Community Plugins`)** | *Given:* Một gói phần mở rộng bên ngoài: (a) tự ghi `"trust_tier": "official"` giả mạo trong Manifest, (b) bị sửa 1 byte so với mã băm trong `tanpopo.lock`, (c) cố mở kết nối mạng ngầm tới `attacker.example` ngoài `allowed_hosts`, hoặc (d) chạy vòng lặp vô hạn `while(true){}`.<br/>*When:* Cài đặt và gửi lưu lượng qua phần mở rộng đó.<br/>*Then:* (a) Nhãn giả mạo bị bác bỏ/hạ cưỡng bức xuống `unverified-external`; (b) Gói lệch băm bị từ chối nạp; (c) Kết nối tới `attacker.example` bị chặn tức khắc kèm ghi `AuditEvent` bảo mật và cách ly `QUARANTINED`; (d) Vòng lặp vô hạn bị ngắt đúng hạn mức CPU mà không làm sập hay nghẽn các tuyến khác trên cổng. |
| **`TP-AC-SEC-03`** | **3. Bảo mật Kết nối `Authorized OAuth` (`User / App OAuth`)** | *Given:* Luồng kết nối OAuth 2.1 được cấp phép.<br/>*When:* (a) Kẻ tấn công gửi callback giả mạo lệch tham số `state` hoặc thiếu bộ kiểm chứng `PKCE`; (b) Nhà cung cấp trả về tập `scopes` thiếu quyền hoặc rộng bất thường không khớp cấu hình; (c) Quản trị viên thu hồi quyền phía nhà cung cấp dẫn tới lỗi `invalid_grant` khi đang có tải cao.<br/>*Then:* (a) và (b) bị từ chối kích hoạt ngay lập tức; (c) Kết nối lập tức chuyển sang `AUTH_EXPIRED_REAUTH_REQUIRED` sau 1 lần gọi làm mới duy nhất, dừng bắn yêu cầu vô ích và không rò rỉ `refresh_token` ra ngoài. |
| **`TP-AC-SEC-04`** | **4. Chuyển tiếp Mã Xác thực & Chống SSRF (`Token Forwarding & SSRF`)** | *Given:* `API Client` gửi `Virtual Key vk-client-123` vào Tanpopo; hoặc kẻ có quyền cấu hình thử nhập điểm cuối mô hình/MCP trỏ tới `http://169.254.169.254/latest/meta-data/`, `http://127.0.0.1:6379` hay máy chủ trả HTTP `302 Redirect` sang tên miền khác.<br/>*When:* Thực thi kiểm tra kết nối hoặc gửi truy vấn.<br/>*Then:* `vk-client-123` luôn bị lột sạch trước khi ra ngoài; khi gặp HTTP `302` khác tên miền đích đã duyệt, hệ thống **tuyệt đối không tự động chuyển tiếp tiêu đề `Authorization`** sang miền lạ; mọi địa chỉ siêu dữ liệu đám mây và mạng nội bộ trái phép đều bị chặn cứng ngay từ tầng kiểm tra biên mạng. |
| **`TP-AC-SEC-05`** | **5. Ranh giới Tin cậy 4 Lớp & Tư thế `Fail-Closed` (`Trust Boundaries`)** | *Given:* (a) `Virtual Key` chỉ được cấp quyền gọi suy luận `Provider`; (b) `Organization` khóa chính sách chỉ cho phép phần mở rộng `official`; (c) Kho kiểm tra ngân sách cứng hoặc bộ kiểm tra vùng dữ liệu bị gián đoạn tạm thời.<br/>*When:* (a) Dùng khóa đó gọi mặt phẳng `/mcp` hoặc `/a2a`; (b) `Team Admin` cố gắn gói `unverified-external` vào dự án; (c) Gửi truy vấn phát sinh phí.<br/>*Then:* Cả 3 kịch bản đều bị từ chối an toàn (`Fail-Closed` — `403`/`503`), không có bất kỳ quyền ngầm nào bắc cầu giữa 4 lớp hay vượt rào chính sách cấp trên. |
| **`TP-AC-SEC-06`** | **6. Tuân thủ Điều khoản Thuê bao (`Subscription Authorization`)** | *Given:* Rà soát toàn bộ mã nguồn Core, bộ Official Adapters, quy tắc kiểm định Marketplace, giao diện Web Console/Setup Wizard và tài liệu chính thức.<br/>*When:* Kiểm tra khả năng cấu hình hoặc nộp gói phần mở rộng trích xuất cookie/phiên trình duyệt/OAuth đóng của gói thuê bao tiêu dùng cá nhân (`ChatGPT Plus/Pro`, `Codex` cá nhân, `Claude Pro/Max`, `Google Antigravity`...).<br/>*Then:* Gói nộp lên Marketplace chính thức bị từ chối cấp nhãn `official`/`verified`; sản phẩm không chứa bất kỳ công cụ trích xuất phiên tiêu dùng, mẫu hướng dẫn hay nút bấm *"Tôi chấp nhận rủi ro để dùng tài khoản Plus/Pro làm API chung"* nào; tài liệu ghi rõ OAuth chỉ áp dụng cho tích hợp được nhà cung cấp cấp phép hợp lệ. |
| **`TP-AC-SEC-07`** | **7. Cô lập Đa Người thuê & Đơn Môi trường (`Multi-Tenancy`)** | *Given:* Trên cùng một máy chủ có `Organization A` và `Organization B`; trong `Org A` có `Virtual Key K_Dev` gắn với môi trường `development` và `K_Sec` đang mở 5 luồng SSE dài.<br/>*When:* (a) Chủ thể của `Org A` dùng ID đoán được để gọi/xem `Route`, `Secret`, `MCP Tool` hoặc `AuditEvent` của `Org B`; (b) Dùng `K_Dev` gọi tuyến `production`; (c) Bấm nút *Thu hồi Khẩn cấp An ninh* đối với `K_Sec`.<br/>*Then:* (a) Trả lỗi không tiết lộ sự tồn tại của tài nguyên `Org B` kèm ghi cảnh báo bảo mật; (b) Từ chối lệch môi trường; (c) Cả 5 luồng SSE đang mở của `K_Sec` bị ngắt đứt và các yêu cầu mới bị chặn trong ngưỡng SLA (`< 1s` đơn nút / `< 5s` cụm). |
| **`TP-AC-SEC-08`** | **8. Kiểm soát Tác động Phụ & Khuếch đại Đặc quyền (`Side Effects`)** | *Given:* Công cụ MCP `execute_refund` (`destructive`) đã được `Reviewer` duyệt cho tham số `(order_id="100", amount=20)` và tác nhân A2A cha còn hạn mức ngân sách `1.00 USD`.<br/>*When:* (a) Tác nhân đổi tham số thành `amount=2000` (hoặc tự duyệt yêu cầu của chính mình); (b) Lệnh gọi `execute_refund` hợp lệ bị quá thời gian chờ mạng sau khi đã gửi tới `MCP Server`; (c) Tác nhân A2A cha ủy quyền tác vụ con đòi trần `5.00 USD`.<br/>*Then:* (a) Bị chặn `403 parameter_hash_mismatch` / `self_approval_forbidden`; (b) Hệ thống chỉ gửi đúng 1 lần duy nhất, **không tự động thử lại lần 2** và trả `ambiguous_side_effect_timeout`; (c) Ủy quyền A2A vượt ngân sách cha bị từ chối ngay lập tức. |

---

## Phần C — Tiêu chí Xác minh Yêu cầu Phi Chức năng (`TP-AC-N01` .. `TP-AC-N10`)

| Mã AC | Các NFR được xác minh | Phương pháp đo đạc & Ngưỡng đạt (*Mục tiêu dự kiến trước Benchmark*) |
|---|---|---|
| **`TP-AC-N01`** | `TP-NFR-0101`..`0103` | Chạy bộ tạo tải tự động nối với máy chủ nhà cung cấp giả lập có độ trễ cố định `0 ms`: xác nhận độ trễ nội bộ cộng thêm `p95 < 2 ms` (Tầng 1) / `< 5 ms` (Tầng 2), độ trễ chuyển tiếp mảnh SSE `< 1 ms`, RAM rỗi đơn nút `< 128 MB` và khởi động `/readyz < 3 giây`. |
| **`TP-AC-N02`** | `TP-NFR-0201`..`0202` | Tiêm lỗi sập ngẫu nhiên 1 nút Data Plane trong cụm `>= 2` nút và tiêm phần mở rộng lỗi lặp vô hạn: xác nhận lưu lượng chung duy trì tỷ lệ thành công mục tiêu `>= 99.9%` và tiến trình chính có `0` lần sập. |
| **`TP-AC-N03`** | `TP-NFR-0301`..`0302` | Mở đồng thời `1.000` kết nối SSE kéo dài 60 giây trên nút chuẩn (2 vCPU, 2 GB RAM): xác nhận không rò rỉ bộ nhớ sau khi đóng luồng và thông lượng tăng tương ứng khi nâng số bản sao từ 1 lên 3 nút. |
| **`TP-AC-N04`** | `TP-NFR-0404` | Trong khi đang bắn `100 req/s` bằng một `Virtual Key`, kích hoạt lệnh thu hồi khẩn cấp và đo dấu thời gian của yêu cầu cuối cùng còn lọt qua: xác nhận `< 1 giây` (đơn nút) và `< 5 giây` (cụm phân tán). |
| **`TP-AC-N05`** | `TP-NFR-0501`..`0502` | Cấu hình TTL ngắn trong môi trường kiểm thử và thực hiện lệnh xóa `Project`: xác nhận trình dọn dẹp xóa đúng hạn không gây khóa nghẽn lưu lượng và `100%` khóa/nội dung của dự án bị xóa biến mất khỏi kho hoạt động trong `< 60 giây`. |
| **`TP-AC-N06`** | `TP-NFR-0601`..`0602` | Thực hiện lệnh sao lưu khi đang có tải, khởi tạo máy chủ mới sạch hoàn toàn và khôi phục: xác nhận hoàn tất trong `< 15 phút` (`RTO` dự kiến), tệp sao lưu không giải mã được khóa bí mật nếu thiếu khóa chủ tách biệt, và cấu hình xuất ra chạy tốt trên môi trường mới. |
| **`TP-AC-N07`** | `TP-NFR-0701`..`0702` | Kiểm thử giao diện tự động trên các trình duyệt mục tiêu ở khung nhìn `1440px`, `1280px`, `768px` và `375px` (cho màn hình cảnh báo/công tắc khẩn cấp): xác nhận không vỡ bố cục, không mất thanh cuộn bảng và bấm được trọn vẹn thao tác khẩn cấp. |
| **`TP-AC-N08`** | `TP-NFR-0801`, `TP-NFR-1001` | Chạy kiểm tra tự động `cmd /c npx astryx doctor --built <dir>`, quét mã nguồn giao diện và chạy bộ kiểm tra WCAG 2.2 AA + điều hướng thuần bàn phím: xác nhận `0 fail` từ `astryx doctor`, `0` thẻ `div`/`span` thô, `0` mã màu cứng ngoài token và hoàn tất được luồng duyệt quyền/thu hồi khóa chỉ bằng bàn phím. |
| **`TP-AC-N09`** | `TP-NFR-0901` | Quy trình CI xuất bản và chạy kiểm thử khói (`smoke test`) thành công ảnh OCI (`linux/amd64`, `linux/arm64`) và tệp thực thi CLI trên Linux, macOS (`arm64`) và Windows (`amd64`). |
| **`TP-AC-N10`** | `TP-NFR-0902`, `TP-NFR-1002` | Nạp phần mở rộng viết theo hợp đồng phiên bản `N-1` trên bản Core `N`: xác nhận chạy tương thích hoàn toàn; kiểm tra mọi phản hồi lỗi từ chối đều khớp schema mã lỗi chuẩn và lệnh `tanpopo doctor` chỉ ra đúng lỗi cấu hình mẫu được tiêm vào. |
