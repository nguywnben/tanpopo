# `TP-IMP-0037` — MCP Side-Effect Firewall (`0` Retries on `mutating`/`destructive`), Dual-Mode JSON-RPC HITL Approvals & HMAC-Signed `Webhook` Dispatcher

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0037` |
| **Milestone / Epic** | `M3` (`v0.2 MCP Governance & Authorized OAuth 2.1`) · `EP-07` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0023`, `TP-IMP-0036` — `GAP-01, 08`)* |
| **Size** | `L` (`internal/ingress/mcp/firewall/*`, `internal/ingress/mcp/approval/*`, `internal/webhook/*`, `db/migrations/sqlite/0013_approvals_webhooks.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Duplicate side-effect prevention `FMEA-02`, 5-tuple parameter-hash-bound HITL approvals & SSRF-guarded HMAC Webhooks)* |
| **Technical Owner** | `Principal Security / Agent Governance Engineer` |

---

## 1. Intent
Complete execution-safety controls for MCP tool invocations alongside the outbound event notification subsystem (`TP-FR-0403`, `TP-FR-0404`, `TP-FR-0803`, `CR-08`, `CR-09`):
1. **Side-Effect Classification Firewall (`Side-Effect Classification Firewall` — `FMEA-02`):** Classify every MCP tool into `read_only`, `mutating`, or `destructive` (unclassified tools default to **`mutating`** per `FMEA-02`: blocking blind retries without paralyzing operator UX with manual approval modals on every tool). Hard-enforce **`max_retries = 0`** across all `mutating` and `destructive` tools upon ambiguous transport failures (`timeout` / `connection reset`), returning the canonical error code **`ambiguous_side_effect_timeout`** (`TP-FR-0404`).
2. **Standard `MCP 2025-03-26` Dual-Mode Human-in-the-Loop Approval (`Dual-Mode HITL Approval` — `SM-06`, `CR-08`, `COND-PO-04`, empirically verified via `CT-36` rather than assumed upfront):**
   - *Mode 1 (Bounded synchronous hold `<= 60 seconds`)* emitting standard MCP progress notifications for interactive connected agents.
   - *Mode 2 (Asynchronous JSON-RPC envelope — `CR-08`):* Returns a standard MCP JSON-RPC structured result/error inside HTTP `200`, supplying `approval_request_id` and a single-use `pre_approval_token` cryptographically bound to the **5-tuple `SHA-256(JCS({ org_id, virtual_key_id, server_slug, tool_name, arguments }))`** (`CR-08`) once approved by an authorized `SECURITY_REVIEWER` / `TEAM_ADMIN` / `ORG_OWNER`.
3. **`HMAC-SHA256`-Signed Outbound Event `Webhook` Dispatcher (`CR-09`):** Deliver reliable event notifications (`50%/80%/100%` budget thresholds, pending MCP approval requests, plugin quarantines, Kill-Switches) via `SSRFGuardDialer` signed with `X-Tanpopo-Signature-256` and anti-replay `X-Tanpopo-Timestamp` headers.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/ingress/mcp/firewall/classifier.go` (enforcing `0` retries on `mutating`/`destructive` tools plus dedicated rate limits for high-risk tool calls).
  - `internal/ingress/mcp/approval/workflow.go` (`SM-06` state machine: `PENDING -> APPROVED -> CONSUMED | REJECTED | EXPIRED`, verifying exact 5-tuple `JCS` hash equality on replay — rejecting any mismatch with **`parameter_hash_mismatch`** per `CT-22`).
  - `internal/webhook/dispatcher.go` (bounded exponential-backoff async delivery queue routed through `SSRFGuardDialer`, signing `HMAC-SHA256(secret, timestamp + "." + raw_body)`).
  - `internal/admin/http/mcp_approvals.go` & `webhooks.go` (`/admin/v1/mcp/*`, `/admin/v1/approvals/*`, `/admin/v1/webhooks/*`).
  - `db/migrations/sqlite/0013_approvals_webhooks.sql` (`approval_requests` supporting nullable `org_id`/`project_id` for Instance-scoped approvals, `webhook_subscriptions`, `webhook_deliveries` — 100% aligned with `TP-IMP-0003`).
- **Out-of-Scope:** Inter-agent `A2A` task lifecycle state machine (`SM-09` decoupled per `CR-08` and covered in `TP-IMP-0042`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0403`, `TP-FR-0404`, `TP-FR-0803`, `COND-PO-04`, `TP-BR-0603`, `SM-06`, `CR-08`, `CR-09`, `CT-22`, `CT-36`, `FMEA-02`.
- **Dependencies:** `TP-IMP-0023`, `TP-IMP-0036`.
- **Design Refs:** [`mcp-a2a-boundaries.md`](../../specifications/mcp-a2a-boundaries.md) (`SPEC-EXT-05`), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-02`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-08`, `CR-09`).

## 4. API, Wire & Data Contracts
- **5-Tuple Bound Single-Use Approval Invariant (`5-Tuple Bound Single-Use Approval` — `CT-22` & `CR-08`):** An approval token unlocks strictly a single call matching all 5 fields (`org_id`, `virtual_key_id`, `server_slug`, `tool_name`, and `JCS(arguments)`) and atomically transitions to `CONSUMED` during that first execution transaction.
- **Anti-Self-Approval Guard:** The caller/virtual key requesting a tool execution can never invoke the Admin API to approve its own request (only authenticated principals holding `SECURITY_REVIEWER` / `TEAM_ADMIN` / `ORG_OWNER` possess `mcp:approval:decide`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`FMEA-02` Zero Blind Retry on Mutating/Destructive Tool):** Given `stripe__refund_charge` (`mutating`) on `FakeMCPServer` accepting a request and abruptly resetting the TCP connection prior to responding, When invoked via `/mcp`, Then `FakeMCPServer` records **exactly 1 invocation** (`0` retries) and the caller receives **`ambiguous_side_effect_timeout`**.
- **`AC-2` (`CT-22` Sync & Async JSON-RPC Approval Flow):** End-to-end verification covers both `< 60s` synchronous hold approval and timeout fallback to an asynchronous JSON-RPC approval token, receiving a valid HMAC-signed `Webhook` notification, approving via the Admin API, and replaying the exact call successfully once.
- **`NEG-1` (`CT-22` Parameter or Key Bait-and-Switch):** Requesting approval with `{"env": "dev"}` and subsequently replaying the approved token with `{"env": "prod"}` (or under a different `virtual_key_id`) is hard-rejected with **`parameter_hash_mismatch`** and logged as a security audit event.
- **`NEG-2` (`Webhook SSRF`):** Registering a Webhook target pointing to `http://169.254.169.254/...` or `http://127.0.0.1:6379` is hard-blocked by `SSRFGuardDialer`.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/ingress/mcp/... ./internal/webhook/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/ingress/mcp/... ./internal/webhook/...`)*
- Race detector (`-race`): `go test -v -race ./internal/ingress/mcp/... ./internal/webhook/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `Webhook` signing secrets are stored in `vault` under `AEAD AAD`; outbound Webhook payloads pass through `privacy.Redactor` to prevent leaking sensitive fields to external Slack/PagerDuty channels.
- **Rollback:** Additive `0013_approvals_webhooks.sql` migration.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` & `TP-IMP-0002` (locking `SM-06` decoupled from `SM-09` & `CR-08` JSON-RPC contracts), `TP-IMP-0023`, and `TP-IMP-0036` are complete.
- **DoD:** `CT-22`, `CT-36` (`MCP 2025-03-26` HITL approval client compatibility per `COND-PO-04`), `FMEA-02`, and Webhook HMAC/SSRF suites passing under `-race`, with `REQUIRES_STRONG_REVIEW` sign-off achieved.
