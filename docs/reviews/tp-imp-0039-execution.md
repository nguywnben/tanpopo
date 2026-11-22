# `TP-IMP-0039` Execution & Verification Report — `PostgreSQL 16+` Driver (Monthly Partitioning, `FORCE RLS`), `Dual-DB` Parity & `migrate-engine`

> **Unit:** `TP-IMP-0039` (`M4` / `EP-08` · `REQUIRES_STRONG_REVIEW`)
> **Branch / Workflow:** `develop` -> PR [#29](https://github.com/nguywnben/tanpopo/pull/29) -> `main` (`61c310b59db1e2dc1296b543d0d10a25d150fe3d`, accepted `2026-11-21` under [`DEC-088`](../governance/decisions.md))
> **Post-Merge CI:** [`44903715757`](https://github.com/nguywnben/tanpopo/actions/runs/44903715757) (`Stage 0-1` `SUCCESS`) · [`44903715776`](https://github.com/nguywnben/tanpopo/actions/runs/44903715776) (`Release Artifact Dry Run` `SUCCESS`)
> **Author:** `Gemini 4 Hydrogen`
> **Independent Strong Reviewers:**
> - `code-reviewer` (`191385d0ba3ad84110a1f9c75ba13c8f`): **`APPROVE`** (Critical: 0, Required Changes: 0, Optional: 0, Nits: 0)
> - `security-auditor` (`781eb2de4a3369cd5f6df057dabc0da1`): **`PASS`** (Critical: 0, High: 0, Medium: 0, Low: 0)

## DoR Evidence

- Upstream dependencies `TP-IMP-0003`, `TP-IMP-0007`, `TP-IMP-0021`, and `TP-IMP-0023` are `DONE` on `origin/main` (`b6dc1d15d48890ad1b4b5f259b16e0fc7b9cd8bf`).
- Active branch is persistent `develop` synced with `origin/develop` and `origin/main`; approved Console v2 Astryx Neutral + 15-language UI baseline (`b6dc1d15`, PR [#28](https://github.com/nguywnben/tanpopo/pull/28)) is preserved intact.

## Implementation & Security Summary

- **PostgreSQL 16+ Schema Parity & Migrations ([`db/spec-check/postgres_schema.sql`](../../db/spec-check/postgres_schema.sql), [`db/migrations/postgres/`](../../db/migrations/postgres/0001_full_schema_rls_partitions.sql)):** Reconciled full table/column/check parity across all 55 canonical tables (including `marketplace_feed_cursors`, `marketplace_index_entries`, `extension_revocation_actions`, `mcp_servers.display_name`, `mcp_tool_grants` composite unique key, `debug_payload_blobs` 256 KiB cap, and `emergency_kill_switches.clear_reason` + `idx_killswitches_active_unique`).
- **`storage/port` & `storage/postgres` ([`internal/storage/port/port.go`](../../internal/storage/port/port.go), [`internal/storage/postgres/`](../../internal/storage/postgres/driver.go)):**
  - Implemented `pgx/v5` + `pgxpool` dual-mode store with `CheckNonPrivilegedRole` failing closed (`ErrPrivilegedRole`) on `SUPERUSER` or `BYPASSRLS`.
  - Added cluster-safe `RunMigrations` serialized under `pg_advisory_lock` with CRLF-normalized SHA-256 verification (`ErrDirtyMigration`).
  - Built `PartitionManager` (`EnsureMonth`, `EnsureLookahead(3)`, `EnsureTimestampCovered`, `DetachAndDropExpired` using `pgx.Identifier` sanitization and strict anchored suffixes, plus periodic background lookahead refresh stopped cleanly in `Close()`), propagating `ENABLE/FORCE ROW LEVEL SECURITY` + `tenant_isolation` policy onto every monthly child partition.
  - Implemented SQLite-parity `TenancyRepository`, `ChangelogRepository`, `LeaseRepository`, and `AuditSink` (`TxAuditSink` enforcement, canonical `AuditEntryPointAdminAPI` emission, auto/validated IDs, complementary GUC resets in `WithOrgTx`/`WithOperatorTx`/shared `port.Tx`). Hardened `RedactDSN` against malformed URLs with `@` in passwords, query `password`/`sslpassword`, and escaped keyword quotes.
- **Dual-DB Modules (`audit`, `finops/ledger`, `security/vault`)**:
  - [`internal/security/audit/`](../../internal/security/audit/logger.go): Per-chain `pg_advisory_xact_lock`, JCS re-canonicalization on `JSONB`, JSONB `->>` blast-radius filtering, `VerifyAuditChainTx`, and cross-org + `SYSTEM` aggregation for `AllowInstanceScope=true, OrgID=""`.
  - [`internal/finops/ledger/`](../../internal/finops/ledger/service.go): Dialect-aware row locks (`FOR UPDATE` / `FOR UPDATE SKIP LOCKED`), `pg_advisory_xact_lock` serialization for direct `Settle` (`ReservationID == ""`), `40001`/`40P01` retries, and retry-safe post-commit per-org `OrphanReservationReaper` audit flushing.
  - [`internal/security/vault/`](../../internal/security/vault/verify.go): Full `FORCE RLS` scoping for DEK/secret CRUD, rotation, shredding, `VerifyAllDEKEnvelopesTx`, and `VerifyAllSecretEnvelopesTx`.
- **`migrate-engine`, `doctor`, Fail-Closed `serve`, and CI (`internal/ops/migrate/`, `internal/cli/`, `cmd/tanpopo/serve.go`, `.github/workflows/ci.yml`)**:
  - Implemented atomic read-only SQLite WAL → PostgreSQL 16+ migration (`tanpopo db migrate-engine --from --to --verify`, localized across all 15 CLI locales) covering all 55 canonical tables, `a2a_tasks` parent-before-child ordering, per-org `usage_rollups_hourly` RLS parity, micro-USD sums, JCS audit hash chains, Vault trial unwrapping, and fail-closed rollback (`exit 22`).
  - Extended `tanpopo doctor` to verify PG role least privilege (`NOSUPERUSER`, `NOBYPASSRLS`) and fail on unrecognized `TANPOPO_STORAGE_ENGINE` values.
  - Enforced strict `TANPOPO_STORAGE_ENGINE` validation and fail-closed startup gate in `cmd/tanpopo/serve.go` when `postgres` runtime is selected prior to `TP-IMP-0040` full control/data-plane `FORCE RLS` cutover.
  - Scoped Stage 1 macOS SQLite storage coverage check to `./internal/storage/sqlite/... ./internal/storage/migrate/...` and added `stage2-dual-db-postgres` (`postgres:16-alpine`, `tanpopo_app` role, `-race`, `>= 85%` `internal/storage/postgres` coverage check, and all PG integration suites).

## Verification & Strong Review Evidence

- **PostgreSQL 16+ Storage & Coverage (`-count=1 -race`):** `internal/storage/postgres/...` passed with **`85.2%`** statement coverage (threshold `>= 85.0%`), proving `TestPostgresSchemaMigrationsAndPartitions`, `TestPostgresDualDBStoragePortParity`, `TestPostgresForceRLSZeroRowFailClosedAndCrossOrgIsolation`, `TestPostgresClusterConcurrentMigrationsFMEA06`, and `TestPostgresRefusesPrivilegedRoles`.
- **Dual-DB PG16 Suites (`-count=1 -race`):** `internal/finops/ledger/...` (`CT-29`, concurrent direct `Settle`, `OrphanReservationReaper`), `internal/security/audit/...` (`CT-30` 8 concurrent HA writers, `AllowInstanceScope` blast radius, real `*audit.Logger` as `postgres.Store` `AuditSink`), `internal/security/vault/...` (`TestVaultPostgresForceRLSAndFullLifecycle`), `internal/ops/migrate/...` (`CT-32`, `ExitContract 22` rollback), `cmd/tanpopo/...`, `internal/cli/support/...`, `internal/cli/ops/...`, and `internal/pkg/i18n/...` all passed cleanly.
- **Repo-Wide Gates:** `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go run ./scripts/ci/check.go --all` PASSED; `(cd tools/spikes && CGO_ENABLED=0 go test -v ./...)` PASSED; `CGO_ENABLED=0 go test -short ./...` PASSED across every Go package.
- **Independent Strong Review (`REQUIRES_STRONG_REVIEW`):**
  - `code-reviewer` (`191385d0ba3ad84110a1f9c75ba13c8f`): **`APPROVE`** (0 Critical, 0 Required, 0 Optional, 0 Nits).
  - `security-auditor` (`781eb2de4a3369cd5f6df057dabc0da1`): **`PASS`** (0 Critical, 0 High, 0 Medium, 0 Low).
