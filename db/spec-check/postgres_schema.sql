-- =============================================================================
-- Tanpopo AI Gateway — Canonical Reference PostgreSQL 16+ Physical Schema
-- Document Ref : docs/architecture/data-model.md (ARCH-DATA-01)
-- Unit Ref     : TP-IMP-0003 (Closes GAP-04, GAP-05, GAP-06; CR-03, CR-04, CR-06, CR-07)
-- Dialect      : PostgreSQL 16+ (pgx/v5, non-SUPERUSER & non-BYPASSRLS role)
-- Isolation    : Composite (org_id, id) keys + ENABLE & FORCE ROW LEVEL SECURITY
--                bound to SET LOCAL app.current_org_id = '<org_id>' per transaction.
-- Partitioning : Monthly PARTITION BY RANGE on usage_settlements (settled_at_ms),
--                request_traces_meta (created_at_ms), and audit_events (created_at_ms).
-- Timestamps   : All *_ms columns store UTC Unix epoch milliseconds (BIGINT) for
--                100% lossless Dual-DB parity with SQLite WAL (CT-32).
-- Currency     : Zero IEEE-754 floats. Unit rates in integer nano-USD (10^-9 USD);
--                ledger balances, holds, and settlements in integer micro-USD (10^-6 USD).
-- Identifiers  : <prefix>_<26_chars_crockford_base32_uuidv7> (<= 32 chars in VARCHAR(40)).
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Shared Immutability Trigger Function (usage_settlements & audit_events)
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION tanpopo_prevent_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '%_immutable: UPDATE and DELETE are prohibited on append-only table %', TG_TABLE_NAME, TG_TABLE_NAME
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;

-- -----------------------------------------------------------------------------
-- 0. Schema Migration Ledger (TP-IMP-0007 / TP-IMP-0039 / FMEA-06)
-- -----------------------------------------------------------------------------
CREATE TABLE schema_migrations (
    version         BIGINT PRIMARY KEY,
    name            VARCHAR(128) NOT NULL,
    checksum_sha256 CHAR(64) NOT NULL,
    dirty           BOOLEAN NOT NULL DEFAULT FALSE,
    applied_at_ms   BIGINT NOT NULL
);

-- -----------------------------------------------------------------------------
-- 1. Organizations (Root Tenant Boundary — CR-03, CR-04)
-- -----------------------------------------------------------------------------
CREATE TABLE organizations (
    id                          VARCHAR(40) PRIMARY KEY, -- 'org_<26_base32>'
    slug                        VARCHAR(64) NOT NULL UNIQUE,
    display_name                VARCHAR(128) NOT NULL,
    status                      VARCHAR(24) NOT NULL DEFAULT 'ACTIVE'
                                CHECK (status IN ('ACTIVE', 'SUSPENDED', 'DELETING', 'PENDING_ERASURE')),
    default_data_region         VARCHAR(32),             -- NULL (Global) | 'EU' | 'US' | 'APAC' | 'ON_PREM'
    min_plugin_trust            VARCHAR(40) NOT NULL DEFAULT 'ANY_APPROVED'
                                CHECK (min_plugin_trust IN (
                                    'OFFICIAL_ONLY',
                                    'VERIFIED_OR_ENTERPRISE_SIGNED',
                                    'VERIFIED_ONLY',
                                    'ANY_APPROVED'
                                )),
    require_four_eyes_approval  BOOLEAN NOT NULL DEFAULT FALSE,
    version                     BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms               BIGINT NOT NULL,
    updated_at_ms               BIGINT NOT NULL
);

-- -----------------------------------------------------------------------------
-- 2. Projects (Composite (org_id, id) Multi-Tenant Key — TP-BR-0101)
-- -----------------------------------------------------------------------------
CREATE TABLE projects (
    id              VARCHAR(40) NOT NULL, -- 'prj_<26_base32>'
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

-- -----------------------------------------------------------------------------
-- 3. Environments (Composite (org_id, id) & Same-Org Project FK — TP-BR-0103)
-- -----------------------------------------------------------------------------
CREATE TABLE environments (
    id                  VARCHAR(40) NOT NULL, -- 'env_<26_base32>'
    org_id              VARCHAR(40) NOT NULL,
    project_id          VARCHAR(40) NOT NULL,
    slug                VARCHAR(32) NOT NULL, -- 'dev' | 'staging' | 'prod'
    require_tested_caps BOOLEAN NOT NULL DEFAULT FALSE,
    version             BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms       BIGINT NOT NULL,
    updated_at_ms       BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    UNIQUE (org_id, project_id, slug),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE
);

-- -----------------------------------------------------------------------------
-- 4. Organization Envelope Encryption Keys (Org DEK wrapped by Master KEK)
-- -----------------------------------------------------------------------------
CREATE TABLE org_encryption_keys (
    org_id          VARCHAR(40) PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    kek_key_id      VARCHAR(128) NOT NULL,
    aead_alg        VARCHAR(32) NOT NULL DEFAULT 'AES-256-GCM' CHECK (aead_alg = 'AES-256-GCM'),
    nonce           BYTEA NOT NULL, -- 96-bit CSPRNG nonce
    wrapped_org_dek BYTEA NOT NULL, -- 256-bit Org DEK AEAD-sealed under Master KEK
    dek_version     BIGINT NOT NULL DEFAULT 1 CHECK (dek_version >= 1),
    created_at_ms   BIGINT NOT NULL,
    rotated_at_ms   BIGINT NOT NULL
);

-- -----------------------------------------------------------------------------
-- 5. Project Envelope Encryption Keys (Project DEK wrapped by Org DEK — CR-06)
-- -----------------------------------------------------------------------------
CREATE TABLE project_encryption_keys (
    org_id              VARCHAR(40) NOT NULL,
    project_id          VARCHAR(40) NOT NULL,
    org_dek_version     BIGINT NOT NULL DEFAULT 1 CHECK (org_dek_version >= 1),
    aead_alg            VARCHAR(32) NOT NULL DEFAULT 'AES-256-GCM' CHECK (aead_alg = 'AES-256-GCM'),
    nonce               BYTEA NOT NULL, -- 96-bit CSPRNG nonce
    wrapped_project_dek BYTEA NOT NULL, -- 256-bit Project DEK AEAD-sealed under Org DEK
    dek_version         BIGINT NOT NULL DEFAULT 1 CHECK (dek_version >= 1),
    created_at_ms       BIGINT NOT NULL,
    rotated_at_ms       BIGINT NOT NULL,
    PRIMARY KEY (org_id, project_id),
    FOREIGN KEY (org_id) REFERENCES org_encryption_keys(org_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE
);

-- -----------------------------------------------------------------------------
-- 6. Administrative Principals (Nullable org_id for Instance OPERATOR — CR-06)
-- -----------------------------------------------------------------------------
CREATE TABLE principals (
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

CREATE UNIQUE INDEX idx_principals_org_unique
    ON principals (org_id, kind, email_or_sub)
    WHERE org_id IS NOT NULL;

CREATE UNIQUE INDEX idx_principals_instance_operator_unique
    ON principals (kind, email_or_sub)
    WHERE org_id IS NULL;

-- -----------------------------------------------------------------------------
-- 7. RBAC Role Bindings (6 Canonical Roles — CR-04)
-- -----------------------------------------------------------------------------
CREATE TABLE role_bindings (
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

-- -----------------------------------------------------------------------------
-- 8. Virtual Keys (SM-05, vk_tp_ Prefix & Plain SHA-256 Digests — CR-06)
-- -----------------------------------------------------------------------------
CREATE TABLE virtual_keys (
    id                      VARCHAR(40) NOT NULL, -- 'vky_<26_base32>'
    org_id                  VARCHAR(40) NOT NULL,
    project_id              VARCHAR(40) NOT NULL,
    env_id                  VARCHAR(40) NOT NULL,
    owner_principal_id      VARCHAR(40) REFERENCES principals(id) ON DELETE SET NULL,
    name                    VARCHAR(128) NOT NULL,
    key_prefix              VARCHAR(24) NOT NULL CHECK (key_prefix LIKE 'vk_tp_%'),
    last_four               CHAR(4) NOT NULL,
    active_key_hash         BYTEA NOT NULL UNIQUE,   -- 32-byte raw SHA-256(rawKey)
    previous_key_hash       BYTEA UNIQUE,            -- 32-byte raw SHA-256(prevKey) during ROTATING_DUAL_VALID
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
    allowed_models_json     JSONB NOT NULL DEFAULT '["*"]'::jsonb,
    allowed_routes_json     JSONB NOT NULL DEFAULT '["*"]'::jsonb,
    allowed_mcp_tools_json  JSONB NOT NULL DEFAULT '[]'::jsonb,
    rpm_limit               INTEGER CHECK (rpm_limit IS NULL OR rpm_limit > 0),
    tpm_limit               INTEGER CHECK (tpm_limit IS NULL OR tpm_limit > 0),
    max_concurrent_streams  INTEGER NOT NULL DEFAULT 50 CHECK (max_concurrent_streams > 0),
    is_ephemeral_playground BOOLEAN NOT NULL DEFAULT FALSE,
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

-- -----------------------------------------------------------------------------
-- 9. Secret Credentials & OAuth Token State / Refresh Leases (TP-FR-0204, SM-03)
--    Org-scoped AAD (owner_project_id IS NULL, Org DEK):
--      "tanpopo:v1:cred:" + org_id + ":" + credential_id + ":" + secret_kind
--    Project-scoped AAD (Project DEK; record_kind IN ('cred', 'debug_blob', 'cache_entry')):
--      "tanpopo:v1:proj:" + org_id + ":" + project_id + ":" + record_kind + ":" + record_id
-- -----------------------------------------------------------------------------
CREATE TABLE secret_credentials (
    id               VARCHAR(40) NOT NULL, -- 'sec_<26_base32>'
    org_id           VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_project_id VARCHAR(40),          -- NULL = Org-owned; NOT NULL = Project-owned
    name             VARCHAR(128) NOT NULL,
    secret_kind      VARCHAR(32) NOT NULL
                     CHECK (secret_kind IN (
                         'API_KEY',
                         'OAUTH2_TOKEN_SET',
                         'HMAC_SECRET',
                         'AWS_SIGV4_KEY',
                         'MTLS_CLIENT_CERT',
                         'WEBHOOK_HMAC_SECRET',
                         'OCI_REGISTRY_AUTH'
                     )),
    aead_alg         VARCHAR(32) NOT NULL DEFAULT 'AES-256-GCM' CHECK (aead_alg = 'AES-256-GCM'),
    nonce            BYTEA NOT NULL,
    ciphertext       BYTEA NOT NULL,
    bound_audience   TEXT,                 -- RFC 8707 resource / audience host binding
    key_fingerprint  VARCHAR(64) NOT NULL, -- Irreversible display digest
    version          BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms    BIGINT NOT NULL,
    updated_at_ms    BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, owner_project_id) REFERENCES project_encryption_keys(org_id, project_id) ON DELETE CASCADE
);

CREATE TABLE oauth_token_states (
    org_id               VARCHAR(40) NOT NULL,
    credential_id        VARCHAR(40) NOT NULL,
    bound_principal_id   VARCHAR(40) REFERENCES principals(id) ON DELETE SET NULL,
    state                VARCHAR(40) NOT NULL DEFAULT 'UNBOUND'
                         CHECK (state IN (
                             'UNBOUND',
                             'AUTHORIZING',
                             'TOKEN_VALID',
                             'REFRESHING_SINGLEFLIGHT',
                             'REFRESH_GRACE_DEGRADED',
                             'REAUTH_REQUIRED',
                             'AUTH_EXPIRED_REAUTH_REQUIRED',
                             'INVALIDATED_HOST_CHANGE',
                             'REVOKED'
                         )),
    granted_scopes       TEXT NOT NULL,
    bound_resource_uri   TEXT,
    access_expires_at_ms BIGINT,
    last_refresh_error   VARCHAR(128),
    version              BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    updated_at_ms        BIGINT NOT NULL,
    PRIMARY KEY (org_id, credential_id),
    FOREIGN KEY (org_id, credential_id) REFERENCES secret_credentials(org_id, id) ON DELETE CASCADE
);

CREATE TABLE oauth_token_leases (
    org_id              VARCHAR(40) NOT NULL,
    credential_id       VARCHAR(40) NOT NULL,
    lease_owner_node_id VARCHAR(64) NOT NULL,
    lease_token         VARCHAR(64) NOT NULL,
    acquired_at_ms      BIGINT NOT NULL,
    expires_at_ms       BIGINT NOT NULL,
    PRIMARY KEY (org_id, credential_id),
    FOREIGN KEY (org_id, credential_id) REFERENCES secret_credentials(org_id, id) ON DELETE CASCADE
);

-- -----------------------------------------------------------------------------
-- 10. OAuth 2.1 Pending Authorization Sessions (PKCE S256 & Device Flow — CR-06)
-- -----------------------------------------------------------------------------
CREATE TABLE oauth_authorization_sessions (
    id                    VARCHAR(40) NOT NULL,
    org_id                VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id            VARCHAR(40),
    principal_id          VARCHAR(40) NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    target_kind           VARCHAR(24) NOT NULL CHECK (target_kind IN ('PROVIDER_CONNECTION', 'MCP_SERVER')),
    target_id             VARCHAR(40) NOT NULL,
    grant_type            VARCHAR(32) NOT NULL
                          CHECK (grant_type IN ('AUTHORIZATION_CODE_PKCE', 'DEVICE_CODE', 'CLIENT_CREDENTIALS')),
    state                 VARCHAR(128) NOT NULL UNIQUE,
    code_challenge_method VARCHAR(16) NOT NULL DEFAULT 'S256' CHECK (code_challenge_method = 'S256'),
    sealed_code_verifier  BYTEA,        -- PKCE code_verifier AEAD-sealed
    verifier_nonce        BYTEA,
    device_code           VARCHAR(255), -- RFC 8628 device_code
    user_code             VARCHAR(64),
    verification_uri      TEXT,
    poll_interval_ms      INTEGER,
    bound_resource_uri    TEXT,
    session_status        VARCHAR(24) NOT NULL DEFAULT 'PENDING'
                          CHECK (session_status IN ('PENDING', 'COMPLETED', 'EXPIRED', 'CONSUMED', 'FAILED')),
    expires_at_ms         BIGINT NOT NULL,
    created_at_ms         BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE
);

-- -----------------------------------------------------------------------------
-- 11. Extension Packages (3 Public Provenance Tiers — CR-03)
-- -----------------------------------------------------------------------------
CREATE TABLE extension_packages (
    id                VARCHAR(40) PRIMARY KEY, -- 'ext_<26_base32>'
    canonical_name    VARCHAR(160) NOT NULL UNIQUE,
    extension_kind    VARCHAR(32) NOT NULL DEFAULT 'PROVIDER_ADAPTER'
                      CHECK (extension_kind IN (
                          'PROVIDER_ADAPTER',
                          'AUTH_DRIVER',
                          'GUARDRAIL_POLICY',
                          'TELEMETRY_EXPORTER'
                      )),
    publisher_id      VARCHAR(128) NOT NULL,
    origin_trust_tier VARCHAR(32) NOT NULL
                      CHECK (origin_trust_tier IN ('official', 'verified', 'unverified_external')),
    created_at_ms     BIGINT NOT NULL
);

-- -----------------------------------------------------------------------------
-- 12. Extension Versions (3 Execution Tiers & Enterprise Signature — CR-02, CR-03)
-- -----------------------------------------------------------------------------
CREATE TABLE extension_versions (
    id                      VARCHAR(40) PRIMARY KEY, -- 'exv_<26_base32>'
    package_id              VARCHAR(40) NOT NULL REFERENCES extension_packages(id) ON DELETE CASCADE,
    semver                  VARCHAR(64) NOT NULL,
    contract_version        VARCHAR(16) NOT NULL,
    core_version_range      VARCHAR(64) NOT NULL DEFAULT '>=0.1.0',
    execution_tier          VARCHAR(32) NOT NULL
                            CHECK (execution_tier IN (
                                'DECLARATIVE_T1',
                                'WASM_SANDBOX_T2',
                                'ISOLATED_SIDECAR_T3'
                            )),
    bundle_sha256           CHAR(64) NOT NULL,
    signature_verified      BOOLEAN NOT NULL DEFAULT FALSE,
    enterprise_signed       BOOLEAN NOT NULL DEFAULT FALSE,
    signing_key_fingerprint VARCHAR(64),
    source_uri              TEXT NOT NULL,
    manifest_json           JSONB NOT NULL,
    sbom_digest             CHAR(64),
    created_at_ms           BIGINT NOT NULL,
    UNIQUE (package_id, semver),
    UNIQUE (package_id, bundle_sha256)
);

-- -----------------------------------------------------------------------------
-- 13. Extension Installations (SM-01; active_version_id NULLABLE while staging)
-- -----------------------------------------------------------------------------
CREATE TABLE extension_installations (
    id                         VARCHAR(40) PRIMARY KEY, -- 'ins_<26_base32>'
    package_id                 VARCHAR(40) NOT NULL UNIQUE REFERENCES extension_packages(id) ON DELETE RESTRICT,
    active_version_id          VARCHAR(40) REFERENCES extension_versions(id),
    previous_pinned_version_id VARCHAR(40) REFERENCES extension_versions(id),
    candidate_version_id       VARCHAR(40) REFERENCES extension_versions(id),
    state                      VARCHAR(40) NOT NULL
                               CHECK (state IN (
                                   'DISCOVERED',
                                   'VERIFYING',
                                   'REJECTED_INVALID',
                                   'PENDING_PERMISSION_APPROVAL',
                                   'CANCELLED',
                                   'INSTALLED_DISABLED',
                                   'ENABLED_ACTIVE',
                                   'UPDATING_PENDING_RECONSENT',
                                   'DRAINING',
                                   'ROLLED_BACK',
                                   'QUARANTINED',
                                   'UNINSTALLED'
                               )),
    approved_permissions_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    approved_by_principal_id   VARCHAR(40) REFERENCES principals(id) ON DELETE SET NULL,
    approved_at_ms             BIGINT,
    quarantine_reason          TEXT,
    version                    BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms              BIGINT NOT NULL,
    updated_at_ms              BIGINT NOT NULL
);

-- -----------------------------------------------------------------------------
-- 14. Organization Extension Enablement Policies (CR-04)
-- -----------------------------------------------------------------------------
CREATE TABLE org_extension_policies (
    org_id                 VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    package_id             VARCHAR(40) NOT NULL REFERENCES extension_packages(id) ON DELETE CASCADE,
    enabled                BOOLEAN NOT NULL DEFAULT TRUE,
    pinned_installation_id VARCHAR(40) REFERENCES extension_installations(id) ON DELETE SET NULL,
    version                BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms          BIGINT NOT NULL,
    updated_at_ms          BIGINT NOT NULL,
    PRIMARY KEY (org_id, package_id)
);

-- -----------------------------------------------------------------------------
-- 15. Signed Extension Revocation Feed Records (Anti-Rollback — CR-06, TP-IMP-0035)
-- -----------------------------------------------------------------------------
CREATE TABLE extension_revocations (
    id                 VARCHAR(40) PRIMARY KEY,
    package_slug       VARCHAR(160) NOT NULL,
    version_or_sha256  VARCHAR(128) NOT NULL,
    severity           VARCHAR(32) NOT NULL
                       CHECK (severity IN ('CRITICAL', 'CRITICAL_MALICIOUS', 'HIGH', 'VULNERABLE_UPGRADE_ADVISED')),
    version_serial     BIGINT NOT NULL CHECK (version_serial > 0),
    reason             TEXT NOT NULL,
    feed_expires_at_ms BIGINT NOT NULL,
    imported_at_ms     BIGINT NOT NULL,
    UNIQUE (package_slug, version_or_sha256)
);

-- -----------------------------------------------------------------------------
-- 16. Provider Adapters (4-Entity Model Chain #1 — CR-04)
-- -----------------------------------------------------------------------------
CREATE TABLE provider_adapters (
    id                      VARCHAR(40) PRIMARY KEY, -- 'adp_<26_base32>'
    installation_id         VARCHAR(40) NOT NULL REFERENCES extension_installations(id) ON DELETE CASCADE,
    adapter_slug            VARCHAR(64) NOT NULL,
    native_prefix           VARCHAR(64),
    supported_dialects_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_hosts_json      JSONB NOT NULL DEFAULT '[]'::jsonb,
    version                 BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms           BIGINT NOT NULL,
    UNIQUE (installation_id, adapter_slug)
);

-- -----------------------------------------------------------------------------
-- 17. Provider Connections (4-Entity Chain #2, SM-02, DEC-004, CR-10)
-- -----------------------------------------------------------------------------
CREATE TABLE provider_connections (
    id                      VARCHAR(40) NOT NULL, -- 'con_<26_base32>'
    org_id                  VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_project_id        VARCHAR(40),          -- NULL = Org-Shared
    adapter_id              VARCHAR(40) NOT NULL REFERENCES provider_adapters(id) ON DELETE RESTRICT,
    credential_id           VARCHAR(40),
    name                    VARCHAR(128) NOT NULL,
    endpoint_base_url       TEXT NOT NULL,
    allow_private_ip        BOOLEAN NOT NULL DEFAULT FALSE,
    allowed_cidrs_json      JSONB NOT NULL DEFAULT '[]'::jsonb,
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
    shared_project_ids_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    config_json             JSONB NOT NULL DEFAULT '{}'::jsonb,
    version                 BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms           BIGINT NOT NULL,
    updated_at_ms           BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, owner_project_id) REFERENCES projects(org_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (org_id, credential_id) REFERENCES secret_credentials(org_id, id) ON DELETE RESTRICT
);

-- -----------------------------------------------------------------------------
-- 18. Multi-Slot Connection Secret Bindings (CR-06, TP-IMP-0012)
-- -----------------------------------------------------------------------------
CREATE TABLE connection_secret_bindings (
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

-- -----------------------------------------------------------------------------
-- 19. Logical Models (4-Entity Chain #3)
-- -----------------------------------------------------------------------------
CREATE TABLE logical_models (
    id                     VARCHAR(40) PRIMARY KEY, -- 'lmd_<26_base32>'
    org_id                 VARCHAR(40) REFERENCES organizations(id) ON DELETE CASCADE,
    canonical_slug         VARCHAR(128) NOT NULL,
    modality_kind          VARCHAR(32) NOT NULL
                           CHECK (modality_kind IN ('CHAT', 'EMBEDDING', 'RERANK', 'MULTIMODAL')),
    context_window         INTEGER NOT NULL CHECK (context_window > 0),
    max_output_tokens      INTEGER NOT NULL CHECK (max_output_tokens >= 0),
    base_capabilities_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    version                BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms          BIGINT NOT NULL,
    updated_at_ms          BIGINT NOT NULL
);

CREATE UNIQUE INDEX idx_logical_models_global_slug
    ON logical_models (canonical_slug)
    WHERE org_id IS NULL;

CREATE UNIQUE INDEX idx_logical_models_org_slug
    ON logical_models (org_id, canonical_slug)
    WHERE org_id IS NOT NULL;

-- -----------------------------------------------------------------------------
-- 20. Model Deployments (4-Entity Chain #4, SM-04, idle_chunk_timeout_ms)
-- -----------------------------------------------------------------------------
CREATE TABLE model_deployments (
    id                        VARCHAR(40) NOT NULL, -- 'dpl_<26_base32>'
    org_id                    VARCHAR(40) NOT NULL,
    connection_id             VARCHAR(40) NOT NULL,
    logical_model_id          VARCHAR(40) NOT NULL REFERENCES logical_models(id) ON DELETE RESTRICT,
    upstream_model_name       VARCHAR(160) NOT NULL,
    native_dialect            VARCHAR(32),
    capability_overrides_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    assurance_level           VARCHAR(16) NOT NULL DEFAULT 'DECLARED'
                              CHECK (assurance_level IN ('DECLARED', 'TESTED', 'VERIFIED')),
    pricing_nano_usd_json     JSONB NOT NULL,
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

-- -----------------------------------------------------------------------------
-- 21. Routing, Hierarchical Governance Policies, Idempotency & Opt-In Cache
-- -----------------------------------------------------------------------------
CREATE TABLE route_policies (
    id                VARCHAR(40) NOT NULL, -- 'rte_<26_base32>'
    org_id            VARCHAR(40) NOT NULL,
    project_id        VARCHAR(40) NOT NULL,
    env_id            VARCHAR(40) NOT NULL,
    route_slug        VARCHAR(128) NOT NULL,
    strategy          VARCHAR(32) NOT NULL DEFAULT 'PRIORITY_FAILOVER'
                      CHECK (strategy IN (
                          'PRIORITY_FAILOVER',
                          'WEIGHTED_ROUND_ROBIN',
                          'WEIGHTED',
                          'LOWEST_COST',
                          'LOWEST_LATENCY_EWMA'
                      )),
    max_failover_hops SMALLINT NOT NULL DEFAULT 3 CHECK (max_failover_hops BETWEEN 0 AND 5),
    required_region   VARCHAR(32),
    version           BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms     BIGINT NOT NULL,
    updated_at_ms     BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    UNIQUE (org_id, env_id, route_slug),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, env_id) REFERENCES environments(org_id, id) ON DELETE CASCADE
);

CREATE TABLE route_targets (
    org_id         VARCHAR(40) NOT NULL,
    route_id       VARCHAR(40) NOT NULL,
    deployment_id  VARCHAR(40) NOT NULL,
    priority_order SMALLINT NOT NULL,
    weight         SMALLINT NOT NULL DEFAULT 100 CHECK (weight >= 0),
    PRIMARY KEY (org_id, route_id, deployment_id),
    FOREIGN KEY (org_id, route_id) REFERENCES route_policies(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, deployment_id) REFERENCES model_deployments(org_id, id) ON DELETE RESTRICT
);

CREATE TABLE policies (
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
    rules_json    JSONB NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    version       BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms BIGINT NOT NULL,
    updated_at_ms BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id)
);

CREATE TABLE idempotency_records (
    org_id                VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id            VARCHAR(40) NOT NULL,
    virtual_key_id        VARCHAR(40) NOT NULL,
    idempotency_key       VARCHAR(128) NOT NULL,
    payload_sha256        CHAR(64) NOT NULL,
    state                 VARCHAR(16) NOT NULL CHECK (state IN ('IN_FLIGHT', 'COMPLETED')),
    http_status           SMALLINT,
    response_headers_json JSONB,
    response_body_bytes   BYTEA,
    created_at_ms         BIGINT NOT NULL,
    expires_at_ms         BIGINT NOT NULL,
    PRIMARY KEY (org_id, project_id, virtual_key_id, idempotency_key),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, virtual_key_id) REFERENCES virtual_keys(org_id, id) ON DELETE CASCADE
);

CREATE TABLE response_cache_entries (
    org_id              VARCHAR(40) NOT NULL,
    project_id          VARCHAR(40) NOT NULL,
    env_id              VARCHAR(40) NOT NULL,
    cache_key_sha256    CHAR(64) NOT NULL,
    model_deployment_id VARCHAR(40) NOT NULL,
    aead_alg            VARCHAR(32) NOT NULL DEFAULT 'AES-256-GCM' CHECK (aead_alg = 'AES-256-GCM'),
    nonce               BYTEA NOT NULL,
    ciphertext          BYTEA NOT NULL, -- Sealed under Project DEK with record_kind='cache_entry'
    created_at_ms       BIGINT NOT NULL,
    expires_at_ms       BIGINT NOT NULL,
    PRIMARY KEY (org_id, project_id, cache_key_sha256),
    FOREIGN KEY (org_id, project_id) REFERENCES project_encryption_keys(org_id, project_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, env_id) REFERENCES environments(org_id, id) ON DELETE CASCADE
);

-- -----------------------------------------------------------------------------
-- 22. MCP Governance Hub (mcp_servers, mcp_tools, mcp_tool_grants — TP-IMP-0036)
-- -----------------------------------------------------------------------------
CREATE TABLE mcp_servers (
    id                 VARCHAR(40) NOT NULL, -- 'mcp_<26_base32>'
    org_id             VARCHAR(40) NOT NULL,
    project_id         VARCHAR(40) NOT NULL,
    server_slug        VARCHAR(64) NOT NULL CHECK (server_slug NOT LIKE '%!_!_%' ESCAPE '!'),
    transport_kind     VARCHAR(24) NOT NULL CHECK (transport_kind IN ('STREAMABLE_HTTP', 'SIDECAR_STDIO')),
    endpoint_url       TEXT NOT NULL,
    allow_private_ip   BOOLEAN NOT NULL DEFAULT FALSE,
    allowed_cidrs_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    credential_id      VARCHAR(40),
    pinning_mode       VARCHAR(24) NOT NULL DEFAULT 'STRICT_LOCK' CHECK (pinning_mode IN ('STRICT_LOCK', 'WARN_ONLY')),
    status             VARCHAR(24) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'SUSPENDED', 'DISABLED')),
    version            BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms      BIGINT NOT NULL,
    updated_at_ms      BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    UNIQUE (org_id, project_id, server_slug),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, credential_id) REFERENCES secret_credentials(org_id, id) ON DELETE RESTRICT
);

CREATE TABLE mcp_tools (
    id                     VARCHAR(40) NOT NULL,
    org_id                 VARCHAR(40) NOT NULL,
    mcp_server_id          VARCHAR(40) NOT NULL,
    tool_name              VARCHAR(128) NOT NULL,
    side_effect_class      VARCHAR(24) NOT NULL DEFAULT 'mutating'
                           CHECK (side_effect_class IN ('read_only', 'mutating', 'destructive')),
    approved_schema_sha256 CHAR(64) NOT NULL,
    observed_schema_sha256 CHAR(64) NOT NULL,
    schema_drift_locked    BOOLEAN NOT NULL DEFAULT FALSE,
    tool_schema_json       JSONB NOT NULL,
    version                BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms          BIGINT NOT NULL,
    updated_at_ms          BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    UNIQUE (org_id, mcp_server_id, tool_name),
    FOREIGN KEY (org_id, mcp_server_id) REFERENCES mcp_servers(org_id, id) ON DELETE CASCADE
);

CREATE TABLE mcp_tool_grants (
    id                      VARCHAR(40) NOT NULL,
    org_id                  VARCHAR(40) NOT NULL,
    project_id              VARCHAR(40) NOT NULL,
    env_id                  VARCHAR(40) NOT NULL,
    virtual_key_id          VARCHAR(40),
    mcp_tool_id             VARCHAR(40) NOT NULL,
    granted_by_principal_id VARCHAR(40) REFERENCES principals(id) ON DELETE SET NULL,
    version                 BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms           BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, env_id) REFERENCES environments(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, virtual_key_id) REFERENCES virtual_keys(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, mcp_tool_id) REFERENCES mcp_tools(org_id, id) ON DELETE CASCADE
);

-- -----------------------------------------------------------------------------
-- 23. A2A Inter-Agent Plane (agent_endpoints & a2a_tasks SM-09 — TP-IMP-0042)
-- -----------------------------------------------------------------------------
CREATE TABLE agent_endpoints (
    id                  VARCHAR(40) NOT NULL, -- 'agt_<26_base32>'
    org_id              VARCHAR(40) NOT NULL,
    project_id          VARCHAR(40) NOT NULL,
    agent_slug          VARCHAR(64) NOT NULL,
    endpoint_url        TEXT NOT NULL,
    allow_private_ip    BOOLEAN NOT NULL DEFAULT FALSE,
    allowed_cidrs_json  JSONB NOT NULL DEFAULT '[]'::jsonb,
    credential_id       VARCHAR(40),
    data_region         VARCHAR(32) NOT NULL,
    agent_card_json     JSONB NOT NULL,
    max_delegation_hops SMALLINT NOT NULL DEFAULT 5 CHECK (max_delegation_hops BETWEEN 1 AND 5),
    status              VARCHAR(24) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'SUSPENDED', 'DISABLED')),
    version             BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms       BIGINT NOT NULL,
    updated_at_ms       BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    UNIQUE (org_id, project_id, agent_slug),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, credential_id) REFERENCES secret_credentials(org_id, id) ON DELETE RESTRICT
);

CREATE TABLE a2a_tasks (
    id                    VARCHAR(40) NOT NULL, -- 'tsk_<26_base32>'
    org_id                VARCHAR(40) NOT NULL,
    project_id            VARCHAR(40) NOT NULL,
    env_id                VARCHAR(40) NOT NULL,
    agent_endpoint_id     VARCHAR(40) NOT NULL,
    caller_virtual_key_id VARCHAR(40) NOT NULL,
    parent_task_id        VARCHAR(40),
    trace_root_id         VARCHAR(64) NOT NULL,
    hop_count             SMALLINT NOT NULL DEFAULT 1 CHECK (hop_count BETWEEN 1 AND 5),
    call_chain_ids_json   JSONB NOT NULL DEFAULT '[]'::jsonb,
    sub_budget_cap_micros BIGINT NOT NULL CHECK (sub_budget_cap_micros >= 0),
    reservation_id        VARCHAR(40),
    enforced_region       VARCHAR(32),
    state                 VARCHAR(24) NOT NULL DEFAULT 'SUBMITTED'
                          CHECK (state IN (
                              'SUBMITTED',
                              'WORKING',
                              'INPUT_REQUIRED',
                              'COMPLETED',
                              'FAILED',
                              'CANCELED'
                          )),
    version               BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms         BIGINT NOT NULL,
    updated_at_ms         BIGINT NOT NULL,
    completed_at_ms       BIGINT,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, env_id) REFERENCES environments(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, agent_endpoint_id) REFERENCES agent_endpoints(org_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (org_id, caller_virtual_key_id) REFERENCES virtual_keys(org_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (org_id, parent_task_id) REFERENCES a2a_tasks(org_id, id) ON DELETE RESTRICT
);

-- -----------------------------------------------------------------------------
-- 24. HITL Approvals (SM-06), Four-Eyes Dual Control & HMAC Webhooks (CR-08, 09)
-- -----------------------------------------------------------------------------
CREATE TABLE approval_requests (
    id                       VARCHAR(40) PRIMARY KEY, -- 'apr_<26_base32>'
    org_id                   VARCHAR(40) REFERENCES organizations(id) ON DELETE CASCADE, -- NULL for Instance actions
    project_id               VARCHAR(40),                                                -- NULL for Instance/Org actions
    action_kind              VARCHAR(32) NOT NULL
                             CHECK (action_kind IN (
                                 'MCP_DESTRUCTIVE_CALL',
                                 'EXTENSION_ESCALATION',
                                 'BUDGET_OVERRIDE',
                                 'DEBUG_CAPTURE_ENABLE'
                             )),
    requester_virtual_key_id VARCHAR(40),
    requester_principal_id   VARCHAR(40) REFERENCES principals(id) ON DELETE SET NULL,
    target_identifier        VARCHAR(160) NOT NULL,
    params_jcs_sha256        CHAR(64) NOT NULL,
    state                    VARCHAR(24) NOT NULL DEFAULT 'PENDING'
                             CHECK (state IN ('PENDING', 'APPROVED', 'CONSUMED', 'REJECTED', 'EXPIRED', 'CANCELLED')),
    pre_approval_token_hash  BYTEA UNIQUE,
    decided_by_principal_id  VARCHAR(40) REFERENCES principals(id) ON DELETE SET NULL,
    decision_reason          TEXT,
    version                  BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    expires_at_ms            BIGINT NOT NULL,
    created_at_ms            BIGINT NOT NULL,
    decided_at_ms            BIGINT,
    consumed_at_ms           BIGINT,
    CHECK (decided_by_principal_id IS NULL OR requester_principal_id IS NULL OR decided_by_principal_id <> requester_principal_id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, requester_virtual_key_id) REFERENCES virtual_keys(org_id, id) ON DELETE RESTRICT
);

CREATE TABLE admin_change_requests (
    id                     VARCHAR(40) PRIMARY KEY,
    org_id                 VARCHAR(40) REFERENCES organizations(id) ON DELETE CASCADE,
    action_type            VARCHAR(64) NOT NULL,
    target_type            VARCHAR(32) NOT NULL,
    target_id              VARCHAR(64) NOT NULL,
    payload_jcs_json       JSONB NOT NULL,
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

CREATE TABLE webhook_subscriptions (
    id                           VARCHAR(40) NOT NULL, -- 'whk_<26_base32>'
    org_id                       VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id                   VARCHAR(40),
    name                         VARCHAR(128) NOT NULL,
    target_url                   TEXT NOT NULL,
    allow_private_ip             BOOLEAN NOT NULL DEFAULT FALSE,
    allowed_cidrs_json           JSONB NOT NULL DEFAULT '[]'::jsonb,
    signing_secret_credential_id VARCHAR(40) NOT NULL,
    event_types_json             JSONB NOT NULL,
    enabled                      BOOLEAN NOT NULL DEFAULT TRUE,
    version                      BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms                BIGINT NOT NULL,
    updated_at_ms                BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, signing_secret_credential_id) REFERENCES secret_credentials(org_id, id) ON DELETE RESTRICT
);

CREATE TABLE webhook_deliveries (
    id                 VARCHAR(40) NOT NULL,
    org_id             VARCHAR(40) NOT NULL,
    subscription_id    VARCHAR(40) NOT NULL,
    event_id           VARCHAR(40) NOT NULL,
    event_type         VARCHAR(64) NOT NULL,
    payload_json       JSONB NOT NULL,
    attempt_count      SMALLINT NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    status             VARCHAR(24) NOT NULL DEFAULT 'PENDING'
                       CHECK (status IN ('PENDING', 'DELIVERED', 'RETRY_SCHEDULED', 'DEAD_LETTER')),
    next_attempt_at_ms BIGINT NOT NULL,
    last_http_status   SMALLINT,
    last_error         TEXT,
    created_at_ms      BIGINT NOT NULL,
    delivered_at_ms    BIGINT,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, subscription_id) REFERENCES webhook_subscriptions(org_id, id) ON DELETE CASCADE
);

-- -----------------------------------------------------------------------------
-- 25. Two-Phase Budget Ledger, Valkey Checkpoints, Settlements & Rollups
-- -----------------------------------------------------------------------------
CREATE TABLE budget_ledgers (
    id                             VARCHAR(40) NOT NULL, -- 'bdg_<26_base32>'
    org_id                         VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    scope_type                     VARCHAR(16) NOT NULL CHECK (scope_type IN ('ORG', 'PROJECT', 'ENV', 'VIRTUAL_KEY')),
    scope_id                       VARCHAR(40) NOT NULL,
    period_kind                    VARCHAR(16) NOT NULL CHECK (period_kind IN ('MONTHLY', 'DAILY', 'TOTAL_LIFETIME')),
    period_start_ms                BIGINT NOT NULL,
    period_end_ms                  BIGINT NOT NULL CHECK (period_end_ms > period_start_ms),
    hard_limit_micros              BIGINT NOT NULL CHECK (hard_limit_micros >= 0),
    reserved_hold_micros           BIGINT NOT NULL DEFAULT 0 CHECK (reserved_hold_micros >= 0),
    settled_exact_micros           BIGINT NOT NULL DEFAULT 0 CHECK (settled_exact_micros >= 0),
    settled_estimated_micros       BIGINT NOT NULL DEFAULT 0 CHECK (settled_estimated_micros >= 0),
    negative_carryover_micros      BIGINT NOT NULL DEFAULT 0 CHECK (negative_carryover_micros >= 0),
    soft_alert_thresholds_pct_json JSONB NOT NULL DEFAULT '[50, 80, 100]'::jsonb,
    last_alerted_threshold_pct     SMALLINT NOT NULL DEFAULT 0,
    version                        BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms                  BIGINT NOT NULL,
    updated_at_ms                  BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    UNIQUE (org_id, scope_type, scope_id, period_kind, period_start_ms)
);

CREATE TABLE budget_reservations (
    id                  VARCHAR(40) NOT NULL, -- 'rsv_<26_base32>'
    request_id          VARCHAR(40) NOT NULL,
    org_id              VARCHAR(40) NOT NULL,
    project_id          VARCHAR(40) NOT NULL,
    env_id              VARCHAR(40) NOT NULL,
    virtual_key_id      VARCHAR(40) NOT NULL,
    state               VARCHAR(32) NOT NULL
                        CHECK (state IN (
                            'RESERVED_HOLD',
                            'RELEASED_UNUSED',
                            'STREAMING_ACCRUING',
                            'SETTLED_EXACT',
                            'SETTLED_ESTIMATED',
                            'SETTLED_UNMETERED_FLAGGED',
                            'EXPIRED_RECLAIMED'
                        )),
    hold_micros         BIGINT NOT NULL CHECK (hold_micros >= 0),
    lease_expires_at_ms BIGINT NOT NULL,
    created_at_ms       BIGINT NOT NULL,
    updated_at_ms       BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    UNIQUE (org_id, request_id)
);

CREATE TABLE valkey_hold_checkpoints (
    org_id                  VARCHAR(40) NOT NULL,
    ledger_id               VARCHAR(40) NOT NULL,
    node_id                 VARCHAR(64) NOT NULL,
    active_hold_micro_usd   BIGINT NOT NULL DEFAULT 0 CHECK (active_hold_micro_usd >= 0),
    active_inflight_streams INTEGER NOT NULL DEFAULT 0 CHECK (active_inflight_streams >= 0),
    updated_at_ms           BIGINT NOT NULL,
    PRIMARY KEY (org_id, ledger_id, node_id),
    FOREIGN KEY (org_id, ledger_id) REFERENCES budget_ledgers(org_id, id) ON DELETE CASCADE
);

-- Monthly Range-Partitioned Append-Only Financial Settlement Ledger
CREATE TABLE usage_settlements (
    id                            VARCHAR(40) NOT NULL, -- 'set_<26_base32>'
    settled_at_ms                 BIGINT NOT NULL,
    request_id                    VARCHAR(40) NOT NULL,
    reservation_id                VARCHAR(40),
    org_id                        VARCHAR(40) NOT NULL,
    project_id                    VARCHAR(40) NOT NULL,
    env_id                        VARCHAR(40) NOT NULL,
    virtual_key_id                VARCHAR(40) NOT NULL,
    plane                         VARCHAR(16) NOT NULL CHECK (plane IN ('PROVIDER', 'MCP', 'A2A')),
    deployment_id                 VARCHAR(40),
    logical_model_slug            VARCHAR(128),
    billing_mode                  VARCHAR(24) NOT NULL CHECK (billing_mode IN ('COMMERCIAL_API', 'SELF_HOSTED_INFRA')),
    usage_accuracy                VARCHAR(24) NOT NULL CHECK (usage_accuracy IN ('EXACT', 'ESTIMATED', 'UNMETERED_FLAGGED')),
    input_tokens                  INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens                 INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cache_read_tokens             INTEGER NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
    cache_write_tokens            INTEGER NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
    reasoning_tokens              INTEGER NOT NULL DEFAULT 0 CHECK (reasoning_tokens >= 0),
    media_usage_json              JSONB NOT NULL DEFAULT '{}'::jsonb,
    applied_pricing_snapshot_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    total_cost_micros             BIGINT NOT NULL CHECK (total_cost_micros >= 0),
    PRIMARY KEY (org_id, id, settled_at_ms),
    UNIQUE (request_id, settled_at_ms)
) PARTITION BY RANGE (settled_at_ms);

-- Reference monthly partitions (automated in runtime by partition_manager.go & tanpopo db migrate-engine):
-- 2026-11-01T00:00:00Z (1793491200000) .. 2026-12-01T00:00:00Z (1796083200000)
CREATE TABLE usage_settlements_2026_11 PARTITION OF usage_settlements
    FOR VALUES FROM (1793491200000) TO (1796083200000);
-- 2026-12-01T00:00:00Z (1796083200000) .. 2027-01-01T00:00:00Z (1798761600000)
CREATE TABLE usage_settlements_2026_12 PARTITION OF usage_settlements
    FOR VALUES FROM (1796083200000) TO (1798761600000);

CREATE TRIGGER trg_usage_settlements_immutable
BEFORE UPDATE OR DELETE ON usage_settlements
FOR EACH ROW
EXECUTE FUNCTION tanpopo_prevent_immutable_mutation();

CREATE TABLE usage_rollups_hourly (
    org_id                  VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id              VARCHAR(40) NOT NULL,
    env_id                  VARCHAR(40) NOT NULL,
    virtual_key_id          VARCHAR(40) NOT NULL,
    logical_model_slug      VARCHAR(128) NOT NULL,
    bucket_start_ms         BIGINT NOT NULL,
    request_count           BIGINT NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    exact_micro_usd         BIGINT NOT NULL DEFAULT 0 CHECK (exact_micro_usd >= 0),
    estimated_micro_usd     BIGINT NOT NULL DEFAULT 0 CHECK (estimated_micro_usd >= 0),
    unmetered_request_count BIGINT NOT NULL DEFAULT 0 CHECK (unmetered_request_count >= 0),
    input_tokens_sum        BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens_sum >= 0),
    output_tokens_sum       BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens_sum >= 0),
    updated_at_ms           BIGINT NOT NULL,
    PRIMARY KEY (org_id, project_id, env_id, virtual_key_id, logical_model_slug, bucket_start_ms)
);

-- -----------------------------------------------------------------------------
-- 26. Observability Traces, Debug Blobs, Audit Hash Chain, Kill-Switches & Sync
-- -----------------------------------------------------------------------------
-- Monthly Range-Partitioned Technical Request Trace Metadata (0 Prompt Bytes)
CREATE TABLE request_traces_meta (
    request_id                VARCHAR(40) NOT NULL, -- 'trc_<26_base32>'
    created_at_ms             BIGINT NOT NULL,
    started_at_ms             BIGINT NOT NULL,
    org_id                    VARCHAR(40) NOT NULL,
    project_id                VARCHAR(40) NOT NULL,
    env_id                    VARCHAR(40) NOT NULL,
    virtual_key_id            VARCHAR(40),
    entry_point               VARCHAR(24) NOT NULL
                              CHECK (entry_point IN (
                                  'unified_http',
                                  'native_http',
                                  'mcp_http',
                                  'a2a_http',
                                  'playground'
                              )),
    otel_trace_id             CHAR(32),
    route_slug                VARCHAR(128),
    selected_deployment_id    VARCHAR(40),
    connection_id             VARCHAR(40),
    extension_installation_id VARCHAR(40),
    extension_version_id      VARCHAR(40),
    failover_attempts         SMALLINT NOT NULL DEFAULT 0 CHECK (failover_attempts >= 0),
    http_status               SMALLINT NOT NULL,
    canonical_error_code      VARCHAR(64),
    proxy_overhead_us         INTEGER NOT NULL CHECK (proxy_overhead_us >= 0),
    ttft_ms                   INTEGER,
    total_duration_ms         INTEGER NOT NULL CHECK (total_duration_ms >= 0),
    waterfall_stages_json     JSONB NOT NULL DEFAULT '[]'::jsonb,
    PRIMARY KEY (org_id, request_id, created_at_ms)
) PARTITION BY RANGE (created_at_ms);

CREATE TABLE request_traces_meta_2026_11 PARTITION OF request_traces_meta
    FOR VALUES FROM (1793491200000) TO (1796083200000);
CREATE TABLE request_traces_meta_2026_12 PARTITION OF request_traces_meta
    FOR VALUES FROM (1796083200000) TO (1798761600000);

CREATE TABLE debug_payload_blobs (
    request_id             VARCHAR(40) NOT NULL,
    org_id                 VARCHAR(40) NOT NULL,
    project_id             VARCHAR(40) NOT NULL,
    aead_alg               VARCHAR(32) NOT NULL DEFAULT 'AES-256-GCM' CHECK (aead_alg = 'AES-256-GCM'),
    nonce                  BYTEA NOT NULL,
    encrypted_payload_aead BYTEA NOT NULL, -- <= 256 KiB; Project DEK AAD = "tanpopo:v1:proj:" + org_id + ":" + project_id + ":debug_blob:" + request_id
    created_at_ms          BIGINT NOT NULL,
    expires_at_ms          BIGINT NOT NULL CHECK (expires_at_ms > created_at_ms AND expires_at_ms <= created_at_ms + 259200000),
    PRIMARY KEY (org_id, project_id, request_id),
    FOREIGN KEY (org_id, project_id) REFERENCES project_encryption_keys(org_id, project_id) ON DELETE CASCADE
);

-- Monthly Range-Partitioned Immutable Per-Org & SYSTEM JCS Hash-Chained Audit Log
-- Note: Per-chain seq_no monotonicity across concurrent HA writers is serialized via
--       SELECT pg_advisory_xact_lock(hashtextextended('audit:' || chain_key, 0));
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
    metadata_json JSONB NOT NULL,       -- Canonicalized via RFC 8785 JCS prior to hashing
    prev_hash     CHAR(64) NOT NULL,    -- seq_no=1: SHA-256("tanpopo:audit:genesis:v1:" + chain_key)
    entry_hash    CHAR(64) NOT NULL,    -- SHA-256(chain_key || ":" || seq_no || ":" || prev_hash || ":" || JCS(envelope))
    CHECK ((org_id IS NULL AND chain_key = 'SYSTEM') OR (org_id IS NOT NULL AND chain_key = org_id)),
    PRIMARY KEY (chain_key, seq_no, created_at_ms),
    UNIQUE (id, created_at_ms)
) PARTITION BY RANGE (created_at_ms);

CREATE TABLE audit_events_2026_11 PARTITION OF audit_events
    FOR VALUES FROM (1793491200000) TO (1796083200000);
CREATE TABLE audit_events_2026_12 PARTITION OF audit_events
    FOR VALUES FROM (1796083200000) TO (1798761600000);

CREATE TRIGGER trg_audit_events_immutable
BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW
EXECUTE FUNCTION tanpopo_prevent_immutable_mutation();

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
    active                    BOOLEAN NOT NULL DEFAULT TRUE,
    abort_active_streams      BOOLEAN NOT NULL DEFAULT TRUE,
    reason                    TEXT NOT NULL CHECK (LENGTH(reason) >= 5),
    triggered_by_principal_id VARCHAR(40) NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
    cleared_by_principal_id   VARCHAR(40) REFERENCES principals(id) ON DELETE RESTRICT,
    clear_reason              TEXT,
    version                   BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    triggered_at_ms           BIGINT NOT NULL,
    cleared_at_ms             BIGINT
);

CREATE TABLE config_changelog (
    config_epoch  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id        VARCHAR(40),
    entity_kind   VARCHAR(32) NOT NULL,
    entity_id     VARCHAR(64) NOT NULL,
    mutation_type VARCHAR(24) NOT NULL
                  CHECK (mutation_type IN ('UPSERT', 'DELETE', 'REVOKE', 'QUARANTINE', 'KEY_ROTATE', 'SHRED')),
    emergency     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at_ms BIGINT NOT NULL
);

CREATE TABLE distributed_leases (
    lease_name     VARCHAR(64) PRIMARY KEY,
    holder_node_id VARCHAR(64) NOT NULL,
    fencing_token  BIGINT NOT NULL DEFAULT 1 CHECK (fencing_token >= 1),
    acquired_at_ms BIGINT NOT NULL,
    expires_at_ms  BIGINT NOT NULL CHECK (expires_at_ms > acquired_at_ms)
);

-- =============================================================================
-- Query-Plan-Justified Secondary & Partial Indexes
-- =============================================================================

CREATE INDEX idx_vkeys_env_active
    ON virtual_keys (org_id, project_id, env_id)
    WHERE state IN ('CREATED_ACTIVE', 'ROTATING_DUAL_VALID');

CREATE INDEX idx_rsv_orphan_lease
    ON budget_reservations (lease_expires_at_ms)
    WHERE state IN ('RESERVED_HOLD', 'STREAMING_ACCRUING');

CREATE INDEX idx_hold_checkpoints_updated
    ON valkey_hold_checkpoints (org_id, ledger_id, updated_at_ms DESC);

CREATE INDEX idx_settlements_chargeback
    ON usage_settlements (org_id, project_id, usage_accuracy, settled_at_ms DESC);

CREATE INDEX idx_traces_project_time
    ON request_traces_meta (org_id, project_id, env_id, created_at_ms DESC);

CREATE INDEX idx_traces_error_lookup
    ON request_traces_meta (org_id, canonical_error_code, created_at_ms DESC)
    WHERE canonical_error_code IS NOT NULL;

CREATE INDEX idx_traces_blast_radius_ext
    ON request_traces_meta (extension_installation_id, created_at_ms DESC)
    WHERE extension_installation_id IS NOT NULL;

CREATE INDEX idx_traces_blast_radius_conn
    ON request_traces_meta (org_id, connection_id, created_at_ms DESC)
    WHERE connection_id IS NOT NULL;

CREATE INDEX idx_debug_blobs_ttl
    ON debug_payload_blobs (expires_at_ms);

CREATE INDEX idx_audit_org_time
    ON audit_events (chain_key, created_at_ms DESC);

CREATE INDEX idx_audit_target_time
    ON audit_events (target_type, target_id, created_at_ms DESC);

CREATE INDEX idx_webhook_deliveries_queue
    ON webhook_deliveries (status, next_attempt_at_ms)
    WHERE status IN ('PENDING', 'RETRY_SCHEDULED');

CREATE INDEX idx_config_changelog_org_epoch
    ON config_changelog (org_id, config_epoch DESC);

-- =============================================================================
-- Mandatory Engine-Level Multi-Tenant Isolation: ENABLE + FORCE ROW LEVEL SECURITY
-- Every tenant-scoped transaction executes:
--   SET LOCAL app.current_org_id = '<org_id>';
-- Instance-level OPERATOR transactions additionally set:
--   SET LOCAL app.is_instance_operator = 'true';
-- When neither session variable is set, every policy below fails closed (0 rows).
-- =============================================================================

-- A. Strictly Tenant-Scoped Tables (org_id NOT NULL)
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE projects FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON projects
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE environments ENABLE ROW LEVEL SECURITY;
ALTER TABLE environments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON environments
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE org_encryption_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_encryption_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON org_encryption_keys
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE project_encryption_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_encryption_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON project_encryption_keys
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE virtual_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE virtual_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON virtual_keys
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE secret_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE secret_credentials FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON secret_credentials
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE oauth_token_states ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_token_states FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON oauth_token_states
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE oauth_token_leases ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_token_leases FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON oauth_token_leases
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE oauth_authorization_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_authorization_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON oauth_authorization_sessions
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE org_extension_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_extension_policies FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON org_extension_policies
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE provider_connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE provider_connections FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON provider_connections
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE connection_secret_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE connection_secret_bindings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connection_secret_bindings
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE model_deployments ENABLE ROW LEVEL SECURITY;
ALTER TABLE model_deployments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON model_deployments
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE route_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE route_policies FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON route_policies
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE route_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE route_targets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON route_targets
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE policies FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON policies
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE idempotency_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_records FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON idempotency_records
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE response_cache_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE response_cache_entries FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON response_cache_entries
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE mcp_servers ENABLE ROW LEVEL SECURITY;
ALTER TABLE mcp_servers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mcp_servers
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE mcp_tools ENABLE ROW LEVEL SECURITY;
ALTER TABLE mcp_tools FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mcp_tools
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE mcp_tool_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE mcp_tool_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mcp_tool_grants
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE agent_endpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_endpoints FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_endpoints
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE a2a_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE a2a_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON a2a_tasks
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE webhook_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON webhook_subscriptions
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON webhook_deliveries
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE budget_ledgers ENABLE ROW LEVEL SECURITY;
ALTER TABLE budget_ledgers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON budget_ledgers
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE budget_reservations ENABLE ROW LEVEL SECURITY;
ALTER TABLE budget_reservations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON budget_reservations
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE valkey_hold_checkpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE valkey_hold_checkpoints FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON valkey_hold_checkpoints
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE usage_settlements ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage_settlements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON usage_settlements
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));
ALTER TABLE usage_settlements_2026_11 ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage_settlements_2026_11 FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON usage_settlements_2026_11
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));
ALTER TABLE usage_settlements_2026_12 ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage_settlements_2026_12 FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON usage_settlements_2026_12
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE usage_rollups_hourly ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage_rollups_hourly FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON usage_rollups_hourly
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE request_traces_meta ENABLE ROW LEVEL SECURITY;
ALTER TABLE request_traces_meta FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON request_traces_meta
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));
ALTER TABLE request_traces_meta_2026_11 ENABLE ROW LEVEL SECURITY;
ALTER TABLE request_traces_meta_2026_11 FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON request_traces_meta_2026_11
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));
ALTER TABLE request_traces_meta_2026_12 ENABLE ROW LEVEL SECURITY;
ALTER TABLE request_traces_meta_2026_12 FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON request_traces_meta_2026_12
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

ALTER TABLE debug_payload_blobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE debug_payload_blobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON debug_payload_blobs
    USING (org_id = current_setting('app.current_org_id', true))
    WITH CHECK (org_id = current_setting('app.current_org_id', true));

-- B. Hybrid Tenant + Instance-Scoped Tables (Nullable org_id; Fail-Closed by Default)
ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organizations
    USING (
        id = current_setting('app.current_org_id', true)
        OR current_setting('app.is_instance_operator', true) = 'true'
    )
    WITH CHECK (
        id = current_setting('app.current_org_id', true)
        OR current_setting('app.is_instance_operator', true) = 'true'
    );

ALTER TABLE principals ENABLE ROW LEVEL SECURITY;
ALTER TABLE principals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON principals
    USING (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    )
    WITH CHECK (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    );

ALTER TABLE role_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_bindings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON role_bindings
    USING (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    )
    WITH CHECK (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    );

ALTER TABLE logical_models ENABLE ROW LEVEL SECURITY;
ALTER TABLE logical_models FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON logical_models
    USING (
        org_id IS NULL
        OR org_id = current_setting('app.current_org_id', true)
    )
    WITH CHECK (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    );

ALTER TABLE approval_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON approval_requests
    USING (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    )
    WITH CHECK (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    );

ALTER TABLE admin_change_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_change_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON admin_change_requests
    USING (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    )
    WITH CHECK (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    );

ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit_events
    USING (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    )
    WITH CHECK (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    );
ALTER TABLE audit_events_2026_11 ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events_2026_11 FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit_events_2026_11
    USING (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    )
    WITH CHECK (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    );
ALTER TABLE audit_events_2026_12 ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events_2026_12 FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit_events_2026_12
    USING (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    )
    WITH CHECK (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    );

ALTER TABLE emergency_kill_switches ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_kill_switches FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_kill_switches
    USING (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    )
    WITH CHECK (
        org_id = current_setting('app.current_org_id', true)
        OR (org_id IS NULL AND current_setting('app.is_instance_operator', true) = 'true')
    );
