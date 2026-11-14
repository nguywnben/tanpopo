# `TP-IMP-0003` — Đóng Đặc tả Lô C: Hoàn thiện DDL `8` Bảng Thiếu, `Project DEK`, Khóa `(org_id, id)`/`FORCE RLS`, Chuỗi Băm `Audit` & `Valkey {org_id}`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0003` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes`) · `EP-00` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0001`, `TP-IMP-0002` & quyết định `CR-06, CR-07` — `GAP-01, 04..06`)* |
| **Quy mô (`Size`)** | `M` (`4` tệp kiến trúc dữ liệu, topo & bảo mật) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Chạm lược đồ mật mã phong bì, cô lập RLS đa thuê bao & sổ cái)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Data Architect + Cryptography/Security Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Đóng toàn bộ lỗ hổng cơ sở dữ liệu, mật mã học và bộ nhớ đệm phân tán phát hiện ở kiểm toán A08 (`AUD-003`, `004`, `010`, `026`, `029`) để lược đồ `ARCH-DATA-01` và `ARCH-OPS-01` khớp `100%` giữa `SQLite WAL` và `PostgreSQL 16+` mà không một kỹ sư lưu trữ nào phải tự chế bảng hay công thức mã hóa.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `docs/architecture/data-model.md`
  - `docs/architecture/deployment-topology.md`
  - `docs/architecture/system-design.md`
  - `docs/security/secrets-and-tenant-isolation.md`
- **Out-of-Scope:** Chưa viết mã Go migration thực thi (thuộc `TP-IMP-0007` và `TP-IMP-0039`).

## 3. Requirements, Dependencies & Design References
- **Requirements & Gaps Refs:** `GAP-04`, `GAP-05`, `GAP-06`; `TP-FR-0204`, `0501`, `0704`, `0801`, `0802`, `1003`, `1005`, `TP-NFR-0402`, `0404`, `0501`, `0502`.
- **Dependencies:** `TP-IMP-0001`, `TP-IMP-0002`.
- **Design / Audit Refs:** [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-06`, `CR-07`), [`ADR-0003`](../../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md), [`ADR-0005`](../../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md).

## 4. API, Wire & Data Contracts
- **Bổ sung Đầy đủ DDL Cho 8 Bảng Còn Thiếu:** `config_changelog`, `distributed_leases`, `usage_rollups_hourly`, `oauth_authorization_sessions`, `connection_secret_bindings`, `org_extension_policies`, `extension_revocations`, `a2a_tasks` và `webhook_subscriptions` + bảng `project_encryption_keys`.
- **Sửa Ràng buộc Quan hệ & Đa Thuê bao:**
  - Cho phép `extension_installations.active_version_id` nhận `NULL` khi đang dàn dựng chờ duyệt; cho phép `approval_requests.org_id` / `project_id` nhận `NULL` cho tác vụ cấp `Instance`; thêm chỉ mục duy nhất một phần `WHERE org_id IS NULL` trên `principals`.
  - Áp dụng khóa tổ hợp `(org_id, id)` trên mọi bảng con thuộc thuê bao (chặn nối chéo `route_targets` hoặc `provider_connections` sang `Org` khác trên cả SQLite và Postgres) kèm câu lệnh mẫu `ENABLE` + `FORCE ROW LEVEL SECURITY`.
  - Bổ sung `version BIGINT` cho `environments`, `mcp_tools`, `agent_endpoints`, `budget_ledgers`, `approval_requests`; bổ sung `owner_principal_id` & `allowed_models_json` cho `virtual_keys`, `idle_chunk_timeout_ms` cho `model_deployments`, `waterfall_stages_json` cho `request_traces_meta`, `project_id` cho `debug_payload_blobs`, và khóa thời gian SQLite là `BIGINT epoch_ms`.
- **Chuẩn hóa Mật mã, Sổ cái & Valkey Cluster:**
  - Công thức `AEAD AAD` duy nhất trên mọi tệp: `"tanpopo:v1:cred:" || org_id || ":" || credential_id || ":" || secret_kind` (và tương ứng cho `debug_payload_blobs` bọc bởi `Project DEK`); băm `Virtual Key` bằng `SHA-256` thuần.
  - Bổ sung `UNIQUE (request_id, settled_at)`, `media_usage_json` và `applied_pricing_snapshot_json` trên `usage_settlements`; đổi `audit_events` sang chuỗi băm theo từng `org_id` có `seq_no BIGINT` trên `RFC 8785 JCS(metadata_json)`.
  - Khóa lược đồ khóa Valkey có hash-tag `tp:v1:{<org_id>}:...` chống `CROSSSLOT` và bảng nhịp đồng bộ giữ chỗ khi sập Valkey.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given toàn bộ khối DDL trong `data-model.md`, When đối chiếu với mọi bảng được gọi trong `system-design.md`, `deployment-topology.md` và `screen-specifications.md`, Then `0` bảng hoặc cột nào bị thiếu.
- **`AC-2`:** Given công thức `AEAD AAD` và thuật toán băm `Virtual Key`, When kiểm tra trên cả `data-model.md`, `system-design.md` và `secrets-and-tenant-isolation.md`, Then trùng khớp từng ký tự `100%`.
- **`AC-3`:** Given quy trình xóa 1 `Project` đơn lẻ (`OPS-AC-09`), When hủy dòng tương ứng trong `project_encryption_keys`, Then toàn bộ bí mật riêng và `debug_payload_blobs` của dự án đó trên bản sao lưu cũ mất khả năng giải mã mà không ảnh hưởng dự án anh em trong cùng `Organization`.
- **`NEG-1`:** Không để tồn tại bất kỳ câu lệnh Lua đa khóa nào trên Valkey thiếu cặp ngoặc nhọn hash-tag `{<org_id>}`.
- **`NEG-2`:** Không để chuỗi băm `audit_events` của `Org A` phụ thuộc vào hàng ẩn của `Org B` làm gãy kiểm chứng dưới `FORCE RLS`.
- **`NEG-3`:** Không cho phép chèn trùng 2 bút toán `usage_settlements` cho cùng 1 `request_id` khi retry lô ghi.

## 6. Test Commands & Required Evidence
- `sqlite3 :memory: < db/spec-check/sqlite_schema.sql` & `psql -v ON_ERROR_STOP=1 --single-transaction -f db/spec-check/postgres_schema.sql` *(kiểm chứng thực nghiệm toàn bộ khối DDL 23+ bảng đạt `0` lỗi cú pháp, khóa ngoại hoặc phân vùng)*.
- `rg -n "tanpopo:v1:cred:" docs/architecture/ docs/security/` *(xác nhận duy nhất 1 công thức `AEAD AAD` đồng nhất trên toàn bộ tài liệu)*.
- Cập nhật [`traceability.md`](../../requirements/traceability.md) xác nhận đóng sạch `GAP-04`, `GAP-05`, `GAP-06`.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Cô lập đa thuê bao phải giữ vững đồng thời ở tầng khóa ngoại SQL, `PostgreSQL FORCE RLS`, khóa `AEAD AAD` và tiền tố hash-tag `{<org_id>}` trên Valkey.
- **Rollback:** Revert nguyên tử trên nhánh tài liệu nếu cần điều chỉnh theo kết quả đo `TP-IMP-0004`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` và `TP-IMP-0002` đã hoàn tất; Product Owner đã duyệt `CR-06` và `CR-07`.
- **DoD:** Đóng toàn bộ `11/11` `CRITICAL` gaps của báo cáo kiểm toán A08; sẵn sàng trình ký `GATE-M0` sau khi xong `TP-IMP-0004`.
