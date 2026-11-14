# Tanpopo

**Open-Source, Self-Hosted-First, Extension-First AI Gateway for Models, MCP Tools, and Inter-Agent (A2A) Connectivity**

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)

> [!IMPORTANT]
> **Current Project Status — Pre-Build Governance & Specification Phase (`HANDOFF_VERDICT: NO_GO`)**
>
> Tanpopo has completed its end-to-end product discovery, requirements engineering, extension contract design, solution architecture, UX/SRE operations design, security threat modeling, adversarial audit, and 47-unit implementation planning framework (`ASTRA A00–A10`).
>
> **No runtime gateway, CLI, plugin sandbox, or production Web Console code has been implemented yet** (only the initial Astryx UI theme setup and placeholder in [`src/`](./src) exist on disk). Execution of implementation units (`TP-IMP-0001`..`TP-IMP-0047`) is currently **`BLOCKED` (`0 / 47` `READY`)** awaiting explicit Product Owner sign-off on **[`docs/governance/owner-approval-sheet.md`](./docs/governance/owner-approval-sheet.md)**.

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
| **Independent Readiness & Spec Audits** | [`docs/reviews/execution-readiness.md`](./docs/reviews/execution-readiness.md) (`A10: NO_GO`), [`docs/reviews/integrated-audit.md`](./docs/reviews/integrated-audit.md) (`A08: FAIL`), [`docs/reviews/unresolved-gaps.md`](./docs/reviews/unresolved-gaps.md) (`CR-01..10`) |
| **Requirements, Architecture & Security** | [`docs/requirements/srs.md`](./docs/requirements/srs.md), [`docs/architecture/system-design.md`](./docs/architecture/system-design.md), [`docs/security/threat-model.md`](./docs/security/threat-model.md), [`docs/quality/release-readiness.md`](./docs/quality/release-readiness.md) |

---

## Current Repository Layout

```text
.
├── AGENTS.md / GEMINI.md        # Astryx v0.7.13 UI rules + Cold-start Engineering Agent handoff gate
├── LICENSE                      # Apache License 2.0
├── README.md                    # Project overview & pre-build status (this file)
├── package.json                 # Initial Astryx v0.7.13 + StyleX setup (relocates to web/console/ in TP-IMP-0005)
├── astryx-themes.js             # Generated Astryx 'neutral' theme module (do not edit manually)
├── src/                         # Unbundled UI placeholder (replaced by template-scaffolded pages in M2)
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
    ├── reviews/                 # A08 Specification Audit (FAIL), Unresolved Gaps (CR-01..10) & A10 Readiness (NO_GO)
    └── planning/                # Roadmap, Milestone Map, DAG, Master Index & units/TP-IMP-0001..0047.md
```

---

## License

Licensed under the **Apache License, Version 2.0**. See [`LICENSE`](./LICENSE) for the full text.
