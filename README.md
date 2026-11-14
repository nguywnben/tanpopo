# Tanpopo

**Open-Source, Self-Hosted-First, Extension-First AI Gateway for Models, MCP Tools, and Inter-Agent (A2A) Connectivity**

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)

> [!IMPORTANT]
> **Current Project Status — Gate G4 Approved (`HANDOFF_VERDICT: GO` · `TP-IMP-0001..0007` `DONE` · `TP-IMP-0008` `READY`)**
>
> Tanpopo has completed its end-to-end discovery, requirements, architecture, UX/SRE, security, and 47-unit planning framework (`ASTRA A00–A10`), Milestone `M0` (`TP-IMP-0001`..`0004` = `DONE`), [`TP-IMP-0005`](./docs/planning/units/TP-IMP-0005.md) (`DONE` — `DEC-011`), shared hermetic test harness [`TP-IMP-0006`](./docs/planning/units/TP-IMP-0006.md) ([`internal/testkit/*`](./internal/testkit/doc.go), `DONE` — `DEC-012`/`DEC-013`, CI Run `43590868455`), and **storage ports, `UUIDv7` IDs, Expand-Only migrations, and pure-Go `SQLite WAL` driver [`TP-IMP-0007`](./docs/planning/units/TP-IMP-0007.md) (`DONE` — Product Owner accepted via `DEC-015` at commit `4cc8b6ad` and GitHub Actions Run `43595815734` `SUCCESS` with `-race` storage coverage `86.2%` on `ubuntu-24.04` `unshare --net` and `87.0%` on `macos-15`)**.
>
> **[`TP-IMP-0008`](./docs/planning/units/TP-IMP-0008.md) (`Three-Tier Envelope Encryption Vault`) is the sole `READY` unit (`1 / 47`):** **`TP-IMP-0009`..`0047` (`39 / 47`) remain `BLOCKED`** behind upstream dependencies and `REQUIRES_STRONG_REVIEW` gates, no gateway HTTP servers or production Web Console screens exist yet, OS-level network isolation on macOS/Windows remains `UNVERIFIED`, `PostgreSQL 16` live DDL (`TP-IMP-0039`) and Stage 2–4 CI remain `UNVERIFIED`, and all unmeasured Gateway SLOs remain `TARGET / UNVERIFIED`.

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
├── AGENTS.md / GEMINI.md        # Astryx v0.7.13 UI rules, CONSTRAINTS.md contract & Cold-start Agent gate
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
├── web/console/                 # Relocated Apache-2.0 Astryx v0.7.13 Web Console (package.json, astryx-themes.js, src/)
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
