# Canonical Business Rules — Tanpopo

> **Business Analysis Specification (Phase ASTRA A02 — `DRAFT / UNLOCKED SCOPE`)**
> - **Version:** `0.1.0-a02-draft`
> - **Last Updated:** `2026-11-11`
> - **Authoring Role:** Lead Business Analyst & Product Architect
> - **Governance Status:** **Unlocked-scope draft (`DRAFT`)** — awaiting Product Owner approval of **Gate G1** and **Gate G2** in [`docs/governance/decisions.md`](../governance/decisions.md).
> - **Authoring Principle:** Every rule defines strictly **business invariants, accountability, compliance boundaries, and externally verifiable behavior**, never conflating internal implementation technology choices except confirmed product constraints (`C-01`..`C-04`).

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

### Group 01: Multi-Tenancy, Ownership & Scope Sharing (`Tenancy & Scope Sharing`)

#### `TP-BR-0101` — Hard Cross-Tenant Isolation
- **Owner:** `Organization Owner` & `Operator` | **Impact:** `Critical` (Security & Legal) | **Source / Status:** `PROPOSED` (From `UC-06`, `WF-06`)
- **Rule:** Every business resource (`Secret Credential`, `Provider Connection`, `Route`, `Virtual Key`, `MCP Server Registration`, `Agent Endpoint`, `Budget Ledger`, `Usage Record`, `Audit Event`) belonging to an `Organization` is completely invisible and cannot be accessed, referenced, or enumerated for existence by any principal belonging to another `Organization` on the same `Instance`.
- **Exception:** Only an instance-level `Operator` performing host infrastructure maintenance may view aggregate operational health of the server (and any direct intervention touching an `Organization` scope must emit an immutable entry into that `Organization`'s audit log).
- **Preliminary Testable Criteria:**
  - *Given:* `Virtual Key K_A` belongs to `Org A` and `Route R_B` belongs to `Org B`.
  - *When:* An `API Client` using `K_A` sends a request targeting `R_B` (or queries a trace ID belonging to `Org B`).
  - *Then:* The system rejects the request with a non-enumerable error code (indistinguishable from not-found within `Org A`), executes zero business logic against `Org B` resources, and records a security alert event.

#### `TP-BR-0102` — Controlled Downward Connection Sharing
- **Owner:** `Organization Owner` | **Impact:** `High` | **Source / Status:** `PROPOSED`
- **Rule:** A `Provider Connection` owned at the `Organization` level may grant *consumption* rights downward to one or more child `Project` scopes within the same `Organization`, but `Team Admin` and `Developer` principals of beneficiary projects can never read, export, or modify the underlying `Secret Credential` bound to that connection. A connection created within a single `Project` cannot be shared laterally to sibling projects without transferring ownership upward to the `Organization`.
- **Exception:** Zero exceptions exist for exporting raw secret strings back to any user interface once persisted.
- **Preliminary Testable Criteria:**
  - *Given:* `Org Owner` shares `Connection C_Org` downward to `Project P1`.
  - *When:* The `Team Admin` of `P1` creates a `Route` targeting a `Model Deployment` of `C_Org` and subsequently queries the detail view of `C_Org`.
  - *Then:* Route creation and inference calls succeed, whereas the credential field displays only an opaque reference/validity status, never returns the raw key, and denies secret mutation attempts.

#### `TP-BR-0103` — Single-Environment Virtual Key Scope
- **Owner:** `Team Admin` | **Impact:** `Medium-High` | **Source / Status:** `PROPOSED`
- **Rule:** Every `Virtual Key` is bound to exactly one `Environment` (e.g., `development`, `staging`, or `production`) belonging to a single `Project`, preventing runaway test scripts from exhausting production quotas or polluting production telemetry.
- **Exception:** Simplified `Personal Mode` provisions a single default environment, yet every virtual key remains explicitly bound to that default environment.
- **Preliminary Testable Criteria:**
  - *Given:* `Virtual Key K_Dev` is issued inside the `development` environment.
  - *When:* `K_Dev` is used to invoke a `Route` or `MCP Tool` restricted exclusively to `production`.
  - *Then:* The request is rejected due to environment scope mismatch.

#### `TP-BR-0104` — Four-Layer Domain Separation
- **Owner:** `Product Owner` & `Product Architect` | **Impact:** `Critical` (Foundational Domain Architecture) | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-003`, `REJ-004`)
- **Rule:** `Provider` (model inference), `Plugin/Extension` (extensibility packaging unit), `MCP` (contextual tools/resources), and `A2A` (inter-agent task delegation) must maintain distinct entity catalogs, data contracts, permission scopes, and governance policies; granting permission to invoke a model never implicitly grants permission to invoke MCP tools or A2A agents, and vice versa.
- **Exception:** Zero exceptions (all conflated designs are `REJECTED`).
- **Preliminary Testable Criteria:**
  - *Given:* A `Virtual Key` is authorized solely for `Provider` model inference.
  - *When:* The same key attempts to list or execute tools on the `MCP` plane or dispatch tasks on the `A2A` plane.
  - *Then:* The request is rejected for lacking authorization on the target plane.

---

### Group 02: Provenance Classification, Publisher Trust & Consent (`Classification, Publisher Trust & Consent`)

#### `TP-BR-0201` — Three-Tier Extension Provenance
- **Owner:** `Product Owner` & `Reviewer` | **Impact:** `High` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-004`) & `PROPOSED` (3-tier taxonomy)
- **Rule:** Every extension in the system must display exactly one unforgeable provenance tier badge:
  1. `official`: Authored, cryptographically signed, and maintained by the official Tanpopo project against documented, provider-authorized APIs.
  2. `verified`: Authored by a third party and verified through compliance review and automated contract tests in Tanpopo's official catalog.
  3. `unverified-external`: Any package installed directly from Git, URL, archive upload, or private registry without certification from the Tanpopo project.
- **Exception:** Self-hosting organizations may attach an internal endorsement tag (`Org-Approved`), which never overwrites or masquerades as Tanpopo's `official` badge.
- **Preliminary Testable Criteria:**
  - *Given:* An `Operator` uploads an internal archive whose manifest self-declares provenance `"official"` without a valid Tanpopo release signature.
  - *When:* The system inspects the package during installation.
  - *Then:* The system rejects the spoofed claim (or forcibly downgrades the badge to `unverified-external` with a provenance spoofing warning).

#### `TP-BR-0202` — Organization Plugin Trust Policy
- **Owner:** `Organization Owner` | **Impact:** `High` | **Source / Status:** `PROPOSED`
- **Rule:** Each `Organization` may configure the minimum provenance trust tier permitted to serve its traffic (e.g., *"Allow `official` only"*, *"Allow `official` + `verified`"*, or *"Additionally allow explicitly allowlisted internal packages"*).
- **Exception:** By default in newly created production organizations, `unverified-external` packages are unavailable to child `Project` scopes unless explicitly enabled by the `Organization Owner`.
- **Preliminary Testable Criteria:**
  - *Given:* `Org A` enforces a policy permitting only `official` and `verified` extensions; the host instance also has `Adapter X` installed with tier `unverified-external`.
  - *When:* A `Team Admin` in `Org A` attempts to create a `Provider Connection` backed by `Adapter X`.
  - *Then:* The operation is blocked by the organization's plugin trust policy.

#### `TP-BR-0203` — Explicit Informed Consent Gate
- **Owner:** `Operator` & `Organization Owner` | **Impact:** `Critical` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-002`)
- **Rule:** Zero extensions may transition to an active traffic-serving state until an authorized principal reviews and explicitly approves the complete permission inspection sheet declared by that package (outbound network hosts, credential access classes, and inspected payload scopes).
- **Exception:** Declarative GitOps automation may bypass interactive UI prompts only when the administrator-committed lockfile explicitly records both the approved permission set and the exact cryptographic SHA-256 digest of that package version.
- **Preliminary Testable Criteria:**
  - *Given:* Package `Plugin P` declares outbound network access to `api.example.com`.
  - *When:* Installation runs without interactive permission approval or a matching signed consent entry in the lockfile.
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
  - *When:* A `Reviewer` conducts compliance verification.
  - *Then:* The submission is unconditionally rejected and denied `official` or `verified` status.

#### `TP-BR-0303` — Authorized OAuth Legitimacy
- **Owner:** `Organization Owner` & `Reviewer` | **Impact:** `High` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-004`)
- **Rule:** Connections using OAuth or federated identity on official channels must use a legitimate `client_id` (registered by the self-hosting organization or publicly authorized by the vendor) alongside documented `scopes` matching their stated purpose; covertly embedding client credentials reverse-engineered from proprietary third-party applications is strictly prohibited.
- **Exception:** Fully self-owned internal enterprise gateways where the self-hosting enterprise legally owns and operates both ends of the authentication boundary.
- **Preliminary Testable Criteria:**
  - *Given:* An `Authorized OAuth` connection configured on an `Official Adapter`.
  - *When:* The system initiates authorization against the `External IdP`.
  - *Then:* The request uses transparently configured client parameters and verifies granted scopes before transitioning the connection to `ACTIVE`.

#### `TP-BR-0304` — Expired Credential Containment
- **Owner:** `Operator` & `Team Admin` | **Impact:** `High` | **Source / Status:** `PROPOSED` (From `WF-05`, `SM-02`, `SM-03`)
- **Rule:** Whenever a `Provider Connection` credential expires without automatic renewal or is revoked upstream, the system must: (1) immediately transition the connection to re-authentication-required state to stop futile upstream calls that trigger account lockouts, (2) execute at most one single-flight refresh per connection at any instant, and (3) return a sanitized, canonical error code to the `API Client` without ever echoing raw upstream tokens or secret headers.
- **Exception:** Zero exceptions exist for secret scrubbing in error payloads.
- **Preliminary Testable Criteria:**
  - *Given:* `Provider Connection C1` holds an upstream-revoked OAuth refresh token while receiving 50 concurrent requests.
  - *When:* The access token hits its expiration window.
  - *Then:* Exactly one refresh request is dispatched; upon receiving upstream rejection, `C1` immediately transitions to `AUTH_EXPIRED_REAUTH_REQUIRED`, emits an operator alert, and leaks zero raw token or secret strings to any `API Client` response.

---

### Group 04: Permission Boundaries & Untrusted Plugin Containment

#### `TP-BR-0401` — Default-Deny Plugin Permissions
- **Owner:** `Operator` & `Security Reviewer` | **Impact:** `Critical` | **Source / Status:** `CONFIRMED_BY_OWNER` (`DEC-002`, `C-02`)
- **Rule:** At runtime, an extension holds zero outbound network access, zero host filesystem access, zero host environment variable access, and zero access to credentials of other connections beyond the exact capabilities declared in its `Extension Manifest` and approved by an administrator for that invocation.
- **Exception:** Zero exceptions.
- **Preliminary Testable Criteria:**
  - *Given:* `Plugin P` is approved strictly for outbound calls to `api.vendor-a.example`.
  - *When:* During request handling, `Plugin P` attempts to connect to `collector.unknown.example` or read configuration belonging to `Connection Vendor B`.
  - *Then:* The attempt is blocked prior to network egress and generates a security violation audit event.

#### `TP-BR-0402` — Zero Unnecessary Raw Secret Exposure
- **Owner:** `Operator` | **Impact:** `Critical` | **Source / Status:** `PROPOSED` (`IDEA-01`, `C-02`)
- **Rule:** For standard authentication schemes (header or query-parameter bearer/API-key injection), the host gateway must inject the secret directly at egress toward approved target hosts without exposing raw secret strings to third-party extension memory. Direct secret access inside an extension may be requested only when custom dynamic request signing requires it, and must surface a high-risk warning during approval.
- **Exception:** Extensions implementing custom dynamic signing schemes whose per-connection secret access has been explicitly approved by an administrator.
- **Preliminary Testable Criteria:**
  - *Given:* A `Community Adapter` using standard bearer/header API key authentication.
  - *When:* Routing an inference call through that adapter and inspecting the complete input memory passed to the extension.
  - *Then:* The extension never receives the plaintext API key value, while the approved upstream host receives the valid authentication header.

#### `TP-BR-0403` — Permission Escalation Block on Update
- **Owner:** `Organization Owner` & `Operator` | **Impact:** `Critical` | **Source / Status:** `PROPOSED` (`WF-11`, `SM-01`)
- **Rule:** Every extension update workflow (manual or scheduled) must diff the target version's requested permissions against the currently approved permission set. If the new version introduces any new permission or broadened scope, it must never auto-activate for live traffic and must halt in `UPDATING_PENDING_RECONSENT` awaiting explicit re-approval.
- **Exception:** Updates whose requested permissions are a strict subset of or equal to previously approved permissions and remain contract-compatible may cut over normally.
- **Preliminary Testable Criteria:**
  - *Given:* `Plugin P@1.0.0` runs with approved access to `a.example.com`; `P@1.1.0` adds `b.example.com` to its manifest.
  - *When:* An unattended update runs without new permission consent.
  - *Then:* `1.1.0` is held in `UPDATING_PENDING_RECONSENT`, live traffic continues uninterrupted through `1.0.0`, and administrators are notified to review the permission diff.

#### `TP-BR-0404` — Plugin Fault Isolation & Quarantine
- **Owner:** `Operator` | **Impact:** `High` | **Source / Status:** `CONFIRMED_BY_OWNER` (`C-02`) & `PROPOSED`
- **Rule:** Hangs, infinite loops, resource exhaustion, or unhandled exceptions inside an extension may fail only the individual request invoking that extension and must never crash the Tanpopo host process or degrade unrelated `Provider Connection` traffic. When an administrator triggers `Quarantine`, all traffic through that extension must cease immediately system-wide.
- **Exception:** Zero exceptions.
- **Preliminary Testable Criteria:**
  - *Given:* `Adapter X` enters an infinite loop while `Adapter Y` operates normally.
  - *When:* Concurrent requests are sent to both `Adapter X` and `Adapter Y`.
  - *Then:* Requests to `Adapter X` terminate cleanly at the execution quota with an isolated error, while requests to `Adapter Y` succeed at unaffected latency.

---

### Group 05: Routing, Residency, Streaming & Fail Posture

#### `TP-BR-0501` — Hard Data Residency Enforcement
- **Owner:** `Organization Owner` | **Impact:** `Critical` (Data Compliance) | **Source / Status:** `PROPOSED` (`UC-05`, `WF-07`)
- **Rule:** Whenever an `Organization`, `Project`, `Route`, or per-request policy constrains geographic data processing regions (e.g., `EU` only or `On-Premise` only), the router must exclude every non-compliant `Model Deployment` during both primary selection and all `Failover` attempts.
- **Exception:** Data residency boundaries must never be bypassed to preserve technical availability.
- **Preliminary Testable Criteria:**
  - *Given:* `Route R1` enforces `Data Region = EU` across `Deployment D_EU` (currently failing `503`) and `Deployment D_US` (healthy).
  - *When:* A request arrives at `Route R1`.
  - *Then:* The router refuses to fail over to `D_US` and returns `503 no_compliant_deployment_available`.

#### `TP-BR-0502` — Failure Posture Matrix (`Fail-Closed` vs `Fail-Open`)
- **Owner:** `Organization Owner` & `Product Architect` | **Impact:** `Critical` | **Source / Status:** `PROPOSED`
- **Rule:**
  1. **Unconditionally `Fail-Closed` (non-overridable):** Identity/virtual-key authentication, cross-tenant isolation, Plugin permission boundaries, data residency enforcement, `destructive` tool approval gates, and `Hard Budget Cap` enforcement.
  2. **Configurable `Fail-Open` with audited warning counters (at `Organization Owner` discretion):** Non-critical auxiliary metadata enrichment or secondary external telemetry export (if an external log collector is unreachable, primary inference traffic continues while incrementing dropped-batch counters).
- **Exception:** Enabling `Fail-Open` on any category-(1) control is strictly prohibited.
- **Preliminary Testable Criteria:**
  - *Given:* The hard-budget evaluator or key authentication subsystem cannot verify authorization for an incoming request.
  - *When:* The request reaches the gateway.
  - *Then:* The request is safely rejected (`Fail-Closed`) and never forwarded upstream.

#### `TP-BR-0503` — No Silent Mid-Stream Fallback
- **Owner:** `Product Architect` | **Impact:** `High` (Downstream Application Data Integrity) | **Source / Status:** `PROPOSED` (`UC-01`, `WF-06`, `WF-07`)
- **Rule:** Automatic failover to a backup `Model Deployment` is permitted **strictly before** the first content chunk or tool-call delta is emitted to the `API Client`. Once at least one content chunk has been streamed (`Partial Stream`), any upstream error or disconnect must terminate the stream with an explicit error event frame rather than silently invoking a second model and concatenating divergent outputs into the same stream.
- **Exception:** Empty protocol keep-alive heartbeats carrying zero semantic payload bytes may be configured not to count as content emission where explicitly permitted by the wire protocol.
- **Preliminary Testable Criteria:**
  - *Given:* A `Route` configures `D1` (primary) and `D2` (fallback); `D1` returns `200 OK`, streams 20 words, and drops the connection.
  - *When:* The `API Client` is consuming the response stream.
  - *Then:* `D2` is never invoked; the `API Client` receives a deterministic mid-stream abort error frame, and tokens already emitted by `D1` enter estimated settlement.

#### `TP-BR-0504` — Strict Capability Verification
- **Owner:** `Product Architect` | **Impact:** `Medium-High` | **Source / Status:** `PROPOSED` (`UC-01`)
- **Rule:** When a caller request requires core capabilities that materially affect output correctness (e.g., strict `json_schema`, tool calling, image/audio inputs), the gateway must never silently strip those parameters to force-fit an incapable model; it must filter for a capable deployment or fail fast with an explicit error code.
- **Exception:** Administrators may explicitly opt into equivalent parameter adaptation on a `Route` solely for non-semantic advisory hints.
- **Preliminary Testable Criteria:**
  - *Given:* A route contains only text-only models (`vision = false`).
  - *When:* An `API Client` submits a request containing image inputs.
  - *Then:* The gateway immediately returns `400 capability_not_supported` without incurring upstream provider calls.

#### `TP-BR-0505` — Safe Retry & Cancellation Propagation
- **Owner:** `Product Architect` | **Impact:** `High` | **Source / Status:** `PROPOSED`
- **Rule:** Automatic retries apply exclusively to transient pre-stream failures (connect errors, `429`/`503` prior to content emission) bounded by the route's retry ceiling. Whenever an `API Client` cancels an active request, the gateway must immediately propagate cancellation to the upstream provider connection to halt further token spend.
- **Exception:** Client-side input/auth errors (`400`, `401`, `403`, `422`) are never retried automatically.
- **Preliminary Testable Criteria:**
  - *Given:* A long-form streaming generation request is active at the 3-second mark.
  - *When:* The `API Client` closes its connection to cancel the request.
  - *Then:* The upstream provider connection is aborted immediately (rather than generating for 60 more seconds) and the budget hold settles promptly.

---

### Group 06: MCP Tool Governance, Approvals & Inter-Agent A2A (`MCP, Approvals & A2A`)

#### `TP-BR-0601` — MCP Tool Scoping & Schema Pinning
- **Owner:** `Organization Owner` & `Team Admin` | **Impact:** `Critical` | **Source / Status:** `PROPOSED` (`UC-06`, `IDEA-03`)
- **Rule:** An `MCP Client` may discover and invoke strictly the subset of tools authorized for its `Virtual Key` / principal. Every tool enabled in enforced environments must pin a cryptographic fingerprint of its name, description, and parameter schema (`schema_hash`) at approval time; if the downstream `MCP Server` alters descriptions or parameters causing `schema_hash` drift, the tool must be automatically suspended until re-approved by an administrator.
- **Exception:** Local `development` environments may opt into warning-only mode while engineers actively iterate on their own tool definitions.
- **Preliminary Testable Criteria:**
  - *Given:* Tool `read_customer` is approved with fingerprint `H1` in `production`.
  - *When:* The `MCP Server` modifies `read_customer`'s text description (changing its fingerprint to `H2`) without administrative re-approval.
  - *Then:* The gateway suspends `read_customer` invocations and emits a tool schema drift security alert.

#### `TP-BR-0602` — Destructive Tool Approval Binding
- **Owner:** `Organization Owner` & `Reviewer` | **Impact:** `Critical` | **Source / Status:** `PROPOSED` (`WF-09`, `SM-06`)
- **Rule:** Every invocation of an MCP tool classified as `destructive` (or matching high-risk project policy) must generate an `Approval Request` and execute only after an authorized `Reviewer` approves it within its TTL. Approval is cryptographically bound to the exact tool name, calling principal, and canonicalized input arguments; any argument mutation invalidates the approval.
- **Exception:** Self-approval by the initiating agent or principal is strictly forbidden.
- **Preliminary Testable Criteria:**
  - *Given:* A `Reviewer` approves `delete_record(id="123")`.
  - *When:* An agent attempts to reuse that approval token for `delete_record(id="456")` or after the approval TTL has expired.
  - *Then:* Execution is denied and an audit violation event is recorded.

#### `TP-BR-0603` — No Blind Retry on Side-Effecting Tools
- **Owner:** `Product Architect` | **Impact:** `High` | **Source / Status:** `PROPOSED` (`WF-09`)
- **Rule:** For `mutating` or `destructive` MCP tools (and A2A tasks not declared idempotent), if a network disconnect or timeout occurs after the call has been dispatched downstream, the gateway **must never** automatically re-send the command—preventing duplicate real-world side effects—and must instead return an explicit `ambiguous_outcome` status with a correlation trace ID.
- **Exception:** Only tools explicitly verified as side-effect-free `read-only` operations may apply transient network retry policies.
- **Preliminary Testable Criteria:**
  - *Given:* Tool `send_invoice_email` is classified as `mutating`.
  - *When:* The `MCP Server` fails to respond before timeout after receiving the dispatched request.
  - *Then:* The gateway dispatches the call exactly once, performs zero retries, and returns an ambiguous-outcome error requiring reconciliation.

#### `TP-BR-0604` — No A2A Privilege Amplification
- **Owner:** `Organization Owner` | **Impact:** `High` | **Source / Status:** `PROPOSED`
- **Rule:** When an `Agent` delegates a child task to another `Agent` across the A2A plane, the child task's data-residency scope, maximum TTL, and budget ceiling must never exceed the entitlements and remaining available budget of the originating parent task.
- **Exception:** Zero exceptions.
- **Preliminary Testable Criteria:**
  - *Given:* A parent agent has `2 USD` remaining budget and data access restricted to `EU`.
  - *When:* The parent agent initiates a child A2A task requesting a `10 USD` ceiling or targeting a non-`EU` agent endpoint.
  - *Then:* The A2A plane rejects the privilege-amplifying delegation immediately.

---

### Group 07: Usage Reporting, Budgets & Chargeback (`Usage, Budgets & Chargeback`)

#### `TP-BR-0701` — Hierarchical Budget Ceiling
- **Owner:** `Organization Owner` & `Team Admin` | **Impact:** `High` | **Source / Status:** `PROPOSED` (`UC-08`, `WF-08`)
- **Rule:** A billable request is admitted only when sufficient available budget exists simultaneously across every applicable level in its hierarchy (`Virtual Key` → `Environment` → `Project` → `Organization`). Even if a `Virtual Key` retains unused individual quota, hitting the parent `Project` or `Organization` hard ceiling must block the request.
- **Exception:** Self-hosted internal models explicitly configured with `0` unit price (or designated as zero-cash fallback targets under emergency downgrade policy).
- **Preliminary Testable Criteria:**
  - *Given:* `Virtual Key K1` has a `50 USD` ceiling (`10 USD` spent), while parent `Project P1` has a `100 USD` ceiling already `100 USD` consumed across sibling keys.
  - *When:* `K1` submits a new request to a paid model.
  - *Then:* The request is rejected with `429 project_budget_exceeded`.

#### `TP-BR-0702` — Mandatory Pre-Execution Budget Hold
- **Owner:** `Organization Owner` | **Impact:** `High` | **Source / Status:** `PROPOSED` (`UC-08`, `SM-07`)
- **Rule:** For every scope governed by a hard budget ceiling, the gateway must deduct a temporary estimated hold from available balance **prior to** forwarding the request upstream, preventing concurrent request bursts from racing past a nearly exhausted ceiling.
- **Exception:** Requests rejected at pre-egress auth/policy gates must immediately release any provisional hold (`RELEASED_UNUSED`).
- **Preliminary Testable Criteria:**
  - *Given:* Hard project budget has exactly `0.10 USD` remaining; each request requires a `0.05 USD` estimated hold.
  - *When:* 20 concurrent requests arrive simultaneously.
  - *Then:* At most 2 requests acquire holds and proceed upstream; the remaining 18 are rejected immediately prior to egress.

#### `TP-BR-0703` — Explicit Usage Accuracy Classification
- **Owner:** `Organization Owner` | **Impact:** `High` (Financial Integrity) | **Source / Status:** `PROPOSED` (`WF-08`)
- **Rule:** Every usage record and ledger entry must carry an explicit measurement accuracy tag:
  - `EXACT`: Backed by authoritative token/usage counters from the upstream provider wire protocol.
  - `ESTIMATED`: Computed via Tanpopo's deterministic estimator when a stream aborts mid-flight or the upstream omits usage counters.
  - `UNMETERED_FLAGGED`: Usage quantity cannot be automatically estimated or lacks pricing metadata; flagged for manual reconciliation.
  Financial and `Chargeback` reports **must never** silently record mid-stream aborts or missing-usage calls as `0 USD` exact spend without segregating estimated/unmetered columns.
- **Exception:** Zero exceptions.
- **Preliminary Testable Criteria:**
  - *Given:* A streaming response emits 500 words before losing upstream connectivity prior to the final token-summary frame.
  - *When:* The gateway closes the stream and exports the project's cost report.
  - *Then:* The transaction settles with `ESTIMATED` spend `> 0` based on streamed bytes/tokens and appears explicitly under the estimated cost column.

---

### Group 08: Concurrency, Retention Privacy & Audit Trail (`Concurrency, Retention & Audit`)

#### `TP-BR-0801` — Optimistic Configuration Concurrency
- **Owner:** `Operator` & `Team Admin` | **Impact:** `Medium-High` | **Source / Status:** `PROPOSED` (`WF-11`)
- **Rule:** When multiple administrators or automation pipelines concurrently update a sensitive control-plane entity (`Route`, `Policy`, `Budget`, `Extension Installation`), the system accepts updates only when matching the latest persisted version and rejects stale writes, preventing accidental clobbering of newly added security policies.
- **Exception:** Emergency containment actions (`Revoke` / `Quarantine` / `Suspend`) preemptively execute immediately to protect system safety.
- **Preliminary Testable Criteria:**
  - *Given:* Administrators A and B both load `Policy P` at version `v3`; A saves an update advancing `P` to `v4`.
  - *When:* B attempts to save changes still referencing base version `v3`.
  - *Then:* The system rejects B's write with a version conflict error prompting B to review `v4` before retrying.

#### `TP-BR-0802` — Privacy-by-Default & Clean Deletion
- **Owner:** `Organization Owner` | **Impact:** `High` | **Source / Status:** `PROPOSED` (`WF-13`)
- **Rule:** Under default configuration, the gateway never persists full-text end-user prompts or completions in durable storage (retaining only operational metadata and cost metrics) unless an administrator explicitly opts into payload logging with a bounded automatic expiration `TTL`. Upon deleting a `Project` or `Organization`, all secret credentials and persisted query payloads belonging to that scope must be irrecoverably purged once active streams finish draining.
- **Exception:** Aggregated financial ledger entries and security audit records containing zero sensitive prompt payloads are retained for the organization's statutory audit window.
- **Preliminary Testable Criteria:**
  - *Given:* A newly created `Project` using default settings receives a request containing sensitive prompt text.
  - *When:* Inspecting durable log storage.
  - *Then:* Only operational metadata (timestamp, model, latency, token counts, cost, status code) is present, with zero raw prompt or completion text stored.

#### `TP-BR-0803` — Immutable Security & Governance Audit
- **Owner:** `Organization Owner` & `Operator` | **Impact:** `Critical` | **Source / Status:** `PROPOSED`
- **Rule:** Every key lifecycle action (create/update/rotate/revoke), plugin lifecycle action (install/approve/upgrade/quarantine), policy or budget mutation, sensitive tool approval/rejection, and blocked security violation (cross-tenant probe, unauthorized plugin egress, tool schema hash drift) must emit an immutable `Audit Event` record (no administrator may edit or selectively delete individual audit rows prior to retention expiry).
- **Exception:** Zero exceptions.
- **Preliminary Testable Criteria:**
  - *Given:* A `Team Admin` revokes a `Virtual Key` or an `Operator` installs an `unverified-external` plugin.
  - *When:* Querying the audit log and attempting to submit an API call to modify or delete that audit entry.
  - *Then:* The record captures the acting principal, timestamp, target entity, and provenance tier, and the system rejects all attempts to mutate or delete individual audit entries.
