# `TP-IMP-0042` — Động cơ `Tier-3 Isolated Sidecar` (`ConnectRPC` UDS/`mTLS`) & Mặt phẳng Liên Tác nhân `A2A` (`AgentCard`, `SM-09`, Trần Ngân sách Con & `hop <= 5`)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0042` |
| **Milestone / Epic** | `M4` (`v0.3 Clustered HA, Enterprise Security & A2A` — **Cổng `GATE-M4`**) · `EP-02` / `EP-04` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0013`, `TP-IMP-0021`, `TP-IMP-0039` — `GAP-01, 08, 09`)* |
| **Quy mô (`Size`)** | `L` (`internal/extension/sidecar/*`, `internal/a2a/*`, `db/migrations/postgres/0002_a2a_sidecar.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Ranh giới tiến trình phụ xác thực hai chiều `mTLS`, chống vòng lặp gọi đệ quy vô tận giữa các Agent `hop_count <= 5` & chống leo thang quyền qua Agent trung gian)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Platform & Agent Systems Architect` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hoàn thiện mảnh ghép kiến trúc cuối cùng của nhịp `v0.3` — khép kín **Cổng `GATE-M4`**:
1. **Động cơ Thực thi Tầng 3 `Isolated Sidecar` (`ADR-0002`, `CR-02` — `TP-FR-0308`):** Dành cho các bộ lọc bảo mật doanh nghiệp nặng (như nhận diện PII bằng mô hình cục bộ) không thể biên dịch sang Wasm: giao tiếp qua `ConnectRPC / gRPC` trên ổ cắm miền Unix có phân quyền chặt (`0600`) hoặc `mTLS` xoay vòng chứng chỉ ngắn hạn (`AUTH-08`), vẫn giữ nguyên bất biến **Sidecar không bao giờ nhận toàn bộ kho khóa của Host**.
2. **Mặt phẳng Kết nối Liên Tác nhân Độc lập `A2A Plane` (`TP-FR-0405`, `TP-FR-0406`, `DEC-003`, `CR-08`):**
   - Khám phá danh thiếp tác nhân đã xác thực (`/.well-known/agent.json` / danh mục `/a2a/agents`) và điều phối vòng đời tác vụ bất đồng bộ theo máy trạng thái riêng **`SM-09`** (`SUBMITTED -> WORKING <-> INPUT_REQUIRED -> COMPLETED | FAILED | CANCELED` — tách biệt hoàn toàn khỏi `SM-06` của MCP theo `CR-08`).
   - **Trần Quyền & Trần Ngân sách Con Thu hẹp Đơn điệu (`Privilege & Sub-Budget Ceiling` — `TP-BR-0604`, `CT-23`):** Khi Tác nhân A ủy quyền tác vụ con cho Tác nhân B, mã thông báo ủy quyền sinh ra bị thu hẹp bằng phép giao $P_{\text{child}} = P_{\text{caller}} \cap P_{\text{agent\_b}}$, ngân sách con không thể vượt phần dư của tác vụ cha, và bộ đếm bước nhảy `X-Tanpopo-A2A-Hop-Count` bị chặn cứng ở **`hop_count <= 5`** (cộng phát hiện chu trình đồ thị `call_chain_ids`) để dập tắt hoàn toàn bão đệ quy đốt tiền giữa các Agent!

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/extension/sidecar/client.go` & `mtls_creds.go` (trình khách `ExtensionRuntimeService` Protobuf đã chốt ở `TP-IMP-0002`, xác thực chứng chỉ hai chiều `mTLS`, ngắt mạch riêng nếu Sidecar sập hoặc quá hạn).
  - `internal/a2a/registry.go` & `task_coordinator.go` (điểm cuối `/a2a/*` JSON-RPC + SSE cập nhật tiến trình tác vụ `SM-09`, kiểm tra `hop_count <= 5`, chống chu trình `A -> B -> A` và phân bổ trần ngân sách con từ `finops/ledger`).
  - Bổ sung thẻ hiển thị danh mục `A2A Agents & Active Tasks` vào màn hình `SCR-10` trên `web/console/`.
- **Out-of-Scope:** Giao thức tự thương lượng kinh tế đa tổ chức bên ngoài (ngoài phạm vi `v1.0`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0405`, `TP-FR-0406`, `DEC-003`, `TP-BR-0604`, `SM-09`, `CR-02`, `CR-08`, `CT-23`, `AUTH-08`.
- **Dependencies:** `TP-IMP-0013`, `TP-IMP-0021`, `TP-IMP-0039`.
- **Design Refs:** [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`mcp-a2a-boundaries.md`](../../specifications/mcp-a2a-boundaries.md) (`SPEC-EXT-05` Mục 6), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-23`, `AUTH-08`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-02`, `CR-08`).

## 4. API, Wire & Data Contracts
- **Bất biến Không Khuếch đại Quyền Qua Tác nhân (`No Confused-Deputy Escalation` — `CT-23`):** Một người dùng/khóa ảo chỉ có quyền gọi mô hình `gpt-4o-mini` không thể nhờ một `A2A Agent` nội bộ gọi hộ mô hình cấm hoặc công cụ MCP mà chính người gọi gốc không có quyền (trừ khi chính sách ủy quyền dịch vụ được `security_reviewer` ký duyệt tường minh kèm trần ngân sách riêng).
- **Chống Vòng lặp Vô tận (`Anti-Loop Invariant`):** Nếu `hop_count > 5` hoặc `target_agent_id` đã xuất hiện trong mảng `call_chain` của cùng vết phân tán → từ chối ngay lập tức `4loop / 422 a2a_recursion_or_hop_limit_exceeded`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`AUTH-08` Mutual-TLS Sidecar Isolation):** Sidecar hợp lệ có chứng chỉ do CA nội bộ của phiên Tanpopo cấp kết nối và xử lý hook thành công; khi Sidecar sập đột ngột (`SIGKILL`), cầu dao ngắt mạch cô lập Sidecar đó mà tiến trình Core không bị ảnh hưởng.
- **`AC-2` (`SM-09` Full Async A2A Task Lifecycle):** Thực thi trọn vẹn vòng đời tác vụ liên tác nhân `SUBMITTED -> WORKING -> INPUT_REQUIRED -> WORKING -> COMPLETED` qua luồng sự kiện SSE và quyết toán đúng phần chi phí vào ngân sách con đã cấp.
- **`NEG-1` (`CT-23` Infinite Agent Ping-Pong Loop & Hop > 5):** Cấu hình `Agent A` gọi `Agent B` và `Agent B` gọi ngược `Agent A` (hoặc chuỗi `A1 -> A2 -> ... -> A6`) → bị chặn đứng ngay ở bước lặp đầu tiên / bước thứ 6, tiêu tốn `0` token dư thừa.
- **`NEG-2` (`AUTH-08` Unauthenticated / Expired Cert Sidecar):** Tiến trình giả mạo cố kết nối vào cổng RPC Sidecar mà không có chứng chỉ `mTLS` hợp lệ → bị từ chối bắt tay TLS ngay lập tức.

## 6. Test Commands & Required Evidence
- `TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/extension/sidecar/... ./internal/a2a/...`
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Cả `Tier-3 Sidecar` và `A2A Plane` mặc định **tắt (`disabled`)** trong cấu hình khởi đầu; chỉ bật khi quản trị viên kích hoạt tường minh.
- **Rollback:** Tắt cờ `runtime.sidecar.enabled` và `a2a.enabled` cô lập hoàn toàn bề mặt này.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` & `TP-IMP-0002` (khóa `TP-FR-0308`, `SM-09` & `runtime.proto`), `TP-IMP-0013`, `TP-IMP-0021` và `TP-IMP-0039` đã hoàn tất.
- **DoD:** Đạt toàn bộ tiêu chí Cổng `GATE-M4` (`v0.3`), xanh ca `CT-23` & `AUTH-08` dưới `-race`, vượt qua `REQUIRES_STRONG_REVIEW`.
