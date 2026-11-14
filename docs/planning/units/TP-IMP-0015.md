# `TP-IMP-0015` — `/v1/chat/completions` Vertical Slice (Sync + `< 1ms/chunk` SSE, `< 500ms` Cancel, Zero Mid-Stream Fallback) & `EXT-SLICE-M1`

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0015` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice` — **Gate `GATE-M1`**) · `EP-03` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0009`, `TP-IMP-0012`, `TP-IMP-0013`, `TP-IMP-0014` — `GAP-01, 02`)* |
| **Size** | `L` (`go.mod`, `internal/ingress/http/*`, `internal/ingress/sse/*`, `tests/integration/ext_accept_01_test.go`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Hot SSE Data Plane, stream-cancellation buffer hygiene & `M1` architectural vertical-slice proof)* |
| **Technical Owner** | `Staff Gateway / Data-Plane Engineer` |

---

## 1. Intent
Close the **Core Extensibility Vertical-Slice Proof of `Milestone M1` (`EXT-SLICE-M1`)**: wire `Virtual Key` authentication (`vk_tp_`, `TP-IMP-0009`), resolve `LogicalModel -> primary active ModelDeployment` directly from `ConfigSnapshot` (`M1` default target selection prior to multi-target `RouteResolver` landing in `TP-IMP-0020`), execute request/response transforms across both Tier-1 Declarative (`TP-IMP-0012`) and Tier-2 Wasm (`TP-IMP-0013`) Adapters, dispatch upstream egress through `SSRFGuard` + `BlindBroker` (`TP-IMP-0010`), emit `x-tanpopo-request-id` on every response (`TP-FR-0101`), pump `SSE` chunks at `< 1 ms/chunk` added overhead (`SLO-03`), propagate client disconnects upstream in `< 500 ms`, and enforce the **zero-fallback invariant once the first SSE byte has been flushed (`mid_stream_abort` — `FMEA-01`)**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (add `MIT`-licensed goroutine-leak detector `go.uber.org/goleak` for tests).
  - `internal/ingress/http/server.go` (Data Plane HTTP server under `internal/ingress` per `ADR-0001`, configurable request-body byte caps, explicit rejection of unsupported WebSocket/WebRTC upgrade handshakes per `TP-FR-0106`, and generation/propagation of `x-tanpopo-request-id` + `traceparent` — `CT-06`).
  - `internal/ingress/http/chat_completions.go` (`POST /v1/chat/completions` for both synchronous and `stream: true` requests, resolving the highest-`priority` active `ModelDeployment` of the target `LogicalModel` from `ConfigSnapshot`, and normalizing errors into `CanonicalError`).
  - `internal/ingress/sse/pump.go` (`text/event-stream; charset=utf-8` frame pump backed by `sync.Pool` buffers, flushing immediately after each event, latching `streamStarted` to forbid mid-stream retries/failovers, emitting `mid_stream_abort` on post-first-byte upstream drops, and severing the upstream socket within `< 500 ms` on downstream client disconnect).
  - `tests/integration/ext_accept_01_test.go` (automated `EXT-SLICE-M1` integration test proving hot installation of a brand-new `AcmeAI` `.tpkg` Adapter across both Tier 1 and Tier 2 **without editing or recompiling Core** → capability approval → blind secret binding → successful synchronous, SSE, `tool_calls`, and stream-abort `/v1/chat/completions` invocations).
- **Out-of-Scope:** `/v1/responses` and `/v1/embeddings` (`TP-IMP-0016`); extended steps of `EXT-ACCEPT-01` requiring `EU` Data Residency Routing (`TP-IMP-0020`), the \$1.00 Budget Ledger (`TP-IMP-0021`), `Blast-Radius` inspection (`TP-IMP-0023`), and background daemon `tanpopo serve` (`TP-IMP-0031`) — upgraded to all **10/10 steps of `EXT-ACCEPT-01`** in `TP-IMP-0032` (`GATE-M2`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0101`, `TP-FR-0103`, `TP-FR-0104`, `TP-NFR-0101`, `TP-NFR-0102`, `TP-NFR-1002`, `SLO-02`, `SLO-03`, `TP-BR-0503`, `TP-BR-0504`, `CT-06`, `CT-07`, `CT-10`, `CT-11`, `FZ-03`, `FMEA-01`, `EXT-ACCEPT-01` *(core `EXT-SLICE-M1` slice)*.
- **Dependencies:** `TP-IMP-0009`, `TP-IMP-0012`, `TP-IMP-0013`, `TP-IMP-0014`.
- **Design Refs:** [`system-design.md`](../../architecture/system-design.md) Sections 4.1 & 4.2, [`api-protocol-compatibility.md`](../../specifications/api-protocol-compatibility.md) (`SPEC-EXT-06`), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-01`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`EXT-ACCEPT-01`).

## 4. API, Wire & Data Contracts
- **Default `M1` Target Selection Rule:** Prior to `route_policies` landing, `ConfigSnapshot.ResolveDefaultDeployment(orgID, envID, modelSlug)` selects the highest-priority `ACTIVE_HEALTHY` (`SM-04`) `ModelDeployment` bound to an `ACTIVE` (`SM-02`) `ProviderConnection`; if none exists, returns `503 no_compliant_deployment_available`.
- **Anti-Mid-Stream-Splicing Invariant (`FMEA-01`):** The instant the first SSE byte is flushed to `ResponseWriter` (`streamStarted == true`), retries and failovers are `100%` locked out; if the upstream drops mid-stream, emit exactly 1 canonical `mid_stream_abort` error event and close the stream — never emit a synthetic `data: [DONE]`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`EXT-SLICE-M1` Zero-Core-Edit Proof):** Given a running Data Plane HTTP server, When packing a `.tpkg` via `TP-IMP-0014` for a net-new synthetic vendor (`AcmeAI`), installing via `TP-IMP-0011`, approving capabilities to `ENABLED_ACTIVE`, sealing `sk-tp-canary-...`, and calling `POST /v1/chat/completions` with `vk_tp_live_...` (`stream: false` & `stream: true` + `tool_calls`), Then every response includes `x-tanpopo-request-id` (`CT-06`) and conforms to OpenAI wire schemas (`CT-07`), `FakeLLMServer` receives the unsealed provider key, and `CanaryScanner` confirms `0` secret leaks.
- **`AC-2` (`CT-11` Downstream Abort Propagation `< 500 ms`):** Given `FakeLLMServer` trickling 1 frame every 100 ms, When the downstream client closes its TCP socket after frame #3, Then the upstream socket to `FakeLLMServer` closes within `< 500 ms` and `goleak.VerifyNone(t)` verifies `0` leaked goroutines.
- **`NEG-1` (`CT-10` / `FMEA-01` Mid-Stream Cut):** `FakeLLMServer` flushes 2 content chunks and resets TCP abruptly → client receives an explicit `mid_stream_abort` error event with `0` failover calls to secondary targets and `0` fake `[DONE]` markers.
- **`NEG-2` (`FZ-03` Malformed / Oversized SSE Line):** Upstream emits an unterminated `> 2 MiB` SSE line or malformed JSON → SSE scanner enforces bounded line caps and aborts cleanly without unbounded RAM allocation.

## 6. Test Commands & Required Evidence
- Pure-Go verification (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/ingress/... ./tests/integration/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/ingress/... ./tests/integration/...`
- Data-race (`-race`) & SSE frame parser fuzzing (`FZ-03`):
  - `go test -v -race ./internal/ingress/... ./tests/integration/...`
  - `go test -fuzz=FuzzSSEParser -fuzztime=30s ./internal/ingress/sse/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Strip all hop-by-hop headers and inbound `vk_tp_*` virtual keys prior to outbound egress; never echo raw unnormalized upstream error payloads to callers.
- **Rollback:** First end-to-end Data Plane milestone; listener endpoints can be disabled cleanly via listener configuration.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0009`, `TP-IMP-0012`, `TP-IMP-0013`, and `TP-IMP-0014` are `100%` green.
- **DoD:** Passes `GATE-M1` (`EXT-SLICE-M1`), `0` goroutine leaks (`go.uber.org/goleak`), clean `FZ-03` fuzzing, and `REQUIRES_STRONG_REVIEW` sign-off.
