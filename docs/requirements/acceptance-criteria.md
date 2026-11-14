# Functional, Security & Non-Functional Acceptance Criteria — Tanpopo

> **Acceptance Verification Specification (Phase ASTRA A03 — Reconciled in `TP-IMP-0001`)**
> - **Version:** `0.2.0-tp-imp-0001`
> - **Last Updated:** `2026-11-14`
> - **Authoring Role:** Principal Requirements Engineer & Product Manager
> - **Governance Status:** **`APPROVED (Subject to COND-PO-01..05)`** — Gate G1, Gate G2, and Change Requests `CR-01`..`04`, `CR-08`, `CR-09` approved by the Product Owner in [`docs/governance/decisions.md`](../governance/decisions.md) (closing `GAP-09` and `GAP-11`). Every criterion below is structured in `Given / When / Then` form suitable for deterministic hermetic CI (`TANPOPO_TEST_HERMETIC=1`), contract testing, and security verification. **100% of `Must` (`P0`) requirements in [`srs.md`](./srs.md) have direct acceptance criteria.**

---

## Part A — Functional Acceptance Criteria by Domain (`TP-AC-F0101` .. `TP-AC-F1003`)

### Domain 01: Gateway, Protocols & Multimodality (`gateway-protocols`)

- **`TP-AC-F0101` — Unified Inference Endpoints, SSE Streaming & Automatic `include_usage` (`TP-FR-0101`):**
  - *Given:* `Route R1` maps to an `ACTIVE_HEALTHY` `Model Deployment` and the `API Client` presents a valid `Virtual Key`.
  - *When:* Standard OpenAI SDK requests are sent to `/v1/chat/completions`, `/v1/responses` (both `stream: false` and `stream: true`), `/v1/embeddings`, and `/v1/models`.
  - *Then:* The system returns `200 OK` with a schema-valid JSON payload or properly terminated `text/event-stream`, automatically injects `stream_options.include_usage = true` on OpenAI-compatible `stream: true` upstream calls (`CR-09`), attaches `x-tanpopo-request-id` to response headers, and records the usage entry in the ledger.
- **`TP-AC-F0102` — Fail-Fast Rejection for Requests Exceeding Target Model Capabilities (`TP-FR-0102`):**
  - *Given:* The target route contains only models declaring `audio_input = false` or `strict_json_schema = false` in their `Capability Profile`.
  - *When:* An `API Client` submits a request containing an audio attachment or a `response_format` requiring `json_schema` with `strict: true`.
  - *Then:* The system immediately rejects the request with `400 capability_not_supported` explicitly naming the missing capability, **opens zero outbound network connections to external providers**, and releases the budget hold (`RELEASED_UNUSED`).
- **`TP-AC-F0103` — Strict Prohibition of Silent Failover After Partial Stream Emission (`TP-FR-0103`):**
  - *Given:* `Route R1` has `Deployment D1` (primary) and `D2` (`ACTIVE_HEALTHY` fallback); `D1` successfully emits the first 15 SSE chunks to the `API Client` and then abruptly drops the connection.
  - *When:* The Gateway detects the mid-stream disconnect from `D1`.
  - *Then:* The Gateway emits a canonical `mid_stream_abort` error event frame to close the SSE stream, **never dispatches a fallback request to `D2`**, and settles the partial output already streamed by `D1` tagged as `ESTIMATED`.
- **`TP-AC-F0104` — Immediate Cancellation Propagation & Uniform Error Envelope (`TP-FR-0104`):**
  - *Given:* A long-form text generation request is actively streaming SSE chunks at the 2-second mark.
  - *When:* The `API Client` actively closes the TCP/HTTP connection to cancel the request.
  - *Then:* The Gateway cancels the corresponding upstream provider request within `< 500 ms` (`TARGET / UNVERIFIED`) and immediately settles the budget hold.
- **`TP-AC-F0105` — Lossless Native Dialect Passthrough & Same-Dialect Failover Guard (`TP-FR-0105`):**
  - *Given:* An `API Client` calls `/native/anthropic/v1/messages` with provider-specific constructs (`cache_control`, `thinking` blocks) plus a newly introduced extension field never previously defined in Core.
  - *When:* The request traverses a route pointing to an Anthropic-dialect deployment (Case 1) or a misconfigured route pointing to a different-dialect deployment (Case 2).
  - *Then:* In Case 1, the upstream provider receives `100%` of the original JSON fields intact without stripping, and the ledger accurately extracts cache/reasoning token counts; in Case 2, the system immediately rejects the request with `400 dialect_mismatch`.
- **`TP-AC-F0106` — Unified Rerank Endpoint & Non-Rerank Capability Guard (`TP-FR-0107` — `Should`, `v0.2`):**
  - *Given:* `Route R_Rerank` targets an `ACTIVE_HEALTHY` reranker `Model Deployment` declaring `rerank = true`, whereas `Route R_Chat` targets a text-generation-only deployment declaring `rerank = false`.
  - *When:* An `API Client` submits `POST /v1/rerank` carrying `model`, `query`, `documents`, and `top_n` to `R_Rerank` (Case 1) and to `R_Chat` (Case 2).
  - *Then:* Case 1 returns `200 OK` with relevance-sorted document indices and scores, attaches `x-tanpopo-request-id`, and settles billed search/token units into `usage_settlements`; Case 2 fails fast prior to egress with `400 capability_not_supported` and releases the pre-request hold (`RELEASED_UNUSED`).

### Domain 02: Providers, Models & Credentials (`provider-credentials`)

- **`TP-AC-F0201` — Four-Entity Separation, Downward One-Way Sharing & In-Use Deletion Guard (`TP-FR-0201`, `TP-FR-0203`):**
  - *Given:* A single `Provider Adapter` is used to create two `Provider Connection` instances (`C_Org` at the Organization level and `C_Proj` at the Project level), where `C_Org` is shared downward to `Project P1` and attached to active `Route R_Active`.
  - *When:* The `TEAM_ADMIN` of `P1` routes traffic through `C_Org`, attempts to read or modify the secret key of `C_Org`, or an administrator attempts to hard-delete `C_Org` while `R_Active` is enabled.
  - *Then:* Inference routing succeeds normally; the `TEAM_ADMIN` attempt to read or modify the secret key is denied with `403`; and the hard-delete request on `C_Org` is blocked with `409 resource_in_use` until detached from `R_Active`.
- **`TP-AC-F0202` — Cold-Start Support for Local Models (`Ollama`/`vLLM`) & Custom Pricing (`TP-FR-0202`):**
  - *Given:* An internal `Model Deployment` (with `OPERATOR`-approved `allow_private_ip = TRUE`) is configured with `first_byte_timeout_ms = 60000` and unit pricing of `0 USD` (`SELF_HOSTED_INFRA`), and the GPU server takes `35 seconds` to load model weights into VRAM before emitting the first chunk.
  - *When:* An initial inference request is sent to that internal model.
  - *Then:* The request is not prematurely aborted at default 5–10s cloud timeouts, the deployment remains `ACTIVE_HEALTHY`, and the ledger records the configured internal unit price accurately.
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
  - *Given:* The Tanpopo Core process is running steadily (preserving an unchanged `tanpopo` binary hash); a mock new provider `Vendor-Z` has never existed in the Core codebase.
  - *When:* An `OPERATOR` installs and approves permissions for a `Declarative Pack` (or `Tier-2 Wasm Sandbox` plugin) for `Vendor-Z`, creates a `Connection`, and invokes inference.
  - *Then:* The inference query completes end-to-end through `Vendor-Z` without recompiling or restarting the entire Core process.
- **`TP-AC-F0301B` — Verification of Exact Five `MVP (v0.1)` Official Reference Adapters (`TP-FR-0301`, `TP-FR-0302` — `CR-01`):**
  - *Given:* A clean `MVP (v0.1)` Tanpopo build executed in offline hermetic mode (`TANPOPO_TEST_HERMETIC=1`).
  - *When:* Replaying deterministic `tanpopo plugin test` and unified `/v1/*` cassettes across all **5 `MVP (v0.1)` Official Reference Adapters** — Tier-1 Declarative Packs (**`OpenAI`**, **`OpenAI-Compatible (vLLM/SGLang/Groq/Together/DeepSeek)`**, **`Ollama`**) and Tier-2 `wazero` Wasm Sandbox Adapters (**`Anthropic`**, **`Gemini`**).
  - *Then:* All 5 Official Reference Adapters pass synchronous completion, SSE streaming, canonical error mapping, and multi-component token extraction with verified `origin_trust_tier = "official"` signatures and `0` bytes of raw credentials in guest memory (while `AWS Bedrock` SigV4 and `Azure OpenAI` remain explicitly scheduled for `v0.2`).
- **`TP-AC-F0302` — Multi-Source Installation, 3 Provenance Tiers, `enterprise_signed`, Permission Consent & `tanpopo.lock` Pinning (`TP-FR-0304`, `TP-FR-0307`):**
  - *Given:* An extension package fetched from pinned Git commit SHA / `.tpkg` / Private Registry lacks an official Tanpopo release signature and declares outbound access to `api.vendor-z.example`.
  - *When:* The install command runs prior to permission sheet confirmation, followed by explicit permission approval into either `INSTALLED_DISABLED` or `ENABLED_ACTIVE` (`SM-01`).
  - *Then:* Prior to approval, the package remains dormant in `PENDING_PERMISSION_APPROVAL` and serves zero traffic; after approval, the package carries `origin_trust_tier = "unverified_external"` (or `verified`/`official` strictly matching project signatures, alongside independent `enterprise_signed` verification when signed by the org's internal key), blocks any unverified `^@tanpopo(-[a-z0-9]+)?/` claim, and pins its SHA-256 digest and approved permissions in `tanpopo.lock`.
- **`TP-AC-F0303` — Automated Cassette-Driven Contract Test Harness (`TP-FR-0305`, `TP-FR-0903`):**
  - *Given:* A plugin author scaffolds a new extension package via `tanpopo plugin init` with recorded `Cassettes` covering 5 scenarios (synchronous response, SSE streaming, `401` auth error, `429` rate limit, mid-stream disconnect).
  - *When:* `tanpopo plugin test` is executed completely offline (without live provider keys).
  - *Then:* The CLI harness automatically validates the `Manifest`, input/output schemas, and error classification, emitting a clear pass/fail report and deterministic CI exit code.
- **`TP-AC-F0304` — Blocking Auto-Activation on Permission Escalation & Instant Rollback (`TP-FR-0306`):**
  - *Given:* `Plugin P@1.0.0` is running with approved network access to `a.example.com`; `P@1.1.0` adds `b.example.com` to its `Manifest`.
  - *When:* An update is triggered without new permission consent (Step 1), followed by consenting to `1.1.0` and then issuing a `Rollback` (`ROLLED_BACK` -> `ENABLED_ACTIVE`) to `1.0.0` while active streams are in flight (Step 2).
  - *Then:* In Step 1, `1.1.0` is held in `UPDATING_PENDING_RECONSENT` while `100%` of live traffic continues through `1.0.0`; in Step 2, new requests revert to `1.0.0` within seconds while in-flight streams on `1.1.0` complete via `DRAINING`.
- **`TP-AC-F0305` — Tier-3 Isolated Sidecar Execution Over `0600` UDS / Mutual TLS (`TP-FR-0308` — `Could`, `v0.3`):**
  - *Given:* An `OPERATOR` registers an `ISOLATED_SIDECAR` extension (`v0.3`) speaking `ConnectRPC` over a `0600` Unix Domain Socket or `mTLS` socket with pinned SPIFFE/SAN client and server certificates.
  - *When:* (a) Authorized traffic traverses the sidecar; (b) An unauthenticated process attempts to connect to the sidecar socket without valid `mTLS`/UDS peer credentials; (c) The sidecar exceeds its RPC deadline or crashes mid-call.
  - *Then:* (a) Succeeds within configured timeout and memory bounds; (b) Terminates during TLS/UDS handshake with `0` RPC frames decoded (`AUTH-08`); (c) Fails only the affected request with `502 extension_execution_fault` and `0` Tanpopo Core process crashes.

### Domain 04: MCP Plane & A2A Inter-Agent Plane (`mcp-a2a`)

- **`TP-AC-F0401` — Per-Key MCP `2025-03-26` Tool Catalog Filtering, Non-Truncating Pagination & Empirical Client Compatibility (`TP-FR-0401` — `COND-PO-04`):**
  - *Given:* An organization registers `MCP Server` backends over **`MCP Specification 2025-03-26 (Streamable HTTP)`** exposing 250 total tools; `Virtual Key K_Dev` is granted access to only 120 read-only tools.
  - *When:* The official MCP reference client and target IDE client fixtures (`CT-36`) authenticating with `K_Dev` paginate through `tools/list` via cursor and attempt a direct invocation of 1 tool belonging to the 130 unauthorized tools.
  - *Then:* `tools/list` returns all 120 authorized tools across pages (without truncating at 100 and hiding the other 130 tools), the unauthorized direct tool call is rejected with `403` and generates an audit event, and compatibility claims are strictly backed by passing `CT-36` test evidence (`COND-PO-04`).
- **`TP-AC-F0402` — Immediate Execution Block on MCP Tool Schema Hash Drift (`TP-FR-0402`):**
  - *Given:* Tool `query_crm` is approved in the `prod` environment with pinned RFC 8785 JCS `schema_hash = H1`.
  - *When:* The downstream `MCP Server` silently alters the text description of `query_crm` (`schema_hash` shifts to `H2`) and an agent invokes `query_crm`.
  - *Then:* The MCP plane immediately blocks the tool invocation (`403 schema_hash_drift_blocked`) and dispatches a security alert to administrators.
- **`TP-AC-F0403` — Dual-Mode 5-Tuple JCS Human Approval for `destructive` Tools (`TP-FR-0403`, `TP-FR-0904`, `SM-06`):**
  - *Given:* Tool `drop_staging_table` is classified with risk level `destructive` on the `MCP 2025-03-26` plane.
  - *When:* An agent invokes the tool either in short-wait synchronous mode (`<= 60s`, approved by a `SECURITY_REVIEWER` within 15s) or asynchronous mode (receiving a standard HTTP `200` MCP JSON-RPC pending response with `approval_request_id` in state `PENDING`, approved by a `SECURITY_REVIEWER` in the Web Console 5 minutes later transitioning `PENDING -> APPROVED`, and resumed/replayed with identical 5-tuple JCS arguments transitioning `APPROVED -> CONSUMED`).
  - *Then:* Both flows execute the downstream tool **strictly once** (`CONSUMED`), block replay or argument tampering (`403 parameter_hash_mismatch`), and record full provenance in `AuditEvent` without requiring custom non-standard client HTTP headers (`CR-08`).
- **`TP-AC-F0404` — `AgentCard` Discovery, `SM-09` Asynchronous A2A Delegation & Hop-Depth Limit (`TP-FR-0405`, `TP-FR-0406`):**
  - *Given:* Two valid `Agent Endpoint` registrations exist on the `/a2a/*` plane with `Max Delegation Hop Depth = 5`.
  - *When:* Agent A delegates a valid task to Agent B traversing `SM-09` states `submitted -> working <-> input-required -> completed` (Case 1), or creates a circular delegation chain reaching hop 6 (Case 2).
  - *Then:* Case 1 maps wire states 1-to-1 to uppercase `a2a_tasks.state` rows (`SUBMITTED`, `WORKING`, `INPUT_REQUIRED`, `COMPLETED`) and settles spend under a single `x-tanpopo-trace-root` ID; Case 2 is rejected immediately at hop 6 with `400 delegation_depth_exceeded`.

### Domain 05: Authentication, Multi-Tenancy & Authorization (`iam-tenancy`)

- **`TP-AC-F0501` — `Org → Project → Env` Hierarchy, `OPERATOR` Multi-Org API & Seamless Personal-Mode Upgrade (`TP-FR-0501`):**
  - *Given:* A user initializes Tanpopo in `Personal Admin` mode (`OPERATOR + ORG_OWNER` on default `Org 1`), creates multiple `Project` and `staging`/`prod` environments, and an `OPERATOR` provisions `Org 2` via `POST /admin/v1/organizations` (`CR-04`).
  - *When:* Transitioning to multi-project team collaboration and running automated cross-org isolation checks in `v0.1`.
  - *Then:* All existing keys, connections, and ledger records remain intact with zero destructive DB migration, and `Org 1` / `Org 2` remain `100%` isolated at the API/DB layer even before the `v1.0` Multi-Org Console UI Switcher is enabled.
- **`TP-AC-F0502` — Six-Role RBAC Enforcement & Brute-Force Login Protection (`TP-FR-0502`):**
  - *Given:* Principals hold each of the **6 canonical RBAC roles** (`OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`), while an external source submits repeated invalid password attempts exceeding the rate threshold.
  - *When:* `DEVELOPER` or `FINOPS_VIEWER` calls the Admin REST API to install an extension, read raw secrets, or raise `P1`'s budget ceiling.
  - *Then:* Unauthorized mutations return `403 Forbidden` (`FINOPS_VIEWER` retains read-only ledger/chargeback access only); the brute-force source is rate-limited (`429 Retry-After`) and logged in `audit_events`.
- **`TP-AC-F0503` — Zero-Downtime Dual-Key Rotation, Emergency Revocation & `QUOTA_PAUSED -> REVOKED` Non-Revival (`TP-FR-0503`, `SM-05`):**
  - *Given:* `Virtual Key K1` is actively serving traffic (`CREATED_ACTIVE`), and `Virtual Key K_Paused` is in `QUOTA_PAUSED` after exhausting its monthly quota.
  - *When:* (a) A 24h grace-window rotation (`ROTATING_DUAL_VALID`) issues `K2` on `K1`; (b) An admin revokes `K_Paused` (`QUOTA_PAUSED -> REVOKED`) and advances the billing clock to the next monthly period.
  - *Then:* (a) Both `K1` and `K2` authenticate validly during the grace window using plain `SHA-256` key hashes before `K1` expires; (b) `K_Paused` remains permanently `REVOKED` across the billing period reset and **never auto-revives**.
- **`TP-AC-F0504` — `OIDC SSO` Federation & Four-Eyes Approval Enforcement (`TP-FR-0504`):**
  - *Given:* An organization enables OIDC login and enforces the `Four-Eyes` policy for `unverified_external` extension approvals.
  - *When:* Administrator A uploads an external extension package and attempts to approve it using the same account.
  - *Then:* The system rejects self-approval (`403 self_approval_forbidden`), requiring a second independent `SECURITY_REVIEWER` / `ORG_OWNER` B to confirm before transitioning to `INSTALLED_DISABLED` or `ENABLED_ACTIVE`.

### Domain 06: Capability-Aware Routing & Resilience (`routing-resilience`)

- **`TP-AC-F0601` — Capability Filtering, Pre-Stream Failover & `HALF_OPEN_PROBING` Circuit Breaking (`TP-FR-0601`, `TP-FR-0603`):**
  - *Given:* `Route R1` contains `D1` (32k context window), `D2` (128k, primary), and `D3` (128k, fallback); `D2` repeatedly returns `503` prior to emitting any content bytes.
  - *When:* A batch of 60k-token context requests is sent to `R1`, followed by circuit cooldown expiry.
  - *Then:* `D1` is skipped due to insufficient context window; requests automatically fail over from `D2` to `D3`; once the failure threshold is crossed, `D2` transitions to `CIRCUIT_OPEN_UNHEALTHY` and subsequently probes recovery strictly via **`HALF_OPEN_PROBING`** (`SM-04`) before returning to `ACTIVE_HEALTHY`.
- **`TP-AC-F0602` — Hard Data Residency Enforcement Without Outage Bypass (`TP-FR-0602`):**
  - *Given:* Policy mandates `Data Region = EU`; `D_EU` is in `CIRCUIT_OPEN_UNHEALTHY` due to a provider outage while `D_US` is `ACTIVE_HEALTHY`.
  - *When:* An inference request is submitted to that route.
  - *Then:* The router refuses to route to `D_US` and immediately returns `503 no_compliant_deployment_available`.
- **`TP-AC-F0603` — Zero-Cost Dry-Run Route & Policy Explain Simulator (`TP-FR-0604`, `TP-FR-0904`):**
  - *Given:* A complex route configuration comprising 4 model deployments and 3 policy rules.
  - *When:* `POST /admin/v1/routes:explain` (or CLI explain) is invoked with simulated request parameters.
  - *Then:* The system returns all 4 deployments with exact selection/rejection rule explanations, emitting `0` outbound network packets to external providers and deducting `0 USD` from the budget ledger.

### Domain 07: Policy Engine & Operational Security (`policy-security`)

- **`TP-AC-F0701` — Immutable Audit Log & 4-Level Emergency Kill-Switches (`TP-FR-0704`, `TP-FR-0705`):**
  - *Given:* The system has recorded plugin installations, key creations, and policy violation events.
  - *When:* An administrator attempts to modify or delete an audit log entry via API (Step 1), then triggers emergency `Extension Quarantine` on a plugin and runs a blast-radius impact query (Step 2).
  - *Then:* Step 1 is rejected with `403`; Step 2 immediately severs traffic through that plugin (`QUARANTINED`) and returns the exact list of all `Project` and `Virtual Key` entities that invoked the plugin within the queried time window.

### Domain 08: Operational Telemetry, Budget Ledger & FinOps (`telemetry-finops`)

- **`TP-AC-F0801` — Two-Phase Pre-Execution Hold & Pod-Crash Orphan Reclamation (`TP-FR-0801`, `SM-07`):**
  - *Given:* `Project P1` has exactly `0.10 USD` (`100_000 micro-USD`) of remaining hard budget; each request requires an estimated `0.05 USD` pre-execution hold.
  - *When:* 30 concurrent streaming requests are fired simultaneously at `P1`, and 1 admitted Data-Plane worker crashes mid-stream (`STREAMING_ACCRUING`) past its lease TTL.
  - *Then:* At most 2 requests acquire `RESERVED_HOLD` and reach the upstream provider while at least 28 fail immediately at ingress with `429 project_budget_exceeded`; the crashed worker's orphaned `STREAMING_ACCRUING` hold transitions deterministically to `EXPIRED_RECLAIMED` via `OrphanReservationReaper` with an immutable audit entry.
- **`TP-AC-F0802` — Multi-Component Settlement & Slight End-of-Stream Negative Balance Carryover (`TP-FR-0802`):**
  - *Given:* Available balance is `0.04 USD`; the final request validly holds `0.04 USD`, but upon stream completion the provider reports actual consumption of `0.05 USD` (including reasoning/cache tokens).
  - *When:* The stream finishes and the client immediately sends a follow-up request.
  - *Then:* The active stream is not truncated on its final token, the ledger truthfully records the `0.05 USD` actual spend (recording `10_000 micro-USD` in `negative_carryover_micros`), and the subsequent request is hard-blocked (`429 project_budget_exceeded`) until budget is replenished.
- **`TP-AC-F0803` — Honest `EXACT` / `ESTIMATED` / `UNMETERED_FLAGGED` Classification & HMAC `Webhooks` (`TP-FR-0802`, `TP-FR-0803`):**
  - *Given:* Within a billing period, 3 transactions occur: (1) completed with authoritative provider token counts, (2) aborted mid-stream after emitting 400 words with no final usage frame, and (3) routed through a custom endpoint lacking a pricing schedule; soft alert thresholds (`50%`, `80%`, `100%`) are crossed.
  - *When:* Soft thresholds fire (`Banner`/OTel in `v0.1`; HMAC-SHA256 signed `webhook_subscriptions` delivery in `v0.2`) and the project's `Chargeback Report` is exported.
  - *Then:* Deduplicated alerts fire once per threshold milestone; Transaction (1) is tagged `EXACT`, transaction (2) computes an estimated cost `> 0` tagged `ESTIMATED`, and transaction (3) is tagged `UNMETERED_FLAGGED`; the report segregates these columns explicitly and **never claims 100% exact total cost while transactions of type (2) or (3) are present**.
- **`TP-AC-F0804` — Metadata-Only Default Privacy & Controlled `Fail-Open` OTLP Export (`TP-FR-0805`, `TP-FR-0806`):**
  - *Given:* A project uses default privacy settings and the external OpenTelemetry collector is abruptly shut down.
  - *When:* An inference query containing unique customer payload marker `SECRET_PII_MARKER_99` and authentication credentials is sent.
  - *Then:* The inference query still succeeds normally (the dropped-OTLP-batch counter increments with a warning without stalling the gateway), and full-text search across Tanpopo's persistent log store returns `0` matches for both `SECRET_PII_MARKER_99` and the raw credential value.

### Domain 09: Web Console, CLI & Admin API (`console-cli-api`)

- **`TP-AC-F0901` — `409 Conflict` Optimistic Concurrency Guard (`TP-FR-0901`):**
  - *Given:* Administrators A and B both fetch `Route R1` at version `v2` (`ETag: "v2"`); A saves an update advancing it to `v3`.
  - *When:* B submits an update to `R1` still carrying stale `If-Match: "v2"`.
  - *Then:* The Admin API rejects B's update with `409 Conflict` (`version_conflict`) without overwriting A's `v3` changes.
- **`TP-AC-F0901B` — Mandatory HMAC-Authenticated Cursor Pagination & Tamper Rejection (`TP-FR-0901` — `GAP-11`):**
  - *Given:* `Org A` holds `130` virtual keys/routes/audit events and `Org B` holds `40` items on the same instance.
  - *When:* (a) `Org A` traverses `GET /admin/v1/virtual-keys?limit=50&cursor=<opaque_hmac_cursor>` across 3 pages; (b) A caller mutates `1` byte of `cursor`, alters query filter parameters while reusing `cursor`, or replays `Org A`'s cursor under `Org B`'s authenticated session; (c) A concurrent `PATCH` omits or sends a stale `If-Match` `ETag`.
  - *Then:* (a) Returns all `130` `Org A` records in deterministic order with `0` duplicates and `0` skipped rows; (b) Rejects tampered, filter-mismatched, or cross-tenant cursors immediately with HTTP **`400 invalid_cursor`** (`0` cross-org leakage); (c) Rejects stale updates with HTTP **`409 Conflict`**.
- **`TP-AC-F0902` — Cohesive Day-1 Onboarding Across Web Console and `tanpopo` CLI (`TP-FR-0902`, `TP-FR-0903`):**
  - *Given:* A freshly started server instance with zero prior configuration.
  - *When:* An operator completes the Setup Wizard in the Web Console (Astryx `neutral`) to enable 1 Official Adapter, enter a credential (or local `Ollama` with `allow_private_ip` consent), atomically provision a `Model Deployment` + `Route`, issue a `Virtual Key`, and run 1 test prompt.
  - *Then:* The end-to-end flow finishes seamlessly (`TTFGR < 3 minutes` — `TARGET / UNVERIFIED`), and every newly created resource is consistently inspectable and manageable via the `tanpopo` CLI.

### Domain 10: Self-Hosted Operations, Installer, Backup & Upgrades (`selfhost-ops`)

- **`TP-AC-F1001` — Zero-Dependency Standalone Startup, `/healthz` `/readyz`, GitOps Bootstrap & `reset-local-owner` Break-Glass (`TP-FR-1001`, `TP-FR-1002`, `TP-FR-0903`):**
  - *Given:* A clean host without external PostgreSQL or Valkey installed, supplied with a declarative config file and `tanpopo.lock`, followed by simulated lockout of the local owner password.
  - *When:* `tanpopo serve` starts up (Step 1), and the host operator runs `tanpopo admin reset-local-owner` locally (Step 2).
  - *Then:* Step 1 initializes `SQLite WAL`, verifies `tanpopo.lock`, transitions `/healthz` and `/readyz` to `200 OK`, and serves governed traffic; Step 2 safely rotates/recovers the local owner credential, invalidates stale admin sessions, and appends an immutable `SYSTEM` audit event.
- **`TP-AC-F1002` — `Preflight Check` Upgrade Block (`ABORTED_SAFE`) & Clean Tenant Crypto-Shredding (`TP-FR-1003`, `TP-FR-1004`, `TP-FR-0307`):**
  - *Given:* An active `Extension` supports only contract `v1` while the target Core upgrade requires at least `v3` (Scenario 1); or an administrator confirms permanent deletion of `Project P_Old` (Scenario 2).
  - *When:* The upgrade preflight checker runs and the operator exits cleanly (Scenario 1), or deletion of `P_Old` is confirmed (Scenario 2).
  - *Then:* Scenario 1 halts safely in `BLOCKED_INCOMPATIBLE` listing the incompatible extension and transitions cleanly to `ABORTED_SAFE` (`SM-08`) with zero DB/binary corruption; Scenario 2 drains active streams and destroys `P_Old`'s `Project DEK`, irrecoverably crypto-shredding all secret credentials, debug blobs, and cached responses belonging to `P_Old`.
- **`TP-AC-F1003` — Multi-Replica Stateless Horizontal Scale-Out (`TP-FR-1005`):**
  - *Given:* A 3-replica Data Plane cluster sits behind a round-robin load balancer (no sticky sessions) backed by `PostgreSQL 16+` (`FORCE RLS`) and `Valkey` (`tp:v1:{<org_id>}:*`).
  - *When:* Continuous traffic is driven while 1 of the 3 replicas is abruptly terminated.
  - *Then:* Global budget ceilings and rate limits remain accurately enforced (never multiplied 3x), and all new requests are automatically served by the 2 surviving replicas.

---

## Part B — Security, Trust Boundary & Compliance Acceptance Criteria (`TP-AC-SEC-01` .. `TP-AC-SEC-09`)

| Security AC ID | Critical Risk Category | Adversarial / Abuse Test Scenario (`Given / When / Then`) |
|---|---|---|
| **`TP-AC-SEC-01`** | **1. Upstream Secret Vault Protection (`Secrets`)** | *Given:* Real API key `sk-live-REAL999` is stored in the secret vault and bound to a Tier-2 Wasm `Community Adapter`.<br/>*When:* (a) An administrator queries every Admin API/CLI/UI endpoint for connection details; (b) Wasm guest code dumps `100%` of its linear memory buffer; (c) The upstream provider returns a `401` error echoing the raw key string.<br/>*Then:* Across (a), (b), and (c), the plaintext string `sk-live-REAL999` **never** appears (`BlindHostCredentialBroker` injects credentials at the host network boundary with `0` secret bytes in Wasm RAM, `Cookie` injection is hard-banned, and the error scrubber strips echoed secrets from all responses and logs). |
| **`TP-AC-SEC-02`** | **2. Untrusted / Community Plugin Containment (`Community Plugins`)** | *Given:* An external extension package: (a) claims reserved namespace `^@tanpopo(-[a-z0-9]+)?/` or spoofs `origin_trust_tier = "official"` without a Tanpopo offline root signature, (b) has 1 byte altered relative to its `tanpopo.lock` digest, (c) attempts covert egress to `attacker.example` outside `allowed_hosts`, or (d) executes an infinite loop `while(true){}`.<br/>*When:* Installing and routing traffic through that extension.<br/>*Then:* (a) Reserved namespace spoofing fails `403 reserved_namespace_spoofing` and unverified packages stamp `unverified_external`; (b) Digest mismatch refuses to load (`artifact_digest_mismatch`); (c) Egress to `attacker.example` is blocked prior to socket open with a security `AuditEvent` and `QUARANTINED` transition; (d) The infinite loop terminates at its CPU quota without crashing or starving other gateway routes. |
| **`TP-AC-SEC-03`** | **3. `Authorized OAuth` Connection Security (`User / App OAuth`)** | *Given:* An authorized OAuth 2.1 connection flow.<br/>*When:* (a) An attacker replays a forged callback with a mismatched `state` parameter or missing `PKCE` verifier; (b) The provider returns insufficient or overly broad `scopes` mismatched against configuration; (c) Provider-side revocation triggers `invalid_grant` under heavy concurrent load.<br/>*Then:* (a) and (b) are rejected prior to activation; (c) The connection transitions immediately to `AUTH_EXPIRED_REAUTH_REQUIRED` (`SM-02`) / `REAUTH_REQUIRED` (`SM-03`) after a single representative refresh call, halts futile retries, and never leaks `refresh_token` material externally. |
| **`TP-AC-SEC-04`** | **4. Credential Stripping & SSRF Defense (`Token Forwarding & SSRF`)** | *Given:* An `API Client` sends `Virtual Key vk_tp_live_...` to Tanpopo; or a privileged user configures a model/MCP endpoint targeting `http://169.254.169.254/latest/meta-data/`, unapproved `http://127.0.0.1:6379`, DNS rebinding (`TTL=0`), or an upstream returning an HTTP `302 Redirect` to a different host.<br/>*When:* Connection validation or request forwarding runs.<br/>*Then:* The client `Virtual Key` is stripped completely before egress; on cross-origin HTTP `302` redirects, the system **never forwards the `Authorization` header** to an unapproved host; `169.254.169.254` and unauthorized private/loopback IPs are blocked at socket `connect()`. |
| **`TP-AC-SEC-04B`** | **4b. Multi-Dimensional Rate Limiting Enforcement (`TP-FR-0702` — `GAP-11`)** | *Given:* A `Virtual Key` and parent `Project` configure `rpm_limit = 60`, `tpm_limit = 20000`, and `max_concurrent_streams = 5`.<br/>*When:* Callers drive traffic exceeding (a) `60` requests/min (`RPM`), (b) `20,000` tokens/min (`TPM`), or (c) `5` simultaneous open SSE streams, and subsequently close `1` open stream.<br/>*Then:* Every excess request across (a), (b), and (c) fails fast pre-egress with HTTP **`429 rate_limit_exceeded`**, a canonical machine-readable limit reason, and a valid **`Retry-After`** HTTP header; stream teardown immediately releases the concurrency slot with `0` counter leaks. |
| **`TP-AC-SEC-05`** | **5. Four-Layer Trust Boundaries & `Fail-Closed` Posture (`Trust Boundaries`)** | *Given:* (a) A `Virtual Key` is scoped solely to `PROVIDER` inference; (b) An `Organization` policy sets `min_plugin_trust = "OFFICIAL_ONLY"`; (c) The hard-budget store or data-residency evaluator experiences a transient outage.<br/>*When:* (a) That key calls `/mcp` or `/a2a`; (b) A `TEAM_ADMIN` attempts to bind an `unverified_external` package to a project; (c) A billable inference request arrives.<br/>*Then:* All 3 scenarios fail safely (`Fail-Closed` — `403`/`503`), with zero implicit cross-plane privilege bridging or parent-policy bypass. |
| **`TP-AC-SEC-05B`** | **5b. Monotonic Child-Policy Narrowing Enforcement (`TP-FR-0701` — `GAP-11`)** | *Given:* Parent `Organization Org_A` configures `allowed_data_regions = ["EU"]`, `min_plugin_trust = "VERIFIED_OR_ENTERPRISE_SIGNED"`, and mandatory `FAIL_CLOSED` enforcement.<br/>*When:* An administrator attempts to create or update a child `Project` or `Environment` policy broadening `allowed_data_regions` to `["EU", "US"]`, lowering `min_plugin_trust` to `"ANY_APPROVED"`, or setting `failure_mode = "FAIL_OPEN"` on residency/auth/budget controls.<br/>*Then:* The Admin API rejects the write deterministically with HTTP **`422 policy_relaxation_forbidden`**, mutates `0` persisted policy rows, and records a governance violation `AuditEvent`. |
| **`TP-AC-SEC-06`** | **6. Consumer Subscription Terms Compliance (`Subscription Authorization`)** | *Given:* A full audit of Core source code, Official Adapters, Marketplace review rules, Web Console/Setup Wizard UI, and official documentation.<br/>*When:* Evaluating support for configuring or submitting extension packages that scrape browser cookies/sessions/closed OAuth tokens from consumer subscriptions (`ChatGPT Plus/Pro`, personal `Codex`, `Claude Pro/Max`, `Google Antigravity`, etc.).<br/>*Then:* Submissions to the official Marketplace are denied `official`/`verified` badges; `BlindHostCredentialBroker` hard-bans `Cookie` header injection; the product contains zero consumer session extractors, templates, or *"I accept the risk to use my Plus/Pro account as a shared API"* toggles. |
| **`TP-AC-SEC-07`** | **7. Multi-Tenant & Single-Environment Isolation (`Multi-Tenancy`)** | *Given:* A single instance hosts `Organization A` and `Organization B` (provisioned via `OPERATOR` API in `v0.1`); inside `Org A`, `Virtual Key K_Dev` is bound to `dev` and `K_Sec` holds 5 active long-lived SSE streams.<br/>*When:* (a) A principal in `Org A` probes UUIDs or cursors to access a `Route`, `Secret`, `MCP Tool`, or `AuditEvent` in `Org B`; (b) `K_Dev` invokes a `prod` route; (c) *Emergency Security Revoke* is triggered on `K_Sec`.<br/>*Then:* (a) Returns uniform `404 resource_not_found` / `400 invalid_cursor` concealing `Org B` existence and logs a security alert; (b) Rejects environment mismatch (`403 environment_scope_denied`); (c) All 5 active SSE streams on `K_Sec` are severed and new requests are blocked within **`< 1s` (`TARGET / UNVERIFIED`) in `v0.1` single-node mode** and **`< 5s` (`TARGET / UNVERIFIED`) in `v0.3+` cluster mode**. |
| **`TP-AC-SEC-08`** | **8. Side-Effect Protection & Privilege Amplification Guard (`Side Effects`)** | *Given:* `destructive` MCP tool `execute_refund` is approved (`SM-06`: `PENDING -> APPROVED`) by a `SECURITY_REVIEWER` for `(order_id="100", amount=20)`, and a parent A2A agent has `1.00 USD` remaining budget.<br/>*When:* (a) The agent tampers with the parameter to `amount=2000`, replays a `CONSUMED` approval, or attempts self-approval; (b) A valid `execute_refund` call times out on the network after dispatch to the `MCP Server`; (c) The parent A2A agent delegates a child task requesting a `5.00 USD` ceiling.<br/>*Then:* (a) Blocked with `403 parameter_hash_mismatch` / `self_approval_forbidden`; (b) Dispatched at most once with **zero automatic second retry**, returning `ambiguous_side_effect_timeout`; (c) Child A2A delegation exceeding parent budget is rejected immediately (`403`). |
| **`TP-AC-SEC-09`** | **9. Opt-In Exact-Match Response Cache Isolation & Crypto-Shredding (`TP-FR-0706` — `Could`, `v0.3`)** | *Given:* Response caching is **disabled by default** (`enabled = false`); `Project P1` (`Org A`) explicitly enables exact-match caching (encrypting cached completions under `P1`'s `Project DEK`), and populates a cached response.<br/>*When:* (a) Unconfigured default projects execute identical requests; (b) `Org B`, `Project P2`, a sibling `Environment` (`staging` vs `prod`), or a `Virtual Key` with a different `policy_hash` / `tool_acl_hash` sends the identical prompt and model; (c) `Project P1` is deleted (`Project DEK` destroyed).<br/>*Then:* (a) Default projects perform `0` cache reads/writes; (b) Cross-org, cross-project, cross-env, and cross-policy callers observe **`0` cache hits** (`CT-28`); (c) Destroying `P1`'s `Project DEK` immediately renders all cached ciphertexts for `P1` cryptographically undecipherable. |

---

## Part C — Non-Functional Requirement Verification Criteria (`TP-AC-N01` .. `TP-AC-N10`)

> **`COND-PO-01` Enforcement:** All numerical latency, memory, startup, availability, concurrency, revocation, erasure, and RTO pass thresholds below remain explicitly designated **`TARGET / UNVERIFIED`** until empirically measured by automated benchmark harnesses.

| AC ID | Verified NFR IDs | Measurement Method & Pass Threshold (**`TARGET / UNVERIFIED`**) |
|---|---|---|
| **`TP-AC-N01`** | `TP-NFR-0101`..`0103` | Run an automated load generator against a deterministic `0 ms` mock upstream provider: verify added internal proxy overhead **`p95 < 2 ms` (`TARGET / UNVERIFIED`, Tier 1)** / **`< 5 ms` (`TARGET / UNVERIFIED`, Tier 2)**, per-chunk SSE passthrough latency **`< 1 ms` (`TARGET / UNVERIFIED`)**, idle single-node RSS **`< 128 MB` (`TARGET / UNVERIFIED`)**, and cold startup to `/readyz` **`< 3 seconds` (`TARGET / UNVERIFIED`)**. |
| **`TP-AC-N02`** | `TP-NFR-0201`..`0202` | Inject random single-node Data Plane crashes in a `>= 2`-node cluster (`v0.3+`) plus an infinite-loop faulty extension (`v0.1+`): verify aggregate HA traffic sustains availability **`>= 99.9%` (`TARGET / UNVERIFIED`)**, unrelated route `p95` latency degrades by **`<= 10%` (`TARGET / UNVERIFIED`)**, and the host gateway process records `0` crashes. |
| **`TP-AC-N03`** | `TP-NFR-0301`..`0302` | Hold **`1,000` concurrent 60-second SSE connections (`TARGET / UNVERIFIED`)** open on a reference node (2 vCPU, 2 GB RAM): verify zero post-close memory leaks and near-linear throughput scaling (**`TARGET / UNVERIFIED`**) when expanding from 1 to 3 replicas (`v0.3+`). |
| **`TP-AC-N04`** | `TP-NFR-0404` | While driving `100 req/s` with a `Virtual Key`, trigger emergency revocation and measure the timestamp delta of the final admitted request: verify **`< 1 second` (`TARGET / UNVERIFIED`) in `v0.1` single-node mode**, and **`< 5 seconds` (`TARGET / UNVERIFIED`) in `v0.3+` distributed HA cluster mode** (`CR-04`). |
| **`TP-AC-N05`** | `TP-NFR-0501`..`0502` | Configure short retention TTLs in test mode and execute `Project` deletion (`Project DEK` destruction): verify background TTL purging completes on schedule without locking live traffic tables and `100%` of deleted project secrets, debug payloads, and cache ciphertexts become unrecoverable within **`< 60 seconds` (`TARGET / UNVERIFIED`)**. |
| **`TP-AC-N06`** | `TP-NFR-0601`..`0602` | Execute an online `.tpop.enc` backup under live load, provision a clean host, and restore: verify full recovery within **`RTO < 15 minutes` (`TARGET / UNVERIFIED`)**, confirm backup archives cannot decrypt secrets without the detached `Master KEK`, and verify exported declarative configs boot cleanly in a new environment. |
| **`TP-AC-N07`** | `TP-NFR-0701`..`0702` | Run automated UI tests across target browsers at `1440px`, `1280px`, `1024px`, `768px`, and `375px` viewports (covering alert banners and emergency kill-switches): verify zero layout breakage, intact table scrollbars, and unobstructed emergency controls. |
| **`TP-AC-N08`** | `TP-NFR-0801`, `TP-NFR-1001` | Run `npx @astryxdesign/cli doctor --built <dir>`, static UI source checks, and automated WCAG 2.2 AA + keyboard-only navigation suites: verify `0 fail` from `astryx doctor`, `0` raw `div`/`span` elements, `0` hardcoded non-token hex/px styles, and full keyboard-only completion of permission approval and key revocation flows. |
| **`TP-AC-N09`** | `TP-NFR-0901` | Verify CI builds and passes smoke tests for multi-arch OCI container images (`linux/amd64`, `linux/arm64`) and native `tanpopo` CLI binaries on Linux, macOS (`arm64`), and Windows (`amd64`). |
| **`TP-AC-N10`** | `TP-NFR-0902`, `TP-NFR-1002` | Load an extension targeting contract version `N-1` on Core version `N`: verify full backward compatibility; verify all rejection responses conform to the canonical error schema and `tanpopo doctor` pinpoints injected misconfigurations accurately. |
