# Unresolved Gaps Register, Proposed Change Requests (`CR`) & Closure Criteria — Tanpopo

> **Post-Audit Remediation Governance Document (Phase ASTRA A08 — Companion to [`integrated-audit.md`](./integrated-audit.md))**
> - **Document ID:** `REV-GAP-01` | **Version:** `0.1.0-step08` | **Date:** `2026-11-14`
> - **Authoring Role:** Independent Product / Architecture / Security Reviewer
> - **Non-Negotiable Governance Principle:** The independent auditor **never unilaterally alters product scope, technology stack, contract schemas, or security policies**. Every architectural, scope, or policy modification is packaged as an explicit **Change Request (`CR-01` .. `CR-10`)** for **Product Owner** decision, paired with measurable **Closure Criteria** for every unresolved gap.

---

## Part A — Ten Proposed Change Requests & ADR Amendments (`CR-01` .. `CR-10` — For Product Owner Review)

### `CR-01` — Lock the `Official Reference Adapters` Roster for `MVP (v0.1)` & Decide `/v1/rerank`
- **Linked Findings:** `AUD-007`, `AUD-023` | **Impact:** `Gate G1 / G2` scope (`srs.md`, `release-scope.md`, `decisions.md`).
- **Problem:** Existing documents conflict across `2–3`, `5`, `5–6`, and `6–8` Official Adapters in `v0.1`; notably, `Bedrock` is referenced in `v0.1` while depending on AWS IAM/SigV4 (`TP-FR-0206`, currently scheduled in `v0.3`). Additionally, `rerank` appears in the `Charter` but is absent from `SRS`.
- **Options for Product Owner Decision:**
  - **Option A *(Recommended)*:** Hard-lock exactly **5 Official Reference Adapters in `MVP (v0.1)`** using standard static API-key/header or local authentication: **(1) `OpenAI`, (2) `Anthropic`, (3) `Google Gemini`, (4) `OpenAI-Compatible (vLLM / Groq / Together / DeepSeek)`, and (5) `Ollama`** (implementing `OpenAI`, `OpenAI-Compatible`, and `Ollama` as Tier-1 Declarative packs, and `Anthropic` plus `Gemini` as Tier-2 Wasm adapters to dogfood both tiers in production!). Schedule **`AWS Bedrock` and `Azure OpenAI` (requiring SigV4 / Entra ID)** in **`v0.2`**, and add `/v1/rerank` as a `Should` requirement in `v0.2`.
  - **Option B:** Pull the `HOST_AWS_SIGV4` signing primitive forward from `v0.3` into `v0.1` to ship `AWS Bedrock` in `MVP (v0.1)` (6 Adapters), accepting ~1 additional week of `v0.1` engineering scope.
- **Closure Criteria:** Reconcile identical counts and vendor names across `decisions.md`, `release-scope.md`, and `srs.md` (`TP-FR-0301`/`0302`), and add an explicit Official Adapters acceptance scenario to `acceptance-criteria.md`.

---

### `CR-02` — Resolve the `2-Tier` (G1/G2/SRS) vs. `3-Tier Isolated Sidecar` (`ADR-0002`) Runtime Split
- **Linked Findings:** `AUD-008` | **Impact:** `SD-G1-02`, `TD-G2-04`, `srs.md`, `release-scope.md`, `ADR-0002`.
- **Problem:** Approving `SD-G1-02` (2-tier) alongside `ADR-0002` (3-tier) as currently written creates a direct contradiction, and `Tier-3 Sidecar` has zero `TP-FR-*` requirements or release milestones.
- **Options for Product Owner Decision:**
  - **Option A *(Recommended)*:** Confirm the **3-tier target architecture (`ADR-0002`)** while **explicitly phasing delivery in `SRS` & `release-scope.md`**: `v0.1` builds only **`Tier-1 Declarative` (`TP-FR-0301`) + `Tier-2 Wasm Sandbox` (`TP-FR-0302`)**; add a new requirement **`TP-FR-0308 (Could — v0.3): Tier-3 Isolated Sidecar Runtime over ConnectRPC UDS/mTLS`** (shared across `stdio` MCP bridges and enterprise HSM integrations).
  - **Option B:** Cut `Tier-3 Sidecar` entirely from pre-`v1.0` scope to preserve a strictly 2-tier lightweight runtime (requiring operators to wrap any `stdio` MCP servers as external `Streamable HTTP` services outside Tanpopo).
- **Closure Criteria:** Align `SD-G1-02`, `TD-G2-04`, `ADR-0002`, `srs.md`, and `release-scope.md`; add an IPC verification `SPIKE` if `Tier-3` is retained.

---

### `CR-03` — Unify Provenance `Trust Tier` Taxonomy (`3` vs. `4` Tiers), Enum Casing & Reserved Namespaces
- **Linked Findings:** `AUD-012`, `AUD-013` | **Impact:** `TP-BR-0201`, `data-model.md`, `routing-and-policy.md`, `SPEC-UX-01/04`, `SPEC-SEC-02`.
- **Problem:** `TP-BR-0201`'s 3-label invariant (`official`, `verified`, `unverified-external` + internal signature boolean) conflicts with the 4-value `origin_trust_tier` column (`PRIVATE_ENTERPRISE`), while reserved namespace rules drift between `@tanpopo/*` and `@tanpopo-official/*`.
- **Options for Product Owner Decision:**
  - **Option A *(Recommended — Preserves `TP-BR-0201`)*:** Retain exactly **3 public `origin_trust_tier` values** (`official`, `verified`, `unverified_external`) and add independent columns **`enterprise_signed BOOLEAN NOT NULL DEFAULT FALSE` + `signing_key_fingerprint VARCHAR(64)`** on `extension_versions` (allowing `min_plugin_trust` policies of `OFFICIAL_ONLY | VERIFIED_OR_ENTERPRISE_SIGNED | VERIFIED_ONLY | ANY_APPROVED`).
  - **Option B:** Formally amend `TP-BR-0201`, `glossary.md`, and `TP-FR-0304` from 3 labels to an **explicit 4-tier hierarchy** (`official > verified > private_enterprise > unverified_external`) across all documents and DB columns.
  - *Mandatory across both A & B:* Enforce the reserved namespace regex **`^@tanpopo(-[a-z0-9]+)?/`** (blocking spoofing of both `@tanpopo/*` and `@tanpopo-official/*`).
- **Closure Criteria:** Single canonical enum matching `100%` across `TP-BR-0201`, SQL `CHECK (...)` constraints, `tanpopo.lock` JSON Schema, and UI `Token` badges.

---

### `CR-04` — Lock the `RBAC` Role Set, Per-`Organization` Plugin Enablement Table & `v0.1` Exit Gate Alignment
- **Linked Findings:** `AUD-007`, `AUD-014` | **Impact:** `data-model.md`, `release-scope.md`, `release-readiness.md`, `contract-test-matrix.md`.
- **Proposed Remediation:**
  1. **`RBAC` Roles:** Lock `role_bindings.role_name` to **6 canonical roles** (`OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`; `Personal Admin` mode automatically grants `OPERATOR + ORG_OWNER` to the bootstrap owner).
  2. **Organization-Scoped Plugin Enablement (`TP-BR-0202`):** Add `org_extension_policies (org_id, package_id, enabled, pinned_installation_id, version)` and change `provider_adapters` uniqueness to `UNIQUE (installation_id, adapter_slug)` so concurrent versions coexist cleanly during rolling upgrades.
  3. **Unblock `MVP (v0.1)` Release Gates:**
     - Expose `POST /admin/v1/organizations` (restricted to `OPERATOR`) and CLI org creation in `v0.1` so automated multi-tenant isolation suite `TP-AC-SEC-07` runs `100%` live in `v0.1`, while rich multi-org UI switching remains scheduled in `v0.3`/`v1.0`.
     - Scope `Air-Gap` bundle drills in `GATE-REL-06` and `< 5s` cluster revocation in `TP-AC-N04` to their respective delivery milestones (`v0.3`/`v1.0`), while `v0.1` verifies `< 1s` single-node revocation and zero-external-CDN offline Web Console rendering (`UX-AC-10`).
- **Closure Criteria:** `MVP (v0.1)` exit criteria are `100%` self-consistent with zero dependencies on `v0.3`/`v1.0` features.

---

### `CR-05` — Redesign `ExtensionRuntimeService` (`runtime.proto`), Out-of-Band Multimodal Blob Handles & Blind Broker Hardening
- **Linked Findings:** `AUD-001`, `AUD-002`, `AUD-016`, `AUD-021` | **Impact:** `SPEC-API-01`, `SPEC-EXT-01/02/03`, `SPEC-CORE-02`, `SPEC-SEC-01/02`.
- **Proposed Technical Specification (amending `ADR-0002` & `SPEC-API-01`):**
  1. **Compile-Clean `runtime.proto` (`0 errors`):** Define `ExtractUsageInput`, add **`rpc TransformResponse(TransformResponseInput) returns (NormalizedInferenceResponse)`** for non-streaming/embedding responses, add **`rpc InspectNativeRequest(NativeRequestInput) returns (NativeRoutingHints)`** for `/native/{dialect}/*`, and split `ValidateConnection` into a 2-phase protocol (`BuildValidationProbe -> UpstreamHttpSpec` executed by Host via `Blind Broker` → `VerifyValidationResult`) so Guests never open network sockets.
  2. **Keep Multimodal Blobs Outside `16 MiB` Wasm Linear Memory (`Out-of-Band Blob Handles`):** When requests carry large base64 images/audio/PDFs, Host replaces raw base64 payloads with opaque `blob_ref://req_.../part_0` handles before passing compact metadata JSON (`< 256 KB`) into Wasm `TransformRequest`, then splices streams directly on outbound egress; simultaneously split pre-hold token estimation into text (`utf8_bytes / 3`) plus per-image/per-second/per-page multimodal schedules!
  3. **Complete `HostCryptoSignSpec` & Eliminate `scoped_secret_unseal` in Tier-1/2 Wasm:** Add structured Host signing (`HMAC-SHA256`, `AWS_SIGV4`, `RSA-SHA256` with `canonical_payload` and `output_encoding`) to `CredentialInjectInstruction`; hard-ban outbound `Cookie` / `Set-Cookie` header injection in `Blind Broker` (`DEC-004`); and **eliminate `scoped_secret_unseal` completely from Tier 1 & Tier 2** (if an enterprise requires proprietary closed signing libraries, it runs strictly inside an `OPERATOR`-managed `Tier-3 Isolated Sidecar`), preserving `100%` of the *Zero Raw Secrets in Wasm Memory* invariant.
- **Closure Criteria:** Extracted `.proto` passes `buf lint` / `protoc`, and `NormalizedInferenceRequestEnvelope` covers `100%` of `TP-FR-0101`..`0105` fields.

---

### `CR-06` — Harden Physical Data Schema (`ARCH-DATA-01`): 8 Missing Tables, `Project DEK` Envelope Hierarchy & Partitioned Audit Chains
- **Linked Findings:** `AUD-003`, `AUD-004`, `AUD-010`, `AUD-026`, `AUD-029` | **Impact:** `data-model.md`, `secrets-and-tenant-isolation.md`, `backups-recovery.md`, `ADR-0003`, `ADR-0005`.
- **Proposed Technical Specification:**
  1. **Author Missing SQL DDL:** `config_changelog (epoch BIGSERIAL PRIMARY KEY, ...)`, `distributed_leases`, `usage_rollups_hourly`, `oauth_authorization_sessions` (persisting PKCE/state/Device Flow with `expires_at`), `connection_secret_bindings (connection_id, slot_name, credential_id)` (multi-slot credentials), `extension_revocations`, and (at their respective milestones) `a2a_tasks` + `webhook_endpoints`.
  2. **Fix SQL Nullability & Multi-Tenant Relational Constraints:** Make `extension_installations.active_version_id` nullable during `PENDING_PERMISSION_APPROVAL`; make `approval_requests.org_id` / `project_id` nullable for `Instance`-scoped actions; add a partial unique index `WHERE org_id IS NULL` on `principals`; enforce composite `(org_id, id)` keys across all tenant child tables with reference `ALTER TABLE ... ENABLE / FORCE ROW LEVEL SECURITY` DDL; standardize SQLite timestamps to **`BIGINT epoch_ms`**.
  3. **Unify Cryptography & Make `Project`-Level `Crypto-Shredding` Mathematically Exact:**
     - Lock **1 canonical `AEAD AAD` formula** matching real columns: `"tanpopo:v1:cred:" || org_id || ":" || credential_id || ":" || secret_kind`.
     - Lock `Virtual Key` hashing to **plain `SHA-256` over 256-bit CSPRNG keys** (decoupled from `Master KEK` so `KEK` rotation never invalidates virtual keys).
     - Add **`project_encryption_keys (project_id, org_id, wrapped_project_dek)`** (wrapped by `Org DEK`) to encrypt `Project`-scoped secrets and `debug_payload_blobs` (adding `project_id`) — making `< 60s` `Project`-level `Crypto-Shredding` (`OPS-AC-09`) **`100%` mathematically sound even across historical cold backups**!
  4. **Prevent Duplicate Ledger Settlements & HA Audit Hash-Chain Forks:**
     - Add `UNIQUE (request_id, settled_at)` paired with atomic `UPDATE budget_reservations SET state = 'SETTLED_...' WHERE id = ? AND state IN ('RESERVED_HOLD', 'STREAMING_ACCRUING')` inside the same transaction, plus `media_usage_json` and `applied_pricing_snapshot_json` on `usage_settlements`.
     - Partition `audit_events` hash chains **per `org_id` (plus a dedicated `SYSTEM` chain for `org_id IS NULL`) using `seq_no BIGINT` over `RFC 8785 JCS(metadata)`**: enabling `ORG_OWNER` self-verification under `FORCE RLS` without exposing `Org B` rows, and surviving `SQLite <-> Postgres` migrations intact!
- **Closure Criteria:** SQL DDL executes cleanly on both `SQLite` and `PostgreSQL 16` with zero orphaned table references or cryptographic mismatches.

---

### `CR-07` — Lock `Valkey Cluster` `{org_id}` Hash-Tag Keyspace & Cache-Loss Budget Hold Reconciliation
- **Linked Findings:** `AUD-004` | **Impact:** `deployment-topology.md`, `usage-billing-observability.md`, `ADR-0003`, `ADR-0005`.
- **Proposed Technical Specification:**
  1. Mandate Redis Cluster **`Hash-Tag` syntax `tp:v1:{<org_id>}:...`** across all `Valkey/Redis` keys (e.g., `tp:v1:{org_01jc...}:budget:prj_01jc...`, `tp:v1:{org_01jc...}:rl:vk_01jc...`). Wrapping `{<org_id>}` guarantees that the entire `Org -> Project -> Env -> Virtual Key` hierarchy for any tenant hashes to **1 exact Redis Cluster slot**, eliminating `CROSSSLOT` errors in atomic Lua scripts!
  2. To prevent budget cap overruns on abrupt `Valkey` failure: each Data-Plane node periodically checkpoints its aggregate in-flight holds (`active_node_hold_micros`) with a `5s` heartbeat into a lightweight PostgreSQL table; during fallback to direct PostgreSQL reservation, Core adds active cluster hold margins before admitting new requests.
- **Closure Criteria:** Document full Valkey keyspace schema in `ARCH-OPS-01` and validate in `SPIKE-03`.

---

### `CR-08` — Redesign Standard-IDE-Compatible `destructive` MCP Approval Flow & Split `SM-09` for `A2A Task`
- **Linked Findings:** `AUD-006` | **Impact:** `mcp-a2a-boundaries.md`, `business-entity-map.md`, `api-contracts.md`, `user-flows.md`.
- **Proposed Technical Specification:**
  1. **`100%` Compatibility with `Claude Code`, `Cursor`, and `Codex` on `POST /mcp`:** Rather than returning HTTP `202` + requiring a non-standard `X-Tanpopo-Approval-Token` HTTP header (which breaks off-the-shelf MCP clients), asynchronous approval returns a **standard MCP HTTP `200` JSON-RPC result** with `isError: true` containing structured instructions with `approval_request_id` plus a built-in system tool `tanpopo__check_approval_and_resume(approval_request_id)` (or `_meta.tanpopoApprovalToken` once approved) — while binding the parameter hash to `(org_id, virtual_key_id, server_slug, tool_name, JCS(arguments))` (never an empty `principal_id`).
  2. **Dedicated `SM-09 (A2A Task Lifecycle)`:** Keep `SM-06` dedicated to `ApprovalRequest`, and introduce `SM-09` mapping 1-to-1 with canonical `Agent2Agent` states (`submitted -> working -> input-required -> completed | failed | canceled`) over a single canonical wire binding (JSON-RPC 2.0 over HTTP/SSE).
- **Closure Criteria:** Verified via `EXP-05` against unmodified standard MCP clients requiring zero client-side patches.

---

### `CR-09` — Add Self-Hosted Break-Glass Essentials & Decide `Webhooks` / `Response Cache` Phasing
- **Linked Findings:** `AUD-023`, `AUD-024` | **Impact:** `srs.md`, `release-scope.md`, `provider-adapter.md`, `installer-upgrades.md`, `data-model.md`.
- **Options for Product Owner Decision:**
  1. **Add Immediately to `Must (v0.1)` (Lightweight survival essentials for self-hosters):**
     - Host/container-exec break-glass recovery CLI: **`tanpopo admin reset-local-owner`** (recovers locked-out local admin passwords or broken OIDC configs).
     - Automatic `stream_options: {"include_usage": true}` injection in the OpenAI-Compatible adapter whenever `stream: true` to maximize `EXACT` ledger settlement.
  2. **Explicit Phasing for `Webhooks` and `Response Cache`:**
     - *Budget / MCP Approval Webhooks:* Formally schedule `webhook_subscriptions` + HMAC-signed delivery worker in **`v0.2` (`Should`)** (`v0.1` surfaces alerts via Console `Banner` and OTel logs/metrics).
     - *Response Cache (`Exact/Semantic Cache`):* Select **Option A (Recommended):** Formally record `Response Cache` as **`Later / Could (v0.3+)`** in `srs.md` (omitted from `v0.1–v0.2` to prevent scope creep, keeping `FMEA-03`/`CT-28` as conditional activation gates in `v0.3+`); or **Option B:** Ship a minimal `Org DEK`-encrypted `Exact Match Cache` in `v0.2`.
- **Closure Criteria:** Zero features appear in tests or runbooks without a corresponding `TP-FR-*` requirement and schema definition.

---

### `CR-10` — Complete `Admin OpenAPI`, `Playground` Session Flow, Day-One Local `Ollama`, Single CLI Tree & `Air-Gap` Font Bundling
- **Linked Findings:** `AUD-005`, `AUD-009`, `AUD-017`, `AUD-018`, `AUD-022` | **Impact:** `api-contracts.md`, `screen-specifications.md`, `design-system-integration.md`, `developer-experience.md`, `ADR-0001`, `ADR-0004`.
- **Proposed Technical Specification:**
  1. **Single `tanpopo` Binary:** Standardize `tanpopo` as the sole binary (`tanpopo serve --role=...`, with an optional `tanpopod -> tanpopo` package symlink in `.deb`/container images for Linux service ergonomics) and consolidate `admin`, `upgrade`, and `incident` subcommands into `SPEC-OPS-04`.
  2. **Unblock Day-One Local `Ollama` in `UJ-01` (`TTFGR < 3m`):** In `Setup Wizard` (`SCR-01`) and `SCR-04`, when an `OPERATOR` / `Personal Admin` enters a local model URL and explicitly checks `Allow Private/Loopback Upstream`, Control Plane persists `provider_connections.allow_private_ip = TRUE` (while still unconditionally blocking `169.254.169.254` and Tanpopo's own admin listener ports!), and the Wizard atomically provisions `Model Deployment (HEALTHY)` + `Default Route` in a single transaction.
  3. **Standardize `Playground` (`SCR-15`):** Web Console calls `POST /admin/v1/playground/sessions` (enforcing RBAC + CSRF) to mint a short-lived (`TTL = 15 min`), micro-capped ephemeral token, then exercises the exact standard Data-Plane hot path identical to external SDKs.
  4. **`Air-Gap` `Figtree` Font Bundling Passing `astryx doctor`:** Keep `astryx-themes.js` `100%` unmodified (satisfying `astryx doctor`'s `theme-module` check) and configure a `vite.config.ts` resolve alias mapping `@astryxdesign/theme-neutral/fonts.css` to a local `@font-face` stylesheet referencing static `Figtree` `.woff2` assets under `web/console/public/fonts/`, while mapping icons strictly to `@astryxdesign/core/Icon`'s 29 built-in semantic icons (or recording an MIT-clean icon package such as `@phosphor-icons/react` / `lucide-react` in `ADR-0004`).
- **Closure Criteria:** `100%` OpenAPI schema coverage for `SCR-01..16`, clean `tsc --noEmit`, `astryx doctor --built` reporting `0 fail`, and `0` failed network requests inside an air-gapped browser session.

---

## Part B — Mandatory Pre-Code Gap Closure Tracker (`Unresolved Gaps Closure Tracker`)

| Gap ID | Linked Audit / CR | Severity | Blocked Milestone | Verifiable Closure Criteria | Status |
|---|---|---|---|---|---|
| **`GAP-01`** | `AUD-011` / Gates `G1–G3` | **`CRITICAL`** | **Blocks all `v0.1` & PoC code** | Product Owner records decisions on `SD-G1-01..05`, `TD-G2-01..06`, `ADR-0001..0006`, `CR-01..10`, and authorizes `SPIKE-01..04` in [`decisions.md`](../governance/decisions.md). | `OPEN — AWAITING OWNER` |
| **`GAP-02`** | `AUD-001` / `CR-05` | **`CRITICAL`** | **Blocks `v0.1` Core & SDK** | `runtime.proto` defines `ExtractUsageInput`, `TransformResponse`, `InspectNativeRequest`, 2-phase `ValidateConnection`, and `HostCryptoSignSpec`; compiles under `protoc` with `0` errors. | `OPEN` |
| **`GAP-03`** | `AUD-002` / `CR-05` | **`CRITICAL`** | **Blocks `v0.1` Gateway & Wasm** | `NormalizedInferenceRequestEnvelope` includes tool/JSON/sampling fields; out-of-band `blob_ref` streaming keeps large blobs outside `16 MiB` Wasm RAM; multimodal token estimation fixed. | `OPEN` |
| **`GAP-04`** | `AUD-003` / `CR-06` | **`CRITICAL`** | **Blocks `v0.1` DB Migration** | `data-model.md` adds `config_changelog`, `distributed_leases`, `usage_rollups_hourly`, `oauth_authorization_sessions`, `connection_secret_bindings`, `org_extension_policies`, and `extension_revocations`; fixes `NOT NULL` bugs on `extension_installations`/`approval_requests`; enforces `(org_id, id)` composite keys + `FORCE RLS`. | `OPEN` |
| **`GAP-05`** | `AUD-010` / `CR-06` | **`CRITICAL`** | **Blocks `v0.1` Vault, Backup & Audit** | Reconciles 1 canonical `AEAD AAD` formula, plain `SHA-256` `Virtual Key` hashing, `project_encryption_keys` for project-level `Crypto-Shredding`, `DR-03` cache-refresh sequencing, and per-`org_id` + `seq_no` + `JCS` `audit_events` hash chains. | `OPEN` |
| **`GAP-06`** | `AUD-004` / `CR-06`, `07` | **`CRITICAL`** | **Blocks `v0.1` Ledger & `v0.3` HA** | Adds idempotency uniqueness on `usage_settlements`, multimodal/pricing-snapshot columns, Valkey `{org_id}` hash-tag keyspace preventing `CROSSSLOT`, and Valkey-loss hold checkpointing. | `OPEN` |
| **`GAP-07`** | `AUD-005` / `CR-10` | **`CRITICAL`** | **Blocks `v0.1` Admin API & UI** | `api-contracts.md` defines complete paths and schemas for `/v1/responses`, `/v1/embeddings`, `/v1/models`, and all `51` `/admin/v1/*` endpoints powering `SCR-01..16` (including `Playground`). | `OPEN` |
| **`GAP-08`** | `AUD-006` / `CR-08` | **`CRITICAL`** | **Blocks `v0.1` Keys/Ops & `v0.2` MCP** | Fixes state machines `SM-01`, `SM-02`, `SM-04`, `SM-05` (`QUOTA_PAUSED -> REVOKED`), `SM-07` (`STREAMING_ACCRUING -> EXPIRED_RECLAIMED`), and `SM-08`; splits `SM-09` for A2A; switches MCP approval to standard JSON-RPC compatible with unmodified IDEs. | `OPEN` |
| **`GAP-09`** | `AUD-007`, `008` / `CR-01..04` | **`CRITICAL`** | **Blocks `v0.1` scope freeze** | Locks `v0.1` Official Adapters roster, `Tier-3 Sidecar` milestone, unified `Trust Tier` + `@tanpopo(-*)/*` prefix rule, 6 RBAC roles, and `v0.1` exit gates. | `OPEN — AWAITING OWNER` |
| **`GAP-10`** | `AUD-009` / `CR-10` | **`CRITICAL`** | **Blocks `v0.1` `UJ-01` Onboarding** | Unifies `allow_private_ip` rules for local `Ollama`/`vLLM`/LAN MCP and adds atomic `Deployment (HEALTHY)` + `Route` creation to Setup Wizard `SCR-01`. | `OPEN` |
| **`GAP-11`** | `AUD-011` | **`CRITICAL`** | **Blocks `v0.1` verification gate** | Authors missing `TP-AC-*` and `CT-*`/`AUTH-*` tests for `RPM/TPM/Concurrency` rate limits (`TP-FR-0702`), child-policy narrowing (`TP-FR-0701`), and cursor pagination & `409 Conflict` (`TP-FR-0901`), plus expands `traceability.md` to `1-to-1` per-requirement mapping. | `OPEN` |
| **`GAP-12`** | `AUD-017`, `022` / `CR-10` | **`MAJOR`** | **Blocks `v0.1` Web Console Build** | Specifies Vite static `Figtree` font bundling for `Air-Gap`, locks valid icon inventory, assigns `ISS-001..005` cleanup units, and fixes `offline_mode` signature rules. | `OPEN` |
| **`GAP-13`** | `AUD-015`, `018`..`021`, `023`..`030` | **`MAJOR / MINOR`** | **Blocks `Contract v0.1` freeze** | Reconciles `snake_case` capability keys, single `tanpopo` CLI tree (including `reset-local-owner` and post-`freeze-writes` unfreeze), canonical timeout/RAM constants table, unified Prometheus/JSON log identifiers, `Blind Broker` `Cookie` ban, `1024–1279px` viewport rules, and macOS/Windows + multi-browser CI runners. | `OPEN` |
