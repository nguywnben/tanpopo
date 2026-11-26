# DEF-067 — A2A synchronous create rejects the supported `working` response

**Severity:** Moderate  
**Status:** FAIL (confirmed on frozen RC6)  
**Affected workflow:** A2A task submission, asynchronous agent handoff, task monitoring and cancellation  
**Baseline:** `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`

## Summary

A synchronous A2A task create dispatches to an endpoint that returns a valid top-level `state: "working"`. Tanpopo rejects the response as an invalid `working -> working` transition, returns HTTP 400 without the task ID, and leaves a persisted task marked Failed in the Console. This blocks an agent from acknowledging that work continues asynchronously. The failed task has no cancel action.

This was reproduced with an isolated A2A-enabled copy of the exact RC6 artifact and a synthetic loopback endpoint. The registration explicitly opted in to `127.0.0.1/32`; the temporary Virtual Key allowed only the A2A plane and `/a2a/*`. The endpoint received one request with no caller Authorization header, as required by the broker boundary. No provider spend occurred. The test key was revoked and the fixture stopped.

## Reproduction

1. Enable the optional A2A plane in an isolated RC6 runtime. Sign in as the synthetic Org Owner.
2. Register a synthetic A2A agent at `http://127.0.0.1:18133`, opt in to private endpoint access, and allow only `127.0.0.1/32`.
3. Issue a Virtual Key bound to that project/environment with **A2A only** and route `/a2a/*`.
4. Configure the local agent to return HTTP 200 with `{"state":"working","spent_micros":0,"artifacts":[]}`.
5. Using the A2A key, send `POST /a2a/tasks` with the registered `target_agent_id`, one synthetic text input, `sub_budget_hold_micros: 0`, and `dispatch_async: false`.
6. Observe HTTP 400 `invalid_request`, message `a2a: invalid SM-09 state transition: working -> working`. The error request id is `trc_01m8a7c2qve00aa3g45b6vj8qt`.
7. Open the real MCP Hub → **A2A Agents & Active Tasks** screen. A new row exists for `qa004-cp65-cancel-agent` in **Failed** state, with zero hold and no actions. Authorized `GET /a2a/tasks/tsk_01m8a7c2qhe00bwdxd8zx1zj2t` returns HTTP 200, state `failed`, version 3, and zero hold/settled cost. The failed create response did not include that task ID.

The deterministic, loopback-only responder is included as [`CP65-a2a-working-fixture.py`](../evidence/CP65-a2a-working-fixture.py). The synthetic endpoint response follows the product's `UpstreamTaskResponse` state contract, which explicitly includes `working`. The SRS and A2A task contract enumerate `submitted -> working <-> input-required -> completed | failed | canceled`.

## Expected versus actual

| Expected | Actual |
|---|---|
| A valid upstream `working` result leaves a created task observable in `working` state (with its task ID returned), or follows a documented accepted asynchronous response contract. | HTTP 400 is returned; the submitted task is transitioned to Failed and remains in the A2A task table without an ID in the create response. |
| The active task can be monitored and canceled according to SM-09. | Failed row has no cancellation action; caller cannot correlate its failed creation response to the persisted task. |
| Zero-cost/zero-hold task remains at zero and state transitions reconcile. | This probe incurred zero cost and zero hold, but the lifecycle state is inconsistent with the supported upstream response. |

## Evidence and confirmed mechanics

- [Sanitized API, persisted state and cleanup results](../evidence/CP65-a2a-lifecycle-results.json)
- [Visually inspected failed-task inventory](../evidence/CP65-a2a-working-response-failed-task.png)
- [Task audit sequence and intact chain verification](../evidence/CP66-a2a-audit-events.json)
- [Submitted task with Cancel action](../evidence/CP65-a2a-submitted-before-cancel.png)
- [Canceled task and success toast](../evidence/CP65-a2a-submitted-canceled.png)

The repository source identifies the mechanism:

- `CreateTask` creates a submitted task, transitions it to working before dispatch, then applies the endpoint's state response ([`task_coordinator.go`](../../internal/ingress/a2a/task_coordinator.go#L811)).
- `UpstreamTaskResponse.State` documents `working | input-required | completed | failed` ([`task_coordinator.go`](../../internal/ingress/a2a/task_coordinator.go#L100)).
- `validSM09Transition` for a task already in working accepts input-required, completed, failed, or canceled, but not working again ([`task_coordinator.go`](../../internal/ingress/a2a/task_coordinator.go#L1714)).
- On response-transition error, the create path attempts a failure transition and returns the original error ([`task_coordinator.go`](../../internal/ingress/a2a/task_coordinator.go#L823)).

This source path matches the observed HTTP error and persisted Failed row. CP66 reconciled the `submitted → working → failed` audit events with the UI/API state and verified the RFC 8785 chain intact across 262 events; audit integrity is not the defect. No product implementation was changed.

## Independent cancellation coverage

A separate `dispatch_async: true` synthetic task returned 201 and appeared in the actual Console as **Submitted** with a **Cancel** action. Clicking Cancel returned HTTP 200; authenticated task readback showed **canceled**, version 2, and zero hold; the UI showed “Canceled A2A task and released held budget.” This validates one submitted-to-canceled Owner path. It does not mitigate DEF-067 because that async test did not dispatch to the endpoint and did not exercise a live `working` task.

## Root-cause assessment

**Code-verified cause:** the synchronous create flow moves the task to `working` before dispatch, then submits the endpoint's `working` state to a transition validator that disallows same-state `working -> working`. Its error recovery marks the already-persisted row Failed, while the API returns a client error rather than the row. This is directly consistent with the observed state and response.

**Not established:** concurrent dispatch races, lost worker responses, or effects on non-RC6 versions.

## Acceptance criteria

1. With a valid registered endpoint returning HTTP 200 and `state: "working"`, synchronous `POST /a2a/tasks` must not return an invalid-transition 4xx or silently create a failed task. Return the created task identifier and a state consistent with the documented A2A contract.
2. Keep `working` as an accepted `UpstreamTaskResponse` state; same-state response handling must be explicit and safe, without duplicate settlement, hold release, or audit side effects.
3. The resulting working task must remain visible through authenticated API and Console reads and expose a valid cancellation action; cancellation must persist `canceled` and release any held budget exactly once.
4. If a task creation failure is persisted, return a correlatable task identifier and truthful state, or roll it back atomically. Do not leave an uncorrelatable Failed row after a rejected create response.
5. Add integration coverage for each supported synchronous upstream state (`working`, `input-required`, `completed`, and `failed`) plus submitted/working cancellation, readback, audit and budget-hold reconciliation.

No remediation was attempted. `GATE-REL-07` remains **BLOCKED**.
