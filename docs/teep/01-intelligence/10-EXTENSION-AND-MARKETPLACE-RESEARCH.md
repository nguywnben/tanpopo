# 10 — Community Extensibility, SDK & Curated Marketplace Research

- **Document ID:** `TEEP-01-DOC-10`
- **Research Execution Date:** `2026-11-27`
- **Founder Requirements Addressed:** `6.C` (Community Extensibility Without Core Releases) & `6.D` (Curated Official, Community & Private Marketplace)
- **Non-Negotiable Invariants Preserved:** `CGO_ENABLED=0` (`ADR-0001`), Zero-Ambient-Authority `wazero` Sandbox & `BlindHostCredentialBroker` (`ADR-0002`, `CR-05`), Strict 4-Plane Separation (`DEC-003`), Commercial/Self-Hosted ToS Firewall (`DEC-004`), and `Apache-2.0` License Cleanliness (`CONSTRAINTS.md`).

---

## 1. WebAssembly Runtime & Cross-Ecosystem Architecture Verdict (`2026–2028`)

### 1.1 `wazero` (`CGO_ENABLED=0`) vs WASI 0.2 (`wasip2`) Component Model (`[VERIFIED_FACT]`, `SRC-079`..`082`)
1. **Upstream `wazero` Reality:** Core `wazero` (`v1.11.0`, pinned in `go.mod`) explicitly declined native WASI Preview 2 (`#2289`), Component Model (`#2200`), and `wasi-http` (`#2350`) until W3C finalization, prioritizing zero-dependency pure-Go stability (`CGO_ENABLED=0`).
2. **Why `wasmtime-go` & `wasi-sockets` Are Rejected for Tanpopo:** `wasmtime-go` mandates `CGO_ENABLED=1` (linking multi-MB C/Rust `libwasmtime`, breaking static builds and importing trampoline attack surface such as `CVE-2025-62711`), while `wasi-sockets`/`wasi-http` grant ambient guest network idioms incompatible with `BlindHostCredentialBroker` edge credential injection.
3. **Why In-Process `.so` / JS Federation Are Rejected:** Envoy `v1.34–v1.40` Dynamic Modules and Maxim Bifrost Go `.so` plugins have **zero memory sandboxing** and break on minor compiler bumps; Backstage (`RHDH 2.1`, Oct 2026) deleted `40,000` LOC of `Scalprum` runtime JS federation due to React upgrade fragility (`[VERIFIED_FACT]`).
4. **Recommended Tanpopo SDK & Runtime Strategy:**
   - **Host Kernel:** Retain `wazero` (`CGO_ENABLED=0`) + Tanpopo's linear-memory Protobuf slice ABI (`14.57 µs/op` full `TransformRequest` `[VERIFIED_FACT]`, `0` WASI FS/env/socket imports, `16 MiB` default cap, `50 ms` watchdog) + supervised Tier-3 ConnectRPC sidecars (`UDS` / `SPIFFE mTLS`) for heavy ONNX/HSM workloads.
   - **Polyglot Guest SDKs:** Ship official generated SDKs (`tanpopo-sdk-go`, `tanpopo-sdk-rust`, `tanpopo-sdk-ts` via pre-initialized `QuickJS-ng` Wasm snapshots) plus optional build-time WIT-to-Core-Wasm lowering inside `tanpopo plugin pack`.
   - **Console UI Extensibility Without XSS:** Ban third-party JavaScript execution in the Admin Console origin; extensions declare localized **JSON Schema + `uiSchema`** descriptors rendered safely by Astryx Neutral form primitives.

---

## 2. Eleven Extension Categories Without a New Core Release (`runtime.proto` `v1.1`)

In frozen RC9, **only `PROVIDER_ADAPTER`** executes at runtime (`AUTH_DRIVER`, `GUARDRAIL_POLICY`, and `TELEMETRY_EXPORTER` are `ENUM_OR_CONTRACT_ONLY` in `manifest_validator.go:46-50`). Expanding `.tpkg` additively (`buf breaking` clean, tags `11..19` on `ExtensionRuntimeService`) unlocks **11 community and enterprise extension categories** while keeping the 7 Core security invariants (`AuthN`, `FORCE RLS`, `DataRegion`, `HardLimitMicros`, `BlindBroker` + `DEC-004` Cookie ban, `SM-06` MCP HITL, and JCS Audit) mathematically non-overridable:

| # | Extension Category | Allowed Tiers | Runtime Hook & Additive RPC | Resource Ceiling (`HYPOTHESIS / UNVERIFIED`) | Secret / Network / Tenant Fence | Default Fault Posture |
|---|---|---|---|---|---|---|
| **1** | **`PROVIDER_ADAPTER`** | `T1`, `T2`, `T3` | Inference, `/native/*`, & `DiscoverModels` (`BuildDiscoveryRequest` / `ParseDiscoveryResult`). | `<= 15 ms` req/resp · `<= 5 ms` chunk · `16 MiB` (`BlobRef` >64 KiB). | `0` raw secrets in `T1/T2`; `BlindBroker` + `HostCryptoSignSpec`. | Pre-stream failover (`SM-04`); mid-stream clean abort (`FMEA-01`). |
| **2** | **`AUTH_PROVIDER`** | `T2` *(Blind Signer)*, `T3` *(External KMS/Vault)* | Custom enterprise auth header/claim mappers (`T2`) & `ExternalVaultService` (`WrapKey`/`UnwrapKey`/`FetchExternalSecret` for Azure KV, AWS SM, CyberArk, PKCS#11 HSM in `T3`). | `T2`: `<= 5 ms` · `T3`: `<= 50 ms` over `0600` UDS / `SPIFFE mTLS`. | `T2` sees `0` secrets; `T3` `OPERATOR`-only, unwrapped DEKs cached in Host RAM `<= 5m`. | **Unconditional `FAIL_CLOSED`** (`503`). |
| **3** | **`ROUTING_STRATEGY`** | `T1` *(CEL)*, `T2` *(Pure Wasm)* | Step 5 candidate scorer (`ScoreRouteCandidates`) running **strictly after** Core Residency, Trust & Capability pruning. | `<= 1.0 ms` (`< 200 µs` target), `8 MiB`, **`0` network calls**. | Pure function over anonymized health, price, queue & `prefix_hash_64` vector (`0` prompt text by default). | **`FAIL_OPEN_TO_CORE_STRATEGY`** (base `PRIORITY_FAILOVER` / `LOWEST_COST`). |
| **4** | **`GUARDRAIL_PROCESSOR`** | `T1` *(RE2)*, `T2` *(Wasm)*, `T3` *(ML/DLP Sidecar)* | `EvaluateGuardrail` across `PRE_REQUEST`, **`STREAM_WINDOW`** (bounded UTF-8 sliding holdback before SSE flush), `POST_RESPONSE`, `MCP_TOOL_CALL/RESULT`, and `A2A_TASK` — plus **speculative parallel input execution**. | `T1/T2`: `<= 10 ms` pre/post, `<= 2 ms` stream window · `T3`: `<= 50 ms`. | Explicit `inspectRequestBody` consent (`SM-01`); reversible PII token map kept in ephemeral per-request Host RAM (`defer clear()`). | Configurable **`FAIL_CLOSED`** (`403`, mandatory for regulated DLP) or `FAIL_OPEN_LOG` (shadow). |
| **5** | **`CONTEXT_TRANSFORMER`** | `T1`, `T2`, `T3` | Pre-routing enrichment (`TransformContext`): compliance preambles, history compaction, RAG injection, and **MCP Tool Schema AST Compression** (`30–55%` input token savings). | `T1/T2`: `<= 10 ms`, `16 MiB` (`<= 100 ms` if `BlindBroker` RAG subrequest). | Explicit `mutateRequestBody` consent; **Phase-1 budget hold runs AFTER `TransformContext`** so added tokens are `100%` budget-covered! | `FAIL_CLOSED` (compliance preambles) or `BYPASS_ON_FAULT`. |
| **6** | **`CACHE_STRATEGY`** | `T1`, `T2` *(SimHash)*, `T3` *(Local Embedding)* | Pre-dispatch cache key & similarity scorer (`ComputeCacheKey`). | `T1/T2`: `<= 2 ms` · `T3`: `<= 15 ms` (local ONNX embedding). | **Cryptographic Tenant Prefix Fence:** Host prepends `(org_id, project_id, env_id, policy_rev, model_family, tools_hash)` and seals entries with `Project DEK`. | **`FAIL_OPEN_CACHE_MISS`** (normal upstream call). |
| **7** | **`TELEMETRY_EXPORTER`** | `T1`, `T2`, `T3` | **100% Async Out-of-Hot-Path** batch formatter (`ExportTelemetryBatch`) for ClickHouse, Splunk HEC, Datadog, Sentinel, QRadar, S3 Parquet / Iceberg. | `<= 50 ms` per `64–256` event batch (`32 MiB` Wasm). | Receives pre-redacted `C2`/`C3` metadata & JCS audit envelopes only; egress via `BlindBroker`. | **`DROP_OLDEST_WITH_METRIC`** (`ALT-TELEMETRY-DROP`). |
| **8** | **`NOTIFICATION_SINK`** | `T1`, `T2` | Outbox formatter & callback verifier (`FormatNotification`, `VerifyActionCallback`) for Slack Block Kit, MS Teams Adaptive Cards, PagerDuty v2, Jira, ServiceNow. | `<= 15 ms` per event (async outbox + `8`-retry DLQ). | Webhook secrets sealed in Vault; interactive `SM-06` Slack/Teams approvals still verify authenticated OIDC principal + 5-tuple JCS hash! | Outbox retry -> Dead-Letter Queue + Console alert. |
| **9** | **`CATALOG_FEED`** | `T1` *(Signed Pack)*, `T2` *(Feed Parser)* | Control-Plane Model Intelligence ingestion (`ParseCatalogFeed` -> `CatalogProposal`). | Background job (`<= 100 ms`, `32 MiB`). | **FinOps Anti-Poisoning Fence:** Stages diffs; any rate swing `> ±25%` or `$0` commercial rate requires `ORG_OWNER`/`FINOPS` approval; never mutates `ManualOverrides` or past settlements. | Rejects invalid feed; keeps last-known-good catalog. |
| **10** | **`TOOL_CONNECTOR` & `AGENT_MIDDLEWARE`** | `T1` *(OpenAPI-to-MCP)*, `T2`, `T3` | Virtualizes internal REST/GraphQL APIs into governed MCP tools (`TranslateVirtualToolCall`) & scrubs A2A task artifacts. | `<= 15 ms`, `16 MiB`. | Virtual tools register as first-class `mcp_tools` rows subject to `<slug>__<tool>` namespacing, JCS `schema_pin`, and `SM-06` HITL! | **`FAIL_CLOSED`** (`0` auto-retries on `mutating`/`destructive` calls). |
| **11** | **`SOLUTION_PACK`** | **`T1` Only** *(Signed Declarative)* | Transactional Control-Plane / GitOps blueprint (`tanpopo pack plan \| apply`): bundles `@sha256`-pinned `.tpkg` deps + parameterized `Models`, `Routes`, `Cedar Policies`, `Guardrails`, `Budgets`, and `Alerts`. | **`0` runtime code execution** (`<= 2 MiB` YAML/JSON + JSON Schema vars). | Contains **`0` secrets** (prompts operator to bind Vault `secretSlots` at apply time). | Atomic `If-Match` dry-run `plan` + single-TX rollback. |

---

## 3. Curated Marketplace, Keyless Provenance, TUF & Air-Gap OCI Distribution

### 3.1 Defeating 2025–2026 Registry Supply-Chain Vectors (`GlassWorm`, `TeamPCP`, Leaked PATs)
1. **Zero Static Publishing PATs (`100%` Keyless OIDC Sigstore):** Community and Partner publishers **cannot** mint long-lived API tokens to upload `.tpkg` packages. Releases must build in attested CI (`GitHub Actions`, `GitLab CI`, `Buildkite`) using 5-minute OIDC tokens to obtain ephemeral **Sigstore Fulcio** certificates logged to **Rekor**, cryptographically bound to the source repo URL and 40-hex commit SHA (`[HYPOTHESIS]`).
2. **DNS Namespace Binding & `@tanpopo/*` Reservation:** `@vendor/*` namespaces require automated `_tanpopo-publisher.<domain>` DNS `TXT` verification; `^@tanpopo(-[a-z0-9_-]+)?/` remains locked to Tanpopo's offline `3-of-5` root keys (`CR-03`).
3. **Seven-Stage Automated Moderation & `DEC-004` Compliance Pipeline:**
   - **(1)** OIDC repo/commit/DNS check → **(2)** `--network=none` reproducible Wasm rebuild matching submitted `SHA-256` → **(3)** Wasm `ImportSection` whitelist (rejecting any `wasi_snapshot_preview1.sock_*`, `path_open`, `proc_exec`) + `gitleaks` secret scan + SPDX license check → **(4)** **`DEC-004` Consumer-Subscription Firewall** (blocking `chatgpt.com/backend-api`, `claude.ai/api`, browser User-Agent spoofing, and `Cookie` scraping) → **(5)** `tanpopo plugin test` (`CT-01..18`) + 30s Wasm ABI fuzzer → **(6)** CycloneDX `1.6` SBOM + `osv-scanner` (`0` Critical/High CVEs) → **(7)** Human review on initial submission and whenever permission delta $\Delta P \neq \emptyset$.

### 3.2 Five-Tier Trust Model, TUF Anti-Rollback & Harbor/Artifactory Air-Gap Mirroring
- **Five Trust Tiers Mapped to `organizations.min_plugin_trust`:**
  1. `OFFICIAL` (`@tanpopo/*` — offline `3-of-5` root keys; allowed under all policies).
  2. `VERIFIED_PARTNER` (`@anthropic/*`, `@datadog/*` — corporate DNS + OIDC + partner target delegation).
  3. `COMMUNITY_REVIEWED` (`@alice-dev/*` — GitHub OIDC + 7-stage CI + human permission review).
  4. `PRIVATE_ENTERPRISE` (`@internal-bank/*` — customer KMS/Ed25519 key in `/etc/tanpopo/trusted-keys/`, never uploaded publicly).
  5. `UNVERIFIED` (sideloaded `.tpkg` — blocked unless `min_plugin_trust = 'ANY_APPROVED'` + explicit Console/CLI `ORG_OWNER` opt-in + Four-Eyes).
- **TUF (`go-tuf/v2`, `Apache-2.0`/`BSD-3`) + OCI `1.1` (`oras-go/v2`, `Apache-2.0`):**
  - Wraps catalog and `revocations.json` in TUF `root.json` / `targets.json` / `snapshot.json` / `timestamp.json` metadata to defeat freeze, mix-and-match, and downgrade attacks.
  - Publishes every `.tpkg` + CycloneDX SBOM + Sigstore bundle as standard **OCI `1.1` Artifacts** (`ghcr.io/tanpopo-marketplace/...`) so regulated enterprises mirror plugins into air-gapped enclaves using standard **Harbor**, **JFrog Artifactory**, **Nexus**, or **ECR** replication rules alongside `.tpbundle` sneakernet archives.
  - Supports environment ring pinning (`dev` / `staging` / `prod` + `5%` canary split), `< 5s` (`HYPOTHESIS / UNVERIFIED`) atomic `tanpopo.lock` rollback, and `CRITICAL_MALICIOUS` auto-quarantine with automated forensic blast-radius reports.
