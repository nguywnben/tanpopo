# Hồ sơ Cơ sở Thực thi & Hợp đồng Bàn giao Cho Luna (`Execution Baseline & Agent Handoff Contract`) — Tanpopo

> **TÀI LIỆU ĐIỂM VÀO BẮT BUỘC CHO MỌI PHIÊN THỰC THI MỚI (`COLD-START ENTRYPOINT — ZERO CHAT MEMORY REQUIRED`)**
> - **Baseline ID:** `EXEC-BL-001-DRAFT`
> - **Phiên bản:** `0.1.0-step10` | **Ngày thiết lập:** `2026-11-14` | **Commit tham chiếu trước A10:** `4adfc26`
> - **TRẠNG THÁI BÀN GIAO HIỆN TẠI (`HANDOFF_VERDICT`):** **`NO_GO`** *(Chưa mở khóa thực thi mã nguồn!)*
> - **TRẠNG THÁI CỔNG `GATE G4`:** **`BLOCKED` / `READY_FOR_REVIEW`** *(Chờ Product Owner phê duyệt tường minh `Gate G1`, `Gate G2`, `Gate G3` và `CR-01`..`CR-10` tại [`decisions.md`](./decisions.md)).*
> - **ĐƠN VỊ SẴN SÀNG (`READY_UNITS`):** **`NONE` (`0 / 47` `READY`, `47 / 47` `BLOCKED`)**

---

## 0. Quy tắc Chặn Cứng Đầu Phiên Dành Cho Agent Thực thi ("Luna")

Nếu bạn là một AI Coding Agent (ví dụ: **Luna**) vừa khởi động phiên mới với **không gian nhớ hội thoại trống (`Zero Prior Chat Context`)** và nhận lệnh kiểu *"Hãy thực thi `TP-IMP-xxxx`"*, bạn **BẮT BUỘC** thực hiện đúng 4 bước kiểm tra cổng sau trước khi sửa bất kỳ tệp nào:

1. **Kiểm tra `HANDOFF_VERDICT` ngay đầu tệp này và bảng cổng tại [`docs/governance/decisions.md`](./decisions.md):**
   - Nếu `HANDOFF_VERDICT` vẫn là **`NO_GO`** hoặc `Gate G4` chưa được Product Owner đánh dấu **`APPROVED`**: **DỪNG NGAY LẬP TỨC (`ABORT SAFELY`)**, báo cáo lại cho người gọi rằng đơn vị đang `BLOCKED` bởi cổng phê duyệt con người và liệt kê đúng mục chờ duyệt ở Mục 2.2 bên dưới. **Tuyệt đối không tự suy diễn sự tồn tại của tài liệu là đã được duyệt!**
2. **Kiểm tra trạng thái của chính `TP-IMP-xxxx` và các phụ thuộc đầu vào (`Dependencies`) tại [`docs/planning/implementation-index.md`](../planning/implementation-index.md):**
   - Chỉ thực thi khi `TP-IMP-xxxx` ở trạng thái **`READY`** và `100%` các đơn vị trong dòng `Dependencies` của tài liệu [`docs/planning/units/TP-IMP-xxxx.md`](../planning/units/TP-IMP-0001.md) đã hoàn tất (`DONE`).
3. **Chỉ đọc ngữ cảnh có chọn lọc theo đúng thứ tự ưu tiên (tránh tràn cửa sổ ngữ cảnh):**
   - (a) [`AGENTS.md`](../../AGENTS.md) → (b) tệp `execution-baseline.md` này → (c) tệp đặc tả đơn vị [`docs/planning/units/TP-IMP-xxxx.md`](../planning/units/TP-IMP-0001.md) → (d) **chỉ** các mục đặc tả được dẫn chiếu trong Mục 3 (`Design Refs`) của đơn vị đó.
4. **Cấm vượt ranh giới phát hành:** Không một Agent nào được phép chạy `git tag`, `git push --tags`, `gh release create`, `docker push` lên kho công khai hay `npm publish` nếu không có chữ ký xác nhận trực tiếp của Product Owner tại [`TP-IMP-0047`](../planning/units/TP-IMP-0047.md) (`GATE-REL-07`).

---

## 1. Các Quyết định Đã Được Product Owner Xác nhận Thật (`CONFIRMED_BY_OWNER`) vs Đang Chờ Duyệt

### 1.1. Tám Quyết định Đã Khóa Cứng (`DEC-001` .. `DEC-008`) & Bốn Hướng Bác bỏ (`REJ-001` .. `REJ-004`)
Chỉ các quyết định dưới đây đã có hiệu lực thật trong kho mã nguồn ([`decisions.md`](./decisions.md)):
- **`DEC-001`:** Tanpopo là **AI Gateway mã nguồn mở, ưu tiên self-hosted, extension-first, dùng được thực tế và có thể mở rộng quy mô** (Unified API, routing, policies, FinOps, MCP, A2A, Web Console, CLI).
- **`DEC-002`:** Người dùng cài đặt độc lập **Provider Adapters và Extensions** từ Marketplace, Git, URL/gói `.tpkg` hoặc Private Registry **mà không cần biên dịch lại hay phát hành lại Tanpopo Core**.
- **`DEC-003`:** **Provider, Plugin, MCP và A2A là 4 mặt phẳng khái niệm độc lập** — cấm gộp chung bảng dữ liệu, giao thức hay mô hình phân quyền (`REJ-004`).
- **`DEC-004` (`Ranh giới Pháp lý Tuyệt đối`):** Official Adapters chỉ dùng API thương mại / luồng xác thực được nhà cung cấp cấp phép chính thức; **tuyệt đối không phát triển, không hướng dẫn và không dùng hộp thoại "miễn trừ rủi ro" để biến gói thuê bao tiêu dùng cá nhân (`ChatGPT Plus/Pro`, `Claude Pro/Max`, `Codex`, `Antigravity`...) thành cổng API dùng chung (`REJ-003`)**.
- **`DEC-005`:** Kho mã chính thức `https://github.com/nguywnben/tanpopo` (nhánh `main`), Git author `Nguyễn Công Ben <nben940665@gmail.com>`.
- **`DEC-006`:** Cài đặt và sử dụng bộ 25 kỹ năng kỹ thuật trong [`.agents/skills/`](../../.agents/skills) (`skills-lock.json`).
- **`DEC-007`:** Giao diện Web Console sử dụng `@astryxdesign/core@^0.7.13` + `@stylexjs/stylex@^0.19.1` với theme mặc định **`neutral`** ([`astryx-themes.js`](../../astryx-themes.js)) — **cấm chạy `astryx theme eject`, cấm thẻ `div`/`span` thô, `Badge` chỉ dùng cho con số đếm**.
- **`DEC-008`:** Kỷ luật atomic Git commit theo chuẩn Conventional Commits `<type>: step <NN> - <mô tả>` (hoặc `<type>(<module>): step <NN> - <mô tả>`); không dùng tiền tố `a` trước số bước và không force-push trên `main`.

### 1.2. Gói Quyết định Duy Nhất Đang Chờ Product Owner Ký Duyệt Để Mở Khóa (`NO_GO` → `GO`)
1. **`Gate G1`:** Chốt `SD-G1-01`..`05` tại [`decisions.md`](./decisions.md) Phần C.
2. **`Gate G2`:** Chốt `BA-OQ-001`..`05` & `TD-G2-01`..`06` tại [`decisions.md`](./decisions.md) Phần E–F.
3. **`Gate G3`:** Phê duyệt `ADR-0001`..`0006` và cấp phép chạy 4 thử nghiệm ngoại tuyến `SPIKE-01`..`04` tại [`decisions.md`](./decisions.md) Phần H & [`release-readiness.md`](../quality/release-readiness.md) Mục 4.
4. **`Audit A08 Change Requests`:** Chốt `CR-01`..`CR-10` tại [`unresolved-gaps.md`](../reviews/unresolved-gaps.md) để mở khóa `TP-IMP-0001`..`0003`.
5. **`Gate G4`:** Phê duyệt khóa Baseline thực thi này (`EXEC-BL-001-DRAFT` → `EXEC-BL-001-APPROVED`) và xác nhận định dạng hậu tố `[TP-IMP-xxxx]` trong thông điệp commit thực thi.

---

## 2. Từ điển Quy ước Kỹ thuật Bất biến Cho Luna (`Canonical Zero-Guesswork Dictionary`)

Khi các cổng ở Mục 1.2 được duyệt theo phương án đề xuất (`ADR-0001`..`0006` + `CR-01`..`10`), mọi đơn vị `TP-IMP-0001`..`0047` **bắt buộc** dùng thống nhất bảng hằng số chuẩn dưới đây (không bao giờ tự đặt tên khác):

| Hạng mục | Giá trị Chuẩn Duy Nhất (`Single Canonical Value`) | Nghiêm cấm Dùng Tên Lạc Chuẩn Cũ |
|---|---|---|
| **Go Module Gốc (`go.mod`)** | `module github.com/nguywnben/tanpopo` (`Go >= 1.24`) | `module tanpopo`, `tanpopo.io/...` |
| **Mô-đun Thử nghiệm Spikes (`M0`)** | `tools/spikes/go.mod` (`module github.com/nguywnben/tanpopo-spikes`) | Trộn chung vào `go.mod` gốc hoặc `internal/` |
| **Tên Nhị phân Duy nhất** | `tanpopo` (`tanpopo serve --role=all\|data-plane\|control-plane`) | `tanpopod`, `tanpopoctl` |
| **Cấu trúc Gói Đường Nóng (`Ingress`)** | `internal/ingress/{http,sse,native,idempotency,blob,cache,mcp,a2a}` | `internal/gateway/*`, `internal/mcp/*`, `internal/a2a/*` |
| **Định dạng Tệp Đóng gói** | Plugin: **`.tpkg`** · Sao lưu: **`.tpop.enc`** · Mạng kín: **`.tpbundle`** | `.tppkg`, `.tpbak` |
| **6 Vai trò RBAC Chuẩn (`CR-04`)** | `OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER` | `org_admin`, `project_admin`, `billing_viewer`, `instance_super_admin` |
| **Định dạng Khóa Ảo (`Virtual Key`)** | `vk_tp_<env>_<32_base62>` · Băm `SHA-256(rawKey)` thuần trên `active_key_hash` & `previous_key_hash` | `tpk_live_...`, băm nối muối chuỗi tùy ý |
| **Định danh Khóa chính (`UUIDv7`)** | `<prefix>_<26_chars_crockford_base32>` (`<= 32` ký tự trong `VARCHAR(40)`) | `prefix_` + 36 ký tự hex làm tràn `41` ký tự |
| **Tiêu đề Bảo mật & Truy vết HTTP** | CSRF: `X-CSRF-Token` · Request ID: `x-tanpopo-request-id` | `X-Tanpopo-CSRF`, `X-Request-ID` |
| **Mã Lỗi & HTTP Status Trọng yếu** | Hết ngân sách: **`429 project_budget_exceeded`** · Cư trú/Đích sập: **`503 no_compliant_deployment_available`** · Lệch phương ngữ: **`400 dialect_mismatch`** · Lỗi Wasm: **`502 extension_execution_fault`** · A2A lặp/quá chặng: **`400 delegation_depth_exceeded`** · Bootstrap gọi lại: **`410 bootstrap_already_completed`** | `402 budget_exceeded`, `503 all_compliant_deployments_unavailable`, `502 plugin_execution_trapped` |
| **Bảng CSDL Chuẩn (`26` Bảng)** | Dùng đúng `config_changelog`, `budget_ledgers`, `budget_reservations`, `usage_settlements`, `usage_rollups_hourly`, `request_traces_meta`, `approval_requests`, `webhook_subscriptions`, `valkey_hold_checkpoints` | `config_revisions`, `budgets`, `usage_ledger_entries`, `request_traces`, `mcp_approval_requests`, `webhook_endpoints` |
| **Mã Thoát `tanpopo` CLI (`SPEC-OPS-04`)** | `0` (OK), `2` (Validation/Args), `10` (Network/Timeout), `14` (Auth/Permission/Canary Leak), `22` (Contract/Hash/Integrity Fail), `30` (Internal), `31` (Dirty Migration) | Tự đặt mã `3`, `4` không khớp `developer-experience.md` |
| **Chạy Kiểm thử Go Đa Hệ điều hành** | **Tách riêng 2 lệnh** (vì `-race` cần `CGO_ENABLED=1`):<br/>1. Thuần Go: `CGO_ENABLED=0 go test ./...` *(PowerShell: `$env:CGO_ENABLED='0'; go test ./...`)*<br/>2. Đua luồng: `go test -race ./...` *(hoặc `go run ./scripts/ci/check.go`)* | Gộp `CGO_ENABLED=0` và `-race` trên cùng một lệnh (`go: -race requires cgo`) |

---

## 3. Thứ tự Ưu tiên Tài liệu Chân lý (`Source-of-Truth Hierarchy`)

Khi thực thi một đơn vị `TP-IMP-xxxx`, nếu phát hiện bất kỳ điểm chưa rõ nào, Luna tra cứu theo thứ tự thẩm quyền giảm dần sau (và nếu có mâu thuẫn thật sự, dừng lại mở `CR` theo Mục 5 chứ **tuyệt đối không tự đoán**):
1. **Cấp 1 (Luật tối cao):** [`AGENTS.md`](../../AGENTS.md) + [`docs/governance/decisions.md`](./decisions.md) (`DEC-001`..`008` & các quyết định đã `APPROVED`) + tệp `execution-baseline.md` này.
2. **Cấp 2 (Hợp đồng máy đọc được — sinh ở `M0`):** `proto/tanpopo/extension/v1/runtime.proto`, `openapi/{dataplane-v1,admin-v1}.yaml`, `db/spec-check/{sqlite,postgres}_schema.sql`, `CONSTRAINTS.md`.
3. **Cấp 3 (Đặc tả yêu cầu & bảo mật):** [`srs.md`](../requirements/srs.md), [`acceptance-criteria.md`](../requirements/acceptance-criteria.md), [`docs/security/*`](../security/threat-model.md), [`docs/quality/*`](../quality/contract-test-matrix.md).
4. **Cấp 4 (Kiến trúc, đặc tả sâu & UX/Ops):** [`docs/architecture/*`](../architecture/system-design.md) (`ADR-0001`..`0006`), [`docs/specifications/*`](../specifications/extension-contract.md), [`docs/ux/*`](../ux/screen-specifications.md), [`docs/operations/*`](../operations/installer-upgrades.md).
5. **Cấp 5 (Đơn vị thực thi chi tiết):** [`docs/planning/units/TP-IMP-xxxx.md`](../planning/units/TP-IMP-0001.md) & [`docs/planning/implementation-index.md`](../planning/implementation-index.md).

---

## 4. Danh sách Thứ tự Khởi chạy Các Đơn vị Đầu Tiên Ngay Sau Khi Mở Khóa (`Initial Wave Queue`)

> **Hiện tại:** `0` đơn vị `READY` (`NO_GO`). Ngay khi Product Owner ký duyệt Mục 1.2, các đơn vị mở khóa theo đúng trình tự phụ thuộc sau:

| Đợt (`Wave`) | Mã Đơn vị (`UNIT_ID`) | Chế độ Chạy | Cổng Dừng Rà soát Trước Khi Qua Đợt Kế Tiếp |
|---|---|---|---|
| **Wave 0.1** | [`TP-IMP-0001`](../planning/units/TP-IMP-0001.md) | Tuần tự (`1` nhánh) | `REQUIRES_STRONG_REVIEW` — Đóng `GAP-08, 09, 11`. |
| **Wave 0.2** | [`TP-IMP-0002`](../planning/units/TP-IMP-0002.md) | Tuần tự (`1` nhánh) | `REQUIRES_STRONG_REVIEW` — Khóa `runtime.proto` & `openapi/*.yaml` (`GAP-02, 03, 07, 10, 12, 13`). |
| **Wave 0.3** | [`TP-IMP-0003`](../planning/units/TP-IMP-0003.md) | Tuần tự (`1` nhánh) | `REQUIRES_STRONG_REVIEW` — Khóa `26` bảng DDL, `AEAD AAD`, `RLS`, `Valkey {org_id}` (`GAP-04, 05, 06`). |
| **Wave 0.4 / 1.1** | [`TP-IMP-0004`](../planning/units/TP-IMP-0004.md) **\|\|** [`TP-IMP-0005`](../planning/units/TP-IMP-0005.md) | **Song song 2 `git worktree`** (`0` đụng tệp) | **`GATE-M0` (`Human + Architect Sign-Off`)** sau `0004` + CI `check.go` xanh trên `0005`. |
| **Wave 1.2** | [`TP-IMP-0006`](../planning/units/TP-IMP-0006.md) **\|\|** [`TP-IMP-0007`](../planning/units/TP-IMP-0007.md) | **Song song 2 `git worktree`** | Kiểm tra `netguard` khép kín (`0006`) + `SQLite WAL` migration (`0007`). |
| **Wave 1.3** | [`TP-IMP-0008`](../planning/units/TP-IMP-0008.md) → [`TP-IMP-0009`](../planning/units/TP-IMP-0009.md) | Tuần tự | `REQUIRES_STRONG_REVIEW` — Kho khóa phong bì 3 tầng + `RBAC` / `Virtual Key` / `ConfigSnapshot`. |
| **Wave 1.4** | [`TP-IMP-0010`](../planning/units/TP-IMP-0010.md) **\|\|** [`TP-IMP-0011`](../planning/units/TP-IMP-0011.md) | **Song song 2 `git worktree`** | `REQUIRES_STRONG_REVIEW` — `SSRFGuard` + `BlindBroker` + `.tpkg` Installer `SM-01`. |
| **Wave 1.5** | [`TP-IMP-0012`](../planning/units/TP-IMP-0012.md) **\|\|** [`TP-IMP-0013`](../planning/units/TP-IMP-0013.md) → [`TP-IMP-0014`](../planning/units/TP-IMP-0014.md) → [`TP-IMP-0015`](../planning/units/TP-IMP-0015.md) | Song song `0012\|\|0013` rồi hội tụ `0014 -> 0015` | **`GATE-M1` (`Vertical Slice Security & Architecture Gate`)** — Chứng minh `EXT-SLICE-M1`. |

---

## 5. Quy trình Thay đổi Kế hoạch / Hợp đồng Qua `Change Request (CR)`

Không một Agent thực thi nào được phép tự ý sửa đổi ngoài phạm vi `Files Touched`, đổi tên trường API/bảng SQL hoặc nới lỏng ngưỡng `SLO`/`CONSTRAINTS.md`. Khi gặp trở ngại thực tế:
1. **Dừng lây lan lỗi (`Stop-the-Line`):** Giữ nguyên trạng thái sạch trên nhánh đơn vị, ghi mục mới `CR-<NN>` vào [`docs/reviews/unresolved-gaps.md`](../reviews/unresolved-gaps.md) gồm: *Bằng chứng kỹ thuật → Các phương án đánh đổi → Danh sách `TP-IMP-*` bị ảnh hưởng*.
2. **Phân quyền Duyệt `CR`:**
   - *CR nội bộ không đổi phạm vi/bảo mật/API công khai* (ví dụ tách nhỏ tệp nội bộ): Principal Architect duyệt và cập nhật `implementation-index.md`.
   - *CR chạm phạm vi sản phẩm, ranh giới `DEC-004`, mô hình bảo mật, lược đồ lưu trữ hoặc `SLO`*: **Bắt buộc Product Owner phê duyệt** tại [`decisions.md`](./decisions.md) trước khi tiếp tục.

---

## 6. Mẫu Prompt Chuẩn Giao Việc Cho Luna & Hợp đồng Báo cáo Nghiệm thu (`Luna Prompt & Report Contract`)

### 6.1. Mẫu Prompt Tối giản Dành Cho Product Owner / Điều phối viên (Không Cần Lịch sử Chat)
```text
Đóng vai kỹ sư thực thi của dự án Tanpopo. Hãy thực hiện duy nhất đơn vị: TP-IMP-xxxx.
Bước 1: Đọc AGENTS.md, docs/governance/execution-baseline.md, docs/planning/implementation-index.md và docs/planning/units/TP-IMP-xxxx.md.
Bước 2: Kiểm tra cổng phê duyệt (nếu HANDOFF_VERDICT là NO_GO hoặc DoR chưa thỏa thì dừng ngay và báo cáo blocker, tuyệt đối không viết code).
Bước 3: Nếu đơn vị đã READY, lập kế hoạch ngắn 3–5 dòng trong đúng phạm vi Files Touched, viết test trước theo TDD (TANPOPO_TEST_HERMETIC=1, 0 gọi mạng ngoài), hiện thực hóa đúng hợp đồng, chạy toàn bộ lệnh kiểm chứng trong Mục 6 của đơn vị (cả CGO_ENABLED=0 và -race / astryx doctor --built nếu có) và xuất Báo cáo Hoàn thành theo mẫu chuẩn của execution-baseline.md.
```

### 6.2. Hợp đồng Báo cáo Nghiệm thu Bắt buộc Khi Kết thúc Mỗi `UNIT_ID` (`Mandatory Unit Completion Report`)
Mọi PR / báo cáo hoàn thành của Luna cho một `TP-IMP-xxxx` **bắt buộc** có đủ 7 mục kiểm chứng sau (thiếu 1 mục coi như chưa hoàn thành):
1. **`Unit & Gate Check`:** Mã `TP-IMP-xxxx`, xác nhận `DoR` đã thỏa và trạng thái `Gate G4`.
2. **`Scope Compliance`:** Danh sách chính xác các tệp đã tạo/sửa (`git status -s` / `git diff --stat`) đối chiếu 1-1 với mục `In-Scope (Files Touched)` (nếu phát sinh tệp ngoài danh sách phải giải trình rõ).
3. **`Acceptance & Negative Criteria Matrix`:** Bảng kết quả từng mục `AC-1..n` và `NEG-1..n` gắn với tên hàm test cụ thể (`Test...`) chứng minh đã chạy đỏ-xanh.
4. **`Hermetic & Floor-Guard Verification`:** Kết quả chạy thật (mã thoát + tóm tắt đầu ra) cho: (a) `CGO_ENABLED=0` build/test, (b) `-race` test, (c) `CanaryScanner` (`0` lộ khóa), (d) `Floor Guard` (`0` `@ts-ignore`/`nolint`/`t.Skip`/thẻ `div`-`span` thô), và (e) `astryx doctor --built dist` (`0 fail`, với đơn vị UI).
5. **`Security & Rollback Check`:** Xác nhận giữ vững `Security Invariant` ở Mục 7 của đơn vị và kiểm tra khả năng quay lui an toàn.
6. **`Review Checkpoint Routing`:** Nếu đơn vị mang nhãn **`REQUIRES_STRONG_REVIEW`**, tự động gắn cờ yêu cầu vòng phản biện độc lập từ `security-auditor` + `code-reviewer` (ngữ cảnh mới, theo `doubt-driven-development`) và phê duyệt của kỹ sư chính trước khi gộp vào `main`.
7. **`Index & Traceability Sync`:** Cập nhật trạng thái của `TP-IMP-xxxx` trong [`implementation-index.md`](../planning/implementation-index.md) sang `DONE` và mở khóa các đơn vị kế tiếp đủ điều kiện sang `READY`.
