-- =============================================================================
-- Tanpopo Migration 0008: Explicit ORG_OWNER TP-BR-0202 Opt-In & Enterprise
--                         Key Fingerprint Tracking (fail-closed)
-- Dialect : Portable SQLite WAL (Expand-Only DDL)
-- Unit Ref: TP-IMP-0012 (TP-BR-0202)
-- =============================================================================

ALTER TABLE org_extension_policies
    ADD COLUMN org_owner_opt_in BOOLEAN NOT NULL DEFAULT 0 CHECK (org_owner_opt_in IN (0, 1));

ALTER TABLE extension_versions
    ADD COLUMN enterprise_key_fingerprint VARCHAR(64);
