# Extension Platform Contract & Isolated Execution Architecture Specification — Tanpopo

> **Extension Architecture Specification Document (ASTRA Phase A04 — `DRAFT / ILLUSTRATIVE CANDIDATE SPEC`)**
> - **Document ID:** `SPEC-EXT-01` | **Version:** `0.1.0-step04-draft` | **Last Updated:** `2026-11-11`
> - **Authoring Role:** Platform Architect & Security-Minded SDK Designer
> - **Standardization Status Statement:** All `Manifest` structures, JSON/ABI schemas, resource quotas, and sequence diagrams in this document are **illustrative candidate designs (`ILLUSTRATIVE — PRE-ADR & PRE-POC`)** — **not yet frozen standards** — pending Product Owner approval at **Gate G1/G2** and completion of technical spikes (`EXP-01`..`EXP-03`) tracked in [`decisions.md`](../governance/decisions.md).
> - **Requirements Traceability:** `TP-FR-0301`..`0306`, `TP-NFR-0101`..`0102`, `TP-NFR-0202`, `TP-NFR-0401`..`0403`, `TP-NFR-0902`, `TP-BR-0104`, `TP-BR-0401`..`0404`.

---

## 1. Ecosystem Taxonomy (`Canonical Extension Taxonomy`) & Core Kernel Boundaries

To solve Tanpopo's foundational extensibility goal — **enabling communities and enterprises to add new AI providers and capabilities without modifying or re-releasing Tanpopo Core** — the architecture strictly decouples **Packaging Identity (`Identity`)**, **Execution Mechanism (`Runtime`)**, and **Governance Lifecycle (`Lifecycle`)**:

### 1.1. Eight-Entity Architectural Partition Matrix

| Architectural Entity | Identity (`Identity`) | Execution Locus (`Runtime`) | Ownership & Lifecycle (`Lifecycle`) | Core Kernel vs. External Extension? |
|---|---|---|---|---|
| **1. `Extension Package`** | Immutable artifact bundle identified by `@publisher/name`, `SemVer` version, and `SHA-256` digest. | Does not execute on its own; serves as the distribution unit holding the `Manifest`, declarative rules, or isolated guest bytecode. | `Instance`-scoped (`SM-01`), enabled/disabled per `Organization` policy (`TP-BR-0202`). | **External extension unit** (dynamically loaded into Core). |
| **2. `Provider Adapter`** | A `Capability` inside an `Extension Package` defining wire translation for an upstream AI provider family. | Executes at Tier 1 (`Declarative`), Tier 2 (`Sandboxed Hook`), or Tier 3 (`Isolated Sidecar`). **Contains zero customer credentials.** | Coupled to its parent `Extension Installation` lifecycle (`SM-01`). | **Decoupled extension** (even `Official Adapters` obey this exact contract). |
| **3. `Provider Connection`** | Operational configuration record (`conn_id`) binding 1 `Provider Adapter` + 1 `secret_ref` + target endpoint URL + `Data Region`. | Managed and access-enforced directly inside **Tanpopo Core**. | Owned by an `Organization` or `Project` (`SM-02`). | **Core Kernel entity** (live configuration state, not a code package). |
| **4. `Logical Model` & `Model Deployment`** | `Logical Model`: canonical capability profile (`model_id`).<br/>`Model Deployment`: concrete routing target (`deploy_id`) bound 1-to-1 to a `Provider Connection`. | Core's router, circuit breaker, and budget ledger evaluate `Model Deployment` entities directly. | `Organization` / `Project`-scoped (`SM-04`). | **Core Kernel entity** (may seed defaults from Adapter catalog hints, but governed in Core). |
| **5. `Auth Driver`** | Upstream authentication mechanism: either a built-in Host primitive or a bounded custom signer inside an `Extension Package`. | Standard primitives (Bearer, OAuth 2.1, mTLS, Host-Sign HMAC) execute inside the **Host Egress Broker**; custom canonicalization runs inside a boundary-locked Sandbox. | Bound to `Secret Credential` & `Provider Connection` lifecycles (`SM-02`, `SM-03`). | **Controlled hybrid:** Secret vault & wire injection belong to **Core**; custom signing string construction is extensible. |
| **6. `MCP Server` & `MCP Tool`** | Standalone context/tool service speaking **Model Context Protocol** and its individual namespaced tools (`<server>__<tool>`). | Runs as an external server (or isolated Sidecar), governed exclusively through the **Core MCP Hub** (`/mcp/...`). | Registered at `Organization`/`Project` scope; tools pinned by `schema_hash` & `SM-06`. | **Dedicated MCP Plane** (`DEC-003`) — **not** a `Provider Adapter`. |
| **7. `Agent Endpoint` & `A2A Task`** | Autonomous agent publishing an `AgentCard` and stateful multi-step asynchronous task units on the **A2A** plane. | Agent executes externally; the **Core A2A Plane** (`/a2a/...`) enforces delegation scopes, hop limits, and sub-budget holds. | Registered at `Organization`/`Project` scope; tasks governed by `SM-06` & `SM-07`. | **Dedicated A2A Plane** (`DEC-003`) — **never conflated** with `Provider` or `MCP`. |
| **8. `Routing / Policy / Exporter Extension`** | Pipeline extension capabilities: custom route scoring, PII/content `Guardrail` hooks, or custom telemetry sinks. | Executes in Tier 2 (`Sandboxed Hook`) or Tier 3 (`Isolated Sidecar`) at deterministic `Hook Stages`. | `Instance` / `Organization`-scoped (`SM-01`). | **Decoupled extension**, **never permitted to override** Core's hard `Fail-Closed` invariants. |

### 1.2. Non-Overridable Kernel of Tanpopo Core (`Non-Overridable Kernel`)

Regardless of which extensions are installed, **the following 7 kernel subsystems remain permanently anchored inside Tanpopo Core and cannot be disabled or bypassed by any Extension**:
1. `Principal` / `Virtual Key` authentication, `RBAC` authorization, and hard multi-tenant isolation (`TP-BR-0101`).
2. The `Master Secret Vault` and network-edge `Blind Host Credential Broker` (`TP-BR-0402`).
3. The `Two-Phase Budget Hold & Settlement` ledger (`TP-BR-0701`, `0702`).
4. `Hard Data Residency` enforcement and mandatory `Fail-Closed` posture (`TP-BR-0501`, `0502`).
5. Sandbox isolation enforcement, `allowed_hosts` egress allowlisting, SSRF prevention, and resource circuit breakers (`TP-BR-0401`, `0404`).
6. Human-in-the-loop approval gates for `destructive` MCP tools and A2A privilege-amplification bounds (`TP-BR-0602`, `0604`).
7. The `Append-Only Audit Sink` and 4-level emergency kill-switch subsystem (`TP-BR-0803`).

---

## 2. SDK Contract & Versioned Language-Agnostic Interface (`Versioned Language-Agnostic Contract`)

### 2.1. Language-Agnostic Design Principle
- **Decoupling Core implementation language from Extension authoring language:** Communication between Host (Tanpopo Core) and Guest (Extension) is governed by **versioned canonical data envelopes (`contract_version`)** across a **language-neutral binary/IPC boundary** (primary candidates: **WebAssembly Component Model / WIT** for Tier 2 and **HTTP/gRPC Protobuf/JSON over Unix Domain Socket / mTLS** for Tier 3).
- Consequently, whether Tanpopo Core is implemented in Go or Rust (`OQ-001`), community and enterprise authors can write extensions in **pure YAML/JSON** (Tier 1) or compile from **TypeScript/JavaScript, Rust, Go, or Python** into standard artifacts (Tier 2 / Tier 3) verified against a single shared contract test suite.

### 2.2. Illustrative `Extension Manifest` (`ILLUSTRATIVE — NOT A FROZEN STANDARD`)

```yaml
# ILLUSTRATIVE CANDIDATE (v0.1-draft — Pending ADR Approval)
apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest

metadata:
  id: "@acme-cloud/regional-llm-adapter"
  name: "Acme Regional Sovereign AI Adapter"
  version: "0.2.0"
  license: "Apache-2.0"
  homepage: "https://github.com/acme-cloud/tanpopo-regional-llm"
  description: "Provider adapter for Acme Sovereign EU/JP inference endpoints."

compatibility:
  contractVersion: "0.1"              # Required Host-Guest contract version
  coreVersionRange: ">=0.1.0 <0.3.0"  # Compatible Tanpopo Core SemVer range

runtime:
  tier: "sandboxed_wasm"              # Enum: declarative | sandboxed_wasm | isolated_sidecar
  entrypoint: "dist/adapter.wasm"
  sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  quotas:
    maxMemoryMb: 32
    maxCpuMsPerCall: 15
    maxStreamChunkCpuMs: 2

capabilities:
  - type: "provider_adapter"
    adapterSlug: "acme-regional"
    supportedDialects:
      - "openai_compatible"
    nativePassthroughPrefix: "/native/acme"
    authMethods:
      - id: "api_key_header"
        mode: "host_blind_inject"     # Host injects credential at network edge; Wasm NEVER sees raw key
        headerName: "X-Acme-Api-Key"
      - id: "enterprise_hmac"
        mode: "host_crypto_sign"      # Wasm emits canonical string; Host computes HMAC and attaches signature

configSchema:                         # JSON Schema for non-secret Connection configuration
  type: "object"
  required: ["regionCode"]
  additionalProperties: false
  properties:
    regionCode:
      type: "string"
      enum: ["eu-central-1", "ap-northeast-1"]
    customSubdomain:
      type: "string"
      pattern: "^[a-z0-9-]{1,32}$"

secretSlots:                          # Declares slot reference names ONLY; NEVER holds secret values
  - slot: "primary_api_key"
    requiredForAuthMethods: ["api_key_header"]
  - slot: "hmac_signing_secret"
    requiredForAuthMethods: ["enterprise_hmac"]

permissions:                          # Default-Deny; every undeclared capability is hard-blocked
  networkEgress:
    allowedHosts:
      - "inference.eu-central-1.acme-ai.example"
      - "inference.ap-northeast-1.acme-ai.example"
    allowPrivateIp: false
    requireTls: true
  hooks:
    - "provider:validate_connection"
    - "provider:transform_request"
    - "provider:transform_stream_chunk"
    - "provider:extract_usage"
    - "provider:classify_error"
  payloadAccess:
    inspectRequestBody: true          # Required to translate inference JSON schemas
    inspectResponseBody: true
    maxPayloadBytes: 16777216         # 16 MB
```

### 2.3. Entrypoints (`Entrypoints / Hooks`), Streaming, Cancellation & Metering Contracts

Every boundary call between Core and an Extension exchanges immutable, contract-versioned **`Normalized Envelopes`**:

| Interface Group (`Hook / Contract`) | Host-to-Guest Input | Guest-to-Host Output | Security & Reliability Invariants |
|---|---|---|---|
| **`validate_connection(cfg, host_probe)`** | Non-secret config validated against `configSchema` + bounded Host probe handle. | `ValidationResult { ok, detected_models[], error_code? }`. | Receives zero raw secret strings; probe calls remain strictly confined to `allowedHosts`. |
| **`transform_request(req_envelope, ctx)`** | Normalized request (or passthrough frame), `request_id`, `capabilities_required`, connection config, and `secret_slot_refs`. | `UpstreamHttpSpec { method, url, headers, body_mode, credential_inject_instruction }`. | Host re-verifies `UpstreamHttpSpec.url` before opening any socket: any mismatch against `allowedHosts` or resolution to private IP ranges aborts immediately (`TP-AC-SEC-02`). |
| **`transform_stream_chunk(state, raw_chunk)`** | Bounded per-stream state (`<= 64 KB`) + upstream raw byte/SSE chunk. | `StreamEmit { normalized_events[], state_next, usage_delta?, terminal_status? }`. | Incremental chunk-by-chunk relay (`zero full-stream buffering`); if an error occurs once `chunks_emitted >= 1`, Host hard-locks out any fallback retry (`TP-BR-0503`). |
| **`on_cancel(cancel_reason)`** | Cancellation signal on `API Client` disconnect or deadline expiry. | Releases per-session scratch state within `< 50 ms`. | Upstream TCP/HTTP2 socket teardown is enforced **directly by Host** within `< 500 ms` (`TP-FR-0104`), independent of Plugin cooperation! |
| **`extract_usage(resp_or_stream_summary)`** | Final response or stream accumulator (paired with Host fallback byte/char counters). | `UsageReport { input_tokens, output_tokens, cache_read, cache_write, reasoning_tokens, accuracy: EXACT \| ESTIMATED \| UNMETERED_FLAGGED }`. | If the Plugin omits usage metrics or a stream aborts mid-flight, Host automatically caps accuracy at `ESTIMATED` or `UNMETERED_FLAGGED`, never accepting a synthetic `EXACT = 0` (`TP-BR-0703`). |
| **`classify_error(http_status, headers, sanitized_body)`** | HTTP status, `Retry-After` headers, and Host-sanitized error body. | `CanonicalError { category, retryable_pre_stream, cooldown_ms, trigger_reauth, public_code }`. | Maps vendor failures onto Tanpopo's canonical error taxonomy so the router and `SM-02`/`SM-04` state machines govern circuit breaking and failover deterministically. |

### 2.4. Versioning & Backward Compatibility Policy (`Compatibility & Deprecation Policy`)
- **Additive-Only Evolution (`Additive Evolution`):** Within any minor contract line (e.g., `0.1` → `0.2` or `1.0` → `1.x`), only new `optional fields` with safe defaults may be introduced; mutating field types or removing existing fields is forbidden.
- **Backward Support Window (`N-1` / `12 months` — `TP-NFR-0902`):** During `0.x`, Tanpopo Core maintains a `Contract Adapter Shim` for at least `N-1` prior contract versions. From `v1.0` onward, any `Compulsory Deprecation` requires at least **12 months** advance notice alongside automated checks in `tanpopo plugin migrate-check`.

---

## 3. Three Extension Execution Models & Security Isolation Boundaries (`Three Extensibility Tiers`)

> **Absolute Security Invariant (`TP-NFR-0401`):** Third-party or untrusted extension code **must never execute `in-process` within the same memory address space** as the Tanpopo Core kernel (arbitrary native `dlopen`, `eval()`, or unisolated `import()` of JS/Python code is unconditionally prohibited).

### 3.1. Comparison & Trade-Off Matrix Across the Three Execution Tiers

| Dimension | **Tier 1: Declarative Package (`Declarative Adapter`)** | **Tier 2: Isolated Execution Hooks (`Sandboxed Hooks — Wasm`)** | **Tier 3: Out-of-Process Service (`Isolated Sidecar`)** |
|---|---|---|---|
| **Technical Nature** | Static configuration artifact (YAML/JSON + non-Turing-complete safe mapping expressions). | Compiled bytecode module running inside a capability-based virtual machine (WebAssembly Component Sandbox). | Standalone OCI Container / OS process communicating over loopback UDS / mTLS. |
| **Target Coverage** | ~75–80% of OpenAI-compatible REST/SSE providers differing only in URL paths, headers, pricing, or model slugs. | ~15–20% of providers with bespoke JSON/SSE wire shapes, custom request signatures, or fast policy guardrails. | ~5% of heavy enterprise integrations: native C/C++/PKCS#11 HSM libraries, local ML PII scanners, or `stdio` MCP bridges. |
| **Primary Advantage** | **`0` arbitrary code execution attack surface**; ultra-low overhead (`< 2 ms`); authored in `< 30 minutes`. | Single portable binary across OS/CPU architectures; hard linear-memory isolation; trivial Git/ZIP distribution. | Unconstrained by Wasm compiler toolchains; reuses existing native enterprise SDKs and C libraries. |
| **Trade-Offs / Cost** | Cannot express complex stateful stream-transformation state machines. | Requires SDK compilation to `.wasm`; high-throughput streaming memory-copy overhead must be benchmarked (`POC-01`). | Per-sidecar baseline RAM footprint; requires `Operator` container/cgroups/NetworkPolicy orchestration. |
| **Who May Install?** | `Operator` / `Org Owner` from permitted origins after network egress consent. | `Operator` / `Org Owner` after `Manifest` & sandbox quota approval. | **Infrastructure `Operator` exclusively** may deploy and approve Sidecar container/network manifests. |

### 3.2. Trust Domains, System Permissions & Resource Quotas Matrix (`Trust Domains & Quotas`)

| Control Boundary | Tier 1: `Declarative` | Tier 2: `Sandboxed Hooks (Wasm)` | Tier 3: `Isolated Sidecar` |
|---|---|---|---|
| **Filesystem Access (`Filesystem`)** | **None** (`0` file permissions). | **None** (no host WASI filesystem mounted; operates purely on passed byte buffers). | `read-only rootfs`, unprivileged `non-root UID`, bounded tmpfs `/tmp` scratch space only. |
| **Host Environment (`Host Env`)** | **None** (barred from reading Core's `os.Environ`). | **None** (completely empty environment by default). | Receives solely non-sensitive configuration variables explicitly declared by the `Operator` for that Sidecar. |
| **Outbound Network (`Network Egress`)** | Exclusively via **Host Egress Proxy** restricted to `allowedHosts`. | Zero raw TCP socket APIs; all HTTP probes flow through Host functions hard-locked to `allowedHosts` + anti-SSRF guards. | Enforced via container/OS network policy: blocks all RFC1918/link-local traffic and permits egress solely via Tanpopo's Egress Proxy or approved host allowlists. |
| **CPU & Timeout Quotas (*Target Design*)** | `< 1 ms` structural mapping. | Instruction metering (`fuel / epoch interruption`): default `<= 15 ms CPU` per request/response transform and `<= 2 ms CPU` per SSE chunk. | `cgroups` CPU limits (e.g., `0.5–2.0 vCPU`) + hard per-IPC call deadline from Core (`<= 50 ms` for policy hooks, backed by a dedicated circuit breaker). |
| **Memory Quotas (*Target Design*)** | Zero dedicated heap allocation. | Hard per-instance linear memory ceiling (`16 MB – 64 MB` per Manifest; breaching traps only that invocation). | Hard container memory ceiling (`OOM-kill` contained inside the Sidecar container without impacting Core). |
| **Audit Trail (`Audit`)** | Logs installation, version pinning, and host allowlist rejections. | Logs module load, every CPU/RAM trap, and any attempted call outside `allowedHosts` (`TP-BR-0803`). | Logs provisioning, internal mTLS certificate rotation, health probes, and Sidecar circuit-breaker trips. |

---

## 4. Open Technical Unknowns Pending PoC / ADR Finalization (`Open Technical Unknowns`)

In accordance with architectural transparency discipline, the items below remain **explicitly unfrozen** and require empirical PoC verification before locking the `1.0` specification:
1. **`POC-01` — Concrete Wasm ABI Standard (`WASI Preview 2 / Component Model WIT` vs Minimal `Extism`-Style ABI):** Contingent on polyglot toolchain maturity (especially TypeScript/Python-to-Wasm) and measured SSE chunk-relay latency in spike `EXP-01` (`ASM-002`).
2. **`POC-02` — Safe Expression Subset in Declarative Packages (Tier 1):** Requires empirical validation across 10 diverse AI providers (`EXP-02`) to select the smallest JSON-mapping/template operator set achieving `>= 75%` coverage (`ASM-004`) without inventing an ad-hoc programming language inside YAML.
3. **`POC-03` — Core-to-Isolated-Sidecar IPC Wire Protocol (Tier 3):** Selecting between `ConnectRPC / gRPC` and `HTTP/2 JSON + SSE` over Unix Domain Sockets / mTLS will be finalized jointly with the Core implementation language decision (`OQ-001`).
