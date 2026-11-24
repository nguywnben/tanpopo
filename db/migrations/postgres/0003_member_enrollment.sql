-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-Only migration adding time-bounded single-use local member enrollment tokens with FORCE RLS (DEF-021).

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

ALTER TABLE member_enrollment_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_enrollment_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON member_enrollment_tokens
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));
