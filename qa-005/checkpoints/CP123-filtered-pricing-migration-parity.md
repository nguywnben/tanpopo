# CP123 — Filtered-source pricing, settlements, and audit migration parity

**Source:** exact RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
**Status:** PASS (scoped, filtered data only). DEF-086 remains FAIL for full-source migration.

## Test setup

Created a disposable SQLite backup of the CP116 source. To isolate non-budget migration coverage after CP117/122 found the accepted budget identity exceeds the PostgreSQL width, removed three overlength budget_ledgers rows and their 39 matching budget_reservation_ledgers association rows. The filtered source passed `PRAGMA foreign_key_check` with zero violations. Remaining selected counts: 9 budget_ledgers, 50 budget_reservations, 4 budget_reservation_ledgers, 48 usage_settlements, 9 model_deployments, 727 audit_events. After closing/checkpointing SQLite, clone SHA-256 was `bd365c97faaf801379ae11042e567607a5e2cedfc431aeb61dc57fbe08e79c53`.

An isolated PostgreSQL 16 process ran only in QA container `/dev/shm` with a dedicated `NOSUPERUSER NOBYPASSRLS` role. The corrected byte-wise local tunnel no longer stalled. The exact-source binary command `db migrate-engine --verify` exited 0 and returned `status=VERIFIED`, `tables_copied=55`, `rows_copied=1230`, `audit_events_verified=727`, `secret_envelopes_verified=0`, `hard_limit_sum_micros=22500000`, `settled_cost_sum_micros=18104016`, `verified=true`.

## Independent financial-data comparisons

Beyond the command summary, parsed SQLite JSON and PostgreSQL JSONB values were compared in deterministic ID order:

- All 9 model_deployments `pricing_nano_usd_json` values matched exactly after JSON normalization; source and target canonical digest: `2027faa05edfcf8f3ccffbe5cf0e70be1bbb2e49115c50fc895055d10e20da8f`.
- All 48 usage_settlements matched exactly across request ID, token counters, media usage JSON, applied pricing snapshot JSON and total_cost_micros; source and target canonical digest: `b91d9317e8563f2bf36e8862d0d0c7ce05ed50633098923f75f89a1863d54965`.
- Independent settlement sum was 18,104,016 micro-USD on each side.

No secret credentials existed in the source clone, so `secret_envelopes_verified=0` is not a Vault-decrypt pass. No migrated API/Console or subsequent settlement was tested. The excluded budget rows and links mean this does not establish full source parity, budget continuity, or a supported migration. It demonstrates scoped pricing, settlement JSON, micro-USD, table-count and audit-chain parity for the retained valid subset.

## Cleanup and evidence integrity

Stopped and removed the isolated PostgreSQL process, log and tunnel; container tmpfs returned to 1 MiB used. The existing QA PostgreSQL database and CP116 source were untouched. [Sanitized evidence](../evidence/CP123-filtered-pricing-migration-parity.json) records counts, digests, command result and exclusions. CP118’s earlier hash was measured before SQLite WAL close and is explicitly corrected in CP122; CP123’s source hash was measured after close/checkpoint.
