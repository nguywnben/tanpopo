# `TP-IMP-0043` — Bộ Đo tải `k6` (`1.000` Luồng SSE), Xác thực Thực nghiệm `SLO-01..08` & Diễn tập Bấm giờ Hỗn loạn `FMEA-01..08` / Khôi phục `DR-01..04`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0043` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release`) · `EP-09` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0040`, `TP-IMP-0042` — `GAP-01`)* |
| **Quy mô (`Size`)** | `L` (`tests/perf/k6/*`, `tests/chaos/*`, `docs/quality/benchmark-evidence.md`) |
| **Cấp độ Review** | `STANDARD_REVIEW` *(Thay thế toàn bộ nhãn `TARGET / UNVERIFIED` bằng số liệu đo thực nghiệm trung thực trên phần cứng chuẩn)* |
| **Chủ sở hữu Kỹ thuật** | `Principal Performance & Chaos SRE Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Thực thi chiến dịch kiểm chứng tải cao, độ trễ đuôi, xả tải quá ngưỡng và diễn tập hỗn loạn/thảm họa có bấm giờ trên môi trường chuẩn hóa (`0` gọi dịch vụ AI thật — dùng cụm `FakeLLMServer` độ trễ khả trình) theo [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`SPEC-QA-03`) và [`backups-recovery.md`](../../operations/backups-recovery.md) (`SPEC-OPS-02`), nhằm **biến toàn bộ các con số `SLO-01`..`08` đang mang nhãn `TARGET / UNVERIFIED` thành bằng chứng đo đạc thực tế có thể tái lập** trước khi xét duyệt `v1.0 GA`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `tests/perf/k6/sse_1000_streams.js`, `declar_vs_wasm_overhead.js`, `overload_shedding_503.js`, `rolling_restart_zero_drop.js` (`OPS-AC-06` đo khởi động lại cuốn chiếu cụm 3 nút dưới `100` luồng SSE không rớt gói).
  - `tests/chaos/fmea_suite_test.go` (tự động hóa tái hiện và xác minh cả **8 ca hỏng hóc sâu**: `FMEA-01`, `FMEA-02`, `FMEA-03`, `FMEA-04`, `FMEA-05`, `FMEA-06`, `FMEA-07`, `FMEA-08`).
  - `tests/chaos/dr_drills_test.go` (bấm giờ thực tế cả **4 kịch bản phục hồi thảm họa**: `DR-01` SQLite hỏng đĩa, `DR-02` PostgreSQL PITR + `tanpopo admin verify-state`, `DR-03` xoay khẩn cấp `Master KEK` & xóa sạch cache RAM, `DR-04` quay lui nâng cấp lỗi `< 60 giây` — xác nhận `RTO < 15 phút` `SLO-08`).
  - `docs/quality/benchmark-evidence.md` (ghi lại kết quả đo thật cho đủ `SLO-01`, `SLO-02`, `SLO-03`, `SLO-04`, `SLO-05`, `SLO-06`, `SLO-07`, `SLO-08` — không làm đẹp số liệu!).
- **Out-of-Scope:** Kiểm thử xâm nhập bảo mật hộp xám & fuzzing dài hạn (thuộc đơn vị song song `TP-IMP-0044`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-NFR-0101`..`0103`, `TP-NFR-0201`, `TP-NFR-0202`, `TP-NFR-0301`, `TP-NFR-0302`, `TP-NFR-0601`, `SLO-01`..`08`, `FMEA-01`..`08`, `DR-01`..`04`, `OPS-AC-01`..`12`, `GATE-REL-03`, `GATE-REL-06`.
- **Dependencies:** `TP-IMP-0040`, `TP-IMP-0042`.
- **Design Refs:** [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`SPEC-QA-03`), [`backups-recovery.md`](../../operations/backups-recovery.md) (`SPEC-OPS-02`), [`monitoring-incidents.md`](../../operations/monitoring-incidents.md) (`SPEC-OPS-03`).

## 4. API, Wire & Data Contracts
- **Bất biến Không Rò rỉ Tài nguyên Dưới Tải Dài Hạn (`Soak Stability`):** Sau bài chạy ngâm tải liên tục, số lượng goroutine và bộ nhớ heap sau `runtime.GC()` phải trở về sát mức nền ban đầu (độ lệch tăng trưởng tuyến tính bằng `0`), `0` mô tả tệp (`file descriptor`) bị rò rỉ.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`SLO-01..08` Empirical Verification on Strict `2 vCPU / 2 GiB RAM` Floor):** Đạt đủ các ngưỡng mục tiêu trên đúng cấu hình máy chuẩn **`2 vCPU / 2 GiB RAM`** cho nút đơn (theo `srs.md` `TP-NFR-0301` & `nonfunctional-slo.md` — không tự ý nâng gấp đôi RAM để né giới hạn!) và cụm 3 nút cho HA, kèm tệp JSON kết quả thô của `k6` và hồ sơ `pprof`.
- **`AC-2` (`FMEA-01..08`, `DR-01..04` & `OPS-AC-06` Pass):** Cả 8 bài kiểm thử hỗn loạn FMEA, 4 bài diễn tập khôi phục DR (`RTO < 15 phút`, `DR-04` quay lui `< 60 giây`) và bài khởi động lại cuốn chiếu 3 nút (`OPS-AC-06` `0` lỗi `502/503`) đều vượt qua tự động với `0` sai lệch sổ cái tiền tệ.
- **`NEG-1` (`3x Overload Shedding`):** Bắn tải gấp `300%` ngưỡng giới hạn hàng đợi → tiến trình Gateway giữ vững RSS trong trần cho phép, trả `503` sạch sẽ dưới `< 5 ms` và tự phục hồi tức thì ngay khi tải hạ nhiệt (không sập `panic` hay treo chết).

## 6. Test Commands & Required Evidence
- `TANPOPO_TEST_HERMETIC=1 go test -v -race -timeout=30m ./tests/chaos/...`
- `k6 run tests/perf/k6/sse_1000_streams.js`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Toàn bộ kịch bản `k6` và `chaos` chạy hoàn toàn trong mạng cục bộ cô lập với `FakeLLMServer` — `0` nguy cơ bắn tải nhầm ra Internet.
- **Rollback:** Chỉ thêm bộ kiểm thử tải/hỗn loạn và báo cáo đo đạc thực nghiệm.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0040` và `TP-IMP-0042` đã hoàn tất.
- **DoD:** Hoàn thành `docs/quality/benchmark-evidence.md` với số liệu đo thật `100%`, xanh trọn bộ 8 FMEA + 4 DR + k6 suite.
