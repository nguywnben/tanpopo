-- =============================================================================
-- Tanpopo Migration 0001: Foundational Multi-Tenant & Coordination Tables
-- Dialect : Portable SQLite WAL (parity with db/spec-check/sqlite_schema.sql)
-- Unit Ref: TP-IMP-0007 (ARCH-DATA-01)
-- =============================================================================

CREATE TABLE IF NOT EXISTS organizations (
    id                          VARCHAR(40) PRIMARY KEY,
    slug                        VARCHAR(64) NOT NULL UNIQUE,
    display_name                VARCHAR(128) NOT NULL,
    status                      VARCHAR(24) NOT NULL DEFAULT 'ACTIVE'
                                CHECK (status IN ('ACTIVE', 'SUSPENDED', 'DELETING', 'PENDING_ERASURE')),
    default_data_region         VARCHAR(32),
    min_plugin_trust            VARCHAR(40) NOT NULL DEFAULT 'ANY_APPROVED'
                                CHECK (min_plugin_trust IN (
                                    'OFFICIAL_ONLY',
                                    'VERIFIED_OR_ENTERPRISE_SIGNED',
                                    'VERIFIED_ONLY',
                                    'ANY_APPROVED'
                                )),
    require_four_eyes_approval  BOOLEAN NOT NULL DEFAULT 0 CHECK (require_four_eyes_approval IN (0, 1)),
    version                     BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms               BIGINT NOT NULL,
    updated_at_ms               BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
    id              VARCHAR(40) NOT NULL,
    org_id          VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    slug            VARCHAR(64) NOT NULL,
    display_name    VARCHAR(128) NOT NULL,
    status          VARCHAR(24) NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'EMERGENCY_HALTED', 'DELETING', 'PENDING_ERASURE')),
    enforced_region VARCHAR(32),
    version         BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms   BIGINT NOT NULL,
    updated_at_ms   BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    UNIQUE (org_id, slug)
);

CREATE TABLE IF NOT EXISTS environments (
    id                  VARCHAR(40) NOT NULL,
    org_id              VARCHAR(40) NOT NULL,
    project_id          VARCHAR(40) NOT NULL,
    slug                VARCHAR(32) NOT NULL,
    require_tested_caps BOOLEAN NOT NULL DEFAULT 0 CHECK (require_tested_caps IN (0, 1)),
    version             BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms       BIGINT NOT NULL,
    updated_at_ms       BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    UNIQUE (org_id, project_id, slug),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS config_changelog (
    config_epoch  INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id        VARCHAR(40),
    entity_kind   VARCHAR(32) NOT NULL,
    entity_id     VARCHAR(64) NOT NULL,
    mutation_type VARCHAR(24) NOT NULL
                  CHECK (mutation_type IN ('UPSERT', 'DELETE', 'REVOKE', 'QUARANTINE', 'KEY_ROTATE', 'SHRED')),
    emergency     BOOLEAN NOT NULL DEFAULT 0 CHECK (emergency IN (0, 1)),
    created_at_ms BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS distributed_leases (
    lease_name     VARCHAR(64) PRIMARY KEY,
    holder_node_id VARCHAR(64) NOT NULL,
    fencing_token  BIGINT NOT NULL DEFAULT 1 CHECK (fencing_token >= 1),
    acquired_at_ms BIGINT NOT NULL,
    expires_at_ms  BIGINT NOT NULL CHECK (expires_at_ms > acquired_at_ms)
);

CREATE INDEX IF NOT EXISTS idx_config_changelog_org_epoch
    ON config_changelog (org_id, config_epoch DESC);
