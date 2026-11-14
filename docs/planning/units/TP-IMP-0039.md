# `TP-IMP-0039` — `PostgreSQL 16+` Driver (Monthly Partitioning, Composite `(org_id, id)` Keys, `FORCE RLS`), `Dual-DB` Parity Suite & `SQLite -> Postgres` Migration

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0039` |
| **Milestone / Epic** | `M4` (`v0.3 Clustered HA, Enterprise Security & A2A`) · `EP-08` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0007`, `TP-IMP-0021`, `TP-IMP-0023` — `GAP-01, 04, 05`)* |
| **Size** | `L` (`go.mod`, `internal/storage/postgres/*`, `db/migrations/postgres/*`, `internal/ops/migrate/sqlite_to_pg.go`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Database-engine multi-tenant isolation via `FORCE ROW LEVEL SECURITY`, concurrent HA audit hash-chain serialization & lossless `CT-32` migration)* |
| **Technical Owner** | `Principal Database & Storage Architect` |

---

## 1. Intent
Extend the `storage/port` persistence abstraction to **`PostgreSQL 16+`** (`pgx/v5`) for high-availability clustered deployments (`TP-FR-1005`, `ADR-0003`, `ARCH-DATA-01`, `CR-06`):
1. **Engine-Level Multi-Tenant Hard Isolation (`ENABLE + FORCE ROW LEVEL SECURITY`):** The application connects under a non-`BYPASSRLS` database role; every transaction MUST set `SET LOCAL app.current_org_id = '<uuid>'` (if omitted, RLS policies fail closed and return `0` rows rather than exposing table contents).
2. **Automated Monthly Partitioning & Composite `(org_id, id)` Keys:** Partition high-volume append tables (`usage_settlements`, `request_traces_meta`, `audit_events`) via monthly `RANGE (created_at)` partitions with 3-month lookahead pre-creation, and serialize per-`chain_key` (`org_id` or `"SYSTEM"`, via `pg_advisory_xact_lock`) `audit_events` hash-chain appends across concurrent HA nodes (`CR-06`).
3. **Symmetric `Dual-DB Contract Suite` & Canonical `tanpopo db migrate-engine` Tool (`SQLite -> PostgreSQL` — `CT-32`, `DR-02`):** Execute the identical storage contract test suite against both `SQLite WAL` and `PostgreSQL 16+` to prove 100% behavioral parity, and ship `tanpopo db migrate-engine` with end-to-end cryptographic & financial verification (`row counts`, `sum(settled_micro_usd)`, `VerifyAuditChain`, and `vault` trial decryption).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (adding pure-Go `github.com/jackc/pgx/v5`).
  - `internal/storage/postgres/driver.go` (`pgxpool` connection pool, mandatory `SET LOCAL app.current_org_id` acquisition hook, and cluster-wide `pg_advisory_lock` migration runner preventing DDL races when 10 pods boot simultaneously — `FMEA-06`).
  - `db/migrations/postgres/0001_full_schema_rls_partitions.sql` (all `26` canonical tables locked in `TP-IMP-0003`, `FORCE RLS`, composite `(org_id, ...)` foreign keys, and triggers blocking `UPDATE/DELETE` on `usage_settlements` & `audit_events`).
  - `internal/storage/postgres/partition_manager.go` (automated pre-creation of upcoming monthly partitions and detachment of expired partitions for archival).
  - `internal/ops/migrate/sqlite_to_pg.go` (`tanpopo db migrate-engine --from sqlite://... --to postgres://... --verify`).
- **Out-of-Scope:** Distributed rate-limit counters and fast revocation fan-out over `Valkey` (covered in `TP-IMP-0040`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-1005`, `ADR-0003`, `ARCH-DATA-01`, `CR-06`, `CT-32`, `AUTH-14`, `FMEA-06`, `DR-02`, `GATE-REL-02`.
- **Dependencies:** `TP-IMP-0007`, `TP-IMP-0021`, `TP-IMP-0023`.
- **Design Refs:** [`ADR-0003`](../../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md), [`data-model.md`](../../architecture/data-model.md) (`ARCH-DATA-01`), [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) Section 2, [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-06`).

## 4. API, Wire & Data Contracts
- **HA Per-Org Audit Hash-Chain Serialization Invariant (`HA Per-Org Audit Serialization`):** When 4 Control Plane nodes concurrently append `audit_events` for `Org A`, each transaction acquires `pg_advisory_xact_lock(hashtextextended('audit:' || org_id::text, 0))` for milliseconds to guarantee strictly linear `seq_no` and `prev_hash` chaining for `Org A` without blocking `Org B`.
- **Lossless Migration Verification (`CT-32`):** If any table differs by even 1 row, diverges by `1 micro-USD` in ledger totals, or fails audit hash-chain verification after copying from SQLite to PostgreSQL, `tanpopo db migrate-engine` rolls back the target state and exits with code `22`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`Live PostgreSQL 16 DDL Execution & Dual-DB Behavioral Parity`):** Both [`db/spec-check/postgres_schema.sql`](../../../db/spec-check/postgres_schema.sql) and `db/migrations/postgres/0001_full_schema_rls_partitions.sql` parse and execute cleanly with **`0` DDL errors on a live `PostgreSQL 16` engine** (closing the `UNVERIFIED` live-PG validation item from `M0`), verifying monthly `RANGE` partition routing, immutability triggers, and `100%` pass rate of the `storage/port`, `finops/ledger` (`CT-29`), and `security/audit` (`CT-30` under 8 concurrent writer nodes) contract suites on both `SQLite WAL` and `PostgreSQL 16`.
- **`AC-2` (`CT-32` Lossless SQLite-to-PostgreSQL Migration):** Seeding multi-project fixtures, `AEAD AAD` secrets, 1,000 `usage_settlements` entries, and 500 audit events in SQLite and running `tanpopo db migrate-engine --verify` into PostgreSQL yields `100%` exact parity across row counts, `micro-USD` totals, `SecretCredential` decryption, and `VerifyAuditChain`.
- **`NEG-1` (`RLS Enforcement Proof`):** In an adversarial test executing raw `SELECT * FROM secret_credentials` over the application connection pool without setting `app.current_org_id` (or with `Org A` set while querying `Org B` rows), PostgreSQL RLS returns **strictly `0` rows**.
- **`NEG-2` (`FMEA-06` Concurrent Pod Migration):** Launching 8 concurrent migration processes against an empty database results in exactly 1 process applying DDL while the remaining 7 wait cleanly on the advisory lock with zero table-contention errors.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/storage/postgres/... ./internal/ops/migrate/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/storage/postgres/... ./internal/ops/migrate/...`; run against a local PostgreSQL 16 instance in a network-isolated container)*
- Race detector (`-race`): `TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/storage/postgres/... ./internal/ops/migrate/...` *(PowerShell: `$env:TANPOPO_TEST_HERMETIC='1'; go test -v -race ./internal/storage/postgres/... ./internal/ops/migrate/...`)*

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Production startup refuses to proceed if the configured PostgreSQL role possesses `SUPERUSER` or `BYPASSRLS` privileges (`tanpopo doctor` queries `pg_roles` directly).
- **Rollback:** The source SQLite database is opened strictly read-only during PostgreSQL migration, remaining 100% intact for immediate rollback if required.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` (completing the 8 missing table DDLs + partition key alignment per `CR-06`), `TP-IMP-0007`, `TP-IMP-0021`, and `TP-IMP-0023` are complete.
- **DoD:** `Dual-DB` parity tests, `CT-32`, `FMEA-06`, and `FORCE RLS` proofs passing, with `REQUIRES_STRONG_REVIEW` sign-off achieved.
