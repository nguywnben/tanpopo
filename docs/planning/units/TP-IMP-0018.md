# `TP-IMP-0018` — Core `v0.1` `5 Official Reference Adapters` (`OpenAI`, `Anthropic`, `Gemini`, `OpenAI-Compatible/vLLM`, `Ollama` + `include_usage`)

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0018` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-03` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0014`, `TP-IMP-0016`, `TP-IMP-0017` — `GAP-01, 09`)* |
| **Size** | `L` (`extensions/official/openai/*`, `anthropic/*`, `gemini/*`, `openai-compatible/*`, `ollama/*`) |
| **Review Tier** | `STANDARD_REVIEW` *(`100%` public extension contract dogfooding — `0` privileged internal Core shortcuts)* |
| **Technical Owner** | `Platform Integrations Lead` |

---

## 1. Intent
Author and package the initial **`5 Official Reference Adapters` for `MVP (v0.1)`** per `CR-01` (`@tanpopo/openai`, `@tanpopo/anthropic`, `@tanpopo/gemini`, `@tanpopo/openai-compatible` for `vLLM`/`LM Studio`/TGI, and `@tanpopo/ollama`), enforcing strict architectural *dogfooding*: **all 5 packages consume the exact same manifest schema, Tier-1/Tier-2 runtime boundaries, and `tanpopo plugin test` harness available to community authors** (Core contains zero privileged `if provider == "openai"` branches), while automatically injecting `stream_options.include_usage = true` (or each provider's native equivalent) upstream so the budget ledger receives exact token counts at end-of-stream (`CR-09`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `extensions/official/openai/*` (Unified `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings` + `/native/openai/*` + automatic `stream_options: {"include_usage": true}` injection for ledger settlement when omitted by the caller, eliding the trailing usage chunk downstream if the caller did not request it).
  - `extensions/official/anthropic/*` (Bidirectional `messages`, `system`, `tool_use`/`tool_result`, multimodal image/PDF `blob_ref` mapping, SSE `message_start`/`content_block_delta`/`message_delta` normalization + `/native/anthropic/v1/messages`).
  - `extensions/official/gemini/*` (`contents`, `systemInstruction`, `functionDeclarations`/`functionCall`, and `streamGenerateContent?alt=sse` injecting credentials strictly via `x-goog-api-key` header rather than URL query parameters + `/native/gemini/*`).
  - `extensions/official/openai-compatible/*` (Canonical adapter for `vLLM`, `SGLang`, `TGI`, `LiteLLM upstream`, and internal OpenAI-compatible gateways with configurable `base_url` + explicit private-CIDR policy support).
  - `extensions/official/ollama/*` (Local `Ollama` adapter defaulting to `billing_mode = SELF_HOSTED_INFRA`, `auth_mode = NONE | BEARER`, and explicit administrator opt-in for loopback/LAN egress).
  - Canonical contract replay cassettes (`cassettes/*.yaml`) across all 5 providers.
- **Out-of-Scope:** `@tanpopo/aws-bedrock`, `@tanpopo/azure-openai`, and `/v1/rerank` (scheduled in `TP-IMP-0035` for `M3 / v0.2` per `CR-01`); **strictly zero browser-cookie or consumer-CLI subscription scraping connectors (`DEC-004`, `REJ-003`)**.

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0202`, `TP-FR-0301`, `TP-FR-0302`, `DEC-004`, `CR-01`, `CR-09`, `CT-06`..`CT-13`.
- **Dependencies:** `TP-IMP-0014`, `TP-IMP-0016`, `TP-IMP-0017`.
- **Design Refs:** [`provider-adapter.md`](../../specifications/provider-adapter.md) (`SPEC-EXT-02`), [`api-protocol-compatibility.md`](../../specifications/api-protocol-compatibility.md) (`SPEC-EXT-06`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-01`, `CR-09`).

## 4. API, Wire & Data Contracts
- **Zero Query-String Secrets (`Zero Query-String Secret`):** For `@tanpopo/gemini`, `BlindHostCredentialBroker` always injects the API key via `x-goog-api-key` rather than `?key=...` to prevent credential leakage in intermediate proxy access logs.
- **Normalized End-of-Stream Usage Extraction (`CR-09`):** All 5 Adapters populate `NormalizedUsage { InputTokens, OutputTokens, CachedInputTokens, ReasoningTokens }` from terminal SSE frames whenever supported upstream, minimizing fallback to `ESTIMATED` settlement.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given hermetic test mode `TANPOPO_TEST_HERMETIC=1`, When running `tanpopo plugin test` across all 5 `extensions/official/*` directories, Then all 5 pass `100%` of contract cases (`sync`, `SSE`, `tool_calls`, `vision/multimodal`, `error 401/429/500`, `native passthrough`) with `0` external Internet calls.
- **`AC-2`:** Given a caller invoking `POST /v1/chat/completions` with `stream: true` **without** `stream_options`, When routed through `@tanpopo/openai` or `@tanpopo/openai-compatible`, Then the Adapter still captures exact terminal `usage` for Tanpopo's ledger without breaking the caller's expected SSE frame sequence.
- **`NEG-1`:** Static AST/grep scan across `internal/` Core packages for hardcoded vendor hostnames (`"api.openai.com"`, `"api.anthropic.com"`) outside test files → yields `0` hits (proving vendor neutrality inside Core).
- **`NEG-2`:** `@tanpopo/gemini` never constructs outbound URLs containing `?key=`.

## 6. Test Commands & Required Evidence
- Pure-Go (`CGO_ENABLED=0`, running package & cassette verification across all 5 official adapters on every OS):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./extensions/official/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./extensions/official/...`
- Data-race (`-race`): `go test -v -race ./extensions/official/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Consume exclusively documented commercial APIs or legitimate self-hosted inference servers (`DEC-004`); each package declares its minimal disjoint `allowed_hosts`.
- **Rollback:** Each `.tpkg` adapter is independently versioned and can be rolled back or disabled individually without recompiling Core.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Product Owner signs off on the 5-adapter `v0.1` roster in `CR-01`; `TP-IMP-0014`, `TP-IMP-0016`, and `TP-IMP-0017` are complete.
- **DoD:** All 5 official adapters pack cleanly into `.tpkg` archives and achieve `VERIFIED` certification across canonical contract cassettes.
