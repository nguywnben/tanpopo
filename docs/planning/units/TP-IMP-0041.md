# `TP-IMP-0041` — Liên kết `Enterprise OIDC SSO`, Quy tắc Bốn Mắt (`Four-Eyes`), Kho Khóa Ngoài (`Cloud KMS / Vault`), `Workload Identity` & `Response Cache` Tùy chọn

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0041` |
| **Milestone / Epic** | `M4` (`v0.3 Clustered HA, Enterprise Security & A2A`) · `EP-08` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0034`, `TP-IMP-0039` — `GAP-01, 09`)* |
| **Quy mô (`Size`)** | `L` (`go.mod`, `internal/iam/oidc/*`, `internal/security/foureyes/*`, `internal/security/vault/kms/*`, `internal/ingress/cache/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Xác thực `OIDC JWT/JWKS`, quy tắc phê duyệt hai người `Four-Eyes`, tích hợp KMS ngoài & cô lập tuyệt đối bộ nhớ đệm phản hồi `FMEA-03`)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Enterprise Security Architect` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hoàn thiện bộ tính năng bảo mật cấp doanh nghiệp (vẫn giữ **100% mã nguồn mở `Apache-2.0`** theo lợi thế cạnh tranh của Tanpopo, không khóa sau tường phí đóng — `SD-G1-05`):
1. **Đăng nhập Một lần `Enterprise OIDC SSO` & Ánh xạ Nhóm sang 6 Vai trò `RBAC` (`TP-FR-0504`, `AUTH-01`):** Xác thực chữ ký `ID Token` qua `JWKS` có xoay khóa, kiểm tra chặt `iss`, `aud`, `exp`, `nonce`, ánh xạ tuyên bố nhóm (`groups` claim) vào đúng `(org_id, role)` mà không cho phép leo thang chéo tổ chức.
2. **Quy tắc Phê duyệt Bốn Mắt (`Four-Eyes Dual Control` — `AUTH-12`):** Khi tổ chức bật chính sách kiểm soát kép, các hành động nhạy cảm bậc cao (bật Plugin `unverified-external` ở môi trường `prod`, xoay/hủy khóa gốc, gỡ bỏ Kill-Switch sự cố) đòi hỏi 2 quản trị viên độc lập (`initiator_id != approver_id`).
3. **Nền tảng Khóa Ngoài (`AWS KMS`, `GCP KMS`, `HashiCorp Vault Transit`) & Xác thực Không Khóa Tĩnh (`Cloud Workload Identity / IRSA` — `TP-FR-0206`).**
4. **Bộ nhớ đệm Phản hồi Chính xác Tùy chọn Mặc định Tắt (`Opt-In Exact-Match Response Cache` — `CR-09`, `CT-28`, `FMEA-03`):** Mặc định **tắt (`disabled`)**; khi dự án bật tường minh cho yêu cầu tất định (`temperature == 0`, không truyền luồng hoặc tái phát có kiểm soát), khóa băm bộ đệm bắt buộc bao hàm **`SHA-256(JCS({ org_id, project_id, env_id, policy_revision, model_deployment_id, tools_schema_hash, canonical_request }))`** và được mã hóa bằng `Project DEK` để **cấm tuyệt đối** rò rỉ câu trả lời giữa hai tổ chức/dự án!

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (thêm thư viện OIDC/JWKS & SDK KMS tối giản).
  - `internal/iam/oidc/provider.go` (luồng `OIDC Authorization Code + PKCE + Nonce`, bộ đệm `JWKS` qua `SSRFGuardDialer`).
  - `internal/security/foureyes/service.go` (hàng đợi phê duyệt hành động quản trị hai người `admin_change_requests`).
  - `internal/security/vault/kms/*.go` (triển khai `KEKProvider` cho KMS/Vault ngoài để bọc `Org DEKs`).
  - `internal/security/broker/workload_identity.go` (đổi mã thông báo OIDC của môi trường sang chứng chỉ ngắn hạn cho `Bedrock` / `Vertex AI` có kiểm soát chặt danh sách cho phép ở mức cấu hình Host).
  - `internal/ingress/cache/exact_cache.go` (bộ nhớ đệm phản hồi cô lập cứng theo `(org_id, project_id)` + mã hóa `Project DEK`).
- **Out-of-Scope:** Bộ nhớ đệm tương đồng ngữ nghĩa véc-tơ mờ (`Semantic Similarity Cache`) — chủ đích loại khỏi `v1.0` do rủi ro trả nhầm dữ liệu nhạy cảm giữa các ngữ cảnh người dùng.

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0206`, `TP-FR-0504`, `AUTH-01`, `AUTH-02`, `AUTH-12`, `CT-28`, `FMEA-03`, `CR-09`.
- **Dependencies:** `TP-IMP-0034`, `TP-IMP-0039`.
- **Design Refs:** [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) Mục 2.3 & 4, [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-03`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`AUTH-01`, `AUTH-12`, `CT-28`).

## 4. API, Wire & Data Contracts
- **Bất biến Chống Đầu độc & Rò rỉ Bộ nhớ đệm (`FMEA-03` & `CT-28`):**
  - Không bao giờ lưu vào bộ nhớ đệm các phản hồi lỗi, phản hồi bị cắt cụt, phản hồi có chứa lệnh gọi công cụ có tác động phụ, hoặc yêu cầu mang header `Cache-Control: no-store`.
  - Hai dự án `P1` và `P2` gửi hai câu hỏi giống hệt nhau từng ký tự vẫn sinh ra 2 khóa bộ đệm hoàn toàn khác nhau và bọc bằng 2 `Project DEK` khác nhau — `0%` khả năng đọc chéo!

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`AUTH-01` OIDC Login & Group-to-Role Sync):** Đăng nhập qua `FakeOAuthIdP` (chế độ OIDC), kiểm tra xác minh chữ ký `RS256/ES256`, ánh xạ nhóm `tanpopo-sec-reviewers` vào đúng vai trò chuẩn `SECURITY_REVIEWER` và tự thu hồi quyền khi nhóm bị gỡ ở lần làm mới phiên.
- **`AC-2` (`AUTH-12` Four-Eyes Enforcement):** Khi bật `Four-Eyes`, quản trị viên `Admin 1` bấm bật một Plugin bên ngoài → yêu cầu dừng ở `PENDING_SECOND_APPROVER`; nếu chính `Admin 1` cố tự duyệt yêu cầu đó → bị từ chối `403 self_approval_forbidden`; chỉ khi `Admin 2` duyệt thì Plugin mới kích hoạt.
- **`AC-3` (`CT-28` & `FMEA-03` Zero Cross-Tenant / Cross-Project Cache Hit):** `Org A / Project 1` bật cache và gửi câu hỏi `Q`; khi `Org B` hoặc `Org A / Project 2` gửi câu hỏi `Q` giống hệt 100%, bộ đệm báo **`CACHE MISS`** tuyệt đối; khi hủy `Project DEK` của `Project 1`, mọi mục cache của `Project 1` lập tức trở thành rác không thể giải mã.
- **`NEG-1` (`OIDC Token Forgery`):** Gửi `ID Token` có `alg: "none"`, sai `aud`, sai `nonce`, hoặc ký bằng khóa lạ không nằm trong `JWKS` → bị từ chối cứng `401 invalid_oidc_token`.

## 6. Test Commands & Required Evidence
- Thuần Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/iam/oidc/... ./internal/security/foureyes/... ./internal/security/vault/... ./internal/ingress/cache/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/iam/oidc/... ./internal/security/foureyes/... ./internal/security/vault/... ./internal/ingress/cache/...`)*
- Tranh chấp dữ liệu (`-race`): `go test -v -race ./internal/iam/oidc/... ./internal/security/foureyes/... ./internal/security/vault/... ./internal/ingress/cache/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `Response Cache` mặc định `enabled = false` ở mọi cấp; chế độ `Bootstrap Token` ban đầu vẫn luôn bị khóa vĩnh viễn sau khi đã thiết lập tổ chức (`AUTH-02`).
- **Rollback:** Tắt cờ `cache.enabled` hoặc `foureyes.enabled` về hành vi tiêu chuẩn không làm mất dữ liệu.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (bổ sung mã yêu cầu cho `Response Cache` theo `CR-09`), `TP-IMP-0034` và `TP-IMP-0039` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca `AUTH-01, 02, 12`, `CT-28` và `FMEA-03` dưới `-race`, vượt qua `REQUIRES_STRONG_REVIEW`.
