# 02 — Group 2 — Routing, Reliability, AI FinOps & Performance Caching

- **Document ID:** `TEEP-02A-DOC-03-02`
- **Domains Covered:** `DOM-04` (Routing and Orchestration), `DOM-05` (Reliability Engineering), `DOM-06` (AI FinOps), `DOM-07` (Caching and Performance)
- **Parent Catalog:** [`../02-MASTER-FEATURE-CATALOG.md`](../02-MASTER-FEATURE-CATALOG.md) · **Founder Guide:** [`../10-FOUNDER-REVIEW-GUIDE.md`](../10-FOUNDER-REVIEW-GUIDE.md)

---

## `DOM-04` — Routing and Orchestration (`8` Proposed Features)

- **Domain Overview:** Routing and Orchestration (DOM-04) governs how Tanpopo selects, balances, scores, adapts, and fails over requests across heterogeneous cloud and on-prem model deployments without silently dropping required capabilities or violating data-residency boundaries. It combines deterministic compliance filtering with prefix/KV-cache affinity, Time-To-First-Token (TTFT) and throughput SLO scoring, progressive canary/shadow rollouts, and hybrid on-prem-to-cloud bursting.
- **Existing RC9 Baseline Posture:** RC9's deterministic 6-step Resolver (internal/routing/resolver/resolver.go) cleanly enforces Smooth Weighted Round-Robin (SWRR), monotonic X-Tanpopo-Data-Residency narrowing, and RequireTestedCaps [RUNTIME_VERIFIED]. However, SWRR scatters multi-turn agent calls across endpoints (destroying prompt/KV cache locality) [SOURCE_VERIFIED], LOWEST_LATENCY tracks total completion duration rather than TTFT or TPOT [SOURCE_VERIFIED], RoutingPage.tsx omits allowed_data_regions and conflates Primary vs Fallback capability warnings (DEF-027, DEF-028, DEF-050) [RUNTIME_VERIFIED], cross-dialect failover is limited to /v1/chat/completions, and conditional canary/shadow/burst routing is absent.
- **Progressive Self-Hosted to Enterprise Experience:** Day-1 users get automatic 1:1 or simple primary-plus-fallback routes created during provider setup with visual Route Explain verification. As workloads mature, engineering and SRE teams enable prefix-cache affinity for multi-turn coding agents, TTFT/TPOT latency SLO scoring, 5% canary splits and 1% budget-capped shadow evaluations, on-prem GPU queue bursting into residency-compliant cloud zones, and sandboxed custom routing scorer plugins.
- **Technical Selections Deferred to `TEEP-03`:** Selection of the conditional route-matching expression engine (CEL vs Cedar vs native structured predicates, DEC-CAND-09), cluster-wide EWMA latency gossip encoding, bounded-load consistent-hash ring sizing, and Wasm ScoreRouteCandidates ABI framing (DEC-CAND-11) are deferred to TEEP-03.

| ID | Subdomain | Feature Name | Class | Disposition | Priority | Placement Candidate |
|---|---|---|---|---|---|---|
| `FEAT-024` | Core Deterministic Route Resolution | **Deterministic 6-Step Route Resolution, Weighted Balancing & Monotonic Residency Narrowing** | `CAT-PROD` | `KEEP` | `P0-Foundational` | `CORE_DATA_PLANE` |
| `FEAT-025` | Cache-Locality & Agentic Routing | **Dual-Mode Prefix & GPU KV-Cache Affinity Routing** | `CAT-PROD` | `ADD` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |
| `FEAT-026` | SLO-Aware Dynamic Scoring | **TTFT / TPOT SLO-Aware & Multi-Objective Dynamic Route Scoring** | `CAT-PROD` | `IMPROVE` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |
| `FEAT-027` | Progressive Delivery & Traffic Engineering | **Conditional Attribute/Header Routing, Progressive Canary Splits & Shadow Mirroring** | `CAT-ADMIN` | `ADD` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |
| `FEAT-028` | Extensible Routing Ecosystem | **Sandboxed Custom ROUTING_STRATEGY Extension Plugins** | `CAT-ECO` | `ADD` | `P2-Differentiator` | `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` |
| `FEAT-029` | Routing Explainability & Operator UX | **Live Route Explain == Select Equivalence, Per-Route Residency UI & Tiered Capability Warnings** | `CAT-ADMIN` | `IMPROVE` | `P0-Foundational` | `CORE_CONTROL_PLANE` |
| `FEAT-030` | Hybrid On-Prem & Sovereign Cloud Bursting | **Hybrid On-Prem GPU (vLLM / llm-d / SGLang) to Residency-Compliant Cloud Bursting** | `CAT-ADMIN` | `ADD` | `P2-Differentiator` | `CORE_DATA_PLANE` |
| `FEAT-031` | Cross-Provider Failover Resilience | **Automatic Cross-Dialect Failover Adaptation Under Zero-Silent-Downgrade (CT-08)** | `CAT-PROD` | `IMPROVE` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |

### FEAT-024 — Deterministic 6-Step Route Resolution, Weighted Balancing & Monotonic Residency Narrowing

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-024` · `CAP` Traceability: `CAP-023` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-04` (Routing and Orchestration) -> **Core Deterministic Route Resolution**
- **3. Plain-Language Product Summary:** Selects the right AI deployment for every request through a strict 6-step safety filter: checking active state, data-residency region, extension trust level, verified model capabilities, and health before balancing traffic across priority tiers and weights. Callers can request stricter regional residency per call, but can never widen or weaken their organization's residency rules.
  - *Practical Example:* A global SaaS backend serves both EU and US customers over one production route containing `eu-central-1` and `us-east-1` deployments; when handling an EU customer, the app passes `X-Tanpopo-Data-Residency: EU_CENTRAL_1`, narrowing candidate selection strictly to Frankfurt even if Frankfurt is degraded (failing closed with 503 rather than spilling EU data to Virginia).
  - *Explicit Trade-Offs:* Strict fail-closed compliance pruning means a regional outage returns `HTTP 503 no_healthy_compliant_target` instead of silently failing over across restricted borders—an intentional enterprise compliance guarantee.
- **4. User Problem & Business Value:** Eliminates the subtle compliance failure mode in best-effort AI routers where fallback tiers accidentally route regulated prompts to non-BAA, non-ZDR, or out-of-region endpoints during an incident.
- **5. Target Personas:** `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-06 (CISO, DPO & Regulatory Compliance Auditor)`
- **6. Existing RC9 Behavior & Limitations:** COMPLETE_FOR_RC9_SCOPE in `internal/routing/resolver/resolver.go` [RUNTIME_VERIFIED, SOURCE_VERIFIED]: implements deterministic 6-step resolution, Smooth Weighted Round-Robin (`SWRR`), monotonic `X-Tanpopo-Data-Residency` intersection, and `RequireTestedCaps`. Minor allocation overhead (`95 allocs/op`, `11.4 KiB/op` in `benchmark-evidence.md` [RUNTIME_VERIFIED]) is addressed by internal enabler `ARCH-007`.
- **7. Proposed Complete Functional Behavior:** Preserve RC9's exact 6-step compliance pruning order (`State -> Residency/BAA/ZDR/License -> Extension Trust -> Capability Assurance -> Circuit Breaker Health -> Priority Tier & Selection Strategy`) while plugging in new Step 6 selection strategies (`PREFIX_CACHE_AFFINITY` FEAT-025, `SLO_MULTI_OBJECTIVE` FEAT-026, custom Wasm scorers FEAT-028) and zero-allocation slice pooling (`ARCH-007`) [DESIGN_PROPOSAL].
- **8. User-Facing Subfeatures:**
  - Deterministic 6-step candidate filtering guaranteeing compliance invariants precede load-balancing heuristics.
  - Priority-tier failover chains combined with Smooth Weighted Round-Robin (`SWRR`) traffic distribution within each tier.
  - Per-request monotonic residency narrowing via `X-Tanpopo-Data-Residency` header (intersection-only against parent policies).
  - `RequireTestedCaps` assurance gate distinguishing vendor-claimed (`DECLARED`) vs probe/cassette-proven (`VERIFIED`) capabilities.
- **9. End-to-End Lifecycle:** `RoutePolicy` compiled into immutable `ConfigSnapshot` -> Per-request `Resolver.Resolve` filters and orders compliant targets in RAM -> Failover executor steps through ordered candidate slice on pre-first-byte retryable errors -> Emits structured resolution decision reason into `request_traces_meta`.
- **10. Primary & Alternative User Journeys:** Primary: Traffic splits 70/30 via SWRR across two Tier-1 deployments and fails over to Tier-2 only if both Tier-1 targets open breakers. Alternative: Caller passes `X-Tanpopo-Data-Residency: US_EAST_1` under an EU-only project policy; intersection is empty and Tanpopo rejects immediately with `403 residency_conflict`.
- **11. Administrative & Permission Boundaries:** Parent Org/BU/Project residency ceilings are monotonic; neither project admins nor per-request Data Plane headers can widen allowed regions.
- **12. Automation Opportunities:** Automatic 1:1 default route creation during Quickstart (`/setup`) and Bulk Model Onboarding (FEAT-019).
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Sub-microsecond in-RAM execution (`0` database or network calls during route resolution), deterministic tie-breaking, and full parity with `Route Explain` (FEAT-029).
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Executes in process RAM on every single Data Plane inference call and enforces non-bypassable compliance invariants prior to upstream network egress.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Priority tiers, weights, strategy, and capability assurance controls in `RoutingPage.tsx`.
  - *CLI:* `tanpopo route list | get | create | update | explain`.
  - *Admin / Data-Plane API:* `/admin/v1/routes` and `X-Tanpopo-Data-Residency` header in `dataplane-v1.yaml`.
  - *GitOps / Terraform / Packs:* Declarative `route_policy` target lists, priority tiers, and weights.
- **16. Dependencies & Cross-Domain Relationships:** `Optimized by `ARCH-007` (`CAP-052` buffer pooling) and extended by FEAT-025..031 and FEAT-069 (residency/BAA/ZDR).` · Related Decisions: None (Standard Catalog Review)
- **17. Failure & Recovery Behavior:** When all compliant targets are unhealthy, returns structured `HTTP 503` with pruned-reason counts without ever falling back to non-compliant targets.
- **18. Security, Privacy & Financial Implications:** Forms the primary runtime enforcement point for sovereign data residency (`ADR-0006`) and plugin trust boundaries.
- **19. Preliminary Measurable Acceptance Criteria:**
  - `X-Tanpopo-Data-Residency` can narrow allowed regions within policy bounds and returns `403` whenever disjoint from parent policy.
  - SWRR distributes requests smoothly without burst clumping across weighted targets within the same priority tier.
  - Zero non-compliant (out-of-region, untrusted-plugin, or capability-missing) targets are ever selected during failover chaos tests.
- **20. Relevant Evidence & Research Confidence:** HIGH — [RUNTIME_VERIFIED] and [SOURCE_VERIFIED] in `internal/routing/resolver/resolver.go` and `ADR-0006` on ac9ae87a.
- **21. Proposal Classification:** `KEEP` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Crown-jewel RC9 architectural backbone guaranteeing that routing decisions never violate data residency, extension trust, or required model capabilities even during chaotic multi-vendor failovers. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-025 — Dual-Mode Prefix & GPU KV-Cache Affinity Routing

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-025` · `CAP` Traceability: `CAP-024` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: `INN-07`
- **2. Functional Domain & Subdomain:** `DOM-04` (Routing and Orchestration) -> **Cache-Locality & Agentic Routing**
- **3. Plain-Language Product Summary:** Keeps multi-turn AI conversations and shared system prompts 'sticky' to the specific cloud connection or self-hosted GPU pool that already holds their warm prompt cache, while automatically spilling overflow traffic if one endpoint gets too busy. This unlocks massive vendor prompt-cache cost discounts and faster first-token speeds without storing a single byte of prompt text.
  - *Practical Example:* Twenty developers use Claude Code and an internal `llm-d` vLLM cluster across three redundant API keys and four GPU pools; instead of round-robin scattering every turn to a cold cache, Tanpopo hashes the leading system + tools + conversation prefix in RAM and routes matching prefixes consistently to the warm endpoint, preserving 90% Anthropic cache-read discounts and cutting vLLM prefill latency.
  - *Explicit Trade-Offs:* Strict consistent hashing can overload a single target when thousands of users share one identical system prompt; Tanpopo resolves this via Bounded-Load Consistent Hashing (default load factor `c = 1.25`), which bounds hot-spot queue buildup at the cost of intentionally warming a second replica when traffic spikes.
- **4. User Problem & Business Value:** Bridges commercial API prompt-cache economics (OpenAI `prompt_cache_key`, Anthropic `cache_control`, Gemini implicit caching) and self-hosted GPU KV-cache locality (`vLLM`, `SGLang`, `CNCF llm-d`) in one unified router, targeting 30–70% multi-turn input-token cost reduction and lower TTFT `[HYPOTHESIS]`.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED]: `resolver.go` offers `WEIGHTED_ROUND_ROBIN`, `LOWEST_LATENCY`, and `LOWEST_COST` only; multi-turn agent turns scatter across connections/deployments, invalidating warm caches.
- **7. Proposed Complete Functional Behavior:** Implement `INN-07` `PREFIX_CACHE_AFFINITY` selection strategy in `Resolver` Step 6 [DESIGN_PROPOSAL]: (1) canonicalize leading `system` instructions, sorted `tools` schemas, and initial prefix blocks up to a configurable token/byte window (`0` prompt bytes persisted); (2) compute a fast 64-bit keyed `SipHash` using a per-project ephemeral RAM key; (3) select among healthy compliant deployments via **Consistent Hashing with Bounded Loads (`c = 1.25`)**; and (4) inject deterministic `prompt_cache_key` (OpenAI) or Kubernetes `GAIE` / `sgl-router` prefix-affinity headers upstream so downstream GPU schedulers co-locate KV blocks.
- **8. User-Facing Subfeatures:**
  - `PREFIX_CACHE_AFFINITY` routing strategy selectable per RoutePolicy with configurable bounded-load factor `c` (default `1.25`).
  - Zero-persistence keyed in-RAM `SipHash` prefix canonicalizer over system prompts, tool definitions, and leading conversation blocks.
  - Dual-mode upstream affinity header injection: OpenAI `prompt_cache_key` and self-hosted `vLLM` / `SGLang` / `llm-d` `GAIE` headers.
  - Cache-hit efficiency telemetry (`cached_input_tokens / total_input_tokens` ratio per route) in FinOps and Traces views.
- **9. End-to-End Lifecycle:** Route configured with `PREFIX_CACHE_AFFINITY` -> Request arrives -> Prefix canonicalized & hashed in RAM -> Bounded-load hash ring picks warm target (or next ring neighbor if active concurrency > `1.25 * mean`) -> Affinity header injected -> Settled `cached_input_tokens` tracked in FinOps rollups.
- **10. Primary & Alternative User Journeys:** Primary: Multi-turn 40k-token coding session hits the same Anthropic connection across 15 consecutive turns, achieving warm cache reads on turns 2–15. Alternative: Flash crowd sends 500 concurrent requests with identical system prompt; bounded-load factor `c=1.25` spills excess load cleanly across secondary endpoints instead of saturating one connection.
- **11. Administrative & Permission Boundaries:** Configured per route by `TEAM_ADMIN` / `ORG_OWNER`; ephemeral per-project `SipHash` keys ensure prefix hashes cannot be reversed or correlated across tenants.
- **12. Automation Opportunities:** Pairs automatically with FEAT-046 (Upstream Prompt-Cache Breakpoint Injection) when a route uses multi-turn chat or agentic `/v1/responses`.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** `<= 50 microseconds` CPU hashing budget `[HYPOTHESIS]`, `0` disk/DB prompt storage, deterministic ring rebalancing when a deployment breaker opens (`SM-04`), and multi-pod hash consistency within an epoch.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — In-RAM keyed prefix hashing (`SipHash`) and Bounded-Load Consistent Hashing must execute inline inside Data Plane Route Resolution Step 6 with sub-microsecond overhead and zero prompt persistence.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* `Prefix / KV-Cache Affinity` strategy option + bounded-load slider in `RoutingPage.tsx` and cache-hit KPI in `FinOpsPage.tsx`.
  - *CLI:* `tanpopo route create/update --strategy prefix-cache-affinity --max-load-factor 1.25`.
  - *Admin / Data-Plane API:* `PREFIX_CACHE_AFFINITY` strategy enum and `affinity_config` on `/admin/v1/routes`.
  - *GitOps / Terraform / Packs:* Declarative `strategy: PREFIX_CACHE_AFFINITY` on `route_policy` manifests.
- **16. Dependencies & Cross-Domain Relationships:** `Core component of `INN-07` alongside FEAT-035 (budget-safe TTFT hedging) and FEAT-046 (prompt-cache breakpoint injection).`, `Integrates with FEAT-010 (`@tanpopo/vllm-llmd` GAIE headers) and FEAT-024 (6-step Resolver).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** If the primary hashed deployment trips its `SM-04` circuit breaker, traffic hashes deterministically to the next healthy ring node, warming that single fallback rather than scattering across all remaining nodes.
- **18. Security, Privacy & Financial Implications:** Stores zero prompt content anywhere, isolates `SipHash` keys per project to prevent cross-tenant timing/hash oracles, and directly reduces enterprise input-token spend.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Multi-turn requests sharing a leading prefix consistently select the same healthy deployment across turns when load is below `c = 1.25`.
  - Under synthetic hot-key skew, no single deployment receives more than `ceil(c * avg_load)` concurrent requests.
  - OpenAI `prompt_cache_key` and `vLLM`/`llm-d` affinity headers are emitted deterministically without leaking raw prompt substrings.
- **20. Relevant Evidence & Research Confidence:** HIGH — Grounded in Google/Vimeo Bounded-Load Consistent Hashing research, OpenRouter sticky cache routing (SRC-013), Anthropic/OpenAI prompt caching (SRC-048, SRC-051), and CNCF `llm-d` 1.0 (SRC-032); `30–70%` cost reduction range is `[HYPOTHESIS]` dependent on multi-turn prefix share.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Multi-turn coding and enterprise RAG agents send massive shared system+tool prefixes on every turn; RC9's round-robin scattering destroys 50–97.5% commercial prompt-cache discounts and self-hosted GPU KV-cache reuse. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-026 — TTFT / TPOT SLO-Aware & Multi-Objective Dynamic Route Scoring

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-026` · `CAP` Traceability: `CAP-025` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-04` (Routing and Orchestration) -> **SLO-Aware Dynamic Scoring**
- **3. Plain-Language Product Summary:** Upgrades latency- and cost-based routing so Tanpopo measures how fast a provider starts speaking (Time-To-First-Token) and how fast it streams words (tokens per second), rather than penalizing models just because a user asked for a longer answer. Operators can pick ready-made presets like Fastest First Token, Highest Throughput, Lowest Effective Cached Cost, or Most Reliable Tool Calling.
  - *Practical Example:* An interactive voice/chat copilot sets its route strategy to `LOWEST_TTFT` with a `1,200 ms` TTFT SLO; when us-east-1 OpenAI experiences prefill queue delays (`TTFT` rising to `2,800 ms`), Tanpopo automatically shifts weight toward a warm Azure Foundry deployment running at `380 ms` TTFT without waiting for hard 5xx errors.
  - *Explicit Trade-Offs:* Pure winner-take-all latency routing can cause herd oscillation where every pod piles onto one fast endpoint until it slows down; Tanpopo prevents flapping via smoothed EWMA decay, minimum exploration floor weights (e.g., 5% probe traffic), and bounded-load caps.
- **4. User Problem & Business Value:** In RC9 (`resolver.go:623`), `LOWEST_LATENCY` measures total end-to-end request duration—meaning an endpoint that streamed 2,000 tokens at 120 tok/s (16.6s) scored 'slower' than a sluggish endpoint that emitted 50 tokens at 10 tok/s (5.0s) [SOURCE_VERIFIED].
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in `internal/routing/resolver/resolver.go:623` [SOURCE_VERIFIED]: `LOWEST_LATENCY` uses total duration EWMA only; `LOWEST_COST` ignores observed prompt-cache hit ratios; and zero `TTFT`, `TPOT` (tokens/sec), or tool-schema reliability signals are tracked.
- **7. Proposed Complete Functional Behavior:** Maintain per-deployment sliding EWMA metrics for `ttft_ms`, `tpot_ms` (inter-token latency / tok/s), `effective_cost_per_1k_tok` (factoring observed cache-read ratio), `throttle_429_rate`, and `tool_valid_rate` [DESIGN_PROPOSAL]. Expose intuitive presets (`LOWEST_TTFT`, `HIGHEST_THROUGHPUT`, `EFFECTIVE_LOWEST_COST`, `TOOL_RELIABILITY`, and custom weighted multi-objective blends) with anti-oscillation floor weights.
- **8. User-Facing Subfeatures:**
  - Separate real-time `TTFT` (prefill responsiveness) and `TPOT` / `tok/s` (generation speed) EWMA tracking per deployment.
  - Composable routing presets: `LOWEST_TTFT`, `HIGHEST_THROUGHPUT`, `EFFECTIVE_LOWEST_COST` (cache-aware), and `TOOL_RELIABILITY`.
  - Optional soft `ttft_slo_ms` threshold deprioritizing sluggish endpoints before hard timeouts occur.
  - Live per-deployment `TTFT`, `tok/s`, and effective cost telemetry cards in RoutingPage and Route Explain.
- **9. End-to-End Lifecycle:** SSE pump timestamps byte #1 (`ttft_ms`) and final token count / stream duration (`tpot_ms`) -> updates lock-free local EWMA (periodically gossiped via Valkey in HA mode) -> Resolver Step 6 computes composite target weights with exploration floor -> Route Explain displays live signal breakdown.
- **10. Primary & Alternative User Journeys:** Primary: Interactive IDE chat route uses `LOWEST_TTFT` to favor snappy prefill endpoints, while background document summarization uses `EFFECTIVE_LOWEST_COST`. Alternative: Cold start with zero recent traffic falls back deterministically to configured static base weights until minimum sample count (`N >= 5`) is observed.
- **11. Administrative & Permission Boundaries:** Configured per route by `TEAM_ADMIN` / `ORG_OWNER`; telemetry aggregates contain zero tenant-identifiable content.
- **12. Automation Opportunities:** Automatic periodic 5% exploration probing allows previously degraded endpoints to prove recovery and smoothly regain traffic share without operator intervention.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Lock-free atomic metric updates on the hot path, anti-flapping damping factors, and cluster-shared EWMA summaries over Valkey (gracefully using pod-local EWMA when Valkey is absent).
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Streaming `TTFT`/`TPOT` observation and EWMA score evaluation run inline inside the Data Plane SSE pump and Route Resolver.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Strategy preset selector, Multi-Objective Weight Sliders, and live TTFT/tok-per-sec sparklines in `RoutingPage.tsx`.
  - *CLI:* `tanpopo route update --strategy lowest-ttft --ttft-slo-ms 1500` and `tanpopo route explain` score breakdown.
  - *Admin / Data-Plane API:* Expanded strategy enums, `scoring_weights`, and live deployment SLO stats in `admin-v1.yaml`.
  - *GitOps / Terraform / Packs:* Declarative `strategy` and `slo_targets` fields on `route_policy` resources.
- **16. Dependencies & Cross-Domain Relationships:** `Pairs with FEAT-029 (`Route Explain == Select` equivalence) and FEAT-032 (cluster telemetry gossip).`, `Drives burst triggers for FEAT-030 (hybrid on-prem GPU to cloud bursting).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Outlier requests aborted by the client do not poison deployment `TTFT`/`TPOT` EWMA trackers.
- **18. Security, Privacy & Financial Implications:** `EFFECTIVE_LOWEST_COST` prevents routers from naively picking a nominally cheaper provider that lacks prompt caching when a warm-cached provider actually costs 75% less per turn.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Long streaming completions at high tok/s never degrade a deployment's `LOWEST_TTFT` or `HIGHEST_THROUGHPUT` score.
  - Injecting a 2,000 ms prefill delay on Deployment A shifts >=80% of traffic to healthy 200 ms Deployment B within 10 requests while retaining floor probe traffic.
  - `Route Explain` displays the exact live `TTFT`, `TPOT`, and composite score used by `Select`.
- **20. Relevant Evidence & Research Confidence:** HIGH — RC9 `LOWEST_LATENCY` total-duration flaw is [SOURCE_VERIFIED] at `resolver.go:623` on ac9ae87a; multi-signal scoring (`:nitro`, `:floor`, `:exacto`) is proven in market practice (SRC-013).
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Fixes RC9's flawed `LOWEST_LATENCY` metric (which tracks total completion duration, unfairly penalizing verbose or reasoning answers) by scoring true Time-To-First-Token (`TTFT`), token throughput (`TPOT`), effective cached cost, and tool-call reliability. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-027 — Conditional Attribute/Header Routing, Progressive Canary Splits & Shadow Mirroring

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-027` · `CAP` Traceability: `CAP-026` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-04` (Routing and Orchestration) -> **Progressive Delivery & Traffic Engineering**
- **3. Plain-Language Product Summary:** Lets teams route traffic conditionally based on request attributes—such as custom headers, customer tier tags, modality, or prompt token length—and safely test model upgrades in production using exact percentage canary splits (like 5% new model / 95% current model) or silent background shadow copies whose answers are logged for quality comparison without affecting live users.
  - *Practical Example:* An AI platform team rolls out a new self-hosted `Qwen-3` deployment by first mirroring 1% of live traffic asynchronously (capped at USD 25/day shadow budget) for 48 hours, then routing 100% of internal staff (`X-Tanpopo-Canary: internal`) and 5% of general traffic via deterministic session bucketing before full promotion.
  - *Explicit Trade-Offs:* Unbounded regex or Turing-complete routing scripts could introduce ReDoS or latency spikes on the hot path; therefore, Tanpopo restricts conditional predicates to compile-time cost-bounded expressions (exact engine selection—`CEL` vs `Cedar` vs structured native matchers—deferred to `TEEP-03` `DEC-CAND-09`) and strictly forbids shadow mirroring on mutating tool/MCP/A2A requests.
- **4. User Problem & Business Value:** In RC9, routes support static priority tiers and weights only (`0` conditional header/tag rules, `0` sticky session canary bucketing, and `0` async shadow mirroring), forcing risky big-bang model switches.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-04 (Business Unit / Line-of-Business Delegated Admin)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED]: `RoutePolicy` has no conditional match rules, sticky canary hash keys, token-length complexity tiers, or asynchronous shadow-mirroring execution path.
- **7. Proposed Complete Functional Behavior:** Extend `RoutePolicy` with ordered conditional routing rules evaluated over safe request metadata (allowlisted `X-Tanpopo-*` / custom headers, VirtualKey attribution tags, `estimated_input_tokens`, modality, tool count), deterministic percentage canary splits (sticky by session/user hash if desired), and asynchronous fire-and-forget Shadow Mirroring (`1–10%` sample, response discarded after quality/latency/cost comparison, strictly bounded by a dedicated `shadow_budget_ledger` and never blocking the primary caller) [DESIGN_PROPOSAL]. Defer exact expression engine selection (`CEL` vs `Cedar` vs native structured rules) to `TEEP-03` (`DEC-CAND-09`).
- **8. User-Facing Subfeatures:**
  - Visual Rule Builder & expression predicates matching headers, VirtualKey tags, modality, and `estimated_input_tokens` (e.g., >32k long-context routing).
  - Deterministic Canary Traffic Splits (1% -> 5% -> 25% -> 100%) with optional sticky session/tenant hashing.
  - Asynchronous Budget-Capped Shadow Mirroring recording side-by-side latency, error rate, and token cost delta without impacting live responses.
  - 1-Click 'Promote Canary to 100%' and 'Abort Canary (Instant 0%)' operational controls.
- **9. End-to-End Lifecycle:** Author conditional/canary/shadow rule -> compile-time cost & residency compliance validation -> preview impact in What-If Engine (FEAT-139) -> activate canary/shadow -> monitor side-by-side canary vs baseline metrics -> promote to primary or roll back in 1 click.
- **10. Primary & Alternative User Journeys:** Primary: Prompts `< 8k` tokens route to fast Flash/Haiku tier while `>= 8k` tokens route to Pro/Sonnet tier automatically. Alternative: Shadow mirror daily cap (`USD 10`) is reached at 14:00 UTC; shadow sampling pauses cleanly (`shadow_budget_exhausted`) with zero effect on primary production traffic.
- **11. Administrative & Permission Boundaries:** Conditional rules can never route to a deployment that violates parent Org/Project residency, trust, or license ceilings (evaluated strictly after Resolver Steps 1–4); mutations require `ActionRouteWrite`.
- **12. Automation Opportunities:** Optional automated canary circuit-breaker rollback if canary 5xx rate or TTFT breaches configured thresholds.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Compile-time bounded expression evaluation (`0` unbounded loops, `0` regex backtracking), strict side-effect prohibition on shadow calls, and separate FinOps attribution (`traffic_kind=SHADOW_MIRROR`).
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Bounded-cost conditional predicate evaluation, deterministic canary bucketing, and async shadow dispatch execute inside the Data Plane routing layer after compliance pruning.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Conditional Rules, Canary Split Slider, and Shadow Mirroring comparison drawer in `RoutingPage.tsx`.
  - *CLI:* `tanpopo route set-canary <route> --target <dep> --percent 5` and `tanpopo route promote-canary`.
  - *Admin / Data-Plane API:* `match_rules`, `canary_splits`, and `shadow_mirror_config` on `/admin/v1/routes`.
  - *GitOps / Terraform / Packs:* Declarative conditional rules and progressive canary weights in route manifests.
- **16. Dependencies & Cross-Domain Relationships:** `Expression engine selection anchored on `DEC-CAND-09` (deferred to TEEP-03).`, `Powers FEAT-022 (`INN-02` migration canary), FEAT-092 (shadow canary evaluation), and FEAT-139 (`INN-01` What-If replay).` · Related Decisions: `DEC-CAND-09`
- **17. Failure & Recovery Behavior:** Shadow worker pool saturation or shadow upstream 500s drop only the shadow copy and never delay, fail, or trip circuit breakers on the primary customer response.
- **18. Security, Privacy & Financial Implications:** Shadow targets undergo the exact same residency, BAA, ZDR, and guardrail checks as primary targets so sensitive prompts never leak to lower-trust staging endpoints during shadow tests.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Conditional rules accurately route traffic by header, VirtualKey tag, and estimated token length within sub-millisecond evaluation overhead.
  - Shadow-mirrored traffic enforces `shadow_budget_ledger` hard stops, skips mutating endpoints, and adds 0 ms blocking wait to primary responses.
  - No conditional or shadow branch can select a deployment pruned by residency, trust, or capability compliance filters.
- **20. Relevant Evidence & Research Confidence:** MEDIUM-HIGH — Conditional DAGs and shadow traffic are proven in Envoy AI Gateway and Portkey (SRC-010, SRC-016); expression DSL selection is explicitly deferred to `TEEP-03` (`DEC-CAND-09`).
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Enterprise SRE and AI product teams require safe progressive delivery (5% canary rollouts, header/tier-based routing, and non-blocking 1% shadow evaluations) before cutting 100% of production traffic over to new models. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-028 — Sandboxed Custom ROUTING_STRATEGY Extension Plugins

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-028` · `CAP` Traceability: `CAP-027` · Catalog Class: `CAT-ECO` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-04` (Routing and Orchestration) -> **Extensible Routing Ecosystem**
- **3. Plain-Language Product Summary:** Allows organizations to write and install custom routing algorithms as safe, sandboxed `.tpkg` plugins—for example, preferring data centers currently running on renewable grid power or custom internal GPU priority formulas. Custom plugins can only re-rank already-compliant healthy targets and automatically fall back to standard routing if a plugin times out.
  - *Practical Example:* A European telecommunications carrier authors a 40-line Rust `ROUTING_STRATEGY` `.tpkg` plugin scoring three sovereign GPU regions by real-time grid carbon intensity and internal interconnect utilization; Tanpopo runs the Wasm scorer in `< 1 ms` after pruning any non-EU targets, falling back to `LOWEST_TTFT` if the plugin ever faults.
  - *Explicit Trade-Offs:* Invoking a Wasm guest on the per-request routing path adds ~50–300 microseconds of serialization overhead `[HYPOTHESIS]`, so built-in native Go strategies (`SWRR`, `PREFIX_CACHE_AFFINITY`, `LOWEST_TTFT`) remain default while `ROUTING_STRATEGY` plugins serve specialized custom logic.
- **4. User Problem & Business Value:** Avoids both Bifrost's fragile Go `.so` plugin ABI (which breaks across Go patch versions) and Plano's requirement to run dedicated GPU orchestrator SLMs just to customize routing logic [SOURCE_VERIFIED, SRC-011, SRC-021].
- **5. Target Personas:** `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-09 (Community & Partner Extension Author)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED]: `runtime.proto` and `manifest_validator.go` have no `ROUTING_STRATEGY` extension category or candidate-scoring RPC.
- **7. Proposed Complete Functional Behavior:** Add `ROUTING_STRATEGY` to Tanpopo's `.tpkg` extension contract (`ScoreRouteCandidates` pure function with `<= 1 ms` CPU fuel limit, `<= 8 MiB` linear memory, `0` outbound network/host I/O) [DESIGN_PROPOSAL]. Invoke the scorer strictly at Step 6 over already-pruned compliant candidates, passing anonymized request metadata + candidate telemetry vectors (`0` raw prompts, `0` credentials by default), and fail open to the route's configured fallback built-in strategy on any trap or fuel exhaustion.
- **8. User-Facing Subfeatures:**
  - Sandboxed `ROUTING_STRATEGY` `.tpkg` plugin category authorable via Go/Rust/TypeScript SDKs (FEAT-102).
  - Strict post-compliance execution boundary: plugins can rank eligible targets (`[0.0..1.0]` weights) but can never re-admit pruned targets.
  - Configured per-route JSON parameter schema validated at save time and visible in `Route Explain`.
  - Deterministic fallback to built-in strategy (`SWRR` / `LOWEST_TTFT`) with `FaultGuard` auto-quarantine on repeated traps.
- **9. End-to-End Lifecycle:** Developer builds & tests `ROUTING_STRATEGY` `.tpkg` via `tanpopo plugin test` -> Admin installs under org trust policy -> Route selects custom strategy + fallback built-in strategy -> `Route Explain` & live `Select` invoke identical Wasm scorer.
- **10. Primary & Alternative User Journeys:** Primary: Custom scorer ranks 3 compliant GPU pools cleanly in 0.2 ms. Alternative: Buggy plugin enters an infinite loop; `wazero` fuel limiter aborts it at `1.0 ms`, Resolver immediately uses the fallback `LOWEST_TTFT` strategy with zero dropped customer calls, and `FaultGuard` alerts the operator.
- **11. Administrative & Permission Boundaries:** Installing a `ROUTING_STRATEGY` plugin obeys `org_extension_policies` trust tiers (FEAT-108); attaching it to a route requires `ActionRouteWrite`.
- **12. Automation Opportunities:** `tanpopo plugin init --type routing-strategy` scaffolds a ready-to-compile scorer with benchmark and fuzz harnesses.
- **13. Enterprise-Specific Requirements (`OPTIONAL_ECOSYSTEM_INTEGRATION`):** Zero raw prompt or secret exposure to scorer plugins, hard `<= 1 ms` CPU ceiling `[HYPOTHESIS]`, and bit-for-bit `Explain == Select` parity.
- **14. Core vs Extension Placement Candidate:** `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` — Core Data Plane enforces compliance pruning (Steps 1–5) and strict CPU/memory sandbox caps, while custom scoring logic executes inside a pure-function Tier-2 Wasm `.tpkg` plugin.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Installed custom strategies appear in the `RoutingPage` Strategy dropdown with schema-driven config inputs.
  - *CLI:* `tanpopo plugin init/test/pack` support for `ROUTING_STRATEGY` and `tanpopo route explain` plugin output.
  - *Admin / Data-Plane API:* `custom_strategy_extension_id` and `strategy_config_json` on `/admin/v1/routes`.
  - *GitOps / Terraform / Packs:* Pinned `@sha256` plugin reference + declarative route config in YAML.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on ARCH-014 (`wazero` sandbox), FEAT-100 (11-category extension host contracts), and FEAT-102 (`DEC-CAND-11` SDKs).` · Related Decisions: `DEC-CAND-11`
- **17. Failure & Recovery Behavior:** Always fails open to the pre-filtered compliant candidate set's built-in fallback strategy (never failing the user request and never bypassing Steps 1–5 compliance checks).
- **18. Security, Privacy & Financial Implications:** Zero network imports (`0` data exfiltration channel), zero prompt text passed, and compliance invariants (residency, BAA, ZDR, budget holds) remain 100% Host-enforced.
- **19. Preliminary Measurable Acceptance Criteria:**
  - A custom `ROUTING_STRATEGY` `.tpkg` re-orders compliant targets identically in both `Route Explain` and live traffic.
  - A malicious or buggy plugin attempting to return a target ID outside the Host-supplied compliant list has that target rejected.
  - A plugin exceeding its `1 ms` CPU fuel limit triggers graceful fallback to the base built-in strategy with 0 HTTP 5xx errors.
- **20. Relevant Evidence & Research Confidence:** MEDIUM-HIGH — Builds directly on RC9's [RUNTIME_VERIFIED] `wazero` `FaultGuard` architecture; `< 1 ms` budget is `[HYPOTHESIS]` based on RC9's sub-millisecond Tier-2 Wasm transform benchmarks.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — Enables regulated enterprises, researchers, and ISVs to ship proprietary domain-specific candidate scoring (e.g., carbon-intensity-aware, sovereign telco quota, or custom SLA formulas) as `.tpkg` extensions without forking Tanpopo Core. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `Out-of-Band .tpkg / Tooling Wave 2`)*

---

### FEAT-029 — Live Route Explain == Select Equivalence, Per-Route Residency UI & Tiered Capability Warnings

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-029` · `CAP` Traceability: `CAP-028` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-04` (Routing and Orchestration) -> **Routing Explainability & Operator UX**
- **3. Plain-Language Product Summary:** Makes Tanpopo's 'Route Explain' simulator execute the exact same decision logic as live production traffic and upgrades the Routing Console UI so operators can configure per-route data-residency regions directly, see full provider/region details on saved targets, and distinguish primary model capability warnings from fallback model gaps.
  - *Practical Example:* An admin configures a route with Claude Sonnet 4 (Primary, supports PDF + tools) and a small local Llama model (Fallback #1, lacks PDF vision); instead of showing a confusing blanket capability warning or hiding regional settings, `RoutingPage` clearly shows `Primary: 100% Capable | Fallback #1: Missing [pdf_input]`, lets the admin restrict the route to `EU_CENTRAL_1`, and proves exact failover order in `Route Explain`.
  - *Explicit Trade-Offs:* Strict `Explain == Select` equivalence requires feeding live breaker, EWMA latency, PTU saturation, and Virtual Key context into the Explain handler (read-only snapshot) rather than evaluating static DB rows, which slightly enriches the Admin-to-Snapshot bridge in split CP/DP deployments.
- **4. User Problem & Business Value:** In RC9, `RoutingPage.tsx:254` omitted `allowed_data_regions` from the UI, conflated Primary vs Fallback capability warnings (`DEF-050`), rendered minimal metadata on saved target rows (`DEF-027`), and had incomplete correlated Virtual-Key Explain E2E coverage (`DEF-028`).
- **5. Target Personas:** `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-04 (Business Unit / Line-of-Business Delegated Admin)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in `web/console/src/pages/RoutingPage.tsx` and `internal/admin/http/routes.go` [RUNTIME_VERIFIED, SOURCE_VERIFIED]: `allowed_data_regions` exists in API/SQL but is missing from the Create/Edit Route UI (`:254`); capability mismatch warnings do not partition Primary vs Fallback tiers (`DEF-050`); and saved route target provenance & Virtual-Key-correlated Explain had open QA-005 partials (`DEF-027`, `DEF-028`, `FND-015`).
- **7. Proposed Complete Functional Behavior:** Guarantee shared-codepath `Explain == Select` equivalence across all 6 steps + dynamic strategies (accepting optional simulated Virtual Key, headers, `estimated_input_tokens`, and required capabilities) [DESIGN_PROPOSAL]. Expose `allowed_data_regions` multi-selector in Create/Edit Route drawers (intersected with parent project/org policy), partition capability/context warnings clearly by tier (`Tier 1 Primary` vs `Tier 2 Fallback #k`), and display full deployment provenance (provider, connection, region, rate summary, live health) on saved route target rows.
- **8. User-Facing Subfeatures:**
  - Per-route `Allowed Data Regions` multi-selector in Console Create/Edit Route drawers constrained by parent policy.
  - Tier-partitioned capability & context-window warnings (`Primary Tier OK` vs `Fallback Tier #2 lacks structured_outputs`).
  - Rich saved target rows displaying provider icon/name, connection, data region, `USD/1M` rate summary, and live breaker status.
  - Interactive `Route Explain` workbench supporting simulated Virtual Keys, residency headers, modalities, and conditional attributes.
- **9. End-to-End Lifecycle:** Author/edit route in Console with live tiered capability & residency feedback -> run interactive `Route Explain` with sample Virtual Key/headers -> save with `If-Match` -> jump directly from any historical trace in `TracesPage` into `Route Explain` pre-filled with that trace's parameters.
- **10. Primary & Alternative User Journeys:** Primary: Operator tests why Virtual Key `vk_eu_support` rejected a vision call; `Route Explain` pinpoints Step 2 pruning of the US vision target and Step 4 pruning of the EU text-only fallback. Alternative: Project Admin views Explain; cross-project unshared connection internals remain redacted per RBAC.
- **11. Administrative & Permission Boundaries:** Explain enforces strict caller RBAC scoping so a project-level user never learns the existence or names of unshared deployments belonging to other projects or tenants.
- **12. Automation Opportunities:** 1-click 'Explain This Trace' deep link from `TracesPage` (FEAT-048) pre-populating the exact parameters of a failed 400/503 request.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** 15-locale localized pruning reason codes (fixing `PG-003`-style raw enum leakage), WCAG 2.2 AA accessible status indicators without color-only encoding, and E2E equivalence tests.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Bridges the shared `Resolver` engine into the Control Plane Explain API and `RoutingPage.tsx` Console drawers.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Upgraded Create/Edit Route Drawer, Target Provenance Rows, and `Route Explain` Workbench in `RoutingPage.tsx`.
  - *CLI:* `tanpopo route explain <route-slug> --vkey <id> --residency EU_CENTRAL_1 --caps vision,tools`.
  - *Admin / Data-Plane API:* POST `/admin/v1/routes/{id}/explain` (and dry-run unsaved route explain) in `admin-v1.yaml`.
  - *GitOps / Terraform / Packs:* `allowed_data_regions` round-tripped cleanly across UI, API, and declarative YAML.
- **16. Dependencies & Cross-Domain Relationships:** `Closes `DEF-027`, `DEF-028`, and `DEF-050` (`FND-015`) and integrates with FEAT-024..028.` · Related Decisions: None (Standard Catalog Review)
- **17. Failure & Recovery Behavior:** Explain calls are strictly read-only (`0` budget reservations, `0` upstream probes, `0` breaker mutations).
- **18. Security, Privacy & Financial Implications:** Prevents operator misconfigurations of regional residency fences by making per-route region rules visible and verifiable in the UI.
- **19. Preliminary Measurable Acceptance Criteria:**
  - `RoutingPage.tsx` Create and Edit drawers view and update `allowed_data_regions`, persisted via API.
  - Capability warnings explicitly distinguish Primary Tier gaps from Fallback Tier #k gaps (verifying `DEF-050` closure).
  - Automated E2E suite proves `Explain` and live `Select` produce identical ordered candidate lists across Virtual Keys, residency headers, and breaker states (`DEF-027`, `DEF-028`).
- **20. Relevant Evidence & Research Confidence:** HIGH — UI omission (`RoutingPage.tsx:254`) and QA-005 `DEF-027`/`028`/`050` are [SOURCE_VERIFIED] and [RUNTIME_VERIFIED] on ac9ae87a.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Closes concrete QA-005 defects (`DEF-027`, `DEF-028`, `DEF-050`) and the missing per-route residency UI control in `RoutingPage.tsx`, ensuring operators can trust dry-run routing explanations 100%. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-030 — Hybrid On-Prem GPU (vLLM / llm-d / SGLang) to Residency-Compliant Cloud Bursting

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-030` · `CAP` Traceability: `CAP-029` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-04` (Routing and Orchestration) -> **Hybrid On-Prem & Sovereign Cloud Bursting**
- **3. Plain-Language Product Summary:** Lets organizations keep everyday AI traffic on their own internal GPU servers (`vLLM`, `SGLang`, `llm-d`) for low marginal cost, and automatically overflow peak traffic queues to approved regional cloud providers (like EU Bedrock or Vertex AI) whenever internal GPU wait times exceed target thresholds—while strictly blocking cloud overflow for confidential 'On-Prem Only' workloads.
  - *Practical Example:* A manufacturing enterprise runs an 8xH200 `vLLM` cluster in Munich (`ON_PREM`); during 09:00–11:00 CET peak load when local in-flight concurrency hits 64 and `TTFT` crosses `1,500 ms`, general engineering traffic bursts smoothly to Frankfurt AWS Bedrock (`EU_CENTRAL_1`), whereas R&D CAD routes tagged strictly `ON_PREM` queue locally or shed cleanly (`503`) with zero cloud leakage.
  - *Explicit Trade-Offs:* Bursting open-weight on-prem models (e.g., `Qwen-3` or `Llama-3.3`) to cloud endpoints requires either matching hosted open-weight endpoints (Bedrock/Vertex/Fireworks) or explicit multi-model route compatibility (FEAT-031) plus separate burst FinOps budget envelopes so cloud spikes cannot run up unlimited bills.
- **4. User Problem & Business Value:** Without governed cloud bursting, enterprises must either over-provision expensive on-prem GPUs for rare peak hours (~30–40% average utilization) or suffer multi-second queue stalls; hybrid bursting targets `>80%` internal GPU utilization while preserving interactive SLAs `[HYPOTHESIS]`.
- **5. Target Personas:** `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-06 (CISO, DPO & Regulatory Compliance Auditor)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED]: fallback tiers trigger only on hard HTTP 429/5xx errors or open circuit breakers, lacking proactive queue-depth, in-flight concurrency, or `TTFT`-SLO saturation burst triggers.
- **7. Proposed Complete Functional Behavior:** Add configurable saturation burst triggers (`max_inflight_requests`, `max_waiting_queue`, `burst_ttft_threshold_ms`, plus optional ingestion of `vllm:num_requests_waiting` / `GAIE` load headers) on `ON_PREM` primary targets [DESIGN_PROPOSAL]. When saturated, overflow eligible requests to secondary cloud deployments **if and only if** the secondary target's `DataRegion` satisfies the effective request/route/project/org `AllowedDataRegions` and dedicated `cloud_burst_budget` headroom remains.
- **8. User-Facing Subfeatures:**
  - Proactive saturation burst thresholds (`max_inflight`, `burst_ttft_ms`, GPU queue depth) on self-hosted deployments.
  - Non-bypassable residency guard: bursts only to regions permitted by effective policy (`ON_PREM`-only policies never burst externally).
  - Optional dedicated daily/monthly `Cloud Burst Budget Cap` preventing runaway variable cloud bills during sustained local GPU maintenance.
  - FinOps On-Prem vs Cloud-Burst traffic split and cost-avoidance analytics.
- **9. End-to-End Lifecycle:** Configure `ON_PREM` GPU pool (Tier 1) + regional cloud target (Tier 2) + burst thresholds -> normal load stays 100% on-prem -> spike breaches `max_inflight` or `burst_ttft_ms` -> Resolver spills excess calls to compliant Tier 2 -> automatically drains back to 100% on-prem as local queue subsides.
- **10. Primary & Alternative User Journeys:** Primary: Route allows `[ON_PREM, EU_CENTRAL_1]`; peak load overflows to Frankfurt Bedrock with zero errors. Alternative: Caller passes `X-Tanpopo-Data-Residency: ON_PREM` (or parent policy restricts to `ON_PREM`); Tier 2 cloud target is pruned at Step 2 and never receives burst traffic.
- **11. Administrative & Permission Boundaries:** Adding external cloud burst targets to a route requires `ORG_OWNER` / `OPERATOR` / `BU_ADMIN` within ancestor residency policies (`SECURITY_REVIEWER` auditable).
- **12. Automation Opportunities:** Automatically polls or reads passive response-header queue metrics from `vLLM` / `SGLang` / `llm-d` adapters (`@tanpopo/vllm-llmd`, FEAT-010).
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Cluster-coordinated in-flight counters via Valkey, damping hysteresis to prevent burst flapping, and strict fail-closed `503` when `ON_PREM` is mandatory and local GPUs are saturated.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Evaluates real-time local GPU queue/concurrency/TTFT saturation signals inline during Route Resolution while strictly enforcing Step 2 residency fences.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Hybrid Burst Policy section in `RoutingPage.tsx` and On-Prem vs Cloud Burst chart in `FinOpsPage.tsx`.
  - *CLI:* `tanpopo route update --burst-max-inflight 64 --burst-ttft-ms 1500`.
  - *Admin / Data-Plane API:* `burst_policy` configuration on `/admin/v1/routes` and `/admin/v1/deployments`.
  - *GitOps / Terraform / Packs:* Declarative hybrid burst thresholds and cloud-burst budget references in route YAML.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-010 (`@tanpopo/vllm-llmd`), FEAT-024 (monotonic residency narrowing), FEAT-026 (`TTFT` EWMA), and FEAT-069 (residency fences).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** If the cloud burst budget is exhausted or cloud target is unhealthy, traffic continues queueing/serving on the local GPU pool up to hard timeout rather than failing healthy local capacity.
- **18. Security, Privacy & Financial Implications:** Guarantees `ON_PREM`-classified prompts never leave internal networks while capping unexpected cloud burst expenditure via FinOps holds.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Exceeding configured `max_inflight_requests` or `burst_ttft_ms` on an `ON_PREM` primary smoothly overflows excess calls to an allowed regional cloud target.
  - Requests with effective `AllowedDataRegions == [ON_PREM]` never burst to external cloud endpoints under any load or local outage condition.
  - Exhausting a configured cloud-burst budget halts external bursting while keeping local `ON_PREM` serving active.
- **20. Relevant Evidence & Research Confidence:** MEDIUM-HIGH — Enterprise hybrid GPU orchestration requirements are verified across `vLLM` / `llm-d` / `SGLang` deployments (SRC-031..033); `>80%` GPU utilization projection is `[HYPOTHESIS]`.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — Strategic hybrid-cloud capability letting enterprises run baseline inference on owned internal GPUs at high utilization while absorbing peak daytime spikes on sovereign cloud APIs without breaching residency rules. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `v1.1 (Core)`)*

---

### FEAT-031 — Automatic Cross-Dialect Failover Adaptation Under Zero-Silent-Downgrade (CT-08)

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-031` · `CAP` Traceability: `CAP-030` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-04` (Routing and Orchestration) -> **Cross-Provider Failover Resilience**
- **3. Plain-Language Product Summary:** Allows applications using Anthropic's `/v1/messages`, OpenAI's `/v1/responses`, or `/v1/chat/completions` to fail over automatically to a different cloud provider (such as AWS Bedrock, Google Vertex/Gemini, or Microsoft Foundry) during an outage—translating request and streaming response formats on the fly while refusing to silently drop required features like structured JSON schemas or tool calls.
  - *Practical Example:* Claude Code sends streaming `POST /v1/messages` to a production coding route; when Anthropic Direct returns `HTTP 529 Overloaded`, Tanpopo fails over pre-first-byte to an AWS Bedrock Converse deployment (or OpenAI `/v1/responses` fallback if configured), translating the upstream stream back into valid Anthropic SSE `content_block_delta` frames seamlessly.
  - *Explicit Trade-Offs:* Not every vendor wire feature is 100% bijective (for example, vendor-specific web-search tool blocks vs standard function tools); Tanpopo classifies translations into Tier 1/2 lossless vs Tier 3 opt-in policy adaptation vs Tier 4 hard incompatibility (`CT-08` fail-closed), avoiding magic silent degradation.
- **4. User Problem & Business Value:** In RC9, `/v1/chat/completions` fails over across chat adapters, whereas `/v1/responses` and native vendor SDK surfaces (`/v1/messages`, `/v1beta/models/*`) cannot fail over across heterogeneous provider dialects.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in `internal/routing/failover/executor.go` and `internal/ingress/*` [SOURCE_VERIFIED]: pre-first-byte failover works for `/v1/chat/completions`, but `/v1/responses` is single-vendor OpenAI-only and top-level `/v1/messages` / `/v1beta/models/*` cross-adapter streaming translation is absent.
- **7. Proposed Complete Functional Behavior:** Normalize `/v1/chat/completions`, `/v1/responses`, `/v1/messages`, and `/v1beta/models/*` through a canonical internal turn/tool/reasoning/usage contract (`TEEP-03` IR design deferred) so pre-first-byte failover adapts wire formats bidirectionally across OpenAI, Anthropic, Gemini, Bedrock, Vertex, Foundry, and vLLM [DESIGN_PROPOSAL]. Enforce strict `CT-08` capability pruning so incompatible fallback targets are skipped (or fail loudly) unless explicit Tier-3 lossy adaptation is enabled on the RoutePolicy, logging every adaptation step in `request_traces_meta`.
- **8. User-Facing Subfeatures:**
  - Bidirectional pre-first-byte failover across `/v1/messages`, `/v1/responses`, `/v1/chat/completions`, and `/v1beta/models/*`.
  - 4-Tier Translation Matrix enforcement: lossless auto-adaptation by default, explicit route opt-in for benign normalization, and fail-closed `CT-08` on unmappable semantics.
  - Automatic Phase-1 budget hold adjustment (`DeltaReserve` / difference check) when a fallback target has a higher RateCard than the failed primary.
  - Detailed failover attempt chain and dialect-adaptation trace spans in `TracesPage`.
- **9. End-to-End Lifecycle:** Inbound dialect parsed -> Resolver filters targets supporting required capabilities & translation tier -> Primary attempt fails pre-first-byte (429/5xx/TTFT timeout) -> Failover executor verifies/adjusts budget hold for Fallback #1 -> adapts outbound request & translates inbound SSE back to caller's exact wire dialect.
- **10. Primary & Alternative User Journeys:** Primary: `/v1/responses` call fails on OpenAI `503` and succeeds transparently on Anthropic or Bedrock ConverseStream, returning valid `response.output_item.*` SSE events. Alternative: Fallback #1 costs more than remaining project budget; Failover executor rejects unbudgeted escalation cleanly (`402 budget_exceeded`) or tries cheaper Fallback #2.
- **11. Administrative & Permission Boundaries:** Strict `CT-08` (`disallow_lossy_adaptation=true`) is enforced by default; enabling Tier-3 adaptation (such as clamping unsupported `top_k` or re-reasoning across vendors) requires explicit `TEAM_ADMIN` / `ORG_OWNER` opt-in on `RoutePolicy`.
- **12. Automation Opportunities:** `Route Explain` and the Route Editor automatically preview translation compatibility across mixed-vendor primary/fallback chains at configuration time.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Zero mid-stream failover after byte #1 has flushed to client, exact FinOps hold adjustment across differently priced fallback models, and full auditability of dialect conversions.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Pre-first-byte failover orchestration, canonical turn/tool intermediate adaptation, and `CT-08` capability verification run inside the Core Data Plane failover executor.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Cross-Dialect Compatibility indicator in `RoutingPage` and per-attempt failover timeline in `TracesPage`.
  - *CLI:* Cross-vendor failover chaos drills in `tanpopo chaos` / `smoke-test`.
  - *Admin / Data-Plane API:* `translation_policy` on `/admin/v1/routes` and attempt metadata in `/admin/v1/traces`.
  - *GitOps / Terraform / Packs:* Declarative `translation_policy` (`STRICT_BIJECTIVE` vs `ALLOW_BENIGN_COERCION`) in route YAML.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-002 (`/v1/messages`, `/v1beta/models/*`), FEAT-003 (`/v1/responses`), FEAT-005 (reasoning signature cross-vendor safety), and FEAT-037 (budget adjustment).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Never emits mixed-dialect SSE frames; if all compliant/compatible targets fail pre-first-byte, releases budget holds and returns an error formatted in the caller's original inbound SDK dialect.
- **18. Security, Privacy & Financial Implications:** Prevents failover from bypassing JSON-schema / safety parameters (`CT-08`) or silently exceeding tenant budgets when failing over from a low-cost model to a higher-cost frontier fallback.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Streaming `POST /v1/messages` and `POST /v1/responses` calls survive injected HTTP 503/529 primary failures by failing over across OpenAI, Anthropic, Gemini, and Bedrock.
  - Failing over from a cheap primary to a pricier fallback verifies sufficient FinOps budget hold before dispatching upstream.
  - Unsupported cross-dialect parameters fail closed per `CT-08` when `STRICT_BIJECTIVE` policy is active.
- **20. Relevant Evidence & Research Confidence:** HIGH — Grounded in the 4-Tier Protocol Translation Matrix verified in `05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md` §1.5 and RC9's existing `failover/executor.go` [SOURCE_VERIFIED]; internal IR structs are deferred to `TEEP-03`.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — True multi-vendor outage survivability requires failing over not only `/v1/chat/completions` but also `/v1/messages` and `/v1/responses` across Anthropic, OpenAI, Gemini, Bedrock, and Foundry without breaking caller wire contracts or silently dropping safety/schema features. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

## `DOM-05` — Reliability Engineering (`5` Proposed Features)

- **Domain Overview:** Reliability Engineering (DOM-05) protects downstream applications and multi-tenant gateway clusters against upstream provider brownouts, silent GPU hangs, hung SSE streams, noisy-neighbor traffic spikes, p99 tail-TTFT stalls, and rolling-restart connection drops. Every resilience mechanism is integrated with Tanpopo's FinOps and security invariants so retries, hedges, and timeouts never trigger unbudgeted spend or leak partial state.
- **Existing RC9 Baseline Posture:** RC9 implements per-node SM-04 circuit breakers, sequential pre-first-byte failover, and per-Virtual-Key token/request rate buckets [RUNTIME_VERIFIED]. Critical gaps were proven in TEEP-01: route_policies.request_timeout_ms, X-Tanpopo-Timeout-Ms, and failover.Config.RequestDeadline are persisted/declared but completely unwired at runtime (FND-004) [SOURCE_VERIFIED]; circuit breakers are node-local RAM only and cannot detect silent GPU generation stalls where /health returns 200 OK [SOURCE_VERIFIED]; aggregate Org/Project bulkheads and priority load shedding are missing; and benchmark-evidence.md recorded 1/575 (0.17%) dropped requests during rolling restarts (COND-PO-01) [RUNTIME_VERIFIED].
- **Progressive Self-Hosted to Enterprise Experience:** Self-hosters benefit immediately from sensible default request, TTFT, and stalled-chunk timeouts plus automatic failover when an API provider returns 429 or 5xx errors. Multi-tenant enterprises add cluster-coordinated circuit breakers with semantic GPU canary probes, hierarchical Org/BU/Project bulkheads with priority-class load shedding, opt-in dual-budget-held TTFT hedging for mission-critical voice/interactive routes, and zero-drop Kubernetes rolling-upgrade readiness fencing.
- **Technical Selections Deferred to `TEEP-03`:** Valkey Pub/Sub circuit-breaker state synchronization wire protocol, exact adaptive concurrency control math (Gradient2 vs Vegas/AIMD variants), and Lua script sharding for hierarchical tenant rate bulkheads are deferred to TEEP-03.

| ID | Subdomain | Feature Name | Class | Disposition | Priority | Placement Candidate |
|---|---|---|---|---|---|---|
| `FEAT-032` | Cluster Circuit Breakers & Stall Detection | **Cluster-Coordinated Circuit Breakers (SM-04), Retry-After Cooldowns & Semantic Stall Probing** | `CAT-ADMIN` | `IMPROVE` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |
| `FEAT-033` | Deterministic Timeout & Deadline Governance | **End-to-End Request, First-Token (TTFT), Idle-Chunk & X-Tanpopo-Timeout-Ms Deadlines** | `CAT-PROD` | `IMPROVE` | `P0-Foundational` | `CORE_DATA_PLANE` |
| `FEAT-034` | Multi-Tenant Isolation & Load Shedding | **Hierarchical Tenant Rate/Token/Byte Bulkheads & Priority Concurrency Load Shedding** | `CAT-ADMIN` | `ADD` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |
| `FEAT-035` | Tail-Latency Elimination | **Budget-Safe Pre-First-Token Speculative Request Hedging** | `CAT-PROD` | `ADD` | `P2-Differentiator` | `CORE_DATA_PLANE` |
| `FEAT-036` | Zero-Downtime Operations & Lifecycle | **Zero-Drop Rolling Upgrade Readiness Fencing & Active Stream Drain** | `CAT-ADMIN` | `IMPROVE` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |

### FEAT-032 — Cluster-Coordinated Circuit Breakers (SM-04), Retry-After Cooldowns & Semantic Stall Probing

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-032` · `CAP` Traceability: `CAP-031` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-05` (Reliability Engineering) -> **Cluster Circuit Breakers & Stall Detection**
- **3. Plain-Language Product Summary:** Synchronizes provider health circuit breakers and rate-limit cooldown timers across every gateway node in a cluster so when one node detects an upstream outage or `Retry-After` throttle, all sibling nodes stop hammering the broken endpoint immediately. For self-hosted GPU pools, optional micro-generation probes detect silent GPU freezes where basic health endpoints falsely report 'OK'.
  - *Practical Example:* In a 12-pod Kubernetes deployment, Pod #1 receives an HTTP 429 with `Retry-After: 30` from an Anthropic key and broadcasts `THROTTLED_COOLDOWN` over Valkey; Pods #2–#12 immediately route around that key for 30s instead of burning 110 wasted failed requests, while a 1-token canary probe catches a wedged `SGLang` worker whose `/health` route still returned `200 OK`.
  - *Explicit Trade-Offs:* Active semantic generation probes consume tiny GPU compute (`max_tokens: 1`), so they are disabled by default on paid commercial per-token APIs and enabled opt-in (or triggered passively on elevated TTFT timeouts) for self-hosted `vLLM`/`SGLang`/`llm-d` pools.
- **4. User Problem & Business Value:** In RC9, `circuit_breaker.go` maintains node-local RAM windows only (`N` pods multiply outage blast radius by `N`), and real-world incidents like `sglang#51980` (`2026-11-21` NIXL disaggregated serving hang [SOURCE_VERIFIED, SRC-033]) prove `/health` 200 checks miss silent generation stalls.
- **5. Target Personas:** `PER-03 (Enterprise AI Platform Architect / SRE)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in `internal/routing/breaker/circuit_breaker.go` [SOURCE_VERIFIED]: implements `SM-04` states (`CLOSED`, `OPEN`, `HALF_OPEN`, `THROTTLED_COOLDOWN`) locally per process, but state transitions are not shared across Valkey pods and semantic generation stall probes are absent.
- **7. Proposed Complete Functional Behavior:** Propagate `SM-04` state transitions (`CIRCUIT_OPEN_UNHEALTHY`, `THROTTLED_COOLDOWN` until `cooldown_until_ms` parsed from `Retry-After` / `x-ratelimit-reset-*`) across Data-Plane pods via Valkey Pub/Sub + shared state keys (`TEEP-03` wire schema deferred), degrading cleanly to node-local RAM if Valkey is unreachable [DESIGN_PROPOSAL]. Add configurable lightweight `max_tokens: 1` semantic stall probes for self-hosted GPU deployments plus manual admin 'Trip / Reset Breaker' overrides in Console and CLI.
- **8. User-Facing Subfeatures:**
  - Cluster-wide `SM-04` circuit-breaker and `Retry-After` / `x-ratelimit-reset` cooldown synchronization across HA pods.
  - Opt-in `max_tokens: 1` semantic token-generation canary probes catching silent `vLLM` / `SGLang` / `llm-d` hangs.
  - Live per-deployment breaker state, trip reason, and cooldown countdown badges in `ProvidersPage` & `RoutingPage`.
  - Manual operator 'Force Open (Isolate)' and 'Reset Half-Open Probe' controls for incident triage.
- **9. End-to-End Lifecycle:** Pod observes threshold 5xx/TTFT stalls or `Retry-After` header -> transitions local `SM-04` state & publishes cluster event -> all pods exclude target until cooldown/half-open window -> single coordinated `HALF_OPEN` probe (or semantic 1-token canary) verifies recovery -> broadcasts `CLOSED`.
- **10. Primary & Alternative User Journeys:** Primary: Upstream returns `429 Retry-After: 15`; entire gateway fleet pauses dispatch to that deployment for 15s and routes seamlessly to Tier-2 fallback. Alternative: Valkey cluster restarts; pods seamlessly continue enforcing node-local `SM-04` breakers without blocking inference.
- **11. Administrative & Permission Boundaries:** Breaker thresholds configured per route/deployment by `OPERATOR` / `ORG_OWNER` / `TEAM_ADMIN`; manual breaker trip/reset requires `ActionDeploymentWrite` and emits an audit log entry.
- **12. Automation Opportunities:** Coordinated single-pod `HALF_OPEN` probing prevents all `N` pods from simultaneously stampeding a newly recovering upstream endpoint.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Graceful fallback when Valkey is unavailable, bounded probe token/rate limits (`0` outage amplification), and Prometheus `tanpopo_breaker_state` + transition counters.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Executes inside the Data Plane SM-04 state machine, failover executor, and Valkey cluster synchronization bus.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Live Circuit Breaker & Cooldown status pills and Reset/Isolate actions in `RoutingPage` and `ProvidersPage`.
  - *CLI:* `tanpopo deployment breaker-status | breaker-trip | breaker-reset <deployment-id>`.
  - *Admin / Data-Plane API:* Breaker telemetry and state-override actions on `/admin/v1/deployments/{id}`.
  - *GitOps / Terraform / Packs:* Declarative `circuit_breaker` failure-rate, window, and semantic-probe settings in YAML.
- **16. Dependencies & Cross-Domain Relationships:** `Integrates with FEAT-010 (`@tanpopo/vllm-llmd`), FEAT-024 (Step 5 health pruning), and FEAT-095 (Valkey degradation visibility).` · Related Decisions: None (Standard Catalog Review)
- **17. Failure & Recovery Behavior:** Never fails closed across healthy sibling deployments when Valkey is down; coordinated `HALF_OPEN` exponential backoff prevents rapid open/close flapping.
- **18. Security, Privacy & Financial Implications:** Semantic probes send fixed synthetic 1-token prompts (`0` customer data) and default off on paid commercial endpoints to avoid unexpected probe charges.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Tripping a deployment circuit breaker or `Retry-After` cooldown on Pod A propagates to Pod B in `< 100 ms` `[HYPOTHESIS]` when Valkey is healthy.
  - A mocked GPU endpoint returning `200 OK` on `/health` while hanging `/v1/chat/completions` is isolated by semantic stall probing.
  - Killing Valkey degrades breaker enforcement cleanly to node-local RAM with zero panics or dropped healthy requests.
- **20. Relevant Evidence & Research Confidence:** HIGH — Node-local-only `circuit_breaker.go` is [SOURCE_VERIFIED] on ac9ae87a; silent GPU `/health` 200 stalls are [SOURCE_VERIFIED] in `sglang#51980` (SRC-033).
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Prevents N gateway pods from independently sending N*threshold failing requests into a brownout upstream and catches silent self-hosted GPU KV/NIXL hangs where HTTP /health still returns 200 OK. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-033 — End-to-End Request, First-Token (TTFT), Idle-Chunk & X-Tanpopo-Timeout-Ms Deadlines

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-033` · `CAP` Traceability: `CAP-032` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-05` (Reliability Engineering) -> **Deterministic Timeout & Deadline Governance**
- **3. Plain-Language Product Summary:** Wires and enforces three distinct safety timers on every AI request: how long to wait for the very first token (`TTFT` timeout), how long a streaming answer can pause between chunks (`idle-chunk` timeout), and the maximum total request duration. Callers can also pass a tighter per-request timeout header (`X-Tanpopo-Timeout-Ms`) so hung providers never tie up server resources or lock budgets.
  - *Practical Example:* A customer-facing chat app sets `ttft_timeout_ms: 4000`, `idle_chunk_timeout_ms: 10000`, and `request_timeout_ms: 90000` on its route; if Primary OpenAI hangs silently before emitting a single token at 4.0s, Tanpopo cancels the socket immediately and fails over to Anthropic with 86s of total budget deadline remaining.
  - *Explicit Trade-Offs:* Deep reasoning models (`o3`/`GPT-5.4`/`Claude Extended Thinking`) can legitimately pause 20–60s before emitting visible text if reasoning summaries/heartbeats are disabled; Model Intelligence metadata therefore supplies higher default `ttft_timeout_ms` profiles for reasoning deployments so operators do not trigger premature failovers.
- **4. User Problem & Business Value:** In RC9 (`FND-004`), operators configuring `request_timeout_ms` on routes or developers sending documented `X-Tanpopo-Timeout-Ms` headers received zero enforcement because neither value was loaded into `snapshot.RoutePolicyView` or ingress context deadlines [SOURCE_VERIFIED].
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`
- **6. Existing RC9 Behavior & Limitations:** ENUM_OR_CONTRACT_ONLY in RC9 (`FND-004`) [SOURCE_VERIFIED]: `route_policies.request_timeout_ms` (`routes.go:264`), `X-Tanpopo-Timeout-Ms` (`dataplane-v1.yaml:81`), and `failover.Config.RequestDeadline` (`executor.go:61`) exist in schema/spec/code but have zero runtime wiring in `ConfigSnapshot` or ingress.
- **7. Proposed Complete Functional Behavior:** Wire `request_timeout_ms`, `ttft_timeout_ms`, and `idle_chunk_timeout_ms` end-to-end through SQL, Admin API, `ConfigSnapshot`, failover executor, and `sse.Pump` [DESIGN_PROPOSAL]. Parse `X-Tanpopo-Timeout-Ms` monotonically (`effective_deadline = min(route_ceiling, client_header)`—clients can tighten but never exceed route/gateway ceilings), trigger pre-first-byte failover on `ttft_timeout_ms` expiry, and abort + settle cleanly on mid-stream `idle_chunk_timeout_ms` or total deadline expiry.
- **8. User-Facing Subfeatures:**
  - End-to-end wired `request_timeout_ms` (total wall-clock deadline) per RoutePolicy and Deployment.
  - Dedicated `ttft_timeout_ms` (Time-To-First-Token deadline) triggering fast pre-first-byte failover on silent upstream queue stalls.
  - Dedicated `idle_chunk_timeout_ms` detecting mid-stream hung sockets and closing streams cleanly via FMEA-01 error frames.
  - Monotonic `X-Tanpopo-Timeout-Ms` request header allowing latency-sensitive callers to tighten per-call deadlines.
- **9. End-to-End Lifecycle:** Route/Deployment deadlines loaded into `ConfigSnapshot` -> Ingress derives request context deadline clamped by `X-Tanpopo-Timeout-Ms` -> Per-attempt `ttft_timeout_ms` timer armed until byte #1 -> `sse.Pump` resets `idle_chunk_timeout_ms` on every upstream frame -> On expiry, upstream socket closes immediately and `SM-07` hold settles/releases.
- **10. Primary & Alternative User Journeys:** Primary: Primary target stalls 3.5s (`> ttft_timeout_ms: 3000`); attempt #1 cancels cleanly, holds re-verify, and Fallback #1 completes within remaining `request_timeout_ms`. Alternative: Client passes `X-Tanpopo-Timeout-Ms: 600000` on a route capped at `60000 ms`; gateway clamps effective deadline to `60000 ms`.
- **11. Administrative & Permission Boundaries:** Route and deployment ceiling timeouts are managed by `TEAM_ADMIN` / `ORG_OWNER` / `OPERATOR`; Data Plane headers can only reduce, never extend, configured ceilings.
- **12. Automation Opportunities:** Model Intelligence Packs automatically suggest appropriate `ttft_timeout_ms` defaults (e.g., `5,000 ms` for standard chat vs `60,000 ms` for deep reasoning models).
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Immediate upstream TCP/HTTP2 stream cancellation on timeout (`0` leaked goroutines), deterministic FinOps hold cleanup without waiting for the 90s orphan reaper, and distinct timeout error codes in traces.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Context deadline derivation, pre-first-token timer cancellation, and inter-chunk SSE stall timers must execute directly inside Data Plane ingress, failover, and `sse.Pump`.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Total, TTFT, and Idle-Chunk Timeout inputs in `RoutingPage.tsx` and `ModelsPage.tsx` drawers.
  - *CLI:* `tanpopo route create/update --timeout-ms 60000 --ttft-timeout-ms 4000 --idle-chunk-timeout-ms 15000`.
  - *Admin / Data-Plane API:* `request_timeout_ms`, `ttft_timeout_ms`, `idle_chunk_timeout_ms` in `admin-v1.yaml`; `X-Tanpopo-Timeout-Ms` in `dataplane-v1.yaml`.
  - *GitOps / Terraform / Packs:* Declarative timeout fields on `route_policy` and `model_deployment` manifests.
- **16. Dependencies & Cross-Domain Relationships:** `Closes `FND-004` (`request_timeout_ms` unwired gap) and supports FEAT-007 (SSE error framing) and FEAT-035 (TTFT hedging).` · Related Decisions: None (Standard Catalog Review)
- **17. Failure & Recovery Behavior:** Distinguishes `UPSTREAM_TTFT_TIMEOUT` (eligible for failover & breaker accounting) from `CLIENT_DEADLINE_EXCEEDED` (tight `X-Tanpopo-Timeout-Ms` budget that must not falsely trip shared upstream circuit breakers!).
- **18. Security, Privacy & Financial Implications:** Protects gateway worker pools against slowloris-style hung upstream connections and releases unused Phase-1 budget holds immediately upon timeout.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Setting `request_timeout_ms`, `ttft_timeout_ms`, and `idle_chunk_timeout_ms` on a route deterministically aborts mocked stalled upstreams at the configured millisecond bounds.
  - `X-Tanpopo-Timeout-Ms` enforces tighter client deadlines while clamping values exceeding the route ceiling.
  - An aggressive client `X-Tanpopo-Timeout-Ms: 50` timeout does not trip the shared deployment `SM-04` circuit breaker for other tenants.
- **20. Relevant Evidence & Research Confidence:** HIGH — Unwired RC9 fields are [SOURCE_VERIFIED] at `routes.go:264`, `dataplane-v1.yaml:81`, and `executor.go:61` on commit ac9ae87a.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Fixes a critical unwired runtime gap (`FND-004`) where `route_policies.request_timeout_ms`, `X-Tanpopo-Timeout-Ms`, and `failover.Config.RequestDeadline` exist in SQL/OpenAPI/Go structs on RC9 with zero production execution path. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-034 — Hierarchical Tenant Rate/Token/Byte Bulkheads & Priority Concurrency Load Shedding

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-034` · `CAP` Traceability: `CAP-033` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-05` (Reliability Engineering) -> **Multi-Tenant Isolation & Load Shedding**
- **3. Plain-Language Product Summary:** Prevents 'noisy neighbor' outages in shared enterprise gateways by enforcing aggregate request, token, and payload-byte limits at the Organization, Business Unit, and Project levels—not just per API key. When upstream capacity gets congested, priority tiers automatically shed low-priority background batch calls first so mission-critical customer and executive workloads stay fast.
  - *Practical Example:* An analytics intern launches a parallel script across 15 project Virtual Keys sending 2-megabyte RAG contexts; Project-level TPM and `max_inflight_input_bytes` bulkheads throttle the runaway batch cleanly with `429 Retry-After` while keeping the Customer Support BU's `MISSION_CRITICAL` routes unaffected.
  - *Explicit Trade-Offs:* Checking 4 hierarchical Valkey buckets (`ORG -> ORG_UNIT -> PROJECT -> VKEY`) naively in serial would add network RTTs; Tanpopo co-locates tenant keys via Valkey `{org_id}` hash tags so a single pipelined Lua evaluation checks the entire hierarchy in one round-trip (`TEEP-03` Lua layout deferred).
- **4. User Problem & Business Value:** Closes RC9's multi-key rate-limit multiplication loophole (`internal/ratelimit` per-VKey only) and delivers predictable multi-tenant noisy-neighbor isolation across hundreds of engineering teams.
- **5. Target Personas:** `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-04 (Business Unit / Line-of-Business Delegated Admin)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in `internal/ratelimit` [SOURCE_VERIFIED]: enforces RPM/TPM token buckets strictly per `virtual_key_id`; lacks aggregate `ORG`, `ORG_UNIT`, or `PROJECT` rate/concurrency/inflight-byte bulkheads and lacks priority-class load shedding.
- **7. Proposed Complete Functional Behavior:** Add hierarchical rate, token, concurrent-stream, and `max_inflight_input_bytes` bulkheads across `ORG -> ORG_UNIT -> PROJECT -> VIRTUAL_KEY` co-located in Valkey `{org_id}` (with local RAM fallback in Mode A) [DESIGN_PROPOSAL]. Assign routes/keys a `priority_class` (`MISSION_CRITICAL`, `INTERACTIVE`, `BATCH_BACKFILL`) and apply adaptive concurrency load shedding (`TEEP-03` controller math deferred) that sheds `BATCH_BACKFILL` first when gateway or upstream queue latency inflates.
- **8. User-Facing Subfeatures:**
  - Aggregate RPM, TPM, concurrent-SSE-stream, and `max_inflight_input_bytes` quotas at Org, Business Unit, Project, and Key scopes.
  - 3-tier workload `priority_class` (`MISSION_CRITICAL > INTERACTIVE > BATCH_BACKFILL`) protecting live user traffic during brownouts.
  - Standardized `RateLimit-Limit`, `RateLimit-Remaining`, `RateLimit-Reset`, and `Retry-After` response headers identifying the exact tripped scope.
  - Per-tenant noisy-neighbor shed counters and live concurrency utilization panels in Console & Prometheus.
- **9. End-to-End Lifecycle:** Admin sets hierarchical quotas & route priority classes -> Ingress estimates input tokens/bytes & evaluates hierarchical bulkhead atomically -> admits or returns `429` before FinOps hold -> reconciles actual token consumption post-stream.
- **10. Primary & Alternative User Journeys:** Primary: Project hits its `500,000 TPM` ceiling across 12 keys; 13th key receives `HTTP 429` with `X-Tanpopo-RateLimit-Scope: PROJECT` while sibling projects continue at `< 0.5 ms` p99 overhead delta `[HYPOTHESIS]`. Alternative: Upstream GPU latency spikes 3x; adaptive shedding pauses `BATCH_BACKFILL` requests (`503 Retry-After`) while `MISSION_CRITICAL` calls proceed.
- **11. Administrative & Permission Boundaries:** Child `PROJECT` or `VIRTUAL_KEY` rate limits cannot exceed parent `ORG` / `ORG_UNIT` ceilings; priority class `MISSION_CRITICAL` can be restricted by `ORG_OWNER` / `BU_ADMIN` policy.
- **12. Automation Opportunities:** Post-response token reconciliation refunds overestimated `max_tokens` reservations back into the rolling TPM bucket when short answers finish early.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Single-RTT `{org_id}` Valkey hash-tag evaluation in HA clusters, lock-free sharded buckets in standalone Mode A, and bounded cardinality on rejection metrics.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Multi-level token/request/byte admission buckets and adaptive concurrency shedding must execute at the Data Plane ingress bulkhead before expensive transform or upstream I/O.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Quotas & Bulkheads tab in Org/BU/Project Settings and `Priority Class` selector in `RoutingPage` / `VirtualKeysPage`.
  - *CLI:* `tanpopo policy set-quotas --scope project:<id> --tpm 500000 --max-concurrent-streams 100`.
  - *Admin / Data-Plane API:* Quota bulkhead fields on `/admin/v1/policies` and `RateLimit-*` headers in `dataplane-v1.yaml`.
  - *GitOps / Terraform / Packs:* Declarative hierarchical rate/byte bulkheads and `priority_class` assignments.
- **16. Dependencies & Cross-Domain Relationships:** `Integrates with FEAT-016 (PTU priority headroom), FEAT-066 (`org_units` hierarchy), and FEAT-067 (Virtual Keys).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** If Valkey becomes unreachable, falls back automatically to proportional per-pod in-RAM token buckets (`configured_limit / active_dp_pods`) rather than failing open to unlimited floods or failing closed.
- **18. Security, Privacy & Financial Implications:** `max_inflight_input_bytes` prevents memory-exhaustion DoS via concurrent multi-megabyte attachments, while aggregate TPM caps provide a fast first line of defense ahead of monetary budgets.
- **19. Preliminary Measurable Acceptance Criteria:**
  - 20 Virtual Keys inside one Project cannot collectively exceed the Project's aggregate RPM, TPM, or concurrent-stream bulkhead.
  - Under simulated upstream congestion, `BATCH_BACKFILL` traffic sheds (`429`/`503`) while `MISSION_CRITICAL` traffic maintains >=99% success.
  - 429 responses include accurate `Retry-After` and `X-Tanpopo-RateLimit-Scope` headers without leaking cross-tenant limits.
- **20. Relevant Evidence & Research Confidence:** MEDIUM-HIGH — RC9 per-key-only limitation is [SOURCE_VERIFIED] in `internal/ratelimit`; `< 0.5 ms` p99 isolation delta is `[HYPOTHESIS]` grounded in Envoy Gradient2 / shuffle-sharding patterns (`QAS-ENT-04`).
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — RC9 rate-limits only individual Virtual Keys—allowing a batch script rotating across 20 keys in one project to starve sibling interactive teams and exhaust shared org/provider TPM quotas. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-035 — Budget-Safe Pre-First-Token Speculative Request Hedging

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-035` · `CAP` Traceability: `CAP-034` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: `INN-07`
- **2. Functional Domain & Subdomain:** `DOM-05` (Reliability Engineering) -> **Tail-Latency Elimination**
- **3. Plain-Language Product Summary:** Eliminates frustrating tail-latency freezes on high-priority interactive AI routes: if the primary provider is unusually slow to produce its first token, Tanpopo dispatches a parallel backup request to a secondary provider, streams whichever responds first, and cancels the slower one immediately. Crucially, it only fires the backup request if your budget can safely cover both holds up front—preventing unbudgeted double spend.
  - *Practical Example:* A live customer-service voice assistant enables speculative hedging at `1.5x p95 TTFT` (~900 ms); when an OpenAI shard experiences a rare 8-second prefill stall, Tanpopo reserves the incremental hedge hold at 900 ms, fires a parallel call to Azure Foundry (which emits token #1 in 280 ms), cancels the stalled OpenAI socket, and releases the unused hold.
  - *Explicit Trade-Offs:* Honest operational trade-offs: (1) hedging requires temporary `hold(Primary) + hold(Hedge)` budget headroom during the brief race window; (2) if the canceled loser already started upstream GPU prefill before TCP FIN/RST arrived, the vendor may still bill input tokens on their invoice (mitigated by firing hedges only past `p95 * 1.5`, capping route hedge ratio at e.g. `<= 5%` of calls, and tracking hedged cancellations in FinOps); and (3) hedging is strictly forbidden on mutating native/MCP/A2A endpoints.
- **4. User Problem & Business Value:** Commercial LLM APIs exhibit 5x–15x p99 TTFT spikes during regional load bursts, while generic proxy hedging (e.g., Envoy retry/hedging) has zero awareness of token-budget holds and can blow past tenant spend caps.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED]: `internal/routing/failover/executor.go` executes strictly sequential attempts after primary error/timeout.
- **7. Proposed Complete Functional Behavior:** Implement opt-in `INN-07` Speculative Pre-First-Token Hedging on non-mutating inference routes [DESIGN_PROPOSAL]: when Primary emits `0` bytes by `max(min_hedge_delay_ms, p95_ttft * multiplier)` AND route rolling hedge-rate budget (`<= max_hedge_pct`, default `5%`) permits, atomically reserve `hold(Hedge)` alongside `hold(Primary)`. Lock the winner atomically (`atomic.Bool` compare-and-swap) on the first valid content/reasoning SSE byte, cancel the loser's `context.Context` immediately (targeting `< 1 ms` local cancellation `[HYPOTHESIS]`), and release/settle both holds accurately.
- **8. User-Facing Subfeatures:**
  - Opt-in per-route Speculative Hedging policy (`hedge_delay_ms` or adaptive `p95_ttft * 1.5` + `max_hedge_rate_pct` cap).
  - Atomic dual-hold FinOps gate (`hold(Primary) + hold(Hedge) <= AvailableBudget`) skipping the hedge cleanly if budget is tight.
  - Race-free first-token winner lock ensuring zero mixed chunks ever reach the client stream.
  - Dedicated trace waterfall spans (`primary_attempt` vs `hedged_attempt`, winner badge, canceled loser duration) and FinOps hedge telemetry.
- **9. End-to-End Lifecycle:** Primary request dispatches with `hold(Primary)` -> `hedge_delay` timer fires with 0 bytes received -> hedge token-bucket (`<= 5%`) & Phase-1 `hold(Hedge)` checked -> Secondary request dispatches in parallel -> First responder wins CAS lock & streams to caller -> Loser canceled & `hold(Loser)` released (or partial usage settled if response header/usage arrived before close).
- **10. Primary & Alternative User Journeys:** Primary: Primary stalls 6s, Hedge wins in 300 ms; caller sees 1.2s total TTFT instead of 6s. Alternative: Tenant has only USD 0.02 remaining budget (enough for Primary, not Primary + Hedge); hedge check fails softly (`hedge_skipped_insufficient_budget`) and Primary continues normally without error.
- **11. Administrative & Permission Boundaries:** Disabled by default; requires explicit `ORG_OWNER` / `TEAM_ADMIN` enablement on latency-critical routes and can be globally prohibited by FinOps cost-conservation policies.
- **12. Automation Opportunities:** Adaptive `p95_ttft * 1.5` threshold tracks real deployment responsiveness (`FEAT-026`) so operators do not have to hand-tune static millisecond delays.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Rolling hedge-storm circuit breaker (`<= max_hedge_rate_pct` so a widespread outage triggers `SM-04` fast failover rather than 100% double traffic), strict idempotency/non-mutation check, and race-detector-clean Go concurrency.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Requires tight sub-millisecond coordination between the Data Plane failover executor, dual Phase-1 FinOps holds, and upstream stream cancellation.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Speculative TTFT Hedging toggle, delay mode, and max-hedge-% cap in `RoutingPage.tsx`; hedge win-rate KPI in `TracesPage`.
  - *CLI:* `tanpopo route update --enable-ttft-hedging --max-hedge-pct 5`.
  - *Admin / Data-Plane API:* `hedging_policy` object on `/admin/v1/routes` and hedge span metadata on `/admin/v1/traces`.
  - *GitOps / Terraform / Packs:* Declarative `hedging_policy` settings in route YAML.
- **16. Dependencies & Cross-Domain Relationships:** `Complements `INN-07` (FEAT-025), FEAT-026 (`TTFT` EWMA), FEAT-033 (deadlines), and FEAT-037 (two-phase FinOps holds).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** If both Primary and Hedge fail pre-first-byte, releases both holds cleanly and proceeds to Tier-3 fallback (if configured) or returns normalized upstream error.
- **18. Security, Privacy & Financial Implications:** Hedge target must pass the exact same Step 1–5 residency, BAA, ZDR, and capability filters as Primary, and dual-hold gating prevents budget ceiling overruns.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Under `go test -race`, 1,000 concurrent hedged SSE streams produce zero corrupted/interleaved frames and zero leaked goroutines.
  - When remaining budget covers `1x` request cost but `< 2x`, the hedge is skipped cleanly and the primary request still succeeds.
  - Sustained upstream slowness caps speculative hedges at `max_hedge_rate_pct` (preventing 2x traffic amplification).
- **20. Relevant Evidence & Research Confidence:** MEDIUM-HIGH — Architectural mechanism (`atomic` CAS + `SM-07` holds) is [SOURCE_VERIFIED] feasible atop RC9 primitives; `< 1 ms` local cancel and p99 TTFT reduction magnitudes are `[HYPOTHESIS]`.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — Slashes severe p99 tail-TTFT stalls on executive/customer-facing interactive and voice routes while preserving Tanpopo's strict zero-overspend FinOps invariant. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `v1.1 (Core)`)*

---

### FEAT-036 — Zero-Drop Rolling Upgrade Readiness Fencing & Active Stream Drain

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-036` · `CAP` Traceability: `CAP-036` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-05` (Reliability Engineering) -> **Zero-Downtime Operations & Lifecycle**
- **3. Plain-Language Product Summary:** Ensures routine Tanpopo software upgrades, configuration rollouts, and Kubernetes pod scaling never cut off users mid-sentence or drop incoming requests. When a gateway node prepares to stop, it immediately tells load balancers to stop sending new traffic, waits briefly for network routing tables to update, lets open AI streams finish naturally, and flushes all billing records before exiting.
  - *Practical Example:* An SRE runs `helm upgrade` across a 6-pod production cluster while 180 developers have 45-second Claude Code reasoning streams open; each terminating pod flips `/readyz` to `503 Draining`, absorbs 5 seconds of in-flight kube-proxy propagation without rejecting calls, waits 52s for open SSE streams to complete and settle, and exits with `0` dropped requests.
  - *Explicit Trade-Offs:* Waiting for long-lived reasoning streams slows pod termination from `<1s` to ~30–120s during deployments; an explicit `--drain-timeout` hard ceiling (e.g., default `120s`, max `300s`) ensures a stalled client can never block urgent security rollouts indefinitely.
- **4. User Problem & Business Value:** RC9 `docs/quality/benchmark-evidence.md` measured `1 / 575` (`0.17%`) dropped requests during rolling restarts (`COND-PO-01`) [RUNTIME_VERIFIED] because closing the listener immediately upon `SIGTERM` races against upstream load-balancer endpoint removal.
- **5. Target Personas:** `PER-03 (Enterprise AI Platform Architect / SRE)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION [RUNTIME_VERIFIED, SOURCE_VERIFIED]: graceful HTTP shutdown exists in `serve.go`, but lacks a configurable readiness-propagation delay window (`--prestop-wait`) between flipping `/readyz -> 503` and rejecting new connections, resulting in the `1/575` (`0.17%`) LB race recorded in `COND-PO-01`.
- **7. Proposed Complete Functional Behavior:** Implement a deterministic 4-phase graceful shutdown sequence on `SIGTERM`/`SIGINT` [DESIGN_PROPOSAL]: (1) immediately flip `/readyz` to `HTTP 503 {status: draining}` and emit `Connection: close` on idle HTTP/1.1 responses / HTTP/2 `GOAWAY`; (2) keep accepting in-flight LB requests during `--prestop-wait` (default `5s` in clustered HA, `0s` in local dev); (3) stop new intake and wait up to `--drain-timeout` (default `120s`) for `ActiveStreamRegistry` SSE/MCP/A2A streams to finish; and (4) flush group-commit settlement/trace batches (`ARCH-005`) and return unused pod budget leases (`ARCH-002`) before exit `0`.
- **8. User-Facing Subfeatures:**
  - 4-phase zero-drop shutdown (`Readiness 503 -> Propagation Grace -> Active SSE Stream Drain -> Ledger/Trace Flush`).
  - Documented `--prestop-wait` and `--drain-timeout` CLI flags and `TANPOPO_PRESTOP_WAIT` / `TANPOPO_DRAIN_TIMEOUT` env vars.
  - Live `draining` node state and active open-stream countdown visible in Cluster Topology Health (FEAT-095).
  - Reference Kubernetes Helm `preStop` / `terminationGracePeriodSeconds`, Envoy, and NGINX upstream drain configurations.
- **9. End-to-End Lifecycle:** Orchestrator sends `SIGTERM` -> Phase 1 `/readyz` 503 + `GOAWAY` -> Phase 2 `--prestop-wait` absorbs LB propagation lag -> Phase 3 `ActiveStreamRegistry` drains to `0` -> Phase 4 synchronous batch flush + budget lease return -> clean process exit.
- **10. Primary & Alternative User Journeys:** Primary: All open streams finish within 38s; pod flushes final settlements and exits cleanly at 43s with `0` errors. Alternative: One client holds a wedged stream past `--drain-timeout=120s`; at 120s, Tanpopo emits an `FMEA-01` `server_draining_abort` SSE error frame, settles partial tokens, and exits predictably.
- **11. Administrative & Permission Boundaries:** Configured at node startup by `OPERATOR` via CLI flags / Helm values; drain telemetry visible to `OPERATOR`.
- **12. Automation Opportunities:** Official Helm chart (FEAT-097) automatically sets `terminationGracePeriodSeconds >= prestopWait + drainTimeout + 15s` so Kubernetes never prematurely `SIGKILL`s a draining gateway pod.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Zero lost FinOps settlements (`ARCH-005` synchronous flush), immediate budget lease return (`ARCH-002`), and verified `0 / 5,000` dropped requests in CI rolling-restart chaos harnesses (`COND-PO-01` target `[HYPOTHESIS]`).
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Signal handling (`SIGTERM`), `/readyz` HTTP fencing, `preStop` propagation delay, active SSE stream tracking, and final settlement batch flushing execute inside the gateway server runtime.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* `DRAINING` node badge and in-flight stream counter in Infrastructure / Cluster Health page (FEAT-095).
  - *CLI:* `tanpopo serve --prestop-wait 5s --drain-timeout 120s`.
  - *Admin / Data-Plane API:* `GET /readyz` returning `503` with structured `draining` status while `GET /healthz` (liveness) stays `200`.
  - *GitOps / Terraform / Packs:* Helm chart `lifecycle.drain` values and Docker Compose `stop_grace_period` templates.
- **16. Dependencies & Cross-Domain Relationships:** `Closes `COND-PO-01` (`FND-018`) and coordinates with `ARCH-002` (pod lease return), `ARCH-005` (batch flush), FEAT-097 (Helm chart), and FEAT-123 (CLI flag parity).` · Related Decisions: None (Standard Catalog Review)
- **17. Failure & Recovery Behavior:** Keeping `/healthz` liveness `200 OK` during drain prevents kubelet from restarting the pod mid-drain while `/readyz` `503` removes it from Service endpoints.
- **18. Security, Privacy & Financial Implications:** Prevents orphaned `SM-07` budget holds and lost `usage_settlements` rows during routine cluster upgrades.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Rolling restart of a multi-replica HA cluster under continuous streaming load records `0` dropped requests (`COND-PO-01` verification).
  - 100% of settled streaming requests completing during the drain window are persisted to SQLite/PostgreSQL before process exit.
  - Streams exceeding `--drain-timeout` receive a clean `FMEA-01` error frame (never synthetic `[DONE]`) and settle partial holds.
- **20. Relevant Evidence & Research Confidence:** HIGH — Root cause of the `1/575` (`0.17%`) rolling-restart drop in `benchmark-evidence.md` is [RUNTIME_VERIFIED]; two-stage readiness fencing + `GOAWAY` is standard Envoy/Kubernetes zero-drop practice.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Directly closes `COND-PO-01` where RC9's rolling-restart benchmark recorded `1 / 575` (`0.17%`) dropped requests at the load-balancer connection-drain boundary during active streaming. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

## `DOM-06` — AI FinOps (`7` Proposed Features)

- **Domain Overview:** AI FinOps (DOM-06) delivers pre-execution spend governance, real-time streaming token metering, multi-scope budget hierarchies, conditional frontier rate evaluation, internal cost-center chargeback, cryptographic settlement receipts, and external vendor bill reconciliation. Unlike gateways that log approximate floating-point spend asynchronously after Upstream execution has already occurred, Tanpopo enforces hard budget ceilings before requests dispatch and settles exact integer micro-USD ledgers.
- **Existing RC9 Baseline Posture:** RC9's zero-float 128-bit integer SM-07 Reserve -> DeltaReserve -> Settle/Release kernel and 90-second orphan hold reaper are verified strengths [RUNTIME_VERIFIED]. However, synchronous per-request SQL ledger locking contributes to RC9's 109.48 req/s SQLite and 56.86 req/s 2-vCPU Docker HA throughput ceilings [RUNTIME_VERIFIED]; internal/finops/rollup/aggregator.go has zero production callers leaving usage_rollups_hourly empty (DEF-078, FND-005) [SOURCE_VERIFIED]; DAILY/MONTHLY budgets lack an automatic period-rollover worker and exhibit closed-vs-half-open boundary skews [SOURCE_VERIFIED]; budgets lack DELETE and Business-Unit/User scopes; and FOCUS 1.1 export, signed chargeback receipts, and vendor invoice reconciliation are absent.
- **Progressive Self-Hosted to Enterprise Experience:** A solo developer sets a simple USD 50 monthly budget cap during Day-1 setup and views real-time spend by model and API key with automatic monthly rollover. Enterprise FinOps controllers govern 7-tier hierarchical budgets (Enterprise -> BU -> Team -> Project -> Env -> Principal -> Key) with burn-rate anomaly alerts, evaluate 5m/1h cache TTLs and >200k context cliffs accurately, apply internal platform basis-point markups, export FinOps FOCUS 1.1 datasets to SAP/Snowflake, issue JCS-signed monthly BU chargeback receipts, and reconcile variances against AWS/Azure/OpenAI invoices.
- **Technical Selections Deferred to `TEEP-03`:** The exact distributed hot-path budget admission and pod escrow lease coordination algorithm (DEC-CAND-22, ARCH-002), sub-micro-USD settlement rounding and carry accumulator math (DEC-CAND-08), and incremental watermarked rollup partitioning internals (ARCH-003) are explicitly deferred to TEEP-03.

| ID | Subdomain | Feature Name | Class | Disposition | Priority | Placement Candidate |
|---|---|---|---|---|---|---|
| `FEAT-037` | Pre-Execution Budget Admission & Integer Ledger | **Two-Phase Pre-Request Budget Reservation, Streaming Delta Holds & Exact Settlement** | `CAT-PROD` | `KEEP` | `P0-Foundational` | `CORE_DATA_PLANE` |
| `FEAT-038` | Multi-Scope Budget Governance & Forecasting | **Hierarchical Multi-Scope Budgets, Burn-Rate Forecasting & Budget Lifecycle Delete** | `CAT-ADMIN` | `IMPROVE` | `P1-Enterprise-Core` | `CORE_CONTROL_PLANE` |
| `FEAT-039` | Billing Period Rollover & Reconciliation UX | **Automated Budget Period Rollover, Timezone-Safe Attribution & Custom Date-Range Explorer** | `CAT-ADMIN` | `IMPROVE` | `P0-Foundational` | `CORE_CONTROL_PLANE` |
| `FEAT-040` | Frontier Multi-Tier Pricing & GPU Costing | **Conditional Multi-Tier Pricing Evaluation (Cache TTLs, Context Cliffs, Service Tiers & GPU Amortization)** | `CAT-ADMIN` | `ADD` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |
| `FEAT-041` | Enterprise Chargeback & FOCUS 1.1 Integration | **Cost-Center Attribution, Internal Platform Markups & FOCUS 1.1 Chargeback Export** | `CAT-ADMIN` | `IMPROVE` | `P1-Enterprise-Core` | `CORE_CONTROL_PLANE` |
| `FEAT-042` | Cryptographic Financial Non-Repudiation | **Tamper-Evident Signed FinOps Settlement & Monthly Chargeback Receipts** | `CAT-ADMIN` | `ADD` | `P2-Differentiator` | `CORE_CONTROL_PLANE` |
| `FEAT-043` | External Cloud Billing Reconciliation | **Out-of-Band Cloud Provider Invoice & Usage Variance Reconciliation** | `CAT-ADMIN` | `ADD` | `P2-Differentiator` | `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` |

### FEAT-037 — Two-Phase Pre-Request Budget Reservation, Streaming Delta Holds & Exact Settlement

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-037` · `CAP` Traceability: `CAP-037` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: `INN-06`
- **2. Functional Domain & Subdomain:** `DOM-06` (AI FinOps) -> **Pre-Execution Budget Admission & Integer Ledger**
- **3. Plain-Language Product Summary:** Protects budgets before money is spent rather than alerting hours later after a runaway agent loop has already racked up thousands of dollars. Before sending any AI request, Tanpopo places a credit-card-style hold for the estimated cost, tops up the hold during long streaming answers if needed, and settles the exact integer cost down to the micro-dollar as soon as the response finishes.
  - *Practical Example:* A developer's recursive coding agent enters an accidental loop against a project with USD 2.00 remaining in its monthly budget; Tanpopo admits the first few calls via pre-request holds, blocks the next call pre-dispatch with `HTTP 402 budget_exceeded` the instant `settled + active_holds` hits the USD 2.00 ceiling, and refunds unused hold headroom when open streams finish short.
  - *Explicit Trade-Offs:* Honest trade-offs: (1) conservative Phase-1 holds temporarily lock worst-case `max_tokens` headroom while a request is in flight (mitigated by sensible default `max_tokens` estimation and `DeltaReserve`); (2) distributed hot-path admission faces a latency-vs-strictness trade-off across pods, so the exact distributed admission / pod-escrow lease algorithm is deferred to `TEEP-03` (`DEC-CAND-22`, `ARCH-002`); and (3) sub-micro-USD carry rounding is deferred to `TEEP-03` (`DEC-CAND-08`).
- **4. User Problem & Business Value:** Most competing gateways track spend asynchronously in IEEE-754 floats after upstream execution completes, allowing parallel request bursts to overshoot hard budgets and introducing cumulative rounding errors.
- **5. Target Personas:** `PER-07 (FinOps Controller & IT Finance Analyst)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-01 (Solo Builder / First-Time Self-Hoster)`
- **6. Existing RC9 Behavior & Limitations:** COMPLETE_FOR_RC9_SCOPE functionally in `internal/finops/{pricing,ledger}` and `valkey/budget_hold_lua.go` [RUNTIME_VERIFIED, SOURCE_VERIFIED]: 128-bit `u128` integer math, `SM-07` `Reserve -> DeltaReserve -> Settle/Release`, `DEF-085` pre-dispatch A2A zero-hold block, and 90s orphan hold reaper work reliably. Throughput limitation (`109.48 req/s` SQLite / `56.86 req/s` Docker HA [RUNTIME_VERIFIED]) is addressed by internal enablers `ARCH-002` (`CAP-038`) and `ARCH-005` (`CAP-047`).
- **7. Proposed Complete Functional Behavior:** Preserve RC9's exact zero-float `SM-07` two-phase state machine (`Reserve` pre-dispatch -> incremental `DeltaReserve` on long streams -> immutable `Settle` or full `Release` on pre-first-byte failure + 90s orphan reaper for synchronous calls) while pairing with `ARCH-002` distributed hot-path budget admission (algorithm deferred to `TEEP-03` `DEC-CAND-22`) and `FEAT-017` high-precision RateCards (`DEC-CAND-08`) [DESIGN_PROPOSAL].
- **8. User-Facing Subfeatures:**
  - Pre-dispatch `HTTP 402 budget_exceeded` hard-stop enforcement across LLM (`/v1/*`, `/native/*`), MCP, and A2A planes.
  - Streaming `DeltaReserve` top-ups and mid-stream budget cutoff option when an unbounded stream exhausts remaining budget.
  - Zero-float integer micro-USD settlement rows freezing `applied_pricing_snapshot_json` and `usage_accuracy`.
  - Automated orphan-hold reaper reclaiming crashed-connection holds + explicit `negative_carryover_micros` overrun tracking.
- **9. End-to-End Lifecycle:** Estimate worst-case/initial cost from `RateCard` -> `Phase 1 Reserve` across applicable budget scopes -> stream chunks with `DeltaReserve` if output exceeds initial estimate -> `Phase 2 Settle` exact usage (refunding unused hold immediately) -> append immutable `usage_settlements` row.
- **10. Primary & Alternative User Journeys:** Primary: Call reserves 5,000 micro-USD, consumes 1,240 micro-USD, refunds 3,760 micro-USD immediately on stream completion. Alternative: Upstream returns `503` pre-first-byte across all targets; hold is `100%` released (`0` charge) in `< 1 ms`.
- **11. Administrative & Permission Boundaries:** Enforced non-bypassably on all Data Plane requests whenever a matching active budget ledger exists; `usage_settlements` rows are protected by database immutability triggers (`0` `UPDATE`/`DELETE` allowed).
- **12. Automation Opportunities:** 90-second background orphan-hold reaper (exempting durable `BATCH_ESCROW` holds in FEAT-125) heals crashed pods automatically without manual ledger surgery.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** High-concurrency scaling via `ARCH-002` + `ARCH-005` (targeting `>= 10,000 RPS` cluster admission `[HYPOTHESIS]`), Valkey checkpoint reconciliation (`ARCH-013`), and zero float instructions in pricing/ledger paths.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Must execute inline on every Data Plane LLM, MCP, and A2A call to guarantee pre-dispatch budget admission and post-stream exact settlement.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Active Holds vs Settled Spend progress bars in `FinOpsPage.tsx` and per-trace micro-USD cost in `TracesPage.tsx`.
  - *CLI:* `tanpopo finops holds` and `tanpopo finops summary`.
  - *Admin / Data-Plane API:* `HTTP 402` error contract in `dataplane-v1.yaml`; ledger inspection on `/admin/v1/budgets`.
  - *GitOps / Terraform / Packs:* Hard-stop vs soft-warning enforcement modes declared on `budget` resources.
- **16. Dependencies & Cross-Domain Relationships:** `Core kernel of `INN-06` paired with `ARCH-002` (`DEC-CAND-22`), `ARCH-005` (group-commit writer), `ARCH-013` (Valkey hold checkpoints), FEAT-017 (`DEC-CAND-08`), and FEAT-042 (signed receipts).` · Related Decisions: `DEC-CAND-08`, `DEC-CAND-22`
- **17. Failure & Recovery Behavior:** Configurable fail-closed (`503 ledger_unavailable`, default for regulated hard-cap budgets) vs bounded grace mode if primary storage is unreachable; orphan reaper reconciles interrupted streams deterministically.
- **18. Security, Privacy & Financial Implications:** Eliminates float precision exploits, prevents parallel race-condition overspend bursts, and flags any unknown native usage via `ALT-UNMETERED-SPEND`.
- **19. Preliminary Measurable Acceptance Criteria:**
  - 500 concurrent requests racing against a tight remaining budget never push `settled_micros` past `hard_limit_micros` (modulo bounded mid-stream single-chunk overrun recorded in `negative_carryover_micros`).
  - Pre-first-byte failed requests release 100% of reserved micro-USD immediately.
  - Database triggers reject any `UPDATE` or `DELETE` against finalized `usage_settlements` rows on both SQLite and PostgreSQL.
- **20. Relevant Evidence & Research Confidence:** HIGH — Core `SM-07` kernel is [RUNTIME_VERIFIED] and [SOURCE_VERIFIED] on ac9ae87a (`ADR-0005`); distributed hot-path lease scaling (`>= 10,000 RPS`) is `[HYPOTHESIS]` deferred to `TEEP-03` (`DEC-CAND-22`).
- **21. Proposal Classification:** `KEEP` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Core architectural differentiator preventing runaway AI spend before upstream execution starts, using exact integer arithmetic with zero floating-point drift. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-038 — Hierarchical Multi-Scope Budgets, Burn-Rate Forecasting & Budget Lifecycle Delete

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-038` · `CAP` Traceability: `CAP-039` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-06` (AI FinOps) -> **Multi-Scope Budget Governance & Forecasting**
- **3. Plain-Language Product Summary:** Lets finance and platform teams set spending caps and early-warning alerts at every organizational level—from the whole Enterprise, Business Unit, Department, or Project down to an individual developer's SSO identity or single API key. It also predicts when a fast-burning budget will run out before it hits 100%, and lets admins edit warning thresholds or delete retired budgets cleanly.
  - *Practical Example:* An enterprise sets a USD 50,000/month cap on the Retail Banking BU, USD 8,000/month on its Fraud Analytics Project, and USD 150/month per developer `PRINCIPAL` using Claude Code; at 10:30 AM Tuesday, burn-rate forecasting detects a 6x velocity spike on Fraud Analytics projected to exhaust its monthly budget in 19 hours and fires an `ALT-BUDGET-BURN-SPIKE` Slack/Email alert at 38% utilization.
  - *Explicit Trade-Offs:* Evaluating up to 7 hierarchical budget scopes per request requires ancestor roll-up awareness (`ARCH-002`) so only scopes that actually have configured budgets incur hold checks (typically 1–3 active scopes per call).
- **4. User Problem & Business Value:** In RC9, budgets support `ORG`, `PROJECT`, `ENV`, and `VIRTUAL_KEY` only (no Business Unit/Team or per-user SSO allowance), cannot be deleted once created (`0` `DELETE /admin/v1/budgets/{id}`), and `FinOpsPage.tsx` cannot edit `soft_warning_percents` after creation [SOURCE_VERIFIED].
- **5. Target Personas:** `PER-04 (Business Unit / Line-of-Business Delegated Admin)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in `internal/admin/http/finops.go` and `web/console/src/pages/FinOpsPage.tsx` [SOURCE_VERIFIED]: 4 scopes only; missing `DELETE /admin/v1/budgets/{id}`; Console Edit omits `soft_warning_percents`; raw `scope_tier` enum strings leaked untranslated in `vi` locale (`PG-003`) [RUNTIME_VERIFIED]; and zero burn-velocity anomaly forecasting exists.
- **7. Proposed Complete Functional Behavior:** Expand budget scopes to 7 tiers (`ENTERPRISE`, `ORG`, `ORG_UNIT`, `PROJECT`, `ENVIRONMENT`, `PRINCIPAL`, `VIRTUAL_KEY`) with `DAILY`, `WEEKLY`, `MONTHLY`, `QUARTERLY`, and custom one-time campaign windows [DESIGN_PROPOSAL]. Add `DELETE /admin/v1/budgets/{id}` (archiving ledger history cleanly while releasing future enforcement), expose full `soft_warning_percents` (e.g., `[50, 75, 90, 100]`) and hard-stop vs advisory-only mode in `FinOpsPage.tsx`, localize all scope/period enums across 15 languages (`PG-003`), and compute EWMA spend-velocity forecasts (`projected_exhaustion_at_ms` + `ALT-BUDGET-BURN-SPIKE`).
- **8. User-Facing Subfeatures:**
  - 7-tier hierarchical budget scopes including `ENTERPRISE`, `ORG_UNIT` (BU/Dept/Team), and individual SSO `PRINCIPAL`.
  - Full Budget Edit & `DELETE` lifecycle in Console, CLI, API, and GitOps (including editable `soft_warning_percents`).
  - Advisory-Only (`SOFT_ALERT`) vs Hard-Stop (`HARD_BLOCK_402`) enforcement modes per budget.
  - Real-time Burn-Rate Velocity indicator, projected exhaustion date, and `ALT-BUDGET-BURN-SPIKE` anomaly alerts.
- **9. End-to-End Lifecycle:** Create budget at desired scope (`USD` input converted to integer micro-USD) -> active admission & threshold monitoring -> emit deduplicated soft warnings + burn-spike alerts -> Update ceiling/thresholds with `If-Match` -> Archive/Delete obsolete budget while preserving historical `usage_settlements`.
- **10. Primary & Alternative User Journeys:** Primary: Engineering VP sets USD 200/month `PRINCIPAL` default template so every SSO developer gets a fair coding-agent allowance automatically without per-user manual setup. Alternative: Admin deletes a temporary hackathon project budget; active enforcement stops cleanly while past settled rows remain queryable in Chargeback.
- **11. Administrative & Permission Boundaries:** Child admins (`BU_ADMIN`, `TEAM_ADMIN`) can create tighter sub-budgets within their subtree but can never raise or delete an ancestor `ORG` / `ENTERPRISE` budget; mutations require `ActionBudgetWrite` + `If-Match`.
- **12. Automation Opportunities:** Default per-`PRINCIPAL` or per-`PROJECT` budget templates auto-provision budgets when new SCIM users or projects are created.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Monotonic parent budget enforcement (`min` remaining headroom across ancestor chain), JCS audit log on all budget ceiling/delete changes, and 15-locale translated labels (`PG-003` closure).
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Budget CRUD, scope hierarchy validation, soft-warning editing, and EWMA burn-velocity forecasting run in the Control Plane and compile into active ledger policies.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Create/Edit/Delete Budget drawers, Burn-Rate Forecast badge, and localized scope filters in `FinOpsPage.tsx`.
  - *CLI:* `tanpopo finops budget create | list | update | delete`.
  - *Admin / Data-Plane API:* GET/POST/PUT/DELETE `/admin/v1/budgets/{id}` in `admin-v1.yaml`.
  - *GitOps / Terraform / Packs:* Declarative multi-scope budgets and auto-provisioning templates in YAML / Terraform.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-037 (two-phase kernel), FEAT-066 (`ENTERPRISE` & `ORG_UNIT` hierarchy), and DOM-19 (alert routing).`, `Closes `PG-003` FinOps enum localization gap (`FND-017`).` · Related Decisions: `DEC-CAND-01`, `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Prevent deleting a budget that currently holds in-flight reservations unless `drain/force` releases enforcement cleanly after active calls settle.
- **18. Security, Privacy & Financial Implications:** Every budget raise, mode switch (`HARD_BLOCK -> SOFT_ALERT`), or deletion is recorded in the tamper-evident ORG audit chain (and supports Four-Eyes approval in FEAT-068).
- **19. Preliminary Measurable Acceptance Criteria:**
  - Budgets across all 7 scopes (`ENTERPRISE` through `PRINCIPAL` and `VIRTUAL_KEY`) enforce cumulative caps accurately.
  - `DELETE /admin/v1/budgets/{id}` and editing `soft_warning_percents` work end-to-end across API, Console, and CLI.
  - Simulated 5x spend-velocity surge emits `ALT-BUDGET-BURN-SPIKE` with projected exhaustion timestamp prior to hitting 100%.
- **20. Relevant Evidence & Research Confidence:** HIGH — Missing `DELETE`, missing `soft_warning_percents` edit, and `PG-003` enum issue are [SOURCE_VERIFIED] and [RUNTIME_VERIFIED] on ac9ae87a.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Extends RC9's 4 budget scopes to full 7-tier enterprise hierarchies (`Enterprise`, `OrgUnit`, `Principal`), adds missing budget `DELETE` and warning-threshold edits, and catches abnormal spend velocity early. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-039 — Automated Budget Period Rollover, Timezone-Safe Attribution & Custom Date-Range Explorer

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-039` · `CAP` Traceability: `CAP-040` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-06` (AI FinOps) -> **Billing Period Rollover & Reconciliation UX**
- **3. Plain-Language Product Summary:** Ensures daily, weekly, monthly, and quarterly budgets automatically reset and carry forward any mid-stream overruns right at midnight or month-end—using your organization's configured billing timezone—and adds flexible date-range pickers to the FinOps Console so budget totals and chargeback reports match to the exact micro-dollar.
  - *Practical Example:* At `00:00:00` on December 1 (`America/New_York` org billing timezone), Tanpopo's rollover worker mints December `budget_ledgers` rows idempotently across all active monthly budgets, carries over a `14 micro-USD` November final-stream overrun cleanly, and lets the FinOps controller filter `Nov 1 – Nov 30` in `FinOpsPage` with zero boundary discrepancy between the Budgets tab and Chargeback CSV export.
  - *Explicit Trade-Offs:* Long-running SSE or batch calls that start at `23:59:40` and finish at `00:00:20` cross period boundaries; stamping `billing_period_ref_ms` at Phase-1 reservation time guarantees the settlement lands in the exact same period ledger that held the reservation (eliminating phantom cross-midnight drift).
- **4. User Problem & Business Value:** In RC9 (`FND-005`, `DEF-078`): (1) `ledger/service.go:1257` had no rollover cron when `nowMs >= period_end_ms`; (2) `finops.go:853` used closed `<= toMs` while `:1118` used `< period_end_ms`; (3) `rollup.NewAggregator` was unwired (`0` rows in `usage_rollups_hourly`); and (4) `FinOpsPage.tsx` lacked `from`/`to` date pickers.
- **5. Target Personas:** `PER-07 (FinOps Controller & IT Finance Analyst)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-01 (Solo Builder / First-Time Self-Hoster)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION [SOURCE_VERIFIED, RUNTIME_VERIFIED]: zero automatic period rollover worker in `serve.go`; `finops.go:853` closed `<= toMs` vs `:1118` `< period_end_ms` interval skew; cross-midnight stream attribution drift (`DEF-078`); unwired `rollup.NewAggregator` (`ARCH-003`); and missing `from`/`to` date-range selectors in `FinOpsPage.tsx`.
- **7. Proposed Complete Functional Behavior:** Wire an idempotent, cluster-safe Period Rollover Worker (plus lazy on-request fallback if a cron tick was delayed) minting next-period `budget_ledgers` rows using UTC or org-configured IANA billing timezone and carrying forward `negative_carryover_micros` [DESIGN_PROPOSAL]. Stamp `billing_period_ref_ms` on Phase-1 holds -> Phase-2 settlements, enforce strict half-open `[fromMs, toMs)` across 100% of SQL/rollup queries, wire `ARCH-003` (`usage_rollups_hourly`), and add preset (`Today`, `MTD`, `Last Month`, `Last 30d`) + custom `From` / `To` date-time pickers in `FinOpsPage.tsx`.
- **8. User-Facing Subfeatures:**
  - Automatic, cluster-idempotent `DAILY` / `WEEKLY` / `MONTHLY` / `QUARTERLY` budget rollover with `negative_carryover_micros` propagation.
  - Configurable organization billing timezone (default `UTC`, supporting IANA zones such as `Europe/Berlin` or `America/New_York`).
  - Preset (`MTD`, `Last Calendar Month`, `Last 7d/30d/90d`) and custom `From` / `To` date-range explorer in `FinOpsPage.tsx`.
  - Zero-skew half-open `[fromMs, toMs)` reconciliation backed by wired `usage_rollups_hourly` (`ARCH-003`).
- **9. End-to-End Lifecycle:** Pre-rollover worker creates next period row ahead of boundary (and finalizes `negative_carryover_micros` at boundary) -> cross-boundary streams settle against their Phase-1 `billing_period_ref_ms` -> `ARCH-003` aggregates hourly watermarks -> Console & exports query rollups + live tail with exact micro-USD parity.
- **10. Primary & Alternative User Journeys:** Primary: Monthly budget rolls over automatically at month-end across a 3-node HA Control Plane with zero duplicate ledger rows (unique constraint + advisory lock) and zero missed enforcement window. Alternative: Single-node SQLite laptop wakes from sleep 3 days into a new month; lazy inline rollover + catch-up worker advances the period immediately before admitting the first request.
- **11. Administrative & Permission Boundaries:** Organization billing timezone configured by `ORG_OWNER` / `OPERATOR`; `FINOPS_VIEWER` and scoped admins query arbitrary historical date ranges within their RBAC subtree.
- **12. Automation Opportunities:** Pre-minting the upcoming period ledger row 5 minutes before midnight eliminates latency spikes at `00:00:00`.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Sub-50ms dashboard summary queries over multi-million-settlement history via `ARCH-003` `[HYPOTHESIS]`, `O(k_active_orgs)` watermark scanning (fixing `ARCH-OBS-01` Loop #17), and exact micro-USD accounting parity.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Background rollover scheduling, canonical `[fromMs, toMs)` interval queries, watermarked rollup reads (`ARCH-003`), and Console date-range pickers span Control Plane FinOps services and UI.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Date-Range Picker bar, Billing Period History selector, and Rollup Freshness indicator in `FinOpsPage.tsx`.
  - *CLI:* `tanpopo finops summary --from 2026-11-01 --to 2026-12-01` and `tanpopo finops export --from ... --to ...`.
  - *Admin / Data-Plane API:* Consistent half-open `from_ms` / `to_ms` query parameters on `/admin/v1/usage/summary` and `/admin/v1/usage/export`.
  - *GitOps / Terraform / Packs:* `billing_timezone` setting on declarative organization configuration.
- **16. Dependencies & Cross-Domain Relationships:** `Closes `FND-005` / `DEF-078` and depends on `ARCH-003` (`CAP-042` watermarked `usage_rollups_hourly` aggregator).` · Related Decisions: None (Standard Catalog Review)
- **17. Failure & Recovery Behavior:** Rollup lags never lose data because `/usage/summary` seamlessly unions materialized `usage_rollups_hourly` up to `watermark_ms` with raw `usage_settlements` above `watermark_ms`.
- **18. Security, Privacy & Financial Implications:** Eliminates boundary double-counting or blind enforcement gaps at month-end financial close.
- **19. Preliminary Measurable Acceptance Criteria:**
  - `DAILY` and `MONTHLY` budgets enforce continuously across midnight/month-end boundaries (including cross-midnight SSE streams) with `0` micro-USD discrepancy between Budget Ledger and Chargeback Summary (`DEF-078`).
  - `usage_rollups_hourly` is populated automatically in production `tanpopo serve` (`> 0` rows verified in E2E).
  - `FinOpsPage.tsx` filters summaries, charts, and exports across arbitrary `From` / `To` ranges in all 15 locales.
- **20. Relevant Evidence & Research Confidence:** HIGH — Unwired `rollup.NewAggregator`, missing rollover worker, and `<= toMs` skew in `finops.go:853` are [SOURCE_VERIFIED] on ac9ae87a and [RUNTIME_VERIFIED] in QA-005 `DEF-078`.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P0-Foundational`** — Fixes critical `FND-005` / `DEF-078` defects where `DAILY`/`MONTHLY` budgets have zero automatic period-rollover worker, closed-vs-half-open interval queries skew totals by 1–2 micro-USD at period boundaries, and `FinOpsPage.tsx` lacks custom date-range pickers. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-040 — Conditional Multi-Tier Pricing Evaluation (Cache TTLs, Context Cliffs, Service Tiers & GPU Amortization)

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-040` · `CAP` Traceability: `CAP-041` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-06` (AI FinOps) -> **Frontier Multi-Tier Pricing & GPU Costing**
- **3. Plain-Language Product Summary:** Accurately calculates costs for modern complex AI pricing structures—including higher rates when prompts cross 200k-token 'context cliffs', different fees for 5-minute versus 1-hour prompt-cache storage, Priority versus Flex/Batch service tiers, off-peak UTC discount windows, web-search tool fees, and hourly GPU/PTU amortization for self-hosted clusters.
  - *Practical Example:* A legal RAG pipeline sends a 240,000-token contract to Claude or Gemini with a 1-hour prompt-cache write on Priority tier; instead of under-billing by 2.5x using base `<200k` 5-minute rates, Tanpopo's conditional evaluator matches the `>200k` context bracket, applies the `2.0x` 1-hour cache-write rate and Priority multiplier, and stamps the exact matched tier rule ID into the settlement snapshot.
  - *Explicit Trade-Offs:* During Phase-1 pre-request reservation, exact final prompt/search counts may sit near a context cliff (e.g., 198k estimated tokens); the calculator reserves conservatively at the upper bracket when within estimation margin and settles the exact vendor-reported bracket in Phase 2.
- **4. User Problem & Business Value:** Prevents 2x–4x silent budget under-counting on >200k long-context RAG workloads, 1-hour prompt-cache writes, and Priority service tiers while enabling accurate showback/chargeback for shared self-hosted GPU clusters.
- **5. Target Personas:** `PER-07 (FinOps Controller & IT Finance Analyst)`, `PER-03 (Enterprise AI Platform Architect / SRE)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in `internal/finops/pricing/calculator.go` [SOURCE_VERIFIED]: supports only a single flat `CacheWritePer1MNanoUSD` and has zero support for context-length cliffs (`81` keys in LiteLLM), `priority`/`flex`/`batch` tiers (`1,101` keys in LiteLLM), 1h cache writes, search queries, or granular self-hosted GPU node hourly amortization.
- **7. Proposed Complete Functional Behavior:** Extend `pricing.Calculate` and `NormalizedUsage` to evaluate ordered, deterministic conditional pricing rules (`min_input_tokens` / `max_input_tokens` context cliffs, `service_tier` = `standard|priority|flex|batch`, `cache_write_ttl` = `5m|1h`, `utc_time_window`, `web_search_requests`) [DESIGN_PROPOSAL]. For self-hosted GPUs (`vLLM`/`Ollama`/`llm-d`) and pre-paid `PTU`s (FEAT-016), support hourly pool cost allocation across consuming projects/keys proportional to GPU compute-seconds or weighted tokens.
- **8. User-Facing Subfeatures:**
  - Context-window cliff brackets (e.g., `<= 128k` vs `> 128k`, `<= 200k` vs `> 200k`, `> 272k`) matching vendor rate sheets.
  - Separate `5m` (`1.25x`) vs `1h` (`2.0x`) prompt-cache write rates plus `standard` / `priority` / `flex` / `batch` service-tier rates.
  - Per-query hosted search/grounding fees and scheduled UTC off-peak discount windows.
  - Self-hosted GPU cluster (`USD/hour` node cost) & PTU hourly cost-allocation rules for internal showback/chargeback.
- **9. End-to-End Lifecycle:** Conditional tiers pre-loaded from Signed Model Intelligence Pack (FEAT-018) or configured in RateCard (FEAT-017) -> Phase-1 `Reserve` evaluates conservative applicable tier -> Adapter extracts exact upstream `service_tier`, `cache_creation.ephemeral_1h_input_tokens`, and token totals -> Phase-2 `Settle` records exact matched bracket in `applied_pricing_snapshot_json`.
- **10. Primary & Alternative User Journeys:** Primary: 215k-token request matches `>200k` tier automatically and settles exact elevated input/output rates. Alternative: On-prem `vLLM` pool configured at `USD 18.50/hour` (`8xH100`) allocates hourly cost across 4 consuming departments based on settled token-seconds during hourly rollup (`ARCH-003`).
- **11. Administrative & Permission Boundaries:** Conditional tiers and GPU hourly rates are edited by `ORG_OWNER` / `OPERATOR` / `FINOPS_ADMIN` and inspected in read-only detail by `FINOPS_VIEWER`.
- **12. Automation Opportunities:** Signed Model Intelligence Packs (FEAT-018) ship pre-built context-cliff and 5m/1h cache-tier rules for Anthropic, Gemini, OpenAI, Bedrock, and DeepSeek out of the box.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** 100% deterministic integer math (`0` floats, `DEC-CAND-08`), zero-allocation evaluation on the hot path, and full support inside the What-If Traffic Replay simulator (FEAT-139).
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — Ordered conditional rate-bracket matching must run deterministically inside `internal/finops/pricing/calculator.go` during both Phase-1 reservation and Phase-2 settlement.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Conditional Pricing Tiers accordion & GPU Hourly Amortization section in `ModelsPage` RateCard drawer.
  - *CLI:* Dry-run cost calculator `tanpopo finops calc-preview --deployment <id> --input-tokens 250000 --tier priority`.
  - *Admin / Data-Plane API:* `pricing_tiers[]` and `gpu_amortization` schema on `/admin/v1/deployments`.
  - *GitOps / Terraform / Packs:* Declarative conditional pricing rules in deployment YAML.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-017 (RateCard schema, `DEC-CAND-08`), FEAT-018 (Model Intelligence Packs), and `ARCH-003` (hourly GPU amortization rollups).` · Related Decisions: `DEC-CAND-08`
- **17. Failure & Recovery Behavior:** Overlapping or ambiguous custom tier rules are rejected at save time by a deterministic overlap validator.
- **18. Security, Privacy & Financial Implications:** Closes the 2x–4x long-context and 1h-cache budget under-counting loophole that attackers or misconfigured RAG loops could otherwise trigger.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Golden test suite verifies exact integer settlement across Anthropic 5m/1h cache writes, Gemini/Claude >200k context cliffs, OpenAI Priority/Flex/Batch tiers, and search query fees.
  - Phase-1 `Reserve` never under-reserves when `max_tokens` or input length spans a higher conditional tier.
  - Self-hosted hourly GPU pool costs reconcile to 100% of configured hourly micro-USD across consuming projects in FinOps exports.
- **20. Relevant Evidence & Research Confidence:** HIGH — Vendor multi-tier pricing structures are [SOURCE_VERIFIED] in SRC-045 (`81` context-tier & `1,101` priority keys), SRC-047, SRC-048, and SRC-051.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — 2026 frontier models apply 2x–4x rate jumps above 128k/200k/272k context cliffs, 1.25x vs 2.0x surcharges on 5m vs 1h prompt-cache writes, and 50% discounts/premiums on Batch/Flex/Priority service tiers; flat single-rate math misprices real enterprise bills by up to 300%. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-041 — Cost-Center Attribution, Internal Platform Markups & FOCUS 1.1 Chargeback Export

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-041` · `CAP` Traceability: `CAP-043` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-06` (AI FinOps) -> **Enterprise Chargeback & FOCUS 1.1 Integration**
- **3. Plain-Language Product Summary:** Tags every AI request with validated cost-center and business attribution codes, optionally adds a transparent internal platform fee (such as a 5% central AI platform recovery markup) kept strictly separate from raw vendor costs, and exports ready-to-ingest FinOps Foundation `FOCUS 1.1` billing files.
  - *Practical Example:* A central AI Platform team requires every production Project to declare a `cost_center` (inherited by keys and overridable via validated `X-Tanpopo-Attribution-Tags`), applies a `400 bps` (`4.00%`) platform fee to fund shared SRE operations, and exports monthly `FOCUS 1.1` Parquet/CSV files directly into Snowflake.
  - *Explicit Trade-Offs:* Allowing arbitrary client-supplied header tags would explode rollup table cardinality (`ARCH-003`); therefore, rollup-indexed attribution keys (`cost_center`, `application_id`, `environment`) are schema-governed and length-capped (`<= 64` chars, allowlisted keys) while free-form request correlation IDs stay in trace metadata.
- **4. User Problem & Business Value:** RC9's `/admin/v1/usage/export` emits a custom CSV/JSON schema lacking `cost_center` codes, internal platform markups (`provider_cost_micros` vs `platform_fee_micros`), and industry-standard `FOCUS 1.1` columns, and had an open QA-005 partial (`DEF-077`) on non-zero duplicate-display-name Virtual Key exports.
- **5. Target Personas:** `PER-07 (FinOps Controller & IT Finance Analyst)`, `PER-04 (Business Unit / Line-of-Business Delegated Admin)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in `internal/admin/http/finops.go` [SOURCE_VERIFIED, RUNTIME_VERIFIED]: basic custom CSV/JSON export exists, but lacks `cost_center` governance, basis-point platform markups, FinOps Foundation `FOCUS 1.1` export format, and full non-zero fixture verification for duplicate-name Virtual Keys (`DEF-077`, `FND-015`).
- **7. Proposed Complete Functional Behavior:** Resolve `effective_cost_center` and schema-validated `attribution_tags_json` across `OrgUnit -> Project -> VirtualKey -> allowlisted request header` (with optional policy requiring mandatory `cost_center` in `prod`) [DESIGN_PROPOSAL]. Compute integer `platform_fee_micros` from configurable basis points (`0..10000 bps`) strictly separate from `provider_cost_micros` (`total_billed_micros = provider_cost_micros + platform_fee_micros`), disambiguate duplicate-name Virtual Keys by immutable `virtual_key_id` (`DEF-077`), and stream **FinOps Foundation `FOCUS 1.1`** exports in CSV, JSONL, and Parquet.
- **8. User-Facing Subfeatures:**
  - Hierarchical `cost_center` inheritance (`OrgUnit -> Project -> Key`) with optional mandatory-tag policy gate.
  - Integer basis-point Internal Platform Markup (`platform_fee_bps`) displaying `Provider Cost`, `Platform Fee`, and `Total Chargeback` separately.
  - Standards-compliant FinOps Foundation `FOCUS 1.1` export (`BilledCost`, `EffectiveCost`, `ListCost`, `ConsumedQuantity`, `ConsumedUnit`, `SubAccountId`, `Tags`) in CSV, JSONL, and Parquet.
  - Guaranteed `virtual_key_id` disambiguation when two keys share the same human display name (`DEF-077` closure).
- **9. End-to-End Lifecycle:** Admin configures `cost_center` rules & optional `platform_fee_bps` -> every settled request stamps `cost_center`, `provider_cost_micros`, and `platform_fee_micros` -> `ARCH-003` aggregates hourly rollups -> Finance exports `FOCUS 1.1` file or streams to warehouse sink (FEAT-117).
- **10. Primary & Alternative User Journeys:** Primary: Monthly close job downloads `FOCUS 1.1` CSV/Parquet grouped by `cost_center` and reconciles exact vendor vs internal recovery totals. Alternative: Self-hosted solo user leaves `cost_center` optional and `platform_fee_bps = 0`, seeing clean raw provider costs with zero extra configuration.
- **11. Administrative & Permission Boundaries:** Platform markup basis points are restricted to `ORG_OWNER` / `OPERATOR`; mandatory `cost_center` policies are managed by `ORG_OWNER` / `FINOPS_ADMIN`; `FINOPS_VIEWER` can export all `FOCUS 1.1` reports.
- **12. Automation Opportunities:** Scheduled monthly chargeback export generation pushed automatically to S3/Snowflake/Webhook sinks (DOM-19).
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Bounded tag cardinality, streaming constant-memory export over millions of rollup rows, CSV formula-injection sanitization (`=`, `+`, `-`, `@` escaping), and exact integer basis-point arithmetic.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — Tag inheritance compiles into `ConfigSnapshot` for settlement stamping while `FOCUS 1.1` formatting and streaming CSV/JSONL/Parquet exports run in the Control Plane FinOps API.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Cost-Center Breakdown table, Platform Markup Settings, and `Export FOCUS 1.1 (CSV / JSONL / Parquet)` button in `FinOpsPage.tsx`.
  - *CLI:* `tanpopo finops export --format focus-1.1-csv --from ... --to ...`.
  - *Admin / Data-Plane API:* `format=focus_1_1_csv|focus_1_1_jsonl|focus_1_1_parquet` on `GET /admin/v1/usage/export`.
  - *GitOps / Terraform / Packs:* `cost_center`, `required_attribution_tags`, and `platform_fee_bps` in org/project/key YAML.
- **16. Dependencies & Cross-Domain Relationships:** `Closes `DEF-077` (`FND-015`) and integrates with `ARCH-003` (hourly rollups), FEAT-066 (`org_units`), and FEAT-067 (VirtualKey tags).` · Related Decisions: `DEC-CAND-01`, `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Requests missing a required `cost_center` fail fast pre-dispatch with `HTTP 400 missing_cost_center_tag` when strict production tag governance is active.
- **18. Security, Privacy & Financial Implications:** Prevents CSV spreadsheet formula injection on exported key/project names and maintains strict separation between external vendor cost and internal markup so tax/AP teams never confuse internal fees with vendor spend.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Exported `FOCUS 1.1` files pass FinOps Foundation `FOCUS 1.1` column/type schema validation.
  - Two Virtual Keys with identical display names and non-zero spend appear as distinct, accurately summed rows keyed by `virtual_key_id` (`DEF-077`).
  - `platform_fee_micros` + `provider_cost_micros` equals `total_billed_micros` with zero floating-point rounding drift.
- **20. Relevant Evidence & Research Confidence:** HIGH — `FOCUS 1.1` schema is standardized (SRC-083) and `DEF-077` is [RUNTIME_VERIFIED] in QA-005.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Integrates Tanpopo directly into enterprise SAP, Snowflake, Apptio, and Vantage FinOps pipelines without custom ETL, while closing QA-005 `DEF-077` non-zero duplicate-name Virtual Key export verification. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-042 — Tamper-Evident Signed FinOps Settlement & Monthly Chargeback Receipts

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-042` · `CAP` Traceability: `CAP-044` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: `INN-06`
- **2. Functional Domain & Subdomain:** `DOM-06` (AI FinOps) -> **Cryptographic Financial Non-Repudiation**
- **3. Plain-Language Product Summary:** Produces tamper-evident, digitally signed monthly AI billing receipts for every Project and Business Unit. Finance controllers and department leads can verify any invoice offline with a single CLI command—proving mathematically that every dollar billed came from authentic settled requests under frozen model rate cards with zero post-hoc database edits.
  - *Practical Example:* When the Investment Research BU questions a USD 14,820 November AI chargeback, the FinOps controller provides `invoice-research-2026-11.receipt.json`; running `tanpopo finops verify-invoice` offline verifies the Ed25519 signature, hourly Merkle rollup roots, and exact rate-card version breakdown in under 1 second.
  - *Explicit Trade-Offs:* Honest scope boundary: signed receipts mathematically prove that gateway-metered settlements and frozen `RateCard` snapshots were not altered after execution, whereas detecting unproxied 'shadow' API keys used outside Tanpopo or vendor-side billing bugs requires out-of-band cloud invoice reconciliation (FEAT-043).
- **4. User Problem & Business Value:** In enterprises running internal AI chargeback, Business Units routinely dispute central platform numbers when gateways store mutable floating-point tables with no cryptographic tie to active rate cards.
- **5. Target Personas:** `PER-07 (FinOps Controller & IT Finance Analyst)`, `PER-06 (CISO, DPO & Regulatory Compliance Auditor)`
- **6. Existing RC9 Behavior & Limitations:** MISSING cryptographic FinOps receipts in RC9 [SOURCE_VERIFIED]: `usage_settlements` rows are protected by SQL immutability triggers (`0` UPDATE/DELETE), which DBAs with superuser access could still bypass on raw storage without cryptographic detection.
- **7. Proposed Complete Functional Behavior:** Complete the `INN-06` non-repudiation chain [DESIGN_PROPOSAL]: (1) compute deterministic `RFC 8785 JCS` SHA-256 digests over settled records (pseudonymized IDs + token counts + integer micro-USD + `rate_card_version` hash, `0` PII/prompts); (2) seal hourly Merkle roots during `ARCH-003` rollup aggregation; (3) generate `Ed25519`-signed monthly per-Project / per-OrgUnit Chargeback Receipt bundles (plus optional per-response `X-Tanpopo-Settlement-Digest` trailer); and (4) ship offline `tanpopo finops verify-invoice`.
- **8. User-Facing Subfeatures:**
  - Hourly Merkle-tree settlement sealing integrated into watermarked FinOps rollups (`ARCH-003`).
  - 1-click & automated month-end `Ed25519`-signed JCS Chargeback Receipt bundles per Org, OrgUnit, and Project.
  - Offline `tanpopo finops verify-invoice <receipt.json> --pubkey <pem>` verifier requiring zero database or network access.
  - Optional `X-Tanpopo-Settlement-Digest` response header/trailer for regulated B2B multi-tenant API resellers.
- **9. End-to-End Lifecycle:** Requests settle with JCS digest -> Hourly rollup seals Merkle root over settled window -> Period close signs monthly Chargeback Receipt with instance/KMS signing key -> BU/Auditor verifies receipt offline.
- **10. Primary & Alternative User Journeys:** Primary: Monthly close generates signed receipts for all 45 enterprise projects automatically. Alternative: Direct SQL tampering alters a historical `usage_settlements` row on disk; `tanpopo finops verify-invoice` (and audit verification) immediately flags `MERKLE_ROOT_MISMATCH` on the affected hour.
- **11. Administrative & Permission Boundaries:** Signing keys are sealed in Vault / KMS (`OPERATOR` custody); `FINOPS_VIEWER`, `ORG_OWNER`, and `BU_ADMIN` download and verify receipts for their authorized scopes.
- **12. Automation Opportunities:** Automatically attaches signed monthly `.receipt.json` artifacts to Compliance Evidence Bundles (FEAT-072) and outbound FinOps notifications.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_ENTERPRISE_GOVERNANCE`):** Zero PII or prompt text inside receipts (safe to share with external accountants), `RFC 8785` canonical JSON determinism across architectures, and key-rotation history support.
- **14. Core vs Extension Placement Candidate:** `CORE_CONTROL_PLANE` — RFC 8785 JCS canonicalization, Merkle hourly/monthly rollup root sealing, and Ed25519 receipt signing execute in the Control Plane FinOps & Audit subsystem.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* 'Download Signed Monthly Receipt' & Verification Status badge in `FinOpsPage.tsx`.
  - *CLI:* `tanpopo finops generate-receipt` and offline `tanpopo finops verify-invoice`.
  - *Admin / Data-Plane API:* GET `/admin/v1/usage/receipts` and POST `/admin/v1/usage/receipts:verify`.
  - *GitOps / Terraform / Packs:* `enable_signed_chargeback_receipts` policy toggle in organization config.
- **16. Dependencies & Cross-Domain Relationships:** `Realizes `INN-06` alongside FEAT-037 (two-phase integer kernel) and `ARCH-003` (watermarked hourly rollups), complemented by FEAT-043 (vendor invoice reconciliation).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Receipt generation refuses to finalize a period seal until all in-flight cross-midnight holds for that `billing_period_ref_ms` have settled or been reaped (`status = PENDING_FINAL_SETTLEMENTS`).
- **18. Security, Privacy & Financial Implications:** Detects even privileged DBA-level tampering against historical financial tables while exposing zero customer prompt or personal user email content.
- **19. Preliminary Measurable Acceptance Criteria:**
  - `tanpopo finops verify-invoice` validates a signed monthly receipt offline with exit code `0` and detects single-bit tampering with non-zero exit code.
  - Signed receipt totals match `FOCUS 1.1` export (`FEAT-041`) and `FinOpsPage` summary to the exact micro-dollar.
  - Receipt JSON contains zero prompt text, API keys, or raw user emails.
- **20. Relevant Evidence & Research Confidence:** HIGH — Reuses Tanpopo's existing [RUNTIME_VERIFIED] `RFC 8785 JCS` + `Ed25519` cryptographic primitives (`internal/security/audit`) over `usage_settlements`.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — Eliminates internal Business Unit chargeback disputes and satisfies external financial/SaaS audit requirements by making monthly AI invoices cryptographically verifiable offline. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `v1.1 (Core)`)*

---

### FEAT-043 — Out-of-Band Cloud Provider Invoice & Usage Variance Reconciliation

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-043` · `CAP` Traceability: `CAP-045` · Catalog Class: `CAT-ADMIN` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-06` (AI FinOps) -> **External Cloud Billing Reconciliation**
- **3. Plain-Language Product Summary:** Compares what Tanpopo metered on each provider connection against the actual daily billing reports from OpenAI, Anthropic, AWS, Azure, and Google Cloud—or imported vendor CSV invoices in air-gapped networks. If actual vendor charges drift from gateway totals (for example, because someone used an API key directly outside Tanpopo or left an idle PTU running), Finance gets an immediate variance alert.
  - *Practical Example:* Every morning at 06:00 UTC, Tanpopo pulls read-only daily cost buckets from OpenAI `/v1/organization/costs` and AWS Cost Explorer; it flags an unexpected USD 640 (+14%) variance on `conn-openai-marketing`, revealing that a legacy script was still calling OpenAI directly using the same upstream organization key.
  - *Explicit Trade-Offs:* Cloud vendor billing APIs typically lag real-time usage by 6–24 hours and aggregate at the upstream account/project/key level rather than individual HTTP request IDs; reconciliation therefore operates on aligned daily UTC buckets (`T-24h` / `T-48h`) with configurable variance tolerance (default `2%`).
- **4. User Problem & Business Value:** Gateway-side metering alone cannot see out-of-band 'shadow' usage sharing an upstream vendor account, forgotten cloud storage fees, or misentered manual `RateCard` overrides.
- **5. Target Personas:** `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED]: zero vendor billing API connectors or invoice CSV reconciliation views exist.
- **7. Proposed Complete Functional Behavior:** Provide opt-in read-only Billing Reconciliation Connectors (OpenAI `/v1/organization/costs` & `/usage/*`, Anthropic Admin Usage/Cost API, AWS Cost Explorer, Azure Cost Management, GCP Billing / `FOCUS` CSV/Parquet offline file upload for air-gapped enclaves) [DESIGN_PROPOSAL]. Compare daily vendor line items per connection/model against `usage_rollups_hourly`, classify variances (`SHADOW_UNPROXIED_USAGE`, `RATE_CARD_DRIFT`, `UNMETERED_STORAGE_OR_TOOL_FEE`), and emit alerts when drift exceeds threshold.
- **8. User-Facing Subfeatures:**
  - Scheduled read-only billing API sync for OpenAI, Anthropic, AWS, Azure, and GCP (strictly off in air-gap unless enabled).
  - Offline Vendor Invoice CSV / `FOCUS 1.1` file upload in `FinOpsPage` for air-gapped or manual month-end reconciliation.
  - Daily Gateway-Metered vs Vendor-Billed Variance Waterfall highlighting >2% drift per connection and model.
  - Actionable root-cause hints distinguishing unproxied shadow key usage from outdated `MANUAL_OVERRIDE` rate cards.
- **9. End-to-End Lifecycle:** Bind read-only billing credential (or upload vendor `FOCUS`/CSV invoice) -> scheduled/manual sync aligns daily UTC buckets -> computes micro-USD & percentage variance against `ARCH-003` rollups -> alerts `FINOPS_VIEWER` / `ORG_OWNER` when variance exceeds threshold.
- **10. Primary & Alternative User Journeys:** Primary: Automated daily sync shows `< 0.3%` variance across 12 cloud connections, giving Finance green-light confidence for month-end close. Alternative: Air-gapped bank uploads monthly AWS/Azure CSV export into `FinOpsPage` to generate the variance report with zero outbound internet calls.
- **11. Administrative & Permission Boundaries:** Billing reconciliation credentials must use least-privilege read-only scopes sealed in Vault; reports visible to `FINOPS_VIEWER`, `ORG_OWNER`, and `OPERATOR`.
- **12. Automation Opportunities:** When systematic per-token rate drift is detected on a connection, offers a 1-click deep link to review that deployment's `RateCard` override (FEAT-020).
- **13. Enterprise-Specific Requirements (`OPTIONAL_ECOSYSTEM_INTEGRATION`):** 100% `BlindHostCredentialBroker` + `SSRFGuardDialer` egress mediation, air-gap CSV/Parquet parity, and tenant-scoped variance isolation.
- **14. Core vs Extension Placement Candidate:** `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` — Core Control Plane stores daily variance records and threshold alerts while vendor-specific billing API parsers run as modular read-only connectors mediated by `BlindHostCredentialBroker`.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Invoice Reconciliation tab, Variance Chart, and Offline Invoice Upload in `FinOpsPage.tsx`.
  - *CLI:* `tanpopo finops reconcile --invoice-file aws-nov.csv` and `tanpopo finops variance-report`.
  - *Admin / Data-Plane API:* POST `/admin/v1/usage/reconcile` and GET `/admin/v1/usage/variances`.
  - *GitOps / Terraform / Packs:* Declarative reconciliation schedules and variance alert thresholds.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on `ARCH-003` (hourly rollups), `ARCH-008` (BlindBroker), FEAT-013 (cloud auth), and DOM-19 (variance notifications).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Vendor billing API lag (`pending/incomplete day`) marks recent buckets as `PROVISIONAL` and re-checks at `T+48h` rather than firing false-positive variance alarms on incomplete vendor data.
- **18. Security, Privacy & Financial Implications:** Uses strictly read-only billing credentials (never inference/admin write keys) and uncovers leaked or shadow upstream API keys early.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Uploading a vendor `FOCUS 1.1` / OpenAI / AWS cost CSV or syncing against mock billing APIs computes accurate daily micro-USD and basis-point variance per connection.
  - Variance exceeding configured threshold (e.g., `> 2%` and `> USD 10`) emits a structured FinOps notification.
  - Zero outbound billing sync requests occur unless explicitly configured (preserving strict air-gap silence).
- **20. Relevant Evidence & Research Confidence:** HIGH — Vendor cost APIs (`GET /v1/organization/costs`, AWS Cost Explorer, Azure Cost Management, `FOCUS 1.1` SRC-048/083) are [SOURCE_VERIFIED].
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — Closes the accounting loop between gateway-metered token spend and actual Accounts Payable invoices from OpenAI, Anthropic, AWS Cost Explorer, Azure Cost Management, and GCP Billing. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `Out-of-Band .tpkg / Tooling Wave 2`)*

---

## `DOM-07` — Caching and Performance (`3` Proposed Features)

- **Domain Overview:** Caching and Performance (DOM-07) minimizes AI inference latency and recurring token expenditure across three complementary layers: deterministic exact response caching, opt-in tenant-isolated semantic similarity caching, and automatic upstream provider prompt-cache optimization—supported under the hood by pooled keep-alive upstream transports (ARCH-004), group-commit storage batching (ARCH-005), incremental config snapshots (ARCH-006), and low-allocation request buffers (ARCH-007).
- **Existing RC9 Baseline Posture:** RC9 includes Project-DEK AES-256-GCM encrypted ExactCache in internal/ingress/cache/exact_cache.go [SOURCE_VERIFIED], zero-allocation policy evaluation [RUNTIME_VERIFIED], and RAM-resident ConfigSnapshot reads [RUNTIME_VERIFIED]. Key limitations: ExactCache is backed solely by synchronous SQL tables, applies only to non-streaming chat completions at temperature=0, ignores the documented X-Tanpopo-No-Cache header (exact_cache.go:389), and has zero Console policy UI controls [SOURCE_VERIFIED]; semantic caching and automatic upstream prompt-cache breakpoint injection are missing; and BlindHostCredentialBroker sets DisableKeepAlives: true on every request [SOURCE_VERIFIED].
- **Progressive Self-Hosted to Enterprise Experience:** Solo builders toggle exact response caching and automatic upstream prompt-cache optimization in one click on a route—immediately accelerating repeated CI tests, deterministic extractions, and multi-turn coding sessions with zero application code edits. Enterprises scale to two-tier RAM + Valkey encrypted exact caches across chat, responses, and embeddings, deploy tenant-prefixed semantic caches for high-volume support workloads via sandboxed CACHE_STRATEGY extensions, and enforce X-Tanpopo-No-Cache and Project-DEK crypto-shredding.
- **Technical Selections Deferred to `TEEP-03`:** In-memory L1 TinyLFU vs clock-pro eviction structures, L2 Valkey serialized envelope framing, vector similarity index bindings for CACHE_STRATEGY plugins, and hot-path sync.Pool arena sizing (ARCH-007) are deferred to TEEP-03.

| ID | Subdomain | Feature Name | Class | Disposition | Priority | Placement Candidate |
|---|---|---|---|---|---|---|
| `FEAT-044` | Deterministic Exact Response Caching | **Multi-Tier Encrypted Exact Response Cache, X-Tanpopo-No-Cache & Console Policy Controls** | `CAT-PROD` | `IMPROVE` | `P1-Enterprise-Core` | `CORE_DATA_PLANE` |
| `FEAT-045` | Tenant-Isolated Semantic Caching | **Tenant-Isolated Pluggable Semantic Response Cache (CACHE_STRATEGY)** | `CAT-PROD` | `ADD` | `P2-Differentiator` | `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` |
| `FEAT-046` | Upstream Vendor Prompt-Cache Optimization | **Automated Upstream Prompt-Cache Breakpoint Injection & Prefix Canonicalization** | `CAT-PROD` | `ADD` | `P1-Enterprise-Core` | `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` |

### FEAT-044 — Multi-Tier Encrypted Exact Response Cache, X-Tanpopo-No-Cache & Console Policy Controls

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-044` · `CAP` Traceability: `CAP-049` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-07` (Caching and Performance) -> **Deterministic Exact Response Caching**
- **3. Plain-Language Product Summary:** Delivers instant, zero-cost answers when identical deterministic AI requests repeat—such as CI test runs, document classifications, or repeated embeddings—while keeping every cached response encrypted with your project's Vault key. It adds fast memory/Valkey caching, honors the `X-Tanpopo-No-Cache` bypass header, and adds simple cache controls to the Web Console.
  - *Practical Example:* A CI pipeline runs 400 deterministic `temperature=0` extraction and `/v1/embeddings` checks on every pull request; L1/L2 Exact Cache serves unchanged prompts in `< 1 ms` `[HYPOTHESIS]` at `USD 0` upstream vendor cost, while a live troubleshooting tool passes `X-Tanpopo-No-Cache: true` to force a fresh upstream call.
  - *Explicit Trade-Offs:* Caching streaming requests requires replaying synthesized SSE chunks from the cached response envelope (optional toggle), and L1 RAM capacity must be strictly byte-bounded per pod so large embedding vectors cannot trigger OOM pressure.
- **4. User Problem & Business Value:** Eliminates redundant upstream token spend and multi-second inference latency on repeated deterministic classification, extraction, embedding, and CI evaluation queries while making cache behavior configurable and auditable in the Web Console.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-03 (Enterprise AI Platform Architect / SRE)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** PARTIAL_IMPLEMENTATION in `internal/ingress/cache/exact_cache.go` [SOURCE_VERIFIED]: hits synchronous SQL (`response_cache_entries`) on every lookup, supports non-streaming `/v1/chat/completions` (`temperature=0`) only, ignores the documented `X-Tanpopo-No-Cache` header (`:118 -> :389`), and is completely hidden from `RoutingPage.tsx` / `PoliciesPage.tsx`.
- **7. Proposed Complete Functional Behavior:** Upgrade `ExactCache` to a bounded L1 in-RAM cache + L2 Valkey `{org_id}` tier (falling back to SQLite/PG in Mode A standalone) while preserving mandatory `Project DEK` AES-256-GCM envelope sealing (`FMEA-03`) [DESIGN_PROPOSAL]. Extend coverage to `/v1/responses`, `/v1/messages`, and `/v1/embeddings` (plus optional deterministic SSE replay), honor both `Cache-Control: no-store/no-cache` and `X-Tanpopo-No-Cache`, emit `X-Tanpopo-Cache: HIT_L1 | HIT_L2 | MISS | BYPASS`, and expose `response_cache_*` policy controls + 1-click project cache purge in the Console.
- **8. User-Facing Subfeatures:**
  - Two-tier (`L1 RAM` + `L2 Valkey` / Mode-A SQL) `Project DEK`-encrypted Exact Cache across Chat, Responses, Messages, and Embeddings.
  - Strict `X-Tanpopo-No-Cache` and `Cache-Control: no-store` request bypass + `X-Tanpopo-Cache` diagnostic response header.
  - Console UI controls for cache enablement, TTL seconds, max entry size, eligible endpoints, and 1-click Project/Route Cache Purge.
  - FinOps `Saved Upstream Cost` KPI tracking exact micro-USD avoided via cache hits.
- **9. End-to-End Lifecycle:** Admin enables Exact Cache on Route/Policy -> Request arrives -> Check `X-Tanpopo-No-Cache` / `temperature` eligibility -> Compute tenant/policy-bound JCS SHA-256 key -> L1/L2 lookup & `Project DEK` AEAD open (or execute upstream & seal into L1/L2) -> Record `USD 0` upstream settlement + `avoided_cost_micros`.
- **10. Primary & Alternative User Journeys:** Primary: Repeated `/v1/embeddings` batch hits L2 Valkey across pods, decrypts under `Project DEK`, and returns with `X-Tanpopo-Cache: HIT_L2`. Alternative: Tenant rotates/shreds `Project DEK` (FEAT-074) or updates guardrail policy (`policy_digest` changes); prior cache entries become immediately unreachable.
- **11. Administrative & Permission Boundaries:** Cache keys forcibly bind `(org_id, project_id, env_id, policy_digest, model_deployment_id, tools_hash, jcs_request_hash)` so cross-tenant or cross-policy cache hits cannot occur; purge requires `ActionPolicyWrite`.
- **12. Automation Opportunities:** Automatic cache-key invalidation whenever a project's `EffectivePolicy` guardrail or system-preamble digest changes—preventing stale pre-guardrail answers from replaying after a security rule tightens.
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Zero plaintext cached prompts/completions in Valkey or RAM swap (`Project DEK` AEAD with AAD binding `org_id + project_id`), bounded memory quotas, and `< 1 ms` L1/L2 hit latency target `[HYPOTHESIS]`.
- **14. Core vs Extension Placement Candidate:** `CORE_DATA_PLANE` — L1 in-RAM + L2 Valkey lookup, `Project DEK` AES-256-GCM decryption/encryption, and `X-Tanpopo-No-Cache` bypass must execute inline in Data Plane ingress prior to upstream dispatch.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Response Cache Policy section & 'Purge Cache' action in `RoutingPage.tsx` / `PoliciesPage.tsx`; Cache Hit Savings KPI in `FinOpsPage.tsx`.
  - *CLI:* `tanpopo cache stats` and `tanpopo cache purge --project <id>`.
  - *Admin / Data-Plane API:* `response_cache_*` fields on `/admin/v1/policies` & `/admin/v1/routes`; `X-Tanpopo-No-Cache` & `X-Tanpopo-Cache` in `dataplane-v1.yaml`.
  - *GitOps / Terraform / Packs:* Declarative cache policy settings in route and policy manifests.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-059 (Vault `Project DEK` encryption, `FMEA-03`) and pairs with internal performance enablers `ARCH-004`..`ARCH-007`.` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** Any Valkey timeout or AEAD authentication failure treats the lookup as a clean `CACHE_MISS` (`fail-open to fresh upstream inference`, never returning corrupted data or failing the caller).
- **18. Security, Privacy & Financial Implications:** Cryptographic per-project sealing ensures even a compromised shared Valkey instance reveals zero plaintext cached responses, while deterministic hits cut upstream cost to `USD 0`.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Passing `X-Tanpopo-No-Cache: true` bypasses both read and write caching 100% of the time (closing the `exact_cache.go:389` bug).
  - Exact Cache accelerates `/v1/chat/completions`, `/v1/responses`, `/v1/messages`, and `/v1/embeddings` and is fully configurable from the Console UI.
  - Tightening a guardrail policy or rotating/shredding a `Project DEK` prevents replay of previously cached responses.
- **20. Relevant Evidence & Research Confidence:** HIGH — `exact_cache.go:118,389` `X-Tanpopo-No-Cache` omission and missing Console UI controls are [SOURCE_VERIFIED] on ac9ae87a; `< 1 ms` L1/L2 hit latency is `[HYPOTHESIS]`.
- **21. Proposal Classification:** `IMPROVE` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Upgrades RC9's SQL-only exact cache into a sub-millisecond RAM + Valkey cache across chat, responses, and embeddings, wires the ignored `X-Tanpopo-No-Cache` header, and exposes missing Console UI controls. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.0.0` · `SCEN-D`: `v1.0.0 (Core)`)*

---

### FEAT-045 — Tenant-Isolated Pluggable Semantic Response Cache (CACHE_STRATEGY)

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-045` · `CAP` Traceability: `CAP-050` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-07` (Caching and Performance) -> **Tenant-Isolated Semantic Caching**
- **3. Plain-Language Product Summary:** Caches answers to questions that mean the same thing even when phrased differently (for example, 'How do I reset my VPN token?' vs 'Steps to reset corporate VPN token?'). Unlike unsafe shared semantic caches, Tanpopo's Host strictly isolates every tenant, project, toolset, and security policy and encrypts cached answers with Vault keys so one customer or team can never receive another's cached answer.
  - *Practical Example:* An e-commerce company enables the opt-in Semantic Cache `.tpkg` (`cosine_similarity >= 0.96` via local quantized ONNX embeddings + `pgvector` / Valkey Vector) on its public Order-FAQ route, cutting repetitive upstream LLM calls while automatically bypassing semantic cache whenever per-user order tools are attached.
  - *Explicit Trade-Offs:* Semantic similarity can return false-positive matches on subtle negations ('Is drug X safe with Y?' vs 'Is drug X unsafe with Y?') or personalized contexts; therefore, it is **strictly opt-in per route**, defaults to high similarity thresholds (`>= 0.95`), and automatically bypasses requests containing tool results or per-user context headers unless explicitly configured.
- **4. User Problem & Business Value:** Competing semantic caches frequently share flat vector collections across keys/teams without cryptographic envelope binding, creating cross-tenant data leakage risks, whereas bundling heavy ML runtimes directly into Core would bloat Tanpopo's single binary.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-07 (FinOps Controller & IT Finance Analyst)`, `PER-09 (Community & Partner Extension Author)`
- **6. Existing RC9 Behavior & Limitations:** MISSING in RC9 [SOURCE_VERIFIED]: zero semantic cache contract (`CACHE_STRATEGY`) or vector similarity lookup exists.
- **7. Proposed Complete Functional Behavior:** Introduce the `CACHE_STRATEGY` `.tpkg` extension category (supporting Tier-2 Wasm or supervised Tier-3 sidecars for local quantized ONNX embeddings + `pgvector` / Valkey Vector / Qdrant backends, `TEEP-03` protocol deferred) [DESIGN_PROPOSAL]. Enforce a mandatory **Host-Owned Isolation Envelope**: the Host prepends a non-overridable partition key `(org_id, project_id, env_id, policy_digest, model_family, tools_hash)` and encrypts/decrypts cached response bodies exclusively on the Host using `Project DEK` so extensions store ciphertext + scoped vectors only.
- **8. User-Facing Subfeatures:**
  - Pluggable `CACHE_STRATEGY` `.tpkg` architecture keeping Tanpopo's core binary lightweight (`CGO_ENABLED=0`).
  - Host-enforced partition isolation `(org_id, project_id, env_id, policy_digest, model_family, tools_hash)` + `Project DEK` ciphertext sealing.
  - Configurable similarity threshold (`0.90..0.99`), TTL, negation/entity guard checks, and automatic bypass when tools/user-state are present.
  - `X-Tanpopo-Cache: HIT_SEMANTIC` header, similarity score trace metadata, and 1-click semantic partition flush.
- **9. End-to-End Lifecycle:** Install `CACHE_STRATEGY` `.tpkg` -> enable on specific FAQ/stateless route with threshold `0.96` -> Eligible request misses L1/L2 Exact Cache -> Host queries `CACHE_STRATEGY` within forced partition -> On `score >= threshold`, Host decrypts `Project DEK` payload & verifies AAD -> On miss, seals response after upstream completion.
- **10. Primary & Alternative User Journeys:** Primary: Paraphrased public FAQ query hits semantic cache (`score=0.974`), decrypts cleanly, and settles at `USD 0` upstream spend. Alternative: Request includes `tools` or `X-Tanpopo-No-Cache`; Host skips semantic lookup automatically to prevent personalized cross-user answers.
- **11. Administrative & Permission Boundaries:** Plugin installation governed by `OPERATOR` / `ORG_OWNER` trust policy (FEAT-108); enabling semantic cache on a route requires explicit opt-in and warns when attached to routes lacking stateless public profiles.
- **12. Automation Opportunities:** Shadow Dry-Run Mode (`semantic_cache_mode: SHADOW_LOG_ONLY`) lets teams measure hit rate and inspect would-be similarity matches in traces before serving semantic hits to live users!
- **13. Enterprise-Specific Requirements (`OPTIONAL_ECOSYSTEM_INTEGRATION`):** Cryptographic cross-tenant isolation (`Project DEK` AAD verification even if vector index were buggy), bounded plugin timeout (`<= 15 ms` default `[HYPOTHESIS]`), and instant invalidation via DEK rotation/purge.
- **14. Core vs Extension Placement Candidate:** `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` — Host Data Plane enforces non-bypassable cryptographic tenant/policy namespace prefixes and `Project DEK` payload sealing, while embedding generation and vector similarity search execute in pluggable Tier-2/Tier-3 `CACHE_STRATEGY` extensions.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* Semantic Cache plugin selector, Similarity Threshold slider, and `Shadow Dry-Run` mode in `RoutingPage.tsx`.
  - *CLI:* `tanpopo plugin install @tanpopo/semantic-cache-*` and `tanpopo cache purge --tier semantic`.
  - *Admin / Data-Plane API:* `semantic_cache_config` on `/admin/v1/routes` and `X-Tanpopo-Cache: HIT_SEMANTIC` in `dataplane-v1.yaml`.
  - *GitOps / Terraform / Packs:* Pinned `CACHE_STRATEGY` `.tpkg` + declarative route threshold configuration.
- **16. Dependencies & Cross-Domain Relationships:** `Depends on FEAT-044 (Exact Cache L1/L2 runs first), FEAT-059 (`Project DEK`), FEAT-100 (`CACHE_STRATEGY` host contract), and FEAT-101 (Tier-3 sidecar supervisor).` · Related Decisions: `DEC-CAND-02`, `DEC-CAND-11`
- **17. Failure & Recovery Behavior:** Strict `FAIL_OPEN_CACHE_MISS`: if the semantic cache sidecar/vector store times out or errors, the gateway proceeds directly to upstream inference with zero user-facing errors.
- **18. Security, Privacy & Financial Implications:** Prevents cross-tenant semantic cache poisoning and plaintext vector-store data breaches via Host-forced partitioning and `Project DEK` encryption.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Two tenants (or two projects within the same org) asking identical questions never match each other's semantic cache entries, and tampering with partition IDs fails `Project DEK` AAD decryption.
  - Requests carrying `tools`, `tool_choice`, or `X-Tanpopo-No-Cache` bypass semantic cache by default.
  - Killing or stalling the `CACHE_STRATEGY` extension degrades cleanly to `CACHE_MISS` upstream execution.
- **20. Relevant Evidence & Research Confidence:** MEDIUM-HIGH — Competitor semantic cache architectures and multi-tenant isolation gaps are [SOURCE_VERIFIED] (SRC-005, SRC-011, SRC-014); `< 15 ms` sidecar lookup target is `[HYPOTHESIS]`.
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P2-Differentiator`** — Unlocks high-hit-rate cost/latency savings on paraphrased FAQ and customer-support workloads via opt-in plugins without bloating Tanpopo Core or risking cross-tenant vector leakage. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.1` · `SCEN-C`: `v1.2` · `SCEN-D`: `Out-of-Band .tpkg / Tooling Wave 2`)*

---

### FEAT-046 — Automated Upstream Prompt-Cache Breakpoint Injection & Prefix Canonicalization

- **1. Feature ID & Original `CAP` Traceability:** `FEAT-046` · `CAP` Traceability: `CAP-051` · Catalog Class: `CAT-PROD` (`RETAINED`) · Linked Innovations: None
- **2. Functional Domain & Subdomain:** `DOM-07` (Caching and Performance) -> **Upstream Vendor Prompt-Cache Optimization**
- **3. Plain-Language Product Summary:** Automatically formats outgoing AI prompts so cloud providers (Anthropic, AWS Bedrock, OpenAI, Gemini, DeepSeek) reuse their upstream prompt caches at maximum efficiency—even when your application uses a generic SDK that knows nothing about vendor cache markers. It sorts tool definitions consistently so random JSON map ordering never invalidates a warm cache, and injects cache checkpoints automatically.
  - *Practical Example:* An internal Python agent using the standard OpenAI SDK sends 35 MCP tool definitions and a 6,000-token system prompt routed to Claude Sonnet 4 on Anthropic and AWS Bedrock; Tanpopo canonicalizes tool JSON key order and auto-injects optimal 5-minute `cache_control` / `cachePoint` markers on the system + tool prefix and rolling conversation tail, slashing input token costs on turns 2+ with zero Python code edits.
  - *Explicit Trade-Offs:* Injecting Anthropic/Bedrock prompt-cache writes incurs a `1.25x` (`5m` TTL) or `2.0x` (`1h` TTL) write surcharge on turn 1 that breaks even on turn 2; therefore, auto-injection triggers only when prefix length meets vendor minimum token thresholds (`>= 1,024` / `2,048` tokens) and never injects `1h` (`2.0x`) surcharges unless explicitly enabled on the route.
- **4. User Problem & Business Value:** Two subtle traps ruin prompt-cache hit rates in production: (1) Python/Go/JS map serialization jitter reordering `tools` JSON keys between turns (breaking exact prefix hashes on OpenAI/Gemini/Anthropic), and (2) unified `/v1/chat/completions` callers omitting vendor-specific `cache_control` / `cachePoint` blocks.
- **5. Target Personas:** `PER-02 (Startup / Mid-Market AI Product Engineer)`, `PER-07 (FinOps Controller & IT Finance Analyst)`
- **6. Existing RC9 Behavior & Limitations:** MISSING automatic optimization in RC9 [SOURCE_VERIFIED]: `@tanpopo/anthropic` passes through manually authored `cache_control` blocks, while unified callers without manual markers receive 0% Anthropic/Bedrock cache hits and `tools` JSON keys are not canonicalized.
- **7. Proposed Complete Functional Behavior:** Implement two synergistic optimizations [DESIGN_PROPOSAL]: (1) **Deterministic Prefix Canonicalization** (canonical key ordering on `tools[].function.parameters` JSON Schemas while preserving semantic tool order unless opted in, eliminating language map-iteration jitter); and (2) **Policy-Driven Breakpoint Injection** (`OFF`, `AUTO_5M_DEFAULT`, `EXTENDED_1H_OPT_IN`) placing up to 4 Anthropic `cache_control` / Bedrock `cachePoint` checkpoints (tools, system, penultimate turn) when minimum token thresholds are met, while respecting any caller-supplied explicit breakpoints.
- **8. User-Facing Subfeatures:**
  - Deterministic JSON Schema key canonicalizer preventing map-ordering jitter from busting OpenAI, Gemini, DeepSeek, and Anthropic caches.
  - Automatic Anthropic (`cache_control`) & AWS Bedrock (`cachePoint`) 4-slot breakpoint placement for unified chat/responses callers.
  - FinOps-safe TTL guard: defaults to `5m` ephemeral TTL and blocks `1h` (`2.0x` write cost) unless explicitly opted in on the route.
  - Upstream Prompt-Cache Hit Ratio & Net Savings (`cache_read_savings - cache_write_surcharge`) analytics in `FinOpsPage`.
- **9. End-to-End Lifecycle:** Route enables `upstream_prompt_cache: AUTO_5M` -> Request arrives -> Prefix JSON canonicalized -> Prefix Affinity Router (FEAT-025) picks warm connection -> Adapter checks token threshold & existing markers -> Injects optimal vendor breakpoints -> FinOps settles exact `cache_write` & `cache_read` tiers (FEAT-040).
- **10. Primary & Alternative User Journeys:** Primary: Legacy OpenAI-SDK app routed to Claude or Bedrock achieves warm `cache_read_input_tokens` on multi-turn conversations automatically. Alternative: Short single-turn 300-token classification call falls below the 1,024-token threshold; optimizer skips `cache_control` injection so no `1.25x` write surcharge is wasted.
- **11. Administrative & Permission Boundaries:** Configured per route by `TEAM_ADMIN` / `ORG_OWNER`; `EXTENDED_1H` mode displays an explicit `2.0x cache write rate` FinOps notice in the Console.
- **12. Automation Opportunities:** Enabled by default (`AUTO_5M`) on multi-turn agentic routes paired with `PREFIX_CACHE_AFFINITY` (FEAT-025).
- **13. Enterprise-Specific Requirements (`ESSENTIAL_SELF_HOSTED_AND_ENTERPRISE`):** Never overwrites caller-authored `cache_control` blocks when native `/v1/messages` clients manage their own 4-slot budget, and executes in single-pass streaming buffer pooling (`ARCH-007`).
- **14. Core vs Extension Placement Candidate:** `HYBRID_CORE_CONTRACT_PLUS_EXTENSION` — Core Data Plane canonicalizes JSON `tools` / `system` ordering deterministically, while official provider adapters inject vendor-native cache breakpoints (`cache_control`, `cachePoint`, `prompt_cache_key`) according to route policy.
- **15. Console, CLI, API & GitOps Touchpoints:**
  - *Web Console:* `Upstream Prompt-Cache Optimizer` mode (`Off / Auto 5m / Extended 1h`) in `RoutingPage.tsx` and Net Cache Savings metric in `FinOpsPage.tsx`.
  - *CLI:* `tanpopo route update --upstream-prompt-cache auto-5m`.
  - *Admin / Data-Plane API:* `upstream_prompt_cache_policy` on `/admin/v1/routes`.
  - *GitOps / Terraform / Packs:* Declarative prompt-cache optimization policy in route YAML.
- **16. Dependencies & Cross-Domain Relationships:** `Compounds directly with FEAT-025 (`INN-07` Prefix/KV-Cache Affinity Routing) and FEAT-040 (`5m` vs `1h` conditional pricing).` · Related Decisions: `DEC-CAND-02`
- **17. Failure & Recovery Behavior:** If a target model deployment does not advertise `prompt_caching` support in Model Intelligence metadata, breakpoint injection is cleanly skipped so unsupported models never reject unknown fields.
- **18. Security, Privacy & Financial Implications:** Upstream prompt caching stays strictly inside the customer's chosen provider account/region (honoring `ZDR` / residency policies) while preventing accidental `2.0x` `1h`-cache write bills on one-shot traffic.
- **19. Preliminary Measurable Acceptance Criteria:**
  - Permuting JSON object key order inside `tools[].function.parameters` produces byte-identical canonical outbound tool schemas.
  - Unified `/v1/chat/completions` multi-turn calls (`>= 2,048` prefix tokens) routed to Anthropic and Bedrock automatically inject valid breakpoints and record `cache_read_input_tokens > 0` on turn 2.
  - Calls already carrying 4 explicit `cache_control` blocks on `/v1/messages` are passed through untouched without exceeding Anthropic's 4-block limit.
- **20. Relevant Evidence & Research Confidence:** HIGH — Anthropic 4-slot `5m`/`1h` rules (SRC-051), Bedrock `cachePoint` (SRC-059), and OpenAI `prompt_cache_key` (SRC-048) are [SOURCE_VERIFIED].
- **21. Proposal Classification:** `ADD` (`RETAINED`)
- **22. Candidate Release Priority, Rationale & Scenario Affinity:** **`P1-Enterprise-Core`** — Captures 75–97.5% vendor prompt-cache discounts automatically for legacy OpenAI-SDK and multi-provider apps without forcing application engineers to hand-code Anthropic `cache_control` or Bedrock `cachePoint` blocks. *(Scenario Mapping: `SCEN-A`: `v1.0.0` · `SCEN-B`: `v1.0.0` · `SCEN-C`: `v1.1` · `SCEN-D`: `v1.0.0 (Core)`)*

---
