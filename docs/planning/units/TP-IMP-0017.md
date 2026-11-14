# `TP-IMP-0017` — Lossless Native Dialect Passthrough (`/native/{dialect}/{upstreamPath}`) & Same-Dialect Guard

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0017` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-03` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0015` — `GAP-01, 02`)* |
| **Size** | `M` (`internal/ingress/native/*`) |
| **Review Tier** | `STANDARD_REVIEW` *(Same-dialect routing invariants & zero-mutation token extraction)* |
| **Technical Owner** | `Staff Gateway / Protocol Engineer` |

---

## 1. Intent
Provide **Lossless Native Dialect Passthrough (`TP-FR-0105`)** under `/native/{dialect}/{upstreamPath...}` (e.g., `/native/anthropic/v1/messages`, `/native/gemini/v1beta/models/...`) so vendor-native SDKs and coding agents preserve `100%` of provider-specific features (`prompt caching`, `thinking/signature blocks`, `citations`, `batch`) while **still traversing the full Tanpopo governance pipeline: `Virtual Key` authentication, hierarchical policies, `SSRFGuard`, `BlindBroker`, micro-USD budget ledger, and OpenTelemetry tracing**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/ingress/native/handler.go` (canonicalize paths via `path.Clean` rejecting `..` traversal and smuggled query parameters; authenticate `Virtual Key` credentials carried in `Authorization: Bearer vk_tp_...`, `x-api-key: vk_tp_...`, or `x-goog-api-key: vk_tp_...` and strip `100%` of `vk_tp_*` headers before handing off to `BlindBroker` for upstream credential injection).
  - `internal/ingress/native/inspector.go` (non-destructively inspect `model` and `stream` hints and invoke `ExtractUsage` / `InspectNativeRequest` **without unmarshaling/re-serializing the request/response body in ways that drop unknown beta fields or perturb signed thinking blocks**).
- **Out-of-Scope:** Cross-dialect translation is strictly excluded on `/native/*` (e.g., never translate `/native/anthropic/v1/messages` into OpenAI wire format).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0105`, `TP-NFR-0102`, `TP-BR-0101`, `TP-BR-0503`, `CT-13`.
- **Dependencies:** `TP-IMP-0015`.
- **Design Refs:** [`api-protocol-compatibility.md`](../../specifications/api-protocol-compatibility.md) (`SPEC-EXT-06`), [`api-contracts.md`](../../specifications/api-contracts.md) (`SPEC-API-01`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-05`).

## 4. API, Wire & Data Contracts
- **Same-Dialect Routing Invariant (`CT-13`):** Requests entering `/native/anthropic/*` may route or pre-stream-failover exclusively to `ModelDeployment` targets declaring `native_dialect == "anthropic"`; targeting a mismatched dialect fails fast with canonical **`400 dialect_mismatch`**.
- **Upstream Path Allowlist (`Upstream Path Allowlist`):** `upstreamPath` must match the Adapter's declared path regex allowlist (e.g., `^v1/messages(/count_tokens)?$`), preventing callers from abusing `/native/*` to invoke upstream provider account/billing administration APIs.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`CT-13` Byte-Preserving Native Passthrough):** Given an Anthropic `/native/anthropic/v1/messages` payload containing `cache_control: {"type": "ephemeral"}`, a signed `thinking` block, and a forward-compatible `x_future_beta_field`, When dispatched through the Gateway (both synchronous and SSE `message_start / content_block_delta / message_delta`), Then `FakeLLMServer` receives every field byte-for-byte intact, the caller receives the exact native event stream, and the Gateway accurately extracts `input_tokens`, `cache_read_input_tokens`, and `output_tokens`.
- **`NEG-1` (`Virtual Key Leak & Dialect Mismatch`):** Passing a virtual key via `x-api-key: vk_tp_live_canary123` → `FakeLLMServer` receives only the upstream provider secret with `0` traces of `vk_tp_live_canary123`; routing to a non-matching dialect target returns `400 dialect_mismatch`.
- **`NEG-2` (`Path Traversal / Admin Endpoint Abuse`):** Requesting `/native/openai/v1/../organization/admin_api_keys` → blocked with `403 native_path_not_allowed` before opening any outbound socket.

## 6. Test Commands & Required Evidence
- Pure-Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/ingress/native/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/ingress/native/...`)*
- Data-race (`-race`): `go test -v -race ./internal/ingress/native/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Per-dialect `allowed_native_paths` is strictly `Default-Deny`; privileged upstream administrative headers outside the Adapter's explicit header allowlist are stripped.
- **Rollback:** Setting `gateway.native_passthrough.enabled = false` safely returns `404` on `/native/*` without affecting `/v1/*`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (locking `InspectNativeRequest` / native usage hooks in the extension contract) and `TP-IMP-0015` are complete.
- **DoD:** `100%` green across `CT-13` for `openai`, `anthropic`, and `gemini` dialects under `-race`.
