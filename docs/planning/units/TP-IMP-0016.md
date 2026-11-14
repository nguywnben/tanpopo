# `TP-IMP-0016` — Unified `/v1/responses`, `/v1/embeddings`, `/v1/models`, `Idempotency-Key` Deduplication & Multimodal `blob_ref` Streaming

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0016` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-03` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0015` — `GAP-01, 03`)* |
| **Size** | `L` (`internal/ingress/http/responses.go`, `embeddings.go`, `models.go`, `internal/ingress/idempotency/*`, `internal/ingress/blob/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Handles `<= 32 MiB` multimodal attachments defending against SSRF/OOM & `Org/Project`-isolated idempotency keys)* |
| **Technical Owner** | `Staff Gateway / Protocol Engineer` |

---

## 1. Intent
Complete the **Unified OpenAI-Compatible API** surface for `MVP (v0.1)` across `POST /v1/responses`, `POST /v1/embeddings`, and `GET /v1/models` (filtered strictly to models authorized for the caller's `Virtual Key`), enforce `Idempotency-Key` replay deduplication scoped tightly to `(org_id, project_id, vkey_id, idempotency_key)` + payload SHA-256 digest, and implement the Host out-of-band `blob_ref` multimodal streaming table (`CR-05`) so image/audio/PDF attachments up to `32 MiB` **never enter Wasm's `16 MiB` linear memory ceiling**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/ingress/http/responses.go` (`POST /v1/responses` synchronous & normalized streaming event lifecycle).
  - `internal/ingress/http/embeddings.go` (`POST /v1/embeddings` validating string/token-array inputs and output vector `dimensions`).
  - `internal/ingress/http/models.go` (`GET /v1/models` returning authorized `LogicalModel` entries enriched with `x_tanpopo_capabilities` metadata).
  - `internal/ingress/blob/store.go` (extract large inline `base64` `image_url` / `input_audio` / `file` parts into bounded `<= 32 MiB`/request Host `blob_ref` stream handles; **disable** automatic fetching of remote media URLs by default to prevent SSRF, or route strictly through `SSRFGuardDialer` + MIME/size caps when explicitly enabled by project policy).
  - `internal/ingress/idempotency/guard.go` (short-TTL `IN_FLIGHT` / completed response cache for non-streaming requests carrying `Idempotency-Key`).
- **Out-of-Scope:** `/v1/rerank` (scheduled in `TP-IMP-0035` for `v0.2` per `CR-01`); bidirectional realtime WebRTC/WebSocket audio (`TP-FR-0106` — `Later / Post-v1.0`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0101`, `TP-FR-0102`, `TP-NFR-1002`, `TP-BR-0503`, `CT-08`, `CT-09`.
- **Dependencies:** `TP-IMP-0015`.
- **Design Refs:** [`api-contracts.md`](../../specifications/api-contracts.md) (`SPEC-API-01`), [`api-protocol-compatibility.md`](../../specifications/api-protocol-compatibility.md) (`SPEC-EXT-06`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-05`, `GAP-03`).

## 4. API, Wire & Data Contracts
- **Strict Capability Enforcement (`No Silent Parameter Drop` — `CT-08`):** Whenever a request supplies `tools`, `response_format: json_schema`, multimodal attachments, or `reasoning_effort` targeting a model lacking that capability (without an explicit policy transform opt-in), the Gateway **must fail-fast prior to egress** with `400 capability_not_supported` naming the unsupported field — strictly never dropping parameters silently.
- **`Idempotency-Key` Contract:**
  - Matching `(org_id, project_id, vkey_id, key)` + matching `body_sha256` already settled → replay cached response with header `Idempotent-Replayed: true` without double-charging the budget ledger.
  - Matching `key` with mismatched `body_sha256` → reject with `422 idempotency_key_payload_mismatch`; while the initial call remains `IN_FLIGHT` → return `409 idempotency_request_in_progress`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given a multimodal request carrying a `20 MiB` `base64` image routed through a Tier-2 Wasm Adapter (`16 MiB` RAM ceiling), When calling `POST /v1/chat/completions` or `POST /v1/responses`, Then the Host offloads the binary payload into `blob_ref`, Wasm transforms only lightweight JSON metadata, and the Host splices the binary stream directly onto the outbound socket without breaching Wasm linear memory.
- **`AC-2`:** Given a `Virtual Key` restricted to `gpt-4o-mini` and `text-embedding-3-small`, When calling `GET /v1/models`, Then the returned list contains exclusively those 2 models (completely hiding all other org models).
- **`NEG-1` (`CT-08` Silent Parameter Drop Prevention):** Sending `response_format: {"type": "json_schema", ...}` to a model declaring `supports_structured_output = false` → rejected in `< 2 ms` with `400 capability_not_supported` and `0` upstream calls.
- **`NEG-2` (`Multimodal SSRF`):** Submitting `image_url: "http://169.254.169.254/latest/meta-data/"` → hard-rejected with `400 remote_media_url_disabled` (or `403 ssrf_blocked_ip` when remote media fetching is policy-enabled).
- **`NEG-3` (`Cross-Tenant Idempotency Isolation`):** `Org A` and `Org B` coincidentally sending identical `Idempotency-Key: req-123` headers → completely isolated with `0` cross-tenant response replay.

## 6. Test Commands & Required Evidence
- Pure-Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/ingress/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/ingress/...`)*
- Data-race (`-race`): `go test -v -race ./internal/ingress/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `blob_ref` buffers live strictly within the request lifecycle (scrubbed immediately via `defer`), never spill unencrypted temporary files to disk unless ephemeral encrypted overflow is configured, and never share `Idempotency-Key` namespaces across distinct `Virtual Key` identities.
- **Rollback:** Individual protocol endpoints can be toggled independently in the HTTP router without impacting `/v1/chat/completions`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (`blob_ref` `CR-05` contract lock) and `TP-IMP-0015` are complete.
- **DoD:** `100%` green across `CT-08`, `CT-09`, and the `20 MiB` multimodal payload test through a `16 MiB` Wasm Sandbox; passes `REQUIRES_STRONG_REVIEW`.
