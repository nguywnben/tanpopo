# 13 — Preliminary Enterprise Capability Inventory (`CAP-001` .. `CAP-161`)

- **Document ID:** `TEEP-01-DOC-13`
- **Research Execution Date:** `2026-11-27`
- **Governance Status:** **Preliminary Research Candidates ONLY** — inputs to `TEEP-02A` (Master Feature Catalog). Scope is **not** locked in `TEEP-01`.
- **Machine-Readable Companions:** [`data/preliminary-capability-inventory.json`](./data/preliminary-capability-inventory.json), [`data/preliminary-capability-inventory.csv`](./data/preliminary-capability-inventory.csv)
- **Anti-Inflation Guarantee:** Consolidates lifecycle completeness per domain rather than splitting trivial CRUD verbs into separate features (`161` substantive architectural, protocol, governance, operational, and UX capabilities across `25` domains).

---

## 1. Portfolio Summary by Classification, Priority & Domain

### 1.1 Proposal Classification & Priority Breakdown

| Proposal Classification | Count | Meaning for `TEEP-02A` Scope Review |
|---|---:|---|
| **`KEEP`** | `10` | Crown-jewel RC9 capabilities verified in code (`wazero` sandbox, `BlindBroker`, `SSRFGuard`, `u128` two-phase ledger, 6-step `Resolver`, `SM-09`, `FMEA-01`) to preserve intact. |
| **`IMPROVE`** | `73` | Existing RC9 capabilities requiring wiring completion, `ARCH-OBS-01` / `109 RPS` scale unlocks, lifecycle parity, or `DEF-*`/`PG-*` closure. |
| **`REPLACE`** | `5` | RC9 implementations whose wire contract, precision unit, or packer architecture should be superseded (`RateCard` v2 `nano/tok` → v3 `nano/1M`, custom `/a2a/tasks` → LF A2A `v1.0.1`, split `.tpkg` packers → unified Merkle packer, unreachable debug sessions → `HPKE` Flight Recorder, file-copy `gitops` → DB GitOps). |
| **`REMOVE`** | `1` | Obsolete/ghost checks, unused packages (`@fontsource/figtree`, ghost `TANPOPO_ADMIN_LISTEN_ADDR`), and stale milestone banners creating drift. |
| **`ADD`** | `72` | Net-new enterprise, protocol, extensibility, marketplace, compliance, and validated product-innovation capabilities. |
| **Total** | **`161`** | **`P0-Foundational`: `54` · `P1-High`: `68` · `P2-Strategic`: `36` · `P3-Horizon`: `3`** |

### 1.2 Quick-Navigation Index Across All 25 Domains

| Domain | Name | Count | `KEEP` | `IMPROVE` | `REPLACE` | `REMOVE` | `ADD` | Capability Range |
|---|---|---:|---:|---:|---:|---:|---:|---|
| `DOM-01` | **Universal AI Gateway** | `8` | `3` | `2` | `0` | `0` | `3` | `CAP-001` .. `CAP-008` |
| `DOM-02` | **Provider Management** | `7` | `0` | `5` | `0` | `0` | `2` | `CAP-009` .. `CAP-015` |
| `DOM-03` | **Model Intelligence** | `7` | `0` | `2` | `1` | `0` | `4` | `CAP-016` .. `CAP-022` |
| `DOM-04` | **Routing and Orchestration** | `8` | `1` | `3` | `0` | `0` | `4` | `CAP-023` .. `CAP-030` |
| `DOM-05` | **Reliability Engineering** | `6` | `0` | `4` | `0` | `0` | `2` | `CAP-031` .. `CAP-036` |
| `DOM-06` | **AI FinOps** | `9` | `1` | `4` | `0` | `0` | `4` | `CAP-037` .. `CAP-045` |
| `DOM-07` | **Caching and Performance** | `7` | `0` | `5` | `0` | `0` | `2` | `CAP-046` .. `CAP-052` |
| `DOM-08` | **Observability** | `7` | `1` | `4` | `0` | `0` | `2` | `CAP-053` .. `CAP-059` |
| `DOM-09` | **AI Security and Guardrails** | `8` | `2` | `3` | `0` | `0` | `3` | `CAP-060` .. `CAP-067` |
| `DOM-10` | **Identity and Access Management** | `8` | `0` | `5` | `0` | `0` | `3` | `CAP-068` .. `CAP-075` |
| `DOM-11` | **Governance and Compliance** | `7` | `0` | `4` | `0` | `0` | `3` | `CAP-076` .. `CAP-082` |
| `DOM-12` | **MCP Infrastructure** | `8` | `0` | `4` | `0` | `0` | `4` | `CAP-083` .. `CAP-090` |
| `DOM-13` | **Agent and A2A Infrastructure** | `6` | `1` | `2` | `1` | `0` | `2` | `CAP-091` .. `CAP-096` |
| `DOM-14` | **Prompt Management** | `3` | `0` | `0` | `0` | `0` | `3` | `CAP-097` .. `CAP-099` |
| `DOM-15` | **AI Evaluation** | `4` | `0` | `0` | `0` | `0` | `4` | `CAP-100` .. `CAP-103` |
| `DOM-16` | **Infrastructure Operations** | `7` | `0` | `6` | `0` | `0` | `1` | `CAP-104` .. `CAP-110` |
| `DOM-17` | **Extension Platform and SDK** | `7` | `1` | `3` | `1` | `0` | `2` | `CAP-111` .. `CAP-117` |
| `DOM-18` | **Marketplace and Community Ecosystem** | `7` | `0` | `4` | `0` | `0` | `3` | `CAP-118` .. `CAP-124` |
| `DOM-19` | **Notifications and Integrations** | `5` | `0` | `2` | `0` | `0` | `3` | `CAP-125` .. `CAP-129` |
| `DOM-20` | **Product and Developer Experience** | `7` | `0` | `6` | `0` | `1` | `0` | `CAP-130` .. `CAP-136` |
| `DOM-21` | **Async, Batch, Realtime & Multimodal Media Workloads** | `6` | `0` | `1` | `0` | `0` | `5` | `CAP-137` .. `CAP-142` |
| `DOM-22` | **Context, Memory, RAG & Vector Governance** | `4` | `0` | `1` | `0` | `0` | `3` | `CAP-143` .. `CAP-146` |
| `DOM-23` | **Privacy Engineering, Data Sovereignty & Cryptographic Custody** | `5` | `0` | `0` | `1` | `0` | `4` | `CAP-147` .. `CAP-151` |
| `DOM-24` | **Resilience, Chaos Engineering & Pre-Flight Policy Simulation** | `5` | `0` | `3` | `0` | `0` | `2` | `CAP-152` .. `CAP-156` |
| `DOM-25` | **GitOps, Terraform/IaC & Portable Declarative Solution Packs** | `5` | `0` | `0` | `1` | `0` | `4` | `CAP-157` .. `CAP-161` |

---

## 2. Complete Capability Specifications (`CAP-001` .. `CAP-161`)

### DOM-01 — Universal AI Gateway

#### `CAP-001` — Unified OpenAI /v1/chat/completions & Streaming Inference Plane
- **Proposal Classification:** `KEEP` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-02, PER-10 | WF-01, WF-03`
- **User Problem & Value:** Provides standard OpenAI-compatible chat, tool calling, structured JSON schema, and SSE streaming across heterogeneous providers.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE in internal/ingress/http/chat_completions.go (CR-09 auto include_usage, strict JSON schema capability check, blob_ref splicing).
- **Competitor & Technology Evidence:** Universal baseline across LiteLLM, Portkey, Bifrost, OpenRouter, Agent Router, and Solo agentgateway (SRC-005..019).
- **Potential Improvements / Target Design:** Preserve core pipeline while expanding reasoning_effort enum to [none, minimal, low, medium, high, xhigh, max] and adding annotations/citations fields.
- **Enterprise Implications:** Supports 80%+ of legacy enterprise LLM microservices with zero client code changes.
- **Dependencies & Constraints:** ADR-0001, SPEC-EXT-06 (CT-08 Zero Silent Downgrade).
- **Security & Operational Considerations:** Enforce MaxRequestBodyBytes, VirtualKey plane check (PROVIDER), and clean SSE abort without synthetic [DONE].

#### `CAP-002` — Top-Level Native Vendor SDK Endpoints (/v1/messages, /v1beta/models/*)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-02, PER-08 | WF-01, WF-03`
- **User Problem & Value:** Allows official Anthropic (@anthropic-ai/sdk / Claude Code) and Google GenAI (@google/genai) SDKs to connect via standard ANTHROPIC_BASE_URL / GEMINI_BASE_URL without path-prefix rewrites.
- **Existing Tanpopo RC9 Capability:** MISSING at top level in RC9; callers must prepend /native/anthropic or /native/gemini (DEF-048 partial).
- **Competitor & Technology Evidence:** Supported natively by Solo agentgateway (/v1/messages), Agent Router (/anthropic/v1/messages), Bifrost (/anthropic, /genai), and Ollama v0.40 (/v1/messages) (SRC-011, SRC-016, SRC-019, SRC-044).
- **Potential Improvements / Target Design:** Mount top-level POST /v1/messages (+ /v1/messages/count_tokens) and /v1beta/models/{model}:{action} delegating into the native/bijective adapter pipeline with full VirtualKey, Policy, and FinOps enforcement.
- **Enterprise Implications:** Eliminates #1 developer onboarding friction for Claude Code and Gemini enterprise teams.
- **Dependencies & Constraints:** Resolves DEF-048 split-role parity; preserves DEC-003 4-plane separation.
- **Security & Operational Considerations:** Authenticate x-api-key, x-goog-api-key, and Authorization: Bearer vk_tp_* uniformly without leaking inbound headers upstream.

#### `CAP-003` — Full Agentic /v1/responses Plane & Upward GPT-5.4+/6.2 Translation
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-08, PER-10 | WF-03, WF-08`
- **User Problem & Value:** Enables modern multi-step agentic tool + reasoning loops; required because OpenAI GPT-5.4+/6.2 disallow tools + active reasoning on /v1/chat/completions.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION in internal/ingress/http/responses.go: stateless single-turn OpenAI-only shim; Tier-2 C/Wasm adapters ignore _tanpopo_modality.
- **Competitor & Technology Evidence:** OpenAI Responses API is the primary 2026 agentic surface (store:false encrypted_content, /compact, /input_tokens, 2h/8h background) (SRC-048); supported by Solo agentgateway and Portkey.
- **Potential Improvements / Target Design:** Implement bijective /v1/responses across OpenAI, Anthropic, Gemini, Bedrock, and Foundry; auto-translate /v1/chat/completions (tools + reasoning_effort) upward to /v1/responses on GPT-5.4+/6.2; wire responseCache.
- **Enterprise Implications:** Prevents silent agent breakages as enterprises migrate from GPT-4o to GPT-5.x/6.x.
- **Dependencies & Constraints:** CAP-005, CAP-143; CT-08 fail-loudly validation on unsupported stateful parameters.
- **Security & Operational Considerations:** Block or redirect OpenAI hosted remote mcp tool blocks through Tanpopo /mcp policy when DEC-003 governance is enforced.

#### `CAP-004` — Full-Method & Query-Aware /native/{dialect}/* Passthrough
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-03 | WF-02, WF-03`
- **User Problem & Value:** Unblocks lossless vendor-specific endpoints (Azure ?api-version=, GET /v1/models, Bedrock Converse, multipart uploads) on /native/{dialect}/{upstreamPath}.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION in internal/ingress/native/handler.go: hardcodes query params to alt/beta (:1798), mandates JSON model hint (:300), and prepends leading '/' in inspector.go:157 ('/model/...') while aws-bedrock/src/adapter.c:654 checks 'model/' without '/' (DEF-048).
- **Competitor & Technology Evidence:** LiteLLM and Portkey provide pass-through routes, though LiteLLM buffers JSON bodies for spend logging (SRC-005).
- **Potential Improvements / Target Design:** Allow manifest-declared query parameter allowlists (e.g., api-version), model-optional native routes (GET /v1/models), aligned '/'-prefixed inspector paths across host and adapters, and streaming multipart passthrough.
- **Enterprise Implications:** Guarantees 100% day-zero access to cutting-edge vendor betas under enterprise Vault and FinOps controls.
- **Dependencies & Constraints:** Resolves DEF-048; depends on BlindHostCredentialBroker (CAP-064).
- **Security & Operational Considerations:** Retain strict dot-segment / encoded-slash path traversal rejection and UNMETERED_FLAGGED fallback alerts.

#### `CAP-005` — Multi-Turn Cryptographic Reasoning Signature Continuity
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-08 | WF-03, WF-08`
- **User Problem & Value:** Prevents HTTP 400 errors and prompt-cache invalidation during multi-turn tool-calling reasoning loops across Gemini 2.5/3/4, Anthropic Claude 4/5, and OpenAI ZDR.
- **Existing Tanpopo RC9 Capability:** MISSING on unified /v1/chat/completions in RC9 (strict parser omits thought_signature / signature / redacted_thinking / encrypted_content).
- **Competitor & Technology Evidence:** Gemini 3/4 strictly validates functionCall thoughtSignature across tool turns (SRC-055); Anthropic verifies thinking signature (SRC-051); OpenAI store:false uses reasoning.encrypted_content (SRC-048).
- **Potential Improvements / Target Design:** Carry vendor-tagged opaque signature envelopes in unified message/item schemas (plus optional short-TTL Valkey (org_id, tool_call_id) -> thoughtSignature bridge for legacy OpenAI SDK callers), failing loudly on cross-vendor replay.
- **Enterprise Implications:** Essential for production coding agents and multi-step enterprise tool workflows.
- **Dependencies & Constraints:** SPEC-EXT-06 (CT-08).
- **Security & Operational Considerations:** Never forward an Anthropic/OpenAI cryptographic blob to a different vendor on failover; strip only if stateless re-reasoning policy is explicitly enabled.

#### `CAP-006` — Request Payload Fingerprint Idempotency Guard
- **Proposal Classification:** `KEEP` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-07, PER-10 | WF-03, WF-06`
- **User Problem & Value:** Prevents duplicate billing and duplicate side-effects when clients retry timed-out POST inference requests with Idempotency-Key.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE in internal/ingress/idempotency/guard.go (409 in-progress lock, 422 SHA-256 payload mismatch, 24h TTL), backed by SQL idempotency_records.
- **Competitor & Technology Evidence:** Rare among OSS AI gateways (absent in LiteLLM OSS, Bifrost OSS, and agentgateway) (SRC-005..019).
- **Potential Improvements / Target Design:** Keep strict RFC-style fingerprint semantics while pairing with CAP-035 Valkey hot-path storage to remove SQL write serialization.
- **Enterprise Implications:** Required for financial/regulated batch and payment-adjacent AI workflows.
- **Dependencies & Constraints:** CAP-035 (Valkey acceleration).
- **Security & Operational Considerations:** Scope idempotency keys strictly by (org_id, project_id, virtual_key_id) and seal cached response bodies with Project DEK.

#### `CAP-007` — Clean SSE Mid-Stream Error Framing & Usage Accuracy Trailers
- **Proposal Classification:** `KEEP` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-07 | WF-03, WF-06`
- **User Problem & Value:** Signals mid-stream upstream disconnects accurately without faking completion ([DONE]) and communicates exact/estimated/unmetered billing status via HTTP Trailers.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE in internal/ingress/sse/pump.go (FMEA-01) and native/handler.go (DEF-083 Trailer: X-Tanpopo-Usage-Accuracy).
- **Competitor & Technology Evidence:** Fixes a notorious bug class in LiteLLM (#54466 mid-stream vLLM aborts treated as clean stops) (SRC-005).
- **Potential Improvements / Target Design:** Keep FMEA-01 & DEF-083 trailer contract and extend automated trailer E2E verification across Anthropic SSE, Gemini SSE, and Bedrock EventStream (RR-014/024).
- **Enterprise Implications:** Prevents truncated legal/medical/code completions from being silently saved by downstream apps as complete.
- **Dependencies & Constraints:** CAP-142 (binary EventStream framing).
- **Security & Operational Considerations:** Bounded 2 MiB line buffers + 15s keepalive pings prevent stalled socket leaks.

#### `CAP-008` — Specialized /v1/fim/completions, /v1/decisions & /v1/moderations Surfaces
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-09 | WF-03`
- **User Problem & Value:** Supports low-latency IDE code autocomplete (Fill-in-the-Middle), typed probabilistic routing/classification (/v1/decisions), and standalone safety moderation.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 openapi/dataplane-v1.yaml.
- **Competitor & Technology Evidence:** Mistral /v1/fim/completions (Codestral), OpenAI & Microsoft Foundry /v1/decisions (GA Oct 2026), Solo agentgateway Decisions, OpenAI /v1/moderations (SRC-048, SRC-061, SRC-064).
- **Potential Improvements / Target Design:** Add typed unified endpoints and declarative adapter mappings with zero-rate or token-metered RateCard support.
- **Enterprise Implications:** Allows enterprises to consolidate IDE Tab-completion and classification microservices onto one governed gateway.
- **Dependencies & Constraints:** DOM-02 adapter manifests.
- **Security & Operational Considerations:** Enforce identical residency, guardrail, and token-budget controls as chat endpoints.

### DOM-02 — Provider Management

#### `CAP-009` — Hardened Official Tier-1 & Tier-2 Provider Adapters
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-03, PER-06 | WF-01, WF-02`
- **User Problem & Value:** Eliminates functional bugs and data-residency bypasses across Tanpopo's 8 bundled official adapters.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION across extensions/official/*: aws-bedrock drops multimodal blocks (adapter.c:36-59), hardcodes claude-3-5-sonnet probe (:104), & checks 'model/' without '/' at :654 vs inspector.go:157 '/model/...'; gemini lacks /v1/embeddings; ollama allows *:cloud remote proxy tags.
- **Competitor & Technology Evidence:** Bedrock Converse multimodal & Mantle (SRC-059), Gemini embedContent (SRC-055), Ollama v0.40 *:cloud residency hazard (SRC-044).
- **Potential Improvements / Target Design:** Restore multimodal blocks, dynamic probe, & '/model/' native inspector prefix check in aws-bedrock, add bedrock.<region> & bedrock-mantle.<region>.api.aws to manifest, block global.* profiles under regional policies, quarantine Ollama *:cloud under ON_PREM, and add Gemini /v1/embeddings.
- **Enterprise Implications:** Prevents silent multimodal data loss and cross-border data leaks in regulated deployments.
- **Dependencies & Constraints:** CAP-016, CAP-077, CAP-117 (build-wasm.sh must compile aws-bedrock).
- **Security & Operational Considerations:** All Tier-2 C/Rust Wasm adapters remain <=16 MiB, 0 WASI imports, and 100% BlindBroker-mediated.

#### `CAP-010` — Expanded Cloud, Sovereign & Self-Hosted GPU Adapter Suite
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-03 | WF-02, WF-03`
- **User Problem & Value:** Provides turnkey first-party adapters for Google Vertex AI, Microsoft Foundry (/openai/v1/*), Cohere v2, Mistral, DeepSeek, Groq, Fireworks, Together, Databricks, and CNCF llm-d / SGLang.
- **Existing Tanpopo RC9 Capability:** MISSING dedicated adapters (users fall back to basic chat-only openai-compatible or legacy azure-openai).
- **Competitor & Technology Evidence:** Vertex AI multi-partner (:generateContent, :rawPredict, MaaS) (SRC-055), Foundry GA /openai/v1/* (SRC-061), llm-d 1.0 GAIE & sgl-router (SRC-032, SRC-033).
- **Potential Improvements / Target Design:** Ship signed Tier-1/Tier-2 .tpkg packages with native Data Zone / regional endpoint templates, GAIE prefix-affinity headers, and provider-specific error normalization.
- **Enterprise Implications:** Covers >98% of enterprise cloud commitments (AWS, Azure, GCP, Databricks) and internal Kubernetes GPU pools.
- **Dependencies & Constraints:** CAP-012 (Entra ID / GCP WIF / SigV4 auth), CAP-118.
- **Security & Operational Considerations:** Enforce strict host suffix allowlists per adapter and DEC-004 commercial/self-hosted API compliance.

#### `CAP-011` — Blind Two-Phase Connection Probing & Actionable Failure Diagnostics
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-03 | WF-01, WF-02`
- **User Problem & Value:** Gives operators immediate, secret-safe root-cause feedback (TLS CA, SSRF block, 401 key, 404 /v1 path, clock skew) when testing a Provider Connection.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION in connection_health.go & ProvidersPage.tsx: failed probe returns HTTP 200 {valid:false} with zero UI banner/toast (PG-013, RR-022).
- **Competitor & Technology Evidence:** Operators in LiteLLM (#8498) and Envoy AI GW suffer from opaque or false-green health checks (SRC-005, SRC-016).
- **Potential Improvements / Target Design:** Return sanitized CheckDiagnosticCode + remediation hint from POST /connections/{id}/test, record socket-level SSRF rejection telemetry (RR-022), and render explicit Probe Failed banner + inline remediation in ProvidersPage.tsx.
- **Enterprise Implications:** Cuts provider troubleshooting tickets from hours to seconds in corporate proxy/firewall environments.
- **Dependencies & Constraints:** Resolves PG-013 and RR-022.
- **Security & Operational Considerations:** Run all probes through BlindHostCredentialBroker + secret scrubber so raw credentials or internal IPs never leak into UI responses.

#### `CAP-012` — Full Multi-Mode Connection Auth (AWS SigV4, Entra ID, mTLS, OAuth 2.1, Outbound WIF)
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-05, PER-06 | WF-02, WF-05`
- **User Problem & Value:** Allows enterprise connections to authenticate using keyless cloud IAM roles (AWS IRSA/STS, GCP WIF, Azure Entra ID), mTLS client certs, SigV4, and OAuth 2.1.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: admin/http/connections.go:798,808 hardcodes InjectModeHostBlind + AuthModeAPIKey (blocking AWS_SIGV4!); broker/workload_identity.go is unwired in serve.go; OAuth requires undocumented TANPOPO_OAUTH_REDIRECT_URI and lacks Device Code UI.
- **Competitor & Technology Evidence:** Agent Router BackendSecurityPolicy and Bedrock AgentCore rely heavily on keyless AWS SigV4 / IRSA and Entra ID (SRC-016, SRC-040, SRC-061).
- **Potential Improvements / Target Design:** Fix connections.go to honor adapter-declared InjectMode/AuthMode (HOST_CRYPTO_SIGN AWS_SIGV4, MTLS_CLIENT_CERT, OAUTH2, WORKLOAD_IDENTITY), wire NewWorkloadIdentityExchanger (+ Azure Entra WIF) in serve.go, and expose Device Code & Client Credentials in Console.
- **Enterprise Implications:** Mandatory for enterprises whose cloud security policies prohibit static IAM access keys or Azure api-keys.
- **Dependencies & Constraints:** DEC-004 (strictly commercial/enterprise OAuth & IAM; zero consumer subscription cookie/session scraping).
- **Security & Operational Considerations:** Cache short-lived STS/Entra tokens in BlindBroker RAM only (never persisted in plaintext); bind mTLS keys inside Vault.

#### `CAP-013` — Complete Provider Connection Lifecycle, Org/Project Sharing & /v1 URL Normalizer
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-03, PER-04 | WF-01, WF-02`
- **User Problem & Value:** Eliminates connection re-creation churn by allowing full-field updates, explicit org-vs-project sharing controls, and automatic URL path normalization.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION in ProvidersPage.tsx: Edit drawer only edits name, endpoint_url, and secret_bindings (omits CIDR, private-IP, region, billing, sharing); trailing '/v1' causes /v1/v1/models 404.
- **Competitor & Technology Evidence:** Verified in QA-005 RC9-REPORT.md:46 (/v1/v1/models trap) and 04-CURRENT-TANPOPO-PRODUCT-AUDIT.md & 07-BUSINESS-WORKFLOW-ANALYSIS.md.
- **Potential Improvements / Target Design:** Add GET /connections/{id}, expose all mutable connection fields + explicit Org-Shared vs Project-Scoped + shared_project_ids selector in ProvidersPage.tsx, and auto-normalize/warn on duplicate '/v1' suffixes.
- **Enterprise Implications:** Enables central cloud ops teams to share vetted regional connections across dozens of application projects cleanly.
- **Dependencies & Constraints:** If-Match optimistic concurrency + CAP-153 blast-radius check on region/billing edits.
- **Security & Operational Considerations:** Changing allow_private_ip or allowed_cidrs triggers immediate pooled-socket eviction (CAP-046) and JCS audit logging.

#### `CAP-014` — In-Place Vault Secret Rotation, OCI_REGISTRY_AUTH & Consumer Dependency Tracking
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-05 | WF-02, WF-12`
- **User Problem & Value:** Lets operators rotate an API key or registry secret in place without minting a new credential ID and manually updating every bound connection and webhook.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: POST/GET/DELETE /secrets only (zero PUT/PATCH in-place rotation); ProvidersPage.tsx omits OCI_REGISTRY_AUTH secret kind and bound-connection counts.
- **Competitor & Technology Evidence:** Automated secret rotation is an enterprise compliance baseline (LiteLLM gates key rotation in Enterprise; Agent Router v1.1 suffered stale K8s Secret cache bugs, SRC-005, SRC-016).
- **Potential Improvements / Target Design:** Add PUT /admin/v1/secrets/{credentialId} atomic secret-material rotation (bumping config_epoch + evicting BlindBroker transport cache), expose OCI_REGISTRY_AUTH in UI, and show bound connections/webhooks per secret.
- **Enterprise Implications:** Supports zero-downtime 30/60/90-day enterprise credential rotation schedules.
- **Dependencies & Constraints:** Vault Project/Org DEK AEAD envelope (CAP-067).
- **Security & Operational Considerations:** Write-only secret API contract preserved (GET never returns secret bytes; old secret zeroized in RAM on epoch refresh).

#### `CAP-015` — Provisioned Throughput (PTU / Reserved Tier) Capacity Modeling & Spillover
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-03, PER-07 | WF-02, WF-06`
- **User Problem & Value:** Maximizes ROI on pre-paid Azure OpenAI PTUs, AWS Bedrock Provisioned Throughput, and Vertex Provisioned units before spilling burst traffic to pay-as-you-go endpoints.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (SELF_HOSTED_INFRA supports hourly GPU cost, but commercial connections lack PTU TPM saturation routing).
- **Competitor & Technology Evidence:** Azure Foundry PTU vs Global Standard and Bedrock Reserved Tier vs Mantle split quotas (SRC-059, SRC-061).
- **Potential Improvements / Target Design:** Model provisioned TPM/RPM/utilization targets on ModelDeployments, prioritizing sunk-cost PTU capacity up to 90-95% saturation and routing excess smoothly to PAYG deployments with distinct FinOps attribution.
- **Enterprise Implications:** Saves 25-50% on large enterprise cloud commitments by eliminating idle PTUs and unexpected 429s.
- **Dependencies & Constraints:** CAP-025, CAP-041.
- **Security & Operational Considerations:** Track PTU utilization in cluster-shared Valkey buckets so multi-pod gateways do not over-saturate reserved capacity.

### DOM-03 — Model Intelligence

#### `CAP-016` — Canonical RateCard v3 (nano_usd_per_1m_tokens Exact Precision & Dual-Unit $/1M UX)
- **Proposal Classification:** `REPLACE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-03, PER-07 | WF-01, WF-02, WF-06`
- **User Problem & Value:** Eliminates up to ~12% integer rounding drift on sub-nano token prices and removes error-prone manual \$/1M-to-nano-USD mental math in the Console.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: RateCard v2 nano_usd_per_token truncates 1,664 real LiteLLM token-cost fields (e.g., 8.75 and 144.375 nano-USD/tok); Admin DTO models.go:60-71 omits 3 fields; UI requires raw nano-USD/tok integers.
- **Competitor & Technology Evidence:** Proven empirically across 7,193 models in litellm_model_prices.json (SRC-045), models.dev (\$/1M structs, SRC-046), and OpenRouter string decimals (SRC-047).
- **Potential Improvements / Target Design:** Standardize RateCard v3 on int64 nano_usd_per_1m_tokens (10^-15 USD/tok, using existing u128 computePerMillionCeiling in calculator.go:702), expose all media/rerank/search/cache-1h/tier fields in Admin API, and provide \$/1M <-> nano-USD dual-unit inputs in Console.
- **Enterprise Implications:** Guarantees 100% exact integer representation of every real vendor token price with zero floating-point drift.
- **Dependencies & Constraints:** Backward-compatible auto-migration of v1/v2 pricing_json rows (x 1,000,000).
- **Security & Operational Considerations:** Reject scientific-notation floats at runtime; enforce u128 overflow checks and explicit self_hosted_zero_rate flag.

#### `CAP-017` — Ed25519/TUF-Signed Offline Model Intelligence Packs & Decimal-Sanitized Sync
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-03, PER-07 | WF-01, WF-02`
- **User Problem & Value:** Provides curated, air-gap-safe model context limits, capabilities, deprecation dates, and multi-tier reference pricing out of the box without trusting raw upstream floats.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (0 bundled model/price catalog; 100% manual entry).
- **Competitor & Technology Evidence:** Cross-audit of litellm_model_prices.json (7,193 models, 22,940 sci-notation literals, 66 IEEE-754 float artifacts, 109 missing providers), models.dev (10,078 models), and OpenRouter (476 models) (SRC-045..047).
- **Potential Improvements / Target Design:** Build a deterministic offline Decimal catalog compiler reconciling models.dev + LiteLLM + OpenRouter into Ed25519/TUF-signed Model Intelligence Packs embedded in the binary and updatable via 'tanpopo catalog import-pack' or opt-in CATALOG_FEED staging.
- **Enterprise Implications:** Works identically in disconnected defense/banking enclaves and cloud deployments.
- **Dependencies & Constraints:** CAP-016, CAP-112 (CATALOG_FEED), CAP-122 (TUF).
- **Security & Operational Considerations:** FinOps anti-poisoning gate: stage upstream price changes > +/-25% or unexpected \$0 commercial rates for ORG_OWNER/FINOPS approval.

#### `CAP-018` — BlindBroker Account-Authenticated Model Discovery & 1-Click Bulk Onboarding
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-03, PER-04 | WF-01, WF-02`
- **User Problem & Value:** Proves which models/deployments a specific credential can actually invoke right now and eliminates tedious manual registration of dozens of models.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: ValidateConnection returns transient detected_model_ids, which ProvidersPage.tsx discards.
- **Competitor & Technology Evidence:** Public catalogs do not prove account entitlement, while native /v1/models, Bedrock ListFoundationModels+ListInferenceProfiles, Azure deployments, and Ollama /api/show lack pricing (SRC-044..063).
- **Potential Improvements / Target Design:** Add two-phase BlindBroker DiscoverModels probes persisting connection_discovered_models (mapping Azure custom deployment names -> canonical models, tagging Bedrock global.* vs regional profiles, reading Ollama /api/show capabilities) + 1-click Console bulk import pre-filled from Layer 1/2 catalog.
- **Enterprise Implications:** Collapses multi-region enterprise provider onboarding from hours of spreadsheet entry to <60 seconds (HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** CAP-011, CAP-017; BlindHostCredentialBroker (0 raw secrets in Wasm).
- **Security & Operational Considerations:** Automatically quarantine Ollama *:cloud tags when DataRegion == ON_PREM and flag Bedrock global.* as MULTI_REGION.

#### `CAP-019` — 4-Layer Precedence Engine, Sticky Manual Overrides & Provenance Tracking
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-06, PER-07 | WF-02, WF-06`
- **User Problem & Value:** Preserves negotiated enterprise EA discounts and custom capability pins across catalog updates while maintaining immutable historical financial records.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: usage_settlements.applied_pricing_snapshot_json freezes settled rows, but model/deployment records lack catalog provenance or sticky override locks.
- **Competitor & Technology Evidence:** Founder Requirement 6.A: never assume public catalog prices equal negotiated enterprise rates.
- **Potential Improvements / Target Design:** Enforce strict 4-layer precedence (Manual Override > Account Discovery > Upstream Feed > Signed Offline Pack) with field-level provenance metadata and non-destructive drift_advisory badges when vendor list prices move.
- **Enterprise Implications:** Essential for Fortune 500 procurement agreements (AWS EDP, Azure EA, OpenAI Enterprise custom rate sheets).
- **Dependencies & Constraints:** CAP-016, CAP-017, CAP-018.
- **Security & Operational Considerations:** Catalog syncs create new deployment versions forward-only and never mutate manual overrides or historical usage_settlements.

#### `CAP-020` — Complete Logical Model & Deployment Rate-Card CRUD / Retirement Lifecycle
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-04, PER-07 | WF-02`
- **User Problem & Value:** Allows operators to fix typos, update vendor token prices, adjust capability assurance tiers, and retire obsolete models cleanly via Console and API.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: Logical Models have zero GET/{id}, PATCH, or DELETE/retire API (PG-004); Console ModelsPage.tsx only toggles Deployment state (cannot edit pricing or upstream model!).
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04 & TEEP-01-DOC-07 (PG-004).
- **Potential Improvements / Target Design:** Add GET/PATCH/DELETE (and RETIRED state) on /admin/v1/models with 409 active-route dependency checks, and expose full RateCard v3 + capability + timeout editing on /admin/v1/deployments in ModelsPage.tsx.
- **Enterprise Implications:** Removes a severe Day-2 operational trap whenever providers update pricing or model IDs.
- **Dependencies & Constraints:** Resolves PG-004; guarded by If-Match ETags.
- **Security & Operational Considerations:** Every rate-card mutation appends a JCS audit event recording old vs new nano-USD rates and actor identity.

#### `CAP-021` — Automated Model Deprecation Monitoring & Capability-Verified Migration Advisor
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-02, PER-03, PER-07 | WF-04`
- **User Problem & Value:** Eliminates emergency outages when cloud vendors retire model checkpoints by proactively validating replacement models against real route traffic and budgets.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** litellm_model_prices.json contains 1,284 deprecation_date entries (407 already past, 877 upcoming as of 2026-11-27) (SRC-045); no competitor unifies EOL alerts + zero-prompt traffic replay + shadow evals.
- **Potential Improvements / Target Design:** Emit T-90d/30d/7d sunset alerts, verify 100% exercised capability & p100 token-window parity from request_traces_meta, compute exact 30d micro-USD cost delta via What-If Engine, optionally run 1% shadow canary, and generate a 1-click If-Match route migration patch (INN-02).
- **Enterprise Implications:** Turns quarterly vendor model churn into a predictable, audited 5-minute workflow.
- **Dependencies & Constraints:** CAP-017, CAP-101, CAP-152 (INN-01, INN-02).
- **Security & Operational Considerations:** Shadow canary evaluations are strictly bounded by a dedicated shadow_budget_ledger and restricted to non-mutating requests.

#### `CAP-022` — Model Weights License & Jurisdictional Compliance Metadata Gate
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-06, PER-03 | WF-02, WF-14`
- **User Problem & Value:** Prevents legal compliance violations from deploying open-weight or partner models with non-OSI geographic, MAU, or commercial restrictions.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Meta Llama 4 Community License legally prohibits EU-domiciled enterprises from using multimodal Llama 4 / Llama Guard 4 weights (SRC-028); Katanemo Plano models use Llama-3.2-derived terms (SRC-021).
- **Potential Improvements / Target Design:** Include SPDX / license_class (OSI_PERMISSIVE, SOURCE_AVAILABLE, COMMUNITY_RESTRICTED, PROPRIETARY) and restricted_jurisdictions[] in Model Intelligence Packs so Cedar/Structural policies automatically block prohibited models for EU/regulated tenants.
- **Enterprise Implications:** Protects European and regulated enterprises from accidental IP/licensing liability.
- **Dependencies & Constraints:** CAP-017, CAP-071, CAP-081.
- **Security & Operational Considerations:** Fail-closed at Route Resolution Step 2 when org jurisdiction intersects model restricted_jurisdictions.

### DOM-04 — Routing and Orchestration

#### `CAP-023` — Deterministic 6-Step Route Resolver, SWRR & Monotonic Residency Narrowing
- **Proposal Classification:** `KEEP` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-06 | WF-03`
- **User Problem & Value:** Guarantees sub-microsecond, zero-silent-downgrade route selection honoring data residency, plugin trust, capabilities, health, and weights.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE in internal/routing/resolver/resolver.go (Smooth Weighted Round-Robin, X-Tanpopo-Data-Residency narrowing, RequireTestedCaps).
- **Competitor & Technology Evidence:** Cleaner deterministic compliance guarantees than best-effort Python/Redis routers (SRC-004, SRC-005).
- **Potential Improvements / Target Design:** Preserve core 6-step resolution order while optimizing heap allocations (CAP-052) and extending Step 5/6 strategies.
- **Enterprise Implications:** Core compliance backbone for multi-region and multi-vendor traffic governance.
- **Dependencies & Constraints:** ADR-0006, SPEC-CORE-01.
- **Security & Operational Considerations:** Request headers can only narrow AllowedDataRegions, never widen parent Org/Project/Env residency.

#### `CAP-024` — Dual-Mode Prefix / KV-Cache Affinity Router (Consistent Hash with Bounded Loads)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-02, PER-03, PER-07 | WF-03, WF-06`
- **User Problem & Value:** Preserves 50-97.5% commercial prompt-cache read discounts and 10x-57x self-hosted vLLM/SGLang TTFT speedups destroyed by naive round-robin load balancing.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (SWRR scatters multi-turn conversations across deployments/connections).
- **Competitor & Technology Evidence:** OpenRouter warm-cache sticky routing (SRC-013), Anthropic 90-97.5% cache reads (SRC-051), OpenAI prompt_cache_key (SRC-048), and CNCF llm-d 1.0 KV prefix routing (SRC-032).
- **Potential Improvements / Target Design:** Compute keyed in-RAM 64-bit SipHash over canonical system + tools + leading prefix blocks (0 prompt bytes stored) and route via Bounded-Load Consistent Hashing (c=1.25) across healthy deployments, injecting prompt_cache_key / GAIE affinity headers (INN-07).
- **Enterprise Implications:** Delivers immediate 30-70% token bill reduction and lower TTFT on multi-turn enterprise agents (HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** CAP-023, CAP-051.
- **Security & Operational Considerations:** Use per-project ephemeral RAM SipHash keys rotated periodically so prefix hashes cannot be reversed or correlated across tenants.

#### `CAP-025` — TTFT / TPOT SLO-Aware & Multi-Objective Dynamic Scoring
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-03 | WF-03`
- **User Problem & Value:** Routes interactive agents to lowest-TTFT / highest-tok/s providers instead of penalizing long-reasoning models via total-duration EWMA.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: LOWEST_LATENCY in resolver.go:623 tracks total request duration EWMA rather than TTFT or TPOT.
- **Competitor & Technology Evidence:** OpenRouter :nitro (tok/s), :floor (cache-adjusted effective price), and :exacto (tool-call reliability) multi-signal scoring (SRC-013).
- **Potential Improvements / Target Design:** Track separate EWMA histograms for TTFT, TPOT (tok/s), cache-adjusted effective cost, 429 rate, and tool-schema validity, exposing composable presets (LOWEST_TTFT, HIGHEST_THROUGHPUT, EFFECTIVE_LOWEST_COST, TOOL_RELIABILITY).
- **Enterprise Implications:** Prevents voice/IDE latency regressions when mixing fast Flash/Haiku endpoints with slow reasoning endpoints.
- **Dependencies & Constraints:** CAP-031 (cluster telemetry).
- **Security & Operational Considerations:** Bound EWMA influence with configurable floor weights to avoid oscillation/herd flapping.

#### `CAP-026` — Conditional CEL/Header Routing, Canary Splits & Shadow Mirroring
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `MEDIUM-HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-02, PER-03, PER-04 | WF-03, WF-04`
- **User Problem & Value:** Enables progressive 5% production rollouts, A/B prompt/model experiments, tier-based routing, and non-blocking shadow evaluations.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (static target priority/weights only).
- **Competitor & Technology Evidence:** Portkey x-portkey-config conditional DAGs, Cloudflare Dynamic Routes, and Envoy HTTPRoute traffic splitting (SRC-010, SRC-015, SRC-016).
- **Potential Improvements / Target Design:** Embed pure-Go google/cel-go bounded-cost predicates over request metadata (headers, VirtualKey tags, estimated_input_tokens, modality) + deterministic canary percentage buckets + async budget-capped shadow mirroring.
- **Enterprise Implications:** Required by enterprise SRE teams for safe progressive delivery of model and prompt upgrades.
- **Dependencies & Constraints:** google/cel-go (Apache-2.0, CGO_ENABLED=0); CAP-101.
- **Security & Operational Considerations:** Enforce cel.CostLimit at compile time (0 unbounded loops) and forbid shadow mirroring on mutating MCP/A2A calls.

#### `CAP-027` — Sandboxed Custom ROUTING_STRATEGY Wasm Scorer Plugins
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (HYPOTHESIS)`
- **Personas & Workflows:** `PER-03, PER-09 | WF-03, WF-11`
- **User Problem & Value:** Allows organizations and researchers to deploy proprietary domain-specific routing algorithms (.tpkg) without modifying Tanpopo Core.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Bifrost Go .so routing plugins break on Go patch upgrades; Plano requires GPU SLM inference (SRC-011, SRC-021).
- **Potential Improvements / Target Design:** Add ScoreRouteCandidates RPC in runtime.proto v1.1 (<=1 ms CPU, 8 MiB RAM, 0 network calls) running strictly after Core residency, trust, and capability filters, falling open to base route strategy on fault.
- **Enterprise Implications:** Supports custom sovereign bank/telco sovereign-capacity or carbon-aware GPU scheduling policies.
- **Dependencies & Constraints:** CAP-112 (runtime.proto v1.1).
- **Security & Operational Considerations:** Plugin receives anonymized telemetry vectors only (0 secrets, 0 raw prompt text by default) and cannot re-admit pruned non-compliant targets.

#### `CAP-028` — Live Explain == Select Equivalence, Regional UI Controls & Tiered Capability Warnings
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-04 | WF-03`
- **User Problem & Value:** Ensures Route Explain never diverges from live routing, exposes per-route region controls in UI, and clearly distinguishes primary vs fallback capability gaps.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: RoutingPage.tsx:254 omits allowed_data_regions input; multi-target capability warnings fail to split Primary vs Fallback (DEF-050); saved target rows & correlated VKey Explain E2E partial (DEF-027, DEF-028).
- **Competitor & Technology Evidence:** Verified in QA-005 CP83/CP98/CP100/CP125 and TEEP-01-DOC-04/07.
- **Potential Improvements / Target Design:** Expose allowed_data_regions in Create/Edit Route drawer, render full deployment provenance on saved route target rows, split capability warnings by tier (Primary vs Fallback #k), and enforce shared code path + E2E parity between Explain and live Select.
- **Enterprise Implications:** Eliminates operator confusion during multi-region failover design.
- **Dependencies & Constraints:** Resolves DEF-027, DEF-028, and DEF-050.
- **Security & Operational Considerations:** Preserve RBAC scoping so Explain never leaks cross-tenant deployment existence.

#### `CAP-029` — Hybrid On-Prem GPU (llm-d / vLLM) to Sovereign Cloud Burst Orchestration
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-03, PER-06, PER-07 | WF-03, WF-06`
- **User Problem & Value:** Keeps baseline inference on owned on-prem GPUs while automatically bursting queue spikes to residency-compliant cloud endpoints when TTFT SLOs breach.
- **Existing Tanpopo RC9 Capability:** MISSING queue-depth/TTFT-SLO burst triggers in RC9.
- **Competitor & Technology Evidence:** Enterprises running vLLM v0.34 / llm-d 1.0 / SGLang need upstream multi-cloud burst governance (SRC-031..033).
- **Potential Improvements / Target Design:** Combine local GPU queue/TTFT saturation thresholds with AllowedDataRegions fences (e.g., ON_PREM primary -> EU DataZone/Bedrock secondary; hard-blocked if policy is strictly ON_PREM only).
- **Enterprise Implications:** Improves internal GPU cluster utilization from ~40% to >80% without sacrificing tail-latency SLAs (HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** CAP-010, CAP-025, CAP-077.
- **Security & Operational Considerations:** Never burst prompts marked DataRegion == ON_PREM to external cloud endpoints; fail closed 503 if cloud burst is disallowed.

#### `CAP-030` — Automatic Cross-Dialect Request Adaptation Under Strict CT-08
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-03 | WF-03`
- **User Problem & Value:** Allows a single route to fail over cleanly across Anthropic (/v1/messages), OpenAI (/v1/responses), Gemini, and Bedrock Converse when capabilities are bijective.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: /v1/chat/completions fails over across chat adapters, but /v1/responses and top-level /v1/messages lack cross-adapter translation.
- **Competitor & Technology Evidence:** Verified in 05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md §1.5 (4-Tier Translation Matrix) and TEEP-01-DOC-04
- **Potential Improvements / Target Design:** Implement canonical internal turn/tool/usage intermediate representation (IR) across Tier-1/Tier-2 adapters, automatically adapting wire dialects on pre-first-byte failover while enforcing Tier-4 fail-loudly checks.
- **Enterprise Implications:** Provides true vendor-outage survivability for enterprise applications.
- **Dependencies & Constraints:** CAP-002, CAP-003, CAP-005.
- **Security & Operational Considerations:** Record exact dialect translation events and any Tier-3 lossy policy opt-ins in request_traces_meta.

### DOM-05 — Reliability Engineering

#### `CAP-031` — Cluster-Shared SM-04 Circuit Breakers & Semantic Inference Stall Detection
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03 | WF-03, WF-12`
- **User Problem & Value:** Prevents N gateway pods from independently sending 10x failing requests into a dead upstream and detects silent GPU hangs where /health returns 200 OK.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: circuit_breaker.go maintains node-local RAM windows only; lacks active semantic stall probing.
- **Competitor & Technology Evidence:** SGLang #51980 (2026-11-21) proved NIXL disaggregated serving can stall/corrupt KV while /health returns 200 OK (SRC-033).
- **Potential Improvements / Target Design:** Broadcast SM-04 state transitions (CIRCUIT_OPEN_UNHEALTHY, THROTTLED_COOLDOWN Retry-After) across pods via Valkey Pub/Sub, and support lightweight token-generation canary probes for self-hosted GPU pools.
- **Enterprise Implications:** Cuts upstream brownout recovery/detection latency by N_pods across HA clusters.
- **Dependencies & Constraints:** Valkey ClusterSync (degrades cleanly to node-local RAM if Valkey is unreachable).
- **Security & Operational Considerations:** Cap canary probe frequency and token budget (max_tokens: 1) so health checks never amplify outage load.

#### `CAP-032` — Wired Per-Route, TTFT, Inter-Chunk & X-Tanpopo-Timeout-Ms Deadlines
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-03 | WF-03`
- **User Problem & Value:** Enforces deterministic request, first-token, and stalled-SSE timeouts so hung upstream streams release worker goroutines and budget holds promptly.
- **Existing Tanpopo RC9 Capability:** ENUM_OR_CONTRACT_ONLY in RC9: route_policies.request_timeout_ms (routes.go:264), X-Tanpopo-Timeout-Ms (dataplane-v1.yaml:81), and failover.Config.RequestDeadline (executor.go:61) are never wired at runtime!
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04
- **Potential Improvements / Target Design:** Load request_timeout_ms, ttft_timeout_ms, and idle_chunk_timeout_ms into snapshot.RoutePolicyView, clamp X-Tanpopo-Timeout-Ms monotonically (<= route ceiling), and enforce pre-first-byte failover on TTFT timeout.
- **Enterprise Implications:** Critical for interactive SLAs and preventing slowloris/hung-upstream goroutine exhaustion.
- **Dependencies & Constraints:** Internal routing/failover & sse.Pump.
- **Security & Operational Considerations:** Abort timed-out upstream sockets immediately and release/settle partial SM-07 holds deterministically.

#### `CAP-033` — Hierarchical Aggregate Tenant Bulkheads & Gradient2 Priority Load Shedding
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `MEDIUM-HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-03, PER-04 | WF-03, WF-12`
- **User Problem & Value:** Stops a runaway batch script using 20 Virtual Keys from starving sibling interactive workloads or blowing through shared org/project TPM quotas.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: internal/ratelimit enforces buckets strictly per Virtual Key; no shared aggregate ORG/PROJECT bucket or priority concurrency shedding.
- **Competitor & Technology Evidence:** Netflix/Envoy Gradient2 adaptive concurrency and shuffle sharding (QAS-ENT-04).
- **Potential Improvements / Target Design:** Add hierarchical Valkey Lua buckets (ORG -> ORG_UNIT -> PROJECT -> VKEY) including max_inflight_input_bytes, Shuffle Sharding (k=2 of N=16), and Gradient2 priority load shedding (MISSION_CRITICAL > INTERACTIVE > BATCH_BACKFILL).
- **Enterprise Implications:** Guarantees multi-tenant noisy-neighbor isolation (<0.5ms p99 delta target, HYPOTHESIS / UNVERIFIED) across hundreds of enterprise teams.
- **Dependencies & Constraints:** Valkey {org_id} hash-tag co-location.
- **Security & Operational Considerations:** Emit standard RateLimit-* / Retry-After headers and per-tier shed counters.

#### `CAP-034` — FinOps-Safe Pre-First-Token Speculative Request Hedging
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (HYPOTHESIS)`
- **Personas & Workflows:** `PER-02, PER-03, PER-07 | WF-03`
- **User Problem & Value:** Eliminates 10x p99 tail-TTFT stalls on mission-critical interactive/voice routes without risking unbudgeted double spend.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (sequential pre-first-byte failover only).
- **Competitor & Technology Evidence:** Commercial LLM APIs exhibit severe p99 queue spikes; standard Envoy hedging lacks token-budget hold awareness (INN-07).
- **Potential Improvements / Target Design:** For opt-in MISSION_CRITICAL routes, if Primary emits 0 bytes by p95_ttft * 1.5, dispatch parallel Hedge ONLY IF Phase-1 atomically reserves hold(Primary) + hold(Hedge); lock winner on byte #1 and cancel loser in <1ms (HYPOTHESIS / UNVERIFIED).
- **Enterprise Implications:** Slashes p99 TTFT for executive/customer-facing voice and realtime agents while preserving 0-overspend FinOps math.
- **Dependencies & Constraints:** CAP-032, CAP-037.
- **Security & Operational Considerations:** Strictly disabled on mutating tool/native endpoints; settle exact prefill if canceled loser already billed tokens.

#### `CAP-035` — Valkey-Accelerated Idempotency-Key Coordination
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-10 | WF-03`
- **User Problem & Value:** Removes the 2-SQL-write-transactions-per-request bottleneck when high-RPS clients supply Idempotency-Key headers.
- **Existing Tanpopo RC9 Capability:** SCALABILITY_BOTTLENECK in internal/ingress/idempotency/guard.go: backed solely by SQL idempotency_records.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04
- **Potential Improvements / Target Design:** Execute atomic SET NX EX / Lua fingerprint locking in Valkey tp:v1:{org_id}:idem:* (falling back to SQLite/PG in Mode A standalone).
- **Enterprise Implications:** Unlocks >10,000 RPS idempotent ingestion in clustered HA deployments (HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** CAP-006, Valkey Cluster.
- **Security & Operational Considerations:** Encrypt cached response bodies in Valkey using Project DEK with bounded payload size (<= 1 MiB).

#### `CAP-036` — Zero-Drop Rolling Upgrade preStop Readiness Fencing & Active SSE Drain
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03 | WF-12`
- **User Problem & Value:** Eliminates dropped requests during Kubernetes / Docker Compose rolling restarts while long-lived 30-90s reasoning SSE streams are open.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: benchmark-evidence.md recorded 1 / 575 (0.17%) dropped requests at LB connection-drain boundary during rolling restart (COND-PO-01).
- **Competitor & Technology Evidence:** Verified in docs/quality/benchmark-evidence.md & TEEP-01-DOC-12
- **Potential Improvements / Target Design:** Flip /readyz to 503 immediately on SIGTERM, hold listener open during configurable preStop propagation delay (--prestop-wait / --drain-timeout CLI flags), drain ActiveStreamRegistry cleanly, and ship reference Envoy/NGINX/Helm drain configs.
- **Enterprise Implications:** Required to meet 99.99% enterprise upgrade availability SLAs (QAS-ENT-10, HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** CAP-107 (Helm chart), CAP-134 (CLI flags).
- **Security & Operational Considerations:** Enforce hard upper drain ceiling (<=300s) so wedged clients cannot block security patch rollouts indefinitely.

### DOM-06 — AI FinOps

#### `CAP-037` — Zero-Float u128 Two-Phase Reserve / DeltaReserve / Settle Kernel & Reaper
- **Proposal Classification:** `KEEP` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-07, PER-03 | WF-06`
- **User Problem & Value:** Prevents runaway agent spend before upstream execution begins using exact 64/128-bit integer arithmetic with zero IEEE-754 float drift.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE in internal/finops/{pricing,ledger} & valkey/budget_hold_lua.go (including DEF-085 pre-dispatch A2A zero-hold block & 90s orphan reaper).
- **Competitor & Technology Evidence:** Major structural moat over LiteLLM, Bifrost, and OpenRouter post-hoc float spend tracking (SRC-005, SRC-011, SRC-013).
- **Potential Improvements / Target Design:** Keep exact SM-07 state machine and u128 ceiling division while scaling hot-path throughput via CAP-038.
- **Enterprise Implications:** Audit-grade financial determinism required by enterprise CFO and FinOps teams.
- **Dependencies & Constraints:** ADR-0005.
- **Security & Operational Considerations:** Preserve negative_carryover_micros accounting for mid-stream token overruns and mandatory ALT-UNMETERED-SPEND alerts.

#### `CAP-038` — Hierarchical Escrow-Backed Local Pod Budget Leases (~30 ns Hot Path, 0 Overspend)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-03, PER-07 | WF-06, WF-12`
- **User Problem & Value:** Unlocks 10,000+ RPS FinOps enforcement by eliminating per-request SQL/network round-trips when budgets have ample headroom, without risking ceiling overruns.
- **Existing Tanpopo RC9 Capability:** SCALABILITY_BOTTLENECK in RC9: synchronous per-request SQL Reserve/Settle caps SQLite at 109.48 req/s and contends on hot PG org ledger rows.
- **Competitor & Technology Evidence:** Grounded in Spanner/CockroachDB escrow quota leasing and PayPal gateway benchmarks (SRC-084; TEEP-01-DOC-09).
- **Potential Improvements / Target Design:** Pods acquire bounded escrow leases (min(\$5, 2% available), TTL=10s) from Valkey/PG and admit via atomic.Int64 (~30 ns); below 10% Low-Water Mark, pods return unused slices and cut over seamlessly to per-request Valkey Lua EVALSHA (INN-06).
- **Enterprise Implications:** Removes FinOps as a throughput bottleneck while mathematically guaranteeing SUM(PodLeases) + Settled <= HardLimitMicros (QAS-ENT-01, QAS-ENT-06, HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** CAP-037, CAP-047.
- **Security & Operational Considerations:** Orphan reaper reclaims crashed-pod LEASE_ESCROW rows within <=15-30s; zero unauthorized overspend.

#### `CAP-039` — 7-Tier Budget Scopes (Enterprise/BU/Team/User), Delete Lifecycle & Burn Forecasting
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-04, PER-07 | WF-06`
- **User Problem & Value:** Lets finance teams cap spend per Business Unit, Department, Team, or individual User/Principal, delete retired budgets, and catch spend spikes before exhaustion.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: supports ORG/PROJECT/ENV/VIRTUAL_KEY only; budgets cannot be deleted (no DELETE /budgets/{id}); UI cannot edit soft_warning_percents after creation.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04 & TEEP-01-DOC-07.
- **Potential Improvements / Target Design:** Extend budget_ledgers scopes to ENTERPRISE, ORG_UNIT, and PRINCIPAL, add DELETE /admin/v1/budgets/{id} + full threshold editing in FinOpsPage.tsx, and emit EWMA burn-velocity forecast alerts (projected exhaustion timestamp).
- **Enterprise Implications:** Matches real enterprise cost-center and per-developer IDE (Claude Code / Cursor) allowance models.
- **Dependencies & Constraints:** CAP-074 (org_units).
- **Security & Operational Considerations:** Deleting or raising a budget requires ActionBudgetWrite + If-Match + JCS audit record.

#### `CAP-040` — Automatic Period Rollover Worker, Date-Range UI & Unified Half-Open Attribution
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-07, PER-03 | WF-06`
- **User Problem & Value:** Prevents DAILY/MONTHLY budgets from silently stopping enforcement after midnight/month-end and eliminates 1-2 micro-USD boundary skews between Budgets and Chargeback.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: ledger/service.go:1257 has zero rollover cron once nowMs >= period_end_ms; finops.go:853 uses closed <= toMs vs :1118 < period_end_ms; cross-midnight streams skew periods (DEF-078); FinOpsPage lacks from/to date pickers.
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-04, TEEP-01-DOC-07, and TEEP-01-DOC-12 (DEF-078).
- **Potential Improvements / Target Design:** Run an idempotent background period-rollover worker minting next period rows (carrying negative_carryover_micros), stamp billing_period_ref_ms on usage_settlements, enforce half-open [fromMs, toMs) everywhere, and add from/to date pickers in FinOpsPage.tsx.
- **Enterprise Implications:** Eliminates month-end reconciliation discrepancies for accounting close.
- **Dependencies & Constraints:** Resolves DEF-078 boundary & rollover gaps.
- **Security & Operational Considerations:** Use UTC or org-configured IANA billing timezone deterministically with advisory/unique constraint idempotency.

#### `CAP-041` — Multi-Tier Conditional Rate Evaluation (1h Cache, Context Cliffs, Service Tiers, GPU)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-07, PER-03 | WF-06`
- **User Problem & Value:** Accurately prices 2026 frontier workloads across 5m vs 1h cache writes, >200k/>272k context cliffs, batch/flex/priority tiers, search queries, and UTC windows.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: calculator.go has a single CacheWritePer1MNanoUSD and zero context-bracket, service-tier, search-query, or UTC-schedule overrides.
- **Competitor & Technology Evidence:** Anthropic 5m (1.25x) vs 1h (2.0x) cache writes (SRC-051), LiteLLM 81 context-tier keys & 1,101 priority keys (SRC-045), OpenRouter pricing.overrides (SRC-047).
- **Potential Improvements / Target Design:** Extend calculator.go & NormalizedUsage to evaluate ordered conditional rate overrides (min_input_tokens, service_tier, cache_ttl, utc_window, search_queries) plus granular GPU/PTU amortization.
- **Enterprise Implications:** Prevents 2x-4x silent budget under-counting on >200k long-context RAG and agent workloads.
- **Dependencies & Constraints:** CAP-016 (RateCard v3).
- **Security & Operational Considerations:** Compute conservative worst-case tier ceiling during Phase-1 Reserve when exact output/search usage is not yet known.

#### `CAP-042` — Wired Streaming / Hourly usage_rollups_hourly Aggregator
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-07, PER-03 | WF-06`
- **User Problem & Value:** Keeps Chargeback dashboards and exports sub-50ms (HYPOTHESIS / UNVERIFIED) over months of multi-million-request history instead of full-scanning raw usage_settlements.
- **Existing Tanpopo RC9 Capability:** ENUM_OR_CONTRACT_ONLY / DEAD CODE: internal/finops/rollup/aggregator.go has 0 callers in serve.go (usage_rollups_hourly has 0 rows; DEF-078) and lacks exact vs estimated counts.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04 & TEEP-01-DOC-12
- **Potential Improvements / Target Design:** Expand usage_rollups_hourly with exact_request_count, estimated_request_count, env_id, virtual_key_id, and cost_center_code; schedule incremental watermarked rollup worker in serve.go; query rollups + tail watermark in /usage/summary & /export.
- **Enterprise Implications:** Prevents FinOps queries from degrading OLTP Postgres performance at 10^8+ settlements.
- **Dependencies & Constraints:** Resolves DEF-078 rollup gap; CAP-040.
- **Security & Operational Considerations:** Use O(k_active_orgs) watermark scanning (fixing ARCH-OBS-01 Loop #17) so idle tenants incur 0 queries.

#### `CAP-043` — Mandatory Cost-Center Tagging, Internal Platform Markup Bps & FOCUS 1.1 Export
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-07, PER-04 | WF-06`
- **User Problem & Value:** Integrates Tanpopo directly into enterprise ERP/Cloud FinOps pipelines (SAP, Snowflake, Vantage, Apptio) with separated raw vendor cost vs internal platform fee.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: CSV/JSON export exists (DEF-077 partial at \$0 fixture), but lacks cost-center tags, platform markup bps, and FinOps FOCUS 1.1 schema.
- **Competitor & Technology Evidence:** FinOps Foundation FOCUS 1.1 specification (SRC-083).
- **Potential Improvements / Target Design:** Resolve effective_cost_center + validated attribution_tags_json per settlement, compute integer platform_fee_micros via basis-points separate from provider_cost_micros, verify non-zero duplicate-name VKey rows (DEF-077), and export FOCUS 1.1 CSV/JSONL/Parquet.
- **Enterprise Implications:** Allows central AI Platform teams to run sustainable internal chargeback/showback programs without custom ETL.
- **Dependencies & Constraints:** Resolves DEF-077; CAP-074 (org_units).
- **Security & Operational Considerations:** Enforce schema/length caps on attribution_tags_json to prevent header-injected storage bloat.

#### `CAP-044` — Cryptographic RFC 8785 JCS FinOps Settlement & Chargeback Invoice Receipts
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (HYPOTHESIS)`
- **Personas & Workflows:** `PER-07, PER-06 | WF-06, WF-14`
- **User Problem & Value:** Eliminates internal BU chargeback disputes by making every monthly AI invoice mathematically verifiable offline down to individual request rate snapshots.
- **Existing Tanpopo RC9 Capability:** MISSING receipt chaining/signing in RC9 (settlement rows are immutable via DB trigger only).
- **Competitor & Technology Evidence:** Unique Tanpopo differentiator over mutable float spend tables in LiteLLM/Bifrost (INN-06).
- **Potential Improvements / Target Design:** Compute JCS digests over settled rows into signed hourly/monthly per-project Merkle rollup receipts verifiable via 'tanpopo finops verify-invoice' (and optional X-Tanpopo-Receipt response trailer).
- **Enterprise Implications:** Provides CFO/external-auditor-grade non-repudiation for regulated chargeback and multi-tenant SaaS billing.
- **Dependencies & Constraints:** RFC 8785 JCS + Ed25519 trust keys.
- **Security & Operational Considerations:** Include zero PII or prompt content inside settlement receipts (pseudonymized IDs + token/micro-USD integers only).

#### `CAP-045` — Out-of-Band Cloud Provider Billing Reconciliation Connectors
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-07 | WF-06`
- **User Problem & Value:** Detects drift between gateway-metered spend and actual vendor invoices (e.g., hosted web_search/storage fees or unproxied shadow keys).
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** OpenAI GET /v1/organization/costs & /usage/* (SRC-048), AWS Cost Explorer, Azure Cost Management, GCP Billing Export.
- **Potential Improvements / Target Design:** Add read-only scheduled reconciliation connectors comparing vendor daily cost buckets against Tanpopo usage_rollups_hourly per connection, alerting on variance > configurable threshold (e.g., >2%).
- **Enterprise Implications:** Closes the loop between engineering proxy telemetry and Accounts Payable vendor bills.
- **Dependencies & Constraints:** CAP-012, CAP-042; BlindHostCredentialBroker.
- **Security & Operational Considerations:** Use read-only billing API scopes sealed in Vault; never grant write/admin permissions to reconciliation credentials.

### DOM-07 — Caching and Performance

#### `CAP-046` — Epoch-Fenced SSRF-Safe Pooled Keep-Alive & HTTP/2 Upstream Transports
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-02 | WF-03, WF-12`
- **User Problem & Value:** Removes the #1 network bottleneck in RC9 (per-request TCP + TLS 1.3 handshakes and TIME_WAIT port exhaustion) without weakening SSRF or revocation safety.
- **Existing Tanpopo RC9 Capability:** SCALABILITY_BOTTLENECK in internal/security/broker/blind_broker.go:876,885: allocates a new http.Transport with DisableKeepAlives: true on every single request!
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-09 & benchmark-evidence.md (109.48 req/s SQLite; 56.86 req/s Docker HA) vs Bifrost/Envoy/PayPal >2,200 RPS pooled transports (SRC-004, SRC-084).
- **Potential Improvements / Target Design:** Maintain an LRU pool of HTTP/1.1 Keep-Alive & HTTP/2 transports keyed by (org_id, conn_id, policy_hash, origin); verify SSRFGuard on dial + ~200ns pre-write ConfigSnapshot & peer netip.Addr check on idle socket reuse; call CloseIdleConnections() on revocation.
- **Enterprise Implications:** Mandatory prerequisite to scale from ~100 RPS to 5,000-20,000+ RPS (QAS-ENT-01, HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** TP-IMP-0010 SSRF & revocation invariants.
- **Security & Operational Considerations:** Zero cross-connection or cross-tenant socket sharing; instant socket eviction on secret/connection/kill-switch change.

#### `CAP-047` — Split SQLite Read/Write Pools & 5 ms Group-Commit WAL / pgx.CopyFrom Batcher
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-03 | WF-06, WF-12`
- **User Problem & Value:** Multiplies standalone SQLite and clustered PostgreSQL write throughput by amortizing disk fsync across hundreds of concurrent settlements and traces.
- **Existing Tanpopo RC9 Capability:** SCALABILITY_BOTTLENECK in internal/storage/sqlite/store.go:104 (SetMaxOpenConns(1) serializes all reads & writes) and synchronous per-row PG INSERTs.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04 & TEEP-01-DOC-09
- **Potential Improvements / Target Design:** Split SQLite into readerDB (MaxOpenConns=NumCPU, ?mode=ro) and writerDB (1), and batch usage_settlements + request_traces_meta via a 5ms / 256-item GroupCommitBatcher (BEGIN IMMEDIATE on SQLite; pgx.CopyFrom/UNNEST on PG16).
- **Enterprise Implications:** Unlocks >=1,500 req/s standalone SQLite target (QAS-ENT-02) and >=10,000 req/s PG16 cluster target (QAS-ENT-01) (HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** CAP-038 (local budget leases).
- **Security & Operational Considerations:** Flush pending micro-batches synchronously on graceful SIGTERM shutdown so 0 settled records are lost.

#### `CAP-048` — Incremental ConfigSnapshot Epoch Delta Reloads
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03 | WF-12`
- **User Problem & Value:** Eliminates the O(N_orgs) PostgreSQL query storm across every Data-Plane pod whenever any single Virtual Key, route, or connection changes.
- **Existing Tanpopo RC9 Capability:** SCALABILITY_BOTTLENECK in internal/config/snapshot/manager.go:857: executes 2 + 7 * N_orgs sequential RLS queries on every config_epoch bump (700k queries across 10 pods at 10k orgs!).
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-04 & TEEP-01-DOC-12 (ARCH-OBS-01).
- **Potential Improvements / Target Design:** Read mutated (org_id, entity_kind, entity_id) deltas from config_changelog WHERE config_epoch > last_epoch and clone/patch only affected tenant slices in ConfigSnapshot (retaining periodic checksum verification + signed snapshot bundles for cold boot).
- **Enterprise Implications:** Reduces 10,000-org config propagation from ~17.5s to <25ms (QAS-ENT-09, HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** Resolves ARCH-OBS-01 Loop #5.
- **Security & Operational Considerations:** Fall back to full reload safely if a node lags behind compacted changelog retention.

#### `CAP-049` — Two-Tier (RAM + Valkey) Project-DEK Exact Cache, X-Tanpopo-No-Cache & Policy UI
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-03, PER-07 | WF-03`
- **User Problem & Value:** Delivers sub-millisecond deterministic response caching across chat, responses, and embeddings, and makes caching configurable from the Web Console.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION in internal/ingress/cache/exact_cache.go: SQL-only (response_cache_entries), chat stream=false temp=0 only, EvaluateEligibility (:118 -> hasNoStoreCacheControl :389) ignores X-Tanpopo-No-Cache, and omitted from RoutingPage.tsx!
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04 & TEEP-01-DOC-07.
- **Potential Improvements / Target Design:** Add bounded L1 in-RAM TinyLFU + L2 Valkey {org_id} cache tiers (keeping Project DEK AES-256-GCM sealing!), honor X-Tanpopo-No-Cache, extend to /v1/responses & /v1/embeddings, and expose response_cache_* controls in RoutingPage.tsx.
- **Enterprise Implications:** Cuts latency to <1ms (HYPOTHESIS / UNVERIFIED) and Upstream Cost to \$0 on repeated deterministic classification, extraction, and CI eval prompts.
- **Dependencies & Constraints:** Vault Project DEK (FMEA-03).
- **Security & Operational Considerations:** Cache key includes (org_id, project_id, env_id, policy_digest, model_deployment, tools_hash, JCS(req)); instant invalidation on Project DEK shred.

#### `CAP-050` — Cryptographically Tenant-Prefixed Semantic Cache (CACHE_STRATEGY)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-02, PER-07 | WF-03, WF-06`
- **User Problem & Value:** Caches semantically equivalent natural-language queries while mathematically preventing cross-tenant or cross-policy cache poisoning/leakage.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (0 semantic cache).
- **Competitor & Technology Evidence:** Supported in LiteLLM (Qdrant/Redis), Bifrost (Weaviate), Kong (ai-semantic-cache), and Apigee, often without per-project cryptographic envelope isolation (SRC-005, SRC-011, SRC-014).
- **Potential Improvements / Target Design:** Support opt-in Tier-2/Tier-3 CACHE_STRATEGY plugins (local quantized ONNX embedding + Valkey Vector / pgvector HNSW) where the Host forcibly namespaces indexes by (org_id, project_id, env_id, policy_rev, model_family, tools_hash) and seals payloads under Project DEK.
- **Enterprise Implications:** High-value FinOps saver for high-volume FAQ/support bots under strict tenant isolation.
- **Dependencies & Constraints:** CAP-112 (CACHE_STRATEGY); Vault Project DEK.
- **Security & Operational Considerations:** Default FAIL_OPEN_CACHE_MISS on sidecar timeout; bypass semantic cache automatically when tools or per-user context are present unless explicitly opted in.

#### `CAP-051` — Automatic Upstream Prompt-Cache Breakpoint & Prefix Canonicalizer
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-07 | WF-03, WF-06`
- **User Problem & Value:** Maximizes 75-97.5% vendor prompt-cache hit rates automatically without requiring application developers to hand-tune Anthropic cache_control or Bedrock cachePoint blocks.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (manual native passthrough only).
- **Competitor & Technology Evidence:** Anthropic 4-slot 5m/1h cache_control + 20-block lookback (SRC-051), Bedrock cachePoint (SRC-059), OpenAI prompt_cache_retention (SRC-048).
- **Potential Improvements / Target Design:** Canonicalize JSON key ordering in tools/system arrays so hashes stay deterministic, and optionally auto-inject vendor breakpoints (e.g., Anthropic top-level/4-slot cache_control or Bedrock cachePoint) when token thresholds (>=512/1024) are met.
- **Enterprise Implications:** Captures massive prompt-cache savings for legacy OpenAI-SDK apps routed to Claude or Bedrock.
- **Dependencies & Constraints:** CAP-024 (Prefix Affinity Router), CAP-041.
- **Security & Operational Considerations:** Never inject 1h (2.0x write) TTL surcharges unless route/policy explicitly enables extended cache retention.

#### `CAP-052` — Zero-Allocation Hot-Path Arena & Slice Pooling
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03 | WF-12`
- **User Problem & Value:** Reduces Go garbage-collection tail-latency spikes when sustaining 10,000+ RPS alongside thousands of concurrent SSE streams.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: Policy eval is 0 allocs/op, while ResolveRoute (95 allocs/op, 11.4 KiB) and Declarative transform (88 allocs/op) allocate per request (benchmark-evidence.md).
- **Competitor & Technology Evidence:** Bifrost and Solo agentgateway emphasize zero-alloc request paths for low p99 jitter (SRC-011, SRC-019).
- **Potential Improvements / Target Design:** Pool request-scoped scratch slices/buffers via sync.Pool (zeroized on put via clear()) and replace hot-path map allocations in Resolver and Declarative engine with pre-indexed flat slices.
- **Enterprise Implications:** Keeps internal gateway p99 overhead <8.0ms under heavy multi-core load (QAS-ENT-01, HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** Go 1.24+ stdlib sync.Pool.
- **Security & Operational Considerations:** Mandatory clear(buf) before returning any buffer to sync.Pool to uphold zero cross-request memory residue.

### DOM-08 — Observability

#### `CAP-053` — Privacy-First 12-Stage Request Waterfall Metadata Tracing
- **Proposal Classification:** `KEEP` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-03, PER-06 | WF-09`
- **User Problem & Value:** Provides per-stage latency attribution (auth, policy, hold, Wasm transform, TTFT, stream, settle) while storing 0 bytes of sensitive prompts by default.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE in internal/telemetry/otel/tracer.go and TracesPage.tsx.
- **Competitor & Technology Evidence:** Architecturally ahead of observability gateways that require storing full prompt payloads in ClickHouse/SaaS to render traces (SRC-020..023).
- **Potential Improvements / Target Design:** Keep zero-content default and enrich waterfall spans with cache hit tier, retry reason, guardrail stage duration, and causal_root_id.
- **Enterprise Implications:** Satisfies DPO/CISO zero-PII telemetry mandates out of the box.
- **Dependencies & Constraints:** CAP-047 (batch trace writer), CAP-057 (TTL cleaner).
- **Security & Operational Considerations:** Enforce privacy/redactor.go on all error messages written to request_traces_meta.

#### `CAP-054` — Server-Side Multi-Dimension Trace Search, Filtering & Metadata Export
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-03 | WF-09`
- **User Problem & Value:** Allows SREs and developers to filter millions of traces accurately by HTTP status, error code, Virtual Key, route, usage accuracy, or mid-stream abort.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: TracesPage.tsx:105 filters error_code client-side over <=200 loaded rows only, and omits status, key, route, accuracy, and export controls (SCR-11 gap).
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04/07.
- **Potential Improvements / Target Design:** Add indexed server-side query parameters on GET /admin/v1/traces (status, error_code, virtual_key_id, route_slug, usage_accuracy, mid_stream_abort, min_duration_ms, from, to), streaming CSV/JSONL export, and 'Simulate in What-If / Route Explain' deep links.
- **Enterprise Implications:** Essential for triaging intermittent 0.1% production errors across large clusters.
- **Dependencies & Constraints:** Composite partition-pruned indexes on request_traces_meta.
- **Security & Operational Considerations:** HMAC-signed pagination cursors bound to org_id and filter hash prevent cross-tenant cursor tampering.

#### `CAP-055` — Authenticated Bounded-Cardinality Prometheus /metrics & Scrape CLI Flags
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03 | WF-09, WF-12`
- **User Problem & Value:** Exposes golden RED/USE gateway, breaker, FinOps, Valkey, and Wasm metrics safely to enterprise Prometheus/VictoriaMetrics scrapers.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: internal/telemetry/otel/metrics.go enforces constant-time bearer auth & label allowlist, but TANPOPO_METRICS_SCRAPE_TOKEN is missing from serve --help and docs (PG-009).
- **Competitor & Technology Evidence:** Verified in QA-005 PG-009 & TEEP-01-DOC-04/07
- **Potential Improvements / Target Design:** Add --metrics-scrape-token and --metrics-include-tenant-labels flags to tanpopo serve, document scrape configs, and add Valkey fallback, budget-lease, and SSRF-reject counters.
- **Enterprise Implications:** Plug-and-play integration with standard Kubernetes ServiceMonitor / Grafana stacks.
- **Dependencies & Constraints:** Resolves PG-009.
- **Security & Operational Considerations:** Keep startup panic/gate forbidding unbounded high-cardinality labels (request_id, virtual_key_id, prompt).

#### `CAP-056` — Guarded OTLP GenAI (semantic-conventions-genai) Exporter & CIDR Flag Parity
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-03 | WF-09`
- **User Problem & Value:** Streams vendor-neutral OpenTelemetry traces, metrics, and logs to Arize Phoenix, Langfuse, Grafana Tempo, Dynatrace, or Datadog without Day-1 ClickHouse lock-in.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: OTLP HTTP exporter with <=32 MiB ring buffers exists, but lacks --otlp-allowed-cidrs CLI flag (PG-008) and latest Nov 2026 gen_ai.* attribute mapping.
- **Competitor & Technology Evidence:** open-telemetry/semantic-conventions-genai split from core v1.42.0 June 2026 (268 development items; gen_ai.provider.name, cache_write.input_tokens, NO_CONTENT default) (SRC-077).
- **Potential Improvements / Target Design:** Add --otlp-allowed-cidrs flag + docs (resolving PG-008), support OTLP/gRPC alongside OTLP/HTTP, and translate Tanpopo's stable internal span model outward to gen_ai.* + tanpopo.* attributes.
- **Enterprise Implications:** Delivers 'Zero-ClickHouse Day-1, Zero-Lock-In Day-100' interoperability with enterprise APM suites.
- **Dependencies & Constraints:** Resolves PG-008.
- **Security & Operational Considerations:** Keep NO_CONTENT default on OTLP spans; route all collector egress through SSRFGuardDialer.

#### `CAP-057` — Wired ttl.Cleaner, Instant PostgreSQL Partition Drop & Retention Settings API/UI
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-06 | WF-09, WF-12, WF-14`
- **User Problem & Value:** Prevents unbounded disk growth and enforces organizational data-retention compliance automatically.
- **Existing Tanpopo RC9 Capability:** UNWIRED / 404 IN RC9: internal/telemetry/ttl/cleaner.go and PartitionManager.DetachAndDropExpired have 0 production callers; PUT /admin/v1/settings/retention returns 404; /settings/residency-retention renders Webhooks!
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04 & TEEP-01-DOC-07.
- **Potential Improvements / Target Design:** Start ttl.Cleaner in serve.go, wire PartitionManager.DetachAndDropExpired for O(1) expired monthly PG partition drops, implement GET/PUT /admin/v1/settings/retention, and restore dedicated Retention UI accessible to ORG_OWNER & OPERATOR.
- **Enterprise Implications:** Mandatory for both disk stability at high RPS and GDPR / EU AI Act (>=180d floor) retention governance.
- **Dependencies & Constraints:** CAP-078 (audit_partition_seals before detaching audit partitions).
- **Security & Operational Considerations:** Enforce compliance-profile minimum/maximum retention bounds (e.g., debug blobs hard-capped <=72h).

#### `CAP-058` — Asynchronous Analytical Warehouse Offload (ClickHouse / S3 Parquet / Iceberg)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-03, PER-07 | WF-06, WF-09`
- **User Problem & Value:** Scales trace and financial analytics to 800M+ requests/day without bloating OLTP PostgreSQL B-tree indexes or autovacuum.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (100% of traces written to SQLite/PG).
- **Competitor & Technology Evidence:** Langfuse v4 and PayPal's enterprise gateway separate OLTP control state from columnar analytical sinks (SRC-020, SRC-084).
- **Potential Improvements / Target Design:** Support configurable OLTP trace sampling (e.g., 100% errors + 1% healthy in PG) paired with 100% async streaming via TELEMETRY_EXPORTER plugins to ClickHouse, S3 Parquet / Apache Iceberg, or BigQuery/Snowflake.
- **Enterprise Implications:** Lets enterprises scale to tens of thousands of RPS while keeping Day-1 deployments 100% ClickHouse-free.
- **Dependencies & Constraints:** CAP-112 (TELEMETRY_EXPORTER).
- **Security & Operational Considerations:** Out-of-hot-path ring buffer with DROP_OLDEST_WITH_METRIC backpressure so analytical sink outages never affect /v1/* latency.

#### `CAP-059` — Real-Time Authenticated 'tanpopo traces tail' & Live Anomaly Stream
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (HYPOTHESIS)`
- **Personas & Workflows:** `PER-02, PER-03 | WF-09`
- **User Problem & Value:** Gives developers and SREs instant terminal/UI visibility into live request flows, failovers, and guardrail blocks during deployments or incidents.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** High-love developer workflow in modern cloud gateways (Cloudflare tail, Stripe listen).
- **Potential Improvements / Target Design:** Expose RBAC-scoped SSE endpoint GET /admin/v1/traces/stream + 'tanpopo traces tail --route ... --errors-only' tapping the in-memory telemetry ring buffer with zero DB read overhead.
- **Enterprise Implications:** Accelerates live incident triage and new-application integration testing.
- **Dependencies & Constraints:** CAP-073 (CLI auth).
- **Security & Operational Considerations:** Strictly metadata-only (redacted) stream scoped by tenant RBAC and auto-terminated on session/PAT revocation.

### DOM-09 — AI Security and Guardrails

#### `CAP-060` — Wired Multi-Stage Guardrail Engine (PRE, STREAM_WINDOW, POST, MCP, A2A) + Speculative Input Rails
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-06, PER-05, PER-03 | WF-03, WF-07, WF-10`
- **User Problem & Value:** Activates real inline payload safety across prompts, streaming SSE chunks, MCP tool calls, and A2A tasks while masking input-rail latency.
- **Existing Tanpopo RC9 Capability:** ENUM_OR_CONTRACT_ONLY in RC9: EffectivePolicy.GuardrailHooks (merge.go:226) and EvaluateGuardrail (port/runtime.go:349) are never called in internal/ingress/*!
- **Competitor & Technology Evidence:** NVIDIA NeMo v0.24 IORails proved speculative parallel input execution masks TTFT penalty, while #2152 showed streaming rails must fail closed on errors (SRC-027).
- **Potential Improvements / Target Design:** Wire EvaluateGuardrail across PRE_REQUEST (supporting parallel speculative upstream dispatch holding byte #1 until ALLOW), STREAM_WINDOW (128-512B UTF-8 sliding holdback), POST_RESPONSE, MCP_TOOL_CALL/RESULT, and A2A_TASK.
- **Enterprise Implications:** Transforms Tanpopo from a transport/FinOps gateway into an active inline AI security firewall.
- **Dependencies & Constraints:** CAP-112 (GUARDRAIL_PROCESSOR); ancestor FAIL_CLOSED rules un-overridable by child scopes.
- **Security & Operational Considerations:** If speculative input rail returns BLOCK, immediately cancel in-flight upstream request before byte #1 reaches caller and audit verdict.

#### `CAP-061` — Built-In Pure-Go/Wasm Deterministic PII, Secret & GlassWorm Invisible-Unicode Guardrails
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-05, PER-06 | WF-03, WF-07`
- **User Problem & Value:** Provides <1ms (HYPOTHESIS / UNVERIFIED) out-of-the-box protection against leaked API keys/JWTs, SSN/CC/IBAN/email PII, and invisible Unicode prompt-injection smuggling with zero Python/GPU dependencies.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (0 built-in content/PII/Unicode guardrails).
- **Competitor & Technology Evidence:** protectai/llm-guard was archived 2026-07-09 (SRC-025); GlassWorm used invisible Unicode variation selectors/tags (U+E0000..E007F) to smuggle malicious payloads (SRC-041).
- **Potential Improvements / Target Design:** Bundle signed @tanpopo/guardrails-core (RE2 linear-time regex + Luhn/IBAN checksums + Shannon entropy secret detector + Unicode tag/bidi stripper) running in <1ms (HYPOTHESIS / UNVERIFIED) inside Core/wazero.
- **Enterprise Implications:** Gives every self-hosted and air-gapped deployment immediate baseline DLP without deploying 4 GiB Python containers.
- **Dependencies & Constraints:** CAP-060; linear-time regexp (0 catastrophic backtracking).
- **Security & Operational Considerations:** Support BLOCK, REDACT_PLACEHOLDER, and SHADOW_AUDIT actions with zero raw PII logged in audit_events.

#### `CAP-062` — Tier-3 / External Neural Safety Connectors (ONNX GLiNER, NeMo, Bedrock, Azure, ModelArmor, Lakera)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-06, PER-03 | WF-03, WF-14`
- **User Problem & Value:** Connects enterprise neural classifiers for nuanced indirect prompt injection, jailbreak, contextual grounding, and multilingual NER PII detection.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Enterprises standardize on AWS Bedrock ApplyGuardrail, Azure Prompt Shield, GCP ModelArmor, Lakera Guard v2, F5 AI Guardrails, NeMo NIMs, or local ONNX GLiNER/PromptGuard 2 (SRC-024..030, SRC-059).
- **Potential Improvements / Target Design:** Ship signed GUARDRAIL_PROCESSOR .tpkg connectors invoking external APIs via BlindHostCredentialBroker or local air-gapped ONNX Tier-3 sidecars over 0600 UDS / SPIFFE mTLS.
- **Enterprise Implications:** Lets enterprises compose fast <1ms deterministic Tier-1/2 pre-filters ahead of selective neural Tier-3 checks.
- **Dependencies & Constraints:** CAP-060, CAP-022 (flag Llama Guard 4 EU license restriction!), CAP-113.
- **Security & Operational Considerations:** Enforce strict per-rail timeout (<=50ms local / <=250ms remote) and explicit FAIL_CLOSED vs FAIL_OPEN_LOG posture.

#### `CAP-063` — Indirect Tool-Output Injection Scanner & Vendor Remote-MCP Bypass Firewall
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-05, PER-06, PER-08 | WF-07, WF-08`
- **User Problem & Value:** Stops untrusted web pages, emails, or MCP tool results from hijacking agent goals, and blocks cloud-provider hosted MCP fields from bypassing Tanpopo /mcp.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (OpenAI remote mcp tools and Anthropic mcp_servers fields pass through uninspected on native/responses).
- **Competitor & Technology Evidence:** Anthropic docs warn MCP Connector is NOT ZDR/HIPAA eligible (SRC-051); Solo agentgateway v1.7 added ToolOutput prompt-guard scoping (SRC-019).
- **Potential Improvements / Target Design:** Inspect role:'tool' / MCP CallToolResult payloads before LLM context ingestion, and enforce policy blocking or rewriting upstream mcp_servers / remote mcp tool declarations unless routed back through Tanpopo /mcp.
- **Enterprise Implications:** Closes a critical enterprise shadow-MCP data exfiltration loophole.
- **Dependencies & Constraints:** DEC-003 (strict 4-plane governance), CAP-060.
- **Security & Operational Considerations:** Fail closed 403 scope_or_plane_denied when unapproved remote tool connectors appear in inference payloads.

#### `CAP-064` — BlindHostCredentialBroker Edge Injection, DEC-004 Cookie Ban & Stream Secret Scrubber
- **Proposal Classification:** `KEEP` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-05, PER-06, PER-03 | WF-02, WF-03, WF-07`
- **User Problem & Value:** Ensures Wasm extensions and LLM contexts never touch raw provider secrets, bans consumer-session cookie scraping, and scrubs accidental upstream secret reflections.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE in internal/security/broker/blind_broker.go (slot_ref_* handles, DEC-004 422 Cookie/Set-Cookie ban, chunk-boundary secret scrubber, post-call zeroization).
- **Competitor & Technology Evidence:** Directly prevents the crown-jewel secret thefts suffered by LiteLLM (1.82.7/1.107.0) and Bifrost/VS Code plugins (SRC-006..008, SRC-041).
- **Potential Improvements / Target Design:** Keep 100% of security invariants while adding epoch-fenced keep-alive pooling (CAP-046) and RFC 8693 OBO token exchange (CAP-087).
- **Enterprise Implications:** Tanpopo's #1 CISO architectural differentiator.
- **Dependencies & Constraints:** ADR-0002, CR-05, DEC-004.
- **Security & Operational Considerations:** Maintain explicit PolicyResolver requirement before unsealing any Vault secret.

#### `CAP-065` — Connect-Time SSRFGuardDialer IP Pinning, IMDS Block & Socket Rejection Telemetry
- **Proposal Classification:** `KEEP` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-05, PER-06, PER-03 | WF-02, WF-07, WF-11`
- **User Problem & Value:** Defeats DNS-rebinding and redirect SSRF attacks against cloud IMDS (169.254.169.254 / fd00:ec2::254), loopback, and RFC1918 networks across all outbound planes.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE in internal/security/egress/ssrf_dialer.go (RR-022 partial on socket-level redirect counter).
- **Competitor & Technology Evidence:** Prevents vulnerabilities like IBM ContextForge CVE-2026-110194 and Grafana go-plugin SSRF bypasses (SRC-020B, SRC-042).
- **Potential Improvements / Target Design:** Keep connect-time net.Dialer.Control IP verification across Broker, OIDC, OAuth, CIMD, Webhooks, and OTLP, and emit structured SSRF block counters/diagnostics (closing RR-022).
- **Enterprise Implications:** Mandatory zero-trust egress control for multi-tenant VPC and Kubernetes clusters.
- **Dependencies & Constraints:** CR-10, RR-022.
- **Security & Operational Considerations:** Keep 169.254.0.0/16 and cloud metadata IPs unconditionally blocked even when allow_private_ip=true for local Ollama/vLLM.

#### `CAP-066` — Single-Transaction O(1) 4-Tier Kill-Switch, Global Quarantine & Direct Valkey Broadcast
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-05, PER-06, PER-03 | WF-10`
- **User Problem & Value:** Severs compromised keys, projects, orgs, or plugins across the entire cluster in milliseconds without stalling or splitting transactions at 10,000+ orgs.
- **Existing Tanpopo RC9 Capability:** PARTIAL / SCALABILITY_BOTTLENECK in admin/http/extensions.go:817: splits TX1 (killswitch) + TX2 (installer), runs O(N_orgs) webhook scan twice, risks advisory lock inversion, and omits direct Valkey publishChange!
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-12 (ARCH-OBS-01 Q4).
- **Potential Improvements / Target Design:** Collapse global quarantine into 1 atomic SQL transaction, move tenant webhook fan-out to an async SYSTEM outbox worker, add direct Emergency Valkey publishChange in extensions.go, and test REVOKED principal quarantine.
- **Enterprise Implications:** Guarantees <50ms containment commit and <1s cluster stream severing at 10,000+ orgs (QAS-ENT-09, HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** Resolves ARCH-OBS-01 Q4 & RC9 Revoked-Principal residual risk.
- **Security & Operational Considerations:** Enforce deterministic global -> org lock ordering and bypass per-org outbox cap for priority containment events.

#### `CAP-067` — 3-Tier Envelope Vault, Wired Cloud KMS BYOK & RLS-Safe KEK Rotation/Retirement
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-06 | WF-12`
- **User Problem & Value:** Provides hardware/cloud-KMS root key protection, per-project cryptographic blast-radius isolation, and safe online Master KEK rotation on PostgreSQL HA.
- **Existing Tanpopo RC9 Capability:** CRITICAL GAPS IN RC9: (1) vault/kms (AWS/GCP/Vault Transit) has 0 imports in serve.go:346; (2) handleRetireMasterKEK (ops_lifecycle.go:196) hits PG FORCE RLS 0-count bug bypassing 409 check!; (3) TANPOPO_MASTER_KEY_PREVIOUS mislabels IDs as kek_prev_0; (4) RetireKEK is single-process only.
- **Competitor & Technology Evidence:** Uncovered in TEEP-01-DOC-04 & TEEP-01-DOC-12 (DEF-041, DEF-072, COND-PO-02).
- **Potential Improvements / Target Design:** Fix handleRetireMasterKEK via RLS-safe SECURITY DEFINER count + cluster-wide KEY_RETIRE epoch scrub, parse <id>=<key> in TANPOPO_MASTER_KEY_PREVIOUS, wire vault/kms RoutingProvider in serve.go & CLI (+ OpenBao & T3 Azure KV / PKCS#11 HSM), and batch RewrapAllOrgDEKs.
- **Enterprise Implications:** Unblocks enterprise BYOK compliance and eliminates cold-backup resurrection when external KMS keys are destroyed.
- **Dependencies & Constraints:** Resolves DEF-072 KEK bugs, DEF-041 KMS gap, and COND-PO-02.
- **Security & Operational Considerations:** Zeroize retired KEK bytes across all CP and DP pods and verify loaded_kek_ids_json convergence before confirming retirement.

### DOM-10 — Identity and Access Management

#### `CAP-068` — Persistent HA Admin Sessions, WebAuthn/TOTP MFA, O(1) Login Directory & SYSTEM Operator Decoupling
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-05 | WF-05, WF-12`
- **User Problem & Value:** Enables multi-replica Control-Plane HA without sticky sessions, protects admin accounts with phishing-resistant MFA, and eliminates O(N_orgs) login/auth scans.
- **Existing Tanpopo RC9 Capability:** PARTIAL / SCALABILITY_BOTTLENECK: Admin sessions live in process RAM only (auth/local.go); LoginForOrg & loadBindings run 2*N_orgs SQL loops (ARCH-OBS-01 #1,#2); bootstrap operator bound to org_1; 0 MFA; requires_password_rotation ignored in UI.
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-04, TEEP-01-DOC-07, and TEEP-01-DOC-12 (ARCH-OBS-01).
- **Potential Improvements / Target Design:** Persist SHA-256 hashed Admin sessions in SQL + Valkey cache, decouple org_id IS NULL SYSTEM operators from org_1 with O(1) loadBindings, add blind HMAC iam_login_directory + org_slug login field, enforce password rotation UI, and add WebAuthn/Passkeys + TOTP MFA + active-session revocation UI.
- **Enterprise Implications:** Foundational prerequisite for active-active Control-Plane HA and SOC 2 / PCI-DSS admin MFA.
- **Dependencies & Constraints:** Resolves ARCH-OBS-01 Loops #1-#4 and DEF-041 org_1 deletion deadlock.
- **Security & Operational Considerations:** Keep Argon2id (64 MiB) semaphore cap, __Host- Secure SameSite=Strict cookies, and constant-time X-CSRF-Token checks.

#### `CAP-069` — Multi-Tenant OIDC & Hardened Pure-Go SAML 2.0 Enterprise SSO
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-05, PER-03 | WF-05`
- **User Problem & Value:** Lets enterprise organizations authenticate workforce users via Okta, Microsoft Entra ID, PingFederate, Google Workspace, or ADFS without open-core paywalls.
- **Existing Tanpopo RC9 Capability:** UNWIRED / MISSING IN RC9: internal/iam/oidc exists in code, but ServerConfig.OIDC is nil in serve.go:1008, single-IdP only, missing from LoginPage.tsx; SAML 2.0 is completely absent.
- **Competitor & Technology Evidence:** LiteLLM, Bifrost, Kong, Langfuse, and Agent Router all paywall SSO behind commercial Enterprise tiers (SRC-005..020).
- **Potential Improvements / Target Design:** Wire multi-tenant per-org OIDC configurations + pure-Go SAML 2.0 SP (crewjam/saml + goxmldsig, CGO_ENABLED=0) with LoginPage SSO discovery, IdP-to-RBAC group mapping UI, and optional enforce-SSO-only lock.
- **Enterprise Implications:** Unlocks immediate enterprise security sign-off for the 35-40% of regulated estates still requiring SAML 2.0 alongside OIDC.
- **Dependencies & Constraints:** BSD-2-Clause / Apache-2.0 pure-Go libraries; DEC-001.
- **Security & Operational Considerations:** Enforce strict XML pre-parser (0 DOCTYPE/XXE/NameID comments), signed-DOM XSW 1-8 defense, and Valkey Assertion.ID replay cache.

#### `CAP-070` — SCIM 2.0 Automated Provisioning & <5s Cascading Offboarding Kill-Chain
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-05, PER-06 | WF-05`
- **User Problem & Value:** Closes OIDC/SAML JIT's dangerous offboarding gap by terminating sessions, PATs, MCP OAuth grants, and live SSE streams within seconds of IdP deprovisioning.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (terminated employees retain active cookies/keys until passive TTL expiry).
- **Competitor & Technology Evidence:** RFC 7643/7644 SCIM 2.0; pure-Go elimity-com/scim (MIT) (SRC-074, SRC-082).
- **Potential Improvements / Target Design:** Mount /admin/v1/scim/v2/{Users,Groups} behind scim_tp_* tokens with Entra/Okta quirk normalization; on active=false or DELETE, atomically suspend principal, revoke sessions/PATs/MCP OAuth grants, apply org key-transfer policy, and sever streams via Valkey Pub/Sub (<5s target, QAS-ENT-03, HYPOTHESIS / UNVERIFIED).
- **Enterprise Implications:** Top-3 IAM audit checklist requirement for Fortune 500 procurement.
- **Dependencies & Constraints:** CAP-068, CAP-073, CAP-087.
- **Security & Operational Considerations:** Isolate scim_tp_* tokens strictly to SCIM endpoints (AUTH-07) and record principal.scim_deprovisioned in JCS audit log.

#### `CAP-071` — Two-Stage Policy-as-Code (Native + cedar-go ABAC) & Policy Delete / Reset-to-Parent
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-05, PER-06, PER-04 | WF-03, WF-05`
- **User Problem & Value:** Empowers security teams to express rich attribute-based rules (groups, headers, hours, workload trust domains, tool risk) while preserving unbreakable parent ceilings.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: native structural merge works (256.2 ns/op), but lacks expressive ABAC predicates and lacks GET /policies & DELETE /policies/{id} reset-to-parent!
- **Competitor & Technology Evidence:** AWS Bedrock AgentCore (GA Mar 2026) and Kong AI GW 2.3 adopted Cedar (cedar-policy/cedar-go v1.9+, Apache-2.0, pure Go) (SRC-014, SRC-040, SRC-082).
- **Potential Improvements / Target Design:** Keep native structural kernel as Stage 1 (~250-500ns) and compile schema-validated Cedar policies into ConfigSnapshot as Stage 2 (~1-5us HYPOTHESIS / UNVERIFIED), plus add GET/DELETE /admin/v1/policies/{id} in API & RoutingPage.tsx.
- **Enterprise Implications:** Cedar's mathematical 'forbid overrides permit' semantics guarantees delegated BU/Team admins can never weaken org security policies.
- **Dependencies & Constraints:** cedar-policy/cedar-go (Apache-2.0, CGO_ENABLED=0).
- **Security & Operational Considerations:** Non-Turing-complete evaluation prevents policy infinite-loop DoS; validate all Cedar policies against What-If Replay (CAP-152).

#### `CAP-072` — Secretless Inbound Workload Identity Federation (K8s SA, SPIFFE/SPIRE, Cloud/CI OIDC) & /v1/sts/token
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-05, PER-10 | WF-05, WF-13`
- **User Problem & Value:** Eliminates leaked static vk_tp_* API keys in Kubernetes Secrets, .env files, and CI pipelines via cryptographic workload attestation.
- **Existing Tanpopo RC9 Capability:** MISSING inbound WIF on Data Plane in RC9 (static vk_tp_* bearer keys only).
- **Competitor & Technology Evidence:** SPIFFE/SPIRE (spiffe/go-spiffe/v2, Apache-2.0), K8s Projected ServiceAccount OIDC, GitHub Actions OIDC (SRC-074, SRC-082).
- **Potential Improvements / Target Design:** Add workload_identity_bindings matching (issuer, subject, custom claims) to (org, project, env, policy, budget) for direct JWT / mTLS X.509-SVID presentation OR /v1/sts/token exchange minting 5-minute wst_tp_* session tokens (<500ns O(1) lookup, QAS-ENT-05 HYPOTHESIS / UNVERIFIED).
- **Enterprise Implications:** Eliminates secret-rotation toil across thousands of microservices and CI eval jobs.
- **Dependencies & Constraints:** SSRFGuardDialer + stale-while-revalidate JWKS/Bundle cache.
- **Security & Operational Considerations:** Enforce strict aud == 'tanpopo-dataplane' and short token exp bounds; bind X.509-SVIDs to TLS peer certificates.

#### `CAP-073` — Scoped Admin Service Accounts (sak_tp_*) & Personal Access Tokens (pat_tp_*)
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-02, PER-10 | WF-05, WF-13`
- **User Problem & Value:** Unblocks the broken tanpopo remote CLI (DEF-004), CI/CD automation, and Terraform/OpenTofu without browser cookie scraping.
- **Existing Tanpopo RC9 Capability:** BROKEN / UNWIRED IN RC9: ServerConfig.TokenVerifier is nil in cmd/tanpopo/serve.go:1008 and managementClient.ts:307 blocks SERVICE_ACCOUNT creation — causing 100% of remote CLI calls to fail 401!
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04, TEEP-01-DOC-07, and TEEP-01-DOC-12 (DEF-004).
- **Potential Improvements / Target Design:** Add admin_api_tokens table (SHA-256 stored), CRUD endpoints & AccessPage UI for sak_tp_* (org/BU service accounts) and pat_tp_* (user-bound, inheriting live RBAC subset), and wire TokenVerifier in serve.go.
- **Enterprise Implications:** Immediate blocker removal for enterprise DevOps, CLI, and IaC adoption.
- **Dependencies & Constraints:** Resolves DEF-004; preserves AUTH-07 plane separation (sak_tp_/pat_tp_ rejected on /v1/*; vk_tp_ rejected on /admin/v1/*).
- **Security & Operational Considerations:** Enforce mandatory expires_at_ms <= 365d, fine-grained action scopes, optional allowed_cidrs, and instant SCIM cascade revocation.

#### `CAP-074` — 7-Tier Organizational Hierarchy (enterprises + org_units), Delegated RBAC & Full Member Lifecycle
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-04, PER-05, PER-07 | WF-05, WF-06`
- **User Problem & Value:** Models real Fortune 500 organizational trees (Enterprise -> Org -> BU -> Dept -> Team -> Project -> Env), custom roles, and complete member/org/project editing & removal.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: flat 4-level hierarchy; missing GET/PATCH on orgs/projects/envs; missing DELETE on envs & members; PG-001 preselects DEVELOPER; PG-011 confuses member scope; role_bindings lacks (org_id, scope_type, scope_id) PG index.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04, TEEP-01-DOC-07, TEEP-01-DOC-12, and TEEP-01-DOC-09
- **Potential Improvements / Target Design:** Add portable enterprises + org_units Materialized Path tables, custom RBAC permission sets + BU_ADMIN subtree scoping, missing role_bindings index, full GET/PATCH/DELETE/suspend APIs & UI for Orgs/Projects/Environments/Members, and fix PG-001/PG-011.
- **Enterprise Implications:** Scales governance cleanly across conglomerates and multi-department banks without rewriting 48 existing (org_id, id) FK tables.
- **Dependencies & Constraints:** Resolves PG-001, PG-011, and ARCH-OBS-01 index #4.
- **Security & Operational Considerations:** Flatten ancestry into ConfigSnapshot at compile time so hot-path evaluation stays O(1)/O(depth) with 0 SQL calls.

#### `CAP-075` — Complete Virtual Key Lifecycle, CIDR Allowlists, Attribution Tags & Explicit Plane Scoping
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-05, PER-07 | WF-05, WF-06`
- **User Problem & Value:** Gives operators full Day-2 control over Virtual Key rate limits, expiry, source IPs, and FinOps tags while fixing UI status and over-grant traps.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: AccessPage.tsx only edits allowed_planes & allowed_routes (cannot edit limits, models, expiry, or rotation grace window); expired Playground keys show Active (PG-002); PROVIDER plane preselected (PG-012); VKey reaper runs 2*N_orgs tick loop (ARCH-OBS-01 #16).
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04, TEEP-01-DOC-07, and TEEP-01-DOC-12
- **Potential Improvements / Target Design:** Expose full-field PATCH + configurable 60..259200s grace window in AccessPage.tsx, add per-key allowed_cidrs & attribution_tags, project/reap expired keys to EXPIRED (PG-002), require explicit plane selection (PG-012), and index/optimize VKey reaper.
- **Enterprise Implications:** Prevents accidental multi-plane over-grants and stolen-key misuse outside corporate egress NATs.
- **Dependencies & Constraints:** Resolves PG-002, PG-012, and ARCH-OBS-01 Loop #16.
- **Security & Operational Considerations:** Constant-time SHA-256 lookup + sub-second ActiveStreamRegistry severing on EMERGENCY revocation preserved.

### DOM-11 — Governance and Compliance

#### `CAP-076` — End-to-End Four-Eyes Dual Approval Workflow, Console UI & O(1) Self-Approval Guard
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-05, PER-06 | WF-10`
- **User Problem & Value:** Prevents any single compromised or rogue administrator from unilaterally rotating root keys, clearing global kill-switches, or activating external extensions.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: backend /admin/v1/change-requests exists, but SecurityPage.tsx has 0 UI, OrganizationsAdminPage cannot toggle four_eyes_enabled, extensions.go:1395 scans N_orgs*M_partitions audit rows, and positive 2-operator PG E2E is partial.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04/07 and TEEP-01-DOC-12 (ARCH-OBS-01 Loop #11).
- **Potential Improvements / Target Design:** Build full Change Requests Queue & Initiation UI in SecurityPage.tsx, persist staged_by_principal_id on extension_installations (O(1) check), add principal FK indexes on admin_change_requests, and add positive 2-operator PG FORCE RLS E2E tests.
- **Enterprise Implications:** Satisfies SOC 2 CC6.1, ISO 27001, and banking dual-control mandates.
- **Dependencies & Constraints:** Resolves ARCH-OBS-01 Loop #11 & index #6.
- **Security & Operational Considerations:** Cryptographically bind approvals to RFC 8785 JCS payload digests so staged changes cannot be mutated between initiation and approval.

#### `CAP-077` — Strict Regional Data-Residency, HIPAA BAA & Upstream ZDR Routing Fences
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-06, PER-03 | WF-03, WF-14`
- **User Problem & Value:** Mathematically blocks prompts containing regulated data from routing to non-compliant regions, global cross-region profiles, cloud-proxied local tags, or non-BAA/non-ZDR endpoints.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: DataRegion intersection exists, but lacks baa_covered / upstream_zdr_verified flags and misses Bedrock global.* & Ollama *:cloud residency leaks.
- **Competitor & Technology Evidence:** OpenRouter zdr:true routing (SRC-013), Bedrock global.* inference profiles (SRC-059), Anthropic MCP non-HIPAA/ZDR warning (SRC-051), Ollama *:cloud (SRC-044).
- **Potential Improvements / Target Design:** Add baa_covered, upstream_zdr_verified, and sub_region tags on Connections/Deployments, block global.* & *:cloud under regional/on-prem policies, auto-inject store:false when ZDR is required, and expand RR-018 multi-target quarantine+residency tests.
- **Enterprise Implications:** Hard technical control for GDPR Schrems II, HIPAA §164.312, BaFin, and FedRAMP/sovereign deployments.
- **Dependencies & Constraints:** Resolves RR-018; CAP-009.
- **Security & Operational Considerations:** Fail closed 403/503 before any secret unsealing or network socket creation when zero compliant targets exist.

#### `CAP-078` — O(1) audit_chain_heads, Monthly audit_partition_seals, External Witness & SYSTEM/ORG UI
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-06, PER-05, PER-03 | WF-10, WF-14`
- **User Problem & Value:** Scales tamper-evident JCS audit logging to high write throughput and multi-year partitioned retention while making SYSTEM audit trails visible in the Console.
- **Existing Tanpopo RC9 Capability:** SCALABILITY_BOTTLENECK / UI GAP: audit/logger.go:251 probes all M monthly partitions under pg_advisory_xact_lock; detaching old partitions triggers false ALT-AUDIT-TAMPER; SecurityPage.tsx hides SYSTEM chain (PG-010).
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-12 (ARCH-OBS-01 Q3) and PG-010.
- **Potential Improvements / Target Design:** Add audit_chain_heads PK upsert table (replacing advisory lock + M-partition scan with O(1) tip lock & monotonic clock clamp), add audit_partition_seals for batched non-xmin-pinning verification & archival, add optional KMS/WORM hourly chain-head witness, and expose SYSTEM/ORG toggle + audit export in SecurityPage.tsx.
- **Enterprise Implications:** Eliminates audit contention at 10^8+ rows and defends against rogue-DBA snapshot tail-truncation.
- **Dependencies & Constraints:** Resolves PG-010 and ARCH-OBS-01 Q3.
- **Security & Operational Considerations:** Preserve append-only immutability triggers and recursive secret redaction on all audit payloads.

#### `CAP-079` — EU AI Act (Art. 12/14/19/50) Provenance Headers & High-Risk Logging Profile
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-06, PER-04 | WF-14`
- **User Problem & Value:** Automates technical compliance with EU AI Act GPAI & Article 50 transparency rules (in force Aug 2, 2026) and High-Risk Article 12/14/19 logging duties.
- **Existing Tanpopo RC9 Capability:** MISSING dedicated EU AI Act compliance profile and Art. 50 headers in RC9.
- **Competitor & Technology Evidence:** Regulation (EU) 2024/1689 timeline (Art. 50 Aug 2, 2026; Art. 12/14/19 >=6-month automatic logging & human oversight records) (SRC-083).
- **Potential Improvements / Target Design:** Provide one-click compliance_profile = EU_AI_ACT_HIGH_RISK enforcing >=180-day metadata/audit retention floor, capturing adapter/model/policy/guardrail/SM-06 reviewer provenance, and stamping X-Tanpopo-AI-Generated & X-Tanpopo-Model-Provenance headers.
- **Enterprise Implications:** Direct procurement accelerator for European financial, healthcare, public-sector, and HR deployments.
- **Dependencies & Constraints:** CAP-057, CAP-078.
- **Security & Operational Considerations:** Provenance headers expose canonical model/policy metadata only (never internal connection IDs or credentials).

#### `CAP-080` — Automated Signed Compliance Evidence Packs (SOC 2, HIPAA, ISO/IEC 42001, EU AI Act)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-06, PER-03 | WF-14`
- **User Problem & Value:** Saves weeks of manual screenshot and SQL-query gathering during external security and AI governance audits.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (operators assemble evidence manually).
- **Competitor & Technology Evidence:** ISO/IEC 42001:2023 Annex A.6.2.8/A.9.3, SOC 2 CC6-CC8, HIPAA §164.312 (SRC-083).
- **Potential Improvements / Target Design:** Implement 'tanpopo admin compliance-pack export' (& Console button) generating a JCS-signed .zip mapping live FORCE RLS posture, audit-chain verification proofs, KEK rotation logs, Four-Eyes history, and RBAC/SCIM matrices onto framework control IDs.
- **Enterprise Implications:** Note per Section 7: generates technical control evidence packs for customer auditors without claiming Tanpopo itself holds invented certifications.
- **Dependencies & Constraints:** CAP-076, CAP-078, CAP-154.
- **Security & Operational Considerations:** Zero secrets, tokens, or prompt payloads included in exported evidence archives.

#### `CAP-081` — Automated Upstream Commercial ToS (DEC-004) & Open-Weights License Firewall
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-06, PER-05 | WF-02, WF-11`
- **User Problem & Value:** Prevents engineers or sideloaded extensions from routing enterprise workloads through ToS-violating consumer subscription scrapers or copyleft/jurisdiction-barred endpoints.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: BlindBroker enforces DEC-004 Cookie ban, but lacks consumer-domain blocklists and model license policy checks.
- **Competitor & Technology Evidence:** Bifrost community issues document GitHub Copilot / ChatGPT Plus OAuth pooling (#2028, #4459); Helicone relicensed to GPL-3.0; Meta Llama 4 bars EU multimodal use (SRC-011, SRC-017, SRC-028).
- **Potential Improvements / Target Design:** Enforce built-in consumer web endpoint denylist (chatgpt.com/backend-api, claude.ai/api, consumer User-Agent spoofing) in manifest validator + BlindBroker alongside CAP-022 license-class checks.
- **Enterprise Implications:** Upholds DEC-004 rigorously across self-hosted and Marketplace ecosystems.
- **Dependencies & Constraints:** DEC-004, CONSTRAINTS.md, CAP-022.
- **Security & Operational Considerations:** Unconditional fail-closed rejection at both package installation time and outbound egress time.

#### `CAP-082` — Immediate-Fence Tenant Crypto-Shredding & Background Erasure Finalizer
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-06, PER-03 | WF-12`
- **User Problem & Value:** Guarantees that deleting an organization immediately revokes all Data-Plane Virtual Keys and Admin sessions even if Phase-3 DB cleanup encounters a transient fault.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION (DEF-041): tenancy.go:535-779 only fences VKeys and revokes sessions AFTER Phase 3 (tx2) commits, has 0 background finalizer worker, uses pgStore.Ping as PG postShredHook, and scans 3*N_orgs in cleanupOrgRestrictedFKsTx.
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-12 (DEF-041, ARCH-OBS-01 Loop #14).
- **Potential Improvements / Target Design:** Fence VKeys + revoke Admin sessions + broadcast Emergency revocation in Phase 1 immediately upon status='DELETING', add periodic background resumeDeletingOrganization worker, index FK columns to eliminate O(N_orgs) cleanup scan, and document PG VACUUM/PITR + external KMS shred posture.
- **Enterprise Implications:** Closes a high-severity tenant offboarding security window in multi-tenant enterprise clusters.
- **Dependencies & Constraints:** Resolves DEF-041 & ARCH-OBS-01 Loop #14; CAP-067 (external KMS).
- **Security & Operational Considerations:** Idempotent state transitions ensure crash recovery completes cleanly across restarts.

### DOM-12 — MCP Infrastructure

#### `CAP-083` — Dual-Stack MCP Broker (2025-03-26 Stateful + 2026-07-28 Stateless HTTP), Resources/Prompts & Schema Cache
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-08, PER-02, PER-10 | WF-07`
- **User Problem & Value:** Bridges legacy stateful MCP clients/servers and modern 2026-07-28 Stateless Streamable HTTP tools while eliminating RC9's 4-upstream-RPCs-per-call bottleneck.
- **Existing Tanpopo RC9 Capability:** PARTIAL / SCALABILITY_BOTTLENECK: internal/ingress/mcp/hub speaks stateful 2025-03-26 only (rejects 2026-07-28 server/discover & stateless calls), runs 4 sequential upstream RPCs on every tools/call (upstream_client.go:124), and returns -32601 on prompts/* & resources/*.
- **Competitor & Technology Evidence:** MCP 2026-07-28 spec (SEP-2567/2575/2243/2322/2549/2663) adopted by Apigee 1.17.4, Traefik Hub 3.21, Langfuse v4, and Solo agentgateway v1.7 (SRC-019, SRC-020, SRC-035, SRC-040B).
- **Potential Improvements / Target Design:** Negotiate stateful (2025-03-26..2025-11-25) and stateless (2026-07-28 server/discover, Mcp-Method/Mcp-Name, MRTR, subscriptions/listen, CacheableResult), support resources/*, prompts/*, and elicitation/create, and cache verified upstream tools/list digests per ttlMs.
- **Enterprise Implications:** Cuts MCP tool-call upstream round-trips by 75% (4 -> 1) and future-proofs serverless/horizontal enterprise MCP deployments.
- **Dependencies & Constraints:** MCP 2025-03-26..2026-07-28 specs; COND-PO-04.
- **Security & Operational Considerations:** Enforce Origin validation (403 on invalid Origin per 2025-11-25+) and cacheScope:'private' on Virtual-Key-filtered tools/list.

#### `CAP-084` — JCS Tool Schema + outputSchema Pinning & Semantic Rug-Pull Drift Classifier
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-05, PER-06, PER-08 | WF-07`
- **User Problem & Value:** Prevents MCP rug-pull prompt-injection attacks (including outputSchema mutations) while ending CI/CD alert fatigue on benign additive schema updates.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: schema_pin/verifier.go hashes {name, description, inputSchema} only (strips 2025-06-18+ outputSchema/annotations) and hard-locks without classifying benign vs malicious drift.
- **Competitor & Technology Evidence:** MCP 2025-06-18 structuredContent/outputSchema (SRC-067) & GlassWorm invisible Unicode injection lessons (SRC-041).
- **Potential Improvements / Target Design:** Include outputSchema in RFC 8785 JCS pinned digest, and add Semantic Drift Classifier (BENIGN_ADDITIVE, BREAKING_CONTRACT with affected VKeys, or SUSPECTED_RUG_PULL_HIGH_RISK detecting imperative prompt overrides, Unicode tags U+E0000..E007F, and exfil URLs) + read-only cassette dry-run gate (INN-11).
- **Enterprise Implications:** Turns MCP security pinning into a developer-friendly enterprise CI/CD promotion gate.
- **Dependencies & Constraints:** CAP-061, CAP-076 (Four-Eyes re-pin).
- **Security & Operational Considerations:** Always fail closed (403 schema_hash_drift_blocked) on SUSPECTED_RUG_PULL_HIGH_RISK until Security Reviewer approval.

#### `CAP-085` — Complete MCP Server, Persistent Tool Inventory, Grant Revocation & Official Registry Sync
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-08, PER-05 | WF-07`
- **User Problem & Value:** Fixes Console tool disappearance on page reload, unlocks viewing/revoking MCP tool grants, allows editing/deleting MCP servers, and imports Official MCP Registry metadata.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: McpHubPage.tsx:78 wipes toolsByServer on reload; listMcpToolGrants/revokeMcpToolGrant are dead code in managementClient.ts:106; zero PATCH/DELETE on /mcp/servers (PG-004).
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04, TEEP-01-DOC-04/07 (PG-004), and Official MCP Registry v0.1 API (SRC-036).
- **Potential Improvements / Target Design:** Add GET /mcp/servers/{id}/tools, PATCH/DELETE /mcp/servers/{id}, wire grant list/revoke in McpHubPage.tsx, and support SSRF-guarded discovery/import from registry.modelcontextprotocol.io/v0.1/servers (flagging dead Smithery-style remotes).
- **Enterprise Implications:** Provides complete Day-2 administrative control over hundreds of internal and partner MCP servers.
- **Dependencies & Constraints:** Resolves PG-004 (MCP slice).
- **Security & Operational Considerations:** Verify reverse-DNS namespace and SSRF posture before enabling any imported remote MCP server.

#### `CAP-086` — Synchronous + Async SM-06 HITL Tool Approvals & Native io.modelcontextprotocol/tasks / MRTR
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-05, PER-06, PER-08 | WF-07, WF-10`
- **User Problem & Value:** Delivers seamless human-in-the-loop approval for destructive MCP tool calls across both legacy 2025 clients and modern 2026-07-28 tasks/elicitation clients.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE for 2025-03-26 (60s hold + tanpopo__check_approval_and_resume 5-tuple JCS token); lacks native 2026-07-28 tasks/MRTR binding.
- **Competitor & Technology Evidence:** MCP io.modelcontextprotocol/tasks (2026-07-28) & URL-mode elicitation (SEP-1036) (SRC-035, SRC-067); Gravitee Gamma HITL (SRC-040C).
- **Potential Improvements / Target Design:** Retain 5-tuple JCS pre_approval_token for 2025-* clients while exposing native CreateTaskResult (resultType:'task' / 'input_required' polled via tasks/get) and URL-mode Console approval links for 2026-07-28 clients.
- **Enterprise Implications:** Satisfies EU AI Act Art. 14 and ISO 42001 A.9.3 human-oversight requirements without custom client wrappers.
- **Dependencies & Constraints:** CAP-083, CAP-127 (Slack/Teams verified approval callbacks).
- **Security & Operational Considerations:** Cryptographically bind approval tokens to exact (org, project, vkey, tool, JCS(args)) with single-use replay prevention.

#### `CAP-087` — Zero-Tax Per-User RFC 9728 / RFC 8707 / RFC 8693 OBO MCP Tool Authorization
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-05, PER-06, PER-08 | WF-05, WF-07`
- **User Problem & Value:** Eliminates shared-service-account confused-deputy vulnerabilities on MCP tools without paying Arcade.dev's \$0.50/call Pro tool execution fee.
- **Existing Tanpopo RC9 Capability:** UNWIRED IN RC9: oauth/service.go:297 hardcodes target_kind='PROVIDER_CONNECTION' (MCP_SERVER in SQL CHECK is unused).
- **Competitor & Technology Evidence:** Arcade.dev (\$0.50/call overage, SRC-039), AWS Bedrock AgentCore Identity (SRC-040), MCP RFC 9728 + RFC 8707 + CIMD draft-03 (SRC-035, SRC-074).
- **Potential Improvements / Target Design:** Publish /.well-known/oauth-protected-resource/mcp (RFC 9728), wire oauth.Service for MCP_SERVER + SSRF-guarded CIMD, and execute RFC 8693 On-Behalf-Of Token Exchange inside BlindHostCredentialBroker (bridging legacy URN-audience IdPs cleanly).
- **Enterprise Implications:** Saves enterprises \$50k+/yr vs per-call OBO brokers while keeping user OAuth tokens 100% invisible to LLMs and Wasm plugins.
- **Dependencies & Constraints:** DEC-004 (strictly enterprise OAuth/OIDC APIs), CAP-064, CAP-070 (SCIM cascade revocation).
- **Security & Operational Considerations:** Never forward inbound client tokens upstream raw; mint target-audience-bound (RFC 8707) attenuated tokens with TTL <= 5m.

#### `CAP-088` — Host-Governed Progressive MCP Tool Discovery & Anthropic defer_loading Passthrough
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-02, PER-07, PER-08 | WF-07`
- **User Problem & Value:** Slashes 50,000+ token system-prompt bloat when workspaces connect 100-500 MCP tools, without executing unsafe LLM-generated scripts in the gateway.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (all granted MCP tools must be listed directly).
- **Competitor & Technology Evidence:** Anthropic tool_search_tool_bm25 + defer_loading:true (SRC-051), Composio session search (SRC-038), vs Bifrost's risky JS/Starlark Code Mode (SRC-011).
- **Potential Improvements / Target Design:** Provide host-governed RBAC-filtered meta-tools (tanpopo__search_tools / tanpopo__describe_tool) + native Anthropic defer_loading:true passthrough + CONTEXT_TRANSFORMER JSON Schema prose compression (INN-12).
- **Enterprise Implications:** Cuts input token spend 50-85% (HYPOTHESIS / UNVERIFIED) and preserves prompt-cache prefixes on large tool estates.
- **Dependencies & Constraints:** CAP-083, CAP-112 (CONTEXT_TRANSFORMER).
- **Security & Operational Considerations:** Zero untrusted script execution inside Gateway; search results strictly filtered by caller's VirtualKey & Cedar permissions.

#### `CAP-089` — Declarative OpenAPI / GraphQL-to-MCP TOOL_CONNECTOR Virtualization
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-08, PER-03 | WF-07, WF-11`
- **User Problem & Value:** Lets platform teams expose existing internal REST/GraphQL microservices as governed MCP tools in minutes without writing and hosting custom MCP servers.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Proven enterprise demand in Kong ai-mcp-proxy, Solo agentgateway, IBM ContextForge, Higress, and Tyk 5.15 (SRC-014, SRC-019, SRC-020B, SRC-040D).
- **Potential Improvements / Target Design:** Support Tier-1/Tier-2 TOOL_CONNECTOR .tpkg manifests mapping OpenAPI 3.1 operations into first-class mcp_tools rows executed via BlindHostCredentialBroker + SSRFGuard.
- **Enterprise Implications:** Accelerates enterprise internal API onboarding onto MCP while enforcing uniform JCS schema pinning and SM-06 HITL gates.
- **Dependencies & Constraints:** DEC-003, CAP-112.
- **Security & Operational Considerations:** Default mutating HTTP verbs (POST/PUT/PATCH/DELETE) to risk_tier='mutating'/'destructive' with 0 automatic retries (FMEA-02).

#### `CAP-090` — Supervised Container/Sidecar SIDECAR_STDIO Bridge & PlaneMCP Per-Tool FinOps Metering
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-08, PER-07, PER-03 | WF-06, WF-07`
- **User Problem & Value:** Safely bridges local/packaged stdio MCP servers without RCE risks and attributes paid third-party tool API costs into tenant FinOps budgets.
- **Existing Tanpopo RC9 Capability:** ENUM_OR_CONTRACT_ONLY in RC9: SIDECAR_STDIO is rejected in admin/http/mcp.go:165; ledger.PlaneMCP is never called by internal/ingress/mcp/*.
- **Competitor & Technology Evidence:** LiteLLM CVE-2026-30623 demonstrated arbitrary command-injection risk in naive stdio MCP spawning; Docker MCP Toolkit isolates stdio in hardened containers (SRC-008, SRC-036B).
- **Potential Improvements / Target Design:** Bridge stdio MCP packages exclusively via confined Tier-3 sidecars/containers (never arbitrary shell strings from HTTP API!), and wire per-tool fixed/unit rate cards into ledger.PlaneMCP.
- **Enterprise Implications:** Enables full chargeback across paid search, financial-data, and SaaS tool invocations.
- **Dependencies & Constraints:** CAP-037, CAP-113.
- **Security & Operational Considerations:** Forbid dynamic command/arg injection from tenant Admin API; allow only operator-pinned OCI/sidecar descriptors.

### DOM-13 — Agent and A2A Infrastructure

#### `CAP-091` — Linux Foundation A2A v1.0.1 Standard Wire Interop & Signed Agent Cards
- **Proposal Classification:** `REPLACE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-08, PER-02 | WF-08`
- **User Problem & Value:** Makes Tanpopo natively interoperable with standard Python/TS/Go/Java/.NET A2A v1.0+ SDKs, Vertex Agent Engine, Solo agentgateway, and Gravitee.
- **Existing Tanpopo RC9 Capability:** PARTIAL / INCOMPATIBLE WIRE CONTRACT: RC9 serves custom /.well-known/agent.json and /a2a/tasks (6 lowercase states, untyped maps) incompatible with LF A2A v1.0.1.
- **Competitor & Technology Evidence:** Linux Foundation A2A v1.0.0 (2026-03-12) & v1.0.1 (2026-05-26) specification (SRC-072).
- **Potential Improvements / Target Design:** Replace custom primary wire surface (retaining deprecated compat alias for 1 cycle) with normative A2A v1.0.1: /.well-known/agent-card.json, /extendedAgentCard, JSON-RPC + HTTP+JSON (/message:send, /message:stream, /tasks/{id}:cancel, /tasks/{id}:subscribe), 8 TASK_STATE_* enums (adding AUTH_REQUIRED & REJECTED), unified Part, google.rpc.Status, and RFC 8785 JCS + JWS signed Agent Cards.
- **Enterprise Implications:** Prevents protocol lock-in and enables multi-vendor enterprise agent federation.
- **Dependencies & Constraints:** CAP-094 (preserve SM-09 governance via official A2A-Extensions).
- **Security & Operational Considerations:** Enforce SSRFGuard whenever unified Part.url is fetched and verify JWS Agent Card signatures.

#### `CAP-092` — Streaming Upstream A2A Relay (ExecuteStream) & Event-Driven Valkey SSE Fan-Out
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-08, PER-03 | WF-08`
- **User Problem & Value:** Delivers real-time token/artifact streaming from downstream agents and removes 1-second SQL polling overhead per open SSE watcher.
- **Existing Tanpopo RC9 Capability:** SCALABILITY_BOTTLENECK in task_coordinator.go:152,631: buffers upstream responses via Broker.Execute and polls SQL every 1s on GET /a2a/tasks/{id} SSE.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04
- **Potential Improvements / Target Design:** Dispatch streaming /message:stream tasks via BlindBroker.ExecuteStream and fan out statusUpdate / artifactUpdate frames across pods via Valkey Pub/Sub.
- **Enterprise Implications:** Eliminates multi-second agent delegation lag and DB read amplification under concurrent multi-agent swarms.
- **Dependencies & Constraints:** CAP-046, CAP-091.
- **Security & Operational Considerations:** Enforce DeltaReserve budget checks and instant stream cancellation across streaming A2A relays.

#### `CAP-093` — Complete A2A Agent Registry CRUD, --a2a-enabled Flag & Console Runtime Banner
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-08, PER-03 | WF-08`
- **User Problem & Value:** Allows operators to update, disable, or delete registered A2A agents and eliminates silent confusion when A2A Data-Plane ingress is disabled.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: zero GET/{id}, PATCH, or DELETE on /admin/v1/a2a/agents; serve.go:740 disables /a2a/* unless TANPOPO_A2A_ENABLED=true with 0 CLI flag or Console warning banner.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04 & TEEP-01-DOC-07.
- **Potential Improvements / Target Design:** Add GET/PATCH/DELETE (and DISABLED state) on /admin/v1/a2a/agents, add --a2a-enabled flag on tanpopo serve, and display explicit runtime enablement status in McpHubPage.tsx.
- **Enterprise Implications:** Closes operational blind spots for multi-agent platform teams.
- **Dependencies & Constraints:** If-Match concurrency guard.
- **Security & Operational Considerations:** Deleting/disabling an agent cleanly rejects new delegations while allowing active tasks to cancel/settle.

#### `CAP-094` — Monotonic SM-09 Sub-Budget Carving, hop_count <= 5 Cycle Guard & Cancel Propagation
- **Proposal Classification:** `KEEP` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-06, PER-07, PER-08 | WF-08`
- **User Problem & Value:** Prevents infinite agent-to-agent recursion loops, cross-border residency escalation, and runaway sub-agent spend.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE in internal/ingress/a2a/task_coordinator.go (including DEF-085 zero-hold pre-dispatch block & DEF-069 downstream cancel propagation).
- **Competitor & Technology Evidence:** Genuine Tanpopo differentiator — absent from core LF A2A v1.0.1 and OSS proxies (SRC-072).
- **Potential Improvements / Target Design:** Keep 100% of SM-09 sub-budget carving, lineage cycle detection, and residency intersection, encoding delegation metadata inside A2A-Extensions + W3C baggage.
- **Enterprise Implications:** Mandatory financial and compliance circuit breaker for autonomous multi-agent deployments.
- **Dependencies & Constraints:** DEC-003, ADR-0005.
- **Security & Operational Considerations:** Verify multi-hop cancel propagation across PostgreSQL + Valkey HA clusters (RR-013).

#### `CAP-095` — SSRF-Guarded A2A Push Notification Webhook Delivery
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-08 | WF-08`
- **User Problem & Value:** Supports long-running asynchronous A2A tasks (minutes to hours) that notify caller agents via webhook upon completion or AUTH_REQUIRED.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (SSE/GET polling only).
- **Competitor & Technology Evidence:** Normative Create/Get/List/DeleteTaskPushNotificationConfig in LF A2A v1.0.1 (SRC-072).
- **Potential Improvements / Target Design:** Implement A2A push notification config endpoints backed by Tanpopo's transactional webhook outbox, routing caller-supplied callback URLs strictly through SSRFGuardDialer + domain allowlists + HMAC/JWS signing.
- **Enterprise Implications:** Enables event-driven enterprise workflows without holding thousands of idle SSE sockets open.
- **Dependencies & Constraints:** CAP-065 (SSRFGuard), CAP-091, CAP-125.
- **Security & Operational Considerations:** Critical SSRF vector if unguarded: reject private/IMDS callback URLs and verify callback domain ownership/allowlist.

#### `CAP-096` — Cross-Plane (LLM + MCP + A2A) Causal Governance Graph, Nested 'act' OBO & 1-Click Swarm Kill-Switch
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-05, PER-06, PER-07, PER-08 | WF-08, WF-10`
- **User Problem & Value:** Answers 'How much did this entire multi-agent task spend, which human authorized each tool hop, and how do I abort the whole runaway tree in 1 click?'
- **Existing Tanpopo RC9 Capability:** MISSING cross-plane causal tree budget & abort in RC9.
- **Competitor & Technology Evidence:** IETF RFC 8693 nested 'act' claim delegation + AAIF cross-A2A/MCP auth guidance (SRC-074; INN-10).
- **Potential Improvements / Target Design:** Propagate X-Tanpopo-Trace-Root (causal_root_id), enforce max_tree_budget_micros & max_tree_tool_calls across all 4 planes, mint nested RFC 8693 'act' OBO tokens enforcing monotonic permission intersection at each hop, render causal cost waterfalls in Console, and expose POST /admin/v1/agents/trees/{id}:abort.
- **Enterprise Implications:** Category-defining enterprise governance primitive for autonomous agent fleets.
- **Dependencies & Constraints:** DEC-003 (planes stay schema-separated while sharing causal_root_id), CAP-087, CAP-091.
- **Security & Operational Considerations:** Enforce EffectivePerms_n <= EffectivePerms_{n-1} intersect AgentPolicy_n at every token-exchange hop.

### DOM-14 — Prompt Management

#### `CAP-097` — Versioned, Project-DEK-Sealed Prompt Registry & Environment Aliasing
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-04 | WF-03, WF-13`
- **User Problem & Value:** Decouples prompt iteration from application container redeployments with immutable versioning, JSON Schema variable validation, and dev/staging/prod promotion.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Core workflow in Portkey, Langfuse, Braintrust, and Arize Phoenix — without requiring Langfuse's 6-service ClickHouse stack (SRC-010, SRC-020, SRC-022).
- **Potential Improvements / Target Design:** Store versioned prompt templates (prm_) in SQLite/PG16 sealed under Project DEK, cached in ConfigSnapshot RAM for 0-SQL hot-path rendering via x-tanpopo-prompt-id + variables.
- **Enterprise Implications:** Gives enterprise teams auditable, RBAC/Four-Eyes-governed prompt rollbacks directly at the gateway edge.
- **Dependencies & Constraints:** Vault Project DEK, ConfigSnapshot.
- **Security & Operational Considerations:** Templates sealed at rest with Project DEK; rendered prompts store 0 bytes in traces unless HPKE/debug is active.

#### `CAP-098` — Mandatory Hierarchical Governance System Preambles (CONTEXT_TRANSFORMER)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-06, PER-04 | WF-03`
- **User Problem & Value:** Lets CISOs and BU admins prepend/append non-removable safety, legal-disclaimer, or tool-governance instructions across scoped routes.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (Kong ai-prompt-decorator equivalent absent).
- **Competitor & Technology Evidence:** Kong ai-prompt-decorator & enterprise banking system-prompt policies (SRC-014).
- **Potential Improvements / Target Design:** Implement Tier-1/Tier-2 CONTEXT_TRANSFORMER policies injecting org/BU system preambles prior to Phase-1 FinOps token pre-hold.
- **Enterprise Implications:** Ensures uniform policy instructions across heterogeneous internal apps.
- **Dependencies & Constraints:** CAP-112 (CONTEXT_TRANSFORMER), CAP-037 (hold computed after injection).
- **Security & Operational Considerations:** Mark compliance preambles FAIL_CLOSED and audit template version digests in request_traces_meta.

#### `CAP-099` — GitOps Prompt Template Sync & Side-by-Side Playground Diffing
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (HYPOTHESIS)`
- **Personas & Workflows:** `PER-02, PER-04 | WF-03, WF-13`
- **User Problem & Value:** Allows engineers to review prompt diffs in Git PRs and compare candidate prompt versions side-by-side across multiple models in the Console Playground.
- **Existing Tanpopo RC9 Capability:** PARTIAL: PlaygroundPage supports 2-route manual text comparison, without saved prompt version diffing or GitOps sync.
- **Competitor & Technology Evidence:** Promptfoo & Langfuse prompt-to-eval workflows (SRC-020, SRC-023).
- **Potential Improvements / Target Design:** Include prompt templates in 'tanpopo config plan | apply' and SOLUTION_PACKs, and add prompt version selector + token/cost diff view in PlaygroundPage.
- **Enterprise Implications:** Bridges Console experimentation and Git-governed production promotion.
- **Dependencies & Constraints:** CAP-097, CAP-135, CAP-157.
- **Security & Operational Considerations:** Enforce If-Match ETag checks so Console edits and GitOps reconciliations never overwrite each other silently.

### DOM-15 — AI Evaluation

#### `CAP-100` — Gateway-Native Deterministic Regression & Contract Cassette Runner ('tanpopo eval run')
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-02, PER-03 | WF-04, WF-13`
- **User Problem & Value:** Lets teams verify JSON Schema validity, tool-call arguments, deterministic assertions, latency, and micro-USD cost bounds in CI without external SaaS dependencies.
- **Existing Tanpopo RC9 Capability:** PARTIAL: EXT-ACCEPT-01 cassette runner exists for adapter SDK tests only, not user routes/prompts.
- **Competitor & Technology Evidence:** Promptfoo (acquired by OpenAI, SRC-023) & Braintrust CI gates (SRC-023B).
- **Potential Improvements / Target Design:** Generalize Tanpopo's cassette engine into 'tanpopo eval run' executing YAML test suites against staging/loopback routes with deterministic assertions (JSON schema, regex, CEL, cost <= X micros, latency <= Y ms).
- **Enterprise Implications:** Vendor-neutral, 100% air-gap-capable CI quality gate.
- **Dependencies & Constraints:** google/cel-go, CAP-072 (CI OIDC auth).
- **Security & Operational Considerations:** Enforce explicit max-suite micro-USD budget cap on live eval runs.

#### `CAP-101` — Budget-Capped Asynchronous 1% Shadow Canary Evaluation Engine
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (HYPOTHESIS)`
- **Personas & Workflows:** `PER-03, PER-02, PER-07 | WF-04`
- **User Problem & Value:** Validates candidate replacement models on live non-mutating traffic distributions before switching production routes.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Powers Innovation INN-02 (Automated Model Deprecation & Migration Advisor).
- **Potential Improvements / Target Design:** Asynchronously mirror a configurable sample (0.1%-5%) of read-only requests to a candidate deployment bounded by a dedicated shadow_budget_ledger, comparing TTFT, TPOT, JSON schema pass rate, refusal rate, and settled cost (discarding raw completion text!).
- **Enterprise Implications:** De-risks model migrations in zero-prompt-persistence regulated environments.
- **Dependencies & Constraints:** CAP-021, CAP-026, CAP-037.
- **Security & Operational Considerations:** Drop raw shadow response bodies immediately after structural metric extraction; hard-stop on shadow_budget_ledger cap.

#### `CAP-102` — Online Structural Quality Scoring & Standard OTel gen_ai.evaluation.result Events
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-03 | WF-09`
- **User Problem & Value:** Tracks real-time production output health (malformed tool JSON, truncations, guardrail blocks, refusals) and exports standard OTel evaluation events.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** OpenTelemetry semantic-conventions-genai gen_ai.evaluation.result event (SRC-077).
- **Potential Improvements / Target Design:** Compute zero-LLM structural quality signals inline on responses/streams and emit bounded Prometheus counters + OTel gen_ai.evaluation.result events to Phoenix/Langfuse/Datadog.
- **Enterprise Implications:** Enables automated circuit-breaking or canary rollback when a model checkpoint's tool-call JSON validity degrades.
- **Dependencies & Constraints:** CAP-025 (:exacto scoring), CAP-056.
- **Security & Operational Considerations:** Zero external LLM-as-a-judge calls on the synchronous hot path.

#### `CAP-103` — Ephemeral CI OIDC Workload Gate for External Eval & Red-Team Suites
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P3-Horizon` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-05, PER-10 | WF-05, WF-13`
- **User Problem & Value:** Allows enterprise security/QA teams to run Promptfoo, UK AISI Inspect, Garak, or Ragas against staging routes without long-lived Virtual Keys.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** GitHub Actions / GitLab CI OIDC workload federation + Promptfoo/Garak red-teaming (SRC-023, SRC-074).
- **Potential Improvements / Target Design:** Bind CI workflow OIDC claims (repo + branch + environment) via CAP-072 to short-lived staging budgets and dedicated evaluation policies.
- **Enterprise Implications:** Integrates smoothly with existing enterprise ML QA pipelines.
- **Dependencies & Constraints:** CAP-072, CAP-039.
- **Security & Operational Considerations:** Pin job_workflow_ref and forbid prod environment access from untrusted PR fork workflows.

### DOM-16 — Infrastructure Operations

#### `CAP-104` — True Cluster Node Tombstones, loaded_kek_ids_json, Valkey/OTLP Health & DP Quorum Alerts
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03 | WF-12`
- **User Problem & Value:** Ends false-green 'Ready / OK' cluster health when all Data-Plane nodes are dead or Valkey is down, and surfaces per-node KEK state.
- **Existing Tanpopo RC9 Capability:** PARTIAL / DEFECTIVE (DEF-072, PG-007): serve.go:547 DELETEs cluster_nodes on shutdown; ops_lifecycle.go:145 fabricates healthy 'primary' when empty; 0 DP nodes still shows Ready/OK; loaded_kek_ids_json omitted in SELECT (:52); Valkey fallback hidden (PG-007).
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-12 (DEF-072) & QA-005 PG-007.
- **Potential Improvements / Target Design:** Transition stopped nodes to STOPPED/STALE tombstones instead of deleting, select & display loaded_kek_ids_json, report valkey_status & OTLP ring-buffer drops, and set overall status DEGRADED/UNHEALTHY when active Data-Plane nodes < expected quorum.
- **Enterprise Implications:** Restores trustworthy operational observability for multi-node HA deployments.
- **Dependencies & Constraints:** Resolves DEF-072 topology bugs & PG-007.
- **Security & Operational Considerations:** Prune ancient node tombstones (>7d) via background TTL job after auditing node departure.

#### `CAP-105` — PostgreSQL HA Parity Across All Break-Glass CLI Commands & Verified PG PITR Runbooks
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03 | WF-12`
- **User Problem & Value:** Gives Mode B/C PostgreSQL HA operators full break-glass CLI parity for KEK rotation, owner reset, upgrade preflight, bundle import, and backup verification.
- **Existing Tanpopo RC9 Capability:** SQLITE-ONLY CLI CLIFF IN RC9: vault rotate-kek, backup create, upgrade preflight, tenant shred, admin reset-local-owner, and bundle import --db hardcode sqlite.Open(...) (RR-015).
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04, TEEP-01-DOC-07, and TEEP-01-DOC-12
- **Potential Improvements / Target Design:** Wire --pg-dsn / TANPOPO_PG_DSN across all break-glass ops/admin/bundle CLI commands, and ship 'tanpopo backup verify-pg' validating pg_dump/basebackup + VerifyAllDEKEnvelopes + VerifyAllSecretEnvelopes + VerifyAllChains.
- **Enterprise Implications:** Eliminates a major operational blocker for production PostgreSQL 16 clusters.
- **Dependencies & Constraints:** Resolves RR-015 & DEF-072 CLI rotation gap.
- **Security & Operational Considerations:** Enforce CheckNonPrivilegedRole and advisory locks on all PostgreSQL CLI maintenance operations.

#### `CAP-106` — ARCH-OBS-01 7-Index PostgreSQL Pack & SECURITY DEFINER O(k_affected) Cross-Org Lookups
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-06 | WF-10, WF-12`
- **User Problem & Value:** Eliminates quadratic sequential scans and 100,000-query RLS loops across PostgreSQL multi-tenant deployments without weakening tenant FORCE RLS.
- **Existing Tanpopo RC9 Capability:** SCALABILITY_BOTTLENECK (ARCH-OBS-01): 18 O(N_orgs) loops + 7 missing PG indexes in 0001_full_schema_rls_partitions.sql.
- **Competitor & Technology Evidence:** Exhaustively mapped in 12-ARCHITECTURAL-DEBT-AND-RESIDUAL-RISKS.md §3.1.
- **Potential Improvements / Target Design:** Add Expand-Only PG migration creating all 7 missing secondary/partial indexes + narrow migration-owned SECURITY DEFINER functions (SET search_path = pg_catalog, public; gated on app.is_instance_operator='true') returning only k_affected org IDs.
- **Enterprise Implications:** Scales multi-tenant Control Plane from ~50 orgs to 10,000-100,000 orgs safely (QAS-ENT-09, HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** Resolves ARCH-OBS-01 SQL/index layer; pairs with CAP-048, CAP-066, CAP-068, CAP-078.
- **Security & Operational Considerations:** Tenant HTTP handlers NEVER gain BYPASSRLS or cross-org visibility; SECURITY DEFINER helpers return org_id keys only.

#### `CAP-107` — Production Helm Chart, Multi-Arch Distroless Images & Optional FIPS 140-3 Build Profile
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-06 | WF-12`
- **User Problem & Value:** Streamlines enterprise Kubernetes, OpenShift, and government/defense procurement with hardened container artifacts and validated manifests.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: basic deploy artifacts exist; physical multi-host K8s HA and ARM64/Windows/macOS runtime matrices remain unverified (RR-005/023/029, COND-PO-01).
- **Competitor & Technology Evidence:** Go 1.24+ native FIPS 140-3 cryptographic module support (GOFIPS140) with CGO_ENABLED=0.
- **Potential Improvements / Target Design:** Publish Cosign/Sigstore-attested linux/{amd64,arm64} distroless images, production Helm chart (split CP/DP, HPA, PDB, NetworkPolicy, ServiceMonitor), and optional GOFIPS140=v1.0.0 build profile.
- **Enterprise Implications:** Removes Kubernetes integration friction while keeping standalone non-K8s single-binary deployments first-class.
- **Dependencies & Constraints:** Addresses RR-005/029; preserves CGO_ENABLED=0.
- **Security & Operational Considerations:** Run non-root (UID 65532), read-only root filesystem, drop ALL Linux capabilities.

#### `CAP-108` — Dual-Engine SQLite <-> PostgreSQL Schema-Width Parity Gate & Rolling Skew Preflight
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03 | WF-12`
- **User Problem & Value:** Prevents cross-engine migration failures (like DEF-086 VARCHAR(40) overflow) and ensures safe N-1 -> N rolling cluster upgrades.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: DEF-086 occurred because SQLite TEXT lacked PG VARCHAR(40) bounds; db/spec-check did not enforce ID column width parity.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-12 (DEF-086) & RR-020/021/027.
- **Potential Improvements / Target Design:** Extend db/spec-check & CI to assert column-width/constraint symmetry between SQLite and PostgreSQL DDL, expand CT-32 migrate-engine parity fixtures (multi-org rotated DEKs, active holds), and verify N-1 Data Plane compatibility during Expand-Only migrations.
- **Enterprise Implications:** Guarantees smooth upgrades from standalone SQLite to enterprise PostgreSQL HA at any lifecycle stage.
- **Dependencies & Constraints:** Addresses DEF-086 & RR-020/027.
- **Security & Operational Considerations:** Strictly enforce Expand-Only migrations across minor versions.

#### `CAP-109` — Wired HTTP Admin Diagnostics (smoke-test, support-bundle, openapi.json) & Two-Way OpenAPI CI Gate
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-02 | WF-09, WF-12`
- **User Problem & Value:** Restores the unimplemented SCR-15 diagnostic endpoints and prevents OpenAPI-documented routes from ever returning HTTP 404 again.
- **Existing Tanpopo RC9 Capability:** UNIMPLEMENTED (404) IN RC9: GET /admin/v1/openapi.json, POST /diagnostics/smoke-test, and POST /diagnostics/support-bundle are in openapi/admin-v1.yaml but missing from server.go:477-611!
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-04 (one-way subset check in openapi_contract_test.go:258).
- **Potential Improvements / Target Design:** Implement all 3 handlers in internal/admin/http (streaming redacted .tar.gz support bundles including DB/Valkey/metrics summaries), wire them into Console DevTools, and enforce bidirectional spec.Paths == srv.Routes() in CI.
- **Enterprise Implications:** Enables browser-based redacted support-bundle collection for remote enterprise clusters.
- **Dependencies & Constraints:** Existing internal/cli/support/bundle.go redaction engine.
- **Security & Operational Considerations:** Gate support-bundle & smoke-test on OPERATOR/ORG_OWNER RBAC + strict privacy/secret redaction.

#### `CAP-110` — Automated Valkey Shard Promotion & DR-02 valkey_hold_checkpoints Reconciliation Proof
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-07 | WF-06, WF-12`
- **User Problem & Value:** Proves mathematically and empirically that Valkey primary shard crashes never lose active FinOps holds or allow unbounded overspend.
- **Existing Tanpopo RC9 Capability:** PARTIAL (RR-010, COND-PO-03): CP46/51 tested 3-primary no-replica outage fallback only; replica promotion & DR-02 rebuild under live traffic remain unverified.
- **Competitor & Technology Evidence:** Verified in QA-005 RR-010 & COND-PO-03.
- **Potential Improvements / Target Design:** Build automated multi-shard Valkey 8 primary-kill + replica-promotion + 'tanpopo admin verify-state --rebuild-valkey-state' chaos harness under live SSE load, exposing reconciliation metrics in /ops/cluster-status.
- **Enterprise Implications:** Closes COND-PO-03 for mission-critical HA certification.
- **Dependencies & Constraints:** Addresses RR-010 & COND-PO-03.
- **Security & Operational Considerations:** Bound worst-case 1s asynchronous Valkey replication window via conservative lease/checkpoint margin.

### DOM-17 — Extension Platform and SDK

#### `CAP-111` — Zero-Ambient-Authority Tier-2 wazero Sandbox (16 MiB, 50ms, DRR Queues, FaultGuard)
- **Proposal Classification:** `KEEP` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-09, PER-05, PER-06 | WF-11`
- **User Problem & Value:** Executes untrusted third-party extensions safely inside a pure-Go WebAssembly sandbox with zero filesystem, socket, or secret access.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE in internal/extension/wasm (14.57 us/op TransformRequest, 0 WASI imports, stateful re-instantiation, FaultGuard auto-quarantine).
- **Competitor & Technology Evidence:** Superior isolation vs LiteLLM Python RCE (CVE-2026-40217), Envoy Dynamic Modules (.so), Bifrost (.so), and Grafana go-plugin SSRF bypasses (SRC-008, SRC-011, SRC-042, SRC-079, SRC-080).
- **Potential Improvements / Target Design:** Retain pure-Go wazero Protobuf slice ABI (CGO_ENABLED=0) and add Deficit Round-Robin (DRR) per-tenant fair queuing across pool slots under >70% contention.
- **Enterprise Implications:** Foundational safety guarantee enabling community plugins in regulated banks and defense networks.
- **Dependencies & Constraints:** ADR-0001, ADR-0002, CONSTRAINTS.md.
- **Security & Operational Considerations:** Maintain hard 256-page (16 MiB) default arena cap, custom-section bomb rejection, and clear(buf) zeroization.

#### `CAP-112` — Additive runtime.proto v1.1 Host Execution Across All 11 .tpkg Extension Categories
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-09, PER-03 | WF-11`
- **User Problem & Value:** Lets third-party developers extend auth, routing, guardrails, context/RAG, caching, telemetry, notifications, model catalogs, MCP tools, and solution packs without Core rebuilds.
- **Existing Tanpopo RC9 Capability:** ENUM_OR_CONTRACT_ONLY for non-providers in RC9: ONLY PROVIDER_ADAPTER executes; AUTH_DRIVER, GUARDRAIL_POLICY, and TELEMETRY_EXPORTER have 0 host execution hooks!
- **Competitor & Technology Evidence:** Founder Requirement 6.C; verified in TEEP-01-DOC-04 & designed in 10-EXTENSION-AND-MARKETPLACE-RESEARCH.md §2.
- **Potential Improvements / Target Design:** Extend ExtensionRuntimeService additively (buf breaking clean, RPCs 11..19) and wire host execution pipelines + per-category fault postures across all 11 canonical categories.
- **Enterprise Implications:** Transforms Tanpopo from an extensible proxy into a true Enterprise AI Infrastructure Platform.
- **Dependencies & Constraints:** Preserves all 7 non-overridable Core kernel security invariants.
- **Security & Operational Considerations:** Security categories (AUTH_PROVIDER, blocking GUARDRAIL_PROCESSOR, TOOL_CONNECTOR) fail closed; optimization categories fail open to Core defaults.

#### `CAP-113` — Supervised Tier-3 ConnectRPC Sidecar Lifecycle (0600 UDS / SPIFFE mTLS + OS Isolation)
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-06, PER-09 | WF-11, WF-12`
- **User Problem & Value:** Supports heavy ML/ONNX guardrails, external HSMs, and stdio MCP bridges out-of-process with automated health supervision and honest OS isolation posture.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: internal/extension/sidecar client exists (disabled by default), with 0 host process supervisor and macOS/Windows CLONE_NEWNET asymmetry caveat.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04 & TEEP-01-DOC-12 (pentest OS sandbox caveat).
- **Potential Improvements / Target Design:** Add optional declarative Tier-3 sidecar supervisor / Kubernetes sidecar-injector template with rotating SPIFFE X.509-SVID mTLS / 0600 UDS, memory/CPU cgroup limits on Linux, and explicit preflight warning on macOS/Windows where CLONE_NEWNET is unavailable.
- **Enterprise Implications:** Enables air-gapped GPU/CPU ONNX models (GLiNER, embeddings) without polluting the CGO_ENABLED=0 gateway binary.
- **Dependencies & Constraints:** spiffe/go-spiffe/v2; --runtime-sidecar-enabled CLI flag.
- **Security & Operational Considerations:** Enforce slot_ref_* secret-leak scrubber and 2-failure circuit breaker on all Tier-3 ConnectRPC calls.

#### `CAP-114` — Official Polyglot Guest SDKs (Go, Rust, TypeScript) & Optional Build-Time WIT Lowering
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-09, PER-02 | WF-11`
- **User Problem & Value:** Lowers extension authoring time from days of raw C pointer plumbing to 15 minutes in idiomatic Go, Rust, or TypeScript.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: sdk/templates ships only YAML (tier1) and raw C (tier2-wasm) skeletons; zero Go/Rust/TS guest libraries.
- **Competitor & Technology Evidence:** Extism PDK ergonomics + wazero wasm32/wasip1 compatibility (SRC-079, SRC-080).
- **Potential Improvements / Target Design:** Ship generated tanpopo-sdk-go (TinyGo / Go 1.24+ wasm), tanpopo-sdk-rust (wasm32-unknown-unknown), and tanpopo-sdk-ts (QuickJS-ng pre-warmed Wasm snapshot in 'tanpopo plugin pack') + Tier-3 ConnectRPC starter.
- **Enterprise Implications:** Unlocks rapid ecosystem growth across both systems engineers (Rust/Go) and AI application developers (TypeScript).
- **Dependencies & Constraints:** Apache-2.0 SDK licensing; 0 runtime CGO in Tanpopo Core.
- **Security & Operational Considerations:** Strip WASI socket/filesystem imports during 'tanpopo plugin pack' validation.

#### `CAP-115` — Declarative JSON Schema + uiSchema Localized Extension Configuration Forms
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-09, PER-04 | WF-02, WF-11`
- **User Problem & Value:** Renders rich, validated, 15-language configuration UIs for custom third-party extensions in the Web Console with zero XSS or React federation risk.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (extensions/official/*/manifest.yaml lacks localized metadata maps and custom config UI schemas).
- **Competitor & Technology Evidence:** Backstage RHDH 2.1 deleted 40k LOC of Scalprum JS federation; Grafana untrusted frontend JS creates admin-origin XSS risk (SRC-042, SRC-080).
- **Potential Improvements / Target Design:** Allow .tpkg manifests to bundle JSON Schema 2020-12 + declarative uiSchema + locales/*.json strings, rendered exclusively through native Astryx Neutral inputs (TextInput, Selector, CheckboxInput, SecretField).
- **Enterprise Implications:** Safe enterprise self-service configuration for partner guardrails, connectors, and solution packs.
- **Dependencies & Constraints:** ADR-0007 (Astryx Neutral + WCAG 2.2 AA + 0 external requests).
- **Security & Operational Considerations:** Hard ban on executing third-party JavaScript, iframes, or external image URLs inside the Admin Console.

#### `CAP-116` — Offline Extension Contract, ABI Fuzz & Memory/CPU Ceiling Harness ('tanpopo plugin test --fuzz')
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-09 | WF-11`
- **User Problem & Value:** Catches panics, memory leaks, secret-slot misuse, and CT-08 downgrade bugs locally and in CI before an extension ever reaches production.
- **Existing Tanpopo RC9 Capability:** COMPLETE_FOR_RC9_SCOPE for deterministic cassette replay (EXT-ACCEPT-01), lacking built-in ABI fuzzing & non-provider category test harnesses.
- **Competitor & Technology Evidence:** Traefik coraza-http-wasm OOMs (#9) proved Wasm plugins must be stress-tested against arena caps before deployment (SRC-080).
- **Potential Improvements / Target Design:** Extend 'tanpopo plugin test' to all 11 extension categories and add '--fuzz 30s' feeding malformed UTF-8/JSON/Protobuf inputs while asserting <=16 MiB memory, <=budget CPU, and 0 WASI traps.
- **Enterprise Implications:** Raises baseline quality across both internal enterprise plugins and community submissions.
- **Dependencies & Constraints:** CAP-112, CAP-114.
- **Security & Operational Considerations:** Runs 100% offline with --live gated behind explicit developer flag.

#### `CAP-117` — Unified Cross-OS Deterministic .tpkg Packer, Merkle Tree Signing & Hermetic Wasm Build
- **Proposal Classification:** `REPLACE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-09 | WF-01, WF-11, WF-12`
- **User Problem & Value:** Permanently resolves RR-001 so Windows, macOS, and Linux builds compute identical .tpkg digests and official release signatures verify everywhere.
- **Existing Tanpopo RC9 Capability:** BLOCKED BY RR-001 IN RC9: builtin.NewCatalog packs 2-3 embedded files (fcd5f51d LF vs 21d4f37a CRLF) while 'plugin pack' packs 10-15 files (cf69f96b LF); runtime gzip.BestCompression varies by Go version; build-wasm.sh omits aws-bedrock & uses unpinned clang.
- **Competitor & Technology Evidence:** Proven empirically in TEEP-01 scratch digest probes and TEEP-01-DOC-12 (RR-001, DEF-043, ref 7503607c).
- **Potential Improvements / Target Design:** Replace split catalog.go / pack.go packers with one unified internal/extension/tpkg package (manifest allowlist excluding cassettes/src/*_test.go), fail closed on CRLF in text assets, sign a Go-version-invariant Canonical File-Tree Merkle digest ('tanpopo:tpkg-tree:v1'), pin containerized clang + add aws-bedrock in build-wasm.sh, and enforce 3-OS SHA-256 parity in CI.
- **Enterprise Implications:** Unblocks Product Owner offline signing ceremony (GATE-REL-07 prerequisite) and eliminates source-build trust drift.
- **Dependencies & Constraints:** Resolves RR-001 and DEF-043.
- **Security & Operational Considerations:** Preserve offline Ed25519 root-key separation: CI never holds private release-signing keys.

### DOM-18 — Marketplace and Community Ecosystem

#### `CAP-118` — Keyless OIDC Sigstore (Fulcio + Rekor) Publisher Onboarding & DNS Namespace Verification
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-09, PER-05 | WF-11`
- **User Problem & Value:** Allows independent developers and partner vendors to publish signed extensions without long-lived PATs that can be leaked or stolen.
- **Existing Tanpopo RC9 Capability:** MISSING public community publisher onboarding pipeline in RC9.
- **Competitor & Technology Evidence:** Directly counters 2025-2026 GlassWorm, Nx Console 18.95, and 550+ leaked VS Code PAT breaches (SRC-041, SRC-081).
- **Potential Improvements / Target Design:** Require short-lived CI OIDC workload tokens (GitHub/GitLab/Buildkite) + Sigstore Fulcio ephemeral certs + Rekor transparency log entries bound to repo & commit SHA, plus _tanpopo-publisher.<domain> DNS TXT namespace proof.
- **Enterprise Implications:** Establishes verifiable SLSA Level 3 provenance for every community and partner .tpkg.
- **Dependencies & Constraints:** Reserved ^@tanpopo(-[a-z0-9_-]+)?/ namespace locked to Tanpopo offline root keys (CR-03).
- **Security & Operational Considerations:** Zero static publishing API tokens ever minted or stored.

#### `CAP-119` — Staged Delta-P Consent, Canary Rings, Graceful Disable, Browser Upload & <5s Target Rollback
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-05 | WF-11`
- **User Problem & Value:** Completes the extension admin UX with non-incident disable, browser .tpkg upload, lockfile download, canary environment rings, and verified instant rollback.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: no graceful disable from ENABLED_ACTIVE without Quarantine (DEF-044); no browser .tpkg upload or lockfile download button; concurrent 409 & live v1->v2->Rollback partial (DEF-036, DEF-060).
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04/07 and TEEP-01-DOC-12 (DEF-036, DEF-044, DEF-060).
- **Potential Improvements / Target Design:** Add calm Disable transition (ENABLED_ACTIVE -> INSTALLED_DISABLED), multipart .tpkg upload & tanpopo.lock file download in ExtensionsPage.tsx, per-environment ring pinning (dev/staging/5% prod canary), clean 409 error mapping, and <5s (HYPOTHESIS / UNVERIFIED) atomic rollback E2E.
- **Enterprise Implications:** Safe enterprise change management for gateway extensions across dev/staging/prod fleets.
- **Dependencies & Constraints:** Resolves DEF-036, DEF-044, and DEF-060.
- **Security & Operational Considerations:** Any permission delta (Delta-P != empty) on upgrade forces re-staging and human/Four-Eyes consent (CT-18).

#### `CAP-120` — 5-Tier Extension Trust Governance & Console/CLI org_extension_policies Controls
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-05, PER-06 | WF-01, WF-11`
- **User Problem & Value:** Removes the #1 Day-0 '403 org_trust_policy_denied' showstopper on self-built/sideloaded binaries while giving CISOs clear 5-tier provenance policies.
- **Existing Tanpopo RC9 Capability:** CRITICAL UI/CLI OMISSION IN RC9: PUT /admin/v1/organizations/{orgId}/extension-policies has ZERO Console UI and ZERO CLI command, forcing raw DevTools cookie + CSRF curl hacks on unsigned builds!
- **Competitor & Technology Evidence:** Proven in QA-005 RC9-REPORT.md and 04-CURRENT-TANPOPO-PRODUCT-AUDIT.md §1.
- **Potential Improvements / Target Design:** Expose Org Extension Trust Policy panel in ExtensionsPage.tsx (SCR-07) + inline ORG_OWNER opt-in checkbox during unverified approval + 'tanpopo org trust-policy set' CLI, mapped cleanly across OFFICIAL, VERIFIED_PARTNER, COMMUNITY_REVIEWED, PRIVATE_ENTERPRISE, and UNVERIFIED tiers.
- **Enterprise Implications:** Balances frictionless developer sideloading in dev orgs with strict OFFICIAL/ENTERPRISE_SIGNED locks in prod banks.
- **Dependencies & Constraints:** Resolves RC9 Setup Trust-Policy Friction; pairs with CAP-117.
- **Security & Operational Considerations:** Child projects can never relax parent org min_plugin_trust; all policy changes require If-Match + JCS audit.

#### `CAP-121` — 7-Stage Automated Marketplace Moderation, Wasm Import Whitelist & DEC-004 CI Gate
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-09, PER-06 | WF-11`
- **User Problem & Value:** Automatically blocks malicious, vulnerable, non-reproducible, copyleft, or ToS-scraping packages before they enter the curated Marketplace index.
- **Existing Tanpopo RC9 Capability:** MISSING Marketplace CI moderation pipeline in RC9.
- **Competitor & Technology Evidence:** Designed in 10-EXTENSION-AND-MARKETPLACE-RESEARCH.md §3.1 from 2026 LiteLLM/OpenVSX post-mortems (SRC-006..008, SRC-041).
- **Potential Improvements / Target Design:** Enforce 7 automated gates: (1) OIDC/DNS check, (2) --network=none reproducible Wasm build match, (3) Wasm ImportSection whitelist + gitleaks + SPDX check, (4) DEC-004 consumer-scraping block, (5) CT-01..18 + 30s fuzz, (6) CycloneDX 1.6 SBOM + 0 High/Crit CVEs, (7) human review on initial release & Delta-P.
- **Enterprise Implications:** Gives enterprise CISOs confidence to enable VERIFIED_PARTNER and COMMUNITY_REVIEWED tiers without re-auditing every line.
- **Dependencies & Constraints:** DEC-004, CONSTRAINTS.md, CAP-116, CAP-118.
- **Security & Operational Considerations:** Hard-reject any Wasm module importing wasi_snapshot_preview1.sock_*, path_open, or proc_exec.

#### `CAP-122` — TUF (go-tuf/v2) Anti-Freeze / Anti-Downgrade Metadata & Monotonic revocations.json Kill-Switch
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-05, PER-06, PER-03 | WF-10, WF-11`
- **User Problem & Value:** Protects online and mirrored gateways against MITM index freeze, version-rollback, and suppressed-revocation attacks.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: internal/extension/registry verifies Ed25519 feed signatures + monotonic serial, without full TUF root/targets/snapshot/timestamp role delegation or root rotation.
- **Competitor & Technology Evidence:** The Update Framework (github.com/theupdateframework/go-tuf/v2, Apache-2.0/BSD-3, pure Go) (SRC-081).
- **Potential Improvements / Target Design:** Adopt TUF metadata hierarchy (offline 3-of-5 root.json, delegated partner/community targets, 7d snapshot.json, 24h timestamp.json, 6h revocations.json) with CRITICAL_MALICIOUS auto-quarantine and forensic blast-radius reports.
- **Enterprise Implications:** Defends against compromised mirror servers and supports seamless root-key rotation.
- **Dependencies & Constraints:** go-tuf/v2 (CGO_ENABLED=0); CAP-066.
- **Security & Operational Considerations:** Offline air-gapped gateways verify TUF bundle expiry windows explicitly during 'tanpopo bundle verify'.

#### `CAP-123` — OCI 1.1 Artifact Distribution (oras-go/v2 + SBOM/Sigstore Referrers) for Harbor/ECR Mirrors
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-06 | WF-11, WF-12`
- **User Problem & Value:** Allows regulated enterprises to mirror Tanpopo extensions, SBOMs, and signatures using their existing Harbor, JFrog Artifactory, Nexus, or AWS ECR infrastructure.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: oci_client.go pulls @sha256 blobs, but ProvidersPage omits OCI_REGISTRY_AUTH in UI and Marketplace lacks OCI 1.1 Referrer SBOM/Sigstore attachments.
- **Competitor & Technology Evidence:** Backstage RHDH OCI dynamic plugins & Guardrails AI Hub shutdown lesson (avoid proprietary account-gated registries!) (SRC-029, SRC-080, SRC-081).
- **Potential Improvements / Target Design:** Publish Marketplace packages as standard OCI 1.1 Artifacts (oras.land/oras-go/v2) with CycloneDX 1.6 SBOM and Sigstore bundle Referrers, and expose private OCI mirror configuration + OCI_REGISTRY_AUTH in Console.
- **Enterprise Implications:** Zero custom proxy servers needed for enterprise DMZ-to-airgap replication.
- **Dependencies & Constraints:** oras-go/v2 (Apache-2.0, pure Go); CAP-014.
- **Security & Operational Considerations:** Enforce @sha256 immutable digest pinning (mutable tags like :latest rejected at install time).

#### `CAP-124` — Dual-Engine (SQLite + PostgreSQL) Air-Gap .tpbundle Export / Verify / Import
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-06 | WF-11, WF-12`
- **User Problem & Value:** Enables sneaker-net extension, revocation-feed, and Model Intelligence Pack updates into disconnected PostgreSQL HA clusters, not just SQLite.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: internal/cli/bundle/import.go only supports --db SQLite (lacks --pg-dsn PostgreSQL support!) and TANPOPO_BUNDLE_OCI_MIRROR_ROOT is undocumented.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04/07
- **Potential Improvements / Target Design:** Add --pg-dsn / TANPOPO_PG_DSN and --include-model-catalog support to 'tanpopo bundle export | verify | import' plus Console air-gap bundle upload for OPERATORs.
- **Enterprise Implications:** Essential for air-gapped defense, intelligence, energy, and banking HA clusters running PostgreSQL 16.
- **Dependencies & Constraints:** CAP-017, CAP-105, CAP-122.
- **Security & Operational Considerations:** Verify Ed25519/TUF signatures and archive extraction bounds before opening any database transaction.

### DOM-19 — Notifications and Integrations

#### `CAP-125` — Decoupled Async Webhook Outbox Fan-Out, Tenant Pending Index & ORG_OWNER Access Restoration
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-04, PER-05 | WF-10, WF-12`
- **User Problem & Value:** Prevents global security quarantines from stalling/duplicating across N_orgs outboxes, speeds up pending checks, and unlocks ORG_OWNER webhook management in UI.
- **Existing Tanpopo RC9 Capability:** DEFECTIVE / BOTTLENECKED IN RC9: (1) serve.go:1046 runs O(N_orgs) webhook scan TWICE synchronously during quarantine; (2) webhook_event_outbox lacks (org_id) pending PG index; (3) OperationsPage locks out ORG_OWNER from /settings/webhooks!
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-04/07 and TEEP-01-DOC-12 (ARCH-OBS-01 #10 & index #5; RC9 Webhook Delivery partial).
- **Potential Improvements / Target Design:** Move global extension webhook fan-out to an async SYSTEM background job, add partial index (org_id) WHERE dispatched_at_ms IS NULL, split /settings/webhooks into an ORG_OWNER-accessible view, and add live PG subscriber HMAC E2E tests.
- **Enterprise Implications:** Restores tenant self-service webhooks while protecting emergency containment SLAs.
- **Dependencies & Constraints:** Resolves ARCH-OBS-01 #10, index #5, & RC9 Webhook Delivery residual risk.
- **Security & Operational Considerations:** Keep dedicated least-privileged tanpopo_webhook_worker PG role, Vault-sealed URLs/secrets, and SSRFGuard HTTPS-only delivery.

#### `CAP-126` — Full Webhook Subscription CRUD, Delivery History / DLQ Inspector, Replay & Expanded Event Taxonomy
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-04, PER-07 | WF-06, WF-10`
- **User Problem & Value:** Lets operators edit webhook URLs/events, send test pings, inspect HTTP failure status codes in the Dead-Letter Queue, and subscribe to rich lifecycle events.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: OperationsPage.tsx:207 only toggles {enabled}; zero delivery log/DLQ API or test-ping; only 4 event types supported.
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-04 & TEEP-01-DOC-07.
- **Potential Improvements / Target Design:** Allow editing target_url/events/secret, add GET /webhooks/{id}/deliveries, POST .../test, and POST .../replay, and expand events (budget.burn_anomaly, model.deprecation_warning, mcp.schema_drift, scim.deprovisioned, four_eyes.requested, key.expiring).
- **Enterprise Implications:** Eliminates blind webhook debugging when downstream Slack/PagerDuty endpoints rotate secrets or experience transient 5xxs.
- **Dependencies & Constraints:** CAP-125.
- **Security & Operational Considerations:** Redact response snippets from webhook delivery failure logs and rate-limit manual test pings.

#### `CAP-127` — Native NOTIFICATION_SINK Connectors (Slack, MS Teams, PagerDuty, Jira, ServiceNow) with Verified HITL Callbacks
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-05, PER-03, PER-08 | WF-07, WF-10`
- **User Problem & Value:** Delivers formatted incident alerts and interactive MCP/Four-Eyes approval cards directly in Slack, Teams, and PagerDuty without custom middleware lambdas.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (raw JSON webhooks only).
- **Competitor & Technology Evidence:** Enterprise SRE/SecOps standard across PagerDuty Events v2, Slack Block Kit, MS Teams Adaptive Cards, Jira, and ServiceNow.
- **Potential Improvements / Target Design:** Ship signed Tier-1/Tier-2 NOTIFICATION_SINK .tpkg formatters; for interactive HITL callbacks, require Slack/Teams signature verification PLUS authenticated OIDC principal + 5-tuple JCS hash check.
- **Enterprise Implications:** Cuts mean-time-to-approve (MTTA) on mutating MCP tool holds and Four-Eyes requests from hours to seconds.
- **Dependencies & Constraints:** CAP-076, CAP-086, CAP-112.
- **Security & Operational Considerations:** Never trust unauthenticated chat user IDs alone for destructive approvals — bind chat actions to verified Tanpopo OIDC/SAML principals.

#### `CAP-128` — Enterprise SIEM / SOAR & FinOps Ecosystem Connectors (Splunk HEC, Sentinel, Datadog, Snowflake)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-06, PER-05, PER-07 | WF-06, WF-14`
- **User Problem & Value:** Streams tamper-evident JCS security audit events directly into enterprise SOC SIEMs and FOCUS 1.1 billing tables into cloud data warehouses.
- **Existing Tanpopo RC9 Capability:** MISSING dedicated SIEM/warehouse formatters in RC9.
- **Competitor & Technology Evidence:** Splunk HEC, Microsoft Sentinel (CEF/OCSF), CrowdStrike Next-Gen SIEM, QRadar, Datadog, Snowflake.
- **Potential Improvements / Target Design:** Deliver signed TELEMETRY_EXPORTER .tpkg plugins mapping Tanpopo audit_events to OCSF (Open Cybersecurity Schema Framework) JSON and usage_rollups_hourly to FOCUS 1.1.
- **Enterprise Implications:** Plug-and-play integration with global Security Operations Centers (SOC).
- **Dependencies & Constraints:** CAP-043, CAP-058, CAP-112.
- **Security & Operational Considerations:** Out-of-hot-path async batching via BlindHostCredentialBroker with zero secret/prompt leakage.

#### `CAP-129` — Verified Developer IDE & Coding-Agent Profiles (Claude Code, Cursor, Cline, Zed, GitHub Actions)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-02, PER-05 | WF-01, WF-05`
- **User Problem & Value:** Allows engineering orgs to onboard hundreds of developers using Claude Code, Cursor, Windsurf, Cline, or Zed onto governed Virtual Keys in <60 seconds.
- **Existing Tanpopo RC9 Capability:** PARTIAL: Playground generates generic curl/Python/TS snippets only.
- **Competitor & Technology Evidence:** Bifrost proved strong market pull for '/gateway/claude-code' OAuth device login, though Bifrost issues mixed in ToS-violating Copilot/ChatGPT subscription scraping (SRC-011).
- **Potential Improvements / Target Design:** Provide 1-click IDE Setup Cards & 'tanpopo login --agent claude-code|cursor' OAuth 2.1 Device-Code flows minting user-attributed Virtual Keys pointed at CAP-002 native /v1/messages & /v1/* endpoints — backed strictly by commercial/self-hosted enterprise connections (100% DEC-004 compliant!).
- **Enterprise Implications:** High-velocity enterprise wedge: govern developer coding-agent spend, MCP tools, and DLP across the entire engineering org.
- **Dependencies & Constraints:** DEC-004, CAP-002, CAP-039 (per-user budgets), CAP-083.
- **Security & Operational Considerations:** Bind minted IDE keys to the developer's SSO identity so SCIM offboarding (CAP-070) revokes IDE access immediately.

### DOM-20 — Product and Developer Experience

#### `CAP-130` — Self-Activating <90s Guided /setup & 'tanpopo init' + Deterministic 'doctor --interactive'
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-02, PER-03 | WF-01`
- **User Problem & Value:** Transforms first-time self-hosted onboarding from a broken 7-screen / 20-minute ordeal into a <90s zero-curl guided flow across Web Console and SSH terminal.
- **Existing Tanpopo RC9 Capability:** BROKEN / HIGH-FRICTION IN RC9: /setup sees available_adapters:[] (bootstrap.go:114) and skips quickstart & FirstKeyDialog (DEF-049); bootstrap.go:509 omits self_hosted_zero_rate; no headless 'tanpopo init'.
- **Competitor & Technology Evidence:** Founder Requirement 6.E; proven in TEEP-01-DOC-04/07; INN-09.
- **Potential Improvements / Target Design:** Populate /setup Step 1 & 'tanpopo init' from builtin.Catalog, atomically activating signed adapter + sealing key + probing + auto-importing discovered models w/ signed \$/1M rate cards (or self_hosted_zero_rate:true) + creating route + opening FirstKeyDialog, backed by 15-locale offline CheckDiagnosticCode playbooks in 'tanpopo doctor --interactive'.
- **Enterprise Implications:** Serves solo homelab self-hosters and air-gapped enterprise evaluators equally with zero external LLM calls.
- **Dependencies & Constraints:** Resolves DEF-049; depends on CAP-017, CAP-018, CAP-117, CAP-120.
- **Security & Operational Considerations:** Keep 169.254.169.254 IMDS hard-blocked even when guided local Ollama/vLLM private-IP CIDR consent is granted.

#### `CAP-131` — 100% Admin OpenAPI <-> Web Console Parity & Elimination of Create-Only Resource Traps
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-04, PER-05, PER-06, PER-07 | WF-02..WF-12`
- **User Problem & Value:** Ensures every administrative capability in Tanpopo can be operated end-to-end in the Web Console without dropping down to raw curl or DB SQL.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: 17 of 68/112 OpenAPI ops have 0 Console UI, 2 client functions are dead code, 4 OpenAPI routes 404, and Models, Deployments, Connections, Keys, Policies, Budgets, Orgs, Projects, Envs, Members, MCP, and A2A have create-only traps.
- **Competitor & Technology Evidence:** Exhaustively audited in TEEP-01-DOC-04 & TEEP-01-DOC-07
- **Potential Improvements / Target Design:** Wire all unexposed screens/tabs (Org Trust Policy, Four-Eyes, SYSTEM/ORG Audit, Retention TTLs decoupled from Webhooks, KEK Retire, SSO/Device OAuth, HPKE/Debug Viewer, MCP Grants, DevTools Explorer/Smoke/Bundle) + full edit/delete/retire + server-side cursor pagination (>200 rows).
- **Enterprise Implications:** Raises Console UX to enterprise control-plane parity without clutter via progressive disclosure drawers.
- **Dependencies & Constraints:** ADR-0007 (<=250 KB gzip initial JS budget preserved via route-level code splitting).
- **Security & Operational Considerations:** Preserve X-CSRF-Token and If-Match optimistic concurrency + 409 diff modal across all new mutation forms.

#### `CAP-132` — Astryx Neutral Enterprise Shell, Cmd+K Palette, Scope Persistence & 403-Safe UX
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-04, PER-05, PER-06 | WF-03, WF-10`
- **User Problem & Value:** Prevents wrong-tenant F5 accidents, eliminates misleading '0 items' states on 403 errors, keeps incident badges synchronized, and speeds up power-user navigation.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: F5 resets Org/Project/Env scope (PG-005 / RR-012); SecurityPage shows '0 pending' + Kill-Switch CTA on 403 (PG-014); MCP HITL badge sync partial (DEF-020); Active vs Disabled StatusPill share identical grey ink.
- **Competitor & Technology Evidence:** Verified in QA-005 DEF-020, PG-005, PG-013, PG-014 & TEEP-01-DOC-04/07
- **Potential Improvements / Target Design:** Persist (orgId, projectId, envId) in URL/sessionStorage, enforce shared 403-exclusive ErrorState component across all 15 pages, unify shell badge invalidation bus, add Astryx CommandPalette (Cmd/Ctrl+K), distinguish Active vs Disabled typographically/iconographically (WCAG 1.4.1), and reconcile AGENTS.md CSS/font docs.
- **Enterprise Implications:** Polishes daily enterprise usability across multi-org operators and keyboard-first engineers.
- **Dependencies & Constraints:** Resolves DEF-020, PG-005, PG-014, RR-012; ADR-0007 (zero horizontal scroll 320-1920px, Light/Dark parity).
- **Security & Operational Considerations:** Validate restored sessionStorage org/project IDs against /admin/v1/auth/session authorized scopes before issuing queries.

#### `CAP-133` — 15-Language Offline Localization, DTO Enum Translation & AST i18n Lint Gate
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01..PER-09 (Global Teams) | WF-01..WF-14`
- **User Problem & Value:** Delivers true native-quality UI copy across all 15 supported languages with zero leaked SCREAMING_SNAKE_CASE backend enums.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION (PG-003): FinOpsPage.tsx:314,358 & McpHubPage.tsx:699 render raw PROJECT, VIRTUAL_KEY, ON_PREM despite keys existing in locales/*.json; check-i18n.mjs misses raw JSX enum expressions.
- **Competitor & Technology Evidence:** Verified in QA-005 PG-003 & TEEP-01-DOC-04/07
- **Potential Improvements / Target Design:** Map all DTO enums through localized formatting helpers, upgrade check-i18n.mjs + Playwright E2E to audit populated FinOps/MCP/Models pages in non-English locales (vi, ja, de, zh-Hans), and run headless Lighthouse CLI in CI (COND-PO-01).
- **Enterprise Implications:** Differentiates Tanpopo globally across APAC (ja, ko, zh, vi, th, id) and EMEA/LATAM enterprises.
- **Dependencies & Constraints:** Resolves PG-003 & COND-PO-01 Lighthouse check.
- **Security & Operational Considerations:** 100% offline bundled locale chunks (0 external translation API calls).

#### `CAP-134` — Complete Remote Read/Write CLI, 'tanpopo login' & Full 41-Var TANPOPO_* Flag/Docs Parity
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-02 | WF-10, WF-12, WF-13`
- **User Problem & Value:** Makes every server setting discoverable via --help and empowers SREs to manage keys, routes, extensions, and kill-switches remotely from terminals and scripts.
- **Existing Tanpopo RC9 Capability:** BROKEN / UNDOCUMENTED IN RC9: all 11 remote CLI commands fail 401 (DEF-004), remote CLI is read-only, 25+ of 41 TANPOPO_* env vars lack CLI flags (PG-008, PG-009, PG_DSN, VALKEY_ADDRS), and docs/README.md + AGENTS.md have stale unit checkpoints.
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-04/07
- **Potential Improvements / Target Design:** Implement 'tanpopo login' + remote mutation subcommands (including emergency 'tanpopo killswitch trigger|clear'), add explicit CLI flags & docs for all legitimate TANPOPO_* env vars, reconcile docs/README.md + AGENTS.md + production-ha-and-airgap.md, and test CLI against a real compiled tanpopo serve binary.
- **Enterprise Implications:** Eliminates silent misconfigurations (such as accidentally booting standalone SQLite in HA guides!).
- **Dependencies & Constraints:** Resolves DEF-004, PG-008, PG-009; depends on CAP-073.
- **Security & Operational Considerations:** Never pass raw secrets on command-line argv where ps/procfs can read them — accept *-file or stdin/env for secret values.

#### `CAP-135` — Multi-Dialect Studio Playground, SDK Snippets & Immediate Ephemeral Key Revocation
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-01, PER-02, PER-08 | WF-01, WF-03, WF-07`
- **User Problem & Value:** Lets developers test Chat, Responses, Anthropic Messages, Gemini, and MCP tools interactively with live cost/trace waterfalls and leak-free ephemeral sessions.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: PlaygroundPage.tsx:299 'End session' leaves pg-... key active on server (PG-006); RouteTypeahead omits LogicalModel slugs; supports /v1/chat/completions only.
- **Competitor & Technology Evidence:** Verified in QA-005 PG-006 & TEEP-01-DOC-04/07.
- **Potential Improvements / Target Design:** Call revokeVirtualKey(session.key_id, 'EMERGENCY_ABORT_STREAMS') on End Session / unmount (fixing PG-006), include LogicalModel slugs in RouteTypeahead, add dialect selector (Chat / Responses / Messages / MCP), and generate Python/TS/Go/curl/Claude Code/Cursor snippets.
- **Enterprise Implications:** Accelerates developer verification while enforcing strict ephemeral-credential hygiene.
- **Dependencies & Constraints:** Resolves PG-006.
- **Security & Operational Considerations:** Keep 15-minute hard TTL + automatic logout/session-end server revocation.

#### `CAP-136` — Obsolete / Ghost Configuration, Unused Font Dependency & Stale Governance Banner Cleanup
- **Proposal Classification:** `REMOVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-09 | WF-12`
- **User Problem & Value:** Removes dead environment checks, unused npm font packages, and contradictory agent/doc banners that mislead operators and maintainers.
- **Existing Tanpopo RC9 Capability:** OBSOLETE ARTIFACTS VERIFIED IN RC9: (1) ghost TANPOPO_ADMIN_LISTEN_ADDR checked in doctor.go:154 but ignored by serve.go; (2) unused @fontsource/figtree in web/console/package.json:29 while vite.config.ts aliases Google Sans; (3) stale TP-IMP-0008/0038/0042 banners in docs/README.md & AGENTS.md.
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-04/07, §4.2 & §4.3.
- **Potential Improvements / Target Design:** Remove dead TANPOPO_ADMIN_LISTEN_ADDR probe (or formally add dedicated admin listener if chosen in OQ-14), remove unused @fontsource/figtree dependency, and remove obsolete cold-start milestone banners.
- **Enterprise Implications:** Reduces cognitive noise, attack surface confusion, and node_modules supply-chain weight.
- **Dependencies & Constraints:** check_drift.go & check.go CI verification.
- **Security & Operational Considerations:** Verify 0 broken doc links and 0 bundle-budget regressions after cleanup.

### DOM-21 — Async, Batch, Realtime & Multimodal Media Workloads

#### `CAP-137` — Durable Async Batch (/v1/batches, /v1/messages/batches) & Background Responses BATCH_ESCROW Ledger
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-07, PER-10 | WF-03, WF-06`
- **User Problem & Value:** Unlocks 30-50% vendor batch/background token discounts for bulk enterprise classification, extraction, and eval jobs under strict budget governance.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (SM-07 90s orphan reaper would prematurely reclaim 2h-24h batch holds!).
- **Competitor & Technology Evidence:** OpenAI /v1/batches (-50%) & /v1/responses background:true (2h -30% / 8h -40%) (SRC-048), Anthropic /v1/messages/batches (-50%, SRC-051), Gemini batchGenerateContent (SRC-055).
- **Potential Improvements / Target Design:** Add /v1/batches + background:true orchestration backed by durable BATCH_ESCROW holds (24h-48h lease horizon exempt from 90s sync reaper), background completion poller/webhook reconciler, and connection-pinned batch job routing.
- **Enterprise Implications:** Captures up to 50% FinOps savings on high-volume offline enterprise AI pipelines.
- **Dependencies & Constraints:** CAP-037, CAP-041, CAP-138 (/v1/files).
- **Security & Operational Considerations:** Enforce tenant RLS ownership on batch_id lookups so Tenant A can never poll or cancel Tenant B's batch.

#### `CAP-138` — Connection-Pinned, DLP-Scanned /v1/files & Resumable Upload Broker
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-06 | WF-03`
- **User Problem & Value:** Allows governed PDF, JSONL batch, audio, and image uploads without breaking multi-connection routing or bypassing DLP.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (inline base64 <=32 MiB only).
- **Competitor & Technology Evidence:** OpenAI /v1/files, Anthropic /v1/files (files-api-2025-04-14), Gemini resumable /upload/v1beta/files (up to 2 GB) (SRC-048, SRC-051, SRC-055).
- **Potential Improvements / Target Design:** Stream multipart uploads through streaming DLP/guardrail scanners, mint gateway-scoped file handles (tpf_...) mapped to (org_id, project_id, connection_id, upstream_file_id) with TTL auto-purge, and pin referencing inference calls to that connection.
- **Enterprise Implications:** Prevents cross-account 404 file_id failures and orphaned sensitive documents in vendor cloud storage.
- **Dependencies & Constraints:** CAP-060, CAP-061.
- **Security & Operational Considerations:** Stream chunks with bounded memory (never buffer multi-hundred-MB files in Go heap) and auto-delete upstream files on TTL/org-shred.

#### `CAP-139` — Realtime BiDi Voice/Audio Gateway (/v1/realtime, BidiGenerateContent) & Constrained Ephemeral Tokens
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-02, PER-03, PER-07 | WF-03, WF-06`
- **User Problem & Value:** Governs low-latency bidirectional speech-to-speech customer service and voice agents across WebSocket and WebRTC transports.
- **Existing Tanpopo RC9 Capability:** REJECTED IN RC9: server.go:687-732 rejects all WebSocket/Upgrade requests with 400 unsupported_protocol_upgrade.
- **Competitor & Technology Evidence:** OpenAI GPT-Live-1.1 / gpt-realtime-2.1 (WS /v1/realtime + WebRTC /calls + /client_secrets, SRC-048) & Google Gemini Live (WSS BidiGenerateContentConstrained, SRC-055); Portkey & Cloudflare proxy WebSockets.
- **Potential Improvements / Target Design:** Support both: (a) Constrained Ephemeral Session Token Minting (locking model, system prompt, tools, and pre-held audio budget for direct WebRTC/browser sessions), and (b) Full Governed WebSocket BiDi Proxy with rolling 30s DeltaReserve audio-token metering and instant kill-switch severing.
- **Enterprise Implications:** Opens enterprise contact-center and voice-copilot workloads to Tanpopo governance.
- **Dependencies & Constraints:** CAP-037 (DeltaReserve), CAP-041 (audio token/per-sec rates), ActiveStreamRegistry.
- **Security & Operational Considerations:** Lock setup-frame instructions/tools inside constrained tokens to prevent browser clients from overriding system guardrails!

#### `CAP-140` — Unified & Native Image, Video, Speech & Transcription Endpoints with Multimodal Metering
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-07 | WF-03, WF-06`
- **User Problem & Value:** Consolidates Whisper/Voxtral transcription, TTS speech synthesis, and GPT-Image/Imagen/Veo generation behind one governed Virtual Key and budget ledger.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (RateCard has image/audio fields, but /v1/images/* and /v1/audio/* endpoints do not exist!).
- **Competitor & Technology Evidence:** 1,055 image_generation, 524 video_generation, 132 audio_transcription, and 89 audio_speech models in litellm_model_prices.json (SRC-045).
- **Potential Improvements / Target Design:** Add /v1/images/{generations,edits}, /v1/audio/{transcriptions,translations,speech} (including SSE transcript/partial-image streaming) + EU AI Act Art. 50 provenance headers on synthetic media.
- **Enterprise Implications:** Eliminates shadow API keys created by marketing, media, and meeting-transcription teams.
- **Dependencies & Constraints:** CAP-016, CAP-079, CAP-138.
- **Security & Operational Considerations:** Enforce resolution/duration rate multipliers and optional C2PA / content-moderation post-rails.

#### `CAP-141` — Per-Page Metered Document OCR & Structured Extraction Plane (/v1/ocr)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-07 | WF-03, WF-06`
- **User Problem & Value:** Supports high-volume enterprise invoice, claims, and clinical PDF OCR pipelines billed accurately per page.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Mistral /v1/ocr (ocr_cost_per_page + annotation_cost_per_page, SRC-064) and LiteLLM v1.91 Rust OCR gateway (SRC-005).
- **Potential Improvements / Target Design:** Expose unified POST /v1/ocr routing across Mistral OCR, Azure Document Intelligence, Bedrock/Textract, and self-hosted VLM/OCR pools, settling exact page counts via DocPerPageNanoUSD.
- **Enterprise Implications:** High-volume banking/insurance/healthcare document-processing staple.
- **Dependencies & Constraints:** CAP-016, CAP-142 (blob.Store).
- **Security & Operational Considerations:** Enforce max page count and PII redaction options on extracted markdown/JSON.

#### `CAP-142` — Host blob.Store Zero-Copy Streaming (<= 32 MiB) & Length-Prefixed Binary AWS EventStream Framing
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-02 | WF-03`
- **User Problem & Value:** Keeps Wasm memory tiny on large PDFs/images while ensuring binary AWS Bedrock ConverseStream responses never stall or corrupt above 2 MiB.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: blob/store.go is strong, but sse/pump.go:99 splits strictly on 0x0A ('\n'), whereas AWS EventStream uses binary length-prefixed packets without newline delimiters!
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-04 & AWS Bedrock ConverseStream spec (SRC-059).
- **Potential Improvements / Target Design:** Preserve blob_ref:// zero-Wasm-copy splicing and add dedicated big-endian prelude-length + CRC32 framing in sse.Pump for application/vnd.amazon.eventstream.
- **Enterprise Implications:** Guarantees rock-solid streaming reliability on AWS Bedrock multimodal and long-output streams.
- **Dependencies & Constraints:** CAP-007, CAP-009.
- **Security & Operational Considerations:** Validate EventStream prelude/message CRC32 and cap single binary frame size <= 4 MiB.

### DOM-22 — Context, Memory, RAG & Vector Governance

#### `CAP-143` — Gateway-Encrypted Stateful Thread Hydration (previous_response_id, /v1/conversations) & Strict CT-08 Reject
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-08 | WF-03, WF-08`
- **User Problem & Value:** Fixes RC9's silent-drop bug on previous_response_id and optionally provides vendor-neutral encrypted conversation state that survives cross-provider failover.
- **Existing Tanpopo RC9 Capability:** CRITICAL GAP IN RC9: responses.go ignores unknown top-level fields (previous_response_id, conversation, background), risking silent context drop (CT-08 violation)!
- **Competitor & Technology Evidence:** OpenAI /v1/responses + /v1/conversations (SRC-048) & Gemini /v1beta/interactions (SRC-055).
- **Potential Improvements / Target Design:** Immediately enforce CT-08 400 capability_not_supported when stateful IDs hit stateless routes; and offer opt-in Gateway-Managed Encrypted Threads (storing Project-DEK-encrypted turn items + store:false encrypted_content with strict TTL) so multi-turn Responses apps can fail over across accounts/vendors!
- **Enterprise Implications:** Solves stateful lock-in while preserving Zero-Data-Retention on upstream cloud providers.
- **Dependencies & Constraints:** CAP-003, CAP-005, CAP-057 (TTL reaper), Vault Project DEK.
- **Security & Operational Considerations:** Threads are strictly opt-in per policy, encrypted under Project DEK, and destroyed on Project/Org shred or GDPR subject erasure.

#### `CAP-144` — Pre-Flight Token Counting (/count_tokens, /input_tokens) & Context Compaction (/compact)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-07, PER-08 | WF-03, WF-06`
- **User Problem & Value:** Allows agents to check exact prompt token budgets before dispatch and compact long multi-step trajectories before hitting context cliffs.
- **Existing Tanpopo RC9 Capability:** MISSING unified endpoints in RC9 (/v1/messages/count_tokens also omitted from anthropic/manifest.yaml in some native configs).
- **Competitor & Technology Evidence:** Anthropic POST /v1/messages/count_tokens (SRC-051), OpenAI POST /v1/responses/input_tokens & /v1/responses/compact (SRC-048), Gemini countTokens (SRC-055).
- **Potential Improvements / Target Design:** Expose unified & native pre-flight token counting and context compaction endpoints, improving Phase-1 FinOps pre-hold accuracy and preventing >200k/>272k context-tier price surprises.
- **Enterprise Implications:** Essential for long-horizon coding and research agents operating near context limits.
- **Dependencies & Constraints:** CAP-003, CAP-041.
- **Security & Operational Considerations:** Enforce rate limits on free upstream count_tokens endpoints to prevent quota exhaustion.

#### `CAP-145` — Vector-Space-Locked Embedding & Rerank Governance (Unified Gemini/Vertex/Cohere v2 + 'rerank' Allowlist Fix)
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-02, PER-03 | WF-03`
- **User Problem & Value:** Prevents silent vector-database corruption from incompatible embedding failover, fixes RC9's 'rerank' allowlist bug, and adds Gemini/Vertex/Bedrock/Cohere v2 embeddings.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: chat_completions.go:1174 omits 'rerank' shorthand (403 bug!); gemini & azure-openai lack unified /v1/embeddings; routes lack vector-space equivalence locks.
- **Competitor & Technology Evidence:** Verified in 05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md §1.5 & TEEP-01-DOC-04/§2.1; Cohere v2 embed input_type & Matryoshka dimensions (SRC-064).
- **Potential Improvements / Target Design:** Add 'rerank' to routeAllowlistPermits, implement unified /v1/embeddings across Gemini/Vertex/Azure/Bedrock/Cohere v2 (passing input_type hints), and enforce hard (vector_space_id, dimensions, quantization) compatibility checks across all targets of an embedding route!
- **Enterprise Implications:** Prevents catastrophic production RAG outages caused by mixing incompatible embedding models in one failover pool.
- **Dependencies & Constraints:** CAP-009, CAP-010.
- **Security & Operational Considerations:** Fail route creation/update (422 incompatible_embedding_space) if target deployments do not share identical vector space and dimensions.

#### `CAP-146` — BlindBroker-Mediated Internal Vector / RAG Enrichment Subrequests (CONTEXT_TRANSFORMER)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P3-Horizon` · **Research Confidence:** `MEDIUM (HYPOTHESIS)`
- **Personas & Workflows:** `PER-08, PER-09 | WF-03, WF-11`
- **User Problem & Value:** Allows gateway policies to enrich prompts from approved internal knowledge bases (pgvector, Qdrant, OpenSearch) with unified RBAC and residency controls.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Enterprise RAG gateway pattern in AWS Bedrock Knowledge Bases & Higress (SRC-016, SRC-059).
- **Potential Improvements / Target Design:** Permit authorized CONTEXT_TRANSFORMER plugins to issue bounded (<=100ms) BlindBroker HTTP subrequests to allowlisted internal vector stores, running indirect-injection scanning (CAP-063) and Phase-1 FinOps holds after retrieval.
- **Enterprise Implications:** Centralizes RAG credential management and chunk-injection defense for thin client apps.
- **Dependencies & Constraints:** CAP-063, CAP-064, CAP-065, CAP-112.
- **Security & Operational Considerations:** Strict subrequest count (<=2) and timeout (<=100ms) caps prevent cascading gateway latency amplification.

### DOM-23 — Privacy Engineering, Data Sovereignty & Cryptographic Custody

#### `CAP-147` — Zero-Knowledge (RFC 9180 HPKE) Failure Flight Recorder & Wired Debug Capture Sessions
- **Proposal Classification:** `REPLACE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-02, PER-06, PER-05 | WF-09`
- **User Problem & Value:** Fixes RC9's 100%-unreachable debug blob feature while guaranteeing that not even Gateway root admins, DBAs, or Master KEK can read captured failure payloads.
- **Existing Tanpopo RC9 Capability:** UNREACHABLE IN RC9: CreateDebugCaptureSession (tracer.go:1160) has 0 non-test callers across cmd/ and internal/ — debug_payload_blobs is permanently empty!
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-04; designed with Go crypto/hpke RFC 9180 in 11-PRODUCT-INNOVATION-OPPORTUNITIES.md (INN-08).
- **Potential Improvements / Target Design:** Replace unreachable stub wiring with two explicit modes: (1) Recommended Zero-Knowledge HPKE Flight Recorder sealing 4xx/5xx/guardrail failure capsules in memory to an offline Project Triage X25519 Public Key (clear(buf) immediately; decrypted locally via 'tanpopo debug decrypt-capsule'), and (2) time-boxed (<=1h) Four-Eyes-gated Vault debug sessions wired in Console.
- **Enterprise Implications:** Solves the acute developer-vs-DPO deadlock in banking and healthcare production debugging.
- **Dependencies & Constraints:** RFC 9180 HPKE (CGO_ENABLED=0); CAP-057 (<=72h hard SQL TTL).
- **Security & Operational Considerations:** Private triage key is NEVER uploaded to Tanpopo; mandatory JCS audit event on capsule creation and download.

#### `CAP-148` — GDPR Art. 17 Per-Subject HMAC Salt-Shredding Pseudonymization (subject_privacy_keys)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-06 | WF-12, WF-14`
- **User Problem & Value:** Resolves the architectural collision between GDPR 'Right to be Forgotten' requests and SQL-immutable JCS audit / financial settlement tables.
- **Existing Tanpopo RC9 Capability:** MISSING per-subject erasure in RC9 (only full-Organization crypto-shredding exists, while audit_events_immutable blocks row edits).
- **Competitor & Technology Evidence:** GDPR Art. 17(3)(b)/(e) & Recital 26 pseudonymization standards (SRC-083; TEEP-01-DOC-09).
- **Potential Improvements / Target Design:** Write subject_pseudonym = 'psu_' + HMAC-SHA256(subject_link_key, canonical_subject_id) into immutable C2/C3 tables while storing subject_link_key in mutable Org-DEK-sealed subject_privacy_keys; DELETE /admin/v1/privacy/subjects/{id} shreds the link key + purges debug/cache blobs in <15s (QAS-ENT-07, HYPOTHESIS / UNVERIFIED).
- **Enterprise Implications:** 100% of JCS audit hash chains and micro-USD financial totals remain intact while irreversibly unlinking the natural person.
- **Dependencies & Constraints:** CAP-078, Vault Org DEK.
- **Security & Operational Considerations:** Truncate/hash client IP addresses in GDPR compliance mode and run WAL checkpoint / secure_delete after key purge.

#### `CAP-149` — Ephemeral In-RAM Reversible PII Session Tokenization & SSE Rehydration
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (HYPOTHESIS)`
- **Personas & Workflows:** `PER-06, PER-02 | WF-03`
- **User Problem & Value:** Allows enterprise workflows to use frontier cloud LLMs on customer records without upstream vendors ever seeing real names, IBANs, or patient IDs.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Kong ai-sanitizer & Lasso Presidio masking (SRC-014, SRC-030).
- **Potential Improvements / Target Design:** Pre-request GUARDRAIL_PROCESSOR replaces detected entities with deterministic placeholders (<PERSON_01>, <IBAN_01>) held solely in per-request Host RAM (defer clear()), rehydrating tokens across sliding UTF-8 SSE chunk boundaries on return to the caller.
- **Enterprise Implications:** Unlocks cloud LLM adoption for sensitive customer-operations workflows.
- **Dependencies & Constraints:** CAP-060, CAP-061, CAP-062.
- **Security & Operational Considerations:** Zero disk/DB/Valkey persistence of the placeholder-to-PII map; buffered sliding window handles split tokens across SSE chunks.

#### `CAP-150` — Dual-Custody HPKE WORM Legal-Hold Compliance Archiver (SEC 17a-4 / FINRA)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P3-Horizon` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-06 | WF-14`
- **User Problem & Value:** Satisfies broker-dealer and litigation-hold mandates requiring immutable communications archival without exposing plaintext prompts to Gateway operators.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** SEC Rule 17a-4 / FINRA AI communications archival requirements in financial services (TEEP-01-DOC-09).
- **Potential Improvements / Target Design:** Provide opt-in COMPLIANCE_ARCHIVE sink encrypting payloads via RFC 9180 HPKE directly to an external Compliance Custody Public Key and streaming sealed blobs to WORM S3 Object Lock (Compliance Mode).
- **Enterprise Implications:** Unlocks regulated capital-markets and wealth-management deployments.
- **Dependencies & Constraints:** RFC 9180 HPKE, BlindHostCredentialBroker.
- **Security & Operational Considerations:** Gateway holds encryption public key ONLY (0 decryption capability in Gateway RAM or Vault).

#### `CAP-151` — Sovereign Cell-Based Multi-Region Active-Active Data Planes & Regional Quota Envelopes
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (HYPOTHESIS)`
- **Personas & Workflows:** `PER-03, PER-06 | WF-12`
- **User Problem & Value:** Guarantees EU/US/APAC prompts, traces, and settlements terminate strictly within regional borders and survive transoceanic WAN cuts.
- **Existing Tanpopo RC9 Capability:** MISSING multi-region cell federation in RC9 (single-cluster FMEA-07 trips 503 after 10s CP partition).
- **Competitor & Technology Evidence:** Designed in 09-ENTERPRISE-REQUIREMENTS-RESEARCH.md §4.2 (QAS-ENT-08).
- **Potential Improvements / Target Design:** Deploy regional active-active PG16+Valkey Data-Plane cells synced via TUF/Ed25519-signed OCI ConfigSnapshot bundles and dynamically rebalanced Regional Budget Envelopes (SUM(RegionEnvelopes) <= OrgHardLimit).
- **Enterprise Implications:** Delivers 99.99% regional availability during global WAN partitions and absolute data sovereignty (HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** CAP-038, CAP-048, CAP-122.
- **Security & Operational Considerations:** Zero cross-region prompt/trace replication; global rollups aggregate anonymized integer counters only.

### DOM-24 — Resilience, Chaos Engineering & Pre-Flight Policy Simulation

#### `CAP-152` — Metadata-Only Historical Traffic Replay ('What-If Engine')
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-03, PER-04, PER-06, PER-07 | WF-03, WF-04, WF-13`
- **User Problem & Value:** Eliminates production change anxiety by simulating candidate route, Cedar policy, or pricing changes against millions of real historical traffic vectors with 0 stored prompt bytes.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9 (single-request dry-run Route Explain only).
- **Competitor & Technology Evidence:** Flagship Innovation INN-01: competitors require raw prompt storage in SaaS/ClickHouse to replay traffic.
- **Potential Improvements / Target Design:** Implement POST /admin/v1/simulations/what-if & 'tanpopo admin what-if' replaying 7-30d of request_traces_meta + usage_settlements vectors against an in-RAM CandidateSnapshot (>=50,000 traces/s target, HYPOTHESIS / UNVERIFIED), reporting exact newly-blocked calls by error code/project and exact micro-USD spend delta.
- **Enterprise Implications:** Transformative Day-2 safety net for delegated BU admins and GitOps pull requests.
- **Dependencies & Constraints:** Shares exact Resolver.Resolve & pricing.Calculate kernel (CAP-028).
- **Security & Operational Considerations:** Read-only bounded worker pool operating strictly over metadata vectors (0 PII/prompt exposure).

#### `CAP-153` — 8-Layer Live Dependency Topology & Pre-Mutation SPOF Cut-Set Simulator
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-05, PER-06 | WF-02, WF-10, WF-12`
- **User Problem & Value:** Warns operators before disabling a connection, rotating/deleting a secret, or quarantining an extension if downstream routes have zero healthy failover targets.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: GET /admin/v1/blast-radius exists for post-incident/kill-switch counts (and scans O(N_orgs) without adapter index in ops.go:1468), lacking full 8-layer SPOF cut-set UI previews.
- **Competitor & Technology Evidence:** Flagship Innovation INN-03; verified in TEEP-01-DOC-12.
- **Potential Improvements / Target Design:** Expose GET /admin/v1/topology/graph & /impact-preview linking Extension -> Adapter -> Secret/DEK -> Connection -> Deployment -> Route -> Policy/Key -> ActiveStreamRegistry, rendering SPOF cut-set warnings inside Astryx AlertDialogs before destructive mutations confirm.
- **Enterprise Implications:** Prevents self-inflicted enterprise outages during routine secret/connection maintenance.
- **Dependencies & Constraints:** ConfigSnapshot + ActiveStreamRegistry + CAP-106 indexes.
- **Security & Operational Considerations:** Scope topology views strictly by caller RBAC (ORG_OWNER sees tenant graph; OPERATOR sees sanitized instance dependency counts).

#### `CAP-154` — Built-In Hermetic Loopback Chaos & DORA Resilience Drill Studio
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-03, PER-06 | WF-14`
- **User Problem & Value:** Generates recurring cryptographic proof that circuit breakers, residency fences, budget hard-stops, and MCP rug-pull locks work — with 0 paid API calls and 0 customer impact.
- **Existing Tanpopo RC9 Capability:** TEST-ONLY IN RC9: internal/testkit/{fakellm,fakemcp} and tests/chaos/fmea_suite_test.go exist only in Go test binaries.
- **Competitor & Technology Evidence:** EU DORA (enforced Jan 2025+) operational resilience testing mandates; Innovation INN-05 (SRC-083).
- **Potential Improvements / Target Design:** Package an OPERATOR-gated 'tanpopo ops chaos-drill run' & Console Drill Runner executing real Data-Plane pipelines against an isolated in-memory loopback fault injector (429, mid-SSE RST, budget race, MCP drift, Wasm OOM) inside a dedicated sandbox_drill_org, emitting JCS-signed dora-drill-receipt.json attestations.
- **Enterprise Implications:** Unique compliance and SRE confidence feature unavailable in any competing gateway.
- **Dependencies & Constraints:** OPERATOR RBAC + dedicated isolated drill tenant.
- **Security & Operational Considerations:** Loopback synthetic upstream permitted exclusively for internal drill adapter handles; 0 external egress.

#### `CAP-155` — Continuous >=4h Protocol/Wasm Fuzzing & Unsigned/Dual-Backend E2E Parity Gate
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P0-Foundational` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-09 | WF-11, WF-12`
- **User Problem & Value:** Eliminates the test blind spots that allowed unsigned Day-0 403s, DEF-004, DEF-081, DEF-084, DEF-086, and PG-003 to escape unit/mock tests.
- **Existing Tanpopo RC9 Capability:** PARTIAL (COND-PO-01 & TEEP-01-DOC-04/07): >=4h fuzzing unexecuted; console_e2e.py injected e2e/signer hiding unsigned 403; mobile-375 sweep ran on empty Bravo Org; CLI tests mocked httptest.
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-04/07 & TEEP-01-DOC-12 (COND-PO-01).
- **Potential Improvements / Target Design:** Add mandatory CI/nightly gates: (1) unsigned-binary E2E onboarding test (without e2e/signer), (2) populated-data 320px/375px + non-English (vi/ja) Playwright assertions across Chromium/Firefox/WebKit, (3) real-binary CLI E2E against SQLite & PG16 FORCE RLS, and (4) scheduled >=4h Go fuzzing across SSE/EventStream/JSON-RPC/Wasm/Decimal parsers.
- **Enterprise Implications:** Structural assurance upgrade preventing regressions across future TEEP implementation waves.
- **Dependencies & Constraints:** Addresses COND-PO-01, RR-011, and RC9 test blind spots.
- **Security & Operational Considerations:** Keep 100% hermetic execution (0 external network or paid API calls in CI).

#### `CAP-156` — Automated Security Quarantine Forensic Timeline & Blast-Radius Exporter
- **Proposal Classification:** `IMPROVE` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-05, PER-06 | WF-10`
- **User Problem & Value:** Produces an instant incident-response report listing every key, connection, tool call, and trace that touched a compromised extension or credential during an exposure window.
- **Existing Tanpopo RC9 Capability:** PARTIAL_IMPLEMENTATION: internal/security/audit/blastradius.go exists, but scans unindexed metadata_json->>'target_id' across N_orgs*M_partitions (ARCH-OBS-01 Loop #18).
- **Competitor & Technology Evidence:** Verified in TEEP-01-DOC-12.
- **Potential Improvements / Target Design:** Add composite target indexes on audit_events & request_traces_meta and auto-generate a JCS-signed Forensic Blast-Radius Dossier whenever a CRITICAL_MALICIOUS TUF revocation or emergency quarantine fires.
- **Enterprise Implications:** Accelerates CISO / GDPR 72-hour breach-assessment reporting from days to seconds.
- **Dependencies & Constraints:** CAP-066, CAP-106, CAP-122.
- **Security & Operational Considerations:** Partition-pruned time-window queries prevent incident forensics from overloading live DB CPU.

### DOM-25 — GitOps, Terraform/IaC & Portable Declarative Solution Packs

#### `CAP-157` — Database-Reconciling Declarative GitOps ('tanpopo config export | diff | plan | apply')
- **Proposal Classification:** `REPLACE` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-04 | WF-13`
- **User Problem & Value:** Replaces RC9's misleading file-copy-only 'config reconcile' with true idempotent Control-Plane database reconciliation from version-controlled YAML.
- **Existing Tanpopo RC9 Capability:** MISLEADING PARTIAL IN RC9: internal/ops/gitops/reconciler.go:54-86 only validates YAML + tanpopo.lock hashes and copies a file on disk — it NEVER writes to SQLite or PostgreSQL!
- **Competitor & Technology Evidence:** Proven in TEEP-01-DOC-04 & TEEP-01-DOC-07.
- **Potential Improvements / Target Design:** Replace file-copy stub with full declarative state export/diff/plan/apply reconciling Orgs, org_units, Connections (\${VAULT:slot}/\${ENV:...} refs), Models, Deployments, Routes, Cedar/Structural Policies, Guardrails, Budgets, and MCP Servers into SQLite/PG16 via transactional ETag/If-Match plans.
- **Enterprise Implications:** Enables true Git-as-single-source-of-truth across standalone VMs and multi-cluster enterprises without requiring Kubernetes.
- **Dependencies & Constraints:** CAP-073 (Service Account auth), CAP-152 (What-If integration).
- **Security & Operational Considerations:** Hard-reject any plaintext secret literal in GitOps YAML (allow \${VAULT:...} and \${ENV:...} references only).

#### `CAP-158` — Official OpenTofu & Terraform Provider (terraform-provider-tanpopo)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT)`
- **Personas & Workflows:** `PER-03, PER-05 | WF-13`
- **User Problem & Value:** Integrates Tanpopo directly into enterprise platform-engineering Terraform / OpenTofu stacks alongside AWS, Azure, GCP, Okta, and Vault resources.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Standard enterprise IaC expectation (Kong, Cloudflare, Okta, Vault Terraform providers) (SRC-043).
- **Potential Improvements / Target Design:** Generate and maintain Apache-2.0 terraform-provider-tanpopo from openapi/admin-v1.yaml using Terraform Plugin Framework, authenticating via sak_tp_* Service Account keys with full If-Match drift detection and import support.
- **Enterprise Implications:** Eliminates custom curl/null_resource glue in enterprise infrastructure repositories.
- **Dependencies & Constraints:** CAP-073, CAP-131 (complete Admin CRUD verbs).
- **Security & Operational Considerations:** Mark secret_credentials value attributes WriteOnly/Sensitive so Terraform state files can opt out of storing raw API keys.

#### `CAP-159` — Optional Kubernetes Operator & CRD Controller (tanpopo-operator)
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-03 | WF-12, WF-13`
- **User Problem & Value:** Offers Kubernetes-native ArgoCD / Flux CRD workflows for K8s-standardized platform teams without forcing Kubernetes onto standalone/Compose users.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Agent Router and Traefik Hub mandate K8s CRDs (alienating non-K8s teams), whereas Solo agentgateway supports both standalone and K8s control planes (SRC-016, SRC-019, SRC-040E).
- **Potential Improvements / Target Design:** Ship an optional lightweight Go controller reconciling TanpopoRoute, TanpopoPolicy, TanpopoVirtualKey (syncing to K8s Secret or WIF binding), and TanpopoMcpServer CRDs against the Control-Plane Admin API.
- **Enterprise Implications:** Best of both worlds: K8s-native GitOps for platform teams + single-binary simplicity for everyone else.
- **Dependencies & Constraints:** CAP-073, CAP-107, CAP-157.
- **Security & Operational Considerations:** Controller authenticates via least-privilege K8s ServiceAccount WIF / sak_tp_* key and respects namespace-to-project tenancy boundaries.

#### `CAP-160` — Portable Signed SOLUTION_PACK Declarative Blueprints ('tanpopo pack plan | apply')
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P1-High` · **Research Confidence:** `HIGH (VERIFIED_FACT + HYPOTHESIS)`
- **Personas & Workflows:** `PER-01, PER-03, PER-04, PER-06 | WF-01, WF-11, WF-13`
- **User Problem & Value:** Packages complete reference architectures (EU GDPR Support Gateway, HIPAA Clinical RAG, Coding-Agent MCP Firewall) as signed, 1-command installable blueprints.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Flagship Innovation INN-04 inspired by Terraform Modules + TUF/OCI signing, portable across SQLite Mode A and PG16 Mode C.
- **Potential Improvements / Target Design:** Implement Category #11 SOLUTION_PACK (.tpkg/OCI, 0 imperative code, @sha256-pinned plugin dependencies + JSON Schema input variables + parameterized Models/Routes/Cedar Policies/Guardrails/Budgets/Alerts) applied via 'tanpopo pack plan | apply' and Console Wizard.
- **Enterprise Implications:** Compresses enterprise compliance & use-case deployment from weeks of architecture consulting to <60 seconds (HYPOTHESIS / UNVERIFIED).
- **Dependencies & Constraints:** CAP-112, CAP-122, CAP-123, CAP-157.
- **Security & Operational Considerations:** Contains 0 secrets (binds operator-selected Vault secretSlots at apply time) and runs purely declarative validation.

#### `CAP-161` — CI/CD Pull-Request What-If & FinOps/Policy Impact Gate Action
- **Proposal Classification:** `ADD` · **Candidate Priority:** `P2-Strategic` · **Research Confidence:** `MEDIUM-HIGH (HYPOTHESIS)`
- **Personas & Workflows:** `PER-03, PER-04, PER-07 | WF-03, WF-13`
- **User Problem & Value:** Catches capability regressions, residency blocks, and \$10,000/mo pricing mistakes inside GitHub/GitLab pull requests before config merges to main.
- **Existing Tanpopo RC9 Capability:** MISSING in RC9.
- **Competitor & Technology Evidence:** Combines Terraform Plan UX with Innovation INN-01 (Metadata-Only Historical Traffic Replay).
- **Potential Improvements / Target Design:** Provide a turnkey CI step ('tanpopo config plan -f gateway.yaml --what-if 7d --fail-on-blocked >0%') authenticating via CI OIDC (CAP-072) and posting a clean Markdown diff table of resource changes, blocked historical traces, and projected monthly micro-USD delta.
- **Enterprise Implications:** Brings software-engineering CI discipline to enterprise AI routing, security policy, and FinOps management.
- **Dependencies & Constraints:** CAP-072, CAP-152, CAP-157.
- **Security & Operational Considerations:** CI runner receives aggregated integer impact counts only (0 raw trace/tenant secrets exported to PR comments).
