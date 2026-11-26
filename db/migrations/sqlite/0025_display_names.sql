-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-Only migration persisting display_name on environments and logical_models (DEF-055, DEF-057).

ALTER TABLE environments
    ADD COLUMN display_name VARCHAR(128) NOT NULL DEFAULT '';

ALTER TABLE logical_models
    ADD COLUMN display_name VARCHAR(128) NOT NULL DEFAULT '';
