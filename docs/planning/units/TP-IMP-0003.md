# `TP-IMP-0003` — Đóng Đặc tả Lô C: Hoàn thiện DDL `11` Bảng Thiếu, `Project DEK`, Khóa `(org_id, id)`/`FORCE RLS`, Chuỗi Băm `Audit` & `Valkey {org_id}`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0003` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes`) · `EP-00` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0001`, `TP-IMP-0002` & quyết định `CR-06, CR-07` — `GAP-01, 04..06`)* |
| **Quy mô (`Size`)** | `M` (`8` tệp kiến trúc dữ liệu, bảo mật, vận hành + tệp SQL chuẩn đối chiếu `db/spec-check/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Chạm lược đồ mật mã phong bì, cô lập RLS đa thuê bao & sổ cái)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Data Architect + Cryptography/Security Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Đóng toàn bộ lỗ hổng cơ sở dữ liệu, mật mã học và bộ nhớ đệm phân tán phát hiện ở kiểm toán A08 (`AUD-003`, `004`, `010`, `026`, `029`) để lược đồ `ARCH-DATA-01` và `ARCH-OPS-01` khớp `100%` giữa `SQLite WAL` và `PostgreSQL 16+` mà không một kỹ sư lưu trữ nào phải tự chế tên bảng, kiểu khóa hay công thức mã hóa `AEAD AAD`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - Tệp SQL tham chiếu chuẩn để kiểm chứng cú pháp tự động: `db/spec-check/sqlite_schema.sql`, `db/spec-check/postgres_schema.sql`.
  - Tài liệu kiến trúc, bảo mật & vận hành: `docs/architecture/data-model.md`, `docs/architecture/deployment-topology.md`, `docs/architecture/system-design.md`, `docs/security/secrets-and-tenant-isolation.md`, `docs/security/plugin-sandbox-and-supply-chain.md`, `docs/operations/backups-recovery.md` (cập nhật xóa bộ nhớ đệm RAM `DEK` trong `DR-03`).
  - Truy vết & sổ khoảng trống: `docs/requirements/traceability.md`, `docs/reviews/unresolved-gaps.md`.
- **Out-of-Scope:** Chưa viết mã Go migration thực thi (thuộc `TP-IMP-0007` và `TP-IMP-0039`).

## 3. Requirements, Dependencies & Design References
- **Requirements & Gaps Refs:** `GAP-04`, `GAP-05`, `GAP-06`; `TP-FR-0204`, `0501`, `0704`, `0801`, `0802`, `1003`, `1005`, `TP-NFR-0402`, `0404`, `0501`, `0502`.
- **Dependencies:** `TP-IMP-0001`, `TP-IMP-0002`.
- **Design / Audit Refs:** [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-06`, `CR-07`), [`ADR-0003`](../../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md), [`ADR-0005`](../../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md).

## 4. API, Wire & Data Contracts
- **Bổ sung Đầy đủ DDL Cho 11 Bảng Còn Thiếu (Tổng cộng `26` Bảng Chuẩn Toàn Hệ thống):**
  `config_changelog`, `distributed_leases`, `usage_rollups_hourly`, `oauth_authorization_sessions`, `connection_secret_bindings`, `org_extension_policies`, `extension_revocations`, `a2a_tasks`, `webhook_subscriptions`, `project_encryption_keys`, và `valkey_hold_checkpoints` (bảng đối chiếu nhịp giữ chỗ ngân sách mỗi `1 giây` từ Valkey xuống PostgreSQL theo `CR-07` gồm `(org_id, ledger_id, node_id, active_hold_micro_usd, updated_at_ms)`).
- **Chuẩn hóa Định danh `UUIDv7` & Ràng buộc Đa Thuê bao:**
  - Định danh khóa chính dùng chuỗi `<prefix>_<26_chars_crockford_base32_uuidv7>` (tổng độ dài `<= 32` ký tự, luôn vừa khít `VARCHAR(40)`).
  - Cho phép `extension_installations.active_version_id` nhận `NULL` khi đang chờ duyệt quyền; cho phép `approval_requests.org_id` / `project_id` nhận `NULL` cho tác vụ cấp `Instance`; thêm chỉ mục duy nhất một phần `WHERE org_id IS NULL` trên `principals`.
  - Áp dụng khóa tổ hợp `(org_id, id)` trên mọi bảng con thuộc thuê bao kèm câu lệnh `ENABLE` + `FORCE ROW LEVEL SECURITY`.
  - Bổ sung `version BIGINT` cho `environments`, `mcp_tools`, `agent_endpoints`, `budget_ledgers`, `approval_requests`; `owner_project_id` (tùy chọn `NULL` cho khóa cấp `Org`) trên `secret_credentials`; `owner_principal_id` & `allowed_models_json` trên `virtual_keys`; `idle_chunk_timeout_ms` trên `model_deployments`; `waterfall_stages_json` trên `request_traces_meta`; `project_id` trên `debug_payload_blobs`; thời gian chuẩn `BIGINT epoch_ms`.
- **Chuẩn hóa Mật mã, Sổ cái & Chuỗi Băm Kiểm toán (`CR-06, 07`):**
  - **2 Công thức `AEAD AAD` Chuẩn Duy Nhất:**
    1. Bí mật cấp `Org` (`Org DEK`): `"tanpopo:v1:cred:" + org_id + ":" + credential_id + ":" + secret_kind`.
    2. Bí mật/thân gỡ lỗi/mục cache cấp `Project` (`Project DEK`): `"tanpopo:v1:proj:" + org_id + ":" + project_id + ":" + record_kind + ":" + record_id`.
  - **Khóa Ảo (`Virtual Key`):** Tiền tố `vk_tp_<env>_<32_base62>`, lưu mã băm `SHA-256(rawKey)` thuần trong 2 cột `active_key_hash` và `previous_key_hash`.
  - **Chuỗi Băm Kiểm toán (`audit_events`):** Tách chuỗi độc lập cho từng `org_id` **cộng 1 chuỗi `SYSTEM` dành riêng cho các sự kiện cấp máy chủ có `org_id IS NULL`** (như `Bootstrap` hoặc `LOCAL_BREAK_GLASS_OWNER_RESET`), băm trên `RFC 8785 JCS(metadata_json)`.
  - **Valkey Cluster:** Lược đồ khóa `tp:v1:{<org_id>}:...` bắt buộc cặp ngoặc nhọn hash-tag `{<org_id>}`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `db/spec-check/sqlite_schema.sql` và `db/spec-check/postgres_schema.sql`, When đối chiếu với mọi bảng được gọi trong `data-model.md`, `system-design.md`, `deployment-topology.md`, `screen-specifications.md` và toàn bộ các đơn vị `TP-IMP-0007..0042`, Then đủ `26/26` bảng trùng tên từng ký tự (`0` bảng mồ côi).
- **`AC-2`:** Given 2 công thức `AEAD AAD` và định dạng `vk_tp_`, When kiểm tra trên `data-model.md`, `system-design.md` và `secrets-and-tenant-isolation.md`, Then trùng khớp `100%`.
- **`AC-3`:** Given quy trình xóa 1 `Project` đơn lẻ (`OPS-AC-09`) và `DR-03`, When hủy dòng tương ứng trong `project_encryption_keys` và xóa cache RAM, Then toàn bộ bí mật riêng và `debug_payload_blobs` của dự án đó trên bản sao lưu cũ mất khả năng giải mã mà không ảnh hưởng dự án anh em.
- **`NEG-1`:** Không để tồn tại bất kỳ câu lệnh Lua đa khóa nào trên Valkey thiếu cặp ngoặc nhọn hash-tag `{<org_id>}`.
- **`NEG-2`:** Không để chuỗi băm `audit_events` của `Org A` phụ thuộc vào hàng ẩn của `Org B` hoặc sự kiện `SYSTEM` làm gãy kiểm chứng dưới `FORCE RLS`.
- **`NEG-3`:** Không cho phép chèn trùng 2 bút toán `usage_settlements` cho cùng 1 `request_id` (`UNIQUE (request_id, settled_at)`).

## 6. Test Commands & Required Evidence
- Kiểm tra tính nhất quán bảng và công thức mật mã bằng kịch bản Node.js đa nền tảng (kèm kiểm tra chạy thực tế trên SQLite/Postgres ở `TP-IMP-0007` và `TP-IMP-0039`):
  - `node -e "const fs=require('fs');['db/spec-check/sqlite_schema.sql','db/spec-check/postgres_schema.sql'].forEach(f=>{const s=fs.readFileSync(f,'utf8');['config_changelog','distributed_leases','usage_rollups_hourly','oauth_authorization_sessions','connection_secret_bindings','org_extension_policies','extension_revocations','a2a_tasks','webhook_subscriptions','project_encryption_keys','valkey_hold_checkpoints'].forEach(t=>{if(!s.includes(t))throw new Error(f+' missing '+t)})})"`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Cô lập đa thuê bao phải giữ vững đồng thời ở tầng khóa ngoại SQL, `PostgreSQL FORCE RLS`, khóa `AEAD AAD` và tiền tố hash-tag `{<org_id>}` trên Valkey.
- **Rollback:** Revert nguyên tử trên nhánh tài liệu nếu cần điều chỉnh theo kết quả đo `TP-IMP-0004`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` và `TP-IMP-0002` đã hoàn tất; Product Owner đã duyệt `CR-06` và `CR-07`.
- **DoD:** Đóng toàn bộ `11/11` `CRITICAL` gaps của báo cáo kiểm toán A08 trong [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md); sẵn sàng trình ký `GATE-M0` sau khi xong `TP-IMP-0004`.
