# `TP-IMP-0007` — `storage/port` Interfaces, Pure-Go `SQLite WAL` Driver (`modernc.org/sqlite`), *Expand-Only* Migration Runner & Root Multi-Tenant Tables

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0007` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Status** | **`READY`** *(Unlocked `2026-11-14` via `DEC-011`; upstream `TP-IMP-0003` and `TP-IMP-0005` are `DONE`)* |
| **Size** | `M` (`go.mod`, `internal/pkg/idgen`, `internal/storage/*`, `db/migrations/sqlite/0001_foundation.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Establishes root multi-tenant isolation invariants & schema migration mechanics)* |
| **Technical Owner** | `Staff Backend / Storage Engineer` |

---

## 1. Intent
Implement the dialect-neutral `internal/storage/port` persistence interfaces and embedded pure-Go `SQLite WAL` driver powered by **`modernc.org/sqlite` (`CGO_ENABLED=0`)** alongside a checksum-verified migration runner per [`data-model.md`](../../architecture/data-model.md) and [`ADR-0003`](../../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md), bootstrapping the foundational schema (`db/migrations/sqlite/0001_foundation.sql`) for `organizations`, `projects`, `environments`, `config_changelog`, and `distributed_leases` plus a time-ordered Crockford Base32 `UUIDv7` generator (`<prefix>_<26_chars>`, total `<= 32` characters fitting cleanly inside `VARCHAR(40)`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (add vetted pure-Go dependency `modernc.org/sqlite`).
  - `internal/pkg/idgen/` (generate 128-bit `UUIDv7` from `crypto/rand` + `epoch_ms`, encoded as 26 lowercase Crockford Base32 characters with canonical system-wide prefixes: `org_`, `prj_`, `env_`, `prc_`, `vky_`, `sec_`, `ext_`, `exv_`, `ins_`, `adp_`, `con_`, `lmd_`, `dpl_`, `pol_`, `rte_`, `bdg_`, `rsv_`, `set_`, `trc_`, `aud_`, `ksw_`, `mcp_`, `apr_`, `whk_`, `agt_`, `tsk_`).
  - `internal/storage/port/` (`Tx`, `TenancyRepository`, `ChangelogRepository`, `AuditSink` interfaces, and canonical domain errors `ErrNotFound`, `ErrVersionConflict`, `ErrResourceInUse`).
  - `internal/storage/migrate/` (sequential SQL migration runner maintaining `schema_migrations`, verifying SHA-256 checksums, and halting on `dirty` state with exit code `31` per `FMEA-06`).
  - `internal/storage/sqlite/` (`modernc.org/sqlite` connection pool configured with `_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)`, rejecting network filesystems NFS/SMB, and exposing `VACUUM INTO` for hot online backups) & `db/migrations/sqlite/0001_foundation.sql`.
- **Out-of-Scope:** `PostgreSQL 16+` driver (owned by `TP-IMP-0039`); secret vault tables (owned by `TP-IMP-0008`); budget ledger tables (owned by `TP-IMP-0021`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0501`, `TP-FR-0901` (`version` column), `TP-FR-1001`, `TP-NFR-0103`, `SLO-05`, `TP-BR-0101`, `RSK-013`, `FMEA-06`.
- **Dependencies:** `TP-IMP-0003`, `TP-IMP-0005`.
- **Design Refs:** [`data-model.md`](../../architecture/data-model.md) Sections 1 & 3.1, [`ADR-0003`](../../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-06`).

## 4. API, Wire & Data Contracts
- **Hard Portability Rules:** Persist all timestamps as UTC `BIGINT epoch_ms`; enforce composite `(org_id, id)` primary/foreign keys on `projects` and `environments`; every mutating repository call accepts `expectedVersion int64` and executes `WHERE org_id = ? AND id = ? AND version = ?` (returning `port.ErrVersionConflict` → HTTP `409` when `rows == 0`).
- **Atomic `config_epoch` Advancement via the Single `config_changelog` Table:** Every configuration-mutating transaction atomically appends one row to `config_changelog` to notify `ConfigSnapshot` watchers (the single canonical configuration changelog table across the system).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given an empty data directory, When opening `sqlite.Open` with `CGO_ENABLED=0`, Then `journal_mode=wal` and `foreign_keys=1` activate cleanly, `0001_foundation.sql` applies in `< 100 ms`, and `config_epoch` increments in `config_changelog` upon creating `Org -> Project -> Env`.
- **`AC-2`:** Given 10,000 IDs minted by `idgen.New("con")`, When checking length and lexicographic order, Then every ID is exactly `30` characters (`<= 40`), `100%` collision-free, and monotonically ordered by creation timestamp.
- **`AC-3`:** Given a `Project` row at `version = 1`, When two goroutines concurrently invoke `UpdateProject(..., expectedVersion=1)`, Then exactly 1 succeeds (`version = 2`) and the other receives `port.ErrVersionConflict`; calling `GetProject(ctx, orgA, projectIDOfOrgB)` uniformly returns `port.ErrNotFound` (`0` cross-org leakage).
- **`NEG-1`:** Attempting to insert an `Environment` with `org_id = Org_A` referencing a `project_id` owned by `Org_B` → immediately rejected at the DB layer by SQLite's composite `(org_id, id)` foreign key constraint.
- **`NEG-2`:** Mutating a single byte of an already-applied migration file or marking `dirty = true` → startup refuses to bind any listening port and exits with code `31` (`FMEA-06`).
- **`NEG-3`:** Zero `FLOAT`/`REAL` columns or vendor-specific SQL functions outside `internal/storage/sqlite`.

## 6. Test Commands & Required Evidence
- Pure-Go verification (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/pkg/idgen/... ./internal/storage/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/pkg/idgen/... ./internal/storage/...`
- Data-race verification (`-race`, `CGO_ENABLED=1`): `go test -v -race ./internal/pkg/idgen/... ./internal/storage/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Mandate `org_id` as the first domain parameter on every `TenancyRepository` method (never expose a `GetProjectByIDOnly(id)` method).
- **Rollback:** All migrations adhere strictly to *Expand-Only* discipline, verified by crash-reopen tests on `.db` files.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` and `TP-IMP-0005` are complete.
- **DoD:** Achieves `>= 85%` test coverage across `internal/storage/...` (under both `CGO_ENABLED=0` and `-race`); passes `REQUIRES_STRONG_REVIEW`.
