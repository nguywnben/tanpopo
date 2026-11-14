# Operational Requirements & Workflow-to-Requirement Candidates — Tanpopo

> **Business Analysis Specification (Phase ASTRA A02 → Promoted to SRS in A03 — `DRAFT / UNLOCKED SCOPE`)**
> - **Version:** `0.1.0-step03-aligned`
> - **Last Updated:** `2026-11-11`
> - **Authoring Role:** Lead Business Analyst & Product Architect
> - **Single Source of Truth (SSOT) Rule:** Starting in **ASTRA A03**, candidates `CAND-001`..`017` in Section 2 were formally promoted to verifiable requirements `TP-FR-0101`..`1005` and `TP-NFR-0101`..`1002` in [`docs/requirements/srs.md`](./srs.md) and traced end-to-end in [`docs/requirements/traceability.md`](./traceability.md). Whenever consulting normative system requirements, [`srs.md`](./srs.md) is the canonical single source of truth; this document remains authoritative for the **9 Operational Requirements (`OPS-01`..`09`)** and the A02-to-A03 analytical bridge.

---

## 1. Nine Operational Business Requirement Groups (`OPS-01` .. `OPS-09`)

| ID | Operational Domain | Expected Operator & Organizational Behavior (`PROPOSED`) |
|---|---|---|
| **OPS-01** | **Onboarding & Day-1 Bootstrap** | Provide two explicit bootstrap modes: (1) *Personal / Small-Team Mode*, auto-provisioning a default workspace via the Setup Wizard in under 3 minutes (`WF-01`, `WF-02`), and (2) *Declarative Bootstrap Mode*, enabling an `Operator` to reconcile organizational hierarchy, pinned plugin lockfiles, and policies from declarative config files without manual browser interaction. |
| **OPS-02** | **Role Model & Segregation of Duties (`RBAC`)** | Cleanly separate host infrastructure operations (`Operator`), tenant ownership (`Organization Owner`), project administration (`Team Admin`), independent verification (`Reviewer`), and engineering consumption (`Developer`). Support optional two-person sign-off (*Four-Eyes Principle*) for external plugin approvals or major budget increases while collapsing cleanly into a single `Personal Admin` role for solo operators. |
| **OPS-03** | **Audit Logging (`Audit`) & Forensic Investigation** | Immutably record (`TP-BR-0803`) all configuration mutations, key lifecycle transitions, approval/rejection decisions, and blocked security violations; support filtering by time window, principal, project, and extension ID, plus structured export for long-term compliance archiving. |
| **OPS-04** | **Incident Response & Blast-Radius Containment** | Provide independent emergency kill-switches across 4 granular scopes to minimize blast radius: (1) revoke a single `Virtual Key`, (2) freeze a `Project`, (3) suspend a `Provider Connection`, and (4) globally quarantine an `Extension` (`QUARANTINED` — `WF-12`), paired with instant impact queries enumerating affected keys and projects. |
| **OPS-05** | **Backup & Disaster Recovery** | Enable safe export and restoration of control-plane configurations, pinned extension lockfiles, and budget ledgers; encrypt all backup credentials under an independent master protection key so a leaked backup archive never exposes raw upstream API keys. |
| **OPS-06** | **Data Retention & Lifecycle Erasure (`Retention & Privacy`)** | Enforce metadata-only default privacy (`TP-BR-0802`); support independent automatic expiration (`TTL`) policies across: (a) short-term technical traces, (b) opt-in debug payload captures, and (c) long-term financial/audit records; guarantee clean erasure upon `Project`/`Organization` decommissioning (`WF-13`). |
| **OPS-07** | **Supportability & Self-Diagnostics** | Provide comprehensive system health diagnostics and zero-cost dry-run simulators (`Dry-Run Policy & Route Explain`) so `Operator` and `Developer` users can pinpoint why a request was blocked without reading source code or enabling secret-exposing debug logs. |
| **OPS-08** | **Marketplace Moderation & Revocation Advisories** | Operate transparent verification workflows for the `verified` tier (automated contract compatibility tests + `TP-BR-0302` subscription-terms compliance checks); maintain vulnerability/copyright/TOS abuse reporting channels and publish signed `Revocation Advisory` feeds alerting self-hosted instances to malicious packages. |
| **OPS-09** | **Financial Reporting & Contract Compatibility Policy** | Generate cost-allocation reports across `Organization`/`Project`/`Environment`/`Virtual Key`/`Model` explicitly segregating `EXACT` spend from `ESTIMATED`/`UNMETERED` entries (`TP-BR-0703`). Enforce a published Plugin Contract lifecycle policy maintaining at least an `N-1 Contract Support Window` across upgrades with advance deprecation notices. |

---

## 2. Workflow-to-Requirement Candidates Matrix (`CAND-001` .. `CAND-017` — `DRAFT`)

The matrix below bridges capability modules (following pre-PRD *Capability Map* decomposition) to workflows `WF-*`, state machines `SM-*`, and business rules `TP-BR-*` **without prematurely locking release scope**:

| Candidate ID | Capability Module (`Module ID`) | Business Requirement Candidate Title | `WF` / `OPS` Trace | `SM` Trace | Governing Rules (`TP-BR`) | Proposed Horizon (`PROPOSED` — Unlocked) |
|---|---|---|---|---|---|---|
| **CAND-001** | `core-tenancy-iam` | Secure first-boot instance bootstrap & Day-1 Setup Wizard | `WF-01`, `WF-02`, `OPS-01` | — | `TP-BR-0301`, `0802` | `v0.1` |
| **CAND-002** | `core-tenancy-iam` | `Organization` → `Project` → `Environment` hierarchy & hard cross-tenant isolation | `WF-06`, `WF-13` | — | `TP-BR-0101`..`0103` | `v0.1` (core) / `v0.3` (full multi-org) |
| **CAND-003** | `core-tenancy-iam` | Role-Based Access Control (`RBAC`) & optional Four-Eyes approval | `OPS-02`, `WF-04`, `WF-09` | `SM-06` | `TP-BR-0102`, `0602` | `v0.1` (core) / `v0.3` (extended) |
| **CAND-004** | `credential-access` | Single-environment `Virtual Key` lifecycle, dual-valid grace rotation & instant revocation | `WF-02`, `WF-10`, `WF-12` | `SM-05` | `TP-BR-0103`, `0803` | `v0.1` |
| **CAND-005** | `credential-access` | Write-only `Secret Credential` vault (API keys, service principals) & blind egress injection | `WF-03`, `WF-05` | `SM-02` | `TP-BR-0102`, `0304`, `0402` | `v0.1` |
| **CAND-006** | `credential-access` | Legitimate `Authorized OAuth` & federated identity with single-flight refresh & expiry alerts | `WF-05` | `SM-02`, `SM-03` | `TP-BR-0301`..`0304` | `v0.2` |
| **CAND-007** | `extension-governance` | 4-source extension installation (Marketplace, Git, archive, private registry) & 3 `Trust Tier` badges | `WF-04`, `OPS-08` | `SM-01` | `TP-BR-0201`..`0203`, `0302` | `v0.1` (Git/URL/Archive) / `v0.2` (Marketplace/Private Registry) |
| **CAND-008** | `extension-governance` | Default-deny plugin permission sandbox, runtime egress guard & emergency `Quarantine` | `WF-04`, `WF-12`, `OPS-04` | `SM-01` | `TP-BR-0401`, `0402`, `0404` | `v0.1` |
| **CAND-009** | `extension-governance` | Zero-downtime extension updates, `Permission Escalation` auto-activation block & instant rollback | `WF-11`, `OPS-09` | `SM-01`, `SM-08` | `TP-BR-0403`, `0801` | `v0.1` – `v0.2` |
| **CAND-010** | `model-gateway-routing` | Separated 4-entity chain: `Provider Adapter` → `Provider Connection` → `Model` → `Model Deployment` | `WF-03` | `SM-02`, `SM-04` | `TP-BR-0102`, `0104` | `v0.1` |
| **CAND-011** | `model-gateway-routing` | Unified inference surface alongside lossless native passthrough, timeouts, cancellation & `Partial Stream` guard | `WF-06` | `SM-04` | `TP-BR-0503`..`0505` | `v0.1` |
| **CAND-012** | `model-gateway-routing` | Capability-aware routing, pre-stream failover, circuit breaking & `Fail-Closed` data residency | `WF-07` | `SM-04` | `TP-BR-0501`, `0502`, `0504` | `v0.1` (core) / `v0.2` (advanced + dry-run explain) |
| **CAND-013** | `finops-budget-ledger` | Hierarchical budget ledger, `Pre-Execution Hold` & transparent `EXACT` / `ESTIMATED` / `UNMETERED` settlement | `WF-08`, `OPS-09` | `SM-07` | `TP-BR-0701`..`0703` | `v0.1` (single-node) / `v0.3` (distributed cluster) |
| **CAND-014** | `mcp-governance-plane` | `MCP Server` registration, virtual-key tool scoping & anti-rug-pull `schema_hash` pinning | `WF-09` | — | `TP-BR-0104`, `0601` | `v0.2` |
| **CAND-015** | `mcp-governance-plane` | Parameter-hash-bound human approval for `destructive` tools & no-blind-retry guard on side-effecting tools | `WF-09` | `SM-06` | `TP-BR-0602`, `0603` | `v0.2` |
| **CAND-016** | `a2a-connectivity-plane` | `Agent Endpoint` registration, AgentCard discovery & privilege-bounded async A2A task delegation | `OPS-02` | `SM-06` | `TP-BR-0104`, `0604` | `v0.3` |
| **CAND-017** | `audit-ops-lifecycle` | Immutable audit log, metadata-only default privacy, clean tenant erasure & safe backup/upgrade lifecycle | `WF-12`, `WF-13`, `OPS-03`..`07` | `SM-08` | `TP-BR-0801`..`0803` | `v0.1` (core) → `v1.0` (complete) |

---

## 3. Mandatory Negative-Case & Compliance Self-Check Verification

| Mandatory A02 Self-Check Item | Coverage Status | Normative Specification Location & Locked Business Behavior |
|---|---|---|
| **1. Untrusted external provider / plugin** | **COVERED** | [`workflows.md`](./workflows.md) (`WF-04`, `WF-06`), [`business-rules.md`](./business-rules.md) (`TP-BR-0201`..`0203`, `0401`, `0402`, `0404`) — Tagged `unverified-external`, mandatory permission sheet consent prior to activation, zero raw secret exposure unless required for dynamic signing, strict egress allowlisting, and emergency `QUARANTINED` containment. |
| **2. Plugin requesting new permissions on update** | **COVERED** | [`workflows.md`](./workflows.md) (`WF-11`), [`business-entity-map.md`](./business-entity-map.md) (`SM-01`), [`business-rules.md`](./business-rules.md) (`TP-BR-0403`) — Transitions to `UPDATING_PENDING_RECONSENT`, blocks silent activation, and keeps the prior approved version serving traffic until escalated permissions receive explicit consent. |
| **3. Expired / revoked upstream credentials** | **COVERED** | [`workflows.md`](./workflows.md) (`WF-05`), [`business-entity-map.md`](./business-entity-map.md) (`SM-02`, `SM-03`), [`business-rules.md`](./business-rules.md) (`TP-BR-0304`) — Single-flight refresh serialization, immediate transition to `AUTH_EXPIRED_REAUTH_REQUIRED`, operator alerting, and zero raw token leakage in client errors. |
| **4. Cross-tenant access (`Cross-tenant`)** | **COVERED** | [`workflows.md`](./workflows.md) (`WF-06`), [`business-rules.md`](./business-rules.md) (`TP-BR-0101`, `0102`) — Hard non-enumerable rejection concealing cross-tenant resource existence paired with a high-severity security audit event. |
| **5. Destructive tool invocation (`Tool destructive`)** | **COVERED** | [`workflows.md`](./workflows.md) (`WF-09`), [`business-entity-map.md`](./business-entity-map.md) (`SM-06`), [`business-rules.md`](./business-rules.md) (`TP-BR-0602`, `0603`) — Mandatory human approval cryptographically bound to input parameter digest; automatic TTL expiry cancellation; zero automatic retry on network timeout. |
| **6. Upstream provider outage (`Provider unavailable`)** | **COVERED** | [`workflows.md`](./workflows.md) (`WF-07`), [`business-entity-map.md`](./business-entity-map.md) (`SM-04`), [`business-rules.md`](./business-rules.md) (`TP-BR-0501`, `0502`) — Pre-stream failover across compliant healthy deployments; strict `Fail-Closed` (`503`) if only out-of-region deployments remain alive. |
| **7. Fallback after partial stream emission (`Fallback after partial stream`)** | **COVERED** | [`workflows.md`](./workflows.md) (`WF-06`, `WF-07`), [`business-rules.md`](./business-rules.md) (`TP-BR-0503`) — Strictly prohibits invoking a backup provider into an active stream once at least one content chunk has been emitted; terminates cleanly with an explicit abort frame. |
| **8. Chargeback when exact usage is missing (`Chargeback without exact usage`)** | **COVERED** | [`workflows.md`](./workflows.md) (`WF-08`), [`business-entity-map.md`](./business-entity-map.md) (`SM-07`), [`business-rules.md`](./business-rules.md) (`TP-BR-0703`) — Applies deterministic estimation, tags entries `ESTIMATED` or `UNMETERED_FLAGGED`, and segregates columns in financial reports rather than recording fabricated `0 USD` spend. |
| **9. Subscription entitlement check (Never promising `Plus/Pro = free API credits`)** | **COVERED** | [`glossary.md`](../product/glossary.md), [`workflows.md`](./workflows.md) (`WF-02`, `WF-04`), [`business-rules.md`](./business-rules.md) (`Section 1`, `TP-BR-0301`, `0302`) — Cleanly separates technical wire execution from provider-granted legal entitlement; prohibits any claim or workflow repurposing personal consumer subscriptions into shared API credits. |

---

## 4. Open Business Ambiguities & Proposed Resolutions (`BA-OQ-001` .. `BA-OQ-005`)

Identified during detailed A02 business modeling, the **5 business-level ambiguities** below require Product Owner resolution alongside (or immediately following) Gate G1 prior to locking the PRD in A03 (synchronized in [`docs/governance/decisions.md`](../governance/decisions.md)):

1. **`BA-OQ-001` — Multi-Organization Tenancy on a Single Self-Hosted Instance:**
   - *Ambiguity:* Must a self-hosted Tanpopo instance expose full multi-`Organization` administration from day one, or does a single `Organization` containing multiple `Project`/`Team` scopes satisfy 95% of self-hosted adopters initially?
   - *Proposal (`PROPOSED`):* Anchor `Organization` at the root of the data schema from day one to avoid destructive migrations later, while streamlining default `0.x` UI workflows around **1 Organization with multiple Projects & Environments** (exposing full multi-Organization instance administration for enterprise conglomerates in `v0.3`/`v1.0`).
2. **`BA-OQ-002` — Handling Variance Between Pre-Execution Hold (`RESERVED_HOLD`) and Actual End-of-Stream Settlement Overrun:**
   - *Ambiguity:* When a caller omits `max_tokens`, how much budget should be held pre-execution, and what happens if actual end-of-stream settlement slightly exceeds the initial hold (pushing available balance negative by a few cents)?
   - *Proposal (`PROPOSED`):* Enforce a configurable default hold ceiling per `Route`/`Environment` (or model safety bound); if final settlement slightly exceeds the hold, truthfully record the overrun in the ledger (`Negative Balance Carryover`) without truncating the final token, and immediately hard-block subsequent requests until budget is topped up.
3. **`BA-OQ-003` — Default Behavior for Open Streaming Connections Upon Key Revocation (`Revoke Key`):**
   - *Ambiguity:* When an administrator revokes a `Virtual Key`, should active in-flight response streams be severed instantaneously or allowed to finish draining over a brief grace window?
   - *Proposal (`PROPOSED`):* Expose two distinct revocation modes: **Standard Rotation/Expiry**, allowing open streams to drain gracefully within a short bound; and **`Emergency Security Revoke`**, immediately severing all active in-flight streams to halt potential data exfiltration.
4. **`BA-OQ-004` — Notification & Waiting UX for `destructive` MCP Tool Approvals (`Destructive Tool Approval UX`):**
   - *Ambiguity:* When an `Agent` invokes an MCP tool requiring human sign-off (`SM-06`), external `MCP Client` transport timeouts may expire if a human reviewer takes 10 minutes to approve in the Web Console.
   - *Proposal (`PROPOSED`):* Support dual policy-selectable modes: (a) *Short Synchronous Hold (`<= 60 seconds`)* in-session for interactive user-assisted agents, and (b) *Immediate Hold Rejection with `Pre-Approval Token`*, allowing an asynchronous `Reviewer` sign-off via Web Console/Webhook followed by immediate execution when the agent replays the request within a short token validity window.
5. **`BA-OQ-005` — Backward-Compatibility Support Window for Extension Contracts (`Plugin Contract Deprecation Window`):**
   - *Ambiguity:* Once Tanpopo publishes new extension contract versions (from `v1.0` onward), how long are existing plugins guaranteed to remain runnable?
   - *Proposal (`PROPOSED`):* Prior to `v1.0` (`0.x`), maintain compatibility across at least `N-1 minor` contract releases; starting at `v1.0`, commit to a minimum **12-month backward-compatibility window (or the entire `1.x` major line)** prior to removing any published contract field.
