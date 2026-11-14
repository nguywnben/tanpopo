# User Segments, Jobs-to-be-Done & Operational Use Cases — Tanpopo

> **Discovery Document (Phase ASTRA A01 — Gate G1: `READY_FOR_REVIEW`)**
> - **Last Updated:** `2026-11-11`
> - **Authored By:** Product Intelligence Lead
> - **Status:** Discovery analysis (`PROPOSED` / `ASSUMED`), not yet a locked requirements specification.

---

## 1. Organizational Segments & Go-to-Market Priority Ranking (Target Segments Ranking)

To avoid the classic open-source infrastructure failure mode — *"designing for a 10,000-employee bank on Day 1 so it feels bloated to developers, while lacking the compliance certifications required to actually sell to banks"* — we consolidate the 5 user profiles into **4 organizational segments** (combining legitimate `Solo Dev` & `AI Power User` workflows into one individual tier) and score them across 4 axes (1–5 scale): **Pain Intensity (Pain)**, **Early Adoption Speed / Low Procurement Friction (Adoption Speed)**, **Plugin Ecosystem Flywheel Value (Ecosystem Flywheel)**, and **0.x Execution Fit (Fit)**.

| Priority Rank | User Segment | Pain (1–5) | Adoption Speed (1–5) | Ecosystem Flywheel (1–5) | Fit 0.x (1–5) | Total Score | Strategic Role for Tanpopo (`PROPOSED`) |
|---|---|---|---|---|---|---|---|
| **#1 (Beachhead)** | **Dev Teams (Product Teams / AI Startups, 5–50 engineers)** | **5** | **5** | **4** | **5** | **19/20** | **Primary early-adopter beachhead:** Actively wiring 3–6 model providers + MCP servers into production apps & internal coding agents; suffering from surprise spend spikes, upstream outages, and Open-Core paywalls; empowered to trial and deploy in a single afternoon. |
| **#2 (Co-Beachhead Expansion)** | **AI Platform Teams (Platform groups at tech companies, 50–500 engineers)** | **5** | **4** | **5** | **4** | **18/20** | **High-quality Plugin creation engine:** Need to connect internal gateways, GPU self-hosted models (vLLM), AWS Bedrock/Vertex IAM, and internal MCP tools without waiting on upstream PR merges; represent the strongest contributors of **Provider Adapters / Private Extensions**. |
| **#3 (Community Flywheel)** | **Legitimate AI Power Users & Solo Developers (BYOK & Local Models)** | **3.5** | **5** | **4** | **5** | **17.5/20** | **Organic bottom-up adoption funnel:** Run multiple coding/agent tools (Claude Code, Codex, Cursor, OpenCode, etc.) alongside local Ollama/vLLM and dozens of MCP tools; want a lightweight gateway to manage keys, cap monthly spend, and inspect token usage. |
| **#4 (v1.0+ Target)** | **Traditional / Regulated Enterprise (Finance, Healthcare, Government, >1,000 employees)** | **5** | **1.5** | **2** | **2** | **10.5/20** | **Explicitly deferred past v0.1–v0.2:** Involves 6–12-month security procurement cycles, FIPS, dedicated HSM/Vault, 24/7 commercial SLAs, and air-gapped supply-chain audits. Targeted only once the platform matures at `v1.0+`. |

### Explicit Anti-Personas (`CONFIRMED_BY_OWNER` / `REJECTED`)
1. **Consumer Subscription Arbitrage Seekers ("Turn personal chat subscriptions into free APIs"):** Users solely looking to stuff browser cookies or OAuth tokens from personal ChatGPT Plus, Claude Pro/Max, or Google Antigravity plans into automated multi-agent loops in violation of provider terms. Serving this cohort invites direct legal liability and destroys credibility with serious engineering teams.
2. **Grey-Market Key Resellers:** Operators seeking prepaid voucher top-ups and multi-tier affiliate commission ledgers rather than a security-governed engineering gateway.

---

## 2. Four Functional Roles & Jobs-to-be-Done

In practice, a Solo Dev wears all 4 hats; a 20-person Dev Team typically splits them across 2 engineers; and an AI Platform Team/Enterprise separates them into 4 distinct functions. Tanpopo's RBAC and UX cleanly decouple all 4 roles:

| ID | Functional Role | Core Job-to-be-Done ("When..., I want to..., so I can...") | Primary Surfaces | Top Fear |
|---|---|---|---|---|
| **R1** | **Gateway Operator (`Gateway Operator / Infra Admin`)** | *"When deploying and maintaining our AI gateway, I want fast declarative startup and zero-downtime adapter installation without upgrading Core, so the platform achieves high uptime with minimal maintenance toil."* | CLI (`tanpopo`), GitOps config files, Docker/Helm, Web Console Health views | Gateway upgrades breaking live apps; a buggy plugin crashing the entire Gateway process. |
| **R2** | **API Consumer (`API Consumer / App & Agent Dev`)** | *"When building AI features or configuring agents, I want to call a standard endpoint using an issued virtual key, preserve frontier model capabilities, and immediately inspect error/trace logs on failure, so I ship faster without juggling 10 vendor SDKs."* | Unified API / Native Passthrough / MCP & A2A Endpoints, Playground & Trace Logs | An intermediary proxy stripping frontier parameters (thinking/caching), inflating streaming latency, or returning opaque error codes. |
| **R3** | **Extension Publisher (`Plugin / Adapter Publisher`)** | *"When a new AI provider launches or my company builds an internal model service, I want to scaffold an adapter from an SDK, validate it against automated contract tests, and publish via Git/Registry, so teams can adopt it same-day without waiting on Core maintainers."* | Plugin SDK, Test CLI (`plugin init/test/pack`), Contract Docs, Registry | Ambiguous contract specs; Core breaking internal hooks across minor releases; convoluted build/packaging toolchains. |
| **R4** | **Security, Governance & FinOps Admin** | *"When dozens of engineers and autonomous agents invoke models and MCP tools daily, I want to audit plugin permissions, lock down allowed MCP tools, enforce hard team budget ceilings, and revoke keys/plugins instantaneously during incidents, preventing data leaks and runaway spend."* | Web Console (Policies, Budgets, Audit Logs, Plugin Permissions), OTel/Webhook Alerts | A third-party plugin covertly exfiltrating master API keys; a runaway agent loop burning thousands of USD overnight; an MCP tool deleting production records. |

---

## 3. Nine Real-World Operational Use Cases (Happy Path & Failure Path)

Each scenario below details the canonical **Happy Path**, harsh real-world **Failure / Edge Paths**, and resulting architectural invariants for Tanpopo.

---

### UC-01: Multi-Provider Invocation via Unified API & Lossless Native Capability Preservation
- **Actors:** `R2 (Developer)` calling through a Gateway configured by `R1 (Operator)`.
- **Preconditions:** Official Adapters enabled for OpenAI, Anthropic, and Google Gemini; `R2` holds a Virtual Key authorized for target models.
- **Happy Path:**
  1. Application sends a standard `/v1/chat/completions` (or `/v1/responses`) request bearing a Virtual Key to Tanpopo.
  2. Tanpopo authenticates the key, resolves the target adapter by model identifier, translates the request payload accurately, streams SSE chunks back with minimal added latency, and settles exact token consumption into the ledger upon completion.
  3. For specialized workloads requiring vendor-exclusive features (e.g., multi-breakpoint `cache_control` directives on Anthropic `/v1/messages`), `R2` calls the native dialect passthrough route; Tanpopo forwards the native payload intact while still enforcing full authentication, budgets, and observability.
- **Failure / Edge Paths:**
  - *Failure 1 — Mid-Stream Upstream Disconnect:* Upstream returns `200 OK`, streams 40% of the SSE response, and abruptly drops the connection or emits an internal error event. The Gateway **must never** silently fail over and replay against a backup model mid-stream because the client has already consumed partial output! Instead, the Gateway cleanly terminates the SSE stream with a structured error event, settles accrued/estimated tokens, and flags `mid_stream_abort` in the trace.
  - *Failure 2 — Unsupported Parameter Capability:* `R2` requests strict `json_schema` enforcement or attaches audio input targeting a model lacking that capability. Rather than silently stripping parameters and producing malformed outputs, the Adapter checks its Capability Matrix and immediately returns an explicit `400 capability_not_supported` error before incurring upstream spend.

---

### UC-02: Connecting Self-Hosted / Local Inference Engines
- **Actors:** `R1 (Operator)` or `R2 (Solo/Team Dev)`.
- **Preconditions:** Internal GPU server running `vLLM` or `SGLang`, or workstation running `Ollama` / `llama.cpp`.
- **Happy Path:**
  1. `R1` configures an internal endpoint (e.g., `http://gpu-node.internal:8000/v1`) via the standard OpenAI-compatible/Ollama Adapter, declaring an internal cost schedule (e.g., `$0` or estimated GPU amortization per million tokens).
  2. Applications and coding agents use a single Tanpopo Virtual Key across both local and cloud models without exposing raw GPU server ports externally.
- **Failure / Edge Paths:**
  - *Failure 1 — VRAM Model Cold-Start Timeout:* `Ollama` takes 25–60 seconds to load a 32B model from disk into VRAM on initial invocation. Applying a rigid cloud-style 5-second connect timeout triggers false failures. Local-engine Adapters must support dedicated `first_byte_timeout` / cold-start budgets and readiness probes.
  - *Failure 2 — Context Overflow or Malformed Tool-Call Syntax:* A quantized model emits truncated JSON tool arguments or the GPU engine returns `CUDA out of memory`. The Gateway classifies internal infrastructure errors accurately to either trigger policy-permitted cloud failover or fail closed when data residency forbids leaving the private network.

---

### UC-03: Installing an External Provider Adapter Without Upgrading Core
- **Actors:** `R3 (Publisher)` publishes; `R4 (Security Admin)` approves permissions; `R1 (Operator)` installs.
- **Preconditions:** A regional AI vendor (or internal corporate AI gateway) launches a bespoke API absent from the shipped Tanpopo Core binary.
- **Happy Path:**
  1. `R3` scaffolds an adapter package using Tanpopo templates, declaring in its manifest: contract version `v1`, outbound HTTPS permission solely to `api.new-provider.example`, and Host-brokered header credential injection. `R3` passes automated contract tests and publishes to a Git release / Private Registry.
  2. `R1` runs installation from the URL/Registry. Tanpopo displays a permissions diff for `R4` or `R1` approval, verifies SHA-256 digests / signatures, loads the plugin inside its sandbox, and activates routing immediately with zero Core restart or recompilation.
- **Failure / Edge Paths:**
  - *Failure 1 — Sandbox Egress Violation:* Compromised plugin code attempts a covert connection to `evil-collector.example` to exfiltrate prompt payloads. Tanpopo's sandbox blocks any host absent from approved `allowed_hosts`, aborts the call, emits a critical security audit event, and optionally quarantines the plugin automatically.
  - *Failure 2 — Contract Version Mismatch or Infinite Loop:* Package requires Core contract `v2` on a `v1` host, or plugin code enters an infinite loop parsing malformed upstream JSON. Tanpopo rejects installation at manifest preflight (on version mismatch) or forcibly terminates the single execution exceeding CPU fuel/timeout quotas without impacting sibling Gateway streams.

---

### UC-04: Connecting via Authorized OAuth & Enterprise Identity
- **Actors:** `R1 (Operator)` / `R4 (Security Admin)`.
- **Preconditions:** Target model provider or cloud platform officially documents OAuth 2.1 (PKCE / Client Credentials) or cloud identity federation (Workload Identity / OIDC / IAM Role) for third-party integrations.
- **Happy Path:**
  1. `R1` selects an Adapter's legitimately declared `Authorized OAuth / Federated Identity` auth mode and completes standard browser consent or binds a Service Account / OIDC role.
  2. Tanpopo stores refresh tokens encrypted in its secret vault, refreshes access tokens transparently ahead of expiry for `R2 (Developer)`, and injects tokens exclusively into authorized upstream hosts.
- **Failure / Edge Paths:**
  - *Failure 1 — Token Revocation / Concurrent Refresh Rotation Race:* Cloud admin revokes app consent, or 50 concurrent requests simultaneously detect an expired access token and race to refresh (triggering upstream refresh-token reuse detection). Tanpopo's secret vault enforces single-flight token refresh locking, transitions the connection to `AUTH_REQUIRES_RECONSENT` upon receiving `invalid_grant`, and dispatches an immediate operational alert.
  - *Failure 2 — Attempted Misuse of Consumer Subscription Sessions:* A user attempts to stuff personal browser session cookies or demands a spoofed login flow for restricted consumer plans. Official Adapters and official documentation provide zero support for such mechanisms (`CONFIRMED_BY_OWNER` / `DEC-004`).

---

### UC-05: Capability-Aware Smart Routing & Pre-Stream Failover
- **Actors:** `R1 (Operator)` authors routing rules; `R2 (Developer)` invokes routes.
- **Preconditions:** A virtual route (e.g., `smart-production`) fronts a primary deployment (e.g., Anthropic on Vertex AI) and secondary fallbacks (e.g., direct Anthropic or AWS Bedrock).
- **Happy Path:**
  1. Request requires `context = 150k tokens` + `tool_calling = true` + policy constraint `data_region = EU`.
  2. Router disqualifies deployments lacking a 150k context window or located outside the EU, selecting the highest-priority healthy target; if target #1 returns `429 Rate Limit` or `503 Overloaded` prior to emitting response bytes, the Gateway fails over to compliant target #2 within milliseconds.
- **Failure / Edge Paths:**
  - *Failure 1 — Empty Intersection of Capability & Policy Constraints:* The sole surviving fallback resides in a US region while policy mandates EU data residency. The router **must never violate security/residency policy to salvage uptime** (strictly fail-closed on security/residency rules), returning `503 no_compliant_upstream_available` with an explicit block reason.
  - *Failure 2 — Cascading Retry Storm:* During widespread egress degradation, naive retries saturate connection pools. Per-deployment Circuit Breakers trip open immediately to shed load and preserve Gateway stability.

---

### UC-06: MCP Tool Governance, Scoping & Credential Masking
- **Actors:** `R4 (Security Admin)` defines ACLs; `R2 (Developer / Coding Agent)` invokes tools.
- **Preconditions:** Tanpopo fronts internal MCP servers (e.g., read-replica databases, issue trackers, documentation stores).
- **Happy Path:**
  1. Agent connects to Tanpopo's MCP Plane endpoint using a team Virtual Key.
  2. Tanpopo exposes only the filtered subset of tools permitted for that key (e.g., `read_ticket`, `search_docs`; completely hiding `delete_project`, `exec_sql_write`), supports standards-compliant cursor pagination without arbitrary 100-tool truncation, injects upstream service credentials transparently at egress, and writes granular tool-call audit records.
- **Failure / Edge Paths:**
  - *Failure 1 — Upstream Tool Poisoning ("MCP Rug-Pull"):* An external MCP server silently alters a tool's description text to embed prompt-injection instructions tricking the Agent into exfiltrating local files. Tanpopo detects drift against the `R4`-approved `tool_schema_hash` and immediately suspends the mutated tool pending re-approval.
  - *Failure 2 — Unauthorized Tool Call or Hung MCP Server:* Agent directly invokes a hidden tool name or the upstream MCP server hangs indefinitely. Tanpopo immediately rejects unauthorized calls (`403` security audit event) and enforces strict per-tool execution timeouts so Agent sessions never deadlock.

---

### UC-07: Emergency Security & Runaway Spend Incident Response
- **Actors:** `R4 (Security Admin)` & `R1 (Operator)`.
- **Preconditions:** A Virtual Key leaks to a public Git repository, or a buggy agent enters an infinite loop firing 500 requests/minute.
- **Happy Path:**
  1. Velocity anomaly detection triggers or `R4` invokes key revocation / emergency Kill-Switch via CLI or Web Console.
  2. The Virtual Key is invalidated across the entire data plane immediately; if an upstream provider key must be rotated, `R1` rotates it once inside Tanpopo without reconfiguring 20 unrelated internal applications.
- **Failure / Edge Paths:**
  - *Failure — Cluster Propagation Lag or Control-Store Partition:* If replica nodes cache key validity blindly for 15 minutes, attackers continue exploiting revoked credentials. Architecture enforces sub-second revocation propagation (pub/sub + short multi-second epoch verification) and `fail-closed` behavior on flagged/stale security states.

---

### UC-08: Hierarchical Budget Governance & Streaming Cost Settlement
- **Actors:** `R4 (FinOps/Security Admin)` configures caps; `R2 (Developer)` consumes quota.
- **Preconditions:** Team A is assigned a hard `100 USD / month` budget ceiling with a soft alert at `80 USD`.
- **Happy Path:**
  1. Crossing `80 USD` emits a soft-threshold webhook/console alert.
  2. Reaching `100 USD` cleanly blocks new billable Team A requests with `429 budget_exceeded` (or routes to a zero-cost local model if policy configures `downgrade-on-budget-cap`).
- **Failure / Edge Paths:**
  - *Failure 1 — Concurrent Streaming Overspend:* With `0.50 USD` remaining, an agent fires 40 parallel long-context SSE streams within the same millisecond. Deducting spend only *after* stream completion lets all 40 streams bypass initial checks and overshoot the cap by tens of USD. Tanpopo enforces **pre-request quota reservation** before opening upstream streams and reconciles exact spend on completion.
  - *Failure 2 — Missing Pricing Schedule or Dropped Upstream Usage Metrics:* On abrupt disconnects where upstream omits final token counts, the Adapter applies fallback token estimation flagged with `estimated_usage = true` so ledger entries never silently default to zero.

---

### UC-09: Plugin Upgrade, Version Pinning, Rollback & Emergency Quarantine
- **Actors:** `R1 (Operator)` & `R4 (Security Admin)`.
- **Preconditions:** Production traffic runs `provider-x-adapter@1.2.0`; `1.3.0` is released (or a critical vulnerability surfaces in an installed community plugin).
- **Happy Path:**
  1. `R1` runs upgrade preflight; Tanpopo validates existing connection configs against `1.3.0` schemas, stages the new package, shifts new requests to `1.3.0`, and gracefully drains in-flight `1.2.0` SSE streams.
  2. If `1.3.0` exhibits elevated error rates, `R1` executes a single rollback command reverting to the lockfile-pinned `1.2.0` artifact within seconds.
- **Failure / Edge Paths:**
  - *Failure — Emergency Malicious Plugin Quarantine:* Upon discovering a severe vulnerability in an external plugin, `R4` triggers `quarantine/revoke`. Tanpopo immediately terminates execution and network privileges for that plugin, fails closed on dependent routes with explicit client diagnostics, and exports an audit impact report enumerating every key and project that traversed the quarantined plugin.
