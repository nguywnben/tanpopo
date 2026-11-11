# Ma trận Kiểm thử Hợp đồng, Xác thực/Ủy quyền, Phủ định & Nhiễu loạn (Contract, AuthN/AuthZ & Fuzz Test Matrix) — Tanpopo

> **Tài liệu Đảm bảo Chất lượng & Kiểm định Hợp đồng (Updated in Phase ASTRA A07 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — NO CODE`)**
> - **Mã tài liệu:** `SPEC-QA-01` | **Phiên bản:** `0.2.0-step07-draft` | **Ngày cập nhật:** `2026-11-12`
> - **Vai trò biên soạn:** Principal Security Engineer, QA Architect & Platform SDK Lead
> - **Mục đích cốt lõi:** Định nghĩa bộ khung kiểm thử băng ghi tất định chạy `100%` ngoại tuyến (`tanpopo plugin test`), ma trận kiểm thử xác thực/ủy quyền/cô lập thuê bao (`AUTH-01`..`16`), các mục tiêu kiểm thử nhiễu loạn & thuộc tính (`FZ-01`..`06`), cùng kịch bản nghiệm thu đầu-cuối chứng minh **`Provider Adapter` bên ngoài vận hành trọn vòng đời sản xuất mà không sửa hay biên dịch lại Tanpopo Core (`EXT-ACCEPT-01`)**.
> - **Truy vết Yêu cầu:** `DEC-002`..`004`, `MO-02`, `MO-04`, `TP-FR-0101`..`1005`, `TP-NFR-0401`..`0502`, `TP-AC-F0101`..`F1003`, `TP-AC-SEC-01`..`08`, `SPEC-SEC-01`..`03`.

---

## 1. Kiến trúc Bộ Kiểm thử Băng ghi Tất định Ngoại tuyến (`Deterministic Cassette Harness`)

Mọi gói phần mở rộng (`Official`, `Verified`, `Private-Enterprise` và `Unverified-External`) đều được kiểm định bằng cùng một công cụ `tanpopo plugin test` dựa trên các tệp băng ghi cố định (`Cassettes` — cặp bản tin HTTP/SSE giả lập của nhà cung cấp và đầu ra kỳ vọng của Host):
- **Chạy `100%` ngoại tuyến trong CI dưới vài giây:** Cấm tuyệt đối mở kết nối ra Internet thật (`TANPOPO_TEST_HERMETIC=1`), không cần khóa API thật, **`0 USD` chi phí gọi hãng AI ngoài** và không bị nhiễu mạng.
- **Quyết định Cấp độ Đảm bảo Năng lực (`Capability Assurance Level`):** Chỉ những năng lực trong `Manifest` có bài băng ghi vượt qua ma trận dưới đây mới được nâng từ `DECLARED` lên `TESTED` (xem [`provider-adapter.md`](../specifications/provider-adapter.md)).

---

## 2. Ma trận Kiểm thử Hợp đồng & Đối kháng Mở rộng (`CT-01` .. `CT-32`)

### Nhóm A — Kiểm định Đóng gói, Lược đồ `Manifest` & Chuỗi Cung ứng (`Packaging & Provenance`)

| Mã Test | Loại | Kịch bản & Đầu vào kiểm thử | Kết quả kỳ vọng bắt buộc (`Pass Criteria`) | Truy vết AC / TM |
|---|---|---|---|---|
| **`CT-01`** | Hợp đồng | Kiểm tra cú pháp `Manifest`, `configSchema`, `secretSlots` và `permissions` hợp lệ. | Đạt `100%` kiểm tra JSON Schema; từ chối mọi trường quyền không xác định hoặc quyền mạng `*` mặc định. | `TP-AC-F0302` |
| **`CT-02`** | Phủ định | Gói khai báo `contractVersion: "9.0"` không nằm trong dải hỗ trợ của Core hiện hành. | Chặn ngay ở bước `VERIFYING` (`SM-01` → `REJECTED_INVALID`) với mã lỗi nêu rõ dải phiên bản hỗ trợ. | `TP-AC-F0303` |
| **`CT-03`** | Phủ định | Sửa đổi `1 byte` trong tệp `.wasm` / khai báo sau khi đã ghi mã băm vào `tanpopo.lock` hoặc chữ ký số. | Từ chối nạp gói (`artifact_digest_mismatch`), ghi `AuditEvent` bảo mật mức cao. | `TP-AC-SEC-02`, `TM-02` |
| **`CT-04`** | Phủ định | Gói nén `.tpkg`/`.zip` chứa tệp thoát thư mục `../../etc/passwd` (*Zip-Slip*), `symlink` ra ngoài hoặc giải nén phình đại `> 64 MiB` (*Zip-Bomb*). | Trình giải nén hủy ngay lập tức tại luồng đọc bộ nhớ giới hạn, không ghi bất kỳ byte nào ra đĩa. | `TP-AC-SEC-02`, `TM-01` |
| **`CT-05`** | Phủ định | Gói bên ngoài tự điền `"trustTier": "official"` hoặc lấy tiền tố `@tanpopo-official/*` nhưng không có chữ ký khóa gốc ngoại tuyến của dự án Tanpopo. | Bác bỏ nhãn giả mạo và chặn chiếm dụng không gian tên dành riêng (`403 reserved_namespace_spoofing`). | `TP-AC-SEC-02`, `TM-03` |

### Nhóm B — Tương thích Giao thức, Truyền luồng & Số đo của `Provider Adapter` (`Protocol & Metering`)

| Mã Test | Loại | Kịch bản & Đầu vào kiểm thử | Kết quả kỳ vọng bắt buộc (`Pass Criteria`) | Truy vết AC / TM |
|---|---|---|---|---|
| **`CT-06`** | Hợp đồng | Yêu cầu đồng bộ `/v1/chat/completions` và `/v1/embeddings` qua băng ghi chuẩn. | Biến đổi đúng đường dẫn, tiêu đề, thân JSON và chuẩn hóa phản hồi khớp `100%` lược đồ đầu ra kèm `x-tanpopo-request-id`. | `TP-AC-F0101` |
| **`CT-07`** | Hợp đồng | Truyền luồng SSE 50 mảnh liên tiếp xen kẽ dòng giữ kết nối `: ping`. | Chuyển tiếp từng mảnh tức thì không gom đệm toàn luồng; bỏ qua `: ping` không sinh mảnh rỗng sai lệch. | `TP-AC-F0101`, `TP-AC-N01` |
| **`CT-08`** | Phủ định | Gửi yêu cầu đính kèm âm thanh, `strict_json_schema: true` hoặc vi phạm `allowed_data_regions = ["EU"]`. | Trả ngay `400 capability_not_supported` (hoặc `503 no_compliant_deployment_available`), `0` lệnh gọi mạng sai vùng, hoàn trả ngay khoản giữ chỗ. | `TP-AC-F0102`, `TP-AC-F0602` |
| **`CT-09`** | Hợp đồng & Phủ định | Gọi hàm (`tool_calls`) dạng luồng delta hợp lệ vs băng ghi mô hình nội bộ trả JSON gọi hàm bị cụt dấu ngoặc. | Ca hợp lệ ghép mảnh đúng thứ tự chỉ mục; ca lỗi phân loại đúng `upstream_malformed_response` mà không làm sập tiến trình. | `TP-AC-F0202` |
| **`CT-10`** | Phủ định | Băng ghi nhà cung cấp ngắt kết nối TCP đột ngột sau khi đã phát 10 mảnh nội dung SSE (`Partial Stream`). | Phát sự kiện kết thúc `mid_stream_abort`, **tuyệt đối không gọi đích dự phòng**, quyết toán sổ cái với cờ `ESTIMATED`. | `TP-AC-F0103`, `FMEA-01` |
| **`CT-11`** | Hợp đồng | `API Client` hủy kết nối ở mảnh SSE thứ 3; kiểm tra thời gian đóng kết nối giả lập phía nhà cung cấp. | Host ngắt kết nối upstream trong `< 500 ms` (`TARGET / UNVERIFIED`), gọi móc dọn dẹp và giải phóng hạn mức giữ chỗ còn dư. | `TP-AC-F0104` |
| **`CT-12`** | Hợp đồng | Băng ghi trả HTTP `429` kèm `Retry-After: 12` trước mảnh đầu tiên, hoặc trả đủ/thiếu usage ở cuối luồng. | Ca `429` chuyển mạch sang đích dự phòng và đặt `THROTTLED_COOLDOWN = 12s`; ca thiếu usage gắn `ESTIMATED` / `UNMETERED_FLAGGED`, cấm ghi `EXACT = 0`. | `TP-AC-F0601`, `TP-AC-F0803`, `FMEA-04` |
| **`CT-13`** | Hợp đồng & Phủ định | Gọi đường dẫn phương ngữ gốc `/native/<dialect>/...` chứa trường JSON mới lạ vs tuyến trỏ nhầm sang khác phương ngữ. | Trường JSON lạ được giữ nguyên `100%` không bị cắt bỏ; trường hợp lệch phương ngữ bị chặn ngay với `400 dialect_mismatch`. | `TP-AC-F0105` |

### Nhóm C — Kiểm thử Phủ định & Tấn công Vùng Cô lập Bảo mật (`Adversarial Security & Sandbox`)

| Mã Test | Loại | Kịch bản Tấn công / Lạm dụng | Kết quả Phòng vệ Bắt buộc (`Pass Criteria`) | Truy vết AC / TM |
|---|---|---|---|---|
| **`CT-14`** | Bảo mật (Phủ định) | Mã trong Plugin cố tình trả `UpstreamHttpSpec` trỏ tới `https://exfil.attacker.example` không nằm trong `allowedHosts`. | Host Egress Proxy chặn đứng trước khi phân giải DNS/mở socket, hủy yêu cầu, ghi `AuditEvent` bảo mật và chuyển sang `QUARANTINED` theo chính sách. | `TP-AC-SEC-02`, `TM-05` |
| **`CT-15`** | Bảo mật (Phủ định) | Cấu hình URL hoặc phản hồi HTTP `302 Redirect` trỏ về `http://169.254.169.254/latest/meta-data/`, `127.0.0.1` hoặc miền thứ ba khác. | Chặn cứng mọi địa chỉ siêu dữ liệu đám mây/loopback; lột sạch tiêu đề `Authorization` khi gặp chuyển hướng khác miền đích đã duyệt. | `TP-AC-SEC-04`, `TM-06` |
| **`CT-16`** | Bảo mật (Phủ định) | Plugin kiểm thử cố quét toàn bộ dữ liệu đầu vào/biến môi trường để tìm khóa `sk-live-SECRET`, và nhà cung cấp giả lập nhại lại chuỗi khóa trong thân lỗi `401`. | Không gian bộ nhớ của Plugin chứa `0` byte chuỗi khóa thật (`host_blind_inject`); bộ lọc lỗi của Host xóa sạch chuỗi khóa khỏi nhật ký và phản hồi trả về Client. | `TP-AC-SEC-01`, `TM-05` |
| **`CT-17`** | Cô lập Lỗi (Phủ định) | Plugin độc hại chạy vòng lặp vô tận `while(true){}` hoặc cấp phát bộ nhớ vượt ngưỡng `maxMemoryMb` trong khi 100 luồng khỏe mạnh khác đang chạy song song. | Phiên thực thi lỗi bị ngắt cưỡng bức đúng hạn mức CPU/RAM; tiến trình Core `0 crash`; 100 luồng khỏe mạnh hoàn tất bình thường (`FMEA-05`). | `TP-AC-SEC-02`, `TP-AC-N02`, `TM-04` |
| **`CT-18`** | Bảo mật (Phủ định) | Cập nhật gói từ `0.2.0` lên `0.3.0`, trong đó `0.3.0` lén thêm 1 tên miền mới vào `allowedHosts` hoặc xin đổi sang `scoped_secret_unseal`. | Tự động chặn kích hoạt bản `0.3.0` ở trạng thái `UPDATING_PENDING_RECONSENT`; lưu lượng hiện tại vẫn đi qua `0.2.0` an toàn tuyệt đối. | `TP-AC-F0304` |
| **`CT-19`** | Bảo mật (Phủ định) | Bắn 50 yêu cầu song song vào kết nối `Authorized OAuth` vừa hết hạn token; thử dùng token của `Connection A` gửi sang tên miền của `Connection B`. | Đúng `1` lệnh làm mới được gọi tới IdP (*Single-Flight*); mọi nỗ lực gắn chéo token khác `allowed_audience_hosts` bị chặn cứng tại biên mạng. | `TP-AC-F0203`, `TP-AC-SEC-03`, `TM-08` |
| **`CT-20`** | Tuân thủ (Phủ định) | Nộp gói kiểm định Marketplace chứa cơ chế đọc cookie trình duyệt hoặc nhúng `client_id` đóng của gói thuê bao cá nhân (`ChatGPT Plus/Pro`, `Codex`, `Claude Pro/Max`, `Antigravity`...). | Bộ kiểm tra tuân thủ đánh trượt dứt khoát, từ chối cấp nhãn `official`/`verified`. | `TP-AC-SEC-06`, `DEC-004` |

### Nhóm D & E — Mặt phẳng MCP, A2A & Tương thích Ngược Khi Nâng cấp Core

| Mã Test | Loại | Kịch bản kiểm thử | Kết quả kỳ vọng bắt buộc (`Pass Criteria`) | Truy vết AC / TM |
|---|---|---|---|---|
| **`CT-21`** | Hợp đồng & Bảo mật | Danh mục 250 công cụ MCP lọc theo khóa ảo qua phân trang; sau đó `MCP Server` sửa 1 từ trong mô tả công cụ đã duyệt. | Phân trang trả đúng các công cụ được cấp quyền không cắt cụt ở 100; công cụ bị sửa mô tả lệch `schema_hash` bị khóa ngay tức khắc (*Rug-Pull Blocked*). | `TP-AC-F0401`, `F0402`, `TM-07` |
| **`CT-22`** | Bảo mật (Phủ định) | Đổi tham số của công cụ MCP `destructive` sau khi đã được `Reviewer` duyệt, hoặc giả lập quá hạn mạng sau khi đã gửi lệnh `mutating`. | Đổi tham số bị chặn `403 parameter_hash_mismatch`; quá hạn mạng chỉ gửi đúng **1 lần duy nhất**, không retry mù và trả `ambiguous_side_effect_timeout`. | `TP-AC-F0403`, `FMEA-02`, `TM-07` |
| **`CT-23`** | Bảo mật (Phủ định) | Chuỗi tác vụ A2A gọi vòng tròn vượt bước nhảy thứ 5 hoặc đòi ngân sách con lớn hơn số dư còn lại của tác nhân cha. | Từ chối ngay với `400 delegation_depth_exceeded` / `403 budget_or_privilege_exceeded`. | `TP-AC-F0404`, `TM-12` |
| **`CT-24`** | Tương thích Nâng cấp | Chạy gói phần mở rộng hợp đồng `N-1` trên bản Core `N`, và thử nâng cấp Core khi đang có gói hợp đồng cũ `N-2` hết hạn hỗ trợ. | Gói `N-1` chạy không cần sửa đổi; gói `N-2` bị bước `Preflight Upgrade Check` phát hiện và chặn nâng cấp an toàn (`BLOCKED_INCOMPATIBLE`). | `TP-AC-F1002`, `TP-AC-N10` |

### Nhóm F — Chuỗi Cung ứng Sâu, `DNS Rebinding`, Bộ nhớ Đệm Đa Thuê bao, Đua tranh Ngân sách, Kiểm toán & Di trú (`CT-25` .. `CT-32` — Bổ sung A07)

| Mã Test | Loại | Kịch bản Đối kháng / Hỏng hóc | Kết quả Phòng vệ Bắt buộc (`Pass Criteria`) | Truy vết AC / TM |
|---|---|---|---|---|
| **`CT-25`** | Chuỗi cung ứng (Phủ định) | Cài đặt phần mở rộng ở môi trường `prod` bằng nhánh/thẻ Git động (`ref=main` hoặc `tag=v1.0`) không kèm `commit SHA` 40 ký tự và `expected_sha256`. | Từ chối ngay lập tức với `400 mutable_git_ref_forbidden` trước khi nạp bất kỳ gói nào. | `TM-02`, `SPEC-SEC-02` |
| **`CT-26`** | Bảo mật Mạng (Phủ định) | Máy chủ DNS giả lập trả IP công khai hợp lệ ở lần hỏi đầu (`TTL=0`), sau đó đổi sang `169.254.169.254` hoặc `127.0.0.1` ngay khoảnh khắc mở kết nối TCP. | Móc kiểm tra IP bên trong `net.Dialer.Control` (lúc `connect()`) chặn đứng tức khắc; **`0` gói tin TCP SYN** chạm vào địa chỉ nội bộ/metadata. | `TP-AC-SEC-04`, `TM-06`, `RSK-014` |
| **`CT-27`** | Bảo mật Đầu ra (Phủ định) | Phản hồi từ mô hình/MCP chứa mã kịch bản độc hại (`javascript:` URI, thuộc tính sự kiện HTML) và chuỗi thoát thiết bị đầu cuối (`\x1b]52;...` chiếm quyền clipboard). | Web Console mã hóa văn bản thuần `100%` dưới `CSP` chặt (`0` kịch bản thực thi); `tanpopo` CLI lọc sạch ký tự điều khiển ANSI nguy hiểm trước khi in ra màn hình. | `TM-07`, `UX-AC-05` |
| **`CT-28`** | Quyền riêng tư Cache (Phủ định) | Bật bộ nhớ đệm phản hồi; `Org A` gửi truy vấn chứa bí mật nội bộ (lưu vào cache), sau đó `Org B` (hoặc môi trường `staging`, hoặc `Virtual Key` có chính sách lọc khác) gửi đúng cùng câu hỏi và mô hình. | **`0` lần trúng cache chéo tổ chức / môi trường / chính sách**; khóa cache bắt buộc phân tách theo `org_id:project_id:env_id:deployment_id:policy_hash`. | `TM-09`, `FMEA-03`, `RSK-016` |
| **`CT-29`** | Tài chính (Đua tranh) | Số dư ngân sách còn đủ cho đúng `5` lần giữ chỗ; bắn đồng thời `50` yêu cầu suy luận song song tại cùng một thời điểm. | Tối đa `5` yêu cầu được mở kết nối tới hãng AI; ít nhất `45` yêu cầu bị từ chối tức thì `402 budget_exhausted`; **`0` vượt trần ngân sách bất ngờ**. | `TP-AC-F0801`, `TM-12`, `RSK-007` |
| **`CT-30`** | Toàn vẹn Kiểm toán (Phủ định) | Dùng quyền quản trị DB trực tiếp sửa `1` ký tự hoặc xóa `1` dòng ở giữa bảng `audit_events`, sau đó chạy `tanpopo audit verify`. | Phát hiện chính xác vị trí dòng gãy chuỗi băm `SHA-256`, trả mã thoát khác `0` và kích hoạt cảnh báo `ALT-AUDIT-TAMPER`. | `TP-AC-F0701`, `TM-13`, `OPS-AC-08` |
| **`CT-31`** | Mạng kín `Air-Gap` (Phủ định) | Nhập gói ngoại tuyến `tanpopo bundle import` có chữ ký gốc sai, danh sách `revocations.json` quá hạn hoặc `1 byte` gói `.tpkg` bên trong bị tráo. | Huỷ toàn bộ giao dịch nhập gói nguyên tử, giữ nguyên kho cục bộ hiện hành và báo rõ tệp vi phạm. | `SPEC-OPS-01`, `OPS-AC-02` |
| **`CT-32`** | Toàn vẹn Di trú DB | Chạy `tanpopo db migrate-engine` chuyển dữ liệu thực tế từ `SQLite WAL` sang `PostgreSQL 16+` rồi chạy kiểm tra đối soát. | Khớp `100%` số dòng, tổng sổ cái `BIGINT micro-USD`, chuỗi băm `audit_events` và giải mã thành công kho khóa `AEAD AAD` với `0` khóa rõ rò rỉ ra đĩa/log. | `FMEA-06`, `OPS-AC-06`, `RSK-013` |

---

## 3. Ma trận Kiểm thử Xác thực, Ủy quyền & Cô lập Thuê bao (`AUTH-01` .. `AUTH-16`)

> Phân biệt rạch ròi giữa **kiểm chứng kỹ thuật giao thức xác thực** và **cưỡng chế ranh giới chính sách của nhà cung cấp (`DEC-004`)**:

| Mã Test | Hạng mục Xác thực / Ủy quyền | Kịch bản Kiểm thử Dương tính & Phủ định | Tiêu chí Đạt Bắt buộc (`Pass Criteria`) |
|---|---|---|---|
| **`AUTH-01`** | **Đăng nhập Quản trị `OIDC + PKCE`** | Đăng nhập Control Plane qua `FakeOAuthIdP` chuẩn `Authorization Code + PKCE (S256)` kèm kiểm tra `state` và `nonce`. | Tạo phiên quản trị hợp lệ với cờ cookie `HttpOnly; Secure; SameSite=Strict`, ánh xạ đúng vai trò RBAC từ nhóm OIDC. |
| **`AUTH-02`** | **Token OIDC Hết hạn / Giả mạo / Dò Mật khẩu** | Gửi JWT quản trị hết hạn (`exp`), lệch đồng hồ quá ngưỡng, sai chữ ký `JWKS`, sai `iss`/`aud`, hoặc bắn 30 lần đăng nhập sai liên tiếp. | Mọi JWT sai/hết hạn bị chặn `401`; dò mật khẩu bị hãm tốc độ `429 Retry-After` và ghi sự kiện kiểm toán. |
| **`AUTH-03`** | **Chống Giả mạo Yêu cầu Chéo Trang (`CSRF`)** | Trình duyệt có cookie quản trị hợp lệ nhưng gửi `POST /admin/v1/keys` thiếu `X-CSRF-Token` hoặc tiêu đề `Origin` từ miền lạ. | Từ chối cứng `403 csrf_validation_failed`, `0` thay đổi trạng thái hệ thống. |
| **`AUTH-04`** | **Khóa Ảo `Virtual Key` & Khóa Môi trường** | Xác thực `/v1/chat/completions` bằng khóa `vk_tp_staging_...` hợp lệ vào tuyến `staging` vs thử gọi vào tuyến chỉ mở cho `prod`. | Đối chiếu băm hằng định thời gian; đúng môi trường cho qua (`200`), lệch môi trường chặn ngay (`403 environment_scope_denied`). |
| **`AUTH-05`** | **Xoay Khóa Hai Chiều Êm (`Grace Window`)** | Xoay `Virtual Key` với cửa sổ ân hạn `grace_ttl = 60s` trên đồng hồ giả lập `FakeClock`. | Tại `T + 30s` cả khóa cũ và mới đều hoạt động; đúng tại `T + 61s` khóa cũ trả `401 key_expired` tức thì (`SM-05`). |
| **`AUTH-06`** | **Thu hồi Khẩn cấp (`Emergency Revoke`)** | Kích hoạt thu hồi an ninh khẩn cấp một `Virtual Key` đang có `10` luồng SSE truyền dở trên cụm nhiều nút. | Nút nhận tin cắt đứt cả `10` luồng đang mở và chặn mọi yêu cầu mới trong `< 1s` (đơn nút) / `< 5s` (cụm — `TARGET / UNVERIFIED`). |
| **`AUTH-07`** | **Chặn Vượt Rào Giữa Hai Mặt phẳng (`Plane Crossover`)** | Dùng `Virtual Key` (`vk_tp_...`) gọi bất kỳ API `/admin/v1/*` nào, hoặc dùng cookie quản trị gọi thẳng `/v1/chat/completions`. | Cả hai chiều đều bị bác bỏ `401 invalid_credential_plane`. |
| **`AUTH-08`** | **Tín nhiệm Liên Dịch vụ (`Service-to-Service mTLS / UDS`)** | Tiến trình lạ gọi vào cổng `ExtensionRuntimeService` của `Tier-3 Sidecar` không có chứng chỉ `mTLS` hợp lệ hoặc lệch định danh `SPIFFE SAN`. | Ngắt kết nối ngay ở bước bắt tay TLS/UDS, `0` khung RPC được giải mã. |
| **`AUTH-09`** | **Luồng `Authorized OAuth 2.1` Hợp lệ Cho Nhà cung cấp** | Cấp quyền kết nối tới cổng AI doanh nghiệp / hãng đối tác hợp lệ qua `OAuth 2.1 + PKCE` và `Client Credentials`; hết hạn tự làm mới 1 lần duy nhất. | Lưu cặp token bằng mã hóa phong bì `AEAD AAD`; làm mới ngầm không làm hỏng các truy vấn song song. |
| **`AUTH-10`** | **Token OAuth Bị Hãng Đích Thu hồi (`Revoked Upstream`)** | Máy chủ ủy quyền trả `400 invalid_grant` khi Tanpopo làm mới `refresh_token`. | Dừng ngay mọi nỗ lực thử lại mù (`0` bão retry), chuyển kết nối sang `REAUTH_REQUIRED` (`SM-03`), phát cảnh báo `ALT-OAUTH-REAUTH` (`FMEA-08`). |
| **`AUTH-11`** | **Ma trận Phân quyền Từng Hành động (`Action RBAC`)** | Kiểm thử tự động toàn bộ tổ hợp `(6 Vai trò × Mọi Endpoint /admin/v1/*)` và kiểm tra khởi động máy chủ khi có route quên gắn quyền. | Vai trò `DEVELOPER` / `FINOPS_VIEWER` bị chặn `403` trên mọi thao tác duyệt plugin, xoay khóa hãng hoặc sửa chính sách; máy chủ từ chối khởi động nếu sót route chưa khai báo quyền. |
| **`AUTH-12`** | **Nguyên tắc Bốn Mắt (`Four-Eyes Duty Separation`)** | Cùng một quản trị viên vừa tải lên gói `unverified-external` (hoặc yêu cầu chạy công cụ MCP `destructive`) vừa tự bấm nút `Approve` khi cờ `Four-Eyes` đang bật. | Bị chặn cứng `403 self_approval_forbidden`; chỉ thành công khi một `SECURITY_REVIEWER` độc lập thứ hai ký duyệt. |
| **`AUTH-13`** | **Chống Giả mạo `org_id` Trong Thân Yêu cầu** | Quản trị viên của `Org A` gửi `POST /admin/v1/routes` kèm trường JSON `"org_id": "<UUID_Org_B>"`. | Hệ thống bác bỏ hoặc ghi đè bằng `org_id` thật của phiên xác thực; `0` bản ghi nào được tạo dưới tên `Org B`. |
| **`AUTH-14`** | **Bảo vệ Cấy ghép Bản mã `AEAD AAD` & `PostgreSQL RLS`** | (1) Dùng SQL chép `secret_ciphertext` của `Org A` sang dòng kết nối của `Org B`; (2) Chạy truy vấn thiếu `WHERE` dưới biến phiên `app.current_org_id = Org_B`. | (1) Giải mã `AES-256-GCM` thất bại tức khắc do sai `AAD`; (2) `FORCE RLS` trả về đúng `0` dòng của `Org A`. |
| **`AUTH-15`** | **Chống Dò Quét Đối tượng Chéo Tổ chức (`Anti-Oracle IDOR`)** | Chủ thể của `Org A` gọi `GET / PATCH / DELETE` lên `Connection`, `Virtual Key`, `Route` và `Trace` của `Org B`. | Trả đồng nhất `404 resource_not_found` (không lộ `403`), đồng thời ghi sự kiện `cross_tenant_probe_blocked` vào nhật ký kiểm toán. |
| **`AUTH-16`** | **Phân biệt Hỗ trợ Kỹ thuật vs Cấm Lạm dụng Thuê bao & Chuyển tiếp Token (`DEC-004` + `RFC 8707`)** | (1) Thử chuyển tiếp token quản trị của người dùng sang máy chủ MCP ngoài hoặc đổi miền đích của kết nối OAuth mà không xin cấp quyền lại; (2) Thử nạp cấu hình `billing_mode = CONSUMER_SUBSCRIPTION` hoặc trình trích xuất cookie thuê bao cá nhân. | (1) Chặn cứng mọi hành vi chuyển tiếp chéo khán giả (`403 token_audience_mismatch`, vô hiệu hóa token khi đổi miền); (2) Chặn dứt khoát ở cả lược đồ dữ liệu, cổng kiểm định Marketplace và rào chắn giao diện (`UX-GR-03`) — khẳng định việc hỗ trợ giao thức `OAuth 2.1` chuẩn hoàn toàn tách biệt với việc bác bỏ hành vi lách điều khoản thuê bao tiêu dùng. |

---

## 4. Kiểm thử Nhiễu loạn & Bất biến Thuộc tính (`FZ-01` .. `FZ-06` — Go Fuzzing & Property Tests)

| Mã Fuzz | Mục tiêu Kiểm thử (`Target Function`) | Thuộc tính Bất biến Phải Giữ Vững Qua Hàng Triệu Đầu vào Ngẫu nhiên |
|---|---|---|
| **`FZ-01`** | **`FuzzManifestAndDeclarativeExpr`** | Trình phân tích `Manifest` và biểu thức ánh xạ Tầng 1 không bao giờ `panic`, không cấp phát quá `2 MiB` RAM và kết thúc dưới `5 ms` trước mọi chuỗi JSON/template dị dạng. |
| **`FZ-02`** | **`FuzzPackageArchiveExtractor`** | Trình đọc gói `.tpkg` / `.zip` / `.tar.zst` không bao giờ tạo tệp ngoài thư mục đích, không đi theo `symlink`, và hủy ngay khi tổng byte giải nén vượt trần `64 MiB`. |
| **`FZ-03`** | **`FuzzSSEStreamFrameParser`** | Bộ tách khung luồng `SSE` xử lý an toàn mọi kiểu cắt mảnh TCP ở giữa ký tự `UTF-8` đa byte, dòng không có ký tự xuống dòng dài `10 MiB` và khung JSON hỏng mà `0` `panic` và không rò rỉ bộ đệm. |
| **`FZ-04`** | **`FuzzJCSSchemaCanonicalizer`** | Kiểm thử thuộc tính chuẩn hóa `RFC 8785 (JCS)` cho công cụ MCP: đảo thứ tự khóa JSON hoặc thêm khoảng trắng luôn cho ra **cùng một `schema_hash`**, nhưng đổi bất kỳ `1` ký tự nào trong `description` hoặc kiểu tham số luôn tạo ra `schema_hash` khác biệt. |
| **`FZ-05`** | **`FuzzWasmHostGuestABI`** | Giả lập khách Wasm độc hại trả về con trỏ âm, độ dài vượt biên vùng nhớ tuyến tính hoặc chuỗi không hợp lệ sang Host → Host bắt lỗi gọn gàng (`guest_abi_violation`), **`0` đọc/ghi tràn vùng nhớ của tiến trình Core**. |
| **`FZ-06`** | **`PropertyLedgerConservation`** | Chạy máy trạng thái ngẫu nhiên `10.000` bước xen kẽ `Reserve`, `SettleExact`, `SettleEstimated`, `CancelHold`, `ExpireHold` trên số nguyên `BIGINT micro-USD` → Luôn thỏa mãn đẳng thức bảo toàn tuyệt đối: $\text{Spent} + \text{ActiveHolds} + \text{Remaining} = \text{Limit} + \text{RecordedOverage}$ với **`0` sai số làm tròn**. |

---

## 5. Kịch bản Nghiệm thu Đầu-Cuối Bắt buộc: External Provider Adapter Độc lập Hoàn toàn Với Core (`EXT-ACCEPT-01`)

> **Mục tiêu chứng minh sống còn (`MO-02`):** Chứng minh bằng bằng chứng mật mã rằng một nhà cung cấp AI hoàn toàn mới chưa từng có mặt lúc biên dịch Tanpopo Core được phát triển ở kho mã độc lập và vận hành trọn vòng đời sản xuất trên tệp thực thi Core giữ nguyên bất biến.

### Điều kiện Tiền đề Kiểm định
1. Ghi nhận mã băm tệp thực thi nhân hệ thống trước bài kiểm thử: `CORE_BIN_SHA256_BEFORE = sha256sum(tanpopod)`.
2. Khởi chạy tiến trình Tanpopo Core và giữ nguyên `PID` xuyên suốt toàn bộ 10 bước dưới đây (không biên dịch lại, không khởi động lại tiến trình Core).
3. Khởi chạy máy chủ giả lập nhà cung cấp khu vực mới `Mock-Acme-Sovereign-AI` trên cổng loopback cục bộ có giao thức tiêu đề/JSON riêng chưa từng được biết tới trong mã nguồn Core.

### Mười Bước Nghiệm thu Bắt buộc (`Given / When / Then` Chuỗi Liên hoàn)

1. **Khởi tạo & Kiểm thử Ngoại tuyến:** Trong một thư mục trống tách rời hoàn toàn khỏi mã nguồn Core, chạy `tanpopo plugin init` tạo gói `@acme-cloud/regional-llm-adapter@0.2.0`, chạy `tanpopo plugin test` vượt qua toàn bộ các bài `CT-01`..`CT-17` và đóng gói ra tệp `acme-0.2.0.tpkg`.
2. **Cài đặt Chờ Duyệt Quyền:** Nạp gói `acme-0.2.0.tpkg` vào tiến trình Tanpopo đang chạy → Xác nhận trạng thái dừng đúng ở `PENDING_PERMISSION_APPROVAL`, nhãn hiển thị là `unverified-external` và chưa thể định tuyến lưu lượng.
3. **Phê duyệt Quyền & Khóa `tanpopo.lock`:** Quản trị viên duyệt quyền mạng chỉ mở tới địa chỉ của `Mock-Acme-Sovereign-AI` và cơ chế `host_blind_inject` → Gói chuyển sang `ENABLED_ACTIVE` và mã băm SHA-256 được ghi vào `tanpopo.lock`.
4. **Tạo Kết nối, Tuyến & Ngân sách:** Lưu khóa bí mật vào kho một chiều, tạo `Provider Connection` (`Data Region = EU`), bật `Model Deployment` và gắn vào `Route` có trần ngân sách `1.00 USD`.
5. **Gọi Suy luận Hợp nhất & Streaming Thực tế:** Dùng OpenAI SDK tiêu chuẩn gửi truy vấn streaming và gọi công cụ vào `/v1/chat/completions` → Nhận luồng SSE mượt mà, nhà cung cấp đích nhận đúng tiêu đề khóa thật trong khi Sandbox của Adapter không hề nhìn thấy khóa, sổ cái ghi nhận bút toán `SETTLED_EXACT`.
6. **Kiểm chứng Tình huống Lỗi & Vùng Dữ liệu:** Ngắt luồng giả lập ở mảnh thứ 5 → Xác nhận nhận khung `mid_stream_abort` không fallback ngầm và quyết toán `SETTLED_ESTIMATED`; tắt đích `EU` khi chỉ còn đích dự phòng `US` → Xác nhận trả `503 no_compliant_deployment_available`.
7. **Chặn Đứng Cập nhật Leo thang Quyền:** Tải lên bản `acme-0.3.0.tpkg` có cài thêm tên miền lạ trong `Manifest` → Xác nhận bản `0.3.0` bị khóa ở `UPDATING_PENDING_RECONSENT` và các truy vấn đang gọi liên tục vẫn đi qua bản `0.2.0` với `0` lỗi rớt mạng.
8. **Nâng cấp Hợp lệ & Quay lui Tức thì (`Rollback`):** Nạp bản `0.2.1` không tăng quyền (chuyển lưu lượng êm qua `DRAINING`), sau đó chạy lệnh `Rollback` về `0.2.0` → Xác nhận lưu lượng quay lại `0.2.0` trong vài giây.
9. **Cách ly Khẩn cấp (`Quarantine`) & Điều tra:** Kích hoạt lệnh cách ly khẩn cấp đối với `@acme-cloud/regional-llm-adapter` → Trong vòng `< 1 giây` (đơn nút — `TARGET / UNVERIFIED`), mọi truy vấn mới bị chặn với `503 extension_quarantined` và truy vấn kiểm toán trả về đúng danh sách `Virtual Key` / `Project` đã đi qua gói này.
10. **Đối chiếu Bất biến Nhân Core:** Kiểm tra lại `sha256sum(tanpopod) == CORE_BIN_SHA256_BEFORE` và `PID` của tiến trình Core không đổi — **chứng minh hoàn chỉnh tính độc lập tuyệt đối giữa hệ sinh thái Extension và chu kỳ phát hành Tanpopo Core**.