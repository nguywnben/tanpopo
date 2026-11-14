# Tanpopo Documentation & Context Hub (`English-First Canonical Edition`)

> **Repository Governance & Readiness Status (`HANDOFF_VERDICT: NO_GO | GATE G4: STAGE_1_DONE / STAGE_2_AWAITING_OWNER_GO`)**
> - **Gate G0 (ASTRA A00 — Context Intake & Baseline):** `COMPLETED`
> - **Gate G1 (ASTRA A01 — Product Discovery):** **`APPROVED` *(`CONFIRMED_BY_OWNER` — `DEC-009`)* **
> - **Baseline A02 (ASTRA A02 — Business Analysis):** **`APPROVED` *(`SM-01..09` & `BA-OQ-001..05` closed in `TP-IMP-0001`)* **
> - **Gate G2 (ASTRA A03 — Product Definition, PRD/SRS & Feature Specs):** **`APPROVED` *(`TD-G2-01..06` & `CR-01..04, 09` closed in `TP-IMP-0001`)* **
> - **Baseline A04 (ASTRA A04 — Extension Platform & Connectivity Specs):** **`APPROVED_WITH_CONDITIONS`** *(`runtime.proto` & `POC-01/02` verified in `TP-IMP-0002, 0004`; `COND-PO-01, 04, 05`).*
> - **Gate G3 (ASTRA A05–A07 — Architecture, UX/Ops, Security, Reliability & Release Gates):** **`APPROVED_WITH_CONDITIONS`** *(`ADR-0001..0006` `ACCEPTED`, `openapi/*.yaml` & `db/spec-check/*.sql` verified, `SPIKE-01..04` completed in [`spike-results.md`](./architecture/spike-results.md); bound to `COND-PO-01..05`).*
> - **Audit Gate A08 (ASTRA A08 — Independent Specification Completeness Audit):** **`REMEDIATED_IN_M0` *(`PASS` — `13 / 13` Gaps Closed)*** — See [`docs/reviews/unresolved-gaps.md`](./reviews/unresolved-gaps.md).
> - **Gate G4 & Pre-Build Closure (ASTRA A09–A10 + `M0`):** **`STAGE_1_DONE` (`TP-IMP-0001..0004 = DONE`) / `STAGE_2_AWAITING_OWNER_GO` | `DELIVERY HANDOFF VERDICT = NO_GO`** — Per Product Owner ruling (`DEC-009`), `TP-IMP-0005`..`0047` (`43 / 47` units) remain **`BLOCKED` (`0 / 47` `READY`)** pending final Stage 2 Owner sign-off (`TP-IMP-0005` is 1st eligible `READY` unit on `GO`).

---

## 1. Cold-Start Reading Guide for Engineers & AI Agents

Every fresh session MUST begin with the Governance, Readiness, and Execution Baseline layer before trusting downstream contracts or picking up an implementation unit:

### 1.0. Execution Baseline, Owner Approval Gate & Audit Layer (`READ FIRST`)

| Order | Path | Core Content | When to Read |
|---|---|---|---|
| **0A** | [`docs/governance/execution-baseline.md`](./governance/execution-baseline.md) | **`EXEC-BL-001-DRAFT` (`HANDOFF: NO_GO`):** Mandatory cold-start entrypoint (`Zero Prior Chat Context Required`); locks the global zero-guesswork technical dictionary, source-of-truth precedence, initial wave queue, `CR` protocol, single-unit prompt template, and 7-part completion report contract. | **Read first in every new engineering/agent session.** |
| **0B** | [`docs/governance/owner-approval-sheet.md`](./governance/owner-approval-sheet.md) | **`GOV-OAS-01` (`AWAITING_OWNER_SIGN_OFF`):** Consolidated Product Owner Approval Sheet partitioning all 33 pending items (`G1`, `G2`, `G3`, `SPIKE-01..04`, `CR-01..10`, `G4`) into Track A (Batch) and Track B (High-Impact Individual Review) with trade-offs and impact maps. | **Read first for Product Owner gate closure** and unlocking `TP-IMP-0001..0004`. |
| **0C** | [`docs/reviews/execution-readiness.md`](./reviews/execution-readiness.md) | **`REV-RDY-01` (`NO_GO`):** Independent A10 Delivery Readiness Audit verifying true approval status, `113/113` verification ID mappings, `M0–M1` cold-start simulation, and 10 core architectural pillars. | When auditing `Gate G4` readiness or transitioning `NO_GO` → `GO`. |
| **0D** | [`docs/reviews/integrated-audit.md`](./reviews/integrated-audit.md) | **`REV-AUD-01` (`FAIL`):** Independent A08 horizontal + 8 vertical-slice adversarial audit (`AUD-001`..`030`: `11 CRITICAL`, `14 MAJOR`, `5 MINOR`) and 54 `P0` readiness check. | Before relying on raw A02–A07 contracts prior to `M0` (`TP-IMP-0001..0003`) completion. |
| **0E** | [`docs/reviews/unresolved-gaps.md`](./reviews/unresolved-gaps.md) | **`REV-GAP-01`:** Detailed Change Requests **`CR-01`..`CR-10`** and verifiable closure criteria for **`GAP-01`..`GAP-13`**. | During Product Owner decision closure and `M0` spec-closure execution. |

### 1.0B. Multi-Tier Implementation Plan & 47 Execution Units (`docs/planning/` — `BLOCKED_ON_OWNER_APPROVAL`)

| Order | Path | Core Content | When to Read |
|---|---|---|---|
| **0F** | [`docs/planning/implementation-index.md`](./planning/implementation-index.md) | **`PLAN-IDX-01`:** Master table of all **47 execution units (`TP-IMP-0001`..`0047`)** with milestones, Epics (`EP-00`..`09`), dependencies, `REQUIRES_STRONG_REVIEW` flags (`37/47`), live `BLOCKED` status, and links to [`docs/planning/units/TP-IMP-0001.md`](./planning/units/TP-IMP-0001.md) .. [`TP-IMP-0047.md`](./planning/units/TP-IMP-0047.md). | When checking unit readiness, dependencies, or locating a `UNIT_ID` spec. |
| **0G** | [`docs/planning/roadmap.md`](./planning/roadmap.md) & [`docs/planning/milestone-map.md`](./planning/milestone-map.md) | **`PLAN-RM-01` & `PLAN-MS-01`:** 9 dependency-driven phases (`EP-00`..`09`), program blockers, trunk/worktree/canary rollout strategy, capacity & uncertainty ranges, and milestone gates `GATE-M0`..`GATE-M5`. | Program coordination, milestone gate checks, and capacity planning. |
| **0H** | [`docs/planning/dependency-graph.md`](./planning/dependency-graph.md) | **`PLAN-DAG-01`:** Verified acyclic 47-node Mermaid DAG ($u < v$), critical path, 4 parallel `git worktree` expansion points, conditional First 5 Units, and hermetic cross-OS build/test procedures. | Parallel worktree scheduling and build/test environment setup. |

### 1.1. Governance & Domain Glossary (`docs/governance/` & `docs/product/glossary.md`)

| Order | Path | Core Content | When to Read |
|---|---|---|---|
| **1** | [`docs/governance/project-charter.md`](./governance/project-charter.md) | Product vision, `extension-first` differentiator, 4-plane separation (`Provider` / `Plugin` / `MCP` / `A2A`), Official vs. Community policy, and `DEC-004` ban on consumer subscription misuse. | Every new session. |
| **2** | [`docs/governance/decisions.md`](./governance/decisions.md) | Canonical Stage-Gate tracker, confirmed decisions (`DEC-001`..`DEC-008`), rejected directions (`REJ-001`..`REJ-004`), `SD-G1-01`..`SD-G1-05`, `OQ-001`..`OQ-007`, `BA-OQ-001`..`BA-OQ-005`, `TD-G2-01`..`TD-G2-06`, `POC/POL`, `ADR-0001`..`ADR-0006`, and `CR-01`..`CR-10`. | Before modifying scope, architecture, or directory layout. |
| **3** | [`docs/product/glossary.md`](./product/glossary.md) | Project-wide canonical terminology: 12 actors, `Adapter → Connection → LogicalModel → Deployment` chain, provenance tiers, and technical capability vs. legal authorization boundaries. | Naming APIs, database columns, and UI labels. |
| **4** | [`docs/governance/assumptions.md`](./governance/assumptions.md) | Unvalidated working hypotheses (`ASM-001`..`ASM-010`) linked to `SPIKE-01`..`SPIKE-04` and verified repository setup issues (`ISS-001`..`ISS-007`). | Validating assumptions and reconciling pre-existing workspace files. |

### 1.2. Security, Reliability, Quality Assurance & Release Gates (`docs/security/` & `docs/quality/` — `PROPOSED`)

| Order | Path | Core Content | When to Read |
|---|---|---|---|
| **5** | [`docs/quality/release-readiness.md`](./quality/release-readiness.md) | **`SPEC-QA-04`:** `Sev-0..3` bug policy, `CONSTRAINTS.md` floor-guard contract, **7 release gates `GATE-REL-01`..`07`**, and Gate G3 review package (`RSK -> P0 Test` map & `SPIKE-01`..`04` scope). | Release gate verification and quality bar enforcement. |
| **6** | [`docs/security/threat-model.md`](./security/threat-model.md) | **`SPEC-SEC-01`:** 7 crown-jewel assets (`AST-01`..`07`), 7 trust boundaries (`TB-1`..`7`), `STRIDE` + `OWASP LLM` analysis for **13 threat vectors (`TM-01`..`13`)**, and 6 security invariants. | Security design and code review of any module. |
| **7** | [`docs/security/plugin-sandbox-and-supply-chain.md`](./security/plugin-sandbox-and-supply-chain.md) | **`SPEC-SEC-02`:** Dual supply-chain security (`npm ci --ignore-scripts`, `CycloneDX SBOM`, `SLSA`, `Cosign`, `40-hex` Git SHA pinning, `revocations.json`), 3-tier sandbox (`wazero <= 16 MiB`), $\Delta P$ escalation gate, and `EvalSymlinks` safe deletion. | Implementing extension installer, Wasm/Sidecar sandboxes, and CI packaging. |
| **8** | [`docs/security/secrets-and-tenant-isolation.md`](./security/secrets-and-tenant-isolation.md) | **`SPEC-SEC-03`:** Context-bound `AEAD AAD` envelope encryption, write-only secrets, 4-layer tenant isolation (`FORCE RLS` + no cross-org cache + `Anti-Oracle 404`), `Four-Eyes`, `RFC 8707`, and `DEC-004` compliance boundaries. | Implementing `vault`, `iam`, `admin/http`, `oauth`, and caching. |
| **9** | [`docs/quality/contract-test-matrix.md`](./quality/contract-test-matrix.md) | **`SPEC-QA-01`:** Offline cassette replay harness, **32 contract & adversarial tests (`CT-01`..`32`)**, **16 AuthN/AuthZ tests (`AUTH-01`..`16`)**, **6 fuzz targets (`FZ-01`..`06`)**, and 10-step **`EXT-ACCEPT-01`**. | Writing contract, security, fuzzing, and `tanpopo plugin test` suites. |
| **10** | [`docs/quality/test-strategy.md`](./quality/test-strategy.md) | **`SPEC-QA-02`:** Test pyramid, 9-domain test matrix, 4 local `Fake*` servers, **dual-layer hermetic network lock (`0` paid external AI calls in CI)**, `CanaryScanner`, and 5-stage CI pipeline. | Implementing `internal/testkit/*` and GitHub Actions CI. |
| **11** | [`docs/quality/nonfunctional-slo.md`](./quality/nonfunctional-slo.md) | **`SPEC-QA-03`:** `SLO-01`..`08` (`TARGET / UNVERIFIED`), `503` overload shedding, HA chaos drills, and **8 deep failure-mode analyses (`FMEA-01`..`08`)**. | Implementing streaming edge cases, tool retries, ledger, DB migrations, and revocation. |

### 1.3. Solution Architecture, Physical Data Model, HA Topology & ADRs (`docs/architecture/` — `PROPOSED`)

| Order | Path | Core Content | When to Read |
|---|---|---|---|
| **12** | [`docs/architecture/system-design.md`](./architecture/system-design.md) | **`ARCH-SYS-01`:** Role-splittable Go Modular Monolith (`--role`), C4 Level 1–3 diagrams, tech-stack decision matrix, 12-step hot inference path, zero-rebuild plugin installation path, `SSRFGuard`, 6 background workers, and `SPIKE-01`..`04`. | Primary architecture reference for all engineers. |
| **13** | [`docs/architecture/data-model.md`](./architecture/data-model.md) | **`ARCH-DATA-01`:** `SQLite WAL` ↔ `PostgreSQL 16+` portability rules, physical SQL DDL (`UUIDv7`, `BIGINT micro-USD` ledger, `RLS`, Write-Only Vault, Hash-Chained Audit), indexes, `Expand/Contract` migrations, and Crypto-Shredding. | Writing SQL migrations, `storage/port`, and backup/erasure tooling. |
| **14** | [`docs/architecture/deployment-topology.md`](./architecture/deployment-topology.md) | **`ARCH-OPS-01`:** Standalone, Compose, and Multi-AZ K8s HA topologies, `Copy-on-Write ConfigSnapshot` + 3-layer revocation (`< 1s` / `< 5s`), cache matrix, distributed Lua rate limiting, and zero-drop rolling upgrades. | Designing Docker/Helm, SSE load balancing, Valkey Lua scripts, and HA drills. |
| **15** | [`docs/architecture/adr/README.md`](./architecture/adr/README.md) & [`docs/architecture/spike-results.md`](./architecture/spike-results.md) | **Index of `ADR-0001` .. `ADR-0006` (`ACCEPTED` under `COND-PO-01..05`) & `ARCH-SPIKE-01`:** Architectural decisions plus empirical `M0` benchmark results (`SPIKE-01`..`04`, `POC-01` Linear-Memory Wasm ABI & `POC-02`). | Before proposing changes to any foundational architectural choice or Wasm sandbox pool. |

### 1.4. User Experience (UX/UI) & Astryx Design System Integration (`docs/ux/` — `PROPOSED`)

| Order | Path | Core Content | When to Read |
|---|---|---|---|
| **16** | [`docs/ux/information-architecture.md`](./ux/information-architecture.md) | **`SPEC-UX-01`:** `AppShell` (`TopNav` context + 5-group `SideNav`), 11-section URL map, 6-role RBAC visibility matrix, domain noun consistency, and 3-axis extension trust badges preventing blind one-click installs. | Implementing SPA routing, navigation chrome, and RBAC visibility. |
| **17** | [`docs/ux/user-flows.md`](./ux/user-flows.md) | **`SPEC-UX-02`:** **8 end-to-end user journeys (`UJ-01`..`UJ-08`)** with Mermaid flows and complete **5-state matrices (`Happy`, `Loading`, `Empty`, `Error`, `Offline`)**. | Implementing interactive UI workflows and Playwright E2E suites. |
| **18** | [`docs/ux/screen-specifications.md`](./ux/screen-specifications.md) | **`SPEC-UX-03`:** **6 UX guardrails (`UX-GR-01`..`06`)**, `WCAG 2.2 AA` / responsive (`1280px` / `768px` / `375px`) contracts, and detailed specs for all **16 screens (`SCR-01`..`16`)**. | Building Console pages, forms, confirmation modals, and `/admin/v1/*` bindings. |
| **19** | [`docs/ux/design-system-integration.md`](./ux/design-system-integration.md) | **`SPEC-UX-04`:** Honest audit of pre-existing Astryx v0.7.13 `neutral` setup vs. `ADR-0004` (no `theme eject`), `Badge` (counts only) vs. `Token` (`icon + label`), 16-screen Astryx template mapping, and `UX-AC-01`..`12`. | Scaffolding Astryx templates, configuring Vite + StyleX, and running `astryx doctor --built`. |

### 1.5. Self-Hosted SRE Operations & Developer Experience (`docs/operations/` — `PROPOSED`)

| Order | Path | Core Content | When to Read |
|---|---|---|---|
| **20** | [`docs/operations/installer-upgrades.md`](./operations/installer-upgrades.md) | **`SPEC-OPS-01`:** Multi-OS installation (`Linux` / `macOS` / `Windows`), 8 `tanpopo doctor` checks, `Bootstrap Token` vs. GitOps, TLS/Nginx SSE tuning, LAN GPU firewall rules, `SQLite -> Postgres` migration, `SM-08` preflight, and `Air-Gap` bundles. | Packaging installers, `systemd`/Compose/Helm manifests, and upgrade flows. |
| **21** | [`docs/operations/backups-recovery.md`](./operations/backups-recovery.md) | **`SPEC-OPS-02`:** `Master KEK`-separated `.tpop.enc` backups, tiered data retention (`0 byte` prompt default, `TTL <= 72h` debug blobs), `< 60s` Crypto-Shredding, and **4 disaster-recovery playbooks (`DR-01`..`04`)**. | Implementing `tanpopo backup/tenant`, TTL cleaners, and DR drills. |
| **22** | [`docs/operations/monitoring-incidents.md`](./operations/monitoring-incidents.md) | **`SPEC-OPS-03`:** `/healthz` vs. `/readyz` vs. `/metrics` truth table, symptom-based alerts (`PAGE` vs. `TICKET`), **7 on-call incident runbooks (`INC-01`..`07`)**, and `OPS-AC-01`..`12`. | Configuring Prometheus/OTel metrics, alert rules, and chaos tests. |
| **23** | [`docs/operations/developer-experience.md`](./operations/developer-experience.md) | **`SPEC-OPS-04`:** Unified `tanpopo` CLI command tree & exit codes (`0/2/10/14/22/30`), Management API `ETag`/`If-Match` ergonomics, polyglot SDK snippets, `CanonicalError` table, and redacted `support-bundle`. | Implementing `tanpopo` CLI, SDK guides, and diagnostic redaction. |

### 1.6. Deep Extension Platform, Routing, FinOps & API Specifications (`docs/specifications/`)

| Order | Path | Core Content | When to Read |
|---|---|---|---|
| **24** | [`docs/specifications/api-contracts.md`](./specifications/api-contracts.md) | **`SPEC-API-01`:** Design-level **OpenAPI 3.1** (Data Plane + Admin API + OAuth + Kill-Switch), **JSON Schema** (Normalized Envelope), and **Protobuf `proto3`** (`ExtensionRuntimeService`). | Generating API types, HTTP/ConnectRPC handlers, and Console/CLI clients. |
| **25** | [`docs/specifications/routing-and-policy.md`](./specifications/routing-and-policy.md) | **`SPEC-CORE-01`:** Monotonic hierarchical policy algebra (`Org ∩ Project ∩ Env`), `Fail-Closed` vs. `Fail-Open` matrix, 6-step capability router, `SM-04` circuit-breaker math, and `Dry-Run Route Explain`. | Implementing `policy/engine`, `routing/resolver`, and route simulation. |
| **26** | [`docs/specifications/usage-billing-observability.md`](./specifications/usage-billing-observability.md) | **`SPEC-CORE-02`:** Hard `COMMERCIAL_API` / `SELF_HOSTED_INFRA` billing boundary, integer `nano/micro-USD` math, two-phase `SM-07` reservation, and **OpenTelemetry** metrics/traces/logs. | Implementing `finops/ledger`, `telemetry/otel`, and chargeback exports. |
| **27** | [`docs/specifications/extension-contract.md`](./specifications/extension-contract.md) | **`SPEC-EXT-01`:** 8 extension entity types vs. 7 immutable Core Kernel responsibilities, language-agnostic SDK contract (`N-1` / `12m`), manifest schema, and 3 isolated execution tiers. | Designing the Extension SDK, Wasm host pool, and package loader. |
| **28** | [`docs/specifications/provider-adapter.md`](./specifications/provider-adapter.md) | **`SPEC-EXT-02`:** `Adapter → Connection → LogicalModel → Deployment` separation, `DECLARED / TESTED / VERIFIED` assurance tiers, sample Declarative pack, and pre-stream failover rules. | Building Official Adapters and the Tier-1 Declarative interpreter. |
| **29** | [`docs/specifications/authentication-connections.md`](./specifications/authentication-connections.md) | **`SPEC-EXT-03`:** Auth invariants (`Zero Full-Vault Access`, `Write-Only Plaintext`, `No Cross-Audience Token Relay`), 5 auth drivers, single-flight OAuth refresh, and consumer subscription compliance case study. | Implementing Vault, Blind Broker, and OAuth 2.1 coordinator. |
| **30** | [`docs/specifications/marketplace-lifecycle.md`](./specifications/marketplace-lifecycle.md) | **`SPEC-EXT-04`:** `Package Origin ≠ Publisher Trust ≠ Runtime Safety`, 4 install sources, deterministic `.tpkg` archive rules, 9-step install/rollback/quarantine lifecycle, `tanpopo.lock`, SBOM, and `revocations.json`. | Implementing `.tpkg` installer, `tanpopo.lock`, and Marketplace/OCI sync. |
| **31** | [`docs/specifications/mcp-a2a-boundaries.md`](./specifications/mcp-a2a-boundaries.md) | **`SPEC-EXT-05`:** 4-plane separation; MCP Hub (`<server>__<tool>`, per-tool ACL, RFC 8707 audience binding, JCS `schema_hash` pinning, sync/async HITL approval, `0` blind retries); and A2A plane (`hop_count <= 5`). | Implementing `/mcp` Governance Hub, HITL approvals, and `/a2a/*` coordinator. |
| **32** | [`docs/specifications/api-protocol-compatibility.md`](./specifications/api-protocol-compatibility.md) | **`SPEC-EXT-06`:** Per-capability protocol compatibility, SSRF-safe multimodal handling, `< 1 ms` SSE chunking & `< 500 ms` cancellation, `Idempotency-Key`, and `Canonical Error Taxonomy`. | Verifying wire-level streaming and error edge cases. |

### 1.7. Product Definition, Business Requirements & Discovery (`docs/product/`, `docs/requirements/` & `docs/discovery/`)

| Order | Path | Core Content | When to Read |
|---|---|---|---|
| **33** | [`docs/product/prd.md`](./product/prd.md) | Problem statement, personas, value proposition, `PG-01`..`05`, `NG-01`..`08`, target outcomes `MO-01`..`06`, acyclic 10-module capability map, and product constraints `PC-01`..`06`. | Checking product goals and non-goal boundaries. |
| **34** | [`docs/product/release-scope.md`](./product/release-scope.md) | Release milestones (`MVP v0.1` → `v0.2` → `v0.3` → `v1.0` → `Post-v1.0`), per-release exit criteria, and 6 trade-offs (`TD-G2-01`..`06`). | Enforcing milestone scope discipline. |
| **35** | [`docs/product/feature-catalog.md`](./product/feature-catalog.md) | 16 structured features (`FT-01`..`16`) across 10 business domains with 14 verifiable attributes per feature. | Checking detailed module preconditions, error flows, and limits. |
| **36** | [`docs/requirements/srs.md`](./requirements/srs.md) | **System Requirements Source of Truth:** 14-row Protocol/Capability Matrix, `53` functional requirements (`TP-FR-0101`..`1005`), and `22` non-functional requirements (`TP-NFR-0101`..`1002`). | Checking normative `Must`/`Should`/`Could` requirements and NFR targets. |
| **37** | [`docs/requirements/acceptance-criteria.md`](./requirements/acceptance-criteria.md) | Testable `Given/When/Then` scenarios across functional (`TP-AC-F*`), security/compliance (`TP-AC-SEC-01`..`08`), and non-functional (`TP-AC-N01`..`10`) requirements. | Designing automated tests and verifying acceptance gates. |
| **38** | [`docs/requirements/traceability.md`](./requirements/traceability.md) | End-to-end bidirectional traceability matrix mapping `UC/WF/RSK/TM` → `TP-FR/NFR` → `ARCH/ADR/SPEC` → `TP-IMP-0001..0047` → `CT/AUTH/FZ/SLO/FMEA/GATE-REL`. | Auditing requirement-to-unit-to-test coverage. |
| **39** | [`docs/requirements/business-entity-map.md`](./requirements/business-entity-map.md) | 12-actor matrix, ER diagram, ownership/sharing rules, and **lifecycle state machines (`SM-01`..`08`)**. | Implementing RBAC ownership and entity state machines. |
| **40** | [`docs/requirements/workflows.md`](./requirements/workflows.md) | **13 end-to-end business workflows (`WF-01`..`WF-13`)**, sequence diagrams, and failure/recovery flows. | Implementing cross-module orchestration and failure recovery. |
| **41** | [`docs/requirements/business-rules.md`](./requirements/business-rules.md) | **30 technology-agnostic business rules (`TP-BR-0101`..`0803`)** with owners and exception policies. | Implementing policy evaluation, security guards, and ledger rules. |
| **42** | [`docs/requirements/workflow-requirement-candidates.md`](./requirements/workflow-requirement-candidates.md) | **9 operational requirement groups (`OPS-01`..`OPS-09`)**, candidate mapping log, and `BA-OQ-001`..`005`. | Tracing operational requirements to business origins. |
| **43** | [`docs/discovery/positioning-and-success-metrics.md`](./discovery/positioning-and-success-metrics.md) | Strategic positioning, North Star Metric (`WGMR`), scope guardrails, and validation experiments `EXP-01`..`06`. | Reviewing strategic metrics and experiment hypotheses. |
| **44** | [`docs/discovery/users-jobs-use-cases.md`](./discovery/users-jobs-use-cases.md) | Ranked organizational segments, Anti-Personas, 4 functional roles, and 9 discovery use cases (`UC-01`..`UC-09`). | Understanding user jobs-to-be-done. |
| **45** | [`docs/discovery/market-and-competitors.md`](./discovery/market-and-competitors.md) | 2025–2026 competitive landscape analysis across 4 gateway/tooling categories with source citations. | Benchmarking competitive parity and differentiation. |
| **46** | [`docs/discovery/opportunity-and-feasibility.md`](./discovery/opportunity-and-feasibility.md) | Parity/Differentiators/Moats breakdown, legal/TOS risk zones, and 7 high-leverage product concepts (`IDEA-01`..`07`). | Reviewing competitive moats and compliance zones. |
| **47** | [`docs/discovery/risk-register.md`](./discovery/risk-register.md) | Ranked register of 16 strategic, security, and technical risks (`RSK-001`..`RSK-016`) mapped to `P0` verification suites. | Managing technical and security risk mitigations. |

### 1.8. Workspace Tooling & Engineering Skills

| Order | Path | Core Content | When to Read |
|---|---|---|---|
| **48** | [`AGENTS.md`](../AGENTS.md) & [`GEMINI.md`](../GEMINI.md) | Mandatory **Astryx Design System v0.7.13** workflow (template-first, no raw `div`/`span`, `Badge` for counts only, tokens only, `astryx doctor`) + **Tanpopo Cold-Start Agent Handoff Gate**. | Automatically loaded in every agent session; mandatory before touching UI or `TP-IMP-*` units. |
| **49** | [`.agents/skills/using-agent-skills/SKILL.md`](../.agents/skills/using-agent-skills/SKILL.md) | Meta-skill governing discovery and invocation of the 25 SDLC engineering skills across Define → Plan → Build → Verify → Review → Ship. | At the start of specialized engineering tasks. |

---

## 2. Explicit Separation: Existing Code on Disk vs. Specification & Planning Artifacts

> **Verified Repository Fact (`VERIFIED_IN_REPO`):** Through Milestone `M0` Closure, the repository **contains zero `M1+` production AI Gateway runtime code and zero production Web Console screen implementations** (no root `go.mod`, no production HTTP server, and no Vite bundler yet — only the pre-existing Astryx placeholder in `src/`, the machine-readable contracts in `proto/`, `openapi/`, and `db/spec-check/`, and the isolated `M0` feasibility test suite in `tools/spikes/`). Execution of `TP-IMP-0005`..`0047` remains at **`HANDOFF_VERDICT: NO_GO`** awaiting Product Owner Gate G4 Stage 2 sign-off.

### 2.1. Complete Repository Inventory (`VERIFIED_IN_REPO`)

| Repository Path | Artifact Class | Verified State (`VERIFIED_IN_REPO`) |
|---|---|---|
| [`README.md`](../README.md) | **Root Repository Entrypoint** | English project overview, honest `NO_GO` pre-build status banner, 4-plane architecture summary, `DEC-004` compliance notice, and navigation links into `docs/`. |
| [`LICENSE`](../LICENSE) | **Root Legal License** | **Apache License 2.0** (201 lines, initial commit `0a35b74`). |
| [`.gitignore`](../.gitignore) | **Git Ignore Rules** | Excludes `node_modules/`, `dist/`, `build/`, `out/`, `coverage/`, `.env*`, logs, and IDE temp files (Go/SQLite/Vault patterns scheduled for `TP-IMP-0005`). |
| [`skills-lock.json`](../skills-lock.json) | **Agent Skills Lockfile** | Pins versions and SHA-256 hashes of all 25 skills installed from `addyosmani/agent-skills`. |
| [`.agents/skills/*`](../.agents/skills) | **Agent Workflow Skills (25)** | 25 workspace skill directories (`SKILL.md` + scripts/resources). Preserved unmodified. |
| [`.agents/references/*`](../.agents/references) | **Engineering Checklists (7)** | Shared review checklists (`security`, `performance`, `accessibility`, `observability`, `testing-patterns`, `orchestration-patterns`, `definition-of-done`). |
| [`.agents/agents/*`](../.agents/agents) | **Subagent Personas (4)** | Specialized reviewers: `code-reviewer`, `security-auditor`, `test-engineer`, `web-performance-auditor`. |
| [`.agents/plugin.json`](../.agents/plugin.json) | **Upstream Skill Metadata** | Original `agent-skills` manifest (`v0.6.15`, `MIT` license). |
| [`.gemini/commands/*.toml`](../.gemini/commands) | **Gemini CLI Shortcuts** | 9 TOML slash-command definitions. Preserved unmodified. |
| [`AGENTS.md`](../AGENTS.md) & [`GEMINI.md`](../GEMINI.md) | **Agent UI & Cold-Start Rules** | Generated Astryx v0.7.13 rules block (preserved 100% verbatim) + `TANPOPO-HANDOFF` cold-start navigation block. |
| [`package.json`](../package.json) & [`package-lock.json`](../package-lock.json) | **Initial Node.js Manifest** | 4 direct packages: `@astryxdesign/core@^0.7.13`, `@astryxdesign/theme-neutral@^0.7.13`, `@astryxdesign/cli@^0.7.13`, `@stylexjs/stylex@^0.19.1` (`"license": "ISC"`, unbundled — `ISS-001..004`, `GATE-REL-04`). Preserved unmodified. |
| [`astryx-themes.js`](../astryx-themes.js) | **Generated Theme Module** | Generated by `npx astryx theme add neutral`, exporting `defaultThemeSlug = "neutral"`. **Preserved 100% unmodified; never run `theme eject`.** |
| [`src/App.tsx`](../src/App.tsx) & [`src/main.tsx`](../src/main.tsx) | **Unbundled UI Placeholder** | 2 placeholder React/TSX files (34 and 13 lines) wiring `Theme` `neutral`. **Preserved 100% unmodified per pre-build source-freeze rule.** |
| `docs/governance/*` (`5` files) & `docs/README.md` | **Governance & Handoff Layer** | `project-charter.md`, `decisions.md`, `assumptions.md`, `execution-baseline.md`, `owner-approval-sheet.md`, and `docs/README.md`. |
| `docs/discovery/*` (`5` files) | **Product Discovery & Risks** | `market-and-competitors.md`, `users-jobs-use-cases.md`, `opportunity-and-feasibility.md`, `positioning-and-success-metrics.md`, `risk-register.md`. |
| `docs/product/*` (`4` files) | **Product Definition (`DRAFT`)** | `glossary.md`, `prd.md`, `feature-catalog.md`, `release-scope.md`. |
| `docs/requirements/*` (`7` files) | **Business Analysis, SRS & Traceability** | `business-entity-map.md`, `workflows.md`, `business-rules.md`, `workflow-requirement-candidates.md`, `srs.md`, `acceptance-criteria.md`, `traceability.md`. |
| `docs/specifications/*` (`9` files) | **Deep Platform & API Specs** | `extension-contract.md`, `provider-adapter.md`, `authentication-connections.md`, `marketplace-lifecycle.md`, `mcp-a2a-boundaries.md`, `api-protocol-compatibility.md`, `routing-and-policy.md`, `usage-billing-observability.md`, `api-contracts.md`. |
| `docs/architecture/*` (`10` files) | **Architecture, SQL DDL & ADRs** | `system-design.md`, `data-model.md`, `deployment-topology.md`, `adr/README.md`, and `ADR-0001`..`ADR-0006`. |
| `docs/ux/*` (`4` files) | **UX/UI & Astryx Integration** | `information-architecture.md`, `user-flows.md`, `screen-specifications.md`, `design-system-integration.md`. |
| `docs/operations/*` (`4` files) | **SRE Operations & CLI DX** | `installer-upgrades.md`, `backups-recovery.md`, `monitoring-incidents.md`, `developer-experience.md`. |
| `docs/security/*` (`3` files) | **Security & Tenant Isolation** | `threat-model.md`, `plugin-sandbox-and-supply-chain.md`, `secrets-and-tenant-isolation.md`. |
| `docs/quality/*` (`4` files) | **QA, Hermetic CI, SLOs & Gates** | `contract-test-matrix.md`, `test-strategy.md`, `nonfunctional-slo.md`, `release-readiness.md`. |
| `docs/reviews/*` (`3` files) | **Independent Audits & Readiness** | `integrated-audit.md` (`FAIL`), `unresolved-gaps.md` (`CR-01`..`10`), `execution-readiness.md` (`NO_GO`). |
| `docs/planning/*` (`51` files) | **Implementation Plan & Units** | `roadmap.md`, `milestone-map.md`, `dependency-graph.md`, `implementation-index.md`, and `units/TP-IMP-0001.md`..`TP-IMP-0047.md`. |

---

## 3. Empirical Verification Summary of Pre-A00 Setup Tasks

| Pre-A00 Setup Task | Verification Verdict | Empirical Evidence (`VERIFIED_IN_REPO`) | Governance Handling |
|---|---|---|---|
| **Setup 1:** Install Agent Skills from `https://skills.addy.ie/` | **100% COMPLETE** | `25/25` skills verified in `.agents/skills/` matching `skills-lock.json`, plus `.agents/references/` and `.agents/agents/`. | Preserved untouched for all downstream implementation phases. |
| **Setup 2:** Install `@astryxdesign/core`, `@stylexjs/stylex`, `@astryxdesign/theme-neutral`, `@astryxdesign/cli` & run `init` | **COMPLETE (With Tracked Minor Discrepancies `ISS-002..004`)** | `npm ls --depth=0` confirms all 4 packages; `AGENTS.md`/`GEMINI.md` contain `ASTRYX:START..END`; `npx @astryxdesign/cli doctor` passes **`18 pass, 0 fail, 3 warn`**. | Relocation to `web/console/` and `"license": "Apache-2.0"` sync are gated behind `ADR-0004` (`TP-IMP-0005`). |
| **Setup 3:** Select look-and-feel (`neutral`), wire `Theme`, and preview | **CONFIG & PLACEHOLDER COMPLETE — NO LIVE BUNDLER YET (`ISS-001`)** | `neutral` is added and set as default (`maintained: yes`); `src/App.tsx` passes static `astryx doctor`, but Vite/`index.html` is not yet installed. | Keep `neutral` untouched (never `theme eject`); scaffold real screens via `npx astryx template` in `TP-IMP-0026..0030` after Gate G4 approval. |

---

## 4. Current Git Posture (`ENGLISH-FIRST DOCUMENTATION LOCKED | AWAITING OWNER GATE APPROVAL`)

- **Active Branch:** `main` (tracked against `origin/main`).
- **Official Tanpopo Artifacts:** `110` English Markdown files in `docs/`, root [`README.md`](../README.md), and [`AGENTS.md`](../AGENTS.md) — **zero modifications to `src/*`, `package.json`, `package-lock.json`, `astryx-themes.js`, or third-party `.agents/*` files**.
