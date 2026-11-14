-- =============================================================================
-- Tanpopo Migration 0004: Monotonic DEK Generation & Shred Idempotency Fencing
-- Dialect : Portable SQLite WAL (parity with db/spec-check/sqlite_schema.sql)
-- Unit Ref: TP-IMP-0008 (Stale Shred Retry & Generation Fencing)
-- =============================================================================

ALTER TABLE org_encryption_keys
    ADD COLUMN key_generation BIGINT NOT NULL DEFAULT 1 CHECK (key_generation >= 1);

ALTER TABLE project_encryption_keys
    ADD COLUMN key_generation BIGINT NOT NULL DEFAULT 1 CHECK (key_generation >= 1);

ALTER TABLE vault_pending_shred_cleanups
    ADD COLUMN target_generation BIGINT NOT NULL DEFAULT 1 CHECK (target_generation >= 1);

CREATE TABLE IF NOT EXISTS vault_key_generations (
    scope_key       VARCHAR(160) PRIMARY KEY, -- 'org:<org_id>' or 'proj:<org_id>:<project_id>'
    org_id          VARCHAR(40) NOT NULL,
    project_id      VARCHAR(40),
    last_generation BIGINT NOT NULL CHECK (last_generation >= 1),
    updated_at_ms   BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS vault_shred_requests (
    org_id            VARCHAR(40) NOT NULL,
    request_id        VARCHAR(128) NOT NULL,
    scope_key         VARCHAR(160) NOT NULL,
    project_id        VARCHAR(40),
    target_generation BIGINT NOT NULL CHECK (target_generation >= 1),
    status            VARCHAR(24) NOT NULL
                      CHECK (status IN ('PENDING_CLEANUP', 'COMPLETED')),
    created_at_ms     BIGINT NOT NULL,
    updated_at_ms     BIGINT NOT NULL,
    PRIMARY KEY (org_id, request_id)
);

CREATE INDEX IF NOT EXISTS idx_vault_shred_requests_scope
    ON vault_shred_requests (org_id, scope_key, status);

INSERT INTO vault_key_generations (scope_key, org_id, project_id, last_generation, updated_at_ms)
SELECT 'org:' || org_id, org_id, NULL, key_generation, rotated_at_ms
FROM org_encryption_keys
WHERE 1 = 1
ON CONFLICT(scope_key) DO NOTHING;

INSERT INTO vault_key_generations (scope_key, org_id, project_id, last_generation, updated_at_ms)
SELECT 'proj:' || org_id || ':' || project_id, org_id, project_id, key_generation, rotated_at_ms
FROM project_encryption_keys
WHERE 1 = 1
ON CONFLICT(scope_key) DO NOTHING;

INSERT INTO vault_key_generations (scope_key, org_id, project_id, last_generation, updated_at_ms)
SELECT scope_key, org_id, project_id, target_generation, updated_at_ms
FROM vault_pending_shred_cleanups
WHERE 1 = 1
ON CONFLICT(scope_key) DO NOTHING;
