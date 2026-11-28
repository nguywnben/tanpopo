# 01 — Group 1 — Core Inference, Providers, Model Intelligence & Media Workloads

- **Document ID:** `TEEP-02A-DOC-03-01`
- **Domains Covered:** `DOM-01` (Universal AI Gateway), `DOM-02` (Provider Management), `DOM-03` (Model Intelligence), `DOM-21` (Async, Batch, Realtime & Multimodal Media Workloads)
- **Parent Catalog:** [`../02-MASTER-FEATURE-CATALOG.md`](../02-MASTER-FEATURE-CATALOG.md) · **Founder Guide:** [`../10-FOUNDER-REVIEW-GUIDE.md`](../10-FOUNDER-REVIEW-GUIDE.md)

---

## `DOM-01` — Universal AI Gateway (`8` Proposed Features)

- **Domain Overview:** Universal AI Gateway (DOM-01) is the front-door inference plane through which developer SDKs, IDE coding agents, and enterprise microservices invoke commercial and self-hosted AI models. Instead of forcing teams to rewrite applications into a lowest-common-denominator chat wrapper, it pairs drop-in compatibility for OpenAI (/v1/chat/completions, /v1/responses), Anthropic (/v1/messages), Google GenAI (/v1beta/models/*), specialized code/decision endpoints, and native vendor passthrough (/native/{dialect}/*) with uniform authentication, policy enforcement, cryptographic reasoning continuity, idempotency, and accurate streaming usage accounting.
- **Existing RC9 Baseline Posture:** Frozen RC9 (ac9ae87a) provides a solid foundation on POST /v1/chat/completions, /v1/embeddings, /v1/rerank, payload-fingerprinted Idempotency-Key locking [SOURCE_VERIFIED], FMEA-01 clean SSE mid-stream error aborts without synthetic [DONE] markers [RUNTIME_VERIFIED], and X-Tanpopo-Usage-Accuracy HTTP trailers (DEF-083) [RUNTIME_VERIFIED]. However, top-level /v1/messages and /v1beta/models/* are absent (requiring /native/* prefixes), /v1/responses is a single-turn OpenAI-only shim unread by Tier-2 Wasm adapters [SOURCE_VERIFIED], multi-turn cryptographic reasoning signatures (thoughtSignature, signature, encrypted_content) are stripped on unified chat routes [SOURCE_VERIFIED], and /native/{dialect}/* blocks Azure ?api-version= query strings, rejects model-less GET requests, and mismatches leading-slash paths on AWS Bedrock Converse (DEF-048, FND-013) [SOURCE_VERIFIED].
- **Progressive Self-Hosted to Enterprise Experience:** On Day-1, a solo self-hoster exports OPENAI_BASE_URL, ANTHROPIC_BASE_URL, or GEMINI_BASE_URL to point Claude Code, Cursor, or an existing SDK directly at Tanpopo with zero code changes and automatic usage tracking. As an organization scales to enterprise governance, teams progressively enable strict CT-08 zero-silent-downgrade validation, payload-fingerprinted Idempotency-Key deduplication for financial batch jobs, Zero-Data-Retention (ZDR) encrypted reasoning continuity, and explicit Virtual Key plane scoping.
- **Technical Selections Deferred to `TEEP-03`:** Internal canonical intermediate representation (IR) wire structs across Tier-1/Tier-2 adapters, exact storage layout and encryption envelope for opt-in stateful /v1/responses thread hydration (DEC-CAND-06), and Valkey hot-path idempotency key lock schemas (ARCH-001) are deferred to TEEP-03 architecture design.

| ID | Subdomain | Feature Name | Class | Disposition | Priority | Placement Candidate |
|---|---|---|---|---|---|---|
| `FEAT-001` | Unified Inference Surfaces | **Unified OpenAI /v1/chat/completions & Streaming Inference Plane** | `CAT-PROD` | `KEEP` | `P0-Foundational` | `CORE_DATA_PLANE` |
| `FEAT-002` | Native SDK Surfaces | **Native Top-Level Vendor SDK Endpoints (/v1/messages, /v1beta/models/*)** | `CAT-PROD` | `ADD` | `P0-Foundational` | `CORE_DATA_PLANE` |
| `FEAT-003` | Agentic Responses & Reasoning | **Full Agentic /v1/responses Plane & Upward Reasoning+Tools Adaptation** | `CAT-PROD` | `IMPROVE` | `P0-Foundational` | `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` |
| `FEAT-004` | Native Passthrough Plane | **Full-Method & Query-Aware /native/{dialect}/* Passthrough** | `CAT-PROD` | `IMPROVE` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |
| `FEAT-005` | Agentic Responses & Reasoning | **Multi-Turn Reasoning Signature & Encrypted State Continuity** | `CAT-PROD` | `ADD` | `P0-Foundational` | `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` |
| `FEAT-006` | Request Integrity & Deduplication | **Payload-Fingerprinted Request Idempotency (Idempotency-Key)** | `CAT-PROD` | `KEEP` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |
| `FEAT-007` | Streaming Integrity & Accounting | **Deterministic SSE Mid-Stream Error Framing & Usage Accuracy Trailers** | `CAT-PROD` | `KEEP` | `P0-Foundational` | `CORE_DATA_PLANE` |
| `FEAT-008` | Specialized Code, Decision & Safety Surfaces | **Specialized /v1/fim/completions, /v1/decisions & /v1/moderations Endpoints** | `CAT-PROD` | `ADD` | `P2-Differentiator` | `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` |

### FEAT-001 — Unified OpenAI /v1/chat/completions & Streaming Inference Plane

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-001` · `CAP` Traceability: `CAP-001` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-01` (Universal AI Gateway) -> **Unified Inference Surfaces**
- **3. Plain-Language Product Summary:** Provides a standard OpenAI-compatible chat endpoint (/v1/chat/completions) so applications can talk to dozens of cloud or local AI models through one consistent API. It supports streaming responses, tool calling, structured JSON outputs, reasoning effort controls, and image/PDF attachments while enforcing security and budget rules.
  - *Practical Example:* An engineering team points five existing Python customer-support services at Tanpopo's /v1/chat/completions endpoint by changing only OPENAI_BASE_URL and OPENAI_API_KEY; requests seamlessly route across OpenAI, Anthropic, and AWS Bedrock with full token metering and zero application code edits.
  - *Explicit Trade-Offs:* Unifying diverse providers behind /v1/chat/completions maximizes portability, but vendor-exclusive features (such as GPT-5.4+/6.2 active reasoning combined with tools) cannot be expressed natively in legacy chat schemas without upward translation (FEAT-003) or native vendor endpoints (FEAT-002).
- **4. User Problem & Business Value:** Eliminates per-vendor SDK lock-in and bespoke wrapper code across teams, allowing organizations to adopt centralized authentication, residency controls, and FinOps cost tracking across existing LLM workloads immediately.
- **5. Target Personas:** `PER-01 (Solo Builder / First-Time Self-Hoster)`, `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-10 (Non-Human Workload / CI Runner / Autonomous Agent)`
- **6. Existing RC9 Behavior & Limitations:** COMPLETE_FOR_RC9_SCOPE in internal/ingress/http/chat_completions.go [SOURCE_VERIFIED, RUNTIME_VERIFIED]: enforces CR-09 automatic stream_options.include_usage injection, strict JSON schema capability verification (CT-08), and <=32 MiB host RAM blob_ref splicing for >64 KiB base64 attachments. Limitations: reasoning_effort enum is restricted to [low, medium, high] (omitting none, minimal, xhigh, max), annotations/citations fields are omitted, and routeAllowlistPermits (:1174) lacks the 'rerank' shorthand [SOURCE_VERIFIED].
- **7. Proposed Complete Functional Behavior:** Retain RC9's hardened /v1/chat/completions execution pipeline while expanding reasoning_effort validation to [none, minimal, low, medium, high, xhigh, max], preserving structured web/document citations and annotations across compliant adapters, and aligning modality allowlist shorthand checks [DESIGN_PROPOSAL].
- **8. User-Facing Subfeatures:**
  - Synchronous and Server-Sent Events (SSE) streaming chat completions with automatic usage-chunk injection.
  - Parallel tool/function calling and strict JSON Schema structured output validation gated by CT-08 capability checks.
  - Expanded reasoning_effort controls (none through max) and normalized citation/annotation response metadata.
  - Large multimodal base64 image/document host splicing (ARCH-017) keeping guest Wasm adapter memory <=16 MiB.
- **9. End-to-End Lifecycle:** Inbound request authenticates via Virtual Key or Workload Identity -> policy, residency, and capability admission -> Phase-1 FinOps budget hold -> Tier-1/Tier-2 adapter transformation & BlindBroker dispatch -> streaming SSE pump with delta holds -> Phase-2 exact integer settlement and metadata trace recording.
- **10. Primary & Alternative User Journeys:** Primary: App sends POST /v1/chat/completions with stream=true and tools; Tanpopo streams normalized chunks and settles exact usage. Alternative: Caller requests response_format: json_schema against a model lacking verified structured-output support; Tanpopo rejects pre-dispatch with HTTP 400 capability_unsupported (CT-08) rather than silently returning unvalidated text.
- **11. Administrative & Permission Boundaries:** Data Plane invocation requires a valid Virtual Key (vk_tp_*) or federated workload token explicitly scoped to the PROVIDER plane and permitted logical model/route; Admin roles configure underlying routes and policies without exposing upstream credentials.
- **12. Automation Opportunities:** Automatically injects stream_options.include_usage=true upstream even when omitted by legacy clients so streaming token settlement stays exact without client-side changes.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Strict tenant isolation on in-memory multimodal blob references, bounded request body enforcement (MaxRequestBodyBytes), deterministic error codes, and zero-prompt-persistence tracing by default.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Sits directly on the latency-critical Data Plane ingress path and coordinates core auth, policy, budget reservation, host multimodal blob splicing, and SSE streaming.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Interactive testing and multi-model comparison in Studio Playground (FEAT-124); trace inspection in TracesPage (FEAT-047).
  - *CLI:* Smoke verification via tanpopo doctor / smoke-test and local curl snippets generated during quickstart.
  - *Admin / Data-Plane API:* POST /v1/chat/completions and GET /v1/models in openapi/dataplane-v1.yaml.
  - *GitOps / Terraform / Packs:* Model allowlists and capability assurance requirements declared on RoutePolicy and VirtualKey manifests.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on ARCH-008 (BlindHostCredentialBroker) and ARCH-017 (blob.Store zero-copy multimodal splicing).`, `Integrates with FEAT-003 (upward translation to /v1/responses when GPT-5.4+/6.2 tools + reasoning are combined).`, `Integrates with FEAT-007 (FMEA-01 SSE framing) and FEAT-037 (two-phase FinOps budget kernel).` · Related Decisions: None (Standard Catalog Review)
- **17. Failure & Recovery Behavior:** Pre-first-byte upstream 429/5xx or TTFT timeout triggers deterministic route failover across remaining healthy targets; mid-stream disconnects emit structured SSE error frames without synthetic [DONE] markers and settle/release partial holds cleanly.
- **18. Security, Privacy & Financial Implications:** Enforces PROVIDER plane key scoping (DEC-003), scrubs upstream error strings for accidental credential echoes, stores zero prompt bytes unless encrypted debug capture is explicitly active, and prevents unheld upstream token spend.
- **19. Preliminary Measurable Acceptance Criteria:**
  - POST /v1/chat/completions accepts reasoning_effort values [none, minimal, low, medium, high, xhigh, max] and maps or rejects unsupported values per CT-08.
  - Streaming requests omitting stream_options still record exact upstream prompt/completion/reasoning/cached token counts when supported by the provider.
  - Multimodal payloads >64 KiB up to configured MaxRequestBodyBytes stream via host blob_ref splicing without exceeding the 16 MiB Wasm sandbox ceiling.
- **20. Relevant Evidence & Research Confidence:** HIGH — Core implementation is [RUNTIME_VERIFIED] and [SOURCE_VERIFIED] in internal/ingress/http/chat_completions.go on commit ac9ae87a; enum/citation additions are straightforward [DESIGN_PROPOSAL] extensions grounded in OpenAI/Anthropic/Gemini 2026 wire specs (SRC-048, SRC-051, SRC-055).
- **21. Proposal Classification:** `KEEP` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Baseline compatibility surface for the majority of existing OpenAI-SDK applications, IDE assistants, and internal enterprise microservices; already working in RC9 and mandatory for every deployment tier. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-002 — Native Top-Level Vendor SDK Endpoints (/v1/messages, /v1beta/models/*)

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-002` · `CAP` Traceability: `CAP-002` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-01` (Universal AI Gateway) -> **Native SDK Surfaces**
- **3. Plain-Language Product Summary:** Lets developers connect official Anthropic (Claude Code, Anthropic SDK) and Google Gemini SDKs directly to Tanpopo using standard base URL environment variables without rewriting URLs or losing vendor-native features. Every native call still passes through Tanpopo's full authentication, safety guardrails, routing, and exact token budgeting.
  - *Practical Example:* A software engineer sets ANTHROPIC_BASE_URL=https://ai.corp.internal and ANTHROPIC_AUTH_TOKEN=vk_tp_live_... in Claude Code; Claude Code's native POST /v1/messages and /v1/messages/count_tokens requests work out of the box, preserving Anthropic 5-minute/1-hour prompt-cache blocks and extended thinking signatures while routing transparently to either Anthropic Direct or AWS Bedrock.
  - *Explicit Trade-Offs:* Mounting top-level vendor routes expands the Data Plane OpenAPI surface and requires careful inbound auth header normalization (x-api-key, x-goog-api-key, Authorization: Bearer) so caller tokens never leak upstream, but avoids brittle client-side URL path-rewriting proxies.
- **4. User Problem & Business Value:** In RC9, pointing official Anthropic or Google SDKs at the root gateway URL fails with 404 unless the user discovers and configures /native/anthropic or /native/gemini prefixes. Native top-level endpoints deliver zero-friction onboarding for modern coding agents and preserve 90-97.5% vendor prompt-cache read discounts.
- **5. Target Personas:** `PER-01 (Solo Builder / First-Time Self-Hoster)`, `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-08 (Agentic Application & MCP Tool Builder)`
- **6. Existing RC9 Behavior & Limitations:** MISSING at top level in RC9 openapi/dataplane-v1.yaml and internal/ingress/http/server.go [SOURCE_VERIFIED]; callers must prepend /native/anthropic/v1/messages or /native/gemini/v1beta/models/*, which also suffered split-role and path-matching issues tracked under DEF-048 / FND-013 [RUNTIME_VERIFIED].
- **7. Proposed Complete Functional Behavior:** Mount top-level POST /v1/messages, POST /v1/messages/count_tokens, and POST /v1beta/models/{model}:{action} (generateContent, streamGenerateContent, countTokens, embedContent) on the Data Plane [DESIGN_PROPOSAL]. Authenticate Virtual Keys supplied via Authorization: Bearer, x-api-key, or x-goog-api-key uniformly, strip all inbound auth headers prior to adapter execution, and delegate through the governed route/failover/FinOps pipeline so a /v1/messages call can route natively to Anthropic or adapt cleanly to AWS Bedrock/Vertex AI.
- **8. User-Facing Subfeatures:**
  - Top-level Anthropic Messages API (/v1/messages and /v1/messages/count_tokens) preserving thinking blocks and cache_control.
  - Top-level Google GenAI API (/v1beta/models/{model}:generateContent, :streamGenerateContent, :countTokens, :embedContent).
  - Unified inbound Virtual Key extraction across Bearer, x-api-key, and x-goog-api-key headers with strict header stripping.
  - Cross-provider routing from /v1/messages to Anthropic Direct, AWS Bedrock Converse, or Google Vertex Claude endpoints.
- **9. End-to-End Lifecycle:** SDK sends native vendor payload to top-level endpoint -> Gateway normalizes inbound Virtual Key and extracts logical model + capability requirements -> Route Resolver selects compliant native or bijective target -> Two-phase budget hold -> Streaming native SSE relay (message_start/content_block_delta or Gemini SSE chunks) -> Exact multi-tier usage settlement.
- **10. Primary & Alternative User Journeys:** Primary: Claude Code sends POST /v1/messages with anthropic-beta headers and 4 cache_control breakpoints; Tanpopo routes to an Anthropic connection and settles exact cache_creation/cache_read tokens. Alternative: Primary Anthropic Direct connection hits HTTP 529 overloaded; Tanpopo fails over pre-first-byte to a regional AWS Bedrock Claude deployment without requiring client retries.
- **11. Administrative & Permission Boundaries:** Requires PROVIDER-plane Virtual Key or Workload Identity token with model/route access; anthropic-beta and vendor extension headers are governed by route/adapter allowlists so unapproved beta surfaces cannot bypass policy.
- **12. Automation Opportunities:** Console Playground and IDE onboarding profiles (FEAT-118) auto-generate copy-paste environment snippets for @anthropic-ai/sdk, Claude Code, and @google/genai pointing directly at the gateway root URL.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Full parity between standalone Mode A and split Control-Plane / Data-Plane HA deployments (closing DEF-048 split-role gaps), strict 4-plane separation (DEC-003), and identical guardrail/audit coverage as /v1/chat/completions.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Top-level HTTP route mounting, multi-header authentication normalization, and unified policy/FinOps hooks must execute at the Core Data Plane ingress router.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Dialect selector in Studio Playground (/v1/messages, /v1beta/models) and SDK quickstart snippets.
  - *CLI:* tanpopo doctor / smoke-test multi-dialect verification checks.
  - *Admin / Data-Plane API:* POST /v1/messages, POST /v1/messages/count_tokens, POST /v1beta/models/{model}:{action} in dataplane-v1.yaml.
  - *GitOps / Terraform / Packs:* RoutePolicy dialect and allowed beta-header policies in declarative config manifests.
- **16. Dependencies & Cross-Domain Relationships:** `Resolves FND-013 / DEF-048 top-level SDK ergonomics alongside FEAT-004.`, `Depends on FEAT-009 and FEAT-010 (Anthropic, Gemini, Bedrock, and Vertex adapters) and FEAT-031 (cross-dialect adaptation).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Returns vendor-idiomatic structured JSON errors (Anthropic error envelope on /v1/messages; Google RPC Status envelope on /v1beta/models/*) so official SDK retry/backoff state machines behave predictably.
- **18. Security, Privacy & Financial Implications:** Prevents accidental leakage of x-api-key or x-goog-api-key headers to fallback providers, enforces CT-08 on vendor-specific parameters, and meters Anthropic 5m/1h cache writes and Gemini thought tokens accurately.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Unmodified @anthropic-ai/sdk and Claude Code clients complete streaming tool-use conversations against ANTHROPIC_BASE_URL=<gateway_origin>.
  - Unmodified @google/genai clients complete streaming generateContent and embedContent calls against GEMINI_BASE_URL=<gateway_origin>.
  - Inbound x-api-key and x-goog-api-key Virtual Keys never appear in outbound upstream request headers or error logs.
- **20. Relevant Evidence & Research Confidence:** HIGH — Competitor support (Solo agentgateway, Bifrost, Ollama v0.40) and SDK base-URL behaviors are [SOURCE_VERIFIED] across SRC-011, SRC-019, SRC-044, SRC-051, and SRC-055; Tanpopo route mounting is [DESIGN_PROPOSAL].
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Eliminates the #1 Day-1 developer friction for Claude Code, @anthropic-ai/sdk, and @google/genai users who expect standard ANTHROPIC_BASE_URL and GEMINI_BASE_URL paths without non-standard /native/{dialect} prefixes. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-003 — Full Agentic /v1/responses Plane & Upward Reasoning+Tools Adaptation

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-003` · `CAP` Traceability: `CAP-003` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-01` (Universal AI Gateway) -> **Agentic Responses & Reasoning**
- **3. Plain-Language Product Summary:** Upgrades Tanpopo's /v1/responses endpoint from a basic single-turn wrapper into a complete multi-provider agentic interface supporting multi-step reasoning, tool calls, encrypted stateless state, and response caching. It also automatically upgrades legacy /v1/chat/completions requests that combine tools and reasoning when routed to frontier models that require /v1/responses.
  - *Practical Example:* A team upgrades an internal coding assistant from GPT-4o to GPT-5.4 while still calling /v1/chat/completions with tools and reasoning_effort='high'; instead of failing upstream with an OpenAI 400 error, Tanpopo transparently adapts the call upward to /v1/responses and translates the streamed response back cleanly.
  - *Explicit Trade-Offs:* Bijective item-based translation across OpenAI, Anthropic, Gemini, Bedrock, and Foundry adds adapter test surface area, and hosted provider-side tools (such as OpenAI's remote mcp block or hosted computer/file containers) must be explicitly governed or rejected under DEC-003 so callers cannot bypass Tanpopo's MCP plane.
- **4. User Problem & Business Value:** Prevents production breakages when teams adopt OpenAI Agents SDK or migrate legacy chat+tool apps onto GPT-5.x/6.x, while keeping enterprises vendor-independent by running /v1/responses workloads seamlessly on Claude, Gemini, Bedrock, or Microsoft Foundry.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-08 (Agentic Application & MCP Tool Builder)`, `PER-10 (Non-Human Workload / CI Runner / Autonomous Agent)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in internal/ingress/http/responses.go:42-595 [SOURCE_VERIFIED]: stateless single-turn OpenAI-only shim; skips ExactCache; restricts reasoning_effort to [low, medium, high]; lacks CT-08 validation on unsupported previous_response_id, conversation, or background fields when translating; and all 3 Tier-2 C/Wasm adapters ignore _tanpopo_modality [SOURCE_VERIFIED].
- **7. Proposed Complete Functional Behavior:** Implement full stateless /v1/responses item streaming (message, reasoning with store:false encrypted_content, function_call, function_call_output) across Tier-1 and Tier-2 official adapters [DESIGN_PROPOSAL]. Auto-adapt /v1/chat/completions (tools + active reasoning) upward to upstream /v1/responses when required by model metadata, wire ExactCache (FEAT-044), reject unconfigured stateful IDs loudly per CT-08 unless opt-in gateway thread hydration (FEAT-130, DEC-CAND-06) is enabled, and fence hosted remote mcp blocks through Tanpopo's MCP governance policy.
- **8. User-Facing Subfeatures:**
  - Multi-provider POST /v1/responses streaming (response.created, response.output_item.*, response.completed) across OpenAI, Anthropic, Gemini, Bedrock, and Foundry.
  - Transparent upward adaptation from /v1/chat/completions (tools + reasoning_effort) to upstream /v1/responses for GPT-5.4+/6.2.
  - Stateless Zero-Data-Retention default (store: false + include: ['reasoning.encrypted_content']) preserving multi-turn reasoning.
  - Policy gate intercepting or rejecting unproxied upstream remote MCP tool blocks to preserve DEC-003 4-plane security.
- **9. End-to-End Lifecycle:** Request arrives on /v1/responses (or upward-adapted /v1/chat/completions) -> CT-08 parameter & hosted-tool policy validation -> ExactCache check -> Route selection & Phase-1 hold -> Tier-1/Tier-2 Responses item transform -> Semantic SSE event streaming -> Exact token/tool settlement.
- **10. Primary & Alternative User Journeys:** Primary: OpenAI Agents SDK sends stateless multi-turn /v1/responses items with encrypted reasoning content; Tanpopo routes to Azure/Foundry or OpenAI with store=false. Alternative: Caller supplies previous_response_id on a stateless project; Tanpopo returns deterministic HTTP 400 stateful_thread_disabled with guidance to enable FEAT-130 or pass stateless history.
- **11. Administrative & Permission Boundaries:** Governed by PROVIDER-plane Virtual Keys; enabling stateful thread storage (FEAT-130) or permitting hosted provider tools requires explicit ORG_OWNER / SECURITY_REVIEWER policy opt-in.
- **12. Automation Opportunities:** Model Intelligence metadata automatically flags models requiring upward chat-to-responses adaptation so operators do not have to write manual rewrite rules.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Zero silent parameter dropping (CT-08), default upstream store:false ZDR posture, Wasm <=16 MiB memory compliance across Tier-2 adapters, and full OpenTelemetry span attribution.
- **14. Core vs Extension Placement Candidate:** `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` — Core Data Plane validates /v1/responses contracts, cache integration, and CT-08 stateful guards, while Tier-1/Tier-2 provider adapters map canonical Responses items to vendor wire protocols.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Responses API mode in Studio Playground with reasoning-item inspector and trace waterfall spans.
  - *CLI:* Smoke tests covering /v1/responses streaming and upward chat-to-responses adaptation.
  - *Admin / Data-Plane API:* POST /v1/responses (plus subroutes paired with FEAT-125, FEAT-130, FEAT-131) in dataplane-v1.yaml.
  - *GitOps / Terraform / Packs:* RoutePolicy and Policy settings governing store:false ZDR enforcement and hosted-tool blocking.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-005 (multi-turn reasoning signature continuity) and FEAT-009/FEAT-010 (Tier-1/Tier-2 adapter updates).`, `Coordinates with FEAT-125 (background:true async responses), FEAT-130 (stateful previous_response_id, DEC-CAND-06), and FEAT-057 (remote MCP bypass firewall).` · Related Decisions: `DEC-CAND-06`
- **17. Failure & Recovery Behavior:** Fails closed with HTTP 400 on unsupported stateful/hosted-tool fields rather than silently stripping them; supports pre-first-event cross-provider failover across adapters advertising tested Responses capability.
- **18. Security, Privacy & Financial Implications:** Enforces store:false by default so upstream vendors do not retain sensitive enterprise agent threads, blocks unmonitored third-party remote MCP exfiltration channels, and meters reasoning + tool tokens accurately.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Tier-2 Wasm adapters (Anthropic, Gemini, AWS Bedrock) and Tier-1 OpenAI/Foundry adapters pass streaming /v1/responses tool-loop conformance tests.
  - A /v1/chat/completions request with tools and reasoning_effort='high' targeting GPT-5.4+ automatically executes via upstream /v1/responses and returns valid chat completion chunks.
  - Requests containing unapproved hosted mcp tool blocks or unsupported previous_response_id values fail pre-dispatch with explicit 400/403 diagnostic codes.
- **20. Relevant Evidence & Research Confidence:** HIGH — RC9 gaps in responses.go and adapter.c are [SOURCE_VERIFIED] on ac9ae87a; OpenAI Responses API constraints on GPT-5.4+/6.2 are verified in SRC-048; multi-adapter expansion is [DESIGN_PROPOSAL].
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — OpenAI's Responses API is the primary 2026 agentic surface, and frontier OpenAI models (GPT-5.4+/6.2) disallow combining tools with active reasoning on legacy /v1/chat/completions; incomplete support breaks enterprise agent migrations. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-004 — Full-Method & Query-Aware /native/{dialect}/* Passthrough

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-004` · `CAP` Traceability: `CAP-004` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-01` (Universal AI Gateway) -> **Native Passthrough Plane**
- **3. Plain-Language Product Summary:** Provides a governed escape hatch (/native/{dialect}/*) so teams can call brand-new or vendor-specific API endpoints directly while still keeping API keys hidden in Tanpopo's Vault and tracking usage costs. It fixes RC9's query-string, HTTP-method, and path-prefix restrictions so Azure, AWS Bedrock, OpenAI, Anthropic, and Gemini native calls work reliably.
  - *Practical Example:* An enterprise Azure AI team calls /native/azure-openai/openai/deployments/gpt-4o/chat/completions?api-version=2025-04-01-preview and GET /native/openai/v1/models; Tanpopo validates the allowlisted query parameters, injects Entra ID or API credentials blindly, and meters token spend without buffering large payloads.
  - *Explicit Trade-Offs:* Arbitrary native endpoints may occasionally return unrecognized usage schemas on brand-new vendor betas; Tanpopo preserves safety by requiring explicit AllowedNativePaths allowlists and emitting UNMETERED_FLAGGED alerts plus X-Tanpopo-Usage-Accuracy trailers whenever exact token extraction is unavailable.
- **4. User Problem & Business Value:** Eliminates the common enterprise dilemma where teams bypass the central AI gateway with raw vendor keys whenever a provider ships a new beta feature, query parameter, or non-POST endpoint.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in internal/ingress/native/handler.go [SOURCE_VERIFIED, RUNTIME_VERIFIED]: (1) hardcodes query allowlists to alt={sse|json} and beta={true|false} (:1798-1818), blocking Azure ?api-version=; (2) mandates extracting a JSON model hint (:300), breaking GET /native/openai/v1/models and multipart requests; (3) inspector.go:157 prepends '/' ('/model/...') while aws-bedrock/src/adapter.c:654 checks 'model/' without '/', breaking Bedrock native Converse; and (4) split-role Data Plane parity had defects tracked in DEF-048 / FND-013.
- **7. Proposed Complete Functional Behavior:** Allow adapter manifests to declare typed query-parameter allowlists (such as api-version with regex pattern validation), support model-optional GET/DELETE and streaming multipart/form-data native routes when permitted by AllowedNativePaths, standardize leading-slash inspector paths across Host and all Wasm adapters, and verify identical behavior in standalone and split CP/DP topologies [DESIGN_PROPOSAL].
- **8. User-Facing Subfeatures:**
  - Manifest-driven query parameter allowlists unblocking Azure ?api-version= and vendor pagination/filter parameters.
  - Support for GET, DELETE, and streaming multipart/form-data requests on allowlisted native paths without mandatory JSON model bodies.
  - Consistent '/' path normalization across Host native inspector and aws-bedrock Converse / ConverseStream.
  - Real-time usage extraction with automatic X-Tanpopo-Usage-Accuracy trailer and ALT-UNMETERED-SPEND alerting on unknown beta payloads.
- **9. End-to-End Lifecycle:** Caller invokes /native/{dialect}/{upstreamPath}?query -> Dot-segment & encoded-slash traversal rejection -> VirtualKey + AllowedNativePaths + manifest query allowlist check -> Phase-1 conservative/configured budget hold -> BlindHostCredentialBroker streaming execution -> Adapter native usage inspection -> Phase-2 settlement.
- **10. Primary & Alternative User Journeys:** Primary: Bedrock SDK invokes POST /native/aws-bedrock/model/anthropic.claude-sonnet-4/converse-stream; Tanpopo signs with AWS SigV4, streams binary EventStream frames (ARCH-017), and settles exact usage. Alternative: Caller attempts /native/openai/v1/../admin or an unallowlisted query param; Tanpopo rejects immediately with HTTP 400/403 before socket dial.
- **11. Administrative & Permission Boundaries:** Native passthrough is disabled unless RoutePolicy / EffectivePolicy explicitly allowlists AllowedNativePaths; ORG_OWNER and SECURITY_REVIEWER control path globs and unmetered-fallback policies.
- **12. Automation Opportunities:** Official adapter manifests ship pre-vetted native path templates and query parameter schemas so operators can enable safe native passthrough via Console checkboxes rather than hand-crafting regexes.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Zero full-body RAM buffering on streaming/multipart uploads, strict path-traversal protection, split-role CP/DP parity (FND-013), and immutable audit logging of unmetered native fallbacks.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Native path normalization, traversal sanitization, query-parameter allowlisting, and binary/multipart streaming relay execute directly inside internal/ingress/native.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Allowed Native Paths policy selector in Route/Policy drawers and usage-accuracy indicator in TracesPage.
  - *CLI:* Native passthrough smoke tests across standalone and split CP/DP modes.
  - *Admin / Data-Plane API:* ANY /native/{dialect}/{upstreamPath} in openapi/dataplane-v1.yaml.
  - *GitOps / Terraform / Packs:* allowed_native_paths and unmetered_policy fields in declarative RoutePolicy / Policy configs.
- **16. Dependencies & Cross-Domain Relationships:** `Closes FND-013 (DEF-048) alongside FEAT-002 and FEAT-009.`, `Depends on ARCH-008 (BlindHostCredentialBroker) and ARCH-017 (binary AWS EventStream framing).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Rejects path-traversal or disallowed query parameters fail-closed (400/403); if an allowlisted beta response omits recognizable token counts, settles configured conservative floor or marks UNMETERED_FLAGGED with Trailer: X-Tanpopo-Usage-Accuracy: unmetered_flagged.
- **18. Security, Privacy & Financial Implications:** Prevents SSRF and URL path smuggling via strict canonicalization while ensuring raw upstream keys remain sealed inside BlindHostCredentialBroker even on day-zero vendor endpoints.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Azure ?api-version= native requests, GET /native/openai/v1/models, and AWS Bedrock /native/aws-bedrock/model/*/converse(-stream) succeed end-to-end in both Mode A and split CP/DP HA.
  - Encoded slashes (%2f, %5c) and dot-segments (..) in /native/* paths are rejected 100% of the time with HTTP 400.
  - Streaming native responses emit accurate X-Tanpopo-Usage-Accuracy trailers across OpenAI, Anthropic, Gemini, and Bedrock.
- **20. Relevant Evidence & Research Confidence:** HIGH — All three RC9 native handler defects are [SOURCE_VERIFIED] at handler.go:300,1798, inspector.go:157, and adapter.c:654 on commit ac9ae87a, and [RUNTIME_VERIFIED] in QA-005 DEF-048.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Guarantees Day-0 access to brand-new vendor endpoints (Azure ?api-version=, AWS Bedrock Converse, GET model listings, multipart uploads) under Vault and FinOps governance without waiting for unified schema updates. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-005 — Multi-Turn Reasoning Signature & Encrypted State Continuity

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-005` · `CAP` Traceability: `CAP-005` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-01` (Universal AI Gateway) -> **Agentic Responses & Reasoning**
- **3. Plain-Language Product Summary:** Preserves the cryptographic 'thinking receipts' that modern reasoning models (Gemini 3/4, Claude 4/5, and OpenAI GPT-5/6 in Zero-Data-Retention mode) attach to tool-calling turns. Without this continuity, multi-step coding and research agents fail mid-task with validation errors or lose their chain-of-thought context.
  - *Practical Example:* A developer's agent calls Gemini 3.1 Pro via Tanpopo's unified chat API, receives two tool calls, executes them locally, and sends the tool results back; Tanpopo preserves Gemini's thoughtSignature across turns (even for legacy OpenAI SDKs via an optional short-lived tool_call_id cache) so Gemini completes the workflow without HTTP 400 errors.
  - *Explicit Trade-Offs:* Cryptographic reasoning blobs are vendor- and model-family-specific: if a route fails over mid-conversation from Anthropic to OpenAI, forwarding Anthropic's signature would fail upstream, so Tanpopo must either fail loudly per CT-08 or strip foreign signatures only when a route explicitly opts into stateless re-reasoning.
- **4. User Problem & Business Value:** Solves silent reasoning degradation, prompt-cache invalidation, and hard HTTP 400 failures in multi-turn enterprise agent loops while upholding Zero-Data-Retention (store:false) privacy postures.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-08 (Agentic Application & MCP Tool Builder)`
- **6. Existing RC9 Behavior & Limitations:** MISSING on unified /v1/chat/completions and /v1/responses in RC9 [SOURCE_VERIFIED]: strict request/response parsers drop Gemini thoughtSignature, Anthropic thinking signature / redacted_thinking, and OpenAI reasoning.encrypted_content across unified turns.
- **7. Proposed Complete Functional Behavior:** Extend unified message and /v1/responses item schemas with a vendor-tagged opaque reasoning_state envelope (carrying Anthropic signature + redacted_thinking, Gemini thought_signature, and OpenAI encrypted_content) [DESIGN_PROPOSAL]. Provide an optional Project-DEK-encrypted short-TTL Valkey/RAM bridge keyed by (org_id, project_id, tool_call_id) so unmodified legacy OpenAI SDKs dropping unknown assistant fields still round-trip Gemini thoughtSignature seamlessly. Enforce strict cross-vendor replay guards on failover.
- **8. User-Facing Subfeatures:**
  - Lossless round-tripping of Anthropic signature/redacted_thinking, Gemini thoughtSignature, and OpenAI encrypted_content.
  - Optional short-TTL encrypted (org_id, project_id, tool_call_id) signature bridge for legacy OpenAI SDK clients.
  - Cross-vendor signature isolation preventing Anthropic/Gemini/OpenAI blobs from leaking to foreign providers on failover.
  - Configurable route failover policy: FAIL_CLOSED_CT08 (default) vs EXPLICIT_STATELESS_REREASON_FALLBACK.
- **9. End-to-End Lifecycle:** Turn 1 response extracts vendor reasoning signature -> returned inline in opaque envelope (and optionally sealed under Project DEK in short-TTL cache if bridge enabled) -> Turn 2 request validates vendor tag against selected target deployment -> re-injects native cryptographic signature or applies configured failover rule.
- **10. Primary & Alternative User Journeys:** Primary: Multi-turn Claude or Gemini agent echoes assistant message envelope; adapter re-attaches exact thinking signature and preserves upstream prompt cache. Alternative: Turn 2 fails over from Anthropic to OpenAI; under default CT-08 policy, Tanpopo rejects cross-vendor signature replay loudly unless the route explicitly enables stateless re-reasoning fallback (recorded in trace metadata).
- **11. Administrative & Permission Boundaries:** Signature bridge TTL and cross-vendor re-reasoning fallback behavior are controlled on RoutePolicy by TEAM_ADMIN / ORG_OWNER; cached bridge entries are strictly scoped to (org_id, project_id).
- **12. Automation Opportunities:** Automatically tags every reasoning signature with provider dialect and model family so operators never manually configure per-vendor header or field mappings.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Zero plaintext reasoning persistence on disk; any short-TTL tool_call_id signature bridge entries are AES-256-GCM sealed with the Project DEK and bounded in size to prevent memory exhaustion.
- **14. Core vs Extension Placement Candidate:** `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` — Core Data Plane defines vendor-tagged opaque signature envelopes and cross-vendor failover safety rules, while provider adapters extract and re-inject vendor-specific cryptographic fields.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Route failover re-reasoning toggle in RoutingPage and signature-continuity badge in Studio Playground / TracesPage.
  - *CLI:* Multi-turn tool + reasoning conformance test cases in tanpopo smoke-test.
  - *Admin / Data-Plane API:* Vendor-tagged reasoning_state / encrypted_content fields on /v1/chat/completions and /v1/responses.
  - *GitOps / Terraform / Packs:* stateless_rereason_on_failover and tool_signature_bridge_ttl_s on RoutePolicy manifests.
- **16. Dependencies & Cross-Domain Relationships:** `Required by FEAT-003 (/v1/responses) and FEAT-031 (cross-dialect failover adaptation under CT-08).`, `Uses Vault Project DEK (FEAT-059) when optional tool_call_id bridge is enabled.` · Related Decisions: `DEC-CAND-06`
- **17. Failure & Recovery Behavior:** Tampered or truncated client-echoed signature blobs return upstream 400 validation errors cleanly without tripping healthy circuit breakers; expired bridge entries return a clear diagnostic code.
- **18. Security, Privacy & Financial Implications:** Prevents cross-vendor leakage of encrypted chain-of-thought state during provider outages and preserves multi-turn prompt-cache read hits (avoiding expensive full-context cache misses).
- **19. Preliminary Measurable Acceptance Criteria:**
  - 3-turn tool-calling loops against Gemini 3/4, Claude 4/5 (extended thinking), and OpenAI GPT-5/6 (store:false) complete with zero HTTP 400 signature errors.
  - Cross-vendor failover never transmits Provider A's cryptographic signature blob to Provider B.
  - Legacy OpenAI SDK clients omitting unknown assistant fields succeed against Gemini tool loops when the encrypted tool_call_id bridge is enabled.
- **20. Relevant Evidence & Research Confidence:** HIGH — Upstream signature enforcement in Gemini 3/4 (SRC-055), Anthropic Claude (SRC-051), and OpenAI ZDR (SRC-048) is [SOURCE_VERIFIED]; RC9 parser omission is [SOURCE_VERIFIED] on ac9ae87a.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Gemini 3/4 strictly requires functionCall thoughtSignature across tool turns (returning HTTP 400 if stripped), Anthropic Claude 4/5 verifies thinking signatures, and OpenAI ZDR relies on reasoning.encrypted_content; omitting them breaks production tool-calling agents. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-006 — Payload-Fingerprinted Request Idempotency (Idempotency-Key)

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-006` · `CAP` Traceability: `CAP-006` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-01` (Universal AI Gateway) -> **Request Integrity & Deduplication**
- **3. Plain-Language Product Summary:** Ensures that if an application retries a timed-out AI request using the same Idempotency-Key, Tanpopo never runs or bills the AI call twice. It returns the sealed original response on safe retries, blocks concurrent duplicates while the first call is still running, and rejects accidental key reuse across different prompts.
  - *Practical Example:* A financial document-processing pipeline experiences a transient NAT gateway blip and retries POST /v1/chat/completions with Idempotency-Key: inv-88412; Tanpopo detects the completed key+SHA-256 payload fingerprint, replays the encrypted response at USD 0 additional upstream spend, and marks idempotency_replayed.
  - *Explicit Trade-Offs:* Caching idempotent response bodies for 24 hours consumes bounded Valkey/SQL storage and requires Project-DEK AEAD encryption/decryption; streaming SSE requests can lock in-progress keys (409) but replay finalized payloads only within configured payload-size bounds (<=1 MiB).
- **4. User Problem & Business Value:** Most open-source AI gateways ignore Idempotency-Key headers, causing retry storms to multiply token bills and emit duplicate downstream side effects. Payload-fingerprinted idempotency provides Stripe/RFC-grade financial determinism.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-07 (FinOps Controller & IT Finance Analyst)`, `PER-10 (Non-Human Workload / CI Runner / Autonomous Agent)`
- **6. Existing RC9 Behavior & Limitations:** COMPLETE_FOR_RC9_SCOPE functionally in internal/ingress/idempotency/guard.go [SOURCE_VERIFIED, RUNTIME_VERIFIED]: enforces HTTP 409 on concurrent in-flight keys, HTTP 422 on SHA-256 request payload mismatch, and 24-hour TTL with Project-DEK encryption. Scalability limitation: backed solely by synchronous SQL idempotency_records writes (2 SQL transactions per call), addressed by internal enabler ARCH-001 (CAP-035) [SOURCE_VERIFIED].
- **7. Proposed Complete Functional Behavior:** Preserve RC9's strict (org_id, project_id, virtual_key_id, idempotency_key) scoping, 409 in-flight lock, and 422 SHA-256 payload-mismatch semantics across /v1/chat/completions, /v1/responses, /v1/messages, and batch creation endpoints, while accelerating hot-path lock/replay via Valkey (ARCH-001) with automatic SQLite/PostgreSQL fallback in standalone Mode A [DESIGN_PROPOSAL].
- **8. User-Facing Subfeatures:**
  - Exact-once execution and Project-DEK-encrypted response replay for non-streaming POST requests carrying Idempotency-Key.
  - HTTP 409 Conflict guard when a duplicate retry arrives while the original request is still in progress.
  - HTTP 422 Unprocessable Entity guard when a caller accidentally reuses an Idempotency-Key with a mutated request body.
  - X-Tanpopo-Idempotent-Replay: true response header and zero-cost trace attribution on replayed responses.
- **9. End-to-End Lifecycle:** Request with Idempotency-Key arrives -> compute canonical SHA-256 payload digest -> acquire atomic Valkey/SQL IN_PROGRESS lock (or return 409 / 422 / decrypted replay) -> execute inference & settle FinOps -> seal completed status + response under Project DEK for 24h TTL -> reap automatically upon expiration or tenant shred.
- **10. Primary & Alternative User Journeys:** Primary: Client retries identical payload after network drop; receives 200 OK with X-Tanpopo-Idempotent-Replay: true and zero duplicate ledger charge. Alternative: Upstream returns transient 502/503; Tanpopo releases the idempotency lock immediately so the client's next retry can execute cleanly.
- **11. Administrative & Permission Boundaries:** Strictly partitioned by (org_id, project_id, virtual_key_id) so one Virtual Key can never probe or replay another key's idempotent responses even within the same project.
- **12. Automation Opportunities:** Automatic lock release on retryable 5xx/429 upstream failures and automatic TTL reaping via ARCH-001 / FEAT-051.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** High-throughput Valkey coordination in Mode B HA clusters (ARCH-001), deterministic Mode A SQLite fallback, and immediate cryptographic unreachability when a Project DEK is shredded (FEAT-074).
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Must intercept ingress requests prior to Phase-1 FinOps reservation and upstream dispatch, sealing cached responses under the tenant's Project DEK.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Idempotent replay indicator badge in TracesPage waterfall inspector.
  - *CLI:* Verified in smoke-test and chaos resilience drills.
  - *Admin / Data-Plane API:* Idempotency-Key request header and X-Tanpopo-Idempotent-Replay response header in dataplane-v1.yaml.
  - *GitOps / Terraform / Packs:* Idempotency TTL and max cached payload bytes configurable via gateway / policy settings.
- **16. Dependencies & Cross-Domain Relationships:** `Accelerated at cluster scale by ARCH-001 (CAP-035 Distributed Valkey Idempotency-Key Coordination Engine).`, `Depends on FEAT-059 (Vault Project DEK envelope encryption) and FEAT-051 (TTL retention cleaner).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** If Valkey is temporarily unreachable in HA mode, degrades gracefully to PostgreSQL idempotency_records locking without dropping or double-charging requests.
- **18. Security, Privacy & Financial Implications:** Stored response bodies are AES-256-GCM encrypted under Project DEK (never plaintext in Valkey or SQL), eliminating duplicate financial charges on retried requests.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Retrying a completed POST request with identical Idempotency-Key and body returns the exact original response with 0 additional budget settlement.
  - Reusing an active Idempotency-Key with a different prompt or model returns HTTP 422 payload_mismatch.
  - Failed 5xx upstream attempts release the idempotency lock so subsequent retries execute normally.
- **20. Relevant Evidence & Research Confidence:** HIGH — Guard logic is [RUNTIME_VERIFIED] and [SOURCE_VERIFIED] in internal/ingress/idempotency/guard.go on ac9ae87a; Valkey backing (ARCH-001) is standard distributed systems practice.
- **21. Proposal Classification:** `KEEP` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Prevents double-billing and duplicate tool-call generation when enterprise batch workers or payment-adjacent microservices retry timed-out HTTP POST requests; already working in RC9 and paired with ARCH-001 for HA scale. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-007 — Deterministic SSE Mid-Stream Error Framing & Usage Accuracy Trailers

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-007` · `CAP` Traceability: `CAP-007` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-01` (Universal AI Gateway) -> **Streaming Integrity & Accounting**
- **3. Plain-Language Product Summary:** Protects applications when an AI provider crashes or disconnects halfway through streaming an answer. Instead of closing the stream as if the answer finished normally, Tanpopo sends an explicit error event, withholds the completion marker, and attaches an HTTP trailer stating whether token usage was exact or estimated.
  - *Practical Example:* A self-hosted vLLM worker restarts mid-generation after emitting 400 tokens of a SQL migration script; Tanpopo emits a structured mid-stream SSE error event without sending data: [DONE], preventing the caller service from committing a half-written SQL script, and settles the partial stream with Trailer: X-Tanpopo-Usage-Accuracy: estimated.
  - *Explicit Trade-Offs:* Some older HTTP/1.0 intermediaries strip HTTP trailers; therefore, Tanpopo pairs HTTP Trailer: X-Tanpopo-Usage-Accuracy with in-band SSE error events and server-side trace/settlement accuracy flags so downstream systems can verify completion regardless of proxy topology.
- **4. User Problem & Business Value:** Addresses a widespread defect class in competing proxies (such as LiteLLM #54466 treating mid-stream vLLM aborts as clean stops [SOURCE_VERIFIED, SRC-005]), safeguarding downstream data integrity and FinOps auditability.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** COMPLETE_FOR_RC9_SCOPE in internal/ingress/sse/pump.go (FMEA-01) and internal/ingress/native/handler.go (DEF-083 Trailer: X-Tanpopo-Usage-Accuracy) [RUNTIME_VERIFIED, SOURCE_VERIFIED]. Residual verification gap: automated E2E trailer assertions need expansion across native Anthropic SSE, Gemini SSE, and binary AWS Bedrock EventStream (RR-014, RR-024, ARCH-017) [SOURCE_VERIFIED].
- **7. Proposed Complete Functional Behavior:** Keep FMEA-01 mid-stream abort framing (structured error event + zero synthetic [DONE] + 15s keepalive comments + bounded 2 MiB line buffers) and X-Tanpopo-Usage-Accuracy trailers (exact | estimated | unmetered_flagged), extending full E2E verification across top-level /v1/messages, /v1beta/models/*, /v1/responses, and length-prefixed AWS Bedrock EventStream (ARCH-017) [DESIGN_PROPOSAL].
- **8. User-Facing Subfeatures:**
  - FMEA-01 mid-stream error event emission and strict suppression of synthetic [DONE] / message_stop on upstream abort.
  - HTTP Trailer: X-Tanpopo-Usage-Accuracy (exact, estimated, unmetered_flagged) across unified and native streaming routes.
  - Periodic 15-second SSE comment keepalives preventing idle load-balancer socket drops during long reasoning pauses.
  - Bounded per-stream line/frame buffers protecting gateway pods from malformed multi-megabyte upstream chunks.
- **9. End-to-End Lifecycle:** Stream opens with Trailer: X-Tanpopo-Usage-Accuracy declared -> chunks pumped with periodic keepalives and incremental DeltaReserve holds -> clean upstream finish or mid-stream error frame -> final trailer written matching usage_settlements.usage_accuracy.
- **10. Primary & Alternative User Journeys:** Primary: Provider streams final usage chunk; trailer reports exact and ledger settles exact provider counts. Alternative: Upstream TCP resets mid-stream before final usage frame; pump emits structured SSE error, omits [DONE], counts emitted UTF-8/token approximation, sets trailer to estimated, and logs mid_stream_abort=true.
- **11. Administrative & Permission Boundaries:** Always active on Data Plane streaming responses; FINOPS_VIEWER and DEVELOPER roles inspect exact vs estimated vs unmetered_flagged counts in FinOps and Traces views.
- **12. Automation Opportunities:** Automatically triggers ALT-UNMETERED-SPEND telemetry counters and optional notifications when streams degrade to unmetered_flagged.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Consistent behavior across HTTP/1.1 chunked and HTTP/2 streams, binary EventStream framing parity (ARCH-017), and deterministic orphan-hold cleanup if both client and upstream disconnect simultaneously.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Implemented directly inside the Core Data Plane SSE/EventStream pump and HTTP trailer framing layer.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Mid-stream abort and usage-accuracy filter/badges in TracesPage (FEAT-048) and FinOps rollups.
  - *CLI:* Chaos drill assertions in Hermetic Loopback Chaos Studio (FEAT-141).
  - *Admin / Data-Plane API:* SSE error event contract and Trailer: X-Tanpopo-Usage-Accuracy in dataplane-v1.yaml.
  - *GitOps / Terraform / Packs:* Stream idle/keepalive parameters in gateway runtime configuration.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on ARCH-017 (CAP-142 binary AWS EventStream framing) for >2 MiB Bedrock ConverseStream frames.`, `Feeds accuracy metadata into FEAT-037 (FinOps settlement) and FEAT-048 (Trace filtering).` · Related Decisions: None (Standard Catalog Review)
- **17. Failure & Recovery Behavior:** Never retries or fails over once byte #1 of SSE content has already been flushed to the client (preventing garbled spliced text); fails cleanly with mid-stream error event so the caller can decide whether to retry.
- **18. Security, Privacy & Financial Implications:** Scrubs upstream disconnect error text via BlindBroker secret redactor and prevents mid-stream aborts from leaving dangling SM-07 budget reservations.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Simulated mid-stream upstream TCP reset across OpenAI, Anthropic, Gemini, and Bedrock streams never emits [DONE] or clean message_stop.
  - Trailer: X-Tanpopo-Usage-Accuracy matches usage_settlements.usage_accuracy across all streaming dialects in automated E2E tests.
  - Long 45-second pre-first-token reasoning pauses emit SSE keepalive comments without corrupting JSON data frames.
- **20. Relevant Evidence & Research Confidence:** HIGH — FMEA-01 and DEF-083 are [RUNTIME_VERIFIED] in RC9 QA-005 and [SOURCE_VERIFIED] in sse/pump.go; Bedrock binary EventStream framing extension is [DESIGN_PROPOSAL] closing RR-014/RR-024.
- **21. Proposal Classification:** `KEEP` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Prevents truncated medical, legal, or code-generation streams from being silently saved by applications as complete responses, and communicates exact vs estimated billing accuracy transparently. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-008 — Specialized /v1/fim/completions, /v1/decisions & /v1/moderations Endpoints

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-008` · `CAP` Traceability: `CAP-008` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-01` (Universal AI Gateway) -> **Specialized Code, Decision & Safety Surfaces**
- **3. Plain-Language Product Summary:** Adds dedicated, low-latency API endpoints for three common enterprise tasks outside standard chat: IDE Fill-in-the-Middle (FIM) code autocomplete, structured probabilistic decision/classification calls, and standalone content-moderation checks. Teams can retire separate single-purpose proxies and govern all three workloads under the same keys, residency rules, and budgets.
  - *Practical Example:* A company's internal VS Code / JetBrains extension calls POST /v1/fim/completions routed to an on-prem vLLM Codestral pool for sub-100ms Tab autocomplete, while their user-generated-content API calls POST /v1/moderations under the same project Virtual Key and audit trail.
  - *Explicit Trade-Offs:* Adding three specialized surfaces increases OpenAPI and adapter conformance maintenance, so each endpoint reuses the exact same Route Resolver, BlindBroker, and RateCard pipeline rather than introducing parallel execution paths.
- **4. User Problem & Business Value:** Prevents shadow API key sprawl caused when IDE autocomplete plugins (Mistral/vLLM FIM), classification routers (OpenAI/Foundry /v1/decisions), or moderation pre-filters cannot use the central AI gateway.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-09 (Community & Partner Extension Author)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 openapi/dataplane-v1.yaml and internal/ingress/http [SOURCE_VERIFIED]; callers must use untyped /native/{dialect}/* passthrough per vendor.
- **7. Proposed Complete Functional Behavior:** Add typed POST /v1/fim/completions (prompt, suffix, max_tokens, stop, stream), POST /v1/decisions (typed options/confidence routing & classification surface aligned with 2026 OpenAI/Foundry/agentgateway patterns), and POST /v1/moderations (text + multimodal moderation taxonomy) backed by capability flags and zero-rate or token-metered RateCards [DESIGN_PROPOSAL].
- **8. User-Facing Subfeatures:**
  - POST /v1/fim/completions with prefix/suffix streaming for Mistral Codestral, DeepSeek, Qwen2.5/3-Coder on vLLM/Ollama.
  - POST /v1/decisions returning calibrated option selection and structured rationale metadata.
  - POST /v1/moderations supporting text and image safety classification with zero-rate commercial API support.
  - Dedicated capability flags (fim_completion, structured_decisions, moderation) in Route Explain and Model Catalog.
- **9. End-to-End Lifecycle:** Typed request arrives -> VirtualKey & capability validation -> Route resolution across deployments supporting the requested modality -> Zero-rate or token-metered FinOps hold/settlement -> Normalized response.
- **10. Primary & Alternative User Journeys:** Primary: IDE sends streaming /v1/fim/completions with prompt and suffix; Tanpopo routes to lowest-TTFT FIM-capable GPU deployment. Alternative: /v1/moderations call targets OpenAI omni-moderation-latest (priced at USD 0 list rate); Tanpopo records request count and trace latency with zero false budget error.
- **11. Administrative & Permission Boundaries:** Governed by Virtual Key modality/model allowlists and identical data-residency fences as chat endpoints.
- **12. Automation Opportunities:** IDE onboarding templates (FEAT-118) auto-populate Continue/Zed/Tab-completion FIM base URLs when a FIM route is active.
- **13. Enterprise-Specific Requirements (`OPTIONAL_ECOSYSTEM_INTEGRATION`):** Minimal internal gateway overhead suitable for high-frequency keystroke FIM traffic, bounded payload sizes, and explicit zero-rate commercial moderation support in RateCard.
- **14. Core vs Extension Placement Candidate:** `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` — Typed unified endpoint schemas live in the Core Data Plane while provider-specific wire mappings (Mistral Codestral FIM, OpenAI/Foundry Decisions, OpenAI/Omni Moderations) live in declarative/Wasm adapter manifests.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Modality badges in ModelsPage and endpoint tabs in Studio Playground.
  - *CLI:* Smoke-test fixtures for FIM, Decisions, and Moderations.
  - *Admin / Data-Plane API:* POST /v1/fim/completions, POST /v1/decisions, POST /v1/moderations in dataplane-v1.yaml.
  - *GitOps / Terraform / Packs:* Modality allowlists on RoutePolicy and VirtualKey declarative schemas.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-010 (Mistral, DeepSeek, Foundry, vLLM adapters) and FEAT-017 (zero-rate & specialized RateCard fields).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Fails closed with HTTP 400 capability_unsupported if a route targets a model lacking FIM/decision/moderation support; fails over pre-first-byte across redundant deployments.
- **18. Security, Privacy & Financial Implications:** Applies identical SSRF, BlindBroker, residency, and zero-prompt-persistence guarantees to source-code snippets sent by developer IDEs.
- **19. Preliminary Measurable Acceptance Criteria:**
  - POST /v1/fim/completions streams valid prefix/suffix code completions against Mistral and vLLM FIM endpoints.
  - POST /v1/decisions and POST /v1/moderations validate schemas, enforce residency policies, and record accurate trace/FinOps rows.
  - Commercial USD 0 moderation endpoints settle cleanly without triggering false UNMETERED_FLAGGED alerts.
- **20. Relevant Evidence & Research Confidence:** HIGH — Upstream surfaces are verified in Mistral Codestral (SRC-064), OpenAI & Microsoft Foundry /v1/decisions (SRC-048, SRC-061), and Solo agentgateway (SRC-019); Tanpopo unified contract is [DESIGN_PROPOSAL].
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — Valuable consolidation surface for IDE Tab-autocomplete, typed classification/routing decisions, and standalone moderation microservices, scheduled after P0/P1 core chat/responses/messages parity. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `Out-of-Band .tpkg / Tooling Wave 2`)*

---

## `DOM-02` — Provider Management (`8` Proposed Features)

- **Domain Overview:** Provider Management (DOM-02) governs how Tanpopo connects to, authenticates with, verifies, shares, rotates, and retires upstream AI providers across hyperscale clouds, frontier model APIs, sovereign enclaves, and self-hosted GPU clusters. It ensures raw credentials never enter guest extension sandboxes while giving operators turnkey onboarding templates, actionable connection diagnostics, keyless enterprise IAM authentication, zero-downtime secret rotation, and Provisioned Throughput (PTU) capacity optimization.
- **Existing RC9 Baseline Posture:** RC9 ships 8 bundled official adapters (5 Tier-1 declarative, 3 Tier-2 C/Wasm) mediated by BlindHostCredentialBroker and SSRFGuardDialer [SOURCE_VERIFIED]. Material limitations remain: aws-bedrock drops multimodal blocks, hardcodes a claude-3-5-sonnet probe, and fails native Converse path matching [SOURCE_VERIFIED]; admin/http/connections.go:798,808 hardcodes HOST_BLIND_INJECT + API_KEY, blocking AWS_SIGV4 creation [SOURCE_VERIFIED]; broker/workload_identity.go is unwired in serve.go (FND-004) [SOURCE_VERIFIED]; failed connection probes return HTTP 200 {valid:false} with zero Console error banner (PG-013, FND-016) [RUNTIME_VERIFIED]; trailing /v1 URLs cause /v1/v1/models 404s [RUNTIME_VERIFIED]; Vault secrets lack in-place PUT rotation; and no curated provider setup templates or PTU spillover exist.
- **Progressive Self-Hosted to Enterprise Experience:** A first-time user picks a curated setup template (such as OpenAI, Anthropic, or local Ollama with automatic Docker host.docker.internal hints), pastes an API key, and runs a 1-click blind verification probe with normalized base URLs. Enterprise platform teams unlock keyless AWS SigV4/IRSA, Azure Entra ID, GCP Workload Identity Federation, mTLS client certificates, cross-project regional connection sharing, scheduled Vault secret rotation with consumer dependency tracking, graceful connection drain/retirement, and pre-paid PTU saturation spillover.
- **Technical Selections Deferred to `TEEP-03`:** Exact Wasm ABI v1.1 streaming adapter framing hooks, pooled keep-alive transport keying and epoch-fenced idle socket eviction internals (ARCH-004), canonical .tpkg archive signing digest format (DEC-CAND-04), and distributed Valkey PTU utilization bucket data structures are deferred to TEEP-03.

| ID | Subdomain | Feature Name | Class | Disposition | Priority | Placement Candidate |
|---|---|---|---|---|---|---|
| `FEAT-009` | Official Provider Adapters | **Hardened Official First-Party Provider Adapters** | `CAT-ADMIN` | `IMPROVE` | `P0-Foundational` | `OFFICIAL_BUNDLED_EXTENSION` |
| `FEAT-010` | Cloud, Sovereign & Self-Hosted GPU Adapters | **Expanded Cloud, Sovereign & Self-Hosted GPU Adapter Suite** | `CAT-ECO` | `ADD` | `P1-Enterprise-Core` | `OFFICIAL_BUNDLED_EXTENSION` |
| `FEAT-011` | Guided Provider Onboarding | **Curated Provider & Self-Hosted Inference Setup Templates** | `CAT-ADMIN` | `ADD` | `P0-Foundational` | `CORE_CONTROL_PLANE` |
| `FEAT-012` | Connection Health & Diagnostics | **Blind Two-Phase Connection Probing & Actionable Failure Diagnostics** | `CAT-ADMIN` | `IMPROVE` | `P0-Foundational` | `CORE_CONTROL_PLANE` |
| `FEAT-013` | Outbound Provider Authentication | **Multi-Mode Outbound Provider Authentication (SigV4, Entra ID, mTLS, OAuth 2.1, Outbound WIF)** | `CAT-ADMIN` | `IMPROVE` | `P0-Foundational` | `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` |
| `FEAT-014` | Connection Lifecycle & Multi-Project Governance | **Complete Provider Connection Lifecycle, Org/Project Sharing & Endpoint Drain/Retirement** | `CAT-ADMIN` | `IMPROVE` | `P1-Enterprise-Core` | `CORE_CONTROL_PLANE` |
| `FEAT-015` | Vault Credential Lifecycle | **In-Place Vault Secret Rotation, OCI_REGISTRY_AUTH & Consumer Dependency Tracking** | `CAT-ADMIN` | `IMPROVE` | `P1-Enterprise-Core` | `CORE_CONTROL_PLANE` |
| `FEAT-016` | Reserved Capacity & Spillover Management | **Provisioned Throughput (PTU / Reserved Tier) Capacity Modeling & PAYG Spillover** | `CAT-ADMIN` | `ADD` | `P2-Differentiator` | `CORE_DATA_PLANE` |

### FEAT-009 — Hardened Official First-Party Provider Adapters

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-009` · `CAP` Traceability: `CAP-009` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-02` (Provider Management) -> **Official Provider Adapters**
- **3. Plain-Language Product Summary:** Fixes and upgrades Tanpopo's eight built-in AI provider connectors (OpenAI, OpenAI-Compatible, Anthropic, Google Gemini, AWS Bedrock, Azure OpenAI, Ollama, and Cohere) so images, PDFs, embeddings, and regional privacy rules work reliably everywhere. Crucially, it blocks hidden cross-border routing traps such as AWS Bedrock global.* profiles and Ollama *:cloud remote models when strict regional or on-prem policies apply.
  - *Practical Example:* A European hospital routes clinical PDF extraction to AWS Bedrock in eu-central-1 and local coding tasks to an on-prem Ollama server; hardened adapters preserve multimodal PDF blocks on Bedrock while automatically blocking a developer's accidental request for deepseek-v3.1:cloud on the ON_PREM Ollama connection.
  - *Explicit Trade-Offs:* Maintaining Tier-2 Wasm adapters (Anthropic, Gemini, AWS Bedrock) requires strict memory discipline (<=16 MiB linear memory, 0 WASI imports) and deterministic cross-OS Wasm compilation (ARCH-015), but provides stronger fault and credential isolation than in-process Go provider code.
- **4. User Problem & Business Value:** Eliminates silent multimodal content loss on AWS Bedrock, enables unified Gemini embeddings, upgrades Cohere and Azure adapters beyond legacy single-modality endpoints, and closes sneaky vendor residency leaks.
- **5. Target Personas:** `PER-01 (Solo Builder / First-Time Self-Hoster)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-06 (CISO, DPO & Regulatory Compliance Auditor)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION across extensions/official/* [SOURCE_VERIFIED]: (1) @tanpopo/aws-bedrock drops image/PDF blocks in adapter.c:36-59, hardcodes retired claude-3-5-sonnet in its probe (:104), checks 'model/' without leading '/' at :654, omits bedrock.<region>.amazonaws.com & bedrock-mantle.<region>.api.aws in manifest, and is omitted from scripts/build-wasm.sh; (2) @tanpopo/gemini lacks /v1/embeddings; (3) @tanpopo/ollama allows v0.40 *:cloud remote-proxied model tags even when DataRegion == ON_PREM; (4) @tanpopo/azure-openai and @tanpopo/cohere-rerank are single-modality only.
- **7. Proposed Complete Functional Behavior:** Harden all 8 bundled official adapters [DESIGN_PROPOSAL]: restore Bedrock Converse multimodal image/document blocks via host blob_ref splicing, replace hardcoded probe model with dynamic ListFoundationModels / configured probe, align '/model/' native inspector check, allowlist bedrock.<region> & bedrock-mantle.<region> hosts, classify Bedrock global.* inference profiles as MULTI_REGION (blocked under strict regional residency), quarantine/block Ollama *:cloud tags when connection DataRegion == ON_PREM, add Gemini unified /v1/embeddings (embedContent/batchEmbedContents), and upgrade Cohere to v2 (/v2/chat, /v2/embed, /v2/rerank) and Azure to Foundry + legacy deployments.
- **8. User-Facing Subfeatures:**
  - Full multimodal (image/PDF), tool-use, caching, and dynamic health-probe support in @tanpopo/aws-bedrock (Converse + Mantle).
  - Residency guardrails blocking AWS Bedrock global.* inference profiles under regional fences and Ollama *:cloud tags under ON_PREM.
  - Unified /v1/embeddings support in @tanpopo/gemini with configurable outputDimensionality and taskType.
  - Upgraded @tanpopo/cohere (v2 chat, embed, rerank) and @tanpopo/azure-openai (deployments + Foundry /openai/v1/*).
- **9. End-to-End Lifecycle:** Embedded official .tpkg verified at boot via canonical signature (ARCH-015, FND-003) -> auto-staged/activated for quickstart or enterprise policy -> executes stateless request/stream transforms inside wazero / declarative engine -> upgradable out-of-band via signed .tpkg updates without upgrading Core.
- **10. Primary & Alternative User Journeys:** Primary: Operator connects eu-central-1 AWS Bedrock; multimodal vision/PDF chat and ConverseStream execute losslessly. Alternative: User attempts to deploy us.anthropic.* or global.anthropic.* on a route restricted to EU_CENTRAL_1, or qwen3:cloud on an ON_PREM Ollama connection; Tanpopo blocks registration/routing with an explicit residency_violation diagnostic.
- **11. Administrative & Permission Boundaries:** Adapter installation and trust policy governed by OPERATOR / ORG_OWNER (FEAT-108); adapters execute with zero ambient network or secret authority.
- **12. Automation Opportunities:** scripts/build-wasm.sh and CI parity gates automatically compile and fuzz-test all 3 Tier-2 Wasm adapters (including aws-bedrock) and verify cross-OS .tpkg digest determinism (ARCH-015).
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** <=16 MiB Wasm linear memory, <=50 ms per-invocation CPU fuel ceiling, 0 WASI imports, 100% BlindBroker secret mediation, and strict host-suffix egress allowlists per adapter.
- **14. Core vs Extension Placement Candidate:** `OFFICIAL_BUNDLED_EXTENSION` — Ships as signed Tier-1 declarative YAML and Tier-2 Wasm .tpkg extensions embedded in the official binary so air-gapped and Day-1 installations work offline without weakening sandbox isolation.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Bundled Official Adapters catalog in ExtensionsPage and provider selector in ProvidersPage / Setup.
  - *CLI:* tanpopo plugin list / verify / pack and build-wasm reproducible pipeline.
  - *Admin / Data-Plane API:* GET /admin/v1/extensions and connection adapter_id bindings in admin-v1.yaml.
  - *GitOps / Terraform / Packs:* Pinned @sha256 official adapter references in tanpopo.lock and declarative connection manifests.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on ARCH-014 (wazero sandbox), ARCH-015 / FND-003 (reproducible .tpkg signing), and ARCH-017 (blob_ref splicing).`, `Enables FEAT-002, FEAT-003, FEAT-018, and FEAT-069 (sovereign residency fences).` · Related Decisions: `DEC-CAND-04`
- **17. Failure & Recovery Behavior:** Any Wasm trap, fuel timeout, or out-of-bounds write inside a Tier-2 adapter is caught by FaultGuard, returning a clean 502 adapter_fault without crashing the Go Data-Plane process.
- **18. Security, Privacy & Financial Implications:** Prevents accidental cross-border exfiltration via Ollama cloud-relay tags and Bedrock global profiles, and keeps raw AWS/Azure/GCP credentials completely outside Wasm linear memory.
- **19. Preliminary Measurable Acceptance Criteria:**
  - @tanpopo/aws-bedrock passes multimodal image/PDF chat, dynamic health probe, and native ConverseStream E2E tests and is compiled in scripts/build-wasm.sh.
  - Ollama *:cloud models are rejected on ON_PREM connections and Bedrock global.* profiles are rejected under single-region policies.
  - @tanpopo/gemini passes /v1/embeddings vector generation and @tanpopo/cohere passes v2 chat + embed + rerank conformance.
- **20. Relevant Evidence & Research Confidence:** HIGH — Every RC9 adapter defect is [SOURCE_VERIFIED] in extensions/official/* on ac9ae87a; vendor behaviors are verified in SRC-044 (Ollama v0.40), SRC-055 (Gemini), SRC-059 (Bedrock), and SRC-063 (Cohere v2).
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Closes concrete functional bugs and data-residency bypass risks discovered in RC9's 8 bundled official adapters (AWS Bedrock multimodal drop, Gemini missing embeddings, Ollama :cloud residency leak). *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Bundled .tpkg)`)*

---

### FEAT-010 — Expanded Cloud, Sovereign & Self-Hosted GPU Adapter Suite

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-010` · `CAP` Traceability: `CAP-010` · Catalog Class: `CAT-ECO` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-02` (Provider Management) -> **Cloud, Sovereign & Self-Hosted GPU Adapters**
- **3. Plain-Language Product Summary:** Adds dedicated, first-party connectors for Google Vertex AI, Microsoft Foundry, Databricks, Mistral, DeepSeek, Groq, Fireworks, Together AI, and self-hosted GPU inference engines (vLLM, SGLang, and CNCF llm-d). Instead of forcing every non-OpenAI provider through a generic fallback connector that misses regional endpoints, error codes, or cache headers, each provider gets first-class support.
  - *Practical Example:* A global enterprise connects Google Vertex AI (europe-west4 partner Claude + Gemini endpoints via GCP Workload Identity) alongside an internal Kubernetes CNCF llm-d vLLM pool; both providers expose full model discovery, normalized rate-limit backoff headers, and prefix-cache affinity routing.
  - *Explicit Trade-Offs:* Shipping ~10 additional first-party .tpkg adapters requires ongoing contract-cassette testing as provider APIs evolve, which is mitigated by keeping most OpenAI-derived providers as declarative Tier-1 manifests and reserving Tier-2 Wasm only for multi-dialect Vertex AI / partner payload translation.
- **4. User Problem & Business Value:** In RC9, connecting Vertex AI, Databricks, Mistral OCR/FIM, DeepSeek reasoning_content, or Kubernetes GAIE llm-d pools via generic openai-compatible loses provider-specific auth, regional DataZone hosts, reasoning fields, and GPU cache-affinity headers.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`
- **6. Existing RC9 Behavior & Limitations:** MISSING dedicated adapters in RC9 extensions/official/* [SOURCE_VERIFIED]; operators must approximate with generic @tanpopo/openai-compatible (chat/embeddings only, static Bearer key only, no DeepSeek reasoning_content or Vertex multi-publisher routing).
- **7. Proposed Complete Functional Behavior:** Deliver signed first-party Tier-1/Tier-2 .tpkg adapters for: (1) @tanpopo/google-vertex-ai (Gemini + Anthropic/Mistral/Llama MaaS publishers, regional & global DataZone endpoints, GCP OAuth/WIF); (2) @tanpopo/microsoft-foundry (/openai/v1/* GA + Entra ID); (3) @tanpopo/vllm-llmd (vLLM, SGLang sgl-router, and CNCF llm-d GAIE prefix-affinity headers); (4) @tanpopo/mistral (chat, embeddings, FIM, OCR); (5) @tanpopo/deepseek (reasoning_content + cache hit accounting); (6) @tanpopo/groq, @tanpopo/fireworks, @tanpopo/together, and @tanpopo/databricks [DESIGN_PROPOSAL].
- **8. User-Facing Subfeatures:**
  - Google Vertex AI multi-publisher adapter supporting Gemini :generateContent and partner :rawPredict / MaaS endpoints.
  - Self-hosted GPU adapter (@tanpopo/vllm-llmd) supporting vLLM, SGLang, and Kubernetes GAIE / llm-d prefix-routing headers.
  - Dedicated Mistral, DeepSeek, Groq, Fireworks, Together, OpenRouter, and Databricks adapters with native reasoning/cache usage extraction.
  - Strict per-provider allowed_host_suffixes and commercial API compliance (DEC-004).
- **9. End-to-End Lifecycle:** Signed .tpkg installed from embedded catalog or Marketplace/offline bundle -> bound to Provider Connection via curated template (FEAT-011) -> probed via BlindBroker -> versioned and updated independently via tanpopo.lock.
- **10. Primary & Alternative User Journeys:** Primary: Platform SRE enables @tanpopo/google-vertex-ai with GCP Workload Identity and discovers regional models in one click. Alternative: Self-hosted engineer deploys @tanpopo/vllm-llmd against an internal private-IP GPU service with allow_private_ip=true and explicit allowed_cidrs.
- **11. Administrative & Permission Boundaries:** Subject to org_extension_policies trust gates (`official_verified`) and private-IP/CIDR egress controls restricted to OPERATOR / ORG_OWNER.
- **12. Automation Opportunities:** Pairs directly with Curated Setup Templates (FEAT-011) and Account Model Discovery (FEAT-019) to pre-fill endpoint URLs, auth modes, and starter models.
- **13. Enterprise-Specific Requirements (`OPTIONAL_ECOSYSTEM_INTEGRATION`):** 100% air-gap offline availability via embedded catalog or .tpbundle, zero consumer-session scraping (DEC-004), and deterministic Cassette conformance tests per adapter.
- **14. Core vs Extension Placement Candidate:** `OFFICIAL_BUNDLED_EXTENSION` — Packaged as modular, signed Tier-1/Tier-2 .tpkg adapters so Tanpopo Core stays lean while enterprises and air-gapped operators get first-party verified connectors without custom YAML authoring.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Available in ExtensionsPage Marketplace and ProvidersPage / Setup provider template picker.
  - *CLI:* tanpopo plugin install / list / verify.
  - *Admin / Data-Plane API:* Exposed via /admin/v1/extensions and connection creation APIs.
  - *GitOps / Terraform / Packs:* Declarative extension installations and @sha256 lockfile pins.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-013 (GCP WIF, Entra ID, OAuth2 outbound auth) and ARCH-014/ARCH-015 (.tpkg runtime & deterministic packer).`, `Powers FEAT-011 (setup templates), FEAT-025 (KV-cache affinity), and FEAT-030 (hybrid GPU cloud bursting).` · Related Decisions: `DEC-CAND-02`, `DEC-CAND-04`
- **17. Failure & Recovery Behavior:** Normalizes provider-specific 429/529/quota errors and Retry-After headers into canonical Tanpopo retryable classifications so SM-04 circuit breakers react consistently.
- **18. Security, Privacy & Financial Implications:** Host-suffix allowlists block credential exfiltration to unintended domains, while native cache-hit token extraction (e.g., DeepSeek prompt_cache_hit_tokens) prevents FinOps over-billing.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Each new official .tpkg passes cassette conformance for chat, streaming, tool calls, usage extraction, and error normalization.
  - @tanpopo/google-vertex-ai authenticates via GCP WIF/OAuth and invokes both Gemini and partner MaaS endpoints.
  - @tanpopo/vllm-llmd emits configurable prefix-affinity headers and normalizes vLLM/SGLang streaming token usage.
- **20. Relevant Evidence & Research Confidence:** HIGH — Upstream wire specs verified in SRC-031..033 (vLLM, llm-d, SGLang), SRC-055 (Vertex AI), SRC-061 (Foundry), SRC-064 (Mistral), and SRC-062 (DeepSeek); adapter packaging is [DESIGN_PROPOSAL].
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Covers the remaining hyperscaler commitments (Google Vertex AI, Microsoft Foundry GA, Databricks), high-speed inference clouds (Groq, Fireworks, Together, DeepSeek, Mistral), and internal Kubernetes GPU stacks (vLLM, SGLang, CNCF llm-d). *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `Out-of-Band .tpkg / Tooling Wave 1`)*

---

### FEAT-011 — Curated Provider & Self-Hosted Inference Setup Templates

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-011` · `CAP` Traceability: `NEW-TEEP-02A` (Expanded Underrepresented Capability) · Catalog Class: `CAT-ADMIN` (`ADDED_IN_TEEP_02A`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-02` (Provider Management) -> **Guided Provider Onboarding**
- **3. Plain-Language Product Summary:** Replaces blank, error-prone connection forms with guided, provider-specific setup cards for 16+ popular cloud APIs and local GPU servers. Each template pre-fills the right base URL, authentication method, regional privacy defaults, and recommended starter models, automatically fixes common URL mistakes like duplicate '/v1' paths, and verifies connectivity in one click before saving.
  - *Practical Example:* A developer running Tanpopo in Docker selects the 'Ollama (Local / Self-Hosted)' template; Tanpopo automatically detects container runtime networking, suggests http://host.docker.internal:11434 instead of localhost, prompts to allow the specific private subnet, runs a 1-click blind probe to discover installed local models, and flags any *:cloud tags if ON_PREM residency is selected.
  - *Explicit Trade-Offs:* Curated templates must never hide or bypass underlying security controls (such as private-IP SSRF fences on local Ollama/vLLM endpoints); therefore, templates pre-populate explicit, reviewable CIDR/private-IP consent toggles rather than silently disabling SSRF protections.
- **4. User Problem & Business Value:** In RC9, operators had to memorize exact base URLs (hitting /v1/v1/models 404 when pasting https://api.openai.com/v1 [RUNTIME_VERIFIED, QA-005]), manually seal Vault secrets in a separate drawer first, guess credential binding keys, and debug Docker localhost SSRF blocks by trial and error.
- **5. Target Personas:** `PER-01 (Solo Builder / First-Time Self-Hoster)`, `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED, RUNTIME_VERIFIED]: ProvidersPage.tsx presents a generic raw form requiring pre-created Vault credential IDs, raw endpoint URLs without '/v1' suffix normalization, and manual model creation; /setup saw available_adapters: [] on fresh boot (DEF-049, FND-010).
- **7. Proposed Complete Functional Behavior:** Provide offline-bundled Curated Setup Templates in /setup, ProvidersPage, and `tanpopo provider add` [DESIGN_PROPOSAL] covering: OpenAI, Anthropic, Gemini AI Studio, Google Vertex AI, AWS Bedrock / Bedrock Mantle, Microsoft Foundry / Azure OpenAI, Ollama (with Docker `host.docker.internal` auto-detection hint & `ON_PREM` residency default), self-hosted `vLLM` / `SGLang` / `llm-d`, OpenRouter, Groq, Fireworks, Together, Mistral, DeepSeek, Cohere, and Databricks. Each template: (1) auto-normalizes trailing `/v1` or `/openai/v1` base URLs with a live preview diff; (2) seals secrets inline into Vault pre-mapped to exact adapter auth slots (API Key, AWS SigV4/IRSA, Entra ID, GCP WIF); (3) suggests curated starter models with signed reference USD/1M rate cards (or zero-rate/GPU-hourly for self-hosted); and (4) executes a 1-click BlindBroker probe before committing.
- **8. User-Facing Subfeatures:**
  - Turnkey setup cards for 16 cloud and self-hosted providers (OpenAI, Anthropic, Gemini AI Studio, Vertex AI, Bedrock/Mantle, Foundry/Azure, Ollama, vLLM/SGLang/llm-d, OpenRouter, Groq, Fireworks, Together, Mistral, DeepSeek, Cohere, Databricks).
  - Automatic base-URL canonicalizer stripping duplicate trailing '/v1' suffixes and templating regional cloud hostnames (e.g., bedrock-runtime.<region>.amazonaws.com).
  - Container-aware local inference helper detecting Docker/Kubernetes environments and surfacing host.docker.internal + explicit CIDR/allow_private_ip consent for Ollama/vLLM.
  - Inline Vault secret sealing + 1-click blind verification probe + curated starter model bundle selection in a single atomic workflow.
- **9. End-to-End Lifecycle:** User selects template in /setup, ProvidersPage, or CLI -> enters region/URL & inline credential (or keyless IAM role) -> live URL normalizer & SSRF check preview -> 1-click dry-run BlindBroker probe + model entitlement discovery -> atomic save creating/activating adapter, Vault secret, connection, and optional starter deployments/routes.
- **10. Primary & Alternative User Journeys:** Primary: Solo user picks 'Anthropic', pastes sk-ant-..., clicks Verify & Connect, and selects Claude Sonnet and Haiku starter models in one drawer. Alternative: Enterprise SRE picks 'AWS Bedrock', selects eu-central-1 and keyless AWS_IRSA auth, verifies regional inference profiles via 1-click blind probe, and exports the resulting normalized configuration to GitOps.
- **11. Administrative & Permission Boundaries:** Available to ORG_OWNER and OPERATOR (and PROJECT/BU admins when delegated connection creation is permitted); enabling allow_private_ip on Ollama/vLLM templates still enforces explicit RBAC and CIDR bounds.
- **12. Automation Opportunities:** Auto-detects trailing '/v1' URL copy-paste errors, auto-activates the required bundled official .tpkg adapter if not yet active, and pre-selects regional data_region tags from cloud region dropdowns.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** 100% offline metadata (zero external fetches), full 15-language localization in Console, Astryx Neutral component compliance, and identical underlying normalized DB rows as manual API/Terraform creation.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Template metadata, URL normalization, credential slot pre-mapping, and orchestrated blind verification belong in the Core Control Plane onboarding API and Console/CLI wizards.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Provider Template Gallery in /setup Quickstart (FEAT-119) and ProvidersPage 'Add Connection' wizard.
  - *CLI:* Interactive and flag-driven `tanpopo provider add --template <slug>`.
  - *Admin / Data-Plane API:* GET /admin/v1/provider-templates and dry-run probe endpoint POST /admin/v1/connections:preview-probe.
  - *GitOps / Terraform / Packs:* Templates generate clean, normalized provider_connection + secret_binding YAML snippets for GitOps.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-009 & FEAT-010 (official adapters), FEAT-012 (blind probing), FEAT-018/019 (model catalog & discovery).`, `Directly powers FEAT-119 (Guided First-Time Quickstart) and closes FND-010 / QA-005 '/v1/v1/models' friction.` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** If the 1-click pre-save verification probe fails (e.g., invalid key, Docker localhost unreachable, corporate TLS CA mismatch), displays exact CheckDiagnosticCode remediation inline without leaving orphaned half-created secrets or broken connections in the database.
- **18. Security, Privacy & Financial Implications:** Inline secret inputs are transmitted write-only, scrubbed from all logs, and sealed immediately with Vault AEAD; pre-save dry-run probes execute strictly through BlindHostCredentialBroker + SSRFGuardDialer.
- **19. Preliminary Measurable Acceptance Criteria:**
  - All 16 curated provider templates render offline in 15 locales with pre-mapped auth slots, regional defaults, and starter model suggestions.
  - Pasting https://api.openai.com/v1 or https://api.groq.com/openai/v1/ auto-normalizes cleanly so model discovery and chat calls never produce /v1/v1/* 404s.
  - Selecting Ollama inside a Docker container surfaces the host.docker.internal hint and explicit private-subnet consent, completing 1-click blind verification.
- **20. Relevant Evidence & Research Confidence:** HIGH — Directly addresses [RUNTIME_VERIFIED] onboarding traps recorded in QA-005 RC9-REPORT.md:46 and TEEP-01-DOC-07 WF-01/WF-02; template orchestration is a low-risk [DESIGN_PROPOSAL].
- **21. Proposal Classification:** `ADD` (`ADDED_IN_TEEP_02A`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Newly added in TEEP-02A to eliminate the manual URL, auth-slot, private-IP, and trailing-/v1 pitfalls that caused Day-1 connection failures in RC9 QA-005 across both cloud APIs and local Docker/GPU setups. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-012 — Blind Two-Phase Connection Probing & Actionable Failure Diagnostics

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-012` · `CAP` Traceability: `CAP-011` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-02` (Provider Management) -> **Connection Health & Diagnostics**
- **3. Plain-Language Product Summary:** Tests whether a provider connection actually works and explains clearly—without ever exposing secret keys—why a test failed. Instead of silently failing or showing generic errors, it pinpoints specific causes like an expired API key, duplicate '/v1' URL path, untrusted corporate TLS certificate, clock skew on AWS SigV4, or a blocked private IP address.
  - *Practical Example:* An enterprise SRE configures an internal vLLM endpoint behind a corporate internal PKI CA and clicks 'Probe Connection'; Tanpopo immediately displays a structured TLS_UNTRUSTED_CA banner with the exact flag/env remediation to trust the internal CA bundle, cutting troubleshooting from an hour of packet captures to seconds.
  - *Explicit Trade-Offs:* Detailed network failure diagnostics can become an SSRF reconnaissance oracle if raw internal DNS/socket responses are echoed verbatim; Tanpopo balances clarity and security by mapping socket errors to an enumerated CheckDiagnosticCode taxonomy and redacting raw internal IP/header payloads for non-OPERATOR roles.
- **4. User Problem & Business Value:** In RC9, clicking 'Probe connection' on a broken connection returned HTTP 200 `{valid: false}` while ProvidersPage.tsx rendered zero error banner or toast (PG-013), and SSRF blocks emitted zero socket-level telemetry (RR-022).
- **5. Target Personas:** `PER-01 (Solo Builder / First-Time Self-Hoster)`, `PER-03 (Enterprise AI Platform Architect / SRE)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in internal/admin/http/connection_health.go and web/console/src/pages/ProvidersPage.tsx [RUNTIME_VERIFIED, SOURCE_VERIFIED]: two-phase blind probe executes, but HTTP 200 `{valid: false}` triggers no visible failure UI in Console (PG-013), SSRF dial rejections lack structured telemetry counters (RR-022, FND-016), and pre-save dry-run probing without persisting a connection first is unsupported.
- **7. Proposed Complete Functional Behavior:** Return structured, secret-scrubbed diagnostic payloads (`valid`, `diagnostic_code`, `http_status`, `latency_ms`, `sanitized_detail`, `remediation_hint_key`, `detected_model_count`) from both saved connection probes (`POST /admin/v1/connections/{id}/test`) and pre-save dry-run probes [DESIGN_PROPOSAL]. Instrument `SSRFGuardDialer` (ARCH-009) with rejection counters + structured audit/telemetry events, and render explicit Astryx Neutral error/success banners with localized 15-language remediation guidance in ProvidersPage and CLI.
- **8. User-Facing Subfeatures:**
  - Enumerated CheckDiagnosticCode classification (AUTH_INVALID_401, PERMISSION_DENIED_403, ENDPOINT_PATH_404, TLS_CA_UNTRUSTED, DNS_LOOKUP_FAILED, SSRF_PRIVATE_IP_BLOCKED, SIGV4_CLOCK_SKEW, UPSTREAM_TIMEOUT).
  - Explicit Console Banner/Toast feedback on both probe success (with latency & discovered model count) and probe failure (closing PG-013).
  - Pre-save dry-run connection verification in Setup and Add/Edit Connection drawers.
  - Socket-level SSRF rejection Prometheus counters and sanitized security audit logs (closing RR-022 / FND-016).
- **9. End-to-End Lifecycle:** Operator triggers pre-save or saved connection probe -> Adapter builds lightweight probe request -> BlindHostCredentialBroker + SSRFGuardDialer execute outbound call -> Secret scrubber + diagnostic classifier categorize outcome -> Connection health status updated & localized UI remediation rendered.
- **10. Primary & Alternative User Journeys:** Primary: Probe succeeds in 140 ms and offers 1-click transition to bulk model import (FEAT-019). Alternative: Probe hits private IP 10.20.4.8 without allow_private_ip; SSRFGuardDialer aborts pre-connect, increments tanpopo_egress_ssrf_rejected_total, and UI displays SSRF_PRIVATE_IP_BLOCKED with CIDR guidance.
- **11. Administrative & Permission Boundaries:** Callable by principals with ActionConnectionWrite / ActionConnectionTest within the connection's scope; detailed internal network/IP hints are restricted to OPERATOR / ORG_OWNER to prevent tenant SSRF probing.
- **12. Automation Opportunities:** Optional background periodic passive/lightweight health verification for idle connections with automatic recovery detection when credentials are rotated.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Zero credential leakage in error messages, support for corporate HTTP_PROXY / custom CA bundles, and full 15-language translation of all diagnostic codes (no raw English server strings in non-English UI).
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Executes inside Admin connection health controllers in coordination with BlindHostCredentialBroker and SSRFGuardDialer.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Inline Probe Status Banner and diagnostic drawer in ProvidersPage and /setup.
  - *CLI:* `tanpopo provider test <connection-id>` with structured exit codes and remediation hints.
  - *Admin / Data-Plane API:* POST /admin/v1/connections/{id}/test and POST /admin/v1/connections:preview-probe.
  - *GitOps / Terraform / Packs:* Optional post-apply smoke probe hook in `tanpopo config apply --verify-connections`.
- **16. Dependencies & Cross-Domain Relationships:** `Closes FND-016 (PG-013 and RR-022) and depends on ARCH-008 (BlindHostCredentialBroker) and ARCH-009 (SSRFGuardDialer).`, `Feeds directly into FEAT-011 (setup templates) and FEAT-019 (model discovery).` · Related Decisions: None (Standard Catalog Review)
- **17. Failure & Recovery Behavior:** Probes enforce a strict bounded timeout (e.g., 10s) so hung upstream endpoints never block Admin API workers.
- **18. Security, Privacy & Financial Implications:** All probe responses pass through the credential scrubber; probes use zero-cost metadata endpoints (/v1/models, ListFoundationModels) wherever supported to avoid unnecessary token charges.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Failed connection probes render a visible, accessible (WCAG 2.2 AA) diagnostic banner in ProvidersPage across all 15 locales (verifying PG-013 closure).
  - Blocked private/link-local/IMDS dial attempts increment SSRF rejection metrics and emit sanitized diagnostic codes (verifying RR-022 closure).
  - Zero secret substrings or Authorization header values appear in probe API responses, traces, or logs under injected 401/500 echo faults.
- **20. Relevant Evidence & Research Confidence:** HIGH — PG-013 and RR-022 are [RUNTIME_VERIFIED] in QA-005 and [SOURCE_VERIFIED] in connection_health.go / ProvidersPage.tsx on ac9ae87a.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Fixes RC9's silent probe failure UX (PG-013) and missing SSRF socket-rejection telemetry (RR-022 / FND-016), which otherwise leave operators blind when debugging TLS CAs, firewalls, or invalid credentials. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-013 — Multi-Mode Outbound Provider Authentication (SigV4, Entra ID, mTLS, OAuth 2.1, Outbound WIF)

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-013` · `CAP` Traceability: `CAP-012` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-02` (Provider Management) -> **Outbound Provider Authentication**
- **3. Plain-Language Product Summary:** Enables Tanpopo to authenticate to cloud and internal AI providers using enterprise-grade, keyless security methods instead of static API keys. It supports AWS SigV4 and IAM Roles (IRSA/Pod Identity), Microsoft Entra ID, Google Cloud Workload Identity, mutual TLS (mTLS) client certificates, and commercial OAuth 2.1.
  - *Practical Example:* A bank running Tanpopo on Amazon EKS configures an AWS Bedrock connection using outbound AWS IRSA Workload Identity and an internal vLLM enclave using mTLS client certificates; zero static API keys exist in Vault or environment variables, and BlindBroker rotates short-lived STS tokens automatically in RAM.
  - *Explicit Trade-Offs:* Supporting five outbound authentication families increases test matrix complexity (requiring mock STS/Entra/OAuth/mTLS harnesses in CI), and strict DEC-004 enforcement means OAuth 2.1 is intentionally restricted to sanctioned commercial/enterprise APIs—never consumer ChatGPT Plus or Claude Pro browser sessions.
- **4. User Problem & Business Value:** Unblocks AWS Bedrock, Google Vertex AI, Microsoft Foundry, and zero-trust internal GPU deployments in enterprises where static API keys fail security review.
- **5. Target Personas:** `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-05 (Enterprise IAM & Workforce Security Lead)`, `PER-06 (CISO, DPO & Regulatory Compliance Auditor)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION [SOURCE_VERIFIED]: (1) internal/admin/http/connections.go:798,808 hardcodes InjectModeHostBlind + AuthModeAPIKey on POST /connections, blocking AWS_SIGV4 connection creation; (2) internal/security/broker/workload_identity.go (AWS IRSA, GCP WIF) is implemented in isolation but never wired in cmd/tanpopo/serve.go (FND-004); (3) Azure Entra ID WIF/Managed Identity is missing; and (4) OAuth 2.1 requires undocumented TANPOPO_OAUTH_REDIRECT_URI and lacks Device Code / Client Credentials UI.
- **7. Proposed Complete Functional Behavior:** Fix `connections.go` to honor adapter-declared `InjectMode` and `AuthMode` (`API_KEY`, `AWS_SIGV4`, `MTLS_CLIENT_CERT`, `OAUTH2`, `WORKLOAD_IDENTITY`), wire `NewWorkloadIdentityExchanger` in `serve.go` (adding Azure Entra ID WIF & Managed Identity alongside AWS IRSA/STS AssumeRole and GCP WIF), expose OAuth 2.1 `client_credentials`, `authorization_code_pkce`, and `device_code` flows cleanly in Console/CLI, and bind Vault-sealed mTLS client keypairs on pooled upstream transports [DESIGN_PROPOSAL].
- **8. User-Facing Subfeatures:**
  - AWS SigV4 request signing (static IAM credentials, STS AssumeRole with ExternalId, and keyless EKS IRSA / Pod Identity).
  - Keyless Azure Entra ID (Workload Identity / Managed Identity / Service Principal) and GCP Workload Identity Federation.
  - Vault-sealed mTLS client certificate + private key authentication with custom upstream CA bundle pinning.
  - Commercial OAuth 2.1 Client Credentials, PKCE, and headless CLI/Console Device Code flows (strictly honoring DEC-004).
- **9. End-to-End Lifecycle:** Admin selects auth mode on connection -> binds Vault credential slots or workload audience/role ARN -> BlindBroker exchanges/refreshes short-lived tokens in process RAM ahead of expiry -> injects Authorization / SigV4 / mTLS handshake at network edge -> evicts cached tokens/sockets immediately upon rotation or disable.
- **10. Primary & Alternative User Journeys:** Primary: EKS pod exchanges projected ServiceAccount token via STS AssumeRoleWithWebIdentity and signs Bedrock ConverseStream via SigV4. Alternative: STS/Entra token refresh fails due to IAM policy revocation; BlindBroker fails closed with a structured upstream_auth_exchange_failed trace code and trips connection health.
- **11. Administrative & Permission Boundaries:** Configuring WORKLOAD_IDENTITY using host/pod ambient identity (IRSA/Managed Identity) requires explicit `OPERATOR` permission or an `OPERATOR`-approved IAM Role ARN allowlist so tenant admins cannot confuse-deputy the gateway pod's own cloud role across tenants!
- **12. Automation Opportunities:** Proactive background jittered refresh of STS, Entra ID, GCP, and OAuth 2.1 access tokens in RAM (at 80% of token lifetime) prevents latency spikes on live inference calls.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Strict multi-tenant confused-deputy prevention on pod-level WIF roles, zero plaintext token persistence on disk/SQL, FIPS-compatible SHA-256 SigV4/TLS crypto, and DEC-004 consumer-cookie prohibition.
- **14. Core vs Extension Placement Candidate:** `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` — Cryptographic signing, mTLS socket binding, OAuth token refresh, and STS/Entra/GCP token exchange MUST execute inside the Host's BlindHostCredentialBroker (ARCH-008) while adapters declare required auth modes.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Dynamic Auth Mode forms (API Key, AWS SigV4/IRSA, Entra ID, GCP WIF, mTLS, OAuth 2.1) in ProvidersPage.
  - *CLI:* `tanpopo provider auth-login` (PKCE / Device Code) and WIF preflight checks in `tanpopo doctor`.
  - *Admin / Data-Plane API:* Full auth_mode / inject_mode / wif_config schema on POST/PATCH /admin/v1/connections.
  - *GitOps / Terraform / Packs:* Declarative auth_mode, role_arn/client_id, and Vault secret slot bindings in connection YAML.
- **16. Dependencies & Cross-Domain Relationships:** `Closes FND-004 unwired `WorkloadIdentityExchanger` and `connections.go:798,808` SigV4 blocker.`, `Depends on ARCH-008 (BlindHostCredentialBroker), ARCH-004 (mTLS-aware pooled transports), and FEAT-059 (Vault).` · Related Decisions: `DEC-CAND-01`
- **17. Failure & Recovery Behavior:** Uses stale-while-revalidate within remaining token validity if an IdP token endpoint blips momentarily; fails closed once token expires and surfaces `CheckDiagnosticCode` in Connection Health.
- **18. Security, Privacy & Financial Implications:** Eliminates static long-lived cloud keys in enterprise deployments and enforces tenant-scoped Role ARN / ExternalId allowlists to prevent cross-tenant IAM privilege escalation.
- **19. Preliminary Measurable Acceptance Criteria:**
  - POST /admin/v1/connections creates working AWS_SIGV4, WORKLOAD_IDENTITY (AWS/GCP/Azure), MTLS_CLIENT_CERT, and OAUTH2 connections end-to-end.
  - Tenant ORG_OWNER cannot bind host-ambient pod IAM credentials unless explicitly allowlisted for that org by an INSTANCE OPERATOR.
  - Short-lived WIF/OAuth tokens reside exclusively in BlindBroker RAM and are zeroized upon connection disable or rotation.
- **20. Relevant Evidence & Research Confidence:** HIGH — `connections.go:798,808` and unwired `workload_identity.go` are [SOURCE_VERIFIED] on ac9ae87a; cloud WIF/SigV4/Entra protocols are industry-standard [DESIGN_PROPOSAL].
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Enterprise cloud security teams prohibit static long-lived AWS IAM access keys and Azure api-keys; moreover, RC9's hardcoded API_KEY bug in connections.go blocks AWS Bedrock SigV4 creation outright. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-014 — Complete Provider Connection Lifecycle, Org/Project Sharing & Endpoint Drain/Retirement

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-014` · `CAP` Traceability: `CAP-013` · Catalog Class: `CAT-ADMIN` (`EXPANDED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-02` (Provider Management) -> **Connection Lifecycle & Multi-Project Governance**
- **3. Plain-Language Product Summary:** Gives administrators full lifecycle control over provider connections—from creation and configuration updates to sharing vetted connections across specific projects, gracefully draining live streams, retiring old endpoints, and safely deleting unused connections. Teams never have to rip-and-replace routes just to update a firewall CIDR, compliance flag, or sharing rule.
  - *Practical Example:* A central Cloud Ops team shares a HIPAA-BAA-covered Azure Foundry connection with three clinical projects, later updates its allowed egress CIDRs in place, and six months later transitions an older regional connection through Deprecate -> Drain (waiting 45s for active streams to finish) -> Retire -> Delete after verifying zero active model deployments depend on it.
  - *Explicit Trade-Offs:* Allowing in-place edits to `data_region`, `baa_covered`, or `shared_project_ids` on a live connection could invalidate downstream routes; Tanpopo mitigates this by running a pre-mutation blast-radius check (FEAT-140) and warning or requiring confirmation if active routes would lose compliance eligibility.
- **4. User Problem & Business Value:** In RC9, ProvidersPage's Edit drawer only exposed 3 fields (`name`, `endpoint_url`, `secret_bindings`), omitting `allow_private_ip`, `allowed_cidrs`, `data_region`, `baa_covered`, `upstream_zdr_verified`, and project sharing (`shared_project_ids`), and lacked graceful drain/retire transitions.
- **5. Target Personas:** `PER-01 (Solo Builder / First-Time Self-Hoster)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-04 (Business Unit / Line-of-Business Delegated Admin)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in internal/admin/http/connections.go and ProvidersPage.tsx [SOURCE_VERIFIED, RUNTIME_VERIFIED]: lacks `GET /admin/v1/connections/{id}` single-resource fetch, Console omits network/compliance/sharing fields on Edit, trailing `/v1` URLs are not normalized, and lifecycle states lack explicit `DEPRECATED -> DRAINING -> DISABLED -> RETIRED` workflow controls.
- **7. Proposed Complete Functional Behavior:** Implement complete lifecycle parity across API, Console, CLI, and GitOps (`Create -> Active -> Update/Rotate -> Deprecate -> Drain -> Disable -> Retire -> Delete`) [DESIGN_PROPOSAL]: add `GET /admin/v1/connections/{id}`, expose all mutable fields (`endpoint_url` with `/v1` normalizer, `allow_private_ip`, `allowed_cidrs`, `data_region`, `baa_covered`, `upstream_zdr_verified`, `billing_mode`, `visibility_scope`: `PROJECT_ONLY` | `SELECTED_PROJECTS` | `ORG_SHARED` + `shared_project_ids`), enforce `HTTP 409` dependency checks showing exact bound deployments/routes before `Delete`, and fence new traffic during `DRAINING` while letting in-flight SSE streams complete up to drain timeout.
- **8. User-Facing Subfeatures:**
  - Full-field Connection Detail & Edit drawer with If-Match optimistic concurrency and blast-radius preview.
  - Granular connection sharing: Project-Private, Selected Projects (`shared_project_ids`), or Organization-Wide.
  - Explicit lifecycle transitions: Active, Deprecated (warns owners, blocks new deployments), Draining (stops new calls, finishes open streams), Disabled, Retired, and 409-guarded Delete.
  - Immediate pooled-transport socket eviction (ARCH-004) whenever network, TLS, auth, or state fields mutate.
- **9. End-to-End Lifecycle:** `Create` (validated & probed) -> `Active` (serving traffic) -> `Update/Rotate` (in-place `PATCH` with `If-Match` & pooled socket eviction) -> `Deprecate` (blocks new deployment bindings, emits sunset notice) -> `Drain` (stops new Route Resolver selection while `ActiveStreamRegistry` drains in-flight streams) -> `Disable` (hard-fences traffic while preserving config for rollback) -> `Retire` (permanent read-only historical state preserving FinOps/Audit FK lineage) -> `Delete` (`HTTP 409` if dependent deployments exist; soft/tombstone archive preserving immutable `usage_settlements` references).
- **10. Primary & Alternative User Journeys:** Primary: Org Owner updates `shared_project_ids` to grant a new BU project access to a vetted regional Bedrock connection without duplicating Vault secrets. Alternative: Operator attempts `DELETE` on a connection still referenced by 2 model deployments; API returns `409 connection_in_use` listing the exact deployment and route IDs and offering a 'Deprecate & Drain' action instead.
- **11. Administrative & Permission Boundaries:** Org-shared connections are managed by `ORG_OWNER` / `OPERATOR`; project-scoped connections can be managed by `TEAM_ADMIN` / `BU_ADMIN` within parent policy ceilings (`allow_private_ip` gated by security policy).
- **12. Automation Opportunities:** Automatic transition from `DRAINING` to `DISABLED` (or `RETIRED`) as soon as `ActiveStreamRegistry` count for the connection reaches zero or the configured drain timeout elapses.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Mandatory `If-Match` ETags on all mutations, RFC 8785 JCS before/after audit records, Four-Eyes hook support on production compliance flags (`baa_covered`, `data_region`), and zero broken FinOps historical references.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Connection state machines, cross-project RBAC visibility, ETag concurrency, and ConfigSnapshot epoch bumps are core Control Plane responsibilities.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Complete Connection Detail/Edit Drawer, Sharing Selector, and Lifecycle State Menu in ProvidersPage.
  - *CLI:* `tanpopo provider get | update | share | deprecate | drain | disable | retire | delete`.
  - *Admin / Data-Plane API:* GET/POST/PATCH/DELETE /admin/v1/connections/{id} and lifecycle state transition actions in admin-v1.yaml.
  - *GitOps / Terraform / Packs:* Full declarative reconciliation of connection fields, project sharing lists, and lifecycle state.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on ARCH-004 (instant idle socket eviction on network/auth edits) and FEAT-140 (blast-radius preview).`, `Aligns with FEAT-120 (unified cross-domain resource lifecycle parity).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Concurrent conflicting edits fail safely with `HTTP 412 Precondition Failed`; disabling a connection can be reverted to `Active` in 1 click if rollback is needed prior to `Retire`.
- **18. Security, Privacy & Financial Implications:** Tightening `allowed_cidrs`, revoking `allow_private_ip`, or removing a project from `shared_project_ids` evicts idle pooled sockets immediately so revocations cannot be bypassed via keep-alive reuse.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Every connection field (`allow_private_ip`, `allowed_cidrs`, `data_region`, `baa_covered`, `upstream_zdr_verified`, `shared_project_ids`) can be viewed and updated via API, Console, CLI, and GitOps.
  - Transitioning a connection to `DRAINING` routes 100% of new requests to healthy fallbacks while allowing in-flight SSE streams to finish without error.
  - Attempting to delete a connection with bound deployments returns `HTTP 409` with exact dependent resource identifiers.
- **20. Relevant Evidence & Research Confidence:** HIGH — Missing fields and lifecycle gaps in `connections.go` and `ProvidersPage.tsx` are [SOURCE_VERIFIED] on ac9ae87a and documented in TEEP-01-DOC-04/07.
- **21. Proposal Classification:** `IMPROVE` (`EXPANDED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Eliminates Day-2 connection re-creation churn by supporting full-field updates, explicit Org vs selected-Project sharing, graceful in-flight stream draining, deprecation, retirement, and dependency-checked deletion. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-015 — In-Place Vault Secret Rotation, OCI_REGISTRY_AUTH & Consumer Dependency Tracking

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-015` · `CAP` Traceability: `CAP-014` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-02` (Provider Management) -> **Vault Credential Lifecycle**
- **3. Plain-Language Product Summary:** Lets administrators update an expired or scheduled API key in place inside Tanpopo's encrypted Vault so every provider connection, webhook, or private extension registry using that secret picks up the new key automatically with zero downtime. It also shows exactly which resources depend on each secret before anyone rotates or deletes it.
  - *Practical Example:* During a quarterly security rotation, an Org Owner rotates secret `cred_openai_prod` via `PUT /admin/v1/secrets/cred_openai_prod` (with an optional pre-commit connection probe); all 8 bound project connections atomically cut over to the new ciphertext version on the next `ConfigSnapshot` epoch without editing a single connection or dropping active traffic.
  - *Explicit Trade-Offs:* Keeping previous secret versions indefinitely would increase breach blast radius if an old key was compromised; therefore, in-place rotation overwrites/zeroizes old secret material in RAM immediately by default, storing only cryptographic fingerprint/version metadata for audit history (`DEC-CAND-21`).
- **4. User Problem & Business Value:** In RC9, `/admin/v1/secrets` only supports `POST`, `GET` (metadata), and `DELETE` (`0` `PUT`/`PATCH` rotation), forcing operators to mint a brand-new `credential_id`, manually edit every bound connection/webhook, and then delete the old secret; Console also omits `OCI_REGISTRY_AUTH` and bound-consumer counts.
- **5. Target Personas:** `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-05 (Enterprise IAM & Workforce Security Lead)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in internal/admin/http/secrets.go and ProvidersPage.tsx [SOURCE_VERIFIED]: write-only AES-256-GCM sealing works, but zero in-place rotation endpoint exists, `OCI_REGISTRY_AUTH` cannot be created from the Console UI, and secret rows do not display reverse consumer counts or age/expiry warnings.
- **7. Proposed Complete Functional Behavior:** Add `PUT /admin/v1/secrets/{credentialId}` atomic secret-material rotation (`secret_version` increment + `If-Match` + optional dry-run probe against bound connections before commit) [DESIGN_PROPOSAL]. Expose `OCI_REGISTRY_AUTH` (Docker config JSON / bearer / basic auth for Harbor/Artifactory/GHCR) in Console, display reverse-dependency consumer lists (connections, MCP servers, webhooks, OCI sources) and rotation age/expiry badges, and block `DELETE` with `HTTP 409` whenever active consumers remain bound.
- **8. User-Facing Subfeatures:**
  - Zero-downtime in-place secret rotation (`PUT /admin/v1/secrets/{id}`) bumping `secret_version` and `config_epoch`.
  - Optional 'Verify Against Bound Connections Before Committing' check preventing accidental rotation to a typoed key.
  - First-class `OCI_REGISTRY_AUTH` secret kind in Console and CLI for private `.tpkg` OCI registries (FEAT-110).
  - Consumer dependency drawer & rotation-age indicators showing every connection, MCP server, or webhook bound to a secret.
- **9. End-to-End Lifecycle:** Create write-only secret (v1) -> bind to connections/registries/webhooks -> monitor rotation age / optional expiry reminder -> Rotate in place to v2 (optional pre-commit probe -> AEAD re-seal under current DEK -> evict pooled sockets & zeroize v1 in RAM) -> Delete (`409` if bound; zeroized + JCS audited when unbound).
- **10. Primary & Alternative User Journeys:** Primary: Automated CI job or admin rotates API key via CLI/API; all Data-Plane pods load `secret_version=2` and refresh upstream transports cleanly. Alternative: Admin pastes an invalid key with `verify_bound=true`; pre-commit blind probe fails `401` and rotation aborts cleanly while `secret_version=1` continues serving.
- **11. Administrative & Permission Boundaries:** Strict write-only contract: `GET` endpoints never return plaintext or raw ciphertext bytes to any role, including `OPERATOR`; rotation requires `ActionSecretWrite` + `If-Match` (and Four-Eyes when policy requires).
- **12. Automation Opportunities:** Configurable secret-age notifications (e.g., 75/85/90 days via DOM-19 Notification Center) and CLI/API hooks for HashiCorp Vault / External Secrets rotation pipelines.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Envelope encryption under Project/Org DEKs (FEAT-059), immediate RAM zeroization (`clear()`) of superseded secret slices on epoch refresh, and JCS audit trails logging `secret_version` and SHA-256 key-id metadata only.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Vault AEAD envelope re-sealing, secret version tracking, `config_epoch` invalidation, and reverse-dependency queries reside in the Core Control Plane.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Secrets & Vault tab in ProvidersPage / Security with Rotate Drawer, Consumer Badge, and OCI_REGISTRY_AUTH support.
  - *CLI:* `tanpopo secret create | list | inspect-consumers | rotate | delete`.
  - *Admin / Data-Plane API:* PUT /admin/v1/secrets/{credentialId} and consumer-count metadata on GET /admin/v1/secrets.
  - *GitOps / Terraform / Packs:* Declarative secret slot references with optional minimum `secret_version` assertions (`DEC-CAND-21`).
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-059 (Vault 3-tier envelope encryption), ARCH-004 (pooled socket eviction), and ARCH-008 (BlindBroker).`, `Required by FEAT-110 (Private OCI Registry authentication via `OCI_REGISTRY_AUTH`).` · Related Decisions: `DEC-CAND-21`
- **17. Failure & Recovery Behavior:** If pre-commit verification fails, rotation rolls back atomically with zero impact on live traffic; if an unverified bad key is committed, rotating again immediately restores service within one snapshot epoch.
- **18. Security, Privacy & Financial Implications:** Eliminates long windows where both old and duplicate connection objects exist during manual rotation, and prevents accidental outage-inducing deletion of shared production credentials.
- **19. Preliminary Measurable Acceptance Criteria:**
  - `PUT /admin/v1/secrets/{id}` rotates credential material in place without changing `credential_id` and updates live Data-Plane outbound auth within one epoch.
  - `OCI_REGISTRY_AUTH` secrets can be created, rotated, and bound to private OCI extension sources via both Console and CLI.
  - Attempting to delete a secret bound to any connection, webhook, or registry returns `HTTP 409` listing all bound consumers.
- **20. Relevant Evidence & Research Confidence:** HIGH — Missing `PUT /secrets/{id}` and `OCI_REGISTRY_AUTH` UI gaps are [SOURCE_VERIFIED] in `secrets.go` and `ProvidersPage.tsx` on ac9ae87a.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Enterprise security standards require 30/60/90-day API key rotation without downtime or manual re-binding across dozens of provider connections, webhooks, and private OCI registries. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-016 — Provisioned Throughput (PTU / Reserved Tier) Capacity Modeling & PAYG Spillover

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-016` · `CAP` Traceability: `CAP-015` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-02` (Provider Management) -> **Reserved Capacity & Spillover Management**
- **3. Plain-Language Product Summary:** Maximizes the return on pre-paid cloud AI capacity (such as Azure Provisioned Throughput Units, AWS Bedrock Reserved Tiers, and Google Vertex Provisioned units). Tanpopo routes traffic to pre-paid capacity first up to a safe utilization target (such as 92%) and smoothly spills sudden traffic bursts over to pay-as-you-go endpoints instead of failing with rate-limit errors.
  - *Practical Example:* A financial services firm pays USD 32,000/month for 100 Azure OpenAI PTUs in Sweden Central; Tanpopo keeps the PTU deployment saturated at ~90% during market hours for predictable low TTFT, bursts excess peak traffic automatically to an EU DataZone pay-per-token deployment, and separates amortized PTU cost from burst token spend in FinOps chargeback.
  - *Explicit Trade-Offs:* Estimating upstream PTU utilization across multiple gateway pods requires shared Valkey token-rate windows plus reactive feedback from vendor rate-limit headers (`x-ratelimit-remaining-tokens`); overly aggressive saturation (>98%) risks occasional 429 retries, so defaults target a configurable 85–92% soft ceiling.
- **4. User Problem & Business Value:** Enterprises buying PTUs either under-utilize expensive sunk-cost capacity or suffer sudden HTTP 429 errors during traffic spikes; intelligent spillover eliminates idle PTU waste while protecting application SLAs.
- **5. Target Personas:** `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED]: `SELF_HOSTED_INFRA` supports basic hourly cost metadata, but commercial deployments lack provisioned TPM/RPM capacity envelopes, cluster-wide saturation tracking, or automatic proactive PAYG spillover prior to hitting HTTP 429.
- **7. Proposed Complete Functional Behavior:** Extend `ModelDeployment` with optional `capacity_commitment` metadata (`PROVISIONED_PTU` / `RESERVED_TIER`, `committed_tpm`, `committed_rpm`, `hourly_commitment_micro_usd`, `soft_spillover_utilization_pct` default 90%) [DESIGN_PROPOSAL]. Track rolling cluster-wide token/request utilization in Valkey (calibrated by upstream remaining-quota response headers), prioritize sunk-cost PTU targets below the soft threshold, proactively spill excess requests to secondary PAYG targets above threshold, and attribute amortized PTU shares vs marginal burst spend in `DOM-06` FinOps.
- **8. User-Facing Subfeatures:**
  - Provisioned capacity envelope configuration (PTU count, TPM/RPM quota, hourly commitment cost, spillover %) on Deployments.
  - Proactive pre-429 spillover routing from saturated PTU deployments to residency-compliant PAYG fallback deployments.
  - Priority-class reservation allowing `MISSION_CRITICAL` routes to claim the final 10% of PTU headroom while `BATCH_BACKFILL` spills early.
  - FinOps PTU utilization vs idle-waste and burst-spillover analytics panel.
- **9. End-to-End Lifecycle:** Operator configures PTU deployment (Primary) + PAYG deployment (Secondary) on route -> Gateway tracks estimated + upstream-reported token burn -> routes to PTU while utilization < soft ceiling -> spills burst calls to PAYG seamlessly -> reconciles hourly PTU commitment amortization across consuming cost centers.
- **10. Primary & Alternative User Journeys:** Primary: Morning traffic spike hits 135% of PTU TPM; first 90% runs on PTU with low latency while 45% spills cleanly to PAYG with zero HTTP 429s. Alternative: Route is strictly locked to PTU-only (`disallow_payg_spillover` to cap marginal variable spend at USD 0); excess non-critical calls queue briefly or shed with `429`.
- **11. Administrative & Permission Boundaries:** PTU capacity envelopes and hourly commitment costs are configured by `OPERATOR` / `ORG_OWNER` and audited by `FINOPS_VIEWER`.
- **12. Automation Opportunities:** Automatically ingests vendor `x-ratelimit-remaining-*` and `anthropic-ratelimit-*` headers to self-correct local token-saturation estimates when multiple external apps share a PTU endpoint.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Cluster-coherent saturation counters via Valkey (gracefully degrading to per-pod proportional slices `quota / N_pods` if Valkey is unreachable) and strict regional residency preservation on PAYG spillover targets.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Real-time TPM/RPM saturation tracking and sub-millisecond spillover target selection execute inside the Core Data Plane Route Resolver and FinOps attribution path.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Capacity Mode (PAYG vs Provisioned PTU) fields on Deployment drawers and PTU Utilization gauge in FinOpsPage.
  - *CLI:* `tanpopo deployment create/update --capacity-mode ptu ...` and utilization stats in `tanpopo finops summary`.
  - *Admin / Data-Plane API:* capacity_commitment object on `/admin/v1/deployments` and PTU metrics on `/admin/v1/usage/summary`.
  - *GitOps / Terraform / Packs:* Declarative PTU capacity and spillover thresholds on `model_deployment` and `route_policy` resources.
- **16. Dependencies & Cross-Domain Relationships:** `Integrates with FEAT-026 (SLO-aware dynamic route scoring), FEAT-034 (priority classes), and FEAT-040 (PTU/GPU hourly cost amortization).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** If PTU still returns an unexpected 429 (e.g., burst sub-second window), `SM-04` `THROTTLED_COOLDOWN` honors `Retry-After` and fails over pre-first-byte to the PAYG target.
- **18. Security, Privacy & Financial Implications:** Prevents surprise variable PAYG bills when `disallow_payg_spillover` is set, or maximizes sunk-cost ROI (projected 25–50% commitment waste reduction `[HYPOTHESIS]`) while upholding regional residency fences.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Under synthetic load at 150% of configured PTU TPM, ~90% of tokens route to the PTU target and ~60% spill cleanly to PAYG with 0 client-visible 429 errors.
  - FinOps settlements distinguish amortized PTU capacity usage (`capacity_tier=PROVISIONED`) from variable PAYG burst spend (`capacity_tier=PAYG_SPILLOVER`).
  - PAYG spillover never selects a target outside the route's effective `AllowedDataRegions`.
- **20. Relevant Evidence & Research Confidence:** MEDIUM-HIGH — Azure Foundry PTU vs Global Standard and AWS Bedrock Reserved vs Mantle split quotas are [SOURCE_VERIFIED] in SRC-059 and SRC-061; exact % commitment savings are `[HYPOTHESIS]` pending customer workload profiles.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — High-ROI enterprise optimization for organizations with pre-paid Azure PTUs, AWS Bedrock Reserved Tiers, or Vertex Provisioned Throughput, layered cleanly atop P0/P1 routing and FinOps foundations. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `v1.1 (Core)`)*

---

## `DOM-03` — Model Intelligence (`7` Proposed Features)

- **Domain Overview:** Model Intelligence (DOM-03) transforms model capabilities, context limits, pricing tiers, deprecation timelines, and weight licensing from error-prone manual data entry into a curated, auditable 4-layer intelligence system. It reconciles signed offline reference catalogs with live account-authenticated model discovery and sticky enterprise contract overrides so teams get accurate USD-per-1M-token pricing on Day-1 and proactive, capability-verified migration guidance whenever vendors sunset model checkpoints.
- **Existing RC9 Baseline Posture:** RC9 freezes settled pricing snapshots on usage_settlements [SOURCE_VERIFIED], which preserves historical immutability, but ships zero bundled model or pricing catalog [SOURCE_VERIFIED]. Operators must manually convert vendor USD/1M prices into raw nano_usd_per_token integers, which suffer fractional truncation on 1,664 entries in the 2026-11-27 LiteLLM 7,193-model catalog snapshot [RUNTIME_VERIFIED]. Furthermore, ValidateConnection discards detected_model_ids in the UI [SOURCE_VERIFIED], Logical Models lack GET/{id}, PATCH, and DELETE/retire endpoints (PG-004) [SOURCE_VERIFIED], Console ModelsPage.tsx cannot edit deployment rate cards [SOURCE_VERIFIED], and neither deprecation monitoring nor model license gating exists.
- **Progressive Self-Hosted to Enterprise Experience:** Solo builders select a provider connection, run 1-click model discovery, and bulk-onboard entitled models pre-populated with verified context windows, capabilities, and human-readable USD/1M reference rates—or mark local Ollama/vLLM models as explicit zero-rate self-hosted deployments. Enterprises layer custom negotiated EA/EDP discounts as sticky manual overrides with provenance audit trails, enforce EU/jurisdictional model-weight license gates, and run automated deprecation impact simulations before switching production routes.
- **Technical Selections Deferred to `TEEP-03`:** Exact internal fixed-point rate-card unit scale (nano_usd_per_1m_tokens vs alternative fixed-point scales), sub-micro-USD carry accumulator rounding rules, and foreign-exchange (FX) normalization policies are explicitly deferred to TEEP-03 (DEC-CAND-08), alongside offline catalog pack signature framing (DEC-CAND-04).

| ID | Subdomain | Feature Name | Class | Disposition | Priority | Placement Candidate |
|---|---|---|---|---|---|---|
| `FEAT-017` | Precision Pricing & Multimodal Rate Cards | **High-Precision Multimodal & Tiered Rate-Card Modeling (USD/1M Dual-Unit UX)** | `CAT-ADMIN` | `REPLACE` | `P0-Foundational` | `CORE_CONTROL_PLANE` |
| `FEAT-018` | Reference Model Catalog & Air-Gap Sync | **Signed Offline Model Intelligence Packs & Opt-In Sanitized Catalog Sync** | `CAT-ADMIN` | `ADD` | `P0-Foundational` | `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` |
| `FEAT-019` | Account-Entitled Model Discovery | **BlindBroker Account-Authenticated Model Discovery & 1-Click Bulk Onboarding** | `CAT-ADMIN` | `IMPROVE` | `P0-Foundational` | `CORE_CONTROL_PLANE` |
| `FEAT-020` | Enterprise Contract Overrides & Provenance | **4-Layer Catalog Precedence, Sticky Enterprise Rate Overrides & Price Provenance** | `CAT-ADMIN` | `ADD` | `P1-Enterprise-Core` | `CORE_CONTROL_PLANE` |
| `FEAT-021` | Logical Model & Deployment Lifecycle | **Complete Logical Model & Deployment Lifecycle (Edit, Versioned Rates, Deprecate & Retire)** | `CAT-ADMIN` | `IMPROVE` | `P0-Foundational` | `CORE_CONTROL_PLANE` |
| `FEAT-022` | Proactive Fleet Lifecycle & Migration Intelligence | **Automated Model Deprecation Monitoring & Capability-Verified Migration Advisor** | `CAT-ADMIN` | `ADD` | `P1-Enterprise-Core` | `CORE_CONTROL_PLANE` |
| `FEAT-023` | Model Licensing & Legal Governance | **Model Weights License & Jurisdictional Compliance Gate** | `CAT-ADMIN` | `ADD` | `P2-Differentiator` | `CORE_CONTROL_PLANE` |

### FEAT-017 — High-Precision Multimodal & Tiered Rate-Card Modeling (USD/1M Dual-Unit UX)

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-017` · `CAP` Traceability: `CAP-016` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-03` (Model Intelligence) -> **Precision Pricing & Multimodal Rate Cards**
- **3. Plain-Language Product Summary:** Replaces RC9's coarse per-token integer pricing with a high-precision rate-card model that eliminates sub-cent rounding drift on low-cost flash, cached, and embedding tokens while letting admins view and edit prices in familiar 'USD per 1 Million tokens' units. It also models multimodal image pixels, audio seconds/tokens, document OCR pages, search fees, and explicit zero-rate self-hosted GPUs.
  - *Practical Example:* A FinOps analyst enters USD 0.0875 per 1M cached input tokens (8.75 nano-USD/token) for a high-volume classification model in the Console's USD/1M field; instead of truncating to 8 or rounding to 9 nano-USD/tok (~12% billing error in RC9), Tanpopo stores the exact fixed-point integer rate and previews both USD/1M and base integer units side by side.
  - *Explicit Trade-Offs:* Higher fixed-point rate precision prevents per-rate truncation, while tiny single-sentence embedding calls still interact with sub-micro-USD ledger settlement rounding; exact internal representation (`nano_usd_per_1m_tokens` vs alternative fixed-point scales), sub-micro carry accumulator rules, non-USD foreign-exchange (FX) conversion policies, and sub-femto-USD rounding policies are explicitly deferred to TEEP-03 (DEC-CAND-08).
- **4. User Problem & Business Value:** Empirical audit of the 2026-11-27 LiteLLM 7,193-model catalog snapshot (SRC-045) proved that 1,664 real token-cost fields (such as 8.75 and 144.375 nano-USD/token) have fractional sub-nano values that suffer up to ~12% rounding drift in RC9's RateCard v2, while missing Admin DTO fields blocked media/rerank pricing.
- **5. Target Personas:** `PER-01 (Solo Builder / First-Time Self-Hoster)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION (warranting schema/unit REPLACE) [RUNTIME_VERIFIED, SOURCE_VERIFIED]: RateCard v2 uses `nano_usd_per_token` integers (truncating 1,664 fractional sub-nano entries observed in the 2026-11-27 LiteLLM snapshot); `internal/admin/http/models.go:60-71` omits `ImagePer1MPixelsNanoUSD`, `AudioOutputPerSecNanoUSD`, and `RerankUnitNanoUSD`; and Console requires raw `nano-USD/token` integers.
- **7. Proposed Complete Functional Behavior:** Supersede RateCard v2 with a high-precision multi-dimensional integer RateCard schema covering all `1,664` fractional sub-nano token rates observed in the `2026-11-27` LiteLLM catalog snapshot without IEEE-754 floating-point drift [DESIGN_PROPOSAL] (deferring exact fixed-point scale selection—such as `int64 nano_usd_per_1m_tokens` at 10^-15 USD/tok—plus sub-micro carry math and FX/sub-femto policies to `TEEP-03` `DEC-CAND-08`). Expose 100% of multimodal fields (text, reasoning, 5m/1h cache writes, cache reads, image pixels/count, audio input/output tokens & seconds, video seconds, OCR pages, rerank units, web-search queries, explicit `self_hosted_zero_rate`) in Admin API and provide synchronized `USD / 1M` <-> raw integer dual-unit inputs in Console.
- **8. User-Facing Subfeatures:**
  - Dual-unit Console rate-card editor (`USD per 1M tokens` decimal string input synchronized losslessly with exact integer storage).
  - Complete multimodal rate dimensions: text, reasoning, 5m/1h cache writes, cache reads, images, audio, video, OCR pages, rerank, and search fees.
  - Explicit `self_hosted_zero_rate` and `commercial_zero_rate` flags distinguishing legitimate USD 0 endpoints from misconfigured missing prices.
  - Automatic, lossless forward migration of existing v1/v2 `pricing_json` rows with immutable historical settlement snapshots preserved.
- **9. End-to-End Lifecycle:** RateCard pre-filled from Signed Model Intelligence Pack (FEAT-018) or entered via USD/1M UI/YAML -> validated as exact decimal string (rejecting binary float artifacts & scientific notation) -> persisted as versioned integer RateCard -> stamped immutably into `applied_pricing_snapshot_json` on settled requests.
- **10. Primary & Alternative User Journeys:** Primary: Operator imports Gemini Flash or Claude Haiku with sub-nano cached read rates (`USD 0.025 / 1M`); UI displays `0.025 USD / 1M` cleanly and settles integer micro-USD accurately. Alternative: Operator sets up local Ollama and toggles `Self-Hosted Zero Rate`; requests settle at USD 0 without triggering unmetered-spend alarms.
- **11. Administrative & Permission Boundaries:** Rate-card creation and updates require `ActionModelWrite` / `ActionDeploymentWrite` (`ORG_OWNER`, `OPERATOR`, or delegated `BU_ADMIN`); `FINOPS_VIEWER` has full read/audit visibility into rate versions.
- **12. Automation Opportunities:** Automatic conversion and validation of decimal strings during GitOps `tanpopo config apply` and catalog pack imports, rejecting IEEE-754 float precision artifacts like `0.0000014000000000000001`.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** 128-bit overflow-checked integer multiplication, zero runtime IEEE-754 float math, backward-compatible migration of existing SQLite/PostgreSQL deployments, and tabular-nums monospace formatting in Astryx UI.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — RateCard schema validation, lossless migration, Admin API serialization, and Data-Plane integer settlement sit at the core contract boundary between Model Intelligence and FinOps.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Dual-unit RateCard editor in ModelsPage Deployment drawers and /setup starter model review.
  - *CLI:* `tanpopo deployment set-pricing --input-usd-per-1m 2.50 --cached-input-usd-per-1m 0.25 ...`.
  - *Admin / Data-Plane API:* Complete RateCard DTO on POST/PATCH/GET `/admin/v1/deployments` in admin-v1.yaml.
  - *GitOps / Terraform / Packs:* Human-readable `usd_per_1m` or canonical integer fields in declarative deployment manifests.
- **16. Dependencies & Cross-Domain Relationships:** `Anchored on Founder/Architecture decision `DEC-CAND-08` (exact unit scale & sub-micro carry deferred to TEEP-03).`, `Required by FEAT-018 (Model Intelligence Packs), FEAT-037 (two-phase FinOps), and FEAT-040 (conditional tier pricing).` · Related Decisions: `DEC-CAND-08`
- **17. Failure & Recovery Behavior:** Out-of-range rates, negative values, or ambiguous floating-point scientific notation fail validation at write time with `HTTP 400 invalid_rate_card` before reaching the Data Plane.
- **18. Security, Privacy & Financial Implications:** Eliminates the ~12% rounding error observed on sub-nano rates in RC9 while preserving deterministic integer auditability across every settled request.
- **19. Preliminary Measurable Acceptance Criteria:**
  - All 1,664 fractional sub-nano token rates observed in the 2026-11-27 LiteLLM catalog snapshot round-trip with zero truncation drift.
  - All multimodal pricing fields (including image pixels, audio output seconds, rerank units, OCR pages, and 1h cache writes) round-trip through Admin API and Console UI.
  - Existing RC9 RateCard v1/v2 database rows migrate automatically with zero loss of historical `applied_pricing_snapshot_json` integrity.
- **20. Relevant Evidence & Research Confidence:** HIGH — Empirical catalog audit of 7,193 models / 1,664 sub-nano rates is [RUNTIME_VERIFIED] (SRC-045..047); missing DTO fields in `models.go:60-71` are [SOURCE_VERIFIED] on ac9ae87a; exact fixed-point scale and FX/sub-micro rules are deferred to `TEEP-03` (`DEC-CAND-08`).
- **21. Proposal Classification:** `REPLACE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — RC9's nano_usd_per_token unit truncates fractional sub-nano vendor prices on 1,664 real catalog entries and forces error-prone mental arithmetic in the Console; high-precision rate cards are foundational to all FinOps and Model Catalog workflows. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-018 — Signed Offline Model Intelligence Packs & Opt-In Sanitized Catalog Sync

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-018` · `CAP` Traceability: `CAP-017` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-03` (Model Intelligence) -> **Reference Model Catalog & Air-Gap Sync**
- **3. Plain-Language Product Summary:** Ships Tanpopo with a built-in, cryptographically signed encyclopedia of AI models—including context window limits, supported capabilities (tools, vision, reasoning, JSON schema), deprecation dates, and sanitized reference pricing. It works 100% offline in air-gapped networks and can be updated safely via signed offline packs or an opt-in feed with price-spike protection.
  - *Practical Example:* An operator in a disconnected defense network upgrades model metadata without internet access by importing a signed `model-intel-2026-12.tpkg` file via `tanpopo catalog import-pack`; meanwhile, a cloud team enables opt-in catalog sync, which automatically stages a sudden 40% upstream vendor price change for human FinOps review rather than silently altering production budgets.
  - *Explicit Trade-Offs:* Upstream community price lists contain noisy data (our 2026-11-27 audit found 22,940 scientific-notation literals and 66 IEEE-754 float artifacts in LiteLLM's JSON [RUNTIME_VERIFIED]); therefore, Tanpopo never ingests raw third-party JSON at runtime, relying instead on an offline deterministic Decimal compiler and signed packs.
- **4. User Problem & Business Value:** Eliminates tedious manual lookup of context windows, tool/vision flags, and multi-tier USD/1M token rates across hundreds of models while protecting air-gapped and FinOps-sensitive deployments from poisoned upstream feeds.
- **5. Target Personas:** `PER-01 (Solo Builder / First-Time Self-Hoster)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED]: `internal/provider/catalog` contains zero bundled model capability, context-window, deprecation, or reference-pricing records; every model and rate card must be typed by hand.
- **7. Proposed Complete Functional Behavior:** Provide an offline deterministic Decimal catalog build pipeline reconciling `models.dev` (`10,078` models), `litellm_model_prices.json` (`7,193` models, stripping float artifacts), `openrouter`, and official vendor docs into a signed Model Intelligence Pack embedded in the Tanpopo binary [DESIGN_PROPOSAL]. Support out-of-band offline updates (`tanpopo catalog import-pack` / `.tpbundle`) and strictly opt-in `CATALOG_FEED` polling (`0` outbound calls when disabled) guarded by an anti-poisoning FinOps quarantine gate whenever list prices swing `> +/-25%` or flip unexpectedly to `USD 0`.
- **8. User-Facing Subfeatures:**
  - Embedded, cryptographically signed baseline Model Intelligence Pack working 100% offline on first boot.
  - Searchable Model Encyclopedia in Console showing context limits, modalities, capabilities, license class, and reference USD/1M rates.
  - Offline CLI/Console pack importer (`tanpopo catalog import-pack`) and `.tpbundle` courier integration for air-gapped environments.
  - Opt-in sanitized `CATALOG_FEED` sync with `> +/-25%` price-swing and unexpected-zero-rate staging quarantine.
- **9. End-to-End Lifecycle:** Offline CI compiler reconciles & signs canonical Decimal catalog pack -> embedded in release binary -> loaded into Layer 1/2 catalog tables on boot -> updated via offline signed pack or opt-in feed -> staged anomalies reviewed & approved by `ORG_OWNER` / `FINOPS_VIEWER`.
- **10. Primary & Alternative User Journeys:** Primary: Fresh offline install boots with hundreds of verified models ready for 1-click onboarding in `/setup`. Alternative: Opt-in feed receives a pack where a commercial model's price drops to `USD 0` due to upstream error; anti-poisoning gate quarantines that entry in `STAGED_REVIEW` while applying clean additive models.
- **11. Administrative & Permission Boundaries:** Importing catalog packs or enabling outbound `CATALOG_FEED` sync requires `OPERATOR` / `ORG_OWNER`; approving quarantined price swings requires `ORG_OWNER` or `FINOPS_VIEWER`.
- **12. Automation Opportunities:** Automatically populates context windows, capability flags (`DECLARED` vs `VERIFIED`), and multi-tier reference pricing whenever a user selects or discovers a known model.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Zero outbound network requests by default (air-gap invariant), cryptographic signature verification on every catalog pack (`DEC-CAND-04`), and forward-only updates that never overwrite manual enterprise overrides (FEAT-020).
- **14. Core vs Extension Placement Candidate:** `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` — Core Control Plane verifies signatures, enforces FinOps price-swing quarantine gates, and merges catalog layers, while updated Model Intelligence Packs distribute as signed `.tpkg` / `CATALOG_FEED` artifacts.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Reference Catalog Browser & Pack Status / Quarantine Review panel in ModelsPage.
  - *CLI:* `tanpopo catalog list | inspect | import-pack | sync-status`.
  - *Admin / Data-Plane API:* GET /admin/v1/catalog/models, POST /admin/v1/catalog/packs:import, and quarantine review endpoints.
  - *GitOps / Terraform / Packs:* Pinned catalog pack digest in `tanpopo.lock`.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-017 (high-precision RateCard), FEAT-020 (4-layer precedence), and ARCH-015 / `DEC-CAND-04` (signed pack verification).`, `Supplies deprecation dates and license metadata to FEAT-022 and FEAT-023.` · Related Decisions: `DEC-CAND-04`, `DEC-CAND-08`
- **17. Failure & Recovery Behavior:** Invalid pack signatures or schema mismatches abort import atomically before touching SQLite/PostgreSQL; if an opt-in remote feed is unreachable, gateway operations continue uninterrupted on the embedded/last-good pack.
- **18. Security, Privacy & Financial Implications:** Prevents supply-chain FinOps poisoning (malicious or buggy feed zeroing out commercial prices to bypass budget holds) via signature verification plus `+/-25%` / zero-rate anomaly quarantine.
- **19. Preliminary Measurable Acceptance Criteria:**
  - A fresh air-gapped Tanpopo boot (`0` internet access) exposes curated models, capabilities, and reference USD/1M rates across all bundled official providers.
  - Zero IEEE-754 float rounding artifacts or scientific-notation literals pass the catalog pack validator.
  - Simulated catalog update containing a 50% price swing or unexpected commercial USD 0 rate is held in quarantine pending admin approval.
- **20. Relevant Evidence & Research Confidence:** HIGH — Cross-catalog audit (`models.dev` 10,078 models, `litellm` 7,193 models / 22,940 sci-notation literals / 66 float artifacts, `openrouter` 476 models) is [RUNTIME_VERIFIED] on 2026-11-27 (SRC-045..047).
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — RC9 ships zero built-in model metadata or reference pricing, forcing 100% manual entry; an embedded, cryptographically signed offline catalog is essential for both `<90s` self-hosted onboarding and air-gap enclaves. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-019 — BlindBroker Account-Authenticated Model Discovery & 1-Click Bulk Onboarding

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-019` · `CAP` Traceability: `CAP-018` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-03` (Model Intelligence) -> **Account-Entitled Model Discovery**
- **3. Plain-Language Product Summary:** Queries a connected provider account securely using its sealed credentials to discover the exact models, custom Azure deployments, AWS Bedrock inference profiles, or local Ollama weights that account can actually use right now. Operators can then onboard dozens of discovered models—automatically matched with capabilities and reference pricing—in a single click.
  - *Practical Example:* An enterprise architect connects an AWS Bedrock `us-east-1` IAM role and clicks 'Discover Entitled Models'; Tanpopo lists all 18 enabled regional foundation models and cross-region inference profiles, automatically tags `global.*` profiles as `MULTI_REGION`, matches reference USD/1M pricing from the signed catalog, and registers the selected 6 models + deployments in one atomic batch.
  - *Explicit Trade-Offs:* Some providers (such as Azure custom deployment names like `corp-chat-prod` or private fine-tunes) return opaque deployment slugs without base-model pricing; when auto-matching confidence is below 100%, the bulk onboarding UI prompts the operator to confirm the canonical base model or enter a custom rate card rather than guessing silently.
- **4. User Problem & Business Value:** In RC9, `ValidateConnection` returned transient `detected_model_ids` which `ProvidersPage.tsx` discarded completely [SOURCE_VERIFIED], forcing admins to manually type every Logical Model and Deployment one by one.
- **5. Target Personas:** `PER-01 (Solo Builder / First-Time Self-Hoster)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-04 (Business Unit / Line-of-Business Delegated Admin)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION [SOURCE_VERIFIED]: `ValidateConnection` extracts basic `detected_model_ids` on OpenAI-style `/v1/models`, but results are never persisted, `ProvidersPage.tsx` ignores them, Bedrock `ListInferenceProfiles`, Azure deployment metadata, and Ollama `/api/show` capabilities/context lengths are not queried, and no bulk onboarding API/UI exists.
- **7. Proposed Complete Functional Behavior:** Add two-phase BlindBroker `DiscoverModels` probes across official adapters (`/v1/models`, Bedrock `ListFoundationModels` + `ListInferenceProfiles`, Azure/Foundry deployments mapping custom deployment names to underlying base model + version, Vertex publisher models, Ollama `/api/tags` + `/api/show` reading local architecture/context/vision/tools metadata) [DESIGN_PROPOSAL]. Persist results in `connection_discovered_models`, cross-join against Layer 1/2 Model Intelligence (FEAT-018), and expose a 1-click Bulk Onboard drawer & CLI command creating Logical Models, Deployments, and optional 1:1 Routes atomically (targeting `<60s` multi-model onboarding `[HYPOTHESIS]`).
- **8. User-Facing Subfeatures:**
  - Account-authenticated `DiscoverModels` probe persisting entitled model/profile inventory per connection.
  - Smart canonical matcher linking custom Azure deployment slugs, Bedrock regional/global profiles, and Ollama tags to signed catalog capabilities & pricing.
  - 1-Click Bulk Onboarding table with checkbox filtering, inline USD/1M rate preview/override, and optional starter route creation.
  - Automatic residency hazard tagging (`MULTI_REGION` on Bedrock `global.*`; quarantine warning on Ollama `*:cloud` under `ON_PREM`).
- **9. End-to-End Lifecycle:** Connection created/probed -> `DiscoverModels` runs via BlindBroker -> discovered inventory stored & diffed against existing deployments -> Operator selects desired models in Bulk Onboard UI/CLI -> atomic transaction creates/links LogicalModels + ModelDeployments -> subsequent re-discovery highlights newly available or newly disappeared upstream models.
- **10. Primary & Alternative User Journeys:** Primary: Solo user connects local Ollama; discovery reads `/api/tags` + `/api/show` for `qwen3:32b` and `llava`, auto-fills context windows + vision/tool capabilities + `self_hosted_zero_rate`, and onboards both in 1 click. Alternative: Custom fine-tune `ft:gpt-4o-mini:corp::99a` is discovered; UI maps to fine-tuned GPT-4o-mini rate tier or prompts for explicit rate confirmation before enabling.
- **11. Administrative & Permission Boundaries:** Discovery requires `ActionConnectionTest` / `ActionConnectionRead`; bulk onboarding requires `ActionModelWrite` + `ActionDeploymentWrite` within the target org/project.
- **12. Automation Opportunities:** Scheduled or on-demand re-discovery detects when cloud IAM entitlements change or local Ollama/vLLM weights are pulled/removed, surfacing drift badges on affected deployments.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** 100% BlindHostCredentialBroker execution (`0` raw credentials in Wasm), pagination handling for accounts with >500 models (e.g., OpenRouter / Together / Bedrock), and atomic all-or-nothing batch onboarding.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Orchestrates BlindHostCredentialBroker discovery calls, persists `connection_discovered_models`, cross-matches against the Model Intelligence catalog, and executes atomic bulk onboarding in the Control Plane.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* 'Discover & Bulk Import Models' drawer in ProvidersPage, ModelsPage, and `/setup` Quickstart.
  - *CLI:* `tanpopo provider discover-models <conn-id>` and `tanpopo model onboard-discovered <conn-id> --select ...`.
  - *Admin / Data-Plane API:* POST `/admin/v1/connections/{id}:discover-models`, GET `/admin/v1/connections/{id}/discovered-models`, POST `/admin/v1/deployments:bulk-onboard`.
  - *GitOps / Terraform / Packs:* Bulk discovery can export ready-to-commit declarative YAML definitions via `--output yaml`.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on ARCH-008 (BlindHostCredentialBroker), FEAT-012 (blind probing), FEAT-017 (RateCard), and FEAT-018 (Model Intelligence Packs).` · Related Decisions: None (Standard Catalog Review)
- **17. Failure & Recovery Behavior:** If a provider account lacks list-models IAM permission (e.g., restricted `bedrock:InvokeModel`-only role) while inference works, degrades gracefully to manual/catalog-picker selection with a clear informational notice.
- **18. Security, Privacy & Financial Implications:** Prevents unpriced or residency-violating discovered profiles from silently activating without explicit rate-card confirmation and regional tag assignment.
- **19. Preliminary Measurable Acceptance Criteria:**
  - DiscoverModels persists entitled models across OpenAI, Anthropic, Gemini, AWS Bedrock, Azure/Foundry, and Ollama through BlindHostCredentialBroker.
  - Operators can select 10+ discovered models in Console and onboard LogicalModels + ModelDeployments with pre-filled USD/1M rate cards in one atomic action.
  - Ollama `*:cloud` tags on `ON_PREM` connections and Bedrock `global.*` profiles are visually flagged and gated by residency policy during bulk import.
- **20. Relevant Evidence & Research Confidence:** HIGH — Discarded `detected_model_ids` in RC9 is [SOURCE_VERIFIED] on ac9ae87a; `<60s` multi-model onboarding speedup is `[HYPOTHESIS]` grounded in workflow step reduction from 2N forms to 1 batch action.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Public catalogs cannot prove which models or regional inference profiles a specific cloud credential is actually entitled to invoke; fixing RC9's discarded `detected_model_ids` collapses multi-model setup from dozens of forms into a single click. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-020 — 4-Layer Catalog Precedence, Sticky Enterprise Rate Overrides & Price Provenance

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-020` · `CAP` Traceability: `CAP-019` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-03` (Model Intelligence) -> **Enterprise Contract Overrides & Provenance**
- **3. Plain-Language Product Summary:** Ensures that custom negotiated enterprise prices and admin capability overrides always win over automatic catalog updates. Every model price and capability carries a clear provenance badge showing where it came from, and catalog updates notify finance teams of vendor list-price changes without overwriting custom rates or altering past financial records.
  - *Practical Example:* A global bank enters a negotiated 18% AWS EDP discount on Claude Sonnet deployments (`MANUAL_OVERRIDE` with contract reference `EA-2026-AWS-04`). Two months later, importing a new Model Intelligence Pack updates context-window metadata automatically while keeping the bank's custom EDP rate locked and surfacing a non-destructive `List Price Changed` advisory for FinOps review.
  - *Explicit Trade-Offs:* Tracking field-level provenance (`SIGNED_PACK`, `CATALOG_FEED`, `PROVIDER_API`, `MANUAL_OVERRIDE`) adds lightweight JSON metadata to model/deployment records, which pays for itself by eliminating silent price clobbering during catalog syncs.
- **4. User Problem & Business Value:** Directly fulfills Founder Requirement 6.A: public list prices rarely equal enterprise contract rates, and naive catalog syncs in competing tools overwrite custom negotiated pricing or retroactively distort spend reports.
- **5. Target Personas:** `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-06 (CISO, DPO & Regulatory Compliance Auditor)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION [SOURCE_VERIFIED]: `usage_settlements.applied_pricing_snapshot_json` rightly freezes historical settled rows, but `logical_models` and `model_deployments` lack catalog provenance metadata, sticky field-level override locks, contract reference notes, or list-price drift advisories.
- **7. Proposed Complete Functional Behavior:** Enforce a deterministic 4-layer catalog resolution hierarchy (`Layer 4 MANUAL_OVERRIDE > Layer 3 PROVIDER_API_DISCOVERY > Layer 2 SANITIZED_CATALOG_FEED > Layer 1 SIGNED_OFFLINE_PACK`) [DESIGN_PROPOSAL]. Record field-level provenance + actor + contract memo on every override, allow 1-click 'Reset to Catalog Default' when desired, emit non-destructive `drift_advisory` notices when underlying Layer 1/2 list prices move, and guarantee forward-only deployment pricing versions (`rate_card_version`) so historical `usage_settlements` remain 100% immutable.
- **8. User-Facing Subfeatures:**
  - Deterministic 4-layer merge engine (`Manual Override > Provider API > Catalog Feed > Signed Offline Pack`).
  - Sticky manual rate and capability overrides with optional enterprise contract ID / discount bps memo.
  - Field-level Provenance Badges and non-destructive `Upstream List Price Moved` drift advisories in ModelsPage.
  - 1-Click 'Reset Field to Catalog Reference' action with before/after diff preview and JCS audit record.
- **9. End-to-End Lifecycle:** Deployment inherits Layer 1/2 reference pricing -> FinOps/Admin applies custom negotiated override (locked at Layer 4, incrementing `rate_card_version`) -> subsequent catalog pack imports update non-overridden metadata and raise drift advisory if list price shifted -> admin acknowledges advisory or updates override.
- **10. Primary & Alternative User Journeys:** Primary: Enterprise applies a blanket 15% discount or custom token rate on an Azure connection's deployments; future catalog imports leave custom rates untouched. Alternative: Self-hosted user relies purely on Layer 1 defaults and automatically receives updated vendor list-price reductions upon upgrading Tanpopo.
- **11. Administrative & Permission Boundaries:** Setting or clearing `MANUAL_OVERRIDE` pricing requires `ORG_OWNER`, `OPERATOR`, or `FINOPS_ADMIN` permissions with `If-Match`; `FINOPS_VIEWER` can inspect provenance and drift advisories.
- **12. Automation Opportunities:** Supports connection-level negotiated discount percentage profiles (e.g., `-12% AWS EDP` applied automatically when onboarding new models onto that connection).
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Forward-only immutable rate-card version history, RFC 8785 JCS audit trail on every override mutation, and strict tenant isolation so Org A's negotiated EA rates are never visible to Org B.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Field-level merge precedence, sticky override locks, drift advisories, and immutable version snapshots are core Control Plane catalog governance rules.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Provenance badges, Sticky Override lock icons, and Drift Advisory banner in ModelsPage & FinOpsPage.
  - *CLI:* `tanpopo deployment inspect-provenance <id>` and `--override-reason` / `--reset-to-catalog` flags.
  - *Admin / Data-Plane API:* Provenance metadata, `override_locked_fields`, and `drift_advisory` on `/admin/v1/deployments`.
  - *GitOps / Terraform / Packs:* Explicit `pricing_override` blocks in declarative deployment YAML preserved across catalog upgrades.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-017 (RateCard), FEAT-018 (Signed Packs), and FEAT-019 (Account Discovery).`, `Feeds versioned rate snapshots into FEAT-037 and FEAT-042 (signed chargeback receipts).` · Related Decisions: `DEC-CAND-08`
- **17. Failure & Recovery Behavior:** Catalog sync bugs can never mutate `MANUAL_OVERRIDE` rows; operators can roll back any manual rate edit to a prior `rate_card_version` (creating a new forward version).
- **18. Security, Privacy & Financial Implications:** Treats negotiated enterprise pricing sheets as confidential tenant-scoped data under PostgreSQL `FORCE RLS` and guarantees audit-grade financial immutability.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Importing a newer Model Intelligence Pack never overwrites any field marked `MANUAL_OVERRIDE` on existing deployments.
  - When a catalog pack changes the reference list price of an overridden model, a non-destructive `drift_advisory` surfaces in Console and API.
  - Historical `usage_settlements` retain the exact `rate_card_version` and `applied_pricing_snapshot_json` active at request time.
- **20. Relevant Evidence & Research Confidence:** HIGH — Frozen `applied_pricing_snapshot_json` is [SOURCE_VERIFIED] in RC9 `db/migrations`; 4-layer precedence and sticky overrides are standard enterprise catalog design [DESIGN_PROPOSAL].
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Fortune 500 enterprises operate under negotiated AWS EDP, Azure EA, GCP commitment, or OpenAI Enterprise custom rate sheets; public catalog updates must never clobber negotiated prices or rewrite past settlements. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-021 — Complete Logical Model & Deployment Lifecycle (Edit, Versioned Rates, Deprecate & Retire)

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-021` · `CAP` Traceability: `CAP-020` · Catalog Class: `CAT-ADMIN` (`EXPANDED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-03` (Model Intelligence) -> **Logical Model & Deployment Lifecycle**
- **3. Plain-Language Product Summary:** Fixes RC9's 'create-only' model trap by giving administrators full lifecycle control over Logical Models and Model Deployments in both the Web Console and API. Operators can edit model metadata, update deployment pricing and capability tiers in place, and safely transition obsolete models through deprecation, active stream draining, disabling, retirement, and dependency-checked deletion.
  - *Practical Example:* When Anthropic releases an updated model snapshot and lowers cached token prices, an admin edits the Deployment's upstream model ID and USD/1M RateCard directly in `ModelsPage`, then marks an older `claude-3` Logical Model as `Deprecated -> Draining -> Retired` after verifying via `409` dependency checks that all production routes have migrated.
  - *Explicit Trade-Offs:* Hard-deleting a Model Deployment that previously settled FinOps transactions would orphan historical cost attribution if foreign keys were severed; therefore, `Retire` / archived tombstones preserve read-only historical lineage for settled ledgers while removing retired entries from active routing and pickers.
- **4. User Problem & Business Value:** In RC9 (`PG-004`), any typo in a Logical Model name, capability tier, or Deployment token price forced operators into disruptive workarounds because `/admin/v1/models` had zero `GET/{id}`, `PATCH`, or `DELETE` endpoints and `ModelsPage.tsx` only toggled deployment state.
- **5. Target Personas:** `PER-01 (Solo Builder / First-Time Self-Hoster)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-04 (Business Unit / Line-of-Business Delegated Admin)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION [SOURCE_VERIFIED, RUNTIME_VERIFIED]: `/admin/v1/models` supports `POST` and list `GET` only (`0` `GET/{id}`, `PATCH`, or `DELETE`/retire — `PG-004`); `/admin/v1/deployments/{id}` lacks Console rate-card, upstream-model-id, and capability-assurance editing in `ModelsPage.tsx`, and neither entity exposes explicit `DEPRECATED -> DRAINING -> DISABLED -> RETIRED` lifecycle workflows.
- **7. Proposed Complete Functional Behavior:** Deliver full lifecycle parity (`Create -> Active -> Update/Rotate -> Deprecate -> Drain -> Disable -> Retire -> Delete`) on both `LogicalModel` (`/admin/v1/models/{id}`) and `ModelDeployment` (`/admin/v1/deployments/{id}`) across API, Console, CLI, and GitOps [DESIGN_PROPOSAL]: (1) full `GET/{id}` and `PATCH` editing of display names, modality/capability tiers (`DECLARED` vs `VERIFIED`), upstream model IDs, context windows, timeouts, and versioned `RateCard`s; (2) `DEPRECATED` state emitting caller response headers (`Deprecation`, `Sunset`) and Console warnings while blocking new route attachments; (3) `DRAINING` state halting new route selections while letting open SSE streams finish; (4) `DISABLED` and permanent `RETIRED` states; and (5) `DELETE` guarded by `HTTP 409` active-route/deployment dependency checks.
- **8. User-Facing Subfeatures:**
  - Complete `LogicalModel` Detail, Edit (`PATCH`), Deprecate, Retire, and `409`-guarded Delete in API, Console, and CLI (closing `PG-004`).
  - Complete `ModelDeployment` Edit drawer in `ModelsPage.tsx` covering upstream model ID, capabilities, timeouts, and versioned `USD/1M` RateCard.
  - Standardized `Create -> Active -> Update -> Deprecate -> Drain -> Disable -> Retire -> Delete` state machine with blast-radius preview.
  - HTTP `Deprecation` and `Sunset` response headers emitted automatically on Data Plane calls hitting `DEPRECATED` models.
- **9. End-to-End Lifecycle:** `Create` (manual, discovered, or catalog-backed) -> `Active` -> `Update` (`If-Match` `PATCH`; pricing edits mint forward-only `rate_card_version` N+1) -> `Deprecate` (emits `Sunset` headers, blocks new routes) -> `Drain` (excludes from new route resolution while in-flight streams finish) -> `Disable` (reversible traffic halt) -> `Retire` (read-only archival preserving FinOps history) -> `Delete` (`409` if referenced by routes/deployments; clean removal of unused typoed records, tombstone archive if settlements exist).
- **10. Primary & Alternative User Journeys:** Primary: Admin fixes a typoed token rate on a live deployment in `ModelsPage.tsx`; `rate_card_version` increments atomically, new calls use the updated rate, and prior settled rows stay unchanged. Alternative: Admin clicks `Delete` on a Logical Model bound to `route-prod-chat`; UI/API returns `409 model_in_use` showing `route-prod-chat` and offers guided migration via FEAT-022.
- **11. Administrative & Permission Boundaries:** Mutations require `ActionModelWrite` / `ActionDeploymentWrite` + `If-Match` ETag; production rate-card or retirement mutations optionally require Four-Eyes approval (FEAT-068) under enterprise governance policies.
- **12. Automation Opportunities:** Optional auto-transition from `DEPRECATED` to `DRAINING -> DISABLED` when a configured `sunset_at_ms` timestamp is reached AND fallback targets are verified healthy.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Optimistic concurrency (`If-Match`), JCS before/after audit records recording old vs new integer rates, and zero foreign-key corruption on `usage_settlements` or `request_traces_meta`.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Core Control Plane CRUD/lifecycle endpoints, dependency graph enforcement, ETag concurrency, and Astryx Console management views.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Full Edit, Rate-Card Version History, and Lifecycle Action Drawers on Logical Models & Deployments in `ModelsPage.tsx`.
  - *CLI:* `tanpopo model get | update | deprecate | retire | delete` and `tanpopo deployment get | update | drain | retire | delete`.
  - *Admin / Data-Plane API:* GET/PATCH/DELETE `/admin/v1/models/{id}` and full-field PATCH/DELETE `/admin/v1/deployments/{id}`.
  - *GitOps / Terraform / Packs:* Declarative in-place reconciliation of model metadata, versioned rate cards, and lifecycle states.
- **16. Dependencies & Cross-Domain Relationships:** `Closes `PG-004` (`FND-017`) and pairs with FEAT-017 (RateCard), FEAT-022 (Migration Advisor), and FEAT-120 (unified lifecycle parity).` · Related Decisions: `DEC-CAND-21`
- **17. Failure & Recovery Behavior:** Stale `If-Match` edits return `HTTP 412`; accidental `Disable` is 1-click reversible to `Active`.
- **18. Security, Privacy & Financial Implications:** Every pricing or capability tier change writes a tamper-evident JCS audit entry so internal auditors can trace every historical rate-card version to an authenticated actor.
- **19. Preliminary Measurable Acceptance Criteria:**
  - `GET`, `PATCH`, `Deprecate`, `Retire`, and `409`-guarded `DELETE` work end-to-end on Logical Models across API, Console, CLI, and GitOps.
  - `ModelsPage.tsx` edits deployment `upstream_model`, capabilities, timeouts, and `RateCard` fields in place without recreating deployments.
  - Deleting a Logical Model or Deployment referenced by any RoutePolicy returns `HTTP 409` listing every dependent route.
- **20. Relevant Evidence & Research Confidence:** HIGH — `PG-004` and `ModelsPage.tsx` read-only rate-card gaps are [SOURCE_VERIFIED] and [RUNTIME_VERIFIED] in TEEP-01-DOC-04 and QA-005.
- **21. Proposal Classification:** `IMPROVE` (`EXPANDED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Directly resolves RC9's severe create-only trap (`PG-004`) where Logical Models cannot be edited or retired at all and Console `ModelsPage.tsx` cannot edit deployment pricing or upstream model identifiers. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-022 — Automated Model Deprecation Monitoring & Capability-Verified Migration Advisor

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-022` · `CAP` Traceability: `CAP-021` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: `INN-02`
- **2. Functional Domain & Subdomain:** `DOM-03` (Model Intelligence) -> **Proactive Fleet Lifecycle & Migration Intelligence**
- **3. Plain-Language Product Summary:** Watches every deployed AI model for upcoming vendor retirement dates and prepares a verified migration plan before outages strike. It checks real historical traffic metadata (without storing prompts) to confirm whether a recommended replacement model supports every feature and context length your apps actually use, calculates the exact 30-day budget impact, optionally runs a 1% shadow test, and generates a 1-click route update.
  - *Practical Example:* Sixty days before an older Gemini or OpenAI checkpoint sunsets, Tanpopo alerts the platform team, scans the last 30 days of `request_traces_meta` on 4 affected routes, confirms the successor model covers 100% of observed tool-calling, JSON-schema, and 148k-token peak context usage while lowering projected monthly cost by USD 420, and prepares a ready-to-approve `If-Match` route cutover patch.
  - *Explicit Trade-Offs:* Metadata-only replay proves structural capability parity, context-window fit, residency compliance, and exact token-cost delta with zero prompt storage, but cannot by itself detect subtle behavioral/tone shifts in model prose; therefore, the workflow surfaces this boundary transparently and integrates optional deterministic eval cassettes (FEAT-091) and budget-capped 1% shadow canarying (FEAT-092).
- **4. User Problem & Business Value:** Replaces chaotic spreadsheet hunts across dozens of microservices whenever OpenAI, Anthropic, Google, or Bedrock announces a model sunset, turning quarterly vendor churn into a calm, auditable 5-minute workflow `[HYPOTHESIS]`.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED]: zero `deprecation_date` / `sunset_at_ms` tracking, zero sunset alerting, zero exercised-capability scan over `request_traces_meta`, and zero guided route migration workflow.
- **7. Proposed Complete Functional Behavior:** Implement the `INN-02` Deprecation & Migration Advisor workflow [DESIGN_PROPOSAL]: (1) ingest `deprecation_date`, `sunset_at_ms`, and `recommended_replacements[]` from Signed Model Intelligence Packs (FEAT-018) and manual annotations; (2) emit `T-90d`, `T-30d`, and `T-7d` notifications (DOM-19) listing affected deployments, routes, and consuming Virtual Keys; (3) execute a zero-prompt compatibility audit against `request_traces_meta` (verifying `100%` of actually exercised modalities, tool calls, JSON schemas, reasoning modes, `p100` input/output token peaks, and regional residency against candidate replacements); (4) compute historical 30-day micro-USD spend delta via the What-If Engine (FEAT-139); (5) optionally launch a budget-capped `1%` non-mutating shadow canary (FEAT-027/FEAT-092); and (6) apply or stage a Four-Eyes `If-Match` route patch.
- **8. User-Facing Subfeatures:**
  - Proactive Sunset Radar in ModelsPage & Notification Center (`T-90d / 30d / 7d / Past-EOL` badges + affected route count).
  - Zero-prompt Capability & Context-Window Parity Matrix comparing exercised production traffic against candidate successor models.
  - Exact 30-day historical micro-USD cost impact projection powered by RateCard + What-If Replay.
  - Guided 1-Click Canary (e.g., 5% split or 1% shadow) -> Full Cutover (`If-Match` route patch or GitOps PR export).
- **9. End-to-End Lifecycle:** Catalog pack or admin flags `sunset_at_ms` -> Sunset Radar alerts route owners -> Advisor computes exercised-capability fit + 30d cost delta across candidate replacements -> Optional shadow/canary validation -> Atomic route target cutover -> Old deployment transitions to `Draining -> Retired` (FEAT-021).
- **10. Primary & Alternative User Journeys:** Primary: Candidate replacement passes 100% capability, residency, and context checks and reduces spend; owner applies cutover in 1 click. Alternative: Candidate replacement has a 128k context limit while `0.4%` of historical requests on the route used 160k tokens; Advisor flags a blocking `CONTEXT_WINDOW_REGRESSION` warning and recommends a 200k/1M-context alternative instead.
- **11. Administrative & Permission Boundaries:** Read-only advisories visible to `DEVELOPER`, `FINOPS_VIEWER`, and `TEAM_ADMIN`; executing canary/cutover patches requires `ActionRouteWrite` (+ Four-Eyes approval when enabled) and respects tenant RLS boundaries.
- **12. Automation Opportunities:** Scheduled daily deprecation sweep emitting structured events (`model.deprecation.upcoming`) to In-App Inbox, Email, Slack, Jira, or Webhook sinks.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Operates strictly on zero-prompt `request_traces_meta` vectors (safe for HIPAA/banking zero-content enclaves), enforces hard shadow-canary budget caps, and exports GitOps YAML diffs for Terraform/Git-managed routes.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Combines Control Plane catalog metadata, `request_traces_meta` historical capability/token queries, What-If cost replay (FEAT-139), and atomic `If-Match` route patching.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Sunset Radar banner & Interactive Migration Advisor Wizard in ModelsPage and RoutingPage.
  - *CLI:* `tanpopo model deprecations` and `tanpopo route advise-migration <route-id> --to <deployment-id>`.
  - *Admin / Data-Plane API:* GET `/admin/v1/models/deprecations` and POST `/admin/v1/routes/{id}:advise-migration`.
  - *GitOps / Terraform / Packs:* Generates declarative route-target migration patches for `tanpopo config plan` / PR workflows.
- **16. Dependencies & Cross-Domain Relationships:** `Realizes `INN-02` by combining FEAT-018 (Model Intelligence Packs), FEAT-021 (model retirement), FEAT-027 (canary/shadow routing), and FEAT-139 (`INN-01` What-If Engine).` · Related Decisions: `DEC-CAND-02`, `DEC-CAND-10`
- **17. Failure & Recovery Behavior:** If historical trace retention is empty on a newly created route, falls back cleanly to static route/model declared capability comparison and labels cost delta as `insufficient_traffic_history`.
- **18. Security, Privacy & Financial Implications:** Requires zero raw prompt persistence, prevents surprise 2x–5x budget shocks when upgrading model generations, and caps optional shadow canary spend via dedicated ledger bounds.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Models within 90/30/7 days of `sunset_at_ms` trigger actionable notifications listing all dependent routes and projects.
  - Migration Advisor accurately flags missing exercised capabilities (e.g., `json_schema` or `vision`) and `p100` token-window overflows from `request_traces_meta`.
  - Advisor outputs a deterministic 30-day micro-USD cost delta and valid `If-Match` route cutover payload.
- **20. Relevant Evidence & Research Confidence:** HIGH — Empirical need (`1,284` deprecation entries in LiteLLM catalog snapshot, SRC-045) and feasibility atop existing `request_traces_meta` + `pricing.Calculate` are [RUNTIME_VERIFIED] / [SOURCE_VERIFIED]; `5-minute` workflow duration target is `[HYPOTHESIS]`.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Cloud AI vendors retire model checkpoints every 3–6 months (877 upcoming and 407 past-due deprecations observed in the 2026-11-27 LiteLLM catalog); automating impact verification prevents emergency outages. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-023 — Model Weights License & Jurisdictional Compliance Gate

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-023` · `CAP` Traceability: `CAP-022` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-03` (Model Intelligence) -> **Model Licensing & Legal Governance**
- **3. Plain-Language Product Summary:** Tracks the legal license of every AI model—such as Apache-2.0 permissive weights versus community licenses that restrict commercial use, competitor distillation, or European Union organizations—and automatically blocks disallowed models based on your organization's legal domicile and compliance policy.
  - *Practical Example:* A Frankfurt-headquartered insurer sets its organization domicile to `EU` and policy to `ALLOW_OSI_AND_APPROVED_COMMERCIAL_ONLY`; when an engineer attempts to deploy multimodal Meta Llama 4 weights (whose Community License prohibits EU-domiciled entities [SOURCE_VERIFIED, SRC-028]) on an internal vLLM cluster, Tanpopo blocks deployment/routing automatically with a clear `model_license_jurisdiction_restricted` notice.
  - *Explicit Trade-Offs:* Automated license metadata is a technical safeguard rather than legal advice, and custom enterprise bilateral licenses can override public community terms; therefore, `ORG_OWNER` / `SECURITY_REVIEWER` can record explicit legal counsel waivers (`license_waiver_ref`) per model with immutable JCS audit logging.
- **4. User Problem & Business Value:** Engineering teams frequently assume downloadable HuggingFace/Ollama weights are universally permissive, unaware of geographic bans (Llama 4 EU restriction), non-commercial clauses (CC-BY-NC / research weights), or output-distillation prohibitions.
- **5. Target Personas:** `PER-06 (CISO, DPO & Regulatory Compliance Auditor)`, `PER-03 (Enterprise AI Platform Architect / SRE)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED]: neither `logical_models` nor `EffectivePolicy` tracks SPDX identifiers, license classes, commercial-use permissions, or restricted legal jurisdictions.
- **7. Proposed Complete Functional Behavior:** Populate `spdx_id`, `license_class` (`OSI_PERMISSIVE`, `SOURCE_AVAILABLE_COMMERCIAL`, `COMMUNITY_RESTRICTED`, `NON_COMMERCIAL_RESEARCH`, `PROPRIETARY_API`), `restricted_domiciles[]`, and `prohibits_output_training` in Signed Model Intelligence Packs (FEAT-018) and custom model forms [DESIGN_PROPOSAL]. Allow org/BU policies to declare `org_domicile_jurisdictions` and allowed `license_classes`, failing closed at both deployment creation and Route Resolution Step 2 unless an audited legal waiver exists.
- **8. User-Facing Subfeatures:**
  - SPDX identifier, license classification, and jurisdictional restriction metadata on catalog and custom models.
  - Organization domicile & Acceptable Model License Policy settings (e.g., block `NON_COMMERCIAL_RESEARCH` in `prod` environments).
  - Fail-closed deployment and runtime route-resolution enforcement when org domicile intersects `restricted_domiciles`.
  - Audited Legal Counsel Waiver workflow (`license_waiver_ref` + expiration date) for enterprises holding direct vendor licenses.
- **9. End-to-End Lifecycle:** Catalog pack supplies model license metadata -> Org configures domicile + allowed license classes -> Deployment onboarding & Route Explain validate compatibility -> Runtime Resolver Step 2 prunes non-compliant targets -> Included in Compliance Evidence Bundles (FEAT-072).
- **10. Primary & Alternative User Journeys:** Primary: Research environment (`env=sandbox`) permits `NON_COMMERCIAL_RESEARCH` weights for experimentation, while `env=prod` strictly blocks them at promotion time. Alternative: Enterprise negotiates a direct commercial license with a model vendor; `SECURITY_REVIEWER` attaches `license_waiver_ref` to unblock `prod`.
- **11. Administrative & Permission Boundaries:** License policies and legal waivers are restricted to `ORG_OWNER` and `SECURITY_REVIEWER` (never editable by `DEVELOPER` or project-level keys).
- **12. Automation Opportunities:** Automatically infers license metadata during Ollama/vLLM model discovery (FEAT-019) by matching model family/template digests against the signed catalog.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Zero runtime latency penalty (compiled into `ConfigSnapshot` bitmasks), JCS audit logging of all waivers, and exportable model-license inventory for ISO/IEC 42001 and M&A due-diligence audits.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — License metadata lives in the Control Plane catalog and compiles into `ConfigSnapshot` so Route Resolution Step 2 enforces jurisdictional/license compliance in sub-microsecond RAM checks.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* License & Jurisdiction badges in ModelsPage and Organization Legal/License Policy panel in Settings.
  - *CLI:* `tanpopo catalog inspect <model>` license section and `tanpopo policy set-license-rules`.
  - *Admin / Data-Plane API:* License metadata on `/admin/v1/models` and license-gate fields on `/admin/v1/policies`.
  - *GitOps / Terraform / Packs:* Declarative `allowed_license_classes`, `org_domicile`, and `license_waivers` in policy YAML.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-018 (Model Intelligence Packs) and integrates with FEAT-063 (Policy-as-Code) and FEAT-073 (`DEC-004` commercial ToS gate).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Custom models with `UNKNOWN` license class fail closed in `prod` only when strict license governance is enabled by the organization (defaulting to non-blocking warning for Day-1 self-hosters).
- **18. Security, Privacy & Financial Implications:** Reduces corporate IP/copyright exposure and prevents training/distillation pipelines from inadvertently consuming outputs from models whose terms prohibit synthetic dataset generation.
- **19. Preliminary Measurable Acceptance Criteria:**
  - An organization configured with `org_domicile=EU` automatically blocks Llama 4 Community-licensed deployments unless an explicit audited waiver is attached.
  - `NON_COMMERCIAL_RESEARCH` models can be permitted in `sandbox` environments while hard-blocked in `prod`.
  - Route Explain clearly reports `PRUNED_MODEL_LICENSE_POLICY` when a target fails license/jurisdiction checks.
- **20. Relevant Evidence & Research Confidence:** HIGH — Meta Llama 4 Community License EU domicile restriction is [SOURCE_VERIFIED] in SRC-028; policy-gate enforcement in `ConfigSnapshot` is a deterministic [DESIGN_PROPOSAL].
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — Protects European and regulated enterprises from subtle legal/IP liability when deploying open-weight or community-licensed models with domicile, commercial-use, or monthly-active-user restrictions. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `v1.1 (Core)`)*

---

## `DOM-21` — Async, Batch, Realtime & Multimodal Media Workloads (`5` Proposed Features)

- **Domain Overview:** Async, Batch, Realtime & Multimodal Media Workloads extends Tanpopo beyond synchronous HTTP text/JSON request-response inference so enterprises can govern offline 24h-48h batch queues, multipart document/media uploads, low-latency bidirectional voice sessions, synthetic image/video/audio endpoints, and per-page OCR under one Virtual Key, residency fence, guardrail pipeline, and FinOps budget ledger.
- **Existing RC9 Baseline Posture:** Substantially ABSENT_IN_RC9 on the ingress wire surface despite partial internal groundwork: RateCard structs contain preliminary image/audio fields and host blob.Store handles inline <= 32 MiB attachments [SOURCE_VERIFIED], but /v1/batches, /v1/files, /v1/images/*, /v1/audio/*, and /v1/ocr are absent from openapi/dataplane-v1.yaml [SOURCE_VERIFIED], server.go:687-732 rejects WebSocket upgrades with HTTP 400 unsupported_protocol_upgrade [SOURCE_VERIFIED], the 90s SM-07 orphan hold reaper would prematurely reclaim multi-hour batch holds [SOURCE_VERIFIED], and sse/pump.go:99 splits strictly on 0x0A newlines rather than binary AWS EventStream frames [SOURCE_VERIFIED].
- **Progressive Self-Hosted to Enterprise Experience:** Solo self-hosters gain immediate value on Day-1 by pointing Whisper transcription, TTS voice tools, or PDF OCR scripts at the same single-binary gateway without extra workers or external queues; as organizations scale, platform and FinOps teams unlock 30%-50% batch token discounts via durable BATCH_ESCROW accounting, DLP-scanned connection-pinned file handles, and constrained ephemeral WebRTC/WebSocket voice session tokens.
- **Technical Selections Deferred to `TEEP-03`:** Transport architecture between constrained ephemeral realtime token brokering vs full governed WebSocket bidirectional proxying (DEC-CAND-07), resumable upload chunk spooling vs pass-through streaming backpressure, and sub-micro-USD multimodal/audio unit carry math (DEC-CAND-08) are deferred to TEEP-03.

| ID | Subdomain | Feature Name | Class | Disposition | Priority | Placement Candidate |
|---|---|---|---|---|---|---|
| `FEAT-125` | Asynchronous Batch & Background Inference | **Asynchronous Batch Inference (/v1/batches, /v1/messages/batches), Background Responses & BATCH_ESCROW Accounting** | `CAT-PROD` | `ADD` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |
| `FEAT-126` | Governed Multimodal File & Upload Broker | **Tenant-Isolated, DLP-Scanned /v1/files & Resumable Upload Broker** | `CAT-PROD` | `ADD` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |
| `FEAT-127` | Realtime Voice & Bidirectional Streaming | **Realtime Bidirectional Voice/Audio Gateway (/v1/realtime, BidiGenerateContent) & Constrained Ephemeral Session Tokens** | `CAT-PROD` | `ADD` | `P2-Differentiator` | `CORE_DATA_PLANE` |
| `FEAT-128` | Multimodal Media Generation & Speech Endpoints | **Unified & Native Image, Video, Speech Synthesis & Audio Transcription Endpoints with Multimodal Metering** | `CAT-PROD` | `ADD` | `P2-Differentiator` | `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` |
| `FEAT-129` | Document OCR & Layout Extraction | **Per-Page Metered Document OCR & Structured Layout Extraction Plane (/v1/ocr)** | `CAT-PROD` | `ADD` | `P2-Differentiator` | `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` |

### FEAT-125 — Asynchronous Batch Inference (/v1/batches, /v1/messages/batches), Background Responses & BATCH_ESCROW Accounting

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-125` · `CAP` Traceability: `CAP-137` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-21` (Async, Batch, Realtime & Multimodal Media Workloads) -> **Asynchronous Batch & Background Inference**
- **3. Plain-Language Product Summary:** Lets applications submit large non-urgent AI job batches or multi-hour background reasoning tasks through Tanpopo to capture 30%-50% vendor price discounts while keeping budgets, tenant isolation, and usage settlement strictly enforced.
  - *Practical Example:* A compliance engineering team uploads 50,000 contract clauses via /v1/files and submits POST /v1/batches with a 24h completion window; Tanpopo places a durable BATCH_ESCROW budget hold at the 50%-discounted batch rate, pins polling/cancellation to the originating connection, and settles exact micro-USD spend per line item when the batch completes.
  - *Explicit Trade-Offs:* Long-lived BATCH_ESCROW holds tie up tenant budget headroom for up to 24h-48h until upstream completion or cancellation, requiring separate escrow visibility in FinOps dashboards so operators do not confuse pending batch holds with leaked synchronous locks.
- **4. User Problem & Business Value:** High-volume offline AI pipelines overpay by up to 2x if forced onto synchronous endpoints or create shadow-IT credential sprawl if sent directly to OpenAI, Anthropic, or Gemini batch endpoints outside gateway governance.
- **5. Target Personas:** `PER-02`, `PER-07`, `PER-10`
- **6. Existing RC9 Behavior & Limitations:** ABSENT_IN_RC9: /v1/batches and /v1/messages/batches do not exist in openapi/dataplane-v1.yaml, responses.go ignores background:true [SOURCE_VERIFIED], and the 90s SM-07 orphan hold reaper in the synchronous ledger would prematurely reclaim multi-hour holds [SOURCE_VERIFIED].
- **7. Proposed Complete Functional Behavior:** Expose unified and native batch/background endpoints (/v1/batches, /v1/messages/batches, Gemini batch mappings, and /v1/responses with background:true). On submission, compute worst-case discounted batch cost from RateCard batch service-tier fields, record a durable BATCH_ESCROW lease (24h-48h TTL horizon with heartbeat renewal via background status poller), bind gateway batch IDs (tpb_*) to (org_id, project_id, connection_id, upstream_batch_id) under tenant RLS, and reconcile exact usage plus unused escrow release upon terminal state or cancellation.
- **8. User-Facing Subfeatures:**
  - Unified /v1/batches and native /v1/messages/batches submission, status polling, cancellation, and result retrieval
  - Durable BATCH_ESCROW budget state machine with dedicated pending-batch headroom breakdown in Console FinOps views
  - Background /v1/responses (background:true) job tracking with webhook completion notifications (batch.completed, batch.failed)
  - Console & CLI Batch Job Inspector listing active, completed, expired, and cancelled jobs per project
- **9. End-to-End Lifecycle:** 1. Caller uploads input JSONL (/v1/files) or submits inline batch items -> 2. Gateway validates Virtual Key, residency, and DLP policies and acquires BATCH_ESCROW hold -> 3. BlindBroker dispatches upstream batch and persists RLS-scoped tpb_* mapping -> 4. Background reconciler or caller poll tracks progress and extends lease -> 5. Terminal completion settles exact discounted token usage, releases excess escrow, and emits outbound webhook.
- **10. Primary & Alternative User Journeys:** Primary: CI/ETL worker submits OpenAI /v1/batches job and receives webhook callback on completion. Alternative A: Anthropic SDK caller uses /v1/messages/batches without input file upload. Alternative B: Operator cancels a runaway batch from Console, triggering upstream cancel and partial settlement.
- **11. Administrative & Permission Boundaries:** Data-plane callers require Virtual Key or WIF token with PROVIDER plane scope and batch model entitlement; Tenant A cannot read, poll, or cancel Tenant B's tpb_* handle (HTTP 404/403 under FORCE RLS); ORG_OWNER and TEAM_ADMIN can cancel in-flight project batches.
- **12. Automation Opportunities:** Automatic off-peak batch submission scheduling, webhook-driven downstream ETL triggers on batch.completed, and automatic orphan upstream batch cancellation when a Virtual Key or Project is suspended.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Dual-engine SQLite and PostgreSQL 16 FORCE RLS persistence for batch_jobs and BATCH_ESCROW ledgers, applied_pricing_snapshot_json immutability at submission/settlement, and JCS audit events on manual cancellations.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Requires tight integration with Core Virtual Key authentication, connection-pinned routing, and durable long-horizon FinOps budget escrow exempt from the 90s synchronous stream reaper.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Batch Jobs tab under Observability/FinOps showing active BATCH_ESCROW holds, progress bars, and Cancel action.
  - *CLI:* 'tanpopo batch list | inspect | cancel' for operational triage and CI pipeline waiting.
  - *Admin / Data-Plane API:* Data Plane /v1/batches, /v1/messages/batches, /v1/responses (background:true); Admin GET /admin/v1/batches.
  - *GitOps / Terraform / Packs:* Policy and RateCard batch_discount / max_batch_escrow_micros declarative fields.
- **16. Dependencies & Cross-Domain Relationships:** `FEAT-017 (High-Precision Multimodal & Tiered Rate-Card Modeling for batch service-tier pricing)`, `FEAT-037 (Two-Phase Budget Reservation & Settlement extended with BATCH_ESCROW lease class)`, `FEAT-114 (Outbound Event Webhooks for batch completion notifications)`, `FEAT-126 (Tenant-Isolated /v1/files Broker for JSONL batch inputs and outputs)` · Related Decisions: `DEC-CAND-02`, `DEC-CAND-08`
- **17. Failure & Recovery Behavior:** If gateway pods restart mid-batch, durable SQL batch_jobs rows survive and resume polling on leader/worker election; if upstream batch expires or fails partially, settled rows bill only completed items and immediately release remaining BATCH_ESCROW micro-USD back to the tenant budget.
- **18. Security, Privacy & Financial Implications:** Prevents cross-tenant batch ID enumeration via opaque tpb_* IDs and RLS; enforces pre-dispatch DLP scanning on batch inputs; eliminates unbilled shadow batch spend while preventing 90s reaper double-spend races.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Submitting /v1/batches or /v1/messages/batches creates a BATCH_ESCROW hold that is not reclaimed by the 90s synchronous orphan reaper over a simulated >2h window.
  - Cross-tenant GET or POST .../cancel on another organization's tpb_* ID fails closed under both SQLite and PostgreSQL FORCE RLS.
  - Partial batch failure settles exact micro-USD solely for succeeded items using batch-tier RateCard rates and releases unused escrow within one reconciliation cycle.
- **20. Relevant Evidence & Research Confidence:** [SOURCE_VERIFIED] RC9 lacks batch routes and uses a 90s SM-07 hold reaper; [VERIFIED_FACT] OpenAI, Anthropic, and Gemini publish 30%-50% async batch/background tiers (SRC-048, SRC-051, SRC-055).
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Enterprise document classification, nightly evaluation, and back-office extraction workloads depend on 30%-50% vendor batch/background pricing discounts; blocking batch APIs forces teams to bypass Tanpopo with raw provider keys. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-126 — Tenant-Isolated, DLP-Scanned /v1/files & Resumable Upload Broker

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-126` · `CAP` Traceability: `CAP-138` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-21` (Async, Batch, Realtime & Multimodal Media Workloads) -> **Governed Multimodal File & Upload Broker**
- **3. Plain-Language Product Summary:** Provides a governed file upload endpoint (/v1/files) that scans uploaded PDFs, JSONL batches, images, and audio for sensitive data, isolates file handles per tenant, pins downstream AI calls to the exact provider account holding the file, and automatically deletes expired files upstream.
  - *Practical Example:* An application uploads a 45 MiB financial PDF via POST /v1/files; Tanpopo streams the upload through bounded memory chunks with PII/secret guardrails, mints handle tpf_01J9..., pins subsequent /v1/responses calls referencing tpf_01J9... to Connection conn_openai_eu_1, and purges the upstream file automatically after its configured 24h TTL.
  - *Explicit Trade-Offs:* Pinning a tpf_* handle to a single upstream connection prevents transparent cross-account failover for requests referencing that file unless optional encrypted gateway staging/re-upload is enabled at higher storage cost.
- **4. User Problem & Business Value:** Without a connection-aware file broker, load-balanced gateways randomly send provider-local file-abc123 IDs to secondary accounts (causing HTTP 404 errors) and leave uploaded enterprise documents orphaned indefinitely in third-party vendor storage.
- **5. Target Personas:** `PER-02`, `PER-06`, `PER-10`
- **6. Existing RC9 Behavior & Limitations:** ABSENT_IN_RC9: RC9 supports inline base64 attachments <= 32 MiB via host blob.Store [SOURCE_VERIFIED], but lacks /v1/files CRUD, resumable upload sessions, connection-pinned file_id routing, and upstream file TTL reaping.
- **7. Proposed Complete Functional Behavior:** Implement POST/GET/DELETE /v1/files and Gemini-compatible resumable upload brokering using bounded streaming chunk buffers (zero full-file heap buffering). Enforce configurable MIME allowlists, byte quotas, and streaming DLP/secret guardrails before upstream egress. Persist tenant-scoped file metadata mapping opaque gateway tpf_* IDs to (org_id, project_id, connection_id, upstream_file_id, purpose, expires_at_ms), automatically narrowing Route Resolution to connection_id when tpf_* is referenced and deleting upstream files on TTL expiry or tenant offboarding.
- **8. User-Facing Subfeatures:**
  - Streaming multipart /v1/files upload, metadata listing, content download (where permitted by purpose), and deletion
  - Connection-pinned tpf_* handle translation preventing cross-account HTTP 404 routing mismatches
  - Pre-egress streaming MIME validation, size quotas, and deterministic secret/PII DLP scanning
  - Automated upstream file TTL reaper and tenant offboarding purge integration
- **9. End-to-End Lifecycle:** 1. Client streams multipart upload to /v1/files -> 2. Gateway validates quota/MIME and scans chunks via DLP guardrails -> 3. BlindBroker streams upload to residency-compliant connection -> 4. Gateway records tpf_* binding with mandatory TTL -> 5. Inference/batch calls referencing tpf_* route to pinned connection -> 6. TTL cleaner or DELETE /v1/files/{id} removes upstream object and marks metadata deleted.
- **10. Primary & Alternative User Journeys:** Primary: SDK uploads PDF or batch JSONL and passes returned tpf_* ID into /v1/responses or /v1/batches. Alternative A: Gemini SDK uses resumable upload protocol for large video/audio media. Alternative B: DLP guardrail detects embedded private key in JSONL upload and aborts stream before completion.
- **11. Administrative & Permission Boundaries:** Virtual Keys require explicit file-upload capability permission; file visibility is isolated by (org_id, project_id) under FORCE RLS; ORG_OWNER and SECURITY_REVIEWER can enforce max_file_ttl_hours and block persistent upstream storage under strict ZDR policies.
- **12. Automation Opportunities:** Scheduled background sweep reconciling upstream provider file lists against local tpf_* records to purge untracked or expired cloud artifacts.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Bounded Go heap memory per concurrent upload [DESIGN_PROPOSAL], full SSRFGuardDialer enforcement, residency region matching before upload target selection, and verifiable purge on GDPR/tenant erasure.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — File handle affinity directly governs Core route resolution, streaming DLP inspection, BlindBroker credential injection, and tenant crypto-shredding.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Project Files inventory drawer showing active tpf_* handles, pinned connection, byte size, and Expiry/Purge controls.
  - *CLI:* 'tanpopo files ls | upload | rm | purge-expired' for developer testing and storage hygiene.
  - *Admin / Data-Plane API:* Data Plane POST/GET/DELETE /v1/files, /v1/files/{id}/content; Admin file quota policy fields.
  - *GitOps / Terraform / Packs:* Declarative Policy fields for allowed_file_purposes, max_upload_bytes, and default_file_ttl_seconds.
- **16. Dependencies & Cross-Domain Relationships:** `FEAT-054 (Multi-Stage Runtime Guardrail Pipeline for streaming upload inspection)`, `FEAT-055 (Built-In Deterministic PII & Secret Guardrails)`, `FEAT-051 (Configurable Telemetry & Artifact TTL Reaping)`, `FEAT-074 (Immediate-Fence Tenant Offboarding purging upstream files)` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** If client disconnects mid-upload, upstream multipart/resumable session is aborted cleanly with zero leaked handles; if upstream DELETE fails transiently during TTL reaping, retry queue backs off with dead-letter alert so unpurged external files are surfaced to operators.
- **18. Security, Privacy & Financial Implications:** Closes data-exfiltration and cross-tenant file-collision vectors; enforces strict ZDR posture by disallowing indefinite vendor file retention when require_upstream_zdr is active.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Uploading a 100 MiB test payload streams through the gateway within bounded heap overhead [HYPOTHESIS] and enforces configured DLP rules.
  - Referencing tpf_* in a multi-target route deterministically selects the owning connection_id instead of failing over to a sibling account.
  - Expired tpf_* records trigger upstream provider file deletion via BlindHostCredentialBroker and record audit confirmation.
- **20. Relevant Evidence & Research Confidence:** [SOURCE_VERIFIED] RC9 lacks /v1/files and connection-pinned file routing; [VERIFIED_FACT] OpenAI, Anthropic, and Gemini all expose provider-local file APIs (SRC-048, SRC-051, SRC-055).
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Batch jobs, PDF document analysis, audio transcription, and fine-tuning/eval workflows require uploading files larger than inline JSON limits without breaking multi-account routing or leaking sensitive documents. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-127 — Realtime Bidirectional Voice/Audio Gateway (/v1/realtime, BidiGenerateContent) & Constrained Ephemeral Session Tokens

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-127` · `CAP` Traceability: `CAP-139` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-21` (Async, Batch, Realtime & Multimodal Media Workloads) -> **Realtime Voice & Bidirectional Streaming**
- **3. Plain-Language Product Summary:** Enables low-latency two-way voice and audio AI sessions through Tanpopo using either locked-down short-lived browser tokens (for direct WebRTC audio) or a fully proxied WebSocket stream with live budget cutoffs and instant kill-switch termination.
  - *Practical Example:* A telehealth mobile app calls POST /v1/realtime/client_secrets with a project Virtual Key; Tanpopo validates BAA residency, locks system instructions and allowed tools inside a 60-second constrained upstream session token with a pre-reserved 10-minute audio budget, or proxies WSS /v1/realtime directly in VPC-locked environments while metering audio tokens every 30 seconds.
  - *Explicit Trade-Offs:* Constrained ephemeral tokens minimize media-path latency for browser WebRTC apps but bypass mid-stream gateway payload inspection after minting; full WebSocket proxying preserves inline DLP, VPC egress control, and instant mid-call kill-switch severing at the cost of long-lived gateway socket state (DEC-CAND-07).
- **4. User Problem & Business Value:** Without governed realtime support, engineering teams either embed raw OpenAI/Gemini API keys in backend token servers without FinOps caps or let browser clients mutate system prompts and tool permissions mid-session.
- **5. Target Personas:** `PER-02`, `PER-03`, `PER-07`
- **6. Existing RC9 Behavior & Limitations:** REJECTED_IN_RC9: internal/ingress/http/server.go:687-732 explicitly rejects all Connection: Upgrade / WebSocket requests with HTTP 400 unsupported_protocol_upgrade [SOURCE_VERIFIED].
- **7. Proposed Complete Functional Behavior:** Support dual enterprise realtime modes (subject to DEC-CAND-07 sequencing): (1) Constrained Ephemeral Session Token Broker (/v1/realtime/client_secrets and Gemini BidiGenerateContentConstrained) that authenticates the caller, enforces model/residency/policy rules, locks immutable system instructions, voice, tools, and max duration, and pre-reserves worst-case audio budget; and (2) Governed WebSocket BiDi Proxy (/v1/realtime and WSS BidiGenerateContent) registered in ActiveStreamRegistry with rolling 30s DeltaReserve audio-token holds, idle/max-duration deadlines, BlindBroker upstream auth, and sub-second kill-switch socket termination.
- **8. User-Facing Subfeatures:**
  - Constrained ephemeral session token minting (/v1/realtime/client_secrets, BidiGenerateContentConstrained) with locked system prompts and tool lists
  - Governed WebSocket bidirectional proxy (/v1/realtime) with BlindHostCredentialBroker credential isolation
  - Rolling 30s DeltaReserve audio input/output token metering and max-session-duration hard caps
  - ActiveStreamRegistry integration for instant WebSocket termination on key revocation, budget exhaustion, or Kill-Switch
- **9. End-to-End Lifecycle:** 1. Client requests constrained token or initiates WSS /v1/realtime upgrade -> 2. Gateway verifies Virtual Key, residency, and initial audio budget hold -> 3. In Proxy mode, gateway relays bidirectional frames while intercepting usage events for 30s DeltaReserve extensions and scrubbing upstream secrets -> 4. On session close, timeout, or kill-switch abort, gateway settles exact audio/text token spend and writes waterfall trace.
- **10. Primary & Alternative User Journeys:** Primary (VPC Enterprise): Backend/telephony bridge connects via WSS /v1/realtime for governed contact-center voice routing. Alternative (Browser/Mobile): App backend mints a 60s constrained client secret with locked guardrail instructions for direct WebRTC voice streaming.
- **11. Administrative & Permission Boundaries:** Requires explicit REALTIME capability grant on Virtual Key/Policy; policies can forbid direct ephemeral browser tokens in high-security projects so all voice traffic traverses the inspectable WebSocket proxy.
- **12. Automation Opportunities:** Automatic graceful voice session wrap-up warning frame N seconds before max_session_duration_ms or budget depletion cutoff.
- **13. Enterprise-Specific Requirements (`OPTIONAL_ECOSYSTEM_INTEGRATION`):** Connection-level concurrent WebSocket bulkheads per tenant, graceful Kubernetes preStop drain hooks honoring bounded voice session drain windows, and full audio input/output/cached token RateCard accounting.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — WebSocket connection upgrades, ActiveStreamRegistry kill-switch severing, and rolling DeltaReserve audio budget holds execute directly on the Data-Plane network edge.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Policy toggle for Realtime Proxy vs Ephemeral Token permissions; Live Streams counter in Cluster & Project views.
  - *CLI:* Smoke-test and diagnostic inspection support for WebSocket upgrade health.
  - *Admin / Data-Plane API:* POST /v1/realtime/client_secrets, WSS /v1/realtime, Gemini BidiGenerateContent equivalents.
  - *GitOps / Terraform / Packs:* Route and Policy realtime_mode, max_realtime_duration_ms, and max_concurrent_voice_sessions config.
- **16. Dependencies & Cross-Domain Relationships:** `FEAT-017 (Rate-Card audio token & per-second pricing fields)`, `FEAT-036 (Zero-Drop Rolling Upgrade Readiness Fencing & Active Stream Drain)`, `FEAT-037 (Streaming DeltaReserve budget holds)`, `FEAT-058 (Atomic 4-Tier Emergency Kill-Switch severing ActiveStreamRegistry WebSockets)` · Related Decisions: `DEC-CAND-02`, `DEC-CAND-07`, `DEC-CAND-08`
- **17. Failure & Recovery Behavior:** If upstream WebSocket drops mid-call, gateway emits structured close frame without silent hang and settles accrued usage up to last confirmed turn; for unclosed constrained-token sessions, budget hold settles or reconciles after bounded session TTL expiry.
- **18. Security, Privacy & Financial Implications:** Prevents client-side prompt-override attacks by locking instructions/tools server-side in constrained tokens; prevents unbounded multi-hour open-mic audio spend via rolling DeltaReserve holds and hard duration caps.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Constrained token endpoint rejects client attempts to request unapproved models or widen policy-locked system instructions/tools.
  - WebSocket /v1/realtime session terminates immediately (< 1s [HYPOTHESIS]) when its Virtual Key is revoked or project budget reaches 0.
  - Audio input, cached audio, and audio output tokens settle accurately against RateCard audio rates.
- **20. Relevant Evidence & Research Confidence:** [SOURCE_VERIFIED] server.go:687-732 rejects WebSockets in RC9; [VERIFIED_FACT] OpenAI /v1/realtime + /client_secrets (SRC-048) and Gemini BidiGenerateContentConstrained (SRC-055) are production vendor contracts; [DESIGN_PROPOSAL] transport staging deferred to TEEP-03 (DEC-CAND-07).
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — Customer-support voice agents and interactive speech-to-speech copilots increasingly rely on OpenAI Realtime and Gemini Live protocols, which RC9 currently rejects at the HTTP upgrade layer. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `v1.1 (Core)`)*

---

### FEAT-128 — Unified & Native Image, Video, Speech Synthesis & Audio Transcription Endpoints with Multimodal Metering

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-128` · `CAP` Traceability: `CAP-140` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-21` (Async, Batch, Realtime & Multimodal Media Workloads) -> **Multimodal Media Generation & Speech Endpoints**
- **3. Plain-Language Product Summary:** Brings image generation/editing, video generation jobs, text-to-speech synthesis, and Whisper/audio transcription under the same Tanpopo Virtual Keys, residency controls, provenance headers, and per-image or per-second cost tracking as text chat.
  - *Practical Example:* A localization pipeline posts meeting recordings to POST /v1/audio/transcriptions and generates localized voiceovers via POST /v1/audio/speech using one project Virtual Key; Tanpopo enforces EU residency, bills exact audio seconds and character/token counts in micro-USD, and attaches EU AI Act Art. 50 provenance headers to synthetic media responses.
  - *Explicit Trade-Offs:* Media endpoints return binary audio streams or multi-megabyte image/video payloads, requiring zero-copy host blob.Store streaming (ARCH-017) rather than copying payloads into 16 MiB Wasm adapter linear memory.
- **4. User Problem & Business Value:** Enterprises currently suffer blind FinOps spots and compliance gaps when product teams spin up unmanaged OpenAI, Vertex Imagen/Veo, ElevenLabs-compatible, or Whisper endpoints because their LLM gateway only proxies chat completions.
- **5. Target Personas:** `PER-02`, `PER-06`, `PER-07`
- **6. Existing RC9 Behavior & Limitations:** ABSENT_IN_RC9 on the ingress plane: although RC9 RateCard structs include preliminary image/audio pricing fields, /v1/images/* and /v1/audio/* endpoints are absent [SOURCE_VERIFIED], aws-bedrock adapter.c:36-59 drops multimodal input blocks [SOURCE_VERIFIED], and sse/pump.go:99 lacks binary AWS EventStream framing for >2 MiB multimodal streams [SOURCE_VERIFIED].
- **7. Proposed Complete Functional Behavior:** Mount unified /v1/images/generations, /v1/images/edits, /v1/audio/transcriptions, /v1/audio/translations, /v1/audio/speech, and async video generation job endpoints alongside native passthrough equivalents. Stream multipart inputs and binary audio/image outputs via host blob.Store (ARCH-017) and binary-safe EventStream framing. Settle exact cost across resolution/quality tiers, per-second audio duration, character counts, or multimodal tokens, and emit machine-readable synthetic-media provenance headers (FEAT-071).
- **8. User-Facing Subfeatures:**
  - Unified /v1/images/{generations,edits} with resolution, quality, and partial-image SSE streaming support
  - Unified /v1/audio/{transcriptions,translations,speech} with streaming audio chunk delivery and timestamp granularities
  - Async video generation job lifecycle integrated with durable escrow accounting
  - Multi-dimensional multimodal RateCard metering (per-image resolution/quality tier, per-second audio/video, per-character TTS) and EU AI Act Art. 50 headers
- **9. End-to-End Lifecycle:** 1. Caller invokes /v1/images/* or /v1/audio/* -> 2. Gateway authenticates key, checks modality policy and residency, and reserves worst-case media budget -> 3. Host blob.Store streams media through adapter/BlindBroker without Wasm heap bloat -> 4. Response streams back with AI provenance headers while exact pixel/duration/token cost settles to usage_settlements.
- **10. Primary & Alternative User Journeys:** Primary: App transcribes customer audio via /v1/audio/transcriptions routed to regional Whisper/Voxtral pool. Alternative: Design tool generates brand-safe images via /v1/images/generations with prompt safety pre-rails.
- **11. Administrative & Permission Boundaries:** Modality allowlists on Policies and Virtual Keys let admins permit transcription while blocking synthetic image/video generation per department.
- **12. Automation Opportunities:** Automatic attachment of EU AI Act Art. 50 provenance headers and optional C2PA metadata pass-through on all generated image/audio/video responses.
- **13. Enterprise-Specific Requirements (`OPTIONAL_ECOSYSTEM_INTEGRATION`):** Zero Wasm 16 MiB OOMs on <= 32 MiB media via ARCH-017 blob_ref splicing, binary AWS EventStream CRC32 framing, and accurate multimodal FOCUS 1.1 chargeback export.
- **14. Core vs Extension Placement Candidate:** `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` — Core Data Plane provides unified /v1/images/* and /v1/audio/* ingress, blob streaming, and pixel/duration/token settlement, while provider-specific media payload mappings live in signed Tier-1/Tier-2 adapters.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Modality filters (Image, Video, Speech, Transcription) in Model Catalog, Playground, and FinOps breakdowns.
  - *CLI:* Media smoke-test support and modality rate-card inspection via 'tanpopo model inspect'.
  - *Admin / Data-Plane API:* POST /v1/images/generations, /v1/images/edits, /v1/audio/transcriptions, /v1/audio/translations, /v1/audio/speech.
  - *GitOps / Terraform / Packs:* Declarative model capabilities and multimodal tier rate cards in gateway YAML / Terraform.
- **16. Dependencies & Cross-Domain Relationships:** `ARCH-017 (Host blob.Store Zero-Copy Multimodal Splicing & Binary EventStream Framing)`, `FEAT-009 (Hardened Official Adapters restoring Bedrock multimodal blocks)`, `FEAT-017 (High-Precision Multimodal Rate-Card Modeling)`, `FEAT-071 (EU AI Act Art. 50 Provenance Response Headers)` · Related Decisions: `DEC-CAND-02`, `DEC-CAND-08`
- **17. Failure & Recovery Behavior:** Upstream media generation failures release pre-reserved media holds cleanly; binary EventStream CRC mismatches or truncated audio streams abort cleanly without recording partial garbage as complete.
- **18. Security, Privacy & Financial Implications:** Prevents shadow spend on high-cost video/image generation models (which can cost multiple USD per call) via strict pre-request media holds and modality-level RBAC.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Image generation and audio transcription/speech calls settle exact micro-USD matching resolution/quality and duration RateCard rules.
  - 32 MiB multimodal payloads stream through Tier-2 Wasm adapters via blob_ref handles without exceeding the 16 MiB Wasm sandbox cap.
  - Synthetic image and speech responses include configured EU AI Act provenance headers when policy profile is enabled.
- **20. Relevant Evidence & Research Confidence:** [VERIFIED_FACT] Empirical TEEP-01 audit of litellm_model_prices.json cataloged 1,055 image, 524 video, 132 transcription, and 89 speech models (SRC-045); [SOURCE_VERIFIED] RC9 lacks /v1/images/* and /v1/audio/*.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — Marketing, localization, accessibility, and meeting-intelligence teams use image, video, TTS, and speech-to-text models daily; governing them removes the need for separate unmonitored API keys. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `Out-of-Band .tpkg / Tooling Wave 2`)*

---

### FEAT-129 — Per-Page Metered Document OCR & Structured Layout Extraction Plane (/v1/ocr)

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-129` · `CAP` Traceability: `CAP-141` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-21` (Async, Batch, Realtime & Multimodal Media Workloads) -> **Document OCR & Layout Extraction**
- **3. Plain-Language Product Summary:** Provides a single governed /v1/ocr endpoint for converting PDFs and scanned images into clean Markdown or structured JSON across cloud and self-hosted OCR engines, with accurate per-page billing and optional PII redaction.
  - *Practical Example:* An insurance claims service sends a 12-page scanned PDF to POST /v1/ocr routed to Mistral OCR primary with on-prem VLM OCR fallback; Tanpopo enforces EU data residency, redacts national ID numbers from extracted Markdown via post-response guardrails, and settles exact 12-page cost via DocPerPageNanoUSD.
  - *Explicit Trade-Offs:* Cross-provider OCR failover normalizes common Markdown/page-block structures, whereas vendor-proprietary bounding-box polygon extensions require /native/{dialect}/* or CT-08 capability pinning.
- **4. User Problem & Business Value:** Token-only gateways misprice per-page OCR APIs (such as Mistral /v1/ocr plus structured annotation page surcharges) and force document-processing teams to build separate auth, residency, and DLP wrappers around OCR vendors.
- **5. Target Personas:** `PER-02`, `PER-06`, `PER-07`
- **6. Existing RC9 Behavior & Limitations:** ABSENT_IN_RC9: POST /v1/ocr and per-page OCR + annotation surcharge RateCard settlement are not implemented in frozen RC9 [SOURCE_VERIFIED].
- **7. Proposed Complete Functional Behavior:** Add unified POST /v1/ocr accepting inline blob_ref documents or governed tpf_* file handles (FEAT-126). Pre-estimate page count from PDF header metadata (or configured max_pages cap) for Phase-1 budget reservation, route across Mistral OCR, Azure Document Intelligence, AWS Textract/Bedrock, or self-hosted OCR pools via signed adapters, optionally run output PII/secret guardrails over extracted text/JSON, and settle exact processed_pages * (ocr_per_page + annotation_per_page) in usage_settlements.
- **8. User-Facing Subfeatures:**
  - Unified POST /v1/ocr endpoint returning normalized per-page Markdown, tables, and structured schema annotations
  - Exact per-page and structured-annotation surcharge FinOps metering (DocPerPageNanoUSD)
  - Max-page guardrail limits and optional post-extraction PII/PHI redaction before client return
  - Seamless integration with /v1/files (tpf_*) handles and zero-copy blob.Store streaming
- **9. End-to-End Lifecycle:** 1. Caller submits PDF/image or tpf_* handle to POST /v1/ocr -> 2. Gateway checks page-count ceiling, residency, and reserves per-page budget hold -> 3. Selected OCR adapter extracts structured pages via BlindBroker -> 4. Optional output guardrail redacts sensitive fields -> 5. Exact page count settles to FinOps ledger.
- **10. Primary & Alternative User Journeys:** Primary: Enterprise ingestion service extracts Markdown tables from invoices via /v1/ocr. Alternative: Strict HIPAA hospital enclave routes /v1/ocr exclusively to ON_PREM vLLM/dots.ocr GPU deployments with self_hosted_zero_rate or GPU-amortized pricing.
- **11. Administrative & Permission Boundaries:** Governed by standard Virtual Key model/route allowlists, max_ocr_pages_per_request policy ceilings, and connection residency/BAA flags.
- **12. Automation Opportunities:** Direct composition with downstream RAG chunking or batch structured-extraction pipelines using shared causal trace IDs.
- **13. Enterprise-Specific Requirements (`OPTIONAL_ECOSYSTEM_INTEGRATION`):** Bounded memory PDF streaming via ARCH-017, strict SSRF protection on any document URL fetch (disabled by default unless explicitly allowlisted), and exact integer page-rate accounting.
- **14. Core vs Extension Placement Candidate:** `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` — Core Data Plane exposes unified POST /v1/ocr with per-page RateCard settlement and PII post-redaction, while Mistral OCR, Azure Document Intelligence, Textract, and self-hosted VLM adapters map upstream schemas.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* OCR capability badge and per-page pricing inputs (USD / 1,000 pages) in Model Catalog; OCR mode in Playground.
  - *CLI:* 'tanpopo ocr extract <file>' developer convenience/smoke command.
  - *Admin / Data-Plane API:* Data Plane POST /v1/ocr; Admin RateCard doc_per_page_nano_usd & annotation_per_page_nano_usd fields.
  - *GitOps / Terraform / Packs:* Declarative OCR routes, max_pages policies, and per-page RateCard definitions.
- **16. Dependencies & Cross-Domain Relationships:** `ARCH-017 (Host blob.Store Zero-Copy Multimodal Splicing)`, `FEAT-010 (Expanded Adapter Suite including Mistral and Azure Document/Foundry adapters)`, `FEAT-017 (High-Precision Multimodal Rate-Card Modeling)`, `FEAT-055 (Built-In Deterministic PII & Secret Guardrails on extracted text)` · Related Decisions: `DEC-CAND-02`, `DEC-CAND-08`
- **17. Failure & Recovery Behavior:** Corrupt/encrypted PDF rejections (HTTP 422) release Phase-1 page holds immediately; upstream 5xx triggers residency-compliant OCR fallback before bytes are committed to the caller.
- **18. Security, Privacy & Financial Implications:** Blocks SSRF via remote document URLs by default; enforces pre-execution page-count budget caps so a 5,000-page PDF cannot unexpectedly exhaust a team's monthly budget.
- **19. Preliminary Measurable Acceptance Criteria:**
  - POST /v1/ocr processes multi-page PDFs across >= 2 provider adapters and settles exact page-count micro-USD.
  - Documents exceeding policy max_ocr_pages_per_request fail closed before upstream dispatch with zero upstream spend.
  - Post-extraction PII guardrail redacts configured patterns across extracted page Markdown blocks.
- **20. Relevant Evidence & Research Confidence:** [VERIFIED_FACT] Mistral /v1/ocr per-page + annotation pricing (SRC-064) and competitor OCR gateway adoption (SRC-005); [SOURCE_VERIFIED] /v1/ocr absent in RC9.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — Banking, insurance claims, logistics, and clinical records workflows process millions of PDF pages through specialized OCR/layout models billed per page rather than per token. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `Out-of-Band .tpkg / Tooling Wave 2`)*

---
