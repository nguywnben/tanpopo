-- Copyright 2026 The Tanpopo Authors.
-- Licensed under the Apache License, Version 2.0.
-- Expand-Only marker migration matching PostgreSQL 0006_widen_budget_ledger_ids (DEF-086).
-- SQLite already stores VARCHAR(...) columns with unbounded TEXT affinity, so existing
-- rows (including Console-derived bgt_<scope>_<id>_<period> IDs up to 128 chars) are preserved intact.

SELECT 1;
