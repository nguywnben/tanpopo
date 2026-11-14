-- =============================================================================
-- Tanpopo Migration 0005: IAM Principals, 6-Role RBAC & Virtual Keys (SM-05)
-- Dialect : Portable SQLite WAL (parity with db/spec-check/sqlite_schema.sql)
-- Unit Ref: TP-IMP-0009 (CR-04 / CR-06 / ADR-0006)
-- =============================================================================

CREATE TABLE IF NOT EXISTS principals (
    id                VARCHAR(40) PRIMARY KEY, -- 'prc_<26_base32>'
    org_id            VARCHAR(40) REFERENCES organizations(id) ON DELETE CASCADE,
    kind              VARCHAR(24) NOT NULL
                      CHECK (kind IN ('HUMAN_LOCAL', 'HUMAN_OIDC', 'ADMIN_SERVICE_TOKEN')),
    email_or_sub      VARCHAR(255) NOT NULL,
    password_argon2id TEXT,
    oidc_issuer       TEXT,
    status            VARCHAR(24) NOT NULL DEFAULT 'ACTIVE'
                      CHECK (status IN ('ACTIVE', 'SUSPENDED', 'DISABLED')),
    version           BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    last_login_at_ms  BIGINT,
    created_at_ms     BIGINT NOT NULL,
    updated_at_ms     BIGINT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_principals_org_unique
    ON principals (org_id, kind, email_or_sub)
    WHERE org_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_principals_instance_operator_unique
    ON principals (kind, email_or_sub)
    WHERE org_id IS NULL;

CREATE TABLE IF NOT EXISTS role_bindings (
    id            VARCHAR(40) PRIMARY KEY,
    org_id        VARCHAR(40) REFERENCES organizations(id) ON DELETE CASCADE,
    principal_id  VARCHAR(40) NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    scope_type    VARCHAR(24) NOT NULL CHECK (scope_type IN ('INSTANCE', 'ORG', 'PROJECT')),
    scope_id      VARCHAR(40) NOT NULL,
    role_name     VARCHAR(32) NOT NULL
                  CHECK (role_name IN (
                      'OPERATOR',
                      'ORG_OWNER',
                      'TEAM_ADMIN',
                      'SECURITY_REVIEWER',
                      'FINOPS_VIEWER',
                      'DEVELOPER'
                  )),
    version       BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms BIGINT NOT NULL,
    UNIQUE (principal_id, scope_type, scope_id, role_name),
    CHECK ((scope_type = 'INSTANCE' AND org_id IS NULL) OR (scope_type IN ('ORG', 'PROJECT') AND org_id IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS virtual_keys (
    id                      VARCHAR(40) NOT NULL, -- 'vky_<26_base32>'
    org_id                  VARCHAR(40) NOT NULL,
    project_id              VARCHAR(40) NOT NULL,
    env_id                  VARCHAR(40) NOT NULL,
    owner_principal_id      VARCHAR(40) REFERENCES principals(id) ON DELETE SET NULL,
    name                    VARCHAR(128) NOT NULL,
    key_prefix              VARCHAR(24) NOT NULL CHECK (key_prefix LIKE 'vk_tp_%'),
    last_four               CHAR(4) NOT NULL,
    active_key_hash         BLOB NOT NULL UNIQUE,   -- 32-byte raw SHA-256(rawKey)
    previous_key_hash       BLOB UNIQUE,            -- 32-byte raw SHA-256(prevKey) during ROTATING_DUAL_VALID
    grace_expires_at_ms     BIGINT,
    state                   VARCHAR(32) NOT NULL DEFAULT 'CREATED_ACTIVE'
                            CHECK (state IN (
                                'CREATED_ACTIVE',
                                'ROTATING_DUAL_VALID',
                                'QUOTA_PAUSED',
                                'SUSPENDED',
                                'REVOKED',
                                'EXPIRED',
                                'PURGED_METADATA_RETAINED'
                            )),
    allowed_planes          VARCHAR(64) NOT NULL DEFAULT 'PROVIDER',
    allowed_models_json     TEXT NOT NULL DEFAULT '["*"]',
    allowed_routes_json     TEXT NOT NULL DEFAULT '["*"]',
    allowed_mcp_tools_json  TEXT NOT NULL DEFAULT '[]',
    rpm_limit               INTEGER CHECK (rpm_limit IS NULL OR rpm_limit > 0),
    tpm_limit               INTEGER CHECK (tpm_limit IS NULL OR tpm_limit > 0),
    max_concurrent_streams  INTEGER NOT NULL DEFAULT 50 CHECK (max_concurrent_streams > 0),
    is_ephemeral_playground BOOLEAN NOT NULL DEFAULT 0 CHECK (is_ephemeral_playground IN (0, 1)),
    expires_at_ms           BIGINT,
    revoked_at_ms           BIGINT,
    revocation_mode         VARCHAR(24)
                            CHECK (revocation_mode IS NULL OR revocation_mode IN ('STANDARD', 'EMERGENCY_SECURITY')),
    version                 BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms           BIGINT NOT NULL,
    updated_at_ms           BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, env_id) REFERENCES environments(org_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_vkeys_env_active
    ON virtual_keys (org_id, project_id, env_id)
    WHERE state IN ('CREATED_ACTIVE', 'ROTATING_DUAL_VALID');
