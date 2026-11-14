# `TP-IMP-0047` — Hồ sơ Sẵn sàng Xuất xưởng (`GATE-REL-01`..`07`), Kịch bản Cuộn Canary & **Cổng Xin Chữ ký Phê duyệt Phát hành Công khai Của Product Owner**

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0047` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release` — **Cổng Cuối `GATE-M5` / `GATE-REL-07`**) · `EP-09` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0046` + **Bắt buộc Chữ ký Con người của Product Owner**)* |
| **Quy mô (`Size`)** | `S` (`docs/releases/v1.0.0-readiness-dossier.md`, `scripts/release/pre_publish_check.go`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** + **`MANDATORY_HUMAN_OWNER_SIGN_OFF`** *(Tuyệt đối cấm AI Agent tự động phát hành công khai!)* |
| **Chủ sở hữu Kỹ thuật** | `Technical Program Manager, Principal Security Engineer & Product Owner` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Tập hợp và kiểm chứng chéo toàn bộ bằng chứng định lượng của 6 cổng kỹ thuật (`GATE-REL-01`..`06` theo đúng định nghĩa gốc tại [`release-readiness.md`](../../quality/release-readiness.md) Mục 3), lập phương án triển khai theo từng bước an toàn (`RC -> Canary 5% -> 25% -> 50% -> 100%` kèm quay lui tức thì **`< 60 giây`** theo `DR-04` và `roadmap.md`), và trình **Hồ sơ Sẵn sàng Xuất xưởng (`Release Readiness Dossier`)** lên **Product Owner** tại **`GATE-REL-07`**: **việc gắn thẻ phát hành chính thức (`git tag v1.0.0`), đẩy ảnh container công khai hay mở GitHub Release bắt buộc phải có quyết định phê duyệt tường minh của con người (Product Owner), tuyệt đối không bao giờ tự động hóa cho AI Agent tự chạy!**

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `scripts/release/pre_publish_check.go` (trình kiểm định tổng hợp đa nền tảng kiểm tra đủ 6 cổng kỹ thuật `GATE-REL-01`..`06`).
  - `docs/releases/v1.0.0-readiness-dossier.md` (bảng tổng hợp đường dẫn bằng chứng cho từng mục `GATE-REL-01`..`07`, danh mục giới hạn đã biết trung thực `Known Limitations`, hướng dẫn cuộn `Canary 5% -> 25% -> 50% -> 100%`, ngưỡng tự động hủy cuộn khi tỷ lệ lỗi nội bộ **`>= 0.05%`** / tốc độ đốt ngân sách lỗi `>= 14.4x` / `ALT-AUDIT-TAMPER > 0` theo đúng `SLO-04` & `roadmap.md`, và ô chữ ký phê duyệt dành riêng cho Product Owner).
- **Out-of-Scope:** **Agent thực thi KHÔNG ĐƯỢC PHÉP chạy `git push --tags`, `gh release create`, `docker push` lên kho công khai hay `npm publish` nếu chưa nhận lệnh xác nhận trực tiếp từ Product Owner sau khi xem hồ sơ này.**

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** Toàn bộ `54/54` yêu cầu `Must` (`P0`) + `19` yêu cầu `Should/Could` của nhánh `v1.0`, `GATE-REL-01`..`07`, `DR-04`, `DEC-001`..`008`.
- **Dependencies:** `TP-IMP-0046`.
- **Design Refs:** [`release-readiness.md`](../../quality/release-readiness.md) (`SPEC-QA-04`), [`roadmap.md`](../roadmap.md), [`milestone-map.md`](../milestone-map.md).

## 4. API, Wire & Data Contracts
- **Hợp đồng 7 Cổng Xuất xưởng Chuẩn Khớp `100%` `release-readiness.md` (`GATE-REL-01` .. `GATE-REL-07`):**
  1. **`GATE-REL-01` (`Functional, Contract & Floor Guard`):** `100%` yêu cầu `P0` pass, trọn vẹn `CT-01..32`, `AUTH-01..16`, `EXT-ACCEPT-01`, `0` lỗi `Sev-0/Sev-1`, `0` vi phạm hạ chuẩn `CONSTRAINTS.md`.
  2. **`GATE-REL-02` (`Compatibility Matrix`):** Xanh trên đủ 5 tổ hợp OS/Arch (`Linux/macOS/Windows`), tương đương hành vi kép `SQLite <-> PostgreSQL 16`, đa trình duyệt `Chromium/Firefox/WebKit`, và tương thích ngược `N-1` (`tanpopo plugin migrate-check`).
  3. **`GATE-REL-03` (`Security, Fuzzing, Grey-Box Pen-Test & SLO Performance`):** `0` CVE `Critical/High`, Fuzzing `FZ-01..06 >= 4 giờ` (`0` crash), **hoàn tất kiểm thử xâm nhập hộp xám 5 bề mặt (`v1.0` Grey-Box Pen-Test) với `0 Critical/High` tồn đọng**, và xác thực thực nghiệm `SLO-01..08` + `FMEA-01..08` trên cấu hình chuẩn `2 vCPU / 2 GiB RAM`.
  4. **`GATE-REL-04` (`License, Provenance & DEC-004 Legal Compliance`):** Đồng bộ `Apache-2.0`, `CycloneDX SBOM` + chứng thực `SLSA` + chữ ký `Cosign`, và tuân thủ tuyệt đối `DEC-004` (không chứa bất kỳ connector hay hướng dẫn lạm dụng gói thuê bao cá nhân nào).
  5. **`GATE-REL-05` (`Documentation, OpenAPI/CLI Sync, Astryx Doctor & Accessibility`):** `check_drift.go` xanh `100%`, `npx @astryxdesign/cli doctor --built dist` đạt `0 fail`, `axe-core` `WCAG 2.2 AA` Sáng/Tối đạt `0` vi phạm nghiêm trọng, `UX-AC-01..12` đạt.
  6. **`GATE-REL-06` (`Operational SRE, DR Drills, Air-Gap & Canary Rollback < 60s`):** `tanpopo doctor`, `support-bundle` `0` lộ bí mật, diễn tập bấm giờ `DR-01..04` (`RTO < 15m`), `Air-Gap Bundle` (`CT-31`), đủ liên kết cảnh báo `INC-01..07`, và diễn tập quay lui `Canary < 60 giây` (`DR-04`).
  7. **`GATE-REL-07` (`Explicit Human Product Owner Sign-Off`):** Phê duyệt tường minh bằng văn bản của Product Owner.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Chạy `go run ./scripts/release/pre_publish_check.go` trên bản ứng viên `Release Candidate (RC)` trả về mã thoát `0` với toàn bộ 6 cổng kỹ thuật `GATE-REL-01..06 = PASS` và xuất biên bản JSON/Markdown đầy đủ mã băm SHA-256 của từng hiện vật.
- **`AC-2` (`DR-04` Canary Rollback Drill `< 60s`):** Diễn tập kích hoạt quay lui từ bản `v1.0.0-rc1` về bản ổn định liền trước (bao gồm kiểm tra tương thích ngược lược đồ DB *Expand-Only* và giữ nguyên lưu lượng SSE) hoàn tất trong **`< 60 giây`**.
- **`NEG-1` (`Hard Block on Unapproved Public Publish`):** Khi biến xác nhận của con người chưa được Product Owner cấp tường minh, mọi kịch bản xuất bản công khai đều từ chối chạy ngay từ dòng đầu tiên (`403 human_release_approval_required`).

## 6. Test Commands & Required Evidence
- `go run ./scripts/release/pre_publish_check.go`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Khóa ký phát hành chính thức của dự án (`Production Release Signing Key` / OIDC GitHub Environment có `Required Reviewers = Product Owner`) tách biệt hoàn toàn khỏi khóa kiểm thử CI thông thường — Agent kỹ thuật không có quyền tự lấy khóa phát hành sản xuất.
- **Rollback:** Sổ tay quay lui `< 60 giây` (`DR-04`) nằm ngay trong Mục 4 của `v1.0.0-readiness-dossier.md`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Toàn bộ 46 đơn vị phía trước (`TP-IMP-0001` .. `TP-IMP-0046`) đã hoàn thành `100%` và vượt qua kiểm toán độc lập.
- **DoD:** Hồ sơ `docs/releases/v1.0.0-readiness-dossier.md` hoàn chỉnh, `pre-publish-check.sh` xanh toàn bộ `GATE-REL-01..06`, và **dừng chờ chữ ký phê duyệt phát hành công khai của Product Owner tại `GATE-REL-07`**.
