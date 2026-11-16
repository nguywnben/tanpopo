-- =============================================================================
-- Tanpopo Migration 0011: Capability-Aware Route Policies & Ordered Targets
-- Dialect : Portable SQLite WAL (parity with db/spec-check/sqlite_schema.sql)
-- Unit Ref: TP-IMP-0020 (SPEC-CORE-01 / TP-FR-0601..0604)
-- =============================================================================

CREATE TABLE IF NOT EXISTS route_policies (
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

CREATE TABLE IF NOT EXISTS route_targets (
    org_id         VARCHAR(40) NOT NULL,
    route_id       VARCHAR(40) NOT NULL,
    deployment_id  VARCHAR(40) NOT NULL,
    priority_order SMALLINT NOT NULL,
    weight         SMALLINT NOT NULL DEFAULT 100 CHECK (weight >= 0),
    PRIMARY KEY (org_id, route_id, deployment_id),
    FOREIGN KEY (org_id, route_id) REFERENCES route_policies(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, deployment_id) REFERENCES model_deployments(org_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_route_policies_lookup
    ON route_policies (org_id, env_id, route_slug);

CREATE INDEX IF NOT EXISTS idx_route_targets_order
    ON route_targets (org_id, route_id, priority_order, deployment_id);
