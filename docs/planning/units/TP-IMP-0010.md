# `TP-IMP-0010` — Bộ Quay số Chống `SSRF / DNS Rebinding` Lúc `connect()`, `Blind Host Credential Broker` & Hợp đồng `extension/port`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0010` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0004`, `TP-IMP-0008` — `GAP-01, 02`)* |
| **Quy mô (`Size`)** | `M` (`internal/security/egress/*`, `internal/security/broker/*`, `internal/extension/port/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Ranh giới mạng đi ra ngoài `TB-3/TB-4` & bảo vệ khóa gốc khỏi Plugin)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Security / Networking Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Xây dựng hai chốt chặn bảo mật cốt lõi của mặt phẳng kết nối ngoài theo [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md) và [`threat-model.md`](../../security/threat-model.md) (`TM-05`, `TM-06`), đồng thời định nghĩa giao diện thực thi chung `extension/port.ProviderRuntime` để Tầng 1 (`0012`) và Tầng 2 (`0013`) phát triển song song không lệch chữ ký hàm:
1. **`SSRFGuardDialer`:** Kiểm tra tên miền trong `allowed_hosts` **và** kiểm tra trực tiếp địa chỉ IP phân giải ngay tại khoảnh khắc mở socket hạt nhân (`net.Dialer.Control`) để chặn đứng `DNS Rebinding` (`TTL=0`), IP nội bộ `RFC1918`, `loopback`, `link-local` và dịch vụ siêu dữ liệu đám mây `169.254.169.254` / `fd00:ec2::254` (trừ khi kết nối được `OPERATOR`/`ORG_OWNER` bật cờ tường minh `allow_private_ip = true` kèm dải `allowed_cidrs` cụ thể cho GPU nội bộ, và tích hợp `netguard.CheckLoopbackAddr` khi chạy kiểm thử khép kín).
2. **`BlindHostCredentialBroker`:** Nhận yêu cầu HTTP trung gian chỉ chứa tên khe tham chiếu mờ `secret_slot` từ Adapter/Wasm qua giao diện trừu tượng `CredentialBindingResolver` (không phụ thuộc cứng vào bảng của `0011`/`0012`), đối chiếu `bound_audience` với `target_host`, dùng `vault.UnsealCapability` (`TP-IMP-0008`) giải mã và gắn tiêu đề/chữ ký ngay trên bộ đệm socket của Host, **cấm tuyệt đối tiêm tiêu đề `Cookie`/`Set-Cookie` ở chiều đi (`CR-05`)**, và **tự động quét che sạch chuỗi khóa bị dội ngược trong thân lỗi `401/403/500` của nhà cung cấp thượng nguồn (`CT-16`)**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/extension/port/runtime.go` (định nghĩa giao diện chung `ProviderRuntime` gồm `TransformRequest`, `TransformResponse`, `TransformStreamChunk`, `NormalizeError`, `ExtractUsage`, `InspectNativeRequest`, `BuildValidationProbe`, `VerifyValidationResult` dùng chung cho cả `TP-IMP-0012`, `0013` và `0042`).
  - `internal/security/egress/ssrf_dialer.go` (bộ lọc CIDR cấm `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16`, `100.64.0.0/10`, `::1/128`, `fc00::/7`, `fe80::/10`, IPv4-mapped IPv6 `::ffff:169.254.169.254` ngay trong `Control`).
  - `internal/security/egress/redirect_guard.go` (`CheckRedirect` đánh giá lại `allowed_hosts` + `SSRFGuardDialer` trên từng bước `301/302/307/308` và lột bỏ toàn bộ tiêu đề xác thực khi đổi tên miền — `CT-15`).
  - `internal/security/broker/blind_broker.go` (giao diện `CredentialBindingResolver` + `port.AuditSink`; tiêm `Bearer`, `Header`, `Basic`, `HMAC_SHA256` ở phía Host, từ chối mọi chỉ thị tiêm `Cookie`, và che sạch chuỗi khóa trong phản hồi lỗi thượng nguồn trước khi trả về cho Plugin/Client — `CT-16`).
- **Out-of-Scope:** Ký `AWS_SIGV4` hoàn chỉnh cho gói Bedrock & làm mới `OAuth 2.1` (thuộc `TP-IMP-0034` và `TP-IMP-0035` ở `M3`); `Cloud Workload Identity` (thuộc `TP-IMP-0041`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0202`, `TP-FR-0303`, `TP-FR-0702`, `TP-FR-0703`, `TP-BR-0304`, `TP-BR-0402`, `CT-14`, `CT-15`, `CT-16`, `CT-26`, `AUTH-16`.
- **Dependencies:** `TP-IMP-0004`, `TP-IMP-0008`.
- **Design Refs:** [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`authentication-connections.md`](../../specifications/authentication-connections.md) Mục 1 & 2, [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Mục 3, [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-14`..`16`, `CT-26`, `AUTH-16`).

## 4. API, Wire & Data Contracts
- **Giao diện `EgressRequest` & Ràng buộc Đối tượng Nhận (`AUTH-16`):**
  - Host gọi `resolver.ResolveSlot(ctx, orgID, connectionID, slotName)` → kiểm tra `HostMatches(URL.Hostname(), AllowedHosts) && AudienceMatches(URL, Secret.BoundAudience)`. Sai lệch bất kỳ vế nào → chặn trước khi mở TCP, ghi sự kiện vào `AuditSink`, trả `403 egress_host_denied` hoặc `403 credential_audience_mismatch`.
  - Nếu `field_name` cấu hình tiêm là `Cookie` hoặc `Set-Cookie` (không phân biệt hoa thường) → từ chối cứng `422 prohibited_cookie_injection` (`DEC-004` / `CR-05`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `Connection` khai báo `allowed_hosts = ["127.0.0.1:19080"]` kèm `allow_private_ip = true` và `allowed_cidrs = ["127.0.0.1/32"]` (cho `FakeLLMServer`), When gửi `EgressRequest` hợp lệ có `secret_slot = "api_key"`, Then `FakeLLMServer` nhận đúng `Authorization: Bearer sk-tp-canary-...` trong khi đối tượng trả về cho Plugin không chứa bất kỳ byte nào của khóa.
- **`NEG-1` (`CT-14` Unapproved Domain Exfiltration):** Plugin được duyệt `api.example.com` cố gửi HTTP tới `evil.attacker.invalid` → bị chặn trước cả bước phân giải DNS với `403 egress_host_denied`.
- **`NEG-2` (`CT-15` Cross-Host Redirect Strip & SSRF):** Máy chủ đích được phép trả `302 Location: https://other-host.test/steal` hoặc `http://169.254.169.254/` → bị chặn/lột sạch `100%` tiêu đề `Authorization`, `0` byte khóa rò rỉ.
- **`NEG-3` (`CT-16` Upstream `401` Echoed-Secret Redaction & Outbound Cookie Ban):** Bật cờ `X-Tanpopo-Fake-Fault: echo_auth_header_in_error` trên `FakeLLMServer` để thân lỗi `401` nhại lại nguyên văn khóa `sk-tp-canary-...` → `BlindBroker` tự động xóa/thay bằng `[REDACTED_CREDENTIAL]` trước khi giao cho Plugin/Client; thử tiêm `Cookie` bị từ chối `422`.
- **`NEG-4` (`CT-26` & `AUTH-16` DNS Rebinding `TTL=0`, Port Scan & Audience Mismatch):** Máy chủ DNS lần 1 trả IP công khai, lần 2 tại `Dial` đổi sang `169.254.169.254` hoặc `[::ffff:127.0.0.1]` → `Control` hook chặn đứng trước gói `SYN`; quét sang cổng LAN không duyệt `10.20.0.5:6379` hoặc gắn khóa của `api.openai.com` vào `custom-proxy.test` bị chặn tức khắc.

## 6. Test Commands & Required Evidence
- Kiểm tra thuần Go (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/extension/port/... ./internal/security/egress/... ./internal/security/broker/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/extension/port/... ./internal/security/egress/... ./internal/security/broker/...`
- Kiểm tra tranh chấp dữ liệu (`-race`): `go test -v -race ./internal/security/egress/... ./internal/security/broker/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Mọi `http.Client` đi ra mạng bên ngoài trong toàn bộ tiến trình Tanpopo **bắt buộc** khởi tạo từ `egress.NewGuardedHTTPClient(...)`; nghiêm cấm `http.DefaultClient` / `http.DefaultTransport` (kiểm tra tự động bởi `scripts/ci/check.go`).
- **Rollback:** Gói thư viện nền tảng độc lập, có thể lùi commit an toàn.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (khóa hợp đồng RPC `BlindBroker`), `TP-IMP-0004` và `TP-IMP-0008` đã hoàn tất.
- **DoD:** Vượt qua 100% các bài kiểm thử tấn công `CT-14, 15, 16, 26` và `AUTH-16`, `CanaryScanner` xác nhận `0` rò rỉ khóa, hoàn tất `REQUIRES_STRONG_REVIEW`.
