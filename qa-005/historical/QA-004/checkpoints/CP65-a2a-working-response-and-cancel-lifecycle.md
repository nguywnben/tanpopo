# CP65 — A2A working response failure and submitted-task cancellation

**Baseline:** exact RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated A2A-enabled RC6 clone `127.0.0.1:18128`  
**Actor:** synthetic Org Owner  
**Product changes:** none

## Findings

- **FAIL — DEF-067, synchronous upstream `working` response:** Registered a synthetic agent at loopback `127.0.0.1:18133` with private-endpoint opt-in limited to `127.0.0.1/32`. A one-time A2A-only `/a2a/*` Virtual Key dispatched a zero-hold task. The endpoint returned HTTP 200 `{"state":"working","spent_micros":0,"artifacts":[]}`. `POST /a2a/tasks` returned HTTP 400 `invalid_request`, `a2a: invalid SM-09 state transition: working -> working`, without the task ID. The actual Console showed a new **Failed** row with no action; authorized task readback returned HTTP 200, state `failed`, version 3, zero hold and zero settled cost. The fixture observed one `/tasks` call and no forwarded caller Authorization header. This is a product lifecycle defect, not an endpoint reachability or SSRF blocker.
- **PASS (scoped) — submitted-to-canceled lifecycle:** A separate zero-hold `dispatch_async: true` task returned HTTP 201 in `submitted`. The actual Console displayed a **Cancel** action. Clicking it returned HTTP 200; authenticated task readback showed `canceled`, version 2 and zero hold. The Console displayed “Canceled A2A task and released held budget.” The async task did not dispatch to the fixture. This tests submitted cancellation only; it does not mitigate the `working` defect or prove cancellation of an actively dispatched task.
- **Cleanup:** The A2A-only test key was gracefully revoked (HTTP 200); the endpoint fixture was stopped after the run. The registered synthetic agent and task records remain in the isolated test runtime as reproducible QA state. No cost was incurred.

## DEF-067 reproduction and cause

1. Open the A2A-enabled exact-source clone and register the private loopback fixture with only `127.0.0.1/32` allowed.
2. Create an environment-bound key with A2A as its only plane and `/a2a/*` as its route grant.
3. Have the endpoint respond to a task dispatch with valid `state: working` and zero spend.
4. Submit a synchronous (`dispatch_async: false`) `POST /a2a/tasks`, then inspect both the HTTP response and real A2A task table/API readback.

The source code confirms the observed sequence: `CreateTask` transitions SUBMITTED to WORKING before dispatch, applies the upstream `working` response through the transition validator, then attempts to mark the task failed when that transition errors. The validator does not allow same-state WORKING → WORKING, although `UpstreamTaskResponse.State` explicitly lists `working` as supported. Full details, expected behavior and acceptance criteria are in [DEF-067](../reports/DEF-067-a2a-working-response-rejected.md).

## Evidence

- [Sanitized lifecycle and state results](../evidence/CP65-a2a-lifecycle-results.json)
- [Deterministic loopback fixture](../evidence/CP65-a2a-working-fixture.py)
- [Failed task in visually inspected Console](../evidence/CP65-a2a-working-response-failed-task.png)
- [Submitted task with Cancel action](../evidence/CP65-a2a-submitted-before-cancel.png)
- [Canceled task and visible success confirmation](../evidence/CP65-a2a-submitted-canceled.png)

The A2A task screenshots were opened and visually inspected. The failure and cancellation rows show zero budget hold and zero settled cost.

A2A remains **PARTIAL**: active-work cancellation, asynchronous dispatch completion, retries/timeouts, error/recovery, role and cross-project isolation, sub-budget holds, and broader endpoint/protocol interoperability remain UNVERIFIED. QA-004 is incomplete; `GATE-REL-07` remains **BLOCKED**.
