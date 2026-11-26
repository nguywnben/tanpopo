# CP108 — DEF-069 retest on PostgreSQL with A2A enabled

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Result:** FAIL, independently reproduces existing Major DEF-069 on PostgreSQL.

## Environment and authorization

Used exact RC6 artifact 45581179274 on isolated PostgreSQL 16 runtime 18133 with the supported `TANPOPO_A2A_ENABLED=true` flag and the existing healthy Valkey cluster. A first probe before enabling A2A returned 404 `A2A plane is disabled`, matching the default-off configuration documented in CP10; no product defect was inferred. The provider setup screen also had zero active adapters and clearly blocked connection creation pending adapter trust/activation, so no provider connection was fabricated.

Registered a synthetic loopback agent in project `QA004 CP107 PostgreSQL Scope`, explicitly opted in to private/loopback access with `127.0.0.1/32`, and used an environment-bound key restricted to the A2A plane and `/a2a/*`. A setup key that initially retained the default Provider plane was revoked before any request; the replacement key read back as A2A-only. Caller authorization was absent from the endpoint request.

## Reproduction

1. A local fixture accepted the A2A task, waited 10 seconds, and then returned HTTP 200 with `state: completed` and a synthetic artifact.
2. From the real MCP Hub → A2A Agents & Active Tasks screen, submitted a cookie-free synchronous task with a one-micro-USD hold. The persistent task reached Working v2; its screenshot showed Cancel available.
3. Clicked **Cancel** in the real Console. The request returned HTTP 200, the row showed the success notice, and API readback showed Canceled v3.
4. The synthetic downstream endpoint still sent its completed response normally (`completed_response_sent`). The original create call returned HTTP 400 `invalid_request`, `a2a: invalid SM-09 state transition: canceled -> completed`. Final task readback remained Canceled v3.

One earlier diagnostic attempt on this runtime closed its browser before waiting for the active row to render; the fixture observed the client disconnect and that task became Failed. That was a harness interruption. It is retained as such and was not used to classify the defect. The final successful reproduction above used the real rendered row and Cancel control.

A separate non-cancelled control task completed normally. The delayed request's initial in-flight screenshot showed Working; the create response later returned Completed v3, and a subsequent authenticated view and reload both showed Completed. This does not establish a persistent stale-state issue; the screenshots were taken at different points of the request lifecycle. The first aborted diagnostic is detailed in [harness evidence](../evidence/CP108-a2a-harness-interruption.json) and its [visible Failed row](../evidence/CP108-a2a-task-inspection.png). The normal completion/re-entry comparison is in [state reconciliation](../evidence/CP108-a2a-state-reconciliation.json), with [initial in-flight](../evidence/CP108-a2a-debug-before-nav.png), [still in flight](../evidence/CP108-a2a-debug-after-nav.png), [completed on re-entry](../evidence/CP108-a2a-state-on-reentry.png), and [completed after reload](../evidence/CP108-a2a-state-after-reload.png) screenshots.

## Evidence and impact

- Visually inspected [Working with Cancel](../evidence/CP108-a2a-postgres-working-before-cancel.png) and [Canceled after click](../evidence/CP108-a2a-postgres-canceled-after-cancel.png).
- [Sanitized PostgreSQL task, cancel, create-response, and fixture result](../evidence/CP108-a2a-postgres-cancel-results.json); [revoked A2A-only key state](../evidence/CP108-a2a-key-revoked.png).
- The separate fresh-install Providers screen showed the documented no-active-adapters gate before Marketplace activation; visually inspected [connection form](../evidence/CP108-provider-create-form.png). No connection request was submitted, and no adapter trust policy was weakened.

This is an independent second-storage-backend confirmation of DEF-069, not a new defect. The behavior risks continued processing of sensitive task input and downstream resource cost after operators receive a success notice; the pending caller then receives a generic transition failure and may retry. Acceptance remains the criteria in the [existing DEF-069 report](../reports/DEF-069-a2a-active-cancellation-does-not-stop-downstream.md), including stopping/signaling downstream work, a stable cancellation result to the original caller, late-response safety, and race/ledger/audit coverage.

The A2A-only key was revoked. The synthetic agent/task records remain only in the isolated QA database; the loopback fixture is test-only. No product code changed. **GATE-REL-07 remains BLOCKED.**
