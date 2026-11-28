# 08.04 — Functional Domain Research: MCP, Agent/A2A, Prompt Management, AI Evaluation & Context/RAG Governance (`DOM-12` .. `DOM-15`, `DOM-22`)

- **Document ID:** `TEEP-01-DOC-08-04`
- **Domains Covered:** `DOM-12` (MCP Infrastructure), `DOM-13` (Agent & A2A Infrastructure), `DOM-14` (Prompt Management), `DOM-15` (AI Evaluation), `DOM-22` (Context, Memory, RAG & Vector Governance)
- **Candidate Capabilities:** `CAP-083`..`CAP-103`, `CAP-143`..`CAP-146`

---

## 1. `DOM-12` — MCP Infrastructure (`CAP-083` .. `CAP-090`)

### 1.1 Bridging Both MCP Eras (`2025-03-26` Stateful + `2026-07-28` Stateless) Without Per-Call Taxes
- **RC9 Audit Gaps (`[VERIFIED_FACT]`):** `internal/ingress/mcp/hub` speaks stateful `2025-03-26` only (`2026-07-28` `server/discover` & stateless `tools/list` rejected with `400 invalid_session`), executes **4 sequential upstream HTTP requests per `tools/call`** (`VerifyAndCallTool` in `upstream_client.go:124-166`), strips `outputSchema`/`annotations`/`icons`, returns `-32601` on `resources/*` and `prompts/*`, leaves `SIDECAR_STDIO` & `MCP_SERVER` OAuth unwired, wipes `toolsByServer` on UI reload, and leaves `listMcpToolGrants`/`revokeMcpToolGrant` dead in `McpHubPage.tsx`.
- **Target Enterprise Capabilities (`CAP-083` .. `CAP-090`):**
  1. **Dual-Stack MCP Broker (`2025-03-26`..`2025-11-25` Stateful + `2026-07-28` Stateless HTTP) & Pooled Schema Cache (`CAP-083`):** Support `server/discover`, `Mcp-Method`/`Mcp-Name`, `MRTR` (`resultType`), `subscriptions/listen`, `CacheableResult` (`ttlMs`, `cacheScope: "private"`), `resources/*`, `prompts/*`, and `elicitation/create`, while caching verified upstream `tools/list` digests (`1` upstream RPC per `tools/call` instead of `4`!).
  2. **Semantic JCS Schema Pinning (`outputSchema` Included) & Rug-Pull Drift Classifier (`CAP-084`, `INN-11`):** Include `outputSchema` in `schema_pin`, and classify drift into `BENIGN_ADDITIVE`, `BREAKING_CONTRACT`, or `SUSPECTED_RUG_PULL_HIGH_RISK` (`GlassWorm` Unicode/prompt injection detection) + cassette dry-run gate.
  3. **Full MCP Server, Tool Inventory, Grant Lifecycle & Official Registry Sync (`CAP-085`, `PG-004`):** Add `GET /mcp/servers/{id}/tools`, `PATCH`/`DELETE` on `/mcp/servers`, wire `listMcpToolGrants`/`revokeMcpToolGrant` in `McpHubPage.tsx`, and support curated import from `registry.modelcontextprotocol.io/v0.1/servers` (`server.json`).
  4. **Native Async HITL Approvals via `io.modelcontextprotocol/tasks` & MRTR (`CAP-086`):** Preserve `SM-06` 5-tuple JCS `pre_approval_token` for `2025-*` clients while exposing native `2026-07-28` `CreateTaskResult` / URL-mode Elicitation for modern clients.
  5. **Zero-Tax Per-User OBO Tool Delegation (`RFC 9728` + `RFC 8707` + `RFC 8693`) (`CAP-087`):** Wire `oauth.Service` for `MCP_SERVER` + `BlindHostCredentialBroker` token exchange so downstream tools execute under the calling human's attenuated OAuth grant (eliminating shared-key confused-deputy bugs and Arcade.dev's `\$0.50/call` Pro tool fee).
  6. **Host-Governed Progressive Tool Search (`CAP-088`, `INN-12`), OpenAPI-to-MCP `TOOL_CONNECTOR` (`CAP-089`), & Sandboxed `SIDECAR_STDIO` + `PlaneMCP` FinOps (`CAP-090`):** Cut `50k`-token tool schemas by `50–85%` (`HYPOTHESIS / UNVERIFIED`) without unsafe JS `eval`, virtualize internal REST APIs into governed MCP tools, supervise containerized/sidecar `stdio` tools, and meter per-tool execution fees on `ledger.PlaneMCP`.

---

## 2. `DOM-13` — Agent and A2A Infrastructure (`CAP-091` .. `CAP-096`)

1. **Linux Foundation A2A `v1.0.1` Standard Wire Interop (`CAP-091`):** Serve standard `GET /.well-known/agent-card.json` + `GET /extendedAgentCard`, JSON-RPC 2.0 + HTTP+JSON (`application/a2a+json`: `/message:send`, `/message:stream`, `/tasks/{id}`, `/tasks/{id}:cancel`, `/tasks/{id}:subscribe`), 8 `TASK_STATE_*` enums (adding **`TASK_STATE_AUTH_REQUIRED`** for mid-task OBO step-up and **`TASK_STATE_REJECTED`**), member-discriminated `Part` (`SSRFGuard` on `Part.url`), and **JCS + JWS signed Agent Cards**.
2. **True Upstream Streaming Relay & Event-Driven SSE (`CAP-092`):** Replace RC9's buffered `Broker.Execute` and `1s` SQL polling (`task_coordinator.go:152,631`) with `Broker.ExecuteStream` + Valkey Pub/Sub live `statusUpdate`/`artifactUpdate` fan-out.
3. **Full A2A Agent Registry Lifecycle & Console Enablement Banner (`CAP-093`):** Add `GET/{id}`, `PATCH`, `DELETE`/disable on `/admin/v1/a2a/agents`, `--a2a-enabled` CLI flag, and explicit runtime status banner in `McpHubPage.tsx`.
4. **Monotonic `SM-09` Sub-Budget Carving, Hop-Count & Residency Extensions (`CAP-094` — `KEEP`/`IMPROVE`):** Preserve `sub_budget_hold_micros` (`DEF-085` pre-dispatch gate), `hop_count <= 5` cycle detection, and downstream cancel propagation (`DEF-069`) carried via official `A2A-Extensions`.
5. **SSRF-Guarded Push Notification Webhooks (`CAP-095`) & Cross-Plane Causal Agent Tree + Swarm Kill-Switch (`CAP-096`, `INN-10`):** Govern `TaskPushNotificationConfig` through `SSRFGuard` + `BlindBroker`, and link `LLM + MCP + A2A` calls under `X-Tanpopo-Trace-Root` (`max_tree_budget_micros` + 1-click `trees/{id}:abort`).

---

## 3. `DOM-14` — Prompt Management (`CAP-097` .. `CAP-099`) & `DOM-15` — AI Evaluation (`CAP-100` .. `CAP-103`)

### 3.1 Positioning Principle: Zero-ClickHouse Day-1 Governance, Open OTLP Interop Day-100
TensorZero's archival (`2026-06-12`) and Langfuse's 6-service stack prove Tanpopo should **not** bloat its core binary into an OLAP annotation warehouse (`[INFERRED_BEHAVIOR]`). Instead, Tanpopo should deliver **gateway-native operational prompt & evaluation primitives** that run directly on SQLite/PG16:
- **`DOM-14` Prompt Management (`CAP-097`..`CAP-099`):**
  1. **Versioned, Environment-Pinned Prompt Templates & Variable Schemas (`CAP-097`):** Store immutable versioned prompt templates (`prm_`, `dev`/`staging`/`prod` aliases, JSON Schema variable validation) referenced directly on `/v1/chat/completions` & `/v1/responses` (`x-tanpopo-prompt-id`), sealed under `Project DEK`.
  2. **Mandatory Governance System Preambles via `CONTEXT_TRANSFORMER` (`CAP-098`):** Org/BU policies injecting un-removable safety/compliance system instructions before routing.
  3. **GitOps Prompt Sync & Playground Diffing (`CAP-099`):** Export/import prompts in `SOLUTION_PACK`s and compare prompt versions side-by-side in `PlaygroundPage`.
- **`DOM-15` AI Evaluation (`CAP-100`..`CAP-103`):**
  1. **Deterministic Contract & Regression Cassette Runner (`CAP-100`):** Extend `EXT-ACCEPT-01` cassette infrastructure to user routes/prompts (`tanpopo eval run`), checking JSON Schema validity, tool-call structure, latency, and `micro-USD` cost ceilings in CI (`0` external SaaS required).
  2. **Budget-Capped Non-Blocking Shadow Canary Evaluations (`CAP-101`, `INN-02`):** Async `1%` live metadata/cassette evaluation bounded by a dedicated `shadow_budget_ledger`.
  3. **Online Structural Quality & Guardrail Score Telemetry (`CAP-102`):** Emit standard OTel `gen_ai.evaluation.result` events (`json_valid`, `tool_schema_valid`, `guardrail_triggered`, `refusal_detected`) to Phoenix/Langfuse/Braintrust.
  4. **CI/CD Promptfoo / Inspect Interop Gate (`CAP-103`):** Ephemeral CI OIDC workload tokens (`CAP-072`) scoped to staging budgets for external red-team/eval suites.

---

## 4. `DOM-22` — Context, Memory, RAG & Vector Governance *(Newly Discovered Domain — `CAP-143` .. `CAP-146`)*

1. **Gateway-Encrypted Stateless/Stateful Thread Hydration (`CAP-143`):** Support `previous_response_id` and `/v1/conversations` across *any* provider by storing `Project DEK`-encrypted turn envelopes (`store: false` upstream + `reasoning.encrypted_content`) in PG/SQLite with strict TTLs — enabling cross-account failover without `404` missing-thread failures (and failing loudly `400 capability_not_supported` when disabled, fixing RC9's `CT-08` silent-drop bug!).
2. **Context-Window Compaction & Token Pre-Flight Counting (`CAP-144`):** Unified `/v1/responses/compact`, `/v1/responses/input_tokens`, and `/v1/messages/count_tokens` + sliding-window summarization hooks before `MaxContextTokens` overflow.
3. **Vector-Space-Safe Embedding & Rerank Governance (`CAP-145`):** Enforce `(embedding_model_family, dimensions, encoding_format)` compatibility fences on `/v1/embeddings` routes so failover **never** corrupts a vector index by mixing incompatible embedding spaces, plus unified Gemini/Vertex/Bedrock/Cohere v2 (`input_type`) embedding support and fixing `"rerank"` shorthand in `routeAllowlistPermits` (`chat_completions.go:1174`).
4. **Governed RAG Subrequest Connectors (`CAP-146`):** Allow `CONTEXT_TRANSFORMER` plugins to query internal vector stores (pgvector, Qdrant, OpenSearch) via `BlindHostCredentialBroker` with post-enrichment Phase-1 budget holds.
