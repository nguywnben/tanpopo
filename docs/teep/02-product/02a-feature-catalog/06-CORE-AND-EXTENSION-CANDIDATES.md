# 06 — Core Kernel vs Extension Ecosystem Placement Candidates

- **Document ID:** `TEEP-02A-DOC-06`
- **Mission:** `TEEP-02A` — Proposed Master Feature Catalog
- **Governance Note:** All Core-vs-Extension placements in this document are **candidate product/architectural placements** for review at `TEEP-02B` and formal interface/runtime evaluation at `TEEP-03`. They honor Tanpopo's founding extension-first philosophy while keeping security, isolation, and financial accounting boundaries inside the trusted Core host.

---

## 1. Strategic Placement Philosophy: Trusted Micro-Kernel Host, Rich Extensible Ecosystem

To serve both solo self-hosters (who expect essential providers, guardrails, and notifications to work immediately after downloading one binary) and enterprise/community developers (who need to add custom providers, policies, connectors, UI views, and blueprints without waiting for a Tanpopo Core release), `TEEP-02A` classifies every proposed capability into **six explicit placement tiers**:

| Placement Tier Code | What Lives Here | Shipped In Single Binary? | Upgradable Without Core Release? | Execution & Security Boundary |
|---|---|---|---|---|
| **`CORE_DATA_PLANE`** | Ingress protocol routers, `ConfigSnapshot` 6-step resolver, two-phase FinOps ledger/lease engine, `BlindHostCredentialBroker`, `SSRFGuardDialer`, `wazero` host sandbox, SSE/EventStream framing, exact cache, rate-limit bulkheads. | **Yes** (compiled into `tanpopo`) | Via Core binary upgrade | Trusted Go host memory (`CGO_ENABLED=0`); enforces non-overridable security and monetary invariants. |
| **`CORE_CONTROL_PLANE`** | Multi-tenant IAM/RBAC/ABAC, 3-tier Envelope Vault & KMS orchestration, JCS audit hash chains, Four-Eyes workflow, Blast-Radius/What-If engines, Config Revision History, Astryx Neutral Web Console shell, Declarative Extension UI host renderer, Update/Advisory/Fleet controllers, CLI. | **Yes** (compiled into `tanpopo` + `go:embed` Console) | Via Core binary upgrade | Trusted Go Control Plane + PostgreSQL `FORCE RLS` / SQLite WAL + CSP-locked Astryx Console origin. |
| **`HYBRID_CORE_CONTRACT_PLUS_EXTENSION`** | Host-side execution pipeline + built-in baseline implementation in Core, paired with open `.tpkg` extension hooks (`runtime.proto` `v1.1` + Declarative UI schema) for custom/vendor plugins (e.g., Guardrails, Semantic Cache, Routing Scorers, Notification Sinks, Context Transformers). | **Host contract + baseline in binary; plugins in `.tpkg`** | **Yes** (new `.tpkg` plugins added anytime) | Host enforces timeouts, memory caps, `BlindHostCredentialBroker`, `SSRFGuard`, and monotonic policy ceilings; plugins supply domain logic. |
| **`OFFICIAL_BUNDLED_EXTENSION`** | First-party `@tanpopo/*` provider adapters, built-in deterministic PII/secret/Unicode guardrails, signed baseline Model Intelligence Pack, and starter Solution Packs embedded in the release distribution. | **Yes** (`builtin.Catalog` `.tpkg` assets) | **Yes** (hot-upgradable via signed `.tpkg` / `.tpbundle`!) | Runs inside identical Tier-1 declarative or Tier-2 `wazero` (`16 MiB`, `50 ms`) sandboxes — zero privileged backdoors over community plugins. |
| **`MARKETPLACE_OR_PRIVATE_EXTENSION`** | Community, partner ISV, and private enterprise `.tpkg` packages distributed via the Public Marketplace, private Git repos, local archives, OCI `1.1` registries, or `.tpbundle` couriers (long-tail providers, vendor neural guardrails, SIEM/Snowflake sinks, OpenAPI-to-MCP connectors, enterprise Solution Packs, custom extension UI reports). | **No** (installed on demand) | **Yes** (`100%` decoupled from Core releases) | Governed by `5`-tier `org_extension_policies`, `Delta-P` permission consent, `tanpopo.lock` digest pinning, Tier-1/Tier-2 Wasm or Tier-3 OS-isolated sidecars (`FEAT-101`), and host-rendered Declarative UI (`ARCH-018`). |
| **`EXTERNAL_TOOLING_AND_INTEGRATION`** | `terraform-provider-tanpopo`, `tanpopo-operator` (Kubernetes CRD controller), GitHub Actions / GitLab CI What-If & Eval gates, guest SDK libraries (`tanpopo-sdk-{go,rust,ts}`), and Public Marketplace static index CI. | **Separate CLI/Provider/SDK artifacts** | **Yes** (independent versioned releases) | Authenticates solely via public Admin/Data-Plane APIs (`sak_tp_*`, `WIF`) with `If-Match` concurrency control. |

---

## 2. Seven Non-Overridable Core Host Invariants (What Extensions Can Never Bypass)

Regardless of which extension category (`1`..`11` + Declarative UI) is invoked, third-party extensions **cannot**:
1. **Read Raw Credentials or Bypass `BlindHostCredentialBroker` (`ARCH-008`):** Extensions reference logical `secret_slot` names; host edge injection applies API keys, `SigV4`, `mTLS`, or `RFC 8693` OBO tokens on outbound sockets outside Wasm linear memory.
2. **Open Unchecked Network Sockets or Bypass `SSRFGuardDialer` (`ARCH-009`):** Every outbound HTTP/OCI/Git/Sidecar call is mediated by host-pinned DNS resolution, CIDR allowlists, and `169.254.0.0/16` IMDS blocking.
3. **Widen Parent Tenant Residency, Trust, or `RBAC`/`ABAC` Ceilings (`FEAT-024`, `FEAT-063`):** Custom `ROUTING_STRATEGY` or `CONTEXT_TRANSFORMER` plugins execute *after* Core residency, `BAA`/`ZDR`, and capability pruning and cannot re-admit disqualified targets.
4. **Mutate Settled Financial Ledgers or Bypass Pre-Request Budget Holds (`FEAT-037`):** Custom `CATALOG_FEED` or `PROVIDER_ADAPTER` usage reports pass through host `u128` overflow bounds, `±25%` price-swing quarantine gates, and `ALT-UNMETERED-SPEND` anomaly detectors.
5. **Break PostgreSQL `FORCE RLS` or Tamper with `RFC 8785 JCS` Audit Chains (`FEAT-070`):** Extensions have zero direct SQL connection access; state persistence uses tenant-prefixed, quota-bounded host key-value/metadata APIs.
6. **Execute Arbitrary Third-Party JavaScript in the Web Console Origin (`FEAT-104`, `ARCH-018`, `REJ-002`):** Extension-contributed pages, widgets, reports, settings forms, and contextual row actions are described via schema-validated declarative descriptors rendered by host `@astryxdesign/core` components (`DEC-CAND-17`).
7. **Bypass `Delta-P` Permission Consent or Emergency Quarantine (`FEAT-058`, `FEAT-107`):** Upgrading an extension that requests broader host capabilities (`network`, `payload_read`, `ui_slots`, `plane`) blocks automatic activation until explicit admin / Four-Eyes approval, and `FaultGuard` / revocation feeds can sever an extension in `< 5s` (`[HYPOTHESIS]`).

---

## 3. Complete Extension Category Map (`11` Backend/Declarative Categories + Safe UI Contributions)

| # | Extension Category | Candidate Execution Tiers | Representative Capabilities Powered Without Core Releases | Anchor `FEAT-*` |
|---|---|---|---|---|
| **1** | `PROVIDER_ADAPTER` | `T1` Declarative, `T2` Wasm, `T3` Sidecar | Official & community LLM, embedding, rerank, OCR, audio, image, and GPU-pool (`vLLM`/`llm-d`) protocol adapters + model discovery. | `FEAT-009`, `010`, `019`, `100` |
| **2** | `AUTH_PROVIDER` | `T2` Wasm, `T3` Sidecar | Custom enterprise IdP token verifiers, regional HSM / Azure Key Vault / PKCS#11 KMS wrappers, custom STS workload claims mappers. | `FEAT-013`, `059`, `064`, `100`, `101` |
| **3** | `ROUTING_STRATEGY` | `T2` Wasm (`<= 1 ms`, `0` net) | Custom latency/cost/carbon/SLO candidate scorers running over Core-filtered route targets. | `FEAT-028`, `100` |
| **4** | `GUARDRAIL_PROCESSOR` | `T1` Rules, `T2` Wasm, `T3` Sidecar/HTTP | Built-in deterministic PII/secret/Unicode scanners (`@tanpopo/guardrails-core`) + connectors for local ONNX (`GLiNER`), NeMo, Bedrock, Azure, ModelArmor, Lakera. | `FEAT-054`..`057`, `100`, `101` |
| **5** | `CACHE_STRATEGY` | `T2` Wasm, `T3` Sidecar/HTTP | Pluggable tenant-prefixed vector/semantic cache backends (`pgvector`, `Qdrant`, `Valkey-Search`, `Weaviate`) & domain canonicalizers. | `FEAT-045`, `100` |
| **6** | `CONTEXT_TRANSFORMER` | `T1` Template, `T2` Wasm | Mandatory governance system preambles, MCP JSON-Schema prose compaction, and BlindBroker-mediated RAG enrichment. | `FEAT-080`, `089`, `133`, `100` |
| **7** | `TELEMETRY_EXPORTER` | `T1` Mapping, `T2` Wasm, `T3` Sidecar | Async analytical offload sinks (`ClickHouse`, `S3 Parquet`/`Iceberg`, `Splunk HEC`, `Sentinel`, `Datadog`, `Snowflake`) & cloud billing reconcilers. | `FEAT-043`, `052`, `117`, `100` |
| **8** | `NOTIFICATION_SINK` | `T1` Webhook Template, `T2` Wasm | Custom chat, ticketing, paging, SMS, and regional IM connectors with verified `HITL` approval callbacks (complementing built-in In-App, Email, Telegram, Webhook). | `FEAT-113`..`116`, `100` |
| **9** | `CATALOG_FEED` | `T1` Signed Pack, `T2` Feed Parser | Offline signed Model Intelligence Packs + custom internal FinOps rate-sheet importers. | `FEAT-018`, `020`, `100` |
| **10** | `TOOL_CONNECTOR` | `T1` OpenAPI/GraphQL Map, `T2` Wasm, `T3` Sidecar | Zero-code OpenAPI 3.1 / GraphQL-to-MCP tool virtualization and supervised `stdio` container bridges. | `FEAT-081`, `082`, `100` |
| **11** | `SOLUTION_PACK` | `T1` Pure Declarative (`.tpkg` / OCI, `0` code) | Portable, `@sha256`-pinned reference blueprints (GDPR Support Gateway, HIPAA Clinical RAG, Safe Coding-Agent Stack) applied via `tanpopo pack plan \| apply`. | `FEAT-147`, `100` |
| **+ UI** | **Declarative UI Contributions (`ui_contributions` manifest block)** | `T1` Declarative Schema + Host RPC (`ARCH-018`) | Extension configuration forms, custom extension sub-pages, dashboard metric/table widgets, custom compliance/FinOps report views, and contextual row actions on Traces/Models/MCP Servers. | `FEAT-104`, `ARCH-018` (`DEC-CAND-17`) |
