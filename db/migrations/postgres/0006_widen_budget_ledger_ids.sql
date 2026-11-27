-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-Only migration widening budget ledger, audit target, and changelog entity
-- identifier columns from VARCHAR(40)/VARCHAR(64) to VARCHAR(128) so long Console-derived
-- and custom budget IDs migrate losslessly from SQLite to PostgreSQL (DEF-086).

ALTER TABLE budget_ledgers
    ALTER COLUMN id TYPE VARCHAR(128),
    ALTER COLUMN scope_id TYPE VARCHAR(128);

ALTER TABLE budget_reservation_ledgers
    ALTER COLUMN ledger_id TYPE VARCHAR(128);

ALTER TABLE valkey_hold_checkpoints
    ALTER COLUMN ledger_id TYPE VARCHAR(128);

ALTER TABLE audit_events
    ALTER COLUMN target_id TYPE VARCHAR(128);

ALTER TABLE config_changelog
    ALTER COLUMN entity_id TYPE VARCHAR(128);
