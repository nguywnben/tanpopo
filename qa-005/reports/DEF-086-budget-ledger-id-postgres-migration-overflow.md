# DEF-086 — SQLite budget ledger IDs fail PostgreSQL migration width

**Severity:** Major. **QA-005 status:** FAIL (fresh exact-RC7 migration reproduction). **Checkpoint:** CP117. **Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`.

## Finding

A real exact-RC7 Owner Console budget-create journey on SQLite returns and persists project/organization identifiers of length 50 and 55 characters. The frozen SQLite and PostgreSQL declarations both say `VARCHAR(40)`, but SQLite does not enforce that maximum. The exact RC7 `db migrate-engine --verify` COPY fails when PostgreSQL enforces the width, aborting the documented storage promotion for a source containing valid budget rows.

## Reproduction and evidence

1. Use the real Owner Console FinOps workflow on exact RC7 SQLite to create one project monthly budget and one organization monthly budget. CP35 shows both create calls returned HTTP 200 and persisted after refresh and scope reselection.
2. Read back IDs `bgt_project_prj_01m8ct3xs7e00bv1hwq3qz2est_monthly` (50 characters) and `bgt_organization_org_01m8crmjtje009gbep9v278fpe_monthly` (55 characters).
3. Run exact binary `tanpopo db migrate-engine --verify` from the isolated SQLite clone into an empty PostgreSQL 16 DB as a verified non-superuser, non-BYPASSRLS role.
4. Observe failure copying `budget_ledgers`: `value too long for type character varying(40)`.
5. Query the target after failure: 0 organizations, 0 budget ledgers, 0 usage settlements and 0 audit events. The row-copy transaction rolled back.

Records: [CP117](../checkpoints/CP117-finops-postgres-migration-budget-id-overflow.md), [sanitized migration evidence](../evidence/CP117-finops-postgres-migration.json), and the independent real Console source journey [CP35](../checkpoints/CP35-first-budget-and-parent-visibility.md) / [CP35 JSON](../evidence/CP35-budget-lifecycle.json). The exact PostgreSQL DDL is `db/migrations/postgres/0001_full_schema_rls_partitions.sql`; the SQLite declaration is `db/migrations/sqlite/0012_budget_ledger.sql`.

## Expected vs actual

- **Expected:** CT-32 / TP-IMP-0039 AC-2 migrates every valid budget row without identity/data loss and verifies table counts, micro-USD values, Vault envelopes and audit chains.
- **Actual:** valid RC7 Console-generated IDs exceed the enforced PostgreSQL column width, so migration returns a database error and no application data is copied.

## Impact and severity

Organizations with existing budget rows cannot promote the documented SQLite database to PostgreSQL using the shipped migration command. This blocks HA scale-up and can delay availability/recovery. The original SQLite remains unchanged; this is a promotion blocker, not data loss.

## Facts vs hypothesis

**Confirmed:** Console/API created and persisted 50/55-character identifiers in exact-RC7 SQLite; PostgreSQL declares `budget_ledgers.id VARCHAR(40)`; the verified migration fails at that column; rollback leaves target application tables empty.

**Root-cause hypothesis:** the PostgreSQL DDL/comment appears based on short ULID-style `bdg_<26>` identifiers, while the Admin budget workflow stores readable `bgt_<scope>_<scope-id>_<period>` values. Engineering must decide how the canonical identity and legacy mapping should work. QA did not modify code or specifications.

## Fix acceptance

1. Define a compatible budget-ID width/format contract for every budget scope and period.
2. Preserve or losslessly map pre-existing SQLite IDs and all references during engine promotion.
3. Migrate the CP35 project and organization budget fixtures successfully on exact RC7 SQLite → PostgreSQL.
4. Verify 100% row counts, integer micro-USD sums and ledger invariants; validate JCS audit chains and Vault envelope decrypt.
5. Read/list/update both migrated budget rows through the real PostgreSQL-backed Admin API/Console and prove subsequent reservations/settlements use those same rows.
6. Retain a negative transaction test where a truly invalid/tampered row aborts and leaves target application tables empty.

## Scope limits

The storage test used a small synthetic QA clone and a PostgreSQL 16 RAM target because the managed overlay had no free bytes. The target was isolated and the failed transaction rolled back; the persistent QA database was not changed. This run did not validate other table-width mismatches, migration performance at scale, live-write freeze/dual-write cutover, PostgreSQL/Valkey runtime, or paid providers.
