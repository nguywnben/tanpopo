-- =============================================================================
-- Tanpopo Migration 0006: Extension Packages, Versions, SM-01 Installations,
--                         Organization Policies & Signed Revocation Feed
-- Dialect : Portable SQLite WAL (parity with db/spec-check/sqlite_schema.sql)
-- Unit Ref: TP-IMP-0011 (SPEC-EXT-01 / SPEC-EXT-04 / SPEC-SEC-02 / CR-03, CR-04)
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 11. Extension Packages (3 Public Provenance Tiers — CR-03)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS extension_packages (
    id                VARCHAR(40) PRIMARY KEY, -- 'ext_<26_base32>'
    canonical_name    VARCHAR(160) NOT NULL UNIQUE,
    extension_kind    VARCHAR(32) NOT NULL DEFAULT 'PROVIDER_ADAPTER'
                      CHECK (extension_kind IN (
                          'PROVIDER_ADAPTER',
                          'AUTH_DRIVER',
                          'GUARDRAIL_POLICY',
                          'TELEMETRY_EXPORTER'
                      )),
    publisher_id      VARCHAR(128) NOT NULL,
    origin_trust_tier VARCHAR(32) NOT NULL
                      CHECK (origin_trust_tier IN ('official', 'verified', 'unverified_external')),
    created_at_ms     BIGINT NOT NULL
);

-- -----------------------------------------------------------------------------
-- 12. Extension Versions (3 Execution Tiers & Enterprise Signature — CR-02, CR-03)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS extension_versions (
    id                      VARCHAR(40) PRIMARY KEY, -- 'exv_<26_base32>'
    package_id              VARCHAR(40) NOT NULL REFERENCES extension_packages(id) ON DELETE CASCADE,
    semver                  VARCHAR(64) NOT NULL,
    contract_version        VARCHAR(16) NOT NULL,
    core_version_range      VARCHAR(64) NOT NULL DEFAULT '>=0.1.0',
    execution_tier          VARCHAR(32) NOT NULL
                            CHECK (execution_tier IN (
                                'DECLARATIVE_T1',
                                'WASM_SANDBOX_T2',
                                'ISOLATED_SIDECAR_T3'
                            )),
    bundle_sha256           CHAR(64) NOT NULL,
    signature_verified      BOOLEAN NOT NULL DEFAULT 0 CHECK (signature_verified IN (0, 1)),
    enterprise_signed       BOOLEAN NOT NULL DEFAULT 0 CHECK (enterprise_signed IN (0, 1)),
    signing_key_fingerprint VARCHAR(64),
    source_uri              TEXT NOT NULL,
    manifest_json           TEXT NOT NULL,
    sbom_digest             CHAR(64),
    created_at_ms           BIGINT NOT NULL,
    UNIQUE (package_id, semver),
    UNIQUE (package_id, bundle_sha256)
);

-- -----------------------------------------------------------------------------
-- 13. Extension Installations (SM-01; active_version_id NULLABLE while staging)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS extension_installations (
    id                         VARCHAR(40) PRIMARY KEY, -- 'ins_<26_base32>'
    package_id                 VARCHAR(40) NOT NULL UNIQUE REFERENCES extension_packages(id) ON DELETE RESTRICT,
    active_version_id          VARCHAR(40) REFERENCES extension_versions(id),
    previous_pinned_version_id VARCHAR(40) REFERENCES extension_versions(id),
    candidate_version_id       VARCHAR(40) REFERENCES extension_versions(id),
    state                      VARCHAR(40) NOT NULL
                               CHECK (state IN (
                                   'DISCOVERED',
                                   'VERIFYING',
                                   'REJECTED_INVALID',
                                   'PENDING_PERMISSION_APPROVAL',
                                   'CANCELLED',
                                   'INSTALLED_DISABLED',
                                   'ENABLED_ACTIVE',
                                   'UPDATING_PENDING_RECONSENT',
                                   'DRAINING',
                                   'ROLLED_BACK',
                                   'QUARANTINED',
                                   'UNINSTALLED'
                               )),
    approved_permissions_json  TEXT NOT NULL DEFAULT '{}',
    approved_by_principal_id   VARCHAR(40) REFERENCES principals(id) ON DELETE SET NULL,
    approved_at_ms             BIGINT,
    quarantine_reason          TEXT,
    version                    BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms              BIGINT NOT NULL,
    updated_at_ms              BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_extension_installations_state
    ON extension_installations (state);

-- -----------------------------------------------------------------------------
-- 14. Organization Extension Enablement Policies (CR-04)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS org_extension_policies (
    org_id                 VARCHAR(40) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    package_id             VARCHAR(40) NOT NULL REFERENCES extension_packages(id) ON DELETE CASCADE,
    enabled                BOOLEAN NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    pinned_installation_id VARCHAR(40) REFERENCES extension_installations(id) ON DELETE SET NULL,
    version                BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms          BIGINT NOT NULL,
    updated_at_ms          BIGINT NOT NULL,
    PRIMARY KEY (org_id, package_id)
);

CREATE INDEX IF NOT EXISTS idx_org_extension_policies_org_enabled
    ON org_extension_policies (org_id, enabled);

-- -----------------------------------------------------------------------------
-- 15. Signed Extension Revocation Feed Records (Anti-Rollback — CR-06, TP-IMP-0035)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS extension_revocations (
    id                 VARCHAR(40) PRIMARY KEY,
    package_slug       VARCHAR(160) NOT NULL,
    version_or_sha256  VARCHAR(128) NOT NULL,
    severity           VARCHAR(32) NOT NULL
                       CHECK (severity IN ('CRITICAL', 'CRITICAL_MALICIOUS', 'HIGH', 'VULNERABLE_UPGRADE_ADVISED')),
    version_serial     BIGINT NOT NULL CHECK (version_serial > 0),
    reason             TEXT NOT NULL,
    feed_expires_at_ms BIGINT NOT NULL,
    imported_at_ms     BIGINT NOT NULL,
    UNIQUE (package_slug, version_or_sha256)
);
