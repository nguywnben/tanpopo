-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-Only SQLite WAL Migration 0012: Two-Phase Integer Micro-USD Budget Ledger (TP-IMP-0021)

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
    soft_alert_thresholds_pct_json TEXT NOT NULL DEFAULT '[50, 80, 100]',
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

CREATE TABLE budget_reservation_ledgers (
    org_id         VARCHAR(40) NOT NULL,
    reservation_id VARCHAR(40) NOT NULL,
    ledger_id      VARCHAR(40) NOT NULL,
    held_micros    BIGINT NOT NULL CHECK (held_micros >= 0),
    PRIMARY KEY (org_id, reservation_id, ledger_id),
    FOREIGN KEY (org_id, reservation_id) REFERENCES budget_reservations(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, ledger_id) REFERENCES budget_ledgers(org_id, id) ON DELETE CASCADE
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
    media_usage_json              TEXT NOT NULL DEFAULT '{}',
    applied_pricing_snapshot_json TEXT NOT NULL DEFAULT '{}',
    total_cost_micros             BIGINT NOT NULL CHECK (total_cost_micros >= 0),
    PRIMARY KEY (org_id, id, settled_at_ms),
    UNIQUE (request_id, settled_at_ms)
);

CREATE TRIGGER trg_usage_settlements_no_update
BEFORE UPDATE ON usage_settlements
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'usage_settlements_immutable');
END;

CREATE TRIGGER trg_usage_settlements_no_delete
BEFORE DELETE ON usage_settlements
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'usage_settlements_immutable');
END;

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

CREATE INDEX idx_ledgers_scope_active
    ON budget_ledgers (org_id, scope_type, scope_id, period_start_ms, period_end_ms);

CREATE INDEX idx_rsv_orphan_lease
    ON budget_reservations (lease_expires_at_ms)
    WHERE state IN ('RESERVED_HOLD', 'STREAMING_ACCRUING');

CREATE INDEX idx_rsv_ledgers_ledger
    ON budget_reservation_ledgers (org_id, ledger_id);

CREATE INDEX idx_hold_checkpoints_updated
    ON valkey_hold_checkpoints (org_id, ledger_id, updated_at_ms DESC);

CREATE INDEX idx_settlements_chargeback
    ON usage_settlements (org_id, project_id, usage_accuracy, settled_at_ms DESC);

CREATE INDEX idx_settlements_reservation
    ON usage_settlements (org_id, reservation_id)
    WHERE reservation_id IS NOT NULL;

CREATE INDEX idx_settlements_rollup
    ON usage_settlements (settled_at_ms, org_id, project_id, env_id, virtual_key_id);
