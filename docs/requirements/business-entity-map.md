# Actors, Business Entity Map & State Machines — Tanpopo

> **Business Analysis Specification (Phase ASTRA A02 — `DRAFT / UNLOCKED SCOPE`)**
> - **Version:** `0.1.0-a02-draft`
> - **Last Updated:** `2026-11-11`
> - **Authoring Role:** Lead Business Analyst & Product Architect
> - **Governance Status:** **Unlocked-scope draft (`DRAFT`)** — awaiting Product Owner approval of **Gate G1** and **Gate G2** in [`docs/governance/decisions.md`](../governance/decisions.md). Contains zero hard-locked programming language or database assumptions.

---

## 1. Complete Catalog of 12 Business Actors (Actors Matrix)

Each actor below operates within an independent authority scope, operational objective, and trust boundary:

| ID | Actor | Nature & Trust Level | Core Allowed Privileges (Allowed) | Prohibited Boundaries (Prohibited) |
|---|---|---|---|---|
| **ACT-01** | **Personal Admin** | Human — Full trust on a personal server (`Single-Tenant Mode`) | Exercises the combined responsibilities of `Operator`, `Organization Owner`, and `Team Admin` on a self-hosted personal instance. | Not applicable in multi-department organizational deployments where policy mandates Segregation of Duties. |
| **ACT-02** | **Organization Owner** | Human — Highest administrative authority within an `Organization` | Configures `Project`/`Environment` hierarchy, top-level budgets, data residency policies, Plugin trust thresholds, retention/erasure rules, and organization-wide emergency kill-switches. | Cannot inspect data belonging to other `Organization` tenants on the same `Instance`; cannot alter host-level infrastructure owned by `Operator`. |
| **ACT-03** | **Team Admin** | Human — Administrative authority scoped to an assigned `Project` | Manages project members, creates/rotates/revokes `Virtual Key` credentials, defines `Route` policies, subdivides project budgets, and attaches dedicated project `Provider Connection` resources (where permitted by organization policy). | Cannot install new `Extension` packages onto the instance or raise budget ceilings above allocations granted by the `Organization Owner`. |
| **ACT-04** | **Developer** | Human — Internal consumer within a `Project` | Uses issued virtual keys, experiments in the Playground, views permitted `Model`/`MCP Tool` catalogs, and inspects logs/traces within authorized `Environment` scopes. | Cannot read raw upstream `Credential` secrets, modify safety policies, or self-approve permission requests. |
| **ACT-05** | **API Client** | Automated software — Headless, non-UI consumer | Sends model inference requests (Unified API or native dialect) bearing a valid `Virtual Key`; consumes streamed responses and canonical error envelopes. | Cannot invoke Control Plane administrative APIs; never observes upstream provider keys or account configurations. |
| **ACT-06** | **Extension Publisher** | Human / Internal or external engineering entity — **Untrusted by default** until verified | Packages `Extension Version` artifacts with permission-declaring `Manifest` files, contract test suites, and provenance signatures; publishes to Marketplace, Git, or private registries. | Cannot request undeclared capabilities outside the `Manifest`; third parties cannot spoof Tanpopo's `official` provenance tier. |
| **ACT-07** | **Reviewer** | Independent human verifier (Public Marketplace tier or Internal Security tier) | *(Marketplace level)* Approves/rejects/revokes the `verified` badge. *(Organization level)* Approves or rejects permission-escalating plugin installs/updates and `destructive` tool invocations. | Cannot self-approve sensitive requests initiated by the same principal when organizational Four-Eyes controls are enabled. |
| **ACT-08** | **Operator** | Human — System infrastructure administrator (`Instance / SRE`) | Executes initial bootstrap, upgrades/rolls back Tanpopo Core, configures master secret protection, manages backups/recovery, monitors host health, and enforces instance-wide plugin quarantine. | Cannot bypass immutable `Audit Event` logging during emergency interventions on keys or connections. |
| **ACT-09** | **MCP Client** | Software (IDE, agent runtime) connecting to the MCP plane | Discovers permission-filtered tool/resource catalogs and submits authenticated tool execution calls. | Cannot enumerate or invoke tools outside the authorized scope of its `Virtual Key` / `Principal`. |
| **ACT-10** | **MCP Server** | Downstream tool backend (internal or third-party) — **Semi-trusted** | Publishes `Tools`/`Resources`/`Prompts` catalogs and executes tool calls validly forwarded by Tanpopo. | Cannot silently mutate pinned tool descriptions/schemas (`schema_hash`) without triggering gateway detection and re-approval blocking. |
| **ACT-11** | **Agent** | Autonomous agent (internal or remote partner via the A2A plane) | Executes multi-step model + MCP workflows or sends/receives asynchronous delegated tasks (`Agent Task`) within granted budget and TTL bounds. | Cannot amplify delegated privileges beyond the entitlements and remaining budget of the originating principal. |
| **ACT-12** | **External Identity / AI Provider** | External authentication service (IdP/OAuth) and upstream model vendor — Independent legal boundary | *(IdP)* Authenticates principals and issues valid authorization tokens. *(AI Provider)* Processes inference calls under its own pricing, rate limits, and commercial terms. | Outside Tanpopo's operational control; upstream outages or terms changes must be safely contained by Tanpopo. |

---

## 2. Business Entity Map & Ownership

### 2.1. Core Domain ER Diagram

```mermaid
erDiagram
    INSTANCE ||--o{ ORGANIZATION : "hosts"
    INSTANCE ||--o{ EXTENSION_INSTALLATION : "activates"
    EXTENSION_PACKAGE ||--|{ EXTENSION_VERSION : "versions"
    EXTENSION_VERSION ||--o{ EXTENSION_INSTALLATION : "installed_as"
    EXTENSION_INSTALLATION ||--o| PROVIDER_ADAPTER : "exposes_adapter"

    ORGANIZATION ||--|{ PROJECT : "contains"
    ORGANIZATION ||--o{ PRINCIPAL : "manages"
    ORGANIZATION ||--o{ SECRET_CREDENTIAL : "owns_org_level"
    ORGANIZATION ||--o{ PROVIDER_CONNECTION : "owns_shared_or_dedicated"
    ORGANIZATION ||--|{ BUDGET_LEDGER : "governs_spend"
    ORGANIZATION ||--o{ POLICY : "enforces"

    PROJECT ||--|{ ENVIRONMENT : "segments"
    PROJECT ||--o{ VIRTUAL_KEY : "issues"
    PROJECT ||--o{ ROUTE : "defines"
    PROJECT ||--o{ MCP_SERVER_REGISTRATION : "connects"
    PROJECT ||--o{ AGENT_ENDPOINT : "registers_a2a"

    PROVIDER_ADAPTER ||--o{ PROVIDER_CONNECTION : "drives"
    SECRET_CREDENTIAL ||--o{ PROVIDER_CONNECTION : "authenticates"
    PROVIDER_CONNECTION ||--|{ MODEL_DEPLOYMENT : "serves"
    LOGICAL_MODEL ||--o{ MODEL_DEPLOYMENT : "implemented_by"
    ROUTE ||--|{ MODEL_DEPLOYMENT : "targets_ordered"

    MCP_SERVER_REGISTRATION ||--|{ MCP_TOOL : "exposes"
    VIRTUAL_KEY ||--o{ BUDGET_RESERVATION : "initiates"
    BUDGET_RESERVATION ||--|| USAGE_SETTLEMENT : "settles_into"
    PRINCIPAL ||--o{ APPROVAL_REQUEST : "decides"
    ORGANIZATION ||--o{ AUDIT_EVENT : "records_immutable"
```

### 2.2. Explicit Disambiguation of the 4-Entity Model Chain

| Entity | Business Question Answered | Concrete Example | Ownership & Lifecycle |
|---|---|---|---|
| **1. `Provider Adapter`** | *"How do we speak the wire protocol of this provider family?"* | Package `anthropic-official@1.4.0` or `corp-internal-llm@0.2.1` | Belongs to the `Instance`/`Organization` extension catalog. Contains zero secret keys and is decoupled from individual projects. |
| **2. `Provider Connection`** | *"Which account/credential authenticates us, and to which service endpoint?"* | Connection `"Anthropic Commercial Prod - Product Division"` bound to `API Key #A1` | Owned by an `Organization` (optionally shared downward to selected `Project` scopes) or dedicated to a single `Project`. |
| **3. `Model`** *(Logical Model)* | *"What intrinsic capabilities does this model family possess?"* | Logical model `claude-sonnet-4-5` (supporting `200k context`, `vision`, `tool_calling`, `thinking`) | Normalized catalog entry derived from `Provider Adapter` metadata plus admin overrides; independent of billing accounts. |
| **4. `Model Deployment`** | *"Which concrete operational target receives live traffic right now, in what data region, at what unit price, and in what health state?"* | `Deployment #1`: `claude-sonnet-4-5` via `Bedrock Frankfurt (EU)` vs `Deployment #2`: via `Anthropic Direct (US)` | Bound 1-to-1 to a `Provider Connection`. **All routing, circuit breaking, data residency, and pricing enforcement operate on `Model Deployment`.** |

### 2.3. Ownership, Sharing & Data Isolation Matrix

| Business Entity | Primary Owner | Valid Sharing Scope | Owner Deletion Behavior |
|---|---|---|---|
| `Organization (Tenant)` | `Organization Owner` | **Zero cross-tenant sharing** of any live resource across different `Organization` boundaries. | Transitions to pending freeze → revokes all keys/connections → purges operational data per retention policy. |
| `Secret Credential` | `Organization` or `Project` | `Organization`-owned secrets permit *indirect consumption* by authorized child `Project` scopes via a `Provider Connection`, never exposing raw secret plaintext. | Hard deletion blocked while bound to an active `Provider Connection` (must detach or revoke the connection first). |
| `Provider Connection` & `Model Deployment` | `Organization` (selectively shared) or `Project` (dedicated) | Consumption rights can be granted downward to specific `Project` / `Environment` scopes via `Policy` and `Route` bindings. | Stops admitting new traffic (`Draining`); retains minimal historical identifiers in the cost ledger to preserve chargeback reconciliation integrity. |
| `Virtual Key` | `Environment` within a `Project` | Strictly bound to 1 `Environment` of 1 `Project` (a single virtual key is never shared across `dev` and `prod`). | Immediately invalidates authentication authority; retains hashed key metadata for audit log traceability. |
| `Extension Installation` | `Instance` (with per-`Organization` enablement policy) | Installed artifacts are governed by `Operator`; each `Organization` may activate only plugins satisfying its trust policy. | Uninstallation succeeds only when no active `Provider Connection` or `Policy` depends on it (unless forcibly transitioned to `Quarantine`). |
| `MCP Server Registration` & `MCP Tool` | `Organization` or `Project` | Scopes access down to individual tool names (`Tool-level Scoping`) per `Virtual Key` / role. | Revokes active tool sessions and cancels pending approval requests. |
| `Budget Ledger` & `Audit Event` | `Organization` (sub-allocating child ceilings to `Project` / `Virtual Key`) | Read-only along administrative hierarchy (`Team Admin` views only their `Project` ledger; `Org Owner` views organization-wide records). | **Append-only / Immutable** throughout statutory/financial retention windows even after child keys or projects are decommissioned. |

---

## 3. Eight Business State Machines & Transitions (`SM-01` .. `SM-08`)

### SM-01: Plugin / Extension Lifecycle

```mermaid
stateDiagram-v2
    [*] --> DISCOVERED
    DISCOVERED --> VERIFYING : "Select install (Marketplace/Git/ZIP/Registry)"
    VERIFYING --> REJECTED_INVALID : "Contract mismatch / invalid signature / source policy violation"
    VERIFYING --> PENDING_PERMISSION_APPROVAL : "Technically valid -> Present permission sheet & Trust Tier badge"
    PENDING_PERMISSION_APPROVAL --> INSTALLED_DISABLED : "Admin/Reviewer approves permissions"
    PENDING_PERMISSION_APPROVAL --> CANCELLED : "Deny permission grant"
    INSTALLED_DISABLED --> ENABLED_ACTIVE : "Enable for live traffic"
    ENABLED_ACTIVE --> DRAINING : "Pause / Prepare upgrade or removal"
    ENABLED_ACTIVE --> UPDATING_PENDING_RECONSENT : "Update requests additional permissions (Permission Escalation)"
    UPDATING_PENDING_RECONSENT --> DRAINING : "Approve new permissions -> Graceful version cutover"
    UPDATING_PENDING_RECONSENT --> ENABLED_ACTIVE : "Reject new permissions -> Keep existing version active"
    DRAINING --> INSTALLED_DISABLED : "In-flight streams complete"
    DRAINING --> ENABLED_ACTIVE : "Activate new version (or Rollback to pinned version)"
    ENABLED_ACTIVE --> QUARANTINED : "Security violation / Egress breach / Emergency revocation"
    INSTALLED_DISABLED --> QUARANTINED : "Malicious advisory received"
    QUARANTINED --> INSTALLED_DISABLED : "Audited unquarantine by Operator/Owner only"
    INSTALLED_DISABLED --> UNINSTALLED : "Uninstall (when zero dependent Connections remain)"
    QUARANTINED --> UNINSTALLED : "Purge malicious package after preserving audit evidence"
```

- **Core Business Gate:** Every extension update introducing expanded capabilities (`Permission Escalation`) **must** halt at `UPDATING_PENDING_RECONSENT` and never silently auto-upgrade. Entering `QUARANTINED` immediately severs new traffic and network permissions while preserving forensic logs.

---

### SM-02: Provider Connection Lifecycle

```mermaid
stateDiagram-v2
    [*] --> DRAFT
    DRAFT --> VALIDATING : "Bind Credential & run connectivity probe"
    VALIDATING --> ACTIVE : "Valid authentication & policy compliance"
    VALIDATING --> DRAFT : "Invalid configuration / network error"
    ACTIVE --> DEGRADED : "Error rate or latency exceeds warning threshold"
    DEGRADED --> ACTIVE : "Steady health recovered"
    ACTIVE --> AUTH_EXPIRED_REAUTH_REQUIRED : "Key expired / OAuth revoked (401/403 auth)"
    DEGRADED --> AUTH_EXPIRED_REAUTH_REQUIRED : "Auth refresh failed"
    AUTH_EXPIRED_REAUTH_REQUIRED --> VALIDATING : "Credential rotated / Re-authorization completed"
    ACTIVE --> SUSPENDED_BY_ADMIN : "Proactive admin suspension"
    SUSPENDED_BY_ADMIN --> VALIDATING : "Re-enable connection"
    ACTIVE --> REVOKED : "Permanently revoke connection"
    AUTH_EXPIRED_REAUTH_REQUIRED --> REVOKED : "Decommission connection"
    SUSPENDED_BY_ADMIN --> REVOKED : "Decommission connection"
    REVOKED --> ARCHIVED : "Archive historical metadata for chargeback reconciliation"
```

---

### SM-03: Authorized OAuth Token Lifecycle

```mermaid
stateDiagram-v2
    [*] --> UNBOUND
    UNBOUND --> AUTHORIZING : "Initiate authorized OAuth flow (PKCE / Client Credentials)"
    AUTHORIZING --> TOKEN_VALID : "Receive valid token & verified allowed scopes"
    AUTHORIZING --> UNBOUND : "User cancelled / Scope mismatch"
    TOKEN_VALID --> REFRESHING_SINGLEFLIGHT : "Reach expiry refresh window (single representative refresh flight)"
    REFRESHING_SINGLEFLIGHT --> TOKEN_VALID : "New token issued successfully"
    REFRESHING_SINGLEFLIGHT --> REFRESH_GRACE_DEGRADED : "Transient IdP network error (current token still within short grace)"
    REFRESH_GRACE_DEGRADED --> REFRESHING_SINGLEFLIGHT : "Scheduled backoff retry"
    REFRESHING_SINGLEFLIGHT --> REAUTH_REQUIRED : "invalid_grant / Token revoked / Password changed"
    REFRESH_GRACE_DEGRADED --> REAUTH_REQUIRED : "Access token fully expired"
    REAUTH_REQUIRED --> AUTHORIZING : "Admin performs interactive re-authorization"
    TOKEN_VALID --> REVOKED : "Proactive revocation from Tanpopo"
    REAUTH_REQUIRED --> REVOKED : "Delete authentication binding"
```

---

### SM-04: Model Deployment Health Lifecycle

```mermaid
stateDiagram-v2
    [*] --> PROVISIONED
    PROVISIONED --> HEALTHY : "Readiness check passes"
    HEALTHY --> THROTTLED_COOLDOWN : "Receive upstream 429 Rate Limit / Retry-After"
    THROTTLED_COOLDOWN --> HEALTHY : "Retry-After cooldown elapses"
    HEALTHY --> CIRCUIT_OPEN_UNHEALTHY : "Consecutive 5xx / Timeout breaches circuit threshold"
    THROTTLED_COOLDOWN --> CIRCUIT_OPEN_UNHEALTHY : "Sustained upstream failure"
    CIRCUIT_OPEN_UNHEALTHY --> HALF_OPEN_PROBING : "Isolation window elapses -> Admit bounded probe requests"
    HALF_OPEN_PROBING --> HEALTHY : "Consecutive probe requests succeed"
    HALF_OPEN_PROBING --> CIRCUIT_OPEN_UNHEALTHY : "Probe request fails"
    HEALTHY --> DRAINING_MAINTENANCE : "Planned maintenance drain"
    DRAINING_MAINTENANCE --> DISABLED : "In-flight drain completes"
    DISABLED --> PROVISIONED : "Re-enable Deployment"
```

---

### SM-05: Virtual Key Lifecycle

```mermaid
stateDiagram-v2
    [*] --> CREATED_ACTIVE
    CREATED_ACTIVE --> ROTATING_DUAL_VALID : "Trigger grace-period rotation (new key active, old key expiring)"
    ROTATING_DUAL_VALID --> CREATED_ACTIVE : "Grace window elapses -> Only new key remains valid"
    CREATED_ACTIVE --> QUOTA_PAUSED : "Period budget ceiling reached (auto-resumes next period or on top-up)"
    QUOTA_PAUSED --> CREATED_ACTIVE : "New billing period starts / Admin raises ceiling"
    CREATED_ACTIVE --> SUSPENDED : "Temporary investigation hold"
    SUSPENDED --> CREATED_ACTIVE : "Cleared safe, re-enabled"
    CREATED_ACTIVE --> EXPIRED : "Preconfigured TTL reached"
    CREATED_ACTIVE --> REVOKED : "Emergency revocation (immediate invalidation)"
    ROTATING_DUAL_VALID --> REVOKED : "Emergency revocation of both paired keys"
    SUSPENDED --> REVOKED : "Permanent termination"
    EXPIRED --> PURGED_METADATA_RETAINED : "Purge auth material, retain audit identifier"
    REVOKED --> PURGED_METADATA_RETAINED : "Purge auth material, retain audit identifier"
```

---

### SM-06: Approval & Task Lifecycle

```mermaid
stateDiagram-v2
    [*] --> CREATED
    CREATED --> EXECUTING : "Action matches pre-authorized allowlist"
    CREATED --> AWAITING_APPROVAL : "Matches mandatory approval policy (e.g., destructive MCP Tool)"
    AWAITING_APPROVAL --> APPROVED : "Authorized Reviewer approves within TTL"
    AWAITING_APPROVAL --> REJECTED : "Reviewer rejects"
    AWAITING_APPROVAL --> APPROVAL_EXPIRED : "Approval TTL elapses -> Safe auto-cancel"
    AWAITING_APPROVAL --> CANCELLED : "Caller cancels request"
    APPROVED --> EXECUTING : "Release execution strictly matching hash-bound parameters"
    EXECUTING --> COMPLETED : "Execution succeeds & receipt recorded"
    EXECUTING --> CANCELLED : "Aborted mid-flight on caller cancellation signal"
    EXECUTING --> FAILED : "Execution failed (no blind retry if side-effecting)"
```

---

### SM-07: Budget Reservation & Settlement Lifecycle

```mermaid
stateDiagram-v2
    [*] --> CHECKED
    CHECKED --> REJECTED_INSUFFICIENT_BUDGET : "Available balance (minus active holds) insufficient"
    CHECKED --> RESERVED_HOLD : "Lock estimated maximum request spend"
    RESERVED_HOLD --> RELEASED_UNUSED : "Rejected at Policy gate / Pre-egress failure"
    RESERVED_HOLD --> STREAMING_ACCRUING : "Upstream provider connection opened"
    STREAMING_ACCRUING --> SETTLED_EXACT : "Completed with authoritative upstream usage metrics"
    STREAMING_ACCRUING --> SETTLED_ESTIMATED : "Mid-stream abort / Missing upstream usage -> Apply deterministic estimator"
    STREAMING_ACCRUING --> SETTLED_UNMETERED_FLAGGED : "Workload unestimable -> Flag for manual reconciliation"
    RESERVED_HOLD --> EXPIRED_RECLAIMED : "Node crash leaves hold orphaned past TTL -> Audited reclamation"
```

---

### SM-08: Release & Upgrade Lifecycle

```mermaid
stateDiagram-v2
    [*] --> PREFLIGHT_COMPAT_CHECK
    PREFLIGHT_COMPAT_CHECK --> BLOCKED_INCOMPATIBLE : "Active Plugin incompatible with target Contract version"
    PREFLIGHT_COMPAT_CHECK --> BACKUP_SNAPSHOT : "Compatibility verified -> Snapshot config & ledger safely"
    BACKUP_SNAPSHOT --> UPGRADING_CONTROL_PLANE : "Apply backward-compatible control plane schema migration"
    UPGRADING_CONTROL_PLANE --> ROLLING_DATA_PLANE : "Shift new traffic to upgraded workers, gracefully drain old workers"
    ROLLING_DATA_PLANE --> VERIFYING_HEALTH : "Run end-to-end self-diagnostic checks"
    VERIFYING_HEALTH --> COMMITTED : "All health metrics & connections nominal"
    UPGRADING_CONTROL_PLANE --> ROLLING_BACK : "Configuration migration failure"
    ROLLING_DATA_PLANE --> ROLLING_BACK : "Error rate spike detected"
    VERIFYING_HEALTH --> ROLLING_BACK : "Post-upgrade health check failed"
    ROLLING_BACK --> ROLLED_BACK_SAFE : "Restore prior stable release & emit root-cause report"
```
