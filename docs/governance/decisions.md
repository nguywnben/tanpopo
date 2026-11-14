# Decision Register & Blocking Open Questions — Tanpopo

> **Canonical Decision Log — Updated Through Pre-Build Closure & Phase A10 Readiness Audit**
> - **Last Updated:** `2026-11-14`
> - **Governance Invariant:** No AI Agent or assistant recommendation may ever promote a decision to `CONFIRMED_BY_OWNER`, `ACCEPTED`, or `APPROVED` without explicit direct approval from the Product Owner. Product scope (`Gate G1` / `Gate G2`), extension contracts (`Baseline A04`), technical architecture & ADRs (`Gate G3 — ADR-0001..0006`), UX/Ops specs (`Baseline A06`), Security/Verification specs (`Baseline A07`), and the Implementation Plan (`Gate G4 — ASTRA A09/A10`) all currently remain **`READY_FOR_REVIEW` / `BLOCKED`**; the independent **ASTRA A08** audit returned **`Specification Completeness = FAIL`** (`GAP-01`..`13`, `CR-01`..`CR-10`), and **ASTRA A10** established **`HANDOFF_VERDICT = NO_GO`** (`0` `READY` units, `47 / 47` `BLOCKED` units — see [`execution-readiness.md`](../reviews/execution-readiness.md), [`execution-baseline.md`](./execution-baseline.md), and [`owner-approval-sheet.md`](./owner-approval-sheet.md)).

---

## Stage-Gate Status Tracker

| Gate / Baseline | Corresponding Phase | Verification Scope | Live Status | Governance Note |
|---|---|---|---|---|
| **Gate G0** | **ASTRA A00** — Context Intake & Baseline Normalization | Repository inventory, empirical verification of 3 pre-A00 setup tasks, separation of confirmed facts vs. working hypotheses. | `COMPLETED` | Context baseline established in `docs/governance/*` and `docs/README.md`. |
| **Gate G1** | **ASTRA A01** — Product Discovery | Positioning, prioritized user segments, competitor matrix, opportunity/feasibility, legal/TOS risk boundaries, success metrics, release phasing, and 5 strategic decisions (`SD-G1-01`..`SD-G1-05`). | **`READY_FOR_REVIEW`** | **Not yet approved by Product Owner.** Kept strictly pending; never auto-elevated to `APPROVED`. |
| **Baseline A02** | **ASTRA A02** — Business Analysis | 12 actors, business entity ownership map, 8 state machines (`SM-01`..`08`), 13 workflows (`WF-01`..`13`), 30 business rules (`TP-BR-0101`..`0803`), `OPS-01`..`09`, glossary, and `BA-OQ-001`..`005`. | `DRAFT_COMPLETED` | Traced 100% into PRD/SRS, Architecture, and UX/Ops (`SM-01..09` dead-end fixes scheduled in `TP-IMP-0001`). |
| **Gate G2** | **ASTRA A03** — Product Definition, PRD/SRS & Feature Specs | PRD, 16-feature catalog across 10 domains (`FT-01`..`16`), release phasing (`MVP v0.1` → `v1.0` → `Post-v1`), SRS (`TP-FR-0101`..`1005`, `TP-NFR-0101`..`1002`), `TP-AC-*`, traceability, and 6 trade-offs (`TD-G2-01`..`06`). | **`READY_FOR_REVIEW`** | **`DRAFT — UNLOCKED SCOPE`**; awaits Product Owner sign-off on `BA-OQ-001..05`, `TD-G2-01..06`, and `CR-01..04, 09`. |
| **Baseline A04** | **ASTRA A04** — Deep Extension Platform & Connectivity Specs | 6 specifications `SPEC-EXT-01`..`06` (`docs/specifications/*`) and contract test matrix `SPEC-QA-01` (`CT-01`..`24`, `EXT-ACCEPT-01`): 3 execution tiers, blind credential broker, Marketplace, MCP Hub/A2A, and protocol compatibility. | **`READY_FOR_REVIEW`** | **`ILLUSTRATIVE — PRE-ADR & PRE-POC`**; tied to open PoC/policy items (`POC-01`..`03`, `POL-01`..`02`) and `CR-05, 08`. |
| **Gate G3** | **ASTRA A05** — Solution Architecture & Technical Design | System design `ARCH-SYS-01`, dual-mode physical schema `ARCH-DATA-01`, HA topology `ARCH-OPS-01`, routing/policy `SPEC-CORE-01`, `micro-USD`/OTel `SPEC-CORE-02`, OpenAPI/JSON Schema/Protobuf `SPEC-API-01`, `SPIKE-01`..`04`, and **6 ADRs (`ADR-0001`..`0006`)**. | **`READY_FOR_REVIEW`** | **`PROPOSED — PRE-APPROVAL`**. Zero application or PoC code written pending Product Owner approval. |
| **Baseline A06** | **ASTRA A06** — UX/UI, Developer Experience & SRE Operations | 4 UX specs `SPEC-UX-01`..`04` (`docs/ux/*`: 11 IA sections, `UJ-01`..`08` across 5 states, `SCR-01`..`16`, `UX-GR-01`..`06`, Astryx `neutral` & `UX-AC-01`..`12`) and 4 Ops/DX specs `SPEC-OPS-01`..`04` (`docs/operations/*`: multi-OS/Air-Gap, `DR-01`..`04`, `INC-01`..`07`, `tanpopo` CLI & `OPS-AC-01`..`12`). | **`READY_FOR_REVIEW`** | **`PROPOSED — NO CODE`**. Preserves `neutral` theme (`DEC-007`), zero `theme eject`, zero frontend/backend code edits. |
| **Gate G3 (Consolidated A05–A07)** | **ASTRA A07** — Security, Reliability & Verification Strategy | 3 security specs `SPEC-SEC-01`..`03` (`docs/security/*`: `TM-01`..`13`, Sandbox/Supply-Chain & Envelope `AEAD AAD` + 4-layer tenancy + `DEC-004`) and 4 QA specs `SPEC-QA-01`..`04` (`docs/quality/*`: `CT-01`..`32`, `AUTH-01`..`16`, `FZ-01`..`06`, hermetic CI, `SLO-01`..`08` `TARGET / UNVERIFIED`, `FMEA-01`..`08`, `GATE-REL-01`..`07`). | **`READY_FOR_REVIEW`** | **`PROPOSED — NO CODE`**. Requests explicit Product Owner authorization before running `SPIKE-01`..`04` or writing runtime code. |
| **Audit Gate A08** | **ASTRA A08** — Independent Specification & Architecture Audit | Horizontal + 8 vertical-slice adversarial audit across all 54 A00–A07 documents ([`integrated-audit.md`](../reviews/integrated-audit.md) — `AUD-001`..`030`: `11 CRITICAL`, `14 MAJOR`, `5 MINOR`; [`unresolved-gaps.md`](../reviews/unresolved-gaps.md) — `CR-01`..`10` & `GAP-01`..`13`). | **`FAIL` *(Blocking Gaps Open)*** | **Blocks locking production execution baseline.** Requires Product Owner sign-off on `Gate G1–G3` + `CR-01`..`10` and completion of `M0` spec-closure units (`TP-IMP-0001`..`0003`). |
| **Gate G4** | **ASTRA A09 & A10** — Implementation Plan, Final Readiness Audit & Engineering Handoff | Reconciled 47-unit plan ([`implementation-index.md`](../planning/implementation-index.md)), Final Readiness Audit ([`execution-readiness.md`](../reviews/execution-readiness.md) — **`NO_GO`**), Execution Baseline ([`execution-baseline.md`](./execution-baseline.md) — `EXEC-BL-001-DRAFT`), and Consolidated Approval Sheet ([`owner-approval-sheet.md`](./owner-approval-sheet.md)). | **`BLOCKED` / `READY_FOR_REVIEW` *(`HANDOFF: NO_GO`)*** | **Awaiting Product Owner sign-off on [`owner-approval-sheet.md`](./owner-approval-sheet.md)**. All `47 / 47` units are `BLOCKED` (`0 READY`); **hard stop enforced (`0` code written, no release tagged/published)**. |

---

## Part A — Confirmed Baseline Decisions (`CONFIRMED_BY_OWNER`)

| ID | Topic | Confirmed Decision | Status | Evidence / Source |
|---|---|---|---|---|
| **DEC-001** | Core Product Identity | Tanpopo is an **open-source, self-hosted-first, extension-first, production-usable, horizontally scalable AI Gateway** spanning Unified API, routing, policies, monitoring, cost control, MCP tools, agent/A2A connectivity, Web Console, and CLI. | `CONFIRMED_BY_OWNER` | Prompt ASTRA A00 |
| **DEC-002** | Independent Extension Mechanism | Users install **Provider Adapters and other extensions** from the Marketplace, Git repositories, URLs/package archives, or private registries without requiring a new Tanpopo Core release. Extensions must have explicit contracts, SDKs, lifecycle state machines, security isolation, compatibility tests, and permissions. | `CONFIRMED_BY_OWNER` | Prompt ASTRA A00 |
| **DEC-003** | Four-Plane Architectural Separation | **MCP, A2A, Provider, and Plugin** are 4 distinct conceptual layers and must never be collapsed into a single business entity, wire protocol, or schema. | `CONFIRMED_BY_OWNER` | Prompt ASTRA A00 |
| **DEC-004** | Official vs. Community Policy & Compliance | Official adapters prioritize official commercial Provider APIs and provider-sanctioned access methods; Community/Private adapters are independently built and distributed by third parties. The SDK supports API Keys, authorized OAuth, enterprise identity/credential types, and custom auth. Neither community plugins nor user-clicked risk disclaimers legitimize turning personal consumer subscriptions into general-purpose APIs; Tanpopo never maintains TOS-violating connectors. | `CONFIRMED_BY_OWNER` | Prompt ASTRA A00 |
| **DEC-005** | Canonical Repository & Git Identity | Official repository `https://github.com/nguywnben/tanpopo` (`main` branch), Git author `Nguyễn Công Ben <nben940665@gmail.com>`. | `CONFIRMED_BY_OWNER` & `VERIFIED_IN_REPO` | Git remote, commits `0a35b74`, `6b73bcd` |
| **DEC-006** | Agent Engineering Workflow Skills | Local workspace installation of all 25 software engineering skills from `addyosmani/agent-skills` (`https://skills.addy.ie/`) under `.agents/skills/` with `.agents/references/` and `.agents/agents/`. | `CONFIRMED_BY_OWNER` & `VERIFIED_IN_REPO` | `skills-lock.json`, `.agents/`, commit `6b73bcd` |
| **DEC-007** | UI Design System & Default Theme | Installed `@astryxdesign/core`, `@stylexjs/stylex`, `@astryxdesign/theme-neutral`, `@astryxdesign/cli`, initialized `AGENTS.md`/`GEMINI.md`, and selected **`neutral`** (`astryx-themes.js`) as the default look-and-feel theme. **Scope note:** Establishes the UI library/theme for Astryx surfaces without locking Core backend language or repository layout ahead of Gate G3. | `CONFIRMED_BY_OWNER` & `VERIFIED_IN_REPO` | `package.json`, `astryx-themes.js`, `AGENTS.md`, pre-A00 setup |
| **DEC-008** | Atomic Git Commit Convention | Separate atomic commits per step following Conventional Commits **`<type>: step <NN> - <description>`** (or `<type>(<module>): step <NN> - <description>`, e.g., `docs: step 03 - ...`; never prefix step numbers with `a` like `a00`/`a01` and never put bare step numbers as the scope); preserve pushed `main` history without force-pushing. | `CONFIRMED_BY_OWNER` | Direct Product Owner instruction |

---

## Part B — Permanently Rejected Directions (`REJECTED`)

| ID | Rejected Direction | Rejection Rationale | Status |
|---|---|---|---|
| **REJ-001** | Narrowing Tanpopo into a simple AI API-key aggregator/rotator. | Violates the comprehensive multi-plane AI Gateway mandate (`DEC-001`). | `REJECTED` |
| **REJ-002** | Hardcoding AI Providers inside Tanpopo Core (requiring a Core release per new provider). | Destroys Tanpopo's core `extension-first` differentiator (`DEC-002`). | `REJECTED` |
| **REJ-003** | Maintaining TOS-violating connectors or using "user assumes risk" disclaimers / community plugins to legitimize turning personal consumer subscriptions (`ChatGPT Plus/Pro`, `Codex`, `Antigravity`, etc.) into general-purpose APIs. | Severe legal/TOS liability and direct violation of `DEC-004`. | `REJECTED` |
| **REJ-004** | Merging MCP, A2A, Provider, and Plugin into one universal entity, table, or wire contract. | Corrupts protocol boundaries, state lifecycles, and least-privilege security (`DEC-003`). | `REJECTED` |

---

## Part C — Five Strategic Decisions Pending Gate G1 Approval (`SD-G1-01`..`SD-G1-05`)

> **Gate G1 Status:** `READY_FOR_REVIEW` (Not yet `APPROVED`). Consolidated for rapid sign-off in [`owner-approval-sheet.md`](./owner-approval-sheet.md).

### SD-G1-01: Beachhead User Persona & Self-Hosted Deployment Model
- **Status:** `OPEN` (Recommended: `PROPOSED` — **Option 1**) | **Refs:** [`users-jobs-use-cases.md`](../discovery/users-jobs-use-cases.md), `ASM-001`, `OQ-004`.
- **Options:**
  1. **Option 1 (Recommended):** **Dual Beachhead on Dev Teams (5–50 engineers) & AI Platform Teams (50–500 engineers)** via dual-mode persistence: zero-dependency single-command standalone mode by default (embedded SQLite WAL + embedded Web Console/CLI — also serving legitimate BYOK solo developers), scaling out seamlessly to stateless PostgreSQL + Valkey clusters; defer >1,000-seat regulated-enterprise hardware certifications (`FIPS`/HSM) to `v1.0+`.
  2. **Option 2:** **Target Regulated Enterprises (>1,000 seats) immediately in `v0.1`** — mandating PostgreSQL + Valkey + Kubernetes + Vault/HSM from day one (sacrifices bottom-up developer adoption and `< 3m` onboarding).
  3. **Option 3:** **Optimize exclusively for local Solo Developers** — omitting multi-project RBAC, departmental budgets, and stateless HA clustering (hits a hard enterprise ceiling).

---

### SD-G1-02: Isolated Extension Execution Model & Marketplace Distribution
- **Status:** `OPEN` (Recommended: `PROPOSED` — **Option 1**, reconciled with `CR-02`) | **Refs:** [`opportunity-and-feasibility.md`](../discovery/opportunity-and-feasibility.md), `OQ-002`, `RSK-001`, `RSK-004`, `RSK-005`, `CR-02`.
- **Options:**
  1. **Option 1 (Recommended):** **Phased Multi-Tier Isolation Engine** (`Tier-1 Declarative Pack` for ~75–80% standard providers + `Tier-2 Wasm Capability Sandbox` in `v0.1`, adding `Tier-3 Isolated mTLS Sidecar` `TP-FR-0308` in `v0.3` per `CR-02`) paired with the **Blind Host Credential Broker** and **Signed Static Marketplace Index (Git/OCI/URL + Private Registry)**.
  2. **Option 2:** **Mandate compiled plugins (Wasm or subprocess) for every adapter** plus a centrally operated dynamic Marketplace SaaS backend (high authoring friction and recurring hosting cost).
  3. **Option 3:** **Declarative YAML/JSON only** with zero programmable sandbox (safe and light, but unable to handle custom enterprise auth/payload transformations).

---

### SD-G1-03: Phased Release Scope (`v0.1` → `v0.2` → `v0.3` → `v1.0`)
- **Status:** `OPEN` (Recommended: `PROPOSED` — **Option 1**) | **Refs:** [`positioning-and-success-metrics.md`](../discovery/positioning-and-success-metrics.md), `OQ-006`, `RSK-002`.
- **Options:**
  1. **Option 1 (Recommended):** **4-Plane Domain Model Upfront, 3-Wave Incremental Delivery:**
     - `v0.1 (MVP)`: Core + Unified/Native LLM API + Tier-1/2 Plugin Engine + 5 Official Adapters (`CR-01`) + Capability Routing/Two-Phase Budget/OTel/Hash-Chained Audit + Core Astryx Web Console & `tanpopo` CLI.
     - `v0.2`: **MCP Governance Plane** (per-tool ACL, JCS `schema_hash` pinning, dual-mode HITL approval, Authorized OAuth 2.1) + Signed Marketplace/Private OCI + `Bedrock`/`Azure`/`/v1/rerank` + `Route Explain` & `Playground`.
     - `v0.3` → `v1.0 GA`: **A2A Plane** + **Stateless HA Cluster** (`PostgreSQL 16 FORCE RLS` + `Valkey`) + `Enterprise OIDC / Four-Eyes / Cloud KMS` + `Tier-3 Sidecar` → 12-month contract freeze, `Air-Gap Bundle`, and Grey-Box Pen-Test at `v1.0`.
  2. **Option 2:** Bundle the full MCP Plane into `v0.1` (delays initial usable release by ~40–50%).
  3. **Option 3:** Big-bang delivery of all 4 planes + HA before the first release (severe schedule and quality risk — `RSK-002`).

---

### SD-G1-04: External Extension Ecosystem Governance & Legal/TOS Enforcement
- **Status:** `OPEN` (Recommended: `PROPOSED` — **Option 1**) | **Refs:** [`opportunity-and-feasibility.md`](../discovery/opportunity-and-feasibility.md), `DEC-004`, `OQ-005`, `RSK-003`.
- **Options:**
  1. **Option 1 (Recommended):** **Strict Official Curation + Transparent Permission-Gated Self-Hosted Runtime:** Grant `official` / `verified` trust badges and Official Marketplace listings exclusively to legitimate commercial/authorized integrations; for operator-installed external packages, enforce explicit network/secret permission review, stamp `unverified_external` in immutable audit logs, block `@tanpopo(-*)/*` namespace spoofing, block `Cookie` credential injection, and support signed `revocations.json` auto-quarantine.
  2. **Option 2:** Restrict the runtime to execute only packages signed by the central Tanpopo team (breaks enterprise sovereignty for private internal gateways).
  3. **Option 3:** Embed a brittle hardcoded domain blocklist inside Core source code (trivially forked in OSS and risks false-positive blocks on legitimate enterprise endpoints).

---

### SD-G1-05: Open-Source Licensing Strategy & Self-Hosted Feature Boundary
- **Status:** `OPEN` (Recommended: `PROPOSED` — **Option 1**) | **Refs:** [`market-and-competitors.md`](../discovery/market-and-competitors.md), `C-01`, `ASM-008`, `OQ-007`, `ISS-002`.
- **Options:**
  1. **Option 1 (Recommended):** **100% Pure Apache License 2.0 Across the Entire Repository** (Core, CLI, SDK, Official Adapters, and self-hosted Web Console including RBAC, OIDC SSO, budgets, hash-chained audit logs, and MCP/A2A governance), synchronizing `"license": "ISC"` in `package.json` to `"Apache-2.0"`.
  2. **Option 2:** Dual-license **Open-Core** (locking RBAC, SSO, Audit Logs, and Private Registries behind a proprietary enterprise directory).
  3. **Option 3:** Source-available commercial restriction license (`BSL` / `AGPLv3` / Modified Apache).

---

## Part D — Architectural Open Questions (`OQ-001`..`OQ-007`)

> Addressed by candidate ADRs (`ADR-0001`..`0006`) in Part H below; all remain `OPEN` / `PROPOSED` until Gate G3 approval:

| ID | Architectural Question | Recommended Proposal (`PROPOSED`) | Key Alternatives & Trade-Offs |
|---|---|---|---|
| **OQ-001** | Core Runtime Language (`Data Plane + Control Plane`) | **Option A: Go (`1.24+`, `CGO_ENABLED=0`) Modular Monolith** (`tanpopo` single binary) + TypeScript/React/Astryx static Web Console (`ADR-0001`). | *Option B (Rust):* peak zero-GC speed, slower Control Plane velocity. *Option C (Full-stack TS/Node):* single language, higher tail-latency/RAM under 1,000+ SSE streams and `import()` isolation hazards. |
| **OQ-002** | Extension Runtime & Security Isolation | **Option C (expanded via `CR-02`): Hybrid Tiered Sandbox** (`Tier-1 Declarative` + `Tier-2 Pure-Go wazero Wasm <= 16 MiB` + `Tier-3 Isolated mTLS Sidecar` in `v0.3`) + `BlindHostCredentialBroker` (`ADR-0002`). | *Option A (Wasm-only):* forces compilation for simple REST providers. *Option B (Subprocess-only):* cross-OS sandboxing inconsistency and high per-plugin RAM. |
| **OQ-003** | Unified API & Protocol Surface | **Option A: Multi-Plane Ingress** (`/v1/*` OpenAI-Compatible Unified API + `/native/{dialect}/*` Lossless Passthrough + `/mcp` Streamable HTTP + `/a2a/*` JSON-RPC/SSE). | *Option B (Proprietary Tanpopo-only schema):* forces every client/IDE to rewrite SDK integrations. |
| **OQ-004** | Persistence & Self-Hosted Topology | **Option A: Dual-Mode Persistence Behind `storage/port`** (`SQLite WAL` standalone default → `PostgreSQL 16+ FORCE RLS` + `Valkey` stateless HA cluster — `ADR-0003`). | *Option B (Mandatory Postgres + Redis on day one):* destroys `< 3m` standalone onboarding. |
| **OQ-005** | Community Plugin & Compliance Enforcement | **Option A: Strict Official Curation + Neutral Permission-Gated Runtime** with `Cookie` injection ban & `@tanpopo(-*)/*` signature check (`DEC-004`). | *Option B (Hardcoded Core domain blocklist):* brittle and easily bypassed in OSS. |
| **OQ-006** | Delivery Phasing | **Option A: Foundation-First 3-Wave Incremental Delivery** (`v0.1` Core/Plugin Vertical Slice → `v0.2` MCP/OAuth → `v0.3/v1.0` HA/A2A/Freeze). | *Option B (All-at-once release):* severe integration and quality risk. |
| **OQ-007** | License Sync & Web Console Directory Layout | **Option A: Pure `Apache-2.0` + Relocate UI into `web/console/`** with Vite + StyleX + preserved `neutral` theme (`ADR-0004`). | *Option B:* Switch project to ISC/MIT or keep flat root UI files mixed with Go modules. |

---

## Part E — Business Analysis Clarifications Pending Gate G2 (`BA-OQ-001`..`BA-OQ-005`)

> Full analysis in [`docs/requirements/workflow-requirement-candidates.md`](../requirements/workflow-requirement-candidates.md):

| ID | Business Question | Recommended Proposal (`PROPOSED`) | Trade-Off If Altered |
|---|---|---|---|
| **BA-OQ-001** | **Multi-Organization UX on a Single Self-Hosted Instance** | Keep `Organization` in the foundational schema from day one (allowing `OPERATOR` API/CLI creation for multi-tenant tests per `CR-04`), optimize `0.x` Web Console UX for **1 Organization with multiple Projects & Environments**, and unlock the full Multi-Org Console Switcher at `v1.0` (`TP-IMP-0045`). | Exposing full multi-org UI chrome in `v0.1` adds unnecessary cognitive overhead for 95% of single-company self-hosters. |
| **BA-OQ-002** | **End-of-Stream Settlement Exceeding Pre-Request Hold (`Over-Reservation Overage`)** | Allow the in-flight stream to finish cleanly, record honest slight negative balance (`Negative Balance Carryover`), and hard-block (`429 project_budget_exceeded`) starting from the very next request until topped up. | Cutting off a streaming response on the last few tokens ruins UX while the upstream provider still bills for generated tokens. |
| **BA-OQ-003** | **Behavior of Active Streams Upon `Virtual Key` Revocation** | Support **two explicit modes**: *Standard Rotation/Expiry* drains active streams gracefully within a short bound; *Emergency Security Revoke* aborts in-flight SSE streams immediately (`< 1s` single-node / `< 5s` cluster). | A single mode either leaves leaked keys streaming during an attack or causes unnecessary errors during routine key rotation. |
| **BA-OQ-004** | **Human-in-the-Loop Approval Flow for `destructive` MCP Tools** | Dual-mode: (1) *Short synchronous wait (`<= 60s`)* for interactive sessions, and (2) *Standard MCP JSON-RPC async pending response (`CR-08`)* issuing an argument-bound single-use approval token once a `SECURITY_REVIEWER` approves via Console/Webhook. | Holding HTTP connections open for hours breaks standard MCP IDE clients (`Claude Code`, `Cursor`). |
| **BA-OQ-005** | **Extension Contract Backward-Compatibility Window (`Deprecation Policy`)** | Support at least **`N-1` minor contract version** during `0.x`, and guarantee a **minimum 12-month backward-compatibility window** across `1.x` starting at `v1.0 GA`. | Lack of a clear SLA discourages third-party and enterprise adapter investment. |

---

## Part F — Six Scope & Design Trade-Offs Pending Gate G2 (`TD-G2-01`..`TD-G2-06`)

> Detailed in Section 5 of [`docs/product/release-scope.md`](../product/release-scope.md):

| ID | Trade-Off Topic | Current PRD/SRS Proposal (`PROPOSED`) | Status |
|---|---|---|---|
| **TD-G2-01** | `MVP (v0.1)` Boundary for the MCP Plane | Schedule MCP traffic plane (`FT-07`) in `v0.2` (`Should`), keeping `v0.1` laser-focused on Core + Extension Engine + LLM Gateway + Console/CLI quality. | `OPEN` (`PROPOSED`) |
| **TD-G2-02** | Inference Surface: Unified Translation vs. Native Passthrough | Combine OpenAI-Compatible Unified API (`Must`) + Lossless `/native/{dialect}/*` Passthrough (`Should` in `v0.1`) + strict `400 capability_not_supported` rejection instead of silent parameter dropping. | `OPEN` (`PROPOSED`) |
| **TD-G2-03** | Concurrent Budget Control vs. Stream Completion UX | Pre-request reservation (`Pre-Hold`) + `Negative Balance Carryover` instead of mid-sentence stream truncation. | `OPEN` (`PROPOSED`) |
| **TD-G2-04** | Extension Execution Tiers & Raw Secret Protection | Hybrid `Declarative Pack` + `Default-Deny Wasm Sandbox` (plus `v0.3` `Tier-3 Sidecar` per `CR-02`) paired with `BlindHostCredentialBroker` from `v0.1`. | `OPEN` (`PROPOSED`) |
| **TD-G2-05** | HITL Approval Ergonomics for `destructive` MCP Tools | Dual-mode synchronous (`<= 60s`) + standard JSON-RPC asynchronous 5-tuple JCS hash-bound approval (`CR-08`). | `OPEN` (`PROPOSED`) |
| **TD-G2-06** | Day-One Single-Node Simplicity vs. Multi-Replica HA | Zero-dependency standalone `SQLite WAL` default in `v0.1`, abstracted behind `storage/port` for seamless `PostgreSQL 16 + Valkey` HA scale-out in `v0.3`. | `OPEN` (`PROPOSED`) |

---

## Part G — Technical & Policy Items Requiring PoC Measurement or External Policy Verification (`POC-01`..`03`, `POL-01`..`02`)

| ID | Unresolved Item | Root Dependency | Planned Resolution |
|---|---|---|---|
| **POC-01** | Exact Host–Wasm binary ABI (`Linear-Memory Ring Buffer` vs. `WASI Preview 2 WIT`) | Depends on empirical SSE latency (`< 5 ms p95`, `< 1 ms/chunk`) and Go/TS/Rust compiler maturity. | Benchmarked offline in `SPIKE-02` (`TP-IMP-0004`) and locked in `ADR-0002` before `M1` runtime coding. |
| **POC-02** | Minimal safe mapping operator set for Tier-1 Declarative Packs | Must cover `>= 8/10` representative providers (`<= 120` lines) without turning YAML into a Turing-complete language. | Validated across 10 provider schemas in `SPIKE-01` (`TP-IMP-0004`). |
| **POC-03** | Standard IPC protocol for `Tier-3 Isolated Sidecar` | Tied to Core runtime (`OQ-001`) and mTLS/UDS streaming ergonomics. | Proposed as `ConnectRPC / Protobuf` over `0600` UDS or mutual TLS (`ADR-0002`, `CR-02`, `CR-05`). |
| **POL-01** | Commercial AI providers officially permitting third-party `Authorized OAuth 2.1` / `Device Flow` | Provider terms vary per vendor and evolve over time; cannot be assumed universally. | Enabled on an `Official Adapter` **only** when backed by published vendor documentation permitting third-party OAuth clients (`DEC-004`). |
| **POL-02** | Dynamic Client Registration (`DCR`) vs. Client Metadata Document in MCP Authorization | MCP Authorization specification continues evolving across IDE clients in 2025–2026. | Keep MCP Auth server discovery modular to lock against current spec in `M3` (`v0.2`). |

---

## Part H — Six Architecture Decision Records (`ADR-0001`..`0006`) & Spikes (`SPIKE-01`..`04`) Pending Gate G3

> Full ADRs in [`docs/architecture/adr/`](../architecture/adr/README.md) and spike protocols in [`system-design.md`](../architecture/system-design.md) Section 7:

| ADR ID | Candidate Architectural Decision (`PROPOSED`) | Resolves | Status & Unlock Condition |
|---|---|---|---|
| [**`ADR-0001`**](../architecture/adr/0001-core-runtime-and-modular-monolith.md) | **Go (`1.24+`, `CGO_ENABLED=0`) Modular Monolith** (`module github.com/nguywnben/tanpopo`) compiled as a single `tanpopo` binary with `--role=all \| data-plane \| control-plane`. | `OQ-001` | `PROPOSED` — Awaits Product Owner sign-off. |
| [**`ADR-0002`**](../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md) | **3-Tier Isolated Extension Execution** (`Declarative` + `Pure-Go wazero Wasm <= 16 MiB` + `ConnectRPC mTLS Sidecar`) + `BlindHostCredentialBroker` (`0` untrusted in-process code). | `OQ-002`, `SD-G1-02`, `POC-01..03` | `PROPOSED` — Awaits Owner sign-off & authorization for `SPIKE-01`, `SPIKE-02`. |
| [**`ADR-0003`**](../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md) | **Dual-Mode Persistence** behind `storage/port`: embedded **`SQLite WAL`** (`modernc.org/sqlite`) for standalone and **`PostgreSQL 16+` (monthly partitions + `FORCE RLS`) + `Valkey`** for HA clusters. | `OQ-004`, `SD-G1-01`, `TD-G2-06` | `PROPOSED` — Awaits Product Owner sign-off. |
| [**`ADR-0004`**](../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md) | **Static SPA (`TypeScript + React 19 + Vite + StyleX + Astryx v0.7.13 neutral`)** relocated to `web/console/`, bundled with local `Figtree` `.woff2` fonts, embedded via Go `embed.FS`, and unified under **Apache-2.0**. | `OQ-007`, `ASM-003`, `ASM-008`, `ISS-001..005` | `PROPOSED` — Awaits Owner sign-off prior to moving root UI files. |
| [**`ADR-0005`**](../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md) | **Integer `BIGINT micro-USD` Two-Phase Reservation Ledger**, honest `EXACT / ESTIMATED / UNMETERED_FLAGGED` settlement tiers, and hard SQL separation of `COMMERCIAL_API` / `SELF_HOSTED_INFRA` from consumer subscriptions. | `BA-OQ-002`, `TD-G2-03`, `DEC-004` | `PROPOSED` — Awaits Owner sign-off & `SPIKE-03` validation. |
| [**`ADR-0006`**](../architecture/adr/0006-immutable-config-snapshots-and-fast-revocation.md) | **Immutable `Copy-on-Write ConfigSnapshot`** (`atomic.Pointer`) + 3-layer revocation propagation (`Valkey Pub/Sub` + `PG LISTEN/NOTIFY` + `2s` `config_changelog` poll) with `< 1s` / `< 5s` SLA and `> 10s` fail-closed stale-node circuit breaker. | `BA-OQ-003`, `TP-NFR-0404`, `RSK-011` | `PROPOSED` — Awaits Product Owner sign-off. |

---

## Part I — Ten Audit A08 Change Requests (`CR-01`..`CR-10`) Pending Product Owner Sign-Off

> Detailed trade-offs and verifiable closure criteria are documented in [`docs/reviews/unresolved-gaps.md`](../reviews/unresolved-gaps.md) and [`owner-approval-sheet.md`](./owner-approval-sheet.md):

| CR ID | Specification Conflict / Gap Addressed | Recommended Resolution (`PROPOSED`) | Status |
|---|---|---|---|
| **`CR-01`** | Exact `MVP (v0.1)` Official Reference Adapters (`Bedrock` depends on SigV4) & missing `/v1/rerank`. | Lock **5** Official Adapters in `v0.1` (`OpenAI`, `Anthropic`, `Gemini`, `OpenAI-Compatible/vLLM`, `Ollama`); schedule `AWS Bedrock`, `Azure OpenAI`, and `/v1/rerank` in `v0.2`. | `OPEN` |
| **`CR-02`** | `2-Tier` (`G1/G2/SRS`) vs. `3-Tier Isolated Sidecar` (`ADR-0002` missing `TP-FR-*`). | Confirm 3-tier target architecture, build `Tier-1 + Tier-2` in `v0.1`, and add `TP-FR-0308 (Could — v0.3)` for `Tier-3 Sidecar`. | `OPEN` |
| **`CR-03`** | `3` vs. `4` `Trust Tier` enum conflict (`PRIVATE_ENTERPRISE`) & `@tanpopo/*` vs. `@tanpopo-official/*` prefix gap. | Keep **3** public `origin_trust_tier` values (`TP-BR-0201`) + independent `enterprise_signed BOOLEAN` column, and enforce reserved regex `^@tanpopo(-[a-z0-9]+)?/`. | `OPEN` |
| **`CR-04`** | `5` vs. `6` RBAC roles, missing per-Org plugin policy table & paradoxical `v0.1` exit gates (`Multi-Org`, `Air-Gap`). | Lock **6** canonical RBAC roles (`OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`), add `org_extension_policies`, and scope `v0.1` exit gates cleanly to standalone capabilities. | `OPEN` |
| **`CR-05`** | Non-compiling `runtime.proto`, missing `TransformResponse`, `32 MB` multimodal overflowing `16 MiB` Wasm & `scoped_secret_unseal` exposure. | Complete `runtime.proto` (2-phase validation, `TransformResponse`, `InspectNativeRequest`), stream media via out-of-band `blob_ref`, ban `Cookie` injection, and remove `scoped_secret_unseal` from Tier-2 Wasm. | `OPEN` |
| **`CR-06`** | Missing SQL tables, 3 conflicting `AEAD AAD` formulas, missing `Project DEK` for `< 60s` Crypto-Shredding & HA `audit_events` hash-chain fork. | Add all 10+1 missing tables (`26` total), composite `(org_id, id)` FKs + `FORCE RLS`, `project_encryption_keys`, plain `SHA-256` `vk_tp_` hashes, and per-`org_id` + `SYSTEM` JCS hash chains. | `OPEN` |
| **`CR-07`** | Valkey/PostgreSQL budget hold split-brain on failover & `Valkey Cluster` `CROSSSLOT` crash. | Enforce `tp:v1:{<org_id>}:*` hash-tag keys and `1s` `valkey_hold_checkpoints` heartbeats to PostgreSQL. | `OPEN` |
| **`CR-08`** | HTTP `202` + `X-Tanpopo-Approval-Token` breaking standard MCP IDEs (`Claude Code`/`Cursor`) & `SM-06`/`A2A` state conflation. | Use standard HTTP `200` MCP JSON-RPC responses with 5-tuple JCS argument binding for async HITL approvals, and split `SM-09` for `A2A` tasks. | `OPEN` |
| **`CR-09`** | Missing local admin password recovery (`Break-Glass`), missing OpenAI `stream_options.include_usage`, and orphaned `Webhooks` / `Response Cache`. | Add `tanpopo admin reset-local-owner` + automatic `include_usage` to `v0.1`; schedule `Webhooks` in `v0.2` and opt-in `Response Cache` in `v0.3`. | `OPEN` |
| **`CR-10`** | Missing `51/57` Admin OpenAPI schemas, `Playground` auth conflict, `SSRFGuard` blocking `Ollama` in `UJ-01`, binary name split & `Air-Gap` Google Fonts leak. | Complete `openapi/*.yaml`, issue short-lived `POST /admin/v1/playground/sessions` keys, add explicit `allow_private_ip` consent in Wizard `SCR-01`, unify on single `tanpopo` binary, and alias `Figtree` `.woff2` locally in Vite. | `OPEN` |
