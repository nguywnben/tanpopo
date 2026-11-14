# `TP-IMP-0001` — Đóng Đặc tả Lô A: Vá Máy Trạng thái `SM-01..09`, Bổ sung `AC` Còn Thiếu & Hiệu chỉnh Cổng `v0.1`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0001` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes`) · `EP-00` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Chờ Product Owner duyệt `Gate G1/G2` & `CR-01..04, 08, 09` — `GAP-01`)* |
| **Quy mô (`Size`)** | `M` (`11` tệp đặc tả nghiệp vụ, yêu cầu, kiểm thử & cổng xuất xưởng) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Chạm máy trạng thái an ninh thu hồi khóa & cổng phạm vi)* |
| **Chủ sở hữu Kỹ thuật** | `Lead Business Analyst + Principal Requirements Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Khắc phục toàn bộ các điểm chết trong máy trạng thái vòng đời (`SM-01`..`SM-08`), tách riêng máy trạng thái `SM-09` cho tác vụ `A2A`, bổ sung chính thức các dòng yêu cầu `TP-FR-*` cho `Tier-3 Sidecar`, `/v1/rerank`, `Webhooks`, `Opt-In Response Cache` và `Admin Break-Glass` theo quyết định của Product Owner trên `CR-01..04, 08, 09`, đồng thời bổ sung đầy đủ các tiêu chí nghiệm thu `TP-AC-*` và ca kiểm thử `CT-*`/`AUTH-*` còn thiếu cho `TP-FR-0701`, `TP-FR-0702`, `TP-FR-0901` trước khi bất kỳ Agent lập trình nào chạm vào mã nguồn.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `docs/requirements/business-entity-map.md`, `docs/requirements/business-rules.md`, `docs/requirements/srs.md`, `docs/requirements/acceptance-criteria.md`, `docs/requirements/traceability.md`.
  - `docs/product/release-scope.md`.
  - `docs/ux/user-flows.md` (đồng bộ tên trạng thái `SM-06` khỏi chuỗi lạc chuẩn cũ).
  - `docs/specifications/mcp-a2a-boundaries.md` (đồng bộ tách `SM-09` khỏi `SM-06`).
  - `docs/quality/contract-test-matrix.md` & `docs/quality/release-readiness.md` (bổ sung ca `CT-*`/`AUTH-*` cho `GAP-11` và hiệu chỉnh cổng `v0.1` tại `GATE-REL-06` khớp `CR-04`).
  - `docs/reviews/unresolved-gaps.md` (đánh dấu đóng `GAP-08`, `GAP-09`, `GAP-11` kèm dòng dẫn chứng).
- **Out-of-Scope:** Không sửa `api-contracts.md` hay `data-model.md` (thuộc `TP-IMP-0002` và `TP-IMP-0003`); tuyệt đối chưa tạo bất kỳ tệp mã nguồn `.go` hay `.ts` nào.

## 3. Requirements, Dependencies & Design References
- **Requirements & Gaps Refs:** `GAP-08`, `GAP-09`, `GAP-11` (`AUD-006`, `007`, `008`, `011`, `012`, `013`, `014`, `023`, `024`); `TP-FR-0301`, `0304`, `0501`..`0504`, `0701`, `0702`, `0901`, `1004`.
- **Dependencies:** `NONE` *(Đơn vị gốc đầu tiên ngay sau khi Product Owner ký duyệt `GAP-01`).*
- **Design / Audit Refs:** [`integrated-audit.md`](../../reviews/integrated-audit.md), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-01`..`04`, `CR-08`, `CR-09`), [`decisions.md`](../../governance/decisions.md).

## 4. API, Wire & Data Contracts
- **Chuẩn hóa Chuyển tiếp Máy Trạng thái Bắt buộc:**
  - `SM-01` (`Extension Lifecycle`): Khóa tên chuẩn `DISCOVERED -> VERIFYING -> PENDING_PERMISSION_APPROVAL -> INSTALLED_DISABLED | ENABLED_ACTIVE -> UPDATING_PENDING_RECONSENT | DRAINING | ROLLED_BACK | QUARANTINED`; đảm bảo mọi trạng thái phi kết thúc (`DISCOVERED`, `VERIFYING`, `PENDING_PERMISSION_APPROVAL`, `INSTALLED_DISABLED`, `ENABLED_ACTIVE`, `UPDATING_PENDING_RECONSENT`, `DRAINING`) đều có cạnh hợp lệ chuyển thẳng sang `QUARANTINED`.
  - `SM-02` (`Provider Connection`): Khóa tên chuẩn `DRAFT -> VALIDATING -> ACTIVE <-> DEGRADED | AUTH_EXPIRED_REAUTH_REQUIRED | SUSPENDED_BY_ADMIN | REVOKED | ARCHIVED`; bổ sung `DEGRADED --> SUSPENDED_BY_ADMIN`, `DEGRADED --> REVOKED`, `DRAFT --> ARCHIVED`.
  - `SM-04` (`Model Deployment`): Khóa tên chuẩn `PROVISIONED -> ACTIVE_HEALTHY <-> THROTTLED_COOLDOWN | CIRCUIT_OPEN_UNHEALTHY -> HALF_OPEN_PROBING -> DISABLED` (cho phép chuyển sang `DISABLED` từ mọi trạng thái hoạt động/lỗi).
  - `SM-05` (`Virtual Key`): Khóa tên chuẩn `CREATED_ACTIVE -> ROTATING_DUAL_VALID -> QUOTA_PAUSED | SUSPENDED | REVOKED | EXPIRED`; bổ sung cạnh `QUOTA_PAUSED --> REVOKED`, `QUOTA_PAUSED --> SUSPENDED`, `QUOTA_PAUSED --> EXPIRED`.
  - `SM-06` (`MCP HITL Approval`): Giữ riêng cho MCP (`PENDING -> APPROVED -> CONSUMED | REJECTED | EXPIRED`).
  - `SM-07` (`Budget Hold`): Bổ sung `STREAMING_ACCRUING --> EXPIRED_RECLAIMED`.
  - `SM-08` (`Upgrade`): Bổ sung đường thoát `BLOCKED_INCOMPATIBLE --> ABORTED_SAFE`.
  - `SM-09` (`A2A Task`): Tách khỏi `SM-06`, dùng đúng trạng thái giao thức dây truyền chữ thường chuẩn A2A (`submitted -> working <-> input-required -> completed | failed | canceled`) ánh xạ 1-1 sang enum CSDL chữ hoa.
- **Chuẩn hóa Enum & Bổ sung Dòng SRS (`CR-01, 02, 04, 09`):**
  - Khóa 6 vai trò RBAC chuẩn trên toàn bộ tài liệu: `OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`.
  - Khóa biểu thức không gian tên cấm mạo danh `^@tanpopo(-[a-z0-9]+)?/` và mô hình `Trust Tier` theo quyết định của Owner tại `CR-03`.
  - Bổ sung mã `TP-FR-0308` (`Tier-3 Isolated Sidecar` — `Could / v0.3`) và cập nhật rõ phạm vi `/v1/rerank`, `Webhooks`, `Opt-In Response Cache`, `tanpopo admin reset-local-owner` trong `srs.md`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given toàn bộ sơ đồ `SM-01`..`09` trong `business-entity-map.md`, When kiểm tra mọi trạng thái phi kết thúc, Then mọi trạng thái của `Virtual Key`, `Connection`, `Deployment` và `Extension` đều có đường chuyển tiếp hợp lệ sang trạng thái ngắt khẩn cấp (`REVOKED` / `DISABLED` / `QUARANTINED`).
- **`AC-2`:** Given `srs.md`, `acceptance-criteria.md` và `contract-test-matrix.md`, When đối chiếu `36/36` mục `Must`, Then có kịch bản `Given/When/Then` và mã kiểm thử riêng cho: (a) giới hạn tốc độ `RPM`, `TPM`, `Max Concurrent Streams` (`429 rate_limit_exceeded`), (b) chặn nới lỏng chính sách con (`422 policy_relaxation_forbidden`), (c) phân trang con trỏ Admin API (`limit`, `cursor`), và (d) danh mục Official Adapters của `v0.1`.
- **`AC-3`:** Given `release-scope.md` và `release-readiness.md`, When kiểm tra tiêu chí xuất xưởng `MVP (v0.1)`, Then không còn yêu cầu tính năng thuộc `v0.3`/`v1.0` (`Air-Gap Bundle`, cụm đa nút `< 5s`) làm chặn oan bản đơn nút `v0.1`.
- **`NEG-1`:** Nghiêm cấm tự ý chốt `CR-01..04, 08, 09` nếu Product Owner chưa ghi quyết định trong `decisions.md`.
- **`NEG-2`:** Không còn bất kỳ tham chiếu nào tới tên trạng thái cũ bị loại bỏ bên trong các thư mục đặc tả quy chuẩn (`docs/product`, `docs/requirements`, `docs/specifications`, `docs/architecture`, `docs/ux`, `docs/operations`, `docs/security`, `docs/quality`).
- **`NEG-3`:** Không trộn lẫn bất kỳ câu chữ nào mở đường cho gói thuê bao tiêu dùng cá nhân (`DEC-004`).

## 6. Test Commands & Required Evidence
- Đa nền tảng (Node.js có sẵn trên mọi máy trạm): `node -e "const fs=require('fs'),p=require('path');const dirs=['product','requirements','specifications','architecture','ux','operations','security','quality'];let hits=0;const walk=d=>fs.readdirSync(d,{withFileTypes:true}).forEach(e=>{const f=p.join(d,e.name);e.isDirectory()?walk(f):f.endsWith('.md')&&/PROBING_HALF_OPEN|APPROVED_ONCE/.test(fs.readFileSync(f,'utf8'))&&(console.error('Drift:',f),hits++)});dirs.forEach(d=>walk(p.join('docs',d)));process.exit(hits?1:0)"`
- Đính kèm bảng đối chiếu trước/sau đóng `GAP-08`, `GAP-09`, `GAP-11` trong PR review.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Bất biến `Emergency Revoke` phải thắng mọi trạng thái tạm dừng hạn mức (`QUOTA_PAUSED`) để ngăn kẻ tấn công chờ đầu tháng sau dùng lại khóa bị lộ.
- **Rollback:** Chỉ thay đổi tài liệu Markdown — có thể `git revert` nguyên tử trong 1 commit nếu cần hiệu chỉnh.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Product Owner đã phê duyệt `Gate G1`, `Gate G2` và các đề xuất `CR-01..04, 08, 09` tại [`decisions.md`](../../governance/decisions.md).
- **DoD:** Hoàn tất cập nhật toàn bộ 11 tệp trong phạm vi, lệnh kiểm tra Node trả mã thoát `0`, đóng `GAP-08`, `GAP-09`, `GAP-11` trong [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md), vượt qua rà soát `REQUIRES_STRONG_REVIEW`.
