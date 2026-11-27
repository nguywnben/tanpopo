# CP117 — exact-RC7 SQLite → PostgreSQL migration and FinOps ID compatibility

**Source:** frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; exact-source binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.  
**SQLite input:** isolated QA clone from CP116, SQLite DB SHA-256 `720be31fcb7442fb7a4ea05d40091b7ac67eb180be0cf3228f9fec1d67e1ba59`.  
**Target:** dedicated PostgreSQL 16 cluster and empty DB in the existing QA PostgreSQL container’s RAM-backed `/dev/shm`, separate port 5433 / database `qa005_cp117`; migration used a distinct `NOSUPERUSER NOBYPASSRLS` role over a local test tunnel. The existing QA database was not used as target and was not changed. No providers or paid usage.  
**Disposition:** Migration FAIL; new DEF-086 Major. No migration data was committed; target application tables remained empty after rollback. QA-005 continues; GATE-REL-07 remains BLOCKED.

## Test and environment adaptation

A disk-backed container could not initialize because the managed overlay had zero free bytes (`df` 100%, `docker run` failed creating its root filesystem). No old containers, images or evidence were deleted. To continue, started a separate PostgreSQL 16 server process in the existing QA container’s 64-MiB tmpfs. Provisioned an isolated empty test DB and a least-privilege migrator role (`rolsuper=false`, `rolbypassrls=false`). Tanpopo rejected an initial test connection as superuser before migration; the run was repeated with the correctly restricted role. The RAM-backed cluster, log and local tunnel were stopped/removed after the run, restoring tmpfs usage to 1 MiB. The host overlay remains full and can block disk-backed infrastructure work.

Ran exact binary `tanpopo db migrate-engine --verify` from the CP116 SQLite QA clone into the isolated PostgreSQL target. The migration initialized the target schema, then exited nonzero while copying `budget_ledgers`:

`ERROR: value too long for type character varying(40)`

Post-failure PostgreSQL readback returned 0 organizations, 0 budget ledgers, 0 usage settlements and 0 audit events. The table-copy transaction rolled back. The source DB was opened read-only by the migration implementation; its hash is recorded in [sanitized evidence](../evidence/CP117-finops-postgres-migration.json).

## DEF-086 — SQLite-valid budget IDs overflow the PostgreSQL column

**Severity:** Major. **Status:** FAIL (fresh exact-RC7 migration run).  
**Accepted basis:** TP-IMP-0039 AC-2 / CT-32 requires 100% exact SQLite-to-PostgreSQL row parity, integer micro-USD totals, encrypted-secret verification and audit-chain parity; a mismatch must roll back. The frozen SQLite/PostgreSQL DDL declares budget-ledger ID `VARCHAR(40)`, while the real Console budget-creation API accepts a path string without a maxLength and constructs/persists scope-derived identifiers.

**Reproduction:**

1. On exact RC7 SQLite, use the real Owner Console FinOps → Budgets & Holds workflow to create a project monthly budget and an organization monthly budget (CP35). The API returned HTTP 200; after refresh/scope reselection both rows persisted.
2. Read-only source SQLite inspection finds valid rows such as `bgt_project_prj_01m8ct3xs7e00bv1hwq3qz2est_monthly` (50 characters) and `bgt_organization_org_01m8crmjtje009gbep9v278fpe_monthly` (55 characters).
3. Migrate the actual SQLite clone with exact RC7 `tanpopo db migrate-engine --verify` into an empty PostgreSQL 16 target using a non-superuser, non-BYPASSRLS role.
4. Observe the COPY fail on table `budget_ledgers` with PostgreSQL `VARCHAR(40)` overflow. Verify all row counts in the transaction remain zero after rollback.

The CP35 UI journey, HTTP statuses, source/commit identity and persisted IDs are preserved in [CP35 checkpoint](CP35-first-budget-and-parent-visibility.md) and [CP35 evidence](../evidence/CP35-budget-lifecycle.json); physical DDL and migration result are in [CP117 evidence](../evidence/CP117-finops-postgres-migration.json).

**Expected:** valid baseline SQLite budget records migrate without identity rewrite or data loss; target IDs remain addressable through list/read/update API behavior, and CT-32 parity/financial/audit/secret verification completes.

**Actual:** the real baseline SQLite database contains budget IDs 50–55 characters long; PostgreSQL rejects them under the 40-character column constraint and verified migration aborts. This blocks storage-engine promotion for any source containing these valid budget rows. Existing source SQLite behavior and the migration rollback are preserved; no data was dropped or silently rewritten.

**Business impact:** organizations with existing budgets cannot complete the documented SQLite-to-PostgreSQL promotion. This blocks the supported HA scaling path and can delay incident recovery/availability. The test shows a migration blocker, not loss of the original SQLite data.

**Root-cause hypothesis (not established):** SQLite’s type declaration does not enforce VARCHAR width; the PG DDL and comments assume short generated ULIDs (`bdg_<26>`), while the budget API stores readable `bgt_<scope>_<scope-id>_<period>` identifiers. The exact design decision for preserving/backfilling historical IDs is for engineering to resolve; QA made no schema/spec change.

**Acceptance criteria:** (1) settle a canonical budget identity width/format contract covering organization/project/environment/virtual-key and all periods; (2) preserve or losslessly map every existing SQLite budget ID and all references on upgrade; (3) repeat migration with the CP35 SQLite-created 50/55-character identifiers on exact RC7 source and exact PostgreSQL; (4) `--verify` passes 100% table row parity, reserved/settled/hard-limit micro-USD totals, audit JCS chain, Vault envelope trial decrypt; (5) verify the migrated budgets remain listable/readable/updatable at API/UI and subsequent settlements use the same ledgers. Re-run full migration negative rollback on deliberately tampered/over-limit fixture and ensure the original SQLite remains intact.

## Limits

This is one SQLite QA clone to an isolated PostgreSQL 16 RAM target. It does not prove all schema parity, larger data volume, concurrent writes/freeze window, PostgreSQL/Valkey operations or recovery. The target was initialized by the exact binary’s embedded PostgreSQL migrations before the row-copy transaction; the failed migration left zero copied application rows. No product source or specification was changed.
