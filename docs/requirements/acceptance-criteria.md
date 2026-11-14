# Functional, Security & Non-Functional Acceptance Criteria — Tanpopo

> **Acceptance Verification Specification (Phase ASTRA A03 — Gate G2: `READY_FOR_REVIEW` | `DRAFT — UNLOCKED SCOPE`)**
> - **Version:** `0.1.0-step03-draft`
> - **Last Updated:** `2026-11-11`
> - **Authoring Role:** Principal Requirements Engineer & Product Manager
> - **Governance Status:** **Unlocked-scope draft (`DRAFT`)**. Every criterion below is structured in `Given / When / Then` form suitable for automated integration testing, contract testing, and security penetration testing. **100% of `Must` (`P0`) requirements in [`srs.md`](./srs.md) have direct acceptance criteria.**

---

## Part A — Functional Acceptance Criteria by Domain (`TP-AC-F0101` .. `TP-AC-F1003`)

### Domain 01: Gateway, Protocols & Multimodality (`gateway-protocols`)

- **`TP-AC-F0101` — Unified Inference Endpoints & SSE Streaming (`TP-FR-0101`):**
  - *Given:* `Route R1` maps to a `HEALTHY` `Model Deployment` and the `API Client` presents a valid `Virtual Key`.
  - *When:* Standard OpenAI SDK requests are sent to `/v1/chat/completions`, `/v1/responses` (both `stream: false` and `stream: true`), and `/v1/embeddings`.
  - *Then:* The system returns `200 OK` with a schema-valid JSON payload or properly terminated `text/event-stream`, attaches `x-tanpopo-request-id` to the response headers, and records the usage entry in the ledger.
- **`TP-AC-F0102` — Fail-Fast Rejection for Requests Exceeding Target Model Capabilities (`TP-FR-0102`):**
  - *Given:* The target route contains only models declaring `audio_input = false` or `strict_json_schema = false` in their `Capability Profile`.
  - *When:* An `API Client` submits a request containing an audio attachment or a `response_format` requiring `json_schema` with `strict: true`.
  - *Then:* The system immediately rejects the request with `400 capability_not_supported` explicitly naming the missing capability, **opens zero outbound network connections to external providers**, and releases the budget hold (`RELEASED_UNUSED`).
- **`TP-AC-F0103` — Strict Prohibition of Silent Failover After Partial Stream Emission (`TP-FR-0103`):**
  - *Given:* `Route R1` has `Deployment D1` (primary) and `D2` (healthy fallback); `D1` successfully emits the first 15 SSE chunks to the `API Client` and then abruptly drops the connection.
  - *When:* The Gateway detects the mid-stream disconnect from `D1`.
  - *Then:* The Gateway emits a canonical `mid_stream_abort` error event frame to close the SSE stream, **never dispatches a fallback request to `D2`**, and settles the partial output already streamed by `D1` tagged as `ESTIMATED`.
- **`TP-AC-F0104` — Immediate Cancellation Propagation & Uniform Error Envelope (`TP-FR-0104`):**
  - *Given:* A long-form text generation request is actively streaming SSE chunks at the 2-second mark.
  - *When:* The `API Client` actively closes the TCP/HTTP connection to cancel the request.
  - *Then:* The Gateway cancels the corresponding upstream provider request within `< 500 ms` and immediately settles the budget hold.
- **`TP-AC-F0105` — Lossless Native Dialect Passthrough & Same-Dialect Failover Guard (`TP-FR-0105`):**
  - *Given:* An `API Client` calls `/native/anthropic/v1/messages` with provider-specific constructs (`cache_control`, `thinking` blocks) plus a newly introduced extension field never previously defined in Core.
  - *When:* The request traverses a route pointing to an Anthropic-dialect deployment (Case 1) or a misconfigured route pointing to a different-dialect deployment (Case 2).
  - *Then:* In Case 1, the upstream provider receives 100% of the original JSON fields intact without stripping, and the ledger accurately extracts cache/reasoning token counts; in Case 2, the system immediately rejects the request with `400 dialect_mismatch`.

### Domain 02: Providers, Models & Credentials (`provider-credentials`)

- **`TP-AC-F0201` — Four-Entity Separation, Downward One-Way Sharing & In-Use Deletion Guard (`TP-FR-0201`, `TP-FR-0203`):**
  - *Given:* A single `Provider Adapter` is used to create two `Provider Connection` instances (`C_Org` at the Organization level and `C_Proj` at the Project level), where `C_Org` is shared downward to `Project P1` and attached to active `Route R_Active`.
  - *When:* The `Team Admin` of `P1` routes traffic through `C_Org`, attempts to read or modify the secret key of `C_Org`, or an administrator attempts to hard-delete `C_Org` while `R_Active` is enabled.
  - *Then:* Inference routing succeeds normally; the `Team Admin` attempt to read or modify the secret key is denied with `403`; and the hard-delete request on `C_Org` is blocked with `409 resource_in_use` until detached from `R_Active`.
- **`TP-AC-F0202` — Cold-Start Support for Local Models (`Ollama`/`vLLM`) & Custom Pricing (`TP-FR-0202`):**
  - *Given:* An internal `Model Deployment` is configured with `first_byte_timeout = 60s` and unit pricing of `0 USD` (or an internal amortization rate), and the GPU server takes `35 seconds` to load model weights into VRAM before emitting the first chunk.
  - *When:* An initial inference request is sent to that internal model.
  - *Then:* The request is not prematurely aborted at default 5–10s cloud timeouts, the deployment remains `HEALTHY`, and the ledger records the configured internal unit price accurately.
- **`TP-AC-F0203` — Single-Flight Refresh for `Authorized OAuth 2.1` (`TP-FR-0205`):**
  - *Given:* An `Authorized OAuth` connection has an `access_token` reaching its expiration threshold while simultaneously receiving 50 concurrent inference requests.
  - *When:* Automatic token refresh is triggered.
  - *Then:* Exactly 1 refresh request is sent to the provider's `token_endpoint`; the other 49 concurrent requests await the refreshed token, and all 50 inference requests complete successfully without token revocation race conditions.
- **`TP-AC-F0204` — Cloud Federated Identity & External KMS Envelope Encryption (`TP-FR-0206`):**
  - *Given:* A connection is configured to use `Workload Identity` / OIDC or an external KMS envelope encryption key store.
  - *When:* Server startup and inference calls to the target cloud provider are executed.
  - *Then:* Authentication succeeds without persisting long-lived static API keys on local disk; if KMS decryption permission is revoked, the server transitions `/readyz` to safely unready (`Fail-Closed`).

### Domain 03: Extension Engine & Marketplace (`plugin-marketplace`)

- **`TP-AC-F0301` — Hot-Installing a New Provider Adapter Without Recompiling Core (`TP-FR-0301`, `TP-FR-0302`):**
  - *Given:* The Tanpopo Core process is running steadily (preserving an unchanged Core binary hash); a mock new provider `Vendor-Z` has never existed in the Core codebase.
  - *When:* An operator installs and approves permissions for a `Declarative Pack` (or `Sandbox Plugin`) for `Vendor-Z`, creates a `Connection`, and invokes inference.
  - *Then:* The inference query completes end-to-end through `Vendor-Z` without recompiling or restarting the entire Core process.
- **`TP-AC-F0302` — Multi-Source Installation, 3 Provenance Tiers, Permission Consent & `tanpopo.lock` Pinning (`TP-FR-0304`, `TP-FR-0307`):**
  - *Given:* An extension package fetched from Git/URL/ZIP/Private Registry lacks an official Tanpopo release signature and declares outbound access to `api.vendor-z.example`.
  - *When:* The install command runs prior to permission sheet confirmation, followed by explicit permission approval.
  - *Then:* Prior to approval, the package remains dormant in `PENDING_PERMISSION_APPROVAL` and serves zero traffic; after approval, the package carries the `unverified-external` badge (or `verified`/`official` strictly matching valid cryptographic signatures) and its SHA-256 digest and approved permission set are recorded accurately in `tanpopo.lock`.
- **`TP-AC-F0303` — Automated Cassette-Driven Contract Test Harness (`TP-FR-0305`, `TP-FR-0903`):**
  - *Given:* A plugin author scaffolds a new extension package via `tanpopo plugin init` with recorded `Cassettes` covering 5 scenarios (synchronous response, SSE streaming, `401` auth error, `429` rate limit, mid-stream disconnect).
  - *When:* `tanpopo plugin test` is executed completely offline (without live provider keys).
  - *Then:* The CLI harness automatically validates the `Manifest`, input/output schemas, and error classification, emitting a clear pass/fail report and deterministic CI exit code.
- **`TP-AC-F0304` — Blocking Auto-Activation on Permission Escalation & Instant Rollback (`TP-FR-0306`):**
  - *Given:* `Plugin P@1.0.0` is running with approved network access to `a.example.com`; `P@1.1.0` adds `b.example.com` to its `Manifest`.
  - *When:* An update is triggered without new permission consent (Step 1), followed by consenting to `1.1.0` and then issuing a `Rollback` to `1.0.0` while active streams are in flight (Step 2).
  - *Then:* In Step 1, `1.1.0` is held in `UPDATING_PENDING_RECONSENT` while `100%` of live traffic continues through `1.0.0`; in Step 2, new requests revert to `1.0.0` within seconds while in-flight streams on `1.1.0` complete via `Graceful Drain`.

### Domain 04: MCP Plane & A2A Inter-Agent Plane (`mcp-a2a`)

- **`TP-AC-F0401` — Per-Key MCP Tool Catalog Filtering & Non-Truncating Pagination (`TP-FR-0401`):**
  - *Given:* An organization registers `MCP Server` backends exposing 250 total tools; `Virtual Key K_Dev` is granted access to only 120 read-only tools.
  - *When:* An `MCP Client` authenticating with `K_Dev` paginates through `tools/list` via cursor and then attempts a direct invocation of 1 tool belonging to the 130 unauthorized tools.
  - *Then:* `tools/list` returns all 120 authorized tools across pages (without truncating at 100 and completely hiding the other 130 tools); the unauthorized direct tool call is rejected with `403` and generates an audit event.
- **`TP-AC-F0402` — Immediate Execution Block on MCP Tool Schema Hash Drift (`TP-FR-0402`):**
  - *Given:* Tool `query_crm` is approved in the `production` environment with pinned `schema_hash = H1`.
  - *When:* The downstream `MCP Server` silently alters the text description of `query_crm` (`schema_hash` shifts to `H2`) and an agent invokes `query_crm`.
  - *Then:* The MCP plane immediately blocks the tool invocation with a schema hash mismatch error code and dispatches a security alert to administrators.
- **`TP-AC-F0403` — Dual-Mode Human Approval for `destructive` Tools (`TP-FR-0403`, `TP-FR-0904`):**
  - *Given:* Tool `drop_staging_table` is classified with risk level `destructive`.
  - *When:* An agent invokes the tool either in short-wait synchronous mode (approved by a `Reviewer` within 15 seconds) or asynchronous mode (receiving a pending token, approved by a `Reviewer` in the Web Console 5 minutes later, and re-invoked by the agent with the parameter-bound `Pre-Approval Token`).
  - *Then:* Both flows execute the downstream tool exactly once only after receiving a valid `Reviewer` approval signature, recording full provenance in `AuditEvent`.
- **`TP-AC-F0404` — `AgentCard` Discovery, Asynchronous A2A Delegation & Hop-Depth Limit (`TP-FR-0405`, `TP-FR-0406`):**
  - *Given:* Two valid `Agent Endpoint` registrations exist on the A2A plane with `Max Delegation Hop Depth = 5`.
  - *When:* Agent A delegates a valid task to Agent B (Case 1) or creates a circular delegation chain reaching hop 6 (Case 2).
  - *Then:* Case 1 completes the task state stream and aggregates cost under a single root trace ID; Case 2 is rejected immediately at the hop exceeding the ceiling with error code `delegation_depth_exceeded`.

### Domain 05: Authentication, Multi-Tenancy & Authorization (`iam-tenancy`)

- **`TP-AC-F0501` — `Org → Project → Env` Hierarchy & Seamless Upgrade from Personal Mode (`TP-FR-0501`):**
  - *Given:* A user initializes Tanpopo in `Personal Mode` (auto-provisioning 1 default `Organization`, 1 `Project`, and 1 `Environment`), then creates a second `Project` and `staging`/`production` environments.
  - *When:* Transitioning to multi-project team collaboration views.
  - *Then:* All existing keys, connections, and ledger records remain intact without requiring a destructive database re-migration.
- **`TP-AC-F0502` — Centralized RBAC Enforcement & Brute-Force Login Protection (`TP-FR-0502`):**
  - *Given:* An account holds the `Developer` role in `Project P1`, while an external source submits repeated invalid password attempts exceeding the rate threshold.
  - *When:* The `Developer` directly calls the Admin REST API to install a new extension or raise `P1`'s budget ceiling.
  - *Then:* The API returns `403 Forbidden` (independent of Web UI controls); the brute-force source is temporarily rate-limited/locked out and logged in the security audit trail.
- **`TP-AC-F0503` — Zero-Downtime Dual-Key Rotation vs Emergency Revocation (`TP-FR-0503`):**
  - *Given:* `Virtual Key K1` is actively serving continuous traffic.
  - *When:* A key rotation (`Rotate` with a 24h grace window) is triggered to issue `K2`, and the persisted key table is inspected.
  - *Then:* Both `K1` and `K2` authenticate validly during the grace window, the database stores only one-way cryptographic hashes (zero plaintext key strings), and `K1` automatically expires once the rotation window elapses.
- **`TP-AC-F0504` — `OIDC SSO` Federation & Four-Eyes Approval Enforcement (`TP-FR-0504`):**
  - *Given:* An organization enables OIDC login and enforces the `Four-Eyes` policy for external extension approvals.
  - *When:* Administrator A uploads an external extension package and attempts to approve it using the same account.
  - *Then:* The system rejects self-approval, requiring a second independent administrator/reviewer B to confirm before transitioning to `ENABLED_ACTIVE`.

### Domain 06: Capability-Aware Routing & Resilience (`routing-resilience`)

- **`TP-AC-F0601` — Capability Filtering, Pre-Stream Failover & Circuit Breaking (`TP-FR-0601`, `TP-FR-0603`):**
  - *Given:* `Route R1` contains `D1` (32k context window), `D2` (128k, primary), and `D3` (128k, fallback); `D2` repeatedly returns `503` prior to emitting any content bytes.
  - *When:* A batch of 60k-token context requests is sent to `R1`.
  - *Then:* `D1` is skipped due to insufficient context window; requests automatically fail over from `D2` to `D3` successfully; once the failure threshold is crossed, `D2` transitions to `CIRCUIT_OPEN_UNHEALTHY` so subsequent requests route directly to `D3` without incurring `D2` timeout latency.
- **`TP-AC-F0602` — Hard Data Residency Enforcement Without Outage Bypass (`TP-FR-0602`):**
  - *Given:* Policy mandates `Data Region = EU`; `D_EU` has an open circuit breaker due to a provider outage while `D_US` is completely healthy.
  - *When:* An inference request is submitted to that route.
  - *Then:* The router refuses to route to `D_US` and immediately returns `503 no_compliant_deployment_available`.
- **`TP-AC-F0603` — Zero-Cost Dry-Run Route & Policy Explain Simulator (`TP-FR-0604`, `TP-FR-0904`):**
  - *Given:* A complex route configuration comprising 4 model deployments and 3 policy rules.
  - *When:* `/v1/route/explain` is invoked (or triggered via Console/CLI) with simulated request parameters.
  - *Then:* The system returns all 4 deployments with exact selection/rejection rule explanations, emitting `0` outbound network packets to external providers and deducting `0 USD` from the budget ledger.

### Domain 07: Policy Engine & Operational Security (`policy-security`)

- **`TP-AC-F0701` — Immutable Audit Log & 4-Level Emergency Kill-Switches (`TP-FR-0704`, `TP-FR-0705`):**
  - *Given:* The system has recorded plugin installations, key creations, and policy violation events.
  - *When:* An administrator attempts to modify or delete an audit log entry via API (Step 1), then triggers emergency `Extension Quarantine` on a plugin and runs a blast-radius impact query (Step 2).
  - *Then:* Step 1 is rejected with `403`; Step 2 immediately severs traffic through that plugin and returns the exact list of all `Project` and `Virtual Key` entities that invoked the plugin within the queried time window.

### Domain 08: Operational Telemetry, Budget Ledger & FinOps (`telemetry-finops`)

- **`TP-AC-F0801` — Two-Phase Pre-Execution Hold Preventing Concurrent Budget Overshoot (`TP-FR-0801`):**
  - *Given:* `Project P1` has exactly `0.10 USD` of remaining hard budget; each request requires an estimated `0.05 USD` pre-execution hold.
  - *When:* 30 concurrent streaming requests are fired simultaneously at `P1`.
  - *Then:* At most 2 requests acquire `RESERVED_HOLD` and reach the upstream provider; at least 28 requests are rejected immediately at ingress with `429 project_budget_exceeded`; if a node crash leaves a hold orphaned past its TTL, the reaper reclaims it cleanly with an audit record.
- **`TP-AC-F0802` — Multi-Component Settlement & Slight End-of-Stream Negative Balance Carryover (`TP-FR-0802`):**
  - *Given:* Available balance is `0.04 USD`; the final request validly holds `0.04 USD`, but upon stream completion the provider reports actual consumption of `0.05 USD` (including reasoning/cache tokens).
  - *When:* The stream finishes and the client immediately sends a follow-up request.
  - *Then:* The active stream is not truncated on its final token, the ledger truthfully records the `0.05 USD` actual spend (bringing available balance to `-0.01 USD`), and the subsequent request is hard-blocked until budget is replenished.
- **`TP-AC-F0803` — Honest `EXACT` / `ESTIMATED` / `UNMETERED_FLAGGED` Classification (`TP-FR-0802`, `TP-FR-0803`):**
  - *Given:* Within a billing period, 3 transactions occur: (1) completed with authoritative provider token counts, (2) aborted mid-stream after emitting 400 words with no final usage frame, and (3) routed through a custom endpoint lacking a pricing schedule.
  - *When:* The project's `Chargeback Report` is exported.
  - *Then:* Transaction (1) is tagged `EXACT`, transaction (2) computes an estimated cost `> 0` tagged `ESTIMATED`, and transaction (3) is tagged `UNMETERED_FLAGGED`; the report segregates these columns explicitly and **never claims 100% exact total cost while transactions of type (2) or (3) are present**.
- **`TP-AC-F0804` — Metadata-Only Default Privacy & Controlled `Fail-Open` OTLP Export (`TP-FR-0805`, `TP-FR-0806`):**
  - *Given:* A project uses default privacy settings and the external OpenTelemetry collector is abruptly shut down.
  - *When:* An inference query containing unique customer payload marker `SECRET_PII_MARKER_99` and authentication credentials is sent.
  - *Then:* The inference query still succeeds normally (the dropped-OTLP-batch counter increments with a warning without stalling the gateway), and full-text search across Tanpopo's persistent log store returns `0` matches for both `SECRET_PII_MARKER_99` and the raw credential value.

### Domain 09: Web Console, CLI & Admin API (`console-cli-api`)

- **`TP-AC-F0901` — Standard Pagination & `409 Conflict` Optimistic Concurrency Guard (`TP-FR-0901`):**
  - *Given:* Administrators A and B both fetch `Route R1` at version `v2`; A saves an update advancing it to `v3`.
  - *When:* B submits an update to `R1` still carrying stale version `v2`.
  - *Then:* The Admin API rejects B's update with `409 Conflict` without overwriting A's `v3` changes.
- **`TP-AC-F0902` — Cohesive Day-1 Onboarding Across Web Console and CLI (`TP-FR-0902`, `TP-FR-0903`):**
  - *Given:* A freshly started server instance with zero prior configuration.
  - *When:* An operator completes the Setup Wizard in the Web Console (Astryx `neutral`) to enable 1 Official Adapter, enter a credential, create a `Route`, issue a `Virtual Key`, and run 1 test prompt.
  - *Then:* The end-to-end flow finishes seamlessly in `< 3 minutes`, and every newly created resource is consistently inspectable and manageable via the `tanpopo` CLI.

### Domain 10: Self-Hosted Operations, Installer, Backup & Upgrades (`selfhost-ops`)

- **`TP-AC-F1001` — Zero-Dependency Standalone Startup, `/healthz` `/readyz` & GitOps Bootstrap (`TP-FR-1001`, `TP-FR-1002`, `TP-FR-0903`):**
  - *Given:* A clean host without external PostgreSQL or Redis installed, supplied only with a declarative config file and `tanpopo.lock`.
  - *When:* The Tanpopo startup command is executed.
  - *Then:* The system initializes its embedded store, verifies `tanpopo.lock`, transitions `/healthz` and `/readyz` to `200 OK`, and serves traffic matching the declarative configuration.
- **`TP-AC-F1002` — `Preflight Check` Upgrade Block on Contract Mismatch & Clean Tenant Erasure (`TP-FR-1003`, `TP-FR-1004`, `TP-FR-0307`):**
  - *Given:* An active `Extension` supports only contract `v1` while the target Core upgrade requires at least `v3` (Scenario 1); or an administrator confirms permanent deletion of `Project P_Old` (Scenario 2).
  - *When:* The upgrade preflight checker runs (Scenario 1) or deletion of `P_Old` is confirmed (Scenario 2).
  - *Then:* Scenario 1 halts safely in `BLOCKED_INCOMPATIBLE`, explicitly listing the incompatible extension requiring update first; Scenario 2 drains active streams and irrecoverably erases all secret credentials and stored query payloads belonging to `P_Old`.
- **`TP-AC-F1003` — Multi-Replica Stateless Horizontal Scale-Out (`TP-FR-1005`):**
  - *Given:* A 3-replica Data Plane cluster sits behind a round-robin load balancer (no sticky sessions) backed by shared relational storage and distributed counters.
  - *When:* Continuous traffic is driven while 1 of the 3 replicas is abruptly terminated.
  - *Then:* Global budget ceilings and rate limits remain accurately enforced (never multiplied 3x), and all new requests are automatically served by the 2 surviving replicas.

---

## Part B — Security, Trust Boundary & Compliance Acceptance Criteria (`TP-AC-SEC-01` .. `TP-AC-SEC-08`)

| Security AC ID | Critical Risk Category | Adversarial / Abuse Test Scenario (`Given / When / Then`) |
|---|---|---|
| **`TP-AC-SEC-01`** | **1. Upstream Secret Vault Protection (`Secrets`)** | *Given:* Real API key `sk-live-REAL999` is stored in the secret vault and bound to a standard `Community Adapter`.<br/>*When:* (a) An administrator queries every Admin API/CLI/UI endpoint for connection details; (b) Extension code attempts to dump its entire input memory buffer; (c) The upstream provider returns a `401` error echoing the raw key string.<br/>*Then:* Across (a), (b), and (c), the plaintext string `sk-live-REAL999` **never** appears (the Host injects credentials at the network egress boundary and scrubs secret strings from all error responses and logs). |
| **`TP-AC-SEC-02`** | **2. Untrusted / Community Plugin Containment (`Community Plugins`)** | *Given:* An external extension package: (a) spoofs `"trust_tier": "official"` in its Manifest, (b) has 1 byte altered relative to its `tanpopo.lock` digest, (c) attempts covert egress to `attacker.example` outside `allowed_hosts`, or (d) executes an infinite loop `while(true){}`.<br/>*When:* Installing and routing traffic through that extension.<br/>*Then:* (a) Spoofed provenance is rejected or forcibly downgraded to `unverified-external`; (b) Digest mismatch refuses to load; (c) Egress to `attacker.example` is blocked immediately with a security `AuditEvent` and `QUARANTINED` transition; (d) The infinite loop is terminated at its CPU quota without crashing or starving other gateway routes. |
| **`TP-AC-SEC-03`** | **3. `Authorized OAuth` Connection Security (`User / App OAuth`)** | *Given:* An authorized OAuth 2.1 connection flow.<br/>*When:* (a) An attacker replays a forged callback with a mismatched `state` parameter or missing `PKCE` verifier; (b) The provider returns insufficient or overly broad `scopes` mismatched against configuration; (c) Provider-side revocation triggers `invalid_grant` under heavy concurrent load.<br/>*Then:* (a) and (b) are rejected prior to activation; (c) The connection transitions immediately to `AUTH_EXPIRED_REAUTH_REQUIRED` after a single representative refresh call, halts futile retries, and never leaks `refresh_token` material externally. |
| **`TP-AC-SEC-04`** | **4. Credential Stripping & SSRF Defense (`Token Forwarding & SSRF`)** | *Given:* An `API Client` sends `Virtual Key vk-client-123` to Tanpopo; or a privileged user configures a model/MCP endpoint targeting `http://169.254.169.254/latest/meta-data/`, `http://127.0.0.1:6379`, or an upstream returning an HTTP `302 Redirect` to a different host.<br/>*When:* Connection validation or request forwarding runs.<br/>*Then:* `vk-client-123` is stripped completely before egress; on cross-origin HTTP `302` redirects, the system **never forwards the `Authorization` header** to an unapproved host; all cloud metadata and unauthorized internal network addresses are blocked at the egress guard. |
| **`TP-AC-SEC-05`** | **5. Four-Layer Trust Boundaries & `Fail-Closed` Posture (`Trust Boundaries`)** | *Given:* (a) A `Virtual Key` is scoped solely to `Provider` inference; (b) An `Organization` policy restricts extensions exclusively to `official`; (c) The hard-budget store or data-residency evaluator experiences a transient outage.<br/>*When:* (a) That key calls `/mcp` or `/a2a`; (b) A `Team Admin` attempts to bind an `unverified-external` package to a project; (c) A billable inference request arrives.<br/>*Then:* All 3 scenarios fail safely (`Fail-Closed` — `403`/`503`), with zero implicit cross-plane privilege bridging or parent-policy bypass. |
| **`TP-AC-SEC-06`** | **6. Consumer Subscription Terms Compliance (`Subscription Authorization`)** | *Given:* A full audit of Core source code, Official Adapters, Marketplace review rules, Web Console/Setup Wizard UI, and official documentation.<br/>*When:* Evaluating support for configuring or submitting extension packages that scrape browser cookies/sessions/closed OAuth tokens from consumer subscriptions (`ChatGPT Plus/Pro`, personal `Codex`, `Claude Pro/Max`, `Google Antigravity`, etc.).<br/>*Then:* Submissions to the official Marketplace are denied `official`/`verified` badges; the product contains zero consumer session extractors, templates, or *"I accept the risk to use my Plus/Pro account as a shared API"* toggles; documentation states unambiguously that OAuth applies strictly to provider-authorized integrations. |
| **`TP-AC-SEC-07`** | **7. Multi-Tenant & Single-Environment Isolation (`Multi-Tenancy`)** | *Given:* A single instance hosts `Organization A` and `Organization B`; inside `Org A`, `Virtual Key K_Dev` is bound to `development` and `K_Sec` holds 5 active long-lived SSE streams.<br/>*When:* (a) A principal in `Org A` probes predictable IDs to access a `Route`, `Secret`, `MCP Tool`, or `AuditEvent` in `Org B`; (b) `K_Dev` invokes a `production` route; (c) *Emergency Security Revoke* is triggered on `K_Sec`.<br/>*Then:* (a) Returns a non-enumerable error concealing `Org B` resource existence and logs a security alert; (b) Rejects environment mismatch; (c) All 5 active SSE streams on `K_Sec` are terminated and new requests are blocked within the revocation SLA (`< 1s` single-node / `< 5s` cluster). |
| **`TP-AC-SEC-08`** | **8. Side-Effect Protection & Privilege Amplification Guard (`Side Effects`)** | *Given:* `destructive` MCP tool `execute_refund` is approved by a `Reviewer` for `(order_id="100", amount=20)`, and a parent A2A agent has `1.00 USD` remaining budget.<br/>*When:* (a) The agent tampers with the parameter to `amount=2000` (or attempts self-approval); (b) A valid `execute_refund` call times out on the network after dispatch to the `MCP Server`; (c) The parent A2A agent delegates a child task requesting a `5.00 USD` ceiling.<br/>*Then:* (a) Blocked with `403 parameter_hash_mismatch` / `self_approval_forbidden`; (b) Dispatched at most once with **zero automatic second retry**, returning `ambiguous_side_effect_timeout`; (c) Child A2A delegation exceeding parent budget is rejected immediately. |

---

## Part C — Non-Functional Requirement Verification Criteria (`TP-AC-N01` .. `TP-AC-N10`)

| AC ID | Verified NFR IDs | Measurement Method & Pass Threshold (*Pre-Benchmark Target*) |
|---|---|---|
| **`TP-AC-N01`** | `TP-NFR-0101`..`0103` | Run an automated load generator against a deterministic `0 ms` mock upstream provider: verify added internal proxy overhead `p95 < 2 ms` (Tier 1) / `< 5 ms` (Tier 2), per-chunk SSE passthrough latency `< 1 ms`, idle single-node RSS `< 128 MB`, and cold startup to `/readyz < 3 seconds`. |
| **`TP-AC-N02`** | `TP-NFR-0201`..`0202` | Inject random single-node Data Plane crashes in a `>= 2`-node cluster plus an infinite-loop faulty extension: verify aggregate traffic sustains target availability `>= 99.9%` and the host gateway process records `0` crashes. |
| **`TP-AC-N03`** | `TP-NFR-0301`..`0302` | Hold `1,000` concurrent 60-second SSE connections open on a reference node (2 vCPU, 2 GB RAM): verify zero post-close memory leaks and near-linear throughput scaling when expanding from 1 to 3 replicas. |
| **`TP-AC-N04`** | `TP-NFR-0404` | While driving `100 req/s` with a `Virtual Key`, trigger emergency revocation and measure the timestamp delta of the final admitted request: verify `< 1 second` (single-node) and `< 5 seconds` (distributed cluster). |
| **`TP-AC-N05`** | `TP-NFR-0501`..`0502` | Configure short retention TTLs in test mode and execute `Project` deletion: verify background TTL purging completes on schedule without locking live traffic tables and `100%` of deleted project secrets/payloads are purged from active storage within `< 60 seconds`. |
| **`TP-AC-N06`** | `TP-NFR-0601`..`0602` | Execute an online backup under live load, provision a clean host, and restore: verify full recovery within `< 15 minutes` (target `RTO`), confirm backup archives cannot decrypt secrets without the detached master key, and verify exported declarative configs boot cleanly in a new environment. |
| **`TP-AC-N07`** | `TP-NFR-0701`..`0702` | Run automated UI tests across target browsers at `1440px`, `1280px`, `768px`, and `375px` viewports (covering alert banners and emergency kill-switches): verify zero layout breakage, intact table scrollbars, and unobstructed emergency controls. |
| **`TP-AC-N08`** | `TP-NFR-0801`, `TP-NFR-1001` | Run `cmd /c npx astryx doctor --built <dir>`, static UI source checks, and automated WCAG 2.2 AA + keyboard-only navigation suites: verify `0 fail` from `astryx doctor`, `0` raw `div`/`span` elements, `0` hardcoded non-token hex/px styles, and full keyboard-only completion of permission approval and key revocation flows. |
| **`TP-AC-N09`** | `TP-NFR-0901` | Verify CI builds and passes smoke tests for multi-arch OCI container images (`linux/amd64`, `linux/arm64`) and native CLI binaries on Linux, macOS (`arm64`), and Windows (`amd64`). |
| **`TP-AC-N10`** | `TP-NFR-0902`, `TP-NFR-1002` | Load an extension targeting contract version `N-1` on Core version `N`: verify full backward compatibility; verify all rejection responses conform to the canonical error schema and `tanpopo doctor` pinpoints injected misconfigurations accurately. |
