# `TP-IMP-0047` — Hồ sơ Sẵn sàng Xuất xưởng (`GATE-REL-01`..`07`), Kịch bản Cuộn Canary & **Cổng Xin Chữ ký Phê duyệt Phát hành Công khai Của Product Owner**

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0047` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release` — **Cổng Cuối `GATE-M5` / `GATE-REL-07`**) · `EP-09` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0046` + **Bắt buộc Chữ ký Con người của Product Owner**)* |
| **Quy mô (`Size`)** | `S` (`docs/releases/v1.0.0-readiness-dossier.md`, `scripts/release/pre-publish-check.sh`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** + **`MANDATORY_HUMAN_OWNER_SIGN_OFF`** *(Tuyệt đối cấm AI Agent tự động phát hành công khai!)* |
| **Chủ sở hữu Kỹ thuật** | `Technical Program Manager, Principal Security Engineer & Product Owner` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Tập hợp và kiểm chứng chéo toàn bộ bằng chứng định lượng của 6 cổng kỹ thuật (`GATE-REL-01`..`06`), lập phương án triển khai theo từng bước an toàn (`RC -> Canary -> GA` kèm nút quay lui tức thì `< 5 phút`), và trình **Hồ sơ Sẵn sàng Xuất xưởng (`Release Readiness Dossier`)** lên **Product Owner** tại **`GATE-REL-07`** theo quy tắc bất khả xâm phạm của dự án: **việc gắn thẻ phát hành chính thức (`git tag v1.0.0`), đẩy ảnh container công khai hay mở GitHub Release bắt buộc phải có quyết định phê duyệt tường minh của con người (Product Owner), tuyệt đối không bao giờ tự động hóa cho AI Agent tự chạy!**

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `scripts/release/pre-publish-check.sh` (kịch bản kiểm định tổng hợp một lệnh duy nhất kiểm tra đủ 6 cổng kỹ thuật: `0` lỗi `Sev-0/Sev-1`, `100%` bài kiểm thử đơn vị/hợp đồng/bảo mật/E2E xanh, `astryx doctor --built` `0 fail`, `SBOM` + `Cosign` hợp lệ, `SLO/FMEA/DR` có bằng chứng đo thật, `check-drift.sh` xanh, `CONSTRAINTS.md` không bị hạ chuẩn).
  - `docs/releases/v1.0.0-readiness-dossier.md` (bảng tổng hợp đường dẫn bằng chứng cho từng mục `GATE-REL-01`..`07`, danh mục giới hạn đã biết trung thực `Known Limitations`, hướng dẫn cuộn `Canary 1% -> 10% -> 50% -> 100%`, ngưỡng tự động hủy cuộn khi tỷ lệ lỗi `5xx > 0.5%` hoặc `ALT-AUDIT-TAMPER > 0`, và ô chữ ký phê duyệt dành riêng cho Product Owner).
- **Out-of-Scope:** **Agent thực thi KHÔNG ĐƯỢC PHÉP chạy `git push --tags`, `gh release create`, `docker push` lên kho công khai hay `npm publish` nếu chưa nhận lệnh xác nhận trực tiếp từ Product Owner sau khi xem hồ sơ này.**

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** Toàn bộ `54/54` yêu cầu `Must` (`P0`) + `19` yêu cầu `Should/Could` của nhánh `v1.0`, `GATE-REL-01`..`07`, `DEC-001`..`008`.
- **Dependencies:** `TP-IMP-0046`.
- **Design Refs:** [`release-readiness.md`](../../quality/release-readiness.md) (`SPEC-QA-04`), [`roadmap.md`](../roadmap.md), [`milestone-map.md`](../milestone-map.md).

## 4. API, Wire & Data Contracts
- **Hợp đồng 7 Cổng Xuất xưởng (`GATE-REL-01` .. `GATE-REL-07`):**
  1. `GATE-REL-01`: Đầy đủ hợp đồng, `100%` `P0` pass, `0` `Sev-0/Sev-1`, `0` hạ chuẩn `CONSTRAINTS.md`.
  2. `GATE-REL-02`: An ninh, cô lập Sandbox, Fuzzing `>= 4h`, `SBOM` + `Cosign`, `0` CVE `Critical/High`.
  3. `GATE-REL-03`: Độ tin cậy thực nghiệm `SLO-01..08`, `FMEA-01..08`, `DR-01..04` (`RTO < 15m`), đa trình duyệt + `WCAG 2.2 AA`.
  4. `GATE-REL-04`: Tuân thủ pháp lý `DEC-004` (không lạm dụng thuê bao cá nhân) & đồng bộ giấy phép `Apache-2.0`.
  5. `GATE-REL-05`: Tài liệu thực thi được, `tanpopo doctor`, `support-bundle` `0` rò rỉ bí mật, kiểm chứng `Air-Gap`.
  6. `GATE-REL-06`: Kịch bản cuộn `Canary` có ngưỡng tự động quay lui `< 5 phút` đã diễn tập thành công.
  7. **`GATE-REL-07`: Phê duyệt tường minh bằng văn bản của Product Owner.**

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Chạy `bash scripts/release/pre-publish-check.sh` trên bản ứng viên `Release Candidate (RC)` trả về mã thoát `0` với toàn bộ 6 cổng kỹ thuật `GATE-REL-01..06 = PASS` và xuất biên bản JSON/Markdown đầy đủ mã băm SHA-256 của từng hiện vật.
- **`AC-2` (`Canary Rollback Drill < 5m`):** Diễn tập kích hoạt quay lui từ bản `v1.0.0-rc1` về bản ổn định liền trước (bao gồm kiểm tra tương thích ngược lược đồ DB *Expand-Only* và giữ nguyên lưu lượng SSE) hoàn tất trong `< 5 phút`.
- **`NEG-1` (`Hard Block on Unapproved Public Publish`):** Khi biến xác nhận của con người chưa được Product Owner cấp tường minh, mọi kịch bản xuất bản công khai đều từ chối chạy ngay từ dòng đầu tiên (`403 human_release_approval_required`).

## 6. Test Commands & Required Evidence
- `bash scripts/release/pre-publish-check.sh`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Khóa ký phát hành chính thức của dự án (`Production Release Signing Key` / OIDC GitHub Environment có `Required Reviewers = Product Owner`) tách biệt hoàn toàn khỏi khóa kiểm thử CI thông thường — Agent kỹ thuật không có quyền tự lấy khóa phát hành sản xuất.
- **Rollback:** Sổ tay quay lui `< 5 phút` nằm ngay trong Mục 4 của `v1.0.0-readiness-dossier.md`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Toàn bộ 46 đơn vị phía trước (`TP-IMP-0001` .. `TP-IMP-0046`) đã hoàn thành `100%` và vượt qua kiểm toán độc lập.
- **DoD:** Hồ sơ `docs/releases/v1.0.0-readiness-dossier.md` hoàn chỉnh, `pre-publish-check.sh` xanh toàn bộ `GATE-REL-01..06`, và **dừng chờ chữ ký phê duyệt phát hành công khai của Product Owner tại `GATE-REL-07`**.
