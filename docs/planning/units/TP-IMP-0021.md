# `TP-IMP-0021` — Sổ cái Ngân sách Hai Pha Số nguyên `Micro-USD` (`Pre-Hold`, Quyết toán 3 Nhãn Độ chính xác, `Carryover`, `OrphanReaper` & Rollups)

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0021` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-04` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0007`, `TP-IMP-0020` — `GAP-01, 06`)* |
| **Quy mô (`Size`)** | `L` (`internal/finops/ledger/*`, `internal/finops/pricing/*`, `db/migrations/sqlite/0008_budget_ledger.sql`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Toán học tiền tệ chính xác không dấu phẩy động, chống vượt trần ngân sách khi bắn song song & dọn giữ chỗ mồ côi)* |
| **Chủ sở hữu Kỹ thuật** | `Principal FinOps / Storage Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hiện thực hóa sổ cái kế toán ngân sách hai pha (`Two-Phase Reservation & Settlement Ledger` — `SM-07`, `ADR-0005`) hoàn toàn bằng số nguyên `int64` (`nano-USD` khi nhân đơn giá token, làm tròn chuẩn lên `micro-USD` khi ghi sổ — **cấm tuyệt đối kiểu `float32`/`float64` cho tiền tệ**):
1. **Pha 1 (`RESERVE` trước khi gọi nhà cung cấp):** Ước tính chi phí trần tối đa từ token đầu vào + đơn vị đa phương thức + `max_output_tokens`, kiểm tra nguyên tử đồng thời trên cả ngân sách `Org`, `Project`, `Environment` và `VirtualKey`; nếu thiếu hạn mức → chặn ngay chuẩn **`429 project_budget_exceeded`** (tương ứng mã phạm vi hết hạn mức trong `srs.md` & `CT-29`) trước khi tốn tiền.
2. **Pha 2 (`SETTLE` hoặc `RELEASE` ở cuối luồng):** Ghi bút toán bất biến vào `usage_settlements` với 1 trong 3 nhãn độ chính xác trung thực (`EXACT`, `ESTIMATED`, `UNMETERED_FLAGGED`), hoàn trả phần giữ chỗ dư thừa hoặc ghi nhận số dư âm kết chuyển nhẹ (`Negative Balance Carryover` — `TD-G2-03`), phát cảnh báo ngưỡng mềm (`ALT-BUDGET-80`, `ALT-UNMETERED-SPEND` gắn sổ tay `INC-01`), kèm tác vụ nền `OrphanReservationReaper` và bảng tổng hợp theo giờ (`usage_rollups_hourly`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/finops/pricing/calculator.go` (nhân số nguyên `int64` có kiểm tra tràn số học cho đủ 6 thành phần đơn giá: `input`, `cache_read`, `cache_write`, `output`, `reasoning` và đơn vị đa phương thức `media_usage_json` theo `CR-05, 06`; không bao giờ hứa hẹn tính đúng `100%` khi thiếu số đo — gán `ESTIMATED` hoặc `UNMETERED_FLAGGED` theo `FMEA-04`).
  - `internal/finops/ledger/service.go` (`Reserve`, `Settle`, `Release`, kiểm tra bất biến kế toán $\text{Limit} - \text{SettledSpend} - \text{ActiveReserved} \ge 0$ dưới giao dịch tuần tự an toàn).
  - `internal/finops/ledger/reaper.go` (`OrphanReservationReaper` quét định kỳ các bản ghi quá hạn `lease_expires_at` sang `EXPIRED_RECLAIMED` có kiểm toán — `SM-07`).
  - `internal/finops/rollup/aggregator.go` (tổng hợp vào `usage_rollups_hourly` tách riêng cột `exact_micro_usd`, `estimated_micro_usd` và `unmetered_request_count`).
  - `db/migrations/sqlite/0008_budget_ledger.sql` (`budget_ledgers`, `budget_reservations`, `usage_settlements` kèm `UNIQUE (request_id, settled_at)`, `usage_rollups_hourly` — khớp 100% tên bảng tại `TP-IMP-0003` và `ADR-0003/0005`).
- **Out-of-Scope:** Tối ưu hóa giữ chỗ theo lô trên `Valkey Lua` cho cụm đa nút (thuộc `TP-IMP-0040`); xuất hóa đơn thu tiền khách hàng bên ngoài (`TP-FR-0804` — `Later / Post-v1.0`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0801`, `TP-FR-0802`, `TP-FR-0803`, `TP-BR-0701`..`0703`, `SM-07`, `CT-10`, `CT-12`, `CT-29`, `FZ-06`, `FMEA-04`, `INC-01`.
- **Dependencies:** `TP-IMP-0007`, `TP-IMP-0020`.
- **Design Refs:** [`ADR-0005`](../../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md), [`usage-billing-observability.md`](../../specifications/usage-billing-observability.md) (`SPEC-CORE-02`), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-04`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-29`, `FZ-06`).

## 4. API, Wire & Data Contracts
- **Bất biến Số học & Kế toán (`FZ-06`):**
  - `1 USD = 1_000_000 micro_usd = 1_000_000_000 nano_usd`. Làm tròn lên trần `ceil_div(nano_usd, 1000)` để không bao giờ thất thoát phần lẻ dưới `1 micro-USD`.
  - Mọi thao tác trên `usage_settlements` là **chỉ ghi nối thêm (`Append-Only`)** — việc điều chỉnh phải tạo bút toán đối ứng mới, không bao giờ `UPDATE` hay `DELETE` dòng đã chốt.
  - Khi chuyển đổi dự phòng ở `TP-IMP-0020` từ mô hình rẻ sang mô hình dự phòng đắt hơn, chênh lệch chi phí giữ chỗ tăng thêm (`DeltaReserve`) phải được giữ chỗ thành công trước khi gọi đích dự phòng!

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`CT-29` Concurrent Race Under Tight Budget):** Given ngân sách còn đúng `10.000 micro-USD` (\$0.01) và mỗi yêu cầu cần giữ chỗ `2.500 micro-USD`, When bắn đồng thời **50 goroutines** cùng lúc, Then đúng **tối đa 4 yêu cầu** được phép đi tiếp và ít nhất **46 yêu cầu** bị từ chối tức khắc **`429 project_budget_exceeded`** (tổng chi giữ chỗ không bao giờ vượt `10.000 micro-USD`).
- **`AC-2` (`FMEA-04` & `INC-01` Missing Upstream Usage & Unpriced Custom Model):**
  1. Khi luồng SSE bị ngắt giữa chừng hoặc thiếu khung `usage` cuối (`CT-12`) → sổ cái quyết toán phần chữ đã phát với `accuracy = 'ESTIMATED'`.
  2. Khi gọi mô hình tự host chưa khai báo bảng giá → quyết toán `0 micro-USD` nhưng bắt buộc đóng dấu `accuracy = 'UNMETERED_FLAGGED'` và tăng cảnh báo `ALT-UNMETERED-SPEND`.
- **`AC-3` (`Negative Carryover`):** Nếu một luồng hợp lệ sinh vượt nhẹ mức ước tính khiến tổng chi đạt `10.050 / 10.000 micro-USD`, luồng đó vẫn hoàn tất êm ái, số dư ghi `-50 micro-USD` và yêu cầu tiếp theo bị chặn `429 project_budget_exceeded` ngay lập tức.
- **`NEG-1`:** Quét AST trên toàn bộ gói `internal/finops/...` đảm bảo `0` phép tính tiền dùng kiểu `float32` / `float64`.
- **`NEG-2` (`Double Settlement / Double Release`):** Gọi `Settle(reservationID)` hai lần cho cùng một mã giữ chỗ → lần thứ hai là thao tác bất biến (`idempotent no-op`), không bao giờ trừ tiền hai lần.

## 6. Test Commands & Required Evidence
- Thuần Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/finops/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/finops/...`)*
- Tranh chấp dữ liệu (`-race`) & Fuzzing bất biến kế toán (`FZ-06`):
  - `go test -v -race ./internal/finops/...`
  - `go test -fuzz=FuzzLedgerInvariants -fuzztime=30s ./internal/finops/ledger/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Tách biệt hoàn toàn ngân sách theo `(org_id, scope_type, scope_id)`; trigger SQLite/Postgres từ chối lệnh `DELETE` và `UPDATE` trên bảng `usage_settlements`.
- **Rollback:** Bảng cộng thêm `0008_budget_ledger.sql`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (vá `SM-07`), `TP-IMP-0003` (khóa lược đồ sổ cái `CR-06`), `TP-IMP-0007` và `TP-IMP-0020` đã hoàn tất.
- **DoD:** Vượt qua bài kiểm thử đua đồng thời 50 luồng `CT-29`, kiểm thử thuộc tính `FZ-06`, `FMEA-04` và đạt `REQUIRES_STRONG_REVIEW`.
