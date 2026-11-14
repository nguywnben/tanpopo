# Write-Only Secrets Management, Envelope Encryption & Multi-Tenant Isolation — Tanpopo

> **Data Security & Multi-Tenancy Specification (Phase ASTRA A07 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — NO CODE`)**
> - **Document ID:** `SPEC-SEC-03` | **Version:** `0.1.0-step07-draft` | **Last Updated:** `2026-11-12`
> - **Authoring Role:** Principal Security Engineer & Cryptography / IAM Lead
> - **Governance Status:** **Proposed Security Specification (`PROPOSED`)** — defines context-bound envelope encryption (`AEAD AAD`), write-only secret lifecycle rules, multi-layer isolation across `Organization / Project / Environment`, every-action administrative authorization, and the strict boundary between **protocol-level technical capability** and **provider policy legitimacy (`DEC-004`)**.
> - **Requirements Traceability:** `DEC-003`, `DEC-004`, `TP-BR-0101`..`0104`, `TP-BR-0301`..`0304`, `TP-BR-0802`..`0803`, `TP-FR-0204`..`0206`, `TP-FR-0501`..`0504`, `TP-FR-0701`..`0705`, `TP-NFR-0402`, `TP-NFR-0404`, `TP-NFR-0501`..`0502`, `RSK-001`, `RSK-003`, `RSK-006`, `RSK-011`, `RSK-016`.

---

## 1. Context-Bound Envelope Encryption Architecture (`Envelope Encryption with AEAD Context Binding`)

All high-sensitivity credentials (`Upstream API Keys`, `Authorized OAuth Refresh/Access Tokens`, webhook signing secrets, and short-lived debug payload captures) are protected by a two-tier key hierarchy that enables instant root key rotation while defeating cross-tenant ciphertext transplant attacks (`Ciphertext Transplant Attack`):

```mermaid
flowchart TD
    KEK["Master KEK (AWS/GCP KMS, HashiCorp Vault Transit, or 0400 Keyfile Separated from DB)"]
    DEK_A["Org-A DEK (256-bit AES-256-GCM, Wrapped by Master KEK)"]
    DEK_B["Org-B DEK (256-bit AES-256-GCM, Wrapped by Master KEK)"]
    SEC_A["Secret Ciphertext A (96-bit CSPRNG Nonce + Ciphertext + 128-bit Tag)"]
    AAD_A["Mandatory AAD = 'tanpopo:v1:' || org_id || ':' || resource_type || ':' || resource_id || ':' || slot_name"]

    KEK -->|"Unwrap on demand (Short-lived Core RAM cache)"| DEK_A
    KEK -->|"Isolated unwrap"| DEK_B
    DEK_A --> SEC_A
    AAD_A -.->|"Verify cryptographic context integrity on unseal"| SEC_A
```

### 1.1. Five Cryptographic Rules & Write-Only Secret Discipline
1. **Hard Additional Authenticated Data Binding (`AEAD AAD Binding`):**
   - Whenever sealing or unsealing any secret record via `AES-256-GCM`, the `vault` layer passes the immutable context identifier `AAD = "tanpopo:v1:" || org_id || ":" || resource_type || ":" || resource_id || ":" || slot_name`.
   - **Security Consequence:** Even if an attacker gains SQL write access and copies `secret_ciphertext` from an `Org A` `Connection` row into an `Org B` `Connection` row (or swaps rows to alter `allowed_hosts`), `GCM Open` fails immediately with an authentication tag error (`cipher: message authentication failed`) — `Org A`'s credential can never be unsealed for another resource or tenant.
2. **Write-Only Plaintext Lifecycle (`Write-Only Plaintext`):**
   - Raw secret material is transmitted exactly once during creation or rotation over TLS. Every `GET / LIST / PATCH /admin/v1/*` response, GitOps configuration export, `tanpopo support-bundle` archive, and backup exposes only `configured: true`, rotation timestamps, and `secret_version` — **no read-back API for plaintext secrets ever exists**.
3. **Blind Edge Injection & Ephemeral Buffer Hygiene (`Blind Injection & Buffer Hygiene`):**
   - The `Blind Host Credential Broker` unseals credentials strictly at the Core outbound HTTP header construction step after verifying that the destination host matches `Connection.allowed_hosts` and passes `SSRFGuard`. The plaintext byte slice is zeroed immediately (`clear(plaintextBuf)`) after writing to the outbound TLS stream and never enters Wasm guest context structures or structured logs.
4. **One-Way Hashing for Data-Plane Virtual Keys (`Virtual Key Hashing`):**
   - Client-facing virtual keys (`vk_tp_<env>_<random_256bit>`) are never reversibly encrypted; only their `SHA-256` digest and a display `key_prefix` are persisted. At inference authentication time, Core hashes the presented bearer token and compares digests via `crypto/subtle.ConstantTimeCompare` against the in-memory `ConfigSnapshot` index (eliminating `Timing Side-Channel` attacks).
5. **Zero-Downtime Online `Master KEK` Rotation (`Online KEK Re-Wrapping`):**
   - `tanpopo vault rotate-kek` unwraps and re-wraps only the small set of intermediate `Org DEK` records under the new `KEK` generation inside an atomic transaction without re-encrypting tens of thousands of individual credential ciphertexts or dropping active SSE streams (`DR-03`).

---

## 2. Four-Layer Multi-Tenant Isolation (`Four-Layer Tenant, Project & Environment Isolation`)

To eliminate cross-tenant data leakage (`Cross-Tenant IDOR` / `Cache Bleed`), Tanpopo never relies on a single application-layer `WHERE` clause, enforcing isolation simultaneously across **4 independent layers**:

| Isolation Layer | Mandatory Enforcement Mechanism | Behavior on Unauthorized Access or Coding Bug |
|---|---|---|
| **Layer 1: Immutable Principal Binding (`Auth Context Binding`)** | Data-Plane `org_id`, `project_id`, and `environment_id` are derived **exclusively from the authenticated `Virtual Key` record**; Control-Plane scope is derived from the authenticated session verified against RBAC membership tables (`org_memberships` / `project_grants`). Any client-supplied `org_id` field in a JSON request body is ignored or rejected. | Blocked at authentication middleware before reaching business logic (`AUTH-13`). |
| **Layer 2: Composite Relational Keys & `PostgreSQL FORCE RLS`** | Every tenant-scoped table enforces composite primary/foreign keys `(org_id, id)` on both SQLite and PostgreSQL (`TP-BR-0102`, preventing an `Org A` `Route` from referencing an `Org B` `Connection`). On PostgreSQL `16+`, every transaction begins with `SET LOCAL app.current_org_id = '<uuid>'` backed by `ENABLE` + **`FORCE ROW LEVEL SECURITY`** policies. | Even if a newly written SQL query omits an explicit `org_id` predicate, the database engine returns `0` rows from other organizations (`AUTH-14`). |
| **Layer 3: Partitioned Cache Keyspace & Zero Cross-Tenant Cache Sharing** | Every key in `Valkey/Redis` and local RAM caches adheres to the strict hierarchical prefix `tp:v1:{org_id}:{project_id}:{env_id}:{subsystem}:{hash}`. Whenever optional response caching (`Exact / Semantic Cache`) is enabled, lookup scope is hard-bound to **`org_id + project_id + env_id + model_deployment_id + policy_version_hash + tool_acl_hash`** — **sharing cached completions across organizations, projects, or environments is strictly prohibited**. | A caller in `Project B` can never receive a cached completion generated from `Project A`'s confidential prompt (`CT-28`, `FMEA-03`). |
| **Layer 4: Uniform Anti-Enumeration Responses (`Anti-Oracle 404`)** | When a principal in `Org A` requests `/admin/v1/connections/{org_b_id}` or queries an `Org B` `request_id`, the API returns **`404 resource_not_found`** with identical payload shape and latency to a nonexistent `UUID` (rather than `403`, which would leak existence), while silently recording `cross_tenant_probe_blocked` in the security audit log. | Attackers cannot enumerate resource UUIDs belonging to other tenants on a shared cluster (`AUTH-15`). |

---

## 3. Every-Action Administrative AuthZ, Service-to-Service Trust & Token Audience Binding

### 3.1. Plane Separation Matrix & Anti-Self-Approval (`Plane Separation & Four-Eyes`)
1. **Strict Credential Separation Between Data Plane and Control Plane:**
   - Presenting a Data-Plane `Virtual Key` (`vk_tp_...`) against any `/admin/v1/*` endpoint → Hard reject **`401 invalid_credential_plane`**.
   - Presenting a Web Console admin session cookie directly against `/v1/chat/completions` (outside the dedicated internal `/admin/v1/playground/completions` endpoint enforcing `Playground:Execute` + `CSRF` + isolated playground quota) → Hard reject **`401`**.
2. **Action-Level RBAC & Optimistic Concurrency Control (`Action-Level RBAC + Optimistic Locking`):**
   - Every handler under `/admin/v1/*` must declare an explicit `(Resource, Permission)` tuple in the central route registry (server initialization panics immediately under test if any `/admin/v1/*` route lacks a registered permission guard).
   - Cookie-backed browser sessions enforce `HttpOnly; Secure; SameSite=Strict` paired with `Origin` header validation and an anti-CSRF token (`CSRF Token`) on every `POST / PUT / PATCH / DELETE` request.
   - Every mutating configuration operation requires `If-Match: "<etag>"` to prevent lost updates across concurrent administrators (`409 conflict_version_mismatch`).
3. **Four-Eyes Separation of Duties (`OPS-02`, `TP-FR-0504`):**
   - When an organization enables `require_four_eyes_approval = true`, the database transaction layer enforces `approver.principal_id != requester.principal_id` across 4 high-risk action classes: (a) activating an `unverified-external` package or approving a capability escalation $\Delta P$, (b) enabling the `scoped_secret_unseal` exception, (c) approving a `destructive` MCP tool invocation, and (d) enabling temporary debug payload capture (`TTL <= 72h`).

### 3.2. Service-to-Service Trust & Unauthorized Token Relay Prohibition (`RFC 8707`)
- **`Control Plane <-> Data Plane` (Split-Role Topology):** Data-Plane nodes accept only HMAC/mTLS-authenticated `config_epoch` invalidation signals and query the database using a least-privilege DB role (`SELECT` on configuration tables, `INSERT` on ledger/audit tables — zero `DROP`/`ALTER` or policy-mutation privileges).
- **`Tanpopo Core <-> Tier-3 Sidecar`:** Co-located deployments use a `0600` Unix Domain Socket owned by a dedicated service user; networked cluster deployments require **mutual `mTLS`** with short-lived certificates (`TTL <= 24h`) bound to the exact extension version's `SPIFFE URI SAN`.
- **Token Audience Binding (`RFC 8707`) & Blind Token Relay Prohibition (`No Token Relay`):**
  - Administrator OIDC tokens issued for Tanpopo carry `aud = tanpopo-control-plane` — Tanpopo **never relays** this user token upstream to external AI providers or MCP servers.
  - `Authorized OAuth 2.1` tokens stored for a `Provider Connection` or `MCP Server` are hard-bound to `allowed_audience_hosts` (and `RFC 8707` `resource` indicators where supported by the authorization server); reconfiguring the upstream endpoint URL to a different host without re-authorizing OAuth from scratch immediately invalidates the stored token (`SM-03 -> INVALIDATED_HOST_CHANGE`, `CT-19`).

### 3.3. Explicit Boundary: Protocol Technical Support vs. Provider Policy Legitimacy (`DEC-004`)

A foundational requirement of Tanpopo's verification strategy is **transparent separation between protocol-level authentication capability and commercial/Terms-of-Service legitimacy**:

| Authentication Scenario | Protocol-Layer Technical Capability | Policy & Legal Legitimacy (`DEC-004`) | Tanpopo Enforcement & Verification Behavior |
|---|---|---|---|
| **1. Official commercial `API Key` / `Cloud IAM` (OpenAI Platform, Anthropic Console, Vertex AI, Bedrock)** | Full support (`Bearer`, `x-api-key`, `SigV4`, `Workload Identity`). | **Fully permitted (`COMMERCIAL_API`).** | First-class support in `Official Adapters` with exact `micro-USD` ledger accounting (`AUTH-04`, `CT-06`). |
| **2. `OAuth 2.1` for Enterprise Internal AI Gateways (Okta / Entra ID / Keycloak fronting internal vLLM / Azure / corporate gateways)** | Standard `Authorization Code + PKCE` or `Client Credentials` using an enterprise-owned `client_id`. | **Fully permitted (`SELF_HOSTED_INFRA` / `COMMERCIAL_API`).** | First-class support via the standard `Authorized OAuth 2.1` driver (`AUTH-09`, `CT-19`). |
| **3. Official AI/MCP vendor `OAuth 2.1` with a published third-party partner `client_id` program** | Standard `OAuth 2.1 + PKCE` / `Device Code` with documented public scopes. | **Permitted strictly when the vendor publishes explicit third-party API access terms (`POL-01`).** | Enabled on `Official/Verified Adapters` only with archived vendor policy documentation (`AUTH-09`). |
| **4. Hijacking web login sessions / `session cookies` / closed internal `client_id`s from personal consumer subscriptions (`ChatGPT Plus/Pro`, `Codex`, `Claude Pro/Max`, `Google Antigravity`, `Copilot Individual`, etc.)** | Even though raw HTTP merely transmits header strings in `Cookie` or `Authorization`... | **STRICTLY PROHIBITED (`REJ-003` / `DEC-004`) — Violates vendor Terms of Service.** | Core **never ships** browser cookie harvesters; DB schema constrains `billing_mode`; Marketplace verification automatically rejects offending packages (`CT-20`, `AUTH-16`); UI forbids official trust badges (`UX-GR-03`). |

---

## 4. Data Classification, Fail-Closed Geo-Residency & Instant Crypto-Shredding

1. **Four Data Classification Tiers & Default Retention:**
   - `C1 — Public`: Logical model catalog and public Marketplace package metadata.
   - `C2 — Operational Metadata`: Token counts, latency histograms, HTTP status codes, and `request_id` (retained by default; zero conversational text).
   - `C3 — Confidential Tenant Config & FinOps`: Routing tables, policies, `micro-USD` ledger entries, and hash-chained audit events (retained `>= 365 days`, protected by `RLS`).
   - `C4 — Restricted Secrets & Payload`: Authentication credentials (permanently envelope-encrypted) and prompt/completion/tool-argument payloads (**`0 bytes` stored by default**; if explicitly enabled via approved debug sampling, encrypted under `Org DEK` and hard-purged at `TTL <= 72h`).
2. **Fail-Closed Data Residency Enforcement (`TP-BR-0502`):**
   - When an `Org / Project` policy sets `allowed_data_regions = ["EU"]`, the router prunes every `Model Deployment` tagged outside `"EU"` **prior to candidate ranking and throughout failover**. If every `"EU"` deployment is unhealthy, Tanpopo immediately returns `503 no_compliant_deployment_available` rather than silently failing over to a `"US"` endpoint (`CT-08`, `EXT-ACCEPT-01`).
3. **Sub-`60-Second` Permanent Erasure via Key Destruction (`Crypto-Shredding` — `TP-NFR-0502`):**
   - Upon executing a full erasure request for an `Organization` (or a `Project` debug store), Tanpopo destroys the target `Org DEK` record from the key store, purges `tp:v1:{org_id}:*` keys from `Valkey/Redis`, and writes a signed audit certificate. Within `< 60s`, every secret ciphertext and debug payload belonging to that tenant — including copies inside immutable cold backups — becomes mathematically unrecoverable random noise (`OPS-AC-09`).
