-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-only OAuth authorization state, token state, and refresh leases (TP-IMP-0034).

CREATE TABLE oauth_token_states (
    org_id               VARCHAR(40) NOT NULL,
    credential_id        VARCHAR(40) NOT NULL,
    bound_principal_id   VARCHAR(40) REFERENCES principals(id) ON DELETE SET NULL,
    state                VARCHAR(40) NOT NULL DEFAULT 'UNBOUND'
                         CHECK (state IN (
                             'UNBOUND', 'AUTHORIZING', 'TOKEN_VALID',
                             'REFRESHING_SINGLEFLIGHT', 'REFRESH_GRACE_DEGRADED',
                             'REAUTH_REQUIRED', 'AUTH_EXPIRED_REAUTH_REQUIRED',
                             'INVALIDATED_HOST_CHANGE', 'REVOKED'
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
    expires_at_ms       BIGINT NOT NULL CHECK (expires_at_ms > acquired_at_ms),
    PRIMARY KEY (org_id, credential_id),
    FOREIGN KEY (org_id, credential_id) REFERENCES secret_credentials(org_id, id) ON DELETE CASCADE
);

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
    sealed_code_verifier  BLOB, -- AEAD-sealed PKCE verifier or device_code (never persisted as plaintext)
    verifier_nonce        BLOB,
    device_code           VARCHAR(255),
    user_code             VARCHAR(64),
    verification_uri      TEXT,
    poll_interval_ms      INTEGER CHECK (poll_interval_ms IS NULL OR poll_interval_ms > 0),
    last_poll_at_ms       BIGINT,
    bound_resource_uri    TEXT,
    session_status        VARCHAR(24) NOT NULL DEFAULT 'PENDING'
                          CHECK (session_status IN ('PENDING', 'COMPLETED', 'EXPIRED', 'CONSUMED', 'FAILED')),
    expires_at_ms         BIGINT NOT NULL,
    created_at_ms         BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    CHECK (expires_at_ms > created_at_ms),
    CHECK (device_code IS NULL),
    CHECK ((grant_type IN ('AUTHORIZATION_CODE_PKCE', 'DEVICE_CODE') AND sealed_code_verifier IS NOT NULL AND verifier_nonce IS NOT NULL)
        OR (grant_type = 'CLIENT_CREDENTIALS' AND sealed_code_verifier IS NULL AND verifier_nonce IS NULL))
);

CREATE INDEX idx_oauth_sessions_pending_expiry
    ON oauth_authorization_sessions (org_id, principal_id, expires_at_ms)
    WHERE session_status = 'PENDING';

CREATE INDEX idx_oauth_token_states_refresh
    ON oauth_token_states (state, access_expires_at_ms)
    WHERE state IN ('TOKEN_VALID', 'REFRESH_GRACE_DEGRADED');
