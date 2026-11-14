# `TP-IMP-0040` — Điều phối Cụm `Valkey` (`tp:v1:{org_id}:*` Lua), Thu hồi Toàn Cụm `< 5s` 3 Lớp, Ngắt mạch Nút Mù `> 10s` & Helm Chart Tách Vai trò `--role`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0040` |
| **Milestone / Epic** | `M4` (`v0.3 Clustered HA, Enterprise Security & A2A`) · `EP-08` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0033`, `TP-IMP-0039` — `GAP-01, 06`)* |
| **Quy mô (`Size`)** | `L` (`go.mod`, `internal/cluster/valkey/*`, `internal/config/snapshot/cluster_sync.go`, `internal/cli/admin/verify_state.go`, `deploy/helm/tanpopo/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Không bị lỗi `CROSSSLOT` trên cụm phân mảnh, truyền tin thu hồi `< 5s` toàn cụm & tự ngắt mạch nút mất liên lạc `FMEA-07`)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Distributed Systems / SRE Architect` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa mặt phẳng điều phối trạng thái phân tán cho chế độ **Clustered Scale-Out HA (`TP-FR-1005`, `ADR-0006`, `ARCH-OPS-01`, `CR-07`)**:
1. **Kịch bản Nguyên tử `Valkey / Redis` Tương thích `Cluster Hash-Tag` (`tp:v1:{<org_id>}:*` — `CR-07`):** Mọi khóa bộ đếm tốc độ (`RPM`, `TPM`, `Concurrency`), khóa chống gửi trùng `Idempotency-Key` và bộ đệm giữ chỗ ngân sách nhanh của cùng một `Organization` đều dùng chung thẻ băm `{<org_id>}` để kịch bản `Lua` đa khóa không bao giờ gặp lỗi `CROSSSLOT` trên `Valkey Cluster`, kèm nhịp đối chiếu `valkey_hold_checkpoints` mỗi `1 giây` xuống PostgreSQL và lệnh đối soát phục hồi sau PITR **`tanpopo admin verify-state --check-audit-chain --reconcile-orphaned-holds --rebuild-valkey-state` (`DR-02`)**.
2. **Truyền tin Thu hồi Khóa & Công tắc Khẩn cấp 3 Lớp Đạt `< 5 giây` Toàn Cụm (`SLO-07`, `FMEA-07`, `INC-07`):** Kết hợp (Lớp 1) `Valkey Pub/Sub`, (Lớp 2) `PostgreSQL LISTEN/NOTIFY`, và (Lớp 3) vòng kéo đối chiếu bảng chuẩn **`config_changelog`** mỗi `2 giây`. Nếu một nút Data Plane mất kết nối tới cả PostgreSQL lẫn Valkey quá **`10 giây` (`Stale-Snapshot Circuit Breaker`)**, nút đó lập tức chuyển `/readyz -> 503` và **từ chối nhận yêu cầu bằng khóa ảo (`Fail-Closed`)**.
3. **Biểu đồ `Helm Chart` Sản xuất Tách Biệt Vai trò (`--role=data-plane` vs `--role=control-plane`):** Hỗ trợ `HPA`, `PodDisruptionBudget`, `NetworkPolicy` và nâng cấp cuốn chiếu 3 nút không rớt bất kỳ luồng SSE nào (`OPS-AC-06`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (thêm trình khách thuần Go `github.com/valkey-io/valkey-go`).
  - `internal/cluster/valkey/ratelimit_lua.go` & `budget_hold_lua.go` (kịch bản Lua nguyên tử dùng khóa `tp:v1:{<org_id>}:rl:*` và `tp:v1:{<org_id>}:budget:*` + nhịp ghi `valkey_hold_checkpoints` + chế độ suy giảm an toàn về PostgreSQL khi Valkey chập chờn).
  - `internal/config/snapshot/cluster_sync.go` (bộ đồng bộ 3 lớp trên `config_changelog` + cầu dao tự ngắt nút mù `max_staleness = 10s`).
  - `internal/cli/admin/verify_state.go` (`tanpopo admin verify-state --check-audit-chain --reconcile-orphaned-holds --rebuild-valkey-state` phục vụ `DR-02`).
  - `deploy/helm/tanpopo/*` (`Chart.yaml`, `values.yaml`, Deployment riêng cho `data-plane` và `control-plane`, `PDB`, `NetworkPolicy`, `ServiceMonitor` kèm kiểm tra `helm lint` & `kubeconform`).
- **Out-of-Scope:** Đo tải lớn `1.000` luồng SSE kéo dài (thuộc `TP-IMP-0043` ở `M5`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-1005`, `TP-NFR-0201`, `TP-NFR-0301`, `TP-NFR-0302`, `TP-NFR-0404`, `SLO-01`, `SLO-04`, `SLO-06`, `SLO-07`, `ADR-0006`, `CR-07`, `FMEA-07`, `DR-02`, `OPS-AC-06`, `INC-07`.
- **Dependencies:** `TP-IMP-0033`, `TP-IMP-0039`.
- **Design Refs:** [`ADR-0006`](../../architecture/adr/0006-immutable-config-snapshots-and-fast-revocation.md), [`deployment-topology.md`](../../architecture/deployment-topology.md) (`ARCH-OPS-01`), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-07`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-07`).

## 4. API, Wire & Data Contracts
- **Quy ước Không gian Khóa `Valkey` Chuẩn (`CR-07`):** `tp:v1:{<org_id>}:<subsystem>:<entity_id>` — cấm tuyệt đối bất kỳ lệnh `EVAL`/`MGET` nào chạm vào khóa của hai `org_id` khác nhau trong cùng một lệnh.
- **Hành vi Khi Mất `Valkey` Nhưng Còn `PostgreSQL`:** Giới hạn tốc độ tạm thời rơi về bộ đếm cục bộ chia theo số nút ước tính (`Local Fallback Quotient`), còn giữ chỗ ngân sách và kiểm tra thu hồi khóa rơi thẳng về `PostgreSQL` để đảm bảo không vượt ngân sách và không lọt khóa bị thu hồi.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`Cluster-Wide < 5s Revocation SLA`):** Khởi chạy 3 thể hiện `data-plane` + 1 `control-plane` nối chung PostgreSQL + Valkey; khi gọi thu hồi khẩn cấp `Virtual Key K1` trên `control-plane`, cả 3 nút `data-plane` đều từ chối `K1` (`401`) và cắt luồng SSE đang mở của `K1` trong **`< 1 giây`** (và dưới **`< 3 giây`** ngay cả khi tắt kênh `Valkey Pub/Sub` để ép chạy qua lớp dự phòng!).
- **`AC-2` (`FMEA-07` Partitioned Node Fail-Closed > 10s):** Cô lập mạng 1 nút `data-plane` khỏi cả PostgreSQL và Valkey; sau đúng `10 giây` (`FakeClock` / cổng proxy cắt kết nối), nút bị cô lập chuyển `/readyz -> 503` và từ chối yêu cầu xác thực thay vì phục vụ bằng ảnh chụp cấu hình cũ mù quáng.
- **`AC-3` (`CR-07` Valkey Cluster Hash-Slot Safety & `DR-02` `verify-state`):** Chạy bộ kiểm thử kịch bản Lua trên cụm `Valkey Cluster` 3 phân mảnh xác nhận `0` lỗi `CROSSSLOT`; xóa trắng Valkey rồi chạy `tanpopo admin verify-state --check-audit-chain --reconcile-orphaned-holds --rebuild-valkey-state` tái tạo lại trạng thái từ PostgreSQL thành công `100%`.
- **`AC-4` (`OPS-AC-06` Zero-Drop 3-Node Rolling Restart Under 100 Active SSE Streams):** Khởi động 3 nút `data-plane` dưới tải 100 luồng SSE đang truyền dở, thực hiện khởi động lại cuốn chiếu lần lượt từng nút (`SIGTERM` + xả luồng êm) → `0` lỗi `502/503` trả về cho khách.
- **`NEG-1`:** Kiểm định biểu đồ Helm bằng `helm template` + `kubeconform` xác nhận không có container nào chạy quyền `root` hay mở `NetworkPolicy` tràn lan.

## 6. Test Commands & Required Evidence
- `TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/cluster/... ./internal/config/snapshot/...`
- `helm lint deploy/helm/tanpopo`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Không bao giờ lưu khóa rõ `SecretCredential` hay nội dung câu lệnh vào `Valkey`; kết nối tới `Valkey` và `PostgreSQL` hỗ trợ bắt buộc `TLS` trong cấu hình sản xuất.
- **Rollback:** Chế độ đơn nút (`--storage=sqlite`) hoàn toàn không bị ảnh hưởng bởi gói `internal/cluster/valkey`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` (khóa lược đồ `Valkey {org_id}` tại `CR-07`), `TP-IMP-0033` và `TP-IMP-0039` đã hoàn tất.
- **DoD:** Xanh toàn bộ kiểm thử cụm 4 nút, `FMEA-07`, `Valkey Cluster` 3-shard và `helm lint`, vượt qua `REQUIRES_STRONG_REVIEW`.
