# `TP-IMP-0034` — Bộ Điều phối `Authorized OAuth 2.1` (`PKCE S256`, `Client Credentials`, `Device Flow`, Bảng Phiên Chờ, Làm mới Đơn luồng & Khóa `RFC 8707`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0034` |
| **Milestone / Epic** | `M3` (`v0.2 MCP Governance & Authorized OAuth 2.1`) · `EP-07` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0010`, `TP-IMP-0024` — `GAP-01, 04`)* |
| **Quy mô (`Size`)** | `L` (`internal/security/oauth/*`, `internal/admin/http/oauth.go`, `db/migrations/sqlite/0011_oauth_sessions.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Luồng ủy quyền OAuth 2.1, chống đánh cắp/chuyển tiếp chéo token `RFC 8707`, chống giẫm đạp làm mới `FMEA-08` & tuân thủ `DEC-004`)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Identity & Security Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa bộ điều phối **`Authorized OAuth 2.1`** ở phía Host (`TP-FR-0205`, `SM-03`, `SPEC-EXT-03`) chỉ dành cho các luồng OAuth thương mại/doanh nghiệp được nhà cung cấp hoặc máy chủ MCP đích chính thức cấp phép (`DEC-004` — **tuyệt đối không giả mạo `client_id` nội bộ hay trích xuất cookie trình duyệt của các gói thuê bao cá nhân**):
1. Hỗ trợ 3 luồng chuẩn: `Authorization Code + PKCE (S256 bắt buộc)`, `Client Credentials`, và `Device Authorization Grant (RFC 8628)` với bảng lưu phiên chờ ngắn hạn `oauth_authorization_sessions` (`TTL <= 10 phút`, chống tấn công `CSRF / Session Fixation` qua tham số `state` mật mã học gắn chặt với `principal_id` + `org_id`).
2. **Làm mới Token Đơn luồng (`Single-Flight Token Refresh` — `FMEA-08`):** Khi `access_token` sắp hết hạn dưới tải 500 luồng đồng thời, đúng **1 yêu cầu duy nhất** gọi điểm cuối `/token` của hãng ủy quyền (xử lý an toàn cả cơ chế xoay vòng `refresh_token` dùng một lần), 499 luồng còn lại chờ kết quả trên cùng `singleflight.Group` mà không làm vô hiệu hóa dây chuyền `refresh_token`.
3. **Khóa Cứng Đối tượng Nhận (`RFC 8707 Resource Indicators` — `AUTH-16` / `CT-19`):** Mọi token cấp ra đều gắn chặt với `bound_resource_uri` / `audience` — `BlindBroker` từ chối tuyệt đối mọi nỗ lực chuyển tiếp token của Dịch vụ A sang Dịch vụ B (`No Token Passthrough / No Confused Deputy`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/security/oauth/pkce.go`, `client_credentials.go`, `device_flow.go` (toàn bộ cuộc gọi HTTP tới Authorization Server đều đi qua `SSRFGuardDialer`; kèm `internal/testkit/fakeoauth/` phục vụ kiểm thử khép kín).
  - `internal/security/oauth/refresh_coordinator.go` (`singleflight` theo `credential_id`, gia hạn chủ động trước khi hết hạn `skew = 60s`, cập nhật nguyên tử cặp `access_token` + `refresh_token` mới vào `vault` có `AEAD AAD`; khi gặp `400 invalid_grant` từ IdP lập tức dừng thử lại `0` retries, chuyển `SM-02/SM-03` sang `AUTH_EXPIRED_REAUTH_REQUIRED` và phát cảnh báo `ALT-OAUTH-REAUTH` theo `AUTH-10` / `INC-05`).
  - `internal/admin/http/oauth.go` (`/admin/v1/oauth/authorize`, `/admin/v1/oauth/callback`, `/admin/v1/oauth/device/start`, `/admin/v1/oauth/device/poll`).
  - `db/migrations/sqlite/0011_oauth_sessions.sql` (`oauth_authorization_sessions`, `oauth_token_leases`).
- **Out-of-Scope:** Đăng nhập người dùng quản trị vào Console qua `Enterprise OIDC SSO` (thuộc `TP-IMP-0041`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0205`, `TP-BR-0301`..`0304`, `DEC-004`, `SM-03`, `CT-19`, `AUTH-09`, `AUTH-10`, `AUTH-16`, `FMEA-08`, `INC-05`.
- **Dependencies:** `TP-IMP-0010`, `TP-IMP-0024`.
- **Design Refs:** [`authentication-connections.md`](../../specifications/authentication-connections.md) (`SPEC-EXT-03`), [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) Mục 3.3, [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-08`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-19`, `AUTH-09`, `AUTH-10`, `AUTH-16`).

## 4. API, Wire & Data Contracts
- **Bất biến Chống Đánh tráo `state` & Hạ cấp `PKCE`:** Từ chối mọi máy chủ cấu hình `code_challenge_method = "plain"` hoặc `http://` không mã hóa (trừ `127.0.0.1` trong chế độ kiểm thử khép kín); `callback` chỉ khớp thành công nếu cùng phiên trình duyệt của chính `principal_id` đã khởi tạo `state` còn hạn.
- **Không Bao Giờ Chuyển tiếp Token Của Khách Lên Thượng nguồn (`No Upstream Token Relay`):** Token mà khách gửi vào Tanpopo chỉ xác thực với Tanpopo; token gửi lên Provider/MCP luôn do `BlindBroker` lấy từ kho khóa được gắn đúng `resource` đích.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`FMEA-08` & `CT-19` Single-Flight Refresh Under 200 Concurrent Calls):** Given `FakeOAuthIdP` bật chế độ `One-Time Rotating Refresh Token` (gọi lần 2 bằng `refresh_token` cũ sẽ hủy toàn bộ phiên!), When token hết hạn đúng lúc 200 goroutines đồng thời gửi yêu cầu qua `BlindBroker`, Then `FakeOAuthIdP` ghi nhận **đúng 1 lệnh gọi `/token`**, cả 200 yêu cầu đều nhận token mới thành công `100%`.
- **`NEG-1` (`AUTH-09` OAuth Callback CSRF / Replay):** Gửi `GET /admin/v1/oauth/callback` với `state` giả mạo, hết hạn `> 10m`, dùng lại lần thứ 2, hoặc từ phiên của người dùng khác → bị từ chối `400 invalid_oauth_state`.
- **`NEG-2` (`AUTH-16` & `CT-19` Cross-Audience Token Theft Attempt):** Cố cấu hình một kết nối tới `https://mcp-other.example.com` sử dụng token được cấp với `resource = https://mcp-billing.example.com` → `BlindBroker` chặn cứng `403 credential_audience_mismatch`.
- **`NEG-3` (`AUTH-10` & `INC-05` Upstream `invalid_grant` Revocation):** Khi `FakeOAuthIdP` trả `400 invalid_grant` lúc làm mới token → bộ điều phối dừng thử lại ngay lập tức (`0` retries chống bão lỗi), chuyển trạng thái sang `AUTH_EXPIRED_REAUTH_REQUIRED` và tăng cảnh báo `ALT-OAUTH-REAUTH`.
- **`NEG-4` (`DEC-004` Compliance Guard):** Cố nhập cấu hình kiểu trích xuất `Cookie` phiên trình duyệt vào luồng OAuth → bị từ chối ngay ở lớp kiểm định schema (`CR-05`).

## 6. Test Commands & Required Evidence
- Thuần Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/security/oauth/... ./internal/admin/http/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/security/oauth/... ./internal/admin/http/...`)*
- Tranh chấp dữ liệu (`-race`): `go test -v -race ./internal/security/oauth/... ./internal/admin/http/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Cả `access_token`, `refresh_token` và `code_verifier` đều được niêm phong bằng `AEAD AAD` và xóa sạch mảng byte tạm ngay sau khi sử dụng; `0` byte token xuất hiện trong URL redirect phía giao diện sau bước callback.
- **Rollback:** Bảng cộng thêm `0011_oauth_sessions.sql`; có thể tắt các kết nối `OAUTH2` về `DISABLED` mà không ảnh hưởng kết nối `API_KEY`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` (DDL bảng `oauth_authorization_sessions`), `TP-IMP-0010` và `TP-IMP-0024` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca `CT-19, 20`, `AUTH-09, 10` và bài kiểm thử giẫm đạp 200 luồng `FMEA-08` dưới `-race`, vượt qua `REQUIRES_STRONG_REVIEW`.
