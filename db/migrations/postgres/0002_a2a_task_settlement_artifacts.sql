-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-Only migration adding durable settlement cost and artifacts persistence on a2a_tasks (TP-IMP-0042).

ALTER TABLE a2a_tasks
    ADD COLUMN IF NOT EXISTS settled_cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (settled_cost_micros >= 0),
    ADD COLUMN IF NOT EXISTS artifacts_json JSONB NOT NULL DEFAULT '[]'::jsonb;
