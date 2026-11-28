# 08.00 — Functional Domain Research Map (`DOM-01` .. `DOM-25`)

- **Document ID:** `TEEP-01-DOC-08-00`
- **Research Execution Date:** `2026-11-27`
- **Scope:** Maps the **20 mandated baseline domains (`DOM-01`..`DOM-20`)** plus **5 newly identified enterprise AI infrastructure domains (`DOM-21`..`DOM-25`)** discovered during protocol, competitor, and architectural intelligence research (`161` candidate capabilities `CAP-001`..`CAP-161` total; zero trivial CRUD inflation).

---

## 1. Master 25-Domain Research Directory

| Domain ID | Functional Domain | Origin | Research File | RC9 Maturity | Candidate Capability Range (`CAP-*`) |
|---|---|---|---|---|---|
| **`DOM-01`** | **Universal AI Gateway** | Mandated | [`01-GATEWAY-PROVIDERS-AND-MODELS.md`](./01-GATEWAY-PROVIDERS-AND-MODELS.md) | `PARTIAL` | `CAP-001` .. `CAP-008` (`8`) |
| **`DOM-02`** | **Provider Management** | Mandated | [`01-GATEWAY-PROVIDERS-AND-MODELS.md`](./01-GATEWAY-PROVIDERS-AND-MODELS.md) | `PARTIAL` | `CAP-009` .. `CAP-015` (`7`) |
| **`DOM-03`** | **Model Intelligence** | Mandated (`6.A`) | [`01-GATEWAY-PROVIDERS-AND-MODELS.md`](./01-GATEWAY-PROVIDERS-AND-MODELS.md) | `MINIMAL` | `CAP-016` .. `CAP-022` (`7`) |
| **`DOM-04`** | **Routing and Orchestration** | Mandated | [`02-ROUTING-RELIABILITY-FINOPS-AND-CACHING.md`](./02-ROUTING-RELIABILITY-FINOPS-AND-CACHING.md) | `PARTIAL` | `CAP-023` .. `CAP-030` (`8`) |
| **`DOM-05`** | **Reliability Engineering** | Mandated | [`02-ROUTING-RELIABILITY-FINOPS-AND-CACHING.md`](./02-ROUTING-RELIABILITY-FINOPS-AND-CACHING.md) | `PARTIAL` | `CAP-031` .. `CAP-036` (`6`) |
| **`DOM-06`** | **AI FinOps** | Mandated | [`02-ROUTING-RELIABILITY-FINOPS-AND-CACHING.md`](./02-ROUTING-RELIABILITY-FINOPS-AND-CACHING.md) | `PARTIAL` | `CAP-037` .. `CAP-045` (`9`) |
| **`DOM-07`** | **Caching and Performance** | Mandated | [`02-ROUTING-RELIABILITY-FINOPS-AND-CACHING.md`](./02-ROUTING-RELIABILITY-FINOPS-AND-CACHING.md) | `BOTTLENECKED` | `CAP-046` .. `CAP-052` (`7`) |
| **`DOM-08`** | **Observability** | Mandated | [`03-OBSERVABILITY-SECURITY-IAM-AND-COMPLIANCE.md`](./03-OBSERVABILITY-SECURITY-IAM-AND-COMPLIANCE.md) | `PARTIAL` | `CAP-053` .. `CAP-059` (`7`) |
| **`DOM-09`** | **AI Security and Guardrails** | Mandated | [`03-OBSERVABILITY-SECURITY-IAM-AND-COMPLIANCE.md`](./03-OBSERVABILITY-SECURITY-IAM-AND-COMPLIANCE.md) | `PARTIAL` (Rails unwired) | `CAP-060` .. `CAP-067` (`8`) |
| **`DOM-10`** | **Identity and Access Management** | Mandated | [`03-OBSERVABILITY-SECURITY-IAM-AND-COMPLIANCE.md`](./03-OBSERVABILITY-SECURITY-IAM-AND-COMPLIANCE.md) | `PARTIAL` | `CAP-068` .. `CAP-075` (`8`) |
| **`DOM-11`** | **Governance and Compliance** | Mandated | [`03-OBSERVABILITY-SECURITY-IAM-AND-COMPLIANCE.md`](./03-OBSERVABILITY-SECURITY-IAM-AND-COMPLIANCE.md) | `PARTIAL` | `CAP-076` .. `CAP-082` (`7`) |
| **`DOM-12`** | **MCP Infrastructure** | Mandated | [`04-AGENTIC-MCP-A2A-PROMPTS-EVALS-AND-CONTEXT.md`](./04-AGENTIC-MCP-A2A-PROMPTS-EVALS-AND-CONTEXT.md) | `PARTIAL` (`2025-03-26` only) | `CAP-083` .. `CAP-090` (`8`) |
| **`DOM-13`** | **Agent and A2A Infrastructure** | Mandated | [`04-AGENTIC-MCP-A2A-PROMPTS-EVALS-AND-CONTEXT.md`](./04-AGENTIC-MCP-A2A-PROMPTS-EVALS-AND-CONTEXT.md) | `PARTIAL` (Custom REST) | `CAP-091` .. `CAP-096` (`6`) |
| **`DOM-14`** | **Prompt Management** | Mandated | [`04-AGENTIC-MCP-A2A-PROMPTS-EVALS-AND-CONTEXT.md`](./04-AGENTIC-MCP-A2A-PROMPTS-EVALS-AND-CONTEXT.md) | `MISSING` | `CAP-097` .. `CAP-099` (`3`) |
| **`DOM-15`** | **AI Evaluation** | Mandated | [`04-AGENTIC-MCP-A2A-PROMPTS-EVALS-AND-CONTEXT.md`](./04-AGENTIC-MCP-A2A-PROMPTS-EVALS-AND-CONTEXT.md) | `MISSING` (SDK cassettes only) | `CAP-100` .. `CAP-103` (`4`) |
| **`DOM-16`** | **Infrastructure Operations** | Mandated | [`05-OPERATIONS-EXTENSIONS-MARKETPLACE-AND-INTEGRATIONS.md`](./05-OPERATIONS-EXTENSIONS-MARKETPLACE-AND-INTEGRATIONS.md) | `PARTIAL` | `CAP-104` .. `CAP-110` (`7`) |
| **`DOM-17`** | **Extension Platform and SDK** | Mandated (`6.C`) | [`05-OPERATIONS-EXTENSIONS-MARKETPLACE-AND-INTEGRATIONS.md`](./05-OPERATIONS-EXTENSIONS-MARKETPLACE-AND-INTEGRATIONS.md) | `PARTIAL` (`1/4` kinds wired) | `CAP-111` .. `CAP-117` (`7`) |
| **`DOM-18`** | **Marketplace and Community Ecosystem** | Mandated (`6.D`) | [`05-OPERATIONS-EXTENSIONS-MARKETPLACE-AND-INTEGRATIONS.md`](./05-OPERATIONS-EXTENSIONS-MARKETPLACE-AND-INTEGRATIONS.md) | `PARTIAL` (`RR-001`) | `CAP-118` .. `CAP-124` (`7`) |
| **`DOM-19`** | **Notifications and Integrations** | Mandated | [`05-OPERATIONS-EXTENSIONS-MARKETPLACE-AND-INTEGRATIONS.md`](./05-OPERATIONS-EXTENSIONS-MARKETPLACE-AND-INTEGRATIONS.md) | `PARTIAL` (`4` webhook events) | `CAP-125` .. `CAP-129` (`5`) |
| **`DOM-20`** | **Product and Developer Experience** | Mandated (`6.E`) | [`06-PRODUCT-DX-AND-DECLARATIVE-GITOPS.md`](./06-PRODUCT-DX-AND-DECLARATIVE-GITOPS.md) | `PARTIAL` | `CAP-130` .. `CAP-136` (`7`) |
| **`DOM-21`** | **Async, Batch, Realtime & Multimodal Media Workloads** | **Discovered** | [`01-GATEWAY-PROVIDERS-AND-MODELS.md`](./01-GATEWAY-PROVIDERS-AND-MODELS.md) | `MINIMAL` (`blob_ref` only) | `CAP-137` .. `CAP-142` (`6`) |
| **`DOM-22`** | **Context, Memory, RAG & Vector Governance** | **Discovered** | [`04-AGENTIC-MCP-A2A-PROMPTS-EVALS-AND-CONTEXT.md`](./04-AGENTIC-MCP-A2A-PROMPTS-EVALS-AND-CONTEXT.md) | `PARTIAL` (`/v1/embeddings`+`rerank`) | `CAP-143` .. `CAP-146` (`4`) |
| **`DOM-23`** | **Privacy Engineering, Data Sovereignty & Cryptographic Custody** | **Discovered** | [`03-OBSERVABILITY-SECURITY-IAM-AND-COMPLIANCE.md`](./03-OBSERVABILITY-SECURITY-IAM-AND-COMPLIANCE.md) | `PARTIAL` | `CAP-147` .. `CAP-151` (`5`) |
| **`DOM-24`** | **Resilience, Chaos Engineering & Pre-Flight Policy Simulation** | **Discovered (`6.F`)** | [`05-OPERATIONS-EXTENSIONS-MARKETPLACE-AND-INTEGRATIONS.md`](./05-OPERATIONS-EXTENSIONS-MARKETPLACE-AND-INTEGRATIONS.md) | `MINIMAL` (Test-only `fmea`) | `CAP-152` .. `CAP-156` (`5`) |
| **`DOM-25`** | **GitOps, Terraform/IaC & Portable Declarative Solution Packs** | **Discovered (`6.F`)** | [`06-PRODUCT-DX-AND-DECLARATIVE-GITOPS.md`](./06-PRODUCT-DX-AND-DECLARATIVE-GITOPS.md) | `MINIMAL` (File-copy `gitops`) | `CAP-157` .. `CAP-161` (`5`) |

---

## 2. Rationale for the 5 Newly Identified Domains (`DOM-21` .. `DOM-25`)

1. **`DOM-21` (Async, Batch, Realtime & Multimodal Media Workloads):** `2026` enterprise spend increasingly shifts to `50%`-discounted `/v1/batches`, `2h`/`8h` `background: true` Responses jobs, WebSocket/WebRTC Realtime voice (`GPT-Live-1.1`, `Gemini Live`), and per-page OCR / image / audio generation — all of which break synchronous request-response budget pre-holds (`SM-07` `90s` reaper) if crammed into `DOM-01`.
2. **`DOM-22` (Context, Memory, RAG & Vector Governance):** Stateful server-side threads (`/v1/conversations`, `previous_response_id`, `/v1beta/interactions`), prompt compaction (`/v1/responses/compact`), vector-space-locked embedding failover, and MCP context compression require dedicated state and token-window governance distinct from stateless routing.
3. **`DOM-23` (Privacy Engineering, Data Sovereignty & Cryptographic Custody):** Supporting GDPR Art. 17 erasure alongside immutable JCS audit chains, recipient-key-encrypted (`HPKE`) incident capsules, dual-custody `SEC 17a-4` archives, and sovereign regional cells goes deeper than RBAC (`DOM-10`) or checklists (`DOM-11`).
4. **`DOM-24` (Resilience, Chaos Engineering & Pre-Flight Policy Simulation):** Elevates `What-If` historical traffic replay, 8-layer SPOF cut-set analysis, and DORA loopback chaos drills into a first-class operational discipline.
5. **`DOM-25` (GitOps, Terraform/IaC & Portable Declarative Solution Packs):** Bridges standalone SQLite developers and Kubernetes GitOps platform teams via database-reconciling `tanpopo config plan | apply`, an official OpenTofu/Terraform provider, and signed `.tpkg` `SOLUTION_PACK`s.
