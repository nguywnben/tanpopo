# 12 — End-to-End Traceability, Research Reconciliation & Coverage Audit

- **Document ID:** `TEEP-02A-DOC-12`
- **Machine-Readable Companion:** [`data/cap-to-catalog-traceability.csv`](./data/cap-to-catalog-traceability.csv)
- **Audit Result:** **`100%` Bidirectional Coverage Verified** across `CAP-001`..`CAP-161` (`161/161`), `INN-01`..`INN-12` (`12/12`), `QAS-ENT-01`..`12` (`12/12`), `OQ-01`..`16` (`16/16` -> `DEC-CAND-01`..`22`), `DEF-001`..`086` (`15/15` open `PARTIAL`s), `PG-001`..`014` (`14/14`), and all `11` underrepresented capability areas from `TEEP-02A` §3.

---

## 1. Explicit `TEEP-01` Research Reconciliation Summary (`ACCEPTED WITH CORRECTIONS`)

Commit 1 on `planning/teep-02a-feature-catalog` reconciled the `TEEP-01` intelligence package while keeping the original `planning/teep-01-intelligence` branch untouched at `a9f8467ddfd10f2a7962aae8f213e7f3277f2bdd`:
1. **Count Alignment:** Standardized **6 Strategic Pillars**, **12 Researched Innovations (`INN-01`..`12`)**, **12 Enterprise Quality Attribute Scenarios (`QAS-ENT-01`..`12`)**, and **92 Source Register Entries (`SRC-001`..`084` + `8` letter-suffixed companion records)**.
2. **Epistemic Precision (`[RUNTIME_VERIFIED]` vs `[SOURCE_VERIFIED]` vs `[DESIGN_PROPOSAL]` vs `[HYPOTHESIS]`):** Explicitly distinguished runtime-reproduced `QA-005`/benchmark findings from static source/migration inspection findings — most importantly classifying the PostgreSQL `handleRetireMasterKEK` `FORCE RLS` visibility issue (`ops_lifecycle.go:196`) as **`FND-001` (`P0` `[SOURCE_VERIFIED — REQUIRES NON-DESTRUCTIVE REPRODUCTION]`)**.
3. **Claim Qualification:** Removed unqualified security, market-first/uniqueness, regulatory compliance, unconditional pricing-fidelity, and unmeasured performance claims, and marked candidate technical designs (`RateCard` unit scale, `.tpkg` signing target, policy engine, SDK runtime, distributed budget algorithm) as deferred to `TEEP-03`.

---

## 2. Summary of Added, Substantially Expanded, Consolidated, Promoted & Rejected Items

### 2.1 Twelve Net-New Product Features Added in `TEEP-02A` (`ADDED_IN_TEEP_02A`)
- **`FEAT-011` (DOM-02):** Curated Provider & Self-Hosted Inference Setup Templates — *Replaces blank, error-prone connection forms with guided, provider-specific setup cards for 16+ popular cloud APIs and local GPU servers. Each template pre-fills the right base URL, authentication method, regional privacy defaults, and recommended starter models, automatically fixes common URL mistakes like duplicate '/v1' paths, and verifies connectivity in one click before saving.*
- **`FEAT-098` (DOM-16):** Dedicated Admin Plane Network Listener & Management Port Isolation (--admin-listen) — *Allows operators in either single-binary (`--role all`) or multi-node cluster mode to bind the Admin API (`/admin/v1/*`), embedded Web Console UI, and optional diagnostics/metrics onto a dedicated private management address, port, VLAN, or WireGuard/Tailscale interface (`--admin-listen` / `TANPOPO_ADMIN_LISTEN_ADDR`), while keeping the Data Plane (`/v1/*`, `/native/*`, `/mcp`, `/a2a`) on the workload listener (`--listen`) with zero admin routes mounted on the public socket.*
- **`FEAT-105` (DOM-18):** Public & Embedded Console Marketplace Discovery, Compatibility Filtering & Security Nutrition Labels — *Delivers a cohesive extension and Solution Pack discovery experience — available both as a public searchable web catalog and an embedded, offline-capable Console Marketplace (`SCR-07`) — featuring multi-faceted filtering, Gateway ABI compatibility checks, verified publisher trust tiers, version-to-version permission/schema diffs, and standardized **Security Nutrition Labels** summarizing sandbox tier, body-access consent, egress hosts, secret slots, and SBOM/Sigstore provenance at a glance.*
- **`FEAT-112` (DOM-19):** In-App Console Notification Center, Actionable Inbox & Per-User / Role Routing Preferences — *Adds a persistent, RBAC-scoped **In-App Notification Center** (TopBar bell icon with neutral numeric unread count badge, quick-triage `Drawer`, and full filterable `/notifications` Inbox page) paired with a **Per-User & Role-Default Routing Engine** that governs which events fan out to In-App, Email (`FEAT-113`), Telegram (`FEAT-113`), Slack/Teams/PagerDuty (`FEAT-115`), or Webhooks (`FEAT-114`) with deduplication, quiet hours, and digests.*
- **`FEAT-113` (DOM-19):** Native Email (SMTP / Transactional API) & Telegram Bot Notification Integrations — *Delivers two essential, zero-middleware outbound notification channels out of the box: (1) **Native Email** via standard `SMTP` / `STARTTLS` / `SMTPS` (compatible with corporate Postfix/Exchange air-gap relays, Gmail, Microsoft 365, and Amazon SES / Resend / Postmark transactional APIs) with clean 15-language HTML + plaintext templates; and (2) **First-Class Telegram Bot Integration** (Vault-sealed bot token, chat ID & supergroup forum `message_thread_id` support, severity filters, and 1-click interactive test ping).*
- **`FEAT-116` (DOM-19):** Turnkey Notification, Observability & Enterprise Integration Setup Templates — *Eliminates manual URL, port, TLS, and auth-header guesswork by shipping **offline-bundled, step-by-step guided setup wizards** and pre-validated endpoint/payload presets for the most common notification, observability, evaluation, and enterprise security tools — complete with Vault secret sealing and 1-click live test verification.*
- **`FEAT-143` (DOM-25):** Configuration Revision History, Side-by-Side Semantic Diff & 1-Click Resource/Workspace Rollback — *Automatically records numbered, immutable configuration revisions (`rev_1`, `rev_2`, ...) for every change across Routes, Policies, Connections, Models, Deployments, Prompts, Budgets, and MCP Pins — providing a secret-safe side-by-side diff viewer, a pre-rollback What-If impact check, and 1-click rollback in Console, CLI, and API.*
- **`FEAT-149` (DOM-26):** Tanpopo Product Update Center, Release Channel Subscription & Offline Upgrade Readiness Advisor — *Provides a built-in Update Center in the Console and CLI (`tanpopo version check`) that compares the running Tanpopo binary, database schema, bundled adapters, and Model Intelligence Pack against signed `stable`, `lts`, or `rc` release manifests (via opt-in zero-telemetry fetch or offline manifest upload) and generates tailored preflight readiness checks plus copy-paste Docker, Helm, or binary upgrade commands.*
- **`FEAT-150` (DOM-26):** Security Advisory Feed (TANPOPO-SA-*), Installed Extension Vulnerability Matching & Remediation Alerts — *Continuously (or via air-gapped `.tpbundle` import) checks the running Tanpopo Core version and every installed `.tpkg` extension against signed `TANPOPO-SA-*` security advisories and CVEs — alerting admins across In-App Inbox, Email, Telegram, Slack, or PagerDuty and offering guided 1-click upgrade, safe disable, or emergency quarantine.*
- **`FEAT-151` (DOM-26):** Multi-Instance Fleet Registry, Federated Health Inventory & Cross-Instance Configuration Drift Detection — *Lets platform teams designate any Tanpopo instance (or dedicated management node) as a Fleet Hub to oversee all their independent Tanpopo environments (`dev`, `staging`, `prod-us`, `prod-eu`, air-gap enclaves) in one Fleet Console — tracking Core versions, `config_epoch`, DB/Valkey/KMS health, unpatched `TANPOPO-SA-*` advisories, and semantic configuration drift against golden baselines while keeping single-instance setups completely untouched.*
- **`FEAT-152` (DOM-26):** Staged Environment Promotion Pipelines (dev -> staging -> prod & Air-Gap Courier Promotion Packs) — *Lets teams promote tested configurations, prompt versions, policies, Solution Packs, and `.tpkg` lockfiles step-by-step across `dev -> staging -> prod-canary -> prod` (either across environments in one cluster or across separate Fleet instances, including signed `.tppromote` courier files for air-gapped production) with automated What-If impact checks and Four-Eyes approval gates.*
- **`FEAT-153` (DOM-26):** Fleet-Wide Emergency Broadcast (Global Advisory Pinning, Coordinated Extension Quarantine & Policy Baselines) — *Gives central CISOs and Platform SREs a Four-Eyes-guarded emergency broadcast switch to quarantine a compromised `.tpkg` extension, block an unsafe model/provider endpoint, or enforce a mandatory minimum security policy floor across every enrolled Tanpopo instance simultaneously — plus a signed `.tprevoke` emergency file for air-gapped sites.*

### 2.2 Substantially Expanded Product Features (`EXPANDED`)
- **`FEAT-014` (DOM-02, from CAP-013):** Complete Provider Connection Lifecycle, Org/Project Sharing & Endpoint Drain/Retirement
- **`FEAT-021` (DOM-03, from CAP-020):** Complete Logical Model & Deployment Lifecycle (Edit, Versioned Rates, Deprecate & Retire)
- **`FEAT-077` (DOM-12, from CAP-085):** Complete MCP Server & Tool-Grant Lifecycle, Persistent Inventory & Official Registry Browser
- **`FEAT-084` (DOM-13, from CAP-093):** Complete A2A Agent Registry Lifecycle, Runtime Activation Controls & Console Status Visibility
- **`FEAT-104` (DOM-17, from CAP-115):** Safe Declarative Extension-Contributed Console UI (Pages, Dashboard Widgets, Reports, Localized Settings & Contextual Actions)
- **`FEAT-107` (DOM-18, from CAP-119):** Complete Extension Installation Lifecycle (Delta-P Consent, Canary Rings, Non-Incident Disable & Lockfile Rollback)
- **`FEAT-110` (DOM-18, from CAP-123):** Direct Private Extension Sources (Git Repositories, Local .tpkg Archives, Private OCI Registries & Enterprise Mirrors)
- **`FEAT-120` (DOM-20, from CAP-131):** Unified Console/API/CLI Lifecycle Parity, Safe Deprecation, Drain & Resource Retirement Workflows

### 2.3 Consolidated Feature Pairs (`CONSOLIDATED` — Eliminating Redundant Splitting)
- **`FEAT-083` (DOM-13, merging CAP-091, CAP-092):** Standard Linux Foundation A2A v1.0.1 Interoperability, Signed Agent Cards & True Streaming Relay
- **`FEAT-106` (DOM-18, merging CAP-118, CAP-121):** Marketplace Publisher Onboarding, Domain Verification, Automated 7-Stage Validation & Package Lifecycle Portal
- **`FEAT-114` (DOM-19, merging CAP-125, CAP-126):** Signed Outbound Event Webhooks, Delivery History Inspector & Dead-Letter Replay

### 2.4 Seventeen `CAP-*` Items Promoted to Internal Enablers (`CAT-ARCH`) or Foundational Gates (`CAT-FOUND`)
- **Promoted to `ARCH-001`..`ARCH-017` (`15` pure internal `CAP`s + `2` dual-mapped DB mechanics):** `CAP-035 -> ARCH-001`, `CAP-038 -> ARCH-002`, `CAP-042 -> ARCH-003/FND-005`, `CAP-046 -> ARCH-004`, `CAP-047 -> ARCH-005`, `CAP-048 -> ARCH-006`, `CAP-052 -> ARCH-007`, `CAP-064 -> ARCH-008`, `CAP-065 -> ARCH-009`, `CAP-078 -> FEAT-070 + ARCH-010`, `CAP-106 -> ARCH-011/FND-002`, `CAP-108 -> ARCH-012/FND-011`, `CAP-110 -> ARCH-013/FND-014`, `CAP-111 -> ARCH-014`, `CAP-117 -> ARCH-015/FND-003`, `CAP-125 -> FEAT-114 + ARCH-016/FND-009`, `CAP-142 -> ARCH-017`.
- **Promoted to `CAT-FOUND` (`2` pure housekeeping/assurance `CAP`s):** `CAP-136 -> FND-019` (while promoting `--admin-listen` into `FEAT-098`), `CAP-155 -> FND-020` (`NFR-016`).
- **Explicitly Rejected Anti-Patterns (`6` items):** `REJ-001`..`REJ-006` ([`02-MASTER-FEATURE-CATALOG.md`](./02-MASTER-FEATURE-CATALOG.md) §5).

---

## 3. Underrepresented Capability Coverage Verification (`TEEP-02A` §3)

| Required Capability Area (`TEEP-02A` §3) | Proposed Catalog Coverage (`FEAT-*` / `ARCH-*`) |
|---|---|
| **1. Tanpopo Product Update Center** | `FEAT-149` (`DOM-26` — Release channels, compatibility preflight, connected opt-in & air-gap manifest diff) |
| **2. In-App Notification Center & Preferences** | `FEAT-112` (`DOM-19` — TopBar unread bell, inbox drawer, ack/snooze, per-user & role routing/digest preferences) |
| **3. Email (`SMTP`) & Telegram Integrations** | `FEAT-113` (`DOM-19` — `SMTP`/STARTTLS/API email + Telegram Bot API with topic routing & air-gap relay support) |
| **4. Security Advisories & Extension Update Alerts** | `FEAT-150`, `FEAT-109` (`DOM-26` / `DOM-18` — `TANPOPO-SA-*`/OSV matching against Core & `tanpopo.lock`, 1-click patch/quarantine) |
| **5. Safe Extension-Contributed UI (Pages, Widgets, Reports, Settings, Actions)** | `FEAT-104` + `ARCH-018` (`DOM-17` — Host-rendered Declarative Astryx UI contributions, `0` third-party JS, `DEC-CAND-17`) |
| **6. Public Marketplace Discovery & Publisher Experience** | `FEAT-105` (Discovery & Security Nutrition Labels) + `FEAT-106` (Publisher Onboarding, Validation & Lifecycle Portal) |
| **7. Direct Private Extension Sources (Git, Local Archives, OCI)** | `FEAT-110` (`DOM-18` — Commit/tag-pinned Git repos, local `.tpkg` upload/watch, private OCI `1.1`, internal mirrors) + `FEAT-111` (`.tpbundle`) |
| **8. Configuration Revision History, Diff & Rollback** | `FEAT-143` (`DOM-25` — Immutable entity/workspace revisions, secret-safe semantic diff, What-If preview & `If-Match` rollback) |
| **9. Multi-Instance & Fleet Management** | `DOM-26`: `FEAT-151` (Fleet Registry & Drift Detection), `FEAT-152` (Staged Promotion & `.tppromote` Couriers), `FEAT-153` (Fleet Emergency Broadcast) |
| **10. Provider & Integration Setup Templates** | `FEAT-011` (`DOM-02` — 16+ Provider/GPU Presets) + `FEAT-116` (`DOM-19` — SMTP, Telegram, Slack, Teams, PagerDuty, OTLP, SIEM Presets) |
| **11. Complete Lifecycle & Retirement Workflows** | `FEAT-120` (`DOM-20` cross-entity `Draft -> Active -> Deprecated -> Draining -> Disabled -> Retired -> Deleted` parity) + `FEAT-014`, `021`, `066`, `067`, `077`, `084`, `107` |

---

## 4. Complete `CAP-001` .. `CAP-161` Forward Traceability Matrix

| `CAP` ID | `TEEP-01` Domain & Title | `TEEP-02A` Target ID(s) | Target Class(es) |
|---|---|---|---|
| `CAP-001` | `DOM-01` — Unified OpenAI /v1/chat/completions & Streaming Inference Plane | `FEAT-001` | `CAT-PROD` |
| `CAP-002` | `DOM-01` — Top-Level Native Vendor SDK Endpoints (/v1/messages, /v1beta/models/*) | `FEAT-002`, `FND-013` | `CAT-PROD`, `CAT-FOUND` |
| `CAP-003` | `DOM-01` — Full Agentic /v1/responses Plane & Upward GPT-5.4+/6.2 Translation | `FEAT-003` | `CAT-PROD` |
| `CAP-004` | `DOM-01` — Full-Method & Query-Aware /native/{dialect}/* Passthrough | `FEAT-004`, `FND-013` | `CAT-PROD`, `CAT-FOUND` |
| `CAP-005` | `DOM-01` — Multi-Turn Cryptographic Reasoning Signature Continuity | `FEAT-005` | `CAT-PROD` |
| `CAP-006` | `DOM-01` — Request Payload Fingerprint Idempotency Guard | `FEAT-006` | `CAT-PROD` |
| `CAP-007` | `DOM-01` — Clean SSE Mid-Stream Error Framing & Usage Accuracy Trailers | `FEAT-007` | `CAT-PROD` |
| `CAP-008` | `DOM-01` — Specialized /v1/fim/completions, /v1/decisions & /v1/moderations Surfaces | `FEAT-008` | `CAT-PROD` |
| `CAP-009` | `DOM-02` — Hardened Official Tier-1 & Tier-2 Provider Adapters | `FEAT-009`, `FND-013` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-010` | `DOM-02` — Expanded Cloud, Sovereign & Self-Hosted GPU Adapter Suite | `FEAT-010` | `CAT-ECO` |
| `CAP-011` | `DOM-02` — Blind Two-Phase Connection Probing & Actionable Failure Diagnostics | `FEAT-012`, `FND-016` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-012` | `DOM-02` — Full Multi-Mode Connection Auth (AWS SigV4, Entra ID, mTLS, OAuth 2.1, Outbound WIF) | `FEAT-013`, `FND-013` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-013` | `DOM-02` — Complete Provider Connection Lifecycle, Org/Project Sharing & /v1 URL Normalizer | `FEAT-014` | `CAT-ADMIN` |
| `CAP-014` | `DOM-02` — In-Place Vault Secret Rotation, OCI_REGISTRY_AUTH & Consumer Dependency Tracking | `FEAT-015` | `CAT-ADMIN` |
| `CAP-015` | `DOM-02` — Provisioned Throughput (PTU / Reserved Tier) Capacity Modeling & Spillover | `FEAT-016` | `CAT-ADMIN` |
| `CAP-016` | `DOM-03` — Canonical RateCard v3 (nano_usd_per_1m_tokens Exact Precision & Dual-Unit \$/1M UX) | `FEAT-017` | `CAT-ADMIN` |
| `CAP-017` | `DOM-03` — Ed25519/TUF-Signed Offline Model Intelligence Packs & Decimal-Sanitized Sync | `FEAT-018` | `CAT-ADMIN` |
| `CAP-018` | `DOM-03` — BlindBroker Account-Authenticated Model Discovery & 1-Click Bulk Onboarding | `FEAT-019` | `CAT-ADMIN` |
| `CAP-019` | `DOM-03` — 4-Layer Precedence Engine, Sticky Manual Overrides & Provenance Tracking | `FEAT-020` | `CAT-ADMIN` |
| `CAP-020` | `DOM-03` — Complete Logical Model & Deployment Rate-Card CRUD / Retirement Lifecycle | `FEAT-021` | `CAT-ADMIN` |
| `CAP-021` | `DOM-03` — Automated Model Deprecation Monitoring & Capability-Verified Migration Advisor | `FEAT-022` | `CAT-ADMIN` |
| `CAP-022` | `DOM-03` — Model Weights License & Jurisdictional Compliance Metadata Gate | `FEAT-023` | `CAT-ADMIN` |
| `CAP-023` | `DOM-04` — Deterministic 6-Step Route Resolver, SWRR & Monotonic Residency Narrowing | `FEAT-024` | `CAT-PROD` |
| `CAP-024` | `DOM-04` — Dual-Mode Prefix / KV-Cache Affinity Router (Consistent Hash with Bounded Loads) | `FEAT-025` | `CAT-PROD` |
| `CAP-025` | `DOM-04` — TTFT / TPOT SLO-Aware & Multi-Objective Dynamic Scoring | `FEAT-026` | `CAT-PROD` |
| `CAP-026` | `DOM-04` — Conditional CEL/Header Routing, Canary Splits & Shadow Mirroring | `FEAT-027` | `CAT-ADMIN` |
| `CAP-027` | `DOM-04` — Sandboxed Custom ROUTING_STRATEGY Wasm Scorer Plugins | `FEAT-028` | `CAT-ECO` |
| `CAP-028` | `DOM-04` — Live Explain == Select Equivalence, Regional UI Controls & Tiered Capability Warnings | `FEAT-029` | `CAT-ADMIN` |
| `CAP-029` | `DOM-04` — Hybrid On-Prem GPU (llm-d / vLLM) to Sovereign Cloud Burst Orchestration | `FEAT-030` | `CAT-ADMIN` |
| `CAP-030` | `DOM-04` — Automatic Cross-Dialect Request Adaptation Under Strict CT-08 | `FEAT-031` | `CAT-PROD` |
| `CAP-031` | `DOM-05` — Cluster-Shared SM-04 Circuit Breakers & Semantic Inference Stall Detection | `FEAT-032` | `CAT-ADMIN` |
| `CAP-032` | `DOM-05` — Wired Per-Route, TTFT, Inter-Chunk & X-Tanpopo-Timeout-Ms Deadlines | `FEAT-033` | `CAT-PROD` |
| `CAP-033` | `DOM-05` — Hierarchical Aggregate Tenant Bulkheads & Gradient2 Priority Load Shedding | `FEAT-034` | `CAT-ADMIN` |
| `CAP-034` | `DOM-05` — FinOps-Safe Pre-First-Token Speculative Request Hedging | `FEAT-035` | `CAT-PROD` |
| `CAP-035` | `DOM-05` — Valkey-Accelerated Idempotency-Key Coordination | `ARCH-001` | `CAT-ARCH` |
| `CAP-036` | `DOM-05` — Zero-Drop Rolling Upgrade preStop Readiness Fencing & Active SSE Drain | `FEAT-036` | `CAT-ADMIN` |
| `CAP-037` | `DOM-06` — Zero-Float u128 Two-Phase Reserve / DeltaReserve / Settle Kernel & Reaper | `FEAT-037` | `CAT-PROD` |
| `CAP-038` | `DOM-06` — Hierarchical Escrow-Backed Local Pod Budget Leases (~30 ns Hot Path, 0 Overspend) | `ARCH-002` | `CAT-ARCH` |
| `CAP-039` | `DOM-06` — 7-Tier Budget Scopes (Enterprise/BU/Team/User), Delete Lifecycle & Burn Forecasting | `FEAT-038` | `CAT-ADMIN` |
| `CAP-040` | `DOM-06` — Automatic Period Rollover Worker, Date-Range UI & Unified Half-Open Attribution | `FEAT-039`, `FND-005` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-041` | `DOM-06` — Multi-Tier Conditional Rate Evaluation (1h Cache, Context Cliffs, Service Tiers, GPU) | `FEAT-040` | `CAT-ADMIN` |
| `CAP-042` | `DOM-06` — Wired Streaming / Hourly usage_rollups_hourly Aggregator | `ARCH-003`, `FND-005` | `CAT-ARCH`, `CAT-FOUND` |
| `CAP-043` | `DOM-06` — Mandatory Cost-Center Tagging, Internal Platform Markup Bps & FOCUS 1.1 Export | `FEAT-041` | `CAT-ADMIN` |
| `CAP-044` | `DOM-06` — Cryptographic RFC 8785 JCS FinOps Settlement & Chargeback Invoice Receipts | `FEAT-042` | `CAT-ADMIN` |
| `CAP-045` | `DOM-06` — Out-of-Band Cloud Provider Billing Reconciliation Connectors | `FEAT-043` | `CAT-ADMIN` |
| `CAP-046` | `DOM-07` — Epoch-Fenced SSRF-Safe Pooled Keep-Alive & HTTP/2 Upstream Transports | `ARCH-004` | `CAT-ARCH` |
| `CAP-047` | `DOM-07` — Split SQLite Read/Write Pools & 5 ms Group-Commit WAL / pgx.CopyFrom Batcher | `ARCH-005` | `CAT-ARCH` |
| `CAP-048` | `DOM-07` — Incremental ConfigSnapshot Epoch Delta Reloads | `ARCH-006`, `FND-002` | `CAT-ARCH`, `CAT-FOUND` |
| `CAP-049` | `DOM-07` — Two-Tier (RAM + Valkey) Project-DEK Exact Cache, X-Tanpopo-No-Cache & Policy UI | `FEAT-044` | `CAT-PROD` |
| `CAP-050` | `DOM-07` — Cryptographically Tenant-Prefixed Semantic Cache (CACHE_STRATEGY) | `FEAT-045` | `CAT-PROD` |
| `CAP-051` | `DOM-07` — Automatic Upstream Prompt-Cache Breakpoint & Prefix Canonicalizer | `FEAT-046` | `CAT-PROD` |
| `CAP-052` | `DOM-07` — Zero-Allocation Hot-Path Arena & Slice Pooling | `ARCH-007` | `CAT-ARCH` |
| `CAP-053` | `DOM-08` — Privacy-First 12-Stage Request Waterfall Metadata Tracing | `FEAT-047` | `CAT-ADMIN` |
| `CAP-054` | `DOM-08` — Server-Side Multi-Dimension Trace Search, Filtering & Metadata Export | `FEAT-048` | `CAT-ADMIN` |
| `CAP-055` | `DOM-08` — Authenticated Bounded-Cardinality Prometheus /metrics & Scrape CLI Flags | `FEAT-049` | `CAT-ADMIN` |
| `CAP-056` | `DOM-08` — Guarded OTLP GenAI (semantic-conventions-genai) Exporter & CIDR Flag Parity | `FEAT-050` | `CAT-ADMIN` |
| `CAP-057` | `DOM-08` — Wired ttl.Cleaner, Instant PostgreSQL Partition Drop & Retention Settings API/UI | `FEAT-051` | `CAT-ADMIN` |
| `CAP-058` | `DOM-08` — Asynchronous Analytical Warehouse Offload (ClickHouse / S3 Parquet / Iceberg) | `FEAT-052` | `CAT-ECO` |
| `CAP-059` | `DOM-08` — Real-Time Authenticated 'tanpopo traces tail' & Live Anomaly Stream | `FEAT-053` | `CAT-ADMIN` |
| `CAP-060` | `DOM-09` — Wired Multi-Stage Guardrail Engine (PRE, STREAM_WINDOW, POST, MCP, A2A) + Speculative Input Rails | `FEAT-054` | `CAT-ADMIN` |
| `CAP-061` | `DOM-09` — Built-In Pure-Go/Wasm Deterministic PII, Secret & GlassWorm Invisible-Unicode Guardrails | `FEAT-055` | `CAT-ADMIN` |
| `CAP-062` | `DOM-09` — Tier-3 / External Neural Safety Connectors (ONNX GLiNER, NeMo, Bedrock, Azure, ModelArmor, Lakera) | `FEAT-056` | `CAT-ECO` |
| `CAP-063` | `DOM-09` — Indirect Tool-Output Injection Scanner & Vendor Remote-MCP Bypass Firewall | `FEAT-057` | `CAT-ADMIN` |
| `CAP-064` | `DOM-09` — BlindHostCredentialBroker Edge Injection, DEC-004 Cookie Ban & Stream Secret Scrubber | `ARCH-008` | `CAT-ARCH` |
| `CAP-065` | `DOM-09` — Connect-Time SSRFGuardDialer IP Pinning, IMDS Block & Socket Rejection Telemetry | `ARCH-009`, `FND-016` | `CAT-ARCH`, `CAT-FOUND` |
| `CAP-066` | `DOM-09` — Single-Transaction O(1) 4-Tier Kill-Switch, Global Quarantine & Direct Valkey Broadcast | `FEAT-058`, `FND-008` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-067` | `DOM-09` — 3-Tier Envelope Vault, Wired Cloud KMS BYOK & RLS-Safe KEK Rotation/Retirement | `FEAT-059`, `FND-001`, `FND-007` | `CAT-ADMIN`, `CAT-FOUND`, `CAT-FOUND` |
| `CAP-068` | `DOM-10` — Persistent HA Admin Sessions, WebAuthn/TOTP MFA, O(1) Login Directory & SYSTEM Operator Decoupling | `FEAT-060`, `FND-002` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-069` | `DOM-10` — Multi-Tenant OIDC & Hardened Pure-Go SAML 2.0 Enterprise SSO | `FEAT-061` | `CAT-ADMIN` |
| `CAP-070` | `DOM-10` — SCIM 2.0 Automated Provisioning & <5s Cascading Offboarding Kill-Chain | `FEAT-062` | `CAT-ADMIN` |
| `CAP-071` | `DOM-10` — Two-Stage Policy-as-Code (Native + cedar-go ABAC) & Policy Delete / Reset-to-Parent | `FEAT-063` | `CAT-ADMIN` |
| `CAP-072` | `DOM-10` — Secretless Inbound Workload Identity Federation (K8s SA, SPIFFE/SPIRE, Cloud/CI OIDC) & /v1/sts/token | `FEAT-064` | `CAT-PROD` |
| `CAP-073` | `DOM-10` — Scoped Admin Service Accounts (sak_tp_*) & Personal Access Tokens (pat_tp_*) | `FEAT-065`, `FND-012` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-074` | `DOM-10` — 7-Tier Organizational Hierarchy (enterprises + org_units), Delegated RBAC & Full Member Lifecycle | `FEAT-066` | `CAT-ADMIN` |
| `CAP-075` | `DOM-10` — Complete Virtual Key Lifecycle, CIDR Allowlists, Attribution Tags & Explicit Plane Scoping | `FEAT-067` | `CAT-ADMIN` |
| `CAP-076` | `DOM-11` — End-to-End Four-Eyes Dual Approval Workflow, Console UI & O(1) Self-Approval Guard | `FEAT-068` | `CAT-ADMIN` |
| `CAP-077` | `DOM-11` — Strict Regional Data-Residency, HIPAA BAA & Upstream ZDR Routing Fences | `FEAT-069` | `CAT-ADMIN` |
| `CAP-078` | `DOM-11` — O(1) audit_chain_heads, Monthly audit_partition_seals, External Witness & SYSTEM/ORG UI | `FEAT-070`, `ARCH-010`, `FND-002` | `CAT-ADMIN`, `CAT-ARCH`, `CAT-FOUND` |
| `CAP-079` | `DOM-11` — EU AI Act (Art. 12/14/19/50) Provenance Headers & High-Risk Logging Profile | `FEAT-071` | `CAT-ADMIN` |
| `CAP-080` | `DOM-11` — Automated Signed Compliance Evidence Packs (SOC 2, HIPAA, ISO/IEC 42001, EU AI Act) | `FEAT-072` | `CAT-ADMIN` |
| `CAP-081` | `DOM-11` — Automated Upstream Commercial ToS (DEC-004) & Open-Weights License Firewall | `FEAT-073` | `CAT-ADMIN` |
| `CAP-082` | `DOM-11` — Immediate-Fence Tenant Crypto-Shredding & Background Erasure Finalizer | `FEAT-074`, `FND-007` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-083` | `DOM-12` — Dual-Stack MCP Broker (2025-03-26 Stateful + 2026-07-28 Stateless HTTP), Resources/Prompts & Schema Cache | `FEAT-075` | `CAT-PROD` |
| `CAP-084` | `DOM-12` — JCS Tool Schema + outputSchema Pinning & Semantic Rug-Pull Drift Classifier | `FEAT-076` | `CAT-ADMIN` |
| `CAP-085` | `DOM-12` — Complete MCP Server, Persistent Tool Inventory, Grant Revocation & Official Registry Sync | `FEAT-077` | `CAT-ADMIN` |
| `CAP-086` | `DOM-12` — Synchronous + Async SM-06 HITL Tool Approvals & Native io.modelcontextprotocol/tasks / MRTR | `FEAT-078` | `CAT-ADMIN` |
| `CAP-087` | `DOM-12` — Zero-Tax Per-User RFC 9728 / RFC 8707 / RFC 8693 OBO MCP Tool Authorization | `FEAT-079` | `CAT-PROD` |
| `CAP-088` | `DOM-12` — Host-Governed Progressive MCP Tool Discovery & Anthropic defer_loading Passthrough | `FEAT-080` | `CAT-PROD` |
| `CAP-089` | `DOM-12` — Declarative OpenAPI / GraphQL-to-MCP TOOL_CONNECTOR Virtualization | `FEAT-081` | `CAT-ECO` |
| `CAP-090` | `DOM-12` — Supervised Container/Sidecar SIDECAR_STDIO Bridge & PlaneMCP Per-Tool FinOps Metering | `FEAT-082` | `CAT-ADMIN` |
| `CAP-091` | `DOM-13` — Linux Foundation A2A v1.0.1 Standard Wire Interop & Signed Agent Cards | `FEAT-083` | `CAT-PROD` |
| `CAP-092` | `DOM-13` — Streaming Upstream A2A Relay (ExecuteStream) & Event-Driven Valkey SSE Fan-Out | `FEAT-083` | `CAT-PROD` |
| `CAP-093` | `DOM-13` — Complete A2A Agent Registry CRUD, --a2a-enabled Flag & Console Runtime Banner | `FEAT-084` | `CAT-ADMIN` |
| `CAP-094` | `DOM-13` — Monotonic SM-09 Sub-Budget Carving, hop_count <= 5 Cycle Guard & Cancel Propagation | `FEAT-085` | `CAT-PROD` |
| `CAP-095` | `DOM-13` — SSRF-Guarded A2A Push Notification Webhook Delivery | `FEAT-086` | `CAT-PROD` |
| `CAP-096` | `DOM-13` — Cross-Plane (LLM + MCP + A2A) Causal Governance Graph, Nested 'act' OBO & 1-Click Swarm Kill-Switch | `FEAT-087` | `CAT-ADMIN` |
| `CAP-097` | `DOM-14` — Versioned, Project-DEK-Sealed Prompt Registry & Environment Aliasing | `FEAT-088` | `CAT-PROD` |
| `CAP-098` | `DOM-14` — Mandatory Hierarchical Governance System Preambles (CONTEXT_TRANSFORMER) | `FEAT-089` | `CAT-ADMIN` |
| `CAP-099` | `DOM-14` — GitOps Prompt Template Sync & Side-by-Side Playground Diffing | `FEAT-090` | `CAT-PROD` |
| `CAP-100` | `DOM-15` — Gateway-Native Deterministic Regression & Contract Cassette Runner ('tanpopo eval run') | `FEAT-091` | `CAT-PROD` |
| `CAP-101` | `DOM-15` — Budget-Capped Asynchronous 1% Shadow Canary Evaluation Engine | `FEAT-092` | `CAT-PROD` |
| `CAP-102` | `DOM-15` — Online Structural Quality Scoring & Standard OTel gen_ai.evaluation.result Events | `FEAT-093` | `CAT-PROD` |
| `CAP-103` | `DOM-15` — Ephemeral CI OIDC Workload Gate for External Eval & Red-Team Suites | `FEAT-094` | `CAT-ECO` |
| `CAP-104` | `DOM-16` — True Cluster Node Tombstones, loaded_kek_ids_json, Valkey/OTLP Health & DP Quorum Alerts | `FEAT-095`, `FND-014` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-105` | `DOM-16` — PostgreSQL HA Parity Across All Break-Glass CLI Commands & Verified PG PITR Runbooks | `FEAT-096` | `CAT-ADMIN` |
| `CAP-106` | `DOM-16` — ARCH-OBS-01 7-Index PostgreSQL Pack & SECURITY DEFINER O(k_affected) Cross-Org Lookups | `ARCH-011`, `FND-002` | `CAT-ARCH`, `CAT-FOUND` |
| `CAP-107` | `DOM-16` — Production Helm Chart, Multi-Arch Distroless Images & Optional FIPS 140-3 Build Profile | `FEAT-097` | `CAT-ADMIN` |
| `CAP-108` | `DOM-16` — Dual-Engine SQLite <-> PostgreSQL Schema-Width Parity Gate & Rolling Skew Preflight | `ARCH-012`, `FND-011` | `CAT-ARCH`, `CAT-FOUND` |
| `CAP-109` | `DOM-16` — Wired HTTP Admin Diagnostics (smoke-test, support-bundle, openapi.json) & Two-Way OpenAPI CI Gate | `FEAT-099`, `FND-006` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-110` | `DOM-16` — Automated Valkey Shard Promotion & DR-02 valkey_hold_checkpoints Reconciliation Proof | `ARCH-013`, `FND-014` | `CAT-ARCH`, `CAT-FOUND` |
| `CAP-111` | `DOM-17` — Zero-Ambient-Authority Tier-2 wazero Sandbox (16 MiB, 50ms, DRR Queues, FaultGuard) | `ARCH-014` | `CAT-ARCH` |
| `CAP-112` | `DOM-17` — Additive runtime.proto v1.1 Host Execution Across All 11 .tpkg Extension Categories | `FEAT-100` | `CAT-ECO` |
| `CAP-113` | `DOM-17` — Supervised Tier-3 ConnectRPC Sidecar Lifecycle (0600 UDS / SPIFFE mTLS + OS Isolation) | `FEAT-101` | `CAT-ECO` |
| `CAP-114` | `DOM-17` — Official Polyglot Guest SDKs (Go, Rust, TypeScript) & Optional Build-Time WIT Lowering | `FEAT-102` | `CAT-ECO` |
| `CAP-115` | `DOM-17` — Declarative JSON Schema + uiSchema Localized Extension Configuration Forms | `FEAT-104` | `CAT-ECO` |
| `CAP-116` | `DOM-17` — Offline Extension Contract, ABI Fuzz & Memory/CPU Ceiling Harness ('tanpopo plugin test --fuzz') | `FEAT-103` | `CAT-ECO` |
| `CAP-117` | `DOM-17` — Unified Cross-OS Deterministic .tpkg Packer, Merkle Tree Signing & Hermetic Wasm Build | `ARCH-015`, `FND-003` | `CAT-ARCH`, `CAT-FOUND` |
| `CAP-118` | `DOM-18` — Keyless OIDC Sigstore (Fulcio + Rekor) Publisher Onboarding & DNS Namespace Verification | `FEAT-106` | `CAT-ECO` |
| `CAP-119` | `DOM-18` — Staged Delta-P Consent, Canary Rings, Graceful Disable, Browser Upload & <5s Target Rollback | `FEAT-107` | `CAT-ADMIN` |
| `CAP-120` | `DOM-18` — 5-Tier Extension Trust Governance & Console/CLI org_extension_policies Controls | `FEAT-108`, `FND-010` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-121` | `DOM-18` — 7-Stage Automated Marketplace Moderation, Wasm Import Whitelist & DEC-004 CI Gate | `FEAT-106` | `CAT-ECO` |
| `CAP-122` | `DOM-18` — TUF (go-tuf/v2) Anti-Freeze / Anti-Downgrade Metadata & Monotonic revocations.json Kill-Switch | `FEAT-109` | `CAT-ADMIN` |
| `CAP-123` | `DOM-18` — OCI 1.1 Artifact Distribution (oras-go/v2 + SBOM/Sigstore Referrers) for Harbor/ECR Mirrors | `FEAT-110` | `CAT-ADMIN` |
| `CAP-124` | `DOM-18` — Dual-Engine (SQLite + PostgreSQL) Air-Gap .tpbundle Export / Verify / Import | `FEAT-111` | `CAT-ADMIN` |
| `CAP-125` | `DOM-19` — Decoupled Async Webhook Outbox Fan-Out, Tenant Pending Index & ORG_OWNER Access Restoration | `FEAT-114`, `ARCH-016`, `FND-009` | `CAT-ADMIN`, `CAT-ARCH`, `CAT-FOUND` |
| `CAP-126` | `DOM-19` — Full Webhook Subscription CRUD, Delivery History / DLQ Inspector, Replay & Expanded Event Taxonomy | `FEAT-114` | `CAT-ADMIN` |
| `CAP-127` | `DOM-19` — Native NOTIFICATION_SINK Connectors (Slack, MS Teams, PagerDuty, Jira, ServiceNow) with Verified HITL Callbacks | `FEAT-115` | `CAT-ADMIN` |
| `CAP-128` | `DOM-19` — Enterprise SIEM / SOAR & FinOps Ecosystem Connectors (Splunk HEC, Sentinel, Datadog, Snowflake) | `FEAT-117` | `CAT-ECO` |
| `CAP-129` | `DOM-19` — Verified Developer IDE & Coding-Agent Profiles (Claude Code, Cursor, Cline, Zed, GitHub Actions) | `FEAT-118` | `CAT-PROD` |
| `CAP-130` | `DOM-20` — Self-Activating <90s Guided /setup & 'tanpopo init' + Deterministic 'doctor --interactive' | `FEAT-119`, `FND-010` | `CAT-PROD`, `CAT-FOUND` |
| `CAP-131` | `DOM-20` — 100% Admin OpenAPI <-> Web Console Parity & Elimination of Create-Only Resource Traps | `FEAT-120`, `FND-006` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-132` | `DOM-20` — Astryx Neutral Enterprise Shell, Cmd+K Palette, Scope Persistence & 403-Safe UX | `FEAT-121` | `CAT-PROD` |
| `CAP-133` | `DOM-20` — 15-Language Offline Localization, DTO Enum Translation & AST i18n Lint Gate | `FEAT-122` | `CAT-PROD` |
| `CAP-134` | `DOM-20` — Complete Remote Read/Write CLI, 'tanpopo login' & Full 41-Var TANPOPO_* Flag/Docs Parity | `FEAT-123`, `FND-012` | `CAT-ADMIN`, `CAT-FOUND` |
| `CAP-135` | `DOM-20` — Multi-Dialect Studio Playground, SDK Snippets & Immediate Ephemeral Key Revocation | `FEAT-124` | `CAT-PROD` |
| `CAP-136` | `DOM-20` — Obsolete / Ghost Configuration, Unused Font Dependency & Stale Governance Banner Cleanup | `FND-019` | `CAT-FOUND` |
| `CAP-137` | `DOM-21` — Durable Async Batch (/v1/batches, /v1/messages/batches) & Background Responses BATCH_ESCROW Ledger | `FEAT-125` | `CAT-PROD` |
| `CAP-138` | `DOM-21` — Connection-Pinned, DLP-Scanned /v1/files & Resumable Upload Broker | `FEAT-126` | `CAT-PROD` |
| `CAP-139` | `DOM-21` — Realtime BiDi Voice/Audio Gateway (/v1/realtime, BidiGenerateContent) & Constrained Ephemeral Tokens | `FEAT-127` | `CAT-PROD` |
| `CAP-140` | `DOM-21` — Unified & Native Image, Video, Speech & Transcription Endpoints with Multimodal Metering | `FEAT-128` | `CAT-PROD` |
| `CAP-141` | `DOM-21` — Per-Page Metered Document OCR & Structured Extraction Plane (/v1/ocr) | `FEAT-129` | `CAT-PROD` |
| `CAP-142` | `DOM-21` — Host blob.Store Zero-Copy Streaming (<= 32 MiB) & Length-Prefixed Binary AWS EventStream Framing | `ARCH-017` | `CAT-ARCH` |
| `CAP-143` | `DOM-22` — Gateway-Encrypted Stateful Thread Hydration (previous_response_id, /v1/conversations) & Strict CT-08 Reject | `FEAT-130` | `CAT-PROD` |
| `CAP-144` | `DOM-22` — Pre-Flight Token Counting (/count_tokens, /input_tokens) & Context Compaction (/compact) | `FEAT-131` | `CAT-PROD` |
| `CAP-145` | `DOM-22` — Vector-Space-Locked Embedding & Rerank Governance (Unified Gemini/Vertex/Cohere v2 + 'rerank' Allowlist Fix) | `FEAT-132` | `CAT-PROD` |
| `CAP-146` | `DOM-22` — BlindBroker-Mediated Internal Vector / RAG Enrichment Subrequests (CONTEXT_TRANSFORMER) | `FEAT-133` | `CAT-ECO` |
| `CAP-147` | `DOM-23` — Recipient-Key-Encrypted (RFC 9180 HPKE) Failure Flight Recorder & Wired Debug Capture Sessions | `FEAT-134` | `CAT-ADMIN` |
| `CAP-148` | `DOM-23` — GDPR Art. 17 Per-Subject HMAC Salt-Shredding Pseudonymization (subject_privacy_keys) | `FEAT-135` | `CAT-ADMIN` |
| `CAP-149` | `DOM-23` — Ephemeral In-RAM Reversible PII Session Tokenization & SSE Rehydration | `FEAT-136` | `CAT-PROD` |
| `CAP-150` | `DOM-23` — Dual-Custody HPKE WORM Legal-Hold Compliance Archiver (SEC 17a-4 / FINRA) | `FEAT-137` | `CAT-ADMIN` |
| `CAP-151` | `DOM-23` — Sovereign Cell-Based Multi-Region Active-Active Data Planes & Regional Quota Envelopes | `FEAT-138` | `CAT-ADMIN` |
| `CAP-152` | `DOM-24` — Metadata-Only Historical Traffic Replay ('What-If Engine') | `FEAT-139` | `CAT-ADMIN` |
| `CAP-153` | `DOM-24` — 8-Layer Live Dependency Topology & Pre-Mutation SPOF Cut-Set Simulator | `FEAT-140` | `CAT-ADMIN` |
| `CAP-154` | `DOM-24` — Built-In Hermetic Loopback Chaos & DORA Resilience Drill Studio | `FEAT-141` | `CAT-ADMIN` |
| `CAP-155` | `DOM-24` — Continuous >=4h Protocol/Wasm Fuzzing & Unsigned/Dual-Backend E2E Parity Gate | `FND-020` | `CAT-FOUND` |
| `CAP-156` | `DOM-24` — Automated Security Quarantine Forensic Timeline & Blast-Radius Exporter | `FEAT-142` | `CAT-ADMIN` |
| `CAP-157` | `DOM-25` — Database-Reconciling Declarative GitOps ('tanpopo config export \| diff \| plan \| apply') | `FEAT-144` | `CAT-ADMIN` |
| `CAP-158` | `DOM-25` — Official OpenTofu & Terraform Provider (terraform-provider-tanpopo) | `FEAT-145` | `CAT-ECO` |
| `CAP-159` | `DOM-25` — Optional Kubernetes Operator & CRD Controller (tanpopo-operator) | `FEAT-146` | `CAT-ECO` |
| `CAP-160` | `DOM-25` — Portable Signed SOLUTION_PACK Declarative Blueprints ('tanpopo pack plan \| apply') | `FEAT-147` | `CAT-ECO` |
| `CAP-161` | `DOM-25` — CI/CD Pull-Request What-If & FinOps/Policy Impact Gate Action | `FEAT-148` | `CAT-ECO` |
