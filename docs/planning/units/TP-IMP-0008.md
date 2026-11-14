# `TP-IMP-0008` — Kho Khóa Mã hóa Phong bì Ba Tầng (`Master KEK -> Org DEK -> Project DEK`), `AEAD AAD` & `SecretCredential` Một chiều

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0008` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0007` — `GAP-01, 05`)* |
| **Quy mô (`Size`)** | `M` (`internal/security/vault/*`, `db/migrations/sqlite/0002_vault.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Mật mã học bảo vệ tài sản trọng yếu `AST-01`, `AST-02`)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Security / Cryptography Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Xây dựng kho bí mật ghi một chiều (`Write-Only Secret Vault`) sử dụng mã hóa phong bì xác thực `AES-256-GCM` ba tầng (`Master KEK` → `Org DEK` → `Project DEK`) có gắn cứng ngữ cảnh `AEAD AAD` theo [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) và `CR-06`, đảm bảo: (1) chống tuyệt đối tấn công cấy ghép bản mã chéo tổ chức, (2) không có bất kỳ hàm đối ngoại nào đọc ngược khóa rõ cho API/UI, (3) xoay `Master KEK` trực tuyến chỉ bằng cách bọc lại `Org DEK`, và (4) hỗ trợ hủy khóa mật mã học (`Crypto-Shredding`) ở cả cấp `Organization` lẫn cấp `Project`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/security/vault/kek.go` (giao diện `KEKProvider` + trình nạp khóa tĩnh `256-bit` từ biến `TANPOPO_MASTER_KEY` hoặc tệp khóa `0400`/`0600` cục bộ).
  - `internal/security/vault/envelope.go` (quản lý `org_encryption_keys`, `project_encryption_keys`, bộ đệm RAM ngắn hạn `TTL <= 5m` có xóa vùng đệm `clear(buf)` khi thu hồi/hủy).
  - `internal/security/vault/store.go` (`SealSecret`, `RotateSecret`, `RewrapAllOrgDEKs`, `ShredProjectDEK`, `ShredOrgDEK` và hàm nội bộ chỉ dành riêng cho gói `egress`: `UnsealForEgressBroker`).
  - `db/migrations/sqlite/0002_vault.sql` (`org_encryption_keys`, `project_encryption_keys`, `secret_credentials`).
- **Out-of-Scope:** Tích hợp AWS/GCP KMS & HashiCorp Vault từ xa (thuộc `TP-IMP-0041` ở `v0.3`); luồng OAuth 2.1 (thuộc `TP-IMP-0034`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0204`, `TP-FR-1003`, `TP-NFR-0402`, `TP-NFR-0502`, `TP-BR-0102`, `AST-01`, `AST-02`, `AUTH-14`, `OPS-AC-07`, `OPS-AC-09`.
- **Dependencies:** `TP-IMP-0007`.
- **Design Refs:** [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) Mục 1 & 4, [`data-model.md`](../../architecture/data-model.md) Mục 3.1 & 3.2, [`backups-recovery.md`](../../operations/backups-recovery.md) (`DR-03`).

## 4. API, Wire & Data Contracts
- **Công thức `AEAD AAD` Chuẩn Hóa Duy Nhất (`CR-06`):**
  - Cho `secret_credentials`: `AAD = []byte("tanpopo:v1:cred:" + orgID + ":" + credentialID + ":" + secretKind)` (sinh `nonce` 96-bit từ `crypto/rand` mới hoàn toàn cho mỗi lần ghi/xoay khóa!).
  - Cho bí mật thuộc riêng `Project` (và `debug_payload_blobs` sau này): bọc dưới `Project DEK` với `AAD` chứa cả `orgID` và `projectID`.
- **Kiểu trả về cho Control Plane (`SecretMetadata`):** Chỉ chứa `{ ID, OrgID, OwnerProjectID, Name, SecretKind, BoundAudience, KeyFingerprint, Version, UpdatedAt }` — **không có trường `Plaintext` hay `Ciphertext`**.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given khóa mồi `sk-tp-canary-DO-NOT-LEAK-9f8e7d6c`, When gọi `SealSecret`, Then dữ liệu trên đĩa SQLite chỉ chứa bản mã ngẫu nhiên (quét tệp `.db` thô tìm chuỗi canary ra `0` kết quả) và `GetSecretMetadata` chỉ trả dấu vân tay ẩn danh.
- **`AC-2`:** Given `KEK_v1` đang bọc 10 `Org DEKs`, When gọi `RewrapAllOrgDEKs(KEK_v1 -> KEK_v2)`, Then cả 10 tổ chức tiếp tục giải mã `SecretCredential` bình thường dưới `KEK_v2` mà không cần mã hóa lại bảng `secret_credentials`.
- **`AC-3`:** Given `Project P1` và `Project P2` cùng thuộc `Org A`, When gọi `ShredProjectDEK(Org_A, P1)`, Then mọi bí mật riêng của `P1` không thể giải mã được nữa (kể cả từ bản sao lưu tệp SQLite chụp trước khi xóa!), trong khi `P2` vẫn hoạt động `100%` bình thường.
- **`NEG-1` (`AUTH-14` Ciphertext Transplant):** Dùng câu lệnh SQL thô chép `nonce` + `ciphertext` từ dòng của `Org A` sang dòng của `Org B` → `UnsealForEgressBroker` thất bại tức khắc với lỗi xác thực thẻ `GCM` (`cipher: message authentication failed`).
- **`NEG-2`:** Khởi chạy ở chế độ sản xuất với `TANPOPO_MASTER_KEY` rỗng, ngắn hơn `32 bytes` hoặc toàn số `0` → từ chối khởi tạo kho khóa ngay lập tức (`Fail-Closed`).
- **`NEG-3`:** Không bao giờ tái sử dụng cùng một `nonce` cho cùng một `DEK` hoặc in lỗi giải mã chứa mảng byte khóa ra nhật ký.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/security/vault/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Gói `vault` giới hạn quyền gọi hàm mở niêm phong chỉ cho `Blind Credential Broker` thông qua cấu trúc điều khiển nội bộ; xóa mảng byte tạm `clear(plaintext)` bằng `defer` ngay sau khi sử dụng.
- **Rollback:** Lược đồ bảng mới hoàn toàn cộng thêm (`0002_vault.sql`), tương thích sao lưu nóng SQLite.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` (khóa công thức `AAD` & `Project DEK`) và `TP-IMP-0007` đã hoàn tất.
- **DoD:** Đạt độ phủ kiểm thử `>= 90%` trên `internal/security/vault/...`, vượt qua kiểm tra `CanaryScanner` và `REQUIRES_STRONG_REVIEW`.
