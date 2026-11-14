# `TP-IMP-0038` — `v0.2` Developer & Governance UX: `Dry-Run Route Explain` (`SCR-08`), Ephemeral `Playground` (`SCR-15`), `MCP Hub` (`SCR-10`) & Approval Queue

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0038` |
| **Milestone / Epic** | `M3` (`v0.2 MCP Governance & Authorized OAuth 2.1` — **Gate `GATE-M3`**) · `EP-07` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0020`, `TP-IMP-0029`, `TP-IMP-0030`, `TP-IMP-0037` — `GAP-01, 07`)* |
| **Size** | `L` (`internal/admin/http/route_explain.go`, `playground.go`, `web/console/src/pages/mcp/*`, `playground/*`) |
| **Review Tier** | `STANDARD_REVIEW` *(Cost-capped ephemeral Playground sessions `CR-10` & strict Astryx Design System compliance)* |
| **Technical Owner** | `Lead Developer Experience & Frontend Engineer` |

---

## 1. Intent
Deliver the interactive Web Console and CLI developer/governance tooling for `v0.2` (`TP-FR-0604`, `TP-FR-0904`, `CR-10`) — closing **Gate `GATE-M3`**:
1. **Zero-Side-Effect `Dry-Run Route Explain` (`TP-FR-0604`, `SCR-08` & `tanpopo route explain`):** Allow engineers to supply a target model, data-residency constraint, and required capability set (`tools`, `vision`, `context_tokens`) to inspect the deterministic 6-step routing decision tree: which `Deployment` candidates were pruned (whether by policy, missing capability, or `OPEN` circuit breaker), which target won, and the estimated budget hold — making `0` outbound network calls.
2. **Interactive `Playground` Backed by Ephemeral Session Keys (`Interactive Playground` — `SCR-15`, `CR-10`):** Instead of forcing operators to paste long-lived `Virtual Keys` into the browser or bypassing the FinOps ledger via admin cookies, the Backend mints a short-lived `Ephemeral Playground Key` (`TTL <= 15 minutes`, tight hard budget ceiling, auto-revoked on logout) routed directly through `/v1/*` so users can test SSE streaming, compare responses, and inspect live execution traces side-by-side.
3. **`MCP Hub` Console (`SCR-10`), `OAuth 2.1` Connection Flow (`SCR-04`) & HITL Approval Queue (`SCR-14`):** Register MCP servers, grant per-tool `<server>__<tool>` permissions, inspect `schema_hash` drift alerts, and `Approve / Reject` queued `destructive` tool calls with full canonical JSON argument inspection.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/admin/http/route_explain.go` (`POST /admin/v1/routes/explain`) & `internal/cli/route/explain.go` (`tanpopo route explain`).
  - `internal/admin/http/playground.go` (`POST /admin/v1/playground/sessions` — 100% aligned with the canonical path locked in `TP-IMP-0002` & `CR-10`, strictly scoped to the authenticated caller's permissions).
  - `web/console/src/pages/mcp/McpHubPage.tsx` (`SCR-10`).
  - `web/console/src/pages/playground/PlaygroundPage.tsx` (`SCR-15`).
  - Targeted feature additions on `SCR-04` (`OAuth 2.1` authorization button), `SCR-08` (`Route Explain` drawer), and `SCR-14` (`Approval Queue` tab).
- **Out-of-Scope:** `A2A AgentCard` catalog tab on `SCR-10` (surfaced in `v0.3` once `TP-IMP-0042` completes).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0604`, `TP-FR-0904`, `UJ-03`, `UJ-05`, `UJ-06`, `SCR-04`, `SCR-08`, `SCR-10`, `SCR-14`, `SCR-15`, `UX-AC-09`, `CR-10`.
- **Dependencies:** `TP-IMP-0020`, `TP-IMP-0029`, `TP-IMP-0030`, `TP-IMP-0037`.
- **Design Refs:** [`user-flows.md`](../../ux/user-flows.md) (`UJ-03`, `UJ-05`, `UJ-06`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-10`, `SCR-15`), [`routing-and-policy.md`](../../specifications/routing-and-policy.md) Section 5, [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-10`).

## 4. API, Wire & Data Contracts
- **Zero-Side-Effect Invariant on `Route Explain`:** Calling `POST /admin/v1/routes/explain` never decrements `RPM/TPM` rate-limit buckets, never creates FinOps budget holds, and never transmits packets to upstream AI providers.
- **`Playground` Safety Guardrails:** Principals with the `FINOPS_VIEWER` role cannot invoke `POST /admin/v1/playground/sessions`; all requests initiated from `SCR-15` settle normally into the selected project's FinOps ledger while preserving the default `0-byte` server-side prompt persistence rule (conversation turns reside solely in transient browser memory).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given a multi-target route spanning 4 deployments with distinct residency regions, capabilities, and circuit-breaker states, When running `tanpopo route explain` or inspecting via `SCR-08`, Then the dry-run explanation matches `RouteResolver`'s live routing decision `100%`.
- **`AC-2` (`UX-AC-09` Smooth `100 tok/s` Playground Stream):** End-to-end Playwright journeys pass at `100%` across `UJ-03` (authorized OAuth 2.1 grant), `UJ-06` (approving a gated `destructive` MCP tool call & surfacing `schema_hash` drift warnings), and `SCR-15` (`Playground` rendering a `100 tokens/s` SSE stream smoothly without dropped frames + mid-stream cancellation).
- **`NEG-1`:** When an ephemeral `Playground` session key expires after 15 minutes or the operator logs out, subsequent calls using that key are immediately rejected (`401`).
- **`NEG-2`:** Running `npx @astryxdesign/cli doctor --built dist` passes with `0 fail` (`0` raw `div`/`span` elements, `Badge` used strictly for numeric counts such as pending approval counts on `SCR-14`).

## 6. Test Commands & Required Evidence
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/admin/http/... ./internal/cli/route/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/admin/http/... ./internal/cli/route/...`)*
- Race detector (`-race`): `go test -v -race ./internal/admin/http/... ./internal/cli/route/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** The pending-approval argument inspector on `SCR-14` renders strictly escaped plain-text JSON preventing XSS and disables the `Approve` button whenever the underlying tool hash is drifted (`SCHEMA_DRIFT_BLOCKED`).
- **Rollback:** `Playground` can be disabled cleanly in locked-down production environments via `console.playground.enabled = false`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0020`, `TP-IMP-0029`, `TP-IMP-0030`, and `TP-IMP-0037` are complete.
- **DoD:** All Gate `GATE-M3` (`v0.2`) exit criteria satisfied, `astryx doctor --built dist` at `0 fail`, and `axe-core` accessibility audits clean.
