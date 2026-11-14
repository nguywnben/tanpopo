# `TP-IMP-0046` — Cổng Tài liệu Hướng dẫn 4 Hành trình, Kiểm tra Tự động Chống Lệch `OpenAPI/CLI`, `CHANGELOG.md` & Đánh bóng Kho Mã Nguồn Mở `Apache-2.0`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0046` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release`) · `EP-09` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0043`, `TP-IMP-0044`, `TP-IMP-0045` — `GAP-01`)* |
| **Quy mô (`Size`)** | `M` (`README.md`, `SECURITY.md`, `CONTRIBUTING.md`, `CHANGELOG.md`, `docs/guides/*`, `scripts/docs/check-drift.sh`) |
| **Cấp độ Review** | `STANDARD_REVIEW` *(Đảm bảo `100%` lệnh ví dụ trong tài liệu chạy thật không lỗi & tuân thủ tuyên ngôn pháp lý `DEC-004`)* |
| **Chủ sở hữu Kỹ thuật** | `Developer Relations & Documentation Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hoàn thiện bộ tài liệu hướng dẫn người dùng cuối, tác giả phần mở rộng và kỹ sư vận hành SRE cho bản phát hành mã nguồn mở `Apache-2.0` (`TP-NFR-0902`, `TP-NFR-1001`, `TP-NFR-1002`, `GATE-REL-04`, `GATE-REL-05`), kèm **cổng kiểm tra tự động chống trôi lệch giữa tài liệu và mã nguồn thực tế (`Zero Doc-to-Code Drift`)** để mọi ví dụ dòng lệnh, cấu hình YAML và lược đồ OpenAPI trong tài liệu đều được thực thi kiểm chứng tự động trong CI.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `README.md` gốc (giới thiệu súc tích Tanpopo, khởi chạy nhanh `< 3 phút`, kiến trúc 4 mặt phẳng, bảng cam kết tuân thủ `Official Commercial API / Self-Hosted Infra Only — DEC-004`), `SECURITY.md` (quy trình báo cáo lỗ hổng bảo mật có trách nhiệm), `CONTRIBUTING.md`, `CHANGELOG.md` và `NOTICE`.
  - `docs/guides/quickstart.md` (Hành trình 1: Người dùng ứng dụng & khởi chạy nhanh), `docs/guides/plugin-authoring-sdk.md` (Hành trình 2: Tự viết & kiểm thử băng ghi cho Provider Adapter trong `< 30 phút`), `docs/guides/production-ha-and-airgap.md` (Hành trình 3: Vận hành SRE, Sao lưu, DR, HA & Air-Gap), `docs/guides/security-and-mcp-governance.md` (Hành trình 4: Bảo mật kho khóa, OAuth 2.1, MCP & A2A).
  - `scripts/docs/check-drift.sh` (so khớp đầu ra `tanpopo --help` đệ quy với tài liệu CLI, kiểm định cú pháp `openapi/*.yaml`, kiểm tra toàn bộ liên kết Markdown nội bộ `0` đứt gãy và chạy thử các đoạn mã `Quickstart` trên máy chủ giả lập).
- **Out-of-Scope:** Bấm nút phát hành công khai (chỉ được phép thực hiện tại `TP-IMP-0047` sau khi Product Owner ký duyệt).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-NFR-0902`, `TP-NFR-1001`, `TP-NFR-1002`, `DEC-001`..`005`, `GATE-REL-04`, `GATE-REL-05`.
- **Dependencies:** `TP-IMP-0043`, `TP-IMP-0044`, `TP-IMP-0045`.
- **Design Refs:** [`developer-experience.md`](../../operations/developer-experience.md) (`SPEC-OPS-04`), [`release-readiness.md`](../../quality/release-readiness.md) (`GATE-REL-04`, `GATE-REL-05`).

## 4. API, Wire & Data Contracts
- **Tính Trung thực Của Số liệu & Tuân thủ Pháp lý:**
  1. Mọi con số độ trễ/thông lượng trích dẫn trong `README.md` và `docs/guides/*` phải dẫn thẳng tới kết quả đo thực nghiệm tại `docs/quality/benchmark-evidence.md` (`TP-IMP-0043`) kèm cấu hình phần cứng đo — **cấm viết khẩu hiệu quảng cáo phóng đại**.
  2. Tuyệt đối không có bất kỳ đoạn hướng dẫn, ví dụ hay gợi ý nào biến gói thuê bao cá nhân (`Plus/Pro/Max/Codex/Antigravity...`) thành API (`DEC-004`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`GATE-REL-05` Executable Docs & Zero Link Rot):** Chạy `bash scripts/docs/check-drift.sh` xác nhận `0` liên kết gãy trên toàn kho mã nguồn, `0` cờ dòng lệnh sai lệch giữa `tanpopo --help` và tài liệu, và kịch bản `Quickstart` chạy tự động từ đầu đến cuối thành công dưới `< 60 giây`.
- **`AC-2` (`GATE-REL-04` Clean Apache-2.0 Licensing):** Kiểm tra đồng bộ `LICENSE`, `NOTICE`, `package.json`, `web/console/package.json` và báo cáo quét giấy phép phụ thuộc của cả Go lẫn npm đạt `100%` tương thích `Apache-2.0`.
- **`NEG-1`:** Thử đổi tên một cờ CLI trong code mà không cập nhật tài liệu → `check-drift.sh` phát hiện và đánh trượt CI ngay lập tức.

## 6. Test Commands & Required Evidence
- `bash scripts/docs/check-drift.sh`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Quét toàn bộ tệp ví dụ trong `docs/guides/*` đảm bảo chỉ dùng khóa giả quy ước (`tpk_test_example...`, `sk-example-placeholder`), không bao giờ chứa khóa thật.
- **Rollback:** Các tệp tài liệu và kịch bản kiểm tra tĩnh dễ dàng cập nhật nguyên tử.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0043`, `TP-IMP-0044` và `TP-IMP-0045` đã hoàn tất.
- **DoD:** Đạt trọn vẹn `GATE-REL-04` và `GATE-REL-05`, sẵn sàng đóng hồ sơ trình duyệt cuối cùng tại `TP-IMP-0047`.
