# `TP-IMP-0021` — Two-Phase Integer `Micro-USD` Budget Ledger (`Pre-Hold`, 3 Accuracy Settlement Badges, `Carryover`, `OrphanReaper` & Rollups)

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0021` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-04` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0007`, `TP-IMP-0020` — `GAP-01, 06`)* |
| **Size** | `L` (`internal/finops/ledger/*`, `internal/finops/pricing/*`, `db/migrations/sqlite/0008_budget_ledger.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Zero-float monetary arithmetic, concurrent overspend prevention & orphan reservation reclamation)* |
| **Technical Owner** | `Principal FinOps / Storage Engineer` |

---

## 1. Intent
Implement the two-phase budget accounting ledger (`Two-Phase Reservation & Settlement Ledger` — `SM-07`, `ADR-0005`) using exclusively `int64` integer arithmetic (`nano-USD` intermediate token multiplication rounded deterministically up to `micro-USD` on ledger posting — **strictly banning `float32`/`float64` for monetary values**):
1. **Phase 1 (`RESERVE` prior to upstream egress):** Estimate worst-case ceiling cost from input tokens + multimodal units + `max_output_tokens` and atomically reserve across `Org`, `Project`, `Environment`, and `VirtualKey` scopes simultaneously; if capacity is insufficient → fail fast with canonical **`429 project_budget_exceeded`** (per `srs.md` & `CT-29`) before incurring upstream provider spend.
2. **Phase 2 (`SETTLE` or `RELEASE` at stream completion):** Append an immutable entry to `usage_settlements` stamped with 1 of 3 honest accuracy badges (`EXACT`, `ESTIMATED`, `UNMETERED_FLAGGED`), refund unused pre-hold headroom or record bounded negative carryover (`Negative Balance Carryover` — `TD-G2-03`), emit soft-threshold alerts (`ALT-BUDGET-80`, `ALT-UNMETERED-SPEND` linked to runbook `INC-01`), and run the background `OrphanReservationReaper` plus hourly rollups (`usage_rollups_hourly`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/finops/pricing/calculator.go` (overflow-checked `int64` multiplication across all 6 pricing dimensions: `input`, `cache_read`, `cache_write`, `output`, `reasoning`, and multimodal `media_usage_json` units per `CR-05, 06`; never fabricate `100%` precision when upstream usage is absent — stamp `ESTIMATED` or `UNMETERED_FLAGGED` per `FMEA-04`).
  - `internal/finops/ledger/service.go` (`Reserve`, `Settle`, `Release`, enforcing accounting invariant $\text{Limit} - \text{SettledSpend} - \text{ActiveReserved} \ge 0$ under serialized transactions).
  - `internal/finops/ledger/reaper.go` (`OrphanReservationReaper` periodically sweeping expired `lease_expires_at` holds to audited `EXPIRED_RECLAIMED` state — `SM-07`).
  - `internal/finops/rollup/aggregator.go` (`usage_rollups_hourly` aggregation maintaining separate `exact_micro_usd`, `estimated_micro_usd`, and `unmetered_request_count` columns).
  - `db/migrations/sqlite/0008_budget_ledger.sql` (`budget_ledgers`, `budget_reservations`, `usage_settlements` with `UNIQUE (request_id, settled_at)`, `usage_rollups_hourly` — `100%` aligned with `TP-IMP-0003` and `ADR-0003/0005`).
- **Out-of-Scope:** Batched `Valkey Lua` cluster hold optimization (owned by `TP-IMP-0040`); external commercial customer invoicing (`TP-FR-0804` — `Later / Post-v1.0`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0801`, `TP-FR-0802`, `TP-FR-0803`, `TP-BR-0701`..`0703`, `SM-07`, `CT-10`, `CT-12`, `CT-29`, `FZ-06`, `FMEA-04`, `INC-01`.
- **Dependencies:** `TP-IMP-0007`, `TP-IMP-0020`.
- **Design Refs:** [`ADR-0005`](../../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md), [`usage-billing-observability.md`](../../specifications/usage-billing-observability.md) (`SPEC-CORE-02`), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-04`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-29`, `FZ-06`).

## 4. API, Wire & Data Contracts
- **Arithmetic & Ledger Invariants (`FZ-06`):**
  - `1 USD = 1_000_000 micro_usd = 1_000_000_000 nano_usd`. Round ceiling via `ceil_div(nano_usd, 1000)` so sub-`micro-USD` fractions never under-account spend.
  - `usage_settlements` is strictly **`Append-Only`** — adjustments require compensating entries and never `UPDATE` or `DELETE` settled rows.
  - Whenever `TP-IMP-0020` fails over pre-stream from a lower-cost primary target to a higher-cost fallback target, the incremental `DeltaReserve` hold must succeed prior to dispatching the fallback request.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`CT-29` Concurrent Race Under Tight Budget):** Given remaining budget of exactly `10,000 micro-USD` (\$0.01) where each request requires a `2,500 micro-USD` pre-hold, When **50 goroutines** fire concurrently, Then **at most 4 requests** proceed and at least **46 requests** fail fast with **`429 project_budget_exceeded`** (aggregate reserved + settled spend never exceeds `10,000 micro-USD`).
- **`AC-2` (`FMEA-04` & `INC-01` Missing Upstream Usage & Unpriced Custom Model):**
  1. When an SSE stream aborts mid-stream or omits a terminal `usage` chunk (`CT-12`) → the ledger settles emitted output tokens stamped `accuracy = 'ESTIMATED'`.
  2. When invoking a self-hosted model lacking configured pricing rates → settles `0 micro-USD` stamped `accuracy = 'UNMETERED_FLAGGED'` and increments `ALT-UNMETERED-SPEND`.
- **`AC-3` (`Negative Carryover`):** If a legitimate stream slightly exceeds its pre-hold estimate bringing total spend to `10,050 / 10,000 micro-USD`, the active stream completes cleanly, records `-50 micro-USD` available balance, and immediately blocks the next request with `429 project_budget_exceeded`.
- **`NEG-1`:** AST check across `internal/finops/...` verifies `0` monetary calculations use `float32` / `float64`.
- **`NEG-2` (`Double Settlement / Double Release`):** Calling `Settle(reservationID)` twice for the same reservation ID → second call is an idempotent no-op, never double-charging the ledger.

## 6. Test Commands & Required Evidence
- Pure-Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/finops/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/finops/...`)*
- Data-race (`-race`) & ledger-invariant property fuzzing (`FZ-06`):
  - `go test -v -race ./internal/finops/...`
  - `go test -fuzz=FuzzLedgerInvariants -fuzztime=30s ./internal/finops/ledger/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Strictly partition budgets by `(org_id, scope_type, scope_id)`; SQLite/PostgreSQL triggers reject `DELETE` and `UPDATE` statements on `usage_settlements`.
- **Rollback:** Additive schema `0008_budget_ledger.sql`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (`SM-07` remediation), `TP-IMP-0003` (`CR-06` ledger DDL lock), `TP-IMP-0007`, and `TP-IMP-0020` are complete.
- **DoD:** Passes the 50-goroutine concurrent race test `CT-29`, `FZ-06` property fuzzing, and `FMEA-04`; completes `REQUIRES_STRONG_REVIEW`.
