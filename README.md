# Tanpopo

**Open-Source, Self-Hosted-First, Extension-First AI Gateway for Models, MCP Tools, and Inter-Agent (A2A) Connectivity**

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)

> [!IMPORTANT]
> **Current Project Status — Gate G4 Approved (`HANDOFF_VERDICT: GO` · `TP-IMP-0001..0024` `DONE` · `DEC-034` Active [`0021..0030`] · `READY: TP-IMP-0025 [1 / 47]`)**
>
> Tanpopo has completed its end-to-end discovery, requirements, architecture, UX/SRE, security, and 47-unit planning framework (`ASTRA A00–A10`), Milestone `M0` (`TP-IMP-0001`..`0004` = `DONE`), **Milestone `M1`** (`TP-IMP-0005`..`0015` = `DONE`, closing `GATE-M1` / `EXT-SLICE-M1` at `baf154dc` / CI `43745804370`: shared `testkit`, pure-Go `SQLite WAL`, 3-tier Envelope Vault, IAM/`RBAC`/`vk_tp_*`/`ConfigSnapshot`, connect-time `SSRFGuard` & `BlindHostCredentialBroker`, safe `.tpkg` installer & `tanpopo.lock`, Tier-1 Declarative engine & 4-entity catalog, Tier-2 pure-Go `wazero v1.11.0` Wasm sandbox pool, `tanpopo plugin` SDK CLI & offline cassette runner, and `/v1/chat/completions` vertical slice), **Wave 2.1 (`M2`)** (`TP-IMP-0016`..`0020` = `DONE` under `DEC-025`..`DEC-032`), **`TP-IMP-0021`** (`DONE`, accepted under `DEC-034`/`DEC-035` at `c751d74d` / CI `43811187903`: `0012_budget_ledger.sql`, zero-float `int64`/`u128` Two-Phase `Micro-USD` Budget Ledger, `OrphanReservationReaper`, hourly rollups, `EXACT`/`ESTIMATED`/`UNMETERED_FLAGGED` accuracy stamps, and `/v1/*` + `/native/*` budget integration), **`TP-IMP-0022`** (`DONE`, accepted under `DEC-034`/`DEC-036` at `331113e6` / CI `43825991960`: `0013_traces_blobs.sql`, zero-prompt-byte `privacy.Redactor` sink boundary, bounded `<= 100k` series `/metrics` + `INC-01..07` Prometheus alerts, `<= 32 MiB` non-blocking `Drop-Oldest` OTLP trace exporter + stage waterfall, `Project DEK`-encrypted `<= 72h` debug blobs & TTL cleaner), **`TP-IMP-0023`** (`DONE`, accepted under `DEC-034`/`DEC-037` at `53e0adc9` / CI `43881564733`: `0014_audit_killswitch.sql`, immutable per-`Org` & `"SYSTEM"` `RFC 8785 JCS` hash-chained audit log, `VerifyAuditChain`, 4-level `< 1s` `killswitch.Registry`, and `BlastRadius` forensics), plus **`TP-IMP-0024`** (`DONE`, accepted under `DEC-034`/`DEC-038` at `0abbbc86` / CI `43900067452`: Admin Control Plane REST API Part 1 `/admin/v1/*` server middleware, `ETag`/`If-Match` `409`/`428` OCC, `org_id`-HMAC cursor pagination, `410` Bootstrap, Session/`X-CSRF-Token` + IP brute-force `429`, Tenancy, 6-Role `RBAC`, and Show-Once `Virtual Keys`).
>
> **Current Queue Posture (`24 DONE`, `1 READY` [`TP-IMP-0025`], `0 READY_FOR_REVIEW`, `22 BLOCKED`):** On `2026-11-16` (`8cfe2492`, CI `43798260070`), `DEC-033` remained **`REVOKED_BY_OWNER`** and the Product Owner formally opened **`TP-IMP-0021`..`0030`** under **[`DEC-034`](./docs/governance/decisions.md)** (delegated sequentially to **Gemini 4 Hydrogen**). With `TP-IMP-0021` (`DEC-035`), `TP-IMP-0022` (`DEC-036`), `TP-IMP-0023` (`DEC-037`), and `TP-IMP-0024` (`DEC-038`) accepted, DoR-verified **[`TP-IMP-0025`](./docs/planning/units/TP-IMP-0025.md)** (Admin Control Plane REST API Part 2) unlocks to **`READY`** while **`TP-IMP-0026`..`0047` (`22 / 47`) remain `BLOCKED`** behind upstream dependencies, `REQUIRES_STRONG_REVIEW` gates, sequential `DEC-034` progression (`0026`..`0030`), and direct human Product Owner authorization (`0031`..`0047`). Downstream Data Plane and Control Plane ingress (`TP-IMP-0025`+) MUST authenticate via `Manager` APIs enforcing `CheckServingReady()` rather than bypassing via `Current().AuthenticateKey`, and downstream wiring of `BlindHostCredentialBroker` MUST explicitly populate **`Config.PolicyResolver`** alongside `Config.Resolver` for credentialed connections; Admin API `/admin/v1/*` Part 2 (`0025`), Web Console (`0026`–`0030`), remote OCI/Marketplace sync (`0035`), `AWS_SIGV4`/`OAuth 2.1`, `MCP` Hub (`0036`–`0037`), `Tier-3 Sidecar` (`0042`), live `Valkey` Lua cluster (`0040`, `COND-PO-03`), cluster revocation SLAs (`< 5s`), cluster replica lag, OS-level network isolation on macOS/Windows, `PostgreSQL 16` live DDL (`TP-IMP-0039`), Stage 2–4 CI, and all unmeasured Gateway SLOs remain `TARGET / UNVERIFIED`.

---

## Vision & Core Capabilities

Tanpopo is designed to decouple your AI infrastructure from the release cycle of any single gateway vendor or model provider (`DEC-001`, `DEC-002`):

1. **Extension-First Provider Connectivity:** Install, verify, upgrade, and roll back **Provider Adapters** and extensions (`.tpkg`) from signed Marketplace indexes, pinned Git commits, URLs, or private OCI registries **without rebuilding or restarting Tanpopo Core**.
2. **Strict Four-Plane Separation (`DEC-003`):** First-class, cleanly separated domain models, wire protocols, and security policies across:
   - **Provider Plane:** OpenAI-compatible Unified Inference (`/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/models`, `/v1/rerank`) plus lossless native dialect passthrough (`/native/{dialect}/*`).
   - **Plugin / Extension Mechanism:** Phased isolation (`Tier-1 Declarative Packs`, `Tier-2 Pure-Go Wasm Capability Sandbox`, and `Tier-3 Isolated mTLS Sidecars`) paired with a **Blind Host Credential Broker** so untrusted plugins never read raw vault secrets.
   - **MCP Governance Plane (`/mcp`):** Scoped per-tool ACLs (`<server>__<tool>`), cryptographic `RFC 8785 JCS` `schema_hash` pinning against rug-pulls, zero blind retries on side-effectful tools, and dual-mode Human-in-the-Loop (`destructive`) approvals.
   - **A2A Inter-Agent Plane (`/a2a/*`):** Authenticated `AgentCard` discovery, asynchronous task lifecycle orchestration (`SM-09`), monotonic privilege/sub-budget attenuation, and recursion/hop ceilings (`hop_count <= 5`).
3. **Production Governance, FinOps & Self-Hosted SRE:**
   - **Capability-Aware Routing & Hard Data Residency:** Deterministic 6-step routing with sliding-window circuit breakers and zero cross-region leakage on failover (`Fail-Closed`).
   - **Two-Phase Integer `Micro-USD` Budget Ledger:** Pre-request holds + honest `EXACT / ESTIMATED / UNMETERED_FLAGGED` settlement tiers, preventing concurrent spend overruns without mid-stream truncation.
   - **Privacy-First Observability & Tamper-Evident Audit:** `0 byte` prompt/completion persistence by default, bounded non-blocking OpenTelemetry export, `RFC 8785 JCS` hash-chained audit logs, and 4-level `< 1s` / `< 5s` emergency kill-switches.
   - **Scale-From-Zero Topology (Proposed `ADR-0001..0004`):** Single static `tanpopo` binary embedding the **Astryx Design System (`neutral`)** Web Console and `SQLite WAL` for `< 3 minute` standalone onboarding, scaling horizontally to stateless **`PostgreSQL 16+ (FORCE RLS)` + `Valkey`** clusters and full **`Air-Gap`** environments.

---

## Official vs. Community Policy & Compliance Boundary (`DEC-004`)

Tanpopo enforces a strict, non-negotiable compliance boundary across all code, SDKs, documentation, and official registries:

- **Official Adapters** exclusively target published commercial Provider APIs, self-hosted inference servers (`vLLM`, `SGLang`, `Ollama`), and provider-authorized OAuth 2.1 / enterprise IAM flows.
- **Zero Consumer Subscription Misuse:** Tanpopo **does not** build, bundle, list, document, or endorse connectors that scrape or repurpose personal consumer subscriptions (such as `ChatGPT Plus/Pro`, `Claude Pro/Max`, `Codex`, `Antigravity`, etc.) into general-purpose APIs — nor does Tanpopo use "accept-risk" UI disclaimers to excuse Provider Terms-of-Service violations (`REJ-003`).

---

## Documentation & Engineering Handoff Hub

Start here based on your role:

| Role / Goal | Canonical Entrypoint |
|---|---|
| **Product Owner (Pre-Build Gate Sign-Off)** | **[`docs/governance/owner-approval-sheet.md`](./docs/governance/owner-approval-sheet.md)** & [`docs/governance/decisions.md`](./docs/governance/decisions.md) |
| **Cold-Start Engineering Agent ("Luna")** | **[`AGENTS.md`](./AGENTS.md)** → **[`docs/governance/execution-baseline.md`](./docs/governance/execution-baseline.md)** → **[`docs/planning/implementation-index.md`](./docs/planning/implementation-index.md)** |
| **Full Documentation Index (`110` Docs)** | **[`docs/README.md`](./docs/README.md)** |
| **Independent Readiness & Spec Audits** | [`docs/reviews/execution-readiness.md`](./docs/reviews/execution-readiness.md) (`Stage 2: GO`), [`docs/reviews/integrated-audit.md`](./docs/reviews/integrated-audit.md) (`Historical A08`), [`docs/reviews/unresolved-gaps.md`](./docs/reviews/unresolved-gaps.md) (`13/13 CLOSED`) |
| **Requirements, Architecture & Security** | [`docs/requirements/srs.md`](./docs/requirements/srs.md), [`docs/architecture/system-design.md`](./docs/architecture/system-design.md), [`docs/security/threat-model.md`](./docs/security/threat-model.md), [`docs/quality/release-readiness.md`](./docs/quality/release-readiness.md) |

---

## Current Repository Layout

```text
.
├── AGENTS.md / GEMINI.md        # Astryx v0.7.16 UI rules, CONSTRAINTS.md contract & Cold-start Agent gate
├── CONSTRAINTS.md               # Repository quality bar & Floor Guard enforcement rules
├── LICENSE                      # Apache License 2.0
├── README.md                    # Project overview & M1 bootstrap status (this file)
├── go.mod                       # Root Go module (github.com/nguywnben/tanpopo, Go 1.24.0)
├── cmd/tanpopo/                 # Unified tanpopo CLI bootstrap entrypoint
├── internal/                    # Canonical 19-package Go DAG skeleton (doc.go boundaries)
├── scripts/ci/check.go          # Pure-Go cross-OS Floor Guard, DAG, license & lockfile gate
├── .github/workflows/ci.yml     # Stage 0/1 Hermetic GitHub Actions CI workflow
├── buf.yaml / proto/            # Canonical Extension Runtime Protobuf v1 contract (compiled in M0)
├── openapi/                     # Canonical OpenAPI 3.1.0 Data Plane & Admin Control Plane specs
├── db/spec-check/               # Canonical 26-group SQLite WAL & PostgreSQL 16 (FORCE RLS) reference DDL
├── tools/spikes/                # Isolated pure-Go (CGO_ENABLED=0) M0 feasibility spikes (SPIKE-01..04)
├── web/console/                 # Relocated Apache-2.0 Astryx v0.7.16 Web Console (package.json, astryx-themes.js, AGENTS.md, src/)
├── .agents/                     # 25 curated SDLC engineering skills, checklists & reviewer personas
└── docs/                        # Complete English engineering specifications & 47-unit execution plan
    ├── governance/              # Charter, Decision Register, Assumptions, Execution Baseline & Owner Approval Sheet
    ├── discovery/               # Market landscape, user JTBD, feasibility, metrics & risk register
    ├── product/                 # Glossary, PRD, 16-feature catalog & phased release scope
    ├── requirements/            # Entity/state maps, workflows, business rules, SRS, ACs & traceability
    ├── specifications/          # Extension SDK, adapters, auth, marketplace, MCP/A2A, routing, FinOps & APIs
    ├── architecture/            # System design, dual-mode SQL data model, HA topology & ADR-0001..0006
    ├── ux/                      # Information architecture, user flows, 16 screen specs & Astryx integration
    ├── operations/              # Multi-OS/Air-Gap installers, backups/DR, monitoring/runbooks & CLI DX
    ├── security/                # STRIDE/OWASP threat model, sandbox/supply-chain & envelope encryption/RLS
    ├── quality/                 # Contract/auth/fuzz matrix, hermetic CI strategy, SLO/FMEA & release gates
    ├── reviews/                 # Historical A08 Audit, Closed Gap Register (13/13) & Stage 2 Readiness (GO)
    └── planning/                # Roadmap, Milestone Map, DAG, Master Index & units/TP-IMP-0001..0047.md
```

---

## License

Licensed under the **Apache License, Version 2.0**. See [`LICENSE`](./LICENSE) for the full text.
