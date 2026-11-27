# CP122 — Second PostgreSQL migration width failure in budget references

**Source:** frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; exact binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
**Status:** FAIL (scoped continuation of DEF-086, not a separate defect).

## Procedure and result

Used a disposable clone of the CP116 SQLite QA DB and removed only the three budget_ledgers rows whose IDs exceeded 40 characters. The first overlength failure from CP117 was therefore bypassed in this test copy; the 39 associated rows in budget_reservation_ledgers were retained. The exact RC7 binary then failed during migration of budget_reservation_ledgers:

`ERROR: value too long for type character varying(40)`

Read-only SQLite evidence: budget_reservation_ledgers has 43 rows; 39 use a 50-character ledger_id such as `bgt_project_..._monthly`. Its schema declares ledger_id VARCHAR(40). The corresponding parent budget_ledgers records exist in the unmodified CP116 source, have lengths 50/55, and are accepted/persisted by the Owner Console. As in CP117, the migration transaction rolled back; PostgreSQL readback of budget_ledgers, budget_reservation_ledgers, usage_settlements and audit_events was zero. The target role was `NOSUPERUSER NOBYPASSRLS` (`rolsuper=false`, `rolbypassrls=false`).

This confirms the same incompatible budget identity crosses both the parent ledger ID and its association foreign-key column. It is one end-to-end migration defect: changing only budget_ledgers.id would leave the reference column unable to store valid IDs. Track under DEF-086, not DEF-087.

## Evidence integrity note

CP118’s previously recorded disposable-copy SHA-256 `900c0d36...` was read before closing/checkpointing its SQLite WAL connection. It identifies the main database file at that moment, not the complete WAL-backed logical source consumed after close; do not treat it as a complete clone digest. The original CP116 source hash is unchanged. CP123 records a correct post-close hash for its own filtered source. This correction preserves the historical CP118 value while making its limit explicit.

See [sanitized CP122 evidence](../evidence/CP122-budget-reservation-ledger-width.json), [CP117/DEF-086](CP117-finops-postgres-migration-budget-id-overflow.md), and the original [CP35 Console-created budget records](../evidence/CP35-budget-lifecycle.json).
