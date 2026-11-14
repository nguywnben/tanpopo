# Extension Platform Contract & Isolated Execution Architecture Specification — Tanpopo

> **Canonical Extension Architecture Specification (Synchronized in `TP-IMP-0002` — Spec Closure Batch B)**
> - **Document ID:** `SPEC-EXT-01` | **Version:** `0.2.0-m0-closure` | **Last Updated:** `2026-11-14`
> - **Authoring Role:** Platform Architect & Security-Minded SDK Designer
> - **Machine-Readable Contract Reference:** [`proto/tanpopo/extension/v1/runtime.proto`](../../proto/tanpopo/extension/v1/runtime.proto) & [`buf.yaml`](../../buf.yaml) (`version: v2`). Host–Wasm binary ABI details (`POC-01`) and Tier-1 mapping operator sets (`POC-02`) are empirically validated in `SPIKE-01` / `SPIKE-02` (`TP-IMP-0004`).
> - **Measurement Honesty (`COND-PO-01`):** All CPU deadlines (`<= 15 ms`, `<= 5 ms`), SSE chunk forwarding targets (`< 1 ms p95`), memory caps (`16 MiB`), and cancellation bounds (`< 500 ms`) in this document are **`TARGET / UNVERIFIED`** until benchmark verification completes.
> - **Requirements Traceability:** `TP-FR-0301`..`0308`, `TP-NFR-0101`..`0102`, `TP-NFR-0202`, `TP-NFR-0401`..`0403`, `TP-NFR-0902`, `TP-BR-0104`, `TP-BR-0401`..`0404`, `CR-02`, `CR-05`.

---

## 1. Ecosystem Taxonomy (`Canonical Extension Taxonomy`) & Core Kernel Boundaries

To enable communities and enterprises to add AI providers and pipeline capabilities **without modifying or re-releasing the `tanpopo` Core binary**, Tanpopo strictly decouples **Packaging Identity**, **Execution Locus**, and **Governance Lifecycle**:

### 1.1. Eight-Entity Architectural Partition Matrix

| Architectural Entity | Identity (`Identity`) | Execution Locus (`Runtime`) | Ownership & Lifecycle (`Lifecycle`) | Core Kernel vs. External Extension? |
|---|---|---|---|---|
| **1. `Extension Package`** | Immutable `.tpkg` / OCI artifact identified by `@publisher/name` (reserved regex `^@tanpopo(-[a-z0-9]+)?/`), `SemVer` version, and `SHA-256` digest. | Distribution unit holding `Manifest`, Tier-1 declarative rules, or Tier-2/Tier-3 guest artifacts. | `Instance`-scoped (`SM-01`), enabled/disabled per `Organization` via `org_extension_policies` (`TP-BR-0202`, `CR-04`). | **External extension unit** (dynamically loaded without Core rebuild). |
| **2. `Provider Adapter`** | Capability inside an `Extension Package` defining wire translation for an upstream AI provider family. | Executes at Tier 1 (`Declarative` — `v0.1`), Tier 2 (`Sandboxed Wasm` — `v0.1`), or Tier 3 (`Isolated Sidecar` — `v0.3`, `TP-FR-0308`). **Contains zero customer credentials.** | Coupled to its parent `Extension Installation` lifecycle (`SM-01`). | **Decoupled extension** (`Official Adapters` obey this exact contract). |
| **3. `Provider Connection`** | Operational configuration record (`conn_id`) binding 1 `Provider Adapter` + multi-slot `connection_secret_bindings` + endpoint URL + `Data Region`. | Managed and access-enforced directly inside **Tanpopo Core**. | Owned by an `Organization` or `Project` (`SM-02`). | **Core Kernel entity** (live configuration state, not a code package). |
| **4. `Logical Model` & `Model Deployment`** | `Logical Model`: canonical `snake_case` capability profile (`model_id`).<br/>`Model Deployment`: concrete routing target (`deploy_id`) bound to a `Provider Connection`. | Core's router, circuit breaker, and two-phase budget ledger evaluate `Model Deployment` entities directly. | `Organization` / `Project`-scoped (`SM-04`). | **Core Kernel entity** (may seed defaults from Adapter catalog hints, governed in Core). |
| **5. `Auth Driver`** | Upstream authentication mechanism: built-in Host primitive or Host-executed cryptographic signer (`HostCryptoSignSpec`). | Standard primitives (`host_blind_inject`, `OAuth 2.1`, `mTLS`, `host_crypto_sign`: `HMAC_SHA256`, `AWS_SIGV4`, `RSA_SHA256`) execute inside the **Host Blind Credential Broker** (`0` raw secrets in Tier-1/Tier-2; `Cookie` injection hard-banned). | Bound to `Secret Credential` & `Provider Connection` lifecycles (`SM-02`, `SM-03`). | **Controlled hybrid:** Vault & wire injection belong exclusively to **Core**; canonical string construction runs in Sandbox. |
| **6. `MCP Server` & `MCP Tool`** | External tool/context service speaking **MCP Specification `2025-03-26` (Streamable HTTP)** and namespaced tools (`<server_slug>__<tool_name>`). | Runs externally (or as an `OPERATOR`-managed Tier-3 Sidecar for `stdio` bridges), governed through the **Core MCP Hub** (`/mcp`). | Registered at `Organization`/`Project` scope; tools pinned by JCS `schema_hash` & HITL `SM-06`. | **Dedicated MCP Plane** (`DEC-003`) — **not** a `Provider Adapter`. |
| **7. `Agent Endpoint` & `A2A Task`** | Autonomous agent publishing an `AgentCard` and stateful asynchronous tasks on the **A2A** plane. | Agent executes externally; the **Core A2A Plane** (`/a2a/*`) enforces delegation scopes, `hop_count <= 5`, and sub-budget holds. | Registered at `Organization`/`Project` scope; tasks governed by dedicated **`SM-09`** (`CR-08`) & `SM-07`. | **Dedicated A2A Plane** (`DEC-003`) — **never conflated** with `Provider`, `MCP`, or `SM-06`. |
| **8. `Routing / Policy / Exporter Extension`** | Pipeline extension capabilities: custom route scoring, PII/content `Guardrail` hooks, or custom telemetry sinks. | Executes in Tier 2 (`Sandboxed Wasm`) or Tier 3 (`Isolated Sidecar`) at deterministic `Hook Stages`. | `Instance` / `Organization`-scoped (`SM-01`). | **Decoupled extension**, **never permitted to override** Core's hard `Fail-Closed` invariants. |

### 1.2. Non-Overridable Kernel of Tanpopo Core

Regardless of which extensions are installed, **the following 7 kernel subsystems remain permanently anchored inside Tanpopo Core and cannot be disabled or bypassed by any Extension**:
1. `Principal` / `Virtual Key` authentication, 6-role `RBAC` authorization, and hard multi-tenant isolation (`TP-BR-0101`).
2. The 3-tier `Envelope Encryption Vault` (`Master KEK -> Org DEK -> Project DEK`) and network-edge `Blind Host Credential Broker` (`TP-BR-0402`, hard `Cookie`/`Set-Cookie` ban per `DEC-004`).
3. The `Two-Phase Budget Hold & Settlement` `micro-USD` ledger (`TP-BR-0701`, `0702`).
4. `Hard Data Residency` enforcement and mandatory `Fail-Closed` posture (`TP-BR-0501`, `0502`).
5. Sandbox isolation enforcement (`16 MiB` Wasm RAM cap, `<= 15 ms` / `<= 5 ms` CPU bounds), `allowedHosts` egress allowlisting, connect-time `SSRFGuard`, and per-extension fault quarantine breakers (`TP-BR-0401`, `0404`).
6. Human-in-the-loop approval gates (`SM-06`, 5-tuple JCS hash) for `destructive` MCP tools and A2A privilege-amplification bounds (`SM-09`, `TP-BR-0602`, `0604`).
7. The `Per-Org + SYSTEM` JCS hash-chained `Audit Sink` and 4-level emergency kill-switch subsystem (`TP-BR-0803`).

---

## 2. SDK Contract & Versioned Language-Agnostic Interface

### 2.1. Language-Agnostic Binary Contract & Out-of-Band `BlobRef` Boundary
- **Decoupling Core runtime from Extension authoring language:** Communication between Host (`tanpopo` Core) and Guest (Extension) is governed by [`proto/tanpopo/extension/v1/runtime.proto`](../../proto/tanpopo/extension/v1/runtime.proto) (`go_package = "github.com/nguywnben/tanpopo/gen/proto/extension/v1;extensionv1"`). Tier-2 Wasm guests exchange the exact same structural envelopes over bounded linear-memory buffers (`POC-01`), while Tier-3 Isolated Sidecars (`v0.3`) speak `ConnectRPC / Protobuf` over `0600` Unix Domain Sockets or `mTLS` (`POC-03`).
- **Out-of-Band Multimodal `BlobRef` Invariant (`CR-05` / `NEG-1`):** Ingress payloads up to `32 MiB` never copy decoded attachments `> 64 KiB` into Tier-2 Wasm's `16 MiB` linear memory. Host Core extracts large media blocks into Host streaming buffers, passes compact `BlobRef` handles (`blob_ref://<request_id>/<part_index>`, `mime_type`, `byte_length`, `sha256_hex`) into `TransformRequestInput`, and splices bytes directly onto the outbound TLS stream on egress.
- **Zero `scoped_secret_unseal` in Tier-1 & Tier-2 (`CR-05`):** Neither Tier-1 Declarative packs nor Tier-2 Wasm modules can ever request raw secret unsealing. Dynamic cryptographic signatures (`HMAC_SHA256`, `AWS_SIGV4`, `RSA_SHA256`) are computed inside Host Core via `HostCryptoSignSpec`. Proprietary closed native C/HSM signing libraries are permitted exclusively inside `OPERATOR`-provisioned Tier-3 Isolated Sidecars (`v0.3`).

### 2.2. Canonical `Extension Manifest` Structure (`.tpkg` Archive)

```yaml
apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest

metadata:
  id: "@acme-cloud/regional-llm-adapter"   # ^@tanpopo(-[a-z0-9]+)?/ reserved exclusively for Tanpopo root key
  name: "Acme Regional Sovereign AI Adapter"
  version: "0.2.0"
  license: "Apache-2.0"
  homepage: "https://github.com/acme-cloud/tanpopo-regional-llm"
  description: "Provider adapter for Acme Sovereign EU/APAC inference endpoints."

compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"

runtime:
  tier: "sandboxed_wasm"                   # Enum: declarative | sandboxed_wasm | isolated_sidecar
  entrypoint: "dist/adapter.wasm"
  sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  quotas:
    maxMemoryMib: 16                       # Hard Tier-2 Wasm ceiling: 16 MiB (TARGET / UNVERIFIED)
    maxCpuMsPerCall: 15                    # Request/Response/Probe/Error hook ceiling: <= 15 ms (TARGET / UNVERIFIED)
    maxStreamChunkCpuMs: 5                 # Per-SSE-chunk hook ceiling: <= 5 ms (< 1 ms p95 target — TARGET / UNVERIFIED)

capabilities:
  - type: "provider_adapter"
    adapterSlug: "acme-regional"
    supportedDialects:
      - "openai_compatible"
    nativePassthroughPrefix: "/native/acme"
    authMethods:
      - id: "api_key_header"
        mode: "host_blind_inject"          # Host injects credential at network edge; Wasm NEVER sees raw secret
        headerName: "X-Acme-Api-Key"       # "Cookie" and "Set-Cookie" are unconditionally rejected (DEC-004)
      - id: "enterprise_hmac"
        mode: "host_crypto_sign"           # Guest builds canonical_payload; Host signs via HMAC_SHA256 / AWS_SIGV4 / RSA_SHA256

configSchema:
  type: "object"
  required: ["regionCode"]
  additionalProperties: false
  properties:
    regionCode:
      type: "string"
      enum: ["eu-central-1", "ap-northeast-1"]

secretSlots:                               # Declares slot reference names ONLY; NEVER holds secret plaintext
  - slot: "primary_api_key"
    requiredForAuthMethods: ["api_key_header"]
  - slot: "hmac_signing_secret"
    requiredForAuthMethods: ["enterprise_hmac"]

permissions:                               # Default-Deny; every undeclared capability is hard-blocked
  networkEgress:
    allowedHosts:
      - "inference.eu-central-1.acme-ai.example"
      - "inference.ap-northeast-1.acme-ai.example"
    allowPrivateIp: false
    requireTls: true
  hooks:
    - "provider:build_validation_probe"
    - "provider:verify_validation_result"
    - "provider:transform_request"
    - "provider:transform_response"
    - "provider:transform_stream_chunk"
    - "provider:normalize_error"
    - "provider:extract_usage"
    - "provider:inspect_native_request"
  payloadAccess:
    inspectRequestBody: true
    inspectResponseBody: true
    maxInlineMetadataBytes: 262144         # <= 256 KiB metadata JSON; > 64 KiB media passed as BlobRef handles
```

### 2.3. Canonical `ExtensionRuntimeService` Hook Reference (`runtime.proto`)

| RPC / Hook Method | Host-to-Guest Input | Guest-to-Host Output | Security & Reliability Invariants |
|---|---|---|---|
| **`BuildValidationProbe` -> `VerifyValidationResult`** | `ValidationProbeInput` -> Host executes via `BlindBroker` -> `ValidationResultInput`. | `UpstreamHttpSpec` -> `ValidationVerdict`. | Two-phase blind probe: Guest opens `0` network sockets and sees `0` raw secret bytes; Host strips `Set-Cookie` and echoed secrets before Phase 2. |
| **`TransformRequest`** | `TransformRequestInput` (`normalized_request_json < 256 KiB`, `BlobRef[]`, `secret_slot_refs`). | `UpstreamHttpSpec` (`method`, `target_url`, `safe_headers`, `body_bytes`, `outbound_blob_splices`, `credential_injections`). | Host re-verifies `target_url` against `allowedHosts` + connect-time `SSRFGuard` and rejects `Cookie`/`Set-Cookie` in `field_name` (`NEG-3`). |
| **`TransformResponse`** | `TransformResponseInput` (`http_status`, `sanitized_headers`, `raw_response_body`, `modality`). | `NormalizedInferenceResponse` (`normalized_response_json`, `NormalizedUsage`, `finish_reason`). | Synchronous response normalization (`stream=false` chat, responses, embeddings, rerank) within `<= 15 ms` (`TARGET / UNVERIFIED`). |
| **`TransformStreamChunk`** | `StreamChunkInput` (`session_state <= 64 KiB`, `raw_upstream_chunk`, `is_eof`). | `StreamChunkOutput` (`next_session_state <= 64 KiB`, `normalized_sse_frames[]`, `incremental_usage`, `stream_completed`). | Incremental chunk relay (`<= 5 ms` ceiling — `TARGET / UNVERIFIED`); once `>= 1` chunk flushes to client, Host forbids fallback retries (`FMEA-01`). |
| **`NormalizeError`** | `NormalizeErrorInput` (`http_status`, `sanitized_headers`, `sanitized_body`). | `CanonicalProviderError` (`canonical_code`, `retryable_pre_stream`, `cooldown_ms`, `trigger_connection_reauth`). | Drives `SM-02` / `SM-04` transitions deterministically; Host pre-scrubs echoed upstream secrets (`CT-16`). |
| **`ExtractUsage`** | `ExtractUsageInput` (`raw_response_or_trailer_bytes`, text-only byte fallbacks, `BlobRef[]`). | `NormalizedUsage` (5 token counters + `MediaUsage` + `UsageAccuracy`). | Never accepts synthetic `EXACT = 0` on non-empty responses missing upstream `usage` (`FMEA-04`). |
| **`InspectNativeRequest`** | `NativeRequestInput` (`dialect`, `upstream_path`, `sanitized_headers`, `raw_request_body`, `BlobRef[]`). | `NativeRoutingHints` (`target_model`, `stream`, `required_capabilities`, `max_output_tokens_hint`, `credential_injections`). | Enables 6-step capability routing, policy checks, and Phase-1 budget holds on lossless `/native/{dialect}/{upstreamPath}` calls. |
| **`EvaluateGuardrail`** | `GuardrailInput` (`PRE_REQUEST` or `POST_RESPONSE`, `payload_json`). | `GuardrailVerdict` (`allowed`, `violation_code`, `redacted_payload_json`). | Honors per-policy `FAIL_CLOSED` vs `FAIL_OPEN` posture (`SPEC-CORE-01`). |
| **`CancelExecution`** | `CancelExecutionSignal` (`request_id`, `reason`). | `CancelAck` (`released`). | Host tears down upstream TCP/HTTP2 sockets directly within `< 500 ms` (`TARGET / UNVERIFIED`), independent of Guest cooperation. |

### 2.4. Versioning & Backward Compatibility Policy
- **Additive-Only Evolution:** Minor contract revisions within `0.x` or `1.x` introduce only optional fields with safe defaults; field numbers and wire types in `runtime.proto` are never reused or mutated (`buf breaking` `FILE` rule in [`buf.yaml`](../../buf.yaml)).
- **Backward Support Window (`N-1` / `12 months` — `TP-NFR-0902`):** During `0.x`, Tanpopo Core supports at least `N-1` minor contract versions. From `v1.0` GA onward, deprecations carry a minimum **12-month** compatibility window verified via `tanpopo plugin migrate-check` and `tanpopo upgrade preflight`.

---

## 3. Three Extension Execution Tiers, Phased Delivery & Canonical Quotas

> **Absolute Security Invariant (`TP-NFR-0401`):** Untrusted or third-party extension code **never executes in-process** inside Tanpopo Core's memory space (`dlopen`, Go `plugin`, `eval()`, or unisolated JS/Python `import()` are unconditionally prohibited). Per `CR-02`, **`Tier-1 Declarative` (`TP-FR-0301`) and `Tier-2 Wasm Sandbox` (`TP-FR-0302`) ship in `MVP (v0.1)`**, while **`Tier-3 Isolated Sidecar` (`TP-FR-0308`) ships in `v0.3`**.

### 3.1. Comparison & Trade-Off Matrix Across the Three Execution Tiers

| Dimension | **Tier 1: Declarative Package (`v0.1`)** | **Tier 2: Sandboxed Wasm (`wazero` — `v0.1`)** | **Tier 3: Isolated Sidecar (`ConnectRPC` — `v0.3`)** |
|---|---|---|---|
| **Technical Nature** | Static `.tpkg` configuration artifact (YAML/JSON + bounded non-Turing-complete mapping operators). | Compiled `.wasm` bytecode running inside pure-Go `wazero` (`CGO_ENABLED=0`) with `0` WASI FS/env/socket imports. | Standalone OCI container / OS process communicating over `0600` UDS or mutual TLS (`ConnectRPC`). |
| **Target Coverage** | ~75–80% of OpenAI-compatible REST/SSE providers (`OpenAI`, `OpenAI-Compatible/vLLM`, `Ollama`). | ~15–20% of bespoke wire/SSE providers (`Anthropic`, `Gemini`, `AWS Bedrock` in `v0.2`) and fast guardrails. | ~5% of heavy enterprise integrations: native C/PKCS#11 HSM signers, ML PII classifiers, or `stdio` MCP bridges. |
| **Primary Advantage** | **`0` arbitrary code execution surface**; `< 2.0 ms p95` target overhead (`TARGET / UNVERIFIED`). | Cross-OS single-binary portability; hard `16 MiB` linear-memory isolation; `< 5.0 ms p95` target overhead (`TARGET / UNVERIFIED`). | Reuses native enterprise libraries outside Wasm toolchain constraints. |
| **Who May Install?** | `OPERATOR` (enabled per Org by `ORG_OWNER`). | `OPERATOR` after `Manifest` permission consent (`SECURITY_REVIEWER` under `Four-Eyes`). | **Infrastructure `OPERATOR` exclusively.** |

### 3.2. Canonical Sandbox Quotas & Fault-Isolation Constants (`TARGET / UNVERIFIED`)

| Control Boundary | Tier 1: `Declarative` | Tier 2: `Sandboxed Wasm` (`wazero`) | Tier 3: `Isolated Sidecar` (`v0.3`) |
|---|---|---|---|
| **Filesystem & Env** | **None** (`0` FS, `0` `os.Environ`). | **None** (`0` WASI FS mounts, empty env). | `read-only rootfs`, `nonroot` UID `65532`, bounded `/tmp` tmpfs only. |
| **Network Egress** | Exclusively via Host `BlindBroker` + `SSRFGuard` locked to `allowedHosts`. | Zero guest socket syscalls; all HTTP probes/calls flow via Host `BlindBroker` + `SSRFGuard`. | Default-deny `NetworkPolicy`; egress restricted to Tanpopo Egress Proxy / approved hosts. |
| **Hook SLO Targets vs. Hard Watchdogs** | `< 1 ms` structural evaluation (`TARGET / UNVERIFIED`; `4.16–5.37 µs/op` in isolated `SPIKE-01`). | **SLO Targets (`TARGET / UNVERIFIED` — NOT proven by microbenchmarks):** `< 5.0 ms p95` request/response hook, `< 1.0 ms p95` stream chunk (`0.67–0.81 µs/op` in isolated `SPIKE-02` harness). **Cancellation & Hard Watchdogs (armed strictly after pool acquisition):** `context.AfterFunc(parentCtx, ...)` aborts running Wasm immediately on caller disconnect, while 1 shared pool watchdog (`CloseWithExitCode`) enforces `>= 50–200 ms` hard kill ceilings above OS `15.6 ms` scheduler preemption quantum. | **`<= 50 ms`** per guardrail/IPC call (`cgroups` `0.5–2.0 vCPU`) backed by circuit breaker (`TARGET / UNVERIFIED`). |
| **Memory Ceiling (`TARGET / UNVERIFIED`)** | Bounded `<= 2 MiB` parser arena (`<= 32` JSON depth, `<= 10,000` steps — `FZ-01`). | Hard **`16 MiB` (`256` pages)** linear memory ceiling per Wasm instance (`> 64 KiB` blobs externalized via `BlobRef`). | Container `cgroups` memory limit (`OOM-kill` isolated from `tanpopo` Core). |
| **Trap & Auto-Quarantine (`FMEA-05`)** | Rejects invalid expressions at install time. | Trap/timeout/OOM permanently closes and replaces the tainted `api.Module` from pre-compiled `CompiledModule` (`0` pool reuse; healthy calls zero input/output slices via `clear`) and returns **`502 extension_execution_fault`**; **`> 20` faults / `60s`** trips `ALT-PLUGIN-TRAP` -> **`QUARANTINED`**. | Per-sidecar circuit breaker + `ALT-PLUGIN-TRAP` auto-quarantine on repeated IPC faults. |

---

## 4. `M0` Technical Spike Closure (`POC-01` & `POC-02` Verified in `TP-IMP-0004`)

Full reproducible benchmarks and invariants are recorded in [`docs/architecture/spike-results.md`](../architecture/spike-results.md) (`tools/spikes/`):
1. **`POC-01` (`SPIKE-02` — `LOCKED`):** Locked the **Linear-Memory Slice ABI `(ptr: i32, len: i32) -> packed u64 ((outPtr << 32) | outLen)`** with `wazero` (`CGO_ENABLED=0`), achieving `673.8–814.6 ns/op` in isolated microbenchmarks and `1,000 / 1,000` simultaneous loopback SSE streams (`50,000` chunk calls, `0` errors, `0` canary secret bytes in Wasm RAM) when enforcing `context.AfterFunc` in-flight cancellation, shared atomic-slot timeout watchdogs, tainted-module replacement, and full pool recovery. Isolated spike figures do **not** prove full Gateway `SLO-02`/`SLO-03`, which remain `TARGET / UNVERIFIED` (`COND-PO-01`) until `TP-IMP-0043`.
2. **`POC-02` (`SPIKE-01` — `LOCKED`):** Validated the non-Turing-complete Tier-1 Declarative operator grammar across **`9 / 10` (`90%`)** representative providers (`<= 48` lines per manifest, `<= 32` depth guard); `AWS Bedrock` (`SigV4` + binary `EventStream`) is cleanly assigned to Tier-2 Wasm (`v0.2` per `CR-01`).
3. **`POC-03` (`Contract Verified in M0`, Runtime in `M4` / `v0.3`):** `proto/tanpopo/extension/v1/runtime.proto` compiled with `0` errors via `protocompile`; `ConnectRPC` over `0600` UDS and `mTLS` (`AUTH-08`) executes in `TP-IMP-0042` (`TP-FR-0308`).
