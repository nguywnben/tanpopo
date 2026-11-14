# Comprehensive Domain Feature Catalog — Tanpopo

> **Product Definition Document (Phase ASTRA A03 — Gate G2: `READY_FOR_REVIEW` | `DRAFT — UNLOCKED SCOPE`)**
> - **Version:** `0.1.0-step03-draft`
> - **Last Updated:** `2026-11-11`
> - **Authored By:** Principal Requirements Engineer & Product Manager
> - **Governance Status:** **Unlocked-scope draft (`DRAFT`)**. Every capability domain below specifies all 14 verifiable engineering attributes and explicitly distinguishes behavior natively supported, partially supported, or unsupported by upstream target providers (`Upstream Provider`).

---

## Domain 01: Gateway, Protocols & Multimodality (`gateway-protocols`)

### `FT-01` — Unified Inference & Multimodal Streaming
- **Actors:** `API Client`, `Developer`, `Agent`.
- **Preconditions:** At least one `Model Deployment` in `HEALTHY` state is mapped via a `Route`; caller presents a `Virtual Key` in `CREATED_ACTIVE` or `ROTATING_DUAL_VALID` state.
- **Trigger:** HTTP/HTTPS request sent to normalized ingress paths (`/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/models`).
- **Inputs:** `Virtual Key` auth header, target model/route alias, message array (text, image URL/base64, audio/document attachments), `tools` schema, output constraint (`response_format`/`json_schema`), `stream` flag, optional `Idempotency-Key`.
- **Outputs:** Synchronous JSON payload or normalized `text/event-stream` (SSE) stream carrying `x-tanpopo-request-id`, token usage metrics (when available), and a canonical structured error envelope on failure.
- **States:** References `SM-04` (Deployment Health) & `SM-07` (Budget Reservation/Settlement).
- **Business Rules:** `TP-BR-0101`, `TP-BR-0103`, `TP-BR-0503` (forbid fallback after partial stream), `TP-BR-0504` (capability pre-check), `TP-BR-0505` (upstream stream cancellation).
- **Permissions:** `inference:invoke` scoped on the `Virtual Key` for the target `Environment` and permitted model/route allowlist.
- **Success Flow:** Authenticate key → Evaluate Policy & Model Capabilities → Place temporary budget hold (`RESERVED_HOLD`) → Translate via `Provider Adapter` → Stream low-latency chunks → Settle ledger (`SETTLED_EXACT`) → Emit trace metadata.
- **Error & Negative Flows:**
  - Request demands capabilities unsupported by target deployment (e.g., audio input or strict `json_schema` sent to a text-only model) → Immediately reject with `400 capability_not_supported` prior to outbound network egress.
  - Upstream provider disconnects after flushing `>= 1` content chunk → Emit terminal SSE error frame `mid_stream_abort`, settle as `SETTLED_ESTIMATED`, and **never** silently replay or splice a fallback model mid-stream.
  - Caller aborts connection mid-flight → Cancel upstream provider request within `< 500 ms` and release unused budget hold.
- **Limits:** Default maximum request body size `32 MB` (configurable per route for multimodal inputs); bounded parallel tool-call chunks and configurable `Max Request Duration` (default `300 seconds` for deep reasoning models).
- **Expected UX:** `API Client` switches only `base_url` and `api_key` inside standard OpenAI SDKs without installing proprietary client libraries; response headers optionally disclose the actual serving `Model Deployment` (when permitted by project policy).
- **Compatibility & Upstream Provider Behavior Distinction:**
  - *Equivalent Standard Support:* Text generation, SSE streaming, baseline `function calling`, `temperature`/`max_tokens`.
  - *Cross-Vendor Divergence (Adapter must declare explicitly in `Capability Profile`):* Internal `reasoning`/`thinking` blocks (some vendors emit plaintext reasoning, others return encrypted summaries or token counts only), maximum image/PDF payload bounds, strict `json_schema` validation vs. best-effort prompting.
  - *Unsupported Over Standard Chat REST:* Ultra-low-latency bidirectional voice/video streaming over WebSocket/WebRTC (`Realtime Media`) — explicitly separated into post-`v1.0` roadmap scope (`TP-FR-0106` — `Later`), never falsely promised over `/v1/chat/completions`.
- **SRS & AC:** `TP-FR-0101`..`0104`, `TP-FR-0106` (`Later`), `TP-AC-F0101`..`F0104`.

### `FT-02` — Lossless Native Dialect Passthrough
- **Actors:** `API Client` (e.g., specialized coding harnesses calling native Anthropic `/v1/messages` or native Gemini endpoints directly), `Developer`.
- **Preconditions:** A `Provider Connection` matching that native protocol family is active and authorized on the caller's `Virtual Key`.
- **Trigger:** Request sent to an explicit native-dialect ingress prefix (e.g., `/native/anthropic/v1/messages`, `/native/gemini/v1beta/...`).
- **Inputs:** Unmodified vendor-native request body and feature headers (e.g., multi-tier `cache_control` breakpoints, granular `thinking` blocks, native document blocks) paired with a Tanpopo `Virtual Key`.
- **Outputs:** Unmodified upstream response payload / SSE event stream (stripped of sensitive internal infrastructure headers).
- **States:** `SM-04`, `SM-07`.
- **Business Rules:** `TP-BR-0101`, `TP-BR-0304`, `TP-BR-0501`, `TP-BR-0503`, `TP-BR-0702`.
- **Permissions:** `inference:invoke:native` on the corresponding scope.
- **Success Flow:** Authenticate `Virtual Key`, strip virtual key from outbound headers, enforce budget & data-residency policies, inject real `Provider Connection` credentials at network egress, forward payload intact without intermediate schema loss, and extract token/cache metrics from the native response for settlement.
- **Error & Negative Flows:** Permit pre-stream failover strictly across `Model Deployment` targets **sharing the exact same native dialect** (e.g., direct Anthropic to cloud-hosted Anthropic running the identical schema version); reject with `400 dialect_mismatch` if a route misconfigures cross-dialect targets.
- **Limits:** Zero mutation of newly introduced vendor extension fields launched same-day.
- **Expected UX:** Specialized agent harnesses retain `100%` of bleeding-edge vendor capabilities while the organization preserves centralized credential vaulting, budgets, and auditability.
- **Compatibility:** Applicable exclusively to `Provider Adapter` packages declaring support for the corresponding native dialect.
- **SRS & AC:** `TP-FR-0105`, `TP-AC-F0105`.

---

## Domain 02: Providers, Models, Deployments & Credentials (`provider-credentials`)

### `FT-03` — Four-Entity Model Chain Management (`Adapter → Connection → Logical Model → Deployment`)
- **Actors:** `Organization Owner`, `Team Admin`, `Operator`.
- **Preconditions:** At least one valid `Provider Adapter` is enabled.
- **Trigger:** Create or update a `Provider Connection` and enable `Model Deployment` targets via Web Console, CLI, or declarative config.
- **Inputs:** `Provider Adapter` ID, `Secret Credential` binding reference, custom base endpoint (for self-hosted engines or sovereign clouds), `Data Region`, list of `Model Deployment` targets with optional pricing overrides and custom timeouts (e.g., extended `first_byte_timeout` for cold-loading `Ollama`/`vLLM` models).
- **Outputs:** Operational `Provider Connection` and `Model Deployment` records ready for routing alongside live health state.
- **States:** `SM-02` (Provider Connection) & `SM-04` (Model Deployment Health).
- **Business Rules:** `TP-BR-0102`, `TP-BR-0104`, `TP-BR-0501`.
- **Permissions:** `connection:manage` (`Organization` or `Project` scope); `connection:share` (`Organization Owner` only).
- **Success Flow:** Persist configuration → Execute live credential verification probe → Register `Model Deployment` entries bound to explicit data regions and price schedules → Transition to `ACTIVE` / `HEALTHY`.
- **Error & Negative Flows:** Block hard deletion of any `Provider Connection` still referenced by an active `Route`; distinguish slow local GPU model loading within declared `cold_start_timeout` from genuine network outages.
- **Limits:** Supports multiple independent `Provider Connection` instances backed by a single `Provider Adapter` (e.g., 5 separate commercial billing accounts for 5 business units).
- **Expected UX:** Crisp UI mental model: install an *Adapter* once, bind multiple account *Connections*, and route traffic to concrete *Deployments*.
- **Compatibility:** Uniformly supports cloud commercial APIs and self-hosted inference servers (`vLLM`, `SGLang`, `Ollama`, `llama.cpp`).
- **SRS & AC:** `TP-FR-0201`..`0203`, `TP-AC-F0201`..`F0202`.

### `FT-04` — Write-Only Secret Vault, Authorized OAuth 2.1 & Federated Identity (`Secret Vault & Authorized OAuth`)
- **Actors:** `Organization Owner`, `Team Admin`, `Operator`, `External IdP`.
- **Preconditions:** System root secret vault key has been initialized (`WF-01`).
- **Trigger:** Import static credential, initiate authorized OAuth 2.1 consent flow, bind cloud identity (IAM/OIDC), or rotate secrets.
- **Inputs:** Secret material (write-only) or compliant OAuth configuration (`authorization_endpoint`, `token_endpoint`, legitimately registered `client_id`, provider-authorized `scopes`, PKCE).
- **Outputs:** Encrypted `Secret Credential` record (exposing only last-4 hint or opaque fingerprint), OAuth session state (`TOKEN_VALID`), and automated `single-flight` token refresh coordination.
- **States:** `SM-02`, `SM-03`.
- **Business Rules:** `TP-BR-0102`, `TP-BR-0301`..`0304`, `TP-BR-0803`.
- **Permissions:** `secret:write`, `secret:bind`, `secret:revoke` (no `secret:read_plaintext` permission exists for any user across UI or API).
- **Success Flow:** Encrypt immediately on ingest → Validate live entitlement → Ahead of expiry, elect a single leader goroutine/task to refresh tokens → Swap access tokens atomically without dropping in-flight traffic.
- **Error & Negative Flows:**
  - Upstream returns `invalid_grant` or `401` auth failure → Transition immediately to `AUTH_EXPIRED_REAUTH_REQUIRED`, dispatch admin alert, and scrub all upstream credential details from `API Client` error responses.
  - Any attempt to repurpose personal consumer subscription browser sessions is unsupported across official packages (`TP-BR-0302`).
- **Limits:** Single-flight refresh lock timeout capped at `10 seconds`; zero secret plaintext emitted in logs at any debug verbosity level.
- **Expected UX:** Proactive expiry warnings on time-bounded credentials; one-click OAuth re-consent workflow upon upstream revocation.
- **Compatibility:** Supports static keys (API Key/Bearer), OAuth 2.1 (Authorization Code + PKCE, Client Credentials), mTLS, Cloud Workload Identity/IAM, and custom enterprise auth primitives via SDK. **Technical OAuth support never implies that arbitrary end-user consumer OAuth accounts of AI vendors may be repurposed as shared API gateways.**
- **SRS & AC:** `TP-FR-0204`..`0206`, `TP-AC-F0203`, `TP-AC-SEC-01`, `TP-AC-SEC-03`, `TP-AC-SEC-06`.

---

## Domain 03: Extension Engine & Marketplace (`plugin-marketplace`)

### `FT-05` — Two-Tier Isolated Plugin Engine & Blind Credential Broker (`Isolated Plugin Engine & Blind Credential Broker`)
- **Actors:** `Operator`, `Extension Publisher` (indirectly via packaged artifact).
- **Preconditions:** Extension passed `Manifest` verification and received explicit permission approval (`SM-01`: `ENABLED_ACTIVE`).
- **Trigger:** Runtime traffic traverses an extension-backed `Provider Adapter` or custom policy/auth filter.
- **Inputs:** Request context normalized to the target `Plugin Contract` version alongside non-secret connection configuration.
- **Outputs:** Transformed upstream/downstream frames or policy verdicts; real secret material injected blindly at the Host egress boundary.
- **States:** `SM-01`.
- **Business Rules:** `TP-BR-0401`, `TP-BR-0402`, `TP-BR-0404`.
- **Permissions:** Enforces the immutable `Approved Permission Grant` snapshot (outbound HTTPS strictly limited to declared `allowed_hosts`, zero host filesystem access, zero host environment variable access).
- **Success Flow:**
  - *Tier 1 (Declarative Pack):* Executes header, path, model alias, pricing, and SSE frame mappings natively in Host memory with zero arbitrary code execution.
  - *Tier 2 (Sandbox Plugin):* Executes custom transformation logic inside a memory- and CPU-bounded capability sandbox; when dispatching an outbound request to a verified `allowed_hosts` domain, the Host injects the real credential at egress without ever exposing raw secret bytes inside plugin memory.
- **Error & Negative Flows:**
  - Plugin attempts egress to an undeclared domain or forbidden internal address (SSRF to `127.0.0.1`, `169.254.169.254`, etc.) → Block instantaneously, abort request, record critical `AuditEvent`, and transition to `QUARANTINED` per policy.
  - Plugin hits infinite loop or memory ceiling → Terminate that isolated execution instance cleanly without impacting concurrent Gateway streams.
- **Limits:** Strict per-sandbox linear memory ceiling and per-event execution fuel/time budget (`< 50 ms` for standard header/JSON frame transforms).
- **Expected UX:** Complete Console/CLI transparency distinguishing code-free Declarative Packs from Sandboxed Code Plugins alongside the exact list of permitted destination hosts.
- **Compatibility:** Explicit `contract_version` negotiation; automated compatibility verification at load time and during Core upgrade preflight (`SM-08`).
- **SRS & AC:** `TP-FR-0301`..`0303`, `TP-NFR-0401`..`0403`, `TP-AC-F0301`, `TP-AC-SEC-02`.

### `FT-06` — Multi-Source Installer, Registry, Lockfile & Contract Harness (`Multi-Source Installer, Registry & Contract Harness`)
- **Actors:** `Operator`, `Organization Owner`, `Reviewer`, `Extension Publisher`.
- **Preconditions:** Reachable distribution source (or valid offline bundle archive).
- **Trigger:** Fresh install, update check, `Rollback`, or author CLI test execution (`plugin test`).
- **Inputs:** Source URI (public Marketplace index, Git repo with tag/commit, URL/ZIP archive, internal Private Registry), cryptographic signature / SHA-256 digest.
- **Outputs:** Installed extension pinned in `tanpopo.lock`, honest `Trust Tier` badge (`official` | `verified` | `unverified-external`), or deterministic contract conformance report replayed against recorded `Cassettes`.
- **States:** `SM-01`.
- **Business Rules:** `TP-BR-0201`..`0203`, `TP-BR-0302`, `TP-BR-0403`, `TP-BR-0801`.
- **Permissions:** `extension:install`, `extension:approve_permissions`, `extension:quarantine`.
- **Success Flow:** Fetch artifact → Verify SHA-256 digest & signature → Assign truthful `Trust Tier` → Render permissions diff → Record admin approval → Hot-activate with zero Gateway downtime.
- **Error & Negative Flows:**
  - Package update requests broader privileges (`Permission Escalation`) → Block auto-activation, hold in `UPDATING_PENDING_RECONSENT` awaiting explicit re-approval while live traffic continues uninterrupted on the prior version (`WF-11`).
  - Post-upgrade regression → `Rollback` reverts traffic to the prior lockfile-pinned version within seconds.
- **Limits:** Bounded archive extraction quotas defending against decompression bombs (*zip bomb*).
- **Expected UX:** External authors scaffold templates, record/replay contract cassettes, and package releases from one unified CLI; operators review crisp browser-style `Permissions Diff` prompts.
- **Compatibility:** Supports exporting all lockfile-pinned extensions into a self-contained `Offline Bundle` for air-gapped networks.
- **SRS & AC:** `TP-FR-0304`..`0307`, `TP-AC-F0302`..`F0304`.

---

## Domain 04: MCP Plane & Agent-to-Agent A2A Plane (`mcp-a2a`)

### `FT-07` — MCP Tool Governance, Schema Hash Pinning & Side-Effect Approvals (`MCP Governance Plane`)
- **Actors:** `MCP Client`, `Agent`, `Team Admin`, `Reviewer`, `MCP Server`.
- **Preconditions:** `MCP Server` registered with per-tool risk classifications (`read-only`, `mutating`, `destructive`) and admin-approved `schema_hash` digests.
- **Trigger:** `MCP Client` invokes discovery (`tools/list`, `resources/list`, `prompts/list`) or execution (`tools/call`) against Tanpopo's MCP Plane endpoint.
- **Inputs:** MCP protocol request (Streamable HTTP / SSE) authenticated via caller `Virtual Key`.
- **Outputs:** Permission-filtered tool catalog (supporting standards-compliant cursor pagination without arbitrary 100-tool truncation), tool execution output, or pending `ApprovalRequest` challenge.
- **States:** `SM-06` (Approval & Task Lifecycle).
- **Business Rules:** `TP-BR-0104`, `TP-BR-0601`..`0603`, `TP-BR-0803`.
- **Permissions:** `mcp:discover` and granular tool invocation grants `mcp:invoke:<server>:<tool>`.
- **Success Flow:** Filter catalog to authorized tools only (conserving model context window tokens) → Verify live `schema_hash` match → Broker upstream server credentials blindly → For `destructive` tools, verify/issue parameter-hash-bound `ApprovalRequest` → Execute at-most-once and record audit event.
- **Error & Negative Flows:**
  - Upstream `MCP Server` silently mutates tool description/schema (`schema_hash` mismatch) → Immediately block tool invocation and fire security alert (`MCP Rug-Pull Blocked`).
  - `mutating`/`destructive` tool call hits network timeout → **Strictly forbid automatic retry**, returning `ambiguous_side_effect_timeout`.
  - Caller tampers with approved tool arguments or replays an expired approval token → Reject with `403`.
- **Limits:** Configurable maximum MCP tool response payload ceiling preventing agent context-window exhaustion.
- **Expected UX:** IDE/CLI coding agents connect to a single Tanpopo MCP URL rather than distributing 10 raw internal service tokens onto individual developer laptops.
- **Compatibility:** Conforms to **MCP Streamable HTTP** (with backward-compatible SSE transport support).
- **SRS & AC:** `TP-FR-0401`..`0404`, `TP-AC-F0401`..`F0403`, `TP-AC-SEC-08`.

### `FT-08` — A2A Agent Connectivity & Bounded Delegation Plane (`A2A Connectivity & Bounded Delegation Plane`)
- **Actors:** `Agent` (caller & callee agents), `Team Admin`, `Organization Owner`.
- **Preconditions:** Target `Agent Endpoint` registered with a verified `Agent Card` capability profile and trust boundary.
- **Trigger:** `Agent Card` discovery or asynchronous task creation/status-update/cancellation on the A2A Plane.
- **Inputs:** A2A task request (JSON-RPC / streaming task status events), distributed correlation ID, delegated sub-budget ceiling, and task TTL.
- **Outputs:** Cross-agent task lifecycle transitions, task `Artifacts`, and rolled-up cost/audit records linked across the delegation chain.
- **States:** `SM-06`, `SM-07`.
- **Business Rules:** `TP-BR-0104`, `TP-BR-0501`, `TP-BR-0604` (anti-privilege-amplification).
- **Permissions:** `a2a:discover`, `a2a:delegate:<agent_id>`.
- **Success Flow:** Verify delegation ACL → Enforce monotonic scope narrowing so child-agent permissions and sub-budget never exceed the parent agent's remaining grant → Route asynchronous task messages with end-to-end `Correlation ID` propagation.
- **Error & Negative Flows:** Block cyclic multi-agent delegation loops (`Max Delegation Hop Depth`, default `<= 5` hops) and reject any scope-escalation attempt beyond the originating principal's grant.
- **Limits:** Bounded delegation hop depth and maximum orphaned asynchronous task TTL.
- **Expected UX:** Unified trace tree visualizing multi-agent task fan-out alongside cumulative rolled-up spend on a single screen.
- **Compatibility:** Aligned with the open **Agent2Agent (A2A) Protocol** specification.
- **SRS & AC:** `TP-FR-0405`..`0406`, `TP-AC-F0404`.

---

## Domain 05: Identity, Multi-Tenancy & Access Control (`iam-tenancy`)

### `FT-09` — Tenancy Hierarchy (`Org → Project → Env`), RBAC & Virtual Key Lifecycle (`Tenancy, RBAC & Virtual Keys`)
- **Actors:** `Personal Admin`, `Organization Owner`, `Team Admin`, `Developer`, `Operator`, `External IdP`.
- **Preconditions:** Initial root admin bootstrap completed.
- **Trigger:** Admin sign-in, member role assignment, `Project`/`Environment` creation, or `Virtual Key` issuance/rotation/revocation.
- **Inputs:** Principal identity (local account with memory-hard password hashing or enterprise OIDC SSO federation), RBAC role bindings, `Virtual Key` scope parameters (environment, allowed models/routes/MCP tools, RPM/TPM limits, budget cap, expiration).
- **Outputs:** Authenticated admin session or issued `Virtual Key` secret (displayed exactly once at creation; persisted solely as a one-way cryptographic hash).
- **States:** `SM-05` (Virtual Key Lifecycle).
- **Business Rules:** `TP-BR-0101`..`0104`, `TP-BR-0803`.
- **Permissions:** Strictly enforced per the 12-actor authorization matrix in [`business-entity-map.md`](../requirements/business-entity-map.md).
- **Success Flow:** Authenticate principal → Enforce `Organization`/`Project`/`Environment` isolation → Support zero-downtime dual-valid key rotation (`ROTATING_DUAL_VALID`) or instantaneous emergency revocation.
- **Error & Negative Flows:** Block any cross-organization access or cross-environment key misuse (e.g., `development` key calling `production` routes); cleanly separate graceful rotation windows from immediate active-stream termination on security compromise.
- **Limits:** Brute-force login rate limiting; mandatory one-way hashing of `Virtual Key` tokens (preventing plaintext key recovery even under full database compromise).
- **Expected UX:** Personal Mode streamlines multi-department UI chrome for solo operators while preserving the exact canonical schema for seamless future team expansion.
- **Compatibility:** Supports air-gapped local authentication as well as standard enterprise OIDC identity providers.
- **SRS & AC:** `TP-FR-0501`..`0504`, `TP-AC-F0501`..`F0503`, `TP-AC-SEC-07`.

---

## Domain 06: Capability-Aware Routing & Resilience (`routing-resilience`)

### `FT-10` — Capability-Aware Router, Pre-Stream Failover & Dry-Run Explain Simulator (`Capability Router & Explain Simulator`)
- **Actors:** `Team Admin`, `Organization Owner`, `Developer`, `API Client`.
- **Preconditions:** At least one `Route` configured with one or more candidate `Model Deployment` targets.
- **Trigger:** Live inference call targeting a `Route` or admin/developer dry-run diagnostic invocation (`/v1/route/explain` / CLI / Console).
- **Inputs:** Request requirements (required capabilities, estimated prompt context tokens, data-residency constraint, selection strategy: priority order, weighted distribution, or lowest-cost among compliant targets).
- **Outputs:** Selected compliant `Model Deployment` for live execution, or a deterministic per-candidate qualification/disqualification trace in dry-run explain mode.
- **States:** `SM-04`.
- **Business Rules:** `TP-BR-0501`..`0505`.
- **Permissions:** `route:manage` (configuration); `route:explain` (diagnostic simulation for `Developer` & `Team Admin`).
- **Success Flow:** Disqualify candidates violating `Data Residency` → Disqualify candidates missing required capabilities or context window capacity → Exclude circuit-open deployments (`CIRCUIT_OPEN_UNHEALTHY`) → Dispatch to top candidate; on network error/`429`/`5xx` **prior to flushing the first response chunk**, fail over to the next compliant target within milliseconds.
- **Error & Negative Flows:** Zero healthy targets satisfy data residency → Immediately fail closed with `503 no_compliant_deployment_available` (`Fail-Closed`); strictly forbid failover once a `Partial Stream` has begun.
- **Limits:** Maximum failover attempts per request capped (default `<= 3` targets) to prevent cascading retry storms.
- **Expected UX:** Dry-run simulator visually pinpoints why Deployment A won and Deployment B was disqualified without spending a cent of upstream API budget.
- **Compatibility:** Operates identically across cloud APIs and self-hosted local inference deployments.
- **SRS & AC:** `TP-FR-0601`..`0604`, `TP-AC-F0601`..`F0603`.

---

## Domain 07: Policy Engine & Operational Security (`policy-security`)

### `FT-11` — Runtime Policy Engine, Rate Limiting & Perimeter SSRF Defense (`Policy Engine & SSRF Defense`)
- **Actors:** `Organization Owner`, `Team Admin`, `Operator`.
- **Preconditions:** System-default and organization-level baseline policies loaded.
- **Trigger:** Every admin operation configuring external URLs and every runtime request traversing the 3 traffic planes (`Provider`, `MCP`, `A2A`).
- **Inputs:** Principal context, request metadata, resolved destination network address, live rate counters.
- **Outputs:** Deterministic `ALLOW`, `DENY` (with canonical reason code), or `REQUIRE_APPROVAL` verdict.
- **States:** `SM-06`.
- **Business Rules:** `TP-BR-0202`, `TP-BR-0501`, `TP-BR-0502`, `TP-BR-0601`..`0604`.
- **Permissions:** `policy:manage` (`Team Admin` may tighten project rules but can never relax upper-level `Organization Owner` guardrails).
- **Success Flow:** Enforce multi-dimensional rate limits (RPM, TPM, max concurrent open streams), model/tool allowlists, and data residency, while blocking private/link-local/cloud-metadata IP ranges (`169.254.169.254`, loopback) on all external URLs unless explicitly allowlisted by `Operator` for internal inference servers.
- **Error & Negative Flows:** Any evaluation failure across authentication, data residency, plugin permissions, or hard budget ceilings enforces strict `Fail-Closed` rejection.
- **Limits:** Sub-millisecond in-memory policy evaluation preserving overall gateway latency budgets.
- **Expected UX:** Organization-level policies act as clear "ceiling guardrails" empowering product teams to self-serve safely inside their projects.
- **Compatibility:** Supports attaching optional content-inspection filters (such as PII redaction) as sandboxed `Extension` modules.
- **SRS & AC:** `TP-FR-0701`..`0703`, `TP-NFR-0402`, `TP-AC-SEC-04`, `TP-AC-SEC-05`.

### `FT-12` — Immutable Audit Log & Emergency Kill-Switches (`Immutable Audit & Kill-Switches`)
- **Actors:** `Organization Owner`, `Operator`, `Reviewer`, `Team Admin`.
- **Preconditions:** Instance running.
- **Trigger:** Any mutation of permissions/keys/plugins/policies, security violation detection, or operator activation of an emergency kill-switch.
- **Inputs:** Administrative/security event context or kill-switch command at 4 granularities (`Virtual Key`, `Project`, `Provider Connection`, `Extension Quarantine`).
- **Outputs:** Append-only `AuditEvent` record and immediate data-plane enforcement.
- **States:** `SM-01`, `SM-02`, `SM-05`.
- **Business Rules:** `TP-BR-0404`, `TP-BR-0803`.
- **Permissions:** `audit:read`, `audit:export`, `emergency:killswitch`.
- **Success Flow:** Record actor principal, timestamp, before/after state diff, and provenance tier; on kill-switch activation, propagate revocation across all data-plane workers within committed SLO bounds.
- **Error & Negative Flows:** Reject with `403` any API call attempting in-place mutation or selective deletion of audit records.
- **Limits:** Supports continuous streaming export of audit events to external immutable WORM/SIEM storage.
- **Expected UX:** Instant blast-radius lookup view (*"Which projects and virtual keys traversed Plugin X over the past 7 days?"*).
- **Compatibility:** Structured JSON audit schema ready for enterprise SIEM ingestion.
- **SRS & AC:** `TP-FR-0704`..`0705`, `TP-NFR-0404`, `TP-AC-F0701`.

---

## Domain 08: Operational Telemetry, Budget Ledger & FinOps (`telemetry-finops`)

### `FT-13` — Two-Phase Budget Ledger & Honest Cost Chargeback (`Two-Phase Budget Ledger & Chargeback`)
- **Actors:** `Organization Owner`, `Team Admin`, `API Client`, `Agent`.
- **Preconditions:** Model pricing schedule configured (bundled via `Provider Adapter` or overridden by custom org tariff) alongside active budget caps.
- **Trigger:** Prior to every billable request (reservation phase) and immediately upon completion/cancellation/error (settlement phase).
- **Inputs:** Estimated worst-case input/output cost hold at start; actual upstream usage metrics (input, output, `cache_read`/`cache_write`, reasoning tokens) at completion.
- **Outputs:** `BudgetReservation` hold, settled ledger entry tagged with accounting precision (`EXACT`, `ESTIMATED`, `UNMETERED_FLAGGED`), soft-threshold alerts (webhook/UI), and multi-dimensional chargeback reports.
- **States:** `SM-07`.
- **Business Rules:** `TP-BR-0701`..`0703`.
- **Permissions:** `budget:manage`, `billing:report:read`.
- **Success Flow:** Atomically verify available balance across the entire hierarchy (`Key → Env → Project → Org`) → Lock pre-request hold → Settle actual spend upon completion and refund unused hold remainder; background reaper reclaims orphaned holds left behind by abrupt worker crashes.
- **Error & Negative Flows:**
  - Upstream omits token usage (or stream aborts mid-flight) → Compute deterministic fallback estimate tagged `ESTIMATED` (or `UNMETERED_FLAGGED` when pricing/tokenization is unavailable), **never falsely claiming 100% exactness or silently recording a fabricated `0 USD` charge**.
  - Final settled spend slightly exceeds initial hold → Record `Negative Balance Carryover` (pending `BA-OQ-002`) and block subsequent billable calls immediately.
- **Limits:** Concurrent burst overshoot strictly bounded within the margin of legitimately pre-reserved in-flight streams.
- **Expected UX:** Financial chargeback tables separate *"Verified Exact Spend (`Exact`)"* from *"Estimated / Pending Reconciliation Spend (`Estimated / Unmetered`)"*.
- **Compatibility:** Supports multi-component tariffs (text, image, audio, cache tiers, or `0 USD` / internal GPU amortization schedules for self-hosted engines).
- **SRS & AC:** `TP-FR-0801`..`0804`, `TP-AC-F0801`..`F0803`.

### `FT-14` — OpenTelemetry Exporter & Privacy-First Trace Explorer (`OTel & Privacy-First Telemetry`)
- **Actors:** `Developer`, `Team Admin`, `Operator`.
- **Preconditions:** Instance processing runtime traffic.
- **Trigger:** Request traverses `Provider`, `MCP`, or `A2A` planes.
- **Inputs:** Request lifecycle telemetry (end-to-end duration, `TTFT`, failover hops, status/error codes, route/tool identifiers).
- **Outputs:** Standards-compliant OTLP Metrics, Traces, and Logs exported to external observability backends alongside bounded local trace inspection in Web Console.
- **States:** —
- **Business Rules:** `TP-BR-0101`, `TP-BR-0802`.
- **Permissions:** `telemetry:read` (strictly scoped to caller's `Project`/`Organization`).
- **Success Flow:** Default `Metadata-Only` mode records operational metrics while omitting raw prompt/completion payloads and redacting 100% of auth headers; optional payload debug capture requires explicit admin opt-in with mandatory auto-expiring `TTL` confined to that project.
- **Error & Negative Flows:** External OTLP collector outage → Bounded ring buffer drops oldest telemetry frames while incrementing drop counters (controlled `Fail-Open` for auxiliary observability — `TP-BR-0502`), never stalling or crashing live AI inference streams.
- **Limits:** Bounded disk/time retention with automatic rotation preventing local storage exhaustion on self-hosted nodes.
- **Expected UX:** Pasting `x-tanpopo-request-id` into the search bar immediately renders a span-by-span waterfall across policy checks and upstream adapter hops.
- **Compatibility:** Adheres to OpenTelemetry GenAI Semantic Conventions.
- **SRS & AC:** `TP-FR-0805`..`0806`, `TP-NFR-0501`..`0502`, `TP-AC-F0804`.

---

## Domain 09: Web Console, CLI & Admin API (`console-cli-api`)

### `FT-15` — Astryx Web Console, `tanpopo` CLI & Admin API (`Unified Management Surfaces`)
- **Actors:** All human actors (`Personal Admin`, `Organization Owner`, `Team Admin`, `Developer`, `Operator`, `Extension Publisher`, `Reviewer`).
- **Preconditions:** Authenticated Control Plane session (or local CLI execution for offline authoring/diagnostics).
- **Trigger:** Browser UI interaction, `tanpopo ...` CLI invocation, or automated Admin REST API call.
- **Inputs:** Administrative commands, declarative config manifests, or interactive UI form submissions.
- **Outputs:** Optimistically concurrency-checked state mutations (`ETag`/`version` — `TP-BR-0801`), responsive UI views, or structured table/JSON CLI output.
- **States:** Exposes and governs full state transitions across `SM-01`..`SM-08`.
- **Business Rules:** `TP-BR-0201`, `TP-BR-0203`, `TP-BR-0801`, Astryx UI governance rules in [`AGENTS.md`](../../AGENTS.md).
- **Permissions:** Enforced through a single unified authorization layer inside the Control Plane API (Web Console UI visibility is purely ergonomic and never relied upon as a security boundary).
- **Success Flow:**
  - *Web Console:* Built from canonical **Astryx Design System** templates under the `neutral` theme, rendering dense operational records (logs, keys, MCP tool catalogs, ledgers) as clean Tables/Lists rather than bloated card grids, covering Setup Wizard, plugin permission diffs, Playground, and emergency kill-switches.
  - *CLI:* Governs server lifecycle, self-diagnostics (`doctor`), plugin install/pin/rollback, SDK scaffolding and contract replay (`plugin init/test/pack`), and offline bundle export.
- **Error & Negative Flows:** Concurrent conflicting admin edits → Return `409 Conflict` prompting reload of latest version; expired session → Prompt safe re-authentication without discarding unsaved non-sensitive form state.
- **Limits:** Mandatory cursor/offset pagination across all large API and UI collection views.
- **Expected UX:** Zero core administrative workflows trapped exclusively behind mouse-only UI (100% CLI/API automatable), paired with intuitive Web Console workflows for daily operations.
- **Compatibility:** Meets WCAG 2.2 AA accessibility and modern evergreen browser targets (`TP-NFR-0701`, `TP-NFR-0801`).
- **SRS & AC:** `TP-FR-0901`..`0904`, `TP-AC-F0901`..`F0902`.

---

## Domain 10: Self-Hosted Operations, Setup, Backup & Upgrades (`selfhost-ops`)

### `FT-16` — Zero-Friction Startup, GitOps Lockfile, Encrypted Backups & Safe Upgrades (`Self-Host Lifecycle & Operations`)
- **Actors:** `Operator`, `Personal Admin`.
- **Preconditions:** Host/container environment satisfies the OS/architecture support matrix (`TP-NFR-0901`).
- **Trigger:** Fresh install, declarative config sync, backup/restore, or instance version upgrade.
- **Inputs:** Declarative config file, `tanpopo.lock` lockfile, root secret vault master key, backup archive, or target release binary.
- **Outputs:** Healthy running instance (exposing distinct `/healthz` liveness and `/readyz` readiness probes), integrity-verified encrypted backup snapshot, or safe rollback state on upgrade failure.
- **States:** `SM-08` (Release & Upgrade Lifecycle).
- **Business Rules:** `TP-BR-0203`, `TP-BR-0802`, `TP-BR-0803`.
- **Permissions:** Infrastructure authority restricted to `Operator`.
- **Success Flow:** Runs single-node out of the box with an embedded database requiring zero external daemons, scaling cleanly into stateless multi-replica clusters at high volume; runs automated `Preflight Compatibility Check` across all installed `Extension` packages prior to applying Core upgrades.
- **Error & Negative Flows:** Installed critical `Extension` incompatible with target Core contract version → Halt upgrade cleanly at preflight (`BLOCKED_INCOMPATIBLE`) with actionable Adapter upgrade guidance rather than breaking production traffic post-upgrade.
- **Limits:** Backup archives never bundle the root master decryption key in plaintext (key custody remains strictly separated).
- **Expected UX:** One-command startup for Day-1 onboarding; deterministic `tanpopo.lock`-driven GitOps reproducibility for production SREs.
- **Compatibility:** Honors backward-compatible contract support windows (`N-1`) per release governance policy.
- **SRS & AC:** `TP-FR-1001`..`1005`, `TP-NFR-0601`..`0602`, `TP-AC-F1001`..`F1003`.
