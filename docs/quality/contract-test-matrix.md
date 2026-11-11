# Ma trận Kiểm thử Hợp đồng, Kiểm thử Phủ định & Kịch bản Nghiệm thu External Adapter Độc lập (Contract Test Matrix) — Tanpopo

> **Tài liệu Đảm bảo Chất lượng & Kiểm định Hợp đồng (Phase ASTRA A04 — `DRAFT / VERIFICATION SPECIFICATION`)**
> - **Mã tài liệu:** `SPEC-QA-01` | **Phiên bản:** `0.1.0-step04-draft` | **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Platform Architect & Security-Minded SDK Designer
> - **Mục đích cốt lõi:** Định nghĩa bộ khung kiểm thử tất định chạy ngoại tuyến (`tanpopo plugin test`) và quy trình nghiệm thu bắt buộc chứng minh **một `Provider Adapter` bên ngoài được phát triển, kiểm định, cài đặt, nâng cấp và cách ly hoàn toàn độc lập mà không cần sửa hay biên dịch lại Tanpopo Core**.
> - **Truy vết Yêu cầu:** `DEC-002`, `MO-02`, `MO-04`, `TP-FR-0301`..`0306`, `TP-FR-1004`, `TP-AC-F0301`..`F0304`, `TP-AC-SEC-01`..`08`.

---

## 1. Kiến trúc Bộ Kiểm thử Băng ghi Tất định (`Deterministic Cassette Harness`)

Mọi gói phần mở rộng (cả `Official`, `Verified` lẫn `Private/Community`) đều được kiểm tra bằng cùng một công cụ `tanpopo plugin test` dựa trên các tệp băng ghi cố định (`Cassettes` — chứa cặp bản tin HTTP/SSE đầu vào/đầu ra giả lập của nhà cung cấp và kết quả kỳ vọng của Host):
- **Chạy `100%` ngoại tuyến trong CI dưới vài giây:** Không cần khóa API thật, không tốn chi phí token và không bị nhiễu do mạng chập chờn.
- **Quyết định Cấp độ Đảm bảo Năng lực:** Chỉ những năng lực trong `Manifest` có bài băng ghi vượt qua bộ ma trận dưới đây mới được nâng trạng thái từ `DECLARED` lên `TESTED` (xem [`provider-adapter.md`](../specifications/provider-adapter.md)).

---

## 2. Ma trận Kiểm thử Hợp đồng & Kiểm thử Phủ định Toàn diện (`CT-01` .. `CT-24`)

### Nhóm A — Kiểm định Đóng gói, Lược đồ `Manifest` & Chuỗi Cung ứng (`Packaging & Provenance`)

| Mã Test | Loại | Kịch bản & Đầu vào kiểm thử | Kết quả kỳ vọng bắt buộc (`Pass Criteria`) | Truy vết AC |
|---|---|---|---|---|
| **`CT-01`** | Hợp đồng | Kiểm tra cú pháp `Manifest`, `configSchema`, `secretSlots` và `permissions` hợp lệ. | Đạt `100%` kiểm tra JSON Schema; từ chối mọi trường quyền không xác định hoặc quyền mạng `*` mặc định. | `TP-AC-F0302` |
| **`CT-02`** | Phủ định | Gói khai báo `contractVersion: "9.0"` không nằm trong dải hỗ trợ của Core hiện hành. | Chặn ngay ở bước `VERIFYING` (`SM-01` → `REJECTED_INVALID`) với mã lỗi rõ khoảng phiên bản hỗ trợ. | `TP-AC-F0303` |
| **`CT-03`** | Phủ định | Sửa đổi `1 byte` trong tệp `.wasm` / khai báo sau khi đã ghi mã băm vào `tanpopo.lock` hoặc chữ ký số. | Từ chối nạp gói (`artifact_digest_mismatch`), ghi `AuditEvent` bảo mật mức cao. | `TP-AC-SEC-02` |
| **`CT-04`** | Phủ định | Gói nén `.tpkg`/`.zip` chứa tệp thoát thư mục `../../etc/passwd` (*Zip-Slip*), `symlink` ra ngoài hoặc giải nén phình đại `> 64 MB` (*Zip-Bomb*). | Trình giải nén hủy ngay lập tức tại luồng đọc bộ nhớ đệm, không ghi bất kỳ byte nào ra đĩa. | `TP-AC-SEC-02` |
| **`CT-05`** | Phủ định | Gói bên ngoài tự điền `"trustTier": "official"` nhưng không có chữ ký khóa gốc ngoại tuyến của dự án Tanpopo. | Bác bỏ nhãn giả mạo (hoặc ép hạ xuống `unverified-external` kèm cảnh báo mạo danh nguồn gốc). | `TP-AC-SEC-02` |

### Nhóm B — Tương thích Giao thức, Truyền luồng & Số đo của `Provider Adapter` (`Protocol & Metering`)

| Mã Test | Loại | Kịch bản & Đầu vào kiểm thử | Kết quả kỳ vọng bắt buộc (`Pass Criteria`) | Truy vết AC |
|---|---|---|---|---|
| **`CT-06`** | Hợp đồng | Yêu cầu đồng bộ `/v1/chat/completions` và `/v1/embeddings` qua băng ghi chuẩn. | Biến đổi đúng đường dẫn, tiêu đề, thân JSON và chuẩn hóa phản hồi khớp 100% lược đồ đầu ra kèm `x-tanpopo-request-id`. | `TP-AC-F0101` |
| **`CT-07`** | Hợp đồng | Truyền luồng SSE 50 mảnh liên tiếp xen kẽ dòng giữ kết nối `: ping`. | Chuyển tiếp từng mảnh tức thì không gom đệm toàn luồng; bỏ qua `: ping` không sinh mảnh rỗng sai lệch. | `TP-AC-F0101`, `TP-AC-N01` |
| **`CT-08`** | Phủ định | Gửi yêu cầu đính kèm âm thanh hoặc `strict_json_schema: true` tới đích khai báo không hỗ trợ năng lực đó. | Trả ngay `400 capability_not_supported`, `0` lệnh gọi mạng ra ngoài, hoàn trả ngay khoản giữ chỗ ngân sách. | `TP-AC-F0102` |
| **`CT-09`** | Hợp đồng & Phủ định | Gọi hàm (`tool_calls`) dạng luồng delta hợp lệ vs băng ghi mô hình nội bộ trả JSON gọi hàm bị cụt dấu ngoặc. | Ca hợp lệ ghép mảnh đúng thứ tự chỉ mục; ca lỗi phân loại đúng `upstream_malformed_response` mà không làm sập tiến trình. | `TP-AC-F0202` |
| **`CT-10`** | Phủ định | Băng ghi nhà cung cấp ngắt kết nối TCP đột ngột sau khi đã phát 10 mảnh nội dung SSE (`Partial Stream`). | Phát sự kiện kết thúc `mid_stream_abort`, **tuyệt đối không gọi đích dự phòng**, quyết toán sổ cái với cờ `ESTIMATED`. | `TP-AC-F0103` |
| **`CT-11`** | Hợp đồng | `API Client` hủy kết nối ở mảnh SSE thứ 3; kiểm tra thời gian đóng kết nối giả lập phía nhà cung cấp. | Host ngắt kết nối upstream trong `< 500 ms`, gọi móc dọn dẹp và giải phóng phần hạn mức giữ chỗ còn dư. | `TP-AC-F0104` |
| **`CT-12`** | Hợp đồng | Băng ghi trả HTTP `429` kèm `Retry-After: 12` trước mảnh đầu tiên, hoặc trả đủ token đọc/ghi cache & suy luận ở cuối luồng. | Ca `429` chuyển mạch sang đích dự phòng và đặt `THROTTLED_COOLDOWN = 12s`; ca hoàn tất ghi nhận đủ 5 thành phần usage với cờ `EXACT` (nếu thiếu usage cuối luồng phải gắn `ESTIMATED` / `UNMETERED_FLAGGED`, cấm ghi `EXACT = 0`). | `TP-AC-F0601`, `TP-AC-F0803` |
| **`CT-13`** | Hợp đồng & Phủ định | Gọi đường dẫn phương ngữ gốc `/native/<dialect>/...` chứa trường JSON mới lạ vs tuyến trỏ nhầm sang khác phương ngữ. | Trường JSON lạ được giữ nguyên 100% không bị cắt bỏ; trường hợp lệch phương ngữ bị chặn ngay với `400 dialect_mismatch`. | `TP-AC-F0105` |

### Nhóm C — Kiểm thử Phủ định & Tấn công Vùng Cô lập Bảo mật (`Adversarial Security & Sandbox`)

| Mã Test | Loại | Kịch bản Tấn công / Lạm dụng | Kết quả Phòng vệ Bắt buộc (`Pass Criteria`) | Truy vết AC |
|---|---|---|---|---|
| **`CT-14`** | Bảo mật (Phủ định) | Mã trong Plugin cố tình trả `UpstreamHttpSpec` trỏ tới `https://exfil.attacker.example` không nằm trong `allowedHosts`. | Host Egress Proxy chặn đứng trước khi phân giải DNS/mở socket, hủy yêu cầu, ghi `AuditEvent` bảo mật và chuyển sang `QUARANTINED` theo chính sách. | `TP-AC-SEC-02` |
| **`CT-15`** | Bảo mật (Phủ định) | Cấu hình URL hoặc phản hồi HTTP `302 Redirect` trỏ về `http://169.254.169.254/latest/meta-data/`, `127.0.0.1` hoặc miền thứ ba khác. | Chặn cứng mọi địa chỉ siêu dữ liệu đám mây/loopback; lột sạch tiêu đề `Authorization` khi gặp chuyển hướng khác miền đích đã duyệt. | `TP-AC-SEC-04` |
| **`CT-16`** | Bảo mật (Phủ định) | Plugin kiểm thử cố quét toàn bộ dữ liệu đầu vào/biến môi trường để tìm khóa `sk-live-SECRET`, và nhà cung cấp giả lập nhại lại chuỗi khóa trong thân lỗi `401`. | Không gian bộ nhớ của Plugin chứa `0` byte chuỗi khóa thật (`host_blind_inject`); bộ lọc lỗi của Host xóa sạch chuỗi khóa khỏi nhật ký và phản hồi trả về Client. | `TP-AC-SEC-01` |
| **`CT-17`** | Cô lập Lỗi (Phủ định) | Plugin độc hại chạy vòng lặp vô tận `while(true){}` hoặc cấp phát bộ nhớ vượt ngưỡng `maxMemoryMb` trong khi 100 luồng khỏe mạnh khác đang chạy song song. | Phiên thực thi lỗi bị ngắt cưỡng bức đúng hạn mức CPU/RAM; tiến trình Core `0 crash`; 100 luồng khỏe mạnh hoàn tất bình thường không tăng độ trễ quá ngưỡng. | `TP-AC-SEC-02`, `TP-AC-N02` |
| **`CT-18`** | Bảo mật (Phủ định) | Cập nhật gói từ `0.2.0` lên `0.3.0`, trong đó `0.3.0` lén thêm 1 tên miền mới vào `allowedHosts` hoặc xin đổi sang `scoped_secret_unseal`. | Tự động chặn kích hoạt bản `0.3.0` ở trạng thái `UPDATING_PENDING_RECONSENT`; lưu lượng hiện tại vẫn đi qua `0.2.0` an toàn tuyệt đối. | `TP-AC-F0304` |
| **`CT-19`** | Bảo mật (Phủ định) | Bắn 50 yêu cầu song song vào kết nối `Authorized OAuth` vừa hết hạn token; thử dùng token của `Connection A` gửi sang tên miền của `Connection B`. | Đúng `1` lệnh làm mới được gọi tới IdP; mọi nỗ lực gắn chéo token khác `allowed_audience_hosts` bị chặn cứng tại biên mạng. | `TP-AC-F0203`, `TP-AC-SEC-03`, `04` |
| **`CT-20`** | Tuân thủ (Phủ định) | Nộp gói kiểm định Marketplace chứa cơ chế đọc cookie trình duyệt hoặc nhúng `client_id` đóng của gói thuê bao cá nhân (`ChatGPT Plus/Pro`, `Codex`, `Claude Pro/Max`, `Antigravity`...). | Bộ kiểm tra tuân thủ đánh trượt dứt khoát, từ chối cấp nhãn `official`/`verified`. | `TP-AC-SEC-06` |

### Nhóm D & E — Mặt phẳng MCP, A2A & Tương thích Ngược Khi Nâng cấp Core

| Mã Test | Loại | Kịch bản kiểm thử | Kết quả kỳ vọng bắt buộc (`Pass Criteria`) | Truy vết AC |
|---|---|---|---|---|
| **`CT-21`** | Hợp đồng & Bảo mật | Danh mục 250 công cụ MCP lọc theo khóa ảo qua phân trang; sau đó `MCP Server` sửa 1 từ trong mô tả công cụ đã duyệt. | Phân trang trả đúng các công cụ được cấp quyền không cắt cụt ở 100; công cụ bị sửa mô tả lệch `schema_hash` bị khóa ngay tức khắc (*Rug-Pull Blocked*). | `TP-AC-F0401`, `F0402` |
| **`CT-22`** | Bảo mật (Phủ định) | Đổi tham số của công cụ MCP `destructive` sau khi đã được `Reviewer` duyệt, hoặc giả lập quá hạn mạng sau khi đã gửi lệnh `mutating`. | Đổi tham số bị chặn `403 parameter_hash_mismatch`; quá hạn mạng chỉ gửi đúng **1 lần duy nhất**, không retry mù và trả `ambiguous_side_effect_timeout`. | `TP-AC-F0403`, `TP-AC-SEC-08` |
| **`CT-23`** | Bảo mật (Phủ định) | Chuỗi tác vụ A2A gọi vòng tròn vượt bước nhảy thứ 5 hoặc đòi ngân sách con lớn hơn số dư còn lại của tác nhân cha. | Từ chối ngay với `400 delegation_depth_exceeded` / `403 budget_or_privilege_exceeded`. | `TP-AC-F0404`, `TP-AC-SEC-08` |
| **`CT-24`** | Tương thích Nâng cấp | Chạy gói phần mở rộng hợp đồng `N-1` trên bản Core `N`, và thử nâng cấp Core khi đang có gói hợp đồng cũ `N-2` hết hạn hỗ trợ. | Gói `N-1` chạy không cần sửa đổi; gói `N-2` bị bước `Preflight Upgrade Check` phát hiện và chặn nâng cấp an toàn (`BLOCKED_INCOMPATIBLE`). | `TP-AC-F1002`, `TP-AC-N10` |

---

## 3. Kịch bản Nghiệm thu Đầu-Cuối Bắt buộc: External Provider Adapter Độc lập Hoàn toàn Với Core (`EXT-ACCEPT-01`)

> **Mục tiêu chứng minh sống còn (`MO-02`):** Chứng minh bằng bằng chứng mật mã rằng một nhà cung cấp AI hoàn toàn mới chưa từng có mặt lúc biên dịch Tanpopo Core được phát triển ở kho mã độc lập và vận hành trọn vòng đời sản xuất trên tệp thực thi Core giữ nguyên bất biến.

### Điều kiện Tiền đề Kiểm định
1. Ghi nhận mã băm tệp thực thi nhân hệ thống trước bài kiểm thử: `CORE_BIN_SHA256_BEFORE = sha256sum(tanpopo)`.
2. Khởi chạy tiến trình Tanpopo Core và giữ nguyên `PID` xuyên suốt toàn bộ 10 bước dưới đây (không biên dịch lại, không khởi động lại tiến trình Core).
3. Khởi chạy một máy chủ giả lập nhà cung cấp khu vực mới `Mock-Acme-Sovereign-AI` có giao thức tiêu đề/JSON riêng chưa từng được biết tới trong mã nguồn Core.

### Mười Bước Nghiệm thu Bắt buộc (`Given / When / Then` Chuỗi Liên hoàn)

1. **Khởi tạo & Kiểm thử Ngoại tuyến:** Trong một thư mục trống tách rời hoàn toàn khỏi mã nguồn Core, chạy `tanpopo plugin init` tạo gói `@acme-cloud/regional-llm-adapter@0.2.0`, chạy `tanpopo plugin test` vượt qua toàn bộ các bài `CT-01`..`CT-17` và đóng gói ra tệp `acme-0.2.0.tpkg`.
2. **Cài đặt Chờ Duyệt Quyền:** Nạp gói `acme-0.2.0.tpkg` vào tiến trình Tanpopo đang chạy → Xác nhận trạng thái dừng đúng ở `PENDING_PERMISSION_APPROVAL`, nhãn hiển thị là `unverified-external` và chưa thể định tuyến lưu lượng.
3. **Phê duyệt Quyền & Khóa `tanpopo.lock`:** Quản trị viên duyệt quyền mạng chỉ mở tới địa chỉ của `Mock-Acme-Sovereign-AI` và cơ chế `host_blind_inject` → Gói chuyển sang `ENABLED_ACTIVE` và mã băm SHA-256 được ghi vào `tanpopo.lock`.
4. **Tạo Kết nối, Tuyến & Ngân sách:** Lưu khóa bí mật vào kho một chiều, tạo `Provider Connection` (`Data Region = EU`), bật `Model Deployment` và gắn vào `Route` có trần ngân sách `1.00 USD`.
5. **Gọi Suy luận Hợp nhất & Streaming Thực tế:** Dùng OpenAI SDK tiêu chuẩn gửi truy vấn streaming và gọi công cụ vào `/v1/chat/completions` → Nhận luồng SSE mượt mà, nhà cung cấp đích nhận đúng tiêu đề khóa thật trong khi Sandbox của Adapter không hề nhìn thấy khóa, sổ cái ghi nhận bút toán `SETTLED_EXACT`.
6. **Kiểm chứng Tình huống Lỗi & Vùng Dữ liệu:** Ngắt luồng giả lập ở mảnh thứ 5 → Xác nhận nhận khung `mid_stream_abort` không fallback ngầm và quyết toán `SETTLED_ESTIMATED`; tắt đích `EU` khi chỉ còn đích dự phòng `US` → Xác nhận trả `503 no_compliant_deployment_available`.
7. **Chặn Đứng Cập nhật Leo thang Quyền:** Tải lên bản `acme-0.3.0.tpkg` có cài thêm tên miền lạ trong `Manifest` → Xác nhận bản `0.3.0` bị khóa ở `UPDATING_PENDING_RECONSENT` và các truy vấn đang gọi liên tục vẫn đi qua bản `0.2.0` với `0` lỗi rớt mạng.
8. **Nâng cấp Hợp lệ & Quay lui Tức thì (`Rollback`):** Nạp bản `0.2.1` không tăng quyền (chuyển lưu lượng êm qua `DRAINING`), sau đó chạy lệnh `Rollback` về `0.2.0` → Xác nhận lưu lượng quay lại `0.2.0` trong vài giây.
9. **Cách ly Khẩn cấp (`Quarantine`) & Điều tra:** Kích hoạt lệnh cách ly khẩn cấp đối với `@acme-cloud/regional-llm-adapter` → Trong vòng `< 1 giây` (đơn nút), mọi truy vấn mới bị chặn với `503 extension_quarantined` và truy vấn kiểm toán trả về đúng danh sách `Virtual Key` / `Project` đã đi qua gói này.
10. **Đối chiếu Bất biến Nhân Core:** Kiểm tra lại `sha256sum(tanpopo) == CORE_BIN_SHA256_BEFORE` và `PID` của tiến trình Core không đổi — **chứng minh hoàn chỉnh tính độc lập tuyệt đối giữa hệ sinh thái Extension và chu kỳ phát hành Tanpopo Core**.
