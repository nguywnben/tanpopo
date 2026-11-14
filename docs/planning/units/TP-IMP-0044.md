# `TP-IMP-0044` — Kiểm toán An ninh Toàn diện, Chạy Fuzzing `>= 4h` (`FZ-01..06`), Bộ Đệm Tương thích `N-1` (`migrate-check`), E2E Đa Trình duyệt/OS & Khắc phục Pen-Test

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0044` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release`) · `EP-08` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0038`, `TP-IMP-0041`, `TP-IMP-0042` — `GAP-01`)* |
| **Quy mô (`Size`)** | `L` (`tests/security/*`, `tests/e2e/*`, `internal/extension/compat/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Kiểm định đối kháng bảo mật `CT-01..32` + `AUTH-01..16`, Fuzzing dài hạn `>= 4h` & cam kết tương thích ngược `1.x`)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Security Auditor & QA Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Thực thi đợt tôi luyện chất lượng và kiểm toán an ninh cuối cùng trước khi đóng băng hợp đồng `v1.0 GA` (`TP-NFR-0401`..`0404`, `TP-NFR-0701`..`0902`, `SPEC-QA-01`, `SPEC-QA-04` — `GATE-REL-01..03`):
1. **Chạy Đầy đủ Ma trận Đối kháng `48/48` Ca (`CT-01`..`32` + `AUTH-01`..`16`) & Chiến dịch Fuzzing Liên tục `>= 4 Giờ` (`FZ-01`..`06`):** Quét kiệt mọi bộ phân tích dữ liệu không tin cậy (ánh xạ Tầng 1, kho nén `.tppkg` & manifest, luồng SSE, JSON-RPC MCP, ranh giới con trỏ Wasm, bất biến sổ cái `micro-USD`) với kết quả bắt buộc `0 crash`, `0 panic`, `0 OOM`, `0 memory bleed`.
2. **Đóng băng Hợp đồng `v1.0` & Công cụ Kiểm tra Tương thích Ngược `N-1` (`tanpopo plugin migrate-check` — `BA-OQ-005`):** Đảm bảo các gói mở rộng viết cho nhánh `0.x` cuối cùng chạy trơn tru trên `v1.0` qua bộ chuyển đổi tương thích hoặc nhận chẩn đoán nâng cấp chính xác từng dòng, thiết lập cam kết ổn định `>= 12 tháng` cho nhánh `1.x`.
3. **Kiểm thử Đầu-Cuối Đa Trình duyệt (`Chromium`, `Firefox`, `WebKit`), Đa Độ phân giải (`1280`, `768`, `375`), `WCAG 2.2 AA` (`axe-core`) & Đa Hệ điều hành (`Linux`, `macOS`, `Windows`).**

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `tests/security/adversarial_matrix_test.go` (hội tụ kiểm chứng tự động đủ 32 ca `CT-*` và 16 ca `AUTH-*` trên môi trường tích hợp đầy đủ).
  - `scripts/qa/run-fuzz-campaign.sh` & kho hạt giống hồi quy `testdata/fuzz/*` cho cả 6 hàm `FZ-01`..`06`.
  - `internal/extension/compat/v1_shim.go` & `internal/cli/plugin/migrate_check.go` (`tanpopo plugin migrate-check`).
  - `web/console/e2e/*.spec.ts` (Playwright chạy đủ 8 hành trình `UJ-01`..`08` trên 3 trình duyệt + quét `axe-core` trên toàn bộ 16 màn hình `SCR-01`..`16`).
- **Out-of-Scope:** Xuất bản cổng tài liệu người dùng cuối (thuộc `TP-IMP-0046`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-NFR-0401`..`0404`, `TP-NFR-0701`, `TP-NFR-0702`, `TP-NFR-0801`, `TP-NFR-0901`, `TP-NFR-0902`, `CT-01`..`32`, `AUTH-01`..`16`, `FZ-01`..`06`, `GATE-REL-01`..`03`.
- **Dependencies:** `TP-IMP-0038`, `TP-IMP-0041`, `TP-IMP-0042`.
- **Design Refs:** [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`SPEC-QA-01`), [`test-strategy.md`](../../quality/test-strategy.md) (`SPEC-QA-02`), [`release-readiness.md`](../../quality/release-readiness.md) (`SPEC-QA-04`).

## 4. API, Wire & Data Contracts
- **Chính sách Lỗi Nghiêm ngặt (`Zero Sev-0 / Sev-1 Policy` — `SPEC-QA-04`):** Bất kỳ lỗi nào làm lộ bí mật, vượt ranh giới Wasm/SSRF, đọc chéo tổ chức, sai lệch sổ cái tiền tệ hoặc vi phạm `CONSTRAINTS.md` (thêm `@ts-ignore`, `eslint-disable`, `t.Skip()` không lý do, hạ ngưỡng độ phủ) đều tự động chặn đứng tiến trình xét duyệt xuất xưởng.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`GATE-REL-01 & 02` Security & Fuzzing Clean Sheet):** Chạy trọn vẹn 48 ca `CT/AUTH`, quét tĩnh `gosec` + `semgrep` + `govulncheck` + `npm audit --omit=dev` đạt `0 Critical / 0 High`, và chạy chiến dịch `FZ-01..06` tổng cộng `>= 4 giờ` với `0` ca sập mới chưa được vá.
- **`AC-2` (`GATE-REL-03` Multi-Browser & Accessibility):** Toàn bộ `UJ-01`..`08` chạy xanh trên `Chromium`, `Firefox`, `WebKit` mà không cần retry che giấu lỗi chập chờn (`0 flaky retries`), `axe-core` đạt `0` lỗi `critical`/`serious` trên cả 16 màn hình `SCR-01`..`16` và `npx astryx doctor --built dist` đạt `0 fail`.
- **`AC-3` (`Contract N-1 Compatibility`):** Các gói `.tppkg` đóng gói từ `M1/M2` chạy kiểm định qua `tanpopo plugin migrate-check` và thực thi thành công trên nhân `v1.0 RC`.
- **`NEG-1`:** Kiểm tra tự động `CONSTRAINTS.md` quét toàn bộ Git diff xác nhận `0` câu lệnh bỏ qua kiểm tra an toàn hay hạ chuẩn độ phủ kiểm thử.

## 6. Test Commands & Required Evidence
- `TANPOPO_TEST_HERMETIC=1 go test -v -race ./tests/security/...`
- `bash scripts/qa/run-fuzz-campaign.sh --duration=4h`
- `cd web/console && npx playwright test --project=chromium --project=firefox --project=webkit && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Mọi lỗ hổng tìm thấy bởi fuzzer hoặc kiểm thử thâm nhập nội bộ đều phải được chuyển thành một hạt giống kiểm thử hồi quy vĩnh viễn trong `testdata/` trước khi đóng đơn vị này.
- **Rollback:** Chỉ bổ sung chốt chặn tương thích, bài kiểm thử và bản vá cứng hóa bảo mật.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0038`, `TP-IMP-0041` và `TP-IMP-0042` đã hoàn tất.
- **DoD:** Đạt `100%` tiêu chí `GATE-REL-01`, `GATE-REL-02`, `GATE-REL-03`, vượt qua `REQUIRES_STRONG_REVIEW` của cả `security-auditor` và `test-engineer`.
