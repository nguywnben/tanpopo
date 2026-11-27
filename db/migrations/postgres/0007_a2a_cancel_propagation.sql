-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-Only migration persisting downstream cancellation propagation outcome
-- and detail on a2a_tasks (DEF-069).

ALTER TABLE a2a_tasks
    ADD COLUMN IF NOT EXISTS downstream_cancel_status VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS downstream_cancel_detail TEXT NOT NULL DEFAULT '';
