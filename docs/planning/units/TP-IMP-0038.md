# `TP-IMP-0038` — Trải nghiệm Kỹ sư & Quản trị `v0.2`: `Dry-Run Route Explain` (`SCR-08`), `Playground` Phiên Ngắn Hạn (`SCR-15`), `MCP Hub` (`SCR-10`) & Hàng đợi Duyệt

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0038` |
| **Milestone / Epic** | `M3` (`v0.2 MCP Governance & Authorized OAuth 2.1` — **Cổng `GATE-M3`**) · `EP-07` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0020`, `TP-IMP-0029`, `TP-IMP-0030`, `TP-IMP-0037` — `GAP-01, 07`)* |
| **Quy mô (`Size`)** | `L` (`internal/admin/http/route_explain.go`, `playground.go`, `web/console/src/pages/mcp/*`, `playground/*`) |
| **Cấp độ Review** | `STANDARD_REVIEW` *(Phiên thử nghiệm ngắn hạn có trần chi phí an toàn `CR-10` & tuân thủ kỷ luật Astryx)* |
| **Chủ sở hữu Kỹ thuật** | `Lead Developer Experience & Frontend Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hoàn thiện bộ công cụ tương tác trực quan trên Web Console và CLI cho nhịp `v0.2` (`TP-FR-0604`, `TP-FR-0904`, `CR-10`) — khép kín **Cổng `GATE-M3`**:
1. **Mô phỏng Giải thích Tuyến Không Gọi Thật (`Dry-Run Route Explain` — `TP-FR-0604`, `SCR-08` & `tanpopo route explain`):** Cho phép kỹ sư nhập mô hình, vùng cư trú, danh sách năng lực yêu cầu (`tools`, `vision`, `context_tokens`) để nhận cây giải thích từng bước 1→6: những `Deployment` nào bị loại (và vì chính sách, thiếu năng lực hay cầu dao đang `OPEN`), đích nào thắng cuộc và giá giữ chỗ ước tính — tốn `0` cuộc gọi mạng ra ngoài.
2. **Bàn Thử nghiệm Tương tác Dùng Khóa Phiên Ngắn Hạn (`Interactive Playground` — `SCR-15`, `CR-10`):** Thay vì bắt người dùng dán lộ `Virtual Key` dài hạn vào trình duyệt hoặc gọi vòng qua cookie quản trị không qua sổ cái, Backend cấp một `Ephemeral Playground Key` ngắn hạn (`TTL <= 15 phút`, trần chi phí cứng thấp, tự hủy khi đăng xuất) đi qua đúng đường nóng `/v1/*` để người dùng thử truyền luồng SSE, so sánh phản hồi và xem vết thực tế ngay bên cạnh.
3. **Giao diện Quản trị `MCP Hub` (`SCR-10`), Kết nối `OAuth 2.1` (`SCR-04`) & Hàng đợi Phê duyệt Con người (`SCR-14`):** Đăng ký máy chủ MCP, cấp quyền từng công cụ `<server>__<tool>`, xem cảnh báo lệch `schema_hash` và bấm `Approve / Reject` các lệnh gọi công cụ `destructive` kèm xem đối số JSON rõ ràng.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/admin/http/route_explain.go` (`POST /admin/v1/routes/explain`) & `internal/cli/route/explain.go` (`tanpopo route explain`).
  - `internal/admin/http/playground.go` (`POST /admin/v1/playground/sessions` — khớp `100%` đường dẫn chuẩn tại `TP-IMP-0002` & `CR-10`, gắn chặt với quyền của người dùng đang đăng nhập).
  - `web/console/src/pages/mcp/McpHubPage.tsx` (`SCR-10`).
  - `web/console/src/pages/playground/PlaygroundPage.tsx` (`SCR-15`).
  - Cập nhật mở rộng `SCR-04` (nút ủy quyền `OAuth 2.1`), `SCR-08` (ngăn kéo `Route Explain`), và `SCR-14` (thẻ `Approval Queue`).
- **Out-of-Scope:** Thẻ quản lý danh mục tác nhân `A2A AgentCard` trên `SCR-10` (sẽ hiển thị ở `v0.3` sau khi `TP-IMP-0042` hoàn tất).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0604`, `TP-FR-0904`, `UJ-03`, `UJ-05`, `UJ-06`, `SCR-04`, `SCR-08`, `SCR-10`, `SCR-14`, `SCR-15`, `UX-AC-09`, `CR-10`.
- **Dependencies:** `TP-IMP-0020`, `TP-IMP-0029`, `TP-IMP-0030`, `TP-IMP-0037`.
- **Design Refs:** [`user-flows.md`](../../ux/user-flows.md) (`UJ-03`, `UJ-05`, `UJ-06`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-10`, `SCR-15`), [`routing-and-policy.md`](../../specifications/routing-and-policy.md) Mục 5, [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-10`).

## 4. API, Wire & Data Contracts
- **Bất biến Không Tác động Phụ Của `Route Explain`:** Gọi `POST /admin/v1/routes/explain` không bao giờ trừ hạn ngạch `RPM/TPM`, không tạo giữ chỗ sổ cái và không gửi bất kỳ gói tin nào lên nhà cung cấp AI.
- **Giới hạn An toàn `Playground`:** Vai trò `FINOPS_VIEWER` không thể gọi `POST /admin/v1/playground/sessions`; mọi cuộc gọi từ `SCR-15` đều được ghi sổ cái bình thường vào dự án được chọn và mặc định vẫn giữ `0 byte` nội dung lưu xuống đĩa trên máy chủ (nội dung hội thoại chỉ nằm tạm trong RAM trình duyệt).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given tuyến phức tạp gồm 4 đích có vùng cư trú, năng lực và trạng thái cầu dao khác nhau, When chạy `tanpopo route explain` hoặc thử trên `SCR-08`, Then kết quả giải thích khớp `100%` với quyết định định tuyến thực tế của `RouteResolver`.
- **`AC-2` (`UX-AC-09` Smooth `100 tok/s` Playground Stream):** Chạy kịch bản E2E cho cả 3 hành trình `UJ-03` (ủy quyền OAuth 2.1 hợp lệ), `UJ-06` (duyệt công cụ MCP bị chặn `destructive` & cảnh báo `schema_hash` thay đổi) và `SCR-15` (`Playground` nhận luồng SSE tốc độ `100 tokens/s` mượt mà không giật khung hình + hủy giữa chừng) đạt `100%`.
- **`NEG-1`:** Khi khóa phiên `Playground` hết hạn sau 15 phút hoặc người dùng đăng xuất, khóa đó bị thu hồi ngay lập tức (`401`).
- **`NEG-2`:** Chạy `npx @astryxdesign/cli doctor --built dist` đạt `0 fail` (`0` thẻ `div`/`span` thô, `Badge` chỉ dùng cho số lượng như số yêu cầu đang chờ duyệt trên `SCR-14`).

## 6. Test Commands & Required Evidence
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`
- Thuần Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/admin/http/... ./internal/cli/route/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/admin/http/... ./internal/cli/route/...`)*
- Tranh chấp dữ liệu (`-race`): `go test -v -race ./internal/admin/http/... ./internal/cli/route/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Bảng xem đối số chờ duyệt trên `SCR-14` hiển thị văn bản JSON thuần an toàn chống XSS và khóa nút `Approve` nếu mã băm công cụ hiện tại bị lệch (`SCHEMA_DRIFT_BLOCKED`).
- **Rollback:** Tắt tính năng `Playground` qua cờ cấu hình `console.playground.enabled = false` cho các môi trường sản xuất khóa chặt.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0020`, `TP-IMP-0029`, `TP-IMP-0030` và `TP-IMP-0037` đã hoàn tất.
- **DoD:** Vượt qua toàn bộ tiêu chí Cổng `GATE-M3` (`v0.2`), `astryx doctor --built dist` đạt `0 fail`, `axe-core` sạch lỗi.
