# `TP-IMP-0044` — Kiểm toán An ninh Toàn diện, Chạy Fuzzing `>= 4h` (`FZ-01..06`), Bộ Đệm Tương thích `N-1` (`migrate-check`), E2E Đa Trình duyệt/OS & Khắc phục Pen-Test

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0044` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release`) · `EP-09` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0038`, `TP-IMP-0041`, `TP-IMP-0042` — `GAP-01`)* |
| **Quy mô (`Size`)** | `L` (`tests/security/*`, `tests/e2e/*`, `internal/extension/compat/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Kiểm định đối kháng bảo mật `CT-01..32` + `AUTH-01..16`, Fuzzing dài hạn `>= 4h` & cam kết tương thích ngược `1.x`)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Security Auditor & QA Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Thực thi đợt tôi luyện chất lượng và kiểm toán an ninh cuối cùng trước khi đóng băng hợp đồng `v1.0 GA` khớp `100%` định nghĩa chuẩn trong [`release-readiness.md`](../../quality/release-readiness.md) (`GATE-REL-01`, `GATE-REL-02`, `GATE-REL-03`, `GATE-REL-05`):
1. **Chạy Đầy đủ Ma trận Đối kháng `48/48` Ca (`CT-01`..`32` + `AUTH-01`..`16`), Chiến dịch Fuzzing Liên tục `>= 4 Giờ` (`FZ-01`..`06`) & Kiểm thử Xâm nhập Hộp Xám 5 Bề mặt Bắt buộc (`5-Surface Grey-Box Penetration Test` — `GATE-REL-03`):** Kiểm thử tấn công thực chiến trên cả 5 bề mặt trọng yếu (Wasm/Sidecar Sandbox escape, `SSRF / DNS Rebinding` & `BlindBroker` exfiltration, leo thang đa người thuê `RLS / AEAD AAD`, đánh tráo `MCP Rug-Pull / OAuth RFC 8707`, và thao túng sổ cái/chuỗi băm kiểm toán) kèm khắc phục triệt để `100%` phát hiện mức `Critical / High`.
2. **Đóng băng Hợp đồng `v1.0` & Công cụ Kiểm tra Tương thích Ngược `N-1` (`tanpopo plugin migrate-check` — `CT-24`, `GATE-REL-02`):** Đảm bảo các gói `.tpkg` viết cho nhánh `0.x` cuối cùng chạy trơn tru trên `v1.0` và thiết lập cam kết ổn định `>= 12 tháng` cho nhánh `1.x`.
3. **Kiểm thử Đầu-Cuối Đa Trình duyệt (`Chromium`, `Firefox`, `WebKit`), Đa Độ phân giải (`1280`, `768`, `375`), `WCAG 2.2 AA` (`axe-core` Sáng + Tối), Lighthouse CI (`UX-AC-08`) & Đa Hệ điều hành (`Linux`, `macOS`, `Windows`) (`GATE-REL-02`, `GATE-REL-05`).**

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `tests/security/adversarial_matrix_test.go` & `docs/security/pentest-v1.0-remediation-report.md` (hội tụ kiểm chứng đủ 32 ca `CT-*` + 16 ca `AUTH-*` và báo cáo khắc phục kiểm thử xâm nhập hộp xám 5 bề mặt).
  - `scripts/qa/run-fuzz-campaign.go` & kho hạt giống hồi quy `testdata/fuzz/*` cho cả 6 hàm `FZ-01`..`06`.
  - `internal/extension/compat/v1_shim.go` & `internal/cli/plugin/migrate_check.go` (`tanpopo plugin migrate-check` — `CT-24`).
  - `web/console/e2e/*.spec.ts` (Playwright chạy đủ 8 hành trình `UJ-01`..`08` trên 3 trình duyệt + quét `axe-core` Sáng/Tối `UX-AC-05`, kiểm tra mù màu `UX-AC-06` & Lighthouse CI `UX-AC-08` trên toàn bộ 16 màn hình `SCR-01`..`16`).
- **Out-of-Scope:** Xuất bản cổng tài liệu người dùng cuối (thuộc `TP-IMP-0046`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-NFR-0401`..`0404`, `TP-NFR-0701`, `TP-NFR-0702`, `TP-NFR-0801`, `TP-NFR-0901`, `TP-NFR-0902`, `CT-01`..`32`, `AUTH-01`..`16`, `FZ-01`..`06`, `UX-AC-05`, `UX-AC-06`, `UX-AC-08`, `GATE-REL-01`, `GATE-REL-02`, `GATE-REL-03`, `GATE-REL-05`.
- **Dependencies:** `TP-IMP-0038`, `TP-IMP-0041`, `TP-IMP-0042`.
- **Design Refs:** [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`SPEC-QA-01`), [`test-strategy.md`](../../quality/test-strategy.md) (`SPEC-QA-02`), [`release-readiness.md`](../../quality/release-readiness.md) (`SPEC-QA-04`).

## 4. API, Wire & Data Contracts
- **Chính sách Lỗi Nghiêm ngặt (`Zero Sev-0 / Sev-1 Policy` — `SPEC-QA-04`):** Bất kỳ lỗi nào làm lộ bí mật, vượt ranh giới Wasm/SSRF, đọc chéo tổ chức, sai lệch sổ cái tiền tệ hoặc vi phạm `CONSTRAINTS.md` đều tự động chặn đứng tiến trình xét duyệt xuất xưởng.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`GATE-REL-01 & 03` Adversarial Matrix, Fuzzing `>= 4h` & 5-Surface Grey-Box Pen-Test):** Chạy trọn vẹn 48 ca `CT/AUTH`, quét tĩnh `gosec` + `govulncheck` + `npm audit --omit=dev` đạt `0 Critical / 0 High`, chạy chiến dịch `FZ-01..06` `>= 4 giờ` (`0` crash) và hoàn tất biên bản kiểm thử xâm nhập hộp xám 5 bề mặt với `0` lỗ hổng `Critical / High` tồn đọng.
- **`AC-2` (`GATE-REL-02 & 05` Multi-Browser, Accessibility & Core Web Vitals):** Toàn bộ `UJ-01`..`08` chạy xanh trên `Chromium`, `Firefox`, `WebKit` (`0 flaky retries`), `axe-core` Sáng/Tối đạt `0` lỗi `critical`/`serious` trên 16 màn hình (`UX-AC-05`), Lighthouse CI đạt `LCP <= 2.0s`, `INP <= 150ms`, `CLS <= 0.05` (`UX-AC-08`), và `npx @astryxdesign/cli doctor --built dist` đạt `0 fail`.
- **`AC-3` (`CT-24` & `GATE-REL-02` Contract N-1 Compatibility):** Các gói `.tpkg` đóng gói từ `M1/M2` chạy qua `tanpopo plugin migrate-check` và thực thi thành công trên nhân `v1.0 RC`.
- **`NEG-1`:** Kiểm tra tự động `CONSTRAINTS.md` (`go run ./scripts/ci/check.go --all`) xác nhận `0` câu lệnh bỏ qua kiểm tra an toàn hay hạ chuẩn độ phủ kiểm thử.

## 6. Test Commands & Required Evidence
- `go test -v -race ./tests/security/...`
- `go run ./scripts/qa/run-fuzz-campaign.go --duration=4h`
- `cd web/console && npx playwright test --project=chromium --project=firefox --project=webkit && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Mọi lỗ hổng tìm thấy bởi fuzzer hoặc Pen-Test hộp xám đều phải được chuyển thành hạt giống kiểm thử hồi quy vĩnh viễn trong `testdata/` trước khi đóng đơn vị này.
- **Rollback:** Chỉ bổ sung chốt chặn tương thích, bài kiểm thử và bản vá cứng hóa bảo mật.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0038`, `TP-IMP-0041` và `TP-IMP-0042` đã hoàn tất.
- **DoD:** Đạt `100%` tiêu chí `GATE-REL-01`, `GATE-REL-02`, `GATE-REL-03` và `GATE-REL-05`, vượt qua `REQUIRES_STRONG_REVIEW` của cả `security-auditor` và `test-engineer`.
