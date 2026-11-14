# `TP-IMP-0030` — Giao diện Quan sát, FinOps & Trực chiến: Thác nước Truy vết (`SCR-11`), Ngân sách (`SCR-12`), Kill-Switch/Audit (`SCR-14`) & Vận hành (`SCR-16`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0030` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-06` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0025`, `TP-IMP-0026` — `GAP-01, 12`)* |
| **Quy mô (`Size`)** | `L` (`web/console/src/pages/traces/*`, `finops/*`, `security/*`, `operations/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Chống Stored XSS từ dữ liệu vết/nhật ký `CT-27`, tách bạch 3 nhãn độ chính xác chi phí `UX-GR-05` & giao diện Công tắc Khẩn cấp)* |
| **Chủ sở hữu Kỹ thuật** | `Lead Frontend / SRE UX Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hoàn thiện bộ 4 màn hình quan sát, tài chính và ứng cứu sự cố của `MVP (v0.1)` theo [`screen-specifications.md`](../../ux/screen-specifications.md) (`SPEC-UX-03`):
1. **Trình Khám phá Vết & Thác nước Gỡ lỗi (`SCR-11 Traces`):** Tra cứu theo `request_id` / mã lỗi / mô hình, hiển thị dòng thời gian từng chặng (`Policy -> Route Attempt 1 (503) -> Failover Attempt 2 (200)`), minh bạch trạng thái quyền riêng tư `Metadata-Only (0 prompt bytes stored)` (`UX-AC-09`).
2. **Bảng Ngân sách & Đối soát FinOps (`SCR-12 Budgets & Chargeback`):** Thực thi nghiêm ngặt rào chắn **`UX-GR-05`** — hiển thị tách rời 3 thành phần `EXACT`, `ESTIMATED` và cảnh báo rõ số lượng yêu cầu `UNMETERED_FLAGGED` (không bao giờ cộng gộp mập mờ thành một con số "chi phí chính xác tuyệt đối"), hiển thị phần giữ chỗ đang treo (`Active Hold`) và nút tải báo cáo `CSV/JSON` (`UX-AC-06`).
3. **Trung tâm An ninh, Công tắc Khẩn cấp 4 Cấp & Nhật ký Kiểm toán (`SCR-14 Security & Audit`):** Kích hoạt/gỡ bỏ Kill-Switch 4 cấp với cảnh báo phạm vi ảnh hưởng (`Blast-Radius`), tra cứu nhật ký kiểm toán và nút kiểm chứng tính toàn vẹn chuỗi băm `Verify Hash Chain` trực tiếp (`UX-AC-02`).
4. **Màn hình Sức khỏe Hệ thống, Sao lưu & Nâng cấp (`SCR-16 Operations`):** Hiển thị trạng thái `/readyz`, dung lượng WAL, lịch sử sao lưu, khóa `tanpopo.lock` và kiểm tra tiền nâng cấp (`UX-AC-05`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `web/console/src/pages/traces/TracesExplorerPage.tsx` (`SCR-11`).
  - `web/console/src/pages/finops/FinOpsBudgetsPage.tsx` (`SCR-12`).
  - `web/console/src/pages/security/SecurityAuditAndKillSwitchPage.tsx` (`SCR-14`).
  - `web/console/src/pages/operations/OperationsSettingsPage.tsx` (`SCR-16`).
- **Out-of-Scope:** Thẻ hàng đợi phê duyệt công cụ MCP `destructive` trên `SCR-14` (sẽ được gắn thêm ở `TP-IMP-0038` tại `M3 / v0.2`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0704`, `TP-FR-0705`, `TP-FR-0802`, `TP-FR-0803`, `TP-FR-0902`, `UJ-08`, `SCR-11`, `SCR-12`, `SCR-14`, `SCR-16`, `CT-27`, `UX-GR-05`, `UX-GR-06`, `UX-AC-02`, `UX-AC-05`, `UX-AC-06`, `UX-AC-09`.
- **Dependencies:** `TP-IMP-0025`, `TP-IMP-0026`.
- **Design Refs:** [`user-flows.md`](../../ux/user-flows.md) (`UJ-08`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-11`, `12`, `14`, `16`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-27`).

## 4. API, Wire & Data Contracts
- **Chống Thực thi Mã Độc Qua Vết & Kiểm toán (`CT-27` Stored XSS Prevention):** Mọi chuỗi tên mô hình, thông báo lỗi thượng nguồn, `User-Agent`, nhãn khóa hoặc thân gỡ lỗi hiển thị trên `SCR-11` và `SCR-14` đều được thoát ký tự an toàn 100% — bài kiểm thử tự động bơm chuỗi tải trọng XSS vào vết và xác nhận trình duyệt không thực thi bất kỳ đoạn kịch bản nào.
- **Hiển thị Số Tiền Chuẩn:** Chuyển đổi từ số nguyên `micro_usd` sang chuỗi hiển thị tiền tệ bằng phép chia nguyên/chuỗi thập phân chính xác ở phía giao diện, không dùng phép cộng dấu phẩy động gây sai số hiển thị.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`UX-AC-06` Honest FinOps Accuracy Breakdown):** Given dữ liệu sổ cái gồm cả bút toán `EXACT`, `ESTIMATED` và `UNMETERED_FLAGGED`, When mở `SCR-12`, Then cả 3 nhóm được hiển thị rõ ràng kèm biểu ngữ nhắc quản trị viên cấu hình bảng giá cho các mô hình `UNMETERED_FLAGGED`.
- **`AC-2` (`UX-AC-02` Emergency Kill-Switch UX):** Trên `SCR-14`, quản trị viên tra cứu `Blast-Radius` của một Plugin nghi vấn và kích hoạt công tắc ngắt khẩn cấp trong `<= 3` thao tác có xác nhận rõ ràng.
- **`NEG-1` (`CT-27` Stored XSS Payload in Trace/Audit):** Bơm chuỗi kịch bản độc hại vào trường `error_message` / `user_agent` của bản ghi truy vết → hiển thị thuần chữ vô hại trên `SCR-11`/`SCR-14`.
- **`NEG-2`:** Kiểm định `npx astryx doctor --built dist` đạt `0 fail` (`0` thẻ `div`/`span` thô, dùng `Table` cho bảng vết/kiểm toán lớn có phân trang con trỏ).

## 6. Test Commands & Required Evidence
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Vai trò `billing_viewer` chỉ xem được `SCR-12` (không xem được nội dung gỡ lỗi trên `SCR-11` hay thao tác `Kill-Switch` trên `SCR-14`).
- **Rollback:** Cụm 4 màn hình quan sát/vận hành độc lập.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0025` và `TP-IMP-0026` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca `CT-27`, `UJ-08`, `UX-AC-02, 05, 06, 09`, `astryx doctor --built dist` đạt `0 fail`, vượt qua `REQUIRES_STRONG_REVIEW`.
