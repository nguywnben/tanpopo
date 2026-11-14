-- =============================================================================
-- Tanpopo Migration 0002: Three-Tier Envelope Encryption Vault & Secret Store
-- Dialect : Portable SQLite WAL (parity with db/spec-check/sqlite_schema.sql)
-- Unit Ref: TP-IMP-0008 (SPEC-SEC-03 / ARCH-DATA-01 / CR-06)
-- =============================================================================

CREATE TABLE IF NOT EXISTS org_encryption_keys (
    org_id          VARCHAR(40) PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    kek_key_id      VARCHAR(128) NOT NULL,
    aead_alg        VARCHAR(32) NOT NULL DEFAULT 'AES-256-GCM' CHECK (aead_alg = 'AES-256-GCM'),
    nonce           BLOB NOT NULL, -- 96-bit CSPRNG nonce
    wrapped_org_dek BLOB NOT NULL, -- 256-bit Org DEK AEAD-sealed under Master KEK
    dek_version     BIGINT NOT NULL DEFAULT 1 CHECK (dek_version >= 1),
    created_at_ms   BIGINT NOT NULL,
    rotated_at_ms   BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS project_encryption_keys (
    org_id              VARCHAR(40) NOT NULL,
    project_id          VARCHAR(40) NOT NULL,
    org_dek_version     BIGINT NOT NULL DEFAULT 1 CHECK (org_dek_version >= 1),
    aead_alg            VARCHAR(32) NOT NULL DEFAULT 'AES-256-GCM' CHECK (aead_alg = 'AES-256-GCM'),
    nonce               BLOB NOT NULL, -- 96-bit CSPRNG nonce
    wrapped_project_dek BLOB NOT NULL, -- 256-bit Project DEK AEAD-sealed under Org DEK
    dek_version         BIGINT NOT NULL DEFAULT 1 CHECK (dek_version >= 1),
    created_at_ms       BIGINT NOT NULL,
    rotated_at_ms       BIGINT NOT NULL,
    PRIMARY KEY (org_id, project_id),
    FOREIGN KEY (org_id) REFERENCES org_encryption_keys(org_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS secret_credentials (
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
    nonce            BLOB NOT NULL,
    ciphertext       BLOB NOT NULL,
    bound_audience   TEXT,                 -- RFC 8707 resource / audience host binding
    key_fingerprint  VARCHAR(64) NOT NULL, -- Irreversible SHA-256 display digest
    version          BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms    BIGINT NOT NULL,
    updated_at_ms    BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, owner_project_id) REFERENCES project_encryption_keys(org_id, project_id) ON DELETE CASCADE
);
