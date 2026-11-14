# Authentication Contracts, Isolated Credential Vault & Connection Compliance Specification — Tanpopo

> **Architecture & Security Specification Document (ASTRA Phase A04 — `DRAFT / ILLUSTRATIVE CANDIDATE SPEC`)**
> - **Document ID:** `SPEC-EXT-03` | **Version:** `0.1.0-step04-draft` | **Last Updated:** `2026-11-11`
> - **Authoring Role:** Platform Architect & Security-Minded SDK Designer
> - **Standardization Status Statement:** The `Auth Driver` interfaces, OAuth refresh flows, and vault record structures below are **illustrative candidate designs (`ILLUSTRATIVE — PRE-ADR`)**; concrete storage cryptography primitives will be finalized alongside the Core language and KMS ADR decisions (`OQ-001`, `TP-FR-0206`).
> - **Requirements Traceability:** `DEC-004`, `REJ-003`, `TP-FR-0203`..`0206`, `TP-FR-0303`, `TP-FR-0703`, `TP-BR-0102`, `TP-BR-0301`..`0304`, `TP-BR-0402`, `TP-AC-SEC-01`, `03`, `04`, `06`.

---

## 1. Core Security Invariants of the Credential Vault (`Credential Vault Invariants`)

1. **Zero Full-Vault Access (`Zero Full-Vault Access`):** No `Extension Package`, `Provider Adapter`, `Auth Driver`, `MCP Server`, or UI administrator possesses an API to enumerate or dump the secret vault. Every credential access operation is hard-scoped to the four-tuple `(organization_id, connection_id, secret_slot, allowed_audience_hosts[])`.
2. **Write-Only Plaintext (`Write-Only Plaintext`):** Raw secret values enter strictly one-way via the Admin API/CLI and are sealed using authenticated encryption (`AES-256-GCM` or `XChaCha20-Poly1305` with a per-record Data Encryption Key `DEK` wrapped by a Master Key Encryption Key `KEK`) before touching persistent storage. Administrative responses return only the opaque `secret_ref` handle, validity status, and an irreversible fingerprint.
3. **No Token Cross-Audience (`No Token Cross-Audience`):** A token or credential bound to `Connection A` (or `MCP Server X`) **must never** be attached to requests targeting any host outside that connection's `allowed_audience_hosts`, must never follow cross-origin HTTP `301/302/307/308` redirects, and must never be forwarded across the `Provider` ↔ `MCP` ↔ `A2A` plane boundaries (`TP-AC-SEC-04`).

---

## 2. Five Canonical Authentication Contracts & `Custom Auth Driver` Boundaries

| Authentication Method | Safe Execution Mechanism in Tanpopo | Valid Usage Conditions | Mandatory Security Boundaries |
|---|---|---|---|
| **1. `API Key / Bearer / Static Header`** | **`host_blind_inject`**: The plugin references only `secretSlot: "api_key"`; the **Host Egress Broker** injects the credential header at the network edge immediately prior to writing to the TLS socket. | Commercial pay-as-you-go API keys or internal service keys. | Plugin code (Tier 1/2/3) never observes the raw secret string in memory (`TP-BR-0402`). |
| **2. `Authorized OAuth 2.1 / OIDC`** *(Auth Code + PKCE `S256` & Client Credentials)* | **Core OAuth Broker** owns the entire code exchange, seals `refresh_token` ciphertext, validates anti-CSRF `state`, and enforces RFC 8707 `resource` indicators. | Upstream vendor publishes official OAuth documentation and issues legitimate `client_id` credentials for third-party / enterprise integrations. | Rejects activation on scope mismatch; serializes token refresh via `Single-Flight` locking and never exposes `refresh_token` to Plugins. |
| **3. `OAuth 2.0 Device Flow`** *(RFC 8628 — Conditional)* | Core surfaces `verification_uri` + `user_code` in CLI/Console for headless servers and polls strictly honoring `interval` and `slow_down` signals. | **Enabled exclusively when the target vendor officially documents and authorizes Device Flow for third-party integrations.** | Strictly forbids embedding `client_id` values scraped from closed first-party apps; pending device codes self-destruct cleanly after `expires_in` (`<= 15 minutes`). |
| **4. `Workload Identity & mTLS`** *(OIDC Token Exchange / IAM / Client Cert)* | **Host TLS & Cloud Broker** performs ambient OIDC token exchange (AWS STS, GCP Workload Identity, Azure Managed Identity) or `mTLS` client-certificate handshakes. | Enterprise cloud environments or high-security internal AI gateways. | `mTLS` private keys remain anchored inside Host/HSM memory and never cross IPC/Wasm boundaries into a Plugin. |
| **5. `Bounded Custom Auth Driver`** *(Enterprise Dynamic Signing)* | **Preferred `host_crypto_sign`:** Guest constructs `canonical_string_to_sign` and asks Host to compute HMAC/RSA signatures.<br/>**Exception `scoped_secret_unseal`:** Unseals solely 1 dedicated `secretSlot` for that specific connection when a proprietary signing cipher is absent in Host. | Corporate internal AI gateways or regional sovereign clouds using custom request-signing schemes. | `scoped_secret_unseal` raises a mandatory red `SECURITY_HIGH_RISK` warning during permission consent (`WF-04`), remains hard-locked to `allowedHosts`, and emits an `AuditEvent` on every unseal. |

---

## 3. Connection Ownership (`Local vs Remote Ownership`) & Refresh / Revocation Flows

### 3.1. Three Ownership Scopes for `Provider Connection` & `MCP Connection`
1. **Organization-Shared Service Connection (`Org-Shared Service Connection`):** Provisioned by an `Organization Owner` using corporate commercial credentials and granted one-way *invocation* access to selected `Project` scopes (`TP-BR-0102`); project members can neither read nor export the underlying secret.
2. **Project-Owned Connection (`Project-Owned Connection`):** Provisioned by a `Team Admin` using a department-specific key; completely isolated from sibling `Project` scopes within the same `Organization`.
3. **Per-Principal Delegated Connection (`Per-Principal Delegated Connection` — especially on MCP / authorized personal OAuth planes):** When a connection uses `Authorized OAuth` acting on behalf of an individual engineer's identity (e.g., reading Jira/GitHub via MCP), the token is cryptographically bound to `(principal_id, connection_id)`. **User B's Virtual Key can never borrow User A's personal OAuth session**, eliminating horizontal privilege escalation (`Confused Deputy`).

### 3.2. Illustrative Sequence Diagram: `Single-Flight` Refresh & Revocation Isolation (`SM-02` / `SM-03`)

```mermaid
sequenceDiagram
    participant C1 as API Client #1..#50 (Concurrent)
    participant DP as Tanpopo Data Plane
    participant Vault as Core Token Vault & Single-Flight Lock
    participant IdP as Authorized External OAuth Server
    actor Admin as Org Owner / Team Admin

    C1->>DP: 50 concurrent requests targeting Connection C_OAuth (Token near/at expiry)
    DP->>Vault: Request valid Access Token handle for C_OAuth
    Vault->>Vault: Acquire per-connection_id Single-Flight lock (1 leader refreshes, 49 waiters bounded <= 10s)
    Vault->>IdP: POST /oauth/token (grant_type=refresh_token, authorized client_id, resource=allowed_host)
    alt Refresh succeeds
        IdP-->>Vault: 200 OK (new_access_token + rotated_refresh_token)
        Vault->>Vault: AEAD-seal & atomically commit rotated token pair (SM-03 -> TOKEN_VALID)
        Vault-->>DP: Return edge-injection handle; all 50 concurrent requests proceed seamlessly
    else Token revoked upstream (400 invalid_grant / 401)
        IdP-->>Vault: 400 invalid_grant
        Vault->>Vault: Transition SM-03 -> REAUTH_REQUIRED & SM-02 -> AUTH_EXPIRED_REAUTH_REQUIRED
        Vault-->>Admin: Emit operational alert + write sanitized AuditEvent (zero raw token material)
        DP-->>C1: Fail over to backup Connection (if configured) or return 503 upstream_auth_unavailable (credential details redacted!)
    end
```

---

## 4. `Official` / `Community` / `Private` Boundaries & Consumer Subscription Risk Case Studies

### 4.1. Origin-Based Distribution and Execution Policy (`DEC-004`, `TP-BR-0201`..`0202`)

| Tier | Maintaining Entity | Distribution Channel | Permitted Authentication Standards | Tanpopo Runtime Posture |
|---|---|---|---|---|
| **`Official Adapters`** | Tanpopo core project team | Bundled or Official Marketplace (signed by Tanpopo root key). | **Exclusively official commercial APIs, Cloud IAM/OIDC, or publicly documented vendor-authorized OAuth programs.** | Labeled `official`; full `VERIFIED` contract suite. |
| **`Verified Community`** | Partners / community developers | Official Marketplace (after passing compliance & contract review). | Exclusively access methods compliant with upstream Terms of Service (`TP-BR-0302`). | Labeled `verified`; still strictly confined inside the default-deny Sandbox with explicit network consent. |
| **`Private Enterprise`** | Internal AI Platform team of a self-hosted organization | Private OCI/Git Registry or archive signed by the enterprise's own internal key. | Customizable for internal corporate gateways (internal SSO, mTLS, custom gateway HMAC). | Labeled `unverified-external` (with internal `Org-Approved` badge when enterprise signature verifies); runs in isolated Sandbox/Sidecar. |
| **`Unverified Community`** | Any third party on the Internet | Public Git repository / URL / operator-supplied archive. | Technically neutral runtime execution, **zero legal or ToS endorsement**. | Permanently badged `unverified-external` in UI & `AuditEvent` logs; blocked by default under strict organizational policies (`TP-BR-0202`). |

### 4.2. `Risk Case Studies`: Third-Party Consumer Subscription Connections

> **Explicit Governance Statement (`CONFIRMED_BY_OWNER` — `DEC-004` / `REJ-003`):** Flat-rate consumer subscription plans such as **`ChatGPT Plus/Pro`**, **`Codex` (personal consumer plan)**, **`Claude Pro/Max`**, and **`Google Antigravity`** are referenced below **strictly as legal, operational, and security `Risk Case Studies`** — they are **NOT** supported, endorsed, or guaranteed integrations in Tanpopo.

| Risk Dimension | Empirical Analysis Across Personal Consumer Subscriptions | Hard Containment Rule in Tanpopo Architecture |
|---|---|---|
| **1. Legal & Terms Boundary (`Entitlement Gap`)** | Personal subscriptions are flat-rate priced for interactive human use inside official first-party apps; vendor Terms of Service prohibit extracting interactive login sessions to power automated API endpoints or multi-user pooling. | **No "click-through" ToS bypass (`TP-BR-0301`):** Tanpopo strictly forbids designing *"I accept the risk — unlock Plus/Pro consumer account as API"* toggles. |
| **2. Production Outage & Mass Account Ban Risk** | Vendors may mutate undocumented internal web payloads without notice or permanently ban violating accounts without refund, causing immediate outages for dependent workloads. | SDK documentation, Setup Wizards, and product messaging explicitly position Tanpopo exclusively for commercial API keys, self-hosted models, and officially authorized connections. |
| **3. Full Personal Account Exposure (`Session Blast Radius`)** | Unlike scoped API keys, scraped browser session cookies or closed-client tokens typically grant full read/write access to personal chat history, uploaded files, and billing settings. | **Zero anti-bot / session-scraping tooling:** Tanpopo **never** implements browser cookie harvesters, TLS/browser fingerprint spoofers, residential proxy rotators, or CAPTCHA / anti-bot solvers (Cloudflare Turnstile, reCAPTCHA, etc.). |
| **4. Open-Source Brand Contamination Risk** | Unofficial GitHub plugins frequently adopt naming that falsely implies endorsement by the upstream gateway project. | Marketplace moderation rejects `100%` of consumer-subscription bypass packages (`TP-AC-SEC-06`), reserves the `@tanpopo/*` namespace, and enforces an automated `Revocation Feed`. |

---

## 5. Open Provider Policy Dependencies (`Open Policy Unknowns`)

1. **`POL-01` — Per-Vendor Third-Party OAuth Licensing Programs:** Commercial AI vendors currently vary widely in whether third-party applications may use *"Sign in with Provider"* OAuth flows to invoke inference on a user's behalf (some expose public OAuth programs with dedicated quotas, some restrict OAuth to contracted partners, and others prohibit it entirely in favor of API Keys). Accordingly, Tanpopo ships only the **standards-compliant `Authorized OAuth 2.1` technical mechanism**, while enabling vendor-specific OAuth presets in any `Official Adapter` requires active, publicly documented vendor authorization at release time.
2. **`POL-02` — Dynamic OAuth Authorization Standards on the MCP Plane:** The MCP ecosystem's authorization specification (spanning `Dynamic Client Registration` vs `Client ID Metadata Documents` and `RFC 8707 Resource Indicators`) continues to evolve rapidly; Phase A04 keeps the authorization server metadata layer pluggable so `v0.2` aligns cleanly with the finalized upstream standard.
