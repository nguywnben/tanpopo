# Canonical Business Rules — Tanpopo

> **Business Analysis Specification (Phase ASTRA A02 — Reconciled in `TP-IMP-0001`)**
> - **Version:** `0.2.0-tp-imp-0001`
> - **Last Updated:** `2026-11-14`
> - **Authoring Role:** Lead Business Analyst & Product Architect
> - **Governance Status:** **`APPROVED (Subject to COND-PO-01..05)`** — Gate G1, Gate G2, and Change Requests `CR-03`, `CR-04`, `CR-05`, and `CR-08` approved by the Product Owner in [`docs/governance/decisions.md`](../governance/decisions.md) (closing `GAP-08` and `GAP-09`).
> - **Authoring Principle:** Every rule defines strictly **business invariants, accountability across the 6 canonical RBAC roles (`OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`), compliance boundaries, and externally verifiable behavior**.

---

## 1. Foundational Principle: Separating Technical Execution Capability from Provider-Granted Entitlement (`CONFIRMED_BY_OWNER`)

A foundational pillar governing all Tanpopo business rules is the strict separation between two concepts frequently conflated across the market:

| Dimension | **1. Technical Execution Capability** | **2. Provider-Granted Entitlement** |
|---|---|---|
| **Essence** | Software is technically capable of transmitting packets, attaching tokens, exchanging OAuth credentials, or parsing responses without syntax errors. | Commercial contracts, Terms of Service (TOS), and developer policies of the upstream provider legally authorize the principal to use that credential through an automated orchestration gateway. |
| **Representative Example** | Tanpopo's Plugin SDK provides OAuth 2.1 primitives, custom headers, and federated identity hooks so enterprises can connect authorized internal SSO gateways. | Metered commercial API keys (`Commercial API Key`) or OAuth applications explicitly registered and authorized by the provider for third-party gateway integration. |
| **Invalid Example (`REJECTED`)** | An external script scrapes browser cookies or closed OAuth tokens from personal consumer subscriptions (`ChatGPT Plus/Pro`, personal `Codex`, `Claude Pro/Max`, `Google Antigravity`, etc.) and successfully generates text. | **ZERO LEGAL ENTITLEMENT:** Flat-rate consumer subscriptions entitle an individual to interactive first-party app usage only; they grant zero entitlement to repurpose accounts as shared API endpoints or free API credits. |

---

## 2. Canonical Business Rules Catalog (`TP-BR-0101` .. `TP-BR-0803`)

### Group 01: Multi-Tenancy, Ownership, RBAC & Scope Sharing (`Tenancy & Scope Sharing`)

#### `TP-BR-0101` — Hard Cross-Tenant Isolation & Six Canonical RBAC Roles
- **Owner:** `ORG_OWNER` & `OPERATOR` | **Impact:** `Critical` (Security & Legal) | **Source / Status:** `APPROVED` (`UC-06`, `WF-06`, `CR-04`)
- **Rule:** Every business resource (`Secret Credential`, `Provider Connection`, `Route`, `Virtual Key`, `MCP Server Registration`, `Agent Endpoint`, `A2A Task`, `Budget Ledger`, `Usage Record`, `Audit Event`) belonging to an `Organization` is completely invisible and cannot be accessed, referenced, or enumerated for existence by any principal belonging to another `Organization` on the same `Instance`. Administrative authorization enforces the **6 canonical RBAC roles** (`OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`), where standalone `Personal Admin` bootstrap grants `OPERATOR + ORG_OWNER` to the initial owner.
- **Exception:** Only an instance-level `OPERATOR` performing host infrastructure maintenance or multi-org provisioning (`POST /admin/v1/organizations`) may view aggregate operational health of the server, and any direct intervention touching an `Organization` scope must emit an immutable entry into that `Organization`'s audit log.
- **Preliminary Testable Criteria:**
  - *Given:* `Virtual Key K_A` belongs to `Org A` and `Route R_B` belongs to `Org B`.
  - *When:* An `API Client` using `K_A` sends a request targeting `R_B` (or queries a trace ID or pagination cursor belonging to `Org B`).
  - *Then:* The system rejects the request with a non-enumerable error code (`404 resource_not_found` / `400 invalid_cursor`, indistinguishable from not-found within `Org A`), executes zero business logic against `Org B` resources, and records a security alert event.

#### `TP-BR-0102` — Controlled Downward Connection Sharing
- **Owner:** `ORG_OWNER` | **Impact:** `High` | **Source / Status:** `APPROVED`
- **Rule:** A `Provider Connection` owned at the `Organization` level may grant *consumption* rights downward to one or more child `Project` scopes within the same `Organization`, but `TEAM_ADMIN`, `FINOPS_VIEWER`, and `DEVELOPER` principals of beneficiary projects can never read, export, or modify the underlying `Secret Credential` bound to that connection. A connection created within a single `Project` cannot be shared laterally to sibling projects without transferring ownership upward to the `Organization`.
- **Exception:** Zero exceptions exist for exporting raw secret strings back to any user interface, CLI, or API once persisted.
- **Preliminary Testable Criteria:**
  - *Given:* `ORG_OWNER` shares `Connection C_Org` downward to `Project P1`.
  - *When:* The `TEAM_ADMIN` of `P1` creates a `Route` targeting a `Model Deployment` of `C_Org` and subsequently queries the detail view of `C_Org`.
  - *Then:* Route creation and inference calls succeed, whereas the credential field displays only an opaque reference/validity status, never returns the raw key, and denies secret mutation attempts (`403`).

#### `TP-BR-0103` — Single-Environment Virtual Key Scope
- **Owner:** `TEAM_ADMIN` | **Impact:** `Medium-High` | **Source / Status:** `APPROVED`
- **Rule:** Every `Virtual Key` is bound to exactly one `Environment` (e.g., `dev`, `staging`, or `prod`) belonging to a single `Project`, preventing runaway test scripts from exhausting production quotas or polluting production telemetry.
- **Exception:** Simplified `Personal Admin` bootstrap provisions a single default environment, yet every virtual key remains explicitly bound to that default environment.
- **Preliminary Testable Criteria:**
  - *Given:* `Virtual Key K_Dev` is issued inside the `dev` environment.
  - *When:* `K_Dev` is used to invoke a `Route` or `MCP Tool` restricted exclusively to `prod`.
  - *Then:* The request is rejected with `403 environment_scope_denied`.

#### `TP-BR-0104` — Four-Layer Domain Separation
- **Owner:** `Product Owner` & `Product Architect` | **Impact:** `Critical` (Foundational Domain Architecture) | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-003`, `REJ-004`)
- **Rule:** `Provider` (model inference), `Plugin/Extension` (extensibility packaging unit), `MCP` (contextual tools/resources), and `A2A` (inter-agent task delegation) must maintain distinct entity catalogs, data contracts, permission scopes, and governance policies; granting permission to invoke a model never implicitly grants permission to invoke MCP tools or A2A agents, and vice versa.
- **Exception:** Zero exceptions (all conflated designs are `REJECTED`).
- **Preliminary Testable Criteria:**
  - *Given:* A `Virtual Key` is authorized solely for `Provider` model inference.
  - *When:* The same key attempts to list or execute tools on the `MCP` plane or dispatch tasks on the `A2A` plane.
  - *Then:* The request is rejected (`403`) for lacking authorization on the target plane.

---

### Group 02: Provenance Classification, Publisher Trust & Consent (`Classification, Publisher Trust & Consent`)

#### `TP-BR-0201` — Three-Tier Public Extension Provenance, Orthogonal Enterprise Signing & Reserved Namespaces
- **Owner:** `Product Owner` & `SECURITY_REVIEWER` | **Impact:** `High` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-004`) & `APPROVED` (`CR-03` Option A)
- **Rule:** Every extension package in the system carries exactly one unforgeable public `origin_trust_tier` value paired with an independent enterprise signature indicator and strict namespace protection:
  1. `official`: Authored, cryptographically signed with Tanpopo's offline release root key, and maintained by the official Tanpopo project against documented, provider-authorized APIs.
  2. `verified`: Authored by a third party and verified through compliance review and automated contract tests in Tanpopo's official catalog.
  3. `unverified_external`: Any package installed directly from Git, URL, `.tpkg` archive upload, or private registry without certification from the Tanpopo project.
  - **Orthogonal Enterprise Signature Flag:** `extension_versions` maintains an independent **`enterprise_signed BOOLEAN NOT NULL DEFAULT FALSE`** (with `signing_key_fingerprint`) verified against the self-hosting organization's configured internal signing keys — enabling internal enterprise trust enforcement without overwriting `origin_trust_tier` or masquerading as `official`.
  - **Reserved Namespace Regex:** Package names matching **`^@tanpopo(-[a-z0-9]+)?/`** (including `@tanpopo/*`, `@tanpopo-official/*`, etc.) are strictly reserved for packages carrying a valid cryptographic signature from Tanpopo's offline release root key.
- **Exception:** Zero exceptions exist for spoofing `official` provenance or squatting `^@tanpopo(-[a-z0-9]+)?/` namespaces.
- **Preliminary Testable Criteria:**
  - *Given:* An `OPERATOR` uploads an external `.tpkg` archive whose manifest self-declares `origin_trust_tier = "official"` or uses package name `@tanpopo/custom-llm` or `@tanpopo-official/custom-llm` without a valid Tanpopo offline root signature.
  - *When:* The installer inspects the package during `VERIFYING` (`SM-01`).
  - *Then:* Reserved namespace spoofing is hard-rejected (`403 reserved_namespace_spoofing`, `SM-01` → `REJECTED_INVALID`), spoofed `official` claims on non-reserved namespaces are rejected or downgraded to `unverified_external`, and a security `AuditEvent` is recorded.

#### `TP-BR-0202` — Organization Plugin Trust Policy (`org_extension_policies`)
- **Owner:** `ORG_OWNER` | **Impact:** `High` | **Source / Status:** `APPROVED` (`CR-03`, `CR-04`)
- **Rule:** Each `Organization` governs permitted extensions via `org_extension_policies` and configures its minimum provenance threshold `min_plugin_trust` (`OFFICIAL_ONLY`, `VERIFIED_OR_ENTERPRISE_SIGNED`, `VERIFIED_ONLY`, or `ANY_APPROVED`).
- **Exception:** By default in newly created production organizations, `unverified_external` packages lacking `enterprise_signed = TRUE` are unavailable to child `Project` scopes unless explicitly enabled by `ORG_OWNER`.
- **Preliminary Testable Criteria:**
  - *Given:* `Org A` enforces `min_plugin_trust = "VERIFIED_OR_ENTERPRISE_SIGNED"`; the host instance also has `Adapter X` installed with `origin_trust_tier = "unverified_external"` and `enterprise_signed = FALSE`.
  - *When:* A `TEAM_ADMIN` in `Org A` attempts to create a `Provider Connection` backed by `Adapter X`.
  - *Then:* The operation is blocked (`403`) by `Org A`'s plugin trust policy.

#### `TP-BR-0203` — Explicit Informed Consent Gate
- **Owner:** `OPERATOR`, `ORG_OWNER` & `SECURITY_REVIEWER` | **Impact:** `Critical` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-002`)
- **Rule:** Zero extensions may transition out of `PENDING_PERMISSION_APPROVAL` into `INSTALLED_DISABLED` or `ENABLED_ACTIVE` (`SM-01`) until an authorized principal (plus an independent `SECURITY_REVIEWER` when `Four-Eyes` is enabled) reviews and explicitly approves the complete permission inspection sheet declared by that package (outbound network `allowed_hosts`, `Blind Broker` credential slot bindings, and execution tier limits).
- **Exception:** Declarative GitOps automation may bypass interactive UI prompts only when the administrator-committed `tanpopo.lock` lockfile explicitly records both the approved permission set and the exact cryptographic SHA-256 digest of that package version.
- **Preliminary Testable Criteria:**
  - *Given:* Package `Plugin P` declares outbound network access to `api.example.com`.
  - *When:* Installation runs without interactive permission approval or a matching signed consent entry in `tanpopo.lock`.
  - *Then:* The package halts in `PENDING_PERMISSION_APPROVAL` and cannot process any traffic.

---

### Group 03: Legitimate Entitlement & Consumer Subscription Restrictions (`Entitlement & Subscription Restrictions`)

#### `TP-BR-0301` — No Legalization by Disclaimer
- **Owner:** `Product Owner` | **Impact:** `Critical` (Legal & Core Positioning) | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-004`, `REJ-003`)
- **Rule:** Neither the existence of a third-party plugin nor a user checking an *"I understand and accept the risk"* box converts a provider-TOS-violating connection mechanism into a legitimate or supported Tanpopo product capability. The system never implements *"accept risk to unlock consumer subscription-to-API conversion"* flows.
- **Exception:** Zero exceptions.
- **Preliminary Testable Criteria:**
  - *Given:* All administrative UIs, the Setup Wizard, user documentation, and Tanpopo's official catalog.
  - *When:* Auditing every connection onboarding workflow and product message.
  - *Then:* Zero toggles, configuration templates, or claims exist that convert personal consumer subscriptions into shared API quotas behind a disclaimer dialog.

#### `TP-BR-0302` — Zero Consumer Subscription Arbitrage
- **Owner:** `Product Owner` | **Impact:** `Critical` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-004`, `REJ-003`)
- **Rule:** Tanpopo never develops, bundles, lists on its official Marketplace, or claims that flat-rate personal consumer subscriptions (such as `ChatGPT Plus/Pro`, personal `Codex`, `Claude Pro/Max`, `Google AI Pro/Ultra/Antigravity`, etc.) equate to free API credits or can be pooled into shared team/application APIs.
- **Exception:** Only if an upstream model vendor formally publishes a documented enterprise/developer API or OAuth program whose public terms explicitly authorize third-party gateway integration may that mechanism be evaluated under canonical onboarding (`TP-BR-0303`).
- **Preliminary Testable Criteria:**
  - *Given:* An `Extension Publisher` submits a Marketplace adapter package that scrapes browser session cookies or impersonates closed first-party client IDs of a personal subscription.
  - *When:* A Marketplace `Reviewer` or compliance scanner conducts verification.
  - *Then:* The submission is unconditionally rejected and denied `official` or `verified` status.

#### `TP-BR-0303` — Authorized OAuth Legitimacy
- **Owner:** `ORG_OWNER` & `SECURITY_REVIEWER` | **Impact:** `High` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-004`)
- **Rule:** Connections using OAuth or federated identity on official channels must use a legitimate `client_id` (registered by the self-hosting organization or publicly authorized by the vendor) alongside documented `scopes` matching their stated purpose; covertly embedding client credentials reverse-engineered from proprietary third-party applications is strictly prohibited.
- **Exception:** Fully self-owned internal enterprise gateways where the self-hosting enterprise legally owns and operates both ends of the authentication boundary.
- **Preliminary Testable Criteria:**
  - *Given:* An `Authorized OAuth` connection configured on an `Official Adapter`.
  - *When:* The system initiates authorization against the `External IdP`.
  - *Then:* The request uses transparently configured client parameters and verifies granted scopes before transitioning the connection to `ACTIVE`.

#### `TP-BR-0304` — Expired Credential Containment
- **Owner:** `OPERATOR` & `TEAM_ADMIN` | **Impact:** `High` | **Source / Status:** `APPROVED` (`WF-05`, `SM-02`, `SM-03`)
- **Rule:** Whenever a `Provider Connection` credential expires without automatic renewal or is revoked upstream, the system must: (1) immediately transition the connection to `AUTH_EXPIRED_REAUTH_REQUIRED` (`SM-02`) / `REAUTH_REQUIRED` (`SM-03`) to stop futile upstream calls that trigger account lockouts, (2) execute at most one single-flight refresh per connection at any instant, and (3) return a sanitized, canonical error code to the `API Client` without ever echoing raw upstream tokens or secret headers.
- **Exception:** Zero exceptions exist for secret scrubbing in error payloads.
- **Preliminary Testable Criteria:**
  - *Given:* `Provider Connection C1` holds an upstream-revoked OAuth refresh token while receiving 50 concurrent requests.
  - *When:* The access token hits its expiration window.
  - *Then:* Exactly one refresh request is dispatched; upon receiving upstream rejection, `C1` immediately transitions to `AUTH_EXPIRED_REAUTH_REQUIRED`, emits an operator alert, and leaks zero raw token or secret strings to any `API Client` response.

---

### Group 04: Permission Boundaries & Untrusted Plugin Containment

#### `TP-BR-0401` — Default-Deny Plugin Permissions
- **Owner:** `OPERATOR` & `SECURITY_REVIEWER` | **Impact:** `Critical` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-002`, `C-02`)
- **Rule:** At runtime, an extension holds zero outbound network access, zero host filesystem access, zero host environment variable access, and zero access to credentials of other connections beyond the exact capabilities declared in its `Extension Manifest` and approved by an administrator for that invocation.
- **Exception:** Zero exceptions.
- **Preliminary Testable Criteria:**
  - *Given:* `Plugin P` is approved strictly for outbound calls to `api.vendor-a.example`.
  - *When:* During request handling, `Plugin P` attempts to connect to `collector.unknown.example` or read configuration belonging to `Connection Vendor B`.
  - *Then:* The attempt is blocked prior to network egress and generates a security violation audit event.

#### `TP-BR-0402` — Blind Credential Injection, Cookie Ban & Zero Raw Secrets in Wasm Memory
- **Owner:** `OPERATOR` | **Impact:** `Critical` | **Source / Status:** `APPROVED` (`IDEA-01`, `C-02`, `CR-02`, `CR-05`, `DEC-004`)
- **Rule:** Tier-1 Declarative Packs and Tier-2 Wasm Sandboxes **never** receive raw secret strings in guest memory (`scoped_secret_unseal` is completely prohibited in Tier 1 and Tier 2). The host gateway's `BlindHostCredentialBroker` injects bearer/header/query secrets or executes structured Host-side request signing (`HostCryptoSignSpec`: `HMAC-SHA256`, `AWS_SIGV4`, `RSA-SHA256`) strictly at the outbound network boundary toward approved `allowed_hosts`, while unconditionally banning outbound `Cookie` / `Set-Cookie` credential injection (`DEC-004`).
- **Exception:** Only an `OPERATOR`-provisioned `Tier-3 Isolated Sidecar` (`TP-FR-0308`, `v0.3`) running over `0600` UDS / `mTLS` may host bespoke closed-source enterprise signing libraries under explicit high-risk administrative governance.
- **Preliminary Testable Criteria:**
  - *Given:* A Tier-2 Wasm `Community Adapter` using API-key authentication or HMAC signing, plus a malicious manifest attempting `Cookie` header injection.
  - *When:* Routing an inference call through that adapter and inspecting `100%` of Wasm linear memory.
  - *Then:* Wasm guest memory contains `0` bytes of the plaintext API key, `Cookie` injection is hard-blocked, and the approved upstream host receives the valid Host-injected authentication header.

#### `TP-BR-0403` — Permission Escalation Block on Update
- **Owner:** `ORG_OWNER` & `OPERATOR` | **Impact:** `Critical` | **Source / Status:** `APPROVED` (`WF-11`, `SM-01`)
- **Rule:** Every extension update workflow (manual or scheduled) must diff the target version's requested permissions against the currently approved permission set. If the new version introduces any new permission or broadened scope, it must never auto-activate for live traffic and must halt in `UPDATING_PENDING_RECONSENT` awaiting explicit re-approval.
- **Exception:** Updates whose requested permissions are a strict subset of or equal to previously approved permissions and remain contract-compatible may cut over normally via `DRAINING`.
- **Preliminary Testable Criteria:**
  - *Given:* `Plugin P@1.0.0` runs with approved access to `a.example.com`; `P@1.1.0` adds `b.example.com` to its manifest.
  - *When:* An unattended update runs without new permission consent.
  - *Then:* `1.1.0` is held in `UPDATING_PENDING_RECONSENT`, live traffic continues uninterrupted through `1.0.0`, and administrators are notified to review the permission diff.

#### `TP-BR-0404` — Plugin Fault Isolation & Immediate Quarantine
- **Owner:** `OPERATOR` | **Impact:** `High` | **Source / Status:** `CONFIRMED_BY_OWNER` (`C-02`) & `APPROVED` (`SM-01`)
- **Rule:** Hangs, infinite loops, resource exhaustion, or unhandled exceptions inside an extension may fail only the individual request invoking that extension (`502 extension_execution_fault`) and must never crash the Tanpopo host process or degrade unrelated `Provider Connection` traffic. Every non-terminal extension state in `SM-01` supports an immediate emergency transition to `QUARANTINED`, severing all traffic through that extension system-wide.
- **Exception:** Zero exceptions.
- **Preliminary Testable Criteria:**
  - *Given:* `Adapter X` enters an infinite loop while `Adapter Y` operates normally.
  - *When:* Concurrent requests are sent to both `Adapter X` and `Adapter Y`.
  - *Then:* Requests to `Adapter X` terminate cleanly at the execution quota with an isolated error, while requests to `Adapter Y` succeed unaffected.

---

### Group 05: Routing, Residency, Policy Narrowing, Streaming & Fail Posture

#### `TP-BR-0501` — Hard Data Residency Enforcement
- **Owner:** `ORG_OWNER` | **Impact:** `Critical` (Data Compliance) | **Source / Status:** `APPROVED` (`UC-05`, `WF-07`)
- **Rule:** Whenever an `Organization`, `Project`, `Route`, or per-request policy constrains geographic data processing regions (e.g., `EU` only or `ON_PREM` only), the router must exclude every non-compliant `Model Deployment` during both primary selection and all `Failover` attempts.
- **Exception:** Data residency boundaries must never be bypassed to preserve technical availability.
- **Preliminary Testable Criteria:**
  - *Given:* `Route R1` enforces `Data Region = EU` across `Deployment D_EU` (currently failing `503` / `CIRCUIT_OPEN_UNHEALTHY`) and `Deployment D_US` (`ACTIVE_HEALTHY`).
  - *When:* A request arrives at `Route R1`.
  - *Then:* The router refuses to fail over to `D_US` and returns `503 no_compliant_deployment_available`.

#### `TP-BR-0502` — Failure Posture Matrix (`Fail-Closed` vs `Fail-Open`) & Monotonic Policy Narrowing
- **Owner:** `ORG_OWNER` & `Product Architect` | **Impact:** `Critical` | **Source / Status:** `APPROVED` (`TP-FR-0701`)
- **Rule:**
  1. **Unconditionally `Fail-Closed` (non-overridable):** Identity/virtual-key authentication, cross-tenant isolation, Plugin permission boundaries, data residency enforcement, `destructive` tool approval gates, and `Hard Budget Cap` enforcement.
  2. **Monotonic Hierarchical Policy Narrowing:** Policies inherit downward from `Organization` → `Project` → `Environment`; child scopes may strictly narrow parent constraints and may never broaden allowed regions, lower `min_plugin_trust`, raise parent ceilings, or flip category-(1) controls to `Fail-Open` (rejected at write time with `422 policy_relaxation_forbidden`).
  3. **Configurable `Fail-Open` with audited warning counters (at `ORG_OWNER` discretion):** Non-critical auxiliary metadata enrichment or secondary external telemetry export (`OTLP`).
- **Exception:** Enabling `Fail-Open` on any category-(1) control or relaxing a parent policy is strictly prohibited.
- **Preliminary Testable Criteria:**
  - *Given:* `Org A` restricts `allowed_data_regions = ["EU"]`, or the hard-budget evaluator experiences a transient outage.
  - *When:* A `TEAM_ADMIN` attempts to save a child `Project` policy allowing `"US"` (Case 1), or a billable request reaches the gateway during evaluator failure (Case 2).
  - *Then:* Case 1 is rejected with `422 policy_relaxation_forbidden`; Case 2 is rejected safely (`Fail-Closed`) prior to egress.

#### `TP-BR-0503` — No Silent Mid-Stream Fallback
- **Owner:** `Product Architect` | **Impact:** `High` (Downstream Application Data Integrity) | **Source / Status:** `APPROVED` (`UC-01`, `WF-06`, `WF-07`)
- **Rule:** Automatic failover to a backup `Model Deployment` is permitted **strictly before** the first content chunk or tool-call delta is emitted to the `API Client`. Once at least one content chunk has been streamed (`Partial Stream`), any upstream error or disconnect must terminate the stream with an explicit `mid_stream_abort` error event frame rather than silently invoking a second model and concatenating divergent outputs into the same stream.
- **Exception:** Empty protocol keep-alive heartbeats carrying zero semantic payload bytes do not count as content emission.
- **Preliminary Testable Criteria:**
  - *Given:* A `Route` configures `D1` (primary) and `D2` (fallback); `D1` returns `200 OK`, streams 20 words, and drops the connection.
  - *When:* The `API Client` is consuming the response stream.
  - *Then:* `D2` is never invoked; the `API Client` receives a deterministic `mid_stream_abort` error frame, and tokens already emitted by `D1` enter `ESTIMATED` settlement.

#### `TP-BR-0504` — Strict Capability Verification
- **Owner:** `Product Architect` | **Impact:** `Medium-High` | **Source / Status:** `APPROVED` (`UC-01`)
- **Rule:** When a caller request requires core capabilities that materially affect output correctness (e.g., strict `json_schema`, tool calling, image/audio/document inputs, or `rerank`), the gateway must never silently strip those parameters to force-fit an incapable model; it must filter for a capable deployment or fail fast with `400 capability_not_supported`.
- **Exception:** Administrators may explicitly opt into equivalent parameter adaptation on a `Route` solely for non-semantic advisory hints.
- **Preliminary Testable Criteria:**
  - *Given:* A route contains only text-only models (`vision = false`).
  - *When:* An `API Client` submits a request containing image inputs.
  - *Then:* The gateway immediately returns `400 capability_not_supported` without incurring upstream provider calls.

#### `TP-BR-0505` — Safe Retry & Cancellation Propagation
- **Owner:** `Product Architect` | **Impact:** `High` | **Source / Status:** `APPROVED`
- **Rule:** Automatic retries apply exclusively to transient pre-stream failures (connect errors, `429`/`503` prior to content emission) bounded by the route's retry ceiling. Whenever an `API Client` cancels an active request, the gateway must immediately propagate cancellation to the upstream provider connection to halt further token spend.
- **Exception:** Client-side input/auth/policy errors (`400`, `401`, `403`, `409`, `422`) are never retried automatically.
- **Preliminary Testable Criteria:**
  - *Given:* A long-form streaming generation request is active at the 3-second mark.
  - *When:* The `API Client` closes its connection to cancel the request.
  - *Then:* The upstream provider connection is aborted promptly and the budget hold settles immediately.

---

### Group 06: MCP Tool Governance, Approvals & Inter-Agent A2A (`MCP, Approvals & A2A`)

#### `TP-BR-0601` — MCP Tool Scoping & Schema Pinning
- **Owner:** `ORG_OWNER` & `TEAM_ADMIN` | **Impact:** `Critical` | **Source / Status:** `APPROVED` (`UC-06`, `IDEA-03`, `COND-PO-04`)
- **Rule:** An `MCP Client` connecting over `MCP Specification 2025-03-26 (Streamable HTTP)` may discover and invoke strictly the subset of tools authorized for its `Virtual Key`. Every tool enabled in enforced environments must pin an RFC 8785 JCS cryptographic fingerprint of its name, description, and parameter schema (`schema_hash`) at approval time; if the downstream `MCP Server` alters descriptions or parameters causing `schema_hash` drift, the tool must be automatically locked until re-approved by an administrator.
- **Exception:** Local `dev` environments may opt into `WARN_ONLY` mode while engineers actively iterate on their own tool definitions.
- **Preliminary Testable Criteria:**
  - *Given:* Tool `read_customer` is approved with fingerprint `H1` in `prod`.
  - *When:* The `MCP Server` modifies `read_customer`'s text description (changing its fingerprint to `H2`) without administrative re-approval.
  - *Then:* The gateway blocks `read_customer` invocations (`403 schema_hash_drift_blocked`) and emits a tool schema drift security alert.

#### `TP-BR-0602` — Destructive Tool Approval Binding (`SM-06`)
- **Owner:** `ORG_OWNER` & `SECURITY_REVIEWER` | **Impact:** `Critical` | **Source / Status:** `APPROVED` (`WF-09`, `SM-06`, `CR-08`)
- **Rule:** Every invocation of an MCP tool classified as `destructive` (or matching high-risk project policy) creates an `ApprovalRequest` in state `PENDING` (`SM-06`: `PENDING -> APPROVED -> CONSUMED | REJECTED | EXPIRED`) and executes strictly once (`APPROVED -> CONSUMED`) only after an authorized `SECURITY_REVIEWER` or `ORG_OWNER` approves it within its TTL. Approval is cryptographically bound to the canonical 5-tuple digest `SHA-256(JCS({org_id, virtual_key_id, server_slug, tool_name, arguments}))`; any argument mutation or replay after `CONSUMED`/`EXPIRED` invalidates the approval.
- **Exception:** Self-approval by the initiating agent or principal is strictly forbidden (`403 self_approval_forbidden`).
- **Preliminary Testable Criteria:**
  - *Given:* A `SECURITY_REVIEWER` transitions `delete_record(id="123")` from `PENDING` to `APPROVED`.
  - *When:* An agent attempts to redeem that approval for `delete_record(id="456")`, replays it a second time after `CONSUMED`, or invokes it after TTL expiry (`EXPIRED`).
  - *Then:* Execution is denied (`403 parameter_hash_mismatch` / approval invalid) and an audit violation event is recorded.

#### `TP-BR-0603` — No Blind Retry on Side-Effecting Tools
- **Owner:** `Product Architect` | **Impact:** `High` | **Source / Status:** `APPROVED` (`WF-09`)
- **Rule:** For `mutating` or `destructive` MCP tools (and non-idempotent `A2A` tasks in `SM-09`), if a network disconnect or timeout occurs after the call has been dispatched downstream, the gateway **must never** automatically re-send the command—preventing duplicate real-world side effects—and must instead return `ambiguous_side_effect_timeout` with a correlation trace ID.
- **Exception:** Only tools explicitly verified as side-effect-free `READ_ONLY` operations may apply transient network retry policies.
- **Preliminary Testable Criteria:**
  - *Given:* Tool `send_invoice_email` is classified as `MUTATING`.
  - *When:* The `MCP Server` fails to respond before timeout after receiving the dispatched request.
  - *Then:* The gateway dispatches the call at most once, performs zero retries, and returns `ambiguous_side_effect_timeout` requiring reconciliation.

#### `TP-BR-0604` — No A2A Privilege Amplification (`SM-09`)
- **Owner:** `ORG_OWNER` | **Impact:** `High` | **Source / Status:** `APPROVED` (`SM-09`, `CR-08`)
- **Rule:** When an `Agent` delegates a child task (`submitted -> working <-> input-required -> completed | failed | canceled`) to another `Agent` across the A2A plane, the child task's data-residency scope, maximum TTL, hop depth (`<= 5` default), and carved sub-budget ceiling must never exceed the entitlements and remaining available budget of the originating parent task.
- **Exception:** Zero exceptions.
- **Preliminary Testable Criteria:**
  - *Given:* A parent agent has `2 USD` remaining budget and data access restricted to `EU`.
  - *When:* The parent agent initiates a child A2A task requesting a `10 USD` ceiling or targeting a non-`EU` agent endpoint.
  - *Then:* The A2A plane rejects the privilege-amplifying delegation immediately (`403`).

---

### Group 07: Usage Reporting, Budgets & Chargeback (`Usage, Budgets & Chargeback`)

#### `TP-BR-0701` — Hierarchical Budget Ceiling & Revocation Preemption
- **Owner:** `ORG_OWNER` & `TEAM_ADMIN` | **Impact:** `High` | **Source / Status:** `APPROVED` (`UC-08`, `WF-08`, `SM-05`)
- **Rule:** A billable request is admitted only when sufficient available budget exists simultaneously across every applicable level in its hierarchy (`Virtual Key` → `Environment` → `Project` → `Organization`). Even if a `Virtual Key` retains unused individual quota, hitting the parent `Project` or `Organization` hard ceiling must block the request (`429 project_budget_exceeded`). Furthermore, revoking a `QUOTA_PAUSED` virtual key (`QUOTA_PAUSED -> REVOKED`) permanently terminates the key so billing-period resets can never revive it.
- **Exception:** Self-hosted internal models explicitly configured with `0` unit price (`SELF_HOSTED_INFRA`).
- **Preliminary Testable Criteria:**
  - *Given:* `Virtual Key K1` has a `50 USD` ceiling (`10 USD` spent), while parent `Project P1` has a `100 USD` ceiling already `100 USD` consumed across sibling keys.
  - *When:* `K1` submits a new request to a paid model.
  - *Then:* The request is rejected with `429 project_budget_exceeded`.

#### `TP-BR-0702` — Mandatory Pre-Execution Budget Hold & Orphan Lease Recovery
- **Owner:** `ORG_OWNER` | **Impact:** `High` | **Source / Status:** `APPROVED` (`UC-08`, `SM-07`)
- **Rule:** For every scope governed by a hard budget ceiling, the gateway must deduct a temporary estimated hold (`RESERVED_HOLD`) from available balance **prior to** forwarding the request upstream, preventing concurrent request bursts from racing past a nearly exhausted ceiling; orphaned holds in either `RESERVED_HOLD` or `STREAMING_ACCRUING` left behind by crashed worker pods past their lease TTL transition cleanly to `EXPIRED_RECLAIMED` with an audit entry.
- **Exception:** Requests rejected at pre-egress auth/policy gates must immediately release any provisional hold (`RELEASED_UNUSED`).
- **Preliminary Testable Criteria:**
  - *Given:* Hard project budget has exactly `0.10 USD` remaining; each request requires a `0.05 USD` estimated hold.
  - *When:* 20 concurrent requests arrive simultaneously.
  - *Then:* At most 2 requests acquire holds and proceed upstream; the remaining 18 are rejected immediately prior to egress (`429 project_budget_exceeded`).

#### `TP-BR-0703` — Explicit Usage Accuracy Classification
- **Owner:** `ORG_OWNER` & `FINOPS_VIEWER` | **Impact:** `High` (Financial Integrity) | **Source / Status:** `APPROVED` (`WF-08`, `CR-09`)
- **Rule:** Every usage record and ledger entry must carry an explicit measurement accuracy tag:
  - `EXACT`: Backed by authoritative token/usage counters from the upstream provider wire protocol (including automatic `stream_options.include_usage = true` injection on OpenAI-compatible streams).
  - `ESTIMATED`: Computed via Tanpopo's deterministic estimator when a stream aborts mid-flight or the upstream omits usage counters.
  - `UNMETERED_FLAGGED`: Usage quantity cannot be automatically estimated or lacks pricing metadata; flagged for manual reconciliation.
  Financial and `Chargeback` reports inspected by `ORG_OWNER` or `FINOPS_VIEWER` **must never** silently record mid-stream aborts or missing-usage calls as `0 USD` exact spend without segregating estimated/unmetered columns.
- **Exception:** Zero exceptions.
- **Preliminary Testable Criteria:**
  - *Given:* A streaming response emits 500 words before losing upstream connectivity prior to the final token-summary frame.
  - *When:* The gateway closes the stream and exports the project's cost report.
  - *Then:* The transaction settles with `ESTIMATED` spend `> 0` based on streamed bytes/tokens and appears explicitly under the estimated cost column.

---

### Group 08: Concurrency, Retention Privacy & Audit Trail (`Concurrency, Retention & Audit`)

#### `TP-BR-0801` — Optimistic Configuration Concurrency
- **Owner:** `OPERATOR` & `TEAM_ADMIN` | **Impact:** `Medium-High` | **Source / Status:** `APPROVED` (`WF-11`)
- **Rule:** When multiple administrators or automation pipelines concurrently update a sensitive control-plane entity (`Route`, `Policy`, `Budget`, `Extension Installation`), the system accepts updates only when matching the latest persisted version (`ETag` / `If-Match`) and rejects stale writes with `409 Conflict`, preventing accidental clobbering of newly added security policies.
- **Exception:** Emergency containment actions (`Revoke` / `Quarantine` / `Disable` / `Suspend`) preemptively execute immediately to protect system safety.
- **Preliminary Testable Criteria:**
  - *Given:* Administrators A and B both load `Policy P` at version `v3`; A saves an update advancing `P` to `v4`.
  - *When:* B attempts to save changes still referencing base version `v3`.
  - *Then:* The system rejects B's write with `409 Conflict` prompting B to review `v4` before retrying.

#### `TP-BR-0802` — Privacy-by-Default, Disabled-by-Default Cache & Clean Crypto-Shredding
- **Owner:** `ORG_OWNER` | **Impact:** `High` | **Source / Status:** `APPROVED` (`WF-13`, `CR-06`, `CR-09`)
- **Rule:** Under default configuration, the gateway never persists full-text end-user prompts or completions in durable storage and keeps response caching (`TP-FR-0706`) **disabled by default**. When an administrator explicitly opts into debug payload logging or exact-match response caching, payloads are encrypted under the project's `Project DEK` with a bounded automatic expiration `TTL`. Upon deleting a `Project` or `Organization`, active streams drain and destroying the `Project DEK` / `Org DEK` irrecoverably crypto-shreds all unit secrets, debug payloads, and cached responses.
- **Exception:** Aggregated financial ledger entries and security audit records containing zero sensitive prompt payloads are retained for the organization's statutory audit window.
- **Preliminary Testable Criteria:**
  - *Given:* A newly created `Project` using default settings receives a request containing sensitive prompt text.
  - *When:* Inspecting durable log and cache storage.
  - *Then:* Only operational metadata (timestamp, model, latency, token counts, cost, status code) is present, with zero raw prompt or completion text stored.

#### `TP-BR-0803` — Immutable Security & Governance Audit
- **Owner:** `ORG_OWNER`, `SECURITY_REVIEWER` & `OPERATOR` | **Impact:** `Critical` | **Source / Status:** `APPROVED` (`CR-06`)
- **Rule:** Every key lifecycle action (create/update/rotate/revoke), plugin lifecycle action (install/approve/upgrade/rollback/quarantine), policy or budget mutation, break-glass admin recovery (`tanpopo admin reset-local-owner`), sensitive tool approval/rejection (`SM-06`), and blocked security violation (cross-tenant probe, policy relaxation attempt, unauthorized plugin egress, reserved namespace spoofing, tool `schema_hash` drift) must emit an immutable, hash-chained `Audit Event` record; no administrator may edit or selectively delete individual audit rows prior to retention expiry.
- **Exception:** Zero exceptions.
- **Preliminary Testable Criteria:**
  - *Given:* A `TEAM_ADMIN` revokes a `Virtual Key` or an `OPERATOR` installs an `unverified_external` plugin.
  - *When:* Querying the audit log and attempting to submit an API call to modify or delete that audit entry.
  - *Then:* The record captures the acting principal, timestamp, target entity, and `origin_trust_tier` / `enterprise_signed` metadata, and the system rejects all attempts to mutate or delete individual audit entries (`403`).
