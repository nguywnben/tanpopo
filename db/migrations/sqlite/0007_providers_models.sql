-- =============================================================================
-- Tanpopo Migration 0007: 4-Entity Provider Adapters, Connections (SM-02),
--                         Multi-Slot Secret Bindings, Logical Models & Deployments
-- Dialect : Portable SQLite WAL (parity with db/spec-check/sqlite_schema.sql)
-- Unit Ref: TP-IMP-0012 (SPEC-EXT-02 / SM-02 / SM-04 / DEC-004 / ADR-0005)
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 16. Provider Adapters (4-Entity Model Chain #1 — CR-04)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS provider_adapters (
    id                      VARCHAR(40) PRIMARY KEY, -- 'adp_<26_base32>'
    installation_id         VARCHAR(40) NOT NULL REFERENCES extension_installations(id) ON DELETE CASCADE,
    adapter_slug            VARCHAR(64) NOT NULL,
    native_prefix           VARCHAR(64),
    supported_dialects_json TEXT NOT NULL DEFAULT '[]',
    allowed_hosts_json      TEXT NOT NULL DEFAULT '[]',
    version                 BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms           BIGINT NOT NULL,
    UNIQUE (installation_id, adapter_slug)
);

-- -----------------------------------------------------------------------------
-- 17. Provider Connections (4-Entity Chain #2, SM-02, DEC-004, CR-10)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS provider_connections (
    id                      VARCHAR(40) NOT NULL, -- 'con_<26_base32>'
    org_id                  VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_project_id        VARCHAR(40),          -- NULL = Org-Shared
    adapter_id              VARCHAR(40) NOT NULL REFERENCES provider_adapters(id) ON DELETE RESTRICT,
    credential_id           VARCHAR(40),
    name                    VARCHAR(128) NOT NULL,
    endpoint_base_url       TEXT NOT NULL,
    allow_private_ip        BOOLEAN NOT NULL DEFAULT 0 CHECK (allow_private_ip IN (0, 1)),
    allowed_cidrs_json      TEXT NOT NULL DEFAULT '[]',
    data_region             VARCHAR(32) NOT NULL,
    billing_mode            VARCHAR(24) NOT NULL
                            CHECK (billing_mode IN ('COMMERCIAL_API', 'SELF_HOSTED_INFRA')),
    auth_mode               VARCHAR(24) NOT NULL DEFAULT 'API_KEY'
                            CHECK (auth_mode IN (
                                'NONE',
                                'API_KEY',
                                'OAUTH2',
                                'AWS_SIGV4',
                                'WORKLOAD_IDENTITY',
                                'MTLS',
                                'CUSTOM_SIGN'
                            )),
    state                   VARCHAR(40) NOT NULL DEFAULT 'DRAFT'
                            CHECK (state IN (
                                'DRAFT',
                                'VALIDATING',
                                'ACTIVE',
                                'DEGRADED',
                                'AUTH_EXPIRED_REAUTH_REQUIRED',
                                'SUSPENDED_BY_ADMIN',
                                'REVOKED',
                                'ARCHIVED'
                            )),
    shared_project_ids_json TEXT NOT NULL DEFAULT '[]',
    config_json             TEXT NOT NULL DEFAULT '{}',
    version                 BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms           BIGINT NOT NULL,
    updated_at_ms           BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, owner_project_id) REFERENCES projects(org_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (org_id, credential_id) REFERENCES secret_credentials(org_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_provider_connections_org_state
    ON provider_connections (org_id, state);

CREATE INDEX IF NOT EXISTS idx_provider_connections_adapter
    ON provider_connections (adapter_id);

-- -----------------------------------------------------------------------------
-- 18. Multi-Slot Connection Secret Bindings (CR-06, TP-IMP-0012)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS connection_secret_bindings (
    org_id        VARCHAR(40) NOT NULL,
    connection_id VARCHAR(40) NOT NULL,
    slot_name     VARCHAR(64) NOT NULL,
    credential_id VARCHAR(40) NOT NULL,
    inject_mode   VARCHAR(32) NOT NULL DEFAULT 'HOST_BLIND_INJECT'
                  CHECK (inject_mode IN ('HOST_BLIND_INJECT', 'HOST_CRYPTO_SIGN', 'MTLS_HANDSHAKE')),
    created_at_ms BIGINT NOT NULL,
    PRIMARY KEY (org_id, connection_id, slot_name),
    FOREIGN KEY (org_id, connection_id) REFERENCES provider_connections(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, credential_id) REFERENCES secret_credentials(org_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_connection_secret_bindings_cred
    ON connection_secret_bindings (org_id, credential_id);

-- -----------------------------------------------------------------------------
-- 19. Logical Models (4-Entity Chain #3)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS logical_models (
    id                     VARCHAR(40) PRIMARY KEY, -- 'lmd_<26_base32>'
    org_id                 VARCHAR(40) REFERENCES organizations(id) ON DELETE CASCADE,
    canonical_slug         VARCHAR(128) NOT NULL,
    modality_kind          VARCHAR(32) NOT NULL
                           CHECK (modality_kind IN ('CHAT', 'EMBEDDING', 'RERANK', 'MULTIMODAL')),
    context_window         INTEGER NOT NULL CHECK (context_window > 0),
    max_output_tokens      INTEGER NOT NULL CHECK (max_output_tokens >= 0),
    base_capabilities_json TEXT NOT NULL DEFAULT '{}',
    version                BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms          BIGINT NOT NULL,
    updated_at_ms          BIGINT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_logical_models_global_slug
    ON logical_models (canonical_slug)
    WHERE org_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_logical_models_org_slug
    ON logical_models (org_id, canonical_slug)
    WHERE org_id IS NOT NULL;

-- -----------------------------------------------------------------------------
-- 20. Model Deployments (4-Entity Chain #4, SM-04, idle_chunk_timeout_ms)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS model_deployments (
    id                        VARCHAR(40) NOT NULL, -- 'dpl_<26_base32>'
    org_id                    VARCHAR(40) NOT NULL,
    connection_id             VARCHAR(40) NOT NULL,
    logical_model_id          VARCHAR(40) NOT NULL REFERENCES logical_models(id) ON DELETE RESTRICT,
    upstream_model_name       VARCHAR(160) NOT NULL,
    native_dialect            VARCHAR(32),
    capability_overrides_json TEXT NOT NULL DEFAULT '{}',
    assurance_level           VARCHAR(16) NOT NULL DEFAULT 'DECLARED'
                              CHECK (assurance_level IN ('DECLARED', 'TESTED', 'VERIFIED')),
    pricing_nano_usd_json     TEXT NOT NULL,
    state                     VARCHAR(32) NOT NULL DEFAULT 'PROVISIONED'
                              CHECK (state IN (
                                  'PROVISIONED',
                                  'ACTIVE_HEALTHY',
                                  'THROTTLED_COOLDOWN',
                                  'CIRCUIT_OPEN_UNHEALTHY',
                                  'HALF_OPEN_PROBING',
                                  'DRAINING_MAINTENANCE',
                                  'DISABLED'
                              )),
    cooldown_until_ms         BIGINT,
    connect_timeout_ms        INTEGER NOT NULL DEFAULT 5000 CHECK (connect_timeout_ms > 0),
    first_byte_timeout_ms     INTEGER NOT NULL DEFAULT 30000 CHECK (first_byte_timeout_ms > 0),
    idle_chunk_timeout_ms     INTEGER NOT NULL DEFAULT 15000 CHECK (idle_chunk_timeout_ms > 0),
    version                   BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms             BIGINT NOT NULL,
    updated_at_ms             BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, connection_id) REFERENCES provider_connections(org_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_model_deployments_org_conn
    ON model_deployments (org_id, connection_id);

CREATE INDEX IF NOT EXISTS idx_model_deployments_org_model
    ON model_deployments (org_id, logical_model_id);
