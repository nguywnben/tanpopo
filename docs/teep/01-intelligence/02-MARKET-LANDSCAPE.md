# 02 — Market & Ecosystem Landscape Intelligence (November 2026)

- **Document ID:** `TEEP-01-DOC-02`
- **Research Execution Date:** `2026-11-27`
- **Baseline Compared:** Frozen RC9 (`ac9ae87a68f095203de82968ac5d08729e33a531`) and Phase ASTRA Discovery (`docs/discovery/market-and-competitors.md`, `2026-11-11`)
- **Machine-Readable Companions:** [`data/competitive-capability-matrix.csv`](./data/competitive-capability-matrix.csv), [`data/source-and-evidence-register.json`](./data/source-and-evidence-register.json)
- **Epistemic Convention (`COND-PO-01`):** Every material claim carries `[VERIFIED_FACT]`, `[VENDOR_CLAIM]`, `[INFERRED_BEHAVIOR]`, `[HYPOTHESIS]`, or `[UNVERIFIED_ASSUMPTION]`, cross-referenced to `SRC-*` entries in [`15-SOURCE-AND-EVIDENCE-REGISTER.md`](./15-SOURCE-AND-EVIDENCE-REGISTER.md).

---

## 1. Macro Market Shifts & Consolidation Wave (2025–2026)

Between July 2025 and November 2026, the AI middleware, gateway, observability, and security ecosystem underwent an aggressive **consolidation, repository archival, relicensing, and supply-chain vulnerability wave** (`[VERIFIED_FACT]`). The fragmented point-solution landscape observed during early 2025 discovery no longer exists in standalone form.

### 1.1 Verified M&A, Archivals, Shutdowns & Relicensing Ledger (`2025-07` to `2026-11`)

| Date | Target / Project | Acquirer / Governance Outcome | Licensing & Operational Consequence | Evidence (`SRC-*`) |
|---|---|---|---|---|
| `2025-07-22` / `2026-07-09` | **Protect AI** & `protectai/llm-guard` (`v0.3.15`) | Acquired by **Palo Alto Networks**; **`protectai/llm-guard` archived read-only on `2026-07-09`** | Stranded self-hosted Python DeBERTa/ONNX guardrail deployments; commercial users steered into Prisma AIRS (`[VERIFIED_FACT]`). | `SRC-025`, `SRC-026` |
| `2025-09-29` | **CalypsoAI** | Acquired by **F5** (\$180M stated / \$145.2M cash) | Folded into proprietary **F5 AI Guardrails**, **F5 AI Red Team** (\$100k/12-mo AWS Marketplace baseline), and **F5 AI Gateway** (Aug 2026); zero OSS path (`[VERIFIED_FACT]`). | `SRC-030` |
| `2025-10-22` | **Lakera** (`Lakera Guard v2`) | Acquired by **Check Point** (~\$190M net cash) | Folded into **Check Point AI Defense Plane**; self-serve public pricing removed from `lakera.ai` in favor of enterprise quote-only sales (`[VERIFIED_FACT]`). | `SRC-024` |
| `2025-12-09` | **Model Context Protocol (MCP)** & `agentgateway` / `Agent Router` | Donated to Linux Foundation **Agentic AI Foundation (AAIF)** | Co-governed by Anthropic, OpenAI, Block, Google, Microsoft, AWS, Cloudflare, Bloomberg; MCP evolved to `2026-07-28` stateless HTTP (`[VERIFIED_FACT]`). | `SRC-035`, `SRC-016`, `SRC-019` |
| `2026-01-16` | **Langfuse** (`v3` → `v4`) | Acquired by **ClickHouse, Inc.** | Root `LICENSE` copyright transferred to `ClickHouse, Inc.`; proprietary `ee/` carve-outs expanded to 4 directories; mandates 6 stateful services (`ClickHouse` + `Postgres` + `Redis` + `S3` + `Web` + `Worker`); Nov 2026 added embedded `ai-gateway` (`[VERIFIED_FACT]`). | `SRC-020`, `SRC-021` |
| `2026-03-03` | **Helicone** (`helicone` & `Helicone/ai-gateway`) | Acquired by **Mintlify**; **entered maintenance mode** | `Helicone/ai-gateway` relicensed from `Apache-2.0` to strong copyleft **`GPL-3.0`**; product roadmap frozen with outbound migration offers (`[VERIFIED_FACT]`). | `SRC-017` |
| `2026-03-09` | **Promptfoo** (`v0.126.0`) | Acquired by **OpenAI** | Integrating red-teaming & evals into **OpenAI Frontier** (`[VERIFIED_FACT]`); creates vendor-neutrality concerns for multi-provider enterprise buyers (`[INFERRED_BEHAVIOR]`). | `SRC-023` |
| `2026-05-29` / `2026-07-16` | **Portkey** (`Portkey-AI/gateway`) | Acquired by **Palo Alto Networks**; launched as **Prisma AIRS AI Gateway** GA (`2026-07-16`) | Resolves M0 rumor (`[VERIFIED_FACT]`). MIT `Portkey-AI/gateway` remains a stateless edge worker with zero DB/UI/vault/budgets; full control plane is commercial Prisma AIRS (`[VERIFIED_FACT]`). | `SRC-009`, `SRC-010` |
| `2026-06-12` | **TensorZero** (`2026.6.0`) | Company wound down; **`tensorzero/tensorzero` archived read-only** | Resolves M0 rumor (`[VERIFIED_FACT]`). Mandatory Day-1 ClickHouse dependency and broad gateway+RLHF scope hindered standalone adoption (`[INFERRED_BEHAVIOR]`). | `SRC-018` |
| `2026-08-05` / `2026-10-31` | **Smithery** (`smithery.ai`) | Acquired by **Arcade.dev**; **hosted MCP proxy, API & CLI shut down `2026-10-31`** | Immediately broke ~218 Official MCP Registry `remotes` entries pointing to Smithery URLs (`[VERIFIED_FACT]`). | `SRC-037` |
| `2026-08-25` | **Guardrails AI Hub** | **Private Hub CLI & hosted remote validator inference shut down** | Merged `feat(hub): deprecate hub CLI + private registry; move to public PyPI` (`2026-07-06`, hard shutoff `2026-08-25`) (`[VERIFIED_FACT]`). | `SRC-029` |
| `2026-10-01` | **Arize AI** (`Arize-ai/phoenix` & `Arize AX`) | Acquired by **Dynatrace** (~\$915M) | `Phoenix` server remains source-available under **Elastic License 2.0 (ELv2)** (non-OSI; forbids managed service offerings) (`[VERIFIED_FACT]`). | `SRC-022` |
| `2026-10-06` | **Envoy AI Gateway** | Renamed to **Agent Router** (`theagentrouter/agent-router` `v1.3.0`, Nov 18, 2026) under LF AAIF | `Apache-2.0` Kubernetes Gateway API + Envoy `ext_proc` stack; commercial enterprise UI/governance via Tetrate (`[VERIFIED_FACT]`). | `SRC-016` |

### 1.2 The 2026 AI Gateway Supply-Chain & Sandbox Crisis

Because an enterprise AI gateway concentrates upstream LLM credentials, cloud IAM roles, internal MCP tool tokens, and financial budgets inside a single network choke point, AI gateways and extension registries became high-priority offensive targets throughout 2026 (`[VERIFIED_FACT]`):

1. **LiteLLM Dual Supply-Chain Compromises (`2026-03-24` & `2026-10-21`):**
   - On `2026-03-24`, threat actor TeamPCP compromised LiteLLM CI/CD via Trivy, publishing poisoned PyPI wheels (`1.82.7`, `1.82.8`) carrying `litellm_init.pth` startup hooks (`[VERIFIED_FACT]`, `SRC-006`).
   - On `2026-10-21`, compromised publishing tokens pushed malicious `litellm==1.107.0` wheels and Docker `latest`/`main-stable` images (`BerriAI/litellm#47826`) executing `cook.pth`/`sitecustomize.py` to harvest AWS, GCP, Azure, SSH, Kubernetes, and PostgreSQL credentials (`[VERIFIED_FACT]`, `SRC-007`).
2. **LiteLLM Application RCE & Master-Key Exposure CVEs (`2026`):**
   - **`CVE-2026-40217` (Added to CISA KEV `2026-11-18`, EPSS `0.828`):** Remote code execution via Python bytecode (`co_code`) rewriting on `/guardrails/test_custom_code` (`[VERIFIED_FACT]`, `SRC-008`).
   - **`GHSA-w4c7-34wj-hqm8` (`2026-10-27`, CVSS `10.0`):** Unauthenticated arbitrary file read leaking the Enterprise master key; accompanied by `CVE-2026-42208` (CVSS `9.8` unauthenticated SQLi), `CVE-2026-49468` (CVSS `9.8` Host-header auth bypass), `CVE-2026-114612` (CVSS `9.9` privilege escalation), and `CVE-2026-30623` (MCP `stdio` command injection) (`[VERIFIED_FACT]`, `SRC-008`).
3. **SSRF & Registry Worm Precedents:**
   - **IBM ContextForge `CVE-2026-110194` (`2026-11-13`):** Server-side request forgery across MCP/gateway registration (`[VERIFIED_FACT]`, `SRC-020B`).
   - **Grafana Backend Plugins:** Official Grafana security documentation confirms `hashicorp/go-plugin` OS subprocesses bypass `[security.egress]` SSRF rules (`[VERIFIED_FACT]`, `SRC-042`).
   - **GlassWorm (`2025-10`–`2026-05`) & Nx Console `v18.95` (`2026-05-18`):** Compromised publisher PATs and invisible Unicode payloads infected `35,800+` Open VSX / VS Code installs and exfiltrated developer cloud/LLM keys (`[VERIFIED_FACT]`, `SRC-041`).

---

## 2. Category-by-Category Market Analysis

### 2.1 Category 1 — AI Gateways (Self-Hosted Open-Core, K8s-Native & Managed SaaS)

#### A. LiteLLM (`BerriAI/litellm` `v1.111.0` / `v1.108.19 LTS`)
- **Stack & Licensing (`[VERIFIED_FACT]`, `SRC-005`):** Python FastAPI/Uvicorn wrapping 100+ in-tree provider translators, PostgreSQL via `prisma-client-py` (bundling a Rust/Node Prisma binary inside the Python container), Redis, and selective late-2026 Rust side-components (OCR gateway in `v1.91.0`, experimental spend writer). Root code is `MIT`, while `enterprise/` (`enterprise/LICENSE.md`) requires a paid seat-counted commercial subscription for SSO/SCIM role sync, RBAC, scheduled key rotation, audit logs, and chargeback, and **assigns ownership of customer modifications/patches to `enterprise/` code to BerriAI** (`#34241`).
- **Strengths:** Broadest community mindshare for Python SDK normalization, `7,193`-entry `model_prices_and_context_window.json` catalog (`SRC-045`), MCP OAuth (`v1.91.0`), and A2A gateway routes (`v1.80.8`).
- **Structural Limitations (`[VERIFIED_FACT]`):**
  1. Prisma migration locks (`#23976`), connection pool exhaustion (`#8498`), false-green `/health/key` responses during DB outages, and buffered JSON responses when guardrails/spend logging inspect bodies.
  2. LiteLLM's own production docs warn against `usage-based-routing` due to Redis latency and note that TPM enforcement is best-effort without two-phase reservations.
  3. High weekly regression churn across tightly coupled in-tree Python translators (`1,400+` open issues) plus severe 2026 supply-chain and `co_code` RCE exposure.

#### B. Portkey / Prisma AIRS AI Gateway (`Portkey-AI/gateway` `v1.15.2+`)
- **Stack & Licensing (`[VERIFIED_FACT]`, `SRC-009`, `SRC-010`):** TypeScript (`95.8%`) stateless proxy (`MIT`) acquired by Palo Alto Networks (`2026-05-29`) and integrated into **Prisma AIRS AI Gateway** (`2026-07-16`).
- **Strengths:** Clean declarative `x-portkey-config` fallback/retry JSON, WebSocket Realtime proxying, 40–50+ cloud guardrail integrations, and commercial MCP Gateway (`2026-01`).
- **Structural Limitations:** Self-hosting the MIT repository yields a headless worker with **zero persistence, zero virtual-key vault, zero dollar budgets, zero RBAC/UI, and zero MCP registry**; full functionality requires Palo Alto Networks commercial procurement (`[VERIFIED_FACT]`).

#### C. Maxim Bifrost (`maximhq/bifrost` `v2.2.6+` / Core `v1.16.0`)
- **Stack & Licensing (`[VERIFIED_FACT]`, `SRC-011`, `SRC-012`):** Go (`fasthttp`) + SQLite/PostgreSQL + Next.js UI (`Apache-2.0` core), with RBAC, SSO, HashiCorp/Azure Vault, clustering, adaptive load balancing, and enterprise guardrails gated in `bifrost-enterprise`.
- **Strengths & Claims:** Fast Go execution (`11 µs` mean internal overhead at `5,000 RPS` `[VENDOR_CLAIM]`), drop-in `/openai`, `/anthropic`, `/genai`, `/v1/mcp`, and Claude Code device-login binding.
- **Verified Caveats (`[VERIFIED_FACT]`):**
  1. *Benchmark Conditions:* `maximhq/bifrost-benchmarking` measures `11 µs` against a `0 ms` local `fasthttp` mock (`~200B` non-streaming payload) with DB ledger, guardrails, and semantic cache disabled; realistic runs in its own README record `45.2 ms` mean / `156.7 ms` p99 at `498.5 RPS`.
  2. *Plugin Fragility & Sandbox Gaps:* Custom Go `.so` plugins require exact compiler lockstep (*"Custom Go plugins must be rebuilt with Go 1.27.4 to load"*), lack memory sandboxing, and Wasm hooks lack a blind host credential broker; MCP "Code Mode" executes LLM-generated JS/Starlark orchestration scripts; `bifrostMcpAllowWrites` defaults to `true` in Helm; and community issues document ToS-violating GitHub Copilot / ChatGPT subscription pooling (strictly banned in Tanpopo under `DEC-004`).

#### D. OpenRouter (Managed Cloud Aggregator)
- **Model & Pricing (`[VERIFIED_FACT]`, `SRC-013`, `SRC-047`):** `100%` closed SaaS routing across `743` catalog models (`476` active `/api/v1/models` entries audited live). Charges `5.5%` Standard / `8.0%` Business credit top-up fees; in **August 2026**, replaced its 1M free BYOK request tier with **\$25,000/month of list-price inference free**, then **`5%` of equivalent OpenRouter list price** (and workspace budgets exclude BYOK spend unless `include_byok_in_budgets` is toggled).
- **Strengths & Blockers:** Best-in-class multi-signal provider scoring (`:nitro`, `:floor`, `:exacto`, `zdr: true`) and string-decimal `/api/v1/models` pricing schema (`pricing.overrides`), but **0% self-hostable**, cannot run air-gapped, and cannot govern private VPC vLLM or internal MCP servers.

#### E. Kong AI Gateway (`3.16` / Konnect `2.3`) & Cloudflare AI Gateway
- **Kong (`[VERIFIED_FACT]`, `SRC-014`):** OpenResty/LuaJIT (`Apache-2.0` basic `ai-proxy` only). Multi-provider load balancing (`ai-proxy-advanced`), token quotas, semantic cache, PII sanitization, MCP `2026-07-28` proxy/OAuth2, A2A entities, and Cedar policies (`2.3`) are **strictly Enterprise/Konnect**, billed across 4 simultaneous meters including **\$100/month per AI model** above 5 models.
- **Cloudflare AI Gateway (`[VERIFIED_FACT]`, `SRC-015`):** Proprietary edge SaaS with Dynamic Routing DAGs and Spend Limits Beta, but non-self-hostable, charges `5%` Unified Billing load fees, bills Workers Logs (since Sept 24, 2026) and Workers AI guardrail tokens separately, and requires paid Zero Trust plans for custom DLP.

#### F. Kubernetes & Rust Gateways (`Agent Router v1.3.0`, `Solo agentgateway v1.7.0`, `Plano`, `Higress`, `IBM ContextForge`)
- **Agent Router (`theagentrouter/agent-router` `v1.3.0`, ex-Envoy AI Gateway, `SRC-016`):** `Apache-2.0` Envoy + Go `ext_proc` + Kubernetes Gateway API CRDs (`GAIE v1.6`). Strong K8s-native Bedrock SigV4 and `QuotaPolicy`, but **requires Kubernetes + Envoy xDS** (unusable for single-VM/Compose teams), lacks an OSS Admin Console or two-phase FinOps ledger, and suffered real xDS secret-caching and `caBundle` upgrade regressions in `v1.1`–`v1.3` (`[VERIFIED_FACT]`).
- **Solo `agentgateway` (`v1.7.0`, `SRC-019`):** High-performance `Apache-2.0` Rust proxy supporting LLM (`/v1/chat/completions`, `/v1/responses`, `/v1/messages`), stateful + stateless MCP, A2A, and CEL policies. However, adding new providers requires upstream Rust code compilation (no `.tpkg` Wasm plugin ecosystem), OSS lacks a multi-tenant SQLite/PG two-phase FinOps ledger and response cache, and fleet RBAC/SSO live in commercial **Solo Enterprise** (`[VERIFIED_FACT]`).
- **Plano (`katanemo/plano`, ex-ArchGW), Higress & One-API:** Plano requires Envoy + GPU/SLM `Plano-Orchestrator` models under a Llama-3.2-derived license; Higress requires Istio/Envoy K8s; One-API (`MIT`) proves single-binary Go+React UX appeal for hobbyists/resellers while lacking enterprise security, Wasm isolation, SSRF guards, MCP, and A2A (`[VERIFIED_FACT]`).

---

### 2.2 Category 2 — AI Observability, Prompt Management & Evaluation Platforms

| Platform | 2026 Status & License (`[VERIFIED_FACT]`) | Self-Hosted Architecture | Key Strengths | Structural Friction for Gateway Buyers |
|---|---|---|---|---|
| **Langfuse (`v4`)** (`SRC-020`) | Owned by **ClickHouse, Inc.** (`MIT` + 4 proprietary `ee/` dirs) | **6 mandatory services:** Next.js, Worker, Postgres, **ClickHouse**, Redis, S3/MinIO | Rich trace trees, Prompt Management, datasets, LLM-as-a-judge, OTLP HTTP | Heavy 6-service Day-1 footprint; SSO/SCIM/RBAC/audit/retention locked in `ee/` (\$2,499/mo Cloud Enterprise); no OTLP gRPC. |
| **Arize Phoenix** (`SRC-022`) | Owned by **Dynatrace** (**`Elastic License 2.0`** server; `Apache-2.0` SDK) | Single Python container (SQLite / Postgres), OTLP HTTP + gRPC | Zero-gate self-hosted trace visualization, datasets, evals, OpenInference | **Non-OSI license (`ELv2`)**; strictly out-of-band collector (not an inline gateway, vault, or FinOps enforcer). |
| **Braintrust** (`SRC-023B`) | Independent (\$800M Series B, Feb 2026); proprietary `Brainstore` | SaaS Control Plane + optional hybrid customer-VPC `Brainstore` | Fast columnar trace search, CI eval gates (`Scorers`), Loop assistant | Control plane/UI cannot run fully offline/air-gapped; Pro \$249/mo + data/score overages. |
| **LangSmith** (`SRC-023C`) | LangChain, Inc. (`100%` closed-source platform) | SaaS or Enterprise-licensed K8s self-host | Tight LangGraph step debugging & dataset annotation | Closed source; per-seat + per-trace pricing; self-hosting locked behind custom Enterprise contracts. |
| **Promptfoo (`v0.126.0`)** (`SRC-023`) | Acquired by **OpenAI** (`2026-03-09`); `MIT` CLI | Node.js CLI / CI runner + local SQLite/Web viewer | Declarative YAML eval matrices & OWASP LLM red-teaming | Offline CLI/CI tool rather than a runtime gateway; OpenAI ownership complicates neutral vendor governance. |

---

### 2.3 Category 3 — AI Security & Guardrail Platforms

- **Market Shakeout (`[VERIFIED_FACT]`):** Standalone guardrail startups largely consolidated into legacy network security vendors (**Protect AI + Portkey → Palo Alto Prisma AIRS**; **CalypsoAI → F5**; **Lakera → Check Point**), while **`protectai/llm-guard` was archived (`2026-07-09`)** and **Guardrails AI shut down its private Hub (`2026-08-25`)**.
- **NVIDIA NeMo Guardrails (`v0.24.1`, `Apache-2.0`, `SRC-027`):** Introduced `IORails` **speculative parallel input-rail execution** (running input rails concurrently with upstream generation to mask latency) and decoupled from mandatory LangChain, while requiring **NVIDIA AI Enterprise** licenses for production Safety NIMs and enabling default-on usage telemetry unless `NEMO_GUARDRAILS_NO_USAGE_STATS=1` is set (`[VERIFIED_FACT]`).
- **Meta LlamaFirewall & Llama Guard 4 (`SRC-028`):** While orchestration code is `MIT`, underlying **Llama Guard 4 (`12B`)** weights use the **Llama 4 Community License**, which **explicitly prohibits multimodal Llama 4 usage by companies or individuals domiciled in the European Union** (`[VERIFIED_FACT]`) — a severe compliance trap for global enterprises.

---

### 2.4 Category 4 — Model-Serving & Distributed Inference Infrastructure

- **`vLLM v0.34.0` & CNCF `llm-d 1.0` (`Apache-2.0`, `SRC-031`, `SRC-032`):** `vLLM v0.34.0` (Nov 2026) standardized on `Model Runner V2` and Extensible KV Cache while patching P2P ZMQ/CORS/shm CVEs. **`llm-d 1.0`** (Oct 2026) pairs Kubernetes **Gateway API Inference Extension (`GAIE v1.6`)** Endpoint Picker (`EPP`) scheduling with **KV-cache-aware global prefix routing** and **NIXL / RDMA prefill-decode disaggregation** (`[VERIFIED_FACT]`).
- **`SGLang v0.5.22+` (`sgl-router`), `NVIDIA Dynamo` & `Ollama v0.40.16` (`SRC-033`, `SRC-034`, `SRC-044`):**
  - `SGLang` delivers RadixAttention prefix trees and 1P1D NIXL disaggregation, though partial RDMA/NIXL faults can corrupt KV state while `/health` still returns `200 OK` (`sglang#51980`, `2026-11-21`, `[VERIFIED_FACT]`).
  - `NVIDIA Dynamo` (`Apache-2.0`) open-sources SLA/KV routing, whereas **NVIDIA NIM** containers require paid per-GPU **NVIDIA AI Enterprise** entitlements (`[VERIFIED_FACT]`).
  - `Ollama v0.40.16` (`MIT`) serves `/api/*`, `/v1/*`, and `/v1/messages` with zero built-in auth/RBAC/budgets, and introduced **`*:cloud` remote proxying to `ollama.com`** unless `OLLAMA_NO_CLOUD=true` is enforced (`[VERIFIED_FACT]`).
- **Architectural Boundary (`[INFERRED_BEHAVIOR]`):** `llm-d`, `vLLM`, `SGLang`, and `Dynamo` optimize **intra-cluster L7-to-GPU scheduling**. Enterprises still require an upstream governance gateway for multi-tenant virtual keys/WIF, cross-cloud failover, exact FinOps ledgers, compliance fences, and MCP/A2A governance.

---

### 2.5 Category 5 — MCP & Agent Infrastructure Platforms

- **Official MCP Registry (`v0.1` Preview) & `2026-07-28` Stateless Spec (`SRC-035`, `SRC-036`):** Read-only metaregistry (`MIT`) with zero code sandboxing or credential brokering; ~218 entries broke when Smithery shut down (`2026-10-31`). Meanwhile, MCP `2026-07-28` eliminated stateful `initialize`/`Mcp-Session-Id` on HTTP, creating a dual-protocol enterprise reality (`2025-03-26`..`2025-11-25` stateful vs `2026-07-28` stateless) (`[VERIFIED_FACT]`).
- **Arcade.dev (`SRC-039`) & Composio (`SRC-038`):** Proved two critical enterprise agent needs: (1) **per-user On-Behalf-Of (`OBO`) tool OAuth delegation** (preventing shared-service-account confused-deputy escalation) and (2) **progressive runtime tool discovery** (avoiding `50,000+`-token schema bloat). However, Arcade locks its engine/vault behind commercial tiers and charges **`\$0.50` per Pro tool call** above 100/month (`\$4,975/mo` at 10k calls!) (`[VERIFIED_FACT]`).
- **Docker MCP Gateway (`SRC-036B`), AWS Bedrock AgentCore & Google Vertex Agent Engine (`SRC-040`):** Docker's `docker mcp gateway` (`MIT`, Beta) lacks inbound multi-tenant RBAC/LLM FinOps and requires privileged Docker access; AWS Bedrock AgentCore enforces Cedar tool policies and `RFC 8693` OBO but locks into AWS IAM across **12+ micro-metered billing dimensions** (`[VERIFIED_FACT]`).

---

### 2.6 Category 6 — Enterprise API Management & Extension Marketplaces

- **Traditional APIM (`Apigee Hybrid 1.17.4`, `Gravitee 4.14 Gamma`, `Tyk 5.15`, `Traefik Hub 3.21`):** All four bolted on MCP (`2026-07-28` in Apigee/Traefik Hub; HITL + `RFC 8693` in Gravitee `Gamma`), **locking 100% of AI/MCP governance behind proprietary Enterprise licenses** while imposing heavy operational footprints (Apigee GCP control-plane tether; Gravitee JVM + MongoDB + Elasticsearch + Hazelcast; Traefik Hub mandatory K8s CRDs) (`[VERIFIED_FACT]`, `SRC-040B`..`040E`).
- **Extension Marketplace Precedents (`VS Code / Open VSX`, `Grafana`, `Terraform Registry`, `Backstage / RHDH`):**
  - *VS Code / Open VSX* proved unsandboxed plugins + static publishing PATs + unpinned auto-updates invite supply-chain worms (`GlassWorm`, `Nx Console`) (`[VERIFIED_FACT]`).
  - *Grafana* proved signed manifest tiers (`Official/Commercial/Community/Private`) are defeated when backend `go-plugin` subprocesses bypass host SSRF rules (`[VERIFIED_FACT]`).
  - *Terraform* proved cryptographic lockfiles (`.terraform.lock.hcl`) + static air-gap network mirrors work at enterprise scale (`[VERIFIED_FACT]`).
  - *Backstage / RHDH* proved runtime JS Module Federation (`Scalprum`) is brittle across React upgrades compared to declarative schemas + OCI artifact distribution (`[VERIFIED_FACT]`).

---

## 3. Eight Evidence-Backed Operator Pain Points & Tanpopo Strategic Positioning

1. **Gateway as Single-Point-of-Compromise:** Python/Node/JVM gateways hold crown-jewel API keys in the same process memory as unsandboxed dependencies and custom scripts (`LiteLLM` `.pth` & `CVE-2026-40217`). **Tanpopo Advantage:** Pure-Go static binary + zero-ambient-authority `wazero` sandbox + `BlindHostCredentialBroker` + connect-time `SSRFGuardDialer` (`[VERIFIED_FACT]`).
2. **M&A Rug-Pulls & License Bait-and-Switch:** Ten major tools were acquired, relicensed (`GPL-3.0`, `ELv2`), or archived within 16 months. **Tanpopo Advantage:** Sovereign `100% Apache-2.0` Core (`DEC-001`) with zero copyleft dependencies and full air-gap `.tpbundle`/OCI self-sufficiency (`[VERIFIED_FACT]`).
3. **The "SSO, RBAC & Budget Tax":** Open-core competitors paywall OIDC/SAML, RBAC, audit logs, and budgets (`LiteLLM`, `Bifrost`, `Kong`, `Traefik Hub`, `Langfuse`). **Tanpopo Advantage:** Security and financial safety are first-class Core primitives, not artificial single-node paywalls (`[VERIFIED_FACT]`).
4. **False Binary Between Toy Proxy and 6-Service Cluster:** Operators hate deploying ClickHouse + MinIO + K8s CRDs on Day 1 (`TensorZero`, `Langfuse`, `Agent Router`), yet outgrow single-connection SQLite bottlenecks under load. **Tanpopo Opportunity:** Progressive scaling from 60-second single-binary SQLite Mode A to `20,000+ RPS` (`HYPOTHESIS / UNVERIFIED`) PG16 + Valkey Mode C with optional OTLP/ClickHouse offload (`[HYPOTHESIS]`).
5. **Broken Cross-Provider Agentic Fidelity:** Naive `/v1/chat/completions` translation breaks GPT-5.4+/6.2 reasoning+tools, strips Gemini 3/4 `thoughtSignature` (`HTTP 400`), and invalidates Anthropic 5m/1h prompt caches (`[VERIFIED_FACT]`). **Tanpopo Opportunity:** True multi-dialect Universal Gateway (`/v1/responses`, `/v1/messages`, `/v1beta/models/*`, `/native/*`) with strict 4-tier lossless/lossy contract enforcement (`[HYPOTHESIS]`).
6. **Confused-Deputy MCP Auth, Context Bloat & `2026-07-28` Split:** Shared MCP service keys violate least privilege while commercial OBO brokers charge `\$0.50/call` (`Arcade.dev`). **Tanpopo Opportunity:** Dual-era MCP (`2025-03-26`..`2026-07-28`) + `BlindHostCredentialBroker` `RFC 8693` OBO delegation + progressive tool search at zero per-call tax (`[HYPOTHESIS]`).
7. **Multi-Dimensional Pricing Drift & Fractional Sub-Nano Rates:** Flat token price JSONs break on `1h` cache writes, `>200k`/`>272k` context cliffs, search fees, and `1,664` fractional `nano-USD/token` rates (`[VERIFIED_FACT]`). **Tanpopo Opportunity:** 4-layer Model Intelligence (`Manual Override > Account Discovery > Upstream Sync > Signed Offline Pack`) standardized on exact integer `nano_usd_per_1m_tokens` (`[HYPOTHESIS]`).
8. **Day-0 Setup & Day-2 Change Anxiety:** Operators fear breaking production routes/policies or fighting undocumented env vars. **Tanpopo Opportunity:** Zero-to-first-token `< 90s` (`HYPOTHESIS / UNVERIFIED`) guided setup + Metadata-Only Historical Traffic Replay ("What-If Engine") + 8-Layer Blast-Radius Topology (`[HYPOTHESIS]`).
