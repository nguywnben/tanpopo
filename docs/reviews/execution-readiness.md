# Báo cáo Kiểm toán Sẵn sàng Bàn giao Cuối cùng (`Final Delivery Readiness Audit — ASTRA A10`) — Tanpopo

> **Tài liệu Phán quyết Kiểm toán Độc lập (Phase ASTRA A10 — Gate G4 Review & Luna Handoff Assessment)**
> - **Mã báo cáo:** `REV-RDY-01` | **Phiên bản:** `1.0.0-step10` | **Ngày kiểm toán:** `2026-11-14`
> - **Vai trò thực hiện:** Independent Delivery Readiness Reviewer & Principal Architect (kèm 2 luồng phản biện đối kháng ngữ cảnh rỗng `Zero-Context Adversarial Subagents`)
> - **PHÁN QUYẾT BÀN GIAO (`DELIVERY HANDOFF VERDICT`):** **`NO_GO`** *(Chưa mở khóa thực thi mã nguồn cho Luna)*
> - **TRẠNG THÁI `GATE G4`:** **`BLOCKED` / `READY_FOR_REVIEW`** *(0 đơn vị `READY`, `47 / 47` đơn vị `BLOCKED`)*

---

## 1. Tóm tắt Điều hành & Luận cứ Phán quyết `NO_GO` (`Why NO_GO, Not CONDITIONAL_GO`)

Theo quy tắc kiểm toán nghiêm ngặt của **ASTRA A10**:
- **`GO`** chỉ được cấp khi toàn bộ cổng `G1/G2/G3/G4` đã được Product Owner phê duyệt thật và `0` lỗ hổng chặn mở.
- **`CONDITIONAL_GO`** chỉ được cấp cho một tập con đơn vị đã thực sự **`READY`**, hoàn toàn không bị ảnh hưởng bởi các khoảng trống chưa duyệt và **không bao giờ được dùng để tự vượt cổng phê duyệt của con người**.

Qua đối chiếu trực tiếp toàn bộ lịch sử Git (`13` commits từ `0a35b74` đến `4adfc26` trên `origin/main`) và [`docs/governance/decisions.md`](../governance/decisions.md):
1. **Product Owner mới chỉ xác nhận `DEC-001`..`DEC-008` (và `REJ-001`..`004`).** Cả 4 cổng **`Gate G1` (`SD-G1-01`..`05`), `Gate G2` (`TD-G2-01`..`06`, `BA-OQ-001`..`05`), `Gate G3` (`ADR-0001`..`0006` + cấp phép `SPIKE-01`..`04`) và `CR-01`..`10` (từ kiểm toán A08 `FAIL`) đều CHƯA ĐƯỢC PHÊ DUYỆT** (`READY_FOR_REVIEW` / `OPEN`).
2. Ngay cả đơn vị đầu tiên của lộ trình là [`TP-IMP-0001`](../planning/units/TP-IMP-0001.md) cũng phụ thuộc trực tiếp vào phán quyết của Product Owner trên `CR-01..04, 08, 09` và `Gate G1/G2` (`NEG-1`: cấm tự chốt thay Owner). Do đó số lượng đơn vị `READY` hôm nay bằng đúng **`0 / 47`**.
3. **Kết luận bắt buộc:** Trạng thái bàn giao thực thi là **`NO_GO`**. Tuy nhiên, toàn bộ sai lệch nội bộ của tầng kế hoạch A09 phát hiện qua mô phỏng đối kháng ngữ cảnh lạnh đã được **sửa triệt để ngay trong bước A10 này**, giúp bộ khung tài liệu đạt độ sẵn sàng cấu trúc `100%` — chỉ cần Product Owner ký duyệt **bảng quyết định tối thiểu duy nhất ở Mục 7** là có thể chuyển sang `GO` và giao ngay `TP-IMP-0001` cho Luna mà không phải sửa lại kế hoạch.

---

## 2. Xác minh Thực chứng Trạng thái Phê duyệt (`Approval Verification — Zero Inferred Approvals`)

| Cổng / Nhóm Quyết định | Số lượng Mục | Trạng thái Thực tế Trong Kho Mã | Bằng chứng Kiểm chứng | Được Owner Duyệt Thật? |
|---|---|---|---|---|
| **Quyết định nền tảng `DEC-001`..`DEC-008`** | `8 / 8` | `CONFIRMED_BY_OWNER` (`DEC-005..007` kèm `VERIFIED_IN_REPO`) | [`decisions.md`](../governance/decisions.md) Phần A, commits `0a35b74`, `6b73bcd` | **CÓ** *(Duy nhất nhóm này)* |
| **Hướng bị bác bỏ `REJ-001`..`REJ-004`** | `4 / 4` | `REJECTED` | [`decisions.md`](../governance/decisions.md) Phần B | **CÓ** *(Rào chắn âm)* |
| **Gate G1 (`SD-G1-01`..`05`)** | `5 / 5` | `READY_FOR_REVIEW` / `OPEN` (`PROPOSED`) | [`decisions.md`](../governance/decisions.md) Phần C | **CHƯA** |
| **Gate G2 (`BA-OQ-001`..`05`, `TD-G2-01`..`06`)** | `11 / 11` | `READY_FOR_REVIEW` (`DRAFT — UNLOCKED SCOPE`) | [`decisions.md`](../governance/decisions.md) Phần E–F, [`release-scope.md`](../product/release-scope.md) | **CHƯA** |
| **Gate G3 (`ADR-0001`..`0006` + `SPIKE-01`..`04`)** | `6` ADRs + `4` Spikes | `READY_FOR_REVIEW` / `PROPOSED — NO CODE` | [`decisions.md`](../governance/decisions.md) Phần H, [`docs/architecture/adr/*`](../architecture/adr/README.md) | **CHƯA** |
| **Audit Gate A08 (`GAP-01`..`13` & `CR-01`..`10`)** | `13` Gaps / `10` CRs | **`FAIL` *(Blocking Gaps Open)*** | [`integrated-audit.md`](./integrated-audit.md), [`unresolved-gaps.md`](./unresolved-gaps.md) | **CHƯA (`FAIL`)** |
| **Gate G4 (`EXEC-BL-001-DRAFT` & `TP-IMP-0001`..`0047`)** | `47 / 47` Units | **`BLOCKED` / `READY_FOR_REVIEW` (`NO_GO`)** | [`execution-baseline.md`](../governance/execution-baseline.md), [`implementation-index.md`](../planning/implementation-index.md) | **CHƯA** |

---

## 3. Kiểm toán Tính Nhất quán Ngang–Dọc & Nhật ký Khắc phục Triệt để Tầng Kế hoạch Tại A10

### 3.1. Bảng Số đo Kiểm định Tự động Toàn Kho Tài liệu (Hậu Khắc phục A10)

| Phép Kiểm tra Tự động | Kết quả Trước A10 (Trên bản nháp A09 `4adfc26`) | Kết quả Sau Khi Khắc phục Tại A10 (`VERIFIED`) |
|---|---|---|
| **1. Liên kết tương đối Markdown bị đứt (`Broken Relative Links`)** | `0` lỗi / `107` tệp | **`0` lỗi / `109` tệp Markdown** |
| **2. Mã `UNIT_ID` trùng lặp / không ổn định / chu trình phụ thuộc (`DAG Cycles`)** | `0` trùng · `0` chu trình ($u < v$) · `0` dep mồ côi | **`0` trùng · `0` chu trình ($u < v$) · `0` dep mồ côi** (`47/47` khớp 1-1 với Index) |
| **3. Yêu cầu `P0 (Must)` thiếu đơn vị `TP-IMP-xxxx` hoặc thiếu thiết kế/test** | `0 / 54` thiếu unit (`3` FR thiếu một vế `AC`/`CT` theo `GAP-11`, `13` dòng bảng thiếu mã test tường minh) | **`0 / 54` thiếu unit** · `73 / 73` yêu cầu hoạt động trong [`traceability.md`](../requirements/traceability.md) Mục 9 gắn đủ mã kiểm thử chuẩn (`GAP-11` khóa phạm vi vá tại `TP-IMP-0001`) |
| **4. Định danh kiểm thử mồ côi (`Orphaned Verification IDs` trên tổng `113` ID)** | Phát hiện **`6` ID mồ côi `0` tham chiếu** (`UX-AC-08`, `INC-01, 04..07`) + `12+` ID gán nhầm/rơi nửa vế + lệch số thứ tự `GATE-REL-02..06` làm sót Pen-Test hộp xám | **`0 / 113` ID mồ côi** — Đủ `32/32 CT`, `16/16 AUTH`, `6/6 FZ`, `1/1 EXT-ACCEPT`, `8/8 SLO`, `8/8 FMEA`, `12/12 UX-AC`, `12/12 OPS-AC`, `7/7 INC`, `4/4 DR`, và khôi phục chuẩn `7/7 GATE-REL` kèm **Grey-Box Pen-Test** |
| **5. Đơn vị thiếu `AC`/`NEG` hoặc chứa lệnh test xung đột cờ / hệ điều hành** | Phát hiện xung đột cờ `CGO_ENABLED=0` + `-race` trên các đơn vị Go, thiếu cú pháp PowerShell và lệch danh sách `Files Touched` ở `M0` | **`0 / 47` xung đột** — Tách bạch hoàn toàn `CGO_ENABLED=0` và `-race`, bổ sung cú pháp PowerShell + trình chạy thuần Go `scripts/ci/check.go` |
| **6. Lối vào khám phá từ gốc cho Agent ngữ cảnh lạnh (`Cold-Start Root Discovery`)** | `AGENTS.md` chỉ có khối Astryx UI (thiếu đường dẫn sang kế hoạch & cổng quản trị) | **ĐẠT (`PASS`)** — Đã gắn khối điều hướng `TANPOPO-HANDOFF` vào [`AGENTS.md`](../../AGENTS.md) (tự nạp qua cả `GEMINI.md`) dẫn thẳng tới [`execution-baseline.md`](../governance/execution-baseline.md) mà giữ nguyên `100%` khối Astryx (`astryx doctor`: `18 pass, 0 fail`) |

### 3.2. Tóm tắt Các Lỗ hổng Tầng Kế hoạch Đã Được Phát hiện Bởi Phản biện Đối kháng & Đã Vá Xong Trong A10
Thực hiện quy trình `doubt-driven-development`, hai kiểm toán viên độc lập không mang ngữ cảnh hội thoại đã soi toàn bộ `TP-IMP-0001`..`0047` và phát hiện các điểm lệch đã được **sửa dứt điểm ngay trên bộ tệp `docs/planning/*` & `traceability.md` trong bước A10**:
1. **Xóa xung đột cờ `go test` & hệ điều hành (`[F-SYS-01, 02]`):** Tách riêng lệnh kiểm tra thuần Go `CGO_ENABLED=0` khỏi lệnh `-race` (`CGO_ENABLED=1`), bổ sung cú pháp Windows PowerShell và thiết kế `scripts/ci/check.go` đa nền tảng tại `TP-IMP-0005`.
2. **Đóng kín phạm vi tệp của `M0` (`[F-0001..0004]`):** Bổ sung đầy đủ các tệp hợp đồng/đặc tả cần chạm tới vào `In-Scope` của `TP-IMP-0001..0003` (`proto/tanpopo/extension/v1/runtime.proto`, `openapi/*.yaml`, `db/spec-check/*.sql`, `contract-test-matrix.md`, `release-readiness.md`, `backups-recovery.md`...), thêm cạnh chuyển tiếp `SM-01` vào `TP-IMP-0001`, sửa `tanpopoctl` → `tanpopod`/`tanpopo` trong `TP-IMP-0002`, và tách `tools/spikes/go.mod` cô lập riêng trong `TP-IMP-0004` để chạy song song không xung đột với `go.mod` gốc của `TP-IMP-0005`.
3. **Đồng bộ `100%` từ điển định danh, bảng SQL, vai trò RBAC, đường dẫn gói & mã lỗi trên toàn bộ 47 đơn vị:**
   - Khóa `module github.com/nguywnben/tanpopo`, gói `internal/ingress/{http,sse,native,cache,mcp,a2a}`, và đồng bộ mã `EP-00`..`EP-09` khớp `roadmap.md`.
   - Khóa 6 vai trò chuẩn (`OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`), tiền tố `vk_tp_`, tiêu đề `X-CSRF-Token` / `x-tanpopo-request-id`, định dạng `.tpkg` / `.tpop.enc` / `.tpbundle`, cờ `allow_private_ip`, cấm tiêm `Cookie` chiều đi (`TP-IMP-0010`), thẻ năng lực `vault.UnsealCapability` (`TP-IMP-0008`), giao diện chung `extension/port.ProviderRuntime` (`TP-IMP-0010`) và mẫu `.wasm` dựng sẵn trong `internal/testkit/wasmfixtures` (`TP-IMP-0006` — xóa phụ thuộc bắt buộc cài `TinyGo` trên máy chạy test).
   - Khóa tên 26 bảng chuẩn (`config_changelog`, `budget_ledgers`, `usage_settlements`, `usage_rollups_hourly`, `request_traces_meta`, `approval_requests`, `webhook_subscriptions`, `valkey_hold_checkpoints`), chuỗi kiểm toán `SYSTEM` (`org_id IS NULL`), mã lỗi `429 project_budget_exceeded` (`TP-IMP-0021`), `503 no_compliant_deployment_available` (`TP-IMP-0020`), `502 extension_execution_fault` + ngưỡng `> 20` lỗi/60s (`TP-IMP-0013`), băm bộ 5 thành phần `JCS` + mặc định `mutating` (`TP-IMP-0037`), `POST /admin/v1/playground/sessions` (`TP-IMP-0038`), `tanpopo db migrate-engine` (`TP-IMP-0039`) và `tanpopo admin verify-state` (`TP-IMP-0040`).
   - Phân tách trung thực `EXT-SLICE-M1` ở `TP-IMP-0015` và hoàn tất trọn vẹn **10/10 bước `EXT-ACCEPT-01`** tại `TP-IMP-0032` (`GATE-M2`); khôi phục sàn đo tải `2 vCPU / 2 GiB RAM` (`TP-IMP-0043`) và khôi phục chuẩn `GATE-REL-01..07` kèm **Kiểm thử Xâm nhập Hộp Xám 5 Bề mặt (`TP-IMP-0044`, `TP-IMP-0047`)**.

---

## 4. Mô phỏng Thực thi Ngữ cảnh Lạnh Cho Luna Trên Toàn bộ `M0` & `M1` (`TP-IMP-0001` .. `0015`)

Giả lập một phiên Agent thực thi hoàn toàn mới chỉ nhận chuỗi `"Thực thi TP-IMP-xxxx"` (sau khi Product Owner đã mở cổng):

| Đơn vị (`M0` & `M1`) | Đường Tìm Tệp Từ Gốc | Độ Rõ Phạm vi & Hợp đồng | Khả năng Chạy Test Độc lập | Kết quả Mô phỏng Hậu Vá A10 |
|---|---|---|---|---|
| **`TP-IMP-0001` .. `0003`** *(Đóng Spec A08)* | `AGENTS.md` → `execution-baseline.md` → `units/TP-IMP-0001..0003.md` | Liệt kê chính xác từng tệp Markdown, `.proto`, `openapi/*.yaml`, `db/spec-check/*.sql`, từng cạnh `SM-01..09` và công thức `AEAD AAD`. | Kịch bản kiểm định bằng `node -e` chạy ngay trên mọi OS không cần cài thêm binary lạ. | **PASS** *(Chờ quyết định `CR-01..10` của Owner)* |
| **`TP-IMP-0004`** *(Spikes `SPIKE-01..04`)* | Tách riêng trong `tools/spikes/go.mod` | Rõ ngưỡng `< 5ms p95`, `< 1ms/chunk`, `16 MiB` Wasm, `2 vCPU / 2 GiB RAM` và chốt socket loopback nội tại. | Chạy độc lập song song với `0005` không xung đột `go.mod` gốc. | **PASS** *(Chờ giấy phép chạy PoC của Owner)* |
| **`TP-IMP-0005` .. `0007`** *(Monorepo, Testkit, SQLite)* | Rõ `module github.com/nguywnben/tanpopo`, `modernc.org/sqlite`, `idgen` Base32 `<= 32` ký tự | Đầy đủ `.gitignore`, `CONSTRAINTS.md`, `scripts/ci/check.go`, `netguard` chặn cả `DefaultResolver`, và `wasmfixtures` dựng sẵn. | Chạy sạch trên cả Windows PowerShell và Linux CI (`CGO_ENABLED=0` tách khỏi `-race`). | **PASS** *(Sau khi xong `M0`)* |
| **`TP-IMP-0008` .. `0011`** *(Vault, IAM, Broker, `.tpkg`)* | Rõ gói `security/{vault,egress,broker}`, `iam/*`, `extension/{port,installer}` | Khóa `UnsealCapability`, 6 vai trò RBAC, `vk_tp_`, cấm `Cookie` chiều đi, che khóa dội lại trong lỗi `401` (`CT-16`), chuẩn `.tpkg` & `SM-01`. | Đủ ca đối kháng `AUTH-02..16`, `CT-01..05, 14..26`, `FZ-02`. | **PASS** *(Sau khi xong `0004, 0007`)* |
| **`TP-IMP-0012` .. `0015`** *(Tier-1, Tier-2 Wasm, SDK, SSE Slice)* | Cùng triển khai `extension/port.ProviderRuntime`; Data Plane tại `internal/ingress/*` | Không đụng độ khi chạy song song `0012 \|\| 0013`; mã thoát `0/2/14/22` chuẩn `SPEC-OPS-04`; quy tắc chọn đích mặc định `M1` rõ ràng. | Chạy `EXT-SLICE-M1`, hủy luồng `< 500ms`, `goleak`, `FZ-01, 03, 05` không cần cài `TinyGo` hay mạng ngoài. | **PASS** *(Khép kín `GATE-M1`)* |

---

## 5. Xác minh Các Giả định Dự án & Hiện trạng Kho Mã Nguồn (`Project & Repo Assumptions Audit`)

1. **Bộ Kỹ năng Agent (`25/25` Verified):** Thư mục [`.agents/skills/`](../../.agents/skills) có đúng `25` kỹ năng khớp `100%` với [`skills-lock.json`](../../skills-lock.json), kèm `4` subagent personas (`.agents/agents/`) và `7` bảng kiểm tham chiếu (`.agents/references/`).
2. **Thiết lập Astryx Design System (`VERIFIED_IN_REPO`):** [`package.json`](../../package.json), [`astryx-themes.js`](../../astryx-themes.js) (`defaultThemeSlug = "neutral"`), [`src/App.tsx`](../../src/App.tsx) và [`src/main.tsx`](../../src/main.tsx) giữ nguyên vẹn `100%` chưa bị sửa đổi; lệnh `cmd /c npx @astryxdesign/cli doctor` đạt **`18 pass, 0 fail, 3 warn`**; các điểm lệch `ISS-001`..`005` đã được gán đơn vị xử lý rõ ràng tại `TP-IMP-0005` và `TP-IMP-0026`.
3. **Quét Bí mật & Thông tin Nhạy cảm (`0` Secrets Committed):** Đã quét toàn bộ cây thư mục làm việc và toàn bộ `13` bản ghi lịch sử `git log -p`: **`0` tệp `.env*`, `0` khóa riêng tư `.pem`/`.key`, `0` API token thật**. Đơn vị `TP-IMP-0005` đã được bổ sung nhiệm vụ cứng hóa [`.gitignore`](../../.gitignore) ngay ở commit code đầu tiên.
4. **Môi trường Đích, Lệnh Khởi chạy, Nhánh/Worktree, CI & Dữ liệu Hạt giống:** Đã quy định đầy đủ, tường minh đa hệ điều hành tại [`execution-baseline.md`](../governance/execution-baseline.md) Mục 2 & 4, [`dependency-graph.md`](../planning/dependency-graph.md) Mục 2–3, [`roadmap.md`](../planning/roadmap.md) Mục 3 và [`test-strategy.md`](../quality/test-strategy.md) Mục 3–5.

---

## 6. Kiểm định 10 Trụ cột Kiến trúc & Sản phẩm Cốt lõi của Tanpopo

| Trụ cột Cốt lõi | Đánh giá Cấu trúc Sau A10 | Trạng thái Mở khóa Thực thi |
|---|---|---|
| **1. Tách biệt Official vs Community Plugin & Ranh giới Pháp lý `DEC-004`** | **ĐẠT (`PASS`)** — Khóa regex `^@tanpopo(-[a-z0-9]+)?/`, 3 chiều tin cậy độc lập trên UI (`UX-GR-04`), cấm tiêm `Cookie` và `CHECK (billing_mode IN ('COMMERCIAL_API', 'SELF_HOSTED_INFRA'))`. | Chờ Owner chốt `CR-01, CR-03` |
| **2. Cài đặt Provider Adapter Độc lập Không Sửa/Biên dịch Lại Core** | **ĐẠT (`PASS`)** — Chứng minh sớm ngay ở `M1` (`TP-IMP-0015` `EXT-SLICE-M1`) và khóa đủ 10 bước bất biến `PID` + `sha256sum(tanpopo)` ở `M2` (`TP-IMP-0032` `EXT-ACCEPT-01`). | Chờ Owner mở cổng `G1–G4` |
| **3. Ranh giới Xác thực & Token (`Write-Only Vault`, `RFC 8707`, `Single-Flight`)** | **ĐẠT (`PASS`)** — Phong bì 3 tầng `AEAD AAD`, `UnsealCapability` độc quyền cho `BlindBroker`, che khóa dội ngược (`CT-16`), khóa đối tượng nhận `RFC 8707` (`AUTH-16`) và dừng retry `invalid_grant` (`AUTH-10`). | Chờ Owner duyệt `G3` & `CR-05, 06` |
| **4. Cô lập Plugin Sandbox (`Declarative` / `wazero <= 16 MiB` / `mTLS Sidecar`)** | **ĐẠT (`PASS`)** — `0` mã ngoài chạy chung tiến trình (`ADR-0002`), cắt CPU `<= 5ms/chunk`, tự cách ly `> 20` lỗi/60s (`FMEA-05`) và truyền tệp lớn qua `blob_ref` ngoài RAM Wasm (`CR-05`). | Chờ Owner duyệt `ADR-0002`, `CR-02, 05` & cấp phép `SPIKE-02` |
| **5. Tách biệt Mặt phẳng `MCP` & `A2A` (`DEC-003`)** | **ĐẠT (`PASS`)** — Tách điểm cuối `/mcp` và `/a2a/*`, ghim `schema_hash` JCS (`CT-21`), `0` retry công cụ tác động phụ (`FMEA-02`), duyệt con người bộ 5 `JCS` chuẩn JSON-RPC (`CR-08`), tách `SM-09` và trần `hop_count <= 5` (`CT-23`). | Chờ Owner duyệt `TD-G2-01, 05` & `CR-08` |
| **6. Định tuyến Nhận thức Năng lực 6 Bước & Cư trú Dữ liệu Cứng** | **ĐẠT (`PASS`)** — Chặn cắt tham số ngầm (`400 capability_not_supported`), cấm vượt rào `Data Residency` kể cả khi mọi đích trong vùng sập (`503 no_compliant_deployment_available` — `CT-08`), cấm fallback sau byte SSE đầu tiên (`FMEA-01`). | Chờ Owner duyệt `TD-G2-02` |
| **7. Sổ cái Ngân sách Hai Pha Số nguyên `Micro-USD` & Độ chính xác Trung thực** | **ĐẠT (`PASS`)** — `0` số thực dấu phẩy động, chặn đua song song `429 project_budget_exceeded` (`CT-29`), tách bạch `EXACT / ESTIMATED / UNMETERED_FLAGGED` (`FMEA-04`, `UX-GR-05`) và đồng bộ `Valkey {org_id}` (`CR-07`). | Chờ Owner duyệt `ADR-0005` & `CR-06, 07` |
| **8. Vận hành Tự chủ, Sao lưu `.tpop.enc`, `Crypto-Shredding < 60s` & `Air-Gap`** | **ĐẠT (`PASS`)** — Một nhị phân `tanpopo` nhúng SPA tĩnh (phông `Figtree` nội bộ `0` gọi ngoài — `UX-AC-10`), `doctor`, `support-bundle` `0` lộ khóa, diễn tập `DR-01..04` (`RTO < 15m`) và `Air-Gap Bundle` (`CT-31`). | Chờ Owner duyệt `ADR-0001, 0003, 0004` & `CR-09, 10` |
| **9. Nhật ký Kiểm toán Chuỗi Băm `JCS` (`Per-Org + SYSTEM`) & Công tắc Khẩn cấp 4 Cấp** | **ĐẠT (`PASS`)** — Giao dịch nguyên tử, trigger cấm `UPDATE/DELETE`, chuỗi riêng từng `Org` + `SYSTEM` tương thích `FORCE RLS` (`CR-06`), thu hồi `< 1s` đơn nút / `< 5s` cụm và ngắt nút mù `> 10s` (`FMEA-07`). | Chờ Owner duyệt `ADR-0006` & `CR-06` |
| **10. Cổng Xuất xưởng `GATE-REL-01..07` & Chữ ký Con người Bắt buộc** | **ĐẠT (`PASS`)** — Đủ 6 cổng kỹ thuật tự động (bao gồm Fuzzing `>= 4h`, Pen-Test hộp xám 5 bề mặt, quay lui Canary `< 60s`) và cổng cuối `GATE-REL-07` (`TP-IMP-0047`) cấm tuyệt đối AI tự phát hành. | Khóa cứng xuyên suốt vòng đời dự án |

---

## 7. Danh sách Duy Nhất Các Quyết định Cần Product Owner Phê duyệt Để Mở Khóa `Gate G4` (`Actionable Owner Sign-Off Sheet`)

Product Owner **không cần** đọc lại từ đầu cả 109 tệp tài liệu — chỉ cần phản hồi xác nhận (hoặc chọn phương án khác nếu muốn đổi) trên **5 dòng quyết định tổng hợp duy nhất** dưới đây để chuyển trạng thái từ `NO_GO` sang `GO` và mở khóa ngay `TP-IMP-0001`:

1. **Duyệt `Gate G1` (`SD-G1-01`..`05` tại [`decisions.md`](../governance/decisions.md) Phần C):** Xác nhận Option 1 cho cả 5 mục (mũi nhọn Dev/Platform Teams chạy đơn nút → mở rộng cụm; Sandbox lai + Blind Broker; phân kỳ 3 nhịp `v0.1 -> v0.2 -> v0.3 -> v1.0`; kiểm duyệt chặt kênh chính thức theo `DEC-004`; thuần giấy phép **Apache-2.0** toàn dự án).
2. **Duyệt `Gate G2` (`BA-OQ-001`..`05` & `TD-G2-01`..`06` tại [`decisions.md`](../governance/decisions.md) Phần E–F):** Xác nhận các đánh đổi phạm vi (1 `Org` mặc định ở `0.x` → Multi-Org GA ở `v1.0`, `Negative Balance Carryover`, 2 chế độ thu hồi `Virtual Key`, duyệt MCP chế độ kép, cửa sổ tương thích `N-1` / `12 tháng`).
3. **Duyệt `Gate G3` (`ADR-0001`..`0006` & cấp phép chạy `SPIKE-01`..`04` tại [`decisions.md`](../governance/decisions.md) Phần H):** Phê chuẩn stack **Go `1.24+` (`CGO_ENABLED=0`) + `wazero` + `SQLite WAL / PostgreSQL 16 + Valkey` + `Vite/StyleX/Astryx neutral`** và cho phép chạy mã đo kiểm cô lập trong `tools/spikes/` ở `TP-IMP-0004`.
4. **Duyệt `CR-01`..`CR-10` (tại [`unresolved-gaps.md`](./unresolved-gaps.md)):** Cho phép thực thi 3 đơn vị đóng đặc tả `TP-IMP-0001`..`0003` theo đúng các phương án khuyến nghị nhằm đưa `Audit A08` từ `FAIL` sang `PASS`.
5. **Duyệt `Gate G4` (`EXEC-BL-001` tại [`execution-baseline.md`](../governance/execution-baseline.md)):** Mở khóa bàn giao tuần tự từ **`TP-IMP-0001`** cho Agent thực thi ("Luna") và xác nhận quy ước commit đơn vị kèm mã tham chiếu `TP-IMP-xxxx`.
