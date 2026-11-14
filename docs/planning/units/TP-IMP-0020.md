# `TP-IMP-0020` — Bộ Giải quyết Định tuyến 6 Bước Nhận thức Năng lực, Khóa Cứng `Data Residency` & Ngắt mạch Cửa sổ Trượt `SM-04`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0020` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-04` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0016`, `TP-IMP-0019` — `GAP-01, 08`)* |
| **Quy mô (`Size`)** | `L` (`internal/routing/resolver/*`, `internal/routing/breaker/*`, `db/migrations/sqlite/0007_routes.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Chốt chặn không rò rỉ dữ liệu xuyên biên giới địa lý khi fallback & máy trạng thái ngắt mạch)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Distributed Systems / Routing Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa thuật toán định tuyến xác định 6 bước của Tanpopo theo [`routing-and-policy.md`](../../specifications/routing-and-policy.md) (`SPEC-CORE-01` Mục 3 & 4): (1) giải bí danh `LogicalModel` / `RouteRule`, (2) lọc cứng theo chính sách & vùng cư trú dữ liệu `Data Residency`, (3) lọc cứng theo ma trận năng lực yêu cầu (`tools`, `vision`, `json_schema`, `context_window`), (4) lọc theo sức khỏe kết nối `SM-02` & cầu dao ngắt mạch `SM-04`, (5) xếp hạng ứng viên theo chiến lược (`PRIORITY_FAILOVER`, `WEIGHTED_ROUND_ROBIN`, `LOWEST_COST`, `LOWEST_LATENCY_EWMA`), và (6) thực thi chuyển đổi dự phòng **chỉ trước khi byte SSE đầu tiên được gửi đi (`Pre-Stream Failover Only`)** kèm dấu vết từng bước thử (`RouteAttemptTrace`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/routing/resolver/resolver.go` (thuật toán chọn ứng viên 6 bước + hàm `Explain(req)` không tác động phụ trả về lý do loại/chọn từng `ModelDeployment` phục vụ gỡ lỗi & màn hình `Dry-Run` sau này).
  - `internal/routing/breaker/circuit_breaker.go` (cầu dao cửa sổ trượt theo từng `ModelDeployment` tuân thủ đúng các trạng thái chuẩn `SM-04`: `PROVISIONED -> ACTIVE_HEALTHY <-> THROTTLED_COOLDOWN | CIRCUIT_OPEN_UNHEALTHY -> HALF_OPEN_PROBING -> DISABLED`, phân biệt `429` có `Retry-After` sang `THROTTLED_COOLDOWN`, lỗi hạ tầng `500/502/503/timeout` sang `CIRCUIT_OPEN_UNHEALTHY`, còn lỗi đầu vào khách `400/422` **không** làm nhảy cầu dao).
  - `internal/routing/failover/executor.go` (vòng lặp thử ứng viên kế tiếp trong ngân sách thời gian tổng `request_deadline`, khóa cứng không vượt qua hàng rào `Data Residency` kể cả khi mọi đích trong vùng đều sập — `CT-08`, `INC-04`).
  - `db/migrations/sqlite/0007_routes.sql` (`route_policies`, `route_targets`).
- **Out-of-Scope:** Giao diện mô phỏng trực quan `Dry-Run Route Explain` trên Web Console (thuộc `TP-IMP-0038` ở `M3`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0102`, `TP-FR-0601`, `TP-FR-0602`, `TP-FR-0603`, `TP-BR-0501`..`0505`, `SM-04`, `CT-08`, `CT-12`, `INC-04`.
- **Dependencies:** `TP-IMP-0016`, `TP-IMP-0019`.
- **Design Refs:** [`routing-and-policy.md`](../../specifications/routing-and-policy.md) (`SPEC-CORE-01`), [`business-entity-map.md`](../../requirements/business-entity-map.md) (`SM-04`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-08`, `CT-12`).

## 4. API, Wire & Data Contracts
- **Toán học Cầu dao `SM-04`:** Chuyển sang `THROTTLED_COOLDOWN` đúng bằng số giây trong `Retry-After` khi thượng nguồn trả `429` (`CT-12`); mở mạch (`CIRCUIT_OPEN_UNHEALTHY`) khi số mẫu trong cửa sổ trượt `60s` `>= min_requests (10)` và tỷ lệ lỗi `>= failure_ratio (0.50)` hoặc `5` lỗi mạng liên tiếp; sau `open_cooldown (30s)` chuyển `HALF_OPEN_PROBING` cho phép đúng `1` yêu cầu thăm dò.
- **Cấm Vượt Rào Cư trú Dữ liệu Khi Dự phòng (`CT-08` & `INC-04`):** Chính sách `data_residency = ["EU"]` đứng ở Bước 2 — một đích dự phòng đặt tại `US` bị loại bỏ hoàn toàn khỏi tập ứng viên ngay từ đầu; nếu đích `EU` trả `503`, Gateway trả chuẩn **`503 no_compliant_deployment_available`** chứ không bao giờ rẽ sang `US`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given tuyến `prod-assistant` gồm `Primary (Deployment A - EU)` và `Fallback (Deployment B - EU)`, When `Deployment A` trả `503` hoặc hết hạn kết nối trước khi gửi header SSE, Then bộ điều phối tự động chuyển sang `Deployment B` thành công và ghi nhận `attempt_count = 2`.
- **`AC-2` (`CT-12` `429 Retry-After` Cooldown & `SM-04` Probing):** Given `Deployment A` trả `429` kèm `Retry-After: 12`, When xử lý phản hồi trước luồng, Then Gateway lập tức dự phòng sang `Deployment B` và đặt `Deployment A` vào `THROTTLED_COOLDOWN` đúng `12 giây` (`FakeClock`); tương tự khi `CIRCUIT_OPEN_UNHEALTHY`, sau `31s` sang `HALF_OPEN_PROBING`, 1 yêu cầu thăm dò thành công khôi phục về `ACTIVE_HEALTHY`.
- **`NEG-1` (`CT-08` Residency & Capability Breach on Failover):** Tuyến khai báo nhầm đích dự phòng `US` cho dự án khóa cứng `EU-only` (hoặc đích dự phòng thiếu `tool_calling`), khi đích chính sập → yêu cầu thất bại an toàn **`503 no_compliant_deployment_available`** (hoặc `400 capability_not_supported`), `0` gói tin gửi tới đích vi phạm.
- **`NEG-2` (`Client 400 Must Not Trip Breaker`):** Bắn 100 yêu cầu sai cú pháp `400 Bad Request` của khách hàng → `SM-04` vẫn giữ nguyên `ACTIVE_HEALTHY`.

## 6. Test Commands & Required Evidence
- Thuần Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/routing/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/routing/...`)*
- Tranh chấp dữ liệu (`-race`): `go test -v -race ./internal/routing/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `RouteResolver` chỉ lựa chọn các `ModelDeployment` thuộc cùng `org_id` (hoặc tài nguyên chia sẻ được `Org` cấp phép cho `Project`); không bao giờ định tuyến chéo kết nối giữa hai tổ chức.
- **Rollback:** Bảng cộng thêm `0007_routes.sql`; mặc định định tuyến trực tiếp 1-1 theo tên `LogicalModel` nếu chưa cấu hình quy tắc nâng cao.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (vá `SM-04`), `TP-IMP-0016` và `TP-IMP-0019` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca kiểm thử `SM-04`, `CT-08`, `CT-12` dưới `-race` + `FakeClock`, vượt qua `REQUIRES_STRONG_REVIEW`.
