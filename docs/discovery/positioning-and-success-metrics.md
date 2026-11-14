# Product Positioning, Success Metrics, Release Scope & Discovery Experiments — Tanpopo

> **Discovery Document (Phase ASTRA A01 — Gate G1: `READY_FOR_REVIEW`)**
> - **Last Updated:** `2026-11-11`
> - **Authored By:** Product Intelligence Lead
> - **Status:** Strategic product proposal (`PROPOSED`), submitted to the Product Owner for review at Gate G1. Not locked as mandatory requirements until formally approved.

---

## 1. Proposed Positioning Statement (`PROPOSED`)

### 1.1. Core Positioning Framework

- **For:** **Product Development Teams (Dev Teams of 5–50 engineers)** and **AI Platform Teams** operating multi-model applications, MCP tools, and autonomous agents in production,
- **Who struggle with:** Being trapped between open-source AI Gateways that hardcode hundreds of integrations into their core (causing upgrade regressions and locking baseline security governance behind expensive Enterprise paywalls) and heavyweight cloud proxies that demand Kubernetes/Istio or extract percentage-based usage fees,
- **Tanpopo is:** An **open-source (Apache-2.0), self-hosted-first, and security-isolated extension-first AI Gateway**,
- **That delivers:** Unified orchestration, security policy enforcement, observability, and budget governance across 3 distinct traffic planes (**Model Providers, MCP Tools, and Agent-to-Agent / A2A**), while enabling teams to install or author sandboxed **Provider Adapters and Extensions** directly from the Marketplace, Git, URLs, or private registries **without upgrading Tanpopo Core**,
- **Unlike LiteLLM, Portkey, Agentgateway, or OpenRouter:** Tanpopo pairs a single-command startup experience with a built-in Web Console + CLI alongside a sandboxed Plugin runtime, automated contract testing, and strict adherence to authorized connectivity methods (zero paywall traps on baseline self-hosted security, and zero reliance on grey-market consumer subscription circumvention).

### 1.2. One-Line Pitch (`PROPOSED`)
> **"Tanpopo — The self-hosted AI Gateway powered by sandboxed plugins: Unify governance across Models, MCP Tools, and A2A Agents without waiting on Core upgrades."**

---

## 2. Success Metrics Framework (North Star Metric & Measurable Outcomes — `PROPOSED`)

### 2.1. North Star Metric (NSM) for Self-Hosted Open-Source Infrastructure

Because Tanpopo is self-hosted-first and strictly respects user data privacy (*zero forced telemetry*), its measurement model combines two complementary lenses:

1. **Product-Value North Star (Displayed locally on every Tanpopo instance & aggregated anonymously only when operators explicitly opt in):**
   - **Metric:** **Weekly Governed Multi-Plane Requests (`WGMR`)**.
   - *Definition:* A request counts toward `WGMR` if and only if it traverses Tanpopo's complete core value chain: (1) valid Virtual Key/Identity authentication, (2) Policy & budget quota evaluation, (3) execution through a contract-verified Adapter/Plane (`Provider`, `MCP`, or `A2A`), and (4) successful settlement into the cost/telemetry ledger.
   - *Rationale:* Reflects genuine day-to-day operational reliance rather than vanity GitHub stars or one-off container downloads.
2. **Ecosystem-Differentiation North Star (Publicly measurable with zero telemetry):**
   - **Metric:** **Zero-Core-Release Onboarding Rate** paired with **Verified Contract-Passing Extensions**.
   - *Maturity Target (`v1.0`):* `>= 90%` of newly launched AI models or providers in the market are onboarded by the community or maintainers solely by publishing or updating an Adapter package without releasing a new Tanpopo Core binary.

### 2.2. Measurable Technical & UX Outcomes (SLO Hypotheses)

| Goal Category | Metric | Proposed Target Threshold (`PROPOSED`) | Practical Rationale |
|---|---|---|---|
| **Onboarding UX** | **Time-to-First-Governed-Request (`TTFGR`)** | **`< 3 minutes`** from downloading the container/binary to completing the first logged & cost-metered request | Guarantees real Day-1 usability for Solo Devs and small teams without hours of manual setup. |
| **Plugin Author UX** | **Time-to-First-Custom-Adapter (`TTFCA`)** | **`< 30 minutes`** for a Declarative pack (Tier-1) and **`< 2 hours`** for a Wasm pack (Tier-2) using SDK templates | Lowers authoring friction to break the Marketplace "chicken-and-egg" loop. |
| **Data-Plane Performance** | **Gateway Internal Latency Overhead (`p95`)** | **`< 2 ms`** (Tier-1 Declarative) / **`< 5 ms`** (Tier-2 Wasm Sandbox), excluding upstream AI provider network latency | Preserves snappy streaming responsiveness for interactive models and agents. |
| **Self-Hosted Footprint** | **Idle RAM Consumption (Standalone Mode)** | **`< 128 MB RAM`** (encompassing Core process, embedded database, and static Web Console assets) | Runs effortlessly on developer laptops or entry-level VPS instances. |
| **Operational & Financial Safety** | **Emergency Key Revocation / Plugin Quarantine Latency** | **`< 1 second`** (standalone mode) / **`< 5 seconds`** (distributed HA cluster mode) | Meets real-world incident containment requirements (`UC-07`, `UC-09`). |
| **Budget Ledger Precision** | **Concurrent Streaming Budget Overshoot Bound** | **`<= 1` maximum in-flight stream margin** via pre-request quota holds (`Pre-reservation`) | Neutralizes runaway agent loops before they drain cloud budgets (`UC-08`). |

---

## 3. Product Boundaries: Core vs Optional Extension vs Out-of-Scope (`PROPOSED`)

Establishing crisp boundaries during Discovery keeps Tanpopo Core lean, fast, and security-auditable while maximizing the `extension-first` architecture.

| Capability Domain | **1. Tanpopo Core (Mandatory Spine)** | **2. Optional Extensions (Official or Community/Private)** | **3. Out-of-Scope** |
|---|---|---|---|
| **Model & Protocol Connectivity** | HTTP/SSE Ingress pipeline (Unified API + Native Passthrough), MCP Plane orchestrator, A2A Plane orchestrator | **All concrete Provider Adapters** (OpenAI, Anthropic, Gemini, vLLM/Ollama, Bedrock, Azure, regional providers, etc.) | Hosting model weights or running GPU/CPU inference engines directly (role of `vLLM`, `Ollama`) |
| **Extensibility Runtime** | Manifest loader & Wasm Sandbox, Blind Credential Broker, lifecycle & lockfile manager, contract test CLI | Custom internal enterprise Adapter packs, specialized enterprise auth transformers (Custom Enterprise Auth) | Executing unsandboxed arbitrary host scripts directly inside the Core host process |
| **Routing, Policy & Security** | Capability-Aware Router, Circuit Breaker, Rate Limiter, RBAC, Virtual Key management, baseline data-residency & MCP tool scoping policies | Deep content inspection filters (advanced PII Redaction, external Guardrail connectors, specialized KMS/Vault backends) | OS-level network intrusion detection systems or L3/L4 perimeter firewalls |
| **Observability & Cost Control** | Hierarchical token/cost ledger, two-phase budget reservation, Audit Log, OpenTelemetry (OTLP) exporter, bounded local trace logs | Specialized data exporters targeting third-party analytics platforms or proprietary cloud object stores | Large-scale LLM-as-a-Judge evaluation suites, dataset labeling, or model fine-tuning pipelines |
| **Surfaces & Distribution** | Admin Web Console (built on Astryx) and `tanpopo` CLI; Registry/Git/URL index resolver | Organization-hosted Private Registries; optional custom internal UI portals | Credit-reselling payment gateways charging markup spreads, or TOS-violating consumer subscription circumvention/repurposing tools (`REJECTED`) |

---

## 4. Proposed Phased Release Scope (`0.x` to `v1.0` — `PROPOSED`)

The roadmap is structured as end-to-end usable vertical slices where **the foundational `v0.1` domain model already cleanly separates all 4 layers (`Provider`, `Plugin`, `MCP`, `A2A`)**, eliminating destructive schema rewrites in later milestones:

| Target Milestone | Strategic Focus | Core Deliverables (`PROPOSED`) | Exit Criteria |
|---|---|---|---|
| **`v0.1` — Usable Core & Extension Engine (Pragmatic MVP)** | Prove the `extension-first` architecture in real usage across flagship AI models | - Canonical 4-layer decoupled domain foundation<br>- Unified Model API + Native Dialect Passthrough + Streaming SSE<br>- Two-Tier Plugin Engine (Declarative + Wasm Sandbox) + Blind Credential Broker + Lockfile<br>- 5–6 Official Reference Adapters (OpenAI, Anthropic, Gemini, OpenAI-Compatible for Ollama/vLLM, Bedrock/Azure)<br>- Virtual Keys, Rate Limiting, baseline Capability-Aware Routing, Budget Ledger & baseline OTel (Standalone SQLite mode)<br>- `tanpopo` CLI (server lifecycle, plugin install/test) + Core Astryx Web Console (Providers, Plugins, Keys, Budgets, Logs) | An engineering team downloads Tanpopo, starts it in `< 3 minutes`, routes traffic across 3 cloud AI vendors + local Ollama, and installs 1 external Adapter from URL/Git without recompiling Core. |
| **`v0.2` — MCP Governance Plane & Registry Ecosystem** | Expand into agent tool governance & mature the Plugin publishing workflow | - First-class **MCP Plane** (Streamable HTTP/SSE, per-key tool filtering, upstream credential injection, `schema_hash` pinning against Rug-Pulls)<br>- Authorized OAuth 2.1 & baseline OIDC primitives in SDK<br>- Static public Extension Catalog Index + Private Registry support + `plugin test --replay` cassette tooling<br>- Dry-run route & policy simulator (`/v1/route/explain`) | Coding agents (IDE/CLI) connect to both LLMs and 5+ MCP servers through a single Tanpopo gateway with strict tool-level ACLs; third-party authors publish contract-verified Adapters independently. |
| **`v0.3` — A2A Connectivity Plane & Clustered Scale-Out** | Unlock multi-agent orchestration and high-availability enterprise scale | - Dedicated **A2A Plane** (Agent Card registry/discovery, asynchronous task delegation, cross-agent policy enforcement, end-to-end correlation tracing)<br>- Stateless **Scale-out HA** cluster mode backed by PostgreSQL + Redis/Valkey (sub-second key revocation sync and distributed quotas)<br>- Multi-team/project RBAC, external secret backends (KMS/Vault extension), and offline air-gapped bundles (`bundle export`) | Sustains multi-replica streaming load without budget ledger drift; governs and audits asynchronous A2A delegations end-to-end. |
| **`v1.0` — Production & Ecosystem Stability Guarantee** | Long-term contract stability commitment & regulated enterprise readiness | - Frozen backward-compatibility guarantees for **Plugin Contract `v1.0`** and **Unified/Admin API `v1.0`**<br>- Independent security audit of Wasm Sandbox & Credential Broker<br>- Complete production operations kit (Helm charts, Terraform/GitOps workflows, disaster recovery runbooks, admin SSO/OIDC) | Stable Plugin Contract enabling confident long-term community and enterprise investment; passes production-grade load, security, and reliability gates. |

---

## 5. Top 6 Early Discovery & Validation Experiments

Before investing months into full-scale implementation, Tanpopo executes **6 short, low-cost validation spikes with explicit Pass/Pivot thresholds** to retire the highest-risk assumptions early:

| ID | Hypothesis Under Test | Execution Method | Estimated Duration | Pass Criterion / Pivot Path on Failure |
|---|---|---|---|---|
| **EXP-01** | **Streaming Across Wasm Sandbox (`OQ-002`, `ASM-002`):** `.wasm` plugins can transform SSE streams chunk-by-chunk while maintaining low latency and bounded memory. | Build a minimal Host spike ingesting high-throughput simulated SSE streams through a JSON-to-OpenAI Wasm module across 1,000 concurrent connections; measure `p95` latency and RAM. | 2–3 days | **Pass:** Added latency `< 5 ms p95`, zero memory leaks.<br>**Pivot:** Scope Wasm transforms to compact event frames/headers/requests and push common streaming normalization into the native Host Declarative Engine. |
| **EXP-02** | **Declarative Adapter Coverage (`ASM-004`):** Declarative YAML/JSON manifests cleanly express at least 75% of popular commercial/open-source AI providers with zero custom code. | Draft a declarative JSON Schema specification against 10 real providers (OpenAI, Groq, Together, Mistral, DeepSeek, Ollama, OpenRouter, Azure, Anthropic, Gemini) to surface edge-case breaks. | 1–2 days | **Pass:** `>= 7/10` providers fully expressed declaratively; only 2–3 bespoke dialects require Tier-2 Wasm.<br>**Pivot:** Refine manifest mapping expressions before publishing the initial SDK. |
| **EXP-03** | **Adapter Authoring Ergonomics (`TTFCA < 30 minutes`):** A developer unfamiliar with Tanpopo Core internals can author a working Adapter from sample docs in under 30 minutes. | Hand a 1-page draft contract guide and template to 3–5 independent engineers, tasking them with wrapping a model API while observing friction points. | 2 days | **Pass:** At least 4/5 engineers finish cleanly in under 30 minutes with zero verbal hints.<br>**Pivot:** Simplify manifest syntax and improve contract validator error diagnostics. |
| **EXP-04** | **Concurrent Streaming Budget Race Defense (`UC-08`):** Two-phase quota reservation prevents budget cap breaches under burst concurrent SSE streams without throttling throughput. | Build a harness firing 100 simultaneous streams at an account holding budget sufficient for only 5 streams, injecting abrupt mid-stream disconnects. | 1–2 days | **Pass:** Final settled spend never exceeds the cap beyond a 1-stream margin, and 100% of orphaned holds are reclaimed cleanly on network drops. |
| **EXP-05** | **Real-World MCP Client Interoperability (`UC-06`):** Tanpopo's intermediary MCP Plane interoperates seamlessly with production clients (Claude Code, Cursor, Codex, MCP Inspector) under tool ACL filtering and large catalogs (>150 tools). | Stand up a prototype MCP Streamable HTTP endpoint fronting 3 real MCP servers; verify tool discovery, pagination, invocation, and `schema_hash` mutation blocking. | 2 days | **Pass:** Every major client discovers and invokes authorized tools cleanly with zero list truncation or session breakage.<br>**Pivot:** Add per-client transport compatibility shims prior to locking detailed `v0.2` specs. |
| **EXP-06** | **Beachhead User Pain Validation Interviews (`ASM-001`):** Confirm which concrete pain points compel Dev Teams / AI Platform Teams to adopt or migrate to Tanpopo. | Conduct 8–10 structured technical interviews with engineers operating LiteLLM / One-API / custom proxies regarding upgrade incidents, MCP governance, and internal policy pain. | 1 week (parallel) | **Pass:** Empirically validates `v0.1` and `v0.2` feature priorities grounded in real operational pain rather than speculation. |
