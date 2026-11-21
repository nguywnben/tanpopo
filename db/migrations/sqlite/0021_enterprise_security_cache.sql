-- Tanpopo Migration 0021: Four-Eyes Admin Change Requests & Response Cache Entries
-- Unit: TP-IMP-0041 (AUTH-12, CR-09, CT-28, FMEA-03 parity with spec-check sqlite_schema.sql)

CREATE TABLE IF NOT EXISTS response_cache_entries (
    org_id              VARCHAR(40) NOT NULL,
    project_id          VARCHAR(40) NOT NULL,
    env_id              VARCHAR(40) NOT NULL,
    cache_key_sha256    CHAR(64) NOT NULL,
    model_deployment_id VARCHAR(40) NOT NULL,
    aead_alg            VARCHAR(32) NOT NULL DEFAULT 'AES-256-GCM' CHECK (aead_alg = 'AES-256-GCM'),
    nonce               BLOB NOT NULL,
    ciphertext          BLOB NOT NULL, -- Sealed under Project DEK with record_kind='cache_entry'
    created_at_ms       BIGINT NOT NULL,
    expires_at_ms       BIGINT NOT NULL,
    PRIMARY KEY (org_id, project_id, cache_key_sha256),
    FOREIGN KEY (org_id, project_id) REFERENCES project_encryption_keys(org_id, project_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, env_id) REFERENCES environments(org_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_response_cache_entries_expires
    ON response_cache_entries (org_id, project_id, expires_at_ms);

CREATE TABLE IF NOT EXISTS admin_change_requests (
    id                     VARCHAR(40) PRIMARY KEY,
    org_id                 VARCHAR(40) REFERENCES organizations(id) ON DELETE CASCADE,
    action_type            VARCHAR(64) NOT NULL,
    target_type            VARCHAR(32) NOT NULL,
    target_id              VARCHAR(64) NOT NULL,
    payload_jcs_json       TEXT NOT NULL,
    payload_sha256         CHAR(64) NOT NULL,
    initiator_principal_id VARCHAR(40) NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
    approver_principal_id  VARCHAR(40) REFERENCES principals(id) ON DELETE RESTRICT,
    state                  VARCHAR(32) NOT NULL DEFAULT 'PENDING_SECOND_APPROVER'
                           CHECK (state IN (
                               'PENDING_SECOND_APPROVER',
                               'APPROVED_APPLIED',
                               'REJECTED',
                               'EXPIRED',
                               'CANCELLED'
                           )),
    decision_reason        TEXT,
    version                BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    expires_at_ms          BIGINT NOT NULL,
    created_at_ms          BIGINT NOT NULL,
    decided_at_ms          BIGINT,
    CHECK (approver_principal_id IS NULL OR approver_principal_id <> initiator_principal_id)
);

CREATE INDEX IF NOT EXISTS idx_admin_change_requests_org_state
    ON admin_change_requests (org_id, state, created_at_ms DESC);
