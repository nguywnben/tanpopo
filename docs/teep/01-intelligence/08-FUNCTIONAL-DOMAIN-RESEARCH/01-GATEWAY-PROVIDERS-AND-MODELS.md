# 08.01 — Functional Domain Research: Universal Gateway, Providers, Model Intelligence & Multimodal/Async Workloads (`DOM-01` .. `DOM-03`, `DOM-21`)

- **Document ID:** `TEEP-01-DOC-08-01`
- **Domains Covered:** `DOM-01` (Universal AI Gateway), `DOM-02` (Provider Management), `DOM-03` (Model Intelligence), `DOM-21` (Async, Batch, Realtime & Multimodal Media Workloads)
- **Candidate Capabilities:** `CAP-001`..`CAP-022`, `CAP-137`..`CAP-142`

---

## 1. `DOM-01` — Universal AI Gateway (`Founder Requirement 6.B`)

### 1.1 Strategic Objective & Market Reality
Developers using **Claude Code / Anthropic SDK (`@anthropic-ai/sdk`)**, **OpenAI Agents SDK (`openai`)**, or **Google GenAI SDK (`@google/genai`)** refuse to rewrite application code into a lossy `/v1/chat/completions` subset (`[VERIFIED_FACT]`). As proven in [`05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md`](../05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md), forcing GPT-5.4+/6.2 tool+reasoning workloads onto `/v1/chat/completions` fails outright, and stripping Gemini 3/4 `thoughtSignature` or Anthropic `signature` / `5m`/`1h` `cache_control` breaks multi-turn tool loops (`HTTP 400`) and destroys `90–97.5%` prompt-cache discounts.

### 1.2 RC9 Audit Baseline vs Gaps (`[VERIFIED_FACT]`)
- **Keep/Improve Strengths:** `/v1/chat/completions`, `/v1/embeddings`, `/v1/rerank`, `GET /v1/models`, `Idempotency-Key` payload hash guard (`CAP-006`), `sse.Pump` `FMEA-01` clean mid-stream abort (`CAP-007`), and `Trailer: X-Tanpopo-Usage-Accuracy` (`DEF-083`).
- **Critical Gaps:**
  1. **Unprefixed First-Party SDK Base URLs (`CAP-002`):** Anthropic SDKs append `/v1/messages` to `ANTHROPIC_BASE_URL` and Google SDKs append `/v1beta/models/{m}:generateContent` to `GOOGLE_GEMINI_BASE_URL`. RC9 requires `/native/{dialect}/...` path prefixes (`DEF-048` partial). Mounting top-level `/v1/messages` (+ `/v1/messages/count_tokens`) and `/v1beta/models/*` alongside `/native/{dialect}/*` gives zero-code-change first-party SDK compatibility.
  2. **Responses API Completeness (`CAP-003`):** RC9's `POST /v1/responses` (`responses.go`) is single-turn OpenAI-only, lacks `CT-08` rejection on unsupported `previous_response_id`/`conversation`/`background` when translating, restricts `reasoning_effort` to `[low, medium, high]`, and is unread by Tier-2 C/Wasm adapters. Must expand to full stateless encrypted reasoning (`include: ["reasoning.encrypted_content"]`), upward `/v1/chat/completions` → `/v1/responses` translation for GPT-5.4+/6.2, `/v1/responses/input_tokens`, `/v1/responses/compact`, and Tier-2 adapter support.
  3. **Native Passthrough Defects (`CAP-004`):** `native/handler.go:1798` blocks `?api-version=` (breaking Azure native); `:300` requires a JSON `model` hint (breaking `GET /native/openai/v1/models` and multipart requests); and `inspector.go:157` prepends a leading `/` (`"/model/..."`) while `aws-bedrock/src/adapter.c:654` checks `"model/"` without a leading `/` (breaking `aws-bedrock` native `Converse`).
  4. **Cryptographic Reasoning Signature Envelope (`CAP-005`) & Specialized Code/Decision Surfaces (`CAP-008`):** Preserve `thoughtSignature`, `signature`, `redacted_thinking`, and `encrypted_content` across multi-turn tool loops (failing loudly on foreign cross-vendor replay), and support `/v1/fim/completions`, `/v1/decisions`, and `/v1/moderations`.

---

## 2. `DOM-02` — Provider Management

### 2.1 RC9 Audit Baseline vs Gaps (`[VERIFIED_FACT]`)
- **8 Bundled Official Adapters (`CAP-009`, `CAP-010`):**
  - *Tier-1 Declarative:* `openai`, `openai-compatible`, `ollama` (needs `:cloud` tag residency fence under `ON_PREM`), `azure-openai` (needs Microsoft Foundry GA `/openai/v1/*`, `.services.ai.azure.com`, Entra ID OAuth/WIF, and embeddings/responses), `cohere-rerank` (expand to Cohere v2 `/v2/chat` & `/v2/embed`).
  - *Tier-2 Wasm:* `anthropic`, `gemini` (needs unified `/v1/embeddings` + Vertex AI `:generateContent`/`:rawPredict` OAuth/WIF), `aws-bedrock` (**fix 4 RC9 bugs:** restore multimodal blocks in `adapter.c:36-59`, remove hardcoded `claude-3-5-sonnet` probe at `:104`, accept `"/model/"` in `adapter.c:654` native inspector, allowlist `bedrock.<region>` + `bedrock-mantle.<region>.api.aws`, and block `global.*` inference profiles under regional residency!).
  - *New First-Party / Partner Adapters (`CAP-010`):* Dedicated presets for **CNCF `llm-d` / Kubernetes `GAIE`**, **`vLLM` / `SGLang` (`sgl-router`)**, **Google Vertex AI**, **Mistral (`/v1/ocr`, `/v1/fim`)**, **DeepSeek**, **Groq**, **Fireworks**, **Together**, and **Databricks**.
- **Connection Auth, Lifecycle & UX Gaps (`CAP-011`..`CAP-015`):**
  - **Fix `POST /admin/v1/connections` (`connections.go:798,808`):** Stop hardcoding `HOST_BLIND_INJECT` + `API_KEY` so `HOST_CRYPTO_SIGN` (`AWS_SIGV4`), `MTLS_CLIENT_CERT`, `OAUTH2` (`authorization_code_pkce`, `device_code`, `client_credentials` without requiring undocumented `TANPOPO_OAUTH_REDIRECT_URI`), and wired `WorkloadIdentityExchanger` (`AWS_IRSA`, `GCP_WIF`, `AZURE_ENTRA_WIF`) work end-to-end (`CAP-012`).
  - **Full Connection Edit, Inline Secret Sealing & `/v1` Normalizer (`CAP-013`, `CAP-014`):** Allow editing `allow_private_ip`, `allowed_cidrs`, `shared_project_ids`, `data_region`, `baa_covered`, and `upstream_zdr_verified`; support in-place secret rotation (`PUT /admin/v1/secrets/{id}`) without rebinding IDs; auto-normalize trailing `/v1` URLs; and surface sanitized probe errors in UI (`PG-013`, `CAP-011`).
  - **Provisioned Throughput (`PTU` / Reserved Tier) Quota Management (`CAP-015`):** Model dedicated Azure PTU / Bedrock Provisioned Throughput / Vertex Provisioned capacity with automatic overflow spillover to pay-per-token deployments.

---

## 3. `DOM-03` — Model Intelligence (`Founder Requirement 6.A`)

### 3.1 Transforming RC9's Manual Model Entry into a 4-Layer Intelligence Engine (`CAP-016` .. `CAP-022`)
1. **Precision-Safe Canonical Rate Card `v3` (`CAP-016`):** Migrate `RateCard` storage/API from `nano_usd_per_token` (which suffers rounding drift on **`1,664` real LiteLLM entries** `[VERIFIED_FACT]`) to exact 64-bit **`nano_usd_per_1m_tokens` ($10^{-15}$ USD/tok)** via `u128` ceiling math, adding `5m` vs `1h` cache write rates, context-length tiers (`>100k`, `>200k`, `>272k`), `priority`/`flex`/`batch` service tiers, audio tokens, per-page OCR, search-query fees, and dual-unit Console inputs (`$ / 1M tokens` ↔ integer `nano-USD`). Expose the 3 omitted `RateCard` fields in `admin/http/models.go:60-71`.
2. **Signed Offline Model Intelligence Packs & `Decimal` Sync Compiler (`CAP-017`):** Ship an Ed25519/TUF-signed embedded catalog compiled from reconciled `models.dev` (`10,078` models) + `litellm_model_prices.json` (`7,193` models, sanitizing `22,940` sci-notation literals & `66` float artifacts) + `openrouter` metadata, updatable offline (`tanpopo catalog import-pack`) or via opt-in `CATALOG_FEED` staging (`> ±25%` price-swing quarantine).
3. **BlindBroker Account-Authenticated Model Discovery & 1-Click Bulk Onboarding (`CAP-018`):** Execute `DiscoverModels` probes (`/v1/models`, Bedrock `ListFoundationModels` + `ListInferenceProfiles`, Azure Resource/Foundry deployments mapping custom deployment slug → canonical model, Vertex publisher models, Ollama `/api/tags` + `/api/show`) into `connection_discovered_models` for 1-click bulk registration.
4. **Sticky Manual Overrides, Provenance & Historical Settlement Immutability (`CAP-019`):** Enforce `MANUAL_OVERRIDE > PROVIDER_API > CATALOG_FEED > SIGNED_PACK` precedence with field-level provenance, non-destructive `drift_advisory` notices, and frozen `usage_settlements.applied_pricing_snapshot_json`.
5. **Full Logical Model & Deployment Lifecycle (`CAP-020`, `PG-004`):** Add `GET/{id}`, `PATCH`, and `DELETE`/retire on `/admin/v1/models` and full rate-card/capability `PATCH` in Console `ModelsPage.tsx` with dependency guards.
6. **Deprecation Monitoring, License-Compliance Metadata & Migration Advisor (`CAP-021`, `CAP-022`, `INN-02`):** Track `deprecation_date` / `sunset_at_ms` (`T-90d/30d/7d` alerts), flag non-OSI/regional model license restrictions (such as **Meta Llama 4's EU domicile multimodal restriction** `[VERIFIED_FACT]`), and generate capability-verified route migration patches.

---

## 4. `DOM-21` — Async, Batch, Realtime & Multimodal Media Workloads *(Newly Discovered Domain)*

### 4.1 Why Synchronous Request-Response Proxying Is Insufficient (`CAP-137` .. `CAP-142`)
1. **Durable Async Batch & Background Jobs (`CAP-137`):** Support `/v1/batches`, Anthropic `/v1/messages/batches`, and OpenAI `/v1/responses` (`background: true` `2h`/`8h` windows) using **durable long-horizon `BATCH_ESCROW` holds** exempt from the `90s` `SM-07` orphan reaper, reconciling `50%`–`40%` batch discounts upon job completion.
2. **Tenant-Isolated Files API Broker (`CAP-138`):** Govern `/v1/files` (`POST/GET/DELETE`) with virus/DLP pre-scan, pinning provider-local `file_id`s to `(org_id, project_id, connection_id)` so routes never fail over a connection-local `file_id` to a foreign account.
3. **Realtime Bidirectional Audio/Voice Gateway & Constrained Ephemeral Tokens (`CAP-139`):** Two complementary modes for OpenAI `/v1/realtime` and Gemini `BidiGenerateContent`: (a) **Constrained Ephemeral Session Token Minting** (`/v1/realtime/client_secrets` / `BidiGenerateContentConstrained` locking model, system instructions, tools, and max duration upfront with pre-reserved audio budget), and (b) **Full Governed WebSocket Proxy Plane** with periodic `DeltaReserve` audio-token metering and stream kill-switch severing.
4. **Multimodal Image, Video, Speech & Transcription Endpoints (`CAP-140`):** Unified & native support for `/v1/images/{generations,edits}`, `/v1/audio/{transcriptions,translations,speech}`, and streaming partial image/transcript SSE with exact per-pixel/per-second/audio-token settlement.
5. **Document OCR & Structured Extraction Plane (`CAP-141`):** First-class `/v1/ocr` (Mistral OCR, Azure Document Intelligence, local sidecars) metered per page (`DocPerPageNanoUSD`) while streaming PDFs through `blob.Store` (`<= 32 MiB`).
6. **Binary AWS EventStream & NDJSON Framing Hardening (`CAP-142`):** Dedicated length-prefixed `application/vnd.amazon.eventstream` parser alongside `sse.Pump` so `> 2 MiB` Bedrock `ConverseStream` frames never rely on incidental `0x0A` newline bytes ([`04-CURRENT-TANPOPO-PRODUCT-AUDIT.md`](../04-CURRENT-TANPOPO-PRODUCT-AUDIT.md) §3.1).
