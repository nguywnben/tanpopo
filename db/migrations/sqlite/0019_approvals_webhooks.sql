-- Tanpopo Migration 0019: MCP Approvals and HMAC Webhooks
-- Unit: TP-IMP-0037 (DEC-075 additive migration slot)

CREATE TABLE approval_requests (
    id                        VARCHAR(40) PRIMARY KEY, -- 'apr_<26_base32>'
    org_id                    VARCHAR(40) REFERENCES organizations(id) ON DELETE CASCADE, -- NULL for Instance actions
    project_id                VARCHAR(40),                                                -- NULL for Instance/Org actions
    action_kind               VARCHAR(32) NOT NULL
                              CHECK (action_kind IN (
                                  'MCP_DESTRUCTIVE_CALL',
                                  'EXTENSION_ESCALATION',
                                  'BUDGET_OVERRIDE',
                                  'DEBUG_CAPTURE_ENABLE'
                              )),
    requester_virtual_key_id  VARCHAR(40),
    requester_principal_id    VARCHAR(40) REFERENCES principals(id) ON DELETE SET NULL,
    target_identifier         VARCHAR(160) NOT NULL,
    payload_preview_json     TEXT NOT NULL,
    params_jcs_sha256         CHAR(64) NOT NULL,
    state                     VARCHAR(24) NOT NULL DEFAULT 'PENDING'
                              CHECK (state IN ('PENDING', 'APPROVED', 'CONSUMED', 'REJECTED', 'EXPIRED', 'CANCELLED')),
    pre_approval_token_hash   BLOB UNIQUE,
    decided_by_principal_id   VARCHAR(40) REFERENCES principals(id) ON DELETE SET NULL,
    decision_reason           TEXT,
    version                   BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    expires_at_ms             BIGINT NOT NULL,
    created_at_ms             BIGINT NOT NULL,
    decided_at_ms             BIGINT,
    consumed_at_ms            BIGINT,
    CHECK (decided_by_principal_id IS NULL OR requester_principal_id IS NULL OR decided_by_principal_id <> requester_principal_id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, requester_virtual_key_id) REFERENCES virtual_keys(org_id, id) ON DELETE RESTRICT
);

CREATE TABLE webhook_subscriptions (
    id                           VARCHAR(40) NOT NULL, -- 'whk_<26_base32>'
    org_id                       VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id                   VARCHAR(40),
    name                         VARCHAR(128) NOT NULL,
    target_url                   TEXT NOT NULL,
    target_url_credential_id     VARCHAR(40) NOT NULL,
    allow_private_ip             BOOLEAN NOT NULL DEFAULT 0 CHECK (allow_private_ip IN (0, 1)),
    allowed_cidrs_json           TEXT NOT NULL DEFAULT '[]',
    signing_secret_credential_id VARCHAR(40) NOT NULL,
    event_types_json             TEXT NOT NULL,
    enabled                      BOOLEAN NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    version                      BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms                BIGINT NOT NULL,
    updated_at_ms                BIGINT NOT NULL,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, signing_secret_credential_id) REFERENCES secret_credentials(org_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (org_id, target_url_credential_id) REFERENCES secret_credentials(org_id, id) ON DELETE RESTRICT
);

CREATE TABLE webhook_deliveries (
    id                 VARCHAR(40) NOT NULL,
    org_id             VARCHAR(40) NOT NULL,
    subscription_id    VARCHAR(40) NOT NULL,
    event_id           VARCHAR(40) NOT NULL,
    event_type         VARCHAR(64) NOT NULL,
    payload_json       TEXT NOT NULL,
    attempt_count      SMALLINT NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    status             VARCHAR(24) NOT NULL DEFAULT 'PENDING'
                       CHECK (status IN ('PENDING', 'DELIVERED', 'RETRY_SCHEDULED', 'DEAD_LETTER')),
    next_attempt_at_ms BIGINT NOT NULL,
    last_http_status   SMALLINT,
    last_error         TEXT,
    created_at_ms      BIGINT NOT NULL,
    delivered_at_ms    BIGINT,
    terminal_at_ms     BIGINT,
    PRIMARY KEY (org_id, id),
    UNIQUE (id),
    FOREIGN KEY (org_id, subscription_id) REFERENCES webhook_subscriptions(org_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_webhook_deliveries_queue
    ON webhook_deliveries (status, next_attempt_at_ms)
    WHERE status IN ('PENDING', 'RETRY_SCHEDULED');

CREATE UNIQUE INDEX idx_webhook_delivery_event_subscription
    ON webhook_deliveries (org_id, subscription_id, event_id);

CREATE TABLE webhook_event_outbox (
    id              VARCHAR(40) PRIMARY KEY,
    org_id          VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id      VARCHAR(40),
    event_type      VARCHAR(64) NOT NULL,
    payload_json    TEXT NOT NULL,
    created_at_ms   BIGINT NOT NULL,
    dispatched_at_ms BIGINT,
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_webhook_event_outbox_pending
    ON webhook_event_outbox (created_at_ms, id) WHERE dispatched_at_ms IS NULL;
