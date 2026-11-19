-- =============================================================================
-- Tanpopo Migration 0017: Marketplace Feed Cursors, Cached Index & Revocation Outbox
-- Unit Ref: TP-IMP-0035 (signed feed anti-rollback and durable quarantine delivery)
-- =============================================================================

CREATE TABLE marketplace_feed_cursors (
    feed_kind          VARCHAR(24) PRIMARY KEY CHECK (feed_kind IN ('index', 'revocations')),
    version_serial     BIGINT NOT NULL CHECK (version_serial > 0),
    feed_expires_at_ms BIGINT NOT NULL,
    updated_at_ms      BIGINT NOT NULL
);

CREATE TABLE marketplace_index_entries (
    package_slug VARCHAR(160) NOT NULL,
    version      VARCHAR(64) NOT NULL,
    digest       CHAR(64) NOT NULL CHECK (length(digest) = 64),
    oci_uri      TEXT NOT NULL,
    updated_at_ms BIGINT NOT NULL,
    PRIMARY KEY (package_slug, version)
);

CREATE TABLE extension_revocation_actions (
    id                  VARCHAR(40) PRIMARY KEY,
    installation_id     VARCHAR(40) NOT NULL REFERENCES extension_installations(id) ON DELETE CASCADE,
    package_slug        VARCHAR(160) NOT NULL,
    version_or_sha256   VARCHAR(128) NOT NULL,
    severity            VARCHAR(32) NOT NULL CHECK (severity IN ('CRITICAL', 'CRITICAL_MALICIOUS')),
    reason              TEXT NOT NULL,
    created_at_ms       BIGINT NOT NULL,
    completed_at_ms     BIGINT,
    UNIQUE (installation_id, package_slug, version_or_sha256)
);

CREATE INDEX idx_extension_revocation_actions_pending
    ON extension_revocation_actions (created_at_ms)
    WHERE completed_at_ms IS NULL;
