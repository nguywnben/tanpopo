# Product Requirements Document (PRD) — Tanpopo

> **Product Definition Document (Phase ASTRA A03 — Gate G2: `READY_FOR_REVIEW` | `DRAFT — UNLOCKED SCOPE`)**
> - **Version:** `0.1.0-step03-draft`
> - **Last Updated:** `2026-11-11`
> - **Authored By:** Principal Requirements Engineer & Product Manager
> - **Governance Status:** **Unlocked-scope draft (`DRAFT`)** because **Gate G1** and **Gate G2** in [`docs/governance/decisions.md`](../governance/decisions.md) remain in `READY_FOR_REVIEW` status (awaiting formal Product Owner approval). Written to verifiable engineering standards with zero promotional hype.
> - **Single Source of Truth (SSOT) Cross-References:**
>   - Canonical Glossary: [`docs/product/glossary.md`](./glossary.md)
>   - Detailed Feature Catalog: [`docs/product/feature-catalog.md`](./feature-catalog.md)
>   - Release Phasing & Scope: [`docs/product/release-scope.md`](./release-scope.md)
>   - System Requirements Specification (`TP-FR` / `TP-NFR`): [`docs/requirements/srs.md`](../requirements/srs.md)
>   - Acceptance Criteria (`TP-AC`): [`docs/requirements/acceptance-criteria.md`](../requirements/acceptance-criteria.md)

---

## 1. Business & Technical Problem Statement

Product engineering teams and AI platform organizations operating multiple models (commercial and self-hosted), MCP tools, and A2A agents face four structural bottlenecks left unresolved by incumbent gateways (evidenced in [`market-and-competitors.md`](../discovery/market-and-competitors.md)):

1. **Tight coupling between Provider integrations and Core release cycles:** Mainstream open-source gateways hardcode hundreds of provider translators inside a single monolithic repository. Whenever an AI vendor launches a new model/parameter or an enterprise stands up an internal model endpoint, operators must either wait for a full Core upgrade (risking global regressions) or maintain costly private forks.
2. **Absence of a security sandbox for third-party extensibility code:** Loading custom scripts directly into the Gateway host process risks exposing the entire master credential vault (`Secret Credential`) or leaking sensitive prompts to unauthorized endpoints; meanwhile, community packages lacking deterministic contract test suites silently rot whenever upstream vendors alter response payloads.
3. **Fragmented governance across Inference (LLM), Tools (MCP), and Agents (A2A), or prohibitive infrastructure/licensing barriers:** Teams must either pay expensive commercial Enterprise licenses for baseline self-hosted security controls (RBAC, hard budgets, audit logs, MCP tool ACLs) or absorb the operational weight of Kubernetes/Envoy/Istio clusters just to achieve a unified policy plane.
4. **Legal liability and reliability hazards from consumer subscription grey zones:** Numerous community tools conflate authorized authentication flows with scraping browser sessions from flat-rate personal subscriptions (`ChatGPT Plus/Pro`, `Codex`, `Claude Pro/Max`, `Antigravity`, etc.), triggering mass upstream account bans and disqualifying adoption in serious production environments.

---

## 2. Target Personas & Actors

Synthesized from the segment ranking in [`users-jobs-use-cases.md`](../discovery/users-jobs-use-cases.md) and the 12 canonical actors in [`business-entity-map.md`](../requirements/business-entity-map.md):

| Priority Tier (`PROPOSED` — Pending `SD-G1-01`) | Organizational Segment | Direct `Actors` | Core Immediate Needs |
|---|---|---|---|
| **Beachhead #1 (`P0`)** | **Dev Teams (5–50 engineers)** | `Organization Owner`, `Team Admin`, `Developer`, `API Client`, `MCP Client` | Start in under 3 minutes; unify official commercial keys & local models behind one gateway; enforce hard budget ceilings immune to concurrent stream races; scope MCP tool invocation permissions. |
| **Co-Beachhead #2 (`P0/P1`)** | **AI Platform Teams (50–500 engineers)** | `Operator`, `Extension Publisher`, `Reviewer`, `Team Admin` | Author and install sandboxed internal `Provider Adapter` packages from Git/Private Registry without modifying Core; enforce `Data Residency` policies and export standard OpenTelemetry/Audit streams. |
| **Community Flywheel #3 (`P1`)** | **Legitimate Solo Developers & Power Users** | `Personal Admin`, `Developer`, `Agent` | Run a single lightweight binary/container (`< 128 MB RAM` idle) managing commercial BYOK credentials, local `Ollama`/`vLLM` engines, and MCP tool allowlists for coding agents. |
| **Later Stage #4 (`v1.0+`)** | **Regulated Enterprise (>1,000 employees)** | `Operator`, `Reviewer`, `External IdP` | Multi-region HA clusters, dedicated HSM/KMS backends, mandatory four-eyes approvals, and offline `Air-gapped Bundle` installations. |

- **Permanently Excluded Cohorts (`CONFIRMED_BY_OWNER` — `REJ-003`):** Users seeking tools to convert personal consumer subscriptions into free/shared API credits; grey-market API key reselling platforms violating upstream terms of service.

---

## 3. Value Proposition, Goals & Non-Goals

### 3.1. Verifiable Core Value Proposition
Tanpopo delivers a **single self-hosted AI orchestration and governance plane** enabling organizations to:
1. Install, upgrade, version-pin, and quarantine **Provider Adapters and Extensions** from the Marketplace, Git, URLs/archives, or Private Registries under a `Default-Deny` capability sandbox **without releasing or recompiling Tanpopo Core**.
2. Uniformly enforce virtual-key authentication, capability-aware routing, data-residency policies, two-phase budget holds, and append-only audit logs across **4 cleanly separated domain layers: `Provider`, `Plugin`, `MCP`, and `A2A`**.
3. Uphold transparent compliance boundaries between `Official Adapters` (strictly prioritizing official commercial APIs and authorized auth flows) and `Community/Private Extensions` (governed by explicit permission grants and honest provenance labels, never endorsing or maintaining TOS-violating connectors).

### 3.2. Product Goals (`Product Goals`)
- **`PG-01` (`CONFIRMED_BY_OWNER`):** Operate pragmatically at small scale (single-command startup with built-in Web Console & CLI) while scaling out horizontally without local state as traffic grows.
- **`PG-02` (`CONFIRMED_BY_OWNER`):** Decouple provider integrations, authentication schemes, and policy filters completely from Tanpopo Core's release cadence via a versioned Plugin Contract, SDK, and conformance test harness.
- **`PG-03` (`CONFIRMED_BY_OWNER`):** Preserve strict data-model, protocol, and authorization boundaries across the 4 independent layers: `Provider`, `Plugin`, `MCP`, and `A2A`.
- **`PG-04` (`PROPOSED`):** Prevent financial overruns and credential exfiltration via pre-stream budget holds (`Pre-Request Hold`), egress-boundary secret brokering (`Blind Credential Injection`), and human-in-the-loop approval for destructive tool executions.
- **`PG-05` (`PROPOSED`):** Guarantee strict protocol and financial accounting honesty (never silently drop unsupported parameters, never silently fail over after flushing a partial stream, and never record fabricated `0 USD` entries when upstream token counts are missing).

### 3.3. Explicit Non-Goals (`Explicit Non-Goals`)
- **`NG-01` (`CONFIRMED_BY_OWNER` / `REJ-001`):** Not a naive API-key round-robin rotator lacking routing, policy enforcement, telemetry, cost controls, and extensibility.
- **`NG-02` (`CONFIRMED_BY_OWNER` / `REJ-002`):** Never hardcode all AI provider integrations inside Tanpopo Core's codebase.
- **`NG-03` (`CONFIRMED_BY_OWNER` / `REJ-003`):** Never develop, package, list on official channels, or rely on "accept-risk" prompts to rationalize repurposing personal consumer subscriptions (`ChatGPT Plus/Pro`, `Codex`, `Claude Pro/Max`, `Antigravity`, etc.) into a general-purpose API.
- **`NG-04` (`CONFIRMED_BY_OWNER` / `REJ-004`):** Never collapse `Provider`, `Plugin`, `MCP`, and `A2A` into a single monolithic entity or catch-all protocol.
- **`NG-05` (`PROPOSED`):** Does not store model weights or execute GPU/CPU model inference directly (role of `vLLM`, `Ollama`, `SGLang`).
- **`NG-06` (`PROPOSED`):** Does not embed application prompt-chaining frameworks (such as LangChain/LlamaIndex) or model evaluation/fine-tuning suites (Evals/Fine-tuning) inside Core.
- **`NG-07` (`PROPOSED`):** Does not operate a commercial credit-reselling payment clearinghouse charging percentage markups inside the core open-source project.
- **`NG-08` (`PROPOSED`):** Never collects mandatory phone-home telemetry from self-hosted user instances.

---

## 4. Measurable Outcomes (*Pre-Benchmark Targets*)

> **Verification Note:** All numerical thresholds below represent **`PROPOSED Pre-Benchmark Targets`** and must be empirically validated via automated load and conformance harnesses prior to locking `v0.1` and `v1.0` releases.

| Outcome ID | Verifiable Metric | Target Threshold | Verification Method |
|---|---|---|---|
| **MO-01** | **Operational North Star (`WGMR`)** | Accurately report Weekly Governed Multi-Plane Requests executed via verified Extensions on every instance | Automated ledger reconciliation & OpenTelemetry exporter conformance tests (`TP-FR-0801`). |
| **MO-02** | **Zero-Core-Upgrade Standard Provider Onboarding Rate** | `100%` for providers conforming to the active frozen Plugin Contract version | Integration test installing a new Adapter package onto an unmodified, un-recompiled Core binary (`TP-AC-F0301`). |
| **MO-03** | **Time-to-First-Governed-Request (`TTFGR`)** | `< 3 minutes` from binary/container download to completing the first governed call via Setup Wizard / CLI | Timed clean-room onboarding harness (`WF-01`, `WF-02`). |
| **MO-04** | **Time-to-First-Custom-Adapter (`TTFCA`)** | `< 30 minutes` for a standard declarative pack (Tier-1) passing `100%` of baseline contract tests | Empirical author study `EXP-03` paired with CLI test runner (`TP-FR-0305`). |
| **MO-05** | **Gateway Internal Latency Overhead (`p95 Overhead`)** | `< 2 ms` (declarative pack) / `< 5 ms` (sandboxed plugin), excluding upstream provider network RTT | Automated benchmark across 1,000 concurrent SSE streams (`TP-NFR-0101`, `EXP-01`). |
| **MO-06** | **Key Revocation / Plugin Quarantine Enforcement Latency** | `< 1 second` (standalone mode) / `< 5 seconds` (distributed HA cluster mode) | Fault-injection revocation test under sustained request load (`TP-NFR-0404`). |

---

## 5. Capability Map & Dependency Direction (`Capability Map`)

Structured along strictly acyclic module boundaries so each domain can be specified, tested, and delivered independently:

| Stable Module ID | Core Responsibility | Direct Dependencies (One-Way) | Architectural Plane |
|---|---|---|---|
| `selfhost-ops` | Instance bootstrap, configuration, persistence, root secret vault, backup & upgrade preflight | — *(Foundational spine)* | Infrastructure & Operations |
| `iam-tenancy` | Tenancy hierarchy (`Org`/`Project`/`Env`), `Principal`, `RBAC`, `Virtual Key` lifecycle | `selfhost-ops` | Control & Data Plane |
| `policy-security` | Policy evaluation engine, data residency, `ApprovalRequest` workflows, append-only `AuditEvent` log | `selfhost-ops`, `iam-tenancy` | Cross-cutting across all 4 layers |
| `telemetry-finops` | `BudgetLedger`, two-phase quota holds, `EXACT`/`ESTIMATED` settlement, OTel export | `selfhost-ops`, `iam-tenancy` | Cross-cutting across all 4 layers |
| `plugin-marketplace` | `Extension` lifecycle, capability sandbox, blind credential broker, contract harness, Registry/Git/ZIP installer | `selfhost-ops`, `policy-security` | **Plugin / Extension** Layer |
| `provider-credentials` | `Secret Credential` vaulting (API Key, Authorized OAuth), `Provider Connection`, `Model`, `Model Deployment` | `iam-tenancy`, `policy-security`, `plugin-marketplace` | **Provider** Layer |
| `routing-resilience` | Capability-aware routing, pre-stream failover, `Circuit Breaker`, dry-run route simulator | `provider-credentials`, `policy-security`, `telemetry-finops` | **Provider** Layer |
| `gateway-protocols` | Unified API endpoints, Native Dialect Passthrough, SSE/Multimodal streaming pipeline, error normalization | `iam-tenancy`, `routing-resilience`, `telemetry-finops` | **Provider Ingress** Layer |
| `mcp-a2a` | **MCP** Plane (server registry, tool ACL filtering, `schema_hash` pinning, unsafe-retry blocking) & **A2A** Plane (`AgentCard`, bounded task delegation) | `iam-tenancy`, `policy-security`, `telemetry-finops`, `plugin-marketplace` | **MCP** Layer & **A2A** Layer |
| `console-cli-api` | Admin REST API, `tanpopo` CLI, and Admin Web Console (Astryx) | All domain modules above | Management & Authoring Surfaces |

---

## 6. Dependencies & Product Constraints

| ID | Category | Mandatory Constraint / Dependency Description | Source / Status |
|---|---|---|---|
| **PC-01** | **Legal & Licensing** | Standardize the entire codebase under a unified permissive open-source license (**Apache-2.0** in [`LICENSE`](../../LICENSE); pending `package.json` alignment in `SD-G1-05`); prohibit embedding viral copyleft libraries (`GPL`/`AGPL`) in Core. | `VERIFIED_IN_REPO` / `OPEN` |
| **PC-02** | **Plugin Security Isolation** | Every external extension must enforce strict resource quotas and default-deny zero network/filesystem/env access unless explicitly declared in its Manifest and approved by an administrator. | `CONFIRMED_BY_OWNER` (`C-02`) |
| **PC-03** | **Upstream Provider Terms Compliance** | Strictly separate technical SDK capabilities from legal Provider Entitlements; never maintain or promote connectors repurposing personal consumer subscriptions as general-purpose APIs. | `CONFIRMED_BY_OWNER` (`C-03`, `DEC-004`) |
| **PC-04** | **Frontend Scaffold Preservation** | Build the Admin Web Console atop **Astryx Design System v0.7.13** with the pre-configured `neutral` theme (strictly prohibiting raw `div`/`span` layout tags, scaffolding from official templates, and passing `astryx doctor`), without forcing the Gateway Core runtime to share the UI's implementation language. | `CONFIRMED_BY_OWNER` & `VERIFIED_IN_REPO` (`C-04`) |
| **PC-05** | **Zero Proprietary Cloud Lock-In** | Core self-hosted mode must operate completely inside private networks (including offline bundle export at maturity) without requiring any hosted Tanpopo cloud account. | `CONFIRMED_BY_OWNER` (`DEC-001`) |
| **PC-06** | **Open Protocol Standards Alignment** | Connectivity planes adhere to public OpenAI API specifications (for unified ingress), native vendor specifications (for Native Passthrough), **Model Context Protocol (MCP)**, **Agent2Agent (A2A)**, and **OpenTelemetry (OTLP)**. | `PROPOSED` |

---

## 7. Phased Roadmap Summary (`MVP v0.1` → `v1.0` → `Post-v1.0`)

The authoritative per-milestone requirement allocation matrix resides exclusively in [`docs/product/release-scope.md`](./release-scope.md) (`DRAFT`):
- **`MVP (v0.1)`:** Resolves core thesis #1 end-to-end — *Core Gateway + Unified/Passthrough API + Isolated Plugin Engine + Official Reference Adapters + Single-node Virtual Keys/Routing/Budgets + Immediately usable Core CLI & Web Console*.
- **`v0.2`:** Adds the **MCP Plane** (tool ACL filtering, `schema_hash` pinning, `destructive` tool human approvals), **Authorized OAuth 2.1**, and **Static Marketplace Index / Private Registry**.
- **`v0.3`:** Adds the **A2A Plane** and **Multi-replica Scale-out HA** mode (distributed database & quota coordination).
- **`v1.0`:** Freezes backward-compatible `Plugin Contract v1.0`, completes independent security audits, delivers `Air-gapped Bundle` packaging, and unlocks full multi-organization governance.
- **`Post-v1.0`:** Expands into low-latency bidirectional media protocols (advanced Realtime Voice/Video over WebRTC/WebSocket), broader verified partner ecosystems, and external observability/evaluation integrations.
