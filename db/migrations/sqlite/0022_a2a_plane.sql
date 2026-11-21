-- Tanpopo Migration 0022: A2A Inter-Agent Plane (agent_endpoints & a2a_tasks SM-09)
-- Unit: TP-IMP-0042 (parity with db/spec-check/sqlite_schema.sql and postgres/0001_full_schema_rls_partitions.sql)

CREATE TABLE IF NOT EXISTS agent_endpoints (
    id                  VARCHAR(40) NOT NULL, -- 'agt_<26_base32>'
    org_id              VARCHAR(40) NOT NULL,
    project_id          VARCHAR(40) NOT NULL,
    agent_slug          VARCHAR(64) NOT NULL,
    endpoint_url        TEXT NOT NULL,
    allow_private_ip    BOOLEAN NOT NULL DEFAULT 0 CHECK (allow_private_ip IN (0, 1)),
    allowed_cidrs_json  TEXT NOT NULL DEFAULT '[]',
    credential_id       VARCHAR(40),
    data_region         VARCHAR(32) NOT NULL,
    agent_card_json     TEXT NOT NULL,
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

CREATE INDEX IF NOT EXISTS idx_agent_endpoints_org_project
    ON agent_endpoints (org_id, project_id, status, agent_slug);

CREATE TABLE IF NOT EXISTS a2a_tasks (
    id                     VARCHAR(40) NOT NULL, -- 'tsk_<26_base32>'
    org_id                 VARCHAR(40) NOT NULL,
    project_id             VARCHAR(40) NOT NULL,
    env_id                 VARCHAR(40) NOT NULL,
    agent_endpoint_id      VARCHAR(40) NOT NULL,
    caller_virtual_key_id  VARCHAR(40) NOT NULL,
    parent_task_id         VARCHAR(40),
    trace_root_id          VARCHAR(64) NOT NULL,
    hop_count              SMALLINT NOT NULL DEFAULT 1 CHECK (hop_count BETWEEN 1 AND 5),
    call_chain_ids_json    TEXT NOT NULL DEFAULT '[]',
    sub_budget_cap_micros  BIGINT NOT NULL CHECK (sub_budget_cap_micros >= 0),
    settled_cost_micros    BIGINT NOT NULL DEFAULT 0 CHECK (settled_cost_micros >= 0),
    reservation_id         VARCHAR(40),
    enforced_region        VARCHAR(32),
    artifacts_json         TEXT NOT NULL DEFAULT '[]',
    state                  VARCHAR(24) NOT NULL DEFAULT 'SUBMITTED'
                           CHECK (state IN (
                               'SUBMITTED',
                               'WORKING',
                               'INPUT_REQUIRED',
                               'COMPLETED',
                               'FAILED',
                               'CANCELED'
                           )),
    version                BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms          BIGINT NOT NULL,
    updated_at_ms          BIGINT NOT NULL,
    completed_at_ms        BIGINT,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, env_id) REFERENCES environments(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, agent_endpoint_id) REFERENCES agent_endpoints(org_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (org_id, caller_virtual_key_id) REFERENCES virtual_keys(org_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (org_id, parent_task_id) REFERENCES a2a_tasks(org_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_a2a_tasks_org_project_state
    ON a2a_tasks (org_id, project_id, state, created_at_ms);
