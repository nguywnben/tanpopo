# `TP-IMP-0023` — Nhật ký Kiểm toán Bất biến Nối Chuỗi Băm `JCS` Theo `Org`, Công tắc Khẩn cấp 4 Cấp (`< 1s` Cắt Luồng) & Truy vấn `Blast-Radius`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0023` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0009`, `TP-IMP-0011`, `TP-IMP-0022` — `GAP-01, 05`)* |
| **Quy mô (`Size`)** | `L` (`internal/security/audit/*`, `internal/security/killswitch/*`, `db/migrations/sqlite/0010_audit_killswitch.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Tính toàn vẹn bằng chứng pháp y chống sửa xóa `TM-13` & cắt đứt khẩn cấp sự cố an ninh)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Security & Incident Response Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Xây dựng hai năng lực phòng thủ và pháp y tối hậu của mặt phẳng quản trị (`TP-FR-0704`, `TP-FR-0705`):
1. **Nhật ký Kiểm toán Bất biến Nối Chuỗi Băm Theo Từng `Organization` (`Per-Org RFC 8785 JCS Hash-Chained Audit Log` — `CR-06`):** Mọi hành động quản trị, cấp/thu hồi khóa, cài/cách ly plugin, thay đổi chính sách và vi phạm bảo mật đều được chuẩn hóa JSON xác định theo `RFC 8785 (JCS)` và nối chuỗi băm `entry_hash = SHA-256(org_id || seq_no || prev_hash || jcs_payload)` kèm trigger CSDL cấm `UPDATE`/`DELETE` và trình xác minh chuỗi độc lập (`VerifyAuditChain`).
2. **Công tắc Ngắt Khẩn cấp 4 Cấp (`4-Level Emergency Kill-Switch`) & Truy vấn Tầm Ảnh hưởng (`Blast-Radius Query`):** Cho phép khóa tức thì ở cấp (1) `VirtualKey`, (2) `ProviderConnection`, (3) `ExtensionInstallation` (`QUARANTINED`), hoặc (4) toàn bộ `Project / Organization Traffic Freeze`, cắt đứt mọi luồng SSE đang mở thuộc phạm vi bị ngắt trong `< 1 giây` trên nút đơn (`OPS-AC-03`, `OPS-AC-04`) và liệt kê chính xác những dự án/khóa/yêu cầu nào vừa chạm tới phần mở rộng hoặc khóa bị lộ trong cửa sổ thời gian sự cố.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/security/audit/jcs.go` (chuẩn hóa JSON `RFC 8785` tất định cho khóa từ điển, chuỗi Unicode và số nguyên).
  - `internal/security/audit/logger.go` (ghi nối chuỗi tuần tự theo `org_id` với số thứ tự `seq_no` tăng liên tục không hở, che sạch bí mật qua `privacy.Redactor` và chế độ `Fail-Closed` cho hành động quản trị: nếu không ghi được bản ghi kiểm toán vào DB thì hủy luôn giao dịch thay đổi cấu hình!).
  - `internal/security/audit/verifier.go` (kiểm định toàn bộ chuỗi băm theo `org_id`, phát cảnh báo `ALT-AUDIT-TAMPER` ngay khi phát hiện sửa 1 bit hoặc xóa dòng giữa chuỗi — `CT-30`, `OPS-AC-08`).
  - `internal/security/killswitch/registry.go` (bảng đăng ký các luồng `InFlightStream` gắn thẻ `(org_id, project_id, vkey_id, connection_id, plugin_install_id)` để khi bật bất kỳ cấp công tắc khẩn cấp nào đều gọi `CancelFunc` hàng loạt dưới `< 1s` kèm lý do kiểm toán).
  - `internal/security/audit/blastradius.go` (truy vấn đối chiếu `request_traces` + `audit_events` theo `plugin_install_id` / `connection_id` / `vkey_id` trong khoảng `[from, to]`).
  - `db/migrations/sqlite/0010_audit_killswitch.sql` (`audit_events` kèm trigger `BEFORE UPDATE / BEFORE DELETE RAISE(ABORT, 'audit_events_immutable')`, `emergency_kill_switches`).
- **Out-of-Scope:** Truyền tin công tắc khẩn cấp toàn cụm đa nút `< 5s` qua `Valkey` (thuộc `TP-IMP-0040`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0704`, `TP-FR-0705`, `TP-NFR-0404`, `TP-NFR-0501`, `TP-BR-0803`, `CT-30`, `AUTH-06`, `OPS-AC-03`, `OPS-AC-04`, `OPS-AC-08`.
- **Dependencies:** `TP-IMP-0009`, `TP-IMP-0011`, `TP-IMP-0022`.
- **Design Refs:** [`threat-model.md`](../../security/threat-model.md) (`TM-13`), [`monitoring-incidents.md`](../../operations/monitoring-incidents.md) (`INC-02`, `INC-03`, `ALT-AUDIT-TAMPER`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-06`).

## 4. API, Wire & Data Contracts
- **Bất biến Giao dịch Kiểm toán (`Transactional Audit Atomicity`):** Mọi thay đổi trạng thái quản trị (tạo/xóa khóa, duyệt plugin, sửa chính sách, bật/tắt kill-switch) phải ghi `audit_events` **trong cùng một giao dịch CSDL (`Tx`)** với thay đổi nghiệp vụ — đảm bảo không bao giờ tồn tại thay đổi cấu hình mà mất dấu vết kiểm toán.
- **Công thức Nối Chuỗi Theo `Org` (`Genesis Hash`):** Dòng đầu tiên (`seq_no = 1`) của mỗi `org_id` có `prev_hash = SHA-256("tanpopo:audit:genesis:v1:" + org_id)`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`CT-30` & `OPS-AC-08` Tamper Detection):** Given 500 bản ghi kiểm toán của `Org A` và `Org B` xen kẽ nhau, When chạy `VerifyAuditChain(Org_A)` và `VerifyAuditChain(Org_B)`, Then cả hai xác nhận hợp lệ `100%`; khi tắt tạm trigger trong bài test để sửa 1 ký tự JSON hoặc xóa dòng `seq_no = 250` trên tệp SQLite thô, `VerifyAuditChain` phát hiện chính xác vị trí dòng bị can thiệp (`seq_no = 250`) và bật cờ `ALT-AUDIT-TAMPER`.
- **`AC-2` (`OPS-AC-03, 04` 4-Level Kill-Switch `< 1s`):** Given 8 luồng SSE đang truyền dở trên 2 Plugin và 2 Dự án khác nhau, When kích hoạt Kill-Switch cấp `ExtensionInstallation` cho `Plugin 1`, Then đúng 4 luồng thuộc `Plugin 1` bị đóng trong `< 1 giây`, còn 4 luồng thuộc `Plugin 2` tiếp tục chạy không gián đoạn.
- **`NEG-1`:** Thử chạy lệnh SQL `DELETE FROM audit_events` hoặc `UPDATE audit_events SET actor_id = 'x'` qua kết nối ứng dụng → bị trigger CSDL chặn cứng ngay lập tức.
- **`NEG-2` (`Transactional Audit Rollback`):** Giả lập lỗi ghi `audit_events` → toàn bộ lệnh tạo `Virtual Key` hoặc bật `Plugin` trong cùng giao dịch bị hoàn tác (`Rollback`), không có thay đổi âm thầm.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/security/audit/... ./internal/security/killswitch/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Vai trò `org_admin` thông thường không thể xóa hay tắt bộ ghi `audit_events`; việc gỡ bỏ trạng thái `Kill-Switch` bắt buộc phải ghi một bản ghi kiểm toán mới kèm lý do giải trình bắt buộc (`reason` không rỗng).
- **Rollback:** Bảng cộng thêm `0010_audit_killswitch.sql`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` (khóa chuẩn băm `JCS` theo `Org` tại `CR-06`), `TP-IMP-0009`, `TP-IMP-0011` và `TP-IMP-0022` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca `CT-30`, `AUTH-06`, `OPS-AC-03, 04, 08` dưới `-race`, vượt qua `REQUIRES_STRONG_REVIEW`.
