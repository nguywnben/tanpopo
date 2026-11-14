# `TP-IMP-0035` — Trình Đọc Chỉ mục Marketplace Tĩnh Đã Ký, Kéo Gói `Private OCI Registry`, Đồng bộ `revocations.json` & Gói `Bedrock` / `Azure` / `/v1/rerank`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0035` |
| **Milestone / Epic** | `M3` (`v0.2 MCP Governance & Authorized OAuth 2.1`) · `EP-02` / `EP-03` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0011`, `TP-IMP-0018`, `TP-IMP-0034` — `GAP-01, 04, 09`)* |
| **Quy mô (`Size`)** | `L` (`internal/extension/registry/*`, `internal/gateway/http/rerank.go`, `extensions/official/aws-bedrock/*`, `azure-openai/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Xác minh chữ ký chỉ mục từ xa, tự động cách ly gói dính CVE qua `revocations.json` & ký `AWS SigV4` trong `BlindBroker`)* |
| **Chủ sở hữu Kỹ thuật** | `Platform Ecosystem & Supply-Chain Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Mở rộng hệ sinh thái phân phối phần mở rộng và danh mục hãng đám mây doanh nghiệp cho nhịp `v0.2` (`TP-FR-0307`, `TP-NFR-0602`, `CR-01`):
1. **Chỉ mục Marketplace Tĩnh Đã Ký & Kéo Gói `OCI Registry` Riêng tư (`OCI Distribution Spec v1.1`):** Đồng bộ tệp `index.json` có chữ ký `Ed25519/Cosign`, cho phép doanh nghiệp thêm kho OCI nội bộ (`Harbor`, `GHCR`, `ECR`) và vẫn chạy bình thường 100% từ bộ đệm cục bộ khi mất kết nối tới Marketplace bên ngoài (`Graceful Offline Degradation`).
2. **Đồng bộ Danh sách Thu hồi Khẩn cấp (`revocations.json` Auto-Quarantine):** Kiểm tra định kỳ (hoặc nạp thủ công trong mạng kín) danh sách gói bị thu hồi do lỗ hổng bảo mật/độc hại; nếu phát hiện gói đang cài khớp mã băm/phiên bản bị thu hồi mức `CRITICAL`, tự động chuyển sang `QUARANTINED`, cắt luồng đang chạy và phát cảnh báo `INC-03`.
3. **Bổ sung Điểm cuối `POST /v1/rerank` + 2 Gói Chính thức Doanh nghiệp (`@tanpopo/aws-bedrock` dùng ký số `SigV4` trên `BlindBroker` & `@tanpopo/azure-openai`).**

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/extension/registry/index_sync.go` (xác minh chữ ký `index.json` + chống tấn công hạ cấp phiên bản `Freeze / Rollback Attack` bằng kiểm tra số `serial` và `expires_at` đơn điệu tăng!).
  - `internal/extension/registry/oci_client.go` (kéo artifact `.tppkg` theo digest `sha256:...` bất biến từ OCI Registry qua `SSRFGuardDialer`).
  - `internal/extension/registry/revocation_watcher.go` (xác minh chữ ký `revocations.json` và kích hoạt cách ly tự động).
  - `internal/gateway/http/rerank.go` (`POST /v1/rerank` chuẩn hóa tài liệu xếp hạng lại).
  - `extensions/official/aws-bedrock/*` & `extensions/official/azure-openai/*` kèm băng ghi kiểm thử ngoại tuyến.
- **Out-of-Scope:** Xác thực vai trò đám mây không khóa tĩnh `AWS IRSA / GCP Workload Identity` (thuộc `TP-IMP-0041` ở `v0.3` — ở `v0.2` `@tanpopo/aws-bedrock` dùng cặp khóa IAM có phạm vi hẹp lưu trong `vault` được `BlindBroker` ký `SigV4`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0307`, `TP-NFR-0602`, `SPEC-EXT-04`, `CR-01`.
- **Dependencies:** `TP-IMP-0011`, `TP-IMP-0018`, `TP-IMP-0034`.
- **Design Refs:** [`marketplace-lifecycle.md`](../../specifications/marketplace-lifecycle.md) (`SPEC-EXT-04`), [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Mục 4, [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-01`).

## 4. API, Wire & Data Contracts
- **Chống Tấn công Phát lại Chỉ mục Cũ (`Anti-Rollback Feed Protection`):** Tệp `index.json` và `revocations.json` bắt buộc có `version_serial` nguyên dương tăng dần; nếu máy chủ từ xa bị chiếm quyền trả về tệp có chữ ký cũ hợp lệ nhưng `version_serial < last_seen_serial`, Tanpopo từ chối cập nhật và ghi cảnh báo bảo mật.
- **Bất biến Suy giảm Êm (`TP-NFR-0602`):** Lỗi mạng khi đồng bộ `index.json` hoặc `revocations.json` **không bao giờ** làm gián đoạn lưu lượng suy luận của các gói đã cài đặt hợp lệ trước đó.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `FakeRegistryServer` cung cấp `index.json`, `revocations.json` đã ký và kho OCI nội bộ, When đồng bộ danh mục và kéo gói theo `@sha256:...`, Then gói được xác minh mã băm thành công; khi tắt sập `FakeRegistryServer`, toàn bộ yêu cầu suy luận và cài lại từ `tanpopo.lock` cục bộ vẫn chạy `100%` bình thường.
- **`AC-2` (`Auto-Quarantine on Signed Advisory`):** Khi `FakeRegistryServer` phát hành `revocations.json` ký hợp lệ đánh dấu gói `community/bad-adapter@1.0.0` mức `CRITICAL`, trình canh gác tự động đưa gói đó về `QUARANTINED` và ghi sự kiện kiểm toán.
- **`AC-3`:** Chạy `tanpopo plugin test` trên `@tanpopo/aws-bedrock` (bao gồm kiểm chứng chữ ký chuẩn `AWS4-HMAC-SHA256` trên `BlindBroker`), `@tanpopo/azure-openai` và điểm cuối `/v1/rerank` đạt `100%`.
- **`NEG-1` (`Unsigned / Downgraded Revocation Feed`):** Bơm tệp `revocations.json` giả mạo không đúng chữ ký (nhằm phá hoại tắt hàng loạt plugin hợp lệ — DoS) hoặc có `version_serial` lùi → bị từ chối hoàn toàn.
- **`NEG-2` (`Mutable OCI Tag Without Digest`):** Kéo từ OCI chỉ bằng thẻ `:latest` mà không khóa `sha256` digest khi chính sách ghim chặt bật → bị từ chối `422 unpinned_oci_digest`.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/extension/registry/... ./internal/gateway/... ./extensions/official/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Thông tin đăng nhập kéo kho `Private OCI Registry` cũng được bảo vệ trong `vault` có `AEAD AAD` và chỉ tiêm cho đúng tên miền kho OCI đó.
- **Rollback:** Có thể đặt `marketplace.sync_enabled = false` để chạy thuần ngoại tuyến.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0011`, `TP-IMP-0018` và `TP-IMP-0034` đã hoàn tất.
- **DoD:** Xanh toàn bộ kiểm thử đồng bộ OCI/chữ ký/cách ly tự động và 2 gói hãng mới, vượt qua `REQUIRES_STRONG_REVIEW`.
