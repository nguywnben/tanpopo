# `TP-IMP-0012` — Động cơ Thông dịch Gói Khai báo Tầng 1 (`Declarative Adapter`) & Chuỗi 4 Thực thể Mô hình (`SM-02`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0012` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0004`, `TP-IMP-0010`, `TP-IMP-0011` — `GAP-01, 02`)* |
| **Quy mô (`Size`)** | `L` (`internal/extension/declarative/*`, `internal/provider/catalog/*`, `internal/config/snapshot/*`, `db/migrations/sqlite/0005_providers_models.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Bộ thông dịch biểu thức không Turing-complete chống ReDoS/OOM & ranh giới chuỗi 4 thực thể)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Platform / Runtime Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa:
1. **Chuỗi 4 thực thể chuẩn không nhập nhằng (`SPEC-EXT-02`):** `ProviderAdapter` (gói logic/biến đổi) → `ProviderConnection` (định danh kết nối, `allowed_hosts`, `allow_private_ip`, gắn khe bí mật qua `connection_secret_bindings` & máy trạng thái chuẩn `SM-02`) → `LogicalModel` (bí danh mô hình thống nhất kèm cờ năng lực `DECLARED / TESTED / VERIFIED` và cấu hình đơn giá `micro-USD`) → `ModelDeployment` (đích triển khai vật lý gắn vùng địa lý `region`, `idle_chunk_timeout_ms` và trọng số).
2. **Động cơ Thông dịch Gói Khai báo Tầng 1 (`Tier-1 Declarative Engine`):** Cài đặt đúng giao diện chung `extension/port.ProviderRuntime` (`TP-IMP-0010`) sử dụng tập toán tử hữu hạn đã khóa ở `POC-02` (`TP-IMP-0004`), đạt mục tiêu phụ phí `< 2 ms p95` (`SLO-02` `TARGET / UNVERIFIED`) và bề mặt thực thi mã tùy ý bằng `0`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/provider/catalog/repository.go` (CRUD & truy vấn `ProviderAdapter`, `ProviderConnection`, `ConnectionSecretBinding`, `LogicalModel`, `ModelDeployment`, ghi `config_changelog`, triển khai `broker.CredentialBindingResolver`).
  - `internal/config/snapshot/providers_view.go` (bổ sung bảng tra cứu `O(1)` cho chuỗi 4 thực thể vào `ConfigSnapshot`).
  - `internal/provider/catalog/connection_health.go` (thực thi đúng các trạng thái chuẩn `SM-02`: `DRAFT -> VALIDATING -> ACTIVE <-> DEGRADED | AUTH_EXPIRED_REAUTH_REQUIRED | SUSPENDED_BY_ADMIN | REVOKED | ARCHIVED` qua 2 pha `BuildValidationProbe` / `VerifyValidationResult`).
  - `internal/extension/declarative/engine.go` (triển khai `extension/port.ProviderRuntime` có giới hạn cứng độ sâu JSON `<= 32`, số bước toán tử `<= 256` mỗi thông điệp, cấm biểu thức chính quy quay lui `ReDoS`).
  - `db/migrations/sqlite/0005_providers_models.sql` (`provider_adapters`, `provider_connections` kèm `CHECK (billing_mode IN ('COMMERCIAL_API', 'SELF_HOSTED_INFRA'))`, `connection_secret_bindings`, `logical_models`, `model_deployments`).
- **Out-of-Scope:** Hồ thực thi mã `Wasm` Tầng 2 (thuộc `TP-IMP-0013` chạy song song trên gói riêng `internal/extension/wasm/*`); thuật toán định tuyến đa đích & chuyển đổi dự phòng (thuộc `TP-IMP-0020`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0201`, `TP-FR-0202`, `TP-FR-0203`, `TP-FR-0301`, `TP-NFR-0101`, `SLO-02`, `TP-BR-0102`, `TP-BR-0401`, `SM-02`, `FZ-01`.
- **Dependencies:** `TP-IMP-0004`, `TP-IMP-0010`, `TP-IMP-0011`.
- **Design Refs:** [`provider-adapter.md`](../../specifications/provider-adapter.md) (`SPEC-EXT-02`), [`data-model.md`](../../architecture/data-model.md) Mục 3.2, [`ADR-0005`](../../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md).

## 4. API, Wire & Data Contracts
- **Ranh giới Thanh toán Cứng (`DEC-004` & `ADR-0005`):** Cột `billing_mode` trên `provider_connections` chỉ chấp nhận `'COMMERCIAL_API'` hoặc `'SELF_HOSTED_INFRA'`; từ chối mọi giá trị cấu hình kiểu thuê bao cá nhân ở cả JSON Schema lẫn ràng buộc `CHECK` SQL.
- **Ba Cấp độ Bảo chứng Năng lực (`Capability Assurance Tier`):** Mỗi cờ năng lực (`streaming`, `tool_calling`, `vision`, `audio_input`, `document_input`, `strict_json_schema`, `reasoning`) mang nhãn `DECLARED`, `TESTED`, hoặc `VERIFIED`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given gói `.tpkg` `Tier-1 Declarative Adapter` cài qua `TP-IMP-0011`, When tạo `ProviderConnection` gắn `SecretCredential` qua `connection_secret_bindings`, khai báo `LogicalModel` + `ModelDeployment` trỏ về `FakeLLMServer` qua `BlindBroker` (`TP-IMP-0010`), Then kiểm tra kết nối chuyển `SM-02` từ `DRAFT -> VALIDATING -> ACTIVE` và biến đổi chuẩn xác cả yêu cầu đồng bộ lẫn khung SSE.
- **`AC-2`:** Given mô hình nội bộ tự vận hành (`vLLM` / `Ollama`) với `billing_mode = 'SELF_HOSTED_INFRA'`, `allow_private_ip = true` và `auth_mode = 'NONE'`, When định nghĩa `ModelDeployment`, Then hệ thống lưu trữ và tra cứu trên `ConfigSnapshot` thành công mà không đòi khóa API thương mại.
- **`NEG-1` (`FZ-01` Malicious Declarative Mapping):** Nạp tệp ánh xạ khai báo có vòng tham chiếu đệ quy, độ sâu lồng nhau `> 32` hoặc mẫu trích xuất bệnh lý → bị từ chối ngay lúc nạp hoặc dừng an toàn dưới `< 1 ms` mà không gây panic hay tràn bộ nhớ.
- **`NEG-2`:** Cố gắng tạo `ProviderConnection` với `billing_mode = 'CONSUMER_SUBSCRIPTION'` → bị từ chối `422 invalid_billing_mode` ở cả API lẫn ràng buộc CSDL.
- **`NEG-3`:** Xóa cứng `ProviderAdapter` hoặc `ProviderConnection` đang còn `ModelDeployment` hoạt động tham chiếu tới → bị chặn `409 resource_in_use` (`port.ErrResourceInUse`).

## 6. Test Commands & Required Evidence
- Kiểm tra thuần Go (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/extension/declarative/... ./internal/provider/catalog/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/extension/declarative/... ./internal/provider/catalog/...`
- Kiểm tra `-race` & Fuzzing (`FZ-01`):
  - `go test -v -race ./internal/extension/declarative/... ./internal/provider/catalog/...`
  - `go test -fuzz=FuzzDeclarativeEngine -fuzztime=30s ./internal/extension/declarative/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Bộ thông dịch Tầng 1 chỉ đọc dữ liệu từ `NormalizedRequest` / `UpstreamResponse` đã lọc — tuyệt đối không có toán tử đọc tệp hệ thống, đọc biến môi trường hay truy cập `vault`.
- **Rollback:** Bảng cộng thêm `0005_providers_models.sql`; đình chỉ kết nối đưa `SM-02` về `SUSPENDED_BY_ADMIN` an toàn.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (vá `SM-02`), `TP-IMP-0004` (chốt toán tử `POC-02`), `TP-IMP-0010` và `TP-IMP-0011` đã hoàn tất.
- **DoD:** Vượt qua bộ kiểm thử đơn vị, kiểm thử `FZ-01`, đo benchmark biến đổi khung `< 0.5 ms/op` trong bộ nhớ và đạt `REQUIRES_STRONG_REVIEW`.
