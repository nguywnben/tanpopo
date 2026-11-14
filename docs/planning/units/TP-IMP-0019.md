# `TP-IMP-0019` — Động cơ Chính sách Phân cấp (`Org ∩ Project ∩ Env` Thu hẹp Đơn điệu, `Fail-Closed`) & Giới hạn Tốc độ `RPM/TPM/Concurrency`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0019` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0009`, `TP-IMP-0015` — `GAP-01, 11`)* |
| **Quy mô (`Size`)** | `M` (`internal/policy/engine/*`, `internal/ratelimit/*`, `db/migrations/sqlite/0006_policies.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Cổng kiểm soát truy cập `Fail-Closed` & chống nới lỏng chính sách cấp dưới)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Security / Policy Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa động cơ hợp nhất chính sách phân cấp 4 tầng (`Org ∩ Project ∩ Environment ∩ VirtualKey`) theo đại số **Thu hẹp Đơn điệu (`Monotonic Narrowing` — `TP-FR-0701`)**, chế độ lỗi mặc định đóng (`Fail-Closed` cho mọi cổng bảo mật, danh sách mô hình/miền địa lý/plugin cho phép), và bộ giới hạn tốc độ 3 chiều trong bộ nhớ đơn nút (`RPM` — yêu cầu/phút, `TPM` — token/phút, và `MaxConcurrentStreams` — số luồng đồng thời tối đa) chuẩn bị sẵn giao diện `RateLimiterPort` cho `Valkey` ở `v0.3`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/policy/engine/merge.go` (toán tử giao tập hợp `Allowlist = A_org ∩ A_proj ∩ A_env ∩ A_vkey`, hợp tập cấm `Denylist = D_org ∪ D_proj ∪ D_env ∪ D_vkey`, lấy cực tiểu `Limit = min(L_org, L_proj, L_env, L_vkey)` và kiểm tra lúc lưu cấu hình trả `422 policy_relaxation_forbidden` nếu cấp con cố mở rộng quyền đã bị cấp cha khóa).
  - `internal/policy/engine/evaluator.go` (đánh giá chính sách trên `ConfigSnapshot` trong `< 0.2 ms`, trả mã lý do từ chối tường minh `policy_denied_model`, `policy_denied_region`, `policy_denied_max_context`).
  - `internal/ratelimit/limiter.go` (thuật toán `GCRA / Token Bucket` trong bộ nhớ dùng đồng hồ đơn điệu `Clock` cho `RPM`, `TPM` ước tính trước + điều chỉnh sau luồng, và semaphore có giải phóng `defer` cho `MaxConcurrentStreams`, trả chuẩn `429 rate_limit_exceeded` kèm header `Retry-After`, `X-RateLimit-Remaining-*`).
  - `db/migrations/sqlite/0006_policies.sql` (`policies`, `org_extension_policies`).
- **Out-of-Scope:** Bộ đếm phân tán bằng kịch bản `Valkey Lua` toàn cụm (thuộc `TP-IMP-0040`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0701`, `TP-FR-0702`, `TP-FR-0703`, `TP-BR-0501`, `TP-BR-0502`, `TP-AC-SEC-05`, `GAP-11`.
- **Dependencies:** `TP-IMP-0009`, `TP-IMP-0015`.
- **Design Refs:** [`routing-and-policy.md`](../../specifications/routing-and-policy.md) (`SPEC-CORE-01` Mục 1 & 2), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`GAP-11`, `CR-04`).

## 4. API, Wire & Data Contracts
- **Bất biến Thu hẹp Đơn điệu (`Monotonic Narrowing`):** Nếu `Org` chỉ cho phép `allowed_regions = ["eu-central-1", "eu-west-1"]`, mọi nỗ lực lưu chính sách `Project` chứa `"us-east-1"` bị từ chối lúc ghi với `422 policy_relaxation_forbidden`; ngay cả khi dữ liệu cũ còn sót do `Org` mới siết lại, phép giao lúc chạy `Org ∩ Project` vẫn tự động loại `"us-east-1"`.
- **Ma trận `Fail-Closed`:** Nếu bản ghi chính sách bị hỏng hoặc không nạp được từ `ConfigSnapshot` → từ chối `503 policy_engine_unavailable`, tuyệt đối không cho lọt yêu cầu không kiểm soát.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `Org` đặt `RPM = 100`, `Project` đặt `RPM = 30`, `VirtualKey` đặt `RPM = 10`, When bắn 11 yêu cầu trong cùng 1 giây (dùng `FakeClock`), Then 10 yêu cầu đầu qua lọt và yêu cầu thứ 11 bị chặn đúng `429 rate_limit_exceeded` kèm `Retry-After` > 0.
- **`AC-2`:** Given `MaxConcurrentStreams = 2` trên một `VirtualKey`, When mở đồng thời 2 luồng SSE đang treo và gửi luồng thứ 3, Then luồng thứ 3 nhận `429 concurrency_limit_exceeded`; ngay khi đóng 1 trong 2 luồng đầu, luồng tiếp theo được chấp nhận ngay lập tức (`0` rò rỉ slot ngay cả khi client ngắt kết nối đột ngột).
- **`NEG-1` (`GAP-11` Relaxation Attempt):** `project_admin` gửi cấu hình chính sách cố bật lại một `ProviderAdapter` hoặc mô hình đã bị `org_admin` đưa vào danh sách cấm của `Org` → nhận `422 policy_relaxation_forbidden`.
- **`NEG-2` (`TPM Burst Abuse`):** Gửi một yêu cầu đơn lẻ có `max_tokens` + độ dài ngữ cảnh ước tính vượt quá hạn ngạch `TPM` của khóa → bị chặn trước khi gọi lên nhà cung cấp.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/policy/... ./internal/ratelimit/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Bộ giải phóng slot đồng thời (`ReleaseConcurrencySlot`) luôn gắn với `sync.Once` trong `defer` của vòng đời HTTP handler để không thể xảy ra lỗi giải phóng kép (âm bộ đếm) hoặc kẹt slot vĩnh viễn.
- **Rollback:** Bảng cộng thêm `0006_policies.sql`; cấu hình mặc định của `Org` vẫn bảo toàn tính đóng cho danh sách miền mạng.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (bổ sung `AC` cho `GAP-11`), `TP-IMP-0009` và `TP-IMP-0015` đã hoàn tất.
- **DoD:** Đạt độ phủ kiểm thử `>= 90%` trên đại số hợp nhất chính sách & giới hạn tốc độ `RPM/TPM/Concurrency`, vượt qua `REQUIRES_STRONG_REVIEW`.
