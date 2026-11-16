-- =============================================================================
-- Tanpopo Migration 0015: Persist route_policies.request_timeout_ms
-- Dialect : Portable SQLite WAL (Expand-Only DDL, parity with postgres_schema.sql)
-- Unit Ref: TP-IMP-0025 (SPEC-API-01 / Admin Control Plane REST API Part 2)
-- =============================================================================

ALTER TABLE route_policies
    ADD COLUMN request_timeout_ms INTEGER NOT NULL DEFAULT 60000 CHECK (request_timeout_ms BETWEEN 500 AND 300000);
