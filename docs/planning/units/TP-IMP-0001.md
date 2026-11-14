# `TP-IMP-0001` — Đóng Đặc tả Lô A: Vá Máy Trạng thái `SM-01..09`, Bổ sung `AC` Còn Thiếu & Hiệu chỉnh Cổng `v0.1`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0001` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes`) · `EP-00` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Chờ Product Owner duyệt `Gate G1/G2` & `CR-01..04, 08, 09` — `GAP-01`)* |
| **Quy mô (`Size`)** | `M` (`5` tệp đặc tả nghiệp vụ & yêu cầu) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Chạm máy trạng thái an ninh thu hồi khóa & cổng phạm vi)* |
| **Chủ sở hữu Kỹ thuật** | `Lead Business Analyst + Principal Requirements Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Khắc phục toàn bộ các điểm chết trong máy trạng thái vòng đời (`SM-01`..`SM-08`), tách riêng máy trạng thái `SM-09` cho tác vụ `A2A`, đồng bộ danh mục `Official Reference Adapters` và phân kỳ `Tier-3 Sidecar` / `/v1/rerank` / `Webhooks` / `Response Cache` / `Admin Break-Glass` theo quyết định của Product Owner trên `CR-01..04, 08, 09`, đồng thời viết bổ sung các tiêu chí nghiệm thu `TP-AC-*` còn thiếu cho `TP-FR-0701`, `TP-FR-0702`, `TP-FR-0901` trước khi bất kỳ Agent lập trình nào chạm vào mã nguồn.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `docs/requirements/business-entity-map.md`
  - `docs/requirements/business-rules.md`
  - `docs/requirements/srs.md`
  - `docs/requirements/acceptance-criteria.md`
  - `docs/product/release-scope.md`
- **Out-of-Scope:** Không sửa `api-contracts.md` hay `data-model.md` (thuộc `TP-IMP-0002` và `TP-IMP-0003`); tuyệt đối chưa tạo bất kỳ tệp mã nguồn `.go` hay `.ts` nào.

## 3. Requirements, Dependencies & Design References
- **Requirements & Gaps Refs:** `GAP-08`, `GAP-09`, `GAP-11` (`AUD-006`, `007`, `008`, `011`, `012`, `013`, `014`, `023`, `024`); `TP-FR-0301`, `0304`, `0501`..`0504`, `0701`, `0702`, `0901`, `1004`.
- **Dependencies:** `NONE` *(Đơn vị gốc đầu tiên ngay sau khi Product Owner ký duyệt `GAP-01`).*
- **Design / Audit Refs:** [`integrated-audit.md`](../../reviews/integrated-audit.md), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-01`..`04`, `CR-08`, `CR-09`), [`decisions.md`](../../governance/decisions.md).

## 4. API, Wire & Data Contracts
- **Chuẩn hóa Chuyển tiếp Máy Trạng thái Bắt buộc:**
  - `SM-05` (`Virtual Key`): Bổ sung cạnh bắt buộc `QUOTA_PAUSED --> REVOKED`, `QUOTA_PAUSED --> SUSPENDED`, `QUOTA_PAUSED --> EXPIRED` (khóa đã thu hồi không bao giờ tự kích hoạt lại sang kỳ mới).
  - `SM-02` (`Provider Connection`): Bổ sung `DEGRADED --> SUSPENDED_BY_ADMIN`, `DEGRADED --> REVOKED`, `DRAFT --> ARCHIVED`.
  - `SM-04` (`Model Deployment`): Cho phép chuyển sang `DISABLED` từ cả `CIRCUIT_OPEN_UNHEALTHY`, `THROTTLED_COOLDOWN`, `HALF_OPEN_PROBING` và thêm cạnh lỗi từ `PROVISIONED`; khóa tên duy nhất `HALF_OPEN_PROBING` (xóa tên lệch `PROBING_HALF_OPEN`).
  - `SM-07` (`Budget Hold`): Bổ sung `STREAMING_ACCRUING --> EXPIRED_RECLAIMED`.
  - `SM-08` (`Upgrade`): Bổ sung đường thoát `BLOCKED_INCOMPATIBLE --> ABORTED_SAFE`.
  - `SM-09` (`A2A Task`): Tách khỏi `SM-06`, dùng đúng trạng thái chuẩn A2A (`SUBMITTED -> WORKING -> INPUT_REQUIRED -> COMPLETED | FAILED | CANCELED`).
- **Chuẩn hóa Enum:** Khóa 6 vai trò RBAC (`OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`), mô hình `Trust Tier` theo phương án Owner chốt tại `CR-03`, và biểu thức không gian tên cấm mạo danh `^@tanpopo(-[a-z0-9]+)?/`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given toàn bộ sơ đồ `SM-01`..`09` trong `business-entity-map.md`, When kiểm tra mọi trạng thái phi kết thúc, Then mọi trạng thái của `Virtual Key`, `Connection`, `Deployment` và `Extension` đều có đường chuyển tiếp hợp lệ sang trạng thái ngắt khẩn cấp (`REVOKED` / `DISABLED` / `QUARANTINED`).
- **`AC-2`:** Given `srs.md` và `acceptance-criteria.md`, When đối chiếu `36/36` mục `Must`, Then có kịch bản `Given/When/Then` riêng biệt cho: (a) giới hạn tốc độ `RPM`, `TPM`, `Max Concurrent Streams` (`429 rate_limit_exceeded`), (b) chặn nới lỏng chính sách con (`422 policy_relaxation_forbidden`), (c) phân trang con trỏ Admin API (`limit`, `cursor`), và (d) danh mục Official Adapters của `v0.1`.
- **`AC-3`:** Given `release-scope.md`, When kiểm tra tiêu chí xuất xưởng `MVP (v0.1)`, Then không còn yêu cầu tính năng thuộc `v0.3`/`v1.0` (`Air-Gap Bundle`, cụm đa nút `< 5s`) làm chặn oan bản đơn nút `v0.1`.
- **`NEG-1`:** Nghiêm cấm tự ý chốt `CR-01..04` nếu Product Owner chưa ghi quyết định trong `decisions.md`.
- **`NEG-2`:** Không để sót bất kỳ tham chiếu nào tới trạng thái cũ `PROBING_HALF_OPEN` hay `APPROVED_ONCE`.
- **`NEG-3`:** Không trộn lẫn bất kỳ câu chữ nào mở đường cho gói thuê bao tiêu dùng cá nhân (`DEC-004`).

## 6. Test Commands & Required Evidence
- `rg -n "PROBING_HALF_OPEN|APPROVED_ONCE" docs/` *(phải trả về `0` kết quả sau khi đồng bộ tên trạng thái chuẩn)*.
- `rg -c "^\| \*\*`TP-(FR|NFR)-[0-9]{4}`\*\*" docs/requirements/srs.md` *(đối chiếu đủ toàn bộ mục `Must` có kịch bản tương ứng trong `docs/requirements/acceptance-criteria.md`)*.
- Đính kèm bảng đối chiếu trước/sau đóng `GAP-08`, `GAP-09`, `GAP-11` trong PR review.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Bất biến `Emergency Revoke` phải thắng mọi trạng thái tạm dừng hạn mức (`QUOTA_PAUSED`) để ngăn kẻ tấn công chờ đầu tháng sau dùng lại khóa bị lộ.
- **Rollback:** Chỉ thay đổi tài liệu Markdown — có thể `git revert` nguyên tử trong 1 commit nếu cần hiệu chỉnh.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Product Owner đã phê duyệt `Gate G1`, `Gate G2` và các đề xuất `CR-01..04, 08, 09` tại [`decisions.md`](../../governance/decisions.md).
- **DoD:** Hoàn tất cập nhật 5 tệp trong phạm vi, đóng `GAP-08`, `GAP-09`, `GAP-11` trong [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md), vượt qua rà soát `REQUIRES_STRONG_REVIEW`.
