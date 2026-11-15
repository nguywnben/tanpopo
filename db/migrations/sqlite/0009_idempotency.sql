-- =============================================================================
-- Tanpopo Migration 0009: Data Plane Idempotency-Key Records
-- Dialect : Portable SQLite WAL (parity with db/spec-check/sqlite_schema.sql)
-- Unit Ref: TP-IMP-0016 (SPEC-EXT-06)
-- =============================================================================

CREATE TABLE IF NOT EXISTS idempotency_records (
    org_id                VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id            VARCHAR(40) NOT NULL,
    virtual_key_id        VARCHAR(40) NOT NULL,
    idempotency_key       VARCHAR(128) NOT NULL,
    payload_sha256        CHAR(64) NOT NULL,
    state                 VARCHAR(16) NOT NULL CHECK (state IN ('IN_FLIGHT', 'COMPLETED')),
    http_status           SMALLINT,
    response_headers_json TEXT,
    response_body_bytes   BLOB,
    created_at_ms         BIGINT NOT NULL,
    expires_at_ms         BIGINT NOT NULL,
    PRIMARY KEY (org_id, project_id, virtual_key_id, idempotency_key),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, virtual_key_id) REFERENCES virtual_keys(org_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_idempotency_records_expires
    ON idempotency_records (expires_at_ms);
