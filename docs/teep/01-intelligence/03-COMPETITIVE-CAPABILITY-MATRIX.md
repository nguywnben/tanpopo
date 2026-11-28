# 03 — Competitive Capability & Architecture Matrix

- **Document ID:** `TEEP-01-DOC-03`
- **Research Execution Date:** `2026-11-27`
- **Machine-Readable Companion:** [`data/competitive-capability-matrix.csv`](./data/competitive-capability-matrix.csv)
- **Epistemic Convention (`COND-PO-01`):** Capabilities reflect verified primary documentation, repositories, and source inspection as of `2026-11-27`. Legend: `✅` First-class OSS/Core · `🔒` Proprietary / Enterprise-Paywalled · `⚠️` Partial / Caveated · `❌` Unsupported / Absent · `🪦` Archived / Sunset.

---

## 1. Primary AI Gateway & Agent Infrastructure Comparison Matrix

| Dimension / Capability | **Tanpopo RC9 (`ac9ae87a`)** | **LiteLLM (`v1.111.0`)** | **Portkey / Prisma AIRS** | **Maxim Bifrost (`v2.2.6+`)** | **OpenRouter (SaaS)** | **Kong AI GW (`3.16`/`2.3`)** | **Cloudflare AI GW** | **Agent Router (`v1.3.0`)** | **Solo `agentgateway` (`v1.7`)** | **Gravitee `4.14` (`Gamma`)** |
|---|---|---|---|---|---|---|---|---|---|---|
| **2026 Corporate / Governance Status** | Independent OSS (`Apache-2.0`) | BerriAI (VC Open-Core) | **Palo Alto Networks** (`2026-05`) | Maxim AI (VC Open-Core) | OpenRouter Inc. (SaaS) | Kong Inc. (Open-Core) | Cloudflare Inc. (SaaS) | **LF AAIF** (`Apache-2.0`) | **LF / CNCF** (`Apache-2.0`) | GraviteeSource (Open-Core) |
| **Core Runtime & Memory Safety** | Pure Go (`CGO_ENABLED=0`) | Python FastAPI + Prisma | TS / Edge Worker | Go (`fasthttp`) + Next.js | Proprietary Edge | C / OpenResty + LuaJIT | Proprietary Edge | Envoy C++ + Go `ext_proc` | Rust (`tokio` / `hyper`) | Java (`JVM`) |
| **Permissive OSS Core License** | `Apache-2.0` (`100%`) | `MIT` + `enterprise/` prop. | `MIT` worker; **UI/CP closed** | `Apache-2.0` + EE closed | ❌ Closed SaaS | `Apache-2.0` basic only | ❌ Closed SaaS | `Apache-2.0` | `Apache-2.0` | `Apache-2.0` + EE closed |
| **Zero-Dependency Single-Binary Day-1** | ✅ (`SQLite WAL` + embedded UI) | ❌ (Python + Postgres + Prisma) | ⚠️ (Stateless CLI, no DB/UI) | ✅ (`npx`/Go bin + SQLite) | ❌ (Cloud only) | ⚠️ (DB-less YAML, no UI) | ❌ (Cloud only) | ❌ (Requires K8s + Envoy) | ⚠️ (Rust bin, YAML-only OSS) | ❌ (JVM + Mongo + ES) |
| **Air-Gapped / Offline Operation** | ✅ (`.tpbundle` + `0` CDN reqs) | ⚠️ (Fragile Python wheel/DB) | ❌ (Control plane cloud/hybrid) | 🔒 (Enterprise VPC/Vault) | ❌ | 🔒 (Kong Enterprise) | ❌ | ✅ (K8s OCI images) | ✅ (Standalone/K8s) | 🔒 (Enterprise) |
| **Unified OpenAI `/v1/chat` & `/v1/embeddings`** | ✅ (⚠️ Gemini embed gap) | ✅ | ✅ | ✅ | ✅ | ✅ (`ai-proxy`) | ✅ | ✅ | ✅ | 🔒 |
| **Full Stateful/Agentic `/v1/responses`** | ⚠️ (Stateless OpenAI-only) | ⚠️ (Frequent state bugs) | ✅ | ✅ | ✅ | 🔒 | ⚠️ | ⚠️ | ✅ | ❌ |
| **Native Top-Level `/v1/messages` & `/v1beta/*`** | ⚠️ (`/native/{dialect}/*` prefix) | ✅ (`/anthropic/*`, `/vertex_ai/*`) | ✅ | ✅ (`/anthropic`, `/genai`) | ✅ (`/api/v1/messages`) | 🔒 (`ai-proxy-advanced`) | ✅ (Provider URLs) | ✅ (`/anthropic/v1/messages`) | ✅ (`/v1/messages`) | ❌ |
| **Realtime WebSocket / WebRTC (`/v1/realtime`)** | ❌ (`400` rejected in RC9) | ✅ | ✅ | ⚠️ | ❌ | 🔒 | ✅ (WebSockets) | ⚠️ | ⚠️ | ❌ |
| **Async Batch & Files (`/v1/batches`, `/v1/files`)** | ❌ (`MISSING` in RC9) | ✅ | ✅ | ⚠️ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Account-Authenticated Model Discovery** | ⚠️ (Probe `detected_model_ids` unused) | ⚠️ (`/models` passthrough) | 🔒 | ⚠️ (`modelcatalogresolver`) | N/A (Hosted union) | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Curated Multi-Tier Pricing Catalog** | ❌ (Manual 8-field nano-USD) | ✅ (`7,193` JSON; float drift) | 🔒 | ✅ (JSON catalog) | ✅ (`476` string-decimal + overrides) | ❌ | ⚠️ | ❌ | ❌ | ❌ |
| **Exact Integer Two-Phase Budget Pre-Holds** | ✅ (`u128` nano/micro-USD + Lua) | ❌ (Async float post-spend) | 🔒 (Cloud soft/hard caps) | 🔒 / ⚠️ (Float DB tracking) | ⚠️ (Excludes BYOK by default!) | 🔒 (`ai-rate-limiting-adv`) | ⚠️ (Spend Limits Beta) | ⚠️ (`QuotaPolicy` tokens) | ❌ (No FinOps ledger) | 🔒 |
| **Prefix / KV-Cache Affinity Routing** | ❌ (`MISSING` in RC9) | ⚠️ (Redis latency warning) | ⚠️ (Sticky header) | 🔒 (Adaptive LB) | ✅ (Warm-cache sticky) | 🔒 | ❌ | ✅ (Via `GAIE` / `llm-d`) | ✅ (`GAIE` inference pool) | ❌ |
| **Exact & Semantic Response Caching** | ⚠️ (SQL Exact JCS only; 0 semantic) | ✅ (Redis / Qdrant semantic) | 🔒 (Simple & Semantic) | ✅ (Weaviate / Redis semantic) | ⚠️ (Vendor prompt cache) | 🔒 (`ai-semantic-cache`) | ✅ (Edge cache) | ❌ | ❌ | 🔒 |
| **Inline PII / Prompt-Injection Guardrails** | ⚠️ (Contract-only in RC9!) | ✅ (⚠️ `CVE-2026-40217` RCE) | 🔒 (`50+` cloud rails / AIRS) | 🔒 (Enterprise guardrails) | ⚠️ (`is_moderated`) | 🔒 (`ai-sanitizer` / guard) | ⚠️ (Paid Workers AI / DLP) | ❌ (External `ext_proc`) | ⚠️ (`ToolOutput` CEL/regex) | 🔒 |
| **Sandboxed Third-Party Plugin Runtime** | ✅ (Tier-2 `wazero` `16MiB`/`50ms`) | ❌ (In-process Python) | ❌ (Cloud webhooks) | ⚠️ (Fragile `.so` / non-blind Wasm) | ❌ | ⚠️ (Unsandboxed Lua/Go) | ❌ | ⚠️ (K8s gRPC sidecars) | ❌ (Compile-in Rust) | ⚠️ (Unsandboxed JVM `.zip`) |
| **Blind Host Credential Broker + SSRF Fence** | ✅ (`0` raw secrets in Wasm; IP pin) | ❌ (`2x` `.pth` key thefts) | 🔒 (SaaS vault) | ❌ (Plugins see env/context) | N/A (SaaS) | ❌ | N/A (SaaS) | ⚠️ (K8s `BackendSecurityPolicy`) | ⚠️ (Host backend auth) | ❌ |
| **Signed Extension Lockfile & Revocation Feed** | ✅ (`tanpopo.lock` + Ed25519 feed) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **MCP `2025-03-26` Stateful + `2026-07-28` Stateless** | ⚠️ (`2025-03-26` only; 4 RPCs/call) | ⚠️ (MCP proxy + `stdio` CVE) | 🔒 (Commercial MCP GW) | ⚠️ (`Code Mode` JS/Starlark) | ❌ | 🔒 (`ai-mcp-proxy` `2.3`) | ❌ | ✅ (MCP routing) | ✅ (Both stateful & stateless) | 🔒 (`Gamma`) |
| **MCP Tool JCS Schema Pinning & HITL Holds** | ✅ (`SM-06` 5-tuple JCS token) | ⚠️ (Basic tool ACLs) | 🔒 (Tool provisioning) | ⚠️ (Allowlist; `AllowWrites=true`) | ❌ | 🔒 (ACL only) | ❌ | ❌ | ⚠️ (CEL tool filter) | 🔒 (HITL hold/approve) |
| **Per-User `RFC 8693` OBO Tool Token Exchange** | ⚠️ (Provider OAuth only in RC9) | ⚠️ (`v1.91` MCP OAuth) | 🔒 (`user_identity_forwarding`) | ❌ | ❌ | 🔒 (`ai-mcp-oauth2`) | ❌ | ❌ | ⚠️ (External authz) | 🔒 (`Gamma` `RFC 8693`) |
| **A2A `v1.0.1` Standard Wire Interop** | ⚠️ (Custom REST `/a2a/tasks` in RC9) | ⚠️ (`/a2a` proxy) | ❌ | ❌ | ❌ | 🔒 (`AI Agent` `2.0`) | ❌ | ⚠️ (JSON-RPC preview) | ✅ (Native A2A) | 🔒 (`Gamma` A2A) |
| **Enterprise SSO (OIDC / SAML) & SCIM 2.0** | ⚠️ (OIDC un-wired in `serve.go`; 0 SCIM/SAML) | 🔒 (`enterprise/LICENSE.md`) | 🔒 | 🔒 | 🔒 (Enterprise) | 🔒 | 🔒 (Zero Trust) | 🔒 (Tetrate Enterprise) | 🔒 (Solo Enterprise) | 🔒 |
| **Secretless Workload Identity (`SPIFFE`/K8s OIDC)** | ⚠️ (Library code unwired in RC9) | ⚠️ (Cloud IAM env) | 🔒 | 🔒 | ❌ | 🔒 | ❌ | ✅ (K8s SA / AWS IRSA) | ✅ (K8s / Istio mTLS) | 🔒 (`Gamma` SPIFFE) |
| **PostgreSQL `FORCE RLS` + Tamper-Evident JCS Audit** | ✅ (Per-org `FORCE RLS` + SHA-256 JCS) | ❌ (Shared table; EE audit) | 🔒 | 🔒 | ❌ | 🔒 | ❌ | ❌ | ❌ | 🔒 |
| **Built-In Localization & WCAG 2.2 AA Console** | ✅ (`15` offline locales; `0` axe) | ❌ (English Next.js UI) | 🔒 (English SaaS UI) | ⚠️ (English Next.js UI) | ⚠️ (English SaaS) | 🔒 (Konnect UI) | ⚠️ (Cloudflare Dash) | ❌ (No OSS UI) | ⚠️ (Dev YAML UI) | 🔒 (English UI) |

---

## 2. Adjacent Category Comparison (Observability, Guardrails, Inference & Marketplaces)

| Adjacency Category | Representative Platforms Audited | What They Do Better Than Gateways Today | Why Enterprises Still Need Tanpopo (Integration vs Replacement Boundary) |
|---|---|---|---|
| **Observability & Eval Suites** | **Langfuse v4** (ClickHouse), **Arize Phoenix** (Dynatrace), **Braintrust**, **LangSmith**, **Promptfoo** (OpenAI) | Deep multi-turn prompt/completion trace diffing, human annotation queues, dataset versioning, CI prompt regression scoring (`LLM-as-a-judge`). | They are **out-of-band OLAP collectors** (requiring ClickHouse/Brainstore and raw prompt storage), not low-latency inline enforcement planes. **Strategy:** Emit first-class OTel `gen_ai.*` spans/events to Phoenix/Langfuse/Tempo while providing lightweight built-in Gateway Prompt Templates & Regression Cassettes (`DOM-14`, `DOM-15`) for teams that refuse a 6-service OLAP stack. |
| **Dedicated Guardrail Engines** | **NeMo Guardrails `v0.24.1`**, **Lakera Guard v2** (Check Point), **F5 / CalypsoAI**, **LlamaFirewall**, **`llm-guard`** (🪦 Archived) | Pre-trained neural classifiers (`DeBERTa`, `PromptGuard 2`, `Nemotron-Safety-8B`) for nuanced indirect prompt injection, toxicity, and contextual grounding. | Running `8B–12B` safety models inside the gateway binary violates `CGO_ENABLED=0` and memory bounds, while `llm-guard`'s archival stranded Python users. **Strategy:** Ship fast deterministic Tier-1/Tier-2 Wasm guardrails (PII/secret/regex/Unicode/entropy) in Core + speculative parallel Tier-3 sidecar/HTTP connectors (`GUARDRAIL_PROCESSOR`) to NeMo, Lakera, Bedrock `ApplyGuardrail`, Azure Content Safety, or local ONNX (`DOM-09`). |
| **GPU Inference Routers** | **CNCF `llm-d 1.0` (`GAIE v1.6`)**, **`vLLM v0.34.0`**, **`SGLang` (`sgl-router`)**, **NVIDIA Dynamo** | Sub-millisecond GPU KV-block hash tracking, RadixAttention prefix trees, NIXL/RDMA prefill-decode disaggregation across GPU pods. | They operate **per model pool inside Kubernetes** with zero multi-tenant auth, zero SaaS cloud burst failover, zero micro-USD chargeback, and silent NIXL stall modes (`sglang#51980`). **Strategy:** Integrate cleanly **upstream** of `llm-d`/`GAIE` and `sgl-router` via prefix-affinity headers (`PREFIX_CACHE_AFFINITY`) and semantic generation probes (`DOM-04`, `DOM-07`). |
| **MCP Tool Auth Brokers** | **Arcade.dev**, **Composio**, **Docker MCP Toolkit**, **AWS Bedrock AgentCore** | Turnkey SaaS OAuth app catalogs (500+ connectors in Composio/Arcade) and interactive step-up user consent flows. | SaaS brokers see every enterprise OAuth token and charge punitive per-call fees (`\$0.50/call` on Arcade Pro). **Strategy:** Deliver self-hosted, zero-per-call-fee `RFC 8693` / `RFC 9728` / `RFC 8707` per-user OBO token brokering inside `BlindHostCredentialBroker` + OpenAPI-to-MCP `TOOL_CONNECTOR` extensions (`DOM-12`). |

---

## 3. Honest Assessment: Where Competitors Outperform Tanpopo RC9 Today (`[VERIFIED_FACT]`)

To ensure `TEEP-02` prioritizes ruthlessly without blind spots, Tanpopo RC9 (`ac9ae87a`) currently **trails** leading competitors in nine concrete areas:

1. **Raw Sustained Throughput (`109.48 req/s` SQLite / `56.86 req/s` 2-vCPU PG16 HA vs `2,200–5,000+ RPS` in Bifrost / Envoy / `agentgateway`):** Caused by per-request `DisableKeepAlives: true` in `blind_broker.go:885` and synchronous per-request SQL `Reserve`/`Settle` transactions (`sqlite/store.go:104`).
2. **Day-0 Time-to-First-Token (`15–25 min` across `7` screens + `403 org_trust_policy_denied` `curl` wall on unsigned builds vs `< 2 min` in Bifrost / LiteLLM / Ollama):** Caused by empty `/setup` adapter list, unexposed `org_extension_policies` UI, manual 7-entity registration, and manual `$/1M` → `nano-USD/token` mental math.
3. **Protocol Surface Completeness:** Missing stateful/multi-provider `/v1/responses`, top-level `/v1/messages` & `/v1beta/models/*`, `/v1/batches`, `/v1/files`, `/v1/audio/*`, `/v1/images/*`, WebSocket `/v1/realtime`, **MCP `2026-07-28` stateless HTTP**, and **Linux Foundation A2A `v1.0.1`** wire compatibility.
4. **Live Guardrail & Non-Provider Extension Execution:** While `GuardrailHooks`, `AUTH_DRIVER`, and `TELEMETRY_EXPORTER` exist in schemas, **zero non-provider extensions execute on the RC9 Data Plane hot path**, and zero built-in PII/secret/injection guardrails ship out of the box.
5. **Model Intelligence & Conditional Pricing:** Zero bundled model/pricing catalog, zero auto-import from `detected_model_ids`, missing `1h` cache write / `>200k` context tiers / batch / search fees, and `nano_usd_per_token` rounding drift on `1,664` real LiteLLM catalog rates.
6. **Semantic Caching & Prefix-Cache Affinity Routing:** RC9 has SQL-only exact JCS caching (`stream=false`, `temp=0`) and naive SWRR/EWMA routing that scatters multi-turn prefixes across upstream caches.
7. **Enterprise Identity & Headless Automation:** `OIDC` and `TokenVerifier` are `nil` in `cmd/tanpopo/serve.go:1008` (breaking all remote CLI commands), Admin sessions live only in single-node Go RAM, and SAML 2.0, SCIM 2.0, MFA/Passkeys, and Terraform/DB GitOps are absent.
8. **Multi-Tenant Control-Plane Scalability (`ARCH-OBS-01`):** `ConfigSnapshot.Refresh` (`2 + 7 * N_orgs` queries) and `18` total admin, auth, snapshot, reaper, outbox, and audit paths execute sequential `O(N_orgs)` RLS loops on PostgreSQL.
9. **Console Lifecycle Completeness:** `17` OpenAPI Admin operations lack UI (plus `4` OpenAPI routes returning `404` in `server.go`), and Logical Models, Deployment rate cards, MCP Servers, A2A Agents, Policies, and Budgets suffer from "create-only" lifecycle traps.

---

## 4. Strategic Positioning Across Audited 2026 Platforms

Among the `19` primary gateway/APIM platforms and `39` adjacent observability, guardrail, inference, and MCP tools audited on `2026-11-27`, none documented all five of the following capabilities together in a single self-hosted open-source distribution: **(1) single-binary `CGO_ENABLED=0` Day-1 deployment**, **(2) permissive `Apache-2.0` enterprise governance & air-gap operation**, **(3) zero-ambient-authority `wazero` + `BlindHostCredentialBroker` plugin isolation across multi-category extensions**, **(4) 128-bit integer two-phase FinOps holds with signed chargeback receipts**, and **(5) unified 4-plane (`LLM + Plugin + MCP + A2A`) causal governance**. Closing the nine RC9 execution gaps above while introducing evidence-backed product innovations ([`11-PRODUCT-INNOVATION-OPPORTUNITIES.md`](./11-PRODUCT-INNOVATION-OPPORTUNITIES.md)) gives Tanpopo a coherent, defensible enterprise platform thesis.
