-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-Only migration adding cluster node heartbeat & snapshot-epoch inventory (DEF-072).

CREATE TABLE IF NOT EXISTS cluster_nodes (
    node_id              VARCHAR(128) PRIMARY KEY,
    role                 VARCHAR(24) NOT NULL CHECK (role IN ('all', 'control-plane', 'data-plane')),
    listen_addr          VARCHAR(128) NOT NULL DEFAULT '',
    config_epoch         BIGINT NOT NULL DEFAULT 0 CHECK (config_epoch >= 0),
    snapshot_age_ms      BIGINT NOT NULL DEFAULT 0 CHECK (snapshot_age_ms >= 0),
    health               VARCHAR(24) NOT NULL DEFAULT 'unknown',
    storage_engine       VARCHAR(24) NOT NULL DEFAULT 'postgres',
    wal_bytes            BIGINT NOT NULL DEFAULT 0 CHECK (wal_bytes >= 0),
    loaded_kek_ids_json  JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_heartbeat_ms    BIGINT NOT NULL,
    started_at_ms        BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cluster_nodes_heartbeat
    ON cluster_nodes (last_heartbeat_ms DESC);
