# CP118 — Supplemental pricing/settlement migration attempt

**Source:** frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; exact-source binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
**Classification:** UNVERIFIED (attempt BLOCKED by the local forwarding/test harness stall). No parity result is claimed. DEF-086 remains FAIL from CP117.

## Scope and setup

To investigate whether the CP117 PostgreSQL `VARCHAR(40)` failure masked separate rate-card/settlement migration problems, I made a disposable SQLite backup of the CP116 source and removed only its three `budget_ledgers` IDs longer than 40 characters. The clone retained 9 budget rows, 48 settlements, 9 model deployments and 727 audit events. Its pre-close main-file SHA-256 was recorded as `900c0d36e6fda5b33957740bac1bc126707ecd6ed5732669499c1e771b8d830e`; that digest does not represent the full WAL-backed logical clone used after close (see CP122/CP123). The original CP116 source hash remains unchanged.

A separate PostgreSQL 16 process, database and `NOSUPERUSER NOBYPASSRLS` migration role were initialized in the existing QA-only PostgreSQL container's RAM-backed `/dev/shm`. The existing QA database was not used as target.

## Attempt result

The host-local port bridge accepted the exact binary's migration connection. The command did not return a result; PostgreSQL showed the migrator idle in `ClientRead`. I stopped the test instead of treating the hang as a successful migration. No row-count, financial sum, pricing snapshot, audit-chain or Vault-decrypt verification completed, so all remain UNVERIFIED for this slice. This does not contradict the fresh CP117 finding, whose isolated migration completed far enough to identify the budget-ID width failure and whose row-copy transaction rolled back.

## Cleanup and limits

Stopped and removed the separate RAM PostgreSQL process and log, stopped the host tunnel, and restored container tmpfs usage to 1 MiB. The CP116 source and existing QA PostgreSQL DB were untouched. The overlay remained full; no image/container/data cleanup was performed. The root cause of the forwarding stall is not established. Sanitized facts are in [CP118 evidence](../evidence/CP118-pricing-migration-attempt.json).
