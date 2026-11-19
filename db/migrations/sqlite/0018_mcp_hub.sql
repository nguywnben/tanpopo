-- Tanpopo Migration 0018: MCP Governance Hub
-- Unit: TP-IMP-0036 (DEC-073 additive migration slot)

CREATE TABLE mcp_servers (
    id                 VARCHAR(40) NOT NULL, -- 'mcp_<26_base32>'
    org_id             VARCHAR(40) NOT NULL,
    project_id         VARCHAR(40) NOT NULL,
    server_slug        VARCHAR(64) NOT NULL CHECK (server_slug NOT LIKE '%!_!_%' ESCAPE '!'),
    display_name       VARCHAR(128) NOT NULL CHECK (length(trim(display_name)) > 0),
    transport_kind     VARCHAR(24) NOT NULL CHECK (transport_kind IN ('STREAMABLE_HTTP', 'SIDECAR_STDIO')),
    endpoint_url       TEXT NOT NULL,
    allow_private_ip   BOOLEAN NOT NULL DEFAULT 0 CHECK (allow_private_ip IN (0, 1)),
    allowed_cidrs_json TEXT NOT NULL DEFAULT '[]',
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
    org_id                  VARCHAR(40) NOT NULL,
    mcp_server_id           VARCHAR(40) NOT NULL,
    tool_name               VARCHAR(128) NOT NULL,
    side_effect_class       VARCHAR(24) NOT NULL DEFAULT 'mutating'
                            CHECK (side_effect_class IN ('read_only', 'mutating', 'destructive')),
    approved_schema_sha256  CHAR(64) NOT NULL,
    observed_schema_sha256  CHAR(64) NOT NULL,
    schema_drift_locked     BOOLEAN NOT NULL DEFAULT 0 CHECK (schema_drift_locked IN (0, 1)),
    tool_schema_json        TEXT NOT NULL,
    version                 BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms           BIGINT NOT NULL,
    updated_at_ms           BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    UNIQUE (org_id, mcp_server_id, tool_name),
    FOREIGN KEY (org_id, mcp_server_id) REFERENCES mcp_servers(org_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_mcp_tools_org_server
    ON mcp_tools (org_id, mcp_server_id, tool_name);

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
    UNIQUE (org_id, project_id, env_id, virtual_key_id, mcp_tool_id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, env_id) REFERENCES environments(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, virtual_key_id) REFERENCES virtual_keys(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, mcp_tool_id) REFERENCES mcp_tools(org_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_mcp_tool_grants_scope
    ON mcp_tool_grants (org_id, project_id, env_id, virtual_key_id, mcp_tool_id);
