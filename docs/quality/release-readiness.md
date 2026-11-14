# Release Quality Gates, Constraint Floor Guard & Gate G3 Review Package — Tanpopo

> **Release Quality Governance & Consolidated Gate G3 Specification (Phase ASTRA A07 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — NO CODE`)**
> - **Document ID:** `SPEC-QA-04` | **Version:** `0.1.0-step07-draft` | **Last Updated:** `2026-11-12`
> - **Authoring Role:** Principal Security Engineer, QA Architect & Release Engineering Lead
> - **Governance Status:** **Gate G3 remains strictly in `READY_FOR_REVIEW` (not yet approved by Product Owner)**. Enforces anti-bar-lowering rules from [`constraint-driven-development`](../../.agents/skills/constraint-driven-development/SKILL.md) and [`definition-of-done.md`](../../.agents/references/definition-of-done.md).
> - **Requirements Traceability:** `DEC-001`..`008`, `SD-G1-01`..`05`, `TD-G2-01`..`06`, `ADR-0001`..`0006`, `RSK-001`..`016`, `SPEC-SEC-01`..`03`, `SPEC-QA-01`..`03`.

---

## 1. Defect Severity Policy

Every defect discovered during internal testing, security scanning, or community reporting is classified into **4 severity levels** with zero-exception release-blocking rules:

| Severity | Technical Definition & Impact | Release Gate Rule | Response SLA & Mandatory Closure Evidence |
|---|---|---|---|
| **`Sev-0 / P0`**<br/>*(Catastrophic / Security Emergency)* | Wasm/Sidecar sandbox escape; upstream secret leakage or cross-tenant data bleed; `SSRF` reaching cloud metadata; unflagged ledger financial loss/corruption; `tanpopod` Core crash; `DEC-004` legal boundary violation. | **HARD BLOCKS ALL RELEASES (`0` open `P0` defects).** If discovered in a shipped build: cut an emergency patch release + publish `revocations.json` immediately. | Immediate hotfix; requires a failing *Prove-It Regression Test* prior to fix plus a documented `5 Whys` root-cause postmortem. |
| **`Sev-1 / P1`**<br/>*(Critical / Core Feature Broken)* | Broken primary inference path, invalid failover behavior (such as post-first-chunk fallback), retrying side-effecting MCP tools, broken `N-1` upgrade/rollback, or `critical` accessibility defects blocking keyboard navigation on primary journeys. | **BLOCKS RELEASE (`0` open `P1` defects).** A new feature carrying a `P1` may only be deferred behind a disabled flag; shipping broken `P1` functionality is prohibited. | Automated regression test locking the fix into `Stage 2 / Stage 3 CI` before ticket closure. |
| **`Sev-2 / P2`**<br/>*(Moderate / Workaround Exists)* | UI display bug or edge-case behavior on a secondary flow with zero impact on security, financial integrity, or availability, and a documented workaround. | At most `<= 3` open `P2` defects explicitly documented under `Known Issues` in Release Notes with a scheduled next-patch fix target. | Scheduled into the immediately following iteration. |
| **`Sev-3 / P3`**<br/>*(Minor / Cosmetic & Copy)* | Minor visual alignment polish, suboptimal message phrasing, or CLI ergonomic suggestions. | Non-blocking. | Batched into routine quality-polish passes. |

---

## 2. Anti-Bar-Lowering Guard (`Constraint Floor Guard — CONSTRAINTS.md Blueprint`)

To prevent engineers or AI coding agents from silently weakening checks to "force CI green", repository initialization (once authorized by the Owner) establishes `CONSTRAINTS.md` and an automated `Stage 0 Floor Guard` script that fails any PR introducing the following patterns without explicit `Security / QA Lead` sign-off:
1. **No Silent Test Skipping or Deletion:** Adding `t.Skip()`, `.skip`, `.only`, or `xit` without an active linked issue ID, or deleting existing `CT-*` / `AUTH-*` / `FZ-*` test cases.
2. **No Static-Analysis or Security Suppression Directives:** Introducing linter/scanner mutes such as `//nolint:gosec`, `/* eslint-disable */`, `@ts-ignore`, `@ts-expect-error`, `nosemgrep`, `# gitleaks:allow`, or empty error-swallowing `catch {}` blocks.
3. **No Threshold Downgrades in Configuration:** Lowering test coverage floors on critical packages (`vault`, `sandbox`, `policy`, `ledger`, `mcp` — line coverage floor `>= 85%` `TARGET / UNVERIFIED`), relaxing latency budgets, or weakening `astryx doctor` rules.
4. **No Astryx UI Rule Violations:** Introducing raw HTML `div` or `span` elements, hardcoded `#hex`/`px` literals, custom `.css` stylesheets, or `theme eject` inside the Web Console (`AGENTS.md`).

---

## 3. Seven Release Quality Gates (`GATE-REL-01 .. 07`)

A release (`v0.1`, `v0.2`, `v0.3`, `v1.0`) may be Git-tagged and published only when it satisfies all **7 measurable evidence gates**:

| Gate ID | Verification Surface | Required Evidence to Pass | Human Sign-Off Owner |
|---|---|---|---|
| **`GATE-REL-01`**<br/>**Test Evidence & Defect Closure** | Full test pyramid & coverage | `100%` green across `Stage 0..4 CI`; `0` open `Sev-0`/`Sev-1` defects; `EXT-ACCEPT-01` passes all 10 steps with unchanged Core binary SHA-256; `0` `Canary Secret` leaks. | `QA Architect` |
| **`GATE-REL-02`**<br/>**Cross-Platform & Contract Matrix** | OS, DB, Browser & `N-1` compatibility | Binaries verified on `Linux (amd64/arm64)`, `macOS (arm64)`, and `Windows (amd64)`; green simultaneously on `SQLite WAL` and `PostgreSQL 16+`; `N-1` extension packages run unmodified; verified on current Chrome/Edge/Firefox/Safari. | `Platform / QA Lead` |
| **`GATE-REL-03`**<br/>**Security, Fuzzing & `Pen-Test Scope`** | AppSec, Sandbox & Multi-Tenancy | - **`v0.1` – `v0.3` milestones:** Pass `100%` internal adversarial suite (`CT-03`..`05`, `CT-14`..`23`, `CT-25`..`31`, `AUTH-01`..`16`), run `FZ-01`..`06` fuzzing for `>= 4 hours` crash-free, `0` reachable `Critical/High` CVEs in `govulncheck`/`osv-scanner`.<br/>- **Prior to `v1.0` (`GA`):** Complete a mandatory **5-surface `Grey-Box Penetration Test`** covering: (1) `wazero` Wasm sandbox escape & resource exhaustion, (2) secret exfiltration via `Blind Broker` & side channels, (3) `SSRFGuard` / `DNS Rebinding` bypass, (4) `PostgreSQL RLS`, `AEAD AAD` & cross-tenant `Cache` bypass, and (5) `OAuth 2.1` forgery / `MCP Rug-Pull`. | `Security Lead` |
| **`GATE-REL-04`**<br/>**`Apache-2.0` Licensing & Supply Chain** | License, `SBOM`, Signatures & `DEC-004` | [`package.json`](../../package.json) reconciled to `"license": "Apache-2.0"` (resolving `ISS-002`); license scanner confirms **`0` viral copyleft dependencies (`GPL` / `AGPL` / `SSPL`)** in distributed binaries; ships `sbom.cdx.json`, `SLSA` provenance, and `Cosign` signatures; `100%` `DEC-004` compliance. | `Product Owner` + `Security Lead` |
| **`GATE-REL-05`**<br/>**Docs, API Contract & UI Parity** | Spec-to-code sync & Accessibility | `OpenAPI 3.1` spec and `tanpopo --help` docs match `100%` of live implementation (automated drift check); `npx @astryxdesign/cli doctor --built` reports `0 fail`; `axe-core` audit meets `WCAG 2.2 AA`. | `DX / UI Lead` |
| **`GATE-REL-06`**<br/>**`SRE Ops Readiness`** | Bootstrap, Monitoring, Backups & Runbooks | `tanpopo doctor` validates all 8 preflight checks; timed drills pass all 4 recovery runbooks `DR-01`..`04` (`RTO < 15m` `TARGET / UNVERIFIED`) plus `Air-Gap` bundle import; every `PAGE` alert links to a valid `INC-01`..`07` runbook. | `SRE / Ops Lead` |
| **`GATE-REL-07`**<br/>**Escalation Policy & Final Human Sign-Off** | Release governance | **AI Agents never hold authority to self-approve release gates.** Any temporary waiver request (strictly limited to `Sev-2`; never permitted for `Sev-0/Sev-1` or security violations) requires a written risk assessment signed directly by the **Product Owner**. | **`Product Owner`** *(Final Authority)* |

---

## 4. Consolidated Gate G3 Review Package

> **Current Governance Status of Gate G3 (alongside Gate G1 & Gate G2):** **`READY_FOR_REVIEW`** *(Never mark `APPROVED` without explicit Product Owner sign-off).*

### 4.1. Blocking Decisions Requiring Product Owner Resolution Prior to Implementation

To transition from Design & Specification (`A00` → `A07`) into detailed task breakdown, PoC execution, or production coding, the Product Owner must review and resolve **4 open decision groups** recorded in [`decisions.md`](../governance/decisions.md):

| Gate Group | Pending Decision IDs | Cohesive Recommended Option Across `A01`–`A07` (`PROPOSED`) | Why This Blocks Writing Code |
|---|---|---|---|
| **1. Product Strategy (`Gate G1`)** | `SD-G1-01` .. `SD-G1-05` | Target self-hosted `Dev Teams` & `AI Platform Teams`; multi-tier extension runtime + `Blind Broker`; 3-wave phasing (`v0.1` Core+Plugin → `v0.2` MCP → `v0.3` A2A+HA); strict `DEC-004` curation on official channels; pure **Apache-2.0** license (fixing `ISS-002`). | Governs `v0.1` scope boundaries, root repository license, and ecosystem trust rules. |
| **2. Scope & Business Trade-Offs (`Gate G2` & `A02`)** | `TD-G2-01` .. `TD-G2-06`<br/>`BA-OQ-001` .. `BA-OQ-005` | Schedule `MCP` in `v0.2`; ship both `Unified API` + `Native Passthrough`; pre-stream budget hold + minor end-of-stream negative carryover; split graceful rotation vs. emergency stream-severing revoke; dual-mode `destructive` approval (sync `<= 60s` + async token); `N-1` (`0.x`) / `12-month` (`1.x`) contract compatibility SLA. | Directly determines SQL schema, lifecycle state machines, and public API contracts. |
| **3. Core Architecture (`Gate G3 — ADRs`)** | [**`ADR-0001`**](../architecture/adr/0001-core-runtime-and-modular-monolith.md) .. [**`ADR-0006`**](../architecture/adr/0006-immutable-config-snapshots-and-fast-revocation.md) | **Go `1.24+` (`CGO_ENABLED=0`) Modular Monolith**; 3-tier **`wazero`** Sandbox; Dual-DB **`SQLite WAL` + `PostgreSQL 16+ / Valkey`**; static **`Astryx v0.7.13 (neutral)`** SPA relocated to `web/console/` and embedded in the Go binary; `BIGINT micro-USD` integer ledger; and `Copy-on-Write ConfigSnapshot` with `< 1s`/`< 5s` revocation. | These are *One-Way Doors*: `go.mod` initialization, `src/` relocation, and SQL DDL authoring cannot start until `ADR-0001`..`0004` are locked. |
| **4. Per-Vendor Policy Gates (`A04`)** | `POL-01`, `POL-02` | Enable `Authorized OAuth 2.1` on `Official Adapters` strictly for vendors with published third-party API authorization terms; keep MCP upstream auth modular for `v0.2`. | Protects legal standing and avoids coupling to unstable external draft specifications. |

---

### 4.2. 1-to-1 Mapping Between Critical Risks (`P0 / High`) and `P0` Risk Acceptance Tests

Every risk in [`risk-register.md`](../discovery/risk-register.md) is locked to at least one **binary Pass/Fail `P0` Risk Acceptance Test**:

| Risk ID | Critical Risk Title | Core Architectural Control (`SPEC-SEC / ARCH`) | Mandatory `P0` Acceptance Gate (`Pass/Fail Gate`) |
|---|---|---|---|
| **`RSK-001`** | Upstream credential exfiltration or sandbox escape via third-party plugins | `Tier-1 Declarative` + `Tier-2 Wazero (0 FS/Env/Net)` + `Blind Host Credential Broker` + `AEAD AAD` | **`CT-14`**, **`CT-16`**, **`CT-17`**, **`FZ-05`**, & **`OPS-AC-10`** *(Asserts `0 bytes` of plaintext secrets in Wasm guest RAM, logs, and error streams).* |
| **`RSK-002`** | Scope creep delaying first usable release | Phased `v0.1 -> v0.2 -> v0.3` delivery (`SD-G1-03`) + immutable lean Core | **`EXT-ACCEPT-01`** *(Proves lean `v0.1` Core loads an external Adapter across its full lifecycle with zero Core edits).* |
| **`RSK-003`** | Legal / ToS contamination from personal subscription scraping tools | Enforce `DEC-004` across DB (`billing_mode`), Marketplace gate, provenance tiers, and UI (`UX-GR-03`) | **`CT-20`** & **`AUTH-16`** *(Automatically fails cookie / personal-plan `client_id` harvesters and blocks `official` spoofing).* |
| **`RSK-004`** | Excessive latency or memory leaks across `Host <-> Wasm` streaming boundary | Prefer native Go Tier-1 evaluator for ~75–80% of providers + bounded `wazero` warm instance pool | **`SPIKE-02`** & **`CT-07`** *(Validates `p95 < 5 ms` overhead and `< 1 ms/chunk` across `1,000` SSE streams — currently `TARGET / UNVERIFIED`).* |
| **`RSK-005`** | Marketplace chicken-and-egg adoption trap & community plugin `Bit-Rot` | Ship bundled `Official Reference Adapters` on day one + scheduled automated cassette tests | **`CT-01`..`CT-13`** & **`SPIKE-01`** *(Author declarative pack in `< 30 min`; run `tanpopo plugin test` cassettes to detect contract drift).* |
| **`RSK-006`** | MCP tool poisoning (*Rug-Pull*), indirect prompt injection & side-effect retries | Per-key tool ACLs + JCS `schema_hash` pinning + `0` retries on `mutating`/`destructive` + argument-hash human approval | **`CT-21`**, **`CT-22`**, & **`FZ-04`** *(Mutating `1` description character immediately locks tool `403`; network timeouts never double-dispatch).* |
| **`RSK-007`** | Concurrent budget race overruns or `0 USD` accounting on aborted streams | Two-phase `Pre-Request Hold` over `micro-USD` integers + `EXACT / ESTIMATED / UNMETERED_FLAGGED` states | **`CT-10`**, **`CT-12`**, **`CT-29`**, & **`FZ-06`** *(`50` racing streams produce `0` budget overruns; `10,000` random transitions conserve exact totals).* |
| **`RSK-008`** | Premature freeze of an incomplete Plugin Contract breaking the ecosystem | Dogfood `Plugin Contract` for all `Official Adapters` + enforce `N-1` / `12-month` compatibility window | **`CT-24`** & **`OPS-AC-05`** *(`N-1` package runs unmodified; `upgrade preflight` blocks Core upgrade when unsupported `N-2` packages remain active).* |
| **`RSK-009`** | Lowest-common-denominator normalization dropping frontier model features | Pair `Unified API` with `Lossless Native Dialect Passthrough` + explicit `400` capability checks | **`CT-08`** & **`CT-13`** *(Preserves `100%` of vendor extension JSON fields on `/native/*`; fails `400` rather than silently dropping unsupported params).* |
| **`RSK-010`** | Open-source maintainer burnout & hosted marketplace operating cost | Keep Core minimal, push integrations into Extensions + static Git/OCI Marketplace index + self-diagnostics | **`EXT-ACCEPT-01`** & **`GATE-REL-05`** *(Ship new Adapters without Core releases; `tanpopo doctor` + `support-bundle` enable self-service triage).* |
| **`RSK-011`** | Revocation propagation lag or partitioned blind nodes in distributed clusters | 3-channel propagation (`Valkey` + `PG LISTEN/NOTIFY` + `2s` poll) + `> 10s` stale-node circuit breaker (`FMEA-07`) | **`AUTH-06`** & **`OPS-AC-03`** *(Severs open streams in `< 1s`/`< 5s`; nodes partitioned `> 10s` flip `/readyz = 503`).* |
| **`RSK-012`** | License mismatch (`ISS-002`) or accidental viral copyleft (`GPL`/`AGPL`) ingestion | Reconcile `Apache-2.0`, build pure-Go `CGO_ENABLED=0` binaries + run `Stage 0 CI` license/`SBOM` gates | **`GATE-REL-04`** *(`0` `GPL`/`AGPL`/`SSPL` dependencies in shipped binaries; verified `Apache-2.0` + bundled `sbom.cdx.json`).* |
| **`RSK-013`** | Behavioral drift between `SQLite WAL` and `PostgreSQL 16+` | 6 portable SQL type rules behind `storage/port` + post-migration state verifier | **`Stage 2 Dual-DB CI`** & **`CT-32`** *(Every integration test passes on both DB engines; engine migration reconciles `100%` of ledger totals and ciphertexts).* |
| **`RSK-014`** | `SSRF` & `DNS Rebinding (TTL=0)` targeting cloud metadata services | Inspect resolved peer IPs inside `net.Dialer.Control` at `connect()` + strip auth headers on cross-host redirects | **`CT-15`** & **`CT-26`** *(`0` TCP SYN packets reach `169.254.169.254` or `127.0.0.1` even when DNS flips at `TTL=0`).* |
| **`RSK-015`** | `Telemetry Cardinality` explosion or `OTLP` collector backpressure stalls | Bounded low-cardinality metric label allowlist + `32 MiB` `Drop-Oldest` OTLP ring buffer (`Fail-Open`) | **`OPS-AC-12`** & **`TP-AC-F0804`** *(`100,000` distinct `request_id`s add zero `/metrics` series growth; dead OTLP collector adds zero inference blocking).* |
| **`RSK-016`** | Cross-tenant sensitive completion leakage via response caching | Cache disabled by default; cache keys bind `org_id + project_id + env_id + policy_hash + tool_acl_hash` encrypted under `Org DEK` (`FMEA-03`) | **`CT-28`** & **`AUTH-14`..`15`** *(`0` cross-org/env/policy cache hits; `FORCE RLS` + `AEAD AAD` defeat cross-tenant reads).* |

---

### 4.3. Concrete Proof Scope of Timeboxed Spikes (`SPIKE-01` .. `SPIKE-04`) & Request for Product Owner Authorization

Before freezing the binary Wasm ABI (`POC-01`..`03`) and authoring full-scale application code, the architecture proposes **4 isolated, strictly timeboxed technical spikes (`Timeboxed Spikes`)**. Per governance rules, the table below specifies **what each PoC empirically proves, what it does NOT prove, and confirms that execution starts only upon explicit Product Owner authorization**:

| Spike ID | Max Timebox | What This Spike Empirically Proves | What This Spike Does NOT Prove | Downstream Design Decision Locked |
|---|---|---|---|---|
| **`SPIKE-01`**<br/>*Tier-1 Declarative Pack Expressiveness* | `1 day` | Whether a non-Turing-complete JSON/template operator set expresses full wire protocols for `>= 8/10` target AI providers (including SSE streaming, canonical errors, and 5-component token extraction) in `< 30 min/pack` (`POC-02`, `ASM-004`). | Does not prove multi-thousand-stream concurrency or Tier-2 Wasm isolation. | Freezes the minimal `Declarative Provider Schema v0.1`. |
| **`SPIKE-02`**<br/>*`wazero` Wasm Streaming Latency & `Blind Broker` Isolation* | `2 days` | Empirically measures on Go `1.24` + `wazero`: `p95`/`p99` overhead across `1,000` SSE streams, preemptive `while(1)` loop interruption, Wasm guest RAM caps, and `0 bytes` of raw secrets in Wasm linear memory (`POC-01`, `ASM-002`, `ASM-005`, `RSK-001`, `RSK-004`). | Does not represent a complete product (no DB, UI, or lifecycle persistence). | Selects the canonical `Host-Guest Streaming ABI` (Linear-Memory Ring Buffer vs. WIT Preview 2) from measured data. |
| **`SPIKE-03`**<br/>*Two-Phase Budget Hold Contention (`SQLite WAL` vs. `PG + Valkey`)* | `1 day` | Measures throughput and atomicity of `Pre-Hold` reservation + `micro-USD` settlement under `500` concurrent workers on both single-node `SQLite WAL` and `Valkey Lua + PostgreSQL` (`ASM-006`, `RSK-007`, `RSK-013`). | Does not include `FinOps` UI reporting or external billing webhooks. | Confirms single-node SQLite write ceiling and documents the PostgreSQL cutover threshold. |
| **`SPIKE-04`**<br/>*`Authorized OAuth 2.1` Single-Flight Refresh & `MCP schema_hash`* | `1 day` | Validates `Single-Flight Refresh` lock contention under `100` simultaneous callers and `RFC 8785 JCS` canonical fingerprinting latency across `300`-tool MCP catalogs (`POL-01`, `POL-02`, `RSK-006`). *(Note: `POC-03` for `Tier-3 Sidecar` IPC awaits scope resolution in `CR-02`).* | Does not establish commercial licensing policies of external AI vendors (`POL-01` remains evaluated per vendor ToS). | Freezes `OAuth Broker` concurrency primitives and `MCP Tool Firewall` hashing. |

> **Execution Boundary Confirmation & Authorization Request:**
> - Through the completion of **ASTRA A07**, **zero lines of PoC code, executable test code, or frontend/backend application code have been written to the repository**.
> - Upon reviewing this Gate G3 package, **explicit Product Owner approval of the open gate decisions (`Gate G1 / G2 / G3`) and explicit authorization** are requested before the team writes `SPIKE-01`..`04` spike code or proceeds to implementation execution.
