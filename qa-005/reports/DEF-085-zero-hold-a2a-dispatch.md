# DEF-085 — A2A dispatch happens before zero-hold spend rejection

**Severity:** Major. **QA-005 status:** FAIL (exact RC7, isolated SQLite). **Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`. **Checkpoint:** CP115.

## Contract and evidence

The accepted boundary in `docs/specifications/mcp-a2a-boundaries.md` §3.2 requires the sub-budget hold before dispatch. TP-IMP-0042 AC-2 requires exact consumption settled against the allocated sub-budget. The frozen unit test covers zero-hold positive-spend rejection at transition time but does not verify no outbound invocation.

Two real `/a2a/tasks` requests with `sub_budget_hold_micros=0` were sent from an authorized A2A-only key to a deterministic loopback fixture. Both fixture calls ran and returned `input-required`, `spent_micros=1000`; only afterward the caller got 403 `budget_or_privilege_exceeded`. SQLite readback for both task IDs showed `FAILED`, cap 0, settled 0, no reservation. Audit event sequences recorded submitted → working → failed. No external provider or paid service was contacted; actual third-party charges are not evidenced.

Reproduction, sanitized task IDs/states/audit rows, fixture call log, credential cleanup, audit verification and visual interaction evidence are recorded in [CP115](../checkpoints/CP115-a2a-resume-and-zero-budget-dispatch.md) and [sanitized JSON](../evidence/CP115-a2a-lifecycle-zero-hold.json). Screenshots: `CP115-a2a-input-required-ui.png`, `CP115-a2a-resume-inflight.png`, `CP115-a2a-task-completed.png`.

## Expected and actual

- **Expected:** fail closed before dispatch if a sub-budget cannot authorize downstream work; any supported zero-cost operation must enforce its no-spend rule before side effects.
- **Actual:** dispatch happened, fixture work ran, then Tanpopo rejected reported spend and recorded a failed task with no internal settlement.

## Impact and confidence

A real downstream agent may perform billable or mutating work even though Tanpopo returns a budget error and records the task as failed. This is a potential unfunded-work and state-consistency risk; the synthetic reproduction proves ordering but not a real financial loss. Severity is Major because the accepted boundary explicitly places budget reservation before dispatch. Root-cause hypothesis: the synchronous request path allows zero-hold dispatch and relies on terminal transition validation after endpoint execution; confirm in engineering diagnosis.

## Fix acceptance

1. No outbound request is issued for a rejected/no-allocation task; a deterministic fixture must observe zero calls.
2. A valid positive reservation is persisted/held atomically before any dispatch.
3. Exact spend below/equal to the hold is settled; over-hold spend cannot trigger further calls and leaves an auditable terminal error without overstating settlement.
4. Validate synchronous, async, resumed, parent/child, cancellation, retry/timeouts and role/tenant isolation.
5. Preserve default-deny A2A plane, route, residency and hop boundaries.

## Residual tests

Repeat on PostgreSQL/Valkey, cover parent-child holds and real resumed tasks, exercise endpoint mutation on an over-hold response, verify trace/ledger/UI recovery, and independently review failure semantics. No product implementation was changed by QA.
