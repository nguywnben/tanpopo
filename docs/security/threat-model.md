# Trust-Boundary Threat Model & Adversarial Matrix — Tanpopo

> **Core Security Architecture Specification (Phase ASTRA A07 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — NO CODE`)**
> - **Document ID:** `SPEC-SEC-01` | **Version:** `0.1.0-step07-draft` | **Last Updated:** `2026-11-12`
> - **Authoring Role:** Principal Security Engineer & Threat Modeling Lead
> - **Governance Status:** **Proposed Security Specification (`PROPOSED`)** — applies a *Threat Model First* methodology (`STRIDE` + `OWASP Top 10:2021/2025` + `OWASP Top 10 for LLM Applications 2025`) across [`system-design.md`](../architecture/system-design.md) (`ARCH-SYS-01`), [`extension-contract.md`](../specifications/extension-contract.md) (`SPEC-EXT-01`), and [`risk-register.md`](../discovery/risk-register.md).
> - **Requirements Traceability:** `DEC-002`..`004`, `TP-FR-0204`, `TP-FR-0301`..`0306`, `TP-FR-0401`..`0406`, `TP-FR-0701`..`0705`, `TP-NFR-0401`..`0502`, `TP-AC-SEC-01`..`08`.

---

## 1. Crown-Jewel Assets

Before defining security controls, the threat model identifies the **7 highest-value asset classes** targeted by adversaries on a self-hosted Tanpopo deployment:

| Asset ID | Security Asset | Worst-Case Impact Upon Compromise |
|---|---|---|
| **`AST-01`** | **Root Key-Encryption Key (`Master KEK`) & Tenant Keys (`Org DEKs`)** | Full decryption of every organization's credential vault across the entire instance (`P0 Catastrophic`). |
| **`AST-02`** | **Upstream Provider Credentials (`Upstream API Keys`, `Authorized OAuth Refresh/Access Tokens`, `IAM Credentials`)** | Unbounded financial quota theft at upstream AI vendors or unauthorized access to enterprise cloud infrastructure (`P0 Catastrophic`). |
| **`AST-03`** | **Core Runtime Process & Host Isolation (`Tanpopo Core Process` & Host OS)** | Remote Code Execution (`RCE`), host filesystem/environment disclosure, or lateral movement into internal corporate networks (`P0 Catastrophic`). |
| **`AST-04`** | **Tenant Inference Payloads, Multimodal Blobs & Tool Arguments (`Tenant Prompts, Completions & PII`)** | Cross-tenant trade-secret or PII disclosure (`Cross-Tenant Breach`) or `GDPR` data-residency violation (`P0 Catastrophic`). |
| **`AST-05`** | **Side-Effecting Actions Across Tool & Agent Planes (`MCP mutating/destructive Tools`, `A2A Tasks`)** | Production database deletion, unauthorized source modification, or privilege escalation via indirect `Prompt Injection` (`P0 Critical`). |
| **`AST-06`** | **Control-Plane Config Integrity, Budget Ledger & Audit Chain (`ConfigSnapshot`, `Ledger`, `audit_events`)** | Unauthorized zero-cost inference bypass, budget-ceiling breach (`Denial of Wallet`), or forensic log tampering (`P1 High`). |
| **`AST-07`** | **Supply-Chain Provenance & Legal Standing of Tanpopo (`Signing Keys`, `@tanpopo-official/*`)** | Distribution of malicious packages impersonating official releases or exposure to vendor Terms-of-Service litigation (`DEC-004` — `P0 Strategic`). |

---

## 2. Seven Trust Boundaries Diagram

Core principle from `security-and-hardening`: **Trust follows the principal that authored the data, never the transport channel carrying it into the system** (upstream LLM responses, MCP Server tool descriptions, and `.tpkg` archives loaded from local disk are all **untrusted inputs**).

```mermaid
flowchart TB
    subgraph TB1["TB-1: External Callers & Untrusted Networks (Untrusted Callers)"]
        AppClient["API Client / SDK / Agent (Virtual Key)"]
        AdminBrowser["Admin Browser (OIDC / Session + CSRF)"]
    end

    subgraph TB2["TB-2: External Extension Supply Chain (Untrusted Supply Chain)"]
        ExtSources["Marketplace / Git SHA / .tpkg-.zip Archive / Private OCI Registry"]
    end

    subgraph TB3["TB-3: Tanpopo Core Runtime (Trusted Computing Base - TCB)"]
        IngressAuth["AuthN/AuthZ, Tenant Guard, RateLimit & Budget Pre-Hold"]
        PolicyRouter["Policy Engine, Capability Router & Audit Hash-Chain"]
        BlindBroker["Blind Host Credential Broker & SSRF Connect-Time Guard"]
    end

    subgraph TB4["TB-4: Isolated Extension Execution Zone (Zero-Trust Guest Sandbox)"]
        Tier2Wasm["Tier-2: Wazero Wasm Sandbox (0 FS, 0 Env, 0 Raw Socket)"]
        Tier3Sidecar["Tier-3: Isolated Sidecar (Non-root, NetPol, Short-lived mTLS)"]
    end

    subgraph TB5["TB-5: Persistence & Key Store Layer (Restricted Persistence)"]
        DB["SQLite WAL / PostgreSQL 16+ (RLS + Ciphertext Vault)"]
        Cache["Valkey / Redis (Rate Limits, Holds, Pub/Sub Epoch)"]
        KMS["Cloud KMS / Vault / Master KEK"]
    end

    subgraph TB6["TB-6: External AI Providers & Identity Providers (Semi-Trusted Upstreams)"]
        LLMProviders["Cloud LLM APIs / Local LAN GPUs (Ollama, vLLM) / OAuth IdP"]
    end

    subgraph TB7["TB-7: External MCP Tool Servers & A2A Agents (Untrusted Tool/Agent Outputs)"]
        MCPServers["External MCP Servers (Tools & Dynamic Schemas)"]
        A2AAgents["External A2A Agents (Delegated Subtasks)"]
    end

    AppClient -->|"HTTPS /v1, /native, /mcp, /a2a"| IngressAuth
    AdminBrowser -->|"HTTPS /admin/v1 + If-Match"| IngressAuth
    ExtSources -->|"Verify SHA-256 + Cosign + Permission Sheet"| PolicyRouter
    IngressAuth --> PolicyRouter
    PolicyRouter <-->|"Sanitized payload only (0 Secrets)"| Tier2Wasm
    PolicyRouter <-->|"ConnectRPC UDS/mTLS"| Tier3Sidecar
    Tier2Wasm -->|"UpstreamHttpSpec + Slot Handle"| BlindBroker
    BlindBroker -->|"Edge secret injection + IP pinning"| LLMProviders
    BlindBroker -->|"RFC 8707 Scoped Call + Schema Hash Check"| MCPServers
    BlindBroker -->|"Attenuated Budget & Hop <= 5"| A2AAgents
    PolicyRouter <--> DB
    IngressAuth <--> Cache
    BlindBroker <--> KMS
```

---

## 3. `STRIDE` & Abuse-Case Analysis Across Thirteen Mandatory Threat Vectors (`TM-01` .. `TM-13`)

The matrix below analyzes all **13 mandatory attack vectors** of an `extension-first` AI Gateway, mapping each threat to its `STRIDE` / `OWASP LLM` classification, mandatory preventive/detective architectural controls, and verification test IDs:

| Threat ID | Concrete Attack Vector & Abuse Case | Boundary & Classification | Mandatory Architectural Controls (`Preventive & Detective Controls`) | Verification Tests (`SPEC-QA-01/02`) |
|---|---|---|---|---|
| **`TM-01`**<br/>**Malicious External Plugin Archive (`Git / ZIP / OCI`)** | Attacker tricks an admin into staging a `.tpkg`/`.zip` containing path traversal entries `../../etc/cron.d/pwn` (*Zip-Slip*), symlinks pointing to the Host filesystem, or a `1 MB` archive expanding to `10 GB` to exhaust RAM/disk (*Zip-Bomb*). | `TB-2 → TB-3`<br/>`Tampering`, `DoS`, `EoP` | Stream extraction entirely in memory under a hard byte reader cap (`io.LimitReader <= 64 MiB`), strictly reject entries containing `..`, absolute paths, or `symlink`/`hardlink` mode bits before touching disk; stage exclusively into `SHA-256` content-addressed directories. | `CT-04`, `FZ-02` |
| **`TM-02`**<br/>**Supply-Chain & Lifecycle-Script Poisoning (`Supply-Chain Tampering`)** | Mutating a published Git tag (`mutable tag v1.0`) post-release, injecting malware into an npm dependency `postinstall` script, or tampering with a package in transit. | `TB-2 → TB-3`<br/>`Tampering`<br/>`OWASP A08 / LLM03` | Prohibit mutable Git branch/tag pinning — enforce `40-hex` commit SHA + archive `SHA-256` pinning in `tanpopo.lock`; verify `Cosign` signatures prior to load; enforce `npm ci --ignore-scripts` and `govulncheck` + `osv-scanner` in CI. | `CT-03`, `CT-25`, `GATE-REL-04` |
| **`TM-03`**<br/>**Official Publisher Impersonation (`Impersonated Publisher`)** | A community package names itself `@tanpopo-official/openai-pro` or hand-edits `Manifest` JSON to claim `"trustTier": "official"` to trick admins into bypassing scrutiny. | `TB-2 → TB-1`<br/>`Spoofing` | `trustTier` is **never read from self-declared Manifest fields**; Core derives it cryptographically against the embedded `Offline Root Public Key`; reserved namespace squatting is hard-rejected at staging (`403 reserved_namespace_spoofing`). | `CT-05`, `UX-AC-03` |
| **`TM-04`**<br/>**Malicious Extension Sandbox Escape or Core Hang** | Third-party Wasm code attempts to open covert sockets, read `/proc/self/environ`, spin in `while(1)` to starve CPU, or allocate unbounded RAM to crash `tanpopo`. | `TB-4 → TB-3`<br/>`EoP`, `DoS` | Prohibit in-process native code loading (`dlopen`/`plugin`); initialize `wazero` with **zero filesystem, zero env vars, zero raw sockets**, cap linear memory at `<= 16 MiB`/instance with per-call CPU context deadlines, and trip per-plugin fault circuit breakers automatically. | `CT-17`, `FZ-05`, `SPIKE-02` |
| **`TM-05`**<br/>**Upstream Secret Theft (`Credential Exfiltration`)** | A community adapter reads `sk-live-...` and hides it (via steganography/base64) inside LLM completion text or outbound headers to an attacker host; or a malicious upstream echoes credentials back inside a `401` error body. | `TB-4 / TB-6 → TB-1`<br/>`Info Disclosure`<br/>`OWASP LLM02` | **`Blind Host Credential Broker` (`ADR-0002`):** Plugins hold only a symbolic handle (`slot_handle_01`) with **zero raw secret bytes in Wasm RAM**; Host injects secrets at the network edge strictly when the target host matches that connection's approved `allowed_hosts`; egress error scrubbers redact secret substrings from error streams and logs. | `CT-14`, `CT-16`, `OPS-AC-10` |
| **`TM-06`**<br/>**`SSRF`, `DNS Rebinding` & Cross-Origin Redirect Leakage** | Configuring a local model URL, MCP server URL, or multimodal `image_url` pointing to `rebind.attacker.com` (`TTL=0`) flipping to `169.254.169.254` (cloud IAM metadata) or internal LAN ports; or returning `302 Redirect` cross-origin to steal `Authorization` headers. | `TB-1 / TB-6 → TB-3`<br/>`Info Disclosure`, `EoP`<br/>`OWASP A10` | Inspect the resolved peer IP **directly inside `net.Dialer.Control` at socket `connect()` time** (eliminating DNS Rebinding `TOCTOU` windows); unconditionally block `169.254.0.0/16` & `fd00:ec2::254` in every mode; disable cross-origin redirect following or strip all auth headers on redirect. | `CT-15`, `CT-26` |
| **`TM-07`**<br/>**`Prompt Injection`, MCP Tool Poisoning & Malicious Output** | Untrusted documents/webpages carry hidden instructions coercing the model into calling `delete_database`; an external MCP server silently mutates a tool description (*Rug-Pull*); or model output embeds `<script>` payloads / ANSI terminal escape sequences targeting an admin's trace viewer or CLI. | `TB-6 / TB-7 → TB-3 / TB-1`<br/>`Tampering`, `EoP`<br/>`OWASP LLM01, 05, 06` | Never treat `system prompt` text as a security boundary: enforce hard per-`Virtual Key` tool allowlists, pin canonical `schema_hash` fingerprints (a 1-char description edit immediately locks the tool `403`), require human `Reviewer` approval bound to argument hashes for `destructive` tools, HTML-escape all Web Console trace text under strict `CSP`, and strip dangerous ANSI control sequences in CLI output. | `CT-21`, `CT-22`, `CT-27` |
| **`TM-08`**<br/>**`OAuth Token` Theft or Unauthorized Relay (`Token Relay / Confused Deputy`)** | Intercepting OAuth callbacks via CSRF, racing concurrent token refreshes, tricking Tanpopo into forwarding Service A's `access_token` to third-party MCP/Adapter B, or abusing personal consumer subscription cookies. | `TB-6 / TB-7 → TB-3`<br/>`Spoofing`, `EoP` | Require `OAuth 2.1 + PKCE (S256)` + session-bound `state`; enforce strict token audience binding (`RFC 8707 Resource Indicators` & `allowed_audience_hosts`); prohibit relaying end-user admin tokens upstream across trust boundaries; reject all consumer subscription cookie-scraping flows (`DEC-004`). | `CT-19`, `CT-20`, `AUTH-08`..`12` |
| **`TM-09`**<br/>**Cross-Tenant Boundary Bypass & `IDOR` (`Cross-Tenant IDOR`)** | An `Org A` admin guesses or supplies an `Org B` `Connection`, `Route`, or `request_id` `UUID` to `/admin/v1/*`, or exploits a cache key collision to receive cached completions containing `Org B` data. | `TB-1 → TB-3 / TB-5`<br/>`Info Disclosure`, `EoP`<br/>`OWASP A01` | Anchor every SQL query and composite foreign key to the session-authenticated `org_id` (never trusting request-body `org_id`) backed by `PostgreSQL RLS FORCE`; return uniform anti-oracle `404` responses; prefix every cache key with `org_id:project_id:env_id`. | `AUTH-13`..`15`, `CT-28` |
| **`TM-10`**<br/>**`Virtual Key` Leakage & Timing Side-Channels** | A `vk_tp_...` bearer key is accidentally committed to a public Git repo, logged by an intermediate proxy, or probed character-by-character via microsecond timing measurements. | `TB-1 → TB-3`<br/>`Spoofing`, `Info Disclosure` | Persist only `SHA-256` virtual key digests, verify via `crypto/subtle.ConstantTimeCompare`, enforce recognizable `vk_tp_` prefixes for automated secret scanners (`gitleaks`/GitHub Secret Scanning), and provide `Emergency Revoke` severing active streams in `< 1s`/`< 5s`. | `AUTH-04`..`06`, `OPS-AC-03` |
| **`TM-11`**<br/>**Sensitive Payload Remnance (`Retention & Erasure Failure`)** | Conversational PII is inadvertently persisted in trace tables, error logs, or cold backups and remains readable after a customer submits a `GDPR` erasure request. | `TB-5`<br/>`Info Disclosure` | Default to `Metadata-Only Telemetry` (`0 bytes` of prompt/completion text persisted); cap optional approved debug sampling at `TTL <= 72h`; on `Project`/`Organization` erasure, destroy the dedicated encryption key (`DEK` *Crypto-Shredding*) within `< 60s`, rendering even historical cold backups unreadable. | `OPS-AC-09`, `OPS-AC-10` |
| **`TM-12`**<br/>**Denial of Service & Budget Exhaustion (`DoS / Denial of Wallet`)** | Opening 500 parallel requests when `1 USD` of budget remains to race past spending caps; posting 10,000-level deep JSON; holding slow-read connections (*Slowloris SSE*); or triggering infinite recursive A2A delegation loops. | `TB-1 / TB-7 → TB-3`<br/>`DoS`<br/>`OWASP LLM10` | Execute atomic `Pre-Request Budget Hold` **before opening upstream streams**; enforce request body byte limits, JSON nesting depth (`<= 64`), `ReadHeaderTimeout <= 5s`, per-key concurrent stream caps, and A2A delegation `hop_count <= 5`. | `CT-23`, `CT-29`, `SPIKE-03` |
| **`TM-13`**<br/>**Administrative Repudiation or Audit Tampering (`Repudiation`)** | A malicious insider raises budget caps, disables PII redaction, or inspects debug payloads and then edits/deletes database log rows to cover tracks. | `TB-3 → TB-5`<br/>`Repudiation`, `Tampering` | Record every configuration mutation, permission approval, and sensitive data view into an *Append-Only* `audit_events` table linked via `SHA-256(prev_hash || canonical_event)` hash chains and streamed in real time to external WORM storage. | `OPS-AC-08`, `CT-30` |

---

## 4. Non-Negotiable Design Invariants

Any feature proposal or source-code PR violating even **1 of the 6 invariants** below fails security review automatically regardless of other considerations:
1. **Zero third-party code executes inside the Core process memory space** (`0` `plugin.Open`, `0` `dlopen`, `0` `eval` / `Function()`).
2. **Zero `/admin/v1/*` endpoints accept Data-Plane `Virtual Key` authentication**, and zero mutating admin operations bypass `org_id`/`project_id` ownership verification or immutable audit logging.
3. **Zero Tier-1 or default Tier-2 `Extension` instances ever observe raw upstream credentials (`API Key` / `OAuth Token`) in guest memory** (`Blind Host Credential Broker` is mandatory).
4. **Zero outbound network connections bypass `connect()`-time IP verification guarding against `169.254.169.254` / `DNS Rebinding`.**
5. **Zero silent post-first-chunk failover concatenates responses from a fallback provider after the first content chunk has been flushed to the caller (`No Post-First-Chunk Silent Failover`).**
6. **Zero mechanisms are designed, maintained, or badged to bypass vendor Terms of Service by turning personal consumer subscriptions into shared API proxies (`DEC-004`).**
