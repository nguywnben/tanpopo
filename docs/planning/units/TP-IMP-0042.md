# `TP-IMP-0042` — `Tier-3 Isolated Sidecar` Runtime (`ConnectRPC` UDS/`mTLS`) & Inter-Agent `A2A` Plane (`AgentCard`, `SM-09`, Sub-Budget Ceilings & `hop <= 5`)

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0042` |
| **Milestone / Epic** | `M4` (`v0.3 Clustered HA, Enterprise Security & A2A` — **Gate `GATE-M4`**) · `EP-08` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0013`, `TP-IMP-0021`, `TP-IMP-0039` — `GAP-01, 08, 09`)* |
| **Size** | `L` (`go.mod`, `internal/extension/sidecar/*`, `internal/ingress/a2a/*`, `db/migrations/postgres/0002_a2a_sidecar.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Mutual-TLS `mTLS` sidecar process boundary, anti-recursion agent loop cap `hop_count <= 5` & anti-confused-deputy privilege narrowing)* |
| **Technical Owner** | `Principal Platform & Agent Systems Architect` |

---

## 1. Intent
Deliver the final architectural pillars of `v0.3` — closing **Gate `GATE-M4`**:
1. **Tier-3 `Isolated Sidecar` Execution Engine (`ADR-0002`, `CR-02` — `TP-FR-0308`):** Built for heavy enterprise security filters (such as local-model PII scrubbers) that cannot compile cleanly to Wasm: communicates via `ConnectRPC / gRPC` over permission-restricted (`0600`) Unix domain sockets or short-lived certificate-rotating `mTLS` (`AUTH-08`), strictly preserving the invariant that **a Sidecar never receives the Host credential vault**.
2. **Dedicated Inter-Agent `A2A Plane` (`TP-FR-0405`, `TP-FR-0406`, `DEC-003`, `CR-08`):**
   - Authenticated agent card discovery (`/.well-known/agent.json` / `/a2a/agents` catalog) and asynchronous task lifecycle coordination governed by its dedicated state machine **`SM-09`** (`SUBMITTED -> WORKING <-> INPUT_REQUIRED -> COMPLETED | FAILED | CANCELED` — cleanly separated from MCP's `SM-06` per `CR-08`).
   - **Monotonically Narrowed Privilege & Sub-Budget Ceilings (`Privilege & Sub-Budget Ceiling` — `TP-BR-0604`, `CT-23`):** When Agent A delegates a subtask to Agent B, the minted delegation token narrows effective permissions via intersection $P_{\text{child}} = P_{\text{caller}} \cap P_{\text{agent\_b}}$, caps the sub-budget strictly within the parent task's remaining balance, and enforces a hard hop ceiling on `X-Tanpopo-A2A-Hop-Count` at **`hop_count <= 5`** (plus `call_chain_ids` cycle detection) to extinguish runaway cost-amplifying agent ping-pong loops.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (adding `connectrpc.com/connect` & `google.golang.org/protobuf`).
  - `internal/extension/sidecar/client.go` & `mtls_creds.go` (`ExtensionRuntimeService` Protobuf client implementing `extension/port.ProviderRuntime`, mutual-TLS `mTLS` authentication, and per-sidecar circuit breaking on process crash or timeout).
  - `internal/ingress/a2a/registry.go` & `task_coordinator.go` (`/a2a/*` JSON-RPC + SSE endpoints persisting `SM-09` task progress in the canonical `a2a_tasks` table, enforcing `hop_count <= 5`, blocking `A -> B -> A` call cycles, and allocating sub-budget holds from `finops/ledger`).
  - `A2A Agents & Active Tasks` catalog tab added to `SCR-10` in `web/console/`.
- **Out-of-Scope:** Cross-organization autonomous economic negotiation protocols (outside `v1.0` scope).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0308` *(added in `TP-IMP-0001`)*, `TP-FR-0405`, `TP-FR-0406`, `DEC-003`, `TP-BR-0604`, `SM-09`, `CR-02`, `CR-08`, `CT-23`, `AUTH-08`.
- **Dependencies:** `TP-IMP-0013`, `TP-IMP-0021`, `TP-IMP-0039`.
- **Design Refs:** [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`mcp-a2a-boundaries.md`](../../specifications/mcp-a2a-boundaries.md) (`SPEC-EXT-05` Section 6), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-23`, `AUTH-08`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-02`, `CR-08`).

## 4. API, Wire & Data Contracts
- **No Confused-Deputy Privilege Escalation Invariant (`No Confused-Deputy Escalation` — `CT-23`):** A caller/virtual key permitted to invoke only `gpt-4o-mini` cannot leverage an internal `A2A Agent` to invoke restricted models or MCP tools that the original caller lacks permission to access (unless an explicit service-delegation policy with a dedicated sub-budget ceiling has been signed off by a `SECURITY_REVIEWER`).
- **Anti-Loop Invariant (`Anti-Loop Invariant` — `CT-23`):** Whenever `hop_count > 5` or `target_agent_id` already appears in the distributed trace's `call_chain` array, the request is rejected immediately with canonical error **`400 delegation_depth_exceeded`**.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`AUTH-08` Mutual-TLS Sidecar Isolation):** A legitimate Sidecar presenting a certificate issued by Tanpopo's ephemeral internal CA connects and executes hooks cleanly; abruptly killing the Sidecar (`SIGKILL`) trips its dedicated circuit breaker without impacting the Core Gateway process.
- **`AC-2` (`SM-09` Full Async A2A Task Lifecycle):** An inter-agent task transitions cleanly across `submitted -> working -> input-required -> working -> completed` over SSE and settles exact consumption against its allocated sub-budget.
- **`NEG-1` (`CT-23` Infinite Agent Ping-Pong Loop & Hop > 5):** Configuring `Agent A` to call `Agent B` and `Agent B` to call back `Agent A` (or chaining `A1 -> A2 -> ... -> A6`) halts deterministically with **`400 delegation_depth_exceeded`**, consuming `0` excess tokens.
- **`NEG-2` (`AUTH-08` Unauthenticated / Expired Cert Sidecar):** An unauthorized local process attempting to connect to the Sidecar RPC endpoint without a valid `mTLS` client certificate is rejected during the TLS handshake.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/extension/sidecar/... ./internal/ingress/a2a/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/extension/sidecar/... ./internal/ingress/a2a/...`)*
- Race detector (`-race`): `go test -v -race ./internal/extension/sidecar/... ./internal/ingress/a2a/...`
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Both `Tier-3 Sidecar` and `A2A Plane` ship **disabled (`disabled`)** by default in baseline configurations, activating only when explicitly enabled by an administrator.
- **Rollback:** Setting `runtime.sidecar.enabled = false` and `a2a.enabled = false` completely isolates both surfaces.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` & `TP-IMP-0002` (locking `TP-FR-0308`, `SM-09`, & `runtime.proto`), `TP-IMP-0013`, `TP-IMP-0021`, and `TP-IMP-0039` are complete.
- **DoD:** All Gate `GATE-M4` (`v0.3`) exit criteria satisfied, `CT-23` & `AUTH-08` passing under `-race`, and `REQUIRES_STRONG_REVIEW` sign-off achieved.
