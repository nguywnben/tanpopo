# `TP-IMP-0010` — Bộ Quay số Chống `SSRF / DNS Rebinding` Lúc `connect()` & Trình Tiêm Khóa Mù `Blind Host Credential Broker`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0010` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` / `EP-02` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0004`, `TP-IMP-0008` — `GAP-01, 02`)* |
| **Quy mô (`Size`)** | `M` (`internal/security/egress/*`, `internal/security/broker/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Ranh giới mạng đi ra ngoài `TB-3/TB-4` & bảo vệ khóa gốc khỏi Plugin)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Security / Networking Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Xây dựng hai chốt chặn bảo mật cốt lõi của mặt phẳng kết nối ngoài theo [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md) và [`threat-model.md`](../../security/threat-model.md) (`TM-05`, `TM-06`):
1. **`SSRFGuardDialer`:** Kiểm tra tên miền trong danh sách `allowed_hosts` đã được quản trị viên duyệt **và** kiểm tra trực tiếp địa chỉ IP phân giải ngay tại khoảnh khắc mở socket hạt nhân (`net.Dialer.Control`) để chặn đứng `DNS Rebinding` (`TTL=0`), IP nội bộ `RFC1918`, `loopback`, `link-local` và dịch vụ siêu dữ liệu đám mây `169.254.169.254` / `fd00:ec2::254` (trừ khi quản trị viên bật cờ tường minh `allow_private_endpoints = true` gắn dải CIDR cụ thể cho GPU nội bộ).
2. **`BlindHostCredentialBroker`:** Nhận yêu cầu HTTP trung gian chỉ chứa thẻ tham chiếu mờ `secret_ref` từ Adapter/Wasm, kiểm tra khớp nối `bound_audience` của khóa với `target_host` đích thực tế, rồi tự giải mã từ `vault` và gắn header/chữ ký ngay trên bộ đệm socket của Host — đảm bảo mã Plugin **không bao giờ nhìn thấy khóa rõ**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/security/egress/ssrf_dialer.go` (bộ lọc CIDR cấm `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16`, `100.64.0.0/10`, `::1/128`, `fc00::/7`, `fe80::/10`, IPv4-mapped IPv6 `::ffff:169.254.169.254` ngay trong `Control(network, address string, c syscall.RawConn)`).
  - `internal/security/egress/redirect_guard.go` (`CheckRedirect` đánh giá lại `allowed_hosts` + `SSRFGuardDialer` trên từng bước chuyển hướng `301/302/307/308` và mặc định lột bỏ toàn bộ header `Authorization` / `X-API-Key` khi đổi tên miền).
  - `internal/security/broker/blind_broker.go` (tiêm `Bearer`, `Header`, `Basic`, hoặc ký `HMAC/AWS SigV4` ở phía Host dựa trên `CredentialBinding`; lột bỏ `Set-Cookie`, `WWW-Authenticate` nhạy cảm ở chiều trả về trước khi giao cho Plugin).
- **Out-of-Scope:** Làm mới token `OAuth 2.1` tự động (thuộc `TP-IMP-0034`); chữ ký `Cloud IAM / Workload Identity` (thuộc `TP-IMP-0041`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0202`, `TP-FR-0303`, `TP-FR-0702`, `TP-FR-0703`, `TP-BR-0304`, `TP-BR-0402`, `CT-14`, `CT-15`, `CT-16`, `CT-26`, `AUTH-16`.
- **Dependencies:** `TP-IMP-0004`, `TP-IMP-0008`.
- **Design Refs:** [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`authentication-connections.md`](../../specifications/authentication-connections.md) Mục 1 & 2, [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Mục 3, [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-14`..`16`, `CT-26`, `AUTH-16`).

## 4. API, Wire & Data Contracts
- **Giao diện `EgressRequest` từ Tầng Mở rộng:**
  - Chứa `Method`, `URL`, `Headers` (không chứa khóa rõ), `CredentialSlot` (tên khe logic khai báo trong `manifest`), `TimeoutMs`.
  - Host đối chiếu `(PluginInstallID, ConnectionID, CredentialSlot)` → lấy `SecretCredential` → kiểm tra `HostMatches(URL.Hostname(), AllowedHosts) && AudienceMatches(URL, Secret.BoundAudience)`. Nếu sai lệch bất kỳ vế nào → chặn cứng trước khi mở kết nối TCP, trả lỗi chuẩn `403 egress_host_denied` hoặc `403 credential_audience_mismatch` kèm sự kiện kiểm toán bảo mật.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `Connection` khai báo `allowed_hosts = ["127.0.0.1:19080"]` kèm `allow_private_endpoints = true` và `allowed_cidrs = ["127.0.0.1/32"]` (cho `FakeLLMServer` / `Ollama` được duyệt), When gửi `EgressRequest` hợp lệ có `CredentialSlot = "api_key"`, Then `FakeLLMServer` nhận đúng header `Authorization: Bearer sk-tp-canary-...` trong khi đối tượng trả về cho Plugin không chứa bất kỳ byte nào của khóa.
- **`NEG-1` (`CT-14` Unapproved Domain Exfiltration):** Plugin được duyệt `api.example.com` cố gửi HTTP tới `evil.attacker.invalid` → bị chặn trước cả bước phân giải DNS với lỗi `403 egress_host_denied`.
- **`NEG-2` (`CT-15` DNS Rebinding `TTL=0` & IPv4-Mapped IPv6):** Máy chủ DNS thử nghiệm lần 1 trả IP công khai giả lập, lần 2 tại lúc `Dial` đổi sang `169.254.169.254` hoặc `[::ffff:127.0.0.1]` → `Control` hook chặn đứng trước khi gửi gói `SYN` (`ssrf_blocked_ip`).
- **`NEG-3` (`CT-16` & `AUTH-16` Cross-Host Redirect / Audience Mismatch):** Máy chủ đích được phép trả `302 Location: https://other-host.test/steal` hoặc Plugin cố gắn `Credential` của `api.openai.com` vào kết nối gọi `custom-proxy.test` → bị chặn ngay lập tức, `0` byte `Authorization` bị rò rỉ sang máy chủ thứ hai.
- **`NEG-4` (`CT-26` Local GPU Port Scan):** Kết nối GPU nội bộ chỉ được phép `10.20.0.5/32:8000` cố quét sang `10.20.0.5:6379` hoặc `10.20.0.6:8000` → bị từ chối tức khắc.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/security/egress/... ./internal/security/broker/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Mọi `http.Client` đi ra mạng bên ngoài trong toàn bộ tiến trình Tanpopo (Adapter, OAuth, MCP, Webhook, Registry) **bắt buộc** khởi tạo từ `egress.NewGuardedHTTPClient(...)`; nghiêm cấm sử dụng `http.DefaultClient` hoặc `http.DefaultTransport` (kiểm tra tự động bằng linter trong CI).
- **Rollback:** Gói thư viện nền tảng độc lập, có thể lùi commit an toàn.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (khóa hợp đồng RPC `BlindBroker`), `TP-IMP-0004` và `TP-IMP-0008` đã hoàn tất.
- **DoD:** Vượt qua 100% các bài kiểm thử tấn công `CT-14, 15, 16, 26` và `AUTH-16`, `CanaryScanner` xác nhận `0` rò rỉ khóa sang phía Plugin, hoàn tất `REQUIRES_STRONG_REVIEW`.
