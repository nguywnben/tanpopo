-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-Only migration adding time-bounded single-use local member enrollment tokens (DEF-021).

CREATE TABLE IF NOT EXISTS member_enrollment_tokens (
    id            VARCHAR(40) PRIMARY KEY,
    org_id        VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    principal_id  VARCHAR(40) NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    token_hash    VARCHAR(64) NOT NULL UNIQUE,
    issued_by     VARCHAR(40) NOT NULL,
    expires_at_ms BIGINT NOT NULL,
    used_at_ms    BIGINT,
    created_at_ms BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_member_enrollment_principal
    ON member_enrollment_tokens (org_id, principal_id, created_at_ms DESC);
