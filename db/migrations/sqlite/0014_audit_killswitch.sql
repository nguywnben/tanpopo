-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-Only SQLite WAL Migration 0014: Immutable JCS Audit Log & 4-Level Emergency Kill-Switch (TP-IMP-0023)

CREATE TABLE audit_events (
    id            VARCHAR(40) NOT NULL, -- 'aud_<26_base32>'
    created_at_ms BIGINT NOT NULL,
    org_id        VARCHAR(40),          -- NULL for Instance-scoped SYSTEM events
    chain_key     VARCHAR(40) NOT NULL, -- org_id when org_id IS NOT NULL; 'SYSTEM' when org_id IS NULL
    seq_no        BIGINT NOT NULL CHECK (seq_no >= 1),
    actor_kind    VARCHAR(24) NOT NULL,
    actor_id      VARCHAR(64) NOT NULL,
    entry_point   VARCHAR(24) NOT NULL
                  CHECK (entry_point IN ('admin_api', 'cli', 'gitops_boot', 'system_reaper', 'data_plane_guard')),
    event_type    VARCHAR(64) NOT NULL,
    target_type   VARCHAR(32) NOT NULL,
    target_id     VARCHAR(64) NOT NULL,
    request_id    VARCHAR(40),
    metadata_json TEXT NOT NULL,        -- Canonicalized via RFC 8785 JCS prior to hashing
    prev_hash     CHAR(64) NOT NULL,    -- seq_no=1: SHA-256("tanpopo:audit:genesis:v1:" + chain_key)
    entry_hash    CHAR(64) NOT NULL,    -- SHA-256(chain_key || ":" || seq_no || ":" || prev_hash || ":" || JCS(envelope))
    CHECK ((org_id IS NULL AND chain_key = 'SYSTEM') OR (org_id IS NOT NULL AND chain_key = org_id)),
    PRIMARY KEY (chain_key, seq_no, created_at_ms),
    UNIQUE (chain_key, seq_no),
    UNIQUE (id, created_at_ms)
);

CREATE TRIGGER trg_audit_events_no_update
BEFORE UPDATE ON audit_events
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'audit_events_immutable');
END;

CREATE TRIGGER trg_audit_events_no_delete
BEFORE DELETE ON audit_events
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'audit_events_immutable');
END;

CREATE TABLE emergency_kill_switches (
    id                        VARCHAR(40) PRIMARY KEY, -- 'ksw_<26_base32>'
    org_id                    VARCHAR(40) REFERENCES organizations(id) ON DELETE CASCADE,
    level                     VARCHAR(32) NOT NULL
                              CHECK (level IN (
                                  'VIRTUAL_KEY',
                                  'PROVIDER_CONNECTION',
                                  'EXTENSION_INSTALLATION',
                                  'PROJECT_OR_ORG_FREEZE'
                              )),
    target_id                 VARCHAR(64) NOT NULL,
    active                    BOOLEAN NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    abort_active_streams      BOOLEAN NOT NULL DEFAULT 1 CHECK (abort_active_streams IN (0, 1)),
    reason                    TEXT NOT NULL CHECK (LENGTH(reason) >= 5),
    triggered_by_principal_id VARCHAR(40) NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
    cleared_by_principal_id   VARCHAR(40) REFERENCES principals(id) ON DELETE RESTRICT,
    clear_reason              TEXT,
    version                   BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    triggered_at_ms           BIGINT NOT NULL,
    cleared_at_ms             BIGINT
);

CREATE INDEX idx_audit_org_time
    ON audit_events (chain_key, created_at_ms DESC);

CREATE INDEX idx_audit_target_time
    ON audit_events (target_type, target_id, created_at_ms DESC);

CREATE INDEX idx_killswitches_active_lookup
    ON emergency_kill_switches (active, level, target_id);
