-- =============================================================================
-- Tanpopo Migration 0010: Hierarchical Governance Policies
-- Dialect : Portable SQLite WAL (parity with db/spec-check/sqlite_schema.sql)
-- Unit Ref: TP-IMP-0019 (SPEC-CORE-01 / TP-FR-0701)
-- =============================================================================

CREATE TABLE IF NOT EXISTS policies (
    id            VARCHAR(40) NOT NULL, -- 'pol_<26_base32>'
    org_id        VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    scope_type    VARCHAR(16) NOT NULL CHECK (scope_type IN ('ORG', 'PROJECT', 'ENV', 'VIRTUAL_KEY')),
    scope_id      VARCHAR(40) NOT NULL,
    policy_type   VARCHAR(32) NOT NULL
                  CHECK (policy_type IN (
                      'MODEL_ALLOWLIST',
                      'DATA_RESIDENCY',
                      'RATE_LIMIT',
                      'GUARDRAIL',
                      'MCP_GOVERNANCE',
                      'RESPONSE_CACHE'
                  )),
    failure_mode  VARCHAR(16) NOT NULL DEFAULT 'FAIL_CLOSED' CHECK (failure_mode IN ('FAIL_CLOSED', 'FAIL_OPEN')),
    rules_json    TEXT NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    version       BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms BIGINT NOT NULL,
    updated_at_ms BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id)
);

CREATE INDEX IF NOT EXISTS idx_policies_scope
    ON policies (org_id, scope_type, scope_id, policy_type, enabled);
