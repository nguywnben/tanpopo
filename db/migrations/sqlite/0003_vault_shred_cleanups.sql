-- =============================================================================
-- Tanpopo Migration 0003: Durable Pending Shred WAL Cleanup Outbox
-- Dialect : Portable SQLite WAL (parity with db/spec-check/sqlite_schema.sql)
-- Unit Ref: TP-IMP-0008 (Crash-Safe Crypto-Shredding Post-Commit Cleanup)
-- =============================================================================

CREATE TABLE IF NOT EXISTS vault_pending_shred_cleanups (
    scope_key     VARCHAR(160) PRIMARY KEY, -- 'org:<org_id>' or 'proj:<org_id>:<project_id>'
    org_id        VARCHAR(40) NOT NULL,
    project_id    VARCHAR(40),              -- NULL when org-scoped shred; non-NULL when project-scoped
    shred_epoch   BIGINT NOT NULL CHECK (shred_epoch >= 1),
    created_at_ms BIGINT NOT NULL,
    updated_at_ms BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_vault_pending_shred_cleanups_org
    ON vault_pending_shred_cleanups (org_id, shred_epoch);
