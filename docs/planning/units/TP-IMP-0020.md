# `TP-IMP-0020` — 6-Step Capability-Aware Route Resolver, Hard `Data Residency` Fence & Sliding-Window `SM-04` Circuit Breaker

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0020` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-04` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0016`, `TP-IMP-0019` — `GAP-01, 08`)* |
| **Size** | `L` (`internal/routing/resolver/*`, `internal/routing/breaker/*`, `db/migrations/sqlite/0007_routes.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Prevents cross-border data-residency failover leaks & governs circuit-breaker state transitions)* |
| **Technical Owner** | `Staff Distributed Systems / Routing Engineer` |

---

## 1. Intent
Implement Tanpopo's deterministic 6-step routing algorithm per [`routing-and-policy.md`](../../specifications/routing-and-policy.md) (`SPEC-CORE-01` Sections 3 & 4): (1) resolve `LogicalModel` / `RouteRule` alias, (2) hard-filter by hierarchical policy & `Data Residency` fence, (3) hard-filter by required capability matrix (`tools`, `vision`, `json_schema`, `context_window`), (4) filter by connection health `SM-02` & deployment circuit breaker `SM-04`, (5) rank surviving candidates by strategy (`PRIORITY_FAILOVER`, `WEIGHTED_ROUND_ROBIN`, `LOWEST_COST`, `LOWEST_LATENCY_EWMA`), and (6) execute **Pre-Stream Failover Only** (strictly before flushing the first SSE byte) while recording per-hop `RouteAttemptTrace` diagnostics.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/routing/resolver/resolver.go` (6-step candidate selection algorithm + side-effect-free `Explain(req)` returning per-`ModelDeployment` inclusion/rejection reasons for debugging and future `Dry-Run` UI).
  - `internal/routing/breaker/circuit_breaker.go` (per-`ModelDeployment` sliding-window circuit breaker adhering strictly to canonical `SM-04` states: `PROVISIONED -> ACTIVE_HEALTHY <-> THROTTLED_COOLDOWN | CIRCUIT_OPEN_UNHEALTHY -> HALF_OPEN_PROBING -> DISABLED`, routing upstream `429` with `Retry-After` to `THROTTLED_COOLDOWN`, `500/502/503/timeout` infrastructure faults to `CIRCUIT_OPEN_UNHEALTHY`, and ensuring caller `400/422` validation errors **never** trip the breaker).
  - `internal/routing/failover/executor.go` (sequential candidate retry loop bounded by overall `request_deadline`, enforcing a hard `Data Residency` wall even when every compliant regional target is down — `CT-08`, `INC-04`).
  - `db/migrations/sqlite/0007_routes.sql` (`route_policies`, `route_targets`).
- **Out-of-Scope:** Interactive `Dry-Run Route Explain` Web Console UI (owned by `TP-IMP-0038` in `M3`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0102`, `TP-FR-0601`, `TP-FR-0602`, `TP-FR-0603`, `TP-BR-0501`..`0505`, `SM-04`, `CT-08`, `CT-12`, `INC-04`.
- **Dependencies:** `TP-IMP-0016`, `TP-IMP-0019`.
- **Design Refs:** [`routing-and-policy.md`](../../specifications/routing-and-policy.md) (`SPEC-CORE-01`), [`business-entity-map.md`](../../requirements/business-entity-map.md) (`SM-04`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-08`, `CT-12`).

## 4. API, Wire & Data Contracts
- **`SM-04` Circuit-Breaker Mathematics:** Transition to `THROTTLED_COOLDOWN` for the exact `Retry-After` duration upon upstream `429` (`CT-12`); trip to `CIRCUIT_OPEN_UNHEALTHY` when samples inside the `60s` sliding window reach `>= min_requests (10)` with error ratio `>= failure_ratio (0.50)` or `5` consecutive transport failures; after `open_cooldown (30s)`, advance to `HALF_OPEN_PROBING` admitting exactly `1` probe request.
- **Zero Residency Bypass on Failover (`CT-08` & `INC-04`):** `data_residency = ["EU"]` executes at Step 2 — a `US` fallback target is eliminated from the candidate set prior to ranking; if all `EU` targets return `503`, the Gateway fails closed with **`503 no_compliant_deployment_available`** rather than spilling traffic to `US`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given route `prod-assistant` with `Primary (Deployment A - EU)` and `Fallback (Deployment B - EU)`, When `Deployment A` returns `503` or connection-timeout prior to flushing SSE headers, Then the executor fails over transparently to `Deployment B` and records `attempt_count = 2`.
- **`AC-2` (`CT-12` `429 Retry-After` Cooldown & `SM-04` Probing):** Given `Deployment A` returns `429` with `Retry-After: 12`, When handled pre-stream, Then the Gateway immediately fails over to `Deployment B` and parks `Deployment A` in `THROTTLED_COOLDOWN` for exactly `12 seconds` (`FakeClock`); similarly, from `CIRCUIT_OPEN_UNHEALTHY`, advancing `31s` enters `HALF_OPEN_PROBING` where 1 successful probe restores `ACTIVE_HEALTHY`.
- **`NEG-1` (`CT-08` Residency & Capability Breach on Failover):** A route misconfigured with a `US` fallback target inside an `EU-only` project (or a fallback lacking `tool_calling`), when the primary target fails → fails closed with **`503 no_compliant_deployment_available`** (or `400 capability_not_supported`) with `0` packets sent to the non-compliant target.
- **`NEG-2` (`Client 400 Must Not Trip Breaker`):** Sending 100 malformed client `400 Bad Request` payloads → `SM-04` remains `ACTIVE_HEALTHY`.

## 6. Test Commands & Required Evidence
- Pure-Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/routing/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/routing/...`)*
- Data-race (`-race`): `go test -v -race ./internal/routing/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `RouteResolver` selects exclusively among `ModelDeployment` targets owned by the caller's `org_id` (or `Org`-shared resources explicitly granted to the `Project`); cross-organization connection routing is impossible.
- **Rollback:** Additive schema `0007_routes.sql`; defaults to direct 1:1 `LogicalModel` routing when custom route rules are absent.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (`SM-04` remediation), `TP-IMP-0016`, and `TP-IMP-0019` are complete.
- **DoD:** `100%` green across `SM-04`, `CT-08`, and `CT-12` under `-race` + `FakeClock`; passes `REQUIRES_STRONG_REVIEW`.
