# 00 — TEEP-01 Product & Technology Intelligence Master Index

- **Mission:** `TEEP-01` — Product & Technology Intelligence (Tanpopo Enterprise Product Expansion Program)
- **Research Execution Date:** `2026-11-27`
- **Delivery Branch:** `planning/teep-01-intelligence`
- **Authoritative Implementation Baseline:** Frozen RC9 `main` (`ac9ae87a68f095203de82968ac5d08729e33a531` — *Unsigned RC9, `BASELINE_STABLE`*)
- **Historical QA Evidence Source:** `origin/qa/qa-005-final-rc9` (`5ee28dc0cfd95aa990c3fbe878beadffed3b9b6f`) & `origin/signing/rc7-owner-manifest` (`7503607c2188e7a314d355864e813960244f6c15`)
- **Governance Gate Status:** `TEEP-01` Research Review Gate (`GATE-REL-07` remains `BLOCKED`; `0` production code/spec mutations; `0` paid API calls; `0` tags/releases).

---

## 1. Complete Deliverable Package Map (`docs/teep/01-intelligence/`)

| Deliverable File | Document ID | Primary Purpose & Key Contents |
|---|---|---|
| [`00-RESEARCH-INDEX.md`](./00-RESEARCH-INDEX.md) | `TEEP-01-DOC-00` | Package map, epistemic taxonomy, canonical ID namespaces, and end-to-end traceability summary. |
| [`01-EXECUTIVE-SYNTHESIS.md`](./01-EXECUTIVE-SYNTHESIS.md) | `TEEP-01-DOC-01` | Strategic thesis, 2026 market upheaval synthesis, RC9 audit verdict, 6 strategic pillars, and candidate horizon map. |
| [`02-MARKET-LANDSCAPE.md`](./02-MARKET-LANDSCAPE.md) | `TEEP-01-DOC-02` | Evidence-backed analysis of 2025–2026 M&A/archivals (`Portkey`, `TensorZero`, `llm-guard`, `Langfuse`, `Helicone`, `Promptfoo`, `Smithery`, `Arize`), LiteLLM supply-chain/CVE breaches, and 8 operator pain points. |
| [`03-COMPETITIVE-CAPABILITY-MATRIX.md`](./03-COMPETITIVE-CAPABILITY-MATRIX.md) | `TEEP-01-DOC-03` | Side-by-side matrix across 19+ AI gateways, observability, security, inference (`llm-d`/`vLLM`), MCP, and APIM platforms + honest assessment of where competitors currently outperform RC9. |
| [`04-CURRENT-TANPOPO-PRODUCT-AUDIT.md`](./04-CURRENT-TANPOPO-PRODUCT-AUDIT.md) | `TEEP-01-DOC-04` | Code-verified audit of frozen RC9 (`ac9ae87a`) across all 14 required areas, proving 9 unwired/contract-only surfaces, 4 `404` OpenAPI routes, 17 unexposed UI operations, and Day-0 `403` setup friction. |
| [`05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md`](./05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md) | `TEEP-01-DOC-05` | Nov 2026 provider APIs (`OpenAI`, `Anthropic`, `Gemini 4`, `Bedrock/Mantle`, `Foundry`, `Cohere v2`, `Mistral`, `Ollama`), 4-tier translation matrix, MCP `2026-07-28` stateless spec, LF A2A `v1.0.1`, OAuth/OBO/OTel standards, and live empirical audit of `litellm_model_prices.json` (`7,193` models), `models.dev` (`10,078`), & `OpenRouter` (`476`). |
| [`06-PERSONAS-AND-USER-NEEDS.md`](./06-PERSONAS-AND-USER-NEEDS.md) | `TEEP-01-DOC-06` | 10 canonical personas (`PER-01` solo self-hoster through `PER-06` regulated CISO/DPO, `PER-07` FinOps, `PER-09` extension publisher, and `PER-10` machine workload) + tension resolution principles. |
| [`07-BUSINESS-WORKFLOW-ANALYSIS.md`](./07-BUSINESS-WORKFLOW-ANALYSIS.md) | `TEEP-01-DOC-07` | 14 end-to-end workflows (`WF-01`..`WF-14`) comparing current RC9 clicks/breakpoints against target progressive-disclosure (`< 90s` `HYPOTHESIS / UNVERIFIED`) and Day-2 enterprise workflows. |
| [`08-FUNCTIONAL-DOMAIN-RESEARCH/`](./08-FUNCTIONAL-DOMAIN-RESEARCH/00-DOMAIN-MAP.md) | `TEEP-01-DOC-08-00..06` | 7-file deep domain research library covering mandated Domains `01`..`20` plus 5 newly discovered enterprise domains (`DOM-21`..`DOM-25`). |
| [`09-ENTERPRISE-REQUIREMENTS-RESEARCH.md`](./09-ENTERPRISE-REQUIREMENTS-RESEARCH.md) | `TEEP-01-DOC-09` | Multi-level tenancy (`enterprises` + `org_units` `[DESIGN_PROPOSAL]`), SCIM/SAML/ABAC/WIF, EU AI Act / GDPR Art. 17 / ISO 42001 technical controls, high-RPS (`[HYPOTHESIS]`) scale blueprint, active-active sovereign cells, and `12` quality attribute scenarios (`QAS-ENT-01..12`). |
| [`10-EXTENSION-AND-MARKETPLACE-RESEARCH.md`](./10-EXTENSION-AND-MARKETPLACE-RESEARCH.md) | `TEEP-01-DOC-10` | `wazero` vs `wasip2` runtime analysis, additive `runtime.proto` `v1.1` candidate contracts across **11 `.tpkg` extension categories**, Go/Rust/TS SDKs, and Keyless OIDC Sigstore + TUF + OCI `1.1` curated Marketplace (`[DESIGN_PROPOSAL — TEEP-03]`). |
| [`11-PRODUCT-INNOVATION-OPPORTUNITIES.md`](./11-PRODUCT-INNOVATION-OPPORTUNITIES.md) | `TEEP-01-DOC-11` | `12` researched product innovation concepts (`INN-01`..`INN-12`), including Metadata-Only What-If Traffic Replay, Deprecation Advisor, Recipient-Key (`HPKE`) Flight Recorder, Dual Prefix/KV Affinity, and Cross-Plane Causal Governance. |
| [`12-ARCHITECTURAL-DEBT-AND-RESIDUAL-RISKS.md`](./12-ARCHITECTURAL-DEBT-AND-RESIDUAL-RISKS.md) | `TEEP-01-DOC-12` | Complete preservation of `DEF-001`..`086` (`15` open `PARTIAL`s), `PG-001`..`014`, `RR-001`..`029`, `COND-PO-01`..`05`, and technical root-cause analyses of `ARCH-OBS-01` (`18` loops + `7` PG indexes), `DEF-041`, `DEF-072` (`[SOURCE_VERIFIED]` `handleRetireMasterKEK` PG RLS visibility hazard), `DEF-078`, and `RR-001`. |
| [`13-PRELIMINARY-CAPABILITY-INVENTORY.md`](./13-PRELIMINARY-CAPABILITY-INVENTORY.md) | `TEEP-01-DOC-13` | `161` substantive candidate capabilities (`CAP-001`..`CAP-161`) across `25` research domains (`KEEP: 10`, `IMPROVE: 73`, `REPLACE: 5`, `REMOVE: 1`, `ADD: 72`) with all 11 mandatory attributes. |
| [`14-OPEN-QUESTIONS-AND-DECISION-POINTS.md`](./14-OPEN-QUESTIONS-AND-DECISION-POINTS.md) | `TEEP-01-DOC-14` | `16` structured Founder / Program Coordinator decision points (`OQ-01`..`OQ-16`) feeding `TEEP-02A` / `TEEP-02B` scope governance and `TEEP-03` architecture evaluation. |
| [`15-SOURCE-AND-EVIDENCE-REGISTER.md`](./15-SOURCE-AND-EVIDENCE-REGISTER.md) | `TEEP-01-DOC-15` | `92` primary bibliography & dataset entries (`SRC-001`..`SRC-084` plus `8` letter-suffixed companion entries `SRC-020B`, `023B`, `023C`, `036B`, `040B`..`040E`) with exact URLs, `2026-11-27` retrieval dates, versions, licenses, and epistemic classifications. |
| [`16-TEEP-02-HANDOFF.md`](./16-TEEP-02-HANDOFF.md) | `TEEP-01-DOC-16` | Structured handoff guide, traceability checklist, and operating guardrails for `TEEP-02A` (Master Feature Catalog). |

### Machine-Readable Companion Datasets (`docs/teep/01-intelligence/data/`)
- [`data/preliminary-capability-inventory.json`](./data/preliminary-capability-inventory.json) & [`data/preliminary-capability-inventory.csv`](./data/preliminary-capability-inventory.csv) (`161` capabilities × `15` structured columns)
- [`data/competitive-capability-matrix.csv`](./data/competitive-capability-matrix.csv) (`19` platforms × `30` architectural/capability columns)
- [`data/rc9-subsystem-and-api-audit.json`](./data/rc9-subsystem-and-api-audit.json) (metrics, unwired code ledger, 8 official adapter audit)
- [`data/qa-debt-and-risk-ledger.json`](./data/qa-debt-and-risk-ledger.json) (`DEF-001..086`, 15 `PARTIAL`s, `PG-001..014`, `ARCH-OBS-01`, `RR-001`)
- [`data/source-and-evidence-register.json`](./data/source-and-evidence-register.json) (`92` structured `SRC-*` entries)

---

## 2. Epistemic Taxonomy & Canonical Identifier Conventions (`COND-PO-01`)

1. **Epistemic Labels (Reconciled in `TEEP-02A`):**
   - `[RUNTIME_VERIFIED]` — Executed and measured in frozen RC9 CI, benchmark harnesses (`docs/quality/benchmark-evidence.md`, `spike-results.md`), independent QA-005 (`5ee28dc0`), or TEEP-01 local offline catalog/digest probes.
   - `[SOURCE_VERIFIED]` — Confirmed via static source, call-graph, SQL migration, or OpenAPI inspection of `ac9ae87a` or primary external repositories/RFCs on `2026-11-27`, **not yet independently reproduced against a live runtime deployment**.
   - `[VERIFIED_FACT]` — Umbrella label for primary external facts (NVD/CISA advisories, corporate M&A filings, published protocol specifications, or live public JSON catalog snapshots retrieved `2026-11-27`).
   - `[VENDOR_CLAIM]` — Competitor-published benchmark, customer metric, or latency/cost claim not independently audited under neutral conditions.
   - `[INFERRED_BEHAVIOR]` — Architectural consequence logically derived from verified source layout, runtime stack, or protocol design.
   - `[DESIGN_PROPOSAL]` — Candidate target design or technical mechanism whose final selection (e.g., `.tpkg` signing target, policy engine, SDK runtime, fixed-point monetary scale, distributed budget admission algorithm) is deferred to `TEEP-03` architecture evaluation.
   - `[HYPOTHESIS]` / `HYPOTHESIS / UNVERIFIED` — Falsifiable performance, latency, availability, or workflow efficiency target awaiting `TEEP-03`/`04` benchmark verification.
   - `[UNVERIFIED_ASSUMPTION]` — Secondary market pricing commentary where private enterprise quotes are undisclosed.
2. **Identifier Namespaces:** `DOM-01..25` (TEEP-01 Research Domains; expanded to `DOM-01..26` in `TEEP-02A`), `CAP-001..161` (Candidate Capabilities), `PER-01..10` (Personas), `WF-01..14` (Workflows), `INN-01..12` (Product Innovations), `QAS-ENT-01..12` (Enterprise Quality Attribute Scenarios), `OQ-01..16` (Open Decision Points), `SRC-001..084` (`92` Source Entries including `8` suffixed sub-entries), preserving all existing `DEF-001..086`, `PG-001..014`, `RR-001..029`, `ARCH-OBS-01`, `COND-PO-01..05`, `DEC-001..097`, and `ADR-0001..0007` identifiers unchanged.

---

## 3. `TEEP-02A` Research Reconciliation & Errata Log (`2026-11-28`, `ACCEPTED WITH CORRECTIONS`)

While preserving the original `planning/teep-01-intelligence` branch untouched at `a9f8467ddfd10f2a7962aae8f213e7f3277f2bdd`, the following reconciliations were applied on `planning/teep-02a-feature-catalog` per Research Review direction:

1. **Standardized Counts Across All Documents:** Reconciled **6 Strategic Pillars** (`00`, `01`, `16`), **12 Researched Innovations (`INN-01`..`INN-12`)** (`00`, `11`, `16`), **12 Enterprise Quality Attribute Scenarios (`QAS-ENT-01`..`QAS-ENT-12`)** (`00`, `09`, `16` — adding `QAS-ENT-11` KEK Rotation RLS Integrity and `QAS-ENT-12` Air-Gap Zero-Egress Verification), and **`92` Source Register Entries** (`SRC-001`..`SRC-084` + `8` letter-suffixed companion entries).
2. **Separated `[SOURCE_VERIFIED]` Static Findings from `[RUNTIME_VERIFIED]` Defects:** Explicitly reclassified the PostgreSQL `handleRetireMasterKEK` `FORCE RLS` visibility hazard (`ops_lifecycle.go:196`) across `01`, `04`, `07`, `12`, `13`, `16`, and `data/*.json` as a **`[SOURCE_VERIFIED]` suspected P0 data-integrity risk requiring non-destructive runtime reproduction and remediation before production use**, rather than an already runtime-reproduced QA defect.
3. **Removed Absolute & Unsupported Claims:** Replaced unqualified security assertions (*"structurally impossible"*, *"unbreakable"*, *"Zero-Knowledge"*, *"eliminates permanently"*), unverified market-uniqueness claims (*"No single platform"*, *"unique to Tanpopo"*, *"category-unique"*, *"no competitor"*), compliance overstatements (*"Zero-Tension Resolution"*), unconditional pricing-fidelity claims (*"100% exact integer representation of every real vendor token price"*), and unqualified performance headlines with bounded, evidence-scoped engineering statements and explicit `TEEP-03` architecture deferral markers.
