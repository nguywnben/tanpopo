-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-Only SQLite WAL Migration 0013: Request Trace Metadata & Opt-In Debug Payload Blobs (TP-IMP-0022)

CREATE TABLE request_traces_meta (
    request_id                VARCHAR(40) NOT NULL, -- 'trc_<26_base32>' / 'req_<26_base32>'
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
    waterfall_stages_json     TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (org_id, request_id, created_at_ms)
);

CREATE TABLE debug_payload_blobs (
    request_id             VARCHAR(40) NOT NULL,
    org_id                 VARCHAR(40) NOT NULL,
    project_id             VARCHAR(40) NOT NULL,
    aead_alg               VARCHAR(32) NOT NULL DEFAULT 'AES-256-GCM' CHECK (aead_alg = 'AES-256-GCM'),
    nonce                  BLOB NOT NULL,
    encrypted_payload_aead BLOB NOT NULL, -- <= 256 KiB; Project DEK AAD = "tanpopo:v1:proj:" + org_id + ":" + project_id + ":debug_blob:" + request_id
    created_at_ms          BIGINT NOT NULL,
    expires_at_ms          BIGINT NOT NULL CHECK (expires_at_ms > created_at_ms AND expires_at_ms <= created_at_ms + 259200000),
    PRIMARY KEY (org_id, project_id, request_id),
    FOREIGN KEY (org_id, project_id) REFERENCES project_encryption_keys(org_id, project_id) ON DELETE CASCADE
);

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
